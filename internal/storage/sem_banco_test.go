package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
)

// Garante em tempo de compilação que conexão e transação do pgx servem de DBTX.
var (
	_ DBTX = (*pgx.Conn)(nil)
	_ DBTX = (pgx.Tx)(nil)
)

func saoPaulo(t testing.TB) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("carregar America/Sao_Paulo: %v", err)
	}
	return loc
}

// semDados falha se msg contém algum dos trechos proibidos (dado do usuário,
// SQL, detalhe do PostgreSQL).
func semDados(t *testing.T, msg string, proibidos ...string) {
	t.Helper()
	for _, p := range proibidos {
		if p != "" && strings.Contains(msg, p) {
			t.Errorf("mensagem de erro %q contém %q", msg, p)
		}
	}
}

// trechosSQL são palavras que só aparecem numa mensagem se o SQL vazou.
var trechosSQL = []string{"INSERT", "UPDATE", "SELECT", "RETURNING", "WHERE", "insert into", "update ", "select "}

func TestSentinelasStorage(t *testing.T) {
	sentinelas := map[string]error{
		"ErrLivroNaoEncontrado":      ErrLivroNaoEncontrado,
		"ErrUsuarioNaoEncontrado":    ErrUsuarioNaoEncontrado,
		"ErrEmprestimoNaoEncontrado": ErrEmprestimoNaoEncontrado,
		"ErrLivroJaEmprestado":       ErrLivroJaEmprestado,
		"ErrJaPersistido":            ErrJaPersistido,
		"ErrEmprestimoDivergente":    ErrEmprestimoDivergente,
		"ErrListagemInvalida":        ErrListagemInvalida,
	}
	vistos := map[string]string{}
	for nome, err := range sentinelas {
		t.Run(nome, func(t *testing.T) {
			if err == nil {
				t.Fatal("sentinela nil")
			}
			msg := err.Error()
			if !strings.HasPrefix(msg, "storage: ") {
				t.Errorf("Error() = %q, want prefixo %q", msg, "storage: ")
			}
			if outro, ok := vistos[msg]; ok {
				t.Errorf("mensagem %q repetida em %s", msg, outro)
			}
			vistos[msg] = nome
			for outroNome, outro := range sentinelas {
				if outroNome != nome && errors.Is(err, outro) {
					t.Errorf("%s é %s", nome, outroNome)
				}
			}
		})
	}
}

