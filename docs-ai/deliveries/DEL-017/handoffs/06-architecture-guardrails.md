DELIVERY_ID: DEL-017
TASK_CODE: PROJ-7
AGENT: architecture-guardrails
STATUS: ok

ABORDAGEM:
  Revisão da A1 do handoff 03, motivada pela medição do test-writer (05-test-writer.md). O CA5 não muda: a listagem precisa sondar emprestimo_livro_aberto_uq por livro, sem hashed SubPlan. A forma da consulta muda. Este subagente não tem shell, então a escolha fica condicionada à medição M1/M2 abaixo, feita pelo test-writer.

  Por que não aceitar o hashed (opção a) agora:
    - O hashed lê todos os empréstimos abertos a cada página. Pela R4 isso chega a N livros, então cada GET /livros custa O(acervo), e não O(101). A paginação keyset da D-19 existe justamente para evitar custo proporcional ao acervo.
    - A escolha vem do custo estimado (estimativa de 500 linhas abaixo do Limit), então muda com as estatísticas. Um teste que "aceita hashed com limite" fixaria um plano que o próprio planner pode trocar.
    - Ainda há forma permitida pelas decisões que não foi medida. A D-21 proíbe concatenação, `($n IS NULL OR ...)`, COALESCE, now(), CURRENT_DATE e AT TIME ZONE. A proibição de LATERAL/JOIN vinha do meu NAO_FAZER (handoff 03), e cabe a mim revê-la. Mudar o CA só se justifica depois que as formas abaixo falharem na medição.

  Por que o NOT EXISTS vira hashed: o planner converte EXISTS com correlação de igualdade simples em ANY hasheável (convert_EXISTS_to_ANY), com ou sem LIMIT 1 interno, como as variantes (a) e (b) do test-writer mostraram. Subconsulta escalar (EXPR_SUBLINK) não tem conversão para hashed, e subconsulta LATERAL com LIMIT não é puxada para join. As duas formas abaixo saem dessa conversão.

  A1'. Forma candidata principal (F1): subconsulta escalar na projeção, com FROM só de livro.
    - Listagem (const sqlListarLivros):
        SELECT l.id, l.titulo,
               (SELECT 1 FROM emprestimo e
                 WHERE e.livro_id = l.id AND e.devolucao IS NULL
                 LIMIT 1) IS NULL AS disponivel
          FROM livro l
         WHERE l.id > $1
         ORDER BY l.id
         LIMIT $2
    - Detalhe (const sqlBuscarLivro): a mesma projeção com `WHERE l.id = $1`. O detalhe também usa F1, para que as duas rotas tenham uma só definição de "disponível". O plano do detalhe é conferido de novo, sem tratar a medição anterior com NOT EXISTS como válida.
    - O que se mantém do desenho anterior: o FROM continua só com livro, então o `LIMIT $2` conta livros sem depender da R4. O `LIMIT 1` interno impede o erro "more than one row returned by a subquery" mesmo se a R4 deixar de valer. O resultado continua sendo uma consulta por requisição. O predicado `devolucao IS NULL` segue literal, para o índice parcial casar no plano genérico. Não há COALESCE: o NULL da subconsulta vazia vira bool por `IS NULL`.
  A1''. Forma alternativa (F2), só se F1 falhar em M1: página limitada numa subconsulta, com LEFT JOIN LATERAL ... LIMIT 1 por cima.
        SELECT p.id, p.titulo, a.um IS NULL AS disponivel
          FROM (SELECT l.id, l.titulo FROM livro l
                 WHERE l.id > $1 ORDER BY l.id LIMIT $2) p
          LEFT JOIN LATERAL (SELECT 1 AS um FROM emprestimo e
                              WHERE e.livro_id = p.id AND e.devolucao IS NULL
                              LIMIT 1) a ON true
         ORDER BY p.id
    - O detalhe usa a mesma forma, com `WHERE l.id = $1` e sem ORDER/LIMIT na subconsulta da página.
    - O `LIMIT 1` do lateral garante no máximo uma linha por livro sem depender da R4, e o LIMIT da página é aplicado antes do join. O ORDER BY externo é obrigatório, porque a ordem da subconsulta não é garantida na saída.
  A2 a A6 do handoff 03 continuam valendo: tipo de leitura, assinaturas, DTO, listarPorID intocado, D-23 e testes HTTP.

  Medição que o test-writer faz (no Postgres 16 do digest do CI, com fixturePlano, EXPLAIN (VERBOSE, FORMAT JSON), genérico e os mesmos custom: listagem com apos 0/250/450 e limite+1 101/21/2; detalhe com genérico, livro 1, livro 151 e inexistente):
    M1 (F1): passa se, em todos os casos, (i) livro usa Index Scan em livro_pkey; (ii) há um único scan em emprestimo, com Parent Relationship = SubPlan, Index Scan ou Index Only Scan em emprestimo_livro_aberto_uq, Index Cond com `livro_id = l.id` e sem Filter; (iii) não há "hashed" em Subplan Name nem em Output; (iv) não há Seq Scan nem Bitmap.
    M2 (F2), só se M1 falhar: os mesmos (i), (iii) e (iv). O emprestimo aparece no lado interno de um Nested Loop (Left Join), com Index Scan ou Index Only Scan em emprestimo_livro_aberto_uq e Index Cond com `livro_id = p.id`. Não pode haver Hash Join, Merge Join nem Sort por cima do Limit da página. Um nó Memoize sobre a sonda é aceito, desde que a Index Cond continue parametrizada por livro.
    Nos dois casos, registrar no handoff o trecho do plano genérico e o do custom com limite+1 = 2.

  Ajustes de teste que decorrem disso (feitos pelo test-writer):
    - TestSQLLeituraLivro (disponibilidade_sem_banco_test.go). Com F1: trocar a exigência de `not exists` por `(select 1 from emprestimo e`, `limit 1) is null` e `as disponivel`. Proibir `exists`, para que a forma hasheável não volte. Na listagem, exigir exatamente um `limit $2` e um `limit 1`. No detalhe, permitir só o `limit 1` interno, sem `limit $` nem ORDER. As demais proibições ficam (join, lateral, coalesce, now() e o resto). Com F2: exigir `left join lateral`, `limit 1) a on true` e `order by p.id` externo, e liberar join/lateral só nesse formato. Nos dois casos, o teste de texto passa a explicar por que o EXISTS é proibido (ele vira hashed).
    - TestPlanoLeituraLivro (plano_livros_test.go): com F1, as asserções atuais servem sem mudança. Com F2, a asserção de Parent Relationship = SubPlan troca para "lado interno de Nested Loop", e a Index Cond fica com `p.id`.
    - Mutantes novos: F1 reescrito como NOT EXISTS (só o teste de texto e o de plano matam, e precisam matar); `IS NULL` trocado por `IS NOT NULL`; remoção do `LIMIT 1` interno (equivalente sob a R4, registrar como sobrevivente esperado).

