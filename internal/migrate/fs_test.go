package migrate_test

import (
	"errors"
	"fmt"
	"io/fs"
	"regexp"
	"strings"
	"testing"
	"testing/fstest"
	"time"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/migrate"
)

// Estes testes não usam banco: ValidarFS e TemConcurrently são funções puras.

// Valor da chave do advisory lock de produção: só o valor da constante é
// conferido; nenhum teste a segura.
func TestChaveLockPadrao(t *testing.T) {
	const k int64 = migrate.ChaveLockPadrao
	if k == 0 {
		t.Error("ChaveLockPadrao = 0: 0 significa 'usar o padrão' em Opcoes.ChaveLock")
	}
}

func TestPadroesDeTimeoutDoExecutor(t *testing.T) {
	// O executor recebe os timeouts por Opcoes; a política (padrão e limites) é
	// do pacote config. Aqui só se confere que os tipos aceitam time.Duration.
	op := migrate.Opcoes{LockTimeout: time.Second, StatementTimeout: time.Minute}
	if op.LockTimeout != time.Second || op.StatementTimeout != time.Minute {
		t.Errorf("Opcoes = %+v", op)
	}
}

func TestErroContrato(t *testing.T) {
	sentinelas := map[string]migrate.Erro{
		"ErrLockOcupado":         migrate.ErrLockOcupado,
		"ErroBancoSemRegistro":   migrate.ErroBancoSemRegistro,
		"ErroChecksumDivergente": migrate.ErroChecksumDivergente,
		"ErroTransacaoEncerrada": migrate.ErroTransacaoEncerrada,
		"ErroFSInvalido":         migrate.ErroFSInvalido,
		"ErroConcurrently":       migrate.ErroConcurrently,
		"ErroVersaoDesconhecida": migrate.ErroVersaoDesconhecida,
		"ErroRegistroComLacuna":  migrate.ErroRegistroComLacuna,
		"ErroControleInvalido":   migrate.ErroControleInvalido,
		"ErroSchemaIndefinido":   migrate.ErroSchemaIndefinido,
		"ErroSQL":                migrate.ErroSQL,
		"ErroBanco":              migrate.ErroBanco,
		"ErroCancelado":          migrate.ErroCancelado,
	}
	vistas := map[string]string{}
	for nome, e := range sentinelas {
		msg := e.Error()
		if msg == "" || strings.ContainsAny(msg, "\n\r") {
			t.Errorf("%s.Error() = %q, want mensagem fixa de uma linha", nome, msg)
		}
		if outra, dup := vistas[msg]; dup {
			t.Errorf("%s e %s têm a mesma mensagem %q", nome, outra, msg)
		}
		vistas[msg] = nome
		if errors.Unwrap(e) != nil {
			t.Errorf("%s tem Unwrap", nome)
		}
		if !errors.Is(e, e) {
			t.Errorf("errors.Is(%s, %s) = false", nome, nome)
		}
		for outro, o := range sentinelas {
			if outro != nome && errors.Is(e, o) {
				t.Errorf("errors.Is(%s, %s) = true", nome, outro)
			}
		}
	}

	// Com arquivo, SQLSTATE e constraint a identidade (tipo) se mantém e a
	// mensagem cita o nome do arquivo.
	e := migrate.ErroSQL
	e.Arquivo = "0007_x.up.sql"
	e.Code = "23505"
	e.Constraint = "x_uq"
	if !errors.Is(e, migrate.ErroSQL) {
		t.Error("ErroSQL com Arquivo/Code/Constraint deixou de casar com migrate.ErroSQL")
	}
	if errors.Is(e, migrate.ErroFSInvalido) {
		t.Error("ErroSQL casa com ErroFSInvalido")
	}
	if !strings.Contains(e.Error(), "0007_x.up.sql") {
		t.Errorf("Error() = %q, want conter o nome do arquivo", e.Error())
	}
	var via error = fmt.Errorf("contexto: %w", e)
	if !errors.Is(via, migrate.ErroSQL) {
		t.Error("errors.Is através de embrulho externo falhou")
	}
}

