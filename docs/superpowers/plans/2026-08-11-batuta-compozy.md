# Batuta no CompozyOS — Plano de Implementação

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Empacotar a opinião do Batuta como extensão resource-only do CompozyOS: um agente maestro (`batuta`) que rege as skills `cy-*` e os Loops bundled (`implement-tasks`, `review-and-fix`) com roteamento de runtime por complexidade.

**Architecture:** Extensão resource-only (sem subprocesso, sem SDK, sem Host API) com `extension.toml` escrito à mão publicando dois recursos: o agente `batuta` (AGENT.md) e a skill `batuta-routing` (tabela de roteamento default). Toda execução usa primitivas nativas do daemon — Loops bundled sem fork, roteamento via camadas de config (`--runtime` per-run, `loop configure`, `loops.inputs`).

**Tech Stack:** CompozyOS 0.3.0-beta.13+ (CLI `compozy`), TOML, Markdown, bash + python3 para testes de contrato.

## Global Constraints

- **Resource-only**: nenhum subprocesso, SDK ou Host API na extensão (spec: "Fora de escopo").
- **Sem fork dos Loops bundled**: a descoberta (2026-08-11) confirmou que `implement-tasks` com `auto_commit=true` instrui "Create exactly one commit for this task" — a contingência de fork NÃO dispara. Rodar os bundled é livre; fork só se alterar definição (não alteramos).
- **Vocabulário de lanes**: `low` / `medium` / `high` / `critical` — é o que `cy-create-tasks` grava no frontmatter `complexity` das tasks e o que `runtime_rules[].match.complexity` casa. Nunca usar "trivial/média/complexa/crítica" em superfícies executáveis.
- **Fluxo em dois Loops encadeados**: `implement-tasks` (execução + commit atômico por task) e depois `review-and-fix` (review por rodada até limpar, `task_name` = slug da feature). O `implement-tasks` NÃO tem gate de review interno.
- **Paralelismo não configurável**: o fan-out do `implement-tasks` tem `max_parallel: 1` fixo na definição. A pergunta de bootstrap "paralelo vs sequencial" da spec cai.
- **Reporte terminal exato**: `done` / `no-op` / `blocked` / `failed` / `canceled` / `exhausted` / `stalled` reportados literalmente, nunca arredondados para sucesso.
- **Quem inicia não aprova**: gates humanos usam `compozy__loop_approve`; o daemon nega auto-aprovação (`approval_self_denied`).
- **Nunca push automático** em nenhum fluxo.
- **Idioma**: docs do repo em PT-BR; AGENT.md e SKILL.md (consumidos por agentes) em inglês, seguindo o padrão dos recursos do `dev-cycle`.
- Inputs dos Loops bundled (conferidos por `loop inspect`): `implement-tasks(slug*, auto_commit=false, implementer=code_implementer)`; `review-and-fix(task_name*, auto_commit=false, reviewer=reviewer, fixer=review_fixer)`.

---

### Task 1: Esqueleto da extensão (`extension.toml`)

**Files:**
- Create: `extension.toml`
- Create: `tests/contract/test_01_validate.sh`
- Create: `tests/contract/run.sh`

**Interfaces:**
- Produces: extensão chamada `batuta`, versão `0.1.0`, publicando as famílias `agents` e `skills` a partir dos diretórios `agents/` e `resources/skills/`. Tasks 2 e 3 criam os arquivos dentro desses diretórios.

- [ ] **Step 1: Escrever o teste de contrato que falha**

```bash
cat > tests/contract/test_01_validate.sh <<'EOF'
#!/usr/bin/env bash
# Valida o manifest da extensão sem executar código.
set -euo pipefail
cd "$(dirname "$0")/../.."

out=$(compozy extension validate . -o json)
echo "$out" | python3 - <<'PY'
import json, sys
d = json.load(sys.stdin)
issues = d.get("issues") or []
errors = [i for i in issues if i.get("severity") == "error"]
assert not errors, f"validate retornou erros: {errors}"
print("OK: manifest valido, sem issues de severidade error")
PY
EOF
chmod +x tests/contract/test_01_validate.sh

cat > tests/contract/run.sh <<'EOF'
#!/usr/bin/env bash
# Roda todos os testes de contrato em ordem.
set -euo pipefail
cd "$(dirname "$0")"
for t in test_*.sh; do
  echo "=== $t ==="
  "./$t"
done
echo "=== todos os testes de contrato passaram ==="
EOF
chmod +x tests/contract/run.sh
```

