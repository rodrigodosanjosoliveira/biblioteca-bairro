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

	"github.com/jackc/pgx/v5/pgconn"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/storage"
)

// pgErroSecreto é um PgError com texto que não pode vazar.
func pgErroSecreto() error {
	return fmt.Errorf("storage: erro do banco (SQLSTATE 23514): %w", &pgconn.PgError{
		Severity:       "ERROR",
		Code:           "23514",
		Message:        "SEGREDO-MSG",
		Detail:         "SEGREDO-DETAIL",
		Hint:           "SEGREDO-HINT",
		Where:          "SEGREDO-WHERE",
		SchemaName:     "public",
		TableName:      "livro",
		ConstraintName: "livro_titulo_check",
	})
}

// TestMapaDeErros cobre cada linha do mapa de erros. A mensagem é fixa por
// código: o mesmo código sempre traz a mesma mensagem.
func TestMapaDeErros(t *testing.T) {
	const (
		livros    = "/livros"
		usuarios  = "/usuarios"
		empr      = "/livros/1/emprestimo"
		devolucao = "/livros/1/devolucao"
	)
	corpoEmpr := `{"usuario_id":2}`
	corpoLivro := `{"titulo":"Dom Casmurro"}`
	corpoUsuario := `{"nome":"Capitu"}`
	envolver := func(e error) error { return fmt.Errorf("camada: %w", e) }

	tests := []struct {
		name   string
		method string
		path   string
		body   string
		setup  func(f *fixture)
		status int
		codigo string
		// semRepo exige que o repositório não seja chamado.
		semRepo bool
	}{
		// 400 corpo_invalido
		{name: "json ruim", method: http.MethodPost, path: livros, body: `{"titulo":`, status: 400, codigo: "corpo_invalido", semRepo: true},
		{name: "campo desconhecido", method: http.MethodPost, path: livros, body: `{"titulo":"a","autor":"b"}`, status: 400, codigo: "corpo_invalido", semRepo: true},
		{name: "dado extra depois do objeto", method: http.MethodPost, path: livros, body: `{"titulo":"a"}{"titulo":"b"}`, status: 400, codigo: "corpo_invalido", semRepo: true},
		{name: "corpo na devolucao", method: http.MethodPost, path: devolucao, body: `{}`, status: 400, codigo: "corpo_invalido", semRepo: true},

		// 422 de validação
		{name: "titulo vazio pelo corpo", method: http.MethodPost, path: livros, body: `{"titulo":"   "}`, status: 422, codigo: "titulo_vazio"},
		{name: "ErrTituloVazio do repositorio", method: http.MethodPost, path: livros, body: corpoLivro, setup: func(f *fixture) { f.repo.errInserirLivro = envolver(domain.ErrTituloVazio) }, status: 422, codigo: "titulo_vazio"},
		{name: "nome vazio pelo corpo", method: http.MethodPost, path: usuarios, body: `{"nome":""}`, status: 422, codigo: "nome_vazio"},
		{name: "ErrNomeVazio do repositorio", method: http.MethodPost, path: usuarios, body: corpoUsuario, setup: func(f *fixture) { f.repo.errInserirUsuario = domain.ErrNomeVazio }, status: 422, codigo: "nome_vazio"},
		{name: "NUL no titulo", method: http.MethodPost, path: livros, body: `{"titulo":"Dom\u0000Casmurro"}`, status: 422, codigo: "texto_invalido", semRepo: true},
		{name: "NUL no nome", method: http.MethodPost, path: usuarios, body: `{"nome":"\u0000"}`, status: 422, codigo: "texto_invalido", semRepo: true},
		{name: "usuario_id zero", method: http.MethodPost, path: empr, body: `{"usuario_id":0}`, status: 422, codigo: "usuario_id_invalido", semRepo: true},
		{name: "ErrUsuarioNaoEncontrado", method: http.MethodPost, path: empr, body: corpoEmpr, setup: func(f *fixture) { f.repo.errAbrir = storage.ErrUsuarioNaoEncontrado }, status: 422, codigo: "usuario_nao_encontrado"},

		// 404
		{name: "ErrLivroNaoEncontrado", method: http.MethodPost, path: empr, body: corpoEmpr, setup: func(f *fixture) { f.repo.errAbrir = storage.ErrLivroNaoEncontrado }, status: 404, codigo: "livro_nao_encontrado"},
		{name: "ErrEmprestimoNaoEncontrado no GET", method: http.MethodGet, path: empr, setup: func(f *fixture) { f.repo.errBuscar = storage.ErrEmprestimoNaoEncontrado }, status: 404, codigo: "emprestimo_aberto_nao_encontrado"},
		{name: "ErrEmprestimoNaoEncontrado na busca da devolucao", method: http.MethodPost, path: devolucao, setup: func(f *fixture) { f.repo.errBuscar = storage.ErrEmprestimoNaoEncontrado }, status: 404, codigo: "emprestimo_aberto_nao_encontrado"},
		{name: "ErrEmprestimoNaoEncontrado ao registrar devolucao", method: http.MethodPost, path: devolucao, setup: func(f *fixture) { f.repo.errRegistrar = storage.ErrEmprestimoNaoEncontrado }, status: 404, codigo: "emprestimo_aberto_nao_encontrado"},

		// 409
		{name: "ErrLivroJaEmprestado", method: http.MethodPost, path: empr, body: corpoEmpr, setup: func(f *fixture) { f.repo.errAbrir = storage.ErrLivroJaEmprestado }, status: 409, codigo: "livro_ja_emprestado"},
		{name: "ErrLivroJaEmprestado embrulhado", method: http.MethodPost, path: empr, body: corpoEmpr, setup: func(f *fixture) { f.repo.errAbrir = envolver(storage.ErrLivroJaEmprestado) }, status: 409, codigo: "livro_ja_emprestado"},
		{name: "ErrEmprestimoJaDevolvido do repositorio", method: http.MethodPost, path: devolucao, setup: func(f *fixture) { f.repo.errRegistrar = domain.ErrEmprestimoJaDevolvido }, status: 409, codigo: "emprestimo_ja_devolvido"},
		{name: "ErrEmprestimoJaDevolvido do Devolver", method: http.MethodPost, path: devolucao, setup: func(f *fixture) {
			f.repo.buscarRet = emprestimo(t, 77, 1, 2, inicioPadrao(), inicioPadrao().Add(time.Hour))
		}, status: 409, codigo: "emprestimo_ja_devolvido"},
		{name: "ErrEmprestimoDivergente", method: http.MethodPost, path: devolucao, setup: func(f *fixture) { f.repo.errRegistrar = storage.ErrEmprestimoDivergente }, status: 409, codigo: "emprestimo_alterado"},

		// 503
		{name: "DeadlineExceeded em livros", method: http.MethodPost, path: livros, body: corpoLivro, setup: func(f *fixture) { f.repo.errInserirLivro = fmt.Errorf("storage: %w", context.DeadlineExceeded) }, status: 503, codigo: "indisponivel"},
		{name: "Canceled em usuarios", method: http.MethodPost, path: usuarios, body: corpoUsuario, setup: func(f *fixture) { f.repo.errInserirUsuario = fmt.Errorf("storage: %w", context.Canceled) }, status: 503, codigo: "indisponivel"},
		{name: "DeadlineExceeded no emprestimo", method: http.MethodPost, path: empr, body: corpoEmpr, setup: func(f *fixture) { f.repo.errAbrir = context.DeadlineExceeded }, status: 503, codigo: "indisponivel"},
		{name: "DeadlineExceeded no GET", method: http.MethodGet, path: empr, setup: func(f *fixture) { f.repo.errBuscar = context.DeadlineExceeded }, status: 503, codigo: "indisponivel"},
		{name: "Canceled ao registrar devolucao", method: http.MethodPost, path: devolucao, setup: func(f *fixture) { f.repo.errRegistrar = context.Canceled }, status: 503, codigo: "indisponivel"},

		// 500
		{name: "ErrDevolucaoAntesDoInicio do Devolver", method: http.MethodPost, path: devolucao, setup: func(f *fixture) { f.agora = inicioPadrao().AddDate(0, 0, -1) }, status: 500, codigo: "erro_interno"},
		{name: "ErrDevolucaoAntesDoInicio do repositorio", method: http.MethodPost, path: devolucao, setup: func(f *fixture) { f.repo.errRegistrar = domain.ErrDevolucaoAntesDoInicio }, status: 500, codigo: "erro_interno"},
		{name: "ErrJaPersistido", method: http.MethodPost, path: livros, body: corpoLivro, setup: func(f *fixture) { f.repo.errInserirLivro = storage.ErrJaPersistido }, status: 500, codigo: "erro_interno"},
		{name: "ErrEmprestimoInvalido", method: http.MethodPost, path: empr, body: corpoEmpr, setup: func(f *fixture) { f.repo.errAbrir = domain.ErrEmprestimoInvalido }, status: 500, codigo: "erro_interno"},
		{name: "ErrReferenciaZero com relogio zerado", method: http.MethodGet, path: empr, setup: func(f *fixture) { f.agora = time.Time{} }, status: 500, codigo: "erro_interno"},
		{name: "relogio zerado no emprestimo", method: http.MethodPost, path: empr, body: corpoEmpr, setup: func(f *fixture) { f.agora = time.Time{} }, status: 500, codigo: "erro_interno"},
		{name: "relogio zerado na devolucao", method: http.MethodPost, path: devolucao, setup: func(f *fixture) { f.agora = time.Time{} }, status: 500, codigo: "erro_interno"},
		{name: "ErrLivroIDInvalido", method: http.MethodPost, path: empr, body: corpoEmpr, setup: func(f *fixture) { f.repo.errAbrir = domain.ErrLivroIDInvalido }, status: 500, codigo: "erro_interno"},
		{name: "erroBanco com PgError", method: http.MethodPost, path: usuarios, body: corpoUsuario, setup: func(f *fixture) { f.repo.errInserirUsuario = pgErroSecreto() }, status: 500, codigo: "erro_interno"},
		{name: "erro desconhecido", method: http.MethodGet, path: empr, setup: func(f *fixture) { f.repo.errBuscar = errors.New("dial tcp host=segredo user=segredo") }, status: 500, codigo: "erro_interno"},
		{name: "erro desconhecido no registro", method: http.MethodPost, path: devolucao, setup: func(f *fixture) { f.repo.errRegistrar = errors.New("host=segredo") }, status: 500, codigo: "erro_interno"},
	}

	mensagens := map[string]string{}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			if tt.setup != nil {
				tt.setup(f)
			}
			rec := f.do(tt.method, tt.path, tt.body, true)
			msg := checarErro(t, rec, tt.status, tt.codigo, true)
			semTextoInterno(t, "resposta", rec.Body.String())
			if tt.semRepo && f.repo.nChamadas() != 0 {
				t.Errorf("repositório chamado %v, want nenhuma chamada", f.repo.chamadas)
			}
			if ant, ok := mensagens[tt.codigo]; ok && ant != msg {
				t.Errorf("mensagem de %s = %q, antes %q: want fixa por código", tt.codigo, msg, ant)
			}
			mensagens[tt.codigo] = msg
		})
	}
}

