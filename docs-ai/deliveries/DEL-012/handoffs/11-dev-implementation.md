DELIVERY_ID: DEL-012
TASK_CODE: DEL-012
AGENT: dev-implementation
STATUS: concluido

RESULTADO:
  Ciclo curto de correção, só no README.md, com S6, S7 (handoff 08), B-S1 e B-S2 (handoff 09) aplicadas. B-S3 ficou fora, como pedido: é configuração do pipeline e cabe ao usuário. Não alterei cmd/, internal/, migrations/, go.mod, go.sum, Makefile, Dockerfile nem .dockerignore.
  - S6/B-S1 [README.md:152]: o bloco "Testes com banco" termina com `docker rm -f biblioteca-pg-teste`. O texto em README.md:143-144 diz que o bloco remove o banco e pode ser repetido, e README.md:159-160 liga a remoção ao `trust`.
  - S7 [README.md:149-151]: as variáveis de teste passam como prefixo do `go test` (`VAR=... \ VAR2=1 \ go test ./...`), sem `export`. A explicação de BIBLIOTECA_TEST_DB_OBRIGATORIO, que estava em comentário inline, foi para README.md:155-157 e avisa que um `go test ./...` posterior na mesma shell volta a pular os testes de banco.
  - B-S2 [README.md:62-75]: a DSN não é mais digitada numa linha de comando. É lida sem eco com `printf 'DATABASE_URL: '; IFS= read -rs DATABASE_URL; echo` e depois `export DATABASE_URL`. O prompt usa printf porque `read -p` não é portável (no zsh, -p é coprocesso). O formato esperado da DSN, com `sslmode=verify-full`, está no texto, e há a dica `unset DATABASE_URL BIBLIOTECA_API_TOKEN` para o fim.

BLOQUEIOS:
  nenhum

