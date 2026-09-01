#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

PYTHONDONTWRITEBYTECODE=1 python3 - <<'PY'
from pathlib import Path
import re

agent = Path("agents/batuta/AGENT.md").read_text(encoding="utf-8")
launcher = Path("loops/batuta-deliver/loop.yaml").read_text(encoding="utf-8")
core = Path("loops/batuta-deliver-core/loop.yaml").read_text(encoding="utf-8")
skill = Path("resources/skills/batuta-routing/SKILL.md").read_text(encoding="utf-8")
readme_en = Path("README.md").read_text(encoding="utf-8")
readme_pt = Path("README.pt-BR.md").read_text(encoding="utf-8")
flatten = lambda text: " ".join(text.split())
agent_flat = flatten(agent)
skill_flat = flatten(skill)

def action_kind(loop_text, node_id):
    node = re.search(
        rf"(?ms)^    - id: {re.escape(node_id)}\n(?P<body>.*?)(?=^    - |\Z)",
        loop_text,
    )
    assert node, node_id
    kind = re.search(r"^      class: action\n      kind: ([^\n]+)$", node["body"], re.M)
    assert kind, node_id
    return kind.group(1)

frontmatter = agent.split("---", 2)[1]
assert "permissions: approve-all" in frontmatter
for tool_pattern in (
    "compozy__*",
    "ext__batuta__executor_inventory",
    "ext__batuta__routing_plan",
    "ext__batuta__routing_apply",
    "ext__batuta__publication_plan",
    "ext__batuta__publish_worktree",
    "ext__batuta__publication_verify",
    "ext__spec_cycle__import_tasks",
):
    assert tool_pattern in frontmatter, tool_pattern
assert "write the SDD artifacts" in agent
assert "Never implement feature code" in agent
assert "repository filesystem tools" in agent
assert ".compozy/tasks/<slug>" in agent
assert "never for feature implementation" in agent
assert re.search(r"workspace\s+boundary", agent)

for ordered in (
    "ext__batuta__executor_inventory",
    "ext__batuta__routing_plan",
    "ext__batuta__routing_apply",
):
    assert ordered in agent, ordered
routing_section = agent.split("## Automatic inventory and routing", 1)[1].split(
    "## Delivery worktree and preflight", 1
)[0]
delivery_section = agent.split("## Delivery worktree and preflight", 1)[1].split(
    "## Terminal return and bounded fallback", 1
)[0]
assert routing_section.index("ext__batuta__executor_inventory") < routing_section.index("ext__batuta__routing_plan")
assert routing_section.index("operation `alignment_status`") < routing_section.index("operation `confirm_alignment`")
assert routing_section.index("ext__batuta__routing_plan") < routing_section.index("operation `alignment_status`")
assert delivery_section.index("operation `bootstrap_repository`") < delivery_section.index("compozy__worktree_create")
assert delivery_section.index("operation `apply_matrix`") < delivery_section.index("operation `start_delivery`")
for text in (agent_flat, skill_flat):
    assert "a successful `routing_plan` result is the only authority" in text.lower()
    assert "copy its returned generation digest verbatim" in text.lower()
    assert "never construct, hash, infer, or reuse a digest" in text.lower()
    assert "a second routing rejection is terminal" in text.lower()
    assert "zero `routing_apply` calls" in text.lower()
    assert "one `compozy__tool_info` read" in text.lower()
    assert "stop and report the blocker" in text.lower()
    assert "never call extension reload, install, remove, validate, or logs" in text.lower()
    assert "never inspect daemon or extension process environments" in text.lower()
    assert "routing planning is independent of git repository initialization" in text.lower()
    assert "`model_below_floor` is candidate evidence only" in text.lower()
    assert "within the single permitted retry" in text.lower()
    assert "never reinterpret a routing rejection from worktree or git state" in text.lower()
    assert "never ask the operator to run `git init`, `git add`, or `git commit`" in text.lower()
    assert "never merge stderr into structured stdout" in text.lower()
    assert "parse only stdout as the single json document" in text.lower()
    assert "routing rejection is session evidence, not durable memory" in text.lower()
    assert "never write provider-specific memory files" in text.lower()
    assert "git_backed:false" not in text
