#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."
source tests/contract/lib.sh
REPO_ROOT=$PWD
REPO_WORKSPACE_PREEXISTED=false
if workspace_marker_present "$REPO_ROOT"; then
  REPO_WORKSPACE_PREEXISTED=true
fi
cleanup() {
  local original_status=$?
  trap - EXIT
  if ! reject_new_repository_marker \
    "$REPO_ROOT" "$REPO_WORKSPACE_PREEXISTED"; then
    exit 1
  fi
  exit "$original_status"
}
trap cleanup EXIT
WS=$(require_test_workspace)

for skill in cy-create-spec cy-create-tasks; do
  compozy skill view "$skill" --workspace "$WS" -o json | \
    python3 -c '
import json, sys
expected = sys.argv[1]
data = json.load(sys.stdin)
assert data["name"] == expected, data
assert data["source"] == "bundled", data
assert data["content"].strip(), data
' "$skill"
done

compozy tool info ext__spec_cycle__import_tasks \
  --workspace "$WS" -o json | python3 -c '
import json, sys
tool = json.load(sys.stdin)["tool"]
descriptor = tool["descriptor"]
availability = tool["availability"]
decision = tool["decision"]
assert descriptor["tool_id"] == "ext__spec_cycle__import_tasks", descriptor
assert descriptor["backend"]["extension_id"] == "spec-cycle", descriptor
assert descriptor["backend"]["handler"] == "import_tasks", descriptor
assert descriptor["read_only"] is True, descriptor
assert availability["available"] is True, availability
assert availability["executable"] is True, availability
assert decision["callable"] is True, decision
'

python3 - <<'PY'
from pathlib import Path

agent = Path("agents/batuta/AGENT.md").read_text()
required = (
    "`cy-create-spec`",
    "`_spec.md`",
    "`_user_stories.md`",
    "`_dx.md`",
    "`_tests.md`",
    "`_uiux.md`",
    "`cy-create-tasks`",
    "`ext__spec_cycle__import_tasks`",
)
for value in required:
    assert value in agent, f"missing current PM contract: {value}"
for retired in (
    "`cy-create-prd`",
    "`cy-create-techspec`",
    "`ext__dev_cycle__import_tasks`",
    "skip PRD/TechSpec",
):
    assert retired not in agent, f"retired PM contract remains active: {retired}"
assert "short grill" in agent, "simple requests must shorten, not skip, the grill"
assert "only when the request changes a Web surface" in agent, (
    "_uiux.md must remain conditional"
)
skill = Path("resources/skills/batuta-routing/SKILL.md").read_text()
for surface in (agent, skill):
    for value in (
        "`compozy__clarify`",
        "one",
        "two to four",
        "recommended",
        "free text",
        "settled",
        "never guess a default",
    ):
        assert value in surface.lower(), f"missing interactive SDD contract: {value}"
    assert "Loop `ask`" in surface, "SDD must reserve ask for running Loop cells"
print("OK: Batuta authors the unified spec-cycle PM, preflight, and clarification contract")
PY
