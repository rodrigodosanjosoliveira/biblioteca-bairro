package storage

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
)

// acervo insere n livros além do livro do fixture e devolve todos, em ordem
// de id. Antes, apaga do meio um livro extra para deixar um buraco na
// sequência de ids (o cursor não pode depender de ids contíguos).
func acervo(t *testing.T, f fixture, n int) []domain.Livro {
	t.Helper()
	todos := []domain.Livro{f.livro}
	for i := 0; i < n; i++ {
		todos = append(todos, f.novoLivro(t, fmt.Sprintf("Livro %02d", i)))
	}
	buraco := f.novoLivro(t, "apagado")
	if _, err := f.conn.Exec(t.Context(), "DELETE FROM livro WHERE id = $1", buraco.ID); err != nil {
		t.Fatalf("apagar livro: %v", err)
	}
	todos = append(todos, f.novoLivro(t, "depois do buraco"))
	// Regrava o primeiro livro (mesmo valor): a versão nova vai para o fim do
	// heap, e uma leitura sem ORDER BY deixa de sair em ordem de id.
	if _, err := f.conn.Exec(t.Context(), "UPDATE livro SET titulo = titulo WHERE id = $1", todos[0].ID); err != nil {
		t.Fatalf("regravar livro: %v", err)
	}
	for i := 1; i < len(todos); i++ {
		if todos[i].ID <= todos[i-1].ID {
			t.Fatalf("ids fora de ordem na inserção: %d depois de %d", todos[i].ID, todos[i-1].ID)
		}
	}
	return todos
}

func idsDe(ls []domain.Livro) []int64 {
	out := make([]int64, 0, len(ls))
	for _, l := range ls {
		out = append(out, l.ID)
	}
	return out
}

// TestListarLivros fixa a página: no máximo limite itens, em ordem de id, só
// ids > apos, temMais verdadeiro só se houver item depois do último
// entregue, e slice vazia (não nil) sem linhas.
func TestListarLivros(t *testing.T) {
	f := novoFixture(t)
	todos := acervo(t, f, 5) // 7 livros: fixture + 5 + depois do buraco
	ids := idsDe(todos)
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
		{name: "última página incompleta", apos: ids[5], limite: 3, wantIDs: ids[6:], wantTemMais: false},
		{name: "limite igual ao restante", apos: ids[3], limite: 3, wantIDs: ids[4:], wantTemMais: false},
		{name: "limite igual ao total", apos: 0, limite: len(ids), wantIDs: ids, wantTemMais: false},
		{name: "limite um a menos que o total", apos: 0, limite: len(ids) - 1, wantIDs: ids[:len(ids)-1], wantTemMais: true},
		{name: "limite maior que o total", apos: 0, limite: LimiteListagemLivros, wantIDs: ids, wantTemMais: false},
		{name: "limite 1", apos: 0, limite: 1, wantIDs: ids[:1], wantTemMais: true},
		{name: "limite 1 no penúltimo", apos: ids[len(ids)-2], limite: 1, wantIDs: ids[len(ids)-1:], wantTemMais: false},
		{name: "apos no meio de um buraco", apos: ids[len(ids)-2] + 1, limite: 3, wantIDs: ids[len(ids)-1:], wantTemMais: false},
		{name: "apos igual ao último", apos: ultimo, limite: 3, wantIDs: nil, wantTemMais: false},
		{name: "apos além do último", apos: ultimo + 1000, limite: 3, wantIDs: nil, wantTemMais: false},
		{name: "apos máximo int64", apos: 1<<63 - 1, limite: 3, wantIDs: nil, wantTemMais: false},
		{name: "apos exclusivo", apos: ids[0], limite: 1, wantIDs: ids[1:2], wantTemMais: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, temMais, err := f.repo.ListarLivros(t.Context(), tt.apos, tt.limite)
			if err != nil {
				t.Fatalf("ListarLivros(%d, %d): %v", tt.apos, tt.limite, err)
			}
			if got == nil {
				t.Error("ListarLivros devolveu nil, want slice (vazia se não houver linhas)")
			}
			if fmt.Sprint(idsDe(got)) != fmt.Sprint(idsOuVazio(tt.wantIDs)) {
				t.Errorf("ids = %v, want %v", idsDe(got), idsOuVazio(tt.wantIDs))
			}
			if temMais != tt.wantTemMais {
				t.Errorf("temMais = %v, want %v", temMais, tt.wantTemMais)
			}
			for _, l := range got {
				for _, w := range todos {
					if w.ID == l.ID && w.Titulo != l.Titulo {
						t.Errorf("livro %d titulo = %q, want %q", l.ID, l.Titulo, w.Titulo)
					}
				}
			}
		})
	}
}

func idsOuVazio(ids []int64) []int64 {
	if ids == nil {
		return []int64{}
	}
	return ids
}

// Percorrer o acervo com apos = id do último item entregue visita cada livro
// exatamente uma vez, em ordem, para todo limite; a última página tem
// temMais falso.
func TestListarLivrosPercorrerTudo(t *testing.T) {
	f := novoFixture(t)
	todos := acervo(t, f, 9) // 11 livros
	want := fmt.Sprint(idsDe(todos))

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
				got, temMais, err := f.repo.ListarLivros(t.Context(), apos, limite)
				if err != nil {
					t.Fatalf("ListarLivros(%d, %d): %v", apos, limite, err)
				}
				if len(got) > limite {
					t.Fatalf("%d itens, want no máximo %d", len(got), limite)
				}
				visitados = append(visitados, idsDe(got)...)
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
			wantPaginas := (len(todos) + limite - 1) / limite
			if paginas != wantPaginas {
				t.Errorf("%d páginas, want %d", paginas, wantPaginas)
			}
		})
	}
}