func TestContratoSaidaLivroEUsuario(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		body   string
		chaves []string
		want   map[string]any
	}{
		{name: "livro", path: "/livros", body: `{"titulo":"  Dom Casmurro  "}`, chaves: []string{"id", "titulo"}, want: map[string]any{"id": json.Number("5"), "titulo": "Dom Casmurro"}},
		{name: "usuario", path: "/usuarios", body: `{"nome":"  Capitu "}`, chaves: []string{"id", "nome"}, want: map[string]any{"id": json.Number("9"), "nome": "Capitu"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodPost, tt.path, tt.body, true)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201 (corpo %s)", rec.Code, rec.Body.String())
			}
			checarCabecalhos(t, rec, true)
			m := objetoJSON(t, rec.Body.Bytes())
			exigirChaves(t, m, tt.chaves...)
			for k, v := range tt.want {
				if m[k] != v {
					t.Errorf("%s = %#v, want %#v", k, m[k], v)
				}
			}
		})
	}
}

// emprestimoJSON confere o DTO de empréstimo; devolucao "" significa null.
func emprestimoJSON(t *testing.T, body []byte, id, livroID, usuarioID, inicio, vencimento, devolucao, multa string) {
	t.Helper()
	m := objetoJSON(t, body)
	exigirChaves(t, m, "id", "livro_id", "usuario_id", "inicio", "vencimento", "devolucao", "multa")
	nums := map[string]string{"id": id, "livro_id": livroID, "usuario_id": usuarioID}
	for k, v := range nums {
		if m[k] != json.Number(v) {
			t.Errorf("%s = %#v, want número %s", k, m[k], v)
		}
	}
	strs := map[string]string{"inicio": inicio, "vencimento": vencimento, "multa": multa}
	for k, v := range strs {
		if m[k] != v {
			t.Errorf("%s = %#v, want string %q", k, m[k], v)
		}
	}
	if devolucao == "" {
		if v, ok := m["devolucao"]; !ok || v != nil {
			t.Errorf("devolucao = %#v, want null", v)
		}
	} else if m["devolucao"] != devolucao {
		t.Errorf("devolucao = %#v, want %q", m["devolucao"], devolucao)
	}
}

