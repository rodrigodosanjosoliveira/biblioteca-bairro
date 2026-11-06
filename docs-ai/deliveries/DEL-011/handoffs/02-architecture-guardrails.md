DELIVERY_ID: DEL-011
TASK_CODE: DEL-011
AGENT: architecture-guardrails
STATUS: ok

ABORDAGEM:
  Migração (`migrations/0002_renovacao_emprestimo.{up,down}.sql`):
    - up: um único `ALTER TABLE emprestimo ADD COLUMN renovacoes integer NOT NULL DEFAULT 0, ADD CONSTRAINT emprestimo_renovacoes_check CHECK (renovacoes >= 0 AND renovacoes <= 2);` (DEFAULT constante = sem rewrite no PG 11+; o CHECK faz uma varredura sob ACCESS EXCLUSIVE, aceita pelo tamanho da tabela; `lock_timeout`/`statement_timeout` vêm do executor D-16). COMMENT opcional na coluna. Nada de SET, BEGIN/COMMIT, `concurrently`.
    - down: `ALTER TABLE emprestimo DROP CONSTRAINT emprestimo_renovacoes_check, DROP COLUMN renovacoes;` (destrutivo: perde o contador; os vencimentos estendidos ficam, o que é coerente). Manual, conforme D-13/D-16.
  Domínio (`internal/domain/emprestimo.go`):
    - Constantes novas `LimiteRenovacoes = 2` e `PrazoRenovacaoDias = 14` (separada de `PrazoEmprestimoDias`, mesmo valor).
    - Campo não exportado `renovacoes int` e getter `Renovacoes() int`.
    - `NovoEmprestimo` inalterado (renovacoes = 0).
    - `ReidratarEmprestimo` ganha o parâmetro final `renovacoes int`: `ReidratarEmprestimo(id, livroID, usuarioID int64, inicio, vencimento, devolucao time.Time, renovacoes int)`. Validação nova, por último na ordem atual: `0 <= renovacoes <= LimiteRenovacoes`, senão sentinela nova `ErrRenovacoesInvalidas`. Continua sem exigir vencimento = início + 14*(1+n) (D-13 "Reidratação").
    - Método `func (e *Emprestimo) Renovar(ref time.Time) error`. Ordem: (1) início zero → `ErrEmprestimoInvalido`; (2) não aberto → `ErrEmprestimoJaDevolvido` (reuso); (3) ref zero → `ErrReferenciaZero` (reuso); (4) vencido → `ErrEmprestimoVencido` (nova); (5) `renovacoes >= LimiteRenovacoes` → `ErrLimiteRenovacoes` (nova). Em erro não altera nada. Em sucesso: vencimento = 12:00 America/Sao_Paulo da data civil (SP) do vencimento atual + `PrazoRenovacaoDias` (mesma construção `time.Date(a, m, d+N, 12, ...)` de `NovoEmprestimo`); renovacoes++.
    - "Vencido" = `meiaNoiteUTC(ref) > meiaNoiteUTC(e.vencimento)`: dia civil SP de ref posterior ao do vencimento. Mesma fronteira em que `CalcularMulta` começa a cobrar. Sem relógio no domínio (D-14).
  Storage (`internal/storage/repositorio.go`):
    - `colunasEmprestimo` passa a incluir `renovacoes`; `lerEmprestimo` faz Scan dele e passa a `ReidratarEmprestimo`. Isso cobre `AbrirEmprestimo`, `RegistrarDevolucao` e `BuscarEmprestimoAbertoPorLivro` sem mudar os SQL de escrita deles (o INSERT usa o DEFAULT 0).
    - Método novo `RegistrarRenovacao(ctx, e domain.Emprestimo) (domain.Emprestimo, error)`, espelhando `RegistrarDevolucao`: antes de qualquer SQL, recusa com `domain.ErrEmprestimoInvalido` se `e.ID() <= 0`, `!e.Aberto()`, `!valido(e)` ou `e.Renovacoes() < 1`. UPDATE condicional (concorrência otimista; o contador funciona como versão):
      `UPDATE emprestimo SET vencimento = $2, renovacoes = $3 WHERE id = $1 AND devolucao IS NULL AND inicio = $4 AND renovacoes = $3 - 1 RETURNING colunasEmprestimo`, com `vencimentoParaDate(e.Vencimento())`.
      Sem linha: diagnóstico como em `RegistrarDevolucao` — inexistente → `ErrEmprestimoNaoEncontrado`; devolvido → `domain.ErrEmprestimoJaDevolvido`; qualquer outro caso (início ou contador divergente, corrida perdida) → `ErrEmprestimoDivergente`. O CHECK do banco é a segunda barreira contra contador > 2.
  HTTP (`internal/http/handler.go`, `erros.go`):
    - `Repositorio` (interface no consumidor) ganha `RegistrarRenovacao`.
    - Rota `mux.Handle("POST /livros/{id}/renovacao", auth(http.HandlerFunc(a.renovar)))`. Handler igual a `devolver`: `parseID` → `exigirCorpoVazio` → `agora := a.agora()` → contexto com `a.timeout` → `BuscarEmprestimoAbertoPorLivro` → `e.Renovar(agora)` → `RegistrarRenovacao` → `responderEmprestimo(w, r, http.StatusOK, got, agora)`.
    - `saidaEmprestimo` ganha `Renovacoes int \`json:"renovacoes"\`` (campo aditivo, vale para todas as respostas de empréstimo).
    - Códigos novos no catálogo: `codEmprestimoVencido = "emprestimo_vencido"` → 409 "empréstimo vencido"; `codLimiteRenovacoes = "limite_renovacoes_atingido"` → 409 "limite de renovações atingido". `classificar` mapeia `domain.ErrEmprestimoVencido` e `domain.ErrLimiteRenovacoes`. Sem empréstimo aberto (inclusive livro inexistente ou já devolvido) usa o código existente `emprestimo_aberto_nao_encontrado` 404, como a devolução; corrida → `emprestimo_ja_devolvido` 409 ou `emprestimo_alterado` 409, já existentes.
    - `domain.ErrRenovacoesInvalidas` entra em `errosDeBug` (dado gravado inválido → 500 `causa=bug_interno`).
  Testes (test-writer): tabela em `Renovar` (fronteiras do dia civil SP, inclusive 23:59 do dia do vencimento permitido e 00:00 do seguinte recusado, horário de verão histórico, 3ª tentativa, ref zero, Emprestimo{}, devolvido, não-mutação em erro); `ReidratarEmprestimo` com -1, 0, 2, 3; atualizar todas as chamadas existentes de `ReidratarEmprestimo` (24 ocorrências em 7 arquivos) para o novo parâmetro; seed/arg novo no `FuzzReidratarEmprestimo` e fuzz novo de `Renovar` (invariante 1.3); storage com banco: renovação ok, contador lido, CAS perdido → `ErrEmprestimoDivergente`, devolvido → `ErrEmprestimoJaDevolvido`, inexistente, recusas pré-SQL, CHECK rejeitando 3 por SQL direto; HTTP: `repoFake` em `internal/http/apoio_test.go` implementa `RegistrarRenovacao`, casos 200/404/409×4/400 corpo/401, e o caso `handler_test.go:659` troca o caminho para outra subrota inexistente (ex.: `/livros/1/inexistente`).

