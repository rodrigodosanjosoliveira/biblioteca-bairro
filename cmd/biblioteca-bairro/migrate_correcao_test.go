package main

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/url"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/config"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/migrate"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/migrations"
)

// Testes do ciclo de correção pós-revisão da DEL-008. Todos usam a
// implementação real (abrirMigradorReal) e exigem banco; sem
// BIBLIOTECA_TEST_DATABASE_URL são pulados.

// ---- bloqueio 1: fechar espera a limpeza assíncrona do pgx ----

// medidaFechar é o que se observa no banco no instante em que o fechar real
// retorna.
type medidaFechar struct {
	duracao  time.Duration
	pidVivo  bool
	locks    int
	consulta error
}

// resultadoCancelamento é o que sobra da execução cancelada.
type resultadoCancelamento struct {
	code   int
	stdout string
	stderr string
	pid    int32
	medida medidaFechar
}

// cancelarMigracaoBloqueada reproduz um SIGTERM no meio de um Exec do migrate
// up, pela implementação real de abrirMigrador e de despachar.
//
// Como bloquear uma migração real por tempo longo: outra sessão cria
// migracao_aplicada numa transação aberta, no mesmo schema; o CREATE TABLE IF
// NOT EXISTS do executor não a enxerga, tenta inserir no catálogo e espera pelo
// COMMIT/ROLLBACK da outra transação (wait_event transactionid). Ele espera
// com o advisory lock de produção na mão e sem que nenhum timeout o encerre
// (lock 60 s, statement 30 min).
//
// atraso é a latência de cada conexão nova pelo proxy, incluindo a do
// CancelRequest. segurarCancelamento faz o proxy aceitar e não encaminhar
// conexões novas depois que a migração está bloqueada: o CancelRequest nunca
// chega ao servidor.
//
// O fechar real é embrulhado: o que se mede (pid vivo? advisory lock preso?)
// é lido na mesma goroutine, imediatamente depois de ele retornar e antes de o
// código de saída chegar ao chamador, que é o ponto em que main chama os.Exit.
func cancelarMigracaoBloqueada(t *testing.T, atraso time.Duration, segurarCancelamento bool) resultadoCancelamento {
	t.Helper()
	dsn, obs := schemaCLI(t)
	px, dsnProxy := novoProxyPG(t, dsn)
	px.atrasoNovas.Store(int64(atraso))

	// Segurador: transação aberta com a tabela de controle criada.
	blk, err := pgx.Connect(t.Context(), dsn)
	if err != nil {
		t.Fatalf("conectar o segurador: falhou (confira %s)", envURLBanco)
	}
	pidBlk := int32(blk.PgConn().PID())
	if _, err := blk.Exec(t.Context(), "BEGIN"); err != nil {
		t.Fatalf("BEGIN do segurador: %v", err)
	}
	if _, err := blk.Exec(t.Context(), "CREATE TABLE migracao_aplicada (versao integer)"); err != nil {
		t.Fatalf("CREATE TABLE do segurador: %v", err)
	}

	var res resultadoCancelamento
	// Limpeza: derruba o backend da migração se sobrou (o CancelRequest pode
	// nunca ter chegado), solta o segurador e só então deixa o DROP SCHEMA do
	// schemaCLI rodar.
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if res.pid != 0 {
			_, _ = obs.Exec(ctx, "SELECT pg_terminate_backend($1)", res.pid)
			for i := 0; i < 100; i++ {
				var n int
				if err := obs.QueryRow(ctx, "SELECT count(*) FROM pg_stat_activity WHERE pid = $1", res.pid).Scan(&n); err != nil || n == 0 {
					break
				}
				time.Sleep(50 * time.Millisecond)
			}
		}
		_, _ = blk.Exec(ctx, "ROLLBACK")
		_ = blk.Close(ctx)
	})

	pidc := make(chan int32, 1)
	var med medidaFechar
	orig := abrirMigrador
	t.Cleanup(func() { abrirMigrador = orig })
	abrirMigrador = func(ctx context.Context, cfg config.ConfigMigracao, logger *slog.Logger) (migrador, func(), erroBoot) {
		m, fechar, eb := orig(ctx, cfg, logger)
		if eb != semErro {
			return m, fechar, eb
		}
		return m, func() {
			ini := time.Now()
			fechar()
			med.duracao = time.Since(ini)

			var pid int32
			select {
			case pid = <-pidc:
			case <-time.After(10 * time.Second):
				med.consulta = errors.New("pid da migração não chegou")
				return
			}
			qctx, qcancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer qcancel()
			var vivos int
			if err := obs.QueryRow(qctx, "SELECT count(*) FROM pg_stat_activity WHERE pid = $1", pid).Scan(&vivos); err != nil {
				med.consulta = err
				return
			}
			med.pidVivo = vivos > 0
			med.consulta = obs.QueryRow(qctx,
				"SELECT count(*) FROM pg_locks WHERE pid = $1 AND locktype = 'advisory' AND granted", pid).Scan(&med.locks)
		}, semErro
	}

	ctx, cancel := context.WithCancel(context.Background())
	env := map[string]string{
		"DATABASE_URL":                         dsnProxy,
		"BIBLIOTECA_MIGRATE_LOCK_TIMEOUT":      "60s",
		"BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT": "30m",
	}
	var out, errb bytes.Buffer
	done := make(chan struct{})
	go func() {
		defer close(done)
		res.code = despachar(ctx, []string{"migrate", "up"}, ambiente(env), &out, &errb)
	}()
	t.Cleanup(func() {
		cancel()
		select {
		case <-done:
		case <-time.After(20 * time.Second):
			t.Error("despachar não terminou na limpeza do teste")
		}
	})

	// Espera a migração bloquear atrás do segurador.
	obsCtx := t.Context()
	limite := time.Now().Add(15 * time.Second)
	for {
		err := obs.QueryRow(obsCtx,
			"SELECT pid FROM pg_stat_activity WHERE application_name = $1 AND $2::int = ANY(pg_blocking_pids(pid))",
			applicationNameMigracao, pidBlk).Scan(&res.pid)
		if err == nil {
			break
		}
		if !errors.Is(err, pgx.ErrNoRows) {
			t.Fatalf("consultar pg_stat_activity: %v", err)
		}
		if time.Now().After(limite) {
			t.Fatal("a migração não chegou a ficar bloqueada atrás do segurador em 15 s")
		}
		time.Sleep(20 * time.Millisecond)
	}
	var presos int
	if err := obs.QueryRow(obsCtx, "SELECT count(*) FROM pg_locks WHERE pid = $1 AND locktype = 'advisory' AND granted", res.pid).Scan(&presos); err != nil {
		t.Fatalf("consultar pg_locks: %v", err)
	}
	if presos == 0 {
		t.Fatal("pré-condição: a migração bloqueada deveria segurar o advisory lock")
	}

	if segurarCancelamento {
		px.segurarNovas.Store(true)
	}
	pidc <- res.pid
	cancel() // o que o SIGTERM faz com o contexto
	select {
	case <-done:
	case <-time.After(30 * time.Second):
		t.Fatal("despachar não retornou 30 s depois do cancelamento")
	}
	res.stdout, res.stderr, res.medida = out.String(), errb.String(), med
	return res
}

