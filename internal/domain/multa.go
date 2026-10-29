// Package domain contém as regras de negócio puras, sem I/O.
package domain

import (
	"errors"
	"time"
	_ "time/tzdata" // embute a base de fusos: o cálculo não depende do sistema.

	"github.com/shopspring/decimal"
)

// ErrVencimentoZero indica que a data de vencimento não foi informada.
var ErrVencimentoZero = errors.New("multa: data de vencimento zero")

// ErrDevolucaoZero indica que a data de devolução não foi informada.
var ErrDevolucaoZero = errors.New("multa: data de devolução zero")

var (
	valorPorDia = decimal.NewFromInt(1)
	tetoMulta   = decimal.NewFromInt(20)
	locSaoPaulo = mustLoadLocation("America/Sao_Paulo")
)

func mustLoadLocation(nome string) *time.Location {
	loc, err := time.LoadLocation(nome)
	if err != nil {
		panic("domain: carregar fuso " + nome + ": " + err.Error())
	}
	return loc
}

// CalcularMulta devolve a multa por atraso: R$ 1,00 por dia civil de atraso
// (contado em America/Sao_Paulo, qualquer que seja o fuso das entradas),
// limitada a R$ 20,00. Devolução no dia do vencimento ou antes não gera multa.
func CalcularMulta(vencimento, devolucao time.Time) (decimal.Decimal, error) {
	if vencimento.IsZero() {
		return decimal.Zero, ErrVencimentoZero
	}
	if devolucao.IsZero() {
		return decimal.Zero, ErrDevolucaoZero
	}

	dias := (meiaNoiteUTC(devolucao) - meiaNoiteUTC(vencimento)) / 86400
	if dias <= 0 {
		return decimal.Zero, nil
	}
	return decimal.Min(valorPorDia.Mul(decimal.NewFromInt(dias)), tetoMulta), nil
}

// meiaNoiteUTC devolve, em segundos Unix, a meia-noite UTC da data civil de t
// em São Paulo. Usar UTC evita dias de 23h ou 25h no horário de verão.
func meiaNoiteUTC(t time.Time) int64 {
	a, m, d := t.In(locSaoPaulo).Date()
	return time.Date(a, m, d, 0, 0, 0, 0, time.UTC).Unix()
}
