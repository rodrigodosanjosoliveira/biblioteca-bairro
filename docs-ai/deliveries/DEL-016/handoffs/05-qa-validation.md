DELIVERY_ID: DEL-016
TASK_CODE: DEL-016
AGENT: qa-validation
STATUS: ok

RESULTADO: ok. Este handoff substitui o `parcial` de 04-qa-validation.md. O motivo do parcial era a camada de banco sem prova, com 220 testes pulados no sensor local. A sessão principal rodou a suíte completa com Postgres e obrigatoriedade de banco, e deu 0 skip e 0 fail. O risco que restava, os query params opcionais e o ramo de cursor não nulo cobertos só de forma estática, também foi exercitado ao vivo. Não há achado bloqueante.

GATES:
  - pipeline_sensors (todos), saída real registrada em 04-qa-validation.md: vet ok; test ok com "testes: 2386 passaram, 0 falharam, 220 pulados", skipped=220, todos com o motivo "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado"; build ok; vuln ok com "No vulnerabilities found."; mod_verify ok com "all modules verified". Desde então o código não mudou: a delivery não toca Go, e o diff segue sem cmd/, internal/, migrations/, go.mod e go.sum.
  - Suíte com banco (evidência da sessão principal; este subagente não tem shell): Postgres 16 com o digest do CI e trust, comando `BIBLIOTECA_TEST_DATABASE_URL=... BIBLIOTECA_TEST_DB_OBRIGATORIO=1 go test -count=1 -p 1 -json ./...`, exit 0, com 3019 pass, 0 fail e 0 skip. Conferi o JSON em /tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/qa016-test.json com Grep:
    - `"Action":"(skip|fail)"` → 0 ocorrências.
    - `"Action":"pass"` → 3027 ocorrências. São 3019 testes mais 8 eventos de pacote, o que bate com o relato.
    - Testes de banco que o sensor tinha pulado aparecem agora como pass: TestMigrateAceitaParametrosDePoolNaDSN (cmd/biblioteca-bairro, linha 109 do JSON), TestBancoListagemEmprestimosFiltros (internal/http, 4157), TestBancoFluxoCompleto (internal/http, 4305), TestControleUpEIdempotente (internal/migrate, 8881) e TestMigracaoUpDownUp (internal/storage, 10109).
    - skipped=0: a camada de banco está provada.
  - Paginação ao vivo (evidência da sessão principal): executor /tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/run016-pag.mjs, saída lida em /tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/qa016-run-pag.txt. Binário da main com migrate up 0001-0003, 2 livros, 2 usuários e 2 empréstimos abertos do usuário 2:
    - 200 GET /livros?limite=1 com proximo=1; o cursor ficou em 1; 200 GET /livros?limite=1&apos=1 com o item id 2 e proximo=null; o cursor depois ficou undefined.
    - 200 GET /usuarios?limite=1 e depois &apos=1, com o mesmo padrão (id 2, proximo=null, cursor undefined).
    - 200 GET /emprestimos?situacao=aberto&usuario_id=2&limite=1 e depois &apos=1, com o mesmo padrão.
    - 200 GET /usuarios/2/emprestimos?situacao=aberto&limite=1 e depois &apos=1, com o mesmo padrão.
  - Fuzz: não se aplica, porque nenhum código Go nem alvo de fuzz foi tocado.

COBERTURA_DO_ESCOPO:
  - O que já constava em 04-qa-validation.md continua valendo: as 15 rotas, a auth Bearer com noauth nas rotas de saúde, o environment só com placeholders, as pastas e descrições, a seção Postman do README, o .gitignore e o status da DEL-015.
  - Query params opcionais: antes só tinham conferência estática, agora rodaram ao vivo. situacao, usuario_id, limite e apos em GET /emprestimos; situacao, limite e apos em /usuarios/{id}/emprestimos; limite e apos em /livros e /usuarios.
  - Script de cursor: o ramo set (proximo não nulo grava o cursor) e o ramo unset (proximo null remove o cursor) rodaram ao vivo nas 4 listagens.
  - livro_id como filtro saiu nas tentativas de qa016-run-params.txt junto com `apos=` vazio, que deu 400. Por isso não há 200 isolado com livro_id ligado. A cobertura dele é a conferência estática (handler.go) mais TestBancoListagemEmprestimosFiltros, que passou com banco.

SCOPE_CREEP:
  - Nenhum. A conclusão do handoff anterior se mantém.

BORDAS:
  - `apos=` vazio deu 400 parametro_invalido, ao vivo (/tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/qa016-run-params.txt, linhas 5, 8, 10, 15, 22, 25, 27 e 32). É o comportamento documentado da API e foi previsto em BORDAS no handoff anterior. Não é defeito da coleção, porque os params vêm desligados.
  - Cursor removido pelo script com apos ligado: no Postman vai o literal `{{cursor_*}}` e a API devolve 400. A descrição da coleção trata disso; o README não (ver ACHADOS).

ACHADOS:
  - Nenhum bloqueante.
  - Sugestão para o docs-update (não bloqueante, documentação): README.md:194-196 diz que "valor vazio" dá 400, mas não diz como usar o cursor. Acrescentar uma frase: ligue `apos` só quando o cursor tiver valor, porque o script remove o cursor quando `proximo` volta null (última página), e aí o Postman envia `{{cursor_...}}` literal, que também dá 400 `parametro_invalido`. A informação já está na descrição da coleção; a frase só a leva para o README. Isso cabe no escopo da seção Postman do README.
  - Cosmético, já registrado antes: a descrição de "Abrir empréstimo" não cita 413.

CONFIRMADO:
  - Contagens do JSON da suíte com banco, conferidas com Grep (0 skip/fail, 3027 eventos pass).
  - Saídas de paginação e de params lidas com Read.
  - Texto do README.md:191-197 lido.
  - enabled_modules sem release-ops, como no handoff anterior.

ASSUNCOES:
  - O comando, o exit 0 e o ambiente (Postgres 16 do digest do CI, trust) vêm do relato da sessão principal. Conferi o JSON produzido, mas não executei nada.
  - O shim de pm reproduz a semântica do Postman para pm.environment set/unset.

ARQUIVOS:
  - Nenhum alterado (o QA só lê).

RISCOS:
  - A coleção é cópia manual das rotas (D-22): rota nova diverge sem aviso.
  - O token no environment versionado é barrado só pela revisão (achado BAIXO do security-review).
  - O sensor local `test` continua pulando os testes de banco quando falta BIBLIOTECA_TEST_DATABASE_URL. O CI cobre isso com BIBLIOTECA_TEST_DB_OBRIGATORIO=1 (D-21).

SINAIS:
  SECURITY: sim — já revisado em 03-security-review, sem bloqueante; nada novo.
  SUPPLY_CHAIN: nao — sem go.mod nem go.sum.
  MIGRATION: nao — sem SQL.
  PERFORMANCE: nao — sem código.

PROXIMO: docs-update
MOTIVO: Não há achado bloqueante e release-ops não está em enabled_modules. O docs-update deve:
  - pôr QA Resultado = ok, com a evidência: suíte com banco em 3019 pass, 0 fail, 0 skip; coleção 15/15 ao vivo; paginação e params ao vivo nas 4 listagens;
  - marcar o checklist;
  - passar o status a "em revisão" no report e no INDEX;
  - revalidar;
  - opcionalmente, aplicar a sugestão da frase sobre o cursor `apos` em README.md:194-196.
