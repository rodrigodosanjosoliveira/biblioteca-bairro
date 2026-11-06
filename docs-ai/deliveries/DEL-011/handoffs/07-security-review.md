DELIVERY_ID: DEL-011
TASK_CODE: DEL-011
AGENT: security-review
STATUS: ok

SUPERFICIE_REVISADA:
  - internal/http/handler.go (rota POST /livros/{id}/renovacao, handler renovar, DTO saidaEmprestimo)
  - internal/http/erros.go (códigos novos, classificar, errosDeBug, causaInterna)
  - internal/storage/repositorio.go (RegistrarRenovacao, colunasEmprestimo, lerEmprestimo)
  - internal/domain/emprestimo.go (Renovar, ReidratarEmprestimo com contador)
  - migrations/0002_renovacao_emprestimo.up.sql e .down.sql
  - internal/http/apoio_test.go (rotasProtegidas)
  - Diff obtido via pipeline_diff (base origin/main, merge-base 1ad40d3). go.mod/go.sum fora do diff.

GOVULNCHECK (pipeline_sensors names=["vuln"]):
  ok: true, exit_code: 0, duration_ms: 4838
  output: "No vulnerabilities found."

ACHADOS:
  - BAIXO [internal/http/handler.go:133] Autorização: a rota nova está atrás de middleware.Auth (bearer token único compartilhado), igual a devolucao/emprestimo. Não há autorização por usuário: qualquer portador do token renova o empréstimo de qualquer livro. É o modelo de autorização já existente do projeto (API operada pela biblioteca), não uma regressão desta delivery. Registro apenas para ciência; se no futuro o token for distribuído a leitores, renovação e devolução precisam de checagem por usuario_id.
  - BAIXO [internal/http/handler.go:222 / responderEmprestimo] O campo novo `renovacoes` na resposta não expõe dado sensível (contador inteiro). Sem achado de vazamento.

CONFIRMADO:
  - Autenticação: rota registrada com auth(...) (handler.go:133) e coberta pelo teste de rotas protegidas (apoio_test.go:344, "POST renovacao").
  - Entrada externa: apenas o path {id}, validado por parseID antes de qualquer I/O (handler.go:353-357); corpo exigido vazio via exigirCorpoVazio, que lê no máximo 1 byte (handler.go:448-461). A data de renovação vem do relógio injetado a.agora(), não do cliente, então o cliente não consegue manipular vencimento nem contador.
  - SQL: UPDATE e SELECT de diagnóstico 100% parametrizados ($1..$5 e $1); colunasEmprestimo é constante de compilação concatenada, sem dado externo (repositorio.go:238-252).
  - Integridade/concorrência: CAS por id + devolucao IS NULL + inicio + renovacoes anterior impede renovação dupla em corrida (exceder o limite); CHECK no banco (renovacoes 0..2) como segunda barreira; ReidratarEmprestimo recusa contador fora da faixa.
  - Erros: códigos 409 com mensagens fixas do catálogo (erros.go:67-68). Falhas inesperadas viram 500 "erro interno"; o log só anota causa classificada e, para PgError, apenas Code e ConstraintName (erros.go:139-157) — sem query, caminho ou stack na resposta nem no log. ErrRenovacoesInvalidas entra em errosDeBug.
  - Timeout: contexto derivado de r.Context() com WithTimeout e defer cancel (handler.go:363-364); deadline vira 503 sem detalhe.
  - Segredo: nenhum segredo, credencial ou DSN em código, migração ou log no diff.
  - Criptografia: não tocada.
  - CI: nenhum workflow alterado no diff.
  - Migração: DDL estática, sem dado dinâmico; down sem CASCADE (não derruba objetos dependentes silenciosamente). Revisão operacional fica com o migration-reviewer, em paralelo.

ASSUNCOES:
  - middleware.Auth não foi alterado no diff; assumi o comportamento já revisado em deliveries anteriores (token vazio recusa tudo, handler.go:58).

ARQUIVOS:
  - nenhum (revisor só leitura).

RISCOS:
  - Ordem de deploy (binário novo sem a 0002 aplicada gera 500 em todas as rotas de empréstimo) é risco de disponibilidade, não de segurança; fica com migration-reviewer/QA.

SINAIS:
  SECURITY: sim — revisado; sem achado CRITICO/ALTO/MEDIO.
  SUPPLY_CHAIN: nao — go.mod e go.sum fora do diff.
  MIGRATION: sim — migrations/0002 (revisão em paralelo pelo migration-reviewer).
  PERFORMANCE: nao

PROXIMO: qa-validation
MOTIVO: sem achado bloqueante; SUPPLY_CHAIN: nao. Resultado para a seção Security do report: aprovado, govulncheck sem vulnerabilidades, dois achados BAIXO informativos. QA deve aguardar também o resultado do migration-reviewer.
