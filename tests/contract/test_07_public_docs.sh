#!/usr/bin/env bash
# Verifies Batuta's public documentation and contribution contract.
set -euo pipefail
cd "$(dirname "$0")/../.."

require_file() {
  [[ -f $1 && ! -L $1 ]] || {
    printf 'missing regular public documentation file: %s\n' "$1" >&2
    exit 1
  }
}

require_text() {
  local document=$1 text=$2
  if ! grep -qF -- "$text" "$document"; then
    printf 'missing public documentation text in %s: %s\n' "$document" "$text" >&2
    exit 1
  fi
}

require_file docs/architecture.md
require_file CONTRIBUTING.md

for text in \
  'Independent community project' \
  'docs/architecture.md' \
  'docs/case-studies/version-subcommand.md' \
  'CONTRIBUTING.md' \
  'LICENSE' \
  'https://www.compozy.com/docs/' \
  'https://github.com/compozy/compozy'; do
  require_text README.md "$text"
done

for text in \
  'Projeto independente da comunidade' \
  'docs/architecture.md' \
  'docs/case-studies/version-subcommand.md' \
  'CONTRIBUTING.md' \
  'LICENSE' \
  'https://www.compozy.com/docs/' \
  'https://github.com/compozy/compozy'; do
  require_text README.pt-BR.md "$text"
done

for text in \
  'Operator' \
  'Batuta session' \
  'spec-cycle' \
  'ext__spec_cycle__import_tasks' \
  'batuta-deliver' \
  'implement-tasks' \
  'review-and-fix' \
  'compozy__session_prompt' \
  'Resource and authority boundaries'; do
  require_text docs/architecture.md "$text"
done

for text in \
  'bash -n scripts/*.sh tests/contract/*.sh' \
  "python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v" \
  'tests/contract/run.sh' \
  'git diff --check' \
  '^(build|ci|docs|feat|fix|perf|refactor|test): [a-z].+$' \
  '.compozy/'; do
  require_text CONTRIBUTING.md "$text"
done

for text in ../../LICENSE ../architecture.md ../case-studies/version-subcommand.md; do
  require_text docs/releases/0.1.0-beta.2.md "$text"
done

python3 - README.md README.pt-BR.md docs/architecture.md CONTRIBUTING.md \
  docs/releases/0.1.0-beta.2.md <<'PY'
import re
import sys

for name in sys.argv[1:]:
    contents = open(name, encoding="utf-8").read()
    sentences = re.split(r"(?<=[.!?])\s+", contents)
    for sentence in sentences:
        lowered = sentence.lower()
        prohibited = (
            "batuta" in lowered
            and ("official" in lowered or "endorsed" in lowered or "compozyos component" in lowered)
        )
        denial = any(phrase in lowered for phrase in (
            "not official", "not an official", "not endorsed", "not an endorsed",
            "not a compozyos component", "não oficial", "não é oficial",
            "não endossado", "não é endossado", "não um componente oficial",
            "não é um componente do compozyos",
        ))
        if prohibited and not denial:
            raise SystemExit(f"unsupported affiliation claim in {name}: {sentence}")
PY

printf 'OK: public documentation exposes guides, contribution workflow, and independent status\n'
