# Batuta Delivery Launcher Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Make `start_delivery` durably create a lightweight `batuta-deliver` launcher inside the existing 30-second boundary, then reconcile the exact `batuta-deliver-core` child without changing Compozy.

**Architecture:** `batuta-deliver` becomes a one-child envelope whose only executable action is reserved `run-loop`; the existing graph moves intact to `batuta-deliver-core`. Batuta adds a fixed envelope protocol marker and explicit child budget inputs, records only the launcher run ID, and resolves terminal graph evidence through a fail-closed launcher-to-core ownership check while retaining direct-run compatibility for marker-free historical runs.

**Tech Stack:** Go 1.26, Compozy Loop YAML, `gopkg.in/yaml.v3`, Bash contract tests, Python assertions embedded in contract tests.

**Spec:** `docs/internal/specs/2026-09-01-batuta-delivery-launcher-design.md`

## Global Constraints

- Make no Compozy source, configuration, database, or runtime-contract change.
- Keep the existing 30-second CLI deadline; do not solve this by increasing a timeout.
- Keep the workspace journal and its lock as the sole delivery state and idempotency boundary.
- Journal and return the public `batuta-deliver` launcher run ID, never an untrusted core ID.
- Use fixed `delivery_envelope_version: 1`; absence means a legacy direct run and any other present value fails closed.
- Preserve the core task-wave, review, publication, terminalization, budget, cleanup, and manual-merge behavior.
- Do not reload the operator's live Batuta extension or retry the incident delivery during implementation.
- Prefix every shell command with `rtk` as required by `/home/francisross/.codex/RTK.md`.
- Route build/test scratch of unknown size through a unique directory below `/home/francisross/tmp-builds`; do not export `TMPDIR` globally.
- Never run `tests/contract/run.sh` from a checkout containing `.compozy/`; use a disposable detached worktree.

## File Structure

- `loops/batuta-deliver/loop.yaml`: public lightweight launcher, terminal hooks, and exact child forwarding.
- `loops/batuta-deliver-core/loop.yaml`: internal extension-heavy delivery graph, without origin-session hooks.
- `main_test.go`: structural contracts for the launcher, core, and existing task Loop.
- `internal/extensionapp/delivery_client.go`: fixed protocol constant, exact CLI input serialization, validation, and start-response matching.
- `internal/extensionapp/delivery_client_test.go`: exact argv/config/response and unsafe-input regressions.
- `internal/extensionapp/delivery_envelope.go`: pure launcher-version, request reconstruction, core ownership, and terminal-compatibility logic.
- `internal/extensionapp/delivery_envelope_test.go`: focused table tests for legacy and launcher/core evidence.
- `internal/extensionapp/routing_recovery.go`: use the reconstructed request and validated core detail at the existing settlement boundary.
- `internal/extensionapp/routing_recovery_test.go`: journal/replay/reconciliation regressions and legacy fixtures.
- `internal/extensionapp/delivery_integration_test.go`: end-to-end service progression using a launcher and exact core child.
- `scripts/stage-extension.sh`, `scripts/republish.sh`, `.github/workflows/release.yml`: package and inventory the third Loop.
- `tests/contract/test_01_stage.sh`, `tests/contract/test_01_republish.sh`, `tests/contract/test_02_domain_lane_surface.sh`, `tests/contract/test_03_lifecycle.sh`, `tests/contract/test_04_deliver_validate.sh`, `tests/contract/test_07_license.sh`, `tests/contract/test_07_public_docs.sh`, `tests/contract/test_07_workflow_contract.sh`: public-package and live-isolated launcher contracts.
- `README.md`, `README.pt-BR.md`, `docs/architecture.md`, `docs/architecture.pt-BR.md`, `docs/how-it-works.md`, `docs/how-it-works.pt-BR.md`, `docs/verify.md`, `docs/verify.pt-BR.md`, `docs/demo-e2e.md`, `agents/batuta/AGENT.md`, `resources/skills/batuta-routing/SKILL.md`: authoritative inventory, run identity, and operator flow.

---

### Task 1: Split the public launcher from the delivery core

**Files:**
- Create: `loops/batuta-deliver-core/loop.yaml`
- Modify: `loops/batuta-deliver/loop.yaml`
- Modify: `main_test.go:116`

**Interfaces:**
- Consumes: the current `batuta-deliver` graph and Compozy's reserved `run-loop` action.
- Produces: launcher node `delivery_core`, child Loop name `batuta-deliver-core`, and the immutable/budget input names consumed by Tasks 2–4.

- [ ] **Step 1: Replace the current parent test with failing launcher and core structural tests**

Add `TestDeliveryLauncherDefersExtensionGraphToExactCore` and rename the existing graph test to `TestDeliveryCoreRunsDependencySafeTaskWavesWithBoundedChildOverrides`. The launcher test must decode both YAML files and assert this exact public boundary:

```go
if launcher.Meta.Name != "batuta-deliver" || launcher.Concurrency != "queue" {
	t.Fatalf("launcher identity = %#v", launcher)
}
for _, name := range []string{
	"delivery_envelope_version", "delivery_id", "attempt", "slug",
	"origin_session_id", "worktree_ref", "routing_generation",
	"absolute_deadline", "token_ceiling", "recovery_operation_id",
	"iteration_cap", "budget_tokens", "budget_wall_seconds",
} {
	if input, exists := launcher.Inputs[name]; !exists || !input.Required {
		t.Fatalf("missing launcher input %q", name)
	}
}
if len(launcher.Graph.Nodes) != 1 || launcher.Graph.Nodes[0].ID != "delivery_core" ||
	launcher.Graph.Nodes[0].Kind != "run-loop" ||
	launcher.Graph.Nodes[0].Params.Loop != "batuta-deliver-core" {
	t.Fatalf("launcher graph = %#v", launcher.Graph)
}
if strings.Contains(string(launcherPayload), "ext__") {
	t.Fatal("public launcher resolves a hosted extension action")
}
if strings.Contains(string(corePayload), "compozy__session_prompt") {
	t.Fatal("core owns an origin-session terminal hook")
}
```

