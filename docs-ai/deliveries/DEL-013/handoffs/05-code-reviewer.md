DELIVERY_ID: DEL-013
TASK_CODE: DEL-013
AGENT: code-reviewer
STATUS: ok

RESULTADO: aprovado sem bloqueios. Diff lido por inteiro: 9 arquivos rastreados via pipeline_diff (base origin/main, merge-base d870750) e 3 novos não rastreados lidos com Read (internal/http/listagem_test.go, internal/http/banco_listagem_test.go, internal/storage/listagem_test.go). Conferido contra o NAO_FAZER do 02-architecture-guardrails, item por item.

BLOQUEIOS:
  - nenhum

SUGESTOES:
  - [internal/http/handler.go:354] A guarda `temMais && len(livros) > 0` descarta sem aviso um temMais=true com página vazia, que seria violação de contrato do repositório. Hoje não acontece (repositorio.go:138 só marca temMais depois de ter `limite` itens). Se quiser tornar a violação visível, dá para tratá-la como bug interno (500 bug_interno) em vez de responder proximo null. Não impede o merge.
  - [internal/storage/repositorio.go:134,148] Há `defer rows.Close()` e também `rows.Close()` explícito. Os dois estão corretos: o Close explícito é necessário para que rows.Err() reflita o fim da leitura depois do `break` na linha extra. Um comentário de uma linha em :148 explicaria por que existem os dois e evitaria que alguém remova um deles.
  - [internal/http/handler.go:518] O teto do HTTP vem de storage.LimiteListagemLivros. É aceitável, porque httpapi já importa storage (erros.go) e o 02 previu isso. Fica registrado para a D-19 que o teto mora no storage e que o handler só o reaproveita.
  - [internal/http/handler.go:337-359] A resposta da listagem pode passar de 6 MB no pior caso (100 títulos de cerca de 64 KiB). O risco já foi aceito no 02. Se incomodar, a correção vai numa delivery própria: limitar o título no domínio ou baixar o teto.

