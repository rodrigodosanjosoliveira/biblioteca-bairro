package migrate_test

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/migrate"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/migrations"
)

// Testes do segundo ciclo de correção da DEL-008 (achados M1, B1, S1, S2, S8).
// Todos exigem banco; sem BIBLIOTECA_TEST_DATABASE_URL são pulados.

func ident(partes ...string) string { return pgx.Identifier(partes).Sanitize() }

func upControle(t *testing.T) string {
	t.Helper()
	b, err := fs.ReadFile(migrations.Controle, "controle/0000_migracao_aplicada.up.sql")
	if err != nil {
		t.Fatalf("ler up do controle: %v", err)
	}
	return string(b)
}

// ---- M1: o controle adotado não pode carregar código nem dono alheio ----

// Contrato fixado (todos viram ErroControleInvalido, mensagem fixa, no Subir E
// no Status, antes de qualquer efeito): a tabela migracao_aplicada adotada tem
// de ter (a) dono que é o papel atual ou um papel de que ele é membro
// (pg_has_role(current_user, relowner, 'USAGE')); (b) sem triggers, regras,
// RLS, herança (filha ou mãe) nem partição; (c) como default apenas now() em
// aplicada_em e nenhuma coluna gerada ou de identidade.

// sentinela cria um schema à parte com a tabela alvo e as funções que um
// atacante poria no controle: se alguma rodar, alvo ganha uma linha.
type sentinela struct{ schema string }

func (s sentinela) alvo() string { return ident(s.schema, "alvo") }
func (s sentinela) fn(nome string) string {
	return ident(s.schema, nome)
}

func novaSentinela(t *testing.T, a *ambiente) sentinela {
	t.Helper()
	outro := novoBanco(t) // o DROP SCHEMA CASCADE vai no Cleanup
	s := sentinela{schema: outro.schema}
	for _, q := range []string{
		"CREATE TABLE " + s.alvo() + " (n integer)",
		`CREATE FUNCTION ` + s.fn("dispara") + `() RETURNS trigger LANGUAGE plpgsql AS $f$
			BEGIN INSERT INTO ` + s.alvo() + ` VALUES (1); RETURN NEW; END $f$`,
		`CREATE FUNCTION ` + s.fn("relogio") + `() RETURNS timestamptz LANGUAGE plpgsql AS $f$
			BEGIN INSERT INTO ` + s.alvo() + ` VALUES (2); RETURN now(); END $f$`,
	} {
		if _, err := a.obs.Exec(t.Context(), q); err != nil {
			t.Fatalf("preparar a sentinela: %v", err)
		}
	}
	return s
}

func (s sentinela) disparos(t *testing.T, a *ambiente) int {
	t.Helper()
	var n int
	if err := a.obs.QueryRow(t.Context(), "SELECT count(*) FROM "+s.alvo()).Scan(&n); err != nil {
		t.Fatalf("ler a sentinela: %v", err)
	}
	return n
}

type varianteControle struct {
	nome string
	// sql recebe a tabela (qualificada) e a sentinela e devolve os comandos que
	// transformam o controle correto na variante.
	sql func(tab string, s sentinela) []string
	// passaHoje marca as variantes que a implementação anterior já recusava
	// (guarda contra regressão, não prova o ajuste).
	passaHoje bool
}

