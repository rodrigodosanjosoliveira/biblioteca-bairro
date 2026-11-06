package migrations

import (
	"regexp"
	"strings"
	"testing"
)

const (
	up0002   = "0002_renovacao_emprestimo.up.sql"
	down0002 = "0002_renovacao_emprestimo.down.sql"
)

// espacos normaliza espaços para comparar trechos de SQL.
func espacos(s string) string {
	return strings.Join(strings.Fields(strings.ToLower(s)), " ")
}

func TestUp0002(t *testing.T) {
	sql := ler(t, up0002)
	limpo := espacos(semComentarios(sql))

	if strings.Contains(strings.ToLower(sql), "public.") {
		t.Error("up qualifica objetos com public.; use o search_path")
	}
	for _, trecho := range []string{
		"alter table emprestimo",
		"add column renovacoes integer not null default 0",
		"add constraint emprestimo_renovacoes_check check (renovacoes >= 0 and renovacoes <= 2)",
	} {
		if !strings.Contains(limpo, trecho) {
			t.Errorf("up sem %q (SQL normalizado: %s)", trecho, limpo)
		}
	}
	// Coluna e CHECK no mesmo ALTER TABLE; além dele, só COMMENT ON COLUMN
	// opcional.
	alters := 0
	for _, cmd := range strings.Split(limpo, ";") {
		cmd = strings.TrimSpace(cmd)
		switch {
		case cmd == "":
		case strings.HasPrefix(cmd, "alter table emprestimo "):
			alters++
		case strings.HasPrefix(cmd, "comment on column emprestimo.renovacoes is "):
		default:
			t.Errorf("comando inesperado no up: %q", cmd)
		}
	}
	if alters != 1 {
		t.Errorf("up com %d ALTER TABLE, want 1", alters)
	}
	for _, proibido := range []string{"not valid", "validate constraint", " set ", "set_config", "smallint", "bigint", "drop ", "update ", "insert "} {
		if strings.Contains(limpo, proibido) {
			t.Errorf("up contém %q", strings.TrimSpace(proibido))
		}
	}
}

func TestDown0002(t *testing.T) {
	sql := ler(t, down0002)
	limpo := espacos(semComentarios(sql))
	up := strings.ToUpper(sql)

	for _, proibido := range []string{"IF EXISTS", "CASCADE"} {
		if strings.Contains(up, proibido) {
			t.Errorf("down contém %q; a reversão deve falhar alto se o esquema não for o esperado", proibido)
		}
	}
	if strings.Contains(limpo, "drop table") {
		t.Error("down da 0002 não pode remover tabela")
	}
	re := regexp.MustCompile(`drop column ([a-z_]+)`)
	var cols []string
	for _, m := range re.FindAllStringSubmatch(limpo, -1) {
		cols = append(cols, m[1])
	}
	if strings.Join(cols, ",") != "renovacoes" {
		t.Errorf("DROP COLUMN em %v, want só renovacoes", cols)
	}
	for _, trecho := range []string{"alter table emprestimo", "drop constraint emprestimo_renovacoes_check"} {
		if !strings.Contains(limpo, trecho) {
			t.Errorf("down sem %q (SQL normalizado: %s)", trecho, limpo)
		}
	}
	if i, j := strings.Index(limpo, "drop constraint"), strings.Index(limpo, "drop column"); i < 0 || j < 0 || i > j {
		t.Errorf("down deve remover a constraint antes da coluna: %s", limpo)
	}
}
