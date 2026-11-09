package httpapi_test

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
	httpapi "github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/storage"
)

// DEL-015 (D-21): filtros situacao, usuario_id e livro_id em GET /emprestimos
// e a rota GET /usuarios/{id}/emprestimos.

// oraculoEmprestimos é a especificação independente da query das listagens
// de empréstimo: as regras de oraculoListagem (url.ParseQuery sem erro, cada
// chave uma vez, limite 1..100 padrão 20, apos > 0) mais situacao (aberto,
// devolvido ou vencido, comparação exata), livro_id e, fora da rota
// aninhada, usuario_id, ambos inteiros base 10 > 0. Ref não é preenchida.
func oraculoEmprestimos(raw string, aninhada bool) (f storage.FiltroEmprestimos, apos int64, limite int, ok bool) {
	vals, err := url.ParseQuery(raw)
	if err != nil {
		return f, 0, 0, false
	}
	limite = 20
	situacoes := map[string]storage.Situacao{
		"aberto":    storage.SituacaoAberto,
		"devolvido": storage.SituacaoDevolvido,
		"vencido":   storage.SituacaoVencido,
	}
	for k, vs := range vals {
		if len(vs) != 1 {
			return storage.FiltroEmprestimos{}, 0, 0, false
		}
		v := vs[0]
		n, errN := strconv.ParseInt(v, 10, 64)
		switch {
		case k == "limite":
			if errN != nil || n < 1 || n > 100 {
				return storage.FiltroEmprestimos{}, 0, 0, false
			}
			limite = int(n)
		case k == "apos":
			if errN != nil || n <= 0 {
				return storage.FiltroEmprestimos{}, 0, 0, false
			}
			apos = n
		case k == "situacao":
			s, existe := situacoes[v]
			if !existe {
				return storage.FiltroEmprestimos{}, 0, 0, false
			}
			f.Situacao = s
		case k == "livro_id":
			if errN != nil || n <= 0 {
				return storage.FiltroEmprestimos{}, 0, 0, false
			}
			f.LivroID = n
		case k == "usuario_id" && !aninhada:
			if errN != nil || n <= 0 {
				return storage.FiltroEmprestimos{}, 0, 0, false
			}
			f.UsuarioID = n
		default:
			return storage.FiltroEmprestimos{}, 0, 0, false
		}
	}
	return f, apos, limite, true
}

// mesmoFiltro compara usuário, livro e situação (Ref é conferida à parte).
func mesmoFiltro(a, b storage.FiltroEmprestimos) bool {
	return a.UsuarioID == b.UsuarioID && a.LivroID == b.LivroID && a.Situacao == b.Situacao
}

// casoQueryEmprestimos é um caso de query das listagens de empréstimo.
type casoQueryEmprestimos struct {
	name       string
	query      string
	wantErr    bool
	want       storage.FiltroEmprestimos // sem Ref
	wantApos   int64
	wantLimite int
}

