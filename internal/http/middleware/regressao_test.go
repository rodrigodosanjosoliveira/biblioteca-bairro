package middleware_test

import (
	"bytes"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http/middleware"
)

// Regressões das revisões da DEL-006 (security-review e code-reviewer).

const politicaCSP = "default-src 'none'; frame-ancestors 'none'"

func TestCabecalhosJSONComCSP(t *testing.T) {
	tests := []struct {
		name   string
		header string
		want   string
	}{
		{name: "Content-Type", header: "Content-Type", want: "application/json; charset=utf-8"},
		{name: "nosniff", header: "X-Content-Type-Options", want: "nosniff"},
		{name: "CSP", header: "Content-Security-Policy", want: politicaCSP},
	}
	rec := httptest.NewRecorder()
	middleware.CabecalhosJSON(rec)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := rec.Header().Get(tt.header); got != tt.want {
				t.Errorf("%s = %q, want %q", tt.header, got, tt.want)
			}
		})
	}
}

// TestResponderErroConnectionClose: só o 413 leva Connection: close.
func TestResponderErroConnectionClose(t *testing.T) {
	tests := []struct {
		name   string
		status int
		want   string
	}{
		{name: "413", status: http.StatusRequestEntityTooLarge, want: "close"},
		{name: "400", status: http.StatusBadRequest, want: ""},
		{name: "401", status: http.StatusUnauthorized, want: ""},
		{name: "500", status: http.StatusInternalServerError, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			middleware.ResponderErro(rec, httptest.NewRequest(http.MethodPost, "/", nil), tt.status, "c", "m")
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if got := rec.Header().Get("Connection"); got != tt.want {
				t.Errorf("Connection = %q, want %q", got, tt.want)
			}
			if got := rec.Header().Get("Content-Security-Policy"); got != politicaCSP {
				t.Errorf("Content-Security-Policy = %q, want %q", got, politicaCSP)
			}
		})
	}
}

// TestRecoverDepoisDeEscrever: panic depois de a resposta começar não pode
// virar um segundo status nem um corpo JSON colado; vira http.ErrAbortHandler.
func TestRecoverDepoisDeEscrever(t *testing.T) {
	tests := []struct {
		name     string
		h        http.HandlerFunc
		wantBody string
	}{
		{name: "WriteHeader e panic string", h: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusOK)
			panic("SEGREDO-PANIC")
		}},
		{name: "Write e panic error", h: func(w http.ResponseWriter, r *http.Request) {
			_, _ = w.Write([]byte("parcial"))
			panic(errors.New("SEGREDO-PANIC"))
		}, wantBody: "parcial"},
		{name: "WriteHeader 201 e panic", h: func(w http.ResponseWriter, r *http.Request) {
			w.WriteHeader(http.StatusCreated)
			_, _ = w.Write([]byte("{"))
			panic("SEGREDO-PANIC")
		}, wantBody: "{"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			func() {
				defer func() {
					if got := recover(); got != http.ErrAbortHandler {
						t.Fatalf("recover() = %v, want http.ErrAbortHandler", got)
					}
				}()
				middleware.Recover(tt.h).ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			}()
			if got := rec.Body.String(); got != tt.wantBody {
				t.Errorf("corpo = %q, want %q (sem erro_interno colado)", got, tt.wantBody)
			}
		})
	}
}

