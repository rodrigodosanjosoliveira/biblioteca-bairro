DELIVERY_ID: DEL-017
TASK_CODE: PROJ-7
AGENT: pm-triage
STATUS: bloqueado

TIPO: feature (comportamento novo: dado de disponibilidade que a API não expõe hoje; se P1 = opção A, vira feature pequena sem mudança de modelo)

MODULOS_IMPACTADOS (pelo caminho):
  - internal/http/ (handler.go: saidaLivro, obterLivro, listarLivros) — superfície de segurança
  - internal/storage/ (repositorio.go: BuscarLivro, ListarLivros/listarPorID) — superfície de segurança, consulta nova
  - internal/domain/ (livro.go; emprestimo.go se P1 = B ou C)
  - migrations/ (só se P1 = B ou C) — superfície de segurança, migration-reviewer habilitado
  - postman/ (coleção: resposta de GET /livros e GET /livros/{id} muda; D-22)
  - docs-ai/ (DECISOES.md: decisão nova; 03-MAPA-RAPIDO-MODULOS.md)

ESCOPO:
  - Expor, por livro, a contagem de exemplares (total e disponíveis) em GET /livros/{id} e em cada item de GET /livros (recomendação de P2).
  - Contagem calculada na consulta, sem coluna de contador (recomendação de P3), numa única consulta por página (sem N+1).
  - O modelo de exemplar que a contagem pressupõe, conforme a resposta de P1.
  - Testes de tabela em domain/storage/http, com banco (BIBLIOTECA_TEST_DATABASE_URL); coleção Postman atualizada.

FORA_DE_ESCOPO:
  - Reserva, fila de espera ou notificação de disponibilidade.
  - Filtro/ordenação de GET /livros por disponibilidade (ex.: ?disponivel=true).
  - Contador armazenado/desnormalizado e gatilhos (triggers).
  - CRUD completo de exemplar (baixa, extravio, transferência), além do mínimo que P1 exigir para criar exemplares.
  - Mudança em multa (D-09/D-10/D-14), renovação (D-17) e no contrato de paginação (D-19).
  - Total geral do acervo ou contagem agregada fora do livro.

CONFIRMADO:
  - "Exemplar" hoje = a própria linha de `livro`: não existe tabela de exemplar nem campo de quantidade. /home/rodrigo/workspace/biblioteca-bairro/migrations/0001_esquema_emprestimo.up.sql (livro só tem id e titulo); /home/rodrigo/workspace/biblioteca-bairro/internal/domain/livro.go:11-15 ("Livro é um exemplar do acervo", só ID e Titulo); /home/rodrigo/workspace/biblioteca-bairro/docs-ai/DECISOES.md:125 (D-12: "`Livro` é o exemplar físico, só com `Titulo`").
  - Migrações 0002 e 0003 não alteram `livro`: /home/rodrigo/workspace/biblioteca-bairro/migrations/0002_renovacao_emprestimo.up.sql (só emprestimo.renovacoes), /home/rodrigo/workspace/biblioteca-bairro/migrations/0003_indices_listagem_emprestimo.up.sql (só índices de emprestimo).
  - Empréstimo ativo = linha de `emprestimo` com `devolucao IS NULL` (status derivado, não gravado; D-12). No máximo um aberto por livro, garantido pelo índice único parcial `emprestimo_livro_aberto_uq ON emprestimo (livro_id) WHERE devolucao IS NULL` (0001, linha 28; R4), traduzido em `ErrLivroJaEmprestado` em /home/rodrigo/workspace/biblioteca-bairro/internal/storage/repositorio.go:28 e :535. Índice parcial `emprestimo_aberto_id_idx` em 0003.
  - Consequência: no modelo atual a "contagem de disponíveis por livro" só pode valer 0 ou 1 (total sempre 1). A demanda, lida ao pé da letra, pressupõe N exemplares por livro, o que contradiz a D-12 e a R4 — daí a P1 bloqueante.
  - Onde a contagem aparece: em lugar nenhum hoje. `saidaLivro` tem só id e titulo (/home/rodrigo/workspace/biblioteca-bairro/internal/http/handler.go:220-223), usada em criarLivro (:286), listarLivros (:373) e obterLivro (:512). Rotas em handler.go:137-144: não há endpoint de disponibilidade; `GET /livros/{id}/emprestimo` (:142) devolve o empréstimo aberto, não contagem.
  - Calculada ou armazenada: não existe nada armazenado; tudo que é status no projeto é derivado (D-12, "status derivado"). A recomendação de calcular na consulta segue esse padrão.
  - Repositório: BuscarLivro (/home/rodrigo/workspace/biblioteca-bairro/internal/storage/repositorio.go:108) e ListarLivros (:130, keyset pela livro_pkey via listarPorID, D-19/D-20) leem só `livro`; incluir a contagem muda essas consultas.
  - Superfície de segurança: internal/http/, internal/storage/ e migrations/ estão em security_surface_paths (/home/rodrigo/workspace/biblioteca-bairro/.pipeline-claude.json:41-55); delivery criada com require_security. enabled_modules = data-model e migration-reviewer.

