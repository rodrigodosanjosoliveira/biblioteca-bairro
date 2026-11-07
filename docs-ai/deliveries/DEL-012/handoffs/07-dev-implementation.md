DELIVERY_ID: DEL-012
TASK_CODE: DEL-012
AGENT: dev-implementation
STATUS: concluido

RESULTADO:
  Ciclo de correção do handoff 06 (code-reviewer). B1 e B2 corrigidos no README: os dois roteiros esperam o PostgreSQL aceitar conexão por TCP (`pg_isready -h 127.0.0.1`) antes do `migrate up`/da API e antes do `go test`. S1 a S5 aplicadas. A corrida foi reproduzida antes da correção, e os blocos do README corrigido, executados literalmente, passam do começo ao fim, inclusive o bloco novo de limpeza. Sem mudança em código Go. O test-writer foi pulado por decisão da sessão principal: o defeito estava no roteiro de documentação.

CORRECOES:
  B1. README.md, roteiro Docker: `until docker exec biblioteca-pg pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; do sleep 1; done` entre o `docker run` do Postgres e o `migrate up`, mais um parágrafo explicando o `-h 127.0.0.1` (o servidor temporário do initdb só escuta no socket e daria falso pronto) e indicando `docker logs biblioteca-pg` se o laço não terminar.
  B2. README.md, roteiro de testes: a mesma espera (`docker exec biblioteca-pg-teste pg_isready -h 127.0.0.1 -U postgres`) antes do `export`/`go test`.
  S1. Criação de .env/.env.postgres num subshell `( umask 077; ... )`; a umask da shell fica intacta (medido: 022 antes e 0022 depois do bloco; arquivos criados com -rw-------).
  S2. Os dois `docker run` do Postgres usam `postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab`, o mesmo digest de .github/workflows/delivery.yml:24, e uma frase diz que é a imagem do CI fixada por digest. `docker pull` por esse digest respondeu "Image is up to date", e `docker image inspect postgres:16-alpine` lista esse RepoDigest: a tag local já era esse índice.
  S3. Novo bloco "Para desfazer o exemplo": `docker stop -t 25 biblioteca-api`, `docker rm biblioteca-api`, `docker rm -f biblioteca-pg`, `docker network rm biblioteca-net`, `rm .env .env.postgres`.
  S4. "nenhuma credencial fica no código, na imagem nem no git."
  S5. O comentário do Dockerfile cita internal/domain/multa.go e internal/storage/repositorio.go e avisa que o import não pode ser removido porque os testes e o `go run` não passam por esse build. Só comentário: as instruções do Dockerfile não mudaram.

