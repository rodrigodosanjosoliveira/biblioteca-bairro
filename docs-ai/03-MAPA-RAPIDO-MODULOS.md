# Mapa rápido dos módulos — biblioteca-bairro

> Guia para o `pm-triage` e os subagentes técnicos identificarem os módulos
> impactados pelo caminho dos arquivos, sem ler a base de código inteira.
> Atualize quando um módulo nascer, sumir ou mudar de responsabilidade.

---

## Estrutura

```text
├── cmd/
│   └── biblioteca-bairro/
│       └── main.go         # ponto de entrada do servidor HTTP
├── internal/               # código do backend (protegido)
│   ├── http/               # rotas, handlers, serialização e middlewares
│   ├── domain/             # entidades e regras de negócio, sem I/O
│   ├── storage/            # persistência e repositórios (superfície de segurança)
│   └── config/             # leitura e validação de variáveis de ambiente
├── migrations/             # migrações SQL do PostgreSQL (superfície de segurança)
├── docs-ai/                # decisões, invariantes, contrato de handoff e deliveries
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
| `migrations/` | `0001_esquema_emprestimo.{up,down}.sql`, `migrations.go` (`FS`, `embed`) | Esquema `livro`, `usuario`, `emprestimo`, com FKs `RESTRICT` e índice único parcial `emprestimo_livro_aberto_uq` (R4). SQL puro `NNNN_nome.{up,down}.sql`, sem tabela de versão; regras para próximas migrações em D-13 | DEL-004 |
| `internal/config/` | `config.go` (`Carregar(getenv)`, `config.Erro`) | Leitura e validação de `BIBLIOTECA_API_TOKEN` (32 a 512 bytes ASCII visível), `DATABASE_URL`, `ADDR` (padrão `:8080`) e `BIBLIOTECA_DB_MAX_CONNS` (1 a 100, padrão 10, sobrescreve `pool_max_conns`). Só stdlib; mensagens fixas que não ecoam o valor (D-15) | DEL-006 |
| `internal/http/` (pacote `httpapi`) | `handler.go` (`NovoHandler`, interfaces `Repositorio` e `Pinger`), `erros.go` (mapa de erros), `servidor.go` (`NovoServidor`, `Servir`) | Os 5 endpoints (`POST /livros`, `POST /usuarios`, `POST /livros/{id}/emprestimo`, `POST /livros/{id}/devolucao`, `GET /livros/{id}/emprestimo`), `/healthz` e `/readyz` sem auth; contrato JSON (multa string `StringFixed(2)`, instantes RFC 3339 UTC, vencimento `AAAA-MM-DD`); erro JSON com código fixo; relógio injetado; timeouts 5/10/15/60s e shutdown gracioso (D-15) | DEL-006 |
| `internal/http/middleware/` | `middleware.go` (RequestID, Log, Recover, LimitarCorpo, Auth, `CabecalhosJSON`, `ResponderErro`) | Cadeia RequestID → Log → Recover → LimitarCorpo(64 KiB) → mux → Auth; token Bearer estático com sha256 e `ConstantTimeCompare`; log `slog` sem `err.Error()`, URL, headers, corpo nem valor de panic; CSP e `Cache-Control: no-store` (D-15) | DEL-006 |
| `cmd/biblioteca-bairro/` | `main.go` (`run`, `configurarPool`) | Boot: config → `pgxpool` com Ping de 5s e fail fast → Listen → `Servir`; SIGINT/SIGTERM com shutdown de 20s; sem migração no boot (DEL-007) (D-15) | DEL-006 |

## Roteamento por caminho

- `internal/http/` (inclui `middleware/`) e `internal/config/`: `SECURITY: sim`
  (autenticação, entrada externa, erros e log, variáveis com segredo; D-15).
  Ainda não estão em `security_surface_paths` do `.pipeline-claude.json`, então
  o validador não força a flag: o triage liga `require_security` à mão até o
  humano acrescentá-los fora da sessão (H10, pendência da DEL-006).
- `internal/auth/`: `SECURITY: sim`.
- `migrations/`: `SECURITY: sim` (superfície de segurança), `MIGRATION: sim`
  e `migration-reviewer`, se habilitado. Seguir as regras de migração da D-13;
  down destrutivo exige justificativa no report.
- `internal/storage/`: `SECURITY: sim` (superfície de segurança); avaliar
  `PERFORMANCE: sim` (índices, N+1) e `query-performance`, se habilitado.
  Mudança de consulta confere o índice que a atende no esquema de
  `migrations/`. `*pgxpool.Pool` satisfaz `DBTX` (D-15).
- `.github/workflows/`: `SECURITY: sim` (serviço Postgres de teste no job
  `gates`, D-13).
- `go.mod` / `go.sum`: `SUPPLY_CHAIN: sim` e `SECURITY: sim`.
- `cmd/`: ciclo de vida do servidor, sinais e desligamento gracioso.

## Diretórios do padrão

| Diretório / arquivo | Função | Acesso na sessão |
|---|---|---|
| `cmd/`, `internal/`, `migrations/`, `go.mod`, `go.sum`, `Makefile` | Código e build | Só subagentes de `editor_agents` |
| `.claude/`, `.mcp.json`, `.pipeline-claude.json`, `.pipeline-claude.lock` | Governança | Ninguém na sessão; mudança feita fora dela |
| `.github/workflows/`, `.gitlab-ci.yml` | CI | Superfície de segurança |
| `docs-ai/deliveries/` | Reports e índice | Aberto para o fluxo de delivery |
| `docs-ai/` | Decisões e invariantes | Aberto |
