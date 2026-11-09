package storage

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgtype"

	"github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/domain"
)

// listagemNova adapta ListarUsuarios e ListarEmprestimos a uma assinatura
// comum: devolve os ids da página, temMais e o erro.
type listagemNova struct {
	nome   string
	listar func(r *Repositorio, ctx context.Context, apos int64, limite int) ([]int64, bool, error)
	// linha devolve os valores de uma linha válida com o id dado, na ordem
	// das colunas do SELECT.
	linha func(t *testing.T, id int64) []any
}

func listagensNovas() []listagemNova {
	return []listagemNova{
		{
			nome: "ListarUsuarios",
			listar: func(r *Repositorio, ctx context.Context, apos int64, limite int) ([]int64, bool, error) {
				got, mais, err := r.ListarUsuarios(ctx, apos, limite)
				if err == nil && got == nil {
					return nil, mais, errors.New("teste: ListarUsuarios devolveu nil sem erro, want slice vazia")
				}
				ids := make([]int64, 0, len(got))
				for _, u := range got {
					ids = append(ids, u.ID)
				}
				return ids, mais, err
			},
			linha: func(_ *testing.T, id int64) []any { return []any{id, fmt.Sprintf("Usuario %d", id)} },
		},
		{
			nome: "ListarEmprestimos",
			listar: func(r *Repositorio, ctx context.Context, apos int64, limite int) ([]int64, bool, error) {
				got, mais, err := r.ListarEmprestimos(ctx, FiltroEmprestimos{}, apos, limite)
				if err == nil && got == nil {
					return nil, mais, errors.New("teste: ListarEmprestimos devolveu nil sem erro, want slice vazia")
				}
				ids := make([]int64, 0, len(got))
				for _, e := range got {
					ids = append(ids, e.ID())
				}
				return ids, mais, err
			},
			linha: func(t *testing.T, id int64) []any {
				return []any{id, int64(1), int64(2), time.Date(2026, 3, 10, 10, 0, 0, 0, saoPaulo(t)),
					time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC), (*time.Time)(nil), 0}
			},
		},
	}
}

// O teto é um só para as três listagens (D-19): LimiteListagem = 100, e
// LimiteListagemLivros continua exportada com o mesmo valor.
func TestLimiteListagem(t *testing.T) {
	if LimiteListagem != 100 {
		t.Errorf("LimiteListagem = %d, want 100", LimiteListagem)
	}
	if LimiteListagemLivros != LimiteListagem {
		t.Errorf("LimiteListagemLivros = %d, want LimiteListagem (%d)", LimiteListagemLivros, LimiteListagem)
	}
}

// apos < 0 e limite fora de 1..LimiteListagem são recusados com
// ErrListagemInvalida antes de qualquer SQL.
func TestListagensNovasRecusaAntesDoSQL(t *testing.T) {
	tests := []struct {
		name   string
		apos   int64
		limite int
	}{
		{name: "limite zero", apos: 0, limite: 0},
		{name: "limite negativo", apos: 0, limite: -1},
		{name: "limite acima do teto", apos: 0, limite: LimiteListagem + 1},
		{name: "limite muito acima do teto", apos: 0, limite: 1 << 30},
		{name: "apos negativo", apos: -1, limite: 10},
		{name: "apos mínimo int64", apos: -1 << 63, limite: 10},
		{name: "apos negativo e limite inválido", apos: -1, limite: 0},
	}
	for _, l := range listagensNovas() {
		for _, tt := range tests {
			t.Run(l.nome+"/"+tt.name, func(t *testing.T) {
				db := &dbFalso{err: errors.New("dbFalso: não devia ser chamado")}
				ids, mais, err := l.listar(NovoRepositorio(db), context.Background(), tt.apos, tt.limite)
				if !errors.Is(err, ErrListagemInvalida) {
					t.Errorf("erro = %v, want %v", err, ErrListagemInvalida)
				}
				if db.chamadas != 0 {
					t.Errorf("%d chamada(s) ao banco, want 0", db.chamadas)
				}
				if len(ids) != 0 || mais {
					t.Errorf("com erro devolveu %v, %v", ids, mais)
				}
			})
		}
	}
}

