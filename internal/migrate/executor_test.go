package migrate_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/migrate"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/migrations"
)

// Testes de banco do executor (DEL-008). Sem BIBLIOTECA_TEST_DATABASE_URL eles
// são pulados. Nenhum usa t.Parallel nem a chave de lock de produção: o
// advisory lock é por banco, e cada teste usa uma chave aleatória.

// ocioso reprova se a conexão ficou com transação aberta ou abortada.
func ocioso(t *testing.T, c *pgx.Conn) {
	t.Helper()
	if st := c.PgConn().TxStatus(); st != 'I' {
		t.Errorf("TxStatus = %q, want 'I' (sem transação aberta)", st)
	}
}

// mostrar devolve o valor de SHOW <nome> na conexão.
func mostrar(t *testing.T, c *pgx.Conn, nome string) string {
	t.Helper()
	var v string
	if err := c.QueryRow(t.Context(), "SHOW "+nome).Scan(&v); err != nil {
		t.Fatalf("SHOW %s: %v", nome, err)
	}
	return v
}

// eventos interpreta o log JSON do slog (uma linha por evento).
func eventos(t *testing.T, log string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(log), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("linha de log não é JSON: %q", l)
		}
		out = append(out, m)
	}
	return out
}

func comMsg(evs []map[string]any, msg string) []map[string]any {
	var out []map[string]any
	for _, e := range evs {
		if e["msg"] == msg {
			out = append(out, e)
		}
	}
	return out
}

var reEscrita = regexp.MustCompile(`(?is)^\s*(insert|update|delete|create|drop|alter|truncate|grant|revoke|lock|comment|copy|do|call)\b`)

func escritas(sqls []string) []string {
	var out []string
	for _, s := range sqls {
		if reEscrita.MatchString(s) {
			out = append(out, s)
		}
	}
	return out
}

// novoAmbienteTracado é novoAmbiente com um tracador de SQL na conexão do
// executor.
func novoAmbienteTracado(t *testing.T) (*ambiente, *tracadorSQL) {
	t.Helper()
	b := novoBanco(t)
	tr := &tracadorSQL{}
	cfg := b.config(t)
	cfg.Tracer = tr
	a := &ambiente{b: b, conn: b.conectarCfg(t, cfg), obs: b.conectar(t), chave: chaveAleatoria(t), log: &bufLog{}}
	return a, tr
}

func TestEstadosTemTextoEstavel(t *testing.T) {
	for got, want := range map[migrate.Estado]string{
		migrate.EstadoAplicada:     "aplicada",
		migrate.EstadoPendente:     "pendente",
		migrate.EstadoDivergente:   "divergente",
		migrate.EstadoDesconhecida: "desconhecida",
	} {
		if string(got) != want {
			t.Errorf("Estado %q, want %q", got, want)
		}
	}
}

// ---- banco vazio: aplica a 0001 de verdade ----

func TestSubirBancoVazioAplicaA0001(t *testing.T) {
	a, tr := novoAmbienteTracado(t)
	e := a.exec(realFS())

	esperado, err := migrate.ValidarFS(realFS())
	if err != nil {
		t.Fatalf("ValidarFS(migrations.FS): %v", err)
	}
	up0001, err := fs.ReadFile(migrations.FS, "0001_esquema_emprestimo.up.sql")
	if err != nil {
		t.Fatalf("ler 0001: %v", err)
	}

	got, err := e.Subir(t.Context())
	if err != nil {
		t.Fatalf("Subir() erro = %v", err)
	}
	semDetalhe(t, err)
	if len(got) != len(esperado) {
		t.Fatalf("Subir() aplicou %d migrações, want %d", len(got), len(esperado))
	}
	for i := range got {
		if got[i].Versao != esperado[i].Versao || got[i].Nome != esperado[i].Nome || got[i].Checksum != esperado[i].Checksum {
			t.Errorf("Subir()[%d] = %+v, want %+v", i, got[i], esperado[i])
		}
	}

	reg := registro(t, a.obs)
	if len(reg) != len(esperado) {
		t.Fatalf("registro = %+v, want %d linhas", reg, len(esperado))
	}
	if reg[0].Versao != 1 || reg[0].Nome != "esquema_emprestimo" || reg[0].Checksum != sha(string(up0001)) {
		t.Errorf("registro[0] = %+v, want 1/esquema_emprestimo/sha256 do .up.sql embutido", reg[0])
	}
	for _, tab := range []string{"livro", "usuario", "emprestimo", "migracao_aplicada"} {
		if !existeTabela(t, a.obs, tab) {
			t.Errorf("tabela %s não existe depois do Subir", tab)
		}
	}
	ocioso(t, a.conn)
	a.esperarLockLivre(t)

	evs := eventos(t, a.log.String())
	if n := len(comMsg(evs, "lock adquirido")); n != 1 {
		t.Errorf("eventos 'lock adquirido' = %d, want 1", n)
	}
	aplicadas := comMsg(evs, "migração aplicada")
	if len(aplicadas) != len(esperado) {
		t.Fatalf("eventos 'migração aplicada' = %d, want %d", len(aplicadas), len(esperado))
	}
	if v, _ := aplicadas[0]["versao"].(float64); v != 1 {
		t.Errorf("evento versao = %v, want 1", aplicadas[0]["versao"])
	}
	if aplicadas[0]["nome"] != "esquema_emprestimo" {
		t.Errorf("evento nome = %v, want esquema_emprestimo", aplicadas[0]["nome"])
	}
	if d, ok := aplicadas[0]["duracao_ms"].(float64); !ok || d < 0 {
		t.Errorf("evento duracao_ms = %v, want número >= 0", aplicadas[0]["duracao_ms"])
	}
	for _, ev := range evs {
		if ev["level"] == "ERROR" {
			t.Errorf("executor registrou erro no log (%v): quem loga o erro é o main", ev)
		}
	}

	// Segunda execução: no-op e sem escrita.
	antes := registro(t, a.obs)
	logAntes := len(eventos(t, a.log.String()))
	tr.limpar()
	got2, err := e.Subir(t.Context())
	if err != nil {
		t.Fatalf("segundo Subir() erro = %v", err)
	}
	if len(got2) != 0 {
		t.Errorf("segundo Subir() = %+v, want vazio", got2)
	}
	depois := registro(t, a.obs)
	if len(antes) != len(depois) {
		t.Fatalf("registro mudou de %d para %d linhas", len(antes), len(depois))
	}
	for i := range antes {
		if antes[i] != depois[i] {
			t.Errorf("linha %d mudou no no-op: %+v -> %+v (xmin/aplicada_em devem ficar iguais)", i, antes[i], depois[i])
		}
	}
	sqls, _ := tr.copia()
	if w := escritas(sqls); len(w) != 0 {
		t.Errorf("segundo Subir enviou SQL de escrita: %q", w)
	}
	for _, ev := range eventos(t, a.log.String())[logAntes:] {
		if ev["msg"] == "migração aplicada" {
			t.Errorf("segundo Subir registrou 'migração aplicada': %v", ev)
		}
	}
	ocioso(t, a.conn)
	a.esperarLockLivre(t)
}

// Logger nil vira descarte: não pode haver pânico.
func TestSubirComLoggerNil(t *testing.T) {
	a := novoAmbiente(t)
	e := a.exec(novoFS(mig{V: 1, Nome: "a", Up: "CREATE TABLE a (x integer);\n"}), func(o *migrate.Opcoes) { o.Logger = nil })
	got, err := e.Subir(t.Context())
	if err != nil || len(got) != 1 {
		t.Fatalf("Subir() = %+v, %v, want 1 migração e nil", got, err)
	}
}

// Controle informado explicitamente em Opcoes equivale ao padrão embutido.
func TestSubirComControleExplicito(t *testing.T) {
	a := novoAmbiente(t)
	e := a.exec(novoFS(mig{V: 1, Nome: "a", Up: "CREATE TABLE a (x integer);\n"}), func(o *migrate.Opcoes) { o.Controle = migrations.Controle })
	if _, err := e.Subir(t.Context()); err != nil {
		t.Fatalf("Subir() erro = %v", err)
	}
	if rs := registro(t, a.obs); len(rs) != 1 {
		t.Errorf("registro = %+v, want 1 linha", rs)
	}
}

// ---- ordem e retomada ----

func TestSubirAplicaEmOrdemComDependencias(t *testing.T) {
	a := novoAmbiente(t)
	fsys := novoFS(
		mig{V: 1, Nome: "a", Up: "CREATE TABLE a (id integer PRIMARY KEY);\n"},
		mig{V: 2, Nome: "b", Up: "CREATE TABLE b (a_id integer REFERENCES a (id));\n"},
		mig{V: 3, Nome: "c", Up: "ALTER TABLE b ADD COLUMN c integer;\n"},
	)
	got, err := a.exec(fsys).Subir(t.Context())
	if err != nil {
		t.Fatalf("Subir() erro = %v", err)
	}
	if len(got) != 3 || got[0].Versao != 1 || got[1].Versao != 2 || got[2].Versao != 3 {
		t.Fatalf("Subir() = %+v, want versões 1,2,3 em ordem", got)
	}
	rs := registro(t, a.obs)
	if len(rs) != 3 {
		t.Fatalf("registro = %+v", rs)
	}
	for i := 1; i < len(rs); i++ {
		if rs[i].AplicadaEm.Before(rs[i-1].AplicadaEm) {
			t.Errorf("aplicada_em da versão %d anterior à da %d", rs[i].Versao, rs[i-1].Versao)
		}
	}
	for i, r := range rs {
		if r.Checksum != sha(string(fsys[mig{V: r.Versao, Nome: r.Nome}.arquivo("up")].Data)) {
			t.Errorf("registro[%d].Checksum = %s, want sha256 do .up.sql", i, r.Checksum)
		}
	}
}

