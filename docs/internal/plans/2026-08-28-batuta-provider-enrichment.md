# Batuta provider-authoritative routing with CLI enrichment — implementation plan

> Implement with strict TDD. Each task begins with a focused failing test,
> makes the smallest production change, reruns focused and affected broad
> gates, self-reviews the diff, and creates one Conventional Commit. Do not
> modify Compozy or create persisted-state migrations.

**Goal:** Make every live Compozy provider/model pair routable independently
of Batuta adapters while preserving optional CLI evidence, and add bounded
Claude Code and Agy enrichers.

**Architecture:** Compozy remains the execution and catalog authority.
`BuildCandidateBindings` creates one Compozy-owned candidate per live exact
provider/model pair. CLI adapters attach evidence to that candidate but never
create or delete it. Fit, selection, and generation identity use the exact
provider/model pair; current `executor_id` wire fields remain as the execution
owner and are written as `compozy` for new generations.

**Tech stack:** Go, Compozy Go extension SDK, YAML Loop resources, Bash/Python
contract tests.

**Design:**
[`../specs/2026-08-28-batuta-provider-enrichment-design.md`](../specs/2026-08-28-batuta-provider-enrichment-design.md)

## Scope constraints

- Batuta repository only. Stop before any Compozy edit.
- Keep `ext__batuta__executor_inventory` and the current nine-tool public
  inventory; this feature changes its payload semantics, not its tool ID.
- No database, Compozy config, routing-journal, or workspace migration.
- Existing routing generations remain readable and are never rewritten.
- Do not make network login, install, refresh, agent-run, or config-mutating
  probes.
- Preserve the pre-existing untracked `docs/demo-e2e.md`; it is outside this
  plan.

---

### Task 1: Make the live Compozy catalog the candidate authority

**Files:**
- Modify: `internal/inventory/types.go`
- Modify: `internal/inventory/types_test.go`
- Modify: `internal/inventory/redact.go`
- Modify: `internal/inventory/redact_test.go`
- Modify: `internal/inventory/adapters/compozy.go`
- Modify: `internal/inventory/adapters/adapters_test.go`
- Modify: `internal/inventory/adapters/testdata/compozy.json`
- Modify: `internal/routing/select.go`
- Modify: `internal/routing/select_test.go`
- Modify: `internal/routing/generation.go`
- Create: `internal/routing/generation_test.go`
- Modify: `internal/extensionapp/routing_runtime.go`
- Modify: `internal/extensionapp/routing_tools_test.go`

- [ ] **Step 1: Write provider-authority RED tests**

Add focused tests with these exact behaviors:

```go
func TestBuildCandidateBindingsIncludesLivePairWithoutDedicatedAdapter(t *testing.T)
func TestBuildCandidateBindingsRejectsAdapterOnlyPairAbsentFromLiveCatalog(t *testing.T)
func TestBuildCandidateBindingsDeduplicatesExactRuntimeAcrossEnrichers(t *testing.T)
func TestBuildCandidateBindingsRejectsHiddenDeprecatedAndUnavailableModels(t *testing.T)
func TestBuildCandidateBindingsRejectsAuthoritativeMissingProviderAuth(t *testing.T)
func TestFitUniverseUsesProviderAndModelInsteadOfAdapterIdentity(t *testing.T)
func TestNewRoutingGenerationWritesCompozyAsExecutionOwner(t *testing.T)
func TestExistingRoutingGenerationWithLegacyExecutorRemainsValid(t *testing.T)
func TestCompozyNormalizationPreservesExactLiveModelEvidence(t *testing.T)
func TestRedactPreservesSafeProviderBindingsAndDropsUnknownCanaries(t *testing.T)
```

Fixture cases must include:

- `claude/claude-fixture` and `gemini/gemini-fixture` as live pairs with no
  dedicated adapter;
- the same `cursor/grok-4.6` pair reported by Compozy and Cursor;
- a CLI-only `invented/model` pair;
- hidden, deprecated, stale, and unavailable catalog rows; and
- provider auth `configured|authenticated|none` ready states;
- `missing_cli|missing_credential|needs_login|permission_denied` rejection;
- `rate_limited|transient|unknown` explicit degraded evidence; and
- an unrecognized future auth state normalized to unknown/degraded; and
- provider-list message, command, login, and auth canaries absent from
  normalized output and diagnostics.

