# Batuta Maintainer DX Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Replace the hash-table version guard with a semver floor, shrink the local republish to one short script, and move internal planning docs to `docs/internal/`.

**Architecture:** Three independent parts. (A) `scripts/check-compozy-version.sh` keeps its CLI contract (`no args` → live `compozy version -o json` from a neutral temp cwd; `--version V --commit C` for tests) but decides by parsing the version against floor `v0.3.0-beta.14`. (B) `scripts/republish.sh` becomes guard → stage into `mktemp` → validate → (remove) → install → enable → inventory; the package store, lock helper, and their five tests are deleted and one call-order contract test is added. (C) `git mv docs/superpowers docs/internal`, tests/CONTRIBUTING/CLAUDE.md updated.

**Tech Stack:** Bash, Python 3 (embedded via `python3 -`), the existing shell contract-test suite (`tests/contract/test_*.sh`, run individually; `run.sh` only from a disposable checkout).

**Spec:** `docs/superpowers/specs/2026-08-16-batuta-maintainer-dx-design.md` (moves to `docs/internal/specs/` in Task 3)

## Global Constraints

- Floor is `v0.3.0-beta.14`; verified build named in messages is `v0.3.0-beta.16`. Post-tag builds (`-COUNT-gHASH`) with base ≥ floor are accepted with a stderr `WARN` line containing `custom post-tag build`. Reject messages contain `v0.3.0-beta.14`.
- `--commit` never affects the decision.
- `extension.toml` unchanged (`version = "0.1.0-beta.2"`, `min_compozy_version = "0.3.0-beta.13"`). No change under `agents/`, `loops/`, `resources/`.
- `scripts/republish.sh` order: guard → stage → validate → (remove only if installed) → install → enable → inventory. Any failure before `remove` leaves the installed extension untouched. Staging dir is `mktemp -d /tmp/batuta-republish.XXXXXX`, removed on exit.
- Package inventory: `LICENSE`, `extension.toml`, `agents/batuta/AGENT.md`, `resources/skills/batuta-routing/SKILL.md`, `loops/batuta-deliver/loop.yaml`.
- Never run `tests/contract/run.sh` in the working checkout while `.compozy/` exists; individual `test_*.sh` scripts that use a fake `compozy` on `PATH` (`test_00_runtime_guard.sh`, `test_00_version_neutral_cwd.sh`, `test_00_republish_guard.sh`, `test_01_republish.sh`) and static doc tests are safe. `test_00_runtime_guard.sh`'s last section calls the real guard (`"$GUARD" >/dev/null`) against the local daemon binary — that is fine on the maintainer machine (`v0.3.0-beta.16-9-ga35eda6d`).
- Conventional commits: `^(build|ci|docs|feat|fix|perf|refactor|test): [a-z].+$`, ending with `Co-Authored-By: Claude Fable 5 <noreply@anthropic.com>`.
- Never touch anything on GitHub (no push, no release).

---

### Task 1: Semver version guard

**Files:**
- Modify: `scripts/check-compozy-version.sh` (full rewrite)
- Modify: `tests/contract/test_00_runtime_guard.sh` (full rewrite), `tests/contract/test_00_version_neutral_cwd.sh:14` (fake version string)

**Interfaces:**
- Produces: `scripts/check-compozy-version.sh [--version V --commit C]` — exit 0 with stdout `OK: …` (and optional stderr `WARN: …`), exit 1 with stderr `incompatible CompozyOS …`, exit 2 on usage error. Consumed unchanged by `scripts/republish.sh` (Task 2) and `test_00_republish_guard.sh`.

- [ ] **Step 1: Rewrite `tests/contract/test_00_runtime_guard.sh` (RED)**

