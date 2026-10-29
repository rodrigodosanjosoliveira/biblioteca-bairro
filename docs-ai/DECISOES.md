# Decisões — biblioteca-bairro

> Registro das decisões do projeto, com o motivo de cada uma. Decisão nova entra
> com número sequencial (`D-NN`), o que foi decidido e o porquê; decisão
> revista ganha uma nova entrada que cita a anterior.

---

## D-01 — Go no backend, stdlib primeiro

Formato único imposto pelo `gofmt`, linguagem que limita abstrações complexas
de propósito e stdlib ampla, que reduz a superfície de dependências externas.

**Motivo:** com IA, o gargalo se desloca da geração para a verificação; código
uniforme e com poucas dependências é mais fácil de verificar.

## D-02 — API em Go com PostgreSQL

Um único banco por projeto. Esquema evolui só por migração versionada em
`migrations/`, com reversão.

## D-03 — Testes com a stdlib `testing`, em tabela

Biblioteca de asserção é opcional e exige decisão registrada aqui.

**Motivo:** cada dependência que entra precisa ser justificada e revisada; o
padrão em tabela já cobre bem a necessidade.

## D-04 — Fluxo por subagentes com ferramentas restritas

Toda demanda passa por `pm-triage` e segue o fluxo do `CLAUDE.md`. Revisores
não têm ferramenta de escrita; só os subagentes de `editor_agents` editam
caminho protegido.

**Motivo:** no Claude Code a identidade do subagente (`agent_type`) e as
ferramentas de cada um são aplicadas pelo runtime, não só pelo prompt.

## D-05 — Gates determinísticos

`go build`, `go vet` e `go test` como sensores (`bin/pipeline-claude sensors`);
`govulncheck` nas revisões de segurança e de dependências; fuzz com
`-fuzztime` para parsing e validação.

**Motivo:** refatoração iterativa de agente sem validação externa degrada
rápido; o gate é a validação externa.

## D-06 — Segurança antes do QA; release antes da documentação

`security-review` roda depois do `code-reviewer` e antes do `qa-validation`;
`release-ops`, quando habilitado, vem antes do `docs-update`.

**Motivo:** achado de segurança muda a implementação, e revalidar o QA depois
sai mais caro.

## D-07 — `require_security` começa desligada e o validador a força

O triage liga a flag quando há superfície de segurança; o validador a exige
quando o diff toca `security_surface_paths` ou `go.mod`/`go.sum`
(`auto_flags`).

**Motivo:** esquecer no triage não pode deixar a delivery passar sem revisão.

## D-08 — Entrega por branch e Pull Request

Branch `task/{código}`, commit `{type}({scope}): {resumo} [{código}]`, push
direto na `main` bloqueado pelo `.githooks/pre-push`.

## D-09 — Valores monetários com `shopspring/decimal` v1.4.0

Valores em dinheiro (começando pela multa por atraso, DEL-001) usam
`github.com/shopspring/decimal`, fixada em v1.4.0. A recomendação técnica era
`int64` em centavos, só com a stdlib; o usuário preferiu a biblioteca decimal.
Licença MIT, sem dependência transitiva (o `go.sum` só tem a própria
`decimal`), aprovada pelo `supply-chain-review` (`go mod verify` e
`govulncheck` limpos). A v1.6.0 saiu em 18/10/2026, mas a v1.4.0 foi mantida
por ser madura; a atualização fica para uma delivery futura.

**Motivo:** aritmética decimal exata, sem erro de arredondamento, com API
legível para regras monetárias.

**Consequências:** `decimal.Decimal` aparece na API pública de
`internal/domain`, que passa a depender de pacote externo. A persistência
futura deve usar coluna `NUMERIC` no PostgreSQL. Em JSON o tipo serializa como
string por padrão; o futuro endpoint precisa assumir isso no contrato.

## D-10 — Datas de negócio pela data civil em `America/Sao_Paulo`

Regras que contam dias (como a multa por atraso, DEL-001) usam a data civil em
`America/Sao_Paulo`, qualquer que seja o fuso do `time.Time` recebido. A base
de fusos é embutida no binário com `import _ "time/tzdata"`, e a diferença em
dias é calculada entre meias-noites UTC reconstruídas a partir da data civil
(divisão do Unix por 86400).

**Motivo:** o resultado não pode depender do fuso do servidor nem da base de
fusos do sistema operacional, e dias de 23h ou 25h no horário de verão não
podem gerar erro de um dia.

**Consequências:** o binário cresce cerca de 450 KB por causa do
`time/tzdata`. Feriados e fusos configuráveis ficam fora até decisão nova.
