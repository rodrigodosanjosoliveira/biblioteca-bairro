package httpapi_test

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"regexp"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"
	_ "time/tzdata"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
	httpapi "github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/storage"
)

// Assinaturas exigidas: o repositório e o pool reais atendem às interfaces do
// consumidor.
var (
	_ httpapi.Repositorio                     = (*storage.Repositorio)(nil)
	_ httpapi.Pinger                          = (*pgxpool.Pool)(nil)
	_ func(httpapi.Dependencias) http.Handler = httpapi.NovoHandler
)

const tokenTeste = "TOKEN-SECRETO-0123456789abcdefghijklmn" // 38 bytes

var (
	sp    = carregarSP()
	hex32 = regexp.MustCompile(`^[0-9a-f]{32}$`)
)

func carregarSP() *time.Location {
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		panic(err)
	}
	return loc
}

// repoFake é o dublê de httpapi.Repositorio. Sem erro configurado, imita o
// storage: valida pelo domínio, gera IDs e devolve valores reidratados.
type repoFake struct {
	mu       sync.Mutex
	chamadas []string
	prazos   []time.Duration // tempo até o deadline do ctx em cada chamada; -1 sem deadline

	errInserirLivro   error
	errInserirUsuario error
	errAbrir          error
	errBuscar         error
	errRegistrar      error

	// bloquear faz toda chamada esperar o ctx e devolver o erro dele embrulhado.
	bloquear bool
	// panico, se não nil, é o valor do panic em toda chamada.
	panico any

	buscarRet    domain.Emprestimo
	registrarRet *domain.Emprestimo

	livroRecebido     domain.Livro
	usuarioRecebido   domain.Usuario
	abrirRecebido     domain.Emprestimo
	buscarRecebido    int64
	registrarRecebido domain.Emprestimo
}

func (r *repoFake) registrar(ctx context.Context, nome string) error {
	r.mu.Lock()
	r.chamadas = append(r.chamadas, nome)
	if dl, ok := ctx.Deadline(); ok {
		r.prazos = append(r.prazos, time.Until(dl))
	} else {
		r.prazos = append(r.prazos, -1)
	}
	bloquear, panico := r.bloquear, r.panico
	r.mu.Unlock()
	if panico != nil {
		panic(panico)
	}
	if bloquear {
		<-ctx.Done()
		return fmt.Errorf("storage: %w", ctx.Err())
	}
	return nil
}

func (r *repoFake) nChamadas() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.chamadas)
}

func (r *repoFake) InserirLivro(ctx context.Context, l domain.Livro) (domain.Livro, error) {
	if err := r.registrar(ctx, "InserirLivro"); err != nil {
		return domain.Livro{}, err
	}
	r.livroRecebido = l
	if r.errInserirLivro != nil {
		return domain.Livro{}, r.errInserirLivro
	}
	novo, err := domain.NovoLivro(l.Titulo)
	if err != nil {
		return domain.Livro{}, err
	}
	novo.ID = 5
	return novo, nil
}

func (r *repoFake) InserirUsuario(ctx context.Context, u domain.Usuario) (domain.Usuario, error) {
	if err := r.registrar(ctx, "InserirUsuario"); err != nil {
		return domain.Usuario{}, err
	}
	r.usuarioRecebido = u
	if r.errInserirUsuario != nil {
		return domain.Usuario{}, r.errInserirUsuario
	}
	novo, err := domain.NovoUsuario(u.Nome)
	if err != nil {
		return domain.Usuario{}, err
	}
	novo.ID = 9
	return novo, nil
}

// dataUTC imita a coluna DATE: meia-noite UTC da data civil em SP.
func dataUTC(v time.Time) time.Time {
	a, m, d := v.In(sp).Date()
	return time.Date(a, m, d, 0, 0, 0, 0, time.UTC)
}

func (r *repoFake) AbrirEmprestimo(ctx context.Context, e domain.Emprestimo) (domain.Emprestimo, error) {
	if err := r.registrar(ctx, "AbrirEmprestimo"); err != nil {
		return domain.Emprestimo{}, err
	}
	r.abrirRecebido = e
	if r.errAbrir != nil {
		return domain.Emprestimo{}, r.errAbrir
	}
	got, err := domain.ReidratarEmprestimo(77, e.LivroID(), e.UsuarioID(), e.Inicio(), dataUTC(e.Vencimento()), time.Time{})
	if err != nil {
		return domain.Emprestimo{}, fmt.Errorf("fake: %w", err)
	}
	return got, nil
}

