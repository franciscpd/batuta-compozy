# Batuta Native Install and Public README Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish Batuta with `compozy extension publish` so users install it with one command, and rewrite the public README around what/install/use.

**Architecture:** A new `release.yml` workflow reuses `ci.yml` for verification, pins an annotated tag at `release_ref`, stages the five package files, calls `compozy extension publish`, attaches notes, and proves the result by installing from GitHub (explicit tag and unversioned) in an isolated daemon. README EN/PT-BR shrink to what/install/use; operational and trust detail move to `docs/how-it-works.md` and `docs/verify.md`. Contract tests (`tests/contract/test_07_*.sh`) pin the new workflow and documents.

**Tech Stack:** GitHub Actions, Bash, Python 3 (`tomllib`), CompozyOS CLI (`compozy extension publish|install|inventory|provenance`), `gh` CLI, existing shell contract-test suite.

**Spec:** `docs/superpowers/specs/2026-08-16-batuta-native-install-design.md`

## Global Constraints

- Package inventory stays exactly: `LICENSE`, `extension.toml`, `agents/batuta/AGENT.md`, `resources/skills/batuta-routing/SKILL.md`, `loops/batuta-deliver/loop.yaml`. No change under `agents/`, `loops/`, `resources/`.
- `extension.toml` keeps `version = "0.1.0-beta.2"` and `min_compozy_version = "0.3.0-beta.13"`.
- Pinned CompozyOS source commit in workflows: `a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c`; Go `1.26.4`; action SHAs `actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1`, `actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16`, `actions/upload-artifact@043fb46d1a93c77aae656e7c1c64a875d1fc6a0a`.
- Release assets are named by CompozyOS: `batuta-v<version>.tar.gz` and `batuta-v<version>.tar.gz.sha256`.
- Install command in every public document: `compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes`. Update command: `compozy extension update batuta --allow-unverified --yes`. Remove: `compozy extension remove batuta --global`.
- README EN and PT-BR are mirrors (same sections, same order). Public docs must keep the "independent community project / not official or endorsed" sentence (scanner in `test_07_public_docs.sh`).
- Conventional commits: `^(build|ci|docs|feat|fix|perf|refactor|test): [a-z].+$`.
- Never run `tests/contract/run.sh` from this repository checkout while `.compozy/` exists; individual `test_07_*.sh` scripts are static and safe to run directly.
- Never touch the live GitHub release outside Task 7.

---

### Task 1: Release workflow on `compozy extension publish`

**Files:**
- Create: `.github/workflows/release.yml`
- Delete: `.github/workflows/preview-release.yml`, `scripts/build-preview-assets.sh`, `tests/contract/test_07_preview_assets.sh`
- Modify: `tests/contract/test_07_workflow_contract.sh` (release section, from `[[ -f $RELEASE_WORKFLOW ]]` to the end), `tests/contract/test_07_license.sh:200-226`

**Interfaces:**
- Consumes: `scripts/stage-extension.sh <empty-dir>` (existing; copies the five package files).
- Produces: `.github/workflows/release.yml` with jobs `verify` and `publish`; step names used by the test: `Check out release source`, `Check out pinned CompozyOS`, `Set up Go`, `Build CompozyOS`, `Verify release preconditions`, `Check remote release preconditions`, `Create and push annotated release tag`, `Stage extension package`, `Publish extension release`, `Attach release notes`, `Verify published release`, `Start isolated CompozyOS daemon`, `Verify GitHub installation`, `Stop isolated CompozyOS daemon`.

- [ ] **Step 1: Rewrite the release section of the workflow contract test (RED)**

Replace everything in `tests/contract/test_07_workflow_contract.sh` from the line `[[ -f $RELEASE_WORKFLOW ]] || {` to the end with the block below. Also change the header variables: `RELEASE_WORKFLOW=.github/workflows/release.yml`; delete the `RELEASE_DESIGN`, `PREVIEW_PLAN`, `PUBLICATION_PLAN`, and `ATTEST_SHA` lines. Keep every CI assertion above that line unchanged. Keep the helper functions (`require_release`, `require_release_block`, `require_release_order`, `workflow_step_block`, `require_step_sequence`, `release_step_block`); delete `require_document`.

```bash
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
require_release 'compozy extension inventory batuta -o json'
require_release 'compozy extension provenance batuta -o json'
require_release 'compozy extension list -o json'
require_release 'compozy extension remove batuta --global -o json'
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

if grep -qE -- "--clobber|git push .*--force|git push .*--delete|gh release delete|git tag -d|git push origin ['\"]?:refs/tags/|git remote set-url .*token" "$RELEASE_WORKFLOW"; then
  printf 'release workflow contains destructive recovery behavior\n' >&2
  exit 1
fi

printf 'OK: reusable CI and release workflow contracts are present\n'
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `tests/contract/test_07_workflow_contract.sh`
Expected: FAIL with `missing release workflow: .github/workflows/release.yml`

- [ ] **Step 3: Write `.github/workflows/release.yml`**

```yaml
name: Release

on:
  workflow_dispatch:
    inputs:
      release_ref:
        description: Full commit SHA to release
        required: true
        type: string
      release_version:
        description: Unprefixed beta SemVer
        required: true
        type: string

permissions:
  contents: read

concurrency:
  group: release-${{ github.repository }}
  cancel-in-progress: false

