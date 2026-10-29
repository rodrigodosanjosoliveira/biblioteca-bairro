package httpapi_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"os"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	httpapi "github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/storage"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/migrations"
)

const (
	envURLBanco         = "BIBLIOTECA_TEST_DATABASE_URL"
	envBancoObrigatorio = "BIBLIOTECA_TEST_DB_OBRIGATORIO"
)

// urlBanco devolve a URL do banco de teste. Sem a variável, o teste é pulado;
// com BIBLIOTECA_TEST_DB_OBRIGATORIO=1, a ausência reprova.
func urlBanco(t *testing.T) string {
	t.Helper()
	u := os.Getenv(envURLBanco)
	if u == "" {
		if os.Getenv(envBancoObrigatorio) == "1" {
			t.Fatalf("%s ausente com %s=1: teste de banco obrigatório", envURLBanco, envBancoObrigatorio)
		}
		t.Skipf("%s não definida: teste de banco pulado", envURLBanco)
	}
	return u
}

// novoPool cria o schema descartável t_<hex>, aplica as migrações de
// migrations.FS e devolve um pgxpool com search_path no schema e maxConns
// conexões. O DROP SCHEMA ... CASCADE roda no Cleanup, depois do pool.Close.
func novoPool(t *testing.T, maxConns int32) *pgxpool.Pool {
	t.Helper()
	u := urlBanco(t)

	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("gerar nome do schema: %v", err)
	}
	schema := "t_" + hex.EncodeToString(b[:])
	ident := pgx.Identifier{schema}.Sanitize()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	ccfg, err := pgx.ParseConfig(u)
	if err != nil {
		t.Fatalf("interpretar %s: falhou", envURLBanco)
	}
	admin, err := pgx.ConnectConfig(ctx, ccfg)
	if err != nil {
		// Sem o erro do pgx: ele traz host, usuário e banco.
		t.Fatalf("conectar ao banco de teste: falhou (confira %s)", envURLBanco)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatalf("criar schema de teste: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(ctx, u)
		if err != nil {
			t.Errorf("conectar para remover schema %s: falhou", schema)
			return
		}
		defer c.Close(ctx)
		if _, err := c.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE"); err != nil {
			t.Errorf("remover schema %s: %v", schema, err)
		}
	})

	if _, err := admin.Exec(ctx, "SET search_path TO "+ident); err != nil {
		t.Fatalf("search_path: %v", err)
	}
	nomes, err := fs.Glob(migrations.FS, "*.up.sql")
	if err != nil || len(nomes) == 0 {
		t.Fatalf("listar migrações: %v (%d)", err, len(nomes))
	}
	sort.Strings(nomes)
	for _, n := range nomes {
		sql, err := fs.ReadFile(migrations.FS, n)
		if err != nil {
			t.Fatalf("ler %s: %v", n, err)
		}
		if _, err := admin.Exec(ctx, string(sql)); err != nil {
			t.Fatalf("aplicar %s: %v", n, err)
		}
	}

	pcfg, err := pgxpool.ParseConfig(u)
	if err != nil {
		t.Fatalf("interpretar %s para o pool: falhou", envURLBanco)
	}
	pcfg.ConnConfig.RuntimeParams["search_path"] = schema
	pcfg.MaxConns = maxConns
	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		t.Fatalf("criar pool: falhou (confira %s)", envURLBanco)
	}
	t.Cleanup(pool.Close)
	return pool
}

// relogio é uma fonte de tempo ajustável e segura para concorrência.
type relogio struct {
	mu sync.Mutex
	t  time.Time
}

func (r *relogio) agora() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.t
}

func (r *relogio) ajustar(t time.Time) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.t = t
}

func handlerBanco(pool *pgxpool.Pool, rel *relogio, timeout time.Duration) http.Handler {
	return httpapi.NovoHandler(httpapi.Dependencias{
		Repo:            storage.NovoRepositorio(pool),
		Pinger:          pool,
		Agora:           rel.agora,
		Token:           tokenTeste,
		Logger:          slog.New(slog.NewJSONHandler(io.Discard, nil)),
		TimeoutOperacao: timeout,
	})
}

func chamar(h http.Handler, method, path, body string) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	req.Header.Set("Authorization", "Bearer "+tokenTeste)
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func exigirStatus(t *testing.T, rec *httptest.ResponseRecorder, status int) map[string]any {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (corpo %s)", rec.Code, status, rec.Body.String())
	}
	return objetoJSON(t, rec.Body.Bytes())
}

func idDe(t *testing.T, m map[string]any) string {
	t.Helper()
	n, ok := m["id"].(json.Number)
	if !ok {
		t.Fatalf("id = %#v, want número", m["id"])
	}
	return n.String()
}

