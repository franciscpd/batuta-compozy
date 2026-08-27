# Compozy child Loop config overrides — implementation plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use `superpowers:test-driven-development` for every behavior change, `superpowers:receiving-code-review` for review findings, and `superpowers:verification-before-completion` before claiming completion. Execute this plan from a clean worktree; do not reuse the experimental prerequisite branch.

**Goal:** Add one generic, migration-free Compozy contract that lets a `run-loop` action pass the closed public `LoopConfig` shape to its child as ephemeral per-run overrides.

**Architecture:** `dsl.RunLoopParams` owns the authored `config_overrides` map because node references are materialized before action execution. The linter decodes that materialized-compatible map through the canonical `loop.LoopConfig` JSON contract and rejects unknown keys. `RunLoopActionExecutor` performs the same closed decode at execution and places the value in `loop.Inputs.ConfigOverrides`; the existing service remains the sole owner of precedence, validation, and child persistence. No stored Loop configuration, database schema, SQL, API endpoint, native tool, SDK, or migration changes are permitted.

**Tech stack:** Go, Compozy Loop DSL, `encoding/json`, YAML/JSON Loop definitions, repository skill documentation, public Fumadocs content, living QA docs.

**Approved design:** [2026-08-26-batuta-migration-free-delivery-recovery-design.md](../specs/2026-08-26-batuta-migration-free-delivery-recovery-design.md)

**Supersession:** For the final Compozy contribution, this plan replaces the Loop-config-CAS and same-lineage recovery tasks in `2026-08-25-compozy-batuta-activation-prerequisites.md`. The required-hooks plan is canceled. None of those experimental commits are inputs to this branch.

## Global constraints

- Create a dedicated upstream issue before implementation and reference it from the PR.
- Update the fork's `main` to the exact current `upstream/main` before creating the feature worktree.
- Confirm the updated base already contains conjunctive runtime-rule matching plus Pedro's upstream `halt` and idempotent Goal cleanup work. Upstream owns overlapping behavior.
- Do not cherry-pick or copy commits from `feat/batuta-platform-prerequisites`; reimplement the six-file delta with TDD on the clean base.
- Do not touch `internal/store/globaldb`, Atlas state, generated SQL, migrations, Loop config CAS, nested recovery, required hooks, or Goal cleanup.
- Preserve absent-override behavior exactly: empty `ConfigOverrides`, inherited parent environment, same await/detach semantics, and no persisted child configuration.
- `enabled_checks_json` is a JSON value, not a YAML string; its object/array shape must survive exactly.
- Current `main` requires public site documentation and a living QA scenario for user-visible Loop behavior. These documentation owners do not expand the production contract.
- All shell commands in this plan are prefixed with `rtk`. For `make gate*`, leave `TMPDIR` and `GOTMPDIR` unset.

---

## Task 1: Create the upstream issue and a clean feature worktree

**Files:**

- Read: Compozy issue templates under `.github/ISSUE_TEMPLATE/`
- Read: repository remotes and current `main`
- Create locally for issue submission only: `$issue_dir/issue.md`, where `$issue_dir` is the absolute result of the `mktemp` command below
- Create worktree: `/home/francisross/Projects/opensource/_worktrees/compozy-run-loop-config-overrides`

### Step 1: Prove the starting checkout and remotes

From the primary Compozy checkout, run:

```bash
rtk git status --short
rtk git remote -v
rtk git fetch upstream main
rtk git fetch origin main
rtk git rev-parse upstream/main
rtk git rev-list --left-right --count origin/main...upstream/main
```

Do not mutate a dirty primary checkout. Resolve the exact primary checkout with `git worktree list --porcelain`; do not assume the experimental worktree is safe.

### Step 2: Confirm current issue naming and write the feature issue

Inspect the repository templates and the newest open core feature issues:

```bash
rtk rg -n "core|feature|name:|title:" .github/ISSUE_TEMPLATE .github 2>/dev/null
rtk gh issue list --repo compozy/compozy --state open --limit 30
```

