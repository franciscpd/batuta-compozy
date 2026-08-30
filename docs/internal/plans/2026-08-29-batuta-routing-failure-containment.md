# Batuta Routing Failure Containment Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use
> `superpowers:subagent-driven-development` (recommended) or
> `superpowers:executing-plans` to implement this plan task-by-task. Steps use
> checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make Batuta stop truthfully after a rejected routing proposal,
preserve actionable routing errors across `routing_apply`, and prevent an
unconfirmed or fabricated generation from reaching repository bootstrap.

**Architecture:** Keep Compozy unchanged and retain Batuta's current
planner/apply split. The Go extension maps planner-domain failures identically
at both public tool boundaries, while the Batuta agent treats a successful
`routing_plan` response as the only source of a generation digest and uses a
bounded terminal policy for unavailable backends. Static contracts and focused
Go tests lock the behavior; an isolated Batuta smoke validates the wire result
without mutating the shared Compozy installation.

**Tech Stack:** Go, Compozy Go extension SDK, Markdown agent/skill contracts,
Bash/Python contract tests.

**Spec:** Existing routing state machine in
[`agents/batuta/AGENT.md`](../../../agents/batuta/AGENT.md), routing guidance in
[`resources/skills/batuta-routing/SKILL.md`](../../../resources/skills/batuta-routing/SKILL.md),
and the diagnosed session `sess-23405b6af992a84f`.

## Global Constraints

- Batuta repository only. Do not edit, commit, stash, rebase, or generate files
  in the Compozy repository.
- Do not add migrations, stored configuration, hidden routing state, or a new
  tool operation.
- Preserve the current public nine-tool Batuta inventory and all JSON schemas.
- Preserve the user's untracked `docs/demo-e2e.md` byte-for-byte.
- Preserve and land the existing trusted CLI fallback correction separately
  before changing routing behavior.
- A routing generation digest is authoritative only when returned by a
  successful `ext__batuta__routing_plan` result for the retained
  byte-equivalent request.
- A rejected plan permits at most the documented single corrected retry. A
  second rejection is terminal and must cause zero `routing_apply`, Git,
  worktree, Loop, extension-lifecycle, or configuration mutations.
- A `tool_backend_failed` result with `backend_unhealthy` permits one
  `compozy__tool_info` read for the failing tool, then Batuta must report the
  blocker and stop. It must not reload, install, remove, validate, or inspect
  extension processes and daemon environments.
- Known residual outside this bounded correction: if a fresh replan succeeds
  but its digest differs from the caller's syntactically valid digest,
  `routing_runtime.go` still returns an untyped generation-change error. The
  agent-side provenance rule prevents that state in normal Batuta operation
  and the engine performs no mutation. A broader public conflict taxonomy must
  be planned separately rather than invented here.
- Use strict TDD: focused RED, minimal GREEN, focused race/vet/contract gates,
  diff review, then one Conventional Commit per task.

---

### Task 1: Land the trusted CLI fallback prerequisite

**Files:**
- Modify: `agents/batuta/AGENT.md`
- Modify: `tests/contract/test_02_domain_lane_surface.sh`

**Interfaces:**
- Consumes: current session, workspace, and agent identities supplied by the
  running Compozy session.
- Produces: the exact fallback command
  `compozy tool invoke <tool-id> --session <current-session-id> --workspace <current-workspace-id-or-path> --agent <current-agent-name> --input '<json>' -o json`.

- [ ] **Step 1: Verify the existing focused contract is GREEN**

Run:

```bash
rtk bash tests/contract/test_02_domain_lane_surface.sh
```

Expected: PASS with `--agent <current-agent-name>` required alongside session
and workspace identity.

- [ ] **Step 2: Review the prerequisite diff for scope**

Run:

```bash
rtk git diff -- agents/batuta/AGENT.md tests/contract/test_02_domain_lane_surface.sh
rtk git diff --check
```