// Os extremos válidos passam pela validação e chegam ao banco uma vez.
func TestListagensNovasLimitesAceitos(t *testing.T) {
	tests := []struct {
		name   string
		apos   int64
		limite int
	}{
		{name: "início com limite 1", apos: 0, limite: 1},
		{name: "início com teto", apos: 0, limite: LimiteListagem},
		{name: "apos máximo", apos: 1<<63 - 1, limite: 20},
	}
	for _, l := range listagensNovas() {
		for _, tt := range tests {
			t.Run(l.nome+"/"+tt.name, func(t *testing.T) {
				falha := errors.New("dbFalso: chamado")
				db := &dbFalso{err: falha}
				ids, mais, err := l.listar(NovoRepositorio(db), context.Background(), tt.apos, tt.limite)
				if !errors.Is(err, falha) {
					t.Errorf("erro = %v, want o erro do banco (validação não deveria recusar)", err)
				}
				if db.chamadas != 1 {
					t.Errorf("%d chamada(s) ao banco, want 1", db.chamadas)
				}
				if len(ids) != 0 || mais {
					t.Errorf("com erro devolveu %v e temMais=%v", ids, mais)
				}
			})
		}
	}
}

// linhasFalsas é um pgx.Rows com linhas fixas. errScan[i] faz o Scan da
// linha i falhar; err sai em Err() ao fim.
type linhasFalsas struct {
	linhas  [][]any
	errScan map[int]error
	err     error

	atual   int
	scans   int
	fechado bool
}

func (l *linhasFalsas) Next() bool {
	if l.fechado || l.atual >= len(l.linhas) {
		return false
	}
	l.atual++
	return true
}

func (l *linhasFalsas) Scan(dest ...any) error {
	l.scans++
	i := l.atual - 1
	if e := l.errScan[i]; e != nil {
		return e
	}
	vals := l.linhas[i]
	if len(dest) != len(vals) {
		return fmt.Errorf("linhasFalsas: %d destinos para %d colunas", len(dest), len(vals))
	}
	for j, d := range dest {
		switch p := d.(type) {
		case *int64:
			*p = vals[j].(int64)
		case *string:
			*p = vals[j].(string)
		case *time.Time:
			*p = vals[j].(time.Time)
		case **time.Time:
			*p = vals[j].(*time.Time)
		case *int:
			*p = vals[j].(int)
		default:
			return fmt.Errorf("linhasFalsas: destino %T não suportado", d)
		}
	}
	return nil
}

func (l *linhasFalsas) Close()                                       { l.fechado = true }
func (l *linhasFalsas) Err() error                                   { return l.err }
func (l *linhasFalsas) CommandTag() pgconn.CommandTag                { return pgconn.CommandTag{} }
func (l *linhasFalsas) FieldDescriptions() []pgconn.FieldDescription { return nil }
func (l *linhasFalsas) Values() ([]any, error)                       { return nil, errors.New("linhasFalsas: Values") }
func (l *linhasFalsas) RawValues() [][]byte                          { return nil }
func (l *linhasFalsas) Conn() *pgx.Conn                              { return nil }
func (l *linhasFalsas) TypeMap() *pgtype.Map                         { return pgtype.NewMap() }

// dbLinhas devolve linhasFalsas em Query e guarda o SQL e os argumentos.
type dbLinhas struct {
	dbFalso
	linhas *linhasFalsas
	sql    string
	args   []any
}

func (d *dbLinhas) Query(_ context.Context, sql string, args ...any) (pgx.Rows, error) {
	d.chamadas++
	d.sql, d.args = sql, args
	return d.linhas, nil
}

