package storage

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
)

// DEL-015 (D-21, handoff 03 do data-model): plano das 16 combinações de
// ListarEmprestimos com os índices da 0003. O texto SQL vem da função real
// (sqlListarEmprestimos), não de cópia.
//
// Divergência registrada no handoff do test-writer: com a massa do handoff 03
// (199 usuários, 2000 livros, 20000 empréstimos) cada usuário tem ~100 e cada
// livro ~10 empréstimos, menos que limite+1, e o PostgreSQL 16 escolhe, com
// razão de custo, Bitmap Heap Scan + Sort nos índices simples para 5-9 e 12.
// A massa foi trocada por proporções em que o keyset por índice composto
// importa (cada usuário ~250 e cada livro ~400 empréstimos), mantendo as
// asserções do handoff para 1-5 e 8-12. Nas combinações com duas condições
// seletivas e independentes (usuário + aberto/vencido e usuário + livro: 6, 7,
// 13-16) o planner prefere a interseção de bitmaps (BitmapAnd), limitada pela
// interseção; ali o teste aceita esse plano além dos Index Scans permitidos.

// Índices de emprestimo.
const (
	idxPK          = "emprestimo_pkey"
	idxAberto      = "emprestimo_aberto_id_idx"
	idxUsuarioID   = "emprestimo_usuario_id_id_idx"
	idxLivroID     = "emprestimo_livro_id_id_idx"
	idxLivroAberto = "emprestimo_livro_aberto_uq"
	idxUsuario     = "emprestimo_usuario_id_idx"
	idxLivro       = "emprestimo_livro_id_idx"
)

// Ids da fixture do plano: o usuário 7 tem ~250 empréstimos; o livro 151
// tem 400, um deles aberto.
const (
	usuarioPlano int64 = 7
	livroPlano   int64 = 151
)

// fixturePlano grava a massa do plano por SQL direto e roda ANALYZE: 799
// usuários e 500 livros (coprimos: usuário e livro não correlacionados),
// 200000 empréstimos; 499 abertos, um por livro (o livro 1 não tem),
// espalhados pela tabela (não contíguos no heap); vencimentos em 60 dias a
// partir de 01/02/2026 (cerca de metade dos abertos vencida em 01/03/2026).
func fixturePlano(t *testing.T) *pgx.Conn {
	t.Helper()
	_, c := novoBancoMigrado(t)
	for _, sql := range []string{
		`INSERT INTO usuario (nome) SELECT 'u' || g FROM generate_series(1, 799) g`,
		`INSERT INTO livro (titulo) SELECT 'l' || g FROM generate_series(1, 500) g`,
		`INSERT INTO emprestimo (livro_id, usuario_id, inicio, vencimento, devolucao)
		 SELECT 1 + g % 500, 1 + g % 799,
		        timestamptz '2026-01-01 12:00Z' + g * interval '10 second',
		        date '2026-02-01' + g % 60,
		        CASE WHEN g / 500 = ((g % 500) * 397) % 400 THEN NULL
		             ELSE timestamptz '2026-03-01 12:00Z' END
		   FROM generate_series(1, 200000) g`,
		`ANALYZE usuario, livro, emprestimo`,
	} {
		if _, err := c.Exec(t.Context(), sql); err != nil {
			t.Fatalf("fixture do plano: %v", err)
		}
	}
	var abertos, livrosAbertos int
	if err := c.QueryRow(t.Context(), `SELECT count(*), count(DISTINCT livro_id) FROM emprestimo WHERE devolucao IS NULL`).Scan(&abertos, &livrosAbertos); err != nil {
		t.Fatalf("conferir fixture: %v", err)
	}
	if abertos != 499 || livrosAbertos != 499 {
		t.Fatalf("fixture com %d abertos em %d livros, want 499 e 499", abertos, livrosAbertos)
	}
	return c
}

// noPlano é um nó do EXPLAIN (FORMAT JSON).
type noPlano struct {
	NodeType     string    `json:"Node Type"`
	RelationName string    `json:"Relation Name"`
	IndexName    string    `json:"Index Name"`
	IndexCond    string    `json:"Index Cond"`
	Filter       string    `json:"Filter"`
	Plans        []noPlano `json:"Plans"`
}

func (n noPlano) String() string {
	var b strings.Builder
	var rec func(n noPlano, nivel int)
	rec = func(n noPlano, nivel int) {
		fmt.Fprintf(&b, "%s%s %s %s cond=%q filter=%q\n", strings.Repeat("  ", nivel), n.NodeType, n.RelationName, n.IndexName, n.IndexCond, n.Filter)
		for _, f := range n.Plans {
			rec(f, nivel+1)
		}
	}
	rec(n, 0)
	return b.String()
}

