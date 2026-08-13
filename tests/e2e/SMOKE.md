# Smoke E2E — feature pequena de ponta a ponta

Pré-condições: extensão `batuta` instalada+habilitada; providers das lanes
autenticados; um repo cobaia registrado como workspace (qualquer projeto
pequeno com suite de testes real).

## Roteiro

1. **Sessão**: crie uma sessão no workspace cobaia com o agente `batuta`.
2. **Bootstrap**: na primeira mensagem, peça uma feature pequena (ex.: "adicione
   um subcomando --version"). Aceite: o batuta aplica a tabela de roteamento
   (confira depois com dry-run: `effective_config.runtime_rules` preenchido) e
   pergunta auto-commit ANTES de qualquer despacho.
3. **Fase PM**: o batuta conduz `cy-create-tasks` (com ou sem PRD/TechSpec,
   conforme o tamanho). Aceite: `.compozy/tasks/<slug>/` existe com `_tasks.md`
   + `task_NN.md`, cada task com `complexity` no frontmatter; o breakdown foi
   apresentado para aprovação em conversa.
4. **Despacho**: o batuta roda dry-run, mostra o plano, e dispara
   `implement-tasks`. Aceite: run visível em `compozy loop runs`; cada geração
   com `resolved_runtime` coerente com a lane da task.
5. **Commits atômicos**: ao terminar, `git log` no cobaia mostra exatamente um
   commit por task implementada (auto_commit=true). Nenhum push.
6. **Review**: o batuta reporta o terminal exato do run e, em `done`, dispara
   `review-and-fix` com `task_name=<slug>`. Aceite: rodada(s) em
   `.compozy/tasks/<slug>/reviews-NNN/` com issues triadas, run termina `done`
   (rodada limpa) ou reporta `exhausted` literalmente.
7. **Reporte final**: o batuta entrega os dois terminais exatos + run IDs.

## Casos comportamentais de aceitação

- Configure `auto_commit=false`, dispatch through Batuta, and confirm both child
  runs persist `inputs.auto_commit=false`.
- Ask Batuta to dispatch a missing slug. Confirm dry-run fails and no real
  `batuta-deliver` run is created.
- Submit one deliberate direct invalid run. Confirm its terminal is not `done`.

## Falhas que reprovam

- O batuta editar/commitar código diretamente na sessão.
- Qualquer terminal != `done` reportado como sucesso.
- Mais de um commit para uma task, ou um commit cobrindo duas tasks.
- Push automático.
- O batuta aprovar o próprio gate (`needs-approval`).

---

## Resultado — 2026-08-11 (workspace ~/Projects/smoke-cobaia)

**Veredito: APROVADO com ressalvas.** Ciclo completo fechou: PM → 1 task (`low`) →
`implement-tasks` `done` (gen 1, `resolved_runtime: codex/gpt-5.6-luna`, proveniência
`config`, 1 commit atômico `017fc0b` com testes 10/10) → `review-and-fix` `done`
(rodada 1: 1 issue válida → fix + commit `b8ebe27` → finalizer `resolved`; rodada 2
limpa). Nenhum critério reprovador ocorreu (batuta não editou código, nenhum terminal
arredondado, 1 commit por unidade, sem push, sem auto-aprovação).

Ressalvas e achados:

- **3 bugs de plataforma contornados** (CompozyOS 0.3.0-beta.13 / providers): agentes
  publicados por extensão invisíveis ao catálogo de skills (500 no prompt; workaround:
  agentes autorados globais); model IDs do opencode exigem prefixo `opencode/...`;
  integração opencode 1.18.16 incompatível (upstream `channel_id/mode/channel_strategy`).
- **2 falhas do batuta, corrigidas no AGENT.md**: não persistiu o "sim" do auto-commit
  (1º run saiu `auto_commit=false`); aplicou a tabela de lanes sem validar contra o
  catálogo vivo (1º run queimou 12 gerações em bind inválido). Agora o bootstrap valida
  catálogo/habilitação/custo, confirma com o operador, e re-lê o override antes de cada
  despacho (regras per-run congelam no run).
- **1 gap estrutural → v1.1**: Loops não se encadeiam e a sessão não acorda no terminal —
  o review precisou de cutucada do operador. Design v1.1: Loop composto `batuta-deliver`
  (`run-loop`) + automation triggers para wake decisório.
- Lanes finais confirmadas pelo operador: low→`codex/gpt-5.6-luna`,
  medium→`codex/gpt-5.6-terra@high`, high→`codex/gpt-5.6-sol`,
  critical→`claude/claude-opus-4-8` (sincronizar na skill `batuta-routing`).
