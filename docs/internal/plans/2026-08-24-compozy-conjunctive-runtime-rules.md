# Compozy Conjunctive Runtime Rules Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Allow one task runtime rule to match `type + complexity` with specificity `id > type+complexity > type > complexity` while preserving every existing single-selector behavior.

**Architecture:** Keep the public `RuntimeMatch` wire shape unchanged and expand only validation and matching semantics. The resolver evaluates the conjunction as an AND selector and continues merging runtime fields independently; config/API/native-tool serialization therefore needs no migration.

**Tech Stack:** Go 1.26, Compozy Loop runtime, JSON/YAML/TOML config, Go tests, official Compozy skill documentation.

**Spec:** `docs/internal/specs/2026-08-24-compozy-batuta-platform-prerequisites-design.md` in the Batuta delivery-hardening worktree.

## Global Constraints

- Implement in a new isolated worktree created from `/home/francisross/Projects/opensource/compozy`; do not modify its dirty primary checkout.
- Valid selector shapes are exactly `id`, `type`, `complexity`, and `type + complexity`.
- `id` stays exclusive; a conjunction is AND, never OR.
- Specificity is exactly `id > type + complexity > type > complexity`.
- Existing layer precedence, later-equal-rule precedence, per-field merging, and provenance remain unchanged.
- Do not add a new compact `--runtime` flag grammar in this change.
- Run `make gate` and `make gate-full` with both `TMPDIR` and `GOTMPDIR` unset.
- Prefix every executed shell command with `rtk`, as required by the active
  repository instructions; command blocks below show the underlying argv.

---

### Task 1: Validate the conjunctive selector grammar

**Files:**
- Modify: `internal/loop/runtime_validation_test.go`
- Modify: `internal/loop/runtime_validation.go`

**Interfaces:**
- Consumes: `RuntimeMatch{ID, Type, Complexity}` and `validateRuntimeRules`.
- Produces: validation accepting only the four specified selector shapes.

- [ ] **Step 1: Add a table-driven failing validation test**

Add `TestValidateRuntimeRulesShouldAcceptTypeComplexityConjunction` beside the existing empty-matcher test. Build rules with a non-empty runtime and assert:

```go
tests := []struct {
    name       string
    match      dsl.RuntimeMatch
    wantReason string
}{
    {name: "id", match: dsl.RuntimeMatch{ID: "task_01"}},
    {name: "type", match: dsl.RuntimeMatch{Type: "frontend"}},
    {name: "complexity", match: dsl.RuntimeMatch{Complexity: "high"}},
    {name: "type and complexity", match: dsl.RuntimeMatch{Type: "frontend", Complexity: "high"}},
    {name: "empty", match: dsl.RuntimeMatch{}, wantReason: "selector_required"},
    {name: "id and type", match: dsl.RuntimeMatch{ID: "task_01", Type: "frontend"}, wantReason: "selector_collision"},
    {name: "id and complexity", match: dsl.RuntimeMatch{ID: "task_01", Complexity: "high"}, wantReason: "selector_collision"},
    {name: "all", match: dsl.RuntimeMatch{ID: "task_01", Type: "frontend", Complexity: "high"}, wantReason: "selector_collision"},
}
```

For valid rows require `ValidateDefinitionRuntime(...) == nil`; for invalid rows use `assertRuntimeValidationItem` at `runtime_rules[0].match`.

- [ ] **Step 2: Run the focused test and prove RED**

```bash
go test ./internal/loop -run TestValidateRuntimeRulesShouldAcceptTypeComplexityConjunction -count=1
```

Expected: the `type and complexity` case fails with `selector_collision`.

- [ ] **Step 3: Implement the minimal selector-shape validation**

Replace the selector-count-only collision check in `validateRuntimeRules` with:

```go
hasID := strings.TrimSpace(rule.Match.ID) != ""
hasType := strings.TrimSpace(rule.Match.Type) != ""
hasComplexity := strings.TrimSpace(rule.Match.Complexity) != ""
if !hasID && !hasType && !hasComplexity {
    return runtimeValidation(path+".match", "", "selector_required")
}
if hasID && (hasType || hasComplexity) {
    return runtimeValidation(path+".match", "", "selector_collision")
}
```

