DELIVERY_ID: DEL-015
TASK_CODE: DEL-015
AGENT: architecture-guardrails
STATUS: ok

DECISOES_DO_USUARIO (confirmadas pela sessão principal; registrar na D-21 como U1/U2 da DEL-015):
  - U1 (era A3): `situacao=aberto` inclui os vencidos (`devolucao IS NULL`, como `Emprestimo.Aberto()`). `vencido` é subconjunto de `aberto`: aberto com vencimento ANTES da data civil de hoje em America/Sao_Paulo; o dia do vencimento não conta como vencido. `devolvido` = `devolucao IS NOT NULL`.
  - U2 (era A4): entra só `GET /usuarios/{id}/emprestimos`. `GET /livros/{id}/emprestimos` fica fora.

ABORDAGEM:
  Pacotes tocados: `internal/storage/`, `internal/http/`, `migrations/` (0003), docs (D-21 e mapa, via docs-update). `internal/domain/` NÃO muda.

  1. Storage — filtro e assinatura.
     - Tipo novo em `internal/storage/repositorio.go`: `FiltroEmprestimos{UsuarioID int64; LivroID int64; Situacao Situacao; Ref time.Time}`, com `Situacao` tipo próprio e constantes `SituacaoTodas` (zero), `SituacaoAberto`, `SituacaoDevolvido`, `SituacaoVencido`. Zero em UsuarioID/LivroID = sem filtro.
     - `ListarEmprestimos` muda de assinatura para `ListarEmprestimos(ctx, f FiltroEmprestimos, apos int64, limite int)` (um só ponto de entrada, sem método paralelo nem wrapper). Com filtro zero, o SQL gerado é byte a byte o de hoje (`... WHERE id > $1 ORDER BY id LIMIT $2`).
     - Revalidação antes de qualquer SQL, com `ErrListagemInvalida` (bug interno, 500 `bug_interno`, como D-19): UsuarioID < 0, LivroID < 0, Situacao fora das constantes, Situacao=Vencido com Ref zero.
  2. Storage — SQL combinável sem concatenar entrada.
     - Função pura não exportada (ex.: `sqlListarEmprestimos(f) (sql string, args []any)`) que monta o WHERE só com fragmentos CONSTANTES do código: `usuario_id = $N`, `livro_id = $N`, `devolucao IS NULL`, `devolucao IS NOT NULL`, `vencimento < $N`, unidos por ` AND `, sempre depois de `id > $1` e antes de `ORDER BY id LIMIT $2`. O número do placeholder é gerado pelo código (`strconv.Itoa(len(args)+3)`); os VALORES vão só em `args`. Ordem fixa dos fragmentos (usuario, livro, situação) para que cada combinação produza um texto SQL estável (o pgx cacheia um prepared statement por texto; são no máximo 2x2x4 = 16 textos).
     - `listarPorID` ganha parâmetro variádico no FIM: `listarPorID[T](ctx, db, sql, apos, limite, ler, extra ...any)`, que repassa `apos, limite+1, extra...` ao `Query`. `ListarLivros`/`ListarUsuarios` não mudam de chamada. O `+1`, o teto e a validação de apos/limite continuam só ali.
     - Função de montagem testável sem banco: teste de tabela com as 16 combinações conferindo texto e args (e que nenhum valor do filtro aparece no texto).
  3. `$hoje` e a regra de vencido.
     - A regra continua no domínio (`meiaNoiteUTC(ref) > meiaNoiteUTC(vencimento)`, usada por `Renovar` e, de forma equivalente, por `CalcularMulta` com dias > 0). O SQL é a tradução dela num ÚNICO ponto do storage.
     - O handler passa o instante (`agora`, lido uma vez) em `FiltroEmprestimos.Ref`; quem converte para data é o storage, com a MESMA função que já converte o vencimento na escrita: `vencimentoParaDate(ref)` (data civil SP em meia-noite UTC; o pgx codifica `date` por Y/M/D do valor). Coluna e parâmetro ficam no mesmo espaço (data civil SP), então `vencimento < $hoje` ⇔ `civilSP(ref) > civilSP(venc)` ⇔ vencido no domínio. Comentário no fragmento citando D-17/D-11 e esta equivalência.
     - Proibido no SQL: `now()`, `CURRENT_DATE`, `localtimestamp`, `AT TIME ZONE` (fuso da sessão não manda; relógio fixo nos testes precisa valer).
     - Barreira contra divergência (R3): teste com banco obrigatório que, para um conjunto de vencimentos em torno da fronteira (ontem, hoje, amanhã em SP) e relógios em 02:59:59Z e 03:00:00Z (23:59:59 e 00:00 SP) e 12:00 SP, confere que o conjunto devolvido por `situacao=vencido` é exatamente `{e aberto : e.Multa(agora) > 0}` e `{e aberto : e.Renovar(agora) == ErrEmprestimoVencido}` (em cópia). Teste de tabela unitário de `vencimentoParaDate` nos mesmos instantes.
  4. HTTP — parsing.
     - Um parser interno compartilhado de `limite`/`apos` (o corpo atual de `lerParametrosListagem`) e um parser novo para empréstimos, ex.: `lerParametrosEmprestimos(rawQuery string, aninhada bool) (filtro, apos, limite, ok)`, que aceita `situacao`, `livro_id` e (só se !aninhada) `usuario_id`. `lerParametrosListagem` continua aceitando SÓ `limite`/`apos` para `/livros` e `/usuarios`.
     - Mesmas regras da D-19: `url.ParseQuery` com erro tratado, chave repetida → 400, chave desconhecida → 400, `;` → 400. `situacao` por comparação exata com `aberto|devolvido|vencido`; vazio ou outro → 400. `usuario_id`/`livro_id` pelo `parseID`. Mensagem fixa `parametro_invalido`, sem eco.
     - Fuzz novo do parser de empréstimos (invariante 1.3), com seeds das chaves novas, propriedade: ok ⇒ valores no domínio válido (ids > 0, situação conhecida, limite 1..100).
  5. HTTP — rotas.
     - `GET /emprestimos`: parser novo (aninhada=false) → `agora := a.agora()` uma vez → `ListarEmprestimos(ctx, filtro{..., Ref: agora}, apos, limite)` → multa com o mesmo `agora` (A7). Restante idêntico ao handler atual.
     - `GET /usuarios/{id}/emprestimos` (novo, atrás do mesmo `auth(...)`): ordem `parseID(path)` → 400 `id_invalido`; parser (aninhada=true) → 400 `parametro_invalido`; um `context.WithTimeout` para as duas operações; `BuscarUsuario(ctx, id)` em TODA página → `ErrUsuarioNaoEncontrado` mapeado localmente a 404 `usuario_inexistente` (como `obterUsuario`); depois `ListarEmprestimos` com `UsuarioID` do path. Sem transação (consistência eventual da D-19; usuário não é apagado: FK `ON DELETE RESTRICT`, sem rota de exclusão). Reaproveitar o laço de montagem da página (extrair helper comum com `listarEmprestimos` é permitido, sem mudar comportamento).
     - Interface `Repositorio` em `handler.go`: só a assinatura de `ListarEmprestimos` muda; nenhum método novo.
  6. Migração 0003 (detalhe dos índices é do data-model; limites aqui).
     - `migrations/0003_<nome>.up.sql` + `.down.sql` (o executor exige o par, D-16 G4). Up ADITIVO: só `CREATE INDEX` (sem DROP, sem ALTER). Down: `DROP INDEX` dos índices criados (manual, D-16 decisão 2).
     - Sem CONCURRENTLY: o `CREATE INDEX` comum toma lock SHARE em `emprestimo` — SELECT segue, INSERT/UPDATE (abrir, devolver, renovar) esperam até o COMMIT do arquivo (uma transação por arquivo, G9). Aceito: tabela de biblioteca de bairro (ordem de 10^4–10^5 linhas), construção em milissegundos a poucos segundos; `statement_timeout` 60s por comando e `lock_timeout` 5s na aquisição (se houver escrita longa em curso, falha e desfaz o arquivo inteiro, sem estado parcial; reexecutar). Como as escritas da API têm prazo de 5s (`timeoutOperacaoPadrao`), uma construção acima de ~5s faria escritas responderem erro: report e runbook devem mandar conferir o tamanho (`SELECT count(*) FROM emprestimo` / `pg_total_relation_size`) antes e rodar em janela de baixo uso; acima de ~1 milhão de linhas, parar e usar o procedimento manual fora do executor (D-16 G7).
     - Índices simples `emprestimo_usuario_id_idx` e `emprestimo_livro_id_idx`: os compostos `(usuario_id, id)`/`(livro_id, id)` os tornam redundantes (mesma coluna líder, servem à FK e à igualdade), mas a remoção NÃO entra na 0003: (a) DROP INDEX toma ACCESS EXCLUSIVE na tabela e, na mesma transação, seguraria também as leituras até o COMMIT; (b) o executor só sobe, então desfazer um DROP exige migração nova; (c) o custo de manter dois índices a mais numa tabela pequena é desprezível. Remoção em delivery futura, em migração própria, com evidência de `pg_stat_user_indexes.idx_scan` estável nos índices antigos.
     - Ordem de deploy: `migrate up` antes do binário novo; o binário novo funciona sem os índices (só mais lento) e o antigo ignora índices novos, então inverter a ordem não quebra.