func TestBancoFluxoCompleto(t *testing.T) {
	pool := novoPool(t, 4)
	rel := &relogio{t: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)}
	h := handlerBanco(pool, rel, 5*time.Second)

	if rec := chamar(h, http.MethodGet, "/readyz", ""); rec.Code != http.StatusOK {
		t.Fatalf("readyz = %d, want 200", rec.Code)
	}
	livro := exigirStatus(t, chamar(h, http.MethodPost, "/livros", `{"titulo":"  Dom Casmurro "}`), 201)
	if livro["titulo"] != "Dom Casmurro" {
		t.Errorf("titulo = %v, want aparado", livro["titulo"])
	}
	usuario := exigirStatus(t, chamar(h, http.MethodPost, "/usuarios", `{"nome":"Capitu"}`), 201)
	lid, uid := idDe(t, livro), idDe(t, usuario)

	criado := exigirStatus(t, chamar(h, http.MethodPost, "/livros/"+lid+"/emprestimo", `{"usuario_id":`+uid+`}`), 201)
	if criado["livro_id"] != json.Number(lid) || criado["usuario_id"] != json.Number(uid) ||
		criado["inicio"] != "2026-03-10T13:00:00Z" || criado["vencimento"] != "2026-03-24" ||
		criado["devolucao"] != nil || criado["multa"] != "0.00" {
		t.Errorf("empréstimo criado = %v", criado)
	}
	eid := idDe(t, criado)

	rel.ajustar(time.Date(2026, 3, 26, 12, 0, 0, 0, sp))
	consulta := exigirStatus(t, chamar(h, http.MethodGet, "/livros/"+lid+"/emprestimo", ""), 200)
	if idDe(t, consulta) != eid || consulta["multa"] != "2.00" || consulta["devolucao"] != nil || consulta["vencimento"] != "2026-03-24" {
		t.Errorf("consulta = %v, want mesmo id, multa 2.00 e devolucao null", consulta)
	}

	if rec := chamar(h, http.MethodPost, "/livros/"+lid+"/emprestimo", `{"usuario_id":`+uid+`}`); rec.Code != http.StatusConflict {
		t.Errorf("segundo empréstimo = %d, want 409", rec.Code)
	}

	dev := exigirStatus(t, chamar(h, http.MethodPost, "/livros/"+lid+"/devolucao", ""), 200)
	if idDe(t, dev) != eid || dev["devolucao"] != "2026-03-26T15:00:00Z" || dev["multa"] != "2.00" {
		t.Errorf("devolução = %v, want devolucao 2026-03-26T15:00:00Z e multa 2.00", dev)
	}

	rec := chamar(h, http.MethodGet, "/livros/"+lid+"/emprestimo", "")
	checarErro(t, rec, http.StatusNotFound, "emprestimo_aberto_nao_encontrado", true)
	rec = chamar(h, http.MethodPost, "/livros/"+lid+"/devolucao", "")
	checarErro(t, rec, http.StatusNotFound, "emprestimo_aberto_nao_encontrado", true)

	novo := exigirStatus(t, chamar(h, http.MethodPost, "/livros/"+lid+"/emprestimo", `{"usuario_id":`+uid+`}`), 201)
	if idDe(t, novo) == eid {
		t.Error("novo empréstimo reaproveitou o id")
	}
	if novo["vencimento"] != "2026-04-09" {
		t.Errorf("vencimento = %v, want 2026-04-09", novo["vencimento"])
	}
}

func TestBancoReferenciasInexistentes(t *testing.T) {
	pool := novoPool(t, 4)
	rel := &relogio{t: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)}
	h := handlerBanco(pool, rel, 5*time.Second)

	lid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/livros", `{"titulo":"a"}`), 201))
	uid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/usuarios", `{"nome":"b"}`), 201))

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		status int
		codigo string
	}{
		{name: "usuario inexistente", method: http.MethodPost, path: "/livros/" + lid + "/emprestimo", body: `{"usuario_id":999999}`, status: 422, codigo: "usuario_nao_encontrado"},
		{name: "livro inexistente", method: http.MethodPost, path: "/livros/999999/emprestimo", body: `{"usuario_id":` + uid + `}`, status: 404, codigo: "livro_nao_encontrado"},
		{name: "devolucao sem emprestimo", method: http.MethodPost, path: "/livros/" + lid + "/devolucao", status: 404, codigo: "emprestimo_aberto_nao_encontrado"},
		{name: "devolucao de livro inexistente", method: http.MethodPost, path: "/livros/999999/devolucao", status: 404, codigo: "emprestimo_aberto_nao_encontrado"},
		{name: "consulta sem emprestimo", method: http.MethodGet, path: "/livros/" + lid + "/emprestimo", status: 404, codigo: "emprestimo_aberto_nao_encontrado"},
		{name: "NUL não chega ao banco", method: http.MethodPost, path: "/livros", body: `{"titulo":"a\u0000b"}`, status: 422, codigo: "texto_invalido"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := chamar(h, tt.method, tt.path, tt.body)
			checarErro(t, rec, tt.status, tt.codigo, true)
			semTextoInterno(t, "resposta", rec.Body.String())
		})
	}
}