Create the scratch path with `issue_dir=$(mktemp -d -p /home/francisross/tmp-builds compozy-run-loop-issue.XXXXXX)`, record the resolved absolute value, then use `apply_patch` to write this exact issue body to `$issue_dir/issue.md`:

```markdown
## Feature

Allow a `run-loop` action to declare `params.config_overrides` using the same closed public `LoopConfig` fields accepted by a direct Loop run.

## Why

A parent Loop can currently provide child inputs and await/detach mode, but it cannot apply child-only iteration, wall, token, environment, reattempt, or runtime-selection limits. Callers must either persist shared Loop configuration or start the child outside the graph. Both choices break run isolation and make concurrent deliveries interfere.

## Examples

```yaml
- id: implement
  class: action
  kind: run-loop
  params:
    loop: implement-tasks
    config_overrides:
      iteration_cap: 4
      budget_tokens: 250000
      budget_wall_sec: 7200
      budget_on_exceeded: halt
      reattempt_strategy: halt
      environment:
        mode: worktree
        worktree_ref: "{{ .inputs.worktree_ref }}"
      runtime_rules: "{{ .nodes.routing.output.runtime_rules }}"
```

`runtime_rules` above is a direct typed node-output reference. Arrays and numbers must remain typed after materialization. JSON-valued fields such as `enabled_checks_json` must retain their object shape.

## Required behavior

- Apply overrides only to the started child run.
- Never write the child's stored Loop configuration.
- Use the existing `LoopConfig` precedence and validation path.
- Reject unknown keys during Loop lint.
- Preserve current environment inheritance and execution when the field is absent.
- Support both `await` and `detach` modes.
- Add focused tests and Compozy skill documentation.

## Non-goals

- Database or Atlas migrations.
- Revisioned stored configuration.
- Batuta-specific persistence or recovery.
- Required hooks or Goal lifecycle changes.
```

Use the exact title prefix established by the current repository convention; the semantic title is `support child Loop config overrides`. Submit it with:

```bash
rtk gh issue create --repo compozy/compozy --title "core: support child Loop config overrides" --body-file "$issue_dir/issue.md"
```

If the current convention uses a different machine prefix, change only the prefix, not the issue semantics. Record the returned issue URL/number in the task report. Remove only this run's unique scratch directory after submission.

### Step 3: Fast-forward the fork and create the clean worktree

After confirming the primary `main` is clean:

```bash
rtk git switch main
rtk git merge --ff-only upstream/main
rtk git push origin main
rtk git rev-parse main
rtk git rev-parse upstream/main
```

The two SHAs must match. Confirm the merged routing, halt, and cleanup contracts with focused searches/tests before branching:

```bash
rtk rg -n "runtime_rules|ReattemptHalt|session.cleanup" internal/loop internal/store/globaldb internal/tools
rtk go test ./internal/loop ./internal/store/globaldb ./internal/tools/builtin -run 'RuntimeRule|ReattemptHalt|GoalSessionCleanup' -count=1
```

Resolve whether the target path already exists before creating anything. Then create:

```bash
rtk git worktree add -b feat/run-loop-config-overrides /home/francisross/Projects/opensource/_worktrees/compozy-run-loop-config-overrides main
rtk git -C /home/francisross/Projects/opensource/_worktrees/compozy-run-loop-config-overrides status --short
```

Expected: empty status, branch based exactly on the recorded upstream SHA.

### Step 4: Commit the setup evidence only in the task report

Do not create a source commit for issue/worktree setup. Record issue URL, upstream SHA, fork SHA, branch name, and the prerequisite-contract evidence in the implementation report.

---

## Task 2: Add RED tests for the authored and materialized contract

**Files:**

- Modify: `internal/loop/action_test.go`
- Modify: `internal/loop/linter_test.go`
- Test owner already present: `internal/loop/action_test.go` → `TestReservedActionExecutorsShouldRunAgentLoopAndTransform`
- Test owner already present: `internal/loop/linter_test.go` → `TestLinterShouldRejectClosedEnumAndReservedSchemaViolations`

### Step 1: Add the executor RED matrix

Extend the existing `run-loop` executor test with table-driven cases that capture the `loop.Inputs` passed to the child starter:

