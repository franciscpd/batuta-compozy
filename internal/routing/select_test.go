package routing

import (
	"errors"
	"slices"
	"testing"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

func TestSelectorRequiresResolvedHardCapabilities(t *testing.T) {
	t.Parallel()

	fixture := selectionFixture()
	fixture.Graph.Tasks[0].Requirements = []CapabilityRequirement{{Kind: CapabilityMCP, ID: "playwright", Hard: true}}
	recommended := slices.Clone(fixture.Fit[0].Candidates)
	fixture.Fit[0].Candidates = nil
	fixture.Inventory.Executors[0].Capabilities = append(fixture.Inventory.Executors[0].Capabilities, inventory.Evidence{
		Name: "mcp", State: inventory.ResolutionDeclared, Identifiers: []string{"playwright"},
	})
	if _, err := fixture.Select(); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("Select(declared hard capability) error = %v, want ErrNoEligibleCandidate", err)
	}

	fixture.Inventory.Executors[0].Capabilities[len(fixture.Inventory.Executors[0].Capabilities)-1].State = inventory.ResolutionResolved
	fixture.Fit[0].Candidates = recommended
	if _, err := fixture.Select(); err != nil {
		t.Fatalf("Select(resolved hard capability) error = %v", err)
	}
}

func TestSelectorAllowsOnlyExactSuccessfulProbePromotion(t *testing.T) {
	t.Parallel()

	fixture := selectionFixture()
	fixture.Graph.Tasks[0].Requirements = []CapabilityRequirement{{Kind: CapabilityBrowserTooling, ID: "chromium", Hard: true}}
	recommended := slices.Clone(fixture.Fit[0].Candidates)
	fixture.Fit[0].Candidates = nil
	fixture.Probes = []CapabilityProbeResult{{
		ExecutorID: inventory.ExecutorCursorAgent, Kind: CapabilityBrowserTooling, ID: "firefox",
		InventoryDigest: fixture.Inventory.Digest, Success: true,
	}}
	if _, err := fixture.Select(); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("Select(non-exact probe) error = %v, want ErrNoEligibleCandidate", err)
	}
	fixture.Probes[0].ID = "chromium"
	fixture.Probes[0].Success = false
	if _, err := fixture.Select(); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("Select(failed probe) error = %v, want ErrNoEligibleCandidate", err)
	}
	fixture.Probes[0].Success = true
	fixture.Fit[0].Candidates = recommended
	if _, err := fixture.Select(); err != nil {
		t.Fatalf("Select(exact successful probe) error = %v", err)
	}
}

func TestSelectorRejectsModelsBelowComplexityFloor(t *testing.T) {
	t.Parallel()

	fixture := selectionFixture()
	fixture.Graph.Tasks[0].Complexity = ComplexityHigh
	fixture.Fit[0].Complexity = ComplexityHigh
	fixture.Fit[0].Candidates = nil
	fixture.Policy.ModelTiers[ModelKey("cursor", "grok-4.6")] = ModelTierAdvanced
	if _, err := fixture.Select(); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("Select(model below floor) error = %v, want ErrNoEligibleCandidate", err)
	}
	delete(fixture.Policy.ModelTiers, ModelKey("cursor", "grok-4.6"))
	if _, err := fixture.Select(); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("Select(unknown high tier) error = %v, want ErrNoEligibleCandidate", err)
	}
}

func TestSelectorRejectsPairsMissingFromLiveCompozyCatalog(t *testing.T) {
	t.Parallel()

	fixture := selectionFixture()
	fixture.Catalog.Models = nil
	fixture.Fit[0].Candidates = nil
	if _, err := fixture.Select(); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("Select(missing pair) error = %v, want ErrNoEligibleCandidate", err)
	}
}

