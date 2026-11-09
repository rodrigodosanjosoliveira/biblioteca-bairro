DELIVERY_ID: DEL-015
TASK_CODE: DEL-015
AGENT: test-writer
STATUS: ok

CONTRATO EXIGIDO PELOS TESTES (o dev implementa exatamente isto):
  - storage: `type Situacao` inteiro, constantes em iota na ordem `SituacaoTodas` (zero), `SituacaoAberto`, `SituacaoDevolvido`, `SituacaoVencido` (os testes usam `SituacaoVencido + 1` e `+ 100` como inválidas); `FiltroEmprestimos{UsuarioID, LivroID int64; Situacao Situacao; Ref time.Time}`; `ListarEmprestimos(ctx, f FiltroEmprestimos, apos int64, limite int)`; função não exportada `sqlListarEmprestimos(f FiltroEmprestimos) (string, []any)`, que devolve só os args extras (sem apos/limite), com o vencido como `vencimentoParaDate(f.Ref)` (time.Time UTC).
  - O texto SQL exato é `SELECT id, livro_id, usuario_id, inicio, vencimento, devolucao, renovacoes FROM emprestimo WHERE id > $1` + fragmentos ` AND usuario_id = $N`, ` AND livro_id = $N`, ` AND devolucao IS NULL` | ` AND devolucao IS NOT NULL` | ` AND devolucao IS NULL AND vencimento < $N` (ordem usuário, livro, situação; $N a partir de 3) + ` ORDER BY id LIMIT $2`. Ref só vira arg com vencido. Query recebe `apos, limite+1, extras...`.
  - ErrListagemInvalida antes de SQL: UsuarioID<0, LivroID<0, Situacao > SituacaoVencido, vencido com Ref zero. Ref zero é aceita nas outras situações.
  - http: `GET /emprestimos` passa `Ref = a.agora()`, lido uma vez antes de ListarEmprestimos, em TODA requisição (não só com vencido); a multa usa o mesmo instante. `GET /usuarios/{id}/emprestimos`: chamadas exatas `BuscarUsuario,ListarEmprestimos`, com o filtro de `UsuarioID` do path; usuário inexistente devolve só `BuscarUsuario` e 404 `usuario_inexistente` em toda página; prazo (4s,5s] nas duas chamadas.
  - migrations: `0003_indices_listagem_emprestimo.{up,down}.sql` com o texto do handoff 03.

CONFIRMADO:
  - Falha esperada no tree (PostgreSQL 16-alpine com o digest do CI, container del015-testwriter-pg em modo trust, `BIBLIOTECA_TEST_DB_OBRIGATORIO=1`, `go test -count=1 ./...`):
    - `internal/storage [build failed]` e `internal/http [build failed]`, com `undefined: FiltroEmprestimos / Situacao / SituacaoTodas / storage.FiltroEmprestimos`;
    - `migrations`: `TestUp0003` e `TestDown0003` falham com `open 0003_indices_listagem_emprestimo.{up,down}.sql: file does not exist`;
    - cmd, config, domain, middleware e migrate: ok.
  - Referência em cópia (mktemp -d fora da árvore): o storage e o handler com o contrato acima e a 0003 do handoff 03. Com ela, `go vet ./...` limpo e `go test -count=1 ./...` todo ok com banco (storage 14s, dos quais ~8s são da massa do EXPLAIN).
  - Fuzz na cópia: `go test -fuzz=^FuzzParametrosEmprestimos$ -fuzztime=30s ./internal/http` deu PASS com 463499 execs. `-fuzz=^FuzzParametrosListagemNovasRotas$ -fuzztime=30s` deu PASS com 400291 execs.
  - Mutantes: 44 mortos, 0 sobreviventes.
    - Storage (16): sem cada uma das 4 validações; `<=` no vencido; hoje em UTC; CURRENT_DATE; ordem livro/usuário; `NOT (... IS NOT NULL)`; devolvido trocado; listarPorID ignorando os extras; placeholder a partir de $2; filtro ignorado; vencido sem `devolucao IS NULL`; valor concatenado no SQL; SQL único `($N = 0 OR ...)`.
    - HTTP (14): situação case-insensitive; `usuario_id` aceito na rota aninhada; sem BuscarUsuario; query validada antes do id; inexistente virando 422; sem Ref; relógio lido 2x; `situacao=` vazia aceita; `livro_id=0`; `usuario_id` ignorado; aninhada sem UsuarioID do path; usuário conferido só na 1ª página; `/livros` aceitando filtro; chave repetida aceita.
    - Migração (8): sem o parcial; IF NOT EXISTS; IF EXISTS no down; down sem um DROP; parcial com `IS NOT NULL`; composto `(id, usuario_id)`; UNIQUE; down removendo índice da 0001.
    - Isolados: `TestListarEmprestimosVencidoFronteira` sozinho mata `<=`, hoje em UTC e CURRENT_DATE. `TestPlanoListarEmprestimos` sozinho mata a falta do parcial, do composto de usuário e do composto de livro.
  - Limpeza: cópia, scripts e schema de exploração removidos; container removido com `docker rm -f del015-testwriter-pg`; sem prune nem go clean.

