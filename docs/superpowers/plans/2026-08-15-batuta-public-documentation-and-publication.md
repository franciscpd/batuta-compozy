# Batuta Public Documentation and Publication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Publish Batuta's first public preview with an exact MIT-licensed package, concise bilingual project documentation, a sanitized end-to-end case study, and a factual bilingual blog-writing brief generated only after release verification.

**Architecture:** The repository remains a resource-only CompozyOS extension. Licensing becomes part of the deterministic package boundary, while architecture, contribution, and case-study documents remain repository-only; the existing CI/release workflows continue to produce exactly two release assets. Publication is gated by local contracts, independent Claude Fable review, GitHub CI, release provenance, and an isolated install before the operator-only blog brief is written.

**Tech Stack:** Markdown, Bash, Python 3 standard library, GitHub Actions, GitHub CLI, Git, CompozyOS CLI, Claude Code CLI.

## Global Constraints

- This plan consumes completed Tasks 1-5 from `docs/superpowers/plans/2026-08-15-batuta-preview-release.md` and supersedes that plan's unexecuted Tasks 6-7.
- Public repository: `franciscpd/batuta-compozy`.
- First preview version: `0.1.0-beta.2`; tag: `v0.1.0-beta.2`.
- Compatible CompozyOS source commit: `a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c`; the canonical embedded binary abbreviation is `a35eda6d`.
- Preserve the full `feat/batuta-reliability` history as remote `main`.
- Never stage, package, upload, modify, move, or delete the repository `.compozy/` directory.
- License: the standard MIT License, with exact notice `Copyright (c) 2026 Francisross Soares de Oliveira`.
- Do not add a `license` field to `extension.toml`; CompozyOS does not define that manifest field.
- The deterministic extension package contains exactly five regular files:

  ```text
  ./LICENSE
  ./agents/batuta/AGENT.md
  ./extension.toml
  ./loops/batuta-deliver/loop.yaml
  ./resources/skills/batuta-routing/SKILL.md
  ```

- Release assets remain exactly `batuta-compozy_0.1.0-beta.2.tar.gz` and `SHA256SUMS`; repository documentation is not copied into the archive.
- Batuta must be described as an independent community project, not an official CompozyOS component or endorsement.
- Public claims about CompozyOS use only official sources:
  - `https://www.compozy.com/docs/`
  - `https://www.compozy.com/docs/getting-started/`
  - `https://github.com/compozy/compozy`
  - `https://www.compozy.com/blog/introducing-compozyos/`
- The case study may describe the verified visual journey, including `todo 1.0.0`, but must not expose raw transcripts, local paths, workspace/session/run IDs, ports, process IDs, operator configuration, provider credentials, model-account state, or cost claims.
- The observed visual result is bounded to these verified facts: the parent delivery, implementation child, and review child all ended `done`; `auto_commit=false` left three implementation files uncommitted; the independent suite passed 9/9 tests; executor sessions were not visually nested and remained `active/idle`.
- Model pricing is omitted unless rechecked against a current official source at publication time; this plan does not require pricing.
- The blog brief is created at `/home/franciscpd/Documents/batuta-compozy-blog-brief.md` only after the public release passes checksum, attestation, and isolated-install verification.
- Blog articles are separate works governed by the blog's own policy; the repository MIT license does not silently license them.
- Before repository creation or release publication, run a read-only independent review with `claude --model fable`; verified findings must be resolved, while ambiguous recommendations must be escalated rather than guessed.
- Publish only by the existing `workflow_dispatch` release workflow. Do not automate stable releases, catalog submission, or installation into the operator's live Compozy home.

---

## File Map

- `LICENSE`: exact MIT grant and warranty disclaimer with the approved copyright holder.
- `scripts/stage-extension.sh`: copies the root license into the staged extension package.
- `tests/contract/test_01_stage.sh`: owns exact staged-tree inventory.
- `tests/contract/test_01_package.sh`: owns exact content-addressed package inventory and modes.
- `tests/contract/test_07_preview_assets.sh`: owns exact deterministic archive inventory and `.compozy` exclusion.
- `tests/contract/test_07_license.sh`: owns license text, manifest-boundary, and packaged-byte parity.
- `README.md`, `README.pt-BR.md`: bilingual public entry points, installation, trust boundary, verified workflow, limitations, and links.
- `docs/architecture.md`: public component boundaries and data flow.
- `CONTRIBUTING.md`: local validation and pull-request contract.
- `tests/contract/test_07_public_docs.sh`: owns public links, source policy, contributor commands, and bilingual contract parity.
- `docs/case-studies/version-subcommand.md`: sanitized end-to-end visual journey.
- `tests/contract/test_07_case_study.sh`: owns factual anchors and privacy exclusions.
- `docs/releases/0.1.0-beta.2.md`: release-facing license, documentation, verification, and limitation summary.
- `/home/franciscpd/Documents/batuta-compozy-blog-brief.md`: final operator-only Claude writing brief; never staged or packaged.

