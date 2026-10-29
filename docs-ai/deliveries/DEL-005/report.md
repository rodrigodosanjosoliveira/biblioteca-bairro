---
delivery_id: DEL-005
status: em revisão
data: 2026-10-29
require_security: false
require_release_ops: false
---

# DEL-005 — Multa por atraso no empréstimo com CalcularMulta

<!--
Preencha cada campo no lugar de <preencher> e marque o checklist só com
evidência dos handoffs. Com require_security: true, acrescente a seção
Security (título de nível 2) com o campo "- Resultado:". Com
require_release_ops: true, acrescente a seção Release/Ops com o campo
"- Plano de rollback:". O validador reprova placeholder, item de checklist
aberto e marcadores de pendência em caixa alta.
-->

## PM/Triage
- Escopo fechado: tipo feature. (1) método derivado em `Emprestimo` delegando para `CalcularMulta`, retornando `(decimal.Decimal, error)`; (2) no empréstimo devolvido, usa `Devolucao()`; (3) no aberto, usa `ref`; (4) sentinelas para `Emprestimo{}` e para `ref` zero no aberto, sem panic; (5) testes em tabela e fuzz de 30s; (6) D-14 e o mapa de módulos. Fora de escopo: persistência e migração 0002; `internal/storage/` e `migrations/`; mudar `CalcularMulta`; HTTP, JSON e `cmd/`; pagamento, abono, bloqueio, feriados e configuração; upgrade do `shopspring/decimal`; `time.Now` e `Clock`; governança. Decisões humanas: H1 — a multa é só calculada, num getter derivado, sem persistência, coluna ou migração; H2 — assinatura `Multa(ref time.Time)`: no devolvido usa `Devolucao()` e ignora `ref`, inclusive zero; no aberto usa `ref`; nada de `time.Now()` no domínio; H3 — as regras da DEL-001 continuam: R$ 1,00 por dia civil de atraso em São Paulo, teto de R$ 20,00 e multa zero com devolução até o vencimento.
- Criterios de aceite: devolvido (marcos de atraso, `ref` diferente e `ref` zero ignorados); aberto (marcos e `ref` zero com sentinela); `Emprestimo{}` com sentinela; fusos; horário de verão 2018/2019; empréstimo reidratado equivalente ao novo; equivalência com `CalcularMulta` e fuzz com oráculo; não mutação do empréstimo; sensores verdes; `go.mod` intocado; nada em `security_surface_paths`; validador aprovado.
- Riscos: divergência entre multa derivada e persistida (evitada por H1); mensagem enganosa se `ErrDevolucaoZero` fosse reaproveitado para `ref` zero; off-by-one da coluna `DATE` do vencimento.

## Architecture/Guardrails
- Invariantes aplicaveis: D-09 (valores em `decimal.Decimal`), D-10 (data civil em `America/Sao_Paulo`), D-11 (vencimento às 12:00 SP), D-12 (estado encapsulado, `Emprestimo{}` inválido) e D-13 (reidratação do vencimento `DATE`); `internal/domain` sem I/O e sem relógio. Decisões do guardrails (H4), sem necessidade de decisão humana: o nome fica `Multa`; `Emprestimo{}` retorna `ErrEmprestimoInvalido`, checado primeiro, coerente com `Devolver`; `ref` zero no aberto retorna a sentinela nova `ErrReferenciaZero` (`"emprestimo: data de referência zero"`); `ref` anterior ao início no aberto dá zero, sem erro, porque o vencimento é sempre posterior ao início; erros de `CalcularMulta` voltam sem wrap; receptor por valor.
- Arquivos provaveis de impacto: `internal/domain/emprestimo.go` (método `Multa` e `ErrReferenciaZero`); `internal/domain/emprestimo_multa_test.go` (tabela e fuzz); `docs-ai/DECISOES.md` (D-14); `docs-ai/03-MAPA-RAPIDO-MODULOS.md`.