// Tabela de livros vazia: página vazia (não nil) e temMais falso.
func TestListarLivrosVazio(t *testing.T) {
	_, c := novoBancoMigrado(t)
	r := NovoRepositorio(c)
	got, temMais, err := r.ListarLivros(t.Context(), 0, 20)
	if err != nil {
		t.Fatalf("ListarLivros: %v", err)
	}
	if got == nil || len(got) != 0 || temMais {
		t.Errorf("ListarLivros = %#v, %v; want [] e false", got, temMais)
	}
}

// Parâmetros inválidos são recusados antes do SQL também com banco real.
func TestListarLivrosRecusas(t *testing.T) {
	f := novoFixture(t)
	tests := []struct {
		name   string
		apos   int64
		limite int
	}{
		{name: "limite 0", apos: 0, limite: 0},
		{name: "limite acima do teto", apos: 0, limite: LimiteListagemLivros + 1},
		{name: "apos negativo", apos: -1, limite: 10},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, temMais, err := f.repo.ListarLivros(t.Context(), tt.apos, tt.limite)
			if !errors.Is(err, ErrListagemInvalida) {
				t.Errorf("erro = %v, want %v", err, ErrListagemInvalida)
			}
			if len(got) != 0 || temMais {
				t.Errorf("com erro devolveu %v, %v", got, temMais)
			}
		})
	}
}

// BuscarEmprestimo lê pelo id, aberto, renovado ou devolvido; inexistente é
// ErrEmprestimoNaoEncontrado.
func TestBuscarEmprestimo(t *testing.T) {
	f := novoFixture(t)
	ctx := t.Context()
	sp := saoPaulo(t)

	// Empréstimo devolvido do livro do fixture.
	n1, err := domain.NovoEmprestimo(f.livro.ID, f.usuario.ID, time.Date(2026, 3, 10, 10, 0, 0, 123456789, sp))
	if err != nil {
		t.Fatalf("NovoEmprestimo: %v", err)
	}
	e1, err := f.repo.AbrirEmprestimo(ctx, n1)
	if err != nil {
		t.Fatalf("AbrirEmprestimo: %v", err)
	}
	if err := e1.Devolver(time.Date(2026, 3, 26, 12, 0, 0, 987654321, sp)); err != nil {
		t.Fatalf("Devolver: %v", err)
	}
	devolvido, err := f.repo.RegistrarDevolucao(ctx, e1)
	if err != nil {
		t.Fatalf("RegistrarDevolucao: %v", err)
	}

	// Empréstimo aberto do mesmo livro, depois renovado.
	n2, err := domain.NovoEmprestimo(f.livro.ID, f.usuario.ID, time.Date(2026, 4, 1, 9, 0, 0, 0, sp))
	if err != nil {
		t.Fatalf("NovoEmprestimo: %v", err)
	}
	aberto, err := f.repo.AbrirEmprestimo(ctx, n2)
	if err != nil {
		t.Fatalf("AbrirEmprestimo: %v", err)
	}

	// Empréstimo aberto de outro livro e outro usuário.
	l3, u3 := f.novoLivro(t, "Quincas Borba"), f.novoUsuario(t, "Bento")
	n3, err := domain.NovoEmprestimo(l3.ID, u3.ID, time.Date(2026, 4, 2, 9, 0, 0, 0, sp))
	if err != nil {
		t.Fatalf("NovoEmprestimo: %v", err)
	}
	outro, err := f.repo.AbrirEmprestimo(ctx, n3)
	if err != nil {
		t.Fatalf("AbrirEmprestimo: %v", err)
	}

	tests := []struct {
		name string
		id   int64
		want domain.Emprestimo
	}{
		{name: "devolvido", id: devolvido.ID(), want: devolvido},
		{name: "aberto", id: aberto.ID(), want: aberto},
		{name: "outro livro e usuário", id: outro.ID(), want: outro},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.repo.BuscarEmprestimo(ctx, tt.id)
			if err != nil {
				t.Fatalf("BuscarEmprestimo(%d): %v", tt.id, err)
			}
			checarEmprestimo(t, got, tt.want)
		})
	}

	t.Run("renovado", func(t *testing.T) {
		e, err := f.repo.BuscarEmprestimo(ctx, aberto.ID())
		if err != nil {
			t.Fatalf("BuscarEmprestimo: %v", err)
		}
		if err := e.Renovar(time.Date(2026, 4, 3, 9, 0, 0, 0, sp)); err != nil {
			t.Fatalf("Renovar: %v", err)
		}
		renovado, err := f.repo.RegistrarRenovacao(ctx, e)
		if err != nil {
			t.Fatalf("RegistrarRenovacao: %v", err)
		}
		got, err := f.repo.BuscarEmprestimo(ctx, aberto.ID())
		if err != nil {
			t.Fatalf("BuscarEmprestimo: %v", err)
		}
		checarEmprestimo(t, got, renovado)
		if got.Renovacoes() != 1 {
			t.Errorf("Renovacoes() = %d, want 1", got.Renovacoes())
		}
	})

	for _, id := range []int64{outro.ID() + 1000, 1<<63 - 1} {
		t.Run(fmt.Sprintf("inexistente %d", id), func(t *testing.T) {
			_, err := f.repo.BuscarEmprestimo(ctx, id)
			if !errors.Is(err, ErrEmprestimoNaoEncontrado) {
				t.Errorf("erro = %v, want %v", err, ErrEmprestimoNaoEncontrado)
			}
		})
	}
}