// Ao cancelar o contexto no meio de um Exec, o pgx fecha a conexão por
// asyncClose: o CancelRequest e o Terminate saem numa goroutine, por uma
// conexão nova. O fechar do CLI tem de esperar essa limpeza (CleanupDone,
// limitada pelo prazo de fechamento): quando ele retorna, o backend já saiu e
// o advisory lock está livre. Sem a espera, main chama os.Exit antes de o
// CancelRequest sair e o servidor segue com o statement, e com o lock, até o
// fim dele.
//
// O proxy atrasa 300 ms cada conexão nova, então o CancelRequest só chega ao
// servidor 300 ms depois do cancelamento. Quem não espera falha sempre, não
// por sorte. A medida é imediata, sem polling.
func TestFecharEsperaOBackendCanceladoSair(t *testing.T) {
	r := cancelarMigracaoBloqueada(t, 300*time.Millisecond, false)

	if r.code != 1 {
		t.Errorf("saída = %d, want 1", r.code)
	}
	if r.stdout != "" {
		t.Errorf("stdout = %q, want vazio", r.stdout)
	}
	if erro := umaLinhaDeErro(t, r.stderr); erro != migrate.ErroCancelado.Error() {
		t.Errorf("campo erro = %q, want %q", erro, migrate.ErroCancelado.Error())
	}
	m := r.medida
	if m.consulta != nil {
		t.Fatalf("medir depois do fechar: %v", m.consulta)
	}
	if m.pidVivo {
		t.Errorf("o backend %d da migração continuava vivo quando fechar() retornou (levou %v): "+
			"o fechar não esperou o CancelRequest e o Terminate do pgx (conn.PgConn().CleanupDone())", r.pid, m.duracao)
	}
	if m.locks != 0 {
		t.Errorf("o advisory lock continuava preso (%d) quando fechar() retornou", m.locks)
	}
}

