package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgtype"
)

// DEL-015 (D-21): ListarEmprestimos com FiltroEmprestimos. Testes sem banco
// da montagem do SQL (sqlListarEmprestimos), da passagem de argumentos e da
// revalidação do filtro.

// sqlSemFiltro é o SQL de ListarEmprestimos antes da DEL-015. Com o filtro
// zero, o texto gerado tem de ser este, byte a byte (cache de prepared
// statement do pgx e plano já conhecidos).
const sqlSemFiltro = `SELECT id, livro_id, usuario_id, inicio, vencimento, devolucao, renovacoes FROM emprestimo WHERE id > $1 ORDER BY id LIMIT $2`

// prefixoSQLEmprestimos e sufixoSQLEmprestimos cercam os fragmentos do filtro.
const (
	prefixoSQLEmprestimos = `SELECT id, livro_id, usuario_id, inicio, vencimento, devolucao, renovacoes FROM emprestimo WHERE id > $1`
	sufixoSQLEmprestimos  = ` ORDER BY id LIMIT $2`
)

// Valores do filtro que não podem aparecer no texto SQL.
const (
	usuarioMarcado int64 = 987654321
	livroMarcado   int64 = 123456789
)

// refMarcada é 19/07/2031 02:59:59Z, ou 18/07/2031 23:59:59 em São Paulo:
// a data civil de hoje é 18/07/2031, não 19/07 (o dia em UTC).
var refMarcada = time.Date(2031, 7, 19, 2, 59, 59, 0, time.UTC)

// hojeMarcado é o parâmetro date esperado para refMarcada.
var hojeMarcado = time.Date(2031, 7, 18, 0, 0, 0, 0, time.UTC)

// combinacaoFiltro é uma das 16 combinações (usuário x livro x situação)
// com o SQL e os argumentos extras esperados (depois de $1 = apos e
// $2 = limite+1).
type combinacaoFiltro struct {
	name     string
	filtro   FiltroEmprestimos
	wantTail string // entre prefixoSQLEmprestimos e sufixoSQLEmprestimos
	wantArgs []any
}

// combinacoesFiltro devolve as 16 combinações. Ref vai preenchida em todas:
// só a situação vencido pode transformá-la em argumento.
func combinacoesFiltro() []combinacaoFiltro {
	f := func(u, l int64, s Situacao) FiltroEmprestimos {
		return FiltroEmprestimos{UsuarioID: u, LivroID: l, Situacao: s, Ref: refMarcada}
	}
	U, L, H := usuarioMarcado, livroMarcado, hojeMarcado
	return []combinacaoFiltro{
		{name: "01 sem filtro", filtro: f(0, 0, SituacaoTodas), wantTail: "", wantArgs: nil},
		{name: "02 aberto", filtro: f(0, 0, SituacaoAberto), wantTail: " AND devolucao IS NULL", wantArgs: nil},
		{name: "03 vencido", filtro: f(0, 0, SituacaoVencido), wantTail: " AND devolucao IS NULL AND vencimento < $3", wantArgs: []any{H}},
		{name: "04 devolvido", filtro: f(0, 0, SituacaoDevolvido), wantTail: " AND devolucao IS NOT NULL", wantArgs: nil},
		{name: "05 usuario", filtro: f(U, 0, SituacaoTodas), wantTail: " AND usuario_id = $3", wantArgs: []any{U}},
		{name: "06 usuario+aberto", filtro: f(U, 0, SituacaoAberto), wantTail: " AND usuario_id = $3 AND devolucao IS NULL", wantArgs: []any{U}},
		{name: "07 usuario+vencido", filtro: f(U, 0, SituacaoVencido), wantTail: " AND usuario_id = $3 AND devolucao IS NULL AND vencimento < $4", wantArgs: []any{U, H}},
		{name: "08 usuario+devolvido", filtro: f(U, 0, SituacaoDevolvido), wantTail: " AND usuario_id = $3 AND devolucao IS NOT NULL", wantArgs: []any{U}},
		{name: "09 livro", filtro: f(0, L, SituacaoTodas), wantTail: " AND livro_id = $3", wantArgs: []any{L}},
		{name: "10 livro+aberto", filtro: f(0, L, SituacaoAberto), wantTail: " AND livro_id = $3 AND devolucao IS NULL", wantArgs: []any{L}},
		{name: "11 livro+vencido", filtro: f(0, L, SituacaoVencido), wantTail: " AND livro_id = $3 AND devolucao IS NULL AND vencimento < $4", wantArgs: []any{L, H}},
		{name: "12 livro+devolvido", filtro: f(0, L, SituacaoDevolvido), wantTail: " AND livro_id = $3 AND devolucao IS NOT NULL", wantArgs: []any{L}},
		{name: "13 usuario+livro", filtro: f(U, L, SituacaoTodas), wantTail: " AND usuario_id = $3 AND livro_id = $4", wantArgs: []any{U, L}},
		{name: "14 usuario+livro+aberto", filtro: f(U, L, SituacaoAberto), wantTail: " AND usuario_id = $3 AND livro_id = $4 AND devolucao IS NULL", wantArgs: []any{U, L}},
		{name: "15 usuario+livro+vencido", filtro: f(U, L, SituacaoVencido), wantTail: " AND usuario_id = $3 AND livro_id = $4 AND devolucao IS NULL AND vencimento < $5", wantArgs: []any{U, L, H}},
		{name: "16 usuario+livro+devolvido", filtro: f(U, L, SituacaoDevolvido), wantTail: " AND usuario_id = $3 AND livro_id = $4 AND devolucao IS NOT NULL", wantArgs: []any{U, L}},
	}
}

