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
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http/middleware"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/storage"
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
	RegistrarRenovacao(ctx context.Context, e domain.Emprestimo) (domain.Emprestimo, error)
	BuscarLivro(ctx context.Context, id int64) (domain.Livro, error)
	ListarLivros(ctx context.Context, apos int64, limite int) ([]domain.Livro, bool, error)
	BuscarEmprestimo(ctx context.Context, id int64) (domain.Emprestimo, error)
	BuscarUsuario(ctx context.Context, id int64) (domain.Usuario, error)
	ListarUsuarios(ctx context.Context, apos int64, limite int) ([]domain.Usuario, bool, error)
	ListarEmprestimos(ctx context.Context, apos int64, limite int) ([]domain.Emprestimo, bool, error)
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
	mux.Handle("POST /livros/{id}/renovacao", auth(http.HandlerFunc(a.renovar)))
	mux.Handle("GET /livros/{id}/emprestimo", auth(http.HandlerFunc(a.consultarEmprestimo)))
	mux.Handle("GET /livros", auth(http.HandlerFunc(a.listarLivros)))
	mux.Handle("GET /livros/{id}", auth(http.HandlerFunc(a.obterLivro)))
	mux.Handle("GET /emprestimos/{id}", auth(http.HandlerFunc(a.obterEmprestimo)))
	mux.Handle("GET /usuarios", auth(http.HandlerFunc(a.listarUsuarios)))
	mux.Handle("GET /usuarios/{id}", auth(http.HandlerFunc(a.obterUsuario)))
	mux.Handle("GET /emprestimos", auth(http.HandlerFunc(a.listarEmprestimos)))

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

// saidaListaLivros é uma página de GET /livros. Proximo é o valor de apos
// para a página seguinte (id do último item), null na última página.
type saidaListaLivros struct {
	Itens   []saidaLivro `json:"itens"`
	Proximo *int64       `json:"proximo"`
}

// saidaListaUsuarios é uma página de GET /usuarios, no contrato de
// saidaListaLivros.
type saidaListaUsuarios struct {
	Itens   []saidaUsuario `json:"itens"`
	Proximo *int64         `json:"proximo"`
}

