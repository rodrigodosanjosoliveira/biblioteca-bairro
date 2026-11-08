DELIVERY_ID: DEL-013
TASK_CODE: DEL-013
AGENT: security-review
STATUS: ok

RESULTADO: aprovado. Nenhum achado CRITICO ou ALTO. Dois achados BAIXO, ambos já aceitos por decisão registrada (D-15/D-17 e o handoff 02).

SUPERFICIE_REVISADA:
  - internal/http/handler.go: rotas novas (linhas 140-142), listarLivros, obterLivro, obterEmprestimo e lerParametrosListagem
  - internal/http/erros.go: códigos parametro_invalido e emprestimo_nao_encontrado, e ErrListagemInvalida em errosDeBug
  - internal/storage/repositorio.go: ListarLivros, BuscarEmprestimo e LimiteListagemLivros
  - internal/http/middleware/middleware.go: só lido, fora do diff, para confirmar o que vai para o log (metodo, rota=r.Pattern e request_id; nunca path nem query)
  - testes do diff: apoio_test.go, erros_interno_test.go, fuzz_test.go, handler_test.go, log_test.go, sem_banco_test.go. Nos não rastreados (listagem_test.go, banco_listagem_test.go, storage/listagem_test.go) procurei credenciais e literais sensíveis: só há marcadores usados para provar que nada vaza ("SECRETO", "QSSECRETO")
  - docs-ai/DECISOES.md D-15 (token Bearer estático, operador único) e D-17 (consequências: sem autorização por dono)

VULN (sensor vuln, govulncheck, resultado real):
  ok=true, exit_code=0, saída: "No vulnerabilities found."

ACHADOS:
  - [BAIXO] Enumeração de empréstimos e exposição de usuario_id (handler.go obterEmprestimo e repositorio.go BuscarEmprestimo). Com o token, ids sequenciais em GET /emprestimos/{id} revelam todos os empréstimos, abertos e devolvidos, com livro_id, usuario_id, datas e multa. GET /livros?apos= percorre o acervo inteiro. Isso fica dentro do modelo aceito: há um único token, de um operador único (D-15), e a autorização por dono do empréstimo está explicitamente fora (D-17). Não há escalada nova, porque quem tem o token já lê e altera qualquer empréstimo pelas rotas por livro. O 404 distingue um id inexistente de um existente, o que só tem valor para quem já está autenticado. Ação: nenhuma nesta delivery. Se surgirem papéis ou usuários finais com credencial própria, as três rotas precisam de autorização por recurso. Registrar na D-19.
  - [BAIXO] Amplificação de resposta (handler.go listarLivros). No pior caso, uma página tem cerca de 6 MB (100 títulos de até 64 KiB). A rota exige auth e o risco já foi aceito no 02. Mitigação futura: teto no título no domínio ou teto menor de página.

INSPECIONADO SEM ACHADO:
  - Autenticação: as três rotas passam por auth() (handler.go:140-142). TestAuthAntesDeTudo cobre query inválida, id inválido e escape inválido sem token, e confirma 401 e repositório com zero chamadas. A auth vem antes da validação, então a query não serve de oráculo para quem não tem token.
  - Entrada externa: url.ParseQuery sobre RawQuery, com erro tratado. Chaves só limite e apos, cada uma uma vez. Duplicadas, desconhecidas e escapes inválidos dão 400. limite é decimal em 1..100 e o cast int(n) só acontece depois da faixa. apos passa por parseID (>0). Não há clamp nem eco. O repositório revalida (apos>=0, 1<=limite<=100) antes do SQL, como defesa em profundidade. FuzzParametrosListagem usa um oráculo independente. Os ids de path passam por parseID e BuscarEmprestimo recusa id<=0 antes do SQL.
  - SQL: só parâmetros ($1, $2), sem concatenação de entrada. A única concatenação é a constante colunasEmprestimo. Ordem fixa por id e nenhuma coluna ou ordenação vinda do cliente. LIMIT limite+1 tem no máximo 101 linhas.
  - Erros: as mensagens vêm do catálogo fixo. ErrListagemInvalida vira 500 bug_interno, sem detalhe. PgError não vaza Message nem Detail (TestRepositorioErroSemDados com "SEGREDO" e "Titulo Secreto Do Acervo", e os casos pgErroSecreto em listagem_test.go). classificar() continua igual. O mapeamento local de ErrEmprestimoNaoEncontrado não altera as rotas antigas.
  - Log: nenhum Anotar de query, limite ou apos. O middleware registra r.Pattern, não o path, então ids e query não vão para o log (TestLogListagemSemQuery e o teste com QSSECRETO).
  - Timeout: context.WithTimeout nos três handlers, o que limita a duração da consulta.
  - Segredos: nenhum em código ou em teste. go.mod e go.sum intactos. Nenhum workflow de CI no diff.

ASSUNCOES:
  - Considerei o modelo de token único das D-15 e D-17 como vigente. A severidade BAIXO da enumeração depende dele.
  - Não reexecutei a suíte nem o fuzz (revisor só lê). Como evidência de execução, usei os registros do 04-dev-implementation.

ARQUIVOS:
  - nenhum (revisor sem escrita)

RISCOS:
  - Se o modelo de autenticação mudar (vários tokens ou usuários finais), GET /emprestimos/{id} passa a ser IDOR e precisará de autorização por dono do empréstimo.

SINAIS:
  SECURITY: sim — revisado aqui. Sem bloqueio, dois achados BAIXO aceitos por decisão registrada
  SUPPLY_CHAIN: nao — go.mod/go.sum intactos
  MIGRATION: nao — nenhum DDL
  PERFORMANCE: sim — conferido pelo code-reviewer (keyset pela PK, até 101 linhas)

PROXIMO: qa-validation
MOTIVO: sem achado CRITICO ou ALTO, govulncheck limpo e SUPPLY_CHAIN nao.