- [ ] **Step 2: Verify RED**

```bash
rtk go test ./internal/inventory/... ./internal/routing ./internal/extensionapp \
  -run 'BuildCandidateBindings|FitUniverse|RoutingGeneration|CompozyNormalization|RedactPreserves' \
  -count=1
```

- [ ] **Step 3: Implement the minimal authority split**

Keep `inventory.SchemaVersion` at 1 and the top-level `executors` field. Add
typed
`ProviderBindings []ProviderBinding \`json:"provider_bindings,omitempty"\``
to `ExecutorSnapshot` and clone/canonicalize/validate it with the rest of the
snapshot. `ProviderBinding` has separate required `ProviderID` and optional
`ModelID` fields so model IDs containing `/` never require ambiguous string
splitting. Do not store raw provider responses.

Extend the closed public redaction vocabulary with only
`provider_bindings`, `provider_id`, and `model_id`. Preserve these normalized
fields while proving unknown keys and secret canaries are still dropped. The
public snapshot digest remains the digest of the visible canonical structure.

Change candidate construction so it iterates the validated `LiveCatalog`
first and creates exactly one binding per available exact pair. Set
`ExecutorID` to `inventory.ExecutorCompozy` for new bindings and add sorted,
deduplicated
`EnrichmentIDs []inventory.ExecutorID \`json:"enrichment_ids,omitempty"\``
to candidate and generation evidence. Attach enrichers only after candidate
creation.

Change `FitCandidate` uniqueness and fit validation to exact
`provider_id + model_id`. Retain required `executor_id` on the beta wire but
accept its existing closed enum and ignore it for admission, uniqueness, and
ranking; new Batuta prompts will emit `compozy`. Reject duplicate pair
recommendations even when the caller varies the legacy field. Continue
decoding legacy stored routing generations; do not migrate or rewrite them.

`liveCatalogFromInventory` remains the only catalog projection owner. Parse
only `providers[].name` and `auth_status.state` into enum-only normalized
evidence, carry its reduced credential state on `CatalogModel`, and reject
authoritative missing CLI/credential/login/permission states. Treat the live
model row plus provider auth as the executable admission signal; never let an
adapter credential field override it. Provider status messages, commands,
login metadata, and raw payload remain redacted and never enter a candidate.

- [ ] **Step 4: Verify GREEN**

```bash
rtk go test ./internal/inventory/... ./internal/routing ./internal/extensionapp \
  -run 'BuildCandidateBindings|FitUniverse|RoutingGeneration|CompozyNormalization|RedactPreserves' \
  -count=1
rtk go test -race ./internal/inventory/... ./internal/routing ./internal/extensionapp \
  -run 'BuildCandidateBindings|FitUniverse|RoutingGeneration|CompozyNormalization|RedactPreserves' \
  -count=1
rtk go vet ./internal/inventory/... ./internal/routing ./internal/extensionapp
rtk git diff --check
```

- [ ] **Step 5: Commit**

```bash
rtk git add internal/inventory internal/routing internal/extensionapp
rtk git commit -m "refactor: make Compozy routing authority"
```

---

### Task 2: Turn existing CLI adapters into optional enrichers

**Files:**
- Modify: `internal/inventory/adapters/adapter.go`
- Modify: `internal/inventory/adapters/collect.go`
- Modify: `internal/inventory/adapters/codex.go`
- Modify: `internal/inventory/adapters/cursor.go`
- Modify: `internal/inventory/adapters/opencode.go`
- Modify: `internal/inventory/adapters/adapters_test.go`
- Modify: `internal/inventory/collect_test.go`
- Modify: `internal/routing/select.go`
- Modify: `internal/routing/select_test.go`
- Modify: `internal/routing/policy.go`
- Create: `internal/routing/policy_test.go`

- [ ] **Step 1: Write enricher-semantics RED tests**

