DELIVERY_ID: DEL-011
TASK_CODE: DEL-011
AGENT: qa-validation
STATUS: ok

RESULTADO: parcial
MOTIVO_PARCIAL: o sensor `test` pulou 200 testes (skipped = 200), entre eles os da camada de banco desta delivery (migração 0002, catálogo/CHECK, CAS de RegistrarRenovacao, fluxo HTTP com banco). O motivo de todos é "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado". O QA não tem shell e não conseguiu rodar a suíte com banco, então a camada de banco NÃO foi provada por este QA. A única evidência com banco é a execução manual registrada no handoff 05-dev-implementation, que o QA cita aqui mas não reproduziu e não assume como prova própria.

LIMITACAO_DO_QA (explícita):
  - Code-reviewer (06), migration-reviewer (08) e, por consequência, o security-review (07) pediram que o QA rodasse `go test -count=1 -p 1 ./...` com BIBLIOTECA_TEST_DATABASE_URL e BIBLIOTECA_TEST_DB_OBRIGATORIO=1. Este subagente não tem Bash. A única ferramenta de execução que ele tem é `pipeline_sensors`, e o sensor `test` roda sem a variável de banco. O pedido NÃO foi cumprido.
  - Evidência com banco disponível, que é de terceiro e não do QA: no 05-dev-implementation, o dev subiu um Postgres 16 descartável (container del011-dev-pg, 127.0.0.1:55411) e rodou `go test -count=1 -p 1 ./...` com BIBLIOTECA_TEST_DB_OBRIGATORIO=1. Resultado relatado: 8 pacotes ok, `grep -c -- "--- SKIP"` = 0, PASS em TestMigracaoUpDownUp, TestEsquemaRestricoes, TestAbrirEmprestimoSemRenovacoes, TestRegistrarRenovacao, TestRegistrarRenovacaoRecusas, TestRegistrarRenovacaoConcorrente, TestRegistrarRenovacaoConcorrenteComDevolucao, TestRegistrarDevolucaoConcorrenteComRenovacao, TestBancoRenovacao e TestBancoRenovacaoConcorrente; com -race em domain/http/storage, ok. O 04-test-writer relata o mesmo, mas numa implementação de referência fora da árvore.
  - Para fechar como `ok`, alguém com shell (humano ou CI com Postgres) precisa rodar `BIBLIOTECA_TEST_DATABASE_URL=... BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 -p 1 ./...` e confirmar 0 SKIP.

