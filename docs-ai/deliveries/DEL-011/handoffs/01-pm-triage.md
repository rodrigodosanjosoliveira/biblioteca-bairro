DELIVERY_ID: DEL-011
TASK_CODE: DEL-011
AGENT: pm-triage
STATUS: ok

TIPO: feature

ESCOPO:
  - Migração nova `migrations/0002_*.{up,down}.sql` com contador de renovações em `emprestimo` (default 0, CHECK de faixa nomeado), seguindo D-13/D-16 e `docs-ai/RUNBOOK-MIGRACAO.md`.
  - Domínio (`internal/domain/emprestimo.go`): operação de renovação sobre `Emprestimo` — exige aberto, não vencido no instante de referência injetado, e menos de 2 renovações; estende o vencimento em 14 dias; erros sentinela próprios (vencido, já devolvido, limite atingido). `ReidratarEmprestimo` passa a aceitar/validar o contador. Sem I/O, sem relógio no domínio (D-14).
  - Repositório (`internal/storage/repositorio.go`): persistir a renovação (vencimento + contador) e ler o contador nas buscas existentes; sentinelas sem SQL/DSN (D-13).
  - HTTP (`internal/http/handler.go`, `erros.go`): `POST /livros/{id}/renovacao`, autenticado como os demais, relógio injetado, contrato JSON no padrão D-15; recusa com erro JSON de código fixo para empréstimo vencido, devolvido/sem empréstimo aberto e limite de renovações.
  - Testes em tabela com `testing` em domínio, storage (com banco) e HTTP; atualizar `docs-ai/03-MAPA-RAPIDO-MODULOS.md` no docs-update.

FORA_DE_ESCOPO:
  - Histórico/auditoria de renovações e endpoint GET para ele.
  - Alteração no cálculo de multa (`CalcularMulta`, `Emprestimo.Multa`).
  - Limite de renovações ou prazo configuráveis por variável de ambiente.
  - Identidade do usuário/autorização por dono do empréstimo (a API tem token Bearer único).
  - Notificações, fila de reserva bloqueando renovação.
  - Dockerfile/README (próxima delivery prevista na memória do projeto).
  - Alteração em `internal/migrate/`, `cmd/`, `go.mod`/`go.sum`.

CONFIRMADO:
  - Módulos pelo mapa `docs-ai/03-MAPA-RAPIDO-MODULOS.md`: `internal/domain/emprestimo.go` (DEL-003/004/005), `internal/storage/repositorio.go` (DEL-004), `internal/http/handler.go` e `erros.go` (DEL-006), `migrations/0001_esquema_emprestimo.{up,down}.sql` é a única migração existente (Glob em `migrations/*.sql`), logo a nova é `0002`.
  - `.pipeline-claude.json`: `migrations/`, `internal/storage/` e `internal/http/` estão em `security_surface_paths`; `enabled_modules` = `data-model`, `migration-reviewer`.
  - Nenhuma regra de renovação de empréstimo existe hoje (Grep `renova` só acha o cache do `/readyz` e um teste).
  - `internal/http/handler_test.go:659` tem caso "subrota desconhecida" que espera 404 em `POST /livros/1/renovacao`: esse teste vai quebrar e precisa ser trocado por outra subrota inexistente.
  - Delivery criada com `require_security` (pipeline_delivery_new → DEL-011).

ASSUNCOES:
  - "Mais 14 dias" conta a partir do vencimento atual (não da data da renovação), mantendo vencimento às 12:00 SP pela data civil (D-11). Confirmar no architecture-guardrails.
  - "Não vencido": renovação permitida até o instante do vencimento (12:00 SP do dia de vencimento), exclusivo; no instante exato ou depois é vencido. Coerente com a multa, mas não explicitado na demanda.
  - "No máximo 2 vezes" = até 2 renovações bem-sucedidas (vencimento final = início + 14 + 28 dias); a terceira tentativa é recusada.
  - `{id}` é o ID do livro (como em `/emprestimo` e `/devolucao`); o empréstimo renovado é o aberto daquele livro. Livro sem empréstimo aberto (inclusive o último devolvido) responde com o mesmo erro/código de "sem empréstimo aberto"; não há como renovar "um empréstimo devolvido" pelo ID do livro, então "devolvido" se traduz em ausência de empréstimo aberto. Livro inexistente segue o mapeamento já existente (404).
  - Códigos HTTP e strings de código fixo (ex.: 409/422 com `emprestimo_vencido`, `limite_renovacoes`, `sem_emprestimo_aberto`) ficam para o architecture-guardrails, alinhados a `internal/http/erros.go`.
  - Resposta de sucesso devolve o empréstimo no mesmo contrato do `GET /livros/{id}/emprestimo`, acrescido do número de renovações; o corpo da requisição é vazio.
  - Empréstimos existentes recebem contador 0 na migração (backfill por DEFAULT).
  - Concorrência: duas renovações simultâneas não podem ultrapassar o limite; espera-se UPDATE condicional ou lock de linha no repositório (decisão técnica do architecture-guardrails/data-model).

ARQUIVOS:
  - docs-ai/deliveries/DEL-011/report.md — criado pela ferramenta (escopo e require_security)
  - docs-ai/deliveries/INDEX.md — linha da DEL-011 registrada pela ferramenta

RISCOS:
  - Corrida entre duas renovações (ou renovação e devolução) sobre o mesmo empréstimo: sem condição no UPDATE, o contador pode passar de 2 ou renovar empréstimo já devolvido.
  - `ReidratarEmprestimo` valida vencimento normalizado a partir do início (D-12/D-13); com renovação o vencimento deixa de ser início+14, e a validação/reidratação atual pode rejeitar empréstimos renovados ou o `ErrEmprestimoDivergente` do storage pode disparar. Precisa ser revisto.
  - Migração em tabela com dados: ADD COLUMN com DEFAULT constante e CHECK; garantir `lock_timeout` do executor e down documentado (D-16).
  - Teste de regressão existente (`handler_test.go:659`) assume 404 para essa rota.
  - O working tree já tem mudanças não commitadas da DEL-010 (`docs-ai/deliveries/DEL-010/report.md`, `INDEX.md` via pipeline_index_sync); o git-delivery deve separá-las ou incluí-las conscientemente.

SINAIS:
  SECURITY: sim — toca `migrations/`, `internal/storage/` e `internal/http/` (todos em security_surface_paths); nova entrada externa autenticada.
  SUPPLY_CHAIN: nao — nenhuma dependência nova prevista; `go.mod`/`go.sum` fora do escopo.
  MIGRATION: sim — nova coluna em `emprestimo` via `migrations/0002_*`; aciona data-model e migration-reviewer.
  PERFORMANCE: nao — busca por empréstimo aberto do livro já usa o índice único parcial `emprestimo_livro_aberto_uq`; UPDATE por chave.

PROXIMO: architecture-guardrails
MOTIVO: feature com regra de domínio, persistência e endpoint; precisa de decisão de contrato (códigos de erro, base da extensão do prazo, concorrência, reidratação com vencimento renovado) antes do data-model e do test-writer.
