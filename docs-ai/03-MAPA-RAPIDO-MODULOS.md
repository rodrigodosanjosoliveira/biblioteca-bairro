# Mapa rápido dos módulos — biblioteca-bairro

> Guia para o `pm-triage` e os subagentes técnicos identificarem os módulos
> impactados pelo caminho dos arquivos, sem ler a base de código inteira.
> Atualize quando um módulo nascer, sumir ou mudar de responsabilidade.

---

## Estrutura

```text
├── cmd/
│   └── biblioteca-bairro/
│       ├── main.go         # ponto de entrada: servidor HTTP ou `migrate up|status`
│       └── migrate.go      # subcomando de migração (despachar, migrar)
├── internal/               # código do backend (protegido)
│   ├── http/               # rotas, handlers, serialização e middlewares
│   ├── domain/             # entidades e regras de negócio, sem I/O
│   ├── storage/            # persistência e repositórios (superfície de segurança)
│   ├── migrate/            # executor de migração em produção (conexão dedicada)
│   └── config/             # leitura e validação de variáveis de ambiente
├── migrations/             # migrações SQL do PostgreSQL (superfície de segurança)
│   └── controle/           # SQL da tabela de controle `migracao_aplicada`
├── docs-ai/                # decisões, invariantes, runbook de migração, contrato de handoff e deliveries
├── Makefile                # gates e instalação do pipeline-claude
├── .pipeline-claude.json   # governança: caminhos protegidos, editores, módulos, sensores
├── .claude/                # subagentes, skills e settings do Claude Code
└── .mcp.json               # servidor MCP do pipeline-claude
```

Os diretórios de `internal/` são o ponto de partida sugerido; crie-os conforme
a necessidade e mantenha este mapa em dia.

## Módulos existentes

