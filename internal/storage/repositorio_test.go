package storage

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
)

// trunc reproduz a precisão de microssegundos do timestamptz.
func trunc(t time.Time) time.Time { return t.Truncate(time.Microsecond) }

// checarEmprestimo compara got com want; o ID de want só é comparado se != 0.
func checarEmprestimo(t *testing.T, got, want domain.Emprestimo) {
	t.Helper()
	if want.ID() != 0 && got.ID() != want.ID() {
		t.Errorf("ID() = %d, want %d", got.ID(), want.ID())
	}
	if got.ID() <= 0 {
		t.Errorf("ID() = %d, want > 0", got.ID())
	}
	if got.LivroID() != want.LivroID() || got.UsuarioID() != want.UsuarioID() {
		t.Errorf("LivroID/UsuarioID = %d/%d, want %d/%d", got.LivroID(), got.UsuarioID(), want.LivroID(), want.UsuarioID())
	}
	if !got.Inicio().Equal(trunc(want.Inicio())) {
		t.Errorf("Inicio() = %v, want %v", got.Inicio(), trunc(want.Inicio()))
	}
	if !got.Vencimento().Equal(want.Vencimento()) {
		t.Errorf("Vencimento() = %v, want %v", got.Vencimento(), want.Vencimento())
	}
	if !got.Devolucao().Equal(trunc(want.Devolucao())) {
		t.Errorf("Devolucao() = %v, want %v", got.Devolucao(), trunc(want.Devolucao()))
	}
	if got.Aberto() != want.Aberto() {
		t.Errorf("Aberto() = %v, want %v", got.Aberto(), want.Aberto())
	}
}

// checarVencimentoSP confere 12:00:00 em SP na data civil a/m/d.
func checarVencimentoSP(t *testing.T, v time.Time, a int, m time.Month, d int) {
	t.Helper()
	sp := v.In(saoPaulo(t))
	ga, gm, gd := sp.Date()
	if ga != a || gm != m || gd != d || sp.Hour() != 12 || sp.Minute() != 0 || sp.Second() != 0 || sp.Nanosecond() != 0 {
		t.Errorf("Vencimento() = %v, want %04d-%02d-%02d 12:00:00 SP", sp, a, m, d)
	}
}

type fixture struct {
	repo    *Repositorio
	conn    *pgx.Conn
	banco   *bancoTeste
	livro   domain.Livro
	usuario domain.Usuario
}

func novoFixture(t *testing.T) fixture {
	t.Helper()
	b, c := novoBancoMigrado(t)
	r := NovoRepositorio(c)
	l, err := domain.NovoLivro("Dom Casmurro")
	if err != nil {
		t.Fatalf("NovoLivro: %v", err)
	}
	l, err = r.InserirLivro(t.Context(), l)
	if err != nil {
		t.Fatalf("InserirLivro: %v", err)
	}
	u, err := domain.NovoUsuario("Ana")
	if err != nil {
		t.Fatalf("NovoUsuario: %v", err)
	}
	u, err = r.InserirUsuario(t.Context(), u)
	if err != nil {
		t.Fatalf("InserirUsuario: %v", err)
	}
	return fixture{repo: r, conn: c, banco: b, livro: l, usuario: u}
}

func (f fixture) novoLivro(t *testing.T, titulo string) domain.Livro {
	t.Helper()
	l, err := domain.NovoLivro(titulo)
	if err != nil {
		t.Fatalf("NovoLivro: %v", err)
	}
	l, err = f.repo.InserirLivro(t.Context(), l)
	if err != nil {
		t.Fatalf("InserirLivro: %v", err)
	}
	return l
}

func (f fixture) novoUsuario(t *testing.T, nome string) domain.Usuario {
	t.Helper()
	u, err := domain.NovoUsuario(nome)
	if err != nil {
		t.Fatalf("NovoUsuario: %v", err)
	}
	u, err = f.repo.InserirUsuario(t.Context(), u)
	if err != nil {
		t.Fatalf("InserirUsuario: %v", err)
	}
	return u
}

func (f fixture) contar(t *testing.T, tabela string) int {
	t.Helper()
	var n int
	if err := f.conn.QueryRow(t.Context(), "SELECT count(*) FROM "+pgx.Identifier{tabela}.Sanitize()).Scan(&n); err != nil {
		t.Fatalf("contar %s: %v", tabela, err)
	}
	return n
}

