DELIVERY_ID: DEL-015
TASK_CODE: DEL-015
AGENT: qa-validation
STATUS: ok

RESULTADO: ok. Cada item de código do ESCOPO tem teste, e os testes passam sem banco (sensor) e com banco (execução da sessão principal, 0 skip e 0 fail). Nada do FORA_DE_ESCOPO foi implementado. A marcação merged da DEL-014 está restrita à linha de status. O item de documentação (D-21, mapa e runbook) é trabalho do docs-update, que vem a seguir. O sensor desta etapa pulou 220 testes, todos da camada de banco e pelo mesmo motivo (DSN ausente). A camada de banco foi provada pela execução da sessão principal; a justificativa de "ok" em vez de "parcial" está em ASSUNCOES, como na DEL-014.

GATES:
  - pipeline_sensors (todos), rodado por este agente: ok=true
      vet: ok, exit 0, 165 ms, saída vazia
      test: ok, exit 0, 199 ms. Saída: "pacotes: 8 (8 ok, 0 com falha, 0 sem testes) / testes: 2386 passaram, 0 falharam, 220 pulados" (skipped=220, resumo truncado). Todos os pulados que aparecem no resumo têm o motivo "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado". Exemplos: TestFecharEsperaOBackendCanceladoSair e TestMigrateAceitaParametrosDePoolNaDSN/* (cmd/biblioteca-bairro); TestControleCatalogo, TestControleDown e TestNovoPrazoNaoPositivoOuSubMilissegundoCaiNoPadrao/* (internal/migrate); TestMigracaoUpDownUp e TestEsquemaRestricoes (internal/storage); TestBancoListagemEm... (internal/http, o resumo corta aqui). São todos da camada de banco (repositório, migração, consulta).
      build: ok, exit 0, 481 ms, saída vazia
      vuln: ok, exit 0, saída "No vulnerabilities found."
      mod_verify: ok, exit 0, saída "all modules verified"
  - Suíte com banco, EXECUTADA PELA SESSÃO PRINCIPAL e não por este agente, em 2026-11-09T00:54:52Z. Ambiente: Postgres 16 descartável, modo trust, digest do CI, BIBLIOTECA_TEST_DB_OBRIGATORIO=1. Comando: `go test -count=1 -p 1 -json ./...`, depois de go build e go vet limpos. Resultado informado: exit 0, 3019 pass, 0 fail, 0 skip, 8 pacotes. Conferi o JSON (/tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/qa015-test.json) com Grep:
      - `"Action":"(fail|skip)"`: 0 ocorrências.
      - Pass no nível de pacote nos 8 pacotes: cmd/biblioteca-bairro (l. 556), internal/config (1032), internal/domain (3840), internal/http (8512), internal/http/middleware (8740), internal/migrate (10100), internal/storage (12056, 11,696 s) e migrations (12108).
      - Testes desta delivery com "Action":"pass" (linha do JSON):
          Filtros, storage com banco: TestListarEmprestimosFiltroCombinacoes (10597)
          Fronteira de vencido: TestListarEmprestimosVencidoFronteira (10781), com subtestes "sessão <tz>/..." a partir da l. 10603
          EXPLAIN: TestPlanoListarEmprestimos (11597, Elapsed 8.37)
          Migração 0003: TestUp0003 (12081), TestDown0003 (12085), TestMigracaoUpDownUp (10109), TestMigracaoUpDownUp/down_só_da_0003 (10107), TestEsquemaRestricoes (10213)
          Rota aninhada e filtros pela API com banco: TestBancoListagemEmprestimosFiltros (4157), com subtestes aberto, devolvido, vencido_às_23:59:59_do_dia_do_vencimento, vencido_às_00:00_do_dia_seguinte, usuario_id, livro_id, usuario_id_e_aberto, usuario_id,_livro_id_e_devolvido, usuario_id_inexistente_é_200_vazio e livro_id_inexistente_é_200_vazio. Também aninhada, aninhada_com_aberto, aninhada_com_livro_id, aninhada_com_vencido, aninhada_de_usuário_sem_empréstimos e vencido_bem_depois, cada um com limite_1/_2/_100. E ainda aninhada_de_usuário_inexistente_é_404_em_toda_página (4143), aninhada_com_usuario_id_na_query_é_400 (4147), situacao_vazia_é_400 (4151) e abertos_não_vencidos_saem_com_multa_zero (4155).
          Sem banco, no mesmo JSON: TestSQLListarEmprestimosCombinacoes (10281), TestSQLListarEmprestimosSemFiltroInalterado (10297), TestSQLListarEmprestimosHojeEmSaoPaulo (10325), TestVencimentoParaDateFronteiraVencido (10345), TestListarEmprestimosFiltroArgs/Invalido/Aceito/ErroSemDados (10417-10501). Na camada HTTP: TestListarEmprestimosFiltrosParametros (6065), TestEmprestimosDoUsuarioParametros/ParametroID/OrdemDeErros/MapaDeErros/Prazo/Contrato (6253-6421), TestListagemFiltradaInstanteUnico (6445), TestFiltrosEmprestimosAuthAntesDeTudo (6477), TestLogFiltrosEmprestimosSemValores (6505), TestFiltrosEmprestimosNaoValemEmOutrasListagens (6541), TestListagensNovasParametros (7337), TestMuxNotFoundEMethodNotAllowed (5745), FuzzParametrosEmprestimos (8421, seeds) e FuzzParametrosListagemNovasRotas (8509, seeds).
  - Fuzz de 30 s por alvo, evidência do handoff 06 (dev-implementation), não repetida aqui:
      `go test -run='^$' -fuzz='^FuzzParametrosEmprestimos$' -fuzztime=30s ./internal/http`: "execs: 314292 ... PASS / ok ... 30.154s"
      `-fuzz='^FuzzParametrosListagemNovasRotas$'`: "execs: 272883 ... PASS / ok ... 30.137s"
      `-fuzz='^FuzzParametrosListagem$'` (o parser antigo passou a delegar a lerQueryListagem): "execs: 198209 ... PASS / ok ... 31.099s"
    Os três alvos tocados pela delivery têm a saída.

COBERTURA_DO_ESCOPO:
  - Filtros em GET /emprestimos (situacao, usuario_id e livro_id, combináveis, no contrato D-19):
      Parsing: internal/http/listagem_filtro_emprestimos_test.go, com TestListarEmprestimosFiltrosParametros (:149) sobre casosQueryComuns (:98-142) e casos de usuario_id (:151-160). Os casos cobrem: os 3 valores; vazio; sem valor; maiúscula; capitalizado; acento; lista; repetida (igual e diferente); NUL; espaço; `+`; `;`; escape inválido; ids 0, negativo, overflow, hex, fracionário e lista; chave desconhecida junto de filtro válido; limite/apos inválidos junto de filtro válido. Também confere que não há eco do valor e que o repositório não é chamado em 400. Fuzz: FuzzParametrosEmprestimos (:577), com oráculo independente (:30).
      Contrato e instante único: TestListagemFiltradaInstanteUnico (:443), que exige Ref = instante do relógio e a mesma multa.
      Mudança intencional de 400 → 200: TestListagensNovasParametros em internal/http/listagem_usuarios_emprestimos_test.go.
      Com banco: TestBancoListagemEmprestimosFiltros (internal/http/banco_listagem_filtro_test.go:46). TestListarEmprestimosFiltroCombinacoes (internal/storage/listagem_filtro_test.go:182) cobre as 16 combinações contra o oráculo do domínio, limites 1/2/3/100, "apos combina com o filtro" (:258) e os vazios (:268-272).
      Código conferido: handler.go lerParametrosEmprestimos/lerQueryListagem (:665-736). `len(vs) != 1` → falso (:713), a comparação de situacao é exata e o filtro sai zerado em erro.
  - GET /usuarios/{id}/emprestimos (autenticada, mesmo contrato; situacao, livro_id; usuario_id → 400; 404 usuario_inexistente em toda página):
      TestEmprestimosDoUsuarioParametros (:218), ParametroID (:244), OrdemDeErros (:281, id_invalido → parametro_invalido → usuario_inexistente), MapaDeErros (:321), Prazo (:371), Contrato (:389) e TestFiltrosEmprestimosAuthAntesDeTudo (:498). A rota está em rotasProtegidas() (apoio_test.go), então TestAuthRotasProtegidas e TestTimeoutPorOperacao também a cobrem.
      Com banco: os subtestes aninhada* e aninhada_de_usuário_inexistente_é_404_em_toda_página.
      Código conferido: handler.go:148, atrás de auth(...); listarEmprestimosDoUsuario em :446-470.
  - Consultas keyset filtradas no storage (uma por página, sem N+1, OFFSET ou COUNT):
      internal/storage/listagem_filtro_sem_banco_test.go: TestSQLListarEmprestimosCombinacoes (:119, 16 textos e args exatos, nenhum valor no texto), SemFiltroInalterado (:170, igual byte a byte ao SQL anterior), HojeEmSaoPaulo (:198), TestListarEmprestimosFiltroArgs (:266, [apos, limite+1, extras...]), FiltroInvalido (:301, ErrListagemInvalida sem SQL), FiltroAceito (:340), FiltroErroSemDados (:370).
      Fronteira de vencido / R3 (domínio x SQL): TestListarEmprestimosVencidoFronteira (listagem_filtro_test.go:302). Relógios: 23:59:59 SP, 00:00 SP, 12:00 SP, 23:59:59.999999 SP e UTC+14 (:312-320), em 5 fusos de sessão (:331). O oráculo é Multa > 0 e Renovar == ErrEmprestimoVencido.
      Plano: TestPlanoListarEmprestimos (plano_listagem_test.go:270), com plano genérico e custom das 16 combinações e regressão sem filtro na PK.
      Código conferido no pipeline_diff de repositorio.go: só fragmentos constantes; placeholder `"$" + strconv.Itoa(len(args)+2)`; valores só em args; sem OFFSET, COUNT, now() ou CURRENT_DATE.
  - Migração 0003 (índices), revisada pelo migration-reviewer (handoff 09, aprovado):
      migrations/indices_listagem_test.go: TestUp0003 (:37) e TestDown0003 (:64).
      internal/storage/esquema_test.go: 9 índices esperados, com a definição exata dos 3 novos, e TestMigracaoUpDownUp/down_só_da_0003. Os dois passam com banco no JSON da sessão principal.
  - D-21 em docs-ai/DECISOES.md e mapa docs-ai/03-MAPA-RAPIDO-MODULOS.md (via docs-update):
      Item de documentação, sem teste aplicável. Ainda NÃO feito: é a etapa seguinte. Nenhum dos dois está no pipeline_diff.
  - Marcação merged da DEL-014, conferida no pipeline_diff:
      docs-ai/deliveries/DEL-014/report.md: só `status: em revisão` → `status: merged`.
      docs-ai/deliveries/INDEX.md: a linha da DEL-014 passa de "em revisão" para "merged", e entra a linha nova da DEL-015 (draft, gerada pela ferramenta). Nada além disso. Sem teste aplicável.

SCOPE_CREEP:
  - Nenhum. O diff tem 28 arquivos: internal/http, internal/storage, migrations e docs-ai/deliveries.
  - Não aparecem: internal/domain/, internal/http/erros.go, internal/migrate/, 0001/0002, go.mod, go.sum, .claude/, .pipeline-claude.json, DTO.
  - GET /livros/{id}/emprestimos (U2) não foi criada. O mux não tem o padrão (handler.go:135-149) e TestMuxNotFoundEMethodNotAllowed exige 404 para ela.
  - Os índices simples da 0001 não foram removidos: o down só cita os 3 novos (TestDown0003).
  - As sugestões não impeditivas (S1-S4 do code-reviewer, S1-S3 do migration-reviewer) não foram aplicadas no código, inclusive a S2 do comentário do .up.sql, conforme combinado.
  - As edições em testes existentes (handler_test.go mux, rotasProtegidas, TestListagensNovasParametros) são consequência direta do escopo, aprovadas pelo code-reviewer (S4).

BORDAS_CONFERIDAS:
  - Combinação que nunca casa e id inexistente dão 200 vazio: os subtestes vazio/* (storage) e *_inexistente_é_200_vazio (HTTP com banco).
  - Paginação filtrada com limite 1/2/100 e apos: subtestes limite_* e "apos combina com o filtro".
  - Fronteira de vencido no dia do vencimento (23:59:59 não vencido, 00:00 vencido): coberta em storage e via API. Abertos não vencidos saem com multa 0.00 e vencidos nunca saem com multa 0.
  - Fuso da sessão do banco não interfere: 5 fusos.
  - ErrListagemInvalida e ErrUsuarioNaoEncontrado vindos de ListarEmprestimos na rota aninhada: 500, sem virar 404 (listagem_filtro_emprestimos_test.go:337-343).
  - Log sem valores dos filtros: TestLogFiltrosEmprestimosSemValores.
  - /livros e /usuarios continuam recusando as chaves novas: TestFiltrosEmprestimosNaoValemEmOutrasListagens.
  - Subrotas vizinhas (barra final, /emprestimos/2, POST/DELETE): TestMuxNotFoundEMethodNotAllowed.
  - Não encontrei borda relevante sem teste. A paginação de vencido atravessando a meia-noite SP não tem teste; é consistência eventual aceita (R1(b) do triage), não defeito.

CONFIRMADO:
  - As linhas de GATES, COBERTURA_DO_ESCOPO e SCOPE_CREEP acima, com caminhos e saídas reais.
  - pipeline_diff de handler.go e repositorio.go lido por inteiro neste agente. handler.go:705-736 lido diretamente.

ASSUNCOES:
  - RESULTADO ok, e não parcial, apesar de skipped=220 no sensor desta etapa. Segui o mesmo critério da DEL-014 (handoff 07-qa-validation). A regra pede parcial quando a camada de banco não foi provada. Aqui ela foi, pela execução da sessão principal com BIBLIOTECA_TEST_DB_OBRIGATORIO=1, cujo JSON conferi (0 skip, 0 fail, e os testes de banco desta delivery com pass). Não repeti essa execução: ela é da sessão principal. Se o critério exigir execução própria do QA com banco, o resultado passa a parcial pelo motivo "sensor test sem BIBLIOTECA_TEST_DATABASE_URL".
  - Os totais 3019/0/0 são os informados pela sessão principal. Por Grep confirmei 0 skip, 0 fail, os 8 pacotes com pass e os testes listados.
  - O fuzz de 30 s é evidência do handoff 06, não repetida aqui.
  - O triage diz "500 bug_interno" para ErrListagemInvalida. O teste (:343) espera o código "erro_interno", que é o mapeamento anterior e não está no diff. Isso não altera o comportamento; o docs-update deve usar o código real na D-21.

ARQUIVOS:
  - nenhum alterado (subagente só leitura)

RISCOS:
  - O sensor `test` padrão roda sem banco (220 pulados). A barreira R3 (domínio x SQL), o EXPLAIN e o up/down da 0003 só são exercidos no CI com PostgreSQL e BIBLIOTECA_TEST_DB_OBRIGATORIO=1.
  - EXPLAIN mais frouxo nas combinações 6, 7 e 13-16, que aceitam BitmapAnd (handoff 05, S1/S2 do code-reviewer). O critério de remoção dos índices simples precisa mudar na D-21.
  - Pendências obrigatórias do docs-update, todas pendentes no diff:
      - D-21 com U1, U2, U3, D7 e a exceção à G7 restrita à 0003, com limite de ~1M linhas;
      - ampliar a condição de bloqueio da D-20 para a rota aninhada e os filtros (MEDIO do security-review);
      - BAIXOs R6 e as duas consultas por página;
      - runbook §6 e nota da 0003 (count/pg_total_relation_size, janela, autovacuum anti-wraparound, down por psql com lock_timeout);
      - mapa de módulos;
      - desvio S4 no report.
  - Premissa de 10^4-10^5 linhas em produção não verificada: o lock SHARE da 0003 bloqueia escritas durante a construção.

SINAIS:
  SECURITY: sim — revisado no handoff 08, sem CRITICO ou ALTO. O MEDIO aceito exige registro na D-21. govulncheck: "No vulnerabilities found."
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff; mod_verify "all modules verified".
  MIGRATION: sim — 0003 aprovada no handoff 09 e provada com banco (TestMigracaoUpDownUp/down_só_da_0003 com pass). Pendência de runbook no docs-update.
  PERFORMANCE: sim — TestPlanoListarEmprestimos passou com banco (8,37 s). A folga do BitmapAnd fica registrada.

PROXIMO: docs-update
MOTIVO: sem achado bloqueante. release-ops não está em enabled_modules (habilitados: data-model e migration-reviewer). O docs-update registra a D-21, ajusta o runbook §6 e o mapa e preenche as seções QA e Security do report da DEL-015.
