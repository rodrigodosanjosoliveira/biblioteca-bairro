package migrations

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
)

var nomeMigracao = regexp.MustCompile(`^[0-9]{4}_[a-z0-9_]+\.(up|down)\.sql$`)

func ler(t *testing.T, nome string) string {
	t.Helper()
	b, err := fs.ReadFile(FS, nome)
	if err != nil {
		t.Fatalf("ler %s do FS embutido: %v", nome, err)
	}
	return string(b)
}

func TestFSContemSoMigracoesPareadas(t *testing.T) {
	nomes, err := fs.Glob(FS, "*")
	if err != nil {
		t.Fatalf("listar FS: %v", err)
	}
	sort.Strings(nomes)
	if len(nomes) == 0 {
		t.Fatal("FS embutido vazio")
	}

	ups := map[string]bool{}
	downs := map[string]bool{}
	for _, n := range nomes {
		if !nomeMigracao.MatchString(n) {
			t.Errorf("arquivo %q no FS fora do padrão NNNN_nome.{up,down}.sql", n)
			continue
		}
		if base, ok := strings.CutSuffix(n, ".up.sql"); ok {
			ups[base] = true
		}
		if base, ok := strings.CutSuffix(n, ".down.sql"); ok {
			downs[base] = true
		}
	}
	for b := range ups {
		if !downs[b] {
			t.Errorf("migração %s.up.sql sem %s.down.sql", b, b)
		}
	}
	for b := range downs {
		if !ups[b] {
			t.Errorf("migração %s.down.sql sem %s.up.sql", b, b)
		}
	}
	if !ups["0001_esquema_emprestimo"] {
		t.Error("falta 0001_esquema_emprestimo.up.sql")
	}
}

func TestDown0001(t *testing.T) {
	sql := ler(t, "0001_esquema_emprestimo.down.sql")
	up := strings.ToUpper(sql)

	for _, proibido := range []string{"IF EXISTS", "CASCADE"} {
		if strings.Contains(up, proibido) {
			t.Errorf("down contém %q; a reversão deve falhar alto se o esquema não for o esperado", proibido)
		}
	}

	// Ordem de remoção respeita as FKs: emprestimo, usuario, livro.
	re := regexp.MustCompile(`(?i)DROP\s+TABLE\s+"?([a-z_]+)"?`)
	var tabelas []string
	for _, m := range re.FindAllStringSubmatch(sql, -1) {
		tabelas = append(tabelas, strings.ToLower(m[1]))
	}
	want := []string{"emprestimo", "usuario", "livro"}
	if strings.Join(tabelas, ",") != strings.Join(want, ",") {
		t.Errorf("DROP TABLE na ordem %v, want %v", tabelas, want)
	}
}

func TestUp0001NaoQualificaSchema(t *testing.T) {
	// As tabelas vão para o search_path corrente; qualificar com public quebra
	// o isolamento por schema dos testes.
	sql := strings.ToLower(ler(t, "0001_esquema_emprestimo.up.sql"))
	if strings.Contains(sql, "public.") {
		t.Error("up qualifica objetos com public.; use o search_path")
	}
}