Expected: the only behavioral change is preservation of agent identity in CLI
fallbacks. `docs/demo-e2e.md` is absent from the diff and index.

Treat this as a hard gate. If the two tracked files contain any other behavior,
stop and separate or re-scope it before committing; do not silently bundle it
under the fallback commit message.

- [ ] **Step 3: Commit the prerequisite independently**

```bash
rtk git add agents/batuta/AGENT.md tests/contract/test_02_domain_lane_surface.sh
rtk git commit -m "fix: preserve agent scope in tool fallback"
```

---

### Task 2: Preserve planner-domain errors through `routing_apply`

**Files:**
- Modify: `internal/extensionapp/routing_tools.go:74-148`
- Modify: `internal/extensionapp/routing_tools_test.go`

**Interfaces:**
- Consumes: errors returned by `serviceSet.routingApply`, including wrapped
  `routing.ErrClassificationRetryable`, `routing.ErrSelectionRetryable`,
  `routing.ErrCatalogDrift`, `routing.ErrReauthoringRequired`, and
  `routing.ErrNoEligibleCandidate`.
- Produces: the existing `*compozysdk.RPCError` contract with code `-32010`,
  data code `tool_invalid_input`, and the same ordered `reason_codes` already
  emitted by `routing_plan`.

- [ ] **Step 1: Write the handler RED test**

Add a table-driven regression beside
`TestRoutingPlanReturnsActionableDomainErrors`:

```go
func TestRoutingApplyReturnsActionableReplanErrors(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name, reason, secondary string
		err                     error
	}{
		{
			name:      "model below floor",
			reason:    "routing_fit_retryable",
			secondary: "model_below_floor",
			err: fmt.Errorf(
				"%w: candidate rejected: model_below_floor",
				routing.ErrSelectionRetryable,
			),
		},
		{
			name:   "catalog drift",
			reason: "catalog_drift",
			err:    routing.ErrCatalogDrift,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			app := application{services: serviceSet{routingApply: func(
				context.Context, publication.TrustedScope, RoutingApplyInput,
			) (RoutingApplyOutput, error) {
				return RoutingApplyOutput{}, tt.err
			}}}
			_, err := app.routingApply(
				context.Background(),
				&compozysdk.ExtensionToolWorkspaceScope{ID: "ws_demo", Root: "/workspace"},
				RoutingApplyInput{
					Operation:                RoutingOperationAlignmentStatus,
					RoutingPlan:              &RoutingPlanInput{Slug: "demo"},
					ExpectedGenerationDigest: digestValue("generation"),
				},
			)
			var rpcErr *compozysdk.RPCError
			if !errors.As(err, &rpcErr) || rpcErr.Code != -32010 {
				t.Fatalf("routingApply() error = %#v, want canonical RPC error", err)
			}
			var data struct {
				Code        string   `json:"code"`
				ReasonCodes []string `json:"reason_codes"`
			}
			if json.Unmarshal(rpcErr.Data, &data) != nil ||
				data.Code != "tool_invalid_input" ||
				len(data.ReasonCodes) == 0 || data.ReasonCodes[0] != tt.reason {
				t.Fatalf("routingApply() error data = %s", rpcErr.Data)
			}
			if tt.secondary != "" &&
				(len(data.ReasonCodes) != 2 || data.ReasonCodes[1] != tt.secondary) {
				t.Fatalf("routingApply() error data = %s", rpcErr.Data)
			}
		})
	}
}
```

- [ ] **Step 2: Verify the handler test fails for the observed reason**

Run:

```bash
rtk go test ./internal/extensionapp \
  -run 'TestRouting(PlanReturnsActionableDomainErrors|ApplyReturnsActionableReplanErrors)$' \
  -count=1
```

Expected: `TestRoutingApplyReturnsActionableReplanErrors` fails because the
apply handler returns the raw Go error instead of `*compozysdk.RPCError`.

- [ ] **Step 3: Add a no-mutation engine RED test**

