DELIVERY_ID: DEL-017
TASK_CODE: PROJ-7
AGENT: architecture-guardrails
STATUS: ok

ABORDAGEM:
  A1. Consulta: EXISTS correlacionado na lista de colunas, não LEFT JOIN.
    - Listagem (texto numa const do pacote storage, ex. `sqlListarLivros`, para o teste de plano usar o texto real, como fez a D-21):
        SELECT l.id, l.titulo,
               NOT EXISTS (SELECT 1 FROM emprestimo e
                            WHERE e.livro_id = l.id AND e.devolucao IS NULL) AS disponivel
          FROM livro l
         WHERE l.id > $1
         ORDER BY l.id
         LIMIT $2
    - Detalhe (const `sqlBuscarLivro`): mesma projeção com `WHERE l.id = $1`, sem ORDER/LIMIT.
    - Por que EXISTS: o FROM só tem `livro`, então cada linha é um livro e o `LIMIT $2` (= limite+1, aplicado por listarPorID) conta livros por construção. Não depende da R4 para não duplicar linha, ao contrário do LEFT JOIN. O SubPlan roda só para as linhas que sobem pelo Limit (no máximo 101 por página, 1 no detalhe), cada uma uma sonda de índice. É uma consulta SQL por requisição (CA5).
    - Os predicados `e.livro_id = l.id AND e.devolucao IS NULL` casam com o índice parcial `emprestimo_livro_aberto_uq (livro_id) WHERE devolucao IS NULL` (migrations/0001_esquema_emprestimo.up.sql:28). `devolucao IS NULL` precisa aparecer literalmente, sem COALESCE nem parâmetro, para o planner provar o predicado do índice parcial também no plano genérico.
    - "Disponível" não depende do relógio (P4: vencido também conta como indisponível). Então não há `now()`, `CURRENT_DATE`, `vencimento` nem `Ref` na consulta, e o handler não lê `a.agora()` nessas rotas.
  A2. O bool fica num tipo de leitura no pacote storage, fora do domínio.
    - `type LivroComDisponibilidade struct { Livro domain.Livro; Disponivel bool }` (campo nomeado, sem embedding: impede passar o tipo de leitura onde se espera `domain.Livro`).
    - `domain.Livro` não muda: é a entidade de escrita (NovoLivro/InserirLivro). Disponibilidade é estado derivado do empréstimo, não atributo do exemplar (D-12, status derivado e não gravado). O precedente de tipo do storage na interface do consumidor é `storage.FiltroEmprestimos` (handler.go:52).
    - As assinaturas mudam no lugar, sem método paralelo (precedente D-21): `BuscarLivro(ctx, id) (storage.LivroComDisponibilidade, error)` e `ListarLivros(ctx, apos, limite) ([]storage.LivroComDisponibilidade, bool, error)`. A interface `Repositorio` em internal/http/handler.go:47-48 acompanha. BuscarLivro só tem um consumidor em produção (obterLivro, handler.go:507), confirmado por grep.
    - Erros: `pgx.ErrNoRows` continua virando `ErrLivroNaoEncontrado`, e erro de Scan passa por `traduzirErro`. Não entra sentinela nova, e `id <= 0` continua respondendo sem SQL.
  A3. DTO de POST /livros: fica `{id, titulo}` como está.
    - `saidaLivro` (handler.go:220) continua só em criarLivro. Entra um DTO novo, ex. `saidaLivroLido { ID int64 json:"id"; Titulo string json:"titulo"; Disponivel bool json:"disponivel" }`, usado por obterLivro e pelos itens de `saidaListaLivros`.
    - `Disponivel` é `bool` puro, sem ponteiro e sem `omitempty`: o `false` precisa sair no JSON.
    - Motivo: POST /livros está fora da demanda. Um `true` fixo seria um valor afirmado sem consulta, e manter a rota inalterada zera o risco de contrato nela. Isso revisa a frase da D-19 "GET /livros/{id} ... mesmo DTO de POST /livros", e a revisão precisa constar na decisão nova.
  A4. Sem impacto em `listarPorID` (D-20): o helper genérico não muda (corpo, assinatura e comentário ficam iguais). ListarLivros só troca o SQL passado, que mantém o contrato `$1 = apos`, `$2 = limite+1`, sem extras, e o tipo T da closure de leitura (Scan de 3 colunas em `LivroComDisponibilidade`). ListarUsuarios e ListarEmprestimos não são tocados.
  A5. Decisão nova D-23 em docs-ai/DECISOES.md. Ela cita: a D-19 (revoga "indicação de 'emprestado' na listagem fica fora" e "mesmo DTO de POST /livros"), a D-12 (Livro = exemplar), a R4/D-13 (índice) e a D-22 (Postman). Registra ainda o contrato aditivo, o EXISTS e a consistência sem lock.
  A6. Testes (stdlib, em tabela):
    - storage com banco: CA3, CA4 e 404.
    - plano: reaproveitar `fixturePlano`/`planoGenerico`/`planoCustom` (internal/storage/plano_listagem_test.go). Exigir Index Scan em `livro_pkey` no livro e, no SubPlan, Index Scan ou Index Only Scan em `emprestimo_livro_aberto_uq`, tanto no plano genérico quanto no custom. Proibir Seq Scan em emprestimo e em livro. A fixture já tem 500 livros e 499 abertos, e o livro 1 está sem empréstimo aberto.
    - http: o JSON traz `disponivel` true/false explícito em GET /livros e GET /livros/{id}, e POST /livros continua só com `{id, titulo}` (sem a chave `disponivel`).
    - Os fakes (internal/http/apoio_test.go, listagem_test.go, fuzz_test.go) e os testes sem banco (internal/storage/sem_banco_test.go) passam a usar a assinatura nova.

