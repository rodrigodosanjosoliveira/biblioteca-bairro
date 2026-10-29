---
delivery_id: DEL-002
status: merged
data: 2026-10-29
require_security: true
require_release_ops: false
---

# DEL-002 — Corrigir nome do módulo no go.mod

<!--
Preencha cada campo no lugar de <preencher> e marque o checklist só com
evidência dos handoffs. Com require_security: true, acrescente a seção
Security (título de nível 2) com o campo "- Resultado:". Com
require_release_ops: true, acrescente a seção Release/Ops com o campo
"- Plano de rollback:". O validador reprova placeholder, item de checklist
aberto e marcadores de pendência em caixa alta.
-->

## PM/Triage
- Escopo fechado: tipo bugfix. Trocar a diretiva `module` em `go.mod:1` de `github.com/rodrigodosanjosoliveira/biblioteca-bairo` para `github.com/rodrigodosanjosoliveira/biblioteca-bairro`, o nome real do repositório. Fora de escopo: dependências, a diretiva `go 1.24`, `cmd/` e `internal/`, o histórico da DEL-001, `no_agent_paths`, rebase e merge.
- Criterios de aceite: (1) nome do módulo correto no `go.mod`; (2) nenhuma ocorrência do caminho antigo em código ou build; (3) `go build`, `go vet`, `go test` e sensores verdes; (4) `go.sum` inalterado; (5) report com `require_security: true`.
- Riscos: consumidores externos do caminho antigo (nenhum conhecido; o projeto é aplicação, não biblioteca); branch `task/DEL-002` empilhada sobre `task/DEL-001` (PR #1 não mergeado): mergear o #1 primeiro e rebasear a DEL-002; caches locais de build/gopls com o nome antigo.

## Architecture/Guardrails
- Invariantes aplicaveis: Go 1.24+ mantido; mudança em `go.mod` exige `require_security` e `supply-chain-review`; o pipeline-claude fica fora do `go.mod`.
- Arquivos provaveis de impacto: `go.mod` (só a linha 1).
- Abordagem: `go mod edit -module=github.com/rodrigodosanjosoliveira/biblioteca-bairro`, sem `go mod tidy` nem `go get`. Sem teste novo: não há mudança de comportamento em runtime, e o gate é build/vet/test/sensores.

## Dev
- Arquivos alterados: `go.mod` (linha 1, diretiva `module`).
- Resumo tecnico: executado `go mod edit -module=github.com/rodrigodosanjosoliveira/biblioteca-bairro`; `git diff go.mod` mostra só a linha 1; `go.sum` sem diff; `go list -m` devolve `github.com/rodrigodosanjosoliveira/biblioteca-bairro`.
- Testes criados/ajustados: nenhum (sem mudança de runtime, conforme a arquitetura); a suíte existente roda sem alteração.
- Evidencias (codigo/teste): `go build ./...`, `go vet ./...` e `go test ./...` ok; a busca pelo caminho antigo só encontra `docs-ai/deliveries/DEL-001/report.md:45` (registro histórico); nenhum import Go usa o caminho antigo.
- Code review: sem bloqueio.
- Supply chain: nenhuma dependência nova ou alterada; `go.sum` sem diff; `mod_verify` e `vuln` ok; pipeline-claude continua fora do `go.mod`.

## Security
- Resultado: Sem achados (nenhum CRITICO/ALTO/MEDIO/BAIXO). A superfície revisada, com diff contra task/DEL-001, foi go.mod (só a diretiva `module`, de biblioteca-bairo para biblioteca-bairro; `go 1.24`, `require github.com/shopspring/decimal v1.4.0` e go.sum ficaram iguais, sem toolchain/replace). docs-ai/deliveries/INDEX.md e o report DEL-002 são só documentação. govulncheck (sensor vuln): "No vulnerabilities found." go mod verify (sensor mod_verify): "all modules verified". Nenhum import Go usa o caminho antigo do módulo.

## QA
- Resultado: aprovado; critérios de aceite 1 a 4 ok (o 5 é atendido por este report).
- Cenarios executados: sensores 5/5 ok: `vet`, `test`, `build`, `vuln` ("No vulnerabilities found.") e `mod_verify` ("all modules verified"). O escopo é coberto pelo gate build/vet/test/sensores, já que a mudança não altera comportamento.
- Riscos residuais: consumidores externos do caminho antigo (nenhum conhecido); ordem de merge (PR #1 antes, depois rebase da DEL-002); caches locais de build/gopls com o nome antigo podem exigir limpeza.

## Docs
- Docs atualizados: `docs-ai/deliveries/DEL-001/report.md:45` recebeu, ao final do item, a nota "(corrigido na DEL-002)", sem reescrever o texto histórico. `CLAUDE.md`, `README.md`, `docs-ai/DECISOES.md` e `docs-ai/03-MAPA-RAPIDO-MODULOS.md` ficaram sem mudança: nenhum cita o caminho do módulo, e nenhuma estrutura, invariante ou decisão mudou. Nenhum símbolo exportado foi alterado.

## Final Checklist
- [x] `bin/pipeline-claude sensors` verde (vet, test, build)
- [x] Escopo coberto por teste
- [x] Nada do fora de escopo foi implementado
- [x] Revisões exigidas pelos sinais concluídas
- [x] Documentação dos módulos tocados atualizada
- [x] Delivery registrada no INDEX.md
