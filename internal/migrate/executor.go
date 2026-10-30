// Package migrate aplica em produção as migrações SQL versionadas de
// migrations/, com registro, checksum e advisory lock.
//
// O executor trabalha sobre uma conexão dedicada (*pgx.Conn), nunca sobre um
// pool: o advisory lock é da sessão e os prazos valem por transação. Cada
// arquivo roda numa transação própria, junto com o INSERT que o registra em
// migracao_aplicada. Só existe "para cima": não há down nem CONCURRENTLY.
//
// Todo erro devolvido é um Erro: mensagem fixa, sem DSN, sem trecho de SQL e
// sem texto do servidor. O executor não loga o erro que devolve; quem chama
// decide.
package migrate

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"strconv"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/migrations"
)

// ChaveLockPadrao é a chave do advisory lock de produção (ASCII "bbmigrat").
const ChaveLockPadrao int64 = 0x62626D6967726174

// Padrões usados quando Opcoes deixa um prazo abaixo de 1 ms. A política com
// limites é do pacote config; aqui só se evita um prazo que viraria "0ms", que
// desligaria o limite no PostgreSQL.
const (
	lockTimeoutPadrao      = 5 * time.Second
	statementTimeoutPadrao = 60 * time.Second

	intervaloLock = 100 * time.Millisecond
	prazoLimpeza  = 5 * time.Second

	arquivoControleUp = "controle/0000_migracao_aplicada.up.sql"
)

// Opcoes configura o executor.
type Opcoes struct {
	// LockTimeout é quanto esperar pelo advisory lock e o lock_timeout de cada
	// transação. Menor que 1 ms (zero, negativo ou sub-milissegundo, que o
	// PostgreSQL leria como "0ms" e desligaria o limite) vira 5 s.
	LockTimeout time.Duration
	// StatementTimeout é o statement_timeout de cada transação. Menor que
	// 1 ms vira 60 s. O PostgreSQL o aplica por COMANDO, e o arquivo vai numa
	// única mensagem simple-Query com vários comandos: cada comando tem o seu
	// prazo, e a transação do arquivo NÃO tem teto total de duração. N
	// statements lentos podem ocupar N vezes o prazo, segurando todo o tempo
	// os locks ACCESS EXCLUSIVE tomados no início. O lock_timeout, por sua
	// vez, vale por aquisição de lock, não pelo arquivo.
	StatementTimeout time.Duration
	// Logger recebe os eventos informativos. Nil descarta.
	Logger *slog.Logger
	// ChaveLock é a chave do advisory lock. Zero vira ChaveLockPadrao.
	ChaveLock int64
	// Controle contém controle/0000_migracao_aplicada.up.sql. Nil vira
	// migrations.Controle.
	Controle fs.FS
}

// Estado é a situação de uma migração diante do registro do banco.
type Estado string

// Estados de uma migração.
const (
	// EstadoAplicada: registrada, com o mesmo checksum do arquivo.
	EstadoAplicada Estado = "aplicada"
	// EstadoPendente: no conjunto e ainda não registrada.
	EstadoPendente Estado = "pendente"
	// EstadoDivergente: registrada, mas o arquivo mudou desde então.
	EstadoDivergente Estado = "divergente"
	// EstadoDesconhecida: registrada, mas ausente do conjunto.
	EstadoDesconhecida Estado = "desconhecida"
)

// Situacao é a linha de Status de uma versão.
type Situacao struct {
	Versao int
	Nome   string
	Estado Estado
	// AplicadaEm, em UTC, é o instante do registro; zero quando pendente.
	AplicadaEm time.Time
}

// Executor aplica migrações numa conexão dedicada.
type Executor struct {
	conn     *pgx.Conn
	fsys     fs.FS
	op       Opcoes
	controle fs.FS
}