// Se a limpeza assíncrona travar (o CancelRequest nunca é atendido, e o pgx
// só desiste em 15 s), o fechar continua limitado pelo próprio prazo e não
// pendura a saída do processo. Com a espera por CleanupDone ele tem de
// respeitar o contexto do fechamento: prazoFecharMigracao, mais folga.
func TestFecharRespeitaOPrazoSeALimpezaTravar(t *testing.T) {
	r := cancelarMigracaoBloqueada(t, 0, true)

	if r.code != 1 {
		t.Errorf("saída = %d, want 1", r.code)
	}
	if erro := umaLinhaDeErro(t, r.stderr); erro != migrate.ErroCancelado.Error() {
		t.Errorf("campo erro = %q, want %q", erro, migrate.ErroCancelado.Error())
	}
	if teto := prazoFecharMigracao + 3*time.Second; r.medida.duracao > teto {
		t.Errorf("fechar() levou %v com a limpeza travada, want até %v (prazo de fechamento %v)",
			r.medida.duracao, teto, prazoFecharMigracao)
	}
}

// ---- sugestão 1: parâmetros pool_* da DSN ----

// comParametro acrescenta chave=valor à DSN (URL ou chave=valor).
func comParametro(t *testing.T, dsn string, kv ...string) string {
	t.Helper()
	if !strings.Contains(dsn, "://") {
		for i := 0; i+1 < len(kv); i += 2 {
			dsn += " " + kv[i] + "=" + kv[i+1]
		}
		return dsn
	}
	u, err := url.Parse(dsn)
	if err != nil {
		t.Fatalf("interpretar %s: falhou", envURLBanco)
	}
	q := u.Query()
	for i := 0; i+1 < len(kv); i += 2 {
		q.Set(kv[i], kv[i+1])
	}
	u.RawQuery = q.Encode()
	return u.String()
}

