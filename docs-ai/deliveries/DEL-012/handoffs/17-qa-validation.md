DELIVERY_ID: DEL-012
TASK_CODE: DEL-012
AGENT: qa-validation
STATUS: ok

RESULTADO: parcial
MOTIVO_DO_PARCIAL:
  1) O sensor de teste tem skipped=200, e há testes da camada de banco entre eles (internal/storage, internal/migrate, internal/http/banco_*, cmd/biblioteca-bairro/migrate_correcao_test.go). O motivo é sempre o mesmo: "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado". Nesta execução minha, a camada de banco não foi provada. A única prova com banco é de terceiro: o handoff 05 (V0, "Extra") registra `BIBLIOTECA_TEST_DATABASE_URL=... BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 ./...` com 8 pacotes ok contra Postgres 16 trust.
  2) Não tenho shell nem Docker. Os critérios de imagem V1 a V10 têm só a evidência do dev-implementation (handoffs 05 e 14). Eu não os repeti e não os declaro como prova minha. A entrega não mexe em código Go, então nenhum teste da camada de banco é afetado. O parcial é a regra do gate, não uma suspeita de regressão.

GATES (pipeline_sensors, todos, executado por mim; ok: true):
  vet:        ok, exit_code 0, 162 ms, output "", skipped 0
  test:       ok, exit_code 0, 194 ms, skipped 200
              "pacotes: 8 (8 ok, 0 com falha, 0 sem testes)
               testes: 1580 passaram, 0 falharam, 200 pulados"
              Amostra dos pulados (saída truncada pelo sensor). Todos têm o motivo "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado":
                cmd/biblioteca-bairro: TestFecharEsperaOBackendCanceladoSair, TestFecharRespeitaOPrazoSeALimpezaTravar, TestMigrateAceitaParametrosDePoolNaDSN/* (8 subtestes), TestMigrateUpCommitComTagInesperadaCitaOArquivo, TestMigrateStatusNomeArbitrarioDoBancoNaoQuebraASaida
                internal/migrate: TestControleCatalogo, TestControleRejeitaLinhasInvalidas, TestControleAceitaLinhasValidas, TestControleUpEIdempotente
                internal/http: TestBancoRenovacao, TestBancoRenovacaoConcorrente, TestBancoFluxoCompleto, TestBancoReferenciasInexistentes, TestBancoEmprestimoConcorrente, TestBancoDevolucaoConcorrente, TestBancoPoolEsgotado
                internal/storage: TestMigracaoUpDownUp, TestEsquemaRestricoes, TestAbrirEmprestimoSemRenovacoes, TestRegistrarRenovacao, TestRegistrarRenovacaoRecusas/* (5), TestRegistrarRenovacaoConcorrente, TestRegistrarRenovacaoConcorrenteComDevolucao, TestRegistrarDevolucaoConcorrenteComRenovacao, ... (truncado)
  build:      ok, exit_code 0, 532 ms, output "", skipped 0
  vuln:       ok, exit_code 0, 2948 ms, "No vulnerabilities found."
  mod_verify: ok, exit_code 0, 274 ms, "all modules verified"
  Fuzz: não se aplica. Nenhum alvo de fuzz foi tocado e o diff não tem código Go (pipeline_diff name_only abaixo).

DIFF (pipeline_diff contra origin/main, merge-base 90c4a10, executado por mim): 27 arquivos.
  Da entrega: Dockerfile (??), .dockerignore (??), README.md, docs-ai/RUNBOOK-MIGRACAO.md, docs-ai/deliveries/INDEX.md, docs-ai/deliveries/DEL-011/report.md, docs-ai/deliveries/DEL-012/ (report e handoffs).
  Fora da entrega e fora do commit, por decisão do usuário: Makefile (só PIPELINE_CLAUDE_VERSION rc.1 → rc.2), .pipeline-claude.lock (toolkit_version rc.2 e hashes de dev-implementation.md e test-writer.md), .claude/agents/dev-implementation.md e .claude/agents/test-writer.md. Conferi o patch do Makefile e do lock: é só a atualização do pipeline-claude, sem nada da DEL-012.
  Nenhum arquivo em cmd/, internal/, migrations/, go.mod, go.sum ou .github/.

COBERTURA_DO_ESCOPO (item → verificação; não há teste Go, e a justificativa do test-writer está no handoff 04 DECISAO_DE_TESTE):
  E1. Dockerfile: uma imagem serve a API sem argumentos e roda migrate up/status como subcomando.
      - Verifiquei por leitura estática (prova minha): Dockerfile:13 `FROM golang:1.27.3-trixie@sha256:af1bfc26...ea80 AS build` e Dockerfile:36 `FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c...8329`. As duas têm digest e tag com patch exato, sem latest (parte estática do V1). USER 65532:65532 (:40), ENTRYPOINT ["/biblioteca-bairro"] em forma exec (:44), sem CMD, sem HEALTHCHECK, sem ENV/ARG de segredo, ADDR ou MAX_CONNS (ENV só CGO_ENABLED, GOTOOLCHAIN e GOFLAGS no estágio de build) (parte estática do V3 e do V9). Build com CGO_ENABLED=0, -trimpath, -tags timetzdata e sem -s -w.
      - Evidência só de terceiro (dev-implementation, handoff 05): V1 (docker build exit 0), V3 em runtime (docker inspect: User 65532:65532, Entrypoint ["/biblioteca-bairro"], Cmd null, Env só da distroless, Healthcheck null), V4 (foo → exit 2), V5 (sem panic de fuso), V6 (migrate up/status exit 0, 2 migrações), V7 (healthz e readyz 200, PID 1 = /biblioteca-bairro, uid 65532), V8 (docker stop -t 25 em 0.27 s, ExitCode 0, "biblioteca-bairro encerrado"), V9 (docker history sem token/senha), V10 (go version -m: go1.27.3, -trimpath, CGO_ENABLED=0, -tags=timetzdata; govulncheck -mode=binary limpo). Os digests (imagetools inspect e pull) também são só de terceiro (SUPPLY_CHAIN_EVIDENCIA, handoff 05).
      - A correção da premissa sobre a tzdata vale: o import time/tzdata é a garantia real, e o comentário em Dockerfile:28-32 registra isso.
  E2. .dockerignore: allowlist.
      - Verifiquei por leitura (prova minha): `*` e depois `!go.mod`, `!go.sum`, `!cmd/`, `!internal/`, `!migrations/`. Excluem .git, .claude, .pipeline-claude*, docs-ai, bin/, .env*, o binário da raiz e o próprio README.
      - Evidência só de terceiro: V2 (build temporário via stdin com find /ctx, que listou só go.mod, go.sum, cmd, internal e migrations), handoff 05.
  E3. README, seção "Executar a API".
      - Verifiquei contra o código (prova minha). A tabela de variáveis em README.md:46-53 bate com internal/config: token de 32 a 512 bytes (config.go:9-10), MAX_CONNS de 1 a 100 com padrão 10 (config.go:15-17), lock de 100ms a 60s com padrão 5s e statement de 1s a 30m com padrão 60s (migracao.go:7-13). ADDR padrão :8080 (config_test.go:80).
      - Ordem migrate up antes da API e código 2 para argumento desconhecido: README.md:55-57.
      - Uso com go run e read -rs isolado (S8, S9 e B-S4): README.md:59-84. Uso com container: :86-144, com --env-file, umask 077, .env e .env.postgres (ignorados por .gitignore:17-18), --stop-timeout 25 e o motivo, e sem HEALTHCHECK (/healthz e /readyz).
      - Testes com banco: Postgres trust descartável, BIBLIOTECA_TEST_DATABASE_URL e BIBLIOTECA_TEST_DB_OBRIGATORIO=1 em :146-169, com o aviso de que trust nunca vale para o banco da API.
      - Nenhum segredo literal: a DSN é placeholder com sslmode=verify-full e o token vem de openssl rand.
      - Evidência só de terceiro: a execução real dos roteiros go run em bash 5.2 e zsh 5.9 (handoff 14, com senha 0 na saída e 0 no histórico, e a contraprova do B-S4) e o go test com banco (handoff 05). Nenhum roteiro do README roda no CI.
  E4. docs-ai/RUNBOOK-MIGRACAO.md §10: o diff troca "(o repositório não tem Dockerfile)" pela referência ao Dockerfile, ENTRYPOINT exec sem flags nem wrapper, e migrate up/status antes da API. Verificado no diff (prova minha).
  E5. Marcação merged da DEL-011: o diff de INDEX.md muda DEL-011 de "em revisão" para "merged" e acrescenta a linha da DEL-012 como draft. O diff de DEL-011/report.md muda status de "em revisão" para "merged". Verificado no diff (prova minha). Coerente com o commit 90c4a10 (Merge PR #13, task/DEL-011).

SCOPE_CREEP (FORA_DE_ESCOPO do triage):
  - cmd/, internal/, migrations/, go.mod, go.sum: sem mudança (diff).
  - Makefile: alterado no working tree, mas é a atualização do pipeline-claude para rc.2 feita pelo usuário fora da sessão. Não pertence à DEL-012 e fica fora do commit. Não é scope creep da entrega, e o git-delivery precisa excluí-lo, junto com .pipeline-claude.lock e .claude/agents/.
  - .github/workflows, docker-compose, registry/tags/K8s, HEALTHCHECK, .gitattributes, .pipeline-claude.json: nenhum no diff.

BORDAS CONFERIDAS:
  - O .dockerignore com `*` exclui .env e .env.postgres, que o README cria na raiz: as credenciais do exemplo não entram no contexto de build.
  - `.env.*` em .gitignore:18 cobre .env.postgres, e .gitignore:19 só reabre .env.example.
  - ENTRYPOINT sem wrapper de shell, então o SIGTERM chega ao PID 1. O prazo de 25 s fica acima do prazoShutdown de 20 s.
  - Sem shell na imagem: o README usa `docker exec` só no container do Postgres, nunca no da API.
  - O contexto inclui os *_test.go de migrations e internal. Não vão para a imagem final, porque o go build ignora _test, e o estágio final só copia o binário. Inofensivo.

PENDENCIAS PARA O DOCS-UPDATE (não bloqueiam o QA):
  - O docs-ai/deliveries/DEL-012/report.md continua com status draft. Falta transcrever SUPPLY_CHAIN_EVIDENCIA (handoff 05), os riscos R1 a R3 (handoff 10), o resultado de Security (handoff 16) e o desta seção QA, Resultado: parcial, com o motivo acima.

ASSUNCOES:
  - Considero os 200 pulados todos de banco pela amostra e pelo motivo uniforme. A saída do sensor veio truncada, então não vi a lista inteira.
  - O enunciado do handoff 15 ("pipeline_diff lista só o README.md") não bate com o diff que obtive, que inclui RUNBOOK, INDEX e DEL-011/report. Conferi esses três diretamente, e o conteúdo está correto.

ARQUIVOS:
  - nenhum (QA sem escrita)

RISCOS:
  - A camada de banco não foi provada nesta execução do QA (200 pulados). Existe só a execução com banco do dev (handoff 05).
  - A imagem não é construída nem escaneada no CI. V1 a V10 valem só para este estado e só pela evidência de terceiro.
  - O cosign da distroless não foi verificado (cosign ausente), e os digests envelhecem sem renovação automática (handoffs 05 e 10).
  - B-S3 (BAIXO): Dockerfile e .dockerignore fora de security_surface_paths. É do usuário, fora da sessão.
  - Risco residual do B-S4: quem colar os dois blocos do go run juntos, num terminal sem bracketed paste. O README avisa.
  - A atualização do pipeline-claude para rc.2 (Makefile, .pipeline-claude.lock, .claude/agents/) está no working tree. O git-delivery precisa deixá-la fora do commit.
  - Incidente do dev (handoff 05): `docker builder prune -f --filter until=1h` apagou cache de BuildKit além do desta delivery. Só cache, mas deve ficar registrado no report.

SINAIS:
  SECURITY: sim. Revisado (handoff 16, aprovado). Só o B-S3 (BAIXO, com o usuário) segue aberto.
  SUPPLY_CHAIN: sim. Revisado (handoff 10, sem bloqueio). Imagens base por digest, cosign não verificado.
  MIGRATION: nao
  PERFORMANCE: nao

PROXIMO: docs-update
MOTIVO: Sem achado bloqueante. Os cinco itens do escopo foram verificados, sem scope creep da entrega. O resultado é parcial por regra: há testes da camada de banco pulados no sensor, e os critérios de imagem V1 a V10 só têm evidência de terceiro. O release-ops não está em enabled_modules, e o report não pede require_release_ops.
