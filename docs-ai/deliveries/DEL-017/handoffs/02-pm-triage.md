DELIVERY_ID: DEL-017
TASK_CODE: PROJ-7
AGENT: pm-triage
STATUS: ok

TIPO: feature (campo derivado novo na leitura de livro; sem mudança de modelo)

DECISOES_DO_USUARIO (sessão de 23/11/2026, repassadas pela sessão principal):
  - P1 = A: o modelo atual fica (D-12: cada linha de `livro` é um exemplar físico). Sem tabela `exemplar`, sem coluna de quantidade, sem migração de schema. Campo exposto: `disponivel` (bool).
  - P4: disponível = não existe empréstimo com `devolucao IS NULL` para o livro. Vencido e não devolvido conta como indisponível.
  - P2 (padrão aceito): `disponivel` em GET /livros/{id} e em cada item de GET /livros; sem endpoint novo.
  - P3 (padrão aceito): calculado na consulta, uma consulta por página (sem N+1), sem valor armazenado.

MODULOS_IMPACTADOS:
  - internal/storage/ (repositorio.go: BuscarLivro, ListarLivros/listarPorID) — superfície de segurança
  - internal/http/ (handler.go: saidaLivro, obterLivro, listarLivros; interface Repositorio, se a assinatura mudar) — superfície de segurança
  - internal/domain/ (livro.go, se o architecture-guardrails decidir carregar o dado no tipo ou num tipo de leitura)
  - postman/ (respostas de GET /livros e GET /livros/{id}; D-22)
  - docs-ai/ (DECISOES.md: decisão nova; 03-MAPA-RAPIDO-MODULOS.md)

ESCOPO:
  - Campo `disponivel` (bool) em GET /livros/{id} e em cada item de GET /livros, derivado de não existir empréstimo aberto (`devolucao IS NULL`) para o livro.
  - Calculado na própria consulta de leitura de livro (uma consulta por requisição ou página), sem valor gravado.
  - Testes de tabela em storage e http, com banco (BIBLIOTECA_TEST_DATABASE_URL); coleção Postman atualizada; decisão registrada em DECISOES.md.

FORA_DE_ESCOPO:
  - Opção B (tabela `exemplar`, empréstimo por exemplar, backfill) e opção C (coluna `livro.quantidade`): descartadas pelo usuário em 23/11/2026.
  - Qualquer migração de schema, incluindo índice novo (ver MIGRATION).
  - Contagem numérica (`exemplares_total`/`exemplares_disponiveis`).
  - Filtro ou ordenação de GET /livros por disponibilidade (ex.: ?disponivel=true).
  - Reserva, fila de espera e notificação.
  - Mudança em multa (D-09/D-10/D-14), renovação (D-17), regra R4 e contrato de paginação (D-19).

CRITERIOS_DE_ACEITE:
  - CA1: GET /livros/{id} devolve 200 com `disponivel` bool; livro inexistente → 404 como hoje.
  - CA2: cada item de GET /livros traz `disponivel`; limite, apos, proximo e ordenação por id não mudam (D-19).
  - CA3 (teste de banco em tabela): livro sem empréstimo → true; com empréstimo aberto no prazo → false; com empréstimo aberto vencido → false; depois da devolução → true; depois de renovação → continua false.
  - CA4: uma página de GET /livros mistura livros disponíveis e indisponíveis e cada item reflete o seu estado (o valor de um não vaza para o outro).
  - CA5: uma única consulta SQL por página de GET /livros e uma por GET /livros/{id} (sem N+1), conferido em teste ou pelo EXPLAIN registrado no report; a verificação de empréstimo aberto usa o índice `emprestimo_livro_aberto_uq`.
  - CA6: nenhum arquivo novo nem alterado em migrations/.
  - CA7: coleção Postman em /home/rodrigo/workspace/biblioteca-bairro/postman atualizada; decisão em docs-ai/DECISOES.md; mapa de módulos atualizado.
  - CA8: go build, go vet, go test ./... (com banco) e bin/pipeline-claude sensors verdes.

