DELIVERY_ID: DEL-012
TASK_CODE: DEL-012
AGENT: dev-implementation
STATUS: concluido

RESULTADO:
  Ciclo curto de correção, só no README.md, seção "Com `go run`", com S8 e S9 (handoff 12) e B-S4 (handoff 13). Não toquei em cmd/, internal/, migrations/, go.mod, go.sum, Dockerfile, .dockerignore, Makefile, .pipeline-claude.lock nem .claude/.
  - S8/B-S4 [README.md:61-73]: a linha do `read` ficou sozinha num bloco (README.md:66-68). O texto pede "Rode **só esta linha**" e explica, em README.md:70-73, que o `read` consome a próxima linha do terminal e que, colada junto com os comandos, ela leria um deles como DSN. Os comandos `export`/`go run` foram para um segundo bloco (README.md:75-81), precedido de "Depois de informar a DSN, rode o restante".
  - S9 [README.md:66, 70]: o bloco do `read -rs` agora é ```bash, e o texto diz "funciona em bash e zsh, não em `sh`/`dash`". O segundo bloco continua ```sh porque só usa `export` e `$(...)`, que são POSIX.
  - A dica `unset DATABASE_URL BIBLIOTECA_API_TOKEN` foi para depois do segundo bloco (README.md:83-84), sem mudar o conteúdo.

BLOQUEIOS:
  nenhum

