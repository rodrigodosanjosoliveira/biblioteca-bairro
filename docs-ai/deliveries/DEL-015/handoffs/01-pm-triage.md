DELIVERY_ID: DEL-015
TASK_CODE: DEL-015
AGENT: pm-triage
STATUS: ok

TIPO: feature (comportamento novo: hoje GET /emprestimos responde 400 a `situacao`, `usuario_id` e `livro_id`; a demanda passa a aceitá-los e cria uma rota nova)

ESCOPO:
  - Filtros em `GET /emprestimos`: `situacao`, `usuario_id`, `livro_id`, combináveis, no contrato de paginação da D-19 sem variação (`limite`, `apos`, ordem por `id` ascendente, `{itens, proximo}`, `parametro_invalido` 400).
  - Rota nova `GET /usuarios/{id}/emprestimos`, autenticada, mesmo contrato de paginação, aceitando `situacao` (e `livro_id`, ver ASSUNCOES A5).
  - Consultas keyset filtradas no storage (`internal/storage/repositorio.go`), uma por página, sem N+1, sem OFFSET nem COUNT.
  - Migração `0003_*` com os índices que atendem o keyset filtrado (decisão do `data-model`; proposta em ASSUNCOES A8), revisada pelo `migration-reviewer`.
  - Nova decisão em `docs-ai/DECISOES.md` (D-21) e atualização do mapa `docs-ai/03-MAPA-RAPIDO-MODULOS.md` (via `docs-update`).
  - Marcação `merged` da DEL-014 já presente e não commitada no working tree (`docs-ai/deliveries/INDEX.md` linha da DEL-014 e `docs-ai/deliveries/DEL-014/report.md`): entra no commit desta entrega, sem outra alteração nesses arquivos.

FORA_DE_ESCOPO:
  - `GET /livros/{id}/emprestimos` (redundante com `GET /emprestimos?livro_id=` e quase homônima de `GET /livros/{id}/emprestimo` singular; ver A4).
  - Ordenação configurável, total/`COUNT`, busca por título ou nome, filtro por intervalo de datas, filtro por `renovacoes` ou por multa.
  - Coluna de situação persistida, gatilho ou job que marque vencido (vencido continua derivado; ver RISCOS R1).
  - `JOIN` com título do livro ou nome do usuário no item (DTO igual ao de `GET /emprestimos/{id}`, D-20).
  - Rate limit, autorização por recurso, múltiplos tokens (condição de bloqueio da D-20 continua valendo).
  - Mudança em qualquer rota, código, mensagem ou DTO existente; mudança em `go.mod`/`go.sum`.
  - Alterar `security_surface_paths` ou qualquer arquivo de governança.

CONFIRMADO:
  - Rotas atuais em `internal/http/handler.go:135-148`; `GET /emprestimos` existe (DEL-014), `GET /usuarios/{id}/emprestimos` e `GET /livros/{id}/emprestimos` não existem; `GET /livros/{id}/emprestimo` (singular, empréstimo aberto) existe na linha 142.
  - Contrato de paginação e rejeição de chave desconhecida: D-19 (`docs-ai/DECISOES.md:670-748`); U1 adiando filtros e a rota aninhada com "índice composto ou parcial, data-model e migration-reviewer": D-20 (`docs-ai/DECISOES.md:797-802`).
  - Índices existentes em `migrations/0001_esquema_emprestimo.up.sql:28-30`: `emprestimo_livro_aberto_uq` UNIQUE (livro_id) WHERE devolucao IS NULL, `emprestimo_livro_id_idx` (livro_id), `emprestimo_usuario_id_idx` (usuario_id). Nenhum atende `WHERE usuario_id = $u AND id > $a ORDER BY id LIMIT n` sem ordenar (precisam de `(usuario_id, id)` / `(livro_id, id)`).
  - Coluna `vencimento` é `date` e `devolucao` `timestamptz NULL` (`migrations/0001_esquema_emprestimo.up.sql:20-21`); não há coluna de situação.
  - Fronteira de vencido: D-17 (`docs-ai/DECISOES.md:555-558`), vencido quando `meiaNoiteUTC(ref) > meiaNoiteUTC(vencimento)` na data civil `America/Sao_Paulo` (D-10, D-11), a mesma em que a multa começa a contar.
  - Códigos 404 existentes: `usuario_inexistente` (só detalhe de usuário, `internal/http/erros.go:41`), `livro_nao_encontrado` (`erros.go:26`), `usuario_nao_encontrado` é 422 (`erros.go:25`).
  - Executor de migração recusa `CONCURRENTLY` (D-16, mapa de módulos `internal/migrate/`), então `CREATE INDEX` toma lock SHARE na tabela `emprestimo` durante a criação.
  - `.pipeline-claude.json`: `internal/http/`, `internal/storage/` e `migrations/` em `security_surface_paths`; `enabled_modules` = `data-model`, `migration-reviewer` (sem `query-performance`, sem `release-ops`).
  - Delivery criada por `pipeline_delivery_new` com `require_security`: `docs-ai/deliveries/DEL-015/report.md`.

