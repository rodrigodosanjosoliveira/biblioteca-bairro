package storage

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
)

// DEL-017: disponibilidade de livro na leitura (BuscarLivro e ListarLivros).
// Disponível = não existe empréstimo com devolucao IS NULL para o livro; o
// vencimento não importa (vencido não devolvido é indisponível).

// cenarioDisp é um livro com um histórico de empréstimos e a disponibilidade
// esperada.
type cenarioDisp struct {
	name  string
	livro domain.Livro
	want  bool
}

// abrirEmp abre e grava um empréstimo do livro para o usuário do fixture.
func abrirEmp(t *testing.T, f fixture, livroID int64, inicio time.Time) domain.Emprestimo {
	t.Helper()
	n, err := domain.NovoEmprestimo(livroID, f.usuario.ID, inicio)
	if err != nil {
		t.Fatalf("NovoEmprestimo: %v", err)
	}
	e, err := f.repo.AbrirEmprestimo(t.Context(), n)
	if err != nil {
		t.Fatalf("AbrirEmprestimo: %v", err)
	}
	return e
}

func devolverEmp(t *testing.T, f fixture, e domain.Emprestimo, em time.Time) domain.Emprestimo {
	t.Helper()
	if err := e.Devolver(em); err != nil {
		t.Fatalf("Devolver: %v", err)
	}
	got, err := f.repo.RegistrarDevolucao(t.Context(), e)
	if err != nil {
		t.Fatalf("RegistrarDevolucao: %v", err)
	}
	return got
}

func renovarEmp(t *testing.T, f fixture, e domain.Emprestimo, ref time.Time) domain.Emprestimo {
	t.Helper()
	if err := e.Renovar(ref); err != nil {
		t.Fatalf("Renovar: %v", err)
	}
	got, err := f.repo.RegistrarRenovacao(t.Context(), e)
	if err != nil {
		t.Fatalf("RegistrarRenovacao: %v", err)
	}
	return got
}

// cenariosDisp monta, em ordem de id, livros cujas disponibilidades
// alternam (true, false, true, ...) para que uma página mista acuse
// vazamento de valor entre itens vizinhos. "No prazo" usa o relógio real
// (vencimento no futuro também para now() do banco) e "vencido" um início
// em 2020 (vencido para qualquer relógio): a consulta não pode depender de
// data. O livro do fixture (sem empréstimo) vem antes de todos.
func cenariosDisp(t *testing.T, f fixture) []cenarioDisp {
	t.Helper()
	sp := saoPaulo(t)
	agora := time.Now().In(sp)
	passado := time.Date(2020, 1, 10, 10, 0, 0, 0, sp)

	var out []cenarioDisp
	add := func(name string, want bool, historico func(l domain.Livro)) {
		l := f.novoLivro(t, "Livro "+name)
		historico(l)
		out = append(out, cenarioDisp{name: name, livro: l, want: want})
	}

	add("sem empréstimo", true, func(domain.Livro) {})
	add("aberto no prazo", false, func(l domain.Livro) {
		abrirEmp(t, f, l.ID, agora)
	})
	add("devolvido", true, func(l domain.Livro) {
		e := abrirEmp(t, f, l.ID, passado)
		devolverEmp(t, f, e, passado.AddDate(0, 0, 3))
	})
	add("aberto vencido", false, func(l domain.Livro) {
		abrirEmp(t, f, l.ID, passado)
	})
	add("vários devolvidos", true, func(l domain.Livro) {
		for i := 0; i < 3; i++ {
			ini := passado.AddDate(0, i, 0)
			e := abrirEmp(t, f, l.ID, ini)
			devolverEmp(t, f, e, ini.AddDate(0, 0, 1))
		}
	})
	add("renovado", false, func(l domain.Livro) {
		e := abrirEmp(t, f, l.ID, agora)
		renovarEmp(t, f, e, agora)
	})
	add("renovado e devolvido", true, func(l domain.Livro) {
		e := abrirEmp(t, f, l.ID, agora)
		e = renovarEmp(t, f, e, agora)
		devolverEmp(t, f, e, agora)
	})
	add("devolvido e reaberto", false, func(l domain.Livro) {
		e := abrirEmp(t, f, l.ID, passado)
		devolverEmp(t, f, e, passado.AddDate(0, 0, 2))
		e = abrirEmp(t, f, l.ID, passado.AddDate(0, 1, 0))
		devolverEmp(t, f, e, passado.AddDate(0, 1, 2))
		abrirEmp(t, f, l.ID, agora)
	})
	add("devolvido depois do vencimento", true, func(l domain.Livro) {
		e := abrirEmp(t, f, l.ID, passado)
		devolverEmp(t, f, e, passado.AddDate(1, 0, 0))
	})

	for i := 1; i < len(out); i++ {
		if out[i].want == out[i-1].want {
			t.Fatalf("cenários %q e %q vizinhos com o mesmo valor: a página mista precisa alternar", out[i-1].name, out[i].name)
		}
	}
	return out
}