NAO_FAZER:
  - Não aceitar hashed SubPlan na listagem nem afrouxar a asserção de plano do CA5 sem decisão do usuário. Isso vale mesmo que só a listagem falhe.
  - Não usar EXISTS/NOT EXISTS, IN nem `= ANY` com subconsulta para o bool. Todas podem virar hashed.
  - Não usar COALESCE para transformar o NULL em bool. Usar `IS NULL`.
  - Não usar count(*), bool_or, GROUP BY nem DISTINCT, e não usar LEFT JOIN direto em emprestimo (sem LATERAL e LIMIT 1). Esse join deu Hash Right Join com Seq Scan em livro.
  - Não usar F2 se F1 passar em M1. JOIN/LATERAL só entram como fallback medido, no formato exato de A1''.
  - Não forçar plano com SET enable_*, SET LOCAL, hint, CTE MATERIALIZED, OFFSET 0 nem mudança de estatística/ANALYZE dentro do teste além do que fixturePlano já faz.
  - Não criar índice, migração, coluna nem view (CA6).
  - Não alterar listarPorID nem os outros listadores. Continuam valendo todos os itens do NAO_FAZER do handoff 03, exceto a proibição de LATERAL, que agora só vale fora do fallback F2.
  - Se F1 e F2 falharem, não inventar uma terceira forma. Devolver ao architecture-guardrails com os planos.

CONFIRMADO:
  - O test-writer mediu no Postgres 16 (05-test-writer.md:40-47). Na listagem, NOT EXISTS, a página em subconsulta com EXISTS fora e EXISTS com LIMIT 1 deram hashed SubPlan sobre emprestimo_aberto_id_idx, sem Index Cond. O LEFT JOIN direto deu Hash Right Join com Seq Scan em livro. O detalhe com NOT EXISTS usou Index Only Scan em emprestimo_livro_aberto_uq com Index Cond (e.livro_id = l.id).
  - O CA5 da triagem (/home/rodrigo/workspace/biblioteca-bairro/docs-ai/deliveries/DEL-017/handoffs/02-pm-triage.md:39) exige uma consulta por requisição e o uso de emprestimo_livro_aberto_uq. F1 e F2 cumprem o critério sem mudança de texto do CA, se M1 ou M2 passarem.
  - A D-21 (/home/rodrigo/workspace/biblioteca-bairro/docs-ai/DECISOES.md:874-886) proíbe concatenação, `($n IS NULL OR ...)`, COALESCE, now(), CURRENT_DATE e AT TIME ZONE. Ela não trata de subconsulta escalar nem de LATERAL. A proibição de LATERAL estava só no NAO_FAZER do handoff 03 (03-architecture-guardrails.md:40).
  - Os demais testes do test-writer passaram com a implementação de referência. TestLeituraLivroUmaConsulta e os testes HTTP não dependem da forma do SQL além do texto da const.

