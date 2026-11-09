package storage

import (
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
)

// DEL-015 (D-21): ListarEmprestimos com FiltroEmprestimos no banco real.

// diaD é a data civil de referência dos vencimentos da massa (24/03/2026).
var diaD = time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC)

// linhaMassa descreve um empréstimo gravado por SQL direto: índices de
// livro e usuário da massa, vencimento em dias relativos a diaD, aberto ou
// devolvido e o contador de renovações.
type linhaMassa struct {
	livro, usuario int
	vencDias       int
	aberto         bool
	renovacoes     int
}

// massaFiltro: 6 livros, 4 usuários (o último sem empréstimo). No máximo um
// aberto por livro (R4). Os abertos têm vencimento em D-2, D-1, D e D+1 (e
// D+5), alguns com renovações (Renovar confere vencido antes do limite).
var massaFiltro = []linhaMassa{
	{livro: 0, usuario: 0, vencDias: -5},                              // 0 devolvido
	{livro: 1, usuario: 1, vencDias: -1},                              // 1 devolvido
	{livro: 0, usuario: 1, vencDias: 3},                               // 2 devolvido
	{livro: 2, usuario: 0, vencDias: -2},                              // 3 devolvido
	{livro: 1, usuario: 1, vencDias: 0},                               // 4 devolvido
	{livro: 0, usuario: 1, vencDias: -1, aberto: true},                // 5 aberto, vence D-1
	{livro: 1, usuario: 0, vencDias: 0, aberto: true, renovacoes: 2},  // 6 aberto, vence D
	{livro: 2, usuario: 1, vencDias: 1, aberto: true},                 // 7 aberto, vence D+1
	{livro: 3, usuario: 1, vencDias: -2, aberto: true, renovacoes: 1}, // 8 aberto, vence D-2
	{livro: 4, usuario: 2, vencDias: -1, aberto: true},                // 9 aberto, vence D-1
	{livro: 5, usuario: 0, vencDias: 5, aberto: true},                 // 10 aberto, vence D+5
}

// massa é o resultado de gravarMassa: ids de livros e usuários e os
// empréstimos relidos do banco, na ordem de massaFiltro (ids crescentes).
type massa struct {
	livros, usuarios []int64
	emprestimos      []domain.Emprestimo
}

// gravarMassa grava massaFiltro por SQL direto (vencimento controlado) e
// relê cada empréstimo com BuscarEmprestimo.
func gravarMassa(t *testing.T, f fixture) massa {
	t.Helper()
	ctx := t.Context()
	sp := saoPaulo(t)
	var m massa
	for i := 0; i < 6; i++ {
		m.livros = append(m.livros, f.novoLivro(t, fmt.Sprintf("Livro %d", i)).ID)
	}
	for i := 0; i < 4; i++ {
		m.usuarios = append(m.usuarios, f.novoUsuario(t, fmt.Sprintf("Usuario %d", i)).ID)
	}
	inicio := time.Date(2026, 3, 1, 10, 0, 0, 0, sp)
	devolucao := time.Date(2026, 3, 5, 10, 0, 0, 0, sp)
	for i, l := range massaFiltro {
		var dev *time.Time
		if !l.aberto {
			dev = &devolucao
		}
		var id int64
		err := f.conn.QueryRow(ctx,
			`INSERT INTO emprestimo (livro_id, usuario_id, inicio, vencimento, devolucao, renovacoes)
			 VALUES ($1, $2, $3, $4, $5, $6) RETURNING id`,
			m.livros[l.livro], m.usuarios[l.usuario], inicio, diaD.AddDate(0, 0, l.vencDias), dev, l.renovacoes).Scan(&id)
		if err != nil {
			t.Fatalf("inserir linha %d da massa: %v", i, err)
		}
		e, err := f.repo.BuscarEmprestimo(ctx, id)
		if err != nil {
			t.Fatalf("BuscarEmprestimo(%d): %v", id, err)
		}
		m.emprestimos = append(m.emprestimos, e)
	}
	return m
}

