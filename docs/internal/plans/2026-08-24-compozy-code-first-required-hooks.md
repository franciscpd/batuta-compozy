# Compozy Code-First Required Hooks Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Preserve hook name, mode, matcher, and `required: true` from Go/TypeScript code-first declarations into generated manifests and prove required failures block matched tool calls.

**Architecture:** Extend the existing `DescribeHookEvent` contract compatibly, keep event-only declarations valid, and translate enriched fields into the existing manifest/hook registry. Runtime execution continues through the existing subprocess hook pipeline, so the change adds declaration fidelity rather than a second authority.

**Tech Stack:** Go 1.26, TypeScript/Bun, Compozy public extension SDKs, generated SDK contracts, extension manifest builder, hook pipeline integration tests.

**Spec:** `docs/internal/specs/2026-08-24-compozy-batuta-platform-prerequisites-design.md` in the Batuta delivery-hardening worktree.

## Global Constraints

- Implement in the same isolated Compozy feature worktree used for the platform prerequisite, after the conjunctive-runtime-rule commits.
- Preserve event-only defaults exactly: generated event name, eligible default mode, empty matcher, and `required: false`.
- The extension subprocess remains the only generated executor; described hooks cannot supply another command, args, env, or executor kind.
- `required: true` is valid only for a synchronous, sync-eligible event.
- The initialization handshake still advertises only deduplicated event names.
- Generated Go and TypeScript files come from `make codegen`; never hand-edit them.
- Run `make gate` and `make gate-full` with `TMPDIR` and `GOTMPDIR` unset.
- Prefix every executed shell command with `rtk`, as required by the active
  repository instructions; command blocks below show the underlying argv.

---

### Task 1: Extend the canonical describe contract and generated SDK types

**Files:**
- Modify: `internal/extension/contract/describe.go`
- Modify: `internal/codegen/sdkgo/generate_test.go`
- Modify: `internal/codegen/sdkts/generate_test.go`
- Generate: `sdk/go/contracts/types_008_gen.go` and any deterministically renumbered Go contract files
- Generate: `sdk/typescript/src/generated/contracts.ts`

**Interfaces:**
- Consumes: `DescribeHookEvent` in the canonical internal contract.
- Produces: public Go/TypeScript fields `name`, `event`, `profile`, `mode`, `matcher`, and `required`.

- [ ] **Step 1: Add failing codegen contract assertions**

Assert the generated Go form contains these fields and JSON names:

```go
type DescribeHookEvent struct {
    Name     string
    Event    HookEvent
    Profile  string
    Mode     HookMode
    Matcher  HookMatcher
    Required bool
}
```

`Name`, `Profile`, `Mode`, and `Required` use `omitempty`; `Event` and `Matcher` remain present. The TypeScript expectation exposes equivalent optional fields and the generated `HookMatcher` reference.

- [ ] **Step 2: Run codegen tests and prove RED**

```bash
go test ./internal/codegen/sdkgo ./internal/codegen/sdkts -count=1
```

Expected: FAIL because the canonical type exposes only `event` and `profile`.

- [ ] **Step 3: Extend the canonical type**

Modify `DescribeHookEvent` in `internal/extension/contract/describe.go` with the exact fields above, using `hookspkg.HookMode` and `hookspkg.HookMatcher`. Do not add command, environment, timeout, priority, or executor fields.

- [ ] **Step 4: Regenerate and verify SDK contracts**

```bash
make codegen
make codegen-check
go test ./internal/codegen/sdkgo ./internal/codegen/sdkts -count=1
```

Expected: generated files are deterministic and tests PASS.

- [ ] **Step 5: Commit the wire contract**

```bash
git add internal/extension/contract/describe.go internal/codegen/sdkgo/generate_test.go internal/codegen/sdkts/generate_test.go sdk/go/contracts sdk/typescript/src/generated/contracts.ts
git commit -m "feat(extension): describe required hooks"
```

### Task 2: Preserve declarations in both SDK runtimes

**Files:**
- Modify: `sdk/go/extension_describe_profiles.go`
- Modify: `sdk/go/runtime_test.go`
- Modify: `sdk/typescript/src/extension-describe.ts`
- Modify: `sdk/typescript/src/__tests__/extension.test.ts`

**Interfaces:**
- Consumes: caller-owned `[]DescribeHookEvent` / `DescribeHookEvent[]`.
- Produces: deterministic describe payloads and event-name-only initialize responses.

- [ ] **Step 1: Add failing Go normalization coverage**

Declare a hook with whitespace, `Mode: contracts.HookModeSync`, `Required: true`, and `Matcher.AgentName: " batuta-publisher "`. Assert `Describe()` emits trimmed name/profile/matcher, preserves mode/required, and does not mutate the input matcher. Assert initialize emits exactly `tool.pre_call` once.