1. no `config_overrides`:
   - `ConfigOverrides` equals the zero `loop.LoopConfig`;
   - `InheritedEnvironment` still equals the parent worktree environment;
   - await and detach behavior are unchanged;
2. literal closed config:
   - `iteration_cap: 4`;
   - `budget_tokens: 250000`;
   - `budget_wall_sec: 7200`;
   - `budget_on_exceeded: halt`;
   - `reattempt_strategy: halt`;
   - explicit worktree environment;
   - one exact runtime rule containing `task_id`, provider, model, and reasoning;
3. materialized typed values:
   - `runtime_rules` arrives as `[]any`/`[]map[string]any` from a direct node-output reference after parameter materialization;
   - numeric budget fields arrive as numbers, not strings;
4. `enabled_checks_json` object:
   - assert its `json.RawMessage` equals `{"quality":{"enabled":true}}` after JSON semantic comparison, not string formatting alone;
5. invalid closed values:
   - unknown key returns an error before `starter.Start`;
   - wrong field type returns an error before `starter.Start`.

Use a starter spy with an explicit call count so every invalid case proves that no child was created.

### Step 2: Add the linter RED matrix

Add cases to the existing reserved-schema table:

- `iteration_caps` produces `refs.CodeUnresolvablePath`;
- wrong type such as `iteration_cap: "four"` produces `refs.CodeUnresolvablePath`;
- a valid `enabled_checks_json` object does not add a lint problem;
- direct template values such as `runtime_rules: "{{ .nodes.routing.output.runtime_rules }}"` and `budget_tokens: "{{ .nodes.routing.output.remaining_tokens }}"` are accepted at authoring time and validated after materialization by the executor/service path.

The linter must remain closed for literal maps without trying to invent a schema for the referenced value.

### Step 3: Run the focused RED

```bash
rtk go test ./internal/loop -run 'TestReservedActionExecutorsShouldRunAgentLoopAndTransform|TestLinterShouldRejectClosedEnumAndReservedSchemaViolations' -count=1
```

Expected: failure because `RunLoopParams` does not expose `config_overrides`, the linter does not reject its invalid content, and the executor does not forward it.

### Step 4: Commit the tests

```bash
rtk git add internal/loop/action_test.go internal/loop/linter_test.go
rtk git commit -m "test: define child loop config overrides"
```

---

## Task 3: Implement the narrow DSL, lint, and executor seam

**Files:**

- Modify: `internal/loop/dsl/node_params.go`
- Modify: `internal/loop/action_runloop.go`
- Modify: `internal/loop/linter_actions.go`

### Step 1: Extend only `RunLoopParams`

Add the authored field without widening the generic node contract:

```go
type RunLoopParams struct {
    Loop            string         `json:"loop"                       yaml:"loop"`
    Inputs          map[string]any `json:"inputs,omitempty"           yaml:"inputs,omitempty"`
    Mode            RunLoopMode    `json:"mode,omitempty"             yaml:"mode,omitempty"`
    ConfigOverrides map[string]any `json:"config_overrides,omitempty" yaml:"config_overrides,omitempty"`
    Extra           map[string]any `json:"-"                          yaml:",inline"`
}
```

Do not add a Batuta-specific type or a second public `LoopConfig` definition.

### Step 2: Add one canonical closed decoder

In `internal/loop/action_runloop.go`, add an unexported helper that:

- returns zero `LoopConfig` for an absent/empty map;
- marshals the materialized value with `encoding/json`;
- decodes with `json.Decoder.DisallowUnknownFields()` into the existing `loop.LoopConfig`;
- rejects a trailing JSON value;
- wraps failures with `run-loop config_overrides` context;
- never uses YAML for the intermediate conversion, because `enabled_checks_json` is raw JSON.

The intended seam is:

```go
func decodeRunLoopConfigOverrides(raw map[string]any) (LoopConfig, error)
```

### Step 3: Forward the decoded value to the child

In `RunLoopActionExecutor.Execute`, decode after `RunLoopParams` has been materialized and before `starter.Start`. Populate:

