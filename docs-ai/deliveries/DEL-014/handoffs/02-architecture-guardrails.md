DELIVERY_ID: DEL-014
TASK_CODE: DEL-014
AGENT: architecture-guardrails
STATUS: ok

DECISOES_DO_USUARIO (respostas às perguntas do triage, recebidas pela sessão principal):
  - U1 (filtros): FORA desta entrega, sem migração. Vale para situação (aberto/devolvido/vencido), por usuario_id, por livro_id e para GET /usuarios/{id}/emprestimos. Ficam para uma delivery seguinte (com índice composto/parcial, data-model e migration-reviewer). A1 do triage fica confirmada.
  - U2 (nome em lote): aceito. GET /usuarios devolve {id, nome}. O risco de vazamento em massa caso surjam vários tokens ou credenciais de usuário final deve ser registrado numa decisão (D-20 nova ou extensão da D-19, a critério do docs-update).

ABORDAGEM:
  Rotas (internal/http/handler.go, NovoHandler), todas atrás do mesmo auth(...):
    - mux.Handle("GET /usuarios", auth(listarUsuarios))
    - mux.Handle("GET /usuarios/{id}", auth(obterUsuario))
    - mux.Handle("GET /emprestimos", auth(listarEmprestimos))
  Contrato de listagem: idêntico à D-19, sem variação. As duas listagens usam lerParametrosListagem(r.URL.RawQuery) sem mudar a assinatura nem a gramática; o teto passa a vir de storage.LimiteListagem (abaixo). Resposta {"itens":[...],"proximo":<id>|null}, itens sempre array, proximo = id do último item entregue só quando temMais. Rota de detalhe ignora a query string.

  Storage (internal/storage/repositorio.go):
    - Teto único: nova constante exportada `LimiteListagem = 100`; `LimiteListagemLivros` passa a ser `= LimiteListagem` (mantida exportada e com o mesmo valor: os testes atuais a referenciam e não podem ser editados). ErrListagemInvalida continua a mesma sentinela, com o comentário estendido às três listagens; continua em errosDeBug (500, causa=bug_interno).
    - Revalidação e "+1" num só lugar (D-19): helper genérico NÃO exportado, por exemplo `listarPorID[T any](ctx, db DBTX, sql string, apos int64, limite int, ler func(pgx.Rows) (T, error)) ([]T, bool, error)`, que (1) recusa apos < 0 e limite fora de 1..LimiteListagem com ErrListagemInvalida antes de qualquer SQL; (2) executa o SQL com (apos, limite+1); (3) faz o laço idêntico ao atual de ListarLivros: ao chegar em len == limite marca temMais e sai SEM escanear a linha extra; (4) devolve slice vazia não nil; (5) rows.Close() + rows.Err() com traduzirErro. ListarLivros passa a delegar a ele sem mudar assinatura, SQL nem comportamento. Se o dev preferir não refatorar ListarLivros, aceita-se duplicar o laço nas duas funções novas, mas a validação tem de usar a mesma função/constante.
    - ListarUsuarios(ctx, apos int64, limite int) ([]domain.Usuario, bool, error) — SQL: `SELECT id, nome FROM usuario WHERE id > $1 ORDER BY id LIMIT $2`.
    - ListarEmprestimos(ctx, apos int64, limite int) ([]domain.Emprestimo, bool, error) — SQL: `SELECT `+colunasEmprestimo+` FROM emprestimo WHERE id > $1 ORDER BY id LIMIT $2`; cada linha reidratada com o lerEmprestimo existente (pgx.Rows satisfaz pgx.Row: só Scan), portanto mesma validação de domínio e mesmo erro "empréstimo gravado inválido" que o detalhe.
    - BuscarUsuario já existe (repositorio.go:177-192) e não muda; só entra na interface httpapi.Repositorio.
    - Interface httpapi.Repositorio (handler.go:40-50) ganha BuscarUsuario, ListarUsuarios, ListarEmprestimos.

  Handler:
    - listarUsuarios: espelho de listarLivros (handler.go:337-359), com saidaListaUsuarios{Itens []saidaUsuario; Proximo *int64}, reaproveitando saidaUsuario.
    - obterUsuario: parseID (id_invalido 400) → BuscarUsuario → `errors.Is(err, storage.ErrUsuarioNaoEncontrado)` mapeado LOCALMENTE para o código novo `usuario_inexistente` (404, mensagem fixa "usuário não encontrado"), no mesmo molde de obterEmprestimo (handler.go:386-392); demais erros por falharErro.
    - listarEmprestimos: lê parâmetros → `agora := a.agora()` UMA vez, antes da chamada ao repositório → ListarEmprestimos → monta TODOS os itens com o mesmo agora → só então responde 200. Para isso, extrair de responderEmprestimo (handler.go:462-482) uma função pura `montarSaidaEmprestimo(e domain.Emprestimo, agora time.Time) (saidaEmprestimo, error)`; responderEmprestimo passa a chamá-la (comportamento idêntico). Qualquer erro de Multa em qualquer item → falharErro (500) antes de escrever qualquer byte; nada de resposta parcial nem item pulado.

  Revisão das ASSUNCOES do triage:
    - A2 (rotas): confirmada.
    - A3 (campos): confirmada. Usuário {id, nome}; empréstimo = saidaEmprestimo atual (id, livro_id, usuario_id, inicio, vencimento, devolucao, multa, renovacoes). Sem JOIN, sem título nem nome embutidos.
    - A4 (404 do usuário): REVISADA. O catálogo (internal/http/erros.go:56-77) é map código → {status, mensagem}: `usuario_nao_encontrado` já é 422 e um mesmo código não pode ter dois status sem mudar o mecanismo do catálogo (fora de escopo). Decisão: código novo `usuario_inexistente` 404 "usuário não encontrado", usado só em GET /usuarios/{id}, mapeado no handler. classificar() continua levando storage.ErrUsuarioNaoEncontrado a usuario_nao_encontrado 422 nas rotas de empréstimo. Precedente: emprestimo_nao_encontrado vs emprestimo_aberto_nao_encontrado (D-19). O docs-update registra o código no catálogo da decisão.
    - A5 (teto): confirmada, 100 e padrão 20 para as três; forma = constante única LimiteListagem (acima). limitePadraoListagem (handler.go:499) continua única.
    - A6 (instante único): confirmada; um a.agora() por requisição, capturado antes do SQL (mesma ordem de obterEmprestimo). Aberto usa ref = agora; devolvido ignora ref (D-14).
    - A7 (exposição): confirmada pelo usuário (U2).
    - A8 (working tree): sem objeção de arquitetura; conferência linha a linha fica para o git-delivery/qa.

  Avaliação de desempenho (query-performance não habilitado):
    - usuario: `WHERE id > $1 ORDER BY id LIMIT $2` sobre usuario_pkey (migrations/0001_esquema_emprestimo.up.sql:11) → Index Scan na PK, custo O(limite+1), independente da posição. Sem COUNT, sem OFFSET.
    - emprestimo: idem sobre emprestimo_pkey (0001:22). Nenhum filtro → nenhum índice secundário necessário; nenhum JOIN.
    - Reidratação: uma única consulta por página (no máximo 101 linhas), reidratação em memória linha a linha; zero consultas adicionais por item. Não há N+1 porque o DTO não embute livro nem usuário.
    - Multa: Emprestimo.Multa/CalcularMulta (internal/domain/emprestimo.go:240-251, multa.go:35) é aritmética pura em memória, sem I/O; 100 cálculos por página são desprezíveis.
    - Pior caso de payload: usuários ~6 MB (nomes sem teto, como títulos na D-19); empréstimos ~25 KB por página (campos de tamanho fixo).

