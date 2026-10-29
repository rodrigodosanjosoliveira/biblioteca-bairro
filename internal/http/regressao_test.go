package httpapi_test

import (
	"errors"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	httpapi "github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http"
)

// Regressões das revisões da DEL-006 (security-review e code-reviewer).

const politicaCSP = "default-src 'none'; frame-ancestors 'none'"

// TestReadyzCacheSequencial: um Ping bem-sucedido vale por 1s no relógio
// injetado; falha não fica em cache; relógio que volta invalida o cache.
func TestReadyzCacheSequencial(t *testing.T) {
	type passo struct {
		avanco  time.Duration // somado a f.agora antes da chamada
		errPing error
		status  int
		pings   int // total acumulado de Pings depois da chamada
	}
	tests := []struct {
		name   string
		passos []passo
	}{
		{name: "chamadas na janela reaproveitam o Ping", passos: []passo{
			{status: 200, pings: 1},
			{status: 200, pings: 1},
			{avanco: 500 * time.Millisecond, status: 200, pings: 1},
			{avanco: 499 * time.Millisecond, status: 200, pings: 1},
			{avanco: time.Millisecond, status: 200, pings: 2},
			{status: 200, pings: 2},
		}},
		{name: "falha não fica em cache", passos: []passo{
			{errPing: errors.New("fora"), status: 503, pings: 1},
			{errPing: errors.New("fora"), status: 503, pings: 2},
			{status: 200, pings: 3},
			{status: 200, pings: 3},
		}},
		{name: "falha depois de sucesso vencido derruba o cache", passos: []passo{
			{status: 200, pings: 1},
			{avanco: time.Second, errPing: errors.New("fora"), status: 503, pings: 2},
			{status: 200, pings: 3},
		}},
		{name: "relógio que volta invalida o cache", passos: []passo{
			{status: 200, pings: 1},
			{avanco: -time.Millisecond, status: 200, pings: 2},
			{status: 200, pings: 2},
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			for i, p := range tt.passos {
				f.agora = f.agora.Add(p.avanco)
				f.pinger.err = p.errPing
				rec := f.do(http.MethodGet, "/readyz", "", false)
				if rec.Code != p.status {
					t.Fatalf("passo %d: status = %d, want %d", i, rec.Code, p.status)
				}
				if p.status == 503 {
					checarErro(t, rec, 503, "indisponivel", false)
				}
				if got := f.pinger.nPings(); got != p.pings {
					t.Fatalf("passo %d: %d Pings, want %d", i, got, p.pings)
				}
			}
		})
	}
}

// TestReadyzUmPingPorVez: com um Ping em andamento, as chamadas simultâneas
// recebem 503 na hora, sem esperar o Ping nem disparar outro; depois que ele
// termina com sucesso, as chamadas dentro da janela respondem 200 do cache.
// Ao todo, 1 Ping.
func TestReadyzUmPingPorVez(t *testing.T) {
	const n = 20
	tests := []struct {
		name string
		// statusDurante é o status das chamadas feitas com o Ping preso.
		statusDurante int
		// statusDepois é o status das chamadas feitas depois do Ping, na janela.
		statusDepois int
		pings        int
	}{
		{name: "N simultâneas na janela geram 1 Ping", statusDurante: 503, statusDepois: 200, pings: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			f.pinger.entrou = make(chan struct{}, 1)
			f.pinger.liberar = make(chan struct{})
			liberarPing := liberadorPing(t, f.pinger)
			h := f.handler()

			primeiro := make(chan int, 1)
			go func() { primeiro <- f.do(http.MethodGet, "/readyz", "", false).Code }()
			select {
			case <-f.pinger.entrou:
			case <-time.After(5 * time.Second):
				t.Fatal("o primeiro Ping não começou")
			}

			disparar := func() []int {
				var wg sync.WaitGroup
				codigos := make([]int, n)
				for i := range n {
					wg.Add(1)
					go func() {
						defer wg.Done()
						rec := doSemFixture(h, "/readyz")
						codigos[i] = rec.Code
					}()
				}
				fim := make(chan struct{})
				go func() { wg.Wait(); close(fim) }()
				select {
				case <-fim:
				case <-time.After(5 * time.Second):
					t.Fatal("chamadas ao /readyz bloquearam esperando o Ping em andamento")
				}
				return codigos
			}

			for i, c := range disparar() {
				if c != tt.statusDurante {
					t.Errorf("chamada %d com Ping em andamento: status = %d, want %d", i, c, tt.statusDurante)
				}
			}
			liberarPing()
			if c := <-primeiro; c != 200 {
				t.Fatalf("primeira chamada: status = %d, want 200", c)
			}
			for i, c := range disparar() {
				if c != tt.statusDepois {
					t.Errorf("chamada %d depois do Ping: status = %d, want %d", i, c, tt.statusDepois)
				}
			}
			if got := f.pinger.nPings(); got != tt.pings {
				t.Errorf("%d Pings, want %d", got, tt.pings)
			}
		})
	}
}