// casosQueryComuns valem para GET /emprestimos e GET /usuarios/{id}/emprestimos
// (sem usuario_id).
func casosQueryComuns() []casoQueryEmprestimos {
	return []casoQueryEmprestimos{
		{name: "sem query", wantLimite: 20},
		{name: "situacao aberto", query: "situacao=aberto", want: storage.FiltroEmprestimos{Situacao: storage.SituacaoAberto}, wantLimite: 20},
		{name: "situacao devolvido", query: "situacao=devolvido", want: storage.FiltroEmprestimos{Situacao: storage.SituacaoDevolvido}, wantLimite: 20},
		{name: "situacao vencido", query: "situacao=vencido", want: storage.FiltroEmprestimos{Situacao: storage.SituacaoVencido}, wantLimite: 20},
		{name: "livro_id", query: "livro_id=7", want: storage.FiltroEmprestimos{LivroID: 7}, wantLimite: 20},
		{name: "livro_id com zeros à esquerda", query: "livro_id=007", want: storage.FiltroEmprestimos{LivroID: 7}, wantLimite: 20},
		{name: "livro_id com +", query: "livro_id=%2B5", want: storage.FiltroEmprestimos{LivroID: 5}, wantLimite: 20},
		{name: "livro_id máximo int64", query: "livro_id=9223372036854775807", want: storage.FiltroEmprestimos{LivroID: 9223372036854775807}, wantLimite: 20},
		{name: "livro e situação com paginação", query: "livro_id=3&situacao=vencido&limite=5&apos=40", want: storage.FiltroEmprestimos{LivroID: 3, Situacao: storage.SituacaoVencido}, wantApos: 40, wantLimite: 5},
		{name: "ordem das chaves não importa", query: "apos=40&situacao=devolvido&limite=100", want: storage.FiltroEmprestimos{Situacao: storage.SituacaoDevolvido}, wantApos: 40, wantLimite: 100},
		{name: "situacao com escape de letra", query: "situacao=%61berto", want: storage.FiltroEmprestimos{Situacao: storage.SituacaoAberto}, wantLimite: 20},

		{name: "situacao vazia", query: "situacao=", wantErr: true},
		{name: "situacao sem valor", query: "situacao", wantErr: true},
		{name: "situacao maiúscula", query: "situacao=ABERTO", wantErr: true},
		{name: "situacao capitalizada", query: "situacao=Aberto", wantErr: true},
		{name: "situacao com acento", query: "situacao=venc%C3%ADdo", wantErr: true},
		{name: "situacao desconhecida", query: "situacao=atrasado", wantErr: true},
		{name: "situacao todas não é valor", query: "situacao=todas", wantErr: true},
		{name: "situacao plural", query: "situacao=abertos", wantErr: true},
		{name: "situacao com espaço", query: "situacao=%20aberto", wantErr: true},
		{name: "situacao com espaço final", query: "situacao=aberto%20", wantErr: true},
		{name: "situacao com +", query: "situacao=aberto+", wantErr: true},
		{name: "situacao lista", query: "situacao=aberto,vencido", wantErr: true},
		{name: "situacao repetida igual", query: "situacao=aberto&situacao=aberto", wantErr: true},
		{name: "situacao repetida diferente", query: "situacao=aberto&situacao=vencido", wantErr: true},
		{name: "situacao com NUL", query: "situacao=aberto%00", wantErr: true},
		{name: "livro_id zero", query: "livro_id=0", wantErr: true},
		{name: "livro_id negativo", query: "livro_id=-1", wantErr: true},
		{name: "livro_id vazio", query: "livro_id=", wantErr: true},
		{name: "livro_id não numérico", query: "livro_id=abc", wantErr: true},
		{name: "livro_id fracionário", query: "livro_id=1.5", wantErr: true},
		{name: "livro_id hexadecimal", query: "livro_id=0x10", wantErr: true},
		{name: "livro_id overflow", query: "livro_id=9223372036854775808", wantErr: true},
		{name: "livro_id repetido", query: "livro_id=1&livro_id=1", wantErr: true},
		{name: "livro_id lista", query: "livro_id=1,2", wantErr: true},
		{name: "chave em maiúscula", query: "Situacao=aberto", wantErr: true},
		{name: "chave desconhecida com filtro válido", query: "situacao=aberto&x=1", wantErr: true},
		{name: "limite inválido com filtro válido", query: "situacao=aberto&limite=0", wantErr: true},
		{name: "apos inválido com filtro válido", query: "livro_id=1&apos=0", wantErr: true},
		{name: "ponto e vírgula", query: "situacao=aberto;livro_id=1", wantErr: true},
		{name: "escape inválido", query: "situacao=%zz", wantErr: true},
	}
}