func variantesControle() []varianteControle {
	coluna := func(col, valor string) func(string, sentinela) []string {
		return func(tab string, _ sentinela) []string {
			return []string{fmt.Sprintf("ALTER TABLE %s ALTER COLUMN %s SET DEFAULT %s", tab, col, valor)}
		}
	}
	return []varianteControle{
		{nome: "trigger", sql: func(tab string, s sentinela) []string {
			return []string{"CREATE TRIGGER t_sent BEFORE INSERT ON " + tab + " FOR EACH ROW EXECUTE FUNCTION " + s.fn("dispara") + "()"}
		}},
		{nome: "regra DO ALSO", sql: func(tab string, s sentinela) []string {
			return []string{"CREATE RULE r_sent AS ON INSERT TO " + tab + " DO ALSO INSERT INTO " + s.alvo() + " VALUES (3)"}
		}},
		{nome: "regra DO INSTEAD", sql: func(tab string, _ sentinela) []string {
			return []string{"CREATE RULE r_inst AS ON INSERT TO " + tab + " DO INSTEAD NOTHING"}
		}},
		{nome: "RLS com policy", sql: func(tab string, _ sentinela) []string {
			return []string{"ALTER TABLE " + tab + " ENABLE ROW LEVEL SECURITY", "CREATE POLICY p_sent ON " + tab + " USING (true)"}
		}},
		{nome: "RLS sem policy", sql: func(tab string, _ sentinela) []string {
			return []string{"ALTER TABLE " + tab + " ENABLE ROW LEVEL SECURITY"}
		}},
		{nome: "herda de outra tabela", sql: func(tab string, s sentinela) []string {
			return []string{
				"CREATE TABLE " + s.fn("mae") + " (versao integer, nome text, checksum text, aplicada_em timestamptz)",
				"ALTER TABLE " + tab + " INHERIT " + s.fn("mae"),
			}
		}},
		{nome: "tem tabela filha", sql: func(tab string, s sentinela) []string {
			return []string{"CREATE TABLE " + s.fn("filha") + " () INHERITS (" + tab + ")"}
		}},
		{nome: "default extra em versao", sql: coluna("versao", "7")},
		{nome: "default extra em nome", sql: coluna("nome", "'x'")},
		{nome: "default extra em checksum", sql: coluna("checksum", "'x'")},
		{nome: "aplicada_em com default diferente de now()", sql: coluna("aplicada_em", "clock_timestamp()")},
		{nome: "aplicada_em com default que chama funcao propria", sql: func(tab string, s sentinela) []string {
			return []string{"ALTER TABLE " + tab + " ALTER COLUMN aplicada_em SET DEFAULT " + s.fn("relogio") + "()"}
		}},
		{nome: "coluna gerada", sql: func(tab string, _ sentinela) []string {
			return []string{"ALTER TABLE " + tab + " DROP COLUMN checksum, ADD COLUMN checksum text GENERATED ALWAYS AS (repeat('a', 64)) STORED NOT NULL"}
		}},
		{nome: "coluna de identidade", sql: func(tab string, _ sentinela) []string {
			return []string{"ALTER TABLE " + tab + " ALTER COLUMN versao ADD GENERATED BY DEFAULT AS IDENTITY"}
		}},
		{nome: "particao de outra tabela", sql: func(tab string, s sentinela) []string {
			return []string{
				"DROP TABLE " + tab,
				"CREATE TABLE " + s.fn("particionada") + " (versao integer NOT NULL, nome text NOT NULL, checksum text NOT NULL, aplicada_em timestamptz NOT NULL) PARTITION BY RANGE (versao)",
				"CREATE TABLE " + tab + " PARTITION OF " + s.fn("particionada") +
					" (CONSTRAINT migracao_aplicada_pkey PRIMARY KEY (versao), aplicada_em DEFAULT now()) FOR VALUES FROM (1) TO (1000000)",
			}
		}},
		{nome: "tabela particionada", passaHoje: true, sql: func(tab string, _ sentinela) []string {
			return []string{
				"DROP TABLE " + tab,
				"CREATE TABLE " + tab + ` (versao integer NOT NULL, nome text NOT NULL, checksum text NOT NULL,
					aplicada_em timestamptz NOT NULL DEFAULT now(), CONSTRAINT migracao_aplicada_pkey PRIMARY KEY (versao)) PARTITION BY RANGE (versao)`,
			}
		}},
	}
}

