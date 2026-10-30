package main

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/config"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/migrate"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/migrations"
)

// Contrato fixado por estes testes (DEL-008, G2 e G12), no pacote main:
//
//	func despachar(ctx, args []string, getenv func(string) string, stdout, stderr io.Writer) int
//
//	type migrador interface {
//	    Subir(ctx context.Context) ([]migrate.Migracao, error)
//	    Status(ctx context.Context) ([]migrate.Situacao, error)
//	}
//
//	// Ponto de injeção: abre a conexão dedicada e o executor. O func() devolvido
//	// fecha a conexão e é chamado exatamente uma vez pelo CLI quando erroBoot é
//	// semErro; com erroBoot != semErro o CLI não o chama (pode ser nil).
//	var abrirMigrador func(ctx context.Context, cfg config.ConfigMigracao, logger *slog.Logger) (migrador, func(), erroBoot)
//
// *migrate.Executor tem que satisfazer migrador.
var (
	_ func(context.Context, []string, func(string) string, io.Writer, io.Writer) int          = despachar
	_ migrador                                                                                = (*migrate.Executor)(nil)
	_ func(context.Context, config.ConfigMigracao, *slog.Logger) (migrador, func(), erroBoot) = abrirMigrador
)

const (
	usoCLI       = "uso: biblioteca-bairro [migrate up | migrate status]"
	msgErroFinal = "biblioteca-bairro migrate encerrado com erro"
	dsnFalsa     = "postgres://usuario-secreto:SENHA-DSN@127.0.0.1:5432/biblioteca"
)

// migradorFalso responde com o que o teste programa e registra as chamadas.
type migradorFalso struct {
	migs      []migrate.Migracao
	subirErr  error
	sits      []migrate.Situacao
	statusErr error

	subirChamadas  int
	statusChamadas int
	ctxErr         error // ctx.Err() visto no Subir/Status
}

func (m *migradorFalso) Subir(ctx context.Context) ([]migrate.Migracao, error) {
	m.subirChamadas++
	m.ctxErr = ctx.Err()
	return m.migs, m.subirErr
}

func (m *migradorFalso) Status(ctx context.Context) ([]migrate.Situacao, error) {
	m.statusChamadas++
	m.ctxErr = ctx.Err()
	return m.sits, m.statusErr
}

// bancada substitui abrirMigrador por um falso que conta aberturas e fechos.
type bancada struct {
	abertos   int
	fechados  int
	cfg       config.ConfigMigracao
	erroAbrir erroBoot
	mig       *migradorFalso
	aoAbrir   func(*slog.Logger)
}

func novaBancada(t *testing.T) *bancada {
	t.Helper()
	b := &bancada{mig: &migradorFalso{}}
	orig := abrirMigrador
	abrirMigrador = func(_ context.Context, cfg config.ConfigMigracao, logger *slog.Logger) (migrador, func(), erroBoot) {
		b.abertos++
		b.cfg = cfg
		if b.erroAbrir != semErro {
			return nil, nil, b.erroAbrir
		}
		if b.aoAbrir != nil {
			b.aoAbrir(logger)
		}
		return b.mig, func() { b.fechados++ }, semErro
	}
	t.Cleanup(func() { abrirMigrador = orig })
	return b
}

// rodar executa despachar com saídas capturadas.
func rodar(t *testing.T, args []string, env map[string]string) (code int, stdout, stderr string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var out, errb bytes.Buffer
	code = despachar(ctx, args, ambiente(env), &out, &errb)
	return code, out.String(), errb.String()
}

func envMigrar(muda map[string]string) map[string]string {
	m := map[string]string{"DATABASE_URL": dsnFalsa}
	for k, v := range muda {
		if v == "" {
			delete(m, k)
			continue
		}
		m[k] = v
	}
	return m
}

// linhasJSON interpreta o stderr como linhas de JSON.
func linhasJSON(t *testing.T, s string) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, l := range strings.Split(strings.TrimSpace(s), "\n") {
		if l == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(l), &m); err != nil {
			t.Fatalf("linha de stderr não é JSON: %q", l)
		}
		out = append(out, m)
	}
	return out
}

