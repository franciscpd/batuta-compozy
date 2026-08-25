# Batuta Executor Inventory and Domain Lanes Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Let Batuta discover the proven local capabilities of Compozy, Codex, OpenCode, and Cursor Agent, classify every task into a `domain × complexity` lane, persist deterministic Compozy runtime rules, and recover through same-lineage ephemeral exact runtimes without asking the operator to route or clean up work.

**Architecture:** A standard-library inventory core runs a closed command allowlist inside the daemon-authenticated workspace, normalizes only allowlisted fields, and applies a final secret-redaction boundary. A closed-schema classifier supplies semantic task judgments while deterministic code validates taxonomy, decomposition, capability requirements, catalog membership, model floors, ranking, persistence, and ownership. Thin SDK tools expose inventory, planning, matrix apply, and recovery reconciliation; the Batuta agent uses them before dispatch and reports the immutable routing generation.

**Tech Stack:** Go 1.26.4, Compozy Go extension SDK, standard-library JSON and `os/exec`, `github.com/pelletier/go-toml/v2` for allowlisted Codex TOML projections, Compozy Loop v1, Bash contract tests, Python `unittest`.

**Spec:** `docs/internal/specs/2026-08-24-batuta-executor-inventory-and-domain-lanes-design.md`

## Global Constraints

- The operational floor is the first released Compozy prerelease containing all five platform contracts: conjunctive `type + complexity` runtime rules, extension-specific minimum daemon version, read-only revisioned Loop config with CAS, same-lineage nested recovery with an ephemeral exact runtime, and the closed complexity-verification policy. Required hooks are not a Batuta prerequisite. Do not activate the matrix on `0.3.0-beta.20`.
- These are platform prerequisites, not behaviors Batuta may emulate with a write-shaped `{}` read, a stale patch, handwritten manifest, advisory prompt, stored fallback rule, or fresh full-run dispatch.
- This plan consumes the code-backed extension foundation from `docs/internal/plans/2026-08-25-batuta-scoped-llm-publication.md`; do not create a second extension binary or a separate SDK bootstrap.
- Do not commit a local `replace`, unpublished pseudo-version, vendored SDK, locally invented tag, or raw hand-authored executable manifest.
- No public tool accepts a home path, config path, command, executable, credential, environment value, workspace ID, or arbitrary rule owner. Workspace identity and root come only from `ToolRequest.TrustedWorkspace`.
- Every subprocess uses the shared no-shell bounded runner, a per-adapter executable fixed at startup, an exact argv allowlist, the trusted workspace as cwd, a timeout, and bounded stdout/stderr. Login, install, update, remove, refresh, run, exec, serve, and caller-controlled path flags are forbidden.
- One complete inventory is capped at 60 seconds, 64 subprocesses, 256 files, 8 MiB of file input, 10,000 normalized records, and 256 diagnostics. Hitting a cap yields a closed safe diagnostic and `unknown`; it never silently returns a partial fact as `resolved`.
- Raw stdout/stderr, prompts, environment values, headers, auth tokens, credential contents, MCP arguments, and unapproved config content never enter errors, logs, artifacts, journals, tool responses, or LLM inputs. A recursive redaction/canary pass runs immediately before serialization.
- Project-local file adapters resolve symlinks and prove containment under the daemon-authenticated root before reading. User-level files come only from internally derived documented roots; no workspace content can redirect those roots.
- `resolved` alone may satisfy a security-sensitive hard requirement. A successful bounded capability probe may promote only the exact probed fact. `declared` may rank candidates; `unknown` never satisfies a hard requirement.
- Concrete runtime rules use only exact `provider_id` and `model_id` pairs from the live Compozy catalog. External executor inventories can affect eligibility and ranking but cannot invent a Compozy binding.
- Exact-task fallbacks never lower the task's complexity floor. They are ephemeral recovery-generation state and never enter stored Loop rules; only Batuta-owned matrix rules participate in revisioned refresh.
- The closed per-task fallback maxima are `low=1`, `medium=2`, `high=3`, and `critical=3`, with at most three recovery operations across one delivery. `batuta-deliver` starts with `iteration_cap: 4`; Batuta never raises it or starts a replacement delivery to obtain a fresh ceiling.
- The routing operator gate, configurable `auto_commit`, healthy-path publication gate, manual fallback selection, and manual cleanup paths are removed. Batuta returns to the operator only for missing product intent or a genuine external blocker; merge remains manual.
- Graph-level conflict sets, joins, critical-path planning, and multi-worktree merge policy remain deferred to the graph-engineering follow-up already recorded in the spec.
- Run `tests/contract/run.sh` only from a disposable detached worktree without `.compozy/`. Every shell command starts with `rtk`.

---

### Task 1: Inventory schema, bounded probes, and secret boundary

**Files:**
- Create: `internal/inventory/types.go`
- Create: `internal/inventory/runner.go`
- Create: `internal/inventory/runner_test.go`
- Create: `internal/inventory/redact.go`
- Create: `internal/inventory/redact_test.go`
- Reuse: `internal/publication/command.go`

**Interfaces:**
- Consumes: publication `CommandRunner` and daemon-authenticated workspace root.
- Produces: normalized immutable `InventorySnapshot`, `ExecutorSnapshot`, `Evidence`, `ResolutionState`, `ProbeSpec`, `ProbeResult`, and `Collector` contracts.