| Módulo | Arquivos | Responsabilidade | Desde |
|---|---|---|---|
| `internal/domain/` | `multa.go` (`CalcularMulta`, `ErrVencimentoZero`, `ErrDevolucaoZero`) | Multa por atraso: R$ 1,00 por dia civil em `America/Sao_Paulo`, teto R$ 20,00, em `decimal.Decimal` (D-09, D-10). Função pura, sem I/O | DEL-001 |
| `internal/domain/` | `livro.go` (`Livro`, `NovoLivro`, `ErrTituloVazio`) | Exemplar físico com `ID` `int64` e `Titulo` obrigatório (sem espaços nas pontas); `ID` 0 até a persistência (D-12) | DEL-003 |
| `internal/domain/` | `usuario.go` (`Usuario`, `NovoUsuario`, `ErrNomeVazio`) | Usuário com `ID` `int64` e `Nome` obrigatório, único dado pessoal (não logar) (D-12) | DEL-003 |
| `internal/domain/` | `emprestimo.go` (`Emprestimo`, `NovoEmprestimo`, `Devolver`, `Aberto`, getters, `PrazoEmprestimoDias`, erros sentinela `ErrLivroIDInvalido`, `ErrUsuarioIDInvalido`, `ErrInicioZero`, `ErrEmprestimoJaDevolvido`, `ErrDevolucaoEmprestimoZero`, `ErrDevolucaoAntesDoInicio`) | Empréstimo com prazo de 14 dias pela data civil em `America/Sao_Paulo`, vencimento às 12:00 SP (D-11); estado encapsulado e status derivado da devolução (D-12). Sem I/O | DEL-003 |
| `internal/domain/` | `emprestimo.go` (`ReidratarEmprestimo`, `ErrEmprestimoIDInvalido`, `ErrVencimentoEmprestimoZero`, `ErrVencimentoNaoPosteriorAoInicio`, `ErrEmprestimoInvalido`) | Reconstrução validada do empréstimo gravado; vencimento normalizado para 12:00 SP pela data civil; `Emprestimo{}` não aceita `Devolver` (D-12, D-13). Sem I/O | DEL-004 |
| `internal/storage/` | `repositorio.go` (`DBTX`, `Repositorio`, `NovoRepositorio`, `InserirLivro`, `BuscarLivro`, `InserirUsuario`, `BuscarUsuario`, `AbrirEmprestimo`, `RegistrarDevolucao`, `BuscarEmprestimoAbertoPorLivro`, sentinelas `ErrLivroNaoEncontrado`, `ErrUsuarioNaoEncontrado`, `ErrEmprestimoNaoEncontrado`, `ErrLivroJaEmprestado`, `ErrJaPersistido`, `ErrEmprestimoDivergente`) | Repositório `pgx/v5` sobre `DBTX` (conexão ou transação, sem pool). Traduz SQLSTATE em sentinelas sem SQL, DSN nem dado identificador; log e resposta só usam `Code` e `ConstraintName` do `PgError` (D-13). Testes de banco com `BIBLIOTECA_TEST_DATABASE_URL`, schema descartável por teste | DEL-004 |
| `internal/domain/` | `emprestimo.go` (`Emprestimo.Multa`, `ErrReferenciaZero`) | Multa derivada do empréstimo via `CalcularMulta`, sem persistência: devolvido usa `Devolucao()` e ignora `ref`; aberto usa `ref` (zero → `ErrReferenciaZero`); `Emprestimo{}` → `ErrEmprestimoInvalido`. Sem relógio no domínio: quem chama fornece `ref` (D-14). Sem I/O | DEL-005 |
| `migrations/` | `0001_esquema_emprestimo.{up,down}.sql`, `migrations.go` (`FS`, `embed`) | Esquema `livro`, `usuario`, `emprestimo`, com FKs `RESTRICT` e índice único parcial `emprestimo_livro_aberto_uq` (R4). SQL puro `NNNN_nome.{up,down}.sql`; o registro de versões é a tabela `migracao_aplicada` (abaixo); regras para próximas migrações em D-13 e D-16 | DEL-004 |
| `internal/config/` | `config.go` (`Carregar(getenv)`, `config.Erro`) | Leitura e validação de `BIBLIOTECA_API_TOKEN` (32 a 512 bytes ASCII visível), `DATABASE_URL`, `ADDR` (padrão `:8080`) e `BIBLIOTECA_DB_MAX_CONNS` (1 a 100, padrão 10, sobrescreve `pool_max_conns`). Só stdlib; mensagens fixas que não ecoam o valor (D-15) | DEL-006 |
| `internal/http/` (pacote `httpapi`) | `handler.go` (`NovoHandler`, interfaces `Repositorio` e `Pinger`), `erros.go` (mapa de erros), `servidor.go` (`NovoServidor`, `Servir`) | Os 5 endpoints (`POST /livros`, `POST /usuarios`, `POST /livros/{id}/emprestimo`, `POST /livros/{id}/devolucao`, `GET /livros/{id}/emprestimo`), `/healthz` e `/readyz` sem auth; contrato JSON (multa string `StringFixed(2)`, instantes RFC 3339 UTC, vencimento `AAAA-MM-DD`); erro JSON com código fixo; relógio injetado; timeouts 5/10/15/60s e shutdown gracioso (D-15) | DEL-006 |
| `internal/http/middleware/` | `middleware.go` (RequestID, Log, Recover, LimitarCorpo, Auth, `CabecalhosJSON`, `ResponderErro`) | Cadeia RequestID → Log → Recover → LimitarCorpo(64 KiB) → mux → Auth; token Bearer estático com sha256 e `ConstantTimeCompare`; log `slog` sem `err.Error()`, URL, headers, corpo nem valor de panic; CSP e `Cache-Control: no-store` (D-15) | DEL-006 |
| `cmd/biblioteca-bairro/` | `main.go` (`main`, `run`, `configurarPool`) | Boot: config → `pgxpool` com Ping de 5s e fail fast → Listen → `Servir`; SIGINT/SIGTERM com shutdown de 20s; sem migração no boot (D-15). Desde a DEL-008, `main` chama `despachar`: sem argumentos é o servidor; `migrate up\|status` é o executor; qualquer outro argumento sai com código 2 (D-16) | DEL-006 |
| `cmd/biblioteca-bairro/` | `migrate.go` (`despachar`, `migrar`, `executarMigracao`, `abrirMigradorReal`, `nomeSeguro`) | Subcomando `migrate up` e `migrate status`: carrega `config.CarregarMigracao`, abre conexão dedicada (descarta `pool_*` da DSN), chama o executor, trata SIGINT/SIGTERM e, ao fechar, espera a limpeza assíncrona do pgx (`CleanupDone`, até 5s). Códigos 0/1/2; mensagens fixas, sem DSN, SQL nem `PgError` (D-16) | DEL-008 |
| `internal/migrate/` | `executor.go` (`Executor`, `Novo`, `Opcoes`, `Subir`, `Status`, `Situacao`, `Estado`, `ChaveLockPadrao`), `fs.go` (`ValidarFS`, `Migracao`, `TemConcurrently`), `erro.go` (`Erro` comparável e sentinelas `Err*`) | Executor de produção, só roll forward: advisory lock, checksum sha256 do `.up.sql`, um arquivo por transação com `lock_timeout` e `statement_timeout` locais, registro em `migracao_aplicada`, baseline fechado, recusa de `CONCURRENTLY`; `Status` somente leitura. Conexão `*pgx.Conn` dedicada (nunca pool) e `fs.FS` injetado (D-16) | DEL-008 |
| `migrations/controle/` | `0000_migracao_aplicada.{up,down}.sql`, `migrations.go` (`Controle`, `embed`) | Tabela de controle `migracao_aplicada` (`versao`, `nome`, `checksum`, `aplicada_em`, CHECKs nomeados). Fora de `migrations.FS` e sem linha própria no registro: bootstrap fora da sequência 1..N, aplicado pelo executor quando a tabela não existe (D-16) | DEL-008 |
| `internal/config/` | `migracao.go` (`CarregarMigracao`, `ConfigMigracao`) | `DATABASE_URL` e os timeouts `BIBLIOTECA_MIGRATE_LOCK_TIMEOUT` (padrão 5s, 100ms a 60s) e `BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT` (padrão 60s, 1s a 30min), `time.ParseDuration`, múltiplo de 1 ms; não exige `BIBLIOTECA_API_TOKEN`; erros `config.Erro` fixos (D-16) | DEL-008 |