// ---- ValidarFS ----

func TestValidarFSValido(t *testing.T) {
	doze := make([]mig, 0, 12)
	for i := 1; i <= 12; i++ {
		doze = append(doze, mig{V: i, Nome: fmt.Sprintf("m%d", i), Up: fmt.Sprintf("CREATE TABLE m%d (x integer);\n", i)})
	}
	tests := []struct {
		name string
		fsys fs.FS
		want []migrate.Migracao
	}{
		{
			name: "uma migracao",
			fsys: novoFS(mig{V: 1, Nome: "esquema", Up: "CREATE TABLE a (x integer);\n"}),
			want: []migrate.Migracao{{Versao: 1, Nome: "esquema", Checksum: sha("CREATE TABLE a (x integer);\n")}},
		},
		{
			name: "nome com sublinhados e digitos",
			fsys: novoFS(mig{V: 1, Nome: "_x1_2__", Up: "SELECT 1;"}),
			want: []migrate.Migracao{{Versao: 1, Nome: "_x1_2__", Checksum: sha("SELECT 1;")}},
		},
		{
			name: "up vazio",
			fsys: novoFS(mig{V: 1, Nome: "vazia", Up: ""}),
			want: []migrate.Migracao{{Versao: 1, Nome: "vazia", Checksum: sha("")}},
		},
		{
			name: "checksum cobre os bytes brutos (CRLF e BOM)",
			fsys: novoFS(mig{V: 1, Nome: "bytes", Up: "\xef\xbb\xbfSELECT 1;\r\n"}),
			want: []migrate.Migracao{{Versao: 1, Nome: "bytes", Checksum: sha("\xef\xbb\xbfSELECT 1;\r\n")}},
		},
		{
			name: "ordem numerica, independente da ordem de insercao",
			fsys: novoFS(doze[11], doze[3], doze[0], doze[10], doze[1], doze[2], doze[9], doze[4], doze[8], doze[5], doze[7], doze[6]),
			want: func() []migrate.Migracao {
				var w []migrate.Migracao
				for _, m := range doze {
					w = append(w, migrate.Migracao{Versao: m.V, Nome: m.Nome, Checksum: sha(m.Up)})
				}
				return w
			}(),
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := migrate.ValidarFS(tt.fsys)
			if err != nil {
				t.Fatalf("ValidarFS() erro = %v", err)
			}
			if len(got) != len(tt.want) {
				t.Fatalf("ValidarFS() = %+v, want %+v", got, tt.want)
			}
			for i := range got {
				if got[i].Versao != tt.want[i].Versao || got[i].Nome != tt.want[i].Nome || got[i].Checksum != tt.want[i].Checksum {
					t.Errorf("migração %d = {%d %s %s}, want {%d %s %s}", i, got[i].Versao, got[i].Nome, got[i].Checksum,
						tt.want[i].Versao, tt.want[i].Nome, tt.want[i].Checksum)
				}
			}
		})
	}
}

func parAB() fstest.MapFS {
	return novoFS(mig{V: 1, Nome: "a", Up: "CREATE TABLE a (x integer);\n"})
}

// comExtra devolve o par 0001_a mais os arquivos extras.
func comExtra(extra map[string]string) fstest.MapFS {
	f := parAB()
	for n, c := range extra {
		f[n] = &fstest.MapFile{Data: []byte(c)}
	}
	return f
}

