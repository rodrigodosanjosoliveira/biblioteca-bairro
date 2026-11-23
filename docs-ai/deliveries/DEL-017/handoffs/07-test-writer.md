DELIVERY_ID: DEL-017
TASK_CODE: PROJ-7
AGENT: test-writer
STATUS: ok

ESCOPO: medição M1 pedida no handoff 06 (architecture-guardrails) e ajuste dos testes para a forma F1. F1 passou em M1, então F2 não foi medida (NAO_FAZER do 06: não usar F2 se F1 passar). O resto do handoff 05 (testes de CA1-CA4, HTTP, uma consulta por requisição) continua valendo.

FORMA_APROVADA_PELA_MEDICAO (F1), para o dev usar nas consts do pacote storage:
  sqlListarLivros:
    SELECT l.id, l.titulo,
           (SELECT 1 FROM emprestimo e
             WHERE e.livro_id = l.id AND e.devolucao IS NULL
             LIMIT 1) IS NULL AS disponivel
      FROM livro l
     WHERE l.id > $1
     ORDER BY l.id
     LIMIT $2
  sqlBuscarLivro: a mesma projeção, com `WHERE l.id = $1` e sem ORDER/LIMIT externos.

PLANOS_MEDIDOS (M1, Postgres 16-alpine com o digest do CI e trust, massa de fixturePlano com 500 livros, 200000 empréstimos e 499 abertos, EXPLAIN (VERBOSE, COSTS OFF)):
  Listagem, genérico (GENERIC_PLAN):
    Limit
      Output: l.id, l.titulo, (((SubPlan 1) IS NULL))
      -> Index Scan using livro_pkey on public.livro l
           Index Cond: (l.id > $1)
           SubPlan 1
             -> Limit
                  -> Index Only Scan using emprestimo_livro_aberto_uq on public.emprestimo e
                       Index Cond: (e.livro_id = l.id)
  Listagem, custom com apos=0 e limite+1=2: o mesmo plano, com Index Cond (l.id > 0) e a mesma sonda (Index Only Scan em emprestimo_livro_aberto_uq, Index Cond (e.livro_id = l.id)).
  Listagem, custom com apos 0/250/450 e limite+1 101/21: o mesmo plano em todos.
  Detalhe, genérico e custom (livro 151): Index Scan livro_pkey (l.id = $1 / 151), SubPlan 1, Limit e Index Only Scan em emprestimo_livro_aberto_uq com Index Cond (e.livro_id = l.id).
  Em nenhum caso há "hashed", Seq Scan, Bitmap nem Filter no scan de emprestimo. O mesmo resultado sai do TestPlanoLeituraLivro (10 casos, genérico e custom) rodado com a implementação de referência, no subteste "valores na massa do plano".

AJUSTES_DE_TESTE:
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/disponibilidade_sem_banco_test.go, TestSQLLeituraLivro, reescrito para F1:
    - Exige `(select 1 from emprestimo e`, `e.livro_id = l.id`, `e.devolucao is null`, `limit 1) is null` e `as disponivel`. A normalização tira espaços e quebras de linha junto aos parênteses.
    - Proíbe `exists`, ` in (`, `any(` e `is not null`, além das proibições anteriores (join, lateral, coalesce, now(), vencimento etc.).
    - Na listagem, exige exatamente 2 LIMIT (`limit $2` e um `limit 1)`). No detalhe, exige exatamente 1 LIMIT, o interno, sem `limit $` nem ORDER.
    - Exige exatamente dois SELECT e dois FROM.
    - O comentário explica que o EXISTS é proibido porque vira hashed SubPlan (convert_EXISTS_to_ANY), com a medição do handoff 05.
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/plano_livros_test.go, checarPlanoLivro: o handoff 06 previa "sem mudança com F1", mas uma mudança foi necessária. Com F1, o nó com Parent Relationship = SubPlan é o Limit do `LIMIT 1`, e o scan de emprestimo fica abaixo dele, com Parent Relationship "Outer". A asserção agora usa caminhoSubPlan: o scan de emprestimo precisa estar dentro de um SubPlan, e entre a raiz do SubPlan e o scan só pode haver nós Limit. Continuam exigidos: um só scan em emprestimo, Index Scan ou Index Only Scan em emprestimo_livro_aberto_uq, Index Cond `livro_id = l.id`, sem Filter, sem hashed (Subplan Name e Output), sem Seq Scan e sem Bitmap, e livro em livro_pkey. O comentário de cabeçalho registra a medição.
  - Nenhum outro arquivo de teste mudou em relação ao handoff 05.

