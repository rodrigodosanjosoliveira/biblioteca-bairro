package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/storage"
)

const msgParametroInvalido = "parâmetro de consulta inválido"

// TestListarLivrosParametros fixa a gramática da query string de GET /livros:
// só limite e apos, cada um no máximo uma vez; limite 1..100 (padrão 20);
// apos > 0 com a gramática de parseID (padrão 0). Qualquer recusa é 400
// parametro_invalido, com mensagem fixa, sem chamar o repositório.
func TestListarLivrosParametros(t *testing.T) {
	tests := []struct {
		name       string
		query      string // sem o "?"
		wantErr    bool
		wantApos   int64
		wantLimite int
	}{
		// Padrões e valores aceitos.
		{name: "sem query", query: "", wantApos: 0, wantLimite: 20},
		{name: "só limite", query: "limite=5", wantApos: 0, wantLimite: 5},
		{name: "só apos", query: "apos=10", wantApos: 10, wantLimite: 20},
		{name: "limite e apos", query: "limite=5&apos=10", wantApos: 10, wantLimite: 5},
		{name: "apos antes de limite", query: "apos=10&limite=5", wantApos: 10, wantLimite: 5},
		{name: "limite mínimo", query: "limite=1", wantLimite: 1},
		{name: "limite máximo", query: "limite=100", wantLimite: 100},
		{name: "limite com zeros à esquerda", query: "limite=007", wantLimite: 7},
		{name: "limite com sinal +", query: "limite=%2B5", wantLimite: 5},
		{name: "limite em percent-encoding", query: "limite=%31%30", wantLimite: 10},
		{name: "apos mínimo", query: "apos=1", wantApos: 1, wantLimite: 20},
		{name: "apos máximo int64", query: "apos=9223372036854775807", wantApos: 9223372036854775807, wantLimite: 20},
		{name: "apos com zeros à esquerda", query: "apos=007", wantApos: 7, wantLimite: 20},
		{name: "apos com sinal +", query: "apos=%2B5", wantApos: 5, wantLimite: 20},
		{name: "separadores vazios", query: "&", wantLimite: 20},
		{name: "separador final", query: "limite=5&", wantLimite: 5},

		// limite inválido.
		{name: "limite zero", query: "limite=0", wantErr: true},
		{name: "limite acima do teto", query: "limite=101", wantErr: true},
		{name: "limite muito acima do teto", query: "limite=1000000", wantErr: true},
		{name: "limite negativo", query: "limite=-1", wantErr: true},
		{name: "limite vazio", query: "limite=", wantErr: true},
		{name: "limite sem =", query: "limite", wantErr: true},
		{name: "limite não numérico", query: "limite=abc", wantErr: true},
		{name: "limite fracionário", query: "limite=1.5", wantErr: true},
		{name: "limite com expoente", query: "limite=1e2", wantErr: true},
		{name: "limite hexadecimal", query: "limite=0x10", wantErr: true},
		{name: "limite overflow int64", query: "limite=9223372036854775808", wantErr: true},
		{name: "limite overflow de int32", query: "limite=4294967297", wantErr: true},
		{name: "limite com espaço à esquerda", query: "limite=%2010", wantErr: true},
		{name: "limite com espaço à direita", query: "limite=10%20", wantErr: true},
		{name: "limite com + (espaço)", query: "limite=+10", wantErr: true},
		{name: "limite só sinal", query: "limite=%2B", wantErr: true},

		// apos inválido.
		{name: "apos zero", query: "apos=0", wantErr: true},
		{name: "apos negativo", query: "apos=-1", wantErr: true},
		{name: "apos vazio", query: "apos=", wantErr: true},
		{name: "apos sem =", query: "apos", wantErr: true},
		{name: "apos não numérico", query: "apos=abc", wantErr: true},
		{name: "apos overflow int64", query: "apos=9223372036854775808", wantErr: true},
		{name: "apos fracionário", query: "apos=1.0", wantErr: true},
		{name: "apos hexadecimal", query: "apos=0x10", wantErr: true},
		{name: "apos com espaço", query: "apos=%201", wantErr: true},
		{name: "apos só zeros", query: "apos=000", wantErr: true},

		// Chaves.
		{name: "chave desconhecida", query: "x=1", wantErr: true},
		{name: "chave desconhecida junto de válidas", query: "limite=5&apos=1&x=1", wantErr: true},
		{name: "cache-buster", query: "_=123", wantErr: true},
		{name: "chave em maiúsculas", query: "LIMITE=5", wantErr: true},
		{name: "chave com espaço", query: "limite%20=5", wantErr: true},
		{name: "chave vazia", query: "=5", wantErr: true},
		{name: "limite repetido igual", query: "limite=5&limite=5", wantErr: true},
		{name: "limite repetido diferente", query: "limite=5&limite=6", wantErr: true},
		{name: "apos repetido", query: "apos=1&apos=2", wantErr: true},
		{name: "repetido com outra chave no meio", query: "apos=1&limite=5&apos=1", wantErr: true},

		// Sintaxe da query.
		{name: "escape inválido sozinho", query: "%zz", wantErr: true},
		{name: "escape inválido no valor", query: "limite=%zz", wantErr: true},
		{name: "escape incompleto", query: "limite=1%", wantErr: true},
		{name: "ponto e vírgula", query: "limite=5;apos=1", wantErr: true},
		{name: "ponto e vírgula depois de par válido", query: "limite=5&apos=1;x", wantErr: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			path := "/livros"
			if tt.query != "" {
				path += "?" + tt.query
			}
			rec := f.do(http.MethodGet, path, "", true)
			if tt.wantErr {
				msg := checarErro(t, rec, http.StatusBadRequest, "parametro_invalido", true)
				if msg != msgParametroInvalido {
					t.Errorf("mensagem = %q, want %q", msg, msgParametroInvalido)
				}
				if f.repo.nChamadas() != 0 {
					t.Errorf("repositório chamado com query inválida: %v", f.repo.chamadas)
				}
				return
			}
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			checarCabecalhos(t, rec, true)
			if got := strings.Join(f.repo.chamadas, ","); got != "ListarLivros" {
				t.Fatalf("chamadas = %s, want ListarLivros (uma vez)", got)
			}
			if f.repo.listarApos != tt.wantApos || f.repo.listarLimite != tt.wantLimite {
				t.Errorf("ListarLivros(apos=%d, limite=%d), want (%d, %d)", f.repo.listarApos, f.repo.listarLimite, tt.wantApos, tt.wantLimite)
			}
		})
	}
}

