DELIVERY_ID: DEL-012
TASK_CODE: DEL-012
AGENT: architecture-guardrails
STATUS: bloqueado

ABORDAGEM:
  Artefatos novos/alterados, nenhum código Go muda (cmd/, internal/, migrations/, go.mod, go.sum, Makefile intocados):
  1. Dockerfile (raiz), multi-stage, editor dev-implementation:
     - Estágio build: imagem oficial golang na versão decidida em B1 (abaixo), variante Debian (não alpine), fixada como `golang:<X.Y.Z>-<variante>@sha256:<digest do índice multi-arch>` (tag mantida só para leitura; o digest é o que vale). ARG de versão permitido (não é segredo).
     - Camada de dependências: COPY go.mod go.sum, `go mod download` e `go mod verify`; depois COPY do código.
     - Build: `CGO_ENABLED=0 GOTOOLCHAIN=local GOFLAGS=-mod=readonly go build -trimpath -buildvcs=false -tags timetzdata -o /out/biblioteca-bairro ./cmd/biblioteca-bairro`.
       * CGO_ENABLED=0: confirmado que o projeto não usa cgo (sem `import "C"`; pgx é Go puro).
       * GOTOOLCHAIN=local: impede o go de baixar outro toolchain durante o build (a versão usada é exatamente a da imagem fixada).
       * -tags timetzdata: OBRIGATÓRIO. internal/domain/multa.go:21-25 e internal/storage/repositorio.go:47-49 fazem time.LoadLocation("America/Sao_Paulo") e dão panic se a tzdata faltar; embutir a tzdata no binário tira a dependência da tzdata da imagem final (é flag de build, não mudança de código).
       * -buildvcs=false: o .git fica fora do contexto (.dockerignore).
       * Sem `-ldflags=-s -w`: manter a tabela de símbolos permite `govulncheck -mode=binary` na imagem.
     - Estágio final: `gcr.io/distroless/static-debian<NN>:nonroot` (a variante Debian mais recente publicada no momento da edição), fixada por digest. Contém ca-certificates (DSN com sslmode=verify-full funciona) e /etc/passwd do nonroot. Sem shell.
     - `COPY --from=build /out/biblioteca-bairro /biblioteca-bairro`; `USER 65532:65532` explícito e numérico (permite runAsNonRoot verificar); `EXPOSE 8080` (só documentação; ADDR padrão :8080, porta >1024, nonroot escuta sem capability).
     - `ENTRYPOINT ["/biblioteca-bairro"]` em forma exec; sem CMD (ou CMD vazio). Servidor: `docker run <img>`; migração: `docker run <img> migrate up` / `migrate status`. O binário é PID 1 e recebe SIGTERM diretamente (signal.NotifyContext em cmd/biblioteca-bairro/main.go:81-85).
  2. .dockerignore (raiz), editor dev-implementation, em modo ALLOWLIST: `*` seguido de `!go.mod`, `!go.sum`, `!cmd/`, `!internal/`, `!migrations/`. Isso exclui .git, .github, .claude, .mcp.json, .pipeline-claude*, docs-ai, bin/, .env*, *.pem, *.key, secrets/ por construção. migrations/ (incluindo migrations/controle/) é obrigatório: é embutido via go:embed em migrations/migrations.go:8 e :15.
  3. README.md, editor docs-update: manter o texto atual e acrescentar a seção de execução descrita no triage, mais:
     - token gerado na hora (`openssl rand -hex 32` = 64 caracteres, dentro de 32..512 ASCII visível); variáveis passadas por `--env-file` apontando para arquivo ignorado pelo git (.env está no .gitignore) ou `-e VAR` sem valor (herda do shell). Nenhum `-e BIBLIOTECA_API_TOKEN=<valor literal>`.
     - DSN de exemplo com placeholder e apenas para banco local descartável; Postgres e API na mesma rede docker definida pelo usuário (`docker network create`), host do DSN = nome do container do Postgres.
     - Ordem: `migrate up` (container de vida curta) antes de subir a API (D-16).
     - `docker stop -t 25` / `--stop-timeout 25`: o padrão do Docker (10 s) é menor que o prazo de shutdown de 20 s (cmd/biblioteca-bairro/main.go:25) e mataria o processo com SIGKILL no meio do drain.
     - Sem HEALTHCHECK na imagem (sem shell/curl): o orquestrador usa GET /healthz (liveness) e GET /readyz (readiness), rotas sem autenticação (internal/http/handler.go:114-128).
     - `trust` só para o Postgres descartável dos testes (local/CI), com BIBLIOTECA_TEST_DATABASE_URL e BIBLIOTECA_TEST_DB_OBRIGATORIO=1; nunca para o banco da API.
     - Argumento desconhecido sai com código 2 (RUNBOOK §10).
  4. docs-ai/RUNBOOK-MIGRACAO.md §10, editor docs-update: trocar "(o repositório não tem Dockerfile)" por referência ao Dockerfile: ENTRYPOINT só o binário em forma exec, sem flags; migração via `docker run <img> migrate up`.
  5. Obtenção e verificação dos digests (a sessão não tem rede de registry garantida):
     - Quem tiver rede (dev-implementation se o ambiente permitir; senão o usuário) obtém o digest do ÍNDICE multi-arch, não de uma plataforma: `docker buildx imagetools inspect golang:<X.Y.Z>-<variante>` e `docker buildx imagetools inspect gcr.io/distroless/static-debian<NN>:nonroot` (ou `crane digest <ref>`), copiando o campo Digest.
     - Verificação: `docker pull <ref>@sha256:<digest>` resolve; `docker buildx imagetools inspect <ref>@sha256:<digest>` lista linux/amd64 e linux/arm64; para a distroless, `cosign verify gcr.io/distroless/static-debian<NN>:nonroot@sha256:<digest> --certificate-oidc-issuer https://accounts.google.com --certificate-identity keyless@distroless.iam.gserviceaccount.com`.
     - O report registra, para cada imagem: referência completa, data da obtenção, comando usado e saída relevante. Digest NUNCA é inventado, copiado de memória de modelo nem deixado como placeholder no commit; sem digest verificado, a etapa volta como bloqueio ao usuário.
  6. Verificação (QA, fora dos gates Go, não vai para o CI nesta delivery): `docker build -t biblioteca-bairro:del012 .`; `docker run --rm biblioteca-bairro:del012 foo; echo $?` = 2; `docker run --rm --env-file <arquivo> --network <rede> <img> migrate status` contra Postgres local; API sobe e /healthz e /readyz respondem 200; `docker history --no-trunc` sem token/DSN; `docker inspect` mostra User 65532:65532 e Entrypoint exec; opcional `govulncheck -mode=binary` no binário extraído. Gates Go (`make check`) continuam obrigatórios e devem ficar inalterados.

