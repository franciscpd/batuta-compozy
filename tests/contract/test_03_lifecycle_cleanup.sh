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
case "$*" in
  "extension list -o json")
    printf '%s\n' '[]'
    ;;
  "extension install "*)
    printf '%s\n' 'not-json-after-install-mutation'
    ;;
  "extension remove batuta --global -o json")
    if [[ ${BATUTA_FAKE_REMOVE_FAIL:-false} == true ]]; then
      exit 7
    fi
    printf '%s\n' '{"status":"removed"}'
    ;;
  *)
    exit 99
    ;;
esac
SH
chmod +x "$TMP/compozy"

if BATUTA_FAKE_LOG="$LOG" PATH="$TMP:$PATH" \
  tests/contract/test_03_lifecycle.sh >/dev/null 2>&1; then
  printf 'lifecycle aceitou output de install malformado\n' >&2
  exit 1
fi

remove_seen=false
while IFS= read -r call; do
  if [[ $call == "extension remove batuta --global -o json" ]]; then
    remove_seen=true
  fi
done < "$LOG"

if [[ $remove_seen != true ]]; then
  printf 'cleanup nao removeu batuta apos mutacao parcial\n' >&2
  exit 1
fi

printf 'OK: lifecycle remove batuta apos falha de parse pos-install\n'

if out=$(BATUTA_FAKE_LOG="$LOG" BATUTA_FAKE_REMOVE_FAIL=true PATH="$TMP:$PATH" \
  tests/contract/test_03_lifecycle.sh 2>&1); then
  printf 'lifecycle escondeu falha de cleanup\n' >&2
  exit 1
fi

case "$out" in
  *"cleanup failed to remove global batuta"*) ;;
  *)
    printf 'diagnostico de cleanup ausente: %s\n' "$out" >&2
    exit 1
    ;;
esac

printf 'OK: lifecycle expoe falha de cleanup\n'