// umaLinhaDeErro confere que o stderr tem exatamente uma linha de erro final e
// devolve o campo "erro".
func umaLinhaDeErro(t *testing.T, stderr string) string {
	t.Helper()
	if n := strings.Count(stderr, msgErroFinal); n != 1 {
		t.Fatalf("stderr com %d linhas %q, want exatamente 1:\n%s", n, msgErroFinal, stderr)
	}
	for _, l := range linhasJSON(t, stderr) {
		if l["msg"] == msgErroFinal {
			if l["level"] != "ERROR" {
				t.Errorf("level = %v, want ERROR", l["level"])
			}
			erro, _ := l["erro"].(string)
			if erro == "" {
				t.Errorf("linha de erro sem o campo erro: %v", l)
			}
			return erro
		}
	}
	t.Fatalf("linha de erro não encontrada em:\n%s", stderr)
	return ""
}

func semSegredos(t *testing.T, saida string, segredos ...string) {
	t.Helper()
	for _, s := range append(segredos, "SENHA-DSN", "usuario-secreto", "SEGREDO-XYZ") {
		if strings.Contains(saida, s) {
			t.Errorf("saída ecoa %q:\n%s", s, saida)
		}
	}
}

// ---- argumentos ----

func TestDespacharUso(t *testing.T) {
	tests := []struct {
		name string
		args []string
	}{
		{name: "migrate sem modo", args: []string{"migrate"}},
		{name: "migrate down", args: []string{"migrate", "down"}},
		{name: "migrate up com argumento extra", args: []string{"migrate", "up", "extra"}},
		{name: "migrate status com argumento extra", args: []string{"migrate", "status", "x"}},
		{name: "migrate up com flag", args: []string{"migrate", "up", "--help"}},
		{name: "migrate com modo vazio", args: []string{"migrate", ""}},
		{name: "modo em maiuscula", args: []string{"migrate", "UP"}},
		{name: "subcomando com erro de digitacao", args: []string{"migrat", "up"}},
		{name: "so o modo", args: []string{"up"}},
		{name: "so status", args: []string{"status"}},
		{name: "help longo", args: []string{"--help"}},
		{name: "help curto", args: []string{"-h"}},
		{name: "argumento vazio", args: []string{""}},
		{name: "server", args: []string{"server"}},
		{name: "migrate com flag antes", args: []string{"--migrate", "up"}},
		{name: "migrate up duas vezes", args: []string{"migrate", "up", "up"}},
		{name: "baseline nao existe", args: []string{"migrate", "baseline"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := novaBancada(t)
			env := envMigrar(map[string]string{"BIBLIOTECA_API_TOKEN": tokenTeste})
			code, stdout, stderr := rodar(t, tt.args, env)
			if code != 2 {
				t.Errorf("saída = %d, want 2", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want vazio", stdout)
			}
			if got := strings.TrimSpace(stderr); got != usoCLI {
				t.Errorf("stderr = %q, want %q", got, usoCLI)
			}
			if b.abertos != 0 || b.mig.subirChamadas != 0 || b.mig.statusChamadas != 0 {
				t.Errorf("abriu conexão ou chamou o migrador (abertos=%d)", b.abertos)
			}
		})
	}
}

// Sem argumentos o comportamento é o do servidor, que não conhece as variáveis
// de migração e não abre conexão de migração.
func TestDespacharSemArgumentosChamaORunExistente(t *testing.T) {
	tests := []struct {
		name    string
		args    []string
		env     map[string]string
		wantMsg string
	}{
		{name: "args nil sem DATABASE_URL", args: nil, env: map[string]string{"BIBLIOTECA_API_TOKEN": tokenTeste}, wantMsg: "config: DATABASE_URL ausente"},
		{name: "args vazio sem DATABASE_URL", args: []string{}, env: map[string]string{"BIBLIOTECA_API_TOKEN": tokenTeste}, wantMsg: "config: DATABASE_URL ausente"},
		{name: "sem token", args: nil, env: map[string]string{"DATABASE_URL": dsnFalsa}, wantMsg: "config: BIBLIOTECA_API_TOKEN ausente"},
		{
			name:    "DSN invalida, variaveis de migracao ignoradas",
			args:    nil,
			env:     map[string]string{"DATABASE_URL": "postgres://usuario-secreto:SENHA-DSN@127.0.0.1:porta/db", "BIBLIOTECA_API_TOKEN": tokenTeste, "BIBLIOTECA_MIGRATE_LOCK_TIMEOUT": "lixo"},
			wantMsg: msgDSN,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := novaBancada(t)
			code, stdout, stderr := rodar(t, tt.args, tt.env)
			if code != 1 {
				t.Errorf("saída = %d, want 1", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want vazio", stdout)
			}
			if !strings.Contains(stderr, tt.wantMsg) {
				t.Errorf("stderr = %q, want conter %q (erro do run existente)", stderr, tt.wantMsg)
			}
			if !strings.Contains(stderr, "biblioteca-bairro encerrado com erro") {
				t.Errorf("stderr = %q, want a linha do run existente", stderr)
			}
			if strings.Contains(stderr, msgErroFinal) {
				t.Errorf("stderr com a linha do migrate: o modo servidor não é migração")
			}
			semSegredos(t, stderr, tokenTeste)
			if b.abertos != 0 {
				t.Errorf("abriu conexão de migração %d vez(es) no modo servidor", b.abertos)
			}
		})
	}
}

// ---- migrate up ----

func TestMigrateUpSucesso(t *testing.T) {
	m := func(n int) []migrate.Migracao {
		var out []migrate.Migracao
		for i := 1; i <= n; i++ {
			out = append(out, migrate.Migracao{Versao: i, Nome: "x"})
		}
		return out
	}
	tests := []struct {
		name       string
		migs       []migrate.Migracao
		wantStdout string
	}{
		{name: "nenhuma pendente", migs: nil, wantStdout: "migrate up: nenhuma migração pendente\n"},
		{name: "lista vazia", migs: []migrate.Migracao{}, wantStdout: "migrate up: nenhuma migração pendente\n"},
		{name: "uma", migs: m(1), wantStdout: "migrate up: 1 migração(ões) aplicada(s)\n"},
		{name: "tres", migs: m(3), wantStdout: "migrate up: 3 migração(ões) aplicada(s)\n"},
		{name: "doze", migs: m(12), wantStdout: "migrate up: 12 migração(ões) aplicada(s)\n"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := novaBancada(t)
			b.mig.migs = tt.migs
			// Sem BIBLIOTECA_API_TOKEN: a migração não o exige.
			code, stdout, stderr := rodar(t, []string{"migrate", "up"}, envMigrar(nil))
			if code != 0 {
				t.Errorf("saída = %d, want 0 (stderr: %s)", code, stderr)
			}
			if stdout != tt.wantStdout {
				t.Errorf("stdout = %q, want %q", stdout, tt.wantStdout)
			}
			if strings.Contains(stderr, msgErroFinal) {
				t.Errorf("stderr tem linha de erro em sucesso: %s", stderr)
			}
			semSegredos(t, stdout+stderr)
			if b.abertos != 1 || b.fechados != 1 {
				t.Errorf("abertos=%d fechados=%d, want 1 e 1", b.abertos, b.fechados)
			}
			if b.mig.subirChamadas != 1 || b.mig.statusChamadas != 0 {
				t.Errorf("Subir=%d Status=%d, want 1 e 0", b.mig.subirChamadas, b.mig.statusChamadas)
			}
		})
	}
}

