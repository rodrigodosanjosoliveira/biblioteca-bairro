DELIVERY_ID: DEL-017
TASK_CODE: PROJ-7
AGENT: dev-implementation
STATUS: ok

ESCOPO: implementação de A1' (F1, handoff 06) e A2-A4 (handoff 03). Nenhum `_test.go` foi editado. D-23, coleção Postman, mapa de módulos e report ficam com o docs-update (abaixo, em PARA_DOCS_UPDATE).

IMPLEMENTADO:
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/repositorio.go
    - `type LivroComDisponibilidade struct { Livro domain.Livro; Disponivel bool }`: campo nomeado, sem embedding.
    - As consts `sqlListarLivros` e `sqlBuscarLivro` usam F1 exatamente: `(SELECT 1 FROM emprestimo e WHERE e.livro_id = l.id AND e.devolucao IS NULL LIMIT 1) IS NULL AS disponivel`, com FROM livro l. A listagem tem `WHERE l.id > $1 ORDER BY l.id LIMIT $2` e o detalhe `WHERE l.id = $1`. Nenhum comentário dentro do SQL. A palavra EXISTS aparece só no comentário Go, fora da string.
    - `BuscarLivro(ctx, id) (LivroComDisponibilidade, error)`: `id <= 0` devolve ErrLivroNaoEncontrado sem SQL, ErrNoRows vira ErrLivroNaoEncontrado e o resto passa por traduzirErro.
    - `ListarLivros(ctx, apos, limite) ([]LivroComDisponibilidade, bool, error)` só troca o SQL e o T da closure (Scan de 3 colunas). `listarPorID`, ListarUsuarios, ListarEmprestimos e sqlListarEmprestimos não foram tocados.
  - /home/rodrigo/workspace/biblioteca-bairro/internal/http/handler.go
    - Na interface `Repositorio`, BuscarLivro e ListarLivros passam a ter as assinaturas novas.
    - DTO novo `saidaLivroLido {id, titulo, disponivel}`, com `Disponivel bool` sem omitempty e sem ponteiro, e o helper `novaSaidaLivroLido`. É usado por obterLivro e pelos itens de saidaListaLivros. O `proximo` passa a ler `livros[len-1].Livro.ID`.
    - `saidaLivro` e criarLivro (POST /livros) não mudaram. listarLivros e obterLivro não leem o relógio, e `lerParametrosListagem` não mudou.
  Não tocados: internal/domain/, migrations/, go.mod, go.sum, cmd/, nenhum teste.

NAO_FAZER (relido e cumprido): sem campo em domain.Livro; listarPorID intocado; sem EXISTS/IN/ANY, JOIN, LATERAL, COALESCE, agregação, relógio ou vencimento; sem segunda consulta nem N+1; sem SET enable_*; sem migração; saidaLivro e POST /livros inalterados; sem `*bool`/omitempty; sem `?disponivel=`; sem método paralelo; nada novo em log ou mensagem de erro.

CONFIRMADO:
  - `go build ./... && go vet ./...` → `BUILD_VET_OK` (nenhuma saída de erro).
  - Banco: postgres:16-alpine com o digest do CI (sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab), trust, porta 55432, descartável (--rm, já parado).
    `BIBLIOTECA_TEST_DATABASE_URL=postgres://postgres@localhost:55432/postgres?sslmode=disable BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 -p 1 -v ./...` → exit=0
      ok  cmd/biblioteca-bairro 7.297s
      ok  internal/config 0.004s
      ok  internal/domain 0.012s
      ok  internal/http 3.954s
      ok  internal/http/middleware 0.005s
      ok  internal/migrate 16.399s
      ok  internal/storage 27.349s
      ok  migrations 0.006s
      SKIP count: 0 (grep "--- SKIP" no log -v); PASS count: 3109
      --- PASS: TestSQLLeituraLivro (listagem, detalhe)
      --- PASS: TestLeituraLivroUmaConsulta (página mista, página vazia, detalhe disponível, detalhe indisponível, detalhe inexistente, detalhe com id zero não consulta)
      --- PASS: TestPlanoLeituraLivro (10.35s): detalhe genérico, custom livro 1, 151 e inexistente; listagem genérica, custom apos=0 com limite+1 101/21/2, apos=250 e apos=450 com limite+1=101; "valores na massa do plano"; "noPlano lê SubPlan"
  - Fuzz dos alvos que passam pelos handlers de livro (30 s cada):
      go test -run='^$' -fuzz='^FuzzParseID$' -fuzztime=30s ./internal/http
        fuzz: elapsed: 31s, execs: 52179 (0/sec), new interesting: 5 (total: 113) / PASS / ok internal/http 31.478s
      go test -run='^$' -fuzz='^FuzzParametrosListagem$' -fuzztime=30s ./internal/http
        fuzz: elapsed: 30s, execs: 114020 (0/sec), new interesting: 3 (total: 100) / PASS / ok internal/http 30.226s
  - `bin/pipeline-claude sensors`, com as mesmas variáveis do banco:
      [ok] vet 254 ms
      [ok] test 68.0 s
      [ok] build 1.1 s
      [ok] vuln 8.7 s
      [ok] mod_verify 1.5 s
      Resultado: 5 sensor(es), 0 falha(s): aprovado.
  - `gofmt -l internal/` sem saída.
  - Diff de produção: internal/http/handler.go | 29 +++++++++++-----, internal/storage/repositorio.go | 75 +++++++++++++++++++++++++++++------------ (2 arquivos, 75 inserções, 29 remoções).