- [ ] **Step 1: Write the failing schema and redaction tests**

Cover:

```go
func TestSnapshotDigestIsStableAcrossInputOrder(t *testing.T)
func TestSnapshotRejectsUnknownExecutorAndResolutionState(t *testing.T)
func TestRedactRemovesSecretCanariesRecursively(t *testing.T)
func TestRedactNeverReturnsRawCommandOutput(t *testing.T)
func TestRedactPreservesSafeIdentifiersAndProvenance(t *testing.T)
```

The canary table embeds secrets in nested maps, arrays, URLs, headers, environment-like keys, diagnostics, MCP arguments, and error text. Assert no whole value or distinctive substring survives canonical JSON serialization.

- [ ] **Step 2: Verify RED**

```bash
rtk go test ./internal/inventory -run 'TestSnapshot|TestRedact' -count=1
```

Expected: compilation fails because the inventory contracts do not exist.

- [ ] **Step 3: Implement the closed normalized model**

Use fixed executor IDs `compozy`, `codex`, `opencode`, and `cursor-agent`; resolution states `resolved`, `declared`, and `unknown`; capability facts with source, state, safe digest, and bounded diagnostic code; and a canonical sorted representation. The digest covers schema version, executor version/health, redacted configuration and instruction digests, Compozy catalog generation, and capability evidence. It never covers timestamps or raw paths outside the trusted workspace.

Redaction is allowlist-first, not a best-effort blacklist. Unknown fields are dropped. Diagnostics use closed codes plus bounded safe summaries. Credential state is only `configured`, `missing`, or `unknown` when safely reported by the executor.

- [ ] **Step 4: Write failing bounded-runner tests**

```go
func TestProbeRunnerAllowsOnlyRegisteredExecutableArgv(t *testing.T)
func TestProbeRunnerRejectsCallerPathsAndMutatingFlags(t *testing.T)
func TestProbeRunnerUsesTrustedWorkspaceTimeoutAndOutputCaps(t *testing.T)
func TestProbeRunnerRedactsFailuresBeforeReturning(t *testing.T)
```

- [ ] **Step 5: Implement the probe registry**

The registry owns each literal command shape. Dynamic arguments are typed enums or validated IDs previously returned by the same adapter; no caller supplies argv. Use a child timeout no greater than 15 seconds per probe, cap retained stdout at 1 MiB and stderr at 64 KiB, and pass only safe parsed projections to collectors. Missing executables produce `availability=missing`, not an unstructured error.

- [ ] **Step 6: Verify GREEN and commit**

```bash
rtk go test ./internal/inventory -count=1
rtk go test -race ./internal/inventory -count=1
rtk go vet ./internal/inventory
rtk git diff --check
```

Commit:

```bash
rtk git add internal/inventory
rtk git commit -m "feat: add redacted executor inventory core"
```

---

### Task 2: Compozy, Codex, OpenCode, and Cursor Agent adapters

**Files:**
- Create: `internal/inventory/adapters/compozy.go`
- Create: `internal/inventory/adapters/codex.go`
- Create: `internal/inventory/adapters/opencode.go`
- Create: `internal/inventory/adapters/cursor.go`
- Create: `internal/inventory/adapters/adapters_test.go`
- Create: `internal/inventory/adapters/testdata/**`
- Modify: `go.mod`
- Modify: `go.sum`
- Create: `internal/inventory/collect.go`
- Create: `internal/inventory/collect_test.go`

**Interfaces:**
- Consumes: Task 1 probe registry, trusted workspace root/ID, and internally discovered documented config locations.
- Produces: one normalized `ExecutorSnapshot` per adapter and a combined `Collector.Collect` result.

- [ ] **Step 1: Add installed, missing, malformed, partial, and version-skew fixtures**

Use golden fixtures for every adapter and assert the exact closed command allowlist:

```text
Compozy:
  version
  status -o json
  config path --scope {global,workspace} --workspace <trusted-id>
  config show --workspace <trusted-id> -o json
  agent list --workspace <trusted-id> -o json
  provider list -o json
  provider models list -o json
  skill list --workspace <trusted-id> -o json
  toolsets list -o json
  tool list -o json

Codex:
  --version
  doctor --json --summary
  mcp list --json
  plugin list --json
  plugin marketplace list --json
  debug models --bundled

OpenCode:
  --version
  debug config
  debug paths
  agent list
  debug agent <validated-listed-name>
  debug skill
  mcp list
  providers list
  models

Cursor Agent:
  --version
  status --format json
  models
  mcp list
  mcp list-tools <validated-listed-id>
  plugin marketplace list --format json
```

Test names:

```go
func TestAdaptersUseOnlyClosedCommandShapes(t *testing.T)
func TestAdaptersNormalizeInstalledMissingMalformedPartialAndSkewed(t *testing.T)
func TestAdaptersNeverLeakFixtureSecretCanaries(t *testing.T)
func TestAdaptersDistinguishResolvedDeclaredAndUnknown(t *testing.T)
```

- [ ] **Step 2: Verify RED**

```bash
rtk go test ./internal/inventory/adapters ./internal/inventory -count=1
```

Expected: compilation fails because adapters and collector do not exist.

- [ ] **Step 3: Implement the four adapters**