CONFIRMADO:
  - `go test -count=1 ./...` no projeto, sem implementação: falha esperada por build, como no handoff 05:
      internal/storage [build failed]: listagem_test.go:48:20: undefined: LivroComDisponibilidade; disponibilidade_sem_banco_test.go:31:27: undefined: sqlListarLivros; :33:26: undefined: sqlBuscarLivro; disponibilidade_test.go:143:11: got.Livro undefined (type domain.Livro has no field or method Livro)
      internal/http [build failed]: apoio_test.go:86:27: undefined: storage.LivroComDisponibilidade (e as demais referências ao tipo)
    Os outros pacotes passam.
  - A implementação de referência F1 foi feita numa cópia em mktemp, com o tipo de leitura, as consts F1, saidaLivroLido e saidaLivro intocado. Com BIBLIOTECA_TEST_DATABASE_URL e BIBLIOTECA_TEST_DB_OBRIGATORIO=1, `go test -count=1 -p 1 ./...` deu ok em todos os pacotes, inclusive internal/storage (29.5s) com TestPlanoLeituraLivro (10 casos, mais "valores na massa do plano" e "noPlano lê SubPlan") e TestSQLLeituraLivro: 16 PASS, nenhum SKIP. `go vet ./...` limpo na cópia.
  - Mutantes: 31 mortos, 1 sobrevivente. Rodados na cópia, agora sem -skip, porque a referência passa no teste de plano.
      Mutantes de forma:
        L1 listagem reescrita como NOT EXISTS: mortos por TestPlanoLeituraLivro e TestSQLLeituraLivro.
        B1 detalhe reescrito como NOT EXISTS: morto por TestSQLLeituraLivro. No detalhe, o plano com NOT EXISTS também usa a sonda, então só o teste de texto mata; isso foi previsto pelo guardrails.
        L2/B2 `IS NULL` → `IS NOT NULL`: mortos pelos testes de CA3/CA4, HTTP com banco, plano e texto.
        L3/B3 sem o LIMIT 1 interno: mortos só por TestSQLLeituraLivro, pela contagem de LIMIT. O handoff 06 previa sobrevivente equivalente sob a R4; o teste de texto o mata de propósito.
        L9 COALESCE no lugar de IS NULL: morto só por TestSQLLeituraLivro.
        L11 LEFT JOIN correto: mortos por TestPlanoLeituraLivro e TestSQLLeituraLivro.
      Demais SQL: sem `devolucao IS NULL` (L4/B4), `devolucao IS NOT NULL` (L5), `AND vencimento >= CURRENT_DATE` (L6/B6, também mortos pelo plano por causa do Filter), sem correlação (L7/B7), correlação deslocada (L8), LEFT JOIN que duplica (L10) e ORDER BY DESC (L12). Todos mortos.
      storage: Scan descartando o bool (L13), detalhe sempre true (B14), duas consultas (B15, só o TestLeituraLivroUmaConsulta mata) e ErrNoRows sem virar 404 (B16). Todos mortos.
      handler: H1 a H9, os mesmos do handoff 05. Todos mortos.
      Sobrevivente: B17, que devolve `got` no ramo ErrNoRows de BuscarLivro. É equivalente: o Scan não preenche nada com ErrNoRows.
  - F2 não foi medida (NAO_FAZER do 06).
  - Cópia temporária apagada e container Postgres parado (--rm). `gofmt -l internal/` limpo.

ASSUNCOES:
  - O dev usa exatamente os aliases `l` e `e` e o texto `(SELECT 1 FROM emprestimo e ... LIMIT 1) IS NULL AS disponivel`, como em A1' do handoff 06. TestSQLLeituraLivro tolera quebras de linha e espaços, mas não outro alias nem outra ordem dos trechos exigidos.
  - O plano medido vale para PostgreSQL 16, a versão do CI. Em versão futura, o próprio TestPlanoLeituraLivro é a barreira.

ARQUIVOS:
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/disponibilidade_sem_banco_test.go — TestSQLLeituraLivro para F1 (proíbe EXISTS, aceita e exige o LIMIT 1 interno)
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/plano_livros_test.go — checarPlanoLivro aceita Limit entre o SubPlan e o scan (caminhoSubPlan); comentário com a medição
  - Os demais arquivos de teste continuam como no handoff 05 (/home/rodrigo/workspace/biblioteca-bairro/docs-ai/deliveries/DEL-017/handoffs/05-test-writer.md, seção ARQUIVOS).
  Não tocados: código de produção, internal/domain/, migrations/, go.mod, go.sum.

RISCOS:
  - TestSQLLeituraLivro proíbe a palavra `exists` em qualquer lugar do texto, inclusive num comentário SQL dentro da const. O dev não deve comentar dentro do SQL.
  - O plano é escolha de custo. Com outra massa ou estatística, o planner pode trocar a sonda, mas não há forma hashed para subconsulta escalar. O teste genérico e custom cobre a massa de fixturePlano.

SINAIS:
  SECURITY: sim — a implementação toca internal/storage/ e internal/http/ (security_surface_paths).
  SUPPLY_CHAIN: nao — nenhuma dependência nova; go.mod/go.sum intocados.
  MIGRATION: nao — nenhuma migração; o índice existente emprestimo_livro_aberto_uq atende a listagem e o detalhe.
  PERFORMANCE: sim — a consulta de GET /livros e GET /livros/{id} muda. Com F1, a medição mostra uma sonda por livro (no máximo 101 por página) em emprestimo_livro_aberto_uq, sem hashed, e o CA5 fica coberto por teste.

PROXIMO: dev-implementation
MOTIVO: F1 passou em M1 em todos os casos, e com ela a suíte inteira passa na referência. Os testes, ajustados para F1, falham hoje só por falta da implementação (tipo, consts e assinaturas). Resta ao dev implementar A1' e A2-A6 (handoffs 03 e 06).
