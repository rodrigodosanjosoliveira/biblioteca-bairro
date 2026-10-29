package middleware_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"testing"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http/middleware"
)

const tokenTeste = "TOKEN-SECRETO-0123456789abcdefghijklmn" // 38 bytes

var hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)

// Assinaturas exigidas do pacote.
var (
	_ func(http.Handler) http.Handler                    = middleware.RequestID
	_ func(*slog.Logger) func(http.Handler) http.Handler = middleware.Log
	_ func(http.Handler) http.Handler                    = middleware.Recover
	_ func(int64) func(http.Handler) http.Handler        = middleware.LimitarCorpo
	_ func(string) func(http.Handler) http.Handler       = middleware.Auth
)

// erroJSON decodifica {"erro":{"codigo","mensagem"}} exigindo exatamente
// essas chaves.
func erroJSON(t *testing.T, body []byte) (codigo, mensagem string) {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("corpo não é JSON: %q", body)
	}
	if len(top) != 1 || top["erro"] == nil {
		t.Fatalf("corpo = %s, want só a chave \"erro\"", body)
	}
	var e map[string]string
	if err := json.Unmarshal(top["erro"], &e); err != nil {
		t.Fatalf("erro não é objeto de strings: %s", top["erro"])
	}
	if len(e) != 2 || e["codigo"] == "" || e["mensagem"] == "" {
		t.Fatalf("erro = %s, want exatamente codigo e mensagem não vazios", top["erro"])
	}
	return e["codigo"], e["mensagem"]
}

func checarJSON(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
}

func TestRequestID(t *testing.T) {
	var visto string
	h := middleware.RequestID(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		visto = w.Header().Get("X-Request-ID")
		w.WriteHeader(http.StatusTeapot)
	}))

	ids := map[string]bool{}
	for i := 0; i < 50; i++ {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("X-Request-ID", "id-do-atacante")
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		id := rec.Header().Get("X-Request-ID")
		if !hex32.MatchString(id) {
			t.Fatalf("X-Request-ID = %q, want 32 hex minúsculos (16 bytes)", id)
		}
		if visto != id {
			t.Errorf("handler interno viu X-Request-ID %q, resposta tem %q", visto, id)
		}
		if ids[id] {
			t.Fatalf("X-Request-ID %q repetido", id)
		}
		ids[id] = true
	}
}

type caso struct {
	name string
	auth []string // valores do header Authorization; nil = sem header
}

func casosAuthInvalidos() []caso {
	mesmoTamanho := strings.Repeat("x", len(tokenTeste))
	return []caso{
		{name: "sem header"},
		{name: "header vazio", auth: []string{""}},
		{name: "basic", auth: []string{"Basic dXNlcjpwYXNz"}},
		{name: "basic com o token", auth: []string{"Basic " + tokenTeste}},
		{name: "token sem esquema", auth: []string{tokenTeste}},
		{name: "esquema sem token", auth: []string{"Bearer"}},
		{name: "esquema com token vazio", auth: []string{"Bearer "}},
		{name: "token errado", auth: []string{"Bearer outro-token-qualquer-com-tamanho-diferente"}},
		{name: "token errado de mesmo tamanho", auth: []string{"Bearer " + mesmoTamanho}},
		{name: "prefixo do token", auth: []string{"Bearer " + tokenTeste[:len(tokenTeste)-1]}},
		{name: "token com sufixo", auth: []string{"Bearer " + tokenTeste + "x"}},
		{name: "token com caixa trocada", auth: []string{"Bearer " + strings.ToLower(tokenTeste)}},
		{name: "esquema colado no token", auth: []string{"Bearer" + tokenTeste}},
		{name: "outro esquema", auth: []string{"Token " + tokenTeste}},
	}
}

