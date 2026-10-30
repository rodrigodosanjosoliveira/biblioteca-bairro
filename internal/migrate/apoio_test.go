package migrate_test

import (
	"bytes"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/url"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"testing/fstest"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/migrate"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/migrations"
)

// realFS devolve as migrações embutidas do repositório.
func realFS() fs.FS { return migrations.FS }

// Contrato de API fixado pelos testes deste pacote (DEL-008). Resumo; o
// handoff do test-writer traz a lista completa.
//
//	func Novo(conn *pgx.Conn, fsys fs.FS, op Opcoes) *Executor
//	func (e *Executor) Subir(ctx context.Context) ([]Migracao, error)
//	func (e *Executor) Status(ctx context.Context) ([]Situacao, error)
//	func ValidarFS(fsys fs.FS) ([]Migracao, error)
//	func TemConcurrently(sql string) bool
//	type Opcoes struct { LockTimeout, StatementTimeout time.Duration; Logger *slog.Logger; ChaveLock int64; Controle fs.FS }
//	type Migracao struct { Versao int; Nome, Checksum string }
//	type Situacao struct { Versao int; Nome string; Estado Estado; AplicadaEm time.Time }
//	type Estado string; EstadoAplicada, EstadoPendente, EstadoDivergente, EstadoDesconhecida
//	type Erro struct { Arquivo, Code, Constraint string; ... }  // comparável, valor, sem Unwrap
//	var ErrLockOcupado, ErroBancoSemRegistro, ErroChecksumDivergente, ErroTransacaoEncerrada,
//	    ErroFSInvalido, ErroConcurrently, ErroVersaoDesconhecida, ErroRegistroComLacuna,
//	    ErroControleInvalido, ErroSchemaIndefinido, ErroSQL, ErroBanco, ErroCancelado Erro
//	const ChaveLockPadrao int64

const (
	envURLBanco         = "BIBLIOTECA_TEST_DATABASE_URL"
	envBancoObrigatorio = "BIBLIOTECA_TEST_DB_OBRIGATORIO"
)

// urlBanco devolve a URL do banco de teste. Sem a variável, o teste é pulado;
// com BIBLIOTECA_TEST_DB_OBRIGATORIO=1, a ausência reprova.
func urlBanco(t *testing.T) string {
	t.Helper()
	u := os.Getenv(envURLBanco)
	if u == "" {
		if os.Getenv(envBancoObrigatorio) == "1" {
			t.Fatalf("%s ausente com %s=1: teste de banco obrigatório", envURLBanco, envBancoObrigatorio)
		}
		t.Skipf("%s não definida: teste de banco pulado", envURLBanco)
	}
	return u
}

// bancoTeste é um schema descartável, exclusivo do teste.
type bancoTeste struct {
	url    string
	schema string
}

// novoBanco cria o schema t_<hex aleatório> e registra o DROP ... CASCADE no
// Cleanup. As conexões abertas por conectar fecham antes do DROP (LIFO).
func novoBanco(t *testing.T) *bancoTeste {
	t.Helper()
	u := urlBanco(t)

	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("gerar nome do schema: %v", err)
	}
	schema := "t_" + hex.EncodeToString(b[:])
	ident := pgx.Identifier{schema}.Sanitize()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, u)
	if err != nil {
		// Sem o erro do pgx: ele traz host, usuário e banco.
		t.Fatalf("conectar ao banco de teste: falhou (confira %s)", envURLBanco)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatalf("criar schema de teste: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(ctx, u)
		if err != nil {
			t.Errorf("conectar para remover schema %s: falhou (confira %s)", schema, envURLBanco)
			return
		}
		defer c.Close(ctx)
		if _, err := c.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE"); err != nil {
			t.Errorf("remover schema %s: %v", schema, err)
		}
	})
	return &bancoTeste{url: u, schema: schema}
}

// dsn devolve a DSN do banco com search_path=<schema> como parâmetro.
func (b *bancoTeste) dsn(t *testing.T) string {
	t.Helper()
	return dsnComSearchPath(t, b.url, b.schema)
}