The child override assertions must require `iteration_cap`, `budget_tokens`, and `budget_wall_sec` to come from the three launcher inputs, require both halt policies, and require all thirteen launcher inputs to be forwarded to the core. The existing topology assertions must read `loops/batuta-deliver-core/loop.yaml`, require `meta.name == "batuta-deliver-core"`, and otherwise retain every current graph node, edge, fan-out, review, publication, and cleanup assertion.

- [ ] **Step 2: Run the structural tests and prove the old single Loop fails**

Run:

```bash
rtk go test . -run 'TestDelivery(Launcher|Core)' -count=1
```

Expected: FAIL because `loops/batuta-deliver-core/loop.yaml` does not exist and the public Loop still contains `ext__*` actions.

- [ ] **Step 3: Add the core definition and write the minimal public launcher**

Create `loops/batuta-deliver-core/loop.yaml` from the current public definition, then make only these core changes:

```yaml
meta:
  name: batuta-deliver-core
  description: Batuta executes the internal validated delivery graph through bounded task waves, one final review, and one publication boundary.

inputs:
  delivery_envelope_version: {type: number, required: true}
  delivery_id: {type: string, required: true}
  attempt: {type: number, required: true}
  slug: {type: string, required: true}
  origin_session_id: {type: string, required: true}
  worktree_ref: {type: string, required: true}
  routing_generation: {type: string, required: true}
  absolute_deadline: {type: string, required: true}
  token_ceiling: {type: number, required: true}
  recovery_operation_id: {type: string, required: true}
  iteration_cap: {type: number, required: true}
  budget_tokens: {type: number, required: true}
  budget_wall_seconds: {type: number, required: true}
```

Delete only the core's `contract.on_done`, `on_noop`, `on_blocked`, `on_failed`, `on_exhausted`, `on_stalled`, and `on_canceled` entries. Preserve its `contract.iteration_cap: 64`, graph, terminal states, start kinds, and every execution edge unchanged.

Replace `loops/batuta-deliver/loop.yaml` with the public envelope below:

```yaml
apiVersion: compozy.loop/v1
kind: Loop
meta:
  name: batuta-deliver
  description: Batuta creates one durable public delivery envelope and supervises its exact internal core run.
  catalog:
    use_when: "You have approved tasks and an applied Batuta routing generation ready for guarded autonomous delivery."
    keywords: [batuta, deliver, launcher, route, graph]
    category: Engineering

concurrency: queue

inputs:
  delivery_envelope_version: {type: number, required: true}
  delivery_id: {type: string, required: true}
  attempt: {type: number, required: true}
  slug: {type: string, required: true}
  origin_session_id: {type: string, required: true}
  worktree_ref: {type: string, required: true}
  routing_generation: {type: string, required: true}
  absolute_deadline: {type: string, required: true}
  token_ceiling: {type: number, required: true}
  recovery_operation_id: {type: string, required: true}
  iteration_cap: {type: number, required: true}
  budget_tokens: {type: number, required: true}
  budget_wall_seconds: {type: number, required: true}

contract:
  goal: >
    Supervise the exact batuta-deliver-core run for delivery {{ .inputs.delivery_id }}
    attempt {{ .inputs.attempt }} and return its terminal outcome to the origin session.
  definition_of_done: >
    Exactly one batuta-deliver-core child with matching immutable inputs reached a terminal outcome.
  iteration_cap: 1
  budget: {tokens: 0, wall_clock_sec: 14400, on_exceeded: halt}
  stop_when: "nodes.delivery_core.status == 'succeeded'"
  terminal_states: [done, no-op, blocked, failed, canceled, exhausted, stalled]
  on_done:
    - &return_to_origin
      tool: compozy__session_prompt
      with:
        session_id: "{{ .inputs.origin_session_id }}"
        message_id: "batuta-terminal-{{ .inputs.delivery_id }}-{{ .effect.identity.loop_run_id }}-g{{ .effect.identity.generation }}-{{ .effect.identity.trigger }}"
        idempotency_key: "batuta-terminal-{{ .inputs.delivery_id }}-{{ .effect.identity.loop_run_id }}-g{{ .effect.identity.generation }}-{{ .effect.identity.trigger }}"
        mode: queue
        message: |
          Batuta delivery_id {{ .inputs.delivery_id }} launcher run
          {{ .effect.identity.loop_run_id }} reached trigger {{ .effect.identity.trigger }} in generation {{ .effect.identity.generation }}.
          Read this exact launcher once with compozy__loop_status, then call ext__batuta__routing_apply
          reconcile_fallbacks with delivery_id {{ .inputs.delivery_id }} and delivery_run_id {{ .effect.identity.loop_run_id }}.
          If recoverable, call recover_delivery exactly once with those same identities and end the turn.
          Otherwise report the authoritative core result, commits, reviewed HEAD, publication operation IDs,
          verified PR URL, blocker, and that merge remains manual.
  on_noop: [*return_to_origin]
  on_blocked: [*return_to_origin]
  on_failed: [*return_to_origin]
  on_exhausted: [*return_to_origin]
  on_stalled: [*return_to_origin]
  on_canceled: [*return_to_origin]

start:
  - {kind: manual}
  - {kind: cli}
  - {kind: http}
  - {kind: uds}
  - {kind: native_tool}
  - {kind: schedule}

graph:
  nodes:
    - id: delivery_core
      class: action
      kind: run-loop
      produces: {loop_run_id: string, status: string}
      params:
        loop: batuta-deliver-core
        config_overrides:
          iteration_cap: "{{ .inputs.iteration_cap }}"
          budget_tokens: "{{ .inputs.budget_tokens }}"
          budget_wall_sec: "{{ .inputs.budget_wall_seconds }}"
          budget_on_exceeded: halt
          reattempt_strategy: halt
        inputs:
          delivery_envelope_version: "{{ .inputs.delivery_envelope_version }}"
          delivery_id: "{{ .inputs.delivery_id }}"
          attempt: "{{ .inputs.attempt }}"
          slug: "{{ .inputs.slug }}"
          origin_session_id: "{{ .inputs.origin_session_id }}"
          worktree_ref: "{{ .inputs.worktree_ref }}"
          routing_generation: "{{ .inputs.routing_generation }}"
          absolute_deadline: "{{ .inputs.absolute_deadline }}"
          token_ceiling: "{{ .inputs.token_ceiling }}"
          recovery_operation_id: "{{ .inputs.recovery_operation_id }}"
          iteration_cap: "{{ .inputs.iteration_cap }}"
          budget_tokens: "{{ .inputs.budget_tokens }}"
          budget_wall_seconds: "{{ .inputs.budget_wall_seconds }}"
```