- [ ] **Step 2: Add failing TypeScript describe coverage**

Extend the describe-mode test with:

```ts
supported_hook_events: [{
  name: " publisher-guard ",
  event: "tool.pre_call",
  profile: " delivery ",
  mode: "sync",
  matcher: { agent_name: " batuta-publisher " },
  required: true,
}],
```

Assert the emitted payload contains normalized values while the original object retains its whitespace.

- [ ] **Step 3: Run both SDK tests and prove RED**

```bash
go test ./sdk/go -run 'Describe|Hook' -count=1
bun test --cwd sdk/typescript src/__tests__/extension.test.ts
```

Expected: enriched fields are absent or unnormalized.

- [ ] **Step 4: Implement deep-copy normalization**

In Go, add:

```go
func normalizeDescribeHookEvent(event contracts.DescribeHookEvent) contracts.DescribeHookEvent
func cloneDescribeHookMatcher(matcher contracts.HookMatcher) contracts.HookMatcher
```

Trim represented string fields, clone pointer/nested matcher fields, and return fresh values. Sort declarations by profile, event, then normalized name and deduplicate only identical complete declarations. Change `describedHookEventNames` to collect event strings into a set before sorting so several declarations for one event advertise it once.

In TypeScript, clone and trim the matcher into a fresh object. Use a stable uniqueness key containing profile, event, name, mode, canonical matcher JSON, and required.

- [ ] **Step 5: Run SDK suites**

```bash
go test ./sdk/go/... -count=1
bun test --cwd sdk/typescript
```

Expected: PASS.

- [ ] **Step 6: Commit SDK normalization**

```bash
git add sdk/go/extension_describe_profiles.go sdk/go/runtime_test.go sdk/typescript/src/extension-describe.ts sdk/typescript/src/__tests__/extension.test.ts
git commit -m "feat(sdk): preserve hook declarations"
```

### Task 3: Generate complete manifest hooks and reject unsafe declarations

**Files:**
- Modify: `internal/extension/build_test.go`
- Modify: `internal/extension/build_describe.go`
- Modify: `internal/extension/manager_hook_declarations.go`

**Interfaces:**
- Consumes: enriched `extensioncontract.DescribeHookEvent` and the declared subprocess.
- Produces: `HookConfig` with normalized declaration fields and the fixed subprocess executor.

- [ ] **Step 1: Add a failing manifest-preservation test**

Build a describe payload for `tool.pre_call` with name `publisher-guard`, sync mode, matcher `AgentName: batuta-publisher`, and `Required: true`. Assert the generated `HookConfig` contains those exact values plus the describe subprocess command/args/env.

Add invalid cases for required async, explicit async mode on `tool.pre_call`, a matcher field unsupported by the event, and duplicate profile/name declarations. Assert `manifestFromDescribe` returns a validation error before publishing a generation.

- [ ] **Step 2: Run the focused builder test and prove RED**

```bash
go test ./internal/extension -run 'ManifestFromDescribe|Describe.*Hook' -count=1
```

Expected: matcher and required are lost and invalid enriched declarations are accepted.

- [ ] **Step 3: Add the inverse matcher conversion**

Add beside `hookConfigMatcher`:

```go
func hookMatcherConfigFromHookMatcher(matcher hookspkg.HookMatcher) HookMatcherConfig
```

It trims and copies every field represented by `HookMatcherConfig`, including `ToolReadOnly`, network matcher fields, and compaction matcher fields. It must not serialize internal-only matcher state that the manifest cannot represent.

- [ ] **Step 4: Preserve fields and use existing hook validation**

In `manifestHooksFromDescribe`, resolve defaults only when absent:

```go
name := strings.TrimSpace(described.Name)
if name == "" {
    name = strings.ReplaceAll(string(event), ".", "-")
}
mode := hookspkg.HookMode(strings.TrimSpace(string(described.Mode)))
if mode == "" {
    mode = hookspkg.HookModeAsync
    if event.SyncEligible() {
        mode = hookspkg.HookModeSync
    }
}
```

Populate `Required` and `Matcher`, keep the subprocess executor fixed, then pass the generated manifest through the same normalization/validation used for hand-authored hook resources.

- [ ] **Step 5: Run extension builder and manifest tests**

```bash
go test ./internal/extension -run 'Build|Manifest|Hook' -count=1
```

Expected: PASS, including legacy event-only fixtures.

- [ ] **Step 6: Commit manifest generation**

```bash
git add internal/extension/build_describe.go internal/extension/build_test.go internal/extension/manager_hook_declarations.go
git commit -m "feat(extension): generate scoped required hooks"
```

