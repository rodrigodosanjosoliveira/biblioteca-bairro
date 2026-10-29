// Package middleware tem os middlewares HTTP da API: request ID, log de
// requisição, recuperação de panic, limite de corpo e autenticação por bearer
// token.
//
// Ordem de produção, de fora para dentro:
//
//	RequestID → Log → Recover → LimitarCorpo → mux → [Auth]
//
// Log, Recover e LimitarCorpo repassam o mesmo *http.Request que recebem (sem
// cópia), para que o r.Pattern preenchido pelo http.ServeMux fique visível ao
// Log depois que o mux volta.
package middleware

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"
)

// Códigos e mensagens de erro produzidos pelos próprios middlewares.
const (
	codigoNaoAutorizado   = "nao_autorizado"
	mensagemNaoAutorizado = "credencial ausente ou inválida"
	codigoErroInterno     = "erro_interno"
	mensagemErroInterno   = "erro interno"
)

// politicaCSP é o Content-Security-Policy das respostas JSON: a API não
// serve conteúdo ativo nem pode ser enquadrada.
const politicaCSP = "default-src 'none'; frame-ancestors 'none'"

// causaAbortado é a causa registrada no log quando a requisição termina com
// panic repropagado (http.ErrAbortHandler).
const causaAbortado = "abortado"

type chaveCtx int

const (
	chaveRequestID chaveCtx = iota
	chaveAnotacao
)

// anotacao acumula os atributos que o handler acrescenta à linha de log da
// requisição (codigo, causa, pg_code...).
type anotacao struct {
	mu    sync.Mutex
	attrs []slog.Attr
}

// Anotar acrescenta attrs à linha de log da requisição de ctx. Sem o
// middleware Log na cadeia, não faz nada. Nunca passe aqui texto de erro,
// corpo, header, título, nome, DSN ou token.
func Anotar(ctx context.Context, attrs ...slog.Attr) {
	a, ok := ctx.Value(chaveAnotacao).(*anotacao)
	if !ok {
		return
	}
	a.mu.Lock()
	a.attrs = append(a.attrs, attrs...)
	a.mu.Unlock()
}

func (a *anotacao) lista() []slog.Attr {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]slog.Attr(nil), a.attrs...)
}

// corpoErro é o envelope {"erro":{"codigo","mensagem"}}.
type corpoErro struct {
	Erro detalheErro `json:"erro"`
}

type detalheErro struct {
	Codigo   string `json:"codigo"`
	Mensagem string `json:"mensagem"`
}

// CabecalhosJSON define Content-Type JSON, X-Content-Type-Options: nosniff e
// Content-Security-Policy: default-src 'none'; frame-ancestors 'none'.
func CabecalhosJSON(w http.ResponseWriter) {
	h := w.Header()
	h.Set("Content-Type", "application/json; charset=utf-8")
	h.Set("X-Content-Type-Options", "nosniff")
	h.Set("Content-Security-Policy", politicaCSP)
}

// ResponderErro escreve {"erro":{"codigo","mensagem"}} com status e anota
// codigo (e attrs) na linha de log da requisição. Em 413 acrescenta
// Connection: close: o resto do corpo não foi lido e o gravador da cadeia
// esconde do net/http o aviso do MaxBytesReader que fecharia a conexão.
func ResponderErro(w http.ResponseWriter, r *http.Request, status int, codigo, mensagem string, attrs ...slog.Attr) {
	Anotar(r.Context(), append([]slog.Attr{slog.String("codigo", codigo)}, attrs...)...)
	CabecalhosJSON(w)
	if status == http.StatusRequestEntityTooLarge {
		w.Header().Set("Connection", "close")
	}
	w.WriteHeader(status)
	// Falha de escrita é do cliente (conexão caída); não há o que fazer.
	_ = json.NewEncoder(w).Encode(corpoErro{Erro: detalheErro{Codigo: codigo, Mensagem: mensagem}})
}

// RequestID gera um ID aleatório de 16 bytes (32 hex minúsculos) por
// requisição, o põe no header X-Request-ID da resposta e no contexto. O
// X-Request-ID recebido é ignorado.
func RequestID(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		id := novoRequestID()
		w.Header().Set("X-Request-ID", id)
		ctx := context.WithValue(r.Context(), chaveRequestID, id)
		next.ServeHTTP(w, r.WithContext(ctx))
	})
}

func novoRequestID() string {
	var b [16]byte
	// crypto/rand.Read não falha (Go 1.24+): em falha do sistema, aborta.
	_, _ = rand.Read(b[:])
	return hex.EncodeToString(b[:])
}

// gravador captura o status escrito na resposta.
type gravador struct {
	http.ResponseWriter
	status   int
	escreveu bool
}

func (g *gravador) WriteHeader(status int) {
	if !g.escreveu {
		g.status, g.escreveu = status, true
	}
	g.ResponseWriter.WriteHeader(status)
}

func (g *gravador) Write(b []byte) (int, error) {
	if !g.escreveu {
		g.status, g.escreveu = http.StatusOK, true
	}
	return g.ResponseWriter.Write(b)
}

