# Batuta Scoped LLM Publication Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Turn Batuta into a code-backed Go extension that owns clean-HEAD publication, permits its publisher LLM to call exactly one mutating capability, and automatically ends a clean delivery only after an independently verified pull request exists.

**Architecture:** A standard-library publication core owns command execution, trusted-workspace validation, worktree inspection, Git evidence, exit-plan classification, push/PR reconciliation, and final verification. A thin Compozy SDK boundary registers three tools, while the existing daemon-enforced agent tool allowlist limits the publisher to the single mutating tool. The existing resource kit remains bundled, while `batuta-deliver` becomes a literal-commit, plan → deterministic route → scoped Goal → verify graph. Only a deterministic pre-mutation blocker may route to bounded operator recovery; the healthy path has no human gate.

**Tech Stack:** Go 1.26.4, Compozy Go extension SDK, standard-library `os/exec` and `encoding/json`, Bash contract tests, Python `unittest`, Compozy Loop v1.

**Spec:** `docs/internal/specs/2026-08-23-batuta-scoped-llm-publication-design.md`

## Global Constraints

- The operational floor is the first published Compozy SDK/binary prerelease that contains the merged conjunctive runtime-routing contract from PR #475 and the existing extension-tool, trusted-workspace, and agent-allowlist surfaces consumed by Batuta.
- Do not require a new Compozy hook, recovery, configuration-CAS, migration, or extension-specific minimum-version contract for this plan.
- Do not commit a local `replace` directive, pseudo-version for an unpublished commit, vendored SDK fork, or manually authored subprocess manifest. If no qualifying SDK release exists, Tasks 1–4 may complete, but execution must stop before Task 5.
- All three tool inputs derive workspace and path from daemon-authenticated `TrustedWorkspace`; no tool accepts a workspace ID, path, repository URL, remote, destination URL, executable, or raw command. `publication_verify` may receive the publisher's claimed PR URL only inside its untrusted result envelope for comparison with freshly observed forge evidence.
- Invoke Compozy only through the manifest-provided `COMPOZY_EXECUTABLE`; invoke all subprocesses with `exec.CommandContext` and argument vectors, never a shell.
- `ext__batuta__publish_worktree` is the only tool the `batuta-publisher` agent may call. The Goal DSL has no `allowed_tools` field, so the existing exact agent allowlist is the publisher capability boundary; do not add a new Compozy Goal or hook surface for this feature.
- `batuta-deliver` has no `auto_commit` input and passes literal `true` to `implement-tasks` and `review-and-fix`.
- A publishable result is successful only with a real PR URL and exact expected remote HEAD; compare-only success is forbidden.
- GitHub distribution for the first executable beta is explicitly `linux/amd64`; local source builds remain host-native. Do not publish one Linux archive as if it were portable to macOS or Windows.
- Do not place a human gate on `publishable` or `nothing_to_publish`. Publication and PR opening are automatic after clean review. A deterministic pre-mutation blocker may route to one bounded recovery gate; post-mutation ambiguity stops terminally with durable operation IDs. Merge remains manual.
- Run `tests/contract/run.sh` only from a disposable detached worktree without a `.compozy/` marker.
- Every shell command starts with `rtk`. Keep heavy build scratch in a unique `/home/francisross/tmp-builds` directory, but leave `TMPDIR` and `GOTMPDIR` unset for Compozy `make gate*` and Go tests that need `/tmp` semantics.

---

### Task 1: Standard-library process and Git evidence boundary

**Files:**
- Create: `go.mod`
- Create: `internal/publication/command.go`
- Create: `internal/publication/command_test.go`
- Create: `internal/publication/git.go`
- Create: `internal/publication/git_test.go`
- Create: `internal/publication/executable.go`
- Create: `internal/publication/executable_test.go`

**Interfaces:**
- Produces: `CommandRunner.Run(context.Context, Command) (CommandResult, error)`, `ExecRunner`, `ExecutableResolver.Resolve(string) (string, error)`, `GitClient.Snapshot(context.Context, string) (GitSnapshot, error)`, and `GitClient.UpstreamHead(context.Context, string) (string, error)`.
- Consumes: no Compozy SDK dependency.

- [ ] **Step 1: Create the module and write the failing command-boundary tests**

Create `go.mod` without an SDK requirement:

```go
module github.com/franciscpd/batuta-compozy

go 1.26.4
```

In `internal/publication/command_test.go`, cover these literal cases:

```go
func TestExecRunnerUsesExactExecutableArgvAndDirectory(t *testing.T)
func TestExecRunnerBoundsStdoutAndStderrWhileProcessRuns(t *testing.T)
func TestExecRunnerReturnsBoundedStderrAndExitCode(t *testing.T)
func TestExecRunnerHonorsContextCancellation(t *testing.T)
func TestExecutableResolverReturnsAnAbsolutePath(t *testing.T)
func TestExecutableResolverRejectsMissingExecutable(t *testing.T)
```

The first test executes a temporary helper binary with arguments containing spaces, newlines, `$()`, semicolons, and backticks and asserts the helper receives each value as one unchanged argv element. The second emits more than 64 KiB on stderr and asserts the returned diagnostic is capped at 64 KiB. The third blocks until cancellation and asserts `errors.Is(err, context.Canceled)`.

- [ ] **Step 2: Verify RED**

Run:

```bash
rtk go test ./internal/publication -run 'TestExecRunner' -count=1
```

Expected: compilation fails because `ExecRunner`, `Command`, and `CommandResult` do not exist.

- [ ] **Step 3: Implement the exact command boundary**

Define:

```go
type Command struct {
	Executable string
	Args       []string
	Directory  string
	StdoutLimit int64
	StderrLimit int64
}

type CommandResult struct {
	Stdout   []byte
	Stderr   []byte
	ExitCode int
	StdoutTruncated bool
	StderrTruncated bool
}

type CommandRunner interface {
	Run(context.Context, Command) (CommandResult, error)
}

type ExecRunner struct{}
```

`ExecRunner.Run` must reject a blank or non-absolute executable and negative limits, call `exec.CommandContext(ctx, executable, args...)`, set only `cmd.Dir`, and attach bounded writers before the process starts so large or unbounded output is drained without materializing it. Zero limits select safe defaults of 1 MiB stdout and 64 KiB stderr. Preserve `context.Canceled`/`DeadlineExceeded`, expose truncation bits, and wrap nonzero exits with only the bounded stderr. It must not mutate environment, invoke a shell, or call `exec.LookPath` internally.