func TestSubirSoAplicaOsPendentes(t *testing.T) {
	a := novoAmbiente(t)
	todas := tresMigracoes()
	if _, err := a.exec(novoFS(todas[:2]...)).Subir(t.Context()); err != nil {
		t.Fatalf("primeiro Subir() erro = %v", err)
	}
	antes := registro(t, a.obs)
	got, err := a.exec(novoFS(todas...)).Subir(t.Context())
	if err != nil {
		t.Fatalf("segundo Subir() erro = %v", err)
	}
	if len(got) != 1 || got[0].Versao != 3 {
		t.Errorf("segundo Subir() = %+v, want só a versão 3", got)
	}
	depois := registro(t, a.obs)
	if len(depois) != 3 {
		t.Fatalf("registro = %+v, want 3 linhas", depois)
	}
	for i := 0; i < 2; i++ {
		if antes[i] != depois[i] {
			t.Errorf("linha %d mudou: %+v -> %+v", i, antes[i], depois[i])
		}
	}
}

func TestSubirFalhaNoMeioFazRollbackSoDoArquivoQueFalhou(t *testing.T) {
	tests := []struct {
		name       string
		upB        string
		code       string
		constraint string
	}{
		{name: "sintaxe invalida no fim", upB: "CREATE TABLE b (x integer);\nSELEC 1;\n", code: "42601"},
		{name: "divisao por zero depois de DDL", upB: "CREATE TABLE b (x integer);\nINSERT INTO b VALUES (1);\nSELECT 1/0;\n", code: "22012"},
		{name: "violacao de check", upB: "CREATE TABLE b (x integer CONSTRAINT b_x_check CHECK (x > 0));\nINSERT INTO b VALUES (-1);\n", code: "23514", constraint: "b_x_check"},
		{name: "objeto inexistente", upB: "CREATE TABLE b (x integer);\nALTER TABLE nao_existe ADD COLUMN y integer;\n", code: "42P01"},
		{name: "falha no primeiro comando", upB: "SELEC 1; CREATE TABLE b (x integer);\n", code: "42601"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := novoAmbiente(t)
			ms := tresMigracoes()
			ms[1].Up = tt.upB
			got, err := a.exec(novoFS(ms...)).Subir(t.Context())
			if err == nil {
				t.Fatal("Subir() sem erro, want ErroSQL")
			}
			if !errors.Is(err, migrate.ErroSQL) {
				t.Fatalf("erro = %v, want ErroSQL", err)
			}
			semDetalhe(t, err)
			me := erroDe(t, err)
			if me.Arquivo != "0002_b.up.sql" || me.Code != tt.code || me.Constraint != tt.constraint {
				t.Errorf("erro = {Arquivo:%q Code:%q Constraint:%q}, want {0002_b.up.sql %s %s}", me.Arquivo, me.Code, me.Constraint, tt.code, tt.constraint)
			}
			if !strings.Contains(err.Error(), "0002_b.up.sql") {
				t.Errorf("erro %q, want conter o nome do arquivo", err.Error())
			}
			// Aplicadas antes da falha: a 1.
			if len(got) != 1 || got[0].Versao != 1 {
				t.Errorf("Subir() devolveu %+v junto com o erro, want só a versão 1", got)
			}
			if v := versoes(registro(t, a.obs)); len(v) != 1 || v[0] != 1 {
				t.Errorf("registro = %v, want [1]", v)
			}
			if !existeTabela(t, a.obs, "a") {
				t.Error("tabela a (arquivo 1, já commitado) sumiu")
			}
			for _, tab := range []string{"b", "c"} {
				if existeTabela(t, a.obs, tab) {
					t.Errorf("tabela %s existe: efeito parcial do arquivo que falhou ou aplicação do seguinte", tab)
				}
			}
			ocioso(t, a.conn)
			a.esperarLockLivre(t)

			// Retoma da 2: só 2 e 3 são aplicadas, a 1 fica como estava.
			antes := registro(t, a.obs)
			ms[1].Up = "CREATE TABLE b (x integer);\n"
			got, err = a.exec(novoFS(ms...)).Subir(t.Context())
			if err != nil {
				t.Fatalf("Subir() de retomada erro = %v", err)
			}
			if len(got) != 2 || got[0].Versao != 2 || got[1].Versao != 3 {
				t.Errorf("retomada aplicou %+v, want versões 2 e 3", got)
			}
			depois := registro(t, a.obs)
			if len(depois) != 3 || depois[0] != antes[0] {
				t.Errorf("registro depois da retomada = %+v (antes %+v)", depois, antes)
			}
		})
	}
}

func TestSubirFalhaNaPrimeiraNaoDeixaNada(t *testing.T) {
	a := novoAmbiente(t)
	ms := tresMigracoes()
	ms[0].Up = "CREATE TABLE a (x integer);\nSELECT 1/0;\n"
	_, err := a.exec(novoFS(ms...)).Subir(t.Context())
	if !errors.Is(err, migrate.ErroSQL) {
		t.Fatalf("erro = %v, want ErroSQL", err)
	}
	semAplicar(t, a.obs)
}

// ---- timeouts ----

func TestSubirStatementTimeout(t *testing.T) {
	a := novoAmbiente(t)
	ms := []mig{
		{V: 1, Nome: "a", Up: "CREATE TABLE a (x integer);\n"},
		{V: 2, Nome: "lenta", Up: "CREATE TABLE b (x integer);\nSELECT pg_sleep(10);\n"},
	}
	inicio := time.Now()
	got, err := a.exec(novoFS(ms...), func(o *migrate.Opcoes) { o.StatementTimeout = 300 * time.Millisecond }).Subir(t.Context())
	d := time.Since(inicio)
	if !errors.Is(err, migrate.ErroSQL) {
		t.Fatalf("erro = %v, want ErroSQL", err)
	}
	semDetalhe(t, err)
	if me := erroDe(t, err); me.Code != "57014" || me.Arquivo != "0002_lenta.up.sql" {
		t.Errorf("erro = {Arquivo:%q Code:%q}, want {0002_lenta.up.sql 57014}", me.Arquivo, me.Code)
	}
	if d > 8*time.Second {
		t.Errorf("Subir levou %v: statement_timeout não interrompeu (pg_sleep de 10s)", d)
	}
	if len(got) != 1 {
		t.Errorf("Subir() = %+v, want só a versão 1", got)
	}
	if v := versoes(registro(t, a.obs)); len(v) != 1 || v[0] != 1 {
		t.Errorf("registro = %v, want [1]", v)
	}
	if existeTabela(t, a.obs, "b") {
		t.Error("tabela b existe: o rollback do arquivo que estourou o prazo não aconteceu")
	}
	ocioso(t, a.conn)
	a.esperarLockLivre(t)
	if _, err := a.conn.Exec(t.Context(), "SELECT 1"); err != nil {
		t.Errorf("conexão inutilizável depois do statement_timeout: %v", err)
	}
}

func TestSubirLockTimeoutNoDDL(t *testing.T) {
	a := novoAmbiente(t)
	ms := []mig{{V: 1, Nome: "a", Up: "CREATE TABLE alvo (x integer);\n"}}
	if _, err := a.exec(novoFS(ms...)).Subir(t.Context()); err != nil {
		t.Fatalf("Subir() da 0001: %v", err)
	}
	ms = append(ms, mig{V: 2, Nome: "b", Up: "ALTER TABLE alvo ADD COLUMN c integer;\n"})

	// Outra sessão segura ACCESS EXCLUSIVE em alvo.
	seg := a.b.conectar(t)
	tx, err := seg.Begin(t.Context())
	if err != nil {
		t.Fatalf("begin: %v", err)
	}
	t.Cleanup(func() { _ = tx.Rollback(context.Background()) })
	if _, err := tx.Exec(t.Context(), "LOCK TABLE alvo IN ACCESS EXCLUSIVE MODE"); err != nil {
		t.Fatalf("LOCK TABLE: %v", err)
	}

	inicio := time.Now()
	got, err := a.exec(novoFS(ms...), func(o *migrate.Opcoes) { o.LockTimeout = 400 * time.Millisecond }).Subir(t.Context())
	d := time.Since(inicio)
	if !errors.Is(err, migrate.ErroSQL) {
		t.Fatalf("erro = %v, want ErroSQL", err)
	}
	semDetalhe(t, err)
	if me := erroDe(t, err); me.Code != "55P03" || me.Arquivo != "0002_b.up.sql" {
		t.Errorf("erro = {Arquivo:%q Code:%q}, want {0002_b.up.sql 55P03}", me.Arquivo, me.Code)
	}
	if d < 300*time.Millisecond || d > 8*time.Second {
		t.Errorf("Subir levou %v, want ≈ lock_timeout (400ms)", d)
	}
	if len(got) != 0 {
		t.Errorf("Subir() = %+v, want vazio", got)
	}
	if v := versoes(registro(t, a.obs)); len(v) != 1 {
		t.Errorf("registro = %v, want só a versão 1", v)
	}
	ocioso(t, a.conn)
	a.esperarLockLivre(t)

	// Liberado o lock da tabela, a mesma migração passa.
	if err := tx.Rollback(t.Context()); err != nil {
		t.Fatalf("rollback do segurador: %v", err)
	}
	got, err = a.exec(novoFS(ms...)).Subir(t.Context())
	if err != nil || len(got) != 1 || got[0].Versao != 2 {
		t.Fatalf("Subir() depois de liberar = %+v, %v, want só a versão 2", got, err)
	}
}

