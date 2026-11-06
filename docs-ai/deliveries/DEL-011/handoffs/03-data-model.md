DELIVERY_ID: DEL-011
TASK_CODE: DEL-011
AGENT: data-model
STATUS: ok

ENTIDADES_DOMINIO:
  `internal/domain/emprestimo.go`, struct `Emprestimo` (campos não exportados, como hoje):
    - id int64, livroID int64, usuarioID int64, inicio time.Time, vencimento time.Time (12:00 SP), devolucao time.Time (zero = aberto): inalterados.
    - renovacoes int (NOVO): número de renovações bem-sucedidas, 0..LimiteRenovacoes. Getter `Renovacoes() int`. Zero em `NovoEmprestimo`. Struct continua comparável com `==` (só tipos comparáveis).
  Constantes novas: `LimiteRenovacoes = 2`, `PrazoRenovacaoDias = 14` (separada de `PrazoEmprestimoDias`).
  Sentinelas novas: `ErrEmprestimoVencido`, `ErrLimiteRenovacoes`, `ErrRenovacoesInvalidas` (esta só na reidratação; dado gravado inválido).
  Construtores/operações:
    - `ReidratarEmprestimo(id, livroID, usuarioID int64, inicio, vencimento, devolucao time.Time, renovacoes int)`: validação nova por último: `renovacoes < 0 || renovacoes > LimiteRenovacoes` → `ErrRenovacoesInvalidas`. Sem coerência exigida entre vencimento, início e contador.
    - `(*Emprestimo).Renovar(ref time.Time) error`: ordem e regras como no handoff 02 (início zero → ErrEmprestimoInvalido; devolvido → ErrEmprestimoJaDevolvido; ref zero → ErrReferenciaZero; `meiaNoiteUTC(ref) > meiaNoiteUTC(e.vencimento)` → ErrEmprestimoVencido; `renovacoes >= LimiteRenovacoes` → ErrLimiteRenovacoes). Sucesso: `a,m,d := e.vencimento.In(locSaoPaulo).Date()`; `vencimento = time.Date(a, m, d+PrazoRenovacaoDias, 12, 0, 0, 0, locSaoPaulo)`; `renovacoes++`. Erro não muta.
  Modelo de persistência separado: o storage continua lendo colunas para variáveis locais em `lerEmprestimo` e passa a `ReidratarEmprestimo`; não há struct de persistência espelhando o domínio (padrão atual, mantido).

ESQUEMA_BANCO:
  Tabela `emprestimo` (migrations/0001) ganha, pela `migrations/0002_renovacao_emprestimo.up.sql`:
    - coluna `renovacoes integer NOT NULL DEFAULT 0`
    - constraint nomeada `emprestimo_renovacoes_check CHECK (renovacoes >= 0 AND renovacoes <= 2)`
  Forma do up (um comando, um DDL pesado por arquivo, RUNBOOK §6-7):
    ALTER TABLE emprestimo
        ADD COLUMN renovacoes integer NOT NULL DEFAULT 0,
        ADD CONSTRAINT emprestimo_renovacoes_check CHECK (renovacoes >= 0 AND renovacoes <= 2);
  Down (`0002_renovacao_emprestimo.down.sql`), sem IF EXISTS nem CASCADE (falha alto, como TestDown0001):
    ALTER TABLE emprestimo
        DROP CONSTRAINT emprestimo_renovacoes_check,
        DROP COLUMN renovacoes;
  Regras para o arquivo (executor `internal/migrate`):
    - Sem `public.`, sem BEGIN/COMMIT/SAVEPOINT, sem SET/set_config, sem a palavra "concurrently" em lugar nenhum (inclusive comentário: `TemConcurrently`, internal/migrate/fs.go:142).
    - Nenhuma linha pode começar com `end`/`begin`/`commit` etc. (regex `(?im)^\s*(begin|commit|rollback|start\s+transaction|end|savepoint)\b` em migrations/controle_test.go:130). Não usar CASE...END quebrado em linha; o CHECK acima não precisa.
    - COMMENT ON COLUMN é opcional; se usado, em português sem as palavras acima. Qualquer byte alterado depois de aplicado quebra checksum.
  Chaves e ações referenciais: nenhuma FK nova; FKs existentes (`ON DELETE RESTRICT`) intocadas. Nenhum índice novo.
  Tipo: `integer` (não smallint/bigint: decisão do 02; Go `int` cabe sem perda; pgx faz Scan de int4 em *int e codifica int como int4 com checagem de faixa).
  Backfill: DEFAULT constante → linhas existentes leem 0 sem rewrite (PG 11+). O ADD CONSTRAINT CHECK varre a tabela sob ACCESS EXCLUSIVE (lock_timeout 5s/statement_timeout 60s do executor).

