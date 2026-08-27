#!/usr/bin/env bash
# Batuta compatibility guard: code-backed routing requires v0.3.0-beta.21 or later.
set -euo pipefail

usage() {
  printf 'usage: %s [--version VERSION --commit COMMIT]\n' "$0" >&2
}

NEUTRAL_CWD=""
cleanup() {
  if [[ -n $NEUTRAL_CWD ]]; then
    case "$NEUTRAL_CWD" in
      /tmp/batuta-version.*) rm -rf -- "$NEUTRAL_CWD" ;;
      *)
        printf 'refusing to clean unexpected version cwd: %s\n' "$NEUTRAL_CWD" >&2
        return 1
        ;;
    esac
  fi
}
trap cleanup EXIT

if [[ $# -eq 0 ]]; then
  NEUTRAL_CWD=$(mktemp -d /tmp/batuta-version.XXXXXX)
  version_json=$(cd "$NEUTRAL_CWD" && compozy version -o json)
  build=$(python3 -c '
import json
import sys

data = json.load(sys.stdin)
version = data.get("Version")
commit = data.get("Commit")
if not isinstance(version, str) or not version:
    raise SystemExit("compozy version JSON is missing Version")
if not isinstance(commit, str) or not commit:
    raise SystemExit("compozy version JSON is missing Commit")
print(version + "\t" + commit)
' <<<"$version_json")
  IFS=$'\t' read -r version commit <<< "$build"
elif [[ $# -eq 4 && $1 == --version && $3 == --commit ]]; then
  version=$2
  commit=$4
else
  usage
  exit 2
fi

python3 - "$version" "$commit" <<'PY'
import re
import sys

version, commit = sys.argv[1:]
FLOOR = (0, 3, 0, 21)          # v0.3.0-beta.21
FLOOR_TEXT = "v0.3.0-beta.21"
VERIFIED_TEXT = "v0.3.0-beta.21"

match = re.fullmatch(
    r"v?(\d+)\.(\d+)\.(\d+)(?:-beta\.(\d+))?"
    r"(?:(?:-(\d+)-g([0-9a-fA-F]+))|(?:\.preview\.([0-9a-fA-F]+)))?",
    version.strip(),
)


def rank(major, minor, patch, beta):
    # A release without -beta.N ranks above every beta of the same triple.
    return (major, minor, patch, float("inf") if beta is None else beta)


if match is None:
    raise SystemExit(
        f"incompatible CompozyOS {version} ({commit}): unrecognized version "
        f"format; Batuta requires {FLOOR_TEXT} or later "
        f"(vMAJOR.MINOR.PATCH[-beta.N][-COUNT-gHASH|.preview.HASH])"
    )

major, minor, patch, beta, count, _, preview_hash = match.groups()
beta_number = int(beta) if beta is not None else None
custom_build = count is not None or preview_hash is not None
if preview_hash is not None and (beta is None or not commit.lower().startswith(preview_hash.lower())):
    raise SystemExit(
        f"incompatible CompozyOS {version} ({commit}): preview identity does not "
        f"match its explicit commit; Batuta requires {FLOOR_TEXT} or later"
    )
compatible = rank(int(major), int(minor), int(patch), beta_number) >= rank(*FLOOR)

if not compatible:
    raise SystemExit(
        f"incompatible CompozyOS {version} ({commit}): Batuta requires "
        f"{FLOOR_TEXT} or later"
    )

if custom_build:
    print(
        f"WARN: CompozyOS {version} ({commit}) is a custom compatible build; "
        f"Batuta is verified on {VERIFIED_TEXT}",
        file=sys.stderr,
    )
print(f"OK: CompozyOS {version} satisfies Batuta's floor {FLOOR_TEXT}")
PY
