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
expected=$({
  printf '%s\n' \
    './LICENSE' \
    './agents/batuta/AGENT.md' \
    './go.mod' \
    './go.sum' \
    './loops/batuta-deliver/loop.yaml' \
    './main.go' \
    './resources/skills/batuta-routing/SKILL.md'
  find internal -type f -name '*.go' ! -name '*_test.go' -print \
    | sed 's#^#./#'
} | LC_ALL=C sort)

if [[ "$actual" != "$expected" ]]; then
  printf 'staging inesperado:\n%s\n' "$actual" >&2
  exit 1
fi

for forbidden in extension.toml .git .compozy dist; do
  if [[ -e $STAGE/$forbidden || -L $STAGE/$forbidden ]]; then
    printf 'staging incluiu entrada proibida: %s\n' "$forbidden" >&2
    exit 1
  fi
done
if find "$STAGE" -type f -name '*_test.go' -print -quit | grep -q .; then
  printf 'staging incluiu arquivos de teste Go\n' >&2
  exit 1
fi

if scripts/stage-extension.sh "$STAGE" >/dev/null 2>&1; then
  printf 'staging aceitou destino nao vazio\n' >&2
  exit 1
fi

printf 'OK: staging contem somente source Go de producao, LICENSE e recursos declarados\n'