- [ ] **Step 2: Rodar o teste e confirmar que falha**

Run: `tests/contract/test_01_validate.sh`
Expected: FAIL — `extension.toml` ainda não existe (validate reporta manifest ausente ou o comando sai com erro).

- [ ] **Step 3: Escrever o `extension.toml`**

Shape espelhado no manifest do bundled `dev-cycle` (`~/.compozy/extensions/dev-cycle/extension.json`), que declara famílias de recursos como listas de paths relativos à raiz da extensão:

```toml
[extension]
name = "batuta"
version = "0.1.0"
description = "Batuta, the conductor: routes CompozyOS dev-cycle work to the cheapest capable executor and never writes code itself."
min_compozy_version = "0.3.0"

[resources]
agents = ["agents"]
skills = ["resources/skills"]
```

Criar também os diretórios vazios que o manifest referencia (com `.gitkeep` até as Tasks 2–3 preenchê-los):

```bash
mkdir -p agents resources/skills
touch agents/.gitkeep resources/skills/.gitkeep
```

- [ ] **Step 4: Rodar o teste e confirmar que passa**

Run: `tests/contract/test_01_validate.sh`
Expected: PASS — `OK: manifest valido...`.

Se `compozy extension validate .` exigir outro layout para resource-only (ex.: rejeitar diretório de família vazio), ajustar o manifest conforme a mensagem estruturada do validate — nunca ignorar um `error`. Se o validate rejeitar as famílias só por estarem vazias, mover este teste para depois da Task 3 e registrar isso no commit.

- [ ] **Step 5: Commit**

```bash
git add extension.toml agents resources tests/contract
git commit -m "feat: esqueleto resource-only da extensão batuta"
```

---

### Task 2: Skill `batuta-routing` (tabela de roteamento default)

**Files:**
- Create: `resources/skills/batuta-routing/SKILL.md`
- Create: `tests/contract/test_02_routing_dryrun.sh`
- Delete: `resources/skills/.gitkeep`

**Interfaces:**
- Produces: skill `batuta-routing` contendo (a) a tabela default legível, (b) um bloco JSON canônico `runtime_rules` que o agente `batuta` (Task 3) aplica via `compozy__loop_configure` no bootstrap e que o teste desta task valida por dry-run. O bloco JSON fica dentro de um fence ```json rotulado `runtime_rules` — o AGENT.md referencia esse rótulo.

**Nota de design (desvio consciente da spec):** a spec pedia `resources/routing.md` como arquivo solto. Arquivo solto na raiz da extensão instalada não é legível de dentro de uma sessão managed; uma **skill** é família publicável e o batuta a lê nativamente com `compozy__skill_view`. Mesmo conteúdo, superfície acessível.

- [ ] **Step 1: Escrever o teste de contrato que falha**

O teste extrai o JSON da skill e confere, via dry-run (não gasta token, não cria run), que o daemon aceita as regras e as reporta em `run_runtime_rules`:

```bash
cat > tests/contract/test_02_routing_dryrun.sh <<'EOF'
#!/usr/bin/env bash
# Extrai runtime_rules da skill batuta-routing e valida por dry-run do implement-tasks.
set -euo pipefail
cd "$(dirname "$0")/../.."
WS="$PWD"
SKILL="resources/skills/batuta-routing/SKILL.md"

