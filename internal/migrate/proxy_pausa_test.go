package migrate_test

import (
	"bytes"
	"net"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// proxyPausa é um proxy TCP mínimo que segura, uma única vez, a mensagem do
// cliente que contém marca: avisa em chegou e só a encaminha ao servidor depois
// de liberar. Serve para parar o Status exatamente entre dois comandos da sua
// inspeção, de forma determinística, enquanto o teste roda outra execução
// completa por uma conexão direta. A conexão só pode ser sem TLS (o teste
// zera TLSConfig e Fallbacks do ConnConfig).
type proxyPausa struct {
	ln    net.Listener
	alvo  string
	marca []byte

	chegou  chan struct{} // recebe um valor quando a mensagem com a marca foi segurada
	liberar chan struct{} // fechado pelo teste para soltar a mensagem
	pausou  atomic.Bool

	mu    sync.Mutex
	conns []net.Conn
	wg    sync.WaitGroup
}

// novoProxyPausa sobe o proxy diante do banco de cfg e devolve também uma cópia
// de cfg que conecta pelo proxy, sem TLS.
func novoProxyPausa(t *testing.T, cfg *pgx.ConnConfig, marca string) (*proxyPausa, *pgx.ConnConfig) {
	t.Helper()
	if strings.HasPrefix(cfg.Host, "/") {
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
	p := &proxyPausa{
		ln:      ln,
		alvo:    net.JoinHostPort(cfg.Host, strconv.Itoa(int(cfg.Port))),
		marca:   []byte(strings.ToLower(marca)),
		chegou:  make(chan struct{}, 1),
		liberar: make(chan struct{}),
	}
	p.wg.Add(1)
	go p.aceitar()
	t.Cleanup(p.fechar)

	host, portStr, _ := net.SplitHostPort(ln.Addr().String())
	port, _ := strconv.Atoi(portStr)
	c := cfg.Copy()
	c.Host, c.Port = host, uint16(port)
	c.TLSConfig = nil
	c.Fallbacks = nil
	return p, c
}

func (p *proxyPausa) fechar() {
	// Solta qualquer mensagem presa para não pendurar o teste.
	select {
	case <-p.liberar:
	default:
		close(p.liberar)
	}
	_ = p.ln.Close()
	p.mu.Lock()
	for _, c := range p.conns {
		_ = c.Close()
	}
	p.mu.Unlock()
	p.wg.Wait()
}

func (p *proxyPausa) guardar(c net.Conn) {
	p.mu.Lock()
	defer p.mu.Unlock()
	p.conns = append(p.conns, c)
}

func (p *proxyPausa) aceitar() {
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

func (p *proxyPausa) atender(cli net.Conn) {
	defer p.wg.Done()
	defer cli.Close()
	srv, err := net.DialTimeout("tcp", p.alvo, 5*time.Second)
	if err != nil {
		return
	}
	p.guardar(srv)
	defer srv.Close()

	fim := make(chan struct{}, 2)
	go func() {
		buf := make([]byte, 32<<10)
		var cauda []byte
		for {
			n, err := cli.Read(buf)
			if n > 0 {
				janela := append(append([]byte(nil), cauda...), bytes.ToLower(buf[:n])...)
				if bytes.Contains(janela, p.marca) && p.pausou.CompareAndSwap(false, true) {
					p.chegou <- struct{}{}
					<-p.liberar
				}
				ini := max(0, len(janela)-(len(p.marca)-1))
				cauda = append([]byte(nil), janela[ini:]...)
				if _, werr := srv.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		fim <- struct{}{}
	}()
	go func() {
		buf := make([]byte, 32<<10)
		for {
			n, err := srv.Read(buf)
			if n > 0 {
				if _, werr := cli.Write(buf[:n]); werr != nil {
					break
				}
			}
			if err != nil {
				break
			}
		}
		fim <- struct{}{}
	}()
	<-fim
	_ = cli.Close()
	_ = srv.Close()
	<-fim
}