NAO_FAZER:
  - Não usar `SELECT ... FOR UPDATE`, transação explícita no handler nem `pgx.Tx` novo: a concorrência é resolvida pelo UPDATE condicional com `renovacoes = $3 - 1`, como `RegistrarDevolucao`.
  - Não incrementar no SQL (`renovacoes = renovacoes + 1`) nem calcular o vencimento novo no SQL (`vencimento + 14`): a regra fica no domínio; o banco só grava e confere.
  - Não usar o instante 12:00 SP como fronteira de "vencido" (`ref.Before(e.vencimento)`): D-11 diz que o horário é convenção; a fronteira é o dia civil.
  - Não contar os 14 dias a partir de `ref`/agora: a base é o vencimento atual.
  - Não exigir em `ReidratarEmprestimo` coerência entre vencimento, início e contador.
  - Não criar `BuscarLivro` extra no handler para distinguir livro inexistente de livro sem empréstimo (a devolução também não faz).
  - Não criar códigos de erro novos para "sem empréstimo aberto" ou "já devolvido" (`sem_emprestimo_aberto` sugerido no triage): reusar `emprestimo_aberto_nao_encontrado` e `emprestimo_ja_devolvido`.
  - Não usar 422 para vencido ou limite: são conflito com o estado do recurso, 409 como `emprestimo_ja_devolvido` (D-15).
  - Não aceitar corpo na renovação nem data vinda do cliente (D-15 H4).
  - Não criar tabela de histórico, coluna de data da renovação, nem tornar limite/prazo configurável por env.
  - Não separar o CHECK em `NOT VALID` + `VALIDATE` em outra migração, nem usar `smallint`/`bigint` ou coluna nullable.
  - Não alterar `migrations/0001_*`, `internal/migrate/`, `cmd/`, `go.mod`, `go.sum`, `CalcularMulta` nem `Emprestimo.Multa`.
  - Não adicionar dependência (biblioteca de asserção, ferramenta de migração).

