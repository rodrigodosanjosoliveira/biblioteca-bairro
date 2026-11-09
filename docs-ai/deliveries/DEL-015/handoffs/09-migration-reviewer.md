DELIVERY_ID: DEL-015
TASK_CODE: DEL-015
AGENT: migration-reviewer
STATUS: ok

RESULTADO: aprovado, sem BLOQUEIO. A 0003 é aditiva (3 CREATE INDEX). O down remove exatamente os 3 índices. O texto bate com o desenho do 03 e com a decisão U3 do 04. Há pendências de documentação para o docs-update (D-21 e runbook) e duas sugestões operacionais.

SCRIPTS_AVALIADOS:
  - migrations/0003_indices_listagem_emprestimo.up.sql (linhas 1-9)
  - migrations/0003_indices_listagem_emprestimo.down.sql (linhas 1-6)
  - apoio: migrations/indices_listagem_test.go, internal/storage/esquema_test.go:26-62, 249-255, 334-342, internal/migrate/executor.go:44-57, 320-364, docs-ai/RUNBOOK-MIGRACAO.md §3, §6, §7

MIGRATION_CHECK:
  Convenção e versionamento:
    - O par `0003_indices_listagem_emprestimo.{up,down}.sql` segue o padrão `NNNN_nome` e a versão 3 vem sem lacuna depois da 0002.
    - pipeline_diff em migrations/ lista só os 3 arquivos novos e não rastreados (up, down e indices_listagem_test.go). 0001, 0002 e controle/ estão intocados, então nenhum checksum aplicado muda.
  Idempotência e transação:
    - O executor envolve cada arquivo em BEGIN, set_config local de lock_timeout e statement_timeout, o arquivo e COMMIT (executor.go:325-364). O arquivo não traz BEGIN/COMMIT/SET próprios, o que está correto. CREATE INDEX comum é permitido em transação.
    - Sem `IF NOT EXISTS` no up e sem `IF EXISTS`/`CASCADE` no down, conforme o padrão do projeto (0001/0002, TestDown0001/0002). A repetibilidade vem de migracao_aplicada, checksum e advisory lock (D-16). Reaplicar à mão falha alto (42P07/42704), que é o desejado: um índice homônimo com outra definição não passa em silêncio.
    - A palavra recusada pela G7 não aparece no texto. Nenhuma linha começa com begin/commit/end/savepoint (todos os comentários começam com `--`). Isso é coberto por TestArquivosEmbutidosNaoControlamTransacao, que varre o FS.
  Locks e indisponibilidade:
    - CREATE INDEX comum toma SHARE em `emprestimo`. Leituras (ACCESS SHARE) seguem. INSERT/UPDATE/DELETE (ROW EXCLUSIVE) esperam até o COMMIT do arquivo.
    - lock_timeout (padrão 5s) vale só na primeira aquisição, no 1º CREATE. O 2º e o 3º já têm SHARE na mesma tabela e não fazem nova aquisição que conflite. Durante a espera pela aquisição, a fila do SHARE também segura as escritas novas, e esse tempo é limitado pelo lock_timeout.
    - statement_timeout (padrão 60s) vale por comando (runbook §7). No pior caso teórico, as escritas ficam bloqueadas por 3 × 60s. Abrir, devolver e renovar têm prazo de 5s na API e respondem erro enquanto isso durar.
    - Interpretação da regra "um DDL pesado por arquivo" (D7 do 03): é aceitável aqui. A regra existe por causa do ACCESS EXCLUSIVE × N. Aqui o lock é SHARE, as leituras seguem e a tabela tem porte de 10^4–10^5 linhas (construção na ordem de milissegundos a segundos). Dividir em 3 arquivos encurtaria cada janela, mas não o total, e criaria estado intermediário.
    - A premissa de tamanho em produção NÃO foi verificada. A barreira é a conferência (a) do runbook (U3).
    - Autovacuum em `emprestimo` (SHARE UPDATE EXCLUSIVE) conflita com SHARE. O autovacuum comum cede sozinho. O anti-wraparound não cede e faria o CREATE esperar até o lock_timeout e falhar, de forma limpa, com rollback. É um risco baixo, mas deve entrar na nota do runbook.
    - Não há coluna nova, NOT NULL, DROP de coluna ou reescrita de tabela.
  Compatibilidade com o binário anterior (deploy):
    - Os índices são invisíveis para o código: o binário anterior funciona igual com eles. O binário novo funciona sem eles, com as mesmas respostas, só que com planos piores (Bitmap/Sort) nos filtros novos.
    - Não há acoplamento de ordem para a corretude.
  Ordem de deploy e rollback:
    - Deploy: `migrate up` (job único, em horário de pouco uso, depois de conferir o tamanho) → binário novo. É a ordem recomendada para o binário novo já subir com os planos bons.
    - Rollback: reverter o binário primeiro. O down é OPCIONAL e, em geral, desnecessário: o binário antigo convive com os índices, cujo único custo é a escrita.
    - Se o down for rodado: é manual por psql (runbook §3), com `DELETE FROM migracao_aplicada WHERE versao = 3` na mesma transação. DROP INDEX toma ACCESS EXCLUSIVE em `emprestimo` e bloqueia até leituras até o COMMIT. É rápido, mas fora do executor não há lock_timeout. O operador deve definir `SET lock_timeout` na sessão psql antes, o que vale aqui (S1).
    - Não há perda de dado em nenhum sentido: o down não é destrutivo e não exige pg_dump pela D-13.
  Reversão:
    - O down tem 3 DROP INDEX, em ordem inversa, com exatamente os 3 nomes criados no up.
    - Não cita os índices da 0001 nem a PK.
    - Não deixa nada órfão: índice não cria sequence, tipo nem constraint (não é UNIQUE).
    - Isso foi provado no banco pelo subteste `TestMigracaoUpDownUp/down_só_da_0003`. Ele roda só o down da 0003, confere que o catálogo volta aos índices da 0002 (`indicesAte0002`) e reaplica o up.
  Conformidade com a U3 (handoff 04):
    - O up roda pelo executor e não usa a opção online. Isso está declarado no comentário das linhas 5-6.
    - O comentário cita "(D-16 G7)" como motivo de não usar a opção online, mas não diz que a 0003 é a exceção U3/D-21 à regra "índice em tabela com dados é manual".
    - Isso não bloqueia: o comentário é informativo, e o registro da exceção cabe à D-21 e ao runbook.
    - Se quiserem citar "exceção U3, D-21; conferir runbook §6" no comentário, é AGORA ou nunca. Depois de aplicado, qualquer byte do .up.sql quebra o checksum (S2).
    - Pendência para o docs-update: `docs-ai/DECISOES.md` e `docs-ai/RUNBOOK-MIGRACAO.md` ainda não citam D-21 (grep sem resultado). O runbook §6 (linhas 85-87) ainda diz "Índice em tabela com dados é procedimento manual". O docs-update precisa registrar na D-21 as decisões U1-U3, D7 e o limite de ~1 milhão de linhas, com a exceção restrita à 0003. Também precisa ajustar o §6 e acrescentar a nota da 0003 com os passos (a) count(*) e pg_total_relation_size, (b) janela de pouco uso e (c) acima de ~1M linhas, procedimento manual. O qa-validation deve conferir.
  Sensor de teste (rodado aqui, sem banco):
    - pipeline_sensors test: ok, 2386 passaram, 0 falharam, `skipped: 220`.
    - Os pulados da camada de banco têm todos o motivo "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado". Entre eles estão TestMigracaoUpDownUp, TestEsquemaRestricoes, TestControle*, TestMigrate*ComBancoReal e TestBanco*. A saída veio truncada, mas TestPlanoListarEmprestimos também pula sem a variável.
    - Esta execução NÃO prova a migração no banco. A camada de banco fica como NÃO exercitada nesta execução, e o motivo do pulo é a variável ausente.
    - TestUp0003 e TestDown0003 são de texto, sem banco, e passaram aqui.
  Evidência com banco da SESSÃO PRINCIPAL (não reexecutada aqui):
    - Execução de 2026-11-09T00:54:52Z, no Postgres 16 em trust com o digest do CI e BIBLIOTECA_TEST_DB_OBRIGATORIO=1. Resultado: 3019 passaram, 0 falharam, 0 pulados.
    - JSON: /tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/qa015-test.json.
    - Conferi no JSON as linhas de pass: TestMigracaoUpDownUp (l. 10109), TestMigracaoUpDownUp/down_só_da_0003 (l. 10107), TestPlanoListarEmprestimos (l. 11597, 8,37s), TestUp0003 (l. 12081) e TestDown0003 (l. 12085). Nenhum evento skip ou fail no arquivo.
    - A prova de up → down → up no banco vem dessa execução da sessão principal.

