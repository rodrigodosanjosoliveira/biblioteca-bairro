package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

// Multa tem receptor por valor: a expressão de método só compila assim.
var _ func(Emprestimo, time.Time) (decimal.Decimal, error) = Emprestimo.Multa

// novoEmprestimoMulta abre um empréstimo e, se dev não for zero, o devolve em
// dev. Falha o teste se o domínio recusar.
func novoEmprestimoMulta(t *testing.T, inicio, dev time.Time) Emprestimo {
	t.Helper()
	e, err := NovoEmprestimo(1, 2, inicio)
	if err != nil {
		t.Fatalf("NovoEmprestimo(1, 2, %v) erro inesperado: %v", inicio, err)
	}
	if !dev.IsZero() {
		if err := e.Devolver(dev); err != nil {
			t.Fatalf("Devolver(%v) erro inesperado: %v", dev, err)
		}
	}
	return e
}

// checarMulta confere valor e erro devolvidos por Multa. Com wantErr, exige o
// próprio sentinela (sem wrap) e valor decimal.Zero.
func checarMulta(t *testing.T, e Emprestimo, ref time.Time, want decimal.Decimal, wantErr error) {
	t.Helper()
	got, err := e.Multa(ref)
	if wantErr != nil {
		if err != wantErr {
			t.Fatalf("Multa(%v) erro = %v, want %v (o próprio sentinela, sem wrap)", ref, err, wantErr)
		}
		if !got.Equal(decimal.Zero) {
			t.Errorf("Multa(%v) em erro = %s, want 0", ref, got)
		}
		return
	}
	if err != nil {
		t.Fatalf("Multa(%v) erro inesperado: %v", ref, err)
	}
	if !got.Equal(want) {
		t.Errorf("Multa(%v) = %s, want %s (vencimento %v, devolução %v)", ref, got, want, e.Vencimento(), e.Devolucao())
	}
}

// Marcos de atraso sobre o empréstimo aberto em 10/03/2026 10:00 SP, que vence
// em 24/03/2026 12:00 SP.
func marcosMulta(sp *time.Location) []struct {
	name   string
	quando time.Time
	want   decimal.Decimal
} {
	d := func(a int, m time.Month, dia, h, min int) time.Time { return time.Date(a, m, dia, h, min, 0, 0, sp) }
	return []struct {
		name   string
		quando time.Time
		want   decimal.Decimal
	}{
		{name: "antes do vencimento", quando: d(2026, 3, 20, 15, 0), want: decimal.Zero},
		{name: "no inicio", quando: d(2026, 3, 10, 10, 0), want: decimal.Zero},
		{name: "no dia do vencimento 00:00", quando: d(2026, 3, 24, 0, 0), want: decimal.Zero},
		{name: "no dia do vencimento 12:00", quando: d(2026, 3, 24, 12, 0), want: decimal.Zero},
		{name: "no dia do vencimento 23:59", quando: d(2026, 3, 24, 23, 59), want: decimal.Zero},
		{name: "1 dia de atraso 00:00", quando: d(2026, 3, 25, 0, 0), want: decimal.NewFromInt(1)},
		{name: "1 dia de atraso 08:00", quando: d(2026, 3, 25, 8, 0), want: decimal.NewFromInt(1)},
		{name: "2 dias de atraso", quando: d(2026, 3, 26, 9, 0), want: decimal.NewFromInt(2)},
		{name: "19 dias de atraso", quando: d(2026, 4, 12, 9, 0), want: decimal.NewFromInt(19)},
		{name: "20 dias atinge o teto", quando: d(2026, 4, 13, 9, 0), want: decimal.NewFromInt(20)},
		{name: "21 dias limitado ao teto", quando: d(2026, 4, 14, 9, 0), want: decimal.NewFromInt(20)},
		{name: "muito acima do teto", quando: d(2031, 1, 1, 9, 0), want: decimal.NewFromInt(20)},
	}
}