// Os timeouts valem só dentro da transação de cada arquivo: a migração os
// enxerga e a sessão não os guarda.
func TestSubirTimeoutsSaoLocaisATransacao(t *testing.T) {
	a, tr := novoAmbienteTracado(t)
	ctx := t.Context()
	if _, err := a.conn.Exec(ctx, "SET lock_timeout = '7s'; SET statement_timeout = '9s'"); err != nil {
		t.Fatalf("SET: %v", err)
	}
	lock0, stmt0 := mostrar(t, a.conn, "lock_timeout"), mostrar(t, a.conn, "statement_timeout")
	if lock0 != "7s" || stmt0 != "9s" {
		t.Fatalf("pré-condição: lock=%s statement=%s", lock0, stmt0)
	}
	checar := func(rotulo string) {
		t.Helper()
		if v := mostrar(t, a.conn, "lock_timeout"); v != lock0 {
			t.Errorf("%s: lock_timeout da sessão = %s, want %s (vazou)", rotulo, v, lock0)
		}
		if v := mostrar(t, a.conn, "statement_timeout"); v != stmt0 {
			t.Errorf("%s: statement_timeout da sessão = %s, want %s (vazou)", rotulo, v, stmt0)
		}
	}

	ms := []mig{{V: 1, Nome: "cfg", Up: "CREATE TABLE cfg AS SELECT current_setting('lock_timeout') AS lt, current_setting('statement_timeout') AS st;\n"}}
	tr.limpar()
	opts := func(o *migrate.Opcoes) {
		o.LockTimeout, o.StatementTimeout = 1500*time.Millisecond, 3500*time.Millisecond
	}
	if _, err := a.exec(novoFS(ms...), opts).Subir(ctx); err != nil {
		t.Fatalf("Subir() erro = %v", err)
	}
	var lt, st string
	if err := a.obs.QueryRow(ctx, "SELECT lt, st FROM cfg").Scan(&lt, &st); err != nil {
		t.Fatalf("ler cfg: %v", err)
	}
	if lt != "1500ms" || st != "3500ms" {
		t.Errorf("dentro da migração lock_timeout=%s statement_timeout=%s, want 1500ms e 3500ms", lt, st)
	}
	checar("depois do sucesso")

	// Valores entram por bind param, nunca interpolados no texto SQL.
	sqls, args := tr.copia()
	achou := false
	for i, s := range sqls {
		if !strings.Contains(strings.ToLower(s), "set_config") {
			continue
		}
		if strings.Contains(s, "1500ms") || strings.Contains(s, "3500ms") {
			t.Errorf("set_config com o valor no texto SQL: %q", s)
		}
		for _, arg := range args[i] {
			if arg == "1500ms" || arg == "3500ms" {
				achou = true
			}
		}
	}
	if !achou {
		t.Errorf("nenhum set_config com os valores 1500ms/3500ms por bind param; SQL enviado: %q", sqls)
	}

	// Depois de uma falha também não vaza.
	ms = append(ms, mig{V: 2, Nome: "falha", Up: "SELECT 1/0;\n"})
	if _, err := a.exec(novoFS(ms...), opts).Subir(ctx); !errors.Is(err, migrate.ErroSQL) {
		t.Fatalf("erro = %v, want ErroSQL", err)
	}
	checar("depois da falha")
}

// ---- advisory lock ----

func TestSubirLockOcupado(t *testing.T) {
	a := novoAmbiente(t)
	liberar := a.segurarLock(t)
	defer liberar()

	inicio := time.Now()
	got, err := a.exec(novoFS(tresMigracoes()...), func(o *migrate.Opcoes) { o.LockTimeout = 500 * time.Millisecond }).Subir(t.Context())
	d := time.Since(inicio)
	if !errors.Is(err, migrate.ErrLockOcupado) {
		t.Fatalf("erro = %v, want ErrLockOcupado", err)
	}
	semDetalhe(t, err)
	if d < 400*time.Millisecond || d > 4*time.Second {
		t.Errorf("Subir levou %v, want ≈ LockTimeout (500ms)", d)
	}
	if len(got) != 0 {
		t.Errorf("Subir() = %+v, want vazio", got)
	}
	// O banco não muda: nem o controle é criado (o lock vem antes).
	if r := relacoes(t, a.obs, false); len(r) != 0 {
		t.Errorf("relações = %v, want nenhuma", r)
	}
	ocioso(t, a.conn)

	// Quando a outra sessão solta, o mesmo executor segue normalmente.
	liberar()
	if got, err := a.exec(novoFS(tresMigracoes()...)).Subir(t.Context()); err != nil || len(got) != 3 {
		t.Fatalf("Subir() depois de liberar = %+v, %v", got, err)
	}
}

func TestSubirLiberaOLockEmSucessoEEmErro(t *testing.T) {
	tests := []struct {
		name string
		fsys fs.FS
		want error
	}{
		{name: "sucesso", fsys: novoFS(tresMigracoes()...), want: nil},
		{name: "erro de SQL", fsys: novoFS(mig{V: 1, Nome: "a", Up: "SELECT 1/0;"}), want: migrate.ErroSQL},
		{name: "FS invalido", fsys: novoFS(mig{V: 2, Nome: "a", Up: "SELECT 1;"}), want: migrate.ErroFSInvalido},
		{name: "concurrently", fsys: novoFS(mig{V: 1, Nome: "a", Up: "CREATE INDEX CONCURRENTLY x ON y (z);"}), want: migrate.ErroConcurrently},
		{name: "transacao encerrada", fsys: novoFS(mig{V: 1, Nome: "a", Up: "SELECT 1; COMMIT;"}), want: migrate.ErroTransacaoEncerrada},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := novoAmbiente(t)
			_, err := a.exec(tt.fsys).Subir(t.Context())
			if tt.want == nil && err != nil {
				t.Fatalf("Subir() erro = %v", err)
			}
			if tt.want != nil && !errors.Is(err, tt.want) {
				t.Fatalf("erro = %v, want %v", err, tt.want)
			}
			a.esperarLockLivre(t)
			ocioso(t, a.conn)
		})
	}
}

func TestSubirComErroDeRegistroLiberaOLock(t *testing.T) {
	a := novoAmbiente(t)
	if _, err := a.exec(novoFS(tresMigracoes()...)).Subir(t.Context()); err != nil {
		t.Fatalf("Subir() inicial: %v", err)
	}
	ms := tresMigracoes()
	ms[1].Up = "CREATE TABLE b (x integer, y integer);\n"
	_, err := a.exec(novoFS(ms...)).Subir(t.Context())
	if !errors.Is(err, migrate.ErroChecksumDivergente) {
		t.Fatalf("erro = %v, want ErroChecksumDivergente", err)
	}
	a.esperarLockLivre(t)
	ocioso(t, a.conn)
}

func TestSubirRespeitaCancelamentoEsperandoOLock(t *testing.T) {
	a := novoAmbiente(t)
	liberar := a.segurarLock(t)
	defer liberar()

	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		time.Sleep(300 * time.Millisecond)
		cancel()
	}()
	inicio := time.Now()
	_, err := a.exec(novoFS(tresMigracoes()...), func(o *migrate.Opcoes) { o.LockTimeout = 30 * time.Second }).Subir(ctx)
	d := time.Since(inicio)
	if !errors.Is(err, migrate.ErroCancelado) {
		t.Fatalf("erro = %v, want ErroCancelado", err)
	}
	semDetalhe(t, err)
	if d > 5*time.Second {
		t.Errorf("Subir levou %v para obedecer ao cancelamento (LockTimeout era 30s)", d)
	}
	if r := relacoes(t, a.obs, false); len(r) != 0 {
		t.Errorf("relações = %v, want nenhuma", r)
	}
}

func TestSubirRespeitaCancelamentoNoMeioDeUmaMigracao(t *testing.T) {
	a := novoAmbiente(t)
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	go func() {
		time.Sleep(400 * time.Millisecond)
		cancel()
	}()
	ms := []mig{
		{V: 1, Nome: "a", Up: "CREATE TABLE a (x integer);\n"},
		{V: 2, Nome: "lenta", Up: "CREATE TABLE b (x integer);\nSELECT pg_sleep(20);\n"},
	}
	inicio := time.Now()
	got, err := a.exec(novoFS(ms...), func(o *migrate.Opcoes) { o.StatementTimeout = 60 * time.Second }).Subir(ctx)
	d := time.Since(inicio)
	if !errors.Is(err, migrate.ErroCancelado) {
		t.Fatalf("erro = %v, want ErroCancelado", err)
	}
	semDetalhe(t, err)
	if d > 8*time.Second {
		t.Errorf("Subir levou %v para obedecer ao cancelamento (pg_sleep de 20s)", d)
	}
	if len(got) != 1 || got[0].Versao != 1 {
		t.Errorf("Subir() = %+v, want só a versão 1 (a 2 não terminou)", got)
	}
	// O pgx pode fechar a conexão no cancelamento; o lock cai de qualquer jeito.
	a.esperarLockLivre(t)
	if v := versoes(registro(t, a.obs)); len(v) != 1 || v[0] != 1 {
		t.Errorf("registro = %v, want [1]", v)
	}
	if existeTabela(t, a.obs, "b") {
		t.Error("tabela b existe: efeito da migração cancelada")
	}
}