```go
func TestBuildCandidateBindingsKeepsCatalogPairsWhenEveryOptionalEnricherIsMissing(t *testing.T)
func TestCollectorAssociatesConstructorOwnedProviderBindings(t *testing.T)
func TestCollectorRejectsDynamicProviderBindingFromRawOutput(t *testing.T)
func TestEnricherFailureChangesEvidenceButNotCandidateUniverse(t *testing.T)
func TestEnricherInstallChangesRankingButNotRuntimeIdentity(t *testing.T)
func TestSecuritySensitiveRequirementNeedsResolvedEnrichmentOrExactProbe(t *testing.T)
func TestDeclaredEnrichmentCannotSatisfySecuritySensitiveRequirement(t *testing.T)
func TestExactProbeFromAttachedEnricherSatisfiesRequirement(t *testing.T)
func TestExactProbeFromUnattachedExecutorIsIgnored(t *testing.T)
func TestUnknownModelTierDefaultsToStandardOnly(t *testing.T)
```

Run each candidate-universe test twice: once with all optional executables
missing and once with Codex/Cursor/OpenCode fixtures present. Assert identical
ordered provider/model pairs and different bounded evidence/ranking only.

- [ ] **Step 2: Verify RED**

```bash
rtk go test ./internal/inventory/... ./internal/routing \
  -run 'Test(BuildCandidateBindings|Collector|Enricher|SecuritySensitive|Declared|ExactProbe|UnknownModelTier)' \
  -count=1
```

- [ ] **Step 3: Implement the enricher registry**

Populate the typed `ExecutorSnapshot.ProviderBindings` field from adapter
normalizers. Values are supplied only by constructors or safely parsed output
and validated as safe public identifiers. Codex binds to `codex`, Cursor to
`cursor`, and OpenCode attaches only to exact safely parsed provider/model
evidence. Compozy is catalog authority rather than an optional enricher.

Keep `ProbeSpec.Executor` as the closed probe owner so command ownership,
budgets, redaction, and diagnostics remain unchanged. Extend candidate
capability lookup to the union of the Compozy snapshot and its ordered attached
enrichers. An absent/unknown enricher contributes no evidence and no rejection
by itself.

An exact capability probe counts only when its `ExecutorID` is `compozy` or is
present in that candidate's `EnrichmentIDs`, and its inventory digest, kind,
and requirement ID all match. A probe owned by another installed but
unattached adapter is ignored. Preserve the current rule that a
security-sensitive requirement needs resolved evidence rather than a probe.

Set an unclassified live model's policy tier to `ModelTierStandard`. Existing
exact model-tier entries remain unchanged. Medium/high/critical floors still
reject a model without an explicit sufficient tier.

- [ ] **Step 4: Verify GREEN and compatibility**

```bash
rtk go test ./internal/inventory/... ./internal/routing -count=1
rtk go test -race ./internal/inventory/... ./internal/routing -count=1
rtk go vet ./internal/inventory/... ./internal/routing
rtk git diff --check
```

- [ ] **Step 5: Commit**

```bash
rtk git add internal/inventory internal/routing
rtk git commit -m "refactor: treat CLI probes as enrichers"
```

---

### Task 3: Add bounded Claude Code and Agy enrichers

**Files:**
- Modify: `internal/inventory/types.go`
- Create: `internal/inventory/adapters/claude.go`
- Create: `internal/inventory/adapters/agy.go`
- Modify: `internal/inventory/adapters/adapter.go`
- Modify: `internal/inventory/adapters/collect.go`
- Modify: `internal/inventory/adapters/adapters_test.go`
- Create: `internal/inventory/adapters/testdata/claude.json`
- Create: `internal/inventory/adapters/testdata/agy.json`
- Modify: `internal/inventory/collect_test.go`
- Modify: `internal/extensionapp/app.go`
- Modify: `internal/extensionapp/app_test.go`

- [ ] **Step 1: Write exact-command and normalization RED tests**

