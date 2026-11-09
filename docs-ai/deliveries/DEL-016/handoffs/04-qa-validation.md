DELIVERY_ID: DEL-016
TASK_CODE: DEL-016
AGENT: qa-validation
STATUS: ok

RESULTADO: parcial. A coleção cobre o escopo e foi executada de ponta a ponta contra a API real com Postgres. O sensor `test` rodou sem BIBLIOTECA_TEST_DATABASE_URL e pulou 220 testes, entre eles os da camada de banco (internal/storage, internal/migrate, internal/http/banco_*, cmd/.../migrate_correcao_test.go). Pela regra do QA, pulo na camada de banco dá `parcial`. A delivery não toca código Go nem banco: o diff não tem nada em cmd/, internal/, migrations/, go.mod ou go.sum.

GATES:
  - pipeline_sensors (todos), saída real: ok=true
    - vet: ok, exit 0, saída vazia
    - test: ok, exit 0, "pacotes: 8 (8 ok, 0 com falha, 0 sem testes) / testes: 2386 passaram, 0 falharam, 220 pulados"; skipped=220. Todos os pulos que aparecem na saída (truncada) têm o motivo "BIBLIOTECA_TEST_DATABASE_URL não definida: teste de banco pulado". Exemplos: TestMigracaoUpDownUp e TestEsquemaRestricoes (internal/storage/esquema_test.go:316,349); TestControleCatalogo, TestControleUpEIdempotente e TestControleDown (internal/migrate/controle_banco_test.go); TestBancoFluxoCompleto, TestBancoRenovacao, TestBancoListagemEmprestimosFiltros e TestBancoListagemUsuarios (internal/http/banco_*_test.go); TestMigrateAceitaParametrosDePoolNaDSN/* (cmd/biblioteca-bairro/migrate_correcao_test.go:304).
    - build: ok, exit 0
    - vuln: ok, exit 0, "No vulnerabilities found."
    - mod_verify: ok, exit 0, "all modules verified"
  - Fuzz: não se aplica (nenhum alvo de fuzz nem código Go tocado).
  - Evidência da sessão principal (este subagente não tem shell; relato da sessão, com saída lida em /tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/qa016-run.txt). Execução da coleção pelo executor /tmp/claude-1000/-home-rodrigo-workspace-biblioteca-bairro/167a368f-e5d8-4557-a378-50174de69ca4/scratchpad/run016.mjs (lido: lê a coleção e o environment versionados, substitui {{var}}, aplica o bearer herdado e o noauth, envia só os query params habilitados, roda os scripts "test" com um shim de pm.environment). A API veio do binário da main, com Postgres 16 e migrate up 0001-0003. Resultado: 15/15 OK, "falhas=0 env final ids: 1 1 1". healthz e readyz 200; POST /livros 201; GET /livros 200; GET /livros/1 200; POST /usuarios 201; GET /usuarios 200; GET /usuarios/1 200; GET /usuarios/1/emprestimos 200; POST /livros/1/emprestimo 201; GET /livros/1/emprestimo 200; POST /livros/1/renovacao 200 com renovacoes=1 e vencimento de 2026-11-23 para 2026-12-07; POST /livros/1/devolucao 200; GET /emprestimos 200; GET /emprestimos/1 200. Com token vazio, healthz e readyz deram 200 e as rotas protegidas 401 nao_autorizado. O token teve 0 ocorrências no log da API. `jq empty` passou nos dois JSON. `git check-ignore` casa a cópia .local com a regra .gitignore:25.

COBERTURA_DO_ESCOPO:
  - 15 rotas de handler.go:135-149 → 15 requests na coleção. Método e caminho conferidos um a um (abaixo) e todos exercitados ao vivo (run 15/15).
  - Bearer {{token}} na coleção, noauth em healthz e readyz → coleção linhas 8-17, 26-28 e 43-45. Na execução ao vivo, token vazio deu 401 nas protegidas e 200 nas de saúde.
  - Environment só com placeholders → environment linhas 5-58: base_url http://localhost:8080; token "" do tipo secret; livro_id, usuario_id, emprestimo_id e os 4 cursores vazios. Os ids foram encadeados pelos scripts no run (1, 1, 1).
  - Pastas e descrições com os códigos de erro → pastas Saúde, Livros, Usuários e Empréstimos. Os códigos das descrições existem todos em internal/http/erros.go:17-41, com o status certo (catalogo, linhas 60-82). usuario_inexistente (404) está só em GET /usuarios/{id} e em /usuarios/{id}/emprestimos, como em handler.go:415-419 e 460-464. usuario_nao_encontrado (422) fica no POST de empréstimo. emprestimo_nao_encontrado vai em GET /emprestimos/{id} (handler.go:525-529) e emprestimo_aberto_nao_encontrado nas rotas por livro.
  - Seção Postman no README → diff README.md:171-197 (copiar para .local, importar, token só no current value, ordem sugerida, params desligados).
  - .gitignore (P2) → diff .gitignore +postman/*.local.postman_environment.json. check-ignore confirmado pela sessão principal.
  - Status merged da DEL-015 no report e no INDEX → diff docs-ai/deliveries/DEL-015/report.md e INDEX.md (só status, mais a linha da DEL-016 em draft).
  - "Teste" desta delivery: não há teste Go, porque não há código. O equivalente é a execução da coleção contra a API real e a conferência estática abaixo. Os query params opcionais não rodaram ao vivo (vêm desligados); só a conferência estática os cobre.

CONFERENCIA_REQUEST_A_REQUEST (coleção x internal/http/handler.go):
  - GET /healthz e GET /readyz (handler.go:135-136): batem, noauth.
  - POST /livros (137): corpo {"titulo"} = entradaLivro (207-209). Bate.
  - GET /livros (143): query limite e apos = lerParametrosListagem (654-658, 716-728), que aceita só essas duas. Bate.
  - GET /livros/{id} (144): path livros/{{livro_id}}. Bate.
  - POST /usuarios (138): corpo {"nome"} = entradaUsuario (211-213). Bate.
  - GET /usuarios (146): limite e apos. Bate.
  - GET /usuarios/{id} (147): bate.
  - GET /usuarios/{id}/emprestimos (148): query situacao, livro_id, limite e apos. Com aninhada=true, usuario_id é recusado (handler.go:685-688), e a coleção não o inclui. Bate.
  - POST /livros/{id}/emprestimo (139): corpo {"usuario_id": {{usuario_id}}}, número sem aspas = entradaEmprestimo UsuarioID int64 (215-217). Bate.
  - GET /livros/{id}/emprestimo (142): bate.
  - POST /livros/{id}/renovacao (141) e POST /livros/{id}/devolucao (140): sem body nem Content-Type, compatível com exigirCorpoVazio (766-779). Bate.
  - GET /emprestimos (149): query situacao (aberto|devolvido|vencido, 669-680), usuario_id, livro_id, limite e apos. Bate.
  - GET /emprestimos/{id} (145): bate.
  - Nenhum request a mais e nenhuma rota sem request: 15 = 15.

SCOPE_CREEP:
  - Nenhum. O diff (pipeline_diff, 12 arquivos) não toca cmd/, internal/, migrations/, go.mod, go.sum, Makefile, .claude/, .mcp.json nem .pipeline-claude.json. Não há OpenAPI nem Newman/CI, e nenhum token ou DSN está versionado. A D-22 em DECISOES.md e a linha de postman/ no mapa são documentação prevista nos ARQUIVOS do triage.

BORDAS:
  - Valor vazio em param habilitado: parseID("") e ParseInt("") falham → 400. Os params vêm desligados e a descrição avisa.
  - Cursor apagado pelo script (unset) com "apos" ligado: o Postman manda "{{cursor_*}}" literal, url.ParseQuery aceita e parseID recusa → 400 parametro_invalido. Documentado na descrição da coleção.
  - Abrir empréstimo com usuario_id vazio gera o corpo `{"usuario_id": }`, JSON inválido → 400 corpo_invalido. A ordem sugerida evita o caso; registrado pelo docs-update.
  - Renovação e devolução sem corpo: o Postman não manda body. Exercitado ao vivo (200).
  - Script de cursor: trata proximo null/undefined com unset. Ao vivo, proximo=null nas 4 listagens, e esse ramo rodou.
  - Ramo set do cursor (proximo não nulo) não rodou ao vivo: com 1 registro e limite padrão 20 não há segunda página.

ACHADOS:
  - Nenhum bloqueante.
  - Cosmético: a descrição de "Abrir empréstimo" não cita 413 corpo_muito_grande, embora o corpo passe por lerCorpo. A descrição da coleção cita o 413 de forma geral. Não exige ação.

CONFIRMADO:
  - Sensores, diff, conferência request a request e evidência ao vivo, como acima.
  - enabled_modules = ["data-model", "migration-reviewer"] (.pipeline-claude.json:39): release-ops não está habilitado.

ASSUNCOES:
  - A execução da coleção e os resultados de check-ignore, jq e grep no log vêm do relato da sessão principal e do arquivo de saída lido. Este subagente não executou comandos de shell.
  - O executor Node reproduz a semântica do Postman nos pontos usados (herança de auth, params desligados não enviados, substituição de variáveis). O Postman real não foi aberto.
  - O status 404 livro_nao_encontrado no POST de empréstimo vem de classificar (erros.go:114). Não li o repositório para confirmar que AbrirEmprestimo devolve ErrLivroNaoEncontrado.

ARQUIVOS:
  - nenhum alterado (QA só lê).

RISCOS:
  - Camada de banco não provada pelo sensor: 220 testes pulados sem BIBLIOTECA_TEST_DATABASE_URL. A delivery não muda código de banco e o CI roda com Postgres e BIBLIOTECA_TEST_DB_OBRIGATORIO=1 (D-21). Mesmo assim, o sensor local não prova a camada de banco.
  - Os query params opcionais e o ramo de cursor não nulo não rodaram ao vivo; a cobertura deles é só estática.
  - A coleção é cópia manual das rotas (D-22): rota nova diverge sem aviso.
  - O token no environment versionado é barrado só pela revisão (achado BAIXO do security-review).

SINAIS:
  SECURITY: sim — já revisado (03-security-review, sem bloqueante); nada novo no QA.
  SUPPLY_CHAIN: nao — sem go.mod nem go.sum.
  MIGRATION: nao — sem SQL.
  PERFORMANCE: nao — sem código.

PROXIMO: docs-update
MOTIVO: release-ops não está em enabled_modules e não há achado bloqueante. Falta o passe final do docs-update: marcar o checklist com a evidência (sensores verdes, escopo coberto pela execução da coleção e pela conferência estática, revisões concluídas), pôr QA Resultado = parcial com o motivo (220 testes de banco pulados no sensor local, delivery sem código de banco; coleção 15/15 ao vivo contra Postgres), passar o status a "em revisão" no report e no INDEX e revalidar. Se a sessão quiser `ok`, deve rodar o sensor test com BIBLIOTECA_TEST_DATABASE_URL (Postgres trust, como na memória do projeto) e repassar a saída ao QA.
