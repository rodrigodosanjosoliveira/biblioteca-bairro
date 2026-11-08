DELIVERY_ID: DEL-012
TASK_CODE: DEL-012
AGENT: security-review
STATUS: concluido

RESULTADO:
  Revisão de segurança da superfície exposta pela DEL-012 (imagem Docker e documentação de execução). O diff não tem código Go, nem workflow de CI, nem mudança em go.mod/go.sum. Nenhum achado CRITICO, ALTO ou MEDIO. Ficam três achados BAIXO, só de documentação/operação e não bloqueantes. Resultado para a seção Security do report: aprovado, sem achado bloqueante; govulncheck sem vulnerabilidades.

SUPERFICIE_REVISADA:
  - Dockerfile (não rastreado, 44 linhas, lido inteiro)
  - .dockerignore (não rastreado, lido inteiro)
  - README.md (seção "Executar a API", +107 linhas do pipeline_diff)
  - docs-ai/RUNBOOK-MIGRACAO.md (§10, trecho do ENTRYPOINT)
  - Conferências de apoio, só leitura: .gitignore:17-19; migrations/migrations.go:8,15 (go:embed); cmd/biblioteca-bairro/main.go:25,41,82,147 (prazo de shutdown, sinal, mensagem de DSN); .pipeline-claude.json:41-58
  - Fora do escopo: docs-ai/deliveries/** (handoffs, report, INDEX, status do DEL-011), que são metadados sem superfície.

GOVULNCHECK:
  pipeline_sensors names=["vuln"] (go run golang.org/x/vuln/cmd/govulncheck@v1.8.0 ./...):
    ok: true, exit_code: 0, duration_ms: 4733
    output: "No vulnerabilities found."

INSPECIONADO (por item do procedimento):
  - Autenticação e autorização: nenhuma rota nem middleware no diff. O README documenta /healthz e /readyz sem autenticação, como já era antes (não é mudança desta delivery). BIBLIOTECA_API_TOKEN é obrigatório e gerado com `openssl rand -hex 32` (64 caracteres, dentro de 32 a 512).
  - Criptografia/TLS: o exemplo de produção (`go run`) usa `sslmode=verify-full`. `sslmode=disable` fica restrito ao Postgres da rede Docker local e ao banco de teste, com aviso explícito (README "sslmode=disable só vale..."). A distroless static traz certificados de CA, então o verify-full funciona na imagem.
  - Segredos fora das camadas: o Dockerfile não tem ARG. Os ENV são só de toolchain (CGO_ENABLED, GOTOOLCHAIN, GOFLAGS). O .dockerignore é uma allowlist (`*` com go.mod, go.sum, cmd/, internal/, migrations/), então .env, .env.postgres, .git e docs-ai não entram no contexto de build. O estágio final copia só o binário (Dockerfile:38). Os arquivos *_test.go entram só no estágio build, têm credenciais fictícias (SENHA-DSN) e não chegam à imagem final.
  - Usuário não root: base `distroless/static-debian13:nonroot` mais `USER 65532:65532` numérico (Dockerfile:40). Porta 8080 não privilegiada. Sem shell na imagem.
  - PID 1 e sinais: `ENTRYPOINT ["/biblioteca-bairro"]` em forma exec (Dockerfile:44), sem wrapper de shell, então o binário recebe o SIGTERM direto. main.go:82 usa signal.NotifyContext(SIGINT, SIGTERM) e o prazo de shutdown é 20 s (main.go:25). `--stop-timeout 25` e `docker stop -t 25` no README cobrem esse prazo. O binário Go como PID 1 não precisa colher zumbis porque não cria subprocessos.
  - Flags de build: `-trimpath` (sem caminho do builder no binário, então stack/erro não vaza /src), `-buildvcs=false`, `CGO_ENABLED=0` (estático), `GOFLAGS=-mod=readonly`, `go mod verify` antes do build. Sem `-s -w`, de propósito, para govulncheck -mode=binary: aceitável.
  - Migrações: embutidas por go:embed (migrations/migrations.go:8,15), então a imagem não lê SQL do filesystem em tempo de execução.
  - Entrada externa e SQL: nenhuma mudança em código.
  - Erro: a mensagem de DSN inválida é fixa ("config: DATABASE_URL inválida", main.go:41/147, com testes de não vazamento em cmd/). Nada no diff altera isso.
  - Credenciais no README: nenhum segredo literal. A senha e o token vêm de `openssl rand`. O .env e o .env.postgres são criados num subshell com `umask 077` (0600), passados por `--env-file` (fora do argv do `docker run`, então não aparecem no `ps` do host), cobertos por .gitignore:17-18 e apagados na limpeza. `printf` é builtin, então a senha não vai para o argv de um processo externo. A variável `senha` leva `unset`.
  - Exposição de rede: API publicada só em 127.0.0.1:8080. O Postgres da API não publica porta. O Postgres de teste publica só em 127.0.0.1:5433.
  - CI: nenhum arquivo em .github/workflows no diff.

ACHADOS:
  - BAIXO B-S1 [README.md, roteiro "Testes com banco"]: o Postgres de teste com `POSTGRES_HOST_AUTH_METHOD=trust` em 127.0.0.1:5433 aceita superusuário sem senha de qualquer processo/usuário local do host enquanto o container estiver de pé, e o roteiro não traz limpeza (é o mesmo ponto da S6 do code-reviewer). Mitigação: adicionar `docker rm -f biblioteca-pg-teste` ao fim do bloco. O aviso "nunca no banco da API" já está lá.
  - BAIXO B-S2 [README.md, roteiro "Com go run"]: `export DATABASE_URL='postgres://<usuário>:<senha>@...'` leva o operador a digitar a senha real numa linha que vai para o histórico da shell. Sugestão: ler de um arquivo/gerenciador de segredos ou citar `read -rs` (ou espaço inicial com HISTCONTROL=ignorespace). Placeholder, sem segredo real no repositório.
  - BAIXO B-S3 [processo, .pipeline-claude.json:41-53]: Dockerfile e .dockerignore não estão em security_surface_paths. Uma mudança futura (ex.: trocar USER, adicionar ARG com segredo, afrouxar a allowlist) não forçaria require_security. Sugestão ao humano: incluir "Dockerfile" e ".dockerignore" (o arquivo está em no_agent_paths e a mudança é feita fora da sessão).
  - INFORMATIVO: `docker inspect` mostra as variáveis do --env-file a quem tem acesso ao daemon Docker. É inerente ao modelo e equivale a root no host, então não é achado.

BLOQUEIOS:
  nenhum

ASSUNCOES:
  - Os digests e a procedência das imagens base (golang:1.27.3-trixie, distroless static-debian13:nonroot, postgres:16-alpine) e o cosign não verificado ficam com o supply-chain-review, que roda em paralelo. Não avaliei esses itens aqui.
  - Não construí a imagem nem executei os roteiros: a revisão é estática, e a reprodução dinâmica está no handoff 07 e será repetida pelo QA.

ARQUIVOS:
  - nenhum (revisor sem escrita)

RISCOS:
  - A imagem não é construída nem escaneada no CI (risco já registrado no handoff 06/08). Um govulncheck -mode=binary ou scanner de imagem no CI fecharia a lacuna.
  - B-S3: Dockerfile fora de security_surface_paths.

SINAIS:
  SECURITY: sim — revisado; só achados BAIXO, sem bloqueio.
  SUPPLY_CHAIN: sim — digests das imagens base e cosign com o supply-chain-review.
  MIGRATION: nao
  PERFORMANCE: nao

PROXIMO: supply-chain-review
MOTIVO: Sem achado CRITICO/ALTO. SUPPLY_CHAIN: sim leva ao supply-chain-review (já em paralelo) e depois ao qa-validation. B-S1/B-S2 podem entrar com S6/S7 num ciclo opcional de dev-implementation. B-S3 é sugestão ao humano.
