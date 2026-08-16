# Batuta Preview Release Automation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Create the public `franciscpd/batuta-compozy` repository and publish `0.1.0-beta.2` through a small, manually dispatched, verifiable GitHub Actions release workflow.

**Architecture:** A reusable CI workflow validates the repository against one pinned compatible CompozyOS commit. A separate `workflow_dispatch` release workflow reuses that gate, builds deterministic assets from the exact requested commit, stages a draft GitHub Release, verifies downloaded assets, and only then publishes the prerelease.

**Tech Stack:** Bash, Python 3 standard library, GitHub Actions, GitHub CLI, Git, CompozyOS extension CLI.

## Global Constraints

- Public repository: `franciscpd/batuta-compozy`.
- First preview version: `0.1.0-beta.2`; tag: `v0.1.0-beta.2`.
- Compatible CompozyOS commit: `a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c`.
- Publish only by `workflow_dispatch`; no tag, push, or schedule publication trigger.
- Preserve the full `feat/batuta-reliability` history as remote `main`.
- Never stage, package, upload, or delete the repository `.compozy/` directory.
- The earlier four-file, no-license package decision is superseded by the approved public documentation and publication plan.
- License the repository software and associated documentation under the exact
  standard MIT text with `Copyright (c) 2026 Francisross Soares de Oliveira`.
- Keep `LICENSE` in the repository root and every preview archive; do not add an
  unsupported license field to `extension.toml`.
- The deterministic package contains exactly five regular files:

  ```text
  ./LICENSE
  ./agents/batuta/AGENT.md
  ./extension.toml
  ./loops/batuta-deliver/loop.yaml
  ./resources/skills/batuta-routing/SKILL.md
  ```

- Release assets are exactly `batuta-compozy_0.1.0-beta.2.tar.gz` and `SHA256SUMS`.
- The GitHub Release must be `isDraft=false`, `isPrerelease=true`, and `isLatest=false` after publication.
- Default workflow permissions are read-only; publication alone receives `contents: write`, `id-token: write`, and `attestations: write`.
- Third-party actions use these reviewed full commit SHAs:
  - `actions/checkout`: `3d3c42e5aac5ba805825da76410c181273ba90b1` (`v7`).
  - `actions/setup-go`: `924ae3a1cded613372ab5595356fb5720e22ba16` (`v6`).
  - `actions/upload-artifact`: `043fb46d1a93c77aae656e7c1c64a875d1fc6a0a` (`v7`), only for failed CI logs.
  - `actions/attest-build-provenance`: `8beda2b7ed98355c0e97c0a63bec38ae472e66c4` (`v4`).
- Do not automate release PRs, version selection, stable publication, catalog submission, or installation into the operator's live Compozy home in this iteration.

---

## File map

- `scripts/build-preview-assets.sh`: validate the requested manifest version and create deterministic release assets.
- `tests/contract/test_07_preview_assets.sh`: prove deterministic packaging, exact inventory, mismatch rejection, and workspace exclusion.
- `.github/workflows/ci.yml`: reusable read-only CI and pinned CompozyOS compatibility gate.
- `.github/workflows/preview-release.yml`: explicit release identity, draft staging, attestation, verification, and publication.
- `tests/contract/test_07_workflow_contract.sh`: enforce triggers, permissions, pins, ordering, and release metadata policy without contacting GitHub.
- `docs/releases/0.1.0-beta.2.md`: canonical reviewed release body and compatibility caveats.
- `extension.toml`: advance the package version to `0.1.0-beta.2`.
- `README.md`, `README.pt-BR.md`: document preview installation and integrity verification.
- `tests/contract/test_07_preview_docs.sh`: keep version, release notes, compatibility, installation, and rollback claims synchronized.

### Task 1: Deterministic preview asset builder

**Files:**
- Create: `scripts/build-preview-assets.sh`
- Create: `tests/contract/test_07_preview_assets.sh`

**Interfaces:**
- Consumes: `scripts/package-extension.sh`, `extension.toml`, `SOURCE_DATE_EPOCH` when supplied.
- Produces: `scripts/build-preview-assets.sh VERSION ABSOLUTE_EMPTY_OUTPUT_DIRECTORY`; writes one archive and `SHA256SUMS`, then prints the archive path.

