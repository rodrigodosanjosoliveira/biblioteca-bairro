DELIVERY_ID: DEL-015
TASK_CODE: DEL-015
AGENT: docs-update
STATUS: ok

DOCS_ATUALIZADOS:
  - docs-ai/DECISOES.md:
    - D-21 nova: contrato dos filtros situacao/usuario_id/livro_id por AND; rota GET /usuarios/{id}/emprestimos (ordem de erros, BuscarUsuario em toda página, usuario_id recusado); storage com FiltroEmprestimos e SQL por fragmentos constantes; código real de ErrListagemInvalida (500 erro_interno; bug_interno é só a causa no log, divergência apontada pelo QA no handoff 10); regra de vencido no domínio com $hoje = vencimentoParaDate(Ref); U1, U2, U3 (exceção restrita à 0003, ~1 milhão de linhas, conferência de tamanho antes); 0003 e D7 (três índices num arquivo); critério de remoção dos índices simples por evidência de plano (S1 do code-reviewer); folga do EXPLAIN; riscos: MEDIO que amplia a condição de bloqueio da D-20 à rota aninhada e aos filtros, dois BAIXO informativos, pior caso de lock 3 × statement_timeout, autovacuum anti-wraparound, vencido na virada do dia, índices redundantes.
    - Nota cruzada na D-16 G7 (exceção U3 da D-21) e nas Consequências da D-20.
  - docs-ai/RUNBOOK-MIGRACAO.md: §3 com SET lock_timeout antes do BEGIN no down por psql; §6 com a exceção restrita à 0003; §11 nova "Notas por versão" com a 0003: count(*) e pg_total_relation_size, procedimento manual acima de ~1 milhão de linhas (com o INSERT em migracao_aplicada versão 3), horário de pouco uso, checagem de autovacuum anti-wraparound (S3 do migration-reviewer), rollback só do binário com down dispensável e, se rodado, por psql com SET lock_timeout (S1 do migration-reviewer).
  - docs-ai/03-MAPA-RAPIDO-MODULOS.md: três linhas da DEL-015 (internal/storage, internal/http, migrations/0003).
  - docs-ai/deliveries/DEL-015/report.md: status em revisão; todas as seções preenchidas por Edit; seção Migration acrescentada; QA ok com a prova com banco da sessão principal citada como tal (2026-11-09T00:54:52Z, 3019 pass, 0 fail, 0 skip) e a ressalva do QA; Security e Migration dos handoffs 08 e 09; desvio aprovado S4; folga do EXPLAIN; sugestões não implementadas S2 e S3 do code-reviewer e S2 do migration-reviewer; checklist marcado com evidência dos handoffs 06-10.
  - docs-ai/deliveries/INDEX.md: só a coluna Status da linha DEL-015 (draft → em revisão). Marcação merged da DEL-014 (INDEX.md e DEL-014/report.md) mantida, sem reversão.
  - CLAUDE.md sem mudança (estrutura e invariantes iguais). Comentários dos símbolos exportados (Situacao, FiltroEmprestimos, ListarEmprestimos, ErrListagemInvalida) já escritos pelo dev; conferidos em internal/storage/repositorio.go:36-40, 158-209.

CONFIRMADO:
  - pipeline_validate target=DEL-015, saída real:
    {"report": "docs-ai/deliveries/DEL-015/report.md", "ok": true, "findings": []}
  - Código de ErrListagemInvalida conferido: internal/http/erros.go:34 (codErroInterno = "erro_interno"), :47 (causaBugInterno = "bug_interno"), :136 (ErrListagemInvalida em errosDeBug).
  - Nenhum arquivo em cmd/, internal/, migrations/ nem handoffs/ editado.

ASSUNCOES:
  - A seção Migration do report não existe no template; foi acrescentada (o validador aprovou).
  - O texto da D-19/D-20 ("500 bug_interno") não foi reescrito; a D-21 registra a correção.

ARQUIVOS:
  - docs-ai/DECISOES.md
  - docs-ai/RUNBOOK-MIGRACAO.md
  - docs-ai/03-MAPA-RAPIDO-MODULOS.md
  - docs-ai/deliveries/DEL-015/report.md
  - docs-ai/deliveries/INDEX.md

RISCOS:
  - Premissa de 10^4–10^5 linhas em produção não verificada; a barreira é a conferência do runbook §11.
  - Sugestão S2 do migration-reviewer (comentário do .up.sql) só é possível antes do primeiro migrate up persistente; decisão do humano no PR.

SINAIS:
  SECURITY: sim — revisado no handoff 08; MEDIO registrado na D-21 com a condição de bloqueio ampliada.
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff.
  MIGRATION: sim — revisada no handoff 09; D-21 e runbook §6/§11 atualizados.
  PERFORMANCE: sim — EXPLAIN em teste com banco; folga e critério de remoção registrados na D-21.

PROXIMO: git-delivery
MOTIVO: documentação coerente com o diff e report aprovado pelo validador; falta commit, push da branch task/DEL-015 e abertura do PR.
