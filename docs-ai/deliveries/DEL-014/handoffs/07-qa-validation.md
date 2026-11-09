DELIVERY_ID: DEL-014
TASK_CODE: DEL-014
AGENT: qa-validation
STATUS: ok

RESULTADO: ok. Cada item do ESCOPO tem teste. Nada do FORA_DE_ESCOPO foi implementado. Os sensores passam. A camada de banco foi provada com PostgreSQL real na execução da sessão principal: 0 skip, 0 fail. Os 216 pulados do sensor desta etapa são todos do mesmo motivo: o sensor roda sem BIBLIOTECA_TEST_DATABASE_URL. Ver GATES e a justificativa em ASSUNCOES.

GATES:
  - pipeline_sensors (todos), rodado por este agente em 2026-11-08: ok=true
      vet: ok, exit 0, saída vazia
      test: ok, exit 0, saída "pacotes: 8 (8 ok, 0 com falha, 0 sem testes) / testes: 2094 passaram, 0 falharam, 216 pulados" (skipped=216). Todos os pulados que aparecem no resumo, que veio truncado, têm o motivo "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado". Exemplos: TestMigracaoUpDownUp e TestEsquemaRestricoes (internal/storage), TestControleCatalogo e TestNovoPrazoNaoPositivoOuSubMilissegundoCaiNoPadrao/* (internal/migrate), TestMigrateAceitaParametrosDePoolNaDSN/* (cmd/biblioteca-bairro). São da camada de banco.
      build: ok, exit 0, saída vazia
      vuln: ok, exit 0, saída "No vulnerabilities found."
      mod_verify: ok, exit 0, saída "all modules verified"
  - Suíte com banco, EXECUTADA PELA SESSÃO PRINCIPAL e não por este agente, em 2026-11-08T23:55:40Z. Ambiente: Postgres 16 descartável em modo trust, com o digest do CI, BIBLIOTECA_TEST_DB_OBRIGATORIO=1. Comando: `go test -count=1 -p 1 -json ./...`, rodado depois de go build e go vet limpos. Resultado informado: exit 0, 2553 pass, 0 fail, 0 skip, 8 pacotes. Conferi o JSON (/tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/qa014-test.json) com Grep:
      - `"Action":"(skip|fail)"`: 0 ocorrências.
      - "Action":"pass" no nível de pacote para os 8 pacotes: cmd/biblioteca-bairro, internal/config, internal/domain, internal/http, internal/http/middleware, internal/migrate, internal/storage e migrations.
      - Testes de banco desta delivery, todos com "Action":"pass":
          internal/storage: TestListarUsuarios, TestListarUsuariosPercorrerTudo, TestListarEmprestimos, TestListarEmprestimosPercorrerTudo, TestListarEmprestimosLinhaInvalida, TestListagensNovasVazio, TestListagensNovasRecusasComBanco
          internal/http: TestBancoListagemUsuarios (subtestes percorrer_com_limite_1..6 e _100, página_exata_tem_proximo_null, apos_além_do_último, parametro_invalido, detalhe, detalhe_inexistente); TestBancoListagemEmprestimos (percorrer_com_limite_1..5 e _100, gravado_inválido)
          Regressão de livros: TestBancoListagemLivros, pass.
  - Fuzz (alvo novo FuzzParametrosListagemNovasRotas), evidência do handoff 04 (dev-implementation): `go test -run '^$' -fuzz=FuzzParametrosListagemNovasRotas -fuzztime=30s ./internal/http/`, resultado "elapsed: 30s, execs: 529349 ... PASS / ok ... internal/http 30.170s". Nenhum outro alvo de fuzz foi tocado, porque fuzz_test.go não está no diff.

COBERTURA_DO_ESCOPO:
  - GET /usuarios paginado com o contrato da D-19 (limite 1..100, padrão 20, sem clamp; apos; ordem por id; {itens, proximo}; chave desconhecida, repetida, escape inválido ou ; → 400):
      internal/http/listagem_usuarios_emprestimos_test.go: TestListagensNovasParametros (:33, inclui situacao/usuario_id/livro_id → 400), TestListarUsuariosContrato (:176), FuzzParametrosListagemNovasRotas (:636)
      Com banco: TestBancoListagemUsuarios em internal/http/banco_listagem_usuarios_emprestimos_test.go:49
  - GET /usuarios/{id} (DTO {id, nome}, ignora a query, 404 usuario_inexistente):
      TestDetalheUsuarioParametroID (:500), TestContratoSaidaDetalheUsuario (:548), TestDetalheUsuarioIgnoraQueryString (:567), TestRotasNovasMapaDeErros (:446)
      internal/http/erros_usuario_interno_test.go: TestCatalogoUsuarioInexistente (:14), que também prova o 422 preservado em classificar
      Com banco: subtestes detalhe e detalhe_inexistente de TestBancoListagemUsuarios
  - GET /emprestimos (keyset pela PK, mesmo DTO do detalhe, instante único, 500 sem página parcial):
      TestListarEmprestimosContrato (:252), TestListarEmprestimosInstanteUnico (:335), TestListarEmprestimosItemInvalido (:398)
      Com banco: TestBancoListagemEmprestimos (:114), em que cada item é DeepEqual ao GET /emprestimos/{id} e o subteste gravado_inválido cobre a linha corrompida na página, como linha extra e depois do apos
  - Auth, timeout e log nas três rotas:
      rotasProtegidas em apoio_test.go estende TestAuthRotasProtegidas e TestTimeoutPorOperacao; também TestRotasNovasAuthAntesDeTudo (:580) e TestLogRotasNovasSemQueryNemNome (:595)
      Código conferido: rotas atrás de auth(...) em handler.go:146-148; WithTimeout em :387, :411 e :437; a.agora() lido uma vez em :436, antes do timeout e do repositório
  - Storage: ListarUsuarios, ListarEmprestimos, BuscarUsuario na interface, revalidação antes do SQL e ErrListagemInvalida:
      internal/storage/listagem_sem_banco_usuarios_emprestimos_test.go: TestLimiteListagem (:68), TestListagensNovasRecusaAntesDoSQL (:79), TestListagensNovasLimitesAceitos (:113), TestListagensNovasLinhaExtra (:219, args (apos, limite+1) e "ORDER BY id"), TestListagensNovasErroDuranteLeitura (:275), TestListarEmprestimosLinhaInvalidaSemBanco (:314), TestListarEmprestimosReidrataCampos (:380), TestListarUsuariosCamposSemBanco (:406), TestListagensNovasErroSemDados (:419)
      Com banco, em internal/storage/listagem_usuarios_emprestimos_test.go: :51, :105, :211, :262, :297, :340, :360
      Código conferido em repositorio.go: LimiteListagem :44, LimiteListagemLivros = LimiteListagem :48, listarPorID :173-174 validando antes do SQL, SQL keyset em :131, :146 e :163; grep por OFFSET e COUNT sem ocorrência
      ListarLivros refatorado sem mudança observável: os testes de livros não estão no diff e passam com e sem banco
  - Marcação merged da DEL-013 (working tree), conferida no pipeline_diff:
      docs-ai/deliveries/DEL-013/report.md: só `status: em revisão` → `status: merged`
      docs-ai/deliveries/INDEX.md: linha da DEL-013 de "em revisão" para "merged", mais a linha nova da DEL-014 (draft)
      Nada além disso nesses dois arquivos. É item de documentação, sem teste aplicável.

SCOPE_CREEP:
  - Nenhum. O diff tem 19 arquivos, todos em internal/http, internal/storage e docs-ai/deliveries. Não toca migrations/, go.mod, go.sum, Dockerfile nem README.
  - Filtros (U1) não implementados: situacao, usuario_id e livro_id dão 400 (TestListagensNovasParametros); /usuarios/1/emprestimos dá 404 (handler_test.go, caso "subrota de usuario").
  - As duas sugestões cosméticas do code-reviewer (comentários em repositorio.go:127 e handler.go:613-615) ficaram de fora, como combinado. O comentário da :127 ainda cita LimiteListagemLivros, que tem o mesmo valor.

BORDAS_CONFERIDAS:
  - Página exata com proximo null: subteste página_exata_tem_proximo_null.
  - apos além do último dá página vazia: subteste apos_além_do_último.
  - Tabelas vazias: TestListagensNovasVazio.
  - Buraco nos ids e heap fora de ordem: TestListarUsuarios e TestListarEmprestimos.
  - Linha extra corrompida não derruba a página: gravado_inválido e TestListagensNovasLinhaExtra.
  - temMais com página vazia dá proximo null; itens é sempre array: TestListar*Contrato.
  - Métodos não permitidos nas rotas novas dão 405 e barras finais dão 404: handler_test.go:722-729.
  - Nenhuma borda relevante sem teste encontrada.

CONFIRMADO:
  - As linhas de GATES e COBERTURA_DO_ESCOPO acima, com os caminhos de arquivo e as saídas reais.
  - handler_test.go: o único caso existente alterado é o de :722, a exceção prevista no handoff 02. Os demais são linhas novas.

ASSUNCOES:
  - RESULTADO ok, e não parcial, apesar de skipped=216 no sensor desta etapa. A regra pede parcial quando a camada de banco não foi provada. Aqui ela foi, pela execução da sessão principal com BIBLIOTECA_TEST_DB_OBRIGATORIO=1, cujo JSON eu conferi (0 skip, 0 fail, e os testes de banco desta delivery com pass). Não repeti essa execução: ela é da sessão principal, não deste agente. Se o critério exigir execução própria do QA com banco, o resultado passa a ser parcial pelo motivo "sensor test sem BIBLIOTECA_TEST_DATABASE_URL".
  - Não li o JSON inteiro, porque Grep não soma linhas de forma confiável. Os totais 2553/0/0 são os informados pela sessão principal. Por Grep confirmei 0 skip, 0 fail e os 8 pacotes com pass.
  - O fuzz é evidência do handoff 04 e não foi repetido aqui.

ARQUIVOS:
  - nenhum alterado (subagente só leitura)

RISCOS:
  - O sensor `test` padrão roda sem banco (216 pulados). Uma regressão de off-by-one ou de ordem no keyset (por exemplo id >= $1) só é pega no CI com BIBLIOTECA_TEST_DB_OBRIGATORIO=1.
  - Riscos de produto vindos dos handoffs 02 e 06, que vão para o docs-update:
      - MEDIO aceito (U2): exposição em lote de nomes e do histórico de empréstimos com o token único, sem rate limit. Registrar em D-20 ou na extensão da D-19 e na seção Security do report.
      - BAIXO: página de usuários de até cerca de 6 MB.
      - BAIXO: uma linha de empréstimo inconsistente gera 500 permanente na página que a contém.
  - Código novo usuario_inexistente (404) no catálogo: a decisão precisa registrá-lo.

SINAIS:
  SECURITY: sim — já revisado no handoff 06, sem achado bloqueante; o MEDIO aceito (U2) precisa ser registrado pelo docs-update
  SUPPLY_CHAIN: nao — go.mod e go.sum fora do diff; mod_verify ok; govulncheck "No vulnerabilities found."
  MIGRATION: nao — nenhuma migração no diff
  PERFORMANCE: nao — keyset pela PK com LIMIT limite+1, uma consulta por página, sem OFFSET, COUNT ou JOIN

PROXIMO: docs-update
MOTIVO: sem achado bloqueante e release-ops fora de enabled_modules (habilitados: data-model e migration-reviewer). O docs-update registra o código usuario_inexistente, o risco MEDIO (U2) em decisão nova ou na extensão da D-19 e as seções QA e Security do report da DEL-014.