// saidaListaEmprestimos é uma página de GET /emprestimos, no contrato de
// saidaListaLivros.
type saidaListaEmprestimos struct {
	Itens   []saidaEmprestimo `json:"itens"`
	Proximo *int64            `json:"proximo"`
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
	Renovacoes int     `json:"renovacoes"`
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

func (a *api) listarLivros(w http.ResponseWriter, r *http.Request) {
	apos, limite, ok := lerParametrosListagem(r.URL.RawQuery)
	if !ok {
		falhar(w, r, codParametroInvalido)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
	defer cancel()
	livros, temMais, err := a.repo.ListarLivros(ctx, apos, limite)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	out := saidaListaLivros{Itens: make([]saidaLivro, 0, len(livros))}
	for _, l := range livros {
		out.Itens = append(out.Itens, saidaLivro{ID: l.ID, Titulo: l.Titulo})
	}
	if temMais && len(livros) > 0 {
		p := livros[len(livros)-1].ID
		out.Proximo = &p
	}
	responder(w, http.StatusOK, out)
}

func (a *api) listarUsuarios(w http.ResponseWriter, r *http.Request) {
	apos, limite, ok := lerParametrosListagem(r.URL.RawQuery)
	if !ok {
		falhar(w, r, codParametroInvalido)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
	defer cancel()
	usuarios, temMais, err := a.repo.ListarUsuarios(ctx, apos, limite)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	out := saidaListaUsuarios{Itens: make([]saidaUsuario, 0, len(usuarios))}
	for _, u := range usuarios {
		out.Itens = append(out.Itens, saidaUsuario{ID: u.ID, Nome: u.Nome})
	}
	if temMais && len(usuarios) > 0 {
		p := usuarios[len(usuarios)-1].ID
		out.Proximo = &p
	}
	responder(w, http.StatusOK, out)
}

func (a *api) obterUsuario(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		falhar(w, r, codIDInvalido)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
	defer cancel()
	got, err := a.repo.BuscarUsuario(ctx, id)
	if errors.Is(err, storage.ErrUsuarioNaoEncontrado) {
		// Mapeamento local: nas rotas de empréstimo o mesmo erro é
		// usuario_nao_encontrado (422) e continua assim em classificar.
		falhar(w, r, codUsuarioInexistente)
		return
	}
	if err != nil {
		falharErro(w, r, err)
		return
	}
	responder(w, http.StatusOK, saidaUsuario{ID: got.ID, Nome: got.Nome})
}

// listarEmprestimos lê o relógio uma única vez, antes do repositório, e
// calcula a multa de todos os itens com esse instante. Erro em qualquer item
// responde 500 sem página parcial.
func (a *api) listarEmprestimos(w http.ResponseWriter, r *http.Request) {
	apos, limite, ok := lerParametrosListagem(r.URL.RawQuery)
	if !ok {
		falhar(w, r, codParametroInvalido)
		return
	}
	agora := a.agora()
	ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
	defer cancel()
	emprestimos, temMais, err := a.repo.ListarEmprestimos(ctx, apos, limite)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	out := saidaListaEmprestimos{Itens: make([]saidaEmprestimo, 0, len(emprestimos))}
	for _, e := range emprestimos {
		item, err := montarSaidaEmprestimo(e, agora)
		if err != nil {
			falharErro(w, r, err)
			return
		}
		out.Itens = append(out.Itens, item)
	}
	if temMais && len(emprestimos) > 0 {
		p := emprestimos[len(emprestimos)-1].ID()
		out.Proximo = &p
	}
	responder(w, http.StatusOK, out)
}

func (a *api) obterLivro(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		falhar(w, r, codIDInvalido)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
	defer cancel()
	got, err := a.repo.BuscarLivro(ctx, id)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	responder(w, http.StatusOK, saidaLivro{ID: got.ID, Titulo: got.Titulo})
}

func (a *api) obterEmprestimo(w http.ResponseWriter, r *http.Request) {
	id, ok := parseID(r.PathValue("id"))
	if !ok {
		falhar(w, r, codIDInvalido)
		return
	}
	agora := a.agora()
	ctx, cancel := context.WithTimeout(r.Context(), a.timeout)
	defer cancel()
	got, err := a.repo.BuscarEmprestimo(ctx, id)
	if errors.Is(err, storage.ErrEmprestimoNaoEncontrado) {
		// Mapeamento local: nas rotas por livro o mesmo erro significa
		// "nenhum empréstimo aberto" e continua assim em classificar.
		falhar(w, r, codEmprestimoNaoEncontrado)
		return
	}
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

func (a *api) renovar(w http.ResponseWriter, r *http.Request) {
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
	if err := e.Renovar(agora); err != nil {
		falharErro(w, r, err)
		return
	}
	got, err := a.repo.RegistrarRenovacao(ctx, e)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	responderEmprestimo(w, r, http.StatusOK, got, agora)
}

// responderEmprestimo calcula a multa com ref = agora (ignorada se o
// empréstimo já foi devolvido) e escreve o DTO.
func responderEmprestimo(w http.ResponseWriter, r *http.Request, status int, e domain.Emprestimo, agora time.Time) {
	out, err := montarSaidaEmprestimo(e, agora)
	if err != nil {
		falharErro(w, r, err)
		return
	}
	responder(w, status, out)
}

// montarSaidaEmprestimo monta o DTO de e com a multa calculada em ref = agora
// (ignorada se o empréstimo já foi devolvido).
func montarSaidaEmprestimo(e domain.Emprestimo, agora time.Time) (saidaEmprestimo, error) {
	multa, err := e.Multa(agora)
	if err != nil {
		return saidaEmprestimo{}, err
	}
	out := saidaEmprestimo{
		ID:         e.ID(),
		LivroID:    e.LivroID(),
		UsuarioID:  e.UsuarioID(),
		Inicio:     e.Inicio().UTC().Format(time.RFC3339),
		Vencimento: e.Vencimento().Format("2006-01-02"),
		Multa:      multa.StringFixed(2),
		Renovacoes: e.Renovacoes(),
	}
	if !e.Aberto() {
		d := e.Devolucao().UTC().Format(time.RFC3339)
		out.Devolucao = &d
	}
	return out, nil
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

// limitePadraoListagem é o limite das listagens (GET /livros, /usuarios e
// /emprestimos) sem o parâmetro limite.
const limitePadraoListagem = 20

// lerParametrosListagem valida a query das listagens (GET /livros, /usuarios
// e /emprestimos): só as chaves limite e apos, cada uma no máximo uma vez.
// limite é inteiro decimal em 1..storage.LimiteListagem (padrão 20); apos
// segue a gramática de
// parseID (padrão 0, início). Qualquer desvio devolve ok = false.
func lerParametrosListagem(rawQuery string) (apos int64, limite int, ok bool) {
	vals, err := url.ParseQuery(rawQuery)
	if err != nil {
		return 0, 0, false
	}
	limite = limitePadraoListagem
	for k, vs := range vals {
		if len(vs) != 1 {
			return 0, 0, false
		}
		switch k {
		case "limite":
			n, err := strconv.ParseInt(vs[0], 10, 64)
			if err != nil || n < 1 || n > storage.LimiteListagem {
				return 0, 0, false
			}
			limite = int(n)
		case "apos":
			n, valido := parseID(vs[0])
			if !valido {
				return 0, 0, false
			}
			apos = n
		default:
			return 0, 0, false
		}
	}
	return apos, limite, true
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
