DELIVERY_ID: DEL-013
TASK_CODE: DEL-013
AGENT: dev-implementation
STATUS: ok

ABORDAGEM:
  Implementação mínima contra os testes do 03-test-writer, dentro do 02-architecture-guardrails. Testes não alterados.
  - internal/storage/repositorio.go:
    * var ErrListagemInvalida = errors.New("storage: parâmetros de listagem inválidos"); const LimiteListagemLivros = 100.
    * ListarLivros(ctx, apos int64, limite int) ([]domain.Livro, bool, error): recusa apos < 0 ou limite fora de 1..LimiteListagemLivros com ErrListagemInvalida antes do SQL. SQL único parametrizado `SELECT id, titulo FROM livro WHERE id > $1 ORDER BY id LIMIT $2` com $2 = limite+1; loop rows.Next, para ao ler a linha limite+1 (temMais = true), defer rows.Close + rows.Close explícito antes de checar rows.Err(); erros via traduzirErro; slice criada com make(…, 0, limite) (vazia, não nil); com erro devolve nil, false.
    * BuscarEmprestimo(ctx, id): id <= 0 → ErrEmprestimoNaoEncontrado sem SQL; `SELECT `+colunasEmprestimo+` FROM emprestimo WHERE id = $1` lido por lerEmprestimo (aberto ou devolvido).
  - internal/http/erros.go: códigos parametro_invalido (400, "parâmetro de consulta inválido") e emprestimo_nao_encontrado (404, "empréstimo não encontrado"); storage.ErrListagemInvalida em errosDeBug. classificar() intacto (ErrEmprestimoNaoEncontrado continua → emprestimo_aberto_nao_encontrado).
  - internal/http/handler.go:
    * Repositorio ganha BuscarLivro, ListarLivros, BuscarEmprestimo.
    * Rotas GET /livros, GET /livros/{id}, GET /emprestimos/{id}, todas atrás de auth(...).
    * lerParametrosListagem(rawQuery) (apos, limite, ok): url.ParseQuery (erro → !ok); só chaves limite/apos, len(vals) == 1; limite via strconv.ParseInt base 10 em 1..storage.LimiteListagemLivros, padrão 20 (const limitePadraoListagem); apos via parseID, padrão 0. Sem clamp, sem eco de valor.
    * listarLivros: parâmetros → 400 parametro_invalido; context.WithTimeout(a.timeout); DTO saidaListaLivros{itens, proximo}; itens sempre array; proximo = ID do último item entregue só quando temMais.
    * obterLivro: parseID → id_invalido; BuscarLivro → falharErro; 200 saidaLivro.
    * obterEmprestimo: parseID → id_invalido; BuscarEmprestimo; errors.Is(ErrEmprestimoNaoEncontrado) → falhar(codEmprestimoNaoEncontrado) local; demais → falharErro; sucesso → responderEmprestimo(200, got, a.agora()).

NAO_FAZER:
  - Itens do NAO_FAZER do 02 respeitados: sem OFFSET/COUNT, sem interpolação no SQL, proximo não vem da linha extra, classificar() sem mapeamento global, sem r.URL.Query()/FormValue, sem clamp, nada de limite/apos/query no log, sem mudança em internal/domain/, middleware/, migrations/, go.mod/go.sum.

CONFIRMADO:
  - Testes não tocados: git status mostra só internal/http/erros.go, internal/http/handler.go e internal/storage/repositorio.go como novos modificados por esta etapa (os _test.go modificados/novos são do test-writer).
  - pipeline_sensors (todos ok):
      vet: ok, exit 0, saída vazia
      test: ok, exit 0, "pacotes: 8 (8 ok, 0 com falha, 0 sem testes) / testes: 1830 passaram, 0 falharam, 207 pulados" (pulados = testes de banco, sensor roda sem BIBLIOTECA_TEST_DATABASE_URL)
      build: ok, exit 0
      vuln: ok, "No vulnerabilities found."
      mod_verify: ok, "all modules verified"
  - Suíte com banco: container descartável biblioteca-pg-del013-dev (postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab, mesmo digest do CI/README), POSTGRES_HOST_AUTH_METHOD=trust, 127.0.0.1:5444.
      BIBLIOTECA_TEST_DATABASE_URL='postgres://postgres@127.0.0.1:5444/biblioteca?sslmode=disable' BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 -p 1 -json ./...
      → exit=0; eventos "Action":"pass" 2219 (2211 de testes), "Action":"fail" 0, "Action":"skip" 0; os 8 pacotes pass. Testes de banco da delivery executados: TestBancoListagemLivros, TestBancoDetalhes, TestListarLivros, TestListarLivrosPercorrerTudo, TestListarLivrosVazio, entre outros.
  - Fuzz: go test -run '^$' -fuzz=FuzzParametrosListagem -fuzztime=30s ./internal/http
      fuzz: elapsed: 30s, execs: 262417 (18715/sec), new interesting: 13 (total: 84)
      PASS
      ok  	github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http	30.170s
    (nenhum crasher; nada gravado em testdata/).
  - go vet ./... e go test -count=1 ./... sem banco também ok antes dos sensores.
  - gofmt -l internal/ vazio; git diff --stat -- go.mod go.sum vazio.
  - Limpeza: docker rm -f biblioteca-pg-del013-dev (só o container criado aqui); sem prune, sem go clean. JSON da suíte ficou só no scratchpad da sessão.

ASSUNCOES:
  - Mensagem de ErrListagemInvalida ("storage: parâmetros de listagem inválidos") não contém apos, limite nem SQL.
  - Guarda `temMais && len(livros) > 0` no handler é defensiva (o repositório nunca devolve temMais com 0 itens); não muda o contrato.

ARQUIVOS:
  - internal/storage/repositorio.go — ErrListagemInvalida, LimiteListagemLivros, ListarLivros, BuscarEmprestimo.
  - internal/http/erros.go — codParametroInvalido, codEmprestimoNaoEncontrado no catálogo; ErrListagemInvalida em errosDeBug.
  - internal/http/handler.go — interface Repositorio (+3 métodos), 3 rotas, saidaListaLivros, listarLivros, obterLivro, obterEmprestimo, lerParametrosListagem, limitePadraoListagem; imports net/url e internal/storage.

RISCOS:
  - Os do 02 continuam: contrato público novo (D-19 pelo docs-update), resposta de até ~6,4 MB no pior caso, rejeição de parâmetro desconhecido (cache-buster), enumeração com token.
  - O sensor test roda sem banco (207 pulados); QA deve repetir a suíte com Postgres e BIBLIOTECA_TEST_DB_OBRIGATORIO=1.
  - Working tree inclui a marcação merged da DEL-012 (INDEX.md, DEL-012/report.md), que é desta entrega e não foi revertida; git-delivery cria task/DEL-013 a partir da main d870750.

SINAIS:
  SECURITY: sim — rotas autenticadas novas, entrada externa por query string e consultas novas em internal/http/ e internal/storage/ (security_surface_paths); fuzz do parsing executado.
  SUPPLY_CHAIN: nao — go.mod/go.sum intactos; só stdlib (net/url, strconv) e pgx existente.
  MIGRATION: nao — nenhum DDL.
  PERFORMANCE: sim — consulta de listagem keyset pela PK com LIMIT limite+1 (≤101), uma consulta por requisição, sem OFFSET/COUNT/N+1.

PROXIMO: code-reviewer
MOTIVO: implementação concluída dentro do escopo; sensores ok, suíte com banco 0 fail / 0 skip, fuzz 30s PASS.
