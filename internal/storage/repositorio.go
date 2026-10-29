// Package storage persiste o modelo de empréstimo no PostgreSQL com pgx/v5.
package storage

import (
	"context"
	"errors"
	"fmt"
	"time"
	_ "time/tzdata" // embute a base de fusos: a conversão não depende do sistema.

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
)

var (
	// ErrLivroNaoEncontrado indica livro inexistente.
	ErrLivroNaoEncontrado = errors.New("storage: livro não encontrado")
	// ErrUsuarioNaoEncontrado indica usuário inexistente.
	ErrUsuarioNaoEncontrado = errors.New("storage: usuário não encontrado")
	// ErrEmprestimoNaoEncontrado indica empréstimo inexistente (ou, na busca
	// por livro, nenhum empréstimo aberto).
	ErrEmprestimoNaoEncontrado = errors.New("storage: empréstimo não encontrado")
	// ErrLivroJaEmprestado indica violação de R4: o livro já tem um
	// empréstimo aberto.
	ErrLivroJaEmprestado = errors.New("storage: livro já emprestado")
	// ErrJaPersistido indica inserção de entidade que já tem ID.
	ErrJaPersistido = errors.New("storage: entidade já persistida")
	// ErrEmprestimoDivergente indica que o empréstimo passado a
	// RegistrarDevolucao não corresponde ao gravado: o ID existe e está
	// aberto, mas o início difere do lido do banco (valor reidratado à mão
	// ou adulterado). Nada é gravado.
	ErrEmprestimoDivergente = errors.New("storage: empréstimo diverge do gravado")
)

// Nomes das constraints mapeadas para sentinelas (migrations/0001).
const (
	constraintLivroAberto = "emprestimo_livro_aberto_uq"
	constraintLivroFK     = "emprestimo_livro_id_fkey"
	constraintUsuarioFK   = "emprestimo_usuario_id_fkey"
)

var locSaoPaulo = carregarSaoPaulo()

func carregarSaoPaulo() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		panic("storage: carregar America/Sao_Paulo: " + err.Error())
	}
	return loc
}

// DBTX é o subconjunto comum a *pgx.Conn e pgx.Tx usado pelo repositório.
// Quem chama decide a transação.
type DBTX interface {
	Exec(ctx context.Context, sql string, arguments ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
}

// Repositorio grava e lê livros, usuários e empréstimos.
type Repositorio struct {
	db DBTX
}

// NovoRepositorio cria um Repositorio sobre db (conexão ou transação).
func NovoRepositorio(db DBTX) *Repositorio {
	return &Repositorio{db: db}
}

// InserirLivro grava l e devolve o livro com o ID gerado. Recusa, antes de
// qualquer SQL, livro com ID (ErrJaPersistido) e título vazio
// (domain.ErrTituloVazio); o título é revalidado e aparado por NovoLivro.
func (r *Repositorio) InserirLivro(ctx context.Context, l domain.Livro) (domain.Livro, error) {
	if l.ID != 0 {
		return domain.Livro{}, ErrJaPersistido
	}
	novo, err := domain.NovoLivro(l.Titulo)
	if err != nil {
		return domain.Livro{}, err
	}
	var got domain.Livro
	err = r.db.QueryRow(ctx,
		`INSERT INTO livro (titulo) VALUES ($1) RETURNING id, titulo`,
		novo.Titulo).Scan(&got.ID, &got.Titulo)
	if err != nil {
		return domain.Livro{}, traduzirErro(err)
	}
	return got, nil
}

// BuscarLivro lê o livro id; inexistente devolve ErrLivroNaoEncontrado.
func (r *Repositorio) BuscarLivro(ctx context.Context, id int64) (domain.Livro, error) {
	if id <= 0 {
		return domain.Livro{}, ErrLivroNaoEncontrado
	}
	var got domain.Livro
	err := r.db.QueryRow(ctx,
		`SELECT id, titulo FROM livro WHERE id = $1`,
		id).Scan(&got.ID, &got.Titulo)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Livro{}, ErrLivroNaoEncontrado
	}
	if err != nil {
		return domain.Livro{}, traduzirErro(err)
	}
	return got, nil
}

// InserirUsuario grava u e devolve o usuário com o ID gerado. Recusa, antes
// de qualquer SQL, usuário com ID (ErrJaPersistido) e nome vazio
// (domain.ErrNomeVazio); o nome é revalidado e aparado por NovoUsuario.
func (r *Repositorio) InserirUsuario(ctx context.Context, u domain.Usuario) (domain.Usuario, error) {
	if u.ID != 0 {
		return domain.Usuario{}, ErrJaPersistido
	}
	novo, err := domain.NovoUsuario(u.Nome)
	if err != nil {
		return domain.Usuario{}, err
	}
	var got domain.Usuario
	err = r.db.QueryRow(ctx,
		`INSERT INTO usuario (nome) VALUES ($1) RETURNING id, nome`,
		novo.Nome).Scan(&got.ID, &got.Nome)
	if err != nil {
		return domain.Usuario{}, traduzirErro(err)
	}
	return got, nil
}