- [ ] **Step 1: Write the failing asset contract**

Create a guarded `/tmp/batuta-preview-assets.*` root. Resolve the current manifest version with `tomllib`, invoke the missing builder twice into separate empty directories with the same `SOURCE_DATE_EPOCH`, and assert:

```bash
first_sha=$(sha256sum "$first/batuta-compozy_${version}.tar.gz" | cut -d' ' -f1)
second_sha=$(sha256sum "$second/batuta-compozy_${version}.tar.gz" | cut -d' ' -f1)
[[ $first_sha == "$second_sha" ]]
(cd "$first" && sha256sum --check SHA256SUMS)
```

Extract the first archive and require exactly:

```text
./LICENSE
./agents/batuta/AGENT.md
./extension.toml
./loops/batuta-deliver/loop.yaml
./resources/skills/batuta-routing/SKILL.md
```

Also require a version mismatch, relative output path, non-empty output directory, and symlink output directory to fail. Snapshot the pre-existing `.compozy` marker before the build and require it to remain byte-identical and absent from both archives; do not create, modify, move, or delete that marker in this focused test.

- [ ] **Step 2: Run the contract to verify RED**

Run: `tests/contract/test_07_preview_assets.sh`

Expected: exit nonzero because `scripts/build-preview-assets.sh` does not exist.

- [ ] **Step 3: Implement the minimal builder**

Use strict Bash mode. Validate two arguments, an absolute existing empty non-symlink output directory, and strict beta SemVer. Read `extension.version` using Python `tomllib` and require an exact match.

Build the content-addressed package twice beneath a guarded temporary package root and require the same returned directory. Create the archive deterministically:

```bash
archive="$output/batuta-compozy_${version}.tar.gz"
epoch=${SOURCE_DATE_EPOCH:-$(git show -s --format=%ct HEAD)}
tar --sort=name --mtime="@$epoch" --owner=0 --group=0 --numeric-owner \
  -cf - -C "$first_package" . | gzip -n -9 > "$archive"
(cd "$output" && sha256sum "${archive##*/}" > SHA256SUMS)
printf '%s\n' "$archive"
```

The cleanup trap may remove only its exact `/tmp/batuta-preview-package.*` directory and must verify the prefix before removal.

- [ ] **Step 4: Run focused and aggregate GREEN checks**

Run:

```bash
bash -n scripts/build-preview-assets.sh tests/contract/test_07_preview_assets.sh
tests/contract/test_07_preview_assets.sh
git diff --check
```

Expected: all commands exit zero and the contract prints one `OK:` line.

- [ ] **Step 5: Commit the asset builder**

```bash
git add scripts/build-preview-assets.sh tests/contract/test_07_preview_assets.sh
git commit -m "feat: build deterministic Batuta preview assets"
```

### Task 2: Reusable read-only CI workflow

**Files:**
- Create: `.github/workflows/ci.yml`
- Create: `tests/contract/test_07_workflow_contract.sh`

**Interfaces:**
- Consumes: repository checkout and optional reusable-workflow input `checkout_ref`.
- Produces: a reusable `verify` job that preview publication can require with `uses: ./.github/workflows/ci.yml`.

- [ ] **Step 1: Write the failing CI workflow contract**

Add static assertions that `.github/workflows/ci.yml`:

- triggers on pull requests to `main`, pushes to `main`, `workflow_dispatch`, and `workflow_call`;
- declares optional string input `checkout_ref` for `workflow_call`;
- sets top-level `permissions: contents: read`;
- uses cancelable per-ref concurrency;
- pins checkout, setup-go, and failure-log upload to the Global Constraints SHAs;
- checks out the candidate at `${{ inputs.checkout_ref || github.sha }}` into `${{ github.workspace }}/candidate`;
- checks out `compozy/compozy` at the full compatible commit into the sibling `${{ github.workspace }}/compozy-source` directory with full history/tags;
- runs `make build-go`, verifies `compozy version -o json` reports the expected full commit, and prepends its `bin` directory to `GITHUB_PATH`;
- runs Bash syntax, Python validators, `tests/contract/run.sh`, and `git diff --exit-code` from the candidate checkout; the aggregate contract runner owns asset and extension validation.