```bash
#!/usr/bin/env bash
set -euo pipefail
cd "$(dirname "$0")/../.."

GUARD=scripts/check-compozy-version.sh

expect_reject() {
  local version=$1 commit=$2 out
  if out=$("$GUARD" --version "$version" --commit "$commit" 2>&1); then
    printf 'expected %s to be rejected\n' "$version" >&2
    return 1
  fi
  case "$out" in
    *"incompatible CompozyOS"*"v0.3.0-beta.14"*) ;;
    *)
      printf 'reject message must name the floor: %s\n' "$out" >&2
      return 1
      ;;
  esac
}

expect_accept() {
  local version=$1 commit=$2 out err
  err=$(mktemp)
  if ! out=$("$GUARD" --version "$version" --commit "$commit" 2>"$err"); then
    printf 'expected %s to be accepted: %s\n' "$version" "$(cat "$err")" >&2
    rm -f "$err"
    return 1
  fi
  case "$out" in
    "OK: "*) ;;
    *)
      printf 'accept output must start with OK: %s\n' "$out" >&2
      rm -f "$err"
      return 1
      ;;
  esac
  if [[ -s $err ]]; then
    printf 'release build must not warn: %s\n' "$(cat "$err")" >&2
    rm -f "$err"
    return 1
  fi
  rm -f "$err"
}

expect_accept_with_warning() {
  local version=$1 commit=$2 out err
  err=$(mktemp)
  if ! out=$("$GUARD" --version "$version" --commit "$commit" 2>"$err"); then
    printf 'expected %s to be accepted with warning: %s\n' "$version" "$(cat "$err")" >&2
    rm -f "$err"
    return 1
  fi
  case "$out" in
    "OK: "*) ;;
    *)
      printf 'accept output must start with OK: %s\n' "$out" >&2
      rm -f "$err"
      return 1
      ;;
  esac
  if ! grep -q 'custom post-tag build' "$err"; then
    printf 'post-tag build must warn on stderr: %s\n' "$(cat "$err")" >&2
    rm -f "$err"
    return 1
  fi
  rm -f "$err"
}

expect_reject "v0.3.0-beta.13" "tag"
expect_reject "v0.3.0-beta.13-14-g36bd8156" "36bd8156"
expect_reject "v0.3.0-beta.13-5-g594d9fdf" "594d9fdf"
expect_reject "v0.2.9" "x"
expect_reject "v0.2.9-beta.99" "x"
expect_reject "garbage" "x"
expect_reject "" "x"

expect_accept "v0.3.0-beta.14" "x"
expect_accept "0.3.0-beta.14" "x"
expect_accept "v0.3.0-beta.16" "x"
expect_accept "v0.3.0" "x"
expect_accept "v0.3.1-beta.1" "x"
expect_accept "v0.4.0-beta.1" "x"
expect_accept "v1.0.0" "x"

expect_accept_with_warning "v0.3.0-beta.14-1-gabcdef12" "abcdef12"
expect_accept_with_warning "v0.3.0-beta.16-9-ga35eda6d" "a35eda6d"
expect_accept_with_warning "v0.3.0-beta.16-9-ga35eda6d" "a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c"
expect_accept_with_warning "v0.3.0-9-gdeadbeef" "deadbeef"

if "$GUARD" --version "v0.3.0-beta.14" >/dev/null 2>&1; then
  printf 'guard accepted a malformed argument list\n' >&2
  exit 1
fi

repo_had_compozy=false
if [[ -e .compozy || -L .compozy ]]; then
  repo_had_compozy=true
fi
"$GUARD" >/dev/null 2>&1
if [[ $repo_had_compozy == false && ( -e .compozy || -L .compozy ) ]]; then
  printf 'version guard generated .compozy in the repository\n' >&2
  exit 1
fi
printf 'OK: runtime guard enforces the v0.3.0-beta.14 semver floor\n'
```

Also in `tests/contract/test_00_version_neutral_cwd.sh` change the fake's JSON line to:

```bash
printf '%s\n' '{"Version":"v0.3.0-beta.16-9-ga35eda6d","Commit":"a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c","BuildDate":"test"}'
```

- [ ] **Step 2: Run to verify it fails**

