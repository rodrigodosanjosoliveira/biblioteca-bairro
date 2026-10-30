package migrate_test

import (
	"io/fs"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/migrations"
)

// Testes de banco do DDL da tabela de controle (data-model, DEL-008). Eles
// aplicam o SQL embutido em migrations.Controle direto, sem o executor, num
// schema vazio.
//
// Armadilha: checarCatalogo/contarObjetos de internal/storage/esquema_test.go
// falham num schema que tenha migracao_aplicada; por isso este arquivo tem as
// suas próprias checagens, sem mexer nas da 0001.

const (
	upControleBD   = "controle/0000_migracao_aplicada.up.sql"
	downControleBD = "controle/0000_migracao_aplicada.down.sql"
)

func sqlControle(t *testing.T, nome string) string {
	t.Helper()
	b, err := fs.ReadFile(migrations.Controle, nome)
	if err != nil {
		t.Fatalf("ler %s: %v", nome, err)
	}
	return string(b)
}

func aplicarControle(t *testing.T, c *pgx.Conn, nome string) error {
	t.Helper()
	_, err := c.Exec(t.Context(), sqlControle(t, nome))
	return err
}

func contarRelacoes(t *testing.T, c *pgx.Conn, schema string) int {
	t.Helper()
	var n int
	err := c.QueryRow(t.Context(), `
		SELECT count(*) FROM pg_class cl JOIN pg_namespace n ON n.oid = cl.relnamespace
		 WHERE n.nspname = $1`, schema).Scan(&n)
	if err != nil {
		t.Fatalf("contar objetos: %v", err)
	}
	return n
}

// novoControle cria o schema e aplica o up do controle.
func novoControle(t *testing.T) (*bancoTeste, *pgx.Conn) {
	t.Helper()
	b := novoBanco(t)
	c := b.conectar(t)
	if err := aplicarControle(t, c, upControleBD); err != nil {
		t.Fatalf("aplicar up do controle: %v", err)
	}
	return b, c
}

var constraintsControle = map[string]string{
	"migracao_aplicada_pkey":              "p",
	"migracao_aplicada_versao_check":      "c",
	"migracao_aplicada_nome_check":        "c",
	"migracao_aplicada_checksum_check":    "c",
	"migracao_aplicada_aplicada_em_check": "c",
}

// catalogoControle devolve uma descrição textual estável do catálogo do
// schema, para comparar antes e depois.
func catalogoControle(t *testing.T, c *pgx.Conn, schema string) string {
	t.Helper()
	var linhas []string
	rows, err := c.Query(t.Context(), `
		SELECT 'con ' || con.conname || ' ' || con.contype::text || ' ' || pg_get_constraintdef(con.oid)
		  FROM pg_constraint con JOIN pg_namespace n ON n.oid = con.connamespace
		 WHERE n.nspname = $1 AND con.contype <> 'n'
		UNION ALL
		SELECT 'idx ' || indexdef FROM pg_indexes WHERE schemaname = $1
		UNION ALL
		SELECT 'col ' || column_name || ' ' || data_type || ' ' || is_nullable || ' ' || COALESCE(column_default, '')
		  FROM information_schema.columns WHERE table_schema = $1`, schema)
	if err != nil {
		t.Fatalf("consultar catálogo: %v", err)
	}
	defer rows.Close()
	for rows.Next() {
		var l string
		if err := rows.Scan(&l); err != nil {
			t.Fatalf("ler catálogo: %v", err)
		}
		linhas = append(linhas, l)
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar catálogo: %v", err)
	}
	sort.Strings(linhas)
	return strings.Join(linhas, "\n")
}

