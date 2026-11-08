DELIVERY_ID: DEL-013
TASK_CODE: DEL-013
AGENT: test-writer
STATUS: ok

ABORDAGEM:
  Testes escritos antes da implementação, stdlib testing em tabela, conforme o 02-architecture-guardrails. Contrato fixado pelos testes (o dev implementa contra ele):
  - storage: `const LimiteListagemLivros = 100`; `var ErrListagemInvalida` (prefixo "storage: ", mensagem distinta das outras sentinelas); `(*Repositorio).ListarLivros(ctx, apos int64, limite int) ([]domain.Livro, bool, error)`; `(*Repositorio).BuscarEmprestimo(ctx, id int64) (domain.Emprestimo, error)`. ListarLivros recusa antes do SQL apos < 0 e limite fora de 1..100; com erro devolve 0 itens e false; sem linhas devolve slice vazia NÃO nil; erro em rows.Err() precisa voltar como erro (com rows fechado); mensagem de erro sem o valor de apos nem SQL. BuscarEmprestimo: id <= 0 → ErrEmprestimoNaoEncontrado sem SQL; devolve aberto, renovado ou devolvido.
  - httpapi.Repositorio ganha BuscarLivro, ListarLivros, BuscarEmprestimo (asserção de interface em apoio_test.go já cobre *storage.Repositorio).
  - HTTP: GET /livros, GET /livros/{id}, GET /emprestimos/{id} atrás do auth; erros parametro_invalido (400, "parâmetro de consulta inválido") e emprestimo_nao_encontrado (404, "empréstimo não encontrado"); storage.ErrListagemInvalida em errosDeBug; classificar() continua mapeando storage.ErrEmprestimoNaoEncontrado para emprestimo_aberto_nao_encontrado. Resposta da listagem exatamente {"itens":[{id,titulo}],"proximo":número|null}; itens nunca null; proximo = id do último item entregue só quando temMais.
  - Os testes HTTP são caixa-preta (pacote httpapi_test, via ServeHTTP); o nome da função de parsing (lerParametrosListagem) não é exigido. O teste interno (erros_interno_test.go) usa os códigos por string, não pelos nomes de constante.
  - Gramática da query (tabela + fuzz com oráculo independente): url.ParseQuery sem erro (%zz, ';' → 400); só as chaves limite e apos, cada uma 1 vez; limite = strconv.ParseInt base 10 em 1..100, padrão 20 ("+5" codificado como %2B5 e "007" aceitos; "+10" cru vira espaço → 400); apos = gramática de parseID, padrão 0. "&" e "limite=5&" são aceitos (ParseQuery ignora pares vazios); "=5" é chave vazia → 400.
  - Rotas de detalhe ignoram a query string (inclusive "%zz" e ";").

NAO_FAZER:
  - Não mudar os testes para acomodar a implementação; se algum caso parecer errado, devolver ao architecture-guardrails.
  - Os demais itens do NAO_FAZER do 02-architecture-guardrails continuam valendo.

CONFIRMADO:
  - `go test ./...` na árvore (antes da implementação), falha pelo motivo certo, compilação dos testes por símbolos ausentes:
      internal/http/erros_interno_test.go:56:53: undefined: storage.ErrListagemInvalida
      internal/storage/listagem_test.go:64:55: undefined: LimiteListagemLivros
      internal/storage/listagem_test.go:75:32: f.repo.ListarLivros undefined (type *Repositorio has no field or method ListarLivros)
      internal/storage/listagem_test.go:245:23: f.repo.BuscarEmprestimo undefined ...
      FAIL internal/http [build failed]; FAIL internal/storage [build failed]; demais pacotes ok.
  - Implementação de referência numa cópia temporária fora da árvore (mktemp -d no scratchpad) + Postgres próprio (container biblioteca-pg-del013, 127.0.0.1:5443, mesma imagem fixada da DEL-012): `BIBLIOTECA_TEST_DATABASE_URL=... BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 ./...` → todos os pacotes ok; go vet ok.
  - Fuzz na cópia com a referência: `go test -run '^$' -fuzz=FuzzParametrosListagem -fuzztime=30s ./internal/http` → PASS, 166854 execs, 0 falhas.
  - Mutantes: 33 mortos, 0 sobreviventes (handler: teto >=, piso 0, sem checagem de repetido, ignora chave desconhecida, padrão 10, proximo+1, proximo sempre, itens nil, ignora erro do ParseQuery, apos aceita 0, sem mapeamento local do 404, classificar global, multa com ref errada, listagem sem timeout (morto por travar TestTimeoutPorOperacao até o -timeout), ErrListagemInvalida fora de errosDeBug, mensagem do catálogo, listagem sem auth, detalhe de empréstimo sem auth, id inválido indo ao repo; storage: sem +1, temMais >=, id >=, ORDER BY DESC, sem checagem de apos, teto >=, piso 0, slice nil, id 0 indo ao banco, filtro só abertos, sem corte em limite, teto 101, sem ORDER BY, ignora rows.Err). Na primeira passada sobreviveram "sem ORDER BY" (o heap saía em ordem de id) e "ignora rows.Err"; foram mortos com o UPDATE no helper acervo() e com TestListarLivrosErroDuranteLeitura. O primeiro "proximo sempre" morreu por erro de compilação e foi refeito como mutante válido.
  - Limpeza: cópia, script e container biblioteca-pg-del013 removidos por nome; sem prune nem go clean.
  - gofmt -l internal/ vazio.