func TestMigrateRepassaAConfiguracao(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want config.ConfigMigracao
	}{
		{name: "padroes", env: envMigrar(nil), want: config.ConfigMigracao{DatabaseURL: dsnFalsa, LockTimeout: 5 * time.Second, StatementTimeout: 60 * time.Second}},
		{name: "informados", env: envMigrar(map[string]string{"BIBLIOTECA_MIGRATE_LOCK_TIMEOUT": "2s", "BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT": "90s"}), want: config.ConfigMigracao{DatabaseURL: dsnFalsa, LockTimeout: 2 * time.Second, StatementTimeout: 90 * time.Second}},
	}
	for _, modo := range []string{"up", "status"} {
		for _, tt := range tests {
			t.Run(modo+"/"+tt.name, func(t *testing.T) {
				b := novaBancada(t)
				code, _, stderr := rodar(t, []string{"migrate", modo}, tt.env)
				if code != 0 {
					t.Fatalf("saída = %d (stderr: %s)", code, stderr)
				}
				if b.cfg != tt.want {
					t.Errorf("config repassada = %+v, want %+v", b.cfg, tt.want)
				}
			})
		}
	}
}

// O logger entregue ao executor escreve JSON no stderr.
func TestMigrateLoggerDoExecutorVaiParaOStderr(t *testing.T) {
	b := novaBancada(t)
	b.aoAbrir = func(l *slog.Logger) {
		if l == nil {
			t.Error("logger nil entregue ao executor")
			return
		}
		l.Info("lock adquirido")
		l.Info("migração aplicada", slog.Int("versao", 1), slog.String("nome", "x"), slog.Int64("duracao_ms", 3))
	}
	b.mig.migs = []migrate.Migracao{{Versao: 1, Nome: "x"}}
	code, stdout, stderr := rodar(t, []string{"migrate", "up"}, envMigrar(nil))
	if code != 0 {
		t.Fatalf("saída = %d (stderr: %s)", code, stderr)
	}
	if strings.Contains(stdout, "lock adquirido") {
		t.Errorf("evento do executor foi para o stdout: %q", stdout)
	}
	var msgs []string
	for _, l := range linhasJSON(t, stderr) {
		if m, _ := l["msg"].(string); m != "" {
			msgs = append(msgs, m)
		}
	}
	if !contem(msgs, "lock adquirido") || !contem(msgs, "migração aplicada") {
		t.Errorf("mensagens no stderr = %v, want 'lock adquirido' e 'migração aplicada'", msgs)
	}
}

