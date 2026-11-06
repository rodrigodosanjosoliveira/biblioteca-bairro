package domain

import (
	"errors"
	"math"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func TestConstantesRenovacao(t *testing.T) {
	if LimiteRenovacoes != 2 {
		t.Errorf("LimiteRenovacoes = %d, want 2", LimiteRenovacoes)
	}
	if PrazoRenovacaoDias != 14 {
		t.Errorf("PrazoRenovacaoDias = %d, want 14", PrazoRenovacaoDias)
	}
}

func TestErrosRenovacaoMensagens(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "vencido", err: ErrEmprestimoVencido, want: "emprestimo: vencido"},
		{name: "limite de renovacoes", err: ErrLimiteRenovacoes, want: "emprestimo: limite de renovações atingido"},
		{name: "renovacoes invalidas", err: ErrRenovacoesInvalidas, want: "emprestimo: renovações inválidas"},
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

// As sentinelas novas são distintas entre si e das que já existiam.
func TestErrosRenovacaoDistintos(t *testing.T) {
	todos := []error{
		ErrEmprestimoVencido, ErrLimiteRenovacoes, ErrRenovacoesInvalidas,
		ErrEmprestimoJaDevolvido, ErrEmprestimoInvalido, ErrReferenciaZero,
		ErrVencimentoEmprestimoZero, ErrVencimentoZero,
	}
	for i, a := range todos {
		for j, b := range todos {
			if i != j && errors.Is(a, b) {
				t.Errorf("errors.Is(%v, %v) = true, want false", a, b)
			}
		}
	}
}

func TestNovoEmprestimoSemRenovacoes(t *testing.T) {
	e, err := NovoEmprestimo(1, 1, time.Date(2026, 3, 10, 10, 0, 0, 0, saoPaulo(t)))
	if err != nil {
		t.Fatalf("NovoEmprestimo erro inesperado: %v", err)
	}
	if e.Renovacoes() != 0 {
		t.Errorf("Renovacoes() = %d, want 0", e.Renovacoes())
	}
	var zero Emprestimo
	if zero.Renovacoes() != 0 {
		t.Errorf("Emprestimo{}.Renovacoes() = %d, want 0", zero.Renovacoes())
	}
}

func TestReidratarEmprestimoRenovacoes(t *testing.T) {
	sp := saoPaulo(t)
	inicio := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)
	venc := dataComoDATE(2026, 3, 24)
	dev := time.Date(2026, 3, 20, 15, 0, 0, 0, sp)

	tests := []struct {
		name       string
		id         int64
		devolucao  time.Time
		vencimento time.Time
		renovacoes int
		wantErr    error
	}{
		{name: "zero", id: 1, vencimento: venc, renovacoes: 0},
		{name: "uma", id: 1, vencimento: venc, renovacoes: 1},
		{name: "no limite", id: 1, vencimento: venc, renovacoes: LimiteRenovacoes},
		{name: "fechado no limite", id: 1, vencimento: venc, devolucao: dev, renovacoes: 2},
		{name: "nao exige vencimento coerente com o contador", id: 1, vencimento: dataComoDATE(2026, 3, 11), renovacoes: 2},
		{name: "negativo", id: 1, vencimento: venc, renovacoes: -1, wantErr: ErrRenovacoesInvalidas},
		{name: "acima do limite", id: 1, vencimento: venc, renovacoes: 3, wantErr: ErrRenovacoesInvalidas},
		{name: "minimo int", id: 1, vencimento: venc, renovacoes: math.MinInt, wantErr: ErrRenovacoesInvalidas},
		{name: "maximo int", id: 1, vencimento: venc, renovacoes: math.MaxInt, wantErr: ErrRenovacoesInvalidas},
		{name: "fechado acima do limite", id: 1, vencimento: venc, devolucao: dev, renovacoes: 3, wantErr: ErrRenovacoesInvalidas},
		// O contador é a última validação.
		{name: "ordem: id antes do contador", id: 0, vencimento: venc, renovacoes: -1, wantErr: ErrEmprestimoIDInvalido},
		{name: "ordem: vencimento zero antes do contador", id: 1, vencimento: time.Time{}, renovacoes: 3, wantErr: ErrVencimentoEmprestimoZero},
		{name: "ordem: vencimento nao posterior antes do contador", id: 1, vencimento: dataComoDATE(2026, 3, 10), renovacoes: 3, wantErr: ErrVencimentoNaoPosteriorAoInicio},
		{name: "ordem: devolucao antes do inicio antes do contador", id: 1, vencimento: venc, devolucao: time.Date(2026, 3, 9, 10, 0, 0, 0, sp), renovacoes: 3, wantErr: ErrDevolucaoAntesDoInicio},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReidratarEmprestimo(tt.id, 2, 3, inicio, tt.vencimento, tt.devolucao, tt.renovacoes)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReidratarEmprestimo erro = %v, want %v", err, tt.wantErr)
			}
			if tt.wantErr != nil {
				checarEmprestimoZero(t, got)
				return
			}
			if got.Renovacoes() != tt.renovacoes {
				t.Errorf("Renovacoes() = %d, want %d", got.Renovacoes(), tt.renovacoes)
			}
			if got.ID() != tt.id || got.LivroID() != 2 || got.UsuarioID() != 3 {
				t.Errorf("ID/LivroID/UsuarioID = %d/%d/%d, want %d/2/3", got.ID(), got.LivroID(), got.UsuarioID(), tt.id)
			}
		})
	}
}

