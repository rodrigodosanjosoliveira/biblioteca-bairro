package domain

import (
	"errors"
	"testing"
	"time"

	"github.com/shopspring/decimal"
)

func saoPaulo(t testing.TB) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatalf("carregar America/Sao_Paulo: %v", err)
	}
	return loc
}

func TestCalcularMulta(t *testing.T) {
	sp := saoPaulo(t)
	d := func(ano int, mes time.Month, dia, hora, min int) time.Time {
		return time.Date(ano, mes, dia, hora, min, 0, 0, sp)
	}

	tests := []struct {
		name       string
		vencimento time.Time
		devolucao  time.Time
		want       decimal.Decimal
	}{
		{name: "devolucao antes do vencimento", vencimento: d(2026, 3, 10, 0, 0), devolucao: d(2026, 3, 5, 10, 0), want: decimal.Zero},
		{name: "mesmo instante", vencimento: d(2026, 3, 10, 12, 0), devolucao: d(2026, 3, 10, 12, 0), want: decimal.Zero},
		{name: "mesmo dia as 23:59 SP", vencimento: d(2026, 3, 10, 0, 0), devolucao: d(2026, 3, 10, 23, 59), want: decimal.Zero},
		{name: "1 dia de atraso", vencimento: d(2026, 3, 10, 0, 0), devolucao: d(2026, 3, 11, 0, 0), want: decimal.NewFromInt(1)},
		{name: "1 dia por data civil com menos de 24h", vencimento: d(2026, 3, 10, 18, 0), devolucao: d(2026, 3, 11, 8, 0), want: decimal.NewFromInt(1)},
		{name: "1 dia por data civil com mais de 24h", vencimento: d(2026, 3, 10, 0, 1), devolucao: d(2026, 3, 11, 23, 59), want: decimal.NewFromInt(1)},
		{name: "19 dias", vencimento: d(2026, 3, 10, 0, 0), devolucao: d(2026, 3, 29, 0, 0), want: decimal.NewFromInt(19)},
		{name: "20 dias atinge o teto", vencimento: d(2026, 3, 10, 0, 0), devolucao: d(2026, 3, 30, 0, 0), want: decimal.NewFromInt(20)},
		{name: "21 dias limitado ao teto", vencimento: d(2026, 3, 10, 0, 0), devolucao: d(2026, 3, 31, 0, 0), want: decimal.NewFromInt(20)},
		{name: "atraso de ~1000 anos limitado ao teto", vencimento: d(2026, 3, 10, 0, 0), devolucao: d(3026, 3, 10, 0, 0), want: decimal.NewFromInt(20)},
		{name: "virada de mes", vencimento: d(2026, 1, 31, 0, 0), devolucao: d(2026, 2, 1, 9, 0), want: decimal.NewFromInt(1)},
		{name: "virada de ano", vencimento: d(2025, 12, 31, 0, 0), devolucao: d(2026, 1, 2, 9, 0), want: decimal.NewFromInt(2)},
		{name: "28/02 a 01/03 em ano bissexto", vencimento: d(2024, 2, 28, 0, 0), devolucao: d(2024, 3, 1, 0, 0), want: decimal.NewFromInt(2)},
		{name: "28/02 a 01/03 em ano comum", vencimento: d(2026, 2, 28, 0, 0), devolucao: d(2026, 3, 1, 0, 0), want: decimal.NewFromInt(1)},
		{name: "vencimento em 29/02/2024", vencimento: d(2024, 2, 29, 0, 0), devolucao: d(2024, 3, 5, 0, 0), want: decimal.NewFromInt(5)},
		{
			name:       "devolucao em UTC no dia seguinte mas mesmo dia em SP",
			vencimento: d(2026, 3, 9, 0, 0),
			devolucao:  time.Date(2026, 3, 10, 2, 30, 0, 0, time.UTC), // 09/03 23:30 SP
			want:       decimal.Zero,
		},
		{
			name:       "devolucao em UTC no dia seguinte tambem em SP",
			vencimento: d(2026, 3, 9, 0, 0),
			devolucao:  time.Date(2026, 3, 10, 3, 30, 0, 0, time.UTC), // 10/03 00:30 SP
			want:       decimal.NewFromInt(1),
		},
		{
			name:       "vencimento em UTC no dia seguinte mas mesmo dia em SP",
			vencimento: time.Date(2026, 3, 10, 2, 0, 0, 0, time.UTC), // 09/03 23:00 SP
			devolucao:  d(2026, 3, 10, 8, 0),
			want:       decimal.NewFromInt(1),
		},
		{
			name:       "inicio do horario de verao 2018 (dia sem 00:00)",
			vencimento: d(2018, 11, 3, 0, 0),
			devolucao:  d(2018, 11, 4, 1, 30), // BRST, -02:00
			want:       decimal.NewFromInt(1),
		},
		{
			name:       "fim do horario de verao 2019 (dia de 25h)",
			vencimento: d(2019, 2, 16, 0, 0),
			devolucao:  d(2019, 2, 17, 0, 30),
			want:       decimal.NewFromInt(1),
		},
		{
			name:       "fim do horario de verao 2019, devolucao na hora repetida",
			vencimento: d(2019, 2, 16, 0, 0),
			devolucao:  d(2019, 2, 16, 23, 30),
			want:       decimal.Zero,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalcularMulta(tt.vencimento, tt.devolucao)
			if err != nil {
				t.Fatalf("CalcularMulta(%v, %v) erro inesperado: %v", tt.vencimento, tt.devolucao, err)
			}
			if !got.Equal(tt.want) {
				t.Errorf("CalcularMulta(%v, %v) = %s, want %s", tt.vencimento, tt.devolucao, got, tt.want)
			}
		})
	}
}

