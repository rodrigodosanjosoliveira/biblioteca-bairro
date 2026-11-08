package httpapi_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
)

// FuzzParseID exercita o {id} pela rota GET /livros/{id}/emprestimo. O oráculo
// é strconv.ParseInt(s, 10, 64) > 0: aceito chega ao repositório com o mesmo
// valor; o resto é 400 id_invalido sem chamar o repositório.
func FuzzParseID(f *testing.F) {
	for _, s := range []string{"1", "007", "0", "-1", "abc", "9223372036854775807", "9223372036854775808", " 1", "1 ", "+1", "1e3", "0x1", "١", "\x00", "%", "1/2"} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, s string) {
		// Segmentos que o mux não entrega ao handler (vazio, ponto, barra).
		if s == "" || s == "." || s == ".." || strings.Contains(s, "/") {
			return
		}
		fx := novaFixture(t)
		req := httptest.NewRequest(http.MethodGet, "/livros/"+url.PathEscape(s)+"/emprestimo", nil)
		req.Header.Set("Authorization", "Bearer "+tokenTeste)
		rec := httptest.NewRecorder()
		fx.handler().ServeHTTP(rec, req)

		n, err := strconv.ParseInt(s, 10, 64)
		if err == nil && n > 0 {
			if rec.Code != http.StatusOK {
				t.Fatalf("id %q: status = %d, want 200 (corpo %s)", s, rec.Code, rec.Body.String())
			}
			if fx.repo.buscarRecebido != n {
				t.Fatalf("id %q: livroID = %d, want %d", s, fx.repo.buscarRecebido, n)
			}
			return
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("id %q: status = %d, want 400 (corpo %s)", s, rec.Code, rec.Body.String())
		}
		if c, _ := erroJSON(t, rec.Body.Bytes()); c != "id_invalido" {
			t.Fatalf("id %q: codigo = %q, want id_invalido", s, c)
		}
		if fx.repo.nChamadas() != 0 {
			t.Fatalf("id %q: repositório chamado", s)
		}
	})
}

// oraculoListagem é a especificação independente da query de GET /livros:
// url.ParseQuery sem erro; só as chaves limite e apos, cada uma uma vez;
// limite = strconv.ParseInt base 10 em 1..100 (padrão 20); apos =
// strconv.ParseInt base 10 > 0 (padrão 0).
func oraculoListagem(raw string) (apos int64, limite int, ok bool) {
	vals, err := url.ParseQuery(raw)
	if err != nil {
		return 0, 0, false
	}
	limite = 20
	for k, vs := range vals {
		if len(vs) != 1 {
			return 0, 0, false
		}
		n, err := strconv.ParseInt(vs[0], 10, 64)
		switch k {
		case "limite":
			if err != nil || n < 1 || n > 100 {
				return 0, 0, false
			}
			limite = int(n)
		case "apos":
			if err != nil || n <= 0 {
				return 0, 0, false
			}
			apos = n
		default:
			return 0, 0, false
		}
	}
	return apos, limite, true
}

// FuzzParametrosListagem manda query strings arbitrárias para GET /livros.
// Aceita chega ao repositório com os valores do oráculo; o resto é 400
// parametro_invalido sem chamar o repositório. Nunca outro status.
func FuzzParametrosListagem(f *testing.F) {
	for _, s := range []string{
		"", "limite=20", "apos=1", "limite=1&apos=9223372036854775807", "limite=100", "limite=101", "limite=0",
		"apos=0", "apos=-1", "apos=9223372036854775808", "limite=%2B5", "limite=+5", "limite=007", "limite=%31%30",
		"x=1", "limite=1&limite=1", "%zz", "limite=5;apos=1", "&", "=", "limite", "apos=", "limite=1e2",
		"limite=4294967297", "limite=18446744073709551617", "apos=١", "limite=\x00", "a=1&b=2&c=3", "limite=5#frag",
	} {
		f.Add(s)
	}
	f.Fuzz(func(t *testing.T, raw string) {
		fx := novaFixture(t)
		req := httptest.NewRequest(http.MethodGet, "/livros", nil)
		req.URL.RawQuery = raw
		req.Header.Set("Authorization", "Bearer "+tokenTeste)
		rec := httptest.NewRecorder()
		fx.handler().ServeHTTP(rec, req)

		apos, limite, ok := oraculoListagem(raw)
		if ok {
			if rec.Code != http.StatusOK {
				t.Fatalf("query %q: status = %d, want 200 (corpo %s)", raw, rec.Code, rec.Body.String())
			}
			if fx.repo.nChamadas() != 1 || fx.repo.listarApos != apos || fx.repo.listarLimite != limite {
				t.Fatalf("query %q: ListarLivros(%d, %d) em %d chamada(s), want (%d, %d) uma vez",
					raw, fx.repo.listarApos, fx.repo.listarLimite, fx.repo.nChamadas(), apos, limite)
			}
			return
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("query %q: status = %d, want 400 (corpo %s)", raw, rec.Code, rec.Body.String())
		}
		if c, m := erroJSON(t, rec.Body.Bytes()); c != "parametro_invalido" || m != "parâmetro de consulta inválido" {
			t.Fatalf("query %q: erro = %q %q, want parametro_invalido com mensagem fixa", raw, c, m)
		}
		if fx.repo.nChamadas() != 0 {
			t.Fatalf("query %q: repositório chamado", raw)
		}
	})
}

