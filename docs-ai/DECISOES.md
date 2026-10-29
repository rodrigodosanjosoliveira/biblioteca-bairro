# Decisões — biblioteca-bairro

> Registro das decisões do projeto, com o motivo de cada uma. Decisão nova entra
> com número sequencial (`D-NN`), o que foi decidido e o porquê; decisão
> revista ganha uma nova entrada que cita a anterior.

---

## D-01 — Go no backend, stdlib primeiro

Formato único imposto pelo `gofmt`, linguagem que limita abstrações complexas
de propósito e stdlib ampla, que reduz a superfície de dependências externas.

**Motivo:** com IA, o gargalo se desloca da geração para a verificação; código
uniforme e com poucas dependências é mais fácil de verificar.

## D-02 — API em Go com PostgreSQL

Um único banco por projeto. Esquema evolui só por migração versionada em
`migrations/`, com reversão.

## D-03 — Testes com a stdlib `testing`, em tabela

Biblioteca de asserção é opcional e exige decisão registrada aqui.

**Motivo:** cada dependência que entra precisa ser justificada e revisada; o
padrão em tabela já cobre bem a necessidade.

## D-04 — Fluxo por subagentes com ferramentas restritas

Toda demanda passa por `pm-triage` e segue o fluxo do `CLAUDE.md`. Revisores
não têm ferramenta de escrita; só os subagentes de `editor_agents` editam
caminho protegido.

**Motivo:** no Claude Code a identidade do subagente (`agent_type`) e as
ferramentas de cada um são aplicadas pelo runtime, não só pelo prompt.

## D-05 — Gates determinísticos

`go build`, `go vet` e `go test` como sensores (`bin/pipeline-claude sensors`);
`govulncheck` nas revisões de segurança e de dependências; fuzz com
`-fuzztime` para parsing e validação.

**Motivo:** refatoração iterativa de agente sem validação externa degrada
rápido; o gate é a validação externa.

## D-06 — Segurança antes do QA; release antes da documentação

`security-review` roda depois do `code-reviewer` e antes do `qa-validation`;
`release-ops`, quando habilitado, vem antes do `docs-update`.

**Motivo:** achado de segurança muda a implementação, e revalidar o QA depois
sai mais caro.

## D-07 — `require_security` começa desligada e o validador a força

O triage liga a flag quando há superfície de segurança; o validador a exige
quando o diff toca `security_surface_paths` ou `go.mod`/`go.sum`
(`auto_flags`).

**Motivo:** esquecer no triage não pode deixar a delivery passar sem revisão.

## D-08 — Entrega por branch e Pull Request

Branch `task/{código}`, commit `{type}({scope}): {resumo} [{código}]`, push
direto na `main` bloqueado pelo `.githooks/pre-push`.

## D-09 — Valores monetários com `shopspring/decimal` v1.4.0

Valores em dinheiro (começando pela multa por atraso, DEL-001) usam
`github.com/shopspring/decimal`, fixada em v1.4.0. A recomendação técnica era
`int64` em centavos, só com a stdlib; o usuário preferiu a biblioteca decimal.
Licença MIT, sem dependência transitiva (o `go.sum` só tem a própria
`decimal`), aprovada pelo `supply-chain-review` (`go mod verify` e
`govulncheck` limpos). A v1.6.0 saiu em 18/10/2026, mas a v1.4.0 foi mantida
por ser madura; a atualização fica para uma delivery futura.

**Motivo:** aritmética decimal exata, sem erro de arredondamento, com API
legível para regras monetárias.

**Consequências:** `decimal.Decimal` aparece na API pública de
`internal/domain`, que passa a depender de pacote externo. A persistência
futura deve usar coluna `NUMERIC` no PostgreSQL. Em JSON o tipo serializa como
string por padrão; o futuro endpoint precisa assumir isso no contrato.

## D-10 — Datas de negócio pela data civil em `America/Sao_Paulo`

Regras que contam dias (como a multa por atraso, DEL-001) usam a data civil em
`America/Sao_Paulo`, qualquer que seja o fuso do `time.Time` recebido. A base
de fusos é embutida no binário com `import _ "time/tzdata"`, e a diferença em
dias é calculada entre meias-noites UTC reconstruídas a partir da data civil
(divisão do Unix por 86400).

