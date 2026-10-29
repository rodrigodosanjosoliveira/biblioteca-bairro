package domain

import (
	"errors"
	"testing"
	"time"
)

// dataComoDATE devolve a meia-noite UTC da data civil a/m/d, que é como o
// pgx entrega uma coluna DATE.
func dataComoDATE(a int, m time.Month, d int) time.Time {
	return time.Date(a, m, d, 0, 0, 0, 0, time.UTC)
}

// vencimentoComoDATE converte um vencimento do domínio (12:00 SP) para a forma
// em que ele volta de uma coluna DATE: meia-noite UTC da data civil em SP.
func vencimentoComoDATE(t testing.TB, v time.Time) time.Time {
	t.Helper()
	a, m, d := v.In(saoPaulo(t)).Date()
	return dataComoDATE(a, m, d)
}

// checarMesmoEmprestimo confere campo a campo que got reproduz want.
func checarMesmoEmprestimo(t *testing.T, got, want Emprestimo) {
	t.Helper()
	if got.ID() != want.ID() || got.LivroID() != want.LivroID() || got.UsuarioID() != want.UsuarioID() {
		t.Errorf("ID/LivroID/UsuarioID = %d/%d/%d, want %d/%d/%d",
			got.ID(), got.LivroID(), got.UsuarioID(), want.ID(), want.LivroID(), want.UsuarioID())
	}
	if !got.Inicio().Equal(want.Inicio()) || got.Inicio().Location() != want.Inicio().Location() {
		t.Errorf("Inicio() = %v, want %v (mesmo instante e mesmo fuso)", got.Inicio(), want.Inicio())
	}
	if !got.Vencimento().Equal(want.Vencimento()) {
		t.Errorf("Vencimento() = %v, want %v", got.Vencimento(), want.Vencimento())
	}
	if !got.Devolucao().Equal(want.Devolucao()) || got.Devolucao().Location() != want.Devolucao().Location() {
		t.Errorf("Devolucao() = %v, want %v (mesmo instante e mesmo fuso)", got.Devolucao(), want.Devolucao())
	}
	if got.Aberto() != want.Aberto() {
		t.Errorf("Aberto() = %v, want %v", got.Aberto(), want.Aberto())
	}
}