PADROES_DE_ACESSO:
  1. `BuscarEmprestimoAbertoPorLivro`: `SELECT colunasEmprestimo FROM emprestimo WHERE livro_id = $1 AND devolucao IS NULL` → índice parcial único `emprestimo_livro_aberto_uq`. Só ganha a coluna `renovacoes` no SELECT.
  2. `RegistrarRenovacao` (novo), UPDATE por PK com CAS. Refinamento do data-model sobre o SQL do 02: o valor anterior esperado do contador vai como parâmetro próprio calculado em Go, em vez de `$3 - 1` no SQL (evita inferência de tipo do parâmetro em expressão aritmética e deixa explícito que o banco só confere):
       UPDATE emprestimo SET vencimento = $2, renovacoes = $3
        WHERE id = $1 AND devolucao IS NULL AND inicio = $4 AND renovacoes = $5
       RETURNING colunasEmprestimo
     com $2 = vencimentoParaDate(e.Vencimento()), $3 = e.Renovacoes(), $4 = e.Inicio(), $5 = e.Renovacoes()-1.
     Recusa pré-SQL com domain.ErrEmprestimoInvalido: `e.ID() <= 0 || !e.Aberto() || !valido(e) || e.Renovacoes() < 1 || e.Renovacoes() > domain.LimiteRenovacoes`.
     As colunas atualizadas (vencimento, renovacoes) não estão em índice → UPDATE elegível a HOT.
  3. Diagnóstico sem linha atualizada (PK): `SELECT devolucao IS NOT NULL FROM emprestimo WHERE id = $1` → ErrNoRows ⇒ ErrEmprestimoNaoEncontrado; devolvido ⇒ domain.ErrEmprestimoJaDevolvido; senão ⇒ ErrEmprestimoDivergente (início divergente, contador divergente ou corrida perdida).
  4. `AbrirEmprestimo` (INSERT) e `RegistrarDevolucao` (UPDATE): SQL de escrita inalterado; só o RETURNING passa a trazer `renovacoes` via `colunasEmprestimo`. INSERT usa o DEFAULT 0.
  5. `lerEmprestimo`: Scan de 7 colunas (`renovacoes` em `var renovacoes int`) e `ReidratarEmprestimo(..., dev, renovacoes)`. Erro de reidratação continua embrulhado "storage: empréstimo gravado inválido: %w" (HTTP: ErrRenovacoesInvalidas em errosDeBug → 500).
  Nenhuma consulta nova varre a tabela: todas por PK ou pelo índice parcial.

CONFIRMADO:
  - `migrations/0001_esquema_emprestimo.up.sql`: `emprestimo` sem contador; constraints nomeadas `emprestimo_*_check`; índices `emprestimo_livro_aberto_uq` (parcial), `emprestimo_livro_id_idx`, `emprestimo_usuario_id_idx`.
  - `internal/storage/repositorio.go:150` `colunasEmprestimo` e `:242-264` `lerEmprestimo` são o único ponto de Scan; `:185-218` molde do CAS + diagnóstico.
  - `internal/domain/emprestimo.go:44-51` struct sem contador; `:92` assinatura atual de `ReidratarEmprestimo` (6 parâmetros); `:109-110` normalização do vencimento.
  - `internal/storage/esquema_test.go:14-52` (`constraintsEsperadas`, `colunasEsperadas`) compara o catálogo de forma EXATA ("constraint inesperada", "coluna inesperada"): `TestMigracaoUpDownUp` quebra com a 0002 até o test-writer acrescentar `"emprestimo_renovacoes_check": "c"`, `"emprestimo.renovacoes": {tipo: "integer", nula: "NO", identity: "NO"}` e os trechos `renovacoes >= 0`, `renovacoes <= 2` em `pg_get_constraintdef`.
  - `internal/storage/banco_test.go:111-133` `aplicar` roda todos os `*.up.sql` em ordem e `*.down.sql` em ordem reversa, então a 0002.down roda antes da 0001.down.
  - `migrations/migrations_test.go`: pareamento up/down e `TestDown0001` (sem IF EXISTS/CASCADE); `migrations/controle_test.go:129-157` proíbe controle de transação e "concurrently" em todos os arquivos do FS.
  - Testes do executor e do cmd que usam o FS real (`internal/migrate/executor_test.go:109-142`, `cmd/biblioteca-bairro/migrate_test.go:716-791`, `migrate_correcao_test.go:298-410`) usam `len(esperadas)` e só fixam a linha da 0001: não quebram com a 0002, e `internal/migrate/` e `cmd/` ficam intocados.
  - `internal/http/banco_test.go:93-99` aplica todos os `*.up.sql`: o banco dos testes HTTP recebe a coluna automaticamente.

