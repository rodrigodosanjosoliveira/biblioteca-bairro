DELIVERY_ID: DEL-013
TASK_CODE: DEL-013
AGENT: pm-triage
STATUS: ok

TIPO: feature

ESCOPO:
  - GET /livros: listagem paginada de livros, autenticada (Bearer, mesma cadeia das rotas atuais).
  - GET /livros/{id}: detalhe de um livro (incluído porque a listagem sem detalhe fica incoerente e o custo é mínimo; ver ASSUNCOES).
  - GET /emprestimos/{id}: detalhe de um empréstimo por ID, aberto ou devolvido, autenticado.
  - Consultas de leitura novas em internal/storage/repositorio.go (listar livros por página, buscar empréstimo por ID) e métodos correspondentes na interface Repositorio de internal/http/handler.go.
  - Novos códigos de erro em internal/http/erros.go, se necessários, no padrão da D-15 (código fixo + mensagem fixa).
  - Registro do contrato de paginação como decisão nova em docs-ai/DECISOES.md (docs-update) e atualização do mapa de módulos.
  - Inclui a mudança não commitada do working tree que marca a DEL-012 como merged (docs-ai/deliveries/INDEX.md e docs-ai/deliveries/DEL-012/report.md), como nas entregas anteriores.

FORA_DE_ESCOPO:
  - Filtros (por título, disponibilidade), busca textual e ordenação configurável pelo cliente.
  - Listagem de usuários, GET /usuarios/{id}, listagem de empréstimos (por livro, por usuário, abertos, vencidos).
  - Indicar na listagem de livros se o livro está emprestado (exigiria junção com emprestimo; fica para delivery futura).
  - Paginação por offset/página numerada e contagem total de itens.
  - Migração, índice novo ou mudança de esquema (a PK livro_pkey atende o keyset por id; emprestimo_pkey atende o detalhe).
  - Mudança em internal/domain/ (as entidades e ReidratarEmprestimo já existem).
  - Autenticação por usuário/escopos, rate limiting.
  - Atualização do pipeline-claude v1.1.0-rc.2 (Makefile, .pipeline-claude.lock, .claude/agents/): PR separado, feito pelo usuário fora da sessão; não entra no commit desta delivery.

CONFIRMADO:
  - Rotas atuais em internal/http/handler.go:127-134: só GET /livros/{id}/emprestimo como leitura de negócio; não existem GET /livros, GET /livros/{id} nem GET /emprestimos/{id}.
  - internal/storage/repositorio.go: já há BuscarLivro(ctx, id) (linha 94) e BuscarEmprestimoAbertoPorLivro (linha 267); não há busca de empréstimo por ID nem listagem.
  - migrations/0001_esquema_emprestimo.up.sql: livro.id e emprestimo.id são bigint identity com PK (livro_pkey, emprestimo_pkey); keyset "WHERE id > $1 ORDER BY id LIMIT $2" é atendido pela PK, sem índice novo.
  - docs-ai/DECISOES.md:358 (D-15, Consequências): "GET /emprestimos/{id}, listagens e paginação ficam para deliveries futuras" — esta delivery cumpre essa pendência.
  - D-15 (docs-ai/DECISOES.md:282-291): erro JSON com codigo e mensagem fixa; 400 sintaxe, 422 semântica; 404 para recurso inexistente; instantes RFC 3339 UTC, vencimento AAAA-MM-DD, multa string StringFixed(2), IDs como número.
  - .pipeline-claude.json: internal/http/ e internal/storage/ estão em security_surface_paths; enabled_modules = data-model, migration-reviewer (query-performance não habilitado).

