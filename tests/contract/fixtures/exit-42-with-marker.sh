#!/usr/bin/env bash
set -euo pipefail

marker=../../.compozy
mkdir -p "$marker"
printf 'foreign repository marker\n' > "$marker/workspace.toml"
exit 42