func TestContratoSaidaEmprestimoCriado(t *testing.T) {
	tests := []struct {
		name           string
		agora          time.Time
		path           string
		body           string
		wantLivro      string
		wantInicio     string
		wantVencimento string
	}{
		{
			name:  "dia comum",
			agora: time.Date(2026, 3, 10, 10, 0, 0, 0, sp),
			path:  "/livros/1/emprestimo", body: `{"usuario_id":2}`,
			wantLivro: "1", wantInicio: "2026-03-10T13:00:00Z", wantVencimento: "2026-03-24",
		},
		{
			name:  "virada UTC/SP: 22:30 em SP já é o dia seguinte em UTC",
			agora: time.Date(2026, 3, 9, 22, 30, 0, 123456789, sp),
			path:  "/livros/007/emprestimo", body: `{"usuario_id":2}`,
			wantLivro: "7", wantInicio: "2026-03-10T01:30:00Z", wantVencimento: "2026-03-23",
		},
		{
			name:  "relogio em outro fuso sai em UTC",
			agora: time.Date(2026, 3, 10, 22, 0, 0, 0, time.FixedZone("UTC+9", 9*3600)),
			path:  "/livros/1/emprestimo", body: `{"usuario_id":2}`,
			wantLivro: "1", wantInicio: "2026-03-10T13:00:00Z", wantVencimento: "2026-03-24",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			f.agora = tt.agora
			rec := f.do(http.MethodPost, tt.path, tt.body, true)
			if rec.Code != http.StatusCreated {
				t.Fatalf("status = %d, want 201 (corpo %s)", rec.Code, rec.Body.String())
			}
			checarCabecalhos(t, rec, true)
			emprestimoJSON(t, rec.Body.Bytes(), "77", tt.wantLivro, "2", tt.wantInicio, tt.wantVencimento, "", "0.00")
			e := f.repo.abrirRecebido
			if e.ID() != 0 || e.LivroID() != mustAtoi(tt.wantLivro) || e.UsuarioID() != 2 || !e.Inicio().Equal(tt.agora) || !e.Aberto() {
				t.Errorf("AbrirEmprestimo recebeu id=%d livro=%d usuario=%d inicio=%v aberto=%v; want NovoEmprestimo(%s, 2, agora)",
					e.ID(), e.LivroID(), e.UsuarioID(), e.Inicio(), e.Aberto(), tt.wantLivro)
			}
		})
	}
}

