package inventory

import (
	"slices"
	"testing"
)

func TestSnapshotDigestIsStableAcrossInputOrder(t *testing.T) {
	t.Parallel()

	first := ExecutorSnapshot{
		ID:           ExecutorCodex,
		Availability: AvailabilityAvailable,
		Version: Evidence{
			Name:   "version",
			Source: "codex --version",
			State:  ResolutionResolved,
			Digest: "sha256:version-codex",
		},
		Capabilities: []Evidence{
			{Name: "mcp", Source: "codex mcp list", State: ResolutionDeclared, Digest: "sha256:mcp"},
			{Name: "models", Source: "codex debug models", State: ResolutionResolved, Digest: "sha256:models", Identifiers: []string{"gpt-5.6-sol"}},
			{Name: "models", Source: "codex debug models", State: ResolutionResolved, Digest: "sha256:models", Identifiers: []string{"gpt-5.6-luna"}},
		},
	}
	second := ExecutorSnapshot{
		ID:           ExecutorCompozy,
		Availability: AvailabilityAvailable,
		Version: Evidence{
			Name:   "version",
			Source: "compozy version",
			State:  ResolutionResolved,
			Digest: "sha256:version-compozy",
		},
	}

	want, err := NewSnapshot("catalog-generation-7", []ExecutorSnapshot{first, second})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	got, err := NewSnapshot("catalog-generation-7", []ExecutorSnapshot{
		second,
		{
			ID:           first.ID,
			Availability: first.Availability,
			Version:      first.Version,
			Capabilities: []Evidence{
				first.Capabilities[2],
				first.Capabilities[0],
				first.Capabilities[1],
			},
		},
	})
	if err != nil {
		t.Fatalf("NewSnapshot(reordered) error = %v", err)
	}

	if got.Digest != want.Digest {
		t.Fatalf("Digest = %q, want stable %q", got.Digest, want.Digest)
	}
	if !slices.EqualFunc(got.Executors, want.Executors, func(a, b ExecutorSnapshot) bool {
		return a.ID == b.ID
	}) {
		t.Fatalf("executor order = %#v, want canonical %#v", got.Executors, want.Executors)
	}
}

func TestSnapshotCanonicalizesAndValidatesProviderBindings(t *testing.T) {
	t.Parallel()

	snapshot, err := NewSnapshot("catalog-generation-7", []ExecutorSnapshot{{
		ID:           ExecutorCursorAgent,
		Availability: AvailabilityAvailable,
		ProviderBindings: []ProviderBinding{
			{ProviderID: "cursor", ModelID: "grok-4.6[effort=high,fast=true]"},
			{ProviderID: "cursor"},
			{ProviderID: "cursor", ModelID: "grok-4.6[effort=high,fast=true]"},
		},
	}})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	want := []ProviderBinding{{ProviderID: "cursor"}, {ProviderID: "cursor", ModelID: "grok-4.6[effort=high,fast=true]"}}
	if !slices.Equal(snapshot.Executors[0].ProviderBindings, want) {
		t.Fatalf("provider bindings = %#v, want canonical %#v", snapshot.Executors[0].ProviderBindings, want)
	}

	for _, invalid := range []ProviderBinding{{}, {ProviderID: "cursor\nsecret"}, {ProviderID: "cursor", ModelID: "model\tsecret"}} {
		if _, err := NewSnapshot("catalog-generation-7", []ExecutorSnapshot{{
			ID: ExecutorCursorAgent, Availability: AvailabilityAvailable, ProviderBindings: []ProviderBinding{invalid},
		}}); err == nil {
			t.Fatalf("NewSnapshot(provider binding %#v) error = nil", invalid)
		}
	}
}

func TestSnapshotRejectsUnknownExecutorAndResolutionState(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		executor ExecutorID
		state    ResolutionState
	}{
		{name: "unknown executor", executor: ExecutorID("shell-agent"), state: ResolutionResolved},
		{name: "unknown resolution", executor: ExecutorCodex, state: ResolutionState("assumed")},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewSnapshot("catalog-generation-7", []ExecutorSnapshot{{
				ID:           tt.executor,
				Availability: AvailabilityAvailable,
				Version: Evidence{
					Name:   "version",
					Source: "bounded probe",
					State:  tt.state,
					Digest: "sha256:safe",
				},
			}})
			if err == nil {
				t.Fatal("NewSnapshot() error = nil, want closed-schema validation error")
			}
		})
	}
}

func TestSnapshotCanonicalizesAndDigestsCatalogModelCosts(t *testing.T) {
	t.Parallel()

	costs := []CatalogModelCost{
		{ProviderID: "codex", ModelID: "gpt-5.5", Cost: ModelCost{InputPerMillion: 1.25, OutputPerMillion: 10}},
		{ProviderID: "claude", ModelID: "claude-opus-5", Cost: ModelCost{InputPerMillion: 5, OutputPerMillion: 25, CacheReadPerMillion: 0.5, CacheWritePerMillion: 6.25}},
	}
	snapshot, err := NewSnapshot("catalog-generation-7", []ExecutorSnapshot{{
		ID: ExecutorCompozy, Availability: AvailabilityAvailable, CatalogModelCosts: costs,
	}})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	got := snapshot.Executors[0].CatalogModelCosts
	if len(got) != 2 || got[0].ProviderID != "claude" || got[1].ProviderID != "codex" {
		t.Fatalf("catalog model costs = %#v, want sorted by provider/model", got)
	}
	without, err := NewSnapshot("catalog-generation-7", []ExecutorSnapshot{{
		ID: ExecutorCompozy, Availability: AvailabilityAvailable,
	}})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	if snapshot.Digest == without.Digest {
		t.Fatal("snapshot digest ignores catalog model costs")
	}
}

func TestSnapshotRejectsInvalidCatalogModelCost(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		cost CatalogModelCost
	}{
		{name: "invalid provider", cost: CatalogModelCost{ProviderID: "bad provider!", ModelID: "m", Cost: ModelCost{InputPerMillion: 1}}},
		{name: "invalid model", cost: CatalogModelCost{ProviderID: "claude", ModelID: "bad model!", Cost: ModelCost{InputPerMillion: 1}}},
		{name: "negative rate", cost: CatalogModelCost{ProviderID: "claude", ModelID: "claude-opus-5", Cost: ModelCost{InputPerMillion: -1}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewSnapshot("catalog-generation-7", []ExecutorSnapshot{{
				ID: ExecutorCompozy, Availability: AvailabilityAvailable,
				CatalogModelCosts: []CatalogModelCost{tt.cost},
			}})
			if err == nil {
				t.Fatal("NewSnapshot() error = nil, want catalog model cost validation error")
			}
		})
	}
}