ASSUNCOES:
  - D-23, as descrições da coleção Postman (GET /livros em postman/biblioteca-bairro.postman_collection.json:145 e GET /livros/{id} em :159) e a linha nova do mapa de módulos ficam com o docs-update. Motivo: o papel do dev é fazer os testes passarem sem sair do escopo, e o docs-update existe no fluxo para isso. O texto proposto está abaixo.
  - O report (docs-ai/deliveries/DEL-017/report.md) não foi editado, porque docs-ai/deliveries/ é do docs-update. A seção Dev vai pronta em TEXTO_REPORT.

PARA_DOCS_UPDATE:
  - D-23 em /home/rodrigo/workspace/biblioteca-bairro/docs-ai/DECISOES.md, com os pontos de A5 (handoff 03) e a forma F1 (handoff 06):
    - `disponivel` é aditivo em GET /livros e GET /livros/{id}. Quebra cliente com decodificação estrita, como o precedente da D-17.
    - Revisa a D-19 em dois pontos: o estado "emprestado" entra na listagem, e GET /livros/{id} deixa de usar o DTO de POST /livros.
    - Disponível significa sem empréstimo aberto, e vencido conta como indisponível. Não depende do relógio.
    - É estado derivado (D-12), por isso fica fora de domain.Livro, no tipo de leitura storage.LivroComDisponibilidade.
    - A forma é a subconsulta escalar `LIMIT 1 ... IS NULL`, e não NOT EXISTS: medido no PG16, NOT EXISTS vira hashed SubPlan, que é O(abertos) por página.
    - Há uma sonda por livro em emprestimo_livro_aberto_uq (R4/D-13), conferida por EXPLAIN genérico e custom em TestPlanoLeituraLivro.
    - A leitura é sem lock, com coerência por statement.
    - Não há migração.
    - Coleção Postman atualizada conforme a D-22.
  - Mapa de módulos (/home/rodrigo/workspace/biblioteca-bairro/docs-ai/03-MAPA-RAPIDO-MODULOS.md), duas linhas DEL-017:
    - storage: `repositorio.go` (`LivroComDisponibilidade`, `sqlListarLivros`, `sqlBuscarLivro`; assinaturas novas de BuscarLivro/ListarLivros), com a disponibilidade por subconsulta escalar sondando emprestimo_livro_aberto_uq (D-23).
    - http: `handler.go` (`saidaLivroLido`, `novaSaidaLivroLido`; interface Repositorio), com `disponivel` em GET /livros e GET /livros/{id} e POST /livros ainda `{id, titulo}` (D-23).
  - Postman: só as descrições de GET /livros e GET /livros/{id} passam a citar `{id, titulo, disponivel}`. Sem exemplos de resposta e sem mexer em POST /livros.

TEXTO_REPORT (seção Dev de /home/rodrigo/workspace/biblioteca-bairro/docs-ai/deliveries/DEL-017/report.md):
  Implementação em internal/storage/repositorio.go:
    - tipo de leitura `LivroComDisponibilidade{Livro, Disponivel}`;
    - consts `sqlListarLivros`/`sqlBuscarLivro` na forma F1 (subconsulta escalar `(SELECT 1 FROM emprestimo e WHERE e.livro_id = l.id AND e.devolucao IS NULL LIMIT 1) IS NULL AS disponivel`, FROM só de livro);
    - BuscarLivro e ListarLivros com as assinaturas novas, sem mexer em listarPorID.
  Em internal/http/handler.go:
    - interface Repositorio atualizada;
    - DTO de leitura `saidaLivroLido` com `disponivel` bool sem omitempty, usado em GET /livros e GET /livros/{id};
    - POST /livros inalterado (`saidaLivro`).
  Sem mudança em domain, migrations, go.mod/go.sum e testes.
  Gates:
    - go build e go vet limpos;
    - `go test -count=1 -p 1 ./...` com Postgres 16-alpine do digest do CI e BIBLIOTECA_TEST_DB_OBRIGATORIO=1: todos os pacotes ok, 0 SKIP, incluindo TestPlanoLeituraLivro (10 casos genéricos e custom), TestSQLLeituraLivro e TestLeituraLivroUmaConsulta;
    - fuzz de FuzzParseID e FuzzParametrosListagem por 30 s cada: PASS;
    - `bin/pipeline-claude sensors`: vet, test, build, vuln e mod_verify ok (5 sensores, 0 falhas).

RISCOS:
  - Os mesmos do handoff 03: o contrato aditivo e a exposição em lote do estado de empréstimo, para o security-review.
  - O plano é escolha de custo. TestPlanoLeituraLivro é a barreira em versão futura do PG.

SINAIS:
  SECURITY: sim — o diff toca internal/storage/ e internal/http/ (security_surface_paths); contrato aditivo e exposição em lote do estado "emprestado".
  SUPPLY_CHAIN: nao — sem dependência nova; go.mod/go.sum intocados (mod_verify ok).
  MIGRATION: nao — sem migração nem índice; usa emprestimo_livro_aberto_uq existente.
  PERFORMANCE: sim — consulta de GET /livros e GET /livros/{id} mudou; plano F1 com uma sonda por livro, conferido por TestPlanoLeituraLivro (genérico e custom) no PG16 do CI.

PROXIMO: code-reviewer
MOTIVO: os testes do test-writer passam sem alteração, com banco e sem SKIP, e os 5 sensores foram aprovados. O diff de produção se limita a repositorio.go e handler.go. O code-reviewer revisa; depois vêm security-review (superfície tocada), qa-validation e docs-update (D-23, Postman, mapa e report).
