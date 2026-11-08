DELIVERY_ID: DEL-012
TASK_CODE: DEL-012
AGENT: pm-triage
STATUS: ok

TIPO: chore (build/empacotamento) com parte docs (README de execução)

ESCOPO:
  - Dockerfile novo na raiz: imagem do binário cmd/biblioteca-bairro, que serve a API sem argumentos e roda `migrate up` / `migrate status` como subcomando (mesma imagem, comando diferente).
  - .dockerignore novo (não levar .git, .claude, .pipeline-claude*, docs-ai, bin/ etc. para o contexto de build).
  - README.md: seção de execução local: variáveis de ambiente (DATABASE_URL, BIBLIOTECA_API_TOKEN 32..512 bytes ASCII visível, ADDR padrão :8080, BIBLIOTECA_DB_MAX_CONNS 1..100 padrão 10, BIBLIOTECA_MIGRATE_LOCK_TIMEOUT, BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT), sempre por ambiente e sem segredo literal; PostgreSQL; ordem `migrate up` antes de subir a API (D-16, D-17); uso via `go run` e via container; como rodar os testes com banco (Postgres com `trust`, exigido por internal/migrate, D-13 e D-17; BIBLIOTECA_TEST_DATABASE_URL; BIBLIOTECA_TEST_DB_OBRIGATORIO=1).
  - docs-ai/RUNBOOK-MIGRACAO.md §10 diz "o repositório não tem Dockerfile": atualizar (docs-update).
  - docs-ai/deliveries/INDEX.md e docs-ai/deliveries/DEL-011/report.md: marcação merged da DEL-011 feita pelo pipeline_index_sync, já no working tree; entra no commit desta entrega (como a DEL-010 entrou na DEL-011).

FORA_DE_ESCOPO:
  - Qualquer mudança em cmd/, internal/, migrations/, go.mod, go.sum e Makefile (nenhum código Go muda; se o Dockerfile exigir mudança de código, volta ao triage).
  - .github/workflows/ (build ou scan da imagem no CI).
  - docker-compose (ver ASSUNCOES), publicação em registry, tags/versionamento de imagem, Kubernetes/Helm.
  - HEALTHCHECK no Dockerfile que dependa de shell/curl na imagem final.
  - .gitattributes (sugestão antiga do RUNBOOK §9, não pedida).
  - Mudança em .pipeline-claude.json (fora da sessão).

CONFIRMADO:
  - Não existe Dockerfile, .dockerignore nem compose no repositório (Glob na raiz); docs-ai/RUNBOOK-MIGRACAO.md:120 confirma "o repositório não tem Dockerfile".
  - README.md atual (43 linhas) só descreve o pipeline-claude, sem seção de execução da API.
  - go.mod: `go 1.25.0` (imagem de build precisa de Go 1.25+).
  - Variáveis lidas pelo binário: internal/config/config.go:59-71 (DATABASE_URL, BIBLIOTECA_API_TOKEN, ADDR, BIBLIOTECA_DB_MAX_CONNS) e internal/config/migracao.go:50-58 (DATABASE_URL, BIBLIOTECA_MIGRATE_LOCK_TIMEOUT, BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT).
  - Subcomando: docs-ai/03-MAPA-RAPIDO-MODULOS.md (cmd/biblioteca-bairro/migrate.go; argumento desconhecido sai com código 2, RUNBOOK §10), então o ENTRYPOINT não pode injetar flags.
  - Testes com banco: BIBLIOTECA_TEST_DB_OBRIGATORIO em internal/http/banco_test.go:30, internal/storage/banco_test.go:22, internal/migrate/apoio_test.go:53, cmd/biblioteca-bairro/migrate_test.go:636; CI em .github/workflows/delivery.yml:21-52 (POSTGRES_HOST_AUTH_METHOD trust, Postgres 16 por digest, D-13 em docs-ai/DECISOES.md:182); exigência de trust em docs-ai/DECISOES.md:617.
  - Dockerfile, .dockerignore e README.md não estão em protected_paths nem em security_surface_paths (.pipeline-claude.json); o validador não força flag, a require_security foi ligada à mão.
  - Delivery criada com pipeline_delivery_new: DEL-012, flags [require_security].