func contem(l []string, s string) bool {
	for _, x := range l {
		if x == s {
			return true
		}
	}
	return false
}

func TestMigratePropagaOContexto(t *testing.T) {
	b := novaBancada(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errb bytes.Buffer
	despachar(ctx, []string{"migrate", "up"}, ambiente(envMigrar(nil)), &out, &errb)
	if b.mig.ctxErr == nil {
		t.Error("o ctx cancelado do chamador não chegou ao migrador")
	}
}

func TestMigrateErros(t *testing.T) {
	arq := migrate.ErroChecksumDivergente
	arq.Arquivo = "0001_esquema_emprestimo.up.sql"
	sql := migrate.ErroSQL
	sql.Arquivo = "0002_x.up.sql"
	sql.Code = "23505"
	sql.Constraint = "x_uq"

	tests := []struct {
		name string
		err  error
		// wantErro, se não vazio, é o campo erro esperado; senão basta não vazar.
		wantErro string
	}{
		{name: "lock ocupado", err: migrate.ErrLockOcupado, wantErro: migrate.ErrLockOcupado.Error()},
		{name: "checksum divergente com arquivo", err: arq, wantErro: arq.Error()},
		{name: "erro de SQL com arquivo, code e constraint", err: sql, wantErro: sql.Error()},
		{name: "baseline", err: migrate.ErroBancoSemRegistro, wantErro: migrate.ErroBancoSemRegistro.Error()},
		{name: "transacao encerrada", err: migrate.ErroTransacaoEncerrada, wantErro: migrate.ErroTransacaoEncerrada.Error()},
		{name: "cancelado", err: migrate.ErroCancelado, wantErro: migrate.ErroCancelado.Error()},
		{name: "erro estranho vira mensagem fixa", err: errors.New("postgres://usuario-secreto:SENHA-DSN@127.0.0.1/db: INSERT INTO t VALUES ('SEGREDO-XYZ')")},
		{name: "erro estranho embrulhado", err: errors.Join(errors.New("SEGREDO-XYZ"), migrate.ErrLockOcupado)},
	}
	for _, modo := range []string{"up", "status"} {
		for _, tt := range tests {
			t.Run(modo+"/"+tt.name, func(t *testing.T) {
				b := novaBancada(t)
				b.mig.subirErr, b.mig.statusErr = tt.err, tt.err
				code, stdout, stderr := rodar(t, []string{"migrate", modo}, envMigrar(nil))
				if code != 1 {
					t.Errorf("saída = %d, want 1", code)
				}
				if stdout != "" {
					t.Errorf("stdout = %q, want vazio em erro", stdout)
				}
				erro := umaLinhaDeErro(t, stderr)
				if tt.wantErro != "" && erro != tt.wantErro {
					t.Errorf("campo erro = %q, want %q", erro, tt.wantErro)
				}
				semSegredos(t, stdout+stderr)
				if strings.Contains(stderr, "INSERT INTO") {
					t.Errorf("stderr traz trecho de SQL: %s", stderr)
				}
				if b.abertos != 1 || b.fechados != 1 {
					t.Errorf("abertos=%d fechados=%d, want 1 e 1 (fecha a conexão também em erro)", b.abertos, b.fechados)
				}
			})
		}
	}
}

func TestMigrateErroAoAbrir(t *testing.T) {
	for _, modo := range []string{"up", "status"} {
		t.Run(modo, func(t *testing.T) {
			b := novaBancada(t)
			b.erroAbrir = erroBoot("marcador fixo de teste")
			code, stdout, stderr := rodar(t, []string{"migrate", modo}, envMigrar(nil))
			if code != 1 {
				t.Errorf("saída = %d, want 1", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want vazio", stdout)
			}
			if erro := umaLinhaDeErro(t, stderr); erro != "marcador fixo de teste" {
				t.Errorf("campo erro = %q, want o erroBoot devolvido", erro)
			}
			if b.mig.subirChamadas+b.mig.statusChamadas != 0 {
				t.Error("chamou o migrador sem conexão")
			}
		})
	}
}

func TestMigrateConfiguracaoInvalida(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantMsg string
	}{
		{name: "sem DATABASE_URL", env: envMigrar(map[string]string{"DATABASE_URL": ""}), wantMsg: "config: DATABASE_URL ausente"},
		{name: "lock invalido", env: envMigrar(map[string]string{"BIBLIOTECA_MIGRATE_LOCK_TIMEOUT": "SEGREDO-XYZ"}), wantMsg: "config: BIBLIOTECA_MIGRATE_LOCK_TIMEOUT inválida"},
		{name: "lock sem unidade", env: envMigrar(map[string]string{"BIBLIOTECA_MIGRATE_LOCK_TIMEOUT": "5"}), wantMsg: "config: BIBLIOTECA_MIGRATE_LOCK_TIMEOUT inválida"},
		{name: "statement invalido", env: envMigrar(map[string]string{"BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT": "SEGREDO-XYZ"}), wantMsg: "config: BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT inválida"},
		{name: "statement fora do intervalo", env: envMigrar(map[string]string{"BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT": "2h"}), wantMsg: "config: BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT inválida"},
	}
	for _, modo := range []string{"up", "status"} {
		for _, tt := range tests {
			t.Run(modo+"/"+tt.name, func(t *testing.T) {
				b := novaBancada(t)
				code, stdout, stderr := rodar(t, []string{"migrate", modo}, tt.env)
				if code != 1 {
					t.Errorf("saída = %d, want 1", code)
				}
				if stdout != "" {
					t.Errorf("stdout = %q, want vazio", stdout)
				}
				if erro := umaLinhaDeErro(t, stderr); erro != tt.wantMsg {
					t.Errorf("campo erro = %q, want %q", erro, tt.wantMsg)
				}
				semSegredos(t, stdout+stderr, "2h")
				if b.abertos != 0 {
					t.Errorf("abriu conexão com configuração inválida")
				}
			})
		}
	}
}

// ---- migrate status ----

func TestMigrateStatusSaida(t *testing.T) {
	saoPaulo := time.FixedZone("BRT", -3*3600)
	apl := time.Date(2026, 10, 30, 9, 0, 0, 0, saoPaulo) // 12:00:00Z
	tests := []struct {
		name       string
		sits       []migrate.Situacao
		wantCode   int
		wantStdout string
		wantErro   bool
	}{
		{
			name: "aplicada e pendente",
			sits: []migrate.Situacao{
				{Versao: 1, Nome: "esquema_emprestimo", Estado: migrate.EstadoAplicada, AplicadaEm: apl},
				{Versao: 2, Nome: "proxima", Estado: migrate.EstadoPendente},
			},
			wantCode:   0,
			wantStdout: "0001 esquema_emprestimo aplicada 2026-10-30T12:00:00Z\n0002 proxima pendente\n",
		},
		{
			name: "so pendentes sai 0",
			sits: []migrate.Situacao{
				{Versao: 1, Nome: "a", Estado: migrate.EstadoPendente},
				{Versao: 2, Nome: "b", Estado: migrate.EstadoPendente},
			},
			wantCode:   0,
			wantStdout: "0001 a pendente\n0002 b pendente\n",
		},
		{
			name:       "aplicada em UTC ja",
			sits:       []migrate.Situacao{{Versao: 12, Nome: "doze", Estado: migrate.EstadoAplicada, AplicadaEm: time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)}},
			wantCode:   0,
			wantStdout: "0012 doze aplicada 2026-01-02T03:04:05Z\n",
		},
		{name: "nada a listar", sits: nil, wantCode: 0, wantStdout: ""},
		{
			name: "divergente sai 1 e ainda lista",
			sits: []migrate.Situacao{
				{Versao: 1, Nome: "a", Estado: migrate.EstadoAplicada, AplicadaEm: apl},
				{Versao: 2, Nome: "b", Estado: migrate.EstadoDivergente, AplicadaEm: apl},
				{Versao: 3, Nome: "c", Estado: migrate.EstadoPendente},
			},
			wantCode: 1,
			wantErro: true,
		},
		{
			name: "desconhecida sai 1 e ainda lista",
			sits: []migrate.Situacao{
				{Versao: 1, Nome: "a", Estado: migrate.EstadoAplicada, AplicadaEm: apl},
				{Versao: 2, Nome: "so_no_banco", Estado: migrate.EstadoDesconhecida, AplicadaEm: apl},
			},
			wantCode: 1,
			wantErro: true,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b := novaBancada(t)
			b.mig.sits = tt.sits
			code, stdout, stderr := rodar(t, []string{"migrate", "status"}, envMigrar(nil))
			if code != tt.wantCode {
				t.Errorf("saída = %d, want %d (stderr: %s)", code, tt.wantCode, stderr)
			}
			if tt.wantStdout != "" || tt.wantCode == 0 {
				if stdout != tt.wantStdout {
					t.Errorf("stdout = %q, want %q", stdout, tt.wantStdout)
				}
			}
			if tt.wantErro {
				umaLinhaDeErro(t, stderr)
				// Todas as versões aparecem, com o estado.
				for _, s := range tt.sits {
					prefixo := formatoVersao(s.Versao) + " " + s.Nome + " " + string(s.Estado)
					if !strings.Contains(stdout, prefixo) {
						t.Errorf("stdout = %q, want linha começando em %q", stdout, prefixo)
					}
				}
			} else if strings.Contains(stderr, msgErroFinal) {
				t.Errorf("stderr com linha de erro: %s", stderr)
			}
			if b.abertos != 1 || b.fechados != 1 {
				t.Errorf("abertos=%d fechados=%d, want 1 e 1", b.abertos, b.fechados)
			}
			if b.mig.statusChamadas != 1 || b.mig.subirChamadas != 0 {
				t.Errorf("Status=%d Subir=%d, want 1 e 0 (status é só leitura)", b.mig.statusChamadas, b.mig.subirChamadas)
			}
		})
	}
}