// TestBuscarLivroDisponibilidade (CA3): o detalhe devolve o livro e o
// disponivel derivado do empréstimo aberto.
func TestBuscarLivroDisponibilidade(t *testing.T) {
	f := novoFixture(t)
	cs := append([]cenarioDisp{{name: "livro do fixture", livro: f.livro, want: true}}, cenariosDisp(t, f)...)
	for _, tt := range cs {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.repo.BuscarLivro(t.Context(), tt.livro.ID)
			if err != nil {
				t.Fatalf("BuscarLivro(%d): %v", tt.livro.ID, err)
			}
			if got.Livro != tt.livro {
				t.Errorf("Livro = %+v, want %+v", got.Livro, tt.livro)
			}
			if got.Disponivel != tt.want {
				t.Errorf("Disponivel = %v, want %v", got.Disponivel, tt.want)
			}
		})
	}
}

// TestBuscarLivroDisponibilidadeTransicoes (CA3): o mesmo livro muda de
// valor a cada operação, na ordem abrir → renovar → devolver → reabrir.
func TestBuscarLivroDisponibilidadeTransicoes(t *testing.T) {
	f := novoFixture(t)
	sp := saoPaulo(t)
	agora := time.Now().In(sp)
	l := f.novoLivro(t, "Helena")
	var e domain.Emprestimo

	passos := []struct {
		name string
		op   func(t *testing.T)
		want bool
	}{
		{name: "novo", op: func(*testing.T) {}, want: true},
		{name: "aberto", op: func(t *testing.T) { e = abrirEmp(t, f, l.ID, agora) }, want: false},
		{name: "renovado", op: func(t *testing.T) { e = renovarEmp(t, f, e, agora) }, want: false},
		{name: "devolvido", op: func(t *testing.T) { e = devolverEmp(t, f, e, agora) }, want: true},
		{name: "reaberto", op: func(t *testing.T) { e = abrirEmp(t, f, l.ID, agora) }, want: false},
	}
	for _, p := range passos {
		t.Run(p.name, func(t *testing.T) {
			p.op(t)
			got, err := f.repo.BuscarLivro(t.Context(), l.ID)
			if err != nil {
				t.Fatalf("BuscarLivro: %v", err)
			}
			if got.Livro != l || got.Disponivel != p.want {
				t.Errorf("BuscarLivro = %+v, want {%+v %v}", got, l, p.want)
			}
		})
	}
}

// TestBuscarLivroDisponibilidadeInexistente: livro apagado, id além do
// último, id <= 0 e máximo int64 continuam ErrLivroNaoEncontrado com valor
// zero.
func TestBuscarLivroDisponibilidadeInexistente(t *testing.T) {
	f := novoFixture(t)
	apagado := f.novoLivro(t, "apagado")
	if _, err := f.conn.Exec(t.Context(), "DELETE FROM livro WHERE id = $1", apagado.ID); err != nil {
		t.Fatalf("apagar livro: %v", err)
	}
	for _, id := range []int64{apagado.ID, apagado.ID + 1000, 1<<63 - 1, 0, -1} {
		t.Run(fmt.Sprint(id), func(t *testing.T) {
			got, err := f.repo.BuscarLivro(t.Context(), id)
			if !errors.Is(err, ErrLivroNaoEncontrado) {
				t.Errorf("erro = %v, want %v", err, ErrLivroNaoEncontrado)
			}
			if got != (LivroComDisponibilidade{}) {
				t.Errorf("com erro devolveu %+v, want valor zero", got)
			}
		})
	}
}