func TestEmprestimoMultaDevolvido(t *testing.T) {
	sp := saoPaulo(t)
	inicio := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)

	refs := []struct {
		name string
		ref  time.Time
	}{
		{name: "ref antes do inicio", ref: time.Date(2026, 3, 1, 9, 0, 0, 0, sp)},
		{name: "ref antes do vencimento", ref: time.Date(2026, 3, 12, 9, 0, 0, 0, sp)},
		{name: "ref depois do vencimento", ref: time.Date(2026, 3, 28, 9, 0, 0, 0, sp)},
		{name: "ref muito depois", ref: time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC)},
		{name: "ref zero", ref: time.Time{}},
	}
	for _, m := range marcosMulta(sp) {
		for _, r := range refs {
			t.Run(m.name+"/"+r.name, func(t *testing.T) {
				e := novoEmprestimoMulta(t, inicio, m.quando)
				checarMulta(t, e, r.ref, m.want, nil)
			})
		}
	}
}

func TestEmprestimoMultaAberto(t *testing.T) {
	sp := saoPaulo(t)
	inicio := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)

	for _, m := range marcosMulta(sp) {
		t.Run(m.name, func(t *testing.T) {
			e := novoEmprestimoMulta(t, inicio, time.Time{})
			checarMulta(t, e, m.quando, m.want, nil)
		})
	}

	tests := []struct {
		name    string
		ref     time.Time
		want    decimal.Decimal
		wantErr error
	}{
		{name: "ref zero", ref: time.Time{}, wantErr: ErrReferenciaZero},
		{name: "ref um dia antes do inicio", ref: time.Date(2026, 3, 9, 10, 0, 0, 0, sp), want: decimal.Zero},
		{name: "ref um minuto antes do inicio", ref: time.Date(2026, 3, 10, 9, 59, 0, 0, sp), want: decimal.Zero},
		{name: "ref muito antes do inicio", ref: time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC), want: decimal.Zero},
		{name: "ref Unix 0", ref: time.Unix(0, 0), want: decimal.Zero},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := novoEmprestimoMulta(t, inicio, time.Time{})
			checarMulta(t, e, tt.ref, tt.want, tt.wantErr)
		})
	}
}

func TestEmprestimoMultaInvalido(t *testing.T) {
	sp := saoPaulo(t)
	venc := time.Date(2026, 3, 24, 12, 0, 0, 0, sp)
	dev := time.Date(2026, 4, 1, 9, 0, 0, 0, sp)
	refValida := time.Date(2026, 4, 1, 9, 0, 0, 0, sp)

	tests := []struct {
		name string
		e    Emprestimo
		ref  time.Time
	}{
		{name: "Emprestimo{} com ref valida", e: Emprestimo{}, ref: refValida},
		{name: "Emprestimo{} com ref zero", e: Emprestimo{}, ref: time.Time{}},
		{name: "inicio zero com vencimento", e: Emprestimo{vencimento: venc}, ref: refValida},
		{name: "inicio zero com vencimento e ref zero", e: Emprestimo{vencimento: venc}, ref: time.Time{}},
		{name: "inicio zero com devolucao", e: Emprestimo{vencimento: venc, devolucao: dev}, ref: refValida},
		{name: "inicio zero com devolucao e ref zero", e: Emprestimo{vencimento: venc, devolucao: dev}, ref: time.Time{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checarMulta(t, tt.e, tt.ref, decimal.Zero, ErrEmprestimoInvalido)
		})
	}
}

// Estado que nenhum construtor produz, montado só para fixar que o erro de
// CalcularMulta volta como está, sem wrap.
func TestEmprestimoMultaErroDeCalcularMultaSemWrap(t *testing.T) {
	sp := saoPaulo(t)
	inicio := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)
	ref := time.Date(2026, 4, 1, 9, 0, 0, 0, sp)

	tests := []struct {
		name    string
		e       Emprestimo
		ref     time.Time
		wantErr error
	}{
		{name: "aberto sem vencimento", e: Emprestimo{inicio: inicio}, ref: ref, wantErr: ErrVencimentoZero},
		{name: "devolvido sem vencimento", e: Emprestimo{inicio: inicio, devolucao: ref}, ref: ref, wantErr: ErrVencimentoZero},
		{name: "devolvido sem vencimento e ref zero", e: Emprestimo{inicio: inicio, devolucao: ref}, ref: time.Time{}, wantErr: ErrVencimentoZero},
		{name: "aberto sem vencimento e ref zero", e: Emprestimo{inicio: inicio}, ref: time.Time{}, wantErr: ErrReferenciaZero},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			checarMulta(t, tt.e, tt.ref, decimal.Zero, tt.wantErr)
		})
	}
}