Compozy daemon output is authoritative for effective config, agents, skills, tools, provider auth state, and live provider/model catalog. OpenCode `debug config` is resolved only for fields actually returned there; dynamic agent names must first come from `agent list`, and `debug skill` supplies only safe skill IDs/origins. Codex config/profile, `AGENTS.md`, roots, approval, and sandbox are `declared` or `unknown` unless `doctor` resolves the exact field. Parse only allowlisted Codex TOML fields with the pinned `go-toml/v2` dependency; unknown keys and raw values are dropped before normalization. Codex skill names/origins come only from `$CODEX_HOME/skills` and installed plugin manifests discovered by `plugin list --json`; their instructions/content are never returned. Cursor rules/MCP JSON are `declared`; models/status are resolved from CLI; marketplace names prove only marketplace availability, not installed plugins, while effective CLI config, agents, installed plugins, approval, sandbox, and writable roots remain `unknown`. Editor settings are tagged `editor-only` and never promoted to effective agent capability.

Documented file reads are internally derived only:

- Compozy paths returned by `config path`, with allowlisted keys only;
- `$CODEX_HOME/config.toml`, `$CODEX_HOME/*.config.toml`, and applicable workspace/ancestor `AGENTS.md`;
- OpenCode roots returned by `debug paths`, limited to documented config files;
- `~/.cursor/mcp.json`, `<trusted-root>/.cursor/mcp.json`, and `<trusted-root>/.cursor/rules/*`.

Resolve every workspace-local candidate and symlink, require `filepath.Rel` containment beneath the trusted root, and reject escapes before opening. Do not return file contents, prompts, agent descriptions, environment, headers, MCP arguments, or credentials. A missing/unsupported surface yields a precise safe diagnostic and `unknown`, never a guessed capability.

- [ ] **Step 4: Implement collection and immutable generation input**

Collect adapters independently so one malformed executor does not erase the others. Enforce the global wall-clock, subprocess, file, byte, record, and diagnostic budgets across adapters, not once per process. Sort results and evidence deterministically, apply the final Task 1 redactor, compute the snapshot digest, and include a safe diagnostic per unavailable adapter. The caller cannot choose adapters or probe paths.

- [ ] **Step 5: Verify GREEN and commit**

```bash
rtk go test ./internal/inventory/... -count=1
rtk go test -race ./internal/inventory/... -count=1
rtk go vet ./internal/inventory/...
rtk git diff --check
```

Commit:

```bash
rtk git add go.mod go.sum internal/inventory
rtk git commit -m "feat: inventory local executor capabilities"
```

---

### Task 3: Closed-schema classification and decomposition validation

**Files:**
- Create: `internal/routing/classification.go`
- Create: `internal/routing/classification_test.go`
- Create: `internal/routing/artifacts.go`
- Create: `internal/routing/artifacts_test.go`
- Create: `internal/routing/decomposition.go`
- Create: `internal/routing/decomposition_test.go`

**Interfaces:**
- Produces: `ClassificationRequest`, `ClassificationProposal`, `ValidatedTask`, `CapabilityRequirement`, and `ValidatedTaskGraph`.
- Consumes: daemon-authenticated root, canonical slug, approved task artifacts loaded from `.compozy/tasks/<slug>/task_*.md`, and an LLM-produced JSON object keyed only to those loaded task IDs; no caller-supplied metadata, path, executor command, or configuration data.

- [ ] **Step 1: Write authoritative artifact-loading and taxonomy tests**

Cover canonical slug/ref validation, symlink escape rejection, stable task ordering, strict frontmatter parsing, exact authored domain/complexity presence, `frontmatter.type == canonical domain`, rejection of generic work types (`test`, `refactor`, `chore`, `bugfix`, `qa-report`, `qa-execution`), and a digest over every loaded byte. The public planner supplies only the slug; it cannot inject authored metadata or a task body.

Cover every domain (`backend`, `frontend`, `mobile`, `data`, `infra`, `security`, `testing`, `docs`, `general`, `fullstack`) and complexity (`low`, `medium`, `high`, `critical`), plus:

```go
func TestClassificationRejectsUnknownAndContradictoryFields(t *testing.T)
func TestClassificationAuthoredMetadataWins(t *testing.T)
func TestClassificationRejectsLowConfidenceAndUnboundedEvidence(t *testing.T)
func TestClassificationRejectsInventedCapabilityKinds(t *testing.T)
```

The LLM schema contains only task ID, proposed domain/complexity, confidence, closed capability requirements, bounded evidence references, dependencies, and an optional indivisible reason. Low confidence returns a structured retryable validation error for the Batuta LLM; it does not become an operator question.

- [ ] **Step 2: Verify RED, then implement validation**

```bash
rtk go test ./internal/routing -run 'TestClassification' -count=1
```

Explicit valid metadata loaded from the approved artifact overrides the proposal. For Batuta tasks, domain is exactly the existing frontmatter `type`; no parallel `domain` field or dispatch-time alias exists. During pre-approval task authoring Batuta materializes the semantic domain, including reclassifying generic spec-cycle work types. Invalid authored metadata found after approval returns `reauthoring_required` as an artifact defect. Evidence entries are length/count bounded and may refer only to loaded task fields, paths, instructions, or acceptance criteria. Unknown JSON fields are rejected. The validated graph and later routing generation carry the complete task-set digest; apply rejects any artifact drift.

- [ ] **Step 3: Write decomposition RED cases**

