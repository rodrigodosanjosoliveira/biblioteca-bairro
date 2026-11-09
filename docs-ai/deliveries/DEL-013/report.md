---
delivery_id: DEL-013
status: merged
data: 2026-11-07
require_security: true
require_release_ops: false
---

# DEL-013 — Leituras da API: GET /livros paginado, GET /livros/{id} e GET /emprestimos/{id}

<!--
Preencha cada campo no lugar de <preencher> e marque o checklist só com
evidência dos handoffs. Com require_security: true, acrescente a seção
Security (título de nível 2) com o campo "- Resultado:". Com
require_release_ops: true, acrescente a seção Release/Ops com o campo
"- Plano de rollback:". O validador reprova placeholder, item de checklist
aberto e marcadores de pendência em caixa alta.
-->

## PM/Triage
- Escopo fechado: GET /livros (listagem keyset por id, limite 1..100 padrão 20), GET /livros/{id} e GET /emprestimos/{id}, autenticados, com consultas de leitura em internal/storage e contrato JSON/erros da D-15; fora: filtros, busca por título, ordenação configurável, listagem de usuários e de empréstimos, GET /usuarios/{id}, status de empréstimo na listagem de livros, migração/índice novo, mudança de domínio, atualização do pipeline-claude v1.1.0-rc.2
- Criterios de aceite: as três rotas autenticadas (401 sem token, repositório não chamado); GET /livros com `limite` 1..100 (padrão 20) e `apos` (id exclusivo, padrão início), ordem por id ascendente, resposta `{"itens":[...],"proximo":<id>|null}` com `proximo` = id do último item entregue e sem total; parâmetro malformado, fora da faixa, desconhecido ou repetido → 400 `parametro_invalido` sem eco do valor; GET /livros/{id} → 200 ou 404 `livro_nao_encontrado`; GET /emprestimos/{id} → 200 para aberto ou devolvido (multa pela D-14) ou 404 `emprestimo_nao_encontrado`; rotas antigas mantêm `emprestimo_aberto_nao_encontrado`; log sem query string; sem migração nem mudança em go.mod/go.sum
- Riscos: enumeração do acervo e dos empréstimos por quem tem o token (operador único, D-15/D-17); resposta de até cerca de 6 MB por página no pior caso; rejeição de cache-buster (`?_=`); `proximo` acima de 2^53 perde precisão em JS; off-by-one do cursor e ORDER BY só detectáveis com banco; interface Repositorio cresce (fakes de teste)

## Architecture/Guardrails
- Invariantes aplicaveis: só stdlib (net/url, strconv) e pgx já presente, go.mod/go.sum intactos; testes stdlib em tabela e fuzz com `-fuzztime`; contrato e catálogo de erros da D-15 (código e mensagem fixos, sem eco, log sem URL/query); SQL só parametrizado; multa pela D-14 com relógio injetado; sem mudança em internal/domain/, middleware/ ou migrations/; keyset atendido por `livro_pkey` e detalhe por `emprestimo_pkey`, sem índice novo
- Arquivos provaveis de impacto: internal/http/handler.go, internal/http/erros.go, internal/storage/repositorio.go, testes em internal/http/ e internal/storage/, docs-ai/DECISOES.md (D-19) e docs-ai/03-MAPA-RAPIDO-MODULOS.md

## Dev
- Arquivos alterados: internal/storage/repositorio.go, internal/http/erros.go, internal/http/handler.go; testes internal/http/apoio_test.go, handler_test.go, log_test.go, erros_interno_test.go, fuzz_test.go, internal/storage/sem_banco_test.go e os novos internal/http/listagem_test.go, internal/http/banco_listagem_test.go, internal/storage/listagem_test.go; docs-ai/DECISOES.md, docs-ai/03-MAPA-RAPIDO-MODULOS.md; marcação merged da DEL-012 em docs-ai/deliveries/INDEX.md e docs-ai/deliveries/DEL-012/report.md
- Resumo tecnico: storage ganha `LimiteListagemLivros = 100`, `ErrListagemInvalida`, `ListarLivros(ctx, apos, limite)` (keyset `WHERE id > $1 ORDER BY id LIMIT $2` com $2 = limite+1, recusa parâmetros inválidos antes do SQL, slice vazia não nil, rows.Err checado) e `BuscarEmprestimo(ctx, id)` (aberto ou devolvido). httpapi: interface Repositorio +3 métodos (BuscarLivro, ListarLivros, BuscarEmprestimo); rotas GET /livros, GET /livros/{id}, GET /emprestimos/{id} atrás de auth; `lerParametrosListagem` com url.ParseQuery e erro tratado, só chaves limite/apos uma vez cada, limite em 1..storage.LimiteListagemLivros sem clamp, apos via parseID; `proximo` = id do último item entregue; códigos `parametro_invalido` 400 e `emprestimo_nao_encontrado` 404 (mapeamento local em obterEmprestimo, classificar() intacto); ErrListagemInvalida em errosDeBug
- Testes criados/ajustados: escritos antes da implementação pelo test-writer (33 mutantes mortos, 0 sobreviventes); tabela de parâmetros (54 casos), contrato de itens/proximo, mapa de erros, rotas antigas mantêm emprestimo_aberto_nao_encontrado, DTOs de detalhe com multa aberto/devolvido/renovado, log sem query, FuzzParametrosListagem com oráculo independente, paginação ponta a ponta no banco (limites 1..6 e 100, página exata, apos além do último), ListarLivros com buraco de id e heap fora de ordem, percurso completo, TestListarLivrosErroDuranteLeitura; casos de handler_test.go/log_test.go trocados por rotas que continuam inválidas (404/405)
- Evidencias (codigo/teste): sensores do dev todos ok (vet, test 1830 pass/0 fail/207 skip sem banco, build, vuln "No vulnerabilities found.", mod_verify "all modules verified"); suíte com banco do dev (Postgres 16 descartável, BIBLIOTECA_TEST_DB_OBRIGATORIO=1, `go test -count=1 -p 1 -json ./...`): exit 0, 2211 pass, 0 fail, 0 skip; fuzz `go test -run '^$' -fuzz=FuzzParametrosListagem -fuzztime=30s ./internal/http`: 262417 execs, PASS, sem crasher; gofmt -l vazio; go.mod/go.sum sem diff

