DELIVERY_ID: DEL-017
TASK_CODE: PROJ-7
AGENT: code-reviewer
STATUS: ok

ESCOPO_REVISADO: diff inteiro da árvore de trabalho contra origin/main (merge-base ec779f8): produção (internal/storage/repositorio.go, internal/http/handler.go), testes alterados (internal/http/apoio_test.go, banco_listagem_test.go, handler_test.go, listagem_test.go; internal/storage/listagem_test.go, plano_listagem_test.go, repositorio_test.go), testes novos lidos por inteiro (internal/storage/plano_livros_test.go, disponibilidade_sem_banco_test.go, disponibilidade_test.go; internal/http/banco_disponibilidade_test.go) e docs-ai/deliveries/INDEX.md. Desenho conferido contra os handoffs 02, 03, 06, 07 e 08.

BLOQUEIOS:
  - nenhum.

SUGESTOES:
  - S1 [docs-ai/deliveries/INDEX.md:32] e [docs-ai/deliveries/DEL-017/report.md:10]: o título "Contagem de exemplares disponíveis por livro" vem da primeira triagem. O escopo fechado (02-pm-triage.md:29) põe a contagem numérica FORA_DE_ESCOPO e entrega um `disponivel` bool. Para o docs-update: retitular o INDEX e o report (ex.: "Disponibilidade (disponivel) em GET /livros e GET /livros/{id}") e levar ao report o escopo e os CAs fechados (02-pm-triage.md:54).
  - S2 [internal/storage/repositorio.go:107-128] e [internal/http/handler.go:225]: os comentários citam a D-23, que ainda não existe em docs-ai/DECISOES.md (a última é a D-22). Isso não impede o merge se o docs-update registrar a D-23 nesta mesma delivery, com o conteúdo de PARA_DOCS_UPDATE do 08-dev-implementation.md. Junto vão a coleção Postman e o mapa de módulos (CA7). O QA precisa conferir que a D-23 existe antes do git-delivery.
  - S3 [internal/storage/plano_livros_test.go:192-193]: o comentário do subteste "noPlano lê SubPlan" fala em "Regressão da DEL-015 ... planos de empréstimo", mas o subteste examina sqlBuscarLivro. Vale alinhar o comentário ao que o subteste faz: garante que o campo ParentRelationship é decodificado. Não muda o comportamento.

