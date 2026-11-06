package httpapi_test

import (
	"encoding/json"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// Fluxo com banco: duas renovações, a terceira recusada, a consulta e a
// devolução trazem o contador; renovação vencida e sem empréstimo recusadas.
func TestBancoRenovacao(t *testing.T) {
	pool := novoPool(t, 4)
	rel := &relogio{t: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)}
	h := handlerBanco(pool, rel, 5*time.Second)

	lid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/livros", `{"titulo":"a"}`), 201))
	uid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/usuarios", `{"nome":"b"}`), 201))
	base := "/livros/" + lid

	rec := chamar(h, http.MethodPost, base+"/renovacao", "")
	checarErro(t, rec, http.StatusNotFound, "emprestimo_aberto_nao_encontrado", true)
	rec = chamar(h, http.MethodPost, "/livros/999999/renovacao", "")
	checarErro(t, rec, http.StatusNotFound, "emprestimo_aberto_nao_encontrado", true)

	criado := exigirStatus(t, chamar(h, http.MethodPost, base+"/emprestimo", `{"usuario_id":`+uid+`}`), 201)
	if criado["renovacoes"] != json.Number("0") {
		t.Errorf("renovacoes no empréstimo criado = %#v, want 0", criado["renovacoes"])
	}
	eid := idDe(t, criado)

	passos := []struct {
		agora    time.Time
		status   int
		codigo   string
		wantVenc string
		wantRen  string
	}{
		{agora: time.Date(2026, 3, 24, 23, 0, 0, 0, sp), status: 200, wantVenc: "2026-04-07", wantRen: "1"},
		{agora: time.Date(2026, 4, 1, 9, 0, 0, 0, sp), status: 200, wantVenc: "2026-04-21", wantRen: "2"},
		{agora: time.Date(2026, 4, 2, 9, 0, 0, 0, sp), status: 409, codigo: "limite_renovacoes_atingido"},
		{agora: time.Date(2026, 4, 22, 0, 0, 0, 0, sp), status: 409, codigo: "emprestimo_vencido"},
	}
	for i, p := range passos {
		rel.ajustar(p.agora)
		rec := chamar(h, http.MethodPost, base+"/renovacao", "")
		if p.codigo != "" {
			checarErro(t, rec, p.status, p.codigo, true)
			continue
		}
		m := exigirStatus(t, rec, p.status)
		if idDe(t, m) != eid || m["vencimento"] != p.wantVenc || m["renovacoes"] != json.Number(p.wantRen) || m["devolucao"] != nil || m["multa"] != "0.00" {
			t.Errorf("passo %d: renovação = %v, want vencimento %s e renovacoes %s", i+1, m, p.wantVenc, p.wantRen)
		}
	}

	rel.ajustar(time.Date(2026, 4, 23, 9, 0, 0, 0, sp))
	consulta := exigirStatus(t, chamar(h, http.MethodGet, base+"/emprestimo", ""), 200)
	if consulta["vencimento"] != "2026-04-21" || consulta["renovacoes"] != json.Number("2") || consulta["multa"] != "2.00" {
		t.Errorf("consulta = %v, want vencimento 2026-04-21, renovacoes 2, multa 2.00", consulta)
	}
	dev := exigirStatus(t, chamar(h, http.MethodPost, base+"/devolucao", ""), 200)
	if dev["renovacoes"] != json.Number("2") || dev["multa"] != "2.00" {
		t.Errorf("devolução = %v, want renovacoes 2 e multa 2.00", dev)
	}
	rec = chamar(h, http.MethodPost, base+"/renovacao", "")
	checarErro(t, rec, http.StatusNotFound, "emprestimo_aberto_nao_encontrado", true)

	novo := exigirStatus(t, chamar(h, http.MethodPost, base+"/emprestimo", `{"usuario_id":`+uid+`}`), 201)
	if novo["renovacoes"] != json.Number("0") {
		t.Errorf("novo empréstimo renovacoes = %#v, want 0", novo["renovacoes"])
	}
}

// Renovações concorrentes do mesmo empréstimo: o contador não passa do limite
// e cada 200 corresponde a exatamente uma renovação gravada.
func TestBancoRenovacaoConcorrente(t *testing.T) {
	const n = 16
	pool := novoPool(t, 8)
	rel := &relogio{t: time.Date(2026, 3, 10, 10, 0, 0, 0, sp)}
	h := handlerBanco(pool, rel, 5*time.Second)
	lid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/livros", `{"titulo":"a"}`), 201))
	uid := idDe(t, exigirStatus(t, chamar(h, http.MethodPost, "/usuarios", `{"nome":"b"}`), 201))
	exigirStatus(t, chamar(h, http.MethodPost, "/livros/"+lid+"/emprestimo", `{"usuario_id":`+uid+`}`), 201)
	rel.ajustar(time.Date(2026, 3, 12, 10, 0, 0, 0, sp))

	recs := paralelo(h, n, http.MethodPost, "/livros/"+lid+"/renovacao", "")
	ok := 0
	for _, rec := range recs {
		switch rec.Code {
		case http.StatusOK:
			ok++
		case http.StatusConflict:
			if c, _ := erroJSON(t, rec.Body.Bytes()); c != "emprestimo_alterado" && c != "limite_renovacoes_atingido" {
				t.Errorf("409 com codigo %q", c)
			}
		default:
			t.Errorf("status inesperado %d: %s", rec.Code, rec.Body.String())
		}
	}
	if ok < 1 || ok > 2 {
		t.Errorf("%d renovações com 200, want 1 ou 2", ok)
	}
	consulta := exigirStatus(t, chamar(h, http.MethodGet, "/livros/"+lid+"/emprestimo", ""), 200)
	want := map[int]string{1: "2026-04-07", 2: "2026-04-21"}[ok]
	if consulta["renovacoes"] != json.Number(strconv.Itoa(ok)) || consulta["vencimento"] != want {
		t.Errorf("consulta = %v, want renovacoes %d e vencimento %s", consulta, ok, want)
	}
}
