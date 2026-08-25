# Batuta scoped LLM publication — design

Status: approved; revised on 2026-08-25 for automatic PR publication

## Context

`batuta-deliver` currently enters a human publication gate and then runs a
`goal` node with `batuta-publisher`. The publisher uses shell commands for
`git status`, `git rev-parse`, and the `compozy worktree` exit verbs.

A live isolated probe on 2026-08-23 closed the merge gate left open by the
delivery-hardening handoff. The Goal session reached the publisher, resolved
Claude successfully, and stopped `waiting-for-auth` on its first read-only
shell command under `permissions: approve-reads`. The ACP session advertised
mode `default` (manual) and its last activity was a permission request for
`git status`/`git rev-parse`. Therefore the existing publication node cannot
run unattended. `deny-all` had already been proven to reject the same commands
and a bare switch to `approve-all` would expose unrestricted provider-native
shell access.

The product requirement is:

- implementation and review fixes are committed by Batuta before publication;
- after implementation and a clean review, Batuta automatically performs
  planning, push, PR creation, reconciliation, and reporting;
- a publishable delivery ends with an actual pull request, never a compare URL
  that leaves PR creation to the operator;
- merge remains a manual forge action and is outside Batuta's authority;
- the operator is involved only after a real blocker, never as a mandatory
  checkpoint on the healthy path;
- the publisher remains an LLM agent, but receives no general-purpose
  mutation capability;
- publication cannot target a workspace or worktree other than the one bound
  to the delivery run.

## Decision

Keep `batuta-publisher` as the Goal's LLM controller, but replace its shell
procedure with one narrow executable extension capability:
`ext__batuta__publish_worktree`.

The extension also supplies two read-only deterministic tools used directly
by the Loop:

- `ext__batuta__publication_plan` captures the exact clean HEAD and daemon
  exit plan before any mutation;
- `ext__batuta__publication_verify` proves the remote/PR outcome after the
  publisher reports completion.

All three tools derive their workspace from the daemon-authenticated
`TrustedWorkspace` attached to the extension tool invocation. None accepts a
workspace ID, filesystem path, repository URL, remote name, or raw command as
input.

This separates judgment from authority. The LLM decides to invoke the one
publication capability and explains its result. Deterministic code owns every
state-changing operation and every acceptance proof.

`batuta-deliver` no longer exposes `auto_commit` as a delivery choice. It
passes the literal `true` to both `implement-tasks` and `review-and-fix`,
so implementation and remediation commits are part of the Batuta-owned
contract. Direct submissions cannot select an uncommitted delivery path.

## Architecture

### Executable Batuta extension

Batuta becomes a Go SDK executable extension while retaining its existing
agents, skills, Loops, and documentation resources. Installation builds one
extension subprocess. The subprocess registers the three publication tools.
Batuta does not require a new Compozy hook declaration or hook-runtime change.

The extension receives the resolved Compozy executable through a manifest
environment value based on `{{compozy_executable}}`; it never searches `PATH`
for a replacement binary. Child processes use `exec.CommandContext` with an
argument vector. No shell, string command concatenation, or evaluation of
repository-provided text is allowed.

Every tool requires non-empty `TrustedWorkspace.ID` and
`TrustedWorkspace.Root`. Missing trusted scope is a hard error. The only
caller-provided repository selector is `worktree_ref`, which is resolved by
`compozy worktree inspect --workspace <trusted workspace ID>`. A ref belonging
to another workspace is therefore not addressable.

### Publication plan

`ext__batuta__publication_plan` is read-only and accepts:

```json
{"worktree_ref":"wt_..."}
```

It resolves the worktree through the trusted workspace and returns:

- worktree ID, branch, and base branch;
- absolute worktree path obtained from the daemon, never supplied by the
  caller;
- `clean` and exact `head_sha` from Git;
- the current daemon exit plan, including blocked reasons, forge capability,
  and PR prefill;
- a disposition of `publishable`, `nothing_to_publish`, or `blocked`.

A dirty tree, detached branch, divergent/behind state, missing remote, missing
serving forge/credentials for a publishable branch, or any other unsafe plan
is `blocked`; no publication mutation starts. A clean branch with no outgoing
commit is `nothing_to_publish` and terminates as a verified no-op.

`publishable` proceeds directly to the publisher without asking the operator.
A pre-mutation `blocked` disposition may park on a conditional recovery gate
that presents the captured branch, base, HEAD, blocker, and exit-plan evidence.
That gate is not part of the healthy delivery path: it exists only so the
operator can repair external state and request a bounded re-plan. A blocker
observed after a remote mutation terminates the run as `blocked` and returns
the durable operation IDs to the originating Batuta session for recovery.

### LLM publisher capability

