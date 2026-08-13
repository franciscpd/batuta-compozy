#!/usr/bin/env bash
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
official_beta13 = {
    "594d9fdf": 6,
    "714b7347": 7,
    "81e49510": 8,
    "fee93b73": 9,
    "765eba13": 10,
    "5579d107": 11,
    "26f7b488": 12,
    "2e58013e": 13,
    "36bd8156": 14,
}
post_tag = re.fullmatch(
    r"v?(\d+)\.(\d+)\.(\d+)-beta\.(\d+)-(\d+)-g([0-9a-fA-F]+)",
    version,
)
release = re.fullmatch(
    r"v?(\d+)\.(\d+)\.(\d+)(?:-beta\.(\d+))?",
    version,
)


def resolve_official_hash(value):
    value = value.lower()
    if not re.fullmatch(r"[0-9a-f]{7,40}", value):
        return None
    matches = [known for known in official_beta13 if known.startswith(value) or value.startswith(known)]
    return matches[0] if len(matches) == 1 else None


compatible = False
official_post_tag = False
if post_tag:
    major, minor, patch, beta, count = map(int, post_tag.groups()[:5])
    described_hash = post_tag.group(6)
    core = (major, minor, patch)
    if core == (0, 3, 0) and beta == 13:
        resolved_commit = resolve_official_hash(commit)
        resolved_described = resolve_official_hash(described_hash)
        compatible = (
            resolved_commit is not None
            and resolved_commit == resolved_described
            and official_beta13[resolved_commit] == count
        )
        official_post_tag = compatible
    else:
        compatible = core > (0, 3, 0) or (core == (0, 3, 0) and beta >= 14)
elif release:
    major, minor, patch = map(int, release.groups()[:3])
    beta = release.group(4)
    core = (major, minor, patch)
    compatible = core > (0, 3, 0) or (
        core == (0, 3, 0) and (beta is None or int(beta) >= 14)
    )

if not compatible:
    raise SystemExit(
        f"incompatible CompozyOS {version} ({commit}): Batuta accepts beta.13 "
        "post-tag builds only when Version and Commit identify a known official "
        "descendant containing 594d9fdf, or accepts a later beta/stable release; "
        "arbitrary custom-history counts do not prove ancestry"
    )

if official_post_tag:
    print(
        f"OK: CompozyOS {version} ({commit}) is a known official descendant "
        "containing 594d9fdf"
    )
else:
    print(f"OK: CompozyOS {version} satisfies Batuta's operational floor")
PY