func TestControleCatalogo(t *testing.T) {
	b, c := novoControle(t)
	ctx := t.Context()

	// Constraints (sem as NOT NULL que o PostgreSQL 18 também registra).
	rows, err := c.Query(ctx, `
		SELECT con.conname, con.contype::text, pg_get_constraintdef(con.oid), con.conrelid::regclass::text
		  FROM pg_constraint con JOIN pg_namespace n ON n.oid = con.connamespace
		 WHERE n.nspname = $1 AND con.contype <> 'n'`, b.schema)
	if err != nil {
		t.Fatalf("consultar pg_constraint: %v", err)
	}
	type con struct{ tipo, def, tabela string }
	got := map[string]con{}
	for rows.Next() {
		var nome string
		var k con
		if err := rows.Scan(&nome, &k.tipo, &k.def, &k.tabela); err != nil {
			t.Fatalf("ler pg_constraint: %v", err)
		}
		got[nome] = k
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar pg_constraint: %v", err)
	}
	for nome, tipo := range constraintsControle {
		k, ok := got[nome]
		if !ok {
			t.Errorf("constraint %s ausente", nome)
			continue
		}
		if k.tipo != tipo {
			t.Errorf("constraint %s tipo = %s, want %s", nome, k.tipo, tipo)
		}
		if k.tabela != "migracao_aplicada" {
			t.Errorf("constraint %s em %q, want migracao_aplicada", nome, k.tabela)
		}
	}
	for nome := range got {
		if _, ok := constraintsControle[nome]; !ok {
			t.Errorf("constraint inesperada %s (%+v)", nome, got[nome])
		}
	}
	for nome, trechos := range map[string][]string{
		"migracao_aplicada_pkey":              {"PRIMARY KEY (versao)"},
		"migracao_aplicada_versao_check":      {"versao > 0"},
		"migracao_aplicada_nome_check":        {"nome ~ '^[a-z0-9_]+$'"},
		"migracao_aplicada_checksum_check":    {"checksum ~ '^[0-9a-f]{64}$'"},
		"migracao_aplicada_aplicada_em_check": {"isfinite(aplicada_em)"},
	} {
		for _, tr := range trechos {
			if !strings.Contains(got[nome].def, tr) {
				t.Errorf("%s = %q, want conter %q", nome, got[nome].def, tr)
			}
		}
	}

	// Só o índice da PK.
	var idx []string
	rows, err = c.Query(ctx, `SELECT indexname FROM pg_indexes WHERE schemaname = $1`, b.schema)
	if err != nil {
		t.Fatalf("consultar pg_indexes: %v", err)
	}
	for rows.Next() {
		var n string
		if err := rows.Scan(&n); err != nil {
			t.Fatalf("ler pg_indexes: %v", err)
		}
		idx = append(idx, n)
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar pg_indexes: %v", err)
	}
	if len(idx) != 1 || idx[0] != "migracao_aplicada_pkey" {
		t.Errorf("índices = %v, want só migracao_aplicada_pkey", idx)
	}

	// Colunas.
	type coluna struct{ tipo, nula, padrao string }
	want := map[string]coluna{
		"versao":      {"integer", "NO", ""},
		"nome":        {"text", "NO", ""},
		"checksum":    {"text", "NO", ""},
		"aplicada_em": {"timestamp with time zone", "NO", "now()"},
	}
	rows, err = c.Query(ctx, `
		SELECT table_name, column_name, data_type, is_nullable, COALESCE(column_default, '')
		  FROM information_schema.columns WHERE table_schema = $1`, b.schema)
	if err != nil {
		t.Fatalf("consultar colunas: %v", err)
	}
	cols := map[string]coluna{}
	for rows.Next() {
		var tab, nome string
		var k coluna
		if err := rows.Scan(&tab, &nome, &k.tipo, &k.nula, &k.padrao); err != nil {
			t.Fatalf("ler colunas: %v", err)
		}
		if tab != "migracao_aplicada" {
			t.Errorf("coluna %s.%s em tabela inesperada", tab, nome)
		}
		cols[nome] = k
	}
	rows.Close()
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar colunas: %v", err)
	}
	for nome, w := range want {
		if g, ok := cols[nome]; !ok {
			t.Errorf("coluna %s ausente", nome)
		} else if g != w {
			t.Errorf("coluna %s = %+v, want %+v", nome, g, w)
		}
	}
	for nome := range cols {
		if _, ok := want[nome]; !ok {
			t.Errorf("coluna inesperada %s", nome)
		}
	}

	// Só a tabela e o índice da PK: sem sequência, view ou outra relação.
	if n := contarRelacoes(t, c, b.schema); n != 2 {
		t.Errorf("relações no schema = %d, want 2 (tabela e índice da PK)", n)
	}

	// O up não faz GRANT: relacl fica NULL.
	var semAcl bool
	if err := c.QueryRow(ctx, `
		SELECT relacl IS NULL FROM pg_class
		 WHERE oid = to_regclass(quote_ident($1) || '.migracao_aplicada')`, b.schema).Scan(&semAcl); err != nil {
		t.Fatalf("ler relacl: %v", err)
	}
	if !semAcl {
		t.Error("relacl não é NULL: o up faz GRANT/REVOKE")
	}

	// A tabela tem comentário.
	var com *string
	if err := c.QueryRow(ctx, `
		SELECT obj_description(to_regclass(quote_ident($1) || '.migracao_aplicada'), 'pg_class')`, b.schema).Scan(&com); err != nil {
		t.Fatalf("ler comentário: %v", err)
	}
	if com == nil || strings.TrimSpace(*com) == "" {
		t.Error("tabela migracao_aplicada sem COMMENT")
	}
}

