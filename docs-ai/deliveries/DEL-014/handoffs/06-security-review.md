DELIVERY_ID: DEL-014
TASK_CODE: DEL-014
AGENT: security-review
STATUS: ok

RESULTADO: aprovado. Nenhum achado CRITICO ou ALTO. Há um MEDIO de exposição em lote, aceito pelo usuário (U2), e três BAIXO, todos com risco registrado. Nenhum deles bloqueia.

SUPERFICIE_REVISADA:
  - internal/http/handler.go (diff): interface Repositorio, rotas em NovoHandler :146-148, listarUsuarios, obterUsuario, listarEmprestimos, montarSaidaEmprestimo/responderEmprestimo, lerParametrosListagem (:629, só a troca de constante)
  - internal/http/erros.go (diff): codUsuarioInexistente e entrada no catálogo; falhar/falharErro/classificar/causaInterna (:84-169) lidos para conferir o vazamento em erro
  - internal/storage/repositorio.go (diff): LimiteListagem, listarPorID, ListarLivros (delegando), ListarUsuarios, ListarEmprestimos
  - internal/http/middleware/middleware.go (contexto, sem diff): Auth (:259-288), ResponderErro (:101-110), Log (:155-209)
  - Testes lidos direto (não rastreados): internal/http/listagem_usuarios_emprestimos_test.go (parâmetros :33-109, mapa de erros :446-496, id do detalhe :500-545, auth antes de tudo :580-591, log :595-630, fuzz :636-675), internal/http/banco_listagem_usuarios_emprestimos_test.go, internal/http/erros_usuario_interno_test.go, internal/storage/listagem_sem_banco_usuarios_emprestimos_test.go, internal/storage/listagem_usuarios_emprestimos_test.go (procurei SQL montado com Sprintf, URL de banco ou credencial no código: nada encontrado; o Exec usa só $1)
  - internal/http/apoio_test.go e internal/http/handler_test.go (diff)

GOVULNCHECK (pipeline_sensors names=["vuln"], resultado real):
  {"name":"vuln","ok":true,"exit_code":0,"duration_ms":4668,"output":"No vulnerabilities found.\n"}

