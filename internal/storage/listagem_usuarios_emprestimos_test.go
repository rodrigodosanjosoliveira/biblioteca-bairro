package storage

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
)

// cadastro insere n usuários além do usuário do fixture e devolve todos, em
// ordem de id, com um buraco na sequência e o primeiro regravado (versão nova
// no fim do heap: leitura sem ORDER BY sai fora de ordem).
func cadastro(t *testing.T, f fixture, n int) []domain.Usuario {
	t.Helper()
	todos := []domain.Usuario{f.usuario}
	for i := 0; i < n; i++ {
		todos = append(todos, f.novoUsuario(t, fmt.Sprintf("Usuario %02d", i)))
	}
	buraco := f.novoUsuario(t, "apagado")
	if _, err := f.conn.Exec(t.Context(), "DELETE FROM usuario WHERE id = $1", buraco.ID); err != nil {
		t.Fatalf("apagar usuário: %v", err)
	}
	todos = append(todos, f.novoUsuario(t, "depois do buraco"))
	if _, err := f.conn.Exec(t.Context(), "UPDATE usuario SET nome = nome WHERE id = $1", todos[0].ID); err != nil {
		t.Fatalf("regravar usuário: %v", err)
	}
	return todos
}

func idsUsuarios(us []domain.Usuario) []int64 {
	out := make([]int64, 0, len(us))
	for _, u := range us {
		out = append(out, u.ID)
	}
	return out
}

func idsEmprestimos(es []domain.Emprestimo) []int64 {
	out := make([]int64, 0, len(es))
	for _, e := range es {
		out = append(out, e.ID())
	}
	return out
}

// TestListarUsuarios fixa a página: no máximo limite itens, em ordem de id,
// só ids > apos, temMais só se houver item depois do último entregue, slice
// vazia (não nil) sem linhas e nome lido do banco.
func TestListarUsuarios(t *testing.T) {
	f := novoFixture(t)
	todos := cadastro(t, f, 5) // 7 usuários
	ids := idsUsuarios(todos)
	ultimo := ids[len(ids)-1]

	tests := []struct {
		name        string
		apos        int64
		limite      int
		wantIDs     []int64
		wantTemMais bool
	}{
		{name: "primeira página", apos: 0, limite: 3, wantIDs: ids[:3], wantTemMais: true},
		{name: "página do meio", apos: ids[2], limite: 3, wantIDs: ids[3:6], wantTemMais: true},
		{name: "última página incompleta", apos: ids[5], limite: 3, wantIDs: ids[6:]},
		{name: "limite igual ao restante", apos: ids[3], limite: 3, wantIDs: ids[4:]},
		{name: "limite igual ao total", apos: 0, limite: len(ids), wantIDs: ids},
		{name: "limite um a menos que o total", apos: 0, limite: len(ids) - 1, wantIDs: ids[:len(ids)-1], wantTemMais: true},
		{name: "teto", apos: 0, limite: LimiteListagem, wantIDs: ids},
		{name: "limite 1", apos: 0, limite: 1, wantIDs: ids[:1], wantTemMais: true},
		{name: "apos no buraco", apos: ids[len(ids)-2] + 1, limite: 3, wantIDs: ids[len(ids)-1:]},
		{name: "apos exclusivo", apos: ids[0], limite: 1, wantIDs: ids[1:2], wantTemMais: true},
		{name: "apos igual ao último", apos: ultimo, limite: 3},
		{name: "apos máximo int64", apos: 1<<63 - 1, limite: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, temMais, err := f.repo.ListarUsuarios(t.Context(), tt.apos, tt.limite)
			if err != nil {
				t.Fatalf("ListarUsuarios(%d, %d): %v", tt.apos, tt.limite, err)
			}
			if got == nil {
				t.Error("ListarUsuarios devolveu nil, want slice")
			}
			if fmt.Sprint(idsUsuarios(got)) != fmt.Sprint(idsOuVazio(tt.wantIDs)) {
				t.Errorf("ids = %v, want %v", idsUsuarios(got), idsOuVazio(tt.wantIDs))
			}
			if temMais != tt.wantTemMais {
				t.Errorf("temMais = %v, want %v", temMais, tt.wantTemMais)
			}
			for _, u := range got {
				for _, w := range todos {
					if w.ID == u.ID && w.Nome != u.Nome {
						t.Errorf("usuário %d nome = %q, want %q", u.ID, u.Nome, w.Nome)
					}
				}
			}
		})
	}
}