// dsnComSearchPath acrescenta search_path à URL (formato URL ou chave=valor).
func dsnComSearchPath(t *testing.T, base, searchPath string) string {
	t.Helper()
	if !strings.Contains(base, "://") {
		return base + " search_path=" + searchPath
	}
	u, err := url.Parse(base)
	if err != nil {
		t.Fatalf("interpretar %s: falhou", envURLBanco)
	}
	q := u.Query()
	q.Set("search_path", searchPath)
	u.RawQuery = q.Encode()
	return u.String()
}

// config devolve a configuração de conexão com search_path no schema do
// teste. params extras (chave, valor, ...) vão para RuntimeParams.
func (b *bancoTeste) config(t *testing.T, params ...string) *pgx.ConnConfig {
	t.Helper()
	cfg, err := pgx.ParseConfig(b.url)
	if err != nil {
		t.Fatalf("interpretar %s: falhou", envURLBanco)
	}
	cfg.RuntimeParams["search_path"] = b.schema
	for i := 0; i+1 < len(params); i += 2 {
		cfg.RuntimeParams[params[i]] = params[i+1]
	}
	return cfg
}

func (b *bancoTeste) conectarCfg(t *testing.T, cfg *pgx.ConnConfig) *pgx.Conn {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("conectar ao schema de teste: falhou (confira %s)", envURLBanco)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// conectar abre uma conexão com search_path no schema do teste.
func (b *bancoTeste) conectar(t *testing.T, params ...string) *pgx.Conn {
	t.Helper()
	return b.conectarCfg(t, b.config(t, params...))
}

// pgErro extrai o *pgconn.PgError de err ou reprova.
func pgErro(t *testing.T, err error) *pgconn.PgError {
	t.Helper()
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		t.Fatalf("erro = %v, want *pgconn.PgError", err)
	}
	return pe
}

// chaveAleatoria devolve uma chave de advisory lock própria do teste: o lock é
// por banco, não por schema, e testes de pacotes diferentes rodam em paralelo.
func chaveAleatoria(t *testing.T) int64 {
	t.Helper()
	var b [8]byte
	for {
		if _, err := rand.Read(b[:]); err != nil {
			t.Fatalf("gerar chave de lock: %v", err)
		}
		if k := int64(binary.BigEndian.Uint64(b[:])); k != 0 && k != migrate.ChaveLockPadrao {
			return k
		}
	}
}

// bufLog é um bytes.Buffer seguro para uso concorrente.
type bufLog struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (l *bufLog) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.Write(p)
}

func (l *bufLog) String() string {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.b.String()
}

// tracadorSQL registra o SQL (e os argumentos) enviado por uma conexão.
type tracadorSQL struct {
	mu   sync.Mutex
	sqls []string
	args [][]any
}

func (r *tracadorSQL) TraceQueryStart(ctx context.Context, _ *pgx.Conn, d pgx.TraceQueryStartData) context.Context {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sqls = append(r.sqls, d.SQL)
	r.args = append(r.args, append([]any(nil), d.Args...))
	return ctx
}

func (r *tracadorSQL) TraceQueryEnd(context.Context, *pgx.Conn, pgx.TraceQueryEndData) {}

func (r *tracadorSQL) limpar() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.sqls, r.args = nil, nil
}

func (r *tracadorSQL) copia() ([]string, [][]any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	return append([]string(nil), r.sqls...), append([][]any(nil), r.args...)
}

// ambiente é um schema, uma conexão para o executor, uma segunda sessão para
// observar o banco e uma chave de advisory lock exclusiva do teste.
type ambiente struct {
	b     *bancoTeste
	conn  *pgx.Conn // conexão entregue ao executor
	obs   *pgx.Conn // outra sessão, só para observar
	chave int64
	log   *bufLog
}

func novoAmbiente(t *testing.T) *ambiente {
	t.Helper()
	b := novoBanco(t)
	return &ambiente{b: b, conn: b.conectar(t), obs: b.conectar(t), chave: chaveAleatoria(t), log: &bufLog{}}
}

// exec cria um executor com padrões de teste (lock 3s, statement 30s, log JSON
// em a.log, chave própria); mod ajusta as opções.
func (a *ambiente) exec(fsys fs.FS, mod ...func(*migrate.Opcoes)) *migrate.Executor {
	return a.execEm(a.conn, fsys, mod...)
}