// checarArgs compara argumentos de SQL: int64 por igualdade; time.Time por
// instante, exigindo UTC (meia-noite da data civil, como vencimentoParaDate).
func checarArgs(t *testing.T, got, want []any) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("args = %#v (%d), want %#v (%d)", got, len(got), want, len(want))
	}
	for i := range want {
		switch w := want[i].(type) {
		case int64:
			g, ok := got[i].(int64)
			if !ok || g != w {
				t.Errorf("args[%d] = %#v (%T), want int64 %d", i, got[i], got[i], w)
			}
		case int:
			g, ok := got[i].(int)
			if !ok || g != w {
				// limite+1 pode sair como int ou int64; compara o valor.
				if fmt.Sprint(got[i]) != fmt.Sprint(w) {
					t.Errorf("args[%d] = %#v, want %d", i, got[i], w)
				}
			}
		case time.Time:
			g, ok := got[i].(time.Time)
			if !ok {
				t.Errorf("args[%d] = %#v (%T), want time.Time", i, got[i], got[i])
				continue
			}
			if !g.Equal(w) || g.Location() != time.UTC {
				t.Errorf("args[%d] = %v (%v), want %v em UTC", i, g, g.Location(), w)
			}
		default:
			t.Fatalf("tipo de arg esperado não suportado: %T", w)
		}
	}
}

// TestSQLListarEmprestimosCombinacoes: as 16 combinações produzem o texto
// SQL exato, só com fragmentos constantes, na ordem usuário, livro,
// situação, com placeholders a partir de $3 e os valores só nos args.
func TestSQLListarEmprestimosCombinacoes(t *testing.T) {
	vistos := map[string]string{}
	for _, tt := range combinacoesFiltro() {
		t.Run(tt.name, func(t *testing.T) {
			sql, args := sqlListarEmprestimos(tt.filtro)
			want := prefixoSQLEmprestimos + tt.wantTail + sufixoSQLEmprestimos
			if sql != want {
				t.Errorf("SQL =\n  %q\nwant\n  %q", sql, want)
			}
			checarArgs(t, args, tt.wantArgs)

			// Nenhum valor do filtro no texto.
			for _, proibido := range []string{
				fmt.Sprint(usuarioMarcado), fmt.Sprint(livroMarcado),
				"2031", "07-18", "07-19", "'",
			} {
				if strings.Contains(sql, proibido) {
					t.Errorf("SQL contém %q (valor fora dos args): %s", proibido, sql)
				}
			}
			// Nada de relógio ou fuso do banco, nem padrões proibidos.
			baixo := strings.ToLower(sql)
			for _, proibido := range []string{
				"now()", "current_date", "current_timestamp", "localtimestamp", "localtime", "at time zone",
				"coalesce", " or ", "::", "offset", "count(", "join", "$0",
			} {
				if strings.Contains(baixo, proibido) {
					t.Errorf("SQL contém %q: %s", proibido, sql)
				}
			}
			// Mesma entrada, mesmo texto (cache de prepared statement).
			if sql2, _ := sqlListarEmprestimos(tt.filtro); sql2 != sql {
				t.Errorf("texto instável: %q != %q", sql2, sql)
			}
		})
	}
	// No máximo 16 textos, um por combinação (2 x 2 x 4).
	for _, tt := range combinacoesFiltro() {
		sql, _ := sqlListarEmprestimos(tt.filtro)
		if outro, ok := vistos[sql]; ok {
			t.Errorf("%s gera o mesmo SQL de %s: %s", tt.name, outro, sql)
		}
		vistos[sql] = tt.name
	}
	if len(vistos) != 16 {
		t.Errorf("%d textos SQL distintos, want 16", len(vistos))
	}
}