func TestTraduzirErro(t *testing.T) {
	detalhe := "Key (livro_id)=(424242) already exists. Titulo Secreto"
	pg := func(code, constraint string) *pgconn.PgError {
		return &pgconn.PgError{
			Severity:       "ERROR",
			Code:           code,
			Message:        "violação",
			Detail:         detalhe,
			ConstraintName: constraint,
			TableName:      "emprestimo",
			InternalQuery:  "INSERT INTO emprestimo (livro_id) VALUES (424242)",
		}
	}
	redeCaiu := errors.New("conexão perdida")

	tests := []struct {
		name    string
		in      error
		wantIs  error   // erro que o resultado deve ser (errors.Is)
		naoE    []error // sentinelas que o resultado NÃO pode ser
		wantNil bool
	}{
		{name: "nil", in: nil, wantNil: true},
		{name: "23505 no indice de R4", in: pg("23505", "emprestimo_livro_aberto_uq"), wantIs: ErrLivroJaEmprestado},
		{name: "23505 embrulhado no indice de R4", in: fmt.Errorf("camada: %w", pg("23505", "emprestimo_livro_aberto_uq")), wantIs: ErrLivroJaEmprestado},
		{
			name: "23505 em outra constraint nao e R4", in: pg("23505", "emprestimo_pkey"),
			naoE: []error{ErrLivroJaEmprestado, ErrLivroNaoEncontrado, ErrUsuarioNaoEncontrado, ErrEmprestimoNaoEncontrado, ErrJaPersistido},
		},
		{
			name: "23505 sem constraint nao e R4", in: pg("23505", ""),
			naoE: []error{ErrLivroJaEmprestado},
		},
		{
			name: "23514 no nome da constraint de R4 nao e R4", in: pg("23514", "emprestimo_livro_aberto_uq"),
			naoE: []error{ErrLivroJaEmprestado},
		},
		{name: "23503 na FK de livro", in: pg("23503", "emprestimo_livro_id_fkey"), wantIs: ErrLivroNaoEncontrado, naoE: []error{ErrUsuarioNaoEncontrado}},
		{name: "23503 na FK de usuario", in: pg("23503", "emprestimo_usuario_id_fkey"), wantIs: ErrUsuarioNaoEncontrado, naoE: []error{ErrLivroNaoEncontrado}},
		{
			name: "23503 em outra FK", in: pg("23503", "outra_fkey"),
			naoE: []error{ErrLivroNaoEncontrado, ErrUsuarioNaoEncontrado, ErrLivroJaEmprestado},
		},
		{
			name: "23505 na FK de livro (codigo errado)", in: pg("23505", "emprestimo_livro_id_fkey"),
			naoE: []error{ErrLivroNaoEncontrado, ErrLivroJaEmprestado},
		},
		{
			name: "23514 check", in: pg("23514", "livro_titulo_check"),
			naoE: []error{ErrLivroJaEmprestado, ErrLivroNaoEncontrado, ErrUsuarioNaoEncontrado},
		},
		{name: "erro qualquer fica embrulhado", in: redeCaiu, wantIs: redeCaiu, naoE: []error{ErrLivroJaEmprestado}},
		{name: "context.Canceled fica embrulhado", in: context.Canceled, wantIs: context.Canceled},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := traduzirErro(tt.in)
			if tt.wantNil {
				if got != nil {
					t.Fatalf("traduzirErro(nil) = %v, want nil", got)
				}
				return
			}
			if got == nil {
				t.Fatalf("traduzirErro(%v) = nil, want erro", tt.in)
			}
			if tt.wantIs != nil && !errors.Is(got, tt.wantIs) {
				t.Errorf("traduzirErro(%v) = %v, want errors.Is %v", tt.in, got, tt.wantIs)
			}
			for _, n := range tt.naoE {
				if errors.Is(got, n) {
					t.Errorf("traduzirErro(%v) = %v, não pode ser %v", tt.in, got, n)
				}
			}
			semDados(t, got.Error(), append([]string{detalhe, "424242", "Titulo Secreto", "Key ("}, trechosSQL...)...)
		})
	}
}

func TestVencimentoParaDate(t *testing.T) {
	sp := saoPaulo(t)
	utc14 := time.FixedZone("UTC+14", 14*60*60)

	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{name: "12:00 SP", in: time.Date(2026, 3, 24, 12, 0, 0, 0, sp), want: "2026-03-24"},
		{name: "12:00 SP em 04/11/2018 (sem 00:00 em SP)", in: time.Date(2018, 11, 4, 12, 0, 0, 0, sp), want: "2018-11-04"},
		{name: "12:00 SP em 17/02/2019 (fim do horario de verao)", in: time.Date(2019, 2, 17, 12, 0, 0, 0, sp), want: "2019-02-17"},
		{name: "mesmo instante expresso em UTC+14 (outro dia no fuso do valor)", in: time.Date(2018, 11, 4, 12, 0, 0, 0, sp).In(utc14), want: "2018-11-04"},
		{name: "23:30 SP expresso em UTC (dia seguinte em UTC)", in: time.Date(2026, 3, 10, 23, 30, 0, 0, sp).UTC(), want: "2026-03-10"},
		{name: "virada de ano", in: time.Date(2026, 12, 31, 12, 0, 0, 0, sp), want: "2026-12-31"},
	}
	m := pgtype.NewMap()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := vencimentoParaDate(tt.in)
			if got.Location() != time.UTC {
				t.Errorf("Location() = %v, want UTC", got.Location())
			}
			if h, mi, s := got.Clock(); h != 0 || mi != 0 || s != 0 || got.Nanosecond() != 0 {
				t.Errorf("hora = %s, want 00:00:00", got.Format("15:04:05.999999999"))
			}
			if s := got.Format(time.DateOnly); s != tt.want {
				t.Errorf("data = %s, want %s", s, tt.want)
			}
			// O que vai para o fio como DATE.
			buf, err := m.Encode(pgtype.DateOID, pgtype.TextFormatCode, got, nil)
			if err != nil {
				t.Fatalf("Encode DATE: %v", err)
			}
			if string(buf) != tt.want {
				t.Errorf("DATE codificado = %q, want %q", buf, tt.want)
			}
		})
	}
}