## Roteamento por caminho

- `internal/http/` (inclui `middleware/`) e `internal/config/`: `SECURITY: sim`
  (autenticação, entrada externa, erros e log, variáveis com segredo; D-15).
  Já estão em `security_surface_paths` do `.pipeline-claude.json` (DEL-007), e o
  validador força a flag.
- `internal/migrate/` e `cmd/`: `SECURITY: sim` (o executor roda DDL com o papel
  de migração, lê `DATABASE_URL` e trata erro de banco sem vazar; D-16) e
  `MIGRATION: sim` para `internal/migrate/`. **Ainda NÃO estão em
  `security_surface_paths`**, então o validador não força a flag: o triage liga
  `require_security` à mão até o humano acrescentá-los em
  `.pipeline-claude.json`, fora da sessão (pendência da DEL-008).
- `internal/auth/`: `SECURITY: sim`.
- `migrations/` (inclui `migrations/controle/`): `SECURITY: sim` (superfície de
  segurança), `MIGRATION: sim` e `migration-reviewer`, se habilitado. Seguir as
  regras de migração da D-13 e da D-16 (`docs-ai/RUNBOOK-MIGRACAO.md`); down
  destrutivo exige justificativa no report. O SQL de `migrations/controle/` é o
  bootstrap do executor: mudança nele afeta a validação de estrutura em
  `internal/migrate/`.
- `internal/storage/`: `SECURITY: sim` (superfície de segurança); avaliar
  `PERFORMANCE: sim` (índices, N+1) e `query-performance`, se habilitado.
  Mudança de consulta confere o índice que a atende no esquema de
  `migrations/`. `*pgxpool.Pool` satisfaz `DBTX` (D-15).
- `.github/workflows/`: `SECURITY: sim` (serviço Postgres de teste no job
  `gates`, D-13).
- `go.mod` / `go.sum`: `SUPPLY_CHAIN: sim` e `SECURITY: sim`.
- `cmd/`: ciclo de vida do servidor, sinais e desligamento gracioso; subcomando
  `migrate up|status` (D-16).

## Diretórios do padrão

| Diretório / arquivo | Função | Acesso na sessão |
|---|---|---|
| `cmd/`, `internal/`, `migrations/`, `go.mod`, `go.sum`, `Makefile` | Código e build | Só subagentes de `editor_agents` |
| `.claude/`, `.mcp.json`, `.pipeline-claude.json`, `.pipeline-claude.lock` | Governança | Ninguém na sessão; mudança feita fora dela |
| `.github/workflows/`, `.gitlab-ci.yml` | CI | Superfície de segurança |
| `docs-ai/deliveries/` | Reports e índice | Aberto para o fluxo de delivery |
| `docs-ai/` | Decisões e invariantes | Aberto |
