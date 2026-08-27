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

type CatalogModel struct {
	ProviderID   string                      `json:"provider_id"`
	ModelID      string                      `json:"model_id"`
	Availability inventory.AvailabilityState `json:"availability"`
	Hidden       bool                        `json:"hidden,omitempty"`
	Deprecated   bool                        `json:"deprecated,omitempty"`
}

type LiveCatalog struct {
	Generation string         `json:"generation"`
	Models     []CatalogModel `json:"models"`
}

type CandidateBinding struct {
	ExecutorID      inventory.ExecutorID `json:"executor_id"`
	ProviderID      string               `json:"provider_id"`
	ModelID         string               `json:"model_id"`
	PermissionScore int                  `json:"permission_score"`
	CostScore       int                  `json:"cost_score"`
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
	modelsByID := make(map[string][]CatalogModel)
	modelsByPair := make(map[string]CatalogModel)
	for _, model := range catalog.Models {
		if model.ProviderID == "" || model.ModelID == "" || model.Availability != inventory.AvailabilityAvailable || model.Hidden || model.Deprecated {
			continue
		}
		modelsByID[model.ModelID] = append(modelsByID[model.ModelID], model)
		modelsByPair[model.ProviderID+"/"+model.ModelID] = model
	}

	bindings := make([]CandidateBinding, 0)
	for _, executor := range snapshot.Executors {
		if executor.Availability != inventory.AvailabilityAvailable {
			continue
		}
		permissionScore := 1
		if executor.CredentialState == inventory.CredentialConfigured {
			permissionScore = 2
		}
		if executor.ID == inventory.ExecutorCursorAgent {
			for _, model := range modelsByPair {
				if model.ProviderID != "cursor" {
					continue
				}
				bindings = append(bindings, CandidateBinding{
					ExecutorID: executor.ID, ProviderID: model.ProviderID, ModelID: model.ModelID,
					PermissionScore: permissionScore,
				})
			}
		}
		for _, evidence := range executor.Capabilities {
			if evidence.Name != "models" || evidence.State != inventory.ResolutionResolved {
				continue
			}
			for _, identifier := range evidence.Identifiers {
				model, ok := modelsByPair[identifier]
				if !ok {
					matches := modelsByID[identifier]
					if len(matches) != 1 {
						continue
					}
					model = matches[0]
				}
				bindings = append(bindings, CandidateBinding{
					ExecutorID: executor.ID, ProviderID: model.ProviderID, ModelID: model.ModelID,
					PermissionScore: permissionScore,
				})
			}
		}
	}
	bindings = canonicalBindings(bindings)
	return bindings, nil
}

type cellKey struct {
	domain     Domain
	complexity Complexity
}

type rankedCandidate struct {
	binding CandidateBinding
	fit     float64
	health  int
	tier    ModelTier
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
		fitScores := make(map[string]float64, len(cellFit.Candidates))
		for _, candidate := range cellFit.Candidates {
			fitScores[bindingKey(candidate.ExecutorID, candidate.ProviderID, candidate.ModelID)] = candidate.Score
		}
		eligible := make([]rankedCandidate, 0, len(bindings))
		recordedRejections := 0
		for _, binding := range bindings {
			reason := candidateRejection(binding, key, tasks, executors, catalog, input.Probes, input.Inventory.Digest, s.policy)
			if reason != "" {
				if _, recommended := fitScores[bindingKey(binding.ExecutorID, binding.ProviderID, binding.ModelID)]; recommended {
					return RoutingGeneration{}, fmt.Errorf("%w: recommendation includes %s candidate", ErrSelectionRetryable, reason)
				}
				if recordedRejections < maxSafeRejectionsPerCell {
					generation.Rejections = append(generation.Rejections, CandidateRejection{
						Domain: key.domain, Complexity: key.complexity, ExecutorID: binding.ExecutorID,
						ProviderID: binding.ProviderID, ModelID: binding.ModelID, Code: reason,
					})
					recordedRejections++
				}
				continue
			}
			executor := executors[binding.ExecutorID]
			health := 0
			if executor.Health.State == inventory.ResolutionResolved {
				health = 1
			}
			eligible = append(eligible, rankedCandidate{
				binding: binding, fit: fitScores[bindingKey(binding.ExecutorID, binding.ProviderID, binding.ModelID)],
				health: health, tier: s.policy.modelTier(binding.ProviderID, binding.ModelID),
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
			Runtime: RuntimeValue{Provider: selected.ProviderID, Model: selected.ModelID, Reasoning: reasoning},
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
	if !exists || model.Availability != inventory.AvailabilityAvailable || model.Hidden || model.Deprecated {
		return "catalog_pair_unavailable"
	}
	if !executorHasModel(executor, binding.ProviderID, binding.ModelID) && !catalogModelOwnedByExecutor(binding.ExecutorID, binding.ProviderID) {
		return "executor_model_unproven"
	}
	if policy.modelTier(binding.ProviderID, binding.ModelID) < modelFloor(key.complexity) {
		return "model_below_floor"
	}
	for _, task := range tasks {
		for _, requirement := range task.Requirements {
			if !requirement.Hard {
				continue
			}
			if resolvedCapability(executor, requirement) {
				continue
			}
			if !requirement.SecuritySensitive && successfulExactProbe(probes, inventoryDigest, binding.ExecutorID, requirement) {
				continue
			}
			return "hard_capability_unresolved"
		}
	}
	return ""
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

func successfulExactProbe(probes []CapabilityProbeResult, digest string, executorID inventory.ExecutorID, requirement CapabilityRequirement) bool {
	for _, probe := range probes {
		if probe.Success && probe.InventoryDigest == digest && probe.ExecutorID == executorID && probe.Kind == requirement.Kind && probe.ID == requirement.ID {
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
	return RuntimeCandidate{ExecutorID: candidate.binding.ExecutorID, ProviderID: candidate.binding.ProviderID, ModelID: candidate.binding.ModelID, Reasoning: reasoning, ModelTier: candidate.tier}
}

func bindingKey(executorID inventory.ExecutorID, providerID, modelID string) string {
	return string(executorID) + "\x00" + providerID + "\x00" + modelID
}

func compareRejections(a, b CandidateRejection) int {
	left := string(a.Domain) + "\x00" + string(a.Complexity) + "\x00" + bindingKey(a.ExecutorID, a.ProviderID, a.ModelID) + "\x00" + a.Code
	right := string(b.Domain) + "\x00" + string(b.Complexity) + "\x00" + bindingKey(b.ExecutorID, b.ProviderID, b.ModelID) + "\x00" + b.Code
	return strings.Compare(left, right)
}
