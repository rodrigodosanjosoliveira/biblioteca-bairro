DELIVERY_ID: DEL-017
TASK_CODE: PROJ-7
AGENT: qa-validation
STATUS: ok

ESCOPO_VALIDADO: os CA1 a CA8 do 02-pm-triage.md:34-42, conferidos contra o pipeline_diff (base origin/main, merge-base ec779f85f499c67d12e051e978211583d139a05f; 26 arquivos). Produção: internal/storage/repositorio.go e internal/http/handler.go. Testes alterados: internal/http/{apoio,banco_listagem,handler,listagem}_test.go e internal/storage/{listagem,plano_listagem,repositorio}_test.go. Testes novos: internal/storage/{disponibilidade,disponibilidade_sem_banco,plano_livros}_test.go e internal/http/banco_disponibilidade_test.go. Também mudaram docs-ai/deliveries/INDEX.md e docs-ai/deliveries/DEL-017/.

GATES:
  1) pipeline_sensors (todos), executado por este subagente sem BIBLIOTECA_TEST_DATABASE_URL:
     - vet: ok, exit 0, 184 ms, sem saída
     - test: ok, exit 0, 1338 ms. Saída: "pacotes: 8 (8 ok, 0 com falha, 0 sem testes) / testes: 2403 passaram, 0 falharam, 228 pulados"
       skipped: 228, todos com o motivo "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado":
         cmd/biblioteca-bairro 14; internal/http 13 (entre eles TestBancoDisponibilidadeLivros e TestBancoListagemLivros); internal/migrate 149; internal/storage 52 (entre eles os testes de disponibilidade_test.go e TestPlanoLeituraLivro).
       Nesta execução, portanto, a camada de banco ficou pulada (ver o item 2).
     - build: ok, exit 0, 707 ms, sem saída
     - vuln: ok, exit 0, 7795 ms, "No vulnerabilities found."
     - mod_verify: ok, exit 0, 888 ms, "all modules verified"
  2) Suíte com banco: medição da SESSÃO PRINCIPAL, não deste subagente, que não tem shell. Feita em 23/11/2026 na árvore atual, já com a S3.
     - Postgres descartável postgres:16-alpine@sha256:0cefaabd882e42bcb2a2f3dc56d3ce4770560ba62fd7f197f1a2e008fd5731ab, o mesmo do CI (.github/workflows/delivery.yml:24), em modo trust, versão 16.15.
     - Comando: BIBLIOTECA_TEST_DATABASE_URL=postgres://postgres@localhost:55432/postgres?sslmode=disable BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 -p 1 -v ./...
     - Resultado: rc=0; 8 pacotes ok; 3109 PASS, 0 SKIP, 0 FAIL.
     - Passaram TestPlanoLeituraLivro (12,64 s), TestSQLLeituraLivro (listagem e detalhe) e TestLeituraLivroUmaConsulta (6 subtestes).
     - Log: /tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/298abec4-9fea-4cbd-8534-40217ccf0ee0/scratchpad/del017-suite.log
     - Com BIBLIOTECA_TEST_DB_OBRIGATORIO=1, a falta de banco vira FAIL, não SKIP. Por isso os 0 SKIP mostram que a camada de banco rodou.
     - A diferença entre 3109 PASS (-v com banco) e 2631 (2403+228) no sensor é esperada: um teste de banco pulado não executa os subtestes de tabela, que só são contados quando ele roda.
  3) Fuzz, dos alvos que passam pelos handlers de livro, com evidência em 08-dev-implementation.md:38-42:
     - FuzzParseID 30 s: PASS, 52179 execs
     - FuzzParametrosListagem 30 s: PASS, 114020 execs
     A S3 posterior (11-test-writer.md) mudou só um comentário de teste, então não é preciso refazer o fuzz.