Keep the test textual and exact; do not add a YAML parser dependency.

- [ ] **Step 2: Run the contract to verify RED**

Run: `tests/contract/test_07_workflow_contract.sh`

Expected: exit nonzero because `.github/workflows/ci.yml` is absent.

- [ ] **Step 3: Implement `.github/workflows/ci.yml`**

Use one `verify` job on `ubuntu-latest`, `timeout-minutes: 45`, Go `1.26.4`, candidate path `candidate`, CompozyOS path `compozy-source`, and candidate working directory `${{ github.workspace }}/candidate`. Use this trigger and concurrency configuration:

```yaml
on:
  pull_request:
    branches: [main]
  push:
    branches: [main]
  workflow_dispatch:
  workflow_call:
    inputs:
      checkout_ref:
        required: false
        type: string

permissions:
  contents: read

concurrency:
  group: ci-${{ github.workflow }}-${{ inputs.checkout_ref || github.ref }}
  cancel-in-progress: true
```

Before `tests/contract/run.sh`, ensure the fresh checkout has no `.compozy` marker and the newly built daemon is started with a runner-local `COMPOZY_HOME`. Register cleanup with a shell trap or a final `if: always()` step that stops the daemon and removes only runner-temporary state. Preserve logs as an Actions artifact only on failure.

- [ ] **Step 4: Prove aggregate-runner discovery without changing the runner**

Run `tests/contract/run.sh` with the existing guarded `.compozy` hold/restore procedure and verify its output contains `=== test_07_workflow_contract.sh ===`. The current `test_*.sh` glob owns discovery, so do not add a duplicate test list.

- [ ] **Step 5: Run local GREEN checks**

Run:

```bash
bash -n tests/contract/test_07_workflow_contract.sh
tests/contract/test_07_workflow_contract.sh
python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v
git diff --check
```

Expected: workflow contract and all Python validators pass; no generated tracked change remains.

- [ ] **Step 6: Commit CI**

```bash
git add .github/workflows/ci.yml tests/contract/test_07_workflow_contract.sh
git commit -m "ci: validate Batuta preview candidates"
```

### Task 3: Manual stage-verify-publish workflow

**Files:**
- Create: `.github/workflows/preview-release.yml`
- Modify: `tests/contract/test_07_workflow_contract.sh`

**Interfaces:**
- Consumes: `release_ref` full SHA, `release_version` unprefixed beta SemVer, reusable CI, and `scripts/build-preview-assets.sh`.
- Produces: annotated tag `v<release_version>` and a verified two-asset GitHub prerelease.

- [ ] **Step 1: Extend the failing workflow contract**

Require all of these properties:

- the only trigger is `workflow_dispatch` with required `release_ref` and `release_version` strings;
- top-level `permissions: contents: read`;
- release concurrency is `cancel-in-progress: false`;
- `verify` calls `./.github/workflows/ci.yml` with `checkout_ref: ${{ inputs.release_ref }}`;
- `publish` needs `verify` and alone receives `contents: write`, `id-token: write`, and `attestations: write`;
- the release checkout uses the exact input ref and verifies `HEAD`, remote-main ancestry, strict beta SemVer, manifest equality, release notes compatibility text, and absence of both tag and release;
- `release_ref` must also equal the workflow's trusted `github.sha`, binding provenance to the released source rather than only to the mutable workflow input;
- asset construction precedes every remote mutation;
- annotated tag creation precedes draft creation;
- the draft is created with `--verify-tag --draft --prerelease --latest=false`;
- both assets receive provenance attestations;
- fresh download and `sha256sum --check` precede `gh release edit --draft=false`;
- final `gh release view --json` asserts draft false, prerelease true, exact tag, and exact two assets; an authenticated GraphQL read asserts `isLatest=false` because the installed CLI does not expose that field in `release view` JSON.

- [ ] **Step 2: Run the contract to verify RED**

Run: `tests/contract/test_07_workflow_contract.sh`

Expected: exit nonzero because the preview workflow is absent.

- [ ] **Step 3: Implement explicit identity and mutation preflight**

