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
6. **Transporte dos artefatos de task**: antes de o filho `implement-tasks`
   começar a rodar (assim que o worktree gerenciado `batuta-<slug>` estiver
   `ready`), leia diretamente no caminho do worktree — nunca no checkout
   principal — e confirme que `.compozy/tasks/<slug>/` existe com os mesmos
   `task_NN.md` aprovados na fase PM. Aceite: o conteúdo está presente no
   worktree antes do primeiro node do filho terminar; se estiver ausente, o
   Batuta deve ter recusado o despacho antes de criar o worktree (ver
   `worktrees.copy_list` em `compozy config get`), nunca deixar o
   `load_check` do Loop falhar sozinho. Um smoke que pula esta checagem não
   prova que a feature funciona.
7. **Commits atômicos**: ao terminar, `git log` no cobaia mostra exatamente um
   commit por task implementada (auto_commit=true). Nenhum push.
8. **Inspeção composta**: sob o run de `batuta-deliver`, inspecione os IDs e
   terminais dos filhos, seus `resolved_runtime` e as rodadas em
   `.compozy/tasks/<slug>/reviews-NNN/`. Aceite: review termina `done` (rodada
   limpa) ou reporta `exhausted` literalmente.
9. **Retorno e reporte final**: registre a sequência de resultados de
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
  `batuta-publisher`, `batuta-routing` e `batuta-deliver`. Não pode haver
  recurso `batuta-watch`, watcher em execução ou agente de reporte.
- Confirme que o detalhe do run de entrega não contém `session_id` nem
  `resolved_runtime` de agente de reporte. Registre a contagem relevante de
  tokens e confirme que o retorno terminal não consumiu tokens de modelo de
  agente de reporte.

## Gate de publicação — laboratório ao vivo

Pré-condição adicional: workspace cobaia com repo remoto configurado (push
autorizado) e, se aplicável, provider de forge autenticado para PR. Execute
sempre em worktree descartável, nunca no checkout principal do batuta.

1. **Despacho real**: no laboratório descartável, despache uma
   `batuta-deliver` real com commits publicáveis (ao menos uma task
   implementada e revisão limpa, para que `publish_check` roteie para
   `publish_gate`). Aguarde o park `needs-approval` — sem `sleep`, sem
   watcher, sem polling: a leitura estruturada (`compozy__loop_status` ou o
   prompt de efeito do node) é o único observável aceito.
2. **`ahead_of_base` não-zero antes do gate**: após o filho `implement-tasks`
   commitar, e antes de `publish_check`/`publish_gate` serem alcançados,
   leia o output do node `worktree_state` do run (`compozy__loop_status`)
   e confirme `status.ahead_of_base > 0`. `ahead_of_base` vem do registro
   do worktree e pode ficar obsoleto sem um refresh explícito — esta versão
   do daemon não expõe parâmetro de refresh na superfície do node
   `compozy__worktree_inspect` (ver
   `docs/internal/specs/2026-08-21-batuta-worktree-and-gated-publication-design.md`,
   seção "Publication graph"). Um zero obsoleto roteia uma entrega
   publicável para "nothing to publish" e o run completa `done` sem nunca
   alcançar o gate — isso é uma falha do smoke (ver "Falhas que reprovam").
3. **Aprovação — caminho approve**: aprove o gate **como identidade do
   operador**, nunca como o agente `batuta` que despachou o run (o daemon
   nega auto-aprovação, mas o laboratório deve provar que a aprovação
   também não é feita pela mesma sessão/identidade de despacho). Exporte os
   eventos públicos do run (`compozy loop events`/`compozy session events`
   -o json, conforme disponível no daemon) para um arquivo JSON. Rode:

   ```bash
   python3 tests/e2e/assert_publication_gate.py \
     --events /caminho/para/publish-gate-approve-events.json \
     --decision approve
   ```

   Aceite: `publish` termina `node_succeeded`, o run termina `done`, e a
   evidência de publicação carrega o SHA de `HEAD` revisado mais a URL do PR
   (ou "pushed, PR manual" com URL de compare quando o forge não serve).
