package storage

import (
	"fmt"
	"sort"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// Constraints esperadas (sem as NOT NULL, que o PostgreSQL 18 também registra
// em pg_constraint): nome -> tipo.
var constraintsEsperadas = map[string]string{
	"livro_pkey":                     "p",
	"livro_titulo_check":             "c",
	"usuario_pkey":                   "p",
	"usuario_nome_check":             "c",
	"emprestimo_pkey":                "p",
	"emprestimo_livro_id_fkey":       "f",
	"emprestimo_usuario_id_fkey":     "f",
	"emprestimo_datas_finitas_check": "c",
}

var indicesEsperados = []string{
	"emprestimo_livro_aberto_uq",
	"emprestimo_livro_id_idx",
	"emprestimo_pkey",
	"emprestimo_usuario_id_idx",
	"livro_pkey",
	"usuario_pkey",
}

type coluna struct {
	tipo      string
	nula      string
	identity  string
	geracaoID string
}

var colunasEsperadas = map[string]coluna{
	"livro.id":              {tipo: "bigint", nula: "NO", identity: "YES", geracaoID: "ALWAYS"},
	"livro.titulo":          {tipo: "text", nula: "NO", identity: "NO"},
	"usuario.id":            {tipo: "bigint", nula: "NO", identity: "YES", geracaoID: "ALWAYS"},
	"usuario.nome":          {tipo: "text", nula: "NO", identity: "NO"},
	"emprestimo.id":         {tipo: "bigint", nula: "NO", identity: "YES", geracaoID: "ALWAYS"},
	"emprestimo.livro_id":   {tipo: "bigint", nula: "NO", identity: "NO"},
	"emprestimo.usuario_id": {tipo: "bigint", nula: "NO", identity: "NO"},
	"emprestimo.inicio":     {tipo: "timestamp with time zone", nula: "NO", identity: "NO"},
	"emprestimo.vencimento": {tipo: "date", nula: "NO", identity: "NO"},
	"emprestimo.devolucao":  {tipo: "timestamp with time zone", nula: "YES", identity: "NO"},
}

func checarCatalogo(t *testing.T, c *pgx.Conn, schema string) {
	t.Helper()
	ctx := t.Context()

	// Constraints.
	rows, err := c.Query(ctx, `
		SELECT con.conname, con.contype::text, con.confdeltype::text,
		       con.conrelid::regclass::text,
		       COALESCE(NULLIF(con.confrelid, 0)::regclass::text, '')
		  FROM pg_constraint con
		  JOIN pg_namespace n ON n.oid = con.connamespace
		 WHERE n.nspname = $1 AND con.contype <> 'n'`, schema)
	if err != nil {
		t.Fatalf("consultar pg_constraint: %v", err)
	}
	type con struct{ tipo, delTipo, tabela, ref string }
	got := map[string]con{}
	for rows.Next() {
		var nome string
		var k con
		if err := rows.Scan(&nome, &k.tipo, &k.delTipo, &k.tabela, &k.ref); err != nil {
			t.Fatalf("ler pg_constraint: %v", err)
		}
		got[nome] = k
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar pg_constraint: %v", err)
	}
	for nome, tipo := range constraintsEsperadas {
		k, ok := got[nome]
		if !ok {
			t.Errorf("constraint %s ausente", nome)
			continue
		}
		if k.tipo != tipo {
			t.Errorf("constraint %s tipo = %s, want %s", nome, k.tipo, tipo)
		}
	}
	for nome := range got {
		if _, ok := constraintsEsperadas[nome]; !ok {
			t.Errorf("constraint inesperada %s (%+v)", nome, got[nome])
		}
	}
	for nome, ref := range map[string]string{"emprestimo_livro_id_fkey": "livro", "emprestimo_usuario_id_fkey": "usuario"} {
		k := got[nome]
		if k.tabela != "emprestimo" || k.ref != ref {
			t.Errorf("%s em %s -> %s, want emprestimo -> %s", nome, k.tabela, k.ref, ref)
		}
		if k.delTipo != "r" {
			t.Errorf("%s ON DELETE = %q, want r (RESTRICT)", nome, k.delTipo)
		}
	}

	// Definição dos CHECKs.
	defs := map[string]string{}
	rows, err = c.Query(ctx, `
		SELECT con.conname, pg_get_constraintdef(con.oid)
		  FROM pg_constraint con
		  JOIN pg_namespace n ON n.oid = con.connamespace
		 WHERE n.nspname = $1 AND con.contype = 'c'`, schema)
	if err != nil {
		t.Fatalf("consultar definição dos CHECKs: %v", err)
	}
	for rows.Next() {
		var nome, def string
		if err := rows.Scan(&nome, &def); err != nil {
			t.Fatalf("ler definição: %v", err)
		}
		defs[nome] = def
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar definição: %v", err)
	}
	for nome, trechos := range map[string][]string{
		"livro_titulo_check":             {"btrim(titulo)", "<> ''"},
		"usuario_nome_check":             {"btrim(nome)", "<> ''"},
		"emprestimo_datas_finitas_check": {"isfinite(inicio)", "isfinite(vencimento)", "isfinite(devolucao)"},
	} {
		for _, tr := range trechos {
			if !strings.Contains(defs[nome], tr) {
				t.Errorf("%s = %q, want conter %q", nome, defs[nome], tr)
			}
		}
	}

	// Índices.
	idx := map[string]string{}
	rows, err = c.Query(ctx, `SELECT indexname, indexdef FROM pg_indexes WHERE schemaname = $1`, schema)
	if err != nil {
		t.Fatalf("consultar pg_indexes: %v", err)
	}
	for rows.Next() {
		var nome, def string
		if err := rows.Scan(&nome, &def); err != nil {
			t.Fatalf("ler pg_indexes: %v", err)
		}
		idx[nome] = def
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar pg_indexes: %v", err)
	}
	var nomes []string
	for n := range idx {
		nomes = append(nomes, n)
	}
	sort.Strings(nomes)
	if strings.Join(nomes, ",") != strings.Join(indicesEsperados, ",") {
		t.Errorf("índices = %v, want %v", nomes, indicesEsperados)
	}
	uq := idx["emprestimo_livro_aberto_uq"]
	if !strings.HasPrefix(uq, "CREATE UNIQUE INDEX") || !strings.Contains(uq, "(livro_id)") || !strings.Contains(uq, "WHERE (devolucao IS NULL)") {
		t.Errorf("emprestimo_livro_aberto_uq = %q, want UNIQUE (livro_id) WHERE (devolucao IS NULL)", uq)
	}
	for nome, col := range map[string]string{"emprestimo_livro_id_idx": "(livro_id)", "emprestimo_usuario_id_idx": "(usuario_id)"} {
		d := idx[nome]
		if strings.Contains(d, "UNIQUE") || strings.Contains(d, "WHERE") || !strings.Contains(d, col) {
			t.Errorf("%s = %q, want índice simples em %s", nome, d, col)
		}
	}

	// Colunas.
	rows, err = c.Query(ctx, `
		SELECT table_name || '.' || column_name, data_type, is_nullable, is_identity, COALESCE(identity_generation, '')
		  FROM information_schema.columns
		 WHERE table_schema = $1`, schema)
	if err != nil {
		t.Fatalf("consultar colunas: %v", err)
	}
	cols := map[string]coluna{}
	for rows.Next() {
		var nome string
		var k coluna
		if err := rows.Scan(&nome, &k.tipo, &k.nula, &k.identity, &k.geracaoID); err != nil {
			t.Fatalf("ler colunas: %v", err)
		}
		cols[nome] = k
	}
	if err := rows.Err(); err != nil {
		t.Fatalf("iterar colunas: %v", err)
	}
	for nome, want := range colunasEsperadas {
		if got, ok := cols[nome]; !ok {
			t.Errorf("coluna %s ausente", nome)
		} else if got != want {
			t.Errorf("coluna %s = %+v, want %+v", nome, got, want)
		}
	}
	for nome := range cols {
		if _, ok := colunasEsperadas[nome]; !ok {
			t.Errorf("coluna inesperada %s", nome)
		}
	}
}

func contarObjetos(t *testing.T, c *pgx.Conn, schema string) int {
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

func TestMigracaoUpDownUp(t *testing.T) {
	b := novoBanco(t)
	c := b.conectar(t)

	if n := contarObjetos(t, c, b.schema); n != 0 {
		t.Fatalf("schema novo com %d objetos, want 0", n)
	}

	aplicar(t, c, ".up.sql")
	checarCatalogo(t, c, b.schema)

	aplicar(t, c, ".down.sql")
	if n := contarObjetos(t, c, b.schema); n != 0 {
		t.Errorf("depois do down: %d objetos em pg_class, want 0", n)
	}

	aplicar(t, c, ".up.sql")
	checarCatalogo(t, c, b.schema)
}

// Violações das regras do esquema por SQL direto, sem passar pelo repositório.
func TestEsquemaRestricoes(t *testing.T) {
	_, c := novoBancoMigrado(t)
	ctx := t.Context()

	var livroID, usuarioID int64
	if err := c.QueryRow(ctx, "INSERT INTO livro (titulo) VALUES ('Dom Casmurro') RETURNING id").Scan(&livroID); err != nil {
		t.Fatalf("inserir livro: %v", err)
	}
	if err := c.QueryRow(ctx, "INSERT INTO usuario (nome) VALUES ('Ana') RETURNING id").Scan(&usuarioID); err != nil {
		t.Fatalf("inserir usuario: %v", err)
	}
	emp := func(inicio, venc, dev string) string {
		return fmt.Sprintf("INSERT INTO emprestimo (livro_id, usuario_id, inicio, vencimento, devolucao) VALUES (%d, %d, %s, %s, %s)",
			livroID, usuarioID, inicio, venc, dev)
	}
	const (
		ini  = "'2026-03-10 10:00:00-03'"
		venc = "'2026-03-24'"
	)

	tests := []struct {
		name       string
		sql        string
		code       string
		constraint string
	}{
		{name: "titulo vazio", sql: "INSERT INTO livro (titulo) VALUES ('')", code: "23514", constraint: "livro_titulo_check"},
		{name: "titulo so espacos", sql: "INSERT INTO livro (titulo) VALUES ('   ')", code: "23514", constraint: "livro_titulo_check"},
		{name: "titulo nulo", sql: "INSERT INTO livro (titulo) VALUES (NULL)", code: "23502"},
		{name: "livro com id explicito", sql: "INSERT INTO livro (id, titulo) VALUES (999, 'x')", code: "428C9"},
		{name: "nome vazio", sql: "INSERT INTO usuario (nome) VALUES ('')", code: "23514", constraint: "usuario_nome_check"},
		{name: "nome so espacos", sql: "INSERT INTO usuario (nome) VALUES ('   ')", code: "23514", constraint: "usuario_nome_check"},
		{name: "nome nulo", sql: "INSERT INTO usuario (nome) VALUES (NULL)", code: "23502"},
		{name: "inicio infinity", sql: emp("'infinity'", venc, "NULL"), code: "23514", constraint: "emprestimo_datas_finitas_check"},
		{name: "inicio -infinity", sql: emp("'-infinity'", venc, "NULL"), code: "23514", constraint: "emprestimo_datas_finitas_check"},
		{name: "vencimento infinity", sql: emp(ini, "'infinity'", "NULL"), code: "23514", constraint: "emprestimo_datas_finitas_check"},
		{name: "vencimento -infinity", sql: emp(ini, "'-infinity'", "NULL"), code: "23514", constraint: "emprestimo_datas_finitas_check"},
		{name: "devolucao infinity", sql: emp(ini, venc, "'infinity'"), code: "23514", constraint: "emprestimo_datas_finitas_check"},
		{name: "inicio nulo", sql: emp("NULL", venc, "NULL"), code: "23502"},
		{name: "vencimento nulo", sql: emp(ini, "NULL", "NULL"), code: "23502"},
		{name: "livro inexistente", sql: fmt.Sprintf("INSERT INTO emprestimo (livro_id, usuario_id, inicio, vencimento) VALUES (%d, %d, %s, %s)", livroID+1000, usuarioID, ini, venc), code: "23503", constraint: "emprestimo_livro_id_fkey"},
		{name: "usuario inexistente", sql: fmt.Sprintf("INSERT INTO emprestimo (livro_id, usuario_id, inicio, vencimento) VALUES (%d, %d, %s, %s)", livroID, usuarioID+1000, ini, venc), code: "23503", constraint: "emprestimo_usuario_id_fkey"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := c.Exec(ctx, tt.sql)
			if err == nil {
				t.Fatalf("Exec(%s) sem erro, want SQLSTATE %s", tt.sql, tt.code)
			}
			pe := pgErro(t, err)
			if pe.Code != tt.code {
				t.Errorf("SQLSTATE = %s, want %s (%v)", pe.Code, tt.code, err)
			}
			if tt.constraint != "" && pe.ConstraintName != tt.constraint {
				t.Errorf("constraint = %q, want %q", pe.ConstraintName, tt.constraint)
			}
		})
	}

	t.Run("R4 no banco: segundo aberto do mesmo livro", func(t *testing.T) {
		if _, err := c.Exec(ctx, emp(ini, venc, "NULL")); err != nil {
			t.Fatalf("primeiro empréstimo: %v", err)
		}
		_, err := c.Exec(ctx, emp(ini, venc, "NULL"))
		pe := pgErro(t, err)
		if pe.Code != "23505" || pe.ConstraintName != "emprestimo_livro_aberto_uq" {
			t.Errorf("erro = %s/%s, want 23505/emprestimo_livro_aberto_uq", pe.Code, pe.ConstraintName)
		}
		// Fechados não contam para R4.
		if _, err := c.Exec(ctx, emp(ini, venc, "'2026-03-12 10:00:00-03'")); err != nil {
			t.Errorf("empréstimo fechado do mesmo livro: %v", err)
		}
	})

	t.Run("DELETE de livro com empréstimo", func(t *testing.T) {
		_, err := c.Exec(ctx, "DELETE FROM livro WHERE id = $1", livroID)
		pe := pgErro(t, err)
		if pe.Code != "23503" || pe.ConstraintName != "emprestimo_livro_id_fkey" {
			t.Errorf("erro = %s/%s, want 23503/emprestimo_livro_id_fkey", pe.Code, pe.ConstraintName)
		}
	})

	t.Run("DELETE de usuario com empréstimo", func(t *testing.T) {
		_, err := c.Exec(ctx, "DELETE FROM usuario WHERE id = $1", usuarioID)
		pe := pgErro(t, err)
		if pe.Code != "23503" || pe.ConstraintName != "emprestimo_usuario_id_fkey" {
			t.Errorf("erro = %s/%s, want 23503/emprestimo_usuario_id_fkey", pe.Code, pe.ConstraintName)
		}
	})
}
