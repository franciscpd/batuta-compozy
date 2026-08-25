#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

PYTHONDONTWRITEBYTECODE=1 python3 tests/contract/test_routing_pair_selection.py

python3 - resources/skills/batuta-routing/SKILL.md <<'PY'
import sys

text = open(sys.argv[1], encoding="utf-8").read()
for required in (
    "type × complexity",
    "id > type + complexity > type > complexity",
    "compozy__provider_models_list",
    "provider may be present while authentication is not",
):
    assert required in text, required
for domain in (
    "backend", "frontend", "mobile", "data", "infra",
    "security", "testing", "docs", "general", "fullstack",
):
    assert f"`{domain}`" in text, domain
print("OK: routing skill closes domain x complexity and redacted catalog evidence")
PY
