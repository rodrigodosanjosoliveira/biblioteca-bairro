package migrate_test

import (
	"context"
	"testing"
	"time"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/migrate"
)

// Testes do ciclo de correção pós-revisão da DEL-008 (sugestão 3).
//
// Opcoes com prazo positivo menor que 1 ms passava pelo "<= 0" e virava "0ms"
// em set_config, que no PostgreSQL desliga o timeout. Zero, negativo e
// sub-milissegundo têm de cair no padrão (lock 5 s, statement 60 s) dentro de
// migrate.Novo.

// prazosEfetivos roda uma migração que grava os prazos vistos dentro da
// transação dela (em ms, via pg_settings) e os devolve.
func prazosEfetivos(t *testing.T, lock, stmt time.Duration) (lockMs, stmtMs int) {
	t.Helper()
	a := novoAmbiente(t)
	ms := []mig{{V: 1, Nome: "cfg", Up: `CREATE TABLE cfg AS SELECT
		(SELECT setting::integer FROM pg_settings WHERE name = 'lock_timeout') AS lt,
		(SELECT setting::integer FROM pg_settings WHERE name = 'statement_timeout') AS st;
`}}
	_, err := a.exec(novoFS(ms...), func(o *migrate.Opcoes) { o.LockTimeout, o.StatementTimeout = lock, stmt }).Subir(t.Context())
	if err != nil {
		t.Fatalf("Subir() com lock=%v statement=%v: %v", lock, stmt, err)
	}
	if err := a.obs.QueryRow(t.Context(), "SELECT lt, st FROM cfg").Scan(&lockMs, &stmtMs); err != nil {
		t.Fatalf("ler cfg: %v", err)
	}
	return lockMs, stmtMs
}

func TestNovoPrazoNaoPositivoOuSubMilissegundoCaiNoPadrao(t *testing.T) {
	valores := []struct {
		nome string
		d    time.Duration
	}{
		{"zero", 0},
		{"-1ns", -time.Nanosecond},
		{"-1s", -time.Second},
		{"1ns", time.Nanosecond},
		{"500us", 500 * time.Microsecond},
		{"999us", 999 * time.Microsecond},
	}
	for _, v := range valores {
		t.Run("lock="+v.nome, func(t *testing.T) {
			lock, stmt := prazosEfetivos(t, v.d, 0)
			if lock != 5000 {
				t.Errorf("lock_timeout dentro da migração = %dms, want 5000ms (padrão; 0 desligaria o timeout)", lock)
			}
			if stmt != 60000 {
				t.Errorf("statement_timeout dentro da migração = %dms, want 60000ms", stmt)
			}
		})
		t.Run("statement="+v.nome, func(t *testing.T) {
			lock, stmt := prazosEfetivos(t, 0, v.d)
			if lock != 5000 {
				t.Errorf("lock_timeout dentro da migração = %dms, want 5000ms", lock)
			}
			if stmt != 60000 {
				t.Errorf("statement_timeout dentro da migração = %dms, want 60000ms (padrão; 0 desligaria o timeout)", stmt)
			}
		})
	}
	t.Run("ambos sub-milissegundo", func(t *testing.T) {
		lock, stmt := prazosEfetivos(t, 500*time.Microsecond, 999*time.Microsecond)
		if lock != 5000 || stmt != 60000 {
			t.Errorf("lock=%dms statement=%dms, want 5000ms e 60000ms", lock, stmt)
		}
	})
}

// 1 ms e 1500 µs não podem desligar o timeout: ele continua valendo (pelo
// menos 1 ms). O efeito é observado pelo comportamento, sem depender do
// quanto o servidor demora para ler o catálogo: um bloqueio de lock a ser
// recusado e um pg_sleep a ser interrompido.
func TestNovoPrazoDeUmMilissegundoOuMaisNaoDesliga(t *testing.T) {
	valores := []struct {
		nome string
		d    time.Duration
	}{
		{"1ms", time.Millisecond},
		{"1500us", 1500 * time.Microsecond},
	}
	for _, v := range valores {
		t.Run("lock="+v.nome, func(t *testing.T) {
			a := novoAmbiente(t)
			ms := []mig{{V: 1, Nome: "a", Up: "CREATE TABLE alvo (x integer);\n"}}
			if _, err := a.exec(novoFS(ms...)).Subir(t.Context()); err != nil {
				t.Fatalf("Subir() da 0001: %v", err)
			}
			ms = append(ms, mig{V: 2, Nome: "b", Up: "ALTER TABLE alvo ADD COLUMN c integer;\n"})

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
			_, err = a.exec(novoFS(ms...), func(o *migrate.Opcoes) { o.LockTimeout = v.d }).Subir(t.Context())
			d := time.Since(inicio)
			semDetalhe(t, err)
			if me := erroDe(t, err); me.Code != "55P03" || me.Arquivo != "0002_b.up.sql" {
				t.Errorf("erro = {Arquivo:%q Code:%q}, want {0002_b.up.sql 55P03}: lock_timeout de %s desligado?", me.Arquivo, me.Code, v.nome)
			}
			if d > 10*time.Second {
				t.Errorf("Subir levou %v, want a recusa quase imediata", d)
			}
		})
		t.Run("statement="+v.nome, func(t *testing.T) {
			a := novoAmbiente(t)
			ms := []mig{{V: 1, Nome: "a", Up: "SELECT pg_sleep(0.5);\n"}}
			inicio := time.Now()
			_, err := a.exec(novoFS(ms...), func(o *migrate.Opcoes) { o.StatementTimeout = v.d }).Subir(t.Context())
			d := time.Since(inicio)
			semDetalhe(t, err)
			// Com 1 ms o timeout pode pegar antes, numa leitura do catálogo
			// (ErroBanco); em qualquer caso é o 57014, e o pg_sleep não termina.
			if me := erroDe(t, err); me.Code != "57014" {
				t.Errorf("erro = %+v, want SQLSTATE 57014 (statement_timeout de %s desligado?)", me, v.nome)
			}
			if d > 10*time.Second {
				t.Errorf("Subir levou %v, want a interrupção quase imediata", d)
			}
			if rs := registro(t, a.obs); len(rs) != 0 {
				t.Errorf("registro = %v, want vazio (a migração foi interrompida)", versoes(rs))
			}
		})
	}
}
