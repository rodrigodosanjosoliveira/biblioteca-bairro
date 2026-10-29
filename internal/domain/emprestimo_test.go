package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// checarVencimento confere que venc cai às 12:00:00 em São Paulo na data civil
// ano/mes/dia.
func checarVencimento(t *testing.T, venc time.Time, ano int, mes time.Month, dia int) {
	t.Helper()
	v := venc.In(saoPaulo(t))
	a, m, d := v.Date()
	if a != ano || m != mes || d != dia {
		t.Errorf("Vencimento() data civil SP = %04d-%02d-%02d, want %04d-%02d-%02d (%v)", a, m, d, ano, mes, dia, v)
	}
	if v.Hour() != 12 || v.Minute() != 0 || v.Second() != 0 || v.Nanosecond() != 0 {
		t.Errorf("Vencimento() hora SP = %s, want 12:00:00 (%v)", v.Format("15:04:05.999999999"), v)
	}
}

// checarEmprestimoZero confere que e se comporta como Emprestimo{}.
func checarEmprestimoZero(t *testing.T, e Emprestimo) {
	t.Helper()
	var zero Emprestimo
	if e.ID() != 0 || e.LivroID() != 0 || e.UsuarioID() != 0 {
		t.Errorf("em erro ID/LivroID/UsuarioID = %d/%d/%d, want 0/0/0", e.ID(), e.LivroID(), e.UsuarioID())
	}
	if !e.Inicio().IsZero() {
		t.Errorf("em erro Inicio() = %v, want zero", e.Inicio())
	}
	if !e.Devolucao().IsZero() {
		t.Errorf("em erro Devolucao() = %v, want zero", e.Devolucao())
	}
	if !e.Vencimento().Equal(zero.Vencimento()) {
		t.Errorf("em erro Vencimento() = %v, want %v (de Emprestimo{})", e.Vencimento(), zero.Vencimento())
	}
	if e.Aberto() != zero.Aberto() {
		t.Errorf("em erro Aberto() = %v, want %v (de Emprestimo{})", e.Aberto(), zero.Aberto())
	}
}

func TestPrazoEmprestimoDias(t *testing.T) {
	if PrazoEmprestimoDias != 14 {
		t.Errorf("PrazoEmprestimoDias = %d, want 14", PrazoEmprestimoDias)
	}
}

func TestErrosEmprestimoMensagens(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "livro id", err: ErrLivroIDInvalido, want: "emprestimo: id do livro inválido"},
		{name: "usuario id", err: ErrUsuarioIDInvalido, want: "emprestimo: id do usuário inválido"},
		{name: "inicio zero", err: ErrInicioZero, want: "emprestimo: data de início zero"},
		{name: "ja devolvido", err: ErrEmprestimoJaDevolvido, want: "emprestimo: já devolvido"},
		{name: "devolucao zero", err: ErrDevolucaoEmprestimoZero, want: "emprestimo: data de devolução zero"},
		{name: "devolucao antes do inicio", err: ErrDevolucaoAntesDoInicio, want: "emprestimo: devolução antes do início"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if tt.err == nil {
				t.Fatal("erro sentinela nil")
			}
			if got := tt.err.Error(); got != tt.want {
				t.Errorf("Error() = %q, want %q", got, tt.want)
			}
		})
	}
}

func TestNovoEmprestimoErros(t *testing.T) {
	valido := time.Date(2026, 3, 10, 10, 0, 0, 0, saoPaulo(t))

	tests := []struct {
		name      string
		livroID   int64
		usuarioID int64
		inicio    time.Time
		wantErr   error
	}{
		{name: "livro id zero", livroID: 0, usuarioID: 1, inicio: valido, wantErr: ErrLivroIDInvalido},
		{name: "livro id negativo", livroID: -1, usuarioID: 1, inicio: valido, wantErr: ErrLivroIDInvalido},
		{name: "usuario id zero", livroID: 1, usuarioID: 0, inicio: valido, wantErr: ErrUsuarioIDInvalido},
		{name: "usuario id negativo", livroID: 1, usuarioID: -7, inicio: valido, wantErr: ErrUsuarioIDInvalido},
		{name: "inicio zero", livroID: 1, usuarioID: 1, inicio: time.Time{}, wantErr: ErrInicioZero},
		{name: "ordem: tudo invalido valida livro primeiro", livroID: 0, usuarioID: 0, inicio: time.Time{}, wantErr: ErrLivroIDInvalido},
		{name: "ordem: livro e usuario invalidos", livroID: -1, usuarioID: -1, inicio: valido, wantErr: ErrLivroIDInvalido},
		{name: "ordem: livro e inicio invalidos", livroID: 0, usuarioID: 1, inicio: time.Time{}, wantErr: ErrLivroIDInvalido},
		{name: "ordem: usuario e inicio invalidos", livroID: 1, usuarioID: 0, inicio: time.Time{}, wantErr: ErrUsuarioIDInvalido},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := NovoEmprestimo(tt.livroID, tt.usuarioID, tt.inicio)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("NovoEmprestimo(%d, %d, %v) erro = %v, want %v", tt.livroID, tt.usuarioID, tt.inicio, err, tt.wantErr)
			}
			checarEmprestimoZero(t, got)
		})
	}
}