Leave `type + complexity` valid and retain existing unknown-field, empty-runtime, model, reasoning, and speed validation.

- [ ] **Step 4: Run focused validation tests**

```bash
go test ./internal/loop -run 'TestValidateRuntimeRulesShouldAcceptTypeComplexityConjunction|TestValidateDefinitionRuntime' -count=1
```

Expected: PASS.

- [ ] **Step 5: Commit the grammar change**

```bash
git add internal/loop/runtime_validation.go internal/loop/runtime_validation_test.go
git commit -m "feat(loop): allow domain complexity matches"
```

### Task 2: Resolve conjunctions with the new specificity

**Files:**
- Modify: `internal/loop/runtime_resolve_test.go`
- Modify: `internal/loop/runtime_resolve.go`

**Interfaces:**
- Consumes: one normalized `RuntimeMatch` and one `ItemRuntime`.
- Produces: `runtimeRuleSpecificity(match, item) (specificity int, matches bool)` with ranks 4, 3, 2, and 1.

- [ ] **Step 1: Add failing resolution truth-table tests**

Add subtests that use one matrix rule:

```go
matrix := loop.RuntimeRule{
    Match: loop.RuntimeMatch{Type: "frontend", Complexity: "high"},
    Runtime: loop.RuntimeSpec{Provider: "cursor", Model: "frontier", Reasoning: "high"},
}
```

Assert it matches only `frontend/high`, and does not match `frontend/low`, `backend/high`, or an item with either field empty. Then add one precedence test:

```go
rules := []loop.RuntimeRule{
    {Match: loop.RuntimeMatch{Complexity: "high"}, Runtime: loop.RuntimeSpec{Reasoning: "medium", Model: "complexity"}},
    {Match: loop.RuntimeMatch{Type: "frontend"}, Runtime: loop.RuntimeSpec{Provider: "type", Model: "type"}},
    {Match: loop.RuntimeMatch{Type: "frontend", Complexity: "high"}, Runtime: loop.RuntimeSpec{Provider: "matrix", Model: "matrix", Reasoning: "high"}},
    {Match: loop.RuntimeMatch{ID: "task_01"}, Runtime: loop.RuntimeSpec{Provider: "exact"}},
}
```

Expected runtime: provider `exact`, model `matrix`, reasoning `high`, all with config provenance.

- [ ] **Step 2: Run the focused resolver test and prove RED**

```bash
go test ./internal/loop -run TestResolveItemRuntimeShouldMergeFieldsByPrecedence -count=1
```

Expected: the matrix rule does not match correctly because the current resolver returns after checking `type`.

- [ ] **Step 3: Implement AND matching and specificity constants**

Replace `runtimeRuleSpecificity` with:

```go
func runtimeRuleSpecificity(match RuntimeMatch, item ItemRuntime) (int, bool) {
    id := strings.TrimSpace(match.ID)
    taskType := strings.TrimSpace(match.Type)
    complexity := strings.TrimSpace(match.Complexity)
    if id != "" {
        return 4, id == strings.TrimSpace(item.TaskID)
    }
    if taskType != "" && complexity != "" {
        return 3,
            taskType == strings.TrimSpace(item.TaskType) &&
                complexity == strings.TrimSpace(item.Complexity)
    }
    if taskType != "" {
        return 2, taskType == strings.TrimSpace(item.TaskType)
    }
    if complexity != "" {
        return 1, complexity == strings.TrimSpace(item.Complexity)
    }
    return 0, false
}
```

- [ ] **Step 4: Run all Loop runtime tests**

```bash
go test ./internal/loop -run 'Runtime|ResolveItem' -count=1
```

Expected: PASS with legacy precedence assertions unchanged except where the new matrix is explicitly present.

- [ ] **Step 5: Commit the resolver change**

```bash
git add internal/loop/runtime_resolve.go internal/loop/runtime_resolve_test.go
git commit -m "feat(loop): resolve domain complexity matrix"
```