func TestSelectorKeepsProviderSpecificModelIDsVerbatim(t *testing.T) {
	t.Parallel()

	fixture := selectionFixture()
	fixture.Catalog.Models[0].ModelID = "xai/grok-4.6:fast"
	fixture.Bindings[0].ModelID = "xai/grok-4.6:fast"
	fixture.Inventory.Executors[0].Capabilities[0].Identifiers = []string{"xai/grok-4.6:fast"}
	fixture.Policy.ModelTiers = map[string]ModelTier{ModelKey("cursor", "xai/grok-4.6:fast"): ModelTierFrontier}
	fixture.Fit[0].Candidates[0].ModelID = "xai/grok-4.6:fast"
	generation, err := fixture.Select()
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got := generation.Rules[0].Runtime.Model; got != "xai/grok-4.6:fast" {
		t.Fatalf("model = %q, want exact provider-specific ID", got)
	}
}

func TestSelectorAcceptsCursorACPModelProvenByLiveCompozyCatalog(t *testing.T) {
	t.Parallel()

	fixture := selectionFixture()
	exactModel := "grok-4.6[effort=high,fast=true]"
	fixture.Catalog.Models[0].ModelID = exactModel
	fixture.Bindings[0].ModelID = exactModel
	fixture.Inventory.Executors[0].Capabilities[0].Identifiers = []string{"cursor/cursor-grok-4.6-high"}
	fixture.Policy = DefaultSelectionPolicy()
	fixture.Fit[0].Candidates[0].ModelID = exactModel
	generation, err := fixture.Select()
	if err != nil {
		t.Fatalf("Select(exact Cursor ACP model) error = %v", err)
	}
	if got := generation.Cells[0].Selected; got.ExecutorID != inventory.ExecutorCursorAgent || got.ProviderID != "cursor" || got.ModelID != exactModel {
		t.Fatalf("selected = %#v, want Cursor Agent with exact live ACP model", got)
	}
}

func TestSelectorAppliesClosedFallbackBudgets(t *testing.T) {
	t.Parallel()

	for complexity, want := range map[Complexity]int{
		ComplexityLow: 1, ComplexityMedium: 2, ComplexityHigh: 3, ComplexityCritical: 3,
	} {
		fixture := selectionFixtureWithCandidates(5)
		fixture.Graph.Tasks[0].Complexity = complexity
		fixture.Fit[0].Complexity = complexity
		generation, err := fixture.Select()
		if err != nil {
			t.Fatalf("Select(%s) error = %v", complexity, err)
		}
		if got := len(generation.Cells[0].Fallbacks); got != want {
			t.Fatalf("fallback count for %s = %d, want %d", complexity, got, want)
		}
		if generation.Cells[0].FallbackLimit != want || generation.DeliveryFallbackLimit != 3 {
			t.Fatalf("limits for %s = cell:%d delivery:%d", complexity, generation.Cells[0].FallbackLimit, generation.DeliveryFallbackLimit)
		}
		if generation.EnclosingBudget.IterationCap != 4 || generation.Cells[0].Policy.VerificationDepth == "" || generation.Cells[0].Policy.ReviewPosture == "" {
			t.Fatalf("complexity/loop policy missing for %s: %#v", complexity, generation)
		}
	}
}

func TestSelectorRanksFitHealthQualityPermissionsCostAndStableIDs(t *testing.T) {
	t.Parallel()

	fixture := selectionFixtureWithCandidates(3)
	fixture.Fit[0].Candidates[0].Score = 0.80
	fixture.Fit[0].Candidates[1].Score = 0.95
	fixture.Fit[0].Candidates[2].Score = 0.95
	fixture.Bindings[1].PermissionScore = 1
	fixture.Bindings[2].PermissionScore = 2
	generation, err := fixture.Select()
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got := generation.Cells[0].Selected.ModelID; got != "model-3" {
		t.Fatalf("selected model = %q, want model-3 by fit then permission", got)
	}
}

func TestSelectorRejectsLLMRecommendationOutsideEligibleSet(t *testing.T) {
	t.Parallel()

	fixture := selectionFixture()
	fixture.Fit[0].Candidates = append(fixture.Fit[0].Candidates, FitCandidate{
		ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "invented", Score: 1,
	})
	if _, err := fixture.Select(); !errors.Is(err, ErrSelectionRetryable) {
		t.Fatalf("Select(invented recommendation) error = %v, want ErrSelectionRetryable", err)
	}
}

