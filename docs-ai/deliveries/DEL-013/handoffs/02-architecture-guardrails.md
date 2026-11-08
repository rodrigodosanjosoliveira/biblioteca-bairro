DELIVERY_ID: DEL-013
TASK_CODE: DEL-013
AGENT: architecture-guardrails
STATUS: ok

ABORDAGEM:
  Escopo do triage ratificado: GET /livros (keyset), GET /livros/{id} e GET /emprestimos/{id}, todas atrás do mesmo auth(...) das rotas atuais. Só stdlib + pgx já presente. Sem mudança em internal/domain/, migrations/, middleware/, cmd/, go.mod/go.sum.

  Rotas (internal/http/handler.go, NovoHandler, ao lado das atuais):
    mux.Handle("GET /livros", auth(...listarLivros))
    mux.Handle("GET /livros/{id}", auth(...obterLivro))
    mux.Handle("GET /emprestimos/{id}", auth(...obterEmprestimo))
  Não conflitam com "GET /livros/{id}/emprestimo" (padrões do ServeMux 1.22+ disjuntos). "GET /livros/" com barra final continua 404 do mux, como hoje para rota inexistente.

  Interface Repositorio (handler.go:38) ganha exatamente 3 métodos:
    BuscarLivro(ctx, id int64) (domain.Livro, error)                 — já existe em storage (repositorio.go:94), só entra na interface.
    ListarLivros(ctx, apos int64, limite int) ([]domain.Livro, bool, error) — novo; o bool é "tem mais".
    BuscarEmprestimo(ctx, id int64) (domain.Emprestimo, error)       — novo.

  Storage (internal/storage/repositorio.go):
    - Constante exportada LimiteListagemLivros = 100 (teto único; o handler valida contra ela para responder 400, o repositório revalida como defesa).
    - ListarLivros: recusa antes de SQL apos < 0 ou limite fora de 1..LimiteListagemLivros com sentinela nova ErrListagemInvalida (erro de bug, não de cliente). SQL único, parametrizado:
        SELECT id, titulo FROM livro WHERE id > $1 ORDER BY id LIMIT $2
      com $2 = limite+1. Lê com r.db.Query + loop/pgx.CollectRows, defer rows.Close(), checa rows.Err(), erro via traduzirErro. Devolve no máximo `limite` itens e temMais = (linhas lidas > limite). Slice vazia (não nil) quando não há linhas.
    - BuscarEmprestimo: id <= 0 → ErrEmprestimoNaoEncontrado sem SQL (mesmo padrão de BuscarLivro); SQL `SELECT `+colunasEmprestimo+` FROM emprestimo WHERE id = $1` lido por lerEmprestimo (reidrata, sem linha → ErrEmprestimoNaoEncontrado). Sem filtro por devolucao: devolve aberto ou devolvido.

  HTTP:
    - Parsing de query numa função pura, p.ex. lerParametrosListagem(rawQuery string) (apos int64, limite int, codigo string), para teste em tabela e fuzz (FuzzParametrosListagem em internal/http/fuzz_test.go, mesmo estilo de FuzzParseID). Regras:
        * url.ParseQuery(r.URL.RawQuery); erro (escape inválido, ';') → 400 parametro_invalido. Não usar r.URL.Query(), que descarta erro silenciosamente.
        * chave fora de {limite, apos} → 400 parametro_invalido; chave repetida (len(vals) > 1) → 400 parametro_invalido.
        * apos: ausente → 0; presente → mesma semântica de parseID (strconv.ParseInt base 10, > 0). Inválido (vazio, 0, negativo, overflow, não decimal) → 400 parametro_invalido. Reusar parseID mantém o path id e o cursor com a mesma gramática (aceita "+5" e "007", como já aceita no path).
        * limite: ausente → 20; presente → strconv.ParseInt(s, 10, 64) e 1..storage.LimiteListagemLivros; fora disso → 400 parametro_invalido. Sem clamp silencioso.
    - listarLivros: valida query → context.WithTimeout(a.timeout) → ListarLivros(apos, limite) → 200 com
        {"itens":[{"id":..,"titulo":".."}], "proximo": <id>|null}
      DTO saidaListaLivros{Itens []saidaLivro `json:"itens"`; Proximo *int64 `json:"proximo"`}. proximo = ID do ÚLTIMO item devolvido quando temMais; null caso contrário. itens sempre array ([] e não null).
    - obterLivro: parseID → 400 id_invalido; BuscarLivro → falharErro (ErrLivroNaoEncontrado já mapeia para 404 livro_nao_encontrado); 200 com saidaLivro.
    - obterEmprestimo: parseID → 400 id_invalido; BuscarEmprestimo; se errors.Is(err, storage.ErrEmprestimoNaoEncontrado) → falhar(codEmprestimoNaoEncontrado) no próprio handler; demais erros → falharErro; sucesso → responderEmprestimo(w, r, 200, got, a.agora()). Multa: aberto usa ref = agora (D-14); devolvido ignora ref (Multa já faz isso, emprestimo.go:240-246).
    - erros.go: dois códigos novos no catálogo, mensagem fixa:
        codParametroInvalido = "parametro_invalido" → 400 "parâmetro de consulta inválido"
        codEmprestimoNaoEncontrado = "emprestimo_nao_encontrado" → 404 "empréstimo não encontrado"
      storage.ErrListagemInvalida entra em errosDeBug (500, causa=bug_interno).
    - Rotas de detalhe ignoram query string, como a GET /livros/{id}/emprestimo atual. GETs novos não chamam exigirCorpoVazio (coerente com o GET existente).

  Testes esperados (para o test-writer): tabela de parâmetros (ausentes, limites 0/1/100/101, apos 0/-1/overflow int64/"", espaços, "+", zeros à esquerda, chave desconhecida, chave repetida, %zz, ';'), paginação ponta a ponta no banco (página cheia com proximo, última página com proximo null, apos além do último id → itens [] e proximo null, cursor não pula nem repete itens), detalhe de empréstimo aberto e devolvido (multa nos dois casos), 404 com os códigos certos, 401 sem token nas três rotas, log sem query string. repoFake de apoio_test.go precisa dos 3 métodos novos; a asserção de interface (apoio_test.go:30) garante que *storage.Repositorio satisfaz.

  Decisão registrada (docs-update, D-19): contrato de paginação keyset (limite/apos/proximo, teto 100, padrão 20, ordem por id asc, sem total), códigos parametro_invalido e emprestimo_nao_encontrado, rejeição de parâmetro desconhecido/repetido. Atualizar a Consequência da D-15 (pendência cumprida) e o mapa de módulos.