// casosFSInvalido é compartilhado com os testes de banco: nenhum deles pode
// ter efeito no banco.
var casosFSInvalido = []struct {
	name string
	fsys fs.FS
	// arquivo, se não vazio, tem que aparecer na mensagem do erro (ele ou o seu
	// par up/down: se os dois são inválidos, vale apontar qualquer um).
	arquivo string
}{
	{name: "FS vazio", fsys: fstest.MapFS{}},
	{name: "nome com 3 digitos", fsys: comExtra(map[string]string{"002_b.up.sql": "SELECT 1;", "002_b.down.sql": "SELECT 1;"}), arquivo: "002_b.up.sql"},
	{name: "nome com 5 digitos", fsys: comExtra(map[string]string{"00002_b.up.sql": "SELECT 1;", "00002_b.down.sql": "SELECT 1;"}), arquivo: "00002_b.up.sql"},
	{name: "hifen no lugar do sublinhado", fsys: comExtra(map[string]string{"0002-b.up.sql": "SELECT 1;", "0002-b.down.sql": "SELECT 1;"}), arquivo: "0002-b.up.sql"},
	{name: "nome com maiuscula", fsys: comExtra(map[string]string{"0002_B.up.sql": "SELECT 1;", "0002_B.down.sql": "SELECT 1;"}), arquivo: "0002_B.up.sql"},
	{name: "direcao em maiuscula", fsys: comExtra(map[string]string{"0002_b.UP.sql": "SELECT 1;", "0002_b.down.sql": "SELECT 1;"}), arquivo: "0002_b.UP.sql"},
	{name: "nome vazio depois do sublinhado", fsys: comExtra(map[string]string{"0002_.up.sql": "SELECT 1;", "0002_.down.sql": "SELECT 1;"}), arquivo: "0002_.up.sql"},
	{name: "sem direcao", fsys: comExtra(map[string]string{"0002_b.sql": "SELECT 1;"}), arquivo: "0002_b.sql"},
	{name: "sufixo extra", fsys: comExtra(map[string]string{"0001_a.up.sql.bak": "SELECT 1;"}), arquivo: "0001_a.up.sql.bak"},
	{name: "quebra de linha no nome", fsys: comExtra(map[string]string{"0002_b.up.sql\n": "SELECT 1;"})},
	{name: "espaco no nome", fsys: comExtra(map[string]string{"0002_b c.up.sql": "SELECT 1;", "0002_b c.down.sql": "SELECT 1;"}), arquivo: "0002_b c.up.sql"},
	{name: "acento no nome", fsys: comExtra(map[string]string{"0002_ação.up.sql": "SELECT 1;", "0002_ação.down.sql": "SELECT 1;"}), arquivo: "0002_ação.up.sql"},
	{name: "digitos nao ASCII", fsys: comExtra(map[string]string{"０００２_b.up.sql": "SELECT 1;", "０００２_b.down.sql": "SELECT 1;"}), arquivo: "０００２_b.up.sql"},
	{name: "arquivo estranho no raiz", fsys: comExtra(map[string]string{"README.md": "x"}), arquivo: "README.md"},
	{name: "arquivo oculto no raiz", fsys: comExtra(map[string]string{".gitkeep": ""}), arquivo: ".gitkeep"},
	{name: "subdiretorio no raiz", fsys: comExtra(map[string]string{"sub/0002_b.up.sql": "SELECT 1;", "sub/0002_b.down.sql": "SELECT 1;"})},
	{name: "up sem down", fsys: novoFSPartes(map[string]string{"0001_a.up.sql": "SELECT 1;"}), arquivo: "0001_a.up.sql"},
	{name: "down sem up", fsys: novoFSPartes(map[string]string{"0001_a.down.sql": "SELECT 1;"})},
	{name: "up e down de nomes diferentes", fsys: novoFSPartes(map[string]string{"0001_a.up.sql": "SELECT 1;", "0001_b.down.sql": "SELECT 1;"})},
	{name: "up da 2 sem down", fsys: comExtra(map[string]string{"0002_b.up.sql": "SELECT 1;"}), arquivo: "0002_b.up.sql"},
	{name: "down da 2 sem up", fsys: comExtra(map[string]string{"0002_b.down.sql": "SELECT 1;"}), arquivo: "0002_b.down.sql"},
	{name: "versao duplicada com nomes diferentes", fsys: comExtra(map[string]string{"0001_b.up.sql": "SELECT 1;", "0001_b.down.sql": "SELECT 1;"})},
	{name: "lacuna 0001 e 0003", fsys: comExtra(map[string]string{"0003_c.up.sql": "SELECT 1;", "0003_c.down.sql": "SELECT 1;"})},
	{name: "lacuna no meio de quatro", fsys: novoFS(
		mig{V: 1, Nome: "a", Up: "SELECT 1;"}, mig{V: 2, Nome: "b", Up: "SELECT 1;"}, mig{V: 4, Nome: "d", Up: "SELECT 1;"})},
	{name: "comeca em 0002", fsys: novoFS(mig{V: 2, Nome: "b", Up: "SELECT 1;"})},
	{name: "comeca em 0000", fsys: novoFS(mig{V: 0, Nome: "controle", Up: "SELECT 1;"})},
	{name: "0000 e 0001", fsys: novoFS(mig{V: 0, Nome: "controle", Up: "SELECT 1;"}, mig{V: 1, Nome: "a", Up: "SELECT 1;"})},
	{name: "diretorio com nome de migracao", fsys: comExtra(map[string]string{"0002_b.up.sql/x": "SELECT 1;", "0002_b.down.sql": "SELECT 1;"})},
}

