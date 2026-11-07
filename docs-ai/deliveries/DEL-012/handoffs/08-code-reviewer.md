DELIVERY_ID: DEL-012
TASK_CODE: DEL-012
AGENT: code-reviewer
STATUS: concluido

RESULTADO:
  Rodada de confirmação do ciclo de correção (handoff 06 -> 07). B1 e B2 caíram: os dois roteiros do README esperam o PostgreSQL aceitar conexão por TCP antes do `migrate up`/API e antes do `go test`. S1 a S5 foram aplicadas como sugerido. Reli o diff inteiro (README, RUNBOOK, INDEX, DEL-011/report) e os arquivos não rastreados Dockerfile e .dockerignore, e não achei problema novo. Ficam só duas sugestões menores no roteiro de testes.

BLOQUEIOS:
  nenhum

SUGESTOES:
  S6. [README.md:135-143]: o roteiro de testes com banco não tem limpeza (`docker rm -f biblioteca-pg-teste`), ao contrário do roteiro Docker (README.md:110-116). Ao rodar de novo, o `docker run --name biblioteca-pg-teste` falha com nome em uso. Uma linha de limpeza depois do bloco resolve.
  S7. [README.md:140-141]: os `export` de BIBLIOTECA_TEST_DATABASE_URL e BIBLIOTECA_TEST_DB_OBRIGATORIO continuam na shell depois do exemplo. Com o container removido, um `go test ./...` posterior na mesma shell reprova em vez de pular. Prefixar o comando (`BIBLIOTECA_TEST_DATABASE_URL=... BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test ./...`) evita o efeito residual, como foi feito com a umask na S1.

CONFIRMADO:
  - B1 caiu. [README.md:93-94]: `until docker exec biblioteca-pg pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; do sleep 1; done` fica entre o `docker run` do Postgres (README.md:90-91) e o `migrate up` (README.md:96), com `-h 127.0.0.1`, então o servidor temporário do initdb, que só escuta no socket, não dá falso pronto. A explicação e o caminho de diagnóstico (`docker logs biblioteca-pg`) estão em README.md:103-106. O handoff 07 traz a reprodução antes (migrate e API com "banco indisponível", API Exited 1) e depois (2 migrações aplicadas, healthz/readyz 200), com os blocos extraídos literalmente do README.
  - B2 caiu. [README.md:139]: a mesma espera contra biblioteca-pg-teste vem antes dos `export` e do `go test`. A reprodução antes deu rc=1 com 10 testes de banco reprovados, e a de depois deu rc=0 nos 8 pacotes.
  - S1 aplicada. [README.md:83-86]: a criação de .env e .env.postgres roda no subshell `( umask 077; ... )`, então a umask da shell não muda (medido 0022 antes e depois, arquivos -rw-------). `senha` é herdada pelo subshell e apagada com `unset` (README.md:87).
  - S2 aplicada. [README.md:91] e [README.md:138]: `postgres:16-alpine@sha256:0cefaabd...31ab` é o mesmo digest de .github/workflows/delivery.yml:24 (conferido por grep), e README.md:76-77 diz que é a imagem do CI fixada por digest.
  - S3 aplicada. [README.md:108-116]: o bloco de limpeza para a API com `-t 25`, coerente com o prazo de shutdown, remove os containers e a rede e apaga .env e .env.postgres. Os dois arquivos também estão cobertos por .gitignore:17-18 (`.env`, `.env.*`).
  - S4 aplicada. [README.md:44]: "nenhuma credencial fica no código, na imagem nem no git."
  - S5 aplicada. [Dockerfile:28-32]: o comentário cita internal/domain/multa.go e internal/storage/repositorio.go, e os dois têm `_ "time/tzdata"` (multa.go:7, repositorio.go:9, conferido por grep). O comentário também avisa que o import não pode ser removido. As instruções do Dockerfile não mudaram em relação à rodada anterior: FROM com digest (linhas 13 e 36), ENV só de toolchain (17-19), `go mod download && go mod verify` (22), build com `-trimpath -buildvcs=false -tags timetzdata` (33-34), `USER 65532:65532` (40), `EXPOSE 8080` (42), ENTRYPOINT exec (44). Nenhum ENV/ARG com segredo.
  - .dockerignore:1-7 continua o mesmo, uma allowlist que deixa de fora .env e .env.postgres.
  - Sem problema novo no README. Não há segredo literal: a senha e o token vêm de `openssl rand`. `trust` e `sslmode=disable` continuam restritos aos bancos locais, com aviso (README.md:118-120, :145-146). As portas publicadas ficam em 127.0.0.1 (README.md:100, :136).
  - RUNBOOK-MIGRACAO.md, INDEX.md e DEL-011/report.md não mudaram desde a rodada anterior.
  - O diff não tem código Go: cmd/, internal/, migrations/, go.mod, go.sum e Makefile estão fora do pipeline_diff (14 arquivos). O handoff 07 registra os sensores verdes (vet, test, build, vuln, mod_verify).

ASSUNCOES:
  - Não reexecutei os roteiros. Conto com a reprodução antes/depois do handoff 07, que é consistente com o diagnóstico do handoff 06. O QA repete.
  - O `until` sem prazo é aceitável num exemplo interativo, e o README aponta o diagnóstico (ASSUNCAO do dev, que eu aceito).

ARQUIVOS:
  - nenhum (revisor sem escrita)

RISCOS:
  - Os riscos do handoff 06 continuam: imagem sem build no CI e roteiro do README não exercitado pelo CI, cosign da distroless não verificado, Dockerfile fora de security_surface_paths (sugestão ao humano).
  - O `go clean -testcache` global do dev (handoff 07, LIMPEZA) deve ir para o report. É inócuo, mas foi ação fora do pedido.

SINAIS:
  SECURITY: sim — imagem de execução (não root, PID 1, segredos fora das camadas) e documentação de credenciais (.env com umask 077 e limpeza) e do modo trust; require_security ligada.
  SUPPLY_CHAIN: sim — imagens base do Dockerfile por digest de índice, postgres:16-alpine do README fixado pelo digest do CI, cosign não verificado; go.mod/go.sum inalterados.
  MIGRATION: nao — sem migração nova.
  PERFORMANCE: nao — nenhum caminho de código alterado.

PROXIMO: security-review
MOTIVO: B1 e B2 resolvidos, sem bloqueio novo. SECURITY: sim leva ao security-review. Em seguida vêm supply-chain-review (SUPPLY_CHAIN: sim), qa-validation e docs-update. S6/S7 são opcionais e podem entrar num ciclo de dev se a sessão quiser.
