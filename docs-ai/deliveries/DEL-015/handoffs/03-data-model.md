DELIVERY_ID: DEL-015
TASK_CODE: DEL-015
AGENT: data-model
STATUS: ok

ENTIDADES_DOMINIO:
  - `internal/domain/` NÃO muda (limite do architecture-guardrails). `Emprestimo`, `Livro`, `Usuario` seguem como estão; a regra de vencido continua em `Renovar`/`Multa` (`meiaNoiteUTC`), e o oráculo dos testes é `Multa(agora) > 0` e `Renovar(agora) == ErrEmprestimoVencido`.
  - Tipo novo é de PERSISTÊNCIA, não de domínio, em `internal/storage/repositorio.go` (como no handoff 02): `FiltroEmprestimos{UsuarioID int64; LivroID int64; Situacao Situacao; Ref time.Time}` e `Situacao` com `SituacaoTodas` (zero), `SituacaoAberto`, `SituacaoDevolvido`, `SituacaoVencido`. Nenhum campo novo em DTO nem coluna nova.

ESQUEMA_BANCO:
  Nenhuma tabela, coluna, constraint ou FK nova. Só a migração 0003, aditiva, com 3 índices btree em `emprestimo`.

  Arquivos: `migrations/0003_indices_listagem_emprestimo.up.sql` e `migrations/0003_indices_listagem_emprestimo.down.sql` (casa com `^[0-9]{4}_[a-z0-9_]+\.(up|down)\.sql$`, versão 3 sem lacuna).

  Up, texto proposto (o dev pode ajustar o comentário, nunca os comandos):
  ```
  -- 0003: índices da listagem filtrada de empréstimos (DEL-015, D-21).
  -- Keyset por id dentro de usuario_id e de livro_id, e por id entre os
  -- abertos (situacao=aberto e vencido; vencido filtra vencimento sobre eles).
  -- Aditiva: os índices simples da 0001 ficam (remoção em migração futura).
  -- Construção comum, sem a opção online (D-16 G7): lock SHARE em emprestimo,
  -- leituras seguem e escritas esperam até o fim do arquivo.
  CREATE INDEX emprestimo_usuario_id_id_idx ON emprestimo (usuario_id, id);
  CREATE INDEX emprestimo_livro_id_id_idx ON emprestimo (livro_id, id);
  CREATE INDEX emprestimo_aberto_id_idx ON emprestimo (id) WHERE devolucao IS NULL;
  ```
  Down, texto proposto (ordem inversa da criação):
  ```
  -- 0003 down: remove os índices da listagem filtrada. Não destrutivo em dado:
  -- as consultas seguem corretas, só mais lentas. DROP INDEX toma ACCESS
  -- EXCLUSIVE em emprestimo até o fim do arquivo (leituras também esperam).
  DROP INDEX emprestimo_aberto_id_idx;
  DROP INDEX emprestimo_livro_id_id_idx;
  DROP INDEX emprestimo_usuario_id_id_idx;
  ```

  Regras de forma (padrão da 0001/0002 e do executor):
  - Nomes: `<tabela>_<colunas>_idx`, como `emprestimo_livro_id_idx` da 0001; o parcial segue `emprestimo_livro_aberto_uq` ("aberto" = `devolucao IS NULL`). Nome explícito em todo índice (nada de nome gerado pelo PostgreSQL).
  - Idempotência como na 0001/0002: SEM `IF NOT EXISTS` no up e SEM `IF EXISTS`/`CASCADE` no down. A repetibilidade vem do controle do executor (`migracao_aplicada` + checksum + advisory lock, D-16); reaplicar o arquivo à mão falha alto (42P07 no up, 42704 no down), que é o desejado: `IF NOT EXISTS` aceitaria em silêncio um índice de mesmo nome com definição diferente.
  - Sem qualificação `public.` (search_path, como `TestUp0001NaoQualificaSchema`/`TestUp0002`).
  - A palavra proibida não aparece em lugar nenhum do `.up.sql` (nem comentário, nem literal): `TestArquivosEmbutidosNaoControlamTransacao` (`migrations/controle_test.go:129-157`) e o executor (G7) recusam. Evitar também comentários que mencionem a opção pelo nome; o texto acima diz "sem a opção online".
  - Comentários só com `--` no início da linha: o regex `(?im)^\s*(begin|commit|rollback|start\s+transaction|end|savepoint)\b` do mesmo teste olha o arquivo bruto, então nenhuma linha (nem continuação de comentário em bloco) pode começar com essas palavras.
  - Sem `UNIQUE`, `INCLUDE`, `USING` explícito (btree é o padrão), `DROP`, `ALTER`, `SET`, `set_config`, `BEGIN`/`COMMIT`, predicado com `now()`/`CURRENT_DATE`.
  - O predicado do parcial é exatamente `devolucao IS NULL` (imutável, sem parâmetro), o mesmo texto do `emprestimo_livro_aberto_uq`.

  Decisões de modelagem e motivo:
  - D1 `(usuario_id, id)` e `(livro_id, id)`: igualdade na coluna líder + `id > $1 ORDER BY id LIMIT $2` vira um Index Scan com `Index Cond: (fk = $n AND id > $1)` já na ordem do keyset, sem Sort, parando em limite+1. Os simples `(usuario_id)`/`(livro_id)` exigiriam Sort de todas as linhas do usuário/livro.
  - D2 parcial `(id) WHERE devolucao IS NULL`: abertos ≤ nº de livros (`emprestimo_livro_aberto_uq`), então o índice é minúsculo e cobre `situacao=aberto` e `situacao=vencido` sem filtro de FK na ordem do keyset. `vencimento < $N` fica como Filter sobre as linhas desse índice (vencido não é indexável: depende do relógio, R1 do triage).
  - D3 sem índice para `devolvido`: é a maioria das linhas; o PK com `Filter: devolucao IS NOT NULL` completa a página quase sem descarte.
  - D4 sem `INCLUDE (vencimento)` ou `(id, vencimento)`: o SELECT lê todas as colunas (`colunasEmprestimo`), então há heap fetch de qualquer jeito; incluir não evita nada e aumenta o índice.
  - D5 sem parciais compostos (`(usuario_id, id) WHERE devolucao IS NULL` etc.): por usuário/livro o volume é pequeno (dezenas a centenas de linhas), o Filter sobre o composto basta; mais índices = mais custo de escrita sem ganho mensurável no porte.
  - D6 os simples da 0001 ficam (decisão do 02): redundantes com D1 (mesma coluna líder; servem à FK e à igualdade), removidos em delivery futura com `pg_stat_user_indexes.idx_scan`.
  - D7 os três índices num só arquivo (pedido da sessão e do 02). Tensão com a regra "um DDL pesado por arquivo" (D-16, `docs-ai/DECISOES.md:521-523`; runbook §7): a regra existe porque `statement_timeout` é por comando e N comandos lentos seguram ACCESS EXCLUSIVE por N vezes o prazo. Aqui o lock é SHARE (leituras seguem), o mesmo nos três comandos (o `lock_timeout` só pesa na primeira aquisição; os seguintes já têm o lock) e o custo é a soma de três construções numa tabela de 10^4–10^5 linhas (milissegundos a poucos segundos). Pior caso teórico: 3 × 60s com escritas bloqueadas. Alternativa (0003/0004/0005, um índice por arquivo) só encurta cada janela, não o total, e cria estado intermediário; não adotada. Registrar na D-21 como interpretação explícita da regra.

  Tamanho estimado: cada composto ~ (2 × bigint + overhead) ≈ 1,2–1,5× o `emprestimo_pkey`; o parcial ≈ abertos/total do PK.

