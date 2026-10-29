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
