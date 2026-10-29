// Package config lê a configuração do processo a partir de variáveis de
// ambiente. Nenhuma mensagem de erro ecoa o valor recebido.
package config

import "strconv"

// Limites do token da API, em bytes.
const (
	tokenMin = 32
	tokenMax = 512
)

// Limites e padrões do pool.
const (
	maxConnsPadrao = 10
	maxConnsMin    = 1
	maxConnsMax    = 100
	addrPadrao     = ":8080"
)

// Erro é um erro de configuração. É uma string constante: a mensagem é fixa e
// nunca embrulha outro erro nem carrega o valor recebido.
type Erro string

func (e Erro) Error() string { return string(e) }

// Erros de configuração. As mensagens são fixas: dentro da mesma regra
// violada, não variam com o valor recebido.
const (
	errDatabaseURLAusente Erro = "config: DATABASE_URL ausente"
	errTokenAusente       Erro = "config: BIBLIOTECA_API_TOKEN ausente"
	errTokenTamanho       Erro = "config: BIBLIOTECA_API_TOKEN fora do tamanho permitido"
	errTokenCaractere     Erro = "config: BIBLIOTECA_API_TOKEN com caractere fora do ASCII imprimível sem espaço"
	errMaxConns           Erro = "config: BIBLIOTECA_DB_MAX_CONNS inválida"
)

// Config é a configuração do processo.
type Config struct {
	// DatabaseURL é o DSN do PostgreSQL. Não é interpretado aqui.
	DatabaseURL string
	// Token é o bearer token aceito pelas rotas autenticadas.
	Token string
	// Addr é o endereço de escuta do servidor HTTP.
	Addr string
	// MaxConns é o tamanho máximo do pool de conexões.
	MaxConns int32
}

// Carregar lê a configuração por getenv:
//
//   - DATABASE_URL: obrigatória; não é interpretada.
//   - BIBLIOTECA_API_TOKEN: obrigatória; de 32 a 512 bytes, só ASCII de 0x21
//     a 0x7E; sem trim.
//   - ADDR: padrão ":8080".
//   - BIBLIOTECA_DB_MAX_CONNS: inteiro de 1 a 100; padrão 10.
//
// Em erro devolve Config{} e um Erro.
func Carregar(getenv func(string) string) (Config, error) {
	dsn := getenv("DATABASE_URL")
	if dsn == "" {
		return Config{}, errDatabaseURLAusente
	}
	token := getenv("BIBLIOTECA_API_TOKEN")
	if err := validarToken(token); err != nil {
		return Config{}, err
	}
	addr := getenv("ADDR")
	if addr == "" {
		addr = addrPadrao
	}
	maxConns, err := lerMaxConns(getenv("BIBLIOTECA_DB_MAX_CONNS"))
	if err != nil {
		return Config{}, err
	}
	return Config{DatabaseURL: dsn, Token: token, Addr: addr, MaxConns: maxConns}, nil
}

// validarToken aplica as regras do token sem citar o valor nem o tamanho.
func validarToken(token string) error {
	if token == "" {
		return errTokenAusente
	}
	if len(token) < tokenMin || len(token) > tokenMax {
		return errTokenTamanho
	}
	for i := 0; i < len(token); i++ {
		if c := token[i]; c < 0x21 || c > 0x7E {
			return errTokenCaractere
		}
	}
	return nil
}

// lerMaxConns interpreta BIBLIOTECA_DB_MAX_CONNS; vazio dá o padrão.
func lerMaxConns(s string) (int32, error) {
	if s == "" {
		return maxConnsPadrao, nil
	}
	n, err := strconv.ParseInt(s, 10, 32)
	if err != nil || n < maxConnsMin || n > maxConnsMax {
		return 0, errMaxConns
	}
	return int32(n), nil
}