func TestGenerationEmitsOneTypeComplexityRulePerPopulatedCell(t *testing.T) {
	t.Parallel()

	fixture := selectionFixture()
	fixture.Graph.Tasks = append(fixture.Graph.Tasks,
		ValidatedTask{ID: "task_02", Domain: DomainFrontend, Complexity: ComplexityLow, Confidence: 0.9},
		ValidatedTask{ID: "task_03", Domain: DomainBackend, Complexity: ComplexityHigh, Confidence: 0.9},
	)
	fixture.Fit = append(fixture.Fit,
		CellFitRecommendation{Domain: DomainBackend, Complexity: ComplexityHigh, Candidates: slices.Clone(fixture.Fit[0].Candidates)},
	)
	generation, err := fixture.Select()
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got := len(generation.Rules); got != 2 {
		t.Fatalf("rule count = %d, want 2 populated cells", got)
	}
	if generation.Rules[0].Match.Domain != DomainBackend || generation.Rules[0].Match.Complexity != ComplexityHigh || generation.Rules[1].Match.Domain != DomainFrontend {
		t.Fatalf("stable rule order/content = %#v", generation.Rules)
	}
}

func TestGenerationDigestIsStableAndSnapshotsInventory(t *testing.T) {
	t.Parallel()

	fixture := selectionFixtureWithCandidates(2)
	first, err := fixture.Select()
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	fixture.Bindings[0], fixture.Bindings[1] = fixture.Bindings[1], fixture.Bindings[0]
	second, err := fixture.Select()
	if err != nil {
		t.Fatalf("Select(reordered) error = %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("digest changed after input reorder: %q != %q", first.Digest, second.Digest)
	}
	if first.InventoryDigest != fixture.Inventory.Digest || first.TaskSetDigest != fixture.Graph.TaskSetDigest {
		t.Fatalf("generation did not snapshot inputs: %#v", first)
	}
}

func TestGenerationProvidesFloorPreservingFallbackOrder(t *testing.T) {
	t.Parallel()

	fixture := selectionFixtureWithCandidates(4)
	fixture.Graph.Tasks[0].Complexity = ComplexityHigh
	fixture.Fit[0].Complexity = ComplexityHigh
	fixture.Policy.ModelTiers[ModelKey("provider-4", "model-4")] = ModelTierAdvanced
	fixture.Fit[0].Candidates = slices.DeleteFunc(fixture.Fit[0].Candidates, func(candidate FitCandidate) bool {
		return candidate.ProviderID == "provider-4" && candidate.ModelID == "model-4"
	})
	generation, err := fixture.Select()
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	for _, fallback := range generation.Cells[0].Fallbacks {
		if fixture.Policy.ModelTiers[ModelKey(fallback.ProviderID, fallback.ModelID)] < ModelTierFrontier {
			t.Fatalf("fallback below high floor: %#v", fallback)
		}
	}
}

func TestGenerationBoundsSafeCandidateRejectionsPerCell(t *testing.T) {
	t.Parallel()

	fixture := selectionFixtureWithCandidates(40)
	fixture.Graph.Tasks[0].Complexity = ComplexityHigh
	fixture.Fit[0].Complexity = ComplexityHigh
	for key := range fixture.Policy.ModelTiers {
		fixture.Policy.ModelTiers[key] = ModelTierEconomy
	}
	selected := fixture.Bindings[0]
	fixture.Policy.ModelTiers[ModelKey(selected.ProviderID, selected.ModelID)] = ModelTierFrontier
	fixture.Fit[0].Candidates = []FitCandidate{{
		ExecutorID: selected.ExecutorID, ProviderID: selected.ProviderID, ModelID: selected.ModelID, Score: 1,
	}}
	generation, err := fixture.Select()
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got, want := len(generation.Rejections), 16; got != want {
		t.Fatalf("candidate rejections = %d, want bounded %d", got, want)
	}
}

func TestBuildCandidateBindingsUsesOnlyLiveCompozyCatalogPairs(t *testing.T) {
	t.Parallel()

	snapshot, err := inventory.NewSnapshot("catalog-generation", []inventory.ExecutorSnapshot{
		{ID: inventory.ExecutorCompozy, Availability: inventory.AvailabilityAvailable},
		{ID: inventory.ExecutorCursorAgent, Availability: inventory.AvailabilityAvailable, Capabilities: []inventory.Evidence{{Name: "models", State: inventory.ResolutionResolved, Identifiers: []string{"cursor/cursor-grok-4.6-high", "shared"}}}},
		{ID: inventory.ExecutorOpenCode, Availability: inventory.AvailabilityAvailable, Capabilities: []inventory.Evidence{{Name: "models", State: inventory.ResolutionResolved, Identifiers: []string{"openai/gpt-5.6-terra"}}}},
	})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	catalog := LiveCatalog{Generation: "catalog-generation", Models: []CatalogModel{
		{ProviderID: "cursor", ModelID: "grok-4.6[effort=high,fast=true]", Availability: inventory.AvailabilityAvailable},
		{ProviderID: "openai", ModelID: "gpt-5.6-terra", Availability: inventory.AvailabilityAvailable},
		{ProviderID: "first", ModelID: "shared", Availability: inventory.AvailabilityAvailable},
		{ProviderID: "second", ModelID: "shared", Availability: inventory.AvailabilityAvailable},
	}}
	bindings, err := BuildCandidateBindings(snapshot, catalog)
	if err != nil {
		t.Fatalf("BuildCandidateBindings() error = %v", err)
	}
	want := []CandidateBinding{
		{ExecutorID: inventory.ExecutorCompozy, ProviderID: "cursor", ModelID: "grok-4.6[effort=high,fast=true]", PermissionScore: 1},
		{ExecutorID: inventory.ExecutorCompozy, ProviderID: "first", ModelID: "shared", PermissionScore: 1},
		{ExecutorID: inventory.ExecutorCompozy, ProviderID: "openai", ModelID: "gpt-5.6-terra", PermissionScore: 1},
		{ExecutorID: inventory.ExecutorCompozy, ProviderID: "second", ModelID: "shared", PermissionScore: 1},
	}
	if !equalCandidateBindings(bindings, want) {
		t.Fatalf("bindings = %#v, want all and only live catalog pairs %#v", bindings, want)
	}
}

func TestBuildCandidateBindingsIncludesLivePairWithoutDedicatedAdapter(t *testing.T) {
	t.Parallel()

	snapshot, err := inventory.NewSnapshot("catalog-generation", []inventory.ExecutorSnapshot{{
		ID: inventory.ExecutorCompozy, Availability: inventory.AvailabilityAvailable,
	}})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	catalog := LiveCatalog{Generation: "catalog-generation", Models: []CatalogModel{
		{ProviderID: "claude", ModelID: "claude-fixture", Availability: inventory.AvailabilityAvailable, CredentialState: inventory.CredentialConfigured},
		{ProviderID: "gemini", ModelID: "gemini-fixture", Availability: inventory.AvailabilityAvailable, CredentialState: inventory.CredentialUnknown},
	}}
	got, err := BuildCandidateBindings(snapshot, catalog)
	if err != nil {
		t.Fatalf("BuildCandidateBindings() error = %v", err)
	}
	want := []CandidateBinding{
		{ExecutorID: inventory.ExecutorCompozy, ProviderID: "claude", ModelID: "claude-fixture", PermissionScore: 2},
		{ExecutorID: inventory.ExecutorCompozy, ProviderID: "gemini", ModelID: "gemini-fixture", PermissionScore: 1},
	}
	if !equalCandidateBindings(got, want) {
		t.Fatalf("bindings = %#v, want generic live pairs %#v", got, want)
	}
}

func equalCandidateBindings(left, right []CandidateBinding) bool {
	return slices.EqualFunc(left, right, func(a, b CandidateBinding) bool {
		return a.ExecutorID == b.ExecutorID && a.ProviderID == b.ProviderID && a.ModelID == b.ModelID &&
			a.PermissionScore == b.PermissionScore && a.CostScore == b.CostScore && slices.Equal(a.EnrichmentIDs, b.EnrichmentIDs)
	})
}

func TestBuildCandidateBindingsRejectsAdapterOnlyPairAbsentFromLiveCatalog(t *testing.T) {
	t.Parallel()

	snapshot, err := inventory.NewSnapshot("catalog-generation", []inventory.ExecutorSnapshot{
		{ID: inventory.ExecutorCompozy, Availability: inventory.AvailabilityAvailable},
		{ID: inventory.ExecutorOpenCode, Availability: inventory.AvailabilityAvailable, ProviderBindings: []inventory.ProviderBinding{{ProviderID: "invented", ModelID: "model"}}},
	})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	got, err := BuildCandidateBindings(snapshot, LiveCatalog{Generation: "catalog-generation"})
	if err != nil {
		t.Fatalf("BuildCandidateBindings() error = %v", err)
	}
	if len(got) != 0 {
		t.Fatalf("bindings = %#v, want adapter-only pair rejected", got)
	}
}

func TestBuildCandidateBindingsRejectsCatalogWithoutCompozyAuthority(t *testing.T) {
	t.Parallel()

	snapshot, err := inventory.NewSnapshot("catalog-generation", []inventory.ExecutorSnapshot{{
		ID: inventory.ExecutorCursorAgent, Availability: inventory.AvailabilityAvailable,
	}})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	_, err = BuildCandidateBindings(snapshot, LiveCatalog{Generation: "catalog-generation", Models: []CatalogModel{{
		ProviderID: "cursor", ModelID: "grok-4.6", Availability: inventory.AvailabilityAvailable,
	}}})
	if !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("BuildCandidateBindings(no Compozy authority) error = %v, want ErrNoEligibleCandidate", err)
	}
}