NAO_FAZER:
  - Não usar `fmt.Sprintf`/concatenação com qualquer valor vindo do cliente ou do filtro no texto SQL; valores só em `args`. Não usar `pgx.NamedArgs` nem query builder externo.
  - Não usar o padrão "SQL único com opcionais" (`($3::bigint IS NULL OR usuario_id = $3)`, `COALESCE`): com plano genérico do prepared statement o PostgreSQL perde o índice composto.
  - Não usar `now()`, `CURRENT_DATE`, `localtimestamp`, `AT TIME ZONE` nem fuso da sessão no SQL; não calcular `$hoje` no handler nem com `time.Local`/`time.Now()` direto (só o relógio injetado `a.agora`).
  - Não ler o relógio mais de uma vez por requisição; não usar instantes diferentes para o filtro e para a multa.
  - Não alterar `internal/domain/` (sem método `Vencido`, sem mexer em `Renovar`/`Multa`/`meiaNoiteUTC`); o oráculo dos testes é `Multa(agora) > 0` e `Renovar`.
  - Não criar método novo de listagem paralelo (`ListarEmprestimosFiltrados`, `ListarEmprestimosPorUsuario`) nem duplicar o `+1`/teto fora de `listarPorID`.
  - Não usar `OFFSET`, `COUNT`, total, `JOIN` com livro/usuário nem campo novo no DTO (nada de `situacao`/`vencido` no item).
  - Não aceitar lista de valores (`situacao=aberto,vencido`), case-insensitive, acento, `situacao=` vazio nem `usuario_id` na rota aninhada.
  - Não fazer `/livros` e `/usuarios` aceitarem as chaves novas; não mudar código, mensagem ou status existente; não criar código de erro novo (404 da aninhada é `usuario_inexistente`; 422 `usuario_nao_encontrado` continua só nas rotas de empréstimo via `classificar()`).
  - Não consultar existência de usuário/livro em `GET /emprestimos?usuario_id=&livro_id=` (200 vazio).
  - Não criar `GET /livros/{id}/emprestimos` (U2).
  - Não logar `usuario_id`, `livro_id`, `situacao`, query nem URL.
  - Na 0003: não usar a palavra "concurrently" em lugar nenhum do `.up.sql`, nem em comentário (o executor recusa, D-16 G7); não incluir `DROP INDEX`, `ALTER TABLE`, `BEGIN`/`COMMIT`, `SET`/`set_config`; não criar índice parcial com `CURRENT_DATE`/`now()` no predicado (não IMMUTABLE, e vencido não é indexável); não criar coluna de situação, gatilho ou job.
  - Não editar 0001/0002 (checksum) nem `internal/migrate/`.
  - Não tocar `go.mod`/`go.sum`, `.pipeline-claude.json`, `.claude/`.
  - Não alterar `docs-ai/deliveries/INDEX.md` e `docs-ai/deliveries/DEL-014/report.md` além da marcação merged já presente no working tree.

