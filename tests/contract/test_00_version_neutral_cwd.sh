#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

TMP=$(mktemp -d)
LOG="$TMP/cwd"
cleanup() {
  rm -rf -- "$TMP"
}
trap cleanup EXIT

cat > "$TMP/compozy" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$PWD" > "$BATUTA_FAKE_CWD_LOG"
printf '%s\n' '{"Version":"v0.3.0-beta.16-9-ga35eda6d","Commit":"a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c","BuildDate":"test"}'
SH
chmod +x "$TMP/compozy"

BATUTA_FAKE_CWD_LOG="$LOG" PATH="$TMP:$PATH" \
  scripts/check-compozy-version.sh >/dev/null

IFS= read -r invoked_cwd < "$LOG"
case "$invoked_cwd" in
  /tmp/batuta-version.*) ;;
  *)
    printf 'compozy version ran outside neutral temp cwd: %s\n' "$invoked_cwd" >&2
    exit 1
    ;;
esac

if [[ -e $invoked_cwd ]]; then
  printf 'version guard left neutral temp cwd behind: %s\n' "$invoked_cwd" >&2
  exit 1
fi

printf 'OK: real version query uses and cleans a neutral cwd\n'