NAO_FAZER:
  - Não criar migração, índice, coluna ou view; não tocar migrations/. Filtros (situacao, usuario_id, livro_id) e GET /usuarios/{id}/emprestimos ficam fora (U1), inclusive como parâmetro "aceito e ignorado": qualquer chave além de limite/apos continua 400 parametro_invalido.
  - Não usar OFFSET, COUNT(*), total, cursor opaco/base64 nem ordenação vinda do cliente.
  - Não fazer JOIN de emprestimo com livro/usuario nem embutir titulo/nome no item de empréstimo.
  - Não buscar empréstimos um a um (BuscarEmprestimo em laço) nem fazer qualquer consulta por item da página.
  - Não usar pgx.CollectRows/RowToStructByName/ForEachRow no laço de listagem: eles escaneiam a linha extra (+1), e uma linha extra corrompida derrubaria uma página que não a entrega.
  - Não chamar a.agora() por item nem dentro do laço; não calcular a multa no SQL.
  - Não mudar classificar() nem a entrada de usuario_nao_encontrado (422) ou de storage.ErrUsuarioNaoEncontrado; o 404 é mapeamento local em obterUsuario. Não mudar a estrutura do catálogo para "código com status por rota".
  - Não renomear nem remover LimiteListagemLivros, ErrListagemInvalida, lerParametrosListagem, limitePadraoListagem, saidaUsuario, saidaEmprestimo, saidaListaLivros; não alterar a assinatura de ListarLivros nem o SQL dela.
  - Não editar os testes existentes de GET /livros (internal/http/listagem_test.go, fuzz_test.go, internal/storage/listagem_test.go, sem_banco_test.go) para acomodar a generalização; eles têm de passar como estão. Exceção esperada e única: o caso "listagem de emprestimos não existe" (internal/http/handler_test.go:722, GET /emprestimos → 404) deixa de ser verdade e deve ser substituído (ex.: GET /emprestimos/ com barra final → 404); e repoFake (internal/http/apoio_test.go) ganha os três métodos novos.
  - Não exportar o helper genérico nem criar pacote/camada de "repository genérico", query builder ou interface nova além dos três métodos.
  - Não pular silenciosamente empréstimo inválido nem devolver página parcial: erro de reidratação ou de multa → 500 da página inteira.
  - Não registrar no log nome, limite, apos, URL nem query; não ecoar valores recebidos nas mensagens de erro.
  - Não adicionar dependência (go.mod/go.sum intocados) nem usar r.URL.Query().
  - Não incluir Dockerfile/README nesta entrega.

