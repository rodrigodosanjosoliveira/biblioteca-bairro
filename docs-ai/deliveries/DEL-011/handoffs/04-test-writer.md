DELIVERY_ID: DEL-011
TASK_CODE: DEL-011
AGENT: test-writer
STATUS: ok

CONFIRMADO:
  - `go test ./...` na árvore do projeto (sem banco) falha pelo motivo certo, porque a implementação ainda não existe:
      internal/domain [build failed]: "too many arguments in call to ReidratarEmprestimo" (emprestimo_multa_test.go:326 etc.), "e.Renovacoes undefined" (emprestimo_test.go:44, reidratar_test.go:42)
      internal/storage [build failed]: "lido.Renovar undefined", "f.repo.RegistrarRenovacao undefined", "undefined: domain.ErrLimiteRenovacoes" (renovacao_test.go)
      internal/http [build failed]: "undefined: domain.ErrEmprestimoVencido / ErrLimiteRenovacoes / ErrRenovacoesInvalidas" (erros_interno_test.go:77-80)
      migrations: "--- FAIL: TestUp0002 / TestDown0002: open 0002_renovacao_emprestimo.{up,down}.sql: file does not exist"
      cmd, config, middleware, migrate: ok (sem mudança)
  - Implementação de referência numa cópia temporária fora da árvore (mktemp -d no scratchpad, sem .git), seguindo os handoffs 02 e 03 (SQL do CAS com $5 = Renovacoes()-1 como no 03). Com ela, `go vet ./...` limpo e `go test -count=1 ./...` verde com e sem banco. Com banco: Postgres 16 descartável em container (trust), `BIBLIOTECA_TEST_DATABASE_URL` + `BIBLIOTECA_TEST_DB_OBRIGATORIO=1`, todos os pacotes ok. Com `-v` confirmei que os testes de banco novos rodaram e não foram pulados (TestMigracaoUpDownUp, TestEsquemaRestricoes, TestRegistrarRenovacao*, TestBancoRenovacao*). `go test -race` em domain e http: ok.
  - Fuzz na referência: `go test -fuzz=^FuzzEmprestimoRenovar$ -fuzztime=30s ./internal/domain` → PASS (1,9M execs); `-fuzz=^FuzzReidratarEmprestimo$ -fuzztime=30s` → PASS (1,08M execs). Nenhum testdata/fuzz criado na árvore.
  - Mutantes: 48 mortos, 2 sobreviventes (50 mutações, uma por vez, na cópia, com banco).
      Mortos: 18 no domínio (fronteira >= e no instante 12:00, limite > e removido, ordem limite/vencido, 13 dias, base ref, sem incremento, sem checar aberto/ref zero/início zero, faixa no Reidratar, contador descartado, getter zero, mutação antes do limite, hora 0, fuso UTC); 11 no storage (CAS sem contador/devolução/início, diagnóstico devolvido/inexistente, Scan descartando contador, grava contador antigo, não grava vencimento, pré-SQL sem ID/aberto/contador<1); 12 no HTTP (rota sem auth, aceita corpo, DTO sem renovacoes, vencido 422, classificar sem vencido/limite, ErrRenovacoesInvalidas fora de errosDeBug, responde o renovado em memória, time.Now no lugar do relógio injetado, não grava, mensagem do limite, status 201); 7 na migração (CHECK <= 3, DEFAULT 1, smallint, CHECK sem mínimo, coluna nullable, down sem DROP CONSTRAINT, down com IF EXISTS).
      Sobrevivente D19 (data civil lida com `e.vencimento.UTC().Date()` no lugar de `.In(locSaoPaulo)`): é equivalente, porque o vencimento é sempre 12:00 SP (NovoEmprestimo, ReidratarEmprestimo e o próprio Renovar normalizam). 12:00 SP cai na mesma data em UTC (15:00 ou 14:00 no horário de verão), e Emprestimo{} é recusado antes.
      Sobrevivente S12 (sem `e.Renovacoes() > domain.LimiteRenovacoes` na recusa pré-SQL de RegistrarRenovacao): é equivalente fora do pacote domain. Não há como construir um Emprestimo com contador > 2, porque Reidratar recusa e Renovar para no limite. O CHECK do banco seria a barreira seguinte. A cláusula pedida no 03 fica como defesa.
  - Cópia temporária, script de mutação e container apagados ao final; nenhuma implementação na árvore. `gofmt -l internal migrations`: vazio.

