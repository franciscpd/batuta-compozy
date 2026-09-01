package extensionapp

import (
	"testing"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/franciscpd/batuta-compozy/internal/routing"
)

func TestLiveCatalogFromInventoryAttachesModelCosts(t *testing.T) {
	t.Parallel()

	snapshot, err := inventory.NewSnapshot("sha256:generation", []inventory.ExecutorSnapshot{{
		ID: inventory.ExecutorCompozy, Availability: inventory.AvailabilityAvailable,
		Capabilities: []inventory.Evidence{{
			Name: "models", Source: "compozy provider models list", State: inventory.ResolutionResolved,
			Digest: "sha256:generation", Identifiers: []string{"claude/claude-opus-5", "cursor/grok-4.6"},
		}},
		CatalogModelCosts: []inventory.CatalogModelCost{{
			ProviderID: "claude", ModelID: "claude-opus-5",
			Cost: inventory.ModelCost{InputPerMillion: 5, OutputPerMillion: 25},
		}},
	}})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	catalog, err := liveCatalogFromInventory(snapshot)
	if err != nil {
		t.Fatalf("liveCatalogFromInventory() error = %v", err)
	}
	byKey := make(map[string]routing.CatalogModel, len(catalog.Models))
	for _, model := range catalog.Models {
		byKey[routing.ModelKey(model.ProviderID, model.ModelID)] = model
	}
	opus := byKey[routing.ModelKey("claude", "claude-opus-5")]
	if opus.Cost == nil || opus.Cost.InputPerMillion != 5 || opus.Cost.OutputPerMillion != 25 {
		t.Fatalf("opus cost = %#v, want catalog rates attached", opus.Cost)
	}
	if grok := byKey[routing.ModelKey("cursor", "grok-4.6")]; grok.Cost != nil {
		t.Fatalf("grok cost = %#v, want nil for unpriced pair", grok.Cost)
	}
}
