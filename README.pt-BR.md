# batuta-compozy

> 🇺🇸 [English version](README.md)

O Batuta é um projeto independente da comunidade, não um componente oficial
ou endossado do CompozyOS. É uma extensão Go para o
[CompozyOS](https://www.compozy.com/docs/)
([github.com/compozy/compozy](https://github.com/compozy/compozy)) que conduz
uma entrega sem escrever o código de produto diretamente.

Ele inclui o agente `batuta`, a skill `batuta-routing`, três Loops
(`batuta-deliver`, o interno `batuta-deliver-core` e `batuta-task`) e exatamente
nove ferramentas hospedadas, incluindo `ext__batuta__delivery_graph`. O Batuta
escreve o SDD e as tasks canônicas, coleta o inventário automático de executores
e seleciona cada task por domínio × complexidade.

![Roadmap de entrega do Batuta](docs/images/batuta-next-roadmap.png)

```text
conversa -> cards interativos de SDD -> tasks -> inventário -> grafo de roteamento
                                                        |
                  launcher batuta-deliver -> core interno -> até quatro worktrees
                                                        |
                       ask/retomada da task -> worktree canônico de integração
                                                        |
                       uma revisão final -> push + um PR automáticos -> verificar
```

## Instalar

As únicas releases remotas publicadas são `v0.1.0-beta.2` e
`v0.1.0-beta.3`; a beta.3 continua sendo a release atual no GitHub. Esta branch
prepara `v0.1.0-beta.6` como a próxima candidata. O SDK Go upstream
`v0.3.0-beta.21` é usado diretamente, sem `replace` nem dependência de fork.

Os contratos de build e lint são testados contra o commit do fonte Compozy
`382976d4b43274630a4b67445812fd4a0216dbcc`. Seu binário ainda se identifica
como beta.20 e não concluiu a qualificação de Start, portanto isso não é uma
afirmação de runtime compatível. A instalação runtime permanece bloqueada até
um binário Compozy beta.21 ou posterior cobrir a superfície de grafo/ask.

Pré-requisitos:

- daemon CompozyOS compatível;
- extensão bundled `spec-cycle` habilitada;
- ao menos um modelo autenticado em `compozy provider models list`.

```bash
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes
compozy extension enable batuta
```

`--allow-unverified` é consentimento explícito para uma fonte da comunidade; a verificação de integridade do archive continua ativa. Veja [docs/verify.pt-BR.md](docs/verify.pt-BR.md).

Atualizar: `compozy extension update batuta --allow-unverified --yes` · Remover: `compozy extension remove batuta --global`

Release publicada atual: [`v0.1.0-beta.3`](docs/releases/0.1.0-beta.3.pt-BR.md) ·
Próxima candidata: [`v0.1.0-beta.6`](docs/releases/0.1.0-beta.6.pt-BR.md).

## Uso

Abra uma sessão Compozy com `batuta` no workspace do projeto e descreva o
resultado. Durante o SDD, o Batuta usa cards interativos de esclarecimento só
para ambiguidade material de produto. Depois que as tasks são aprovadas, um
`batuta-task` em execução usa seu `ask` de entrega apenas para uma decisão
material ou valor externo indisponível; a resposta retoma a mesma task/worktree.

O Batuta então:

1. cria e aprova o SDD e o grafo canônico de tasks;
2. lê o catálogo vivo de providers/modelos do Compozy e evidência bounded
   opcional de Codex, OpenCode, Cursor Agent, Claude Code e Agy sem expor
   secrets, propõe a matriz domínio × complexidade e pede ao operador a
   confirmação dos runtimes e fallbacks exatos antes de qualquer mutação;
3. inicializa com segurança um workspace sem Git quando necessário e admite
   ondas de tasks dependentes com paralelismo seguro de no máximo quatro
   em worktrees de task isolados, nunca dois writers no mesmo worktree;
4. integra cada commit verificado no worktree canônico de integração; um conflito
   recebe reexecução canônica de conflito com nova tentativa imutável;
5. executa uma revisão final, publica e verifica automaticamente o HEAD exato
   revisado e abre ou reutiliza um PR. O merge continua manual.

A confirmação de roteamento é um preflight transparente, não um gate de
implementação ou publicação. O caminho saudável não possui gate humano
rotineiro de publicação. Stops como
orçamento esgotado, evidência ambígua/desatualizada, cancelamento, publicação
bloqueada ou worktrees de diagnóstico retidos param o grafo e preservam a
evidência verdadeira no journal, sem iniciar outra geração.

O Compozy é a única autoridade de execução de provider/modelo. Por exemplo,
uma célula frontend pode selecionar o par ACP vivo exato
`cursor/grok-4.6[effort=high,fast=true]`, enquanto backend usa outro par vivo.
Claude Code e Agy são enriquecedores opcionais de evidência, não backends de
execução; a ausência deles nunca remove um par vivo do Compozy. O inventário
automático não chama o comando de rede `agy models`.

O Compozy atual exibe as sessões dos executores na hierarquia pai/filho e
encerra as sessões run-agent após o assentamento terminal.

`start_delivery` e `recover_delivery` retornam o ID público durável do run
launcher `batuta-deliver`; o journal armazena o ID do launcher. A tool protegida
valida seu filho core `batuta-deliver-core` interno exato, portanto operadores
nunca fornecem nem reconciliam um ID de run core.

Os contratos completos estão em
[docs/how-it-works.pt-BR.md](docs/how-it-works.pt-BR.md) e
[docs/architecture.pt-BR.md](docs/architecture.pt-BR.md).

## Limitações conhecidas

- Forge provider, remote ou credencial ausente vira blocker; o Batuta nunca
  trata uma compare URL como PR publicado.
- O pin de fonte compatível é uma baseline de desenvolvimento, não a afirmação
  de que toda build pública do Compozy já lançou a superfície de ação do grafo.

## Saiba mais

- [Como funciona](docs/how-it-works.pt-BR.md) ·
  [Verificar e instalar](docs/verify.pt-BR.md)
- [Arquitetura](docs/architecture.pt-BR.md) ·
  [Estudo de caso](docs/case-studies/version-subcommand.pt-BR.md)
- [Contribuindo](CONTRIBUTING.pt-BR.md) · [Licença MIT](LICENSE)
