DELIVERY_ID: DEL-014
TASK_CODE: DEL-014
AGENT: code-reviewer
STATUS: ok

RESULTADO: aprovado sem bloqueios. Li o diff inteiro contra origin/main (merge-base fb135d2): internal/storage/repositorio.go, internal/http/handler.go, internal/http/erros.go, internal/http/apoio_test.go, internal/http/handler_test.go. Li também com Read os 5 arquivos de teste não rastreados: internal/http/listagem_usuarios_emprestimos_test.go, internal/http/banco_listagem_usuarios_emprestimos_test.go, internal/http/erros_usuario_interno_test.go, internal/storage/listagem_sem_banco_usuarios_emprestimos_test.go e internal/storage/listagem_usuarios_emprestimos_test.go (este por busca de estrutura: cada teste usa um banco próprio migrado, e o DELETE só remove a linha criada para abrir o buraco no id).

BLOQUEIOS:
  - nenhum

SUGESTOES:
  - [internal/storage/repositorio.go:127] O comentário de ListarLivros ainda cita 1..LimiteListagemLivros. O valor é o mesmo e não há efeito no comportamento, mas ficaria coerente com listarPorID (:171) e com ErrListagemInvalida (:35-38) citar LimiteListagem.
  - [internal/http/handler.go:613-615] A quebra de linha do comentário de lerParametrosListagem ficou irregular ("apos\n// segue a gramática de\n// parseID (padrão 0, início)"). É só cosmético: dá para refluir o parágrafo.

CONFIRMADO:
  - Refatoração de ListarLivros para listarPorID (repositorio.go:129-140, :173-201), sem mudança de comportamento:
      - A validação é a mesma (apos < 0, limite < 1, limite > 100) e roda antes do SQL. LimiteListagemLivros = LimiteListagem = 100 (:44, :48).
      - O SQL é idêntico, byte a byte, e os argumentos continuam (apos, limite+1).
      - Erro de Query vai por traduzirErro, como antes. Erro de Scan: o closure aplica traduzirErro (:135-137) e o helper devolve o erro sem embrulhar de novo (:190-192). É o mesmo erro que o código antigo devolvia.
      - O laço sai em len == limite sem escanear a linha extra. A slice é criada com make(0, limite), então nunca é nil.
      - Continuam rows.Close() explícito, rows.Err() com traduzirErro e o defer rows.Close() fora do laço.
      - A assinatura pública não mudou e o helper não é exportado.
      - Nenhum teste existente de livros foi alterado: o diff de teste se limita a apoio_test.go e handler_test.go, e em handler_test.go só troca o caso :722, a exceção prevista no handoff 02.
  - Extração de montarSaidaEmprestimo (handler.go:561-591): responderEmprestimo continua fazendo, na mesma ordem, Multa(agora), depois falharErro em caso de erro, depois responder(status, out). O DTO é o mesmo campo a campo, inclusive Devolucao só quando !Aberto(). A função é pura: não recebe w nem r e não escreve nada. obterEmprestimo, consultarEmprestimo, devolver e renovar não mudaram.
  - Leitura única do relógio, sem página parcial (handler.go:430-458):
      - agora := a.agora() é chamado uma vez, depois da validação da query e antes do WithTimeout e de ListarEmprestimos, na mesma ordem de obterEmprestimo (:482).
      - Todos os itens são montados com esse mesmo agora.
      - Um erro em qualquer item chama falharErro e retorna antes de responder, então nenhum byte é escrito antes do erro e não há item pulado.
      - Um erro de reidratação vindo do storage também é 500 da página inteira, pelo retorno nil, false, err do helper.
      - TestListarEmprestimosInstanteUnico prova as duas coisas: uma única leitura do relógio, e essa leitura acontece antes do repositório.
  - SQL das consultas novas (o query-performance não está ligado):
      - ListarUsuarios (:147): `SELECT id, nome FROM usuario WHERE id > $1 ORDER BY id LIMIT $2`.
      - ListarEmprestimos (:163): `SELECT `+colunasEmprestimo+` FROM emprestimo WHERE id > $1 ORDER BY id LIMIT $2`, com colunasEmprestimo constante em :242.
      - Ambas são keyset pela PK (usuario_pkey e emprestimo_pkey) com LIMIT limite+1, parametrizadas e sem concatenar entrada externa. Não há OFFSET, COUNT, JOIN nem filtro.
      - É uma consulta por página e a reidratação é em memória, via lerEmprestimo (:391-414), com o mesmo erro "empréstimo gravado inválido" do detalhe. Não há N+1 nem BuscarEmprestimo em laço, e não se usa CollectRows nem ForEachRow.
  - NAO_FAZER do handoff 02:
      - Nenhuma migração.
      - Nenhum parâmetro novo: lerParametrosListagem não mudou, salvo a constante de mesmo valor em :629. Continua usando url.ParseQuery(RawQuery) e não r.URL.Query().
      - classificar() e a entrada usuario_nao_encontrado (422) estão intactos. usuario_inexistente 404 entrou no catálogo (erros.go:41, :81) e é mapeado só em obterUsuario (handler.go:414-418) com errors.Is, que pega também o erro embrulhado.
      - Nenhum nome existente foi renomeado ou removido.
      - Não há log novo nem eco de entrada nas mensagens.
      - go.mod e go.sum fora do diff.
  - Checklist Go/HTTP:
      - Erros: nenhum novo ignorado com `_ =`. O `_ = json...Encode` em responder já existia.
      - Context e timeout: context.WithTimeout com defer cancel em cada handler novo, fora de laço.
      - Nenhuma goroutine nova e nenhum estado compartilhado mutável.
      - A interface Repositorio continua definida no consumidor (httpapi) e ganhou só os três métodos.
      - As três rotas novas estão atrás de auth (handler.go:146-148), e a entrada é validada antes do repositório (parseID e lerParametrosListagem).
      - Status: 400, 404 (usuario_inexistente), 500 e 503 coerentes, com mensagens do catálogo, sem detalhe interno.
      - Nenhum segredo no código.
  - Testes: os novos são em tabela, com a stdlib testing, e cobrem os pontos do handoff 02:
      - linha extra não escaneada, argumentos (apos, limite+1) e "ORDER BY id";
      - recusa antes do SQL;
      - erro sem página parcial e rows fechado;
      - 404 local e 422 preservado;
      - log sem query nem nome;
      - fuzz com o mesmo oráculo de GET /livros;
      - com banco: percorrer as listagens com vários limites e linha corrompida como linha extra.