### Task 1: Add MIT Licensing to the Deterministic Package

**Files:**
- Create: `LICENSE`
- Modify: `scripts/stage-extension.sh`
- Modify: `tests/contract/test_01_stage.sh`
- Modify: `tests/contract/test_01_package.sh`
- Modify: `tests/contract/test_07_preview_assets.sh`
- Create: `tests/contract/test_07_license.sh`

**Interfaces:**
- Consumes: the root `LICENSE`, existing staging/package scripts, and the deterministic preview builder.
- Produces: byte-identical repository, staged-package, content-addressed-package, and preview-archive copies of `LICENSE`; all package inventories contain exactly the five files in Global Constraints.

- [ ] **Step 1: Write the failing exact-license contract**

Create `tests/contract/test_07_license.sh` with strict Bash mode. Have it generate an expected license in a guarded temporary file and compare it byte-for-byte to the repository license:

```text
MIT License

Copyright (c) 2026 Francisross Soares de Oliveira

Permission is hereby granted, free of charge, to any person obtaining a copy
of this software and associated documentation files (the "Software"), to deal
in the Software without restriction, including without limitation the rights
to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
copies of the Software, and to permit persons to whom the Software is
furnished to do so, subject to the following conditions:

The above copyright notice and this permission notice shall be included in all
copies or substantial portions of the Software.

THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
SOFTWARE.
```

The test must additionally:

- reject a symlinked `LICENSE`;
- require `extension.toml` not to contain a `license` key;
- stage the extension into a guarded empty directory and `cmp` staged `LICENSE` with the root file;
- build one content-addressed package under a guarded temporary `BATUTA_PACKAGE_ROOT` and compare its license bytes;
- build one preview archive, extract it, and compare its license bytes;
- assert that every one of those package trees contains the exact five-file inventory;
- preserve a pre-existing repository `.compozy/` entry byte-for-byte using the established marker-preservation helpers rather than deleting it.

- [ ] **Step 2: Run the license and inventory contracts to verify RED**

Run:

```bash
bash -n tests/contract/test_07_license.sh
tests/contract/test_07_license.sh
tests/contract/test_01_stage.sh
tests/contract/test_01_package.sh
tests/contract/test_07_preview_assets.sh
```

Expected: the new license contract fails because `LICENSE` is absent; existing inventory contracts fail after their expected lists are changed to five files and before staging copies the license.

- [ ] **Step 3: Add the exact MIT file and stage it**

Create `LICENSE` with the exact text from Step 1, including its final newline. Add this copy beside the manifest in `scripts/stage-extension.sh`:

```bash
cp -- "$ROOT/LICENSE" "$STAGE/LICENSE"
cp -- "$ROOT/extension.toml" "$STAGE/extension.toml"
```

Do not modify `extension.toml`.

- [ ] **Step 4: Update all exact inventories**

In each of `test_01_stage.sh`, `test_01_package.sh`, and `test_07_preview_assets.sh`, set the expected sorted file list to:

```bash
expected=$(printf '%s\n' \
  './LICENSE' \
  './agents/batuta/AGENT.md' \
  './extension.toml' \
  './loops/batuta-deliver/loop.yaml' \
  './resources/skills/batuta-routing/SKILL.md')
```

Keep the live extension resource assertion at exactly three resources; `LICENSE` is a package file, not an extension resource.

- [ ] **Step 5: Run focused GREEN verification**

Run:

```bash
bash -n scripts/stage-extension.sh tests/contract/test_01_stage.sh \
  tests/contract/test_01_package.sh tests/contract/test_07_preview_assets.sh \
  tests/contract/test_07_license.sh
tests/contract/test_07_license.sh
tests/contract/test_01_stage.sh
tests/contract/test_01_package.sh
tests/contract/test_07_preview_assets.sh
git diff --check
```

Expected: every command exits zero; the package/archive inventory is five files; `.compozy/` is unchanged.

- [ ] **Step 6: Commit the license boundary**

```bash
git add LICENSE scripts/stage-extension.sh \
  tests/contract/test_01_stage.sh tests/contract/test_01_package.sh \
  tests/contract/test_07_preview_assets.sh tests/contract/test_07_license.sh
git commit -m "build: license Batuta preview under MIT"
```

### Task 2: Add the Public Architecture and Contribution Guides