// Percorrer os usuários com apos = id do último entregue visita cada um
// exatamente uma vez, em ordem, para todo limite.
func TestListarUsuariosPercorrerTudo(t *testing.T) {
	f := novoFixture(t)
	todos := cadastro(t, f, 8)
	want := fmt.Sprint(idsUsuarios(todos))
	for limite := 1; limite <= len(todos)+1; limite++ {
		t.Run(fmt.Sprintf("limite %d", limite), func(t *testing.T) {
			var visitados []int64
			var apos int64
			paginas := 0
			for {
				paginas++
				if paginas > len(todos)+2 {
					t.Fatalf("paginação não termina: %v", visitados)
				}
				got, temMais, err := f.repo.ListarUsuarios(t.Context(), apos, limite)
				if err != nil {
					t.Fatalf("ListarUsuarios(%d, %d): %v", apos, limite, err)
				}
				visitados = append(visitados, idsUsuarios(got)...)
				if !temMais {
					break
				}
				if len(got) == 0 {
					t.Fatal("temMais com página vazia")
				}
				apos = got[len(got)-1].ID
			}
			if fmt.Sprint(visitados) != want {
				t.Errorf("visitados = %v, want %v", visitados, want)
			}
			if wantPaginas := (len(todos) + limite - 1) / limite; paginas != wantPaginas {
				t.Errorf("%d páginas, want %d", paginas, wantPaginas)
			}
		})
	}
}

// historico grava empréstimos variados (abertos, devolvidos e um renovado)
// de livros novos, com um buraco nos ids e o primeiro regravado, e devolve o
// estado final de cada um em ordem de id.
func historico(t *testing.T, f fixture) []domain.Emprestimo {
	t.Helper()
	ctx := t.Context()
	sp := saoPaulo(t)
	var todos []domain.Emprestimo
	abrir := func(livroID, usuarioID int64, inicio time.Time) domain.Emprestimo {
		n, err := domain.NovoEmprestimo(livroID, usuarioID, inicio)
		if err != nil {
			t.Fatalf("NovoEmprestimo: %v", err)
		}
		e, err := f.repo.AbrirEmprestimo(ctx, n)
		if err != nil {
			t.Fatalf("AbrirEmprestimo: %v", err)
		}
		return e
	}
	outro := f.novoUsuario(t, "Bento")
	for i := 0; i < 6; i++ {
		l := f.novoLivro(t, fmt.Sprintf("Livro %02d", i))
		u := f.usuario.ID
		if i%3 == 1 {
			u = outro.ID
		}
		e := abrir(l.ID, u, time.Date(2026, 3, 10+i, 10, 0, 0, 123456000, sp))
		switch i % 3 {
		case 0:
			if err := e.Devolver(time.Date(2026, 3, 26, 9, 30, 0, 0, sp)); err != nil {
				t.Fatalf("Devolver: %v", err)
			}
			d, err := f.repo.RegistrarDevolucao(ctx, e)
			if err != nil {
				t.Fatalf("RegistrarDevolucao: %v", err)
			}
			e = d
		case 2:
			if err := e.Renovar(time.Date(2026, 3, 20, 10, 0, 0, 0, sp)); err != nil {
				t.Fatalf("Renovar: %v", err)
			}
			r, err := f.repo.RegistrarRenovacao(ctx, e)
			if err != nil {
				t.Fatalf("RegistrarRenovacao: %v", err)
			}
			e = r
		}
		todos = append(todos, e)
		if i == 3 {
			buraco := abrir(f.livro.ID, f.usuario.ID, time.Date(2026, 3, 15, 10, 0, 0, 0, sp))
			if _, err := f.conn.Exec(ctx, "DELETE FROM emprestimo WHERE id = $1", buraco.ID()); err != nil {
				t.Fatalf("apagar empréstimo: %v", err)
			}
		}
	}
	if _, err := f.conn.Exec(ctx, "UPDATE emprestimo SET renovacoes = renovacoes WHERE id = $1", todos[0].ID()); err != nil {
		t.Fatalf("regravar empréstimo: %v", err)
	}
	for i := 1; i < len(todos); i++ {
		if todos[i].ID() <= todos[i-1].ID() {
			t.Fatalf("ids fora de ordem: %d depois de %d", todos[i].ID(), todos[i-1].ID())
		}
	}
	return todos
}

