package config_test

import (
	"strconv"
	"strings"
	"testing"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/config"
)

const (
	dsnTeste   = "postgres://usuario:SENHA-DSN@127.0.0.1:5432/biblioteca"
	tokenTeste = "TOKEN-SECRETO-0123456789abcdefghijklmn" // 38 bytes
)

// ambiente devolve um getenv sobre o mapa m.
func ambiente(m map[string]string) func(string) string {
	return func(k string) string { return m[k] }
}

// base devolve um ambiente válido; muda aplica as alterações (valor "" apaga).
func base(muda map[string]string) map[string]string {
	m := map[string]string{
		"DATABASE_URL":         dsnTeste,
		"BIBLIOTECA_API_TOKEN": tokenTeste,
	}
	for k, v := range muda {
		if v == "" {
			delete(m, k)
			continue
		}
		m[k] = v
	}
	return m
}

// imprimiveis devolve os 94 bytes ASCII de 0x21 a 0x7E.
func imprimiveis() string {
	var b strings.Builder
	for c := byte(0x21); c <= 0x7E; c++ {
		b.WriteByte(c)
	}
	return b.String()
}

func TestCarregarValido(t *testing.T) {
	tests := []struct {
		name string
		env  map[string]string
		want config.Config
	}{
		{
			name: "padroes",
			env:  base(nil),
			want: config.Config{DatabaseURL: dsnTeste, Token: tokenTeste, Addr: ":8080", MaxConns: 10},
		},
		{
			name: "addr e max conns informados",
			env:  base(map[string]string{"ADDR": "127.0.0.1:9000", "BIBLIOTECA_DB_MAX_CONNS": "25"}),
			want: config.Config{DatabaseURL: dsnTeste, Token: tokenTeste, Addr: "127.0.0.1:9000", MaxConns: 25},
		},
		{
			name: "max conns no limite inferior",
			env:  base(map[string]string{"BIBLIOTECA_DB_MAX_CONNS": "1"}),
			want: config.Config{DatabaseURL: dsnTeste, Token: tokenTeste, Addr: ":8080", MaxConns: 1},
		},
		{
			name: "max conns no limite superior",
			env:  base(map[string]string{"BIBLIOTECA_DB_MAX_CONNS": "100"}),
			want: config.Config{DatabaseURL: dsnTeste, Token: tokenTeste, Addr: ":8080", MaxConns: 100},
		},
		{
			name: "DATABASE_URL não é interpretada",
			env:  base(map[string]string{"DATABASE_URL": "qualquer coisa :: não é DSN"}),
			want: config.Config{DatabaseURL: "qualquer coisa :: não é DSN", Token: tokenTeste, Addr: ":8080", MaxConns: 10},
		},
		{
			name: "token com 32 bytes",
			env:  base(map[string]string{"BIBLIOTECA_API_TOKEN": strings.Repeat("a", 32)}),
			want: config.Config{DatabaseURL: dsnTeste, Token: strings.Repeat("a", 32), Addr: ":8080", MaxConns: 10},
		},
		{
			name: "token com 512 bytes",
			env:  base(map[string]string{"BIBLIOTECA_API_TOKEN": strings.Repeat("Z", 512)}),
			want: config.Config{DatabaseURL: dsnTeste, Token: strings.Repeat("Z", 512), Addr: ":8080", MaxConns: 10},
		},
		{
			name: "token com todos os imprimíveis de 0x21 a 0x7E",
			env:  base(map[string]string{"BIBLIOTECA_API_TOKEN": imprimiveis()}),
			want: config.Config{DatabaseURL: dsnTeste, Token: imprimiveis(), Addr: ":8080", MaxConns: 10},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Carregar(ambiente(tt.env))
			if err != nil {
				t.Fatalf("Carregar() erro inesperado: %v", err)
			}
			if got != tt.want {
				t.Errorf("Carregar() = %+v, want %+v", got, tt.want)
			}
		})
	}
}

