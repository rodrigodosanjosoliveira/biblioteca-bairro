# Runbook de migração — biblioteca-bairro

> Procedimentos de quem opera `biblioteca-bairro migrate up|status` (D-16). As
> decisões e o motivo de cada uma estão em `docs-ai/DECISOES.md`; aqui ficam só
> os passos e as regras operacionais. Quem escreve migração lê a seção 6.

## 1. Comandos

- `biblioteca-bairro migrate up`: aplica as migrações pendentes, um arquivo por
  transação, só roll forward.
- `biblioteca-bairro migrate status`: lista cada versão (`aplicada`,
  `pendente`, `divergente` ou `desconhecida`), somente leitura, sem lock.
- Variáveis: `DATABASE_URL` (obrigatória), `BIBLIOTECA_MIGRATE_LOCK_TIMEOUT`
  (padrão 5s, de 100ms a 60s) e `BIBLIOTECA_MIGRATE_STATEMENT_TIMEOUT` (padrão
  60s, de 1s a 30min). Não exige `BIBLIOTECA_API_TOKEN`.
- Códigos de saída: 0 sucesso, 1 falha (config, conexão, lock ocupado,
  divergência, baseline, SQL, estado inconsistente, cancelamento), 2 uso
  incorreto (qualquer outro argumento, inclusive no binário do servidor).
- `migrate up` roda uma vez por deploy (job ou etapa), nunca em todo pod. Se o
  lock estiver ocupado (`ErrLockOcupado`), sai com código 1 como qualquer erro.

## 2. Baseline manual de banco que já tem a 0001

O executor recusa registro vazio com schema que já tem relações
(`ErroBancoSemRegistro`) e não tem subcomando `baseline`. Para adotar um banco
existente:

1. Conferir que o esquema real é o da 0001 (`pg_dump -s` contra
   `migrations/0001_esquema_emprestimo.up.sql`).
2. Rodar por `psql` o `migrations/controle/0000_migracao_aplicada.up.sql`
   embutido no binário, não um DDL equivalente escrito à mão: um
   `DEFAULT CURRENT_TIMESTAMP`, por exemplo, seria recusado.
3. Na mesma transação:
   `INSERT INTO migracao_aplicada (versao, nome, checksum) VALUES (1,
   'esquema_emprestimo', '<sha256sum do 0001_esquema_emprestimo.up.sql do commit
   implantado>')`.
4. Rodar `biblioteca-bairro migrate status`: a versão 1 deve sair `aplicada`.

## 3. Down manual

O executor só faz `up`. Para reverter, rode o `.down.sql` por `psql`, da maior
para a menor versão, e na mesma transação de cada down faça
`DELETE FROM migracao_aplicada WHERE versao = N`. O down da tabela de controle
(`migrations/controle/0000_migracao_aplicada.down.sql`) só depois de todos os
outros, com o schema sem relações. Down destrutivo segue a D-13: reverter o
deploy antes e ter `pg_dump`.

## 4. Rede e papéis

- DSN direta ao primário. Nunca por pgbouncer em modo transação: quebra o
  advisory lock, o `set_config` local e o CancelRequest. Nunca por réplica: o
  `status` funciona (é READ ONLY), o `up` não.
- Papel de DDL separado do papel de runtime. O servidor não lê
  `migracao_aplicada`, e `ALTER DEFAULT PRIVILEGES` não pode conceder escrita no
  registro ao runtime.
- O papel de DDL não pode ser superuser: com superuser a conferência de dono da
  tabela de controle (`pg_has_role`) é sempre verdadeira (risco R1 da D-16).
- `sslmode=verify-full` na `DATABASE_URL` de migração; o padrão do pgx é
  `prefer` (risco B2).
- A chave do advisory lock é constante: qualquer sessão do banco pode segurá-la e
  bloquear o `up` (risco B3).

## 5. Schema alvo e tabela de controle

- O executor atua em `current_schema()`. Declare o `search_path` na DSN de
  migração e use schema dedicado quando extensões (`pg_stat_statements`,
  `postgis`) instalarem relações no schema corrente: relação alheia trava o
  `migrate up` com `ErroBancoSemRegistro`.
- Controle adotado ou restaurado: o dono tem de ser o papel de DDL ou um papel
  de que ele seja membro (`ALTER TABLE migracao_aplicada OWNER TO
  <papel_de_DDL>` depois de um `pg_restore`). Se um flag de trigger, regra ou
  herança ficar preso depois de remover o objeto, `VACUUM migracao_aplicada`.
- `ErroControleInvalido` é uma mensagem única e não diz o motivo. Causas
  possíveis: dono fora do alcance do papel, gatilho, regra, RLS, partição ou
  herança, coluna gerada ou identidade, default diferente de `now()` em
  `aplicada_em`, tabela que não é `r`.

## 6. Regras para os arquivos de migração

- Não alterar o `search_path` nem o papel da sessão. `SET LOCAL`, `SET ROLE` e
  `set_config(..., true)` são recusados com `ErroSessaoAlterada`, a menos que o
  arquivo os restaure antes do fim. Use nomes qualificados e `ALTER ... OWNER
  TO`.
- Sem `BEGIN`, `COMMIT`, `ROLLBACK` nem `SAVEPOINT` próprios.
- A palavra `concurrently` é recusada em qualquer ponto do `.up.sql`, inclusive
  em comentário e literal. Índice em tabela com dados é procedimento manual
  (`psql` mais registro manual).
- Alterar qualquer byte, inclusive comentário, de um `.up.sql` já aplicado
  quebra o checksum (`ErroChecksumDivergente`): a correção é uma migração nova.
- As regras da D-13 seguem: FK nova `NOT VALID` com `VALIDATE CONSTRAINT` em
  outro arquivo, `NOT NULL` com `DEFAULT` constante, expand/contract.
- Nomes `NNNN_nome.{up,down}.sql`, versões 1..N sem lacuna, sempre em par.

## 7. Timeouts

`statement_timeout` vale por COMANDO numa mensagem simple-Query com vários
comandos: a transação do arquivo não tem teto total, e N comandos lentos podem
ocupar N vezes o prazo segurando os `ACCESS EXCLUSIVE` tomados no início.
`lock_timeout` vale por aquisição de lock. Por isso, um DDL pesado por arquivo e
`VALIDATE CONSTRAINT` em arquivo próprio.

## 8. Cancelamento

SIGINT ou SIGTERM cancela a operação (saída 1, "migrate: operação cancelada") e
o executor envia o CancelRequest e espera a limpeza do pgx antes de sair. Um
segundo SIGINT mata o processo sem CancelRequest: o backend segue até o
`statement_timeout`, e a transação é desfeita por não haver COMMIT (risco B4).

## 9. Fim de linha

Sem `.gitattributes`, um checkout com conversão de fim de linha (CRLF) muda o
checksum dos `.up.sql`. Sugestão ao humano, fora do escopo da DEL-008:
`.gitattributes` com `migrations/**/*.sql text eol=lf`.

## 10. Mudança de comportamento do binário

Argumento desconhecido agora sai com código 2 e imprime
`uso: biblioteca-bairro [migrate up | migrate status]`; antes os argumentos eram
ignorados. Vale para qualquer entrypoint que passe flags ao binário (o
repositório não tem Dockerfile).