func TestSubirConcorrenteNaoDuplicaNada(t *testing.T) {
	b := novoBanco(t)
	obs := b.conectar(t)
	chave := chaveAleatoria(t)
	const n = 4

	ms := tresMigracoes()
	ms[0].Up = "CREATE TABLE a (x integer);\nSELECT pg_sleep(0.3);\n"
	fsys := novoFS(ms...)

	type res struct {
		got []migrate.Migracao
		err error
	}
	resultados := make([]res, n)
	conns := make([]*pgx.Conn, n)
	for i := range conns {
		conns[i] = b.conectar(t)
	}
	var wg sync.WaitGroup
	largada := make(chan struct{})
	for i := 0; i < n; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			e := migrate.Novo(conns[i], fsys, migrate.Opcoes{LockTimeout: 30 * time.Second, StatementTimeout: 30 * time.Second, ChaveLock: chave})
			<-largada
			resultados[i].got, resultados[i].err = e.Subir(context.Background())
		}()
	}
	close(largada)
	wg.Wait()

	total := 0
	for i, r := range resultados {
		if r.err != nil {
			t.Errorf("executor %d: erro = %v", i, r.err)
		}
		total += len(r.got)
	}
	if total != 3 {
		t.Errorf("soma das migrações aplicadas = %d, want 3 (cada versão uma vez só)", total)
	}
	reg := registro(t, obs)
	if v := versoes(reg); len(v) != 3 || v[0] != 1 || v[1] != 2 || v[2] != 3 {
		t.Errorf("registro = %v, want [1 2 3]", v)
	}
}

// ---- FS inválido: nenhum efeito no banco ----

func TestSubirFSInvalidoNaoTocaNoBanco(t *testing.T) {
	a := novoAmbiente(t)
	for _, tt := range casosFSInvalido {
		t.Run(tt.name, func(t *testing.T) {
			got, err := a.exec(tt.fsys).Subir(t.Context())
			if !errors.Is(err, migrate.ErroFSInvalido) {
				t.Fatalf("Subir() = %+v, %v, want ErroFSInvalido", got, err)
			}
			semDetalhe(t, err)
			if len(got) != 0 {
				t.Errorf("Subir() devolveu %+v com erro", got)
			}
			if tt.arquivo != "" && !citaArquivo(err.Error(), tt.arquivo) {
				t.Errorf("erro %q, want conter %q (ou o seu par)", err.Error(), tt.arquivo)
			}
			// Nem o controle pode ter sido criado: a validação vem antes de
			// qualquer efeito.
			if r := relacoes(t, a.obs, false); len(r) != 0 {
				t.Errorf("relações = %v, want nenhuma", r)
			}
			ocioso(t, a.conn)
			if !a.lockLivreAgora(t) {
				t.Error("advisory lock preso")
			}
		})
	}
	if evs := comMsg(eventos(t, a.log.String()), "lock adquirido"); len(evs) != 0 {
		t.Errorf("FS inválido adquiriu o lock (%d eventos)", len(evs))
	}
}

// ---- CONCURRENTLY ----

func TestSubirRecusaConcurrently(t *testing.T) {
	variantes := []struct {
		name string
		sql  string
	}{
		{name: "maiusculas", sql: "CREATE TABLE t (x integer);\nCREATE INDEX CONCURRENTLY t_x ON t (x);\n"},
		{name: "minusculas", sql: "CREATE TABLE t (x integer);\ncreate index concurrently t_x on t (x);\n"},
		{name: "capitalizacao mista", sql: "CREATE TABLE t (x integer);\nCREATE INDEX CoNcUrReNtLy t_x ON t (x);\n"},
		{name: "comentario de linha", sql: "-- depois usar concurrently\nCREATE TABLE t (x integer);\n"},
		{name: "comentario de bloco", sql: "CREATE TABLE t (x integer); /* CONCURRENTLY */\n"},
		{name: "no fim sem quebra de linha", sql: "CREATE TABLE t (x integer);\n-- Concurrently"},
		{name: "dentro de literal", sql: "CREATE TABLE t (x text DEFAULT 'concurrently');\n"},
	}
	a := novoAmbiente(t)
	for _, v := range variantes {
		t.Run("arquivo unico/"+v.name, func(t *testing.T) {
			got, err := a.exec(novoFS(mig{V: 1, Nome: "idx", Up: v.sql})).Subir(t.Context())
			if !errors.Is(err, migrate.ErroConcurrently) {
				t.Fatalf("Subir() = %+v, %v, want ErroConcurrently", got, err)
			}
			semDetalhe(t, err)
			if me := erroDe(t, err); me.Arquivo != "0001_idx.up.sql" {
				t.Errorf("Arquivo = %q, want 0001_idx.up.sql", me.Arquivo)
			}
			if !strings.Contains(err.Error(), "0001_idx.up.sql") {
				t.Errorf("erro %q, want conter o nome do arquivo", err.Error())
			}
			if strings.Contains(err.Error(), "t_x") {
				t.Errorf("erro %q cita conteúdo do SQL", err.Error())
			}
			semAplicar(t, a.obs)
			ocioso(t, a.conn)
		})
	}

	// A pré-validação cobre todos os pendentes: nada do arquivo 1 é aplicado
	// quando o CONCURRENTLY está no 2 ou no 3.
	for _, pos := range []int{1, 2} {
		t.Run("pre-validacao de todos os pendentes/posicao "+string(rune('1'+pos)), func(t *testing.T) {
			a := novoAmbiente(t)
			ms := tresMigracoes()
			ms[pos].Up = "CREATE INDEX CONCURRENTLY ix ON a (x);\n"
			got, err := a.exec(novoFS(ms...)).Subir(t.Context())
			if !errors.Is(err, migrate.ErroConcurrently) {
				t.Fatalf("Subir() = %+v, %v, want ErroConcurrently", got, err)
			}
			if me := erroDe(t, err); me.Arquivo != ms[pos].arquivo("up") {
				t.Errorf("Arquivo = %q, want %q", me.Arquivo, ms[pos].arquivo("up"))
			}
			if len(got) != 0 {
				t.Errorf("Subir() aplicou %+v antes de recusar", got)
			}
			semAplicar(t, a.obs) // a tabela a do arquivo 1 não pode existir
			a.esperarLockLivre(t)
		})
	}

	t.Run("com migracoes ja aplicadas", func(t *testing.T) {
		a := novoAmbiente(t)
		ms := tresMigracoes()
		if _, err := a.exec(novoFS(ms[:1]...)).Subir(t.Context()); err != nil {
			t.Fatalf("Subir() da 1: %v", err)
		}
		ms[2].Up = "CREATE INDEX CONCURRENTLY ix ON a (x);\n"
		_, err := a.exec(novoFS(ms...)).Subir(t.Context())
		if !errors.Is(err, migrate.ErroConcurrently) {
			t.Fatalf("erro = %v, want ErroConcurrently", err)
		}
		if v := versoes(registro(t, a.obs)); len(v) != 1 || v[0] != 1 {
			t.Errorf("registro = %v, want [1]: a 2 não pode ter sido aplicada", v)
		}
		if existeTabela(t, a.obs, "b") {
			t.Error("tabela b existe: a pré-validação deveria barrar antes de aplicar a 2")
		}
	})
}

// ---- COMMIT/BEGIN próprio no arquivo ----

func TestSubirArquivoQueEncerraATransacao(t *testing.T) {
	tests := []struct {
		name string
		up   string
		// COMMIT deixa o efeito (já commitado); ROLLBACK não.
		tabelaExiste bool
	}{
		{name: "COMMIT no fim", up: "CREATE TABLE t (x integer);\nCOMMIT;\n", tabelaExiste: true},
		{name: "ROLLBACK no fim", up: "CREATE TABLE t (x integer);\nROLLBACK;\n", tabelaExiste: false},
		{name: "COMMIT no meio", up: "CREATE TABLE t (x integer);\nCOMMIT;\nSELECT 1;\n", tabelaExiste: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := novoAmbiente(t)
			ms := []mig{
				{V: 1, Nome: "tx", Up: tt.up},
				{V: 2, Nome: "seguinte", Up: "CREATE TABLE seguinte (x integer);\n"},
			}
			got, err := a.exec(novoFS(ms...)).Subir(t.Context())
			if !errors.Is(err, migrate.ErroTransacaoEncerrada) {
				t.Fatalf("Subir() = %+v, %v, want ErroTransacaoEncerrada", got, err)
			}
			semDetalhe(t, err)
			if me := erroDe(t, err); me.Arquivo != "0001_tx.up.sql" {
				t.Errorf("Arquivo = %q, want 0001_tx.up.sql", me.Arquivo)
			}
			if len(got) != 0 {
				t.Errorf("Subir() = %+v, want vazio", got)
			}
			if rs := registro(t, a.obs); len(rs) != 0 {
				t.Errorf("registro = %+v, want vazio: nada registrado", rs)
			}
			if existeTabela(t, a.obs, "seguinte") {
				t.Error("a migração seguinte foi aplicada depois do erro")
			}
			if got := existeTabela(t, a.obs, "t"); got != tt.tabelaExiste {
				t.Errorf("tabela t existe = %v, want %v", got, tt.tabelaExiste)
			}
			ocioso(t, a.conn)
			a.esperarLockLivre(t)
		})
	}
}