// TestListarEmprestimos fixa a página de empréstimos (abertos, devolvidos e
// renovados, de qualquer livro e usuário), cada um reidratado como em
// BuscarEmprestimo.
func TestListarEmprestimos(t *testing.T) {
	f := novoFixture(t)
	todos := historico(t, f) // 6 empréstimos
	ids := idsEmprestimos(todos)

	tests := []struct {
		name        string
		apos        int64
		limite      int
		want        []domain.Emprestimo
		wantTemMais bool
	}{
		{name: "primeira página", apos: 0, limite: 2, want: todos[:2], wantTemMais: true},
		{name: "página do meio", apos: ids[1], limite: 2, want: todos[2:4], wantTemMais: true},
		{name: "página depois do buraco", apos: ids[3], limite: 2, want: todos[4:]},
		{name: "limite igual ao total", apos: 0, limite: len(ids), want: todos},
		{name: "limite um a menos que o total", apos: 0, limite: len(ids) - 1, want: todos[:len(ids)-1], wantTemMais: true},
		{name: "teto", apos: 0, limite: LimiteListagem, want: todos},
		{name: "limite 1", apos: 0, limite: 1, want: todos[:1], wantTemMais: true},
		{name: "apos no buraco", apos: ids[3] + 1, limite: 1, want: todos[4:5], wantTemMais: true},
		{name: "apos igual ao último", apos: ids[len(ids)-1], limite: 3},
		{name: "apos máximo int64", apos: 1<<63 - 1, limite: 3},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, temMais, err := f.repo.ListarEmprestimos(t.Context(), tt.apos, tt.limite)
			if err != nil {
				t.Fatalf("ListarEmprestimos(%d, %d): %v", tt.apos, tt.limite, err)
			}
			if got == nil {
				t.Error("ListarEmprestimos devolveu nil, want slice")
			}
			if fmt.Sprint(idsEmprestimos(got)) != fmt.Sprint(idsOuVazio(idsEmprestimos(tt.want))) {
				t.Fatalf("ids = %v, want %v", idsEmprestimos(got), idsEmprestimos(tt.want))
			}
			if temMais != tt.wantTemMais {
				t.Errorf("temMais = %v, want %v", temMais, tt.wantTemMais)
			}
			for i := range got {
				checarEmprestimo(t, got[i], tt.want[i])
				b, err := f.repo.BuscarEmprestimo(t.Context(), got[i].ID())
				if err != nil {
					t.Fatalf("BuscarEmprestimo: %v", err)
				}
				checarEmprestimo(t, got[i], b)
			}
		})
	}
}

// Percorrer os empréstimos visita cada um exatamente uma vez, em ordem.
func TestListarEmprestimosPercorrerTudo(t *testing.T) {
	f := novoFixture(t)
	todos := historico(t, f)
	want := fmt.Sprint(idsEmprestimos(todos))
	for limite := 1; limite <= len(todos)+1; limite++ {
		t.Run(fmt.Sprintf("limite %d", limite), func(t *testing.T) {
			var visitados []int64
			var apos int64
			for paginas := 1; ; paginas++ {
				if paginas > len(todos)+2 {
					t.Fatalf("paginação não termina: %v", visitados)
				}
				got, temMais, err := f.repo.ListarEmprestimos(t.Context(), apos, limite)
				if err != nil {
					t.Fatalf("ListarEmprestimos(%d, %d): %v", apos, limite, err)
				}
				visitados = append(visitados, idsEmprestimos(got)...)
				if !temMais {
					break
				}
				if len(got) == 0 {
					t.Fatal("temMais com página vazia")
				}
				apos = got[len(got)-1].ID()
			}
			if fmt.Sprint(visitados) != want {
				t.Errorf("visitados = %v, want %v", visitados, want)
			}
		})
	}
}

