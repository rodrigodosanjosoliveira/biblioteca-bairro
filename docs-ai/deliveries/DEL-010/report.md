---
delivery_id: DEL-010
status: em revisão
data: 2026-11-05
require_security: true
require_release_ops: false
---

# DEL-010 — Atualizar o pipeline-claude de v1.0.0-rc.5 para v1.0.0

<!--
Preencha cada campo no lugar de <preencher> e marque o checklist só com
evidência dos handoffs. Com require_security: true, acrescente a seção
Security (título de nível 2) com o campo "- Resultado:". Com
require_release_ops: true, acrescente a seção Release/Ops com o campo
"- Plano de rollback:". O validador reprova placeholder, item de checklist
aberto e marcadores de pendência em caixa alta.
-->

## PM/Triage
- Escopo fechado: chore sem código Go: Makefile (PIPELINE_CLAUDE_VERSION rc.5 para v1.0.0); upgrade feito fora da sessão (.pipeline-claude.lock só toolkit_version; .pipeline-claude.json sem query-performance em editor_agents); DEL-009 como merged no INDEX e no report. Fora: cmd/, internal/, migrations/, go.mod, go.sum, .claude/, .mcp.json, editar .claude/, json ou lock na sessão, pôr cmd/ e internal/migrate/ em security_surface_paths (pendência DEL-008), retrofit de reports
- Criterios de aceite: `bin/pipeline-claude doctor` com 0 erro(s) e 0 aviso(s); sensores verdes (vet, test, build, vuln, mod_verify); diff restrito aos 3 arquivos de configuração (Makefile, .pipeline-claude.lock, .pipeline-claude.json) e aos artefatos da delivery; validador aprovando o report
- Riscos: BAIXO-2 (Makefile instala pela tag exata sem hash, mitigado pelo sumdb, aceito na DEL-007); pendência da DEL-008 segue aberta: cmd/ e internal/migrate/ fora de security_surface_paths. Sinais: SECURITY sim (Makefile em security_surface_paths), SUPPLY_CHAIN nao, MIGRATION nao, PERFORMANCE nao

## Architecture/Guardrails
- Invariantes aplicaveis: STATUS ok. CLAUDE.md inv. 4 / 01-INVARIANTES §2.3 (pipeline-claude fora do go.mod, versão fixada no Makefile); §3.2 (Makefile em security_surface_paths, require_security true); §4.2 (.pipeline-claude.json e lock alterados só fora da sessão); §4.3/§4.4 (branch task/DEL-010 e PR); §4.5 (report e validador). Remover query-performance de editor_agents cumpre a D-04 (revisores sem escrita; só editor_agents editam); nenhum subagente com esse nome existe, logo não há privilégio novo nem necessidade de decisão nova. Rota: data-model, test-writer, dev-implementation e code-reviewer pulados, sem código Go (precedentes DEL-007 e DEL-009)
- Arquivos provaveis de impacto: Makefile; .pipeline-claude.lock (só toolkit_version); .pipeline-claude.json (editor_agents); docs-ai/deliveries/INDEX.md; docs-ai/deliveries/DEL-009/report.md (status); docs-ai/deliveries/DEL-010/report.md

## Dev
- Arquivos alterados: Makefile (PIPELINE_CLAUDE_VERSION v1.0.0-rc.5 para v1.0.0); .pipeline-claude.lock (só toolkit_version, nenhum hash mudou); .pipeline-claude.json (sai "query-performance" de editor_agents); artefatos da delivery (INDEX.md, DEL-009/report.md status, DEL-010/report.md)
- Resumo tecnico: sem dev-implementation; mudanças aplicadas pelo humano fora da sessão via `make tools`, `bin/pipeline-claude sync` e edição de editor_agents. A remoção de query-performance de editor_agents resolve o aviso do doctor registrado na DEL-008 (anomalia 3) e nos riscos da DEL-009. Nenhum código Go alterado
- Testes criados/ajustados: nenhum (sem código Go; precedentes DEL-007 e DEL-009)
- Evidencias (codigo/teste): `bin/pipeline-claude doctor`: "Resultado: 0 erro(s), 0 aviso(s)"; sync com 16 arquivos gerenciados em dia com o lock; 12 subagentes conforme o TD-29; binário bin/pipeline-claude v1.0.0; aviso de editor_agents/query-performance ausente. pipeline_sensors ok=true (vet, test, build, vuln, mod_verify com exit 0)

## Code Review
- Resultado: não aplicável: diff sem código Go, code-reviewer pulado (precedentes DEL-007 e DEL-009)
- Bloqueios e sugestões: nenhum

## QA
- Resultado: STATUS ok. pipeline_sensors ok=true; diff com exatamente 5 arquivos, sem scope creep
- Cenarios executados: vet exit 0; test exit 0 (8 pacotes ok, cached, nenhum .go mudou); build exit 0; vuln "No vulnerabilities found."; mod_verify "all modules verified". Fuzz não se aplica (sem parsing, validação ou desserialização alterados)

## Docs
- Docs atualizados: docs-ai/deliveries/DEL-010/report.md preenchido; docs-ai/deliveries/INDEX.md (DEL-009 merged, DEL-010 em revisão); status da DEL-009 em docs-ai/deliveries/DEL-009/report.md como merged. CLAUDE.md, 03-MAPA-RAPIDO-MODULOS.md e DECISOES.md sem mudança (nenhum módulo, invariante ou decisão nova)

## Final Checklist
- [x] `bin/pipeline-claude sensors` verde (todos os sensores do .pipeline-claude.json)
- [x] Escopo coberto por teste
- [x] Nada do fora de escopo foi implementado
- [x] Revisões exigidas pelos sinais concluídas
- [x] Documentação dos módulos tocados atualizada
- [x] Delivery registrada no INDEX.md

Nota: "Escopo coberto por teste" não se aplica a teste de unidade (sem código Go); coberto pelos gates: sensores verdes e doctor com 0 erro(s) e 0 aviso(s), como na DEL-007 e DEL-009.

## Security
- Resultado: STATUS ok, aprovado. pipeline_diff contra origin/main: 5 arquivos. .pipeline-claude.json: um único hunk, que remove "query-performance" de editor_agents; protected_paths, no_agent_paths, security_surface_paths, auto_flags, sensors e enabled_modules inalterados; a lista de quem escreve só encolheu. Lock: só toolkit_version, nenhum hash mudou. Makefile: tag exata, sem @latest, instala em bin/ fora do go.mod. Sem segredos e sem CI no diff. govulncheck: "No vulnerabilities found." Achado: só o BAIXO-2 (tag sem hash, mitigada pelo sumdb), já aceito na DEL-007