NAO_FAZER:
  - Não acrescentar campo em `domain.Livro` (internal/domain/livro.go) nem criar `ReidratarLivro`. internal/domain/ não é tocado.
  - Não alterar `listarPorID` (internal/storage/repositorio.go:252-281): nem parâmetro novo, nem callback de pós-processamento, nem mudança no `+1`/teto. Também não tocar ListarUsuarios, ListarEmprestimos e sqlListarEmprestimos.
  - Não usar LEFT JOIN, JOIN com GROUP BY, `DISTINCT`, `bool_or`/`count(*)` agregado nem LATERAL para o bool.
  - Não fazer segunda consulta por página (ex. `SELECT livro_id FROM emprestimo WHERE livro_id = ANY($ids) AND devolucao IS NULL`) nem consulta por item (N+1), e não chamar BuscarEmprestimoAbertoPorLivro a partir do handler para montar o campo.
  - Não usar `now()`, `CURRENT_DATE`, `vencimento`, `$hoje`/`Ref`, nem ler o relógio (`a.agora()`) em listarLivros/obterLivro.
  - Não usar `COALESCE`, `($n IS NULL OR ...)` nem parametrizar `devolucao IS NULL` (o índice parcial deixa de casar no plano genérico).
  - Não forçar plano com `SET enable_*`, hint ou CTE MATERIALIZED. Se o teste mostrar outro índice que não `emprestimo_livro_aberto_uq`, o caso volta ao architecture-guardrails, sem contornar.
  - Não criar migração, índice, coluna, view nem tabela (CA6). migrations/ fica intocado.
  - Não mudar `saidaLivro` nem a resposta de POST /livros (nem `disponivel: true` fixo).
  - Não usar `*bool` nem `omitempty` em `disponivel`.
  - Não aceitar `?disponivel=` nem outra chave nova em GET /livros: `lerParametrosListagem` continua aceitando só `limite` e `apos` (filtro fora do escopo; a chave continua dando 400).
  - Não criar método paralelo (`BuscarLivroComDisponibilidade`, `ListarLivrosDisponibilidade`) mantendo o antigo, e não criar camada de serviço nova.
  - Não incluir título, id nem o bool em mensagem de erro ou log.
  - Não mexer em go.mod/go.sum.
  - Na coleção Postman, não salvar exemplos de resposta (D-22 P4) nem alterar a descrição de POST /livros.