PERGUNTAS_BLOQUEANTES (uma por decisão; a P1 bloqueia o desenho, P2-P4 têm default recomendado e só bloqueiam se o usuário discordar):
  - P1 — Modelo de exemplar. Hoje 1 livro = 1 exemplar físico (D-12), então a contagem é 0/1. Opções:
      A) Manter o modelo: expor `disponivel` (bool) ou `exemplares_disponiveis` 0/1 derivado do empréstimo aberto. Sem migração; pequeno. Não permite vários exemplares do mesmo título.
      B) Separar obra e exemplar: tabela nova `exemplar` (FK para livro), `emprestimo` passa a apontar para o exemplar, R4 vira "um aberto por exemplar"; backfill de 1 exemplar por livro existente; `POST /livros/{id}/emprestimo` escolhe um exemplar livre no servidor (contrato HTTP mantido) e uma rota mínima para cadastrar exemplares. Grande: migração com dados, revisão da D-12/D-13, concorrência na escolha do exemplar.
      C) Coluna `livro.quantidade` (inteiro >= 1) e R4 trocada por "abertos < quantidade". Perde a identidade do exemplar físico e troca uma garantia do banco (índice único) por verificação sob concorrência (lock/serialização).
    Recomendação: B se a biblioteca de fato tem cópias repetidas do mesmo título (é o modelo correto e preserva R4 no banco por exemplar), dividindo em duas deliveries — DEL-017 com modelo + migração + backfill + empréstimo por exemplar, e a contagem na seguinte — ou A se a necessidade real é só "está disponível?". Evitar C.
  - P2 — Onde a contagem aparece. Recomendação: campos novos em GET /livros/{id} e em cada item de GET /livros (`exemplares_total` e `exemplares_disponiveis`; em A, só `disponivel`), sem endpoint novo. Campo aditivo na resposta, sem quebrar clientes; POST /livros devolve o mesmo DTO.
  - P3 — Calculada ou armazenada. Recomendação: calculada na consulta (agregação/LEFT JOIN sobre empréstimos abertos, atendida por índice parcial), uma consulta por página, sem contador gravado — segue o "status derivado" da D-12 e não cria divergência sob concorrência.
  - P4 — Definição de "disponível". Recomendação: exemplar sem empréstimo aberto (`devolucao IS NULL`); empréstimo vencido e não devolvido conta como indisponível. Não há reserva nem baixa no modelo, então nada mais subtrai.

