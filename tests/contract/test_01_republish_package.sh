#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

TMP=$(mktemp -d /tmp/batuta-republish-test.XXXXXX)
LOG="$TMP/calls"
PACKAGE_ROOT="$TMP/packages"
cleanup() {
  case "$TMP" in
    /tmp/batuta-republish-test.*)
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
    printf '%s\n' '{"issues":[]}'
    ;;
  "extension list -o json")
    printf '%s\n' '[]'
    ;;
  "extension install "*)
    printf '%s\n' '{}'
    ;;
  "extension enable batuta -o json")
    printf '%s\n' '{"extension":{"state":"active"}}'
    ;;
  "extension inventory batuta -o json")
    printf '%s\n' '{"items":[{"kind":"agent","name":"batuta","live":true},{"kind":"loop","name":"batuta-deliver","live":true},{"kind":"skill","name":"batuta-routing","live":true}]}'
    ;;
  *)
    exit 99
    ;;
esac
SH
chmod +x "$TMP/compozy"

BATUTA_FAKE_LOG="$LOG" BATUTA_PACKAGE_ROOT="$PACKAGE_ROOT" \
  PATH="$TMP:$PATH" scripts/republish.sh >/dev/null

install_path=""
while IFS= read -r call; do
  case "$call" in
    "extension install "*" --allow-unverified --yes -o json")
      install_path=${call#extension install }
      install_path=${install_path% --allow-unverified --yes -o json}
      ;;
  esac
done < "$LOG"

if [[ -z $install_path || ! -d $install_path || ${install_path%/*} != "$PACKAGE_ROOT" ]]; then
  printf 'republish did not retain its installed source: %s\n' "$install_path" >&2
  exit 1
fi

printf 'OK: republish installs from a retained content-addressed package\n'