**Files:**
- Create: `docs/architecture.md`
- Create: `CONTRIBUTING.md`
- Modify: `README.md`
- Modify: `README.pt-BR.md`
- Modify: `docs/releases/0.1.0-beta.2.md`
- Create: `tests/contract/test_07_public_docs.sh`

**Interfaces:**
- Consumes: current agent/skill/Loop behavior, existing preview commands, official CompozyOS sources, and the MIT package contract from Task 1.
- Produces: one canonical English public contract, one aligned PT-BR contract, one architecture explanation, one contribution workflow, and release links that later case-study/publication tasks can rely on.

- [ ] **Step 1: Write the failing public-documentation contract**

Create `tests/contract/test_07_public_docs.sh`. Require real regular files at `docs/architecture.md` and `CONTRIBUTING.md`, then require all of these exact public anchors:

```text
README.md:
  Independent community project
  docs/architecture.md
  docs/case-studies/version-subcommand.md
  CONTRIBUTING.md
  LICENSE
  https://www.compozy.com/docs/
  https://github.com/compozy/compozy

README.pt-BR.md:
  Projeto independente da comunidade
  docs/architecture.md
  docs/case-studies/version-subcommand.md
  CONTRIBUTING.md
  LICENSE
  https://www.compozy.com/docs/
  https://github.com/compozy/compozy

docs/architecture.md:
  Operator
  Batuta session
  spec-cycle
  ext__spec_cycle__import_tasks
  batuta-deliver
  implement-tasks
  review-and-fix
  compozy__session_prompt
  Resource and authority boundaries

CONTRIBUTING.md:
  bash -n scripts/*.sh tests/contract/*.sh
  python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v
  tests/contract/run.sh
  git diff --check
  ^(build|ci|docs|feat|fix|perf|refactor|test): [a-z].+$
  .compozy/
```

Require the release notes to link `../../LICENSE`, `../architecture.md`, and `../case-studies/version-subcommand.md`. Reject wording that calls Batuta `official`, `endorsed`, or a `CompozyOS component` unless the same sentence explicitly denies that status.

- [ ] **Step 2: Run the public-documentation contract to verify RED**

Run:

```bash
bash -n tests/contract/test_07_public_docs.sh
tests/contract/test_07_public_docs.sh
```

Expected: exit nonzero because architecture and contribution files do not exist and the README links are absent.

- [ ] **Step 3: Write `docs/architecture.md`**

Use these sections and facts:

1. **Purpose and boundaries** — Batuta is a resource-only community extension; it orchestrates but does not implement code.
2. **Components** — one `batuta` agent, one `batuta-routing` skill, one `batuta-deliver` Loop; `spec-cycle` supplies `cy-*`, `implement-tasks`, and `review-and-fix`; CompozyOS owns sessions, tool policy, durable Loop execution, and terminal effects.
3. **Data flow** — use this compact sequence:

   ```text
   Operator
     -> Batuta session
     -> spec-cycle requirements and artifacts
     -> .compozy/tasks/$slug/task_*.md
     -> ext__spec_cycle__import_tasks preflight
     -> batuta-deliver
        -> implement-tasks
        -> review-and-fix
     -> compozy__session_prompt terminal return
     -> original Batuta conversation
   ```

4. **Preference and routing authority** — exact workspace `auto_commit` gate; live provider catalog; stored `implement-tasks` override; child Loops resolve their own runtime rules.
5. **Resource and authority boundaries** — Batuta never writes implementation code, approves its own gates, polls live runs, pushes, or silently selects a commit preference.
6. **Session lifecycle** — terminal callbacks return to the origin conversation; current upstream UI does not visually nest executor sessions, and normal terminal completion leaves them `active/idle`.
7. **Trust and compatibility** — link the official docs/repository and distinguish manifest grammar floor from the verified full source commit.

Do not copy internal design docs or expose local evidence paths.

- [ ] **Step 4: Write `CONTRIBUTING.md`**

Document:

- Bash, Python 3, Git, GitHub CLI, Go 1.26.4, and a compatible CompozyOS checkout/runtime;
- isolated feature branches/worktrees;
- the exact four validation commands owned by Step 1;
- `empty_output_directory=$(mktemp -d /tmp/batuta-contributor-assets.XXXXXX)` followed by `scripts/build-preview-assets.sh 0.1.0-beta.2 "$empty_output_directory"` as the deterministic package check;
- that `tests/contract/run.sh` may register a temporary workspace but must leave `.compozy/` absent or byte-identical;
- contract ownership by `test_01_*`, `test_05_*`, and `test_07_*` families;
- commit regex `^(build|ci|docs|feat|fix|perf|refactor|test): [a-z].+$`;
- no direct release mutation outside `.github/workflows/preview-release.yml`;
- PR evidence: focused RED/GREEN, aggregate contracts, deterministic asset digest, and visual evidence only when behavior changes.