// Com o filtro zero, o SQL é byte a byte o de antes da DEL-015, sem args
// extras; Ref, sozinha, não muda nada.
func TestSQLListarEmprestimosSemFiltroInalterado(t *testing.T) {
	tests := []struct {
		name   string
		filtro FiltroEmprestimos
	}{
		{name: "filtro zero", filtro: FiltroEmprestimos{}},
		{name: "só Ref", filtro: FiltroEmprestimos{Ref: refMarcada}},
		{name: "SituacaoTodas explícita", filtro: FiltroEmprestimos{Situacao: SituacaoTodas}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			sql, args := sqlListarEmprestimos(tt.filtro)
			if sql != sqlSemFiltro {
				t.Errorf("SQL = %q, want %q", sql, sqlSemFiltro)
			}
			if len(args) != 0 {
				t.Errorf("args = %#v, want nenhum", args)
			}
		})
	}
	if SituacaoTodas != 0 {
		t.Errorf("SituacaoTodas = %v, want o valor zero do tipo", SituacaoTodas)
	}
}

// Ref vira o parâmetro date pela data civil em São Paulo (vencimentoParaDate),
// qualquer que seja o fuso do valor: 23:59:59 em SP ainda é o dia anterior,
// 00:00 em SP já é o dia seguinte.
func TestSQLListarEmprestimosHojeEmSaoPaulo(t *testing.T) {
	sp := saoPaulo(t)
	utc14 := time.FixedZone("UTC+14", 14*60*60)
	tests := []struct {
		name string
		ref  time.Time
		want string
	}{
		{name: "23:59:59 SP (02:59:59Z)", ref: time.Date(2026, 3, 24, 2, 59, 59, 0, time.UTC), want: "2026-03-23"},
		{name: "23:59:59.999999999 SP", ref: time.Date(2026, 3, 23, 23, 59, 59, 999999999, sp), want: "2026-03-23"},
		{name: "00:00 SP (03:00Z)", ref: time.Date(2026, 3, 24, 3, 0, 0, 0, time.UTC), want: "2026-03-24"},
		{name: "12:00 SP", ref: time.Date(2026, 3, 24, 12, 0, 0, 0, sp), want: "2026-03-24"},
		{name: "mesmo instante em UTC+14 (dia seguinte no fuso do valor)", ref: time.Date(2026, 3, 24, 2, 59, 59, 0, time.UTC).In(utc14), want: "2026-03-23"},
		{name: "virada de ano", ref: time.Date(2027, 1, 1, 2, 30, 0, 0, time.UTC), want: "2026-12-31"},
	}
	m := pgtype.NewMap()
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, args := sqlListarEmprestimos(FiltroEmprestimos{Situacao: SituacaoVencido, Ref: tt.ref})
			if len(args) != 1 {
				t.Fatalf("args = %#v, want só a data de hoje", args)
			}
			hoje, ok := args[0].(time.Time)
			if !ok {
				t.Fatalf("args[0] = %#v (%T), want time.Time", args[0], args[0])
			}
			if !hoje.Equal(vencimentoParaDate(tt.ref)) || hoje.Location() != time.UTC {
				t.Errorf("hoje = %v, want vencimentoParaDate(ref) = %v", hoje, vencimentoParaDate(tt.ref))
			}
			if h, mi, s := hoje.Clock(); h != 0 || mi != 0 || s != 0 || hoje.Nanosecond() != 0 {
				t.Errorf("hoje = %v, want meia-noite UTC", hoje)
			}
			buf, err := m.Encode(pgtype.DateOID, pgtype.TextFormatCode, hoje, nil)
			if err != nil {
				t.Fatalf("Encode DATE: %v", err)
			}
			if string(buf) != tt.want {
				t.Errorf("DATE codificado = %q, want %q", buf, tt.want)
			}
		})
	}
}