// dbFalso implementa DBTX sem banco: conta as chamadas e devolve err em todas.
type dbFalso struct {
	err      error
	chamadas int
}

type linhaFalsa struct{ err error }

func (l linhaFalsa) Scan(...any) error { return l.err }

func (d *dbFalso) Exec(context.Context, string, ...any) (pgconn.CommandTag, error) {
	d.chamadas++
	return pgconn.CommandTag{}, d.err
}

func (d *dbFalso) Query(context.Context, string, ...any) (pgx.Rows, error) {
	d.chamadas++
	return nil, d.err
}

func (d *dbFalso) QueryRow(context.Context, string, ...any) pgx.Row {
	d.chamadas++
	return linhaFalsa{err: d.err}
}

func emprestimoValido(t *testing.T) domain.Emprestimo {
	t.Helper()
	e, err := domain.NovoEmprestimo(1, 1, time.Date(2026, 3, 10, 10, 0, 0, 0, saoPaulo(t)))
	if err != nil {
		t.Fatalf("NovoEmprestimo: %v", err)
	}
	return e
}

func emprestimoDevolvido(t *testing.T) domain.Emprestimo {
	t.Helper()
	e := emprestimoValido(t)
	if err := e.Devolver(time.Date(2026, 3, 20, 10, 0, 0, 0, saoPaulo(t))); err != nil {
		t.Fatalf("Devolver: %v", err)
	}
	return e
}

func reidratado(t *testing.T, id int64, devolvido bool) domain.Emprestimo {
	t.Helper()
	sp := saoPaulo(t)
	var dev time.Time
	if devolvido {
		dev = time.Date(2026, 3, 20, 10, 0, 0, 0, sp)
	}
	e, err := domain.ReidratarEmprestimo(id, 1, 1, time.Date(2026, 3, 10, 10, 0, 0, 0, sp), time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC), dev, 0)
	if err != nil {
		t.Fatalf("ReidratarEmprestimo: %v", err)
	}
	return e
}

// reidratadoRenovacoes reidrata o empréstimo aberto de reidratado com o
// contador dado (sem passar por Renovar).
func reidratadoRenovacoes(t *testing.T, id int64, renovacoes int) domain.Emprestimo {
	t.Helper()
	e, err := domain.ReidratarEmprestimo(id, 1, 1, time.Date(2026, 3, 10, 10, 0, 0, 0, saoPaulo(t)), time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), time.Time{}, renovacoes)
	if err != nil {
		t.Fatalf("ReidratarEmprestimo: %v", err)
	}
	return e
}

// renovadoSemID devolve um NovoEmprestimo renovado uma vez: válido e com
// contador 1, mas sem ID.
func renovadoSemID(t *testing.T) domain.Emprestimo {
	t.Helper()
	e := emprestimoValido(t)
	if err := e.Renovar(time.Date(2026, 3, 12, 10, 0, 0, 0, saoPaulo(t))); err != nil {
		t.Fatalf("Renovar: %v", err)
	}
	return e
}