ASSUNCOES:
  - Texto das sentinelas novas fixado pelos testes (padrão de TestErrosEmprestimoMensagens): ErrEmprestimoVencido = "emprestimo: vencido", ErrLimiteRenovacoes = "emprestimo: limite de renovações atingido", ErrRenovacoesInvalidas = "emprestimo: renovações inválidas". Os handoffs não definiam esses textos. Se o dev preferir outro, ajustar só renovar_test.go:TestErrosRenovacaoMensagens.
  - Mensagens HTTP fixadas como no 02: "empréstimo vencido" e "limite de renovações atingido" (409).
  - Renovar não recusa ref anterior ao início, porque a ordem do 02 não tem essa checagem. Coberto em "ref anterior ao inicio nao e recusada" (renovar_test.go).
  - Em TestUp0002, além do ALTER TABLE único, só se aceita `COMMENT ON COLUMN emprestimo.renovacoes IS ...` (opcional no 03). Qualquer outro comando reprova. O ALTER precisa conter, depois de normalizar espaços e caixa, "add column renovacoes integer not null default 0" e "add constraint emprestimo_renovacoes_check check (renovacoes >= 0 and renovacoes <= 2)". No down, DROP CONSTRAINT vem antes de DROP COLUMN.
  - O default da coluna é conferido por `information_schema.columns.column_default = '0'` (catálogo em esquema_test.go).
  - TestBancoRenovacaoConcorrente aceita 1 ou 2 renovações com 200 em 16 paralelas, porque requisições serializadas podem renovar duas vezes legitimamente. O teste exige que o contador e o vencimento gravados batam com o número de 200 e que os 409 sejam só `emprestimo_alterado` ou `limite_renovacoes_atingido`.

