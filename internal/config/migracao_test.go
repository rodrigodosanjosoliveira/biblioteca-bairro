package config_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/config"
)

// Contrato fixado por estes testes (DEL-008, G11):
//
//	type ConfigMigracao struct {
//	    DatabaseURL      string
//	    LockTimeout      time.Duration
//	    StatementTimeout time.Duration
//	}
//	func CarregarMigracao(getenv func(string) string) (ConfigMigracao, error)
//
// Todo erro é config.Erro (string constante, sem eco do valor recebido).

const (
	envLock = "BIBLIOTECA_MIGRATE_LOCK_TIMEOUT"
	envStmt = "BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT"

	msgLockInvalida = "config: BIBLIOTECA_MIGRATE_LOCK_TIMEOUT inválida"
	msgStmtInvalida = "config: BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT inválida"
	msgDSNAusente   = "config: DATABASE_URL ausente"

	lockMin = 100 * time.Millisecond
	lockMax = 60 * time.Second
	stmtMin = time.Second
	stmtMax = 30 * time.Minute
)

// baseMig devolve um ambiente de migração válido, sem token (a migração não
// exige BIBLIOTECA_API_TOKEN). Valor "" apaga a chave.
func baseMig(muda map[string]string) map[string]string {
	m := map[string]string{"DATABASE_URL": dsnTeste}
	for k, v := range muda {
		if v == "" {
			delete(m, k)
			continue
		}
		m[k] = v
	}
	return m
}

