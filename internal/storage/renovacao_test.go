package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
)

// abrirParaRenovar abre o empréstimo-base (início 10/03/2026 10:00 SP,
// vencimento 24/03/2026) do livro e usuário do fixture.
func abrirParaRenovar(t *testing.T, f fixture) domain.Emprestimo {
	t.Helper()
	e, err := domain.NovoEmprestimo(f.livro.ID, f.usuario.ID, time.Date(2026, 3, 10, 10, 0, 0, 0, saoPaulo(t)))
	if err != nil {
		t.Fatalf("NovoEmprestimo: %v", err)
	}
	aberto, err := f.repo.AbrirEmprestimo(t.Context(), e)
	if err != nil {
		t.Fatalf("AbrirEmprestimo: %v", err)
	}
	return aberto
}

// linhaGravada lê vencimento (como texto da DATE) e renovacoes direto do banco.
func (f fixture) linhaGravada(t *testing.T, id int64) (string, int) {
	t.Helper()
	var venc string
	var renov int
	if err := f.conn.QueryRow(t.Context(),
		"SELECT vencimento::text, renovacoes FROM emprestimo WHERE id = $1", id).Scan(&venc, &renov); err != nil {
		t.Fatalf("ler linha %d: %v", id, err)
	}
	return venc, renov
}

func TestAbrirEmprestimoSemRenovacoes(t *testing.T) {
	f := novoFixture(t)
	aberto := abrirParaRenovar(t, f)
	if aberto.Renovacoes() != 0 {
		t.Errorf("AbrirEmprestimo Renovacoes() = %d, want 0", aberto.Renovacoes())
	}
	if _, renov := f.linhaGravada(t, aberto.ID()); renov != 0 {
		t.Errorf("renovacoes gravado = %d, want 0", renov)
	}
}

