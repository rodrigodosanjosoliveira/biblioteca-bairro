DELIVERY_ID: DEL-015
TASK_CODE: DEL-015
AGENT: data-model
STATUS: ok

DECISOES_DO_USUARIO (recebida pela sessão principal; registrar na D-21 como U3 da DEL-015):
  - U3 "Exceção pelo executor": resolve o conflito apontado em RISCOS do handoff 03 (runbook §6 e D-16 G7: "índice em tabela com dados é procedimento manual"). A 0003 (`0003_indices_listagem_emprestimo`) roda pelo `migrate up`, como exceção registrada na D-21. O runbook (`docs-ai/RUNBOOK-MIGRACAO.md` §6 e nota da 0003) passa a mandar:
      (a) conferir o tamanho da tabela antes (`SELECT count(*) FROM emprestimo` e `pg_total_relation_size('emprestimo')`);
      (b) rodar em horário de pouco uso;
      (c) acima de ~1 milhão de linhas, NÃO usar o executor: usar o procedimento manual fora dele (psql + registro manual, D-16 G7).
  - A premissa de 10^4–10^5 linhas em produção continua NÃO verificada; o usuário não a confirmou nem a contestou. A conferência (a) do runbook é a única barreira.

EFEITO SOBRE O HANDOFF 03:
  - Esquema, índices, nomes, up/down, planos esperados, testes exigidos e mudanças no esquema_test.go: sem alteração.
  - O risco "conflito de documentação" sai de aberto para decidido (U3); o que sobra é trabalho do docs-update (D-21 com U1, U2, U3 e D7 do 03; ajuste do runbook §6 e nota da 0003) e conferência do migration-reviewer de que o texto bate com U3.
  - A decisão D7 do 03 (três CREATE INDEX num só arquivo) segue valendo e entra na mesma exceção da D-21.

CONFIRMADO:
  - Texto conflitante: `docs-ai/RUNBOOK-MIGRACAO.md:85-87` e `docs-ai/DECISOES.md:435-438` (G7).
  - Handoff anterior desta etapa: `docs-ai/deliveries/DEL-015/handoffs/03-data-model.md`.

ASSUNCOES:
  - Premissa de tamanho (10^4–10^5) não verificada (sem acesso ao banco de produção).

ARQUIVOS:
  - nenhum editado. Previstos para docs-update: docs-ai/DECISOES.md (D-21 com U3), docs-ai/RUNBOOK-MIGRACAO.md (§6 e nota da 0003).

RISCOS:
  - Se o operador pular a conferência de tamanho e a tabela for grande, escritas em `emprestimo` ficam bloqueadas pela soma das três construções e a API responde erro em abrir/devolver/renovar (prazo de 5s).
  - A exceção abre precedente para a G7; a D-21 deve deixá-la restrita à 0003, com o limite de ~1 milhão de linhas.

SINAIS:
  SECURITY: sim — migração e SQL novo em security_surface_paths (inalterado).
  SUPPLY_CHAIN: nao — sem mudança em go.mod/go.sum.
  MIGRATION: sim — 0003 pelo executor como exceção U3; runbook a ajustar.
  PERFORMANCE: sim — EXPLAIN das 16 combinações em teste com banco (inalterado).

PROXIMO: test-writer
MOTIVO: o conflito com a G7 foi decidido pelo usuário (U3) sem mudar o modelo; seguem os testes definidos no handoff 03.