func TestAuth(t *testing.T) {
	var chamou bool
	h := middleware.Auth(tokenTeste)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		chamou = true
		w.WriteHeader(http.StatusNoContent)
	}))
	do := func(auth []string) *httptest.ResponseRecorder {
		chamou = false
		req := httptest.NewRequest(http.MethodPost, "/livros", strings.NewReader(`{"titulo":"x"}`))
		for _, a := range auth {
			req.Header.Add("Authorization", a)
		}
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		return rec
	}

	t.Run("validos", func(t *testing.T) {
		for _, a := range []string{"Bearer " + tokenTeste, "bearer " + tokenTeste, "BEARER " + tokenTeste, "bEaReR " + tokenTeste} {
			rec := do([]string{a})
			if rec.Code != http.StatusNoContent || !chamou {
				t.Errorf("Authorization %q: status %d, chamou=%v; want 204 e handler chamado", strings.SplitN(a, " ", 2)[0], rec.Code, chamou)
			}
		}
	})

	var corpoRef []byte
	for _, tt := range casosAuthInvalidos() {
		t.Run(tt.name, func(t *testing.T) {
			rec := do(tt.auth)
			if rec.Code != http.StatusUnauthorized {
				t.Fatalf("status = %d, want 401", rec.Code)
			}
			if chamou {
				t.Error("handler protegido foi chamado")
			}
			if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
				t.Errorf("WWW-Authenticate = %q, want Bearer", got)
			}
			checarJSON(t, rec)
			if cod, _ := erroJSON(t, rec.Body.Bytes()); cod != "nao_autorizado" {
				t.Errorf("codigo = %q, want nao_autorizado", cod)
			}
			if strings.Contains(rec.Body.String(), tokenTeste[:10]) {
				t.Errorf("corpo ecoa o token: %s", rec.Body.String())
			}
			if corpoRef == nil {
				corpoRef = rec.Body.Bytes()
			} else if !bytes.Equal(rec.Body.Bytes(), corpoRef) {
				t.Errorf("401 diferente entre falhas: %s != %s", rec.Body.Bytes(), corpoRef)
			}
		})
	}
}

// Com token configurado vazio, nada passa (defesa contra config errada).
func TestAuthTokenConfiguradoVazio(t *testing.T) {
	h := middleware.Auth("")(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Error("handler protegido chamado com token configurado vazio")
	}))
	for _, a := range []string{"Bearer ", "Bearer", "", "bearer  "} {
		req := httptest.NewRequest(http.MethodGet, "/", nil)
		req.Header.Set("Authorization", a)
		rec := httptest.NewRecorder()
		h.ServeHTTP(rec, req)
		if rec.Code != http.StatusUnauthorized {
			t.Errorf("Authorization %q: status = %d, want 401", a, rec.Code)
		}
	}
}

func TestRecover(t *testing.T) {
	h := middleware.Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic("SEGREDO-PANIC")
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("status = %d, want 500", rec.Code)
	}
	checarJSON(t, rec)
	if cod, _ := erroJSON(t, rec.Body.Bytes()); cod != "erro_interno" {
		t.Errorf("codigo = %q, want erro_interno", cod)
	}
	if strings.Contains(rec.Body.String(), "SEGREDO-PANIC") {
		t.Errorf("corpo ecoa o valor do panic: %s", rec.Body.String())
	}
}

func TestRecoverErro(t *testing.T) {
	h := middleware.Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		panic(errors.New("SEGREDO-PANIC-ERR"))
	}))
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
	if rec.Code != http.StatusInternalServerError || strings.Contains(rec.Body.String(), "SEGREDO") {
		t.Errorf("status = %d, corpo = %s; want 500 sem o erro", rec.Code, rec.Body.String())
	}
}

func TestRecoverRepropagaErrAbortHandler(t *testing.T) {
	tests := []struct {
		name  string
		valor any
	}{
		{name: "ErrAbortHandler", valor: http.ErrAbortHandler},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			h := middleware.Recover(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				panic(tt.valor)
			}))
			rec := httptest.NewRecorder()
			defer func() {
				got := recover()
				if got != tt.valor {
					t.Fatalf("recover() = %v, want %v repropagado", got, tt.valor)
				}
				if rec.Body.Len() != 0 {
					t.Errorf("Recover escreveu resposta antes de repropagar: %q", rec.Body.String())
				}
			}()
			h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
		})
	}
}

func TestLimitarCorpo(t *testing.T) {
	tests := []struct {
		name       string
		limite     int64
		corpo      string
		wantMaxErr bool
	}{
		{name: "abaixo do limite", limite: 10, corpo: "123456789"},
		{name: "no limite", limite: 10, corpo: "1234567890"},
		{name: "um byte acima", limite: 10, corpo: "12345678901", wantMaxErr: true},
		{name: "vazio", limite: 10, corpo: ""},
		{name: "64 KiB exatos", limite: 64 << 10, corpo: strings.Repeat("a", 64<<10)},
		{name: "64 KiB + 1", limite: 64 << 10, corpo: strings.Repeat("a", 64<<10+1), wantMaxErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var errLido error
			var lido int
			h := middleware.LimitarCorpo(tt.limite)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				b, err := io.ReadAll(r.Body)
				lido, errLido = len(b), err
			}))
			h.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodPost, "/", strings.NewReader(tt.corpo)))
			var mbe *http.MaxBytesError
			if tt.wantMaxErr {
				if !errors.As(errLido, &mbe) {
					t.Fatalf("erro de leitura = %v, want *http.MaxBytesError", errLido)
				}
				if mbe.Limit != tt.limite {
					t.Errorf("Limit = %d, want %d", mbe.Limit, tt.limite)
				}
				return
			}
			if errLido != nil {
				t.Fatalf("erro de leitura inesperado: %v", errLido)
			}
			if lido != len(tt.corpo) {
				t.Errorf("lidos %d bytes, want %d", lido, len(tt.corpo))
			}
		})
	}
}

