DELIVERY_ID: DEL-016
TASK_CODE: DEL-016
AGENT: security-review
STATUS: ok

SUPERFICIE_REVISADA:
  - postman/biblioteca-bairro.postman_environment.json (lido inteiro)
  - postman/biblioteca-bairro.postman_collection.json (lido inteiro, 497 linhas, todos os 7 scripts "test")
  - .gitignore (arquivo inteiro e diff)
  - README.md (diff: seção "Postman")
  - docs-ai/DECISOES.md (diff: D-22), docs-ai/03-MAPA-RAPIDO-MODULOS.md (diff)
  - docs-ai/deliveries/DEL-015/report.md e INDEX.md (diff: só status)
  - docs-ai/deliveries/DEL-016/ (grep por DSN com credencial, BIBLIOTECA_API_TOKEN=valor, Bearer literal: nenhum)
  - internal/http/handler.go:132-149 (fonte de verdade das rotas e do middleware Auth, para conferir noauth)
  - Sem código Go, go.mod ou go.sum no diff (pipeline_diff, 11 arquivos).

CONFIRMADO:
  - govulncheck via pipeline_sensors names=["vuln"] (saída real): ok=true, exit_code=0, "No vulnerabilities found."
  - Environment: token com "value": "" e "type": "secret" (linhas 11-16); base_url http://localhost:8080; ids e cursores vazios; nenhum outro valor preenchido.
  - Nenhum segredo versionado: grep em postman/ por postgres://, password, senha=, BIBLIOTECA_API_TOKEN=valor, "bearer <literal>" → sem ocorrência; a auth da coleção usa só "{{token}}" (linhas 8-17).
  - .gitignore: postman/*.local.postman_environment.json casa exatamente com a cópia do README (postman/biblioteca-bairro.local.postman_environment.json); nenhuma regra de negação (!) posterior a reabre. Glob em postman/ mostra só os dois arquivos versionados, nenhuma cópia .local presente.
  - Scripts: só pm.environment.set/unset de livro_id, usuario_id, emprestimo_id e cursor_* (valores da resposta); nenhum console.log, nenhum pm.collectionVariables/globals/variables.set, nenhuma leitura de token nem de request.headers. Nenhum script pre-request.
  - Auth: noauth só em Healthz e Readyz (linhas 26-28, 43-45); demais 13 requisições herdam bearer da coleção. Bate com handler.go:135-149 (healthz/readyz sem auth, as 13 rotas restantes em auth(...)). 15 rotas no handler = 15 requisições na coleção.
  - README instrui token só no current value e export só por cima da cópia .local; D-22 registra que o arquivo versionado é protegido só pela revisão.
  - Descrições da coleção não expõem caminho, query SQL nem stack; só códigos de erro públicos da API. Corpo de usuário de exemplo é fictício ("Usuário Exemplo").

ACHADOS:
  - BAIXO: o .gitignore não impede gravar o token no environment versionado (nome sem .local), nem o Postman Cloud de sincronizar o "initial value" se o usuário o preencher. Mitigado por documentação (README passo 3, D-22) e por revisão; já em RISCOS do docs-update. Sem ação bloqueante; opcional futuro: checagem no CI de que "token" segue vazio no environment versionado.
  - BAIXO: base_url padrão em http://; apontar para host remoto sem TLS mandaria o Bearer em claro. Padrão é localhost, aceitável; informativo.
  - Nenhum CRITICO, ALTO ou MEDIO.

ASSUNCOES:
  - Postman não executado; comportamento de secret/current value assumido conforme documentação do Postman.

ARQUIVOS:
  - nenhum alterado (revisão só leitura).

RISCOS:
  - Os mesmos do handoff 02-docs-update (token no arquivo versionado barrado só por revisão; sincronização do initial value no Postman Cloud).

SINAIS:
  SECURITY: sim — revisado, sem achado bloqueante.
  SUPPLY_CHAIN: nao — sem go.mod/go.sum.
  MIGRATION: nao
  PERFORMANCE: nao

PROXIMO: qa-validation
MOTIVO: SUPPLY_CHAIN: nao; achados só BAIXO. Resultado para a seção Security do report: aprovado, govulncheck "No vulnerabilities found.", nenhum segredo versionado, token vazio tipo secret, .gitignore cobre a cópia .local, só healthz/readyz sem auth.
