DELIVERY_ID: DEL-013
TASK_CODE: DEL-013
AGENT: qa-validation
STATUS: ok

RESULTADO: ok. Todos os itens de código do ESCOPO têm teste, e os testes passaram com e sem banco. Não há scope creep. A camada de banco foi provada pela execução da sessão principal, com 0 skip (detalhes abaixo). O sensor test, rodado sem banco, pula 207 testes de banco. Esses pulos não deixam lacuna, porque a mesma suíte rodou com banco obrigatório.

GATES:
  - pipeline_sensors (todos), executado por este subagente:
      vet: ok=true, exit 0, saída vazia
      test: ok=true, exit 0, "pacotes: 8 (8 ok, 0 com falha, 0 sem testes) / testes: 1830 passaram, 0 falharam, 207 pulados", skipped=207. O motivo de todos os pulos listados é "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado". Entre eles estão os testes de banco desta delivery: TestListarLivros (listagem_test.go:51), TestListarLivrosPercorrerTudo (:115), TestListarLivrosVazio (:158), TestListarLivrosRecusas (:171) e TestBuscarEmprestimo (:197). Também aparecem TestMigracaoUpDownUp, TestEsquemaRestricoes, TestControle* e TestRegistrarRenovacao*. A saída do sensor veio truncada ("resumo truncado").
      build: ok=true, exit 0
      vuln: ok=true, exit 0, "No vulnerabilities found."
      mod_verify: ok=true, exit 0, "all modules verified"
  - Suíte com banco, executada pela SESSÃO PRINCIPAL e não por este subagente. Ambiente: Postgres 16 descartável em modo trust, mesmo digest do CI, em 2026-11-08T00:38:39Z. Comando: BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 -p 1 -json ./... Resultado relatado: exit 0, 2211 pass, 0 fail, 0 skip, 8 pacotes. Conferi o JSON (/tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/qa013-test.json) com Grep:
      * O padrão "Action":"(fail|skip)" não teve nenhuma ocorrência.
      * Os 8 pacotes terminam com "Action":"pass": cmd/biblioteca-bairro, internal/config, internal/domain, internal/http, internal/http/middleware, internal/migrate, internal/storage e migrations.
      * Têm "Action":"pass" os testes TestBancoListagemLivros (l.3973), TestBancoDetalhes (l.3977), TestCatalogoListagemEDetalhe (l.3885), TestAuthAntesDeTudo (l.4705), TestRotasAntigasMantemEmprestimoAbertoNaoEncontrado (l.5689), TestLogListagemSemQuery (l.5877), FuzzParametrosListagem como seed corpus (l.6429), TestListarLivros (l.8273), TestListarLivrosPercorrerTudo (l.8325), TestListarLivrosVazio (l.8329), TestListarLivrosRecusas (l.8345), TestBuscarEmprestimo (l.8373), incluindo os subtestes devolvido, aberto, renovado, outro_livro_e_usuário, inexistente_1003 e inexistente_9223372036854775807, e TestListarLivrosErroDuranteLeitura (l.8657).
  - Fuzz (alvo novo FuzzParametrosListagem, tocado pela delivery): a evidência está no 04-dev-implementation. O comando foi `go test -run '^$' -fuzz=FuzzParametrosListagem -fuzztime=30s ./internal/http` e a saída foi "elapsed: 30s, execs: 262417, new interesting: 13 (total: 84) PASS". Não houve crasher. O alvo pré-existente FuzzParseID não mudou.

COBERTURA_DO_ESCOPO:
  - GET /livros paginado e autenticado:
    * Parsing da query: internal/http/listagem_test.go TestListarLivrosParametros, uma tabela com 54 casos (padrões, limites 0/1/100/101, overflow int64/int32, sinal, zeros, espaço, chave desconhecida/repetida/vazia/maiúscula, cache-buster, %zz, ';').
    * Mensagem sem eco da entrada: TestListarLivrosErroNaoEcoaEntrada.
    * Contrato de itens/proximo: TestListarLivrosContrato.
    * Mapa de erros: TestListagemEDetalheMapaDeErros.
    * Paginação ponta a ponta no banco: banco_listagem_test.go TestBancoListagemLivros, que percorre as páginas com limites 1..6 e 100, cobre página exata com proximo null, apos além do último ([] e null), o caso padrão sem query e parâmetro inválido.
    * Fuzz: fuzz_test.go FuzzParametrosListagem, com oráculo independente.
    * Auth: handler_test.go TestAuthAntesDeTudo (401 e repositório não chamado) e rotasProtegidas() em apoio_test.go.
  - GET /livros/{id}: TestDetalheParametroID, TestContratoSaidaDetalheLivro, TestDetalheIgnoraQueryString e TestListagemEDetalheMapaDeErros. No banco, TestBancoDetalhes, que inclui 404 livro_nao_encontrado em /livros/999999. Também TestAuthAntesDeTudo e TestMuxNotFoundEMethodNotAllowed (405 em PUT/DELETE /livros/1 e 404 em /livros/1/).
  - GET /emprestimos/{id}, aberto ou devolvido: TestContratoSaidaDetalheEmprestimo cobre a multa de empréstimo aberto, devolvido e renovado. TestBancoDetalhes cobre aberto e devolvido e o 404 emprestimo_nao_encontrado. TestRotasAntigasMantemEmprestimoAbertoNaoEncontrado garante que as rotas antigas não regrediram. TestAuthAntesDeTudo cobre /emprestimos/0 sem token.
  - Consultas novas no repositório:
    * ListarLivros: storage/listagem_test.go TestListarLivros (buraco de id e heap fora de ordem), TestListarLivrosPercorrerTudo, TestListarLivrosVazio (slice não nil) e TestListarLivrosRecusas. Em storage/sem_banco_test.go: recusas antes do SQL, TestLimiteListagemLivros e TestListarLivrosErroDuranteLeitura (rows.Err).
    * BuscarEmprestimo: TestBuscarEmprestimo, que inclui id<=0 sem SQL.
    * Interface Repositorio: a asserção de interface em apoio_test.go e o repoFake com os três métodos.
  - Códigos de erro novos (D-15): erros_interno_test.go TestCatalogoListagemEDetalhe cobre parametro_invalido 400, emprestimo_nao_encontrado 404, os códigos antigos intactos, classificar() sem mapeamento global e ErrListagemInvalida como bug_interno. Também log_test.go TestLogChaves.
  - Log sem query string: TestLogListagemSemQuery e TestListarLivrosErroNaoEcoaEntrada (log sem o valor).
  - D-19 em DECISOES.md e mapa de módulos: são do docs-update, que é a próxima etapa, e não têm teste por serem documentação. O diff ainda não toca docs-ai/DECISOES.md nem docs-ai/03-MAPA-RAPIDO-MODULOS.md, como esperado.
  - Marcação merged da DEL-012: pipeline_diff mostra docs-ai/deliveries/DEL-012/report.md com "status: em revisão" trocado por "status: merged", e INDEX.md com a linha DEL-012 trocada de "em revisão" para "merged", mais a linha DEL-013 em "draft". Não tem teste por ser documentação. O conteúdo confere.

