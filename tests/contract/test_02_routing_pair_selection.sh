#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

PYTHONDONTWRITEBYTECODE=1 python3 tests/contract/test_routing_pair_selection.py

python3 - resources/skills/batuta-routing/SKILL.md <<'PY'
import sys

text = open(sys.argv[1], encoding="utf-8").read()
flat = " ".join(text.split())
for required in (
    "domain × complexity",
    "id > type + complexity > type > complexity",
    "immutable routing generation",
    "executable catalog projection",
    "catalog availability is unknown remains ineligible unless an available dedicated CLI adapter proves that exact provider/model pair",
    "unknown provider auth is degraded",
    "missing enricher cannot exclude a live pair",
):
    assert required.lower() in flat.lower(), required
for domain in (
    "backend", "frontend", "mobile", "data", "infra",
    "security", "testing", "docs", "general", "fullstack",
):
    assert f"`{domain}`" in text, domain
print("OK: routing skill closes domain x complexity and redacted catalog evidence")
PY
