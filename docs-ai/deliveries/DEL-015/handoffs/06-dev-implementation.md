DELIVERY_ID: DEL-015
TASK_CODE: DEL-015
AGENT: dev-implementation
STATUS: ok

IMPLEMENTADO (só o que o handoff 05 cobra; NAO_FAZER do 02 relido antes):
  - migrations/0003_indices_listagem_emprestimo.up.sql: texto do handoff 03, sem alteração (comentário e os 3 CREATE INDEX: `emprestimo_usuario_id_id_idx (usuario_id, id)`, `emprestimo_livro_id_id_idx (livro_id, id)`, `emprestimo_aberto_id_idx (id) WHERE devolucao IS NULL`).
  - migrations/0003_indices_listagem_emprestimo.down.sql: texto do handoff 03 (3 DROP INDEX, em ordem inversa, sem IF EXISTS nem CASCADE).
  - internal/storage/repositorio.go:
    - `type Situacao int` com as constantes em iota `SituacaoTodas` (zero), `SituacaoAberto`, `SituacaoDevolvido`, `SituacaoVencido`.
    - `FiltroEmprestimos{UsuarioID, LivroID int64; Situacao Situacao; Ref time.Time}` e o método não exportado `valido()`. Ele recusa ids < 0, situação fora de Todas..Vencido e vencido com Ref zero.
    - `ListarEmprestimos(ctx, f, apos, limite)` devolve ErrListagemInvalida antes de qualquer SQL quando o filtro é inválido. Depois chama `listarPorID` com os extras.
    - `sqlListarEmprestimos(f) (string, []any)` usa só fragmentos constantes, na ordem usuário, livro, situação. O placeholder é `"$" + strconv.Itoa(len(args)+2)`, com os extras a partir de $3.
    - No vencido, os fragmentos são ` AND devolucao IS NULL AND vencimento < $N` e o arg é `vencimentoParaDate(f.Ref)`. Há comentário citando D-17/D-11 e a equivalência com o domínio.
    - Com o filtro zero, o SQL é byte a byte o anterior.
    - `listarPorID(..., ler, extra ...any)` passa `apos, limite+1, extra...` ao Query. O `+1`, o teto e a validação continuam só ali. ListarLivros e ListarUsuarios não mudaram de chamada.
    - O doc de ErrListagemInvalida passou a citar o filtro.
  - internal/http/handler.go:
    - Interface `Repositorio`: só a assinatura de ListarEmprestimos mudou.
    - Rota nova `GET /usuarios/{id}/emprestimos`, atrás do mesmo `auth(...)`.
    - `listarEmprestimos` usa `lerParametrosEmprestimos(raw, false)`.
    - `listarEmprestimosDoUsuario` segue a ordem `parseID` → 400 id_invalido, depois o parser com aninhada=true → 400 parametro_invalido, depois um único `context.WithTimeout`, depois `BuscarUsuario` em toda página. ErrUsuarioNaoEncontrado é mapeado localmente para 404 usuario_inexistente.
    - Helper comum `responderPaginaEmprestimos`: lê `a.agora()` uma vez, faz `filtro.Ref = agora` em toda requisição, chama ListarEmprestimos e calcula a multa com o mesmo instante. A montagem da página não mudou.
    - Parser comum `lerQueryListagem(raw, extra)`, com o corpo antigo de limite/apos, chave repetida e chave desconhecida. `lerParametrosListagem` delega a ele sem chaves extras, então /livros e /usuarios continuam só com limite/apos.
    - `lerParametrosEmprestimos(raw, aninhada)` aceita `situacao` (comparação exata com aberto/devolvido/vencido) e `livro_id`. `usuario_id` só fora da rota aninhada. Os ids passam pelo parseID.
  - Sem código de erro novo, sem log novo, sem mudança em internal/domain/, internal/migrate/, 0001/0002, go.mod/go.sum. Nenhum arquivo de teste alterado: o diff meu é só handler.go, repositorio.go e os dois .sql. A marcação merged da DEL-014 (INDEX.md e DEL-014/report.md) ficou intacta.

