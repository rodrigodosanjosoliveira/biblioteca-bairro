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
| `migrations/` | `0001_esquema_emprestimo.{up,down}.sql`, `migrations.go` (`FS`, `embed`) | Esquema `livro`, `usuario`, `emprestimo`, com FKs `RESTRICT` e índice único parcial `emprestimo_livro_aberto_uq` (R4). SQL puro `NNNN_nome.{up,down}.sql`, sem tabela de versão; regras para próximas migrações em D-13 | DEL-004 |

Ainda não existem: `internal/http/`, `internal/config/`.

## Roteamento por caminho

- `internal/http/middleware/` ou `internal/auth/`: `SECURITY: sim`.
- `migrations/`: `SECURITY: sim` (superfície de segurança), `MIGRATION: sim`
  e `migration-reviewer`, se habilitado. Seguir as regras de migração da D-13;
  down destrutivo exige justificativa no report.
- `internal/storage/`: `SECURITY: sim` (superfície de segurança); avaliar
  `PERFORMANCE: sim` (índices, N+1) e `query-performance`, se habilitado.
  Mudança de consulta confere o índice que a atende no esquema de
  `migrations/`.
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