func mustAtoi(s string) int64 {
	var n int64
	if _, err := fmt.Sscan(s, &n); err != nil {
		panic(err)
	}
	return n
}

func TestContratoSaidaConsulta(t *testing.T) {
	tests := []struct {
		name      string
		agora     time.Time
		wantMulta string
	}{
		{name: "antes do vencimento", agora: time.Date(2026, 3, 20, 9, 0, 0, 0, sp), wantMulta: "0.00"},
		{name: "no dia do vencimento", agora: time.Date(2026, 3, 24, 23, 59, 0, 0, sp), wantMulta: "0.00"},
		{name: "um dia de atraso", agora: time.Date(2026, 3, 25, 8, 0, 0, 0, sp), wantMulta: "1.00"},
		{name: "um dia de atraso pela virada UTC", agora: time.Date(2026, 3, 25, 3, 30, 0, 0, time.UTC), wantMulta: "1.00"},
		{name: "ainda no vencimento em SP embora dia seguinte em UTC", agora: time.Date(2026, 3, 25, 2, 30, 0, 0, time.UTC), wantMulta: "0.00"},
		{name: "doze dias", agora: time.Date(2026, 4, 5, 8, 0, 0, 0, sp), wantMulta: "12.00"},
		{name: "teto", agora: time.Date(2026, 4, 13, 9, 0, 0, 0, sp), wantMulta: "20.00"},
		{name: "acima do teto", agora: time.Date(2027, 1, 1, 9, 0, 0, 0, sp), wantMulta: "20.00"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			f.agora = tt.agora
			rec := f.do(http.MethodGet, "/livros/1/emprestimo", "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			checarCabecalhos(t, rec, true)
			emprestimoJSON(t, rec.Body.Bytes(), "77", "1", "2", "2026-03-10T13:00:00Z", "2026-03-24", "", tt.wantMulta)
			if f.repo.buscarRecebido != 1 {
				t.Errorf("BuscarEmprestimoAbertoPorLivro(%d), want 1", f.repo.buscarRecebido)
			}
		})
	}
}

func TestContratoSaidaDevolucao(t *testing.T) {
	t.Run("usa o devolvido pelo Devolver(agora)", func(t *testing.T) {
		f := novaFixture(t)
		f.agora = time.Date(2026, 4, 1, 9, 0, 0, 0, sp)
		rec := f.do(http.MethodPost, "/livros/1/devolucao", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
		}
		checarCabecalhos(t, rec, true)
		emprestimoJSON(t, rec.Body.Bytes(), "77", "1", "2", "2026-03-10T13:00:00Z", "2026-03-24", "2026-04-01T12:00:00Z", "8.00")
		e := f.repo.registrarRecebido
		if e.ID() != 77 || e.Aberto() || !e.Devolucao().Equal(f.agora) {
			t.Errorf("RegistrarDevolucao recebeu id=%d devolucao=%v; want 77 devolvido em %v", e.ID(), e.Devolucao(), f.agora)
		}
		if got := strings.Join(f.repo.chamadas, ","); got != "BuscarEmprestimoAbertoPorLivro,RegistrarDevolucao" {
			t.Errorf("chamadas = %s", got)
		}
	})
	t.Run("responde com o valor devolvido pelo repositorio", func(t *testing.T) {
		f := novaFixture(t)
		f.agora = time.Date(2026, 4, 20, 9, 0, 0, 0, sp)
		ret := emprestimo(t, 77, 1, 2, inicioPadrao(), time.Date(2026, 3, 25, 8, 0, 0, 0, sp))
		f.repo.registrarRet = &ret
		rec := f.do(http.MethodPost, "/livros/1/devolucao", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
		}
		emprestimoJSON(t, rec.Body.Bytes(), "77", "1", "2", "2026-03-10T13:00:00Z", "2026-03-24", "2026-03-25T11:00:00Z", "1.00")
	})
	t.Run("devolucao no mesmo dia, sem multa", func(t *testing.T) {
		f := novaFixture(t)
		f.agora = inicioPadrao().Add(30 * time.Minute)
		rec := f.do(http.MethodPost, "/livros/1/devolucao", "", true)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
		}
		emprestimoJSON(t, rec.Body.Bytes(), "77", "1", "2", "2026-03-10T13:00:00Z", "2026-03-24", "2026-03-10T13:30:00Z", "0.00")
	})
}