// liberadorPing devolve a função que solta o Ping preso em p.liberar. Ela fecha
// o canal uma vez só, e t.Cleanup também a chama: um t.Fatal antes da
// liberação não deixa a goroutine do Ping presa.
func liberadorPing(t *testing.T, p *pingerFake) func() {
	t.Helper()
	var once sync.Once
	liberar := func() { once.Do(func() { close(p.liberar) }) }
	t.Cleanup(liberar)
	return liberar
}

// TestReadyzToleranciaDuranteRenovacao: com o cache vencido e um Ping de
// renovação em andamento, quem chega recebe 200 se o último sucesso estiver a
// menos de janelaReadyz + prazoReadyz (3s) no relógio injetado, e 503 na hora
// caso contrário; em ambos os casos só a renovação pinga.
func TestReadyzToleranciaDuranteRenovacao(t *testing.T) {
	const n = 20
	tests := []struct {
		name          string
		idade         time.Duration // tempo desde o último sucesso no momento da renovação
		statusDurante int
	}{
		{name: "cache recém-vencido responde 200", idade: time.Second, statusDurante: 200},
		{name: "no limite inferior da tolerância responde 200", idade: 3*time.Second - time.Millisecond, statusDurante: 200},
		{name: "no limite da tolerância responde 503", idade: 3 * time.Second, statusDurante: 503},
		{name: "sucesso antigo responde 503", idade: time.Minute, statusDurante: 503},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			h := f.handler()
			// Sucesso anterior, sem Ping preso.
			if c := f.do(http.MethodGet, "/readyz", "", false).Code; c != 200 {
				t.Fatalf("primeiro readyz: status = %d, want 200", c)
			}
			if got := f.pinger.nPings(); got != 1 {
				t.Fatalf("%d Pings depois do primeiro readyz, want 1", got)
			}
			// O relógio e os canais mudam antes de qualquer goroutine nova; a
			// partir daqui ninguém os escreve.
			f.agora = f.agora.Add(tt.idade)
			f.pinger.entrou = make(chan struct{}, 1)
			f.pinger.liberar = make(chan struct{})
			liberarPing := liberadorPing(t, f.pinger)

			renovacao := make(chan int, 1)
			go func() { renovacao <- doSemFixture(h, "/readyz").Code }()
			select {
			case <-f.pinger.entrou:
			case <-time.After(5 * time.Second):
				t.Fatal("o Ping de renovação não começou")
			}

			var wg sync.WaitGroup
			codigos := make([]int, n)
			for i := range n {
				wg.Add(1)
				go func() {
					defer wg.Done()
					codigos[i] = doSemFixture(h, "/readyz").Code
				}()
			}
			fim := make(chan struct{})
			go func() { wg.Wait(); close(fim) }()
			select {
			case <-fim:
			case <-time.After(5 * time.Second):
				t.Fatal("chamadas ao /readyz bloquearam esperando o Ping em andamento")
			}
			for i, c := range codigos {
				if c != tt.statusDurante {
					t.Errorf("chamada %d durante a renovação: status = %d, want %d", i, c, tt.statusDurante)
				}
			}

			liberarPing()
			if c := <-renovacao; c != 200 {
				t.Fatalf("renovação: status = %d, want 200", c)
			}
			if got := f.pinger.nPings(); got != 2 {
				t.Errorf("%d Pings, want 2 (o inicial e só uma renovação)", got)
			}
		})
	}
}