CONFIRMADO:
  - NAO_FAZER do 02, item por item:
    * proximo vem do último item entregue: handler.go:354-356 usa `livros[len(livros)-1].ID`. O repositório (repositorio.go:137-145) para ao ver a linha limite+1 sem incluí-la nem expor o id dela.
    * Sem OFFSET, COUNT nem total. Ordem fixa por id e nenhum filtro ou ordenação vindo do cliente (repositorio.go:129).
    * SQL parametrizado ($1, $2), sem fmt.Sprintf (repositorio.go:128-130 e 329-331).
    * classificar() não mudou (erros.go no diff só ganha o catálogo e errosDeBug). O mapeamento local de ErrEmprestimoNaoEncontrado para emprestimo_nao_encontrado fica em handler.go:387-392. TestRotasAntigasMantemEmprestimoAbertoNaoEncontrado e TestCatalogoListagemEDetalhe fixam isso.
    * Nenhum código existente do catálogo foi renomeado ou alterado (erros.go: só adições).
    * O parsing usa url.ParseQuery(r.URL.RawQuery) e o erro é tratado (handler.go:505-509). Não há r.URL.Query(), FormValue nem ParseForm.
    * Sem clamp: limite fora de 1..100 e apos inválido devolvem ok=false e a resposta é 400 parametro_invalido (handler.go:518-527).
    * Nenhum Anotar de limite, apos ou query. middleware/ fora do diff. TestLogListagemSemQuery e TestListarLivrosErroNaoEcoaEntrada cobrem.
    * As mensagens de erro são fixas do catálogo, sem eco do valor recebido. ErrListagemInvalida não traz valores (repositorio.go:38).
    * Sem join com emprestimo ou usuario e sem N+1: uma consulta por requisição nas três rotas.
    * Sem transação, migração, índice, mudança em internal/domain/ ou middleware/, e go.mod/go.sum intactos. Os 12 arquivos do diff estão todos em internal/http e internal/storage.
    * Sem cursor opaco e sem abstração genérica: a função local é lerParametrosListagem (handler.go:505), sem regexp.
  - PERFORMANCE (query-performance não habilitado):
    * repositorio.go:129-130: `SELECT id, titulo FROM livro WHERE id > $1 ORDER BY id LIMIT $2` com $2 = limite+1, no máximo 101 linhas. É keyset pela PK livro_pkey, sem Sort no plano esperado e com custo independente da página.
    * repositorio.go:125: a validação (apos >= 0, 1 <= limite <= 100) vem antes do SQL e funciona como defesa em profundidade.
    * repositorio.go:136: o slice é pré-alocado com cap = limite e fica vazio (não nil) quando não há linhas.
    * repositorio.go:330: o detalhe faz `WHERE id = $1` pela PK emprestimo_pkey e lê com lerEmprestimo.
  - Go:
    * Nenhum erro foi ignorado. O único `_ =` é o encoder em handler.go:489, que já existia antes.
    * context.WithTimeout(a.timeout) está nos três handlers novos (handler.go:343, 367, 384), com defer cancel fora de loop.
    * rows é fechado em todos os caminhos e rows.Err() é checado. TestListarLivrosErroDuranteLeitura cobre esse caso.
    * Sem goroutine nova e sem estado compartilhado mutável.
    * A interface Repositorio continua no pacote consumidor (handler.go:41-50).
    * `int(n)` em handler.go:521 só roda depois da checagem 1..100, sem risco de overflow.
    * A ordem de iteração do map em lerParametrosListagem não muda o resultado: qualquer desvio devolve false.
  - HTTP:
    * A query é validada antes de qualquer chamada ao repositório.
    * Os status estão coerentes: 400 parametro_invalido e id_invalido, 404 livro_nao_encontrado e emprestimo_nao_encontrado, 500 bug_interno para ErrListagemInvalida, 503 em deadline e cancelamento.
    * As três rotas estão atrás de auth (handler.go:140-142), e TestAuthAntesDeTudo confirma que o repositório não é chamado sem token.
    * obterEmprestimo calcula agora antes da busca, como consultarEmprestimo (handler.go:326 e 383). Isso preserva a D-14: um empréstimo aberto usa ref = agora e um devolvido ignora ref.
  - Testes:
    * As mudanças em testes pré-existentes acompanham as rotas novas e não afrouxam nada. Em handler_test.go, "GET /livros → 405" e "GET /livros/1 → 404" foram substituídos por casos equivalentes em rotas que continuam inválidas. Em log_test.go, "405 do mux" passou a usar PUT /livros.
    * O dev declara que não tocou nos testes depois do 03, e o conteúdo bate com o contrato descrito no handoff 03.
  - Nenhum segredo em código. Os nomes descrevem o que fazem.

ASSUNCOES:
  - Não reexecutei os gates (revisor só lê). Usei como evidência os sensores, a suíte com banco (0 fail, 0 skip) e o fuzz de 30s registrados no 04-dev-implementation.
  - Considerei a autoria dos testes conforme os handoffs 03 e 04 (test-writer antes, dev sem alterá-los). Não há como separar as etapas pelo git, porque nada foi commitado.

ARQUIVOS:
  - nenhum (revisor sem escrita)

RISCOS:
  - O off-by-one do cursor e o ORDER BY só são pegos com banco real. O QA deve rodar a suíte com BIBLIOTECA_TEST_DATABASE_URL e BIBLIOTECA_TEST_DB_OBRIGATORIO=1.
  - Continuam os riscos do 02: contrato público novo (registrar na D-19), rejeição de cache-buster, enumeração com token e resposta de vários MB no pior caso.

SINAIS:
  SECURITY: sim — rotas autenticadas novas, entrada externa por query string e consultas novas em internal/http/ e internal/storage/ (security_surface_paths)
  SUPPLY_CHAIN: nao — go.mod/go.sum intactos
  MIGRATION: nao — nenhum DDL
  PERFORMANCE: sim — keyset pela PK com LIMIT limite+1 (no máximo 101), sem OFFSET, COUNT ou N+1, conferido aqui porque query-performance não está habilitado

PROXIMO: security-review
MOTIVO: sem bloqueios. O diff toca security_surface_paths e SECURITY é sim. migration-reviewer não se aplica (MIGRATION nao). query-performance e tenant-isolation não estão habilitados. A performance da consulta foi conferida acima.
