package httpapi_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"
)

// TestBancoDisponibilidadeLivros (DEL-017, CA1 e CA2 no banco real): o
// disponivel de GET /livros/{id} e de cada item de GET /livros segue o
// empréstimo aberto pelas rotas de escrita; vencido continua indisponível;
// POST /livros continua {id, titulo}; 404 como antes.
func TestBancoDisponibilidadeLivros(t *testing.T) {
	pool := novoPool(t, 4)
	rel := &relogio{t: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)}
	h := handlerBanco(pool, rel, 5*time.Second)

	uid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/usuarios", `{"nome":"Capitu"}`), 201))
	var ids []string
	for i := 0; i < 6; i++ {
		criado := exigirStatus(t, chamar(h, http.MethodPost, "/livros", fmt.Sprintf(`{"titulo":"Livro %d"}`, i)), 201)
		exigirChaves(t, criado, "id", "titulo")
		ids = append(ids, idDe(t, criado))
	}
	base := func(i int) string { return "/livros/" + ids[i] }

	// 0: nunca emprestado; 1: aberto; 2: devolvido; 3: renovado;
	// 4: aberto e vencido (relógio adiante); 5: devolvido e reaberto.
	for _, i := range []int{1, 2, 3, 4, 5} {
		exigirStatus(t, chamar(h, http.MethodPost, base(i)+"/emprestimo", `{"usuario_id":`+uid+`}`), 201)
	}
	exigirStatus(t, chamar(h, http.MethodPost, base(2)+"/devolucao", ""), 200)
	exigirStatus(t, chamar(h, http.MethodPost, base(3)+"/renovacao", ""), 200)
	exigirStatus(t, chamar(h, http.MethodPost, base(5)+"/devolucao", ""), 200)
	exigirStatus(t, chamar(h, http.MethodPost, base(5)+"/emprestimo", `{"usuario_id":`+uid+`}`), 201)
	rel.ajustar(time.Date(2026, 5, 1, 10, 0, 0, 0, sp)) // todos os abertos vencidos

	want := []bool{true, false, true, false, false, false}

	for i, w := range want {
		t.Run("detalhe "+ids[i], func(t *testing.T) {
			m := exigirStatus(t, chamar(h, http.MethodGet, base(i), ""), 200)
			exigirChaves(t, m, "id", "titulo", "disponivel")
			if got, ok := m["disponivel"].(bool); !ok || got != w {
				t.Errorf("disponivel = %#v, want %v", m["disponivel"], w)
			}
		})
	}

	for _, limite := range []int{1, 2, 4, 6, 100} {
		t.Run(fmt.Sprintf("listagem com limite %d", limite), func(t *testing.T) {
			var visitados []string
			var disp []bool
			path := fmt.Sprintf("/livros?limite=%d", limite)
			for pag := 0; ; pag++ {
				if pag > len(ids)+1 {
					t.Fatalf("paginação não termina: %v", visitados)
				}
				body := exigirCorpo(t, chamar(h, http.MethodGet, path, ""))
				got, _, proximo := listaJSON(t, body)
				visitados = append(visitados, got...)
				disp = append(disp, disponiveisJSON(t, body)...)
				if proximo == "" {
					break
				}
				path = fmt.Sprintf("/livros?limite=%d&apos=%s", limite, proximo)
			}
			if strings.Join(visitados, ",") != strings.Join(ids, ",") {
				t.Errorf("visitados = %v, want %v", visitados, ids)
			}
			if fmt.Sprint(disp) != fmt.Sprint(want) {
				t.Errorf("disponivel = %v, want %v", disp, want)
			}
		})
	}

	t.Run("devolver torna disponível nas duas rotas", func(t *testing.T) {
		exigirStatus(t, chamar(h, http.MethodPost, base(1)+"/devolucao", ""), 200)
		if m := exigirStatus(t, chamar(h, http.MethodGet, base(1), ""), 200); m["disponivel"] != true {
			t.Errorf("detalhe disponivel = %#v, want true", m["disponivel"])
		}
		body := exigirCorpo(t, chamar(h, http.MethodGet, "/livros?limite=1&apos="+ids[0], ""))
		if got := disponiveisJSON(t, body); fmt.Sprint(got) != "[true]" {
			t.Errorf("listagem disponivel = %v, want [true]", got)
		}
	})

	t.Run("inexistente continua 404", func(t *testing.T) {
		checarErro(t, chamar(h, http.MethodGet, "/livros/999999", ""), http.StatusNotFound, "livro_nao_encontrado", true)
	})
}
