#!/usr/bin/env bash
# Valida a definição do Loop batuta-watch (watch-events em loop.terminal).
set -euo pipefail
cd "$(dirname "$0")/../.."

out=$(compozy loop validate --file loops/batuta-watch/loop.yaml --workspace "$PWD" -o json)
TMP=$(mktemp); trap 'rm -f "$TMP"' EXIT
printf '%s' "$out" > "$TMP"
python3 - "$TMP" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
assert d.get("valid") is True, f"batuta-watch invalido: {d}"
print("OK: batuta-watch valido (lint+compile, kind loop.terminal aceito)")
PY

python3 - loops/batuta-watch/loop.yaml <<'PY'
import sys
text = open(sys.argv[1]).read()
assert "kind: watch-events" in text, "fonte watch-events ausente"
assert "loop.terminal" in text and "batuta-deliver" in text, "assinatura do terminal do deliver ausente"
assert "iteration_cap: 0" in text, "watch deve ser unbounded (iteration_cap 0)"
print("OK: watch assina loop.terminal do batuta-deliver, unbounded")
PY