func TestCalcularMultaIndependeDoFusoDaEntrada(t *testing.T) {
	sp := saoPaulo(t)

	tests := []struct {
		name       string
		vencimento time.Time
		devolucao  time.Time
		want       decimal.Decimal
	}{
		{
			name:       "09/03 23:30 SP nao gera multa",
			vencimento: time.Date(2026, 3, 9, 0, 0, 0, 0, sp),
			devolucao:  time.Date(2026, 3, 10, 2, 30, 0, 0, time.UTC),
			want:       decimal.Zero,
		},
		{
			name:       "3 dias de atraso",
			vencimento: time.Date(2026, 3, 9, 22, 0, 0, 0, sp),
			devolucao:  time.Date(2026, 3, 12, 1, 0, 0, 0, sp),
			want:       decimal.NewFromInt(3),
		},
	}
	zonas := []*time.Location{
		sp,
		time.UTC,
		time.FixedZone("UTC-3", -3*60*60),
		time.FixedZone("UTC+9", 9*60*60),
		time.FixedZone("UTC-11", -11*60*60),
	}
	for _, tt := range tests {
		for _, zv := range zonas {
			for _, zd := range zonas {
				t.Run(tt.name+"/"+zv.String()+"/"+zd.String(), func(t *testing.T) {
					venc := tt.vencimento.In(zv)
					dev := tt.devolucao.In(zd)
					got, err := CalcularMulta(venc, dev)
					if err != nil {
						t.Fatalf("CalcularMulta(%v, %v) erro inesperado: %v", venc, dev, err)
					}
					if !got.Equal(tt.want) {
						t.Errorf("CalcularMulta(%v, %v) = %s, want %s", venc, dev, got, tt.want)
					}
				})
			}
		}
	}
}

func TestCalcularMultaErros(t *testing.T) {
	valida := time.Date(2026, 3, 10, 12, 0, 0, 0, time.UTC)

	tests := []struct {
		name       string
		vencimento time.Time
		devolucao  time.Time
		wantErr    error
	}{
		{name: "vencimento zero", vencimento: time.Time{}, devolucao: valida, wantErr: ErrVencimentoZero},
		{name: "devolucao zero", vencimento: valida, devolucao: time.Time{}, wantErr: ErrDevolucaoZero},
		{name: "ambos zero valida vencimento primeiro", vencimento: time.Time{}, devolucao: time.Time{}, wantErr: ErrVencimentoZero},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := CalcularMulta(tt.vencimento, tt.devolucao)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("CalcularMulta() erro = %v, want %v", err, tt.wantErr)
			}
			if !got.Equal(decimal.Zero) {
				t.Errorf("CalcularMulta() em erro = %s, want 0", got)
			}
		})
	}
}

func TestErrosSentinelaMensagens(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "vencimento", err: ErrVencimentoZero, want: "multa: data de vencimento zero"},
		{name: "devolucao", err: ErrDevolucaoZero, want: "multa: data de devolução zero"},
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