```go
func TestDecompositionSplitsIndependentBackendAndFrontendWork(t *testing.T)
func TestDecompositionPreservesDependencyEdgesAndRejectsCycles(t *testing.T)
func TestDecompositionUsesFullstackOnlyWithIndivisibilityEvidence(t *testing.T)
func TestDecompositionRejectsDuplicateMissingAndInventedTaskIDs(t *testing.T)
```

- [ ] **Step 4: Implement deterministic graph validation**

Validate IDs, closed fields, acyclic dependencies already materialized in the approved task files, parent coverage, acceptance evidence, and the `fullstack` invariant. Deterministic code validates an LLM proposal; it does not perform semantic classification itself and never silently repairs or creates an executable graph. Semantic decomposition happens during the spec-cycle task-authoring phase before approval/import; routing may return `reauthoring_required` for an invalidly indivisible task, and Batuta re-enters task authoring rather than inventing an in-memory graph that `implement-tasks` cannot consume.

- [ ] **Step 5: Verify GREEN and commit**

```bash
rtk go test ./internal/routing -run 'TestClassification|TestDecomposition' -count=1
rtk go test -race ./internal/routing -run 'TestClassification|TestDecomposition' -count=1
rtk go vet ./internal/routing
rtk git diff --check
```

Commit:

```bash
rtk git add internal/routing/artifacts.go internal/routing/artifacts_test.go internal/routing/classification.go internal/routing/classification_test.go internal/routing/decomposition.go internal/routing/decomposition_test.go
rtk git commit -m "feat: validate task lane classification"
```

---

### Task 4: Deterministic capability and model selection

**Files:**
- Create: `internal/routing/policy.go`
- Create: `internal/routing/policy_test.go`
- Create: `internal/routing/select.go`
- Create: `internal/routing/select_test.go`
- Create: `internal/routing/generation.go`

**Interfaces:**
- Consumes: validated task graph, redacted inventory snapshot, live Compozy catalog, and closed-schema LLM fit recommendations.
- Produces: immutable `RoutingGeneration`, matrix rules, fallback chains, rejected candidates, and safe evidence.

- [ ] **Step 1: Write model-floor and hard-capability RED tests**

```go
func TestSelectorRequiresResolvedHardCapabilities(t *testing.T)
func TestSelectorAllowsOnlyExactSuccessfulProbePromotion(t *testing.T)
func TestSelectorRejectsModelsBelowComplexityFloor(t *testing.T)
func TestSelectorRejectsPairsMissingFromLiveCompozyCatalog(t *testing.T)
func TestSelectorKeepsProviderSpecificModelIDsVerbatim(t *testing.T)
func TestSelectorAppliesClosedFallbackBudgets(t *testing.T)
```

Use a versioned conservative model-tier policy. Unknown model tiers are ineligible for `high` and `critical`, not guessed from the model name. Security-sensitive hard requirements reject `declared` evidence.

- [ ] **Step 2: Verify RED, then implement filters**

```bash
rtk go test ./internal/routing -run 'TestSelector' -count=1
```

Candidate bindings are an explicit mapping from an inventoried executor to a pair in the live Compozy catalog. Validate health, required capabilities, model floor, provider/model availability, hidden/deprecated flags, and fit recommendation membership before ranking.

- [ ] **Step 3: Write stable ranking and matrix RED tests**

```go
func TestSelectorRanksFitHealthQualityPermissionsCostAndStableIDs(t *testing.T)
func TestSelectorRejectsLLMRecommendationOutsideEligibleSet(t *testing.T)
func TestGenerationEmitsOneTypeComplexityRulePerPopulatedCell(t *testing.T)
func TestGenerationDigestIsStableAndSnapshotsInventory(t *testing.T)
func TestGenerationProvidesFloorPreservingFallbackOrder(t *testing.T)
```

- [ ] **Step 4: Implement ranking and generation**

Rank by validated capability fit, resolved health, expected quality, compatible permission posture, cost, then stable `executor_id/provider_id/model_id`. Produce rules shaped exactly as:

```json
{"match":{"type":"backend","complexity":"high"},"runtime":{"provider":"codex","model":"gpt-5.6-sol","reasoning":"high"}}
```

The generation records schema/policy versions, trusted workspace identity digest, task-set digest, inventory digest, Compozy catalog generation, task classifications, selected rules, fallback chains, and safe rejections. Complexity policy also records reasoning, required verification depth, review posture, the closed per-task fallback maximum (`low=1`, `medium=2`, `high=3`, `critical=3`), the delivery-wide maximum of three, and the enclosing Loop budget ceiling. Runtime rules apply provider/model/reasoning; the `code_implementer` contract applies verification depth/review posture from authored complexity; Batuta fallback attempts consume but never enlarge the existing Loop ceiling. The daemon's own retry policy remains authoritative and is not claimed as a per-cell field. No raw configuration is stored.

- [ ] **Step 5: Verify GREEN and commit**

```bash
rtk go test ./internal/routing -count=1
rtk go test -race ./internal/routing -count=1
rtk go vet ./internal/routing
rtk git diff --check
```

Commit:

```bash
rtk git add internal/routing
rtk git commit -m "feat: select domain complexity lanes"
```

---

### Task 5: Owned matrix persistence and revisioned read-back