PADROES_DE_ACESSO:
  Texto SQL (por `sqlListarEmprestimos(f)`, ordem fixa usuario → livro → situação, placeholders a partir de $3):
  `SELECT <colunasEmprestimo> FROM emprestimo WHERE id > $1 [AND usuario_id = $3] [AND livro_id = $k] [AND devolucao IS NULL | AND devolucao IS NOT NULL | AND devolucao IS NULL AND vencimento < $m] ORDER BY id LIMIT $2`
  Requisito para o parcial ser usado, inclusive no plano genérico do prepared statement: o fragmento de aberto/vencido tem de ser o texto literal `devolucao IS NULL` (sem parâmetro, sem `NOT (... IS NOT NULL)`, sem `COALESCE`/`$n::bool`). Vencido = DOIS fragmentos: `devolucao IS NULL` AND `vencimento < $m` (com `$m` = `vencimentoParaDate(Ref)`, tipo `date`).

  Plano esperado por combinação (U = usuario_id, L = livro_id; "Index Cond" contém `id >` em todos menos onde dito):
   1. sem filtro → `emprestimo_pkey`, Index Cond `id > $1` (inalterado, SQL byte a byte o atual).
   2. aberto → `emprestimo_aberto_id_idx`, Index Cond `id > $1`.
   3. vencido → `emprestimo_aberto_id_idx`, Index Cond `id > $1`, Filter `vencimento < $3`.
   4. devolvido → `emprestimo_pkey`, Filter `devolucao IS NOT NULL`.
   5. U → `emprestimo_usuario_id_id_idx`, Index Cond `usuario_id = $3 AND id > $1`.
   6. U + aberto → `emprestimo_usuario_id_id_idx`, Filter `devolucao IS NULL`.
   7. U + vencido → `emprestimo_usuario_id_id_idx`, Filter `devolucao IS NULL AND vencimento < $4`. (O parcial perde: teria de varrer todos os abertos filtrando usuário.)
   8. U + devolvido → `emprestimo_usuario_id_id_idx`, Filter `devolucao IS NOT NULL`.
   9. L → `emprestimo_livro_id_id_idx`, Index Cond `livro_id = $3 AND id > $1`.
  10. L + aberto → `emprestimo_livro_aberto_uq` (Index Cond `livro_id = $3`, Filter `id > $1`, ≤ 1 linha, Sort/top-N de 1 linha aceito) OU `emprestimo_livro_id_id_idx` com Filter `devolucao IS NULL`. Os dois são ótimos; o planner escolhe pelo custo.
  11. L + vencido → mesmo conjunto do 10, com Filter `vencimento < $4` adicional. Com o uq, no máximo 1 linha é lida.
  12. L + devolvido → `emprestimo_livro_id_id_idx`, Filter `devolucao IS NOT NULL`.
  13. U + L → `emprestimo_livro_id_id_idx` (mais seletivo: livro tem menos empréstimos que usuário) ou `emprestimo_usuario_id_id_idx`, com a outra FK em Filter.
  14. U + L + aberto → {`emprestimo_livro_aberto_uq`, `emprestimo_livro_id_id_idx`, `emprestimo_usuario_id_id_idx`}.
  15. U + L + vencido → mesmo conjunto do 14, com `vencimento < $5` no Filter.
  16. U + L + devolvido → {`emprestimo_livro_id_id_idx`, `emprestimo_usuario_id_id_idx`}.
  Em nenhum: Seq Scan, Bitmap (Heap/Index/And) Scan, Sort (exceto sobre o uq), nem os simples `emprestimo_usuario_id_idx`/`emprestimo_livro_id_idx`.

  Outras leituras desta entrega (sem índice novo): `BuscarUsuario` por PK em toda página da rota aninhada (`usuario_pkey`); `BuscarEmprestimoAberto` por livro continua no uq (`repositorio.go:366`). Escrita: INSERT/UPDATE de empréstimo passam a manter 3 índices a mais (custo desprezível no porte; UPDATE de devolução tira a linha do parcial).

