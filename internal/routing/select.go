package routing

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

var (
	ErrNoEligibleCandidate = errors.New("routing: no eligible runtime candidate")
	ErrSelectionRetryable  = errors.New("routing: fit recommendation must be retried")
	ErrCatalogDrift        = errors.New("routing: live catalog changed during selection")
)

const maxSafeRejectionsPerCell = 16

// RecommendedCandidateError preserves the exact candidate rejected by the
// deterministic selector so callers can correct only that proposal.
type RecommendedCandidateError struct {
	Rejection CandidateRejection
}

func (e *RecommendedCandidateError) Error() string {
	if e == nil {
		return ""
	}
	return fmt.Sprintf("%s: recommendation includes %s candidate", ErrSelectionRetryable, e.Rejection.Code)
}

func (e *RecommendedCandidateError) Unwrap() error { return ErrSelectionRetryable }

type CatalogModel struct {
	ProviderID      string                      `json:"provider_id"`
	ModelID         string                      `json:"model_id"`
	Availability    inventory.AvailabilityState `json:"availability"`
	Hidden          bool                        `json:"hidden,omitempty"`
	Deprecated      bool                        `json:"deprecated,omitempty"`
	CredentialState inventory.CredentialState   `json:"credential_state,omitempty"`
	Cost            *inventory.ModelCost        `json:"cost,omitempty"`
}

type LiveCatalog struct {
	Generation string         `json:"generation"`
	Models     []CatalogModel `json:"models"`
}

type CandidateBinding struct {
	ExecutorID      inventory.ExecutorID   `json:"executor_id"`
	ProviderID      string                 `json:"provider_id"`
	ModelID         string                 `json:"model_id"`
	PermissionScore int                    `json:"permission_score"`
	CostScore       int                    `json:"cost_score"`
	Cost            *inventory.ModelCost   `json:"cost,omitempty"`
	EnrichmentIDs   []inventory.ExecutorID `json:"enrichment_ids,omitempty"`
}

// unknownCostScore ranks unpriced pairs after every priced pair at the
// cost tie-break, without letting missing data beat a known cheap rate.
const unknownCostScore = math.MaxInt32

func costScore(cost *inventory.ModelCost) int {
	if cost == nil || *cost == (inventory.ModelCost{}) {
		return unknownCostScore
	}
	return int(math.Round((cost.InputPerMillion + cost.OutputPerMillion) * 100))
}

type CapabilityProbeResult struct {
	ExecutorID      inventory.ExecutorID `json:"executor_id"`
	Kind            CapabilityKind       `json:"kind"`
	ID              string               `json:"id"`
	InventoryDigest string               `json:"inventory_digest"`
	Success         bool                 `json:"success"`
}

type FitCandidate struct {
	ExecutorID inventory.ExecutorID `json:"executor_id"`
	ProviderID string               `json:"provider_id"`
	ModelID    string               `json:"model_id"`
	Score      float64              `json:"score"`
	Reasoning  string               `json:"reasoning,omitempty"`
}

type CellFitRecommendation struct {
	Domain     Domain         `json:"domain"`
	Complexity Complexity     `json:"complexity"`
	Candidates []FitCandidate `json:"candidates"`
}

type SelectionInput struct {
	Graph                   ValidatedTaskGraph
	Inventory               inventory.InventorySnapshot
	Catalog                 LiveCatalog
	Bindings                []CandidateBinding
	Fit                     []CellFitRecommendation
	Probes                  []CapabilityProbeResult
	WorkspaceIdentityDigest string
	EnclosingBudget         LoopBudgetCeiling
}

type Selector struct {
	policy SelectionPolicy
}

func NewSelector(policy SelectionPolicy) *Selector { return &Selector{policy: policy} }

