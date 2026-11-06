DELIVERY_ID: DEL-011
TASK_CODE: DEL-011
AGENT: code-reviewer
STATUS: ok

BLOQUEIOS:
  - nenhum.

SUGESTOES:
  - [internal/storage/repositorio.go:238-242] O CAS usa só `renovacoes` como versão e não confere o vencimento anterior. Isso é correto porque hoje o vencimento só muda junto com o contador (INSERT e RegistrarRenovacao). Vale registrar esse invariante no comentário de RegistrarRenovacao ou na D-17 ("toda escrita de vencimento incrementa renovacoes"), para que um escritor futuro do vencimento não quebre o CAS sem perceber.
  - [migrations/0002_renovacao_emprestimo.up.sql:6] O literal `2` do CHECK duplica domain.LimiteRenovacoes (internal/domain/emprestimo.go:14). A duplicação é intencional (segunda barreira, já aceita no 02), mas convém citar no comentário da constante que mudar o limite exige migração nova. Fica para o docs-update (D-17).
  - [internal/storage/repositorio.go:235] `e.Renovacoes() > domain.LimiteRenovacoes` não é alcançável por valor vindo do domínio, porque Renovar e ReidratarEmprestimo já limitam o contador. Pode ficar como defesa (pedida no 03), mas é código morto para os testes (mutante equivalente S12, como o test-writer registrou).
  - [internal/http/handler.go:352-380] `renovar` repete passo a passo o esqueleto de `devolver` (parse, corpo vazio, agora, timeout, busca, mutação, registro, resposta). Hoje são dois casos e a repetição é aceitável. Se surgir um terceiro, vale extrair um auxiliar que receba a mutação e o método de registro.

CONFIRMADO:
  - Diff inteiro lido via pipeline_diff (base origin/main, merge-base 1ad40d3): produção em internal/domain/emprestimo.go, internal/storage/repositorio.go, internal/http/handler.go e internal/http/erros.go. Lidos com Read os arquivos novos migrations/0002_renovacao_emprestimo.{up,down}.sql, migrations/renovacao_test.go, internal/domain/renovar_test.go, internal/storage/renovacao_test.go, internal/http/renovacao_test.go e internal/http/banco_renovacao_test.go. Também li o diff dos testes alterados (emprestimo_multa_test, emprestimo_test, reidratar_test, repositorio_test, sem_banco_test, esquema_test, apoio_test, handler_test, erros_interno_test).
  - Erros: nenhum `_ =` sobre retorno de erro. Em repositorio.go:243-262, o erro do UPDATE só cai no diagnóstico quando é ErrEmprestimoNaoEncontrado; os demais voltam direto. A falha do SELECT de diagnóstico passa por traduzirErro (l. 257-258). lerEmprestimo embrulha falha de reidratação com %w (l. 305-307).
  - context.Context: RegistrarRenovacao recebe ctx e o usa nos dois QueryRow (l. 238, 251). O handler deriva ctx de r.Context() com context.WithTimeout(a.timeout) e chama defer cancel() (handler.go:363-364), sem defer em loop. Não há goroutine nova em produção.
  - Entrada externa validada antes do uso: parseID e exigirCorpoVazio vêm antes de qualquer acesso ao repositório (handler.go:353-361). A data não vem do cliente; vale o relógio injetado `a.agora()`. A rota está atrás de auth (handler.go:133), e o teste a inclui em rotasProtegidas (apoio_test.go).
  - Status e mensagens: vencido e limite dão 409 com mensagem fixa (erros.go:67-68). classificar mapeia as sentinelas com errors.Is (erros.go:113-116). ErrRenovacoesInvalidas entra em errosDeBug (erros.go:135) e vira 500 sem detalhe interno. Os códigos existentes foram reaproveitados para não encontrado, já devolvido e divergente, como pede o NAO_FAZER do 02.
  - Interface no consumidor: Repositorio continua em internal/http (handler.go:44) e ganhou RegistrarRenovacao. O storage expõe tipo concreto.
  - Domínio: Renovar (emprestimo.go:182-202) segue a ordem do 02 (início zero, aberto, ref zero, vencido por meiaNoiteUTC, limite) e não altera nada em erro. O vencimento novo é time.Date(a, m, d+PrazoRenovacaoDias, 12, ...) em locSaoPaulo, com base no vencimento atual. ReidratarEmprestimo valida o contador por último (emprestimo.go:139-141).
  - SQL: UPDATE parametrizado ($1..$5) sem incremento nem aritmética de data (repositorio.go:239-242). O contador anterior é calculado em Go (e.Renovacoes()-1). O diagnóstico é por PK.
  - Migração: um único ALTER TABLE com ADD COLUMN NOT NULL DEFAULT 0 e CHECK nomeado. O down remove a constraint antes da coluna, sem IF EXISTS nem CASCADE. Não há BEGIN, COMMIT, SET nem concurrently.
  - Testes alterados no mesmo diff: as mudanças nos _test.go existentes são o parâmetro novo de ReidratarEmprestimo (sempre 0 nos casos antigos) e casos acrescentados. Nenhuma asserção antiga foi enfraquecida. Exceção conhecida: a troca em handler_test.go do caso "subrota desconhecida" de /livros/1/renovacao para /livros/1/inexistente, prevista no 02. A autoria é do test-writer (04), não do dev (o 05 confirma).
  - Segredo em código: nenhum.
  - .pipeline-claude.json:39: enabled_modules = ["data-model", "migration-reviewer"].

ASSUNCOES:
  - Não rodei build, vet nem test (revisor só leitura). Considerei a evidência de sensores, da suíte com banco (0 SKIP), de race e de fuzz registrada no 05-dev-implementation.
  - A igualdade `inicio = $4` é exata porque o início sempre vem de leitura do banco (precisão de µs). Esse é o mesmo pressuposto documentado em RegistrarDevolucao (repositorio.go:182-184), não um pressuposto novo.

ARQUIVOS:
  - nenhum (revisor sem escrita).

RISCOS:
  - O sensor `test` do pipeline roda sem banco (200 pulados). CAS, CHECK e migração só foram provados na execução manual com banco do 05. O qa-validation precisa repetir com BIBLIOTECA_TEST_DATABASE_URL e BIBLIOTECA_TEST_DB_OBRIGATORIO=1.
  - Ordem de deploy: o binário novo lê `renovacoes` em toda leitura de empréstimo. Sem a 0002 aplicada, todas as rotas de empréstimo dão 500 (42703). O migration-reviewer deve confirmar o runbook.
  - Contrato da API: campo novo `renovacoes` em todas as respostas de empréstimo e dois códigos 409 novos. É aditivo, mas cliente com decodificação estrita quebra.

SINAIS:
  SECURITY: sim — rota autenticada nova; diff em internal/storage/ e migrations/ (security_surface_paths).
  SUPPLY_CHAIN: nao — go.mod e go.sum fora do diff.
  MIGRATION: sim — migrations/0002_renovacao_emprestimo.{up,down}.sql (coluna NOT NULL DEFAULT e CHECK em emprestimo).
  PERFORMANCE: nao — UPDATE e SELECT de diagnóstico por PK; busca pelo índice parcial existente.

PROXIMO: migration-reviewer
MOTIVO: sem BLOQUEIO. MIGRATION: sim e migration-reviewer está em enabled_modules (.pipeline-claude.json:39). Depois dele, security-review (SECURITY: sim, diff em security_surface_paths) e então qa-validation.