func TestControleRejeitaLinhasInvalidas(t *testing.T) {
	_, c := novoControle(t)
	ctx := t.Context()
	ok := strings.Repeat("a", 64)

	tests := []struct {
		name       string
		versao     any
		nome       any
		checksum   any
		code       string
		constraint string
		coluna     string
	}{
		{name: "versao zero", versao: 0, nome: "a", checksum: ok, code: "23514", constraint: "migracao_aplicada_versao_check"},
		{name: "versao negativa", versao: -1, nome: "a", checksum: ok, code: "23514", constraint: "migracao_aplicada_versao_check"},
		{name: "versao minima do integer", versao: -2147483648, nome: "a", checksum: ok, code: "23514", constraint: "migracao_aplicada_versao_check"},

		{name: "checksum com 63", versao: 1, nome: "a", checksum: strings.Repeat("a", 63), code: "23514", constraint: "migracao_aplicada_checksum_check"},
		{name: "checksum com 65", versao: 1, nome: "a", checksum: strings.Repeat("a", 65), code: "23514", constraint: "migracao_aplicada_checksum_check"},
		{name: "checksum em maiuscula", versao: 1, nome: "a", checksum: strings.Repeat("A", 64), code: "23514", constraint: "migracao_aplicada_checksum_check"},
		{name: "checksum nao hex", versao: 1, nome: "a", checksum: strings.Repeat("g", 64), code: "23514", constraint: "migracao_aplicada_checksum_check"},
		{name: "checksum vazio", versao: 1, nome: "a", checksum: "", code: "23514", constraint: "migracao_aplicada_checksum_check"},
		{name: "checksum com quebra de linha no fim", versao: 1, nome: "a", checksum: ok + "\n", code: "23514", constraint: "migracao_aplicada_checksum_check"},
		{name: "checksum com espaco no inicio", versao: 1, nome: "a", checksum: " " + strings.Repeat("a", 63), code: "23514", constraint: "migracao_aplicada_checksum_check"},
		{name: "checksum com espaco no fim", versao: 1, nome: "a", checksum: strings.Repeat("a", 63) + " ", code: "23514", constraint: "migracao_aplicada_checksum_check"},
		{name: "checksum com prefixo 0x", versao: 1, nome: "a", checksum: "0x" + strings.Repeat("a", 62), code: "23514", constraint: "migracao_aplicada_checksum_check"},

		{name: "nome vazio", versao: 1, nome: "", checksum: ok, code: "23514", constraint: "migracao_aplicada_nome_check"},
		{name: "nome so espacos", versao: 1, nome: "   ", checksum: ok, code: "23514", constraint: "migracao_aplicada_nome_check"},
		{name: "nome em maiuscula", versao: 1, nome: "A", checksum: ok, code: "23514", constraint: "migracao_aplicada_nome_check"},
		{name: "nome com hifen", versao: 1, nome: "a-b", checksum: ok, code: "23514", constraint: "migracao_aplicada_nome_check"},
		{name: "nome com acento", versao: 1, nome: "ação", checksum: ok, code: "23514", constraint: "migracao_aplicada_nome_check"},
		{name: "nome com quebra de linha no fim", versao: 1, nome: "abc\n", checksum: ok, code: "23514", constraint: "migracao_aplicada_nome_check"},
		{name: "nome com espaco no meio", versao: 1, nome: "a b", checksum: ok, code: "23514", constraint: "migracao_aplicada_nome_check"},
		{name: "nome com ponto", versao: 1, nome: "a.up", checksum: ok, code: "23514", constraint: "migracao_aplicada_nome_check"},

		{name: "versao nula", versao: nil, nome: "a", checksum: ok, code: "23502", coluna: "versao"},
		{name: "nome nulo", versao: 1, nome: nil, checksum: ok, code: "23502", coluna: "nome"},
		{name: "checksum nulo", versao: 1, nome: "a", checksum: nil, code: "23502", coluna: "checksum"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.Exec(ctx, "INSERT INTO migracao_aplicada (versao, nome, checksum) VALUES ($1, $2, $3)", tt.versao, tt.nome, tt.checksum)
			if err == nil {
				t.Fatalf("INSERT aceito, want SQLSTATE %s", tt.code)
			}
			pe := pgErro(t, err)
			if pe.Code != tt.code {
				t.Errorf("SQLSTATE = %s, want %s (%v)", pe.Code, tt.code, err)
			}
			if tt.constraint != "" && pe.ConstraintName != tt.constraint {
				t.Errorf("constraint = %q, want %q", pe.ConstraintName, tt.constraint)
			}
			if tt.coluna != "" && pe.ColumnName != tt.coluna {
				t.Errorf("coluna = %q, want %q", pe.ColumnName, tt.coluna)
			}
		})
	}

	t.Run("aplicada_em infinity", func(t *testing.T) {
		for _, v := range []string{"infinity", "-infinity"} {
			_, err := c.Exec(ctx, "INSERT INTO migracao_aplicada (versao, nome, checksum, aplicada_em) VALUES (1, 'a', $1, $2::timestamptz)", ok, v)
			pe := pgErro(t, err)
			if pe.Code != "23514" || pe.ConstraintName != "migracao_aplicada_aplicada_em_check" {
				t.Errorf("%s: erro = %s/%s, want 23514/migracao_aplicada_aplicada_em_check", v, pe.Code, pe.ConstraintName)
			}
		}
	})
	t.Run("aplicada_em nulo", func(t *testing.T) {
		_, err := c.Exec(ctx, "INSERT INTO migracao_aplicada (versao, nome, checksum, aplicada_em) VALUES (1, 'a', $1, NULL)", ok)
		pe := pgErro(t, err)
		if pe.Code != "23502" || pe.ColumnName != "aplicada_em" {
			t.Errorf("erro = %s/%s, want 23502/aplicada_em", pe.Code, pe.ColumnName)
		}
	})

	// Nenhuma das tentativas deixou linha.
	var n int
	if err := c.QueryRow(ctx, "SELECT count(*) FROM migracao_aplicada").Scan(&n); err != nil || n != 0 {
		t.Errorf("linhas depois das rejeições = %d (%v), want 0", n, err)
	}
}