ASSUNCOES:
  - Mantida a escolha do 02 de fixar o limite 2 no CHECK do banco (segunda barreira); mudar o limite exige migração nova.
  - O CAS confere início e contador, não o vencimento anterior: um `Emprestimo` reidratado à mão com contador coerente e vencimento arbitrário seria gravado. Aceito como em `RegistrarDevolucao` (o repositório confia no valor vindo de `Renovar` sobre leitura do próprio repositório). Endurecimento possível, não exigido: `AND vencimento = $6` com o vencimento anterior.
  - Troca de `renovacoes = $3 - 1` por `$5` separado: refinamento de forma, mesma semântica; não exige decisão humana.
  - Volume de `emprestimo` pequeno (biblioteca de bairro); não medido em produção.

ARQUIVOS:
  - nenhum (data-model sem escrita). Previstos: migrations/0002_renovacao_emprestimo.up.sql, migrations/0002_renovacao_emprestimo.down.sql, internal/domain/emprestimo.go, internal/storage/repositorio.go; testes: internal/storage/esquema_test.go (catálogo + casos novos em TestEsquemaRestricoes: renovacoes -1 e 3 → 23514/emprestimo_renovacoes_check, NULL → 23502, INSERT sem a coluna lê 0), migrations/migrations_test.go (TestDown0002: sem IF EXISTS/CASCADE, um DROP COLUMN renovacoes; TestUp0002 sem `public.`).

RISCOS:
  - `TestMigracaoUpDownUp`/`checarCatalogo` falha enquanto o catálogo esperado não for atualizado: atualização obrigatória no test-writer.
  - Deploy fora de ordem: binário novo antes do `migrate up` 0002 dá 42703 em toda leitura de empréstimo (500 causa=banco); down só depois de reverter o binário. O down perde o contador; os vencimentos estendidos ficam, e uma nova subida da 0002 zera o contador desses empréstimos (eles poderiam renovar de novo). Documentar no report/runbook.
  - Esquecer `renovacoes` no Scan ou na lista de colunas desalinha as 7 colunas: o `Scan` falha em toda leitura. Teste de storage com contador != 0 lido de volta cobre isso.
  - Renovação concorrente com devolução: coberta pelo CAS (`devolucao IS NULL`) e pelo diagnóstico; exige teste de storage (devolução grava antes → `ErrEmprestimoJaDevolvido`).

SINAIS:
  SECURITY: sim — toca migrations/, internal/storage/ e internal/http/ (security_surface_paths); rota autenticada nova.
  SUPPLY_CHAIN: nao — sem dependência nova; go.mod/go.sum intocados.
  MIGRATION: sim — 0002 adiciona coluna NOT NULL DEFAULT 0 e CHECK em `emprestimo`; down destrutivo do contador; migration-reviewer obrigatório.
  PERFORMANCE: nao — UPDATE e diagnóstico por PK, busca pelo índice parcial existente; nenhuma consulta nova sobre tabela grande; UPDATE sem coluna indexada (HOT).

PROXIMO: test-writer
MOTIVO: esquema, entidades e padrões de acesso definidos; o test-writer escreve os testes de domínio, storage (incluindo o catálogo exato de esquema_test.go), migrations e HTTP antes do dev-implementation.
