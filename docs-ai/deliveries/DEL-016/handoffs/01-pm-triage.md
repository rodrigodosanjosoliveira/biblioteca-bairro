DELIVERY_ID: DEL-016
TASK_CODE: DEL-016
AGENT: pm-triage
STATUS: ok

TIPO: docs (artefato de consumo da API, sem mudança de comportamento nem de código Go)

ESCOPO:
  - Coleção Postman (formato v2.1.0) cobrindo as 15 rotas registradas em internal/http/handler.go:135-149:
    GET /healthz, GET /readyz (sem auth);
    POST /livros, GET /livros (limite, apos), GET /livros/{id};
    POST /usuarios, GET /usuarios (limite, apos), GET /usuarios/{id}, GET /usuarios/{id}/emprestimos (situacao, livro_id, limite, apos);
    POST /livros/{id}/emprestimo, GET /livros/{id}/emprestimo, POST /livros/{id}/devolucao, POST /livros/{id}/renovacao (corpo vazio);
    GET /emprestimos (situacao=aberto|devolvido|vencido, usuario_id, livro_id, limite, apos), GET /emprestimos/{id}.
  - Autenticação Bearer no nível da coleção ({{token}}), com healthz/readyz marcados "No Auth".
  - Environment versionado só com placeholders: base_url (ex.: http://localhost:8080, que é o padrão de ADDR), token com valor VAZIO, e variáveis de id (livro_id, usuario_id, emprestimo_id) e de cursor.
  - Pastas por recurso (Saúde, Livros, Usuários, Empréstimos), descrição curta por request com os códigos de erro relevantes (400 parametro_invalido, 404, 409 emprestimo_vencido / limite_renovacoes_atingido, 422 usuario_nao_encontrado) tirados de internal/http/erros.go.
  - Seção curta no README.md explicando importar, preencher token só no "current value"/local e não exportar o environment com o token.
  - Alterações locais já pendentes em docs-ai/deliveries/DEL-015/report.md e docs-ai/deliveries/INDEX.md (marcação merged da DEL-015 pelo pipeline_index_sync) entram nesta delivery, como nas anteriores.

FORA_DE_ESCOPO:
  - Qualquer mudança em cmd/, internal/, migrations/, go.mod, go.sum, Makefile (rotas, contrato JSON, auth, middlewares).
  - Especificação OpenAPI/Swagger, interface gráfica, Newman ou execução da coleção no CI.
  - Token real, DSN, ou qualquer segredo em arquivo versionado (invariante 5).
  - Mudança em .claude/, .mcp.json, .pipeline-claude.json (no_agent_paths).

CONFIRMADO:
  - Rotas: internal/http/handler.go linhas 135-149 (15 rotas; healthz/readyz sem auth, demais com auth Bearer).
  - Contrato de listagem e filtros: docs-ai/03-MAPA-RAPIDO-MODULOS.md linhas 59-64 (D-19, D-20, D-21): limite 1..100 padrão 20, apos, resposta {itens, proximo}; chave de query desconhecida ou repetida → 400; /usuarios/{id}/emprestimos recusa usuario_id com 400.
  - Token: internal/config/config.go (BIBLIOTECA_API_TOKEN, 32 a 512 bytes ASCII visível); ADDR padrão :8080 (mapa, linha 48).
  - Não existe coleção Postman no repo (Glob **/*.postman* sem resultado); não existe diretório docs/.
  - .gitignore ignora .env e .env.* mas NÃO ignora *.postman_environment.json: um environment com token preenchido seria versionado sem aviso.
  - Nenhum caminho previsto está em protected_paths nem em security_surface_paths (.pipeline-claude.json); docs-update é editor e pode gravar.

ASSUNCOES:
  - "Templates" = coleção Postman + environment (não scripts de carga nem OpenAPI).
  - Formato Postman Collection v2.1.0 (schema https://schema.getpostman.com/json/collection/v2.1.0/collection.json), importável no Postman atual e no Newman.
  - Local proposto: diretório postman/ na raiz (biblioteca-bairro.postman_collection.json e biblioteca-bairro.postman_environment.json). Pendente de confirmação humana (P1).
  - Sem testes/scripts Postman por padrão, exceto, se o humano aceitar, um test script mínimo que grava livro_id/usuario_id/emprestimo_id/proximo nas variáveis a partir das respostas 201/200 (P3).
  - Corpos de exemplo usam dados fictícios genéricos (título de livro, nome "Usuário Exemplo"); nome é dado pessoal (D-12), então nada real.
  - O environment versionado é um template; o usuário cria uma cópia local com o token. Adicionar padrão no .gitignore para essa cópia é desejável (P2).
  - Corpo exato de POST /livros, /usuarios e /livros/{id}/emprestimo será lido do handler pelo docs-update (não conferido aqui; triage não lê o código inteiro).

ARQUIVOS:
  - docs-ai/deliveries/DEL-016/report.md — criado pela pipeline_delivery_new (require_security: true)
  - docs-ai/deliveries/INDEX.md — linha da DEL-016 registrada
  - Previstos para docs-update: postman/biblioteca-bairro.postman_collection.json, postman/biblioteca-bairro.postman_environment.json, README.md (seção Postman), .gitignore (opcional, P2), docs-ai/03-MAPA-RAPIDO-MODULOS.md (linha de estrutura para postman/)

RISCOS:
  - Vazamento de token: Postman exporta environment com "current value" se o usuário preencher e reexportar por cima do arquivo versionado; .gitignore atual não protege *.postman_environment.json.
  - Divergência futura: coleção é cópia manual das rotas; novas rotas exigem atualizar a coleção (registrar no checklist do docs-update / invariante opcional).
  - Corpo ou nome de campo errado na coleção (ex.: livro_id vs usuario_id no POST de empréstimo) só aparece ao usar; QA deve conferir cada request contra handler.go.
  - Query com chave repetida ou desconhecida dá 400: params desabilitados no Postman não são enviados, mas params habilitados vazios são; deixar os opcionais desabilitados por padrão.

SINAIS:
  SECURITY: sim — environment pode carregar BIBLIOTECA_API_TOKEN (invariante 5); exige revisão de que nenhum valor real/secreto foi versionado e do padrão do .gitignore. Flag require_security ligada à mão (caminhos não estão em security_surface_paths).
  SUPPLY_CHAIN: nao — sem go.mod/go.sum nem dependência nova.
  MIGRATION: nao — sem SQL nem esquema.
  PERFORMANCE: nao — sem consulta nem código.

PERGUNTAS_AO_HUMANO:
  - P1 Local dos arquivos: (a) postman/ na raiz [RECOMENDADO: visível, fora de protected_paths]; (b) docs-ai/postman/; (c) docs/postman/.
  - P2 Proteção do token: (a) environment versionado só com token vazio + entrada no .gitignore para cópia local (ex.: postman/*.local.postman_environment.json) [RECOMENDADO]; (b) só token vazio, sem mudar .gitignore.
  - P3 Scripts Postman: (a) só test scripts mínimos que capturam ids e o cursor "proximo" para encadear requests, sem asserções [RECOMENDADO]; (b) nenhum script; (c) scripts com asserções de status/contrato (vira mini-suíte, mais manutenção).
  - P4 Exemplos de resposta salvos (examples) por request: (a) não [RECOMENDADO, evita divergência]; (b) sim, para 2xx e erros principais.

PROXIMO: docs-update
MOTIVO: tipo docs puro (artefatos JSON + README, nenhum caminho protegido nem código Go), roteamento direto para docs-update após respostas P1-P4. Como SECURITY: sim, a sessão deve passar por security-review (conferir environment e .gitignore sem segredo) antes de qa-validation/git-delivery.