// idsDe devolve os ids dos empréstimos da massa nas posições dadas.
func (m massa) idsDe(pos ...int) []int64 {
	out := make([]int64, 0, len(pos))
	for _, p := range pos {
		out = append(out, m.emprestimos[p].ID())
	}
	return out
}

// vencidoNoDominio é o oráculo do domínio para "vencido em ref": aberto e
// com multa positiva. Confere, numa cópia, que Renovar concorda (D-17).
func vencidoNoDominio(t *testing.T, e domain.Emprestimo, ref time.Time) bool {
	t.Helper()
	if !e.Aberto() {
		return false
	}
	multa, err := e.Multa(ref)
	if err != nil {
		t.Fatalf("Multa(%v) do empréstimo %d: %v", ref, e.ID(), err)
	}
	copia := e
	errRenovar := copia.Renovar(ref)
	porMulta := multa.IsPositive()
	porRenovar := errors.Is(errRenovar, domain.ErrEmprestimoVencido)
	if porMulta != porRenovar {
		t.Fatalf("domínio diverge para %d em %v: multa %s, Renovar %v", e.ID(), ref, multa, errRenovar)
	}
	return porMulta
}

// oraculoFiltro aplica f à massa em Go, com o domínio como regra de situação.
func oraculoFiltro(t *testing.T, todos []domain.Emprestimo, f FiltroEmprestimos) []int64 {
	t.Helper()
	out := []int64{}
	for _, e := range todos {
		if f.UsuarioID != 0 && e.UsuarioID() != f.UsuarioID {
			continue
		}
		if f.LivroID != 0 && e.LivroID() != f.LivroID {
			continue
		}
		switch f.Situacao {
		case SituacaoAberto:
			if !e.Aberto() {
				continue
			}
		case SituacaoDevolvido:
			if e.Aberto() {
				continue
			}
		case SituacaoVencido:
			if !vencidoNoDominio(t, e, f.Ref) {
				continue
			}
		}
		out = append(out, e.ID())
	}
	return out
}

// percorrerFiltro segue as páginas de ListarEmprestimos com f e devolve os
// empréstimos visitados, conferindo o contrato de cada página.
func percorrerFiltro(t *testing.T, r *Repositorio, f FiltroEmprestimos, limite int) []domain.Emprestimo {
	t.Helper()
	var visitados []domain.Emprestimo
	var apos int64
	for paginas := 1; ; paginas++ {
		if paginas > 50 {
			t.Fatalf("paginação não termina: %v", idsEmprestimos(visitados))
		}
		got, temMais, err := r.ListarEmprestimos(t.Context(), f, apos, limite)
		if err != nil {
			t.Fatalf("ListarEmprestimos(%+v, %d, %d): %v", f, apos, limite, err)
		}
		if got == nil {
			t.Fatal("ListarEmprestimos devolveu nil, want slice")
		}
		if len(got) > limite {
			t.Fatalf("%d itens, want no máximo %d", len(got), limite)
		}
		visitados = append(visitados, got...)
		if !temMais {
			return visitados
		}
		if len(got) == 0 {
			t.Fatal("temMais com página vazia")
		}
		apos = got[len(got)-1].ID()
	}
}

