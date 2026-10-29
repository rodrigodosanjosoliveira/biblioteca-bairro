package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/config"
)

// Regressões das revisões da DEL-006 (code-reviewer).

// executar só devolve erroBoot: o compilador impede devolver o erro do pgx.
var _ func(context.Context, func(string) string, *slog.Logger) erroBoot = executar

// TestConfigurarPoolMinIdleEJitter: pool_min_idle_conns e
// pool_max_conn_lifetime_jitter da DSN são sobrescritos com 0.
func TestConfigurarPoolMinIdleEJitter(t *testing.T) {
	const dsn = "postgres://u:p@localhost:5432/db?pool_max_conns=50&pool_min_conns=3&pool_min_idle_conns=3&pool_max_conn_lifetime=2h&pool_max_conn_lifetime_jitter=10m&pool_max_conn_idle_time=1h&pool_health_check_period=10m"

	// A DSN de fato traz valores diferentes de zero (o teste não é vazio).
	bruto, err := pgxpool.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("ParseConfig: %v", err)
	}
	if bruto.MinIdleConns != 3 || bruto.MaxConnLifetimeJitter != 10*time.Minute {
		t.Fatalf("DSN de teste não fixa MinIdleConns/Jitter: %d %v", bruto.MinIdleConns, bruto.MaxConnLifetimeJitter)
	}

	pc, err := configurarPool(config.Config{DatabaseURL: dsn, Token: tokenTeste, Addr: ":8080", MaxConns: 7})
	if err != nil {
		t.Fatalf("configurarPool: %v", err)
	}
	checks := []struct {
		nome      string
		got, want any
	}{
		{"MaxConns", pc.MaxConns, int32(7)},
		{"MinConns", pc.MinConns, int32(0)},
		{"MinIdleConns", pc.MinIdleConns, int32(0)},
		{"MaxConnLifetime", pc.MaxConnLifetime, 30 * time.Minute},
		{"MaxConnLifetimeJitter", pc.MaxConnLifetimeJitter, time.Duration(0)},
		{"MaxConnIdleTime", pc.MaxConnIdleTime, 5 * time.Minute},
		{"HealthCheckPeriod", pc.HealthCheckPeriod, time.Minute},
		{"ConnectTimeout", pc.ConnConfig.ConnectTimeout, 5 * time.Second},
	}
	for _, c := range checks {
		if c.got != c.want {
			t.Errorf("%s = %v, want %v", c.nome, c.got, c.want)
		}
	}
}

// TestRunDevolveErroBoot: todo erro de run é um erroBoot de mensagem fixa.
func TestRunDevolveErroBoot(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantMsg string
	}{
		{name: "sem DATABASE_URL", env: map[string]string{"BIBLIOTECA_API_TOKEN": tokenTeste}, wantMsg: "config: DATABASE_URL ausente"},
		{name: "sem token", env: map[string]string{"DATABASE_URL": "postgres://u:SENHA-DSN@127.0.0.1:1/db"}, wantMsg: "config: BIBLIOTECA_API_TOKEN ausente"},
		{name: "max conns", env: map[string]string{"DATABASE_URL": "postgres://u:SENHA-DSN@127.0.0.1:1/db", "BIBLIOTECA_API_TOKEN": tokenTeste, "BIBLIOTECA_DB_MAX_CONNS": "0"}, wantMsg: "config: BIBLIOTECA_DB_MAX_CONNS inválida"},
		{name: "DSN inválida", env: map[string]string{"DATABASE_URL": "postgres://u:SENHA-DSN@127.0.0.1:porta/db", "BIBLIOTECA_API_TOKEN": tokenTeste}, wantMsg: msgDSN},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr strings.Builder
			err := run(context.Background(), ambiente(tt.env), &stderr)
			eb, ok := err.(erroBoot)
			if !ok {
				t.Fatalf("run() = %T, want erroBoot", err)
			}
			if string(eb) != tt.wantMsg {
				t.Errorf("erro = %q, want %q", eb, tt.wantMsg)
			}
		})
	}
}

// TestBootDeConfig: só config.Erro vira erroBoot com a própria mensagem;
// qualquer outro erro vira a mensagem genérica, sem ecoar texto.
func TestBootDeConfig(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want erroBoot
	}{
		{name: "config.Erro", err: config.Erro("config: DATABASE_URL ausente"), want: "config: DATABASE_URL ausente"},
		{name: "config.Erro embrulhado perde o embrulho", err: fmt.Errorf("SEGREDO %w", config.Erro("config: X")), want: "config: X"},
		{name: "erro qualquer", err: errors.New("postgres://u:SENHA-DSN@h/db"), want: errConfig},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := bootDeConfig(tt.err); got != tt.want {
				t.Errorf("bootDeConfig = %q, want %q", got, tt.want)
			}
		})
	}
}