func TestLivroIdaEVolta(t *testing.T) {
	f := novoFixture(t)
	ctx := t.Context()

	tests := []struct {
		name       string
		titulo     string
		wantTitulo string
	}{
		{name: "titulo simples", titulo: "O Alienista", wantTitulo: "O Alienista"},
		{name: "titulo aparado (revalida com NovoLivro)", titulo: "  Memórias Póstumas \t", wantTitulo: "Memórias Póstumas"},
		{name: "titulo com aspas e unicode", titulo: `Quincas "Borba" — 1891`, wantTitulo: `Quincas "Borba" — 1891`},
	}
	ids := map[int64]bool{f.livro.ID: true}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			// Livro montado à mão (sem NovoLivro): o repositório revalida.
			got, err := f.repo.InserirLivro(ctx, domain.Livro{Titulo: tt.titulo})
			if err != nil {
				t.Fatalf("InserirLivro erro inesperado: %v", err)
			}
			if got.ID <= 0 || ids[got.ID] {
				t.Errorf("ID = %d, want > 0 e novo", got.ID)
			}
			ids[got.ID] = true
			if got.Titulo != tt.wantTitulo {
				t.Errorf("Titulo = %q, want %q", got.Titulo, tt.wantTitulo)
			}
			lido, err := f.repo.BuscarLivro(ctx, got.ID)
			if err != nil {
				t.Fatalf("BuscarLivro erro inesperado: %v", err)
			}
			if lido != got {
				t.Errorf("BuscarLivro = %+v, want %+v", lido, got)
			}
		})
	}

	for _, id := range []int64{f.livro.ID + 1000, 0, -1} {
		if _, err := f.repo.BuscarLivro(ctx, id); !errors.Is(err, ErrLivroNaoEncontrado) {
			t.Errorf("BuscarLivro(%d) erro = %v, want %v", id, err, ErrLivroNaoEncontrado)
		}
	}

	antes := f.contar(t, "livro")
	if _, err := f.repo.InserirLivro(ctx, f.livro); !errors.Is(err, ErrJaPersistido) {
		t.Errorf("InserirLivro(persistido) erro = %v, want %v", err, ErrJaPersistido)
	}
	if _, err := f.repo.InserirLivro(ctx, domain.Livro{Titulo: "   "}); !errors.Is(err, domain.ErrTituloVazio) {
		t.Errorf("InserirLivro(vazio) erro = %v, want %v", err, domain.ErrTituloVazio)
	}
	if depois := f.contar(t, "livro"); depois != antes {
		t.Errorf("linhas em livro %d -> %d depois de inserções recusadas", antes, depois)
	}
}

func TestUsuarioIdaEVolta(t *testing.T) {
	f := novoFixture(t)
	ctx := t.Context()

	tests := []struct {
		name     string
		nome     string
		wantNome string
	}{
		{name: "nome simples", nome: "Bento Santiago", wantNome: "Bento Santiago"},
		{name: "nome aparado (revalida com NovoUsuario)", nome: "\t Capitu  ", wantNome: "Capitu"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := f.repo.InserirUsuario(ctx, domain.Usuario{Nome: tt.nome})
			if err != nil {
				t.Fatalf("InserirUsuario erro inesperado: %v", err)
			}
			if got.ID <= 0 || got.ID == f.usuario.ID {
				t.Errorf("ID = %d, want > 0 e novo", got.ID)
			}
			if got.Nome != tt.wantNome {
				t.Errorf("Nome = %q, want %q", got.Nome, tt.wantNome)
			}
			lido, err := f.repo.BuscarUsuario(ctx, got.ID)
			if err != nil {
				t.Fatalf("BuscarUsuario erro inesperado: %v", err)
			}
			if lido != got {
				t.Errorf("BuscarUsuario = %+v, want %+v", lido, got)
			}
		})
	}

	for _, id := range []int64{f.usuario.ID + 1000, 0, -1} {
		if _, err := f.repo.BuscarUsuario(ctx, id); !errors.Is(err, ErrUsuarioNaoEncontrado) {
			t.Errorf("BuscarUsuario(%d) erro = %v, want %v", id, err, ErrUsuarioNaoEncontrado)
		}
	}

	antes := f.contar(t, "usuario")
	if _, err := f.repo.InserirUsuario(ctx, f.usuario); !errors.Is(err, ErrJaPersistido) {
		t.Errorf("InserirUsuario(persistido) erro = %v, want %v", err, ErrJaPersistido)
	}
	if _, err := f.repo.InserirUsuario(ctx, domain.Usuario{Nome: ""}); !errors.Is(err, domain.ErrNomeVazio) {
		t.Errorf("InserirUsuario(vazio) erro = %v, want %v", err, domain.ErrNomeVazio)
	}
	if depois := f.contar(t, "usuario"); depois != antes {
		t.Errorf("linhas em usuario %d -> %d depois de inserções recusadas", antes, depois)
	}
}

