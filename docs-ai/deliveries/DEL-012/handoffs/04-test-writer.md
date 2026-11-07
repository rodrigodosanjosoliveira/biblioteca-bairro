DELIVERY_ID: DEL-012
TASK_CODE: DEL-012
AGENT: test-writer
STATUS: ok

DECISAO_DE_TESTE:
  Nenhum teste Go escrito. Não há teste Go viável que prove algo desta entrega sem ser artificial:
  - A entrega não tem código Go (Dockerfile, .dockerignore, README, RUNBOOK). Teste novo exigiria arquivo em cmd/ ou internal/ (protected_paths, .pipeline-claude.json:16-22) e o triage pôs cmd/ e internal/ fora de escopo.
  - O teste sugerido ("binário tem tzdata embutida com -tags timetzdata") não prova nada: a tzdata JÁ está embutida pelo código, independente de tag de build. internal/domain/multa.go:7 e internal/storage/repositorio.go:9 importam `_ "time/tzdata"` (DECISOES.md:90, desde DEL-001). Um teste disso testaria a stdlib ou um import existente.
  - Comportamentos que a imagem precisa preservar (código 2 para argumento desconhecido, SIGTERM e shutdown, migrate status) já têm cobertura nos testes de cmd/biblioteca-bairro; o que a entrega acrescenta (FROM, USER, ENTRYPOINT, contexto de build) só se verifica construindo e rodando a imagem, o que não é gate Go nem roda no CI nesta delivery.
  A verificação vira os critérios de imagem abaixo, que o dev-implementation executa e registra no docs-ai/deliveries/DEL-012/report.md (comando + saída relevante), e que o QA repete.

CORRECAO_PARA_O_DEV (premissa do guardrails):
  - O handoff 02 diz que `-tags timetzdata` é OBRIGATÓRIO porque a API daria panic sem tzdata na imagem. Isso não procede: os dois pacotes que chamam LoadLocation("America/Sao_Paulo") já importam time/tzdata. A tag fica redundante e inofensiva. Manter a tag conforme a ABORDAGEM não tem custo; retirar não quebra nada. O critério V5 abaixo prova a tzdata de forma empírica de qualquer jeito. Registre a redundância no report para o code-reviewer não tratar a tag como a única garantia.

CRITERIOS_DE_VERIFICACAO (dev-implementation executa e registra; QA repete):
  V0. Gates Go inalterados: `go build ./...`, `go vet ./...`, `go test ./...`, `make check` verdes; `git diff --stat -- cmd internal migrations go.mod go.sum Makefile` vazio.
  V1. Build: `docker build -t biblioteca-bairro:del012 .` termina com sucesso; as linhas FROM do Dockerfile têm `@sha256:` e tag com patch exato (grep `^FROM` no Dockerfile, sem `latest` nem `X.Y` sem patch).
  V2. Contexto de build: o .dockerignore é allowlist. Prova: um estágio temporário ou `docker build --no-cache --progress=plain` com `RUN ls -la /src` NÃO entra no Dockerfile commitado; alternativa sem editar: `docker build -f - . <<'EOF'` com `FROM <builder@digest>` + `COPY . /ctx` + `RUN find /ctx -maxdepth 2 | sort` e conferir que só aparecem go.mod, go.sum, cmd/, internal/, migrations/ (sem .git, .claude, docs-ai, bin/, .env, nem o binário `biblioteca-bairro` da raiz, que existe localmente e está no .gitignore:4).
  V3. Usuário e entrypoint: `docker inspect -f '{{.Config.User}} {{json .Config.Entrypoint}} {{json .Config.Cmd}} {{json .Config.Env}}' biblioteca-bairro:del012` → User `65532:65532`, Entrypoint `["/biblioteca-bairro"]`, Cmd null/vazio, Env sem DATABASE_URL, BIBLIOTECA_API_TOKEN, ADDR, BIBLIOTECA_DB_MAX_CONNS. `docker inspect -f '{{json .Config.Healthcheck}}'` → null.
  V4. Contrato de argumentos: `docker run --rm biblioteca-bairro:del012 foo; echo $?` → 2 (cmd/biblioteca-bairro/migrate.go:67).
  V5. Fuso sem tzdata do sistema: a imagem distroless static não tem /usr/share/zoneinfo garantido; subir a API (V7) sem panic "carregar fuso"/"carregar America/Sao_Paulo" nos logs prova a tzdata embutida.
  V6. Migração: Postgres 16 local descartável em rede docker própria (`docker network create`), DSN via `--env-file` de arquivo ignorado pelo git; `docker run --rm --network <rede> --env-file <arq> biblioteca-bairro:del012 migrate up` → código 0; em seguida `... migrate status` → código 0 e lista as migrações aplicadas.
  V7. API: `docker run -d --name api --network <rede> --env-file <arq> -p 8080:8080 --stop-timeout 25 biblioteca-bairro:del012`; `curl -s -o /dev/null -w '%{http_code}' localhost:8080/healthz` → 200 e `/readyz` → 200 (internal/http/handler.go:114-128); `docker top api` ou `docker exec` indisponível (sem shell) — confirmar PID 1 = /biblioteca-bairro via `docker inspect -f '{{.State.Pid}}' api` + `ps -o args= -p <pid>` no host.
  V8. Shutdown limpo: `time docker stop -t 25 api`; depois `docker inspect -f '{{.State.ExitCode}} {{.State.OOMKilled}}' api` → código 0 (não 137) e logs (`docker logs api`) com a mensagem de encerramento do servidor; tempo bem abaixo de 25 s com a API ociosa. Registrar também que `docker stop` com timeout padrão de 10 s é menor que prazoShutdown de 20 s (cmd/biblioteca-bairro/main.go:25), motivo do `-t 25` no README.
  V9. Segredos fora das camadas: `docker history --no-trunc biblioteca-bairro:del012 | grep -Ei 'token|database_url|password|senha'` sem resultado (exceto nomes em texto que não sejam valores; anotar se houver).
  V10. Opcional: extrair o binário (`docker create` + `docker cp`) e `govulncheck -mode=binary <binario>`; e `go version -m <binario>` mostrando a versão Go stable do builder, `-trimpath=true`, `CGO_ENABLED=0` e `-tags=timetzdata`.
  Limpeza: remover containers, rede e o arquivo de env temporário; o arquivo de env fica fora do repositório (scratchpad), nunca no diff.