CONFIRMADO:
  - Handler e rotas: `internal/http/handler.go:134-148` (mux, `GET /usuarios/{id}` na 147, sem conflito de padrão com `/usuarios/{id}/emprestimos`); `listarEmprestimos` lê `a.agora()` uma vez (`handler.go:436`) e calcula multa com ele (`montarSaidaEmprestimo`, 572-591); `obterUsuario` mapeia `storage.ErrUsuarioNaoEncontrado` → `codUsuarioInexistente` localmente (405-425).
  - Parser atual: `lerParametrosListagem` em `handler.go:616-644` (só `limite`/`apos`, repetida → falso); fuzz existentes `FuzzParametrosListagem` (`internal/http/fuzz_test.go:89`) e `FuzzParametrosListagemNovasRotas` (`internal/http/listagem_usuarios_emprestimos_test.go:636`).
  - Storage: `listarPorID` em `internal/storage/repositorio.go:173-201` passa só `apos, limite+1`; `ListarEmprestimos` em 161-166 com SQL fixo; `vencimentoParaDate` em 419-422 (data civil SP em meia-noite UTC, usada na escrita do vencimento, linhas 258 e 334).
  - Domínio: fronteira de vencido em `Renovar` (`internal/domain/emprestimo.go:192`, `meiaNoiteUTC(ref) > meiaNoiteUTC(e.vencimento)`) e multa `dias > 0` com o mesmo `meiaNoiteUTC` (`internal/domain/multa.go:43-55`); `Aberto()` = devolução zero (`emprestimo.go:208`). Equivalência com `vencimento < $hoje` por construção (mesma conversão civil SP dos dois lados).
  - pgx sem `QueryExecMode` configurado (grep por `QueryExecMode|statement_cache|NamedArgs` em `*.go`: nenhum resultado), logo modo padrão com cache de prepared statement por texto SQL.
  - Executor: `internal/migrate/executor.go:7` ("Só existe para cima: não há down nem CONCURRENTLY"), timeouts padrão 5s/60s (35-36), `statement_timeout` por comando e transação por arquivo (50-57); D-16 G4 (pares up/down), G7 (palavra recusada até em comentário; índice com dados grandes = procedimento manual), regra "um DDL pesado por arquivo" (`docs-ai/DECISOES.md:435-438, 511-523`).
  - Esquema: `migrations/0001_esquema_emprestimo.up.sql:24` FK de usuário `ON DELETE RESTRICT`; índices 28-30; nenhuma rota de exclusão de usuário em `handler.go`.
  - Testes que mudam por mudança de comportamento intencional: `internal/http/listagem_usuarios_emprestimos_test.go:63-66, 89, 581, 640` esperam 400 para `situacao`/`usuario_id`/`livro_id` em `/emprestimos`.