func TestNovoEmprestimoSucesso(t *testing.T) {
	inicio := time.Date(2026, 3, 10, 10, 15, 30, 0, time.FixedZone("UTC+9", 9*60*60))

	e, err := NovoEmprestimo(42, 7, inicio)
	if err != nil {
		t.Fatalf("NovoEmprestimo erro inesperado: %v", err)
	}
	if e.ID() != 0 {
		t.Errorf("ID() = %d, want 0", e.ID())
	}
	if e.LivroID() != 42 {
		t.Errorf("LivroID() = %d, want 42", e.LivroID())
	}
	if e.UsuarioID() != 7 {
		t.Errorf("UsuarioID() = %d, want 7", e.UsuarioID())
	}
	if !e.Inicio().Equal(inicio) {
		t.Errorf("Inicio() = %v, want %v", e.Inicio(), inicio)
	}
	if e.Inicio().Location() != inicio.Location() {
		t.Errorf("Inicio().Location() = %v, want %v (guardado como recebido)", e.Inicio().Location(), inicio.Location())
	}
	if !e.Devolucao().IsZero() {
		t.Errorf("Devolucao() = %v, want zero", e.Devolucao())
	}
	if !e.Aberto() {
		t.Error("Aberto() = false, want true")
	}
}

func TestNovoEmprestimoVencimento(t *testing.T) {
	sp := saoPaulo(t)

	tests := []struct {
		name   string
		inicio time.Time
		wantA  int
		wantM  time.Month
		wantD  int
	}{
		{name: "inicio comum", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), wantA: 2026, wantM: 3, wantD: 24},
		{name: "inicio 23:30 BRT (dia seguinte em UTC)", inicio: time.Date(2026, 3, 10, 23, 30, 0, 0, sp), wantA: 2026, wantM: 3, wantD: 24},
		{name: "mesmo instante 23:30 BRT expresso em UTC", inicio: time.Date(2026, 3, 11, 2, 30, 0, 0, time.UTC), wantA: 2026, wantM: 3, wantD: 24},
		{name: "inicio em UTC+9 (dia seguinte no fuso de entrada)", inicio: time.Date(2026, 3, 11, 9, 0, 0, 0, time.FixedZone("UTC+9", 9*60*60)), wantA: 2026, wantM: 3, wantD: 24}, // 10/03 21:00 SP
		{name: "inicio em UTC", inicio: time.Date(2026, 3, 10, 15, 0, 0, 0, time.UTC), wantA: 2026, wantM: 3, wantD: 24},
		{name: "inicio 00:00 SP", inicio: time.Date(2026, 3, 10, 0, 0, 0, 0, sp), wantA: 2026, wantM: 3, wantD: 24},
		{name: "virada de mes", inicio: time.Date(2026, 1, 25, 9, 0, 0, 0, sp), wantA: 2026, wantM: 2, wantD: 8},
		{name: "virada de ano", inicio: time.Date(2026, 12, 25, 9, 0, 0, 0, sp), wantA: 2027, wantM: 1, wantD: 8},
		{name: "fevereiro bissexto", inicio: time.Date(2024, 2, 20, 9, 0, 0, 0, sp), wantA: 2024, wantM: 3, wantD: 5},
		{name: "fevereiro comum", inicio: time.Date(2026, 2, 20, 9, 0, 0, 0, sp), wantA: 2026, wantM: 3, wantD: 6},
		{name: "vencimento em 04/11/2018 (dia sem 00:00 em SP)", inicio: time.Date(2018, 10, 21, 9, 0, 0, 0, sp), wantA: 2018, wantM: 11, wantD: 4},
		{name: "fim do horario de verao 2019", inicio: time.Date(2019, 2, 3, 9, 0, 0, 0, sp), wantA: 2019, wantM: 2, wantD: 17},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := NovoEmprestimo(1, 1, tt.inicio)
			if err != nil {
				t.Fatalf("NovoEmprestimo(1, 1, %v) erro inesperado: %v", tt.inicio, err)
			}
			checarVencimento(t, e.Vencimento(), tt.wantA, tt.wantM, tt.wantD)
		})
	}
}

