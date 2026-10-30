// Command biblioteca-bairro é o servidor HTTP do projeto.
package main

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"net"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/config"
	httpapi "github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/storage"
)

// Prazos do boot e do encerramento.
const (
	prazoPingBoot        = 5 * time.Second
	prazoShutdown        = 20 * time.Second
	timeoutOperacao      = 5 * time.Second
	connectTimeoutPadrao = 5 * time.Second
)

// erroBoot é o único tipo de erro que executar devolve. É uma string
// constante, sem Unwrap: o compilador impede devolver (ou embrulhar) o erro do
// pgx, que pode trazer host, usuário e trechos do DSN. O valor zero significa
// "sem erro".
type erroBoot string

func (e erroBoot) Error() string { return string(e) }

// Erros do boot, com mensagens fixas.
const (
	semErro          erroBoot = ""
	errDSN           erroBoot = "config: DATABASE_URL inválida"
	errConfig        erroBoot = "config: inválida"
	errBancoBoot     erroBoot = "banco indisponível no boot"
	errEscutar       erroBoot = "servidor: não foi possível escutar em ADDR"
	errServir        erroBoot = "servidor: falha ao atender"
	errPrazoShutdown erroBoot = "servidor: prazo de shutdown esgotado"
)

// bootDeConfig converte o erro de config.Carregar em erroBoot. Só o tipo
// config.Erro (string constante de mensagem fixa) é convertido; qualquer outro
// vira a mensagem genérica errConfig.
func bootDeConfig(err error) erroBoot {
	var ec config.Erro
	if errors.As(err, &ec) {
		return erroBoot(ec)
	}
	return errConfig
}

func main() {
	os.Exit(despachar(context.Background(), os.Args[1:], os.Getenv, os.Stdout, os.Stderr))
}

// run carrega a configuração, abre o pool, sobe o servidor e atende até
// SIGINT/SIGTERM ou o fim de ctx. Loga em JSON em stderr. Todo erro devolvido
// tem mensagem fixa, sem segredo; ele também é registrado no log.
func run(ctx context.Context, getenv func(string) string, stderr io.Writer) error {
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	eb := executar(ctx, getenv, logger)
	if eb == semErro {
		return nil
	}
	logger.Error("biblioteca-bairro encerrado com erro", slog.String("erro", eb.Error()))
	return eb
}

// contextoDeSinal devolve um contexto que termina no primeiro SIGINT/SIGTERM
// (ou no fim de ctx). Assim que ele termina, a captura dos sinais é desfeita:
// um segundo SIGINT durante o shutdown encerra o processo pelo comportamento
// padrão do Go.
func contextoDeSinal(ctx context.Context) (context.Context, context.CancelFunc) {
	ctx, stop := signal.NotifyContext(ctx, os.Interrupt, syscall.SIGTERM)
	context.AfterFunc(ctx, stop)
	return ctx, stop
}

func executar(ctx context.Context, getenv func(string) string, logger *slog.Logger) erroBoot {
	cfg, err := config.Carregar(getenv)
	if err != nil {
		return bootDeConfig(err)
	}
	pcfg, err := configurarPool(cfg)
	if err != nil {
		// configurarPool só falha com errDSN.
		return errDSN
	}

	ctx, stop := contextoDeSinal(ctx)
	defer stop()

	pool, err := pgxpool.NewWithConfig(ctx, pcfg)
	if err != nil {
		return errBancoBoot
	}
	// Roda depois do Servir, isto é, depois do Shutdown do servidor.
	defer pool.Close()

	pctx, cancel := context.WithTimeout(ctx, prazoPingBoot)
	err = pool.Ping(pctx)
	cancel()
	if err != nil {
		return errBancoBoot
	}

	var lc net.ListenConfig
	ln, err := lc.Listen(ctx, "tcp", cfg.Addr)
	if err != nil {
		return errEscutar
	}

	h := httpapi.NovoHandler(httpapi.Dependencias{
		Repo:            storage.NovoRepositorio(pool),
		Pinger:          pool,
		Agora:           time.Now,
		Token:           cfg.Token,
		Logger:          logger,
		TimeoutOperacao: timeoutOperacao,
	})
	srv := httpapi.NovoServidor(cfg.Addr, h, logger)
	logger.Info("biblioteca-bairro ouvindo", slog.String("addr", ln.Addr().String()))

	switch err := httpapi.Servir(ctx, srv, ln, prazoShutdown); {
	case err == nil:
		logger.Info("biblioteca-bairro encerrado")
		return semErro
	case errors.Is(err, context.DeadlineExceeded):
		return errPrazoShutdown
	default:
		return errServir
	}
}

// configurarPool interpreta o DSN e aplica a política do pool: MaxConns da
// configuração (sobrescreve o pool_max_conns do DSN), MinConns e MinIdleConns
// 0, vida máxima de 30 min sem jitter, ociosidade de 5 min, health check a
// cada 1 min e connect timeout de 5 s quando o DSN não define um. DSN
// inválido devolve a mensagem fixa "config: DATABASE_URL inválida", sem o
// erro do pgx.
func configurarPool(c config.Config) (*pgxpool.Config, error) {
	pc, err := pgxpool.ParseConfig(c.DatabaseURL)
	if err != nil {
		return nil, errDSN
	}
	pc.MaxConns = c.MaxConns
	pc.MinConns = 0
	pc.MinIdleConns = 0
	pc.MaxConnLifetime = 30 * time.Minute
	pc.MaxConnLifetimeJitter = 0
	pc.MaxConnIdleTime = 5 * time.Minute
	pc.HealthCheckPeriod = time.Minute
	if pc.ConnConfig.ConnectTimeout == 0 {
		pc.ConnConfig.ConnectTimeout = connectTimeoutPadrao
	}
	return pc, nil
}