Checkout with full history and tags at `inputs.release_ref`. Use:

```bash
[[ $(git rev-parse HEAD) == "$RELEASE_REF" ]]
[[ $RELEASE_REF =~ ^[0-9a-f]{40}$ ]]
[[ $GITHUB_SHA == "$RELEASE_REF" ]]
[[ $RELEASE_VERSION =~ ^(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)\.(0|[1-9][0-9]*)-beta\.(0|[1-9][0-9]*)$ ]]
git fetch origin main --tags
git merge-base --is-ancestor "$RELEASE_REF" origin/main
```

Read the manifest version with `tomllib`. Query the tag and release through one authenticated GraphQL request and require both nodes to be null; a transport or schema error must fail the job rather than be treated as absence. Use this response shape, passing `refs/tags/v<release_version>` as `tagRef` and `v<release_version>` as `tagName`:

```graphql
query($owner: String!, $name: String!, $tagRef: String!, $tagName: String!) {
  repository(owner: $owner, name: $name) {
    ref(qualifiedName: $tagRef) { target { oid } }
    release(tagName: $tagName) { isDraft tagName }
  }
}
```

- [ ] **Step 4: Implement local asset staging and provenance**

In the single protected `publish` job, build into `${RUNNER_TEMP}/release-assets`, validate `SHA256SUMS`, and attest both paths with the pinned attestation action before creating any tag. Keep all later operations in that same runner workspace; no workflow artifact transfer or additional publication job is needed for this two-file preview.

A successful provenance attestation upload is the first preserved remote mutation.
A failure before the first provenance attestation upload leaves no remote mutation.
A failure after either attestation upload preserves and reports the uploaded attestation records, even when no tag exists.

- [ ] **Step 5: Implement annotated tag, draft, download verification, and publish**

Configure the GitHub Actions bot identity, create `git tag -a "v${RELEASE_VERSION}" -m "Release v${RELEASE_VERSION}" "$RELEASE_REF"`, verify `git cat-file -t` returns `tag`, then push only that full tag ref.

Create the draft with the reviewed notes file and both assets. Download into a fresh directory and compare the downloaded asset-name set against:

```text
SHA256SUMS
batuta-compozy_<version>.tar.gz
```

Only after checksum success, publish with `gh release edit "$tag" --draft=false --prerelease --latest=false`. Do not use `--clobber` or automatic deletion. If a post-attestation step fails, retain and report every uploaded attestation plus any tag or draft for explicit operator recovery.

- [ ] **Step 6: Run local GREEN checks**

Run:

```bash
tests/contract/test_07_workflow_contract.sh
bash -n scripts/*.sh tests/contract/*.sh
git diff --check
```

Expected: all checks exit zero.

- [ ] **Step 7: Commit the release workflow**

```bash
git add .github/workflows/preview-release.yml tests/contract/test_07_workflow_contract.sh
git commit -m "ci: publish verified Batuta previews"
```

### Task 4: Version, release notes, and installation documentation

**Files:**
- Create: `docs/releases/0.1.0-beta.2.md`
- Modify: `extension.toml`
- Modify: `README.md`
- Modify: `README.pt-BR.md`
- Modify: `tests/contract/test_01_validate.sh`
- Create: `tests/contract/test_07_preview_docs.sh`

**Interfaces:**
- Consumes: the exact release tag, asset names, compatibility commit, and known upstream limitations.
- Produces: manifest `0.1.0-beta.2` and the canonical body consumed by the release workflow.

- [ ] **Step 1: Write failing documentation/version assertions**

Extend `test_01_validate.sh` to require the manifest version to equal `0.1.0-beta.2`. Add `test_07_preview_docs.sh` to require both READMEs and the release body to contain:

- `franciscpd/batuta-compozy`;
- `v0.1.0-beta.2`;
- archive and `SHA256SUMS` names;
- `gh release download` and `sha256sum --check` installation verification;
- full compatible CompozyOS commit;
- the nesting and active/idle limitations;
- `compozy extension remove batuta --global` rollback/removal guidance.

- [ ] **Step 2: Run focused tests to verify RED**

Run:

```bash
tests/contract/test_01_validate.sh
tests/contract/test_07_preview_docs.sh
```