// TestListarLivrosDisponibilidade (CA4 e CA2): cada item da página traz o
// próprio valor, sem vazamento entre vizinhos, para todo tamanho de página;
// livro com vários empréstimos não duplica item nem consome limite a mais
// (LIMIT conta livros) e a paginação keyset continua a mesma.
func TestListarLivrosDisponibilidade(t *testing.T) {
	f := novoFixture(t)
	cs := append([]cenarioDisp{{name: "livro do fixture", livro: f.livro, want: true}}, cenariosDisp(t, f)...)
	want := map[int64]cenarioDisp{}
	var ids []int64
	for _, c := range cs {
		want[c.livro.ID] = c
		ids = append(ids, c.livro.ID)
	}

	for limite := 1; limite <= len(cs)+1; limite++ {
		t.Run(fmt.Sprintf("limite %d", limite), func(t *testing.T) {
			var visitados []int64
			var apos int64
			for pag := 0; ; pag++ {
				if pag > len(cs)+1 {
					t.Fatalf("paginação não termina: %v", visitados)
				}
				got, temMais, err := f.repo.ListarLivros(t.Context(), apos, limite)
				if err != nil {
					t.Fatalf("ListarLivros(%d, %d): %v", apos, limite, err)
				}
				restantes := len(cs) - len(visitados)
				if wantN := min(limite, restantes); len(got) != wantN {
					t.Fatalf("página %d com %d itens, want %d (LIMIT conta livros)", pag, len(got), wantN)
				}
				if wantMais := restantes > limite; temMais != wantMais {
					t.Errorf("página %d temMais = %v, want %v", pag, temMais, wantMais)
				}
				for _, it := range got {
					c, ok := want[it.Livro.ID]
					if !ok {
						t.Fatalf("livro %d fora do acervo", it.Livro.ID)
					}
					if it.Livro != c.livro {
						t.Errorf("Livro = %+v, want %+v", it.Livro, c.livro)
					}
					if it.Disponivel != c.want {
						t.Errorf("livro %d (%s) Disponivel = %v, want %v", it.Livro.ID, c.name, it.Disponivel, c.want)
					}
					visitados = append(visitados, it.Livro.ID)
				}
				if !temMais {
					break
				}
				apos = got[len(got)-1].Livro.ID
			}
			if fmt.Sprint(visitados) != fmt.Sprint(ids) {
				t.Errorf("visitados = %v, want %v (cada livro uma vez, em ordem de id)", visitados, ids)
			}
		})
	}

	// A página inteira e as páginas de um item dão o mesmo valor por livro.
	t.Run("página inteira igual às páginas unitárias", func(t *testing.T) {
		inteira, temMais, err := f.repo.ListarLivros(t.Context(), 0, LimiteListagemLivros)
		if err != nil || temMais {
			t.Fatalf("ListarLivros = %v, %v", temMais, err)
		}
		for i, it := range inteira {
			unit, _, err := f.repo.ListarLivros(t.Context(), it.Livro.ID-1, 1)
			if err != nil || len(unit) != 1 {
				t.Fatalf("ListarLivros(%d, 1) = %v, %v", it.Livro.ID-1, unit, err)
			}
			if unit[0] != it {
				t.Errorf("item %d: página inteira %+v, unitária %+v", i, it, unit[0])
			}
		}
	})
}