func TestErrosReidratacaoMensagens(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{name: "id do emprestimo", err: ErrEmprestimoIDInvalido, want: "emprestimo: id inválido"},
		{name: "vencimento zero", err: ErrVencimentoEmprestimoZero, want: "emprestimo: data de vencimento zero"},
		{name: "vencimento nao posterior", err: ErrVencimentoNaoPosteriorAoInicio, want: "emprestimo: vencimento não posterior ao início"},
		{name: "emprestimo invalido", err: ErrEmprestimoInvalido, want: "emprestimo: inválido"},
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

// O vencimento zero do empréstimo tem sentinela própria; não reusa a da multa.
func TestErrVencimentoEmprestimoZeroDistintoDaMulta(t *testing.T) {
	if errors.Is(ErrVencimentoEmprestimoZero, ErrVencimentoZero) || errors.Is(ErrVencimentoZero, ErrVencimentoEmprestimoZero) {
		t.Error("ErrVencimentoEmprestimoZero e ErrVencimentoZero não podem ser o mesmo erro")
	}
}

func TestReidratarEmprestimoErros(t *testing.T) {
	sp := saoPaulo(t)
	inicio := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)
	venc := time.Date(2026, 3, 24, 12, 0, 0, 0, sp)
	dev := time.Date(2026, 3, 20, 15, 0, 0, 0, sp)

	tests := []struct {
		name                      string
		id, livroID, usuarioID    int64
		inicio, vencimento, devol time.Time
		wantErr                   error
	}{
		{name: "id zero", id: 0, livroID: 1, usuarioID: 1, inicio: inicio, vencimento: venc, wantErr: ErrEmprestimoIDInvalido},
		{name: "id negativo", id: -3, livroID: 1, usuarioID: 1, inicio: inicio, vencimento: venc, wantErr: ErrEmprestimoIDInvalido},
		{name: "livro id zero", id: 1, livroID: 0, usuarioID: 1, inicio: inicio, vencimento: venc, wantErr: ErrLivroIDInvalido},
		{name: "livro id negativo", id: 1, livroID: -1, usuarioID: 1, inicio: inicio, vencimento: venc, wantErr: ErrLivroIDInvalido},
		{name: "usuario id zero", id: 1, livroID: 1, usuarioID: 0, inicio: inicio, vencimento: venc, wantErr: ErrUsuarioIDInvalido},
		{name: "usuario id negativo", id: 1, livroID: 1, usuarioID: -9, inicio: inicio, vencimento: venc, wantErr: ErrUsuarioIDInvalido},
		{name: "inicio zero", id: 1, livroID: 1, usuarioID: 1, inicio: time.Time{}, vencimento: venc, wantErr: ErrInicioZero},
		{name: "vencimento zero", id: 1, livroID: 1, usuarioID: 1, inicio: inicio, vencimento: time.Time{}, wantErr: ErrVencimentoEmprestimoZero},
		{name: "vencimento no mesmo dia civil do inicio", id: 1, livroID: 1, usuarioID: 1, inicio: inicio, vencimento: time.Date(2026, 3, 10, 12, 0, 0, 0, sp), wantErr: ErrVencimentoNaoPosteriorAoInicio},
		{name: "vencimento DATE no mesmo dia civil do inicio", id: 1, livroID: 1, usuarioID: 1, inicio: inicio, vencimento: dataComoDATE(2026, 3, 10), wantErr: ErrVencimentoNaoPosteriorAoInicio},
		{name: "vencimento antes do inicio", id: 1, livroID: 1, usuarioID: 1, inicio: inicio, vencimento: time.Date(2026, 3, 1, 12, 0, 0, 0, sp), wantErr: ErrVencimentoNaoPosteriorAoInicio},
		{
			// A data civil do DATE (10/03) é igual à do início em SP (10/03).
			name: "vencimento DATE 10/03 com inicio 10/03 23:30 SP", id: 1, livroID: 1, usuarioID: 1,
			inicio: time.Date(2026, 3, 10, 23, 30, 0, 0, sp), vencimento: dataComoDATE(2026, 3, 10), wantErr: ErrVencimentoNaoPosteriorAoInicio,
		},
		{name: "devolucao em dia civil anterior ao inicio", id: 1, livroID: 1, usuarioID: 1, inicio: inicio, vencimento: venc, devol: time.Date(2026, 3, 9, 23, 59, 0, 0, sp), wantErr: ErrDevolucaoAntesDoInicio},
		{
			name: "devolucao no dia anterior em SP mesmo sendo depois em UTC", id: 1, livroID: 1, usuarioID: 1,
			inicio: time.Date(2026, 3, 10, 0, 30, 0, 0, sp), vencimento: venc,
			devol:   time.Date(2026, 3, 10, 2, 30, 0, 0, time.UTC), // 09/03 23:30 SP
			wantErr: ErrDevolucaoAntesDoInicio,
		},
		{name: "ordem: tudo invalido valida id primeiro", id: 0, livroID: 0, usuarioID: 0, inicio: time.Time{}, vencimento: time.Time{}, wantErr: ErrEmprestimoIDInvalido},
		{name: "ordem: livro antes de usuario", id: 1, livroID: 0, usuarioID: 0, inicio: inicio, vencimento: venc, wantErr: ErrLivroIDInvalido},
		{name: "ordem: usuario antes de inicio", id: 1, livroID: 1, usuarioID: 0, inicio: time.Time{}, vencimento: venc, wantErr: ErrUsuarioIDInvalido},
		{name: "ordem: inicio antes de vencimento", id: 1, livroID: 1, usuarioID: 1, inicio: time.Time{}, vencimento: time.Time{}, wantErr: ErrInicioZero},
		{name: "ordem: vencimento zero antes de devolucao", id: 1, livroID: 1, usuarioID: 1, inicio: inicio, vencimento: time.Time{}, devol: time.Date(2026, 3, 1, 10, 0, 0, 0, sp), wantErr: ErrVencimentoEmprestimoZero},
		{name: "ordem: vencimento nao posterior antes de devolucao", id: 1, livroID: 1, usuarioID: 1, inicio: inicio, vencimento: time.Date(2026, 3, 10, 12, 0, 0, 0, sp), devol: time.Date(2026, 3, 1, 10, 0, 0, 0, sp), wantErr: ErrVencimentoNaoPosteriorAoInicio},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := ReidratarEmprestimo(tt.id, tt.livroID, tt.usuarioID, tt.inicio, tt.vencimento, tt.devol)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("ReidratarEmprestimo erro = %v, want %v", err, tt.wantErr)
			}
			checarEmprestimoZero(t, got)
		})
	}

	// Sanidade do caso base: com os mesmos valores válidos não há erro.
	if _, err := ReidratarEmprestimo(1, 1, 1, inicio, venc, dev); err != nil {
		t.Fatalf("caso base válido erro inesperado: %v", err)
	}
}