// doSemFixture faz um GET anônimo direto no handler, sem tocar no estado da
// fixture (seguro para goroutines).
func doSemFixture(h http.Handler, path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

// TestConnectionCloseNo413: a resposta 413 fecha a conexão; as demais não.
func TestConnectionCloseNo413(t *testing.T) {
	grande := `{"titulo":"` + strings.Repeat("a", 70<<10) + `"}`
	tests := []struct {
		name   string
		path   string
		body   string
		status int
		want   string
	}{
		{name: "413 livro", path: "/livros", body: grande, status: 413, want: "close"},
		{name: "413 usuario", path: "/usuarios", body: `{"nome":"` + strings.Repeat("n", 70<<10) + `"}`, status: 413, want: "close"},
		{name: "400 não fecha", path: "/livros", body: `{"titulo":1}`, status: 400, want: ""},
		{name: "201 não fecha", path: "/livros", body: `{"titulo":"a"}`, status: 201, want: ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodPost, tt.path, tt.body, true)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if got := rec.Header().Get("Connection"); got != tt.want {
				t.Errorf("Connection = %q, want %q", got, tt.want)
			}
		})
	}
}

// TestConnectionCloseNo413ServidorReal: com o servidor de verdade, o
// cliente vê Connection: close e a conexão termina depois do 413.
func TestConnectionCloseNo413ServidorReal(t *testing.T) {
	tests := []struct {
		name      string
		tamanho   int
		status    int
		wantClose bool
	}{
		{name: "413 fecha", tamanho: 70 << 10, status: 413, wantClose: true},
		{name: "201 mantém", tamanho: 10, status: 201, wantClose: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			ln, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatalf("Listen: %v", err)
			}
			srv := httpapi.NovoServidor(ln.Addr().String(), f.handler(), nil)
			go func() { _ = srv.Serve(ln) }()
			defer srv.Close()

			body := `{"titulo":"` + strings.Repeat("a", tt.tamanho) + `"}`
			req, _ := http.NewRequest(http.MethodPost, "http://"+ln.Addr().String()+"/livros", strings.NewReader(body))
			req.Header.Set("Authorization", "Bearer "+tokenTeste)
			cli := &http.Client{Timeout: 5 * time.Second}
			resp, err := cli.Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
			if resp.StatusCode != tt.status {
				t.Fatalf("status = %d, want %d", resp.StatusCode, tt.status)
			}
			if resp.Close != tt.wantClose {
				t.Errorf("resp.Close = %v, want %v", resp.Close, tt.wantClose)
			}
		})
	}
}

// TestMaxHeaderBytes: o servidor limita os headers a 64 KiB.
func TestMaxHeaderBytes(t *testing.T) {
	srv := httpapi.NovoServidor(":0", http.NewServeMux(), nil)
	if srv.MaxHeaderBytes != 64<<10 {
		t.Fatalf("MaxHeaderBytes = %d, want %d", srv.MaxHeaderBytes, 64<<10)
	}

	tests := []struct {
		name    string
		tamanho int
		status  int
	}{
		{name: "header de 1 KiB", tamanho: 1 << 10, status: 200},
		{name: "header de 100 KiB", tamanho: 100 << 10, status: http.StatusRequestHeaderFieldsTooLarge},
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("Listen: %v", err)
	}
	f := novaFixture(t)
	srvReal := httpapi.NovoServidor(ln.Addr().String(), f.handler(), nil)
	go func() { _ = srvReal.Serve(ln) }()
	defer srvReal.Close()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			req, _ := http.NewRequest(http.MethodGet, "http://"+ln.Addr().String()+"/healthz", nil)
			req.Header.Set("X-Grande", strings.Repeat("h", tt.tamanho))
			resp, err := clienteNovo().Do(req)
			if err != nil {
				t.Fatalf("Do: %v", err)
			}
			resp.Body.Close()
			if resp.StatusCode != tt.status {
				t.Errorf("status = %d, want %d", resp.StatusCode, tt.status)
			}
		})
	}
}