// Novo cria um Executor sobre conn (dedicada ao executor) e as migrações de
// fsys. Não faz E/S.
func Novo(conn *pgx.Conn, fsys fs.FS, op Opcoes) *Executor {
	if op.Logger == nil {
		op.Logger = slog.New(slog.DiscardHandler)
	}
	if op.ChaveLock == 0 {
		op.ChaveLock = ChaveLockPadrao
	}
	if op.LockTimeout < time.Millisecond {
		op.LockTimeout = lockTimeoutPadrao
	}
	if op.StatementTimeout < time.Millisecond {
		op.StatementTimeout = statementTimeoutPadrao
	}
	controle := op.Controle
	if controle == nil {
		controle = migrations.Controle
	}
	return &Executor{conn: conn, fsys: fsys, op: op, controle: controle}
}

// Subir aplica, em ordem, as migrações pendentes e devolve as que aplicou
// nesta chamada. O fluxo é:
//
//  1. valida o conjunto inteiro, sem efeito no banco;
//  2. pega o advisory lock (ErrLockOcupado depois de LockTimeout);
//  3. lê o registro e confere contra o conjunto: versão desconhecida, lacuna,
//     checksum divergente, schema com objetos e sem registro;
//  4. recusa CONCURRENTLY em qualquer pendente;
//  5. cria a tabela de controle, se não existir, e aplica cada pendente numa
//     transação própria, junto com o seu registro;
//  6. solta o lock.
//
// Se o arquivo k falha, só a transação dele é desfeita: as anteriores ficam
// aplicadas (e vêm no resultado, junto com o erro) e a próxima execução
// continua do k.
func (e *Executor) Subir(ctx context.Context) ([]Migracao, error) {
	migs, err := carregar(e.fsys)
	if err != nil {
		return nil, err
	}
	if err := e.travar(ctx); err != nil {
		return nil, err
	}
	defer e.destravar(ctx)

	insp, err := e.inspecionar(ctx)
	if err != nil {
		return nil, err
	}
	if err := conferir(migs, insp.registro); err != nil {
		return nil, err
	}
	pendentes := migs[len(insp.registro):]
	for _, m := range pendentes {
		if TemConcurrently(m.sql) {
			err := ErroConcurrently
			err.Arquivo = m.arquivo
			return nil, err
		}
	}
	if len(pendentes) == 0 {
		return nil, nil
	}
	if !insp.controle {
		if err := e.criarControle(ctx); err != nil {
			return nil, err
		}
	}

	var aplicadas []Migracao
	for _, m := range pendentes {
		inicio := time.Now()
		if err := e.aplicar(ctx, m, insp.sessao); err != nil {
			return aplicadas, err
		}
		aplicadas = append(aplicadas, m.Migracao)
		e.op.Logger.Info("migração aplicada",
			slog.Int("versao", m.Versao),
			slog.String("nome", m.Nome),
			slog.Int64("duracao_ms", time.Since(inicio).Milliseconds()))
	}
	return aplicadas, nil
}

// Status devolve a situação de cada versão, em ordem. É somente leitura: não
// pega o advisory lock, não cria a tabela de controle e roda numa transação
// READ ONLY. Divergente e desconhecida são estados, não erro; lacuna no
// registro, controle estranho, schema indefinido e schema sem registro são
// erro.
func (e *Executor) Status(ctx context.Context) ([]Situacao, error) {
	migs, err := carregar(e.fsys)
	if err != nil {
		return nil, err
	}
	insp, err := e.inspecionar(ctx)
	if err != nil {
		return nil, err
	}
	if err := conferirLacuna(insp.registro); err != nil {
		return nil, err
	}

	noFS := make(map[int]arquivoMigracao, len(migs))
	for _, m := range migs {
		noFS[m.Versao] = m
	}
	out := make([]Situacao, 0, len(migs))
	// O registro vai de 1 a n e o conjunto de 1 a N: a união ordenada por
	// versão é o registro seguido das pendentes ou, se o registro tem
	// versões além do conjunto, o conjunto seguido das desconhecidas.
	for _, r := range insp.registro {
		m, ok := noFS[r.versao]
		switch {
		case !ok:
			out = append(out, Situacao{Versao: r.versao, Nome: r.nome, Estado: EstadoDesconhecida, AplicadaEm: r.aplicadaEm})
		case r.checksum != m.Checksum || r.nome != m.Nome:
			out = append(out, Situacao{Versao: r.versao, Nome: m.Nome, Estado: EstadoDivergente, AplicadaEm: r.aplicadaEm})
		default:
			out = append(out, Situacao{Versao: r.versao, Nome: m.Nome, Estado: EstadoAplicada, AplicadaEm: r.aplicadaEm})
		}
	}
	for _, m := range migs {
		if m.Versao > len(insp.registro) {
			out = append(out, Situacao{Versao: m.Versao, Nome: m.Nome, Estado: EstadoPendente})
		}
	}
	return out, nil
}

