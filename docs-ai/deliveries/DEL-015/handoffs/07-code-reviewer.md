DELIVERY_ID: DEL-015
TASK_CODE: DEL-015
AGENT: code-reviewer
STATUS: ok

RESULTADO: aprovado, sem BLOQUEIO. Li o diff inteiro: handler.go, repositorio.go, os testes alterados (pipeline_diff) e os arquivos novos (Read). Conferi tudo contra o NAO_FAZER do handoff 02. O query-performance não está habilitado, então a conferência de plano foi feita aqui (ponto 3 abaixo).

BLOQUEIOS:
  - nenhum

SUGESTOES:
  - S1 [internal/storage/plano_listagem_test.go:207-216, 242, 248, 250] — impacto da folga do EXPLAIN na remoção futura dos índices simples. O teste aceita BitmapAnd com folhas nos índices simples nas combinações 6, 7 e 13-16. Nessas combinações o planner prefere `emprestimo_usuario_id_idx`/`emprestimo_livro_id_idx` porque são menores que os compostos. Na produção, `pg_stat_user_indexes.idx_scan` desses índices vai continuar crescendo. Com isso, o critério de remoção do handoff 02 (ABORDAGEM 6, "idx_scan estável nos índices antigos") nunca será cumprido. Proposta para o docs-update registrar na D-21: trocar o critério por evidência de plano. Proposta para a delivery de remoção: um subteste em `TestPlanoListarEmprestimos` que faça DROP dos dois índices simples na fixture e reaplique `checarPlano`. Sem eles, o BitmapAnd passa a usar os compostos, que têm a mesma coluna líder, e o custo é equivalente. A folga em si é aceitável e não esconde plano ruim. O BitmapAnd é limitado pela interseção: abertos ≤ nº de livros por causa do `emprestimo_livro_aberto_uq`, e o par usuário+livro é pequeno. O custo é O(empréstimos do usuário + abertos) em bitmap, não um heap scan sem limite. Seq Scan, Bitmap sem interseção e Index Scan nos simples continuam proibidos (linhas 166-168, 207-209, 220-226). A asserção estrita vale em 1-5, 8, 9 e 12 (linhas 233-246) e cobre a regressão "composto ausente".
  - S2 [internal/storage/plano_listagem_test.go:212-216] — no ramo de interseção, as combinações 13 e 16 aceitam qualquer par de folhas de emprestimo, por exemplo PK + aberto, que deixa `usuario_id`/`livro_id` só no Filter do heap. Dá para endurecer: exigir que ao menos uma folha seja de usuário (`idxUsuario` ou `idxUsuarioID`) e uma de livro. Hoje a fixture não produz esse plano, então não bloqueia.
  - S3 [internal/storage/plano_listagem_test.go:51-63] — a massa de 200 mil linhas acrescenta ~8 s ao pacote storage no CI (13,5 s no total, handoff 06). Para o porte é aceitável. Se o tempo de CI pesar, o candidato é este teste, e a saída não é reduzir a massa: com 20 mil linhas os planos de 5-9 e 12 deixam de exercitar os compostos (handoff 05).
  - S4 [internal/http/handler_test.go:725-732; internal/http/apoio_test.go:464] — o handoff 02 não listava estas edições em teste existente, mas elas são legítimas. Antes, `GET /usuarios/1/emprestimos` 404 virava falso positivo, e a troca foi por 6 casos mais estritos (subrota desconhecida, barra final, /emprestimos/2, 405 em POST/DELETE, `/livros/1/emprestimos` 404 por U2). `rotasProtegidas()` só ganhou a rota nova, e com isso auth e timeout passaram a cobri-la. Em `listagem_usuarios_emprestimos_test.go:66-69, 84`, /usuarios continua com 400 nos 4 casos de filtro, e só /emprestimos passa a 200. Foram escritas pelo test-writer antes da implementação; o dev declara 0 testes alterados (handoff 06). Registrar no report como desvio aprovado.

