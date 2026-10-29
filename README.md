# biblioteca-bairro

Projeto **fictício** de demonstração do
[pipeline-claude](https://github.com/rodrigodosanjosoliveira/pipeline-ai-claude):
uma API em Go para o acervo e os empréstimos de uma biblioteca de bairro.

O domínio é inventado; o que importa aqui é a governança. Este repositório foi
criado pelo `pipeline-claude init` e mostra, num projeto real e público, o fluxo
de desenvolvimento assistido por agentes no Claude Code:

- subagentes em `.claude/agents/` (triage, guardrails, testes, implementação,
  revisores só leitura, QA, docs e entrega);
- hook `PreToolUse` que bloqueia edição de caminho protegido fora dos agentes
  autorizados, e `fmt-hook` que formata o Go editado;
- servidor MCP do pipeline (`.mcp.json`) com diff de revisão, sensores e
  criação de delivery;
- report de cada delivery em `docs-ai/deliveries/`, validado no CI pelo
  `pipeline-claude validate`;
- gates do projeto (`vet`, `test`, `build`, `govulncheck`, `go mod verify`)
  declarados em `.pipeline-claude.json` e rodados por `make check` e no CI.

## Como usar

Requer Go (versão do `go.mod` ou mais nova), `git`, `gh` e o Claude Code.

```sh
make tools                    # instala bin/pipeline-claude na versão do Makefile
bin/pipeline-claude doctor    # diagnóstico do ambiente e do projeto
make check                    # gates
```

Uma demanda nova, numa sessão do Claude Code aberta aqui:

```text
/nova-delivery <título>       # cria DEL-NNN e o report
/fluxo <demanda>              # conduz os subagentes do triage à entrega
```

A branch é `task/<ID>` e a PR roda a validação da delivery no CI.

## Deliveries

O índice fica em [`docs-ai/deliveries/INDEX.md`](docs-ai/deliveries/INDEX.md).