**Motivo:** o resultado não pode depender do fuso do servidor nem da base de
fusos do sistema operacional, e dias de 23h ou 25h no horário de verão não
podem gerar erro de um dia.

**Consequências:** o binário cresce cerca de 450 KB por causa do
`time/tzdata`. Feriados e fusos configuráveis ficam fora até decisão nova.

## D-11 — Vencimento do empréstimo às 12:00 em `America/Sao_Paulo`

O prazo do empréstimo é de 14 dias corridos (`PrazoEmprestimoDias`, DEL-003),
contados pela data civil em `America/Sao_Paulo` (D-10). O vencimento é
representado como 12:00 em `America/Sao_Paulo` da data civil de `inicio` + 14
dias. Na persistência (DEL-004), o vencimento vai para uma coluna `DATE` e é
reidratado ao meio-dia em São Paulo.

**Motivo:** a meia-noite pode não existir no início do horário de verão (por
exemplo, 04/11/2018 em São Paulo); o meio-dia sempre existe e cai na data civil
correta, o que mantém `CalcularMulta(e.Vencimento(), e.Devolucao())` correto.

**Consequências:** o horário 12:00 é convenção, não regra de negócio; quem ler
o vencimento deve usar só a data civil. Prazo configurável e feriados ficam
fora até decisão nova.

## D-12 — Modelo de empréstimo: estado encapsulado, status derivado, IDs `int64`

`Emprestimo` tem campos não exportados com getters; o estado só muda por
`NovoEmprestimo` e `Devolver`, que validam as regras e não alteram o estado em
erro. A reconstrução a partir do banco será feita por uma função de
reidratação validada (`ReidratarEmprestimo`, DEL-004). O status não é gravado:
`Aberto()` é derivado de `Devolucao` estar zerada. `Livro`, `Usuario` e
`Emprestimo` usam IDs `int64` (identity no PostgreSQL), sem UUID; o ID fica 0
até a persistência. `Livro` é o exemplar físico, só com `Titulo`; `Usuario` só
com `Nome`, para minimizar dado pessoal. A regra de um empréstimo aberto por
livro (R4) será garantida no banco por índice único parcial
(`WHERE devolucao IS NULL`) na DEL-004.

**Motivo:** invariantes protegidos pelo tipo, sem estado inconsistente entre
status e data de devolução, e unicidade sob concorrência garantida por quem
pode garanti-la, o banco.

**Consequências:** desde a DEL-004, o zero-value `Emprestimo{}` não aceita
`Devolver` (`ErrEmprestimoInvalido`), e `ReidratarEmprestimo` reconstrói o
empréstimo gravado validando os invariantes. IDs sequenciais permitem
enumeração quando expostos via HTTP, o que deve ser tratado na camada HTTP.
`Nome` é dado pessoal e não deve ir para log. Desde a DEL-004, a R4 é garantida
no banco pelo índice único parcial `emprestimo_livro_aberto_uq` (D-13).

## D-13 — Persistência em PostgreSQL com `pgx/v5` v5.11.0, migrações em SQL puro e Go 1.25

Decisões aprovadas pelo humano na DEL-004.

> Revisada pela D-15 (pgxpool).

**Driver.** A persistência usa `github.com/jackc/pgx/v5` v5.11.0, no modo
nativo, com a interface `DBTX` em `internal/storage` (atendida por conexão ou
transação) e sem `pgxpool`. Justificativa: a stdlib não tem driver PostgreSQL;
foi escolhida a versão mais recente compatível e sem vulnerabilidade conhecida.
Licenças e transitivas que entram no build: `pgx/v5` v5.11.0 (MIT),
`jackc/pgpassfile` v1.0.0 (MIT), `jackc/pgservicefile` em pseudo-versão fixada
pelo pgx (MIT) e `golang.org/x/text` v0.41.0 (BSD-3). `testify`, `puddle` e
`x/sync` aparecem só no `go.sum`, fora do build; o `x/crypto` saiu do grafo.
Aprovada pelo `supply-chain-review`, com `go mod verify` ok e `govulncheck`
limpo.

