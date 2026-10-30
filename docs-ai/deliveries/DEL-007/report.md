---
delivery_id: DEL-007
status: merged
data: 2026-10-29
require_security: true
require_release_ops: false
---

# DEL-007 — Atualizar o pipeline-claude de v1.0.0-rc.2 para v1.0.0-rc.4

<!--
Preencha cada campo no lugar de <preencher> e marque o checklist só com
evidência dos handoffs. Com require_security: true, acrescente a seção
Security (título de nível 2) com o campo "- Resultado:". Com
require_release_ops: true, acrescente a seção Release/Ops com o campo
"- Plano de rollback:". O validador reprova placeholder, item de checklist
aberto e marcadores de pendência em caixa alta.
-->

## PM/Triage
- Tipo: chore de governança, sem código Go.
- Escopo fechado: (1) `Makefile`: `PIPELINE_CLAUDE_VERSION` de v1.0.0-rc.2 para v1.0.0-rc.4; (2) arquivos gerenciados sincronizados por `bin/pipeline-claude sync` (`.claude/agents/code-reviewer.md`, `.claude/agents/docs-update.md`, `.claude/skills/fluxo/SKILL.md`, `.pipeline-claude.lock`); (3) `.pipeline-claude.json`: `Makefile`, `internal/http/` e `internal/config/` em `security_surface_paths`, o que resolve a pendência humana H10 da DEL-006. Os itens (2) e (3) estão em `no_agent_paths` e foram alterados pelo humano fora da sessão (commits `ea94f40` e `a5e955b`).
- Fora de escopo: `cmd/`, `internal/`, `migrations/`, `go.mod`, `go.sum`; remoção da entrada redundante `internal/http/middleware/`.
- Criterios de aceite: `bin/pipeline-claude doctor` sem erro (binário rc.4, sync em dia com o lock); sensores verdes; diff restrito ao escopo; nenhum código de produto alterado.
- Riscos: a rc.4 muda o fluxo (regra MESCLADAS e passo de status no índice); `make tools` executa binário de terceiros, com integridade garantida pelo sumdb.

## Architecture/Guardrails
- Invariantes aplicaveis: invariante 4 do `CLAUDE.md` (pipeline-claude fora do `go.mod`, instalado em `bin/` por `make tools` na versão fixada no `Makefile`); invariante 3 (`go.mod`/`go.sum` não mudam, então não há supply-chain-review).
- Arquivos provaveis de impacto: `Makefile`, `.pipeline-claude.json`, `.pipeline-claude.lock`, `.claude/agents/code-reviewer.md`, `.claude/agents/docs-update.md`, `.claude/skills/fluxo/SKILL.md`, `docs-ai/deliveries/`.

## Revisões
- code-reviewer: não aplicável, porque o diff não tem código Go (só configuração, texto de instrução gerenciado e `Makefile`).
- security-review: aprovado. Detalhes na seção Security.
- supply-chain-review: não aplicável (`go.mod`/`go.sum` sem mudança; mod_verify "all modules verified").

## Security
- Resultado: aprovado, sem achado CRÍTICO ou ALTO. govulncheck@v1.8.0: "No vulnerabilities found." `Makefile:6` fixa a tag exata (sem `@latest`) e a ferramenta segue fora do `go.mod`. Os arquivos gerenciados não mudaram o frontmatter `tools:`, nenhum revisor ganhou escrita e os gates foram mantidos. Os hashes de `security-review.md`, `supply-chain-review.md`, `git-delivery.md` e `.githooks/pre-push` não mudaram. `security_surface_paths` só ficou mais estrita.
- Riscos aceitos: BAIXO-1, `internal/http/middleware/` redundante em `security_surface_paths` (`.pipeline-claude.json:43` e `:51`), sem efeito, para limpeza futura fora da sessão; BAIXO-2, versão fixada por tag e não por hash, mitigada pelo sumdb enquanto `GONOSUMDB`/`GOPRIVATE` não cobrirem o módulo. Na regra 1 do `/fluxo`, o ID do índice entra num comando git, com risco mitigado por `--fixed-strings` e pelo padrão `^DEL-[0-9]{3,}$`.

## Dev
- Arquivos alterados: `Makefile`, `.pipeline-claude.json`, `.pipeline-claude.lock`, `.claude/agents/code-reviewer.md`, `.claude/agents/docs-update.md`, `.claude/skills/fluxo/SKILL.md`, mais `docs-ai/deliveries/DEL-007/report.md` e `docs-ai/deliveries/INDEX.md`. Não houve dev-implementation: as mudanças vieram do humano, via `pipeline-claude sync` e edição fora da sessão.
- Resumo tecnico: bump da versão fixada no `Makefile`. O sync atualizou 3 arquivos gerenciados (code-reviewer: a saída alimenta a seção Code Review; docs-update: passo 6 de status no índice com MESCLADAS; fluxo: regra 1, que calcula MESCLADAS via `git log --fixed-strings`) e o lock (`toolkit_version` rc.4, com hashes trocados só desses 3). A superfície de segurança foi ampliada.
- Testes criados/ajustados: nenhum, porque não há código Go. A cobertura vem dos gates determinísticos (doctor e sensores).
- Evidencias (codigo/teste): `bin/pipeline-claude doctor` com 0 erro(s), 0 aviso(s): "binário bin/pipeline-claude v1.0.0-rc.4", "sync 16 arquivo(s) gerenciado(s) em dia com .pipeline-claude.lock", "subagentes 12 subagente(s) ... conforme o TD-29". `bin/pipeline-claude sensors`: vet, test, build, vuln e mod_verify ok, 5 sensores, 0 falhas.

## QA
- Resultado: aprovado. 5 sensores verdes; o diff tem 8 arquivos, todos dentro do escopo ou artefatos da delivery; nada em `cmd/`, `internal/`, `migrations/`, `go.mod` nem `go.sum`.
- Cenarios executados: (1) pipeline_sensors: vet, test (7 pacotes ok), build, vuln, mod_verify; (2) pipeline_diff name_only contra `origin/main` (merge-base `94101e9`); (3) patch de `Makefile`, `.pipeline-claude.json`, `.pipeline-claude.lock` e `INDEX.md`; (4) patch de `.claude/` conferido contra os hashes do lock rc.4; (5) report conferido contra as `security_surface_paths` novas. Fuzz não se aplica.

## Docs
- Docs atualizados: este report e `docs-ai/deliveries/INDEX.md` (DEL-006 como `merged`, PR #7 em `94101e9` na `main`; DEL-007 como `em revisão`); `docs-ai/deliveries/DEL-006/report.md` (só o front matter, `status: merged`, igual ao índice, como foi feito com a DEL-005 no commit `0571245`). Nenhum doc de módulo foi tocado, porque nenhum módulo de código mudou. `CLAUDE.md`, `docs-ai/03-MAPA-RAPIDO-MODULOS.md` e `docs-ai/DECISOES.md` ficaram sem mudança.

## Final Checklist
- [x] `bin/pipeline-claude sensors` verde (vet, test, build)
- [x] Escopo coberto por teste (sem código Go: a cobertura vem dos gates `doctor` e `sensors`, e o QA conferiu o diff e os hashes do lock)
- [x] Nada do fora de escopo foi implementado
- [x] Revisões exigidas pelos sinais concluídas
- [x] Documentação dos módulos tocados atualizada (nenhum módulo de código tocado)
- [x] Delivery registrada no INDEX.md