RULES_JSON=$(python3 - "$SKILL" <<'PY'
import re, sys, json
text = open(sys.argv[1]).read()
m = re.search(r"```json runtime_rules\n(.*?)```", text, re.S)
assert m, "bloco '```json runtime_rules' nao encontrado na skill"
rules = json.loads(m.group(1))
assert isinstance(rules, list) and rules, "runtime_rules deve ser lista nao vazia"
lanes = [r["match"]["complexity"] for r in rules]
assert lanes == ["low", "medium", "high", "critical"], f"lanes erradas: {lanes}"
print(json.dumps(rules))
PY
)

# Fixture descartavel: uma task minima para o import do dry-run resolver.
mkdir -p .compozy/tasks/_routing_probe
cat > .compozy/tasks/_routing_probe/task_01.md <<'TASK'
---
status: pending
title: Routing probe
type: chore
complexity: low
---
# Routing probe
Dry-run probe only.
TASK
cat > .compozy/tasks/_routing_probe/_tasks.md <<'MANIFEST'
---
schema_version: "compozy.tasks/v2"
workflow: _routing_probe
graph:
  nodes:
    - id: task_01
      file: task_01.md
  edges: []
---
# Routing Probe Task List
MANIFEST
trap 'rm -rf .compozy' EXIT

# Monta os --runtime a partir do JSON da skill (expressao provider/model@reasoning).
mapfile -t RUNTIME_FLAGS < <(python3 - <<PY
import json
for r in json.loads('''$RULES_JSON'''):
    rt = r["runtime"]
    expr = f"{rt['provider']}/{rt['model']}"
    if rt.get("reasoning"):
        expr += "@" + rt["reasoning"]
    print(f"complexity={r['match']['complexity']}:{expr}")
PY
)
ARGS=()
for f in "${RUNTIME_FLAGS[@]}"; do ARGS+=(--runtime "$f"); done

out=$(compozy loop run --workspace "$WS" --name implement-tasks \
  --input slug=_routing_probe "${ARGS[@]}" --dry-run -o json)

echo "$out" | python3 - <<'PY'
import json, sys
d = json.load(sys.stdin)["dry_run"]
rules = d["effective_config"]["run_runtime_rules"]
lanes = {r["match"]["complexity"]: r["runtime"] for r in rules}
assert set(lanes) == {"low", "medium", "high", "critical"}, f"lanes no dry-run: {sorted(lanes)}"
for lane, rt in lanes.items():
    assert rt.get("provider") and rt.get("model"), f"lane {lane} sem provider/model resolvido: {rt}"
print("OK: dry-run aceitou as 4 lanes em run_runtime_rules")
PY
EOF
chmod +x tests/contract/test_02_routing_dryrun.sh
```

- [ ] **Step 2: Rodar o teste e confirmar que falha**

Run: `tests/contract/test_02_routing_dryrun.sh`
Expected: FAIL — `SKILL.md` não existe ainda.

- [ ] **Step 3: Conferir os IDs de modelo disponíveis**

Antes de gravar os defaults, confirmar os IDs reais no daemon (a expressão de runtime só resolve provider registrado; IDs exatos de modelo passam adiante):

Run: `compozy provider models list -o json | python3 -m json.tool | head -80`

Ajustar os IDs do Step 4 para os que o catálogo listar (ex.: se o provider do Kimi for `opencode` com outro model ID, usar o listado). Os valores abaixo são os defaults da spec; trocar o **valor**, nunca a **estrutura**.

- [ ] **Step 4: Escrever a skill**

```bash
mkdir -p resources/skills/batuta-routing
rm -f resources/skills/.gitkeep
cat > resources/skills/batuta-routing/SKILL.md <<'EOF'
---
name: batuta-routing
description: Default cost/complexity routing table for the batuta conductor. Read at bootstrap to seed per-workspace loop configuration; the stored workspace override is authoritative afterwards.
---

# Batuta Routing Table

Batuta's core opinion: route every task to the cheapest executor that can
handle it. Lanes use the `complexity` vocabulary that `cy-create-tasks`
writes into task frontmatter (`low`, `medium`, `high`, `critical`) — the
same vocabulary `runtime_rules[].match.complexity` matches on.

