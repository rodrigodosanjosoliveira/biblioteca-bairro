DELIVERY_ID: DEL-017
TASK_CODE: PROJ-7
AGENT: test-writer
STATUS: ok

ESCOPO: sugestão S3 do code-reviewer (09-code-reviewer.md:14). Só o texto do comentário do subteste "noPlano lê SubPlan" em internal/storage/plano_livros_test.go:192 muda. Nenhuma linha de código mudou, e o comportamento do teste é o mesmo.

ALTERACAO:
  - Antes: "Regressão da DEL-015: o noPlano ampliado não muda a leitura dos planos de empréstimo (as combinações continuam no TestPlanoListarEmprestimos)."
  - Depois: o comentário diz que o campo ParentRelationship do noPlano é decodificado do EXPLAIN: no plano de sqlBuscarLivro (livro 1), algum nó traz Parent Relationship "SubPlan". Sem isso, a exigência de SubPlan em checarPlanoLivro (caminhoSubPlan, :114-115) dependeria de um campo sempre vazio.

CONFIRMADO:
  - go vet ./internal/storage/: sem saída (OK).
  - go test -count=1 -v -run TestPlanoLeituraLivro ./internal/storage/ sem banco: --- SKIP (BIBLIOTECA_TEST_DATABASE_URL não definida), pacote ok.
  - Com banco: NÃO RODADO. O Postgres de teste usado antes (postgres://postgres@localhost:55432, handoffs 07/08) não está de pé: docker ps não mostra nenhum container, e a conexão falha. Com BIBLIOTECA_TEST_DB_OBRIGATORIO=1 o resultado é FAIL "conectar ao banco de teste: falhou". O motivo é o ambiente, não o código. A última execução com banco (08-dev-implementation.md) deu PASS nos 10 casos, e esta mudança é só de comentário.
  - Mutantes: não aplicado. A mudança é só de comentário, sem comportamento novo para validar.

ASSUNCOES:
  - Que a mudança é só de comentário se confere pelo git diff do arquivo, nas linhas 192-195.

ARQUIVOS:
  - /home/rodrigo/workspace/biblioteca-bairro/internal/storage/plano_livros_test.go (comentário nas linhas 192-195)

RISCOS:
  - nenhum novo. O qa-validation precisa subir o Postgres de teste e rodar a suíte com BIBLIOTECA_TEST_DB_OBRIGATORIO=1 (0 SKIP).

SINAIS:
  SECURITY: nao — só um comentário de teste.
  SUPPLY_CHAIN: nao — go.mod/go.sum intocados.
  MIGRATION: nao — migrations/ intocado.
  PERFORMANCE: nao — sem mudança de consulta.

PROXIMO: qa-validation
MOTIVO: S3 aplicada, só no comentário. O qa-validation roda a suíte completa com banco.