ASSUNCOES:
  - Não rodei testes nem sensores: este agente só lê. Tomei como base a evidência do handoff 04: sensores ok, suíte com PostgreSQL real com 0 SKIP e 0 FAIL, fuzz de 30s PASS. O qa-validation deve repetir com BIBLIOTECA_TEST_DB_OBRIGATORIO=1.
  - A marcação merged da DEL-013 (docs-ai/deliveries/INDEX.md e DEL-013/report.md) está fora do escopo de código desta revisão (A8).

ARQUIVOS:
  - nenhum alterado (revisor sem escrita)

RISCOS:
  - Os riscos de produto do handoff 02 continuam e vão para security-review e docs-update:
      - extração em lote de nomes e do histórico de empréstimos com o token único;
      - página de usuários de até cerca de 6 MB;
      - uma linha de empréstimo inconsistente faz 500 permanente na página que a contém.
  - O off-by-one e a ordem das consultas novas (por exemplo id >= $1) só são pegos pelos testes com banco.

SINAIS:
  SECURITY: sim — diff em internal/http/ e internal/storage/ (security_surface_paths); entrada por query string; enumeração em lote de dado pessoal
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff
  MIGRATION: nao — nenhuma migração; consultas só nas PKs
  PERFORMANCE: nao — keyset pela PK com LIMIT limite+1, uma consulta por página, sem JOIN/N+1; conferido nesta revisão (query-performance desabilitado)

PROXIMO: security-review
MOTIVO: sem bloqueios; o diff toca security_surface_paths (internal/http/, internal/storage/) e a delivery tem require_security.
