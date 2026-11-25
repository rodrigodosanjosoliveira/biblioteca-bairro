DELIVERY_ID: DEL-017
TASK_CODE: PROJ-7
AGENT: data-model
STATUS: ok

ESCOPO_DA_CONFERENCIA: o data-model está em enabled_modules e a demanda toca a leitura de livro na persistência. O architecture-guardrails pulou esta etapa, e a sessão principal pediu a conferência. Não há modelagem nova: P1 = A. Este handoff só confirma o modelo e registra um risco de plano para o test-writer.

ENTIDADES_DOMINIO:
  - domain.Livro { ID int64; Titulo string } (/home/rodrigo/workspace/biblioteca-bairro/internal/domain/livro.go:12-15). SEM MUDANÇA. Continua sendo a entidade de escrita (NovoLivro/InserirLivro) e o exemplar físico (D-12).
  - Nenhuma struct nova em internal/domain/. `disponivel` é estado derivado do empréstimo (D-12: "status não é gravado", `Aberto()` derivado de `Devolucao`), não um atributo do exemplar.
  - Tipo de leitura fora do domínio, no pacote storage: `type LivroComDisponibilidade struct { Livro domain.Livro; Disponivel bool }`, com campo nomeado e sem embedding. Está de acordo com D-12 e com a separação entre domínio e modelo de persistência/leitura: não amplia `domain.Livro` e não cria reidratação. O campo nomeado impede usar o tipo de leitura onde se espera `domain.Livro`. O precedente de tipo do storage na interface do consumidor é `storage.FiltroEmprestimos` (/home/rodrigo/workspace/biblioteca-bairro/internal/http/handler.go:52).

ESQUEMA_BANCO (sem mudança, conferido nas migrações):
  - livro(id bigint identity PK livro_pkey, titulo text NOT NULL CHECK btrim<>'') — /home/rodrigo/workspace/biblioteca-bairro/migrations/0001_esquema_emprestimo.up.sql:1-6.
  - emprestimo(id PK, livro_id bigint NOT NULL FK → livro ON DELETE RESTRICT, usuario_id FK RESTRICT, inicio timestamptz, vencimento date, devolucao timestamptz NULL, CHECK de datas finitas) — 0001:15-26. A coluna renovacoes integer NOT NULL DEFAULT 0 com CHECK 0..2 vem de /home/rodrigo/workspace/biblioteca-bairro/migrations/0002_renovacao_emprestimo.up.sql:4-6.
  - Índices de emprestimo que a nova consulta pode usar:
      emprestimo_livro_aberto_uq UNIQUE (livro_id) WHERE devolucao IS NULL — 0001:28
      emprestimo_livro_id_idx (livro_id) — 0001:29
      emprestimo_livro_id_id_idx (livro_id, id) — /home/rodrigo/workspace/biblioteca-bairro/migrations/0003_indices_listagem_emprestimo.up.sql:8
      emprestimo_aberto_id_idx (id) WHERE devolucao IS NULL — 0003:9
  - Nenhuma migração remove nem altera `emprestimo_livro_aberto_uq`. O grep por "emprestimo_livro_aberto_uq|DROP INDEX" em /home/rodrigo/workspace/biblioteca-bairro/migrations encontrou só a criação (0001:28) e os DROP do down da 0003, restritos aos três índices dela. /home/rodrigo/workspace/biblioteca-bairro/migrations/indices_listagem_test.go:92 ainda protege `emprestimo_livro_aberto_uq` contra o down da 0003.
  - Sem coluna, tabela, view nem índice novo. migrations/ fica intocado (CA6).

PADROES_DE_ACESSO:
  - PA1, detalhe (GET /livros/{id}): `SELECT l.id, l.titulo, NOT EXISTS (SELECT 1 FROM emprestimo e WHERE e.livro_id = l.id AND e.devolucao IS NULL) AS disponivel FROM livro l WHERE l.id = $1`. Faz 1 busca em livro_pkey e 1 sonda no índice parcial.
  - PA2, listagem keyset (GET /livros): a mesma projeção com `WHERE l.id > $1 ORDER BY l.id LIMIT $2` e `$2 = limite+1` (até 101 linhas). O FROM tem só livro, então o LIMIT conta livros. É uma consulta por página (CA5).
  - Por que o índice parcial atende o NOT EXISTS: a subconsulta tem igualdade em `livro_id`, que é a coluna do índice, e o predicado literal `devolucao IS NULL`, que é o mesmo do índice. O planner prova o predicado parcial sem depender de parâmetro, então vale também no plano genérico. Pela unicidade (R4), a sonda devolve 0 ou 1 entrada, e só as linhas abertas estão no índice, que é o menor candidato. O bool é exato porque há no máximo um empréstimo aberto por livro, e a consulta não agrega nada.
  - Sem relógio: vencido também conta como indisponível (P4), então `vencimento` e `now()` não entram.
  - POST /livros (INSERT ... RETURNING id, titulo, /home/rodrigo/workspace/biblioteca-bairro/internal/storage/repositorio.go:98-100) não muda.