func TestControleAdotadoComArmadilhaFalhaFechado(t *testing.T) {
	ms := []mig{{V: 1, Nome: "a", Up: "CREATE TABLE a (x integer);\n"}}
	for _, v := range variantesControle() {
		for _, modo := range []string{"Subir", "Status"} {
			t.Run(v.nome+"/"+modo, func(t *testing.T) {
				a := novoAmbiente(t)
				s := novaSentinela(t, a)
				tab := ident(a.b.schema, "migracao_aplicada")
				if _, err := a.obs.Exec(t.Context(), upControle(t)); err != nil {
					t.Fatalf("criar o controle a mão: %v", err)
				}
				for _, q := range v.sql(tab, s) {
					if _, err := a.obs.Exec(t.Context(), q); err != nil {
						t.Fatalf("montar a variante %q: %v\nSQL: %s", v.nome, err, q)
					}
				}
				antes := relacoes(t, a.obs, false)

				var err error
				if modo == "Subir" {
					_, err = a.exec(novoFS(ms...)).Subir(t.Context())
				} else {
					_, err = a.exec(novoFS(ms...)).Status(t.Context())
				}
				if !errors.Is(err, migrate.ErroControleInvalido) {
					// Segue adiante: as conferências abaixo mostram o dano.
					t.Errorf("%s() erro = %v, want ErroControleInvalido (controle com armadilha aceito)", modo, err)
				} else {
					semDetalhe(t, err)
					if got := err.Error(); got != migrate.ErroControleInvalido.Error() {
						t.Errorf("erro = %q, want a mensagem fixa %q (sem eco do que foi achado)", got, migrate.ErroControleInvalido.Error())
					}
				}
				if rs := registro(t, a.obs); len(rs) != 0 {
					t.Errorf("registro = %v, want vazio", versoes(rs))
				}
				if depois := relacoes(t, a.obs, false); !igual(antes, depois) {
					t.Errorf("relações mudaram: %v -> %v (houve efeito antes de recusar)", antes, depois)
				}
				if n := s.disparos(t, a); n != 0 {
					t.Errorf("a sentinela disparou %d vez(es): código da tabela maliciosa executou", n)
				}
				ocioso(t, a.conn)
			})
		}
	}
}

// Guarda: o controle criado pelo up embutido e o pré-criado correto (adoção)
// continuam passando, no Subir e no Status.
func TestControleCorretoContinuaPassando(t *testing.T) {
	ms := []mig{{V: 1, Nome: "a", Up: "CREATE TABLE a (x integer);\n"}}
	t.Run("criado pelo proprio Subir", func(t *testing.T) {
		a := novoAmbiente(t)
		got, err := a.exec(novoFS(ms...)).Subir(t.Context())
		if err != nil || len(got) != 1 {
			t.Fatalf("Subir() = %+v, %v, want 1 migração", got, err)
		}
		if sit, err := a.exec(novoFS(ms...)).Status(t.Context()); err != nil || len(sit) != 1 || sit[0].Estado != migrate.EstadoAplicada {
			t.Errorf("Status() = %+v, %v, want 1 aplicada", sit, err)
		}
	})
	t.Run("pre-criado correto", func(t *testing.T) {
		a := novoAmbiente(t)
		if _, err := a.obs.Exec(t.Context(), upControle(t)); err != nil {
			t.Fatalf("criar o controle a mão: %v", err)
		}
		if sit, err := a.exec(novoFS(ms...)).Status(t.Context()); err != nil || len(sit) != 1 || sit[0].Estado != migrate.EstadoPendente {
			t.Fatalf("Status() = %+v, %v, want 1 pendente", sit, err)
		}
		got, err := a.exec(novoFS(ms...)).Subir(t.Context())
		if err != nil || len(got) != 1 {
			t.Fatalf("Subir() = %+v, %v, want 1 migração", got, err)
		}
	})
}

