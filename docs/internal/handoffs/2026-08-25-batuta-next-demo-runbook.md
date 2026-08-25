# Batuta Next — runbook da apresentação local

Data: 2026-08-25  
Estado: laboratório ativo; execução final registrada abaixo

## O que esta preview prova

- Batuta `0.1.0-beta.5` instalado como extensão resource-only e saudável.
- Compozy local `v0.3.0-beta.21` construído de `upstream/main` mais somente o
  stack genérico de regras conjuntivas; nenhuma migration adicional.
- Catálogo vivo distingue presença do provider, modelo catalogado e estado de
  autenticação sem copiar secrets ou configuração bruta.
- Duas tarefas ordenadas usam lanes distintas por `type + complexity`, têm
  limites explícitos, executam verificação focada e geram um commit local por
  item.
- O slide separa explicitamente a preview entregue dos próximos incrementos.

Não afirmar nesta apresentação: paralelismo entre writers, fallback automático,
integração multi-worktree, push ou abertura autônoma de PR. Esses itens estão na
faixa `PRÓXIMO`.

## Laboratório ativo

| Item | Valor |
| --- | --- |
| Compozy source | `/home/francisross/Projects/opensource/_worktrees/batuta-matrix-preview` |
| Compozy binary | `/home/francisross/tmp-builds/batuta-matrix-build.yFXRxU/compozy` |
| `COMPOZY_HOME` | `/home/francisross/tmp-builds/batuta-matrix-home.yDR5Ok` |
| HTTP/UI | `http://127.0.0.1:32124` |
| Batuta stage | `/home/francisross/tmp-builds/batuta-next-stage.nwCtQ4` |
| Demo workspace | `/home/francisross/tmp-builds/batuta-next-workspace.tddEu0` |
| Workspace ID | `ws_8a3ae7016b48c166` |
| Delivery worktree | `wt_3f356a79f880672c` at `/home/francisross/tmp-builds/batuta-next-delivery` |
| Run | `looprun-a04b07d83bf2dc87` |
| Run UI | `http://127.0.0.1:32124/loop-runs/looprun-a04b07d83bf2dc87` |

Prefixe os comandos da demonstração com:

```bash
export COMPOZY_HOME=/home/francisross/tmp-builds/batuta-matrix-home.yDR5Ok
export PATH=/home/francisross/tmp-builds/batuta-matrix-build.yFXRxU:$PATH
cd /home/francisross/tmp-builds/batuta-next-workspace.tddEu0
```

## Sequência curta de comandos

### 1. Mostrar saúde e inventário

```bash
compozy version -o json
compozy extension status batuta -o json
compozy extension inventory batuta -o json
compozy provider list -o json
compozy provider models list -o json
```

Em `provider list`, destaque Codex, OpenCode e Cursor Agent como presença local.
Não trate `auth_status: unknown` como autenticado: o inventário separa as
evidências e a execução real abaixo prova somente as lanes Codex exercitadas.

### 2. Mostrar os metadados das tarefas

```bash
sed -n '1,24p' .compozy/tasks/batuta-next-demo/task_01.md
sed -n '1,24p' .compozy/tasks/batuta-next-demo/task_02.md
```

As células são `backend/low` e `frontend/medium`. Os arquivos de produção são
disjuntos e a aresta `task_01 → task_02` impede dois writers simultâneos.

### 3. Mostrar o dry-run

```bash
compozy loop run --workspace ws_8a3ae7016b48c166 \
  --name implement-tasks \
  --input slug=batuta-next-demo \
  --input auto_commit=true \
  --config-file runtime-rules.json \
  --dry-run -o json
```

Âncoras no resultado:

- `iteration_cap: 8`
- `budget_tokens: 120000`
- `budget_wall_sec: 1800`
- `backend + low → codex/gpt-5.6-luna@medium`
- `frontend + medium → codex/gpt-5.6-terra@high`
- `max_parallel: 1`

Nota de precisão: `budget_tokens` é verificado nas fronteiras de agendamento,
não interrompe uma sessão de modelo já em voo. Este run terminou com `150828`
tokens depois de iniciar a segunda sessão quando ainda havia saldo. Apresente o
wall clock e o iteration cap como backstops claros; não chame `120000` de corte
instantâneo exato.