// TestListagensNovasLinhaExtra: o SQL recebe (apos, limite+1); com limite+1
// linhas, a página tem limite itens, temMais verdadeiro e a linha extra não é
// escaneada (aqui o Scan dela falharia). Com até limite linhas, temMais é
// falso.
func TestListagensNovasLinhaExtra(t *testing.T) {
	tests := []struct {
		name        string
		apos        int64
		limite      int
		nLinhas     int
		wantIDs     string
		wantTemMais bool
	}{
		{name: "limite+1 linhas", apos: 0, limite: 3, nLinhas: 4, wantIDs: "[1 2 3]", wantTemMais: true},
		{name: "limite 1 com extra", apos: 7, limite: 1, nLinhas: 2, wantIDs: "[1]", wantTemMais: true},
		{name: "exatamente limite linhas", apos: 0, limite: 3, nLinhas: 3, wantIDs: "[1 2 3]", wantTemMais: false},
		{name: "menos que limite", apos: 5, limite: 3, nLinhas: 2, wantIDs: "[1 2]", wantTemMais: false},
		{name: "sem linhas", apos: 5, limite: 3, nLinhas: 0, wantIDs: "[]", wantTemMais: false},
		{name: "teto com extra", apos: 0, limite: LimiteListagem, nLinhas: LimiteListagem + 1, wantTemMais: true},
	}
	for _, l := range listagensNovas() {
		for _, tt := range tests {
			t.Run(l.nome+"/"+tt.name, func(t *testing.T) {
				linhas := &linhasFalsas{errScan: map[int]error{}}
				for i := 0; i < tt.nLinhas; i++ {
					linhas.linhas = append(linhas.linhas, l.linha(t, int64(i+1)))
				}
				if tt.nLinhas > tt.limite {
					linhas.errScan[tt.limite] = errors.New("linha extra escaneada")
				}
				db := &dbLinhas{linhas: linhas}
				ids, mais, err := l.listar(NovoRepositorio(db), context.Background(), tt.apos, tt.limite)
				if err != nil {
					t.Fatalf("erro = %v", err)
				}
				if len(db.args) != 2 || db.args[0] != tt.apos || fmt.Sprint(db.args[1]) != fmt.Sprint(tt.limite+1) {
					t.Errorf("argumentos do SQL = %v, want [%d %d] (apos, limite+1)", db.args, tt.apos, tt.limite+1)
				}
				if !strings.Contains(db.sql, "ORDER BY id") {
					t.Errorf("SQL sem ORDER BY id: %s", db.sql)
				}
				if tt.wantIDs != "" && fmt.Sprint(ids) != tt.wantIDs {
					t.Errorf("ids = %v, want %s", ids, tt.wantIDs)
				}
				if want := min(tt.nLinhas, tt.limite); len(ids) != want || linhas.scans != want {
					t.Errorf("%d itens e %d Scan(s), want %d e %d", len(ids), linhas.scans, want, want)
				}
				if mais != tt.wantTemMais {
					t.Errorf("temMais = %v, want %v", mais, tt.wantTemMais)
				}
				if !linhas.fechado {
					t.Error("rows não foi fechado")
				}
			})
		}
	}
}