func lerPlano(t *testing.T, raw []byte) noPlano {
	t.Helper()
	var out []struct {
		Plan noPlano `json:"Plan"`
	}
	if err := json.Unmarshal(raw, &out); err != nil || len(out) != 1 {
		t.Fatalf("EXPLAIN JSON inválido (%v): %s", err, raw)
	}
	return out[0].Plan
}

func nosDoPlano(n noPlano, acc *[]noPlano) {
	*acc = append(*acc, n)
	for _, f := range n.Plans {
		nosDoPlano(f, acc)
	}
}

// planoGenerico roda EXPLAIN (GENERIC_PLAN, FORMAT JSON) sem argumentos pelo
// protocolo simples (o plano que o cache de prepared statement do pgx usa
// depois das primeiras execuções). PostgreSQL 16+.
func planoGenerico(t *testing.T, c *pgx.Conn, sql string) noPlano {
	t.Helper()
	res, err := c.PgConn().Exec(t.Context(), "EXPLAIN (GENERIC_PLAN, FORMAT JSON) "+sql).ReadAll()
	if err != nil {
		t.Fatalf("EXPLAIN GENERIC_PLAN: %v", err)
	}
	if len(res) != 1 || len(res[0].Rows) != 1 || len(res[0].Rows[0]) != 1 {
		t.Fatalf("EXPLAIN GENERIC_PLAN: resultado inesperado %+v", res)
	}
	return lerPlano(t, res[0].Rows[0][0])
}

// planoCustom roda EXPLAIN (FORMAT JSON) com os argumentos reais.
func planoCustom(t *testing.T, c *pgx.Conn, sql string, args ...any) noPlano {
	t.Helper()
	var raw []byte
	if err := c.QueryRow(t.Context(), "EXPLAIN (FORMAT JSON) "+sql, args...).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN com argumentos: %v", err)
	}
	return lerPlano(t, raw)
}

// esperado é o plano aceito para uma combinação.
type esperado struct {
	// indices são os índices aceitos num Index Scan sobre emprestimo.
	indices []string
	// intersecao aceita também Bitmap Heap Scan sobre um BitmapAnd de dois
	// ou mais Bitmap Index Scans em índices de emprestimo (e Sort acima).
	intersecao bool
}

// checarPlano aplica as asserções do handoff 03 a um plano: exatamente um
// nó de scan em emprestimo; nenhum Seq Scan; Index Scan num índice aceito,
// com o keyset (id >) no Index Cond e sem Sort (salvo sobre o uq) e sem os
// índices simples; Bitmap só como interseção, e só onde aceita.
func checarPlano(t *testing.T, p noPlano, e esperado) {
	t.Helper()
	var nos []noPlano
	nosDoPlano(p, &nos)
	var scans []noPlano
	temSort := false
	for _, n := range nos {
		if n.NodeType == "Seq Scan" {
			t.Errorf("Seq Scan em %s\n%s", n.RelationName, p)
		}
		if strings.Contains(n.NodeType, "Sort") {
			temSort = true
		}
		if n.RelationName == "emprestimo" {
			scans = append(scans, n)
		}
	}
	if len(scans) != 1 {
		t.Fatalf("%d nós de scan em emprestimo, want 1\n%s", len(scans), p)
	}
	s := scans[0]
	switch s.NodeType {
	case "Index Scan":
		aceito := false
		for _, w := range e.indices {
			aceito = aceito || s.IndexName == w
		}
		if !aceito {
			t.Errorf("Index Scan em %q, want um de %v\n%s", s.IndexName, e.indices, p)
		}
		if s.IndexName != idxLivroAberto {
			if !strings.Contains(s.IndexCond, "(id > ") {
				t.Errorf("Index Cond = %q, want com (id > ...) (keyset no índice)\n%s", s.IndexCond, p)
			}
			if temSort {
				t.Errorf("Sort no plano com %s: o índice já entrega a ordem do keyset\n%s", s.IndexName, p)
			}
		}
		for _, n := range nos {
			if strings.HasPrefix(n.NodeType, "Bitmap") {
				t.Errorf("nó %s num plano de Index Scan\n%s", n.NodeType, p)
			}
		}
	case "Bitmap Heap Scan":
		if !e.intersecao {
			t.Errorf("Bitmap Heap Scan, want Index Scan em %v\n%s", e.indices, p)
			return
		}
		if len(s.Plans) != 1 || s.Plans[0].NodeType != "BitmapAnd" || len(s.Plans[0].Plans) < 2 {
			t.Errorf("Bitmap Heap Scan sem BitmapAnd de 2+ índices (sem interseção, o heap é varrido por um filtro só)\n%s", p)
			return
		}
		conhecidos := map[string]bool{idxPK: true, idxAberto: true, idxUsuarioID: true, idxLivroID: true, idxLivroAberto: true, idxUsuario: true, idxLivro: true}
		for _, f := range s.Plans[0].Plans {
			if f.NodeType != "Bitmap Index Scan" || !conhecidos[f.IndexName] {
				t.Errorf("folha do BitmapAnd = %s %s, want Bitmap Index Scan num índice de emprestimo\n%s", f.NodeType, f.IndexName, p)
			}
		}
	default:
		t.Errorf("scan em emprestimo = %s, want Index Scan\n%s", s.NodeType, p)
	}
	if s.NodeType == "Index Scan" {
		for _, proibido := range []string{idxUsuario, idxLivro} {
			if s.IndexName == proibido {
				t.Errorf("Index Scan no índice simples %s\n%s", proibido, p)
			}
		}
	}
}