func TestCarregarInvalido(t *testing.T) {
	const msgMaxConns = "config: BIBLIOTECA_DB_MAX_CONNS inválida"
	tokenCurto := "CURTO-SECRETO-" + strings.Repeat("x", 17) // 31 bytes
	tokenLongo := strings.Repeat("L", 513)

	tests := []struct {
		name string
		env  map[string]string
		// wantMsg, se não vazio, é a mensagem exata.
		wantMsg string
		// wantCita é o nome da variável que a mensagem tem que citar.
		wantCita string
		// segredos não podem aparecer na mensagem.
		segredos []string
	}{
		{name: "DATABASE_URL ausente", env: base(map[string]string{"DATABASE_URL": ""}), wantMsg: "config: DATABASE_URL ausente", segredos: []string{tokenTeste}},
		{name: "token ausente", env: base(map[string]string{"BIBLIOTECA_API_TOKEN": ""}), wantCita: "BIBLIOTECA_API_TOKEN", segredos: []string{dsnTeste, "SENHA-DSN"}},
		{name: "token com 31 bytes", env: base(map[string]string{"BIBLIOTECA_API_TOKEN": tokenCurto}), wantCita: "BIBLIOTECA_API_TOKEN", segredos: []string{tokenCurto, "CURTO-SECRETO", strconv.Itoa(len(tokenCurto)), "SENHA-DSN"}},
		{name: "token com 513 bytes", env: base(map[string]string{"BIBLIOTECA_API_TOKEN": tokenLongo}), wantCita: "BIBLIOTECA_API_TOKEN", segredos: []string{"LLLL", strconv.Itoa(len(tokenLongo))}},
		{name: "token com espaço no meio", env: base(map[string]string{"BIBLIOTECA_API_TOKEN": "TOKEN-SECRETO-0123456789 abcdefghijklm"}), wantCita: "BIBLIOTECA_API_TOKEN", segredos: []string{"TOKEN-SECRETO", "38"}},
		{name: "token com espaço à esquerda, sem trim", env: base(map[string]string{"BIBLIOTECA_API_TOKEN": " " + tokenTeste}), wantCita: "BIBLIOTECA_API_TOKEN", segredos: []string{"TOKEN-SECRETO", "39"}},
		{name: "token com espaço à direita, sem trim", env: base(map[string]string{"BIBLIOTECA_API_TOKEN": tokenTeste + " "}), wantCita: "BIBLIOTECA_API_TOKEN", segredos: []string{"TOKEN-SECRETO", "39"}},
		{name: "token com tab", env: base(map[string]string{"BIBLIOTECA_API_TOKEN": tokenTeste + "\t"}), wantCita: "BIBLIOTECA_API_TOKEN", segredos: []string{"TOKEN-SECRETO"}},
		{name: "token com quebra de linha", env: base(map[string]string{"BIBLIOTECA_API_TOKEN": tokenTeste + "\n"}), wantCita: "BIBLIOTECA_API_TOKEN", segredos: []string{"TOKEN-SECRETO"}},
		{name: "token com DEL 0x7F", env: base(map[string]string{"BIBLIOTECA_API_TOKEN": tokenTeste + "\x7f"}), wantCita: "BIBLIOTECA_API_TOKEN", segredos: []string{"TOKEN-SECRETO"}},
		{name: "token com NUL", env: base(map[string]string{"BIBLIOTECA_API_TOKEN": tokenTeste + "\x00"}), wantCita: "BIBLIOTECA_API_TOKEN", segredos: []string{"TOKEN-SECRETO"}},
		{name: "token com não ASCII", env: base(map[string]string{"BIBLIOTECA_API_TOKEN": tokenTeste + "é"}), wantCita: "BIBLIOTECA_API_TOKEN", segredos: []string{"TOKEN-SECRETO"}},
		{name: "token com byte alto inválido em UTF-8", env: base(map[string]string{"BIBLIOTECA_API_TOKEN": tokenTeste + "\xff"}), wantCita: "BIBLIOTECA_API_TOKEN", segredos: []string{"TOKEN-SECRETO"}},
		{name: "max conns zero", env: base(map[string]string{"BIBLIOTECA_DB_MAX_CONNS": "0"}), wantMsg: msgMaxConns},
		{name: "max conns 101", env: base(map[string]string{"BIBLIOTECA_DB_MAX_CONNS": "101"}), wantMsg: msgMaxConns, segredos: []string{"101"}},
		{name: "max conns negativo", env: base(map[string]string{"BIBLIOTECA_DB_MAX_CONNS": "-3"}), wantMsg: msgMaxConns, segredos: []string{"-3"}},
		{name: "max conns não numérico", env: base(map[string]string{"BIBLIOTECA_DB_MAX_CONNS": "VALOR-ECOADO"}), wantMsg: msgMaxConns, segredos: []string{"VALOR-ECOADO"}},
		{name: "max conns fracionário", env: base(map[string]string{"BIBLIOTECA_DB_MAX_CONNS": "1.5"}), wantMsg: msgMaxConns, segredos: []string{"1.5"}},
		{name: "max conns com espaço", env: base(map[string]string{"BIBLIOTECA_DB_MAX_CONNS": " 5"}), wantMsg: msgMaxConns},
		{name: "max conns com overflow", env: base(map[string]string{"BIBLIOTECA_DB_MAX_CONNS": "99999999999999999999"}), wantMsg: msgMaxConns, segredos: []string{"99999"}},
		{name: "max conns além de int32", env: base(map[string]string{"BIBLIOTECA_DB_MAX_CONNS": "4294967306"}), wantMsg: msgMaxConns, segredos: []string{"4294967306"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := config.Carregar(ambiente(tt.env))
			if err == nil {
				t.Fatalf("Carregar() = %+v, want erro", got)
			}
			if got != (config.Config{}) {
				t.Errorf("Carregar() em erro = %+v, want Config{}", got)
			}
			msg := err.Error()
			if tt.wantMsg != "" && msg != tt.wantMsg {
				t.Errorf("erro = %q, want %q", msg, tt.wantMsg)
			}
			if tt.wantCita != "" {
				if !strings.HasPrefix(msg, "config: ") {
					t.Errorf("erro = %q, want prefixo \"config: \"", msg)
				}
				if !strings.Contains(msg, tt.wantCita) {
					t.Errorf("erro = %q, want citar %s", msg, tt.wantCita)
				}
			}
			for _, s := range append(tt.segredos, dsnTeste, "SENHA-DSN", tokenTeste) {
				if strings.Contains(msg, s) {
					t.Errorf("erro %q ecoa %q", msg, s)
				}
			}
		})
	}
}

// As mensagens de erro do token são fixas: dentro da mesma regra violada, não
// variam com o valor recebido.
func TestCarregarTokenMensagemFixa(t *testing.T) {
	grupos := []struct {
		name string
		vals []string
	}{
		{name: "tamanho", vals: []string{"curto", strings.Repeat("a", 31), strings.Repeat("a", 513), strings.Repeat("b", 1000)}},
		{name: "charset", vals: []string{strings.Repeat("a", 40) + " ", strings.Repeat("b", 40) + "é", " " + strings.Repeat("c", 50), strings.Repeat("d", 35) + "\x7f"}},
	}
	for _, g := range grupos {
		t.Run(g.name, func(t *testing.T) {
			var primeira string
			for i, tok := range g.vals {
				_, err := config.Carregar(ambiente(base(map[string]string{"BIBLIOTECA_API_TOKEN": tok})))
				if err == nil {
					t.Fatalf("token de %d bytes aceito, want erro", len(tok))
				}
				if i == 0 {
					primeira = err.Error()
					continue
				}
				if err.Error() != primeira {
					t.Errorf("mensagem varia com o valor: %q != %q", err.Error(), primeira)
				}
			}
		})
	}
}