func TestEmprestimoMultaFusos(t *testing.T) {
	sp := saoPaulo(t)
	zonas := []*time.Location{
		sp,
		time.UTC,
		time.FixedZone("UTC+9", 9*60*60),
		time.FixedZone("UTC-11", -11*60*60),
	}

	tests := []struct {
		name   string
		inicio time.Time
		quando time.Time
		want   decimal.Decimal
	}{
		{
			// 25/03 02:30 UTC = 24/03 23:30 SP: dia seguinte em UTC, dia do vencimento em SP.
			name:   "dia seguinte em UTC mas dia do vencimento em SP",
			inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp),
			quando: time.Date(2026, 3, 25, 2, 30, 0, 0, time.UTC),
			want:   decimal.Zero,
		},
		{
			name:   "dia seguinte tambem em SP",
			inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp),
			quando: time.Date(2026, 3, 25, 3, 30, 0, 0, time.UTC),
			want:   decimal.NewFromInt(1),
		},
		{
			// 10/03 01:00 UTC = 09/03 22:00 SP: vencimento em 23/03 12:00 SP.
			name:   "inicio em dia UTC diferente de SP, ref no dia do vencimento SP",
			inicio: time.Date(2026, 3, 10, 1, 0, 0, 0, time.UTC),
			quando: time.Date(2026, 3, 24, 2, 0, 0, 0, time.UTC),
			want:   decimal.Zero,
		},
		{
			name:   "inicio em dia UTC diferente de SP, ref no dia seguinte ao vencimento SP",
			inicio: time.Date(2026, 3, 10, 1, 0, 0, 0, time.UTC),
			quando: time.Date(2026, 3, 24, 3, 0, 0, 0, time.UTC),
			want:   decimal.NewFromInt(1),
		},
		{
			name:   "3 dias de atraso",
			inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp),
			quando: time.Date(2026, 3, 27, 22, 0, 0, 0, sp),
			want:   decimal.NewFromInt(3),
		},
		{
			name:   "teto",
			inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp),
			quando: time.Date(2026, 4, 14, 0, 30, 0, 0, sp),
			want:   decimal.NewFromInt(20),
		},
	}
	for _, tt := range tests {
		for _, zi := range zonas {
			for _, zq := range zonas {
				inicio := tt.inicio.In(zi)
				quando := tt.quando.In(zq)
				t.Run(tt.name+"/aberto/"+zi.String()+"/"+zq.String(), func(t *testing.T) {
					e := novoEmprestimoMulta(t, inicio, time.Time{})
					checarMulta(t, e, quando, tt.want, nil)
				})
				t.Run(tt.name+"/devolvido/"+zi.String()+"/"+zq.String(), func(t *testing.T) {
					e := novoEmprestimoMulta(t, inicio, quando)
					checarMulta(t, e, time.Time{}, tt.want, nil)
				})
			}
		}
	}
}

