package httpapi_test

import (
	"net/http"
	"strings"
	"testing"
	"time"
)

// Renovação bem-sucedida: busca o aberto do livro, renova com o relógio
// injetado, grava e responde 200 com o DTO de empréstimo e o contador.
func TestContratoSaidaRenovacao(t *testing.T) {
	tests := []struct {
		name           string
		agora          time.Time
		vencAtual      time.Time
		renovAtual     int
		wantVencimento string
		wantRenovacoes string
		wantMulta      string
	}{
		{name: "primeira, antes do vencimento", agora: time.Date(2026, 3, 12, 9, 0, 0, 0, sp), vencAtual: time.Date(2026, 3, 24, 12, 0, 0, 0, sp), wantVencimento: "2026-04-07", wantRenovacoes: "1", wantMulta: "0.00"},
		{name: "primeira, no dia do vencimento 23:59 SP", agora: time.Date(2026, 3, 24, 23, 59, 0, 0, sp), vencAtual: time.Date(2026, 3, 24, 12, 0, 0, 0, sp), wantVencimento: "2026-04-07", wantRenovacoes: "1", wantMulta: "0.00"},
		{name: "primeira, dia seguinte em UTC mas 24/03 em SP", agora: time.Date(2026, 3, 25, 2, 30, 0, 0, time.UTC), vencAtual: time.Date(2026, 3, 24, 12, 0, 0, 0, sp), wantVencimento: "2026-04-07", wantRenovacoes: "1", wantMulta: "0.00"},
		{name: "segunda, parte do vencimento atual", agora: time.Date(2026, 4, 1, 9, 0, 0, 0, sp), vencAtual: time.Date(2026, 4, 7, 12, 0, 0, 0, sp), renovAtual: 1, wantVencimento: "2026-04-21", wantRenovacoes: "2", wantMulta: "0.00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			f.agora = tt.agora
			f.repo.buscarRet = emprestimoRenovado(t, 77, 1, 2, inicioPadrao(), tt.vencAtual, tt.renovAtual)

			rec := f.do(http.MethodPost, "/livros/1/renovacao", "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			checarCabecalhos(t, rec, true)
			emprestimoJSONRenovacoes(t, rec.Body.Bytes(), "77", "1", "2", "2026-03-10T13:00:00Z", tt.wantVencimento, "", tt.wantMulta, tt.wantRenovacoes)

			if got := strings.Join(f.repo.chamadas, ","); got != "BuscarEmprestimoAbertoPorLivro,RegistrarRenovacao" {
				t.Errorf("chamadas = %s, want BuscarEmprestimoAbertoPorLivro,RegistrarRenovacao", got)
			}
			if f.repo.buscarRecebido != 1 {
				t.Errorf("BuscarEmprestimoAbertoPorLivro(%d), want 1", f.repo.buscarRecebido)
			}
			e := f.repo.renovarRecebido
			if e.ID() != 77 || !e.Aberto() || e.Renovacoes() != tt.renovAtual+1 {
				t.Errorf("RegistrarRenovacao recebeu id=%d aberto=%v renovacoes=%d; want 77, aberto, %d",
					e.ID(), e.Aberto(), e.Renovacoes(), tt.renovAtual+1)
			}
			if got := e.Vencimento().In(sp).Format("2006-01-02 15:04"); got != tt.wantVencimento+" 12:00" {
				t.Errorf("RegistrarRenovacao recebeu vencimento %s, want %s 12:00 SP", got, tt.wantVencimento)
			}
		})
	}
}

// A resposta usa o valor devolvido pelo repositório, não o renovado em
// memória, e a multa é calculada com o relógio injetado.
func TestContratoSaidaRenovacaoUsaRetornoDoRepositorio(t *testing.T) {
	f := novaFixture(t)
	f.agora = time.Date(2026, 3, 20, 9, 0, 0, 0, sp)
	ret := emprestimoRenovado(t, 77, 1, 2, inicioPadrao(), time.Date(2026, 3, 18, 12, 0, 0, 0, sp), 1)
	f.repo.renovarRet = &ret

	rec := f.do(http.MethodPost, "/livros/1/renovacao", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
	}
	emprestimoJSONRenovacoes(t, rec.Body.Bytes(), "77", "1", "2", "2026-03-10T13:00:00Z", "2026-03-18", "", "2.00", "1")
}

