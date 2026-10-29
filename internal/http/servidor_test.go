package httpapi_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strings"
	"testing"
	"time"

	httpapi "github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http"
)

// Assinaturas exigidas.
var (
	_ func(string, http.Handler, *slog.Logger) *http.Server                  = httpapi.NovoServidor
	_ func(context.Context, *http.Server, net.Listener, time.Duration) error = httpapi.Servir
)

func TestNovoServidor(t *testing.T) {
	var buf bytes.Buffer
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	mux := http.NewServeMux()
	srv := httpapi.NovoServidor("127.0.0.1:8081", mux, logger)

	if srv.Addr != "127.0.0.1:8081" {
		t.Errorf("Addr = %q", srv.Addr)
	}
	if h, ok := srv.Handler.(*http.ServeMux); !ok || h != mux {
		t.Errorf("Handler = %v, want o handler recebido", srv.Handler)
	}
	tempos := []struct {
		nome      string
		got, want time.Duration
	}{
		{"ReadHeaderTimeout", srv.ReadHeaderTimeout, 5 * time.Second},
		{"ReadTimeout", srv.ReadTimeout, 10 * time.Second},
		{"WriteTimeout", srv.WriteTimeout, 15 * time.Second},
		{"IdleTimeout", srv.IdleTimeout, 60 * time.Second},
	}
	for _, tt := range tempos {
		if tt.got != tt.want {
			t.Errorf("%s = %v, want %v", tt.nome, tt.got, tt.want)
		}
	}
	if srv.ErrorLog == nil {
		t.Fatal("ErrorLog nil, want logger ligado ao slog")
	}
	srv.ErrorLog.Print("erro-do-net-http")
	if !strings.Contains(buf.String(), "erro-do-net-http") {
		t.Errorf("ErrorLog não escreve no logger: %q", buf.String())
	}
}

func clienteNovo() *http.Client {
	return &http.Client{
		Timeout:   2 * time.Second,
		Transport: &http.Transport{DisableKeepAlives: true},
	}
}

func TestServirShutdownGracioso(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	base := "http://" + ln.Addr().String()

	entrou := make(chan struct{})
	liberar := make(chan struct{})
	mux := http.NewServeMux()
	mux.HandleFunc("GET /lento", func(w http.ResponseWriter, r *http.Request) {
		close(entrou)
		<-liberar
		_, _ = io.WriteString(w, "terminou")
	})
	mux.HandleFunc("GET /rapido", func(w http.ResponseWriter, r *http.Request) {
		_, _ = io.WriteString(w, "ok")
	})
	srv := httpapi.NovoServidor(ln.Addr().String(), mux, slog.New(slog.NewJSONHandler(io.Discard, nil)))

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fim := make(chan error, 1)
	go func() { fim <- httpapi.Servir(ctx, srv, ln, 10*time.Second) }()

	resp, err := clienteNovo().Get(base + "/rapido")
	if err != nil {
		t.Fatalf("requisição antes do shutdown: %v", err)
	}
	resp.Body.Close()

	type resultado struct {
		status int
		corpo  string
		err    error
	}
	emVoo := make(chan resultado, 1)
	go func() {
		c := &http.Client{Timeout: 20 * time.Second, Transport: &http.Transport{DisableKeepAlives: true}}
		resp, err := c.Get(base + "/lento")
		if err != nil {
			emVoo <- resultado{err: err}
			return
		}
		defer resp.Body.Close()
		b, _ := io.ReadAll(resp.Body)
		emVoo <- resultado{status: resp.StatusCode, corpo: string(b)}
	}()
	select {
	case <-entrou:
	case <-time.After(5 * time.Second):
		t.Fatal("requisição lenta não chegou ao handler")
	}

	cancel()

	recusada := false
	limite := time.Now().Add(5 * time.Second)
	for time.Now().Before(limite) {
		resp, err := clienteNovo().Get(base + "/rapido")
		if err != nil {
			recusada = true
			break
		}
		resp.Body.Close()
		time.Sleep(10 * time.Millisecond)
	}
	if !recusada {
		t.Fatal("requisição nova aceita depois do cancelamento do contexto")
	}

	select {
	case err := <-fim:
		t.Fatalf("Servir voltou (%v) com requisição em voo", err)
	case <-time.After(100 * time.Millisecond):
	}

	close(liberar)
	select {
	case r := <-emVoo:
		if r.err != nil || r.status != http.StatusOK || r.corpo != "terminou" {
			t.Errorf("requisição em voo: status %d, corpo %q, erro %v; want 200 terminou", r.status, r.corpo, r.err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("requisição em voo não terminou")
	}
	select {
	case err := <-fim:
		if err != nil {
			t.Errorf("Servir() = %v, want nil no shutdown gracioso", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("Servir não voltou depois do shutdown")
	}
}

func TestServirPrazoDeShutdownEsgotado(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	entrou := make(chan struct{})
	liberar := make(chan struct{})
	defer close(liberar)
	mux := http.NewServeMux()
	mux.HandleFunc("GET /preso", func(w http.ResponseWriter, r *http.Request) {
		close(entrou)
		<-liberar
	})
	srv := httpapi.NovoServidor(ln.Addr().String(), mux, slog.New(slog.NewJSONHandler(io.Discard, nil)))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	fim := make(chan error, 1)
	go func() { fim <- httpapi.Servir(ctx, srv, ln, 100*time.Millisecond) }()
	go func() {
		resp, err := clienteNovo().Get("http://" + ln.Addr().String() + "/preso")
		if err == nil {
			resp.Body.Close()
		}
	}()
	select {
	case <-entrou:
	case <-time.After(5 * time.Second):
		t.Fatal("requisição não chegou ao handler")
	}
	cancel()
	select {
	case err := <-fim:
		if !errors.Is(err, context.DeadlineExceeded) {
			t.Errorf("Servir() = %v, want context.DeadlineExceeded com prazo esgotado", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Servir não respeitou o prazo de shutdown")
	}
}

func TestServirListenerFechado(t *testing.T) {
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("listen: %v", err)
	}
	ln.Close()
	srv := httpapi.NovoServidor(ln.Addr().String(), http.NewServeMux(), slog.New(slog.NewJSONHandler(io.Discard, nil)))
	fim := make(chan error, 1)
	go func() { fim <- httpapi.Servir(context.Background(), srv, ln, time.Second) }()
	select {
	case err := <-fim:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			t.Errorf("Servir() = %v, want erro do Serve", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Servir não voltou com listener fechado")
	}
}