func TestEmprestimoMultaHorarioDeVerao(t *testing.T) {
	sp := saoPaulo(t)
	d := func(a int, m time.Month, dia, h, min int) time.Time { return time.Date(a, m, dia, h, min, 0, 0, sp) }

	tests := []struct {
		name   string
		inicio time.Time
		quando time.Time
		want   decimal.Decimal
	}{
		// Início em 21/10/2018: vence em 04/11/2018, o dia sem 00:00 (BRST).
		{name: "2018 ref antes do vencimento", inicio: d(2018, 10, 21, 9, 0), quando: d(2018, 11, 3, 23, 59), want: decimal.Zero},
		{name: "2018 ref na hora inexistente", inicio: d(2018, 10, 21, 9, 0), quando: d(2018, 11, 4, 0, 30), want: decimal.Zero},
		{name: "2018 ref 01:00 do vencimento", inicio: d(2018, 10, 21, 9, 0), quando: d(2018, 11, 4, 1, 0), want: decimal.Zero},
		{name: "2018 ref 23:59 do vencimento", inicio: d(2018, 10, 21, 9, 0), quando: d(2018, 11, 4, 23, 59), want: decimal.Zero},
		{name: "2018 ref 1 dia depois", inicio: d(2018, 10, 21, 9, 0), quando: d(2018, 11, 5, 0, 0), want: decimal.NewFromInt(1)},
		{name: "2018 ref 1 dia depois em UTC", inicio: d(2018, 10, 21, 9, 0), quando: time.Date(2018, 11, 5, 2, 0, 0, 0, time.UTC), want: decimal.NewFromInt(1)},
		{name: "2018 ref 23:59 do vencimento em UTC", inicio: d(2018, 10, 21, 9, 0), quando: time.Date(2018, 11, 5, 1, 59, 0, 0, time.UTC), want: decimal.Zero},
		{name: "2018 ref 20 dias", inicio: d(2018, 10, 21, 9, 0), quando: d(2018, 11, 24, 9, 0), want: decimal.NewFromInt(20)},
		{name: "2018 ref 21 dias", inicio: d(2018, 10, 21, 9, 0), quando: d(2018, 11, 25, 9, 0), want: decimal.NewFromInt(20)},
		// Início em 02/02/2019: vence em 16/02/2019; à meia-noite de 17/02 o relógio volta para 23:00 de 16/02.
		{name: "2019 ref 16/02 23:30 BRST", inicio: d(2019, 2, 2, 9, 0), quando: time.Date(2019, 2, 17, 1, 30, 0, 0, time.UTC), want: decimal.Zero},
		{name: "2019 ref 16/02 23:30 BRT", inicio: d(2019, 2, 2, 9, 0), quando: time.Date(2019, 2, 17, 2, 30, 0, 0, time.UTC), want: decimal.Zero},
		{name: "2019 ref 16/02 23:59 BRT", inicio: d(2019, 2, 2, 9, 0), quando: time.Date(2019, 2, 17, 2, 59, 0, 0, time.UTC), want: decimal.Zero},
		{name: "2019 ref 17/02 00:00 BRT", inicio: d(2019, 2, 2, 9, 0), quando: time.Date(2019, 2, 17, 3, 0, 0, 0, time.UTC), want: decimal.NewFromInt(1)},
		{name: "2019 ref 17/02 00:30 SP", inicio: d(2019, 2, 2, 9, 0), quando: d(2019, 2, 17, 0, 30), want: decimal.NewFromInt(1)},
		// Início em 03/02/2019: vence em 17/02/2019, o dia de 25h.
		{name: "2019 vence 17/02, ref 16/02 23:30", inicio: d(2019, 2, 3, 9, 0), quando: d(2019, 2, 16, 23, 30), want: decimal.Zero},
		{name: "2019 vence 17/02, ref 17/02 23:59", inicio: d(2019, 2, 3, 9, 0), quando: d(2019, 2, 17, 23, 59), want: decimal.Zero},
		{name: "2019 vence 17/02, ref 18/02 00:00", inicio: d(2019, 2, 3, 9, 0), quando: d(2019, 2, 18, 0, 0), want: decimal.NewFromInt(1)},
	}
	for _, tt := range tests {
		t.Run(tt.name+"/aberto", func(t *testing.T) {
			e := novoEmprestimoMulta(t, tt.inicio, time.Time{})
			checarMulta(t, e, tt.quando, tt.want, nil)
		})
		t.Run(tt.name+"/devolvido", func(t *testing.T) {
			e := novoEmprestimoMulta(t, tt.inicio, tt.quando)
			checarMulta(t, e, time.Date(2100, 1, 1, 0, 0, 0, 0, time.UTC), tt.want, nil)
		})
	}
}

