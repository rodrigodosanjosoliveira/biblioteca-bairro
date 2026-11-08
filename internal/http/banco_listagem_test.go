package httpapi_test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

// TestBancoListagemLivros percorre GET /livros no banco real: proximo é o id
// do último item entregue, a última página tem proximo null, nenhum livro é
// pulado nem repetido e apos além do último dá itens [] e proximo null.
func TestBancoListagemLivros(t *testing.T) {
	pool := novoPool(t, 4)
	rel := &relogio{t: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)}
	h := handlerBanco(pool, rel, 5*time.Second)

	var ids []string
	titulos0 := map[string]string{}
	for i := 0; i < 5; i++ {
		id := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/livros", fmt.Sprintf(`{"titulo":"Livro %d"}`, i)), 201))
		ids = append(ids, id)
		titulos0[id] = fmt.Sprintf("Livro %d", i)
	}
	todos := strings.Join(ids, ",")

	for _, limite := range []int{1, 2, 3, 4, 5, 6, 100} {
		t.Run(fmt.Sprintf("percorrer com limite %d", limite), func(t *testing.T) {
			var visitados []string
			path := fmt.Sprintf("/livros?limite=%d", limite)
			for pag := 0; ; pag++ {
				if pag > len(ids)+1 {
					t.Fatalf("paginação não termina: %v", visitados)
				}
				rec := chamar(h, http.MethodGet, path, "")
				if rec.Code != http.StatusOK {
					t.Fatalf("GET %s = %d (corpo %s)", path, rec.Code, rec.Body.String())
				}
				got, titulos, proximo := listaJSON(t, rec.Body.Bytes())
				if len(got) > limite {
					t.Fatalf("%d itens, want no máximo %d", len(got), limite)
				}
				for i, id := range got {
					if want := titulos0[id]; titulos[i] != want {
						t.Errorf("item %s titulo = %q, want %q", id, titulos[i], want)
					}
				}
				visitados = append(visitados, got...)
				if proximo == "" {
					break
				}
				if len(got) == 0 || proximo != got[len(got)-1] {
					t.Fatalf("proximo = %s, want id do último item entregue (%v)", proximo, got)
				}
				path = fmt.Sprintf("/livros?limite=%d&apos=%s", limite, proximo)
			}
			if got := strings.Join(visitados, ","); got != todos {
				t.Errorf("visitados = %s, want %s", got, todos)
			}
		})
	}

	t.Run("padrão sem query", func(t *testing.T) {
		got, _, proximo := listaJSON(t, exigirCorpo(t, chamar(h, http.MethodGet, "/livros", "")))
		if strings.Join(got, ",") != todos || proximo != "" {
			t.Errorf("ids = %v proximo = %q, want %s e null", got, proximo, todos)
		}
	})
	t.Run("página exata tem proximo null", func(t *testing.T) {
		got, _, proximo := listaJSON(t, exigirCorpo(t, chamar(h, http.MethodGet, "/livros?limite=2&apos="+ids[2], "")))
		if strings.Join(got, ",") != ids[3]+","+ids[4] || proximo != "" {
			t.Errorf("ids = %v proximo = %q, want %s,%s e null", got, proximo, ids[3], ids[4])
		}
	})
	t.Run("apos além do último", func(t *testing.T) {
		rec := chamar(h, http.MethodGet, "/livros?apos=9223372036854775807", "")
		got, _, proximo := listaJSON(t, exigirCorpo(t, rec))
		if len(got) != 0 || proximo != "" || !strings.Contains(rec.Body.String(), `"itens":[]`) {
			t.Errorf("corpo = %s, want itens [] e proximo null", rec.Body.String())
		}
	})
	t.Run("parametro invalido", func(t *testing.T) {
		checarErro(t, chamar(h, http.MethodGet, "/livros?limite=101", ""), http.StatusBadRequest, "parametro_invalido", true)
	})
}

// exigirCorpo exige 200 e devolve o corpo.
func exigirCorpo(t *testing.T, rec *httptest.ResponseRecorder) []byte {
	t.Helper()
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
	}
	return rec.Body.Bytes()
}

// TestBancoDetalhes: GET /livros/{id} e GET /emprestimos/{id} no banco real,
// com empréstimo aberto (multa pelo relógio) e devolvido (multa pela
// devolução, mesmo com o relógio adiante).
func TestBancoDetalhes(t *testing.T) {
	pool := novoPool(t, 4)
	rel := &relogio{t: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)}
	h := handlerBanco(pool, rel, 5*time.Second)

	lid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/livros", `{"titulo":"  Dom Casmurro "}`), 201))
	uid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/usuarios", `{"nome":"Capitu"}`), 201))

	livro := exigirStatus(t, chamar(h, http.MethodGet, "/livros/"+lid, ""), 200)
	exigirChaves(t, livro, "id", "titulo")
	if idDe(t, livro) != lid || livro["titulo"] != "Dom Casmurro" {
		t.Errorf("livro = %v, want id %s e titulo aparado", livro, lid)
	}
	checarErro(t, chamar(h, http.MethodGet, "/livros/999999", ""), http.StatusNotFound, "livro_nao_encontrado", true)

	eid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/livros/"+lid+"/emprestimo", `{"usuario_id":`+uid+`}`), 201))

	rel.ajustar(time.Date(2026, 3, 26, 12, 0, 0, 0, sp))
	rec := chamar(h, http.MethodGet, "/emprestimos/"+eid, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /emprestimos/%s = %d (corpo %s)", eid, rec.Code, rec.Body.String())
	}
	emprestimoJSON(t, rec.Body.Bytes(), eid, lid, uid, "2026-03-10T13:00:00Z", "2026-03-24", "", "2.00")

	exigirStatus(t, chamar(h, http.MethodPost, "/livros/"+lid+"/devolucao", ""), 200)

	rel.ajustar(time.Date(2027, 1, 1, 9, 0, 0, 0, sp))
	rec = chamar(h, http.MethodGet, "/emprestimos/"+eid, "")
	if rec.Code != http.StatusOK {
		t.Fatalf("GET /emprestimos/%s devolvido = %d (corpo %s)", eid, rec.Code, rec.Body.String())
	}
	emprestimoJSON(t, rec.Body.Bytes(), eid, lid, uid, "2026-03-10T13:00:00Z", "2026-03-24", "2026-03-26T15:00:00Z", "2.00")

	// Depois da devolução, a consulta por livro continua 404 com o código antigo.
	checarErro(t, chamar(h, http.MethodGet, "/livros/"+lid+"/emprestimo", ""), http.StatusNotFound, "emprestimo_aberto_nao_encontrado", true)

	n, _ := json.Number(eid).Int64()
	rec = chamar(h, http.MethodGet, fmt.Sprintf("/emprestimos/%d", n+1000), "")
	msg := checarErro(t, rec, http.StatusNotFound, "emprestimo_nao_encontrado", true)
	if msg != "empréstimo não encontrado" {
		t.Errorf("mensagem = %q", msg)
	}
	semTextoInterno(t, "resposta", rec.Body.String())
}