// TestCSPEmRespostasJSON: toda resposta JSON leva o CSP junto com nosniff.
func TestCSPEmRespostasJSON(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		auth   bool
		setup  func(f *fixture)
		status int
	}{
		{name: "healthz", method: http.MethodGet, path: "/healthz", status: 200},
		{name: "readyz ok", method: http.MethodGet, path: "/readyz", status: 200},
		{name: "readyz 503", method: http.MethodGet, path: "/readyz", setup: func(f *fixture) { f.pinger.err = errors.New("fora") }, status: 503},
		{name: "201 livro", method: http.MethodPost, path: "/livros", body: `{"titulo":"a"}`, auth: true, status: 201},
		{name: "201 usuario", method: http.MethodPost, path: "/usuarios", body: `{"nome":"a"}`, auth: true, status: 201},
		{name: "201 emprestimo", method: http.MethodPost, path: "/livros/1/emprestimo", body: `{"usuario_id":2}`, auth: true, status: 201},
		{name: "200 consulta", method: http.MethodGet, path: "/livros/1/emprestimo", auth: true, status: 200},
		{name: "200 devolucao", method: http.MethodPost, path: "/livros/1/devolucao", auth: true, status: 200},
		{name: "401", method: http.MethodPost, path: "/livros", body: `{"titulo":"a"}`, status: 401},
		{name: "400", method: http.MethodGet, path: "/livros/x/emprestimo", auth: true, status: 400},
		{name: "413", method: http.MethodPost, path: "/livros", body: `{"titulo":"` + strings.Repeat("a", 70<<10) + `"}`, auth: true, status: 413},
		{name: "500 panic", method: http.MethodPost, path: "/livros", body: `{"titulo":"a"}`, auth: true, setup: func(f *fixture) { f.repo.panico = "x" }, status: 500},
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
			if got := rec.Header().Get("Content-Security-Policy"); got != politicaCSP {
				t.Errorf("Content-Security-Policy = %q, want %q", got, politicaCSP)
			}
			if got := rec.Header().Get("X-Content-Type-Options"); got != "nosniff" {
				t.Errorf("X-Content-Type-Options = %q, want nosniff", got)
			}
		})
	}
}

// TestLogAbortado: panic repropagado (http.ErrAbortHandler) gera a linha de
// log com causa=abortado, status 0 (nada foi escrito) e é repropagado.
func TestLogAbortado(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		rota   string
		chaves []string
		campos map[string]any
	}{
		{name: "ErrAbortHandler no repositório", method: http.MethodGet, path: "/livros/1/emprestimo", rota: "GET /livros/{id}/emprestimo",
			chaves: comChaves("causa"), campos: map[string]any{"causa": "abortado", "status": float64(0), "level": "ERROR"}},
		{name: "ErrAbortHandler no POST", method: http.MethodPost, path: "/usuarios", body: `{"nome":"a"}`, rota: "POST /usuarios",
			chaves: comChaves("causa"), campos: map[string]any{"causa": "abortado", "status": float64(0), "level": "ERROR"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			f.repo.panico = http.ErrAbortHandler
			func() {
				defer func() {
					if got := recover(); got != http.ErrAbortHandler {
						t.Fatalf("recover() = %v, want http.ErrAbortHandler", got)
					}
				}()
				f.do(tt.method, tt.path, tt.body, true)
			}()
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
			if l["rota"] != tt.rota {
				t.Errorf("rota = %v, want %q", l["rota"], tt.rota)
			}
			for k, v := range tt.campos {
				if l[k] != v {
					t.Errorf("%s = %#v, want %#v", k, l[k], v)
				}
			}
			if strings.Contains(f.logs.String(), "abort Handler") {
				t.Errorf("log contém o valor do panic: %s", f.logs.String())
			}
		})
	}
}
