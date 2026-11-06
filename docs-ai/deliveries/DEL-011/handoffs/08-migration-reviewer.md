DELIVERY_ID: DEL-011
TASK_CODE: DEL-011
AGENT: migration-reviewer
STATUS: ok

BLOQUEIOS:
  - nenhum.

SCRIPTS_AVALIADOS:
  - migrations/0002_renovacao_emprestimo.up.sql
  - migrations/0002_renovacao_emprestimo.down.sql
  - (contexto) migrations/0001_esquema_emprestimo.up.sql, migrations/migrations.go, migrations/renovacao_test.go, internal/migrate/executor.go (emTx, l. 320-357)

MIGRATION_CHECK:
  Convenção e versionamento:
    - Numeração NNNN do projeto (RUNBOOK-MIGRACAO.md:92), 0002 sem lacuna depois da 0001, em par up/down.
    - pipeline_diff com paths ["migrations/"] (base origin/main, merge-base 1ad40d3): só três arquivos novos e não rastreados (0002 up, 0002 down e renovacao_test.go). A 0001 e controle/ não foram alteradas. Nenhuma migração aplicada mudou retroativamente.
  Transação e idempotência:
    - Transação explícita: fica a cargo do executor. emTx (internal/migrate/executor.go:320-357) abre BEGIN, aplica lock_timeout e statement_timeout com set_config local e faz COMMIT conferido. A convenção do projeto proíbe BEGIN/COMMIT no arquivo (migrations/controle_test.go:129-157), e o arquivo cumpre. Assim, coluna e CHECK são atômicos: falha no meio desfaz tudo.
    - Proteção: sem IF NOT EXISTS no up e sem IF EXISTS no down. Aceito: a idempotência vem do controle de versão do executor (migracao_aplicada mais advisory lock), e o projeto exige que o down falhe alto se o esquema não for o esperado (TestDown0002, TestDown0001). ADD CONSTRAINT não aceita IF NOT EXISTS, e um ADD COLUMN IF NOT EXISTS isolado esconderia esquema divergente.
  Locks e indisponibilidade:
    - ADD COLUMN integer NOT NULL DEFAULT 0: o default é constante, então no PG 11+ só mexe no catálogo, sem reescrever a tabela. As linhas existentes leem 0.
    - ADD CONSTRAINT CHECK validado no mesmo ALTER: segura ACCESS EXCLUSIVE em emprestimo durante uma varredura completa (sem reescrita). Aceito porque a tabela é pequena (biblioteca de bairro, 02:68) e porque o lock_timeout e o statement_timeout do executor (D-16) limitam a espera pela fila e a duração. NOT VALID mais VALIDATE foi recusado de propósito no 02:41. Não medi o volume de produção.
    - Sem índice novo, então CONCURRENTLY não se aplica.
    - Expand and contract: o up só expande. O binário antigo continua funcionando com o esquema novo, porque nenhum SELECT * ou RETURNING * aparece em internal/ (Grep) e o INSERT antigo não cita a coluna, que recebe o DEFAULT 0. Logo a ordem sem indisponibilidade é: migrate up 0002, depois o deploy do binário novo. O inverso dá 42703 (500) em toda leitura de empréstimo.
  Reversão:
    - O down desfaz exatamente o up: DROP CONSTRAINT emprestimo_renovacoes_check e depois DROP COLUMN renovacoes, no mesmo ALTER. Não sobra sequence, tipo nem índice, porque o up não criou nenhum. A constraint cairia junto com a coluna de qualquer forma, e removê-la explicitamente falha alto se o nome divergir.
    - É destrutivo só no contador. Justificativa e plano registrados em 02:9, 03:72, no cabeçalho do próprio down e no RUNBOOK §3 (down manual com DELETE FROM migracao_aplicada na mesma transação). Pré-condição: reverter o binário antes. Efeito conhecido: os vencimentos já estendidos ficam, e uma nova subida da 0002 zera o contador, permitindo renovar de novo. Não há DROP TABLE nem TRUNCATE.
  Prova no banco (sensor test, rodado agora):
    - ok, exit 0, 1580 passaram, 0 falharam, skipped = 200, todos pelo mesmo motivo: "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado".
    - Pulados da camada de banco e de migração ligados a esta revisão: TestMigracaoUpDownUp e TestEsquemaRestricoes (internal/storage/esquema_test.go), TestAbrirEmprestimoSemRenovacoes (internal/storage/renovacao_test.go), TestBancoRenovacao e TestBancoRenovacaoConcorrente (internal/http/banco_renovacao_test.go), TestControle* (internal/migrate) e TestMigrate* (cmd). A saída do sensor veio truncada, e os demais pulados têm o mesmo motivo.
    - Por isso, pelo sensor, up, down, up, catálogo e CHECK NÃO foram exercitados contra o banco. Só os testes estáticos de texto rodaram (TestUp0002 e TestDown0002, pacote migrations, sem skip).
    - A única evidência com banco é a execução manual do 05-dev-implementation (Postgres 16 descartável, BIBLIOTECA_TEST_DB_OBRIGATORIO=1, 0 SKIP, TestMigracaoUpDownUp com PASS). Não a reproduzi.

SUGESTOES:
  - Runbook/report da DEL-011 (docs-update): registrar a ordem "migrate up 0002, depois o binário novo" e, no rollback, "binário antigo, depois o down 0002 com DELETE FROM migracao_aplicada WHERE versao = 2".
  - Comentário do up: o cabeçalho poderia citar que o CHECK faz uma varredura sob ACCESS EXCLUSIVE. Isso é opcional e não bloqueia.

CONFIRMADO:
  - Arquivos lidos com Read: os dois scripts da 0002, a 0001.up, migrations.go e renovacao_test.go. Executor via Grep (emTx e set_config de lock_timeout e statement_timeout).
  - Nenhum SELECT * nem RETURNING * em internal/, o que garante a compatibilidade do binário antigo com o esquema novo.

ASSUNCOES:
  - PostgreSQL 11 ou superior em produção, para o fast default (o 05 testou no 16).
  - A tabela emprestimo é pequena o bastante para a varredura do CHECK caber no statement_timeout do executor.

ARQUIVOS:
  - nenhum (revisor só leitura).

RISCOS:
  - A migração não foi provada no banco pelo sensor (200 pulados). O qa-validation precisa rodar `go test -count=1 -p 1 ./...` com BIBLIOTECA_TEST_DATABASE_URL e BIBLIOTECA_TEST_DB_OBRIGATORIO=1 e confirmar 0 SKIP e TestMigracaoUpDownUp com PASS.
  - Deploy fora de ordem (binário antes da migração) derruba as rotas de empréstimo com 500. Down com o binário novo no ar também.

SINAIS:
  SECURITY: sim — migrations/, internal/storage/ e internal/http/ em security_surface_paths; rota autenticada nova. A migração não toca tabela de autenticação ou credencial.
  SUPPLY_CHAIN: nao — go.mod e go.sum fora do diff.
  MIGRATION: sim — revisada aqui, aprovada sem bloqueio; prova em banco pendente para o qa-validation.
  PERFORMANCE: nao — sem índice novo; query-performance não está em enabled_modules.

PROXIMO: security-review
MOTIVO: migração aprovada sem bloqueio. query-performance não está habilitado. SECURITY: sim (diff em security_surface_paths), então segue para security-review e depois para qa-validation, que deve repetir a suíte com banco.
