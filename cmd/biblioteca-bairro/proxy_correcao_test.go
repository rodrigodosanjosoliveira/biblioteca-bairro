package main

import (
	"bufio"
	"encoding/binary"
	"io"
	"net"
	"net/url"
	"os"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// proxyPG é um proxy TCP mínimo entre o CLI e o PostgreSQL de teste. Existe
// para tornar reprodutíveis duas situações que dependem de tempo ou de bytes
// que o servidor real não produz sob demanda:
//
//   - atrasar novas conexões: o CancelRequest que o pgx envia quando um
//     contexto é cancelado no meio de um Exec sai por uma conexão nova, aberta
//     numa goroutine (asyncClose). Com o atraso, quem não espera a limpeza
//     assíncrona perde a corrida de forma determinística, não por sorte;
//   - segurar novas conexões sem encaminhá-las: a limpeza assíncrona trava, e o
//     CLI tem de continuar limitado pelo prazo de fechamento;
//   - trocar a tag "COMMIT" por "ROLLBACK" no primeiro CommandComplete depois do
//     INSERT em migracao_aplicada: o servidor real só devolve essa tag num COMMIT
//     de transação abortada, que o executor já recusa antes.
//
// Só entende PostgreSQL sem TLS (a DSN do proxy força sslmode=disable): depois
// do StartupMessage, cada mensagem do servidor tem tipo, tamanho e corpo.
type proxyPG struct {
	ln   net.Listener
	alvo string
	done chan struct{}

	atrasoNovas      atomic.Int64 // ns de espera antes de atender cada conexão nova
	segurarNovas     atomic.Bool  // conexões novas ficam abertas, sem encaminhar
	reescreverCommit atomic.Bool
	reescritos       atomic.Int32

	mu    sync.Mutex
	conns []net.Conn
	wg    sync.WaitGroup
}

// novoProxyPG sobe o proxy diante do banco de dsn e devolve também a DSN que
// passa por ele (mesmo usuário, banco e parâmetros, com sslmode=disable).
func novoProxyPG(t *testing.T, dsn string) (*proxyPG, string) {
	t.Helper()
	cc, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("interpretar a DSN do teste: falhou")
	}
	if strings.HasPrefix(cc.Host, "/") {
		const msg = "o teste usa um proxy TCP e a %s aponta para socket unix"
		if os.Getenv(envBancoObrigatorio) == "1" {
			t.Fatalf(msg, envURLBanco)
		}
		t.Skipf(msg, envURLBanco)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("abrir o proxy: %v", err)
	}
	p := &proxyPG{ln: ln, alvo: net.JoinHostPort(cc.Host, strconv.Itoa(int(cc.Port))), done: make(chan struct{})}
	p.wg.Add(1)
	go p.aceitar()
	t.Cleanup(p.fechar)
	return p, dsnPeloProxy(t, dsn, ln.Addr().String())
}

// dsnPeloProxy troca host e porta de dsn (URL ou chave=valor) por addr e força
// sslmode=disable.
func dsnPeloProxy(t *testing.T, dsn, addr string) string {
	t.Helper()
	if strings.Contains(dsn, "://") {
		u, err := url.Parse(dsn)
		if err != nil {
			t.Fatalf("interpretar %s: falhou", envURLBanco)
		}
		u.Host = addr
		q := u.Query()
		q.Set("sslmode", "disable")
		u.RawQuery = q.Encode()
		return u.String()
	}
	host, port, err := net.SplitHostPort(addr)
	if err != nil {
		t.Fatalf("endereço do proxy: %v", err)
	}
	return dsn + " host=" + host + " port=" + port + " sslmode=disable"
}

func (p *proxyPG) fechar() {
	close(p.done)
	_ = p.ln.Close()
	p.mu.Lock()
	for _, c := range p.conns {
		_ = c.Close()
	}
	p.mu.Unlock()
	p.wg.Wait()
}

func (p *proxyPG) guardar(c net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conns = append(p.conns, c)
}

func (p *proxyPG) aceitar() {
	defer p.wg.Done()
	for {
		c, err := p.ln.Accept()
		if err != nil {
			return
		}
		p.guardar(c)
		p.wg.Add(1)
		go p.atender(c)
	}
}

func (p *proxyPG) atender(cli net.Conn) {
	defer p.wg.Done()
	defer cli.Close()

	if d := time.Duration(p.atrasoNovas.Load()); d > 0 {
		select {
		case <-time.After(d):
		case <-p.done:
			return
		}
	}
	if p.segurarNovas.Load() {
		<-p.done
		return
	}
	srv, err := net.DialTimeout("tcp", p.alvo, 5*time.Second)
	if err != nil {
		return
	}
	p.guardar(srv)
	defer srv.Close()

	var armado atomic.Bool
	fim := make(chan struct{}, 2)
	go func() { p.clienteParaServidor(srv, cli, &armado); fim <- struct{}{} }()
	go func() { p.servidorParaCliente(cli, srv, &armado); fim <- struct{}{} }()
	<-fim
	// Um lado acabou: fecha os dois (o defer) e espera o outro soltar.
	_ = cli.Close()
	_ = srv.Close()
	<-fim
}

// clienteParaServidor copia os bytes e arma a reescrita ao ver o INSERT de
// registro (que o pgx envia como Parse, com o texto do SQL).
func (p *proxyPG) clienteParaServidor(dst, src net.Conn, armado *atomic.Bool) {
	// Aceita o INSERT com a tabela qualificada pelo schema ("schema"."migracao_aplicada").
	marca := regexp.MustCompile(`INSERT INTO [^(]{0,120}migracao_aplicada`)
	const janelaMax = 160
	buf := make([]byte, 32<<10)
	var cauda []byte
	for {
		n, err := src.Read(buf)
		if n > 0 {
			if p.reescreverCommit.Load() {
				janela := append(append([]byte(nil), cauda...), buf[:n]...)
				if marca.Match(janela) {
					armado.Store(true)
				}
				ini := max(0, len(janela)-janelaMax)
				cauda = append([]byte(nil), janela[ini:]...)
			}
			if _, werr := dst.Write(buf[:n]); werr != nil {
				return
			}
		}
		if err != nil {
			return
		}
	}
}

// servidorParaCliente repassa mensagem por mensagem e, armado, troca a tag do
// primeiro CommandComplete "COMMIT" por "ROLLBACK".
func (p *proxyPG) servidorParaCliente(dst io.Writer, src io.Reader, armado *atomic.Bool) {
	r := bufio.NewReader(src)
	var cab [5]byte
	for {
		if _, err := io.ReadFull(r, cab[:]); err != nil {
			return
		}
		n := int(binary.BigEndian.Uint32(cab[1:])) - 4
		if n < 0 || n > 64<<20 {
			return
		}
		corpo := make([]byte, n)
		if _, err := io.ReadFull(r, corpo); err != nil {
			return
		}
		if cab[0] == 'C' && string(corpo) == "COMMIT\x00" && armado.Load() &&
			p.reescreverCommit.Load() && p.reescritos.CompareAndSwap(0, 1) {
			corpo = []byte("ROLLBACK\x00")
			binary.BigEndian.PutUint32(cab[1:], uint32(len(corpo)+4))
		}
		if _, err := dst.Write(cab[:]); err != nil {
			return
		}
		if _, err := dst.Write(corpo); err != nil {
			return
		}
	}
}