TESTES_EXIGIDOS (para o test-writer):
  A. EXPLAIN com banco (internal/storage, novo, ex.: `TestPlanoListarEmprestimos`, obrigatório com `BIBLIOTECA_TEST_DB_OBRIGATORIO=1`):
    - Fixture por SQL direto em schema descartável (`novoBancoMigrado`), para o planner ter estatística estável:
      `INSERT INTO usuario (nome) SELECT 'u' || g FROM generate_series(1, 200) g;`
      `INSERT INTO livro (titulo) SELECT 'l' || g FROM generate_series(1, 2000) g;`
      `INSERT INTO emprestimo (livro_id, usuario_id, inicio, vencimento, devolucao) SELECT 1 + g % 2000, 1 + g % 199, timestamptz '2026-01-01 12:00Z' + g * interval '1 minute', date '2026-02-01' + g % 60, CASE WHEN g > 19000 THEN NULL ELSE timestamptz '2026-03-01 12:00Z' END FROM generate_series(1, 20000) g;`
      (2000 e 199 coprimos: livro e usuário não correlacionados; os 1000 abertos têm g consecutivos, logo livros distintos, e respeitam o uq; ~metade vencida com Ref em 2026-03-01.) Depois `ANALYZE usuario, livro, emprestimo;`. Sem `SET enable_*`.
    - Para cada uma das 16 combinações, gerar o texto com `sqlListarEmprestimos(f)` (a função real, não SQL copiado) e rodar:
      (a) plano GENÉRICO: `EXPLAIN (GENERIC_PLAN, FORMAT JSON) ` + sql, sem args (PostgreSQL 16+; o CI usa postgres:16-alpine). É o que importa para o cache de prepared statement do pgx.
      (b) plano CUSTOM: `EXPLAIN (FORMAT JSON) ` + sql com os args reais (apos=0 e apos=15000, limite+1=101, ids existentes, Ref date). Se o protocolo recusar parâmetros no EXPLAIN, usar `PREPARE`/`EXPLAIN EXECUTE` com valores constantes do teste.
    - Asserções no JSON (percorrer `Plan`/`Plans`): exatamente um nó de scan sobre `emprestimo`; `Node Type` = `Index Scan`; `Index Name` no conjunto permitido acima; `Index Cond` contém `id >` (exceto quando o índice é `emprestimo_livro_aberto_uq`); nenhum `Seq Scan`, `Bitmap*`, nenhum `Sort` (exceto acima do uq); nenhum uso de `emprestimo_usuario_id_idx`/`emprestimo_livro_id_idx`. Para 2, 3, 5, 9 o índice é ÚNICO (sem conjunto).
    - Combinação 1 em teste próprio de regressão: SQL igual byte a byte a `SELECT ... FROM emprestimo WHERE id > $1 ORDER BY id LIMIT $2` e plano em `emprestimo_pkey`.
  B. Texto da migração (pacote `migrations`, novo arquivo ex. `migrations/indices_listagem_test.go`, no estilo de `renovacao_test.go`):
    - `TestUp0003`: SQL sem comentários e normalizado (`espacos(semComentarios(...))`) tem exatamente 3 comandos, iguais a `create index emprestimo_usuario_id_id_idx on emprestimo (usuario_id, id)`, `create index emprestimo_livro_id_id_idx on emprestimo (livro_id, id)`, `create index emprestimo_aberto_id_idx on emprestimo (id) where devolucao is null`; nenhum `unique`, `if not exists`, `drop `, `alter `, ` set `, `set_config`, `include`, `now()`, `current_date`, `public.`.
    - `TestDown0003`: só `drop index` dos três nomes (regex `drop index ([a-z_]+)`, conjunto exato), sem `IF EXISTS`/`CASCADE`, sem `drop table`/`drop column`, e sem citar `emprestimo_usuario_id_idx`, `emprestimo_livro_id_idx`, `emprestimo_livro_aberto_uq`, `emprestimo_pkey`.
    - A checagem da palavra proibida e de controle de transação já cobre a 0003 por varrer o FS inteiro (`controle_test.go:129`); não duplicar.

