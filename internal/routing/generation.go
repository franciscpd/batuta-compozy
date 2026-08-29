package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

const (
	routingGenerationSchemaVersion = 1
	deliveryFallbackLimit          = 3
)

type RuntimeRule struct {
	Match   RuntimeMatch `json:"match"`
	Runtime RuntimeValue `json:"runtime"`
}

type RuntimeMatch struct {
	ID         string     `json:"id,omitempty"`
	Domain     Domain     `json:"type,omitempty"`
	Complexity Complexity `json:"complexity,omitempty"`
}

type RuntimeValue struct {
	Provider  string `json:"provider"`
	Model     string `json:"model"`
	Reasoning string `json:"reasoning"`
	Speed     string `json:"speed,omitempty"`
}

type RuntimeCandidate struct {
	ExecutorID    inventory.ExecutorID   `json:"executor_id"`
	ProviderID    string                 `json:"provider_id"`
	ModelID       string                 `json:"model_id"`
	EnrichmentIDs []inventory.ExecutorID `json:"enrichment_ids,omitempty"`
	Reasoning     string                 `json:"reasoning"`
	ModelTier     ModelTier              `json:"model_tier"`
}

type RoutingCell struct {
	Domain        Domain                   `json:"domain"`
	Complexity    Complexity               `json:"complexity"`
	TaskIDs       []string                 `json:"task_ids"`
	Selected      RuntimeCandidate         `json:"selected"`
	Fallbacks     []RuntimeCandidate       `json:"fallbacks"`
	FallbackLimit int                      `json:"fallback_limit"`
	Policy        ComplexityPolicySnapshot `json:"policy"`
}

type LoopBudgetCeiling struct {
	IterationCap    int   `json:"iteration_cap"`
	TokenBudget     int64 `json:"token_budget,omitempty"`
	WallTimeSeconds int64 `json:"wall_time_seconds,omitempty"`
}

type CandidateRejection struct {
	Domain        Domain                 `json:"domain"`
	Complexity    Complexity             `json:"complexity"`
	ExecutorID    inventory.ExecutorID   `json:"executor_id"`
	ProviderID    string                 `json:"provider_id"`
	ModelID       string                 `json:"model_id"`
	EnrichmentIDs []inventory.ExecutorID `json:"enrichment_ids,omitempty"`
	Code          string                 `json:"code"`
}

type GenerationTask struct {
	ID         string     `json:"task_id"`
	Domain     Domain     `json:"domain"`
	Complexity Complexity `json:"complexity"`
}

type RoutingGeneration struct {
	SchemaVersion           int                  `json:"schema_version"`
	PolicyVersion           string               `json:"policy_version"`
	WorkspaceIdentityDigest string               `json:"workspace_identity_digest"`
	TaskSetDigest           string               `json:"task_set_digest"`
	InventoryDigest         string               `json:"inventory_digest"`
	CatalogGeneration       string               `json:"catalog_generation"`
	Tasks                   []GenerationTask     `json:"tasks"`
	Rules                   []RuntimeRule        `json:"rules"`
	Cells                   []RoutingCell        `json:"cells"`
	Rejections              []CandidateRejection `json:"rejections,omitempty"`
	DeliveryFallbackLimit   int                  `json:"delivery_fallback_limit"`
	EnclosingBudget         LoopBudgetCeiling    `json:"enclosing_budget"`
	Digest                  string               `json:"digest"`
}

func finalizeGeneration(generation RoutingGeneration) (RoutingGeneration, error) {
	for cellIndex := range generation.Cells {
		cell := &generation.Cells[cellIndex]
		cell.Selected.EnrichmentIDs = canonicalEnrichmentIDs(cell.Selected.EnrichmentIDs)
		for fallbackIndex := range cell.Fallbacks {
			cell.Fallbacks[fallbackIndex].EnrichmentIDs = canonicalEnrichmentIDs(cell.Fallbacks[fallbackIndex].EnrichmentIDs)
		}
	}
	for rejectionIndex := range generation.Rejections {
		generation.Rejections[rejectionIndex].EnrichmentIDs = canonicalEnrichmentIDs(generation.Rejections[rejectionIndex].EnrichmentIDs)
	}
	generation.Digest = ""
	payload, err := json.Marshal(generation)
	if err != nil {
		return RoutingGeneration{}, fmt.Errorf("routing: encode generation: %w", err)
	}
	digest := sha256.Sum256(payload)
	generation.Digest = "sha256:" + hex.EncodeToString(digest[:])
	return generation, nil
}

func canonicalEnrichmentIDs(values []inventory.ExecutorID) []inventory.ExecutorID {
	values = slices.Clone(values)
	slices.Sort(values)
	return slices.Compact(values)
}