// planoEsperado é o plano aceito por combinação (handoff 03,
// PADROES_DE_ACESSO). 1-5, 8, 9 e 12 são índice único, como no handoff.
func planoEsperado(nome string) esperado {
	switch nome[:2] {
	case "01", "04":
		return esperado{indices: []string{idxPK}}
	case "02", "03":
		return esperado{indices: []string{idxAberto}}
	case "05", "08":
		return esperado{indices: []string{idxUsuarioID}}
	case "06", "07":
		// O parcial de abertos também entrega a ordem do keyset e é limitado
		// pelo número de livros (emprestimo_livro_aberto_uq).
		return esperado{indices: []string{idxUsuarioID, idxAberto}, intersecao: true}
	case "09", "12":
		return esperado{indices: []string{idxLivroID}}
	case "10", "11":
		return esperado{indices: []string{idxLivroAberto, idxLivroID}}
	case "13", "16":
		return esperado{indices: []string{idxLivroID, idxUsuarioID}, intersecao: true}
	case "14", "15":
		return esperado{indices: []string{idxLivroAberto, idxLivroID, idxUsuarioID, idxAberto}, intersecao: true}
	}
	panic("combinação desconhecida: " + nome)
}

// filtroPlano troca os valores marcados das combinações pelos ids da
// fixture do plano e Ref por 01/03/2026.
func filtroPlano(f FiltroEmprestimos) FiltroEmprestimos {
	if f.UsuarioID != 0 {
		f.UsuarioID = usuarioPlano
	}
	if f.LivroID != 0 {
		f.LivroID = livroPlano
	}
	f.Ref = time.Date(2026, 3, 1, 12, 0, 0, 0, time.UTC)
	return f
}

// TestPlanoListarEmprestimos: para cada combinação, o plano genérico e o
// custom (apos 0 e 15000, limite+1 = 101) seguem planoEsperado.
func TestPlanoListarEmprestimos(t *testing.T) {
	c := fixturePlano(t)
	var versao int
	if err := c.QueryRow(t.Context(), "SELECT current_setting('server_version_num')::int").Scan(&versao); err != nil {
		t.Fatalf("versão do servidor: %v", err)
	}
	if versao < 160000 {
		t.Fatalf("PostgreSQL %d sem EXPLAIN (GENERIC_PLAN); o CI usa 16", versao)
	}
	for _, comb := range combinacoesFiltro() {
		filtro := filtroPlano(comb.filtro)
		sql, extras := sqlListarEmprestimos(filtro)
		e := planoEsperado(comb.name)
		t.Run(comb.name+"/genérico", func(t *testing.T) {
			checarPlano(t, planoGenerico(t, c, sql), e)
		})
		for _, apos := range []int64{0, 15000} {
			t.Run(fmt.Sprintf("%s/custom apos=%d", comb.name, apos), func(t *testing.T) {
				args := append([]any{apos, 101}, extras...)
				checarPlano(t, planoCustom(t, c, sql, args...), e)
			})
		}
	}

	// Regressão da combinação 1, com a mesma massa: o SQL é o de antes, byte
	// a byte, e o plano continua na PK.
	t.Run("sem filtro: SQL e plano de antes", func(t *testing.T) {
		sql, extras := sqlListarEmprestimos(FiltroEmprestimos{})
		if sql != sqlSemFiltro || len(extras) != 0 {
			t.Fatalf("SQL = %q %v, want %q sem extras", sql, extras, sqlSemFiltro)
		}
		for _, p := range []noPlano{planoGenerico(t, c, sql), planoCustom(t, c, sql, int64(0), 101), planoCustom(t, c, sql, int64(15000), 101)} {
			checarPlano(t, p, esperado{indices: []string{idxPK}})
		}
	})
}
