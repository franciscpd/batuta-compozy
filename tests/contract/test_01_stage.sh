#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

STAGE=$(mktemp -d)
cleanup() {
  rm -rf -- "$STAGE"
}
trap cleanup EXIT

scripts/stage-extension.sh "$STAGE"

actual=$(cd "$STAGE" && find . -type f -print | LC_ALL=C sort)
expected=$(printf '%s\n' \
  './LICENSE' \
  './agents/batuta-publisher/AGENT.md' \
  './agents/batuta/AGENT.md' \
  './extension.toml' \
  './loops/batuta-deliver/loop.yaml' \
  './resources/skills/batuta-routing/SKILL.md')

if [[ "$actual" != "$expected" ]]; then
  printf 'staging inesperado:\n%s\n' "$actual" >&2
  exit 1
fi

if scripts/stage-extension.sh "$STAGE" >/dev/null 2>&1; then
  printf 'staging aceitou destino nao vazio\n' >&2
  exit 1
fi

printf 'OK: staging contem somente manifest, LICENSE e tres recursos declarados\n'