func TestEmprestimoIdaEVolta(t *testing.T) {
	sp := saoPaulo(t)

	tests := []struct {
		name     string
		inicio   time.Time
		timezone string // TimeZone da sessão; vazio = padrão do servidor
		wantA    int
		wantM    time.Month
		wantD    int
		wantDATE string
	}{
		{name: "inicio comum com nanossegundos", inicio: time.Date(2026, 3, 10, 10, 15, 30, 123456789, sp), wantA: 2026, wantM: 3, wantD: 24, wantDATE: "2026-03-24"},
		{name: "inicio 23:30 SP", inicio: time.Date(2026, 3, 10, 23, 30, 0, 0, sp), wantA: 2026, wantM: 3, wantD: 24, wantDATE: "2026-03-24"},
		{name: "vencimento 04/11/2018 (sem 00:00 em SP)", inicio: time.Date(2018, 10, 21, 9, 0, 0, 0, sp), wantA: 2018, wantM: 11, wantD: 4, wantDATE: "2018-11-04"},
		{name: "vencimento 17/02/2019 (fim do horario de verao)", inicio: time.Date(2019, 2, 3, 9, 0, 0, 0, sp), wantA: 2019, wantM: 2, wantD: 17, wantDATE: "2019-02-17"},
		{name: "04/11/2018 com sessao em Pacific/Kiritimati", inicio: time.Date(2018, 10, 21, 9, 0, 0, 0, sp), timezone: "Pacific/Kiritimati", wantA: 2018, wantM: 11, wantD: 4, wantDATE: "2018-11-04"},
		{name: "04/11/2018 com sessao em America/Sao_Paulo", inicio: time.Date(2018, 10, 21, 9, 0, 0, 0, sp), timezone: "America/Sao_Paulo", wantA: 2018, wantM: 11, wantD: 4, wantDATE: "2018-11-04"},
		{name: "inicio em UTC+9 com sessao em Pacific/Pago_Pago", inicio: time.Date(2026, 3, 11, 9, 0, 0, 0, time.FixedZone("UTC+9", 9*60*60)), timezone: "Pacific/Pago_Pago", wantA: 2026, wantM: 3, wantD: 24, wantDATE: "2026-03-24"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			b, c := novoBancoMigrado(t)
			if tt.timezone != "" {
				c = b.conectar(t, "timezone", tt.timezone)
			}
			ctx := t.Context()
			r := NovoRepositorio(c)
			l, err := r.InserirLivro(ctx, domain.Livro{Titulo: "Dom Casmurro"})
			if err != nil {
				t.Fatalf("InserirLivro: %v", err)
			}
			u, err := r.InserirUsuario(ctx, domain.Usuario{Nome: "Ana"})
			if err != nil {
				t.Fatalf("InserirUsuario: %v", err)
			}

			novo, err := domain.NovoEmprestimo(l.ID, u.ID, tt.inicio)
			if err != nil {
				t.Fatalf("NovoEmprestimo: %v", err)
			}
			aberto, err := r.AbrirEmprestimo(ctx, novo)
			if err != nil {
				t.Fatalf("AbrirEmprestimo erro inesperado: %v", err)
			}
			checarEmprestimo(t, aberto, novo)
			checarVencimentoSP(t, aberto.Vencimento(), tt.wantA, tt.wantM, tt.wantD)

			var dataGravada string
			if err := c.QueryRow(ctx, "SELECT vencimento::text FROM emprestimo WHERE id = $1", aberto.ID()).Scan(&dataGravada); err != nil {
				t.Fatalf("ler vencimento gravado: %v", err)
			}
			if dataGravada != tt.wantDATE {
				t.Errorf("vencimento gravado = %s, want %s", dataGravada, tt.wantDATE)
			}

			lido, err := r.BuscarEmprestimoAbertoPorLivro(ctx, l.ID)
			if err != nil {
				t.Fatalf("BuscarEmprestimoAbertoPorLivro erro inesperado: %v", err)
			}
			checarEmprestimo(t, lido, aberto)
			checarVencimentoSP(t, lido.Vencimento(), tt.wantA, tt.wantM, tt.wantD)

			// Devolução 20 dias depois, com nanossegundos.
			em := tt.inicio.Add(20*24*time.Hour + 987654321)
			if err := lido.Devolver(em); err != nil {
				t.Fatalf("Devolver: %v", err)
			}
			fechado, err := r.RegistrarDevolucao(ctx, lido)
			if err != nil {
				t.Fatalf("RegistrarDevolucao erro inesperado: %v", err)
			}
			checarEmprestimo(t, fechado, lido)
			if fechado.Aberto() {
				t.Error("depois de RegistrarDevolucao Aberto() = true")
			}

			if _, err := r.BuscarEmprestimoAbertoPorLivro(ctx, l.ID); !errors.Is(err, ErrEmprestimoNaoEncontrado) {
				t.Errorf("BuscarEmprestimoAbertoPorLivro depois da devolução erro = %v, want %v", err, ErrEmprestimoNaoEncontrado)
			}
		})
	}
}

