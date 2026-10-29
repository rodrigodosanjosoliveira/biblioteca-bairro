package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net/http"

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http/middleware"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/storage"
)

// Códigos de erro da API.
const (
	codCorpoInvalido        = "corpo_invalido"
	codCorpoMuitoGrande     = "corpo_muito_grande"
	codIDInvalido           = "id_invalido"
	codTituloVazio          = "titulo_vazio"
	codNomeVazio            = "nome_vazio"
	codTextoInvalido        = "texto_invalido"
	codUsuarioIDInvalido    = "usuario_id_invalido"
	codUsuarioNaoEncontrado = "usuario_nao_encontrado"
	codLivroNaoEncontrado   = "livro_nao_encontrado"
	codSemEmprestimoAberto  = "emprestimo_aberto_nao_encontrado"
	codLivroJaEmprestado    = "livro_ja_emprestado"
	codJaDevolvido          = "emprestimo_ja_devolvido"
	codEmprestimoAlterado   = "emprestimo_alterado"
	codIndisponivel         = "indisponivel"
	codErroInterno          = "erro_interno"
)

// Valores de "causa" no log de um 500.
const (
	causaRelogioDivergente = "relogio_divergente"
	causaBugInterno        = "bug_interno"
	causaBanco             = "banco"
	causaDesconhecido      = "desconhecido"
)

// erroAPI é o status e a mensagem fixa de cada código.
type erroAPI struct {
	status   int
	mensagem string
}

// catalogo tem uma mensagem fixa por código: a resposta nunca ecoa entrada nem
// texto de erro interno.
var catalogo = map[string]erroAPI{
	codCorpoInvalido:        {http.StatusBadRequest, "corpo da requisição inválido"},
	codCorpoMuitoGrande:     {http.StatusRequestEntityTooLarge, "corpo da requisição grande demais"},
	codIDInvalido:           {http.StatusBadRequest, "id inválido"},
	codTituloVazio:          {http.StatusUnprocessableEntity, "título vazio"},
	codNomeVazio:            {http.StatusUnprocessableEntity, "nome vazio"},
	codTextoInvalido:        {http.StatusUnprocessableEntity, "texto com caractere inválido"},
	codUsuarioIDInvalido:    {http.StatusUnprocessableEntity, "usuario_id inválido"},
	codUsuarioNaoEncontrado: {http.StatusUnprocessableEntity, "usuário não encontrado"},
	codLivroNaoEncontrado:   {http.StatusNotFound, "livro não encontrado"},
	codSemEmprestimoAberto:  {http.StatusNotFound, "nenhum empréstimo aberto para o livro"},
	codLivroJaEmprestado:    {http.StatusConflict, "livro já emprestado"},
	codJaDevolvido:          {http.StatusConflict, "empréstimo já devolvido"},
	codEmprestimoAlterado:   {http.StatusConflict, "empréstimo alterado por outra operação"},
	codIndisponivel:         {http.StatusServiceUnavailable, "serviço indisponível"},
	codErroInterno:          {http.StatusInternalServerError, "erro interno"},
}

// falhar responde o código com o status e a mensagem do catálogo. Código fora
// do catálogo é bug: responde erro_interno (500) em vez de status 0.
func falhar(w http.ResponseWriter, r *http.Request, codigo string, attrs ...slog.Attr) {
	e, ok := catalogo[codigo]
	if !ok {
		codigo, e = codErroInterno, catalogo[codErroInterno]
	}
	middleware.ResponderErro(w, r, e.status, codigo, e.mensagem, attrs...)
}

// falharErro mapeia err para o código da API e responde. Em 500, anota a
// causa e, se houver *pgconn.PgError na cadeia, só o Code e o ConstraintName.
func falharErro(w http.ResponseWriter, r *http.Request, err error) {
	codigo, attrs := classificar(err)
	falhar(w, r, codigo, attrs...)
}

// classificar devolve o código da API para err e os atributos extras do log.
func classificar(err error) (string, []slog.Attr) {
	switch {
	case errors.Is(err, context.DeadlineExceeded), errors.Is(err, context.Canceled):
		return codIndisponivel, nil
	case errors.Is(err, domain.ErrTituloVazio):
		return codTituloVazio, nil
	case errors.Is(err, domain.ErrNomeVazio):
		return codNomeVazio, nil
	case errors.Is(err, domain.ErrUsuarioIDInvalido):
		return codUsuarioIDInvalido, nil
	case errors.Is(err, storage.ErrUsuarioNaoEncontrado):
		return codUsuarioNaoEncontrado, nil
	case errors.Is(err, storage.ErrLivroNaoEncontrado):
		return codLivroNaoEncontrado, nil
	case errors.Is(err, storage.ErrEmprestimoNaoEncontrado):
		return codSemEmprestimoAberto, nil
	case errors.Is(err, storage.ErrLivroJaEmprestado):
		return codLivroJaEmprestado, nil
	case errors.Is(err, domain.ErrEmprestimoJaDevolvido):
		return codJaDevolvido, nil
	case errors.Is(err, storage.ErrEmprestimoDivergente):
		return codEmprestimoAlterado, nil
	}
	return codErroInterno, causaInterna(err)
}

// errosDeBug são erros que só aparecem se o código chamar o domínio ou o
// repositório de forma errada (ou se o relógio devolver zero).
var errosDeBug = []error{
	storage.ErrJaPersistido,
	domain.ErrEmprestimoInvalido,
	domain.ErrReferenciaZero,
	domain.ErrLivroIDInvalido,
	domain.ErrInicioZero,
	domain.ErrDevolucaoEmprestimoZero,
	domain.ErrEmprestimoIDInvalido,
	domain.ErrVencimentoEmprestimoZero,
	domain.ErrVencimentoNaoPosteriorAoInicio,
	domain.ErrVencimentoZero,
	domain.ErrDevolucaoZero,
}

// causaInterna monta os atributos de log de um 500.
func causaInterna(err error) []slog.Attr {
	if errors.Is(err, domain.ErrDevolucaoAntesDoInicio) {
		return []slog.Attr{slog.String("causa", causaRelogioDivergente)}
	}
	var pe *pgconn.PgError
	if errors.As(err, &pe) {
		return []slog.Attr{
			slog.String("causa", causaBanco),
			slog.String("pg_code", pe.Code),
			slog.String("pg_constraint", pe.ConstraintName),
		}
	}
	for _, b := range errosDeBug {
		if errors.Is(err, b) {
			return []slog.Attr{slog.String("causa", causaBugInterno)}
		}
	}
	return []slog.Attr{slog.String("causa", causaDesconhecido)}
}