`ExecutableResolver` is the single startup boundary allowed to call `exec.LookPath`; it canonicalizes the result to an absolute path. `extensionapp.New` receives the resolved Git path explicitly, while the Compozy path comes only from the generated subprocess environment. Tests inject both paths and never depend on the host PATH.

- [ ] **Step 4: Write and verify the failing Git behavior tests**

In `git_test.go`, create real temporary repositories and cover:

```go
func TestGitClientSnapshotReturnsExactCleanBranchAndHead(t *testing.T)
func TestGitClientSnapshotReportsDirtyAndDetached(t *testing.T)
func TestGitClientUpstreamHeadReturnsTheRemoteTrackingSHA(t *testing.T)
func TestGitClientRejectsRelativeWorktreePath(t *testing.T)
```

Run:

```bash
rtk go test ./internal/publication -run 'TestGitClient' -count=1
```

Expected: compilation fails because `GitClient` and `GitSnapshot` do not exist.

- [ ] **Step 5: Implement Git evidence with fixed argv**

Define:

```go
type GitSnapshot struct {
	HeadSHA  string
	Branch   string
	Clean    bool
	Detached bool
}

type GitClient struct {
	Executable string
	Runner     CommandRunner
}
```

`Snapshot` runs, in the daemon-returned absolute directory, `git status --porcelain=v1`, `git rev-parse HEAD`, and `git symbolic-ref --quiet --short HEAD`. Exit code 1 from `symbolic-ref` means detached; every other nonzero result is an error. `UpstreamHead` runs `git rev-parse @{upstream}`. Validate returned SHAs with `^[0-9a-f]{40,64}$`; trim only command framing whitespace, never repository-derived title/body values.

- [ ] **Step 6: Verify GREEN and commit**

Run:

```bash
rtk go test ./internal/publication -count=1
rtk go test -race ./internal/publication -count=1
rtk go vet ./internal/publication
rtk git diff --check
```

Expected: all pass.

Commit:

```bash
rtk git add go.mod internal/publication/command.go internal/publication/command_test.go internal/publication/executable.go internal/publication/executable_test.go internal/publication/git.go internal/publication/git_test.go
rtk git commit -m "feat: add safe publication process boundary"
```

---

### Task 2: Trusted worktree resolution and publication planning

**Files:**
- Create: `internal/publication/types.go`
- Create: `internal/publication/compozy.go`
- Create: `internal/publication/compozy_test.go`
- Create: `internal/publication/plan.go`
- Create: `internal/publication/plan_test.go`

**Interfaces:**
- Consumes: Task 1 `CommandRunner`, `GitClient`, and `GitSnapshot`.
- Produces: `TrustedScope`, `CLIClient`, `PublicationPlanner.Plan(context.Context, TrustedScope, PlanInput) (PlanOutput, error)`, and stable JSON structs reused by Tasks 3–5. This planner only classifies publication safety; Compozy remains the owner of task decomposition and execution planning.

- [ ] **Step 1: Write failing CLI argv and decode tests**

Define the wished-for API in `compozy_test.go` and cover:

```go
func TestCLIClientInspectUsesTrustedWorkspaceAndOpaqueRef(t *testing.T)
func TestCLIClientExitPlanUsesTrustedWorkspaceAndOpaqueRef(t *testing.T)
func TestCLIClientPushAndPRKeepPrefillAsSingleArguments(t *testing.T)
func TestCLIClientRejectsMalformedStructuredOutput(t *testing.T)
```

The recording runner must receive these exact argv arrays. Flags precede a
literal `--` separator so an opaque ref can never be parsed as a flag:

```text
worktree inspect --workspace ws_trusted -o json -- wt_delivery
worktree exit --workspace ws_trusted -o json -- wt_delivery
worktree push --workspace ws_trusted -o json -- wt_delivery
worktree pr --workspace ws_trusted --title 'Feature $(touch nope)' --body 'line one; echo nope\nline two' --base main -o json -- wt_delivery
```

Use title/body literals containing newlines, quotes, `$()`, semicolons, and backticks; assert they remain single argv values. Add refs `--help`, `--workspace=foreign`, and `--` to the table and prove they remain positional after the separator or are rejected by canonical ref validation before execution.

- [ ] **Step 2: Verify RED**

Run:

```bash
rtk go test ./internal/publication -run 'TestCLIClient' -count=1
```

Expected: compilation fails because `CLIClient` is absent.

- [ ] **Step 3: Implement the typed Compozy boundary**

Define explicit wire projections matching Compozy's public JSON. Every field
is separate and has its exact snake-case tag; grouped untagged fields are
forbidden:

```go
type TrustedScope struct {
	WorkspaceID string `json:"workspace_id"`
	WorkspaceRoot string `json:"workspace_root"`
}
type Worktree struct {
	ID string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Branch string `json:"branch"`
	Path string `json:"path"`
	State string `json:"state"`
	BaseRef string `json:"base_ref,omitempty"`
}
type WorktreeStatus struct {
	Branch *string `json:"branch"`
	Detached *bool `json:"detached"`
	HeadSHA *string `json:"head_sha"`
	DirtyFiles *int `json:"dirty_files"`
	HasUpstream *bool `json:"has_upstream"`
	Ahead *int `json:"ahead"`
	AheadOfBase *int `json:"ahead_of_base"`
	Behind *int `json:"behind"`
	ReadError string `json:"read_error,omitempty"`
}
type WorktreeRepo struct {
	GitBacked bool `json:"git_backed"`
	GitAvailable bool `json:"git_available"`
	Diagnostic string `json:"diagnostic,omitempty"`
}
type WorktreeInspection struct {
	Worktree Worktree `json:"worktree"`
	Status *WorktreeStatus `json:"status"`
	Forge *ForgeStatus `json:"forge"`
	Repo WorktreeRepo `json:"repo"`
}
type ExitAction struct {
	Action string `json:"action"`
	Enabled bool `json:"enabled"`
	BlockedReason string `json:"blocked_reason,omitempty"`
	Publish bool `json:"publish,omitempty"`
	URL string `json:"url,omitempty"`
}
type ExitPlan struct {
	WorktreeID string `json:"worktree_id"`
	Primary string `json:"primary,omitempty"`
	Actions []ExitAction `json:"actions"`
	GlobalPauseCause string `json:"global_pause_cause,omitempty"`
	BrowserURL string `json:"browser_url,omitempty"`
	Forge *ForgeCapabilities `json:"forge,omitempty"`
	ForgeStatus *ForgeStatus `json:"forge_status,omitempty"`
	PRPrefill *PRPrefill `json:"pr_prefill,omitempty"`
	Base string `json:"base,omitempty"`
}
type ForgeCapabilities struct {
	Provider string `json:"provider"`
	DefaultBranch string `json:"default_branch,omitempty"`
}
type ForgeStatus struct {
	Provider string `json:"provider"`
	PRURL string `json:"pr_url,omitempty"`
}
type PRPrefill struct {
	Title string `json:"title,omitempty"`
	Body string `json:"body,omitempty"`
}
type Operation struct { OperationID string `json:"op_id"` }
type WorktreeClient interface {
	Inspect(context.Context, TrustedScope, string) (WorktreeInspection, error)
	ExitPlan(context.Context, TrustedScope, string) (ExitPlan, error)
	Push(context.Context, TrustedScope, string) (Operation, error)
	OpenPR(context.Context, TrustedScope, string, PRPrefill, string) (Operation, error)
}
type GitEvidence interface {
	Snapshot(context.Context, string) (GitSnapshot, error)
	UpstreamHead(context.Context, string) (string, error)
	CommitsAheadOfBase(context.Context, string, string) (int, error)
}
```

`CLIClient` stores an absolute `Executable` supplied by the extension environment and a `CommandRunner`. Reject blank workspace IDs, blank worktree refs, non-absolute executable paths, empty/mismatched worktree IDs, and malformed JSON. Do not accept path or workspace values in public input structs.

Decode literal fixtures copied from the public CLI contracts and re-encode the
planner result in tests, explicitly asserting keys `workspace_id`, `base_ref`,
`blocked_reason`, `global_pause_cause`, `browser_url`, `forge_status`,
`pr_prefill`, and `head_sha`.

- [ ] **Step 4: Write the failing planning table**

In `plan_test.go`, use literal inspection, Git, and exit-plan fixtures for:

```go
func TestPlannerRejectsMissingTrustedScope(t *testing.T)
func TestPlannerRejectsForeignWorkspaceRecord(t *testing.T)
func TestPlannerRejectsNonReadyOrRelativePath(t *testing.T)
func TestPlannerBlocksDirtyDetachedDriftedAndUnreadableGit(t *testing.T)
func TestPlannerBlocksDivergedBehindMissingRemoteAndMissingForge(t *testing.T)
func TestPlannerReturnsNothingToPublishForCleanBaseIdenticalBranch(t *testing.T)
func TestPlannerReturnsPublishableWithExactHeadPrefillAndForge(t *testing.T)
func TestPlannerRecognizesAnExistingPRWithoutInventingAnOperation(t *testing.T)
```

Each table row hand-authors the expected `Disposition` (`publishable`, `nothing_to_publish`, `blocked`) and blocker code. The publishable row requires: clean non-detached Git snapshot; inspection workspace ID equal to trusted ID; `state=ready`; absolute path; matching branch; exit plan worktree ID match; no global pause; serving forge with non-empty provider/default branch; and enabled `push`, enabled `open_pr`, or an existing `view_pr` URL.

- [ ] **Step 5: Verify RED**

Run:

```bash
rtk go test ./internal/publication -run 'TestPlanner' -count=1
```

Expected: compilation fails because `PublicationPlanner`, `PlanInput`, and `PlanOutput` are absent.

- [ ] **Step 6: Implement deterministic planning**

Define:

```go
type PlanInput struct { WorktreeRef string `json:"worktree_ref"` }
type Disposition string
const (
	DispositionPublishable Disposition = "publishable"
	DispositionNothing Disposition = "nothing_to_publish"
	DispositionBlocked Disposition = "blocked"
)
type PlanOutput struct {
	Disposition Disposition `json:"disposition"`
	WorktreeID string `json:"worktree_id"`
	Branch string `json:"branch"`
	BaseBranch string `json:"base_branch"`
	WorktreePath string `json:"worktree_path"`
	HeadSHA string `json:"head_sha"`
	Clean bool `json:"clean"`
	ExitPlan ExitPlan `json:"exit_plan"`
	Blockers []string `json:"blockers,omitempty"`
}
type PublicationPlanner struct { Compozy WorktreeClient; Git GitEvidence }
type BlockedPlanError struct { Plan PlanOutput }
```

`BlockedPlanError.Error()` returns only a bounded summary of its closed blocker
codes; callers recover the structured plan with `errors.As` and never parse the
message.

Use stable machine blocker values: `trusted_scope_missing`, `worktree_unavailable`, `workspace_mismatch`, `worktree_not_ready`, `worktree_path_invalid`, `git_unreadable`, `dirty_worktree`, `detached_head`, `branch_mismatch`, `head_invalid`, `exit_plan_mismatch`, `exit_plan_paused`, `branch_diverged`, `branch_behind`, `remote_missing`, `forge_unavailable`, and `publication_state_ambiguous`. Return `BlockedPlanError` carrying the structured blocked plan for expected unsafe posture. Malformed input and transport/decoding failures remain distinct errors. The SDK handler in Task 5 converts only expected pre-mutation `BlockedPlanError` values into a safe structured `blocked` plan so Task 6 can route them to bounded recovery. Transport, decoding, and unexpected failures remain tool failures and stop the Loop.

Treat tracking-upstream state separately from remote existence. A first publication legitimately has `has_upstream=false`, null `ahead`/`behind`, and a non-null `ahead_of_base`; the fresh exit plan's `push` row proves that an origin exists. After push, `has_upstream=true` and `ahead_of_base` is null. Classify both using `CommitsAheadOfBase` against the daemon-selected base, call `UpstreamHead` only when tracking exists, and cross-check `ahead_of_base` when Compozy supplies it.

- [ ] **Step 7: Verify GREEN and commit**

Run:

```bash
rtk go test ./internal/publication -count=1
rtk go test -race ./internal/publication -count=1
rtk go vet ./internal/publication
rtk git diff --check
```