jobs:
  verify:
    uses: ./.github/workflows/ci.yml
    with:
      checkout_ref: ${{ inputs.release_ref }}

  publish:
    needs: verify
    runs-on: ubuntu-latest
    timeout-minutes: 45
    permissions:
      contents: write
    env:
      RELEASE_REF: ${{ inputs.release_ref }}
      RELEASE_VERSION: ${{ inputs.release_version }}
    defaults:
      run:
        shell: bash
        working-directory: ${{ github.workspace }}/candidate
    steps:
      - name: Check out release source
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1
        with:
          ref: ${{ inputs.release_ref }}
          path: ${{ github.workspace }}/candidate
          fetch-depth: 0
          fetch-tags: true
          persist-credentials: false

      - name: Check out pinned CompozyOS
        uses: actions/checkout@3d3c42e5aac5ba805825da76410c181273ba90b1
        with:
          repository: compozy/compozy
          ref: a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c
          path: ${{ github.workspace }}/compozy-source
          fetch-depth: 0
          fetch-tags: true
          persist-credentials: false

      - name: Set up Go
        uses: actions/setup-go@924ae3a1cded613372ab5595356fb5720e22ba16
        with:
          go-version: 1.26.4

      - name: Build CompozyOS
        working-directory: ${{ github.workspace }}/compozy-source
        run: |
          set -euo pipefail
          [[ $(git rev-parse HEAD) == a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c ]]
          make build-go
          echo "${{ github.workspace }}/compozy-source/bin" >> "$GITHUB_PATH"

      - name: Verify release preconditions
        run: |
          set -euo pipefail
          [[ $(git rev-parse HEAD) == "$RELEASE_REF" ]]
          [[ $RELEASE_REF =~ ^[0-9a-f]{40}$ ]]
          [[ $GITHUB_SHA == "$RELEASE_REF" ]]
          [[ $RELEASE_VERSION =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-beta\.(0|[1-9][0-9]*)$ ]]

          git fetch origin main --tags
          git merge-base --is-ancestor "$RELEASE_REF" origin/main

          manifest_version=$(python3 - extension.toml <<'PY'
          import sys
          import tomllib

          with open(sys.argv[1], "rb") as manifest_file:
              print(tomllib.load(manifest_file)["extension"]["version"])
          PY
          )
          [[ $manifest_version == "$RELEASE_VERSION" ]]

          notes_file="docs/releases/${RELEASE_VERSION}.md"
          [[ -f $notes_file && ! -L $notes_file ]]
          [[ ! -e .compozy && ! -L .compozy ]]

      - name: Check remote release preconditions
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          set -euo pipefail
          tag="v${RELEASE_VERSION}"
          [[ -z $(git ls-remote --tags origin "refs/tags/${tag}") ]]
          ! gh release view "$tag" >/dev/null 2>&1

      - name: Create and push annotated release tag
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          set -euo pipefail
          tag="v${RELEASE_VERSION}"
          git config user.name github-actions[bot]
          git config user.email 41898282+github-actions[bot]@users.noreply.github.com
          git tag -a "v${RELEASE_VERSION}" -m "Release v${RELEASE_VERSION}" "$RELEASE_REF"
          [[ $(git cat-file -t "$tag") == tag ]]
          auth=$(printf 'x-access-token:%s' "$GH_TOKEN" | base64 -w0)
          GIT_CONFIG_COUNT=1 \
            GIT_CONFIG_KEY_0=http.https://github.com/.extraheader \
            GIT_CONFIG_VALUE_0="AUTHORIZATION: basic $auth" \
            git push origin "refs/tags/${tag}:refs/tags/${tag}"
          unset auth

      - name: Stage extension package
        run: |
          set -euo pipefail
          package_dir="${RUNNER_TEMP}/package"
          mkdir "$package_dir"
          [[ -d $package_dir && ! -L $package_dir ]]
          [[ -z $(find "$package_dir" -mindepth 1 -print -quit) ]]
          scripts/stage-extension.sh "$package_dir"

      - name: Publish extension release
        env:
          GITHUB_TOKEN: ${{ github.token }}
        run: |
          set -euo pipefail
          tag="v${RELEASE_VERSION}"
          package_dir="${RUNNER_TEMP}/package"
          publish_json=$(compozy extension publish "$package_dir" --repository "$GITHUB_REPOSITORY" --tag "$tag" -o json)
          python3 - "$publish_json" >> "$GITHUB_STEP_SUMMARY" <<'PY'
          import json
          import sys

          data = json.loads(sys.argv[1])
          for key in ("release_url", "asset_url", "digest_sha256"):
              print(f"- {key}: {data[key]}")
          PY

      - name: Attach release notes
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          set -euo pipefail
          tag="v${RELEASE_VERSION}"
          notes_file="docs/releases/${RELEASE_VERSION}.md"
          gh release edit "$tag" --title "Batuta ${RELEASE_VERSION}" --notes-file "$notes_file"

      - name: Verify published release
        env:
          GH_TOKEN: ${{ github.token }}
        run: |
          set -euo pipefail
          tag="v${RELEASE_VERSION}"
          archive="batuta-${tag}.tar.gz"
          peeled=$(git ls-remote --tags origin "refs/tags/${tag}^{}" | cut -f1)
          [[ $peeled == "$RELEASE_REF" ]]
          release_json=$(gh release view "$tag" --json isDraft,isPrerelease,tagName,assets)
          jq -e --arg tag "$tag" --arg archive "$archive" '
            (.isDraft == false) and
            (.isPrerelease == false) and
            (.tagName == $tag) and
            ([.assets[].name] | sort) == ([$archive, ($archive + ".sha256")] | sort)
          ' <<<"$release_json" >/dev/null

      - name: Start isolated CompozyOS daemon
        env:
          COMPOZY_HOME: ${{ runner.temp }}/batuta-compozy-home
        run: |
          [[ ! -e .compozy && ! -L .compozy ]]
          compozy daemon start

      - name: Verify GitHub installation
        env:
          COMPOZY_HOME: ${{ runner.temp }}/batuta-compozy-home
        run: |
          set -euo pipefail
          tag="v${RELEASE_VERSION}"
          for source in "github:${GITHUB_REPOSITORY}@${tag}" "github:${GITHUB_REPOSITORY}"; do
            compozy extension install "$source" --allow-unverified --yes -o json >/dev/null
            inventory_json=$(compozy extension inventory batuta -o json)
            provenance_json=$(compozy extension provenance batuta -o json)
            list_json=$(compozy extension list -o json)
            python3 - "$inventory_json" "$provenance_json" "$list_json" "$RELEASE_VERSION" <<'PY'
          import json
          import sys

          inventory = json.loads(sys.argv[1])["items"]
          provenance = json.loads(sys.argv[2])
          rows = json.loads(sys.argv[3])
          expected_version = sys.argv[4]
          actual = {(item["kind"], item["name"]) for item in inventory}
          expected={('agent','batuta'), ('loop','batuta-deliver'), ('skill','batuta-routing')}
          assert actual == expected, f"unexpected inventory: {sorted(actual)}"
          assert all(item["live"] for item in inventory), f"resources not live: {inventory}"
          assert provenance["installed_from"] == "github"
          assert provenance["digest_matched"] is True
          installed = next(row for row in rows if row["name"] == "batuta")
          assert installed["version"] == expected_version
          PY
            compozy extension remove batuta --global -o json >/dev/null
          done

      - name: Stop isolated CompozyOS daemon
        if: always()
        env:
          COMPOZY_HOME: ${{ runner.temp }}/batuta-compozy-home
        run: |
          stop_status=0
          compozy daemon stop || stop_status=$?
          [[ $COMPOZY_HOME == "$RUNNER_TEMP"/batuta-compozy-home ]]
          rm -rf -- "$COMPOZY_HOME"
          exit "$stop_status"