**Go 1.25.** A diretiva `go` do `go.mod` subiu para `1.25.0`, sem linha
`toolchain`. Motivo: o pgx v5.7.6 tem GO-2026-5004, GO-2026-4771 e
GO-2026-4772, e o `x/text` v0.24.0 tem GO-2026-6629 e GO-2026-5970; as versões
corrigidas exigem go 1.25. O `x/text` fica em v0.41.0 porque a v0.42 exige
go 1.26. O `go.mod` declara a versão mínima; o CI usa a versão stable.

**Migrações.** SQL puro em `migrations/`, no padrão
`NNNN_nome.{up,down}.sql` (compatível com o golang-migrate), embutido no
binário via `embed` (`migrations.FS`). Sem ferramenta de migração e sem tabela
de versão nesta delivery; aplicação no boot e executor de produção ficam para
decisão futura. Sem CHECK de data civil no banco: o domínio aceita devolução no
mesmo dia civil com hora anterior ao início, e a tzdata do PostgreSQL pode
divergir da do Go, inclusive num restore.

**Reidratação.** `ReidratarEmprestimo` aceita o vencimento gravado (não exige
início + 14 dias). O vencimento lido da coluna `DATE` é normalizado pela data
civil do próprio valor para 12:00 em `America/Sao_Paulo` (D-11). Não há
`ReidratarLivro` nem `ReidratarUsuario`.

**Testes de banco.** Rodam contra PostgreSQL real quando
`BIBLIOTECA_TEST_DATABASE_URL` está definida e são pulados quando ausente, de
modo que `go test ./...` passa sem banco. No CI são obrigatórios: serviço
Postgres 16 no job `gates`, imagem fixada por digest, acesso `trust` e
`BIBLIOTECA_TEST_DB_OBRIGATORIO=1`. Cada teste usa um schema descartável.

**Erros.** Erros do repositório não carregam SQL, DSN, senha nem dado
identificador (`Nome`, `Titulo`). A violação da R4 (SQLSTATE 23505 em
`emprestimo_livro_aberto_uq`) vira `ErrLivroJaEmprestado`. Log e resposta só
podem usar `Code` e `ConstraintName` do `*pgconn.PgError`, nunca `Detail`,
`Message` ou `Where` (o `PgError` continua na cadeia de `Unwrap`).

**Regras para as próximas migrações:**

- o executor roda cada arquivo numa única transação;
- `CREATE INDEX CONCURRENTLY` e `DROP INDEX CONCURRENTLY` ficam sozinhos no
  arquivo;
- FK nova entra com `NOT VALID` e é validada depois com `VALIDATE CONSTRAINT`;
- coluna `NOT NULL` nova leva `DEFAULT` constante;
- remoção de coluna segue expand/contract;
- quem aplica usa `lock_timeout` curto e `statement_timeout`;
- o down destrutivo só roda depois de reverter o deploy e com `pg_dump`.

**Motivo:** menos dependências e menos peças móveis (sem pool, sem ferramenta
de migração), unicidade e integridade garantidas pelo banco, e teste de
persistência contra PostgreSQL real sem obrigar banco no desenvolvimento local.

**Consequências:** a proteção do down destrutivo é só procedimental, e ainda
não há executor de migração de produção definido. Dependência nova exige
go 1.25+ de quem compila.

## D-14 — Multa do empréstimo derivada por `Emprestimo.Multa(ref)`, sem relógio no domínio

Decisões humanas (H1 a H3) e do `architecture-guardrails` (H4) na DEL-005.

**Multa derivada (H1).** A multa do empréstimo só é calculada, pelo método
`Emprestimo.Multa`, que delega para `CalcularMulta` (D-09, D-10). Não é
persistida: sem coluna, sem migração e sem mudança em `internal/storage`.

**Assinatura e semântica (H2).** `func (e Emprestimo) Multa(ref time.Time)
(decimal.Decimal, error)`. No empréstimo devolvido, a referência é
`Devolucao()` e `ref` é ignorado, inclusive quando é zero. No aberto, a
referência é `ref`. As regras da DEL-001 continuam (H3): R$ 1,00 por dia civil
de atraso em `America/Sao_Paulo`, teto de R$ 20,00 e multa zero com devolução
até o vencimento.