// Uma linha de empréstimo gravada inválida (vencimento antes do início, sem
// CHECK no banco) derruba a página que a entrega com o erro de reidratação;
// como linha extra (+1) não é lida; depois dela, as páginas saem normais.
func TestListarEmprestimosLinhaInvalida(t *testing.T) {
	f := novoFixture(t)
	todos := historico(t, f)
	ids := idsEmprestimos(todos)
	if _, err := f.conn.Exec(t.Context(), "UPDATE emprestimo SET vencimento = DATE '2000-01-01' WHERE id = $1", ids[2]); err != nil {
		t.Fatalf("corromper empréstimo: %v", err)
	}
	tests := []struct {
		name        string
		apos        int64
		limite      int
		wantErr     bool
		wantIDs     []int64
		wantTemMais bool
	}{
		{name: "página que a contém", apos: 0, limite: 3, wantErr: true},
		{name: "página em que é a primeira", apos: ids[1], limite: 2, wantErr: true},
		{name: "página em que é a linha extra", apos: 0, limite: 2, wantIDs: ids[:2], wantTemMais: true},
		{name: "página depois dela", apos: ids[2], limite: LimiteListagem, wantIDs: ids[3:]},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, temMais, err := f.repo.ListarEmprestimos(t.Context(), tt.apos, tt.limite)
			if tt.wantErr {
				if !errors.Is(err, domain.ErrVencimentoNaoPosteriorAoInicio) {
					t.Errorf("erro = %v, want %v", err, domain.ErrVencimentoNaoPosteriorAoInicio)
				}
				if len(got) != 0 || temMais {
					t.Errorf("com erro devolveu %d itens e temMais=%v", len(got), temMais)
				}
				return
			}
			if err != nil {
				t.Fatalf("ListarEmprestimos: %v", err)
			}
			if fmt.Sprint(idsEmprestimos(got)) != fmt.Sprint(tt.wantIDs) || temMais != tt.wantTemMais {
				t.Errorf("ids = %v temMais = %v, want %v e %v", idsEmprestimos(got), temMais, tt.wantIDs, tt.wantTemMais)
			}
		})
	}
}

// Tabelas vazias: página vazia (não nil) e temMais falso.
func TestListagensNovasVazio(t *testing.T) {
	_, c := novoBancoMigrado(t)
	r := NovoRepositorio(c)
	us, maisU, err := r.ListarUsuarios(t.Context(), 0, 20)
	if err != nil {
		t.Fatalf("ListarUsuarios: %v", err)
	}
	if us == nil || len(us) != 0 || maisU {
		t.Errorf("ListarUsuarios = %#v, %v; want [] e false", us, maisU)
	}
	es, maisE, err := r.ListarEmprestimos(t.Context(), 0, 20)
	if err != nil {
		t.Fatalf("ListarEmprestimos: %v", err)
	}
	if es == nil || len(es) != 0 || maisE {
		t.Errorf("ListarEmprestimos = %#v, %v; want [] e false", es, maisE)
	}
}

// Parâmetros inválidos são recusados antes do SQL também com banco real.
func TestListagensNovasRecusasComBanco(t *testing.T) {
	f := novoFixture(t)
	for _, l := range listagensNovas() {
		for _, c := range []struct {
			apos   int64
			limite int
		}{{0, 0}, {0, LimiteListagem + 1}, {-1, 10}} {
			t.Run(fmt.Sprintf("%s(%d,%d)", l.nome, c.apos, c.limite), func(t *testing.T) {
				ids, mais, err := l.listar(f.repo, t.Context(), c.apos, c.limite)
				if !errors.Is(err, ErrListagemInvalida) {
					t.Errorf("erro = %v, want %v", err, ErrListagemInvalida)
				}
				if len(ids) != 0 || mais {
					t.Errorf("com erro devolveu %v, %v", ids, mais)
				}
			})
		}
	}
}