Add this focused invariant using a catalog-missing candidate and a counting
bootstrap seam:

```go
func TestRoutingApplyRejectedReplanNeverBootstrapsRepository(t *testing.T) {
	t.Parallel()
	root := t.TempDir()
	writeRoutingTask(t, root)
	bootstrapCalls := 0
	engine := routingEngine{
		inventory: func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error) {
			return routingInventory(t, nil), nil
		},
		bootstrapRepository: func(context.Context, string) (repository.BootstrapResult, error) {
			bootstrapCalls++
			return repository.BootstrapResult{}, nil
		},
	}
	input := routingPlanFixture()
	input.Fit[0].Candidates[0].ProviderID = "missing-provider"
	input.Fit[0].Candidates[0].ModelID = "missing-model"
	_, err := engine.Apply(context.Background(), publication.TrustedScope{
		WorkspaceID: "ws_demo", WorkspaceRoot: root,
	}, RoutingApplyInput{
		Operation:                RoutingOperationBootstrapRepository,
		RoutingPlan:              &input,
		ExpectedGenerationDigest: digestValue("invented"),
	})
	if !errors.Is(err, routing.ErrSelectionRetryable) {
		t.Fatalf("Apply() error = %v, want ErrSelectionRetryable", err)
	}
	if bootstrapCalls != 0 {
		t.Fatalf("bootstrap calls = %d, want 0", bootstrapCalls)
	}
}
```

This test only proves that every rejected replan precedes repository mutation;
the handler table above preserves the exact observed `model_below_floor` wire
classification.

- [ ] **Step 4: Verify the no-mutation RED/characterization**

Run:

```bash
rtk go test ./internal/extensionapp \
  -run 'TestRoutingApplyRejectedReplanNeverBootstrapsRepository$' -count=1
```

Expected: the test proves `bootstrapCalls == 0`. If the existing engine already
satisfies it, record it as a characterization GREEN; the handler RED remains
the required failing proof.

- [ ] **Step 5: Reuse the existing domain mapping at the apply boundary**

Make the smallest handler change:

```go
output, err := a.services.routingApply(ctx, scope, input)
if err != nil {
	if rpcErr := routingPlanDomainError(err); rpcErr != nil {
		return compozysdk.ToolResult{}, rpcErr
	}
	return compozysdk.ToolResult{}, err
}
```

Do not translate unrelated executor crashes, filesystem failures, or internal
programming errors into input errors. Do not change the public schemas.

- [ ] **Step 6: Verify focused GREEN and affected package quality**

```bash
rtk go test ./internal/extensionapp \
  -run 'TestRouting(PlanReturnsActionableDomainErrors|ApplyReturnsActionableReplanErrors|ApplyRejectedReplanNeverBootstrapsRepository)$' \
  -count=1
rtk go test -race ./internal/extensionapp -run 'RoutingPlan|RoutingApply' -count=1
rtk go vet ./internal/extensionapp
rtk git diff --check
```

- [ ] **Step 7: Commit the wire-contract fix**

```bash
rtk git add internal/extensionapp/routing_tools.go internal/extensionapp/routing_tools_test.go
rtk git commit -m "fix: preserve routing rejection details"
```

---

### Task 3: Make rejected routing a hard agent boundary

**Files:**
- Modify: `agents/batuta/AGENT.md:92-148`
- Modify: `resources/skills/batuta-routing/SKILL.md`
- Modify: `tests/contract/test_02_domain_lane_surface.sh`

**Interfaces:**
- Consumes: `routing_plan` success output containing the byte-equivalent
  request plus `digest`, or a typed rejection containing `reason_codes`.
- Produces: either a confirmed routing generation that may reach
  `bootstrap_repository`, or a terminal operator-language blocker with zero
  delivery mutation.

- [ ] **Step 1: Write contract RED assertions**

Add assertions for both `agent` and `skill` text. Use flattened text for line
wrapping but require these exact concepts:

```python
for text in (agent_flat, skill_flat):
    assert "a successful `routing_plan` result is the only authority" in text.lower()
    assert "copy its returned generation digest verbatim" in text.lower()
    assert "never construct, hash, infer, or reuse a digest" in text.lower()
    assert "a second routing rejection is terminal" in text.lower()
    assert "zero `routing_apply` calls" in text.lower()
    assert "one `compozy__tool_info` read" in text.lower()
    assert "stop and report the blocker" in text.lower()
    assert "never call extension reload, install, remove, validate, or logs" in text.lower()
    assert "never inspect daemon or extension process environments" in text.lower()

assert routing_section.index("successful `routing_plan` result") < \
       routing_section.index("operation `alignment_status`")
assert "operation `confirm_alignment`" in routing_section
assert "operation `bootstrap_repository`" in delivery_section
assert agent.index("operation `confirm_alignment`") < \
       agent.index("operation `bootstrap_repository`")
```

Also retain an absence regression guard for language that treats
`git_backed:false` as a reason to ignore `routing_fit_retryable` or
`model_below_floor`. This absence guard is not part of the expected RED; the
new digest-provenance and terminal-behavior assertions are the failing proof.

- [ ] **Step 2: Verify the contract fails**

```bash
rtk bash tests/contract/test_02_domain_lane_surface.sh
```

Expected: FAIL because digest provenance and terminal backend behavior are not
yet explicit in both agent and skill contracts.

- [ ] **Step 3: Encode the bounded state machine**

In both the agent and skill routing sections, state these transitions without
introducing a new tool or hidden state:

```text
PLAN_PENDING
  routing_plan success -> PLAN_ACCEPTED(request, returned digest)
  routing_fit_retryable -> FILTER_EXACT_CANDIDATES -> RETRY_ONCE
  second rejection / no eligible candidate -> BLOCKED (zero routing_apply)

PLAN_ACCEPTED
  alignment_status -> DISPLAY_MATRIX -> CLARIFY_APPROVE_OR_ADJUST
  explicit approval -> confirm_alignment
  confirmed -> BOOTSTRAP_ALLOWED

ANY_TOOL_CALL
  backend_unhealthy -> one tool_info read -> BLOCKED
```

Require copying the successful response digest verbatim. Explicitly forbid
using an inventory digest, request hash, guessed constant, previous session
digest, or digest from a rejected response. Require the same retained request
for `alignment_status`, confirmation, bootstrap, and apply.

For `backend_unhealthy`, allow only one read-only `compozy__tool_info` call for
the exact failing tool. Then report tool ID, operation, reason codes, and the
last successful state. Forbid `compozy__extensions_reload`, install, remove,
validate, logs, `doctor`, `/proc`, daemon environment inspection, repeated
probes, and unrelated tool calls. Runtime repair is an operator responsibility,
not Batuta delivery behavior.

- [ ] **Step 4: Verify contract GREEN**

```bash
rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk bash -n tests/contract/*.sh scripts/*.sh
rtk git diff --check
```

- [ ] **Step 5: Commit the agent-state correction**

```bash
rtk git add agents/batuta/AGENT.md resources/skills/batuta-routing/SKILL.md \
  tests/contract/test_02_domain_lane_surface.sh
rtk git commit -m "fix: stop Batuta after routing rejection"
```

---

### Task 4: Qualify the observed failure without touching Compozy source

**Files:**
- Create: `docs/internal/qa/2026-08-29-batuta-routing-failure-containment.md`

**Interfaces:**
- Consumes: the staged Batuta candidate, an isolated Compozy runtime, a
  disposable non-Git workspace, and a deliberately below-floor routing
  proposal.
- Produces: reproducible QA evidence proving actionable wire errors, zero
  repository mutation, and bounded agent behavior. It does not qualify healthy
  delivery or publication.

- [ ] **Step 1: Run the complete local Batuta gates**