// TestListarEmprestimosFiltroCombinacoes: as 16 combinações de filtro no
// banco devolvem exatamente o conjunto do oráculo em Go, em ordem de id, com
// qualquer limite; cada item é igual ao BuscarEmprestimo.
func TestListarEmprestimosFiltroCombinacoes(t *testing.T) {
	f := novoFixture(t)
	m := gravarMassa(t, f)
	// Relê tudo (inclui o empréstimo/usuário/livro do fixture: nenhum).
	todos := percorrerFiltro(t, f.repo, FiltroEmprestimos{}, LimiteListagem)
	if len(todos) != len(massaFiltro) {
		t.Fatalf("%d empréstimos no banco, want %d", len(todos), len(massaFiltro))
	}
	ref := time.Date(2026, 3, 24, 12, 0, 0, 0, saoPaulo(t))
	U, L := m.usuarios[1], m.livros[0]

	// Conjuntos esperados escritos à mão (independentes do oráculo) para as
	// combinações em que o erro seria mais provável.
	explicitos := map[string][]int64{
		"todas":                   m.idsDe(0, 1, 2, 3, 4, 5, 6, 7, 8, 9, 10),
		"aberto":                  m.idsDe(5, 6, 7, 8, 9, 10),
		"devolvido":               m.idsDe(0, 1, 2, 3, 4),
		"vencido":                 m.idsDe(5, 8, 9),
		"usuario":                 m.idsDe(1, 2, 4, 5, 7, 8),
		"usuario+vencido":         m.idsDe(5, 8),
		"livro":                   m.idsDe(0, 2, 5),
		"usuario+livro":           m.idsDe(2, 5),
		"usuario+livro+aberto":    m.idsDe(5),
		"usuario+livro+devolvido": m.idsDe(2),
		"usuario+livro+vencido":   m.idsDe(5),
		"livro+devolvido":         m.idsDe(0, 2),
		"usuario+devolvido":       m.idsDe(1, 2, 4),
		"usuario+aberto":          m.idsDe(5, 7, 8),
		"livro+aberto":            m.idsDe(5),
		"livro+vencido":           m.idsDe(5),
	}
	nomeSit := map[Situacao]string{SituacaoTodas: "", SituacaoAberto: "aberto", SituacaoDevolvido: "devolvido", SituacaoVencido: "vencido"}
	for _, u := range []int64{0, U} {
		for _, l := range []int64{0, L} {
			for _, s := range []Situacao{SituacaoTodas, SituacaoAberto, SituacaoDevolvido, SituacaoVencido} {
				filtro := FiltroEmprestimos{UsuarioID: u, LivroID: l, Situacao: s, Ref: ref}
				nome := ""
				if u != 0 {
					nome += "+usuario"
				}
				if l != 0 {
					nome += "+livro"
				}
				if nomeSit[s] != "" {
					nome += "+" + nomeSit[s]
				}
				if nome == "" {
					nome = "+todas"
				}
				nome = nome[1:]
				t.Run(nome, func(t *testing.T) {
					want := oraculoFiltro(t, todos, filtro)
					if exp, ok := explicitos[nome]; ok && fmt.Sprint(exp) != fmt.Sprint(want) {
						t.Fatalf("oráculo = %v, want à mão %v (massa ou oráculo errado)", want, exp)
					}
					for _, limite := range []int{1, 2, 3, LimiteListagem} {
						got := percorrerFiltro(t, f.repo, filtro, limite)
						if fmt.Sprint(idsEmprestimos(got)) != fmt.Sprint(want) {
							t.Errorf("limite %d: ids = %v, want %v", limite, idsEmprestimos(got), want)
							continue
						}
						for i := range got {
							checarEmprestimo(t, got[i], todos[indicePorID(t, todos, got[i].ID())])
						}
					}
				})
			}
		}
	}

	t.Run("primeira página com limite exato tem temMais falso", func(t *testing.T) {
		got, temMais, err := f.repo.ListarEmprestimos(t.Context(), FiltroEmprestimos{Situacao: SituacaoVencido, Ref: ref}, 0, 3)
		if err != nil || temMais || fmt.Sprint(idsEmprestimos(got)) != fmt.Sprint(m.idsDe(5, 8, 9)) {
			t.Errorf("= %v, %v, %v; want %v, false, nil", idsEmprestimos(got), temMais, err, m.idsDe(5, 8, 9))
		}
	})
	t.Run("apos combina com o filtro", func(t *testing.T) {
		got, temMais, err := f.repo.ListarEmprestimos(t.Context(), FiltroEmprestimos{UsuarioID: U}, m.emprestimos[4].ID(), 2)
		if err != nil || !temMais || fmt.Sprint(idsEmprestimos(got)) != fmt.Sprint(m.idsDe(5, 7)) {
			t.Errorf("= %v, %v, %v; want %v, true, nil", idsEmprestimos(got), temMais, err, m.idsDe(5, 7))
		}
	})
	for _, tt := range []struct {
		name   string
		filtro FiltroEmprestimos
	}{
		{name: "usuário sem empréstimos", filtro: FiltroEmprestimos{UsuarioID: m.usuarios[3]}},
		{name: "usuário inexistente", filtro: FiltroEmprestimos{UsuarioID: 1<<63 - 1}},
		{name: "livro inexistente", filtro: FiltroEmprestimos{LivroID: 999999}},
		{name: "usuário e livro sem empréstimo em comum", filtro: FiltroEmprestimos{UsuarioID: m.usuarios[2], LivroID: m.livros[0]}},
		{name: "livro do fixture sem empréstimo", filtro: FiltroEmprestimos{LivroID: f.livro.ID, Situacao: SituacaoAberto}},
	} {
		t.Run("vazio/"+tt.name, func(t *testing.T) {
			got, temMais, err := f.repo.ListarEmprestimos(t.Context(), tt.filtro, 0, 20)
			if err != nil {
				t.Fatalf("ListarEmprestimos: %v", err)
			}
			if got == nil || len(got) != 0 || temMais {
				t.Errorf("= %#v, %v; want [] e false", got, temMais)
			}
		})
	}
}