// papel cria um papel LOGIN sem privilégios especiais, com uso e criação no
// schema do teste, e o remove no Cleanup (DROP OWNED BY antes do DROP ROLE),
// mesmo com o teste em falha.
func novoPapel(t *testing.T, a *ambiente, prefixo string) string {
	t.Helper()
	var b [6]byte
	if _, err := rand.Read(b[:]); err != nil {
		t.Fatalf("gerar nome do papel: %v", err)
	}
	nome := prefixo + "_" + hex.EncodeToString(b[:])
	id := ident(nome)
	if _, err := a.obs.Exec(t.Context(), "CREATE ROLE "+id+" LOGIN"); err != nil {
		t.Fatalf("CREATE ROLE %s: %v", nome, err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
		defer cancel()
		if _, err := a.obs.Exec(ctx, "DROP OWNED BY "+id); err != nil {
			t.Errorf("DROP OWNED BY %s: %v", nome, err)
		}
		if _, err := a.obs.Exec(ctx, "DROP ROLE IF EXISTS "+id); err != nil {
			t.Errorf("DROP ROLE %s: %v", nome, err)
		}
	})
	if _, err := a.obs.Exec(t.Context(), "GRANT USAGE, CREATE ON SCHEMA "+ident(a.b.schema)+" TO "+id); err != nil {
		t.Fatalf("GRANT no schema para %s: %v", nome, err)
	}
	return nome
}

// Para superuser pg_has_role é sempre verdadeiro: o teste do dono precisa
// criar papéis e conectar o executor como papel não superuser.
func exigirPoderCriarPapeis(t *testing.T, a *ambiente) {
	t.Helper()
	var ok bool
	if err := a.obs.QueryRow(t.Context(), "SELECT rolsuper OR rolcreaterole FROM pg_roles WHERE rolname = current_user").Scan(&ok); err != nil {
		t.Fatalf("consultar pg_roles: %v", err)
	}
	if !ok {
		const msg = "o usuário do banco de teste não pode criar papéis (precisa de superuser ou CREATEROLE)"
		if os.Getenv(envBancoObrigatorio) == "1" {
			t.Fatal(msg)
		}
		t.Skip(msg)
	}
}

// O dono da tabela adotada tem de ser o papel atual ou um papel de que ele é
// membro. Um papel que só tem CREATE no schema pré-cria a tabela com a
// estrutura certa e dá os privilégios ao papel de DDL: o executor, rodando
// como "ddl", executaria os efeitos que o dono (o atacante) puder armar nela.
func TestControleAdotadoDeOutroDono(t *testing.T) {
	tests := []struct {
		name    string
		dono    string // "atacante" ou "ddl"
		membro  bool   // GRANT atacante TO ddl
		aceitar bool
	}{
		{name: "dono atacante, ddl nao e membro", dono: "atacante", aceitar: false},
		{name: "dono atacante, ddl e membro", dono: "atacante", membro: true, aceitar: true},
		{name: "dono e o proprio ddl", dono: "ddl", aceitar: true},
	}
	ms := []mig{{V: 1, Nome: "a", Up: "CREATE TABLE a (x integer);\n"}}
	for _, modo := range []string{"Subir", "Status"} {
		for _, tt := range tests {
			t.Run(modo+"/"+tt.name, func(t *testing.T) {
				a := novoAmbiente(t)
				exigirPoderCriarPapeis(t, a)
				ddl, atq := novoPapel(t, a, "ddl"), novoPapel(t, a, "atq")
				tab := ident(a.b.schema, "migracao_aplicada")
				if _, err := a.obs.Exec(t.Context(), upControle(t)); err != nil {
					t.Fatalf("criar o controle a mão: %v", err)
				}
				dono := atq
				if tt.dono == "ddl" {
					dono = ddl
				}
				for _, q := range []string{
					"ALTER TABLE " + tab + " OWNER TO " + ident(dono),
					"GRANT ALL ON " + tab + " TO " + ident(ddl), // o atacante daria acesso ao papel de DDL
				} {
					if _, err := a.obs.Exec(t.Context(), q); err != nil {
						t.Fatalf("preparar o dono: %v", err)
					}
				}
				if tt.membro {
					if _, err := a.obs.Exec(t.Context(), "GRANT "+ident(atq)+" TO "+ident(ddl)); err != nil {
						t.Fatalf("GRANT de papel: %v", err)
					}
				}
				// O executor conecta como o papel de DDL, que não é superuser.
				cfg := a.b.config(t)
				cfg.User = ddl
				conn := a.b.conectarCfg(t, cfg)
				var su bool
				if err := conn.QueryRow(t.Context(), "SELECT rolsuper FROM pg_roles WHERE rolname = current_user").Scan(&su); err != nil || su {
					t.Fatalf("pré-condição: o papel de DDL é superuser (%v, err=%v)", su, err)
				}
				antes := relacoes(t, a.obs, false)

				var err error
				if modo == "Subir" {
					_, err = a.execEm(conn, novoFS(ms...)).Subir(t.Context())
				} else {
					_, err = a.execEm(conn, novoFS(ms...)).Status(t.Context())
				}
				if tt.aceitar {
					if err != nil {
						t.Fatalf("%s() erro = %v, want aceitar (o dono é o papel atual ou um papel de que ele é membro)", modo, err)
					}
					return
				}
				if !errors.Is(err, migrate.ErroControleInvalido) {
					t.Fatalf("%s() erro = %v, want ErroControleInvalido (dono alheio aceito)", modo, err)
				}
				semDetalhe(t, err)
				if rs := registro(t, a.obs); len(rs) != 0 {
					t.Errorf("registro = %v, want vazio", versoes(rs))
				}
				if depois := relacoes(t, a.obs, false); !igual(antes, depois) {
					t.Errorf("relações mudaram: %v -> %v", antes, depois)
				}
			})
		}
	}
}