NAO_FAZER:
  - Não usar tag flutuante (golang:latest, golang:1.25, golang:1, distroless sem digest) nem digest de uma única plataforma; sempre índice multi-arch por digest.
  - Não escrever digest sem tê-lo obtido e verificado por comando com rede nesta delivery.
  - Não usar ENTRYPOINT/CMD em forma shell, wrapper sh/tini/dumb-init nem script de entrada; não passar flags ao binário no ENTRYPOINT (quebra o contrato de código 2 e o SIGTERM).
  - Não rodar `migrate up` automaticamente na subida do container da API (D-16: sem migração no boot).
  - Não colocar ENV/ARG com DATABASE_URL, BIBLIOTECA_API_TOKEN ou senha; não definir ENV para ADDR/BIBLIOTECA_DB_MAX_CONNS (os padrões já vêm do binário).
  - Não usar imagem final alpine/debian com shell, nem `USER root`, nem HEALTHCHECK com curl/wget/shell.
  - Não omitir `-tags timetzdata` (panic em America/Sao_Paulo se a imagem final perder a tzdata) nem habilitar cgo.
  - Não usar `-ldflags "-s -w"`.
  - Não excluir migrations/ (ou migrations/controle/) no .dockerignore; não trocar a allowlist por denylist.
  - Não criar docker-compose.yml, Makefile target de imagem, workflow em .github/workflows/, publicação em registry ou .gitattributes.
  - Não alterar go.mod (nem acrescentar linha `toolchain`) para alinhar com a imagem: go.mod é caminho protegido e de superfície de segurança e fica fora do escopo.
  - No README, não exemplificar token, senha ou DSN de produção literal, nem sugerir `trust` para o banco da API.
  - Não editar .pipeline-claude.json para incluir Dockerfile em security_surface_paths (fora da sessão; pode ser sugerido ao humano).

