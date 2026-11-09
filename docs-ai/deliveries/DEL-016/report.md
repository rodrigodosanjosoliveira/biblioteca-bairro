---
delivery_id: DEL-016
status: em revisão
data: 2026-11-09
require_security: true
require_release_ops: false
---

# DEL-016 — Coleção e environment do Postman com todos os endpoints da API

<!--
Preencha cada campo no lugar de <preencher> e marque o checklist só com
evidência dos handoffs. Com require_security: true, acrescente a seção
Security (título de nível 2) com o campo "- Resultado:". Com
require_release_ops: true, acrescente a seção Release/Ops com o campo
"- Plano de rollback:". O validador reprova placeholder, item de checklist
aberto e marcadores de pendência em caixa alta.
-->

## PM/Triage
- Escopo fechado: Coleção Postman v2.1 com os 15 endpoints de internal/http/handler.go (healthz, readyz, livros, usuários, empréstimos, devolução, renovação, listagens com limite/apos/filtros) + environment versionado só com placeholders (base_url, token vazio, ids) + instruções de uso no README; inclui a marcação merged da DEL-015 no INDEX/report; fora: qualquer mudança em código Go, rotas, contrato JSON, autenticação, CI, OpenAPI/Swagger, Newman no CI e token real em arquivo versionado
- Criterios de aceite: coleção Postman v2.1.0 em `postman/` cobrindo as 15 rotas de `internal/http/handler.go:135-149` com métodos, paths, query params e campos JSON iguais aos do handler; auth Bearer `{{token}}` na coleção e `noauth` em `/healthz` e `/readyz`; query params opcionais desligados por padrão; environment versionado com `token` vazio (tipo secret) e sem segredo real; cópia local `postman/*.local.postman_environment.json` ignorada no `.gitignore`; scripts só de captura de ids e cursor, sem asserções; sem exemplos de resposta; seção de uso no README (decisões P1 a P4 do usuário)
- Riscos: vazamento do token se o usuário gravá-lo no environment versionado em vez da cópia `.local` (o `.gitignore` só cobre o nome `.local`); divergência futura entre a coleção (cópia manual) e as rotas; campo ou nome errado na coleção só aparece ao usar; parâmetro de query ligado com valor vazio ou cursor apagado dá 400 `parametro_invalido`

## Architecture/Guardrails
- Invariantes aplicaveis: invariante 5 do CLAUDE.md (nada de segredo em código nem em log: environment versionado com `token` vazio); etapa architecture-guardrails não acionada, porque o triage classificou a delivery como tipo docs, sem código Go nem caminho protegido, e roteou direto para o docs-update
- Arquivos provaveis de impacto: `postman/biblioteca-bairro.postman_collection.json`, `postman/biblioteca-bairro.postman_environment.json`, `.gitignore`, `README.md`, `docs-ai/03-MAPA-RAPIDO-MODULOS.md`, `docs-ai/DECISOES.md`, além da marcação merged da DEL-015 em `docs-ai/deliveries/DEL-015/report.md` e `docs-ai/deliveries/INDEX.md`

## Dev
- Arquivos alterados: `postman/biblioteca-bairro.postman_collection.json` (novo), `postman/biblioteca-bairro.postman_environment.json` (novo), `.gitignore` (padrão `postman/*.local.postman_environment.json`), `README.md` (seção Postman); nenhum arquivo em `cmd/`, `internal/`, `migrations/`, `go.mod`, `go.sum` ou `Makefile`. Produzidos pelo docs-update, sem etapa dev-implementation (tipo docs)
- Resumo tecnico: coleção com pastas Saúde (Healthz, Readyz), Livros (Criar, Listar, Obter), Usuários (Criar, Listar, Obter, Listar empréstimos do usuário) e Empréstimos (Abrir, Consultar aberto do livro, Renovar, Devolver, Listar, Obter); corpos `{"titulo"}`, `{"nome"}` e `{"usuario_id"}` conforme `entradaLivro`, `entradaUsuario` e `entradaEmprestimo`; renovação e devolução sem corpo (`exigirCorpoVazio`); query de `/livros` e `/usuarios` só `limite`/`apos`, de `/emprestimos` com `situacao`, `usuario_id`, `livro_id`, e da rota aninhada sem `usuario_id`, todos desligados; descrição por request com os códigos de `internal/http/erros.go` e o 401 `nao_autorizado` do middleware; scripts de pós-resposta gravam `livro_id`, `usuario_id`, `emprestimo_id` nos 201 e `cursor_*` a partir de `proximo` nos 200 (apagam quando null); environment com `base_url` `http://localhost:8080`, `token` vazio do tipo secret, ids e quatro cursores vazios
- Testes criados/ajustados: nenhum; a delivery não tem código Go e os scripts Postman são só de captura, sem asserções (decisão P3)
- Evidencias (codigo/teste): rotas conferidas em `internal/http/handler.go:135-149`, corpos em `handler.go:207-217`, parâmetros em `lerQueryListagem`/`lerParametrosEmprestimos` (`handler.go:654-736`), códigos e status em `internal/http/erros.go:17-82`, formato de erro e 401 em `internal/http/middleware/middleware.go:80-86,254-283`; os dois JSON foram escritos e conferidos estruturalmente pelo docs-update (sem shell, sem parser executado)