**Hard entry gate:** Compozy must first expose a read-only stored Loop-config
snapshot with a canonical revision and a compare-and-swap write. The current
`loop configure --file {}` uses the mutation path and can create an empty row;
it must never be used for discovery. If `compozy loop config --name
implement-tasks --workspace <trusted-id> -o json` and `loop configure
--expected-revision <revision>` are absent, stop before this task and report
the missing platform contract.

**Files:**
- Create: `internal/routing/config.go`
- Create: `internal/routing/config_test.go`
- Create: `internal/routing/ownership.go`
- Create: `internal/routing/ownership_test.go`
- Create: `internal/routing/apply.go`
- Create: `internal/routing/apply_test.go`

**Interfaces:**
- Consumes: Task 4 generation, publication command boundary, trusted workspace identity, and `COMPOZY_EXECUTABLE`.
- Produces: matrix apply/read-back result, immutable archived routing generations, durable delivery-to-generation bindings, and matrix ownership evidence. Exact-task fallback is ephemeral recovery state owned by Task 6 and never enters stored rules.

- [ ] **Step 1: Write failing configuration-boundary tests**

```go
func TestLoopConfigUsesInternalFileAndTrustedWorkspaceOnly(t *testing.T)
func TestLoopConfigReadUsesStructuredReadOnlySnapshot(t *testing.T)
func TestLoopConfigWriteUsesExpectedRevisionAndReportsConflict(t *testing.T)
func TestLoopConfigRejectsMalformedOrMismatchedReadBack(t *testing.T)
func TestLoopConfigNeverAcceptsCallerPathOrRawRules(t *testing.T)
func TestLoopConfigAppendsOwnedMatrixAndPreservesOperatorRules(t *testing.T)
```

The only mutation subprocess form is:

```text
compozy loop configure --workspace <trusted-id> --name implement-tasks --expected-revision <revision> --file <internally-created-file> -o json
```

The read-only form is `compozy loop config --workspace <trusted-id> --name implement-tasks -o json`. It returns stored config, effective config, and canonical `config_revision` without mutation. The implementation creates mutation JSON itself with mode `0600`, removes it on return, and never includes its path/content in public diagnostics.

- [ ] **Step 2: Verify RED, then implement configuration read/write**

```bash
rtk go test ./internal/routing -run 'TestLoopConfig' -count=1
```

Read the current rule array and revision, remove only journaled Batuta rules that still match their exact fingerprints, retain every non-owned decoded rule semantically unchanged and in its original relative order, then append the newly selected matrix. Compozy prefers the later declaration at equal specificity, so this lets Batuta's current cell win while retaining an equal-specificity operator cell as shadowed evidence; more-specific operator exact-ID rules still win over the matrix. Apply the complete result with that exact expected revision. On conflict, recollect/replan instead of retrying a stale merge. Read back through the read-only surface and require deep equality plus a changed canonical revision before returning success. Preserve unrelated loop config fields.

- [ ] **Step 3: Write matrix ownership RED tests**

```go
func TestOwnershipJournalUsesWorkspaceHashAndRestrictiveModes(t *testing.T)
func TestMatrixRefreshReplacesOnlyExactOwnedFingerprints(t *testing.T)
func TestMatrixRefreshPreservesModifiedAndOperatorRules(t *testing.T)
func TestMatrixRefreshBlocksDeletionWhenOwnershipCannotBeProven(t *testing.T)
func TestMatrixJournalNeverContainsRawInventoryOrCredentials(t *testing.T)
func TestRoutingGenerationArchiveSurvivesMatrixRefreshAndRestart(t *testing.T)
func TestDeliveryBindingUsesAuthoritativeRunInput(t *testing.T)
func TestGenerationGCPrunesOnlyUnreferencedTerminalDeliveries(t *testing.T)
```

- [ ] **Step 4: Implement durable matrix ownership**

Use `os.UserCacheDir()/batuta/routing/v1/<sha256(workspace-id)>.json`, directory `0700`, file `0600`, atomic replace, schema version, current matrix generation, immutable routing generations keyed by canonical digest, delivery bindings keyed by run ID, exact owned rules, and canonical fingerprints. The journal contains no credentials, raw inventory, task bodies, or raw config. Before refresh, compare journal entries against the read-only stored snapshot. Remove only exact deep-equal owned matrix rules; preserve modified and operator rules. Archive the fully validated fallback chain before returning an applied generation. A delivery's required `routing_generation` Loop input binds it durably to that archive; recovery reads the input from authoritative run status and lazily records or repairs the run binding after restart, so a later matrix refresh cannot change its candidates. Automatic GC pages through structured Batuta delivery runs and removes an archived generation only when it is neither current nor referenced by any live or recoverable run; unavailable or ambiguous status preserves it and fails safe rather than handing cleanup to the operator. Cache loss means ownership or fallback provenance cannot be proven, so Batuta preserves stored rules and returns a structured safe conflict rather than guessing. Exact-task runtime state is not represented as a stored rule.

- [ ] **Step 5: Verify GREEN and commit**

```bash
rtk go test ./internal/routing -count=1
rtk go test -race ./internal/routing -count=1
rtk go vet ./internal/routing
rtk git diff --check
```

Commit:

```bash
rtk git add internal/routing
rtk git commit -m "feat: persist owned routing generations"
```

---

### Task 6: SDK inventory and routing tools