func BuildCandidateBindings(snapshot inventory.InventorySnapshot, catalog LiveCatalog) ([]CandidateBinding, error) {
	if err := snapshot.Validate(); err != nil || snapshot.Digest == "" {
		return nil, errors.New("routing: inventory snapshot is invalid")
	}
	if catalog.Generation == "" || catalog.Generation != snapshot.CompozyCatalogGeneration {
		return nil, ErrCatalogDrift
	}
	executors := indexExecutors(snapshot.Executors)
	compozy, exists := executors[inventory.ExecutorCompozy]
	if !exists || compozy.Availability != inventory.AvailabilityAvailable {
		return nil, ErrNoEligibleCandidate
	}
	bindings := make([]CandidateBinding, 0, len(catalog.Models))
	for _, model := range catalog.Models {
		if model.ProviderID == "" || model.ModelID == "" || model.Hidden || model.Deprecated || model.CredentialState == inventory.CredentialMissing ||
			!catalogModelExecutable(model, executors) {
			continue
		}
		permissionScore := 1
		if model.CredentialState == inventory.CredentialConfigured {
			permissionScore = 2
		}
		enrichments := make([]inventory.ExecutorID, 0)
		for _, executor := range executors {
			if executor.ID == inventory.ExecutorCompozy || executor.Availability != inventory.AvailabilityAvailable {
				continue
			}
			if executorMatchesProviderBinding(executor, model.ProviderID, model.ModelID) {
				enrichments = append(enrichments, executor.ID)
			}
		}
		slices.Sort(enrichments)
		bindings = append(bindings, CandidateBinding{
			ExecutorID: inventory.ExecutorCompozy, ProviderID: model.ProviderID, ModelID: model.ModelID,
			PermissionScore: permissionScore, CostScore: costScore(model.Cost), Cost: model.Cost,
			EnrichmentIDs: slices.Compact(enrichments),
		})
	}
	bindings = canonicalBindings(bindings)
	return bindings, nil
}

func executorMatchesProviderBinding(executor inventory.ExecutorSnapshot, providerID, modelID string) bool {
	for _, binding := range executor.ProviderBindings {
		if binding.ProviderID == providerID && (binding.ModelID == "" || binding.ModelID == modelID) {
			return true
		}
	}
	return false
}

func catalogModelExecutable(model CatalogModel, executors map[inventory.ExecutorID]inventory.ExecutorSnapshot) bool {
	if model.Availability == inventory.AvailabilityAvailable {
		return true
	}
	if model.Availability != inventory.AvailabilityUnknown {
		return false
	}
	for _, executor := range executors {
		if executor.ID == inventory.ExecutorCompozy || executor.Availability != inventory.AvailabilityAvailable || executor.CredentialState == inventory.CredentialMissing {
			continue
		}
		for _, binding := range executor.ProviderBindings {
			if binding.ProviderID == model.ProviderID && binding.ModelID == model.ModelID {
				return true
			}
		}
	}
	return false
}

type cellKey struct {
	domain     Domain
	complexity Complexity
}

type rankedCandidate struct {
	binding   CandidateBinding
	fit       float64
	health    int
	tier      ModelTier
	reasoning string
}

