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
require_file docs/case-studies/version-subcommand.md
require_file CONTRIBUTING.md

for text in \
  'independent community project' \
  'docs/architecture.md' \
  'docs/case-studies/version-subcommand.md' \
  'CONTRIBUTING.md' \
  'LICENSE' \
  'stages the manifest, LICENSE, and declared resources' \
  'https://www.compozy.com/docs/' \
  'https://github.com/compozy/compozy'; do
  require_text README.md "$text"
done

for text in \
  'projeto independente da comunidade' \
  'docs/architecture.md' \
  'docs/case-studies/version-subcommand.md' \
  'CONTRIBUTING.md' \
  'LICENSE' \
  'monta o manifesto, a licença e os recursos declarados' \
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

for text in \
  'https://github.com/franciscpd/batuta-compozy/blob/v0.1.0-beta.2/LICENSE' \
  'https://github.com/franciscpd/batuta-compozy/blob/v0.1.0-beta.2/docs/architecture.md' \
  'https://github.com/franciscpd/batuta-compozy/blob/v0.1.0-beta.2/docs/case-studies/version-subcommand.md'; do
  require_text docs/releases/0.1.0-beta.2.md "$text"
done

python3 - README.md README.pt-BR.md docs/architecture.md CONTRIBUTING.md \
  docs/case-studies/version-subcommand.md docs/releases/0.1.0-beta.2.md <<'PY'
import re
import sys


def unsupported_affiliation(contents):
    sentences = re.split(r"(?<=[.!?])\s+", contents)
    for sentence in sentences:
        lowered = sentence.lower()
        prohibited = (
            "batuta" in lowered
            and any(term in lowered for term in (
                "official", "endorsed", "compozyos component",
                "oficial", "endossad", "componente do compozyos",
                "componente oficial",
            ))
        )
        denial = any(phrase in lowered for phrase in (
            "not official", "not an official", "not endorsed", "not an endorsed",
            "not a compozyos component", "não oficial", "não é oficial",
            "não endossado", "não é endossado", "não um componente oficial",
            "não é um componente oficial", "não é um componente do compozyos",
        ))
        if prohibited and not denial:
            return sentence
    return None


for statement in (
    "Batuta é um componente oficial do CompozyOS.",
    "Batuta é endossado pelo CompozyOS.",
):
    if unsupported_affiliation(statement) is None:
        raise SystemExit(
            f"affiliation scanner accepted affirmative Portuguese claim: {statement}"
        )

for statement in (
    "Batuta is not an official or endorsed CompozyOS component.",
    "O Batuta não é um componente oficial ou endossado do CompozyOS.",
):
    if unsupported_affiliation(statement) is not None:
        raise SystemExit(f"affiliation scanner rejected explicit denial: {statement}")

for name in sys.argv[1:]:
    contents = open(name, encoding="utf-8").read()
    unsupported = unsupported_affiliation(contents)
    if unsupported is not None:
        raise SystemExit(f"unsupported affiliation claim in {name}: {unsupported}")
PY

printf 'OK: public documentation exposes guides, contribution workflow, and independent status\n'
