// Package httpapi é a camada HTTP do empréstimo: rotas, decodificação de
// corpo, mapa de erros para status e servidor com shutdown gracioso.
package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http/middleware"
)

const (
	// limiteCorpo é o tamanho máximo do corpo de requisição.
	limiteCorpo = 64 << 10
	// prazoReadyz é o prazo do Ping do /readyz.
	prazoReadyz = 2 * time.Second
	// janelaReadyz é por quanto tempo um Ping bem-sucedido vale para o /readyz.
	janelaReadyz = time.Second
	// toleranciaReadyz é por quanto tempo o último Ping bem-sucedido ainda
	// responde 200 a quem chega com outro Ping em andamento: o cache venceu,
	// mas a renovação está no prazo.
	toleranciaReadyz = janelaReadyz + prazoReadyz
	// timeoutOperacaoPadrao vale quando Dependencias.TimeoutOperacao <= 0.
	timeoutOperacaoPadrao = 5 * time.Second
)

// Repositorio é o que a camada HTTP usa da persistência.
// *storage.Repositorio a satisfaz.
type Repositorio interface {
	InserirLivro(ctx context.Context, l domain.Livro) (domain.Livro, error)
	InserirUsuario(ctx context.Context, u domain.Usuario) (domain.Usuario, error)
	AbrirEmprestimo(ctx context.Context, e domain.Emprestimo) (domain.Emprestimo, error)
	BuscarEmprestimoAbertoPorLivro(ctx context.Context, livroID int64) (domain.Emprestimo, error)
	RegistrarDevolucao(ctx context.Context, e domain.Emprestimo) (domain.Emprestimo, error)
}

// Pinger verifica a disponibilidade do banco. *pgxpool.Pool o satisfaz.
type Pinger interface {
	Ping(ctx context.Context) error
}

// Dependencias são as dependências do handler.
type Dependencias struct {
	Repo   Repositorio
	Pinger Pinger
	// Agora é o relógio; nil usa time.Now.
	Agora func() time.Time
	// Token é o bearer token das rotas autenticadas; vazio recusa tudo.
	Token string
	// Logger recebe uma linha por requisição; nil descarta.
	Logger *slog.Logger
	// TimeoutOperacao é o prazo do contexto passado ao repositório em cada
	// requisição; <= 0 usa 5s.
	TimeoutOperacao time.Duration
}

type api struct {
	repo    Repositorio
	pinger  Pinger
	agora   func() time.Time
	timeout time.Duration
	pronto  prontidao
}

// prontidao limita o custo do /readyz, que é anônimo: no máximo um Ping em
// andamento (pingando funciona como semáforo de capacidade 1) e um Ping
// bem-sucedido vale por janelaReadyz no relógio Agora. Com um Ping em
// andamento, o último sucesso vale por toleranciaReadyz. Falha não é guardada
// e apaga o sucesso anterior: a requisição seguinte volta a pingar, para que o
// serviço volte a ficar pronto assim que o banco responder.
type prontidao struct {
	pingando chan struct{}
	mu       sync.Mutex
	okEm     time.Time
	temOK    bool
}

// recente informa se há Ping bem-sucedido dentro da janela em agora. Relógio
// que voltou para antes do último Ping invalida o cache.
func (p *prontidao) recente(agora time.Time) bool {
	return p.dentro(agora, janelaReadyz)
}

// tolerado informa se há Ping bem-sucedido dentro de toleranciaReadyz em
// agora; vale só para quem chega com outro Ping em andamento.
func (p *prontidao) tolerado(agora time.Time) bool {
	return p.dentro(agora, toleranciaReadyz)
}

func (p *prontidao) dentro(agora time.Time, janela time.Duration) bool {
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.temOK && !agora.Before(p.okEm) && agora.Sub(p.okEm) < janela
}

func (p *prontidao) registrar(ok bool, em time.Time) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.temOK, p.okEm = ok, em
}

// NovoHandler monta a API com a cadeia
// RequestID → Log → Recover → LimitarCorpo(64 KiB) → mux → [Auth].
// /healthz e /readyz ficam sem autenticação; as demais rotas exigem o token.
func NovoHandler(d Dependencias) http.Handler {
	a := &api{repo: d.Repo, pinger: d.Pinger, agora: d.Agora, timeout: d.TimeoutOperacao}
	a.pronto.pingando = make(chan struct{}, 1)
	if a.agora == nil {
		a.agora = time.Now
	}
	if a.timeout <= 0 {
		a.timeout = timeoutOperacaoPadrao
	}
	auth := middleware.Auth(d.Token)

	mux := http.NewServeMux()
	mux.HandleFunc("GET /healthz", a.healthz)
	mux.HandleFunc("GET /readyz", a.readyz)
	mux.Handle("POST /livros", auth(http.HandlerFunc(a.criarLivro)))
	mux.Handle("POST /usuarios", auth(http.HandlerFunc(a.criarUsuario)))
	mux.Handle("POST /livros/{id}/emprestimo", auth(http.HandlerFunc(a.abrirEmprestimo)))
	mux.Handle("POST /livros/{id}/devolucao", auth(http.HandlerFunc(a.devolver)))
	mux.Handle("GET /livros/{id}/emprestimo", auth(http.HandlerFunc(a.consultarEmprestimo)))

	return middleware.RequestID(
		middleware.Log(d.Logger)(
			middleware.Recover(
				middleware.LimitarCorpo(limiteCorpo)(mux))))
}

