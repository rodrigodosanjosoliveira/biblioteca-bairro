package storage

import (
	"fmt"
	"strings"
	"testing"

	"github.com/jackc/pgx/v5"
)

// DEL-017 (CA5): plano de BuscarLivro e ListarLivros com a massa de
// fixturePlano (500 livros, 200000 empréstimos, 499 abertos, um por livro;
// o livro 1 sem aberto). O texto vem das constantes reais (sqlBuscarLivro e
// sqlListarLivros), não de cópia.
//
// Plano exigido, genérico e custom: Index Scan em livro_pkey no livro;
// disponibilidade por SubPlan correlacionado, uma sonda por livro
// (Index Scan ou Index Only Scan em emprestimo_livro_aberto_uq com Index
// Cond em livro_id = l.id, só Limit entre o SubPlan e o scan); sem hashed
// SubPlan (que lê todos os abertos de uma vez, sem correlação) e sem Seq
// Scan. Medido no handoff 05: NOT EXISTS vira hashed SubPlan na listagem
// (convert_EXISTS_to_ANY); a subconsulta escalar com LIMIT 1 (handoff 06,
// F1) não. O hashed não aparece no JSON sem VERBOSE (Subplan Name fica
// "SubPlan N"), por isso o EXPLAIN aqui é VERBOSE e a asserção lê Output e
// Index Cond.

// planoVerbose roda EXPLAIN (VERBOSE, FORMAT JSON), genérico sem argumentos
// pelo protocolo simples ou custom com os argumentos reais.
func planoVerbose(t *testing.T, c *pgx.Conn, generico bool, sql string, args ...any) noPlano {
	t.Helper()
	if generico {
		res, err := c.PgConn().Exec(t.Context(), "EXPLAIN (GENERIC_PLAN, VERBOSE, FORMAT JSON) "+sql).ReadAll()
		if err != nil {
			t.Fatalf("EXPLAIN GENERIC_PLAN: %v", err)
		}
		if len(res) != 1 || len(res[0].Rows) != 1 || len(res[0].Rows[0]) != 1 {
			t.Fatalf("EXPLAIN GENERIC_PLAN: resultado inesperado %+v", res)
		}
		return lerPlano(t, res[0].Rows[0][0])
	}
	var raw []byte
	if err := c.QueryRow(t.Context(), "EXPLAIN (VERBOSE, FORMAT JSON) "+sql, args...).Scan(&raw); err != nil {
		t.Fatalf("EXPLAIN com argumentos: %v", err)
	}
	return lerPlano(t, raw)
}

// checarPlanoLivro aplica as asserções do CA5 a um plano de leitura de livro.
func checarPlanoLivro(t *testing.T, p noPlano) {
	t.Helper()
	var nos []noPlano
	nosDoPlano(p, &nos)
	var livros, emps []noPlano
	for _, n := range nos {
		if n.NodeType == "Seq Scan" {
			t.Errorf("Seq Scan em %s\n%s", n.RelationName, p)
		}
		if strings.HasPrefix(n.NodeType, "Bitmap") {
			t.Errorf("nó %s: want Index Scan\n%s", n.NodeType, p)
		}
		if strings.HasPrefix(n.SubplanName, "hashed") {
			t.Errorf("hashed SubPlan %q: want SubPlan correlacionado (uma sonda por livro)\n%s", n.SubplanName, p)
		}
		for _, o := range n.Output {
			if strings.Contains(o, "hashed SubPlan") {
				t.Errorf("Output %q usa hashed SubPlan: want SubPlan correlacionado (uma sonda por livro)\n%s", o, p)
			}
		}
		switch n.RelationName {
		case "livro":
			livros = append(livros, n)
		case "emprestimo":
			emps = append(emps, n)
		}
	}
	if len(livros) != 1 {
		t.Fatalf("%d nós de scan em livro, want 1\n%s", len(livros), p)
	}
	if l := livros[0]; l.NodeType != "Index Scan" || l.IndexName != "livro_pkey" {
		t.Errorf("livro lido por %s %q, want Index Scan em livro_pkey\n%s", l.NodeType, l.IndexName, p)
	}
	if len(emps) != 1 {
		t.Fatalf("%d nós de scan em emprestimo, want 1 (uma subconsulta)\n%s", len(emps), p)
	}
	e := emps[0]
	// A sonda fica dentro de um SubPlan, com no máximo Limit entre a raiz do
	// SubPlan e o scan (o LIMIT 1 da subconsulta escalar), sem JOIN.
	if cadeia, ok := caminhoSubPlan(p, nil); !ok {
		t.Errorf("scan de emprestimo fora de SubPlan (JOIN?)\n%s", p)
	} else {
		for _, n := range cadeia[:len(cadeia)-1] {
			if n.NodeType != "Limit" {
				t.Errorf("nó %s entre o SubPlan e o scan de emprestimo, want só Limit\n%s", n.NodeType, p)
			}
		}
	}
	if e.NodeType != "Index Scan" && e.NodeType != "Index Only Scan" {
		t.Errorf("emprestimo lido por %s, want Index Scan ou Index Only Scan\n%s", e.NodeType, p)
	}
	if e.IndexName != idxLivroAberto {
		t.Errorf("emprestimo lido por %q, want %s\n%s", e.IndexName, idxLivroAberto, p)
	}
	if !strings.Contains(e.IndexCond, "livro_id = l.id") {
		t.Errorf("Index Cond = %q, want a sonda correlacionada (livro_id = l.id)\n%s", e.IndexCond, p)
	}
	if e.Filter != "" {
		t.Errorf("Filter %q no scan de emprestimo: o predicado do índice parcial deveria bastar\n%s", e.Filter, p)
	}
}