// TestVencimentoCompativelComCalcularMulta garante que o vencimento produzido
// pelo empréstimo conversa com CalcularMulta: devolver até 23:59 do dia do
// vencimento em SP não gera multa; 00:30 do dia seguinte gera R$ 1,00.
func TestVencimentoCompativelComCalcularMulta(t *testing.T) {
	sp := saoPaulo(t)

	tests := []struct {
		name   string
		inicio time.Time
	}{
		{name: "inicio comum", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)},
		{name: "inicio 23:30 BRT", inicio: time.Date(2026, 3, 10, 23, 30, 0, 0, sp)},
		{name: "vencimento em 04/11/2018", inicio: time.Date(2018, 10, 21, 9, 0, 0, 0, sp)},
		{name: "vencimento no fim do horario de verao 2019", inicio: time.Date(2019, 2, 3, 9, 0, 0, 0, sp)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := NovoEmprestimo(1, 1, tt.inicio)
			if err != nil {
				t.Fatalf("NovoEmprestimo erro inesperado: %v", err)
			}
			venc := e.Vencimento()
			a, m, d := venc.In(sp).Date()

			casos := []struct {
				dev  time.Time
				want decimal.Decimal
			}{
				{dev: venc, want: decimal.Zero},
				{dev: time.Date(a, m, d, 23, 59, 0, 0, sp), want: decimal.Zero},
				{dev: time.Date(a, m, d+1, 0, 30, 0, 0, sp), want: decimal.NewFromInt(1)},
			}
			for _, c := range casos {
				got, err := CalcularMulta(venc, c.dev)
				if err != nil {
					t.Fatalf("CalcularMulta(%v, %v) erro inesperado: %v", venc, c.dev, err)
				}
				if !got.Equal(c.want) {
					t.Errorf("CalcularMulta(%v, %v) = %s, want %s", venc, c.dev, got, c.want)
				}
			}
		})
	}
}

func TestEmprestimoDevolver(t *testing.T) {
	sp := saoPaulo(t)

	tests := []struct {
		name    string
		inicio  time.Time
		em      time.Time
		wantErr error
	}{
		{name: "devolucao dias depois", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), em: time.Date(2026, 3, 20, 15, 0, 0, 0, sp)},
		{name: "devolucao no mesmo instante", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), em: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)},
		{name: "mesmo dia civil com hora anterior ao inicio", inicio: time.Date(2026, 3, 10, 15, 0, 0, 0, sp), em: time.Date(2026, 3, 10, 8, 0, 0, 0, sp)},
		{name: "devolucao em UTC no mesmo dia civil SP", inicio: time.Date(2026, 3, 9, 8, 0, 0, 0, sp), em: time.Date(2026, 3, 10, 2, 30, 0, 0, time.UTC)}, // 09/03 23:30 SP
		{name: "devolucao com atraso", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), em: time.Date(2026, 4, 30, 10, 0, 0, 0, sp)},
		{
			name:    "dia civil SP anterior ao inicio mesmo sendo depois em UTC",
			inicio:  time.Date(2026, 3, 10, 0, 30, 0, 0, sp),
			em:      time.Date(2026, 3, 10, 2, 30, 0, 0, time.UTC), // 09/03 23:30 SP
			wantErr: ErrDevolucaoAntesDoInicio,
		},
		{name: "dia anterior ao inicio", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), em: time.Date(2026, 3, 9, 23, 59, 0, 0, sp), wantErr: ErrDevolucaoAntesDoInicio},
		{name: "devolucao zero", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), em: time.Time{}, wantErr: ErrDevolucaoEmprestimoZero},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := NovoEmprestimo(1, 1, tt.inicio)
			if err != nil {
				t.Fatalf("NovoEmprestimo erro inesperado: %v", err)
			}
			venc := e.Vencimento()

			err = e.Devolver(tt.em)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("Devolver(%v) erro = %v, want %v", tt.em, err, tt.wantErr)
				}
				if !e.Aberto() {
					t.Error("Aberto() = false depois de erro, want true")
				}
				if !e.Devolucao().IsZero() {
					t.Errorf("Devolucao() = %v depois de erro, want zero", e.Devolucao())
				}
			} else {
				if err != nil {
					t.Fatalf("Devolver(%v) erro inesperado: %v", tt.em, err)
				}
				if e.Aberto() {
					t.Error("Aberto() = true depois de devolver, want false")
				}
				if !e.Devolucao().Equal(tt.em) {
					t.Errorf("Devolucao() = %v, want %v", e.Devolucao(), tt.em)
				}
				if e.Devolucao().Location() != tt.em.Location() {
					t.Errorf("Devolucao().Location() = %v, want %v (guardado como recebido)", e.Devolucao().Location(), tt.em.Location())
				}
			}
			// Os demais campos não mudam, com ou sem erro.
			if e.LivroID() != 1 || e.UsuarioID() != 1 || e.ID() != 0 {
				t.Errorf("IDs alterados por Devolver: ID=%d LivroID=%d UsuarioID=%d", e.ID(), e.LivroID(), e.UsuarioID())
			}
			if !e.Inicio().Equal(tt.inicio) {
				t.Errorf("Inicio() alterado por Devolver: %v, want %v", e.Inicio(), tt.inicio)
			}
			if !e.Vencimento().Equal(venc) {
				t.Errorf("Vencimento() alterado por Devolver: %v, want %v", e.Vencimento(), venc)
			}
		})
	}
}

