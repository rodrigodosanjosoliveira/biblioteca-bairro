package httpapi_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
	httpapi "github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/storage"
)

// listagensNovas são as listagens da DEL-014 e o método do repositório que
// cada uma chama.
var listagensNovas = []struct{ path, metodo string }{
	{path: "/usuarios", metodo: "ListarUsuarios"},
	{path: "/emprestimos", metodo: "ListarEmprestimos"},
}

// TestListagensNovasParametros: GET /usuarios e GET /emprestimos seguem a
// mesma gramática de GET /livros (D-19), sem variação: só limite e apos, cada
// um no máximo uma vez; limite 1..100 (padrão 20); apos > 0. Filtros ficaram
// fora (U1): situacao, usuario_id e livro_id também são 400.
func TestListagensNovasParametros(t *testing.T) {
	tests := []struct {
		name       string
		query      string
		wantErr    bool
		wantApos   int64
		wantLimite int
	}{
		{name: "sem query", query: "", wantLimite: 20},
		{name: "só limite", query: "limite=5", wantLimite: 5},
		{name: "só apos", query: "apos=10", wantApos: 10, wantLimite: 20},
		{name: "limite e apos", query: "apos=10&limite=5", wantApos: 10, wantLimite: 5},
		{name: "limite mínimo", query: "limite=1", wantLimite: 1},
		{name: "limite máximo", query: "limite=100", wantLimite: 100},
		{name: "limite com sinal +", query: "limite=%2B5", wantLimite: 5},
		{name: "apos máximo int64", query: "apos=9223372036854775807", wantApos: 9223372036854775807, wantLimite: 20},
		{name: "separador final", query: "limite=5&", wantLimite: 5},

		{name: "limite zero", query: "limite=0", wantErr: true},
		{name: "limite acima do teto", query: "limite=101", wantErr: true},
		{name: "limite negativo", query: "limite=-1", wantErr: true},
		{name: "limite vazio", query: "limite=", wantErr: true},
		{name: "limite não numérico", query: "limite=abc", wantErr: true},
		{name: "limite overflow", query: "limite=9223372036854775808", wantErr: true},
		{name: "apos zero", query: "apos=0", wantErr: true},
		{name: "apos negativo", query: "apos=-1", wantErr: true},
		{name: "apos vazio", query: "apos=", wantErr: true},
		{name: "apos overflow", query: "apos=9223372036854775808", wantErr: true},
		{name: "chave desconhecida", query: "x=1", wantErr: true},
		{name: "cache-buster", query: "_=1", wantErr: true},
		{name: "filtro situacao fora", query: "situacao=aberto", wantErr: true},
		{name: "filtro usuario_id fora", query: "usuario_id=2", wantErr: true},
		{name: "filtro livro_id fora", query: "livro_id=1", wantErr: true},
		{name: "filtro junto de válidas", query: "limite=5&situacao=vencido", wantErr: true},
		{name: "limite repetido", query: "limite=5&limite=5", wantErr: true},
		{name: "apos repetido", query: "apos=1&apos=2", wantErr: true},
		{name: "escape inválido", query: "%zz", wantErr: true},
		{name: "ponto e vírgula", query: "limite=5;apos=1", wantErr: true},
	}
	for _, rota := range listagensNovas {
		for _, tt := range tests {
			t.Run(rota.path+"/"+tt.name, func(t *testing.T) {
				f := novaFixture(t)
				path := rota.path
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
					for _, s := range []string{"situacao", "usuario_id", "livro_id", "limite", "apos"} {
						if strings.Contains(rec.Body.String(), s) {
							t.Errorf("resposta ecoa %q: %s", s, rec.Body.String())
						}
					}
					return
				}
				if rec.Code != http.StatusOK {
					t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
				}
				checarCabecalhos(t, rec, true)
				if got := strings.Join(f.repo.chamadas, ","); got != rota.metodo {
					t.Fatalf("chamadas = %s, want %s (uma vez)", got, rota.metodo)
				}
				if f.repo.listarApos != tt.wantApos || f.repo.listarLimite != tt.wantLimite {
					t.Errorf("%s(apos=%d, limite=%d), want (%d, %d)", rota.metodo, f.repo.listarApos, f.repo.listarLimite, tt.wantApos, tt.wantLimite)
				}
			})
		}
	}
}

