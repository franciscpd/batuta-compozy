#!/usr/bin/env bash
# republish.sh contract: guard -> stage -> validate -> remove -> install -> enable -> inventory, temp staging removed.
set -euo pipefail
cd "$(dirname "$0")/../.."

TMP=$(mktemp -d /tmp/batuta-republish-test.XXXXXX)
LOG="$TMP/calls"
TREE="$TMP/installed-tree"
cleanup() {
  case "$TMP" in
    /tmp/batuta-republish-test.*) rm -rf -- "$TMP" ;;
  esac
}
trap cleanup EXIT

cat > "$TMP/compozy" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$BATUTA_FAKE_LOG"
case "$*" in
  "version -o json")
    printf '%s\n' '{"Version":"v0.3.0-beta.16-9-ga35eda6d","Commit":"a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c","BuildDate":"test"}'
    ;;
  "extension validate "*)
    printf '%s\n' '{"issues":[]}'
    ;;
  "extension list -o json")
    printf '%s\n' '[{"name":"batuta","version":"0.1.0-beta.3","state":"active"}]'
    ;;
  "extension remove batuta --global -o json")
    printf '%s\n' '{"status":"removed"}'
    ;;
  "extension install "*" --allow-unverified --yes -o json")
    (cd "$3" && find . -type f | LC_ALL=C sort) > "$BATUTA_FAKE_TREE"
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

BATUTA_FAKE_LOG="$LOG" BATUTA_FAKE_TREE="$TREE" PATH="$TMP:$PATH" \
  scripts/republish.sh >/dev/null

mapfile -t calls < "$LOG"
[[ ${#calls[@]} -eq 7 ]] || {
  printf 'expected exactly 7 compozy calls, got %d:\n%s\n' "${#calls[@]}" "$(cat "$LOG")" >&2
  exit 1
}
expect_call() {
  local index=$1 pattern=$2
  # shellcheck disable=SC2053
  if [[ ${calls[$index]} != $pattern ]]; then
    printf 'call %d mismatch: expected %s, got %s\n' "$index" "$pattern" "${calls[$index]}" >&2
    exit 1
  fi
}
expect_call 0 "version -o json"
expect_call 1 "extension validate * -o json"
expect_call 2 "extension list -o json"
expect_call 3 "extension remove batuta --global -o json"
expect_call 4 "extension install * --allow-unverified --yes -o json"
expect_call 5 "extension enable batuta -o json"
expect_call 6 "extension inventory batuta -o json"

validate_path=${calls[1]#extension validate }
validate_path=${validate_path% -o json}
install_path=${calls[4]#extension install }
install_path=${install_path% --allow-unverified --yes -o json}
[[ $validate_path == "$install_path" ]] || {
  printf 'validate and install used different staging paths: %s vs %s\n' \
    "$validate_path" "$install_path" >&2
  exit 1
}
case "$install_path" in
  /tmp/batuta-republish.*) ;;
  *)
    printf 'staging path is not a guarded temp dir: %s\n' "$install_path" >&2
    exit 1
    ;;
esac
if [[ -e $install_path ]]; then
  printf 'republish left its staging directory behind: %s\n' "$install_path" >&2
  exit 1
fi

expected_tree=$(printf '%s\n' \
  './LICENSE' \
  './agents/batuta-publisher/AGENT.md' \
  './agents/batuta/AGENT.md' \
  './extension.toml' \
  './loops/batuta-deliver/loop.yaml' \
  './resources/skills/batuta-routing/SKILL.md')
if [[ $(cat "$TREE") != "$expected_tree" ]]; then
  printf 'installed staging tree mismatch:\n%s\n' "$(cat "$TREE")" >&2
  exit 1
fi

# When batuta is not installed, remove must not be called.
: > "$LOG"
sed -i 's/\[{"name":"batuta","version":"0.1.0-beta.3","state":"active"}\]/[]/' "$TMP/compozy"
BATUTA_FAKE_LOG="$LOG" BATUTA_FAKE_TREE="$TREE" PATH="$TMP:$PATH" \
  scripts/republish.sh >/dev/null
if grep -q '^extension remove' "$LOG"; then
  printf 'republish removed an extension that was not installed\n' >&2
  exit 1
fi
second_count=$(wc -l < "$LOG")
if [[ $second_count -ne 6 ]]; then
  printf 'expected 6 compozy calls without remove, got %s:\n%s\n' "$second_count" "$(cat "$LOG")" >&2
  exit 1
fi

printf 'OK: republish stages to a temp dir, validates, reinstalls, enables, and verifies in order\n'
