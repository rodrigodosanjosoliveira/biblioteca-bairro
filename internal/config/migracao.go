package config

import "time"

// Padrões e limites dos prazos da migração.
const (
	lockTimeoutPadrao = 5 * time.Second
	lockTimeoutMin    = 100 * time.Millisecond
	lockTimeoutMax    = 60 * time.Second

	statementTimeoutPadrao = 60 * time.Second
	statementTimeoutMin    = time.Second
	statementTimeoutMax    = 30 * time.Minute
)

// Erros de configuração da migração, com mensagens fixas que não citam o
// valor recebido.
const (
	errLockTimeout      Erro = "config: BIBLIOTECA_MIGRATE_LOCK_TIMEOUT inválida"
	errStatementTimeout Erro = "config: BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT inválida"
)

// ConfigMigracao é a configuração do subcomando migrate. Ela não depende do
// token, de ADDR nem do tamanho do pool, que são só do servidor.
type ConfigMigracao struct {
	// DatabaseURL é o DSN do PostgreSQL. Não é interpretado aqui.
	DatabaseURL string
	// LockTimeout é o prazo de espera pelo advisory lock e o lock_timeout de
	// cada transação de migração.
	LockTimeout time.Duration
	// StatementTimeout é o statement_timeout de cada transação de migração. O
	// PostgreSQL o aplica por COMANDO, e cada arquivo vai numa mensagem
	// simple-Query com vários comandos: a transação do arquivo NÃO tem teto
	// total de duração. N statements lentos podem ocupar N vezes o prazo,
	// segurando os locks ACCESS EXCLUSIVE tomados no início. O lock_timeout vale
	// por aquisição de lock.
	StatementTimeout time.Duration
}

// CarregarMigracao lê a configuração da migração por getenv:
//
//   - DATABASE_URL: obrigatória; não é interpretada.
//   - BIBLIOTECA_MIGRATE_LOCK_TIMEOUT: duração de time.ParseDuration, múltiplo
//     exato de 1 ms, de 100 ms a 60 s; padrão 5 s.
//   - BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT: idem, de 1 s a 30 min; padrão 60 s.
//
// Valor vazio dá o padrão; zero é recusado. A validação segue a ordem
// DATABASE_URL, lock, statement. Em erro devolve ConfigMigracao{} e um Erro.
func CarregarMigracao(getenv func(string) string) (ConfigMigracao, error) {
	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		return ConfigMigracao{}, errDatabaseURLAusente
	}
	lock, err := lerPrazo(getenv("BIBLIOTECA_MIGRATE_LOCK_TIMEOUT"), lockTimeoutPadrao, lockTimeoutMin, lockTimeoutMax, errLockTimeout)
	if err != nil {
		return ConfigMigracao{}, err
	}
	stmt, err := lerPrazo(getenv("BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT"), statementTimeoutPadrao, statementTimeoutMin, statementTimeoutMax, errStatementTimeout)
	if err != nil {
		return ConfigMigracao{}, err
	}
	return ConfigMigracao{DatabaseURL: dsn, LockTimeout: lock, StatementTimeout: stmt}, nil
}

// lerPrazo interpreta s com time.ParseDuration. Vazio dá o padrão; o resultado
// tem de ficar em [piso, teto] e ser múltiplo exato de 1 ms. O erro devolvido é
// sempre falha, sem o valor nem o erro de ParseDuration.
func lerPrazo(s string, padrao, piso, teto time.Duration, falha Erro) (time.Duration, error) {
	if s == "" {
		return padrao, nil
	}
	d, err := time.ParseDuration(s)
	if err != nil || d < piso || d > teto || d%time.Millisecond != 0 {
		return 0, falha
	}
	return d, nil
}