// O empréstimo lido do banco (vencimento DATE à meia-noite UTC) tem que dar a
// mesma multa que o aberto pelo domínio.
func TestEmprestimoMultaNovoEReidratadoEquivalentes(t *testing.T) {
	sp := saoPaulo(t)

	tests := []struct {
		name   string
		inicio time.Time
		dev    time.Time
	}{
		{name: "aberto", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)},
		{name: "aberto inicio 23:30 SP", inicio: time.Date(2026, 3, 10, 23, 30, 0, 0, sp)},
		{name: "aberto inicio UTC em outro dia civil", inicio: time.Date(2026, 3, 10, 1, 0, 0, 0, time.UTC)},
		{name: "aberto inicio UTC+9", inicio: time.Date(2026, 3, 10, 8, 0, 0, 0, time.FixedZone("UTC+9", 9*60*60))},
		{name: "aberto horario de verao 2018", inicio: time.Date(2018, 10, 21, 9, 0, 0, 0, sp)},
		{name: "aberto horario de verao 2019", inicio: time.Date(2019, 2, 2, 9, 0, 0, 0, sp)},
		{name: "devolvido no prazo", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), dev: time.Date(2026, 3, 24, 23, 59, 0, 0, sp)},
		{name: "devolvido com atraso", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), dev: time.Date(2026, 3, 30, 9, 0, 0, 0, sp)},
		{name: "devolvido acima do teto", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), dev: time.Date(2027, 1, 1, 9, 0, 0, 0, sp)},
		{name: "devolvido em UTC", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), dev: time.Date(2026, 3, 25, 2, 30, 0, 0, time.UTC)},
		{name: "devolvido horario de verao 2019", inicio: time.Date(2019, 2, 2, 9, 0, 0, 0, sp), dev: time.Date(2019, 2, 17, 2, 30, 0, 0, time.UTC)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig := novoEmprestimoMulta(t, tt.inicio, tt.dev)
			venc := vencimentoComoDATE(t, orig.Vencimento())
			reid, err := ReidratarEmprestimo(7, orig.LivroID(), orig.UsuarioID(), orig.Inicio(), venc, orig.Devolucao())
			if err != nil {
				t.Fatalf("ReidratarEmprestimo erro inesperado: %v", err)
			}

			v := orig.Vencimento()
			refs := []time.Time{
				time.Time{},
				orig.Inicio().AddDate(0, 0, -1),
				v.Add(-24 * time.Hour),
				v,
				v.Add(11*time.Hour + 59*time.Minute),
				v.Add(12 * time.Hour),
				v.Add(24 * time.Hour),
				v.AddDate(0, 0, 5),
				v.AddDate(0, 0, 20),
				v.AddDate(0, 0, 21),
				v.AddDate(5, 0, 0),
			}
			for _, ref := range refs {
				gotO, errO := orig.Multa(ref)
				gotR, errR := reid.Multa(ref)
				if errO != errR || !gotO.Equal(gotR) {
					t.Errorf("ref %v: NovoEmprestimo.Multa = (%s, %v), ReidratarEmprestimo.Multa = (%s, %v)", ref, gotO, errO, gotR, errR)
				}
			}
		})
	}
}

// Empréstimo reidratado com vencimento fora do prazo padrão: a multa usa o
// vencimento guardado, não início + PrazoEmprestimoDias.
func TestEmprestimoMultaUsaVencimentoGuardado(t *testing.T) {
	sp := saoPaulo(t)
	inicio := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)
	venc := dataComoDATE(2026, 4, 9) // 30 dias depois do início

	tests := []struct {
		name string
		dev  time.Time
		ref  time.Time
		want decimal.Decimal
	}{
		{name: "aberto no dia do prazo padrao", ref: time.Date(2026, 3, 25, 9, 0, 0, 0, sp), want: decimal.Zero},
		{name: "aberto no dia do vencimento", ref: time.Date(2026, 4, 9, 23, 59, 0, 0, sp), want: decimal.Zero},
		{name: "aberto 1 dia depois", ref: time.Date(2026, 4, 10, 0, 0, 0, 0, sp), want: decimal.NewFromInt(1)},
		{name: "devolvido 2 dias depois", dev: time.Date(2026, 4, 11, 9, 0, 0, 0, sp), want: decimal.NewFromInt(2)},
		{name: "devolvido no dia do prazo padrao", dev: time.Date(2026, 3, 25, 9, 0, 0, 0, sp), ref: time.Date(2026, 5, 1, 9, 0, 0, 0, sp), want: decimal.Zero},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := ReidratarEmprestimo(9, 1, 2, inicio, venc, tt.dev)
			if err != nil {
				t.Fatalf("ReidratarEmprestimo erro inesperado: %v", err)
			}
			checarMulta(t, e, tt.ref, tt.want, nil)
		})
	}
}