// novoFSPartes monta um MapFS só com os arquivos dados.
func novoFSPartes(arquivos map[string]string) fstest.MapFS {
	f := fstest.MapFS{}
	for n, c := range arquivos {
		f[n] = &fstest.MapFile{Data: []byte(c)}
	}
	return f
}

func TestValidarFSInvalido(t *testing.T) {
	for _, tt := range casosFSInvalido {
		t.Run(tt.name, func(t *testing.T) {
			got, err := migrate.ValidarFS(tt.fsys)
			if err == nil {
				t.Fatalf("ValidarFS() = %+v, nil, want ErroFSInvalido", got)
			}
			if !errors.Is(err, migrate.ErroFSInvalido) {
				t.Errorf("erro = %v, want ErroFSInvalido", err)
			}
			semDetalhe(t, err)
			if got != nil {
				t.Errorf("ValidarFS() devolveu %+v junto com erro, want nil", got)
			}
			if tt.arquivo != "" && !citaArquivo(err.Error(), tt.arquivo) {
				t.Errorf("erro %q, want conter o arquivo %q (ou o seu par up/down)", err.Error(), tt.arquivo)
			}
		})
	}
}

// citaArquivo diz se msg cita o arquivo ou o seu par (up/down): quando os dois
// lados do par são inválidos, o executor pode apontar qualquer um deles.
func citaArquivo(msg, arquivo string) bool {
	par := strings.Replace(arquivo, ".up.sql", ".down.sql", 1)
	if par == arquivo {
		par = strings.Replace(arquivo, ".down.sql", ".up.sql", 1)
	}
	return strings.Contains(msg, arquivo) || strings.Contains(msg, par)
}

// O FS embutido real passa na validação com 0001 no topo (a lista cresce com
// as próximas migrações; o piso é a 0001).
func TestValidarFSDoRepositorio(t *testing.T) {
	ms, err := migrate.ValidarFS(realFS())
	if err != nil {
		t.Fatalf("ValidarFS(migrations.FS) erro = %v", err)
	}
	if len(ms) == 0 || ms[0].Versao != 1 || ms[0].Nome != "esquema_emprestimo" {
		t.Fatalf("migrations.FS = %+v, want começar em 1 esquema_emprestimo", ms)
	}
	if !regexp.MustCompile(`^[0-9a-f]{64}$`).MatchString(ms[0].Checksum) {
		t.Errorf("checksum %q, want 64 hex minúsculos", ms[0].Checksum)
	}
}

