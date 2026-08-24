# Batuta scoped LLM publication — design

Status: approved; implementation requires the Compozy hook prerequisite

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

The product requirement remains:

- implementation and review fixes are committed by Batuta before publication;
- the operator makes one publication decision at `publish_gate`;
- after `approve`, Batuta performs all remaining planning, push, PR creation,
  reconciliation, and reporting without further operator action;
- a publishable delivery ends with an actual pull request, never a compare URL
  that leaves PR creation to the operator;
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
  exit plan before the human gate;
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
extension subprocess. The subprocess registers the three tools and one
required synchronous `tool.pre_call` hook. This requires a Compozy code-first
hook declaration that preserves `required: true` and the
`agent_name=batuta-publisher` matcher in the generated manifest; the
compatibility section records the missing runtime prerequisite.

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
is `blocked`; the node fails before the gate. A clean branch with no outgoing
commit is `nothing_to_publish` and still reaches the gate, preserving the
existing fail-closed policy.

The gate prompt presents the captured branch, base, HEAD, disposition, and
exit-plan evidence. The operator only chooses `approve` or `reject`; they do
not run publication commands.

### LLM publisher capability

`batuta-publisher` remains the agent used by the `publish` Goal. Its effective
surface is narrowed in three independent layers:

1. Its agent definition and the Goal node both allow only
   `ext__batuta__publish_worktree`.
2. A required synchronous `tool.pre_call` hook matching
   `agent_name=batuta-publisher` denies every tool call except that exact
   canonical ToolID. This includes provider-native shell, file reads/writes,
   subprocess creation, session tools, and all other hosted tools. Hook
   unavailability or malformed input denies execution.
3. The publication tool itself accepts only `worktree_ref` and
   `expected_head_sha`, and derives the workspace and path from trusted daemon
   context.

With those containment layers in place, the publisher uses
`permissions: approve-all` solely to prevent the provider from pausing on its
one authorized tool call. `approve-all` is not treated as the security
boundary; the required hook, the concrete tool allowlist, and trusted
workspace resolution are. A test must prove that shell and a second benign
tool are denied even while the session reports `approve-all`.

The Goal objective gives the publisher the pre-gate `head_sha` and worktree
ref and instructs it to call the capability exactly once. Its structured
output reports the tool result without inventing evidence.

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
  -> publish_check (unconditional seam)
  -> publish_gate (human approve/reject only)
  -> publish (LLM Goal; one scoped capability)
  -> publication_verify (deterministic evidence)
  -> done
```

`publish_check` remains unconditional because the current Loop CEL still
cannot prove cached worktree inspection freshness. `publication_plan` replaces
the stale generic inspection as the authoritative pre-gate publication
snapshot. Gate rejection remains terminal `blocked` with the branch intact.

The pre-review delivery path hardcodes `auto_commit=true` for both child
Loops. A dirty worktree after review is a contract failure, not an alternate
publication mode and not work delegated to the operator.

The definition of done requires gate approval, successful publisher output,
and successful independent verification. A Goal output alone can never make
the delivery successful.

## Failure and recovery semantics

- Permission hook denies a non-publication tool: the Goal fails closed; no
  publication operation starts.
- Extension subprocess unavailable: required tools/hooks are unavailable and
  the Goal cannot start or mutate state.
- Forge or forge credentials unavailable before the gate: publication
  planning returns `blocked`; the operator is not asked to approve a delivery
  Batuta cannot finish.
- HEAD or cleanliness changes after the gate: publication returns `blocked`
  before push.
- Push outcome is ambiguous: re-read the exit plan; never retry blindly.
- Push succeeds and PR fails transiently: retain the pushed branch and return
  a truthful blocked result; never report the pushed branch as a completed
  delivery without a PR.
- LLM omits, alters, or fabricates evidence: `publication_verify` fails.
- Tool deadline expires: return the last structured exit plan plus all known
  operation IDs; the run fails without rounding the result to success.

## Security invariants

- No publication tool accepts workspace, path, remote, URL, or command input.
- The daemon-authenticated workspace scope is mandatory and authoritative.
- The expected HEAD captured before the gate is rechecked before mutation and
  after publication.
- The delivery graph always enables child-Loop commits; dirty post-review
  state fails before the publication gate.
- A publishable delivery cannot end successfully without an observable PR
  bound to the exact expected HEAD.
- The publisher cannot use shell or filesystem tools even under
  `approve-all`.
- The extension invokes the daemon-resolved Compozy executable directly with
  argument vectors.
- PR prefill and repository content never enter a shell or capability
  decision.
- A stopped hook, unavailable tool, stale worktree, or incomplete proof fails
  closed.

## Verification

Implementation acceptance requires:

1. Unit tests for trusted-scope rejection, input validation, exit-plan
   classification, HEAD drift, dirty trees, argv safety, bounded
   reconciliation, mandatory-forge handling, and output validation.
2. Hook tests proving `batuta-publisher` may call only the exact publication
   tool and that shell/file/second-tool attempts are denied under
   `approve-all`.
3. Contract tests for executable-extension staging, the three tool
   descriptors, required hook, agent allowlist, revised graph, and independent
   verifier.
4. Existing Batuta contract and Python E2E suites remain green.
5. An isolated daemon smoke with a local bare remote proves unattended
   Goal execution, exact HEAD push, structured `op_id`, and zero pending
   permission interactions.
6. A forge-backed smoke proves commit creation, exact-HEAD push, PR creation,
   and independent verification.
7. Negative live smokes cover rejection, `auto_commit=false` authoring,
   dirty/drifted worktree, missing forge/credentials, fabricated Goal output,
   a forbidden shell attempt, and an out-of-workspace ref.

## Compatibility and release

This is an executable-extension and runtime-contract change. Compozy
`0.3.0-beta.19` contains executable extension tools, `TrustedWorkspace`,
hosted exposure of extension tools, and Loop invocation of extension ToolIDs,
but its code-first SDK declaration reduces `supported_hook_events` to an
unmatched, non-required generated hook. It cannot preserve the required
publisher matcher or fail-closed availability contract in this design.

Implementation therefore has a platform prerequisite: Compozy must expose a
code-first synchronous hook declaration with matcher and `required: true`, and
must prove that a required hook execution failure denies the intercepted tool
call. The complete platform contract is recorded in
`docs/internal/specs/2026-08-24-compozy-batuta-platform-prerequisites-design.md`.
Batuta's operational floor must be the first released prerelease that contains
that contract; it must not claim `0.3.0-beta.19` is sufficient.
Installation and republish documentation must also state the Go
build-toolchain requirement.

The public behavior remains intentionally stable: users still request a
delivery, inspect one publication gate, and choose approve or reject. The
change removes the undocumented per-command approval burden after `approve`.

## Rejected alternatives

- **Operator executes exit/push/PR:** violates full-cycle Batuta ownership.
- **Direct deterministic Loop action with no LLM:** secure and simpler, but
  removes the explicitly retained publisher agent role.
- **Current LLM plus `approve-reads`:** proven to stop on the first shell
  command.
- **Current LLM plus bare `approve-all`:** leaves unrestricted
  provider-native shell and cross-workspace reach.
- **Generated non-required hook from `supported_hook_events`:** fails open if
  the hook subprocess cannot execute and cannot bind the guard declaratively
  to `batuta-publisher`.
- **Mutating command criterion in the gate:** abuses verification as an action
  and risks mutation before an attributable approval.
- **Compare-URL success without a PR:** leaves an operational publication step
  to the operator and violates full-cycle Batuta ownership.
