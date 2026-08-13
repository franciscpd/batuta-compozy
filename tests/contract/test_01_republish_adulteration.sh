#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

TMP=$(mktemp -d /tmp/batuta-adulteration-test.XXXXXX)
PACKAGE_ROOT="$TMP/packages"
LOG="$TMP/calls"
cleanup() {
  case "$TMP" in
    /tmp/batuta-adulteration-test.*)
      chmod -R u+w "$TMP"
      rm -rf -- "$TMP"
      ;;
  esac
}
trap cleanup EXIT

cat > "$TMP/compozy" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$BATUTA_FAKE_LOG"
case "$*" in
  "version -o json")
    printf '%s\n' '{"Version":"v0.3.0-beta.13-14-g36bd8156","Commit":"36bd8156","BuildDate":"test"}'
    ;;
  "extension validate "*)
    chmod u+w "$3/extension.toml"
    printf '\n# adulterated\n' >> "$3/extension.toml"
    printf '%s\n' '{"issues":[]}'
    ;;
  "extension list -o json") printf '%s\n' '[]' ;;
  "extension install "*) printf '%s\n' '{}' ;;
  *) exit 99 ;;
esac
SH
chmod +x "$TMP/compozy"

if BATUTA_FAKE_LOG="$LOG" BATUTA_PACKAGE_ROOT="$PACKAGE_ROOT" \
  PATH="$TMP:$PATH" scripts/republish.sh > "$TMP/out" 2> "$TMP/err"; then
  printf 'republish consumed a package adulterated after validation\n' >&2
  exit 1
fi

while IFS= read -r call; do
  case "$call" in
    "extension remove "*|"extension install "*)
      printf 'republish mutated extension state after package adulteration: %s\n' \
        "$call" >&2
      exit 1
      ;;
  esac
done < "$LOG"

printf 'OK: package adulteration fails before remove/install\n'
