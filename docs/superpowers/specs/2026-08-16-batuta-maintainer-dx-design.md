# Batuta maintainer DX design: version guard, dev loop, internal docs

Date: 2026-08-16

## Goal

Make the maintainer's side of Batuta as small as the user's side became in
the native-install change: a version guard that states a semver floor
instead of a hash table, a local republish that is one short script without
locks or a package store, and internal planning documents out of the
public reader's path.

## Why

- `scripts/check-compozy-version.sh` (158 lines) accepts only commits it
  lists by hash. Every new CompozyOS build needs a table edit, and the
  maintainer's own `main` builds are rejected. The README already states
  the real contract — `v0.3.0-beta.14` or later — so the script should
  enforce that and nothing more.
- `scripts/republish.sh` + `package-extension.sh` + `with-batuta-lock.py`
  (≈330 lines) implement a per-user lock, a content-addressed read-only
  package store, and re-verification of package bytes — for copying five
  files into a temp dir and running `compozy extension install`. Five
  contract tests exist only to test that machinery. `compozy extension dev`
  cannot replace it: `internal/extension/build_toolchain.go:49` requires
  `package.json` or `go.mod`, so a resource-only extension has no `dev`
  path today (upstream follow-up, out of scope).
- `docs/superpowers/{specs,plans}` (12 files, thousands of lines) sit
  beside the public docs and dominate `docs/` for a newcomer.

## Scope

In scope:

1. Rewrite `scripts/check-compozy-version.sh` to a semver floor policy.
2. Rewrite `scripts/republish.sh`; delete `scripts/package-extension.sh`,
   `scripts/with-batuta-lock.py`, and the five package/lock contract tests;
   add one republish contract test.
3. Move `docs/superpowers/` to `docs/internal/`; update tests, CONTRIBUTING,
   and add a root `CLAUDE.md` declaring the spec/plan location.
4. Update `docs/verify.md` (local development paragraph) and `README` only
   if a touched string appears there (it does not today).

Out of scope: any change to `agents/`, `loops/`, `resources/`,
`extension.toml` (stays `version = "0.1.0-beta.2"`,
`min_compozy_version = "0.3.0-beta.13"` — raising the manifest floor is a
package change and belongs to the next functional release), the release
workflow, an upstream `dev`-for-resource-only contribution (recorded as a
follow-up), and any GitHub release action.

## Part A — version guard

File: `scripts/check-compozy-version.sh` (full rewrite, Bash + one
embedded Python block, target ≤ 60 lines).

Interface (unchanged): no arguments → run `compozy version -o json` from a
fresh `mktemp -d /tmp/batuta-version.XXXXXX` cwd, removed on exit (the
neutral-cwd behavior `test_00_version_neutral_cwd.sh` pins); or exactly
`--version V --commit C` for tests. Anything else → usage, exit 2.

Policy, evaluated on `V`:

- Parse `^v?(\d+)\.(\d+)\.(\d+)(?:-beta\.(\d+))?(?:-(\d+)-g([0-9a-fA-F]+))?$`.
  Unparseable → reject.
- Floor is `(0,3,0,beta 14)`. Ordering: compare `(major, minor, patch)`;
  a version with no `-beta.N` ranks above every beta of the same triple;
  betas compare by `N`.
- Release (no post-tag suffix) with base ≥ floor → print
  `OK: CompozyOS <V> satisfies Batuta's floor v0.3.0-beta.14`, exit 0.
- Post-tag build (`-COUNT-gHASH`) with base ≥ floor → print
  `WARN: CompozyOS <V> (<C>) is a custom post-tag build; Batuta is verified
  on v0.3.0-beta.16` to stderr and `OK: …` to stdout, exit 0.
- Otherwise → print
  `incompatible CompozyOS <V> (<C>): Batuta requires v0.3.0-beta.14 or later`
  to stderr, exit 1.

`C` (commit) is only echoed in messages; it never affects the decision.
The `official_beta13` and `trusted_post_tag_builds` tables disappear.

Docs: `extension.toml`'s comment already says the script enforces the
operational floor — unchanged. `docs/architecture.md` "Trust and
compatibility" paragraph gains no new text; the sentence naming
`0.3.0-beta.13` as a grammar floor stays true.

Tests:

- `tests/contract/test_00_runtime_guard.sh` rewritten as a table:
  reject `v0.3.0-beta.13 tag`, `v0.3.0-beta.13-14-g36bd8156 36bd8156`,
  `v0.2.9 x`, `garbage x`; accept `v0.3.0-beta.14 x`, `v0.3.0 x`,
  `v0.4.0-beta.1 x`, `v1.0.0 x`, `0.3.0-beta.14 x` (no `v`); accept-with-
  warning `v0.3.0-beta.16-9-ga35eda6d a35eda6d` (stderr contains
  `custom post-tag build`, stdout starts with `OK:`). Reject messages must
  contain `v0.3.0-beta.14`.
- `tests/contract/test_00_version_neutral_cwd.sh`: unchanged except the
  fake's version string becomes `v0.3.0-beta.16-9-ga35eda6d` (still
  accepted).
- `tests/contract/test_00_republish_guard.sh`: unchanged (fake reports
  `v0.3.0-beta.13`, republish must fail before `extension remove`).