func TestErrReferenciaZero(t *testing.T) {
	if ErrReferenciaZero == nil {
		t.Fatal("ErrReferenciaZero nil")
	}
	if got, want := ErrReferenciaZero.Error(), "emprestimo: data de referência zero"; got != want {
		t.Errorf("Error() = %q, want %q", got, want)
	}

	outros := []struct {
		name string
		err  error
	}{
		{name: "ErrDevolucaoZero", err: ErrDevolucaoZero},
		{name: "ErrDevolucaoEmprestimoZero", err: ErrDevolucaoEmprestimoZero},
		{name: "ErrEmprestimoInvalido", err: ErrEmprestimoInvalido},
		{name: "ErrVencimentoZero", err: ErrVencimentoZero},
		{name: "ErrInicioZero", err: ErrInicioZero},
	}
	for _, tt := range outros {
		t.Run(tt.name, func(t *testing.T) {
			if errors.Is(ErrReferenciaZero, tt.err) || errors.Is(tt.err, ErrReferenciaZero) {
				t.Errorf("ErrReferenciaZero e %s não podem ser o mesmo erro", tt.name)
			}
		})
	}
}

func TestEmprestimoMultaNaoMuta(t *testing.T) {
	sp := saoPaulo(t)
	inicio := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)
	dev := time.Date(2026, 3, 30, 9, 0, 0, 0, time.UTC)
	ref := time.Date(2026, 4, 1, 9, 0, 0, 0, sp)

	reid, err := ReidratarEmprestimo(5, 1, 2, inicio, dataComoDATE(2026, 3, 24), dev)
	if err != nil {
		t.Fatalf("ReidratarEmprestimo erro inesperado: %v", err)
	}

	tests := []struct {
		name string
		e    Emprestimo
		ref  time.Time
	}{
		{name: "aberto", e: novoEmprestimoMulta(t, inicio, time.Time{}), ref: ref},
		{name: "aberto ref zero (erro)", e: novoEmprestimoMulta(t, inicio, time.Time{}), ref: time.Time{}},
		{name: "aberto ref antes do inicio", e: novoEmprestimoMulta(t, inicio, time.Time{}), ref: inicio.AddDate(0, 0, -3)},
		{name: "devolvido", e: novoEmprestimoMulta(t, inicio, dev), ref: ref},
		{name: "devolvido ref zero", e: novoEmprestimoMulta(t, inicio, dev), ref: time.Time{}},
		{name: "reidratado", e: reid, ref: ref},
		{name: "Emprestimo{} (erro)", e: Emprestimo{}, ref: ref},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := tt.e
			antes := tt.e
			_, _ = e.Multa(tt.ref)
			p := &e
			_, _ = p.Multa(tt.ref)
			checarMesmoEmprestimo(t, e, antes)
			if !e.Vencimento().Equal(antes.Vencimento()) || e.Vencimento().Location() != antes.Vencimento().Location() {
				t.Errorf("Vencimento() = %v, want %v", e.Vencimento(), antes.Vencimento())
			}
		})
	}
}