func TestBuildCandidateBindingsDeduplicatesExactRuntimeAcrossEnrichers(t *testing.T) {
	t.Parallel()

	snapshot, err := inventory.NewSnapshot("catalog-generation", []inventory.ExecutorSnapshot{
		{ID: inventory.ExecutorCompozy, Availability: inventory.AvailabilityAvailable},
		{ID: inventory.ExecutorCursorAgent, Availability: inventory.AvailabilityAvailable, ProviderBindings: []inventory.ProviderBinding{{ProviderID: "cursor"}}},
		{ID: inventory.ExecutorOpenCode, Availability: inventory.AvailabilityAvailable, ProviderBindings: []inventory.ProviderBinding{{ProviderID: "cursor", ModelID: "grok-4.6"}}},
	})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	got, err := BuildCandidateBindings(snapshot, LiveCatalog{Generation: "catalog-generation", Models: []CatalogModel{{
		ProviderID: "cursor", ModelID: "grok-4.6", Availability: inventory.AvailabilityAvailable, CredentialState: inventory.CredentialConfigured,
	}}})
	if err != nil {
		t.Fatalf("BuildCandidateBindings() error = %v", err)
	}
	if len(got) != 1 || got[0].ExecutorID != inventory.ExecutorCompozy || !slices.Equal(got[0].EnrichmentIDs, []inventory.ExecutorID{inventory.ExecutorCursorAgent, inventory.ExecutorOpenCode}) {
		t.Fatalf("bindings = %#v, want one Compozy-owned pair with ordered enrichers", got)
	}
}