CONFIRMADO:
  Roteiros reexecutados em 2026-11-06. Os blocos foram extraídos literalmente do README por awk, entre o título da seção e o fecho ```. Os scripts estão no scratchpad: testes.sh, roda2x.zsh, testes_count1.sh e gorun.sh, em /tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/.
  Estado inicial: `docker ps -a` vazio, nenhuma rede biblioteca*, nenhum .env ou .env.postgres.

  1) "Testes com banco": o bloco rodou duas vezes seguidas na mesma shell zsh (S6, S7).
     Bloco extraído (testes.sh):
       docker run -d --name biblioteca-pg-teste -p 127.0.0.1:5433:5432 \
         -e POSTGRES_HOST_AUTH_METHOD=trust \
         postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab
       until docker exec biblioteca-pg-teste pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; do sleep 1; done
       BIBLIOTECA_TEST_DATABASE_URL='postgres://postgres@localhost:5433/postgres?sslmode=disable' \
         BIBLIOTECA_TEST_DB_OBRIGATORIO=1 \
         go test ./...
       docker rm -f biblioteca-pg-teste
     Saída (zsh roda2x.zsh):
       ### RODADA 1
       650ce0b0d87a38ead58d1daef8698937cb0326c0cf750742c1e39f56e575e578
       ok  .../cmd/biblioteca-bairro (cached)
       ok  .../internal/config (cached)
       ok  .../internal/domain (cached)
       ok  .../internal/http (cached)
       ok  .../internal/http/middleware (cached)
       ok  .../internal/migrate (cached)
       ok  .../internal/storage (cached)
       ok  .../migrations (cached)
       biblioteca-pg-teste
       rc_go_test_ultimo=0
       ### residuo na shell:
       (nenhuma variavel BIBLIOTECA_TEST no ambiente)
       (nenhum container biblioteca-pg-teste)
       ### RODADA 2
       d23663600b75eea4e798b7c3aee96a5254ffeebcd17faec8858fb0ad5e75b67f
       ok  (os mesmos 8 pacotes, cached)
       biblioteca-pg-teste
       ### residuo na shell:
       (nenhuma variavel BIBLIOTECA_TEST no ambiente)
       (nenhum container biblioteca-pg-teste)
       ### go test sem variaveis na mesma shell:
       ok  .../cmd/biblioteca-bairro 0.041s
       ok  .../internal/http 0.479s
       ok  .../internal/migrate 0.010s
       ok  .../internal/storage 0.007s
       (demais pacotes cached; os 8 ok)
     A segunda rodada criou um container novo, com outro ID, sem "name already in use", e as duas removeram o container ao final (S6 e B-S1). Depois de cada rodada não sobrou variável BIBLIOTECA_TEST* no ambiente, e o `go test ./...` posterior rodou de novo os pacotes de banco sem reprovar, porque pulou os testes de banco (S7).
     Os resultados das rodadas 1 e 2 vieram do cache do go test (mesmas variáveis do ciclo anterior). Para provar que os testes de banco rodam de fato contra o container, sem `go clean`, executei o bloco uma terceira vez trocando só a linha do go test por `go test -count=1 -v ./...` (testes_count1.sh):
       ccc64d96dda3ee784762d88e5a4cecd6fa3cdc0944fa87c17f4f0277ea1e2577
       rc_go_test=0
       biblioteca-pg-teste
       com banco: PASS=1908 SKIP=0 FAIL=0
       rc_sem_banco=0
       sem banco: PASS=1580 SKIP=200 FAIL=0
       (nenhum container biblioteca-pg-teste)

  2) "Com go run" (B-S2): o bloco gorun.sh foi extraído literalmente e rodado em zsh. A DSN entrou pelo stdin do `read`, como se fosse colada.
     Banco: PostgreSQL descartável `biblioteca-pg-gorun` (mesma imagem por digest), 127.0.0.1:5434, senha de `openssl rand -hex 16` em arquivo 0600 no scratchpad. Desvios necessários, fora do bloco: `sslmode=disable` na DSN colada, porque o banco local não tem TLS para verify-full, e `ADDR=127.0.0.1:18080` no ambiente para não escutar em todas as interfaces. A API foi parada com SIGINT no grupo de processos.
     Saída:
       healthz: 200
       readyz: 200
       rc_bloco=1   (go run interrompido pelo meu SIGINT; o servidor encerrou limpo, ver última linha)
       DATABASE_URL:
       {"level":"INFO","msg":"lock adquirido"}
       {"level":"INFO","msg":"migração aplicada","versao":1,"nome":"esquema_emprestimo","duracao_ms":23}
       {"level":"INFO","msg":"migração aplicada","versao":2,"nome":"renovacao_emprestimo","duracao_ms":3}
       migrate up: 2 migração(ões) aplicada(s)
       0001 esquema_emprestimo aplicada 2026-11-06T20:41:39Z
       0002 renovacao_emprestimo aplicada 2026-11-06T20:41:39Z
       {"level":"INFO","msg":"biblioteca-bairro ouvindo","addr":"127.0.0.1:18080"}
       {"msg":"requisicao","rota":"GET /healthz","status":200,...} (x2)
       {"msg":"requisicao","rota":"GET /readyz","status":200,...}
       {"level":"INFO","msg":"biblioteca-bairro encerrado"}
       --- senha aparece na saida? 0 ocorrencia(s)
     Histórico: colei as linhas do bloco e, na linha seguinte ao `read`, a DSN em shells interativas.
       bash --norc -i, com HISTFILE no scratchpad: len=92 (DSN inteira lida). O HISTFILE gravado tem só
         printf 'DATABASE_URL: '; IFS= read -rs DATABASE_URL; echo
         export DATABASE_URL
         echo "len=${#DATABASE_URL}"
         exit
         senha no historico bash: 0
       zsh -f -i, com histórico da sessão por `fc -ln 1`: len=92, e o histórico tem só
         HISTSIZE=100
         printf 'DATABASE_URL: '; IFS= read -rs DATABASE_URL; echo
         export DATABASE_URL
         echo "len=${#DATABASE_URL}"
         senha no historico zsh: 0
         senha em qualquer saida da zsh: 0

  3) Limpeza, só do que criei e por nome:
       docker rm -f biblioteca-pg-gorun   -> biblioteca-pg-gorun
       (biblioteca-pg-teste foi removido pelo próprio bloco nas 3 execuções)
       rm dos arquivos de segredo e logs com segredo no scratchpad (dsn.txt, pg-gorun.env, colar.txt, hist_bash, bash.out, zsh.out, zsh_hist.txt, gorun.log)
       Conferência: "(nenhum container biblioteca*)", "portas 5433/5434/18080 livres"
     Sem docker prune, system prune nem go clean. Os scripts e os logs com_banco.log e sem_banco.log, sem segredo, ficam no scratchpad como evidência.

  4) Sensores (pipeline_sensors, todos): ok: true
       vet:        ok, exit 0, saída vazia
       test:       ok, exit 0, "pacotes: 8 (8 ok, 0 com falha, 0 sem testes) / testes: 1580 passaram, 0 falharam, 200 pulados" (pulados = testes de banco sem BIBLIOTECA_TEST_DATABASE_URL)
       build:      ok, exit 0, saída vazia
       vuln:       ok, exit 0, "No vulnerabilities found."
       mod_verify: ok, exit 0, "all modules verified"
     Fuzz: nenhum alvo de fuzz tocado (sem código Go no diff).

  5) git status depois do ciclo: o mesmo conjunto de antes (M README.md, M RUNBOOK-MIGRACAO.md, M DEL-011/report.md, M INDEX.md, ?? .dockerignore, ?? Dockerfile, ?? DEL-012/). Este ciclo só mexeu no README.md.

ASSUNCOES:
  - "Literalmente, como quem copia e cola": rodei os blocos como script zsh, linha a linha, e o go run com a DSN no stdin. É equivalente a colar numa shell interativa, exceto pela digitação no prompt sem eco. O comportamento de não ecoar com `-s` só existe em TTY e não foi medido aqui. A ausência no histórico foi provada em bash -i e zsh -i.
  - A DSN do teste de go run usou sslmode=disable porque o banco local não tem TLS. O README continua indicando verify-full.

ARQUIVOS:
  - README.md (seções "Com `go run`" e "Testes com banco")

RISCOS:
  - `read -rs` lê uma única linha. Uma DSN colada com quebra de linha seria truncada, o que não é o caso de uma DSN normal.
  - Riscos anteriores mantidos: imagem e roteiros do README não exercitados pelo CI, cosign não verificado, B-S3 (Dockerfile e .dockerignore fora de security_surface_paths) com o usuário.

SINAIS:
  SECURITY: sim — documentação de credenciais alterada (DSN fora do histórico, banco trust removido ao fim); require_security já ligada.
  SUPPLY_CHAIN: nao — nenhuma dependência nova; go.mod/go.sum e imagens inalterados neste ciclo.
  MIGRATION: nao
  PERFORMANCE: nao

PROXIMO: code-reviewer
MOTIVO: S6, S7, B-S1 e B-S2 aplicadas e reproduzidas com saída real; sensores verdes. Cabe ao code-reviewer confirmar. Como o roteiro de credenciais mudou, o security-review pode confirmar B-S1 e B-S2, e depois vêm qa-validation e docs-update.
