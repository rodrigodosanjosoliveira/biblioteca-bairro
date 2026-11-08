# Imagem da API biblioteca-bairro (DEL-012).
#
# Servidor:  docker run <imagem>
# Migração:  docker run <imagem> migrate up | migrate status
#
# Configuração só por variável de ambiente em tempo de execução (DATABASE_URL,
# BIBLIOTECA_API_TOKEN etc.); nada de segredo em ENV/ARG desta imagem.
#
# Imagens base fixadas pelo digest do índice multi-arch (a tag é só leitura).
# Builder na Go stable (D-13: go.mod é a versão mínima). Digests obtidos e
# verificados em 2026-11-06 (docs-ai/deliveries/DEL-012).

FROM golang:1.27.3-trixie@sha256:af1bfc2614ebb1dbd31af514b78740fd678850e76498762b425ecee4c5c6ea80 AS build

WORKDIR /src

ENV CGO_ENABLED=0 \
    GOTOOLCHAIN=local \
    GOFLAGS=-mod=readonly

COPY go.mod go.sum ./
RUN go mod download && go mod verify

COPY cmd/ ./cmd/
COPY internal/ ./internal/
COPY migrations/ ./migrations/

# -tags timetzdata é redundante com o import time/tzdata do código
# (internal/domain/multa.go e internal/storage/repositorio.go); fica como
# reforço. Não remova o import achando que a tag basta: os testes e o go run
# não passam por este build. Sem -ldflags "-s -w" para permitir
# govulncheck -mode=binary.
RUN go build -trimpath -buildvcs=false -tags timetzdata \
    -o /out/biblioteca-bairro ./cmd/biblioteca-bairro

FROM gcr.io/distroless/static-debian13:nonroot@sha256:25ac8f0c3b2334a9eb05452266bd4697864a8c8ef0ef71e5e6480d7f92868329

COPY --from=build /out/biblioteca-bairro /biblioteca-bairro

USER 65532:65532

EXPOSE 8080

ENTRYPOINT ["/biblioteca-bairro"]