// novoParaRenovar abre o empréstimo-base: início 10/03/2026 10:00 SP,
// vencimento 24/03/2026 12:00 SP.
func novoParaRenovar(t *testing.T) Emprestimo {
	t.Helper()
	e, err := NovoEmprestimo(42, 7, time.Date(2026, 3, 10, 10, 0, 0, 0, saoPaulo(t)))
	if err != nil {
		t.Fatalf("NovoEmprestimo erro inesperado: %v", err)
	}
	return e
}

func reidratadoParaRenovar(t *testing.T, inicio, venc, dev time.Time, renovacoes int) Emprestimo {
	t.Helper()
	e, err := ReidratarEmprestimo(11, 42, 7, inicio, venc, dev, renovacoes)
	if err != nil {
		t.Fatalf("ReidratarEmprestimo erro inesperado: %v", err)
	}
	return e
}

func TestEmprestimoRenovarSucesso(t *testing.T) {
	sp := saoPaulo(t)
	utc9 := time.FixedZone("UTC+9", 9*60*60)
	inicioBase := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)

	tests := []struct {
		name           string
		e              func(t *testing.T) Emprestimo
		ref            time.Time
		wantA          int
		wantM          time.Month
		wantD          int
		wantRenovacoes int
	}{
		{name: "no dia do inicio", e: novoParaRenovar, ref: inicioBase, wantA: 2026, wantM: 4, wantD: 7, wantRenovacoes: 1},
		{name: "ref anterior ao inicio nao e recusada", e: novoParaRenovar, ref: time.Date(2026, 3, 1, 10, 0, 0, 0, sp), wantA: 2026, wantM: 4, wantD: 7, wantRenovacoes: 1},
		{name: "no dia do vencimento antes das 12:00", e: novoParaRenovar, ref: time.Date(2026, 3, 24, 8, 0, 0, 0, sp), wantA: 2026, wantM: 4, wantD: 7, wantRenovacoes: 1},
		{name: "no instante do vencimento (12:00 SP)", e: novoParaRenovar, ref: time.Date(2026, 3, 24, 12, 0, 0, 0, sp), wantA: 2026, wantM: 4, wantD: 7, wantRenovacoes: 1},
		{name: "no dia do vencimento as 23:59:59.999 SP", e: novoParaRenovar, ref: time.Date(2026, 3, 24, 23, 59, 59, 999999999, sp), wantA: 2026, wantM: 4, wantD: 7, wantRenovacoes: 1},
		{
			// 25/03 02:30 UTC é 24/03 23:30 SP: ainda o dia do vencimento.
			name: "ref em UTC ja no dia seguinte, mas 24/03 em SP", e: novoParaRenovar,
			ref: time.Date(2026, 3, 25, 2, 30, 0, 0, time.UTC), wantA: 2026, wantM: 4, wantD: 7, wantRenovacoes: 1,
		},
		{
			// 24/03 10:00 em UTC+9 é 23/03 22:00 SP.
			name: "ref em UTC+9", e: novoParaRenovar,
			ref: time.Date(2026, 3, 24, 10, 0, 0, 0, utc9), wantA: 2026, wantM: 4, wantD: 7, wantRenovacoes: 1,
		},
		{
			name: "segunda renovacao parte do vencimento atual",
			e: func(t *testing.T) Emprestimo {
				return reidratadoParaRenovar(t, inicioBase, dataComoDATE(2026, 4, 7), time.Time{}, 1)
			},
			ref: time.Date(2026, 3, 11, 10, 0, 0, 0, sp), wantA: 2026, wantM: 4, wantD: 21, wantRenovacoes: 2,
		},
		{
			// A base é o vencimento guardado (30/03), não início+14 nem ref.
			name: "base e o vencimento guardado, nao o inicio nem ref",
			e: func(t *testing.T) Emprestimo {
				return reidratadoParaRenovar(t, inicioBase, dataComoDATE(2026, 3, 30), time.Time{}, 0)
			},
			ref: time.Date(2026, 3, 11, 10, 0, 0, 0, sp), wantA: 2026, wantM: 4, wantD: 13, wantRenovacoes: 1,
		},
		{
			name: "virada de ano",
			e: func(t *testing.T) Emprestimo {
				return reidratadoParaRenovar(t, time.Date(2026, 12, 11, 9, 0, 0, 0, sp), dataComoDATE(2026, 12, 25), time.Time{}, 0)
			},
			ref: time.Date(2026, 12, 20, 9, 0, 0, 0, sp), wantA: 2027, wantM: 1, wantD: 8, wantRenovacoes: 1,
		},
		{
			name: "fevereiro de ano bissexto",
			e: func(t *testing.T) Emprestimo {
				return reidratadoParaRenovar(t, time.Date(2028, 2, 6, 9, 0, 0, 0, sp), dataComoDATE(2028, 2, 20), time.Time{}, 0)
			},
			ref: time.Date(2028, 2, 20, 23, 0, 0, 0, sp), wantA: 2028, wantM: 3, wantD: 5, wantRenovacoes: 1,
		},
		{
			// O vencimento novo, 04/11/2018, é o dia em que o horário de
			// verão começou e não houve 00:00 em SP.
			name: "vencimento novo em dia sem 00:00 em SP (04/11/2018)",
			e: func(t *testing.T) Emprestimo {
				return reidratadoParaRenovar(t, time.Date(2018, 10, 7, 9, 0, 0, 0, sp), dataComoDATE(2018, 10, 21), time.Time{}, 0)
			},
			ref: time.Date(2018, 10, 21, 23, 59, 0, 0, sp), wantA: 2018, wantM: 11, wantD: 4, wantRenovacoes: 1,
		},
		{
			name: "atravessa o fim do horario de verao (17/02/2019)",
			e: func(t *testing.T) Emprestimo {
				return reidratadoParaRenovar(t, time.Date(2019, 1, 20, 9, 0, 0, 0, sp), dataComoDATE(2019, 2, 3), time.Time{}, 0)
			},
			ref: time.Date(2019, 2, 3, 23, 30, 0, 0, sp), wantA: 2019, wantM: 2, wantD: 17, wantRenovacoes: 1,
		},
		{
			name: "vencimento no fim do horario de verao (17/02/2019), ref 23:59 do dia",
			e: func(t *testing.T) Emprestimo {
				return reidratadoParaRenovar(t, time.Date(2019, 2, 3, 9, 0, 0, 0, sp), dataComoDATE(2019, 2, 17), time.Time{}, 1)
			},
			ref: time.Date(2019, 2, 17, 23, 59, 0, 0, sp), wantA: 2019, wantM: 3, wantD: 3, wantRenovacoes: 2,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := tt.e(t)
			antes := e

			if err := e.Renovar(tt.ref); err != nil {
				t.Fatalf("Renovar(%v) erro inesperado: %v", tt.ref, err)
			}
			checarVencimento(t, e.Vencimento(), tt.wantA, tt.wantM, tt.wantD)
			if got := e.Vencimento().Location().String(); got != "America/Sao_Paulo" {
				t.Errorf("Vencimento().Location() = %q, want America/Sao_Paulo", got)
			}
			want := time.Date(tt.wantA, tt.wantM, tt.wantD, 12, 0, 0, 0, sp)
			if !e.Vencimento().Equal(want) {
				t.Errorf("Vencimento() = %v, want %v", e.Vencimento(), want)
			}
			if e.Renovacoes() != tt.wantRenovacoes {
				t.Errorf("Renovacoes() = %d, want %d", e.Renovacoes(), tt.wantRenovacoes)
			}
			// O resto não muda.
			if e.ID() != antes.ID() || e.LivroID() != antes.LivroID() || e.UsuarioID() != antes.UsuarioID() {
				t.Errorf("ID/LivroID/UsuarioID mudou: %d/%d/%d, want %d/%d/%d",
					e.ID(), e.LivroID(), e.UsuarioID(), antes.ID(), antes.LivroID(), antes.UsuarioID())
			}
			if e.Inicio() != antes.Inicio() {
				t.Errorf("Inicio() = %v, want %v (inalterado)", e.Inicio(), antes.Inicio())
			}
			if !e.Aberto() || !e.Devolucao().IsZero() {
				t.Errorf("Aberto/Devolucao = %v/%v, want true/zero", e.Aberto(), e.Devolucao())
			}
		})
	}
}

