#!/usr/bin/env bash
# Verifica o gate reutilizável de candidatos sem precisar chamar o GitHub.
set -euo pipefail
cd "$(dirname "$0")/../.."

WORKFLOW=.github/workflows/ci.yml
RELEASE_WORKFLOW=.github/workflows/preview-release.yml
CHECKOUT_SHA=3d3c42e5aac5ba805825da76410c181273ba90b1
SETUP_GO_SHA=924ae3a1cded613372ab5595356fb5720e22ba16
UPLOAD_SHA=043fb46d1a93c77aae656e7c1c64a875d1fc6a0a
ATTEST_SHA=8beda2b7ed98355c0e97c0a63bec38ae472e66c4
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

require_release() {
  local value=$1
  if ! grep -qF -- "$value" "$RELEASE_WORKFLOW"; then
    printf 'missing preview release workflow contract: %s\n' "$value" >&2
    exit 1
  fi
}

require_release_block() {
  local block=$1
  if ! grep -qF -- "$block" "$RELEASE_WORKFLOW"; then
    printf 'missing preview release workflow block:\n%s\n' "$block" >&2
    exit 1
  fi
}

require_release_order() {
  local earlier=$1
  local later=$2
  local earlier_line later_line
  earlier_line=$(grep -nF -- "$earlier" "$RELEASE_WORKFLOW" | head -n 1 | cut -d: -f1)
  later_line=$(grep -nF -- "$later" "$RELEASE_WORKFLOW" | head -n 1 | cut -d: -f1)
  if [[ -z $earlier_line || -z $later_line || $earlier_line -ge $later_line ]]; then
    printf 'preview release ordering violation: %s must precede %s\n' \
      "$earlier" "$later" >&2
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

[[ -f $RELEASE_WORKFLOW ]] || {
  printf 'missing preview release workflow: %s\n' "$RELEASE_WORKFLOW" >&2
  exit 1
}

require_release_block $'on:\n  workflow_dispatch:\n    inputs:\n      release_ref:\n        description: Full commit SHA to release\n        required: true\n        type: string\n      release_version:\n        description: Unprefixed beta SemVer\n        required: true\n        type: string'
require_release_block $'permissions:\n  contents: read'
require_release_block $'concurrency:\n  group: preview-release-${{ github.repository }}\n  cancel-in-progress: false'
require_release_block $'  verify:\n    uses: ./.github/workflows/ci.yml\n    with:\n      checkout_ref: ${{ inputs.release_ref }}'
require_release_block $'  publish:\n    needs: verify\n    runs-on: ubuntu-latest\n    timeout-minutes: 30\n    permissions:\n      contents: write\n      id-token: write\n      attestations: write'
if grep -qE '^  (pull_request|push|schedule|workflow_call):' "$RELEASE_WORKFLOW"; then
  printf 'preview release workflow has a non-manual trigger\n' >&2
  exit 1
fi
[[ $(grep -cF -- 'contents: write' "$RELEASE_WORKFLOW") -eq 1 ]]
[[ $(grep -cF -- 'id-token: write' "$RELEASE_WORKFLOW") -eq 1 ]]
[[ $(grep -cF -- 'attestations: write' "$RELEASE_WORKFLOW") -eq 1 ]]
require_release "uses: actions/checkout@$CHECKOUT_SHA"
require_release 'ref: ${{ inputs.release_ref }}'
require_release 'fetch-depth: 0'
require_release 'fetch-tags: true'
require_release 'RELEASE_REF: ${{ inputs.release_ref }}'
require_release 'RELEASE_VERSION: ${{ inputs.release_version }}'
require_release 'GH_TOKEN: ${{ github.token }}'
require_release '[[ $(git rev-parse HEAD) == "$RELEASE_REF" ]]'
require_release '[[ $RELEASE_REF =~ ^[0-9a-f]{40}$ ]]'
require_release '[[ $GITHUB_SHA == "$RELEASE_REF" ]]'
require_release '[[ $RELEASE_VERSION =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-beta\.(0|[1-9][0-9]*)$ ]]'
require_release 'git fetch origin main --tags'
require_release 'git merge-base --is-ancestor "$RELEASE_REF" origin/main'
require_release 'tomllib.load(manifest_file)["extension"]["version"]'
require_release '[[ $manifest_version == "$RELEASE_VERSION" ]]'
require_release 'a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c or a later release that contains it'
require_release 'query($owner: String!, $name: String!, $tagRef: String!, $tagName: String!) {'
require_release 'ref(qualifiedName: $tagRef) { target { oid } }'
require_release 'release(tagName: $tagName) { isDraft tagName }'
require_release '-f tagRef="refs/tags/$tag"'
require_release '-f tagName="$tag"'
require_release '(.errors == null) and'
require_release '(.data.repository != null) and'
require_release '(.data.repository.ref == null) and'
require_release '(.data.repository.release == null)'
require_release 'asset_dir="${RUNNER_TEMP}/release-assets"'
require_release '[[ ! -e .compozy && ! -L .compozy ]]'
require_release './scripts/build-preview-assets.sh "$RELEASE_VERSION" "$asset_dir"'
require_release '(cd "$asset_dir" && sha256sum --check SHA256SUMS)'
require_release "uses: actions/attest-build-provenance@$ATTEST_SHA"
[[ $(grep -cF -- "uses: actions/attest-build-provenance@$ATTEST_SHA" "$RELEASE_WORKFLOW") -eq 2 ]]
require_release 'subject-path: ${{ runner.temp }}/release-assets/batuta-compozy_${{ inputs.release_version }}.tar.gz'
require_release 'subject-path: ${{ runner.temp }}/release-assets/SHA256SUMS'
require_release 'git tag -a "v${RELEASE_VERSION}" -m "Release v${RELEASE_VERSION}" "$RELEASE_REF"'
require_release '[[ $(git cat-file -t "$tag") == tag ]]'
require_release 'git push origin "refs/tags/${tag}:refs/tags/${tag}"'
require_release 'gh release create "$tag"'
require_release '--verify-tag'
require_release '--draft'
require_release '--prerelease'
require_release '--latest=false'
require_release '--notes-file "$notes_file"'
require_release 'download_dir="${RUNNER_TEMP}/release-download"'
require_release 'gh release download "$tag" --dir "$download_dir"'
require_release $'expected_assets=$(printf \'%s\\n\' \'SHA256SUMS\' "batuta-compozy_${RELEASE_VERSION}.tar.gz")'
require_release '[[ $actual_assets == "$expected_assets" ]]'
require_release '(cd "$download_dir" && sha256sum --check SHA256SUMS)'
require_release 'gh release edit "$tag" --draft=false --prerelease --latest=false'
require_release 'gh release view "$tag" --json isDraft,isPrerelease,tagName,assets'
require_release '(.isDraft == false) and'
require_release '(.isPrerelease == true) and'
require_release '(.tagName == $tag) and'
require_release '([.assets[].name] | sort) == (["SHA256SUMS", $archive] | sort)'
require_release 'release(tagName: $tagName) { isLatest tagName }'
require_release '(.data.repository.release.isLatest == false) and'
require_release '(.data.repository.release.tagName == $tag)'

require_release_order './scripts/build-preview-assets.sh "$RELEASE_VERSION" "$asset_dir"' "uses: actions/attest-build-provenance@$ATTEST_SHA"
require_release_order 'subject-path: ${{ runner.temp }}/release-assets/SHA256SUMS' 'git tag -a "v${RELEASE_VERSION}" -m "Release v${RELEASE_VERSION}" "$RELEASE_REF"'
require_release_order 'git tag -a "v${RELEASE_VERSION}" -m "Release v${RELEASE_VERSION}" "$RELEASE_REF"' 'gh release create "$tag"'
require_release_order 'gh release download "$tag" --dir "$download_dir"' '(cd "$download_dir" && sha256sum --check SHA256SUMS)'
require_release_order '(cd "$download_dir" && sha256sum --check SHA256SUMS)' 'gh release edit "$tag" --draft=false --prerelease --latest=false'
require_release_order 'gh release edit "$tag" --draft=false --prerelease --latest=false' 'gh release view "$tag" --json isDraft,isPrerelease,tagName,assets'

if grep -qE -- "--clobber|git push .*--force|git push .*--delete|gh release delete|git tag -d|git push origin ['\"]?:refs/tags/" "$RELEASE_WORKFLOW"; then
  printf 'preview release workflow contains destructive recovery behavior\n' >&2
  exit 1
fi

printf 'OK: reusable CI and preview release workflow contracts are present\n'