// ---- registro versus FS ----

func TestSubirChecksumDivergente(t *testing.T) {
	const marca = "MARCA-DO-CONTEUDO-NOVO"
	tests := []struct {
		name    string
		altera  func(ms []mig)
		arquivo string
	}{
		{name: "primeira alterada", altera: func(ms []mig) { ms[0].Up += "-- " + marca + "\n" }, arquivo: "0001_a.up.sql"},
		{name: "do meio alterada", altera: func(ms []mig) { ms[1].Up = "CREATE TABLE b (x integer, y integer); -- " + marca + "\n" }, arquivo: "0002_b.up.sql"},
		{name: "so espaco em branco no fim", altera: func(ms []mig) { ms[0].Up += "\n" }, arquivo: "0001_a.up.sql"},
		{name: "so CRLF", altera: func(ms []mig) { ms[0].Up = strings.ReplaceAll(ms[0].Up, "\n", "\r\n") }, arquivo: "0001_a.up.sql"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := novoAmbiente(t)
			ms := tresMigracoes()
			// Aplica 1 e 2; a 3 fica pendente.
			if _, err := a.exec(novoFS(ms[:2]...)).Subir(t.Context()); err != nil {
				t.Fatalf("Subir() inicial: %v", err)
			}
			antes := registro(t, a.obs)

			alt := tresMigracoes()
			tt.altera(alt)
			got, err := a.exec(novoFS(alt...)).Subir(t.Context())
			if !errors.Is(err, migrate.ErroChecksumDivergente) {
				t.Fatalf("Subir() = %+v, %v, want ErroChecksumDivergente", got, err)
			}
			semDetalhe(t, err)
			if me := erroDe(t, err); me.Arquivo != tt.arquivo {
				t.Errorf("Arquivo = %q, want %q", me.Arquivo, tt.arquivo)
			}
			if !strings.Contains(err.Error(), tt.arquivo) {
				t.Errorf("erro %q, want conter o arquivo", err.Error())
			}
			if strings.Contains(err.Error(), marca) || strings.Contains(err.Error(), "CREATE TABLE") {
				t.Errorf("erro %q expõe conteúdo do SQL", err.Error())
			}
			if len(got) != 0 {
				t.Errorf("Subir() = %+v, want vazio: nada reaplicado nem aplicado", got)
			}
			depois := registro(t, a.obs)
			if len(depois) != len(antes) {
				t.Fatalf("registro mudou de %d para %d linhas: a 3 foi aplicada apesar da divergência", len(antes), len(depois))
			}
			for i := range antes {
				if antes[i] != depois[i] {
					t.Errorf("registro[%d] mudou: %+v -> %+v (checksum não pode ser atualizado)", i, antes[i], depois[i])
				}
			}
			if existeTabela(t, a.obs, "c") {
				t.Error("tabela c existe: pendente aplicado com checksum divergente")
			}
			ocioso(t, a.conn)
		})
	}
}

// Só o .up.sql entra no checksum: mexer no .down.sql não diverge.
func TestSubirMudarODownNaoDiverge(t *testing.T) {
	a := novoAmbiente(t)
	ms := tresMigracoes()
	if _, err := a.exec(novoFS(ms...)).Subir(t.Context()); err != nil {
		t.Fatalf("Subir() inicial: %v", err)
	}
	ms[1].Down = "DROP TABLE b; -- reescrito\n"
	got, err := a.exec(novoFS(ms...)).Subir(t.Context())
	if err != nil || len(got) != 0 {
		t.Errorf("Subir() = %+v, %v, want no-op sem erro", got, err)
	}
}

// insereRegistro grava uma linha direto em migracao_aplicada.
func insereRegistro(t *testing.T, c *pgx.Conn, versao int, nome, checksum string) {
	t.Helper()
	if _, err := c.Exec(t.Context(), "INSERT INTO migracao_aplicada (versao, nome, checksum) VALUES ($1, $2, $3)", versao, nome, checksum); err != nil {
		t.Fatalf("inserir registro %d: %v", versao, err)
	}
}

func TestSubirVersaoRegistradaForaDoFS(t *testing.T) {
	tests := []struct {
		name   string
		noFS   int // quantas das três migrações ficam no FS
		extras []int
	}{
		{name: "versao 2 registrada, FS so com a 1", noFS: 1, extras: []int{2}},
		{name: "versao 4 registrada, FS com 1 a 3", noFS: 3, extras: []int{4}},
		{name: "duas versoes a mais", noFS: 1, extras: []int{2, 3}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := novoAmbiente(t)
			ms := tresMigracoes()
			if _, err := a.exec(novoFS(ms[:tt.noFS]...)).Subir(t.Context()); err != nil {
				t.Fatalf("Subir() inicial: %v", err)
			}
			for _, v := range tt.extras {
				insereRegistro(t, a.obs, v, "fantasma", strings.Repeat("a", 64))
			}
			antes := registro(t, a.obs)

			got, err := a.exec(novoFS(ms[:tt.noFS]...)).Subir(t.Context())
			if !errors.Is(err, migrate.ErroVersaoDesconhecida) {
				t.Fatalf("Subir() = %+v, %v, want ErroVersaoDesconhecida", got, err)
			}
			semDetalhe(t, err)
			if len(got) != 0 {
				t.Errorf("Subir() = %+v, want vazio", got)
			}
			depois := registro(t, a.obs)
			if len(depois) != len(antes) {
				t.Errorf("registro mudou: %+v -> %+v", antes, depois)
			}
			ocioso(t, a.conn)
			a.esperarLockLivre(t)
		})
	}
}

func TestSubirNaoAplicaPendentesComRegistroInconsistente(t *testing.T) {
	a := novoAmbiente(t)
	ms := tresMigracoes()
	if _, err := a.exec(novoFS(ms[:1]...)).Subir(t.Context()); err != nil {
		t.Fatalf("Subir() inicial: %v", err)
	}
	insereRegistro(t, a.obs, 2, "outra_coisa", strings.Repeat("b", 64))
	insereRegistro(t, a.obs, 3, "mais_outra", strings.Repeat("c", 64))
	// A versão 2 do banco tem outro checksum que a do FS: é divergente (mesma
	// versão, conteúdo diferente) e nada mais é aplicado, nem a pendente 4.
	got, err := a.exec(novoFS(append(ms, mig{V: 4, Nome: "d", Up: "CREATE TABLE d (x integer);\n"})...)).Subir(t.Context())
	if err == nil {
		t.Fatalf("Subir() = %+v, nil, want erro", got)
	}
	if !errors.Is(err, migrate.ErroChecksumDivergente) {
		t.Errorf("erro = %v, want ErroChecksumDivergente", err)
	}
	if existeTabela(t, a.obs, "d") {
		t.Error("pendente 4 foi aplicada com o registro inconsistente")
	}
}

func TestSubirLacunaNoRegistro(t *testing.T) {
	tests := []struct {
		name      string
		remover   []int
		aplicadas int
	}{
		{name: "falta a 2 entre 1 e 3", remover: []int{2}, aplicadas: 3},
		{name: "falta a 1 (registro comeca em 2)", remover: []int{1}, aplicadas: 2},
		{name: "faltam 1 e 2 (so a 3)", remover: []int{1, 2}, aplicadas: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := novoAmbiente(t)
			ms := tresMigracoes()
			if _, err := a.exec(novoFS(ms[:tt.aplicadas]...)).Subir(t.Context()); err != nil {
				t.Fatalf("Subir() inicial: %v", err)
			}
			for _, v := range tt.remover {
				if _, err := a.obs.Exec(t.Context(), "DELETE FROM migracao_aplicada WHERE versao = $1", v); err != nil {
					t.Fatalf("remover registro %d: %v", v, err)
				}
			}
			antes := registro(t, a.obs)
			// FS com uma quarta migração pendente: não pode ser aplicada.
			fsys := novoFS(append(tresMigracoes(), mig{V: 4, Nome: "d", Up: "CREATE TABLE d (x integer);\n"})...)
			got, err := a.exec(fsys).Subir(t.Context())
			if !errors.Is(err, migrate.ErroRegistroComLacuna) {
				t.Fatalf("Subir() = %+v, %v, want ErroRegistroComLacuna", got, err)
			}
			semDetalhe(t, err)
			if len(got) != 0 {
				t.Errorf("Subir() = %+v, want vazio", got)
			}
			if depois := registro(t, a.obs); len(depois) != len(antes) {
				t.Errorf("registro mudou: %+v -> %+v (a migração perdida não pode ser reaplicada)", antes, depois)
			}
			if existeTabela(t, a.obs, "d") {
				t.Error("pendente 4 aplicada sobre registro com lacuna")
			}
			ocioso(t, a.conn)
			a.esperarLockLivre(t)
		})
	}
}