BLOQUEIO (decisões humanas, o resto da abordagem está fechado):
  B1. Versão do Go no builder. Conflito entre a instrução "a imagem golang deve bater com a diretiva go/toolchain do go.mod" e o que o repositório registra:
     - go.mod tem só `go 1.25.0`, sem `toolchain` (go.mod:3); a D-13 (docs-ai/DECISOES.md:158-162) diz que ela é a versão MÍNIMA e que o CI usa a versão stable (.github/workflows/delivery.yml:43 `go-version: stable`; o job de validação usa go-version-file, :69). Os testes e o govulncheck do CI rodam portanto com a stable, não com 1.25.
     - Pela política de suporte do Go (duas últimas versões principais, lançamentos em fev/ago), hoje (2026-11) a 1.25 deve estar fora de suporte desde a chegada da 1.27; a confirmar em go.dev/doc/devel/release por quem tiver rede. Binário compilado com 1.25 não recebe correções de stdlib e não seria o mesmo toolchain testado no CI.
     Opções:
       (a) golang:1.25.<último patch> por digest — casa literalmente com o go.mod; possivelmente sem suporte de segurança e diferente do CI.
       (b) golang:<stable atual, X.Y.Z> por digest — mesmo toolchain do CI de testes/govulncheck, coerente com a D-13 (go.mod = mínimo). RECOMENDADO. Registrar no report (ou em DECISOES como D-18) que a imagem segue a stable e é atualizada por delivery.
       (c) acrescentar `toolchain goX.Y.Z` no go.mod e casar a imagem com ela — muda go.mod, liga auto_flag require_security, exige supply-chain-review e volta ao triage para ampliar escopo.
  B2. Quem fornece os digests. Se o ambiente do dev-implementation não tiver rede de registry, o usuário roda os comandos do item 5 da ABORDAGEM e entrega as duas referências com digest (golang e distroless) e a saída do inspect/cosign. Opções: (a) dev-implementation obtém (requer docker/crane com rede na sessão de edição); (b) usuário obtém e repassa. Sem (a) ou (b), o Dockerfile não pode ser escrito.
  Confirmadas como técnicas (não precisam de decisão humana): distroless static nonroot no lugar de scratch (traz CA certs e passwd; tzdata vai embutida), CGO_ENABLED=0, ENTRYPOINT só o binário, sem docker-compose (não pedido; ampliação de escopo se o usuário quiser).

