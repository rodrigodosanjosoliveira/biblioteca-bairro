package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"testing"
	"time"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/storage"
)

// Chaves de toda linha de log de requisição.
var chavesBase = []string{"time", "level", "msg", "metodo", "rota", "status", "duracao_ms", "request_id"}

func comChaves(extra ...string) []string {
	return append(append([]string(nil), chavesBase...), extra...)
}

func linhasLog(t testing.TB, buf *bytes.Buffer) []map[string]any {
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

// TestLogChaves fixa o conjunto exato de chaves de cada tipo de linha:
// base em sucesso e nos 404/405 do mux; + codigo em erro mapeado; + causa em
// 500; + pg_code e pg_constraint quando há PgError; + panic em panic.
func TestLogChaves(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		auth   bool
		setup  func(f *fixture)
		status int
		rota   string
		chaves []string
		campos map[string]any
	}{
		{name: "201 livro", method: http.MethodPost, path: "/livros", body: `{"titulo":"a"}`, auth: true, status: 201, rota: "POST /livros", chaves: chavesBase},
		{name: "200 consulta", method: http.MethodGet, path: "/livros/3/emprestimo", auth: true, status: 200, rota: "GET /livros/{id}/emprestimo", chaves: chavesBase},
		{name: "200 healthz", method: http.MethodGet, path: "/healthz", status: 200, rota: "GET /healthz", chaves: chavesBase},
		{name: "404 do mux", method: http.MethodGet, path: "/nada", status: 404, rota: "", chaves: chavesBase},
		{name: "405 do mux", method: http.MethodGet, path: "/livros", status: 405, rota: "", chaves: chavesBase},
		{name: "401", method: http.MethodPost, path: "/livros/1/devolucao", status: 401, rota: "POST /livros/{id}/devolucao", chaves: comChaves("codigo"), campos: map[string]any{"codigo": "nao_autorizado"}},
		{name: "400 id", method: http.MethodGet, path: "/livros/x/emprestimo", auth: true, status: 400, rota: "GET /livros/{id}/emprestimo", chaves: comChaves("codigo"), campos: map[string]any{"codigo": "id_invalido"}},
		{name: "400 corpo", method: http.MethodPost, path: "/usuarios", body: `{"nome":1}`, auth: true, status: 400, rota: "POST /usuarios", chaves: comChaves("codigo"), campos: map[string]any{"codigo": "corpo_invalido"}},
		{name: "413", method: http.MethodPost, path: "/usuarios", body: `{"nome":"` + strings.Repeat("n", 70<<10) + `"}`, auth: true, status: 413, rota: "POST /usuarios", chaves: comChaves("codigo"), campos: map[string]any{"codigo": "corpo_muito_grande"}},
		{name: "422", method: http.MethodPost, path: "/livros/1/emprestimo", body: `{"usuario_id":0}`, auth: true, status: 422, rota: "POST /livros/{id}/emprestimo", chaves: comChaves("codigo"), campos: map[string]any{"codigo": "usuario_id_invalido"}},
		{name: "409", method: http.MethodPost, path: "/livros/1/emprestimo", body: `{"usuario_id":2}`, auth: true, setup: func(f *fixture) { f.repo.errAbrir = storage.ErrLivroJaEmprestado }, status: 409, rota: "POST /livros/{id}/emprestimo", chaves: comChaves("codigo"), campos: map[string]any{"codigo": "livro_ja_emprestado"}},
		{name: "503 timeout", method: http.MethodGet, path: "/livros/1/emprestimo", auth: true, setup: func(f *fixture) { f.repo.errBuscar = context.DeadlineExceeded }, status: 503, rota: "GET /livros/{id}/emprestimo", chaves: comChaves("codigo"), campos: map[string]any{"codigo": "indisponivel"}},
		{name: "503 readyz", method: http.MethodGet, path: "/readyz", setup: func(f *fixture) { f.pinger.err = errors.New("host=segredo") }, status: 503, rota: "GET /readyz", chaves: comChaves("codigo"), campos: map[string]any{"codigo": "indisponivel"}},
		{name: "500 PgError", method: http.MethodPost, path: "/livros", body: `{"titulo":"a"}`, auth: true, setup: func(f *fixture) { f.repo.errInserirLivro = pgErroSecreto() }, status: 500, rota: "POST /livros",
			chaves: comChaves("codigo", "causa", "pg_code", "pg_constraint"),
			campos: map[string]any{"codigo": "erro_interno", "causa": "banco", "pg_code": "23514", "pg_constraint": "livro_titulo_check"}},
		{name: "500 relogio divergente", method: http.MethodPost, path: "/livros/1/devolucao", auth: true, setup: func(f *fixture) { f.agora = inicioPadrao().AddDate(0, 0, -2) }, status: 500, rota: "POST /livros/{id}/devolucao",
			chaves: comChaves("codigo", "causa"), campos: map[string]any{"codigo": "erro_interno", "causa": "relogio_divergente"}},
		{name: "500 ErrJaPersistido", method: http.MethodPost, path: "/livros", body: `{"titulo":"a"}`, auth: true, setup: func(f *fixture) { f.repo.errInserirLivro = storage.ErrJaPersistido }, status: 500, rota: "POST /livros",
			chaves: comChaves("codigo", "causa"), campos: map[string]any{"codigo": "erro_interno", "causa": "bug_interno"}},
		{name: "500 ErrEmprestimoInvalido", method: http.MethodPost, path: "/livros/1/emprestimo", body: `{"usuario_id":2}`, auth: true, setup: func(f *fixture) { f.repo.errAbrir = domain.ErrEmprestimoInvalido }, status: 500, rota: "POST /livros/{id}/emprestimo",
			chaves: comChaves("codigo", "causa"), campos: map[string]any{"codigo": "erro_interno", "causa": "bug_interno"}},
		{name: "500 ErrReferenciaZero", method: http.MethodGet, path: "/livros/1/emprestimo", auth: true, setup: func(f *fixture) { f.agora = time.Time{} }, status: 500, rota: "GET /livros/{id}/emprestimo",
			chaves: comChaves("codigo", "causa"), campos: map[string]any{"codigo": "erro_interno", "causa": "bug_interno"}},
		{name: "500 desconhecido", method: http.MethodGet, path: "/livros/1/emprestimo", auth: true, setup: func(f *fixture) { f.repo.errBuscar = errors.New("host=segredo") }, status: 500, rota: "GET /livros/{id}/emprestimo",
			chaves: comChaves("codigo", "causa"), campos: map[string]any{"codigo": "erro_interno", "causa": "desconhecido"}},
		{name: "500 panic", method: http.MethodPost, path: "/usuarios", body: `{"nome":"a"}`, auth: true, setup: func(f *fixture) { f.repo.panico = "SEGREDO-PANIC" }, status: 500, rota: "POST /usuarios",
			chaves: comChaves("codigo", "panic"), campos: map[string]any{"codigo": "erro_interno", "panic": true}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			rec := f.do(tt.method, tt.path, tt.body, tt.auth)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d (corpo %.200s)", rec.Code, tt.status, rec.Body.String())
			}
			ls := linhasLog(t, &f.logs)
			if len(ls) != 1 {
				t.Fatalf("%d linhas de log, want 1:\n%s", len(ls), f.logs.String())
			}
			l := ls[0]
			want := append([]string(nil), tt.chaves...)
			sort.Strings(want)
			if got := chavesOrdenadas(l); got != strings.Join(want, ",") {
				t.Errorf("chaves = %s, want %s", got, strings.Join(want, ","))
			}
			if l["metodo"] != tt.method {
				t.Errorf("metodo = %v, want %s", l["metodo"], tt.method)
			}
			if l["rota"] != tt.rota {
				t.Errorf("rota = %v, want %q", l["rota"], tt.rota)
			}
			if st, ok := l["status"].(float64); !ok || int(st) != tt.status {
				t.Errorf("status no log = %v, want %d", l["status"], tt.status)
			}
			if d, ok := l["duracao_ms"].(float64); !ok || d < 0 {
				t.Errorf("duracao_ms = %v, want número >= 0", l["duracao_ms"])
			}
			if id := rec.Header().Get("X-Request-ID"); l["request_id"] != id {
				t.Errorf("request_id = %v, want %q (o X-Request-ID da resposta)", l["request_id"], id)
			}
			for k, v := range tt.campos {
				if l[k] != v {
					t.Errorf("%s = %#v, want %#v", k, l[k], v)
				}
			}
			semTextoInterno(t, "log", f.logs.String())
		})
	}
}