func (r *repoFake) BuscarEmprestimoAbertoPorLivro(ctx context.Context, livroID int64) (domain.Emprestimo, error) {
	if err := r.registrar(ctx, "BuscarEmprestimoAbertoPorLivro"); err != nil {
		return domain.Emprestimo{}, err
	}
	r.buscarRecebido = livroID
	if r.errBuscar != nil {
		return domain.Emprestimo{}, r.errBuscar
	}
	return r.buscarRet, nil
}

func (r *repoFake) RegistrarDevolucao(ctx context.Context, e domain.Emprestimo) (domain.Emprestimo, error) {
	if err := r.registrar(ctx, "RegistrarDevolucao"); err != nil {
		return domain.Emprestimo{}, err
	}
	r.registrarRecebido = e
	if r.errRegistrar != nil {
		return domain.Emprestimo{}, r.errRegistrar
	}
	if r.registrarRet != nil {
		return *r.registrarRet, nil
	}
	return e, nil
}

// pingerFake é o dublê de httpapi.Pinger.
type pingerFake struct {
	mu      sync.Mutex
	err     error
	chamado int
	prazo   time.Duration

	// entrou, se não nil, recebe um aviso quando um Ping começa; liberar, se
	// não nil, segura o Ping até ser fechado. Ambos são definidos antes do
	// primeiro uso do handler.
	entrou  chan struct{}
	liberar chan struct{}
}

// nPings lê o número de chamadas sob o mutex (seguro com -race).
func (p *pingerFake) nPings() int {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.chamado
}

func (p *pingerFake) Ping(ctx context.Context) error {
	if p.entrou != nil {
		p.entrou <- struct{}{}
	}
	if p.liberar != nil {
		<-p.liberar
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.chamado++
	p.prazo = -1
	if dl, ok := ctx.Deadline(); ok {
		p.prazo = time.Until(dl)
	}
	return p.err
}

// emprestimo reidrata um empréstimo como o storage faria.
func emprestimo(t testing.TB, id, livroID, usuarioID int64, inicio, devolucao time.Time) domain.Emprestimo {
	t.Helper()
	novo, err := domain.NovoEmprestimo(livroID, usuarioID, inicio)
	if err != nil {
		t.Fatalf("NovoEmprestimo: %v", err)
	}
	e, err := domain.ReidratarEmprestimo(id, livroID, usuarioID, inicio, dataUTC(novo.Vencimento()), devolucao)
	if err != nil {
		t.Fatalf("ReidratarEmprestimo: %v", err)
	}
	return e
}

// inicioPadrao é 10/03/2026 10:00 em SP; vencimento em 24/03/2026.
func inicioPadrao() time.Time { return time.Date(2026, 3, 10, 10, 0, 0, 0, sp) }

type fixture struct {
	repo    *repoFake
	pinger  *pingerFake
	agora   time.Time
	timeout time.Duration
	logs    bytes.Buffer
	h       http.Handler
}

func novaFixture(t testing.TB) *fixture {
	t.Helper()
	f := &fixture{
		repo:    &repoFake{},
		pinger:  &pingerFake{},
		agora:   time.Date(2026, 3, 12, 9, 0, 0, 0, sp),
		timeout: 5 * time.Second,
	}
	f.repo.buscarRet = emprestimo(t, 77, 1, 2, inicioPadrao(), time.Time{})
	return f
}

func (f *fixture) handler() http.Handler {
	if f.h == nil {
		f.h = httpapi.NovoHandler(httpapi.Dependencias{
			Repo:            f.repo,
			Pinger:          f.pinger,
			Agora:           func() time.Time { return f.agora },
			Token:           tokenTeste,
			Logger:          slog.New(slog.NewJSONHandler(&f.logs, nil)),
			TimeoutOperacao: f.timeout,
		})
	}
	return f.h
}

// do executa a requisição; com auth, manda o token válido.
func (f *fixture) do(method, path, body string, auth bool) *httptest.ResponseRecorder {
	return f.doHeader(method, path, body, authHeader(auth))
}

func authHeader(auth bool) http.Header {
	h := http.Header{}
	if auth {
		h.Set("Authorization", "Bearer "+tokenTeste)
	}
	return h
}

func (f *fixture) doHeader(method, path, body string, hdr http.Header) *httptest.ResponseRecorder {
	var rd io.Reader
	if body != "" {
		rd = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, path, rd)
	for k, vs := range hdr {
		for _, v := range vs {
			req.Header.Add(k, v)
		}
	}
	rec := httptest.NewRecorder()
	f.handler().ServeHTTP(rec, req)
	return rec
}

// rota descreve uma rota autenticada com um corpo válido.
type rota struct {
	nome   string
	method string
	path   string // com {id} = 1
	body   string
}

func rotasProtegidas() []rota {
	return []rota{
		{nome: "POST /livros", method: http.MethodPost, path: "/livros", body: `{"titulo":"Dom Casmurro"}`},
		{nome: "POST /usuarios", method: http.MethodPost, path: "/usuarios", body: `{"nome":"Capitu"}`},
		{nome: "POST emprestimo", method: http.MethodPost, path: "/livros/1/emprestimo", body: `{"usuario_id":2}`},
		{nome: "POST devolucao", method: http.MethodPost, path: "/livros/1/devolucao"},
		{nome: "GET emprestimo", method: http.MethodGet, path: "/livros/1/emprestimo"},
	}
}

// erroJSON decodifica {"erro":{"codigo","mensagem"}} exigindo exatamente essas
// chaves, com strings não vazias.
func erroJSON(t testing.TB, body []byte) (codigo, mensagem string) {
	t.Helper()
	var top map[string]json.RawMessage
	if err := json.Unmarshal(body, &top); err != nil {
		t.Fatalf("corpo não é JSON: %q", body)
	}
	if len(top) != 1 || top["erro"] == nil {
		t.Fatalf("corpo = %s, want só a chave \"erro\"", body)
	}
	var e map[string]any
	if err := json.Unmarshal(top["erro"], &e); err != nil {
		t.Fatalf("erro não é objeto: %s", top["erro"])
	}
	c, ok1 := e["codigo"].(string)
	m, ok2 := e["mensagem"].(string)
	if len(e) != 2 || !ok1 || !ok2 || c == "" || m == "" {
		t.Fatalf("erro = %s, want exatamente codigo e mensagem (strings não vazias)", top["erro"])
	}
	return c, m
}

// objetoJSON decodifica um objeto com UseNumber.
func objetoJSON(t testing.TB, body []byte) map[string]any {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader(body))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("corpo não é objeto JSON: %q", body)
	}
	return m
}