## Code Review
- Resultado: code-reviewer não acionado: a delivery não tem diff de código Go (tipo docs, roteamento do triage)
- Bloqueios e sugestões: nenhum bloqueio registrado; o triage pede que o QA confira cada request da coleção contra `internal/http/handler.go`

## QA
- Resultado: ok (05-qa-validation, que substitui o `parcial` de 04-qa-validation): sensores verdes (vet, test, build, vuln "No vulnerabilities found.", mod_verify "all modules verified"); suíte com banco (Postgres 16 do digest do CI, `BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 -p 1 -json ./...`) com exit 0, 3019 pass, 0 fail e 0 skip; nenhum achado bloqueante e nenhum scope creep
- Cenarios executados: conferência request a request das 15 rotas de `internal/http/handler.go:135-149` contra as 15 requests da coleção (método, path, query, corpo, auth); execução ao vivo da coleção contra a API real com Postgres e migrate up 0001-0003, 15/15 OK com ids encadeados pelos scripts; token vazio dá 401 `nao_autorizado` nas rotas protegidas e 200 em healthz/readyz; paginação e filtros ao vivo nas 4 listagens (`limite=1` com `proximo` gravado no cursor, depois `apos` com `proximo` null e cursor removido; `situacao` e `usuario_id` em `/emprestimos`, `situacao` em `/usuarios/{id}/emprestimos`); `apos=` vazio dá 400 `parametro_invalido`; `jq empty` nos dois JSON e `git check-ignore` da cópia `.local`
- Riscos remanescentes: achados BAIXO do security-review (token gravado no environment versionado, em vez da cópia `.local`, barrado só por revisão, e sincronização do initial value no Postman Cloud; `base_url` em `http://` mandaria o Bearer em claro a host remoto); coleção mantida à mão (D-22), rota nova diverge sem aviso; filtro `livro_id` coberto só de forma estática e por `TestBancoListagemEmprestimosFiltros`; o sensor local `test` pula os testes de banco sem `BIBLIOTECA_TEST_DATABASE_URL` (o CI cobre com `BIBLIOTECA_TEST_DB_OBRIGATORIO=1`)

## Docs
- Docs atualizados: `README.md` (seção Postman: copiar o environment para o nome `.local`, importar, preencher o token só no valor atual, ordem sugerida, query params desligados e, no passe final, a frase do QA 05 sobre ligar `apos` só com cursor preenchido), descrição de "Abrir empréstimo" na coleção com 413 `corpo_muito_grande` (cosmético do QA 04), `docs-ai/03-MAPA-RAPIDO-MODULOS.md` (diretório `postman/` na estrutura), `docs-ai/DECISOES.md` (D-22 com as decisões P1 a P4); `CLAUDE.md` sem mudança (estrutura de código e invariantes inalterados)

## Final Checklist
- [x] `bin/pipeline-claude sensors` verde (todos os sensores do .pipeline-claude.json)
- [x] Escopo coberto por teste
- [x] Nada do fora de escopo foi implementado
- [x] Revisões exigidas pelos sinais concluídas
- [x] Documentação dos módulos tocados atualizada
- [x] Delivery registrada no INDEX.md

## Security
- Resultado: aprovado (03-security-review), sem achado CRITICO, ALTO ou MEDIO; govulncheck via sensor `vuln` com exit 0 e "No vulnerabilities found."; environment com `token` vazio do tipo secret e nenhum segredo versionado (grep por DSN, senha, token e Bearer literal sem ocorrência); `.gitignore` cobre `postman/*.local.postman_environment.json` sem negação posterior; scripts só fazem `pm.environment.set/unset` de ids e cursores, sem log nem leitura do token; `noauth` só em healthz e readyz, como no handler. Achados BAIXO, não bloqueantes: (1) o `.gitignore` não impede gravar o token no environment versionado nem o Postman Cloud de sincronizar o initial value, mitigado por README e D-22 (opcional futuro: checagem no CI de `token` vazio); (2) `base_url` padrão em `http://`, apontado a host remoto sem TLS mandaria o Bearer em claro (padrão localhost, informativo)
