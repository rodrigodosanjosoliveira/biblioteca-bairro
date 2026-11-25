DELIVERY_ID: DEL-017
TASK_CODE: PROJ-7
AGENT: test-writer
STATUS: bloqueado

TESTES_ESCRITOS (stdlib testing, em tabela):
  storage com banco (/home/rodrigo/workspace/biblioteca-bairro/internal/storage/disponibilidade_test.go):
    - TestBuscarLivroDisponibilidade (CA3). Tabela de 10 cenários: livro do fixture, sem empréstimo, aberto no prazo (início = relógio real), devolvido, aberto vencido (início em 2020), vários devolvidos, renovado, renovado e devolvido, devolvido e reaberto, devolvido depois do vencimento. Confere Livro (id, título) e Disponivel.
    - TestBuscarLivroDisponibilidadeTransicoes (CA3): o mesmo livro passa por novo, aberto, renovado, devolvido e reaberto (true, false, false, true, false).
    - TestBuscarLivroDisponibilidadeInexistente: livro apagado, id além do último, máximo int64, 0 e -1 dão ErrLivroNaoEncontrado com valor zero.
    - TestListarLivrosDisponibilidade (CA4 e CA2): os cenários alternam true/false de propósito (o helper reprova vizinhos iguais). Percorre com limite de 1 a 11 e confere: número de itens = min(limite, restantes), ou seja, o LIMIT conta livros e livro com vários empréstimos não duplica; temMais; ordem; cada livro visitado uma vez; Disponivel por item. Também confere que a página inteira é igual às páginas unitárias.
    - TestListarLivrosDisponibilidadeTodosEmprestados: página só com indisponíveis e, depois de devolver tudo, só com disponíveis.
    - TestLeituraLivroUmaConsulta (CA5, parte "uma consulta"). Um DBTX contador envolve o banco real e exige exatamente um comando por chamada, com o texto igual a sqlListarLivros ou sqlBuscarLivro, em: página mista, página vazia, detalhe disponível, detalhe indisponível e detalhe inexistente. Com id 0, exige zero comandos.
  storage sem banco (/home/rodrigo/workspace/biblioteca-bairro/internal/storage/disponibilidade_sem_banco_test.go):
    - TestSQLLeituraLivro (D-21 e NAO_FAZER). O texto precisa ter `not exists`, `from emprestimo e`, `e.livro_id = l.id` e `e.devolucao is null`. Na listagem exige ainda `l.id > $1`, `order by l.id` e `limit $2`; no detalhe, `l.id = $1` e nem LIMIT nem ORDER. Proíbe now()/current_*, vencimento, coalesce, " or ", join, distinct, lateral, bool_or, count(, group by, offset, materialized, "with ", enable_, ";" e "::". Exige exatamente dois FROM e nenhum parâmetro além do contrato.
  plano (/home/rodrigo/workspace/biblioteca-bairro/internal/storage/plano_livros_test.go):
    - TestPlanoLeituraLivro (CA5, parte "índice"). Reaproveita fixturePlano, lerPlano e nosDoPlano, com EXPLAIN VERBOSE próprio (planoVerbose), genérico e custom. O detalhe tem 4 casos: genérico, livro 1 sem aberto, livro 151 com aberto e inexistente. A listagem tem 6: genérico, e custom com apos 0/250/450 e limite+1 101/21/2.
    - Exige em todos: Index Scan em livro_pkey; um único scan em emprestimo, com Parent Relationship = SubPlan, Index Scan ou Index Only Scan em emprestimo_livro_aberto_uq, Index Cond com `livro_id = l.id` e sem Filter; nenhum Seq Scan nem Bitmap; nenhum "hashed SubPlan" no Subplan Name nem no Output.
    - O subteste "valores na massa do plano" confere que o livro 1 é o único disponível.
    - O JSON sem VERBOSE não revela o hashed (o Subplan Name fica "SubPlan 2"), por isso o teste lê Output, que o VERBOSE preenche, e Index Cond.
  http com fake:
    - /home/rodrigo/workspace/biblioteca-bairro/internal/http/listagem_test.go:
      - TestContratoSaidaDetalheLivro (CA1), em tabela: disponível; indisponível com `"disponivel":false` literal; relógio zerado não muda nada. Exige as chaves exatas {id, titulo, disponivel}, bool JSON e uma só chamada BuscarLivro.
      - TestListarLivrosDisponivel (CA2): páginas só true, só false, mista com temMais e de um item false, e relógio zerado. Conta as ocorrências de `"disponivel":false`, confere proximo e exige uma só chamada ListarLivros.
      - TestListarLivrosSemFiltroDisponivel: ?disponivel=... continua 400 parametro_invalido sem chamar o repositório.
      - listaJSON passa a exigir {id, titulo, disponivel} com bool. O helper novo disponiveisJSON devolve o bool de cada item.
      - TestListarLivrosContrato confere o disponivel de cada item (ímpar = true).
    - /home/rodrigo/workspace/biblioteca-bairro/internal/http/handler_test.go: TestContratoSaidaLivroEUsuario (POST /livros) já exigia exatamente {id, titulo}. Agora exige também a ausência do texto "disponivel".
  http com banco:
    - /home/rodrigo/workspace/biblioteca-bairro/internal/http/banco_disponibilidade_test.go: TestBancoDisponibilidadeLivros. Seis livros criados por POST /livros, que precisa devolver só {id, titulo}: nunca emprestado, aberto, devolvido, renovado, aberto vencido (relógio adiante) e devolvido e reaberto. Confere GET /livros/{id} e a paginação de GET /livros com limite 1, 2, 4, 6 e 100. Depois, devolver torna o livro disponível nas duas rotas, e o 404 continua.
    - /home/rodrigo/workspace/biblioteca-bairro/internal/http/banco_listagem_test.go: TestBancoDetalhes passa a exigir {id, titulo, disponivel:true}.

CONFIRMADO:
  - `go test -count=1 -p 1 ./...` com BIBLIOTECA_TEST_DATABASE_URL (Postgres 16-alpine com o digest do CI e trust, descartável e já parado) dá a falha esperada, por falta da implementação:
      FAIL internal/http [build failed]: apoio_test.go:86:27: undefined: storage.LivroComDisponibilidade (e as demais referências ao tipo)
      FAIL internal/storage [build failed]: listagem_test.go:48:20: undefined: LivroComDisponibilidade; disponibilidade_sem_banco_test.go:22:27: undefined: sqlListarLivros; :24:26: undefined: sqlBuscarLivro; disponibilidade_test.go:143:11: got.Livro undefined (type domain.Livro has no field or method Livro)
      ok cmd/biblioteca-bairro, internal/config, internal/domain, internal/http/middleware, internal/migrate, migrations
    Os dois pacotes não compilam até existirem `storage.LivroComDisponibilidade{Livro domain.Livro; Disponivel bool}`, as consts `sqlListarLivros`/`sqlBuscarLivro` e as assinaturas novas de BuscarLivro/ListarLivros (A2 do handoff 03). Por isso nenhum teste desses pacotes roda antes da implementação.
  - Com uma implementação de referência que segue à letra o desenho do handoff 03 (EXISTS na projeção, tipo de leitura, saidaLivroLido, saidaLivro intocado), escrita numa cópia em mktemp, toda a suíte com banco passa, exceto TestPlanoLeituraLivro/listagem/* (os 6 casos). Os 4 casos de detalhe passam: Index Only Scan em emprestimo_livro_aberto_uq com Index Cond (e.livro_id = l.id).
  - BLOQUEIO, o risco do handoff 04 confirmado. Na listagem, o PostgreSQL 16 escolhe hashed SubPlan, no genérico e em todos os custom, até com LIMIT 2:
      Limit → Index Scan livro_pkey (l.id > '0'::bigint), Output "(NOT (hashed SubPlan 2))"
        SubPlan 2 → Index Scan emprestimo_aberto_id_idx, sem Index Cond (lê todos os abertos)
    A escolha usa a estimativa do scan de livro abaixo do Limit (500 linhas), não o LIMIT. Testei no psql com a massa de fixturePlano, sem tocar no projeto:
      (a) livro paginado em subconsulta, com o EXISTS fora: hashed, emprestimo_aberto_id_idx;
      (b) EXISTS com LIMIT 1 interno: hashed, emprestimo_aberto_id_idx;
      (c) LEFT JOIN ... AND devolucao IS NULL, proibido e só para referência: Hash Right Join com Seq Scan em livro e Sort.
    Não há Seq Scan em emprestimo, e o custo do hashed é O(abertos), limitado pelo número de livros pela R4. Mesmo assim, o CA5 como está escrito ("usando emprestimo_livro_aberto_uq") e a regra do handoff 04 (sem hashed) não se cumprem com nenhuma forma permitida pelo NAO_FAZER (sem SET enable_*, LATERAL nem JOIN).
  - Mutantes: 25 mortos, 1 sobrevivente. Rodados na cópia temporária com `-skip ^TestPlanoLeituraLivro$`, porque a referência já reprova esse teste. Mortos:
      SQL: EXISTS invertido (listagem e detalhe); devolucao IS NULL removido (listagem e detalhe); IS NOT NULL; `AND vencimento >= CURRENT_DATE` (listagem e detalhe); correlação trocada por `livro_id > 0` (listagem e detalhe); `livro_id = l.id + 1`; LEFT JOIN sem filtro (duplica); LEFT JOIN correto (só o TestSQLLeituraLivro mata); ORDER BY DESC.
      storage: Scan descartando disponivel; detalhe sempre true; detalhe com duas consultas (só o TestLeituraLivroUmaConsulta mata); ErrNoRows sem virar 404.
      handler: disponivel fixo true na listagem; valor do primeiro item na listagem; disponivel fixo true no detalhe; detalhe com o DTO de POST; omitempty; `,string`; POST /livros com disponivel; proximo do primeiro item; listarLivros lendo o relógio.
    Sobrevivente: B12, que devolve `got` em vez do valor zero no ramo ErrNoRows de BuscarLivro. É equivalente: com ErrNoRows o Scan não preenche nada e `got` já é zero. TestBuscarLivroDisponibilidadeInexistente exige o valor zero.
  - Fuzz: a demanda não traz parsing novo (lerParametrosListagem não muda). Na cópia com a referência, o alvo existente rodou com o fake novo: `go test -run '^$' -fuzz='^FuzzParametrosListagem$' -fuzztime=30s ./internal/http` → PASS, 30343 execs.
  - Cópia temporária e referência apagadas. O container Postgres foi parado (--rm). `gofmt -l internal/` limpo.

ASSUNCOES:
  - Nomes e forma da implementação vêm do handoff 03: storage.LivroComDisponibilidade{Livro, Disponivel}, consts sqlListarLivros e sqlBuscarLivro no pacote storage, e alias `l` para livro e `e` para emprestimo. TestSQLLeituraLivro exige esses aliases e os trechos `e.livro_id = l.id` e `e.devolucao is null`. Outro alias reprova o teste de texto, não o comportamento.
  - O fake repoFake devolve Disponivel: true por padrão em BuscarLivro, como um livro sem empréstimo.
  - Assumo que quem decide o CA5 é o architecture-guardrails, com o usuário se o critério mudar. Se o hashed SubPlan sobre índice parcial de abertos for aceito, o ajuste do teste é local: em checarPlanoLivro (plano_livros_test.go), aceitar no caso da listagem o hashed sobre emprestimo_aberto_id_idx/emprestimo_livro_aberto_uq sem Seq Scan, e manter as asserções do detalhe.

ARQUIVOS:
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/disponibilidade_test.go — novo (CA3, CA4, CA5 uma consulta, 404)
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/disponibilidade_sem_banco_test.go — novo (texto SQL, D-21)
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/plano_livros_test.go — novo (CA5 plano, genérico e custom, VERBOSE)
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/plano_listagem_test.go — noPlano ganha ParentRelationship, SubplanName e Output, e String() passa a mostrá-los. TestPlanoListarEmprestimos não muda.
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/listagem_test.go — idsLidos e assinatura nova; TestListarLivros exige Disponivel true no acervo sem empréstimo
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/repositorio_test.go — TestLivroIdaEVolta compara lido.Livro e exige Disponivel true
  - /home/rodrigo/workspace/biblioteca-bairro/internal/http/apoio_test.go — repoFake com a assinatura nova (livroRet e listarRet do tipo storage.LivroComDisponibilidade)
  - /home/rodrigo/workspace/biblioteca-bairro/internal/http/listagem_test.go — livros(), listaJSON, disponiveisJSON, TestContratoSaidaDetalheLivro em tabela, TestListarLivrosDisponivel, TestListarLivrosSemFiltroDisponivel
  - /home/rodrigo/workspace/biblioteca-bairro/internal/http/handler_test.go — POST /livros sem "disponivel"
  - /home/rodrigo/workspace/biblioteca-bairro/internal/http/banco_listagem_test.go — TestBancoDetalhes com disponivel
  - /home/rodrigo/workspace/biblioteca-bairro/internal/http/banco_disponibilidade_test.go — novo (CA1 e CA2 no banco)
  Não tocados: internal/domain/, migrations/, go.mod, go.sum, código de produção.

RISCOS:
  - CA5 inalcançável com o desenho atual: o dev implementa tudo, e TestPlanoLeituraLivro/listagem/* continua vermelho (6 subtestes), sem forma permitida de corrigir.
  - TestSQLLeituraLivro fixa trechos textuais (aliases l/e). Um dev que reescreva o SQL com outro alias, de forma equivalente, verá falha de texto, não de comportamento. É intencional, pela D-21 e porque o teste de plano depende do texto real.
  - O teste de plano depende de VERBOSE: o formato do Output pode mudar em versão futura do PostgreSQL. A asserção de Index Cond com `livro_id = l.id` cobre o mesmo caso por outro caminho.

SINAIS:
  SECURITY: sim — os testes cobrem internal/http/ e internal/storage/ (security_surface_paths), e a implementação vai tocar os dois.
  SUPPLY_CHAIN: nao — nenhuma dependência nova; go.mod/go.sum intocados.
  MIGRATION: nao — nenhuma migração. O plano mostra que o índice existente basta para o detalhe; na listagem, o planner prefere emprestimo_aberto_id_idx (hashed).
  PERFORMANCE: sim — na listagem, hashed SubPlan via emprestimo_aberto_id_idx, que lê todos os abertos por página, em vez de uma sonda por livro em emprestimo_livro_aberto_uq. Isso diverge do CA5 e precisa de decisão.

PROXIMO: architecture-guardrails
MOTIVO: o NAO_FAZER do handoff 03 manda devolver ao architecture-guardrails, sem contornar, se o plano não usar emprestimo_livro_aberto_uq. O teste comprova isso na listagem, no genérico e em todos os custom, e as variantes permitidas que testei também caem em hashed. Cabe ao architecture-guardrails escolher: rever o CA5 (aceitar hashed SubPlan sobre índice parcial de abertos, limitado pela R4), o que pode exigir o pm-triage ou o usuário, ou propor outra forma de consulta. Os demais testes estão prontos para o dev-implementation: com a referência do desenho atual, todos passam.
