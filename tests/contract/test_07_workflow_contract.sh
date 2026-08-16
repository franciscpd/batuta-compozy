#!/usr/bin/env bash
# Verifica o gate reutilizável de candidatos sem precisar chamar o GitHub.
set -euo pipefail
cd "$(dirname "$0")/../.."

WORKFLOW=.github/workflows/ci.yml
RELEASE_WORKFLOW=.github/workflows/release.yml
RELEASE_NOTES=docs/releases/0.1.0-beta.2.md
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

require_release() {
  local value=$1
  if ! grep -qF -- "$value" "$RELEASE_WORKFLOW"; then
    printf 'missing release workflow contract: %s\n' "$value" >&2
    exit 1
  fi
}

require_release_block() {
  local block=$1
  if ! grep -qF -- "$block" "$RELEASE_WORKFLOW"; then
    printf 'missing release workflow block:\n%s\n' "$block" >&2
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
    printf 'release ordering violation: %s must precede %s\n' \
      "$earlier" "$later" >&2
    exit 1
  fi
}

workflow_step_block() {
  local workflow=$1
  local name=$2
  awk -v marker="      - name: $name" '
    $0 == marker {
      found = 1
    }
    found && $0 != marker && /^      - name: / {
      exit
    }
    found {
      print
    }
    END {
      if (!found) {
        exit 1
      }
    }
  ' "$workflow"
}

require_step_sequence() {
  local label=$1
  local block=$2
  shift 2
  local expected line_number previous_line=0
  for expected in "$@"; do
    line_number=$(grep -nxF -- "$expected" <<<"$block" | cut -d: -f1 || true)
    if [[ -z $line_number || $line_number == *$'\n'* ||
          ( $previous_line -ne 0 && $line_number -ne $((previous_line + 1)) ) ]]; then
      printf 'missing executable %s sequence line: %s\n' "$label" "$expected" >&2
      exit 1
    fi
    previous_line=$line_number
  done
}