// usuarios monta usuários com os ids dados e nomes "U<id>".
func usuarios(ids ...int64) []domain.Usuario {
	out := make([]domain.Usuario, 0, len(ids))
	for _, id := range ids {
		out = append(out, domain.Usuario{ID: id, Nome: fmt.Sprintf("U%d", id)})
	}
	return out
}

// proximoJSON lê proximo: número ou null ("" para null).
func proximoJSON(t testing.TB, v any) string {
	t.Helper()
	switch p := v.(type) {
	case nil:
		return ""
	case json.Number:
		return p.String()
	}
	t.Fatalf("proximo = %#v, want número ou null", v)
	return ""
}

// itensJSON decodifica {"itens":[...],"proximo":...} exigindo exatamente essas
// chaves e itens sempre array de objetos.
func itensJSON(t testing.TB, body []byte) (itens []map[string]any, proximo string) {
	t.Helper()
	m := objetoJSON(t, body)
	exigirChaves(t, m, "itens", "proximo")
	arr, ok := m["itens"].([]any)
	if !ok {
		t.Fatalf("itens = %#v, want array (nunca null)", m["itens"])
	}
	for i, it := range arr {
		o, ok := it.(map[string]any)
		if !ok {
			t.Fatalf("itens[%d] = %#v, want objeto", i, it)
		}
		itens = append(itens, o)
	}
	return itens, proximoJSON(t, m["proximo"])
}

// listaUsuariosJSON decodifica uma página de GET /usuarios: itens {id, nome}.
func listaUsuariosJSON(t testing.TB, body []byte) (ids, nomes []string, proximo string) {
	t.Helper()
	itens, proximo := itensJSON(t, body)
	for i, o := range itens {
		exigirChaves(t, o, "id", "nome")
		n, ok := o["id"].(json.Number)
		if !ok {
			t.Fatalf("itens[%d].id = %#v, want número", i, o["id"])
		}
		s, ok := o["nome"].(string)
		if !ok {
			t.Fatalf("itens[%d].nome = %#v, want string", i, o["nome"])
		}
		ids = append(ids, n.String())
		nomes = append(nomes, s)
	}
	return ids, nomes, proximo
}

