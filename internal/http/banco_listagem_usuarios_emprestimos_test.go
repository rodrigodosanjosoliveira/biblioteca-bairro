package httpapi_test

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// percorrer segue proximo a partir de base?limite=N até proximo null e
// devolve os itens visitados, conferindo que proximo é o id do último item
// entregue e que nenhuma página passa do limite.
func percorrer(t *testing.T, h http.Handler, base string, limite, maxPaginas int) []map[string]any {
	t.Helper()
	var visitados []map[string]any
	path := fmt.Sprintf("%s?limite=%d", base, limite)
	for pag := 0; ; pag++ {
		if pag > maxPaginas {
			t.Fatalf("paginação não termina: %d itens", len(visitados))
		}
		itens, proximo := itensJSON(t, exigirCorpo(t, chamar(h, http.MethodGet, path, "")))
		if len(itens) > limite {
			t.Fatalf("%d itens, want no máximo %d", len(itens), limite)
		}
		visitados = append(visitados, itens...)
		if proximo == "" {
			return visitados
		}
		if len(itens) == 0 || proximo != idDe(t, itens[len(itens)-1]) {
			t.Fatalf("proximo = %s, want id do último item entregue", proximo)
		}
		path = fmt.Sprintf("%s?limite=%d&apos=%s", base, limite, proximo)
	}
}

func idsDosItens(t *testing.T, itens []map[string]any) string {
	t.Helper()
	ids := make([]string, 0, len(itens))
	for _, it := range itens {
		ids = append(ids, idDe(t, it))
	}
	return strings.Join(ids, ",")
}

// TestBancoListagemUsuarios percorre GET /usuarios no banco real e confere
// GET /usuarios/{id} (200 com o nome aparado, 404 usuario_inexistente).
func TestBancoListagemUsuarios(t *testing.T) {
	pool := novoPool(t, 4)
	rel := &relogio{t: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)}
	h := handlerBanco(pool, rel, 5*time.Second)

	var ids []string
	nomes := map[string]string{}
	for i := 0; i < 5; i++ {
		id := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/usuarios", fmt.Sprintf(`{"nome":"  Usuario %d "}`, i)), 201))
		ids = append(ids, id)
		nomes[id] = fmt.Sprintf("Usuario %d", i)
	}
	todos := strings.Join(ids, ",")

	for _, limite := range []int{1, 2, 3, 4, 5, 6, 100} {
		t.Run(fmt.Sprintf("percorrer com limite %d", limite), func(t *testing.T) {
			itens := percorrer(t, h, "/usuarios", limite, len(ids)+1)
			if got := idsDosItens(t, itens); got != todos {
				t.Errorf("visitados = %s, want %s", got, todos)
			}
			for _, it := range itens {
				exigirChaves(t, it, "id", "nome")
				if id := idDe(t, it); it["nome"] != nomes[id] {
					t.Errorf("usuario %s nome = %#v, want %q", id, it["nome"], nomes[id])
				}
			}
		})
	}
	t.Run("página exata tem proximo null", func(t *testing.T) {
		itens, proximo := itensJSON(t, exigirCorpo(t, chamar(h, http.MethodGet, "/usuarios?limite=2&apos="+ids[2], "")))
		if got := idsDosItens(t, itens); got != ids[3]+","+ids[4] || proximo != "" {
			t.Errorf("ids = %s proximo = %q, want %s,%s e null", got, proximo, ids[3], ids[4])
		}
	})
	t.Run("apos além do último", func(t *testing.T) {
		rec := chamar(h, http.MethodGet, "/usuarios?apos=9223372036854775807", "")
		itens, proximo := itensJSON(t, exigirCorpo(t, rec))
		if len(itens) != 0 || proximo != "" || !strings.Contains(rec.Body.String(), `"itens":[]`) {
			t.Errorf("corpo = %s, want itens [] e proximo null", rec.Body.String())
		}
	})
	t.Run("parametro invalido", func(t *testing.T) {
		checarErro(t, chamar(h, http.MethodGet, "/usuarios?limite=101", ""), http.StatusBadRequest, "parametro_invalido", true)
	})
	t.Run("detalhe", func(t *testing.T) {
		u := exigirStatus(t, chamar(h, http.MethodGet, "/usuarios/"+ids[1], ""), 200)
		exigirChaves(t, u, "id", "nome")
		if idDe(t, u) != ids[1] || u["nome"] != nomes[ids[1]] {
			t.Errorf("usuario = %v, want id %s e nome %q", u, ids[1], nomes[ids[1]])
		}
	})
	t.Run("detalhe inexistente", func(t *testing.T) {
		rec := chamar(h, http.MethodGet, "/usuarios/999999", "")
		msg := checarErro(t, rec, http.StatusNotFound, "usuario_inexistente", true)
		if msg != "usuário não encontrado" {
			t.Errorf("mensagem = %q", msg)
		}
		semTextoInterno(t, "resposta", rec.Body.String())
	})
}