func TestAuthRotasProtegidas(t *testing.T) {
	mesmoTamanho := strings.Repeat("x", len(tokenTeste))
	invalidos := []struct {
		name string
		auth string // "" = sem header
	}{
		{name: "sem header"},
		{name: "basic", auth: "Basic dXNlcjpwYXNz"},
		{name: "token errado", auth: "Bearer token-errado"},
		{name: "token de tamanho diferente", auth: "Bearer " + tokenTeste + tokenTeste},
		{name: "token errado de mesmo tamanho", auth: "Bearer " + mesmoTamanho},
		{name: "prefixo do token", auth: "Bearer " + tokenTeste[:20]},
		{name: "token sem esquema", auth: tokenTeste},
		{name: "esquema vazio", auth: "Bearer "},
	}
	for _, r := range rotasProtegidas() {
		var corpoRef string
		for _, c := range invalidos {
			t.Run(r.nome+"/"+c.name, func(t *testing.T) {
				f := novaFixture(t)
				h := http.Header{}
				if c.auth != "" {
					h.Set("Authorization", c.auth)
				}
				rec := f.doHeader(r.method, r.path, r.body, h)
				checarErro(t, rec, http.StatusUnauthorized, "nao_autorizado", true)
				if got := rec.Header().Get("WWW-Authenticate"); got != "Bearer" {
					t.Errorf("WWW-Authenticate = %q, want Bearer", got)
				}
				if f.repo.nChamadas() != 0 {
					t.Errorf("repositório chamado sem auth: %v", f.repo.chamadas)
				}
				if corpoRef == "" {
					corpoRef = rec.Body.String()
				} else if rec.Body.String() != corpoRef {
					t.Errorf("401 varia: %s != %s", rec.Body.String(), corpoRef)
				}
			})
		}
		t.Run(r.nome+"/bearer minusculo aceito", func(t *testing.T) {
			f := novaFixture(t)
			h := http.Header{}
			h.Set("Authorization", "bearer "+tokenTeste)
			rec := f.doHeader(r.method, r.path, r.body, h)
			if rec.Code >= 400 {
				t.Errorf("status = %d, want sucesso (corpo %s)", rec.Code, rec.Body.String())
			}
		})
	}
}

// A auth vem antes do parse do {id}, do corpo e do limite de corpo.
func TestAuthAntesDeTudo(t *testing.T) {
	grande := `{"titulo":"` + strings.Repeat("a", 70<<10) + `"}`
	tests := []struct {
		name   string
		method string
		path   string
		body   string
	}{
		{name: "id invalido", method: http.MethodGet, path: "/livros/abc/emprestimo"},
		{name: "id zero na devolucao", method: http.MethodPost, path: "/livros/0/devolucao"},
		{name: "corpo invalido", method: http.MethodPost, path: "/livros", body: `{"x":`},
		{name: "corpo grande", method: http.MethodPost, path: "/livros", body: grande},
		{name: "corpo na devolucao", method: http.MethodPost, path: "/livros/1/devolucao", body: "x"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(tt.method, tt.path, tt.body, false)
			checarErro(t, rec, http.StatusUnauthorized, "nao_autorizado", true)
		})
	}
}

func TestHealthzEReadyz(t *testing.T) {
	t.Run("healthz sem auth", func(t *testing.T) {
		f := novaFixture(t)
		rec := f.do(http.MethodGet, "/healthz", "", false)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		checarCabecalhos(t, rec, false)
		if m := objetoJSON(t, rec.Body.Bytes()); chavesOrdenadas(m) != "status" || m["status"] != "ok" {
			t.Errorf("corpo = %s, want {\"status\":\"ok\"}", rec.Body.String())
		}
		if f.pinger.chamado != 0 {
			t.Error("healthz chamou o Ping")
		}
	})
	t.Run("readyz ok sem auth, com ping de 2s", func(t *testing.T) {
		f := novaFixture(t)
		rec := f.do(http.MethodGet, "/readyz", "", false)
		if rec.Code != http.StatusOK {
			t.Fatalf("status = %d, want 200", rec.Code)
		}
		checarCabecalhos(t, rec, false)
		if m := objetoJSON(t, rec.Body.Bytes()); chavesOrdenadas(m) != "status" || m["status"] != "ok" {
			t.Errorf("corpo = %s, want {\"status\":\"ok\"}", rec.Body.String())
		}
		if f.pinger.chamado != 1 {
			t.Fatalf("Ping chamado %d vezes, want 1", f.pinger.chamado)
		}
		if f.pinger.prazo <= time.Second || f.pinger.prazo > 2*time.Second {
			t.Errorf("prazo do Ping = %v, want (1s, 2s]", f.pinger.prazo)
		}
	})
	t.Run("readyz 503 com banco fora", func(t *testing.T) {
		f := novaFixture(t)
		f.pinger.err = errors.New("conectar postgres://u:SENHA-DSN@host=segredo/db")
		rec := f.do(http.MethodGet, "/readyz", "", false)
		checarErro(t, rec, http.StatusServiceUnavailable, "indisponivel", false)
		semTextoInterno(t, "resposta", rec.Body.String())
		if strings.Contains(rec.Body.String(), "SENHA-DSN") {
			t.Errorf("resposta vaza o DSN: %s", rec.Body.String())
		}
	})
	t.Run("readyz 503 com timeout", func(t *testing.T) {
		f := novaFixture(t)
		f.pinger.err = fmt.Errorf("ping: %w", context.DeadlineExceeded)
		rec := f.do(http.MethodGet, "/readyz", "", false)
		checarErro(t, rec, http.StatusServiceUnavailable, "indisponivel", false)
	})
	t.Run("readyz e healthz ignoram Authorization invalido", func(t *testing.T) {
		f := novaFixture(t)
		h := http.Header{}
		h.Set("Authorization", "Bearer errado")
		for _, p := range []string{"/healthz", "/readyz"} {
			if rec := f.doHeader(http.MethodGet, p, "", h); rec.Code != http.StatusOK {
				t.Errorf("%s com token errado: status = %d, want 200", p, rec.Code)
			}
		}
	})
}

