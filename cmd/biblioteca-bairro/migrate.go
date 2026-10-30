package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/config"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/migrate"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/migrations"
)

// Textos do subcomando migrate.
const (
	textoUso       = "uso: biblioteca-bairro [migrate up | migrate status]"
	msgMigrateErro = "biblioteca-bairro migrate encerrado com erro"

	// Prazo para fechar a conexão de migração depois do fim do trabalho.
	prazoFecharMigracao = 5 * time.Second

	applicationNameMigracao = "biblioteca-bairro-migrate"
)

// Erros do subcomando migrate, com mensagens fixas, sem DSN nem texto do
// servidor. Os erros do executor (migrate.Erro) e da configuração
// (config.Erro) são convertidos para este tipo com as mensagens deles, que
// também são fixas.
const (
	errBancoMigrate  erroBoot = "migrate: banco indisponível"
	errMigrateFalha  erroBoot = "migrate: falha inesperada"
	errMigrateEstado erroBoot = "migrate: há migrações divergentes ou desconhecidas no banco"
)

// migrador é o que o CLI usa do executor de migrações.
type migrador interface {
	Subir(ctx context.Context) ([]migrate.Migracao, error)
	Status(ctx context.Context) ([]migrate.Situacao, error)
}

// abrirMigrador abre a conexão dedicada e o executor. O func() devolvido fecha
// a conexão; o chamador o executa uma vez quando erroBoot é semErro. É
// variável para os testes injetarem um migrador falso.
var abrirMigrador = abrirMigradorReal

// despachar escolhe o modo pelos argumentos (sem o nome do programa) e devolve
// o código de saída: sem argumentos é o servidor; "migrate up" e "migrate
// status" são as migrações; qualquer outra coisa imprime o uso e devolve 2,
// sem abrir conexão.
func despachar(ctx context.Context, args []string, getenv func(string) string, stdout, stderr io.Writer) int {
	switch {
	case len(args) == 0:
		if err := run(ctx, getenv, stderr); err != nil {
			return 1
		}
		return 0
	case len(args) == 2 && args[0] == "migrate" && (args[1] == "up" || args[1] == "status"):
		return migrar(ctx, args[1], getenv, stdout, stderr)
	default:
		fmt.Fprintln(stderr, textoUso)
		return 2
	}
}

// migrar roda "migrate up" ou "migrate status" e devolve o código de saída: 0
// em sucesso e 1 em erro. Loga em JSON no stderr; o resultado vai no stdout.
func migrar(ctx context.Context, modo string, getenv func(string) string, stdout, stderr io.Writer) int {
	logger := slog.New(slog.NewJSONHandler(stderr, nil))
	eb := executarMigracao(ctx, modo, getenv, stdout, logger)
	if eb == semErro {
		return 0
	}
	logger.Error(msgMigrateErro, slog.String("erro", eb.Error()))
	return 1
}

func executarMigracao(ctx context.Context, modo string, getenv func(string) string, stdout io.Writer, logger *slog.Logger) erroBoot {
	cfg, err := config.CarregarMigracao(getenv)
	if err != nil {
		return bootDeConfig(err)
	}

	ctx, stop := contextoDeSinal(ctx)
	defer stop()

	m, fechar, eb := abrirMigrador(ctx, cfg, logger)
	if eb != semErro {
		return eb
	}
	defer fechar()

	if modo == "up" {
		aplicadas, err := m.Subir(ctx)
		if err != nil {
			return bootDeMigracao(err)
		}
		if len(aplicadas) == 0 {
			fmt.Fprintln(stdout, "migrate up: nenhuma migração pendente")
		} else {
			fmt.Fprintf(stdout, "migrate up: %d migração(ões) aplicada(s)\n", len(aplicadas))
		}
		return semErro
	}

	situacoes, err := m.Status(ctx)
	if err != nil {
		return bootDeMigracao(err)
	}
	problema := false
	for _, s := range situacoes {
		linha := fmt.Sprintf("%04d %s %s", s.Versao, nomeSeguro(s.Nome), s.Estado)
		if !s.AplicadaEm.IsZero() {
			linha += " " + s.AplicadaEm.UTC().Format(time.RFC3339)
		}
		fmt.Fprintln(stdout, linha)
		if s.Estado == migrate.EstadoDivergente || s.Estado == migrate.EstadoDesconhecida {
			problema = true
		}
	}
	if problema {
		return errMigrateEstado
	}
	return semErro
}

// nomeSeguro devolve nome como está quando é texto simples e entre aspas
// escapadas quando tem controle (quebra de linha, ESC, DEL), aspas ou bytes
// inválidos. O nome de uma versão desconhecida vem do banco, que pode ter sido
// adotado sem os CHECKs: sem isso ele quebraria a linha do status ou levaria
// escape de terminal para a saída.
func nomeSeguro(nome string) string {
	if q := strconv.Quote(nome); q != `"`+nome+`"` {
		return q
	}
	return nome
}

// bootDeMigracao converte o erro do executor em erroBoot. Só o tipo
// migrate.Erro (mensagem fixa) é aproveitado; qualquer outro vira a mensagem
// genérica errMigrateFalha, para que nada do que um erro estranho traga
// chegue ao stderr.
func bootDeMigracao(err error) erroBoot {
	var me migrate.Erro
	if errors.As(err, &me) {
		return erroBoot(me.Error())
	}
	return errMigrateFalha
}

// abrirMigradorReal abre uma conexão dedicada (nunca um pool) com o DSN de
// cfg e cria o executor sobre migrations.FS. DSN inválido e falha de conexão
// devolvem mensagens fixas, sem o erro do pgx, que traz host, usuário e trechos
// do DSN.
func abrirMigradorReal(ctx context.Context, cfg config.ConfigMigracao, logger *slog.Logger) (migrador, func(), erroBoot) {
	cc, err := pgx.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, nil, errDSN
	}
	if cc.ConnectTimeout == 0 {
		cc.ConnectTimeout = connectTimeoutPadrao
	}
	if cc.RuntimeParams["application_name"] == "" {
		cc.RuntimeParams["application_name"] = applicationNameMigracao
	}
	// A DSN é a mesma do servidor, e o pgxpool consome os parâmetros pool_*.
	// Esta conexão não é um pool: deixá-los nos RuntimeParams faria o servidor
	// recusar o startup por parâmetro de configuração desconhecido.
	for k := range cc.RuntimeParams {
		if strings.HasPrefix(k, "pool_") {
			delete(cc.RuntimeParams, k)
		}
	}
	conn, err := pgx.ConnectConfig(ctx, cc)
	if err != nil {
		return nil, nil, errBancoMigrate
	}
	fechar := func() {
		fctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), prazoFecharMigracao)
		defer cancel()
		_ = conn.Close(fctx)
		// Depois de um cancelamento no meio de um Exec, o pgx fecha a conexão
		// por asyncClose: o CancelRequest e o Terminate saem numa goroutine,
		// por uma conexão nova, e Close retorna na hora. Sem esperar por ela o
		// processo sairia antes do CancelRequest e o backend seguiria com o
		// DDL e o advisory lock. A espera é limitada por fctx.
		select {
		case <-conn.PgConn().CleanupDone():
		case <-fctx.Done():
		}
	}
	ex := migrate.Novo(conn, migrations.FS, migrate.Opcoes{
		LockTimeout:      cfg.LockTimeout,
		StatementTimeout: cfg.StatementTimeout,
		Logger:           logger,
	})
	return ex, fechar, semErro
}
