# Índice de deliveries

Toda delivery precisa estar registrada na tabela abaixo: o validador procura o
`delivery_id` nas células e reprova o report que não encontrar.

Crie deliveries com `bin/pipeline-claude delivery new --title "<título>"`, a
ferramenta MCP `pipeline_delivery_new` ou a skill `/nova-delivery`: elas geram o
próximo ID da sequência, criam o report e acrescentam a linha no fim da tabela.
Não escreva IDs de exemplo neste arquivo: qualquer célula com ID conta para a
sequência.

Status: draft, em revisão, merged, revertida.

| ID | Título | Status | Data |
|---|---|---|---|
| DEL-001 | Multa por atraso na devolução | merged | 2026-10-29 |
| DEL-002 | Corrigir nome do módulo no go.mod | merged | 2026-10-29 |
| DEL-003 | Modelo de empréstimo: Livro, Usuário e Empréstimo | merged | 2026-10-29 |
| DEL-004 | Persistência do modelo de empréstimo em PostgreSQL: migrações, repositório, pgx e ReidratarEmprestimo | merged | 2026-10-29 |
| DEL-005 | Multa por atraso no empréstimo com CalcularMulta | merged | 2026-10-29 |
| DEL-006 | Camada HTTP do empréstimo: internal/http, internal/config e boot com pool PostgreSQL | merged | 2026-10-29 |
| DEL-007 | Atualizar o pipeline-claude de v1.0.0-rc.2 para v1.0.0-rc.4 | merged | 2026-10-29 |
| DEL-008 | Executor de migração em produção | em revisão | 2026-10-30 |