Commit:

```bash
rtk git add internal/publication/types.go internal/publication/compozy.go internal/publication/compozy_test.go internal/publication/plan.go internal/publication/plan_test.go
rtk git commit -m "feat: plan trusted worktree publication"
```

---

### Task 3: Bounded push and pull-request state machine

**Files:**
- Create: `internal/publication/publish.go`
- Create: `internal/publication/publish_test.go`

**Interfaces:**
- Consumes: Task 2 `PublicationPlanner`, `WorktreeClient`, `GitEvidence`, `ExitPlan`, `Operation`, and `TrustedScope`.
- Produces: `Publisher.Publish(context.Context, TrustedScope, PublishInput) (PublishOutput, error)`.

- [ ] **Step 1: Write the failing state-machine tests**

Cover these independent behaviors with a scripted fake client and literal plan sequence:

```go
func TestPublisherRejectsHeadOrCleanlinessDriftBeforeMutation(t *testing.T)
func TestPublisherReturnsNothingToPublishWithoutCallingMutation(t *testing.T)
func TestPublisherReconcilesPushBeforeOpeningPR(t *testing.T)
func TestPublisherReturnsExistingPRWithoutDuplicateMutation(t *testing.T)
func TestPublisherDoesNotRetryAmbiguousPushBlindly(t *testing.T)
func TestPublisherReportsPushedButBlockedWhenPRCannotBeObserved(t *testing.T)
func TestPublisherRequiresRealPRURLAndExactUpstreamHead(t *testing.T)
func TestPublisherReturnsKnownOperationIDsOnDeadline(t *testing.T)
func TestPublisherRejectsMalformedExpectedHeadAndBoundsSummary(t *testing.T)
```

The successful sequence is: fresh plan `publishable/push`; `Push` returns `op-push`; exit plans progress from paused/push to enabled `open_pr`; `OpenPR` returns `op-pr`; exit plan progresses to `view_pr` with `https://github.com/acme/repo/pull/42`; `UpstreamHead` equals the expected SHA. Assert calls occur exactly once and in that order.

- [ ] **Step 2: Verify RED**

Run:

```bash
rtk go test ./internal/publication -run 'TestPublisher' -count=1
```

Expected: compilation fails because `Publisher` is absent.

- [ ] **Step 3: Implement the minimal bounded state machine**

Define:

```go
type PublishInput struct {
	WorktreeRef    string `json:"worktree_ref"`
	ExpectedHeadSHA string `json:"expected_head_sha"`
}
type PublishStatus string
const (
	PublishStatusPublished PublishStatus = "published"
	PublishStatusNothing PublishStatus = "nothing_to_publish"
	PublishStatusBlocked PublishStatus = "blocked"
)
type PublishOutput struct {
	Status PublishStatus `json:"status"`
	HeadSHA string `json:"head_sha"`
	OperationIDs []string `json:"op_ids"`
	PRURL string `json:"pr_url,omitempty"`
	Summary string `json:"summary"`
	LastExitPlan ExitPlan `json:"last_exit_plan"`
}
type Publisher struct {
	Planner PublicationPlanner
	Compozy WorktreeClient
	Git GitEvidence
	PollInterval time.Duration
}
```

Validate `expected_head_sha` as a lowercase 40–64 character hexadecimal SHA before planning. At entry, rerun planning and require a clean exact `expected_head_sha`. If no changes exist, return `nothing_to_publish` with zero operations. If an existing PR is visible and upstream equals expected, return `published` without mutating. Otherwise push exactly once, append the non-empty `op_id`, and poll fresh exit plans until `open_pr` is enabled or a PR is visible. Open the PR exactly once using the exit plan's title/body/base as separate argv fields, append its `op_id`, and poll until a non-empty absolute HTTPS PR URL is visible. Never treat `BrowserURL` or an `open_pr` action URL as success. On timeout or failure, return `blocked` with the last plan and every known operation ID; do not retry either mutation. Bound `Summary` to 1 KiB after UTF-8-safe truncation and never include raw command output.

- [ ] **Step 4: Verify GREEN and commit**

Run:

```bash
rtk go test ./internal/publication -run 'TestPublisher|TestPlanner|TestCLIClient|TestGitClient' -count=1
rtk go test -race ./internal/publication -count=1
rtk go vet ./internal/publication
rtk git diff --check
```

Commit:

```bash
rtk git add internal/publication/publish.go internal/publication/publish_test.go
rtk git commit -m "feat: publish worktree through bounded state machine"
```

---

### Task 4: Independent publication verifier

**Files:**
- Create: `internal/publication/verify.go`
- Create: `internal/publication/verify_test.go`

**Interfaces:**
- Consumes: Tasks 2–3 types and evidence clients.
- Produces: `Verifier.Verify(context.Context, TrustedScope, VerifyInput) (VerifyOutput, error)`.

- [ ] **Step 1: Write failing verifier tests**

Cover:

```go
func TestVerifierRejectsFabricatedOrMismatchedPublisherEvidence(t *testing.T)
func TestVerifierRejectsCompareURLAndMissingPR(t *testing.T)
func TestVerifierRejectsDirtyTreeHeadDriftAndUpstreamDrift(t *testing.T)
func TestVerifierAcceptsExactPublishedHeadAndObservedPR(t *testing.T)
func TestVerifierAcceptsGenuineNothingToPublishWithoutOperationClaims(t *testing.T)
func TestVerifierRejectsNothingToPublishWithOperationClaims(t *testing.T)
```

Use a publisher result containing a syntactically valid but different PR URL to prove the verifier reads current exit-plan evidence instead of trusting narrative output.

- [ ] **Step 2: Verify RED**

Run:

```bash
rtk go test ./internal/publication -run 'TestVerifier' -count=1
```

Expected: compilation fails because `Verifier` is absent.

- [ ] **Step 3: Implement independent verification**

Define:

```go
type VerifyInput struct {
	WorktreeRef string `json:"worktree_ref"`
	ExpectedHeadSHA string `json:"expected_head_sha"`
	PublisherResult PublishOutput `json:"publisher_result"`
}
type VerifyOutput struct {
	Verified bool `json:"verified"`
	Status PublishStatus `json:"status"`
	HeadSHA string `json:"head_sha"`
	PRURL string `json:"pr_url,omitempty"`
	Summary string `json:"summary"`
}
type Verifier struct { Planner PublicationPlanner; Git GitEvidence }
```

