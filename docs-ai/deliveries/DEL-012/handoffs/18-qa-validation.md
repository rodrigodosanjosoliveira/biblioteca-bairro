DELIVERY_ID: DEL-012
TASK_CODE: DEL-012
AGENT: qa-validation
STATUS: ok

RESULTADO: parcial
MOTIVO_DO_PARCIAL:
  1) O sensor de teste, na minha execução de agora, continua com skipped=200, todos com o motivo "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado". O ambiente do sensor não tem banco. A camada de banco agora tem prova executada, mas é da sessão principal, não minha nem do sensor (ver EVIDENCIA_DA_SESSAO_PRINCIPAL, V0): 1908 pass, 0 fail, 0 skip. Conferi o artefato JSON por conta própria. Pela regra do gate, o pulo no sensor mantém o resultado em parcial. O motivo, porém, deixou de ser "camada de banco sem prova" e passou a ser "prova de banco fora do sensor".
  2) Ainda não têm prova além da do dev-implementation: V2 (contexto de build), V10 (go version -m e govulncheck -mode=binary no binário da imagem), os roteiros do README em bash e zsh (handoffs 11 e 14) e o cosign da distroless, que não está instalado.
  Não há achado bloqueante, e nenhuma execução contradiz o que o dev registrou.

GATES (pipeline_sensors, todos, executado por mim nesta rodada; ok: true):
  vet:        ok, exit_code 0, 514 ms, output "", skipped 0
  test:       ok, exit_code 0, 1390 ms, skipped 200
              "pacotes: 8 (8 ok, 0 com falha, 0 sem testes)
               testes: 1580 passaram, 0 falharam, 200 pulados"
              Amostra dos pulados (saída truncada pelo sensor), todos com o motivo "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado":
                cmd/biblioteca-bairro: TestFecharEsperaOBackendCanceladoSair (migrate_correcao_test.go:210), TestFecharRespeitaOPrazoSeALimpezaTravar (:239), TestMigrateAceitaParametrosDePoolNaDSN/pool_max_conns (:304)
                internal/migrate: TestControleCatalogo, TestControleRejeitaLinhasInvalidas, TestControleAceitaLinhasValidas, TestControleUpEIdempotente, TestControleDown, TestControleDownComLinhas (controle_banco_test.go), TestNovoPrazoNaoPositivoOuSubMilissegundoCaiNoPadrao/* e TestNovoPrazoDeUmMilissegundoOuMaisNaoDesliga/* (correcao_test.go), TestControleAdotadoComArmadilhaFalhaFechado/* (endurecimento_test.go:158) ... (truncado)
  build:      ok, exit_code 0, 2699 ms, output "", skipped 0
  vuln:       ok, exit_code 0, 7758 ms, "No vulnerabilities found."
  mod_verify: ok, exit_code 0, 1765 ms, "all modules verified"
  Fuzz: não se aplica. O diff não tem código Go nem alvo de fuzz.

DIFF (pipeline_diff name_only contra origin/main, merge-base 90c4a10, executado por mim): 28 arquivos. É o mesmo conjunto do handoff 17, mais o próprio 17-qa-validation.md. Nenhum arquivo em cmd/, internal/, migrations/, go.mod, go.sum ou .github/. Makefile, .pipeline-claude.lock e .claude/agents/* são a atualização do pipeline-claude para rc.2, feita pelo usuário. Ficam fora do commit da DEL-012.

EVIDENCIA_DA_SESSAO_PRINCIPAL (executada pela sessão principal em 2026-11-07T23:49:50Z, não por mim):
  Script: /tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/qa012.sh
  Saída:  /tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/qa012-saida.txt
  Artefatos: qa012-test.json e qa012-history.txt, na mesma pasta.
  Li o script e a saída. Nos artefatos, conferi por minha conta o seguinte:
  - V0 (banco): Postgres postgres:16-alpine@sha256:0cefaabd...31ab com trust. O digest é o mesmo de .github/workflows/delivery.yml:24, que conferi. O comando foi `BIBLIOTECA_TEST_DATABASE_URL=... BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 -p 1 -json ./...`, com "go test exit=0". Os 8 pacotes têm pass no nível de pacote. No qa012-test.json, contei 1908 eventos pass de teste, 0 eventos skip e 0 eventos fail (grep meu). Testes de banco com pass no JSON incluem TestBanco*, TestMigracaoUpDownUp, TestControleUpEIdempotente, TestRegistrarRenovacaoConcorrente e TestFecharEsperaOBackendCanceladoSair.
  - V1: "build exit=0", imagem sha256:9e9d2bbe...548a, Dockerfile:13 e :36 com FROM por tag e digest. Observação: o docker history marca as camadas da aplicação com "27 hours ago", então o build reaproveitou o cache do build do dev. O exit 0 prova que o Dockerfile atual resolve para essa imagem, mas não prova um build a frio.
  - V3: User=65532:65532, Entrypoint=["/biblioteca-bairro"], Cmd=null, Healthcheck=null. Env só PATH e SSL_CERT_FILE, os da distroless.
  - V4: `docker run --rm IMG foo` termina com exit=2.
  - V6: num banco novo (qa012_app), migrate up aplicou a versão 1 (esquema_emprestimo) e a 2 (renovacao_emprestimo), com "lock adquirido", exit 0. migrate status listou 0001 e 0002 como aplicadas, exit 0.
  - V5/V7: healthz=200, readyz=200. docker top mostra um único processo, /biblioteca-bairro, e State.Pid tem a cmdline "/biblioteca-bairro". O log tem 0 linhas com panic.
  - V8: depois de docker stop -t 25, ExitCode=0, e a última linha do log é "biblioteca-bairro encerrado".
  - V9: o token e DATABASE_URL=/postgres:// aparecem 0 vezes no history e 0 vezes no log. O teste do token no history é fraco, porque o token foi gerado depois do build. Por isso li o qa012-history.txt inteiro: só há ENTRYPOINT, EXPOSE 8080, USER 65532:65532, COPY do binário (16.9MB) e as camadas bazel da distroless. Não há ENV, ARG nem RUN com segredo.
  - Limpeza: rede, containers, imagem e qa012.env removidos por nome. O env-file foi criado com modo 600.
  Não cobertos nesta execução: V2, V10, os roteiros do README em bash e zsh, e o cosign.

COBERTURA_DO_ESCOPO (item → verificação; não há teste Go, e a justificativa está no handoff 04 DECISAO_DE_TESTE):
  E1. Dockerfile: imagem única que serve a API sem argumentos e roda migrate up/status como subcomando.
      - Leitura estática (prova minha, handoff 17): digests, USER, ENTRYPOINT exec, sem CMD, HEALTHCHECK ou ENV de segredo.
      - Runtime (sessão principal): V1, V3, V4, V5, V6, V7, V8 e V9, conforme acima.
      - Só do dev (handoff 05): V10 e a verificação dos digests (imagetools e pull).
  E2. .dockerignore em modo allowlist.
      - Leitura (prova minha, handoff 17): `*` seguido de !go.mod, !go.sum, !cmd/, !internal/, !migrations/.
      - Indireta (sessão principal): o history não mostra nenhum COPY além do binário. Isso não prova o conteúdo do contexto.
      - V2 (find /ctx) só tem a evidência do dev (handoff 05).
  E3. README, seção "Executar a API".
      - Conferido contra o código (prova minha, handoff 17).
      - A sessão principal exercitou o fluxo de container do README: env-file com umask 077, migrate up antes da API, --stop-timeout/-t 25, /healthz e /readyz, e testes com Postgres trust descartável usando BIBLIOTECA_TEST_DATABASE_URL e BIBLIOTECA_TEST_DB_OBRIGATORIO=1. O script não é o README colado literalmente.
      - Os roteiros go run com read -rs em bash 5.2 e zsh 5.9 só têm a evidência do dev (handoffs 11 e 14).
  E4. RUNBOOK-MIGRACAO §10: verificado no diff (prova minha, handoff 17). A ordem migrate up → API foi confirmada em runtime pela sessão principal (V6 e V7).
  E5. DEL-011 marcada como merged no INDEX e no report: verificado no diff (prova minha, handoff 17).

SCOPE_CREEP: nenhum. O diff não mudou desde o handoff 17.

BORDAS CONFERIDAS (além das do handoff 17):
  - O Postgres do teste usa o mesmo digest do CI, então V0 reproduz o ambiente do CI.
  - V6 rodou num banco vazio criado na hora, então cobre a migração do zero, não só a idempotência.
  - O history mostra as camadas tzdata e tzdata-legacy da distroless. O import time/tzdata segue como garantia independente da base, e o comentário de Dockerfile:28-32 continua correto.

ASSUNCOES:
  - A diferença entre 1908 pass com banco e 1580 pass + 200 pulados sem banco (128 a mais) vem de subtestes que só existem quando o teste-pai não é pulado. Não listei esses 128 um por um.
  - Tomo como fiel a saída da sessão principal. Verifiquei o JSON e o history. A saída do docker e do curl conheço só pelo texto gravado.

ARQUIVOS:
  - nenhum (QA sem escrita)

RISCOS:
  - O sensor de teste não tem banco, então skipped=200 vai se repetir em qualquer QA futuro. Para fechar a regra com "ok", o ambiente do sensor precisa de BIBLIOTECA_TEST_DATABASE_URL, ou de um sensor dedicado com banco. Isso é configuração (.pipeline-claude.json), que o usuário faz fora da sessão.
  - V1 foi um rebuild com cache, não um build a frio. Os digests podem envelhecer sem renovação automática.
  - V2 e V10 só têm a evidência do dev. A imagem não é construída nem escaneada no CI.
  - O cosign da distroless não foi verificado.
  - B-S3 (BAIXO): Dockerfile e .dockerignore estão fora de security_surface_paths. Fica com o usuário.
  - Risco residual do B-S4 (colar os blocos go run juntos sem bracketed paste). O README avisa.
  - A atualização para rc.2 (Makefile, lock, .claude/agents) precisa ficar fora do commit.
  - O incidente do docker builder prune (handoff 05) precisa entrar no report.

PENDENCIAS PARA O DOCS-UPDATE:
  - Na seção QA do report, registrar Resultado: parcial, com os dois motivos acima. Citar V0 a V9 como execução da sessão principal em 2026-11-07T23:49:50Z e V2, V10 e os roteiros do README como evidência só do dev. Transcrever também SUPPLY_CHAIN_EVIDENCIA (05), R1 a R3 (10) e Security (16).

SINAIS:
  SECURITY: sim. Revisado (handoff 16, aprovado). Só o B-S3 (BAIXO, com o usuário) segue aberto.
  SUPPLY_CHAIN: sim. Revisado (handoff 10, sem bloqueio). Imagens base por digest, cosign não verificado.
  MIGRATION: nao
  PERFORMANCE: nao

PROXIMO: docs-update
MOTIVO: Sem achado bloqueante. A execução da sessão principal provou a camada de banco (0 skip, 0 fail) e o runtime da imagem (V1 e V3 a V9), sem contradizer o dev. O resultado fica em parcial por dois motivos: o sensor de teste ainda pula 200 testes de banco, e V2, V10, os roteiros do README e o cosign só têm evidência do dev. O release-ops não está em enabled_modules.
