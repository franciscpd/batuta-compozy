# Batuta no CompozyOS — design

Design aprovado em conversa, 2026-08-11. Este repositório é a nova casa principal do
Batuta; o plugin Claude (`~/Projects/batuta`) e o `batuta-cli` viram legado/referência.

## O problema

O Batuta v1 existe como plugin do Claude Code (ciclo em prosa: classifica → roteia →
brief → delega → verifica → commit atômico) e como um CLI em Go que reimplementa esse
ciclo headless. O CompozyOS, porém, já entrega nativamente quase tudo que essas duas
encarnações garantem por disciplina: Loops deterministicos (goal → verify → settle),
roteamento de runtime por complexidade, gates de verificação com revise/escalação,
run history durável e uma esteira completa de skills de desenvolvimento (`cy-create-prd`
→ `cy-final-verify`).

Reimplementar isso seria chover no molhado. O objetivo desta conversão é um só:
**habilitar o desenvolvimento de um sistema inteiro em loop**, aproveitando o que o
compozy já tem, e empacotar apenas o que é genuinamente do Batuta — a opinião.

## O que é "a opinião do Batuta"

1. **Quem rege não toca** — o maestro nunca escreve código; classifica, monta contexto
   e despacha para o executor mais barato que dá conta.
2. **Roteamento por custo/complexidade** — tabela editável: trivial → opencode/kimi
   (centavos), média → codex, complexa → codex com reasoning alto, crítica → claude.
3. **Um item = um commit** — pedidos-lista são decompostos; cada item ganha ciclo
   próprio (brief → execução → verificação → commit atômico).
4. **Verificação sempre** — nenhum diff vira commit sem review + testes + critérios;
   resultado terminal é reportado exato, nunca arredondado para sucesso.

## Decisões estruturais (tomadas em conversa)

| Decisão | Escolha |
| --- | --- |
| Papel do compozy | **Casa principal.** O ciclo acopla às primitivas do daemon; abandona a neutralidade de host do v1. |
| Repositório | Novo: `~/Projects/batuta-compozy`. |
| Executores | **Só providers nativos.** Os adapters em prosa (codex.md, opencode.md, claude.md) morrem; a tabela vira regras de runtime (`provider/model@reasoning`). |
| Loops | **Apontar para os bundled, sem fork.** Fork só é obrigatório para alterar definição; rodar é livre. A opinião entra por camadas de config (ver Roteamento). |
| PM/PRD | **Chapéu do maestro**, usando as skills bundled `cy-create-prd` / `cy-create-techspec` / `cy-create-tasks`. Sem agente PM separado. |
| Clarificação de requisitos | Conversa na sessão (fase PM) e mecanismos nativos durante o run (Goal `blocked` com evidência, gates humanos `needs-approval`). O clarification-loop em disco do batuta-cli não é portado — existia porque o CLI era headless. |
| Onboarding | **Sem skill de init.** O maestro se auto-configura no primeiro contato com o workspace. |
| Porta de entrada | Agente **`batuta`** numa sessão compozy. |
| Distribuição | Local/dev-link primeiro; publicação github quando estabilizar (viável: resource-only). |

## Arquitetura

Uma **extensão resource-only** do CompozyOS — sem subprocesso, sem SDK, sem Host API.
O `extension.toml` é escrito à mão (permitido para resource-only) e declara apenas
recursos:

```
batuta-compozy/
├── extension.toml            # manifest resource-only
├── agents/
│   └── batuta/AGENT.md       # o maestro: conversa, classifica, decompõe, despacha
├── resources/
│   └── routing.md            # tabela de roteamento default (editável pelo usuário)
├── docs/
│   └── superpowers/specs/    # este design e os que vierem
├── README.md                 # inclui pré-requisitos de provider (auth é operador)
└── tests/contract/           # smoke de install/enable/inventory/dry-run
```

Três princípios do v1 agora garantidos pelo daemon em vez de por prosa:

- **Estado**: WORK.md morre. Retomabilidade vem do run history (`generations[]`,
  pause/resume, `best_generation`) e do sistema de tasks do compozy.
- **Commits atômicos**: o fan-out do Loop dá um ciclo por task — o cumprimento deixa
  de depender de disciplina do modelo (falha conhecida do plugin Claude).
- **Escalação**: gates com rotas `revise`/`next_generation` e redespacho com runtime
  de lane superior, tudo auditável no `resolved_runtime` por geração.

## Fluxo: sistema inteiro em loop

