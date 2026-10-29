package httpapi

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http/middleware"
)

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