// Recusas do domínio não chegam a RegistrarRenovacao.
func TestRenovacaoRecusadaNaoGrava(t *testing.T) {
	tests := []struct {
		name   string
		agora  time.Time
		renov  int
		dev    bool
		status int
		codigo string
	}{
		{name: "vencido: 00:00 SP do dia seguinte", agora: time.Date(2026, 3, 25, 0, 0, 0, 0, sp), status: 409, codigo: "emprestimo_vencido"},
		{name: "vencido: 25/03 03:00 UTC", agora: time.Date(2026, 3, 25, 3, 0, 0, 0, time.UTC), status: 409, codigo: "emprestimo_vencido"},
		{name: "vencido ganha do limite", agora: time.Date(2026, 3, 30, 9, 0, 0, 0, sp), renov: 2, status: 409, codigo: "emprestimo_vencido"},
		{name: "limite atingido", agora: time.Date(2026, 3, 12, 9, 0, 0, 0, sp), renov: 2, status: 409, codigo: "limite_renovacoes_atingido"},
		{name: "ja devolvido", agora: time.Date(2026, 3, 12, 9, 0, 0, 0, sp), dev: true, status: 409, codigo: "emprestimo_ja_devolvido"},
		{name: "relogio zerado", agora: time.Time{}, status: 500, codigo: "erro_interno"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			f.agora = tt.agora
			if tt.dev {
				f.repo.buscarRet = emprestimo(t, 77, 1, 2, inicioPadrao(), inicioPadrao().Add(time.Hour))
			} else {
				f.repo.buscarRet = emprestimoRenovado(t, 77, 1, 2, inicioPadrao(), time.Date(2026, 3, 24, 12, 0, 0, 0, sp), tt.renov)
			}
			rec := f.do(http.MethodPost, "/livros/1/renovacao", "", true)
			checarErro(t, rec, tt.status, tt.codigo, true)
			semTextoInterno(t, "resposta", rec.Body.String())
			semTextoInterno(t, "log", f.logs.String())
			if got := strings.Join(f.repo.chamadas, ","); got != "BuscarEmprestimoAbertoPorLivro" {
				t.Errorf("chamadas = %s, want só BuscarEmprestimoAbertoPorLivro", got)
			}
		})
	}
}

// Mensagens fixas dos códigos novos.
func TestRenovacaoMensagens(t *testing.T) {
	tests := []struct {
		name   string
		agora  time.Time
		renov  int
		codigo string
		msg    string
	}{
		{name: "vencido", agora: time.Date(2026, 4, 1, 9, 0, 0, 0, sp), codigo: "emprestimo_vencido", msg: "empréstimo vencido"},
		{name: "limite", agora: time.Date(2026, 3, 12, 9, 0, 0, 0, sp), renov: 2, codigo: "limite_renovacoes_atingido", msg: "limite de renovações atingido"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			f.agora = tt.agora
			f.repo.buscarRet = emprestimoRenovado(t, 77, 1, 2, inicioPadrao(), time.Date(2026, 3, 24, 12, 0, 0, 0, sp), tt.renov)
			rec := f.do(http.MethodPost, "/livros/1/renovacao", "", true)
			if msg := checarErro(t, rec, http.StatusConflict, tt.codigo, true); msg != tt.msg {
				t.Errorf("mensagem = %q, want %q", msg, tt.msg)
			}
		})
	}
}

// O campo renovacoes aparece em todas as respostas de empréstimo.
func TestRenovacoesNasRespostasDeEmprestimo(t *testing.T) {
	t.Run("GET emprestimo renovado", func(t *testing.T) {
		f := novaFixture(t)
		f.agora = time.Date(2026, 4, 9, 9, 0, 0, 0, sp)
		f.repo.buscarRet = emprestimoRenovado(t, 77, 1, 2, inicioPadrao(), time.Date(2026, 4, 7, 12, 0, 0, 0, sp), 1)
		rec := f.do(http.MethodGet, "/livros/1/emprestimo", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
		}
		emprestimoJSONRenovacoes(t, rec.Body.Bytes(), "77", "1", "2", "2026-03-10T13:00:00Z", "2026-04-07", "", "2.00", "1")
	})
	t.Run("devolucao de renovado", func(t *testing.T) {
		f := novaFixture(t)
		f.agora = time.Date(2026, 4, 22, 9, 0, 0, 0, sp)
		f.repo.buscarRet = emprestimoRenovado(t, 77, 1, 2, inicioPadrao(), time.Date(2026, 4, 21, 12, 0, 0, 0, sp), 2)
		rec := f.do(http.MethodPost, "/livros/1/devolucao", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
		}
		emprestimoJSONRenovacoes(t, rec.Body.Bytes(), "77", "1", "2", "2026-03-10T13:00:00Z", "2026-04-21", "2026-04-22T12:00:00Z", "1.00", "2")
	})
}
