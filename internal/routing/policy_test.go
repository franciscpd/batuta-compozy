package routing

import "testing"

func TestUnknownModelTierDefaultsToStandardOnly(t *testing.T) {
	t.Parallel()

	policy := DefaultSelectionPolicy()
	if got := policy.modelTier("future-provider", "future-model"); got != ModelTierStandard {
		t.Fatalf("unknown model tier = %v, want conservative standard", got)
	}
	if policy.modelTier("future-provider", "future-model") >= modelFloor(ComplexityMedium) {
		t.Fatal("unknown model unexpectedly satisfies medium complexity floor")
	}
}