CONFIRMADO:
  - Suíte com banco: PostgreSQL `postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab` (digest do CI), container `del015-dev-pg`, `POSTGRES_HOST_AUTH_METHOD=trust`, porta 127.0.0.1:55415. Comando: `BIBLIOTECA_TEST_DATABASE_URL=... BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 -v ./...` com EXIT=0:
      ok  	.../cmd/biblioteca-bairro	7.543s
      ok  	.../internal/config	0.007s
      ok  	.../internal/domain	0.023s
      ok  	.../internal/http	4.304s
      ok  	.../internal/http/middleware	0.008s
      ok  	.../internal/migrate	15.197s
      ok  	.../internal/storage	13.496s
      ok  	.../migrations	0.005s
    Contagem no log: 3019 `--- PASS`, 0 `--- FAIL`, 0 `--- SKIP` (grep -ci skip = 0).
    Destaques:
      --- PASS: TestMigracaoUpDownUp (0.20s)
      --- PASS: TestEsquemaRestricoes (0.12s)
      --- PASS: TestSQLListarEmprestimosCombinacoes (0.00s)
      --- PASS: TestListarEmprestimosVencidoFronteira (0.25s)
      --- PASS: TestPlanoListarEmprestimos (8.87s)
      --- PASS: TestUp0003 (0.00s)
      --- PASS: TestDown0003 (0.00s)
  - Fuzz, 30s cada, em ./internal/http:
      `go test -run='^$' -fuzz='^FuzzParametrosEmprestimos$' -fuzztime=30s ./internal/http`
        fuzz: elapsed: 30s, execs: 314292 (0/sec), new interesting: 5 (total: 85)
        PASS
        ok  	.../internal/http	30.154s
      `go test -run='^$' -fuzz='^FuzzParametrosListagemNovasRotas$' -fuzztime=30s ./internal/http`
        fuzz: elapsed: 30s, execs: 272883 (0/sec), new interesting: 8 (total: 97)
        PASS
        ok  	.../internal/http	30.137s
      `go test -run='^$' -fuzz='^FuzzParametrosListagem$' -fuzztime=30s ./internal/http` (o parser antigo foi refatorado para o helper comum)
        fuzz: elapsed: 31s, execs: 198209 (0/sec), new interesting: 9 (total: 93)
        PASS
        ok  	.../internal/http	31.099s
  - Sensores (pipeline_sensors) com ok=true:
      vet: ok (exit 0, sem saída)
      test: ok (exit 0). "pacotes: 8 (8 ok, 0 com falha, 0 sem testes); testes: 2386 passaram, 0 falharam, 220 pulados". Os 220 pulados são os testes de banco: o sensor roda sem BIBLIOTECA_TEST_DATABASE_URL ("BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado"). A cobertura com banco é a da execução acima, com 0 SKIP.
      build: ok (exit 0)
      vuln: ok, "No vulnerabilities found."
      mod_verify: ok, "all modules verified"
  - `grep -rni concurrently migrations/0003_*`: nenhuma ocorrência (exit 1).
  - Limpeza: `docker rm -f del015-dev-pg` executado. `docker ps -a | grep -c del015` dá 0. Sem prune e sem go clean. O log ficou só no scratchpad da sessão. O fuzz não gravou testdata (git status sem testdata novo).

ASSUNCOES:
  - Em `GET /usuarios/{id}/emprestimos`, o relógio é lido depois de BuscarUsuario, dentro do helper comum, uma vez por requisição e antes de ListarEmprestimos. Os testes de relógio único e Ref passam.
  - A divergência do EXPLAIN (combinações 6, 7, 13-16 aceitam BitmapAnd), registrada pelo test-writer, continua para confirmação do code-reviewer e do humano. O teste passou com a implementação.

ARQUIVOS:
  - migrations/0003_indices_listagem_emprestimo.up.sql (novo)
  - migrations/0003_indices_listagem_emprestimo.down.sql (novo)
  - internal/storage/repositorio.go
  - internal/http/handler.go

RISCOS:
  - O sensor `test` sem DSN pula os testes de banco. A barreira R3 (domínio x SQL) e o EXPLAIN só valem com o CI no PostgreSQL e `BIBLIOTECA_TEST_DB_OBRIGATORIO=1`, como hoje.
  - Os riscos herdados continuam: lock SHARE da 0003 (U3, runbook), índices redundantes até a remoção, paginação de vencido através da meia-noite SP e R4 (histórico por usuário) para o security-review.

SINAIS:
  SECURITY: sim. Entrada externa nova (situacao, usuario_id, livro_id e a rota aninhada) em internal/http. SQL por fragmentos constantes com os valores só em args, em internal/storage. Migração 0003.
  SUPPLY_CHAIN: nao. Sem mudança em go.mod/go.sum; só stdlib (strconv) e pgx existente.
  MIGRATION: sim. 0003 aditiva com 3 CREATE INDEX e down com 3 DROP INDEX, no texto do handoff 03.
  PERFORMANCE: sim. Keyset filtrado nos índices novos; TestPlanoListarEmprestimos passou no PG 16 com o digest do CI.

PROXIMO: code-reviewer
MOTIVO: implementação mínima concluída. Testes com banco (0 SKIP), os três fuzz de 30s e todos os sensores passaram. Falta a revisão de código, depois migration-reviewer, security-review e qa.