## Part B — dev loop

File: `scripts/republish.sh` (full rewrite, ≈25 lines):

```bash
#!/usr/bin/env bash
# Local dev install: stage the five package files, validate, reinstall, enable, verify.
set -euo pipefail
ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
cd "$ROOT"

scripts/check-compozy-version.sh >/dev/null

STAGE=$(mktemp -d /tmp/batuta-republish.XXXXXX)
cleanup() { case "$STAGE" in /tmp/batuta-republish.*) rm -rf -- "$STAGE" ;; esac; }
trap cleanup EXIT
scripts/stage-extension.sh "$STAGE"

compozy extension validate "$STAGE" -o json | python3 -c "<error-severity assert, verbatim from today>"
if compozy extension list -o json | python3 -c '<same "batuta installed?" check>'; then
  compozy extension remove batuta --global -o json >/dev/null
fi
compozy extension install "$STAGE" --allow-unverified --yes -o json >/dev/null
compozy extension enable batuta -o json | python3 -c '<same state==active assert>'
compozy extension inventory batuta -o json | python3 -c '<same exact-inventory + live assert>'
```

Order is a contract: guard → stage → validate → (remove) → install →
enable → inventory. Any failure before `remove` leaves the installed
extension untouched.

Deleted: `scripts/package-extension.sh`, `scripts/with-batuta-lock.py`,
`tests/contract/test_01_package.sh`, `test_01_package_lock.sh`,
`test_01_republish_adulteration.sh`, `test_01_republish_global_lock.sh`,
`test_01_republish_package.sh`.

Added: `tests/contract/test_01_republish.sh` — fake `compozy` on `PATH`
logging every call (same pattern as today's `test_01_republish_package.sh`:
`version` returns `v0.3.0-beta.16-9-ga35eda6d`, `validate` returns
`{"issues":[]}`, `list` returns `[{"name":"batuta"}]`, `install`/`remove`
return `{}`, `enable` returns active, `inventory` returns the three live
items). Asserts: call order exactly `version`, `extension validate <S>`,
`extension list`, `extension remove batuta --global`, `extension install <S>
--allow-unverified --yes`, `extension enable batuta`, `extension inventory
batuta`; `<S>` is the same `/tmp/batuta-republish.*` path in validate and
install; `<S>` no longer exists after the script exits; the staged tree seen
by the fake at install time had exactly the five package files (the fake
records `find "$2" -type f | sort` into a side file on `install`).

Modified: `tests/contract/test_07_license.sh` drops the content-addressed
block (`package_root=…` through `assert_inventory "$package_dir"
content-addressed`); final message becomes `OK: MIT license is exact and
preserved in the staged package tree`. `test_01_stage.sh`,
`test_01_validate.sh`, `test_00_republish_guard.sh` stay.

`docs/verify.md` "Local development install" becomes:

> From a checkout, `scripts/republish.sh` checks the CompozyOS version,
> stages the five package files into a temporary directory, validates them,
> reinstalls and enables the extension, and checks the live inventory.
> See `CONTRIBUTING.md`.

Ruling recorded here: the lock and package store guarded against
concurrent republishes from several worktrees and against a tampered
retained package. Batuta has one maintainer and the staged tree is
temporary; that protection is not worth 300 lines and five tests.

## Part C — internal docs

- `git mv docs/superpowers docs/internal` (both `specs/` and `plans/`,
  including this spec and its plan once written).
- `tests/contract/test_07_preview_docs.sh`: the three `docs/superpowers/…`
  paths become `docs/internal/…`; assertions unchanged.
- `CONTRIBUTING.md`, in the validation section, add:
  > Design specs and implementation plans live in `docs/internal/specs`
  > and `docs/internal/plans`. New ones go there. Nothing under
  > `docs/internal/` is part of the extension package or the public guides.
- New root `CLAUDE.md`:
  ```
  # batuta-compozy
  - Design specs go in `docs/internal/specs/`, implementation plans in
    `docs/internal/plans/` (not `docs/superpowers/`).
  - Never run `tests/contract/run.sh` from a checkout that has `.compozy/`;
    use a disposable detached worktree.
  ```
- `tests/contract/test_07_public_docs.sh` gains `require_text
  CONTRIBUTING.md 'docs/internal/specs'` and a check that
  `docs/superpowers` no longer exists.

## Follow-up (not in scope)

Upstream issue for CompozyOS: let `compozy extension dev` and `build`
accept a resource-only extension (no `package.json`/`go.mod`) by using
`extension.toml` and the declared resource directories directly, so
resource-only authors get the workspace overlay and `--watch` loop.

## Acceptance

- `scripts/check-compozy-version.sh` passes on the maintainer's current
  build (`v0.3.0-beta.16-9-ga35eda6d`) with a warning and rejects
  `v0.3.0-beta.13`.
- `scripts/republish.sh` on the maintainer's machine reinstalls and
  enables Batuta with the three resources live; no files under
  `~/.local/share/batuta-compozy/` or `~/.compozy/locks/` are created.
- `ls docs/superpowers` fails; `docs/internal/{specs,plans}` exist.
- `tests/contract/run.sh` passes from a disposable checkout; CI green on
  `main`.