### 4. Mostrar execução e commits

```bash
compozy loop status --workspace ws_8a3ae7016b48c166 \
  --run-id looprun-a04b07d83bf2dc87 -o json
git log --oneline --decorate -3
tests/backend_status_test.sh
tests/frontend_card_test.sh
git status --short
```

Evidência terminal:

| Item | Lane resolvida | Sessão | Resultado | Commit |
| --- | --- | --- | --- | --- |
| `task_01` | `backend/low → codex/gpt-5.6-luna@medium` | `sess_2f165145d2ca8b5c91cbadb1208b441a` | `succeeded` | `f456cf7` |
| `task_02` | `frontend/medium → codex/gpt-5.6-terra@high` | `sess_04f62ef413661e49def320427cdd9bf4` | `succeeded` | `2d38e99` |

O run terminou `done`; `collect` registrou `total=2`, `succeeded=2`,
`failed=0`, `coverage_rate=1`. Os dois testes passaram novamente após o run,
`git rev-list --count 125170e..HEAD` retornou `2`, e o repositório não possui
remote. Permanecem fora dos commits apenas os arquivos de tracking/memória e o
marker local de workspace, conforme o contrato de `implement-tasks`.

### 5. Mostrar o grafo completo sem publicar

```bash
compozy loop run --workspace ws_8a3ae7016b48c166 \
  --name batuta-deliver \
  --input slug=batuta-next-demo \
  --input origin_session_id=demo-presentation-session \
  --input worktree_ref=wt_3f356a79f880672c \
  --input auto_commit=true \
  --config-file runtime-rules.json \
  --dry-run -o json
```

O dry-run validado contém a cadeia
`import_tasks → implement-tasks → review-and-fix → worktree_inspect → branch → human gate → publisher`.
Não execute o run real deste grafo no laboratório sem um `origin_session_id`
real e uma decisão explícita sobre o gate.

## Roteiro falado — 5 a 7 minutos

1. **Problema (40 s).** “Hoje um agente genérico tende a usar o mesmo modelo
   para tudo. O Batuta transforma uma entrega em tarefas verificáveis e escolhe
   a lane usando dois sinais autorados: domínio e complexidade.”
2. **Catálogo vivo (60 s).** Abra o inventário. “O Batuta não adivinha nomes de
   modelo e não lê secrets. Ele vê providers, catálogo e postura de autenticação
   como fatos diferentes. Aqui aparecem Codex, OpenCode e Cursor; nesta prova eu
   exercito duas lanes Codex para manter a demonstração determinística.”
3. **Matriz (60 s).** Mostre os dois task files e o dry-run. “Backend simples vai
   para Luna; frontend médio vai para Terra com mais reasoning. A decisão
   resolvida fica persistida no run, não apenas narrada pelo agente.”
4. **Guardrails (45 s).** Aponte os limites. “A execução tem teto de iterações e
   30 minutos. O budget de tokens impede novo agendamento quando esgotado, mas
   uma sessão já em voo pode ultrapassar o número — esta prova mostrou isso.
   Hoje os itens são sequenciais: não colocamos dois writers no mesmo worktree.”
5. **Execução real (90 s).** Abra a UI do run e depois `git log`. “Cada tarefa
   roda numa sessão isolada, executa seu teste e cria exatamente um commit local.
   Não há push escondido.”
6. **Loop completo (45 s).** Mostre o dry-run de `batuta-deliver` se houver tempo.
   “O grafo já encadeia implementação, review, inspeção do worktree e gate humano
   antes do publisher. Hoje a prova viva fica no trecho de execução e commit.”
7. **Roadmap (60 s).** Mostre `docs/images/batuta-next-roadmap.png`. “O verde é o
   que está demonstrável agora. O violeta é o próximo desenho: um worktree por
   tarefa, paralelismo limitado, journal/fallback dentro do budget, integração
   determinística e só então PR autônomo.”

## Encerramento e segurança

- O laboratório não tem remote e não pode fazer push ou abrir PR.
- O daemon é isolado no port `32124`; não confundir com o daemon pessoal.
- Depois da apresentação, parar o daemon com `compozy daemon stop -o json` usando
  o mesmo `COMPOZY_HOME`.
- Remover somente os diretórios de laboratório listados acima quando não forem
  mais necessários.