CONFIRMADO:
  - BuscarLivro lê só `livro` (/home/rodrigo/workspace/biblioteca-bairro/internal/storage/repositorio.go:108-123). ListarLivros delega a `listarPorID` com `SELECT id, titulo FROM livro WHERE id > $1 ORDER BY id LIMIT $2` (:130-141). `listarPorID` é genérico em T e monta `args = [apos, limite+1, extra...]` (:252-281), então trocar o SQL e o T não exige mudar o helper.
  - BuscarLivro e ListarLivros têm um consumidor cada em produção: obterLivro (/home/rodrigo/workspace/biblioteca-bairro/internal/http/handler.go:507) e listarLivros (:366). A interface `Repositorio` está em handler.go:40-53 e já importa `storage.FiltroEmprestimos` (:52).
  - `saidaLivro` (handler.go:220-223) é usado em criarLivro (:286), listarLivros (:371-373, via saidaListaLivros :232-235) e obterLivro (:512).
  - `domain.Livro` tem só ID e Titulo (/home/rodrigo/workspace/biblioteca-bairro/internal/domain/livro.go:12-15).
  - O helper de plano já existe: `fixturePlano`, `planoGenerico` e `planoCustom`, com a constante `idxLivroAberto = "emprestimo_livro_aberto_uq"` (/home/rodrigo/workspace/biblioteca-bairro/internal/storage/plano_listagem_test.go:34, 51-78, 124-141). A fixture tem 500 livros e 499 abertos, um por livro, e o livro 1 está sem empréstimo aberto (:46-50, :74).
  - Invariantes lidos: /home/rodrigo/workspace/biblioteca-bairro/docs-ai/01-INVARIANTES-GLOBAIS.md (1.2, 1.4, 2.1, 3.2, 3.3) e /home/rodrigo/workspace/biblioteca-bairro/docs-ai/DECISOES.md (D-12, D-13 erros, D-15 log, D-19 contrato e "emprestado fora até decisão nova", D-20 listarPorID, D-21 SQL proibido, D-22 Postman). A última decisão é a D-22, então a próxima é a D-23.
  - Na coleção Postman, as descrições de GET /livros e GET /livros/{id} citam `{id, titulo}` (/home/rodrigo/workspace/biblioteca-bairro/postman/biblioteca-bairro.postman_collection.json:145 e :159). A de POST /livros (:99) fica.
  - O mapa de módulos descreve ListarLivros em /home/rodrigo/workspace/biblioteca-bairro/docs-ai/03-MAPA-RAPIDO-MODULOS.md:60.
  - `require_security: true` já está no report, e internal/http/ e internal/storage/ estão em `security_surface_paths` (/home/rodrigo/workspace/biblioteca-bairro/.pipeline-claude.json:41-55).

ASSUNCOES:
  - Assumo que o PostgreSQL 16 escolhe `emprestimo_livro_aberto_uq` no SubPlan (o menor índice cujo predicado casa, só linhas abertas) e não `emprestimo_livro_id_idx`/`emprestimo_livro_id_id_idx` com Filter. Não rodei EXPLAIN. A conferência é o teste de plano (A6), e divergência volta para cá (NAO_FAZER).
  - Roteamento: `data-model` está habilitado e a demanda toca internal/storage/, mas não há mudança de modelo, schema nem índice (P1 = A). A forma da consulta e a verificação de plano estão fechadas aqui e serão provadas em teste, por isso o próximo é o test-writer, como já indicava o pm-triage. Se a sessão principal ler a regra "toca persistência → data-model" de forma literal, o data-model tem só a conferência do índice parcial a fazer.
  - Este subagente não tem ferramenta de escrita: a seção Architecture/Guardrails do report vai abaixo (TEXTO_REPORT), para a sessão principal levar.

ARQUIVOS:
  - nenhum alterado por este subagente.
  Prováveis de impacto, para dev/test-writer/docs-update:
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/repositorio.go: tipo LivroComDisponibilidade, consts SQL, BuscarLivro e ListarLivros.
  - /home/rodrigo/workspace/biblioteca-bairro/internal/http/handler.go: interface Repositorio (:47-48), DTO novo de leitura, obterLivro e listarLivros/saidaListaLivros.
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/repositorio_test.go, listagem_test.go, sem_banco_test.go, plano_listagem_test.go (ou um arquivo novo de plano de livros no mesmo pacote).
  - /home/rodrigo/workspace/biblioteca-bairro/internal/http/apoio_test.go, listagem_test.go, fuzz_test.go e handler_test.go (asserção de POST /livros sem `disponivel`).
  - /home/rodrigo/workspace/biblioteca-bairro/postman/biblioteca-bairro.postman_collection.json: descrições em :145 e :159.
  - /home/rodrigo/workspace/biblioteca-bairro/docs-ai/DECISOES.md (D-23) e /home/rodrigo/workspace/biblioteca-bairro/docs-ai/03-MAPA-RAPIDO-MODULOS.md (linha de DEL-017).
  - /home/rodrigo/workspace/biblioteca-bairro/docs-ai/deliveries/DEL-017/report.md.
  Fora: internal/domain/, migrations/, go.mod, go.sum, cmd/.