func TestReidratarEmprestimoSucesso(t *testing.T) {
	sp := saoPaulo(t)
	utc9 := time.FixedZone("UTC+9", 9*60*60)

	tests := []struct {
		name       string
		inicio     time.Time
		vencimento time.Time
		devolucao  time.Time
		wantA      int
		wantM      time.Month
		wantD      int
		wantAberto bool
	}{
		{name: "aberto, vencimento 12:00 SP", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), vencimento: time.Date(2026, 3, 24, 12, 0, 0, 0, sp), wantA: 2026, wantM: 3, wantD: 24, wantAberto: true},
		{name: "aberto, vencimento como DATE em meia-noite UTC", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), vencimento: dataComoDATE(2026, 3, 24), wantA: 2026, wantM: 3, wantD: 24, wantAberto: true},
		{name: "DATE 04/11/2018 (dia sem 00:00 em SP)", inicio: time.Date(2018, 10, 21, 9, 0, 0, 0, sp), vencimento: dataComoDATE(2018, 11, 4), wantA: 2018, wantM: 11, wantD: 4, wantAberto: true},
		{name: "DATE 17/02/2019 (fim do horario de verao)", inicio: time.Date(2019, 2, 3, 9, 0, 0, 0, sp), vencimento: dataComoDATE(2019, 2, 17), wantA: 2019, wantM: 2, wantD: 17, wantAberto: true},
		{name: "DATE 21/10/2018 (inicio do horario de verao)", inicio: time.Date(2018, 10, 7, 9, 0, 0, 0, sp), vencimento: dataComoDATE(2018, 10, 21), wantA: 2018, wantM: 10, wantD: 21, wantAberto: true},
		{
			// Data civil no fuso do próprio valor, sem .In(SP): 24/03 00:00 em
			// UTC+9 é 23/03 12:00 em SP, mas a data a guardar é 24/03.
			name: "data civil no fuso do proprio valor (UTC+9)", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp),
			vencimento: time.Date(2026, 3, 24, 0, 0, 0, 0, utc9), wantA: 2026, wantM: 3, wantD: 24, wantAberto: true,
		},
		{
			// 11/03 00:00 UTC é 10/03 21:00 SP; a data do DATE é 11/03, que é
			// posterior ao início (10/03 23:30 SP).
			name: "DATE 11/03 com inicio 10/03 23:30 SP", inicio: time.Date(2026, 3, 10, 23, 30, 0, 0, sp),
			vencimento: dataComoDATE(2026, 3, 11), wantA: 2026, wantM: 3, wantD: 11, wantAberto: true,
		},
		{name: "vencimento 1 dia depois (nao exige inicio+14)", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), vencimento: dataComoDATE(2026, 3, 11), wantA: 2026, wantM: 3, wantD: 11, wantAberto: true},
		{name: "vencimento 60 dias depois (nao exige inicio+14)", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), vencimento: dataComoDATE(2026, 5, 9), wantA: 2026, wantM: 5, wantD: 9, wantAberto: true},
		{name: "fechado, devolucao dias depois", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), vencimento: dataComoDATE(2026, 3, 24), devolucao: time.Date(2026, 3, 20, 15, 0, 0, 0, sp), wantA: 2026, wantM: 3, wantD: 24},
		{name: "fechado, devolucao no mesmo dia civil com hora anterior", inicio: time.Date(2026, 3, 10, 15, 0, 0, 0, sp), vencimento: dataComoDATE(2026, 3, 24), devolucao: time.Date(2026, 3, 10, 8, 0, 0, 0, sp), wantA: 2026, wantM: 3, wantD: 24},
		{name: "fechado, devolucao com atraso depois do vencimento", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), vencimento: dataComoDATE(2026, 3, 24), devolucao: time.Date(2026, 4, 30, 10, 0, 0, 0, utc9), wantA: 2026, wantM: 3, wantD: 24},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			e, err := ReidratarEmprestimo(17, 42, 7, tt.inicio, tt.vencimento, tt.devolucao)
			if err != nil {
				t.Fatalf("ReidratarEmprestimo erro inesperado: %v", err)
			}
			if e.ID() != 17 || e.LivroID() != 42 || e.UsuarioID() != 7 {
				t.Errorf("ID/LivroID/UsuarioID = %d/%d/%d, want 17/42/7", e.ID(), e.LivroID(), e.UsuarioID())
			}
			if !e.Inicio().Equal(tt.inicio) || e.Inicio().Location() != tt.inicio.Location() {
				t.Errorf("Inicio() = %v, want %v (guardado como recebido)", e.Inicio(), tt.inicio)
			}
			if !e.Devolucao().Equal(tt.devolucao) || e.Devolucao().Location() != tt.devolucao.Location() {
				t.Errorf("Devolucao() = %v, want %v (guardado como recebido)", e.Devolucao(), tt.devolucao)
			}
			if e.Aberto() != tt.wantAberto {
				t.Errorf("Aberto() = %v, want %v", e.Aberto(), tt.wantAberto)
			}
			checarVencimento(t, e.Vencimento(), tt.wantA, tt.wantM, tt.wantD)
			if got := e.Vencimento().Location().String(); got != "America/Sao_Paulo" {
				t.Errorf("Vencimento().Location() = %q, want America/Sao_Paulo", got)
			}
		})
	}
}

