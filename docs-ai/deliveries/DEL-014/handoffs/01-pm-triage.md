DELIVERY_ID: DEL-014
TASK_CODE: DEL-014
AGENT: pm-triage
STATUS: ok

TIPO: feature (comportamento novo: rotas de leitura que não existem)

ESCOPO:
  - GET /usuarios: listagem paginada por cursor, reaproveitando o contrato da D-19 sem variação: query só `limite` (1..100, padrão 20, fora da faixa 400 sem clamp) e `apos` (gramática do parseID, ausente = início); ordem por id ascendente; resposta {"itens":[...],"proximo":<id>|null}; proximo = id do último item entregue; sem total, sem OFFSET, sem COUNT; chave desconhecida, repetida, escape inválido ou `;` → 400 parametro_invalido; leitura com url.ParseQuery(r.URL.RawQuery). Item: mesmo DTO de POST /usuarios ({id, nome}).
  - GET /usuarios/{id}: detalhe do usuário, mesmo DTO de POST /usuarios; ignora a query string, como as rotas de detalhe da D-19.
  - GET /emprestimos: listagem paginada por cursor no mesmo contrato, keyset pela PK emprestimo_pkey; item = mesmo DTO de GET /emprestimos/{id} (com renovacoes e multa; aberto usa ref = relógio injetado, devolvido ignora ref, D-14), um único instante de referência por requisição.
  - As três rotas atrás do mesmo auth(...), com context.WithTimeout por operação, log sem limite/apos/URL/query/nome.
  - Storage: ListarUsuarios, BuscarUsuario (já existe; conferir uso) e ListarEmprestimos, com revalidação de apos/limite antes do SQL como em ListarLivros (ErrListagemInvalida → 500 bug_interno). Como o teto e o +1 se generalizam é decisão do architecture-guardrails.
  - Working tree: a marcação merged da DEL-013 em docs-ai/deliveries/INDEX.md e docs-ai/deliveries/DEL-013/report.md (feita pelo pipeline_index_sync, ainda não commitada) entra nesta entrega, no commit da DEL-014.

FORA_DE_ESCOPO:
  - Filtros em GET /emprestimos (aberto, devolvido, vencido, por usuario_id, por livro_id) e GET /usuarios/{id}/emprestimos: keyset com filtro por usuário em ordem de id pede índice composto (usuario_id, id), e por situação um índice parcial; isso é migração e fica para delivery própria (recorte proposto abaixo).
  - Busca por nome, ordenação configurável, contagem total, cursor opaco.
  - Qualquer índice novo ou migração.
  - Autorização por recurso, múltiplos tokens, escopo por usuário final.
  - Mudança em rota, código de erro, mensagem ou DTO existente (inclusive o mapeamento global de storage.ErrUsuarioNaoEncontrado → 422 nas rotas de empréstimo).
  - Teto de tamanho de nome/título e mudança no teto de 100.
  - Dockerfile/README (pendência da memória, outra delivery).

CONFIRMADO:
  - Tabela `usuario` existe (id bigint identity, nome text NOT NULL, usuario_pkey, usuario_nome_check) e `emprestimo` com emprestimo_pkey, índices emprestimo_livro_id_idx, emprestimo_usuario_id_idx e o parcial emprestimo_livro_aberto_uq: migrations/0001_esquema_emprestimo.up.sql. Coluna renovacoes em migrations/0002. Nenhum esquema novo é necessário para o recorte.
  - Rotas atuais em internal/http/handler.go:132-142: não há GET /usuarios, GET /usuarios/{id} nem GET /emprestimos.
  - Contrato e implementação a reaproveitar: lerParametrosListagem e listarLivros em internal/http/handler.go (~345, ~503-518); ListarLivros, LimiteListagemLivros e ErrListagemInvalida em internal/storage/repositorio.go:35-42 e :118-124; testes em internal/http/listagem_test.go, internal/http/fuzz_test.go, internal/storage/listagem_test.go e internal/storage/sem_banco_test.go.
  - Pendência registrada: D-15 Consequências ("listagens de usuários e de empréstimos continuam pendentes") e D-19 Consequências (listagens de usuários e de empréstimos e GET /usuarios/{id} fora até decisão nova), docs-ai/DECISOES.md.
  - internal/http/ e internal/storage/ estão em security_surface_paths (.pipeline-claude.json); delivery criada com require_security.
  - Módulos habilitados: data-model e migration-reviewer (enabled_modules); query-performance não está habilitado.