## Dev
- Arquivos alterados: `internal/domain/emprestimo.go` (único arquivo de produção tocado; `go.mod`/`go.sum` sem mudança, `git diff --stat go.mod go.sum` vazio).
- Resumo tecnico: novo método `func (e Emprestimo) Multa(ref time.Time) (decimal.Decimal, error)` (receptor por valor, não escreve em `e`), posto depois dos getters. Ordem: `inicio.IsZero()` → `(decimal.Zero, ErrEmprestimoInvalido)`; devolvido → `CalcularMulta(e.vencimento, e.devolucao)` ignorando `ref` (inclusive zero); aberto com `ref` zero → `(decimal.Zero, ErrReferenciaZero)`; aberto → `CalcularMulta(e.vencimento, ref)`. Erros de `CalcularMulta` voltam sem wrap (e ela já devolve `decimal.Zero` em erro). Nova sentinela `ErrReferenciaZero = errors.New("emprestimo: data de referência zero")` no bloco `var` de erros, documentada. O doc comment de `Multa` registra a referência efetiva (`Devolucao()` se devolvido, `ref` se aberto), que `ref` é ignorado no devolvido, que `ref` até o dia civil do vencimento (em America/Sao_Paulo), inclusive antes do início, dá zero e que não há relógio no domínio. Importa `github.com/shopspring/decimal` (já no `go.mod`). Sem `time.Now`, sem `Clock`, sem campo novo, sem `float64`; `CalcularMulta`, `NovoEmprestimo`, `ReidratarEmprestimo`, `Devolver`, `internal/storage/` e `migrations/` intocados.
- Testes criados/ajustados: nenhum criado nem alterado pelo dev; o contrato é `internal/domain/emprestimo_multa_test.go` (do test-writer), que agora passa inteiro, incluindo `FuzzEmprestimoMulta`.
- Evidencias (codigo/teste): `gofmt -l ./internal` vazio; `go build ./...` exit 0; `go vet ./...` exit 0; `go test -count=1 ./...` → `ok cmd/biblioteca-bairro`, `ok internal/domain 0.011s`, `ok internal/storage`, `ok migrations`. Fuzz `go test -run='^$' -fuzz='^FuzzEmprestimoMulta$' -fuzztime=30s ./internal/domain` → `fuzz: elapsed: 30s, execs: 1973408 (0/sec), new interesting: 0 (total: 13)` / `PASS` / `ok internal/domain 30.095s`. Fuzz `go test -run='^$' -fuzz='^FuzzCalcularMulta$' -fuzztime=30s ./internal/domain` → `fuzz: elapsed: 30s, execs: 2567099 (0/sec), new interesting: 0 (total: 13)` / `PASS` / `ok internal/domain 30.095s`. `bin/pipeline-claude sensors` → `[ok] vet`, `[ok] test`, `[ok] build`, `[ok] vuln` (No vulnerabilities found), `[ok] mod_verify` (all modules verified); `Resultado: 5 sensor(es), 0 falha(s): aprovado.`

## Revisões
- code-reviewer: aprovado sem bloqueio. Sugestão 1 (doc comment de `Multa`): aplicada; o comentário agora diz que `ref` até o dia civil do vencimento, inclusive antes do início, dá zero. Sugestão 2 (o `_, _ =` no teste de não mutação): não aplicada, porque o teste já cumpre o que promete. Sugestão 3 (placeholders do report): resolvida pelo `docs-update` neste report.
- security-review e supply-chain-review: não acionados. Sinal SECURITY nao e SUPPLY_CHAIN nao: o diff de código só toca `internal/domain/`, fora de `security_surface_paths`, e `go.mod`/`go.sum` estão intocados; por isso `require_security: false`, sem seção Security.

## QA
- Resultado: aprovado. STATUS ok, com todos os itens de escopo e critérios de aceite cobertos. Sensores 5/5 (vet, test, build, vuln com `govulncheck` limpo, mod_verify ok). Sem scope creep.
- Cenarios executados: suíte em tabela de `internal/domain/emprestimo_multa_test.go` (devolvido com marcos e `ref` ignorado, aberto com marcos e `ref` zero, `Emprestimo{}`, fusos, horário de verão 2018/2019, reidratado equivalente ao novo, equivalência com `CalcularMulta`, não mutação). Fuzz de 30s: `FuzzEmprestimoMulta` com 1.973.408 execuções, PASS; `FuzzCalcularMulta` com 2.567.099 execuções, PASS. Bordas: o fuzz não gera `ref` zero (coberto pela tabela) e usa só instantes UTC (fusos e horário de verão cobertos pelas tabelas). Risco futuro: no devolvido, `Multa(agora)` devolve o valor congelado na devolução, e quem consome precisa saber; para o aberto, a camada de cima vai precisar de uma fonte de tempo.

## Docs
- Docs atualizados: `docs-ai/DECISOES.md` (nova D-14: multa derivada sem persistência, assinatura `Multa(ref)` e semântica de devolvido e aberto, escolhas do guardrails, domínio sem relógio com `ref` fornecido por quem chama, e convenção de status das deliveries); `docs-ai/03-MAPA-RAPIDO-MODULOS.md` (linha de `internal/domain/` com `Emprestimo.Multa` e `ErrReferenciaZero`, DEL-005); `docs-ai/deliveries/INDEX.md` (DEL-005 em revisão). Correção de status: DEL-001 a DEL-004, já mergeadas na `main` pelos PRs #1 a #4, passaram a `merged` no `INDEX.md` (estavam `draft` as três primeiras e `em revisão` a DEL-004) e no front matter dos reports (estavam `em revisão`); `pipeline_validate` continua aprovando as quatro. Os valores aceitos vêm do `INDEX.md` (`draft`, `em revisão`, `merged`, `revertida`); o template só traz `draft` como inicial. Convenção registrada na D-14: a delivery seguinte marca a anterior como `merged` depois de conferir o merge na `main`. `CLAUDE.md` sem mudança (nenhuma estrutura nem invariante mudou). Doc comments de `Multa` e `ErrReferenciaZero` já escritos pelo dev.

## Final Checklist
- [x] `bin/pipeline-claude sensors` verde (vet, test, build)
- [x] Escopo coberto por teste
- [x] Nada do fora de escopo foi implementado
- [x] Revisões exigidas pelos sinais concluídas
- [x] Documentação dos módulos tocados atualizada
- [x] Delivery registrada no INDEX.md
