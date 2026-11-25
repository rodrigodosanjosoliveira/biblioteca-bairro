DELIVERY_ID: DEL-017
TASK_CODE: PROJ-7
AGENT: security-review
STATUS: ok

SUPERFICIE_REVISADA (pelo pipeline_diff contra origin/main, merge-base ec779f8; produção = 2 arquivos):
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/repositorio.go (LivroComDisponibilidade, consts sqlListarLivros e sqlBuscarLivro, BuscarLivro, ListarLivros)
  - /home/rodrigo/workspace/biblioteca-bairro/internal/http/handler.go (interface Repositorio, saidaLivroLido, novaSaidaLivroLido, listarLivros, obterLivro)
  - contexto lido fora do diff: registro de rotas e auth em handler.go:121-154; D-15 (H7, token Bearer), D-19 (riscos aceitos, :722-736), D-21 (riscos aceitos, :980-995) em /home/rodrigo/workspace/biblioteca-bairro/docs-ai/DECISOES.md; handoff 09-code-reviewer.md.
  - go.mod/go.sum: fora do diff (pipeline_diff com paths go.mod, go.sum não lista nenhum dos dois).

GOVULNCHECK (pipeline_sensors names=["vuln"], resultado real):
  ok: true, exit_code: 0, duration_ms: 4350
  output: "No vulnerabilities found."

ACHADOS:
  - BAIXO, informativo [internal/http/handler.go:143-144, :228-232]: GET /livros passa a revelar em lote o estado "emprestado" (bool) de todo o acervo, e GET /livros/{id} o de um livro. A saída é só {id, titulo, disponivel}: sem usuario_id, datas, vencimento nem multa. As duas rotas continuam atrás do mesmo auth(...) (handler.go:143-144). Com o token, a informação já era obtida, e com mais detalhe, por GET /emprestimos?situacao=aberto e ?livro_id=&situacao=aberto (D-21, que expõe também quem está com o livro), e GET /livros/{id}/emprestimo. Portanto não há dado novo nem enumeração nova para quem tem o token; é um subconjunto agregado do que o modelo de token único e operador único (D-15, D-19, D-21) já aceita. Recomendação para o docs-update: a D-23 registrar este risco aceito e estender a condição de bloqueio da D-21: se surgirem vários tokens ou login de usuário final (ex.: leitor com credencial própria e escopo só de catálogo), `disponivel` em GET /livros deve ser reavaliado, pois um perfil de catálogo passaria a ver estado de circulação. Não bloqueia.
  - Nenhum CRITICO, ALTO ou MEDIO.

CONFIRMADO:
  SQL (D-21)
  - [internal/storage/repositorio.go:124-139] sqlListarLivros e sqlBuscarLivro são const de texto fixo, sem fmt.Sprintf, concatenação nem montagem dinâmica. Placeholders só $1/$2 (ListarLivros: apos, limite) e $1 (BuscarLivro: id), os mesmos de antes; nenhum parâmetro novo. `devolucao IS NULL` literal. Chamadas: r.db.QueryRow(ctx, sqlBuscarLivro, id) e listarPorID(ctx, r.db, sqlListarLivros, apos, limite, ...), ambos parametrizados pelo pgx.
  Entrada
  - Nenhum parâmetro de query/rota novo: lerParametrosListagem fora do diff; BuscarLivro mantém `id <= 0` → ErrLivroNaoEncontrado antes de SQL; ListarLivros mantém a validação de apos/limite em listarPorID (fora do diff). `?disponivel=` segue 400 parametro_invalido (internal/http/listagem_test.go:547-560, conforme 09-code-reviewer).
  Erros e logs (invariante 3.3, D-13)
  - Nenhuma mensagem de erro nem chamada de log nova no diff. Erros saem por ErrLivroNaoEncontrado e traduzirErro (storage) e por falharErro (HTTP), caminhos inalterados; SQL, PgError, título e o bool não vão para resposta nem log.
  Autenticação e autorização
  - Nenhuma rota nova nem alterada no registro (handler.go:121-154 fora do diff); GET /livros e GET /livros/{id} continuam com auth(...). POST /livros mantém saidaLivro (sem `disponivel`).
  Segredos e CI
  - Nenhum segredo, credencial ou token no diff. Nenhum workflow de CI no diff.
  Criptografia
  - Não se aplica (nada de cripto no diff).
  Dependências
  - go.mod e go.sum intocados; govulncheck sem achados.

ASSUNCOES:
  - A avaliação de exposição pressupõe o modelo de token único e operador único da D-15/D-19/D-21, vigente hoje.

ARQUIVOS:
  - nenhum alterado (subagente só de leitura).

RISCOS:
  - Risco aceito (BAIXO) a registrar na D-23 pelo docs-update, com a condição de reavaliação descrita em ACHADOS.
  - Rate limit e log de volume por cliente seguem ausentes (já registrado na D-21); não muda com esta delivery.

TEXTO_REPORT (seção "## Security" de /home/rodrigo/workspace/biblioteca-bairro/docs-ai/deliveries/DEL-017/report.md):
- Resultado: aprovado, sem achado bloqueante. govulncheck (sensor vuln): "No vulnerabilities found." Superfície: internal/storage/repositorio.go e internal/http/handler.go. SQL em const de texto fixo, só com os placeholders já existentes ($1/$2 na listagem, $1 no detalhe), sem concatenação (D-21). Nenhum erro ou log novo; erros seguem por sentinelas, traduzirErro e falharErro, sem SQL nem dado (invariante 3.3, D-13). Nenhum parâmetro nem rota nova; `?disponivel=` segue 400; GET /livros e GET /livros/{id} seguem atrás do auth. go.mod/go.sum intocados. Achado BAIXO, informativo: GET /livros expõe em lote o estado emprestado do acervo (bool, sem usuario_id nem datas), subconjunto do que GET /emprestimos?situacao=aberto já revela a quem tem o token; risco aceito no modelo de token único (D-15, D-19, D-21), a registrar na D-23 com reavaliação se surgirem vários tokens ou login de usuário final.

SINAIS:
  SECURITY: sim — revisado; sem CRITICO/ALTO/MEDIO; um BAIXO informativo.
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff.
  MIGRATION: nao — migrations/ fora do diff.
  PERFORMANCE: sim — já conferido pelo code-reviewer; nada de segurança relacionado.

PROXIMO: qa-validation
MOTIVO: sem achado bloqueante e SUPPLY_CHAIN: nao. O docs-update deve registrar na D-23 o risco BAIXO e a condição de reavaliação.