```go
func TestClaudeAdapterUsesOnlyReadOnlyBoundedCommands(t *testing.T)
func TestAgyAdapterUsesOnlyReadOnlyBoundedCommands(t *testing.T)
func TestClaudeAdapterNormalizesInstalledMissingMalformedPartialAndSkewed(t *testing.T)
func TestAgyAdapterNormalizesInstalledMissingMalformedPartialAndSkewed(t *testing.T)
func TestClaudeAndAgyAdaptersNeverLeakSecretCanaries(t *testing.T)
func TestMissingClaudeAndAgyEnrichersDoNotRemoveLivePairs(t *testing.T)
func TestApplicationDiscoversAbsoluteClaudeAndAgyExecutables(t *testing.T)
func TestLiveInventoryDigestChangesWhenEnrichmentChangesButCatalogGenerationDoesNot(t *testing.T)
func TestCollectorAlwaysReportsAllSixRecordsWhenOptionalBinariesAreMissing(t *testing.T)
```

Lock these command shapes exactly:

```text
claude --version
claude plugin list --json

agy --version
agy agent
agy plugin list
```

Fixtures must cover a secret canary in every raw field, malformed JSON/text,
truncated output, unknown future fields, duplicate identifiers, and outputs
over the normalized record cap. No test may call the user's installed Claude
or Agy binary.

Place digest/candidate invariance coverage in `internal/inventory/collect_test.go`
using the existing fake runner, not in the build-tagged live integration file.
Rename the existing four-record missing-binary test to the six-record name and
update its exact count. `agy agent --help` in Agy 1.1.14 identifies the bare
command as “List available agents”; fixtures lock that meaning. Stop and remove
the probe if a supported CLI version changes it.

- [ ] **Step 2: Verify RED**

```bash
rtk go test ./internal/inventory/... ./internal/extensionapp \
  -run 'Claude|Agy|EnrichmentChanges|Collector' -count=1
```

- [ ] **Step 3: Implement both enrichers**

Add `ExecutorClaude = "claude"` and `ExecutorAgy = "agy"` only as probe/evidence
owner IDs. Claude emits a provider-only `claude` association. Agy emits no
runtime binding because `agy models` is an authenticated network fetch in Agy
1.1.14; it contributes only bounded local version, agent, and plugin evidence.
Agy-backed providers/models remain generic candidates from Compozy's live
catalog, and Batuta does not infer custom provider aliases.
Neither adapter emits authoritative runtime model IDs unless an output can be
unambiguously matched to an existing Compozy pair later in candidate binding.

Extend `CollectorOptions`, the fixed builder list, `inventoryExecutables`, and
`optionalExecutable` wiring. Preserve one shared collection timeout, probe
limit, record limit, diagnostic limit, and parallelism cap across all six
records. Do not raise budgets merely because two adapters were added; tests
must prove deterministic degradation when the shared budget is exhausted.

Normalize only safe identifiers and digests. Agy capability evidence remains
inventory-only; runtime provider/model IDs always come from Compozy.

- [ ] **Step 4: Verify GREEN**

```bash
rtk go test ./internal/inventory/... ./internal/extensionapp \
  -run 'Claude|Agy|Inventory|Describe|Collector' -count=1
rtk go test -race ./internal/inventory/... ./internal/extensionapp \
  -run 'Claude|Agy|Inventory' -count=1
rtk go vet ./internal/inventory/... ./internal/extensionapp
rtk git diff --check
```

- [ ] **Step 5: Commit**

```bash
rtk git add internal/inventory internal/extensionapp
rtk git commit -m "feat: enrich routing with Claude and Agy"
```

---

### Task 4: Close routing schemas, policy evidence, and replay compatibility

**Files:**
- Modify: `internal/extensionapp/routing_tools.go`
- Modify: `internal/extensionapp/routing_tools_test.go`
- Modify: `internal/extensionapp/routing_runtime.go`
- Modify: `internal/extensionapp/routing_recovery_test.go`
- Modify: `internal/extensionapp/delivery_integration_test.go`
- Modify: `internal/extensionapp/parallel_delivery_integration_test.go`
- Modify: `internal/routing/generation.go`
- Modify: `internal/routing/generation_test.go`
- Modify: `internal/routing/ownership_test.go`
- Modify: `internal/routing/select_test.go`

- [ ] **Step 1: Write public-schema and replay RED tests**

