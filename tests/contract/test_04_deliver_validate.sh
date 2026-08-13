#!/usr/bin/env bash
# Valida a definição do Loop batuta-deliver sem publicar (lint + compile no daemon).
set -euo pipefail
cd "$(dirname "$0")/../.."
source tests/contract/lib.sh
WS=$(require_test_workspace)

out=$(compozy loop validate --file loops/batuta-deliver/loop.yaml --workspace "$WS" -o json)
TMP=$(mktemp); trap 'rm -f "$TMP"' EXIT
printf '%s' "$out" > "$TMP"
python3 - "$TMP" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
assert d.get("valid") is True, f"batuta-deliver invalido: {d}"
print("OK: batuta-deliver valido (lint+compile)")
PY

# O grafo deve encadear implement -> review via run-loop, sem tocar nos bundled.
python3 - loops/batuta-deliver/loop.yaml <<'PY'
import sys
text = open(sys.argv[1]).read()
assert "kind: run-loop" in text, "nós run-loop ausentes"
assert "loop: implement-tasks" in text and "loop: review-and-fix" in text, "encadeamento incompleto"
assert text.count("kind: run-loop") == 2, "esperados exatamente 2 nós run-loop"
print("OK: encadeamento implement-tasks -> review-and-fix por composição")
PY