| Lane       | Runtime (`provider/model@reasoning`) | Intent                                  |
| ---------- | ------------------------------------ | --------------------------------------- |
| `low`      | `opencode/kimi-k2`                   | Contained change, cents per task        |
| `medium`   | `codex/gpt-5.4`                      | New interfaces, moderate coordination   |
| `high`     | `codex/gpt-5.4@high`                 | New subsystem, heavy reasoning          |
| `critical` | `claude/opus`                        | Cross-cutting, high regression risk     |

## Canonical rules

This is the machine-readable form batuta applies with `compozy__loop_configure`
(stored per-workspace override for `implement-tasks`) during bootstrap, and the
form dispatches reuse as per-run `--runtime` rules. Precedence when both exist:
per-run > stored config. Rule matching precedence inside a layer: `id > type > complexity`.

```json runtime_rules
[
  {"match": {"complexity": "low"},      "runtime": {"provider": "opencode", "model": "kimi-k2"}},
  {"match": {"complexity": "medium"},   "runtime": {"provider": "codex",    "model": "gpt-5.4"}},
  {"match": {"complexity": "high"},     "runtime": {"provider": "codex",    "model": "gpt-5.4", "reasoning": "high"}},
  {"match": {"complexity": "critical"}, "runtime": {"provider": "claude",   "model": "opus"}}
]
```

## Escalation and reclassification

- Repeated failure in a lane: re-dispatch the affected task with a per-run
  `id` rule one lane up (`--runtime id=task_NN:<runtime of the next lane>`).
  `id` beats `complexity`, so the override is surgical.
- Operator reclassification in conversation ("use kimi for this one") becomes
  the same per-run `id` rule on the next dispatch.
- The daemon persists `resolved_runtime` with per-field provenance on every
  generation — routing decisions are auditable via `compozy__loop_status`,
  never narrated.
EOF
```

- [ ] **Step 5: Rodar o teste e confirmar que passa**

Run: `tests/contract/test_02_routing_dryrun.sh`
Expected: PASS — `OK: dry-run aceitou as 4 lanes em run_runtime_rules`.

Se o dry-run rejeitar um provider/model, voltar ao Step 3 e corrigir o ID na tabela E no bloco JSON (os dois devem sempre concordar).

- [ ] **Step 6: Rodar o teste da Task 1 (regressão do manifest)**

Run: `tests/contract/test_01_validate.sh`
Expected: PASS.

- [ ] **Step 7: Commit**

```bash
git add resources/skills tests/contract/test_02_routing_dryrun.sh
git commit -m "feat: skill batuta-routing com tabela de roteamento validada por dry-run"
```

---

### Task 3: O maestro (`agents/batuta/AGENT.md`)

**Files:**
- Create: `agents/batuta/AGENT.md`
- Delete: `agents/.gitkeep`

**Interfaces:**
- Consumes: skill `batuta-routing` (Task 2) — o AGENT.md referencia o bloco `runtime_rules` dela pelo nome da skill.
- Produces: agente público `batuta` que a Task 4 confere no inventory e a Task 6 exercita de ponta a ponta.

**Formato:** frontmatter YAML com `name` e `category_path`, corpo em inglês — mesmo padrão dos agentes do `dev-cycle` (ver `~/.compozy/extensions/dev-cycle/agents/code_implementer/AGENT.md`). Sem override de provider/model no frontmatter: o maestro herda o runtime default do projeto (a opinião de custo é para os **executores**, não para ele).

- [ ] **Step 1: Escrever o AGENT.md**

```bash
mkdir -p agents/batuta
rm -f agents/.gitkeep
cat > agents/batuta/AGENT.md <<'EOF'
---
name: batuta
category_path: [Batuta]
---

You are Batuta, the conductor. You orchestrate full-system development in a
loop on top of CompozyOS primitives. Four non-negotiable principles:

1. **The conductor never plays** — you never write or edit code. You
   converse, clarify, classify, decompose, configure, dispatch, and report.
2. **Route by cost/complexity** — every executable task goes to the cheapest
   runtime lane that can handle it, per the `batuta-routing` skill and the
   workspace's stored override.