`batuta-publisher` remains the agent used by the `publish` Goal. Its effective
surface is narrowed by two existing enforcement layers:

1. Its agent definition allows only `ext__batuta__publish_worktree`. Loop v1
   has no Goal-level `allowed_tools` field, so the Goal objective repeats the
   one-tool instruction but is not counted as a separate enforcement surface.
2. The publication tool itself accepts only `worktree_ref` and
   `expected_head_sha`, and derives the workspace and path from trusted daemon
   context.

With those containment layers in place, the publisher uses
`permissions: approve-all` solely to prevent the provider from pausing on its
one authorized tool call. `approve-all` is not treated as the security
boundary; the daemon-enforced concrete tool allowlist and trusted workspace
resolution are. A test must prove that shell and a second benign tool are
absent or denied even while the session reports `approve-all`.

Implementation ruling: Loop v1 exposes `allowed_tools` on `run-agent`, but not
on `goal`. Batuta keeps the approved Goal controller instead of adding another
platform prerequisite. The exact `tools` allowlist therefore lives in the
`batuta-publisher` definition and is enforced by Compozy's existing agent tool
projection.

The Goal objective gives the publisher the planned `head_sha` and worktree
ref and instructs it to call the capability exactly once. Compozy owns the
top-level Goal control status, so the Goal returns `status: complete|blocked`
and places the unmodified tool result under `publication_result`. A successful
tool result uses top-level `complete`; a tool result with publication status
`blocked` uses top-level `blocked`, terminating the Loop before verification.
The independent verifier receives only `publication_result` and never treats
the Goal control status as publication evidence.

### Deterministic publication

`ext__batuta__publish_worktree` executes this bounded state machine:

1. Resolve the worktree inside `TrustedWorkspace` and reject missing, removed,
   non-ready, or mismatched records.
2. Verify the tree is clean and current HEAD equals `expected_head_sha`. Any
   drift fails before a remote mutation.
3. Read a fresh exit plan. If it is `nothing_to_publish`, return a successful
   no-op with the verified HEAD and no push operation ID.
4. Start `compozy worktree push` and record its durable `op_id`.
5. Re-read the exit plan on a bounded interval until the push is reflected,
   the operation is observably blocked/failed, or the tool deadline expires.
   Reconciliation always reads state before considering a retry.
6. Call `compozy worktree pr` using the daemon's `pr_prefill` and default
   branch as argument-vector values. Record its `op_id`, then reconcile until
   an existing or newly opened PR URL is observable. Forge unavailability is
   never converted into a compare-URL success.

The output schema requires:

```text
status: published | nothing_to_publish | blocked
head_sha: exact verified SHA
op_ids: ordered durable operation IDs
pr_url: required when status is published
summary: bounded human-readable result
```

Repository-derived PR title/body and exit-plan strings are data. They are
passed as individual process arguments and are never interpreted as commands
or agent instructions.

### Independent verification

The publisher's narrative is not acceptance evidence.
`ext__batuta__publication_verify` runs as a direct Loop action after the Goal.
It accepts `worktree_ref`, `expected_head_sha`, and the publisher's structured
result. It independently re-resolves the trusted workspace, re-reads HEAD and
the exit plan, and verifies one of:

- the exact expected SHA is the published remote/PR head and the reported PR
  URL is observable;
- there was genuinely nothing to publish and no push `op_id` was claimed.

Mismatch, unverifiable output, dirty state, or an ambiguous operation fails
the node and prevents the parent Loop from ending `done`.

## Revised Loop graph

The post-review graph becomes:

```text
review
  -> publication_plan
  -> publication_route
       |-- publishable -> publish (LLM Goal; one scoped capability)
       |                  -> publication_verify -> done with PR
       |-- nothing_to_publish -> publication_verify -> no-op
       `-- blocked -> recovery_gate -> bounded re-plan or stop