INSPECIONADO:
  - Autenticação e autorização:
      - As três rotas novas estão registradas com auth(...) (handler.go:146-148). Auth compara o SHA-256 do token com subtle.ConstantTimeCompare, exige exatamente um header Authorization, nega quando o token configurado está vazio e põe Cache-Control: no-store na resposta (middleware.go:259-288). O no-store também cobre as respostas 200 com nomes.
      - TestRotasNovasAuthAntesDeTudo prova que a auth roda antes da validação de query e de {id}, sem chamar o repositório. rotasProtegidas ganhou as 3 rotas, então TestAuthRotasProtegidas e TestTimeoutPorOperacao as cobrem.
      - Não existe autorização por recurso. É coerente com o modelo de token único (D-15/D-17/D-19).
  - Entrada externa:
      - A query passa por lerParametrosListagem, que não mudou exceto pela constante de mesmo valor 100. Usa url.ParseQuery(RawQuery) e aceita só as chaves limite e apos, sem repetição. limite fica em 1..100 e apos em >0 pela gramática do parseID. Os casos ;, escape inválido e overflow dão 400.
      - Os testes conferem que a resposta de erro não ecoa a entrada e que o repositório não é chamado.
      - O fuzz com o mesmo oráculo de livros passou 30s no handoff 04.
      - {id} usa parseID e responde 400 id_invalido antes do repositório.
      - O storage revalida apos e limite antes do SQL (defesa em profundidade). limite+1 tem no máximo 101, sem overflow. apos = MaxInt64 é aceito e devolve página vazia.
  - SQL:
      - As duas consultas novas são literais constantes com $1 e $2 parametrizados: `SELECT id, nome FROM usuario WHERE id > $1 ORDER BY id LIMIT $2` e `SELECT `+colunasEmprestimo+` FROM emprestimo WHERE id > $1 ORDER BY id LIMIT $2`. colunasEmprestimo é constante.
      - Nenhuma entrada externa entra por concatenação. Não há ordenação vinda do cliente.
  - Erro e vazamento:
      - Respostas de erro vêm só do catálogo, com mensagem fixa.
      - Em 500, o log anota apenas causa e, se houver PgError, pg_code e pg_constraint. Não registra a mensagem do erro, então o texto "empréstimo gravado inválido: ..." não vai para o log nem para a resposta.
      - TestRotasNovasMapaDeErros aplica semTextoInterno à resposta e ao log, com PgError secreto e "host=segredo".
  - Log:
      - middleware.Log grava metodo, rota (r.Pattern, ou seja o padrão "GET /usuarios/{id}" e não a URL), status, duracao_ms, request_id e anotações. Não grava URL, query, headers nem corpo.
      - Os handlers novos não anotam nada além do que falhar/falharErro anotam (codigo e causa).
      - TestLogRotasNovasSemQueryNemNome exige uma única linha de log e afirma que ela não contém o nome "NOME-SIGILOSO-QWERTY", nem 424242/424243, QSSECRETO, "limite", "apos" ou "?". Isso vale em sucesso, em 404 e em PgError. Confirmado: o log não grava nome nem query string.
  - Código usuario_inexistente:
      - 404 com a mensagem fixa "usuário não encontrado", mapeado só em obterUsuario com errors.Is. classificar continua com 422 nas rotas de empréstimo (teste "emprestimo com usuário inexistente continua 422").
      - Outros erros, como ErrLivroNaoEncontrado, não viram usuario_inexistente.
      - Revela se um id existe, mas só para quem tem o token, e esse mesmo token já enumera tudo por GET /usuarios. O código novo não abre exposição adicional.
  - Segredos: nenhum no diff nem nos testes novos (busca por postgres://, password, senha e Getenv nos arquivos novos: sem ocorrências).
  - Criptografia: o diff não toca nada de criptografia.
  - CI: nenhum workflow no diff.
  - Dependências: go.mod e go.sum fora do diff.

ACHADOS:
  - [MEDIO, aceito (U2)] Exposição em lote de dado pessoal. Com o token, GET /usuarios percorre todos os nomes (único dado pessoal, D-12) e GET /emprestimos todo o histórico (livro_id, usuario_id, datas, multa), e os dois se cruzam por usuario_id. Hoje o impacto é limitado porque o token é único e de operador. Em compensação, o vazamento do token passa a significar extração completa em cerca de N/100 requisições, quando antes exigia enumerar id por id. Não há rate limit nem log de volume por cliente. Condição de bloqueio futura: com vários tokens ou credenciais de usuário final, as três rotas viram IDOR/vazamento em massa e precisam de autorização por recurso antes. O docs-update deve registrar isso em D-20 ou na extensão da D-19, como decidido em U2. Não bloqueia esta entrega.
  - [BAIXO] Tamanho de resposta: uma página de usuários pode chegar a cerca de 6 MB, porque o nome não tem teto além do corpo de 64 KiB na criação. Um portador do token pode forçar respostas grandes repetidamente. É o mesmo risco aceito na D-19 para títulos.
  - [BAIXO] Disponibilidade: uma linha de emprestimo inconsistente (vencimento <= início ou devolução < início, sem CHECK no banco) gera 500 permanente na página que a contém. É fail-closed e o cliente contorna com apos. Não vaza dado. Aceito no handoff 02.
  - [BAIXO, informativo] usuario_inexistente (404) vs 200 permite confirmar a existência de um id. Não é exposição nova, pela listagem autenticada. Nenhuma ação necessária.

ASSUNCOES:
  - A configuração de produção do token (não vazio, por variável de ambiente) segue como nas deliveries anteriores. Não está no diff.
  - Não rodei a suíte de testes. As afirmações sobre testes vêm da leitura do código e da evidência do handoff 04 (suíte com banco 0 SKIP / 0 FAIL, fuzz de 30s PASS).

ARQUIVOS:
  - nenhum alterado (revisor sem escrita)

RISCOS:
  - O risco MEDIO acima precisa estar no report (seção Security) e na decisão nova. Se ficar só no handoff, a próxima delivery que introduzir múltiplos tokens pode não vê-lo.
  - Sem rate limit, a extração em massa com token vazado é rápida. Fica como sugestão de delivery futura, fora do escopo atual.

SINAIS:
  SECURITY: sim — revisado; sem achado bloqueante; um MEDIO aceito (U2) e três BAIXO registrados
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff; govulncheck "No vulnerabilities found."
  MIGRATION: nao — nenhuma migração
  PERFORMANCE: nao — keyset pela PK, LIMIT limite+1, uma consulta por página

PROXIMO: qa-validation
MOTIVO: sem achado CRITICO ou ALTO e SUPPLY_CHAIN: nao. O QA deve repetir a suíte com BIBLIOTECA_TEST_DB_OBRIGATORIO=1. O docs-update registra o risco MEDIO (U2) na decisão e na seção Security do report.