func TestBuscarEmprestimoAbertoPorLivroInexistente(t *testing.T) {
	f := novoFixture(t)
	for _, id := range []int64{f.livro.ID, f.livro.ID + 1000, 0} {
		if _, err := f.repo.BuscarEmprestimoAbertoPorLivro(t.Context(), id); !errors.Is(err, ErrEmprestimoNaoEncontrado) {
			t.Errorf("BuscarEmprestimoAbertoPorLivro(%d) erro = %v, want %v", id, err, ErrEmprestimoNaoEncontrado)
		}
	}
}

func TestAbrirEmprestimoRecusas(t *testing.T) {
	f := novoFixture(t)
	ctx := t.Context()
	sp := saoPaulo(t)
	inicio := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)

	ja, err := domain.ReidratarEmprestimo(7, f.livro.ID, f.usuario.ID, inicio, time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC), time.Time{})
	if err != nil {
		t.Fatalf("ReidratarEmprestimo: %v", err)
	}
	devolvido, err := domain.NovoEmprestimo(f.livro.ID, f.usuario.ID, inicio)
	if err != nil {
		t.Fatalf("NovoEmprestimo: %v", err)
	}
	if err := devolvido.Devolver(inicio.Add(time.Hour)); err != nil {
		t.Fatalf("Devolver: %v", err)
	}
	semLivro, err := domain.NovoEmprestimo(f.livro.ID+1000, f.usuario.ID, inicio)
	if err != nil {
		t.Fatalf("NovoEmprestimo: %v", err)
	}
	semUsuario, err := domain.NovoEmprestimo(f.livro.ID, f.usuario.ID+1000, inicio)
	if err != nil {
		t.Fatalf("NovoEmprestimo: %v", err)
	}

	tests := []struct {
		name    string
		e       domain.Emprestimo
		wantErr error
	}{
		{name: "zero-value", e: domain.Emprestimo{}, wantErr: domain.ErrEmprestimoInvalido},
		{name: "ja persistido", e: ja, wantErr: domain.ErrEmprestimoInvalido},
		{name: "devolvido", e: devolvido, wantErr: domain.ErrEmprestimoInvalido},
		{name: "livro inexistente", e: semLivro, wantErr: ErrLivroNaoEncontrado},
		{name: "usuario inexistente", e: semUsuario, wantErr: ErrUsuarioNaoEncontrado},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := f.repo.AbrirEmprestimo(ctx, tt.e)
			if !errors.Is(err, tt.wantErr) {
				t.Errorf("AbrirEmprestimo erro = %v, want %v", err, tt.wantErr)
			}
		})
	}
	if n := f.contar(t, "emprestimo"); n != 0 {
		t.Errorf("%d linha(s) em emprestimo depois só de recusas, want 0", n)
	}
}