ASSUNCOES:
  - DIVERGÊNCIA DO HANDOFF 03 (EXPLAIN), a confirmar pelo code-reviewer/humano: com a massa proposta (199 usuários, 2000 livros, 20000 linhas), cada usuário tem ~100 linhas e cada livro ~10, menos que limite+1. Nessa massa o PG 16 escolhe Bitmap Heap Scan + Sort nos índices simples para 5-9 e 12, e BitmapAnd para 13-16, o que é correto por custo. Conferido com EXPLAIN no container, por exemplo: `Bitmap Index Scan on emprestimo_usuario_id_idx` para `usuario_id = 7 ... LIMIT 101`.
    - Massa trocada, como o handoff 03 manda: 799 usuários, 500 livros, 200000 linhas, 499 abertos (um por livro) espalhados no heap.
    - Com ela, as asserções do 03 valem sem mudança para 1-5 e 8-12 (índice único em 1-5, 8, 9 e 12).
    - Para 6, 7 e 13-16 (duas condições seletivas e independentes), nenhuma proporção faz o planner trocar a interseção de bitmaps por Index Scan composto sem `SET enable_*` ou estatística estendida, ambos fora do escopo.
    - Nessas seis combinações o teste aceita, além dos Index Scans do conjunto, um Bitmap Heap Scan sobre BitmapAnd de 2 ou mais Bitmap Index Scans em índices de emprestimo, com Sort acima. As folhas podem ser os índices simples.
    - Em 6, 7, 14 e 15 aceita também `emprestimo_aberto_id_idx`, que é limitado pelo número de livros.
    - Continuam proibidos: Seq Scan, Bitmap sem interseção e Index Scan nos índices simples.
    - Isto afrouxa a asserção nessas 6 combinações. Está documentado no topo de `internal/storage/plano_listagem_test.go`.
  - Edições em testes de /usuarios além das apontadas no 02, inevitáveis:
    - `TestMuxNotFoundEMethodNotAllowed` esperava 404 em `GET /usuarios/1/emprestimos`. O caso foi trocado por `/usuarios/1/x`, `/usuarios/1/emprestimos/`, `/usuarios/1/emprestimos/2`, POST/DELETE 405 e `GET /livros/1/emprestimos` 404 (U2).
    - `TestListagensNovasParametros` (tabela compartilhada /usuarios + /emprestimos): os 4 casos de filtro continuam 400 em /usuarios e passam a 200 em /emprestimos.
    - `rotasProtegidas()` ganhou a rota aninhada. Os testes de auth e de timeout passam a cobri-la.
  - `listagem_usuarios_emprestimos_test.go:581` (`/emprestimos?situacao=aberto` sem auth → 401) continua válido e não foi mudado.
  - `TestVencimentoParaDateFronteiraVencido` já passa hoje (função existente). É regressão pedida pelo 02, não falha esperada.