// A mensagem de parametro_invalido é fixa: não ecoa a chave nem o valor.
func TestListarLivrosErroNaoEcoaEntrada(t *testing.T) {
	queries := []string{
		"limite=VALOR-SECRETO",
		"CHAVE-SECRETA=1",
		"apos=-424242",
		"limite=5&limite=SEGREDO-REPETIDO",
		"limite=%zzSEGREDO",
	}
	for _, q := range queries {
		t.Run(q, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodGet, "/livros?"+q, "", true)
			checarErro(t, rec, http.StatusBadRequest, "parametro_invalido", true)
			for _, s := range []string{"SECRET", "SEGREDO", "424242", "limite", "apos"} {
				if strings.Contains(rec.Body.String(), s) {
					t.Errorf("resposta contém %q: %s", s, rec.Body.String())
				}
				if log := logEstavel(t, f); strings.Contains(log, s) {
					t.Errorf("log contém %q: %s", s, log)
				}
			}
		})
	}
}

// logEstavel devolve as linhas de log sem time, request_id e duracao_ms,
// cujos dígitos variam a cada execução e poderiam conter por acaso o valor
// procurado.
func logEstavel(t testing.TB, f *fixture) string {
	t.Helper()
	var b strings.Builder
	for _, l := range linhasLog(t, &f.logs) {
		delete(l, "time")
		delete(l, "request_id")
		delete(l, "duracao_ms")
		j, err := json.Marshal(l)
		if err != nil {
			t.Fatalf("remarshal do log: %v", err)
		}
		b.Write(j)
		b.WriteByte('\n')
	}
	return b.String()
}

// livros monta livros com os ids dados e títulos "L<id>".
func livros(ids ...int64) []domain.Livro {
	out := make([]domain.Livro, 0, len(ids))
	for _, id := range ids {
		out = append(out, domain.Livro{ID: id, Titulo: fmt.Sprintf("L%d", id)})
	}
	return out
}