// linhas devolve as linhas JSON do buffer de log.
func linhas(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("linha de log não é JSON: %q", l)
		}
		out = append(out, m)
	}
	return out
}

func chaves(m map[string]any) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return out
}

func mesmasChaves(t *testing.T, linha map[string]any, want []string) {
	t.Helper()
	w := append([]string(nil), want...)
	sort.Strings(w)
	got := chaves(linha)
	if strings.Join(got, ",") != strings.Join(w, ",") {
		t.Errorf("chaves do log = %v, want %v", got, w)
	}
}

var chavesBase = []string{"time", "level", "msg", "metodo", "rota", "status", "duracao_ms", "request_id"}

// cadeia monta a cadeia de produção em volta do mux:
// RequestID → Log → Recover → LimitarCorpo(64 KiB) → mux.
func cadeia(logger *slog.Logger, mux http.Handler) http.Handler {
	return middleware.RequestID(middleware.Log(logger)(middleware.Recover(middleware.LimitarCorpo(64 << 10)(mux))))
}

func TestLog(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /x/{id}", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusTeapot)
	})
	mux.HandleFunc("GET /sem-writeheader", func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte("ok"))
	})
	mux.HandleFunc("GET /panico", func(w http.ResponseWriter, r *http.Request) {
		panic("SEGREDO-PANIC")
	})

	tests := []struct {
		name       string
		method     string
		path       string
		wantStatus int
		wantRota   string
		wantChaves []string
		extra      map[string]any
	}{
		{name: "rota com wildcard", method: http.MethodGet, path: "/x/123?token=QS-SECRETO", wantStatus: http.StatusTeapot, wantRota: "GET /x/{id}", wantChaves: chavesBase},
		{name: "status implícito 200", method: http.MethodGet, path: "/sem-writeheader", wantStatus: http.StatusOK, wantRota: "GET /sem-writeheader", wantChaves: chavesBase},
		{name: "404 do mux", method: http.MethodGet, path: "/nada", wantStatus: http.StatusNotFound, wantRota: "", wantChaves: chavesBase},
		{name: "405 do mux", method: http.MethodPost, path: "/x/1", wantStatus: http.StatusMethodNotAllowed, wantRota: "", wantChaves: chavesBase},
		{
			name: "panic", method: http.MethodGet, path: "/panico", wantStatus: http.StatusInternalServerError, wantRota: "GET /panico",
			wantChaves: append(append([]string(nil), chavesBase...), "codigo", "panic"),
			extra:      map[string]any{"codigo": "erro_interno", "panic": true},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			h := cadeia(slog.New(slog.NewJSONHandler(&buf, nil)), mux)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, httptest.NewRequest(tt.method, tt.path, nil))
			if rec.Code != tt.wantStatus {
				t.Fatalf("status = %d, want %d", rec.Code, tt.wantStatus)
			}
			ls := linhas(t, &buf)
			if len(ls) != 1 {
				t.Fatalf("%d linhas de log, want 1:\n%s", len(ls), buf.String())
			}
			l := ls[0]
			mesmasChaves(t, l, tt.wantChaves)
			if l["metodo"] != tt.method {
				t.Errorf("metodo = %v, want %s", l["metodo"], tt.method)
			}
			if l["rota"] != tt.wantRota {
				t.Errorf("rota = %v, want %q", l["rota"], tt.wantRota)
			}
			if st, ok := l["status"].(float64); !ok || int(st) != tt.wantStatus {
				t.Errorf("status no log = %v, want %d", l["status"], tt.wantStatus)
			}
			if d, ok := l["duracao_ms"].(float64); !ok || d < 0 {
				t.Errorf("duracao_ms = %v, want número >= 0", l["duracao_ms"])
			}
			if id := rec.Header().Get("X-Request-ID"); l["request_id"] != id || !hex32.MatchString(id) {
				t.Errorf("request_id = %v, X-Request-ID = %q; want iguais e 32 hex", l["request_id"], id)
			}
			for k, v := range tt.extra {
				if l[k] != v {
					t.Errorf("%s = %v, want %v", k, l[k], v)
				}
			}
			for _, s := range []string{"SEGREDO-PANIC", "QS-SECRETO", "goroutine ", "/panico.func"} {
				if strings.Contains(buf.String(), s) {
					t.Errorf("log contém %q:\n%s", s, buf.String())
				}
			}
		})
	}
}