func TestControleAceitaLinhasValidas(t *testing.T) {
	_, c := novoControle(t)
	ctx := t.Context()

	tests := []struct {
		name     string
		versao   int
		nome     string
		checksum string
	}{
		{name: "minimo", versao: 1, nome: "a", checksum: strings.Repeat("0", 64)},
		{name: "nome real", versao: 2, nome: "esquema_emprestimo", checksum: "0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"},
		{name: "nome so de sublinhado e digitos", versao: 3, nome: "_0_9_", checksum: strings.Repeat("f", 64)},
		{name: "versao maxima do integer", versao: 2147483647, nome: "ultima", checksum: strings.Repeat("9", 64)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var recente bool
			err := c.QueryRow(ctx,
				`INSERT INTO migracao_aplicada (versao, nome, checksum) VALUES ($1, $2, $3)
				 RETURNING aplicada_em <= now() AND aplicada_em > now() - interval '1 minute'`,
				tt.versao, tt.nome, tt.checksum).Scan(&recente)
			if err != nil {
				t.Fatalf("INSERT válido: %v", err)
			}
			if !recente {
				t.Error("aplicada_em não recebeu o padrão now()")
			}
		})
	}

	t.Run("versao duplicada", func(t *testing.T) {
		_, err := c.Exec(ctx, "INSERT INTO migracao_aplicada (versao, nome, checksum) VALUES (1, 'outra', $1)", strings.Repeat("b", 64))
		pe := pgErro(t, err)
		if pe.Code != "23505" || pe.ConstraintName != "migracao_aplicada_pkey" {
			t.Errorf("erro = %s/%s, want 23505/migracao_aplicada_pkey", pe.Code, pe.ConstraintName)
		}
	})
	t.Run("mesmo nome e checksum em versoes diferentes", func(t *testing.T) {
		if _, err := c.Exec(ctx, "INSERT INTO migracao_aplicada (versao, nome, checksum) VALUES (10, 'a', $1)", strings.Repeat("0", 64)); err != nil {
			t.Errorf("a PK é só a versão; nome e checksum repetidos devem passar: %v", err)
		}
	})
}