// Duas renovações levam o vencimento a início + 42 dias; a terceira é recusada
// sem alterar o empréstimo.
func TestEmprestimoRenovarAteOLimite(t *testing.T) {
	sp := saoPaulo(t)
	e := novoParaRenovar(t)

	passos := []struct {
		ref            time.Time
		wantErr        error
		wantD          int
		wantM          time.Month
		wantRenovacoes int
	}{
		{ref: time.Date(2026, 3, 24, 23, 0, 0, 0, sp), wantM: 4, wantD: 7, wantRenovacoes: 1},
		{ref: time.Date(2026, 4, 7, 23, 0, 0, 0, sp), wantM: 4, wantD: 21, wantRenovacoes: 2},
		{ref: time.Date(2026, 4, 8, 9, 0, 0, 0, sp), wantErr: ErrLimiteRenovacoes, wantM: 4, wantD: 21, wantRenovacoes: 2},
		{ref: time.Date(2026, 4, 9, 9, 0, 0, 0, sp), wantErr: ErrLimiteRenovacoes, wantM: 4, wantD: 21, wantRenovacoes: 2},
	}
	for i, p := range passos {
		err := e.Renovar(p.ref)
		if !errors.Is(err, p.wantErr) {
			t.Fatalf("passo %d: Renovar(%v) erro = %v, want %v", i+1, p.ref, err, p.wantErr)
		}
		checarVencimento(t, e.Vencimento(), 2026, p.wantM, p.wantD)
		if e.Renovacoes() != p.wantRenovacoes {
			t.Errorf("passo %d: Renovacoes() = %d, want %d", i+1, e.Renovacoes(), p.wantRenovacoes)
		}
	}
	if e.Renovacoes() > LimiteRenovacoes {
		t.Errorf("Renovacoes() = %d passou do limite %d", e.Renovacoes(), LimiteRenovacoes)
	}
}

