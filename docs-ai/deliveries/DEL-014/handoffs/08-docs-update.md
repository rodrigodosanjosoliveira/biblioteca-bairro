DELIVERY_ID: DEL-014
TASK_CODE: DEL-014
AGENT: docs-update
STATUS: ok

CONFIRMADO:
  - pipeline_validate target=DEL-014, saída real:
      {"report": "docs-ai/deliveries/DEL-014/report.md", "ok": true, "findings": []}
  - docs-ai/DECISOES.md: D-20 nova ("Listagens de usuários e de empréstimos e detalhe de usuário, no contrato da D-19"), com rotas GET /usuarios, GET /usuarios/{id} e GET /emprestimos; contrato de paginação da D-19 sem variação; storage.LimiteListagem compartilhada (LimiteListagemLivros = LimiteListagem, helper listarPorID); código usuario_inexistente 404 só no detalhe, com o motivo (usuario_nao_encontrado já é 422); U1 (filtros depois, sem migração) e U2 (nome em lote aceito); risco MEDIO de exposição em lote com a condição de bloqueio (vários tokens ou login de usuário final → autorização por recurso antes nas três rotas); BAIXO de página de usuários de até ~6 MB, BAIXO de empréstimo inconsistente → 500 na página e BAIXO informativo do 404 vs 200.
  - Escolha D-20 nova em vez de extensão da D-19: traz decisões do usuário, código de erro novo e risco de dado pessoal próprios; a D-19 continua sendo a fonte do contrato, citada sem variação.
  - D-15 Consequências: listagens de usuários e de empréstimos e GET /usuarios/{id} registradas como entregues na DEL-014 (D-20); autenticação completa segue pendente e exige antes a autorização por recurso da D-20. D-19 Consequências: nota parenthetical apontando para a D-20.
  - docs-ai/03-MAPA-RAPIDO-MODULOS.md: duas linhas DEL-014 (internal/storage: ListarUsuarios, ListarEmprestimos, LimiteListagem, listarPorID; internal/http: listarUsuarios, obterUsuario, listarEmprestimos, montarSaidaEmprestimo, usuario_inexistente).
  - Report DEL-014 preenchido com Edit campo a campo: status em revisão, require_security true com seção Security (achados do handoff 06), QA ok com a prova com banco da sessão principal citada como tal (2026-11-08T23:55:40Z, 2553 pass, 0 fail, 0 skip) e a ressalva do QA sobre o critério, as duas sugestões cosméticas do code-reviewer como não implementadas, Final Checklist marcado com evidência dos handoffs 01, 04, 05, 06 e 07.
  - INDEX.md: só a coluna Status da linha DEL-014, de draft para em revisão. Marcação merged da DEL-013 (INDEX.md e DEL-013/report.md) mantida, não revertida.

ASSUNCOES:
  - CLAUDE.md não mudou: nem estrutura nem invariante mudaram.
  - O campo "Escopo fechado" do PM/Triage citava 404 usuario_nao_encontrado; acrescentei a nota da revisão do handoff 02 (usuario_inexistente) sem apagar o texto do triage.
  - Comentários de símbolos exportados: não editei código (internal/ fora do meu papel); as duas sugestões cosméticas ficam registradas como não implementadas.

ARQUIVOS:
  - docs-ai/DECISOES.md
  - docs-ai/03-MAPA-RAPIDO-MODULOS.md
  - docs-ai/deliveries/DEL-014/report.md
  - docs-ai/deliveries/INDEX.md (só Status da DEL-014)

RISCOS:
  - O risco MEDIO (U2) agora está na D-20 e na seção Security; a próxima delivery que introduzir múltiplos tokens precisa ler a D-20.
  - O sensor test padrão roda sem banco; o CI precisa manter BIBLIOTECA_TEST_DB_OBRIGATORIO=1.

SINAIS:
  SECURITY: sim — revisado no handoff 06, sem achado bloqueante; MEDIO (U2) registrado na D-20 e no report
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff
  MIGRATION: nao — nenhuma migração
  PERFORMANCE: nao — keyset pela PK, uma consulta por página

PROXIMO: git-delivery
MOTIVO: report aprovado pelo validador (ok: true, sem findings); documentação dos módulos tocados atualizada; segue para commit e PR.
