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

func TestBuildCandidateBindingsUsesOnlyUnambiguousInventoryCatalogPairs(t *testing.T) {
	t.Parallel()

	snapshot, err := inventory.NewSnapshot("catalog-generation", []inventory.ExecutorSnapshot{
		{ID: inventory.ExecutorCursorAgent, Availability: inventory.AvailabilityAvailable, Capabilities: []inventory.Evidence{{Name: "models", State: inventory.ResolutionResolved, Identifiers: []string{"grok-4.6", "shared"}}}},
		{ID: inventory.ExecutorOpenCode, Availability: inventory.AvailabilityAvailable, Capabilities: []inventory.Evidence{{Name: "models", State: inventory.ResolutionResolved, Identifiers: []string{"openai/gpt-5.6-terra"}}}},
	})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	catalog := LiveCatalog{Generation: "catalog-generation", Models: []CatalogModel{
		{ProviderID: "cursor", ModelID: "grok-4.6", Availability: inventory.AvailabilityAvailable},
		{ProviderID: "openai", ModelID: "gpt-5.6-terra", Availability: inventory.AvailabilityAvailable},
		{ProviderID: "first", ModelID: "shared", Availability: inventory.AvailabilityAvailable},
		{ProviderID: "second", ModelID: "shared", Availability: inventory.AvailabilityAvailable},
	}}
	bindings, err := BuildCandidateBindings(snapshot, catalog)
	if err != nil {
		t.Fatalf("BuildCandidateBindings() error = %v", err)
	}
	want := []CandidateBinding{
		{ExecutorID: inventory.ExecutorCursorAgent, ProviderID: "cursor", ModelID: "grok-4.6", PermissionScore: 1},
		{ExecutorID: inventory.ExecutorOpenCode, ProviderID: "openai", ModelID: "gpt-5.6-terra", PermissionScore: 1},
	}
	if !slices.Equal(bindings, want) {
		t.Fatalf("bindings = %#v, want %#v (ambiguous shared model excluded)", bindings, want)
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
	if policy.Version == "" || policy.modelTier("cursor", "grok-4.6") != ModelTierFrontier {
		t.Fatalf("DefaultSelectionPolicy() = %#v, want versioned cursor/grok-4.6 frontier entry", policy)
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