func TestLimiteDeCorpo(t *testing.T) {
	const limite = 64 << 10
	// preencher completa o JSON com espaços até n bytes.
	preencher := func(json string, n int) string { return json + strings.Repeat(" ", n-len(json)) }
	titulo := func(n int) string {
		pre, suf := `{"titulo":"`, `"}`
		return pre + strings.Repeat("a", n-len(pre)-len(suf)) + suf
	}

	tests := []struct {
		name   string
		path   string
		body   string
		status int
		codigo string
	}{
		{name: "livro com 64 KiB exatos de espaço", path: "/livros", body: preencher(`{"titulo":"a"}`, limite), status: 201},
		{name: "livro com 64 KiB exatos de titulo", path: "/livros", body: titulo(limite), status: 201},
		{name: "livro com 64 KiB + 1 de espaço", path: "/livros", body: preencher(`{"titulo":"a"}`, limite+1), status: 413, codigo: "corpo_muito_grande"},
		{name: "livro com 64 KiB + 1 de titulo", path: "/livros", body: titulo(limite + 1), status: 413, codigo: "corpo_muito_grande"},
		{name: "livro com 1 MiB sem fechar", path: "/livros", body: `{"titulo":"` + strings.Repeat("a", 1<<20), status: 413, codigo: "corpo_muito_grande"},
		{name: "usuario com 64 KiB + 1", path: "/usuarios", body: preencher(`{"nome":"a"}`, limite+1), status: 413, codigo: "corpo_muito_grande"},
		{name: "emprestimo com 64 KiB exatos", path: "/livros/1/emprestimo", body: preencher(`{"usuario_id":2}`, limite), status: 201},
		{name: "emprestimo com 64 KiB + 1", path: "/livros/1/emprestimo", body: preencher(`{"usuario_id":2}`, limite+1), status: 413, codigo: "corpo_muito_grande"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodPost, tt.path, tt.body, true)
			if tt.codigo == "" {
				if rec.Code != tt.status {
					t.Fatalf("status = %d, want %d (corpo %.200s)", rec.Code, tt.status, rec.Body.String())
				}
				return
			}
			checarErro(t, rec, tt.status, tt.codigo, true)
			if f.repo.nChamadas() != 0 {
				t.Errorf("repositório chamado com corpo grande: %v", f.repo.chamadas)
			}
		})
	}
}