ASSUNCOES:
  - A1 (recorte): a demanda não fixa rotas nem filtros. Padrão proposto: as duas listagens sem filtro + GET /usuarios/{id}, sem migração. Filtros (situacao=aberto|devolvido|vencido, usuario_id, livro_id) e GET /usuarios/{id}/emprestimos ficam para uma DEL seguinte, que trará o índice composto. Se o humano quiser filtros já, o escopo cresce com migração, data-model e migration-reviewer.
  - A2 (rotas): GET /usuarios, GET /usuarios/{id} e GET /emprestimos, nomes no plural coerentes com POST /usuarios e GET /emprestimos/{id}.
  - A3 (campos): usuário {id, nome}, igual ao POST /usuarios; empréstimo igual ao DTO de GET /emprestimos/{id} (livro_id, usuario_id, inicio, vencimento, devolucao, renovacoes, multa). Sem título do livro nem nome do usuário embutidos no empréstimo (evita JOIN e mais dado pessoal).
  - A4 (404 do usuário): GET /usuarios/{id} sem usuário responde 404 com código `usuario_nao_encontrado`, mapeado só no handler de detalhe (precedente: emprestimo_nao_encontrado na D-19), mantendo o 422 nas rotas de empréstimo. Alternativa: código novo distinto. Decide o architecture-guardrails; não muda código existente.
  - A5 (teto): o mesmo teto de 100 e padrão 20 para as três listagens; a forma (constante única ou uma por recurso) fica com o architecture-guardrails.
  - A6 (multa na listagem): calculada por item com um único `agora` do relógio injetado por requisição, para a página ser coerente.
  - A7 (exposição): o modelo continua token único e operador único (D-15, D-17, D-19); as listagens são aceitas nesse modelo, com o risco registrado abaixo. Se o humano discordar (por exemplo, quiser omitir `nome` na listagem), é decisão de produto no PR, não bloqueio da triagem.
  - A8 (working tree): as alterações em INDEX.md e DEL-013/report.md são só a marcação merged da DEL-013 (D-14, status de deliveries); não foram conferidas linha a linha nesta triagem.

ARQUIVOS:
  - docs-ai/deliveries/DEL-014/report.md — criado pela pipeline_delivery_new (escopo e require_security: true)
  - docs-ai/deliveries/INDEX.md — linha da DEL-014 registrada pela ferramenta

RISCOS:
  - Exposição ampliada (D-19): quem tem o token passa a extrair em lote todos os nomes de usuários (único dado pessoal, D-12) e o histórico completo de empréstimos com livro_id, usuario_id, datas e multa, cruzáveis entre si. Com vários tokens ou credenciais de usuário final, as três rotas viram IDOR/vazamento em massa e precisam de autorização por recurso antes. Registrar no report e na decisão nova.
  - `nome` não pode aparecer em log nem em mensagem de erro; testes devem afirmar que o log da listagem não carrega nomes, limite, apos nem query.
  - Off-by-one do cursor e ordem só são pegos com banco: testes de storage com PostgreSQL real (BIBLIOTECA_TEST_DB_OBRIGATORIO=1 no CI).
  - Página de usuários pode ter ~6 MB no pior caso (nome limitado só pelo corpo de 64 KiB), como o risco aceito da D-19 para títulos.
  - Generalizar lerParametrosListagem/ErrListagemInvalida pode alterar sem querer o comportamento de GET /livros: os testes atuais da listagem de livros precisam continuar verdes sem edição.
  - Reidratação de empréstimo em lote: linha inválida no banco (renovacoes fora de 0..2) derruba a página inteira com 500; aceitar ou não é do architecture-guardrails.

SINAIS:
  SECURITY: sim — toca internal/http/ e internal/storage/ (security_surface_paths), entrada externa por query string e amplia a enumeração de dado pessoal (nomes) e de empréstimos
  SUPPLY_CHAIN: nao — nenhuma dependência nova prevista; stdlib e pgx já presentes
  MIGRATION: nao — o recorte usa só as PKs existentes; filtros que exigiriam índice ficaram fora
  PERFORMANCE: sim — duas consultas keyset novas em internal/storage (conferir que usam usuario_pkey/emprestimo_pkey, LIMIT limite+1, sem N+1 nem JOIN) e cálculo de multa por item; query-performance não habilitado, a conferência fica com architecture-guardrails e code-reviewer

PROXIMO: architecture-guardrails
MOTIVO: feature em internal/http e internal/storage; precisa fixar a generalização do contrato da D-19 (parâmetros, teto, erro de listagem), o mapeamento do 404 de usuário (A4) e as assinaturas do repositório antes dos testes. Sem migração, data-model não é necessário salvo se o architecture-guardrails discordar do recorte A1.