func (s *Selector) Select(input SelectionInput) (RoutingGeneration, error) {
	if strings.TrimSpace(s.policy.Version) == "" {
		return RoutingGeneration{}, errors.New("routing: selection policy is incomplete")
	}
	if err := input.Inventory.Validate(); err != nil || input.Inventory.Digest == "" {
		return RoutingGeneration{}, errors.New("routing: inventory snapshot is invalid")
	}
	if input.Catalog.Generation == "" || input.Catalog.Generation != input.Inventory.CompozyCatalogGeneration {
		return RoutingGeneration{}, ErrCatalogDrift
	}
	if strings.TrimSpace(input.WorkspaceIdentityDigest) == "" || input.Graph.TaskSetDigest == "" {
		return RoutingGeneration{}, errors.New("routing: immutable selection identity is incomplete")
	}

	executors := indexExecutors(input.Inventory.Executors)
	catalog := indexCatalog(input.Catalog.Models)
	bindings := canonicalBindings(input.Bindings)
	if err := validateFitUniverse(input.Fit, bindings); err != nil {
		return RoutingGeneration{}, err
	}
	fit := indexFit(input.Fit)
	cells := groupTasks(input.Graph.Tasks)
	keys := sortedCellKeys(cells)

	generation := RoutingGeneration{
		SchemaVersion: routingGenerationSchemaVersion, PolicyVersion: s.policy.Version,
		WorkspaceIdentityDigest: input.WorkspaceIdentityDigest, TaskSetDigest: input.Graph.TaskSetDigest,
		InventoryDigest: input.Inventory.Digest, CatalogGeneration: input.Catalog.Generation,
		DeliveryFallbackLimit: deliveryFallbackLimit,
		EnclosingBudget:       input.EnclosingBudget,
	}
	for _, task := range input.Graph.Tasks {
		generation.Tasks = append(generation.Tasks, GenerationTask{ID: task.ID, Domain: task.Domain, Complexity: task.Complexity})
	}
	slices.SortFunc(generation.Tasks, func(a, b GenerationTask) int { return strings.Compare(a.ID, b.ID) })

	for _, key := range keys {
		tasks := cells[key]
		cellFit, exists := fit[key]
		if !exists {
			return RoutingGeneration{}, fmt.Errorf("%w: recommendation missing populated cell", ErrSelectionRetryable)
		}
		fitScores := make(map[string]FitCandidate, len(cellFit.Candidates))
		for _, candidate := range cellFit.Candidates {
			fitScores[bindingKey(candidate.ExecutorID, candidate.ProviderID, candidate.ModelID)] = candidate
		}
		eligible := make([]rankedCandidate, 0, len(bindings))
		recordedRejections := 0
		for _, binding := range bindings {
			reason := candidateRejection(binding, key, tasks, executors, catalog, input.Probes, input.Inventory.Digest, s.policy)
			if reason != "" {
				if _, recommended := fitScores[bindingKey(binding.ExecutorID, binding.ProviderID, binding.ModelID)]; recommended {
					return RoutingGeneration{}, &RecommendedCandidateError{Rejection: CandidateRejection{
						Domain: key.domain, Complexity: key.complexity, ExecutorID: binding.ExecutorID,
						ProviderID: binding.ProviderID, ModelID: binding.ModelID,
						EnrichmentIDs: slices.Clone(binding.EnrichmentIDs), Code: reason,
					}}
				}
				if recordedRejections < maxSafeRejectionsPerCell {
					generation.Rejections = append(generation.Rejections, CandidateRejection{
						Domain: key.domain, Complexity: key.complexity, ExecutorID: binding.ExecutorID,
						ProviderID: binding.ProviderID, ModelID: binding.ModelID, EnrichmentIDs: slices.Clone(binding.EnrichmentIDs), Code: reason,
					})
					recordedRejections++
				}
				continue
			}
			fitCandidate := fitScores[bindingKey(binding.ExecutorID, binding.ProviderID, binding.ModelID)]
			eligible = append(eligible, rankedCandidate{
				binding: binding, fit: fitCandidate.Score, health: candidateHealthScore(binding, executors),
				tier: s.policy.modelTier(binding.ProviderID, binding.ModelID), reasoning: fitCandidate.Reasoning,
			})
		}
		if len(eligible) == 0 {
			return RoutingGeneration{}, fmt.Errorf("%w: %s/%s", ErrNoEligibleCandidate, key.domain, key.complexity)
		}
		sortRankedCandidates(eligible)
		reasoning := reasoningFor(key.complexity)
		selected := runtimeCandidate(eligible[0], reasoning)
		limit := fallbackLimit(key.complexity)
		fallbacks := make([]RuntimeCandidate, 0, min(limit, len(eligible)-1))
		seenRuntimes := map[string]struct{}{selected.ProviderID + "\x00" + selected.ModelID: {}}
		for i := 1; i < len(eligible) && len(fallbacks) < limit; i++ {
			candidate := runtimeCandidate(eligible[i], reasoning)
			key := candidate.ProviderID + "\x00" + candidate.ModelID
			if _, duplicate := seenRuntimes[key]; duplicate {
				continue
			}
			seenRuntimes[key] = struct{}{}
			fallbacks = append(fallbacks, candidate)
		}
		taskIDs := make([]string, 0, len(tasks))
		for _, task := range tasks {
			taskIDs = append(taskIDs, task.ID)
		}
		slices.Sort(taskIDs)
		generation.Cells = append(generation.Cells, RoutingCell{
			Domain: key.domain, Complexity: key.complexity, TaskIDs: taskIDs,
			Selected: selected, Fallbacks: fallbacks, FallbackLimit: limit, Policy: complexityPolicy(key.complexity),
		})
		generation.Rules = append(generation.Rules, RuntimeRule{
			Match:   RuntimeMatch{Domain: key.domain, Complexity: key.complexity},
			Runtime: RuntimeValue{Provider: selected.ProviderID, Model: selected.ModelID, Reasoning: selected.Reasoning},
		})
	}
	slices.SortFunc(generation.Rejections, compareRejections)
	return finalizeGeneration(generation)
}