CONFIRMADO:
  - `internal/domain/emprestimo.go:92-127`: `ReidratarEmprestimo` só exige vencimento civil posterior ao início, não início+14. O RISCO do triage de rejeitar empréstimo renovado não se confirma; a mudança necessária é só o parâmetro do contador.
  - `internal/domain/multa.go:52`: `meiaNoiteUTC` já faz a conversão para dia civil SP usada na regra de "vencido".
  - `internal/storage/repositorio.go:185-218`: `RegistrarDevolucao` já usa UPDATE condicional com diagnóstico em três ramos; `RegistrarRenovacao` segue o mesmo padrão. `lerEmprestimo` (l. 242) é o único ponto de Scan, logo o contador entra em todas as leituras por ele.
  - `internal/storage/repositorio.go:222-232`: `BuscarEmprestimoAbertoPorLivro` não consulta `livro`, então livro inexistente já dá `emprestimo_aberto_nao_encontrado` 404 na devolução (`internal/http/erros.go:101-102`).
  - `internal/http/erros.go:17-67`: catálogo atual e status; 409 já é usado para conflito de estado.
  - `internal/http/handler.go:38-44,125-132,215-223,319-347`: interface `Repositorio`, mux, DTO e o handler `devolver`, que é o molde do `renovar`.
  - `internal/http/apoio_test.go:142-168`: `repoFake` implementa a interface e vai precisar do método novo.
  - `internal/http/handler_test.go:659` espera 404 em `POST /livros/1/renovacao`.
  - Grep `ReidratarEmprestimo\(`: 24 ocorrências em 7 arquivos (domain, storage, http apoio_test), todas afetadas pela assinatura.
  - Grep `^func Fuzz`: `FuzzReidratarEmprestimo` em `internal/domain/reidratar_test.go:335`.
  - `migrations/0001_esquema_emprestimo.up.sql`: constraints nomeadas no padrão `emprestimo_*_check`; `0001.down` é `DROP TABLE`.
  - `docs-ai/DECISOES.md` D-11, D-13 (regras de migração, reidratação, erros), D-14 (sem relógio), D-15 (contrato, 400/422/409), D-16 e `docs-ai/RUNBOOK-MIGRACAO.md` §6-7.