```

Notes for the implementer: the heredoc bodies inside `run:` keep the same 10-space indentation as `ci.yml`; the `python3 - ... <<'PY'` blocks are indented so the `PY` terminator sits at column 11 like in `ci.yml`. `jq` and `gh` are preinstalled on `ubuntu-latest`.

- [ ] **Step 4: Delete the superseded workflow, script, and test**

```bash
git rm -q .github/workflows/preview-release.yml scripts/build-preview-assets.sh tests/contract/test_07_preview_assets.sh
```

- [ ] **Step 5: Update `test_07_license.sh` to stop building the old archive**

Replace lines from `version=$(python3 - extension.toml <<'PY'` through `assert_inventory "$extracted" preview` (the block that calls `scripts/build-preview-assets.sh`) with nothing — the staged-tree check (`assert_inventory "$stage" staged`) and the content-addressed package check just above already cover the shipped tree. Change the final message to:

```bash
printf 'OK: MIT license is exact and preserved in the staged and content-addressed package trees\n'
```

- [ ] **Step 6: Run the static tests to verify they pass**

Run: `bash -n scripts/*.sh tests/contract/*.sh .github/workflows/*.yml 2>/dev/null; bash -n scripts/*.sh tests/contract/*.sh && tests/contract/test_07_workflow_contract.sh && tests/contract/test_07_license.sh`
Expected: `OK: reusable CI and release workflow contracts are present` and `OK: MIT license is exact and preserved in the staged and content-addressed package trees`. (`test_07_license.sh` needs `python3` and writes only under `/tmp/batuta-license-contract.*`.)

Also validate the YAML parses: `python3 -c "import yaml,sys; yaml.safe_load(open('.github/workflows/release.yml'))"` (if PyYAML is missing, `ruby -ryaml -e 'YAML.load_file(".github/workflows/release.yml")'` or skip).

- [ ] **Step 7: Commit**

```bash
git add .github/workflows/release.yml tests/contract/test_07_workflow_contract.sh tests/contract/test_07_license.sh
git commit -m "ci: publish releases with compozy extension publish"
```

---

### Task 2: `docs/how-it-works.md` and `docs/verify.md`

**Files:**
- Create: `docs/how-it-works.md`, `docs/verify.md`
- Modify: `tests/contract/test_07_public_docs.sh` (add a block after the `docs/architecture.md` loop)

**Interfaces:**
- Produces: two documents linked from README (Task 3), release notes and CONTRIBUTING (Task 4).

- [ ] **Step 1: Add assertions for the new documents (RED)**

Insert into `tests/contract/test_07_public_docs.sh`, right after the `for text in ... require_text docs/architecture.md "$text"; done` loop:

```bash
require_file docs/how-it-works.md
require_file docs/verify.md

for text in \
  'loops.inputs.batuta-deliver.auto_commit' \
  'config_path_not_found' \
  'compozy__provider_models_list' \
  'compozy__loop_configure' \
  'cy-create-spec' \
  'cy-create-tasks' \
  'ext__spec_cycle__import_tasks' \
  'batuta-deliver' \
  'origin_session_id' \
  'compozy__loop_status' \
  'agents/batuta/AGENT.md' \
  'resources/skills/batuta-routing/SKILL.md'; do
  require_text docs/how-it-works.md "$text"
done

for text in \
  '--allow-unverified' \
  'unverified' \
  'digest_matched' \
  'compozy extension provenance batuta' \
  'batuta-v0.1.0-beta.2.tar.gz' \
  'batuta-v0.1.0-beta.2.tar.gz.sha256' \
  'sha256sum --check' \
  'compozy extension validate' \
  'compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes' \
  'scripts/republish.sh'; do
  require_text docs/verify.md "$text"
done
```

- [ ] **Step 2: Run the test to verify it fails**

Run: `tests/contract/test_07_public_docs.sh`
Expected: FAIL with `missing regular public documentation file: docs/how-it-works.md`

- [ ] **Step 3: Write `docs/how-it-works.md`**

```markdown
# How Batuta works

This is the operational contract of the `batuta` agent. The source of truth
is [`agents/batuta/AGENT.md`](../agents/batuta/AGENT.md) and
[`resources/skills/batuta-routing/SKILL.md`](../resources/skills/batuta-routing/SKILL.md);
this page explains the same rules in reading order.

## 1. Delivery preference gate

The first tool call of every new session is `compozy__config_get` for exactly
`loops.inputs.batuta-deliver.auto_commit` in the current workspace. Both
`true` and `false` open the gate. On `config_path_not_found`, Batuta asks
whether automatic commits should be enabled, persists your boolean answer at
workspace scope, rereads it, and only then continues. Any other config error
stops the session with the exact structured error. Global defaults, child
Loop defaults, the `batuta-deliver` definition default, and dry-runs never
substitute for the stored preference. Batuta rereads the key before every
dispatch.

## 2. Bootstrap and routing

After the gate opens, Batuta reads the stored `implement-tasks` runtime rules.
When absent, it derives them: the `batuta-routing` skill gives lane semantics
(`low`, `medium`, `high`, `critical`) and the JSON shape; the live catalog
from `compozy__provider_models_list` (with costs) is the only source of
provider and model IDs. Batuta shows the derived table with costs, waits for
your confirmation, then stores it with `compozy__loop_configure`
(`name: implement-tasks`, field `runtime_rules`). Reconfiguration later is a
conversation request that re-applies the override. Routing is auditable per
generation in `resolved_runtime`.

Provider authentication is yours to do once, outside the extension.

## 3. Requirements and tasks (spec-cycle)

Batuta runs `cy-create-spec` for every delivery. A simple request may use a
short grill but never skips the spec. You approve `_spec.md`,
`_user_stories.md`, `_dx.md`, and `_tests.md`; `_uiux.md` only when a Web
surface changes. Then `cy-create-tasks` writes `_tasks.md` plus `task_NN.md`
under `.compozy/tasks/<slug>/`, each with `type` and `complexity`
frontmatter — the fields routing matches on. Executable requirements
(package names and versions, commands, paths, flags) stay literal from the
conversation to the execution prompts.

## 4. Preflight, dry-run, dispatch

Before dispatch Batuta calls the read-only `ext__spec_cycle__import_tasks`
with `pattern=.compozy/tasks/<slug>/task_*.md` and continues only when it
returns `count > 0`. A Loop dry-run plans nodes but does not execute the
import, so it cannot prove tasks exist. Then it dry-runs `batuta-deliver` with
`slug`, `origin_session_id` (its own session), and `auto_commit` (the
verified workspace boolean), shows the plan, and submits the real run with
the same inputs. `batuta-deliver` chains the bundled `implement-tasks` and
`review-and-fix` Loops inside the daemon; Batuta never dispatches them
separately and never sends per-run runtime rules.

A successful dispatch ends the turn: Batuta reports `run_id` (and `web_url`
when available) and stops.

## 5. Event-driven return

Every terminal effect of `batuta-deliver` (`done`, `no-op`, `blocked`,
`failed`, `canceled`, `exhausted`, `stalled`) queues one idempotent prompt
back to the `origin_session_id`. In that turn Batuta's first operational call
is `compozy__loop_status` for the exact delivery run; it then reports the
literal parent and child outcomes, commits, and blockers. There is no
watcher, poller, or reporting agent. An explicit progress request takes one
`compozy__loop_status` snapshot and ends the turn.

## 6. Escalation

Retry, quarantine, and failure classes belong to the daemon. A task that
fails repeatedly in its lane gets a surgical `id` rule one lane up in the
stored `implement-tasks` override, a redispatch, and the rule removed after
it lands. `needs-approval` gates are surfaced to you with run and gate IDs —
Batuta cannot approve runs it started. Ambiguity mid-run comes back to the
conversation.

## What Batuta never does

Write, edit, or commit code; fork or mutate the bundled Loop definitions;
push to any remote; approve its own runs; report a terminal state other than
the daemon's exact one.
```

- [ ] **Step 4: Write `docs/verify.md`**

```markdown
# Verifying and installing Batuta

## What `--allow-unverified` means

CompozyOS installs extensions from a curated catalog, from GitHub, from a git
URL, or from a local path. Anything that is not an official or community
catalog entry — including a direct GitHub install like Batuta — is in the
`unverified` registry tier and needs two things: the live policy
`extensions.trust.allow_unverified` (default `true`) and your explicit
`--allow-unverified` on the command (`--yes` skips the confirmation prompt).
That flag is the whole consent; it does not disable integrity checks.

Every Batuta release carries a `.sha256` sidecar next to its archive. When
the daemon finds it, it verifies the archive against that digest before
extraction and records `digest_matched: true` in the extension's provenance.
Inspect the decision at any time:

```bash
compozy extension provenance batuta -o json
```

Expected for a GitHub install: `installed_from: "github"`,
`digest_matched: true`, `registry_tier: "unverified"`.

## One-command install (recommended)

```bash
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes
```

Pin a version with `github:franciscpd/batuta-compozy@v0.1.0-beta.2`.

## Manual path

If you prefer to inspect the archive before the daemon does:

```bash
version=0.1.0-beta.2
work=$(mktemp -d)
gh release download "v$version" --repo franciscpd/batuta-compozy --dir "$work"
(cd "$work" && sha256sum --check "batuta-v$version.tar.gz.sha256")
extracted=$(mktemp -d)
tar -xzf "$work/batuta-v$version.tar.gz" -C "$extracted"
compozy extension validate "$extracted" -o json
compozy extension install "$extracted" --allow-unverified --yes
```

The release assets are exactly `batuta-v0.1.0-beta.2.tar.gz` and
`batuta-v0.1.0-beta.2.tar.gz.sha256`. The archive contains five files:
`LICENSE`, `extension.toml`, `agents/batuta/AGENT.md`,
`resources/skills/batuta-routing/SKILL.md`, `loops/batuta-deliver/loop.yaml`.

## Update and remove

```bash
compozy extension update batuta --allow-unverified --yes
compozy extension remove batuta --global
```

## Local development install

From a checkout, `scripts/republish.sh` validates compatibility, stages the
five package files into a content-addressed, read-only package under
`~/.local/share/batuta-compozy/packages` (override with `BATUTA_PACKAGE_ROOT`),
and installs, enables, and checks the live inventory under a per-user lock at
`~/.compozy/locks/batuta-republish.lock`. See `CONTRIBUTING.md`.
```

- [ ] **Step 5: Run the test to verify it passes**

Run: `tests/contract/test_07_public_docs.sh`
Expected: PASS — the new block passes and the README assertions in this file are still the old ones, which the current README satisfies (`OK: public documentation exposes guides, contribution workflow, and independent status`).

- [ ] **Step 6: Commit**

```bash
git add docs/how-it-works.md docs/verify.md tests/contract/test_07_public_docs.sh
git commit -m "docs: add how-it-works and verify guides"
```

---

### Task 3: README EN and PT-BR rewrite

**Files:**
- Modify: `README.md`, `README.pt-BR.md` (full rewrite)
- Modify: `tests/contract/test_07_preview_docs.sh` (rewrite the README/release-notes section; keep the superseded-document and aggregate-plan blocks verbatim), `tests/contract/test_07_public_docs.sh` (README text lists)

**Interfaces:**
- Consumes: `docs/how-it-works.md`, `docs/verify.md` (Task 2).
- Produces: README text the release notes (Task 4) link to.

- [ ] **Step 1: Rewrite the README section of `test_07_preview_docs.sh` (RED)**

Replace the file's content from `for document in "${documents[@]}"; do` through the `require_wrapped README.pt-BR.md 'permanecem active/idle após a conclusão terminal normal'` line with:

```bash
install_command='compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes'
update_command='compozy extension update batuta --allow-unverified --yes'

for document in "${documents[@]}"; do
  [[ -f $document && ! -L $document ]]
  require "$document" 'franciscpd/batuta-compozy'
  require "$document" 'v0.1.0-beta.2'
  require "$document" "$install_command"
  require "$document" "$update_command"
  require "$document" 'compozy extension remove batuta --global'
  require "$document" 'docs/verify.md'
  for obsolete in \
    'SHA256SUMS' \
    'gh release download' \
    'batuta-compozy_0.1.0-beta.2.tar.gz' \
    'a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c' \
    'check-compozy-version.sh' \
    'batuta-republish.lock'; do
    if grep -qF -- "$obsolete" "$document"; then
      printf 'obsolete install text in %s: %s\n' "$document" "$obsolete" >&2
      exit 1
    fi
  done
done

for readme in README.md README.pt-BR.md; do
  require "$readme" 'docs/how-it-works.md'
  require "$readme" 'compozy provider models list'
  require "$readme" 'v0.3.0-beta.14'
  first_code_block=$(awk '/^```bash$/{n=1; next} n==1{print; exit}' "$readme")
  if [[ $first_code_block != "$install_command" ]]; then
    printf 'first bash code block in %s is not the install command: %s\n' \
      "$readme" "$first_code_block" >&2
    exit 1
  fi
  usage_line=$(grep -nE '^## (Use|Uso)$' "$readme" | head -n 1 | cut -d: -f1)
  if [[ -z $usage_line || $usage_line -gt 60 ]]; then
    printf 'usage section in %s starts after line 60 (line %s)\n' \
      "$readme" "${usage_line:-none}" >&2
    exit 1
  fi
done

require_wrapped README.md 'independent community project'
require_wrapped README.pt-BR.md 'projeto independente da comunidade'
require_wrapped "$release_notes" 'republished'

for document in README.md "$release_notes"; do
  require_wrapped "$document" 'executor sessions are not visually nested'
  require_wrapped "$document" 'remain active/idle after normal terminal completion'
done
require_wrapped README.pt-BR.md 'sessões dos executores não são visualmente aninhadas'
require_wrapped README.pt-BR.md 'permanecem active/idle após a conclusão terminal normal'
```

Change the final `printf` to `printf 'OK: public documentation carries the one-command install, update, removal, and upstream limitations\n'`.

In `tests/contract/test_07_public_docs.sh`, in the README.md list replace `'stages the manifest, LICENSE, and declared resources'` with `'docs/how-it-works.md'` and `'docs/verify.md'`; in the README.pt-BR.md list replace `'monta o manifesto, a licença e os recursos declarados'` with the same two paths.

- [ ] **Step 2: Run the tests to verify they fail**

Run: `tests/contract/test_07_preview_docs.sh; tests/contract/test_07_public_docs.sh`
Expected: both FAIL (first on `missing preview documentation text in README.md: compozy extension install github:...`).

- [ ] **Step 3: Write `README.md`**

```markdown
# batuta-compozy

> 🇧🇷 [Versão em português](README.pt-BR.md)

Batuta is a conductor agent for [CompozyOS](https://www.compozy.com/docs/).
You describe a change in conversation; Batuta turns it into a spec and tasks
(via the bundled `spec-cycle`), routes each task to the cheapest capable
model, dispatches one durable delivery Loop, and reports the exact outcome
back in the same conversation. It never writes code itself.

Batuta is an independent community project, not an official or endorsed
CompozyOS component. CompozyOS itself lives at
[github.com/compozy/compozy](https://github.com/compozy/compozy).

```text
you  ─▶ batuta session ─▶ cy-create-spec ─▶ cy-create-tasks
                                                  │
                                                  ▼
             terminal report ◀── batuta-deliver ──▶ implement-tasks ─▶ review-and-fix
```

## Install

Prerequisites:

- CompozyOS `v0.3.0-beta.14` or later with the daemon running (verified on
  `v0.3.0-beta.16`).
- The bundled `spec-cycle` extension enabled (`compozy extension list`).
- At least one provider authenticated: `compozy provider models list` shows
  models.

```bash
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes
```

`--allow-unverified` is CompozyOS's consent for community (non-catalog)
sources; the daemon still verifies the release archive against its
`.sha256` sidecar. Details, the manual path, and provenance checks are in
[docs/verify.md](docs/verify.md).

Update: `compozy extension update batuta --allow-unverified --yes` ·
Remove: `compozy extension remove batuta --global`

Current release: `v0.1.0-beta.2` —
[release notes](docs/releases/0.1.0-beta.2.md).

## Use

Create a session with the `batuta` agent in your project's workspace and
describe what you want. A first session looks like this:

```text
you     Add a --version subcommand that prints literally "todo 1.0.0".
batuta  Should I enable automatic commits for deliveries in this workspace? (yes/no)
you     no
batuta  Routing derived from your provider catalog: low → …, medium → …,
        high → …, critical → … (costs shown). Store it?
you     yes
batuta  [runs cy-create-spec] Please review _spec.md, _user_stories.md, _dx.md, _tests.md.
you     approved
batuta  [runs cy-create-tasks] 1 task, complexity low. Preflight OK, dry-run OK.
        Dispatched batuta-deliver run <id>. I'll report here when it finishes.
batuta  Delivery <id> reached done: implement-tasks done, review-and-fix done,
        9/9 tests passing, no commit (auto_commit=false).
```

Routing comes from your live provider catalog and is stored per workspace;
ask Batuta in conversation to change it. The full contract — gate, bootstrap,
preflight, dry-run, event-driven return, escalation — is in
[docs/how-it-works.md](docs/how-it-works.md).

## Known limitations

Two upstream CompozyOS limitations remain: executor sessions are not
visually nested, and they remain active/idle after normal terminal
completion.

## Learn more

- [How it works](docs/how-it-works.md) · [Verify and install](docs/verify.md)
- [Architecture](docs/architecture.md) ·
  [Case study: version-subcommand](docs/case-studies/version-subcommand.md)
- [Contributing](CONTRIBUTING.md) · [MIT license](LICENSE)
```

- [ ] **Step 4: Write `README.pt-BR.md`**

```markdown
# batuta-compozy

> 🇺🇸 [English version](README.md)

O Batuta é um agente maestro para o [CompozyOS](https://www.compozy.com/docs/).
Você descreve uma mudança em conversa; o Batuta a transforma em spec e tasks
(via o `spec-cycle` bundled), roteia cada task para o modelo mais barato
capaz de resolvê-la, despacha um único Loop de entrega durável e reporta o
resultado exato na mesma conversa. Ele nunca escreve código.

O Batuta é um projeto independente da comunidade, não um componente oficial
ou endossado do CompozyOS. O CompozyOS vive em
[github.com/compozy/compozy](https://github.com/compozy/compozy).

```text
você ─▶ sessão batuta ─▶ cy-create-spec ─▶ cy-create-tasks
                                                  │
                                                  ▼
             relatório terminal ◀── batuta-deliver ──▶ implement-tasks ─▶ review-and-fix
```

## Instalar

Pré-requisitos:

- CompozyOS `v0.3.0-beta.14` ou posterior com o daemon rodando (verificado em
  `v0.3.0-beta.16`).
- Extensão bundled `spec-cycle` habilitada (`compozy extension list`).
- Ao menos um provider autenticado: `compozy provider models list` lista
  modelos.

```bash
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes
```

`--allow-unverified` é o consentimento do CompozyOS para fontes da comunidade
(fora do catálogo); o daemon ainda verifica o archive da release contra o
sidecar `.sha256`. Detalhes, o caminho manual e a checagem de proveniência
estão em [docs/verify.md](docs/verify.md).

Atualizar: `compozy extension update batuta --allow-unverified --yes` ·
Remover: `compozy extension remove batuta --global`

Release atual: `v0.1.0-beta.2` —
[notas da release](docs/releases/0.1.0-beta.2.md).

## Uso

Crie uma sessão com o agente `batuta` no workspace do seu projeto e descreva
o que quer. Uma primeira sessão se parece com isto:

```text
você    Adicione um subcomando --version que imprima literalmente "todo 1.0.0".
batuta  Devo habilitar commits automáticos nas entregas deste workspace? (sim/não)
você    não
batuta  Roteamento derivado do seu catálogo de providers: low → …, medium → …,
        high → …, critical → … (custos exibidos). Armazenar?
você    sim
batuta  [roda cy-create-spec] Revise _spec.md, _user_stories.md, _dx.md, _tests.md.
você    aprovado
batuta  [roda cy-create-tasks] 1 task, complexity low. Preflight OK, dry-run OK.
        Despachei o run <id> de batuta-deliver. Reporto aqui quando terminar.
batuta  Entrega <id> chegou a done: implement-tasks done, review-and-fix done,
        9/9 testes passando, sem commit (auto_commit=false).
```

O roteamento vem do seu catálogo vivo de providers e fica armazenado por
workspace; peça ao Batuta em conversa para mudá-lo. O contrato completo —
gate, bootstrap, preflight, dry-run, retorno orientado a eventos, escalada —
está em [docs/how-it-works.md](docs/how-it-works.md) (em inglês).

## Limitações conhecidas

Duas limitações upstream do CompozyOS permanecem: as sessões dos executores
não são visualmente aninhadas e permanecem active/idle após a conclusão
terminal normal.

## Saiba mais

- [Como funciona](docs/how-it-works.md) · [Verificar e instalar](docs/verify.md)
- [Arquitetura](docs/architecture.md) ·
  [Estudo de caso: version-subcommand](docs/case-studies/version-subcommand.md)
- [Contribuindo](CONTRIBUTING.md) · [Licença MIT](LICENSE)
```

- [ ] **Step 5: Run the tests**

Run: `tests/contract/test_07_preview_docs.sh; tests/contract/test_07_public_docs.sh`
Expected: `test_07_public_docs.sh` passes (`OK: public documentation exposes guides...`). `test_07_preview_docs.sh` still fails, and the failure must name `docs/releases/0.1.0-beta.2.md` (not a README) — release notes are Task 4. If it names a README, fix the README before committing.

- [ ] **Step 6: Commit**

```bash
git add README.md README.pt-BR.md tests/contract/test_07_preview_docs.sh tests/contract/test_07_public_docs.sh
git commit -m "docs: rewrite readme around one-command install"
```

---

### Task 4: Release notes and CONTRIBUTING

**Files:**
- Modify: `docs/releases/0.1.0-beta.2.md` (full rewrite), `CONTRIBUTING.md`
- Modify: `tests/contract/test_07_public_docs.sh` (CONTRIBUTING list), `tests/contract/test_07_case_study.sh` (no change expected; run it)

- [ ] **Step 1: Extend the CONTRIBUTING assertions (RED)**

In `tests/contract/test_07_public_docs.sh`, in the CONTRIBUTING.md `for text in` list, add these entries and remove nothing:

```bash
  'gh workflow run release.yml' \
  'gh release delete' \
  '--cleanup-tag' \
  'release.yml' \
```

Run: `tests/contract/test_07_public_docs.sh`
Expected: FAIL with `missing public documentation text in CONTRIBUTING.md: gh workflow run release.yml`

- [ ] **Step 2: Rewrite `docs/releases/0.1.0-beta.2.md`**

```markdown
# Batuta v0.1.0-beta.2

`v0.1.0-beta.2` is the reviewed preview release of Batuta from
`franciscpd/batuta-compozy`.

## Install

```bash
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes
```

Pin this version with `github:franciscpd/batuta-compozy@v0.1.0-beta.2`.
Update later with `compozy extension update batuta --allow-unverified --yes`;
remove with `compozy extension remove batuta --global`. The release carries
exactly two assets, `batuta-v0.1.0-beta.2.tar.gz` and
`batuta-v0.1.0-beta.2.tar.gz.sha256`; the daemon verifies the archive against
the sidecar. Manual verification: [docs/verify.md](https://github.com/franciscpd/batuta-compozy/blob/v0.1.0-beta.2/docs/verify.md).

This release was republished on 2026-08-16 with `compozy extension publish`
so that the native `github:` install works; the tag now points at the commit
containing that change. The earlier preview assets were removed.

## Verified scope

One `batuta` agent, one `batuta-routing` skill, one `batuta-deliver` Loop.
Batuta is resource-only orchestration: it classifies, decomposes, dispatches,
and reports, and does not write implementation code. Verified on CompozyOS
`v0.3.0-beta.16-9-ga35eda6d`; the manifest floor is `0.3.0-beta.13`, and
`v0.3.0-beta.14` or later is the operational floor.

## Known upstream limitations

These are CompozyOS limitations, unchanged by this preview: executor
sessions are not visually nested and remain active/idle after normal
terminal completion.

## Documentation

[README](https://github.com/franciscpd/batuta-compozy/blob/v0.1.0-beta.2/README.md) ·
[How it works](https://github.com/franciscpd/batuta-compozy/blob/v0.1.0-beta.2/docs/how-it-works.md) ·
[Architecture](https://github.com/franciscpd/batuta-compozy/blob/v0.1.0-beta.2/docs/architecture.md) ·
[Case study](https://github.com/franciscpd/batuta-compozy/blob/v0.1.0-beta.2/docs/case-studies/version-subcommand.md) ·
[LICENSE](https://github.com/franciscpd/batuta-compozy/blob/v0.1.0-beta.2/LICENSE) (MIT).
```

- [ ] **Step 3: Update `CONTRIBUTING.md`**

Replace the paragraph and code block starting `Check preview assets deterministically with:` (through the closing fence) with:

```markdown
## Releasing

Releases are published only by `.github/workflows/release.yml`
(`gh workflow run release.yml -f release_ref=<40-hex SHA on main>
-f release_version=<X.Y.Z-beta.N>`). It reruns CI on that commit, creates the
annotated tag at that commit, publishes with `compozy extension publish`,
attaches `docs/releases/<version>.md` as notes, and proves the result by
installing from GitHub in an isolated daemon. Before dispatching, bump
`extension.toml` and add `docs/releases/<version>.md` on `main`.

If a run fails after the tag step, remote state may be partial. Recovery is
always the same: `gh release delete v<version> --cleanup-tag --yes` (ignore
"release not found"), then `git push origin :refs/tags/v<version>` if the tag
survived, then dispatch again. Never edit a release by hand outside this
procedure.
```

In the "Change and review workflow" section, replace `Do not mutate a release directly outside\n\`.github/workflows/preview-release.yml\`.` with `Do not mutate a release directly outside \`.github/workflows/release.yml\` and the recovery procedure above.` and replace `a deterministic asset digest,` with `the contract results,` in the pull-request sentence.

- [ ] **Step 4: Run all static doc tests**

Run: `tests/contract/test_07_public_docs.sh && tests/contract/test_07_preview_docs.sh && tests/contract/test_07_case_study.sh && tests/contract/test_07_workflow_contract.sh && tests/contract/test_07_license.sh && bash -n scripts/*.sh tests/contract/*.sh && git diff --check`
Expected: five `OK:` lines, no diff-check output.

- [ ] **Step 5: Commit**

```bash
git add docs/releases/0.1.0-beta.2.md CONTRIBUTING.md tests/contract/test_07_public_docs.sh
git commit -m "docs: describe native release and recovery procedure"
```

---

### Task 5: Full contract suite in a disposable checkout

**Files:** none modified.

- [ ] **Step 1: Run the aggregate suite from a clean detached checkout**

The repository checkout has `.compozy/`, so the aggregate must run elsewhere (see CONTRIBUTING). Requires the local daemon running (`compozy daemon status`).

```bash
clean=$(mktemp -d /tmp/batuta-clean.XXXXXX)
git worktree add --detach "$clean" HEAD
(cd "$clean" && tests/contract/run.sh)
git worktree remove --force "$clean"
```

Expected: `=== todos os testes de contrato passaram ===`. If a daemon-backed test fails for environment reasons unrelated to this change (provider auth, workspace registration), record the exact failing test name and output in the PR description; do not weaken the test.

- [ ] **Step 2: Push `main`**

```bash
git push origin main
```

Expected: `Candidate CI` run on `main` goes green (`gh run watch` or `gh run list --limit 1`).

---

### Task 6: Repository topic

- [ ] **Step 1: Add the search topic**

```bash
gh repo edit franciscpd/batuta-compozy --add-topic compozy-extension
gh repo view franciscpd/batuta-compozy --json repositoryTopics --jq '[.repositoryTopics[].name]'
```

Expected: the list contains `compozy-extension` and the existing topics.

---

### Task 7: Republish v0.1.0-beta.2 (operator, destructive — confirm with the maintainer before Step 1)

**Precondition:** Task 5 pushed and CI green; `git rev-parse origin/main` is the SHA to release; `extension.toml` says `0.1.0-beta.2`; `docs/releases/0.1.0-beta.2.md` exists on that SHA.

- [ ] **Step 1: Delete the old preview release and tag**

```bash
gh release view v0.1.0-beta.2 --repo franciscpd/batuta-compozy --json assets --jq '[.assets[].name]'
gh release delete v0.1.0-beta.2 --repo franciscpd/batuta-compozy --cleanup-tag --yes
git ls-remote --tags origin refs/tags/v0.1.0-beta.2   # expected: empty
```

- [ ] **Step 2: Dispatch the release workflow and watch it**

```bash
sha=$(git rev-parse origin/main)
gh workflow run release.yml --repo franciscpd/batuta-compozy -f release_ref="$sha" -f release_version=0.1.0-beta.2
sleep 20
run_id=$(gh run list --repo franciscpd/batuta-compozy --workflow release.yml --limit 1 --json databaseId --jq '.[0].databaseId')
gh run watch "$run_id" --repo franciscpd/batuta-compozy --exit-status
```

Expected: exit 0; the `Verify GitHub installation` step passed for both sources.

- [ ] **Step 3: Verify from this machine (read-only)**

```bash
gh release view v0.1.0-beta.2 --repo franciscpd/batuta-compozy --json isDraft,isPrerelease,assets --jq '{isDraft,isPrerelease,assets:[.assets[].name]}'
git ls-remote --tags origin 'refs/tags/v0.1.0-beta.2^{}'
compozy extension search batuta -o json
```

Expected: `isDraft:false, isPrerelease:false, assets:["batuta-v0.1.0-beta.2.tar.gz","batuta-v0.1.0-beta.2.tar.gz.sha256"]`; peeled tag == `$sha`; search lists `franciscpd/batuta-compozy` (may lag GitHub's search index by a few minutes; `sources_degraded: ["curated"]` is expected and unrelated).

- [ ] **Step 4: Upgrade the local production install (optional, maintainer's machine)**

The local daemon currently runs beta.1 from `local_path`. To move it to the published release:

```bash
compozy extension remove batuta --global -o json
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes -o json
compozy extension provenance batuta -o json | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["installed_from"], d["digest_matched"])'
```

Expected: `github True`.

- [ ] **Step 5: Record**

Add one line to `docs/releases/0.1.0-beta.2.md` only if the release date differs from `2026-08-16`; otherwise nothing to commit.

---

## Self-review notes

- Spec coverage: workflow (Task 1), README + new docs (Tasks 2–3), release notes + CONTRIBUTING (Task 4), contract tests (Tasks 1–4), topic (Task 6), beta.2 republish (Task 7). Version guard, dev-loop, internal docs relocation stay out of scope per spec.
- The old `test_07_workflow_contract.sh` assertions on `docs/superpowers/*` recovery sentences are dropped with the release section; the CI section is untouched.
- Asset name derivation: `publishAssetName(manifest.Name, tag)` → `batuta-v0.1.0-beta.2.tar.gz`; used consistently in workflow (`archive="batuta-${tag}.tar.gz"`), docs, and tests.