func TestEmprestimoRenovarErros(t *testing.T) {
	sp := saoPaulo(t)
	inicio := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)
	venc := dataComoDATE(2026, 3, 24)
	dev := time.Date(2026, 3, 20, 10, 0, 0, 0, sp)
	noPrazo := time.Date(2026, 3, 20, 10, 0, 0, 0, sp)
	vencido := time.Date(2026, 3, 25, 0, 0, 0, 0, sp)

	aberto := func(n int) func(t *testing.T) Emprestimo {
		return func(t *testing.T) Emprestimo { return reidratadoParaRenovar(t, inicio, venc, time.Time{}, n) }
	}
	fechado := func(n int) func(t *testing.T) Emprestimo {
		return func(t *testing.T) Emprestimo { return reidratadoParaRenovar(t, inicio, venc, dev, n) }
	}
	vazio := func(*testing.T) Emprestimo { return Emprestimo{} }

	tests := []struct {
		name    string
		e       func(t *testing.T) Emprestimo
		ref     time.Time
		wantErr error
	}{
		{name: "Emprestimo{}", e: vazio, ref: noPrazo, wantErr: ErrEmprestimoInvalido},
		{name: "ordem: Emprestimo{} antes de ref zero", e: vazio, ref: time.Time{}, wantErr: ErrEmprestimoInvalido},
		{name: "devolvido", e: fechado(0), ref: noPrazo, wantErr: ErrEmprestimoJaDevolvido},
		{name: "ordem: devolvido antes de ref zero", e: fechado(0), ref: time.Time{}, wantErr: ErrEmprestimoJaDevolvido},
		{name: "ordem: devolvido antes de vencido", e: fechado(0), ref: vencido, wantErr: ErrEmprestimoJaDevolvido},
		{name: "ordem: devolvido antes do limite", e: fechado(2), ref: noPrazo, wantErr: ErrEmprestimoJaDevolvido},
		{name: "ref zero", e: aberto(0), ref: time.Time{}, wantErr: ErrReferenciaZero},
		{name: "ordem: ref zero antes do limite", e: aberto(2), ref: time.Time{}, wantErr: ErrReferenciaZero},
		{name: "vencido: 00:00 SP do dia seguinte", e: aberto(0), ref: vencido, wantErr: ErrEmprestimoVencido},
		{name: "vencido: 25/03 03:00 UTC (00:00 SP)", e: aberto(0), ref: time.Date(2026, 3, 25, 3, 0, 0, 0, time.UTC), wantErr: ErrEmprestimoVencido},
		{name: "vencido: muitos dias depois", e: aberto(1), ref: time.Date(2026, 6, 1, 10, 0, 0, 0, sp), wantErr: ErrEmprestimoVencido},
		{name: "ordem: vencido antes do limite", e: aberto(2), ref: vencido, wantErr: ErrEmprestimoVencido},
		{name: "limite atingido", e: aberto(2), ref: noPrazo, wantErr: ErrLimiteRenovacoes},
		{name: "limite atingido no dia do vencimento", e: aberto(2), ref: time.Date(2026, 3, 24, 23, 59, 0, 0, sp), wantErr: ErrLimiteRenovacoes},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e := tt.e(t)
			antes := e
			err := e.Renovar(tt.ref)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("Renovar(%v) erro = %v, want %v", tt.ref, err, tt.wantErr)
			}
			if e != antes {
				t.Errorf("Renovar com erro alterou o empréstimo: %+v, want %+v", e, antes)
			}
		})
	}
}