RISCOS:
  - Contrato: `disponivel` é aditivo em GET /livros e GET /livros/{id} e quebra cliente com decodificação estrita (mesmo precedente da D-17 com `renovacoes`). GET /livros/{id} deixa de ter o mesmo DTO de POST /livros (revisão da D-19). Os dois pontos precisam estar na D-23.
  - Plano: se o planner preferir `emprestimo_livro_id_idx` + Filter, o resultado continua certo, mas o CA5 cai. O plano genérico (cache de prepared statement do pgx) também precisa ser conferido, além do custom.
  - Exposição (para o security-review): o estado "emprestado agora" de todo o acervo passa a sair em lote em GET /livros. Já era obtenível com `GET /emprestimos?situacao=aberto` e `GET /livros/{id}/emprestimo`, e o campo não traz usuario_id nem datas. Fica dentro do modelo de token único e operador único (D-15/D-19/D-21). Se surgirem vários tokens, entra na condição de bloqueio da D-21.
  - Consistência: leitura sem lock pode refletir o estado imediatamente anterior a uma abertura ou devolução concorrente (aceito no triage). A consulta é um só statement, então os itens de uma página são coerentes entre si.
  - Custo: até 101 sondas de índice por página (inclusive a linha +1, que é avaliada mas não escaneada). É desprezível com o índice parcial.

TEXTO_REPORT (seção "## Architecture/Guardrails" de /home/rodrigo/workspace/biblioteca-bairro/docs-ai/deliveries/DEL-017/report.md, substituir os dois placeholders):
- Invariantes aplicaveis: invariantes globais 1.2 (testes stdlib em tabela), 1.4 (sem migração: nada em migrations/), 2.1 (sem dependência nova), 3.2 (internal/http/ e internal/storage/ são superfície de segurança; require_security true e security-review) e 3.3 (erro não vaza SQL nem dado); D-12 (Livro é o exemplar; disponibilidade é estado derivado, não gravado e fora de `domain.Livro`); D-13 (erros do repositório sem SQL nem título; ErrNoRows → ErrLivroNaoEncontrado); D-14/D-15 (sem relógio: `disponivel` não depende de data, porque vencido também é indisponível); D-19 (paginação keyset inalterada, `LIMIT limite+1` contando livros; revisada em dois pontos: "emprestado na listagem" entra, e GET /livros/{id} deixa de usar o DTO de POST /livros); D-20 (`listarPorID` inalterado); D-21 (SQL só com texto constante, sem `now()`, COALESCE ou `($n IS NULL OR ...)`); D-22 (coleção Postman atualizada na mesma delivery). Abordagem: `NOT EXISTS (SELECT 1 FROM emprestimo e WHERE e.livro_id = l.id AND e.devolucao IS NULL)` na projeção de `SELECT ... FROM livro l` (uma consulta por página e por detalhe, sondando `emprestimo_livro_aberto_uq`, conferido por EXPLAIN genérico e custom em teste); tipo de leitura `storage.LivroComDisponibilidade{Livro, Disponivel}` nas assinaturas de BuscarLivro e ListarLivros; DTO de leitura novo com `disponivel` bool sem omitempty; POST /livros continua `{id, titulo}`; decisão nova D-23.
- Arquivos provaveis de impacto: internal/storage/repositorio.go; internal/http/handler.go; testes em internal/storage/ (repositorio_test.go, listagem_test.go, sem_banco_test.go, plano_listagem_test.go ou arquivo de plano novo) e em internal/http/ (apoio_test.go, listagem_test.go, fuzz_test.go, handler_test.go); postman/biblioteca-bairro.postman_collection.json (descrições de GET /livros e GET /livros/{id}); docs-ai/DECISOES.md (D-23); docs-ai/03-MAPA-RAPIDO-MODULOS.md. Não são tocados: internal/domain/, migrations/, go.mod, go.sum e cmd/.

SINAIS:
  SECURITY: sim — o diff toca internal/storage/ e internal/http/ (security_surface_paths). Há mudança de contrato aditiva e nova exposição em lote do estado de empréstimo do acervo, para avaliação.
  SUPPLY_CHAIN: nao — sem dependência nova; go.mod/go.sum intocados.
  MIGRATION: nao — sem schema nem índice novo; o índice parcial existente `emprestimo_livro_aberto_uq` atende.
  PERFORMANCE: sim — muda a consulta de GET /livros (até 101 linhas) e de GET /livros/{id}. query-performance não está habilitado, então a conferência fica no teste de plano (genérico e custom) e com o code-reviewer/QA.

PROXIMO: test-writer
MOTIVO: Abordagem, assinaturas, DTO e consulta estão fechados, e não há mudança de modelo nem de schema para o data-model. O test-writer escreve primeiro os testes de CA1-CA5 (tabela, com banco, e plano com `fixturePlano`) e os de contrato HTTP, incluindo POST /livros sem `disponivel`.