3. **One item = one commit** — list-shaped requests are decomposed into
   tasks; the `implement-tasks` Loop gives each task its own isolated cycle
   and (with `auto_commit=true`) exactly one commit.
4. **Verification always, reported exactly** — nothing ships unverified, and
   terminal outcomes (`done`, `no-op`, `blocked`, `failed`, `canceled`,
   `exhausted`, `stalled`) are reported literally. Never round anything up
   to success.

## Bootstrap (first contact with a workspace)

On the first conversation in a workspace, before any dispatch:

1. Read the stored Loop config: resolve `compozy__loop_inspect` /
   `compozy__loop_status` surfaces or run a dry-run of `implement-tasks` and
   check `effective_config.runtime_rules`. If it is already populated, the
   workspace is configured — skip to normal operation.
2. If not configured: read the `batuta-routing` skill with
   `compozy__skill_view`, extract the ```json runtime_rules block, and apply
   it as the stored override with `compozy__loop_configure`
   (`name: implement-tasks`, field `runtime_rules`).
3. Ask the operator (in conversation, one question at a time) only the
   preferences that matter:
   - auto-commit per task? (default: yes — it is the atomic-commit
     guarantee; if no, diffs stay for manual review)
   - which lane for `critical` tasks? (default: the table's)
   Persist auto-commit with `compozy__config_set` on
   `loops.inputs.implement-tasks.auto_commit` and
   `loops.inputs.review-and-fix.auto_commit` (workspace scope).
4. Reconfiguration later is a conversation request: re-apply the override
   with `compozy__loop_configure` and confirm with a structured read.

Provider authentication is an operator surface (README prerequisite), never
something you configure or ask secrets for.

## Phase PM (conversation, this session)

Requirement intake happens here — dialogue is the clarification mechanism.

- Use the `cy-create-prd` skill to produce `_prd.md` + `_user_stories.md`.
- Use `cy-create-techspec` for `_techspec.md` + `_tests.md`.
- Use `cy-create-tasks` for `_tasks.md` + `task_NN.md`. It writes `type` and
  `complexity` frontmatter per task — that frontmatter is what routing
  matches on, so review the complexity assignments with the operator during
  the interactive approval step.
- Small, unambiguous requests may skip PRD/TechSpec, but never skip
  `cy-create-tasks`: tasks are the unit of dispatch, commit, and routing.

## Dispatch (two chained Loops, both bundled — never forked)

1. **Implementation**: start `implement-tasks` with `compozy__loop_run`:
   - `inputs`: `slug=<feature-slug>`; `auto_commit` comes from the stored
     input default set at bootstrap.
   - Per-run runtime rules: reuse the stored override; add per-task `id`
     rules only for operator reclassifications or escalations.
   - Always dry-run first (`dry: true`) and confirm resolved inputs and
     runtime rules before the real run.
2. **Review**: when the implementation run reaches a terminal state, report
   it exactly. Only on `done`, start `review-and-fix` with
   `inputs: task_name=<feature-slug>`. It reviews, writes review artifacts,
   fixes in batches, and repeats up to 3 generations until a round is clean.
3. Report the final terminal outcome of both runs, with run IDs and the
   `web_url` when available.

While a run is live: observe with `compozy__loop_status` / `compozy__loop_runs`;
routing decisions are auditable in each generation's `resolved_runtime`.

## Escalation and failure

- Retry/quarantine/failure classes belong to the daemon — do not
  re-implement them. Inspect `compozy__loop_nodes` for quarantined or
  attention cells and report them.
- A task failing repeatedly in its lane: re-dispatch with a per-run
  `--runtime id=task_NN:<next lane up>` rule (see `batuta-routing`).
- `needs-approval` is a live pause on a human gate. You must not approve a
  run you started (the daemon denies it: `approval_self_denied`) — surface
  the gate to the operator with run ID and gate ID, and wait.
- Requirement ambiguity mid-run surfaces as Goal `blocked` with evidence or
  a human gate; bring it back to conversation, resolve, then re-dispatch or
  resume. Never guess on the operator's behalf.

## What you never do

- Write, edit, or commit code yourself.
- Fork or mutate the bundled Loop definitions.
- Push to any remote.
- Approve your own runs.
- Report a terminal state other than the daemon's exact one.
EOF
```

