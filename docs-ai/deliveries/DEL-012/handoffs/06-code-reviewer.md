DELIVERY_ID: DEL-012
TASK_CODE: DEL-012
AGENT: code-reviewer
STATUS: bloqueado

RESULTADO:
  Dockerfile, .dockerignore e RUNBOOK §10 conferem com as decisões dos handoffs 02 a 04 (B1 = b, B2 = a). O README não traz segredo real e não contradiz D-13/D-16/D-17 nem o runbook. Há um bloqueio de correção no procedimento do README: os dois roteiros para copiar e colar falham na primeira execução por corrida com a subida do PostgreSQL.

BLOQUEIOS:
  B1. [README.md:89] e [README.md:91]: o roteiro com Docker faz `docker run -d ... postgres:16-alpine` e logo em seguida `migrate up`. O container do Postgres leva alguns segundos para o initdb (e reinicia o servidor no fim do init). O `migrate` abre uma conexão dedicada por `pgx.ConnectConfig` sem nova tentativa (D-16 G1), e o `migrate up` imediato sai com erro de conexão. A mesma corrida acontece na subida da API, porque o Ping de boot falho encerra o processo (cmd/biblioteca-bairro/main.go:24 e :108-109, D-15). O README é o entregável desta delivery, e um procedimento que falha ao ser copiado não cumpre o escopo do triage (ordem `migrate up` antes da API). Correção: esperar a prontidão por TCP antes do `migrate up`, por exemplo `until docker exec biblioteca-pg pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; do sleep 1; done`. Use `-h 127.0.0.1` e não o socket, porque o servidor temporário do initdb só escuta no socket unix e daria falso pronto.
  B2. [README.md:116-120]: a mesma corrida existe no roteiro de testes com banco. `docker run -d ... trust postgres:16-alpine` é seguido de `go test ./...` com `BIBLIOTECA_TEST_DB_OBRIGATORIO=1` (README.md:119), e assim os testes de banco reprovam, em vez de pular, se o Postgres ainda não aceita conexão (com cache de build quente, o `go test` chega ao banco em menos de 1 s). Correção: a mesma espera do B1 (`docker exec biblioteca-pg-teste pg_isready -h 127.0.0.1 -U postgres`) antes do `go test`.

SUGESTOES:
  S1. [README.md:82]: `umask 077` solto altera a umask da shell interativa de quem copia o bloco, e o efeito dura depois do exemplo. Sugestão: rodar a criação dos arquivos num subshell, por exemplo `( umask 077; printf ... > .env.postgres; printf ... > .env )`.
  S2. [README.md:89] e [README.md:117]: `postgres:16-alpine` aparece por tag flutuante. É só um exemplo local e o dev registrou isso em ASSUNCOES. Fixar pelo mesmo digest do CI (.github/workflows/delivery.yml:24) ou dizer em uma linha que o exemplo usa tag e o CI usa digest deixa a escolha explícita.
  S3. [README.md:96]: falta a limpeza do exemplo (`docker stop -t 25 biblioteca-api`, `docker rm biblioteca-api biblioteca-pg`, `docker network rm biblioteca-net`, `rm .env .env.postgres`). Sem ela, o `.env` com senha e token fica no diretório de trabalho. Ele é ignorado pelo git (.gitignore:17-18) e pela allowlist do .dockerignore, mas continua em disco.
  S4. [README.md:44]: "nenhuma credencial fica ... no repositório" convive com o exemplo que grava `.env` na raiz do checkout. A frase está certa no sentido de "versionada"; trocar para "no código, na imagem nem no git" evita a leitura literal errada.
  S5. [Dockerfile:28-29]: o comentário já registra que `-tags timetzdata` é redundante com o import `time/tzdata` (correção do test-writer). Pode citar os dois arquivos (internal/domain/multa.go:7, internal/storage/repositorio.go:9) para que ninguém remova o import achando que a tag basta.