Use unique compiler scratch without changing test temp semantics:

```bash
build_tmp="$(mktemp -d -p /home/francisross/tmp-builds batuta-routing.XXXXXX)"
TMPDIR="$build_tmp" rtk go test ./... -count=1
TMPDIR="$build_tmp" rtk go test -race ./internal/extensionapp -run 'RoutingPlan|RoutingApply' -count=1
TMPDIR="$build_tmp" rtk go vet ./...
rtk bash -n tests/contract/*.sh scripts/*.sh
rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk git diff --check
```

Remove only `$build_tmp` after every command has exited and its path has been
checked to start with `/home/francisross/tmp-builds/batuta-routing.`.

- [ ] **Step 2: Stage the Batuta extension into an isolated runtime**

Create a unique isolated home, daemon port, and disposable workspace. Do not
reuse `~/.compozy`, the production daemon, or the shared demo workspace. Stage
the current Batuta checkout with `scripts/stage-extension.sh`, install it only
into that isolated runtime, and record:

- Batuta full commit SHA plus dirty-diff digest if the candidate is not yet
  committed;
- Compozy version and full commit SHA;
- staged extension version and package digest;
- workspace ID and root;
- the exact nine Batuta tool IDs and health state.

- [ ] **Step 3: Exercise the exact rejected-plan wire path**

Invoke `ext__batuta__routing_plan` with a valid task classification and a live
provider/model candidate known to be below the required tier. Assert:

```json
{
  "code": "tool_invalid_input",
  "reason_codes": ["routing_fit_retryable", "model_below_floor"]
}
```

Then invoke `routing_apply/alignment_status` with the same rejected request and
a syntactically valid decoy digest. Assert it returns the same actionable
planner-domain classification rather than `tool_backend_failed` or
`backend_unhealthy`.

Snapshot before and after:

```text
workspace file tree
.git existence
Compozy worktree list
Loop run list
Batuta routing journal projection
extension generation and PID
```

Require exact equality and specifically require that `.git` remains absent.

- [ ] **Step 4: Run one bounded model-facing session smoke**

Start one Batuta session in the isolated non-Git workspace with an approved SDD
fixture that reaches routing. Feed the same below-floor proposal condition and
assert the transcript contains:

- one initial `routing_plan` call;
- at most one corrected retry;
- zero `routing_apply`, Git, worktree, Loop, config mutation, and extension
  lifecycle calls after the terminal rejection;
- one concise blocker naming `routing_fit_retryable` and
  `model_below_floor`; and
- no invented generation digest.

Bound the session command with an external timeout. If the model does not reach
the routing boundary deterministically, record `blocked-verify`; do not weaken
the assertions, inject hidden state, or modify Compozy.

- [ ] **Step 5: Teardown and record QA truthfully**

Stop the isolated daemon, remove the isolated extension/workspace through
public commands where available, confirm no owned PIDs or scratch roots remain,
and delete only task-created scratch. Record commands, exit codes, exact
identities, before/after snapshots, and whether the model smoke passed or
remained `blocked-verify`.

- [ ] **Step 6: Commit QA evidence only after successful deterministic gates**

```bash
rtk git add docs/internal/qa/2026-08-29-batuta-routing-failure-containment.md
rtk git commit -m "test: qualify routing failure containment"
```

Do not commit session dumps containing prompts, credentials, absolute personal
configuration, raw environment variables, or Compozy state directories.

---

## Final Review and Stop Condition

- [ ] Request an independent code review after Tasks 1-4.
- [ ] Re-run only the affected focused gates after valid review fixes.
- [ ] Stop when planner-domain failures are actionable at both tools, the agent
  cannot reach `routing_apply` without a successful returned digest and explicit
  alignment, and the isolated no-mutation proof is recorded.
- [ ] Do not expand into Compozy changes, extension lifecycle redesign, general
  error taxonomy, routing-policy retuning, or delivery/publication behavior.