// Duas renovações seguidas: cada uma grava vencimento e contador, e as
// leituras trazem o contador.
func TestRegistrarRenovacao(t *testing.T) {
	f := novoFixture(t)
	ctx := t.Context()
	sp := saoPaulo(t)
	aberto := abrirParaRenovar(t, f)

	passos := []struct {
		ref            time.Time
		wantM          time.Month
		wantD          int
		wantDate       string
		wantRenovacoes int
	}{
		{ref: time.Date(2026, 3, 20, 10, 0, 0, 0, sp), wantM: 4, wantD: 7, wantDate: "2026-04-07", wantRenovacoes: 1},
		{ref: time.Date(2026, 4, 7, 23, 0, 0, 0, sp), wantM: 4, wantD: 21, wantDate: "2026-04-21", wantRenovacoes: 2},
	}
	atual := aberto
	for i, p := range passos {
		lido, err := f.repo.BuscarEmprestimoAbertoPorLivro(ctx, f.livro.ID)
		if err != nil {
			t.Fatalf("passo %d: BuscarEmprestimoAbertoPorLivro: %v", i+1, err)
		}
		checarEmprestimo(t, lido, atual)
		if err := lido.Renovar(p.ref); err != nil {
			t.Fatalf("passo %d: Renovar: %v", i+1, err)
		}
		got, err := f.repo.RegistrarRenovacao(ctx, lido)
		if err != nil {
			t.Fatalf("passo %d: RegistrarRenovacao: %v", i+1, err)
		}
		checarEmprestimo(t, got, lido)
		checarVencimentoSP(t, got.Vencimento(), 2026, p.wantM, p.wantD)
		if got.Renovacoes() != p.wantRenovacoes {
			t.Errorf("passo %d: Renovacoes() = %d, want %d", i+1, got.Renovacoes(), p.wantRenovacoes)
		}
		venc, renov := f.linhaGravada(t, aberto.ID())
		if venc != p.wantDate || renov != p.wantRenovacoes {
			t.Errorf("passo %d: gravado (%s, %d), want (%s, %d)", i+1, venc, renov, p.wantDate, p.wantRenovacoes)
		}
		atual = got
	}

	// Lido de novo pelo repositório, o contador volta.
	lido, err := f.repo.BuscarEmprestimoAbertoPorLivro(ctx, f.livro.ID)
	if err != nil {
		t.Fatalf("BuscarEmprestimoAbertoPorLivro: %v", err)
	}
	if lido.Renovacoes() != 2 {
		t.Errorf("Renovacoes() lido = %d, want 2", lido.Renovacoes())
	}
	if err := lido.Renovar(time.Date(2026, 4, 10, 10, 0, 0, 0, sp)); !errors.Is(err, domain.ErrLimiteRenovacoes) {
		t.Errorf("terceira Renovar erro = %v, want %v", err, domain.ErrLimiteRenovacoes)
	}

	// A devolução preserva o contador e devolve o empréstimo com ele.
	if err := lido.Devolver(time.Date(2026, 4, 12, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Devolver: %v", err)
	}
	devolvido, err := f.repo.RegistrarDevolucao(ctx, lido)
	if err != nil {
		t.Fatalf("RegistrarDevolucao: %v", err)
	}
	if devolvido.Renovacoes() != 2 {
		t.Errorf("RegistrarDevolucao Renovacoes() = %d, want 2", devolvido.Renovacoes())
	}
	checarVencimentoSP(t, devolvido.Vencimento(), 2026, 4, 21)
}

// Falhas do UPDATE condicional: nada é gravado e o diagnóstico distingue
// inexistente, devolvido e divergente.
func TestRegistrarRenovacaoRecusas(t *testing.T) {
	sp := saoPaulo(t)
	inicioReal := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)
	noPrazo := time.Date(2026, 3, 20, 10, 0, 0, 0, sp)

	tests := []struct {
		name string
		// preparar devolve o empréstimo a passar para RegistrarRenovacao.
		preparar func(t *testing.T, f fixture, aberto domain.Emprestimo) domain.Emprestimo
		wantErr  error
		// Estado esperado da linha depois da recusa.
		wantDate       string
		wantRenovacoes int
	}{
		{
			name: "inexistente",
			preparar: func(t *testing.T, f fixture, aberto domain.Emprestimo) domain.Emprestimo {
				e, err := domain.ReidratarEmprestimo(aberto.ID()+1000, aberto.LivroID(), aberto.UsuarioID(), aberto.Inicio(), aberto.Vencimento(), time.Time{}, 0)
				if err != nil {
					t.Fatalf("ReidratarEmprestimo: %v", err)
				}
				if err := e.Renovar(noPrazo); err != nil {
					t.Fatalf("Renovar: %v", err)
				}
				return e
			},
			wantErr: ErrEmprestimoNaoEncontrado, wantDate: "2026-03-24", wantRenovacoes: 0,
		},
		{
			name: "devolvido depois da leitura",
			preparar: func(t *testing.T, f fixture, aberto domain.Emprestimo) domain.Emprestimo {
				renovado := aberto
				if err := renovado.Renovar(noPrazo); err != nil {
					t.Fatalf("Renovar: %v", err)
				}
				fechado := aberto
				if err := fechado.Devolver(noPrazo); err != nil {
					t.Fatalf("Devolver: %v", err)
				}
				if _, err := f.repo.RegistrarDevolucao(t.Context(), fechado); err != nil {
					t.Fatalf("RegistrarDevolucao: %v", err)
				}
				return renovado
			},
			wantErr: domain.ErrEmprestimoJaDevolvido, wantDate: "2026-03-24", wantRenovacoes: 0,
		},
		{
			name: "copia velha: contador ja avancou",
			preparar: func(t *testing.T, f fixture, aberto domain.Emprestimo) domain.Emprestimo {
				primeira := aberto
				if err := primeira.Renovar(noPrazo); err != nil {
					t.Fatalf("Renovar: %v", err)
				}
				if _, err := f.repo.RegistrarRenovacao(t.Context(), primeira); err != nil {
					t.Fatalf("primeira RegistrarRenovacao: %v", err)
				}
				// Outra cópia da mesma leitura (contador 0 -> 1) chega depois.
				velha := aberto
				if err := velha.Renovar(time.Date(2026, 3, 21, 10, 0, 0, 0, sp)); err != nil {
					t.Fatalf("Renovar: %v", err)
				}
				return velha
			},
			wantErr: ErrEmprestimoDivergente, wantDate: "2026-04-07", wantRenovacoes: 1,
		},
		{
			name: "contador a frente do gravado",
			preparar: func(t *testing.T, f fixture, aberto domain.Emprestimo) domain.Emprestimo {
				// Reidratado com contador 1 sem a primeira renovação gravada:
				// renovar leva a 2, mas o banco tem 0.
				e, err := domain.ReidratarEmprestimo(aberto.ID(), aberto.LivroID(), aberto.UsuarioID(), aberto.Inicio(), aberto.Vencimento(), time.Time{}, 1)
				if err != nil {
					t.Fatalf("ReidratarEmprestimo: %v", err)
				}
				if err := e.Renovar(noPrazo); err != nil {
					t.Fatalf("Renovar: %v", err)
				}
				return e
			},
			wantErr: ErrEmprestimoDivergente, wantDate: "2026-03-24", wantRenovacoes: 0,
		},
		{
			name: "inicio divergente",
			preparar: func(t *testing.T, f fixture, aberto domain.Emprestimo) domain.Emprestimo {
				e, err := domain.ReidratarEmprestimo(aberto.ID(), aberto.LivroID(), aberto.UsuarioID(), inicioReal.Add(time.Microsecond), aberto.Vencimento(), time.Time{}, 0)
				if err != nil {
					t.Fatalf("ReidratarEmprestimo: %v", err)
				}
				if err := e.Renovar(noPrazo); err != nil {
					t.Fatalf("Renovar: %v", err)
				}
				return e
			},
			wantErr: ErrEmprestimoDivergente, wantDate: "2026-03-24", wantRenovacoes: 0,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novoFixture(t)
			aberto := abrirParaRenovar(t, f)
			e := tt.preparar(t, f, aberto)

			got, err := f.repo.RegistrarRenovacao(t.Context(), e)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("RegistrarRenovacao erro = %v, want %v", err, tt.wantErr)
			}
			if got.ID() != 0 || got.Renovacoes() != 0 || !got.Inicio().IsZero() {
				t.Errorf("em erro devolveu %+v, want Emprestimo{}", got)
			}
			venc, renov := f.linhaGravada(t, aberto.ID())
			if venc != tt.wantDate || renov != tt.wantRenovacoes {
				t.Errorf("gravado (%s, %d), want (%s, %d)", venc, renov, tt.wantDate, tt.wantRenovacoes)
			}
		})
	}
}

