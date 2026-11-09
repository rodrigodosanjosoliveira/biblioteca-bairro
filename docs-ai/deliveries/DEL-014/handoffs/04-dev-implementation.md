DELIVERY_ID: DEL-014
TASK_CODE: DEL-014
AGENT: dev-implementation
STATUS: ok

CONFIRMADO:
  - NAO_FAZER do handoff 02 relido e seguido: sem migração, sem OFFSET/COUNT, sem JOIN, sem consulta por item, sem CollectRows/ForEachRow, a.agora() uma vez por requisição e antes do SQL, classificar() e a entrada usuario_nao_encontrado (422) intactos, nenhum nome existente renomeado nem removido, assinatura e SQL de ListarLivros iguais, go.mod/go.sum intocados, sem r.URL.Query(), nenhum arquivo de teste alterado.
  - Storage (internal/storage/repositorio.go):
      - Nova `const LimiteListagem = 100`; `LimiteListagemLivros = LimiteListagem` (mantida exportada). Comentário de ErrListagemInvalida estendido às três listagens.
      - Helper genérico não exportado `listarPorID[T any](ctx, db DBTX, sql, apos, limite, ler func(pgx.Rows) (T, error)) ([]T, bool, error)`: recusa apos < 0 e limite fora de 1..LimiteListagem com ErrListagemInvalida antes do SQL; Query(sql, apos, limite+1); sai do laço em len == limite sem escanear a linha extra; slice vazia não nil; rows.Close() + rows.Err() com traduzirErro; erro de leitura → nil, false, err (sem página parcial).
      - ListarLivros delega ao helper (mesmo SQL, mesma assinatura, mesmo Scan com traduzirErro).
      - ListarUsuarios: `SELECT id, nome FROM usuario WHERE id > $1 ORDER BY id LIMIT $2`.
      - ListarEmprestimos: `SELECT `+colunasEmprestimo+` FROM emprestimo WHERE id > $1 ORDER BY id LIMIT $2`, cada linha via lerEmprestimo(rows) (mesmo erro "storage: empréstimo gravado inválido: %w").
  - HTTP:
      - internal/http/handler.go: interface Repositorio ganha BuscarUsuario, ListarUsuarios, ListarEmprestimos. Rotas novas atrás de auth: GET /usuarios (listarUsuarios), GET /usuarios/{id} (obterUsuario), GET /emprestimos (listarEmprestimos). DTOs saidaListaUsuarios e saidaListaEmprestimos ({itens, proximo}). obterUsuario mapeia localmente storage.ErrUsuarioNaoEncontrado → usuario_inexistente; demais erros via falharErro. listarEmprestimos: lerParametrosListagem → agora := a.agora() → WithTimeout → ListarEmprestimos → montarSaidaEmprestimo de todos os itens com o mesmo agora → erro em qualquer item = falharErro antes de escrever. responderEmprestimo passa a usar a função pura montarSaidaEmprestimo (comportamento idêntico). lerParametrosListagem usa storage.LimiteListagem (mesmo valor 100); comentários atualizados.
      - internal/http/erros.go: código `usuario_inexistente` → 404 "usuário não encontrado" no catálogo.
  - Sensores (pipeline_sensors, sem banco): ok=true.
      vet: ok, exit 0, saída vazia
      test: ok, exit 0 — "pacotes: 8 (8 ok, 0 com falha, 0 sem testes) / testes: 2094 passaram, 0 falharam, 216 pulados" (os 216 pulados são todos de banco: "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado")
      build: ok, exit 0, saída vazia
      vuln: ok, exit 0 — "No vulnerabilities found."
      mod_verify: ok, exit 0 — "all modules verified"
  - Suíte com banco: container postgres:16-alpine `del014-dev-pg` (POSTGRES_HOST_AUTH_METHOD=trust, 127.0.0.1:33464), `BIBLIOTECA_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:33464/biblioteca_test?sslmode=disable BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 -v ./...` → exit=0; contagem no log: `--- SKIP` 0, `--- PASS` 2553, `--- FAIL` 0.
      ok  .../cmd/biblioteca-bairro	7.077s
      ok  .../internal/config	0.012s
      ok  .../internal/domain	0.028s
      ok  .../internal/http	3.957s
      ok  .../internal/http/middleware	0.010s
      ok  .../internal/migrate	13.324s
      ok  .../internal/storage	3.322s
      ok  .../migrations	0.006s
    Inclui PASS de TestBancoListagemLivros, TestBancoDetalhes, TestBancoListagemUsuarios, TestBancoListagemEmprestimos, TestListarUsuarios, TestListarUsuariosPercorrerTudo, TestListarEmprestimos, TestListarEmprestimosPercorrerTudo, TestListarEmprestimosLinhaInvalida, TestListagensNovasVazio, TestListagensNovasRecusasComBanco, TestListarLivros*, TestCatalogoUsuarioInexistente, TestListarEmprestimosInstanteUnico, TestListarEmprestimosItemInvalido, TestRotasNovasMapaDeErros, TestMuxNotFoundEMethodNotAllowed, TestAuthRotasProtegidas, TestTimeoutPorOperacao, TestListagensNovasLinhaExtra, TestLogRotasNovasSemQueryNemNome. Testes de GET /livros passam sem edição.
  - Fuzz: `go test -run '^$' -fuzz=FuzzParametrosListagemNovasRotas -fuzztime=30s ./internal/http/`
      fuzz: elapsed: 30s, execs: 529349 (0/sec), new interesting: 64 (total: 85)
      PASS
      ok  	github.com/rodrigodosanjosoliveira/biblioteca-bairro/internal/http	30.170s
  - Limpeza: container del014-dev-pg removido com `docker rm -f` por nome (docker ps -a --filter name=del014 vazio); corpus gerado só deste alvo removido por nome ($GOCACHE/fuzz/.../internal/http/FuzzParametrosListagemNovasRotas, que não existia antes; os corpora dos outros alvos ficaram); log temporário no scratchpad removido. Sem prune nem go clean.
  - Marcação merged da DEL-013 (INDEX.md, DEL-013/report.md) mantida, não revertida.

