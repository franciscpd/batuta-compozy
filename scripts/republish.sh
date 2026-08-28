#!/usr/bin/env bash
# Local dev install: stage source, build one immutable generation, reinstall, verify.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
cd "$ROOT"

scripts/check-compozy-version.sh >/dev/null

SOURCE_STAGE=$(mktemp -d /tmp/batuta-republish-source.XXXXXX)
BUILD_TMP_ROOT=${TMPDIR:-/tmp}
BUILD_TMP=$(mktemp -d "$BUILD_TMP_ROOT/batuta-republish-build.XXXXXX")
cleanup() {
  case "$SOURCE_STAGE" in
    /tmp/batuta-republish-source.*) rm -rf -- "$SOURCE_STAGE" ;;
    *)
      printf 'refusing to clean unexpected source staging path: %s\n' "$SOURCE_STAGE" >&2
      return 1
      ;;
  esac
  if [[ ${BUILD_TMP##*/} != batuta-republish-build.* || ! -d $BUILD_TMP || -L $BUILD_TMP ]]; then
    printf 'refusing to clean unexpected build scratch path: %s\n' "$BUILD_TMP" >&2
    return 1
  fi
  rm -rf -- "$BUILD_TMP"
}
trap cleanup EXIT
scripts/stage-extension.sh "$SOURCE_STAGE"

build_json=$(TMPDIR="$BUILD_TMP" GOWORK=off compozy extension build "$SOURCE_STAGE" -o json)
generation_dir=$(python3 -c '
import json, sys
data = json.load(sys.stdin)
generation = data.get("generation_dir")
if not isinstance(generation, str) or not generation:
    raise SystemExit("extension build returned no generation_dir")
print(generation)
' <<<"$build_json")

if [[ ! -d $generation_dir || -L $generation_dir ]]; then
  printf 'extension build returned an invalid generation directory: %s\n' "$generation_dir" >&2
  exit 1
fi
source_canonical=$(cd "$SOURCE_STAGE" && pwd -P)
generation_canonical=$(cd "$generation_dir" && pwd -P)
case "$generation_canonical" in
  "$source_canonical"/dist/gen-*) ;;
  *)
    printf 'extension build returned a generation outside staged source: %s\n' \
      "$generation_canonical" >&2
    exit 1
    ;;
esac

compozy extension validate "$generation_canonical" -o json | python3 -c '
import json, sys
d = json.load(sys.stdin)
errors = [i for i in d.get("issues", []) if i.get("severity") == "error"]
assert not errors, f"pacote invalido: {errors}"
'

if compozy extension list -o json | python3 -c '
import json, sys
rows = json.load(sys.stdin)
raise SystemExit(0 if any(row.get("name") == "batuta" for row in rows) else 1)
'; then
  compozy extension remove batuta --global -o json >/dev/null
fi

compozy extension install "$generation_canonical" --allow-unverified --yes -o json >/dev/null
compozy extension enable batuta -o json >/dev/null
compozy extension status batuta -o json | python3 -c "
import json,sys
d=json.load(sys.stdin)
assert d.get('enabled') is True, f'extensao nao habilitada: {d}'
assert d.get('state')=='active', f'extensao nao ativa: {d}'
assert d.get('health')=='healthy', f'extensao sem saude: {d}'
print('extensao ativa')"

compozy extension inventory batuta -o json | python3 -c "
import json,sys
items=json.load(sys.stdin)['items']
actual={(it['kind'], it['name']) for it in items}
expected={
  ('agent','batuta'),
  ('loop','batuta-deliver'),
  ('loop','batuta-task'),
  ('skill','batuta-routing'),
  ('tool','ext__batuta__delivery_budget_context'),
  ('tool','ext__batuta__delivery_graph'),
  ('tool','ext__batuta__executor_inventory'),
  ('tool','ext__batuta__publication_plan'),
  ('tool','ext__batuta__publication_verify'),
  ('tool','ext__batuta__publish_worktree'),
  ('tool','ext__batuta__routing_apply'),
  ('tool','ext__batuta__routing_context'),
  ('tool','ext__batuta__routing_plan'),
}
assert actual==expected, f'inventario inesperado: {sorted(actual)}'
assert all(it['live'] for it in items), f'recursos nao-live: {items}'
print('recursos live:', ', '.join(it['name'] for it in items))"
