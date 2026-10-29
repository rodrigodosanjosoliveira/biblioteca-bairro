package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/config"
)

// Assinaturas exigidas do pacote main.
var (
	_ func(context.Context, func(string) string, io.Writer) error = run
	_ func(config.Config) (*pgxpool.Config, error)                = configurarPool
)

const (
	tokenTeste = "TOKEN-SECRETO-0123456789abcdefghijklmn" // 38 bytes
	msgDSN     = "config: DATABASE_URL inválida"
)

func ambiente(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

func TestRunErrosDeConfiguracao(t *testing.T) {
	segredosBase := []string{tokenTeste, "SENHA-DSN", "usuario-secreto"}
	tests := []struct {
		name string
		env  map[string]string
		// wantMsg é a mensagem exata; wantContem, um trecho obrigatório.
		wantMsg    string
		wantContem string
		segredos   []string
	}{
		{
			name:    "sem DATABASE_URL",
			env:     map[string]string{"BIBLIOTECA_API_TOKEN": tokenTeste},
			wantMsg: "config: DATABASE_URL ausente",
		},
		{
			name:       "sem token",
			env:        map[string]string{"DATABASE_URL": "postgres://usuario-secreto:SENHA-DSN@127.0.0.1:1/db"},
			wantContem: "BIBLIOTECA_API_TOKEN",
		},
		{
			name:       "token curto",
			env:        map[string]string{"DATABASE_URL": "postgres://usuario-secreto:SENHA-DSN@127.0.0.1:1/db", "BIBLIOTECA_API_TOKEN": "CURTO-SECRETO"},
			wantContem: "BIBLIOTECA_API_TOKEN",
			segredos:   []string{"CURTO-SECRETO"},
		},
		{
			name:    "max conns inválida",
			env:     map[string]string{"DATABASE_URL": "postgres://usuario-secreto:SENHA-DSN@127.0.0.1:1/db", "BIBLIOTECA_API_TOKEN": tokenTeste, "BIBLIOTECA_DB_MAX_CONNS": "500"},
			wantMsg: "config: BIBLIOTECA_DB_MAX_CONNS inválida",
		},
		{
			name:    "DSN URL com porta inválida",
			env:     map[string]string{"DATABASE_URL": "postgres://usuario-secreto:SENHA-DSN@127.0.0.1:porta/db", "BIBLIOTECA_API_TOKEN": tokenTeste},
			wantMsg: msgDSN,
		},
		{
			name:    "DSN chave-valor com pool_max_conns inválido",
			env:     map[string]string{"DATABASE_URL": "host=127.0.0.1 user=usuario-secreto password=SENHA-DSN pool_max_conns=abc", "BIBLIOTECA_API_TOKEN": tokenTeste},
			wantMsg: msgDSN,
		},
		{
			name:    "DSN com pool_min_conns inválido",
			env:     map[string]string{"DATABASE_URL": "postgres://usuario-secreto:SENHA-DSN@127.0.0.1/db?pool_min_conns=x", "BIBLIOTECA_API_TOKEN": tokenTeste},
			wantMsg: msgDSN,
		},
		{
			name:       "banco indisponível no boot",
			env:        map[string]string{"DATABASE_URL": "postgres://usuario-secreto:SENHA-DSN@127.0.0.1:1/db?connect_timeout=2&sslmode=disable", "BIBLIOTECA_API_TOKEN": tokenTeste, "ADDR": "127.0.0.1:0"},
			wantContem: "banco indisponível no boot",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var stderr bytes.Buffer
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			inicio := time.Now()
			err := run(ctx, ambiente(tt.env), &stderr)
			if err == nil {
				t.Fatal("run() = nil, want erro")
			}
			if d := time.Since(inicio); d > 15*time.Second {
				t.Errorf("run levou %v para falhar", d)
			}
			msg := err.Error()
			if tt.wantMsg != "" {
				if msg != tt.wantMsg {
					t.Errorf("erro = %q, want %q", msg, tt.wantMsg)
				}
				if errors.Unwrap(err) != nil {
					t.Errorf("erro embrulha outro (%T), want mensagem fixa sem causa", errors.Unwrap(err))
				}
			}
			if tt.wantContem != "" && !strings.Contains(msg, tt.wantContem) {
				t.Errorf("erro = %q, want conter %q", msg, tt.wantContem)
			}
			for _, s := range append(segredosBase, tt.segredos...) {
				if strings.Contains(msg, s) {
					t.Errorf("erro %q ecoa %q", msg, s)
				}
				if strings.Contains(stderr.String(), s) {
					t.Errorf("stderr ecoa %q: %s", s, stderr.String())
				}
			}
		})
	}
}