- [ ] **Step 4: Run the structural tests and both Loop lint checks**

Run:

```bash
rtk go test . -run 'TestDelivery(Launcher|Core)' -count=1
rtk compozy loop validate --file loops/batuta-deliver/loop.yaml -o json
rtk compozy loop validate --file loops/batuta-deliver-core/loop.yaml -o json
```

Expected: the Go tests PASS and both JSON results contain `"valid": true`. These commands validate definitions only; do not install or reload Batuta.

- [ ] **Step 5: Commit the Loop boundary**

```bash
rtk git add main_test.go loops/batuta-deliver/loop.yaml loops/batuta-deliver-core/loop.yaml
rtk git commit -m "fix: defer delivery graph behind launcher"
```

---

### Task 2: Add an exact launcher protocol to the delivery client

**Files:**
- Modify: `internal/extensionapp/delivery_client.go:18`
- Modify: `internal/extensionapp/delivery_client_test.go:18`

**Interfaces:**
- Consumes: the Task 1 input names and fixed launcher Loop name `batuta-deliver`.
- Produces: `deliveryEnvelopeVersion`, `deliveryIdentityMatchesRequest`, and exact modern `deliveryRunMatchesRequest` used by Task 3.

- [ ] **Step 1: Write failing client tests for protocol and budget inputs**

Extend `TestDeliveryClientUsesExactBoundedCommandsAndSecureConfig` so `wantStartPrefix` includes these four arguments before `--config-file`:

```go
"--input", "delivery_envelope_version=1",
"--input", "iteration_cap=3",
"--input", "budget_tokens=750000",
"--input", "budget_wall_seconds=7200",
```

Extend `deliveryInputs` with the same typed values. Add table rows to `TestDeliveryClientRejectsUnsafeInputsAndMalformedResponses` that mutate each returned marker/budget field independently and expect `started delivery does not match the requested attempt`. Add one request-validation row for each out-of-range budget already enforced by `validateDeliveryStartRequest`, proving no command executes.

- [ ] **Step 2: Run the client test and prove serialization is missing**

Run:

```bash
rtk go test ./internal/extensionapp -run '^TestDeliveryClient' -count=1
```

Expected: FAIL because the start argv and response matcher do not include the marker or explicit child budgets.

- [ ] **Step 3: Implement the fixed modern request contract**

Add the protocol constant beside the command constants:

```go
const deliveryEnvelopeVersion int64 = 1
```

Serialize the new inputs in `deliveryLoopCLIClient.Start`:

```go
"--input", "delivery_envelope_version=" + strconv.FormatInt(deliveryEnvelopeVersion, 10),
"--input", "iteration_cap=" + strconv.Itoa(request.IterationCap),
"--input", "budget_tokens=" + strconv.FormatInt(request.BudgetTokens, 10),
"--input", "budget_wall_seconds=" + strconv.Itoa(request.BudgetWallSec),
```

Split the matcher so historical identity can be reused without weakening modern starts:

```go
func deliveryIdentityMatchesRequest(run deliveryRun, request deliveryStartRequest) bool {
	values := run.Inputs
	return stringInput(values, "delivery_id") == request.DeliveryID &&
		intInput(values, "attempt") == int64(request.Attempt) &&
		stringInput(values, "slug") == request.Slug &&
		stringInput(values, "origin_session_id") == request.OriginSessionID &&
		stringInput(values, "worktree_ref") == request.WorktreeRef &&
		stringInput(values, "routing_generation") == request.RoutingGeneration &&
		stringInput(values, "absolute_deadline") == request.AbsoluteDeadline.Format(time.RFC3339) &&
		intInput(values, "token_ceiling") == request.TokenCeiling &&
		stringInput(values, "recovery_operation_id") == request.RecoveryOperationID
}

func deliveryRunMatchesRequest(run deliveryRun, request deliveryStartRequest) bool {
	return deliveryIdentityMatchesRequest(run, request) &&
		intInput(run.Inputs, "delivery_envelope_version") == deliveryEnvelopeVersion &&
		intInput(run.Inputs, "iteration_cap") == int64(request.IterationCap) &&
		intInput(run.Inputs, "budget_tokens") == request.BudgetTokens &&
		intInput(run.Inputs, "budget_wall_seconds") == int64(request.BudgetWallSec)
}
```

