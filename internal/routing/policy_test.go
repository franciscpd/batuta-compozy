package routing

import "testing"

func TestUnknownModelTierRemainsUnclassified(t *testing.T) {
	t.Parallel()

	policy := DefaultSelectionPolicy()
	if got := policy.modelTier("future-provider", "future-model"); got != ModelTierUnknown {
		t.Fatalf("unknown model tier = %v, want unclassified", got)
	}
}