- [ ] **Step 2: Validar o manifest com o agente presente**

Run: `tests/contract/test_01_validate.sh`
Expected: PASS — a família `agents` agora tem conteúdo real.

- [ ] **Step 3: Rodar o teste de roteamento (regressão)**

Run: `tests/contract/test_02_routing_dryrun.sh`
Expected: PASS.

- [ ] **Step 4: Commit**

```bash
git add agents
git commit -m "feat: agente batuta (maestro) como recurso da extensão"
```

---

### Task 4: Ciclo de vida install → enable → inventory → remove

**Files:**
- Create: `tests/contract/test_03_lifecycle.sh`

**Interfaces:**
- Consumes: extensão completa (Tasks 1–3).
- Produces: prova executável de que o daemon real publica o agente `batuta` e a skill `batuta-routing`. É o teste nº 1 da spec ("install + enable + `extension inventory` confere os recursos publicados").

- [ ] **Step 1: Escrever o teste**

O teste instala do path local (fonte `local_path`, exige `--allow-unverified --yes`), confere o inventory, e remove ao final (instalação publicada global → `remove --global`). Ele é destrutivo-e-restaurador por design; se uma instalação `batuta` já existir, aborta em vez de destruí-la.

```bash
cat > tests/contract/test_03_lifecycle.sh <<'EOF'
#!/usr/bin/env bash
# Ciclo de vida: install local_path -> enable -> inventory -> remove.
set -euo pipefail
cd "$(dirname "$0")/../.."

if compozy extension list -o json | python3 -c '
import json,sys
rows=json.load(sys.stdin)
sys.exit(0 if any(r["name"]=="batuta" for r in rows) else 1)'; then
  echo "ABORT: extensao batuta ja instalada; remova manualmente antes de rodar" >&2
  exit 1
fi

cleanup() { compozy extension remove batuta --global -o json >/dev/null 2>&1 || true; }
trap cleanup EXIT

compozy extension install . --allow-unverified --yes -o json \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); print("install:", json.dumps(d)[:200])'

compozy extension enable batuta -o json >/dev/null 2>&1 || true  # install pode ja habilitar

compozy extension inventory batuta -o json | python3 - <<'PY'
import json, sys
d = json.load(sys.stdin)
blob = json.dumps(d)
assert "batuta" in blob, "agente batuta ausente do inventory"
assert "batuta-routing" in blob, "skill batuta-routing ausente do inventory"
print("OK: inventory publica agente batuta e skill batuta-routing")
PY

compozy extension remove batuta --global -o json | python3 -c '
import json,sys
d=json.load(sys.stdin)
assert d.get("status") in ("removed", None) or "removed" in json.dumps(d), d
print("OK: remocao limpa")'
trap - EXIT
EOF
chmod +x tests/contract/test_03_lifecycle.sh
```

- [ ] **Step 2: Rodar o teste**

Run: `tests/contract/test_03_lifecycle.sh`
Expected: PASS — `OK: inventory publica...` e `OK: remocao limpa`.

Diagnóstico se falhar: `compozy extension preview batuta -o json` mostra added/changed/removed antes do enable; um erro de install nomeia o campo do manifest. Corrigir o `extension.toml` (Task 1) conforme o erro estruturado — o shape das entradas de recurso segue o `extension.json` do `dev-cycle`.

- [ ] **Step 3: Rodar a suíte completa**

Run: `tests/contract/run.sh`
Expected: PASS nos três testes.

- [ ] **Step 4: Commit**

```bash
git add tests/contract/test_03_lifecycle.sh
git commit -m "test: contrato de ciclo de vida install/enable/inventory/remove"
```

---

### Task 5: README (pré-requisitos e uso)

**Files:**
- Create: `README.md`

**Interfaces:**
- Consumes: nomes finais de tudo: extensão `batuta`, agente `batuta`, skill `batuta-routing`, Loops bundled, lanes da Task 2.