```go
Inputs{
    ProfileID:            in.ToolScope.ProfileID,
    Values:               spec.Inputs,
    ParentLoopRunID:      in.LoopRunID,
    ConfigOverrides:      configOverrides,
    InheritedEnvironment: cloneEnvironmentSpec(in.EnvironmentValue()),
}
```

Do not resolve precedence here. `Service.Run`/effective config remains the owner.

### Step 4: Lint literal closed values

In `lintRunLoopNode`, add `lintRunLoopConfigOverrides` and preserve the existing `params.Extra` rejection and loop/mode validation. The helper must:

- copy the authored map;
- remove only top-level values that are a complete Loop template expression, because their typed value is unavailable until materialization;
- decode the remaining literal fields through `decodeRunLoopConfigOverrides`, preserving `DisallowUnknownFields` for every authored key;
- add `refs.CodeUnresolvablePath` on failure.

This means `runtime_rules: "{{ ... }}"` and numeric budget references lint successfully, while `iteration_caps`, literal wrong types, and invalid literal enum values fail before start. Execution always decodes the fully materialized map, so a reference that resolves to a wrong type still fails before `starter.Start`. Do not silently accept a literal map with unknown keys.

### Step 5: Format and run GREEN

```bash
rtk gofmt -w internal/loop/dsl/node_params.go internal/loop/action_runloop.go internal/loop/linter_actions.go internal/loop/action_test.go internal/loop/linter_test.go
rtk go test ./internal/loop -run 'TestReservedActionExecutorsShouldRunAgentLoopAndTransform|TestLinterShouldRejectClosedEnumAndReservedSchemaViolations' -count=1
```

Expected: all new cases pass and invalid cases keep the starter call count at zero.

### Step 6: Commit the implementation

```bash
rtk git add internal/loop/dsl/node_params.go internal/loop/action_runloop.go internal/loop/linter_actions.go
rtk git commit -m "feat: pass config overrides to child loops"
```

---

## Task 4: Add a service-level child-run isolation regression

**Files:**

- Modify the nearest existing run-loop integration/service test under `internal/loop/` discovered by:

```bash
rtk rg -n "RunLoopActionExecutor|ParentLoopRunID|InheritedEnvironment|ConfigOverrides" internal/loop --glob '*test.go'
```

- Do not add a new production seam.

### Step 1: Write the RED at the highest practical in-process boundary

Start a parent definition containing a `run-loop` node and capture the child `Run`/effective config. Assert:

- the child has the requested iteration, token, wall, halt, environment, and runtime rule values;
- the parent's effective config is unchanged;
- a second direct child run without overrides still resolves its stored/default configuration;
- no stored Loop config write method is invoked;
- invalid enum/type fails before a child run is persisted.

If the existing harness exposes `Inputs` rather than a full persisted child, keep the regression in `action_test.go`; do not build a parallel integration harness merely to rename the boundary.

### Step 2: Run RED then GREEN

```bash
rtk go test ./internal/loop -run 'RunLoop.*ConfigOverride|Child.*ConfigOverride|ReservedActionExecutors' -count=1
```

If this RED exposes missing validation in the canonical effective-config service, fix the narrow owner already used by direct runs. Do not duplicate validation in the action.

### Step 3: Commit the regression

```bash
rtk git add internal/loop/*_test.go
rtk git commit -m "test: prove child config override isolation"
```

---

## Task 5: Document the generic contract

**Files:**

- Modify: `skills/compozy/references/loops.md`
- Modify: `packages/site/content/docs/loops/dsl-reference.mdx`
- Create: `docs/qa/scenarios/LP-child-loop-config-overrides.md`
- Create: `docs/qa/reports/2026-08-26-child-loop-config-overrides.md`

### Step 1: Add the contract beside existing `run-loop` documentation

Document:

- same closed shape as a direct run's `LoopConfig`;
- child-only, per-run precedence and no persistence;
- inherited environment when absent;
- unknown literal keys fail lint;
- typed direct node-output reference for `runtime_rules`;
- JSON object example for `enabled_checks_json`;
- both await and detach support.

Use a generic example with `implement-tasks`; mention Batuta only as one use case, not as the owner of the core feature.

