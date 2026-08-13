#!/usr/bin/env bash
set -euo pipefail

usage() {
  printf 'usage: %s [--version VERSION]\n' "$0" >&2
}

if [[ $# -eq 0 ]]; then
  version_json=$(compozy version -o json)
  version=$(python3 -c '
import json
import sys

data = json.load(sys.stdin)
value = data.get("Version")
if not isinstance(value, str) or not value:
    raise SystemExit("compozy version JSON is missing Version")
print(value)
' <<<"$version_json")
elif [[ $# -eq 2 && $1 == --version ]]; then
  version=$2
else
  usage
  exit 2
fi

python3 - "$version" <<'PY'
import re
import sys

version = sys.argv[1]
post_tag = re.fullmatch(
    r"v?(\d+)\.(\d+)\.(\d+)-beta\.(\d+)-(\d+)-g[0-9a-fA-F]+",
    version,
)
release = re.fullmatch(
    r"v?(\d+)\.(\d+)\.(\d+)(?:-beta\.(\d+))?",
    version,
)

compatible = False
if post_tag:
    major, minor, patch, beta, commits = map(int, post_tag.groups())
    core = (major, minor, patch)
    compatible = core > (0, 3, 0) or (
        core == (0, 3, 0) and (beta > 13 or (beta == 13 and commits >= 6))
    )
elif release:
    major, minor, patch = map(int, release.groups()[:3])
    beta = release.group(4)
    core = (major, minor, patch)
    compatible = core > (0, 3, 0) or (
        core == (0, 3, 0) and (beta is None or int(beta) >= 14)
    )

if not compatible:
    raise SystemExit(
        f"incompatible CompozyOS {version}: Batuta requires an official linear "
        "git-describe build at v0.3.0-beta.13-6 or newer (containing "
        "594d9fdf), or a later beta/stable release; counts from arbitrary "
        "custom histories do not prove ancestry, so base custom builds on a "
        "later release or verify the fix ancestry before continuing"
    )

if post_tag and tuple(map(int, post_tag.groups()[:3])) == (0, 3, 0):
    print(
        f"OK: CompozyOS {version} satisfies Batuta's operational floor for "
        "official linear git-describe provenance; custom-history counts do "
        "not prove ancestry"
    )
else:
    print(f"OK: CompozyOS {version} satisfies Batuta's operational floor")
PY