func TestControleUpEIdempotente(t *testing.T) {
	b, c := novoControle(t)
	ctx := t.Context()
	antes := catalogoControle(t, c, b.schema)

	if err := aplicarControle(t, c, upControleBD); err != nil {
		t.Fatalf("segundo up: %v", err)
	}
	if depois := catalogoControle(t, c, b.schema); depois != antes {
		t.Errorf("catálogo mudou no segundo up:\n%s\n---\n%s", antes, depois)
	}

	if _, err := c.Exec(ctx, "INSERT INTO migracao_aplicada (versao, nome, checksum) VALUES (1, 'a', $1)", strings.Repeat("a", 64)); err != nil {
		t.Fatalf("inserir entre os ups: %v", err)
	}
	var em0 string
	if err := c.QueryRow(ctx, "SELECT aplicada_em::text FROM migracao_aplicada WHERE versao = 1").Scan(&em0); err != nil {
		t.Fatalf("ler linha: %v", err)
	}
	if err := aplicarControle(t, c, upControleBD); err != nil {
		t.Fatalf("terceiro up: %v", err)
	}
	var n int
	var em1 string
	if err := c.QueryRow(ctx, "SELECT count(*), min(aplicada_em::text) FROM migracao_aplicada").Scan(&n, &em1); err != nil {
		t.Fatalf("ler linhas: %v", err)
	}
	if n != 1 || em1 != em0 {
		t.Errorf("depois do up repetido: %d linhas, aplicada_em %s (era %s), want a linha intacta", n, em1, em0)
	}
	if depois := catalogoControle(t, c, b.schema); depois != antes {
		t.Errorf("catálogo mudou no terceiro up")
	}
}

func TestControleDown(t *testing.T) {
	b, c := novoControle(t)

	if err := aplicarControle(t, c, downControleBD); err != nil {
		t.Fatalf("down: %v", err)
	}
	if n := contarRelacoes(t, c, b.schema); n != 0 {
		t.Errorf("depois do down: %d objetos em pg_class, want 0", n)
	}

	// Segundo down: sem IF EXISTS, falha alto com tabela inexistente.
	err := aplicarControle(t, c, downControleBD)
	if err == nil {
		t.Fatal("segundo down sem erro, want 42P01 (DROP sem IF EXISTS)")
	}
	if pe := pgErro(t, err); pe.Code != "42P01" {
		t.Errorf("segundo down SQLSTATE = %s, want 42P01", pe.Code)
	}

	// Ciclo up-down-up.
	if err := aplicarControle(t, c, upControleBD); err != nil {
		t.Fatalf("up depois do down: %v", err)
	}
	antes := catalogoControle(t, c, b.schema)
	if err := aplicarControle(t, c, downControleBD); err != nil {
		t.Fatalf("down: %v", err)
	}
	if err := aplicarControle(t, c, upControleBD); err != nil {
		t.Fatalf("up: %v", err)
	}
	if depois := catalogoControle(t, c, b.schema); depois != antes {
		t.Errorf("catálogo depois de up-down-up difere:\n%s\n---\n%s", antes, depois)
	}
}

// O down derruba a tabela com dados: a reversão não preserva o registro.
func TestControleDownComLinhas(t *testing.T) {
	b, c := novoControle(t)
	if _, err := c.Exec(t.Context(), "INSERT INTO migracao_aplicada (versao, nome, checksum) VALUES (1, 'a', $1)", strings.Repeat("a", 64)); err != nil {
		t.Fatalf("inserir: %v", err)
	}
	if err := aplicarControle(t, c, downControleBD); err != nil {
		t.Fatalf("down com linhas: %v", err)
	}
	if n := contarRelacoes(t, c, b.schema); n != 0 {
		t.Errorf("depois do down: %d objetos, want 0", n)
	}
}
