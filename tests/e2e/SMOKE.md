# Smoke E2E — feature pequena de ponta a ponta

Pré-condições: extensão `batuta` instalada+habilitada; providers das lanes
autenticados; um repo cobaia registrado como workspace (qualquer projeto
pequeno com suite de testes real).

## Roteiro

1. **Sessão**: crie uma sessão no workspace cobaia com o agente `batuta`.
2. **Gate inicial**: na primeira mensagem, peça uma feature pequena (ex.:
   "adicione um subcomando --version usando literalmente todo 1.0.0"). Aceite:
   a primeira chamada de ferramenta é `compozy__config_get` para a chave exata
   do workspace. Quando ausente, o Batuta pergunta auto-commit sem fazer
   discovery, routing, PM, preflight, dry-run ou inspeção de Loop.
3. **Bootstrap**: somente após o gate confirmar um booleano, o Batuta aplica a
   tabela de roteamento (confira depois com dry-run:
   `effective_config.run_runtime_rules` preenchido).
4. **Fase PM**: o Batuta conduz `cy-create-spec`, inclusive para uma feature
   pequena. Aceite: o grill pode ser curto, mas não é pulado; o operador
   aprova `_spec.md`, `_user_stories.md`, `_dx.md` e `_tests.md`, com
   `_uiux.md` somente se houver mudança Web. Depois o Batuta conduz
   `cy-create-tasks`; `.compozy/tasks/<slug>/` contém `_tasks.md` +
   `task_NN.md`, cada task com `complexity`, e o breakdown é apresentado para
   aprovação em conversa.
5. **Despacho composto**: o Batuta chama diretamente o
   `ext__spec_cycle__import_tasks` somente leitura e confirma `count > 0`;
   depois roda dry-run, mostra o plano e
   submete exatamente um `batuta-deliver` com `slug`, `origin_session_id` da
   sessão atual e `auto_commit` resolvido. O dry-run apenas planeja nós e não
   executa o import. Aceite: o run composto fica visível em `compozy loop
   runs`; o daemon executa `implement-tasks` e depois `review-and-fix` como
   filhos desse run.
6. **Commits atômicos**: ao terminar, `git log` no cobaia mostra exatamente um
   commit por task implementada (auto_commit=true). Nenhum push.
7. **Inspeção composta**: sob o run de `batuta-deliver`, inspecione os IDs e
   terminais dos filhos, seus `resolved_runtime` e as rodadas em
   `.compozy/tasks/<slug>/reviews-NNN/`. Aceite: review termina `done` (rodada
   limpa) ou reporta `exhausted` literalmente.
8. **Retorno e reporte final**: registre a sequência de resultados de
   ferramentas do despacho real e o ID daquele turno. O resultado aceito de
   `batuta-deliver` retorna `run_id` e, quando disponível, `web_url`; não pode
   haver outra chamada de ferramenta nesse turno. O efeito terminal inicia um
   turno posterior na sessão original, com a identidade exata desse run. Nesse
   turno posterior, `loop_status` com o pai exato é a primeira ferramenta
   operacional; só então o Batuta reporta o terminal exato do composto,
   terminais dos filhos, commits e blocker.

## Casos comportamentais de aceitação

Execute estes casos somente após republicar a extensão local. Eles são prova
de comportamento do daemon e do agente consumidor; ainda não foram executados
por esta alteração.

Para a aceitação do retorno dirigido a eventos, registre o despacho real, a
sequência de resultados de ferramentas e seu ID de turno; confirme que não há
chamada posterior de ferramenta nesse turno. Registre também o prompt terminal
posterior com a identidade exata do run e confirme que `loop_status` do pai
exato é sua primeira ferramenta operacional. Se um turno de solicitação
explícita de progresso for exercitado separadamente, ele deve fazer exatamente
uma chamada de status, sem polling. Não use `sleep`, espera, watcher, prompt
lógico terminal extra, push ou código autorado pelo Batuta. Valide a evidência
registrada com:

```bash
COMPOZY_BIN=/absolute/path/to/compozy
SESSION_ID=sess-example
DELIVERY_RUN_ID=looprun-example
python3 tests/e2e/assert_event_driven_return.py \
  --compozy "$COMPOZY_BIN" \
  --session "$SESSION_ID" \
  --run-id "$DELIVERY_RUN_ID"
```

Somente quando esse caso explícito de progresso tiver sido efetivamente
exercitado, repita o validador com `--progress-turn "$PROGRESS_TURN_ID"`.

- Configure `loops.inputs.batuta-deliver.auto_commit=false`, faça o despacho
  pelo Batuta e confirme que ambos os filhos persistem
  `inputs.auto_commit=false`. Nenhum commit de implementação ou review pode
  ser criado nesse caso.
- Remova somente o valor workspace de
  `loops.inputs.batuta-deliver.auto_commit`, abra uma sessão nova e confirme
  que `compozy__config_get` retorna `config_path_not_found`. O Batuta deve
  perguntar a preferência sem nenhuma outra chamada, gravá-la com
  `compozy__config_set` em escopo workspace e usar `compozy__config_get` como a
  chamada imediatamente seguinte; registre a leitura estruturada confirmando
  o booleano escolhido antes de discovery, routing, PM, preflight, dry-run,
  inspeção de Loop ou despacho. Valide a ordem diretamente com:

  ```bash
  python3 tests/e2e/assert_preference_gate.py \
    --compozy /caminho/para/compozy \
    --session <session-id> --expected false
  ```
- Repita o bootstrap com uma falha de configuração diferente de
  `config_path_not_found`. O Batuta deve parar e apresentar o erro estruturado
  exato, sem consultar defaults globais, os Loops filhos, o default da
  definição ou um dry-run para inventar `auto_commit`.
- Peça uma feature que exija literalmente `todo 1.0.0`. Confirme que
  `_spec.md`, `_user_stories.md`, `_dx.md`, `_tests.md`, `_tasks.md`, cada
  `task_NN.md` e os prompts de execução aplicáveis preservam `todo 1.0.0` sem
  upgrade, normalização ou paráfrase. Para mudança Web, inclua `_uiux.md` na
  mesma verificação.
- Peça ao Batuta para despachar um `slug` inexistente. O preflight direto de
  `import_tasks` deve falhar antes do dry-run e nenhum run real de
  `batuta-deliver` pode ser criado. Em seguida, envie uma submissão direta
  deliberadamente inválida e confirme que o terminal nunca é `done`.
- Após uma entrega terminal, a sessão original do Batuta deve receber
  exatamente um novo turno enfileirado ou direto. Registre o ID do run de
  entrega, o ID da sessão de origem, o gatilho do efeito e o resultado de
  entrega/replay da admissão do prompt.
- Reproduza a mesma identidade de efeito terminal e confirme que ela não cria
  outro turno na sessão original.
- Confirme que o inventário da extensão contém exatamente `batuta`,
  `batuta-routing` e `batuta-deliver`. Não pode haver recurso `batuta-watch`,
  watcher em execução ou agente de reporte.
- Confirme que o detalhe do run de entrega não contém `session_id` nem
  `resolved_runtime` de agente de reporte. Registre a contagem relevante de
  tokens e confirme que o retorno terminal não consumiu tokens de modelo de
  agente de reporte.

## Falhas que reprovam

- O batuta editar/commitar código diretamente na sessão.
- Qualquer terminal != `done` reportado como sucesso.
- Mais de um commit para uma task, ou um commit cobrindo duas tasks.
- Push automático.
- O batuta aprovar o próprio gate (`needs-approval`).

---

## Resultado histórico — 2026-08-11 (workspace ~/Projects/smoke-cobaia)

Este resultado descreve a arquitetura anterior e não prova os efeitos
terminais, a deduplicação ou o inventário atuais.

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