// listar adapta ListarLivros às tabelas de chamada; a lista devolvida com erro
// precisa ser vazia.
func listar(ctx context.Context, apos int64, limite int) func(r *Repositorio) error {
	return func(r *Repositorio) error {
		got, mais, err := r.ListarLivros(ctx, apos, limite)
		if err != nil && (len(got) != 0 || mais) {
			return fmt.Errorf("ListarLivros com erro devolveu %d itens e temMais=%v: %w", len(got), mais, err)
		}
		return err
	}
}

// linhasComErro é um pgx.Rows que termina sem linhas com err em Err(), como
// uma conexão perdida no meio da leitura.
type linhasComErro struct {
	err     error
	fechado bool
}

func (l *linhasComErro) Close()                                       { l.fechado = true }
func (l *linhasComErro) Err() error                                   { return l.err }
func (l *linhasComErro) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (l *linhasComErro) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (l *linhasComErro) Next() bool                                   { return false }
func (l *linhasComErro) Scan(...any) error                            { return l.err }
func (l *linhasComErro) Values() ([]any, error)                       { return nil, l.err }
func (l *linhasComErro) RawValues() [][]byte                          { return nil }
func (l *linhasComErro) Conn() *pgx.Conn                              { return nil }
func (l *linhasComErro) TypeMap() *pgtype.Map                         { return pgtype.NewMap() }

// dbLinhasComErro devolve linhasComErro em Query.
type dbLinhasComErro struct {
	dbFalso
	linhas *linhasComErro
}

func (d *dbLinhasComErro) Query(context.Context, string, ...any) (pgx.Rows, error) {
	d.chamadas++
	return d.linhas, nil
}

// Erro no meio da leitura das linhas (rows.Err) não vira página vazia.
func TestListarLivrosErroDuranteLeitura(t *testing.T) {
	falha := errors.New("conexão perdida no meio")
	db := &dbLinhasComErro{linhas: &linhasComErro{err: falha}}
	got, mais, err := NovoRepositorio(db).ListarLivros(context.Background(), 0, 20)
	if !errors.Is(err, falha) {
		t.Errorf("erro = %v, want %v", err, falha)
	}
	if len(got) != 0 || mais {
		t.Errorf("com erro devolveu %d itens e temMais=%v", len(got), mais)
	}
	if !db.linhas.fechado {
		t.Error("rows não foi fechado")
	}
}

// O teto da listagem é 100 (D-19): o handler valida contra ele e o
// repositório revalida.
func TestLimiteListagemLivros(t *testing.T) {
	if LimiteListagemLivros != 100 {
		t.Errorf("LimiteListagemLivros = %d, want 100", LimiteListagemLivros)
	}
}

// Os extremos válidos da listagem passam pela validação e chegam ao banco.
func TestListarLivrosLimitesAceitos(t *testing.T) {
	tests := []struct {
		name   string
		apos   int64
		limite int
	}{
		{name: "início com limite 1", apos: 0, limite: 1},
		{name: "início com teto", apos: 0, limite: LimiteListagemLivros},
		{name: "apos máximo", apos: 1<<63 - 1, limite: 20},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			falha := errors.New("dbFalso: chamado")
			db := &dbFalso{err: falha}
			got, mais, err := NovoRepositorio(db).ListarLivros(context.Background(), tt.apos, tt.limite)
			if !errors.Is(err, falha) {
				t.Errorf("erro = %v, want o erro do banco (validação não deveria recusar)", err)
			}
			if db.chamadas != 1 {
				t.Errorf("%d chamada(s) ao banco, want 1", db.chamadas)
			}
			if len(got) != 0 || mais {
				t.Errorf("com erro devolveu %d itens e temMais=%v, want nada", len(got), mais)
			}
		})
	}
}