- [ ] **Step 1: Escrever o README**

```bash
cat > README.md <<'EOF'
# batuta-compozy

O Batuta como extensão resource-only do CompozyOS: um agente maestro que rege
o dev-cycle (skills `cy-*` + Loops bundled) com roteamento de runtime por
custo/complexidade. O maestro nunca escreve código — classifica, decompõe,
despacha e reporta.

Design completo: `docs/superpowers/specs/2026-08-11-batuta-compozy-design.md`.

## Pré-requisitos

1. CompozyOS >= 0.3.0 com daemon rodando (`compozy status`).
2. Extensão bundled `dev-cycle` ativa (`compozy extension list`) — ela publica
   as skills `cy-*` e os Loops `implement-tasks` / `review-and-fix`.
3. **Autenticação dos providers das lanes** (superfície de operador, uma vez,
   global — fora do escopo da extensão):

   ```bash
   compozy provider auth login opencode   # lane low
   compozy provider auth login codex      # lanes medium/high
   compozy provider auth login claude     # lane critical
   ```

   Confira com `compozy provider models list`.

## Instalação (local/dev)

```bash
compozy extension install ~/Projects/batuta-compozy --allow-unverified --yes
compozy extension enable batuta
compozy extension inventory batuta -o json   # deve listar o agente batuta e a skill batuta-routing
```

## Uso

Crie uma sessão com o agente `batuta` no workspace do seu projeto e converse.
No primeiro contato o batuta se auto-configura: aplica a tabela default da
skill `batuta-routing` como override do Loop `implement-tasks` e pergunta as
poucas preferências que importam (auto-commit, lane da `critical`).

Fluxo: fase PM em conversa (PRD → TechSpec → tasks via skills `cy-*`) →
despacho do `implement-tasks` (um ciclo isolado + um commit por task) →
`review-and-fix` (rodadas de review até limpar) → resultado terminal exato.

## Roteamento

Tabela default em `resources/skills/batuta-routing/SKILL.md`
(lanes `low`/`medium`/`high`/`critical`). Para mudar num workspace, peça em
conversa ao batuta — ele regrava o override com `loop configure`. A decisão
de roteamento fica auditável por geração em `resolved_runtime`.

## Testes

```bash
tests/contract/run.sh
```

Smoke E2E guiado: `tests/e2e/SMOKE.md`.
EOF
```

- [ ] **Step 2: Conferir os fatos do README contra o repo**

Run: `grep -n "batuta-routing\|implement-tasks\|review-and-fix" README.md resources/skills/batuta-routing/SKILL.md agents/batuta/AGENT.md | head -20`
Expected: nomes consistentes nos três arquivos (mesma grafia, mesmos Loops).

- [ ] **Step 3: Commit**

```bash
git add README.md
git commit -m "docs: README com pré-requisitos de provider e uso"
```

---

### Task 6: Smoke E2E guiado (repo cobaia)

**Files:**
- Create: `tests/e2e/SMOKE.md`

**Interfaces:**
- Consumes: extensão instalada e habilitada (Task 4), providers autenticados (README).
- Produces: roteiro executável por um operador humano — o teste nº 3 da spec. Não é automatizado: gasta tokens reais e exige um repo cobaia; o roteiro fixa os critérios de aceite verificáveis.

- [ ] **Step 1: Escrever o roteiro**

```bash
mkdir -p tests/e2e
cat > tests/e2e/SMOKE.md <<'EOF'
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
EOF
```

- [ ] **Step 2: Rodar a suíte de contrato (regressão final)**

Run: `tests/contract/run.sh`
Expected: PASS nos três testes.

- [ ] **Step 3: Commit**

```bash
git add tests/e2e/SMOKE.md
git commit -m "test: roteiro de smoke E2E guiado"
```

- [ ] **Step 4: Executar o smoke com o operador**

O roteiro exige humano + repo cobaia + tokens: agendar com o operador, não executar autonomamente. O resultado (aprovado/reprovado + evidência) entra como nota no roteiro ou num follow-up.