BLOQUEIOS:
  - nenhum

SUGESTOES:
  - S1 [docs-ai/RUNBOOK-MIGRACAO.md §3 e nota da 0003, docs-update] — down da 0003 por psql:
    - abrir a sessão com `SET lock_timeout = '5s'` antes do BEGIN;
    - rodar só depois de reverter o binário, e só se for necessário;
    - registrar que o down é dispensável no rollback, porque o binário antigo convive com os índices.
  - S2 [migrations/0003_indices_listagem_emprestimo.up.sql:5-6, dev-implementation, opcional] — se quiserem rastrear a exceção no próprio arquivo, trocar "(D-16 G7)" por algo como "(exceção U3 à D-16 G7, D-21; runbook §6)". Isso só pode ser feito antes do primeiro `migrate up` em qualquer ambiente persistente. Sem a palavra proibida e sem linha começando por palavra de transação.
  - S3 [nota da 0003 no runbook, docs-update]:
    - antes do `migrate up`, conferir `pg_stat_progress_vacuum`/`pg_stat_activity` em busca de autovacuum anti-wraparound em `emprestimo`, que faria o CREATE esperar até o lock_timeout e falhar;
    - se falhar, reexecutar o `migrate up`, que é seguro porque houve rollback total do arquivo.

ASSUNCOES:
  - Não reexecutei a suíte com banco: o subagente não tem acesso ao banco. A prova no banco é a da sessão principal, citada acima.
  - Porte de 10^4–10^5 linhas em produção não verificado (herdado do 02/03/04).
  - Comportamento de lock segundo a documentação do PostgreSQL 16 (CREATE INDEX → SHARE; DROP INDEX → ACCESS EXCLUSIVE). Não medi isso em carga.