func TestEmprestimoDevolverJaDevolvido(t *testing.T) {
	sp := saoPaulo(t)
	inicio := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)
	primeira := time.Date(2026, 3, 15, 10, 0, 0, 0, sp)

	tests := []struct {
		name    string
		segunda time.Time
	}{
		{name: "segunda devolucao valida", segunda: time.Date(2026, 3, 20, 10, 0, 0, 0, sp)},
		{name: "ordem: ja devolvido antes de data zero", segunda: time.Time{}},
		{name: "ordem: ja devolvido antes de antes do inicio", segunda: time.Date(2026, 3, 1, 10, 0, 0, 0, sp)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := NovoEmprestimo(1, 1, inicio)
			if err != nil {
				t.Fatalf("NovoEmprestimo erro inesperado: %v", err)
			}
			if err := e.Devolver(primeira); err != nil {
				t.Fatalf("primeira Devolver erro inesperado: %v", err)
			}

			err = e.Devolver(tt.segunda)
			if !errors.Is(err, ErrEmprestimoJaDevolvido) {
				t.Fatalf("segunda Devolver(%v) erro = %v, want %v", tt.segunda, err, ErrEmprestimoJaDevolvido)
			}
			if !e.Devolucao().Equal(primeira) {
				t.Errorf("Devolucao() = %v, want %v (primeira devolucao intacta)", e.Devolucao(), primeira)
			}
			if e.Aberto() {
				t.Error("Aberto() = true, want false")
			}
		})
	}
}

func FuzzNovoEmprestimoVencimento(f *testing.F) {
	sp := saoPaulo(f)
	for _, t0 := range []time.Time{
		time.Date(2026, 3, 10, 10, 0, 0, 0, sp),
		time.Date(2026, 3, 10, 23, 30, 0, 0, sp),
		time.Date(2026, 1, 25, 9, 0, 0, 0, sp),
		time.Date(2026, 12, 25, 9, 0, 0, 0, sp),
		time.Date(2024, 2, 20, 9, 0, 0, 0, sp),
		time.Date(2026, 2, 20, 9, 0, 0, 0, sp),
		time.Date(2018, 10, 21, 9, 0, 0, 0, sp),
		time.Date(2019, 2, 3, 9, 0, 0, 0, sp),
		time.Date(2019, 2, 16, 23, 30, 0, 0, sp),
	} {
		f.Add(t0.Unix())
	}
	f.Add(fuzzMin)
	f.Add(fuzzMax - 1)

	f.Fuzz(func(t *testing.T, u int64) {
		inicio := time.Unix(limitarUnix(u), 0).UTC()

		e, err := NovoEmprestimo(1, 1, inicio)
		if err != nil {
			t.Fatalf("NovoEmprestimo(1, 1, %v) erro inesperado: %v", inicio, err)
		}
		venc := e.Vencimento()

		// Oráculo independente: data civil SP do início, ao meio-dia UTC,
		// somada de 14 dias com AddDate.
		a, m, d := inicio.In(sp).Date()
		wa, wm, wd := time.Date(a, m, d, 12, 0, 0, 0, time.UTC).AddDate(0, 0, 14).Date()

		v := venc.In(sp)
		ga, gm, gd := v.Date()
		if ga != wa || gm != wm || gd != wd {
			t.Errorf("inicio %v: Vencimento() data civil SP = %04d-%02d-%02d, oraculo = %04d-%02d-%02d", inicio, ga, gm, gd, wa, wm, wd)
		}
		if v.Hour() != 12 {
			t.Errorf("inicio %v: Vencimento() hora SP = %d (%v), want 12", inicio, v.Hour(), v)
		}

		got, err := CalcularMulta(venc, venc)
		if err != nil {
			t.Fatalf("CalcularMulta(%v, %v) erro inesperado: %v", venc, venc, err)
		}
		if !got.Equal(decimal.Zero) {
			t.Errorf("CalcularMulta(%v, %v) = %s, want 0", venc, venc, got)
		}
	})
}