type corpoStatus struct {
	Status string `json:"status"`
}

func (a *api) healthz(w http.ResponseWriter, _ *http.Request) {
	responder(w, http.StatusOK, corpoStatus{Status: "ok"})
}

// readyz responde 200 se o banco responde ao Ping. Um Ping bem-sucedido vale
// por janelaReadyz. Só um Ping roda por vez: quem chega com um Ping em
// andamento não espera nem ocupa outra conexão do pool; recebe 200 se o último
// sucesso estiver dentro de toleranciaReadyz e 503 indisponivel na hora caso
// contrário (ou sem sucesso anterior).
func (a *api) readyz(w http.ResponseWriter, r *http.Request) {
	if a.pinger == nil {
		falhar(w, r, codIndisponivel)
		return
	}
	if a.pronto.recente(a.agora()) {
		responder(w, http.StatusOK, corpoStatus{Status: "ok"})
		return
	}
	select {
	case a.pronto.pingando <- struct{}{}:
		defer func() { <-a.pronto.pingando }()
	default:
		if a.pronto.tolerado(a.agora()) {
			responder(w, http.StatusOK, corpoStatus{Status: "ok"})
			return
		}
		falhar(w, r, codIndisponivel)
		return
	}
	// Outro Ping pode ter terminado entre a consulta ao cache e o semáforo.
	if a.pronto.recente(a.agora()) {
		responder(w, http.StatusOK, corpoStatus{Status: "ok"})
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), prazoReadyz)
	defer cancel()
	if err := a.pinger.Ping(ctx); err != nil {
		a.pronto.registrar(false, time.Time{})
		falhar(w, r, codIndisponivel)
		return
	}
	a.pronto.registrar(true, a.agora())
	responder(w, http.StatusOK, corpoStatus{Status: "ok"})
}

// Corpos de entrada.
type entradaLivro struct {
	Titulo string `json:"titulo"`
}

type entradaUsuario struct {
	Nome string `json:"nome"`
}

type entradaEmprestimo struct {
	UsuarioID int64 `json:"usuario_id"`
}

// Corpos de saída.
type saidaLivro struct {
	ID     int64  `json:"id"`
	Titulo string `json:"titulo"`
}

type saidaUsuario struct {
	ID   int64  `json:"id"`
	Nome string `json:"nome"`
}

// saidaEmprestimo é o DTO de empréstimo. Instantes em RFC 3339 UTC, o
// vencimento como data civil e a multa como string com duas casas.
type saidaEmprestimo struct {
	ID         int64   `json:"id"`
	LivroID    int64   `json:"livro_id"`
	UsuarioID  int64   `json:"usuario_id"`
	Inicio     string  `json:"inicio"`
	Vencimento string  `json:"vencimento"`
	Devolucao  *string `json:"devolucao"`
	Multa      string  `json:"multa"`
}

func (a *api) criarLivro(w http.ResponseWriter, r *http.Request) {
	in, cod := lerCorpo[entradaLivro](r.Body)
	if cod != "" {
		falhar(w, r, cod)
		return
	}
	if strings.ContainsRune(in.Titulo, 0) {
		falhar(w, r, codTextoInvalido)
		return
	}
	livro, err := domain.NovoLivro(in.Titulo)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
	defer cancel()
	got, err := a.repo.InserirLivro(ctx, livro)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	responder(w, http.StatusCreated, saidaLivro{ID: got.ID, Titulo: got.Titulo})
}

func (a *api) criarUsuario(w http.ResponseWriter, r *http.Request) {
	in, cod := lerCorpo[entradaUsuario](r.Body)
	if cod != "" {
		falhar(w, r, cod)
		return
	}
	if strings.ContainsRune(in.Nome, 0) {
		falhar(w, r, codTextoInvalido)
		return
	}
	usuario, err := domain.NovoUsuario(in.Nome)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
	defer cancel()
	got, err := a.repo.InserirUsuario(ctx, usuario)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	responder(w, http.StatusCreated, saidaUsuario{ID: got.ID, Nome: got.Nome})
}

