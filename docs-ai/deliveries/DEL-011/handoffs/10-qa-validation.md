DELIVERY_ID: DEL-011
TASK_CODE: DEL-011
AGENT: qa-validation
STATUS: ok

RESULTADO: ok
MOTIVO_RESULTADO: este handoff substitui o 09-qa-validation, que deu `parcial` porque a camada de banco não tinha sido provada. A suíte com banco agora foi rodada pela SESSÃO PRINCIPAL, e não pelo QA, que não tem shell: Postgres 16 descartável, BIBLIOTECA_TEST_DB_OBRIGATORIO=1, exit 0, 1908 pass, 0 fail, 0 skip em 8 pacotes. O QA conferiu essa evidência no JSON bruto do go test com Grep (detalhes em GATES). Com isso, migração 0002, catálogo, CHECK, CAS de RegistrarRenovacao e concorrência ficam provados. O sensor `test` continua com skipped = 200 porque roda sem a variável de banco. Esses mesmos testes aparecem como PASS na execução com banco, e por isso o motivo do `parcial` deixou de valer.

GATES:
  - pipeline_sensors (todos), reexecutado agora pelo QA. ok: true.
      vet: ok, exit_code 0, 162 ms, saída vazia, skipped 0
      test: ok, exit_code 0, 194 ms, skipped 200. Saída (resumo, truncada pela ferramenta):
        "pacotes: 8 (8 ok, 0 com falha, 0 sem testes)
         testes: 1580 passaram, 0 falharam, 200 pulados"
        Pulados visíveis, todos com "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado": cmd/biblioteca-bairro (TestFechar*, TestMigrateAceitaParametrosDePoolNaDSN/*, TestMigrateUpCommitComTagInesperadaCitaOArquivo, TestMigrateStatusNomeArbitrarioDoBancoNaoQuebraASaida), internal/migrate (TestControleCatalogo, TestControleRejeitaLinhasInvalidas, TestControleAceitaLinhasValidas, TestControleUpEIdempotente) e internal/storage (TestMigracaoUpDownUp esquema_test.go:236, TestEsquemaRestricoes esquema_test.go:257, TestAbrirEmprestimoSemRenovacoes renovacao_test.go:40, TestRegistrarRenovacao renovacao_test.go:53, TestRegistrarRenovacaoRecusas/* renovacao_test.go:220, TestRegistrarRenovacaoConcorrente renovacao_test.go:297, TestRegistrarRenovacaoConcorrenteComDevolucao renovacao_test.go:328, TestRegistrarDevolucaoConcorrenteComRenovacao renovacao_test.go:359, TestLivroIdaEVolta, TestUsuarioIdaEVolta, TestEmprestimoIdaEVolta/*), seguidos de "… (resumo truncado)".
      build: ok, exit_code 0, 506 ms, saída vazia
      vuln: ok, exit_code 0, 4848 ms, "No vulnerabilities found."
      mod_verify: ok, exit_code 0, 270 ms, "all modules verified"
  - Suíte com banco, EXECUTADA PELA SESSÃO PRINCIPAL em 2026-11-06, não pelo QA:
      Ambiente: container descartável del011-pg, Postgres 16, POSTGRES_HOST_AUTH_METHOD=trust, 127.0.0.1:55432, removido com --rm ao final; go1.27.3.
      Comando: BIBLIOTECA_TEST_DATABASE_URL='postgres://postgres@127.0.0.1:55432/postgres?sslmode=disable' BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 -p 1 -json ./...
      Resultado relatado: exit=0, 1908 pass, 0 fail, 0 skip; 8 pacotes pass (cmd/biblioteca-bairro, internal/config, internal/domain, internal/http, internal/http/middleware, internal/migrate, internal/storage, migrations).
      Fontes: /tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/del011-db-evidencia.txt (resumo) e del011-db-test.json (JSON bruto), na mesma pasta.
      Conferência do QA no JSON bruto, com Grep:
        - `"Action":"(skip|fail)"`: 0 ocorrências.
        - `--- (SKIP|FAIL)`: 0 ocorrências.
        - `"Action":"pass","Package":"[^"]*","Test"`: 1908 ocorrências, batendo com o resumo.
        - Linha de pass de pacote (sem Test) para os 8 pacotes (linhas 556, 1032, 3840, 5540, 5768, 7128, 7620, 7664).
        - PASS individual dos testes de banco da DEL-011: TestBancoRenovacao (l. 3885), TestBancoRenovacaoConcorrente (l. 3889), TestMigracaoUpDownUp (l. 7133), TestEsquemaRestricoes (l. 7237), TestAbrirEmprestimoSemRenovacoes (l. 7241), TestRegistrarRenovacao (l. 7245), TestRegistrarRenovacaoRecusas e os 5 subtestes inexistente/devolvido_depois_da_leitura/copia_velha/contador_a_frente_do_gravado/inicio_divergente (l. 7251-7269), TestRegistrarRenovacaoConcorrente (l. 7273), TestRegistrarRenovacaoConcorrenteComDevolucao (l. 7277), TestRegistrarDevolucaoConcorrenteComRenovacao (l. 7281).
        - PASS dos testes de banco anteriores, que servem de regressão: TestControleCatalogo, TestControleRejeitaLinhasInvalidas, TestControleAceitaLinhasValidas, TestControleUpEIdempotente, TestControleDown, TestControleDownComLinhas, TestControleAdotadoDeOutroDono, TestLivroIdaEVolta, TestEmprestimoIdaEVolta, TestFecharEsperaOBackendCanceladoSair, TestMigrateAceitaParametrosDePoolNaDSN.
      A diferença de contagem (sensor 1580+200 = 1780; banco 1908) é de subtestes que só existem quando o teste-pai roda com banco. O pai pulado não cria filhos.
      Nota da sessão principal: uma primeira execução com container com senha (scram) teve 7 FAIL em internal/migrate TestControleAdotadoDeOutroDono (conexão como papel criado sem senha). É requisito de ambiente (trust), igual ao da DEL-008, e não regressão. Na execução com trust, TestControleAdotadoDeOutroDono passou (l. 6165).
  - Fuzz (alvos tocados: FuzzEmprestimoRenovar, novo, e FuzzReidratarEmprestimo, assinatura alterada). Saída de 30 s por alvo no 05-dev-implementation:
      go test -run='^$' -fuzz='^FuzzEmprestimoRenovar$' -fuzztime=30s ./internal/domain → "elapsed: 30s, execs: 2300295 ... PASS"
      go test -run='^$' -fuzz='^FuzzReidratarEmprestimo$' -fuzztime=30s ./internal/domain → "elapsed: 30s, execs: 796566 ... PASS"
    FuzzEmprestimoRenovar também passou (seeds) na execução da sessão principal.

COBERTURA_DO_ESCOPO:
  - Migração 0002 (coluna com default 0 e CHECK nomeado):
      sem banco: migrations/renovacao_test.go TestUp0002 e TestDown0002 (PASS no sensor e na execução com banco).
      com banco: internal/storage/esquema_test.go TestMigracaoUpDownUp (catálogo com emprestimo_renovacoes_check, coluna integer NOT NULL default '0') e TestEsquemaRestricoes (-1, 3, MaxInt32 → 23514; NULL → 23502; INSERT sem a coluna lê 0), com PASS na execução da sessão principal.
  - Domínio (Renovar, sentinelas, ReidratarEmprestimo com contador): internal/domain/renovar_test.go (TestConstantesRenovacao, TestErrosRenovacaoMensagens, TestErrosRenovacaoDistintos, TestNovoEmprestimoSemRenovacoes, TestReidratarEmprestimoRenovacoes, TestEmprestimoRenovarSucesso, TestEmprestimoRenovarAteOLimite, TestEmprestimoRenovarErros, TestEmprestimoRenovarMesmaFronteiraDaMulta, TestEmprestimoRenovarMultaUsaVencimentoNovo, TestEmprestimoRenovarIdaEVolta, FuzzEmprestimoRenovar) e reidratar_test.go (FuzzReidratarEmprestimo). PASS no sensor e na execução com banco.
  - Repositório (persistir renovação, ler contador, sentinelas sem SQL):
      sem banco: internal/storage/sem_banco_test.go (recusas pré-SQL e erro sem IDs/SQL).
      com banco: internal/storage/renovacao_test.go TestAbrirEmprestimoSemRenovacoes, TestRegistrarRenovacao, TestRegistrarRenovacaoRecusas (5 casos), TestRegistrarRenovacaoConcorrente, TestRegistrarRenovacaoConcorrenteComDevolucao, TestRegistrarDevolucaoConcorrenteComRenovacao; repositorio_test.go TestEmprestimoIdaEVolta (checarEmprestimo com Renovacoes). Todos com PASS na execução da sessão principal.
  - HTTP (POST /livros/{id}/renovacao, auth, relógio injetado, JSON D-15, 409 vencido/limite, 404 sem empréstimo aberto):
      sem banco: internal/http/renovacao_test.go (TestContratoSaidaRenovacao, TestContratoSaidaRenovacaoUsaRetornoDoRepositorio, TestRenovacaoRecusadaNaoGrava, TestRenovacaoMensagens, TestRenovacoesNasRespostasDeEmprestimo); handler_test.go TestMapaDeErros (casos de renovação), TestAuthAntesDeTudo, TestDecodificacaoDoCorpo, TestParametroID, 405 em GET/PUT; apoio_test.go "POST renovacao" em rotasProtegidas (401 e timeout); erros_interno_test.go TestCatalogoRenovacao; TestReadyzToleranciaDuranteRenovacao.
      com banco: internal/http/banco_renovacao_test.go TestBancoRenovacao e TestBancoRenovacaoConcorrente, com PASS na execução da sessão principal.
  - Testes em tabela com `testing` nas três camadas: presentes. Nenhuma biblioteca de asserção nova; go.mod/go.sum fora do diff.
  - Atualizar docs-ai/03-MAPA-RAPIDO-MODULOS.md: fica com o docs-update, pendente e não verificável neste gate.

SCOPE_CREEP (FORA_DE_ESCOPO):
  - Sem mudança desde o 09: nenhum arquivo em internal/migrate/, cmd/, go.mod, go.sum nem migrations/0001_*; sem histórico, GET de renovações, env de limite/prazo nem checagem por usuário; CalcularMulta/Multa sem hunk. Sem scope creep de código.
  - docs-ai/deliveries/DEL-010/report.md e docs-ai/deliveries/INDEX.md continuam modificados no working tree. São da DEL-010, anteriores a esta delivery. O git-delivery deve separar ou incluir conscientemente.

BORDAS_CONFERIDAS:
  - Fronteira de vencido no dia civil SP (23:59:59.999 permitido, 00:00 seguinte recusado), igual à da multa: coberta no domínio.
  - Vencido e limite ao mesmo tempo: o vencido ganha, coberto no domínio e no HTTP.
  - Recusa não muta e não grava: TestEmprestimoRenovarErros e TestRenovacaoRecusadaNaoGrava.
  - Corrida renovação×renovação, renovação×devolução e devolução×renovação: agora provadas com banco (TestRegistrarRenovacaoConcorrente*, TestRegistrarDevolucaoConcorrenteComRenovacao, TestBancoRenovacaoConcorrente, PASS).
  - CAS com cópia velha, contador à frente e início divergente: TestRegistrarRenovacaoRecusas/*, PASS com banco.
  - Lacuna sem impacto bloqueante: RegistrarRenovacao com contador > LimiteRenovacoes não é testável fora do pacote domain (mutante equivalente S12, 04); a cláusula fica como defesa.

CONFIRMADO:
  - Handoff anterior 09-qa-validation relido.
  - pipeline_sensors reexecutado nesta sessão, com a saída transcrita em GATES.
  - Resumo da sessão principal (del011-db-evidencia.txt) lido e JSON bruto (del011-db-test.json) conferido com Grep: 0 skip, 0 fail, 1908 pass, 8 pacotes pass, PASS individual dos testes de banco da DEL-011.
  - Cobertura, scope creep e bordas reaproveitados do 09 (diff inalterado desde então, segundo a sessão principal; o QA não refez pipeline_diff).

ASSUNCOES:
  - A execução com banco é da sessão principal. O QA não a reproduziu: conferiu os artefatos que ela gerou, com o comando e o ambiente que ela informou.
  - O working tree não mudou entre a execução da sessão principal e o 09 (sem nova edição de código).
  - O volume da tabela emprestimo é pequeno o bastante para o CHECK validar dentro do statement_timeout (assunção herdada de 02, 03 e 08, não medida).

ARQUIVOS:
  - nenhum (QA só lê).

RISCOS:
  - O sensor `test` do projeto continua sem banco (skipped = 200). Sem CI com Postgres, uma regressão futura na camada de banco só aparece em execução manual. Sugestão fora do escopo: sensor ou job de CI com BIBLIOTECA_TEST_DB_OBRIGATORIO=1.
  - TestControleAdotadoDeOutroDono exige Postgres com auth trust (falha com scram). É requisito de ambiente já conhecido da DEL-008 e vale registrar no README/runbook de testes.
  - Ordem de deploy: `migrate up` da 0002 antes do binário novo, senão dá 500 (42703) em toda rota de empréstimo. No rollback, reverter o binário antes do down, que perde o contador. Precisa ir para o report/runbook (docs-update).
  - Contrato da API: o campo `renovacoes` é novo em todas as respostas de empréstimo, além da rota nova e dos 409 `emprestimo_vencido` e `limite_renovacoes_atingido`. A mudança é aditiva, mas quebra cliente com decodificação estrita.
  - Working tree com mudanças da DEL-010 (report.md e INDEX.md) misturadas ao diff.

SINAIS:
  SECURITY: sim — já revisado no 07 (aprovado, dois achados BAIXO informativos); nada novo no QA.
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff; mod_verify "all modules verified"; vuln "No vulnerabilities found."
  MIGRATION: sim — 0002 aprovada no 08 e agora provada em banco (TestMigracaoUpDownUp e TestEsquemaRestricoes com PASS na execução da sessão principal).
  PERFORMANCE: nao — UPDATE e SELECT por PK, busca pelo índice parcial existente.

PROXIMO: docs-update
MOTIVO: sem achado bloqueante; release-ops não está em enabled_modules. RESULTADO ok, porque a camada de banco foi provada pela execução da sessão principal (0 skip, 0 fail) e conferida pelo QA no JSON. O docs-update deve registrar no report, seção QA, "Resultado: ok", citando que a prova com banco veio da execução da sessão principal (comando e ambiente acima) e que o sensor sem banco tem 200 pulados. Deve registrar também a D-17, o mapa de módulos, a ordem de deploy/rollback e o requisito de auth trust para os testes de internal/migrate.