CONFIRMADO:
  - go.mod:3 `go 1.25.0`, sem `toolchain`; D-13 em docs-ai/DECISOES.md:158-162 declara go.mod como mínimo e CI na stable; .github/workflows/delivery.yml:43 `go-version: stable`, :69 `go-version-file: go.mod`, :24 postgres:16-alpine por digest (precedente de pinagem).
  - Sem cgo: Grep `import "C"` em **/*.go sem resultados; dependências diretas pgx/v5 e shopspring/decimal (go.mod:6-7), Go puro.
  - go:embed em migrations/migrations.go:8 (`*.sql`) e :15 (`controle/*.sql`): migrations/ precisa estar no contexto de build.
  - time.LoadLocation("America/Sao_Paulo") com panic em internal/domain/multa.go:21-25 e internal/storage/repositorio.go:47-49: justifica -tags timetzdata.
  - Sinais: cmd/biblioteca-bairro/main.go:81-85 (NotifyContext SIGINT/SIGTERM); prazo de shutdown 20 s em main.go:25; main.go:61 despacha os.Args[1:] (subcomando migrate).
  - /healthz e /readyz sem autenticação: internal/http/handler.go:114, :127-128.
  - RUNBOOK §10 texto a atualizar: docs-ai/RUNBOOK-MIGRACAO.md:115-120.
  - .gitignore cobre .env, .env.*, *.pem, *.key, secrets/ (usado pelo README com --env-file).
  - Dockerfile, .dockerignore, README.md e docs-ai/ fora de protected_paths e de security_surface_paths (.pipeline-claude.json:16-55). enabled_modules = data-model, migration-reviewer; a demanda não toca persistência nem esquema, então data-model não entra.
  - Pacotes Go do build: só cmd/, internal/ e migrations/ (Glob */**/*.go), o que sustenta a allowlist do .dockerignore.

ASSUNCOES:
  - A distroless static da variante Debian mais recente contém ca-certificates e usuário nonroot 65532 (documentação pública da distroless; a confirmar pelo inspect na obtenção do digest).
  - O calendário de suporte do Go citado em B1 é inferido da política pública e não foi verificado com rede.
  - test-writer não tem teste Go a escrever (nenhum código Go muda); a verificação da imagem é por comando no QA (item 6), não gate do projeto.
  - Assumo que a marcação merged da DEL-011 no working tree (INDEX.md e DEL-011/report.md) entra no commit, conforme o triage; não revisei o diff.

ARQUIVOS:
  - nenhum (revisor sem escrita); previstos: Dockerfile, .dockerignore (dev-implementation); README.md, docs-ai/RUNBOOK-MIGRACAO.md (docs-update).

RISCOS:
  - Imagens base são supply chain fora do go.sum: govulncheck ./... não vê CVE do toolchain da imagem nem da distroless; digest fixo envelhece sem atualização (não há Dependabot/renovate nesta delivery).
  - Toolchain do builder diferente do usado no CI (se B1 = a) produz binário cuja stdlib não foi testada nem escaneada.
  - Sem -tags timetzdata ou com imagem trocada para scratch, a API dá panic no boot por falta de tzdata.
  - docker stop com timeout padrão (10 s) corta o shutdown de 20 s e, no migrate, gera SIGKILL sem CancelRequest (RUNBOOK §8, risco B4).
  - Exemplo de README copiado para produção com trust ou token fraco.
  - Imagem não é construída no CI: um Dockerfile quebrado por mudança futura passa despercebido até alguém construir.
  - Dockerfile não está em security_surface_paths: mudanças futuras nele não forçam require_security (sugestão ao humano, fora da sessão).

SINAIS:
  SECURITY: sim — imagem de execução (usuário não root, segredos fora das camadas, PID 1 e sinais) e documentação de credenciais e do modo trust; require_security ligada à mão.
  SUPPLY_CHAIN: sim — duas imagens base de terceiros (golang e distroless) entram como dependência nova fora do go.mod; pinagem por digest e verificação (cosign na distroless) a revisar. go.mod/go.sum não mudam.
  MIGRATION: nao — nenhuma migração nova nem mudança de esquema; só empacota `migrate up|status` existentes.
  PERFORMANCE: nao — nenhum caminho de consulta ou HTTP alterado.

PROXIMO: usuário
MOTIVO: B1 (versão do Go no builder: 1.25 literal do go.mod vs stable do CI vs toolchain no go.mod) e B2 (quem obtém e verifica os digests com rede) são decisões humanas que impedem escrever as linhas FROM. Decididas, o fluxo segue para test-writer (data-model não se aplica: sem persistência) e depois dev-implementation com esta ABORDAGEM e NAO_FAZER.