- [ ] **Step 5: Refactor both READMEs into public entry points**

Keep their existing installation and verified behavior, then add matching public sections:

- concise CompozyOS introduction with official source links;
- exact independent-community disclaimer;
- “How Batuta fits” / “Como o Batuta se encaixa” linking the architecture guide;
- links to the case study, contributing guide, release notes, and MIT license;
- a five-file archive statement that does not confuse package files with three live resources;
- a direct note that this is a beta preview with known session nesting/lifecycle limitations.

English remains canonical; PT-BR must preserve commands, identifiers, hashes, versions, URLs, and behavior exactly.

- [ ] **Step 6: Update the beta.2 release notes**

Add short sections for documentation and licensing. State that the archive includes `LICENSE`, the repository is MIT licensed, the release still publishes exactly two assets, and repository-only docs are not in the extension archive. Link architecture, case study, and license using the relative paths from Step 1.

- [ ] **Step 7: Run focused documentation GREEN checks**

Run:

```bash
bash -n tests/contract/test_07_public_docs.sh tests/contract/test_07_preview_docs.sh
tests/contract/test_07_public_docs.sh
tests/contract/test_07_preview_docs.sh
git diff --check
```

Expected: all commands exit zero and the EN/PT-BR public contracts agree on versions, commands, verified commit, archive inventory, and limitations.

- [ ] **Step 8: Commit the public guides**

```bash
git add README.md README.pt-BR.md CONTRIBUTING.md docs/architecture.md \
  docs/releases/0.1.0-beta.2.md tests/contract/test_07_public_docs.sh
git commit -m "docs: add Batuta public project guides"
```

### Task 3: Publish the Sanitized Version-Subcommand Case Study

**Files:**
- Create: `docs/case-studies/version-subcommand.md`
- Create: `tests/contract/test_07_case_study.sh`
- Modify: `tests/contract/test_07_public_docs.sh`

**Interfaces:**
- Consumes: the verified isolated planning report, the durable visual-session evidence, the final visual workspace diff, and public architecture terminology.
- Produces: a reproducible technical narrative containing only bounded public facts; the privacy contract rejects operator-specific evidence.

- [ ] **Step 1: Write the failing case-study contract**

Create `tests/contract/test_07_case_study.sh`. Require these factual anchors:

```text
todo 1.0.0
auto_commit=false
cy-create-spec
cy-create-tasks
ext__spec_cycle__import_tasks
batuta-deliver
implement-tasks
review-and-fix
9/9
README.md
src/cli.py
tests/test_cli.py
executor sessions are not visually nested
active/idle
v0.1.0-beta.2
```

Use a Python denylist over the complete Markdown file and fail on:

```python
forbidden = [
    r"/home/", r"/tmp/", r"COMPOZY_HOME", r"sess[-_]", r"ws_",
    r"looprun-", r"turn-", r"127\.0\.0\.1", r"localhost:\d+",
    r"\bPID\b", r"\$\d+(?:\.\d+)?", r"\bUSD\b",
    r"acp_session_id", r"provider credential", r"raw transcript",
]
```

Also reject Markdown links targeting `.superpowers/`, `.compozy/`, or `.local/state/`, while allowing the literal public task pattern `.compozy/tasks/$slug/task_*.md` only as unlinked prose.

- [ ] **Step 2: Run the case-study contract to verify RED**

Run:

```bash
bash -n tests/contract/test_07_case_study.sh
tests/contract/test_07_case_study.sh
```

Expected: exit nonzero because the case-study file does not exist.

- [ ] **Step 3: Write the reproducible case study**

Create `docs/case-studies/version-subcommand.md` with this exact narrative boundary:

1. **Question** — sanitized request: “Add a minimal CLI feature that preserves the literal requirement `todo 1.0.0`. Do not write code before the specification and tasks are approved.”
2. **Environment** — Batuta beta.2 candidate, compatible CompozyOS source identity, bundled `spec-cycle` 0.4.0, clean fixture repository; no provider/model or machine identity.
3. **Preference gate** — the exact workspace key was absent, the operator chose false, and Batuta persisted and reread `auto_commit=false` before planning.
4. **Specification** — `cy-create-spec` produced `_spec.md`, `_user_stories.md`, `_dx.md`, and `_tests.md`; no `_uiux.md` was needed because the request had no Web surface.
5. **Tasks and preflight** — `cy-create-tasks` produced one backend task; direct `ext__spec_cycle__import_tasks` returned a positive task count; the literal version survived unchanged.
6. **Delivery** — a dry-run preceded the real `batuta-deliver`; the composite run invoked `implement-tasks` and then `review-and-fix`; all three runs reached exact terminal `done`.
7. **Observable result** — only `README.md`, `src/cli.py`, and `tests/test_cli.py` changed; 9/9 tests passed; no commit was created because `auto_commit=false`.
8. **What this proves** — bounded orchestration, literal requirement preservation, implementation/review ordering, and event-driven terminal return.
9. **What this does not prove** — general performance, cost, provider superiority, stable compatibility, automatic session nesting, or automatic executor-session stop.
10. **Reproduce** — link the public README, architecture, release notes, Loop file, routing skill, and release URL `https://github.com/franciscpd/batuta-compozy/releases/tag/v0.1.0-beta.2`.

Paraphrase the evidence; do not paste transcript messages or raw JSON.

- [ ] **Step 4: Run privacy and factual GREEN checks**

Run:

```bash
bash -n tests/contract/test_07_case_study.sh tests/contract/test_07_public_docs.sh
tests/contract/test_07_case_study.sh
tests/contract/test_07_public_docs.sh
git diff --check
```

Then manually compare every outcome sentence with the durable public CLI evidence:

```bash
compozy loop runs --workspace /home/franciscpd/Projects/batuta-visual-smoke-spec-cycle -o json
compozy session recap sess-bc0d0c681a97977b --limit 30 -o json
git -C /home/franciscpd/Projects/batuta-visual-smoke-spec-cycle status --short
python3 -m unittest discover -s /home/franciscpd/Projects/batuta-visual-smoke-spec-cycle/tests -v
```

These commands are private verification inputs only. Their IDs and paths must not be copied into the case study.

- [ ] **Step 5: Commit the sanitized case study**

```bash
git add docs/case-studies/version-subcommand.md \
  tests/contract/test_07_case_study.sh tests/contract/test_07_public_docs.sh
git commit -m "docs: add sanitized Batuta case study"
```

### Task 4: Run the Complete Local Gate and Claude Fable Review

**Files:**
- Modify only if a verified review finding requires a scoped correction.

**Interfaces:**
- Consumes: the complete candidate branch through Task 3.
- Produces: a clean local candidate, complete verification evidence, and one read-only independent review verdict before any GitHub repository mutation.

- [ ] **Step 1: Record the protected workspace marker and candidate identity**

Run:

```bash
git status --short
git rev-parse HEAD
git log --oneline --decorate -12
```

Require that `.compozy/` is the only untracked path. Reuse the repository's established no-atime marker snapshot helper before aggregate tests; do not remove the marker.

- [ ] **Step 2: Run syntax and focused validators**

Run:

```bash
bash -n scripts/*.sh tests/contract/*.sh
python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v
tests/contract/test_07_license.sh
tests/contract/test_07_public_docs.sh
tests/contract/test_07_case_study.sh
tests/contract/test_07_preview_assets.sh
tests/contract/test_07_preview_docs.sh
tests/contract/test_07_workflow_contract.sh
git diff --check
```

Expected: every command exits zero.

- [ ] **Step 3: Run the aggregate contract suite safely**

Run `tests/contract/run.sh` with the same hold/restore procedure already used by the preview verification: preserve the exact pre-existing `.compozy/` tree outside the runner-visible path, verify the hold target, run the suite, restore the exact tree in an `EXIT` trap, and compare its byte/metadata snapshot afterward.

Expected: every discovered `test_*.sh` passes; the restored `.compozy/` snapshot is byte-identical; no registration or daemon survives the run.

- [ ] **Step 4: Rebuild and inspect deterministic beta.2 assets**

Use guarded empty directories created with `mktemp -d /tmp/batuta-final-assets.XXXXXX` and `mktemp -d /tmp/batuta-final-extract.XXXXXX`, run the builder twice with the same `SOURCE_DATE_EPOCH`, verify identical archive SHA-256 values and `SHA256SUMS`, extract one archive into the second directory, and require the five-file inventory from Global Constraints. Run `compozy extension validate "$extracted_directory" -o json` with the trusted compatible binary after assigning that second canonical path to `extracted_directory`.

Expected: deterministic digest, successful extension validation, no `.compozy/` entry, and exactly two outer release assets.

- [ ] **Step 5: Run deslop review**

Use the `deslop` skill against the branch diff from the last public-release baseline. Accept only changes that remove duplication, unsupported claims, or unnecessary prose without weakening tests or public contracts. Re-run the affected focused contracts after any edit.