CONFIRMADO:
  Correção
  - [internal/storage/repositorio.go:113-116] LivroComDisponibilidade usa o campo nomeado `Livro domain.Livro`, sem embedding, como na A2.
  - [internal/storage/repositorio.go:124-139] As consts sqlListarLivros e sqlBuscarLivro estão exatamente na forma F1 (handoff 06 A1', medida em 07-test-writer.md:8-18): subconsulta escalar `(SELECT 1 FROM emprestimo e WHERE e.livro_id = l.id AND e.devolucao IS NULL LIMIT 1) IS NULL AS disponivel` e FROM externo só de livro. `devolucao IS NULL` é literal. Não há EXISTS, IN, ANY, JOIN, LATERAL, COALESCE, agregação, now() nem vencimento. Não há comentário dentro da string SQL (a palavra EXISTS só aparece no comentário Go, :120).
  - [internal/storage/repositorio.go:143-158] BuscarLivro: `id <= 0` devolve ErrLivroNaoEncontrado sem SQL; pgx.ErrNoRows vira ErrLivroNaoEncontrado; os demais erros passam por traduzirErro. Todos os ramos de erro devolvem o valor zero. Nenhum erro foi ignorado e o ctx é repassado.
  - [internal/storage/repositorio.go:165-174] ListarLivros só troca o SQL e o T da closure (Scan de 3 colunas). O erro de Scan passa por traduzirErro.
  - [internal/http/handler.go:384-391] listarLivros: `proximo` lê `livros[len-1].Livro.ID` sob `temMais && len > 0`, como antes. O handler faz uma única chamada ao repositório, sem N+1 e sem BuscarEmprestimoAbertoPorLivro.
  - [internal/http/handler.go:525] obterLivro responde com novaSaidaLivroLido(got). O caminho de erro (falharErro) não mudou.
  Erros sem vazar SQL
  - Nenhuma mensagem de erro nem log novo. Os erros saem pelas sentinelas existentes (ErrLivroNaoEncontrado, ErrListagemInvalida) e por traduzirErro, e chegam ao HTTP pelo falharErro de sempre. O título, o id e o bool não aparecem em erro nem em log.
  listarPorID e POST /livros intocados
  - O diff de repositorio.go não tem hunk em listarPorID, ListarUsuarios, ListarEmprestimos nem sqlListarEmprestimos.
  - [internal/http/handler.go:220-223] saidaLivro e criarLivro não aparecem no diff. [internal/http/handler_test.go:176-180] passa a exigir POST /livros sem a chave `disponivel`, e [internal/http/banco_disponibilidade_test.go:24] confere POST /livros com exatamente {id, titulo} no banco.
  - lerParametrosListagem não aparece no diff. [internal/http/listagem_test.go:547-560] fixa `?disponivel=` como 400 parametro_invalido, sem chamar o repositório.
  `disponivel` sempre no JSON
  - [internal/http/handler.go:228-232] `Disponivel bool `json:"disponivel"``: sem ponteiro e sem omitempty. O false explícito é coberto por TestContratoSaidaDetalheLivro (trecho literal `"disponivel":false`) e por TestListarLivrosDisponivel (contagem de `"disponivel":false` na página). listaJSON passa a exigir a chave e o tipo bool em todo item.
  Plano de consulta (query-performance não habilitado: enabled_modules = ["data-model", "migration-reviewer"], .pipeline-claude.json:39)
  - O SQL testado é o real: TestPlanoLeituraLivro (internal/storage/plano_livros_test.go:151-160) examina as próprias consts sqlBuscarLivro e sqlListarLivros, não uma cópia.
  - Cobertura do EXPLAIN (VERBOSE, FORMAT JSON): o plano genérico (GENERIC_PLAN, que é o que o cache de prepared statement do pgx usa após 5 execuções) e o custom em 10 casos (detalhe com livro 1, com livro 151 e inexistente; listagem com apos 0/250/450 e limite+1 101/21/2).
  - checarPlanoLivro (:49-109) exige: Index Scan em livro_pkey; um único scan em emprestimo, dentro de SubPlan, com só Limit no caminho até ele; Index Scan ou Index Only Scan em emprestimo_livro_aberto_uq; Index Cond `livro_id = l.id`, sem Filter. Proíbe hashed SubPlan (por Subplan Name e Output), Seq Scan e Bitmap.
  - As asserções provam o CA5: sonda O(página), no máximo 101 por página e 1 no detalhe, sem a leitura O(abertos) do hashed.
  - O plano medido no PG16 do digest do CI (07-test-writer.md:20-33) bate com essas asserções. O dev relata TestPlanoLeituraLivro PASS nos 10 casos, com BIBLIOTECA_TEST_DB_OBRIGATORIO=1 e 0 SKIP (08-dev-implementation.md:24-37).
  - TestLeituraLivroUmaConsulta (internal/storage/disponibilidade_test.go:360-413) confere uma consulta por chamada, com o texto exato das consts, e zero consultas com id 0.
  - TestSQLLeituraLivro (internal/storage/disponibilidade_sem_banco_test.go) bloqueia a volta de EXISTS, JOIN, LATERAL, COALESCE, relógio e parâmetro extra.
  - Não há índice nem migração novos (CA6). migrations/ não aparece no diff.
  Convenções e invariantes
  - Os testes usam a stdlib testing, em tabela. Não há dependência nova: go.mod e go.sum estão fora do diff. internal/domain/ e cmd/ também estão fora do diff.
  - A interface Repositorio continua no consumidor (internal/http/handler.go:40-53), e usar um tipo do storage nela tem precedente (storage.FiltroEmprestimos).
  - Não há goroutine, defer em loop, estado compartilhado nem segredo no diff.
  - Teste alterado junto com a implementação: o dev declara que não editou nenhum _test.go (08-dev-implementation.md:6,18). O 07-test-writer.md:46-48 registra a falha de build sem a implementação, ou seja, os testes vieram antes. Os testes alterados não afrouxam asserção existente: só trocam o tipo (domain.Livro → LivroComDisponibilidade) e acrescentam exigências de `disponivel`.
  Sensores
  - Não rodei: este subagente não tem shell. A evidência é a do dev (08-dev-implementation.md:23-50): build, vet, a suíte completa com banco (0 SKIP), os fuzz FuzzParseID e FuzzParametrosListagem (30 s cada) e `bin/pipeline-claude sensors` com 5 sensores e 0 falhas. O qa-validation deve rodar tudo de novo.

ASSUNCOES:
  - Que os testes não foram editados pelo dev depende da palavra do 08 e da sequência dos handoffs. Os arquivos ainda não foram commitados, então não há histórico git para conferir isso.
  - Que o plano é o mesmo no CI depende da mesma imagem de PG16 (digest citado no 07/08).

ARQUIVOS:
  - nenhum alterado (subagente só de leitura).
  - Revisados: /home/rodrigo/workspace/biblioteca-bairro/internal/storage/repositorio.go, /home/rodrigo/workspace/biblioteca-bairro/internal/http/handler.go, /home/rodrigo/workspace/biblioteca-bairro/internal/storage/plano_livros_test.go, /home/rodrigo/workspace/biblioteca-bairro/internal/storage/disponibilidade_sem_banco_test.go, /home/rodrigo/workspace/biblioteca-bairro/internal/storage/disponibilidade_test.go, /home/rodrigo/workspace/biblioteca-bairro/internal/http/banco_disponibilidade_test.go e os demais _test.go do diff.

RISCOS:
  - O plano é escolha de custo, e TestPlanoLeituraLivro é a barreira contra regressão em versão futura do PG.
  - O contrato aditivo (`disponivel`) quebra cliente com decodificação estrita, e o estado "emprestado" do acervo inteiro passa a sair em lote em GET /livros. As duas coisas vão para o security-review e para a D-23.
  - O CA7 (D-23, Postman, mapa de módulos) ainda está pendente, com o docs-update.

TEXTO_REPORT (seção "## Code Review" de /home/rodrigo/workspace/biblioteca-bairro/docs-ai/deliveries/DEL-017/report.md):
- Resultado: aprovado, sem bloqueios. O diff de produção (repositorio.go e handler.go) segue a F1 do handoff 06 e a A2-A4 do handoff 03. listarPorID e POST /livros (saidaLivro) estão intocados; `disponivel` é bool sem omitempty; os erros saem pelas sentinelas e por traduzirErro, sem SQL. Plano conferido no lugar do query-performance (não habilitado): TestPlanoLeituraLivro examina as consts reais em EXPLAIN VERBOSE, genérico e em 9 casos custom, e exige sonda por livro em emprestimo_livro_aberto_uq sem hashed SubPlan nem Seq Scan. TestLeituraLivroUmaConsulta prova uma consulta por requisição.
- Bloqueios e sugestoes: nenhum bloqueio. Sugestões: S1, retitular o INDEX/report, que ainda dizem "Contagem de exemplares", fora do escopo fechado; S2, registrar a D-23, já citada nos comentários do código, junto com a Postman e o mapa (CA7); S3, corrigir o comentário do subteste "noPlano lê SubPlan" (plano_livros_test.go:192).

SINAIS:
  SECURITY: sim — o diff toca internal/storage/ e internal/http/ (security_surface_paths); contrato aditivo e exposição em lote do estado de empréstimo.
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff.
  MIGRATION: nao — migrations/ fora do diff; o índice existente emprestimo_livro_aberto_uq atende.
  PERFORMANCE: sim — a consulta de GET /livros e GET /livros/{id} mudou; o plano foi conferido aqui (query-performance não habilitado), sem achado.

PROXIMO: security-review
MOTIVO: não há bloqueio. SECURITY: sim, e o diff toca internal/storage/ (security_surface_paths). Os módulos migration-reviewer e query-performance não se aplicam (MIGRATION: nao; query-performance não habilitado), e não há dado por inquilino. Depois vêm qa-validation e docs-update (S1, S2 e CA7).