ASSUNCOES:
  - LimiteListagemLivros e ErrListagemInvalida com esses nomes exatos, como no handoff de arquitetura.
  - Default do fake: BuscarLivro devolve {id, "Dom Casmurro"}, ListarLivros devolve nil,false, BuscarEmprestimo devolve buscarRet. Assim as rotas novas entram em rotasProtegidas() (auth 401 e timeout por operação) sem configuração extra.

ARQUIVOS:
  - internal/http/apoio_test.go — repoFake com BuscarLivro/ListarLivros/BuscarEmprestimo (campos errBuscarLivro, errListar, errBuscarEmpr, livroRet, listarRet, listarTemMais, buscarEmprRet e os recebidos); rotasProtegidas() com GET /livros, GET /livros/1, GET /emprestimos/1.
  - internal/http/handler_test.go — TestMuxNotFoundEMethodNotAllowed: casos "GET /livros/1 → 404" e "GET /livros → 405" trocados (agora são rotas válidas) por /livros/, /livros/1/, /emprestimos, /emprestimos/1/x (404) e PUT/DELETE /livros/1, POST /emprestimos/1, DELETE /livros (405); TestAuthAntesDeTudo com casos das rotas novas e checagem de que o repositório não é chamado.
  - internal/http/log_test.go — "405 do mux" passa a usar PUT /livros; linhas novas em TestLogChaves (rotas e códigos novos, ErrListagemInvalida com causa bug_interno).
  - internal/http/erros_interno_test.go — TestCatalogoListagemEDetalhe (catálogo novo e antigo, classificar sem mapeamento global).
  - internal/http/fuzz_test.go — oraculoListagem e FuzzParametrosListagem.
  - internal/http/listagem_test.go (novo) — parâmetros, sem eco do valor, contrato de proximo, mapa de erros, rotas antigas mantêm emprestimo_aberto_nao_encontrado, id de path, DTOs de detalhe (multa aberto/devolvido/renovado), query ignorada nos detalhes, log sem query.
  - internal/http/banco_listagem_test.go (novo) — ponta a ponta no banco: paginação com limites 1..6 e 100, página exata com proximo null, apos além do último, detalhes de livro e empréstimo aberto/devolvido, 404s.
  - internal/storage/sem_banco_test.go — sentinela nova, recusas antes do SQL, extremos aceitos, erro sem dados, TestLimiteListagemLivros, TestListarLivrosErroDuranteLeitura (fake pgx.Rows).
  - internal/storage/listagem_test.go (novo) — ListarLivros (tabela de páginas com buraco de id e heap fora de ordem), percorrer tudo para todo limite, vazio, recusas, BuscarEmprestimo.

RISCOS:
  - Os testes de banco pulam sem BIBLIOTECA_TEST_DATABASE_URL; o off-by-one do cursor e o ORDER BY só são pegos com banco. dev e QA devem rodar com Postgres e BIBLIOTECA_TEST_DB_OBRIGATORIO=1.
  - Estado do git mudou durante o trabalho, por ação do usuário fora da sessão: um commit "chore: pipeline-claude v1.1.0" na branch chore/pipeline-claude-v1.1.0 chegou a incluir 4 destes testes e os handoffs da DEL-013; depois foi refeito (ac590f0, só os arquivos do chore) e mesclado na main (PR #15, d870750). Estado final: main em d870750, alterações da DEL-013 não commitadas e íntegras. Antes do commit da delivery, o git-delivery deve criar a branch task/DEL-013 a partir dessa main.

SINAIS:
  SECURITY: sim — rotas autenticadas novas e entrada externa (query string) em internal/http/, consultas novas em internal/storage/; fuzz do parsing escrito.
  SUPPLY_CHAIN: nao — go.mod/go.sum intactos; só stdlib e pgx já presente.
  MIGRATION: nao — nenhum DDL.
  PERFORMANCE: sim — consulta de listagem nova; o teste de banco fixa LIMIT/ordem/cursor; plano avaliado no handoff de arquitetura.

PROXIMO: dev-implementation
MOTIVO: testes escritos e falhando pelo motivo certo (símbolos ausentes); referência validou que eles passam com uma implementação correta e matam 33/33 mutantes.