func TestR4Sequencial(t *testing.T) {
	f := novoFixture(t)
	ctx := t.Context()
	sp := saoPaulo(t)
	outro := f.novoUsuario(t, "Bento")
	outroLivro := f.novoLivro(t, "Helena")

	e1, _ := domain.NovoEmprestimo(f.livro.ID, f.usuario.ID, time.Date(2026, 3, 10, 10, 0, 0, 0, sp))
	aberto, err := f.repo.AbrirEmprestimo(ctx, e1)
	if err != nil {
		t.Fatalf("primeiro AbrirEmprestimo: %v", err)
	}

	// Mesmo livro, outro usuário: recusado.
	e2, _ := domain.NovoEmprestimo(f.livro.ID, outro.ID, time.Date(2026, 3, 11, 10, 0, 0, 0, sp))
	if _, err := f.repo.AbrirEmprestimo(ctx, e2); !errors.Is(err, ErrLivroJaEmprestado) {
		t.Fatalf("segundo AbrirEmprestimo do mesmo livro erro = %v, want %v", err, ErrLivroJaEmprestado)
	}

	// Outro livro, mesmo usuário: permitido.
	e3, _ := domain.NovoEmprestimo(outroLivro.ID, f.usuario.ID, time.Date(2026, 3, 11, 10, 0, 0, 0, sp))
	if _, err := f.repo.AbrirEmprestimo(ctx, e3); err != nil {
		t.Fatalf("AbrirEmprestimo de outro livro: %v", err)
	}

	// Depois da devolução, o livro volta a poder ser emprestado.
	if err := aberto.Devolver(time.Date(2026, 3, 15, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Devolver: %v", err)
	}
	if _, err := f.repo.RegistrarDevolucao(ctx, aberto); err != nil {
		t.Fatalf("RegistrarDevolucao: %v", err)
	}
	reaberto, err := f.repo.AbrirEmprestimo(ctx, e2)
	if err != nil {
		t.Fatalf("AbrirEmprestimo depois da devolução: %v", err)
	}
	if reaberto.ID() == aberto.ID() {
		t.Errorf("reabertura reaproveitou o ID %d", aberto.ID())
	}
	lido, err := f.repo.BuscarEmprestimoAbertoPorLivro(ctx, f.livro.ID)
	if err != nil {
		t.Fatalf("BuscarEmprestimoAbertoPorLivro: %v", err)
	}
	if lido.ID() != reaberto.ID() || lido.UsuarioID() != outro.ID {
		t.Errorf("aberto do livro = ID %d usuário %d, want ID %d usuário %d", lido.ID(), lido.UsuarioID(), reaberto.ID(), outro.ID)
	}
}

type resultado struct {
	e   domain.Emprestimo
	err error
}

// R4 sob concorrência: tx1 insere sem commit, tx2 bloqueia no índice único,
// tx1 commita e tx2 recebe ErrLivroJaEmprestado.
func TestR4Concorrente(t *testing.T) {
	f := novoFixture(t)
	ctx := t.Context()
	sp := saoPaulo(t)
	outro := f.novoUsuario(t, "Bento")

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

	e1, _ := domain.NovoEmprestimo(f.livro.ID, f.usuario.ID, time.Date(2026, 3, 10, 10, 0, 0, 0, sp))
	e2, _ := domain.NovoEmprestimo(f.livro.ID, outro.ID, time.Date(2026, 3, 10, 10, 0, 1, 0, sp))

	if _, err := NovoRepositorio(tx1).AbrirEmprestimo(ctx, e1); err != nil {
		t.Fatalf("tx1 AbrirEmprestimo: %v", err)
	}

	// A goroutine usa tx2; o defer abaixo (roda antes dos Rollback, LIFO)
	// cancela o contexto dela e espera seu fim se o teste parar antes.
	ctxG, cancelG := context.WithCancel(ctx)
	ch := make(chan resultado, 1)
	fim := make(chan struct{})
	go func() {
		defer close(fim)
		e, err := NovoRepositorio(tx2).AbrirEmprestimo(ctxG, e2)
		ch <- resultado{e, err}
	}()
	defer func() {
		cancelG()
		<-fim
	}()

	esperarBloqueio(t, f.conn, c2.PgConn().PID())
	select {
	case r := <-ch:
		t.Fatalf("tx2 terminou antes do commit de tx1: %+v", r)
	default:
	}

	if err := tx1.Commit(ctx); err != nil {
		t.Fatalf("Commit tx1: %v", err)
	}

	select {
	case r := <-ch:
		if !errors.Is(r.err, ErrLivroJaEmprestado) {
			t.Errorf("tx2 AbrirEmprestimo erro = %v, want %v", r.err, ErrLivroJaEmprestado)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("tx2 não terminou depois do commit de tx1")
	}
	_ = tx2.Rollback(ctx)

	var n int
	if err := f.conn.QueryRow(ctx, "SELECT count(*) FROM emprestimo WHERE livro_id = $1 AND devolucao IS NULL", f.livro.ID).Scan(&n); err != nil {
		t.Fatalf("contar abertos: %v", err)
	}
	if n != 1 {
		t.Errorf("%d empréstimos abertos do livro, want 1", n)
	}
}

func TestRegistrarDevolucao(t *testing.T) {
	f := novoFixture(t)
	ctx := t.Context()
	sp := saoPaulo(t)

	e, _ := domain.NovoEmprestimo(f.livro.ID, f.usuario.ID, time.Date(2026, 3, 10, 10, 0, 0, 0, sp))
	aberto, err := f.repo.AbrirEmprestimo(ctx, e)
	if err != nil {
		t.Fatalf("AbrirEmprestimo: %v", err)
	}

	primeira := aberto
	if err := primeira.Devolver(time.Date(2026, 3, 15, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Devolver: %v", err)
	}
	if _, err := f.repo.RegistrarDevolucao(ctx, primeira); err != nil {
		t.Fatalf("RegistrarDevolucao: %v", err)
	}

	// Cópia antiga (aberta em memória) devolvida de novo com outra data.
	segunda := aberto
	if err := segunda.Devolver(time.Date(2026, 3, 18, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Devolver: %v", err)
	}
	if _, err := f.repo.RegistrarDevolucao(ctx, segunda); !errors.Is(err, domain.ErrEmprestimoJaDevolvido) {
		t.Errorf("segunda RegistrarDevolucao erro = %v, want %v", err, domain.ErrEmprestimoJaDevolvido)
	}
	var gravada time.Time
	if err := f.conn.QueryRow(ctx, "SELECT devolucao FROM emprestimo WHERE id = $1", aberto.ID()).Scan(&gravada); err != nil {
		t.Fatalf("ler devolução: %v", err)
	}
	if !gravada.Equal(primeira.Devolucao()) {
		t.Errorf("devolução gravada = %v, want %v (primeira intacta)", gravada, primeira.Devolucao())
	}

	inexistente, err := domain.ReidratarEmprestimo(aberto.ID()+1000, f.livro.ID, f.usuario.ID, aberto.Inicio(), aberto.Vencimento(), time.Date(2026, 3, 15, 10, 0, 0, 0, sp))
	if err != nil {
		t.Fatalf("ReidratarEmprestimo: %v", err)
	}
	if _, err := f.repo.RegistrarDevolucao(ctx, inexistente); !errors.Is(err, ErrEmprestimoNaoEncontrado) {
		t.Errorf("RegistrarDevolucao(inexistente) erro = %v, want %v", err, ErrEmprestimoNaoEncontrado)
	}
}

// Empréstimo reidratado com ID real e início divergente do gravado: o UPDATE
// fica preso ao início lido, nada é gravado e o livro continua emprestado.
func TestRegistrarDevolucaoInicioDivergente(t *testing.T) {
	sp := saoPaulo(t)
	inicioReal := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)

	tests := []struct {
		name      string
		inicio    time.Time // início reidratado
		devolucao time.Time
	}{
		// Devolver checa contra o início reidratado: aceita uma devolução
		// anterior ao início real.
		{name: "inicio anterior, devolucao antes do inicio real", inicio: time.Date(2026, 3, 1, 10, 0, 0, 0, sp), devolucao: time.Date(2026, 3, 5, 10, 0, 0, 0, sp)},
		{name: "inicio posterior", inicio: time.Date(2026, 3, 12, 10, 0, 0, 0, sp), devolucao: time.Date(2026, 3, 15, 10, 0, 0, 0, sp)},
		{name: "inicio 1us depois", inicio: inicioReal.Add(time.Microsecond), devolucao: time.Date(2026, 3, 15, 10, 0, 0, 0, sp)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := novoFixture(t)
			ctx := t.Context()
			e, _ := domain.NovoEmprestimo(f.livro.ID, f.usuario.ID, inicioReal)
			aberto, err := f.repo.AbrirEmprestimo(ctx, e)
			if err != nil {
				t.Fatalf("AbrirEmprestimo: %v", err)
			}

			forjado, err := domain.ReidratarEmprestimo(aberto.ID(), aberto.LivroID(), aberto.UsuarioID(), tt.inicio, aberto.Vencimento(), time.Time{})
			if err != nil {
				t.Fatalf("ReidratarEmprestimo: %v", err)
			}
			if err := forjado.Devolver(tt.devolucao); err != nil {
				t.Fatalf("Devolver: %v", err)
			}
			_, err = f.repo.RegistrarDevolucao(ctx, forjado)
			if !errors.Is(err, ErrEmprestimoDivergente) {
				t.Errorf("RegistrarDevolucao erro = %v, want %v", err, ErrEmprestimoDivergente)
			}

			var abertos int
			if err := f.conn.QueryRow(ctx,
				"SELECT count(*) FROM emprestimo WHERE id = $1 AND devolucao IS NULL", aberto.ID()).Scan(&abertos); err != nil {
				t.Fatalf("ler linha: %v", err)
			}
			if abertos != 1 {
				t.Errorf("linha %d com devolucao IS NULL: %d, want 1", aberto.ID(), abertos)
			}
			lido, err := f.repo.BuscarEmprestimoAbertoPorLivro(ctx, f.livro.ID)
			if err != nil {
				t.Fatalf("BuscarEmprestimoAbertoPorLivro erro = %v, want livro ainda emprestado", err)
			}
			checarEmprestimo(t, lido, aberto)

			// R4 continua valendo para o livro.
			outro, _ := domain.NovoEmprestimo(f.livro.ID, f.usuario.ID, time.Date(2026, 3, 16, 10, 0, 0, 0, sp))
			if _, err := f.repo.AbrirEmprestimo(ctx, outro); !errors.Is(err, ErrLivroJaEmprestado) {
				t.Errorf("AbrirEmprestimo do livro emprestado erro = %v, want %v", err, ErrLivroJaEmprestado)
			}

			// O próprio empréstimo lido do banco ainda devolve normalmente.
			if err := lido.Devolver(tt.devolucao.Add(10 * 24 * time.Hour)); err != nil {
				t.Fatalf("Devolver lido: %v", err)
			}
			if _, err := f.repo.RegistrarDevolucao(ctx, lido); err != nil {
				t.Errorf("RegistrarDevolucao do lido erro inesperado: %v", err)
			}
		})
	}
}

// Devolução concorrente do mesmo empréstimo: uma vence, a outra recebe
// ErrEmprestimoJaDevolvido.
func TestRegistrarDevolucaoConcorrente(t *testing.T) {
	f := novoFixture(t)
	ctx := t.Context()
	sp := saoPaulo(t)

	e, _ := domain.NovoEmprestimo(f.livro.ID, f.usuario.ID, time.Date(2026, 3, 10, 10, 0, 0, 0, sp))
	aberto, err := f.repo.AbrirEmprestimo(ctx, e)
	if err != nil {
		t.Fatalf("AbrirEmprestimo: %v", err)
	}
	d1, d2 := aberto, aberto
	if err := d1.Devolver(time.Date(2026, 3, 15, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Devolver d1: %v", err)
	}
	if err := d2.Devolver(time.Date(2026, 3, 16, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Devolver d2: %v", err)
	}

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

	if _, err := NovoRepositorio(tx1).RegistrarDevolucao(ctx, d1); err != nil {
		t.Fatalf("tx1 RegistrarDevolucao: %v", err)
	}

	// A goroutine usa tx2; o defer abaixo (roda antes dos Rollback, LIFO)
	// cancela o contexto dela e espera seu fim se o teste parar antes.
	ctxG, cancelG := context.WithCancel(ctx)
	ch := make(chan resultado, 1)
	fim := make(chan struct{})
	go func() {
		defer close(fim)
		e, err := NovoRepositorio(tx2).RegistrarDevolucao(ctxG, d2)
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

	select {
	case r := <-ch:
		if !errors.Is(r.err, domain.ErrEmprestimoJaDevolvido) {
			t.Errorf("tx2 RegistrarDevolucao erro = %v, want %v", r.err, domain.ErrEmprestimoJaDevolvido)
		}
	case <-time.After(15 * time.Second):
		t.Fatal("tx2 não terminou depois do commit de tx1")
	}
	_ = tx2.Rollback(ctx)

	var gravada time.Time
	if err := f.conn.QueryRow(ctx, "SELECT devolucao FROM emprestimo WHERE id = $1", aberto.ID()).Scan(&gravada); err != nil {
		t.Fatalf("ler devolução: %v", err)
	}
	if !gravada.Equal(d1.Devolucao()) {
		t.Errorf("devolução gravada = %v, want %v (a de tx1)", gravada, d1.Devolucao())
	}
}