// TestListarUsuariosContrato: itens {id, nome} na ordem do repositório;
// proximo é o id do último item entregue quando há mais, null caso
// contrário. Ids não contíguos de propósito.
func TestListarUsuariosContrato(t *testing.T) {
	tests := []struct {
		name        string
		query       string
		ret         []domain.Usuario
		temMais     bool
		wantIDs     string
		wantProximo string
	}{
		{name: "vazio (nil do repositório)", ret: nil},
		{name: "vazio (slice vazia)", ret: []domain.Usuario{}},
		{name: "vazio com temMais não inventa proximo", ret: []domain.Usuario{}, temMais: true},
		{name: "página única sem mais", ret: usuarios(3, 8, 21), wantIDs: "3,8,21"},
		{name: "página com mais", query: "limite=3", ret: usuarios(3, 8, 21), temMais: true, wantIDs: "3,8,21", wantProximo: "21"},
		{name: "página com mais depois de apos", query: "limite=2&apos=8", ret: usuarios(21, 34), temMais: true, wantIDs: "21,34", wantProximo: "34"},
		{name: "um item com mais", query: "limite=1", ret: usuarios(42), temMais: true, wantIDs: "42", wantProximo: "42"},
		{name: "id grande", query: "limite=1", ret: usuarios(9223372036854775806), temMais: true, wantIDs: "9223372036854775806", wantProximo: "9223372036854775806"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			f.repo.listarUsuariosRet, f.repo.listarUsuariosTemMais = tt.ret, tt.temMais
			path := "/usuarios"
			if tt.query != "" {
				path += "?" + tt.query
			}
			rec := f.do(http.MethodGet, path, "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			checarCabecalhos(t, rec, true)
			ids, nomes, proximo := listaUsuariosJSON(t, rec.Body.Bytes())
			if got := strings.Join(ids, ","); got != tt.wantIDs {
				t.Errorf("ids = %s, want %s", got, tt.wantIDs)
			}
			for i, id := range ids {
				if nomes[i] != "U"+id {
					t.Errorf("nome de %s = %q, want %q", id, nomes[i], "U"+id)
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

// emprestimoEsperado é o DTO esperado de um item de GET /emprestimos.
type emprestimoEsperado struct {
	id, livroID, usuarioID, inicio, vencimento, devolucao, multa, renovacoes string
}

// checarItensEmprestimo confere cada item com o DTO de GET /emprestimos/{id}.
func checarItensEmprestimo(t *testing.T, itens []map[string]any, want []emprestimoEsperado) {
	t.Helper()
	if len(itens) != len(want) {
		t.Fatalf("%d itens, want %d", len(itens), len(want))
	}
	for i, it := range itens {
		b, err := json.Marshal(it)
		if err != nil {
			t.Fatalf("remarshal do item %d: %v", i, err)
		}
		w := want[i]
		t.Run(fmt.Sprintf("item %d", i), func(t *testing.T) {
			emprestimoJSONRenovacoes(t, b, w.id, w.livroID, w.usuarioID, w.inicio, w.vencimento, w.devolucao, w.multa, w.renovacoes)
		})
	}
}

// TestListarEmprestimosContrato: cada item é o DTO de GET /emprestimos/{id}
// (com multa e renovacoes); aberto calcula a multa com ref = agora, devolvido
// usa a devolução e ignora agora. proximo segue a D-19.
func TestListarEmprestimosContrato(t *testing.T) {
	// agora = 26/03/2026 12:00 SP: dois dias depois do vencimento padrão.
	agora := time.Date(2026, 3, 26, 12, 0, 0, 0, sp)
	pagina := func(t *testing.T) []domain.Emprestimo {
		return []domain.Emprestimo{
			emprestimo(t, 4, 1, 2, inicioPadrao(), time.Time{}),
			emprestimo(t, 9, 3, 2, inicioPadrao(), time.Date(2026, 3, 25, 8, 0, 0, 0, sp)),
			emprestimoRenovado(t, 15, 5, 6, inicioPadrao(), time.Date(2026, 4, 7, 12, 0, 0, 0, sp), 1),
		}
	}
	want := []emprestimoEsperado{
		{id: "4", livroID: "1", usuarioID: "2", inicio: "2026-03-10T13:00:00Z", vencimento: "2026-03-24", multa: "2.00", renovacoes: "0"},
		{id: "9", livroID: "3", usuarioID: "2", inicio: "2026-03-10T13:00:00Z", vencimento: "2026-03-24", devolucao: "2026-03-25T11:00:00Z", multa: "1.00", renovacoes: "0"},
		{id: "15", livroID: "5", usuarioID: "6", inicio: "2026-03-10T13:00:00Z", vencimento: "2026-04-07", multa: "0.00", renovacoes: "1"},
	}
	tests := []struct {
		name        string
		query       string
		ret         func(t *testing.T) []domain.Emprestimo
		temMais     bool
		want        []emprestimoEsperado
		wantProximo string
	}{
		{name: "vazio (nil do repositório)", ret: func(*testing.T) []domain.Emprestimo { return nil }},
		{name: "vazio (slice vazia)", ret: func(*testing.T) []domain.Emprestimo { return []domain.Emprestimo{} }},
		{name: "vazio com temMais não inventa proximo", ret: func(*testing.T) []domain.Emprestimo { return []domain.Emprestimo{} }, temMais: true},
		{name: "página sem mais", ret: pagina, want: want},
		{name: "página com mais", query: "limite=3", ret: pagina, temMais: true, want: want, wantProximo: "15"},
		{name: "um item com mais depois de apos", query: "limite=1&apos=4", ret: func(t *testing.T) []domain.Emprestimo { return pagina(t)[1:2] }, temMais: true, want: want[1:2], wantProximo: "9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			f.agora = agora
			f.repo.listarEmprRet, f.repo.listarEmprTemMais = tt.ret(t), tt.temMais
			path := "/emprestimos"
			if tt.query != "" {
				path += "?" + tt.query
			}
			rec := f.do(http.MethodGet, path, "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			checarCabecalhos(t, rec, true)
			itens, proximo := itensJSON(t, rec.Body.Bytes())
			checarItensEmprestimo(t, itens, tt.want)
			if proximo != tt.wantProximo {
				t.Errorf("proximo = %q, want %q (\"\" = null)", proximo, tt.wantProximo)
			}
			if len(tt.want) == 0 && !strings.Contains(rec.Body.String(), `"itens":[]`) {
				t.Errorf("corpo = %s, want \"itens\":[]", rec.Body.String())
			}
		})
	}
}

// relogioContado avança passo a cada leitura e conta as leituras.
type relogioContado struct {
	mu    sync.Mutex
	t     time.Time
	passo time.Duration
	lidas int
}

func (r *relogioContado) agora() time.Time {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.lidas++
	v := r.t
	r.t = r.t.Add(r.passo)
	return v
}

func (r *relogioContado) leituras() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.lidas
}

// TestListarEmprestimosInstanteUnico: o relógio é lido uma única vez por
// requisição, antes da chamada ao repositório, e esse instante vale para
// todos os itens da página. O relógio do teste anda 10 dias a cada leitura:
// uma leitura por item daria multas diferentes para empréstimos iguais.
func TestListarEmprestimosInstanteUnico(t *testing.T) {
	tests := []struct {
		name  string
		itens func(t *testing.T) []domain.Emprestimo
		want  []string // multas
	}{
		{name: "dois abertos com o mesmo vencimento", itens: func(t *testing.T) []domain.Emprestimo {
			return []domain.Emprestimo{
				emprestimo(t, 1, 1, 2, inicioPadrao(), time.Time{}),
				emprestimo(t, 2, 3, 4, inicioPadrao(), time.Time{}),
			}
		}, want: []string{"2.00", "2.00"}},
		{name: "devolvido entre dois abertos", itens: func(t *testing.T) []domain.Emprestimo {
			return []domain.Emprestimo{
				emprestimo(t, 1, 1, 2, inicioPadrao(), time.Time{}),
				emprestimo(t, 2, 3, 2, inicioPadrao(), time.Date(2026, 3, 25, 8, 0, 0, 0, sp)),
				emprestimo(t, 3, 5, 6, inicioPadrao(), time.Time{}),
			}
		}, want: []string{"2.00", "1.00", "2.00"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rel := &relogioContado{t: time.Date(2026, 3, 26, 12, 0, 0, 0, sp), passo: 10 * 24 * time.Hour}
			repo := &repoFake{listarEmprRet: tt.itens(t)}
			leiturasAntes := -1
			repo.aoListarEmpr = func() { leiturasAntes = rel.leituras() }
			h := httpapi.NovoHandler(httpapi.Dependencias{
				Repo:   repo,
				Pinger: &pingerFake{},
				Agora:  rel.agora,
				Token:  tokenTeste,
				Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
			})
			req := httptest.NewRequest(http.MethodGet, "/emprestimos", nil)
			req.Header.Set("Authorization", "Bearer "+tokenTeste)
			rec := httptest.NewRecorder()
			h.ServeHTTP(rec, req)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			if n := rel.leituras(); n != 1 {
				t.Errorf("relógio lido %d vezes, want 1 por requisição", n)
			}
			if leiturasAntes != 1 {
				t.Errorf("relógio lido %d vez(es) antes de ListarEmprestimos, want 1 (instante capturado antes da consulta)", leiturasAntes)
			}
			itens, _ := itensJSON(t, rec.Body.Bytes())
			if len(itens) != len(tt.want) {
				t.Fatalf("%d itens, want %d", len(itens), len(tt.want))
			}
			for i, it := range itens {
				if it["multa"] != tt.want[i] {
					t.Errorf("itens[%d].multa = %#v, want %q", i, it["multa"], tt.want[i])
				}
			}
		})
	}
}

// TestListarEmprestimosItemInvalido: erro ao montar qualquer item (multa de
// empréstimo inválido ou relógio zerado com empréstimo aberto) ou erro de
// reidratação vindo do repositório é 500 da página inteira: corpo só com o
// erro, sem itens parciais nem item pulado.
func TestListarEmprestimosItemInvalido(t *testing.T) {
	tests := []struct {
		name  string
		setup func(t *testing.T, f *fixture)
	}{
		{name: "empréstimo zero no meio da página", setup: func(t *testing.T, f *fixture) {
			f.repo.listarEmprRet = []domain.Emprestimo{
				emprestimo(t, 1, 1, 2, inicioPadrao(), time.Time{}),
				{},
				emprestimo(t, 3, 5, 6, inicioPadrao(), time.Time{}),
			}
		}},
		{name: "empréstimo zero no fim da página", setup: func(t *testing.T, f *fixture) {
			f.repo.listarEmprRet = []domain.Emprestimo{emprestimo(t, 1, 1, 2, inicioPadrao(), time.Time{}), {}}
			f.repo.listarEmprTemMais = true
		}},
		{name: "relógio zerado com aberto depois de devolvido", setup: func(t *testing.T, f *fixture) {
			f.agora = time.Time{}
			f.repo.listarEmprRet = []domain.Emprestimo{
				emprestimo(t, 1, 1, 2, inicioPadrao(), time.Date(2026, 3, 25, 8, 0, 0, 0, sp)),
				emprestimo(t, 2, 3, 2, inicioPadrao(), time.Time{}),
			}
		}},
		{name: "reidratação inválida no repositório", setup: func(t *testing.T, f *fixture) {
			f.repo.errListarEmpr = fmt.Errorf("storage: empréstimo gravado inválido: %w", domain.ErrVencimentoNaoPosteriorAoInicio)
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			tt.setup(t, f)
			rec := f.do(http.MethodGet, "/emprestimos", "", true)
			checarErro(t, rec, http.StatusInternalServerError, "erro_interno", true)
			for _, s := range []string{"itens", "proximo", "multa"} {
				if strings.Contains(rec.Body.String(), s) {
					t.Errorf("resposta 500 contém %q (página parcial): %s", s, rec.Body.String())
				}
			}
			semTextoInterno(t, "resposta", rec.Body.String())
			semTextoInterno(t, "log", f.logs.String())
		})
	}
}

// Erros do repositório nas rotas novas seguem o mapa de erros. O 404 de
// GET /usuarios/{id} é o código novo usuario_inexistente, decidido só no
// handler do detalhe; nas rotas de empréstimo ErrUsuarioNaoEncontrado
// continua usuario_nao_encontrado (422).
func TestRotasNovasMapaDeErros(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		body   string
		setup  func(f *fixture)
		status int
		codigo string
		msg    string
	}{
		{name: "usuarios: DeadlineExceeded", path: "/usuarios", setup: func(f *fixture) { f.repo.errListarUsuarios = fmt.Errorf("storage: %w", context.DeadlineExceeded) }, status: 503, codigo: "indisponivel"},
		{name: "usuarios: Canceled", path: "/usuarios", setup: func(f *fixture) { f.repo.errListarUsuarios = context.Canceled }, status: 503, codigo: "indisponivel"},
		{name: "usuarios: ErrListagemInvalida", path: "/usuarios", setup: func(f *fixture) { f.repo.errListarUsuarios = storage.ErrListagemInvalida }, status: 500, codigo: "erro_interno"},
		{name: "usuarios: PgError", path: "/usuarios", setup: func(f *fixture) { f.repo.errListarUsuarios = pgErroSecreto() }, status: 500, codigo: "erro_interno"},
		{name: "usuarios: desconhecido", path: "/usuarios", setup: func(f *fixture) { f.repo.errListarUsuarios = errors.New("host=segredo") }, status: 500, codigo: "erro_interno"},

		{name: "emprestimos: DeadlineExceeded", path: "/emprestimos", setup: func(f *fixture) { f.repo.errListarEmpr = fmt.Errorf("storage: %w", context.DeadlineExceeded) }, status: 503, codigo: "indisponivel"},
		{name: "emprestimos: ErrListagemInvalida", path: "/emprestimos", setup: func(f *fixture) { f.repo.errListarEmpr = storage.ErrListagemInvalida }, status: 500, codigo: "erro_interno"},
		{name: "emprestimos: PgError", path: "/emprestimos", setup: func(f *fixture) { f.repo.errListarEmpr = pgErroSecreto() }, status: 500, codigo: "erro_interno"},
		{name: "emprestimos: desconhecido", path: "/emprestimos", setup: func(f *fixture) { f.repo.errListarEmpr = errors.New("host=segredo") }, status: 500, codigo: "erro_interno"},

		{name: "usuario: inexistente", path: "/usuarios/9", setup: func(f *fixture) { f.repo.errBuscarUsuario = storage.ErrUsuarioNaoEncontrado }, status: 404, codigo: "usuario_inexistente", msg: "usuário não encontrado"},
		{name: "usuario: inexistente embrulhado", path: "/usuarios/9", setup: func(f *fixture) {
			f.repo.errBuscarUsuario = fmt.Errorf("x: %w", storage.ErrUsuarioNaoEncontrado)
		}, status: 404, codigo: "usuario_inexistente", msg: "usuário não encontrado"},
		{name: "usuario: DeadlineExceeded", path: "/usuarios/9", setup: func(f *fixture) { f.repo.errBuscarUsuario = fmt.Errorf("storage: %w", context.DeadlineExceeded) }, status: 503, codigo: "indisponivel"},
		{name: "usuario: PgError", path: "/usuarios/9", setup: func(f *fixture) { f.repo.errBuscarUsuario = pgErroSecreto() }, status: 500, codigo: "erro_interno"},
		{name: "usuario: ErrLivroNaoEncontrado não vira usuario_inexistente", path: "/usuarios/9", setup: func(f *fixture) { f.repo.errBuscarUsuario = storage.ErrLivroNaoEncontrado }, status: 404, codigo: "livro_nao_encontrado"},

		{name: "emprestimo com usuário inexistente continua 422", method: http.MethodPost, path: "/livros/1/emprestimo", body: `{"usuario_id":2}`,
			setup: func(f *fixture) { f.repo.errAbrir = storage.ErrUsuarioNaoEncontrado }, status: 422, codigo: "usuario_nao_encontrado", msg: "usuário não encontrado"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			tt.setup(f)
			method := tt.method
			if method == "" {
				method = http.MethodGet
			}
			rec := f.do(method, tt.path, tt.body, true)
			msg := checarErro(t, rec, tt.status, tt.codigo, true)
			if tt.msg != "" && msg != tt.msg {
				t.Errorf("mensagem = %q, want %q", msg, tt.msg)
			}
			semTextoInterno(t, "resposta", rec.Body.String())
			semTextoInterno(t, "log", f.logs.String())
		})
	}
}

// TestDetalheUsuarioParametroID: GET /usuarios/{id} usa a gramática de
// parseID; inválido é 400 id_invalido sem chamar o repositório.
func TestDetalheUsuarioParametroID(t *testing.T) {
	invalidos := []struct{ name, seg string }{
		{name: "abc", seg: "abc"},
		{name: "zero", seg: "0"},
		{name: "negativo", seg: "-1"},
		{name: "overflow", seg: "9223372036854775808"},
		{name: "espaço à esquerda", seg: "%201"},
		{name: "fracionário", seg: "1.5"},
		{name: "hexadecimal", seg: "0x10"},
		{name: "zeros", seg: "000"},
	}
	for _, iv := range invalidos {
		t.Run("invalido/"+iv.name, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodGet, "/usuarios/"+iv.seg, "", true)
			checarErro(t, rec, http.StatusBadRequest, "id_invalido", true)
			if f.repo.nChamadas() != 0 {
				t.Errorf("repositório chamado com id inválido: %v", f.repo.chamadas)
			}
		})
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
		t.Run("valido/"+v.seg, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodGet, "/usuarios/"+v.seg, "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			if got := strings.Join(f.repo.chamadas, ","); got != "BuscarUsuario" {
				t.Errorf("chamadas = %s, want BuscarUsuario", got)
			}
			if f.repo.usuarioIDRecebido != v.want {
				t.Errorf("BuscarUsuario(%d), want %d", f.repo.usuarioIDRecebido, v.want)
			}
		})
	}
}