**Hard entry gate:** The publication plan's Task 5 and this plan's Task 5 must be complete. `go.mod` must pin a released Compozy SDK/binary containing all five platform contracts: conjunctive runtime rules, the extension-specific minimum-version override, read-only revisioned Loop config and CAS writes, bounded same-lineage recovery, and the closed complexity-verification policy. Otherwise stop here and report the missing released identity; do not create a local dependency workaround.

**Files:**
- Modify: `main.go`
- Modify/Create: `internal/extensionapp/app.go`
- Create: `internal/extensionapp/inventory_tools.go`
- Create: `internal/extensionapp/inventory_tools_test.go`
- Create: `internal/extensionapp/routing_tools.go`
- Create: `internal/extensionapp/routing_tools_test.go`
- Modify: `internal/extensionapp/app_test.go`

**Tools:**
- `executor_inventory`: read-only; no input fields; returns one redacted immutable snapshot.
- `routing_plan`: read-only; accepts a canonical slug plus LLM classification/fit proposals keyed to task IDs, loads the approved task set itself under `TrustedWorkspace`, recollects inventory, and returns a proposed immutable generation containing the task-set digest.
- `routing_apply`: mutating; accepts only a closed union. `apply_matrix` carries the original typed `routing_plan` request plus its expected generation digest. `recover_delivery` carries only one validated opaque delivery run ID; the handler loads parent/child/item failure evidence, selects the next floor-preserving candidate, and passes its exact runtime directly to one durable bounded same-lineage recovery operation. `reconcile_fallbacks` carries only the delivery run ID and reloads authoritative run/runtime state to verify the replacement snapshot and decide whether another ephemeral recovery remains within the original attempt/budget ceiling. It never accepts raw rules, caller-authored failure evidence, task IDs, node IDs, item indices, runtime snapshots, or owners, and recovery never mutates stored Loop config.

- [ ] **Step 1: Write descriptor and request-boundary RED tests**

```go
func TestInventoryToolHasNoCallerControlledPathsOrCommands(t *testing.T)
func TestRoutingPlanSchemaAcceptsSlugAndProposalsButNoTaskMetadataOrPath(t *testing.T)
func TestRoutingPlanSchemaRejectsUnknownFieldsAndRawConfig(t *testing.T)
func TestRoutingApplyAcceptsOnlyClosedOwnedOperations(t *testing.T)
func TestRoutingApplyReplansAndRejectsChangedGeneration(t *testing.T)
func TestRoutingRecoveryLoadsRunEvidenceAndSnapshotsInternally(t *testing.T)
func TestRoutingRecoveryUsesPinnedGenerationAfterRefreshAndRestart(t *testing.T)
func TestRoutingRecoveryPreservesOriginalBudgetAndStoredRules(t *testing.T)
func TestOnlyBatutaAgentDeclaresRoutingApplyTool(t *testing.T)
func TestToolsRequireTrustedWorkspaceAndNeverReturnSecrets(t *testing.T)
func TestDescribeRegistersInventoryAndRoutingTools(t *testing.T)
func TestGeneratedManifestPinsExactPlatformFloor(t *testing.T)
```

- [ ] **Step 2: Verify RED**

```bash
rtk go test ./internal/extensionapp -run 'TestInventory|TestRouting|TestDescribe' -count=1
```

- [ ] **Step 3: Implement thin SDK handlers**

Handlers translate `ToolRequest.TrustedWorkspace`, reject missing identity/root, call Tasks 1–5, and return typed JSON. They do not contain adapter, classification, selection, or recovery policy. `apply_matrix` recollects inventory, reloads the task set, reruns the deterministic plan from the supplied typed request, and archives the winning immutable generation before returning it; it mutates only when the fresh canonical digest equals the caller's expected digest. This closes the plan/apply gap without making the read-only plan tool persist hidden state. `recover_delivery` accepts only the parent delivery run ID, reads its required `routing_generation` input from authoritative status, loads that exact archived fallback chain, lazily repairs the run binding, discovers child/item lineage and prior recovery evidence, then passes one selected exact runtime into the fixed `compozy loop recover-nested` CLI operation. It never exposes the raw native recovery tool to Batuta. The next terminal return calls `reconcile_fallbacks`, which verifies authoritative runtime/budget evidence before another recovery or normal reporting. Stored Loop rules remain byte-for-byte unchanged by recovery. Register the tools alongside the publication tools in the same fixed SDK runtime. Set `ExtensionDefinition.MinCompozyVersion` to the exact released five-contract floor and assert the generated manifest carries it; the earlier publication-only floor is no longer sufficient. Protect the mutating surface with its closed input union, daemon-authenticated `TrustedWorkspace`, and the shipped agent definitions: only the Batuta conductor declares `ext__batuta__routing_apply` in its exact tool allowlist. No required-hook dependency or parallel authorization mechanism is introduced.

- [ ] **Step 4: Verify GREEN and commit**

```bash
rtk go test ./internal/extensionapp ./internal/inventory/... ./internal/routing -count=1
rtk go test -race ./internal/extensionapp ./internal/inventory/... ./internal/routing -count=1
rtk go vet ./internal/extensionapp ./internal/inventory/... ./internal/routing
rtk git diff --check
```

Commit:

```bash
rtk git add main.go internal/extensionapp
rtk git commit -m "feat: expose autonomous routing tools"
```

---

### Task 7: Batuta classification, routing, dispatch, and recovery contract