func (a *ambiente) execEm(c *pgx.Conn, fsys fs.FS, mod ...func(*migrate.Opcoes)) *migrate.Executor {
	op := migrate.Opcoes{
		LockTimeout:      3 * time.Second,
		StatementTimeout: 30 * time.Second,
		Logger:           slog.New(slog.NewJSONHandler(a.log, nil)),
		ChaveLock:        a.chave,
	}
	for _, m := range mod {
		m(&op)
	}
	return migrate.Novo(c, fsys, op)
}

// segurarLock abre uma sessão própria que segura o advisory lock da chave até o
// fim do teste (ou até a função devolvida ser chamada).
func (a *ambiente) segurarLock(t *testing.T) (liberar func()) {
	t.Helper()
	c := a.b.conectar(t)
	if _, err := c.Exec(t.Context(), "SELECT pg_advisory_lock($1)", a.chave); err != nil {
		t.Fatalf("segurar advisory lock: %v", err)
	}
	return func() {
		if _, err := c.Exec(context.Background(), "SELECT pg_advisory_unlock($1)", a.chave); err != nil {
			t.Errorf("soltar advisory lock: %v", err)
		}
	}
}

// esperarLockLivre reprova se, em até 5 s, nenhuma outra sessão consegue o
// advisory lock da chave.
func (a *ambiente) esperarLockLivre(t *testing.T) {
	t.Helper()
	c := a.b.conectar(t)
	limite := time.Now().Add(5 * time.Second)
	for {
		var ok bool
		if err := c.QueryRow(t.Context(), "SELECT pg_try_advisory_lock($1)", a.chave).Scan(&ok); err != nil {
			t.Fatalf("pg_try_advisory_lock: %v", err)
		}
		if ok {
			if _, err := c.Exec(t.Context(), "SELECT pg_advisory_unlock($1)", a.chave); err != nil {
				t.Fatalf("pg_advisory_unlock: %v", err)
			}
			return
		}
		if time.Now().After(limite) {
			t.Fatal("advisory lock continua preso depois do Subir/Status: vazamento de lock")
		}
		time.Sleep(50 * time.Millisecond)
	}
}

// lockLivreAgora diz se a chave está livre neste instante.
func (a *ambiente) lockLivreAgora(t *testing.T) bool {
	t.Helper()
	c := a.b.conectar(t)
	var ok bool
	if err := c.QueryRow(t.Context(), "SELECT pg_try_advisory_lock($1)", a.chave).Scan(&ok); err != nil {
		t.Fatalf("pg_try_advisory_lock: %v", err)
	}
	if ok {
		if _, err := c.Exec(t.Context(), "SELECT pg_advisory_unlock($1)", a.chave); err != nil {
			t.Fatalf("pg_advisory_unlock: %v", err)
		}
	}
	return ok
}

// ---- observação do banco ----

type linhaReg struct {
	Versao     int
	Nome       string
	Checksum   string
	AplicadaEm time.Time
	Xmin       string
}

// existeTabela diz se há relação com esse nome no schema corrente da conexão.
func existeTabela(t *testing.T, c *pgx.Conn, nome string) bool {
	t.Helper()
	var ok bool
	err := c.QueryRow(t.Context(),
		`SELECT to_regclass(quote_ident(current_schema()) || '.' || quote_ident($1)) IS NOT NULL`, nome).Scan(&ok)
	if err != nil {
		t.Fatalf("to_regclass(%s): %v", nome, err)
	}
	return ok
}

// registro devolve as linhas de migracao_aplicada (nil se a tabela não existe).
func registro(t *testing.T, c *pgx.Conn) []linhaReg {
	t.Helper()
	if !existeTabela(t, c, "migracao_aplicada") {
		return nil
	}
	rows, err := c.Query(t.Context(),
		`SELECT versao, nome, checksum, aplicada_em, xmin::text FROM migracao_aplicada ORDER BY versao`)
	if err != nil {
		t.Fatalf("ler migracao_aplicada: %v", err)
	}
	defer rows.Close()
	var out []linhaReg
	for rows.Next() {
		var l linhaReg
		if err := rows.Scan(&l.Versao, &l.Nome, &l.Checksum, &l.AplicadaEm, &l.Xmin); err != nil {
			t.Fatalf("ler linha do registro: %v", err)
		}
		out = append(out, l)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar registro: %v", err)
	}
	return out
}