Run: `tests/contract/test_00_runtime_guard.sh`
Expected: FAIL — first line `reject message must name the floor: incompatible CompozyOS v0.3.0-beta.13 (tag): Batuta accepts beta.13 post-tag builds only…` (old message lacks `v0.3.0-beta.14`).

- [ ] **Step 3: Rewrite `scripts/check-compozy-version.sh`**

```bash
#!/usr/bin/env bash
# Batuta compatibility guard: CompozyOS must be v0.3.0-beta.14 or later.
set -euo pipefail

usage() {
  printf 'usage: %s [--version VERSION --commit COMMIT]\n' "$0" >&2
}

NEUTRAL_CWD=""
cleanup() {
  if [[ -n $NEUTRAL_CWD ]]; then
    case "$NEUTRAL_CWD" in
      /tmp/batuta-version.*) rm -rf -- "$NEUTRAL_CWD" ;;
      *)
        printf 'refusing to clean unexpected version cwd: %s\n' "$NEUTRAL_CWD" >&2
        return 1
        ;;
    esac
  fi
}
trap cleanup EXIT

if [[ $# -eq 0 ]]; then
  NEUTRAL_CWD=$(mktemp -d /tmp/batuta-version.XXXXXX)
  version_json=$(cd "$NEUTRAL_CWD" && compozy version -o json)
  build=$(python3 -c '
import json
import sys

data = json.load(sys.stdin)
version = data.get("Version")
commit = data.get("Commit")
if not isinstance(version, str) or not version:
    raise SystemExit("compozy version JSON is missing Version")
if not isinstance(commit, str) or not commit:
    raise SystemExit("compozy version JSON is missing Commit")
print(version + "\t" + commit)
' <<<"$version_json")
  IFS=$'\t' read -r version commit <<< "$build"
elif [[ $# -eq 4 && $1 == --version && $3 == --commit ]]; then
  version=$2
  commit=$4
else
  usage
  exit 2
fi

python3 - "$version" "$commit" <<'PY'
import re
import sys

version, commit = sys.argv[1:]
FLOOR = (0, 3, 0, 14)          # v0.3.0-beta.14
FLOOR_TEXT = "v0.3.0-beta.14"
VERIFIED_TEXT = "v0.3.0-beta.16"

match = re.fullmatch(
    r"v?(\d+)\.(\d+)\.(\d+)(?:-beta\.(\d+))?(?:-(\d+)-g([0-9a-fA-F]+))?",
    version.strip(),
)


def rank(major, minor, patch, beta):
    # A release without -beta.N ranks above every beta of the same triple.
    return (major, minor, patch, float("inf") if beta is None else beta)


compatible = False
post_tag = False
if match:
    major, minor, patch, beta, count, _ = match.groups()
    beta_number = int(beta) if beta is not None else None
    post_tag = count is not None
    compatible = rank(int(major), int(minor), int(patch), beta_number) >= rank(*FLOOR)

if not compatible:
    raise SystemExit(
        f"incompatible CompozyOS {version} ({commit}): Batuta requires "
        f"{FLOOR_TEXT} or later"
    )

if post_tag:
    print(
        f"WARN: CompozyOS {version} ({commit}) is a custom post-tag build; "
        f"Batuta is verified on {VERIFIED_TEXT}",
        file=sys.stderr,
    )
print(f"OK: CompozyOS {version} satisfies Batuta's floor {FLOOR_TEXT}")
PY
```

- [ ] **Step 4: Run to verify it passes**

Run: `tests/contract/test_00_runtime_guard.sh && tests/contract/test_00_version_neutral_cwd.sh && tests/contract/test_00_republish_guard.sh && bash -n scripts/*.sh tests/contract/*.sh`
Expected: three `OK:` lines (`runtime guard enforces the v0.3.0-beta.14 semver floor`, `real version query uses and cleans a neutral cwd`, `republish falha antes de remover em runtime incompativel`).

- [ ] **Step 5: Commit**