**Files:**
- Modify: `agents/batuta/AGENT.md`
- Rewrite: `resources/skills/batuta-routing/SKILL.md`
- Modify: `loops/batuta-deliver/loop.yaml`
- Modify: `tests/contract/test_02_routing_dryrun.sh`
- Modify: `tests/contract/test_02_routing_pair_selection.sh`
- Modify: `tests/contract/test_routing_pair_selection.py`
- Modify: `tests/contract/test_02_spec_cycle_surface.sh`
- Create: `tests/contract/test_02_domain_lane_surface.sh`
- Create: `tests/e2e/assert_domain_routing.py`
- Create: `tests/e2e/test_assert_domain_routing.py`

- [ ] **Step 1: Write the failing autonomous contract tests**

Assert:

- no `auto_commit` preference gate or operator routing confirmation remains;
- explicit valid task metadata wins, while LLM output is closed-schema and deterministically validated;
- the agent calls inventory → routing plan → routing apply/read-back before delivery;
- stored rules cover populated `type + complexity` cells with exact live catalog IDs;
- each approved task stores its canonical domain directly in `type`, with no generic work-type alias at dispatch;
- availability `unknown` is ineligible for hard requirements;
- failures cause an ephemeral exact-runtime same-lineage recovery within existing budgets, followed by authoritative runtime reconciliation and no stored-rule mutation;
- `batuta-deliver` declares `iteration_cap: 4`, and recovery stops at the lower of task allowance, delivery-wide allowance, available candidates, and the original daemon budget;
- `load_check.output.highest_complexity` is passed as the `review-and-fix` delivery-review complexity, while each `implement-tasks` item retains its own authored complexity;
- a terminal return first reconciles any prior recovery, then starts at most one eligible same-lineage recovery without asking the operator;
- terminal-effect `message_id` and `idempotency_key` include `loop_run_id`, effect generation, and trigger, so two distinct recovery settlements each wake exactly once;
- operator-authored rules survive reconciliation;
- dispatch reports the immutable routing generation and still sends literal `auto_commit: true` through the delivery loop.
- dispatch passes the exact applied digest as required `routing_generation`, and recovery after matrix refresh or restart uses only that pinned archive;

- [ ] **Step 2: Verify RED**

```bash
rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk python3 -m unittest tests.e2e.test_assert_domain_routing
```

- [ ] **Step 3: Rewrite the Batuta routing contract**

The agent automatically inventories, classifies, requests any semantic task split during task authoring, retries invalid low-confidence LLM output, plans and applies routing, proves the read-back, dispatches, publishes, and reconciles ephemeral recovery evidence. It asks the operator only for missing product requirements or a genuine external blocker such as unavailable credentials. It never asks the operator to select a model, executor, lane, commit preference, fallback, cleanup action, or approve healthy publication.

Set `permissions: approve-all` only together with an explicit Batuta conductor tool allowlist covering its documented spec-cycle, inventory/routing, worktree, Loop dispatch/status, configuration-budget, skill, and session-return surfaces. The exact list is asserted by contract tests; shell, arbitrary filesystem, raw config, unrelated extension tools, and direct `compozy__loop_recover_nested` are absent. Recovery is reachable only through the guarded `ext__batuta__routing_apply`, whose fixed implementation invokes the structured CLI internally after deterministic candidate selection. On every terminal-effect turn, Batuta first reads the exact delivery status, calls `routing_apply.reconcile_fallbacks` for that run, and, when the result reports one recoverable candidate within the original lineage budget, calls `routing_apply.recover_delivery` once and ends the turn. The Compozy recovery operation reopens the same parent/child lineage, carries successful task items, reruns only the failed item and transitive dependents with the ephemeral exact runtime, and preserves original token/wall-clock/iteration accounting. Stored matrix/operator rules are untouched. Its next terminal effect repeats reconciliation. Change every terminal-effect `message_id` and `idempotency_key` to include `{{ .effect.identity.loop_run_id }}`, `{{ .effect.identity.generation }}`, and `{{ .effect.identity.trigger }}`; Compozy already exposes generation in effect identity. Only an exhausted fallback chain or genuine external prerequisite is reported blocked.

Change the authored `batuta-deliver` contract from `iteration_cap: 1` to
`iteration_cap: 4` before dispatch. Generation 1 is the initial delivery and
at most generations 2–4 are recoveries. The routing journal and authoritative
run status must agree on the delivery-wide count; a mismatch fails closed.
Pass `{{ .nodes.load_check.output.highest_complexity }}` into the review
child's `complexity` input so the whole-slug independent review enforces the
strongest authored verification row without changing per-task implementation
classification.
Add required string input `routing_generation` to `batuta-deliver` and pass the
exact digest returned by `apply_matrix` on every dispatch. The Loop snapshot
therefore carries the binding even if Batuta or the extension restarts before
the first terminal reconciliation.

The skill defines the canonical taxonomy, evidence states, hard requirements, model floors, stable ranking, matrix shape, ownership lifecycle, and exact failure classes. Remove dated example provider/model IDs and the rule that `unknown` may be used as a fallback.

- [ ] **Step 4: Verify GREEN and commit**

```bash
rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk bash tests/contract/test_02_routing_pair_selection.sh
rtk python3 -m unittest tests.e2e.test_assert_domain_routing
rtk git diff --check
```

Commit:

```bash
rtk git add agents/batuta/AGENT.md resources/skills/batuta-routing/SKILL.md loops/batuta-deliver/loop.yaml tests/contract tests/e2e
rtk git commit -m "feat: route batuta by domain and complexity"
```

---

### Task 8: Matrix integration, release contract, QA, and public documentation

**Files:**
- Modify: `.github/workflows/ci.yml`
- Modify: `.github/workflows/release.yml`
- Modify: `scripts/check-compozy-version.sh`
- Modify: `scripts/stage-extension.sh`
- Modify: `README.md`
- Modify: `README.pt-BR.md`
- Modify: `docs/architecture.md`
- Modify: `docs/how-it-works.md`
- Modify: `docs/verify.md`
- Modify: `CONTRIBUTING.md`
- Modify/Create: `docs/releases/<next-beta>.md`
- Create: `tests/integration/routing_matrix_test.go`
- Modify: `tests/e2e/SMOKE.md`
- Create: `docs/internal/qa/2026-08-25-batuta-domain-lanes.md`

- [ ] **Step 1: Add the Compozy matrix integration test**

In an isolated workspace/config home, install/build the Batuta extension, seed at least four distinct live or fixture-backed runtime rules, and prove:

- `backend × low` and `frontend × high` resolve to distinct exact provider/model pairs;
- `type + complexity` beats type-only and complexity-only matches;
- an ephemeral recovery runtime has precedence over the matrix cell for only the recovered child generation;
- read-back preserves field provenance and routing generation;
- recovery applies the exact task runtime ephemerally and leaves stored rules unchanged.

- [ ] **Step 2: Add secret and lifecycle integration coverage**

Exercise all four adapters with secret canaries and malformed/skewed fixtures through the public SDK tool. Assert no canary in tool JSON, logs, journal, classification payload, routing generation, or diagnostics. Run two fallback settlements through select → same-lineage recover → authoritative runtime snapshot → reconcile, assert each generation wakes Batuta exactly once, assert stored Loop config never changes, then verify pending extension processes are empty.

- [ ] **Step 3: Update release/CI/staging contracts**

Require the exact released Compozy floor, run all inventory/routing tests, build the same Linux/amd64 code-backed bundle established by the publication plan, and include the new tools/resources in staged inventory assertions. Do not claim macOS/Windows release support until platform-aware bundles exist.

- [ ] **Step 4: Update public docs and release note**

Document automatic inventory, redaction guarantees, `resolved|declared|unknown`, domain taxonomy, complexity floors, matrix precedence, ephemeral exact-runtime recovery, matrix ownership refresh, literal `auto_commit=true`, automatic healthy-path publication, blocker-only operator recovery, and manual merge. State that foreign executor configuration informs capability selection but only Compozy-reported bindings execute tasks. Record the operator-routing, configurable `auto_commit`, and healthy-path publication-gate removal as breaking beta behavior.

- [ ] **Step 5: Run focused verification**

```bash
rtk go test ./... -count=1
rtk go test -race ./internal/inventory/... ./internal/routing ./internal/extensionapp -count=1
rtk go vet ./...
rtk python3 -m unittest discover -s tests/e2e -p 'test_*.py'
rtk bash tests/contract/test_00_runtime_guard.sh
rtk bash tests/contract/test_01_stage.sh
rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk bash tests/contract/test_02_routing_dryrun.sh
rtk bash tests/contract/test_04_deliver_validate.sh
rtk git diff --check
```

- [ ] **Step 6: Run disposable contract and isolated E2E smokes**

Create a detached disposable worktree outside the repository's `.compozy/` marker, run `tests/contract/run.sh`, and remove only that worktree afterward. Then run the isolated smoke with at least two domains and two complexities, proving resolved runtime provenance, one commit per task, committed review fixes, clean exact HEAD, push, a real PR URL, and final exact-HEAD verification. Record unavailable credentials/provider access as `blocked-verify` and stop release acceptance; do not weaken acceptance, substitute fixture evidence for the external result, or hand cleanup to the operator.

- [ ] **Step 7: Final review and commit**

Request independent code/spec review, address every branch-owned finding with RED/GREEN evidence, rerun affected gates, and commit:

```bash
rtk git add .github scripts README.md README.pt-BR.md docs CONTRIBUTING.md tests
rtk git commit -m "docs: ship autonomous domain routing"
```

## Stop Conditions

- Stop before Task 6 if no released Compozy SDK/runtime contains all five platform contracts: conjunctive runtime rules, extension-specific minimum daemon version, read-only revisioned Loop config with CAS, same-lineage nested recovery with an ephemeral exact runtime, and the closed complexity-verification policy.
- Stop before applying rules if inventory or catalog generation changed since planning; recollect and replan automatically.
- Stop a route only when no candidate can satisfy a hard requirement or a genuine external prerequisite is unavailable. Report the precise blocker; do not provide operator routing or cleanup steps.
- Stop final completion if any secret canary crosses the inventory boundary, any rule is not confirmed by read-back, any fallback lacks provable Batuta ownership, any task executes below its complexity floor, or publication lacks a real PR at the exact verified HEAD.

## Deferred Work

Do not implement graph engineering in this plan. After publication and domain lanes are complete, evaluate Compozy's released graph contract for conflict sets, shared-resource ownership, dependency joins, concurrency budgets, deterministic commit integration, and merge/reconciliation nodes as specified in the design's deferred follow-up.
