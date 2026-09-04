# Verificando e instalando o Batuta

[Versão em inglês](verify.md)

A release remota publicada continua sendo `v0.1.0-beta.3`; `v0.1.0-beta.6` é o
próximo candidato e ainda não é uma tag nem uma release. Sua baseline de
compatibilidade é o SDK Go oficial do Compozy `v0.3.0-beta.21`; o CI compila e
testa contra o commit de fonte `34208e9990622ee62e9a5cf114386273ae6abfa0`, a
release `v0.3.0-beta.22`.

O piso público de runtime continua sendo `v0.3.0-beta.21`. Uma build de fonte
que se identifica como beta.20 permanece abaixo desse piso, mesmo quando sua
fonte inclui alguns contratos posteriores; lint bem-sucedido, sozinho, não é
compatibilidade de runtime.

## O que `--allow-unverified` significa

Instalações diretas do GitHub estão no nível `unverified` do registro do
CompozyOS. A política viva `extensions.trust.allow_unverified` e seu
consentimento explícito `--allow-unverified` são necessários; `--yes` apenas
pula a confirmação. A flag não desativa verificações de integridade. Inspecione
a proveniência após a instalação:

```bash
compozy extension provenance batuta -o json
```

A proveniência esperada do GitHub inclui `installed_from: "github"` e, quando a
release fornece um sidecar do archive, `digest_matched: true`.

## Instalação pelo GitHub

```bash
compozy extension install github:batuta-ai/compozy --allow-unverified --yes
compozy extension enable batuta
```

O pin publicado atual é
`github:batuta-ai/compozy@v0.1.0-beta.3`. Não apresente beta.6 como
instalável até que sua tag e release remotas existam.

```bash
compozy extension update batuta --allow-unverified --yes
compozy extension remove batuta --global
```

## Verificação do candidato e do desenvolvimento local

`scripts/stage-extension.sh` cria uma geração candidata imutável. Suas fontes
Go em staging incluem `go.mod`, `go.sum`, o agente `batuta`, a skill
`batuta-routing` e os três arquivos de Loop:

```text
agents/batuta/AGENT.md
resources/skills/batuta-routing/SKILL.md
loops/batuta-deliver/loop.yaml
loops/batuta-deliver-core/loop.yaml
loops/batuta-task/loop.yaml
```

Ele exclui deliberadamente planos, specs, relatórios de QA e artefatos de SDD do
pacote da extensão. Faça build e valide a geração em staging, não o checkout de
fonte:

```bash
stage=$(mktemp -d)
scripts/stage-extension.sh "$stage"
compozy extension build "$stage" -o json
compozy extension validate "$stage" -o json
```

Para uma instalação local completa, use `scripts/republish.sh` com um
`COMPOZY_HOME` isolado. Ele prepara a mesma geração de fonte Go, valida,
instala e exige este inventário exato: um agente `batuta`, os Loops
`batuta-deliver` público, `batuta-deliver-core` interno e `batuta-task`,
`batuta-routing` e todas as nove tools hospedadas do Batuta, incluindo
`ext__batuta__delivery_graph`.

O pin exato de fonte é um piso de compatibilidade de desenvolvimento; valide a
superfície pública do daemon que será operado. Uma instalação por caminho local
não pode usar `compozy extension update`; faça novamente build e instalação da
geração validada.

A disponibilidade de provider/modelo vem somente do catálogo vivo do Compozy.
A extensão pode enriquecer essa evidência com probes locais limitados de Codex,
OpenCode, Cursor Agent, Claude Code e Agy. Ela não faz login, instalação,
refresh de configuração nem chama o comando `models` do Agy, autenticado e
baseado em rede. A execução viva com Claude/Agy só é qualificada quando o daemon
reporta o par exato como `available_live` e a proveniência do runtime filho o
confirma.

## Verificação da release

O workflow de release aceita somente o SHA completo do candidato e um SemVer
beta ainda não usado. Ele verifica o candidato pelo CI antes de uma tag anotada,
prepara o mesmo pacote imutável, publica e então reinstala pelo GitHub para
verificar inventário e proveniência. Ele nunca empacota planos apenas de fonte
nem troca o daemon de um workspace vivo durante a verificação da release.