// BuscarUsuario lê o usuário id; inexistente devolve ErrUsuarioNaoEncontrado.
func (r *Repositorio) BuscarUsuario(ctx context.Context, id int64) (domain.Usuario, error) {
	if id <= 0 {
		return domain.Usuario{}, ErrUsuarioNaoEncontrado
	}
	var got domain.Usuario
	err := r.db.QueryRow(ctx,
		`SELECT id, nome FROM usuario WHERE id = $1`,
		id).Scan(&got.ID, &got.Nome)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Usuario{}, ErrUsuarioNaoEncontrado
	}
	if err != nil {
		return domain.Usuario{}, traduzirErro(err)
	}
	return got, nil
}

const colunasEmprestimo = `id, livro_id, usuario_id, inicio, vencimento, devolucao`

// AbrirEmprestimo grava um empréstimo novo (de NovoEmprestimo: sem ID e
// aberto) e devolve o empréstimo reidratado com o ID gerado. Empréstimo
// inválido, já persistido ou devolvido é recusado com
// domain.ErrEmprestimoInvalido antes de qualquer SQL. R4 violada devolve
// ErrLivroJaEmprestado; livro ou usuário inexistente, ErrLivroNaoEncontrado
// ou ErrUsuarioNaoEncontrado.
func (r *Repositorio) AbrirEmprestimo(ctx context.Context, e domain.Emprestimo) (domain.Emprestimo, error) {
	if e.ID() != 0 || !e.Aberto() || !valido(e) {
		return domain.Emprestimo{}, domain.ErrEmprestimoInvalido
	}
	row := r.db.QueryRow(ctx,
		`INSERT INTO emprestimo (livro_id, usuario_id, inicio, vencimento)
		 VALUES ($1, $2, $3, $4)
		 RETURNING `+colunasEmprestimo,
		e.LivroID(), e.UsuarioID(), e.Inicio(), vencimentoParaDate(e.Vencimento()))
	return lerEmprestimo(row)
}

// RegistrarDevolucao grava a devolução de e (persistido e já devolvido em
// memória por Devolver) e devolve o empréstimo relido. O UPDATE fica preso ao
// estado lido: grava só se a linha com o ID ainda estiver aberta e tiver o
// mesmo início de e. Assim a regra "devolução não antes do início", que
// Devolver checou contra e.Inicio(), vale contra o início real gravado.
//
// Sem linha atualizada, o diagnóstico devolve ErrEmprestimoNaoEncontrado se o
// ID não existir, domain.ErrEmprestimoJaDevolvido se a linha já estiver
// devolvida e ErrEmprestimoDivergente se estiver aberta com outro início.
// Empréstimo sem ID, inválido ou aberto é recusado com
// domain.ErrEmprestimoInvalido antes de qualquer SQL.
//
// O início de um empréstimo com ID vem do banco (timestamptz, precisão de
// µs), e o pgx o codifica truncando para µs: a igualdade em inicio = $3 é
// exata para valores lidos pelo repositório.
func (r *Repositorio) RegistrarDevolucao(ctx context.Context, e domain.Emprestimo) (domain.Emprestimo, error) {
	if e.ID() <= 0 || e.Aberto() || !valido(e) {
		return domain.Emprestimo{}, domain.ErrEmprestimoInvalido
	}
	row := r.db.QueryRow(ctx,
		`UPDATE emprestimo SET devolucao = $2
		  WHERE id = $1 AND devolucao IS NULL AND inicio = $3
		 RETURNING `+colunasEmprestimo,
		e.ID(), e.Devolucao(), e.Inicio())
	got, err := lerEmprestimo(row)
	if !errors.Is(err, ErrEmprestimoNaoEncontrado) {
		return got, err
	}

	// Nenhuma linha atualizada: distingue inexistente, já devolvido e início
	// divergente.
	var devolvido, mesmoInicio bool
	err = r.db.QueryRow(ctx,
		`SELECT devolucao IS NOT NULL, inicio = $2 FROM emprestimo WHERE id = $1`,
		e.ID(), e.Inicio()).Scan(&devolvido, &mesmoInicio)
	switch {
	case errors.Is(err, pgx.ErrNoRows):
		return domain.Emprestimo{}, ErrEmprestimoNaoEncontrado
	case err != nil:
		return domain.Emprestimo{}, traduzirErro(err)
	case devolvido:
		return domain.Emprestimo{}, domain.ErrEmprestimoJaDevolvido
	case !mesmoInicio:
		return domain.Emprestimo{}, ErrEmprestimoDivergente
	}
	// Aberta e com o mesmo início: a linha mudou entre o UPDATE e esta
	// leitura (devolução desfeita por outra transação). Não grava às cegas.
	return domain.Emprestimo{}, ErrEmprestimoDivergente
}