- [ ] **Step 6: Run Claude Fable read-only review**

First verify CLI/auth availability:

```bash
claude --version
claude auth status
```

Then run from the candidate worktree:

```bash
claude -p \
  --model fable \
  --effort high \
  --permission-mode dontAsk \
  --tools "Read,Glob,Grep,Bash" \
  --allowed-tools "Read,Glob,Grep,Bash(git status:*),Bash(git diff:*),Bash(git log:*),Bash(rg:*),Bash(find:*)" \
  --no-session-persistence \
  "Review the Batuta public-preview candidate from commit d97889a42d58ad827d105161212821af675688a1 through HEAD. Read the approved design at docs/superpowers/specs/2026-08-15-batuta-public-documentation-and-blog-design.md and its implementation plan. Verify correctness, security/trust boundaries, GitHub Actions least privilege, deterministic packaging, exact MIT inclusion, English/PT-BR consistency, privacy of the version-subcommand case study, factual support, and compliance with existing repository conventions. Do not edit files, run mutating commands, contact GitHub, or propose broader product features. Report only evidence-backed findings with severity, exact file/line, and a concrete correction; otherwise say APPROVED."
```

The permitted Bash patterns are read-only. If the CLI requests broader permission, stop rather than granting it.

- [ ] **Step 7: Resolve only verified review findings**

For each factual finding, reproduce it with a focused test or exact file comparison before editing. Apply the smallest in-scope correction, rerun the owning focused contract, and rerun the same read-only Fable review prompt. Do not implement ambiguous architectural preferences; report them to the operator.

- [ ] **Step 8: Run final local verification and commit review fixes if needed**

Repeat Steps 2-4 and run `git diff --check`. If review fixes changed tracked files, create one conventional commit whose type matches the change; do not create an empty review commit. Require a clean tracked worktree and only the preserved untracked `.compozy/` before Task 5.

### Task 5: Create and Verify the Public GitHub Repository

**Files:**
- No repository file changes expected.

**Interfaces:**
- Consumes: locally reviewed `feat/batuta-reliability` HEAD and authenticated `gh` account `franciscpd`.
- Produces: public `franciscpd/batuta-compozy`, remote `main` at the exact local HEAD, repository metadata, read-only default workflow permissions, and one successful initial Candidate CI run.

- [ ] **Step 1: Verify account, absence, and local identity**

Run:

```bash
gh auth status
gh repo view franciscpd/batuta-compozy --json nameWithOwner,isPrivate
git status --short
git rev-parse HEAD
git branch --show-current
```

Expected: authenticated as `franciscpd`; repository lookup returns not found; branch is `feat/batuta-reliability`; tracked tree is clean; only `.compozy/` is untracked. If the repository already exists, stop and inspect it—do not overwrite it.

- [ ] **Step 2: Create the empty public repository**

Run:

```bash
gh repo create franciscpd/batuta-compozy \
  --public \
  --description "Batuta, an independent community orchestration extension for CompozyOS" \
  --disable-wiki
```

Do not use `--add-readme`, `--license`, or `--gitignore`; all repository content already exists locally.

- [ ] **Step 3: Add the remote and push only candidate HEAD as `main`**

Run:

```bash
git remote add origin git@github.com:franciscpd/batuta-compozy.git
git push -u origin HEAD:main
```

Do not push local feature refs, old tags, or `.compozy/`.

- [ ] **Step 4: Configure repository metadata and workflow permissions**

Run:

```bash
gh repo edit franciscpd/batuta-compozy \
  --homepage "https://www.compozy.com/docs/" \
  --add-topic compozyos \
  --add-topic ai-agents \
  --add-topic orchestration \
  --add-topic github-actions
gh api --method PUT repos/franciscpd/batuta-compozy/actions/permissions/workflow \
  -f default_workflow_permissions=read \
  -F can_approve_pull_request_reviews=false
```

- [ ] **Step 5: Verify remote identity and public files**

Use structured reads to require:

- `defaultBranchRef.name == "main"`;
- `isPrivate == false`;
- remote `main` OID equals the local full HEAD;
- `LICENSE`, both READMEs, architecture, case study, contribution guide, release notes, and both workflows are readable from `main`;
- Actions default permissions are read-only.

- [ ] **Step 6: Monitor initial CI to a terminal result**

Run:

```bash
local_head=$(git rev-parse HEAD)
run_id=$(gh run list --repo franciscpd/batuta-compozy --workflow ci.yml --limit 20 \
  --json databaseId,headSha,createdAt \
  --jq "map(select(.headSha == \"$local_head\")) | sort_by(.createdAt) | last | .databaseId")
[[ $run_id =~ ^[0-9]+$ ]]
gh run watch --repo franciscpd/batuta-compozy "$run_id" --exit-status
```