CONFIRMADO:
  - O índice existente atende à verificação sem migração: `CREATE UNIQUE INDEX emprestimo_livro_aberto_uq ON emprestimo (livro_id) WHERE devolucao IS NULL;` em /home/rodrigo/workspace/biblioteca-bairro/migrations/0001_esquema_emprestimo.up.sql:28. Um EXISTS ou LEFT JOIN por `livro_id` com `devolucao IS NULL` casa com o predicado do índice parcial.
  - /home/rodrigo/workspace/biblioteca-bairro/migrations/0002_renovacao_emprestimo.up.sql e /home/rodrigo/workspace/biblioteca-bairro/migrations/0003_indices_listagem_emprestimo.up.sql não removem nem alteram esse índice (0003 só acrescenta índices; o comentário dela fala em remover os simples da 0001 numa migração futura, o que não atinge `emprestimo_livro_aberto_uq`).
  - Por R4 (o mesmo índice único), há no máximo um empréstimo aberto por livro, então `disponivel` é exatamente "não existe linha aberta"; não há contagem a agregar.
  - O DTO `saidaLivro` (/home/rodrigo/workspace/biblioteca-bairro/internal/http/handler.go:220-223) também é usado por criarLivro (:286), além de listarLivros (:373) e obterLivro (:512).
  - BuscarLivro (/home/rodrigo/workspace/biblioteca-bairro/internal/storage/repositorio.go:108) e ListarLivros (:130) hoje leem só `livro`.

ASSUNCOES:
  - A resposta de POST /livros: fica a critério do architecture-guardrails. Ou mantém só id e titulo (DTO separado), ou devolve `disponivel: true`, já que um livro recém-criado não tem empréstimo. A demanda não cobre essa rota.
  - "Disponível" é avaliado no instante da consulta, sem lock: uma leitura concorrente com abertura ou devolução pode refletir o estado imediatamente anterior. É aceitável para um campo informativo; a R4 continua garantida na abertura.
  - O report /home/rodrigo/workspace/biblioteca-bairro/docs-ai/deliveries/DEL-017/report.md ainda tem o escopo da primeira triagem (total/disponíveis, P1 em aberto). O pm-triage não tem ferramenta de escrita: o escopo fechado acima, os CAs, o que fica fora do escopo e as decisões datadas precisam ser levados ao report pela sessão principal (docs-ai/deliveries/ é aberto) ou pelo docs-update.

ARQUIVOS:
  - nenhum alterado por este subagente (sem ferramenta de escrita); o report precisa de atualização (ver ASSUNCOES).

RISCOS:
  - Mudar `saidaLivro` sem cuidado altera também a resposta de POST /livros.
  - Se a assinatura de ListarLivros mudar para carregar o bool, o helper compartilhado `listarPorID` (D-20, também usado por usuários e empréstimos) pode ser afetado. Manter a mudança restrita à listagem de livros.
  - Consulta com JOIN mal escrito pode duplicar ou omitir livros na página keyset (o LIMIT limite+1 precisa continuar contando livros, não linhas do join). Isso é evitado com EXISTS, ou com LEFT JOIN amparado pela unicidade da R4.

SINAIS:
  SECURITY: sim — toca internal/http/ e internal/storage/, ambos em security_surface_paths; require_security já está ligada no report.
  SUPPLY_CHAIN: nao — sem dependência nova; go.mod/go.sum intocados.
  MIGRATION: nao — sem mudança de schema (P1 = A), e o índice parcial `emprestimo_livro_aberto_uq` (0001:28) já atende a verificação; CA6 garante que migrations/ não muda.
  PERFORMANCE: sim — muda a consulta keyset de GET /livros (até 100 itens por página) e a de GET /livros/{id}. O risco é baixo com o índice existente, mas o plano precisa ser conferido (CA5, sem N+1). query-performance não está em enabled_modules, então a conferência fica com o code-reviewer e o QA.

PROXIMO: architecture-guardrails
MOTIVO: as decisões que bloqueavam estão tomadas (P1 = A, P4, P2/P3 nos padrões). Falta o desenho técnico: forma da consulta (EXISTS ou LEFT JOIN), se o bool entra no domínio ou num tipo de leitura, o DTO de POST /livros e o impacto em `listarPorID`. Não há mudança de modelo, então data-model e migration-reviewer não são acionados.