// TestLogAbortado: panic que atravessa o Recover gera a linha de log com
// causa=abortado (nível ERROR) antes de ser repropagado, sem o valor do panic.
func TestLogAbortado(t *testing.T) {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /abortar", func(w http.ResponseWriter, r *http.Request) {
		panic(http.ErrAbortHandler)
	})
	mux.HandleFunc("GET /escreve-e-panica", func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusAccepted)
		panic("SEGREDO-PANIC")
	})
	mux.HandleFunc("GET /anota-e-aborta", func(w http.ResponseWriter, r *http.Request) {
		middleware.Anotar(r.Context(), slog.String("codigo", "x"))
		panic(http.ErrAbortHandler)
	})

	tests := []struct {
		name       string
		path       string
		wantRota   string
		wantStatus int
		wantChaves []string
		extra      map[string]any
	}{
		{name: "ErrAbortHandler sem escrever", path: "/abortar", wantRota: "GET /abortar", wantStatus: 0,
			wantChaves: append(append([]string(nil), chavesBase...), "causa")},
		{name: "panic depois de escrever", path: "/escreve-e-panica", wantRota: "GET /escreve-e-panica", wantStatus: http.StatusAccepted,
			wantChaves: append(append([]string(nil), chavesBase...), "panic", "causa"), extra: map[string]any{"panic": true}},
		{name: "anotação preservada", path: "/anota-e-aborta", wantRota: "GET /anota-e-aborta", wantStatus: 0,
			wantChaves: append(append([]string(nil), chavesBase...), "codigo", "causa"), extra: map[string]any{"codigo": "x"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			h := cadeia(slog.New(slog.NewJSONHandler(&buf, nil)), mux)
			rec := httptest.NewRecorder()
			func() {
				defer func() {
					if got := recover(); got != http.ErrAbortHandler {
						t.Fatalf("recover() = %v, want http.ErrAbortHandler", got)
					}
				}()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, tt.path, nil))
			}()
			ls := linhas(t, &buf)
			if len(ls) != 1 {
				t.Fatalf("%d linhas de log, want 1:\n%s", len(ls), buf.String())
			}
			l := ls[0]
			mesmasChaves(t, l, tt.wantChaves)
			if l["causa"] != "abortado" {
				t.Errorf("causa = %v, want abortado", l["causa"])
			}
			if l["level"] != "ERROR" {
				t.Errorf("level = %v, want ERROR", l["level"])
			}
			if l["rota"] != tt.wantRota {
				t.Errorf("rota = %v, want %q", l["rota"], tt.wantRota)
			}
			if st, ok := l["status"].(float64); !ok || int(st) != tt.wantStatus {
				t.Errorf("status no log = %v, want %d", l["status"], tt.wantStatus)
			}
			if id := rec.Header().Get("X-Request-ID"); l["request_id"] != id || !hex32.MatchString(id) {
				t.Errorf("request_id = %v, X-Request-ID = %q", l["request_id"], id)
			}
			for k, v := range tt.extra {
				if l[k] != v {
					t.Errorf("%s = %v, want %v", k, l[k], v)
				}
			}
			for _, s := range []string{"SEGREDO-PANIC", "goroutine ", "abort Handler"} {
				if strings.Contains(buf.String(), s) {
					t.Errorf("log contém %q:\n%s", s, buf.String())
				}
			}
		})
	}
}

// TestRecoverErrAbortHandlerEmbrulhado: um ErrAbortHandler embrulhado é
// repropagado como http.ErrAbortHandler puro (o net/http não loga o valor nem
// o stack), sem resposta escrita e sem anotar panic.
func TestRecoverErrAbortHandlerEmbrulhado(t *testing.T) {
	tests := []struct {
		name  string
		valor any
	}{
		{name: "embrulhado com %w", valor: fmt.Errorf("x: %w", http.ErrAbortHandler)},
		{name: "embrulhado duas vezes", valor: fmt.Errorf("SEGREDO-PANIC: %w", fmt.Errorf("y: %w", http.ErrAbortHandler))},
		{name: "puro", valor: http.ErrAbortHandler},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var buf bytes.Buffer
			mux := http.NewServeMux()
			mux.HandleFunc("GET /", func(w http.ResponseWriter, r *http.Request) { panic(tt.valor) })
			h := cadeia(slog.New(slog.NewJSONHandler(&buf, nil)), mux)
			rec := httptest.NewRecorder()
			func() {
				defer func() {
					got := recover()
					if err, ok := got.(error); !ok || err != http.ErrAbortHandler {
						t.Fatalf("recover() = %#v, want exatamente http.ErrAbortHandler", got)
					}
				}()
				h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, "/", nil))
			}()
			if rec.Body.Len() != 0 {
				t.Errorf("Recover escreveu resposta: %q", rec.Body.String())
			}
			ls := linhas(t, &buf)
			if len(ls) != 1 {
				t.Fatalf("%d linhas de log, want 1:\n%s", len(ls), buf.String())
			}
			mesmasChaves(t, ls[0], append(append([]string(nil), chavesBase...), "causa"))
			if strings.Contains(buf.String(), "SEGREDO-PANIC") {
				t.Errorf("log contém o valor do panic: %s", buf.String())
			}
		})
	}
}