NAO_FAZER:
  - Não calcular `proximo` a partir da linha extra (limite+1): isso faria a página seguinte pular um livro. proximo é o id do último item entregue.
  - Não usar OFFSET, COUNT(*) nem total de itens; não ordenar por titulo nem aceitar ordenação/filtro do cliente.
  - Não interpolar limite/apos no SQL (sempre $1/$2) e não usar fmt.Sprintf para montar consulta.
  - Não alterar classificar() para mapear storage.ErrEmprestimoNaoEncontrado globalmente para emprestimo_nao_encontrado: devolver/renovar/consultar por livro continuam respondendo emprestimo_aberto_nao_encontrado. O mapeamento novo fica só em obterEmprestimo.
  - Não renomear nem mudar o status/mensagem de nenhum código existente do catálogo.
  - Não usar r.URL.Query() (engole erro de parsing) nem r.FormValue/ParseForm (lê corpo).
  - Não clampar limite fora da faixa para 100/1: responder 400.
  - Não anotar limite, apos, URL nem query string no log (Anotar/ResponderErro); middleware.Log não muda.
  - Não ecoar o valor recebido na mensagem de erro.
  - Não juntar emprestimo/usuario na listagem (sem campo "emprestado", sem nome de usuário no DTO de empréstimo); nada de N+1 (uma consulta por requisição).
  - Não abrir transação nem snapshot entre páginas.
  - Não criar índice, migração, nem mexer em internal/domain/ ou internal/http/middleware/.
  - Não criar cursor opaco (base64/HMAC) nem pacote/abstração genérica de paginação; uma função de parsing local basta.
  - Não adicionar dependência (go.mod/go.sum intactos); não validar parâmetro com regexp quando strconv resolve.
  - Não incluir no commit Makefile, .pipeline-claude.lock nem .claude/agents/ (atualização rc.2 do usuário).

CONFIRMADO:
  - internal/http/handler.go:38-45: Repositorio tem 6 métodos, sem BuscarLivro/ListarLivros/BuscarEmprestimo; rotas em :127-134.
  - internal/http/handler.go:384-404 responderEmprestimo calcula multa com ref = agora e preenche devolucao só se fechado; reutilizável sem mudança.
  - internal/domain/emprestimo.go:240-251: Multa de empréstimo devolvido usa a devolução e ignora ref; aberto exige ref não zero.
  - internal/http/erros.go:105-106: classificar mapeia storage.ErrEmprestimoNaoEncontrado → emprestimo_aberto_nao_encontrado ("nenhum empréstimo aberto para o livro"), mensagem errada para detalhe por id — daí o mapeamento local em obterEmprestimo.
  - internal/http/erros.go:123-136: errosDeBug é o lugar da sentinela de chamada inválida.
  - internal/storage/repositorio.go:94-109 BuscarLivro existe e trata id<=0 e ErrNoRows; :150 colunasEmprestimo e :287 lerEmprestimo reutilizáveis; DBTX (:56-60) já tem Query.
  - migrations/0001_esquema_emprestimo.up.sql:1-6,15-22: livro.id e emprestimo.id bigint identity com livro_pkey/emprestimo_pkey (btree).
  - internal/http/middleware/middleware.go:155-181: Log registra só metodo, r.Pattern, status, duracao_ms, request_id e anotações; não registra URL nem query.
  - internal/domain/livro.go:19-25: título sem teto de tamanho no domínio; limite efetivo vem do corpo de 64 KiB (handler.go:23).
  - internal/http/fuzz_test.go:16 FuzzParseID: padrão de fuzz para o parsing novo.
  - docs-ai/DECISOES.md:282-291 (D-15 H3) e :356-358 (pendência de GET /emprestimos/{id}, listagens e paginação).