func TestConfigurarPool(t *testing.T) {
	cfgCom := func(dsn string, maxConns int32) config.Config {
		return config.Config{DatabaseURL: dsn, Token: tokenTeste, Addr: ":8080", MaxConns: maxConns}
	}

	t.Run("padroes e sobrescrita da DSN", func(t *testing.T) {
		pc, err := configurarPool(cfgCom("postgres://u:p@localhost:5432/db?pool_max_conns=50&pool_min_conns=3&pool_max_conn_lifetime=2h&pool_max_conn_idle_time=1h&pool_health_check_period=10m", 7))
		if err != nil {
			t.Fatalf("configurarPool: %v", err)
		}
		checks := []struct {
			nome      string
			got, want any
		}{
			{"MaxConns", pc.MaxConns, int32(7)},
			{"MinConns", pc.MinConns, int32(0)},
			{"MaxConnLifetime", pc.MaxConnLifetime, 30 * time.Minute},
			{"MaxConnIdleTime", pc.MaxConnIdleTime, 5 * time.Minute},
			{"HealthCheckPeriod", pc.HealthCheckPeriod, time.Minute},
			{"ConnectTimeout", pc.ConnConfig.ConnectTimeout, 5 * time.Second},
		}
		for _, c := range checks {
			if c.got != c.want {
				t.Errorf("%s = %v, want %v", c.nome, c.got, c.want)
			}
		}
	})
	t.Run("connect_timeout da DSN é mantido", func(t *testing.T) {
		pc, err := configurarPool(cfgCom("postgres://u:p@localhost:5432/db?connect_timeout=2", 10))
		if err != nil {
			t.Fatalf("configurarPool: %v", err)
		}
		if pc.ConnConfig.ConnectTimeout != 2*time.Second {
			t.Errorf("ConnectTimeout = %v, want 2s", pc.ConnConfig.ConnectTimeout)
		}
		if pc.MaxConns != 10 {
			t.Errorf("MaxConns = %d, want 10", pc.MaxConns)
		}
	})
	t.Run("DSN chave-valor", func(t *testing.T) {
		pc, err := configurarPool(cfgCom("host=localhost user=u password=p dbname=db pool_max_conns=2", 1))
		if err != nil {
			t.Fatalf("configurarPool: %v", err)
		}
		if pc.MaxConns != 1 {
			t.Errorf("MaxConns = %d, want 1", pc.MaxConns)
		}
	})
	for _, dsn := range []string{
		"postgres://u:SENHA-DSN@localhost:porta/db",
		"host=localhost password=SENHA-DSN pool_max_conns=abc",
		"postgres://u:SENHA-DSN@localhost/db?pool_max_conn_lifetime=xyz",
	} {
		t.Run("invalida", func(t *testing.T) {
			pc, err := configurarPool(cfgCom(dsn, 10))
			if err == nil {
				t.Fatalf("configurarPool(%q) = %v, want erro", dsn, pc)
			}
			if pc != nil {
				t.Errorf("config não nil em erro")
			}
			if err.Error() != msgDSN || errors.Unwrap(err) != nil {
				t.Errorf("erro = %q (unwrap %v), want %q sem embrulhar", err.Error(), errors.Unwrap(err), msgDSN)
			}
		})
	}
}