CRITERIOS_DE_ACEITE (medíveis; ajustar após P1):
  - CA1: GET /livros/{id} devolve 200 com `exemplares_total` e `exemplares_disponiveis` (inteiros, 0 <= disponiveis <= total); em A, `disponivel` bool.
  - CA2: cada item de GET /livros traz os mesmos campos; paginação (limite, apos, proximo) e ordenação por id inalteradas (D-19); testes de listagem existentes passam sem alteração de expectativa além dos campos novos.
  - CA3: abrir empréstimo decrementa `exemplares_disponiveis` em 1; registrar devolução incrementa em 1; renovação não altera; empréstimo vencido não devolvido conta como indisponível — teste de banco em tabela cobrindo os quatro casos.
  - CA4: livro sem nenhum empréstimo → disponíveis = total; livro inexistente → 404 como hoje.
  - CA5: a listagem executa uma única consulta por página para livros + contagem (sem N+1), conferido em teste ou no EXPLAIN registrado no report, usando índice existente ou novo.
  - CA6 (se B ou C): `migrate up` aplica em banco com dados existentes, backfill deixa total = 1 por livro existente e empréstimos abertos preservados; R4 continua garantida no banco (teste de violação concorrente/sequencial devolve o mesmo 409).
  - CA7: coleção Postman em /home/rodrigo/workspace/biblioteca-bairro/postman atualizada com os campos novos; decisão registrada em docs-ai/DECISOES.md.
  - CA8: go build, go vet, go test ./... (com banco) e bin/pipeline-claude sensors verdes.

ASSUNCOES:
  - "Exemplar" na demanda significa cópia física de um mesmo título; o código usa "livro" para a cópia física (D-12). Não confirmado com o usuário — é a P1.
  - "Disponível" = sem empréstimo aberto; vencido não devolvido é indisponível (P4).
  - A contagem vai nos endpoints de leitura de livro existentes, não em endpoint novo (P2).
  - Calculada na consulta, não armazenada (P3).
  - Clientes da API toleram campo aditivo no JSON de livro.
  - PROJ-7 citada na demanda como referência externa; registrada em ref: sem consulta ao gestor.

ARQUIVOS:
  - docs-ai/deliveries/DEL-017/report.md — criado pela ferramenta (ref PROJ-7, require_security, escopo fechado)
  - docs-ai/deliveries/INDEX.md — linha da DEL-017 registrada pela ferramenta

RISCOS:
  - P1 = B: migração com dados (nova tabela, coluna nova em emprestimo, backfill, troca do índice único emprestimo_livro_aberto_uq por um por exemplar) — ordem de deploy migrate-antes-do-binário (D-16/D-17), lock em emprestimo durante a troca de índice, e down destrutivo que exige justificativa.
  - P1 = B: escolha concorrente do exemplar livre em POST /livros/{id}/emprestimo pode colidir; precisa de retry sobre o índice único ou SELECT ... FOR UPDATE SKIP LOCKED.
  - P1 = C: perde a garantia de R4 no banco; verificação "abertos < quantidade" é sujeita a corrida sem lock explícito.
  - Contagem na listagem sem agregação única gera N+1 (até 100 consultas por página).
  - Mudança no DTO saidaLivro afeta também POST /livros (mesmo DTO) — decidir se a criação devolve os campos.
  - Escopo pode estourar a delivery se B for escolhida sem a divisão recomendada.

SINAIS:
  SECURITY: sim — toca internal/http/ e internal/storage/ (e migrations/ em B/C), todos em security_surface_paths; require_security ligada.
  SUPPLY_CHAIN: nao — nenhuma dependência nova prevista; stdlib e pgx já presentes.
  MIGRATION: sim — certa em B/C (tabela/coluna e troca de índice) e possível índice novo para a agregação mesmo em A; na dúvida, sim.
  PERFORMANCE: sim — agregação por livro em GET /livros (até 100 itens por página) com risco de N+1 e de plano ruim sem índice adequado.

PROXIMO: usuário
MOTIVO: P1 decide o modelo de dados e muda o tamanho da entrega (A sem migração; B com migração, backfill e mudança de R4). Sem ela o architecture-guardrails desenharia sobre suposição contrária à D-12. Respondida a P1 (e confirmadas ou ajustadas P2-P4), seguir para architecture-guardrails (depois data-model e migration-reviewer, se B/C).