ASSUNCOES:
  - Decisões humanas pendentes, com padrão proposto para o architecture-guardrails confirmar ou levar ao usuário:
    a) Imagem base: proposta de multi-stage com builder golang:1.25 e final distroless static nonroot (ou scratch com CA certs), ambas fixadas por digest como no CI (D-13). Alpine só se o usuário quiser shell na imagem.
    b) docker-compose: NÃO entra; o README usa `docker run` de um postgres:16 local. Se o usuário quiser compose, é ampliação de escopo.
    c) Build estático (CGO_ENABLED=0, -trimpath); o projeto não usa cgo (supor, a confirmar no guardrails).
  - ENTRYPOINT = binário, CMD vazio (servidor); migração por `docker run <img> migrate up`. Usuário não root na imagem final.
  - Nenhum segredo em ENV/ARG do Dockerfile nem em exemplo literal do README: exemplos usam placeholder ou geração (ex.: `openssl rand`), e o DSN do README é de banco local descartável.
  - O README pode ser em português, mantendo o texto atual sobre o pipeline-claude e acrescentando a seção de execução.
  - Testes com banco descritos no README rodam fora do container da API (go test no host contra o Postgres trust); não se cria imagem de teste.
  - A marcação merged da DEL-011 está correta no working tree (não revisei o conteúdo do diff).
  - Editor: Dockerfile/.dockerignore pelo dev-implementation; README e RUNBOOK pelo docs-update. test-writer provavelmente sem trabalho (não há código Go); se houver teste viável (ex.: build da imagem), não é gate do projeto.

ARQUIVOS:
  - docs-ai/deliveries/DEL-012/report.md — criado pela ferramenta (escopo e require_security)
  - docs-ai/deliveries/INDEX.md — linha da DEL-012 registrada pela ferramenta

RISCOS:
  - Imagem base sem digest ou tag flutuante (golang:latest) quebra reprodutibilidade e abre superfície de supply chain fora do go.sum, que o govulncheck não cobre.
  - Token ou DSN com senha como ENV/ARG no Dockerfile ficaria em camada da imagem (docker history); exemplo de README com token "real" seria copiado para produção.
  - ENTRYPOINT que passe flags ou wrapper de shell muda o contrato de argumentos (código 2) e pode impedir o SIGTERM de chegar ao binário (shutdown gracioso de 20s e cancelamento do migrate, RUNBOOK §8).
  - Contexto de build sem .dockerignore leva .git, .claude e arquivos locais para a imagem de build.
  - README pode sugerir `trust` fora do contexto de teste; precisa deixar claro que trust é só para banco descartável local/CI.
  - Imagem distroless não tem shell: HEALTHCHECK do Docker não funciona sem utilitário; README deve apontar /healthz e /readyz para o orquestrador.

SINAIS:
  SECURITY: sim — imagem de execução (usuário, segredos fora das camadas, sinais), documentação de credenciais e do modo trust; Dockerfile não está em security_surface_paths, flag require_security ligada à mão.
  SUPPLY_CHAIN: sim — imagens base de terceiros (builder e runtime) são dependência nova fora do go.mod; pinagem por digest a revisar. go.mod/go.sum não mudam.
  MIGRATION: nao — nenhuma migração nova; só documenta e empacota o `migrate up` existente.
  PERFORMANCE: nao — sem consulta nem caminho quente alterado.

PROXIMO: architecture-guardrails
MOTIVO: tipo chore com artefato novo (Dockerfile) e decisões de desenho pendentes (imagem base, usuário, ENTRYPOINT, compose); o guardrails fecha essas escolhas ou as leva ao usuário antes do dev-implementation e do docs-update.
