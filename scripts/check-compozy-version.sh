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
    "594d9fdf041e722fca5f60a62351c4c084c71430": 6,
    "714b7347ba8922eb123ea0c57630cf2a01b92b37": 7,
    "81e49510735deac14201f144fe068c9cc1ff97ef": 8,
    "fee93b73b740650d64fb47a749b6248889baa613": 9,
    "765eba13aceda231d2c5537053b7415fe08466a0": 10,
    "5579d10763db6d0b9552768237acd5a8d26e6ddf": 11,
    "26f7b488cd09c42fdc6759b15c4ebd149594e2e2": 12,
    "2e58013e4504f3fe7c7ea81762867b6ef6c621d3": 13,
    "36bd8156bba6f91b10929ed4d5e1b91623a3cb5f": 14,
    "4154d25c89794dff634fccef00a3d968fc09c3f9": 15,
}
trusted_post_tag_builds = {
    "ef1bc78d0f6c1cd02adadd949483439bb0f43b6c":
        "v0.3.0-beta.15-6-gef1bc78d",
    "c88b3e5274e86103215fbf900faf742d6593b7dd":
        "v0.3.0-beta.15-26-gc88b3e52",
}
post_tag = re.fullmatch(
    r"v?(\d+)\.(\d+)\.(\d+)-beta\.(\d+)-(\d+)-g([0-9a-fA-F]+)",
    version,
)
release = re.fullmatch(
    r"v?(\d+)\.(\d+)\.(\d+)(?:-beta\.(\d+))?",
    version,
)


def resolve_commit(value):
    value = value.lower()
    if len(value) not in {8, 40} or not re.fullmatch(r"[0-9a-f]+", value):
        return None
    if len(value) == 40:
        return value if value in official_beta13 else None
    matches = [known for known in official_beta13 if known.startswith(value)]
    return matches[0] if len(matches) == 1 else None


def resolve_described_hash(value):
    value = value.lower()
    if not re.fullmatch(r"[0-9a-f]{7,40}", value):
        return None
    matches = [known for known in official_beta13 if known.startswith(value)]
    return matches[0] if len(matches) == 1 else None


def resolve_trusted_post_tag(value):
    value = value.lower()
    if len(value) not in {8, 40} or not re.fullmatch(r"[0-9a-f]+", value):
        return None
    if len(value) == 40:
        return value if value in trusted_post_tag_builds else None
    matches = [known for known in trusted_post_tag_builds if known.startswith(value)]
    return matches[0] if len(matches) == 1 else None


compatible = False
official_post_tag = False
trusted_post_tag = False
if post_tag:
    major, minor, patch, beta, count = map(int, post_tag.groups()[:5])
    described_hash = post_tag.group(6)
    core = (major, minor, patch)
    if core == (0, 3, 0) and beta == 13:
        resolved_commit = resolve_commit(commit)
        resolved_described = resolve_described_hash(described_hash)
        compatible = (
            resolved_commit is not None
            and resolved_commit == resolved_described
            and official_beta13[resolved_commit] == count
        )
        official_post_tag = compatible
    else:
        resolved_commit = resolve_trusted_post_tag(commit)
        compatible = (
            resolved_commit is not None
            and trusted_post_tag_builds[resolved_commit] == version.lower()
        )
        trusted_post_tag = compatible
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
elif trusted_post_tag:
    print(f"OK: CompozyOS {version} ({commit}) is an exact trusted test build")
else:
    print(f"OK: CompozyOS {version} satisfies Batuta's operational floor")
PY
