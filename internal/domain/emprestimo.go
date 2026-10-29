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
	// ErrEmprestimoIDInvalido indica id de empréstimo menor ou igual a zero.
	ErrEmprestimoIDInvalido = errors.New("emprestimo: id inválido")
	// ErrVencimentoEmprestimoZero indica que a data de vencimento não foi informada.
	ErrVencimentoEmprestimoZero = errors.New("emprestimo: data de vencimento zero")
	// ErrVencimentoNaoPosteriorAoInicio indica vencimento em dia civil (SP)
	// igual ou anterior ao do início.
	ErrVencimentoNaoPosteriorAoInicio = errors.New("emprestimo: vencimento não posterior ao início")
	// ErrEmprestimoInvalido indica operação sobre um empréstimo que não veio de
	// NovoEmprestimo nem de ReidratarEmprestimo (por exemplo, Emprestimo{}).
	ErrEmprestimoInvalido = errors.New("emprestimo: inválido")
)

// Emprestimo é o empréstimo de um livro a um usuário. Os campos não são
// exportados: só NovoEmprestimo e ReidratarEmprestimo produzem
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

// ReidratarEmprestimo reconstrói um empréstimo já persistido. Valida, nesta
// ordem: id > 0, livroID > 0, usuarioID > 0, início não zero, vencimento não
// zero, data civil (SP) do vencimento posterior à do início e, se a devolução
// não for zero, data civil (SP) da devolução não anterior à do início. Em
// erro devolve Emprestimo{}. Não exige vencimento igual a início + 14 dias: o
// prazo vale na abertura, não na leitura.
//
// Normalização do vencimento: a data civil é lida no fuso do próprio valor
// (vencimento.Date(), sem converter para São Paulo) e o vencimento passa a ser
// o meio-dia em America/Sao_Paulo dessa data. Assim, tanto a meia-noite UTC
// entregue pelo pgx para uma coluna DATE quanto o 12:00 SP do domínio
// resultam no mesmo valor, e reidratar de novo não o altera. Início e
// devolução são guardados como recebidos.
func ReidratarEmprestimo(id, livroID, usuarioID int64, inicio, vencimento, devolucao time.Time) (Emprestimo, error) {
	if id <= 0 {
		return Emprestimo{}, ErrEmprestimoIDInvalido
	}
	if livroID <= 0 {
		return Emprestimo{}, ErrLivroIDInvalido
	}
	if usuarioID <= 0 {
		return Emprestimo{}, ErrUsuarioIDInvalido
	}
	if inicio.IsZero() {
		return Emprestimo{}, ErrInicioZero
	}
	if vencimento.IsZero() {
		return Emprestimo{}, ErrVencimentoEmprestimoZero
	}

	a, m, d := vencimento.Date()
	venc := time.Date(a, m, d, 12, 0, 0, 0, locSaoPaulo)

	if meiaNoiteUTC(venc) <= meiaNoiteUTC(inicio) {
		return Emprestimo{}, ErrVencimentoNaoPosteriorAoInicio
	}
	if !devolucao.IsZero() && meiaNoiteUTC(devolucao) < meiaNoiteUTC(inicio) {
		return Emprestimo{}, ErrDevolucaoAntesDoInicio
	}

	return Emprestimo{
		id:         id,
		livroID:    livroID,
		usuarioID:  usuarioID,
		inicio:     inicio,
		vencimento: venc,
		devolucao:  devolucao,
	}, nil
}

// Devolver registra a devolução em em. Valida, nesta ordem: empréstimo válido
// (início não zero, o que recusa Emprestimo{}), empréstimo aberto, data não
// zero e dia civil (SP) não anterior ao do início. Em erro, o empréstimo não
// é alterado. A data é guardada como recebida.
func (e *Emprestimo) Devolver(em time.Time) error {
	if e.inicio.IsZero() {
		return ErrEmprestimoInvalido
	}
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
