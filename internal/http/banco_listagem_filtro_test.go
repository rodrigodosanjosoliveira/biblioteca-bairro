package httpapi_test

import (
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// percorrerQuery segue proximo a partir de base?<query>&limite=N até proximo
// null, conferindo o contrato da D-19 em cada página.
func percorrerQuery(t *testing.T, h http.Handler, base, query string, limite int) []map[string]any {
	t.Helper()
	sep := "?"
	if query != "" {
		base += "?" + query
		sep = "&"
	}
	var visitados []map[string]any
	path := fmt.Sprintf("%s%slimite=%d", base, sep, limite)
	for pag := 0; ; pag++ {
		if pag > 20 {
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
		path = fmt.Sprintf("%s%slimite=%d&apos=%s", base, sep, limite, proximo)
	}
}

// TestBancoListagemEmprestimosFiltros (DEL-015) percorre GET /emprestimos com
// filtros e GET /usuarios/{id}/emprestimos no banco real. Cada item é igual
// ao detalhe no mesmo instante; vencido segue a fronteira da D-17 (o dia do
// vencimento não conta) e todo item vencido sai com multa > 0.
func TestBancoListagemEmprestimosFiltros(t *testing.T) {
	pool := novoPool(t, 4)
	rel := &relogio{t: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)}
	h := handlerBanco(pool, rel, 5*time.Second)

	var livros []string
	for i := 0; i < 3; i++ {
		livros = append(livros, idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/livros", fmt.Sprintf(`{"titulo":"Livro %d"}`, i)), 201)))
	}
	var usuarios []string
	for i := 0; i < 3; i++ {
		usuarios = append(usuarios, idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/usuarios", fmt.Sprintf(`{"nome":"Usuario %d"}`, i)), 201)))
	}
	u1, u2, semEmprestimo := usuarios[0], usuarios[1], usuarios[2]
	abrir := func(lid, uid string) string {
		return idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/livros/"+lid+"/emprestimo", `{"usuario_id":`+uid+`}`), 201))
	}
	// 10/03 10:00: três abertos, vencimento 24/03.
	e1, e2, e3 := abrir(livros[0], u1), abrir(livros[1], u2), abrir(livros[2], u1)
	// 20/03: renova e3 (vence 07/04).
	rel.ajustar(time.Date(2026, 3, 20, 10, 0, 0, 0, sp))
	exigirStatus(t, chamar(h, http.MethodPost, "/livros/"+livros[2]+"/renovacao", ""), 200)
	// 22/03: devolve e1 e reabre o livro para u2 (vence 05/04).
	rel.ajustar(time.Date(2026, 3, 22, 10, 0, 0, 0, sp))
	exigirStatus(t, chamar(h, http.MethodPost, "/livros/"+livros[0]+"/devolucao", ""), 200)
	e4 := abrir(livros[0], u2)

	ids := func(xs ...string) string { return strings.Join(xs, ",") }
	tests := []struct {
		name  string
		agora time.Time
		base  string
		query string
		want  string
	}{
		{name: "aberto", agora: time.Date(2026, 3, 24, 12, 0, 0, 0, sp), base: "/emprestimos", query: "situacao=aberto", want: ids(e2, e3, e4)},
		{name: "devolvido", agora: time.Date(2026, 3, 24, 12, 0, 0, 0, sp), base: "/emprestimos", query: "situacao=devolvido", want: ids(e1)},
		{name: "vencido às 23:59:59 do dia do vencimento", agora: time.Date(2026, 3, 24, 23, 59, 59, 0, sp), base: "/emprestimos", query: "situacao=vencido", want: ""},
		{name: "vencido às 00:00 do dia seguinte", agora: time.Date(2026, 3, 25, 0, 0, 0, 0, sp), base: "/emprestimos", query: "situacao=vencido", want: ids(e2)},
		{name: "usuario_id", agora: time.Date(2026, 3, 25, 0, 0, 0, 0, sp), base: "/emprestimos", query: "usuario_id=" + u1, want: ids(e1, e3)},
		{name: "livro_id", agora: time.Date(2026, 3, 25, 0, 0, 0, 0, sp), base: "/emprestimos", query: "livro_id=" + livros[0], want: ids(e1, e4)},
		{name: "usuario_id e aberto", agora: time.Date(2026, 3, 25, 0, 0, 0, 0, sp), base: "/emprestimos", query: "usuario_id=" + u2 + "&situacao=aberto", want: ids(e2, e4)},
		{name: "usuario_id, livro_id e devolvido", agora: time.Date(2026, 3, 25, 0, 0, 0, 0, sp), base: "/emprestimos", query: "usuario_id=" + u1 + "&livro_id=" + livros[0] + "&situacao=devolvido", want: ids(e1)},
		{name: "usuario_id inexistente é 200 vazio", agora: time.Date(2026, 3, 25, 0, 0, 0, 0, sp), base: "/emprestimos", query: "usuario_id=999999", want: ""},
		{name: "livro_id inexistente é 200 vazio", agora: time.Date(2026, 3, 25, 0, 0, 0, 0, sp), base: "/emprestimos", query: "livro_id=999999", want: ""},
		{name: "aninhada", agora: time.Date(2026, 3, 25, 0, 0, 0, 0, sp), base: "/usuarios/" + u1 + "/emprestimos", want: ids(e1, e3)},
		{name: "aninhada com aberto", agora: time.Date(2026, 3, 25, 0, 0, 0, 0, sp), base: "/usuarios/" + u1 + "/emprestimos", query: "situacao=aberto", want: ids(e3)},
		{name: "aninhada com livro_id", agora: time.Date(2026, 3, 25, 0, 0, 0, 0, sp), base: "/usuarios/" + u2 + "/emprestimos", query: "livro_id=" + livros[0], want: ids(e4)},
		{name: "aninhada com vencido", agora: time.Date(2026, 3, 25, 0, 0, 0, 0, sp), base: "/usuarios/" + u2 + "/emprestimos", query: "situacao=vencido", want: ids(e2)},
		{name: "aninhada de usuário sem empréstimos", agora: time.Date(2026, 3, 25, 0, 0, 0, 0, sp), base: "/usuarios/" + semEmprestimo + "/emprestimos", want: ""},
		{name: "vencido bem depois", agora: time.Date(2026, 4, 10, 12, 0, 0, 0, sp), base: "/emprestimos", query: "situacao=vencido", want: ids(e2, e3, e4)},
	}
	for _, tt := range tests {
		for _, limite := range []int{1, 2, 100} {
			t.Run(fmt.Sprintf("%s/limite %d", tt.name, limite), func(t *testing.T) {
				rel.ajustar(tt.agora)
				itens := percorrerQuery(t, h, tt.base, tt.query, limite)
				if got := idsDosItens(t, itens); got != tt.want {
					t.Fatalf("visitados = %s, want %s", got, tt.want)
				}
				for _, it := range itens {
					id := idDe(t, it)
					detalhe := exigirStatus(t, chamar(h, http.MethodGet, "/emprestimos/"+id, ""), 200)
					if !reflect.DeepEqual(it, detalhe) {
						t.Errorf("item %s = %v, want igual ao detalhe %v", id, it, detalhe)
					}
					if strings.Contains(tt.query, "situacao=vencido") && it["multa"] == "0.00" {
						t.Errorf("item %s filtrado como vencido com multa 0.00", id)
					}
				}
			})
		}
	}

	t.Run("aninhada de usuário inexistente é 404 em toda página", func(t *testing.T) {
		for _, q := range []string{"", "?situacao=aberto", "?apos=" + e2 + "&limite=1"} {
			rec := chamar(h, http.MethodGet, "/usuarios/999999/emprestimos"+q, "")
			msg := checarErro(t, rec, http.StatusNotFound, "usuario_inexistente", true)
			if msg != "usuário não encontrado" {
				t.Errorf("mensagem = %q", msg)
			}
			semTextoInterno(t, "resposta", rec.Body.String())
		}
	})
	t.Run("aninhada com usuario_id na query é 400", func(t *testing.T) {
		checarErro(t, chamar(h, http.MethodGet, "/usuarios/"+u1+"/emprestimos?usuario_id="+u1, ""), http.StatusBadRequest, "parametro_invalido", true)
	})
	t.Run("situacao vazia é 400", func(t *testing.T) {
		checarErro(t, chamar(h, http.MethodGet, "/emprestimos?situacao=", ""), http.StatusBadRequest, "parametro_invalido", true)
	})
	t.Run("abertos não vencidos saem com multa zero", func(t *testing.T) {
		rel.ajustar(time.Date(2026, 3, 24, 23, 59, 59, 0, sp))
		for _, it := range percorrerQuery(t, h, "/emprestimos", "situacao=aberto", 100) {
			if it["multa"] != "0.00" {
				t.Errorf("item %s multa = %#v, want 0.00 no dia do vencimento", idDe(t, it), it["multa"])
			}
		}
	})
}
