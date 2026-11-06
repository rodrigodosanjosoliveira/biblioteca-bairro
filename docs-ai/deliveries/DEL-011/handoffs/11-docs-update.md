DELIVERY_ID: DEL-011
TASK_CODE: DEL-011
AGENT: docs-update
STATUS: ok

CONFIRMADO:
  - Handoffs 01 a 10 lidos via pipeline_handoff_get (DEL-011). O 10-qa-validation (RESULTADO: ok) substitui o 09 (parcial).
  - pipeline_validate target=DEL-011, saída real:
      {
        "report": "docs-ai/deliveries/DEL-011/report.md",
        "ok": true,
        "findings": []
      }
  - Report preenchido com Edit, campo a campo (sem Write): front matter `status: em revisão`, `require_security: true` (security-review rodou; seção Security com `Resultado`), `require_release_ops: false` (release-ops não está em enabled_modules e não rodou). Nenhum `<preencher>` restante nos campos; Final Checklist marcado só com evidência: sensores verdes (10), escopo coberto e sem scope creep (09/10), revisões code/migration/security concluídas (06/07/08), docs (este passo), INDEX (01).
  - QA registrado como "Resultado: ok", citando que a prova com banco veio da execução da sessão principal (comando e ambiente do 10) e que o sensor sem banco tem 200 pulados; requisito de auth trust para internal/migrate registrado.
  - Símbolos exportados novos (LimiteRenovacoes, PrazoRenovacaoDias, ErrEmprestimoVencido, ErrLimiteRenovacoes, ErrRenovacoesInvalidas, ReidratarEmprestimo, Renovar, Renovacoes, RegistrarRenovacao) já têm comentário de documentação (Grep em internal/); código de negócio não editado.
  - Mudanças da DEL-010 no working tree (report e linha no INDEX.md) preservadas, por decisão do usuário; registradas no report como parte desta entrega.

ASSUNCOES:
  - As sugestões do code-reviewer (invariante do CAS e limite duplicado no CHECK) foram atendidas na D-17, não em comentário de código (docs-update não edita internal/ nem migrations/).
  - docs-ai/RUNBOOK-MIGRACAO.md não foi alterado: a ordem de deploy/rollback específica da 0002 ficou na D-17 e na seção Security do report; o procedimento geral de down manual já está no runbook (§3).

ARQUIVOS:
  - docs-ai/DECISOES.md — nova D-17 (regra de renovação, fronteira de vencido, CAS e invariante, limite no CHECK, contrato HTTP, deploy/rollback, requisito trust nos testes de migrate)
  - docs-ai/03-MAPA-RAPIDO-MODULOS.md — linhas DEL-011 de internal/domain, internal/storage, internal/http e migrations; rota nova na linha HTTP da DEL-006
  - docs-ai/deliveries/DEL-011/report.md — todas as seções preenchidas, checklist marcado, status em revisão
  - docs-ai/deliveries/INDEX.md — só a coluna Status da DEL-011: draft → em revisão
  - CLAUDE.md — sem mudança (estrutura e invariantes inalterados)

RISCOS:
  - Contrato da API muda de forma aditiva (rota nova, campo `renovacoes`, 409 `emprestimo_vencido` e `limite_renovacoes_atingido`): citar no PR.
  - Ordem de deploy: migrate up 0002 antes do binário novo; rollback com binário antigo antes do down 0002 (perde o contador).
  - Sensor `test` sem banco (200 pulados); prova com banco só na execução manual da sessão principal.
  - O diff inclui docs-ai/deliveries/DEL-010/report.md e a linha da DEL-010 no INDEX.md (decisão do usuário); o git-delivery deve mencioná-los no commit/PR.
  - D-17 traz decisões técnicas a confirmar pelo humano no PR.

SINAIS:
  SECURITY: sim — revisado no 07 (aprovado, dois BAIXO informativos); require_security true no report.
  SUPPLY_CHAIN: nao — go.mod/go.sum fora do diff.
  MIGRATION: sim — 0002 aprovada no 08 e provada com banco (10).
  PERFORMANCE: nao — UPDATE e SELECT por PK; índice parcial existente.

PROXIMO: git-delivery
MOTIVO: documentação coerente, report completo e validador aprovado (ok: true, findings vazio); segue para branch task/DEL-011, commit e PR.