// TestListarEmprestimosFiltrosParametros: GET /emprestimos aceita situacao,
// usuario_id e livro_id, combináveis entre si e com limite/apos, e passa ao
// repositório o filtro com Ref = o instante do relógio. Desvio é 400
// parametro_invalido sem chamar o repositório e sem eco.
func TestListarEmprestimosFiltrosParametros(t *testing.T) {
	tests := append(casosQueryComuns(),
		casoQueryEmprestimos{name: "usuario_id", query: "usuario_id=2", want: storage.FiltroEmprestimos{UsuarioID: 2}, wantLimite: 20},
		casoQueryEmprestimos{name: "usuario_id máximo int64", query: "usuario_id=9223372036854775807", want: storage.FiltroEmprestimos{UsuarioID: 9223372036854775807}, wantLimite: 20},
		casoQueryEmprestimos{name: "todos os filtros", query: "usuario_id=2&livro_id=3&situacao=aberto", want: storage.FiltroEmprestimos{UsuarioID: 2, LivroID: 3, Situacao: storage.SituacaoAberto}, wantLimite: 20},
		casoQueryEmprestimos{name: "todas as chaves", query: "usuario_id=2&livro_id=3&situacao=devolvido&limite=1&apos=9", want: storage.FiltroEmprestimos{UsuarioID: 2, LivroID: 3, Situacao: storage.SituacaoDevolvido}, wantApos: 9, wantLimite: 1},
		casoQueryEmprestimos{name: "usuario_id zero", query: "usuario_id=0", wantErr: true},
		casoQueryEmprestimos{name: "usuario_id negativo", query: "usuario_id=-2", wantErr: true},
		casoQueryEmprestimos{name: "usuario_id vazio", query: "usuario_id=", wantErr: true},
		casoQueryEmprestimos{name: "usuario_id overflow", query: "usuario_id=9223372036854775808", wantErr: true},
		casoQueryEmprestimos{name: "usuario_id repetido", query: "usuario_id=2&usuario_id=3", wantErr: true},
		casoQueryEmprestimos{name: "usuario_id não numérico", query: "usuario_id=dois", wantErr: true},
	)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			path := "/emprestimos"
			if tt.query != "" {
				path += "?" + tt.query
			}
			rec := f.do(http.MethodGet, path, "", true)
			checarQueryEmprestimos(t, f, rec, tt, "ListarEmprestimos")
		})
	}
}

// checarQueryEmprestimos confere o resultado de um casoQueryEmprestimos;
// wantChamadas é a sequência de chamadas ao repositório em sucesso.
func checarQueryEmprestimos(t *testing.T, f *fixture, rec *httptest.ResponseRecorder, tt casoQueryEmprestimos, wantChamadas string) {
	t.Helper()
	if tt.wantErr {
		msg := checarErro(t, rec, http.StatusBadRequest, "parametro_invalido", true)
		if msg != msgParametroInvalido {
			t.Errorf("mensagem = %q, want %q", msg, msgParametroInvalido)
		}
		if f.repo.nChamadas() != 0 {
			t.Errorf("repositório chamado com query inválida: %v", f.repo.chamadas)
		}
		for _, s := range []string{"situacao", "usuario_id", "livro_id", "limite", "apos", "aberto", "vencido", "ABERTO", "atrasado"} {
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
	if got := strings.Join(f.repo.chamadas, ","); got != wantChamadas {
		t.Fatalf("chamadas = %s, want %s", got, wantChamadas)
	}
	got := f.repo.listarEmprFiltro
	if !mesmoFiltro(got, tt.want) {
		t.Errorf("filtro = {UsuarioID:%d LivroID:%d Situacao:%v}, want {UsuarioID:%d LivroID:%d Situacao:%v}",
			got.UsuarioID, got.LivroID, got.Situacao, tt.want.UsuarioID, tt.want.LivroID, tt.want.Situacao)
	}
	if !got.Ref.Equal(f.agora) {
		t.Errorf("filtro.Ref = %v, want o instante do relógio %v", got.Ref, f.agora)
	}
	if f.repo.listarApos != tt.wantApos || f.repo.listarLimite != tt.wantLimite {
		t.Errorf("ListarEmprestimos(apos=%d, limite=%d), want (%d, %d)", f.repo.listarApos, f.repo.listarLimite, tt.wantApos, tt.wantLimite)
	}
}

// TestEmprestimosDoUsuarioParametros: GET /usuarios/{id}/emprestimos aceita
// limite, apos, situacao e livro_id; usuario_id na query é 400 (o usuário vem
// do path). Em sucesso: BuscarUsuario(id) e depois ListarEmprestimos com
// UsuarioID = id do path.
func TestEmprestimosDoUsuarioParametros(t *testing.T) {
	tests := append(casosQueryComuns(),
		casoQueryEmprestimos{name: "usuario_id na query", query: "usuario_id=12", wantErr: true},
		casoQueryEmprestimos{name: "usuario_id igual ao do path", query: "usuario_id=12", wantErr: true},
		casoQueryEmprestimos{name: "usuario_id inválido na query", query: "usuario_id=0", wantErr: true},
		casoQueryEmprestimos{name: "usuario_id vazio na query", query: "usuario_id=", wantErr: true},
		casoQueryEmprestimos{name: "usuario_id com filtros válidos", query: "situacao=aberto&usuario_id=12&livro_id=3", wantErr: true},
	)
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			path := "/usuarios/12/emprestimos"
			if tt.query != "" {
				path += "?" + tt.query
			}
			rec := f.do(http.MethodGet, path, "", true)
			tt.want.UsuarioID = 12
			checarQueryEmprestimos(t, f, rec, tt, "BuscarUsuario,ListarEmprestimos")
			if !tt.wantErr && f.repo.usuarioIDRecebido != 12 {
				t.Errorf("BuscarUsuario(%d), want 12", f.repo.usuarioIDRecebido)
			}
		})
	}
}

