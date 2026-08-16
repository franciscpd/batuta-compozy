#!/usr/bin/env bash
# Verifies the public version-subcommand case study is factual and sanitized.
set -euo pipefail
cd "$(dirname "$0")/../.."

case_study=docs/case-studies/version-subcommand.md

[[ -f $case_study && ! -L $case_study ]] || {
  printf 'missing regular case-study file: %s\n' "$case_study" >&2
  exit 1
}

require_text() {
  local text=$1
  if ! grep -qF -- "$text" "$case_study"; then
    printf 'missing case-study factual anchor: %s\n' "$text" >&2
    exit 1
  fi
}

for text in \
  'todo 1.0.0' \
  'auto_commit=false' \
  'cy-create-spec' \
  'cy-create-tasks' \
  'ext__spec_cycle__import_tasks' \
  'batuta-deliver' \
  'implement-tasks' \
  'review-and-fix' \
  '9/9' \
  'README.md' \
  'src/cli.py' \
  'tests/test_cli.py' \
  'executor sessions are not visually nested' \
  'active/idle' \
  'v0.1.0-beta.2'; do
  require_text "$text"
done

python3 - "$case_study" <<'PY'
import re
import sys

name = sys.argv[1]
contents = open(name, encoding="utf-8").read()
forbidden = [
    r"/home/", r"/tmp/", r"COMPOZY_HOME", r"sess[-_]", r"ws_",
    r"looprun-", r"turn-", r"127\.0\.0\.1", r"localhost:\d+",
    r"\bPID\b", r"\$\d+(?:\.\d+)?", r"\bUSD\b",
    r"acp_session_id", r"provider credential", r"raw transcript",
]

for pattern in forbidden:
    if re.search(pattern, contents, re.IGNORECASE):
        raise SystemExit(f"forbidden private evidence in {name}: {pattern}")

for match in re.finditer(r"!?\[[^\]]*\]\(([^)]+)\)", contents):
    target = match.group(1).strip().strip("<>").split(maxsplit=1)[0]
    if any(forbidden_path in target for forbidden_path in (
        ".superpowers/", ".compozy/", ".local/state/",
    )):
        raise SystemExit(f"forbidden private Markdown link in {name}: {target}")
PY

printf 'OK: case study preserves public factual anchors without private evidence\n'