// Reidratar um vencimento já normalizado não o altera, e reidratar o
// resultado de novo dá o mesmo valor.
func TestReidratarEmprestimoNormalizacaoIdempotente(t *testing.T) {
	sp := saoPaulo(t)
	inicio := time.Date(2018, 10, 21, 9, 0, 0, 0, sp)

	for _, venc := range []time.Time{
		dataComoDATE(2018, 11, 4),
		time.Date(2018, 11, 4, 12, 0, 0, 0, sp),
		time.Date(2018, 11, 4, 23, 59, 0, 0, time.UTC),
	} {
		t.Run(venc.String(), func(t *testing.T) {
			e1, err := ReidratarEmprestimo(1, 1, 1, inicio, venc, time.Time{})
			if err != nil {
				t.Fatalf("primeira ReidratarEmprestimo erro inesperado: %v", err)
			}
			e2, err := ReidratarEmprestimo(1, 1, 1, inicio, e1.Vencimento(), time.Time{})
			if err != nil {
				t.Fatalf("segunda ReidratarEmprestimo erro inesperado: %v", err)
			}
			if !e2.Vencimento().Equal(e1.Vencimento()) {
				t.Errorf("normalização não idempotente: %v -> %v", e1.Vencimento(), e2.Vencimento())
			}
			checarVencimento(t, e2.Vencimento(), 2018, 11, 4)
		})
	}
}