CONFIRMADO:
  - Montagem do SQL [internal/storage/repositorio.go:216-243]:
    - O texto sai só de literais constantes concatenados. O placeholder é `"$" + strconv.Itoa(len(args)+2)` e começa em $3. Os valores (`f.UsuarioID`, `f.LivroID`, `vencimentoParaDate(f.Ref)`) só entram em `args`, via `proximo`.
    - Não há `IS NULL OR`, `COALESCE`, `Sprintf`, NamedArgs, `now()`, `CURRENT_DATE` ou `AT TIME ZONE`.
    - A ordem é fixa (usuário, livro, situação), o que dá 16 textos estáveis (`listagem_filtro_sem_banco_test.go:163`).
    - Com o filtro zero, o SQL é igual byte a byte a `sqlSemFiltro`, que é o SQL removido do diff (`listagem_filtro_sem_banco_test.go:21, 170-193`; `plano_listagem_test.go:296-304`). Ref sozinha não muda o texto.
    - `listarPorID` repassa `apos, limite+1, extra...` (`repositorio.go:257`). O +1 e o teto ficaram só ali. ListarLivros e ListarUsuarios não mudaram de chamada.
  - Validação antes de SQL [repositorio.go:185-194, ListarEmprestimos]: ids < 0, situação fora de Todas..Vencido e vencido com Ref zero são recusados com ErrListagemInvalida antes de montar o SQL.
  - Regra de vencido:
    - O parâmetro é `vencimentoParaDate(f.Ref)` (`repositorio.go:241`), que é `v.In(locSaoPaulo).Date()` em meia-noite UTC (`repositorio.go:499-502`). É a mesma conversão de `domain.meiaNoiteUTC` (`internal/domain/multa.go:52-55`), e a coluna é gravada com a mesma função. Então `vencimento < $hoje` ⇔ `meiaNoiteUTC(ref) > meiaNoiteUTC(venc)`.
    - `internal/domain/` não está no diff.
    - Fronteira com banco [`internal/storage/listagem_filtro_test.go:302-380`]: relógios em 23:59:59 SP (02:59:59Z), 00:00 SP (03:00Z), 12:00 SP, 23:59:59.999999 SP e UTC+14, × 5 fusos de sessão. Oráculo duplo (`Multa > 0` e `Renovar == ErrEmprestimoVencido`, este em cópia), mais o recorte usuário+vencido com limite 1.
    - Sem banco: `listagem_filtro_sem_banco_test.go:198-262` confere o parâmetro e a codificação DATE nos mesmos instantes e na virada de ano.
  - Rota aninhada [internal/http/handler.go:148, 446-470]:
    - A rota fica atrás do mesmo `auth`.
    - Ordem dos erros: `parseID` → id_invalido; parser aninhado → parametro_invalido; um único `context.WithTimeout`; `BuscarUsuario` em toda página com `ErrUsuarioNaoEncontrado` mapeado localmente para 404 `usuario_inexistente`; os outros erros vão para `falharErro`.
    - `usuario_id` é recusado na aninhada (handler.go, `lerParametrosEmprestimos`, caso `usuario_id` com `aninhada`).
  - Relógio único [handler.go:475-497]: `responderPaginaEmprestimos` lê `a.agora()` uma vez, define `filtro.Ref = agora` em toda requisição e usa o mesmo `agora` em `montarSaidaEmprestimo`. Na aninhada, a leitura acontece depois de BuscarUsuario e antes de ListarEmprestimos, o que é aceitável. Coberto por `listagem_filtro_emprestimos_test.go:443-494` (1 leitura, antes do repositório, `Ref == instante`, multa com o mesmo instante, nas duas rotas).
  - Parser [handler.go:650-736]:
    - `lerQueryListagem` mantém ParseQuery com erro tratado, chave repetida → falso e desconhecida → falso.
    - `lerParametrosListagem` passa um extra que recusa tudo, então /livros e /usuarios continuam só com limite/apos.
    - `situacao` usa comparação exata. Em caso de falha, o filtro parcial é zerado no retorno.
    - O oráculo do fuzz (`oraculoEmprestimos`, `listagem_filtro_emprestimos_test.go:30`) é independente da implementação.
  - Erros e logs: nenhum código de erro novo, nenhum log novo, nenhum `_ =` em erro, nenhuma goroutine. O ctx com timeout cobre as duas consultas.
  - Migração 0003 [migrations/0003_indices_listagem_emprestimo.up.sql:1-9, .down.sql:1-6]:
    - O up só tem os 3 CREATE INDEX e é aditivo. Não tem a palavra proibida, nem DROP/ALTER/BEGIN/SET, nem predicado com relógio. O parcial é `WHERE devolucao IS NULL`, que é IMMUTABLE.
    - O down tem 3 DROP em ordem inversa.
    - 0001, 0002 e `internal/migrate/` não estão no diff.
  - Fora do escopo intocado: go.mod/go.sum, `.claude/`, `.pipeline-claude.json` e `internal/domain/` não aparecem no pipeline_diff. INDEX.md e DEL-014/report.md têm só a marcação merged prevista.

ASSUNCOES:
  - Não reexecutei a suíte nem o EXPLAIN (o revisor não tem shell). Os resultados com banco (0 SKIP, `TestPlanoListarEmprestimos` 8,87 s no PG 16 com o digest do CI) são os do handoff 06.
  - A análise da S1 sobre a preferência do planner pelos índices simples no BitmapAnd vem dos handoffs 05/06 (EXPLAIN no container) e do custo relativo de tamanho de índice. Não observei isso em produção.

ARQUIVOS:
  - nenhum editado (revisor só lê).

RISCOS:
  - Nas combinações 6, 7 e 13-16, uma regressão que troque o composto por outro índice só é detectada se o plano sair do BitmapAnd. O risco é limitado pelos casos estritos de 5, 8, 9 e 12.
  - Critério de remoção dos índices simples inalcançável como está escrito no 02 (S1). Sem ajuste na D-21, a pendência de remoção tende a ficar parada.
  - Os herdados continuam: lock SHARE da 0003 (premissa de tamanho não verificada), paginação de vencido atravessando a meia-noite SP e R4 (histórico por usuário) para o security-review.

SINAIS:
  SECURITY: sim — entrada externa nova (situacao, usuario_id, livro_id, rota aninhada) e SQL por fragmentos em internal/storage/ e migrations/ (security_surface_paths).
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff.
  MIGRATION: sim — 0003 com 3 CREATE INDEX sem a opção online (lock SHARE) e down com 3 DROP INDEX.
  PERFORMANCE: sim — keyset filtrado nos compostos e parcial. Plano conferido aqui porque o query-performance está desabilitado; folga do BitmapAnd aceita, com S1-S3.

PROXIMO: migration-reviewer
MOTIVO: MIGRATION: sim e o módulo migration-reviewer está em enabled_modules. Depois vem o security-review (SECURITY: sim, diff em internal/storage/ e migrations/) e então o qa-validation. As sugestões S1 e S4 são para o docs-update registrar na D-21 e no report.
