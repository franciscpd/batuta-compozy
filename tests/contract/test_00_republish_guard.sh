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

remove_seen=false
if [[ -f $LOG ]]; then
  while IFS= read -r call; do
    if [[ $call == "extension remove batuta"* ]]; then
      remove_seen=true
    fi
  done < "$LOG"
fi

if [[ ! -f $LOG || $remove_seen == true ]]; then
  printf 'republish removeu a extensao antes de validar o runtime\n' >&2
  exit 1
fi

printf 'OK: republish falha antes de remover em runtime incompativel\n'