func TestBuildCandidateBindingsRejectsHiddenDeprecatedAndUnavailableModels(t *testing.T) {
	t.Parallel()

	snapshot, err := inventory.NewSnapshot("catalog-generation", []inventory.ExecutorSnapshot{{ID: inventory.ExecutorCompozy, Availability: inventory.AvailabilityAvailable}})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	catalog := LiveCatalog{Generation: "catalog-generation", Models: []CatalogModel{
		{ProviderID: "ok", ModelID: "model", Availability: inventory.AvailabilityAvailable, CredentialState: inventory.CredentialConfigured},
		{ProviderID: "hidden", ModelID: "model", Availability: inventory.AvailabilityAvailable, Hidden: true},
		{ProviderID: "deprecated", ModelID: "model", Availability: inventory.AvailabilityAvailable, Deprecated: true},
		{ProviderID: "unavailable", ModelID: "model", Availability: inventory.AvailabilityMissing},
	}}
	got, err := BuildCandidateBindings(snapshot, catalog)
	if err != nil {
		t.Fatalf("BuildCandidateBindings() error = %v", err)
	}
	if len(got) != 1 || got[0].ProviderID != "ok" {
		t.Fatalf("bindings = %#v, want only visible live model", got)
	}
}

func TestBuildCandidateBindingsRejectsAuthoritativeMissingProviderAuth(t *testing.T) {
	t.Parallel()

	snapshot, err := inventory.NewSnapshot("catalog-generation", []inventory.ExecutorSnapshot{{ID: inventory.ExecutorCompozy, Availability: inventory.AvailabilityAvailable}})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	catalog := LiveCatalog{Generation: "catalog-generation", Models: []CatalogModel{
		{ProviderID: "ready", ModelID: "model", Availability: inventory.AvailabilityAvailable, CredentialState: inventory.CredentialConfigured},
		{ProviderID: "degraded", ModelID: "model", Availability: inventory.AvailabilityAvailable, CredentialState: inventory.CredentialUnknown},
		{ProviderID: "missing", ModelID: "model", Availability: inventory.AvailabilityAvailable, CredentialState: inventory.CredentialMissing},
	}}
	got, err := BuildCandidateBindings(snapshot, catalog)
	if err != nil {
		t.Fatalf("BuildCandidateBindings() error = %v", err)
	}
	if gotIDs := []string{got[0].ProviderID, got[1].ProviderID}; !slices.Equal(gotIDs, []string{"degraded", "ready"}) {
		t.Fatalf("providers = %#v, want degraded+ready and missing rejected", gotIDs)
	}
}