// ---- B1: o registro vai para a tabela inspecionada, nunca para outra ----

// Contrato aceito: um arquivo que redireciona a resolução de migracao_aplicada
// (SET search_path, tabela TEMP de mesmo nome) NÃO pode fazer o registro cair
// no lugar errado. As duas correções valem: (a) INSERT e SELECT qualificados
// com o schema inspecionado, e o registro da versão vai para a tabela real; ou
// (b) o executor recusa com erro fixo e desfaz a transação do arquivo (a
// versão não é registrada e não sobra efeito parcial). Em nenhum caso a tabela
// falsa recebe linha.
func TestRegistroNaoVaiParaOLugarErrado(t *testing.T) {
	falsa := `(versao integer NOT NULL PRIMARY KEY, nome text NOT NULL, checksum text NOT NULL, aplicada_em timestamptz NOT NULL DEFAULT now())`
	tests := []struct {
		name string
		// up2 devolve o SQL do arquivo 0002, dado o schema "outro" que já
		// contém uma migracao_aplicada falsa e vazia.
		up2 func(outro string) string
		// falsaRecebeu devolve quantas linhas a tabela falsa tem.
		falsaRecebeu func(t *testing.T, a *ambiente, outro string) int
		// vestigios lista, depois de um Subir recusado, as relações que NÃO
		// podem existir (efeito parcial do arquivo desfeito).
		vestigios func(a *ambiente, outro string) []string
	}{
		{
			name: "SET search_path para outro schema",
			up2: func(outro string) string {
				return "SET search_path TO " + ident(outro) + ";\nCREATE TABLE b (x integer);\n"
			},
			falsaRecebeu: func(t *testing.T, a *ambiente, outro string) int {
				var n int
				if err := a.obs.QueryRow(t.Context(), "SELECT count(*) FROM "+ident(outro, "migracao_aplicada")).Scan(&n); err != nil {
					t.Fatalf("contar a tabela falsa: %v", err)
				}
				return n
			},
			vestigios: func(a *ambiente, outro string) []string {
				return []string{ident(outro, "b"), ident(a.b.schema, "b")}
			},
		},
		{
			name: "CREATE TEMP TABLE migracao_aplicada",
			up2: func(string) string {
				return "CREATE TEMP TABLE migracao_aplicada (versao integer, nome text, checksum text, aplicada_em timestamptz DEFAULT now());\nCREATE TABLE b (x integer);\n"
			},
			falsaRecebeu: func(t *testing.T, a *ambiente, _ string) int {
				// A temporária só existe na sessão do executor (a.conn).
				var existe bool
				if err := a.conn.QueryRow(t.Context(), "SELECT to_regclass('pg_temp.migracao_aplicada') IS NOT NULL").Scan(&existe); err != nil {
					t.Fatalf("procurar a temporária: %v", err)
				}
				if !existe {
					return 0
				}
				var n int
				if err := a.conn.QueryRow(t.Context(), "SELECT count(*) FROM pg_temp.migracao_aplicada").Scan(&n); err != nil {
					t.Fatalf("contar a temporária: %v", err)
				}
				return n
			},
			vestigios: func(a *ambiente, _ string) []string { return []string{ident(a.b.schema, "b")} },
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			a := novoAmbiente(t)
			outro := novoBanco(t).schema
			if _, err := a.obs.Exec(t.Context(), "CREATE TABLE "+ident(outro, "migracao_aplicada")+" "+falsa); err != nil {
				t.Fatalf("criar a tabela falsa: %v", err)
			}
			v1 := mig{V: 1, Nome: "a", Up: "CREATE TABLE a (x integer);\n"}
			if _, err := a.exec(novoFS(v1)).Subir(t.Context()); err != nil {
				t.Fatalf("Subir() da 0001: %v", err)
			}

			got, err := a.exec(novoFS(v1, mig{V: 2, Nome: "redireciona", Up: tt.up2(outro)})).Subir(t.Context())

			if n := tt.falsaRecebeu(t, a, outro); n != 0 {
				t.Errorf("a tabela falsa recebeu %d linha(s): o registro foi para o lugar errado", n)
			}
			vs := versoes(registro(t, a.obs))
			if err == nil {
				// (a) qualificou: a versão 2 está na tabela real.
				if len(got) != 1 || got[0].Versao != 2 {
					t.Errorf("Subir() = %+v, want só a versão 2", got)
				}
				if fmt.Sprint(vs) != "[1 2]" {
					t.Errorf("registro real = %v, want [1 2]", vs)
				}
				return
			}
			// (b) recusou: erro fixo, rollback, sem efeito parcial.
			semDetalhe(t, err)
			if fmt.Sprint(vs) != "[1]" {
				t.Errorf("registro real = %v, want [1] (arquivo recusado não registra)", vs)
			}
			for _, rel := range tt.vestigios(a, outro) {
				var existe bool
				if err := a.obs.QueryRow(t.Context(), "SELECT to_regclass($1) IS NOT NULL", rel).Scan(&existe); err != nil {
					t.Fatalf("to_regclass(%s): %v", rel, err)
				}
				if existe {
					t.Errorf("sobrou %s depois do arquivo recusado: efeito parcial", rel)
				}
			}
			ocioso(t, a.conn)
		})
	}
}