// O servidor HTTP sobe com a mesma DATABASE_URL, e o pgxpool consome os
// parâmetros pool_*. A conexão dedicada da migração não é um pool: se deixar
// esses parâmetros nos RuntimeParams da conexão, o PostgreSQL recusa o
// startup ("unrecognized configuration parameter") e o migrate falha com a
// mesma DSN que o servidor aceita.
func TestMigrateAceitaParametrosDePoolNaDSN(t *testing.T) {
	tests := []struct {
		name   string
		params []string
	}{
		{name: "pool_max_conns", params: []string{"pool_max_conns", "5"}},
		{name: "pool_min_conns", params: []string{"pool_min_conns", "1"}},
		{name: "pool_max_conn_lifetime", params: []string{"pool_max_conn_lifetime", "1h"}},
		{name: "pool_health_check_period", params: []string{"pool_health_check_period", "1m"}},
		{name: "pool_max_conn_idle_time", params: []string{"pool_max_conn_idle_time", "30m"}},
		{name: "pool_min_idle_conns", params: []string{"pool_min_idle_conns", "1"}},
		{name: "pool_max_conn_lifetime_jitter", params: []string{"pool_max_conn_lifetime_jitter", "1m"}},
		{name: "todos juntos", params: []string{
			"pool_max_conns", "5", "pool_min_conns", "1", "pool_max_conn_lifetime", "1h",
			"pool_health_check_period", "1m", "pool_max_conn_idle_time", "30m",
		}},
	}
	esperadas, err := migrate.ValidarFS(migrations.FS)
	if err != nil {
		t.Fatalf("ValidarFS(migrations.FS): %v", err)
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			dsn, _ := schemaCLI(t)
			env := map[string]string{"DATABASE_URL": comParametro(t, dsn, tt.params...)}

			code, stdout, stderr := rodar(t, []string{"migrate", "up"}, env)
			if code != 0 {
				t.Fatalf("migrate up: saída = %d, want 0 (stderr: %s)", code, stderr)
			}
			if want := fmt.Sprintf("migrate up: %d migração(ões) aplicada(s)\n", len(esperadas)); stdout != want {
				t.Errorf("migrate up: stdout = %q, want %q", stdout, want)
			}
			semSegredos(t, stdout+stderr, "pool_")

			code, stdout, stderr = rodar(t, []string{"migrate", "status"}, env)
			if code != 0 {
				t.Fatalf("migrate status: saída = %d, want 0 (stderr: %s)", code, stderr)
			}
			if want := "0001 esquema_emprestimo aplicada "; !strings.HasPrefix(stdout, want) {
				t.Errorf("migrate status: stdout = %q, want começar com %q", stdout, want)
			}
			semSegredos(t, stdout+stderr, "pool_")
		})
	}
}

// ---- sugestão 2: erro de COMMIT com tag inesperada cita o arquivo ----

// O proxy troca a tag do COMMIT que fecha a transação de um arquivo (o
// primeiro depois do INSERT em migracao_aplicada) por "ROLLBACK": o executor
// trata como transação desfeita pelo servidor. O contrato do Erro é citar o
// arquivo da migração; o erro precisa levá-lo, com a mensagem fixa de banco.
func TestMigrateUpCommitComTagInesperadaCitaOArquivo(t *testing.T) {
	dsn, _ := schemaCLI(t)
	px, dsnProxy := novoProxyPG(t, dsn)
	px.reescreverCommit.Store(true)

	esperadas, err := migrate.ValidarFS(migrations.FS)
	if err != nil || len(esperadas) == 0 {
		t.Fatalf("ValidarFS(migrations.FS) = %v, %v", esperadas, err)
	}
	primeira := fmt.Sprintf("%04d_%s.up.sql", esperadas[0].Versao, esperadas[0].Nome)

	code, stdout, stderr := rodar(t, []string{"migrate", "up"}, map[string]string{"DATABASE_URL": dsnProxy})
	if n := px.reescritos.Load(); n != 1 {
		t.Fatalf("pré-condição: o proxy reescreveu %d tags de COMMIT, want 1", n)
	}
	if code != 1 {
		t.Errorf("saída = %d, want 1", code)
	}
	if stdout != "" {
		t.Errorf("stdout = %q, want vazio", stdout)
	}
	erro := umaLinhaDeErro(t, stderr)
	if !strings.HasPrefix(erro, migrate.ErroBanco.Error()) {
		t.Errorf("campo erro = %q, want a mensagem fixa de banco %q", erro, migrate.ErroBanco.Error())
	}
	if !strings.Contains(erro, primeira) {
		t.Errorf("campo erro = %q, want citar o arquivo %q", erro, primeira)
	}
	semSegredos(t, stdout+stderr)
}

// ---- sugestão 4: nome vindo do banco no migrate status ----

// semControleNaSaida reprova se s tem byte de controle (< 0x20 exceto o \n de
// fim de linha, ou 0x7f) ou ESC.
func semControleNaSaida(t *testing.T, rotulo, s string) {
	t.Helper()
	for i := 0; i < len(s); i++ {
		if b := s[i]; (b < 0x20 && b != '\n') || b == 0x7f {
			t.Errorf("%s tem o byte de controle 0x%02x na posição %d: %q", rotulo, b, i, s)
			return
		}
	}
}