CONFIRMADO:
  - `_ "time/tzdata"` em internal/domain/multa.go:7 e internal/storage/repositorio.go:9 (grep `time/tzdata` em cmd internal migrations); panic em multa.go:21-27 e repositorio.go:44-49 só ocorreria sem esse import.
  - `go test ./...` no estado atual: todos os pacotes ok (cmd/biblioteca-bairro, internal/config, internal/domain, internal/http, internal/http/middleware, internal/migrate, internal/storage, migrations). Nenhum teste novo, então não há falha esperada a registrar.
  - Código 2 em cmd/biblioteca-bairro/migrate.go:67; prazoShutdown = 20 s em cmd/biblioteca-bairro/main.go:25; NotifyContext SIGINT/SIGTERM em main.go:82; despacho de os.Args[1:] em main.go:61.
  - protected_paths inclui cmd/, internal/, migrations/, go.mod, go.sum, Makefile (.pipeline-claude.json:16-22).
  - Binário local /home/rodrigo/workspace/biblioteca-bairro/biblioteca-bairro existe e é ignorado (`git check-ignore -v` → .gitignore:4); a allowlist do .dockerignore o exclui.
  - Mutantes: não aplicado — não há código Go nem teste novo; a entrega é de configuração de imagem e documentação.
  - Fuzz: não aplicado — sem parsing/validação nova.

ASSUNCOES:
  - Assumo que o Docker desta máquina tem rede para os pulls e para o Postgres 16 local (B2 = a, segundo a sessão principal).
  - Assumo que o log de encerramento do servidor é identificável em `docker logs` (não conferi o texto exato da mensagem; o dev registra a linha real).
  - A obtenção do PID no host (V7) depende do Docker Engine rodar no mesmo kernel (WSL2/Docker Desktop pode não expor o PID; nesse caso registrar e usar `docker inspect -f '{{.Path}} {{json .Args}}'`).

ARQUIVOS:
  - nenhum — nenhum teste escrito (justificativa em DECISAO_DE_TESTE).

RISCOS:
  - A imagem não é construída no CI: V1–V9 só valem para este commit; mudança futura que quebre o Dockerfile não é pega por gate.
  - Se o code-reviewer ou o dev acreditar que `-tags timetzdata` é a única garantia de fuso, pode remover o import time/tzdata no futuro achando-o redundante (ou vice-versa); a garantia real hoje é o import.
  - V2 depende de um build temporário fora do Dockerfile commitado; se feito editando o Dockerfile, o estágio de inspeção pode vazar para o diff.

SINAIS:
  SECURITY: sim — imagem de execução (não root, segredos fora das camadas, PID 1 e sinais); V3 e V9 cobrem a evidência; require_security ligada.
  SUPPLY_CHAIN: sim — duas imagens base de terceiros por digest fora do go.mod; V1 confere a pinagem; go.mod/go.sum não mudam.
  MIGRATION: nao — sem migração nova; V6 só exercita `migrate up|status` existentes.
  PERFORMANCE: nao — nenhum caminho de consulta ou HTTP alterado.

PROXIMO: dev-implementation
MOTIVO: sem teste Go viável; o dev escreve Dockerfile e .dockerignore conforme os handoffs 02 e 03 do architecture-guardrails, obtém os digests e executa/registra os critérios V0–V9 (V10 opcional) no report.