// listaJSON decodifica {"itens":[...],"proximo":...} exigindo exatamente
// essas chaves, itens sempre array de objetos {id, titulo} e proximo número
// ou null ("" para null).
func listaJSON(t testing.TB, body []byte) (ids []string, titulos []string, proximo string) {
	t.Helper()
	m := objetoJSON(t, body)
	exigirChaves(t, m, "itens", "proximo")
	itens, ok := m["itens"].([]any)
	if !ok {
		t.Fatalf("itens = %#v, want array (nunca null)", m["itens"])
	}
	for i, it := range itens {
		o, ok := it.(map[string]any)
		if !ok {
			t.Fatalf("itens[%d] = %#v, want objeto", i, it)
		}
		exigirChaves(t, o, "id", "titulo")
		n, ok := o["id"].(json.Number)
		if !ok {
			t.Fatalf("itens[%d].id = %#v, want número", i, o["id"])
		}
		s, ok := o["titulo"].(string)
		if !ok {
			t.Fatalf("itens[%d].titulo = %#v, want string", i, o["titulo"])
		}
		ids = append(ids, n.String())
		titulos = append(titulos, s)
	}
	switch p := m["proximo"].(type) {
	case nil:
		proximo = ""
	case json.Number:
		proximo = p.String()
	default:
		t.Fatalf("proximo = %#v, want número ou null", m["proximo"])
	}
	return ids, titulos, proximo
}

// TestListarLivrosContrato: proximo é o id do último item entregue quando o
// repositório indica que há mais; null caso contrário. Os ids não são
// contíguos de propósito: proximo não é apos+limite nem o id seguinte.
func TestListarLivrosContrato(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		ret         []domain.Livro
		temMais     bool
		wantIDs     string
		wantProximo string // "" = null
	}{
		{name: "vazio (nil do repositório)", ret: nil, wantIDs: "", wantProximo: ""},
		{name: "vazio (slice vazia)", ret: []domain.Livro{}, wantIDs: "", wantProximo: ""},
		{name: "página única sem mais", ret: livros(4, 9, 15), wantIDs: "4,9,15", wantProximo: ""},
		{name: "página com mais", query: "limite=3", ret: livros(4, 9, 15), temMais: true, wantIDs: "4,9,15", wantProximo: "15"},
		{name: "página com mais depois de apos", query: "limite=2&apos=9", ret: livros(15, 23), temMais: true, wantIDs: "15,23", wantProximo: "23"},
		{name: "um item com mais", query: "limite=1", ret: livros(42), temMais: true, wantIDs: "42", wantProximo: "42"},
		{name: "id grande", query: "limite=1", ret: livros(9223372036854775806), temMais: true, wantIDs: "9223372036854775806", wantProximo: "9223372036854775806"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			f.repo.listarRet, f.repo.listarTemMais = tt.ret, tt.temMais
			path := "/livros"
			if tt.query != "" {
				path += "?" + tt.query
			}
			rec := f.do(http.MethodGet, path, "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			checarCabecalhos(t, rec, true)
			ids, titulos, proximo := listaJSON(t, rec.Body.Bytes())
			if got := strings.Join(ids, ","); got != tt.wantIDs {
				t.Errorf("ids = %s, want %s", got, tt.wantIDs)
			}
			for i, id := range ids {
				if titulos[i] != "L"+id {
					t.Errorf("titulo de %s = %q, want %q", id, titulos[i], "L"+id)
				}
			}
			if proximo != tt.wantProximo {
				t.Errorf("proximo = %q, want %q (\"\" = null)", proximo, tt.wantProximo)
			}
			if len(tt.ret) == 0 && !strings.Contains(rec.Body.String(), `"itens":[]`) {
				t.Errorf("corpo = %s, want \"itens\":[]", rec.Body.String())
			}
		})
	}
}