func TestCarregarMigracaoValido(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want config.ConfigMigracao
	}{
		{name: "padroes", env: baseMig(nil), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: 5 * time.Second, StatementTimeout: 60 * time.Second}},
		{name: "valores vazios dao o padrao", env: map[string]string{"DATABASE_URL": dsnTeste, envLock: "", envStmt: ""}, want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: 5 * time.Second, StatementTimeout: 60 * time.Second}},
		{name: "unidades ms e s", env: baseMig(map[string]string{envLock: "750ms", envStmt: "90s"}), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: 750 * time.Millisecond, StatementTimeout: 90 * time.Second}},
		{name: "unidade m", env: baseMig(map[string]string{envLock: "1m", envStmt: "2m"}), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: time.Minute, StatementTimeout: 2 * time.Minute}},
		{name: "fracao exata em ms", env: baseMig(map[string]string{envLock: "1.5s", envStmt: "2.25s"}), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: 1500 * time.Millisecond, StatementTimeout: 2250 * time.Millisecond}},
		{name: "lock no minimo", env: baseMig(map[string]string{envLock: "100ms"}), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: lockMin, StatementTimeout: 60 * time.Second}},
		{name: "lock minimo mais 1ms", env: baseMig(map[string]string{envLock: "101ms"}), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: lockMin + time.Millisecond, StatementTimeout: 60 * time.Second}},
		{name: "lock maximo menos 1ms", env: baseMig(map[string]string{envLock: "59999ms"}), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: lockMax - time.Millisecond, StatementTimeout: 60 * time.Second}},
		{name: "lock no maximo", env: baseMig(map[string]string{envLock: "60s"}), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: lockMax, StatementTimeout: 60 * time.Second}},
		{name: "lock no maximo em minutos", env: baseMig(map[string]string{envLock: "1m"}), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: lockMax, StatementTimeout: 60 * time.Second}},
		{name: "statement no minimo", env: baseMig(map[string]string{envStmt: "1s"}), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: 5 * time.Second, StatementTimeout: stmtMin}},
		{name: "statement minimo mais 1ms", env: baseMig(map[string]string{envStmt: "1001ms"}), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: 5 * time.Second, StatementTimeout: stmtMin + time.Millisecond}},
		{name: "statement maximo menos 1ms", env: baseMig(map[string]string{envStmt: "1799999ms"}), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: 5 * time.Second, StatementTimeout: stmtMax - time.Millisecond}},
		{name: "statement no maximo", env: baseMig(map[string]string{envStmt: "30m"}), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: 5 * time.Second, StatementTimeout: stmtMax}},
		{name: "statement no maximo em horas fracionadas", env: baseMig(map[string]string{envStmt: "0.5h"}), want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: 5 * time.Second, StatementTimeout: stmtMax}},
		{name: "DATABASE_URL nao e interpretada", env: baseMig(map[string]string{"DATABASE_URL": "qualquer coisa :: não é DSN"}), want: config.ConfigMigracao{DatabaseURL: "qualquer coisa :: não é DSN", LockTimeout: 5 * time.Second, StatementTimeout: 60 * time.Second}},
		{
			name: "token, ADDR e max conns invalidos sao ignorados",
			env:  baseMig(map[string]string{"BIBLIOTECA_API_TOKEN": "curto", "ADDR": "lixo", "BIBLIOTECA_DB_MAX_CONNS": "xyz"}),
			want: config.ConfigMigracao{DatabaseURL: dsnTeste, LockTimeout: 5 * time.Second, StatementTimeout: 60 * time.Second},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.CarregarMigracao(ambiente(tt.env))
			if err != nil {
				t.Fatalf("CarregarMigracao() erro = %v, want nil", err)
			}
			if got != tt.want {
				t.Errorf("CarregarMigracao() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCarregarMigracaoInvalido(t *testing.T) {
	tests := []struct {
		name    string
		env     map[string]string
		wantMsg string
		// valor é o texto que não pode aparecer na mensagem.
		valor string
	}{
		{name: "sem DATABASE_URL", env: baseMig(map[string]string{"DATABASE_URL": ""}), wantMsg: msgDSNAusente},
		{name: "sem DATABASE_URL tem precedencia sobre timeout invalido", env: baseMig(map[string]string{"DATABASE_URL": "", envLock: "lixo"}), wantMsg: msgDSNAusente},
		{name: "lock abaixo do minimo", env: baseMig(map[string]string{envLock: "99ms"}), wantMsg: msgLockInvalida, valor: "99ms"},
		{name: "lock acima do maximo", env: baseMig(map[string]string{envLock: "60001ms"}), wantMsg: msgLockInvalida, valor: "60001ms"},
		{name: "lock muito acima", env: baseMig(map[string]string{envLock: "24h"}), wantMsg: msgLockInvalida, valor: "24h"},
		{name: "lock zero", env: baseMig(map[string]string{envLock: "0s"}), wantMsg: msgLockInvalida, valor: "0s"},
		{name: "lock zero sem unidade", env: baseMig(map[string]string{envLock: "0"}), wantMsg: msgLockInvalida},
		{name: "lock zero em ms", env: baseMig(map[string]string{envLock: "0ms"}), wantMsg: msgLockInvalida, valor: "0ms"},
		{name: "lock negativo", env: baseMig(map[string]string{envLock: "-5s"}), wantMsg: msgLockInvalida, valor: "-5s"},
		{name: "lock sem unidade", env: baseMig(map[string]string{envLock: "5"}), wantMsg: msgLockInvalida},
		{name: "lock sem unidade grande", env: baseMig(map[string]string{envLock: "5000"}), wantMsg: msgLockInvalida, valor: "5000"},
		{name: "lock nao multiplo de 1ms", env: baseMig(map[string]string{envLock: "100.5ms"}), wantMsg: msgLockInvalida, valor: "100.5ms"},
		{name: "lock em microssegundos nao multiplo", env: baseMig(map[string]string{envLock: "100001us"}), wantMsg: msgLockInvalida, valor: "100001us"},
		{name: "lock em nanossegundos nao multiplo", env: baseMig(map[string]string{envLock: "1000000001ns"}), wantMsg: msgLockInvalida, valor: "1000000001ns"},
		{name: "lock lixo", env: baseMig(map[string]string{envLock: "SEGREDO-XYZ"}), wantMsg: msgLockInvalida, valor: "SEGREDO-XYZ"},
		{name: "lock com espaco no inicio", env: baseMig(map[string]string{envLock: " 5s"}), wantMsg: msgLockInvalida},
		{name: "lock com espaco no fim", env: baseMig(map[string]string{envLock: "5s "}), wantMsg: msgLockInvalida},
		{name: "lock com quebra de linha", env: baseMig(map[string]string{envLock: "5s\n"}), wantMsg: msgLockInvalida},
		{name: "lock com NUL", env: baseMig(map[string]string{envLock: "5s\x00"}), wantMsg: msgLockInvalida},
		{name: "lock em maiuscula", env: baseMig(map[string]string{envLock: "5S"}), wantMsg: msgLockInvalida, valor: "5S"},
		{name: "lock notacao cientifica", env: baseMig(map[string]string{envLock: "1e3ms"}), wantMsg: msgLockInvalida, valor: "1e3ms"},
		{name: "lock estouro", env: baseMig(map[string]string{envLock: "9999999999999h"}), wantMsg: msgLockInvalida, valor: "9999999999999h"},
		{name: "lock so sinal", env: baseMig(map[string]string{envLock: "-"}), wantMsg: msgLockInvalida},
		{name: "lock so unidade", env: baseMig(map[string]string{envLock: "s"}), wantMsg: msgLockInvalida},
		{name: "statement abaixo do minimo", env: baseMig(map[string]string{envStmt: "999ms"}), wantMsg: msgStmtInvalida, valor: "999ms"},
		{name: "statement acima do maximo", env: baseMig(map[string]string{envStmt: "1800001ms"}), wantMsg: msgStmtInvalida, valor: "1800001ms"},
		{name: "statement muito acima", env: baseMig(map[string]string{envStmt: "2h"}), wantMsg: msgStmtInvalida, valor: "2h"},
		{name: "statement zero", env: baseMig(map[string]string{envStmt: "0s"}), wantMsg: msgStmtInvalida, valor: "0s"},
		{name: "statement zero sem unidade", env: baseMig(map[string]string{envStmt: "0"}), wantMsg: msgStmtInvalida},
		{name: "statement negativo", env: baseMig(map[string]string{envStmt: "-1m"}), wantMsg: msgStmtInvalida, valor: "-1m"},
		{name: "statement sem unidade", env: baseMig(map[string]string{envStmt: "60"}), wantMsg: msgStmtInvalida, valor: "60"},
		{name: "statement nao multiplo de 1ms", env: baseMig(map[string]string{envStmt: "1.0005s"}), wantMsg: msgStmtInvalida, valor: "1.0005s"},
		{name: "statement lixo", env: baseMig(map[string]string{envStmt: "SEGREDO-XYZ"}), wantMsg: msgStmtInvalida, valor: "SEGREDO-XYZ"},
		{name: "statement com espaco", env: baseMig(map[string]string{envStmt: "60 s"}), wantMsg: msgStmtInvalida},
		{name: "statement nao ASCII", env: baseMig(map[string]string{envStmt: "６０s"}), wantMsg: msgStmtInvalida, valor: "６０s"},
		{name: "statement estouro", env: baseMig(map[string]string{envStmt: "9999999999999h"}), wantMsg: msgStmtInvalida, valor: "9999999999999h"},
		{name: "lock invalido tem precedencia sobre statement invalido", env: baseMig(map[string]string{envLock: "lixo", envStmt: "lixo"}), wantMsg: msgLockInvalida},
		{name: "statement invalido com lock valido", env: baseMig(map[string]string{envLock: "1s", envStmt: "lixo"}), wantMsg: msgStmtInvalida},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.CarregarMigracao(ambiente(tt.env))
			if err == nil {
				t.Fatalf("CarregarMigracao() = %+v, nil, want erro %q", got, tt.wantMsg)
			}
			if got != (config.ConfigMigracao{}) {
				t.Errorf("config em erro = %+v, want zero", got)
			}
			var ce config.Erro
			if !errors.As(err, &ce) {
				t.Fatalf("erro %T, want config.Erro", err)
			}
			if err.Error() != tt.wantMsg {
				t.Errorf("erro = %q, want %q", err.Error(), tt.wantMsg)
			}
			if errors.Unwrap(err) != nil {
				t.Errorf("erro embrulha outro (%T), want mensagem fixa sem causa", errors.Unwrap(err))
			}
			if tt.valor != "" && strings.Contains(err.Error(), tt.valor) {
				t.Errorf("erro %q ecoa o valor %q", err.Error(), tt.valor)
			}
			if strings.Contains(err.Error(), dsnTeste) || strings.Contains(err.Error(), "SENHA-DSN") {
				t.Errorf("erro %q ecoa a DATABASE_URL", err.Error())
			}
		})
	}
}