// ---- registro versus conjunto ----

// linhaRegistro é uma linha de migracao_aplicada.
type linhaRegistro struct {
	versao     int
	nome       string
	checksum   string
	aplicadaEm time.Time
}

// conferirLacuna exige que o registro seja as versões 1..n, em sequência.
func conferirLacuna(reg []linhaRegistro) error {
	for i, r := range reg {
		if r.versao != i+1 {
			return ErroRegistroComLacuna
		}
	}
	return nil
}

// conferir compara o registro com o conjunto: versão ausente do conjunto,
// lacuna e, por fim, checksum (e nome) de todas as aplicadas.
func conferir(migs []arquivoMigracao, reg []linhaRegistro) error {
	noFS := make(map[int]arquivoMigracao, len(migs))
	for _, m := range migs {
		noFS[m.Versao] = m
	}
	for _, r := range reg {
		if _, ok := noFS[r.versao]; !ok {
			return ErroVersaoDesconhecida
		}
	}
	if err := conferirLacuna(reg); err != nil {
		return err
	}
	for _, r := range reg {
		if m := noFS[r.versao]; r.checksum != m.Checksum || r.nome != m.Nome {
			err := ErroChecksumDivergente
			err.Arquivo = m.arquivo
			return err
		}
	}
	return nil
}

// ---- advisory lock ----

// travar espera pelo advisory lock em laço de pg_try_advisory_lock, até
// LockTimeout ou o fim de ctx.
func (e *Executor) travar(ctx context.Context) error {
	limite := time.NewTimer(e.op.LockTimeout)
	defer limite.Stop()
	for {
		var ok bool
		if err := e.conn.QueryRow(ctx, "SELECT pg_try_advisory_lock($1)", e.op.ChaveLock).Scan(&ok); err != nil {
			return e.erro(ctx, err, "")
		}
		if ok {
			e.op.Logger.Info("lock adquirido")
			return nil
		}
		espera := time.NewTimer(intervaloLock)
		select {
		case <-ctx.Done():
			espera.Stop()
			return ErroCancelado
		case <-limite.C:
			espera.Stop()
			return ErrLockOcupado
		case <-espera.C:
		}
	}
}

// destravar solta o advisory lock com um contexto novo, para soltá-lo mesmo
// com ctx cancelado. Se a conexão já caiu, o servidor soltou o lock.
func (e *Executor) destravar(ctx context.Context) {
	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), prazoLimpeza)
	defer cancel()
	var solto bool
	if err := e.conn.QueryRow(ctx, "SELECT pg_advisory_unlock($1)", e.op.ChaveLock).Scan(&solto); err != nil {
		return
	}
	if !solto {
		e.op.Logger.Warn("advisory lock já estava solto")
	}
}

// ---- transação ----

