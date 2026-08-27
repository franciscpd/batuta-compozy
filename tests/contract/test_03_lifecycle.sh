#!/usr/bin/env bash
# Ciclo de vida: stage source -> build generation -> install -> inventory -> remove.
set -euo pipefail
cd "$(dirname "$0")/../.."

if compozy extension list -o json | python3 -c '
import json,sys
rows=json.load(sys.stdin)
sys.exit(0 if any(r["name"]=="batuta" for r in rows) else 1)'; then
  echo "SKIP: extensao batuta instalada (estado intencional); ciclo de vida requer estado desinstalado"
  exit 0
fi

STAGE=$(mktemp -d /tmp/batuta-lifecycle-source.XXXXXX)
BUILD_TMP_ROOT=${TMPDIR:-/tmp}
BUILD_TMP=$(mktemp -d "$BUILD_TMP_ROOT/batuta-lifecycle-build.XXXXXX")
INV_OUT=$(mktemp)
MUTATION_STARTED=false
cleanup() {
  local original_status=$?
  local cleanup_failed=false
  trap - EXIT

  if [[ $MUTATION_STARTED == true ]]; then
    if ! compozy extension remove batuta --global -o json >/dev/null; then
      printf 'cleanup failed to remove global batuta after lifecycle mutation\n' >&2
      cleanup_failed=true
    fi
  fi
  rm -f -- "$INV_OUT"
  case "$STAGE" in
    /tmp/batuta-lifecycle-source.*) rm -rf -- "$STAGE" ;;
    *)
      printf 'refusing to clean unexpected staging path: %s\n' "$STAGE" >&2
      cleanup_failed=true
      ;;
  esac
  if [[ ${BUILD_TMP##*/} == batuta-lifecycle-build.* && -d $BUILD_TMP && ! -L $BUILD_TMP ]]; then
    rm -rf -- "$BUILD_TMP"
  else
    printf 'refusing to clean unexpected lifecycle build path: %s\n' "$BUILD_TMP" >&2
    cleanup_failed=true
  fi

  if [[ $cleanup_failed == true ]]; then
    exit 1
  fi
  exit "$original_status"
}
trap cleanup EXIT

scripts/stage-extension.sh "$STAGE"
build_json=$(TMPDIR="$BUILD_TMP" GOWORK=off compozy extension build "$STAGE" -o json)
generation_dir=$(python3 -c '
import json, sys
value = json.load(sys.stdin).get("generation_dir")
if not isinstance(value, str) or not value:
    raise SystemExit("extension build returned no generation_dir")
print(value)
' <<<"$build_json")
MUTATION_STARTED=true
compozy extension install "$generation_dir" --allow-unverified --yes -o json \
  | python3 -c 'import json,sys; d=json.load(sys.stdin); print("install:", json.dumps(d)[:200])'

compozy extension enable batuta -o json | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert d.get("enabled") is True, d
'

compozy extension status batuta -o json | python3 -c '
import json, sys
d = json.load(sys.stdin)
assert d.get("state") == "active", d
assert d.get("health") == "healthy", d
'

# Nota: nao usar `cmd | python3 - <<PY`; `python3 -` le o proprio script do
# stdin via heredoc, o que sobrescreve o stdin herdado do pipe antes que o
# script possa ler o JSON de sys.stdin (mesma armadilha documentada em
# test_02_routing_dryrun.sh). Passa o JSON por arquivo temporario + argv.
compozy extension inventory batuta -o json > "$INV_OUT"

python3 - "$INV_OUT" <<'PY'
import json, sys
d = json.load(open(sys.argv[1]))
items = d["items"]
actual = {(item["kind"], item["name"]) for item in items}
expected = {
    ("agent", "batuta"),
    ("loop", "batuta-deliver"),
    ("skill", "batuta-routing"),
    ("tool", "ext__batuta__executor_inventory"),
    ("tool", "ext__batuta__delivery_budget_context"),
    ("tool", "ext__batuta__publication_plan"),
    ("tool", "ext__batuta__publication_verify"),
    ("tool", "ext__batuta__publish_worktree"),
    ("tool", "ext__batuta__routing_apply"),
    ("tool", "ext__batuta__routing_context"),
    ("tool", "ext__batuta__routing_plan"),
}
assert actual == expected, f"inventory inesperado: {sorted(actual)}"
assert all(item["live"] for item in items), f"recursos nao-live: {items}"
print("OK: inventory publica recursos e oito tools live")
PY
rm -f "$INV_OUT"

compozy extension remove batuta --global -o json | python3 -c '
import json,sys
d=json.load(sys.stdin)
assert d.get("status") in ("removed", None) or "removed" in json.dumps(d), d
print("OK: remocao limpa")'
MUTATION_STARTED=false