ASSUNCOES:
  - Resolvido por decisão técnica (registrar em DECISOES.md no docs-update como D-17; o humano confirma no PR):
    - Base da extensão: vencimento atual + 14 dias civis, às 12:00 SP. Contar a partir de "agora" encurtaria o prazo numa renovação antecipada, o que contradiz "mais 14 dias".
    - "Não vencido": a renovação vale até o fim do dia civil (SP) do vencimento. Isso substitui a suposição do triage (instante 12:00 exclusivo), que contradiz D-11 e daria "vencido sem multa" entre 12:00 e 23:59.
    - "No máximo 2 vezes" = 2 renovações bem-sucedidas; vencimento final = data civil do início + 42 dias; a 3ª tentativa leva 409 `limite_renovacoes_atingido`.
    - Quando valem as duas recusas (vencido e no limite), ganha `emprestimo_vencido` (ordem do `Renovar`).
    - `{id}` é o ID do livro; "devolvido" chega como ausência de empréstimo aberto (404 `emprestimo_aberto_nao_encontrado`), e só numa corrida como 409 `emprestimo_ja_devolvido`.
    - Sucesso: 200 com o DTO de empréstimo acrescido de `renovacoes`; o corpo precisa ser vazio.
    - Backfill pelo DEFAULT 0.
  - A tabela `emprestimo` é pequena (biblioteca de bairro), então a varredura do CHECK sob ACCESS EXCLUSIVE dentro do `lock_timeout` de 5s é aceitável. Não medi o volume de produção; o migration-reviewer confirma.
  - Nenhuma suposição exige decisão humana antes de seguir.

ARQUIVOS:
  - nenhum (revisor sem escrita). Previstos para dev/test: migrations/0002_renovacao_emprestimo.up.sql, migrations/0002_renovacao_emprestimo.down.sql, internal/domain/emprestimo.go (+ testes), internal/storage/repositorio.go (+ testes), internal/http/handler.go, internal/http/erros.go (+ apoio_test.go, handler_test.go), docs-ai/DECISOES.md e docs-ai/03-MAPA-RAPIDO-MODULOS.md no docs-update.

RISCOS:
  - Mudança de contrato da API: rota nova `POST /livros/{id}/renovacao` (antes 404); campo novo `renovacoes` em todas as respostas de empréstimo (GET/POST emprestimo e devolução). É aditivo, mas cliente com decodificação estrita quebra. Códigos novos `emprestimo_vencido` e `limite_renovacoes_atingido`, ambos 409.
  - O CHECK `renovacoes <= 2` fixa o limite de negócio no banco: mudar o limite exige migração nova (aceito, porque limite configurável está fora de escopo).
  - Deploy: o binário novo lê `renovacoes` em toda busca. Se subir antes do `migrate up` 0002, todas as rotas de empréstimo dão 500 `causa=banco` (42703). A ordem é migrate antes do deploy (D-16), e o down só depois de reverter o binário.
  - A devolução concorrente com a renovação não confere o contador. Se a renovação ganhar, a devolução grava sobre o vencimento novo, e a multa sai do RETURNING, o que é correto. Se a devolução ganhar, a renovação cai no diagnóstico `emprestimo_ja_devolvido`. Isso precisa de teste de storage.
  - O working tree tem mudanças não commitadas da DEL-010 (report/INDEX); o git-delivery decide.

SINAIS:
  SECURITY: sim — entrada externa autenticada nova e mudança em `internal/http/`, `internal/storage/` e `migrations/` (security_surface_paths).
  SUPPLY_CHAIN: nao — sem dependência nova; `go.mod`/`go.sum` intocados.
  MIGRATION: sim — `migrations/0002_*` adiciona coluna NOT NULL DEFAULT e CHECK em `emprestimo`; aciona data-model e migration-reviewer.
  PERFORMANCE: nao — UPDATE por PK e busca pelo índice parcial `emprestimo_livro_aberto_uq`; uma coluna integer a mais no SELECT.

PROXIMO: data-model
MOTIVO: `data-model` está em enabled_modules e a demanda toca persistência (coluna nova, CHECK, UPDATE condicional); ele valida o esquema da 0002 e o contrato de `RegistrarRenovacao` antes do test-writer.