Mirror the user-facing contract in the public DSL reference beside the existing `run-loop` row and runtime-rule paragraph. Keep the exact config keys and precedence statement aligned with runtime truth.

### Step 2: Add and walk the living QA scenario

Create the content-addressed `LP-child-loop-config-overrides` scenario under the existing `J-await-child-loop` journey. Start it as `untested` with empty evidence/report metadata, then walk a real parent-to-child run through the narrowest available public surface. The scenario must prove child-only application, absence compatibility, typed runtime-rule materialization, and no stored-config mutation. Record the verdict and durable evidence in the dated report; do not claim `pass` from unit tests alone.

### Step 3: Run docs/skill checks

Use the repository's current skill validator discovered from `Makefile`/scripts, then run:

```bash
rtk git diff --check
rtk make codegen
rtk make codegen-check
rtk bunx turbo run typecheck test build --filter=./packages/site
```

If `make codegen` changes files, inspect why. This DSL-only contract is expected to produce no generated artifact; revert no user work, but do not include unrelated generated drift in this branch.

### Step 4: Commit documentation, QA evidence, and required generated output

```bash
rtk git add skills/compozy/references/loops.md packages/site/content/docs/loops/dsl-reference.mdx docs/qa/scenarios/LP-child-loop-config-overrides.md docs/qa/reports/2026-08-26-child-loop-config-overrides.md
rtk git commit -m "docs: explain child loop config overrides"
```

Before committing, unstage any generated file whose content is unrelated to this contract.

---

## Task 6: Independent review, full verification, and PR preparation

**Files:**

- Read: every changed file
- Write only if findings require fixes: the six approved owners and their focused tests
- Create: implementation/review reports under the repository's existing ignored SDD report location

### Step 1: Prove scope and migration absence

```bash
rtk git status --short
rtk git diff main...HEAD --stat
rtk git diff main...HEAD --name-only
rtk git diff main...HEAD -- internal/store/globaldb/schema/migrations internal/store/globaldb/schema.hcl internal/store/globaldb/sql
```

Expected migration/schema/SQL diff: empty. Expected source scope: DSL params, run-loop executor/linter, focused tests, Loop skill documentation, and only unavoidable generated catalog output.

### Step 2: Request independent code review

The reviewer must verify:

- no persistence or config CAS call;
- strict unknown-field behavior;
- raw JSON preservation;
- typed reference behavior after materialization;
- absent-field compatibility;
- child-only precedence;
- no duplicate upstream halt/cleanup/routing implementation;
- no Batuta-specific API in Compozy core.

Apply only technically validated findings using `superpowers:receiving-code-review`, then rerun the focused RED/GREEN commands.

### Step 3: Run fresh verification

```bash
rtk go test -race ./internal/loop ./internal/tools/builtin -count=1
rtk go vet ./internal/loop ./internal/tools/builtin
rtk make codegen-check
rtk git diff --check
rtk git diff main...HEAD -- internal/store/globaldb/schema/migrations
```

For a full Compozy gate, do not set `TMPDIR` or `GOTMPDIR`:

```bash
rtk env -u TMPDIR -u GOTMPDIR make gate
```

Record any pre-existing baseline separately with exact command, output, and proof that the failing file is unchanged. Do not call the branch ready while a branch-owned failure remains.

### Step 4: Prepare and open the PR only after the user authorizes it

Rebase/merge the latest upstream according to repository policy immediately before push, rerun Task 6 Step 3, then:

```bash
rtk git push -u origin feat/run-loop-config-overrides
pr_body=$(mktemp -p /home/francisross/tmp-builds compozy-run-loop-pr.XXXXXX.md)
rtk gh pr create --repo compozy/compozy --base main --head franciscpd:feat/run-loop-config-overrides --title "feat(core): support child Loop config overrides" --body-file "$pr_body"
```

Before the command, write the reviewed PR body to `$pr_body` with `apply_patch`. It must include `Closes #` followed by the actual issue number, the generic use cases, the exact test evidence, and the explicit statement `Database migrations: none`. Never open or push the PR without the user's explicit approval in the execution session. Remove only `$pr_body` after `gh` has consumed it.
