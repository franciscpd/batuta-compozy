# Batuta native install and public README design

Date: 2026-08-16

## Goal

Make installing and understanding Batuta take one command and one screen.
Publication switches from a hand-built archive that the CompozyOS daemon
cannot consume to `compozy extension publish`, so users install with:

```bash
compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes
```

The README is rewritten around what Batuta is, how to install it, and what a
session looks like. Verification, trust, and operational detail move to
dedicated documents.

## Why

Verified against the CompozyOS source at `a35eda6d` (`internal/registry/github/`):

- `releases.go`: an explicit tag lookup (`@vX`) rejects a `draft` or
  `prerelease` release as "not a published full release", and the unversioned
  path uses GitHub's `/releases/latest` (which by GitHub's contract excludes
  drafts and prereleases) with a fallback that filters them out
  (`filterPublishedReleases`). Our preview workflow publishes with
  `--prerelease`, so the native `github:` source cannot install Batuta either
  way. That is the only reason the README needs the
  eight-command `gh release download` / `sha256sum` / `tar` / `validate` /
  `install` sequence.
- `sidecar.go`: when an asset named `<archive>.sha256` exists, the daemon
  verifies the archive against it and records `digest_matched: true`; when it
  is absent the install proceeds with no digest check. We publish
  `SHA256SUMS`, which the daemon ignores.
- `search.go`: `compozy extension search` filters GitHub by
  `topic:compozy-extension`. The repository lacks that topic and is invisible.
- `internal/extension/publish.go`: `compozy extension publish <dir>` needs
  only a directory with a loadable `extension.toml`. It archives the tree,
  uploads `<name>-<tag>.tar.gz` plus `<name>-<tag>.tar.gz.sha256`, and creates
  the release with `draft=false, prerelease=false`. It works for a
  resource-only extension; no `compozy extension build` generation is needed.
- `publish.go` (`ensureRelease`): the release payload carries only
  `tag_name`, `name`, `draft`, `prerelease` — no `target_commitish` — so if the
  tag does not exist GitHub creates it from the default branch, not from the
  workflow checkout. Publishing to a tag that already has a release PATCHes it
  in place and replaces only same-named assets; foreign `.tar.gz` assets
  remain and `selectReleaseDownload` then fails with "multiple .tar.gz
  assets". Publication is not atomic: create/PATCH release, delete
  conflicting assets, upload archive, upload sidecar are separate requests; a
  sidecar failure removes the archive but leaves the release.
- `internal/cli/extension.go` (`update`): `compozy extension update` re-runs
  the trust gate; an `unverified`-tier install (any `github:` install) needs
  `--allow-unverified --yes` to apply an update non-interactively.

## Scope

In scope:

1. Replace `.github/workflows/preview-release.yml` with `release.yml` built
   on `compozy extension publish`.
2. Rewrite `README.md` and `README.pt-BR.md`; add `docs/how-it-works.md` and
   `docs/verify.md`; rewrite `docs/releases/0.1.0-beta.2.md`.
3. Update the contract tests that pin the old workflow and documentation.
4. Republish `v0.1.0-beta.2` in the new format (manual, documented steps).
5. Add the `compozy-extension` GitHub topic.

Out of scope (separate specs): the commit-hash version guard
(`scripts/check-compozy-version.sh`), moving the dev loop to
`compozy extension dev`, relocating internal planning documents, curated
catalog submission, and any change to `agents/`, `loops/`, or
`resources/`. The five-file package inventory is unchanged.

## Release workflow

File: `.github/workflows/release.yml` (replaces `preview-release.yml`).

Trigger: `workflow_dispatch` with `release_ref` (full 40-hex commit SHA) and
`release_version` (unprefixed SemVer, `MAJOR.MINOR.PATCH-beta.N`). Concurrency
group `release-${{ github.repository }}`, `cancel-in-progress: false`.

Job `verify`: `uses: ./.github/workflows/ci.yml` with
`checkout_ref: release_ref`. Unchanged; it builds the pinned CompozyOS and
runs the contract suite.

