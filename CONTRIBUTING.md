# Contributing to Batuta

Batuta is an independent community project. Contributions need Bash, Python 3,
Git, GitHub CLI, Go 1.26.4, and a compatible CompozyOS checkout/runtime.
Work on an isolated feature branch or worktree so local experiments do not
overlap another change.

## Validate before opening a pull request

Run these repository checks:

```bash
bash -n scripts/*.sh tests/contract/*.sh
python3 -m unittest discover -s tests/e2e -p 'test_*.py' -v
tests/contract/run.sh
git diff --check
```

Check preview assets deterministically with:

```bash
empty_output_directory=$(mktemp -d /tmp/batuta-contributor-assets.XXXXXX)
scripts/build-preview-assets.sh 0.1.0-beta.2 "$empty_output_directory"
```

Run `tests/contract/run.sh` only from a disposable checkout with no
`.compozy/`. Its cleanup removes a minimal `.compozy/workspace.toml` marker
when one is present, including after preflight rejection; never run it in a
registered operator checkout. Contract ownership spans the `test_00_*` through
`test_07_*` families; update the owning contract when changing its public
behavior.

## Change and review workflow

Use conventional commits matching:

```text
^(build|ci|docs|feat|fix|perf|refactor|test): [a-z].+$
```

Do not mutate a release directly outside
`.github/workflows/preview-release.yml`. A pull request must include focused
RED/GREEN evidence, aggregate contract results, a deterministic asset digest,
and visual evidence only when behavior changes.