func TestDecodificacaoDoCorpo(t *testing.T) {
	tests := []struct {
		name   string
		path   string
		body   string
		status int
		codigo string // vazio em 201
	}{
		// livros
		{name: "livro ok", path: "/livros", body: `{"titulo":"a"}`, status: 201},
		{name: "livro com espaço e newline depois", path: "/livros", body: "{\"titulo\":\"a\"} \n\t", status: 201},
		{name: "corpo vazio", path: "/livros", body: "", status: 400, codigo: "corpo_invalido"},
		{name: "só espaços", path: "/livros", body: "   ", status: 400, codigo: "corpo_invalido"},
		{name: "null no topo", path: "/livros", body: "null", status: 400, codigo: "corpo_invalido"},
		{name: "array", path: "/livros", body: `[{"titulo":"a"}]`, status: 400, codigo: "corpo_invalido"},
		{name: "string no topo", path: "/livros", body: `"a"`, status: 400, codigo: "corpo_invalido"},
		{name: "titulo numero", path: "/livros", body: `{"titulo":1}`, status: 400, codigo: "corpo_invalido"},
		{name: "titulo array", path: "/livros", body: `{"titulo":["a"]}`, status: 400, codigo: "corpo_invalido"},
		{name: "titulo objeto", path: "/livros", body: `{"titulo":{}}`, status: 400, codigo: "corpo_invalido"},
		{name: "chave sem aspas", path: "/livros", body: `{titulo:"a"}`, status: 400, codigo: "corpo_invalido"},
		{name: "campo desconhecido", path: "/livros", body: `{"titulo":"a","id":3}`, status: 400, codigo: "corpo_invalido"},
		{name: "objeto vazio extra", path: "/livros", body: `{"titulo":"a"}{}`, status: 400, codigo: "corpo_invalido"},
		{name: "lixo depois", path: "/livros", body: `{"titulo":"a"} x`, status: 400, codigo: "corpo_invalido"},
		{name: "número depois", path: "/livros", body: `{"titulo":"a"} 1`, status: 400, codigo: "corpo_invalido"},
		{name: "objeto truncado depois", path: "/livros", body: `{"titulo":"a"}{`, status: 400, codigo: "corpo_invalido"},
		{name: "titulo ausente", path: "/livros", body: `{}`, status: 422, codigo: "titulo_vazio"},
		{name: "titulo null", path: "/livros", body: `{"titulo":null}`, status: 422, codigo: "titulo_vazio"},
		{name: "titulo com NUL no fim", path: "/livros", body: `{"titulo":"abc\u0000"}`, status: 422, codigo: "texto_invalido"},

		// usuarios
		{name: "usuario ok", path: "/usuarios", body: `{"nome":"Capitu"}`, status: 201},
		{name: "usuario campo do livro", path: "/usuarios", body: `{"titulo":"a"}`, status: 400, codigo: "corpo_invalido"},
		{name: "usuario nome booleano", path: "/usuarios", body: `{"nome":true}`, status: 400, codigo: "corpo_invalido"},
		{name: "usuario nome ausente", path: "/usuarios", body: `{}`, status: 422, codigo: "nome_vazio"},
		{name: "usuario nome null", path: "/usuarios", body: `{"nome":null}`, status: 422, codigo: "nome_vazio"},
		{name: "usuario nome só espaço", path: "/usuarios", body: `{"nome":" \t "}`, status: 422, codigo: "nome_vazio"},

		// emprestimo
		{name: "emprestimo ok", path: "/livros/1/emprestimo", body: `{"usuario_id":2}`, status: 201},
		{name: "usuario_id maximo", path: "/livros/1/emprestimo", body: `{"usuario_id":9223372036854775807}`, status: 201},
		{name: "usuario_id fracionário", path: "/livros/1/emprestimo", body: `{"usuario_id":1.5}`, status: 400, codigo: "corpo_invalido"},
		{name: "usuario_id 2.0", path: "/livros/1/emprestimo", body: `{"usuario_id":2.0}`, status: 400, codigo: "corpo_invalido"},
		{name: "usuario_id com expoente", path: "/livros/1/emprestimo", body: `{"usuario_id":1e2}`, status: 400, codigo: "corpo_invalido"},
		{name: "usuario_id com expoente E", path: "/livros/1/emprestimo", body: `{"usuario_id":2E0}`, status: 400, codigo: "corpo_invalido"},
		{name: "usuario_id overflow", path: "/livros/1/emprestimo", body: `{"usuario_id":9223372036854775808}`, status: 400, codigo: "corpo_invalido"},
		{name: "usuario_id string", path: "/livros/1/emprestimo", body: `{"usuario_id":"2"}`, status: 400, codigo: "corpo_invalido"},
		{name: "usuario_id booleano", path: "/livros/1/emprestimo", body: `{"usuario_id":true}`, status: 400, codigo: "corpo_invalido"},
		{name: "livro_id no corpo", path: "/livros/1/emprestimo", body: `{"usuario_id":2,"livro_id":3}`, status: 400, codigo: "corpo_invalido"},
		{name: "usuario_id zero", path: "/livros/1/emprestimo", body: `{"usuario_id":0}`, status: 422, codigo: "usuario_id_invalido"},
		{name: "usuario_id negativo", path: "/livros/1/emprestimo", body: `{"usuario_id":-5}`, status: 422, codigo: "usuario_id_invalido"},
		{name: "usuario_id null", path: "/livros/1/emprestimo", body: `{"usuario_id":null}`, status: 422, codigo: "usuario_id_invalido"},
		{name: "usuario_id ausente", path: "/livros/1/emprestimo", body: `{}`, status: 422, codigo: "usuario_id_invalido"},
		{name: "emprestimo corpo vazio", path: "/livros/1/emprestimo", body: "", status: 400, codigo: "corpo_invalido"},

		// devolucao: qualquer byte é corpo inválido
		{name: "devolucao sem corpo", path: "/livros/1/devolucao", body: "", status: 200},
		{name: "devolucao com espaço", path: "/livros/1/devolucao", body: " ", status: 400, codigo: "corpo_invalido"},
		{name: "devolucao com newline", path: "/livros/1/devolucao", body: "\n", status: 400, codigo: "corpo_invalido"},
		{name: "devolucao com objeto vazio", path: "/livros/1/devolucao", body: "{}", status: 400, codigo: "corpo_invalido"},
		{name: "devolucao com null", path: "/livros/1/devolucao", body: "null", status: 400, codigo: "corpo_invalido"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodPost, tt.path, tt.body, true)
			if tt.codigo == "" {
				if rec.Code != tt.status {
					t.Fatalf("status = %d, want %d (corpo %s)", rec.Code, tt.status, rec.Body.String())
				}
				return
			}
			checarErro(t, rec, tt.status, tt.codigo, true)
			if tt.status == 400 && f.repo.nChamadas() != 0 {
				t.Errorf("repositório chamado com corpo inválido: %v", f.repo.chamadas)
			}
		})
	}
}