func formatoVersao(v int) string { return fmt.Sprintf("%04d", v) }

// ---- migrate sem banco: conexão que falha ----

func TestMigrateConexaoFalhaSemVazar(t *testing.T) {
	tests := []struct {
		name    string
		dsn     string
		wantErr string // trecho exato esperado no campo erro; vazio = qualquer não vazio
	}{
		{name: "porta fechada", dsn: "postgres://usuario-secreto:SENHA-DSN@127.0.0.1:1/db?connect_timeout=2&sslmode=disable"},
		{name: "DSN invalida", dsn: "postgres://usuario-secreto:SENHA-DSN@127.0.0.1:porta/db", wantErr: msgDSN},
		{name: "DSN chave-valor invalida", dsn: "host=127.0.0.1 user=usuario-secreto password=SENHA-DSN pool_max_conns=abc sslmode=bogus", wantErr: msgDSN},
	}
	for _, modo := range []string{"up", "status"} {
		for _, tt := range tests {
			t.Run(modo+"/"+tt.name, func(t *testing.T) {
				// Usa o abrirMigrador real: sem banco, tem que falhar fechado.
				env := map[string]string{"DATABASE_URL": tt.dsn}
				inicio := time.Now()
				code, stdout, stderr := rodar(t, []string{"migrate", modo}, env)
				if d := time.Since(inicio); d > 20*time.Second {
					t.Errorf("levou %v para falhar", d)
				}
				if code != 1 {
					t.Errorf("saída = %d, want 1", code)
				}
				if stdout != "" {
					t.Errorf("stdout = %q, want vazio", stdout)
				}
				erro := umaLinhaDeErro(t, stderr)
				if tt.wantErr != "" && erro != tt.wantErr {
					t.Errorf("campo erro = %q, want %q", erro, tt.wantErr)
				}
				semSegredos(t, stdout+stderr, "127.0.0.1", "sslmode", "pool_max_conns")
			})
		}
	}
}