// corrida executa a do segundo agente em tx2 enquanto tx1 (já com o primeiro
// agente feito) segura a linha; depois do commit de tx1, devolve o resultado de
// tx2.
func corrida(t *testing.T, f fixture, primeiro, segundo func(ctx context.Context, r *Repositorio) (domain.Emprestimo, error)) resultado {
	t.Helper()
	ctx := t.Context()
	c1 := f.banco.conectar(t)
	c2 := f.banco.conectar(t)
	tx1, err := c1.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin tx1: %v", err)
	}
	defer tx1.Rollback(context.Background())
	tx2, err := c2.Begin(ctx)
	if err != nil {
		t.Fatalf("Begin tx2: %v", err)
	}
	defer tx2.Rollback(context.Background())

	if _, err := primeiro(ctx, NovoRepositorio(tx1)); err != nil {
		t.Fatalf("tx1: %v", err)
	}

	ctxG, cancelG := context.WithCancel(ctx)
	ch := make(chan resultado, 1)
	fim := make(chan struct{})
	go func() {
		defer close(fim)
		e, err := segundo(ctxG, NovoRepositorio(tx2))
		ch <- resultado{e, err}
	}()
	defer func() {
		cancelG()
		<-fim
	}()

	esperarBloqueio(t, f.conn, c2.PgConn().PID())
	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("Commit tx1: %v", err)
	}

	var r resultado
	select {
	case r = <-ch:
	case <-time.After(15 * time.Second):
		t.Fatal("tx2 não terminou depois do commit de tx1")
	}
	if r.err == nil {
		if err := tx2.Commit(ctx); err != nil {
			t.Fatalf("Commit tx2: %v", err)
		}
	}
	return r
}