```bash
git add scripts/check-compozy-version.sh tests/contract/test_00_runtime_guard.sh tests/contract/test_00_version_neutral_cwd.sh
git commit -m "fix: enforce a semver floor in the compozy version guard"
```

---

### Task 2: One-script republish

**Files:**
- Modify: `scripts/republish.sh` (full rewrite), `tests/contract/test_07_license.sh:200-207,220`, `docs/verify.md:62-68`
- Delete: `scripts/package-extension.sh`, `scripts/with-batuta-lock.py`, `tests/contract/test_01_package.sh`, `tests/contract/test_01_package_lock.sh`, `tests/contract/test_01_republish_adulteration.sh`, `tests/contract/test_01_republish_global_lock.sh`, `tests/contract/test_01_republish_package.sh`
- Create: `tests/contract/test_01_republish.sh`

**Interfaces:**
- Consumes: `scripts/check-compozy-version.sh` (Task 1; exit 0/1), `scripts/stage-extension.sh <empty-dir>` (unchanged).
- Produces: `scripts/republish.sh` with the fixed call order (see Global Constraints).

- [ ] **Step 1: Write `tests/contract/test_01_republish.sh` (RED)**

```bash
#!/usr/bin/env bash
# republish.sh contract: guard -> stage -> validate -> remove -> install -> enable -> inventory, temp staging removed.
set -euo pipefail
cd "$(dirname "$0")/../.."

TMP=$(mktemp -d /tmp/batuta-republish-test.XXXXXX)
LOG="$TMP/calls"
TREE="$TMP/installed-tree"
cleanup() {
  case "$TMP" in
    /tmp/batuta-republish-test.*) rm -rf -- "$TMP" ;;
  esac
}
trap cleanup EXIT

cat > "$TMP/compozy" <<'SH'
#!/usr/bin/env bash
printf '%s\n' "$*" >> "$BATUTA_FAKE_LOG"
case "$*" in
  "version -o json")
    printf '%s\n' '{"Version":"v0.3.0-beta.16-9-ga35eda6d","Commit":"a35eda6d3a2ec47995c19a14a5a01d4f9452cf1c","BuildDate":"test"}'
    ;;
  "extension validate "*)
    printf '%s\n' '{"issues":[]}'
    ;;
  "extension list -o json")
    printf '%s\n' '[{"name":"batuta","version":"0.1.0-beta.2","state":"active"}]'
    ;;
  "extension remove batuta --global -o json")
    printf '%s\n' '{"status":"removed"}'
    ;;
  "extension install "*" --allow-unverified --yes -o json")
    (cd "$2" && find . -type f | LC_ALL=C sort) > "$BATUTA_FAKE_TREE"
    printf '%s\n' '{}'
    ;;
  "extension enable batuta -o json")
    printf '%s\n' '{"extension":{"state":"active"}}'
    ;;
  "extension inventory batuta -o json")
    printf '%s\n' '{"items":[{"kind":"agent","name":"batuta","live":true},{"kind":"loop","name":"batuta-deliver","live":true},{"kind":"skill","name":"batuta-routing","live":true}]}'
    ;;
  *)
    exit 99
    ;;
esac
SH
chmod +x "$TMP/compozy"

BATUTA_FAKE_LOG="$LOG" BATUTA_FAKE_TREE="$TREE" PATH="$TMP:$PATH" \
  scripts/republish.sh >/dev/null

mapfile -t calls < "$LOG"
[[ ${#calls[@]} -eq 7 ]] || {
  printf 'expected exactly 7 compozy calls, got %d:\n%s\n' "${#calls[@]}" "$(cat "$LOG")" >&2
  exit 1
}
[[ ${calls[0]} == "version -o json" ]]
[[ ${calls[1]} == "extension validate "*" -o json" ]]
[[ ${calls[2]} == "extension list -o json" ]]
[[ ${calls[3]} == "extension remove batuta --global -o json" ]]
[[ ${calls[4]} == "extension install "*" --allow-unverified --yes -o json" ]]
[[ ${calls[5]} == "extension enable batuta -o json" ]]
[[ ${calls[6]} == "extension inventory batuta -o json" ]]

validate_path=${calls[1]#extension validate }
validate_path=${validate_path% -o json}
install_path=${calls[4]#extension install }
install_path=${install_path% --allow-unverified --yes -o json}
[[ $validate_path == "$install_path" ]] || {
  printf 'validate and install used different staging paths: %s vs %s\n' \
    "$validate_path" "$install_path" >&2
  exit 1
}
case "$install_path" in
  /tmp/batuta-republish.*) ;;
  *)
    printf 'staging path is not a guarded temp dir: %s\n' "$install_path" >&2
    exit 1
    ;;
esac
if [[ -e $install_path ]]; then
  printf 'republish left its staging directory behind: %s\n' "$install_path" >&2
  exit 1
fi

expected_tree=$(printf '%s\n' \
  './LICENSE' \
  './agents/batuta/AGENT.md' \
  './extension.toml' \
  './loops/batuta-deliver/loop.yaml' \
  './resources/skills/batuta-routing/SKILL.md')
if [[ $(cat "$TREE") != "$expected_tree" ]]; then
  printf 'installed staging tree mismatch:\n%s\n' "$(cat "$TREE")" >&2
  exit 1
fi

# When batuta is not installed, remove must not be called.
: > "$LOG"
sed -i 's/\[{"name":"batuta","version":"0.1.0-beta.2","state":"active"}\]/[]/' "$TMP/compozy"
BATUTA_FAKE_LOG="$LOG" BATUTA_FAKE_TREE="$TREE" PATH="$TMP:$PATH" \
  scripts/republish.sh >/dev/null
if grep -q '^extension remove' "$LOG"; then
  printf 'republish removed an extension that was not installed\n' >&2
  exit 1
fi
[[ $(wc -l < "$LOG") -eq 6 ]]

printf 'OK: republish stages to a temp dir, validates, reinstalls, enables, and verifies in order\n'
```