// O {id} da rota aninhada usa a gramática de parseID.
func TestEmprestimosDoUsuarioParametroID(t *testing.T) {
	invalidos := []string{"abc", "0", "-1", "9223372036854775808", "%201", "1.5", "0x10", "000"}
	for _, seg := range invalidos {
		t.Run("invalido/"+seg, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodGet, "/usuarios/"+seg+"/emprestimos", "", true)
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
			rec := f.do(http.MethodGet, "/usuarios/"+v.seg+"/emprestimos", "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			if f.repo.usuarioIDRecebido != v.want || f.repo.listarEmprFiltro.UsuarioID != v.want {
				t.Errorf("BuscarUsuario(%d) e filtro.UsuarioID %d, want %d", f.repo.usuarioIDRecebido, f.repo.listarEmprFiltro.UsuarioID, v.want)
			}
		})
	}
}

// Ordem dos erros na rota aninhada: id_invalido, depois parametro_invalido,
// depois usuario_inexistente. Os dois primeiros não chamam o repositório.
func TestEmprestimosDoUsuarioOrdemDeErros(t *testing.T) {
	tests := []struct {
		name         string
		path         string
		inexistente  bool
		status       int
		codigo       string
		wantChamadas string
	}{
		{name: "id inválido e query inválida", path: "/usuarios/abc/emprestimos?situacao=", status: 400, codigo: "id_invalido"},
		{name: "id inválido, query inválida e usuário inexistente", path: "/usuarios/0/emprestimos?usuario_id=1", inexistente: true, status: 400, codigo: "id_invalido"},
		{name: "id inválido e query válida", path: "/usuarios/-1/emprestimos?situacao=aberto", status: 400, codigo: "id_invalido"},
		{name: "query inválida e usuário inexistente", path: "/usuarios/9/emprestimos?situacao=", inexistente: true, status: 400, codigo: "parametro_invalido"},
		{name: "usuario_id na query e usuário inexistente", path: "/usuarios/9/emprestimos?usuario_id=9", inexistente: true, status: 400, codigo: "parametro_invalido"},
		{name: "query válida e usuário inexistente", path: "/usuarios/9/emprestimos?situacao=vencido", inexistente: true, status: 404, codigo: "usuario_inexistente", wantChamadas: "BuscarUsuario"},
		{name: "sem query e usuário inexistente", path: "/usuarios/9/emprestimos", inexistente: true, status: 404, codigo: "usuario_inexistente", wantChamadas: "BuscarUsuario"},
		{name: "página seguinte e usuário inexistente", path: "/usuarios/9/emprestimos?apos=50&limite=10", inexistente: true, status: 404, codigo: "usuario_inexistente", wantChamadas: "BuscarUsuario"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			if tt.inexistente {
				f.repo.errBuscarUsuario = storage.ErrUsuarioNaoEncontrado
			}
			rec := f.do(http.MethodGet, tt.path, "", true)
			msg := checarErro(t, rec, tt.status, tt.codigo, true)
			if tt.codigo == "usuario_inexistente" && msg != "usuário não encontrado" {
				t.Errorf("mensagem = %q, want usuário não encontrado", msg)
			}
			if got := strings.Join(f.repo.chamadas, ","); got != tt.wantChamadas {
				t.Errorf("chamadas = %q, want %q", got, tt.wantChamadas)
			}
		})
	}
}