// CarregarMigracao lê só DATABASE_URL e as duas variáveis de timeout.
func TestCarregarMigracaoSoLeAsVariaveisProprias(t *testing.T) {
	lidas := map[string]bool{}
	getenv := func(k string) string {
		lidas[k] = true
		switch k {
		case "DATABASE_URL":
			return dsnTeste
		default:
			return ""
		}
	}
	if _, err := config.CarregarMigracao(getenv); err != nil {
		t.Fatalf("CarregarMigracao() erro = %v", err)
	}
	permitidas := map[string]bool{"DATABASE_URL": true, envLock: true, envStmt: true}
	for k := range lidas {
		if !permitidas[k] {
			t.Errorf("CarregarMigracao leu %q, que não é variável da migração", k)
		}
	}
}

// Carregar (servidor) não ganha as variáveis de migração e segue igual.
func TestCarregarIgnoraVariaveisDeMigracao(t *testing.T) {
	env := base(map[string]string{envLock: "lixo", envStmt: "lixo"})
	got, err := config.Carregar(ambiente(env))
	if err != nil {
		t.Fatalf("Carregar() erro = %v, want nil (as variáveis de migração não valem para o servidor)", err)
	}
	want := config.Config{DatabaseURL: dsnTeste, Token: tokenTeste, Addr: ":8080", MaxConns: 10}
	if got != want {
		t.Errorf("Carregar() = %+v, want %+v", got, want)
	}
}