// emTx roda f numa transação: BEGIN (READ ONLY ISOLATION LEVEL REPEATABLE READ
// se somenteLeitura, para a inspeção enxergar um único instantâneo), os prazos
// por set_config local à transação, f e COMMIT. Em erro, ROLLBACK com contexto
// novo. Os prazos não vazam para a sessão. arquivo é o arquivo que a
// transação aplica, para os erros de banco.
func (e *Executor) emTx(ctx context.Context, arquivo string, somenteLeitura bool, f func() error) (err error) {
	begin := "BEGIN"
	if somenteLeitura {
		begin = "BEGIN READ ONLY ISOLATION LEVEL REPEATABLE READ"
	}
	if _, err := e.conn.Exec(ctx, begin); err != nil {
		return e.erro(ctx, err, arquivo)
	}
	feito := false
	defer func() {
		if feito {
			return
		}
		rctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), prazoLimpeza)
		defer cancel()
		// Com a conexão caída o ROLLBACK falha e o servidor já descartou a
		// transação; com a transação já encerrada ele é só um aviso.
		_, _ = e.conn.Exec(rctx, "ROLLBACK")
	}()

	if _, err := e.conn.Exec(ctx, "SELECT set_config('lock_timeout', $1, true), set_config('statement_timeout', $2, true)",
		milissegundos(e.op.LockTimeout), milissegundos(e.op.StatementTimeout)); err != nil {
		return e.erro(ctx, err, arquivo)
	}
	if err := f(); err != nil {
		return e.erro(ctx, err, arquivo)
	}
	tag, err := e.conn.Exec(ctx, "COMMIT")
	if err != nil {
		return e.erro(ctx, err, arquivo)
	}
	if tag.String() != "COMMIT" {
		// O servidor desfez a transação no lugar de confirmá-la.
		err := ErroBanco
		err.Arquivo = arquivo
		return err
	}
	feito = true
	return nil
}

func milissegundos(d time.Duration) string {
	return strconv.FormatInt(d.Milliseconds(), 10) + "ms"
}

// erro converte err num Erro sem detalhe do servidor. Um Erro já pronto passa
// como está; cancelamento de ctx vira ErroCancelado; *pgconn.PgError vira
// ErroSQL (com arquivo) ou ErroBanco (sem arquivo), só com SQLSTATE e nome da
// constraint; o resto vira ErroBanco.
func (e *Executor) erro(ctx context.Context, err error, arquivo string) Erro {
	var pronto Erro
	if errors.As(err, &pronto) {
		return pronto
	}
	if ctx.Err() != nil || errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
		return ErroCancelado
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		out := ErroBanco
		if arquivo != "" {
			out = ErroSQL
		}
		out.Arquivo, out.Code, out.Constraint = arquivo, pe.Code, pe.ConstraintName
		return out
	}
	out := ErroBanco
	out.Arquivo = arquivo
	return out
}

// ---- inspeção do banco ----

// regclassControle é a tabela de controle no schema corrente, sem depender do
// resto do search_path.
const regclassControle = "to_regclass(quote_ident(current_schema()) || '.migracao_aplicada')"

// sessao é o schema corrente e o papel atual vistos pela inspeção. O registro é
// sempre gravado e lido no schema capturado, e a sessão não pode mudar de
// schema nem de papel durante um arquivo de migração.
type sessao struct {
	schema  string
	usuario string
}

// inspecao é o que o banco diz sobre o registro.
type inspecao struct {
	sessao   sessao
	controle bool // a tabela migracao_aplicada existe
	registro []linhaRegistro
}

// inspecionar lê o registro numa transação READ ONLY. Falha fechada quando o
// schema é indefinido, o controle tem estrutura estranha ou o schema tem
// objetos e nenhum registro.
func (e *Executor) inspecionar(ctx context.Context) (insp inspecao, err error) {
	err = e.emTx(ctx, "", true, func() error {
		var schema *string
		var usuario string
		if err := e.conn.QueryRow(ctx, "SELECT current_schema(), current_user::text, "+regclassControle+" IS NOT NULL").Scan(&schema, &usuario, &insp.controle); err != nil {
			return err
		}
		if schema == nil {
			return ErroSchemaIndefinido
		}
		insp.sessao = sessao{schema: *schema, usuario: usuario}
		if insp.controle {
			if err := e.validarEstrutura(ctx); err != nil {
				return err
			}
			reg, err := e.lerRegistro(ctx, insp.sessao)
			if err != nil {
				return err
			}
			insp.registro = reg
		}
		if len(insp.registro) == 0 {
			var objetos bool
			if err := e.conn.QueryRow(ctx, `SELECT EXISTS (
				SELECT 1 FROM pg_class c
				 WHERE c.relnamespace = to_regnamespace(quote_ident(current_schema()))
				   AND c.relkind IN ('r', 'p', 'v', 'm', 'f', 'S')
				   AND c.relname <> 'migracao_aplicada')`).Scan(&objetos); err != nil {
				return err
			}
			if objetos {
				return ErroBancoSemRegistro
			}
		}
		return nil
	})
	if err != nil {
		return inspecao{}, err
	}
	return insp, nil
}