COBERTURA_DO_ESCOPO:
  - CA1: GET /livros/{id} devolve 200 com `disponivel` bool, e livro inexistente dá 404. Atendido.
    Diff: handler.go obterLivro → novaSaidaLivroLido; saidaLivroLido.Disponivel é `json:"disponivel"`, sem omitempty.
    Testes:
      - internal/http/listagem_test.go:449 TestContratoSaidaDetalheLivro: true e false presentes no JSON, chaves exatas, uma chamada ao repositório.
      - internal/http/banco_disponibilidade_test.go:15 TestBancoDisponibilidadeLivros: subtestes "detalhe <id>" e "inexistente continua 404".
      - internal/storage/disponibilidade_test.go:190 TestBuscarLivroDisponibilidadeInexistente: livro apagado, id além do último, MaxInt64, 0 e -1.
  - CA2: `disponivel` em cada item de GET /livros, com a D-19 inalterada. Atendido.
    Diff: saidaListaLivros.Itens passa a []saidaLivroLido; proximo = livros[len-1].Livro.ID; lerParametrosListagem e listarPorID fora do diff.
    Testes:
      - internal/http/listagem_test.go:494 TestListarLivrosDisponivel: proximo, uma chamada ao repositório.
      - internal/http/listagem_test.go:549 TestListarLivrosSemFiltroDisponivel: ?disponivel= dá 400.
      - TestListarLivrosContrato (listagem_test.go:255) e TestBancoListagemLivros (banco_listagem_test.go:16).
      - TestBancoDisponibilidadeLivros, com limites 1, 2, 4, 6 e 100 e keyset completo.
  - CA3 (tabela com banco). Atendido. Testes:
    - internal/storage/disponibilidade_test.go:134 TestBuscarLivroDisponibilidade, com os cenários: sem empréstimo, aberto no prazo, devolvido, aberto vencido, vários devolvidos, renovado, renovado e devolvido, devolvido e reaberto, devolvido depois do vencimento.
    - internal/storage/disponibilidade_test.go:155 TestBuscarLivroDisponibilidadeTransicoes: novo → aberto → renovado → devolvido → reaberto.
    - No HTTP, TestBancoDisponibilidadeLivros cobre vencido pelo relógio adiantado e renovado.
  - CA4: página mista, sem vazamento entre itens. Atendido.
    - internal/storage/disponibilidade_test.go:213 TestListarLivrosDisponibilidade: a massa alterna true/false, com guarda em :124-128; todos os limites de 1 a n+1; a página inteira é igual às unitárias.
    - :287 TestListarLivrosDisponibilidadeTodosEmprestados.
    - O caso "mista" de TestListarLivrosDisponivel.
  - CA5: uma consulta por página e por detalhe, usando emprestimo_livro_aberto_uq. Atendido.
    - internal/storage/disponibilidade_test.go:360 TestLeituraLivroUmaConsulta (6 subtestes, texto exato das consts, zero SQL com id 0).
    - internal/storage/plano_livros_test.go:135 TestPlanoLeituraLivro: EXPLAIN VERBOSE em 10 casos, genérico e custom, mais "valores na massa do plano" e "noPlano lê SubPlan".
    - TestSQLLeituraLivro (internal/storage/disponibilidade_sem_banco_test.go:22).
    - Todos passaram com banco, na medição da sessão principal.
  - CA6: nada em migrations/. Atendido: a lista do pipeline_diff (26 arquivos) não tem nenhum caminho em migrations/.
  - CA7: Postman, D-23 em DECISOES.md e mapa de módulos. PENDENTE, a cargo do docs-update, que roda depois do QA pela ordem do fluxo. Não é defeito.
    - Hoje nenhum dos três aparece no diff.
    - O texto está pronto em 08-dev-implementation.md:57-71 (PARA_DOCS_UPDATE).
    - A D-23 precisa incluir o risco BAIXO e a condição de reavaliação de 10-security-review.md:17.
    - Os comentários em repositorio.go e handler.go já citam a D-23 (S2 de 09-code-reviewer.md:13). O git-delivery deve confirmar que a D-23 existe.
  - CA8: build, vet, test com banco e sensors verdes. Atendido.
    - Os sensores rodaram aqui (item 1) e os 5 estão ok.
    - go test com banco: 0 SKIP e 0 FAIL, na medição da sessão principal (item 2).

SCOPE_CREEP (FORA_DE_ESCOPO de 02-pm-triage.md:26-32): nenhum.
  - Sem tabela exemplar nem coluna de quantidade, e sem migração nem índice.
  - Sem contagem numérica.
  - Sem filtro nem ordenação por disponibilidade: ?disponivel= continua dando 400, fixado em teste.
  - Sem mudança em multa, renovação, R4 ou paginação. O diff de produção se restringe a BuscarLivro, ListarLivros, ao DTO de leitura e aos dois handlers.
  - POST /livros continua {id, titulo}: saidaLivro fica fora do diff, e banco_disponibilidade_test.go:24 exige exatamente as chaves id e titulo.

BORDAS_CONFERIDAS:
  - Livro com vários empréstimos não duplica item nem consome limite a mais (LIMIT conta livros), em TestListarLivrosDisponibilidade.
  - Página vazia, em TestLeituraLivroUmaConsulta.
  - id 0, negativo, MaxInt64 e livro apagado, em TestBuscarLivroDisponibilidadeInexistente.
  - Valor independente do relógio do handler: os subtestes "relógio zerado não importa".
  - Devolução refletida nas duas rotas: o subteste "devolver torna disponível nas duas rotas".
  - Não testado, por decisão: leitura concorrente com abertura ou devolução. É sem lock, com coerência por statement, e foi aceita na ASSUNCAO do 02-pm-triage.md:53.