```

`publication_plan` replaces stale generic inspection as the authoritative
pre-mutation snapshot. There is no approval gate on the `publishable` or
`nothing_to_publish` routes. The conditional `recovery_gate` is reachable only
from a deterministic `blocked` disposition and never authorizes publication
by itself: approval means "repair completed, re-plan", not "push despite the
blocker". Re-planning remains bounded by the Loop's iteration, time, and token
limits.

The pre-review delivery path hardcodes `auto_commit=true` for both child
Loops. A dirty worktree after review is a contract failure, not an alternate
publication mode and not work delegated to the operator.

The definition of done requires successful publisher output with a real PR
and successful independent verification. A Goal output alone can never make
the delivery successful. Merge is never attempted.

## Failure and recovery semantics

- Agent allowlist excludes a non-publication tool: the Goal cannot dispatch it;
  no publication operation starts.
- Extension subprocess unavailable: the publication tools are unavailable and
  the Goal cannot start or mutate state.
- Forge or forge credentials unavailable before mutation: publication
  planning returns `blocked` and may enter the conditional recovery path.
- HEAD or cleanliness changes after planning: publication returns `blocked`
  before push.
- Push outcome is ambiguous: re-read the exit plan; never retry blindly.
- Push succeeds and PR fails transiently: retain the pushed branch and return
  a truthful blocked result; never report the pushed branch as a completed
  delivery without a PR.
- LLM omits, alters, or fabricates evidence: `publication_verify` fails.
- Tool deadline expires: return the last structured exit plan plus all known
  operation IDs; the run fails without rounding the result to success.

## Security invariants

- No publication tool accepts workspace, path, remote, destination URL, or
  command input. The verifier may receive the publisher's claimed PR URL only
  as untrusted evidence to compare with a freshly observed forge URL.
- The daemon-authenticated workspace scope is mandatory and authoritative.
- The expected HEAD captured by planning is rechecked before mutation and
  after publication.
- The delivery graph always enables child-Loop commits; dirty post-review
  state blocks before publication.
- A publishable delivery cannot end successfully without an observable PR
  bound to the exact expected HEAD.
- The publisher cannot use shell or filesystem tools even under
  `approve-all`.
- The extension invokes the daemon-resolved Compozy executable directly with
  argument vectors.
- PR prefill and repository content never enter a shell or capability
  decision.
- An unavailable tool, stale worktree, or incomplete proof fails closed.

## Verification

Implementation acceptance requires:

1. Unit tests for trusted-scope rejection, input validation, exit-plan
   classification, HEAD drift, dirty trees, argv safety, bounded
   reconciliation, mandatory-forge handling, and output validation.
2. Agent-surface tests proving `batuta-publisher` receives only the exact
   publication tool and that shell/file/second-tool attempts are unavailable
   or denied under `approve-all`.
3. Contract tests for executable-extension staging, the three tool
   descriptors, agent allowlist, revised graph, and independent verifier.
4. Existing Batuta contract and Python E2E suites remain green.
5. An isolated daemon smoke with a local bare remote proves missing-forge
   recovery routing and zero publication mutation.
6. A deterministic serving-forge integration proves unattended Goal
   execution, exact-HEAD push, structured operation IDs, PR creation,
   independent verification, and zero pending permission interactions. A
   release QA smoke against a real serving forge must prove the same external
   outcome before the beta is published; missing credentials may block QA but
   cannot satisfy release acceptance.
7. Negative live smokes cover rejection, `auto_commit=false` authoring,
   dirty/drifted worktree, missing forge/credentials, fabricated Goal output,
   a forbidden shell attempt, and an out-of-workspace ref.

## Compatibility and release

This is a Batuta executable-extension change built on Compozy's existing
extension tools, `TrustedWorkspace`, hosted extension-tool exposure, agent
tool allowlists, and Loop invocation of extension ToolIDs. It introduces no
new Compozy hook or manifest contract. Batuta pins a published SDK/binary pair
that contains the existing extension surfaces it consumes; its runtime guard
checks that pair directly rather than requiring a new extension-specific
minimum-version field. Installation and republish documentation must state the
Go build-toolchain requirement.

Code-backed GitHub packages are immutable generated bundles and include the
compiled subprocess; installing a GitHub package does not compile its source.
Until Compozy gains platform-aware asset selection, the first executable
Batuta beta release is explicitly `linux/amd64`. Local development remains
source-based and builds on the host through `compozy extension build`. Other
published platforms require a later distribution contract and are not
silently served a Linux binary.

The public behavior is intentionally more autonomous: users request a
delivery and Batuta opens a verified PR after implementation and clean review.
The operator is contacted only when the run is blocked. Merge remains manual.

## Rejected alternatives

- **Operator executes exit/push/PR:** violates full-cycle Batuta ownership.
- **Direct deterministic Loop action with no LLM:** secure and simpler, but
  removes the explicitly retained publisher agent role.
- **Current LLM plus `approve-reads`:** proven to stop on the first shell
  command.
- **Current LLM plus bare `approve-all`:** leaves unrestricted
  provider-native shell and cross-workspace reach; the accepted design pairs
  `approve-all` with an exact daemon-enforced agent tool allowlist.
- **New required-hook prerequisite:** adds a broad Compozy core/SDK/runtime
  change when the existing exact agent allowlist already provides the needed
  publisher capability boundary.
- **Mandatory publication gate:** contradicts full-cycle Batuta ownership and
  makes the operator approve every healthy delivery without adding a security
  boundary; deterministic planning, scoped capability, and independent
  verification remain the actual safeguards.
- **Compare-URL success without a PR:** leaves an operational publication step
  to the operator and violates full-cycle Batuta ownership.
