#!/usr/bin/env bash
# republish.sh contract: guard -> stage source -> build -> validate generation -> reinstall -> inventory.
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
    printf '%s\n' '{"Version":"v0.3.0-beta.21","Commit":"release","BuildDate":"test"}'
    ;;
  "extension build "*" -o json")
    generation="$3/dist/gen-test"
    mkdir -p "$generation"
    printf '%s\n' generated > "$generation/bin"
    printf '%s\n' generated > "$generation/extension.toml"
    printf '{"generation_dir":"%s","generation_hash":"sha256:test"}\n' \
      "$generation"
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
    printf '%s\n' '{"profile":"default","enabled":true}'
    ;;
  "extension status batuta -o json")
    printf '%s\n' '{"name":"batuta","enabled":true,"state":"active","health":"healthy"}'
    ;;
  "extension inventory batuta -o json")
    printf '%s\n' '{"items":[{"kind":"agent","name":"batuta","live":true},{"kind":"loop","name":"batuta-deliver","live":true},{"kind":"loop","name":"batuta-task","live":true},{"kind":"skill","name":"batuta-routing","live":true},{"kind":"tool","name":"ext__batuta__delivery_budget_context","live":true},{"kind":"tool","name":"ext__batuta__delivery_graph","live":true},{"kind":"tool","name":"ext__batuta__executor_inventory","live":true},{"kind":"tool","name":"ext__batuta__publication_plan","live":true},{"kind":"tool","name":"ext__batuta__publication_verify","live":true},{"kind":"tool","name":"ext__batuta__publish_worktree","live":true},{"kind":"tool","name":"ext__batuta__routing_apply","live":true},{"kind":"tool","name":"ext__batuta__routing_context","live":true},{"kind":"tool","name":"ext__batuta__routing_plan","live":true}]}'
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
[[ ${#calls[@]} -eq 9 ]] || {
  printf 'expected exactly 9 compozy calls, got %d:\n%s\n' "${#calls[@]}" "$(cat "$LOG")" >&2
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
expect_call 1 "extension build * -o json"
expect_call 2 "extension validate * -o json"
expect_call 3 "extension list -o json"
expect_call 4 "extension remove batuta --global -o json"
expect_call 5 "extension install * --allow-unverified --yes -o json"
expect_call 6 "extension enable batuta -o json"
expect_call 7 "extension status batuta -o json"
expect_call 8 "extension inventory batuta -o json"

validate_path=${calls[2]#extension validate }
validate_path=${validate_path% -o json}
install_path=${calls[5]#extension install }
install_path=${install_path% --allow-unverified --yes -o json}
[[ $validate_path == "$install_path" ]] || {
  printf 'validate and install used different staging paths: %s vs %s\n' \
    "$validate_path" "$install_path" >&2
  exit 1
}
case "$install_path" in
  /tmp/batuta-republish-source.*/dist/gen-test) ;;
  *)
    printf 'republish did not install the generated directory: %s\n' "$install_path" >&2
    exit 1
    ;;
esac
if [[ -e $install_path ]]; then
  printf 'republish left its generated source tree behind: %s\n' "$install_path" >&2
  exit 1
fi

expected_tree=$(printf '%s\n' \
  './bin' \
  './extension.toml')
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
if [[ $second_count -ne 8 ]]; then
  printf 'expected 8 compozy calls without remove, got %s:\n%s\n' "$second_count" "$(cat "$LOG")" >&2
  exit 1
fi

printf 'OK: republish builds one immutable generation, validates, installs, enables, and verifies it\n'