CONFIRMADO:
  - Dockerfile:13 `golang:1.27.3-trixie@sha256:af1bfc26...ea80` e Dockerfile:33 `gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c...8329`: tag com patch exato e digest de índice, com a evidência registrada no handoff 05 (imagetools inspect, plataformas amd64/arm64 e pull). Builder na stable, coerente com B1 = b e com a D-13. go.mod não mudou (pipeline_diff lista 12 arquivos, nenhum em cmd/, internal/, migrations/, go.mod, go.sum ou Makefile).
  - Dockerfile:17-19 CGO_ENABLED=0, GOTOOLCHAIN=local e GOFLAGS=-mod=readonly. Dockerfile:22 `go mod download && go mod verify`. Dockerfile:30-31 `-trimpath -buildvcs=false -tags timetzdata`, sem `-ldflags -s -w`.
  - Dockerfile:35-41: COPY só do binário, `USER 65532:65532` numérico, `EXPOSE 8080`, `ENTRYPOINT ["/biblioteca-bairro"]` em forma exec, sem CMD, HEALTHCHECK, wrapper ou flags. Nenhum ENV/ARG de DATABASE_URL, token, ADDR ou MAX_CONNS na imagem final (o ENV da linha 17 fica no estágio de build e só tem flags de toolchain).
  - .dockerignore:2-7 é allowlist (`*`, `!go.mod`, `!go.sum`, `!cmd/`, `!internal/`, `!migrations/`), inclui migrations/controle/ (go:embed) e exclui .env, .git, .claude, docs-ai e o binário local (V2 do handoff 05).
  - Tabela do README (README.md:46-53) bate com o código e as decisões: token de 32 a 512 bytes (internal/config/config.go:9-10), MAX_CONNS de 1 a 100 com padrão 10 e ADDR padrão :8080 (config.go:15-18), lock de 100ms a 60s com padrão 5s e statement de 1s a 30m com padrão 60s (D-16 item 4, DECISOES.md:381-385).
  - README.md:55-57: migração antes da API, sem migrar no boot, e código 2 para argumento desconhecido, coerentes com a D-16 item 1 (DECISOES.md:372-377) e com o RUNBOOK §10.
  - Nenhum segredo literal no README. O token vem de `openssl rand -hex 32` (64 caracteres hex, dentro de 0x21-0x7E), a senha de `openssl rand -hex 16`, e o DSN de `go run` usa placeholders com `sslmode=verify-full` (README.md:62-63, :81-86). Nenhum `-e VAR=valor` com credencial; tudo passa por `--env-file`.
  - `trust` aparece só no Postgres de teste, com o aviso "nunca no banco da API" (README.md:112-124), coerente com a D-17 (DECISOES.md:617-619) e com a D-13 (CI com trust, DECISOES.md:182). `sslmode=disable` fica restrito ao banco local da rede Docker (README.md:98-100).
  - `--stop-timeout 25` e `docker stop -t 25` justificados pelo prazo de shutdown de 20 s (README.md:95, :102-104). /healthz e /readyz indicados no lugar do HEALTHCHECK (README.md:106-107).
  - RUNBOOK-MIGRACAO.md:119-122: a frase "não tem Dockerfile" foi trocada pela referência ao ENTRYPOINT exec sem flags e ao `docker run <imagem> migrate up|status` antes da API.
  - INDEX.md e DEL-011/report.md: só a marcação merged da DEL-011 e a linha draft da DEL-012, como o triage previu.
  - Revisão Go (erro, context, goroutine): não se aplica, porque o diff não tem código Go.

ASSUNCOES:
  - Não reexecutei o build nem os critérios V0-V10. Conto com a evidência do handoff 05; o QA repete.
  - O tempo de initdb do postgres:16-alpine que sustenta B1/B2 vem do comportamento conhecido da imagem oficial (servidor temporário só em socket e reinício). Não medi nesta revisão. O handoff 05 também não mostra se o dev esperou entre os passos no V6.
  - docs-ai/deliveries/DEL-012/report.md ainda tem campos `<preencher>`. Preenchê-lo e transcrever SUPPLY_CHAIN_EVIDENCIA é tarefa do docs-update, não achado desta revisão.

ARQUIVOS:
  - nenhum (revisor sem escrita)

RISCOS:
  - O `docker builder prune -f --filter until=1h` do dev (handoff 05, LIMPEZA) apagou cache de BuildKit de fora desta delivery. Só cache, mas deve ser registrado no report.
  - A assinatura da distroless não foi verificada com cosign (ausente na máquina): fica para o supply-chain-review ou o humano.
  - A imagem não é construída no CI, e o Dockerfile está fora de security_surface_paths (sugestão ao humano, fora da sessão).

SINAIS:
  SECURITY: sim — imagem de execução (não root, PID 1, segredos fora das camadas) e documentação de credenciais e do modo trust; require_security ligada.
  SUPPLY_CHAIN: sim — duas imagens base de terceiros por digest de índice; cosign não verificado; go.mod/go.sum inalterados.
  MIGRATION: nao — sem migração nova; o README só documenta `migrate up|status`.
  PERFORMANCE: nao — nenhum caminho de consulta ou HTTP alterado.

PROXIMO: dev-implementation
MOTIVO: B1 e B2 (espera de prontidão do PostgreSQL nos dois roteiros do README) impedem o merge. Corrigidos, o fluxo volta ao code-reviewer e segue para security-review, supply-chain-review, qa-validation e docs-update (transcrição da evidência de digests e preenchimento do report).