release_step_block() {
  local name=$1
  workflow_step_block "$RELEASE_WORKFLOW" "$name"
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
[[ $(grep -cF -- 'persist-credentials: false' "$WORKFLOW") -eq 2 ]]
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
cleanup_block=$(workflow_step_block "$WORKFLOW" 'Stop isolated CompozyOS daemon')
require_step_sequence 'CI daemon cleanup' "$cleanup_block" \
  '          stop_status=0' \
  '          compozy daemon stop || stop_status=$?' \
  '          [[ $COMPOZY_HOME == "$RUNNER_TEMP"/batuta-compozy-home ]]' \
  '          rm -rf -- "$COMPOZY_HOME"' \
  '          exit "$stop_status"'
if grep -qF -- 'compozy daemon stop || true' <<<"$cleanup_block"; then
  printf 'CI daemon cleanup suppresses stop failure\n' >&2
  exit 1
fi

source_identity_line=$(grep -nF -- '- name: Verify pinned CompozyOS source' "$WORKFLOW" | head -n 1 | cut -d: -f1)
build_line=$(grep -nF -- '- name: Build CompozyOS' "$WORKFLOW" | head -n 1 | cut -d: -f1)
runtime_identity_line=$(grep -nF -- '- name: Verify CompozyOS build identity' "$WORKFLOW" | head -n 1 | cut -d: -f1)
if [[ -z $source_identity_line || -z $build_line || -z $runtime_identity_line ||
      $source_identity_line -ge $build_line || $build_line -ge $runtime_identity_line ]]; then
  printf 'CI identity ordering must be full source check, build, then runtime check\n' >&2
  exit 1
fi

source_identity_block=$(workflow_step_block "$WORKFLOW" 'Verify pinned CompozyOS source')
require_step_sequence 'pinned source identity' "$source_identity_block" \
  '          set -euo pipefail' \
  '          expected_source_commit="a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c"' \
  '          actual_source_commit=$(git rev-parse HEAD)' \
  '          [[ $actual_source_commit == "$expected_source_commit" ]]'

runtime_identity_block=$(workflow_step_block "$WORKFLOW" 'Verify CompozyOS build identity')
require_step_sequence 'runtime identity setup' "$runtime_identity_block" \
  '          set -euo pipefail' \
  '          expected_embedded_commit=$(git rev-parse --short HEAD)' \
  '          version_json=$(./bin/compozy version -o json)' \
  '          python3 - "$version_json" "$expected_embedded_commit" <<'\''PY'\'''
require_step_sequence 'runtime identity expectation' "$runtime_identity_block" \
  '          expected_commit = sys.argv[2]'
require_step_sequence 'runtime exact comparison' "$runtime_identity_block" \
  '          if data.get("Commit") != expected_commit:' \
  '              raise SystemExit(' \
  '                  "built CompozyOS commit differs from its canonical source abbreviation"' \
  '              )'
if grep -qE -- 'starts?with|ends?with|[[:space:]](not[[:space:]]+)?in[[:space:]]|\[[^]]*:[^]]*\]|==.*\*|!=.*\*' \
  <<<"$runtime_identity_block"; then
  printf 'CI runtime identity permits prefix or substring matching\n' >&2
  exit 1
fi

[[ -f $RELEASE_WORKFLOW ]] || {
  printf 'missing release workflow: %s\n' "$RELEASE_WORKFLOW" >&2
  exit 1
}
[[ ! -e .github/workflows/preview-release.yml ]] || {
  printf 'superseded preview release workflow still present\n' >&2
  exit 1
}
[[ ! -e scripts/build-preview-assets.sh ]] || {
  printf 'superseded preview asset builder still present\n' >&2
  exit 1
}
[[ -f $RELEASE_NOTES && ! -L $RELEASE_NOTES ]] || {
  printf 'missing regular release notes: %s\n' "$RELEASE_NOTES" >&2
  exit 1
}

require_release_block $'on:\n  workflow_dispatch:\n    inputs:\n      release_ref:\n        description: Full commit SHA to release\n        required: true\n        type: string\n      release_version:\n        description: Unprefixed beta SemVer\n        required: true\n        type: string'
require_release_block $'permissions:\n  contents: read'
require_release_block $'concurrency:\n  group: release-${{ github.repository }}\n  cancel-in-progress: false'
require_release_block $'  verify:\n    uses: ./.github/workflows/ci.yml\n    with:\n      checkout_ref: ${{ inputs.release_ref }}'
require_release_block $'  publish:\n    needs: verify\n    runs-on: ubuntu-latest\n    timeout-minutes: 45\n    permissions:\n      contents: write'
release_triggers=$(awk '
  /^on:$/ { in_triggers = 1; next }
  in_triggers && /^[^[:space:]]/ { exit }
  in_triggers && /^  [^[:space:]][^:]*:/ {
    trigger = $0; sub(/^  /, "", trigger); sub(/:.*/, "", trigger); print trigger
  }
' "$RELEASE_WORKFLOW")
if [[ $release_triggers != workflow_dispatch ]]; then
  printf 'release workflow trigger set is not exactly workflow_dispatch: %s\n' \
    "$release_triggers" >&2
  exit 1
fi
[[ $(grep -cF -- 'contents: write' "$RELEASE_WORKFLOW") -eq 1 ]]
for forbidden in 'id-token: write' 'attestations: write' 'attest-build-provenance' \
  'SHA256SUMS' 'build-preview-assets.sh' 'graphql' '--prerelease' '--draft' \
  '--latest=false' 'gh release create' 'gh release download'; do
  if grep -qF -- "$forbidden" "$RELEASE_WORKFLOW"; then
    printf 'release workflow retains superseded behavior: %s\n' "$forbidden" >&2
    exit 1
  fi
done

require_release "uses: actions/checkout@$CHECKOUT_SHA"
[[ $(grep -cF -- "uses: actions/checkout@$CHECKOUT_SHA" "$RELEASE_WORKFLOW") -eq 2 ]]
require_release "uses: actions/setup-go@$SETUP_GO_SHA"
require_release 'go-version: 1.26.4'
require_release 'repository: compozy/compozy'
require_release "ref: $COMPOZY_COMMIT"
require_release 'make build-go'
require_release './bin/compozy version -o json'
require_release 'echo "${{ github.workspace }}/compozy-source/bin" >> "$GITHUB_PATH"'
require_release 'ref: ${{ inputs.release_ref }}'
require_release 'fetch-depth: 0'
require_release 'fetch-tags: true'
[[ $(grep -cF -- 'persist-credentials: false' "$RELEASE_WORKFLOW") -eq 2 ]]
require_release 'RELEASE_REF: ${{ inputs.release_ref }}'
require_release 'RELEASE_VERSION: ${{ inputs.release_version }}'

if grep -qE '^ {0,8}(GH_TOKEN|GITHUB_TOKEN):' "$RELEASE_WORKFLOW"; then
  printf 'release token is exposed above step scope\n' >&2
  exit 1
fi
for token_step in \
  'Check remote release preconditions' \
  'Create and push annotated release tag' \
  'Attach release notes' \
  'Verify published release'; do
  token_step_block=$(release_step_block "$token_step")
  if ! grep -qF -- 'GH_TOKEN: ${{ github.token }}' <<<"$token_step_block"; then
    printf 'release token missing from required step: %s\n' "$token_step" >&2
    exit 1
  fi
done
publish_step_block=$(release_step_block 'Publish extension release')
if ! grep -qF -- 'GITHUB_TOKEN: ${{ github.token }}' <<<"$publish_step_block"; then
  printf 'compozy publish step lacks GITHUB_TOKEN\n' >&2
  exit 1
fi
for tokenless_step in 'Stage extension package' 'Verify GitHub installation'; do
  tokenless_block=$(release_step_block "$tokenless_step")
  if grep -qE -- 'GH_TOKEN:|GITHUB_TOKEN:' <<<"$tokenless_block"; then
    printf 'release step receives an unneeded token: %s\n' "$tokenless_step" >&2
    exit 1
  fi
done

require_release '[[ $(git rev-parse HEAD) == "$RELEASE_REF" ]]'
require_release '[[ $RELEASE_REF =~ ^[0-9a-f]{40}$ ]]'
require_release '[[ $GITHUB_SHA == "$RELEASE_REF" ]]'
require_release '[[ $RELEASE_VERSION =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-beta\.(0|[1-9][0-9]*)$ ]]'
require_release 'git fetch origin main --tags'
require_release 'git merge-base --is-ancestor "$RELEASE_REF" origin/main'
require_release 'tomllib.load(manifest_file)["extension"]["version"]'
require_release '[[ $manifest_version == "$RELEASE_VERSION" ]]'
require_release 'notes_file="docs/releases/${RELEASE_VERSION}.md"'
require_release '[[ -f $notes_file && ! -L $notes_file ]]'
require_release '[[ ! -e .compozy && ! -L .compozy ]]'
require_release '[[ -z $(git ls-remote --tags origin "refs/tags/${tag}") ]]'
require_release '! gh release view "$tag" >/dev/null 2>&1'

require_release 'git tag -a "v${RELEASE_VERSION}" -m "Release v${RELEASE_VERSION}" "$RELEASE_REF"'
require_release '[[ $(git cat-file -t "$tag") == tag ]]'
require_release 'GIT_CONFIG_COUNT=1'
require_release 'GIT_CONFIG_KEY_0=http.https://github.com/.extraheader'
require_release 'GIT_CONFIG_VALUE_0="AUTHORIZATION: basic $auth"'
require_release 'git push origin "refs/tags/${tag}:refs/tags/${tag}"'

require_release 'package_dir="${RUNNER_TEMP}/package"'
require_release 'mkdir "$package_dir"'
require_release '[[ -d $package_dir && ! -L $package_dir ]]'
require_release '[[ -z $(find "$package_dir" -mindepth 1 -print -quit) ]]'
require_release 'scripts/stage-extension.sh "$package_dir"'
require_release 'compozy extension publish "$package_dir" --repository "$GITHUB_REPOSITORY" --tag "$tag" -o json'
require_release 'gh release edit "$tag" --title "Batuta ${RELEASE_VERSION}" --notes-file "$notes_file"'

require_release 'peeled=$(git ls-remote --tags origin "refs/tags/${tag}^{}" | cut -f1)'
require_release '[[ $peeled == "$RELEASE_REF" ]]'
require_release 'gh release view "$tag" --json isDraft,isPrerelease,tagName,assets'
require_release '(.isDraft == false) and'
require_release '(.isPrerelease == false) and'
require_release '(.tagName == $tag) and'
require_release '([.assets[].name] | sort) == ([$archive, ($archive + ".sha256")] | sort)'

require_release 'COMPOZY_HOME: ${{ runner.temp }}/batuta-compozy-home'
require_release 'compozy daemon start'
require_release 'for source in "github:${GITHUB_REPOSITORY}@${tag}" "github:${GITHUB_REPOSITORY}"; do'
require_release 'compozy extension install "$source" --allow-unverified --yes -o json'
require_release 'compozy extension enable batuta -o json >/dev/null'
require_release 'compozy extension inventory batuta -o json'
require_release 'compozy extension provenance batuta -o json'
require_release 'compozy extension list -o json'
require_release 'compozy extension remove batuta --global -o json'
require_release "uses: actions/upload-artifact@$UPLOAD_SHA"
require_release 'if: failure()'
require_release "expected={('agent','batuta'), ('loop','batuta-deliver'), ('skill','batuta-routing')}"
require_release 'assert provenance["installed_from"] == "github"'
require_release 'assert provenance["digest_matched"] is True'
require_release 'assert installed["version"] == expected_version'
require_release 'if: always()'
cleanup_block=$(release_step_block 'Stop isolated CompozyOS daemon')
require_step_sequence 'release daemon cleanup' "$cleanup_block" \
  '          stop_status=0' \
  '          compozy daemon stop || stop_status=$?' \
  '          [[ $COMPOZY_HOME == "$RUNNER_TEMP"/batuta-compozy-home ]]' \
  '          rm -rf -- "$COMPOZY_HOME"' \
  '          exit "$stop_status"'

require_release_order '! gh release view "$tag" >/dev/null 2>&1' 'git tag -a "v${RELEASE_VERSION}" -m "Release v${RELEASE_VERSION}" "$RELEASE_REF"'
require_release_order 'git push origin "refs/tags/${tag}:refs/tags/${tag}"' 'scripts/stage-extension.sh "$package_dir"'
require_release_order 'scripts/stage-extension.sh "$package_dir"' 'compozy extension publish "$package_dir" --repository "$GITHUB_REPOSITORY" --tag "$tag" -o json'
require_release_order 'compozy extension publish "$package_dir" --repository "$GITHUB_REPOSITORY" --tag "$tag" -o json' 'gh release edit "$tag" --title "Batuta ${RELEASE_VERSION}" --notes-file "$notes_file"'
require_release_order 'gh release edit "$tag" --title "Batuta ${RELEASE_VERSION}" --notes-file "$notes_file"' 'peeled=$(git ls-remote --tags origin "refs/tags/${tag}^{}" | cut -f1)'
require_release_order 'gh release view "$tag" --json isDraft,isPrerelease,tagName,assets' 'compozy daemon start'
require_release_order 'compozy daemon start' 'for source in "github:${GITHUB_REPOSITORY}@${tag}" "github:${GITHUB_REPOSITORY}"; do'
require_release_order 'compozy extension install "$source" --allow-unverified --yes -o json' 'compozy extension enable batuta -o json >/dev/null'
require_release_order 'compozy extension enable batuta -o json >/dev/null' 'compozy extension inventory batuta -o json'

if grep -qE -- "--clobber|git push .*--force|git push .*--delete|gh release delete|git tag -d|git push origin ['\"]?:refs/tags/|git config .*extraheader|git remote set-url .*token" "$RELEASE_WORKFLOW"; then
  printf 'release workflow contains destructive recovery behavior\n' >&2
  exit 1
fi

printf 'OK: reusable CI and release workflow contracts are present\n'