For `published`, rerun planning, require clean exact local HEAD, require upstream HEAD equal to expected, require a current `view_pr` action/forge status with an absolute HTTPS URL, and require it equal to the publisher result. For `nothing_to_publish`, require a fresh `nothing_to_publish` disposition, zero claimed operations, and no PR URL. `blocked`, unknown status, malformed URLs, missing evidence, or mismatch returns an error and `Verified=false`.

- [ ] **Step 4: Verify GREEN and commit**

Run:

```bash
rtk go test ./internal/publication -count=1
rtk go test -race ./internal/publication -count=1
rtk go vet ./internal/publication
rtk git diff --check
```

Commit:

```bash
rtk git add internal/publication/verify.go internal/publication/verify_test.go
rtk git commit -m "feat: verify publication independently"
```

---

### Task 5: Code-first extension tools on existing Compozy boundaries

**Files:**
- Modify: `go.mod`
- Create: `go.sum`
- Create: `main.go`
- Create: `main_test.go`
- Create: `internal/extensionapp/app.go`
- Create: `internal/extensionapp/app_test.go`
- Delete: `extension.toml`

**Interfaces:**
- Consumes: Tasks 1–4 publication services and the first qualifying released Compozy Go SDK.
- Produces: executable `batuta`; tools `ext__batuta__publication_plan`, `ext__batuta__publish_worktree`, and `ext__batuta__publication_verify`; generated manifest and bundled resources using the released SDK contract.

- [ ] **Step 1: Enforce the release gate before changing dependencies**

Query the public Go module registry and remote repository tags. Select one
exact prerelease identity that exists both as `sdk/go/vX` and `vX`, contains
the merged PR #475 runtime-routing contract, and retains the existing
extension-tool, trusted-workspace, and agent-allowlist surfaces used here. In
a unique temporary audit repository under `/home/francisross/tmp-builds`,
fetch only the selected tags and prerequisite history, prove both tags contain
the merge, then remove only that audit directory:

```bash
rtk go list -m -versions github.com/compozy/compozy/sdk/go
rtk git -C /home/francisross/Projects/opensource/compozy ls-remote --tags origin
rtk git -C <temporary-audit-repository> merge-base --is-ancestor <pr-475-merge> sdk/go/<qualifying-version>^{commit}
rtk git -C <temporary-audit-repository> merge-base --is-ancestor <pr-475-merge> <qualifying-version>^{commit}
```

Expected: one published matching SDK/binary prerelease pair. If it is absent,
record `BLOCKED: qualifying Compozy SDK and binary release unavailable` in the
SDD task report and stop this plan here. Do not create a local tag, pseudo
version, SDK fork, or dependency workaround.

- [ ] **Step 2: Write failing executable entrypoint tests**

In `main_test.go`, test the executable startup seam without duplicating SDK
transport tests:

```go
func TestRunRejectsMissingOrRelativeCompozyExecutable(t *testing.T)
func TestRunResolvesGitOnceAndStartsInjectedExtension(t *testing.T)
```

The test injects the executable resolver and extension runner, proving startup
passes only absolute resolved paths into `extensionapp.New`. Provider
transport behavior remains owned by the published SDK.

- [ ] **Step 3: Verify RED**

Run:

```bash
rtk go test . -run 'TestRun' -count=1
```

Expected: compilation fails because the executable startup seam does not exist.

- [ ] **Step 4: Pin the qualifying SDK and implement the executable entrypoint**

Add the exact released module version proven in Step 1 to `go.mod`; record that
literal version in the task report and all Batuta floor checks:

```bash
rtk go get github.com/compozy/compozy/sdk/go@<qualifying-version-from-step-1>
```

`main.go` requires an absolute `COMPOZY_EXECUTABLE`, resolves `git` once through
Task 1's `ExecutableResolver`, constructs the SDK definition, and delegates
describe/runtime transport to the published SDK. It contains no hook
multiplexer, shell fallback, local SDK fork, or handwritten manifest.

- [ ] **Step 5: Write failing declaration and tool tests**

In `internal/extensionapp/app_test.go`, use the SDK's in-memory transport to assert:

```go
func TestDefinitionShipsResourcesAndResolvedCompozyExecutable(t *testing.T)
func TestDefinitionPinsBatutaBetaFive(t *testing.T)
func TestDefinitionRegistersExactPublicationToolDescriptors(t *testing.T)
func TestToolHandlersRejectMissingTrustedWorkspace(t *testing.T)
func TestToolHandlersPassOnlyDaemonTrustedScopeToServices(t *testing.T)
```

The definition declares `Version: "0.1.0-beta.5"`, `agents`,
`resources/skills`, and `loops`, subprocess `./bin`, and env
`COMPOZY_EXECUTABLE={{compozy_executable}}`. It uses the released SDK's
generated compatibility metadata without requiring a Batuta-specific Compozy
manifest field.

- [ ] **Step 6: Implement the SDK boundary**

`main` resolves `git` once through Task 1's `ExecutableResolver`, requires absolute `COMPOZY_EXECUTABLE`, and injects both paths into `extensionapp.New`. `extensionapp.New` constructs `ExecRunner`, `CLIClient`, `GitClient`, `PublicationPlanner`, `Publisher`, and `Verifier`, then registers:

```go
publication_plan      read_only=true  input={worktree_ref}
publish_worktree      risk=mutating   input={worktree_ref,expected_head_sha}
publication_verify    read_only=true  input={worktree_ref,expected_head_sha,publisher_result}
```

Every input schema uses `additionalProperties:false`. Tool handlers require non-nil trusted workspace with non-empty ID and absolute Root and convert only that object into `publication.TrustedScope`. The plan handler maps an expected `BlockedPlanError` to a successful structured result whose disposition is `blocked` and whose evidence contains only the closed blocker code, clean/head/branch facts, and safe exit-plan projection. That is routing data, not publication success. Transport, decoding, malformed-input, or unknown errors remain typed tool failures. The mutating publisher returns structured `blocked` after any unsafe or ambiguous mutation outcome so the Goal emits authoritative top-level `status: blocked`. No handler returns textual success without structured evidence.

- [ ] **Step 7: Verify generated manifest round-trip and commit**