ASSUNCOES:
  - Formato da paginação (não definido na demanda): keyset/cursor por id ascendente, não offset. Padrão proposto: query params `limite` e `apos`. `apos` = último id da página anterior (exclusivo, inteiro > 0, opcional; ausente = início). Motivo: estável sob inserções, sem custo de OFFSET, atendido pela PK, sem cursor opaco (IDs já são expostos e sequenciais, risco aceito na D-15).
  - Limites: `limite` inteiro de 1 a 100, padrão 20.
  - Ordenação: fixa por id ascendente (ordem de cadastro); não configurável.
  - Formato da resposta de GET /livros: `{"itens":[{"id":1,"titulo":"..."}], "proximo": <id>|null}`, onde `proximo` é o valor a passar em `apos` para a próxima página e null quando não há mais itens (detectado buscando limite+1). Nomes em português, coerentes com o contrato existente (codigo, renovacoes). Sem total.
  - Campos de livro: id e titulo (os únicos do domínio, D-12), mesmo DTO da resposta de POST /livros.
  - GET /emprestimos/{id}: mesmo DTO de GET /livros/{id}/emprestimo (incluindo renovacoes e multa), para aberto ou devolvido; multa de empréstimo aberto calculada com o relógio injetado como referência (D-14), de devolvido pela data de devolução. O DTO não ganha dados do usuário além de usuario_id (nome é dado pessoal, D-12).
  - GET /livros/{id} entra no escopo (decisão de triagem, não do usuário). Se o usuário preferir não incluir, o recorte é removê-lo sem afetar o resto.
  - Códigos de erro: id de path inválido → 400 `id_invalido` (existente); `limite`/`apos` malformado ou fora da faixa → 400 com código novo fixo (proposta: `parametro_invalido`); parâmetro de query desconhecido ou repetido → 400 `parametro_invalido` (coerente com DisallowUnknownFields); livro inexistente → 404 `livro_nao_encontrado` (existente); empréstimo inexistente → 404 com código novo (proposta: `emprestimo_nao_encontrado`). Página vazia → 200 com itens [] e proximo null.
  - Listagem lida só da tabela livro; sem transação nem snapshot entre páginas (consistência eventual entre páginas é aceitável).
  - Nenhuma destas assunções foi considerada genuinamente decisão humana: são extensões do contrato da D-15 e reversíveis; architecture-guardrails pode revisar e registrar como decisão.

ARQUIVOS:
  - docs-ai/deliveries/DEL-013/report.md — criado por pipeline_delivery_new (require_security: true, escopo fechado).
  - docs-ai/deliveries/INDEX.md — linha da DEL-013 registrada pela ferramenta.

RISCOS:
  - Enumeração: GET /livros e GET /emprestimos/{id} facilitam enumerar todo o acervo e empréstimos a quem tem o token (já aceito na D-15 para operador único); a listagem amplia o volume por requisição, por isso o teto de 100.
  - Validação de query string é entrada externa nova: parsing de inteiro (sinal, overflow de int64, zeros à esquerda, espaços, valores repetidos) precisa de teste em tabela e fuzz (gate de fuzz do CLAUDE.md).
  - Log: a D-15 proíbe logar query string; o middleware de Log não deve passar a registrar `limite`/`apos` nem a URL.
  - Interface Repositorio cresce: os fakes de teste de internal/http precisam implementar os novos métodos.
  - Reutilizar o DTO de empréstimo exige que o detalhe de devolvido calcule multa sem ref (Multa já ignora ref quando devolvido, D-14); erro aqui geraria multa errada.
  - INDEX.md também foi tocado pelo index_sync (DEL-012 merged); commit conjunto, sem misturar arquivos da atualização rc.2.

SINAIS:
  SECURITY: sim — toca internal/http/ e internal/storage/ (security_surface_paths): rotas autenticadas novas, entrada externa nova por query string, novas consultas SQL e novos códigos de erro.
  SUPPLY_CHAIN: nao — stdlib e pgx já presentes cobrem; go.mod/go.sum não devem mudar.
  MIGRATION: nao — PKs existentes atendem keyset e detalhe; nenhuma mudança de esquema prevista.
  PERFORMANCE: sim — consulta de listagem nova (keyset com LIMIT, teto de 100); conferir plano pela PK e ausência de N+1 (query-performance não habilitado; fica com architecture-guardrails e code-reviewer).

PROXIMO: architecture-guardrails
MOTIVO: tipo feature; definir/ratificar o contrato de paginação e erros proposto em ASSUNCOES, as assinaturas novas do repositório e da interface Repositorio, antes do test-writer. data-model não é necessário (sem mudança de esquema).