// Erros do repositório na listagem e nos detalhes seguem o mapa de erros.
func TestListagemEDetalheMapaDeErros(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		setup  func(f *fixture)
		status int
		codigo string
		msg    string
	}{
		{name: "listagem: DeadlineExceeded", path: "/livros", setup: func(f *fixture) { f.repo.errListar = fmt.Errorf("storage: %w", context.DeadlineExceeded) }, status: 503, codigo: "indisponivel"},
		{name: "listagem: Canceled", path: "/livros", setup: func(f *fixture) { f.repo.errListar = context.Canceled }, status: 503, codigo: "indisponivel"},
		{name: "listagem: ErrListagemInvalida", path: "/livros", setup: func(f *fixture) { f.repo.errListar = storage.ErrListagemInvalida }, status: 500, codigo: "erro_interno"},
		{name: "listagem: PgError", path: "/livros", setup: func(f *fixture) { f.repo.errListar = pgErroSecreto() }, status: 500, codigo: "erro_interno"},
		{name: "listagem: desconhecido", path: "/livros", setup: func(f *fixture) { f.repo.errListar = errors.New("host=segredo") }, status: 500, codigo: "erro_interno"},

		{name: "livro: inexistente", path: "/livros/9", setup: func(f *fixture) { f.repo.errBuscarLivro = storage.ErrLivroNaoEncontrado }, status: 404, codigo: "livro_nao_encontrado", msg: "livro não encontrado"},
		{name: "livro: inexistente embrulhado", path: "/livros/9", setup: func(f *fixture) { f.repo.errBuscarLivro = fmt.Errorf("x: %w", storage.ErrLivroNaoEncontrado) }, status: 404, codigo: "livro_nao_encontrado"},
		{name: "livro: DeadlineExceeded", path: "/livros/9", setup: func(f *fixture) { f.repo.errBuscarLivro = context.DeadlineExceeded }, status: 503, codigo: "indisponivel"},
		{name: "livro: PgError", path: "/livros/9", setup: func(f *fixture) { f.repo.errBuscarLivro = pgErroSecreto() }, status: 500, codigo: "erro_interno"},

		{name: "emprestimo: inexistente", path: "/emprestimos/9", setup: func(f *fixture) { f.repo.errBuscarEmpr = storage.ErrEmprestimoNaoEncontrado }, status: 404, codigo: "emprestimo_nao_encontrado", msg: "empréstimo não encontrado"},
		{name: "emprestimo: inexistente embrulhado", path: "/emprestimos/9", setup: func(f *fixture) { f.repo.errBuscarEmpr = fmt.Errorf("x: %w", storage.ErrEmprestimoNaoEncontrado) }, status: 404, codigo: "emprestimo_nao_encontrado"},
		{name: "emprestimo: DeadlineExceeded", path: "/emprestimos/9", setup: func(f *fixture) { f.repo.errBuscarEmpr = fmt.Errorf("storage: %w", context.DeadlineExceeded) }, status: 503, codigo: "indisponivel"},
		{name: "emprestimo: ErrLivroNaoEncontrado não vira emprestimo_nao_encontrado", path: "/emprestimos/9", setup: func(f *fixture) { f.repo.errBuscarEmpr = storage.ErrLivroNaoEncontrado }, status: 404, codigo: "livro_nao_encontrado"},
		{name: "emprestimo: gravado inválido", path: "/emprestimos/9", setup: func(f *fixture) {
			f.repo.errBuscarEmpr = fmt.Errorf("storage: empréstimo gravado inválido: %w", domain.ErrRenovacoesInvalidas)
		}, status: 500, codigo: "erro_interno"},
		{name: "emprestimo: PgError", path: "/emprestimos/9", setup: func(f *fixture) { f.repo.errBuscarEmpr = pgErroSecreto() }, status: 500, codigo: "erro_interno"},
		{name: "emprestimo: relógio zerado com empréstimo aberto", path: "/emprestimos/77", setup: func(f *fixture) { f.agora = time.Time{} }, status: 500, codigo: "erro_interno"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			tt.setup(f)
			rec := f.do(http.MethodGet, tt.path, "", true)
			msg := checarErro(t, rec, tt.status, tt.codigo, true)
			if tt.msg != "" && msg != tt.msg {
				t.Errorf("mensagem = %q, want %q", msg, tt.msg)
			}
			semTextoInterno(t, "resposta", rec.Body.String())
			semTextoInterno(t, "log", f.logs.String())
		})
	}
}

// As rotas antigas continuam respondendo emprestimo_aberto_nao_encontrado
// quando o repositório diz ErrEmprestimoNaoEncontrado: o código novo é só do
// detalhe por id.
func TestRotasAntigasMantemEmprestimoAbertoNaoEncontrado(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
	}{
		{name: "consulta por livro", method: http.MethodGet, path: "/livros/1/emprestimo"},
		{name: "devolucao", method: http.MethodPost, path: "/livros/1/devolucao"},
		{name: "renovacao", method: http.MethodPost, path: "/livros/1/renovacao"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			f.repo.errBuscar = storage.ErrEmprestimoNaoEncontrado
			rec := f.do(tt.method, tt.path, "", true)
			msg := checarErro(t, rec, http.StatusNotFound, "emprestimo_aberto_nao_encontrado", true)
			if msg != "nenhum empréstimo aberto para o livro" {
				t.Errorf("mensagem = %q", msg)
			}
		})
	}
}