// ---- baseline: falhar fechado (G6) ----

func TestSubirBancoComEsquemaSemRegistro(t *testing.T) {
	up0001, err := fs.ReadFile(migrations.FS, "0001_esquema_emprestimo.up.sql")
	if err != nil {
		t.Fatalf("ler 0001: %v", err)
	}
	tests := []struct {
		name  string
		setup string
	}{
		{name: "0001 aplicada a mao", setup: string(up0001)},
		{name: "tabela solta", setup: "CREATE TABLE solta (x integer);"},
		{name: "sequencia solta", setup: "CREATE SEQUENCE solta_seq;"},
		{name: "view solta", setup: "CREATE VIEW solta_v AS SELECT 1 AS x;"},
		{name: "view materializada solta", setup: "CREATE MATERIALIZED VIEW solta_mv AS SELECT 1 AS x;"},
		{name: "tabela particionada solta", setup: "CREATE TABLE solta_p (x integer) PARTITION BY RANGE (x);"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := novoAmbiente(t)
			if _, err := a.conn.Exec(t.Context(), tt.setup); err != nil {
				t.Fatalf("preparar o banco: %v", err)
			}
			antes := relacoes(t, a.obs, false)

			got, err := a.exec(realFS()).Subir(t.Context())
			if !errors.Is(err, migrate.ErroBancoSemRegistro) {
				t.Fatalf("Subir() = %+v, %v, want ErroBancoSemRegistro", got, err)
			}
			semDetalhe(t, err)
			if len(got) != 0 {
				t.Errorf("Subir() = %+v, want vazio", got)
			}
			if depois := relacoes(t, a.obs, false); !igual(antes, depois) {
				t.Errorf("relações mudaram: %v -> %v (nada pode ser criado nem aplicado)", antes, depois)
			}
			if existeTabela(t, a.obs, "migracao_aplicada") {
				t.Error("migracao_aplicada foi criada: o baseline tem que falhar fechado antes de qualquer efeito")
			}
			ocioso(t, a.conn)
			a.esperarLockLivre(t)

			// Status trata a mesma condição como erro, sem escrever.
			if _, err := a.exec(realFS()).Status(t.Context()); !errors.Is(err, migrate.ErroBancoSemRegistro) {
				t.Errorf("Status() erro = %v, want ErroBancoSemRegistro", err)
			}
			if existeTabela(t, a.obs, "migracao_aplicada") {
				t.Error("Status criou migracao_aplicada")
			}
		})
	}
}

// Registro vazio (controle criado, nenhuma migração registrada) e uma relação
// qualquer no schema também é "esquema sem registro".
func TestSubirRegistroVazioComRelacaoSolta(t *testing.T) {
	a := novoAmbiente(t)
	ms := []mig{{V: 1, Nome: "a", Up: "SELECT 1/0;"}}
	if _, err := a.exec(novoFS(ms...)).Subir(t.Context()); !errors.Is(err, migrate.ErroSQL) {
		t.Fatalf("Subir() que falha na 0001: %v", err)
	}
	if !existeTabela(t, a.obs, "migracao_aplicada") {
		t.Fatal("pré-condição: o controle deveria ter sido criado e commitado")
	}
	if _, err := a.obs.Exec(t.Context(), "CREATE TABLE solta (x integer)"); err != nil {
		t.Fatalf("CREATE TABLE solta: %v", err)
	}
	ms[0].Up = "CREATE TABLE a (x integer);\n"
	got, err := a.exec(novoFS(ms...)).Subir(t.Context())
	if !errors.Is(err, migrate.ErroBancoSemRegistro) {
		t.Fatalf("Subir() = %+v, %v, want ErroBancoSemRegistro", got, err)
	}
	if existeTabela(t, a.obs, "a") {
		t.Error("tabela a criada apesar do baseline")
	}
}

// O controle criado pela própria execução que depois falhou na 0001 não é um
// "banco com esquema": a próxima execução continua do zero.
func TestSubirControleVazioDepoisDeFalhaNaPrimeiraNaoEBaseline(t *testing.T) {
	a := novoAmbiente(t)
	ms := []mig{{V: 1, Nome: "a", Up: "CREATE TABLE a (x integer);\nSELECT 1/0;\n"}}
	if _, err := a.exec(novoFS(ms...)).Subir(t.Context()); !errors.Is(err, migrate.ErroSQL) {
		t.Fatalf("Subir() que falha: %v", err)
	}
	if !existeTabela(t, a.obs, "migracao_aplicada") {
		t.Fatal("pré-condição: o controle deveria existir (o up do controle commitou)")
	}
	if rs := registro(t, a.obs); len(rs) != 0 {
		t.Fatalf("pré-condição: registro = %+v, want vazio", rs)
	}

	ms[0].Up = "CREATE TABLE a (x integer);\n"
	got, err := a.exec(novoFS(ms...)).Subir(t.Context())
	if err != nil {
		t.Fatalf("Subir() depois do conserto erro = %v (controle vazio não é baseline)", err)
	}
	if len(got) != 1 || got[0].Versao != 1 {
		t.Errorf("Subir() = %+v, want a versão 1", got)
	}
}

// Um registro não vazio com outras relações é o caso normal.
func TestSubirRegistroNaoVazioComRelacoesEOk(t *testing.T) {
	a := novoAmbiente(t)
	ms := tresMigracoes()
	if _, err := a.exec(novoFS(ms[:1]...)).Subir(t.Context()); err != nil {
		t.Fatalf("Subir() inicial: %v", err)
	}
	if _, err := a.exec(novoFS(ms...)).Subir(t.Context()); err != nil {
		t.Fatalf("Subir() seguinte: %v", err)
	}
}

func TestSubirSchemaCorrenteIndefinido(t *testing.T) {
	tests := []struct {
		name       string
		searchPath string
	}{
		{name: "search_path vazio", searchPath: ""},
		{name: "schema que nao existe", searchPath: "schema_que_nao_existe_xyz"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := novoAmbiente(t)
			c := a.b.conectar(t, "search_path", tt.searchPath)
			var nulo bool
			if err := c.QueryRow(t.Context(), "SELECT current_schema() IS NULL").Scan(&nulo); err != nil || !nulo {
				t.Fatalf("pré-condição: current_schema() IS NULL = %v (%v)", nulo, err)
			}
			got, err := a.execEm(c, realFS()).Subir(t.Context())
			if !errors.Is(err, migrate.ErroSchemaIndefinido) {
				t.Fatalf("Subir() = %+v, %v, want ErroSchemaIndefinido", got, err)
			}
			semDetalhe(t, err)
			if _, err := a.execEm(c, realFS()).Status(t.Context()); !errors.Is(err, migrate.ErroSchemaIndefinido) {
				t.Errorf("Status() erro = %v, want ErroSchemaIndefinido", err)
			}
			a.esperarLockLivre(t)
		})
	}
}

// ---- controle estranho ----

const (
	colunasControle = "versao integer NOT NULL, nome text NOT NULL, checksum text NOT NULL, aplicada_em timestamptz NOT NULL DEFAULT now()"
	pkControle      = ", CONSTRAINT migracao_aplicada_pkey PRIMARY KEY (versao)"
)

var controlesEstranhos = []struct {
	name  string
	setup string
}{
	{name: "tabela com outras colunas", setup: "CREATE TABLE migracao_aplicada (id integer)"},
	{name: "view homonima", setup: "CREATE VIEW migracao_aplicada AS SELECT 1 AS versao, 'a'::text AS nome, repeat('a', 64) AS checksum, now() AS aplicada_em"},
	{name: "view materializada homonima", setup: "CREATE MATERIALIZED VIEW migracao_aplicada AS SELECT 1 AS versao, 'a'::text AS nome, repeat('a', 64) AS checksum, now() AS aplicada_em"},
	{name: "sequencia homonima", setup: "CREATE SEQUENCE migracao_aplicada"},
	{name: "versao bigint", setup: "CREATE TABLE migracao_aplicada (versao bigint NOT NULL, nome text NOT NULL, checksum text NOT NULL, aplicada_em timestamptz NOT NULL DEFAULT now()" + pkControle + ")"},
	{name: "versao text", setup: "CREATE TABLE migracao_aplicada (versao text NOT NULL, nome text NOT NULL, checksum text NOT NULL, aplicada_em timestamptz NOT NULL DEFAULT now()" + pkControle + ")"},
	{name: "nome nulavel", setup: "CREATE TABLE migracao_aplicada (versao integer NOT NULL, nome text, checksum text NOT NULL, aplicada_em timestamptz NOT NULL DEFAULT now()" + pkControle + ")"},
	{name: "checksum varchar", setup: "CREATE TABLE migracao_aplicada (versao integer NOT NULL, nome text NOT NULL, checksum varchar(64) NOT NULL, aplicada_em timestamptz NOT NULL DEFAULT now()" + pkControle + ")"},
	{name: "aplicada_em sem fuso", setup: "CREATE TABLE migracao_aplicada (versao integer NOT NULL, nome text NOT NULL, checksum text NOT NULL, aplicada_em timestamp NOT NULL DEFAULT now()" + pkControle + ")"},
	{name: "sem a coluna aplicada_em", setup: "CREATE TABLE migracao_aplicada (versao integer NOT NULL, nome text NOT NULL, checksum text NOT NULL" + pkControle + ")"},
	{name: "coluna extra", setup: "CREATE TABLE migracao_aplicada (" + colunasControle + ", extra text" + pkControle + ")"},
	{name: "sem chave primaria", setup: "CREATE TABLE migracao_aplicada (" + colunasControle + ")"},
	{name: "chave primaria com outro nome", setup: "CREATE TABLE migracao_aplicada (" + colunasControle + ", CONSTRAINT outro_pk PRIMARY KEY (versao))"},
	{name: "chave primaria em outra coluna", setup: "CREATE TABLE migracao_aplicada (" + colunasControle + ", CONSTRAINT migracao_aplicada_pkey PRIMARY KEY (nome))"},
}