// Ida e volta: NovoEmprestimo -> getters -> ReidratarEmprestimo reproduz o
// empréstimo, com o vencimento como veio do domínio e como volta do DATE.
func TestReidratarEmprestimoIdaEVolta(t *testing.T) {
	sp := saoPaulo(t)
	utc9 := time.FixedZone("UTC+9", 9*60*60)

	tests := []struct {
		name      string
		inicio    time.Time
		devolucao time.Time
	}{
		{name: "aberto, inicio SP", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)},
		{name: "aberto, inicio 23:30 SP", inicio: time.Date(2026, 3, 10, 23, 30, 0, 0, sp)},
		{name: "aberto, inicio UTC+9", inicio: time.Date(2026, 3, 11, 9, 0, 0, 0, utc9)},
		{name: "aberto, vencimento 04/11/2018", inicio: time.Date(2018, 10, 21, 9, 0, 0, 0, sp)},
		{name: "aberto, vencimento 17/02/2019", inicio: time.Date(2019, 2, 3, 9, 0, 0, 0, sp)},
		{name: "fechado", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), devolucao: time.Date(2026, 3, 30, 18, 0, 0, 0, time.UTC)},
		{name: "fechado no mesmo dia", inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp), devolucao: time.Date(2026, 3, 10, 9, 0, 0, 0, sp)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			orig, err := NovoEmprestimo(42, 7, tt.inicio)
			if err != nil {
				t.Fatalf("NovoEmprestimo erro inesperado: %v", err)
			}
			if !tt.devolucao.IsZero() {
				if err := orig.Devolver(tt.devolucao); err != nil {
					t.Fatalf("Devolver erro inesperado: %v", err)
				}
			}

			formatos := []struct {
				name string
				venc time.Time
			}{
				{name: "vencimento do dominio", venc: orig.Vencimento()},
				{name: "vencimento como DATE", venc: vencimentoComoDATE(t, orig.Vencimento())},
			}
			for _, f := range formatos {
				t.Run(f.name, func(t *testing.T) {
					got, err := ReidratarEmprestimo(99, orig.LivroID(), orig.UsuarioID(), orig.Inicio(), f.venc, orig.Devolucao())
					if err != nil {
						t.Fatalf("ReidratarEmprestimo erro inesperado: %v", err)
					}
					if got.ID() != 99 {
						t.Errorf("ID() = %d, want 99", got.ID())
					}
					want := orig
					want.id = 99
					checarMesmoEmprestimo(t, got, want)
				})
			}
		})
	}
}

func TestReidratarEmprestimoDevolver(t *testing.T) {
	sp := saoPaulo(t)
	inicio := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)

	aberto, err := ReidratarEmprestimo(5, 1, 1, inicio, dataComoDATE(2026, 3, 24), time.Time{})
	if err != nil {
		t.Fatalf("ReidratarEmprestimo erro inesperado: %v", err)
	}
	em := time.Date(2026, 3, 20, 10, 0, 0, 0, sp)
	if err := aberto.Devolver(em); err != nil {
		t.Fatalf("Devolver em reidratado aberto erro inesperado: %v", err)
	}
	if aberto.Aberto() || !aberto.Devolucao().Equal(em) || aberto.ID() != 5 {
		t.Errorf("depois de Devolver: Aberto=%v Devolucao=%v ID=%d, want false/%v/5", aberto.Aberto(), aberto.Devolucao(), aberto.ID(), em)
	}

	fechado, err := ReidratarEmprestimo(6, 1, 1, inicio, dataComoDATE(2026, 3, 24), em)
	if err != nil {
		t.Fatalf("ReidratarEmprestimo erro inesperado: %v", err)
	}
	if err := fechado.Devolver(time.Date(2026, 3, 21, 10, 0, 0, 0, sp)); !errors.Is(err, ErrEmprestimoJaDevolvido) {
		t.Errorf("Devolver em reidratado fechado erro = %v, want %v", err, ErrEmprestimoJaDevolvido)
	}
	if !fechado.Devolucao().Equal(em) {
		t.Errorf("Devolucao() = %v, want %v (inalterada)", fechado.Devolucao(), em)
	}
}