// GET /usuarios/{id} responde exatamente {id, nome}, o DTO de POST /usuarios.
func TestContratoSaidaDetalheUsuario(t *testing.T) {
	f := novaFixture(t)
	f.repo.usuarioRet = &domain.Usuario{ID: 12, Nome: "Bento Santiago"}
	rec := f.do(http.MethodGet, "/usuarios/12", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
	}
	checarCabecalhos(t, rec, true)
	m := objetoJSON(t, rec.Body.Bytes())
	exigirChaves(t, m, "id", "nome")
	if m["id"] != json.Number("12") || m["nome"] != "Bento Santiago" {
		t.Errorf("usuario = %v, want id 12 e nome Bento Santiago", m)
	}
	if f.repo.usuarioIDRecebido != 12 {
		t.Errorf("BuscarUsuario(%d), want 12", f.repo.usuarioIDRecebido)
	}
}

// GET /usuarios/{id} ignora a query string, como as outras rotas de detalhe.
func TestDetalheUsuarioIgnoraQueryString(t *testing.T) {
	for _, p := range []string{"/usuarios/1?x=1", "/usuarios/1?limite=0", "/usuarios/1?%zz", "/usuarios/1?apos=-1;x"} {
		t.Run(p, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodGet, p, "", true)
			if rec.Code != http.StatusOK {
				t.Errorf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// A auth vem antes da validação da query e do {id} nas rotas novas.
func TestRotasNovasAuthAntesDeTudo(t *testing.T) {
	for _, p := range []string{"/usuarios?limite=0", "/usuarios?x=1", "/emprestimos?%zz", "/emprestimos?situacao=aberto", "/usuarios/abc", "/usuarios/0"} {
		t.Run(p, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodGet, p, "", false)
			checarErro(t, rec, http.StatusUnauthorized, "nao_autorizado", true)
			if f.repo.nChamadas() != 0 {
				t.Errorf("repositório chamado sem auth: %v", f.repo.chamadas)
			}
		})
	}
}

// Nas rotas novas, nenhum valor de limite/apos/query nem nome de usuário
// chega ao log, em sucesso ou erro.
func TestLogRotasNovasSemQueryNemNome(t *testing.T) {
	const nome = "NOME-SIGILOSO-QWERTY"
	tests := []struct {
		path  string
		setup func(t *testing.T, f *fixture)
	}{
		{path: "/usuarios?limite=77&apos=424242", setup: func(_ *testing.T, f *fixture) {
			f.repo.listarUsuariosRet = []domain.Usuario{{ID: 424243, Nome: nome}}
			f.repo.listarUsuariosTemMais = true
		}},
		{path: "/usuarios?apos=424242&QSSECRETO=1"},
		{path: "/usuarios/424242?QSSECRETO=1", setup: func(_ *testing.T, f *fixture) { f.repo.usuarioRet = &domain.Usuario{ID: 424242, Nome: nome} }},
		{path: "/usuarios/424242", setup: func(_ *testing.T, f *fixture) { f.repo.errBuscarUsuario = storage.ErrUsuarioNaoEncontrado }},
		{path: "/emprestimos?limite=77&apos=424242"},
		{path: "/emprestimos?limite=424242"},
		{path: "/emprestimos?apos=424242", setup: func(_ *testing.T, f *fixture) { f.repo.errListarEmpr = pgErroSecreto() }},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			f := novaFixture(t)
			if tt.setup != nil {
				tt.setup(t, f)
			}
			f.do(http.MethodGet, tt.path, "", true)
			if n := len(linhasLog(t, &f.logs)); n != 1 {
				t.Fatalf("%d linhas de log, want 1: %s", n, f.logs.String())
			}
			log := logEstavel(t, f)
			for _, s := range []string{"424242", "424243", "QSSECRETO", "limite", "apos", "?", nome} {
				if strings.Contains(log, s) {
					t.Errorf("log contém %q: %s", s, log)
				}
			}
		})
	}
}