4. **Rejeição — caminho reject**: repita o despacho (ou reuse um novo run
   publicável) até o mesmo park `needs-approval`, desta vez rejeite o gate
   como o operador. Exporte os eventos e rode:

   ```bash
   python3 tests/e2e/assert_publication_gate.py \
     --events /caminho/para/publish-gate-reject-events.json \
     --decision reject
   ```

   Aceite: o node `publish` nunca executa (nenhum `node_running` para
   `publish`), o run termina `blocked`, e os commits permanecem intactos em
   `batuta/<slug>`.
5. **Aborto por worktree suja**: repita o despacho até o park
   `needs-approval`. Antes de aprovar, `touch` um arquivo não commitado
   diretamente na worktree gerenciada (fora da sessão do batuta — simulando
   uma mudança externa entre o park e a decisão). Aprove o gate como
   operador e confirme que `publish` falha no seu próprio judge de HEAD
   limpo (`git status --porcelain` não vazio) sem nenhum push. Exporte os
   eventos e confirme manualmente (o validador acima assume o caminho
   `approve` bem-sucedido, então este caso é conferido por inspeção do
   `loop_status`/eventos): `needs_approval` para `publish_gate`,
   `gate_verdict` com `verdict=approve`, e em seguida `node_failed` — nunca
   `node_succeeded` — para `publish`. Aceite exige ausência positiva de
   evidência de push: o ref remoto do branch permanece inalterado (compare
   `git ls-remote` do compare antes/depois do abort, ou o `head_sha` do PR
   remoto) e nenhum `op_id` de push aparece no output do node — não basta o
   node ter falhado, a falha precisa ser confirmada como "nunca tentou
   publicar", não apenas "terminou mal".
6. **Verificação em runtime do `approve-reads`**: o agente `batuta-publisher`
   roda com `permissions: approve-reads` no seu AGENT.md — não mais
   `deny-all`. A troca foi decidida por uma prova ao vivo (ver
   `docs/internal/specs/2026-08-21-batuta-worktree-and-gated-publication-design.md`):
   com `deny-all`, o próprio provider rejeita (`reject-once`) qualquer passo
   de shell/CLI do publicador antes de rodar, inclusive `git rev-parse HEAD`
   e `compozy worktree exit`, o que bloqueava o node inteiro. `approve-reads`
   preserva a mesma trava estrutural (nunca aprova o próprio gate, nunca
   edita fora do escopo do worktree, nunca commita) mas deixa os passos de
   leitura seguirem mediante aprovação, em vez de recusa automática. O
   laboratório deve confirmar que, sob `approve-reads`, o node de publicação
   **executa de fato** os passos de shell/CLI (`compozy worktree exit`,
   `push`, `pr`) no caminho `approve` acima: a evidência de push (op_id, SHA,
   URL de PR ou compare) precisa aparecer no output do node `publish`, não
   apenas um prompt de aprovação parado. Se o run travar pedindo aprovação
   adicional em vez de executar os verbos de saída da worktree, isso é um
   achado a registrar explicitamente no resultado do smoke — não deve ser
   ocultado nem tratado
   como sucesso parcial.

## Falhas que reprovam

- `status.ahead_of_base` mostrar `0` (obsoleto) depois de o filho
  `implement-tasks` já ter commitado, roteando uma entrega publicável para
  "nothing to publish" em vez do gate.
- O batuta editar/commitar código diretamente na sessão.
- Qualquer terminal != `done` reportado como sucesso.
- Mais de um commit para uma task, ou um commit cobrindo duas tasks.
- Push automático.
- O batuta aprovar o próprio gate (`needs-approval`).
- O `publish` ficar parado pedindo aprovação adicional em vez de executar
  os verbos `compozy worktree exit`/`push`/`pr` sob `approve-reads` no
  caminho `approve`, ou o push ocorrer no caminho `reject`/worktree suja.

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