### Task 3: Prove config, API, and daemon round trips

**Files:**
- Modify: `internal/config/loops_test.go`
- Modify: `internal/daemon/loop_runtime_selection_integration_test.go`
- Modify: `internal/daemon/loop_runtime_selection_integration_helpers_test.go` only if the existing shared fixture needs the new matrix row

**Interfaces:**
- Consumes: JSON/TOML `runtime_rules[].match.type` plus `.complexity`.
- Produces: unchanged wire structs and a daemon generation output with matrix-selected `resolved_runtime`.

- [ ] **Step 1: Add a TOML round-trip case**

Extend the existing runtime-rule config fixture with:

```toml
[[loops.defaults.delivery.runtime_rules]]
[loops.defaults.delivery.runtime_rules.match]
type = "frontend"
complexity = "high"
[loops.defaults.delivery.runtime_rules.runtime]
provider = "cursor"
model = "frontier"
reasoning = "high"
```

Assert both matcher fields survive load/clone/merge and the runtime fields are unchanged.

- [ ] **Step 2: Add a daemon mixed-matrix integration case**

Create one imported batch containing at least `frontend/high`, `frontend/low`, and `backend/high`. Configure distinct matrix rules and assert each `execute_task` output's `ResolvedRuntime` and per-field `Source` exactly match the chosen rule. Include an exact-ID rule for one item and assert it wins over its matrix cell.

- [ ] **Step 3: Run focused config and integration tests**

```bash
go test ./internal/config -run 'Loop|Runtime' -count=1
go test -tags=integration ./internal/daemon -run 'RuntimeSelection.*Matrix' -count=1
```

Expected: PASS.

- [ ] **Step 4: Commit round-trip coverage**

```bash
git add internal/config/loops_test.go internal/daemon/loop_runtime_selection_integration_test.go internal/daemon/loop_runtime_selection_integration_helpers_test.go
git commit -m "test(loop): prove matrix routing round trips"
```

### Task 4: Update authoritative documentation and gates

**Files:**
- Modify: `skills/compozy/references/loops.md`
- Modify: `skills/compozy/references/configuration.md`
- Modify: `docs/qa/scenarios/LP-runtime-selection-overrides.md`

**Interfaces:**
- Consumes: shipped matcher grammar and specificity.
- Produces: official documentation without promising compact CLI conjunction syntax.

- [ ] **Step 1: Update routing documentation**

Use this exact contract:

```text
Rules match one exact `id`, one `type`, one `complexity`, or the conjunction
`type + complexity`. The conjunction is AND. Specificity is
`id > type + complexity > type > complexity`; later equal-specificity rules
win per non-empty runtime field.
```

Add one JSON/TOML matrix example and retain existing repeatable `--runtime` examples as single-selector examples.

- [ ] **Step 2: Update the QA scenario**

Require one mixed task batch to prove a matrix rule, a legacy single-selector rule, and an exact-ID override through dry-run and real resolution.

- [ ] **Step 3: Format and run focused checks**

```bash
gofmt -w internal/loop/runtime_validation.go internal/loop/runtime_validation_test.go internal/loop/runtime_resolve.go internal/loop/runtime_resolve_test.go internal/config/loops_test.go internal/daemon/loop_runtime_selection_integration_test.go internal/daemon/loop_runtime_selection_integration_helpers_test.go
make codegen-check
go test ./internal/loop ./internal/config -count=1
```

Expected: PASS and no generated diff.

- [ ] **Step 4: Run repository gates with Go test temp semantics intact**

Ensure `TMPDIR` and `GOTMPDIR` are unset, then run:

```bash
make gate
make gate-full
git status --short
```

Expected: both gates PASS and status shows only intended tracked changes.

- [ ] **Step 5: Commit documentation and final evidence**

```bash
git add skills/compozy/references/loops.md skills/compozy/references/configuration.md docs/qa/scenarios/LP-runtime-selection-overrides.md
git commit -m "docs(loop): document matrix routing"
```