func versoes(rs []linhaReg) []int {
	var v []int
	for _, r := range rs {
		v = append(v, r.Versao)
	}
	return v
}

// relacoes lista os nomes das relações do schema corrente (tabelas, índices,
// sequências, views...), ordenados, ignorando a tabela de controle e o índice
// da sua PK quando ignorarControle é true.
func relacoes(t *testing.T, c *pgx.Conn, ignorarControle bool) []string {
	t.Helper()
	rows, err := c.Query(t.Context(), `
		SELECT cl.relname FROM pg_class cl
		 WHERE cl.relnamespace = to_regnamespace(quote_ident(current_schema()))
		 ORDER BY 1`)
	if err != nil {
		t.Fatalf("listar relações: %v", err)
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("ler relação: %v", err)
		}
		if ignorarControle && (n == "migracao_aplicada" || n == "migracao_aplicada_pkey") {
			continue
		}
		out = append(out, n)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar relações: %v", err)
	}
	sort.Strings(out)
	return out
}

func igual(a, b []string) bool { return strings.Join(a, ",") == strings.Join(b, ",") }

// semAplicar reprova se o schema tem qualquer relação além do controle ou se o
// registro tem linhas.
func semAplicar(t *testing.T, c *pgx.Conn) {
	t.Helper()
	if r := relacoes(t, c, true); len(r) != 0 {
		t.Errorf("relações no schema = %v, want nenhuma além do controle", r)
	}
	if rs := registro(t, c); len(rs) != 0 {
		t.Errorf("registro = %+v, want vazio", rs)
	}
}

func sha(b string) string {
	h := sha256.Sum256([]byte(b))
	return hex.EncodeToString(h[:])
}

// ---- FS sintético ----

// mig descreve uma migração do MapFS; Down vazio vira "SELECT 1;".
type mig struct {
	V    int
	Nome string
	Up   string
	Down string
}

func (m mig) arquivo(dir string) string { return fmt.Sprintf("%04d_%s.%s.sql", m.V, m.Nome, dir) }

func novoFS(ms ...mig) fstest.MapFS {
	f := fstest.MapFS{}
	for _, m := range ms {
		down := m.Down
		if down == "" {
			down = "SELECT 1;"
		}
		f[m.arquivo("up")] = &fstest.MapFile{Data: []byte(m.Up)}
		f[m.arquivo("down")] = &fstest.MapFile{Data: []byte(down)}
	}
	return f
}

// tresMigracoes: a, b e c, cada uma cria uma tabela.
func tresMigracoes() []mig {
	return []mig{
		{V: 1, Nome: "a", Up: "CREATE TABLE a (x integer);\n"},
		{V: 2, Nome: "b", Up: "CREATE TABLE b (x integer);\n"},
		{V: 3, Nome: "c", Up: "CREATE TABLE c (x integer);\n"},
	}
}

// semDetalhe confere o contrato de erro do executor: tipo migrate.Erro (valor),
// sem Unwrap e sem *pgconn.PgError na cadeia.
func semDetalhe(t *testing.T, err error) {
	t.Helper()
	if err == nil {
		return
	}
	var me migrate.Erro
	if !errors.As(err, &me) {
		t.Errorf("erro %T (%v), want migrate.Erro", err, err)
	}
	if u := errors.Unwrap(err); u != nil {
		t.Errorf("erro embrulha outro (%T), want string constante sem Unwrap", u)
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		t.Errorf("erro expõe *pgconn.PgError (%q)", pe.Code)
	}
}

// erroDe extrai o migrate.Erro de err ou reprova.
func erroDe(t *testing.T, err error) migrate.Erro {
	t.Helper()
	var me migrate.Erro
	if !errors.As(err, &me) {
		t.Fatalf("erro = %v (%T), want migrate.Erro", err, err)
	}
	return me
}