// Erro no Scan de uma linha entregue ou em rows.Err não vira página parcial
// nem vazia.
func TestListagensNovasErroDuranteLeitura(t *testing.T) {
	falha := errors.New("conexão perdida no meio")
	tests := []struct {
		name    string
		nLinhas int
		errScan map[int]error
		errFim  error
	}{
		{name: "rows.Err sem linhas", nLinhas: 0, errFim: falha},
		{name: "rows.Err depois de linhas", nLinhas: 2, errFim: falha},
		{name: "Scan da primeira linha", nLinhas: 3, errScan: map[int]error{0: falha}},
		{name: "Scan da última linha entregue", nLinhas: 3, errScan: map[int]error{2: falha}},
	}
	for _, l := range listagensNovas() {
		for _, tt := range tests {
			t.Run(l.nome+"/"+tt.name, func(t *testing.T) {
				linhas := &linhasFalsas{errScan: tt.errScan, err: tt.errFim}
				for i := 0; i < tt.nLinhas; i++ {
					linhas.linhas = append(linhas.linhas, l.linha(t, int64(i+1)))
				}
				db := &dbLinhas{linhas: linhas}
				ids, mais, err := l.listar(NovoRepositorio(db), context.Background(), 0, 20)
				if !errors.Is(err, falha) {
					t.Errorf("erro = %v, want %v", err, falha)
				}
				if len(ids) != 0 || mais {
					t.Errorf("com erro devolveu %v e temMais=%v", ids, mais)
				}
				if !linhas.fechado {
					t.Error("rows não foi fechado")
				}
			})
		}
	}
}

// Uma linha de empréstimo que o domínio recusa derruba a página inteira com
// o mesmo erro do detalhe ("empréstimo gravado inválido"), sem item pulado;
// se ela for a linha extra (+1), não é lida e a página sai normal.
func TestListarEmprestimosLinhaInvalidaSemBanco(t *testing.T) {
	sp := saoPaulo(t)
	valida := func(id int64) []any {
		return []any{id, int64(1), int64(2), time.Date(2026, 3, 10, 10, 0, 0, 0, sp), time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC), (*time.Time)(nil), 0}
	}
	vencimentoAntes := func(id int64) []any {
		v := valida(id)
		v[4] = time.Date(2000, 1, 1, 0, 0, 0, 0, time.UTC)
		return v
	}
	devolucaoAntes := func(id int64) []any {
		v := valida(id)
		d := time.Date(2026, 3, 1, 10, 0, 0, 0, sp)
		v[5] = &d
		return v
	}
	tests := []struct {
		name        string
		linhas      [][]any
		limite      int
		wantErr     error
		wantIDs     string
		wantTemMais bool
	}{
		{name: "vencimento antes do início no meio", linhas: [][]any{valida(1), vencimentoAntes(2), valida(3)}, limite: 20, wantErr: domain.ErrVencimentoNaoPosteriorAoInicio},
		{name: "devolução antes do início na primeira", linhas: [][]any{devolucaoAntes(1), valida(2)}, limite: 20, wantErr: domain.ErrDevolucaoAntesDoInicio},
		{name: "inválida na última entregue", linhas: [][]any{valida(1), vencimentoAntes(2)}, limite: 2, wantErr: domain.ErrVencimentoNaoPosteriorAoInicio},
		{name: "inválida como linha extra", linhas: [][]any{valida(1), vencimentoAntes(2)}, limite: 1, wantIDs: "[1]", wantTemMais: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			linhas := &linhasFalsas{linhas: tt.linhas}
			got, mais, err := NovoRepositorio(&dbLinhas{linhas: linhas}).ListarEmprestimos(context.Background(), FiltroEmprestimos{}, 0, tt.limite)
			if tt.wantErr != nil {
				if !errors.Is(err, tt.wantErr) {
					t.Fatalf("erro = %v, want %v", err, tt.wantErr)
				}
				if errors.Is(err, ErrListagemInvalida) || errors.Is(err, ErrEmprestimoNaoEncontrado) {
					t.Errorf("erro = %v, não pode ser sentinela de parâmetro nem de inexistente", err)
				}
				if !strings.Contains(err.Error(), "empréstimo gravado inválido") {
					t.Errorf("erro = %q, want o mesmo erro do detalhe (empréstimo gravado inválido)", err)
				}
				if len(got) != 0 || mais {
					t.Errorf("com erro devolveu %d itens e temMais=%v", len(got), mais)
				}
				if !linhas.fechado {
					t.Error("rows não foi fechado")
				}
				return
			}
			if err != nil {
				t.Fatalf("erro = %v", err)
			}
			ids := make([]int64, 0, len(got))
			for _, e := range got {
				ids = append(ids, e.ID())
			}
			if fmt.Sprint(ids) != tt.wantIDs || mais != tt.wantTemMais {
				t.Errorf("ids = %v temMais = %v, want %s e %v", ids, mais, tt.wantIDs, tt.wantTemMais)
			}
		})
	}
}

