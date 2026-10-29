---
delivery_id: DEL-003
status: merged
data: 2026-10-29
require_security: false
require_release_ops: false
---

# DEL-003 — Modelo de empréstimo: Livro, Usuário e Empréstimo

<!--
Preencha cada campo no lugar de <preencher> e marque o checklist só com
evidência dos handoffs. Com require_security: true, acrescente a seção
Security (título de nível 2) com o campo "- Resultado:". Com
require_release_ops: true, acrescente a seção Release/Ops com o campo
"- Plano de rollback:". O validador reprova placeholder, item de checklist
aberto e marcadores de pendência em caixa alta.
-->

## PM/Triage
- Escopo fechado: tipo feature, opção A (só domínio) por decisão humana. (1) `Livro` como exemplar físico, só com `Titulo` obrigatório; (2) `Usuario` só com `Nome` obrigatório; (3) `Emprestimo` criado com livro, usuário e início, com prazo de 14 dias corridos pela data civil em `America/Sao_Paulo`; (4) devolução do empréstimo, com status aberto derivado. A regra R4 (um empréstimo aberto por livro) fica só documentada, com garantia por índice único parcial na DEL-004. Fora de escopo: migrações, storage, driver, multa no empréstimo, HTTP, config, JSON, renovação, reserva, limites, bloqueio, pagamento, ISBN, feriados e prazo configurável.
- Criterios de aceite: testes em tabela cobrindo os itens 1 a 4, com caso de sucesso e de erro por regra, passando em `go test ./...`; sensores de `bin/pipeline-claude sensors` verdes; fuzz com `-fuzztime=30s` nas regras de validação e de data.
- Riscos: off-by-one de fuso e horário de verão no vencimento; dado pessoal em `Nome`; R4 sem garantia até a DEL-004; estado inconsistente do empréstimo se os campos fossem exportados.
- Decisoes humanas: prazo de 14 dias corridos pela data civil em São Paulo; `Livro` = exemplar físico só com `Titulo`; `Usuario` só com `Nome` (minimizar dado pessoal); opção A (só domínio). Fatiamento: DEL-004 = migrações, repositório, pgx e `ReidratarEmprestimo`; DEL-005 = `Emprestimo.Multa()` com `CalcularMulta`.

## Architecture/Guardrails
- Invariantes aplicaveis: Go 1.24+; testes com a stdlib `testing`, em tabela; sem dependência externa nova (`go.mod`/`go.sum` intocados); nenhum segredo nem dado pessoal em código ou log (erros sem ID, `Nome` ou `Titulo`); `internal/domain` sem I/O; data civil em `America/Sao_Paulo` (D-10).
- Arquivos provaveis de impacto: `internal/domain/livro.go`, `internal/domain/usuario.go`, `internal/domain/emprestimo.go` e os respectivos `_test.go`.
- Contrato: `Livro{ID, Titulo}` com `NovoLivro` e `Usuario{ID, Nome}` com `NovoUsuario` (aplicam `TrimSpace`, erros `ErrTituloVazio`/`ErrNomeVazio`, ID 0 até a persistência); `Emprestimo` com campos não exportados e getters, `PrazoEmprestimoDias = 14`, `NovoEmprestimo(livroID, usuarioID int64, inicio time.Time)` validando nessa ordem; `Devolver` valida, nesta ordem, já devolvido, data zero e devolução antes do início pela data civil em SP (mesmo dia permitido, sem alterar estado em erro); `Aberto()` derivado. Vencimento = 12:00 em `America/Sao_Paulo` da data civil de `inicio` + 14 dias (o meio-dia evita a meia-noite inexistente do horário de verão de 04/11/2018 e mantém `CalcularMulta(e.Vencimento(), e.Devolucao())` correto). IDs `int64` (identity), sem UUID.