Select the run whose `headSha` equals the pushed HEAD. A failed check blocks release; diagnose only evidence-backed in-scope failures and do not weaken a contract to obtain green.

### Task 6: Publish and Independently Smoke-Test `v0.1.0-beta.2`

**Files:**
- No repository file changes expected unless a verified release defect requires returning to an earlier task.

**Interfaces:**
- Consumes: green remote `main`, exact full release SHA, existing `preview-release.yml`, and GitHub release permissions.
- Produces: annotated `v0.1.0-beta.2`, non-draft prerelease, exact two outer assets, attestations, and an isolated install exposing exactly three Batuta resources from a five-file package.

- [ ] **Step 1: Dispatch the exact release workflow**

Resolve remote `main` to a full SHA, require it equals the locally reviewed HEAD, then run:

```bash
release_ref=$(gh api repos/franciscpd/batuta-compozy/git/ref/heads/main --jq .object.sha)
[[ $release_ref =~ ^[0-9a-f]{40}$ ]]
[[ $release_ref == "$(git rev-parse HEAD)" ]]
gh workflow run preview-release.yml \
  --repo franciscpd/batuta-compozy \
  --ref main \
  -f release_ref="$release_ref" \
  -f release_version=0.1.0-beta.2
```

The value passed to `release_ref` is the exact 40-character SHA returned immediately before dispatch; never use a prefix or mutable branch name.

- [ ] **Step 2: Monitor the workflow and stop on failure**

Select the run by workflow, dispatch time, and exact head SHA; then run `gh run watch --exit-status`. If verification fails before mutation, correct the candidate through a new reviewed commit and repeat Task 5 CI. If it fails after tag or draft creation, preserve the partial remote state and request operator direction; do not auto-delete or republish.

- [ ] **Step 3: Verify release metadata and assets through GitHub**

Require with `gh release view` and GraphQL:

- tag `v0.1.0-beta.2` is annotated and resolves to the exact release SHA;
- `isDraft=false`, `isPrerelease=true`, and `isLatest=false`;
- release assets are exactly `SHA256SUMS` and `batuta-compozy_0.1.0-beta.2.tar.gz`;
- the release body matches `docs/releases/0.1.0-beta.2.md`;
- build provenance attestations verify for both assets.

- [ ] **Step 4: Download and verify in a fresh guarded directory**

Run `gh release download`, then:

```bash
(cd "$download_dir" && sha256sum --check SHA256SUMS)
tar -xzf "$download_dir/batuta-compozy_0.1.0-beta.2.tar.gz" -C "$extracted_directory"
```

Require the exact five-file internal inventory and byte-identical standard `LICENSE`. Record the archive SHA-256 for Task 7.

- [ ] **Step 5: Perform isolated CompozyOS install smoke**

Create a fresh guarded `COMPOZY_HOME`, use the trusted compatible CompozyOS binary, start its daemon on isolated ports/UDS, validate the extracted directory, install with the explicit preview trust flag, enable Batuta, and verify:

- extension `batuta` is `active/healthy`;
- exact live resources are one agent `batuta`, one skill `batuta-routing`, and one Loop `batuta-deliver`;
- `spec-cycle` compatibility resolves;
- no fourth live resource appears because `LICENSE` is package metadata, not a resource.

Always stop the isolated daemon and remove only the exact guarded home. Record teardown with zero survivors. Do not install into the operator's live home.

- [ ] **Step 6: Record final publication facts**

Capture, from structured reads:

- repository URL;
- release URL;
- exact tag and source SHA;
- archive SHA-256;
- successful attestation verification;
- Candidate CI and release workflow URLs/conclusions;
- exact CompozyOS version/full commit;
- contract and Python validator counts;
- isolated extension status, three-resource inventory, and clean teardown.

These values are the only release facts Task 7 may use.

### Task 7: Generate the Final Bilingual Blog-Writing Brief

**Files:**
- Create outside repository: `/home/franciscpd/Documents/batuta-compozy-blog-brief.md`

**Interfaces:**
- Consumes: Task 6 structured publication facts, official CompozyOS sources, public repository documents, and sanitized case-study facts.
- Produces: one operator-owned factual brief for Claude Fable to draft four articles; the file contains no unresolved placeholders and is never committed or packaged.

- [ ] **Step 1: Re-read every dynamic publication value**

