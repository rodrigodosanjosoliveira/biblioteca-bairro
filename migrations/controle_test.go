package migrations

import (
	"io/fs"
	"regexp"
	"sort"
	"strings"
	"testing"
)

// Contrato fixado por estes testes (DEL-008, G5/G7):
//
//	//go:embed controle/*.sql
//	var Controle embed.FS   // (qualquer fs.FS; o executor o lê como fs.FS)
//
// com exatamente controle/0000_migracao_aplicada.up.sql e .down.sql. FS
// permanece intocado e a raiz de migrations/ não ganha arquivo de versão 0000.

const (
	upControle   = "controle/0000_migracao_aplicada.up.sql"
	downControle = "controle/0000_migracao_aplicada.down.sql"
)

var (
	comentarioLinha = regexp.MustCompile(`--[^\n]*`)
	comentarioBloco = regexp.MustCompile(`(?s)/\*.*?\*/`)
)

// semComentarios remove comentários `--` e `/* */`.
func semComentarios(sql string) string {
	sql = comentarioBloco.ReplaceAllString(sql, " ")
	return comentarioLinha.ReplaceAllString(sql, " ")
}

func lerControle(t *testing.T, nome string) string {
	t.Helper()
	b, err := fs.ReadFile(Controle, nome)
	if err != nil {
		t.Fatalf("ler %s do Controle embutido: %v", nome, err)
	}
	return string(b)
}

func TestControleContemSoOParDaVersao0000(t *testing.T) {
	var arquivos []string
	err := fs.WalkDir(Controle, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			arquivos = append(arquivos, p)
		}
		return nil
	})
	if err != nil {
		t.Fatalf("percorrer Controle: %v", err)
	}
	sort.Strings(arquivos)
	want := []string{downControle, upControle}
	if strings.Join(arquivos, ",") != strings.Join(want, ",") {
		t.Errorf("arquivos do Controle = %v, want %v", arquivos, want)
	}
}

func TestFSNaoTemVersao0000(t *testing.T) {
	nomes, err := fs.Glob(FS, "*")
	if err != nil {
		t.Fatalf("listar FS: %v", err)
	}
	for _, n := range nomes {
		if strings.HasPrefix(n, "0000") || strings.Contains(n, "migracao_aplicada") {
			t.Errorf("arquivo %q na raiz de FS: o controle fica só em Controle", n)
		}
	}
}

func TestControleDown(t *testing.T) {
	sql := lerControle(t, downControle)
	limpo := strings.ToUpper(semComentarios(sql))

	for _, proibido := range []string{"IF EXISTS", "CASCADE"} {
		if strings.Contains(limpo, proibido) {
			t.Errorf("down do controle contém %q; deve falhar alto se a tabela não existir", proibido)
		}
	}
	re := regexp.MustCompile(`(?i)\bDROP\b`)
	if n := len(re.FindAllString(limpo, -1)); n != 1 {
		t.Errorf("down com %d DROP, want exatamente 1", n)
	}
	tabelas := regexp.MustCompile(`(?i)\bDROP\s+TABLE\s+"?([a-z_]+)"?\s*;`).FindAllStringSubmatch(sql, -1)
	if len(tabelas) != 1 || strings.ToLower(tabelas[0][1]) != "migracao_aplicada" {
		t.Errorf("down deve ser um único DROP TABLE migracao_aplicada; achou %v", tabelas)
	}
	if strings.Contains(strings.ToLower(sql), "public.") {
		t.Error("down qualifica objetos com public.; use o search_path")
	}
}

func TestControleUp(t *testing.T) {
	sql := lerControle(t, upControle)
	limpo := strings.ToLower(semComentarios(sql))

	if strings.Contains(strings.ToLower(sql), "public.") {
		t.Error("up qualifica objetos com public.; use o search_path")
	}
	for _, p := range []string{"begin", "commit", "rollback", "savepoint", "start transaction", "end", "concurrently"} {
		if regexp.MustCompile(`\b` + p + `\b`).MatchString(limpo) {
			t.Errorf("up do controle contém %q fora de comentário", p)
		}
	}
	for _, trecho := range []string{
		"create table if not exists migracao_aplicada",
		"migracao_aplicada_pkey",
		"migracao_aplicada_versao_check",
		"migracao_aplicada_nome_check",
		"migracao_aplicada_checksum_check",
		"migracao_aplicada_aplicada_em_check",
	} {
		if !strings.Contains(limpo, trecho) {
			t.Errorf("up do controle sem %q", trecho)
		}
	}
}

// Os arquivos embutidos (FS e Controle) não controlam transação nem usam
// CONCURRENTLY: o executor aplica cada arquivo dentro de uma transação sua
// (G7). Considera comentários também: o executor recusa a palavra em qualquer
// ponto do arquivo.
func TestArquivosEmbutidosNaoControlamTransacao(t *testing.T) {
	inicioTx := regexp.MustCompile(`(?im)^\s*(begin|commit|rollback|start\s+transaction|end|savepoint)\b`)
	concurrently := regexp.MustCompile(`(?i)\bconcurrently\b`)

	for nome, sistema := range map[string]fs.FS{"FS": FS, "Controle": Controle} {
		err := fs.WalkDir(sistema, ".", func(p string, d fs.DirEntry, err error) error {
			if err != nil {
				return err
			}
			if d.IsDir() {
				return nil
			}
			b, err := fs.ReadFile(sistema, p)
			if err != nil {
				return err
			}
			if m := inicioTx.FindString(string(b)); m != "" {
				t.Errorf("%s/%s tem linha iniciando em %q: o executor já abre e fecha a transação", nome, p, strings.TrimSpace(m))
			}
			if concurrently.Match(b) {
				t.Errorf("%s/%s contém CONCURRENTLY (inclusive em comentário): o executor recusa", nome, p)
			}
			return nil
		})
		if err != nil {
			t.Fatalf("percorrer %s: %v", nome, err)
		}
	}
}
