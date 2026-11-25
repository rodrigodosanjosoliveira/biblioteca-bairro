package storage

import (
	"strings"
	"testing"
)

// TestSQLLeituraLivro (DEL-017, D-21, handoff 06 do architecture-guardrails):
// o texto de BuscarLivro e de ListarLivros é constante e deriva a
// disponibilidade por subconsulta escalar na projeção,
// `(SELECT 1 FROM emprestimo e WHERE e.livro_id = l.id AND e.devolucao IS
// NULL LIMIT 1) IS NULL AS disponivel`, com FROM externo só de livro.
//
// EXISTS/NOT EXISTS, IN e = ANY com subconsulta são proibidos porque o
// planner os converte em ANY hasheável (convert_EXISTS_to_ANY): medido no
// Postgres 16 (handoff 05), NOT EXISTS na listagem virou hashed SubPlan, que
// lê todos os empréstimos abertos a cada página em vez de uma sonda por
// livro em emprestimo_livro_aberto_uq (CA5). A subconsulta escalar não tem
// forma hashed. devolucao IS NULL fica literal (o índice parcial precisa
// casar também no plano genérico); o NULL vira bool por IS NULL, sem
// COALESCE; nada de relógio, vencimento, JOIN, LATERAL nem agregação.
func TestSQLLeituraLivro(t *testing.T) {
	tests := []struct {
		name      string
		sql       string
		exige     []string
		proibe    []string
		maxParams int
		nLimit    int // ocorrências de "limit"
	}{
		{name: "listagem", sql: sqlListarLivros, maxParams: 2, nLimit: 2,
			exige: []string{"from livro l", "l.id > $1", "order by l.id", "limit $2"}},
		{name: "detalhe", sql: sqlBuscarLivro, maxParams: 1, nLimit: 1,
			exige:  []string{"from livro l", "l.id = $1"},
			proibe: []string{"limit $", "order by"}},
	}
	comuns := []string{
		"(select 1 from emprestimo e", "e.livro_id = l.id", "e.devolucao is null",
		"limit 1) is null", "as disponivel",
	}
	proibidosComuns := []string{
		"exists", " in (", " in(", "any(", "any (", "is not null",
		"now()", "current_date", "current_timestamp", "localtimestamp", "localtime", "at time zone",
		"vencimento", "coalesce", " or ", "join", "distinct", "lateral", "bool_or", "count(",
		"group by", "offset", "materialized", "with ", "enable_", ";", "::",
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			baixo := strings.ToLower(strings.Join(strings.Fields(tt.sql), " "))
			baixo = strings.ReplaceAll(strings.ReplaceAll(baixo, "( ", "("), " )", ")")
			for _, w := range append(append([]string{}, comuns...), tt.exige...) {
				if !strings.Contains(baixo, w) {
					t.Errorf("SQL sem %q: %s", w, tt.sql)
				}
			}
			for _, p := range append(append([]string{}, proibidosComuns...), tt.proibe...) {
				if strings.Contains(baixo, p) {
					t.Errorf("SQL contém %q: %s", p, tt.sql)
				}
			}
			// Exatamente um LIMIT 1 interno (e, na listagem, um LIMIT $2).
			if n := strings.Count(baixo, "limit"); n != tt.nLimit {
				t.Errorf("%d LIMIT, want %d: %s", n, tt.nLimit, tt.sql)
			}
			if n := strings.Count(baixo, "limit 1)"); n != 1 {
				t.Errorf("%d LIMIT 1 interno, want 1: %s", n, tt.sql)
			}
			// devolucao IS NULL literal, nunca parametrizado.
			if strings.Contains(baixo, "devolucao = $") {
				t.Errorf("predicado de aberto parametrizado: %s", tt.sql)
			}
			// Uma só tabela na consulta externa: o FROM de fora é só livro.
			if strings.Count(baixo, " from ") != 2 || strings.Count(baixo, "select") != 2 {
				t.Errorf("want exatamente duas consultas (livro e a subconsulta escalar de emprestimo): %s", tt.sql)
			}
			for n := tt.maxParams + 1; n <= 9; n++ {
				if strings.Contains(baixo, "$"+string(rune('0'+n))) {
					t.Errorf("SQL com parâmetro $%d além do contrato: %s", n, tt.sql)
				}
			}
		})
	}
	if sqlListarLivros == sqlBuscarLivro {
		t.Error("listagem e detalhe com o mesmo texto")
	}
}