// Unwrap expõe o ResponseWriter original a http.ResponseController.
func (g *gravador) Unwrap() http.ResponseWriter { return g.ResponseWriter }

// Log escreve uma linha por requisição com metodo, rota (r.Pattern), status,
// duracao_ms e request_id, mais o que o handler anotar com Anotar ou
// ResponderErro. Não registra URL, query, headers nem corpo.
//
// Se um panic atravessar a cadeia (só http.ErrAbortHandler passa pelo
// Recover), a linha é gravada em nível Error com causa=abortado e o panic é
// repropagado. O status é o que chegou a ser escrito, ou 0 se nada foi.
func Log(logger *slog.Logger) func(http.Handler) http.Handler {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			inicio := time.Now()
			an := &anotacao{}
			r = r.WithContext(context.WithValue(r.Context(), chaveAnotacao, an))
			g := &gravador{ResponseWriter: w, status: http.StatusOK}

			gravar := func(status int, nivel slog.Level, extra ...slog.Attr) {
				id, _ := r.Context().Value(chaveRequestID).(string)
				attrs := []slog.Attr{
					slog.String("metodo", r.Method),
					slog.String("rota", r.Pattern),
					slog.Int("status", status),
					slog.Int64("duracao_ms", time.Since(inicio).Milliseconds()),
					slog.String("request_id", id),
				}
				attrs = append(attrs, an.lista()...)
				attrs = append(attrs, extra...)
				logger.LogAttrs(r.Context(), nivel, "requisicao", attrs...)
			}

			defer func() {
				v := recover()
				if v == nil {
					return
				}
				status := 0
				if g.escreveu {
					status = g.status
				}
				// Nunca registra o valor do panic.
				gravar(status, slog.LevelError, slog.String("causa", causaAbortado))
				panic(v)
			}()
			next.ServeHTTP(g, r)

			nivel := slog.LevelInfo
			if g.status >= http.StatusInternalServerError {
				nivel = slog.LevelError
			}
			gravar(g.status, nivel)
		})
	}
}

// Recover transforma panic em 500 erro_interno e anota panic=true no log, sem
// o valor do panic nem o stack. Valor que satisfaz errors.Is(v,
// http.ErrAbortHandler) é repropagado como http.ErrAbortHandler puro, sem
// escrever resposta nem anotar panic. Se a resposta já começou a ser escrita
// (status ou corpo), não há como trocá-la por um 500: anota panic=true e o
// panic vira http.ErrAbortHandler, e o net/http derruba a conexão.
func Recover(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		g := &gravador{ResponseWriter: w, status: http.StatusOK}
		defer func() {
			v := recover()
			if v == nil {
				return
			}
			if err, ok := v.(error); ok && errors.Is(err, http.ErrAbortHandler) {
				// Repropaga o sentinela puro: um ErrAbortHandler embrulhado
				// faria o net/http logar o valor e o stack.
				panic(http.ErrAbortHandler)
			}
			if g.escreveu {
				Anotar(r.Context(), slog.Bool("panic", true))
				panic(http.ErrAbortHandler)
			}
			ResponderErro(w, r, http.StatusInternalServerError, codigoErroInterno, mensagemErroInterno, slog.Bool("panic", true))
		}()
		next.ServeHTTP(g, r)
	})
}

// LimitarCorpo limita o corpo da requisição a max bytes com
// http.MaxBytesReader; a leitura além do limite devolve *http.MaxBytesError.
// Troca r.Body no próprio request (ver o comentário do pacote).
func LimitarCorpo(max int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, max)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Auth exige "Authorization: Bearer <token>" (esquema sem diferenciar
// maiúsculas). A comparação é em tempo constante sobre o SHA-256 dos dois
// lados. Token configurado vazio recusa tudo. Toda resposta passa a ter
// Cache-Control: no-store; a recusa é 401 nao_autorizado com
// WWW-Authenticate: Bearer, sempre o mesmo corpo.
func Auth(token string) func(http.Handler) http.Handler {
	configurado := token != ""
	esperado := sha256.Sum256([]byte(token))
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			w.Header().Set("Cache-Control", "no-store")
			if !configurado || !autorizado(r, esperado) {
				w.Header().Set("WWW-Authenticate", "Bearer")
				ResponderErro(w, r, http.StatusUnauthorized, codigoNaoAutorizado, mensagemNaoAutorizado)
				return
			}
			next.ServeHTTP(w, r)
		})
	}
}

// autorizado confere o header Authorization (exatamente um) contra o hash
// esperado.
func autorizado(r *http.Request, esperado [sha256.Size]byte) bool {
	vals := r.Header.Values("Authorization")
	if len(vals) != 1 {
		return false
	}
	esquema, credencial, ok := strings.Cut(vals[0], " ")
	if !ok || credencial == "" || !strings.EqualFold(esquema, "Bearer") {
		return false
	}
	recebido := sha256.Sum256([]byte(credencial))
	return subtle.ConstantTimeCompare(recebido[:], esperado[:]) == 1
}