func indicePorID(t *testing.T, es []domain.Emprestimo, id int64) int {
	t.Helper()
	for i, e := range es {
		if e.ID() == id {
			return i
		}
	}
	t.Fatalf("id %d fora da massa", id)
	return -1
}

// TestListarEmprestimosVencidoFronteira (R3 do triage, obrigatório): para
// relógios em torno da meia-noite de São Paulo, o conjunto de situacao=vencido
// devolvido pelo SQL é exatamente {e aberto : e.Multa(ref) > 0} e
// {e aberto : e.Renovar(ref) == ErrEmprestimoVencido}, em qualquer fuso da
// sessão do banco. Vencimento igual a hoje não é vencido.
func TestListarEmprestimosVencidoFronteira(t *testing.T) {
	f := novoFixture(t)
	m := gravarMassa(t, f)
	sp := saoPaulo(t)

	tests := []struct {
		name string
		ref  time.Time
		want []int // posições em massaFiltro
	}{
		{name: "23/03 23:59:59 SP (24/03 02:59:59Z)", ref: time.Date(2026, 3, 24, 2, 59, 59, 0, time.UTC), want: []int{8}},
		{name: "24/03 00:00:00 SP (24/03 03:00:00Z)", ref: time.Date(2026, 3, 24, 3, 0, 0, 0, time.UTC), want: []int{5, 8, 9}},
		{name: "24/03 12:00 SP", ref: time.Date(2026, 3, 24, 12, 0, 0, 0, sp), want: []int{5, 8, 9}},
		{name: "24/03 23:59:59 SP (25/03 02:59:59Z)", ref: time.Date(2026, 3, 25, 2, 59, 59, 0, time.UTC), want: []int{5, 8, 9}},
		{name: "24/03 23:59:59.999999 SP", ref: time.Date(2026, 3, 24, 23, 59, 59, 999999000, sp), want: []int{5, 8, 9}},
		{name: "25/03 00:00:00 SP (25/03 03:00:00Z)", ref: time.Date(2026, 3, 25, 3, 0, 0, 0, time.UTC), want: []int{5, 6, 8, 9}},
		{name: "25/03 00:00:00 SP expresso em UTC+14", ref: time.Date(2026, 3, 25, 3, 0, 0, 0, time.UTC).In(time.FixedZone("UTC+14", 14*3600)), want: []int{5, 6, 8, 9}},
		{name: "muito antes", ref: time.Date(2026, 3, 1, 12, 0, 0, 0, sp), want: nil},
		{name: "muito depois", ref: time.Date(2026, 4, 30, 12, 0, 0, 0, sp), want: []int{5, 6, 7, 8, 9, 10}},
	}
	// O fuso da sessão do banco não pode mudar o resultado.
	for _, tz := range []string{"", "UTC", "Pacific/Kiritimati", "Pacific/Pago_Pago", "America/Sao_Paulo"} {
		var r *Repositorio
		if tz == "" {
			r = f.repo
		} else {
			r = NovoRepositorio(f.banco.conectar(t, "timezone", tz))
		}
		for _, tt := range tests {
			t.Run(fmt.Sprintf("sessão %q/%s", tz, tt.name), func(t *testing.T) {
				want := m.idsDe(tt.want...)
				var porMulta, porRenovar []int64
				for _, e := range m.emprestimos {
					if !e.Aberto() {
						continue
					}
					multa, err := e.Multa(tt.ref)
					if err != nil {
						t.Fatalf("Multa: %v", err)
					}
					if multa.IsPositive() {
						porMulta = append(porMulta, e.ID())
					}
					copia := e
					if err := copia.Renovar(tt.ref); errors.Is(err, domain.ErrEmprestimoVencido) {
						porRenovar = append(porRenovar, e.ID())
					}
				}
				if fmt.Sprint(idsOuVazio(porMulta)) != fmt.Sprint(idsOuVazio(want)) || fmt.Sprint(idsOuVazio(porRenovar)) != fmt.Sprint(idsOuVazio(want)) {
					t.Fatalf("oráculos do domínio: multa %v, Renovar %v, want à mão %v", porMulta, porRenovar, want)
				}
				got := percorrerFiltro(t, r, FiltroEmprestimos{Situacao: SituacaoVencido, Ref: tt.ref}, LimiteListagem)
				if fmt.Sprint(idsEmprestimos(got)) != fmt.Sprint(idsOuVazio(porMulta)) {
					t.Errorf("SQL vencido = %v, want {Multa > 0} = %v", idsEmprestimos(got), porMulta)
				}
				if fmt.Sprint(idsEmprestimos(got)) != fmt.Sprint(idsOuVazio(porRenovar)) {
					t.Errorf("SQL vencido = %v, want {Renovar = ErrEmprestimoVencido} = %v", idsEmprestimos(got), porRenovar)
				}
				// aberto inclui os vencidos e não depende do relógio.
				abertos := percorrerFiltro(t, r, FiltroEmprestimos{Situacao: SituacaoAberto, Ref: tt.ref}, LimiteListagem)
				if fmt.Sprint(idsEmprestimos(abertos)) != fmt.Sprint(m.idsDe(5, 6, 7, 8, 9, 10)) {
					t.Errorf("aberto = %v, want %v", idsEmprestimos(abertos), m.idsDe(5, 6, 7, 8, 9, 10))
				}
				// Com usuário: o mesmo recorte do conjunto vencido.
				U := m.usuarios[1]
				var wantU []int64
				for _, id := range idsOuVazio(porMulta) {
					if m.emprestimos[indicePorID(t, m.emprestimos, id)].UsuarioID() == U {
						wantU = append(wantU, id)
					}
				}
				gotU := percorrerFiltro(t, r, FiltroEmprestimos{UsuarioID: U, Situacao: SituacaoVencido, Ref: tt.ref}, 1)
				if fmt.Sprint(idsEmprestimos(gotU)) != fmt.Sprint(idsOuVazio(wantU)) {
					t.Errorf("usuario+vencido = %v, want %v", idsEmprestimos(gotU), idsOuVazio(wantU))
				}
			})
		}
	}
}