ASSUNCOES:
  - Não medi: este subagente não tem shell. Que a subconsulta escalar com correlação vira SubPlan por linha, sem forma hashed, e que o lateral com LIMIT não é puxado para join são comportamentos conhecidos do planner do PostgreSQL 16. Vale como hipótese até M1/M2.
  - Que o planner escolha emprestimo_livro_aberto_uq na sonda de F1 é provável, porque o detalhe, com o mesmo predicado e a mesma correlação, já o escolheu. Mesmo assim, só M1 confirma.

ARQUIVOS:
  - nenhum alterado por este subagente.
  - A ajustar pelo test-writer: /home/rodrigo/workspace/biblioteca-bairro/internal/storage/disponibilidade_sem_banco_test.go (texto SQL) e, só com F2, /home/rodrigo/workspace/biblioteca-bairro/internal/storage/plano_livros_test.go (checarPlanoLivro).
  - Depois, pelo dev: /home/rodrigo/workspace/biblioteca-bairro/internal/storage/repositorio.go (consts sqlListarLivros e sqlBuscarLivro na forma escolhida).

RISCOS:
  - Se F1 e F2 falharem em M1/M2, a decisão passa a ser do usuário. Pergunta pronta: "Na listagem GET /livros, o PostgreSQL só consegue resolver a disponibilidade lendo todos os empréstimos abertos (até o tamanho do acervo, por índice parcial e sem Seq Scan) a cada página, em vez de no máximo 101 sondas. Aceita revisar o CA5 para permitir isso, com o teste conferindo que não há Seq Scan e que o índice é parcial de abertos?". Minha recomendação nesse caso é aceitar só com o limite da U3 da D-21 como gatilho de revisão (cerca de 10^6 linhas em emprestimo).
  - Com F1, a semântica de "disponível" depende de `IS NULL` sobre uma subconsulta. O mutante IS NOT NULL e os testes CA3/CA4 com banco cobrem esse ponto.
  - O plano continua sendo escolha de custo. O teste de plano, genérico e custom, é a barreira contra regressão em versão futura do Postgres.

TEXTO_REPORT (substitui, na seção "## Architecture/Guardrails" de /home/rodrigo/workspace/biblioteca-bairro/docs-ai/deliveries/DEL-017/report.md, o trecho "Abordagem: `NOT EXISTS (...)` na projeção"):
  Abordagem revista após EXPLAIN do test-writer: NOT EXISTS na listagem virou hashed SubPlan, que lê todos os empréstimos abertos por página. A disponibilidade passa a ser `(SELECT 1 FROM emprestimo e WHERE e.livro_id = l.id AND e.devolucao IS NULL LIMIT 1) IS NULL` na projeção de `SELECT ... FROM livro l`, com sonda por livro em `emprestimo_livro_aberto_uq` conferida por EXPLAIN genérico e custom. O fallback, só se o plano não sair assim, é a página em subconsulta com `LEFT JOIN LATERAL (... LIMIT 1)`.

SINAIS:
  SECURITY: sim — a implementação toca internal/storage/ e internal/http/ (security_surface_paths). Nada muda em relação ao handoff 03.
  SUPPLY_CHAIN: nao — sem dependência nova; go.mod/go.sum intocados.
  MIGRATION: nao — sem schema nem índice novo.
  PERFORMANCE: sim — a forma da consulta muda para garantir sonda por livro (O(página)) em vez de hashed (O(abertos)). A medição M1/M2 decide.

PROXIMO: test-writer
MOTIVO: o CA5 continua de pé, e há duas formas permitidas pela D-21 que saem da conversão de EXISTS para hashed. O test-writer mede F1 (M1) e, se precisar, F2 (M2), e ajusta TestSQLLeituraLivro (e checarPlanoLivro, se F2) para a forma aprovada. Se as duas falharem, o caso volta ao architecture-guardrails com os planos, e este subagente devolve ao usuário com a pergunta de RISCOS.
