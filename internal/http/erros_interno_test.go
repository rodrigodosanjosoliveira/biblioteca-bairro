package httpapi

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http/middleware"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/storage"
)

// Códigos da DEL-013 no catálogo, com status e mensagem fixos, e os códigos
// existentes intactos. classificar continua mapeando
// storage.ErrEmprestimoNaoEncontrado para emprestimo_aberto_nao_encontrado (o
// código novo é decidido só no handler do detalhe por id), e
// storage.ErrListagemInvalida é bug interno.
func TestCatalogoListagemEDetalhe(t *testing.T) {
	t.Run("catalogo", func(t *testing.T) {
		tests := []struct {
			codigo   string
			status   int
			mensagem string
		}{
			{codigo: "parametro_invalido", status: 400, mensagem: "parâmetro de consulta inválido"},
			{codigo: "emprestimo_nao_encontrado", status: 404, mensagem: "empréstimo não encontrado"},
			{codigo: "emprestimo_aberto_nao_encontrado", status: 404, mensagem: "nenhum empréstimo aberto para o livro"},
			{codigo: "livro_nao_encontrado", status: 404, mensagem: "livro não encontrado"},
			{codigo: "id_invalido", status: 400, mensagem: "id inválido"},
		}
		for _, tt := range tests {
			t.Run(tt.codigo, func(t *testing.T) {
				e, ok := catalogo[tt.codigo]
				if !ok {
					t.Fatalf("código %q fora do catálogo", tt.codigo)
				}
				if e.status != tt.status || e.mensagem != tt.mensagem {
					t.Errorf("catálogo[%q] = %d %q, want %d %q", tt.codigo, e.status, e.mensagem, tt.status, tt.mensagem)
				}
			})
		}
	})

	tests := []struct {
		name      string
		err       error
		wantCod   string
		wantCausa string
	}{
		{name: "ErrEmprestimoNaoEncontrado continua aberto_nao_encontrado", err: storage.ErrEmprestimoNaoEncontrado, wantCod: "emprestimo_aberto_nao_encontrado"},
		{name: "ErrEmprestimoNaoEncontrado embrulhado", err: fmt.Errorf("x: %w", storage.ErrEmprestimoNaoEncontrado), wantCod: "emprestimo_aberto_nao_encontrado"},
		{name: "ErrListagemInvalida é bug", err: storage.ErrListagemInvalida, wantCod: codErroInterno, wantCausa: causaBugInterno},
		{name: "ErrListagemInvalida embrulhado é bug", err: fmt.Errorf("x: %w", storage.ErrListagemInvalida), wantCod: codErroInterno, wantCausa: causaBugInterno},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cod, attrs := classificar(tt.err)
			if cod != tt.wantCod {
				t.Fatalf("classificar = %q, want %q", cod, tt.wantCod)
			}
			causa := ""
			for _, a := range attrs {
				if a.Key == "causa" {
					causa = a.Value.String()
				}
			}
			if causa != tt.wantCausa {
				t.Errorf("causa = %q, want %q", causa, tt.wantCausa)
			}
		})
	}
}

// TestFalharCodigoForaDoCatalogo: código fora do catálogo vira erro_interno
// (500), nunca status 0, e o log registra erro_interno.
func TestFalharCodigoForaDoCatalogo(t *testing.T) {
	tests := []struct {
		name      string
		codigo    string
		status    int
		wantCod   string
		wantMsg   string
		wantNivel string
	}{
		{name: "código inexistente", codigo: "codigo_que_nao_existe", status: 500, wantCod: codErroInterno, wantMsg: "erro interno", wantNivel: "ERROR"},
		{name: "código vazio", codigo: "", status: 500, wantCod: codErroInterno, wantMsg: "erro interno", wantNivel: "ERROR"},
		{name: "maiúsculas não casam", codigo: "CORPO_INVALIDO", status: 500, wantCod: codErroInterno, wantMsg: "erro interno", wantNivel: "ERROR"},
		{name: "código do catálogo inalterado", codigo: codCorpoInvalido, status: 400, wantCod: codCorpoInvalido, wantMsg: "corpo da requisição inválido", wantNivel: "INFO"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var logs bytes.Buffer
			h := middleware.Log(slog.New(slog.NewJSONHandler(&logs, nil)))(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				falhar(w, r, tt.codigo)
			}))
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			var corpo struct {
				Erro struct {
					Codigo   string `json:"codigo"`
					Mensagem string `json:"mensagem"`
				} `json:"erro"`
			}
			if err := json.Unmarshal(rec.Body.Bytes(), &corpo); err != nil {
				t.Fatalf("corpo não é JSON: %q", rec.Body.String())
			}
			if corpo.Erro.Codigo != tt.wantCod || corpo.Erro.Mensagem != tt.wantMsg {
				t.Errorf("erro = %+v, want codigo %q mensagem %q", corpo.Erro, tt.wantCod, tt.wantMsg)
			}
			var linha map[string]any
			if err := json.Unmarshal(logs.Bytes(), &linha); err != nil {
				t.Fatalf("log não é JSON: %q", logs.String())
			}
			if linha["codigo"] != tt.wantCod || linha["level"] != tt.wantNivel {
				t.Errorf("log codigo = %v nível = %v, want %q %q", linha["codigo"], linha["level"], tt.wantCod, tt.wantNivel)
			}
		})
	}
}

// Códigos da renovação: 409 com mensagem fixa, e o mapeamento das sentinelas
// do domínio. ErrRenovacoesInvalidas é dado gravado inválido: 500 bug_interno.
func TestCatalogoRenovacao(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		wantCod   string
		wantStat  int
		wantMsg   string
		wantCausa string
	}{
		{name: "vencido", err: domain.ErrEmprestimoVencido, wantCod: "emprestimo_vencido", wantStat: 409, wantMsg: "empréstimo vencido"},
		{name: "vencido embrulhado", err: fmt.Errorf("x: %w", domain.ErrEmprestimoVencido), wantCod: "emprestimo_vencido", wantStat: 409, wantMsg: "empréstimo vencido"},
		{name: "limite", err: domain.ErrLimiteRenovacoes, wantCod: "limite_renovacoes_atingido", wantStat: 409, wantMsg: "limite de renovações atingido"},
		{name: "renovacoes invalidas", err: fmt.Errorf("storage: empréstimo gravado inválido: %w", domain.ErrRenovacoesInvalidas), wantCod: codErroInterno, wantStat: 500, wantMsg: "erro interno", wantCausa: causaBugInterno},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cod, attrs := classificar(tt.err)
			if cod != tt.wantCod {
				t.Fatalf("classificar = %q, want %q", cod, tt.wantCod)
			}
			e, ok := catalogo[cod]
			if !ok {
				t.Fatalf("código %q fora do catálogo", cod)
			}
			if e.status != tt.wantStat || e.mensagem != tt.wantMsg {
				t.Errorf("catálogo[%q] = %d %q, want %d %q", cod, e.status, e.mensagem, tt.wantStat, tt.wantMsg)
			}
			causa := ""
			for _, a := range attrs {
				if a.Key == "causa" {
					causa = a.Value.String()
				}
			}
			if causa != tt.wantCausa {
				t.Errorf("causa = %q, want %q", causa, tt.wantCausa)
			}
		})
	}
}