func chavesOrdenadas(m map[string]any) string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	sort.Strings(out)
	return strings.Join(out, ",")
}

func exigirChaves(t testing.TB, m map[string]any, want ...string) {
	t.Helper()
	sort.Strings(want)
	if got := chavesOrdenadas(m); got != strings.Join(want, ",") {
		t.Errorf("chaves = %s, want %s", got, strings.Join(want, ","))
	}
}

// checarCabecalhos confere os headers de toda resposta JSON da API.
func checarCabecalhos(t testing.TB, rec *httptest.ResponseRecorder, autenticada bool) {
	t.Helper()
	if got := rec.Header().Get("Content-Type"); got != "application/json; charset=utf-8" {
		t.Errorf("Content-Type = %q, want application/json; charset=utf-8", got)
	}
	if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
		t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
	}
	if got := rec.Header().Get("X-Request-ID"); !hex32.MatchString(got) {
		t.Errorf("X-Request-ID = %q, want 32 hex", got)
	}
	if autenticada {
		if got := rec.Header().Get("Cache-Control"); got != "no-store" {
			t.Errorf("Cache-Control = %q, want no-store", got)
		}
	}
}

// checarErro confere status, headers e código de uma resposta de erro.
func checarErro(t testing.TB, rec *httptest.ResponseRecorder, status int, codigo string, autenticada bool) string {
	t.Helper()
	if rec.Code != status {
		t.Fatalf("status = %d, want %d (corpo %s)", rec.Code, status, rec.Body.String())
	}
	checarCabecalhos(t, rec, autenticada)
	c, m := erroJSON(t, rec.Body.Bytes())
	if c != codigo {
		t.Errorf("codigo = %q, want %q", c, codigo)
	}
	return m
}

// textosInternos não podem aparecer em resposta nem em log.
var textosInternos = []string{"storage:", "emprestimo:", "livro:", "usuario:", "multa:", "SQLSTATE", "segredo", "SEGREDO", "fake:"}

func semTextoInterno(t testing.TB, onde, s string) {
	t.Helper()
	for _, x := range textosInternos {
		if strings.Contains(s, x) {
			t.Errorf("%s contém %q: %s", onde, x, s)
		}
	}
}