- [ ] **Step 2: Run to verify it fails**

Run: `chmod +x tests/contract/test_01_republish.sh && tests/contract/test_01_republish.sh`
Expected: FAIL — the current republish calls the lock helper/package store, so either `expected exactly 7 compozy calls` or `staging path is not a guarded temp dir` fires (the fake receives extra `version` calls from the re-exec and installs from `~/.local/share/...`).

- [ ] **Step 3: Rewrite `scripts/republish.sh`**

```bash
#!/usr/bin/env bash
# Local dev install: stage the five package files, validate, reinstall, enable, verify.
set -euo pipefail

ROOT=$(cd "$(dirname "$0")/.." && pwd -P)
cd "$ROOT"

scripts/check-compozy-version.sh >/dev/null

STAGE=$(mktemp -d /tmp/batuta-republish.XXXXXX)
cleanup() {
  case "$STAGE" in
    /tmp/batuta-republish.*) rm -rf -- "$STAGE" ;;
    *)
      printf 'refusing to clean unexpected staging path: %s\n' "$STAGE" >&2
      return 1
      ;;
  esac
}
trap cleanup EXIT
scripts/stage-extension.sh "$STAGE"

compozy extension validate "$STAGE" -o json | python3 -c '
import json, sys
d = json.load(sys.stdin)
errors = [i for i in d.get("issues", []) if i.get("severity") == "error"]
assert not errors, f"pacote invalido: {errors}"
'

if compozy extension list -o json | python3 -c '
import json, sys
rows = json.load(sys.stdin)
raise SystemExit(0 if any(row["name"] == "batuta" for row in rows) else 1)
'; then
  compozy extension remove batuta --global -o json >/dev/null
fi

compozy extension install "$STAGE" --allow-unverified --yes -o json >/dev/null
compozy extension enable batuta -o json | python3 -c "
import json,sys
d=json.load(sys.stdin)
state=d.get('extension',{}).get('state')
assert state=='active', f'enable falhou: {d}'
print('extensao ativa')"

compozy extension inventory batuta -o json | python3 -c "
import json,sys
items=json.load(sys.stdin)['items']
actual={(it['kind'], it['name']) for it in items}
expected={('agent','batuta'), ('loop','batuta-deliver'), ('skill','batuta-routing')}
assert actual==expected, f'inventario inesperado: {sorted(actual)}'
assert all(it['live'] for it in items), f'recursos nao-live: {items}'
print('recursos live:', ', '.join(it['name'] for it in items))"
```