PERFORMANCE (query-performance desligado; avaliação aqui):
  - Listagem: `WHERE id > $1 ORDER BY id LIMIT $2` → plano esperado Limit → Index Scan using livro_pkey (Index Cond: id > $1), sem Sort; custo O(limite+1) por página independentemente da posição. Em tabela pequena o planner pode preferir Seq Scan + top-N sort, aceitável. Com statement cache do pgx (plano genérico) a forma continua Index Scan pela PK. Sem índice novo.
  - Detalhe de empréstimo: `WHERE id = $1` → Index Scan emprestimo_pkey, ≤1 linha. Detalhe de livro: já existente, livro_pkey.
  - Uma consulta por requisição nas três rotas; nada de N+1.
  - Teto: 100 itens (101 linhas lidas). Pior caso de resposta: ~100 × ~64 KiB de título ≈ 6,4 MB (título limitado só pelo corpo de 64 KiB); dentro do timeout de operação de 5s e do WriteTimeout, aceitável para operador único. Ver RISCOS.

ASSUNCOES:
  - Todas as assunções do triage ratificadas, com dois ajustes: (1) apos usa a gramática de parseID (aceita "+" e zeros à esquerda, como o path id) em vez de uma gramática mais estrita; (2) ListarLivros devolve temMais em vez de o handler pedir limite+1, para o teto e o +1 ficarem num só lugar.
  - Nenhuma decisão aqui é considerada genuinamente humana: as rotas são aditivas (nenhum contrato existente muda), não há cliente externo conhecido além do operador, e tudo é reversível. Por isso não bloqueio; o contrato vai como D-19 e o humano o vê no PR.
  - LimiteListagemLivros exportado em storage (e não em httpapi) assume que o teto é também uma proteção do repositório; se o code-reviewer preferir o teto só no HTTP, a mudança é local.
  - IDs começam em 1 (identity padrão), então apos=0 implícito cobre o início.

ARQUIVOS:
  - nenhum (revisor sem escrita). Previstos para a implementação: internal/http/handler.go, internal/http/erros.go, internal/storage/repositorio.go, testes em internal/http/ e internal/storage/, docs-ai/DECISOES.md e docs-ai/03-MAPA-RAPIDO-MODULOS.md (docs-update).

RISCOS:
  - Contrato público novo (rotas, payload {itens, proximo}, códigos 400 parametro_invalido e 404 emprestimo_nao_encontrado): aditivo, mas passa a ser contrato; registrar na D-19.
  - Regressão de código de erro: se alguém mexer em classificar() em vez do handler, devolver/renovar passam a responder emprestimo_nao_encontrado; o test-writer deve fixar os códigos das rotas antigas.
  - Off-by-one no cursor (proximo vindo da linha extra) — teste de banco percorrendo todas as páginas e comparando com o conjunto completo.
  - Resposta de até ~6,4 MB por página no pior caso (títulos longos); se incomodar, reduzir o teto ou limitar o título no domínio em delivery própria.
  - Rejeitar parâmetro desconhecido quebra clientes que mandem cache-buster (?_=...); decisão consciente, coerente com DisallowUnknownFields.
  - Enumeração do acervo e dos empréstimos por quem tem o token (já aceito na D-15, operador único); a listagem aumenta o volume por chamada.
  - proximo como número JSON acima de 2^53 perde precisão em JS; mesmo risco já existente para os IDs (D-15).

SINAIS:
  SECURITY: sim — rotas autenticadas novas e entrada externa nova (query string) em internal/http/, consultas novas em internal/storage/ (security_surface_paths); exige security-review e fuzz do parsing.
  SUPPLY_CHAIN: nao — stdlib (net/url, strconv) e pgx já presentes; go.mod/go.sum não mudam.
  MIGRATION: nao — livro_pkey e emprestimo_pkey atendem as consultas; nenhum DDL.
  PERFORMANCE: sim — consulta de listagem nova; avaliada acima (Index Scan pela PK, LIMIT 101, sem N+1); code-reviewer confere o teto e a ausência de OFFSET/COUNT.

PROXIMO: test-writer
MOTIVO: abordagem e limites definidos sem mudança de esquema nem de entidade; data-model está habilitado, mas a demanda só acrescenta leituras sobre tabelas e PKs existentes (sem DDL, sem campo novo, sem mudança em internal/domain/), e o plano das consultas já foi avaliado aqui. Os testes em tabela, de banco e de fuzz do contrato acima vêm antes da implementação.