func TestParametroID(t *testing.T) {
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
	type alvo struct {
		method, sufixo, body string
	}
	alvos := []alvo{
		{method: http.MethodPost, sufixo: "/emprestimo", body: `{"usuario_id":2}`},
		{method: http.MethodPost, sufixo: "/devolucao"},
		{method: http.MethodGet, sufixo: "/emprestimo"},
	}
	for _, a := range alvos {
		for _, iv := range invalidos {
			t.Run(a.method+a.sufixo+"/"+iv.name, func(t *testing.T) {
				f := novaFixture(t)
				rec := f.do(a.method, "/livros/"+iv.seg+a.sufixo, a.body, true)
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
		{seg: "007", want: 7},
		{seg: "1", want: 1},
		{seg: "9223372036854775807", want: 9223372036854775807},
	}
	for _, v := range validos {
		t.Run("GET/"+v.seg, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodGet, "/livros/"+v.seg+"/emprestimo", "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			if f.repo.buscarRecebido != v.want {
				t.Errorf("livroID = %d, want %d", f.repo.buscarRecebido, v.want)
			}
		})
		t.Run("POST devolucao/"+v.seg, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(http.MethodPost, "/livros/"+v.seg+"/devolucao", "", true)
			if rec.Code != http.StatusOK {
				t.Fatalf("status = %d, want 200 (corpo %s)", rec.Code, rec.Body.String())
			}
			if f.repo.buscarRecebido != v.want {
				t.Errorf("livroID = %d, want %d", f.repo.buscarRecebido, v.want)
			}
		})
	}
	t.Run("POST emprestimo/007", func(t *testing.T) {
		f := novaFixture(t)
		rec := f.do(http.MethodPost, "/livros/007/emprestimo", `{"usuario_id":2}`, true)
		if rec.Code != http.StatusCreated {
			t.Fatalf("status = %d, want 201 (corpo %s)", rec.Code, rec.Body.String())
		}
		if f.repo.abrirRecebido.LivroID() != 7 {
			t.Errorf("livroID = %d, want 7", f.repo.abrirRecebido.LivroID())
		}
	})
}

func TestMuxNotFoundEMethodNotAllowed(t *testing.T) {
	tests := []struct {
		name   string
		method string
		path   string
		auth   bool
		status int
	}{
		{name: "raiz", method: http.MethodGet, path: "/", status: 404},
		{name: "rota inexistente", method: http.MethodGet, path: "/inexistente", status: 404},
		{name: "livro sem subrota", method: http.MethodGet, path: "/livros/1", auth: true, status: 404},
		{name: "subrota desconhecida", method: http.MethodPost, path: "/livros/1/renovacao", auth: true, status: 404},
		{name: "GET em /livros", method: http.MethodGet, path: "/livros", auth: true, status: 405},
		{name: "DELETE no emprestimo", method: http.MethodDelete, path: "/livros/1/emprestimo", auth: true, status: 405},
		{name: "GET na devolucao", method: http.MethodGet, path: "/livros/1/devolucao", auth: true, status: 405},
		{name: "POST no healthz", method: http.MethodPost, path: "/healthz", status: 405},
		{name: "POST no readyz", method: http.MethodPost, path: "/readyz", status: 405},
		{name: "405 sem auth continua 405", method: http.MethodPut, path: "/usuarios", status: 405},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(tt.method, tt.path, "", tt.auth)
			if rec.Code != tt.status {
				t.Fatalf("status = %d, want %d", rec.Code, tt.status)
			}
			if got := rec.Header().Get("X-Request-ID"); !hex32.MatchString(got) {
				t.Errorf("X-Request-ID = %q, want 32 hex", got)
			}
			if f.repo.nChamadas() != 0 {
				t.Errorf("repositório chamado: %v", f.repo.chamadas)
			}
		})
	}
}

func TestRequestIDIgnoraEntrada(t *testing.T) {
	f := novaFixture(t)
	vistos := map[string]bool{}
	for i := 0; i < 5; i++ {
		h := authHeader(true)
		h.Set("X-Request-ID", "id-do-atacante")
		rec := f.doHeader(http.MethodGet, "/livros/1/emprestimo", "", h)
		id := rec.Header().Get("X-Request-ID")
		if !hex32.MatchString(id) {
			t.Fatalf("X-Request-ID = %q, want 32 hex gerado", id)
		}
		if vistos[id] {
			t.Errorf("X-Request-ID %q repetido", id)
		}
		vistos[id] = true
	}
}

func TestTimeoutPorOperacao(t *testing.T) {
	for _, r := range rotasProtegidas() {
		t.Run(r.nome+"/prazo de 5s", func(t *testing.T) {
			f := novaFixture(t)
			rec := f.do(r.method, r.path, r.body, true)
			if rec.Code >= 400 {
				t.Fatalf("status = %d (corpo %s)", rec.Code, rec.Body.String())
			}
			if len(f.repo.prazos) == 0 {
				t.Fatal("repositório não chamado")
			}
			for i, p := range f.repo.prazos {
				if p <= 4*time.Second || p > 5*time.Second {
					t.Errorf("chamada %s: prazo do ctx = %v, want (4s, 5s]", f.repo.chamadas[i], p)
				}
			}
		})
		t.Run(r.nome+"/estouro vira 503", func(t *testing.T) {
			f := novaFixture(t)
			f.timeout = 30 * time.Millisecond
			f.repo.bloquear = true
			inicio := time.Now()
			rec := f.do(r.method, r.path, r.body, true)
			checarErro(t, rec, http.StatusServiceUnavailable, "indisponivel", true)
			if d := time.Since(inicio); d > 2*time.Second {
				t.Errorf("levou %v, want perto de 30ms", d)
			}
		})
	}
}