ARQUIVOS:
  - internal/storage/listagem_filtro_sem_banco_test.go — novo:
    - 16 combinações com SQL e args exatos, 16 textos distintos;
    - SQL sem filtro igual byte a byte ao atual;
    - nenhum valor e nenhum now/CURRENT_DATE/AT TIME ZONE/COALESCE/OR no texto;
    - $hoje pela data civil SP;
    - args `[apos, limite+1, extras...]`;
    - validação (ErrListagemInvalida sem SQL) e extremos aceitos;
    - erro sem dados.
  - internal/storage/listagem_filtro_test.go — novo, com banco:
    - 16 combinações contra oráculo do domínio e conjuntos escritos à mão, limites 1/2/3/100, `apos` combinado, vazios;
    - R3 `TestListarEmprestimosVencidoFronteira`: vencido = {Multa > 0} = {Renovar == ErrEmprestimoVencido} às 23:59:59, 00:00 e 12:00 SP (e UTC+14), em 5 fusos de sessão do banco.
  - internal/storage/plano_listagem_test.go — novo: `TestPlanoListarEmprestimos`, com plano genérico e custom (apos 0/15000) das 16 combinações e a regressão sem filtro na PK.
  - internal/storage/esquema_test.go — 9 índices esperados; definição exata dos 3 da 0003; helpers `indicesDoSchema`, `aplicarArquivo`; subteste "down só da 0003" em `TestMigracaoUpDownUp`.
  - internal/storage/listagem_usuarios_emprestimos_test.go, internal/storage/listagem_sem_banco_usuarios_emprestimos_test.go — chamadas com `FiltroEmprestimos{}`.
  - migrations/indices_listagem_test.go — novo: `TestUp0003`, `TestDown0003`.
  - internal/http/apoio_test.go — repoFake com a assinatura nova e `listarEmprFiltro`; rota aninhada em `rotasProtegidas()`.
  - internal/http/listagem_filtro_emprestimos_test.go — novo:
    - parsing das duas rotas (~45 casos cada);
    - id da aninhada; ordem id_invalido → parametro_invalido → usuario_inexistente;
    - mapa de erros; prazo; contrato da página; relógio único com `Ref` = instante;
    - auth antes de tudo; log sem valores;
    - /livros e /usuarios recusam as chaves;
    - `FuzzParametrosEmprestimos`.
  - internal/http/banco_listagem_filtro_test.go — novo, com banco: filtros e rota aninhada pela API, fronteira 23:59:59 e 00:00 SP, item igual ao detalhe, vencido nunca com multa 0.00, 404 em toda página.
  - internal/http/listagem_usuarios_emprestimos_test.go — `TestListagensNovasParametros` (casos de filtro); `FuzzParametrosListagemNovasRotas` com oráculo de empréstimos para /emprestimos.
  - internal/http/handler_test.go — casos do mux da subrota de usuário (ver ASSUNCOES).

RISCOS:
  - EXPLAIN em 6, 7 e 13-16 aceita a interseção de bitmaps. Uma regressão que troque o composto por outro índice nessas combinações não é pega ali. É pega em 5, 8, 9 e 12 e pelos mutantes G9/G10.
  - Massa do EXPLAIN de 200k linhas: cerca de 8s por execução no CI. Planos podem mudar com versão do planner. O CI fixa o digest do 16-alpine.
  - `Ref` sempre preenchida e verificada (`filtro.Ref == agora`) em todas as requisições: se o dev só preencher com vencido, 3 testes HTTP falham. É o que o 02 desenha.

SINAIS:
  SECURITY: sim — entrada externa nova em internal/http, SQL montado por fragmentos em internal/storage (testes de "valores só nos args" e mutante de concatenação morto), migração.
  SUPPLY_CHAIN: nao — nenhuma dependência nova; só stdlib e pgx já presentes.
  MIGRATION: sim — 0003 aditiva, testada por texto, catálogo e down isolado.
  PERFORMANCE: sim — EXPLAIN genérico e custom em teste com banco, com a divergência de massa e de asserção registrada em ASSUNCOES.

PROXIMO: dev-implementation
MOTIVO: os testes falham pelo motivo certo (símbolos e arquivos da 0003 inexistentes) e passam com uma referência curta, com 44/44 mutantes mortos. Falta a implementação no tree. A divergência do EXPLAIN (6, 7, 13-16) deve ser confirmada pelo code-reviewer/humano.