GATES:
  - pipeline_sensors (todos), executado agora pelo QA. ok: true.
      vet: ok, exit_code 0, 174 ms, saída vazia, skipped 0
      test: ok, exit_code 0, 192 ms, skipped 200. Saída (resumo, truncada pela ferramenta):
        "pacotes: 8 (8 ok, 0 com falha, 0 sem testes)
         testes: 1580 passaram, 0 falharam, 200 pulados"
        Pulados visíveis na saída, todos com o motivo "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado":
          internal/storage: TestMigracaoUpDownUp (esquema_test.go:236), TestEsquemaRestricoes (esquema_test.go:257), TestAbrirEmprestimoSemRenovacoes (renovacao_test.go:40), TestRegistrarRenovacao (renovacao_test.go:53)
          internal/migrate: TestControleCatalogo, TestControleRejeitaLinhasInvalidas, TestControleAceitaLinhasValidas, TestControleUpEIdempotente, TestControleDown, TestControleDownComLinhas, TestNovoPrazoNaoPositivoOuSubMilissegundoCaiNoPadrao/* (lock e statement), TestNovoPrazoDeUmMilissegundoOuMaisNaoDesliga/*
          cmd/biblioteca-bairro: TestFecharEsperaOBackendCanceladoSair, TestFecharRespeitaOPrazoSeALimpezaTravar, TestMigrateAceitaParametrosDePoolNaDSN/* (8 subtestes)
        A saída veio com "… (resumo truncado)". Os demais pulados (entre eles, pelo 04/05, TestRegistrarRenovacaoRecusas, TestRegistrarRenovacaoConcorrente*, TestRegistrarDevolucaoConcorrenteComRenovacao, TestBancoRenovacao, TestBancoRenovacaoConcorrente) não aparecem na parte visível. Pelo padrão, têm o mesmo motivo, mas o QA não os viu listados.
      build: ok, exit_code 0, 639 ms, saída vazia
      vuln: ok, exit_code 0, 4819 ms, "No vulnerabilities found."
      mod_verify: ok, exit_code 0, 270 ms, "all modules verified"
  - Fuzz (alvos tocados: FuzzEmprestimoRenovar, novo, e FuzzReidratarEmprestimo, assinatura alterada). O QA não executou. A saída de 30 s por alvo está no 05-dev-implementation:
      go test -run='^$' -fuzz='^FuzzEmprestimoRenovar$' -fuzztime=30s ./internal/domain → "elapsed: 30s, execs: 2300295 ... PASS"
      go test -run='^$' -fuzz='^FuzzReidratarEmprestimo$' -fuzztime=30s ./internal/domain → "elapsed: 30s, execs: 796566 ... PASS"
    Requisito do gate atendido, porque a saída de fuzz consta no handoff do dev.

COBERTURA_DO_ESCOPO:
  - Migração 0002 (coluna com default 0 e CHECK nomeado):
      sem banco, rodou no sensor: migrations/renovacao_test.go TestUp0002 e TestDown0002 (texto do ALTER, ordem DROP CONSTRAINT antes de DROP COLUMN, sem IF EXISTS/CASCADE); migrations/controle_test.go (sem BEGIN/COMMIT/concurrently).
      com banco, PULADO no sensor: internal/storage/esquema_test.go TestMigracaoUpDownUp (catálogo exato com emprestimo_renovacoes_check e coluna integer NOT NULL default '0') e TestEsquemaRestricoes (-1, 3 e MaxInt32 → 23514; NULL → 23502; INSERT sem a coluna lê 0).
      Arquivos conferidos com Read: migrations/0002_renovacao_emprestimo.up.sql (um ALTER: ADD COLUMN renovacoes integer NOT NULL DEFAULT 0, ADD CONSTRAINT emprestimo_renovacoes_check CHECK (renovacoes >= 0 AND renovacoes <= 2)) e .down.sql (DROP CONSTRAINT, depois DROP COLUMN).
  - Domínio (Renovar, sentinelas, ReidratarEmprestimo com contador): internal/domain/renovar_test.go, todos rodaram no sensor: TestConstantesRenovacao, TestErrosRenovacaoMensagens, TestErrosRenovacaoDistintos, TestNovoEmprestimoSemRenovacoes, TestReidratarEmprestimoRenovacoes (-1, 0..3, MinInt, MaxInt, validação por último), TestEmprestimoRenovarSucesso (base no vencimento atual, 23:59:59.999 SP, fusos, virada de ano, bissexto, horário de verão), TestEmprestimoRenovarAteOLimite (3ª e 4ª tentativas), TestEmprestimoRenovarErros (Emprestimo{}, devolvido, ref zero, vencido, vencido antes do limite, não mutação), TestEmprestimoRenovarMesmaFronteiraDaMulta, TestEmprestimoRenovarMultaUsaVencimentoNovo, TestEmprestimoRenovarIdaEVolta e FuzzEmprestimoRenovar (seeds). Também reidratar_test.go (FuzzReidratarEmprestimo com renov) e emprestimo_test.go. Implementação conferida no diff: internal/domain/emprestimo.go Renovar e ReidratarEmprestimo, sem relógio e sem I/O.
  - Repositório (persistir renovação, ler contador, sentinelas sem SQL):
      sem banco, rodou: internal/storage/sem_banco_test.go (recusas pré-SQL de RegistrarRenovacao com Emprestimo{}, sem ID, devolvido e contador 0; erro do banco sem IDs nem SQL).
      com banco, PULADO no sensor: internal/storage/renovacao_test.go TestAbrirEmprestimoSemRenovacoes, TestRegistrarRenovacao, TestRegistrarRenovacaoRecusas, TestRegistrarRenovacaoConcorrente, TestRegistrarRenovacaoConcorrenteComDevolucao e TestRegistrarDevolucaoConcorrenteComRenovacao; repositorio_test.go (checarEmprestimo com Renovacoes).
      Implementação conferida no diff: internal/storage/repositorio.go RegistrarRenovacao com UPDATE parametrizado ($1..$5) e diagnóstico por PK; lerEmprestimo com 7 colunas.
  - HTTP (POST /livros/{id}/renovacao, auth, relógio injetado, JSON D-15, 409 para vencido e limite, 404 sem empréstimo aberto):
      sem banco, rodou: internal/http/renovacao_test.go (TestContratoSaidaRenovacao, TestContratoSaidaRenovacaoUsaRetornoDoRepositorio, TestRenovacaoRecusadaNaoGrava, TestRenovacaoMensagens, TestRenovacoesNasRespostasDeEmprestimo); handler_test.go TestMapaDeErros (casos de renovação: 400 corpo, 404, 409 vencido/limite/já devolvido/alterado, 503, 500), TestAuthAntesDeTudo, TestDecodificacaoDoCorpo, TestParametroID, "subrota desconhecida" agora em /livros/1/inexistente (handler_test.go:713) e 405 em GET/PUT /renovacao; apoio_test.go:344 "POST renovacao" em rotasProtegidas (401 e timeout); erros_interno_test.go TestCatalogoRenovacao (erros_interno_test.go:79).
      com banco, PULADO: internal/http/banco_renovacao_test.go TestBancoRenovacao e TestBancoRenovacaoConcorrente.
  - Testes em tabela com `testing` nas três camadas: presentes (Grep `^func (Test|Fuzz)` nos arquivos novos). Nenhuma biblioteca de asserção nova, e go.mod/go.sum fora do diff.
  - Atualizar docs-ai/03-MAPA-RAPIDO-MODULOS.md: item atribuído ao docs-update, ainda pendente e não verificável neste gate.

SCOPE_CREEP (FORA_DE_ESCOPO):
  - pipeline_diff name_only (base origin/main, merge-base 1ad40d3): nenhum arquivo em internal/migrate/, cmd/, go.mod, go.sum nem migrations/0001_*. Não há tabela ou coluna de histórico, endpoint GET de renovações, env de limite ou prazo, nem checagem por usuário.
  - CalcularMulta e Emprestimo.Multa: sem hunk no diff de internal/domain/emprestimo.go (só constantes, sentinelas, campo, ReidratarEmprestimo, Renovar e getter). multa.go não aparece no diff.
  - Sem scope creep de código. Fora do escopo da DEL-011 aparecem no diff docs-ai/deliveries/DEL-010/report.md e docs-ai/deliveries/INDEX.md, que são mudanças da DEL-010 não commitadas e anteriores a esta delivery (já registradas no triage). Fica para o git-delivery separar ou incluir conscientemente.

BORDAS_CONFERIDAS:
  - Fronteira de vencido no dia civil SP (23:59:59.999 permitido, 00:00 do dia seguinte recusado), igual à da multa: coberta em renovar_test.go.
  - Vencido e limite ao mesmo tempo: o vencido ganha, coberto no domínio e no HTTP ("vencido ganha do limite").
  - Recusa não muta e não chama RegistrarRenovacao: coberto (TestEmprestimoRenovarErros, TestRenovacaoRecusadaNaoGrava).
  - Corrida renovação×renovação e renovação×devolução: coberta só por testes de banco, pulados no sensor.
  - Lacuna sem impacto bloqueante: `RegistrarRenovacao` com contador > LimiteRenovacoes não é testável fora do pacote domain (mutante equivalente S12, 04), e a cláusula fica como defesa.

CONFIRMADO:
  - Handoffs 01 a 08 lidos via pipeline_handoff_get (DEL-011).
  - pipeline_sensors executado nesta sessão, com a saída transcrita em GATES.
  - pipeline_diff de internal/domain/emprestimo.go, internal/storage/repositorio.go, internal/http/handler.go e internal/http/erros.go lido por inteiro. A rota está com auth(...) (handler.go), os códigos 409 estão no catálogo (erros.go:31-32, 67-68) e ErrRenovacoesInvalidas está em errosDeBug.
  - Read de migrations/0002_renovacao_emprestimo.{up,down}.sql.
  - Grep das funções de teste nos arquivos novos e dos casos de renovação em handler_test.go, apoio_test.go e erros_interno_test.go.

ASSUNCOES:
  - Os pulados não visíveis na saída truncada do sensor têm o mesmo motivo (falta de BIBLIOTECA_TEST_DATABASE_URL). O QA infere isso pelo padrão e pelos handoffs 04, 05 e 08, sem ter visto a lista.
  - A execução com banco do 05 é tomada como relatada, não como reproduzida.
  - Volume da tabela emprestimo pequeno o bastante para o CHECK validar dentro do statement_timeout (assunção herdada de 02, 03 e 08, não medida).

ARQUIVOS:
  - nenhum (QA só lê).

RISCOS:
  - Camada de banco não provada por este gate: migração 0002 up/down/up, catálogo, CHECK, CAS de RegistrarRenovacao e concorrência dependem só do relato do 05. Mitigação: rodar a suíte com banco antes do merge (humano ou CI com Postgres e BIBLIOTECA_TEST_DB_OBRIGATORIO=1).
  - Pulados fora da camada desta delivery (internal/migrate TestControle*/TestNovoPrazo*, cmd TestMigrate*/TestFechar*): mesmo motivo, falta de banco. Não foram tocados pela DEL-011, mas também não foram reexecutados com banco pelo QA.
  - Ordem de deploy: `migrate up` 0002 antes do binário novo, senão dá 500 (42703) em toda rota de empréstimo. No rollback, reverter o binário antes do down, que perde o contador. Precisa ir para o report/runbook no docs-update.
  - Contrato da API: o campo `renovacoes` é novo em todas as respostas de empréstimo, além da rota nova e dos códigos 409 `emprestimo_vencido` e `limite_renovacoes_atingido`. A mudança é aditiva, mas quebra cliente com decodificação estrita.
  - Working tree com mudanças da DEL-010 (report.md e INDEX.md) misturadas ao diff.

SINAIS:
  SECURITY: sim — já revisado no 07 (aprovado, dois achados BAIXO informativos); nada novo no QA.
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff; mod_verify "all modules verified"; vuln "No vulnerabilities found."
  MIGRATION: sim — 0002 aprovada no 08; prova em banco pendente (pulada no sensor, só a evidência do 05).
  PERFORMANCE: nao — UPDATE e SELECT por PK, busca pelo índice parcial existente.

PROXIMO: docs-update
MOTIVO: sem achado bloqueante de escopo, cobertura ou scope creep; release-ops não está em enabled_modules (data-model, migration-reviewer). RESULTADO parcial porque a camada de banco não foi provada pelo QA. O docs-update deve registrar no report, seção QA, "Resultado: parcial" com este motivo, a D-17, o mapa de módulos e a ordem de deploy/rollback. Para virar ok, é preciso rodar a suíte com banco antes do merge.