// As validações de entrada acontecem antes de qualquer SQL.
func TestRepositorioRecusaAntesDoSQL(t *testing.T) {
	ctx := context.Background()

	tests := []struct {
		name    string
		chamar  func(r *Repositorio) error
		wantErr error
	}{
		{name: "InserirLivro com ID", chamar: func(r *Repositorio) error {
			_, err := r.InserirLivro(ctx, domain.Livro{ID: 3, Titulo: "Dom Casmurro"})
			return err
		}, wantErr: ErrJaPersistido},
		{name: "InserirLivro com titulo vazio", chamar: func(r *Repositorio) error {
			_, err := r.InserirLivro(ctx, domain.Livro{Titulo: " \t "})
			return err
		}, wantErr: domain.ErrTituloVazio},
		{name: "InserirLivro ordem: ID antes de titulo", chamar: func(r *Repositorio) error {
			_, err := r.InserirLivro(ctx, domain.Livro{ID: 3, Titulo: ""})
			return err
		}, wantErr: ErrJaPersistido},
		{name: "InserirUsuario com ID", chamar: func(r *Repositorio) error {
			_, err := r.InserirUsuario(ctx, domain.Usuario{ID: 3, Nome: "Ana"})
			return err
		}, wantErr: ErrJaPersistido},
		{name: "InserirUsuario com nome vazio", chamar: func(r *Repositorio) error {
			_, err := r.InserirUsuario(ctx, domain.Usuario{Nome: "   "})
			return err
		}, wantErr: domain.ErrNomeVazio},
		{name: "AbrirEmprestimo zero-value", chamar: func(r *Repositorio) error {
			_, err := r.AbrirEmprestimo(ctx, domain.Emprestimo{})
			return err
		}, wantErr: domain.ErrEmprestimoInvalido},
		{name: "AbrirEmprestimo ja persistido", chamar: func(r *Repositorio) error {
			_, err := r.AbrirEmprestimo(ctx, reidratado(t, 5, false))
			return err
		}, wantErr: domain.ErrEmprestimoInvalido},
		{name: "AbrirEmprestimo devolvido", chamar: func(r *Repositorio) error {
			_, err := r.AbrirEmprestimo(ctx, emprestimoDevolvido(t))
			return err
		}, wantErr: domain.ErrEmprestimoInvalido},
		{name: "RegistrarDevolucao zero-value", chamar: func(r *Repositorio) error {
			_, err := r.RegistrarDevolucao(ctx, domain.Emprestimo{})
			return err
		}, wantErr: domain.ErrEmprestimoInvalido},
		{name: "RegistrarDevolucao sem ID", chamar: func(r *Repositorio) error {
			_, err := r.RegistrarDevolucao(ctx, emprestimoDevolvido(t))
			return err
		}, wantErr: domain.ErrEmprestimoInvalido},
		{name: "RegistrarDevolucao aberto", chamar: func(r *Repositorio) error {
			_, err := r.RegistrarDevolucao(ctx, reidratado(t, 5, false))
			return err
		}, wantErr: domain.ErrEmprestimoInvalido},
		{name: "RegistrarRenovacao zero-value", chamar: func(r *Repositorio) error {
			_, err := r.RegistrarRenovacao(ctx, domain.Emprestimo{})
			return err
		}, wantErr: domain.ErrEmprestimoInvalido},
		{name: "RegistrarRenovacao sem ID", chamar: func(r *Repositorio) error {
			_, err := r.RegistrarRenovacao(ctx, renovadoSemID(t))
			return err
		}, wantErr: domain.ErrEmprestimoInvalido},
		{name: "RegistrarRenovacao devolvido", chamar: func(r *Repositorio) error {
			e, err := domain.ReidratarEmprestimo(5, 1, 1, time.Date(2026, 3, 10, 10, 0, 0, 0, saoPaulo(t)), time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 20, 10, 0, 0, 0, saoPaulo(t)), 1)
			if err != nil {
				t.Fatalf("ReidratarEmprestimo: %v", err)
			}
			_, err = r.RegistrarRenovacao(ctx, e)
			return err
		}, wantErr: domain.ErrEmprestimoInvalido},
		{name: "RegistrarRenovacao sem renovacao (contador 0)", chamar: func(r *Repositorio) error {
			_, err := r.RegistrarRenovacao(ctx, reidratadoRenovacoes(t, 5, 0))
			return err
		}, wantErr: domain.ErrEmprestimoInvalido},
		{name: "ListarLivros com limite zero", chamar: listar(ctx, 0, 0), wantErr: ErrListagemInvalida},
		{name: "ListarLivros com limite negativo", chamar: listar(ctx, 0, -1), wantErr: ErrListagemInvalida},
		{name: "ListarLivros com limite acima do teto", chamar: listar(ctx, 0, LimiteListagemLivros+1), wantErr: ErrListagemInvalida},
		{name: "ListarLivros com limite muito acima do teto", chamar: listar(ctx, 0, 1<<30), wantErr: ErrListagemInvalida},
		{name: "ListarLivros com apos negativo", chamar: listar(ctx, -1, 10), wantErr: ErrListagemInvalida},
		{name: "ListarLivros com apos mínimo int64", chamar: listar(ctx, -1<<63, 10), wantErr: ErrListagemInvalida},
		{name: "BuscarEmprestimo id zero", chamar: func(r *Repositorio) error {
			_, err := r.BuscarEmprestimo(ctx, 0)
			return err
		}, wantErr: ErrEmprestimoNaoEncontrado},
		{name: "BuscarEmprestimo id negativo", chamar: func(r *Repositorio) error {
			_, err := r.BuscarEmprestimo(ctx, -7)
			return err
		}, wantErr: ErrEmprestimoNaoEncontrado},
		{name: "BuscarLivro id zero", chamar: func(r *Repositorio) error {
			_, err := r.BuscarLivro(ctx, 0)
			return err
		}, wantErr: ErrLivroNaoEncontrado},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &dbFalso{err: errors.New("dbFalso: não devia ser chamado")}
			err := tt.chamar(NovoRepositorio(db))
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("erro = %v, want %v", err, tt.wantErr)
			}
			if db.chamadas != 0 {
				t.Errorf("%d chamada(s) ao banco, want 0", db.chamadas)
			}
		})
	}
}