CONFIRMADO:
  - pipeline_diff --name_only (base origin/main, merge-base d870750) lista 21 arquivos: código e testes só em internal/http e internal/storage; handoffs e report da DEL-013; DEL-012/report.md; INDEX.md. go.mod, go.sum, migrations/, internal/domain/, middleware/, Makefile, .pipeline-claude.lock e .claude/ estão fora do diff.
  - Scope creep, verificado contra o FORA_DE_ESCOPO:
    * handler.go:505-533 lerParametrosListagem só aceita as chaves limite e apos. Não há filtro, ordenação, offset nem total.
    * Não há listagem de empréstimos. GET /emprestimos dá 404, fixado em TestMuxNotFoundEMethodNotAllowed.
    * Não há GET /usuarios, campo "emprestado", DDL nem mudança no domínio.
    * O B-S3 e as sugestões do code-reviewer não foram implementados (estão fora do escopo).
  - Mudanças em testes pré-existentes (handler_test.go): os casos "GET /livros/1 → 404" e "GET /livros → 405" deixaram de valer porque as rotas agora existem. Foram substituídos por casos equivalentes em rotas que continuam inválidas. A suíte não ficou mais frouxa.
  - Bordas verificadas além da tabela:
    * Chave vazia ("=5") cai no default e dá 400.
    * apos=9223372036854775807 é aceito e o banco devolve página vazia.
    * limite em percent-encoding é aceito.
    * ";" dá 400 porque o erro de ParseQuery é tratado.
    * id inexistente no limite de int64 em BuscarEmprestimo dá 404 (subteste inexistente_9223372036854775807).

ASSUNCOES:
  - A execução com banco é da sessão principal. Não a repeti, e conferi só o JSON que ela gerou. O 04-dev-implementation registra uma execução independente equivalente (2211 pass, 0 fail, 0 skip).
  - A evidência de fuzz de 30s é a do 04-dev-implementation, que não repeti. No JSON da sessão principal, FuzzParametrosListagem rodou só como seed corpus.
  - release-ops não está em enabled_modules (o triage registra data-model e migration-reviewer).

ARQUIVOS:
  - nenhum (QA só lê)

RISCOS:
  - O sensor test, rodado sem banco, não cobre o off-by-one do cursor nem o ORDER BY. O CI precisa continuar rodando com Postgres e BIBLIOTECA_TEST_DB_OBRIGATORIO=1.
  - Riscos já aceitos (02 e 06):
    * Rejeição de cache-buster (?_=), que é intencional e coberta por teste.
    * Página de até cerca de 6 MB no pior caso.
    * Enumeração por quem tem o token (BAIXO).
    * proximo acima de 2^53 perde precisão em JS.
    Esses riscos precisam ficar registrados na D-19.
  - A D-19 e o mapa de módulos ainda não foram escritos. Se o docs-update os omitir, o contrato público novo fica sem decisão registrada.

SINAIS:
  SECURITY: sim — já revisado pelo security-review (aprovado, dois achados BAIXO aceitos)
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff; mod_verify ok
  MIGRATION: nao — nenhum DDL
  PERFORMANCE: sim — keyset pela PK com LIMIT de até 101 linhas, conferido no 02 e no 05; percurso completo coberto em banco

PROXIMO: docs-update
MOTIVO: QA ok sem achado bloqueante. release-ops não está habilitado. Falta registrar a D-19 (contrato de paginação, códigos novos, rejeição de parâmetro desconhecido/repetido e riscos aceitos), atualizar a consequência da D-15 e atualizar o mapa de módulos.