// paralelo dispara n requisições iguais ao mesmo tempo e devolve os status e
// códigos.
func paralelo(h http.Handler, n int, method, path, body string) []*httptest.ResponseRecorder {
	out := make([]*httptest.ResponseRecorder, n)
	largada := make(chan struct{})
	var wg sync.WaitGroup
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-largada
			out[i] = chamar(h, method, path, body)
		}(i)
	}
	close(largada)
	wg.Wait()
	return out
}

func TestBancoEmprestimoConcorrente(t *testing.T) {
	const n = 16
	pool := novoPool(t, 8)
	rel := &relogio{t: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)}
	h := handlerBanco(pool, rel, 5*time.Second)
	lid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/livros", `{"titulo":"a"}`), 201))
	uid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/usuarios", `{"nome":"b"}`), 201))

	recs := paralelo(h, n, http.MethodPost, "/livros/"+lid+"/emprestimo", `{"usuario_id":`+uid+`}`)
	criados := 0
	for _, rec := range recs {
		switch rec.Code {
		case http.StatusCreated:
			criados++
		case http.StatusConflict:
			if c, _ := erroJSON(t, rec.Body.Bytes()); c != "livro_ja_emprestado" {
				t.Errorf("409 com codigo %q, want livro_ja_emprestado", c)
			}
		default:
			t.Errorf("status inesperado %d: %s", rec.Code, rec.Body.String())
		}
	}
	if criados != 1 {
		t.Errorf("%d empréstimos criados, want exatamente 1 (R4)", criados)
	}
}

func TestBancoDevolucaoConcorrente(t *testing.T) {
	const n = 16
	pool := novoPool(t, 8)
	rel := &relogio{t: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)}
	h := handlerBanco(pool, rel, 5*time.Second)
	lid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/livros", `{"titulo":"a"}`), 201))
	uid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/usuarios", `{"nome":"b"}`), 201))
	exigirStatus(t, chamar(h, http.MethodPost, "/livros/"+lid+"/emprestimo", `{"usuario_id":`+uid+`}`), 201)
	rel.ajustar(time.Date(2026, 3, 12, 10, 0, 0, 0, sp))

	recs := paralelo(h, n, http.MethodPost, "/livros/"+lid+"/devolucao", "")
	ok := 0
	for _, rec := range recs {
		switch rec.Code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			if c, _ := erroJSON(t, rec.Body.Bytes()); c != "emprestimo_ja_devolvido" && c != "emprestimo_alterado" {
				t.Errorf("409 com codigo %q", c)
			}
		case http.StatusNotFound:
			if c, _ := erroJSON(t, rec.Body.Bytes()); c != "emprestimo_aberto_nao_encontrado" {
				t.Errorf("404 com codigo %q", c)
			}
		default:
			t.Errorf("status inesperado %d: %s", rec.Code, rec.Body.String())
		}
	}
	if ok != 1 {
		t.Errorf("%d devoluções com 200, want exatamente 1", ok)
	}
	rec := chamar(h, http.MethodGet, "/livros/"+lid+"/emprestimo", "")
	checarErro(t, rec, http.StatusNotFound, "emprestimo_aberto_nao_encontrado", true)
}

func TestBancoPoolEsgotado(t *testing.T) {
	pool := novoPool(t, 1)
	rel := &relogio{t: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)}
	h := handlerBanco(pool, rel, 200*time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	conn, err := pool.Acquire(ctx)
	if err != nil {
		t.Fatalf("Acquire: falhou")
	}
	liberada := false
	defer func() {
		if !liberada {
			conn.Release()
		}
	}()

	inicio := time.Now()
	rec := chamar(h, http.MethodPost, "/livros", `{"titulo":"a"}`)
	checarErro(t, rec, http.StatusServiceUnavailable, "indisponivel", true)
	if d := time.Since(inicio); d > 2*time.Second {
		t.Errorf("503 levou %v, want perto do timeout de 200ms", d)
	}
	rec = chamar(h, http.MethodGet, "/readyz", "")
	checarErro(t, rec, http.StatusServiceUnavailable, "indisponivel", false)

	conn.Release()
	liberada = true
	if rec := chamar(h, http.MethodGet, "/readyz", ""); rec.Code != http.StatusOK {
		t.Errorf("readyz depois de liberar = %d, want 200", rec.Code)
	}
	exigirStatus(t, chamar(h, http.MethodPost, "/livros", `{"titulo":"a"}`), 201)
}