// Vencido para a renovação é a mesma fronteira em que a multa começa a contar:
// dia civil SP posterior ao do vencimento.
func TestEmprestimoRenovarMesmaFronteiraDaMulta(t *testing.T) {
	sp := saoPaulo(t)
	inicio := time.Date(2018, 10, 7, 9, 0, 0, 0, sp)
	venc := dataComoDATE(2018, 10, 21)

	for _, ref := range []time.Time{
		time.Date(2018, 10, 21, 1, 0, 0, 0, sp),
		time.Date(2018, 10, 21, 23, 59, 59, 0, sp),
		time.Date(2018, 10, 22, 0, 0, 0, 0, sp),
		time.Date(2018, 10, 22, 2, 59, 0, 0, time.UTC), // 21/10 23:59 SP (UTC-3 antes de 04/11/2018)
		time.Date(2018, 10, 22, 3, 0, 0, 0, time.UTC),
		time.Date(2018, 10, 20, 23, 59, 0, 0, sp),
	} {
		t.Run(ref.String(), func(t *testing.T) {
			e := reidratadoParaRenovar(t, inicio, venc, time.Time{}, 0)
			multa, err := e.Multa(ref)
			if err != nil {
				t.Fatalf("Multa erro inesperado: %v", err)
			}
			err = e.Renovar(ref)
			temMulta := !multa.IsZero()
			if temMulta != errors.Is(err, ErrEmprestimoVencido) {
				t.Errorf("ref %v: multa %s, Renovar erro %v; vencido deve coincidir com multa > 0", ref, multa, err)
			}
			if !temMulta && err != nil {
				t.Errorf("ref %v: Renovar erro inesperado %v", ref, err)
			}
		})
	}
}