// Erros do repositório na rota aninhada: ErrUsuarioNaoEncontrado (também
// embrulhado) é 404 usuario_inexistente, nunca 422 usuario_nao_encontrado;
// os demais seguem o mapa de erros; erro em BuscarUsuario não chama
// ListarEmprestimos.
func TestEmprestimosDoUsuarioMapaDeErros(t *testing.T) {
	tests := []struct {
		name         string
		setup        func(f *fixture)
		status       int
		codigo       string
		wantChamadas string
	}{
		{name: "BuscarUsuario: inexistente embrulhado", setup: func(f *fixture) {
			f.repo.errBuscarUsuario = fmt.Errorf("x: %w", storage.ErrUsuarioNaoEncontrado)
		}, status: 404, codigo: "usuario_inexistente", wantChamadas: "BuscarUsuario"},
		{name: "BuscarUsuario: DeadlineExceeded", setup: func(f *fixture) {
			f.repo.errBuscarUsuario = fmt.Errorf("storage: %w", context.DeadlineExceeded)
		}, status: 503, codigo: "indisponivel", wantChamadas: "BuscarUsuario"},
		{name: "BuscarUsuario: PgError", setup: func(f *fixture) { f.repo.errBuscarUsuario = pgErroSecreto() }, status: 500, codigo: "erro_interno", wantChamadas: "BuscarUsuario"},
		{name: "BuscarUsuario: desconhecido", setup: func(f *fixture) { f.repo.errBuscarUsuario = errors.New("host=segredo") }, status: 500, codigo: "erro_interno", wantChamadas: "BuscarUsuario"},
		{name: "ListarEmprestimos: ErrUsuarioNaoEncontrado não vira 404 do detalhe", setup: func(f *fixture) {
			f.repo.errListarEmpr = storage.ErrUsuarioNaoEncontrado
		}, status: 422, codigo: "usuario_nao_encontrado", wantChamadas: "BuscarUsuario,ListarEmprestimos"},
		{name: "ListarEmprestimos: DeadlineExceeded", setup: func(f *fixture) {
			f.repo.errListarEmpr = fmt.Errorf("storage: %w", context.DeadlineExceeded)
		}, status: 503, codigo: "indisponivel", wantChamadas: "BuscarUsuario,ListarEmprestimos"},
		{name: "ListarEmprestimos: ErrListagemInvalida", setup: func(f *fixture) { f.repo.errListarEmpr = storage.ErrListagemInvalida }, status: 500, codigo: "erro_interno", wantChamadas: "BuscarUsuario,ListarEmprestimos"},
		{name: "ListarEmprestimos: PgError", setup: func(f *fixture) { f.repo.errListarEmpr = pgErroSecreto() }, status: 500, codigo: "erro_interno", wantChamadas: "BuscarUsuario,ListarEmprestimos"},
		{name: "ListarEmprestimos: reidratação inválida", setup: func(f *fixture) {
			f.repo.errListarEmpr = fmt.Errorf("storage: empréstimo gravado inválido: %w", domain.ErrVencimentoNaoPosteriorAoInicio)
		}, status: 500, codigo: "erro_interno", wantChamadas: "BuscarUsuario,ListarEmprestimos"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			tt.setup(f)
			rec := f.do(http.MethodGet, "/usuarios/9/emprestimos?situacao=aberto", "", true)
			checarErro(t, rec, tt.status, tt.codigo, true)
			if got := strings.Join(f.repo.chamadas, ","); got != tt.wantChamadas {
				t.Errorf("chamadas = %s, want %s", got, tt.wantChamadas)
			}
			for _, s := range []string{"itens", "proximo"} {
				if strings.Contains(rec.Body.String(), s) {
					t.Errorf("resposta de erro contém %q: %s", s, rec.Body.String())
				}
			}
			semTextoInterno(t, "resposta", rec.Body.String())
			semTextoInterno(t, "log", f.logs.String())
		})
	}
}