// Erros do banco não vazam ID, título, nome, SQL nem o Detail do PostgreSQL.
func TestRepositorioErroSemDados(t *testing.T) {
	ctx := context.Background()
	falha := errors.New("conexão perdida")
	pgCancelado := &pgconn.PgError{Severity: "ERROR", Code: "57014", Message: "canceling statement SEGREDO", Detail: "Titulo Secreto Do Acervo"}
	pgR4 := &pgconn.PgError{Severity: "ERROR", Code: "23505", Message: "duplicate key", ConstraintName: "emprestimo_livro_aberto_uq", Detail: "Key (livro_id)=(424242) already exists."}

	livroE, err := domain.NovoEmprestimo(424242, 515151, time.Date(2026, 3, 10, 10, 0, 0, 0, saoPaulo(t)))
	if err != nil {
		t.Fatalf("NovoEmprestimo: %v", err)
	}
	devolver, err := domain.ReidratarEmprestimo(55555, 424242, 515151, time.Date(2026, 3, 10, 10, 0, 0, 0, saoPaulo(t)), time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC), time.Date(2026, 3, 20, 10, 0, 0, 0, saoPaulo(t)), 0)
	if err != nil {
		t.Fatalf("ReidratarEmprestimo: %v", err)
	}

	renovar, err := domain.ReidratarEmprestimo(55555, 424242, 515151, time.Date(2026, 3, 10, 10, 0, 0, 0, saoPaulo(t)), time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC), time.Time{}, 0)
	if err != nil {
		t.Fatalf("ReidratarEmprestimo: %v", err)
	}
	if err := renovar.Renovar(time.Date(2026, 3, 12, 10, 0, 0, 0, saoPaulo(t))); err != nil {
		t.Fatalf("Renovar: %v", err)
	}

	tests := []struct {
		name      string
		dbErr     error
		chamar    func(r *Repositorio) error
		wantIs    error
		proibidos []string
	}{
		{name: "InserirLivro", dbErr: falha, wantIs: falha, chamar: func(r *Repositorio) error {
			_, err := r.InserirLivro(ctx, domain.Livro{Titulo: "Titulo Secreto Do Acervo"})
			return err
		}, proibidos: []string{"Titulo Secreto Do Acervo"}},
		{name: "BuscarLivro", dbErr: falha, wantIs: falha, chamar: func(r *Repositorio) error {
			_, err := r.BuscarLivro(ctx, 987654)
			return err
		}, proibidos: []string{"987654"}},
		{name: "InserirUsuario", dbErr: falha, wantIs: falha, chamar: func(r *Repositorio) error {
			_, err := r.InserirUsuario(ctx, domain.Usuario{Nome: "Maria Leitora Sigilosa"})
			return err
		}, proibidos: []string{"Maria Leitora Sigilosa"}},
		{name: "BuscarUsuario", dbErr: falha, wantIs: falha, chamar: func(r *Repositorio) error {
			_, err := r.BuscarUsuario(ctx, 876543)
			return err
		}, proibidos: []string{"876543"}},
		{name: "AbrirEmprestimo", dbErr: falha, wantIs: falha, chamar: func(r *Repositorio) error {
			_, err := r.AbrirEmprestimo(ctx, livroE)
			return err
		}, proibidos: []string{"424242", "515151"}},
		{name: "AbrirEmprestimo com R4 violada", dbErr: pgR4, wantIs: ErrLivroJaEmprestado, chamar: func(r *Repositorio) error {
			_, err := r.AbrirEmprestimo(ctx, livroE)
			return err
		}, proibidos: []string{"424242", "515151", "Key ("}},
		{name: "RegistrarDevolucao", dbErr: falha, wantIs: falha, chamar: func(r *Repositorio) error {
			_, err := r.RegistrarDevolucao(ctx, devolver)
			return err
		}, proibidos: []string{"55555", "424242", "515151"}},
		{name: "RegistrarRenovacao", dbErr: falha, wantIs: falha, chamar: func(r *Repositorio) error {
			_, err := r.RegistrarRenovacao(ctx, renovar)
			return err
		}, proibidos: []string{"55555", "424242", "515151"}},
		{name: "BuscarEmprestimoAbertoPorLivro", dbErr: falha, wantIs: falha, chamar: func(r *Repositorio) error {
			_, err := r.BuscarEmprestimoAbertoPorLivro(ctx, 313373)
			return err
		}, proibidos: []string{"313373"}},
		{name: "BuscarEmprestimo", dbErr: falha, wantIs: falha, chamar: func(r *Repositorio) error {
			_, err := r.BuscarEmprestimo(ctx, 646464)
			return err
		}, proibidos: []string{"646464"}},
		{name: "ListarLivros", dbErr: falha, wantIs: falha, chamar: listar(ctx, 757575, 37), proibidos: []string{"757575"}},
		{name: "ListarLivros com PgError", dbErr: pgCancelado, wantIs: pgCancelado, chamar: listar(ctx, 757575, 37),
			proibidos: []string{"757575", "SEGREDO", "Titulo Secreto Do Acervo"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &dbFalso{err: tt.dbErr}
			err := tt.chamar(NovoRepositorio(db))
			if err == nil {
				t.Fatal("erro nil, want erro do banco")
			}
			if !errors.Is(err, tt.wantIs) {
				t.Errorf("erro = %v, want errors.Is %v", err, tt.wantIs)
			}
			if db.chamadas == 0 {
				t.Error("nenhuma chamada ao banco")
			}
			semDados(t, err.Error(), append(tt.proibidos, trechosSQL...)...)
		})
	}
}