// A multa passa a usar o vencimento renovado.
func TestEmprestimoRenovarMultaUsaVencimentoNovo(t *testing.T) {
	sp := saoPaulo(t)
	e := novoParaRenovar(t)
	if err := e.Renovar(time.Date(2026, 3, 24, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Renovar erro inesperado: %v", err)
	}
	tests := []struct {
		name string
		ref  time.Time
		want decimal.Decimal
	}{
		{name: "dia seguinte ao vencimento antigo", ref: time.Date(2026, 3, 25, 10, 0, 0, 0, sp), want: decimal.Zero},
		{name: "no vencimento novo", ref: time.Date(2026, 4, 7, 23, 0, 0, 0, sp), want: decimal.Zero},
		{name: "um dia depois do vencimento novo", ref: time.Date(2026, 4, 8, 10, 0, 0, 0, sp), want: decimal.NewFromInt(1)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := e.Multa(tt.ref)
			if err != nil {
				t.Fatalf("Multa erro inesperado: %v", err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("Multa(%v) = %s, want %s", tt.ref, got, tt.want)
			}
		})
	}
}

// O resultado de Renovar sobrevive à ida e volta pela reidratação.
func TestEmprestimoRenovarIdaEVolta(t *testing.T) {
	sp := saoPaulo(t)
	e := novoParaRenovar(t)
	if err := e.Renovar(time.Date(2026, 3, 12, 10, 0, 0, 0, sp)); err != nil {
		t.Fatalf("Renovar erro inesperado: %v", err)
	}
	got, err := ReidratarEmprestimo(5, e.LivroID(), e.UsuarioID(), e.Inicio(), vencimentoComoDATE(t, e.Vencimento()), e.Devolucao(), e.Renovacoes())
	if err != nil {
		t.Fatalf("ReidratarEmprestimo erro inesperado: %v", err)
	}
	want := e
	want.id = 5
	checarMesmoEmprestimo(t, got, want)
}

func FuzzEmprestimoRenovar(f *testing.F) {
	sp := saoPaulo(f)
	for _, s := range []struct {
		inicio time.Time
		refSeg int64
		n      uint8
	}{
		{inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), refSeg: 0},
		{inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), refSeg: 14*86400 + 13*3600 + 59*60, n: 1},
		{inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), refSeg: 14*86400 + 14*3600, n: 2},
		{inicio: time.Date(2018, 10, 7, 9, 0, 0, 0, sp), refSeg: 14 * 86400, n: 3},
		{inicio: time.Date(2019, 2, 3, 9, 0, 0, 0, sp), refSeg: 15 * 86400, n: 5},
		{inicio: time.Date(2026, 12, 20, 9, 0, 0, 0, sp), refSeg: -86400, n: 1},
	} {
		f.Add(s.inicio.Unix(), s.refSeg, s.n)
	}
	f.Add(fuzzMin, int64(0), uint8(2))
	f.Add(fuzzMax-1, int64(100*86400), uint8(7))

	const maxRefSeg = 120 * 86400

	f.Fuzz(func(t *testing.T, u, refSeg int64, n uint8) {
		inicio := time.Unix(limitarUnix(u), 0).UTC()
		e, err := NovoEmprestimo(1, 1, inicio)
		if err != nil {
			t.Fatalf("NovoEmprestimo erro inesperado: %v", err)
		}
		ref := inicio.Add(time.Duration(refSeg%maxRefSeg) * time.Second)

		for i := 0; i < int(n%8); i++ {
			antes := e
			err := e.Renovar(ref)

			// Oráculo independente: data civil SP de ref posterior à do vencimento.
			ra, rm, rd := ref.In(sp).Date()
			va, vm, vd := antes.vencimento.In(sp).Date()
			refDia := time.Date(ra, rm, rd, 0, 0, 0, 0, time.UTC)
			vencDia := time.Date(va, vm, vd, 0, 0, 0, 0, time.UTC)
			vencidoOraculo := refDia.After(vencDia)

			switch {
			case vencidoOraculo:
				if !errors.Is(err, ErrEmprestimoVencido) {
					t.Fatalf("ref %v venc %v: erro = %v, want %v", ref, antes.vencimento, err, ErrEmprestimoVencido)
				}
			case antes.Renovacoes() >= LimiteRenovacoes:
				if !errors.Is(err, ErrLimiteRenovacoes) {
					t.Fatalf("ref %v renov %d: erro = %v, want %v", ref, antes.Renovacoes(), err, ErrLimiteRenovacoes)
				}
			default:
				if err != nil {
					t.Fatalf("ref %v venc %v renov %d: erro inesperado %v", ref, antes.vencimento, antes.Renovacoes(), err)
				}
			}

			if err != nil {
				if e != antes {
					t.Fatalf("erro %v alterou o empréstimo", err)
				}
				continue
			}
			wa, wm, wd := time.Date(va, vm, vd, 12, 0, 0, 0, time.UTC).AddDate(0, 0, 14).Date()
			v := e.Vencimento().In(sp)
			ga, gm, gd := v.Date()
			if ga != wa || gm != wm || gd != wd || v.Hour() != 12 || v.Minute() != 0 {
				t.Errorf("venc %v -> %v, want %04d-%02d-%02d 12:00 SP", antes.vencimento, v, wa, wm, wd)
			}
			if e.Renovacoes() != antes.Renovacoes()+1 {
				t.Errorf("Renovacoes() = %d, want %d", e.Renovacoes(), antes.Renovacoes()+1)
			}
			if e.Inicio() != antes.Inicio() || !e.Aberto() || e.ID() != antes.ID() {
				t.Errorf("Renovar alterou campos fora de vencimento/renovacoes")
			}
		}
		if e.Renovacoes() < 0 || e.Renovacoes() > LimiteRenovacoes {
			t.Fatalf("Renovacoes() = %d fora de 0..%d", e.Renovacoes(), LimiteRenovacoes)
		}
	})
}