## Dev
- Arquivos alterados: `internal/domain/livro.go`, `internal/domain/livro_test.go`, `internal/domain/usuario.go`, `internal/domain/usuario_test.go`, `internal/domain/emprestimo.go`, `internal/domain/emprestimo_test.go` (todos novos).
- Resumo tecnico: construtores `NovoLivro` e `NovoUsuario` com `TrimSpace` e erro sentinela para valor vazio; `NovoEmprestimo` valida IDs e início e calcula o vencimento ao meio-dia em São Paulo da data civil + 14 dias; `Devolver` registra a devolução com validação e sem efeito colateral em erro; `Aberto()` deriva o status da devolução zerada. Erros sentinela sem dado identificador.
- Testes criados/ajustados: testes em tabela para os três tipos, cobrindo fusos UTC, UTC+9 e São Paulo, 23h30 BRT, viradas de mês e ano, ano bissexto, horário de verão 2018/2019 e compatibilidade com `CalcularMulta`; fuzz `FuzzNovoEmprestimoVencimento` (oráculo independente com `AddDate`) e `FuzzNomeObrigatorio`. O `test-writer` validou os testes contra uma implementação descartável e reprovou cerca de 15 mutantes.
- Evidencias (codigo/teste): `go test ./...` verde; fuzz com `-fuzztime=30s` executado pelo `dev-implementation`: `FuzzNovoEmprestimoVencimento` cerca de 3,5 milhões, `FuzzNomeObrigatorio` cerca de 3,6 milhões e `FuzzCalcularMulta` cerca de 3,5 milhões de execuções, sem falha.
- Code review: sem bloqueio. Sugestões não aplicadas: guarda do zero-value `Emprestimo{}` em `Devolver` (adiada para quando houver `ReidratarEmprestimo`, DEL-004); receptores mistos (valor e ponteiro); comparar a struct inteira no teste; texto "em em" no comentário de `Devolver`.
- Supply chain e security: não aplicáveis; `go.mod` e `go.sum` intocados (sinal `SUPPLY_CHAIN: nao`) e o diff não toca `security_surface_paths` (sinal `SECURITY: nao`).

## QA
- Resultado: aprovado; itens 1 a 4 do escopo cobertos com caso de sucesso e de erro por regra.
- Cenarios executados: sensores 5/5 ok: `vet`, `test`, `build`, `vuln` ("No vulnerabilities found.") e `mod_verify` ("all modules verified"); fuzz com `-fuzztime=30s` executado pelo `dev-implementation` (o QA não reexecutou, sem shell). Bordas sem teste: `Devolver` com `inicio` fora do fuso de São Paulo; zero-value `Emprestimo{}`.
- Riscos residuais: zero-value `Emprestimo{}` aceita `Devolver` até existir `ReidratarEmprestimo` (DEL-004); o meio-dia em São Paulo é convenção, e na DEL-004 o vencimento deve ser coluna `DATE` reidratada ao meio-dia em São Paulo; IDs sequenciais permitem enumeração quando expostos via HTTP; `Nome` é dado pessoal e não deve ir para log; R4 sem garantia até o índice único parcial da DEL-004.

## Docs
- Docs atualizados: `docs-ai/DECISOES.md` (D-11, vencimento às 12:00 em `America/Sao_Paulo` da data civil + 14 dias, coluna `DATE` na persistência; D-12, campos não exportados no `Emprestimo` com reidratação validada na DEL-004, status derivado da devolução, IDs `int64` e R4 garantida por índice único parcial `WHERE devolucao IS NULL` na DEL-004); `docs-ai/03-MAPA-RAPIDO-MODULOS.md` (seção Módulos existentes com `livro.go`, `usuario.go` e `emprestimo.go` em `internal/domain/`). Os símbolos exportados novos já têm comentário de documentação no código. `CLAUDE.md` sem mudança (nenhuma estrutura nem invariante global alterado).

## Final Checklist
- [x] `bin/pipeline-claude sensors` verde (vet, test, build)
- [x] Escopo coberto por teste
- [x] Nada do fora de escopo foi implementado
- [x] Revisões exigidas pelos sinais concluídas
- [x] Documentação dos módulos tocados atualizada
- [x] Delivery registrada no INDEX.md