MUDANCAS_EM_esquema_test.go (`internal/storage/esquema_test.go`):
  - `indicesEsperados` (linhas 26-33) passa a ter 9 nomes, em `sort.Strings`:
    `emprestimo_aberto_id_idx`, `emprestimo_livro_aberto_uq`, `emprestimo_livro_id_id_idx`, `emprestimo_livro_id_idx`, `emprestimo_pkey`, `emprestimo_usuario_id_id_idx`, `emprestimo_usuario_id_idx`, `livro_pkey`, `usuario_pkey`. (`_id_id_idx` vem antes de `_id_idx` porque `_` < `x`.) Sem essa mudança `checarCatalogo` reprova (linha 163) assim que a 0003 existir.
  - Acrescentar em `checarCatalogo`, junto do bloco das linhas 166-175, conferência exata da definição por sufixo do `indexdef` do `pg_indexes` (o prefixo traz o schema descartável):
      `emprestimo_usuario_id_id_idx` termina em `USING btree (usuario_id, id)`;
      `emprestimo_livro_id_id_idx` termina em `USING btree (livro_id, id)`;
      `emprestimo_aberto_id_idx` termina em `USING btree (id) WHERE (devolucao IS NULL)`;
      os três começam com `CREATE INDEX` (não UNIQUE).
  - O laço dos simples (linhas 170-175) continua válido: ele busca `(livro_id)`/`(usuario_id)` só nos nomes simples, e a definição dos compostos (`(livro_id, id)`) não casa com `(livro_id)`; não mudar.
  - `constraintsEsperadas`, `colunasEsperadas`, o default de `renovacoes` e `TestEsquemaRestricoes`: sem mudança (a 0003 não cria constraint nem coluna; índice não-UNIQUE não entra em `pg_constraint`).
  - `TestMigracaoUpDownUp` continua passando sem mudança de estrutura: `aplicar(".down.sql")` ordena reverso, roda 0003 down antes da 0002/0001, e `contarObjetos` volta a 0. Recomendado acrescentar um subteste que aplica todas as ups, roda SÓ o down da 0003 e confere que o conjunto de índices volta aos 6 da 0002 (prova que o down remove exatamente os três e não toca os da 0001); exige um helper de teste que aplique um arquivo por nome.
  - Nenhuma outra suíte depende do número de migrações: `cmd/biblioteca-bairro/migrate_*_test.go`, `internal/migrate/*` e `internal/http/banco_test.go` usam `ValidarFS(migrations.FS)`/glob dinâmicos (conferido por grep).