func candidateRejection(binding CandidateBinding, key cellKey, tasks []ValidatedTask, executors map[inventory.ExecutorID]inventory.ExecutorSnapshot, catalog map[string]CatalogModel, probes []CapabilityProbeResult, inventoryDigest string, policy SelectionPolicy) string {
	executor, exists := executors[binding.ExecutorID]
	if !exists || executor.Availability != inventory.AvailabilityAvailable {
		return "executor_unavailable"
	}
	if executor.CredentialState == inventory.CredentialMissing {
		return "credential_missing"
	}
	model, exists := catalog[ModelKey(binding.ProviderID, binding.ModelID)]
	if !exists || !catalogModelExecutable(model, executors) || model.Hidden || model.Deprecated {
		return "catalog_pair_unavailable"
	}
	if model.CredentialState == inventory.CredentialMissing {
		return "credential_missing"
	}
	if binding.ExecutorID != inventory.ExecutorCompozy && !executorHasModel(executor, binding.ProviderID, binding.ModelID) && !catalogModelOwnedByExecutor(binding.ExecutorID, binding.ProviderID) {
		return "executor_model_unproven"
	}
	if tier := policy.modelTier(binding.ProviderID, binding.ModelID); tier != ModelTierUnknown && tier < modelFloor(key.complexity) {
		return "model_below_floor"
	}
	for _, task := range tasks {
		for _, requirement := range task.Requirements {
			if !requirement.Hard {
				continue
			}
			if candidateResolvedCapability(binding, executors, requirement) {
				continue
			}
			if !requirement.SecuritySensitive && successfulExactProbe(probes, inventoryDigest, binding, requirement) {
				continue
			}
			return "hard_capability_unresolved"
		}
	}
	return ""
}

func candidateHealthScore(binding CandidateBinding, executors map[inventory.ExecutorID]inventory.ExecutorSnapshot) int {
	score := 0
	for _, executorID := range candidateEvidenceExecutors(binding) {
		if executor, ok := executors[executorID]; ok && executor.Health.State == inventory.ResolutionResolved {
			score++
		}
	}
	return score
}

func candidateResolvedCapability(binding CandidateBinding, executors map[inventory.ExecutorID]inventory.ExecutorSnapshot, requirement CapabilityRequirement) bool {
	for _, executorID := range candidateEvidenceExecutors(binding) {
		if executor, ok := executors[executorID]; ok && resolvedCapability(executor, requirement) {
			return true
		}
	}
	return false
}

func candidateEvidenceExecutors(binding CandidateBinding) []inventory.ExecutorID {
	ids := make([]inventory.ExecutorID, 0, len(binding.EnrichmentIDs)+1)
	ids = append(ids, binding.ExecutorID)
	ids = append(ids, binding.EnrichmentIDs...)
	slices.Sort(ids)
	return slices.Compact(ids)
}

func catalogModelOwnedByExecutor(executorID inventory.ExecutorID, providerID string) bool {
	return executorID == inventory.ExecutorCursorAgent && providerID == "cursor"
}