func TestSubirControleEstranhoFalhaFechado(t *testing.T) {
	for _, tt := range controlesEstranhos {
		t.Run(tt.name, func(t *testing.T) {
			a := novoAmbiente(t)
			if _, err := a.conn.Exec(t.Context(), tt.setup); err != nil {
				t.Fatalf("preparar o banco: %v", err)
			}
			antes := relacoes(t, a.obs, false)

			got, err := a.exec(novoFS(tresMigracoes()...)).Subir(t.Context())
			if !errors.Is(err, migrate.ErroControleInvalido) {
				t.Fatalf("Subir() = %+v, %v, want ErroControleInvalido", got, err)
			}
			semDetalhe(t, err)
			if strings.Contains(err.Error(), "migracao_aplicada") && strings.Contains(err.Error(), "bigint") {
				t.Errorf("erro %q descreve a estrutura encontrada", err.Error())
			}
			if len(got) != 0 {
				t.Errorf("Subir() = %+v, want vazio", got)
			}
			if depois := relacoes(t, a.obs, false); !igual(antes, depois) {
				t.Errorf("relações mudaram: %v -> %v", antes, depois)
			}
			for _, tab := range []string{"a", "b", "c"} {
				if existeTabela(t, a.obs, tab) {
					t.Errorf("tabela %s criada sobre controle inválido", tab)
				}
			}
			ocioso(t, a.conn)
			a.esperarLockLivre(t)

			if _, err := a.exec(novoFS(tresMigracoes()...)).Status(t.Context()); !errors.Is(err, migrate.ErroControleInvalido) {
				t.Errorf("Status() erro = %v, want ErroControleInvalido", err)
			}
		})
	}
}

// Um controle correto, criado a mão com o próprio SQL embutido, é adotado.
func TestSubirAdotaControlePreexistenteCorreto(t *testing.T) {
	a := novoAmbiente(t)
	up, err := fs.ReadFile(migrations.Controle, "controle/0000_migracao_aplicada.up.sql")
	if err != nil {
		t.Fatalf("ler up do controle: %v", err)
	}
	if _, err := a.conn.Exec(t.Context(), string(up)); err != nil {
		t.Fatalf("aplicar o controle a mão: %v", err)
	}
	got, err := a.exec(novoFS(tresMigracoes()...)).Subir(t.Context())
	if err != nil || len(got) != 3 {
		t.Fatalf("Subir() = %+v, %v, want 3 migrações", got, err)
	}
}

// A checagem do controle é qualificada por current_schema(): uma
// migracao_aplicada de um schema posterior do search_path não é usada.
func TestSubirIgnoraControleDeSchemaPosteriorDoSearchPath(t *testing.T) {
	a := novoAmbiente(t)
	outro := novoBanco(t) // schema "posterior", com um controle falso e pronto
	adm := outro.conectar(t)
	up, err := fs.ReadFile(migrations.Controle, "controle/0000_migracao_aplicada.up.sql")
	if err != nil {
		t.Fatalf("ler up do controle: %v", err)
	}
	if _, err := adm.Exec(t.Context(), string(up)); err != nil {
		t.Fatalf("criar controle no outro schema: %v", err)
	}
	if _, err := adm.Exec(t.Context(), "INSERT INTO migracao_aplicada (versao, nome, checksum) VALUES (1, 'falso', $1)", strings.Repeat("f", 64)); err != nil {
		t.Fatalf("semear o outro schema: %v", err)
	}

	c := a.b.conectar(t, "search_path", a.b.schema+","+outro.schema)
	ms := tresMigracoes()
	got, err := a.execEm(c, novoFS(ms...)).Subir(t.Context())
	if err != nil {
		t.Fatalf("Subir() erro = %v (usou o controle do schema posterior?)", err)
	}
	if len(got) != 3 {
		t.Errorf("Subir() = %+v, want 3 migrações", got)
	}
	if v := versoes(registro(t, a.obs)); len(v) != 3 {
		t.Errorf("registro do schema corrente = %v, want [1 2 3]", v)
	}
	var n int
	var nome string
	if err := adm.QueryRow(t.Context(), "SELECT count(*), min(nome) FROM migracao_aplicada").Scan(&n, &nome); err != nil {
		t.Fatalf("ler o outro schema: %v", err)
	}
	if n != 1 || nome != "falso" {
		t.Errorf("controle do outro schema = %d linhas (%s), want intocado (1 linha 'falso')", n, nome)
	}

	// Status também lê o schema corrente: tudo pendente num schema novo.
	a2 := novoAmbiente(t)
	c2 := a2.b.conectar(t, "search_path", a2.b.schema+","+outro.schema)
	sit, err := a2.execEm(c2, novoFS(ms...)).Status(t.Context())
	if err != nil {
		t.Fatalf("Status() erro = %v", err)
	}
	for _, s := range sit {
		if s.Estado != migrate.EstadoPendente {
			t.Errorf("Status() %+v, want tudo pendente (o controle do schema posterior não conta)", s)
		}
	}
}

// ---- Status ----

func TestStatusBancoVazioTudoPendenteSemEscrever(t *testing.T) {
	a, tr := novoAmbienteTracado(t)
	ms := tresMigracoes()
	tr.limpar()
	got, err := a.exec(novoFS(ms...)).Status(t.Context())
	if err != nil {
		t.Fatalf("Status() erro = %v", err)
	}
	semDetalhe(t, err)
	if len(got) != 3 {
		t.Fatalf("Status() = %+v, want 3 situações", got)
	}
	for i, s := range got {
		if s.Versao != i+1 || s.Nome != ms[i].Nome || s.Estado != migrate.EstadoPendente || !s.AplicadaEm.IsZero() {
			t.Errorf("Status()[%d] = %+v, want pendente da versão %d (%s) sem aplicada_em", i, s, i+1, ms[i].Nome)
		}
	}
	if r := relacoes(t, a.obs, false); len(r) != 0 {
		t.Errorf("Status criou relações: %v", r)
	}
	sqls, _ := tr.copia()
	if w := escritas(sqls); len(w) != 0 {
		t.Errorf("Status enviou SQL de escrita: %q", w)
	}
	ocioso(t, a.conn)
}

func TestStatusEhSomenteLeitura(t *testing.T) {
	a, tr := novoAmbienteTracado(t)
	ms := tresMigracoes()
	if _, err := a.exec(novoFS(ms[:2]...)).Subir(t.Context()); err != nil {
		t.Fatalf("Subir() inicial: %v", err)
	}
	antes := registro(t, a.obs)

	tr.limpar()
	if _, err := a.exec(novoFS(ms...)).Status(t.Context()); err != nil {
		t.Fatalf("Status() erro = %v", err)
	}
	sqls, _ := tr.copia()
	leitura := false
	for _, s := range sqls {
		l := strings.ToLower(strings.TrimSpace(s))
		if strings.HasPrefix(l, "begin") && strings.Contains(l, "read only") {
			leitura = true
		}
		if strings.Contains(l, "advisory") {
			t.Errorf("Status mexeu em advisory lock: %q", s)
		}
		ok := false
		for _, p := range []string{"begin", "commit", "rollback", "select", "show"} {
			if strings.HasPrefix(l, p) {
				ok = true
			}
		}
		if !ok {
			t.Errorf("Status enviou SQL que não é leitura: %q", s)
		}
	}
	if !leitura {
		t.Errorf("Status não abriu transação READ ONLY; SQL enviado: %q", sqls)
	}
	if w := escritas(sqls); len(w) != 0 {
		t.Errorf("Status enviou SQL de escrita: %q", w)
	}
	depois := registro(t, a.obs)
	if len(depois) != len(antes) {
		t.Fatalf("registro mudou: %+v -> %+v", antes, depois)
	}
	for i := range antes {
		if antes[i] != depois[i] {
			t.Errorf("registro[%d] mudou: %+v -> %+v", i, antes[i], depois[i])
		}
	}
	ocioso(t, a.conn)
}

