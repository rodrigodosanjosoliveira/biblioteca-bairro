package httpapi

import (
	"context"
	"errors"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// Timeouts do servidor HTTP.
const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 15 * time.Second
	idleTimeout       = 60 * time.Second
	// maxHeaderBytes limita o tamanho dos headers da requisição.
	maxHeaderBytes = 64 << 10
)

// NovoServidor cria o *http.Server com os timeouts de leitura, escrita e
// ociosidade, headers limitados a 64 KiB e o ErrorLog do net/http ligado ao logger (nível Error).
func NovoServidor(addr string, h http.Handler, logger *slog.Logger) *http.Server {
	if logger == nil {
		logger = slog.New(slog.DiscardHandler)
	}
	return &http.Server{
		Addr:              addr,
		Handler:           h,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
		MaxHeaderBytes:    maxHeaderBytes,
		ErrorLog:          slog.NewLogLogger(logger.Handler(), slog.LevelError),
	}
}

// Servir atende em ln até ctx terminar e então faz o shutdown gracioso:
// para de aceitar conexões e espera as requisições em voo por até
// prazoShutdown. Devolve nil no shutdown gracioso, o erro do Shutdown (que
// contém context.DeadlineExceeded) se o prazo esgotar, ou o erro do Serve se
// ele falhar antes.
func Servir(ctx context.Context, srv *http.Server, ln net.Listener, prazoShutdown time.Duration) error {
	fimServe := make(chan error, 1)
	go func() { fimServe <- srv.Serve(ln) }()

	select {
	case err := <-fimServe:
		return err
	case <-ctx.Done():
	}

	sctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), prazoShutdown)
	defer cancel()
	if err := srv.Shutdown(sctx); err != nil {
		// Prazo esgotado: derruba as conexões que restaram.
		_ = srv.Close()
		<-fimServe
		return err
	}
	if err := <-fimServe; !errors.Is(err, http.ErrServerClosed) {
		return err
	}
	return nil
}