// Intervalo de instantes usados no fuzz: 1900-01-01 a 3000-01-01 UTC.
var (
	fuzzMin = time.Date(1900, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
	fuzzMax = time.Date(3000, 1, 1, 0, 0, 0, 0, time.UTC).Unix()
)

func limitarUnix(u int64) int64 {
	span := fuzzMax - fuzzMin
	m := u % span
	if m < 0 {
		m += span
	}
	return fuzzMin + m
}

func FuzzCalcularMulta(f *testing.F) {
	sp := saoPaulo(f)
	seed := func(v, d time.Time) { f.Add(v.Unix(), d.Unix()) }

	seed(time.Date(2026, 3, 10, 0, 0, 0, 0, sp), time.Date(2026, 3, 5, 0, 0, 0, 0, sp))
	seed(time.Date(2026, 3, 10, 0, 0, 0, 0, sp), time.Date(2026, 3, 10, 23, 59, 0, 0, sp))
	seed(time.Date(2026, 3, 10, 0, 0, 0, 0, sp), time.Date(2026, 3, 11, 0, 0, 0, 0, sp))
	seed(time.Date(2026, 3, 10, 0, 0, 0, 0, sp), time.Date(2026, 3, 29, 0, 0, 0, 0, sp))
	seed(time.Date(2026, 3, 10, 0, 0, 0, 0, sp), time.Date(2026, 3, 30, 0, 0, 0, 0, sp))
	seed(time.Date(2026, 3, 10, 0, 0, 0, 0, sp), time.Date(2026, 3, 31, 0, 0, 0, 0, sp))
	seed(time.Date(2024, 2, 28, 0, 0, 0, 0, sp), time.Date(2024, 3, 1, 0, 0, 0, 0, sp))
	seed(time.Date(2026, 3, 9, 0, 0, 0, 0, sp), time.Date(2026, 3, 10, 2, 30, 0, 0, time.UTC))
	seed(time.Date(2018, 11, 3, 0, 0, 0, 0, sp), time.Date(2018, 11, 4, 1, 30, 0, 0, sp))
	seed(time.Date(2019, 2, 16, 0, 0, 0, 0, sp), time.Date(2019, 2, 17, 0, 30, 0, 0, sp))
	f.Add(fuzzMin, fuzzMax-1)
	f.Add(fuzzMax-1, fuzzMin)

	vinte := decimal.NewFromInt(20)

	f.Fuzz(func(t *testing.T, vencUnix, devUnix int64) {
		venc := time.Unix(limitarUnix(vencUnix), 0).UTC()
		dev := time.Unix(limitarUnix(devUnix), 0).UTC()

		got, err := CalcularMulta(venc, dev)
		if err != nil {
			t.Fatalf("CalcularMulta(%v, %v) erro inesperado: %v", venc, dev, err)
		}
		if got.LessThan(decimal.Zero) || got.GreaterThan(vinte) {
			t.Errorf("CalcularMulta(%v, %v) = %s, fora de [0, 20]", venc, dev, got)
		}
		if !got.IsInteger() {
			t.Errorf("CalcularMulta(%v, %v) = %s, nao inteiro", venc, dev, got)
		}
		if dev.Before(venc) && !got.Equal(decimal.Zero) {
			t.Errorf("CalcularMulta(%v, %v) = %s, devolucao antes do vencimento deve ser 0", venc, dev, got)
		}
		if want := oraculoMulta(venc, dev, sp); !got.Equal(want) {
			t.Errorf("CalcularMulta(%v, %v) = %s, oraculo = %s", venc, dev, got, want)
		}
	})
}

// oraculoMulta calcula a multa esperada de forma independente da produção:
// toma a data civil em São Paulo de cada instante e conta dias andando de um
// em um com AddDate sobre a data civil (construída em UTC ao meio-dia), até no
// máximo 21 passos, limitando o resultado a 20.
func oraculoMulta(venc, dev time.Time, sp *time.Location) decimal.Decimal {
	civil := func(t time.Time) time.Time {
		a, m, d := t.In(sp).Date()
		return time.Date(a, m, d, 12, 0, 0, 0, time.UTC)
	}
	cv, cd := civil(venc), civil(dev)
	dias := int64(0)
	for d := cv; dias < 21 && d.Before(cd); d = d.AddDate(0, 0, 1) {
		dias++
	}
	return decimal.NewFromInt(min(dias, 20))
}
