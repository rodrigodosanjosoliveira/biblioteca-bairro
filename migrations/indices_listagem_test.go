package migrations

import (
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Migração 0003 (DEL-015, D-21): índices da listagem filtrada de
// empréstimos. Aditiva: só CREATE INDEX no up, só DROP INDEX dos mesmos três
// no down. A palavra recusada pelo executor e o controle de transação já são
// conferidos em todo o FS por TestArquivosEmbutidosNaoControlamTransacao.
const (
	up0003   = "0003_indices_listagem_emprestimo.up.sql"
	down0003 = "0003_indices_listagem_emprestimo.down.sql"
)

// indices0003 são os nomes criados pela 0003, em ordem alfabética.
var indices0003 = []string{
	"emprestimo_aberto_id_idx",
	"emprestimo_livro_id_id_idx",
	"emprestimo_usuario_id_id_idx",
}

// comandos separa o SQL normalizado em comandos não vazios.
func comandos(limpo string) []string {
	var out []string
	for _, c := range strings.Split(limpo, ";") {
		if c = strings.TrimSpace(c); c != "" {
			out = append(out, c)
		}
	}
	return out
}

func TestUp0003(t *testing.T) {
	sql := ler(t, up0003)
	limpo := espacos(semComentarios(sql))

	want := []string{
		"create index emprestimo_usuario_id_id_idx on emprestimo (usuario_id, id)",
		"create index emprestimo_livro_id_id_idx on emprestimo (livro_id, id)",
		"create index emprestimo_aberto_id_idx on emprestimo (id) where devolucao is null",
	}
	got := comandos(limpo)
	if strings.Join(got, ";\n") != strings.Join(want, ";\n") {
		t.Errorf("comandos do up =\n  %q\nwant\n  %q", got, want)
	}
	if strings.Contains(strings.ToLower(sql), "public.") {
		t.Error("up qualifica objetos com public.; use o search_path")
	}
	for _, proibido := range []string{
		"unique", "if not exists", "drop ", "alter ", " set ", "set_config", "include",
		"now()", "current_date", "current_timestamp", "localtimestamp", "using ", "public.",
		"begin", "commit",
	} {
		if strings.Contains(limpo, proibido) {
			t.Errorf("up contém %q (SQL normalizado: %s)", strings.TrimSpace(proibido), limpo)
		}
	}
}

func TestDown0003(t *testing.T) {
	sql := ler(t, down0003)
	limpo := espacos(semComentarios(sql))
	up := strings.ToUpper(sql)

	for _, proibido := range []string{"IF EXISTS", "CASCADE"} {
		if strings.Contains(up, proibido) {
			t.Errorf("down contém %q; a reversão deve falhar alto se o esquema não for o esperado", proibido)
		}
	}
	for _, proibido := range []string{"drop table", "drop column", "alter ", "create ", " set ", "set_config"} {
		if strings.Contains(limpo, proibido) {
			t.Errorf("down da 0003 contém %q", strings.TrimSpace(proibido))
		}
	}
	re := regexp.MustCompile(`drop index ([a-z_]+)`)
	var nomes []string
	for _, m := range re.FindAllStringSubmatch(limpo, -1) {
		nomes = append(nomes, m[1])
	}
	if len(comandos(limpo)) != len(nomes) {
		t.Errorf("down com %d comandos e %d DROP INDEX: só DROP INDEX é permitido (%s)", len(comandos(limpo)), len(nomes), limpo)
	}
	sort.Strings(nomes)
	if strings.Join(nomes, ",") != strings.Join(indices0003, ",") {
		t.Errorf("DROP INDEX de %v, want exatamente %v", nomes, indices0003)
	}
	// Os índices das migrações anteriores e a PK ficam.
	for _, alheio := range []string{"emprestimo_usuario_id_idx", "emprestimo_livro_id_idx", "emprestimo_livro_aberto_uq", "emprestimo_pkey"} {
		if regexp.MustCompile(`\b` + alheio + `\b`).MatchString(limpo) {
			t.Errorf("down da 0003 cita %s, que não é dela", alheio)
		}
	}
}
