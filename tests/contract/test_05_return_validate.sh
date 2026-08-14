#!/usr/bin/env bash
# Valida o contrato público necessário para o retorno terminal de batuta-deliver.
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
  if [[ $REPO_WORKSPACE_PREEXISTED == false ]] && \
    ! cleanup_generated_workspace_marker "$REPO_ROOT"; then
    exit 1
  fi
  exit "$original_status"
}
trap cleanup EXIT
WS=$(require_test_workspace)

schema=$(compozy tool info compozy__session_prompt --workspace "$WS" -o json)
printf '%s' "$schema" | python3 -c '
import json, sys
d = json.load(sys.stdin)["tool"]["descriptor"]["input_schema"]
required = set(d["required"])
expected = {"session_id", "message", "message_id", "idempotency_key"}
assert required == expected, f"campos obrigatorios inesperados: {required}"
assert "queue" in d["properties"]["mode"]["enum"], "mode=queue indisponivel"
print("OK: session_prompt aceita identidade idempotente e mode=queue")
'

python3 - <<'PY'
from pathlib import Path
import re

agent = Path("agents/batuta/AGENT.md").read_text()


def dispatch_section(text):
    match = re.search(r"(?ms)^## Dispatch .*?(?=^## |\Z)", text)
    assert match, "missing Batuta Dispatch section"
    return match.group()


def assert_dispatch_contract(text):
    dispatch = dispatch_section(text)
    ordered_clauses = (
        (
            "hard dispatch boundary",
            r"^4\.\s+After the successful real result, retain its \x60run_id\x60 and "
            r"\x60web_url\x60 when\s+available\. A successful real dispatch is a hard "
            r"turn boundary\. Acknowledge\s+durable acceptance, tell the operator "
            r"the daemon will return here, and\s+end the turn without another "
            r"tool call\.",
        ),
        (
            "terminal-effect return",
            r"^5\.\s+.*?On a terminal-effect turn, the first operational tool "
            r"call is compozy__loop_status for the exact parent delivery\s+run; "
            r"then report literal parent, child, commit, and blocker evidence\. "
            r"Failed\s+terminal-effect delivery never authorizes a watcher or "
            r"polling fallback\.",
        ),
        (
            "operator progress return",
            r"^6\.\s+On an explicit operator progress turn, make one "
            r"compozy__loop_status read\s+for the matching delivery run, report "
            r"the snapshot, and end the turn\.",
        ),
    )
    for description, pattern in ordered_clauses:
        assert re.search(pattern, dispatch, re.MULTILINE | re.DOTALL), (
            f"missing positive ordered Dispatch clause: {description}"
        )

    assert "While a run is live: observe with" not in dispatch, (
        "live-run watcher instruction must not remain after accepted dispatch"
    )
    watcher_instruction = re.compile(r"\b(?:poll|keep\s+watching)\b", re.IGNORECASE)
    assert not watcher_instruction.search(dispatch), (
        "Dispatch must not instruct Batuta to poll or keep watching after acceptance"
    )


assert_dispatch_contract(agent)


def assert_rejected(label, mutated):
    try:
        assert_dispatch_contract(mutated)
    except AssertionError:
        return
    raise AssertionError(f"authored Dispatch contract accepted {label}")


assert_rejected(
    "negated hard-boundary guidance",
    agent.replace(
        "A successful real dispatch is a hard turn boundary.",
        "Never claim that a successful real dispatch is a hard turn boundary.",
    ),
)
assert_rejected(
    "successful-real-dispatch watcher guidance",
    agent.replace(
        "\n## Escalation and failure",
        "\nAfter a successful real dispatch.\nKeep watching for terminal effects."
        "\n\n## Escalation and failure",
    ),
)
assert_rejected(
    "successful-real-dispatch polling guidance",
    agent.replace(
        "\n## Escalation and failure",
        "\nAfter a successful real dispatch, poll for terminal effects."
        "\n\n## Escalation and failure",
    ),
)
print("OK: Batuta dispatch boundary is authored")
PY

out=$(compozy loop validate --file loops/batuta-deliver/loop.yaml --workspace "$WS" -o json)
printf '%s' "$out" | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert d.get("valid") is True, f"batuta-deliver invalido: {d}"
print("OK: batuta-deliver valido (efeitos terminais compilados)")
'