// Num controle adotado sem os CHECKs, o nome de uma versão desconhecida é texto
// arbitrário do banco. O que o migrate status imprime nunca pode quebrar a
// linha nem levar escape de terminal. Aceita-se qualquer destes contratos:
//
//	(a) o nome é sanitizado na impressão: saída sem controle, uma linha por
//	    versão (a desconhecida continua listada);
//	(b) lerRegistro recusa o nome: erro fixo, sem eco do conteúdo.
//
// Em ambos o código de saída é 1 (versão desconhecida no banco).
func TestMigrateStatusNomeArbitrarioDoBancoNaoQuebraASaida(t *testing.T) {
	tests := []struct {
		name string
		nome string
		// marca é um trecho do nome que não pode aparecer em (b).
		marca string
	}{
		{name: "quebra de linha forjando outra versao", nome: "ok\n0001 esquema_emprestimo aplicada", marca: "ok"},
		{name: "ESC e cor", nome: "VERMELHO\x1b[31mred\x1b[0m", marca: "VERMELHO"},
		{name: "retorno de carro", nome: "ANTES\rDEPOIS", marca: "ANTES"},
		{name: "titulo de janela", nome: "TITULO\x1b]0;hack\x07", marca: "TITULO"},
		{name: "DEL", nome: "ABC\x7fDEF", marca: "ABC"},
		{name: "tabulacao e fim de linha no fim", nome: "FIM\t\n", marca: "FIM"},
		{name: "todos juntos", nome: "A\nB\rC\x1b[31mD\x7fE", marca: "A"},
	}

	dsn, obs := schemaCLI(t)
	env := map[string]string{"DATABASE_URL": dsn, "BIBLIOTECA_MIGRATE_LOCK_TIMEOUT": "10s"}
	esperadas, err := migrate.ValidarFS(migrations.FS)
	if err != nil {
		t.Fatalf("ValidarFS(migrations.FS): %v", err)
	}
	n := len(esperadas)
	if code, _, stderr := rodar(t, []string{"migrate", "up"}, env); code != 0 {
		t.Fatalf("preparar: migrate up saiu %d (stderr: %s)", code, stderr)
	}
	// Controle adotado sem o CHECK do nome (a estrutura continua válida).
	if _, err := obs.Exec(t.Context(), "ALTER TABLE migracao_aplicada DROP CONSTRAINT migracao_aplicada_nome_check"); err != nil {
		t.Fatalf("remover o CHECK do nome: %v", err)
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if _, err := obs.Exec(t.Context(),
				"INSERT INTO migracao_aplicada (versao, nome, checksum) VALUES ($1, $2, repeat('a', 64))", n+1, tt.nome); err != nil {
				t.Fatalf("inserir a versão %d: %v", n+1, err)
			}
			t.Cleanup(func() {
				_, _ = obs.Exec(context.Background(), "DELETE FROM migracao_aplicada WHERE versao = $1", n+1)
			})

			code, stdout, stderr := rodar(t, []string{"migrate", "status"}, env)
			if code != 1 {
				t.Errorf("saída = %d, want 1 (versão desconhecida no banco)", code)
			}
			semControleNaSaida(t, "stdout", stdout)
			semControleNaSaida(t, "stderr", stderr)
			umaLinhaDeErro(t, stderr)

			if stdout == "" {
				// (b) recusa: sem eco do nome.
				if strings.Contains(stderr, tt.marca) {
					t.Errorf("stderr ecoa o nome do banco (%q): %s", tt.marca, stderr)
				}
				return
			}
			// (a) sanitiza: uma linha por versão, a desconhecida na última.
			if !strings.HasSuffix(stdout, "\n") {
				t.Errorf("stdout = %q, want terminado em \\n", stdout)
			}
			linhas := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
			if len(linhas) != n+1 {
				t.Fatalf("stdout com %d linhas, want %d (uma por versão): %q", len(linhas), n+1, stdout)
			}
			for i, l := range linhas {
				if want := fmt.Sprintf("%04d ", i+1); !strings.HasPrefix(l, want) {
					t.Errorf("linha %d = %q, want começar com %q", i+1, l, want)
				}
			}
			if !strings.Contains(linhas[n], "desconhecida") {
				t.Errorf("última linha = %q, want o estado desconhecida", linhas[n])
			}
		})
	}
}
