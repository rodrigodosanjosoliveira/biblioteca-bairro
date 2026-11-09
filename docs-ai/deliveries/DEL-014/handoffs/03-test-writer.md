DELIVERY_ID: DEL-014
TASK_CODE: DEL-014
AGENT: test-writer
STATUS: ok

CONFIRMADO:
  - Testes escritos antes da implementação, stdlib testing, em tabela. Nenhum teste existente de GET /livros foi editado (listagem_test.go, fuzz_test.go, banco_listagem_test.go, storage/listagem_test.go, storage/sem_banco_test.go intactos).
  - Falha esperada, com PostgreSQL real (container postgres:16-alpine com nome próprio, BIBLIOTECA_TEST_DB_OBRIGATORIO=1), `go test -count=1 ./...`:
      FAIL internal/storage [build failed]: r.ListarUsuarios undefined, r.ListarEmprestimos undefined, undefined: LimiteListagem
      FAIL internal/http: TestCatalogoUsuarioInexistente (código fora do catálogo), TestBancoListagemUsuarios, TestBancoListagemEmprestimos, TestAuthRotasProtegidas e TestTimeoutPorOperacao (só os subtestes das 3 rotas novas: 404/405 em vez de 401/503), TestMuxNotFoundEMethodNotAllowed (POST /emprestimos e DELETE /usuarios: 404, want 405), TestListagensNovasParametros, TestListarUsuariosContrato, TestListarEmprestimosContrato, TestListarEmprestimosInstanteUnico, TestListarEmprestimosItemInvalido, TestRotasNovasMapaDeErros, TestDetalheUsuarioParametroID, TestContratoSaidaDetalheUsuario, TestDetalheUsuarioIgnoraQueryString, TestRotasNovasAuthAntesDeTudo, FuzzParametrosListagemNovasRotas. Motivo em todos: rota ausente (status 404 ou 405, e 405 em /usuarios, que já tem POST).
      ok: cmd, config, domain, middleware, migrate, migrations. `go test -run 'Livros|Listagem' ./internal/http/`: só o fuzz novo falha; os testes de livros passam.
  - Implementação de referência numa cópia fora da árvore (scratchpad, mktemp -d), seguindo o handoff 02 (listarPorID genérico, LimiteListagem, LimiteListagemLivros = LimiteListagem, montarSaidaEmprestimo, usuario_inexistente mapeado em obterUsuario): `go vet ./...` e `go test ./...` com banco passam INTEIROS, inclusive todos os testes atuais de GET /livros sem edição.
  - Fuzz: `go test -run '^$' -fuzz=FuzzParametrosListagemNovasRotas -fuzztime=30s ./internal/http/` na cópia com a referência: PASS (479763 execs). O corpus gerado no GOCACHE só para esse alvo foi removido por nome.
  - Mutantes: 34 mortos, 0 sobreviventes. Storage: teto+1, apos < -1, limite < 0, SQL sem +1, escanear a linha extra, temMais falso, slice nil, ignorar rows.Err, sem ORDER BY (usuario e emprestimo), id >= $1 (usuario e emprestimo), pular linha inválida, usuário sem nome. HTTP: 404 vira 422, sem mapeamento local, agora por item, agora depois do repositório, pular item inválido, página parcial (break), proximo = primeiro id, proximo +1, proximo sem temMais, proximo sem len (panic com página vazia), itens nil (usuarios e emprestimos), ignorar parametro_invalido (usuarios e emprestimos), catálogo 422, catálogo com mensagem errada, id+1 no detalhe, sem timeout (usuarios e emprestimos), nome trocado no detalhe. 3 mutantes da primeira rodada não valiam (2 só davam erro de compilação, 1 não aplicou por causa do alinhamento do gofmt). Foram reescritos como variantes válidas e mortos na segunda rodada.
  - Limpeza: cópia temporária e script removidos; container del014-testwriter-pg removido com docker rm -f por nome; sem prune nem go clean.

ASSUNCOES:
  - Os testes de storage usam os nomes do handoff 02: LimiteListagem (exportada), ListarUsuarios e ListarEmprestimos com a assinatura ([]T, bool, error). O helper genérico não é referenciado, então o dev pode duplicar o laço.
  - TestListagensNovasLinhaExtra confere que o SQL recebe exatamente (apos, limite+1) como argumentos, nessa ordem, e contém o texto "ORDER BY id". Isso fixa o formato do SQL do handoff 02.
  - TestListarEmprestimosInstanteUnico exige que o relógio seja lido UMA vez por requisição em GET /emprestimos e ANTES de ListarEmprestimos (A6 do handoff 02). Usa um relógio que anda 10 dias a cada leitura.
  - Com temMais=true e página vazia, proximo é null nas rotas novas, como em GET /livros (temMais && len > 0).
  - A mensagem do 404 usuario_inexistente é "usuário não encontrado", a mesma do 422 usuario_nao_encontrado (handoff 02).
  - O erro de linha inválida em ListarEmprestimos tem de conter "empréstimo gravado inválido" e embrulhar o erro do domínio (errors.Is), como o lerEmprestimo atual já faz.