// tabelaRegistro é migracao_aplicada qualificada pelo schema inspecionado, com
// o identificador escapado: nem o search_path nem uma tabela temporária de mesmo
// nome redirecionam o INSERT e o SELECT do registro.
func (s sessao) tabelaRegistro() string {
	return pgx.Identifier{s.schema, "migracao_aplicada"}.Sanitize()
}

// lerRegistro lê migracao_aplicada no schema inspecionado.
func (e *Executor) lerRegistro(ctx context.Context, s sessao) ([]linhaRegistro, error) {
	rows, err := e.conn.Query(ctx, "SELECT versao, nome, checksum, aplicada_em FROM "+s.tabelaRegistro()+" ORDER BY versao")
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var reg []linhaRegistro
	for rows.Next() {
		var r linhaRegistro
		if err := rows.Scan(&r.versao, &r.nome, &r.checksum, &r.aplicadaEm); err != nil {
			return nil, err
		}
		r.aplicadaEm = r.aplicadaEm.UTC()
		reg = append(reg, r)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return reg, nil
}

// colunasControle é a estrutura esperada de migracao_aplicada.
var colunasControle = map[string]string{
	"versao":      "integer",
	"nome":        "text",
	"checksum":    "text",
	"aplicada_em": "timestamp with time zone",
}

// validarEstrutura confere que migracao_aplicada é uma tabela comum, de um dono
// que o papel atual é (ou de que é membro), sem triggers, regras, RLS, herança
// nem partição, com as quatro colunas esperadas (tipo e NOT NULL, nenhuma gerada
// ou de identidade), o único default now() em aplicada_em e a chave primária
// migracao_aplicada_pkey em versao. Tudo por catálogo, sem invocar nada da
// tabela: uma tabela adotada não pode carregar código que o executor dispare.
// Qualquer outra coisa é ErroControleInvalido, sem descrever o que foi achado.
func (e *Executor) validarEstrutura(ctx context.Context) error {
	var relkind string
	var gatilhos, regras, rls, particao, mae, filha, doDono bool
	if err := e.conn.QueryRow(ctx, `SELECT c.relkind::text, c.relhastriggers, c.relhasrules, c.relrowsecurity,
		       c.relispartition, c.relhassubclass,
		       EXISTS (SELECT 1 FROM pg_inherits i WHERE i.inhrelid = c.oid),
		       pg_has_role(current_user, c.relowner, 'USAGE')
		  FROM pg_class c WHERE c.oid = `+regclassControle).Scan(&relkind, &gatilhos, &regras, &rls, &particao, &mae, &filha, &doDono); err != nil {
		return err
	}
	if relkind != "r" || gatilhos || regras || rls || particao || mae || filha || !doDono {
		return ErroControleInvalido
	}

	rows, err := e.conn.Query(ctx, `SELECT a.attname::text, format_type(a.atttypid, a.atttypmod), a.attnotnull,
		       a.attgenerated::text <> '' OR a.attidentity::text <> ''
		  FROM pg_attribute a
		 WHERE a.attrelid = `+regclassControle+` AND a.attnum > 0 AND NOT a.attisdropped`)
	if err != nil {
		return err
	}
	cols := map[string]string{}
	valido := true
	for rows.Next() {
		var nome, tipo string
		var naoNula, gerada bool
		if err := rows.Scan(&nome, &tipo, &naoNula, &gerada); err != nil {
			rows.Close()
			return err
		}
		if want, ok := colunasControle[nome]; !ok || want != tipo || !naoNula || gerada {
			valido = false
		}
		cols[nome] = tipo
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		return err
	}
	if !valido || len(cols) != len(colunasControle) {
		return ErroControleInvalido
	}

	if err := e.validarDefaults(ctx); err != nil {
		return err
	}

	var nomePK string
	var colunasPK []string
	err = e.conn.QueryRow(ctx, `SELECT con.conname::text,
		       ARRAY(SELECT a.attname::text
		               FROM unnest(con.conkey) WITH ORDINALITY AS k(attnum, ord)
		               JOIN pg_attribute a ON a.attrelid = con.conrelid AND a.attnum = k.attnum
		              ORDER BY k.ord)
		  FROM pg_constraint con
		 WHERE con.conrelid = `+regclassControle+` AND con.contype = 'p'`).Scan(&nomePK, &colunasPK)
	if errors.Is(err, pgx.ErrNoRows) {
		return ErroControleInvalido
	}
	if err != nil {
		return err
	}
	if nomePK != "migracao_aplicada_pkey" || len(colunasPK) != 1 || colunasPK[0] != "versao" {
		return ErroControleInvalido
	}
	return nil
}

// validarDefaults exige que o único default do controle seja now() em
// aplicada_em, sem dependência de função do usuário. pg_get_expr só decompila a
// expressão: nada dela é executado.
func (e *Executor) validarDefaults(ctx context.Context) error {
	rows, err := e.conn.Query(ctx, `SELECT a.attname::text, pg_get_expr(d.adbin, d.adrelid),
		       EXISTS (SELECT 1 FROM pg_depend dp
		                WHERE dp.classid = 'pg_attrdef'::regclass AND dp.objid = d.oid
		                  AND dp.refclassid = 'pg_proc'::regclass)
		  FROM pg_attrdef d
		  JOIN pg_attribute a ON a.attrelid = d.adrelid AND a.attnum = d.adnum
		 WHERE d.adrelid = `+regclassControle)
	if err != nil {
		return err
	}
	defer rows.Close()
	valido, vistos := true, 0
	for rows.Next() {
		var coluna, expr string
		var funcaoUsuario bool
		if err := rows.Scan(&coluna, &expr, &funcaoUsuario); err != nil {
			return err
		}
		vistos++
		if coluna != "aplicada_em" || expr != "now()" || funcaoUsuario {
			valido = false
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	if !valido || vistos != 1 {
		return ErroControleInvalido
	}
	return nil
}

// ---- efeitos ----

// criarControle aplica o up do controle numa transação e confere a estrutura
// do resultado.
func (e *Executor) criarControle(ctx context.Context) error {
	sql, err := fs.ReadFile(e.controle, arquivoControleUp)
	if err != nil {
		return ErroControleInvalido
	}
	return e.emTx(ctx, arquivoControleUp, false, func() error {
		if _, err := e.conn.Exec(ctx, string(sql)); err != nil {
			return err
		}
		if e.conn.PgConn().TxStatus() != 'T' {
			return ErroTransacaoEncerrada
		}
		return e.validarEstrutura(ctx)
	})
}

// aplicar roda o .up.sql de m e o INSERT do registro na mesma transação.
func (e *Executor) aplicar(ctx context.Context, m arquivoMigracao, s sessao) error {
	return e.emTx(ctx, m.arquivo, false, func() error {
		if _, err := e.conn.Exec(ctx, m.sql); err != nil {
			return err
		}
		// O arquivo não pode ter encerrado a transação (COMMIT ou ROLLBACK
		// próprio): o registro tem de ir junto com o efeito.
		if e.conn.PgConn().TxStatus() != 'T' {
			err := ErroTransacaoEncerrada
			err.Arquivo = m.arquivo
			return err
		}
		// O arquivo não pode ter mudado de schema nem de papel (SET search_path,
		// SET ROLE...): sem SET LOCAL, o SET persistiria na sessão depois do
		// COMMIT. Se mudou, o erro desfaz a transação e o SET some com ela.
		var schema *string
		var usuario string
		if err := e.conn.QueryRow(ctx, "SELECT current_schema(), current_user::text").Scan(&schema, &usuario); err != nil {
			return err
		}
		if schema == nil || *schema != s.schema || usuario != s.usuario {
			err := ErroSessaoAlterada
			err.Arquivo = m.arquivo
			return err
		}
		// Tabela qualificada pelo schema inspecionado: uma tabela temporária ou
		// outra migracao_aplicada no search_path não recebe o registro.
		_, err := e.conn.Exec(ctx, "INSERT INTO "+s.tabelaRegistro()+" (versao, nome, checksum) VALUES ($1, $2, $3)", m.Versao, m.Nome, m.Checksum)
		return err
	})
}
