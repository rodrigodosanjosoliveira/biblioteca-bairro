DELIVERY_ID: DEL-011
TASK_CODE: DEL-011
AGENT: dev-implementation
STATUS: ok

CONFIRMADO:
  - NAO_FAZER do 02 relido antes de começar e respeitado: sem FOR UPDATE nem transação no handler, sem incremento nem aritmética de data no SQL, fronteira "vencido" pelo dia civil SP (meiaNoiteUTC), base dos 14 dias no vencimento atual, sem BuscarLivro extra, sem códigos novos para "sem empréstimo aberto"/"já devolvido", 409 (não 422), corpo vazio obrigatório, sem histórico nem env. 0001, internal/migrate, cmd, go.mod/go.sum, CalcularMulta e Multa intocados.
  - Nenhum arquivo _test.go alterado por mim. Os auxiliares citados (emprestimoRenovado, emprestimoJSONRenovacoes, campos renovarRecebido/renovarRet/errRenovar do repoFake) já existem em internal/http/apoio_test.go e handler_test.go (do test-writer): não houve lacuna, e nada foi criado em código de produção para eles.
  - Sensores (pipeline_sensors, todos ok=true):
      vet: ok, exit 0, saída vazia
      test: ok, exit 0 — "pacotes: 8 (8 ok, 0 com falha, 0 sem testes) / testes: 1580 passaram, 0 falharam, 200 pulados" (os pulados são os testes de banco, porque o sensor roda sem BIBLIOTECA_TEST_DATABASE_URL)
      build: ok, exit 0, saída vazia
      vuln: ok — "No vulnerabilities found."
      mod_verify: ok — "all modules verified"
  - Suíte COM banco: subi um Postgres 16 próprio e descartável (container del011-dev-pg, trust, 127.0.0.1:55411; já parado e removido com --rm). Não usei o container postgres:16 de outro projeto que estava rodando. Com BIBLIOTECA_TEST_DATABASE_URL=postgres://postgres@127.0.0.1:55411/postgres?sslmode=disable e BIBLIOTECA_TEST_DB_OBRIGATORIO=1, `go test -count=1 -p 1 ./...` deu:
      ok cmd/biblioteca-bairro 7.148s
      ok internal/config 0.004s
      ok internal/domain 0.015s
      ok internal/http 3.137s
      ok internal/http/middleware 0.005s
      ok internal/migrate 14.388s
      ok internal/storage 2.104s
      ok migrations 0.003s
    `go test -count=1 -p 1 -v ./... | grep -c -- "--- SKIP"` → 0 (nenhum teste pulado com banco).
    Testes de banco novos e afetados, todos com PASS: TestMigracaoUpDownUp, TestEsquemaRestricoes, TestAbrirEmprestimoSemRenovacoes, TestRegistrarRenovacao, TestRegistrarRenovacaoRecusas, TestRegistrarRenovacaoConcorrente, TestRegistrarRenovacaoConcorrenteComDevolucao, TestRegistrarDevolucaoConcorrenteComRenovacao, TestBancoRenovacao, TestBancoRenovacaoConcorrente.
  - Race, com banco: `go test -count=1 -p 1 -race ./internal/domain ./internal/http ./internal/storage` → ok, ok, ok.
  - Sem banco: `go vet ./... && go test -count=1 ./...` → 8 pacotes ok.
  - Fuzz (alvos tocados), 30 s cada:
      go test -run='^$' -fuzz='^FuzzEmprestimoRenovar$' -fuzztime=30s ./internal/domain
        fuzz: elapsed: 30s, execs: 2300295 (0/sec), new interesting: 0 (total: 12)
        PASS
        ok  .../internal/domain 30.105s
      go test -run='^$' -fuzz='^FuzzReidratarEmprestimo$' -fuzztime=30s ./internal/domain
        fuzz: elapsed: 30s, execs: 796566 (0/sec), new interesting: 0 (total: 13)
        PASS
        ok  .../internal/domain 30.109s
    Nenhum testdata/fuzz criado na árvore.
  - `gofmt -l internal migrations`: vazio.