// BuscarEmprestimoAbertoPorLivro lê o empréstimo aberto do livro; sem
// empréstimo aberto devolve ErrEmprestimoNaoEncontrado.
func (r *Repositorio) BuscarEmprestimoAbertoPorLivro(ctx context.Context, livroID int64) (domain.Emprestimo, error) {
	if livroID <= 0 {
		return domain.Emprestimo{}, ErrEmprestimoNaoEncontrado
	}
	row := r.db.QueryRow(ctx,
		`SELECT `+colunasEmprestimo+`
		   FROM emprestimo
		  WHERE livro_id = $1 AND devolucao IS NULL`,
		livroID)
	return lerEmprestimo(row)
}

// valido informa se e tem os campos que NovoEmprestimo e ReidratarEmprestimo
// garantem; recusa Emprestimo{} e valores montados à mão.
func valido(e domain.Emprestimo) bool {
	return e.LivroID() > 0 && e.UsuarioID() > 0 && !e.Inicio().IsZero() && !e.Vencimento().IsZero()
}

// lerEmprestimo lê uma linha com colunasEmprestimo e a reidrata. Nenhuma
// linha devolve ErrEmprestimoNaoEncontrado.
func lerEmprestimo(row pgx.Row) (domain.Emprestimo, error) {
	var (
		id, livroID, usuarioID int64
		inicio, vencimento     time.Time
		devolucao              *time.Time
	)
	err := row.Scan(&id, &livroID, &usuarioID, &inicio, &vencimento, &devolucao)
	if errors.Is(err, pgx.ErrNoRows) {
		return domain.Emprestimo{}, ErrEmprestimoNaoEncontrado
	}
	if err != nil {
		return domain.Emprestimo{}, traduzirErro(err)
	}
	var dev time.Time
	if devolucao != nil {
		dev = *devolucao
	}
	e, err := domain.ReidratarEmprestimo(id, livroID, usuarioID, inicio, vencimento, dev)
	if err != nil {
		return domain.Emprestimo{}, fmt.Errorf("storage: empréstimo gravado inválido: %w", err)
	}
	return e, nil
}

// vencimentoParaDate converte o vencimento do domínio para o valor gravado
// na coluna DATE: a meia-noite UTC da data civil do vencimento em
// America/Sao_Paulo, qualquer que seja o fuso de v.
func vencimentoParaDate(v time.Time) time.Time {
	a, m, d := v.In(locSaoPaulo).Date()
	return time.Date(a, m, d, 0, 0, 0, 0, time.UTC)
}

// erroBanco embrulha um erro do PostgreSQL não mapeado. A mensagem traz só o
// SQLSTATE, nunca Message, Detail ou SQL (que podem conter dados).
//
// O *pgconn.PgError original fica na cadeia de Unwrap só para diagnóstico
// (errors.As). Log e resposta só podem usar Code e ConstraintName; nunca
// Detail, Message, Where nem os demais campos de texto, que podem conter dado
// pessoal (por exemplo, o Nome do usuário num "Key (...)=(...)").
type erroBanco struct {
	codigo string
	err    error
}

func (e *erroBanco) Error() string { return "storage: erro do banco (SQLSTATE " + e.codigo + ")" }

func (e *erroBanco) Unwrap() error { return e.err }

// traduzirErro mapeia erros do banco para as sentinelas do pacote: 23505 em
// emprestimo_livro_aberto_uq vira ErrLivroJaEmprestado; 23503 nas FKs de
// livro e usuário vira ErrLivroNaoEncontrado e ErrUsuarioNaoEncontrado. O
// mapeamento é pelo ConstraintName, nunca pela mensagem. Outros erros voltam
// embrulhados, sem dados na mensagem.
func traduzirErro(err error) error {
	if err == nil {
		return nil
	}
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		return fmt.Errorf("storage: %w", err)
	}
	switch {
	case pe.Code == "23505" && pe.ConstraintName == constraintLivroAberto:
		return ErrLivroJaEmprestado
	case pe.Code == "23503" && pe.ConstraintName == constraintLivroFK:
		return ErrLivroNaoEncontrado
	case pe.Code == "23503" && pe.ConstraintName == constraintUsuarioFK:
		return ErrUsuarioNaoEncontrado
	}
	return &erroBanco{codigo: pe.Code, err: err}
}