Keep the secure config file unchanged; it bounds the launcher itself while the new inputs let that launcher apply the same values to its child.

- [ ] **Step 4: Run client tests, including race detection**

Run:

```bash
rtk go test -race ./internal/extensionapp -run '^TestDeliveryClient' -count=1
```

Expected: PASS, including exact argv, `0600` config cleanup, malformed-response rejection, and cancellation propagation.

- [ ] **Step 5: Commit the wire protocol**

```bash
rtk git add internal/extensionapp/delivery_client.go internal/extensionapp/delivery_client_test.go
rtk git commit -m "fix: bind delivery launcher inputs"
```

---

### Task 3: Prove launcher-to-core ownership before settlement

**Files:**
- Create: `internal/extensionapp/delivery_envelope.go`
- Create: `internal/extensionapp/delivery_envelope_test.go`
- Modify: `internal/extensionapp/routing_recovery.go:145`
- Modify: `internal/extensionapp/routing_recovery_test.go:574`
- Modify: `internal/extensionapp/routing_context_test.go:117`

**Interfaces:**
- Consumes: `deliveryEnvelopeVersion`, `deliveryIdentityMatchesRequest`, `deliveryRunMatchesRequest`, `deliveryRunDetail`, and `deliveryAttemptService.Client.Status`.
- Produces:
  - `deliveryRequestForAttempt(delivery routing.DeliveryRecord, attempt routing.DeliveryAttempt) (deliveryStartRequest, error)`;
  - `deliveryEnvelopeVersionOf(run deliveryRun) (int64, bool)`;
  - `(deliveryAttemptService).settlementParentDetail(context.Context, string, deliveryStartRequest, deliveryRunDetail) (deliveryRunDetail, error)`.

- [ ] **Step 1: Write failing request-reconstruction tests**

Create `delivery_envelope_test.go` with a fixture containing one terminal prior attempt and one planned second attempt. Assert the exact reconstruction:

```go
request, err := deliveryRequestForAttempt(delivery, delivery.Attempts[1])
if err != nil {
	t.Fatalf("deliveryRequestForAttempt() error = %v", err)
}
if request.Attempt != 2 || request.RecoveryOperationID != delivery.Attempts[1].OperationID ||
	request.IterationCap != 64 || request.BudgetTokens != delivery.TokenCeiling-delivery.Attempts[0].TokensUsed ||
	request.BudgetWallSec != int(delivery.AbsoluteDeadline.Sub(delivery.Attempts[1].PlannedAt)/time.Second) {
	t.Fatalf("reconstructed request = %#v", request)
}
```

Cover attempt 1's empty recovery ID, legacy graph iteration cap `4`, invalid attempt position/state, negative prior usage, token exhaustion, and non-positive wall budget.

- [ ] **Step 2: Write failing table tests for exact core ownership**

Use launcher node ID `delivery_core` and table cases with these expected results:

```go
tests := []struct {
	name      string
	launcher  deliveryRunDetail
	core      deliveryRunDetail
	wantRunID string
	wantErr   error
}{
	{name: "valid launcher", wantRunID: "run_core"},
	{name: "legacy direct run", wantRunID: "run_legacy"},
	{name: "unsupported present version", wantErr: routing.ErrDeliveryConflict},
	{name: "missing core output", wantErr: routing.ErrDeliveryConflict},
	{name: "duplicate core output", wantErr: routing.ErrDeliveryConflict},
	{name: "empty core id", wantErr: routing.ErrDeliveryConflict},
	{name: "foreign workspace", wantErr: routing.ErrDeliveryConflict},
	{name: "wrong parent", wantErr: routing.ErrDeliveryConflict},
	{name: "wrong loop", wantErr: routing.ErrDeliveryConflict},
	{name: "nonterminal core", wantErr: routing.ErrDeliveryConflict},
	{name: "contradictory core inputs", wantErr: routing.ErrDeliveryConflict},
	{name: "success output with failed core", wantErr: routing.ErrDeliveryConflict},
	{name: "failed launcher with successful core", wantErr: routing.ErrDeliveryConflict},
}
```

Assert that every invalid launcher case performs at most one child status read and returns no adoptable core detail.

- [ ] **Step 3: Run the focused envelope tests and prove the helpers are absent**

Run:

```bash
rtk go test ./internal/extensionapp -run 'TestDelivery(RequestForAttempt|Envelope)' -count=1
```

Expected: FAIL to compile because the three envelope interfaces do not exist.

- [ ] **Step 4: Implement request reconstruction and envelope validation**

Implement `deliveryRequestForAttempt` by deriving all values only from the journaled delivery and attempt. Subtract only terminal prior-attempt token usage and compute `BudgetWallSec` from `attempt.PlannedAt`, not the reconciliation clock. Return `routing.ErrDeliveryConflict` for malformed ordering or exhausted reconstructed budgets.

Implement protocol detection without treating malformed present data as absence:

```go
func deliveryEnvelopeVersionOf(run deliveryRun) (int64, bool) {
	_, present := run.Inputs["delivery_envelope_version"]
	if !present {
		return 0, false
	}
	return intInput(run.Inputs, "delivery_envelope_version"), true
}
```

Implement `settlementParentDetail` with this decision sequence:

```go
version, present := deliveryEnvelopeVersionOf(launcher.Run)
if !present {
	return launcher, nil
}
if version != deliveryEnvelopeVersion || !deliveryRunMatchesRequest(launcher.Run, request) {
	return deliveryRunDetail{}, routing.ErrDeliveryConflict
}
// Collect exactly one output whose NodeID is "delivery_core" and whose
// status is "succeeded" or "failed", then read its non-empty child ID once.
```

Validate the child ID, workspace, `ParentLoopRunID`, `LoopName == "batuta-deliver-core"`, exact modern inputs, terminal status, and output/status compatibility. A successful child (`done` or `no-op`) requires output `succeeded` and launcher `done`/`no-op`; every other terminal child requires output `failed` and a launcher with the same status or `failed`. Return the core detail only after all checks pass.

Update `runMatchesAttempt` to reconstruct the request. Marker-free runs use `deliveryIdentityMatchesRequest`; present marker runs require version `1` and `deliveryRunMatchesRequest`.

Update `deliveryAttemptService.start` to build its `deliveryStartRequest` with `deliveryRequestForAttempt` after the planned attempt exists. Keep the existing request digest, remaining-budget, recent-run, lock, and journal transition order unchanged.

- [ ] **Step 5: Make existing direct-parent fixtures explicitly legacy**

Add this test helper and use it for old direct graph details in both
`routing_recovery_test.go` and `routing_context_test.go`:

```go
func legacyDeliveryInputs(request deliveryStartRequest) map[string]any {
	inputs := deliveryInputs(request)
	delete(inputs, "delivery_envelope_version")
	delete(inputs, "iteration_cap")
	delete(inputs, "budget_tokens")
	delete(inputs, "budget_wall_seconds")
	return inputs
}
```

Do not allow production code to infer legacy mode from missing core outputs; only absence of `delivery_envelope_version` selects the legacy path.

- [ ] **Step 6: Run envelope, client, and start/replay tests with race detection**

Run:

```bash
rtk go test -race ./internal/extensionapp -run 'TestDelivery(Client|RequestForAttempt|Envelope|AttemptServiceStarts|AttemptServiceAdopts|AttemptServiceBlocks)' -count=1
```

Expected: PASS; recent adoption accepts one exact modern launcher, rejects contradictory budgets/version, and legacy fixtures remain usable only in reconciliation.

- [ ] **Step 7: Commit the ownership boundary**

```bash
rtk git add internal/extensionapp/delivery_envelope.go internal/extensionapp/delivery_envelope_test.go internal/extensionapp/routing_recovery.go internal/extensionapp/routing_recovery_test.go internal/extensionapp/routing_context_test.go
rtk git commit -m "fix: verify delivery launcher core"
```

---

### Task 4: Reconcile graph evidence through the validated core

**Files:**
- Modify: `internal/extensionapp/routing_recovery.go:246`
- Modify: `internal/extensionapp/routing_recovery_test.go:57`
- Modify: `internal/extensionapp/delivery_integration_test.go:50`

**Interfaces:**
- Consumes: `deliveryRequestForAttempt` and `settlementParentDetail` from Task 3.
- Produces: public reconciliation that retains `attempt.RunID == launcher.ID` while all graph usage and terminal state derive from the validated core.

- [ ] **Step 1: Add failing reconciliation tests for the public/core identity split**

Add a helper that installs one terminal launcher and exact child into the fake client:

```go
func setLauncherAndCore(
	t *testing.T,
	fixture deliveryServiceFixture,
	launcherID string,
	launcherStatus string,
	coreStatus string,
	coreOutputs []deliveryOutput,
) string {
	t.Helper()
	coreID := launcherID + "_core"
	request := fixture.client.lastRequest
	fixture.client.statuses[launcherID] = deliveryRunDetail{
		Run: deliveryRun{ID: launcherID, WorkspaceID: fixture.scope.WorkspaceID,
			LoopName: "batuta-deliver", Status: launcherStatus, Inputs: deliveryInputs(request)},
		Generations: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{{
			NodeID: "delivery_core", Status: launcherOutputStatus(coreStatus), ChildLoopRunID: coreID,
		}}}},
	}
	fixture.client.statuses[coreID] = deliveryRunDetail{
		Run: deliveryRun{ID: coreID, WorkspaceID: fixture.scope.WorkspaceID,
			ParentLoopRunID: launcherID, LoopName: "batuta-deliver-core",
			Status: coreStatus, Inputs: deliveryInputs(request)},
		Generations: []deliveryGeneration{{Generation: 1, Outputs: coreOutputs}},
	}
	return coreID
}

func launcherOutputStatus(coreStatus string) string {
	if coreStatus == "done" || coreStatus == "no-op" {
		return "succeeded"
	}
	return "failed"
}
```

Add assertions that:

- a running launcher returns `in_progress` without reading a core;
- a terminal valid launcher reads exactly one core and then existing review/graph children;
- journal `RunID` and public `DeliveryRunID` stay equal to the launcher ID;
- `TerminalStatus`, recoverability, blocker mapping, graph/review token usage, publication mutation, and failed task IDs derive from the core;
- launcher `done`, `no-op`, `failed`, `blocked`, `exhausted`, `stalled`, and `canceled` combinations preserve the current terminal transition rules after compatibility validation;
- missing/duplicate/foreign/nonterminal/contradictory core evidence leaves the attempt `submitted` and journal graph unchanged;
- a marker-free legacy direct run still follows the current path.