// Duas renovações concorrentes da mesma leitura: só uma grava; a outra cai no
// CAS do contador e recebe ErrEmprestimoDivergente.
func TestRegistrarRenovacaoConcorrente(t *testing.T) {
	f := novoFixture(t)
	sp := saoPaulo(t)
	aberto := abrirParaRenovar(t, f)

	r1, r2 := aberto, aberto
	if err := r1.Renovar(time.Date(2026, 3, 20, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Renovar r1: %v", err)
	}
	if err := r2.Renovar(time.Date(2026, 3, 21, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Renovar r2: %v", err)
	}

	r := corrida(t, f,
		func(ctx context.Context, repo *Repositorio) (domain.Emprestimo, error) {
			return repo.RegistrarRenovacao(ctx, r1)
		},
		func(ctx context.Context, repo *Repositorio) (domain.Emprestimo, error) {
			return repo.RegistrarRenovacao(ctx, r2)
		})
	if !errors.Is(r.err, ErrEmprestimoDivergente) {
		t.Errorf("tx2 RegistrarRenovacao erro = %v, want %v", r.err, ErrEmprestimoDivergente)
	}
	venc, renov := f.linhaGravada(t, aberto.ID())
	if venc != "2026-04-07" || renov != 1 {
		t.Errorf("gravado (%s, %d), want (2026-04-07, 1)", venc, renov)
	}
}

// Devolução grava antes da renovação concorrente: a renovação recebe
// ErrEmprestimoJaDevolvido e não altera vencimento nem contador.
func TestRegistrarRenovacaoConcorrenteComDevolucao(t *testing.T) {
	f := novoFixture(t)
	sp := saoPaulo(t)
	aberto := abrirParaRenovar(t, f)

	dev, ren := aberto, aberto
	if err := dev.Devolver(time.Date(2026, 3, 20, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Devolver: %v", err)
	}
	if err := ren.Renovar(time.Date(2026, 3, 20, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Renovar: %v", err)
	}

	r := corrida(t, f,
		func(ctx context.Context, repo *Repositorio) (domain.Emprestimo, error) {
			return repo.RegistrarDevolucao(ctx, dev)
		},
		func(ctx context.Context, repo *Repositorio) (domain.Emprestimo, error) {
			return repo.RegistrarRenovacao(ctx, ren)
		})
	if !errors.Is(r.err, domain.ErrEmprestimoJaDevolvido) {
		t.Errorf("tx2 RegistrarRenovacao erro = %v, want %v", r.err, domain.ErrEmprestimoJaDevolvido)
	}
	venc, renov := f.linhaGravada(t, aberto.ID())
	if venc != "2026-03-24" || renov != 0 {
		t.Errorf("gravado (%s, %d), want (2026-03-24, 0)", venc, renov)
	}
}

// Renovação grava antes da devolução concorrente: a devolução grava sobre o
// vencimento novo e devolve o contador.
func TestRegistrarDevolucaoConcorrenteComRenovacao(t *testing.T) {
	f := novoFixture(t)
	sp := saoPaulo(t)
	aberto := abrirParaRenovar(t, f)

	dev, ren := aberto, aberto
	if err := dev.Devolver(time.Date(2026, 3, 20, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Devolver: %v", err)
	}
	if err := ren.Renovar(time.Date(2026, 3, 20, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Renovar: %v", err)
	}

	r := corrida(t, f,
		func(ctx context.Context, repo *Repositorio) (domain.Emprestimo, error) {
			return repo.RegistrarRenovacao(ctx, ren)
		},
		func(ctx context.Context, repo *Repositorio) (domain.Emprestimo, error) {
			return repo.RegistrarDevolucao(ctx, dev)
		})
	if r.err != nil {
		t.Fatalf("tx2 RegistrarDevolucao erro inesperado: %v", r.err)
	}
	if r.e.Renovacoes() != 1 {
		t.Errorf("RegistrarDevolucao Renovacoes() = %d, want 1", r.e.Renovacoes())
	}
	checarVencimentoSP(t, r.e.Vencimento(), 2026, 4, 7)
	if r.e.Aberto() {
		t.Error("RegistrarDevolucao devolveu aberto, want devolvido")
	}
}