ASSUNCOES:
  - A1 (nomes e valores): query params `situacao` ∈ {`aberto`, `devolvido`, `vencido`} (minúsculas, sem acento, comparação exata; outro valor ou vazio → 400 `parametro_invalido`), `usuario_id` e `livro_id` com a gramática do `parseID` (inteiro > 0). Chave repetida (inclusive `situacao=aberto&situacao=vencido`) → 400, como na D-19. Sem lista de valores (`situacao=aberto,vencido`) nesta entrega.
  - A2 (combinação): todos os filtros se combinam por AND, inclusive com `limite` e `apos`. Combinação que nunca casa (ex.: `livro_id` de livro sem empréstimo) → 200 com `itens: []` e `proximo: null`.
  - A3 (definição de situação, PROPOSTA A CONFIRMAR PELO HUMANO NO PR):
      `aberto` = `devolucao IS NULL` (inclui os vencidos, coerente com `Emprestimo.Aberto()` do domínio e com `emprestimo_livro_aberto_uq`);
      `devolvido` = `devolucao IS NOT NULL` (inclui devolvido com atraso; "vencido" não se aplica a devolvido);
      `vencido` = `devolucao IS NULL AND vencimento < $hoje`, onde `$hoje` é a data civil em `America/Sao_Paulo` do relógio injetado, calculada em Go e passada como parâmetro `date`. Vencimento igual a hoje NÃO é vencido (fronteira da D-17/D-11: vence no fim do dia civil, o 12:00 é convenção). Nada de `now()`/`CURRENT_DATE` no SQL: o fuso da sessão do banco não manda e o teste com relógio fixo precisa funcionar.
      Alternativa que o humano pode preferir: `aberto` exclusivo (aberto e não vencido). Não bloqueei porque a inclusiva é a que já existe no domínio; o `architecture-guardrails` deve registrar a escolha na D-21.
  - A4 (rotas aninhadas): entra `GET /usuarios/{id}/emprestimos` (pedida em U1); `GET /livros/{id}/emprestimos` fica fora (não está em U1, é redundante com `?livro_id=` e a diferença singular/plural com `GET /livros/{id}/emprestimo` confunde cliente). Se o humano quiser, delivery própria.
  - A5 (query da rota aninhada): `GET /usuarios/{id}/emprestimos` aceita `limite`, `apos`, `situacao` e `livro_id`; `usuario_id` na query dessa rota → 400 `parametro_invalido` (o usuário vem do path; evita ambiguidade de conflito).
  - A6 (inexistente): em `GET /emprestimos?usuario_id=X` ou `?livro_id=Y` com id inexistente → 200 com lista vazia, sem consulta de existência (filtro não é recurso; evita oráculo extra e consulta a mais). Em `GET /usuarios/{id}/emprestimos` com usuário inexistente → 404 `usuario_inexistente`, coerente com `GET /usuarios/{id}` (D-20); custa uma verificação de existência (só na primeira página ou em toda página: decisão do `architecture-guardrails`; sugestão: em toda página, sem transação, consistência eventual aceita como na D-19). Nenhum código de erro novo.
  - A7 (relógio): o relógio injetado continua lido uma vez por requisição (D-20) e o mesmo instante serve para `$hoje` do filtro `vencido` e para a multa dos itens, para que um item filtrado como vencido nunca saia com multa zero na mesma página.
  - A8 (índices, proposta para o `data-model`, que decide): migração `0003` com
      `(usuario_id, id)` e `(livro_id, id)` btree para o keyset filtrado (servem também à FK e às buscas atuais por igualdade, então os índices simples `emprestimo_usuario_id_idx` e `emprestimo_livro_id_idx` ficam redundantes — trocar ou manter é decisão do `data-model`; se trocar, o down recria os antigos e não é destrutivo em dado);
      índice parcial `(id) WHERE devolucao IS NULL` para `situacao=aberto|vencido` sem filtro de usuário/livro (abertos são no máximo um por livro por causa de `emprestimo_livro_aberto_uq`, então o conjunto é pequeno e o filtro `vencimento < $hoje` é aplicado sobre ele);
      `devolvido` sem índice próprio: é a maioria das linhas, a PK com filtro basta.
      Sem `CONCURRENTLY` (o executor recusa, D-16): lock SHARE em `emprestimo` bloqueia escrita durante a criação, aceitável pelo tamanho da tabela e limitado pelos timeouts do executor. Ordem de deploy: `migrate up` antes do binário novo (como D-17), mas as consultas novas funcionam sem os índices (só mais lentas), então não há quebra se a ordem inverter.
  - A9: `situacao` é filtro, não campo novo no DTO; o item continua igual ao de `GET /emprestimos/{id}` (sem campo `situacao`/`vencido` na resposta). Acrescentar campo seria mudança de DTO, fora de escopo.
  - A10: a marcação merged da DEL-014 no working tree está correta e completa (verifiquei só a linha do INDEX.md: `| DEL-014 | ... | merged | 2026-11-07 |`); não foi revisada pelo triage além disso.