// Os itens de ListarEmprestimos são reidratados com todos os campos da linha.
func TestListarEmprestimosReidrataCampos(t *testing.T) {
	sp := saoPaulo(t)
	inicio := time.Date(2026, 3, 10, 10, 0, 0, 0, sp)
	venc := time.Date(2026, 4, 7, 0, 0, 0, 0, time.UTC)
	dev := time.Date(2026, 3, 20, 10, 0, 0, 0, sp)
	linhas := &linhasFalsas{linhas: [][]any{
		{int64(11), int64(21), int64(31), inicio, venc, (*time.Time)(nil), 1},
		{int64(12), int64(22), int64(32), inicio, time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC), &dev, 0},
	}}
	got, mais, err := NovoRepositorio(&dbLinhas{linhas: linhas}).ListarEmprestimos(context.Background(), FiltroEmprestimos{}, 0, 20)
	if err != nil || mais || len(got) != 2 {
		t.Fatalf("ListarEmprestimos = %d itens, %v, %v; want 2, false, nil", len(got), mais, err)
	}
	aberto, err := domain.ReidratarEmprestimo(11, 21, 31, inicio, venc, time.Time{}, 1)
	if err != nil {
		t.Fatalf("ReidratarEmprestimo: %v", err)
	}
	devolvido, err := domain.ReidratarEmprestimo(12, 22, 32, inicio, time.Date(2026, 3, 24, 0, 0, 0, 0, time.UTC), dev, 0)
	if err != nil {
		t.Fatalf("ReidratarEmprestimo: %v", err)
	}
	checarEmprestimo(t, got[0], aberto)
	checarEmprestimo(t, got[1], devolvido)
}

// ListarUsuarios devolve id e nome de cada linha.
func TestListarUsuariosCamposSemBanco(t *testing.T) {
	linhas := &linhasFalsas{linhas: [][]any{{int64(4), "Ana"}, {int64(9), "Bento"}}}
	got, mais, err := NovoRepositorio(&dbLinhas{linhas: linhas}).ListarUsuarios(context.Background(), 0, 20)
	if err != nil || mais {
		t.Fatalf("ListarUsuarios: %v, temMais=%v", err, mais)
	}
	if fmt.Sprint(got) != fmt.Sprint([]domain.Usuario{{ID: 4, Nome: "Ana"}, {ID: 9, Nome: "Bento"}}) {
		t.Errorf("usuarios = %v", got)
	}
}

// Erros do banco nas listagens novas não vazam apos, limite, SQL nem o texto
// do PostgreSQL.
func TestListagensNovasErroSemDados(t *testing.T) {
	falha := errors.New("conexão perdida")
	pgCancelado := &pgconn.PgError{Severity: "ERROR", Code: "57014", Message: "canceling statement SEGREDO", Detail: "Nome Sigiloso Do Usuario"}
	for _, l := range listagensNovas() {
		for _, dbErr := range []error{falha, pgCancelado} {
			t.Run(fmt.Sprintf("%s/%T", l.nome, dbErr), func(t *testing.T) {
				db := &dbFalso{err: dbErr}
				_, _, err := l.listar(NovoRepositorio(db), context.Background(), 757575, 37)
				if !errors.Is(err, dbErr) {
					t.Errorf("erro = %v, want errors.Is %v", err, dbErr)
				}
				if db.chamadas != 1 {
					t.Errorf("%d chamada(s) ao banco, want 1", db.chamadas)
				}
				semDados(t, err.Error(), append([]string{"757575", "38", "SEGREDO", "Nome Sigiloso Do Usuario"}, trechosSQL...)...)
			})
		}
	}
}
