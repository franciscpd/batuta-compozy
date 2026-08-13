#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

TMP=$(mktemp -d)
LOG="$TMP/calls"
cleanup() {
  rm -rf -- "$TMP"
}
trap cleanup EXIT

cat > "$TMP/compozy" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$BATUTA_FAKE_LOG"
if [[ ${1:-} == version ]]; then
  printf '%s\n' '{"Version":"v0.3.0-beta.13","Commit":"tag","BuildDate":"test"}'
  exit 0
fi
exit 99
SH
chmod +x "$TMP/compozy"

if BATUTA_FAKE_LOG="$LOG" PATH="$TMP:$PATH" scripts/republish.sh >/dev/null 2>&1; then
  printf 'republish aceitou runtime incompativel\n' >&2
  exit 1
fi

if [[ ! -f "$LOG" ]] || rg -q '^extension remove batuta' "$LOG"; then
  printf 'republish removeu a extensao antes de validar o runtime\n' >&2
  exit 1
fi

printf 'OK: republish falha antes de remover em runtime incompativel\n'