// Teste de tabela de vencimentoParaDate nos instantes da fronteira de vencido
// (D-17): é a mesma conversão do vencimento gravado e do parâmetro $hoje.
func TestVencimentoParaDateFronteiraVencido(t *testing.T) {
	sp := saoPaulo(t)
	tests := []struct {
		name string
		in   time.Time
		want string
	}{
		{name: "02:59:59Z = 23:59:59 SP", in: time.Date(2026, 3, 25, 2, 59, 59, 0, time.UTC), want: "2026-03-24"},
		{name: "03:00:00Z = 00:00 SP", in: time.Date(2026, 3, 25, 3, 0, 0, 0, time.UTC), want: "2026-03-25"},
		{name: "12:00 SP", in: time.Date(2026, 3, 24, 12, 0, 0, 0, sp), want: "2026-03-24"},
		{name: "00:00:00 SP", in: time.Date(2026, 3, 24, 0, 0, 0, 0, sp), want: "2026-03-24"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := vencimentoParaDate(tt.in).Format(time.DateOnly); got != tt.want {
				t.Errorf("vencimentoParaDate(%v) = %s, want %s", tt.in, got, tt.want)
			}
		})
	}
}

// ListarEmprestimos envia ao banco exatamente o SQL de sqlListarEmprestimos
// e os args (apos, limite+1, extras...), na ordem.
func TestListarEmprestimosFiltroArgs(t *testing.T) {
	for _, tt := range combinacoesFiltro() {
		t.Run(tt.name, func(t *testing.T) {
			db := &dbLinhas{linhas: &linhasFalsas{}}
			got, mais, err := NovoRepositorio(db).ListarEmprestimos(context.Background(), tt.filtro, 41, 7)
			if err != nil {
				t.Fatalf("ListarEmprestimos: %v", err)
			}
			if got == nil || len(got) != 0 || mais {
				t.Errorf("= %#v, %v; want [] e false", got, mais)
			}
			if db.chamadas != 1 {
				t.Errorf("%d chamada(s) ao banco, want 1", db.chamadas)
			}
			wantSQL := prefixoSQLEmprestimos + tt.wantTail + sufixoSQLEmprestimos
			if db.sql != wantSQL {
				t.Errorf("SQL = %q, want %q", db.sql, wantSQL)
			}
			checarArgs(t, db.args, append([]any{int64(41), 8}, tt.wantArgs...))
		})
	}
	t.Run("filtro zero: SQL e args de antes", func(t *testing.T) {
		db := &dbLinhas{linhas: &linhasFalsas{}}
		if _, _, err := NovoRepositorio(db).ListarEmprestimos(context.Background(), FiltroEmprestimos{}, 0, 20); err != nil {
			t.Fatalf("ListarEmprestimos: %v", err)
		}
		if db.sql != sqlSemFiltro {
			t.Errorf("SQL = %q, want %q", db.sql, sqlSemFiltro)
		}
		checarArgs(t, db.args, []any{int64(0), 21})
	})
}