ASSUNCOES:
  - Textos das sentinelas são os fixados pelos testes: "emprestimo: vencido", "emprestimo: limite de renovações atingido" e "emprestimo: renovações inválidas".
  - O SQL do CAS segue o refinamento do 03 (`renovacoes = $5`, com $5 = Renovacoes()-1 calculado em Go). O diagnóstico sem linha usa só `SELECT devolucao IS NOT NULL ... WHERE id = $1`: inexistente → ErrEmprestimoNaoEncontrado, devolvido → domain.ErrEmprestimoJaDevolvido, demais casos → ErrEmprestimoDivergente.
  - A recusa pré-SQL inclui `e.Renovacoes() > domain.LimiteRenovacoes` como defesa (pedida no 03; mutante equivalente S12 do test-writer).
  - Migração com comentário SQL de cabeçalho (`--`) em português, sem COMMENT ON COLUMN e sem palavras proibidas pelo controle (concurrently/begin/commit/end no início de linha).

ARQUIVOS:
  - migrations/0002_renovacao_emprestimo.up.sql — novo: um ALTER TABLE com ADD COLUMN renovacoes integer NOT NULL DEFAULT 0 e ADD CONSTRAINT emprestimo_renovacoes_check CHECK (renovacoes >= 0 AND renovacoes <= 2).
  - migrations/0002_renovacao_emprestimo.down.sql — novo: ALTER TABLE com DROP CONSTRAINT emprestimo_renovacoes_check e DROP COLUMN renovacoes, sem IF EXISTS e sem CASCADE.
  - internal/domain/emprestimo.go — constantes LimiteRenovacoes e PrazoRenovacaoDias; sentinelas ErrEmprestimoVencido, ErrLimiteRenovacoes e ErrRenovacoesInvalidas; campo renovacoes e getter Renovacoes(); ReidratarEmprestimo com 7º parâmetro e validação de faixa por último; método (*Emprestimo).Renovar(ref).
  - internal/storage/repositorio.go — colunasEmprestimo com renovacoes; lerEmprestimo faz Scan de 7 colunas e reidrata com o contador; método novo RegistrarRenovacao (CAS + diagnóstico).
  - internal/http/handler.go — RegistrarRenovacao na interface Repositorio; rota autenticada POST /livros/{id}/renovacao; handler renovar (molde de devolver); saidaEmprestimo.Renovacoes (json "renovacoes"), preenchido em responderEmprestimo.
  - internal/http/erros.go — códigos emprestimo_vencido e limite_renovacoes_atingido (409, mensagens "empréstimo vencido" e "limite de renovações atingido"); classificar mapeia as duas sentinelas; ErrRenovacoesInvalidas entra em errosDeBug.

RISCOS:
  - O sensor `test` do pipeline roda sem banco (200 pulados). A prova do CAS, do CHECK e da migração está só na execução manual com banco descrita em CONFIRMADO. O qa-validation deve considerar isso, ou o CI precisa rodar com banco.
  - Contrato da API: campo novo `renovacoes` em todas as respostas de empréstimo, rota nova e dois códigos 409 novos (já listados no 02).
  - Ordem de deploy: `migrate up` 0002 antes do binário novo, senão dá 42703 em toda leitura de empréstimo. O down perde o contador; reverter o binário antes.
  - O working tree segue com as mudanças não commitadas da DEL-010 (report/INDEX), que não toquei. O git-delivery decide.

SINAIS:
  SECURITY: sim — rota autenticada nova e mudanças em internal/http, internal/storage e migrations (security_surface_paths).
  SUPPLY_CHAIN: nao — nenhuma dependência nova; go.mod e go.sum intocados.
  MIGRATION: sim — migrations/0002_renovacao_emprestimo.{up,down}.sql; migration-reviewer obrigatório.
  PERFORMANCE: nao — UPDATE e diagnóstico por PK; busca pelo índice parcial existente.

PROXIMO: code-reviewer
MOTIVO: implementação completa dentro do escopo; sensores, suíte com banco (0 pulados), race e fuzz verdes; segue para revisão de código e, depois, migration-reviewer e security-review.