CONFIRMADO:
  - Esquema atual: `migrations/0001_esquema_emprestimo.up.sql:15-30` (colunas, FKs RESTRICT, uq parcial e os dois simples); 0002 só `renovacoes` + CHECK (`migrations/0002_renovacao_emprestimo.up.sql`).
  - Padrão de idempotência sem `IF [NOT] EXISTS`: `migrations/0001_*.up.sql`, `0002_*.up.sql`; `TestDown0001` (`migrations/migrations_test.go:61-81`) e `TestDown0002` (`migrations/renovacao_test.go:59-88`) proíbem `IF EXISTS`/`CASCADE`; motivo registrado em `docs-ai/deliveries/DEL-011/handoffs/08-migration-reviewer.md:20`.
  - Barreira da palavra proibida e de controle de transação em todo o FS, comentário incluído: `migrations/controle_test.go:129-157`; regra G7 em `docs-ai/DECISOES.md:435-438` e runbook §6 (`docs-ai/RUNBOOK-MIGRACAO.md:85-87`).
  - Regra "um DDL pesado por arquivo" e o motivo (ACCESS EXCLUSIVE × N comandos): `docs-ai/RUNBOOK-MIGRACAO.md:96-100`, `docs-ai/DECISOES.md:521-523`.
  - Catálogo testado por nome e por lista exata: `internal/storage/esquema_test.go:26-33, 158-175`; down aplicado em ordem reversa: `internal/storage/banco_test.go:111-133`.
  - Consultas atuais: `internal/storage/repositorio.go:161-166` (listagem keyset por PK), `:366` (aberto por livro, usa o uq), `listarPorID` em `:173-201`.
  - CI com `postgres:16-alpine` (`.github/workflows/delivery.yml:24`): `EXPLAIN (GENERIC_PLAN)` disponível (16+).
  - Nenhum EXPLAIN nos testes hoje (grep `EXPLAIN|enable_seqscan|ANALYZE` em `internal/`: nenhum resultado).