Create one unique build scratch directory under `/home/francisross/tmp-builds`, set `TMPDIR` only for the single extension build command, retain its structured result, validate that generation once, and remove only that directory afterward:

```bash
rtk go test . ./internal/extensionapp ./internal/publication -count=1
rtk go test -race . ./internal/extensionapp ./internal/publication -count=1
rtk go vet ./...
rtk zsh -c 'build_tmp=$(mktemp -d -p /home/francisross/tmp-builds batuta-extension.XXXXXX); trap '\''rm -rf -- "$build_tmp"'\'' EXIT; TMPDIR="$build_tmp" compozy extension build . -o json >"$build_tmp/build.json"; generation_dir=$(python3 -c '\''import json,sys; print(json.load(open(sys.argv[1]))["generation_dir"])'\'' "$build_tmp/build.json"); compozy extension validate "$generation_dir" -o json'
rtk git diff --check
```

Inspect the generated manifest and assert the three tools, resource paths, and
fixed subprocess survived build/reload. The publisher's exact tool allowlist
is verified from the bundled agent definition in Task 6.

Commit:

```bash
rtk git add go.mod go.sum main.go main_test.go internal/extensionapp internal/publication
rtk git rm extension.toml
rtk git commit -m "feat: expose scoped publication extension"
```

---

### Task 6: Automatic publication graph and publisher containment

**Files:**
- Modify: `loops/batuta-deliver/loop.yaml`
- Modify: `agents/batuta-publisher/AGENT.md`
- Modify: `agents/batuta/AGENT.md`
- Modify: `tests/contract/test_04_deliver_validate.sh`
- Rename: `tests/e2e/assert_publication_gate.py` → `tests/e2e/assert_publication_flow.py`
- Rename: `tests/e2e/test_assert_publication_gate.py` → `tests/e2e/test_assert_publication_flow.py`

**Interfaces:**
- Consumes: Task 5 exact tool IDs and Task 3/4 output schemas.
- Produces: review → plan → deterministic route → automatic scoped publisher Goal → independent verify; only a pre-mutation blocker reaches operator recovery.

- [ ] **Step 1: Rewrite contract tests first**

Add behavioral YAML parsing/assertions that fail unless:

```text
auto_commit input is absent
implement.params.inputs.auto_commit == true
review.params.inputs.auto_commit == true
publication_plan kind == ext__batuta__publication_plan
publication_plan receives only worktree_ref
publication_route is a route over publication_plan.output.disposition
publishable routes directly to publish with no gate
nothing_to_publish routes directly to publication_verify with no gate
blocked routes only to recovery_gate
recovery_gate cannot reach publish directly
publish Goal output has control status complete|blocked and a nested publication_result
publication_result forbids compare_url and requires status/head_sha/op_ids/summary
publication_verify kind == ext__batuta__publication_verify
publication_verify receives publish.output.publication_result on the published path
definition_of_done requires publication_verify and a real PR for published work
no automatic merge node or tool exists
```

Replace the gate-centric Python checker with a publication-flow checker. It must require `publish` and then `publication_verify` on the healthy published path without any `needs_approval`; accept a verified `nothing_to_publish` path without running `publish`; require `needs_approval` only for `recovery_gate` after a structured blocked plan; and reject compare-only evidence, an operator gate on the healthy path, or any merge mutation.

- [ ] **Step 2: Verify RED**

Run:

```bash
rtk tests/contract/test_04_deliver_validate.sh
rtk python3 -m unittest tests.e2e.test_assert_publication_flow
```

Expected: failures on the old `auto_commit` input, mandatory human gate, missing plan/verify nodes, and compare URL acceptance.

- [ ] **Step 3: Implement the revised graph and agent contract**

Remove `inputs.auto_commit` and pass literal `true` to both child Loops. Replace `worktree_state`/`publish_check` with direct `publication_plan` and a closed `route`:

```yaml
- id: publication_route
  class: control
  kind: route
  routes:
    - when: nodes.publication_plan.output.disposition == 'publishable'
      to: publish
    - when: nodes.publication_plan.output.disposition == 'nothing_to_publish'
      to: publication_verify_nothing
    - when: nodes.publication_plan.output.disposition == 'blocked'
      to: recovery_gate
  default: publication_contract_failure
```

`publication_contract_failure` fails closed without mutation. `recovery_gate` is the only human gate and its prompt contains the safe blocker code plus run/worktree identity. Approval performs one bounded re-plan by returning to a distinct `publication_replan` node and route; rejection stops blocked. The recovery path has no edge directly to `publish`, and the graph admits at most one operator-assisted re-plan. Any blocker after `publish_worktree` returns top-level Goal `status: blocked` with durable operation IDs and terminates without a gate or retry.

The publisher Goal objective supplies only `worktree_ref` and `publication_plan.output.head_sha`, instructs exactly one call to `ext__batuta__publish_worktree`, and forbids invented evidence. Its output uses Compozy's authoritative control envelope:

```yaml
status: complete | blocked
publication_result:
  status: published | nothing_to_publish | blocked
  head_sha: string
  op_ids: [string]
  pr_url: string # required only when publication status is published
  summary: string
  last_exit_plan: object
```

When the tool returns `published` or `nothing_to_publish`, the agent reports top-level `status: complete`; when it returns `blocked`, it reports top-level `status: blocked`. `publication_verify` receives `publish.output.publication_result`, never the top-level Goal control status. The `nothing_to_publish` verifier receives a deterministic publisher-result envelope built from the fresh plan and zero operation IDs. No path accepts a compare URL. No path merges the PR.

Set `agents/batuta-publisher/AGENT.md` frontmatter to:

```yaml
---
name: batuta-publisher
category_path: [Batuta]
permissions: approve-all
tools:
  - ext__batuta__publish_worktree
---
```

Its body must say: call the tool exactly once with the provided ref/SHA; never call shell, Git, filesystem, session, gate, merge, or another extension tool; report the structured result exactly; a blocked tool result remains blocked. Remove all shell procedure and compare-URL language. Update the conductor so literal commits, automatic publication after clean review, automatic PR opening, and manual merge are Batuta invariants.

- [ ] **Step 4: Verify GREEN and commit**

Run from the disposable detached contract worktree required by `CLAUDE.md`:

```bash
rtk tests/contract/test_04_deliver_validate.sh
rtk python3 -m unittest discover -s tests/e2e -p 'test_*.py'
```

Expected: all pass with no healthy-path approval event.

Commit:

```bash
rtk git add loops/batuta-deliver/loop.yaml agents/batuta-publisher/AGENT.md agents/batuta/AGENT.md tests/contract/test_04_deliver_validate.sh tests/e2e
rtk git commit -m "feat: automate verified publication loop"
```

---

### Task 7: Executable packaging and republish workflow

**Files:**
- Modify: `scripts/stage-extension.sh`
- Modify: `scripts/republish.sh`
- Modify: `scripts/check-compozy-version.sh`
- Modify: `tests/contract/test_01_stage.sh`
- Modify: `tests/contract/test_01_republish.sh`
- Modify: `tests/contract/test_01_validate.sh`
- Modify: `tests/contract/test_00_runtime_guard.sh`
- Modify: `tests/contract/test_03_lifecycle.sh`
- Modify: `tests/contract/test_07_license.sh`
- Modify: `tests/contract/test_07_preview_docs.sh`
- Modify: `tests/contract/test_07_workflow_contract.sh`
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release.yml`

**Interfaces:**
- Consumes: complete code-backed extension and Loop.
- Produces: host-buildable staged source, validated immutable generations, and workflow contracts that build before any release tag is pushed.

- [ ] **Step 1: Write failing packaging, validation, version, and workflow tests**

Update contract fixtures to require staged `go.mod`, `go.sum`, `main.go`, production `internal/**/*.go`, agents, skill, Loop, and LICENSE; forbid `_test.go`, handwritten `extension.toml`, `.git`, `.compozy`, `dist`, and local workspace files. Rewrite validation, license, preview-doc, lifecycle, and workflow assertions from handwritten-manifest semantics to source → build → generated-manifest semantics. Update republish expectations to stage source → build → validate generated directory → remove/install generated directory → enable → inventory. Update the runtime guard to reject `0.3.0-beta.20`, accept the qualifying release from Task 5 as the minimum, and accept later official compatible SemVer prereleases/releases. The SDK module remains pinned to one exact published version.

- [ ] **Step 2: Verify RED**

Run:

```bash
rtk tests/contract/test_01_stage.sh
rtk tests/contract/test_01_republish.sh
rtk tests/contract/test_01_validate.sh
rtk tests/contract/test_07_license.sh
rtk tests/contract/test_07_preview_docs.sh
rtk tests/contract/test_07_workflow_contract.sh
rtk tests/contract/test_00_runtime_guard.sh
```

Expected: failures because staging, validation, workflows, and republish still implement resource-only packaging.

- [ ] **Step 3: Implement code-backed staging, republish, CI, and release preflight**

`stage-extension.sh` copies only the curated source/resource set into an empty real directory. `republish.sh` builds that source, reads `generation_dir` from structured output, validates it, and installs that immutable generated package before enabling and verifying inventory. It cleans only temporary directories it created. Do not install or validate the source directory as if it were a handwritten manifest package. Inventory must contain the four static resources plus the three tool descriptors; use the actual public inventory `kind`/`name` projection returned by the qualifying Compozy release.

CI installs Go 1.26.4, tests the module, builds once into a unique heavy scratch directory, and validates the generated manifest. Release consumes the already verified source, requires `release_version == 0.1.0-beta.5 == generated_manifest.version`, runs every build/test/contract/archive check before any irreversible tag push, sets `GOOS=linux GOARCH=amd64`, inspects the binary/archive concretely, and publishes only the immutable generated directory. There is no invented machine-readable platform label: workflow/artifact naming and public docs state the `linux/amd64` limitation until Compozy supports platform-aware assets.

- [ ] **Step 4: Verify GREEN and commit**

```bash
rtk tests/contract/test_01_stage.sh
rtk tests/contract/test_01_republish.sh
rtk tests/contract/test_01_validate.sh
rtk tests/contract/test_03_lifecycle.sh
rtk tests/contract/test_07_license.sh
rtk tests/contract/test_07_preview_docs.sh
rtk tests/contract/test_07_workflow_contract.sh
rtk tests/contract/test_00_runtime_guard.sh
rtk git diff --check
```

Commit:

```bash
rtk git add scripts tests/contract .github/workflows
rtk git commit -m "build: package executable batuta extension"
```

---

### Task 8: Integrated containment, publication smokes, and documentation

**Files:**
- Create: `tests/integration/publication_negative_smoke.sh`
- Create: `tests/integration/publication_forge_fixture_smoke.sh`
- Create: `tests/integration/testdata/forge-fixture/main.go`
- Create: `tests/integration/testdata/forge-fixture/go.mod`
- Create: `tests/integration/testdata/forge-fixture/go.sum`
- Create: `tests/integration/testdata/scripted-publisher-acp/main.go`
- Create: `tests/integration/testdata/scripted-publisher-acp/go.mod`
- Create: `tests/integration/testdata/scripted-publisher-acp/go.sum`
- Create: `tests/integration/testdata/counter-tool/main.go`
- Create: `tests/integration/testdata/counter-tool/go.mod`
- Create: `tests/integration/testdata/counter-tool/go.sum`
- Create: `tests/integration/testdata/README.md`
- Modify: `tests/contract/test_07_public_docs.sh`
- Modify: `tests/e2e/SMOKE.md`
- Modify: `README.md`
- Modify: `README.pt-BR.md`
- Modify: `docs/how-it-works.md`
- Modify: `docs/architecture.md`
- Modify: `docs/verify.md`
- Modify: `docs/images/batuta-no-compozy.png`
- Modify: `CONTRIBUTING.md`
- Create: `docs/releases/0.1.0-beta.5.md`
- Create: `docs/internal/qa/2026-08-25-batuta-publication.md`

**Interfaces:**
- Consumes: Task 7 installed generated extension and complete delivery Loop.
- Produces: real-path containment evidence, negative no-forge proof, deterministic serving-forge publication proof, release QA record, and public operating documentation.

- [ ] **Step 1: Write the no-forge refusal smoke before implementation**

`publication_negative_smoke.sh` creates a temporary workspace, local bare Git remote, base commit, delivery worktree/branch, committed change, isolated Compozy home/daemon, and dev-linked Batuta generation. It invokes `publication_plan` through the public Compozy surface and requires a structured `forge_unavailable` blocked disposition routed to `recovery_gate`. It then proves the remote branch was not created, no push/PR operation ID exists, and no publication handler mutation occurred. It never calls `publish_worktree` before an approved bounded re-plan becomes publishable and never treats a compare URL as success.

- [ ] **Step 2: Write the serving-forge and Goal-containment smoke**

`forge-fixture` is a test-only code-backed extension implementing the public `forge/capabilities`, `forge/status`, and `forge/pr_create` methods. It reports one serving provider/default branch, reads the configured temporary bare remote to bind branch and exact HEAD, stores PR state only in the test directory, returns a deterministic HTTPS PR URL, and never enters production artifacts. `counter-tool` exposes one benign hosted tool whose handler atomically increments a fixture count.

`scripted-publisher-acp` is a deterministic ACP stdio provider configured only in the isolated test profile. Across Goal turns it requests, in order, one provider-native shell tool, one filesystem tool, the counter extension tool through its real hosted MCP binding, and finally `ext__batuta__publish_worktree`; after each denied result it continues, and after the admitted publish result it emits the exact Goal control/publication envelope. The fixture asserts the attempted ToolIDs and never fabricates publication evidence itself.

`publication_forge_fixture_smoke.sh` builds those fixtures, serves the temporary bare remote through the public forge fixture, and runs the installed `batuta-publisher` Goal under `approve-all`. The production `batuta-publisher` definition proves its exact daemon-enforced allowlist rejects shell, filesystem, and counter before dispatch, with zero pending permission interaction and zero counter-handler execution.

It proves:

- the production allowlist denies shell, filesystem, and the second extension
  tool with zero handler execution;
- the exact publish tool is admitted once;
- exact-HEAD push and PR creation expose ordered durable operation IDs and an observable HTTPS PR URL;
- `publication_verify` independently confirms the exact remote/PR head;
- dirty/drifted, foreign-workspace, fabricated-result, and unavailable-tool cases fail closed;
- teardown leaves no extension or Goal subprocess alive.

It also runs live negative rows for a structured pre-mutation blocker routed to `recovery_gate`, recovery rejection, an authored submission containing the removed `auto_commit=false` input, and a serving forge with credentials deliberately unavailable. Every pre-mutation blocked row requires zero push, zero PR, zero durable publication operation, zero pending permission interaction outside the explicit recovery gate, and clean fixture teardown. This deterministic fixture is integration evidence, not a substitute for the release QA run against a real serving forge.

- [ ] **Step 3: Verify RED, then implement only the fixtures/harnesses required by the tests**

Run each smoke once to capture honest RED at its named missing fixture/boundary, implement the three exact test-only fixtures above, and rerun. Their committed `go.sum` files are generated from the pinned published dependencies and checked for drift; do not use implicit network resolution during the smoke. Do not add a production fake-forge path or weaken URL/HEAD checks.

- [ ] **Step 4: Update docs and the release QA matrix**

Document: Go 1.26.4 toolchain; exact qualifying Compozy prerelease; literal commits; automatic publication and PR opening after clean review; conditional bounded operator recovery only for pre-mutation blockers; manual merge; exact daemon-enforced publisher allowlist; trusted publication capability; actual PR requirement; local bare-remote negative smoke; fixture-backed and real-forge positive smokes; dirty/drifted/foreign-worktree/fabricated-result/forbidden-tool negatives; and the initial `linux/amd64` GitHub-package limit. Remove every instruction that asks the operator to commit, push, open a PR, accept compare-only success, configure `auto_commit=false`, approve healthy publication, or approve publisher shell commands. Update `docs/images/batuta-no-compozy.png` so the dominant path is `Implementar → Revisar → Publicar + abrir PR (Automático)`, `Bloqueio → Operador` is a side branch, and `Merge manual` is explicit. The beta release note records removal of `auto_commit=false` as a breaking change.

- [ ] **Step 5: Run focused and full local verification**

Use a disposable detached contract worktree without `.compozy/` and run:

```bash
rtk go test ./... -count=1
rtk go test -race ./... -count=1
rtk go vet ./...
rtk python3 -m unittest discover -s tests/e2e -p 'test_*.py'
rtk tests/contract/run.sh
rtk tests/integration/publication_negative_smoke.sh
rtk tests/integration/publication_forge_fixture_smoke.sh
rtk git diff --check
```

Then build and validate one immutable generation with a unique heavy scratch directory.

- [ ] **Step 6: Run release QA against a real serving forge**

Before publishing the beta, run the same positive assertions against a real serving forge and credentials in an isolated QA workspace. Record exact run/worktree/operation/PR/HEAD evidence without credentials. If the forge or credentials are unavailable, record `blocked-verify` and stop the beta release; do not declare implementation acceptance or substitute the bare remote/fixture for this external proof.

- [ ] **Step 7: Commit docs and integration evidence**

```bash
rtk git add tests/integration tests/e2e tests/contract/test_07_public_docs.sh README.md README.pt-BR.md CONTRIBUTING.md docs/architecture.md docs/how-it-works.md docs/verify.md docs/images/batuta-no-compozy.png docs/releases docs/internal/qa
rtk git commit -m "docs: ship autonomous publication workflow"
```

---

## Final Review and Stop Condition

After every task has an approved task review, generate one whole-branch review package from the branch merge base through HEAD and dispatch the most capable reviewer. One fix wave and one scoped re-review are allowed by the SDD workflow.

Stop this plan when:

1. all unit, race, vet, Python, contract, and local integration commands above have fresh evidence;
2. the generated manifest preserves all resources and three tool descriptors, while the bundled publisher agent preserves its exact one-tool allowlist;
3. a publisher-shell attempt is denied under `approve-all` with zero handler execution;
4. a local bare remote proves missing-forge refusal with zero mutation;
5. a deterministic serving-forge integration proves the complete Goal, push, PR, and verifier path;
6. a real serving-forge QA smoke proves actual PR creation and independent verification; `blocked-verify` stops release and is not acceptance;
7. no operator-owned commit, push, PR, compare URL, path, remote, or workspace input remains.

Do not add graph engineering, executor inventory, domain routing, multi-forge support, automatic merge, branch cleanup, release publishing, or product UI work in this plan. The presentation image update is documentation, not product UI.