### Task 4: Prove fail-closed behavior from generated declaration to tool boundary

**Files:**
- Modify: `internal/daemon/native_extension_tool_provider_test.go`
- Modify: `internal/daemon/hook_binding_resources_integration_test.go` only if its existing harness is reused
- Modify: `internal/extension/testdata/secret-guard/main.go` only if the subprocess fixture needs a failing-hook mode

**Interfaces:**
- Consumes: an SDK-built generated required `tool.pre_call` hook matched to one agent.
- Produces: a failed tool result with zero calls to the protected tool handler.

- [ ] **Step 1: Add the integration fixture**

Build/install a fixture extension whose described hook is:

```go
compozysdk.DescribeHookEvent{
    Name: "publisher-guard",
    Event: contracts.HookEventToolPreCall,
    Mode: contracts.HookModeSync,
    Matcher: contracts.HookMatcher{AgentName: "batuta-publisher"},
    Required: true,
}
```

Its hook subprocess exits non-zero after receiving the payload. Register a protected tool whose handler increments an atomic counter.

- [ ] **Step 2: Prove RED at the runtime boundary**

Invoke the protected tool as `batuta-publisher`. Before the fix, expect the handler counter to become one because the generated hook is non-required or unmatched.

- [ ] **Step 3: Assert matched failure and non-match isolation**

After implementation, assert:

```text
matched batuta-publisher call -> structured failure; handler count remains 0
different agent call          -> tool succeeds; handler count becomes 1
```

Also assert the hook catalog reports mode `sync`, matcher agent name, extension source, and `required: true`.

- [ ] **Step 4: Run focused integration tests under race detection**

```bash
go test -race -tags=integration ./internal/daemon -run 'Required.*Extension.*Hook|ExtensionTool' -count=1
```

Expected: PASS without a pending process or goroutine leak.

- [ ] **Step 5: Commit fail-closed proof**

```bash
git add internal/daemon/native_extension_tool_provider_test.go internal/daemon/hook_binding_resources_integration_test.go internal/extension/testdata/secret-guard/main.go
git commit -m "test(extension): prove required hook containment"
```

### Task 5: Document, generate, and run release gates

**Files:**
- Modify: `skills/compozy/references/extension-authoring.md`
- Modify: `skills/compozy/references/extensions.md`
- Modify: `docs/qa/scenarios/ET-extension-manifest-v2-surfaces.md`
- Modify: `docs/qa/scenarios/ET-extension-code-first-authoring.md`

**Interfaces:**
- Consumes: shipped SDK declaration and fail-closed behavior.
- Produces: authoritative authoring guidance and QA expectations.

- [ ] **Step 1: Add exact Go and TypeScript declaration examples**

Document `tool.pre_call` with `mode: sync`, `matcher.agent_name`, and `required: true`. State that command/args/env come from the extension subprocess and that required failures block matched execution.

- [ ] **Step 2: Update QA scenarios**

Require generated-manifest inspection plus one live matched failure and one non-matching success. Keep event-only compatibility in the code-first authoring scenario.

- [ ] **Step 3: Format and run focused suites**

```bash
gofmt -w internal/extension/contract/describe.go internal/extension/build_describe.go internal/extension/build_test.go internal/extension/manager_hook_declarations.go sdk/go/extension_describe_profiles.go sdk/go/runtime_test.go internal/daemon/native_extension_tool_provider_test.go internal/daemon/hook_binding_resources_integration_test.go internal/extension/testdata/secret-guard/main.go
make codegen
make codegen-check
go test ./internal/extension ./internal/hooks ./sdk/go/... -count=1
bun test --cwd sdk/typescript
```

Expected: PASS and generated output clean.

- [ ] **Step 4: Cross-build and run repository gates**

Create a unique compiler scratch directory under `/home/francisross/tmp-builds`
and use it only for the cross-build:

```bash
build_tmp=$(mktemp -d -p /home/francisross/tmp-builds compozy-hooks.XXXXXX)
TMPDIR="$build_tmp" GOOS=windows GOARCH=amd64 go build ./internal/extension/... ./internal/hooks/... ./sdk/go/...
find "$build_tmp" -depth -delete
```

Then leave `TMPDIR` and `GOTMPDIR` unset:

```bash
make gate
make gate-full
git status --short
```

Expected: build and gates PASS; status contains only intended files.

- [ ] **Step 5: Commit docs and final generated state**

```bash
git add skills/compozy/references/extension-authoring.md skills/compozy/references/extensions.md docs/qa/scenarios/ET-extension-manifest-v2-surfaces.md docs/qa/scenarios/ET-extension-code-first-authoring.md sdk/go/contracts sdk/typescript/src/generated/contracts.ts
git commit -m "docs(extension): explain scoped required hooks"
```