// FuzzDecodificarCorpo manda corpos arbitrários às rotas com corpo. Sem
// panic; erro só como 400, 413 ou 422, com o código coerente; JSON inválido
// nunca passa; na devolução, qualquer byte é 400.
func FuzzDecodificarCorpo(f *testing.F) {
	seeds := []string{
		`{"titulo":"a"}`, `{"nome":"b"}`, `{"usuario_id":2}`, `{}`, ``, ` `, `null`, `[]`,
		`{"titulo":"a"}{}`, `{"titulo":1}`, `{"usuario_id":1.5}`, `{"usuario_id":1e2}`,
		`{"usuario_id":9223372036854775808}`, `{"titulo":"\u0000"}`, `{"x":1}`, `{"titulo":"a"`,
		"\xff\xfe", `{"titulo":"\ud800"}`, `{"usuario_id":-0}`, `{"TITULO":"a"}`,
	}
	for i, s := range seeds {
		f.Add(uint8(i), []byte(s))
	}
	rotas := []string{"/livros", "/usuarios", "/livros/1/emprestimo", "/livros/1/devolucao"}
	codigosPorStatus := map[int]map[string]bool{
		400: {"corpo_invalido": true},
		413: {"corpo_muito_grande": true},
		422: {"titulo_vazio": true, "nome_vazio": true, "texto_invalido": true, "usuario_id_invalido": true},
	}
	f.Fuzz(func(t *testing.T, r uint8, body []byte) {
		path := rotas[int(r)%len(rotas)]
		fx := novaFixture(t)
		req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
		req.Header.Set("Authorization", "Bearer "+tokenTeste)
		rec := httptest.NewRecorder()
		fx.handler().ServeHTTP(rec, req)

		if path == "/livros/1/devolucao" {
			want := http.StatusOK
			if len(body) > 0 {
				want = http.StatusBadRequest
			}
			if rec.Code != want {
				t.Fatalf("devolução com %d bytes: status = %d, want %d", len(body), rec.Code, want)
			}
			if want == http.StatusBadRequest {
				if c, _ := erroJSON(t, rec.Body.Bytes()); c != "corpo_invalido" {
					t.Fatalf("codigo = %q, want corpo_invalido", c)
				}
			}
			return
		}

		if rec.Code == http.StatusCreated {
			if !json.Valid(body) {
				t.Fatalf("%s aceitou JSON inválido %q", path, body)
			}
			if strings.ContainsRune(fx.repo.livroRecebido.Titulo, 0) || strings.ContainsRune(fx.repo.usuarioRecebido.Nome, 0) {
				t.Fatalf("%s levou NUL ao repositório: %q", path, body)
			}
			return
		}
		permitidos, ok := codigosPorStatus[rec.Code]
		if !ok {
			t.Fatalf("%s com %q: status = %d, want 201, 400, 413 ou 422", path, body, rec.Code)
		}
		c, _ := erroJSON(t, rec.Body.Bytes())
		if !permitidos[c] {
			t.Fatalf("%s com %q: status %d com codigo %q", path, body, rec.Code, c)
		}
		if rec.Code == http.StatusUnprocessableEntity && !json.Valid(body) {
			t.Fatalf("%s com JSON inválido %q deu 422, want 400", path, body)
		}
	})
}