ASSUNCOES:
  - Optei pelo helper genérico (forma recomendada no 02) e refatorei ListarLivros para delegar; a garantia de comportamento idêntico são os testes de livros (com e sem banco) passando sem edição.
  - lerParametrosListagem passou a referenciar storage.LimiteListagem em vez de LimiteListagemLivros (mesmo valor, ver 02: "o teto passa a vir de storage.LimiteListagem").

ARQUIVOS:
  - internal/storage/repositorio.go — LimiteListagem, listarPorID, ListarLivros delegando, ListarUsuarios, ListarEmprestimos
  - internal/http/handler.go — interface, 3 rotas, listarUsuarios, obterUsuario, listarEmprestimos, saidaListaUsuarios, saidaListaEmprestimos, montarSaidaEmprestimo
  - internal/http/erros.go — codUsuarioInexistente no catálogo (404)

RISCOS:
  - O sensor `test` padrão roda sem banco (216 pulados); o off-by-one/ordem das novas consultas só é provado na execução com BIBLIOTECA_TEST_DB_OBRIGATORIO=1 acima. QA deve repetir com banco.
  - Riscos de produto do 02 permanecem (exposição em lote de nomes e empréstimos, página de usuários até ~6 MB, linha de empréstimo inconsistente → 500 da página), para registro pelo docs-update.

SINAIS:
  SECURITY: sim — internal/http/ e internal/storage/ (security_surface_paths); entrada por query string, enumeração em lote de dado pessoal
  SUPPLY_CHAIN: nao — nenhuma dependência nova; go.mod/go.sum intocados; mod_verify ok
  MIGRATION: nao — nenhuma migração; consultas só nas PKs
  PERFORMANCE: nao — keyset pela PK com LIMIT limite+1, uma consulta por página, sem JOIN nem N+1, multa em memória

PROXIMO: code-reviewer
MOTIVO: testes e sensores passam (inclusive com PostgreSQL real, 0 SKIP, e o fuzz de 30s); segue para revisão de código e depois security-review.
