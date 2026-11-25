DELIVERY_ID: DEL-017
TASK_CODE: PROJ-7
AGENT: docs-update
STATUS: ok

ESCOPO: CA7 (D-23, coleção Postman, mapa de módulos), S1 e S2 do 09-code-reviewer, seção Docs, status e Final Checklist do report. Nenhum código nem teste editado.

ARQUIVOS:
  - /home/rodrigo/workspace/biblioteca-bairro/docs-ai/DECISOES.md
    - D-23 nova, no fim do arquivo. Cobre: `disponivel` derivado de empréstimo aberto, calculado na consulta, fora de `domain.Livro`; forma F1 `(SELECT 1 FROM emprestimo e WHERE e.livro_id = l.id AND e.devolucao IS NULL LIMIT 1) IS NULL` e o motivo (o NOT EXISTS virava hashed SubPlan na listagem, O(abertos)); tipo de leitura `storage.LivroComDisponibilidade` e DTO `saidaLivroLido`, com POST /livros inalterado (`saidaLivro`); revisão da D-19 em dois pontos; risco BAIXO do 10-security-review com a condição de reavaliação (vários tokens ou login de usuário final); decisões do usuário de 23/11/2026 (P1 = A, P4, e P2/P3 nos padrões).
    - Nota de remissão para a D-23 no fim das Consequências da D-19.
  - /home/rodrigo/workspace/biblioteca-bairro/postman/biblioteca-bairro.postman_collection.json
    - As descrições de "Listar livros" e "Obter livro" passam a `{id, titulo, disponivel}`, com a semântica e um exemplo JSON inline.
    - `response` continua `[]`. A D-22 P4 proíbe exemplos de resposta salvos, então o exemplo vai na descrição.
    - POST /livros não mudou. O JSON continua válido: as aspas dentro das strings estão escapadas, e conferi a estrutura relendo as linhas 140-164.
  - /home/rodrigo/workspace/biblioteca-bairro/docs-ai/03-MAPA-RAPIDO-MODULOS.md: duas linhas DEL-017 (storage e http) em "Módulos existentes".
  - /home/rodrigo/workspace/biblioteca-bairro/docs-ai/deliveries/DEL-017/report.md
    - Título passa a "Disponibilidade por livro em GET /livros e GET /livros/{id}" (S1).
    - status: em revisão.
    - "- Docs atualizados:" preenchido.
    - Final Checklist marcado com evidência:
      - sensores: 12-qa-validation GATES 1-2;
      - escopo coberto: 12 COBERTURA_DO_ESCOPO;
      - fora de escopo: 12 SCOPE_CREEP nenhum;
      - revisões: 09 e 10, security ok;
      - docs: este handoff;
      - INDEX: linha DEL-017 presente.
  - /home/rodrigo/workspace/biblioteca-bairro/docs-ai/deliveries/INDEX.md: só a coluna Status da linha DEL-017 mudou, de draft para em revisão.
  - CLAUDE.md não mudou: estrutura e invariantes são os mesmos.

NAO_FEITO:
  - Título da linha DEL-017 no INDEX.md (parte da S1). Meu papel só permite mudar a coluna Status dessa linha ("Nada de linha nova, ID ou título"). A coluna Título segue "Contagem de exemplares disponíveis por livro".
    - Quem pode fazer: a sessão principal (docs-ai/deliveries/ é aberto) ou outra etapa autorizada.
    - Mudança: só essa célula, para "Disponibilidade por livro em GET /livros e GET /livros/{id}", igual ao título do report.
    - O validador não reclamou da divergência.

CONFIRMADO:
  pipeline_validate target=DEL-017 (saída real):
  {
    "report": "docs-ai/deliveries/DEL-017/report.md",
    "ok": true,
    "findings": []
  }
  - Rodei o validador pela ferramenta MCP, com o diff contra a base padrão (sem no_git), e não por `bin/pipeline-claude validate`, porque este subagente não tem shell. O Final Checklist foi marcado antes, com a evidência dos handoffs. Por isso o validador aprovou, ao contrário do "desmarcado esperado".

ASSUNCOES:
  - "Exemplos" na Postman foi lido como exemplo inline na descrição, por causa da D-22 P4 (sem exemplos salvos), que também está em 08-dev-implementation PARA_DOCS_UPDATE.

RISCOS:
  - Até alguém ajustar a célula de título no INDEX.md, os títulos do INDEX e do report ficam divergentes.

SINAIS:
  SECURITY: sim — já revisado (10-security-review): sem bloqueio; risco BAIXO registrado na D-23.
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff.
  MIGRATION: nao — migrations/ fora do diff.
  PERFORMANCE: sim — plano conferido (TestPlanoLeituraLivro), registrado na D-23.

PROXIMO: git-delivery
MOTIVO: CA7 fechado e validador aprovado. Antes do commit, a sessão principal deve retitular a célula Título da linha DEL-017 no INDEX.md, fora do papel do docs-update.