S3 (11-test-writer.md): conferida em internal/storage/plano_livros_test.go:192-195. Só o comentário mudou e o código do subteste (:196-207) está como estava. O subteste passou com banco na medição da sessão principal.

ASSUNCOES:
  - A prova da camada de banco vem da medição da sessão principal (item 2 de GATES), não de execução deste subagente, que não tem shell e cujo sensor rodou sem banco (228 pulados). A medição é desta data, da árvore atual, com BIBLIOTECA_TEST_DB_OBRIGATORIO=1 e a imagem do CI. Por isso o RESULTADO é ok, e não parcial.

ARQUIVOS:
  - nenhum alterado (só leitura).
  - Lidos:
    - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/repositorio.go (diff)
    - /home/rodrigo/workspace/biblioteca-bairro/internal/http/handler.go (diff)
    - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/disponibilidade_test.go
    - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/plano_livros_test.go:140-208
    - /home/rodrigo/workspace/biblioteca-bairro/internal/http/banco_disponibilidade_test.go
    - /home/rodrigo/workspace/biblioteca-bairro/internal/http/listagem_test.go:445-560

RISCOS:
  - O CA7 só fecha no docs-update. Sem a D-23, os comentários do código apontam para uma decisão inexistente.
  - O sensor `test` sem a variável de banco pula 228 testes. No CI, a camada de banco depende do Postgres do workflow.
  - O plano é escolha de custo do PG16, e TestPlanoLeituraLivro é a barreira contra mudança em versão futura.
  - Risco aceito BAIXO, do security-review: GET /livros expõe em lote o estado "emprestado" do acervo. Fica registrado na D-23.

TEXTO_REPORT (seção QA de /home/rodrigo/workspace/biblioteca-bairro/docs-ai/deliveries/DEL-017/report.md):
- Resultado: ok. Os critérios CA1 a CA6 e CA8 estão atendidos, e cada um tem teste no diff. O CA7 (D-23, coleção Postman e mapa de módulos) está pendente e é tarefa do docs-update, que vem depois desta etapa. Não houve scope creep. Sensores: vet, test, build, vuln e mod_verify ok. O `test` rodou sem banco e pulou 228 testes, todos por falta da BIBLIOTECA_TEST_DATABASE_URL. A camada de banco foi provada pela suíte com banco que a sessão principal rodou em 23/11/2026: postgres:16-alpine com o digest do CI, BIBLIOTECA_TEST_DB_OBRIGATORIO=1, rc=0, 3109 PASS, 0 SKIP, 0 FAIL. Fuzz de FuzzParseID e FuzzParametrosListagem, 30 s cada: PASS.
- Cenarios executados: detalhe com disponivel true e false (o false presente no JSON), 404 de livro inexistente, id 0, negativo e MaxInt64. Disponibilidade sem empréstimo, com empréstimo aberto no prazo, aberto vencido, devolvido, devolvido depois do vencimento, renovado, renovado e devolvido, e devolvido e reaberto, mais a transição abrir → renovar → devolver → reabrir. Página mista alternada com limite de 1 a n+1, página inteira igual às unitárias, página toda emprestada e depois toda devolvida, página vazia. Keyset e proximo inalterados, ?disponivel= devolvendo 400 e POST /livros continuando {id, titulo}. Uma consulta SQL por página e por detalhe, nenhuma com id 0. EXPLAIN genérico e custom em 10 casos com sonda em emprestimo_livro_aberto_uq, sem hashed SubPlan nem Seq Scan. Fluxo HTTP com banco: emprestar, devolver, renovar e vencer pelo relógio, conferindo as duas rotas.

SINAIS:
  SECURITY: sim — já revisado (10-security-review.md): sem achado bloqueante, um BAIXO informativo para a D-23.
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff; mod_verify e vuln ok.
  MIGRATION: nao — migrations/ fora do diff (CA6).
  PERFORMANCE: sim — plano conferido por TestPlanoLeituraLivro (PASS com banco); sem achado.

PROXIMO: docs-update
MOTIVO: sem achado bloqueante. release-ops não está em enabled_modules (["data-model", "migration-reviewer"]). O docs-update fecha o CA7: D-23 com o risco BAIXO do security-review, coleção Postman e mapa de módulos. Também aplica a S1 (retitular INDEX e report) e leva ao report o escopo e os CAs fechados.