CONFIRMADO:
  - Rotas atuais em internal/http/handler.go:131-142: não há GET /usuarios, GET /usuarios/{id} nem GET /emprestimos; POST /usuarios existe (135).
  - lerParametrosListagem (handler.go:505-533) usa url.ParseQuery e storage.LimiteListagemLivros (518); limitePadraoListagem = 20 (499).
  - ListarLivros (repositorio.go:123-153) valida antes do SQL, usa limite+1 e sai do laço sem escanear a linha extra (138-141).
  - BuscarUsuario já existe (repositorio.go:177-192), devolve ErrUsuarioNaoEncontrado em ausência e id <= 0.
  - lerEmprestimo(row pgx.Row) (repositorio.go:343-366) só usa Scan; reaproveitável com pgx.Rows.
  - Catálogo em internal/http/erros.go:56-77: usuario_nao_encontrado é 422 (64); classificar mapeia ErrUsuarioNaoEncontrado → 422 (107-108); ErrListagemInvalida em errosDeBug (131).
  - Multa é cálculo puro (internal/domain/emprestimo.go:240-251; internal/domain/multa.go:35-43); ReidratarEmprestimo valida vencimento > início, devolução >= início e renovacoes 0..2 (emprestimo.go:113-152).
  - Esquema: usuario_pkey e emprestimo_pkey em migrations/0001_esquema_emprestimo.up.sql:11 e :22; CHECK de renovacoes 0..2 no banco (migrations/0002_renovacao_emprestimo.up.sql:6); não há CHECK de vencimento > início nem de devolução >= início no banco.
  - Teste existente que muda de expectativa: internal/http/handler_test.go:722 (GET /emprestimos → 404).
  - Testes que referenciam LimiteListagemLivros: internal/storage/sem_banco_test.go:338-352,448 e internal/storage/listagem_test.go:69,178.
  - middleware.Log não registra URL nem query (internal/http/middleware/middleware.go:157).