// ---- ponta a ponta com banco real ----

const (
	envURLBanco         = "BIBLIOTECA_TEST_DATABASE_URL"
	envBancoObrigatorio = "BIBLIOTECA_TEST_DB_OBRIGATORIO"
)

func urlBancoCLI(t *testing.T) string {
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

// schemaCLI cria um schema descartável e devolve a DSN com search_path nele e
// uma conexão observadora no mesmo schema.
func schemaCLI(t *testing.T) (dsn string, obs *pgx.Conn) {
	t.Helper()
	base := urlBancoCLI(t)
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("gerar nome do schema: %v", err)
	}
	schema := "t_" + hex.EncodeToString(b[:])
	ident := pgx.Identifier{schema}.Sanitize()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	adm, err := pgx.Connect(ctx, base)
	if err != nil {
		t.Fatalf("conectar ao banco de teste: falhou (confira %s)", envURLBanco)
	}
	defer adm.Close(ctx)
	if _, err := adm.Exec(ctx, "CREATE SCHEMA "+ident); err != nil {
		t.Fatalf("criar schema: %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		c, err := pgx.Connect(ctx, base)
		if err != nil {
			t.Errorf("conectar para remover schema %s: falhou", schema)
			return
		}
		defer c.Close(ctx)
		if _, err := c.Exec(ctx, "DROP SCHEMA "+ident+" CASCADE"); err != nil {
			t.Errorf("remover schema %s: %v", schema, err)
		}
	})

	if strings.Contains(base, "://") {
		u, err := url.Parse(base)
		if err != nil {
			t.Fatalf("interpretar %s: falhou", envURLBanco)
		}
		q := u.Query()
		q.Set("search_path", schema)
		u.RawQuery = q.Encode()
		dsn = u.String()
	} else {
		dsn = base + " search_path=" + schema
	}
	cfg, err := pgx.ParseConfig(dsn)
	if err != nil {
		t.Fatalf("interpretar DSN do teste: falhou")
	}
	obs, err = pgx.ConnectConfig(ctx, cfg)
	if err != nil {
		t.Fatalf("conectar observador: falhou")
	}
	t.Cleanup(func() { _ = obs.Close(context.Background()) })
	return dsn, obs
}