func FuzzEmprestimoMulta(f *testing.F) {
	sp := saoPaulo(f)
	add := func(inicio, ref, dev time.Time, aberto bool) { f.Add(inicio.Unix(), ref.Unix(), dev.Unix(), aberto) }

	i26 := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)
	add(i26, time.Date(2026, 3, 24, 23, 59, 0, 0, sp), time.Date(2026, 3, 25, 0, 0, 0, 0, sp), true)
	add(i26, time.Date(2026, 3, 25, 0, 0, 0, 0, sp), time.Date(2026, 3, 24, 23, 59, 0, 0, sp), false)
	add(i26, time.Date(2026, 3, 1, 0, 0, 0, 0, sp), time.Date(2026, 4, 14, 0, 0, 0, 0, sp), true)
	add(i26, time.Date(2026, 4, 13, 0, 0, 0, 0, sp), time.Date(2026, 4, 13, 0, 0, 0, 0, sp), false)
	add(i26, time.Date(2026, 3, 25, 2, 30, 0, 0, time.UTC), time.Date(2026, 3, 25, 3, 30, 0, 0, time.UTC), false)
	add(time.Date(2026, 3, 10, 1, 0, 0, 0, time.UTC), time.Date(2026, 3, 24, 3, 0, 0, 0, time.UTC), time.Date(2026, 3, 24, 2, 0, 0, 0, time.UTC), false)
	add(time.Date(2018, 10, 21, 9, 0, 0, 0, sp), time.Date(2018, 11, 5, 0, 0, 0, 0, sp), time.Date(2018, 11, 4, 23, 59, 0, 0, sp), true)
	add(time.Date(2019, 2, 2, 9, 0, 0, 0, sp), time.Date(2019, 2, 17, 2, 30, 0, 0, time.UTC), time.Date(2019, 2, 17, 3, 0, 0, 0, time.UTC), false)
	f.Add(fuzzMin, fuzzMax-1, fuzzMax-1, true)
	f.Add(fuzzMax-1, fuzzMin, fuzzMax-1, false)

	f.Fuzz(func(t *testing.T, inicioUnix, refUnix, devUnix int64, aberto bool) {
		inicio := time.Unix(limitarUnix(inicioUnix), 0).UTC()
		ref := time.Unix(limitarUnix(refUnix), 0).UTC()
		dev := time.Unix(limitarUnix(devUnix), 0).UTC()

		e, err := NovoEmprestimo(1, 2, inicio)
		if err != nil {
			return
		}
		refEfetiva := ref
		if !aberto {
			if err := e.Devolver(dev); err != nil {
				return
			}
			refEfetiva = dev
		}

		got, err := e.Multa(ref)
		if err != nil {
			t.Fatalf("inicio %v ref %v dev %v aberto %v: Multa erro inesperado: %v", inicio, ref, dev, aberto, err)
		}
		want, err := CalcularMulta(e.Vencimento(), refEfetiva)
		if err != nil {
			t.Fatalf("CalcularMulta(%v, %v) erro inesperado: %v", e.Vencimento(), refEfetiva, err)
		}
		if !got.Equal(want) {
			t.Errorf("inicio %v ref %v dev %v aberto %v: Multa = %s, CalcularMulta = %s", inicio, ref, dev, aberto, got, want)
		}
		if o := oraculoMulta(e.Vencimento(), refEfetiva, sp); !got.Equal(o) {
			t.Errorf("inicio %v ref %v dev %v aberto %v: Multa = %s, oraculo = %s", inicio, ref, dev, aberto, got, o)
		}

		if !aberto {
			if g0, err := e.Multa(time.Time{}); err != nil || !g0.Equal(got) {
				t.Errorf("devolvido: Multa(zero) = (%s, %v), want (%s, nil)", g0, err, got)
			}
		}

		reid, err := ReidratarEmprestimo(8, e.LivroID(), e.UsuarioID(), e.Inicio(), vencimentoComoDATE(t, e.Vencimento()), e.Devolucao())
		if err != nil {
			t.Fatalf("ReidratarEmprestimo erro inesperado: %v", err)
		}
		if gr, err := reid.Multa(ref); err != nil || !gr.Equal(got) {
			t.Errorf("reidratado: Multa = (%s, %v), want (%s, nil)", gr, err, got)
		}
	})
}