CONFIRMADO:
  Execução em 2026-11-06, com bash 5.2.21 e zsh 5.9. Os scripts estão no scratchpad /tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/: colagem.sh, gorun-bloco1.sh e gorun-bloco2.sh.
  Estado inicial: `docker ps -a` vazio, nenhum .env ou .env.postgres, nenhum processo da API, porta 18080 livre.

  1) Os blocos foram extraídos do README por awk, entre "### Com `go run`" e "### Com Docker", um arquivo por cerca:
     gorun-bloco1.sh:
       printf 'DATABASE_URL: '; IFS= read -rs DATABASE_URL; echo
     gorun-bloco2.sh:
       export DATABASE_URL
       export BIBLIOTECA_API_TOKEN="$(openssl rand -hex 32)"
       go run ./cmd/biblioteca-bairro migrate up
       go run ./cmd/biblioteca-bairro migrate status
       go run ./cmd/biblioteca-bairro

  2) Simulei a colagem em sequência no pior caso, sem bracketed paste. O stdin de uma shell interativa recebeu, nesta ordem, o bloco 1, a DSN na linha seguinte (o que a pessoa cola no prompt) e o bloco 2 inteiro. Usei `bash --norc -i` e `zsh -f -i`, com HISTFILE no scratchpad.
     Banco: PostgreSQL descartável `biblioteca-pg-gorun`, na mesma imagem por digest, em 127.0.0.1:5434, com senha de `openssl rand -hex 16` em arquivo 0600 no scratchpad.
     Precisei de três ajustes fora dos blocos. A DSN colada usa `sslmode=disable`, porque o banco local não tem TLS. Pus `ADDR=127.0.0.1:18080` para a API não escutar em todas as interfaces. Um vigia mediu /healthz e /readyz e mandou SIGINT ao binário da API.
     bash (senha mascarada na exibição):
       bash-5.2$ printf 'DATABASE_URL: '; IFS= read -rs DATABASE_URL; echo
       DATABASE_URL:
       bash-5.2$ export DATABASE_URL
       bash-5.2$ export BIBLIOTECA_API_TOKEN="$(openssl rand -hex 32)"
       bash-5.2$ go run ./cmd/biblioteca-bairro migrate up
       {"level":"INFO","msg":"lock adquirido"}
       {"level":"INFO","msg":"migração aplicada","versao":1,"nome":"esquema_emprestimo","duracao_ms":39}
       {"level":"INFO","msg":"migração aplicada","versao":2,"nome":"renovacao_emprestimo","duracao_ms":4}
       migrate up: 2 migração(ões) aplicada(s)
       bash-5.2$ go run ./cmd/biblioteca-bairro migrate status
       0001 esquema_emprestimo aplicada 2026-11-06T20:46:36Z
       0002 renovacao_emprestimo aplicada 2026-11-06T20:46:37Z
       bash-5.2$ go run ./cmd/biblioteca-bairro
       {"level":"INFO","msg":"biblioteca-bairro ouvindo","addr":"127.0.0.1:18080"}
       {"msg":"requisicao","rota":"GET /healthz","status":200,...} (x2)
       {"msg":"requisicao","rota":"GET /readyz","status":200,...}
       {"level":"INFO","msg":"biblioteca-bairro encerrado"}
       bash-5.2$ echo "len_dsn=${#DATABASE_URL} len_token=${#BIBLIOTECA_API_TOKEN}"
       len_dsn=92 len_token=64
       rc_shell=0 / vigia: healthz: 200, readyz: 200
       Histórico gravado (HISTFILE), na íntegra:
         export HISTFILE=... HISTSIZE=100 HISTFILESIZE=100
         export ADDR=127.0.0.1:18080
         cd /home/rodrigo/workspace/biblioteca-bairro
         printf 'DATABASE_URL: '; IFS= read -rs DATABASE_URL; echo
         export DATABASE_URL
         export BIBLIOTECA_API_TOKEN="$(openssl rand -hex 32)"
         go run ./cmd/biblioteca-bairro migrate up
         go run ./cmd/biblioteca-bairro migrate status
         go run ./cmd/biblioteca-bairro
         echo "len_dsn=${#DATABASE_URL} len_token=${#BIBLIOTECA_API_TOKEN}"
         exit
       senha na saida: 0
       senha no historico: 0
     zsh, no mesmo banco, por isso o migrate up não tinha pendência:
       DATABASE_URL:
       {"level":"INFO","msg":"lock adquirido"}
       migrate up: nenhuma migração pendente
       0001 esquema_emprestimo aplicada 2026-11-06T20:46:36Z
       0002 renovacao_emprestimo aplicada 2026-11-06T20:46:37Z
       {"level":"INFO","msg":"biblioteca-bairro ouvindo","addr":"127.0.0.1:18080"}
       {"msg":"requisicao","rota":"GET /healthz","status":200,...} (x2)
       {"msg":"requisicao","rota":"GET /readyz","status":200,...}
       {"level":"INFO","msg":"biblioteca-bairro encerrado"}
       len_dsn=92 len_token=64
       rc_shell=0 / vigia: healthz: 200, readyz: 200
       O histórico gravado tem as mesmas linhas do bash (bloco 1, bloco 2, echo, exit), sem a DSN.
       senha na saida: 0
       senha no historico: 0
     Nas duas shells, a DSN colada na linha seguinte ao bloco 1 foi consumida pelo `read` (len_dsn=92) e não entrou no histórico. O bloco 2, colado depois, rodou inteiro.

  3) Contraprova do motivo da separação, sem segredo. Colei o bloco 1 e a primeira linha do bloco 2 juntos, como no layout anterior, em `bash --norc -i`:
       bash-5.2$ printf 'DATABASE_URL: '; IFS= read -rs DATABASE_URL; echo
       DATABASE_URL:
       bash-5.2$ echo "DATABASE_URL lida=[$DATABASE_URL]"
       DATABASE_URL lida=[export DATABASE_URL]
     Isso reproduz o B-S4 sem bracketed paste: o `read` engole o `export DATABASE_URL`. Com o bloco do `read` sozinho, a próxima linha é a DSN que a pessoa cola.

  4) Limpeza, só do que criei e por nome:
       docker rm -f biblioteca-pg-gorun   -> biblioteca-pg-gorun
       rm -f dos arquivos com segredo ou saída no scratchpad: dsn.txt, pg-gorun.env, colar-bash.txt, colar-zsh.txt, colar-junto.txt, colagem-bash.out, colagem-zsh.out, hist-bash, hist-zsh, vigia-bash.out, vigia-zsh.out
       Conferência: "(nenhum container biblioteca*)", "(portas 5434/18080 livres)", "(nenhum processo da API)"
     Não usei docker prune, system prune nem go clean. Ficam no scratchpad, como evidência, só os scripts, sem segredo.
     Incidente da execução: no bash, o vigia procurou o binário em '/exe/biblioteca-bairro$', mas o go run atual executa a partir de ~/.cache/go-build/.../biblioteca-bairro, e o SIGINT não saiu. Confirmei o PID pelo pai (go run 1432429, filho do bash --norc -i 1432327) e mandei `kill -INT 1432470` só a ele. O servidor encerrou limpo ("biblioteca-bairro encerrado"). Corrigi o padrão do vigia antes da rodada no zsh, que parou sozinha.

  5) Sensores (pipeline_sensors, todos): ok: true
       vet:        ok, exit 0, saída vazia
       test:       ok, exit 0, "pacotes: 8 (8 ok, 0 com falha, 0 sem testes) / testes: 1580 passaram, 0 falharam, 200 pulados" (os pulados são testes de banco sem BIBLIOTECA_TEST_DATABASE_URL)
       build:      ok, exit 0, saída vazia
       vuln:       ok, exit 0, "No vulnerabilities found."
       mod_verify: ok, exit 0, "all modules verified"
     Fuzz: nenhum alvo tocado, porque não há código Go no diff.

  6) git status depois do ciclo, igual ao inicial: M .claude/agents/dev-implementation.md, M .claude/agents/test-writer.md, M .pipeline-claude.lock, M Makefile, M README.md, M docs-ai/RUNBOOK-MIGRACAO.md, M docs-ai/deliveries/DEL-011/report.md, M docs-ai/deliveries/INDEX.md, ?? .dockerignore, ?? Dockerfile, ?? docs-ai/deliveries/DEL-012/. Este ciclo só alterou o README.md.