// TestMigrateFimAFimComBancoReal usa a chave de advisory lock real: é o único
// teste do repositório que o faz (G13), e roda os passos em sequência.
func TestMigrateFimAFimComBancoReal(t *testing.T) {
	dsn, obs := schemaCLI(t)
	env := map[string]string{"DATABASE_URL": dsn, "BIBLIOTECA_MIGRATE_LOCK_TIMEOUT": "10s"}
	esperadas, err := migrate.ValidarFS(migrations.FS)
	if err != nil {
		t.Fatalf("ValidarFS(migrations.FS): %v", err)
	}

	contar := func() int {
		var n int
		if err := obs.QueryRow(t.Context(), "SELECT count(*) FROM migracao_aplicada").Scan(&n); err != nil {
			t.Fatalf("contar registro: %v", err)
		}
		return n
	}

	t.Run("status antes de tudo nao cria nada", func(t *testing.T) {
		code, stdout, stderr := rodar(t, []string{"migrate", "status"}, env)
		if code != 0 {
			t.Fatalf("saída = %d (stderr: %s)", code, stderr)
		}
		if want := "0001 esquema_emprestimo pendente\n"; !strings.HasPrefix(stdout, want) {
			t.Errorf("stdout = %q, want começar com %q", stdout, want)
		}
		var existe bool
		if err := obs.QueryRow(t.Context(), "SELECT to_regclass(quote_ident(current_schema()) || '.migracao_aplicada') IS NOT NULL").Scan(&existe); err != nil || existe {
			t.Errorf("status criou migracao_aplicada (existe=%v, err=%v)", existe, err)
		}
	})

	t.Run("primeiro up aplica tudo", func(t *testing.T) {
		code, stdout, stderr := rodar(t, []string{"migrate", "up"}, env)
		if code != 0 {
			t.Fatalf("saída = %d (stderr: %s)", code, stderr)
		}
		want := "migrate up: " + strconv.Itoa(len(esperadas)) + " migração(ões) aplicada(s)\n"
		if stdout != want {
			t.Errorf("stdout = %q, want %q", stdout, want)
		}
		var msgs []string
		for _, l := range linhasJSON(t, stderr) {
			if m, _ := l["msg"].(string); m != "" {
				msgs = append(msgs, m)
			}
		}
		if !contem(msgs, "lock adquirido") || !contem(msgs, "migração aplicada") {
			t.Errorf("stderr sem os eventos do executor; mensagens = %v", msgs)
		}
		semSegredos(t, stdout+stderr)
		if strings.Contains(stderr, msgErroFinal) {
			t.Errorf("stderr com linha de erro em sucesso: %s", stderr)
		}
		if n := contar(); n != len(esperadas) {
			t.Errorf("registro com %d linhas, want %d", n, len(esperadas))
		}
	})

	t.Run("segundo up e no-op", func(t *testing.T) {
		code, stdout, stderr := rodar(t, []string{"migrate", "up"}, env)
		if code != 0 || stdout != "migrate up: nenhuma migração pendente\n" {
			t.Errorf("saída = %d stdout = %q (stderr: %s), want 0 e 'nenhuma migração pendente'", code, stdout, stderr)
		}
		if n := contar(); n != len(esperadas) {
			t.Errorf("registro com %d linhas, want %d", n, len(esperadas))
		}
	})

	t.Run("status depois do up", func(t *testing.T) {
		code, stdout, stderr := rodar(t, []string{"migrate", "status"}, env)
		if code != 0 {
			t.Fatalf("saída = %d (stderr: %s)", code, stderr)
		}
		linhas := strings.Split(strings.TrimSuffix(stdout, "\n"), "\n")
		if len(linhas) != len(esperadas) {
			t.Fatalf("stdout = %q, want %d linhas", stdout, len(esperadas))
		}
		campos := strings.Fields(linhas[0])
		if len(campos) != 4 || campos[0] != "0001" || campos[1] != "esquema_emprestimo" || campos[2] != "aplicada" {
			t.Fatalf("linha 1 = %q, want '0001 esquema_emprestimo aplicada <RFC3339 UTC>'", linhas[0])
		}
		ts, err := time.Parse(time.RFC3339, campos[3])
		if err != nil || !strings.HasSuffix(campos[3], "Z") {
			t.Errorf("aplicada_em %q não é RFC3339 em UTC (%v)", campos[3], err)
		}
		if d := time.Since(ts); d < -time.Minute || d > time.Hour {
			t.Errorf("aplicada_em %v longe de agora", ts)
		}
	})

	t.Run("checksum adulterado no banco: status e up saem 1", func(t *testing.T) {
		if _, err := obs.Exec(t.Context(), "UPDATE migracao_aplicada SET checksum = repeat('0', 64) WHERE versao = 1"); err != nil {
			t.Fatalf("adulterar checksum: %v", err)
		}
		code, stdout, stderr := rodar(t, []string{"migrate", "status"}, env)
		if code != 1 {
			t.Errorf("status: saída = %d, want 1", code)
		}
		if !strings.Contains(stdout, "0001 esquema_emprestimo divergente") {
			t.Errorf("status: stdout = %q, want linha 'divergente'", stdout)
		}
		umaLinhaDeErro(t, stderr)

		code, stdout, stderr = rodar(t, []string{"migrate", "up"}, env)
		if code != 1 || stdout != "" {
			t.Errorf("up: saída = %d stdout = %q, want 1 e vazio", code, stdout)
		}
		erro := umaLinhaDeErro(t, stderr)
		if !strings.Contains(erro, "0001_esquema_emprestimo.up.sql") {
			t.Errorf("erro = %q, want conter o nome do arquivo", erro)
		}
		if strings.Contains(stderr, "CREATE TABLE") {
			t.Errorf("stderr traz conteúdo do SQL: %s", stderr)
		}
		var cs string
		if err := obs.QueryRow(t.Context(), "SELECT checksum FROM migracao_aplicada WHERE versao = 1").Scan(&cs); err != nil || cs != strings.Repeat("0", 64) {
			t.Errorf("checksum no banco = %q (%v): o up não pode atualizá-lo", cs, err)
		}
	})
}