Add `TestDeliveryAttemptServiceConcurrentInstancesCreateOneLauncher`. Construct
two `deliveryAttemptService` values with the same `OwnershipStore` and a
mutex-protected client whose first `Start` blocks on a channel. Start service A,
wait until its client call begins, start service B, release service A, and
require both results to carry the same launcher ID while the shared client's
`startCalls` remains exactly `1`. Run this test under `-race`; it proves the
journal lock remains held through durable launcher creation.

- [ ] **Step 2: Run the reconciliation tests and prove graph evidence still comes from the launcher**

Run:

```bash
rtk go test ./internal/extensionapp -run 'TestDeliveryAttemptService.*(Launcher|Core|Legacy)' -count=1
```

Expected: FAIL because `Reconcile` currently passes the launcher detail directly to `graphParentUsage` or `settlementEvidence`.

- [ ] **Step 3: Route only terminal settlement through the exact core**

Keep the initial `Status` read and launcher identity check. After proving the launcher is terminal, reconstruct the request and resolve settlement evidence:

```go
request, err := deliveryRequestForAttempt(delivery, attempt)
if err != nil {
	return err
}
settlementParent, err := s.settlementParentDetail(
	ctx, scope.WorkspaceID, request, detail,
)
if err != nil {
	return err
}
```

Use `settlementParent` for `graphOwnsParentSettlement`, `parentReportsGraphTask`, `graphParentUsage`, `settlementEvidence`, and terminal status mapping. Keep `attempt.RunID` and `RoutingReconcileResult.DeliveryRunID` untouched. Persist `attempt.TerminalStatus = settlementParent.Run.Status`; this preserves exact core outcomes when a non-success child is surfaced as launcher `failed`.

- [ ] **Step 4: Update the service integration progression to model launcher and core runs**

In `delivery_integration_test.go`, make `integrationDeliveryClient.Start` continue returning only the launcher. Replace each direct terminal parent fixture with a launcher output `delivery_core` plus a core detail holding the old delivery graph outputs. Assert the journal stores only launcher IDs and that core IDs never appear in `DeliveryAttempt.RunID` or the public start/reconcile results.

Keep the existing two-attempt fallback, task/runtime changes, worktree fingerprint, review usage, publication, and final state assertions unchanged.

- [ ] **Step 5: Run the complete extensionapp package with race detection**

Run:

```bash
build_tmp=$(rtk mktemp -d -p /home/francisross/tmp-builds batuta-launcher-go.XXXXXX)
TMPDIR="$build_tmp" rtk go test -race ./internal/extensionapp -count=1
rtk rm -rf -- "$build_tmp"
```

Expected: PASS with no race report. The test output must include the launcher/core and legacy reconciliation tests.

- [ ] **Step 6: Commit reconciled settlement behavior**

```bash
rtk git add internal/extensionapp/routing_recovery.go internal/extensionapp/routing_recovery_test.go internal/extensionapp/delivery_integration_test.go
rtk git commit -m "fix: reconcile delivery core evidence"
```

---

### Task 5: Ship and validate the internal core resource

**Files:**
- Modify: `scripts/stage-extension.sh:26`
- Modify: `scripts/republish.sh:84`
- Modify: `.github/workflows/release.yml:239`
- Modify: `tests/contract/test_01_stage.sh:15`
- Modify: `tests/contract/test_01_republish.sh:50`
- Modify: `tests/contract/test_02_domain_lane_surface.sh:8`
- Modify: `tests/contract/test_03_lifecycle.sh:88`
- Modify: `tests/contract/test_04_deliver_validate.sh:36`
- Modify: `tests/contract/test_07_license.sh:163`
- Modify: `tests/contract/test_07_workflow_contract.sh:337`

**Interfaces:**
- Consumes: the two delivery Loop definitions and launcher protocol from Tasks 1–2.
- Produces: an installed inventory containing exactly `batuta-deliver`, `batuta-deliver-core`, and `batuta-task`, plus an isolated smoke test that observes core execution through the launcher.

- [ ] **Step 1: Add failing package and inventory expectations for the third Loop**

Add `./loops/batuta-deliver-core/loop.yaml` to both exact staged-file lists. Add `('loop','batuta-deliver-core')` or `("loop", "batuta-deliver-core")` to every exact inventory set in the files above, including the release workflow contract. Update the fake republish inventory response to return the core resource as live.

Change `test_02_domain_lane_surface.sh` to read both files:

```python
launcher = Path("loops/batuta-deliver/loop.yaml").read_text(encoding="utf-8")
core = Path("loops/batuta-deliver-core/loop.yaml").read_text(encoding="utf-8")
assert "kind: ext__" not in launcher
assert "loop: batuta-deliver-core" in launcher
assert re.search(r"iteration_cap:\s*64\b", core)
assert "auto_commit: true" in core
```

- [ ] **Step 2: Run the non-live package contracts and prove the core is not staged**

Run:

```bash
rtk bash tests/contract/test_01_stage.sh
rtk bash tests/contract/test_01_republish.sh
rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk bash tests/contract/test_07_license.sh
rtk bash tests/contract/test_07_workflow_contract.sh
```

Expected: at least stage/republish/license FAIL because `scripts/stage-extension.sh` and inventory producers still omit `batuta-deliver-core`.

- [ ] **Step 3: Stage and publish the exact third Loop**

