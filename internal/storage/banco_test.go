package storage

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"io/fs"
	"os"
	"sort"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/migrations"
)

const (
	envURLBanco         = "BIBLIOTECA_TEST_DATABASE_URL"
	envBancoObrigatorio = "BIBLIOTECA_TEST_DB_OBRIGATORIO"
)

// urlBanco devolve a URL do banco de teste. Sem a variável, o teste é pulado;
// com BIBLIOTECA_TEST_DB_OBRIGATORIO=1, a ausência reprova.
func urlBanco(t *testing.T) string {
	t.Helper()
	u := os.Getenv(envURLBanco)
	if u == "" {
		if os.Getenv(envBancoObrigatorio) == "1" {
			t.Fatalf("%s ausente com %s=1: teste de banco obrigatório", envURLBanco, envBancoObrigatorio)
		}
		t.Skipf("%s não definida: teste de banco pulado", envURLBanco)
	}
	return u
}

// bancoTeste é um schema descartável, exclusivo do teste.
type bancoTeste struct {
	url    string
	schema string
}

// novoBanco cria o schema t_<hex aleatório> e registra o DROP ... CASCADE no
// Cleanup. As conexões abertas por conectar fecham antes do DROP (LIFO).
func novoBanco(t *testing.T) *bancoTeste {
	t.Helper()
	u := urlBanco(t)

	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("gerar nome do schema: %v", err)
	}
	schema := "t_" + hex.EncodeToString(b[:])
	ident := pgx.Identifier{schema}.Sanitize()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	admin, err := pgx.Connect(ctx, u)
	if err != nil {
		// Sem o erro do pgx: ele traz host, usuário e banco.
		t.Fatalf("conectar ao banco de teste: falhou (confira %s)", envURLBanco)
	}
	defer admin.Close(ctx)
	if _, err := admin.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatalf("criar schema de teste: %v", err)
	}

	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(ctx, u)
		if err != nil {
			t.Errorf("conectar para remover schema %s: falhou (confira %s)", schema, envURLBanco)
			return
		}
		defer c.Close(ctx)
		if _, err := c.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE"); err != nil {
			t.Errorf("remover schema %s: %v", schema, err)
		}
	})
	return &bancoTeste{url: u, schema: schema}
}

// conectar abre uma conexão com search_path no schema do teste. params extras
// vão para RuntimeParams (por exemplo, timezone).
func (b *bancoTeste) conectar(t *testing.T, params ...string) *pgx.Conn {
	t.Helper()
	cfg, err := pgx.ParseConfig(b.url)
	if err != nil {
		t.Fatalf("interpretar %s: %v", envURLBanco, err)
	}
	cfg.RuntimeParams["search_path"] = b.schema
	for i := 0; i+1 < len(params); i += 2 {
		cfg.RuntimeParams[params[i]] = params[i+1]
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	c, err := pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("conectar ao schema de teste: falhou (confira %s)", envURLBanco)
	}
	t.Cleanup(func() { _ = c.Close(context.Background()) })
	return c
}

// aplicar executa, em ordem, os arquivos do FS embutido que terminam em
// sufixo (".up.sql" em ordem crescente, ".down.sql" em ordem decrescente),
// com Exec sem argumentos.
func aplicar(t *testing.T, c *pgx.Conn, sufixo string) {
	t.Helper()
	nomes, err := fs.Glob(migrations.FS, "*"+sufixo)
	if err != nil {
		t.Fatalf("listar migrações: %v", err)
	}
	if len(nomes) == 0 {
		t.Fatalf("nenhuma migração *%s", sufixo)
	}
	sort.Strings(nomes)
	if sufixo == ".down.sql" {
		sort.Sort(sort.Reverse(sort.StringSlice(nomes)))
	}
	for _, n := range nomes {
		sql, err := fs.ReadFile(migrations.FS, n)
		if err != nil {
			t.Fatalf("ler %s: %v", n, err)
		}
		if _, err := c.Exec(t.Context(), string(sql)); err != nil {
			t.Fatalf("aplicar %s: %v", n, err)
		}
	}
}

// novoBancoMigrado cria o schema, aplica os .up.sql e devolve uma conexão.
func novoBancoMigrado(t *testing.T) (*bancoTeste, *pgx.Conn) {
	t.Helper()
	b := novoBanco(t)
	c := b.conectar(t)
	aplicar(t, c, ".up.sql")
	return b, c
}

// pgErro extrai o *pgconn.PgError de err ou reprova.
func pgErro(t *testing.T, err error) *pgconn.PgError {
	t.Helper()
	var pe *pgconn.PgError
	if !errors.As(err, &pe) {
		t.Fatalf("erro = %v, want *pgconn.PgError", err)
	}
	return pe
}

// esperarBloqueio espera até a conexão pid estar parada em lock.
func esperarBloqueio(t *testing.T, obs *pgx.Conn, pid uint32) {
	t.Helper()
	limite := time.Now().Add(15 * time.Second)
	for time.Now().Before(limite) {
		var tipo *string
		err := obs.QueryRow(t.Context(), "SELECT wait_event_type FROM pg_stat_activity WHERE pid = $1", int32(pid)).Scan(&tipo)
		if err != nil && !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("consultar pg_stat_activity: %v", err)
		}
		if tipo != nil && *tipo == "Lock" {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("conexão %d não bloqueou em lock em 15s", pid)
}
