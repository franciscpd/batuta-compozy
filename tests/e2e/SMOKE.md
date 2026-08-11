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

## Falhas que reprovam

- O batuta editar/commitar código diretamente na sessão.
- Qualquer terminal != `done` reportado como sucesso.
- Mais de um commit para uma task, ou um commit cobrindo duas tasks.
- Push automático.
- O batuta aprovar o próprio gate (`needs-approval`).