ASSUNCOES:
  - A simulação pelo stdin de `bash -i`/`zsh -i` equivale a colar num terminal sem bracketed paste, o pior caso do S8/B-S4. Não medi o eco em TTY real: `-s` só tem efeito com TTY.
  - A DSN do teste usou sslmode=disable porque o banco local não tem TLS. O README continua indicando verify-full.
  - O segundo bloco fica com ```sh porque não tem construção fora do POSIX. A S9 pedia `bash` só onde há `read -s`.

ARQUIVOS:
  - README.md (seção "Com `go run`", linhas 59-84)

RISCOS:
  - Quem ignorar o "Rode só esta linha" e colar os dois blocos de uma vez num terminal sem bracketed paste ainda cai no B-S4. O texto em README.md:70-72 avisa disso.
  - Observação fora do escopo, não investigada: na rodada bash, o horário das linhas "requisicao" (17:46:36.69) ficou antes do "ouvindo" (17:46:37.53) do mesmo processo. Na rodada zsh a ordem saiu coerente. Pode ser ajuste de relógio do WSL2. Não muda nenhum resultado.
  - Riscos anteriores mantidos: os roteiros do README não são exercitados pelo CI, o cosign não é verificado e o B-S3 está com o usuário. A atualização do pipeline-claude (Makefile, .pipeline-claude.lock, .claude/) segue no working tree e fica fora desta entrega, por decisão do usuário.

SINAIS:
  SECURITY: sim. O roteiro de credenciais foi alterado: o `read` da DSN ficou isolado e protege contra o B-S4. require_security já está ligado.
  SUPPLY_CHAIN: nao. Nenhuma dependência nem imagem alterada.
  MIGRATION: nao
  PERFORMANCE: nao

PROXIMO: code-reviewer
MOTIVO: S8, S9 e B-S4 aplicadas e reproduzidas com a saída real da colagem em sequência no bash e no zsh, com a contraprova do caso antigo. Sensores verdes. O code-reviewer confirma S8 e S9. Como o roteiro de credenciais mudou, o security-review confirma o B-S4. Depois vêm qa-validation e docs-update.