func (a *api) abrirEmprestimo(w http.ResponseWriter, r *http.Request) {
	livroID, ok := parseID(r.PathValue("id"))
	if !ok {
		falhar(w, r, codIDInvalido)
		return
	}
	in, cod := lerCorpo[entradaEmprestimo](r.Body)
	if cod != "" {
		falhar(w, r, cod)
		return
	}
	agora := a.agora()
	novo, err := domain.NovoEmprestimo(livroID, in.UsuarioID, agora)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
	defer cancel()
	got, err := a.repo.AbrirEmprestimo(ctx, novo)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	responderEmprestimo(w, r, http.StatusCreated, got, agora)
}

func (a *api) consultarEmprestimo(w http.ResponseWriter, r *http.Request) {
	livroID, ok := parseID(r.PathValue("id"))
	if !ok {
		falhar(w, r, codIDInvalido)
		return
	}
	agora := a.agora()
	ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
	defer cancel()
	got, err := a.repo.BuscarEmprestimoAbertoPorLivro(ctx, livroID)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	responderEmprestimo(w, r, http.StatusOK, got, agora)
}

func (a *api) devolver(w http.ResponseWriter, r *http.Request) {
	livroID, ok := parseID(r.PathValue("id"))
	if !ok {
		falhar(w, r, codIDInvalido)
		return
	}
	if cod := exigirCorpoVazio(r.Body); cod != "" {
		falhar(w, r, cod)
		return
	}
	agora := a.agora()
	ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
	defer cancel()
	e, err := a.repo.BuscarEmprestimoAbertoPorLivro(ctx, livroID)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	if err := e.Devolver(agora); err != nil {
		falharErro(w, r, err)
		return
	}
	got, err := a.repo.RegistrarDevolucao(ctx, e)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	responderEmprestimo(w, r, http.StatusOK, got, agora)
}

// responderEmprestimo calcula a multa com ref = agora (ignorada se o
// empréstimo já foi devolvido) e escreve o DTO.
func responderEmprestimo(w http.ResponseWriter, r *http.Request, status int, e domain.Emprestimo, agora time.Time) {
	multa, err := e.Multa(agora)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	out := saidaEmprestimo{
		ID:         e.ID(),
		LivroID:    e.LivroID(),
		UsuarioID:  e.UsuarioID(),
		Inicio:     e.Inicio().UTC().Format(time.RFC3339),
		Vencimento: e.Vencimento().Format("2006-01-02"),
		Multa:      multa.StringFixed(2),
	}
	if !e.Aberto() {
		d := e.Devolucao().UTC().Format(time.RFC3339)
		out.Devolucao = &d
	}
	responder(w, status, out)
}

// responder escreve v em JSON com status.
func responder(w http.ResponseWriter, status int, v any) {
	middleware.CabecalhosJSON(w)
	w.WriteHeader(status)
	// Falha de escrita é do cliente (conexão caída); não há o que fazer.
	_ = json.NewEncoder(w).Encode(v)
}

// parseID aceita o que strconv.ParseInt(s, 10, 64) aceita, se > 0.
func parseID(s string) (int64, bool) {
	n, err := strconv.ParseInt(s, 10, 64)
	return n, err == nil && n > 0
}

// lerCorpo decodifica exatamente um objeto JSON em T, recusando campos
// desconhecidos e qualquer dado depois do objeto (salvo espaço). Devolve o
// código de erro ("" em sucesso): corpo_muito_grande se o limite de corpo
// estourar; corpo_invalido nos demais casos.
func lerCorpo[T any](body io.Reader) (T, string) {
	var zero T
	if body == nil {
		return zero, codCorpoInvalido
	}
	dec := json.NewDecoder(body)
	dec.DisallowUnknownFields()
	var p *T
	if err := dec.Decode(&p); err != nil {
		return zero, codigoLeitura(err)
	}
	if p == nil { // null no topo
		return zero, codCorpoInvalido
	}
	if _, err := dec.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return zero, codCorpoInvalido
		}
		return zero, codigoLeitura(err)
	}
	return *p, ""
}

// exigirCorpoVazio recusa qualquer byte de corpo.
func exigirCorpoVazio(body io.Reader) string {
	if body == nil {
		return ""
	}
	var b [1]byte
	n, err := io.ReadFull(body, b[:])
	if n > 0 {
		return codCorpoInvalido
	}
	if err != nil && !errors.Is(err, io.EOF) {
		return codigoLeitura(err)
	}
	return ""
}

// codigoLeitura classifica um erro de leitura/decodificação do corpo.
func codigoLeitura(err error) string {
	var mbe *http.MaxBytesError
	if errors.As(err, &mbe) {
		return codCorpoMuitoGrande
	}
	return codCorpoInvalido
}