Immediately before writing, use `gh repo view`, `gh release view`, GraphQL, `gh attestation verify`, `gh run view`, downloaded `SHA256SUMS`, and `compozy version -o json` to re-read all values captured in Task 6. If any identity differs, stop and resolve the mismatch instead of copying stale data.

- [ ] **Step 2: Create the brief with final facts**

Use `apply_patch` to create `/home/franciscpd/Documents/batuta-compozy-blog-brief.md` with these sections:

1. **Purpose** — four drafts, first-person engineering-journey voice, technical audience new to CompozyOS.
2. **Publication order** — CompozyOS introduction first; Batuta article second.
3. **Final release identity** — actual repository/release URLs, tag, exact source SHA, archive SHA-256, attestation result, CI/release run URLs, tested CompozyOS version/full commit, and exact validation counts.
4. **Official CompozyOS sources** — the four URLs in Global Constraints and what each supports.
5. **Public Batuta sources** — actual URLs to README, PT-BR README, architecture, case study, release notes, contribution guide, and license at the released SHA/tag.
6. **Verified Batuta behavior** — resource-only role, initial preference gate, spec/task creation, direct import preflight, dry-run, event-driven dispatch, implementation, review, and terminal return.
7. **Sanitized journey facts** — literal `todo 1.0.0`, `auto_commit=false`, three changed files, 9/9 tests, three terminal `done` runs, no commit.
8. **Known limitations** — beta trust boundary, no visual executor-session nesting, executor sessions remain `active/idle`, no stable compatibility promise, no automatic provider failover in this release.
9. **Claims Claude must not make** — official affiliation, endorsement, adoption, benchmarks, pricing, provider superiority, stable support, automatic commits, automatic push, nested sessions, stopped executor sessions, or features not present in the release.
10. **Draft instructions** — exact four outputs: CompozyOS EN, CompozyOS PT-BR, Batuta EN, Batuta PT-BR.
11. **Localization contract** — PT-BR versions preserve facts and keep code, commands, identifiers, versions, hashes, and URLs byte-for-byte.
12. **Licensing note** — repository software/docs are MIT; blog articles remain under the blog's separate policy.

Do not leave bracketed values, template markers, “TBD”, or an instruction to infer a missing release fact.

- [ ] **Step 3: Run a privacy and placeholder audit**

Use a one-off read-only Python check against the final brief. Require every final URL/SHA/tag/digest from Task 6 and reject:

```python
forbidden = [
    "TBD", "TO BE FILLED", "<release", "<sha", "<url", "/home/",
    "COMPOZY_HOME", "sess-", "sess_", "ws_", "looprun-", "turn-",
    "127.0.0.1", "localhost:", "USD", "acp_session_id",
]
```

The file path itself is outside the document body and is not part of the `/home/` content check. Confirm no raw transcript or model-account data appears.

- [ ] **Step 4: Validate the Claude drafting request without generating articles**

Run a read-only Claude Fable pass over the brief:

```bash
claude -p \
  --model fable \
  --effort high \
  --permission-mode dontAsk \
  --tools "Read" \
  --allowed-tools "Read" \
  --add-dir /home/franciscpd/Documents \
  --no-session-persistence \
  "Read /home/franciscpd/Documents/batuta-compozy-blog-brief.md. Audit whether it is internally consistent, sourced, privacy-safe, free of placeholders, and sufficient to write all four requested drafts without invention. Do not write the articles and do not edit any file. Report APPROVED or evidence-backed blocking gaps."
```

Resolve only factual blocking gaps by re-reading the public release/source. Do not ask Claude to invent missing values.

- [ ] **Step 5: Final handoff**

Report the public repository/release URLs, exact source SHA and archive checksum, local verification totals, independent review verdicts, isolated-smoke result, and the absolute blog-brief path. State explicitly that no blog article was published and that the repository `.compozy/` remained excluded and preserved.

---

## Final Self-Review Checklist

- [ ] Every requirement in `docs/superpowers/specs/2026-08-15-batuta-public-documentation-and-blog-design.md` maps to one task above.
- [ ] The archive inventory is five internal files while the GitHub Release still has two assets and the live extension still has three resources.
- [ ] The MIT notice is exact and no unsupported manifest field is introduced.
- [ ] README EN/PT-BR, architecture, case study, release notes, contribution guide, and license links are all tested.
- [ ] The case study reports only the verified visual boundary and contains no private runtime identity.
- [ ] Claude Fable review is read-only and precedes repository creation/release.
- [ ] Repository creation, push, release dispatch, and install smoke all use structured identity checks and fail closed.
- [ ] The blog brief is written only after release verification, contains actual final values, and stays outside Git.
- [ ] `.compozy/` is never staged, packaged, uploaded, deleted, or silently regenerated.
