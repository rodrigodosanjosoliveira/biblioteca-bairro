DELIVERY_ID: DEL-016
TASK_CODE: DEL-016
AGENT: docs-update
STATUS: ok

CONFIRMADO:
  - Rotas (15) lidas em internal/http/handler.go:135-149; healthz/readyz sem auth, demais atrás de middleware.Auth.
  - Corpos de entrada: entradaLivro {"titulo"}, entradaUsuario {"nome"}, entradaEmprestimo {"usuario_id"} (handler.go:207-217); lerCorpo usa DisallowUnknownFields; devolucao e renovacao exigem corpo vazio (exigirCorpoVazio, handler.go:766-779). Content-Type da requisição não é conferido pelo handler; a coleção o envia nos POST com corpo.
  - Query: /livros e /usuarios só limite (1..100, padrão 20) e apos; /emprestimos aceita situacao (aberto|devolvido|vencido), usuario_id, livro_id; /usuarios/{id}/emprestimos recusa usuario_id; chave repetida, desconhecida ou valor inválido → 400 parametro_invalido (handler.go:654-736).
  - Códigos/status de erro em internal/http/erros.go:17-82; corpo de erro {"erro":{"codigo","mensagem"}} e 401 nao_autorizado com Authorization: Bearer em internal/http/middleware/middleware.go:80-86,254-283.
  - Coleção e environment escritos com Write e conferidos estruturalmente na escrita (schema v2.1.0, info/auth/item, aspas escapadas); sem shell, nenhum parser JSON foi executado.
  - pipeline_validate DEL-016 (saída real):
    {"report":"docs-ai/deliveries/DEL-016/report.md","ok":false,"findings":[
     {"level":"ERRO","message":"checklist \"Final Checklist\": item não concluído (linha 47): `bin/pipeline-claude sensors` verde (todos os sensores do .pipeline-claude.json)"},
     {"level":"ERRO","message":"checklist \"Final Checklist\": item não concluído (linha 48): Escopo coberto por teste"},
     {"level":"ERRO","message":"checklist \"Final Checklist\": item não concluído (linha 50): Revisões exigidas pelos sinais concluídas"}]}
    Os três itens dependem de security-review e qa-validation, que ainda não rodaram; não foram marcados por falta de evidência. Nenhum outro achado (placeholders, seções, índice e diff passaram).

ASSUNCOES:
  - Postman envia "Authorization: Bearer <token>" com auth bearer da coleção e nada em noauth; Postman não foi executado aqui.
  - O "test" event (pós-resposta) e pm.environment.set/unset funcionam no Postman atual; o script de cursor usa unset quando proximo é null.
  - Status do report e da linha DEL-016 no INDEX mantidos em draft (não em revisão), porque security-review e QA ainda vão rodar; o passe final do docs-update ajusta para em revisão.
  - Alterações locais da DEL-015 (report e INDEX, merged) mantidas sem edição.

ARQUIVOS:
  - postman/biblioteca-bairro.postman_collection.json — novo: pastas Saúde, Livros, Usuários, Empréstimos; bearer {{token}} na coleção, noauth em healthz/readyz; query opcionais desligados; scripts só de captura (livro_id, usuario_id, emprestimo_id, cursor_livros, cursor_usuarios, cursor_emprestimos, cursor_emprestimos_usuario); sem exemplos.
  - postman/biblioteca-bairro.postman_environment.json — novo: base_url http://localhost:8080, token "" tipo secret, ids e cursores vazios.
  - .gitignore — padrão postman/*.local.postman_environment.json.
  - README.md — seção "Postman" (copiar para .local, importar, token só no current value, ordem sugerida, params desligados, manutenção manual).
  - docs-ai/03-MAPA-RAPIDO-MODULOS.md — diretório postman/ na estrutura.
  - docs-ai/DECISOES.md — D-22 (P1 a P4).
  - docs-ai/deliveries/DEL-016/report.md — campos preenchidos; checklist marcado só em fora de escopo, documentação e INDEX.

RISCOS:
  - Token gravado no environment versionado (e não na cópia .local) não é barrado pelo .gitignore; só pela revisão.
  - Postman Cloud sincroniza o "initial value": usuário que preencher o token no initial value o envia ao workspace Postman, mesmo sem commit.
  - Ligar "apos" depois que o script apagou o cursor envia "{{cursor_*}}" literal → 400 parametro_invalido (documentado na coleção).
  - Abrir empréstimo com usuario_id vazio gera corpo JSON inválido → 400 corpo_invalido (esperado, documentado pela ordem sugerida).
  - Coleção é cópia manual: rota nova diverge sem aviso (registrado na D-22 e no README).

SINAIS:
  SECURITY: sim — environment pode carregar BIBLIOTECA_API_TOKEN (invariante 5); revisar que nenhum valor secreto foi versionado e o padrão do .gitignore.
  SUPPLY_CHAIN: nao — sem go.mod/go.sum nem dependência.
  MIGRATION: nao — sem SQL.
  PERFORMANCE: nao — sem código.

PROXIMO: security-review
MOTIVO: SECURITY: sim obriga security-review antes do qa-validation; o validador só reprova os três itens de checklist que dependem dessas etapas. Depois do QA, o docs-update precisa de um passe final (marcar checklist com evidência, Security/QA Resultado, status em revisão no report e no INDEX, revalidar) antes do git-delivery.