func TestFitUniverseUsesProviderAndModelInsteadOfAdapterIdentity(t *testing.T) {
	t.Parallel()

	bindings := []CandidateBinding{{ExecutorID: inventory.ExecutorCompozy, ProviderID: "claude", ModelID: "claude-fixture"}}
	legacyFit := []CellFitRecommendation{{Domain: DomainFrontend, Complexity: ComplexityLow, Candidates: []FitCandidate{{
		ExecutorID: inventory.ExecutorCodex, ProviderID: "claude", ModelID: "claude-fixture", Score: 0.9,
	}}}}
	if err := validateFitUniverse(legacyFit, bindings); err != nil {
		t.Fatalf("validateFitUniverse(legacy executor) error = %v", err)
	}
	legacyFit[0].Candidates = append(legacyFit[0].Candidates, FitCandidate{
		ExecutorID: inventory.ExecutorCompozy, ProviderID: "claude", ModelID: "claude-fixture", Score: 0.8,
	})
	if err := validateFitUniverse(legacyFit, bindings); !errors.Is(err, ErrSelectionRetryable) {
		t.Fatalf("validateFitUniverse(duplicate runtime pair) error = %v, want ErrSelectionRetryable", err)
	}
}

func TestNewRoutingGenerationWritesCompozyAsExecutionOwner(t *testing.T) {
	t.Parallel()

	fixture := selectionFixture()
	fixture.Bindings[0].ExecutorID = inventory.ExecutorCompozy
	fixture.Inventory.Executors[0].ID = inventory.ExecutorCompozy
	fixture.Fit[0].Candidates[0].ExecutorID = inventory.ExecutorCursorAgent
	generation, err := fixture.Select()
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	if got := generation.Cells[0].Selected.ExecutorID; got != inventory.ExecutorCompozy {
		t.Fatalf("selected execution owner = %q, want compozy", got)
	}
}

func TestSelectorRejectsRecommendationForKnownButIneligibleCandidate(t *testing.T) {
	t.Parallel()

	fixture := selectionFixture()
	fixture.Graph.Tasks[0].Requirements = []CapabilityRequirement{{Kind: CapabilityMCP, ID: "missing", Hard: true}}
	if _, err := fixture.Select(); !errors.Is(err, ErrSelectionRetryable) {
		t.Fatalf("Select(recommended ineligible binding) error = %v, want ErrSelectionRetryable", err)
	}
}