ARQUIVOS:
  - internal/http/apoio_test.go — repoFake ganha BuscarUsuario, ListarUsuarios e ListarEmprestimos (campos errBuscarUsuario, errListarUsuarios, errListarEmpr, usuarioRet, usuarioIDRecebido, listarUsuariosRet/TemMais, listarEmprRet/TemMais, aoListarEmpr; as listagens gravam em listarApos/listarLimite). rotasProtegidas ganha GET /usuarios, GET /usuarios/{id} e GET /emprestimos, o que estende os testes de auth e de timeout às três rotas.
  - internal/http/handler_test.go — o caso "listagem de emprestimos não existe" (GET /emprestimos → 404) dá lugar a /emprestimos/, /usuarios/ e /usuarios/1/ (404) e /usuarios/1/emprestimos (404, fora do escopo, U1). Entram também POST /emprestimos, DELETE /usuarios, PUT e DELETE /usuarios/1 (405).
  - internal/http/listagem_usuarios_emprestimos_test.go — novo: gramática da query nas duas listagens (inclui situacao/usuario_id/livro_id → 400), contrato de GET /usuarios e GET /emprestimos (DTO exato, proximo = id do último item, itens []), instante único, 500 sem página parcial (item zero, relógio zerado com aberto, reidratação inválida), mapa de erros (usuario_inexistente 404 só no detalhe; POST emprestimo continua 422 usuario_nao_encontrado), id do detalhe, DTO {id, nome}, detalhe ignora query, auth antes de tudo, log sem query/limite/apos/nome, FuzzParametrosListagemNovasRotas (mesmo oraculoListagem do fuzz de livros).
  - internal/http/erros_usuario_interno_test.go — novo (package httpapi): catálogo usuario_inexistente 404 e usuario_nao_encontrado 422; classificar(ErrUsuarioNaoEncontrado) continua usuario_nao_encontrado.
  - internal/http/banco_listagem_usuarios_emprestimos_test.go — novo, com banco: percorre GET /usuarios e GET /emprestimos com vários limites. Cada item de empréstimo tem de ser igual (DeepEqual) ao GET /emprestimos/{id} no mesmo instante. Testa também o detalhe 200/404 usuario_inexistente e uma linha corrompida (vencimento 2000-01-01): 500 na página que a contém, 200 quando ela é a linha extra (+1) e 200 com apos depois dela.
  - internal/storage/listagem_sem_banco_usuarios_emprestimos_test.go — novo, sem banco: LimiteListagem = 100 e LimiteListagemLivros = LimiteListagem; recusa antes do SQL; extremos aceitos; argumentos (apos, limite+1); linha extra não escaneada (o Scan dela falharia); erro de Scan e de rows.Err sem página parcial e com rows fechado; linha inválida → erro do domínio, ou ignorada se for a extra; reidratação completa dos campos; erros sem dados.
  - internal/storage/listagem_usuarios_emprestimos_test.go — novo, com banco: ListarUsuarios e ListarEmprestimos com buraco nos ids e o primeiro registro regravado (heap fora de ordem); percorrer tudo; linha inválida; tabelas vazias; recusas.

RISCOS:
  - Os testes de banco (storage e http) são pulados sem BIBLIOTECA_TEST_DATABASE_URL. Os mutantes "id >= $1", "proximo +1/primeiro" e "404 vira 422" foram mortos primeiro por eles. Os dois "404" também morrem nos testes com fake, mas "id >= $1" só morre com banco. O QA precisa rodar com BIBLIOTECA_TEST_DB_OBRIGATORIO=1.
  - TestLogRotasNovasSemQueryNemNome já passa hoje (as rotas dão 404). Ele protege contra regressão e não prova o comportamento novo sozinho.
  - Mexer em rotasProtegidas muda o número de subtestes de TestAuthRotasProtegidas e TestTimeoutPorOperacao. A estrutura desses testes ficou igual.

SINAIS:
  SECURITY: sim — internal/http/ e internal/storage/ são security_surface_paths; os testes cobrem auth, recusa de parâmetros, que a resposta não ecoa a entrada e que o log não leva nome nem query
  SUPPLY_CHAIN: nao — nenhuma dependência nova; go.mod/go.sum intocados
  MIGRATION: nao — nenhuma migração; os testes de banco só usam as tabelas existentes
  PERFORMANCE: nao — os testes fixam a consulta keyset (id > $1 ORDER BY id LIMIT limite+1) e a linha extra sem Scan; não há consulta por item

PROXIMO: dev-implementation
MOTIVO: os testes definem o comportamento e falham pelo motivo certo (rotas e métodos ausentes); uma implementação conforme o handoff 02 faz a suíte inteira passar, como mostrou a referência na cópia temporária.