ASSUNCOES:
  - O nome `usuario_inexistente` para o 404 é escolha desta etapa; se o humano preferir outro nome no PR, muda só a constante e o catálogo.
  - O helper genérico listarPorID é a forma recomendada, não obrigatória; o requisito é teto e validação num só lugar e ListarLivros sem mudança observável.
  - Consistência entre páginas segue a D-19 (sem snapshot); um empréstimo devolvido entre duas páginas aparece com o estado do momento em que sua página foi lida.

ARQUIVOS:
  - nenhum alterado nesta etapa (revisor sem escrita). Previstos para as próximas: internal/storage/repositorio.go, internal/http/handler.go, internal/http/erros.go, testes em internal/http/ e internal/storage/, docs-ai/DECISOES.md, docs-ai/deliveries/DEL-014/report.md.

RISCOS:
  - Contrato da API ampliado: três rotas novas e o código novo usuario_inexistente 404; GET /emprestimos deixa de ser 404 e /usuarios passa a anunciar Allow: GET, HEAD, POST no 405. Nenhuma rota, código ou DTO existente muda.
  - Exposição em massa (U2): com o token, extração de todos os nomes e de todo o histórico de empréstimos cruzável por usuario_id. Aceito no modelo token único; docs-update registra (D-20 ou extensão da D-19) que vários tokens/usuários finais exigem autorização por recurso antes nestas três rotas.
  - Linha de emprestimo inconsistente (vencimento <= início ou devolução < início, não barrados por CHECK no banco) faz 500 em toda página que a entregue, permanentemente, até correção do dado; o cliente contorna com apos. Aceito por coerência com GET /emprestimos/{id} (fail-closed); a linha extra (+1) não é escaneada, então só afeta a página que a contém.
  - Página de usuários até ~6 MB (nome sem teto), mesmo risco aceito da D-19.
  - Refatorar ListarLivros e responderEmprestimo pode mudar comportamento sem querer; a garantia são os testes atuais de livros, de detalhe de empréstimo e das rotas de empréstimo passando sem edição.
  - Off-by-one do cursor e ordem nas duas listagens novas só são pegos com PostgreSQL real (BIBLIOTECA_TEST_DB_OBRIGATORIO=1 no CI); os testes de storage precisam de fixture com usuários/empréstimos e livros suficientes para várias páginas.
  - Instante único: um teste deve fixar o relógio injetado e afirmar que o relógio é lido uma vez e que dois empréstimos abertos com o mesmo vencimento recebem a mesma multa.

SINAIS:
  SECURITY: sim — internal/http/ e internal/storage/ são security_surface_paths; entrada externa por query string e enumeração em lote de dado pessoal (nome) e de empréstimos
  SUPPLY_CHAIN: nao — nenhuma dependência nova; stdlib, pgx e decimal já presentes
  MIGRATION: nao — usa só usuario_pkey e emprestimo_pkey; filtros que pediriam índice ficaram fora (U1)
  PERFORMANCE: nao — keyset pela PK com LIMIT limite+1, uma consulta por página, sem JOIN, sem N+1, multa em memória; avaliado acima (query-performance não habilitado); code-reviewer confere o SQL final

PROXIMO: test-writer
MOTIVO: sem persistência nova nem mudança de esquema, data-model não se aplica; o test-writer escreve antes os testes das três rotas, de ListarUsuarios/ListarEmprestimos (com banco), do 404 usuario_inexistente, do instante único e a troca do caso handler_test.go:722, mantendo intactos os testes de livros.