// FuzzValidarFSNome varia só o miolo do nome de um par 0001_<s>.{up,down}.sql.
// O par é válido se, e somente se, s casa com [a-z0-9_]+.
func FuzzValidarFSNome(f *testing.F) {
	for _, s := range []string{"a", "esquema_emprestimo", "", "A", "a-b", "a b", "ação", "a/b", "..", "a\n", "x.up", "0", "_", "a.down", "a\x00b"} {
		f.Add(s)
	}
	reNome := regexp.MustCompile(`^[a-z0-9_]+$`)
	f.Fuzz(func(t *testing.T, s string) {
		fsys := fstest.MapFS{
			"0001_" + s + ".up.sql":   &fstest.MapFile{Data: []byte("SELECT 1;")},
			"0001_" + s + ".down.sql": &fstest.MapFile{Data: []byte("SELECT 1;")},
		}
		got, err := migrate.ValidarFS(fsys)
		if want := reNome.MatchString(s); (err == nil) != want {
			t.Fatalf("nome %q: erro = %v, válido esperado = %v", s, err, want)
		}
		if err != nil {
			if !errors.Is(err, migrate.ErroFSInvalido) {
				t.Fatalf("erro %v, want ErroFSInvalido", err)
			}
			return
		}
		if len(got) != 1 || got[0].Versao != 1 || got[0].Nome != s {
			t.Fatalf("ValidarFS() = %+v", got)
		}
	})
}

// FuzzValidarFS monta um FS com nomes arbitrários separados por '|'. Sem
// pânico; em sucesso, o FS é exatamente os pares 0001..000N, em ordem.
func FuzzValidarFS(f *testing.F) {
	f.Add("0001_a.up.sql|0001_a.down.sql")
	f.Add("0001_a.up.sql|0001_a.down.sql|0002_b.up.sql|0002_b.down.sql")
	f.Add("0002_a.up.sql|0002_a.down.sql")
	f.Add("0001_a.up.sql|0001_b.up.sql|0001_a.down.sql|0001_b.down.sql")
	f.Add("0001_a.up.sql|0001_a.down.sql|0003_c.up.sql|0003_c.down.sql")
	f.Add("")
	f.Add("0001_a.up.sql|0001_a.down.sql|x/0002_b.up.sql")
	f.Add("0001_a.up.sql|0001_a.down.sql|/|//|..")
	f.Fuzz(func(t *testing.T, lista string) {
		fsys := fstest.MapFS{}
		for _, n := range strings.Split(lista, "|") {
			if !fs.ValidPath(n) || n == "." {
				continue
			}
			fsys[n] = &fstest.MapFile{Data: []byte("SELECT 1;")}
		}
		got, err := migrate.ValidarFS(fsys)
		if err != nil {
			if !errors.Is(err, migrate.ErroFSInvalido) {
				t.Fatalf("erro %v, want ErroFSInvalido", err)
			}
			return
		}
		if len(got) == 0 {
			t.Fatal("ValidarFS() = vazio sem erro")
		}
		if len(fsys) != 2*len(got) {
			t.Fatalf("FS com %d arquivos aceito com %d migrações", len(fsys), len(got))
		}
		for i, m := range got {
			if m.Versao != i+1 {
				t.Fatalf("migração %d com versão %d, want %d", i, m.Versao, i+1)
			}
			if _, ok := fsys[fmt.Sprintf("%04d_%s.up.sql", m.Versao, m.Nome)]; !ok {
				t.Fatalf("migração %+v sem o .up.sql correspondente", m)
			}
			if _, ok := fsys[fmt.Sprintf("%04d_%s.down.sql", m.Versao, m.Nome)]; !ok {
				t.Fatalf("migração %+v sem o .down.sql correspondente", m)
			}
		}
	})
}

// ---- TemConcurrently ----