ARQUIVOS:
  - nenhum editado (revisor só lê).

RISCOS:
  - Se o operador pular a conferência de tamanho com tabela grande, as escritas em `emprestimo` ficam bloqueadas pela soma das 3 construções (até 3 × statement_timeout) e a API responde erro em abrir, devolver e renovar.
  - Enquanto o docs-update não ajustar a D-21 e o runbook §6, a documentação contradiz a 0003: o §6 ainda manda fazer índice em tabela com dados manualmente.
  - O down fora do executor, sem lock_timeout, pode enfileirar ACCESS EXCLUSIVE atrás de uma transação longa e bloquear até as leituras (S1).
  - Índices redundantes (simples da 0001 e compostos) até a delivery de remoção. O critério de remoção precisa de ajuste (S1 do code-reviewer).

SINAIS:
  SECURITY: sim — migrations/ e internal/storage/ em security_surface_paths. O security-review roda em paralelo, e a migração não toca tabela de autenticação, permissão ou credencial.
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff.
  MIGRATION: sim — revisada e aprovada: 0003 aditiva, lock SHARE, down exato. Pendências de documentação (D-21 e runbook §6) com o docs-update.
  PERFORMANCE: sim — o query-performance não está habilitado. O plano foi conferido pelo code-reviewer e pelo TestPlanoListarEmprestimos (que passou na sessão principal).

PROXIMO: qa-validation
MOTIVO: o query-performance não está habilitado e o security-review já roda em paralelo sobre o mesmo diff. Sem bloqueio na migração. S1 e S3 vão para o docs-update junto com a D-21 e o runbook §6. S2 é opcional e, se adotada, volta ao dev-implementation antes de qualquer `migrate up` persistente.