ARQUIVOS:
  - docs-ai/deliveries/DEL-015/report.md — criado por `pipeline_delivery_new` (flag `require_security`, escopo preenchido)
  - docs-ai/deliveries/INDEX.md — linha da DEL-015 acrescentada pela ferramenta (o arquivo já tinha a marcação merged da DEL-014 sem commit)
  - Previstos (não editados aqui): internal/http/handler.go, internal/http/erros.go (sem código novo esperado), internal/storage/repositorio.go, migrations/0003_*.{up,down}.sql, testes em internal/http e internal/storage, docs-ai/DECISOES.md, docs-ai/03-MAPA-RAPIDO-MODULOS.md

RISCOS:
  - R1 (vencido depende do relógio): "vencido" não é coluna e não pode entrar em predicado de índice parcial (`CURRENT_DATE`/`now()` não são IMMUTABLE). O índice só restringe a "aberto"; `vencimento < $hoje` é filtro sobre as linhas abertas. Consequências: (a) a mesma URL muda de resultado com a virada do dia civil SP sem nenhuma escrita; (b) paginando por cursor através da meia-noite SP, um empréstimo pode entrar no conjunto "vencido" numa página já passada e não aparecer (id menor que `apos`) — consistência eventual, como a D-19 já aceita; (c) com `ORDER BY id`, um índice em `(vencimento, id)` não serve ao keyset, então o custo de `vencido` é proporcional ao número de abertos percorridos até completar a página (limitado pelo número de livros, por `emprestimo_livro_aberto_uq`).
  - R2 (cálculo de `$hoje`): se `$hoje` for calculado no fuso do servidor ou com `CURRENT_DATE` do banco, a fronteira desloca um dia entre 21:00 e 24:00 SP (UTC-3). Teste de tabela na fronteira (vencimento = hoje, = ontem; relógio às 23:59 e 00:00 SP, e 02:59/03:00 UTC) é obrigatório.
  - R3 (divergência domínio x SQL): a regra de vencido existe no domínio (`Renovar`, D-17) e passaria a existir também no SQL. Precisa de teste com banco cruzando o resultado do filtro com `ErrEmprestimoVencido`/multa > 0 para o mesmo relógio, senão as duas definições derivam.
  - R4 (exposição de dado pessoal): `GET /usuarios/{id}/emprestimos` e `?usuario_id=` tornam trivial extrair o histórico de leitura de uma pessoa específica; com o token isso já era possível percorrendo `GET /emprestimos` (U2 da D-20), mas o custo cai de N/100 requisições para uma. Fica dentro do modelo token único/operador único; a condição de bloqueio da D-20 (autorização por recurso antes de múltiplos tokens) passa a cobrir também a rota nova. O `security-review` deve registrar isso.
  - R5 (lock na migração): `CREATE INDEX` sem `CONCURRENTLY` bloqueia INSERT/UPDATE em `emprestimo` (abrir, devolver, renovar) durante a criação; ok para a tabela de uma biblioteca de bairro, mas precisa de nota no report e no runbook.
  - R6 (oráculo de existência): 404 `usuario_inexistente` na rota aninhada contra 200 vazio em `?usuario_id=` é assimétrico de propósito; a listagem autenticada de usuários já revela os ids (mesmo risco BAIXO da D-20).
  - R7 (compatibilidade): cliente que hoje recebe 400 por mandar `situacao`/`usuario_id`/`livro_id` passa a receber 200 filtrado; nenhum cliente conhecido depende do 400.

SINAIS:
  SECURITY: sim — toca `internal/http/` (entrada externa nova: três query params e rota nova) e `internal/storage/` (SQL novo parametrizado) e `migrations/`, todos em `security_surface_paths`; exposição do histórico por usuário (R4). Flag `require_security` ligada na criação.
  SUPPLY_CHAIN: nao — sem mudança em `go.mod`/`go.sum`; stdlib e `pgx/v5` já presentes bastam.
  MIGRATION: sim — migração `0003` com índices novos (e possível troca dos índices simples), executada pelo executor da D-16 sem `CONCURRENTLY`; `data-model` e `migration-reviewer` habilitados.
  PERFORMANCE: sim — keyset filtrado depende de índice composto `(fk, id)` e de índice parcial para abertos; `vencido` não é indexável (R1). `query-performance` não está habilitado: o `data-model` e o `code-reviewer` devem conferir o plano (EXPLAIN em teste com banco, se viável).

PROXIMO: architecture-guardrails
MOTIVO: tipo feature com contrato HTTP novo (parâmetros, semântica de situação, rota aninhada, comportamento em inexistente) e decisão D-21 a registrar; depois `data-model` para os índices da migração 0003. Nenhum ponto exige bloqueio: A3 (aberto inclusivo x exclusivo) e A4 (rota por livro) têm padrão proposto e ficam para confirmação humana no PR.