func TestTemConcurrently(t *testing.T) {
	tests := []struct {
		name string
		sql  string
		want bool
	}{
		{name: "create index", sql: "CREATE INDEX CONCURRENTLY ix ON t (c);", want: true},
		{name: "minusculas", sql: "create index concurrently ix on t (c);", want: true},
		{name: "capitalizacao mista", sql: "CoNcUrReNtLy", want: true},
		{name: "so a palavra", sql: "concurrently", want: true},
		{name: "com ponto e virgula", sql: "DROP INDEX CONCURRENTLY;", want: true},
		{name: "reindex", sql: "REINDEX (VERBOSE) INDEX CONCURRENTLY ix;", want: true},
		{name: "refresh materialized view", sql: "REFRESH MATERIALIZED VIEW CONCURRENTLY mv;", want: true},
		{name: "comentario de linha", sql: "-- CONCURRENTLY\nSELECT 1;", want: true},
		{name: "comentario de bloco", sql: "SELECT 1; /* usar CONCURRENTLY depois */", want: true},
		{name: "dentro de literal", sql: "SELECT 'concurrently';", want: true},
		{name: "separado por tab e quebra de linha", sql: "CREATE INDEX\tCONCURRENTLY\nix ON t (c);", want: true},
		{name: "no fim sem quebra de linha", sql: "SELECT 1;\n-- concurrently", want: true},
		{name: "no inicio", sql: "concurrently SELECT 1;", want: true},
		{name: "entre aspas duplas", sql: `CREATE INDEX "concurrently" ON t (c);`, want: true},
		{name: "vazio", sql: "", want: false},
		{name: "sem a palavra", sql: "CREATE INDEX ix ON t (c);", want: false},
		{name: "concurrent", sql: "concurrent", want: false},
		{name: "truncada", sql: "concurrentl", want: false},
		{name: "partida em duas", sql: "concur rently", want: false},
		{name: "partida por hifen", sql: "concur-rently", want: false},
		{name: "com sufixo alfabetico", sql: "concurrentlyx", want: false},
		{name: "com prefixo alfabetico", sql: "xconcurrently", want: false},
		{name: "parte de outra palavra", sql: "nonconcurrentlyish", want: false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := migrate.TemConcurrently(tt.sql); got != tt.want {
				t.Errorf("TemConcurrently(%q) = %v, want %v", tt.sql, got, tt.want)
			}
		})
	}
}

// FuzzTemConcurrently: sem pânico e duas propriedades que valem para qualquer
// definição razoável de "palavra": (1) sem a substring (ignorando caixa) não há
// detecção; (2) a palavra isolada por ASCII não alfanumérico e diferente de '_'
// é sempre detectada.
func FuzzTemConcurrently(f *testing.F) {
	for _, s := range []string{
		"", "CREATE INDEX CONCURRENTLY ix ON t (c);", "-- concurrently", "/* CONCURRENTLY */", "concurrentl",
		"xconcurrentlyx", "CoNcUrReNtLy;", "\nconcurrently\n", "concurrently_", "\xff\xfeconcurrently",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, sql string) {
		got := migrate.TemConcurrently(sql)
		if !strings.Contains(strings.ToLower(sql), "concurrently") && got {
			t.Fatalf("TemConcurrently(%q) = true sem a substring", sql)
		}
		if palavraIsolada(sql) && !got {
			t.Fatalf("TemConcurrently(%q) = false com a palavra isolada", sql)
		}
	})
}

func asciiLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 'a' - 'A'
		}
	}
	return string(b)
}

// separadorASCII: byte ASCII que não é letra, dígito nem '_'.
func separadorASCII(c byte) bool {
	if c >= 0x80 {
		return false
	}
	alnum := (c >= '0' && c <= '9') || (c >= 'a' && c <= 'z') || (c >= 'A' && c <= 'Z')
	return !alnum && c != '_'
}

// palavraIsolada: "concurrently" (qualquer caixa) cercado por início/fim ou
// por separadores ASCII.
func palavraIsolada(s string) bool {
	const p = "concurrently"
	b := asciiLower(s)
	for i := 0; ; {
		j := strings.Index(b[i:], p)
		if j < 0 {
			return false
		}
		j += i
		antes := j == 0 || separadorASCII(b[j-1])
		depois := j+len(p) == len(b) || separadorASCII(b[j+len(p)])
		if antes && depois {
			return true
		}
		i = j + 1
	}
}