- [ ] **Step 4: Delete the package store, lock helper, and their tests**

```bash
git rm -q scripts/package-extension.sh scripts/with-batuta-lock.py \
  tests/contract/test_01_package.sh tests/contract/test_01_package_lock.sh \
  tests/contract/test_01_republish_adulteration.sh tests/contract/test_01_republish_global_lock.sh \
  tests/contract/test_01_republish_package.sh
```

- [ ] **Step 5: Trim `tests/contract/test_07_license.sh`**

Delete the block from `package_root="$TEST_ROOT/packages"` through `assert_inventory "$package_dir" content-addressed` (8 lines). Change the final message to:

```bash
printf 'OK: MIT license is exact and preserved in the staged package tree\n'
```

- [ ] **Step 6: Update `docs/verify.md` "Local development install"**

Replace the paragraph under `## Local development install` with:

```markdown
From a checkout, `scripts/republish.sh` checks the CompozyOS version, stages
the five package files into a temporary directory, validates them,
reinstalls and enables the extension, and checks the live inventory. See
`CONTRIBUTING.md`.
```

- [ ] **Step 7: Run to verify it passes**

Run: `tests/contract/test_01_republish.sh && tests/contract/test_00_republish_guard.sh && tests/contract/test_07_license.sh && tests/contract/test_07_public_docs.sh && tests/contract/test_01_stage.sh && bash -n scripts/*.sh tests/contract/*.sh && git diff --check && ! grep -rn 'package-extension\|with-batuta-lock\|BATUTA_PACKAGE_ROOT\|batuta-republish.lock' scripts tests docs/*.md README.md README.pt-BR.md CONTRIBUTING.md .github/workflows`
Expected: `OK:` lines from each test; final grep finds nothing (exit 0 overall).

- [ ] **Step 8: Real-machine check (maintainer daemon running)**

Run: `scripts/republish.sh && compozy extension provenance batuta -o json | python3 -c 'import json,sys; d=json.load(sys.stdin); print(d["installed_from"], d["source_url"])' && ls ~/.local/share/batuta-compozy 2>/dev/null; ls ~/.compozy/locks 2>/dev/null | grep batuta || echo no-lock`
Expected: `extensao ativa`, `recursos live: batuta, batuta-deliver, batuta-routing`, `local_path /tmp/batuta-republish.…` (the source path no longer exists — that is expected: the daemon copied it into its managed dir), no new files under `~/.local/share/batuta-compozy/` (pre-existing ones may remain), `no-lock` for new locks. Note: this replaces the maintainer's GitHub-sourced install with a local one; the maintainer restores it afterwards with `compozy extension remove batuta --global && compozy extension install github:franciscpd/batuta-compozy --allow-unverified --yes && compozy extension enable batuta` if desired.

- [ ] **Step 9: Commit**

```bash
git add -A scripts tests/contract docs/verify.md
git commit -m "refactor: republish from a temporary staging directory"
```

---

### Task 3: Internal docs under `docs/internal/`

**Files:**
- Move: `docs/superpowers/` → `docs/internal/` (all files, `git mv`)
- Modify: `tests/contract/test_07_preview_docs.sh:8-9,116`, `tests/contract/test_07_public_docs.sh` (CONTRIBUTING list + new check), `CONTRIBUTING.md` (validation section)
- Create: `CLAUDE.md`

- [ ] **Step 1: Extend the doc contract tests (RED)**

In `tests/contract/test_07_public_docs.sh`, in the CONTRIBUTING.md `for text in` list add:

```bash
  'docs/internal/specs' \
  'docs/internal/plans' \
```

and after that loop add:

```bash
if [[ -e docs/superpowers ]]; then
  printf 'internal planning docs must live under docs/internal, not docs/superpowers\n' >&2
  exit 1
fi
require_file CLAUDE.md
require_text CLAUDE.md 'docs/internal/specs/'
require_text CLAUDE.md 'docs/internal/plans/'
```

In `tests/contract/test_07_preview_docs.sh` change lines 8, 9, and 116 so every `docs/superpowers/` becomes `docs/internal/` (three occurrences: `preview_design=`, `preview_plan=`, and the `aggregate_plans=(…)` line).

Run: `tests/contract/test_07_public_docs.sh`
Expected: FAIL with `missing public documentation text in CONTRIBUTING.md: docs/internal/specs`.

- [ ] **Step 2: Move the directory and add the docs**

```bash
git mv docs/superpowers docs/internal
```

`CONTRIBUTING.md`: after the paragraph beginning "Run `tests/contract/run.sh` only from a disposable checkout" (still in the validation section, before `## Releasing`) add:

```markdown
Design specs and implementation plans live in `docs/internal/specs` and
`docs/internal/plans`. New ones go there. Nothing under `docs/internal/` is
part of the extension package or the public guides.
```

Create `CLAUDE.md` at the repository root:

```markdown
# batuta-compozy

- Design specs go in `docs/internal/specs/`, implementation plans in
  `docs/internal/plans/` (not `docs/superpowers/`).
- Never run `tests/contract/run.sh` from a checkout that has `.compozy/`;
  use a disposable detached worktree.
```

- [ ] **Step 3: Run to verify it passes**

Run: `tests/contract/test_07_public_docs.sh && tests/contract/test_07_preview_docs.sh && tests/contract/test_07_case_study.sh && tests/contract/test_07_workflow_contract.sh && git diff --check && ! grep -rn 'docs/superpowers' tests scripts README.md README.pt-BR.md CONTRIBUTING.md docs/*.md docs/releases .github/workflows CLAUDE.md`
Expected: four `OK:` lines; final grep finds nothing. (References inside `docs/internal/**` to their old paths are historical text and stay.)

- [ ] **Step 4: Commit**

```bash
git add -A docs CONTRIBUTING.md CLAUDE.md tests/contract/test_07_public_docs.sh tests/contract/test_07_preview_docs.sh
git commit -m "docs: move internal specs and plans under docs/internal"
```

---

### Task 4: Full suite and push

- [ ] **Step 1: Aggregate suite in a disposable checkout** (needs the local daemon running)

```bash
git worktree add --detach /tmp/batuta-clean-dx HEAD
(cd /tmp/batuta-clean-dx && tests/contract/run.sh)
git worktree remove --force /tmp/batuta-clean-dx
```

Expected: `=== todos os testes de contrato passaram ===`. Note `test_03_lifecycle.sh` prints SKIP while batuta is installed — expected.

- [ ] **Step 2: Push `main` and watch CI**

```bash
git push origin main
gh run watch "$(gh run list --repo franciscpd/batuta-compozy --workflow ci.yml --limit 1 --json databaseId --jq '.[0].databaseId')" --repo franciscpd/batuta-compozy --exit-status
```

Expected: exit 0.

---

## Self-review notes

- Spec coverage: Part A → Task 1; Part B → Task 2 (script, deletions, new test, license test, verify.md); Part C → Task 3 (move, tests, CONTRIBUTING, CLAUDE.md); acceptance → Task 2 Step 8 + Task 4.
- `test_00_republish_guard.sh` keeps working: the new republish calls the guard first; the fake reports `v0.3.0-beta.13` → guard exits 1 → script exits before any `extension remove`.
- Names consistent: staging prefix `/tmp/batuta-republish.` in script and test; guard messages `incompatible CompozyOS … v0.3.0-beta.14 …`, `custom post-tag build`, `OK: ` in script and test.
