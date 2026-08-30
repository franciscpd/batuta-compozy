#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

PYTHONDONTWRITEBYTECODE=1 python3 - <<'PY'
from pathlib import Path
import re

agent = Path("agents/batuta/AGENT.md").read_text(encoding="utf-8")
loop = Path("loops/batuta-deliver/loop.yaml").read_text(encoding="utf-8")
skill = Path("resources/skills/batuta-routing/SKILL.md").read_text(encoding="utf-8")
readme_en = Path("README.md").read_text(encoding="utf-8")
readme_pt = Path("README.pt-BR.md").read_text(encoding="utf-8")
flatten = lambda text: " ".join(text.split())
agent_flat = flatten(agent)
skill_flat = flatten(skill)

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
assert "grok-4.6[effort=high,fast=true]" in agent
assert "highest fit score for every eligible `frontend` cell" in agent
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

assert re.search(r"iteration_cap:\s*4\b", loop)
assert "routing_generation: {type: string, required: true}" in loop
assert "kind: human" not in loop
assert "recovery_gate" not in loop
assert "auto_commit: true" in loop
for identity in (
    "{{ .effect.identity.loop_run_id }}",
    "{{ .effect.identity.generation }}",
    "{{ .effect.identity.trigger }}",
):
    assert identity in loop, identity

for required in (
    "resolved | declared | unknown",
    "unknown is ineligible",
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