ASSUNCOES:
  - Os planos das combinações 10, 11, 13-16 dependem do custo; por isso o teste aceita um conjunto. Os das 1-9 e 12 são esperados como índice único com a fixture proposta; se o planner do PG 16 escolher diferente com ela, ajustar a FIXTURE (proporções), não afrouxar a asserção, e registrar.
  - Parâmetros em `EXPLAIN` pelo protocolo estendido do pgx funcionam (não testado aqui); fallback `PREPARE`/`EXPLAIN EXECUTE` indicado.
  - Premissa de tamanho da tabela em produção (10^4–10^5) herdada do 02, não verificada.
  - Nome do arquivo `0003_indices_listagem_emprestimo` é proposta; qualquer nome no padrão serve, desde que os testes citem o mesmo.

ARQUIVOS:
  - nenhum editado (subagente sem escrita). Previstos: migrations/0003_indices_listagem_emprestimo.up.sql e .down.sql (novos); migrations/indices_listagem_test.go (novo); internal/storage/esquema_test.go (lista e definições dos índices; subteste do down da 0003); internal/storage/<novo>_test.go (EXPLAIN das 16 combinações).

RISCOS:
  - Conflito de documentação: o runbook §6 diz "Índice em tabela com dados é procedimento manual" e a D-16 G7 repete. A 0003 cria índice em tabela com dados pelo executor. O 02 aceitou com limite (~1 milhão de linhas → manual); a D-21 precisa registrar a exceção/interpretação, e o docs-update precisa ajustar o runbook §6 e acrescentar a nota da 0003 (conferir `count(*)`/`pg_total_relation_size`, janela de baixo uso). O migration-reviewer deve conferir isso.
  - D7 (três CREATE INDEX num arquivo): escritas em `emprestimo` ficam bloqueadas pela SOMA das três construções; com timeout de escrita da API de 5s, construção total acima disso faz abrir/devolver/renovar responderem erro durante o deploy.
  - Down toma ACCESS EXCLUSIVE (bloqueia leituras também) até o COMMIT; rápido, mas rodar com o binário antigo ou em janela.
  - Estabilidade do teste de EXPLAIN: estatística e versão do planner podem mudar a escolha nas combinações com conjunto; mitigado por fixture fixa, `ANALYZE` explícito e conjuntos permitidos.
  - Índices redundantes (simples + compostos) até a delivery de remoção; pendência a registrar na D-21.
  - `vencido` sem FK percorre o parcial inteiro no pior caso (abertos não vencidos com id menor), limitado pelo nº de livros.

SINAIS:
  SECURITY: sim — migração em `migrations/` e SQL novo em `internal/storage/`, ambos em `security_surface_paths` (herdado do 02).
  SUPPLY_CHAIN: nao — sem mudança em `go.mod`/`go.sum`.
  MIGRATION: sim — `0003_indices_listagem_emprestimo`, três CREATE INDEX com lock SHARE em `emprestimo`, down com três DROP INDEX; conflito com runbook §6 a resolver na D-21.
  PERFORMANCE: sim — keyset filtrado depende dos compostos e do parcial; plano conferido por EXPLAIN genérico e custom em teste com banco (query-performance não habilitado).

PROXIMO: test-writer
MOTIVO: esquema e índices definidos; faltam os testes (EXPLAIN das 16 combinações, texto da 0003, mudanças no esquema_test.go, além dos testes de filtro/rota do 02) antes do dev-implementation.