**Escolhas de implementação (H4).**

- O nome é `Multa`, receptor por valor; o empréstimo não é alterado.
- `Emprestimo{}` retorna `ErrEmprestimoInvalido`, checado primeiro, coerente
  com `Devolver` (D-12).
- `ref` zero no aberto retorna a sentinela nova `ErrReferenciaZero`
  (`"emprestimo: data de referência zero"`), em vez de reaproveitar
  `ErrDevolucaoZero`, cuja mensagem seria enganosa.
- `ref` até o dia civil do vencimento, inclusive antes do início, dá zero sem
  erro, porque o vencimento é sempre posterior ao início.
- Erros de `CalcularMulta` voltam sem wrap; em qualquer erro o valor é
  `decimal.Zero`.

**Sem relógio no domínio.** `internal/domain` não chama `time.Now()` e não tem
`Clock`: quem chama fornece `ref`. A camada de cima (HTTP ou serviço) é quem vai
precisar de uma fonte de tempo para o empréstimo aberto.

**Status das deliveries.** Os valores aceitos são os listados no
`docs-ai/deliveries/INDEX.md`: `draft`, `em revisão`, `merged` e `revertida`
(o template de report e o validador não definem outra lista; o template só
traz `draft` como valor inicial). O status vai igual no `INDEX.md` e no front
matter do report. A delivery fica `em revisão` até o merge do PR; a delivery
seguinte, depois de conferir o merge na `main`, marca a anterior como `merged`
nos dois lugares. Na DEL-005 isso corrigiu DEL-001 a DEL-004 (PRs #1 a #4),
que estavam como `draft` ou `em revisão`.

**Motivo:** multa derivada não diverge de uma multa gravada; domínio sem
relógio continua determinístico e testável em tabela e fuzz; e o status das
deliveries deixa de ficar parado depois do merge.

**Consequências:** no empréstimo devolvido, `Multa(agora)` devolve o valor
congelado na devolução, e quem consome precisa saber disso. Pagamento, abono,
bloqueio por multa, feriados e valores configuráveis ficam fora até decisão
nova.

## D-15 — Camada HTTP com `net/http`, `pgxpool`, token Bearer estático e relógio injetado

Decisões humanas (H1 a H9 e quatro complementares) e do
`architecture-guardrails` na DEL-006, primeira fatia vertical da camada HTTP.

**Servidor (H1, H8).** `net/http` da stdlib, sem framework. Pacotes
`internal/config` (só stdlib, `Carregar(getenv)`), `internal/http` (pacote
`httpapi`) e `internal/http/middleware`; o wiring fica em
`cmd/biblioteca-bairro` (`run()` e `configurarPool()`). Timeouts do servidor de
5/10/15/60s, `MaxHeaderBytes` de 64 KiB, corpo limitado a 64 KiB e
`DisallowUnknownFields`. Cadeia de middlewares: RequestID → Log → Recover →
LimitarCorpo → mux → Auth (por rota). As interfaces `Repositorio` e `Pinger`
são definidas no consumidor.

**Endpoints (H2).** `POST /livros`, `POST /usuarios`,
`POST /livros/{id}/emprestimo`, `POST /livros/{id}/devolucao` e
`GET /livros/{id}/emprestimo`, mais `/healthz` e `/readyz` sem autenticação. O
`/readyz` faz um Ping por vez, guarda o sucesso por 1s, tolera 3s durante a
renovação e responde 503 na hora sem sucesso recente.

**Contrato e erros (H3).** Erro em JSON com `codigo` e mensagem fixa por
código: `corpo_invalido` 400, `corpo_muito_grande` 413, `id_invalido` 400,
`nao_autorizado` 401, `titulo_vazio`/`nome_vazio`/`texto_invalido`/
`usuario_id_invalido`/`usuario_nao_encontrado` 422,
`livro_nao_encontrado`/`emprestimo_aberto_nao_encontrado` 404,
`livro_ja_emprestado`/`emprestimo_ja_devolvido`/`emprestimo_alterado` 409,
`indisponivel` 503 e `erro_interno` 500. 400 para sintaxe, 422 para semântica,
inclusive usuário inexistente. Saída: multa com `StringFixed(2)`, como string
(D-09); instantes RFC 3339 em UTC; vencimento `AAAA-MM-DD` (D-11); IDs como
número. O corpo da devolução precisa ser vazio, e NUL no texto vira 422.

**Relógio injetado (H4).** A camada HTTP fornece a referência de tempo ao
domínio (D-14); o cliente não envia datas.

**Pool (H5), revisando a D-13.** A D-13 previa "sem `pgxpool`"; o
`*pgx.Conn` único não é seguro sob requisições concorrentes, então o boot usa
`pgxpool`, que satisfaz `storage.DBTX` sem mudar `internal/storage`.
Configuração: `MaxConns` de 1 a 100 (padrão 10), `MinConns` 0,
`MinIdleConns` 0, lifetime 30min, idle 5min, jitter 0, health check 1min,
`ConnectTimeout` 5s e timeout de 5s por requisição. Ping falho no boot encerra
o processo (fail fast). Com isso, `jackc/puddle/v2` v2.2.2 (MIT) e
`golang.org/x/sync` v0.22.0 (BSD-3), que a D-13 registrava só no `go.sum`,
entram no build como indiretas, pela cadeia `pgxpool` → `puddle` →
`x/sync/semaphore`. O `x/sync` v0.22.0 sai do MVS por causa do `x/text`
v0.41.0; a `puddle` v2.2.3 existe e fica para o próximo bump do pgx. Aprovado
pelo `supply-chain-review`, com `go mod verify` ok e `govulncheck` limpo.

**Sem migração no boot (H6).** O executor de migração de produção fica para a
DEL-007.

**Autenticação (H7).** Token Bearer estático: o servidor guarda o sha256 do
token e compara com `subtle.ConstantTimeCompare`; exige um único header
`Authorization`; `Auth("")` recusa tudo. Respostas autenticadas e o 401 levam
`Cache-Control: no-store`.

**Log (H9).** `slog` em JSON no stderr, uma linha por requisição, com
`codigo`, `causa` e só `pg_code`/`pg_constraint` do `PgError`, nunca
`err.Error()`, URL, query, headers, corpo, valor nem stack de panic.

**Variáveis de ambiente.**

- `BIBLIOTECA_API_TOKEN`: obrigatório, 32 a 512 bytes de ASCII visível
  (0x21 a 0x7E), sem trim.
- `DATABASE_URL`: DSN do PostgreSQL.
- `ADDR`: endereço de escuta, padrão `:8080`.
- `BIBLIOTECA_DB_MAX_CONNS`: 1 a 100, padrão 10; sobrescreve `pool_max_conns`
  da DSN.

Os erros de configuração e de boot têm mensagem fixa (`config.Erro`,
`erroBoot`) e não ecoam o valor.

**Riscos aceitos.**

- `encoding/json` aceita chave sem diferenciar maiúsculas e chave duplicada (a
  última vence); nenhuma autorização depende do corpo.
- RequestID e Log ficam fora do Recover; hoje sem caminho alcançável por
  entrada externa.
- A mensagem fixa de `erroBoot` e `config.Erro` é convenção, não garantia do
  compilador.
- A refatoração de `r.Pattern` não foi feita, e `Repo` nil não é validado.
- O 404 revela a existência do livro, e IDs sequenciais são enumeráveis por
  quem tem o token (operador único).
- Sem executor, o binário sobe contra banco sem schema: o `/readyz` dá 200 e as
  rotas dão 500 com `causa=banco`.

**Motivo:** stdlib cobre o servidor HTTP; o pool resolve a concorrência sem
mudar o repositório; erros com código e mensagem fixa e log sem
`err.Error()` impedem vazamento de DSN, `PgError` e dado pessoal; relógio
injetado mantém o domínio determinístico.

**Consequências:** `/healthz` e `/readyz` devem ser expostos só na rede
interna, e TLS e HSTS ficam a cargo do proxy. Autenticação completa (DEL-008),
`GET /emprestimos/{id}`, listagens e paginação (DEL-009 em diante) ficam para
deliveries futuras. Pendente de ação humana fora da sessão (H10): acrescentar
`internal/http/` e `internal/config/` a `security_surface_paths` no
`.pipeline-claude.json`.
