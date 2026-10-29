---
delivery_id: DEL-001
status: em revisão
data: 2026-10-29
require_security: true
require_release_ops: false
---

# DEL-001 — Multa por atraso na devolução

<!--
Preencha cada campo no lugar de <preencher> e marque o checklist só com
evidência dos handoffs. Com require_security: true, acrescente a seção
Security (título de nível 2) com o campo "- Resultado:". Com
require_release_ops: true, acrescente a seção Release/Ops com o campo
"- Plano de rollback:". O validador reprova placeholder, item de checklist
aberto e marcadores de pendência em caixa alta.
-->

## PM/Triage
- Escopo fechado: tipo feature. (1) função pura `CalcularMulta` em `internal/domain`; (2) R$ 1,00 por dia de atraso; (3) teto de R$ 20,00; (4) devolução no dia do vencimento ou antes gera multa zero; (5) dia contado pela data civil em `America/Sao_Paulo`; (6) testes em tabela; (7) data zero (vencimento ou devolução) devolve erro. Fora de escopo: endpoint HTTP, persistência, entidades, pagamento, feriados, configuração por variável de ambiente e formatação de valores.
- Criterios de aceite: testes em tabela cobrindo os itens 1 a 7 passando em `go test ./...`; sensores de `bin/pipeline-claude sensors` verdes.
- Riscos: off-by-one de fuso/horário de verão (mitigado com data civil em São Paulo e contagem entre meias-noites UTC); arredondamento monetário (mitigado com `shopspring/decimal`); valor diário e teto fixos no código; primeiro pacote em `internal/`, que estabelece o padrão do domínio.
- Decisoes humanas: A1, biblioteca decimal (`shopspring/decimal`) por preferência do usuário, em vez da recomendação de `int64` em centavos; A2, data civil em `America/Sao_Paulo`; A5, erro sentinela para data zero; versão mantida em v1.4.0 mesmo com a v1.6.0 (18/10/2026) disponível.

## Architecture/Guardrails
- Invariantes aplicaveis: Go 1.24+; testes com a stdlib `testing`, em tabela; dependência externa com justificativa e revisão (`go.mod`/`go.sum` exigem `require_security` e `supply-chain-review`); nenhum segredo em código ou log; `internal/domain` sem I/O.
- Arquivos provaveis de impacto: `internal/domain/multa.go`, `internal/domain/multa_test.go`, `go.mod`, `go.sum`.
- Contrato: `CalcularMulta(vencimento, devolucao time.Time) (decimal.Decimal, error)`, com os erros sentinela `ErrVencimentoZero` e `ErrDevolucaoZero`; base de fusos embutida com `time/tzdata`; contagem de dias entre meias-noites UTC reconstruídas da data civil de São Paulo, dividindo a diferença Unix por 86400 (evita dias de 23h/25h no horário de verão).

## Dev
- Arquivos alterados: `internal/domain/multa.go` (novo), `internal/domain/multa_test.go` (novo), `go.mod` (require `github.com/shopspring/decimal v1.4.0`), `go.sum` (novo, 2 linhas da `decimal`).
- Resumo tecnico: `CalcularMulta` valida as datas zero, converte as duas datas para a data civil em `America/Sao_Paulo`, calcula os dias de atraso pela diferença entre meias-noites UTC e devolve `min(dias × R$ 1,00, R$ 20,00)` em `decimal.Decimal`; atraso zero ou negativo devolve `decimal.Zero`.
- Testes criados/ajustados: `TestCalcularMulta` (21 casos em tabela), teste de independência de fuso (5 fusos de vencimento × 5 fusos de devolução), testes dos erros sentinela, teste das mensagens de erro e `FuzzCalcularMulta` com oráculo independente baseado em `AddDate`.
- Evidencias (codigo/teste): `go test ./...` verde; `go test -fuzz=FuzzCalcularMulta -fuzztime=30s ./internal/domain` com cerca de 4,1 milhões de execuções sem falha (executado pelo `dev-implementation`).
- Code review: sem bloqueio no código; o único bloqueio era `require_security` desligado no report, resolvido no `docs-update`. Sugestão aplicada: oráculo independente no fuzz. Sugestões não aplicadas: fuso aleatório no fuzz e remoção do teste de mensagens.
- Supply chain: `github.com/shopspring/decimal` v1.4.0, licença MIT, 0 dependências transitivas, `mod_verify` ok, `govulncheck` ok; alternativa stdlib (`int64` em centavos) preterida por decisão humana; v1.6.0 existe, v1.4.0 mantida por decisão do usuário (versão madura; upgrade em delivery futura).

## Security
- Resultado: ok, nenhum achado bloqueante. O gatilho foi o auto_flags require_security (go.mod/go.sum); nenhum security_surface_path foi tocado. govulncheck v1.8.0 (sensor vuln): "No vulnerabilities found." Nova dependência direta github.com/shopspring/decimal fixada em v1.4.0, sem dependência transitiva (go.sum com 2 linhas, só a decimal). internal/domain/multa.go é função pura, sem entrada externa, I/O, autenticação, criptografia, segredo ou log, e os erros sentinela não vazam detalhe interno. Achados BAIXO, não bloqueantes: (1) panic na init de mustLoadLocation só ocorre se ZONEINFO apontar para um arquivo corrompido; em uso normal a base de fusos vem embutida via time/tzdata; (2) overflow int64 teórico em multa.go:43 para datas extremas, com resultado ainda limitado a [0, 20]; validar o intervalo de datas na futura camada HTTP.

## QA
- Resultado: aprovado; 7 de 7 itens do escopo cobertos por teste.
- Cenarios executados: sensores 5/5 ok: `vet`, `test`, `build`, `vuln` ("No vulnerabilities found.") e `mod_verify` ("all modules verified"); fuzz com `-fuzztime=30s`, cerca de 4,1 milhões de execuções ok (executado pelo `dev-implementation`).
- Riscos residuais: `time/tzdata` aumenta o binário em cerca de 450 KB; `decimal.Decimal` exposto na API pública do domínio; em JSON o tipo serializa como string (impacta o futuro endpoint); a persistência futura deve usar coluna `NUMERIC`; o nome do módulo no `go.mod` é "biblioteca-bairo" (pré-existente, fora do escopo) (corrigido na DEL-002).

## Docs
- Docs atualizados: `docs-ai/DECISOES.md` (D-09, valores monetários com `shopspring/decimal` v1.4.0; D-10, data civil em `America/Sao_Paulo` com `time/tzdata`); `docs-ai/03-MAPA-RAPIDO-MODULOS.md` (seção Módulos existentes com `internal/domain/`, `multa.go`, `CalcularMulta`). Os comentários de documentação dos símbolos exportados em `internal/domain/multa.go` já estavam completos. `CLAUDE.md` sem mudança (nenhuma estrutura nem invariante global alterado).

## Final Checklist
- [x] `bin/pipeline-claude sensors` verde (vet, test, build)
- [x] Escopo coberto por teste
- [x] Nada do fora de escopo foi implementado
- [x] Revisões exigidas pelos sinais concluídas
- [x] Documentação dos módulos tocados atualizada
- [x] Delivery registrada no INDEX.md