// Emprestimo{} não é empréstimo válido: Devolver recusa sem alterar estado, e
// essa checagem vem antes das demais.
func TestEmprestimoZeroDevolver(t *testing.T) {
	sp := saoPaulo(t)

	tests := []struct {
		name string
		em   time.Time
	}{
		{name: "data valida", em: time.Date(2026, 3, 20, 10, 0, 0, 0, sp)},
		{name: "ordem: antes de data zero", em: time.Time{}},
		{name: "data muito antiga", em: time.Date(1901, 1, 1, 10, 0, 0, 0, sp)},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var e Emprestimo
			err := e.Devolver(tt.em)
			if !errors.Is(err, ErrEmprestimoInvalido) {
				t.Fatalf("Emprestimo{}.Devolver(%v) erro = %v, want %v", tt.em, err, ErrEmprestimoInvalido)
			}
			checarEmprestimoZero(t, e)
			if !e.Aberto() {
				t.Error("Aberto() = false depois de erro, want true (estado inalterado)")
			}
		})
	}
}

func FuzzReidratarEmprestimo(f *testing.F) {
	sp := saoPaulo(f)
	for _, s := range []struct {
		inicio    time.Time
		devSeg    int64
		devolvido bool
	}{
		{inicio: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)},
		{inicio: time.Date(2026, 3, 10, 23, 30, 0, 0, sp), devSeg: 60, devolvido: true},
		{inicio: time.Date(2018, 10, 21, 9, 0, 0, 0, sp)},
		{inicio: time.Date(2018, 10, 20, 23, 30, 0, 0, sp), devSeg: 3600, devolvido: true},
		{inicio: time.Date(2019, 2, 3, 9, 0, 0, 0, sp), devSeg: 30 * 86400, devolvido: true},
		{inicio: time.Date(2019, 2, 16, 23, 30, 0, 0, sp), devSeg: 1800, devolvido: true},
		{inicio: time.Date(2024, 2, 20, 9, 0, 0, 0, sp)},
	} {
		f.Add(s.inicio.Unix(), s.devSeg, s.devolvido)
	}
	f.Add(fuzzMin, int64(0), false)
	f.Add(fuzzMax-1, int64(86400), true)

	const maxDevSeg = 400 * 86400

	f.Fuzz(func(t *testing.T, u, devSeg int64, devolvido bool) {
		inicio := time.Unix(limitarUnix(u), 0).UTC()

		orig, err := NovoEmprestimo(3, 4, inicio)
		if err != nil {
			t.Fatalf("NovoEmprestimo(3, 4, %v) erro inesperado: %v", inicio, err)
		}
		if devolvido {
			off := devSeg % maxDevSeg
			if off < 0 {
				off = -off
			}
			if err := orig.Devolver(inicio.Add(time.Duration(off) * time.Second)); err != nil {
				return // nada a reidratar: Devolver recusou
			}
		}

		for _, venc := range []time.Time{orig.Vencimento(), vencimentoComoDATE(t, orig.Vencimento())} {
			got, err := ReidratarEmprestimo(8, orig.LivroID(), orig.UsuarioID(), orig.Inicio(), venc, orig.Devolucao())
			if err != nil {
				t.Fatalf("inicio %v venc %v dev %v: ReidratarEmprestimo erro inesperado: %v", inicio, venc, orig.Devolucao(), err)
			}
			if !got.Vencimento().Equal(orig.Vencimento()) {
				t.Errorf("inicio %v venc %v: Vencimento() = %v, want %v", inicio, venc, got.Vencimento(), orig.Vencimento())
			}
			if !got.Inicio().Equal(orig.Inicio()) || !got.Devolucao().Equal(orig.Devolucao()) || got.Aberto() != orig.Aberto() {
				t.Errorf("inicio %v: ida e volta divergiu: got (%v, %v, %v), want (%v, %v, %v)",
					inicio, got.Inicio(), got.Devolucao(), got.Aberto(), orig.Inicio(), orig.Devolucao(), orig.Aberto())
			}

			again, err := ReidratarEmprestimo(8, got.LivroID(), got.UsuarioID(), got.Inicio(), got.Vencimento(), got.Devolucao())
			if err != nil {
				t.Fatalf("segunda ReidratarEmprestimo erro inesperado: %v", err)
			}
			if !again.Vencimento().Equal(got.Vencimento()) {
				t.Errorf("normalização não idempotente: %v -> %v", got.Vencimento(), again.Vencimento())
			}
		}
	})
}