// Banco com esquema e sem registro: o CLI falha fechado e não cria nada.
func TestMigrateBancoSemRegistroFalhaFechado(t *testing.T) {
	dsn, obs := schemaCLI(t)
	if _, err := obs.Exec(t.Context(), "CREATE TABLE solta (x integer)"); err != nil {
		t.Fatalf("preparar: %v", err)
	}
	env := map[string]string{"DATABASE_URL": dsn, "BIBLIOTECA_MIGRATE_LOCK_TIMEOUT": "10s"}
	for _, modo := range []string{"up", "status"} {
		t.Run(modo, func(t *testing.T) {
			code, stdout, stderr := rodar(t, []string{"migrate", modo}, env)
			if code != 1 {
				t.Errorf("saída = %d, want 1", code)
			}
			if stdout != "" {
				t.Errorf("stdout = %q, want vazio", stdout)
			}
			umaLinhaDeErro(t, stderr)
			var n int
			if err := obs.QueryRow(t.Context(), `
				SELECT count(*) FROM pg_class
				 WHERE relnamespace = to_regnamespace(quote_ident(current_schema()))`).Scan(&n); err != nil || n != 1 {
				t.Errorf("relações no schema = %d (%v), want 1 (só a tabela solta)", n, err)
			}
		})
	}
}
