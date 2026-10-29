.PHONY: tools build test vet check validate doctor

# Versão do pipeline-claude usada por este projeto: instalada em bin/ por
# `make tools`, fora do go.mod (a ferramenta de governança não entra no grafo de
# dependências do produto). O doctor confere o binário contra esta versão.
PIPELINE_CLAUDE_VERSION := v1.0.0-rc.2

tools:
	GOBIN=$(CURDIR)/bin go install github.com/rodrigodosanjosoliveira/pipeline-ai-claude/cmd/pipeline-claude@$(PIPELINE_CLAUDE_VERSION)

build:
	go build -o bin/biblioteca-bairro ./cmd/biblioteca-bairro

test:
	go test ./...

vet:
	go vet ./...

# Os gates do projeto, declarados em sensors no .pipeline-claude.json.
check:
	bin/pipeline-claude sensors

# Uso: make validate DELIVERY=DEL-NNN
validate:
	@test -n "$(DELIVERY)" || (echo "informe DELIVERY=<id da delivery>"; exit 2)
	bin/pipeline-claude validate $(DELIVERY)

doctor:
	bin/pipeline-claude doctor