// TestBancoListagemEmprestimos percorre GET /emprestimos no banco real com
// empréstimos abertos e devolvidos: cada item é igual ao GET
// /emprestimos/{id} no mesmo instante. Um empréstimo gravado inválido derruba
// com 500 só a página que o entrega, não a anterior (em que é a linha extra).
func TestBancoListagemEmprestimos(t *testing.T) {
	pool := novoPool(t, 4)
	rel := &relogio{t: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)}
	h := handlerBanco(pool, rel, 5*time.Second)

	var livros []string
	for i := 0; i < 3; i++ {
		livros = append(livros, idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/livros", fmt.Sprintf(`{"titulo":"Livro %d"}`, i)), 201)))
	}
	uid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/usuarios", `{"nome":"Capitu"}`), 201))
	abrir := func(lid string) string {
		return idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/livros/"+lid+"/emprestimo", `{"usuario_id":`+uid+`}`), 201))
	}
	// 10/03 10:00: três empréstimos abertos (vencimento 24/03).
	e1, e2, e3 := abrir(livros[0]), abrir(livros[1]), abrir(livros[2])
	// 25/03 08:00: devolve e1 (um dia de atraso) e reabre o livro (vence 08/04).
	rel.ajustar(time.Date(2026, 3, 25, 8, 0, 0, 0, sp))
	exigirStatus(t, chamar(h, http.MethodPost, "/livros/"+livros[0]+"/devolucao", ""), 200)
	e4 := abrir(livros[0])
	todos := strings.Join([]string{e1, e2, e3, e4}, ",")

	// 26/03 12:00: abertos de 10/03 com dois dias de atraso.
	rel.ajustar(time.Date(2026, 3, 26, 12, 0, 0, 0, sp))
	wantMulta := map[string]string{e1: "1.00", e2: "2.00", e3: "2.00", e4: "0.00"}

	for _, limite := range []int{1, 2, 3, 4, 5, 100} {
		t.Run(fmt.Sprintf("percorrer com limite %d", limite), func(t *testing.T) {
			itens := percorrer(t, h, "/emprestimos", limite, 6)
			if got := idsDosItens(t, itens); got != todos {
				t.Fatalf("visitados = %s, want %s", got, todos)
			}
			for _, it := range itens {
				id := idDe(t, it)
				detalhe := exigirStatus(t, chamar(h, http.MethodGet, "/emprestimos/"+id, ""), 200)
				if !reflect.DeepEqual(it, detalhe) {
					t.Errorf("item %s = %v, want igual ao detalhe %v", id, it, detalhe)
				}
				if it["multa"] != wantMulta[id] {
					t.Errorf("item %s multa = %#v, want %q", id, it["multa"], wantMulta[id])
				}
			}
		})
	}

	t.Run("gravado inválido", func(t *testing.T) {
		// Sem CHECK de vencimento > início no banco: a reidratação recusa.
		if _, err := pool.Exec(t.Context(), "UPDATE emprestimo SET vencimento = DATE '2000-01-01' WHERE id = $1", e2); err != nil {
			t.Fatalf("corromper empréstimo: %v", err)
		}
		rec := chamar(h, http.MethodGet, "/emprestimos", "")
		checarErro(t, rec, http.StatusInternalServerError, "erro_interno", true)
		if strings.Contains(rec.Body.String(), "itens") {
			t.Errorf("500 com página parcial: %s", rec.Body.String())
		}
		semTextoInterno(t, "resposta", rec.Body.String())

		// e2 é a linha extra (+1) da página de e1: não é lida nem reidratada.
		itens, proximo := itensJSON(t, exigirCorpo(t, chamar(h, http.MethodGet, "/emprestimos?limite=1", "")))
		if got := idsDosItens(t, itens); got != e1 || proximo != e1 {
			t.Errorf("ids = %s proximo = %q, want %s e %s", got, proximo, e1, e1)
		}
		// O cliente contorna com apos.
		itens, proximo = itensJSON(t, exigirCorpo(t, chamar(h, http.MethodGet, "/emprestimos?apos="+e2, "")))
		if got := idsDosItens(t, itens); got != e3+","+e4 || proximo != "" {
			t.Errorf("ids = %s proximo = %q, want %s,%s e null", got, proximo, e3, e4)
		}
	})
}
