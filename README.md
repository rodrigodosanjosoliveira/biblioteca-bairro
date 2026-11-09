# biblioteca-bairro

Projeto **fictício** de demonstração do
[pipeline-claude](https://github.com/rodrigodosanjosoliveira/pipeline-ai-claude):
uma API em Go para o acervo e os empréstimos de uma biblioteca de bairro.

O domínio é inventado; o que importa aqui é a governança. Este repositório foi
criado pelo `pipeline-claude init` e mostra, num projeto real e público, o fluxo
de desenvolvimento assistido por agentes no Claude Code:

- subagentes em `.claude/agents/` (triage, guardrails, testes, implementação,
  revisores só leitura, QA, docs e entrega);
- hook `PreToolUse` que bloqueia edição de caminho protegido fora dos agentes
  autorizados, e `fmt-hook` que formata o Go editado;
- servidor MCP do pipeline (`.mcp.json`) com diff de revisão, sensores e
  criação de delivery;
- report de cada delivery em `docs-ai/deliveries/`, validado no CI pelo
  `pipeline-claude validate`;
- gates do projeto (`vet`, `test`, `build`, `govulncheck`, `go mod verify`)
  declarados em `.pipeline-claude.json` e rodados por `make check` e no CI.

## Como usar

Requer Go (versão do `go.mod` ou mais nova), `git`, `gh` e o Claude Code.

```sh
make tools                    # instala bin/pipeline-claude na versão do Makefile
bin/pipeline-claude doctor    # diagnóstico do ambiente e do projeto
make check                    # gates
```

Uma demanda nova, numa sessão do Claude Code aberta aqui:

```text
/nova-delivery <título>       # cria DEL-NNN e o report
/fluxo <demanda>              # conduz os subagentes do triage à entrega
```

A branch é `task/<ID>` e a PR roda a validação da delivery no CI.

## Executar a API

A API precisa de um PostgreSQL. Toda a configuração vem de variáveis de
ambiente; nenhuma credencial fica no código, na imagem nem no git.

| Variável | Obrigatória | Valor |
|---|---|---|
| `DATABASE_URL` | sim | DSN do PostgreSQL (API e `migrate`) |
| `BIBLIOTECA_API_TOKEN` | sim (API) | de 32 a 512 bytes ASCII visível |
| `ADDR` | não | endereço de escuta; padrão `:8080` |
| `BIBLIOTECA_DB_MAX_CONNS` | não | de 1 a 100; padrão 10 |
| `BIBLIOTECA_MIGRATE_LOCK_TIMEOUT` | não | duração Go, de 100ms a 60s; padrão 5s |
| `BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT` | não | duração Go, de 1s a 30m; padrão 60s |

A ordem é sempre aplicar as migrações e depois subir a API: a API não migra no
boot. O binário aceita só `migrate up`, `migrate status` ou nenhum argumento
(servidor); qualquer outro argumento sai com código 2.

### Com `go run`

A DSN leva a senha do banco. Leia-a sem eco, para que ela não apareça na tela
nem vá para o histórico da shell. Rode **só esta linha** e cole a DSN quando o
prompt pedir
(`postgres://<usuário>:<senha>@<host>:5432/<banco>?sslmode=verify-full`):

```bash
printf 'DATABASE_URL: '; IFS= read -rs DATABASE_URL; echo
```

`read -rs` funciona em bash e zsh, não em `sh`/`dash`. A linha fica sozinha
porque o `read` consome a próxima linha do terminal: colada junto com os
comandos abaixo, ela leria um deles como DSN. Depois de informar a DSN, rode o
restante:

```sh
export DATABASE_URL
export BIBLIOTECA_API_TOKEN="$(openssl rand -hex 32)"
go run ./cmd/biblioteca-bairro migrate up
go run ./cmd/biblioteca-bairro migrate status
go run ./cmd/biblioteca-bairro
```

Ao terminar, `unset DATABASE_URL BIBLIOTECA_API_TOKEN` tira as credenciais da
shell.

### Com Docker

O `Dockerfile` gera uma imagem distroless, sem shell, que roda como usuário não
root (65532) com o binário como `ENTRYPOINT`. A mesma imagem serve a API (sem
argumentos) e roda a migração (`migrate up`/`migrate status`).

Exemplo com um PostgreSQL local descartável. As credenciais são geradas na hora
e ficam num arquivo `.env` (ignorado pelo git), passado por `--env-file`. O
PostgreSQL usa a mesma imagem do CI, fixada por digest:

```sh
docker build -t biblioteca-bairro .

senha="$(openssl rand -hex 16)"
( umask 077
  printf 'POSTGRES_PASSWORD=%s\n' "$senha" > .env.postgres
  printf 'DATABASE_URL=postgres://postgres:%s@biblioteca-pg:5432/postgres?sslmode=disable\nBIBLIOTECA_API_TOKEN=%s\n' \
    "$senha" "$(openssl rand -hex 32)" > .env )
unset senha

docker network create biblioteca-net
docker run -d --name biblioteca-pg --network biblioteca-net --env-file .env.postgres \
  postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab

# espera o PostgreSQL aceitar conexão por TCP (o initdb leva alguns segundos)
until docker exec biblioteca-pg pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; do sleep 1; done

docker run --rm --network biblioteca-net --env-file .env biblioteca-bairro migrate up
docker run --rm --network biblioteca-net --env-file .env biblioteca-bairro migrate status

docker run -d --name biblioteca-api --network biblioteca-net --env-file .env \
  -p 127.0.0.1:8080:8080 --stop-timeout 25 biblioteca-bairro
```

A espera usa `-h 127.0.0.1`, e não o socket: durante o initdb o PostgreSQL sobe
um servidor temporário só no socket unix, que daria um falso pronto. Sem essa
espera, `migrate up` e a API saem com "banco indisponível". Se o laço não
terminar, veja `docker logs biblioteca-pg`.

Para desfazer o exemplo (o `.env` guarda a senha e o token):

```sh
docker stop -t 25 biblioteca-api
docker rm biblioteca-api
docker rm -f biblioteca-pg
docker network rm biblioteca-net
rm .env .env.postgres
```

`sslmode=disable` só vale para esse banco local na rede do Docker; em qualquer
outro ambiente use TLS (`sslmode=verify-full`; a imagem traz os certificados de
CA).

Use `docker stop -t 25` (ou `--stop-timeout 25`, como acima): o encerramento
gracioso da API leva até 20 s e o padrão do Docker (10 s) mataria o processo
com SIGKILL no meio. O binário é o PID 1 e recebe o SIGTERM diretamente.

A imagem não tem `HEALTHCHECK` (não há shell nem curl). O orquestrador usa
`GET /healthz` (liveness) e `GET /readyz` (readiness), rotas sem autenticação.

### Testes com banco

`go test ./...` roda sem banco; os testes que precisam de PostgreSQL são
pulados. Para rodá-los, suba um PostgreSQL **descartável** com autenticação
`trust` (exigida pelos testes de migração) e passe as variáveis de teste só ao
`go test`. O bloco termina removendo o banco, e pode ser repetido:

```sh
docker run -d --name biblioteca-pg-teste -p 127.0.0.1:5433:5432 \
  -e POSTGRES_HOST_AUTH_METHOD=trust \
  postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab
until docker exec biblioteca-pg-teste pg_isready -h 127.0.0.1 -U postgres >/dev/null 2>&1; do sleep 1; done
BIBLIOTECA_TEST_DATABASE_URL='postgres://postgres@localhost:5433/postgres?sslmode=disable' \
  BIBLIOTECA_TEST_DB_OBRIGATORIO=1 \
  go test ./...
docker rm -f biblioteca-pg-teste
```

Com `BIBLIOTECA_TEST_DB_OBRIGATORIO=1`, os testes de banco reprovam em vez de
pular se a URL faltar. As duas variáveis valem só para esse `go test`; um
`go test ./...` posterior na mesma shell volta a pular os testes de banco.

`trust` dispensa senha: use só nesse banco de teste local ou no CI, nunca no
banco da API. Por isso o bloco remove o container ao final.

### Postman

`postman/` traz uma coleção (formato v2.1.0) com todas as rotas da API e um
environment modelo, com `base_url` em `http://localhost:8080` e `token` vazio.

1. Copie o environment para o nome local, ignorado pelo git:

   ```sh
   cp postman/biblioteca-bairro.postman_environment.json \
     postman/biblioteca-bairro.local.postman_environment.json
   ```

2. No Postman, importe `postman/biblioteca-bairro.postman_collection.json` e a
   cópia `.local`, e selecione o environment.
3. Preencha `token` com o valor de `BIBLIOTECA_API_TOKEN` só no valor atual
   (current value). Nunca grave o token no environment versionado; se exportar
   o environment, salve só por cima da cópia `.local`.
4. Ordem sugerida: Criar livro, Criar usuário, Abrir empréstimo, as consultas,
   Renovar empréstimo e Devolver livro.

`/healthz` e `/readyz` vão sem autenticação; as demais rotas usam
`Authorization: Bearer {{token}}`, definido na coleção. Os scripts de
pós-resposta só gravam no environment os ids criados (`livro_id`, `usuario_id`,
`emprestimo_id`) e o cursor `proximo` das listagens. Os parâmetros de query
opcionais vêm desligados: a API responde 400 `parametro_invalido` a chave
desconhecida, repetida ou com valor vazio. Ligue `apos` só quando o cursor
tiver valor: na última página (`proximo` null) o script remove o cursor, e o
Postman passa a enviar o texto literal `{{cursor_...}}`, que também dá 400
`parametro_invalido`. A coleção é mantida à mão: rota nova
em `internal/http/handler.go` exige atualizá-la.

## Deliveries

O índice fica em [`docs-ai/deliveries/INDEX.md`](docs-ai/deliveries/INDEX.md).