CONTRATO_HTTP (conferido em /home/rodrigo/workspace/biblioteca-bairro/internal/http/handler.go):
  - Mudança só aditiva: a chave `disponivel` (bool, sem omitempty) entra nos itens de GET /livros e no corpo de GET /livros/{id}. `itens`, `proximo`, ordenação e paginação (D-19) ficam iguais.
  - POST /livros não muda: `saidaLivro {id, titulo}` (handler.go:220-223) continua só em criarLivro (:286). Hoje `saidaListaLivros.Itens []saidaLivro` (:232-235) e obterLivro usam o mesmo DTO, então o DTO de leitura novo precisa substituir `saidaLivro` nesses dois pontos sem tocar em `saidaLivro`. Isso revisa a frase da D-19 "mesmo DTO de POST /livros" (/home/rodrigo/workspace/biblioteca-bairro/docs-ai/DECISOES.md:677-678), e a D-23 precisa registrar a revisão.
  - Interface `Repositorio` (handler.go:47-48): BuscarLivro e ListarLivros mudam de assinatura para o tipo de leitura, uma mudança interna ao módulo. As demais rotas não mudam.

CONFIRMADO:
  - O domínio não muda: /home/rodrigo/workspace/biblioteca-bairro/internal/domain/livro.go só tem ID e Titulo, e D-12 (/home/rodrigo/workspace/biblioteca-bairro/docs-ai/DECISOES.md:117-139) define Livro = exemplar, status derivado e não gravado, e R4 garantida pelo `emprestimo_livro_aberto_uq`.
  - Hoje BuscarLivro (repositorio.go:108-123) e ListarLivros (:130-141) leem só `livro`.
  - O schema atual vem das migrações 0001, 0002 e 0003, todas lidas. Nenhuma toca `livro` depois da 0001.
  - A fixture de plano (/home/rodrigo/workspace/biblioteca-bairro/internal/storage/plano_listagem_test.go:51-78) tem 500 livros, 200000 empréstimos e 499 abertos, um por livro, e o livro 1 está sem empréstimo aberto. A constante `idxLivroAberto` já existe (:34).

ASSUNCOES:
  - Não rodei EXPLAIN. A preferência do planner por `emprestimo_livro_aberto_uq` em vez de `emprestimo_livro_id_idx` ou `emprestimo_livro_id_id_idx` com Filter é inferência, porque o parcial é o menor índice cujo predicado casa. Quem confirma é o teste de plano.
  - Risco de plano para o test-writer: um EXISTS na lista de colunas vira AlternativeSubPlan no PostgreSQL. O planner escolhe entre o SubPlan correlacionado (uma sonda por linha) e o "hashed SubPlan", que lê uma vez todos os abertos, via `livro_id` IN hash, e não fica correlacionado. Assumo que a escolha é feita pela estimativa de linhas do nó de scan, que fica abaixo do Limit e pode estimar todos os livros com id > $1. Com isso, na fixture (500 livros e 499 abertos) e no plano genérico, o hashed pode ganhar. Ele pode aparecer de duas formas:
      a) Seq Scan em emprestimo (200000 linhas) com Filter `devolucao IS NULL`, que reprova a regra "sem Seq Scan em emprestimo" do architecture-guardrails;
      b) Index Only Scan completo em `emprestimo_livro_aberto_uq`, que PASSA numa asserção só de nome de índice, embora não seja a sonda por livro.
    O `noPlano` atual (plano_listagem_test.go:81-88) não lê "Subplan Name" nem "Parent Relationship", então não distingue SubPlan de hashed SubPlan. Recomendação: o teste deve ler "Subplan Name" e exigir que ele não comece com "hashed", e/ou exigir que a Index Cond em `emprestimo_livro_aberto_uq` cite `livro_id = l.id`. Se o plano der hashed, o caso volta ao architecture-guardrails, sem `SET enable_*` (NAO_FAZER dele).

ARQUIVOS:
  - nenhum alterado (subagente sem ferramenta de escrita).

RISCOS:
  - O hashed SubPlan em produção custaria O(empréstimos abertos) por página em vez de no máximo 101 sondas. Hoje é pequeno (os abertos são limitados pelo número de livros), mas foge do CA5 como foi escrito ("usa o índice", por sonda). O teste de plano precisa pegar esse caso (ver ASSUNCOES).
  - Se uma migração futura remover `emprestimo_livro_aberto_uq`, a R4 e o plano desta consulta caem juntos. Hoje isso está protegido pelo teste da 0003 (indices_listagem_test.go:92), mas não por um teste desta consulta. O teste de plano novo passa a cobrir esse caso.
  - A troca de DTO em `saidaListaLivros.Itens` também atinge o fuzz e os fakes em internal/http/. Se `saidaLivro` for alterado por engano, POST /livros muda e quebra o contrato. O teste de POST /livros deve exigir a ausência da chave `disponivel`.

SINAIS:
  SECURITY: sim — o diff toca internal/storage/ e internal/http/ (security_surface_paths), e o estado de empréstimo do acervo passa a sair em lote (sem usuario_id nem datas).
  SUPPLY_CHAIN: nao — sem dependência nova; go.mod/go.sum intocados.
  MIGRATION: nao — sem mudança de schema nem índice novo; `emprestimo_livro_aberto_uq` (0001:28) já existe e nenhuma migração o altera.
  PERFORMANCE: sim — consulta nova em emprestimo (200k linhas na fixture) por página de GET /livros e em GET /livros/{id}. O risco é o planner escolher hashed SubPlan no lugar da sonda por livro, e o teste de plano precisa distinguir os dois.

PROXIMO: test-writer
MOTIVO: o modelo está confirmado sem mudança (domínio e schema), o tipo de leitura respeita D-12 e o índice parcial sustenta o NOT EXISTS. O test-writer escreve os testes de CA1-CA5, incluindo o de plano que distingue SubPlan correlacionado de hashed SubPlan, e o de POST /livros sem `disponivel`.