Add this staging line between the public launcher and task Loop:

```bash
cp -R -- "$ROOT/loops/batuta-deliver-core" "$STAGE/loops/"
```

Add the core tuple to the exact inventory in `scripts/republish.sh` and `.github/workflows/release.yml`. Do not change the nine hosted-tool set, agent count, skill count, release triggers, or publication behavior.

- [ ] **Step 4: Rewrite the isolated delivery smoke around launcher/core lineage**

In `test_04_deliver_validate.sh`:

- validate both YAML files before install;
- invoke the public launcher with `timeout 30s` and require it to return a run,
  matching
  the production Batuta command deadline;
- pass all four new public inputs (`delivery_envelope_version=1`, `iteration_cap=1`, `budget_tokens=1000`, `budget_wall_seconds=30`);
- retain `SMOKE_RUN_ID` as the launcher and add `SMOKE_CORE_RUN_ID`;
- poll launcher status until exactly one `delivery_core` output supplies a child ID;
- read that exact child and require `loop_name == "batuta-deliver-core"`, `parent_loop_run_id == SMOKE_RUN_ID`, and exact forwarded inputs;
- poll the core detail until `load_check` succeeds and `routing_context` has a task run in `retrying`, `failed`, or `blocked`;
- cancel the core first and launcher second during cleanup;
- assert the launcher contains one `run-loop` and zero `ext__*`, while the core retains two nested `run-loop` actions and the current graph topology.

The Python lineage assertion must include:

```python
assert core_run["loop_name"] == "batuta-deliver-core", core_run
assert core_run["parent_loop_run_id"] == launcher_id, core_run
for key in (
    "delivery_envelope_version", "delivery_id", "attempt", "slug",
    "origin_session_id", "worktree_ref", "routing_generation",
    "absolute_deadline", "token_ceiling", "recovery_operation_id",
    "iteration_cap", "budget_tokens", "budget_wall_seconds",
):
    assert core_run["inputs"][key] == launcher_run["inputs"][key], key
```

- [ ] **Step 5: Run the non-live package contracts again**

Run:

```bash
rtk bash tests/contract/test_01_stage.sh
rtk bash tests/contract/test_01_republish.sh
rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk bash tests/contract/test_07_license.sh
rtk bash tests/contract/test_07_workflow_contract.sh
```

Expected: PASS. Defer the live `test_03_lifecycle.sh` and `test_04_deliver_validate.sh` to Task 7's isolated daemon gate so the operator's active profile is never touched.

- [ ] **Step 6: Commit packaging and contract changes**

```bash
rtk git add .github/workflows/release.yml scripts/stage-extension.sh scripts/republish.sh tests/contract/test_01_stage.sh tests/contract/test_01_republish.sh tests/contract/test_02_domain_lane_surface.sh tests/contract/test_03_lifecycle.sh tests/contract/test_04_deliver_validate.sh tests/contract/test_07_license.sh tests/contract/test_07_workflow_contract.sh
rtk git commit -m "test: ship delivery core resource"
```

---

### Task 6: Align Batuta's operator contract and public documentation

**Files:**
- Modify: `agents/batuta/AGENT.md:223`
- Modify: `resources/skills/batuta-routing/SKILL.md:208`
- Modify: `README.md:10`
- Modify: `README.pt-BR.md:11`
- Modify: `docs/architecture.md:14`
- Modify: `docs/architecture.pt-BR.md:16`
- Modify: `docs/how-it-works.md:66`
- Modify: `docs/how-it-works.pt-BR.md:74`
- Modify: `docs/verify.md:43`
- Modify: `docs/verify.pt-BR.md:48`
- Modify: `docs/demo-e2e.md:11`
- Modify: `tests/contract/test_07_public_docs.sh:80`
- Modify: `tests/contract/test_02_domain_lane_surface.sh:90`

**Interfaces:**
- Consumes: public launcher identity and three-Loop package inventory.
- Produces: operator guidance that always reconciles the launcher ID and describes the core as internal durable execution, never as a caller-supplied identity.

- [ ] **Step 1: Write failing documentation assertions**

Require the public docs and resource contracts to contain all of these concepts in English and Portuguese where a translated document exists:

```text
three Loops / três Loops
batuta-deliver-core
launcher run / run launcher
journal stores the launcher ID / journal armazena o ID do launcher
core child / filho core
```

Keep assertions for nine hosted tools, manual merge, immutable routing, bounded recovery, and automatic publication unchanged. Replace assertions for “fresh parent run ID” only where that phrase now ambiguously identifies the heavy graph; require “fresh launcher run ID” there.

- [ ] **Step 2: Run public-surface contracts and prove the old two-Loop wording fails**

Run:

```bash
rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk bash tests/contract/test_07_public_docs.sh
```

Expected: FAIL on the new launcher/core and three-Loop wording.

- [ ] **Step 3: Update the authoritative agent and routing skill**

State that `start_delivery` and `recover_delivery` return a durable public `batuta-deliver` launcher ID. Terminal handling reads and submits that exact launcher ID; the guarded tool alone validates its exact `batuta-deliver-core` child. Keep the instruction to end the turn after durable acceptance and keep direct starts unsupported.

Replace the delivery description with this ownership chain:

```text
batuta-deliver launcher (journaled public identity)
  -> exactly one batuta-deliver-core child (validated internally)
     -> dependency-safe batuta-task waves
     -> one review-and-fix child
     -> publication, verification, and cleanup
```

Do not instruct the Batuta agent to inspect, infer, or pass a core run ID.