// As duas operações da rota aninhada correm com prazo (um contexto com o
// timeout da operação).
func TestEmprestimosDoUsuarioPrazo(t *testing.T) {
	f := novaFixture(t)
	rec := f.do(http.MethodGet, "/usuarios/9/emprestimos", "", true)
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
	}
	if len(f.repo.prazos) != 2 {
		t.Fatalf("%d chamadas, want 2 (%v)", len(f.repo.prazos), f.repo.chamadas)
	}
	for i, p := range f.repo.prazos {
		if p <= 4*time.Second || p > 5*time.Second {
			t.Errorf("chamada %s: prazo = %v, want (4s, 5s]", f.repo.chamadas[i], p)
		}
	}
}

// Página da rota aninhada: mesmo contrato de GET /emprestimos (DTO de
// GET /emprestimos/{id}, multa com ref = agora, proximo da D-19).
func TestEmprestimosDoUsuarioContrato(t *testing.T) {
	agora := time.Date(2026, 3, 26, 12, 0, 0, 0, sp)
	pagina := func(t *testing.T) []domain.Emprestimo {
		return []domain.Emprestimo{
			emprestimo(t, 4, 1, 2, inicioPadrao(), time.Time{}),
			emprestimo(t, 9, 3, 2, inicioPadrao(), time.Date(2026, 3, 25, 8, 0, 0, 0, sp)),
		}
	}
	want := []emprestimoEsperado{
		{id: "4", livroID: "1", usuarioID: "2", inicio: "2026-03-10T13:00:00Z", vencimento: "2026-03-24", multa: "2.00", renovacoes: "0"},
		{id: "9", livroID: "3", usuarioID: "2", inicio: "2026-03-10T13:00:00Z", vencimento: "2026-03-24", devolucao: "2026-03-25T11:00:00Z", multa: "1.00", renovacoes: "0"},
	}
	tests := []struct {
		name        string
		query       string
		ret         func(t *testing.T) []domain.Emprestimo
		temMais     bool
		want        []emprestimoEsperado
		wantProximo string
	}{
		{name: "usuário sem empréstimos (nil)", ret: func(*testing.T) []domain.Emprestimo { return nil }},
		{name: "vazio com temMais não inventa proximo", ret: func(*testing.T) []domain.Emprestimo { return []domain.Emprestimo{} }, temMais: true},
		{name: "página sem mais", ret: pagina, want: want},
		{name: "página com mais", query: "limite=2", ret: pagina, temMais: true, want: want, wantProximo: "9"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			f.agora = agora
			f.repo.listarEmprRet, f.repo.listarEmprTemMais = tt.ret(t), tt.temMais
			path := "/usuarios/2/emprestimos"
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

// Nas duas rotas, o relógio é lido uma única vez, antes de ListarEmprestimos;
// o mesmo instante vai em filtro.Ref e na multa de todos os itens (A7: um
// item filtrado como vencido nunca sai com multa zero na mesma página).
func TestListagemFiltradaInstanteUnico(t *testing.T) {
	for _, path := range []string{
		"/emprestimos?situacao=vencido",
		"/emprestimos?situacao=aberto&usuario_id=2",
		"/emprestimos",
		"/usuarios/2/emprestimos?situacao=vencido",
		"/usuarios/2/emprestimos",
	} {
		t.Run(path, func(t *testing.T) {
			inicio := time.Date(2026, 3, 26, 12, 0, 0, 0, sp)
			rel := &relogioContado{t: inicio, passo: 10 * 24 * time.Hour}
			repo := &repoFake{listarEmprRet: []domain.Emprestimo{
				emprestimo(t, 1, 1, 2, inicioPadrao(), time.Time{}),
				emprestimo(t, 2, 3, 2, inicioPadrao(), time.Time{}),
			}}
			leiturasAntes := -1
			repo.aoListarEmpr = func() { leiturasAntes = rel.leituras() }
			h := httpapi.NovoHandler(httpapi.Dependencias{
				Repo:   repo,
				Pinger: &pingerFake{},
				Agora:  rel.agora,
				Token:  tokenTeste,
				Logger: slog.New(slog.NewJSONHandler(io.Discard, nil)),
			})
			req := httptest.NewRequest(http.MethodGet, path, nil)
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
				t.Errorf("relógio lido %d vez(es) antes de ListarEmprestimos, want 1", leiturasAntes)
			}
			if !repo.listarEmprFiltro.Ref.Equal(inicio) {
				t.Errorf("filtro.Ref = %v, want o instante lido %v", repo.listarEmprFiltro.Ref, inicio)
			}
			itens, _ := itensJSON(t, rec.Body.Bytes())
			if len(itens) != 2 {
				t.Fatalf("%d itens, want 2", len(itens))
			}
			for i, it := range itens {
				if it["multa"] != "2.00" {
					t.Errorf("itens[%d].multa = %#v, want 2.00 (mesmo instante do filtro)", i, it["multa"])
				}
			}
		})
	}
}

// A auth vem antes da validação do {id} e da query na rota aninhada e nos
// filtros novos.
func TestFiltrosEmprestimosAuthAntesDeTudo(t *testing.T) {
	for _, p := range []string{
		"/usuarios/abc/emprestimos", "/usuarios/0/emprestimos?situacao=", "/usuarios/1/emprestimos",
		"/usuarios/1/emprestimos?usuario_id=1", "/emprestimos?situacao=", "/emprestimos?usuario_id=0", "/emprestimos?livro_id=1",
	} {
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

// Nenhum valor de filtro, id do path, query nem URL chega ao log, em sucesso
// ou erro.
func TestLogFiltrosEmprestimosSemValores(t *testing.T) {
	tests := []struct {
		path  string
		setup func(f *fixture)
	}{
		{path: "/emprestimos?usuario_id=424242&livro_id=535353&situacao=vencido"},
		{path: "/emprestimos?usuario_id=424242&situacao=atrasado"},
		{path: "/emprestimos?livro_id=535353", setup: func(f *fixture) { f.repo.errListarEmpr = pgErroSecreto() }},
		{path: "/usuarios/424242/emprestimos?livro_id=535353&situacao=devolvido"},
		{path: "/usuarios/424242/emprestimos?usuario_id=535353"},
		{path: "/usuarios/424242/emprestimos?situacao=vencido", setup: func(f *fixture) { f.repo.errBuscarUsuario = storage.ErrUsuarioNaoEncontrado }},
	}
	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			f := novaFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			f.do(http.MethodGet, tt.path, "", true)
			if n := len(linhasLog(t, &f.logs)); n != 1 {
				t.Fatalf("%d linhas de log, want 1: %s", n, f.logs.String())
			}
			log := logEstavel(t, f)
			for _, s := range []string{"424242", "535353", "situacao", "usuario_id", "livro_id", "vencido", "devolvido", "atrasado", "?"} {
				if strings.Contains(log, s) {
					t.Errorf("log contém %q: %s", s, log)
				}
			}
		})
	}
}

// GET /livros e GET /usuarios continuam recusando as chaves novas.
func TestFiltrosEmprestimosNaoValemEmOutrasListagens(t *testing.T) {
	for _, base := range []string{"/livros", "/usuarios"} {
		for _, q := range []string{"situacao=aberto", "usuario_id=1", "livro_id=1", "limite=5&situacao=vencido"} {
			t.Run(base+"?"+q, func(t *testing.T) {
				f := novaFixture(t)
				rec := f.do(http.MethodGet, base+"?"+q, "", true)
				checarErro(t, rec, http.StatusBadRequest, "parametro_invalido", true)
				if f.repo.nChamadas() != 0 {
					t.Errorf("repositório chamado: %v", f.repo.chamadas)
				}
			})
		}
	}
}

// rotasFiltroEmprestimos são as rotas que aceitam os filtros de empréstimo.
var rotasFiltroEmprestimos = []struct {
	path     string
	aninhada bool
}{
	{path: "/emprestimos"},
	{path: "/usuarios/31/emprestimos", aninhada: true},
}

// FuzzParametrosEmprestimos manda query strings arbitrárias às duas rotas de
// empréstimos. Aceita (pelo oráculo) chega ao repositório com o filtro, apos
// e limite do oráculo, ids > 0, situação conhecida e limite 1..100; o resto é
// 400 parametro_invalido sem chamar o repositório. Nunca outro status.
func FuzzParametrosEmprestimos(f *testing.F) {
	seeds := []string{
		"", "situacao=aberto", "situacao=devolvido", "situacao=vencido", "situacao=", "situacao", "situacao=ABERTO",
		"situacao=aberto,vencido", "situacao=aberto&situacao=aberto", "situacao=venc%C3%ADdo", "situacao=%61berto",
		"usuario_id=1", "usuario_id=0", "usuario_id=-1", "usuario_id=9223372036854775807", "usuario_id=9223372036854775808",
		"livro_id=1", "livro_id=007", "livro_id=%2B5", "livro_id=+5", "livro_id=1e3",
		"usuario_id=2&livro_id=3&situacao=vencido&limite=100&apos=1", "limite=101&situacao=aberto", "situacao=aberto;livro_id=1",
		"%zz", "&", "=", "situacao=aberto\x00", "x=1&situacao=aberto", "situacao=aberto#frag",
	}
	for i, s := range seeds {
		f.Add(uint8(i), s)
	}
	f.Fuzz(func(t *testing.T, r uint8, raw string) {
		rota := rotasFiltroEmprestimos[int(r)%len(rotasFiltroEmprestimos)]
		fx := novaFixture(t)
		req := httptest.NewRequest(http.MethodGet, rota.path, nil)
		req.URL.RawQuery = raw
		req.Header.Set("Authorization", "Bearer "+tokenTeste)
		rec := httptest.NewRecorder()
		fx.handler().ServeHTTP(rec, req)

		want, apos, limite, ok := oraculoEmprestimos(raw, rota.aninhada)
		if rota.aninhada {
			want.UsuarioID = 31
		}
		if ok {
			if rec.Code != http.StatusOK {
				t.Fatalf("%s?%q: status = %d, want 200 (corpo %s)", rota.path, raw, rec.Code, rec.Body.String())
			}
			got := fx.repo.listarEmprFiltro
			if !mesmoFiltro(got, want) || !got.Ref.Equal(fx.agora) || fx.repo.listarApos != apos || fx.repo.listarLimite != limite {
				t.Fatalf("%s?%q: ListarEmprestimos(%+v, %d, %d), want (%+v com Ref %v, %d, %d)",
					rota.path, raw, got, fx.repo.listarApos, fx.repo.listarLimite, want, fx.agora, apos, limite)
			}
			if got.UsuarioID < 0 || got.LivroID < 0 || fx.repo.listarApos < 0 || limite < 1 || limite > 100 {
				t.Fatalf("%s?%q: valores fora do domínio: %+v", rota.path, raw, got)
			}
			switch got.Situacao {
			case storage.SituacaoTodas, storage.SituacaoAberto, storage.SituacaoDevolvido, storage.SituacaoVencido:
			default:
				t.Fatalf("%s?%q: situação desconhecida %v", rota.path, raw, got.Situacao)
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
			t.Fatalf("%s?%q: repositório chamado: %v", rota.path, raw, fx.repo.chamadas)
		}
	})
}
