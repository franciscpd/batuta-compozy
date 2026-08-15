# Batuta preview release automation design

Date: 2026-08-15

## Goal

Publish Batuta preview releases from a dedicated public GitHub repository with
a small, auditable workflow. The first release is `0.1.0-beta.2`. Publication
must be manually authorized, reproducible, and verifiable without requiring
maintainers to assemble or upload assets by hand.

## Scope

This iteration creates `franciscpd/batuta-compozy`, makes the current
`feat/batuta-reliability` history its `main` branch, and adds two workflows:

- pull-request and `main` CI;
- manually dispatched preview publication.

Automatic release pull requests, automatic version selection, stable-channel
publication, catalog submission, and tag-triggered publication are deferred.

## Repository initialization

Create an empty public repository with GitHub CLI, add it as the candidate
worktree's `origin`, and push only the candidate HEAD to remote `main`. Do not
push the unrelated local `main` branch or the untracked `.compozy` workspace
state.

The repository description identifies Batuta as a CompozyOS orchestration
extension. Issues remain enabled. The default branch is `main`.

## Continuous integration

`.github/workflows/ci.yml` runs on pull requests to `main`, pushes to `main`,
and manual dispatch. It has read-only repository permissions and cancelable
per-ref concurrency.

The job performs:

1. Bash syntax validation for scripts and contract tests.
2. Python event-validator unit tests.
3. The complete contract runner in its supported CI environment.
4. Two independent package builds whose content-addressed directories and
   file digests must match.
5. Exact package inventory validation: the manifest plus one agent, one skill,
   and one Loop.
6. Manifest and extension validation against the pinned compatible CompozyOS
   runtime.
7. `git diff --check` and repository cleanliness checks that allow no generated
   tracked changes.

The compatible CompozyOS source is pinned to full commit
`a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c` until an official release includes
that commit. CI must fetch or build exactly that revision and verify its
reported commit before using it. A later change may replace the commit pin with
an official immutable release tag and checksum.

## Preview publication

`.github/workflows/preview-release.yml` is started only with
`workflow_dispatch`. It accepts:

- `release_ref`: a full commit SHA;
- `release_version`: strict unprefixed prerelease SemVer.

The first invocation is equivalent to:

```bash
gh workflow run preview-release.yml \
  --repo franciscpd/batuta-compozy \
  -f release_ref=<full-main-commit> \
  -f release_version=0.1.0-beta.2
```

The workflow uses non-cancelable release concurrency. It fails before any
GitHub mutation unless all of these conditions hold:

- the resolved checkout HEAD equals `release_ref`;
- the workflow event SHA equals `release_ref`, binding provenance to the same
  source commit;
- the commit is reachable from remote `main`;
- `release_version` is a prerelease and matches `extension.toml` exactly;
- the matching tag and GitHub Release do not exist;
- CI-equivalent verification passes;
- two package builds are byte-equivalent at the file level;
- the package contains only the four expected files.

After the gates pass, the workflow creates a deterministic archive named
`batuta-compozy_<version>.tar.gz`, generates `SHA256SUMS`, and generates GitHub
artifact provenance for both files. It then:

1. creates and pushes an annotated `v<version>` tag at the validated commit;
2. stages a draft GitHub Release;
3. uploads the archive and checksum file;
4. downloads the assets into a fresh directory;
5. verifies names, sizes, and SHA-256 values;
6. publishes the draft as a prerelease with `latest=false`;
7. verifies the published release metadata and asset inventory.

Publication uses the repository `GITHUB_TOKEN`; no personal token or custom
secret is required. Job permissions stay read-only until the publication job,
which receives only `contents: write`, `id-token: write`, and
`attestations: write`.

## Release notes and compatibility

Preview notes are maintained as a reviewed repository file rendered for the
exact version. They include:

- the spec-cycle migration and supported flow;
- installation and checksum-verification commands;
- the exact compatible CompozyOS boundary;
- the known upstream limitations that Loop-created executor sessions are not
  visually nested and remain active/idle after normal terminal completion;
- the preview support expectation and rollback/removal command.

Until a compatible official CompozyOS release exists, the notes must state that
users need commit `a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c` or a later release that
contains it. The workflow fails if this compatibility statement is absent.

## Security and failure handling

Third-party actions are pinned to full commit SHAs. Default token permissions
are read-only. Shell steps use strict mode, validate paths before cleanup, and
never consume the repository's `.compozy` directory.

The workflow follows a stage-then-publish boundary. A failure before the
annotated tag step leaves no remote mutation. A failure after the tag is pushed
may leave the exact tag, or the tag plus a draft, for inspection and explicit
operator recovery; the workflow does not delete or overwrite either
automatically. It never publishes a partially verified release. Re-running
with an existing tag or release fails closed rather than overwriting assets.

## Verification and acceptance

Before the first public preview:

1. Run both workflows on the new remote repository.
2. Confirm CI passes on the exact release commit.
3. Dispatch the preview workflow through `gh workflow run`.
4. Watch it with `gh run watch --exit-status`.
5. Verify `gh release view` reports prerelease true, draft false, the expected
   tag, and exactly two assets; verify latest false through GitHub's GraphQL
   release metadata because that field is not exposed by the CLI command.
6. Download the archive and `SHA256SUMS` with `gh release download` and verify
   the checksum locally.
7. Verify the GitHub attestation with `gh attestation verify` or the release
   verification command supported by the installed GitHub CLI.
8. Extract the archive, run `compozy extension validate`, and confirm the exact
   three-resource inventory in an isolated Compozy home.

The preview is accepted only when the remote workflow, local download check,
and isolated extension validation all pass. No automatic installation into the
operator's live Compozy home is part of publication.