const envFilhoSinal = "BIBLIOTECA_TESTE_FILHO_SINAL"

// TestSegundoSinalEncerraProcesso roda um processo filho que usa
// contextoDeSinal e, depois do primeiro SIGTERM, finge um shutdown demorado. O
// segundo SIGTERM tem que matar o filho pelo comportamento padrão do Go. Usa
// SIGTERM e não SIGINT: um SIGINT herdado como ignorado (comum em CI, quando o
// job roda em segundo plano) não mataria o filho.
func TestSegundoSinalEncerraProcesso(t *testing.T) {
	if os.Getenv(envFilhoSinal) == "1" {
		filhoSinal()
		return
	}
	if runtime.GOOS == "windows" {
		t.Skip("SIGTERM para processo filho não é suportado no Windows")
	}
	tests := []struct {
		name       string
		wantSinal  syscall.Signal
		prazoMorte time.Duration
	}{
		{name: "segundo SIGTERM encerra durante o shutdown", wantSinal: syscall.SIGTERM, prazoMorte: 5 * time.Second},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cmd := exec.Command(os.Args[0], "-test.run=^TestSegundoSinalEncerraProcesso$", "-test.count=1")
			cmd.Env = append(os.Environ(), envFilhoSinal+"=1")
			out, err := cmd.StdoutPipe()
			if err != nil {
				t.Fatalf("StdoutPipe: %v", err)
			}
			if err := cmd.Start(); err != nil {
				t.Fatalf("Start: %v", err)
			}
			// Wait roda uma vez só, nesta goroutine, iniciada depois do
			// "shutdown". Na falha, o defer mata o filho e espera por ela (ou
			// chama Wait se ela ainda não existe); no caminho feliz o Wait já
			// voltou e o defer não faz nada.
			var fim chan error
			esperado := false
			defer func() {
				if esperado {
					return
				}
				_ = cmd.Process.Kill()
				if fim == nil {
					_ = cmd.Wait()
					return
				}
				<-fim
			}()

			linhas := make(chan string, 4)
			go func() {
				sc := bufio.NewScanner(out)
				for sc.Scan() {
					linhas <- sc.Text()
				}
				close(linhas)
			}()
			esperar := func(want string) {
				t.Helper()
				select {
				case l := <-linhas:
					if l != want {
						t.Fatalf("filho escreveu %q, want %q", l, want)
					}
				case <-time.After(10 * time.Second):
					t.Fatalf("filho não escreveu %q", want)
				}
			}

			esperar("pronto")
			if err := cmd.Process.Signal(syscall.SIGTERM); err != nil {
				t.Fatalf("primeiro SIGTERM: %v", err)
			}
			esperar("shutdown")

			fim = make(chan error, 1)
			go func() { fim <- cmd.Wait() }()
			// O stop roda em goroutine logo depois do Done; repete o SIGTERM até
			// o filho morrer ou o prazo esgotar.
			prazo := time.After(tt.prazoMorte)
			tique := time.NewTicker(50 * time.Millisecond)
			defer tique.Stop()
			for {
				_ = cmd.Process.Signal(syscall.SIGTERM)
				select {
				case <-fim:
					esperado = true
					ws, ok := cmd.ProcessState.Sys().(syscall.WaitStatus)
					if !ok || !ws.Signaled() || ws.Signal() != tt.wantSinal {
						t.Fatalf("filho terminou com %v, want morto por %v", cmd.ProcessState, tt.wantSinal)
					}
					return
				case <-prazo:
					t.Fatalf("segundo SIGTERM não encerrou o processo em %v", tt.prazoMorte)
				case <-tique.C:
				}
			}
		})
	}
}

// filhoSinal é o corpo do processo filho de TestSegundoSinalEncerraProcesso.
func filhoSinal() {
	ctx, stop := contextoDeSinal(context.Background())
	defer stop()
	fmt.Println("pronto")
	<-ctx.Done()
	fmt.Println("shutdown")
	// Shutdown demorado: só o segundo sinal deve encerrar antes disto.
	time.Sleep(30 * time.Second)
	os.Exit(3)
}