Expected: failure because the manifest and release documentation still identify beta.1 or omit the preview contract.

- [ ] **Step 3: Update version and write concise release documentation**

Set `extension.version = "0.1.0-beta.2"`. The release notes must distinguish verified Batuta behavior from upstream CompozyOS limitations and must not claim that nesting or automatic stopping is fixed.

Document installation as download, checksum verification, extraction into a new directory, `compozy extension validate`, and `compozy extension install <extracted-directory> --allow-unverified --yes` only when the operator explicitly accepts that preview trust boundary.

- [ ] **Step 4: Run documentation and package GREEN checks**

Run:

```bash
tests/contract/test_01_validate.sh
tests/contract/test_07_preview_docs.sh
tests/contract/test_07_preview_assets.sh
tests/contract/test_07_workflow_contract.sh
git diff --check
```

Expected: all checks pass and the archive contains manifest beta.2.

- [ ] **Step 5: Commit the preview identity**

```bash
git add extension.toml README.md README.pt-BR.md docs/releases/0.1.0-beta.2.md \
  tests/contract/test_01_validate.sh tests/contract/test_07_preview_docs.sh
git commit -m "build: prepare Batuta 0.1.0-beta.2"
```

### Task 5: Aggregate local verification and review

**Files:**
- Modify only if a reproduced defect requires a root-cause correction.

**Interfaces:**
- Consumes: Tasks 1-4 candidate tree.
- Produces: a clean, review-approved release commit suitable for remote `main`.

- [ ] **Step 1: Run all syntax and validator tests**

```bash
bash -n scripts/*.sh tests/contract/*.sh
python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v
```

Expected: all 12 existing Python tests plus any added tests pass.

- [ ] **Step 2: Run the full contract runner with workspace preservation**

Move the pre-existing `.compozy` directory to a guarded `/tmp/batuta-contract-state.*` hold, install a trap that restores it, run `tests/contract/run.sh`, restore it, and verify the workspace marker SHA-256 is unchanged. Never stage the marker.

- [ ] **Step 3: Build and validate final assets locally**

```bash
asset_root=$(mktemp -d /tmp/batuta-preview-final.XXXXXX)
SOURCE_DATE_EPOCH=$(git show -s --format=%ct HEAD) \
  scripts/build-preview-assets.sh 0.1.0-beta.2 "$asset_root"
(cd "$asset_root" && sha256sum --check SHA256SUMS)
```

Extract into a fresh directory, run `compozy extension validate`, and assert the exact five-file package tree listed in Global Constraints.

- [ ] **Step 4: Run deslop and independent code review**

Review only the Task 1-4 diff for unnecessary abstraction, duplicated workflow policy, unsafe cleanup, unpinned actions, weakened tests, and claims unsupported by evidence. Apply only evidence-backed corrections and rerun the owning tests.

- [ ] **Step 5: Confirm the release commit is clean**

```bash
git diff --check
git status --short
git log --oneline d97889a42d58ad827d105161212821af675688a1..HEAD
```

Expected: status contains only `?? .compozy/`; all implementation commits use unscoped English conventional subjects.

### Task 6: Create and configure the public GitHub repository

**Files:**
- No source files; GitHub and Git remote state only.

**Interfaces:**
- Consumes: clean release commit from Task 5 and authenticated `gh` account `franciscpd`.
- Produces: public repository with candidate history on remote `main` and read-only default Actions token permissions.

- [ ] **Step 1: Prove the target does not already exist**

Run `gh repo view franciscpd/batuta-compozy`. Expected: repository-not-found. If it exists, stop and inspect rather than mutating it.

- [ ] **Step 2: Create the empty public repository**

```bash
gh repo create franciscpd/batuta-compozy \
  --public \
  --description "Batuta orchestration extension for CompozyOS" \
  --disable-wiki
```

Do not use `--add-readme`, `--gitignore`, `--license`, or `--push`; the local candidate remains authoritative.

- [ ] **Step 3: Add the remote and push only candidate HEAD as main**

```bash
git remote add origin git@github.com:franciscpd/batuta-compozy.git
git push -u origin HEAD:main
```