```go
func TestRoutingPlanFitSchemaKeysCandidatesByProviderAndModel(t *testing.T)
func TestRoutingPlanRejectsCallerSuppliedEnrichmentIdentity(t *testing.T)
func TestRoutingOutputRecordsSortedEnrichmentEvidence(t *testing.T)
func TestRoutingOutputNeverReturnsRawAdapterPayload(t *testing.T)
func TestLegacyArchivedGenerationReplaysWithoutInventoryRewrite(t *testing.T)
func TestRecoveryUsesArchivedExactProviderModelWithoutReadingRefreshedInventory(t *testing.T)
func TestGenericProviderSurvivesRestartAndMatrixReconciliation(t *testing.T)
func TestParallelDeliveryCanSelectGenericAndEnrichedProvidersInOneGraph(t *testing.T)
```

The compatibility fixture must load a real prior JSON generation whose
`executor_id` is `codex` or `cursor-agent`, then reconcile/recover it without
changing provider, model, digest, or journal bytes.

- [ ] **Step 2: Verify RED**

```bash
rtk go test ./internal/routing ./internal/extensionapp \
  -run 'Test(RoutingPlan|RoutingOutput|LegacyArchived|RecoveryUsesArchived|GenericProvider|ParallelDeliveryCanSelect)' \
  -count=1
```

- [ ] **Step 3: Implement closed wire changes**

Update routing output JSON schemas for server-derived enrichment evidence and
reject caller input named `enrichment_ids`. Keep the existing closed
`executor_id` fit field for rolling compatibility, but exclude it from fit
identity and ranking; new documented requests use `compozy`. Canonicalize
enrichment IDs before hashing the routing generation. Keep
`routingGenerationSchemaVersion` at 1 and tag every new nested enrichment field
with `omitempty`; a decoded legacy generation must marshal byte-equivalently
for digest verification. Keep the ownership-journal envelope, decoder, and
migration code unchanged.

The ownership store continues persisting immutable generations. Its decoder
accepts the prior generation shape and the new shape, but the writer emits only
the new canonical form. No bulk rewrite occurs at startup or apply time.

- [ ] **Step 4: Verify GREEN and broad routing behavior**

```bash
rtk go test ./internal/routing ./internal/extensionapp -count=1
rtk go test -race ./internal/routing ./internal/extensionapp -count=1
rtk go vet ./internal/routing ./internal/extensionapp
rtk git diff --check
```

- [ ] **Step 5: Commit**

```bash
rtk git add internal/routing internal/extensionapp
rtk git commit -m "fix: preserve provider routing evidence"
```

---

### Task 5: Update Batuta behavior, contracts, docs, and QA

**Files:**
- Modify: `agents/batuta/AGENT.md`
- Modify: `resources/skills/batuta-routing/SKILL.md`
- Modify: `README.md`
- Modify: `README.pt-BR.md`
- Modify: `docs/architecture.md`
- Modify: `docs/how-it-works.md`
- Modify: `docs/verify.md`
- Modify: `tests/contract/test_02_domain_lane_surface.sh`
- Modify: `tests/contract/test_02_routing_pair_selection.sh`
- Modify: `tests/contract/select_routing_pair.py`
- Modify: `tests/contract/test_routing_pair_selection.py`
- Modify: `tests/e2e/assert_domain_routing.py`
- Modify: `tests/e2e/test_assert_domain_routing.py`
- Create: `docs/internal/qa/2026-08-28-batuta-provider-enrichment.md`

- [ ] **Step 1: Write documentation-contract RED tests**

Assert all of the following:

- agent and skill say Compozy is the only provider/model execution authority;
- new fit candidates contain `executor_id: compozy` plus provider/model, while
  the legacy closed executor values remain accepted-but-ignored and no
  caller-authored enrichment ID is accepted;
- a missing adapter cannot exclude a live pair;
- adapters are optional evidence and hard capabilities still need proof;
- Claude and Agy are named as supported enrichers, not standalone execution
  backends;
- Agy enrichment never rewrites a runtime ID, never invokes the network-backed
  `agy models` command, and leaves all provider/model authority to Compozy;
- the contract helper rejects `missing_cli`, `missing_credential`,
  `needs_login`, and `permission_denied`, while treating unknown auth as
  degraded rather than as capability proof;