ARQUIVOS:
  - internal/domain/renovar_test.go — novo: constantes, mensagens e distinção das sentinelas, NovoEmprestimo com contador 0, ReidratarEmprestimo com contador (-1, 0, 1, 2, 3, MinInt, MaxInt e ordem da validação, sempre por último), Renovar sucesso (dia do início, ref antes do início, 12:00 exato, 23:59:59.999 SP, UTC e UTC+9, base no vencimento guardado, virada de ano, bissexto, 04/11/2018, fim do horário de verão 2019), até o limite (3ª e 4ª tentativas), erros com ordem e não-mutação (Emprestimo{}, devolvido, ref zero, vencido 00:00 SP e 03:00 UTC, vencido antes do limite), fronteira igual à da multa, multa sobre o vencimento novo, ida e volta pela reidratação, FuzzEmprestimoRenovar com oráculo independente.
  - internal/domain/reidratar_test.go — 7º argumento (0) nas chamadas; checarMesmoEmprestimo compara Renovacoes(); FuzzReidratarEmprestimo ganha o argumento `renov uint8` (0..2 renovações antes da ida e volta) e seeds novas.
  - internal/domain/emprestimo_test.go — checarEmprestimoZero exige Renovacoes() == 0.
  - internal/domain/emprestimo_multa_test.go — 7º argumento (0) em ReidratarEmprestimo.
  - internal/storage/renovacao_test.go — novo (com banco): AbrirEmprestimo com contador 0, duas renovações gravadas e relidas (DATE e contador direto no banco), 3ª recusada, devolução preservando o contador, recusas do CAS com estado da linha intacto (inexistente → ErrEmprestimoNaoEncontrado, devolvido → domain.ErrEmprestimoJaDevolvido, cópia velha, contador adiantado e início divergente → ErrEmprestimoDivergente), corrida renovação×renovação (Divergente, contador 1), devolução antes da renovação (JaDevolvido, linha intacta), renovação antes da devolução (devolução grava sobre o vencimento novo e devolve contador 1).
  - internal/storage/esquema_test.go — catálogo exato: `emprestimo_renovacoes_check` (c), coluna `emprestimo.renovacoes` integer NOT NULL, trechos `renovacoes >= 0` e `renovacoes <= 2`, default '0'. TestEsquemaRestricoes ganha -1, 3 e 2147483647 → 23514/emprestimo_renovacoes_check, NULL → 23502, INSERT sem a coluna lendo 0, 0..2 aceitos e UPDATE para 3 recusado.
  - internal/storage/sem_banco_test.go — 7º argumento nas chamadas; recusas pré-SQL de RegistrarRenovacao (Emprestimo{}, sem ID, devolvido, contador 0); erro do banco em RegistrarRenovacao sem IDs nem SQL.
  - internal/storage/repositorio_test.go — 7º argumento nas chamadas; checarEmprestimo compara Renovacoes().
  - internal/http/apoio_test.go — repoFake.RegistrarRenovacao (errRenovar, renovarRet, renovarRecebido); helper emprestimoRenovado; rota "POST renovacao" em rotasProtegidas, o que a coloca em TestAuthRotasProtegidas e TestTimeoutPorOperacao.
  - internal/http/handler_test.go — emprestimoJSON passa a exigir a chave `renovacoes` (com 0), e emprestimoJSONRenovacoes é novo. "subrota desconhecida" (antigo :659) usa `/livros/1/inexistente`, e há casos novos de GET/PUT em /renovacao → 405. TestMapaDeErros ganha os casos de renovação (400 corpo, 404 na busca e no registro, 409 vencido/limite/já devolvido/alterado, 503, 500 relógio zerado, ErrRenovacoesInvalidas, ErrEmprestimoInvalido, PgError e erro desconhecido). Também há casos de renovação em TestAuthAntesDeTudo, TestDecodificacaoDoCorpo e TestParametroID.
  - internal/http/renovacao_test.go — novo: contrato 200 (vencimento novo, renovacoes, multa, chamadas Busca→RegistrarRenovacao, valor entregue ao repositório), resposta com o retorno do repositório, recusas que não chamam RegistrarRenovacao (vencido, vencido ganha do limite, limite, devolvido, relógio zerado), mensagens fixas, `renovacoes` no GET e na devolução.
  - internal/http/erros_interno_test.go — TestCatalogoRenovacao: classificar, catálogo (409 e mensagens) e causa bug_interno para ErrRenovacoesInvalidas.
  - internal/http/banco_renovacao_test.go — novo (com banco): fluxo completo (404 sem empréstimo e com livro inexistente, 2 renovações, 3ª com limite, vencido, consulta e devolução com contador, novo empréstimo com 0) e 16 renovações paralelas.
  - migrations/renovacao_test.go — novo: TestUp0002 e TestDown0002 (nomes `0002_renovacao_emprestimo.{up,down}.sql`).

RISCOS:
  - Fora do CI, os testes de banco (storage e http) só rodam com `BIBLIOTECA_TEST_DATABASE_URL`. Sem a variável, o verde local de storage e http não prova o CAS, o CHECK nem a migração. O dev precisa rodar com banco, como eu fiz na referência.
  - TestUp0002 e TestDown0002 dependem do nome exato `0002_renovacao_emprestimo`. Outro nome reprova.
  - O FuzzReidratarEmprestimo mudou de assinatura (4 argumentos). Um corpus em cache do formato antigo é ignorado, e não há corpus versionado.
  - TestBancoRenovacaoConcorrente aceita 1 ou 2 sucessos, então não detecta sozinho uma implementação que renove 2 vezes a partir da mesma leitura. Quem cobre isso de forma determinística é TestRegistrarRenovacaoConcorrente (storage).

SINAIS:
  SECURITY: sim — os testes cobrem a rota autenticada nova e mudanças em internal/http, internal/storage e migrations (security_surface_paths).
  SUPPLY_CHAIN: nao — nenhuma dependência nova; go.mod e go.sum intocados.
  MIGRATION: sim — testes de catálogo, restrições e up/down da 0002; migration-reviewer segue obrigatório.
  PERFORMANCE: nao — sem consulta nova além de UPDATE e SELECT por PK.

PROXIMO: dev-implementation
MOTIVO: os testes definem o contrato (domínio, storage, HTTP e migração 0002) e falham por ausência de implementação; o dev implementa contra eles e roda a suíte com banco.