// FuzzParametrosListagemNovasRotas manda query strings arbitrárias para
// GET /usuarios e GET /emprestimos com o mesmo oráculo de GET /livros:
// aceita chega ao repositório com os valores do oráculo; o resto é 400
// parametro_invalido sem chamar o repositório.
func FuzzParametrosListagemNovasRotas(f *testing.F) {
	seeds := []string{
		"", "limite=20", "apos=1", "limite=1&apos=9223372036854775807", "limite=100", "limite=101", "limite=0",
		"apos=0", "apos=-1", "limite=%2B5", "limite=+5", "x=1", "limite=1&limite=1", "%zz", "limite=5;apos=1",
		"situacao=aberto", "usuario_id=1", "livro_id=1", "&", "=", "limite=\x00",
	}
	for i, s := range seeds {
		f.Add(uint8(i), s)
	}
	f.Fuzz(func(t *testing.T, r uint8, raw string) {
		rota := listagensNovas[int(r)%len(listagensNovas)]
		fx := novaFixture(t)
		req := httptest.NewRequest(http.MethodGet, rota.path, nil)
		req.URL.RawQuery = raw
		req.Header.Set("Authorization", "Bearer "+tokenTeste)
		rec := httptest.NewRecorder()
		fx.handler().ServeHTTP(rec, req)

		apos, limite, ok := oraculoListagem(raw)
		if ok {
			if rec.Code != http.StatusOK {
				t.Fatalf("%s?%q: status = %d, want 200 (corpo %s)", rota.path, raw, rec.Code, rec.Body.String())
			}
			if got := strings.Join(fx.repo.chamadas, ","); got != rota.metodo || fx.repo.listarApos != apos || fx.repo.listarLimite != limite {
				t.Fatalf("%s?%q: chamadas %s com (%d, %d), want %s(%d, %d) uma vez",
					rota.path, raw, got, fx.repo.listarApos, fx.repo.listarLimite, rota.metodo, apos, limite)
			}
			return
		}
		if rec.Code != http.StatusBadRequest {
			t.Fatalf("%s?%q: status = %d, want 400 (corpo %s)", rota.path, raw, rec.Code, rec.Body.String())
		}
		if c, m := erroJSON(t, rec.Body.Bytes()); c != "parametro_invalido" || m != msgParametroInvalido {
			t.Fatalf("%s?%q: erro = %q %q, want parametro_invalido com mensagem fixa", rota.path, raw, c, m)
		}
		if fx.repo.nChamadas() != 0 {
			t.Fatalf("%s?%q: repositório chamado", rota.path, raw)
		}
	})
}