- English and Portuguese public docs express the same boundary; and
- no documentation asks for a Compozy patch, config rewrite, or migration.

- [ ] **Step 2: Verify RED**

```bash
rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk bash tests/contract/test_02_routing_pair_selection.sh
rtk python3 -m unittest tests.e2e.test_assert_domain_routing
```

- [ ] **Step 3: Update the agent and routing skill**

Teach Batuta to recommend exact live `provider_id + model_id` pairs. It may use
enrichment evidence to justify capability fit but must never propose or echo an
enrichment ID as execution input. If a hard requirement is unproven, it either
chooses another proven candidate or returns the exact blocker; it does not ask
the operator to choose a CLI.

Document conservative unknown-tier behavior: a new live provider can handle
low-complexity work generically, while stronger floors require explicit
versioned model policy. Keep the existing autonomous delivery, recovery,
review, publication, and manual-merge boundaries unchanged.

Align `tests/contract/select_routing_pair.py` with the authoritative auth
vocabulary and add exact test cases for all four ineligible states plus
unknown/degraded. Update the skill assertion so “unknown capability evidence
cannot prove a hard requirement” is not misread as “every unknown provider
auth state is absent from the live candidate universe.”

- [ ] **Step 4: Update public docs and truthful QA**

Explain the authority/enrichment split in paired English/Portuguese copy. Add
Claude and Agy examples without claiming either is configured on the user's
machine. In QA, record fixture-backed evidence separately from an optional
isolated live-catalog observation. Mark live Claude/Agy execution
`blocked-verify` unless the isolated Compozy catalog reports an exact
`available_live` pair and the resulting child runtime provenance is observed.
Treat the canonical `claude` and `gemini` provider IDs, provider-auth vocabulary
including `configured`, and supported Claude/Agy local command flags as QA
hypotheses until isolated fixtures/labs confirm them. Record separately that
`agy models` is network-backed and intentionally excluded; do not turn its
remote display names into runtime IDs or a release claim.

- [ ] **Step 5: Run final gates**

For Go builds/tests with potentially large scratch, create one unique directory
under `/home/francisross/tmp-builds`, pass it only to the command through
`TMPDIR`, and remove only that directory afterward. Do not set `TMPDIR` or
`GOTMPDIR` for Compozy `make gate*`; this plan does not run or edit Compozy.

```bash
rtk go test ./... -count=1
rtk go test -race ./internal/inventory/... ./internal/routing ./internal/extensionapp -count=1
rtk go vet ./...
rtk python3 -m unittest discover -s tests/e2e -p 'test_*.py'
rtk bash tests/contract/test_02_domain_lane_surface.sh
rtk bash tests/contract/test_02_routing_pair_selection.sh
rtk bash tests/contract/test_02_routing_dryrun.sh
rtk bash tests/contract/test_04_deliver_validate.sh
rtk bash tests/contract/test_07_public_docs.sh
rtk git diff --check
```

Run `tests/contract/run.sh` only from a disposable detached Batuta worktree
without `.compozy/`, as required by repository instructions. Do not mutate the
shared Compozy installation to manufacture live-provider evidence.

- [ ] **Step 6: Request independent review, fix, and commit**

Request a read-only review of the implementation against the design and this
plan. Address every valid branch-owned finding with a focused RED/GREEN test,
rerun affected gates, and commit only Batuta product/docs/QA files:

```bash
rtk git add agents resources internal README.md README.pt-BR.md docs tests
rtk git commit -m "docs: explain provider routing enrichment"
```

## Stop conditions

- Stop if implementation would require a Compozy code or API change; report
  the exact missing public contract before editing Compozy.
- Stop planning when the Compozy catalog generation is absent or changes
  between inventory and apply; recollect and replan.
- Stop selection when no live pair satisfies the complexity floor and every
  hard capability with resolved evidence.
- Stop release qualification if a secret canary, raw CLI output, caller-owned
  provider binding, or unbounded probe crosses the inventory boundary.
- Stop live-support claims unless exact isolated Compozy runtime provenance is
  observed. Fixture evidence proves Batuta behavior, not provider availability.