ASSUNCOES:
  - A1 confirmada, com acréscimo: `situacao=` vazio → 400; ids pelo `parseID` (até 2^63-1).
  - A2 confirmada (AND entre todos, inclusive `limite`/`apos`; combinação sem resultado → 200 `{"itens":[],"proximo":null}`).
  - A3 → decisão do usuário U1 (acima).
  - A4 → decisão do usuário U2 (acima).
  - A5 confirmada: rota aninhada aceita `limite`, `apos`, `situacao`, `livro_id`; `usuario_id` → 400.
  - A6 confirmada e decidida: verificação de existência do usuário em TODA página da rota aninhada (2 consultas por requisição, sem transação); `?usuario_id=`/`?livro_id=` sem verificação. Ordem de erros: `id_invalido` → `parametro_invalido` → `usuario_inexistente`.
  - A7 confirmada: um instante por requisição para filtro e multa.
  - A8 revisada: 0003 só aditiva; remoção dos índices simples adiada (motivos em ABORDAGEM 6). Composto vs parcial e INCLUDE ficam com o data-model, dentro desses limites. Lock SHARE sem CONCURRENTLY aceito sob a premissa de tamanho (abaixo).
  - Premissa não verificada: a tabela `emprestimo` em produção tem ordem de 10^4–10^5 linhas (não há acesso ao banco de produção daqui). O runbook manda conferir antes.
  - Premissa: o planner escolhe o composto `(fk, id)` para `fk = $ AND id > $ ORDER BY id LIMIT n` mesmo com o índice simples presente; o data-model/code-reviewer conferem com EXPLAIN em teste com banco.