## Code Review
- Resultado: aprovado sem bloqueios (diff lido por inteiro, NAO_FAZER do architecture-guardrails conferido item por item; performance conferida: keyset pela PK, no máximo 101 linhas, sem OFFSET/COUNT/N+1)
- Bloqueios e sugestões: nenhum bloqueio. Sugestões: (1) tratar `temMais` com página vazia como bug interno em vez de `proximo` null (handler.go:354) — não implementada; (2) comentário explicando o `defer rows.Close()` junto do `rows.Close()` explícito (repositorio.go:148) — não implementada; (3) registrar que o teto mora no storage e o handler o reaproveita — atendida na D-19 (docs-ai/DECISOES.md); (4) resposta de até cerca de 6 MB no pior caso, correção em delivery própria — não implementada, risco aceito na D-19

## QA
- Resultado: ok. Sensores executados pelo qa-validation todos ok (vet; test 1830 pass, 0 fail, 207 skip por falta de BIBLIOTECA_TEST_DATABASE_URL; build; vuln "No vulnerabilities found."; mod_verify "all modules verified"). Prova com banco executada pela sessão principal, não pelo QA: Postgres 16 descartável em trust, mesmo digest do CI, 2026-11-08T00:38:39Z, `BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 -p 1 -json ./...` → exit 0, 2211 pass, 0 fail, 0 skip, 8 pacotes; o QA conferiu o JSON (nenhum "Action":"fail"/"skip", 8 pacotes pass, testes de banco da delivery pass). Fuzz de 30s pela evidência do dev-implementation. Sem scope creep
- Cenarios executados: GET /livros (tabela de 54 casos de parâmetros, sem eco, contrato itens/proximo, paginação ponta a ponta no banco com limites 1..6 e 100, página exata, apos além do último, fuzz); GET /livros/{id} (id de path, DTO, query ignorada, 404 livro_nao_encontrado no banco, 404/405 do mux); GET /emprestimos/{id} (aberto, devolvido e renovado com multa, 404 emprestimo_nao_encontrado no banco, rotas antigas sem regressão); auth 401 nas três rotas sem chamar o repositório; ListarLivros/BuscarEmprestimo no storage (buraco de id, heap fora de ordem, percurso completo, vazio, recusas antes do SQL, rows.Err); catálogo de erros e ErrListagemInvalida como bug_interno; log sem query; bordas extras (`=5`, apos = max int64, limite percent-encoded, `;`, id inexistente max int64)

## Docs
- Docs atualizados: docs-ai/DECISOES.md (D-19 nova: contrato de paginação por cursor, códigos parametro_invalido e emprestimo_nao_encontrado, teto no storage reaproveitado pelo handler, riscos aceitos; consequência da D-15 atualizada); docs-ai/03-MAPA-RAPIDO-MODULOS.md (linhas DEL-013 de internal/storage/ e internal/http/); docs-ai/deliveries/INDEX.md (DEL-013 em revisão). README.md não alterado: só lista /healthz e /readyz, não as rotas de negócio. CLAUDE.md não alterado (estrutura e invariantes iguais)

## Final Checklist
- [x] `bin/pipeline-claude sensors` verde (todos os sensores do .pipeline-claude.json)
- [x] Escopo coberto por teste
- [x] Nada do fora de escopo foi implementado
- [x] Revisões exigidas pelos sinais concluídas
- [x] Documentação dos módulos tocados atualizada
- [x] Delivery registrada no INDEX.md

## Security
- Resultado: aprovado, nenhum achado CRITICO ou ALTO; govulncheck "No vulnerabilities found.". Dois achados BAIXO aceitos e registrados na D-19: (1) enumeração de empréstimos e exposição de usuario_id por quem tem o token via GET /emprestimos/{id} e do acervo via GET /livros?apos=, dentro do modelo de token único (D-15/D-17), vira IDOR se surgirem vários tokens ou usuários finais; (2) amplificação de resposta, página de até cerca de 6 MB no pior caso (100 títulos de até 64 KiB), rota autenticada. Inspecionados sem achado: auth antes da validação, parsing da query, SQL parametrizado, mensagens fixas sem vazar PgError, log sem query, timeout por operação, ausência de segredos