- [ ] **Step 4: Update inventory, architecture, operation, verification, and demo docs**

Change all current inventory statements from two to three Loops and list the internal core. Update architecture diagrams so the launcher is the parent returned to the conversation and the core owns the existing graph. Update verification file lists to include `loops/batuta-deliver-core/loop.yaml`. Update the demo checklist/catalog instructions to display all three Loops and identify `batuta-deliver-core` as internal execution, not a manual entrypoint.

Do not edit historical release notes, superseded specs, prior plans, case-study evidence, or QA/handoff records; those documents describe pinned historical artifacts.

- [ ] **Step 5: Run documentation and resource contracts**

Run:

```bash
rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk bash tests/contract/test_07_public_docs.sh
rtk go test . -run 'TestDelivery(Launcher|Core)|TestBatutaTask' -count=1
```

Expected: PASS with consistent English/Portuguese inventory and launcher identity.

- [ ] **Step 6: Commit the operator contract**

```bash
rtk git add agents/batuta/AGENT.md resources/skills/batuta-routing/SKILL.md README.md README.pt-BR.md docs/architecture.md docs/architecture.pt-BR.md docs/how-it-works.md docs/how-it-works.pt-BR.md docs/verify.md docs/verify.pt-BR.md docs/demo-e2e.md tests/contract/test_02_domain_lane_surface.sh tests/contract/test_07_public_docs.sh
rtk git commit -m "docs: explain delivery launcher boundary"
```

---

### Task 7: Run focused and isolated acceptance gates

**Files:**
- Verify only; modify a task-owned file only if a failing gate exposes a launcher-scope defect.

**Interfaces:**
- Consumes: all Tasks 1–6.
- Produces: test evidence for the Batuta-only acceptance criteria without modifying the operator's live extension or incident journal.

- [ ] **Step 1: Run formatting and diff hygiene**

Run:

```bash
rtk gofmt -w internal/extensionapp/delivery_client.go internal/extensionapp/delivery_client_test.go internal/extensionapp/delivery_envelope.go internal/extensionapp/delivery_envelope_test.go internal/extensionapp/routing_recovery.go internal/extensionapp/routing_recovery_test.go internal/extensionapp/delivery_integration_test.go main_test.go
rtk git diff --check
rtk git status --short
```

Expected: `git diff --check` exits zero and status contains only intended Batuta files.

- [ ] **Step 2: Run all Go tests with race detection**

Run:

```bash
build_tmp=$(rtk mktemp -d -p /home/francisross/tmp-builds batuta-launcher-all.XXXXXX)
TMPDIR="$build_tmp" rtk go test -race ./... -count=1
rtk rm -rf -- "$build_tmp"
```

Expected: every package PASS with no race report.

- [ ] **Step 3: Run shell syntax and non-live contract tests in the implementation worktree**

Run:

```bash
rtk bash -n scripts/*.sh tests/contract/*.sh
rtk bash tests/contract/test_01_stage.sh
rtk bash tests/contract/test_01_republish.sh
rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk bash tests/contract/test_07_license.sh
rtk bash tests/contract/test_07_public_docs.sh
rtk bash tests/contract/test_07_workflow_contract.sh
```

Expected: all commands PASS. None installs, reloads, or invokes the incident delivery.

- [ ] **Step 4: Require a clean implementation branch before the isolated gate**

If Steps 1–3 expose a defect, stop this verification task, return to the task
that owns the affected file, perform that task's red/green cycle, and use its
exact commit step. Once the owning task is green, run:

```bash
rtk git status --short
```

Expected: the final status is empty. Do not create an empty commit when no correction was needed.

- [ ] **Step 5: Create a disposable detached checkout for the full contract suite**

Run:

```bash
gate_parent=$(rtk mktemp -d -p /home/francisross/tmp-builds batuta-launcher-gate.XXXXXX)
gate_tree="$gate_parent/checkout"
rtk git worktree add --detach "$gate_tree" HEAD
rtk test ! -e "$gate_tree/.compozy"
rtk mkdir -p "$gate_parent/build-tmp" "$gate_parent/compozy-home"
```

Expected: the detached checkout exists at the exact HEAD under test and contains no `.compozy/` marker.

- [ ] **Step 6: Run the full suite against an isolated Compozy home**

Run from the disposable checkout:

```bash
COMPOZY_HOME="$gate_parent/compozy-home" rtk compozy daemon start
COMPOZY_HOME="$gate_parent/compozy-home" TMPDIR="$gate_parent/build-tmp" rtk bash tests/contract/run.sh
COMPOZY_HOME="$gate_parent/compozy-home" rtk compozy daemon stop
```

Expected: `=== todos os testes de contrato passaram ===`. The isolated delivery smoke must show one launcher and its exact core child reaching `routing_context`; it must not use the operator profile or the incident delivery ID.

- [ ] **Step 7: Remove only the disposable gate resources created in Step 5**

Run from the implementation worktree after the isolated daemon is stopped:

```bash
rtk git worktree remove "$gate_tree"
rtk rm -rf -- "$gate_parent"
```

Expected: `git worktree list` no longer contains the disposable checkout. Never remove `/home/francisross/tmp-builds` itself.

- [ ] **Step 8: Record final verification evidence without operational rollout**

Run:

```bash
rtk git status --short
rtk git log --oneline -8
```

Expected: clean status and the design plus implementation commits visible. Report the exact tests run and explicitly state that no operator-profile extension reload, incident retry, publication, release, or Compozy source/config change occurred.