// caminhoSubPlan devolve a cadeia de nós da raiz de um SubPlan (Parent
// Relationship = SubPlan) até o scan de emprestimo, inclusive; ok falso se o
// scan de emprestimo não estiver dentro de um SubPlan.
func caminhoSubPlan(n noPlano, cadeia []noPlano) ([]noPlano, bool) {
	if n.ParentRelationship == "SubPlan" {
		cadeia = []noPlano{}
	}
	if cadeia != nil {
		cadeia = append(append([]noPlano{}, cadeia...), n)
	}
	if n.RelationName == "emprestimo" {
		return cadeia, cadeia != nil
	}
	for _, f := range n.Plans {
		if c, ok := caminhoSubPlan(f, cadeia); ok || c != nil {
			return c, ok
		}
	}
	return nil, false
}

// TestPlanoLeituraLivro: plano genérico e custom do detalhe (livro 1 sem
// aberto, livro 151 com aberto, inexistente) e da listagem (início, meio e
// fim do acervo, limite+1 = 101 e 2).
func TestPlanoLeituraLivro(t *testing.T) {
	c := fixturePlano(t)
	var versao int
	if err := c.QueryRow(t.Context(), "SELECT current_setting('server_version_num')::int").Scan(&versao); err != nil {
		t.Fatalf("versão do servidor: %v", err)
	}
	if versao < 160000 {
		t.Fatalf("PostgreSQL %d sem EXPLAIN (GENERIC_PLAN); o CI usa 16", versao)
	}

	tests := []struct {
		name     string
		sql      string
		generico bool
		args     []any
	}{
		{name: "detalhe/genérico", sql: sqlBuscarLivro, generico: true},
		{name: "detalhe/custom livro 1 (sem aberto)", sql: sqlBuscarLivro, args: []any{int64(1)}},
		{name: "detalhe/custom livro 151 (com aberto)", sql: sqlBuscarLivro, args: []any{livroPlano}},
		{name: "detalhe/custom inexistente", sql: sqlBuscarLivro, args: []any{int64(9999)}},
		{name: "listagem/genérico", sql: sqlListarLivros, generico: true},
		{name: "listagem/custom apos=0 limite+1=101", sql: sqlListarLivros, args: []any{int64(0), 101}},
		{name: "listagem/custom apos=0 limite+1=21", sql: sqlListarLivros, args: []any{int64(0), 21}},
		{name: "listagem/custom apos=250 limite+1=101", sql: sqlListarLivros, args: []any{int64(250), 101}},
		{name: "listagem/custom apos=450 limite+1=101", sql: sqlListarLivros, args: []any{int64(450), 101}},
		{name: "listagem/custom apos=0 limite+1=2", sql: sqlListarLivros, args: []any{int64(0), 2}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checarPlanoLivro(t, planoVerbose(t, c, tt.generico, tt.sql, tt.args...))
		})
	}

	// O plano examinado é o da consulta que devolve o valor certo na mesma
	// massa: livro 1 disponível, os demais não.
	t.Run("valores na massa do plano", func(t *testing.T) {
		r := NovoRepositorio(c)
		for _, tt := range []struct {
			id   int64
			want bool
		}{{1, true}, {2, false}, {livroPlano, false}, {500, false}} {
			got, err := r.BuscarLivro(t.Context(), tt.id)
			if err != nil || got.Disponivel != tt.want {
				t.Errorf("BuscarLivro(%d) = %+v, %v; want Disponivel %v", tt.id, got, err, tt.want)
			}
		}
		pagina, temMais, err := r.ListarLivros(t.Context(), 0, LimiteListagemLivros)
		if err != nil || len(pagina) != LimiteListagemLivros || !temMais {
			t.Fatalf("ListarLivros = %d itens, %v, %v", len(pagina), temMais, err)
		}
		for i, it := range pagina {
			if want := it.Livro.ID == 1; it.Disponivel != want || it.Livro.ID != int64(i+1) {
				t.Errorf("item %d = %+v, want id %d Disponivel %v", i, it, i+1, want)
			}
		}
	})

	// O campo ParentRelationship do noPlano é decodificado do EXPLAIN: no
	// plano de sqlBuscarLivro (livro 1), algum nó traz Parent Relationship
	// "SubPlan". Sem isso, a exigência de SubPlan em checarPlanoLivro passaria
	// a depender de um campo sempre vazio.
	t.Run("noPlano lê SubPlan", func(t *testing.T) {
		p := planoVerbose(t, c, false, sqlBuscarLivro, int64(1))
		var nos []noPlano
		nosDoPlano(p, &nos)
		achou := false
		for _, n := range nos {
			achou = achou || n.ParentRelationship == "SubPlan"
		}
		if !achou {
			t.Errorf("nenhum nó com Parent Relationship SubPlan\n%s", fmt.Sprint(p))
		}
	})
}
