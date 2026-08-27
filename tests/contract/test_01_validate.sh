#!/usr/bin/env bash
# Builds staged production source and validates the immutable generated manifest.
set -euo pipefail
cd "$(dirname "$0")/../.."

SOURCE_STAGE=$(mktemp -d /tmp/batuta-validate-source.XXXXXX)
BUILD_TMP_ROOT=${TMPDIR:-/tmp}
BUILD_TMP=$(mktemp -d "$BUILD_TMP_ROOT/batuta-validate-build.XXXXXX")
cleanup() {
  case "$SOURCE_STAGE" in
    /tmp/batuta-validate-source.*) rm -rf -- "$SOURCE_STAGE" ;;
    *) return 1 ;;
  esac
  if [[ ${BUILD_TMP##*/} != batuta-validate-build.* || ! -d $BUILD_TMP || -L $BUILD_TMP ]]; then
    return 1
  fi
  rm -rf -- "$BUILD_TMP"
}
trap cleanup EXIT

scripts/stage-extension.sh "$SOURCE_STAGE"
build_json=$(TMPDIR="$BUILD_TMP" GOWORK=off compozy extension build "$SOURCE_STAGE" -o json)
generation_dir=$(python3 -c '
import json, sys
value = json.load(sys.stdin).get("generation_dir")
if not isinstance(value, str) or not value:
    raise SystemExit("extension build returned no generation_dir")
print(value)
' <<<"$build_json")

compozy extension validate "$generation_dir" -o json | python3 -c '
import json, sys
d = json.load(sys.stdin)
issues = d.get("issues") or []
errors = [i for i in issues if i.get("severity") == "error"]
assert not errors, f"validate retornou erros: {errors}"
manifest = d.get("manifest") or {}
assert manifest.get("version") == "0.1.0-beta.5", manifest
assert isinstance(manifest.get("min_compozy_version"), str) and manifest["min_compozy_version"], manifest
'

python3 - "$generation_dir/extension.toml" <<'PY'
import sys
import tomllib

with open(sys.argv[1], "rb") as source:
    manifest = tomllib.load(source)

tools = manifest["resources"]["tools"]
expected = {
    "delivery_budget_context", "delivery_graph", "executor_inventory", "publication_plan",
    "publication_verify", "publish_worktree", "routing_apply",
    "routing_context", "routing_plan",
}
assert set(tools) == expected, sorted(tools)
assert manifest["subprocess"]["command"] == "./bin"
print("OK: generated manifest is code-backed and exposes nine tools")
PY