func resolvedCapability(executor inventory.ExecutorSnapshot, requirement CapabilityRequirement) bool {
	if requirement.Kind == CapabilityRepositoryInstruction {
		for _, evidence := range executor.InstructionDigests {
			if evidence.State == inventory.ResolutionResolved && evidenceMatchesID(evidence, requirement.ID, "") {
				return true
			}
		}
	}
	for _, evidence := range executor.Capabilities {
		if evidence.State != inventory.ResolutionResolved || evidence.Name != string(requirement.Kind) {
			continue
		}
		if evidenceMatchesID(evidence, requirement.ID, string(requirement.Kind)+":") {
			return true
		}
	}
	return false
}

func evidenceMatchesID(evidence inventory.Evidence, id, prefix string) bool {
	for _, identifier := range evidence.Identifiers {
		if identifier == id || prefix != "" && identifier == prefix+id {
			return true
		}
	}
	return false
}

func successfulExactProbe(probes []CapabilityProbeResult, digest string, binding CandidateBinding, requirement CapabilityRequirement) bool {
	owners := candidateEvidenceExecutors(binding)
	for _, probe := range probes {
		if probe.Success && probe.InventoryDigest == digest && slices.Contains(owners, probe.ExecutorID) && probe.Kind == requirement.Kind && probe.ID == requirement.ID {
			return true
		}
	}
	return false
}

func executorHasModel(executor inventory.ExecutorSnapshot, providerID, modelID string) bool {
	for _, evidence := range executor.Capabilities {
		if evidence.Name != "models" || evidence.State != inventory.ResolutionResolved {
			continue
		}
		for _, identifier := range evidence.Identifiers {
			if identifier == modelID || identifier == providerID+"/"+modelID {
				return true
			}
		}
	}
	return false
}

func indexExecutors(executors []inventory.ExecutorSnapshot) map[inventory.ExecutorID]inventory.ExecutorSnapshot {
	indexed := make(map[inventory.ExecutorID]inventory.ExecutorSnapshot, len(executors))
	for _, executor := range executors {
		indexed[executor.ID] = executor
	}
	return indexed
}

func indexCatalog(models []CatalogModel) map[string]CatalogModel {
	indexed := make(map[string]CatalogModel, len(models))
	for _, model := range models {
		indexed[ModelKey(model.ProviderID, model.ModelID)] = model
	}
	return indexed
}

func canonicalBindings(bindings []CandidateBinding) []CandidateBinding {
	result := slices.Clone(bindings)
	for i := range result {
		result[i].EnrichmentIDs = slices.Clone(result[i].EnrichmentIDs)
		slices.Sort(result[i].EnrichmentIDs)
		result[i].EnrichmentIDs = slices.Compact(result[i].EnrichmentIDs)
	}
	slices.SortFunc(result, func(a, b CandidateBinding) int {
		return strings.Compare(bindingKey(a.ExecutorID, a.ProviderID, a.ModelID), bindingKey(b.ExecutorID, b.ProviderID, b.ModelID))
	})
	return slices.CompactFunc(result, func(a, b CandidateBinding) bool {
		return bindingKey(a.ExecutorID, a.ProviderID, a.ModelID) == bindingKey(b.ExecutorID, b.ProviderID, b.ModelID)
	})
}

func validateFitUniverse(recommendations []CellFitRecommendation, bindings []CandidateBinding) error {
	universe := make(map[string]struct{}, len(bindings))
	for _, binding := range bindings {
		universe[bindingKey(binding.ExecutorID, binding.ProviderID, binding.ModelID)] = struct{}{}
	}
	seenCells := make(map[cellKey]struct{}, len(recommendations))
	for _, recommendation := range recommendations {
		key := cellKey{domain: recommendation.Domain, complexity: recommendation.Complexity}
		if !key.domain.Valid() || !key.complexity.Valid() {
			return fmt.Errorf("%w: recommendation taxonomy is invalid", ErrSelectionRetryable)
		}
		if _, duplicate := seenCells[key]; duplicate {
			return fmt.Errorf("%w: duplicate cell recommendation", ErrSelectionRetryable)
		}
		seenCells[key] = struct{}{}
		seenCandidates := make(map[string]struct{}, len(recommendation.Candidates))
		for _, candidate := range recommendation.Candidates {
			candidateKey := bindingKey(candidate.ExecutorID, candidate.ProviderID, candidate.ModelID)
			if _, exists := universe[candidateKey]; !exists || math.IsNaN(candidate.Score) || math.IsInf(candidate.Score, 0) || candidate.Score < 0 || candidate.Score > 1 {
				return fmt.Errorf("%w: recommendation references an unknown candidate", ErrSelectionRetryable)
			}
			if candidate.Reasoning != "" && candidate.Reasoning != "low" && candidate.Reasoning != "medium" && candidate.Reasoning != "high" && candidate.Reasoning != "xhigh" {
				return fmt.Errorf("%w: recommendation reasoning is invalid", ErrSelectionRetryable)
			}
			if _, duplicate := seenCandidates[candidateKey]; duplicate {
				return fmt.Errorf("%w: duplicate candidate recommendation", ErrSelectionRetryable)
			}
			seenCandidates[candidateKey] = struct{}{}
		}
	}
	return nil
}

