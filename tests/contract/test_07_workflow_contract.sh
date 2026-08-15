#!/usr/bin/env bash
# Verifica o gate reutilizável de candidatos sem precisar chamar o GitHub.
set -euo pipefail
cd "$(dirname "$0")/../.."

WORKFLOW=.github/workflows/ci.yml
CHECKOUT_SHA=3d3c42e5aac5ba805825da76410c181273ba90b1
SETUP_GO_SHA=924ae3a1cded613372ab5595356fb5720e22ba16
UPLOAD_SHA=043fb46d1a93c77aae656e7c1c64a875d1fc6a0a
COMPOZY_COMMIT=a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c

require() {
  local value=$1
  if ! grep -qF -- "$value" "$WORKFLOW"; then
    printf 'missing CI workflow contract: %s\n' "$value" >&2
    exit 1
  fi
}

require_block() {
  local block=$1
  if ! grep -qF -- "$block" "$WORKFLOW"; then
    printf 'missing CI workflow block:\n%s\n' "$block" >&2
    exit 1
  fi
}

[[ -f $WORKFLOW ]] || {
  printf 'missing CI workflow: %s\n' "$WORKFLOW" >&2
  exit 1
}

require_block $'on:\n  pull_request:\n    branches: [main]\n  push:\n    branches: [main]\n  workflow_dispatch:\n  workflow_call:\n    inputs:\n      checkout_ref:\n        required: false\n        type: string'
require_block $'permissions:\n  contents: read'
require_block $'concurrency:\n  group: ci-${{ github.workflow }}-${{ inputs.checkout_ref || github.ref }}\n  cancel-in-progress: true'
require 'verify:'
require 'runs-on: ubuntu-latest'
require 'timeout-minutes: 45'
require 'working-directory: ${{ github.workspace }}/candidate'
require "uses: actions/checkout@$CHECKOUT_SHA"
require "uses: actions/setup-go@$SETUP_GO_SHA"
require "uses: actions/upload-artifact@$UPLOAD_SHA"
require 'go-version: 1.26.4'
require 'ref: ${{ inputs.checkout_ref || github.sha }}'
require 'path: ${{ github.workspace }}/candidate'
require 'repository: compozy/compozy'
require "ref: $COMPOZY_COMMIT"
require 'path: ${{ github.workspace }}/compozy-source'
require 'fetch-depth: 0'
require 'fetch-tags: true'
require 'make build-go'
require './bin/compozy version -o json'
require "expected_commit = \"$COMPOZY_COMMIT\""
require 'data.get("Commit") != expected_commit'
require 'echo "${{ github.workspace }}/compozy-source/bin" >> "$GITHUB_PATH"'
require 'bash -n scripts/*.sh tests/contract/*.sh'
require "python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v"
require '[[ ! -e .compozy && ! -L .compozy ]]'
require 'COMPOZY_HOME: ${{ runner.temp }}/batuta-compozy-home'
require 'compozy daemon start'
require 'tests/contract/run.sh'
require 'git diff --exit-code'
require 'if: failure()'
require 'path: ${{ runner.temp }}/batuta-compozy-home/logs'
require 'if: always()'
require 'compozy daemon stop || true'
require 'rm -rf -- "$COMPOZY_HOME"'

printf 'OK: reusable CI workflow contract is present\n'