REPRODUCAO (scratchpad /tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad; os blocos ```sh foram extraídos do README por extrai.sh, com awk, sem nenhuma edição, e executados com bash -x na raiz do checkout; hexadecimais de 32 a 64 caracteres foram redigidos nos logs):
  Antes (README original, bloco 3, Docker) — a corrida aparece:
    + docker run -d --name biblioteca-pg --network biblioteca-net --env-file .env.postgres postgres:16-alpine
    + docker run --rm --network biblioteca-net --env-file .env biblioteca-bairro migrate up
    {"level":"ERROR","msg":"biblioteca-bairro migrate encerrado com erro","erro":"migrate: banco indisponível"}
    + ... migrate status
    {"level":"ERROR","msg":"biblioteca-bairro migrate encerrado com erro","erro":"migrate: banco indisponível"}
    + docker run -d --name biblioteca-api ...
    docker logs biblioteca-api: {"level":"ERROR","msg":"biblioteca-bairro encerrado com erro","erro":"banco indisponível no boot"}
    docker ps: biblioteca-api Exited (1); biblioteca-pg Up 4 seconds
  Antes (bloco 4, testes) — a corrida aparece, rc=1:
    --- FAIL: TestMigrateFimAFimComBancoReal ... migrate_test.go:714: conectar ao banco de teste: falhou (confira BIBLIOTECA_TEST_DATABASE_URL)
    --- FAIL: TestBancoRenovacao ... banco_renovacao_test.go:14: conectar ao banco de teste: falhou
    (mais 8 testes de cmd/biblioteca-bairro com o mesmo erro)
    FAIL	github.com/rodrigodosanjosoliveira/biblioteca-bairro/cmd/biblioteca-bairro	0.119s
  Depois (README corrigido, blocos 3 e 4: Docker e limpeza) — rc=0:
    + docker exec biblioteca-pg pg_isready -h 127.0.0.1 -U postgres
    + sleep 1
    + docker exec biblioteca-pg pg_isready -h 127.0.0.1 -U postgres
    + docker run --rm ... biblioteca-bairro migrate up
    {"level":"INFO","msg":"lock adquirido"}
    {"level":"INFO","msg":"migração aplicada","versao":1,"nome":"esquema_emprestimo","duracao_ms":24}
    {"level":"INFO","msg":"migração aplicada","versao":2,"nome":"renovacao_emprestimo","duracao_ms":3}
    migrate up: 2 migração(ões) aplicada(s)
    + docker run --rm ... migrate status
    0001 esquema_emprestimo aplicada 2026-11-06T20:31:18Z
    0002 renovacao_emprestimo aplicada 2026-11-06T20:31:18Z
    + docker run -d --name biblioteca-api ...
    umask depois do bloco: 0022
    -rw------- .env ; -rw------- .env.postgres
    biblioteca-api Up 2 seconds ; biblioteca-pg Up 4 seconds
    healthz 200
    readyz 200
    + docker stop -t 25 biblioteca-api / docker rm biblioteca-api / docker rm -f biblioteca-pg / docker network rm biblioteca-net / rm .env .env.postgres  (todos ok)
    depois: nenhum container biblioteca*, nenhuma rede biblioteca-net, .env e .env.postgres ausentes
  Depois (bloco 5, testes) — rc=0:
    + docker exec biblioteca-pg-teste pg_isready -h 127.0.0.1 -U postgres
    + sleep 1
    + docker exec biblioteca-pg-teste pg_isready -h 127.0.0.1 -U postgres
    + go test ./...
    ok  	.../cmd/biblioteca-bairro	7.127s
    ok  	.../internal/config	(cached)
    ok  	.../internal/domain	(cached)
    ok  	.../internal/http	3.350s
    ok  	.../internal/http/middleware	(cached)
    ok  	.../internal/migrate	13.327s
    ok  	.../internal/storage	2.360s
    ok  	.../migrations	(cached)
  O laço deu uma volta de sleep nas duas execuções: a janela da corrida existe e a espera a cobre.

CONFIRMADO (pipeline_sensors, ok: true):
  vet: ok, exit 0, sem saída
  test: ok, exit 0. "pacotes: 8 (8 ok, 0 com falha, 0 sem testes) / testes: 1580 passaram, 0 falharam, 200 pulados". Os pulados são os testes de banco, porque o sensor roda sem BIBLIOTECA_TEST_DATABASE_URL; com banco, todos passaram (bloco 5 acima).
  build: ok, exit 0, sem saída
  vuln: ok, exit 0, "No vulnerabilities found."
  mod_verify: ok, exit 0, "all modules verified"
  fuzz: não se aplica, porque nenhum alvo de fuzz foi tocado.

LIMPEZA (só o que eu criei, por nome):
  docker rm -f biblioteca-api biblioteca-pg biblioteca-pg-teste (a cada rodada); docker network rm biblioteca-net; rm -f .env .env.postgres; docker rmi biblioteca-bairro (a imagem não existia antes deste ciclo). A imagem postgres:16-alpine já estava na máquina e ficou. Não rodei docker builder prune, docker system prune nem nenhuma limpeza genérica do Docker. O cache de BuildKit do `docker build` deste ciclo ficou na máquina.
  Registro: antes da reprodução do B2, rodei `go clean -testcache`, que é global da máquina. Só invalida resultados de teste em cache, que se refazem na próxima execução, sem perda de dado. Mesmo assim, foi limpeza genérica fora do pedido e fica registrada.

ASSUNCOES:
  - O `until` não tem prazo. Se o container do Postgres morrer, o laço não termina, e o README diz para ver `docker logs biblioteca-pg`. Preferi a forma legível sugerida pelo revisor a um laço com contador.
  - Os logs do scratchpad tiveram a senha e o token descartáveis redigidos (sed). Os valores existiram só durante a execução, e os containers e o .env foram removidos.

ARQUIVOS:
  - /home/rodrigo/workspace/biblioteca-bairro/README.md
  - /home/rodrigo/workspace/biblioteca-bairro/Dockerfile (só comentário, S5)

NAO_ALTERADO: cmd/, internal/, migrations/, go.mod, go.sum, Makefile.

RISCOS:
  - A imagem da API continua sem build no CI, e o roteiro do README não é exercitado pelo CI: uma regressão no procedimento só aparece executando-o à mão.

SINAIS:
  SECURITY: sim — documentação de credenciais (.env com umask 077 em subshell, limpeza do .env) e imagem de execução; require_security ligada.
  SUPPLY_CHAIN: sim — postgres:16-alpine passa a ser fixado por digest no README (o mesmo do CI); imagens base do Dockerfile inalteradas; go.mod/go.sum inalterados; nenhuma dependência nova.
  MIGRATION: nao — sem migração nova.
  PERFORMANCE: nao — nenhum caminho de código alterado.

PROXIMO: code-reviewer
MOTIVO: B1/B2 corrigidos e S1 a S5 aplicadas, com a reprodução antes e depois executada literalmente e os sensores verdes. Volta à revisão.
