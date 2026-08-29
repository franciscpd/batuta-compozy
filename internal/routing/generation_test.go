package routing

import (
	"bytes"
	"encoding/json"
	"slices"
	"testing"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

func TestExistingRoutingGenerationWithLegacyExecutorRemainsValid(t *testing.T) {
	t.Parallel()

	legacy := RoutingGeneration{
		SchemaVersion:           routingGenerationSchemaVersion,
		PolicyVersion:           "policy-v1",
		WorkspaceIdentityDigest: "sha256:workspace",
		TaskSetDigest:           "sha256:tasks",
		InventoryDigest:         "sha256:inventory",
		CatalogGeneration:       "catalog-v1",
		Tasks:                   []GenerationTask{{ID: "task_01", Domain: DomainFrontend, Complexity: ComplexityLow}},
		Cells: []RoutingCell{{
			Domain: DomainFrontend, Complexity: ComplexityLow, TaskIDs: []string{"task_01"},
			Selected:  RuntimeCandidate{ExecutorID: inventory.ExecutorCursorAgent, ProviderID: "cursor", ModelID: "grok-4.6", Reasoning: "medium", ModelTier: ModelTierFrontier},
			Fallbacks: []RuntimeCandidate{}, FallbackLimit: 1,
		}},
		Rules:                 []RuntimeRule{{Match: RuntimeMatch{Domain: DomainFrontend, Complexity: ComplexityLow}, Runtime: RuntimeValue{Provider: "cursor", Model: "grok-4.6", Reasoning: "medium"}}},
		DeliveryFallbackLimit: 3,
		EnclosingBudget:       LoopBudgetCeiling{IterationCap: 4},
	}
	finalized, err := finalizeGeneration(legacy)
	if err != nil {
		t.Fatalf("finalizeGeneration() error = %v", err)
	}
	payload, err := json.Marshal(finalized)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	var decoded RoutingGeneration
	if err := json.Unmarshal(payload, &decoded); err != nil {
		t.Fatalf("json.Unmarshal() error = %v", err)
	}
	replayed, err := finalizeGeneration(decoded)
	if err != nil {
		t.Fatalf("finalizeGeneration(decoded) error = %v", err)
	}
	if replayed.Digest != finalized.Digest || replayed.Cells[0].Selected.ExecutorID != inventory.ExecutorCursorAgent {
		t.Fatalf("legacy replay = %#v, want digest %q and executor %q", replayed, finalized.Digest, inventory.ExecutorCursorAgent)
	}
	replayedPayload, err := json.Marshal(replayed)
	if err != nil {
		t.Fatalf("json.Marshal(replayed) error = %v", err)
	}
	if !bytes.Equal(replayedPayload, payload) {
		t.Fatalf("legacy generation bytes changed\nwant %s\ngot  %s", payload, replayedPayload)
	}
}

func TestRoutingOutputRecordsSortedEnrichmentEvidence(t *testing.T) {
	t.Parallel()

	generation := RoutingGeneration{
		SchemaVersion: routingGenerationSchemaVersion,
		Cells: []RoutingCell{{
			Selected: RuntimeCandidate{EnrichmentIDs: []inventory.ExecutorID{
				inventory.ExecutorCursorAgent, inventory.ExecutorClaude, inventory.ExecutorCursorAgent,
			}},
			Fallbacks: []RuntimeCandidate{{EnrichmentIDs: []inventory.ExecutorID{
				inventory.ExecutorOpenCode, inventory.ExecutorAgy, inventory.ExecutorAgy,
			}}},
		}},
		Rejections: []CandidateRejection{{EnrichmentIDs: []inventory.ExecutorID{
			inventory.ExecutorClaude, inventory.ExecutorCodex, inventory.ExecutorClaude,
		}}},
	}
	finalized, err := finalizeGeneration(generation)
	if err != nil {
		t.Fatalf("finalizeGeneration() error = %v", err)
	}
	if got, want := finalized.Cells[0].Selected.EnrichmentIDs, []inventory.ExecutorID{inventory.ExecutorClaude, inventory.ExecutorCursorAgent}; !slices.Equal(got, want) {
		t.Fatalf("selected enrichments = %#v, want %#v", got, want)
	}
	if got, want := finalized.Cells[0].Fallbacks[0].EnrichmentIDs, []inventory.ExecutorID{inventory.ExecutorAgy, inventory.ExecutorOpenCode}; !slices.Equal(got, want) {
		t.Fatalf("fallback enrichments = %#v, want %#v", got, want)
	}
	if got, want := finalized.Rejections[0].EnrichmentIDs, []inventory.ExecutorID{inventory.ExecutorClaude, inventory.ExecutorCodex}; !slices.Equal(got, want) {
		t.Fatalf("rejection enrichments = %#v, want %#v", got, want)
	}
}
