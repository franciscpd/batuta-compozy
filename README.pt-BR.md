# batuta-compozy

> 🇺🇸 [English version](README.md)

O Batuta é um projeto independente da comunidade, não um componente oficial
ou endossado do CompozyOS. É uma extensão Go para o
[CompozyOS](https://www.compozy.com/docs/)
([github.com/compozy/compozy](https://github.com/compozy/compozy)) que conduz
uma entrega sem escrever o código de produto diretamente.

Ele inclui o agente `batuta`, a skill `batuta-routing`, dois Loops
(`batuta-deliver` e `batuta-task`) e exatamente nove ferramentas hospedadas,
incluindo `ext__batuta__delivery_graph`. O Batuta escreve o SDD e as tasks
canônicas, coleta o inventário automático de executores e seleciona cada task
por domínio × complexidade.

![Roadmap de entrega do Batuta](docs/images/batuta-next-roadmap.png)

```text
conversa -> cards interativos de SDD -> tasks -> inventário -> grafo de roteamento
                                                        |
                                      até quatro worktrees de task
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

A compatibilidade é testada contra o commit do fonte Compozy
`382976d4b43274630a4b67445812fd4a0216dbcc`. Trate esse commit e o SDK
`v0.3.0-beta.21` como a baseline mínima de desenvolvimento compatível até uma
release pública mais nova do Compozy cobrir explicitamente a superfície de
grafo/ask.

Pré-requisitos:

- daemon CompozyOS compatível;
- extensão bundled `spec-cycle` habilitada;
- ao menos um modelo autenticado em `compozy provider models list`.

```bash
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes
compozy extension enable batuta
```

`--allow-unverified` é consentimento explícito para uma fonte da comunidade; a
verificação de integridade do archive continua ativa. Veja [docs/verify.md](docs/verify.md).

Atualizar: `compozy extension update batuta --allow-unverified --yes` · Remover: `compozy extension remove batuta --global`

Release publicada atual: [`v0.1.0-beta.3`](docs/releases/0.1.0-beta.3.md) · Próxima candidata: [`v0.1.0-beta.6`](docs/releases/0.1.0-beta.6.md).

## Uso

Abra uma sessão Compozy com `batuta` no workspace do projeto e descreva o
resultado. Durante o SDD, o Batuta usa cards interativos de esclarecimento só
para ambiguidade material de produto. Depois que as tasks são aprovadas, um
`batuta-task` em execução usa seu `ask` de entrega apenas para uma decisão
material ou valor externo indisponível; a resposta retoma a mesma task/worktree.

O Batuta então:

1. cria e aprova o SDD e o grafo canônico de tasks;
2. inventaria Compozy, Codex, OpenCode e Cursor Agent sem expor secrets, e faz
   escolhas de runtime por domínio × complexidade;
3. admite ondas de tasks dependentes com paralelismo seguro de no máximo quatro
   em worktrees de task isolados, nunca dois writers no mesmo worktree;
4. integra cada commit verificado no worktree canônico de integração; um conflito
   recebe reexecução canônica de conflito com nova tentativa imutável;
5. executa uma revisão final, publica e verifica automaticamente o HEAD exato
   revisado e abre ou reutiliza um PR. O merge continua manual.

O caminho saudável não possui gate humano rotineiro de publicação. Stops como
orçamento esgotado, evidência ambígua/desatualizada, cancelamento, publicação
bloqueada ou worktrees de diagnóstico retidos param o grafo e preservam a
evidência verdadeira no journal, sem iniciar outra geração.

Por exemplo, uma célula frontend pode selecionar Cursor Agent com o ID ACP vivo
exato `cursor/grok-4.6[effort=high,fast=true]`, enquanto backend usa outro
executor/modelo. Configuração externa informa capacidade, mas somente bindings
reportados pelo Compozy podem executar.

Os contratos completos estão em [docs/how-it-works.md](docs/how-it-works.md) e
[docs/architecture.md](docs/architecture.md).

## Limitações conhecidas

- As sessões dos executores não são visualmente aninhadas e permanecem
  active/idle após a conclusão terminal normal.
- Forge provider, remote ou credencial ausente vira blocker; o Batuta nunca
  trata uma compare URL como PR publicado.
- O pin de fonte compatível é uma baseline de desenvolvimento, não a afirmação
  de que toda build pública do Compozy já lançou a superfície de ação do grafo.

## Saiba mais

- [Como funciona](docs/how-it-works.md) · [Verificar e instalar](docs/verify.md)
- [Arquitetura](docs/architecture.md) ·
  [Estudo de caso](docs/case-studies/version-subcommand.md)
- [Contribuindo](CONTRIBUTING.md) · [Licença MIT](LICENSE)