assert "successful `routing_plan` result" in routing_section
assert "operation `confirm_alignment`" in routing_section
assert "operation `bootstrap_repository`" in delivery_section
assert "present the derived table" in agent_flat.lower()
assert "blocked_sensitive_paths" in agent
assert "chore: initialize workspace" in agent
assert "auto_commit=true" in agent
assert "compozy tool invoke <tool-id>" in agent
assert "--session <current-session-id>" in agent
assert "--workspace <current-workspace-id-or-path>" in agent
assert "--agent <current-agent-name>" in agent
assert "Never omit `--workspace`" in agent
assert "routing_fit_retryable" in agent
assert "no cli, provider, or model family has a built-in domain preference" in agent_flat.lower()
assert "explicit reasoning effort on that candidate" in agent_flat
assert "unclassified tier" in agent_flat
assert "do not turn those choices into a global or workspace default" in agent_flat
assert "separate metadata and must never be encoded" in agent_flat
assert "encode the order with descending fit scores" in agent_flat.lower()
assert "routing discriminators" in agent
assert "hard_capability_unresolved" in agent
assert "Compozy is the only provider/model execution authority" in agent_flat
assert "`executor_id: compozy`" in agent_flat
assert "Never submit `enrichment_ids`" in agent_flat
assert "A missing enricher cannot exclude a live pair" in agent_flat
assert "Claude Code and Agy are optional enrichers, not execution backends" in agent_flat
assert "never calls `agy models` automatically" in agent_flat
assert "compozy__config_set" in agent
assert "preserve every existing entry" in agent
assert "one-time operator configuration prerequisite" not in agent
assert "guarded tool submits the" in agent.lower()
assert "typed ephemeral overrides" in agent
assert "fresh parent run id" in agent.lower()

assert "kind: ext__" not in launcher
assert "loop: batuta-deliver-core" in launcher
assert launcher.count("kind: run-loop") == 1
assert core.count("kind: run-loop") == 2
assert action_kind(launcher, "delivery_core") == "run-loop"
assert re.search(r"iteration_cap:\s*64\b", core)
assert "auto_commit: true" in core
assert action_kind(core, "run_task") == "run-loop"
assert action_kind(core, "review") == "run-loop"
for node_id in ("load_check", "routing_context", "prepare_wave", "run_task", "review", "cleanup"):
    assert f"id: {node_id}" in core, node_id
assert "routing_generation: {type: string, required: true}" in launcher
assert "kind: human" not in launcher
assert "recovery_gate" not in launcher
for identity in (
    "{{ .effect.identity.loop_run_id }}",
    "{{ .effect.identity.generation }}",
    "{{ .effect.identity.trigger }}",
):
    assert identity in launcher, identity

for required in (
    "resolved | declared | unknown",
    "catalog availability is unknown remains ineligible unless an available dedicated CLI adapter proves that exact provider/model pair",
    "immutable routing generation",
    "ephemeral",
    "does not write Compozy Loop configuration",
    "operator confirms the exact derived matrix",
    "blocked_sensitive_paths",
    "chore: initialize workspace",
    "fresh Compozy parent run",
    "id > type + complexity > type > complexity",
    "Compozy is the only provider/model execution authority",
    "`executor_id: compozy`",
    "Never submit `enrichment_ids`",
    "A missing enricher cannot exclude a live pair",
    "Claude Code and Agy are optional enrichers, not execution backends",
    "never calls `agy models` automatically",
):
    assert required in skill_flat, required
for stale in (
    "ask the operator what their account enables",
    "write a surgical `id` rule",
):
    assert stale not in skill, stale

for text, required in (
    (readme_en, "Compozy is the only provider/model execution authority"),
    (readme_en, "Claude Code and Agy are optional evidence enrichers"),
    (readme_pt, "O Compozy é a única autoridade de execução de provider/modelo"),
    (readme_pt, "Claude Code e Agy são enriquecedores opcionais de evidência"),
):
    assert required in flatten(text), required
for text in (agent, skill, readme_en, readme_pt):
    lowered = text.lower()
    assert "patch compozy" not in lowered
    assert "compozy migration" not in lowered
    assert "rewrite compozy config" not in lowered

print("OK: Batuta owns inventory, domain routing, matrix apply, and bounded fallback without a human gate")
PY
