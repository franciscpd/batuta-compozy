# Contributing to Batuta

Batuta is an independent community project. Contributions need Bash, Python 3,
Git, GitHub CLI, Go 1.26.4, and a compatible CompozyOS checkout/runtime.
Work on an isolated feature branch or worktree so local experiments do not
overlap another change.

## Validate before opening a pull request

Run these repository checks:

```bash
go test -race ./... -count=1
go vet ./...
bash -n scripts/*.sh tests/contract/*.sh
python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v
tests/contract/run.sh
git diff --check
```

Run `tests/contract/run.sh` only from a disposable checkout with no
`.compozy/`. Preflight rejects and preserves any marker that already exists.
The suite registers a guarded external temporary workspace, then removes its
exact registration and root during cleanup; any repository marker remains
foreign state and is preserved. Contract ownership spans the
`test_00_*` through `test_07_*` families; update the owning contract when
changing its public behavior.

Beta.5 is code-backed: staging contains production Go source and resources,
then `compozy extension build` produces the only directory that may be
validated or installed. `scripts/republish.sh` automates that path (see
`docs/verify.md`) with the official beta.21 SDK. Do not add a local `replace`,
pseudo-version, or fork-only dependency to make release packaging appear
reproducible. Use a compatible preview binary only for the child-override lab
until that generic Compozy contract is officially released.

Design specs and implementation plans live in `docs/internal/specs` and
`docs/internal/plans`. New ones go there. Nothing under `docs/internal/` is
part of the extension package or the public guides.

## Releasing

Releases are published only by `.github/workflows/release.yml`
(`gh workflow run release.yml -f release_ref=$(git rev-parse origin/main)
-f release_version=<X.Y.Z-beta.N>`). It reruns CI on that commit, creates the
annotated tag at that commit, publishes with `compozy extension publish`,
attaches `docs/releases/<version>.md` as notes, and proves the result by
installing from GitHub in an isolated daemon. Before dispatching, bump
`extension.toml` and add `docs/releases/<version>.md` on `main`.
`release_ref` must be the current `main` head at dispatch time (the workflow
asserts `GITHUB_SHA == release_ref`); an older ancestor is rejected. The
unversioned install check resolves GitHub's latest full release, so dispatch
releases in version order.

If a run fails after the tag step, remote state may be partial. Recovery is
always the same: `gh release delete v<version> --cleanup-tag --yes` (ignore
"release not found"), then `git push origin :refs/tags/v<version>` if the tag
survived, then dispatch again. Never edit a release by hand outside this
procedure.

## Change and review workflow

Use conventional commits matching:

```text
^(build|ci|docs|feat|fix|perf|refactor|test): [a-z].+$
```

Do not mutate a release directly outside `.github/workflows/release.yml` and
the recovery procedure above. A pull request must include focused RED/GREEN
evidence, aggregate contract results, and visual evidence only when behavior
changes.