ARQUIVOS:
  - nenhum editado (revisor sem escrita). Previstos: internal/storage/repositorio.go; internal/http/handler.go; migrations/0003_*.{up,down}.sql; testes em internal/storage e internal/http (inclusive fakes e os casos que hoje esperam 400); migrations/ (teste da 0003, se seguir o padrão de renovacao_test.go); docs-ai/DECISOES.md (D-21 com U1, U2 e este handoff); docs-ai/03-MAPA-RAPIDO-MODULOS.md; docs-ai/RUNBOOK-MIGRACAO.md (nota da 0003: lock SHARE, conferir tamanho, janela de baixo uso).

RISCOS:
  - Mudança de contrato da API: rota nova `GET /usuarios/{id}/emprestimos`; `GET /emprestimos` passa a aceitar `situacao`, `usuario_id`, `livro_id` (antes 400 → agora 200 filtrado). Nenhum status/código/DTO existente muda. Registrar na D-21.
  - Lock da 0003: escritas de empréstimo bloqueadas durante a construção dos índices; com construção > 5s, escritas da API falham por timeout. Mitigação só operacional (tamanho conferido, janela). Premissa de tamanho não verificada.
  - Índices redundantes temporários (simples + compostos) até a delivery de remoção; custo de escrita pequeno, mas a remoção pode ser esquecida — registrar como pendência na D-21.
  - `vencido` percorre abertos (até 1 por livro) com filtro `vencimento < $hoje`; custo cresce com o número de abertos não vencidos antes de completar a página. Aceitável no porte; não indexável (R1 do triage).
  - Paginação através da meia-noite SP com `situacao=vencido`: item pode entrar no conjunto depois da página que o cobriria (consistência eventual, como D-19).
  - Divergência domínio x SQL só é pega pelo teste com banco cruzado (ABORDAGEM 3): o CI precisa continuar com PostgreSQL e `BIBLIOTECA_TEST_DB_OBRIGATORIO=1`.
  - R4 do triage (histórico de leitura por usuário em uma requisição) fica para o security-review; a condição de bloqueio da D-20 passa a cobrir `GET /usuarios/{id}/emprestimos`.

SINAIS:
  SECURITY: sim — entrada externa nova (três query params e rota nova) em `internal/http/`, SQL novo montado por fragmentos em `internal/storage/`, migração em `migrations/`; exposição de histórico por usuário.
  SUPPLY_CHAIN: nao — stdlib e `pgx/v5` existentes; sem mudança em `go.mod`/`go.sum`.
  MIGRATION: sim — `0003` aditiva com índices, sem CONCURRENTLY, lock SHARE em `emprestimo`.
  PERFORMANCE: sim — keyset filtrado depende dos índices compostos/parcial; `vencido` não indexável; `query-performance` não habilitado, então EXPLAIN em teste com banco fica com data-model e code-reviewer.

PROXIMO: data-model
MOTIVO: módulo `data-model` habilitado e a demanda toca persistência: falta decidir os índices exatos da 0003 (colunas, parcial, nomes, down) dentro dos limites acima (aditiva, sem DROP dos simples, sem a palavra proibida) antes do test-writer.