// TestDetalheParametroID: GET /livros/{id} e GET /emprestimos/{id} usam a
// gramática de parseID; inválido é 400 id_invalido sem chamar o repositório.
func TestDetalheParametroID(t *testing.T) {
	invalidos := []struct{ name, seg string }{
		{name: "abc", seg: "abc"},
		{name: "zero", seg: "0"},
		{name: "negativo", seg: "-1"},
		{name: "overflow", seg: "9223372036854775808"},
		{name: "espaço à esquerda", seg: "%201"},
		{name: "espaço à direita", seg: "1%20"},
		{name: "fracionário", seg: "1.5"},
		{name: "hexadecimal", seg: "0x10"},
		{name: "zeros", seg: "000"},
	}
	for _, base := range []string{"/livros/", "/emprestimos/"} {
		for _, iv := range invalidos {
			t.Run(base+iv.name, func(t *testing.T) {
				f := novaFixture(t)
				rec := f.do(http.MethodGet, base+iv.seg, "", true)
				checarErro(t, rec, http.StatusBadRequest, "id_invalido", true)
				if f.repo.nChamadas() != 0 {
					t.Errorf("repositório chamado com id inválido: %v", f.repo.chamadas)
				}
			})
		}
	}

	validos := []struct {
		seg  string
		want int64
	}{
		{seg: "1", want: 1},
		{seg: "007", want: 7},
		{seg: "%2B5", want: 5},
		{seg: "9223372036854775807", want: 9223372036854775807},
	}
	for _, v := range validos {
		t.Run("livro/"+v.seg, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodGet, "/livros/"+v.seg, "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			if got := strings.Join(f.repo.chamadas, ","); got != "BuscarLivro" {
				t.Errorf("chamadas = %s, want BuscarLivro", got)
			}
			if f.repo.livroIDRecebido != v.want {
				t.Errorf("BuscarLivro(%d), want %d", f.repo.livroIDRecebido, v.want)
			}
		})
		t.Run("emprestimo/"+v.seg, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodGet, "/emprestimos/"+v.seg, "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			if got := strings.Join(f.repo.chamadas, ","); got != "BuscarEmprestimo" {
				t.Errorf("chamadas = %s, want BuscarEmprestimo", got)
			}
			if f.repo.emprIDRecebido != v.want {
				t.Errorf("BuscarEmprestimo(%d), want %d", f.repo.emprIDRecebido, v.want)
			}
		})
	}
}

// GET /livros/{id} responde exatamente {id, titulo} com o livro do
// repositório.
func TestContratoSaidaDetalheLivro(t *testing.T) {
	f := novaFixture(t)
	f.repo.livroRet = &domain.Livro{ID: 12, Titulo: "Memórias Póstumas"}
	rec := f.do(http.MethodGet, "/livros/12", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
	}
	checarCabecalhos(t, rec, true)
	m := objetoJSON(t, rec.Body.Bytes())
	exigirChaves(t, m, "id", "titulo")
	if m["id"] != json.Number("12") || m["titulo"] != "Memórias Póstumas" {
		t.Errorf("livro = %v, want id 12 e titulo Memórias Póstumas", m)
	}
}

