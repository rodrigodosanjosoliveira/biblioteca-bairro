DELIVERY_ID: DEL-015
TASK_CODE: DEL-015
AGENT: security-review
STATUS: ok

RESULTADO: aprovado. Nenhum achado CRITICO ou ALTO. Um MEDIO já aceito (U2 da D-20) sai mais forte com a rota nova e precisa entrar na D-21. Os outros dois são BAIXO e informativos.

SUPERFICIE_REVISADA:
  - internal/http/handler.go (pipeline_diff): a rota `GET /usuarios/{id}/emprestimos` (linha 148), `listarEmprestimos`, `listarEmprestimosDoUsuario`, `responderPaginaEmprestimos` (linhas 428-497), `lerParametrosListagem`, `lerParametrosEmprestimos`, `lerQueryListagem` (linhas 650-736) e `parseID` (linha 641).
  - internal/storage/repositorio.go (pipeline_diff): `Situacao`, `FiltroEmprestimos.valido`, `ListarEmprestimos`, `sqlListarEmprestimos` e `listarPorID`.
  - migrations/0003_indices_listagem_emprestimo.up.sql e .down.sql (lidos direto, não rastreados).
  - internal/http/middleware/middleware.go: o que o `Log` e o `Anotar` gravam (linhas 60-62, 101-102, 155-184).
  - docs-ai/DECISOES.md D-20 (linhas 797-824). Handoffs 01 (R4, R6), 02 (U2, NAO_FAZER) e 07.
  - Sensor vuln (govulncheck).

GOVULNCHECK (pipeline_sensors names=["vuln"]):
  ok: true, exit_code 0, duration 4457 ms
  output: "No vulnerabilities found."

ACHADOS:
  - MEDIO (risco aceito, já existia, agora amplificado), R4 do triage. Antes, o histórico de empréstimos de uma pessoa só saía percorrendo todo o `GET /emprestimos` (cerca de N/100 requisições). Com `GET /usuarios/{id}/emprestimos` e `?usuario_id=`, ele sai numa requisição por página, já filtrado. Também ficou trivial pegar "quem está com o livro X" (`?livro_id=&situacao=aberto`) e "quem está em atraso" (`?situacao=vencido`). Isso continua dentro do modelo de token único e operador único (D-15/D-17/D-19, U2 da D-20), porque o mesmo token já dava acesso a tudo. Não bloqueia. O docs-update precisa ampliar na D-21 a condição de bloqueio da D-20: com vários tokens ou login de usuário final, `GET /usuarios/{id}/emprestimos` e os filtros `usuario_id`/`livro_id`/`situacao` de `GET /emprestimos` passam a exigir autorização por recurso antes da mudança. Na rota aninhada, isso significa o dono do `{id}` ou o operador. Rate limit e log de volume por cliente continuam ausentes, como na D-20.
  - BAIXO (informativo), R6. A rota aninhada responde 404 `usuario_inexistente` para id inexistente. `?usuario_id=` responde 200 com lista vazia. Isso é um oráculo de existência de id, e `GET /usuarios` e `GET /usuarios/{id}` autenticados já revelam o mesmo. Mesmo risco BAIXO da D-20.
  - BAIXO (informativo). A rota aninhada faz duas consultas por página (`BuscarUsuario` + `ListarEmprestimos`), dentro do mesmo `context.WithTimeout`. Isso só amplifica carga para quem tem o token, e a amplificação é limitada (limite <= 100, prazo único). Não pede ação.