// ---- S1: o COMMENT de aplicada_em diz o que o default grava ----

// DEFAULT now() grava o início da transação que aplicou a migração, não o
// instante do registro.
func TestComentarioDeAplicadaEmDizInicioDaTransacao(t *testing.T) {
	a := novoAmbiente(t)
	if _, err := a.obs.Exec(t.Context(), upControle(t)); err != nil {
		t.Fatalf("criar o controle: %v", err)
	}
	var coment string
	err := a.obs.QueryRow(t.Context(), `SELECT coalesce(col_description(c.oid, a.attnum), '')
		  FROM pg_class c JOIN pg_attribute a ON a.attrelid = c.oid AND a.attname = 'aplicada_em'
		 WHERE c.oid = to_regclass(quote_ident(current_schema()) || '.migracao_aplicada')`).Scan(&coment)
	if err != nil {
		t.Fatalf("ler o comentário de aplicada_em: %v", err)
	}
	norm := normalizarTexto(coment)
	if !strings.Contains(norm, "inicio da transacao") {
		t.Errorf("COMMENT de aplicada_em = %q, want mencionar o início da transação", coment)
	}
	if strings.Contains(norm, "registrada") {
		t.Errorf("COMMENT de aplicada_em = %q, não pode dizer que é o instante em que foi 'registrada'", coment)
	}

	// Os comentários do controle seguem sem as palavras de controle de transação.
	proibidas := regexp.MustCompile(`(?i)\b(begin|commit|rollback|savepoint|concurrently)\b`)
	rows, err := a.obs.Query(t.Context(), `SELECT coalesce(obj_description(oid, 'pg_class'), '') FROM pg_class
		 WHERE oid = to_regclass(quote_ident(current_schema()) || '.migracao_aplicada')
		UNION ALL SELECT coalesce(col_description(a.attrelid, a.attnum), '') FROM pg_attribute a
		 WHERE a.attrelid = to_regclass(quote_ident(current_schema()) || '.migracao_aplicada') AND a.attnum > 0`)
	if err != nil {
		t.Fatalf("ler os comentários: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var c string
		if err := rows.Scan(&c); err != nil {
			t.Fatalf("ler comentário: %v", err)
		}
		if m := proibidas.FindString(c); m != "" {
			t.Errorf("comentário %q usa a palavra %q", c, m)
		}
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar comentários: %v", err)
	}
}

// normalizarTexto põe em minúsculas, tira acentos comuns do português e troca
// qualquer pontuação por um espaço, para o teste tolerar a grafia da frase.
func normalizarTexto(s string) string {
	s = strings.ToLower(s)
	s = strings.NewReplacer("á", "a", "à", "a", "â", "a", "ã", "a", "é", "e", "ê", "e", "í", "i", "ó", "o", "ô", "o", "õ", "o", "ú", "u", "ç", "c").Replace(s)
	s = regexp.MustCompile(`[^a-z0-9]+`).ReplaceAllString(s, " ")
	return strings.TrimSpace(s)
}

// ---- S2: statement_timeout vale por comando, não pelo arquivo ----

// Documenta e confirma, no PostgreSQL do teste, o comportamento que a doc de
// Opcoes e de config passa a descrever: num arquivo de vários comandos o
// statement_timeout vale para cada comando, não para o arquivo inteiro. Dois
// pg_sleep de 0,7 s cabem, cada um, em 1 s; a soma (1,4 s) não precisa caber.
func TestStatementTimeoutVale_PorComandoNaoPorArquivo(t *testing.T) {
	t.Run("dois comandos cuja soma passa do prazo concluem", func(t *testing.T) {
		a := novoAmbiente(t)
		ms := []mig{{V: 1, Nome: "dois", Up: "SELECT pg_sleep(0.7);\nSELECT pg_sleep(0.7);\n"}}
		inicio := time.Now()
		got, err := a.exec(novoFS(ms...), func(o *migrate.Opcoes) { o.StatementTimeout = time.Second }).Subir(t.Context())
		if err != nil || len(got) != 1 {
			t.Fatalf("Subir() = %+v, %v, want concluir sem erro (cada comando cabe em 1s)", got, err)
		}
		if d := time.Since(inicio); d < 1300*time.Millisecond {
			t.Errorf("Subir levou %v, want ≥ 1,4 s (a soma dos dois pg_sleep): os comandos rodaram mesmo?", d)
		}
		if vs := versoes(registro(t, a.obs)); fmt.Sprint(vs) != "[1]" {
			t.Errorf("registro = %v, want [1]", vs)
		}
	})
	t.Run("um comando mais longo que o prazo falha com 57014 e desfaz", func(t *testing.T) {
		a := novoAmbiente(t)
		ms := []mig{
			{V: 1, Nome: "a", Up: "CREATE TABLE a (x integer);\n"},
			{V: 2, Nome: "lento", Up: "CREATE TABLE b (x integer);\nSELECT pg_sleep(1.5);\n"},
		}
		got, err := a.exec(novoFS(ms...), func(o *migrate.Opcoes) { o.StatementTimeout = time.Second }).Subir(t.Context())
		if !errors.Is(err, migrate.ErroSQL) {
			t.Fatalf("erro = %v, want ErroSQL", err)
		}
		semDetalhe(t, err)
		if me := erroDe(t, err); me.Code != "57014" || me.Arquivo != "0002_lento.up.sql" {
			t.Errorf("erro = {Arquivo:%q Code:%q}, want {0002_lento.up.sql 57014}", me.Arquivo, me.Code)
		}
		if len(got) != 1 || got[0].Versao != 1 {
			t.Errorf("Subir() = %+v, want só a versão 1", got)
		}
		if vs := versoes(registro(t, a.obs)); fmt.Sprint(vs) != "[1]" {
			t.Errorf("registro = %v, want [1]", vs)
		}
		if r := relacoes(t, a.obs, true); !igual(r, []string{"a"}) {
			t.Errorf("relações = %v, want só a tabela da 0001 (a transação da 0002 foi desfeita)", r)
		}
	})
}

// ---- S8: a inspeção do Status é um instantâneo ----

// O BEGIN do Status pede REPEATABLE READ: as consultas da inspeção
// (existência do controle, estrutura, registro, objetos) enxergam o mesmo
// instante, e um Subir concorrente não produz estado misto.
func TestStatusAbreTransacaoRepeatableReadSomenteLeitura(t *testing.T) {
	a, tr := novoAmbienteTracado(t)
	tr.limpar()
	if _, err := a.exec(novoFS(tresMigracoes()...)).Status(t.Context()); err != nil {
		t.Fatalf("Status() erro = %v", err)
	}
	sqls, _ := tr.copia()
	for _, s := range sqls {
		l := strings.ToLower(strings.Join(strings.Fields(s), " "))
		if !strings.HasPrefix(l, "begin") {
			continue
		}
		if !strings.Contains(l, "isolation level repeatable read") || !strings.Contains(l, "read only") {
			t.Errorf("BEGIN do Status = %q, want READ ONLY ISOLATION LEVEL REPEATABLE READ", s)
		}
		return
	}
	t.Errorf("Status não abriu transação; SQL enviado: %q", sqls)
}

// Um Subir completo (cria o controle e aplica a 0001) roda enquanto o Status
// está parado entre o primeiro comando da inspeção e o seguinte. Em READ
// COMMITTED o Status via "sem controle" e depois "objetos sem registro", e
// falhava com ErroBancoSemRegistro; com um instantâneo vê o estado anterior
// (tudo pendente) ou o posterior (tudo aplicado), nunca um estado misto.
//
// O proxy segura a primeira mensagem do Status que cita pg_class (a
// verificação de objetos do schema, que vem logo depois de "o controle não
// existe") até o Subir terminar.
func TestStatusNaoVeEstadoMistoComSubirConcorrente(t *testing.T) {
	a := novoAmbiente(t)
	ms := tresMigracoes()
	fsys := novoFS(ms...)

	px, cfgProxy := novoProxyPausa(t, a.b.config(t), "pg_class")
	conn := a.b.conectarCfg(t, cfgProxy)

	type res struct {
		sit []migrate.Situacao
		err error
	}
	fim := make(chan res, 1)
	go func() {
		sit, err := a.execEm(conn, fsys).Status(context.Background())
		fim <- res{sit, err}
	}()

	select {
	case <-px.chegou:
	case r := <-fim:
		t.Fatalf("pré-condição: o Status terminou sem passar pela consulta de objetos (%+v, %v)", r.sit, r.err)
	case <-time.After(15 * time.Second):
		t.Fatal("o Status não chegou à consulta de objetos em 15 s")
	}

	// Com o Status parado, outra conexão aplica tudo.
	got, err := a.exec(fsys).Subir(t.Context())
	if err != nil || len(got) != len(ms) {
		close(px.liberar)
		t.Fatalf("Subir() concorrente = %+v, %v, want %d migrações", got, err, len(ms))
	}
	close(px.liberar)

	var r res
	select {
	case r = <-fim:
	case <-time.After(15 * time.Second):
		t.Fatal("o Status não terminou depois de liberado")
	}
	if r.err != nil {
		t.Fatalf("Status() concorrente com Subir: erro = %v, want estado coerente (anterior ou posterior)", r.err)
	}
	if len(r.sit) != len(ms) {
		t.Fatalf("Status() = %+v, want %d situações", r.sit, len(ms))
	}
	primeiro := r.sit[0].Estado
	if primeiro != migrate.EstadoPendente && primeiro != migrate.EstadoAplicada {
		t.Fatalf("Status()[0] = %q, want pendente ou aplicada", primeiro)
	}
	for _, s := range r.sit {
		if s.Estado != primeiro {
			t.Errorf("Status() misto: %+v (todas deviam ser %s)", r.sit, primeiro)
			break
		}
	}
}