func indexFit(recommendations []CellFitRecommendation) map[cellKey]CellFitRecommendation {
	indexed := make(map[cellKey]CellFitRecommendation, len(recommendations))
	for _, recommendation := range recommendations {
		indexed[cellKey{domain: recommendation.Domain, complexity: recommendation.Complexity}] = recommendation
	}
	return indexed
}

func groupTasks(tasks []ValidatedTask) map[cellKey][]ValidatedTask {
	grouped := make(map[cellKey][]ValidatedTask)
	for _, task := range tasks {
		key := cellKey{domain: task.Domain, complexity: task.Complexity}
		grouped[key] = append(grouped[key], task)
	}
	return grouped
}

func sortedCellKeys(cells map[cellKey][]ValidatedTask) []cellKey {
	keys := make([]cellKey, 0, len(cells))
	for key := range cells {
		keys = append(keys, key)
	}
	slices.SortFunc(keys, func(a, b cellKey) int {
		if value := strings.Compare(string(a.domain), string(b.domain)); value != 0 {
			return value
		}
		return strings.Compare(string(a.complexity), string(b.complexity))
	})
	return keys
}

func sortRankedCandidates(candidates []rankedCandidate) {
	slices.SortFunc(candidates, func(a, b rankedCandidate) int {
		if a.fit != b.fit {
			if a.fit > b.fit {
				return -1
			}
			return 1
		}
		if a.health != b.health {
			return b.health - a.health
		}
		if a.tier != b.tier {
			return int(b.tier - a.tier)
		}
		if a.binding.PermissionScore != b.binding.PermissionScore {
			return b.binding.PermissionScore - a.binding.PermissionScore
		}
		if a.binding.CostScore != b.binding.CostScore {
			return a.binding.CostScore - b.binding.CostScore
		}
		return strings.Compare(bindingKey(a.binding.ExecutorID, a.binding.ProviderID, a.binding.ModelID), bindingKey(b.binding.ExecutorID, b.binding.ProviderID, b.binding.ModelID))
	})
}

func runtimeCandidate(candidate rankedCandidate, reasoning string) RuntimeCandidate {
	if candidate.reasoning != "" {
		reasoning = candidate.reasoning
	}
	return RuntimeCandidate{
		ExecutorID: candidate.binding.ExecutorID, ProviderID: candidate.binding.ProviderID, ModelID: candidate.binding.ModelID,
		EnrichmentIDs: slices.Clone(candidate.binding.EnrichmentIDs), Reasoning: reasoning, ModelTier: candidate.tier,
		Cost: candidate.binding.Cost,
	}
}

func bindingKey(executorID inventory.ExecutorID, providerID, modelID string) string {
	return providerID + "\x00" + modelID
}

func compareRejections(a, b CandidateRejection) int {
	left := string(a.Domain) + "\x00" + string(a.Complexity) + "\x00" + bindingKey(a.ExecutorID, a.ProviderID, a.ModelID) + "\x00" + a.Code
	right := string(b.Domain) + "\x00" + string(b.Complexity) + "\x00" + bindingKey(b.ExecutorID, b.ProviderID, b.ModelID) + "\x00" + b.Code
	return strings.Compare(left, right)
}