CONFIRMADO:
  - Autenticação: a rota nova está atrás do mesmo `auth` das outras (handler.go:148), e `rotasProtegidas()` a inclui nos testes de auth e timeout (handoff 07, S4). `GET /livros/{id}/emprestimos` não existe (U2), e há teste de 404 para ela.
  - Entrada externa, validada antes do uso:
    - `url.ParseQuery` tem o erro tratado. Chave repetida dá 400 e chave desconhecida também.
    - `situacao` é comparada de forma exata com aberto|devolvido|vencido.
    - `usuario_id` e `livro_id` passam por `parseID` (`ParseInt` base 10, > 0) e chegam como int64.
    - `usuario_id` é recusado na rota aninhada, então o id do path não pode ser sobrescrito.
    - Em caso de falha, o filtro sai zerado. A resposta é a mensagem fixa `parametro_invalido`, sem eco do valor.
    - Defesa em profundidade: `FiltroEmprestimos.valido()` recusa id negativo, situação fora do enum e vencido sem Ref antes de qualquer SQL, e o handler mapeia isso para 500 sem detalhe.
  - SQL: `sqlListarEmprestimos` junta só literais constantes. O único fragmento que muda é o placeholder `"$" + strconv.Itoa(len(args)+2)`, que vem de um contador interno e não da entrada. `f.UsuarioID`, `f.LivroID` e `vencimentoParaDate(f.Ref)` vão só em `args`, e `listarPorID` repassa tudo por `db.Query(ctx, sql, args...)`. Não há Sprintf, concatenação de valor nem identificador dinâmico. Os textos possíveis são 16, finitos e estáveis. O `fmt` importado não aparece no caminho novo.
  - Migração 0003:
    - O up tem 3 `CREATE INDEX`, todos com nome e coluna fixos. O predicado parcial (`devolucao IS NULL`) é imutável. Não há GRANT, ROLE, função, `SECURITY DEFINER`, extensão nem dado.
    - O down tem 3 `DROP INDEX`.
    - Não altera privilégio nem expõe coluna. O lock SHARE e a exceção da U3 ficam com o migration-reviewer.
  - Log e dado pessoal: o middleware `Log` grava metodo, `r.Pattern`, status, duracao_ms, request_id e o código de erro anotado. Não grava URL, query, headers nem corpo (middleware.go:155-157). Para a rota nova, a rota sai como o padrão `GET /usuarios/{id}/emprestimos`, sem o id concreto. O diff não traz nenhum `slog`/`Anotar`/`log` novo, então os valores de `situacao`, `usuario_id` e `livro_id` não chegam ao log.
  - Erros: não há código de erro novo. Os 404 e 400 usam mensagem fixa. Os erros de repositório passam por `falharErro`/`traduzirErro`, sem texto de query, caminho ou stack.
  - Segredo e criptografia: nada novo no diff.
  - CI: nenhum workflow no diff. go.mod e go.sum ficam fora do diff, então não há supply chain.

ASSUNCOES:
  - Não reexecutei a suíte. A cobertura de auth e de fuzz do parser foi confirmada pelos handoffs 05, 06 e 07 e pela leitura das assinaturas, sem rodar.
  - Considerei que `falharErro` mapeia `ErrListagemInvalida` para 500 `bug_interno` sem detalhe, como na D-19/D-20. O mapeamento é anterior e não está no diff.

ARQUIVOS:
  - nenhum editado (revisor só lê).

RISCOS:
  - Se a D-21 não ampliar a condição de bloqueio da D-20 (achado MEDIO), uma futura delivery de vários tokens pode acabar cobrindo só as três rotas antigas. Item obrigatório para o docs-update.
  - Continuam sem rate limit e sem log de volume por cliente (herdado da D-20).

SINAIS:
  SECURITY: sim — revisado e aprovado. MEDIO aceito (R4, U2 da D-20 ampliada para a rota aninhada e os filtros) e dois BAIXO informativos. govulncheck sem vulnerabilidades.
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff.
  MIGRATION: sim — 0003 sem impacto de segurança. Lock e U3 ficam com o migration-reviewer, em paralelo.
  PERFORMANCE: sim — sem implicação de segurança além da carga extra BAIXA da rota aninhada.

PROXIMO: qa-validation
MOTIVO: Não há achado CRITICO nem ALTO, e SUPPLY_CHAIN está em nao. O qa-validation entra depois que o migration-reviewer (em paralelo) também aprovar. O docs-update deve registrar na D-21 o MEDIO (condição de bloqueio ampliada) e os dois BAIXO.