// GET /emprestimos/{id}: mesmo DTO do empréstimo; aberto calcula a multa com
// ref = agora; devolvido usa a data de devolução e ignora agora.
func TestContratoSaidaDetalheEmprestimo(t *testing.T) {
	tests := []struct {
		name           string
		agora          time.Time
		ret            func(t *testing.T) domain.Emprestimo
		wantID         string
		wantVencimento string
		wantDevolucao  string // "" = null
		wantMulta      string
		wantRenovacoes string
	}{
		{
			name:  "aberto antes do vencimento",
			agora: time.Date(2026, 3, 20, 9, 0, 0, 0, sp),
			ret: func(t *testing.T) domain.Emprestimo {
				return emprestimo(t, 31, 1, 2, inicioPadrao(), time.Time{})
			},
			wantID: "31", wantVencimento: "2026-03-24", wantMulta: "0.00", wantRenovacoes: "0",
		},
		{
			name:  "aberto com dois dias de atraso",
			agora: time.Date(2026, 3, 26, 12, 0, 0, 0, sp),
			ret: func(t *testing.T) domain.Emprestimo {
				return emprestimo(t, 31, 1, 2, inicioPadrao(), time.Time{})
			},
			wantID: "31", wantVencimento: "2026-03-24", wantMulta: "2.00", wantRenovacoes: "0",
		},
		{
			name:  "aberto acima do teto",
			agora: time.Date(2027, 1, 1, 9, 0, 0, 0, sp),
			ret: func(t *testing.T) domain.Emprestimo {
				return emprestimo(t, 31, 1, 2, inicioPadrao(), time.Time{})
			},
			wantID: "31", wantVencimento: "2026-03-24", wantMulta: "20.00", wantRenovacoes: "0",
		},
		{
			name:  "devolvido com um dia de atraso, consultado muito depois",
			agora: time.Date(2027, 1, 1, 9, 0, 0, 0, sp),
			ret: func(t *testing.T) domain.Emprestimo {
				return emprestimo(t, 31, 1, 2, inicioPadrao(), time.Date(2026, 3, 25, 8, 0, 0, 0, sp))
			},
			wantID: "31", wantVencimento: "2026-03-24", wantDevolucao: "2026-03-25T11:00:00Z", wantMulta: "1.00", wantRenovacoes: "0",
		},
		{
			name:  "devolvido no prazo, consultado muito depois",
			agora: time.Date(2027, 1, 1, 9, 0, 0, 0, sp),
			ret: func(t *testing.T) domain.Emprestimo {
				return emprestimo(t, 31, 1, 2, inicioPadrao(), time.Date(2026, 3, 12, 8, 0, 0, 0, sp))
			},
			wantID: "31", wantVencimento: "2026-03-24", wantDevolucao: "2026-03-12T11:00:00Z", wantMulta: "0.00", wantRenovacoes: "0",
		},
		{
			name:  "aberto renovado",
			agora: time.Date(2026, 4, 1, 9, 0, 0, 0, sp),
			ret: func(t *testing.T) domain.Emprestimo {
				return emprestimoRenovado(t, 31, 1, 2, inicioPadrao(), time.Date(2026, 4, 7, 12, 0, 0, 0, sp), 1)
			},
			wantID: "31", wantVencimento: "2026-04-07", wantMulta: "0.00", wantRenovacoes: "1",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			f.agora = tt.agora
			e := tt.ret(t)
			f.repo.buscarEmprRet = &e
			rec := f.do(http.MethodGet, "/emprestimos/31", "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			checarCabecalhos(t, rec, true)
			emprestimoJSONRenovacoes(t, rec.Body.Bytes(), tt.wantID, "1", "2", "2026-03-10T13:00:00Z", tt.wantVencimento, tt.wantDevolucao, tt.wantMulta, tt.wantRenovacoes)
			if f.repo.emprIDRecebido != 31 {
				t.Errorf("BuscarEmprestimo(%d), want 31", f.repo.emprIDRecebido)
			}
		})
	}
}

// As rotas de detalhe ignoram a query string (como GET
// /livros/{id}/emprestimo); só a listagem a valida.
func TestDetalheIgnoraQueryString(t *testing.T) {
	paths := []string{
		"/livros/1?x=1",
		"/livros/1?limite=0",
		"/livros/1?%zz",
		"/emprestimos/77?apos=-1",
		"/emprestimos/77?limite=5;x",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodGet, p, "", true)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// Nenhum valor de limite/apos/URL/query chega ao log, em sucesso ou erro.
func TestLogListagemSemQuery(t *testing.T) {
	paths := []string{
		"/livros?limite=77&apos=424242",
		"/livros?limite=424242",
		"/livros?apos=424242&QSSECRETO=1",
		"/livros/424242?QSSECRETO=1",
		"/emprestimos/424242?QSSECRETO=1",
	}
	for _, p := range paths {
		t.Run(p, func(t *testing.T) {
			f := novaFixture(t)
			f.do(http.MethodGet, p, "", true)
			if n := len(linhasLog(t, &f.logs)); n != 1 {
				t.Fatalf("%d linhas de log, want 1: %s", n, f.logs.String())
			}
			log := logEstavel(t, f)
			for _, s := range []string{"424242", "QSSECRETO", "limite", "apos", "?"} {
				if strings.Contains(log, s) {
					t.Errorf("log contém %q: %s", s, log)
				}
			}
		})
	}
}
