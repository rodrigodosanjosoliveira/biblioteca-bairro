package domain

import (
	"errors"
	"time"
)

// PrazoEmprestimoDias é o prazo do empréstimo, em dias civis de São Paulo.
const PrazoEmprestimoDias = 14

var (
	// ErrLivroIDInvalido indica id de livro menor ou igual a zero.
	ErrLivroIDInvalido = errors.New("emprestimo: id do livro inválido")
	// ErrUsuarioIDInvalido indica id de usuário menor ou igual a zero.
	ErrUsuarioIDInvalido = errors.New("emprestimo: id do usuário inválido")
	// ErrInicioZero indica que a data de início não foi informada.
	ErrInicioZero = errors.New("emprestimo: data de início zero")
	// ErrEmprestimoJaDevolvido indica tentativa de devolver um empréstimo fechado.
	ErrEmprestimoJaDevolvido = errors.New("emprestimo: já devolvido")
	// ErrDevolucaoEmprestimoZero indica que a data de devolução não foi informada.
	ErrDevolucaoEmprestimoZero = errors.New("emprestimo: data de devolução zero")
	// ErrDevolucaoAntesDoInicio indica devolução em dia civil (SP) anterior ao início.
	ErrDevolucaoAntesDoInicio = errors.New("emprestimo: devolução antes do início")
)

// Emprestimo é o empréstimo de um livro a um usuário. Os campos não são
// exportados: só NovoEmprestimo (e, futuramente, ReidratarEmprestimo) produz
// valores válidos. O valor zero Emprestimo{} não é um empréstimo válido.
type Emprestimo struct {
	id         int64
	livroID    int64
	usuarioID  int64
	inicio     time.Time
	vencimento time.Time
	devolucao  time.Time
}

// NovoEmprestimo abre um empréstimo. Valida, nesta ordem, livroID > 0,
// usuarioID > 0 e início não zero; em erro devolve Emprestimo{}. O início é
// guardado como recebido. O vencimento é o meio-dia em America/Sao_Paulo da
// data civil (SP) do início somada de PrazoEmprestimoDias dias.
func NovoEmprestimo(livroID, usuarioID int64, inicio time.Time) (Emprestimo, error) {
	if livroID <= 0 {
		return Emprestimo{}, ErrLivroIDInvalido
	}
	if usuarioID <= 0 {
		return Emprestimo{}, ErrUsuarioIDInvalido
	}
	if inicio.IsZero() {
		return Emprestimo{}, ErrInicioZero
	}

	a, m, d := inicio.In(locSaoPaulo).Date()
	vencimento := time.Date(a, m, d+PrazoEmprestimoDias, 12, 0, 0, 0, locSaoPaulo)

	return Emprestimo{
		livroID:    livroID,
		usuarioID:  usuarioID,
		inicio:     inicio,
		vencimento: vencimento,
	}, nil
}

// Devolver registra a devolução em em. Valida, nesta ordem: empréstimo
// aberto, data não zero e dia civil (SP) não anterior ao do início. Em erro,
// o empréstimo não é alterado. A data é guardada como recebida.
func (e *Emprestimo) Devolver(em time.Time) error {
	if !e.Aberto() {
		return ErrEmprestimoJaDevolvido
	}
	if em.IsZero() {
		return ErrDevolucaoEmprestimoZero
	}
	if meiaNoiteUTC(em) < meiaNoiteUTC(e.inicio) {
		return ErrDevolucaoAntesDoInicio
	}
	e.devolucao = em
	return nil
}

// Aberto informa se o empréstimo ainda não foi devolvido.
func (e Emprestimo) Aberto() bool { return e.devolucao.IsZero() }

// ID devolve o identificador; 0 significa ainda não persistido.
func (e Emprestimo) ID() int64 { return e.id }

// LivroID devolve o id do livro emprestado.
func (e Emprestimo) LivroID() int64 { return e.livroID }

// UsuarioID devolve o id do usuário.
func (e Emprestimo) UsuarioID() int64 { return e.usuarioID }

// Inicio devolve a data de início, como recebida.
func (e Emprestimo) Inicio() time.Time { return e.inicio }

// Vencimento devolve o vencimento (meio-dia em São Paulo).
func (e Emprestimo) Vencimento() time.Time { return e.vencimento }

// Devolucao devolve a data de devolução; zero enquanto aberto.
func (e Emprestimo) Devolucao() time.Time { return e.devolucao }