func TestStatusEstados(t *testing.T) {
	a := novoAmbiente(t)
	ms := tresMigracoes()
	if _, err := a.exec(novoFS(ms[:2]...)).Subir(t.Context()); err != nil {
		t.Fatalf("Subir() inicial: %v", err)
	}
	// Sessão com fuso diferente: aplicada_em tem que vir em UTC mesmo assim.
	c := a.b.conectar(t, "timezone", "America/Sao_Paulo")

	t.Run("aplicadas e pendente", func(t *testing.T) {
		got, err := a.execEm(c, novoFS(ms...)).Status(t.Context())
		if err != nil {
			t.Fatalf("Status() erro = %v", err)
		}
		if len(got) != 3 {
			t.Fatalf("Status() = %+v, want 3", got)
		}
		reg := registro(t, a.obs)
		for i := 0; i < 2; i++ {
			s := got[i]
			if s.Versao != i+1 || s.Nome != ms[i].Nome || s.Estado != migrate.EstadoAplicada {
				t.Errorf("Status()[%d] = %+v, want aplicada %d %s", i, s, i+1, ms[i].Nome)
			}
			if !s.AplicadaEm.Equal(reg[i].AplicadaEm) || s.AplicadaEm.IsZero() {
				t.Errorf("Status()[%d].AplicadaEm = %v, want %v", i, s.AplicadaEm, reg[i].AplicadaEm)
			}
			if s.AplicadaEm.Location() != time.UTC {
				t.Errorf("Status()[%d].AplicadaEm em %v, want UTC", i, s.AplicadaEm.Location())
			}
		}
		if s := got[2]; s.Versao != 3 || s.Nome != "c" || s.Estado != migrate.EstadoPendente || !s.AplicadaEm.IsZero() {
			t.Errorf("Status()[2] = %+v, want pendente 3 c sem aplicada_em", s)
		}
	})

	t.Run("divergente", func(t *testing.T) {
		alt := tresMigracoes()
		alt[1].Up += "-- mudou\n"
		got, err := a.exec(novoFS(alt...)).Status(t.Context())
		if err != nil {
			t.Fatalf("Status() erro = %v (divergência é um estado, o CLI decide a saída)", err)
		}
		want := []migrate.Estado{migrate.EstadoAplicada, migrate.EstadoDivergente, migrate.EstadoPendente}
		if len(got) != 3 {
			t.Fatalf("Status() = %+v", got)
		}
		for i, w := range want {
			if got[i].Estado != w {
				t.Errorf("Status()[%d].Estado = %q, want %q", i, got[i].Estado, w)
			}
		}
	})

	t.Run("desconhecida", func(t *testing.T) {
		insereRegistro(t, a.obs, 3, "so_no_banco", strings.Repeat("d", 64))
		defer func() {
			if _, err := a.obs.Exec(context.Background(), "DELETE FROM migracao_aplicada WHERE versao = 3"); err != nil {
				t.Errorf("limpar: %v", err)
			}
		}()
		got, err := a.exec(novoFS(ms[:2]...)).Status(t.Context())
		if err != nil {
			t.Fatalf("Status() erro = %v (versão desconhecida é um estado)", err)
		}
		if len(got) != 3 {
			t.Fatalf("Status() = %+v, want 3 (2 do FS e 1 desconhecida)", got)
		}
		if s := got[2]; s.Versao != 3 || s.Nome != "so_no_banco" || s.Estado != migrate.EstadoDesconhecida {
			t.Errorf("Status()[2] = %+v, want desconhecida 3 so_no_banco", s)
		}
	})

	t.Run("lacuna no registro e erro", func(t *testing.T) {
		if _, err := a.obs.Exec(t.Context(), "DELETE FROM migracao_aplicada WHERE versao = 1"); err != nil {
			t.Fatalf("remover registro: %v", err)
		}
		_, err := a.exec(novoFS(ms...)).Status(t.Context())
		if !errors.Is(err, migrate.ErroRegistroComLacuna) {
			t.Errorf("Status() erro = %v, want ErroRegistroComLacuna", err)
		}
		semDetalhe(t, err)
	})
}

func TestStatusNaoPrecisaDoLockENaoOTocaEnquantoOutroExecutorOSegura(t *testing.T) {
	a := novoAmbiente(t)
	ms := tresMigracoes()
	if _, err := a.exec(novoFS(ms[:1]...)).Subir(t.Context()); err != nil {
		t.Fatalf("Subir() inicial: %v", err)
	}
	liberar := a.segurarLock(t)
	defer liberar()

	inicio := time.Now()
	got, err := a.exec(novoFS(ms...), func(o *migrate.Opcoes) { o.LockTimeout = 20 * time.Second }).Status(t.Context())
	if err != nil {
		t.Fatalf("Status() com o lock ocupado erro = %v", err)
	}
	if d := time.Since(inicio); d > 5*time.Second {
		t.Errorf("Status levou %v: esperou pelo lock", d)
	}
	if len(got) != 3 {
		t.Errorf("Status() = %+v, want 3", got)
	}
	// O lock continua sendo da outra sessão.
	if a.lockLivreAgora(t) {
		t.Error("lock livre: o Status o tomou e soltou, ou a sessão segurante o perdeu")
	}
}

func TestStatusFSInvalido(t *testing.T) {
	a := novoAmbiente(t)
	_, err := a.exec(novoFS(mig{V: 2, Nome: "b", Up: "SELECT 1;"})).Status(t.Context())
	if !errors.Is(err, migrate.ErroFSInvalido) {
		t.Errorf("Status() erro = %v, want ErroFSInvalido", err)
	}
	if r := relacoes(t, a.obs, false); len(r) != 0 {
		t.Errorf("relações = %v, want nenhuma", r)
	}
}

// ---- vazamento ----

func TestSemVazamentoDeValoresSQLENomes(t *testing.T) {
	const (
		segredo = "SEGREDO-XYZ"
		trecho  = "INSERT INTO seg"
	)
	a := novoAmbiente(t)
	up := "CREATE TABLE seg (v text CONSTRAINT seg_v_uq UNIQUE);\n" +
		trecho + " VALUES ('" + segredo + "');\n" +
		trecho + " VALUES ('" + segredo + "');\n"
	fsys := novoFS(mig{V: 1, Nome: "seg", Up: up})

	got, err := a.exec(fsys).Subir(t.Context())
	if err == nil {
		t.Fatalf("Subir() = %+v, nil, want erro de unicidade", got)
	}
	semDetalhe(t, err)
	if !errors.Is(err, migrate.ErroSQL) {
		t.Fatalf("erro = %v, want ErroSQL", err)
	}
	me := erroDe(t, err)
	if me.Arquivo != "0001_seg.up.sql" || me.Code != "23505" || me.Constraint != "seg_v_uq" {
		t.Errorf("erro = {Arquivo:%q Code:%q Constraint:%q}, want {0001_seg.up.sql 23505 seg_v_uq}", me.Arquivo, me.Code, me.Constraint)
	}

	proibidos := []string{segredo, trecho, "duplicate key", "already exists", "violates", "Key (v)"}
	saidas := map[string]string{
		"Error()": err.Error(),
		"%v":      fmt.Sprintf("%v", err),
		"%+v":     fmt.Sprintf("%+v", err),
		"%#v":     fmt.Sprintf("%#v", err),
		"%s":      fmt.Sprintf("%s", err),
		"log":     a.log.String(),
	}
	for nome, s := range saidas {
		for _, p := range proibidos {
			if strings.Contains(s, p) {
				t.Errorf("%s contém %q: %s", nome, p, s)
			}
		}
	}
	for _, ev := range eventos(t, a.log.String()) {
		if ev["level"] == "ERROR" {
			t.Errorf("executor logou o erro que devolve: %v", ev)
		}
	}
	if existeTabela(t, a.obs, "seg") {
		t.Error("tabela seg existe: rollback não aconteceu")
	}
}

func TestErrosDoExecutorSemDetalheDoPgx(t *testing.T) {
	// Cada cenário produz um erro; todos têm que cumprir o contrato de erro.
	cenarios := map[string]func(t *testing.T) error{
		"lock ocupado": func(t *testing.T) error {
			a := novoAmbiente(t)
			defer a.segurarLock(t)()
			_, err := a.exec(novoFS(tresMigracoes()...), func(o *migrate.Opcoes) { o.LockTimeout = 200 * time.Millisecond }).Subir(t.Context())
			return err
		},
		"checksum": func(t *testing.T) error {
			a := novoAmbiente(t)
			ms := tresMigracoes()
			_, _ = a.exec(novoFS(ms...)).Subir(t.Context())
			ms[0].Up = "SELECT 'SEGREDO-XYZ';"
			_, err := a.exec(novoFS(ms...)).Subir(t.Context())
			return err
		},
		"baseline": func(t *testing.T) error {
			a := novoAmbiente(t)
			_, _ = a.conn.Exec(t.Context(), "CREATE TABLE solta (x integer)")
			_, err := a.exec(realFS()).Subir(t.Context())
			return err
		},
		"controle estranho": func(t *testing.T) error {
			a := novoAmbiente(t)
			_, _ = a.conn.Exec(t.Context(), "CREATE TABLE migracao_aplicada (id integer)")
			_, err := a.exec(realFS()).Subir(t.Context())
			return err
		},
		"sql": func(t *testing.T) error {
			a := novoAmbiente(t)
			_, err := a.exec(novoFS(mig{V: 1, Nome: "a", Up: "SELECT 'SEGREDO-XYZ'::integer;"})).Subir(t.Context())
			return err
		},
	}
	for nome, f := range cenarios {
		t.Run(nome, func(t *testing.T) {
			err := f(t)
			if err == nil {
				t.Fatal("sem erro")
			}
			semDetalhe(t, err)
			if strings.Contains(err.Error(), "SEGREDO-XYZ") {
				t.Errorf("erro %q ecoa valor", err.Error())
			}
		})
	}
}