// TestLogSemSegredos manda token, título, nome, query, DSN e textos de
// PgError por várias rotas e confere que nada disso chega ao log, nem às
// respostas de erro.
func TestLogSemSegredos(t *testing.T) {
	const (
		titulo  = "TITULO-SECRETO-XYZ"
		nome    = "NOME-SECRETO-ABC"
		tokErr  = "TOKEN-ERRADO-SECRETO-0123456789abcdef"
		query   = "QS-SECRETO"
		dsn     = "postgres://u:SENHA-DSN@banco.interno:5432/db"
		campo   = "CAMPO-SECRETO"
		panicV  = "VALOR-PANIC-SECRETO"
		detalhe = "SEGREDO-DETAIL"
	)
	segredos := []string{tokenTeste, tokErr, titulo, nome, query, "SENHA-DSN", "banco.interno", campo, panicV,
		detalhe, "SEGREDO-MSG", "SEGREDO-HINT", "SEGREDO-WHERE", "host=segredo", "goroutine "}

	f := novaFixture(t)
	var erros []string
	passo := func(method, path, body string, hdr http.Header, setup func()) {
		f.repo.errInserirLivro, f.repo.errInserirUsuario, f.repo.errAbrir, f.repo.errBuscar, f.repo.errRegistrar = nil, nil, nil, nil, nil
		f.repo.panico = nil
		f.pinger.err = nil
		if setup != nil {
			setup()
		}
		rec := f.doHeader(method, path, body, hdr)
		if rec.Code >= 400 {
			erros = append(erros, rec.Body.String())
		}
	}
	ok := authHeader(true)
	errado := http.Header{}
	errado.Set("Authorization", "Bearer "+tokErr)

	passo(http.MethodPost, "/livros?q="+query, `{"titulo":"`+titulo+`"}`, ok, nil)
	passo(http.MethodPost, "/usuarios", `{"nome":"`+nome+`"}`, ok, nil)
	passo(http.MethodPost, "/livros", `{"titulo":"`+titulo+`"}`, errado, nil)
	passo(http.MethodPost, "/livros", `{"titulo":"`+titulo+`\u0000"}`, ok, nil)
	passo(http.MethodPost, "/usuarios", `{"nome":"`+nome+`","`+campo+`":1}`, ok, nil)
	passo(http.MethodPost, "/usuarios", `{"nome":"`+nome+`"`, ok, nil)
	passo(http.MethodPost, "/usuarios", `{"nome":"`+nome+`"}`, ok, func() { f.repo.errInserirUsuario = pgErroSecreto() })
	passo(http.MethodPost, "/livros", `{"titulo":"`+titulo+`"}`, ok, func() { f.repo.errInserirLivro = errors.New("dial " + dsn + " host=segredo") })
	passo(http.MethodGet, "/livros/1/emprestimo?token="+tokenTeste, "", ok, func() { f.repo.errBuscar = fmt.Errorf("storage: %w", pgErroSecreto()) })
	passo(http.MethodPost, "/livros/1/devolucao", "", ok, func() { f.repo.errRegistrar = errors.New("host=segredo " + detalhe) })
	passo(http.MethodGet, "/readyz", "", nil, func() { f.pinger.err = errors.New("ping " + dsn) })
	passo(http.MethodPost, "/usuarios", `{"nome":"`+nome+`"}`, ok, func() { f.repo.panico = panicV })
	passo(http.MethodPost, "/livros", `{"titulo":"`+titulo+`"}`, ok, func() { f.repo.panico = errors.New(panicV) })

	log := f.logs.String()
	if n := len(linhasLog(t, &f.logs)); n != 13 {
		t.Errorf("%d linhas de log, want 13 (uma por requisição)", n)
	}
	for _, s := range segredos {
		if strings.Contains(log, s) {
			t.Errorf("log contém %q", s)
		}
		for _, e := range erros {
			if strings.Contains(e, s) {
				t.Errorf("resposta de erro contém %q: %s", s, e)
			}
		}
	}
}

// http.ErrAbortHandler atravessa o handler inteiro.
func TestErrAbortHandlerRepropagado(t *testing.T) {
	f := novaFixture(t)
	f.repo.panico = http.ErrAbortHandler
	defer func() {
		if got := recover(); got != http.ErrAbortHandler {
			t.Fatalf("recover() = %v, want http.ErrAbortHandler", got)
		}
	}()
	req := httptest.NewRequest(http.MethodGet, "/livros/1/emprestimo", nil)
	req.Header.Set("Authorization", "Bearer "+tokenTeste)
	f.handler().ServeHTTP(httptest.NewRecorder(), req)
}