```
você ⇄ agente batuta (sessão compozy)
         │  fase PM: conversa, clarifica ambiguidade aqui (é só diálogo)
         │  usa cy-create-prd → cy-create-techspec → cy-create-tasks
         ▼
   tasks criadas no daemon (um item = uma task)
         │  batuta despacha o Loop bundled implement-tasks
         │  com as regras de runtime da tabela
         ▼
   fan-out: por task → cy-execute-task (worker resolvido por complexidade)
   → verificação (cy-review-round / cy-fix-reviews, revise até limpar)
   → commit atômico
         │  falha repetida → escalação de runtime / gate humano
         ▼
   cy-final-verify → run `done` (ou `blocked`/`exhausted` reportado exato)
```

O batuta não escreve nenhuma dessas skills — ele rege: o AGENT.md define quando usar
cada uma, a tabela decide quem executa, o Loop garante que nada pula verificação nem
commit.

## Roteamento

A tabela (`resources/routing.md`) entra no runtime por três camadas nativas, sem tocar
nas definições dos Loops:

1. **Per-run `--runtime`** — o batuta despacha `loop run` com regras repetíveis
   (`--runtime complexity=trivial:opencode/kimi ...`); precedência `id > type >
   complexity`. Reclassificação na conversa ("usa o kimi pra isso") vira
   `--runtime id=task_N:...`.
2. **Config armazenada por Loop** — `compozy loop configure` grava a tabela como
   override no workspace; runs subsequentes herdam.
3. **Input defaults** — `[loops.inputs.<loop>]` para preferências persistentes
   (auto_commit, reviewer/fixer, paralelismo).

O daemon persiste `resolved_runtime` com proveniência por campo em cada geração — a
decisão de roteamento é auditável, não narrada.

## Verificação

Nenhum gate novo. `cy-review-round` + `cy-fix-reviews` fazem o review por rodada,
`cy-final-verify` fecha, e o contrato do Loop garante reporte terminal exato (`done`
só com verificação; `exhausted`/`blocked`/`stalled` nunca viram sucesso). Aprovações
humanas usam o gate nativo — incluindo a regra de que quem inicia o run não aprova o
próprio run.

## Bootstrap (primeiro contato com um workspace)

O AGENT.md instrui o batuta a, na primeira conversa num workspace:

1. Checar se a configuração batuta existe (config armazenada dos Loops /
   `loops.inputs`).
2. Se não existe: aplicar os defaults de `resources/routing.md` via `loop configure`
   e fazer as poucas perguntas de preferência que importam (paralelo vs sequencial,
   auto_commit, lane da complexa) na própria conversa.
3. Reconfiguração posterior é pedido em conversa; o batuta regrava o override.

Autenticação de provider (API keys, OAuth) é superfície de operador no compozy —
uma vez, global, fora do workspace. Vira seção de pré-requisitos no README, não
feature.

## Erros

O design não inventa tratamento de erro: retry/escala/quarentena são do daemon
(precedência fixa de failure classes), resultados terminais são reportados exatos
pelo batuta, e ambiguidade de requisito é conversa (fase PM) ou gate humano (durante
o run).

## Testes

Estilo contrato, como no POC do repo legado:

1. **Extensão**: build à mão validado com `compozy extension validate`; install +
   enable + `extension inventory` confere os recursos publicados (agente `batuta`,
   `routing.md`).
2. **Roteamento**: dry-run do `implement-tasks` com as regras do batuta confere o
   `resolved_runtime` esperado por lane (dry-run não gasta token nem cria run).
3. **E2E smoke**: num repo cobaia, uma feature pequena de ponta a ponta — PRD →
   tasks → loop → commits atômicos (um por task) → final-verify → `done`.

## Passo de descoberta (primeira tarefa do plano)

O design assume dos Loops bundled apenas o que a documentação do compozy afirma.
Antes de escrever o AGENT.md, inspecionar no daemon real:

- `compozy loop list` / `loop inspect` dos Loops do dev-cycle (`implement-tasks`,
  `review-and-fix`): inputs, contrato, gates, semântica de commit.
- As skills `cy-*` publicadas (`compozy skill view`): o que cada uma espera e produz.

**Contingência**: se o `implement-tasks` bundled não fizer commit atômico por item
como o Batuta exige, forkar pontualmente só esse Loop volta à mesa — decisão a tomar
com a evidência do passo de descoberta, não antes.

## Fora de escopo do v1

- Publicação no github / marketplace (fica para quando estabilizar).
- Templates por stack como recurso próprio — entram só se o passo de descoberta
  mostrar que as skills `cy-*` não cobrem o contexto por stack. (Os 9 templates do
  v1 ficam no repo legado como referência.)
- Agente PM separado, clarification-loop em disco, WORK.md, adapters em prosa,
  worktrees próprios — tudo substituído por primitivas nativas.
- Qualquer subprocesso/Host API na extensão.