// TestListarLivrosDisponibilidadeTodosEmprestados: página só com
// indisponíveis (nenhum true herdado de outro livro) e, depois de devolver
// tudo, só com disponíveis.
func TestListarLivrosDisponibilidadeTodosEmprestados(t *testing.T) {
	_, c := novoBancoMigrado(t)
	r := NovoRepositorio(c)
	u, err := domain.NovoUsuario("Ana")
	if err != nil {
		t.Fatalf("NovoUsuario: %v", err)
	}
	u, err = r.InserirUsuario(t.Context(), u)
	if err != nil {
		t.Fatalf("InserirUsuario: %v", err)
	}
	f := fixture{repo: r, conn: c, usuario: u}
	agora := time.Now().In(saoPaulo(t))
	var abertos []domain.Emprestimo
	for i := 0; i < 4; i++ {
		l := f.novoLivro(t, fmt.Sprintf("L%d", i))
		abertos = append(abertos, abrirEmp(t, f, l.ID, agora))
	}
	tests := []struct {
		name string
		prep func(t *testing.T)
		want bool
	}{
		{name: "todos abertos", prep: func(*testing.T) {}, want: false},
		{name: "todos devolvidos", prep: func(t *testing.T) {
			for _, e := range abertos {
				devolverEmp(t, f, e, agora)
			}
		}, want: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.prep(t)
			got, _, err := r.ListarLivros(t.Context(), 0, 10)
			if err != nil {
				t.Fatalf("ListarLivros: %v", err)
			}
			if len(got) != 4 {
				t.Fatalf("%d itens, want 4", len(got))
			}
			for _, it := range got {
				if it.Disponivel != tt.want {
					t.Errorf("livro %d Disponivel = %v, want %v", it.Livro.ID, it.Disponivel, tt.want)
				}
			}
		})
	}
}

// dbContador repassa ao banco real e grava cada comando enviado.
type dbContador struct {
	db   DBTX
	sqls []string
}

func (d *dbContador) Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	d.sqls = append(d.sqls, sql)
	return d.db.Exec(ctx, sql, args...)
}

func (d *dbContador) Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error) {
	d.sqls = append(d.sqls, sql)
	return d.db.Query(ctx, sql, args...)
}

func (d *dbContador) QueryRow(ctx context.Context, sql string, args ...any) pgx.Row {
	d.sqls = append(d.sqls, sql)
	return d.db.QueryRow(ctx, sql, args...)
}

// TestLeituraLivroUmaConsulta (CA5): uma consulta SQL por página e por
// detalhe, com o texto das constantes que o teste de plano examina (sem N+1
// e sem segunda consulta para a disponibilidade).
func TestLeituraLivroUmaConsulta(t *testing.T) {
	f := novoFixture(t)
	cs := cenariosDisp(t, f)
	ultimo := cs[len(cs)-1].livro.ID

	tests := []struct {
		name    string
		chamar  func(r *Repositorio) error
		wantSQL []string
	}{
		{name: "página mista", wantSQL: []string{sqlListarLivros}, chamar: func(r *Repositorio) error {
			got, _, err := r.ListarLivros(t.Context(), 0, LimiteListagemLivros)
			if err == nil && len(got) != len(cs)+1 {
				return fmt.Errorf("%d itens, want %d", len(got), len(cs)+1)
			}
			return err
		}},
		{name: "página vazia", wantSQL: []string{sqlListarLivros}, chamar: func(r *Repositorio) error {
			_, _, err := r.ListarLivros(t.Context(), ultimo, 20)
			return err
		}},
		{name: "detalhe disponível", wantSQL: []string{sqlBuscarLivro}, chamar: func(r *Repositorio) error {
			_, err := r.BuscarLivro(t.Context(), cs[0].livro.ID)
			return err
		}},
		{name: "detalhe indisponível", wantSQL: []string{sqlBuscarLivro}, chamar: func(r *Repositorio) error {
			_, err := r.BuscarLivro(t.Context(), cs[1].livro.ID)
			return err
		}},
		{name: "detalhe inexistente", wantSQL: []string{sqlBuscarLivro}, chamar: func(r *Repositorio) error {
			if _, err := r.BuscarLivro(t.Context(), ultimo+1000); !errors.Is(err, ErrLivroNaoEncontrado) {
				return fmt.Errorf("erro = %v, want %v", err, ErrLivroNaoEncontrado)
			}
			return nil
		}},
		{name: "detalhe com id zero não consulta", wantSQL: nil, chamar: func(r *Repositorio) error {
			if _, err := r.BuscarLivro(t.Context(), 0); !errors.Is(err, ErrLivroNaoEncontrado) {
				return fmt.Errorf("erro = %v, want %v", err, ErrLivroNaoEncontrado)
			}
			return nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &dbContador{db: f.conn}
			if err := tt.chamar(NovoRepositorio(db)); err != nil {
				t.Fatal(err)
			}
			if fmt.Sprintf("%q", db.sqls) != fmt.Sprintf("%q", tt.wantSQL) {
				t.Errorf("comandos = %q, want %q", db.sqls, tt.wantSQL)
			}
		})
	}
}
