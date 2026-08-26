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