// FuzzLerTimeout exercita a leitura dos dois timeouts por CarregarMigracao
// (a função interna não faz parte do contrato). Invariantes: sem pânico; em
// sucesso, o valor está no intervalo e é múltiplo exato de 1 ms; em erro, a
// mensagem é uma das duas fixas, sem eco da entrada.
func FuzzLerTimeout(f *testing.F) {
	for _, s := range []string{
		"", "5s", "100ms", "60s", "1m", "30m", "0", "0s", "-5s", "5", "99ms", "60001ms",
		"1.5s", "100.5ms", "1e3ms", " 5s", "5s\n", "SEGREDO-XYZ", "9999999999999h", "１s", "5µs", "+5s", "1h", "999ms",
	} {
		f.Add(s, s)
	}
	f.Fuzz(func(t *testing.T, lock, stmt string) {
		env := map[string]string{"DATABASE_URL": dsnTeste, envLock: lock, envStmt: stmt}
		got, err := config.CarregarMigracao(ambiente(env))
		if err != nil {
			var ce config.Erro
			if !errors.As(err, &ce) {
				t.Fatalf("erro %T, want config.Erro", err)
			}
			if m := err.Error(); m != msgLockInvalida && m != msgStmtInvalida {
				t.Fatalf("mensagem fora das fixas: %q", m)
			}
			return
		}
		if got.DatabaseURL != dsnTeste {
			t.Fatalf("DatabaseURL = %q", got.DatabaseURL)
		}
		if lock == "" && got.LockTimeout != 5*time.Second {
			t.Fatalf("lock vazio = %v, want 5s", got.LockTimeout)
		}
		if stmt == "" && got.StatementTimeout != 60*time.Second {
			t.Fatalf("statement vazio = %v, want 60s", got.StatementTimeout)
		}
		if got.LockTimeout < lockMin || got.LockTimeout > lockMax || got.LockTimeout%time.Millisecond != 0 {
			t.Fatalf("lock = %v fora do intervalo [100ms, 60s] ou não múltiplo de 1ms (entrada %q)", got.LockTimeout, lock)
		}
		if got.StatementTimeout < stmtMin || got.StatementTimeout > stmtMax || got.StatementTimeout%time.Millisecond != 0 {
			t.Fatalf("statement = %v fora do intervalo [1s, 30m] ou não múltiplo de 1ms (entrada %q)", got.StatementTimeout, stmt)
		}
	})
}