Job `publish` (`needs: verify`, `permissions: contents: write`), steps:

1. Check out `release_ref` with `fetch-depth: 0`, `fetch-tags: true`,
   `persist-credentials: false`.
2. Check out and build the pinned CompozyOS exactly as `ci.yml` does, and put
   `bin/compozy` on `PATH`. (The `compozy` binary is needed for `publish` and
   the post-publish install check; `ci.yml` builds it in a different job, so
   this job builds it again. A later refactor may share it as an artifact.)
3. Preconditions, each a hard failure:
   - `git rev-parse HEAD == release_ref` and `release_ref` is a 40-hex SHA;
   - `git merge-base --is-ancestor release_ref origin/main`;
   - `extension.toml` `[extension].version == release_version`;
   - `docs/releases/<release_version>.md` exists and is a regular file;
   - tag `v<release_version>` does not exist on origin and no release with
     that tag exists (`gh release view` returns not found). The workflow never
     overwrites; republishing is a manual deletion followed by a new dispatch.
   Every `gh` step binds `GH_TOKEN: ${{ github.token }}` in its own `env`.
4. Create and push the annotated tag at `release_ref` (kept from the current
   workflow, same token-in-`extraheader` push): `git tag -a v<version> -m
   "Release v<version>" "$RELEASE_REF"` then push `refs/tags/v<version>`.
   `compozy extension publish` sends no `target_commitish`; creating the tag
   first is what pins the release to `release_ref` instead of the default
   branch head.
5. Stage: `mkdir "$RUNNER_TEMP/package"` (fails if it exists), assert it is an
   empty non-symlink directory, then `scripts/stage-extension.sh
   "$RUNNER_TEMP/package"` (existing script, five files).
6. Publish, with `GITHUB_TOKEN: ${{ github.token }}` in the step `env`:
   `compozy extension publish "$RUNNER_TEMP/package" --repository
   "$GITHUB_REPOSITORY" --tag "v$release_version" -o json`. Record
   `release_url`, `asset_url`, `digest_sha256` from the JSON output into the
   step summary.
7. Attach notes and title:
   `gh release edit "v$release_version" --title "Batuta $release_version"
   --notes-file "docs/releases/$release_version.md"`. The release keeps
   GitHub's default "Latest" designation: while Batuta has only beta
   releases, the unversioned install path resolves through
   `/releases/latest`, and this is what the check below asserts. When a
   stable line exists, betas get `--latest=false` (future change, out of
   scope).
8. Post-publish verification:
   - `git ls-remote --tags origin refs/tags/v<version>^{}` (peeled) equals
     `release_ref`; `gh release view v<version> --json isDraft,isPrerelease,
     assets` shows `isDraft=false`, `isPrerelease=false`, and exactly
     `batuta-v<version>.tar.gz` and `batuta-v<version>.tar.gz.sha256`.
   - Install check in the isolated daemon (`COMPOZY_HOME` under
     `RUNNER_TEMP`, started the same way `ci.yml` does), run twice — once with
     the explicit tag `github:$GITHUB_REPOSITORY@v<version>` and once
     unversioned `github:$GITHUB_REPOSITORY` (the README command) — each
     followed by: `compozy extension inventory batuta -o json` lists exactly
     `agent/batuta`, `skill/batuta-routing`, `loop/batuta-deliver`;
     `compozy extension provenance batuta -o json` reports `installed_from ==
     "github"`, `digest_matched == true`, and the installed version equals
     `release_version`; then `compozy extension remove batuta --global`.
     The daemon is stopped `if: always()`.

Removed from the old workflow: both `attest-build-provenance` steps, the
GraphQL preflight and postflight queries, the manual annotated tag push, the
draft-then-publish sequence, the download-and-recheck step,
`scripts/build-preview-assets.sh`, and `SHA256SUMS`.

