DELIVERY_ID: DEL-012
TASK_CODE: DEL-012
AGENT: architecture-guardrails
STATUS: ok

DECISOES_DESBLOQUEIO (repassadas pela sessão principal como decisão do usuário):
  - B1 = (b): builder com a imagem oficial golang na versão STABLE atual (X.Y.Z exato, último patch), fixada por digest do índice multi-arch, seguindo a D-13 (go.mod = versão mínima; CI na stable). go.mod NÃO muda (sem linha toolchain).
  - B2 = (a): o dev-implementation obtém e verifica os digests nesta máquina (Docker disponível).

ABORDAGEM:
  Vale integralmente a ABORDAGEM de docs-ai/deliveries/DEL-012/handoffs/02-architecture-guardrails.md, itens 1 a 6, com as decisões acima. Resumo do que muda/fecha:
  1. FROM do build: `golang:<stable X.Y.Z>-<variante Debian>@sha256:<digest do índice>`. O dev confirma qual é a stable em go.dev/dl ou pela lista de tags (ex.: `docker buildx imagetools inspect golang:<X.Y>` e conferir o patch) e usa o patch exato na tag, nunca só `X.Y`. Precisa ser >= 1.25.0 (go.mod:3); GOTOOLCHAIN=local garante que o binário é compilado com o Go da imagem.
  2. FROM final: `gcr.io/distroless/static-debian<NN>:nonroot@sha256:<digest do índice>`, variante Debian mais recente publicada.
  3. Procedimento obrigatório do dev-implementation para cada uma das duas imagens:
     a. `docker buildx imagetools inspect <ref>` → copiar o Digest do índice (não o de uma plataforma);
     b. `docker buildx imagetools inspect <ref>@sha256:<digest>` → confirmar linux/amd64 e linux/arm64 no índice;
     c. `docker pull <ref>@sha256:<digest>` → confirmar que resolve;
     d. só na distroless: `cosign verify gcr.io/distroless/static-debian<NN>:nonroot@sha256:<digest> --certificate-oidc-issuer https://accounts.google.com --certificate-identity keyless@distroless.iam.gserviceaccount.com`. Se o cosign não estiver instalado ou falhar por ambiente, REGISTRAR isso explicitamente no report (comando tentado e erro), sem pular em silêncio e sem instalar o cosign via go.mod;
     e. registrar no docs-ai/deliveries/DEL-012/report.md: referência completa com digest, data/hora UTC, comandos executados e trecho relevante da saída (Digest, plataformas, resultado do cosign ou motivo da ausência), e a versão Go stable escolhida com a fonte consultada.
  4. Demais itens inalterados: CGO_ENABLED=0, -trimpath, -buildvcs=false, -tags timetzdata, sem -s -w, GOFLAGS=-mod=readonly, `go mod download` + `go mod verify`, USER 65532:65532, EXPOSE 8080, ENTRYPOINT ["/biblioteca-bairro"] em forma exec sem CMD, .dockerignore allowlist (`*`, `!go.mod`, `!go.sum`, `!cmd/`, `!internal/`, `!migrations/`), README e RUNBOOK §10 pelo docs-update, verificação da imagem pelo QA (item 6 do handoff 02).
  5. Recomendado (não obrigatório nesta delivery): registrar em docs-ai/DECISOES.md uma decisão curta (D-18) "imagem de build segue a Go stable por digest; go.mod segue como mínimo; digests atualizados por delivery", feita pelo docs-update.

NAO_FAZER:
  - Não usar golang 1.25.x nem tag sem patch (golang:1.27, golang:latest); não usar digest de uma plataforma só; não usar tag sem digest.
  - Não alterar go.mod/go.sum (nem acrescentar `toolchain`) para casar com a imagem.
  - Não escrever digest que não tenha saído dos comandos do item 3 nesta máquina, nesta delivery; não deixar placeholder.
  - Não omitir do report a falta ou falha do cosign.
  - Todos os demais itens do NAO_FAZER do handoff 02 seguem valendo (ENTRYPOINT exec sem wrapper/flags, sem migrate no boot, sem segredo em ENV/ARG, sem imagem com shell nem root nem HEALTHCHECK, não omitir timetzdata, não excluir migrations/, sem compose/CI/registry/.gitattributes, sem token/DSN literal ou trust para a API no README, não editar .pipeline-claude.json).

CONFIRMADO:
  - Abordagem técnica e evidências do código em docs-ai/deliveries/DEL-012/handoffs/02-architecture-guardrails.md (CONFIRMADO daquele handoff: go.mod:3, DECISOES.md:158-162, delivery.yml:43/:69, ausência de cgo, go:embed em migrations/migrations.go:8 e :15, LoadLocation com panic em internal/domain/multa.go:21-25 e internal/storage/repositorio.go:47-49, sinais em cmd/biblioteca-bairro/main.go:81-85, shutdown de 20 s em main.go:25, /healthz e /readyz em internal/http/handler.go:114-128).
  - B1 = (b) é coerente com a D-13 (go.mod mínimo, CI stable) e não toca caminho protegido.
  - B2 = (a): a sessão principal informa que o Docker funciona nesta máquina.

ASSUNCOES:
  - A versão stable exata e os digests ainda não foram obtidos; serão definidos pelo dev-implementation com os comandos do item 3.
  - A disponibilidade do cosign nesta máquina não foi verificada; o caso de ausência está coberto (registrar).
  - test-writer sem teste Go a escrever (nenhum código Go muda); a verificação da imagem é por comando no QA.
  - As decisões B1/B2 chegaram pela sessão principal como decisão do usuário; não foram vistas diretamente.

ARQUIVOS:
  - nenhum (revisor sem escrita); previstos: Dockerfile, .dockerignore e registro dos digests no report (dev-implementation); README.md, docs-ai/RUNBOOK-MIGRACAO.md e opcionalmente docs-ai/DECISOES.md (docs-update).

RISCOS:
  - Stable no builder diverge do mínimo do go.mod: alguém compilando localmente com 1.25 obtém binário diferente da imagem (aceito pela D-13).
  - Digest fixo envelhece: sem renovate/Dependabot, a atualização depende de delivery futura.
  - Sem cosign, a procedência da distroless fica só pelo digest do registry oficial (registrado no report).
  - Demais riscos do handoff 02 seguem (imagem fora do CI, docker stop de 10 s contra shutdown de 20 s, Dockerfile fora de security_surface_paths, README copiado para produção).

SINAIS:
  SECURITY: sim — imagem de execução (não root, segredos fora das camadas, PID 1 e sinais) e documentação de credenciais e do modo trust; require_security ligada.
  SUPPLY_CHAIN: sim — duas imagens base de terceiros fora do go.mod, por digest, com verificação de plataformas e cosign (ou registro da ausência); go.mod/go.sum não mudam.
  MIGRATION: nao — sem migração nova nem mudança de esquema; só empacota `migrate up|status`.
  PERFORMANCE: nao — nenhum caminho de consulta ou HTTP alterado.

PROXIMO: test-writer
MOTIVO: bloqueios B1 e B2 resolvidos; data-model não se aplica (sem persistência), então o roteamento segue para test-writer, que deve registrar a ausência de teste Go viável e passar ao dev-implementation com esta ABORDAGEM e o NAO_FAZER.