Verify remote `main` equals local `HEAD` and remote branches do not include the unrelated local `main` or feature branch name.

- [ ] **Step 4: Configure safe repository defaults through GitHub CLI**

Use `gh api` to set default workflow permissions to read and disallow Actions from approving pull requests. Add topics `compozy`, `compozyos`, `extension`, and `orchestration`. Read the settings back and require exact values.

```bash
gh api -X PUT repos/franciscpd/batuta-compozy/actions/permissions/workflow \
  -f default_workflow_permissions=read \
  -F can_approve_pull_request_reviews=false
gh api -X PUT repos/franciscpd/batuta-compozy/topics \
  -F 'names[]=compozy' \
  -F 'names[]=compozyos' \
  -F 'names[]=extension' \
  -F 'names[]=orchestration'
```

- [ ] **Step 5: Verify repository identity**

Run:

```bash
gh repo view franciscpd/batuta-compozy \
  --json nameWithOwner,visibility,defaultBranchRef,hasIssuesEnabled,url
git ls-remote --heads origin
```

Expected: public, issues enabled, default branch `main`, and exactly the intended main SHA.

### Task 7: Run remote CI and publish the preview

**Files:**
- No source changes unless remote execution exposes a reproducible in-scope defect.

**Interfaces:**
- Consumes: remote main release commit and both workflows.
- Produces: verified GitHub prerelease `v0.1.0-beta.2`.

- [ ] **Step 1: Observe the initial main CI**

Resolve the workflow run for the pushed commit with `gh run list --workflow ci.yml --commit "$release_ref" --json databaseId,status,conclusion,url`. Watch it using `gh run watch "$run_id" --exit-status --compact`.

Expected: conclusion `success`. Diagnose any failure from logs; do not bypass or weaken a gate.

- [ ] **Step 2: Dispatch the release by exact identity**

```bash
release_ref=$(git rev-parse HEAD)
gh workflow run preview-release.yml \
  --repo franciscpd/batuta-compozy \
  --ref main \
  -f release_ref="$release_ref" \
  -f release_version=0.1.0-beta.2
```

Resolve the new run ID by workflow, event, branch, and creation time; never watch an older run accidentally.

- [ ] **Step 3: Watch publication to a terminal result**

Run `gh run watch "$run_id" --repo franciscpd/batuta-compozy --exit-status --compact`.

Expected: success. If it fails after attestation upload, inspect and report the exact attestation, tag, and draft state, then stop for explicit recovery; do not delete or overwrite automatically.

- [ ] **Step 4: Verify release metadata and download integrity**

```bash
gh release view v0.1.0-beta.2 \
  --repo franciscpd/batuta-compozy \
  --json tagName,isDraft,isPrerelease,assets,url
```

Download both assets into a new guarded temp directory and run `sha256sum --check SHA256SUMS`. Require exactly two assets. Resolve the annotated tag through the GitHub API, peel it to its commit object, and require that commit to equal `release_ref`; do not infer commit identity from `targetCommitish` display metadata.

Query the published release through GraphQL and require `isDraft=false`, `isPrerelease=true`, `isLatest=false`, and the exact tag name. Treat transport errors, missing nodes, and unexpected nulls as verification failures.

- [ ] **Step 5: Verify provenance**

For the downloaded archive and checksum file, run:

```bash
gh attestation verify "$asset" \
  --repo franciscpd/batuta-compozy \
  --signer-workflow franciscpd/batuta-compozy/.github/workflows/preview-release.yml \
  --source-digest "$release_ref" \
  --deny-self-hosted-runners
```

Expected: both verifications succeed with the release workflow as signer.

- [ ] **Step 6: Perform isolated install smoke**

Extract the archive into a fresh QA directory and isolated `COMPOZY_HOME`. Run extension validation, install the extracted directory with explicit preview trust consent, and assert Batuta is active/healthy with exactly one agent, one skill, and one Loop. Remove it and tear down the isolated daemon; record `clean: true` and no surviving processes.

- [ ] **Step 7: Final handoff**

Report the repository URL, release URL, release commit, tag, archive SHA-256, attestation result, CI run URL, preview limitations, and install/remove commands. Do not claim stable support or that the two upstream session lifecycle defects are fixed.
