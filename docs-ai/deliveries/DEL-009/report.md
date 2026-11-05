---
delivery_id: DEL-009
status: merged
data: 2026-11-04
require_security: true
require_release_ops: false
---

# DEL-009 — Atualizar o pipeline-claude de v1.0.0-rc.4 para v1.0.0-rc.5

<!--
Preencha cada campo no lugar de <preencher> e marque o checklist só com
evidência dos handoffs. Com require_security: true, acrescente a seção
Security (título de nível 2) com o campo "- Resultado:". Com
require_release_ops: true, acrescente a seção Release/Ops com o campo
"- Plano de rollback:". O validador reprova placeholder, item de checklist
aberto e marcadores de pendência em caixa alta.
-->

## PM/Triage
- Escopo fechado: chore sem código Go: Makefile (PIPELINE_CLAUDE_VERSION rc.4 para rc.5); arquivos do sync feito fora da sessão (.claude/agents/docs-update.md, .claude/agents/pm-triage.md, .claude/skills/fluxo/SKILL.md, .pipeline-claude.lock, _template/report.md); INDEX e report da DEL-008 como merged. Fora: cmd/, internal/, migrations/, go.mod, go.sum, .pipeline-claude.json (pendência da DEL-008), .mcp.json, edição de .claude/ ou lock na sessão, retrofit de reports antigos
- Criterios de aceite: Makefile com PIPELINE_CLAUDE_VERSION v1.0.0-rc.5; `bin/pipeline-claude doctor` com 0 erro e sync em dia com o .pipeline-claude.lock; sensores verdes (vet, test, build, vuln, mod_verify); go.mod/go.sum, cmd/, internal/ e migrations/ intocados; DEL-008 como merged; validador aprovando o report
- Riscos: BAIXO-1 (instrução nova no passo 2 de .claude/agents/docs-update.md sobre origem das recusas); BAIXO-2 (Makefile instala pela tag exata sem hash, mitigado pelo sumdb, aceito na DEL-007); aviso pré-existente do doctor sobre editor_agents: query-performance; reports antigos sem a seção Code Review (retrofit fora de escopo). Sinais: SECURITY sim (Makefile em security_surface_paths), SUPPLY_CHAIN nao, MIGRATION nao, PERFORMANCE nao

## Architecture/Guardrails
- Invariantes aplicaveis: STATUS ok. CLAUDE.md inv. 4 / 01-INVARIANTES §2.3 (pipeline-claude fora do go.mod, instalado em bin/ por `make tools` na versão do Makefile); §2.2 (go.mod/go.sum intocados); §3.2 (Makefile em security_surface_paths, require_security true); §4.2 (.claude/ e lock alterados só fora da sessão); §4.5 (validador antes do PR). Rota: data-model, test-writer, dev-implementation e code-reviewer pulados (sem código Go, mudanças aplicadas pelo humano; precedente DEL-007)
- Arquivos provaveis de impacto: Makefile; .claude/agents/docs-update.md; .claude/agents/pm-triage.md; .claude/skills/fluxo/SKILL.md; .pipeline-claude.lock (toolkit_version e 3 hashes); docs-ai/deliveries/_template/report.md; docs-ai/deliveries/INDEX.md; docs-ai/deliveries/DEL-008/report.md (status); docs-ai/deliveries/DEL-009/report.md

## Dev
- Arquivos alterados: Makefile (PIPELINE_CLAUDE_VERSION v1.0.0-rc.4 para v1.0.0-rc.5); .claude/agents/docs-update.md, .claude/agents/pm-triage.md, .claude/skills/fluxo/SKILL.md e .pipeline-claude.lock (toolkit_version e 3 hashes), atualizados pelo sync; docs-ai/deliveries/_template/report.md (seção nova ## Code Review, placeholder `<escopo>`, linha nova do checklist); artefatos da delivery (INDEX.md, DEL-008/report.md status, DEL-009/report.md)
- Resumo tecnico: sem dev-implementation; mudanças aplicadas pelo humano fora da sessão via `make tools` e `bin/pipeline-claude sync`. Nenhum código Go alterado
- Testes criados/ajustados: nenhum (sem código Go; precedente DEL-007)
- Evidencias (codigo/teste): `bin/pipeline-claude doctor`: 0 erro(s), 1 aviso(s); sync "16 arquivo(s) gerenciado(s) em dia com .pipeline-claude.lock"; binário "bin/pipeline-claude v1.0.0-rc.5". pipeline_sensors ok=true (vet, test, build, vuln, mod_verify com exit 0)

## Code Review
- Resultado: não aplicável: diff sem código Go, code-reviewer pulado (precedente DEL-007)
- Bloqueios e sugestões: nenhum

## QA
- Resultado: STATUS ok. pipeline_sensors ok=true; diff contra origin/main com exatamente os arquivos do escopo, sem scope creep
- Cenarios executados: vet exit 0; test exit 0 (8 pacotes ok); build exit 0; vuln "No vulnerabilities found."; mod_verify "all modules verified". Fuzz não se aplica (sem parsing, validação ou desserialização alterados)

## Docs
- Docs atualizados: docs-ai/deliveries/DEL-009/report.md preenchido; docs-ai/deliveries/INDEX.md (DEL-008 merged, DEL-009 em revisão); status da DEL-008 em docs-ai/deliveries/DEL-008/report.md como merged. CLAUDE.md, 03-MAPA-RAPIDO-MODULOS.md e DECISOES.md sem mudança (nenhum módulo, invariante ou decisão nova)

## Final Checklist
- [x] `bin/pipeline-claude sensors` verde (todos os sensores do .pipeline-claude.json)
- [x] Escopo coberto por teste
- [x] Nada do fora de escopo foi implementado
- [x] Revisões exigidas pelos sinais concluídas
- [x] Documentação dos módulos tocados atualizada
- [x] Delivery registrada no INDEX.md

## Security
- Resultado: STATUS ok, aprovado com 2 achados BAIXO. govulncheck: "No vulnerabilities found." BAIXO-1: o passo 2 novo de .claude/agents/docs-update.md manda distinguir recusas pela origem e insistir com Edit nas que não começam por "BLOQUEADO pelo pipeline-claude"; não amplia capacidade (hook e permissions.deny continuam valendo); sugestão ao upstream de restringir a regra ao report da delivery. BAIXO-2: Makefile instala pela tag exata sem hash, mitigado pelo sumdb, já aceito na DEL-007. Frontmatter tools: de pm-triage.md e docs-update.md inalterado; hashes do lock de security-review.md, supply-chain-review.md, git-delivery.md e .githooks/pre-push inalterados; sem segredos; CI não tocado