func TestDefaultSelectionPolicyClassifiesCursorGrok46AsFrontier(t *testing.T) {
	t.Parallel()

	policy := DefaultSelectionPolicy()
	if policy.Version == "" || policy.modelTier("cursor", "grok-4.6[effort=high,fast=true]") != ModelTierFrontier {
		t.Fatalf("DefaultSelectionPolicy() = %#v, want versioned exact Cursor/Grok 4.6 frontier entry", policy)
	}
}

type selectorFixture struct {
	Graph        ValidatedTaskGraph
	Inventory    inventory.InventorySnapshot
	Catalog      LiveCatalog
	Bindings     []CandidateBinding
	Fit          []CellFitRecommendation
	Probes       []CapabilityProbeResult
	Policy       SelectionPolicy
	WorkspaceKey string
	Budget       LoopBudgetCeiling
}

func (f selectorFixture) Select() (RoutingGeneration, error) {
	return NewSelector(f.Policy).Select(SelectionInput{
		Graph: f.Graph, Inventory: f.Inventory, Catalog: f.Catalog, Bindings: f.Bindings,
		Fit: f.Fit, Probes: f.Probes, WorkspaceIdentityDigest: f.WorkspaceKey,
		EnclosingBudget: f.Budget,
	})
}

func selectionFixture() selectorFixture { return selectionFixtureWithCandidates(1) }

func selectionFixtureWithCandidates(count int) selectorFixture {
	models := make([]CatalogModel, 0, count)
	bindings := make([]CandidateBinding, 0, count)
	fit := make([]FitCandidate, 0, count)
	tiers := make(map[string]ModelTier, count)
	modelIDs := make([]string, 0, count)
	for i := 1; i <= count; i++ {
		executorID := inventory.ExecutorCursorAgent
		provider, model := "cursor", "grok-4.6"
		if count > 1 {
			executorID = inventory.ExecutorCodex
			provider, model = "provider-"+string(rune('0'+i)), "model-"+string(rune('0'+i))
		}
		modelIDs = append(modelIDs, model)
		models = append(models, CatalogModel{ProviderID: provider, ModelID: model, Availability: inventory.AvailabilityAvailable})
		bindings = append(bindings, CandidateBinding{ExecutorID: executorID, ProviderID: provider, ModelID: model, PermissionScore: 1, CostScore: i})
		fit = append(fit, FitCandidate{ExecutorID: executorID, ProviderID: provider, ModelID: model, Score: 0.9})
		tiers[ModelKey(provider, model)] = ModelTierFrontier
	}
	executorID := inventory.ExecutorCursorAgent
	if count > 1 {
		executorID = inventory.ExecutorCodex
	}
	executors := []inventory.ExecutorSnapshot{{
		ID: executorID, Availability: inventory.AvailabilityAvailable,
		Health:       inventory.Evidence{Name: "health", State: inventory.ResolutionResolved},
		Capabilities: []inventory.Evidence{{Name: "models", State: inventory.ResolutionResolved, Identifiers: modelIDs}},
	}}
	snapshot, err := inventory.NewSnapshot("catalog-generation", executors)
	if err != nil {
		panic(err)
	}
	return selectorFixture{
		Graph: ValidatedTaskGraph{Slug: "demo", TaskSetDigest: "task-set-digest", Tasks: []ValidatedTask{{
			ID: "task_01", Domain: DomainFrontend, Complexity: ComplexityLow, Confidence: 0.9,
		}}},
		Inventory: snapshot,
		Catalog:   LiveCatalog{Generation: "catalog-generation", Models: models},
		Bindings:  bindings,
		Fit: []CellFitRecommendation{{
			Domain: DomainFrontend, Complexity: ComplexityLow, Candidates: fit,
		}},
		Policy:       SelectionPolicy{Version: "test-v1", ModelTiers: tiers},
		WorkspaceKey: "sha256:workspace",
		Budget:       LoopBudgetCeiling{IterationCap: 4, TokenBudget: 100_000, WallTimeSeconds: 3_600},
	}
}