// Filtro inválido é bug de quem chama: ErrListagemInvalida antes de qualquer
// SQL. Inclui vencido sem Ref (não há "hoje" sem relógio).
func TestListarEmprestimosFiltroInvalido(t *testing.T) {
	ref := time.Date(2026, 3, 24, 12, 0, 0, 0, saoPaulo(t))
	tests := []struct {
		name   string
		filtro FiltroEmprestimos
		apos   int64
		limite int
	}{
		{name: "UsuarioID negativo", filtro: FiltroEmprestimos{UsuarioID: -1}, limite: 20},
		{name: "UsuarioID mínimo int64", filtro: FiltroEmprestimos{UsuarioID: -1 << 63}, limite: 20},
		{name: "LivroID negativo", filtro: FiltroEmprestimos{LivroID: -1}, limite: 20},
		{name: "LivroID negativo com usuário válido", filtro: FiltroEmprestimos{UsuarioID: 2, LivroID: -7}, limite: 20},
		{name: "situação fora das constantes", filtro: FiltroEmprestimos{Situacao: SituacaoVencido + 1, Ref: ref}, limite: 20},
		{name: "situação muito fora das constantes", filtro: FiltroEmprestimos{Situacao: SituacaoVencido + 100, Ref: ref}, limite: 20},
		{name: "vencido sem Ref", filtro: FiltroEmprestimos{Situacao: SituacaoVencido}, limite: 20},
		{name: "vencido sem Ref com usuário e livro", filtro: FiltroEmprestimos{UsuarioID: 2, LivroID: 3, Situacao: SituacaoVencido}, limite: 20},
		{name: "filtro válido com apos negativo", filtro: FiltroEmprestimos{UsuarioID: 2}, apos: -1, limite: 20},
		{name: "filtro válido com limite zero", filtro: FiltroEmprestimos{LivroID: 3}, limite: 0},
		{name: "filtro válido com limite acima do teto", filtro: FiltroEmprestimos{Situacao: SituacaoAberto}, limite: LimiteListagem + 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			db := &dbFalso{err: errors.New("dbFalso: não devia ser chamado")}
			got, mais, err := NovoRepositorio(db).ListarEmprestimos(context.Background(), tt.filtro, tt.apos, tt.limite)
			if !errors.Is(err, ErrListagemInvalida) {
				t.Errorf("erro = %v, want %v", err, ErrListagemInvalida)
			}
			if db.chamadas != 0 {
				t.Errorf("%d chamada(s) ao banco, want 0", db.chamadas)
			}
			if len(got) != 0 || mais {
				t.Errorf("com erro devolveu %d itens e temMais=%v", len(got), mais)
			}
		})
	}
}

// Os extremos válidos do filtro passam pela validação e chegam ao banco uma
// vez. Ref zero só é recusada com vencido.
func TestListarEmprestimosFiltroAceito(t *testing.T) {
	ref := time.Date(2026, 3, 24, 12, 0, 0, 0, saoPaulo(t))
	tests := []struct {
		name   string
		filtro FiltroEmprestimos
	}{
		{name: "UsuarioID máximo", filtro: FiltroEmprestimos{UsuarioID: 1<<63 - 1}},
		{name: "LivroID máximo", filtro: FiltroEmprestimos{LivroID: 1<<63 - 1}},
		{name: "UsuarioID 1 e LivroID 1", filtro: FiltroEmprestimos{UsuarioID: 1, LivroID: 1}},
		{name: "aberto sem Ref", filtro: FiltroEmprestimos{Situacao: SituacaoAberto}},
		{name: "devolvido sem Ref", filtro: FiltroEmprestimos{Situacao: SituacaoDevolvido}},
		{name: "todas sem Ref", filtro: FiltroEmprestimos{}},
		{name: "vencido com Ref", filtro: FiltroEmprestimos{Situacao: SituacaoVencido, Ref: ref}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			falha := errors.New("dbFalso: chamado")
			db := &dbFalso{err: falha}
			_, _, err := NovoRepositorio(db).ListarEmprestimos(context.Background(), tt.filtro, 0, 20)
			if !errors.Is(err, falha) {
				t.Errorf("erro = %v, want o erro do banco (validação não deveria recusar)", err)
			}
			if db.chamadas != 1 {
				t.Errorf("%d chamada(s) ao banco, want 1", db.chamadas)
			}
		})
	}
}

// Erro do banco com filtro não vaza os valores do filtro, a data nem o SQL.
func TestListarEmprestimosFiltroErroSemDados(t *testing.T) {
	falha := errors.New("conexão perdida")
	db := &dbFalso{err: falha}
	f := FiltroEmprestimos{UsuarioID: usuarioMarcado, LivroID: livroMarcado, Situacao: SituacaoVencido, Ref: refMarcada}
	_, _, err := NovoRepositorio(db).ListarEmprestimos(context.Background(), f, 757575, 37)
	if !errors.Is(err, falha) {
		t.Fatalf("erro = %v, want %v", err, falha)
	}
	semDados(t, err.Error(), append([]string{fmt.Sprint(usuarioMarcado), fmt.Sprint(livroMarcado), "2031", "757575", "38"}, trechosSQL...)...)
}