Failure behaviour: any step failure stops the job. A failure before step 4
leaves no remote state. From step 4 on, remote state may be partial (tag
only; release without assets; release with archive and no sidecar; release
without notes). Recovery is one documented procedure for every case:
`gh release delete v<version> --cleanup-tag --yes` (tolerating "release not
found", then `git push origin :refs/tags/v<version>` if the tag survived),
then dispatch again. The "tag and release must not exist" precondition is
what makes a retry without cleanup fail loudly instead of publishing on top
of debris. `docs/releases/RELEASING.md` is not created; this procedure lives
in `CONTRIBUTING.md`.

## Assets and naming

The published assets are exactly `batuta-v<version>.tar.gz` and
`batuta-v<version>.tar.gz.sha256`, named by CompozyOS from the manifest name
and the tag. Release notes, README, and `docs/verify.md` use these names.

## Republishing v0.1.0-beta.2

Done once, by the maintainer, and recorded in the pull request description:

```bash
gh release delete v0.1.0-beta.2 --repo franciscpd/batuta-compozy --cleanup-tag --yes
gh workflow run release.yml --repo franciscpd/batuta-compozy \
  -f release_ref=<new main SHA> -f release_version=0.1.0-beta.2
```

The tag moves to the new `main` commit that contains this change (the
workflow pins it there in step 4). This is a deliberate history rewrite of a
preview release; the release notes say so in one sentence. Between the delete
and the new run's step 6, `v0.1.0-beta.2` is not installable; that gap is
accepted for a preview with no known external installs.

`extension.toml` keeps `version = "0.1.0-beta.2"`.

## Repository metadata

`gh repo edit franciscpd/batuta-compozy --add-topic compozy-extension` so
`compozy extension search batuta` finds the repository. Existing topics stay.

## README (EN and PT-BR, mirrored)

Both files carry the same sections in the same order; PT-BR is a
translation, not a different document. Target length: about 60 lines each.

1. Title, one-line language switch, then two sentences: Batuta is a conductor
   agent for CompozyOS that turns a conversation into a spec, tasks, and a
   delivery Loop, routing each task to the cheapest capable model; it never
   writes code. One line: independent community project, not official.
2. Flow diagram (fenced text, six to eight lines): conversation → `cy-create-spec`
   → `cy-create-tasks` → `batuta-deliver` (`implement-tasks` → `review-and-fix`)
   → terminal return to the same conversation.
3. Install:
   - Prerequisites, three bullets: CompozyOS `v0.3.0-beta.14` or later with the
     daemon running (verified on `v0.3.0-beta.16`); bundled `spec-cycle`
     extension enabled; at least one provider authenticated (`compozy provider
     models list` shows models).
   - The one-line install command. One sentence on what `--allow-unverified`
     means (community source, digest verified from the release sidecar) with a
     link to `docs/verify.md`.
   - Update: `compozy extension update batuta --allow-unverified --yes`
     (the same consent as install; a `github:` install stays in the
     `unverified` tier). Remove: `compozy extension remove batuta --global`.
4. Use: create a session with the `batuta` agent in your project workspace and
   describe the change. A short sample exchange (six to ten lines) drawn from
   the case study: request → auto-commit question → routing table
   confirmation → spec approval → dispatch → the return message with the
   terminal outcome. One sentence: routing comes from your live provider
   catalog and is stored per workspace.
5. Known limitations: the two upstream CompozyOS items (executor sessions not
   visually nested; they stay active/idle after completion), two bullets.
6. Learn more: links to `docs/how-it-works.md`, `docs/verify.md`,
   `docs/architecture.md`, the case study, release notes, `CONTRIBUTING.md`,
   `LICENSE`.

Removed from the README: the five-file inventory, the commit hash and
`check-compozy-version.sh` reference, the `gh release download` sequence, the
package store, lock, and content-addressing paragraphs, and the detailed gate
and preflight prose. That content lands in the documents below.

## New documents

`docs/how-it-works.md` (EN only): the operational contract currently in the
README's Usage and Routing sections — delivery preference gate and the exact
config key, bootstrap and routing derivation, PM phase and required
approvals, `import_tasks` preflight, dry-run, dispatch inputs, event-driven
terminal return, progress requests, escalation. Same facts, moved, lightly
tightened. It links to `agents/batuta/AGENT.md` and
`resources/skills/batuta-routing/SKILL.md` as the source of truth.

`docs/verify.md` (EN only): what `--allow-unverified` means in CompozyOS
(unverified registry tier; when the release carries a `.sha256` sidecar —
ours always does — the daemon checks the archive against it and records
`digest_matched: true`; `compozy extension provenance batuta` shows the
decision), and the manual path: download `batuta-v<version>.tar.gz` and its `.sha256`, check
with `sha256sum --check`, extract, `compozy extension validate`, install from
the directory. It also covers the local-development install
(`scripts/republish.sh`) in one short paragraph.

`docs/releases/0.1.0-beta.2.md` rewritten: scope and verified runtime
(unchanged facts), the one-line install, asset names, the sentence about the
republication, known limitations, links.

`CONTRIBUTING.md`: replace the `build-preview-assets.sh` check with the
release procedure (dispatch `release.yml`; the failed-run recovery above;
never edit a release by hand outside that procedure), and update the
validation list. Everything else stays.

## Contract tests

The suite convention stays (`tests/contract/test_NN_*.sh`, `run.sh`). Changes:

- Delete `test_07_preview_assets.sh` and `scripts/build-preview-assets.sh`.
- `test_07_workflow_contract.sh`: keep its `ci.yml` assertions unchanged;
  replace its `preview-release.yml` assertions with `release.yml` ones:
  dispatch inputs, `needs: verify`, `contents: write` only on the publish
  job, the precondition set, per-step `GH_TOKEN`/`GITHUB_TOKEN` bindings, the
  tag-at-`release_ref` step before publish, the exact `compozy extension
  publish` invocation, `gh release edit --notes-file`, the peeled-tag check,
  both install checks (explicit tag and unversioned), and the absence of
  `--prerelease`, `--draft`, `--latest=false`, `attest-build-provenance`,
  `SHA256SUMS`, `build-preview-assets.sh`, and `graphql`.
- Rewrite `test_07_preview_docs.sh` and `test_07_public_docs.sh` for the new
  documents: README EN/PT-BR and release notes contain the one-line install,
  `batuta-v0.1.0-beta.2.tar.gz`, `compozy extension update batuta`, the
  `docs/how-it-works.md` and `docs/verify.md` links; and do not contain
  `SHA256SUMS`, `gh release download`, `batuta-compozy_0.1.0-beta.2.tar.gz`,
  or a 40-hex commit hash. `docs/verify.md` contains the manual path and the
  sidecar name; `docs/how-it-works.md` contains the exact config key
  `loops.inputs.batuta-deliver.auto_commit`.
- `test_07_license.sh`: its archive-extraction check (built the old archive
  and extracted `LICENSE`) becomes a staged-tree check — run
  `scripts/stage-extension.sh` into a temp dir and assert `LICENSE` is
  present with the exact MIT text. The archive itself is now produced by
  `compozy extension publish`, and the CI install check proves the shipped
  tree. `test_07_case_study.sh`: update references to removed asset names;
  otherwise unchanged.

The workflow itself is exercised end to end by the beta.2 republication run,
which is the acceptance test for this change.

## Acceptance

- On a machine with CompozyOS `v0.3.0-beta.14+`,
  `compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes`
  installs Batuta; `inventory` lists the three resources; `provenance` shows
  `installed_from: github`, `digest_matched: true`.
- `compozy extension search batuta` lists the repository.
- `gh release view v0.1.0-beta.2` shows a non-draft, non-prerelease release
  whose tag peels to the dispatched `release_ref`, with exactly the two
  assets.
- README EN and PT-BR fit on one screen before the "Use" section, and the
  first code block a reader sees is the install command.
- `tests/contract/run.sh` passes; CI is green on `main`; the `release.yml`
  run for beta.2 is green including its install check.
