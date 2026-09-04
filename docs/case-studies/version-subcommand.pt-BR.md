# Estudo de caso do subcomando de versão

[Versão em inglês](version-subcommand.md)

Este estudo de caso registra uma jornada limitada e reproduzível de entrega do
Batuta. Ele descreve resultados observados, não um benchmark nem uma afirmação
sobre toda carga de trabalho.

## Pergunta

A solicitação sanitizada foi: “Adicione uma feature mínima de CLI que preserve
o requisito literal `todo 1.0.0`. Não escreva código antes que a especificação e
as tasks sejam aprovadas.”

## Ambiente

A execução usou um candidato Batuta beta.2 com o commit de fonte compatível do
CompozyOS `a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c`, `spec-cycle` 0.4.0
empacotado e um repositório fixture limpo. Este estudo omite deliberadamente as
identidades de provider, modelo e máquina.

## Gate de preferência

A chave exata de preferência do workspace não existia. O operador escolheu
false, e o Batuta persistiu e releu `auto_commit=false` antes de começar o
planejamento.

## Especificação

`cy-create-spec` produziu `_spec.md`, `_user_stories.md`, `_dx.md` e `_tests.md`.
Nenhum `_uiux.md` foi necessário porque a solicitação não tinha superfície Web.

## Tasks e preflight

`cy-create-tasks` produziu uma task backend. Uma chamada direta de
`ext__spec_cycle__import_tasks` retornou uma contagem positiva, e o requisito
literal `todo 1.0.0` permaneceu inalterado.

## Entrega

Um dry-run antecedeu o dispatch real de `batuta-deliver`. A entrega composta
então chamou `implement-tasks` e, em seguida, `review-and-fix`. As três
execuções alcançaram exatamente o estado terminal `done`.

## Resultado observável

Somente `README.md`, `src/cli.py` e `tests/test_cli.py` mudaram. A fixture
reportou 9/9 testes aprovados. Nenhum commit foi criado porque
`auto_commit=false`.

## O que isso comprova

Esta jornada demonstra orquestração limitada, preservação literal de requisitos,
ordenação implementação-antes-da-revisão e retorno terminal dirigido por
eventos.

## O que isso não comprova

Ela não estabelece desempenho geral, custo, superioridade de provider nem
compatibilidade estável. Na versão beta.2 observada, sessões dos executores não
eram visualmente aninhadas e permaneciam `active/idle` após a conclusão
terminal normal. O Compozy atual já oferece hierarquia pai/filho e encerramento
terminal de sessões run-agent; esse comportamento posterior não faz parte da
evidência desta jornada histórica.

## Reprodução

Comece pelo [README em português](../../README.pt-BR.md), pelo
[guia de arquitetura](../architecture.pt-BR.md) e pelas
[notas da release beta.2](../releases/0.1.0-beta.2.md). Inspecione o
[Loop de entrega](../../loops/batuta-deliver/loop.yaml) e a
[skill de roteamento](../../resources/skills/batuta-routing/SKILL.md), depois use
a [release v0.1.0-beta.2](https://github.com/batuta-ai/compozy/releases/tag/v0.1.0-beta.2).
O padrão público de artefatos de task é `.compozy/tasks/$slug/task_*.md`.
