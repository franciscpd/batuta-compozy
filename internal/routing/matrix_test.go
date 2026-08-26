package routing

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"
)

func TestMatrixRefreshReplacesOnlyExactOwnedFingerprints(t *testing.T) {
	t.Parallel()

	operator := RuntimeRule{Match: RuntimeMatch{ID: "task-special"}, Runtime: RuntimeValue{Provider: "operator", Model: "exact"}}
	old := RuntimeRule{Match: RuntimeMatch{Domain: DomainFrontend, Complexity: ComplexityHigh}, Runtime: RuntimeValue{Provider: "cursor", Model: "old"}}
	next := RuntimeRule{Match: RuntimeMatch{Domain: DomainFrontend, Complexity: ComplexityHigh}, Runtime: RuntimeValue{Provider: "cursor", Model: "grok-4.6"}}
	boundary := newMemoryConfigBoundary(7, LoopConfigDocument{"runtime_rules": mustJSON(t, []RuntimeRule{operator, old})})
	store, _ := NewOwnershipStore(t.TempDir())
	if err := store.Save("workspace-1", RoutingJournal{SchemaVersion: 1, OwnedRules: []OwnedRuntimeRule{mustOwnedRule(t, old)}, Generations: map[string]RoutingGeneration{}, DeliveryBindings: map[string]string{}}); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	manager := MatrixManager{Config: boundary, Store: store}
	generation := safeGenerationFixture("sha256:next")
	generation.Rules = []RuntimeRule{next}
	if _, err := manager.Apply(context.Background(), "workspace-1", "implement-tasks", generation); err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	rules, _ := decodeRuntimeRules(boundary.config["runtime_rules"])
	if !slices.Equal(rules, []RuntimeRule{operator, next}) {
		t.Fatalf("rules = %#v, want exact old owned replaced after operator", rules)
	}
}

func TestMatrixRefreshPreservesModifiedAndOperatorRules(t *testing.T) {
	t.Parallel()

	old := RuntimeRule{Match: RuntimeMatch{Domain: DomainFrontend, Complexity: ComplexityHigh}, Runtime: RuntimeValue{Provider: "cursor", Model: "old"}}
	modified := old
	modified.Runtime.Model = "operator-modified"
	boundary := newMemoryConfigBoundary(7, LoopConfigDocument{"runtime_rules": mustJSON(t, []RuntimeRule{modified})})
	store, _ := NewOwnershipStore(t.TempDir())
	_ = store.Save("workspace-1", RoutingJournal{SchemaVersion: 1, OwnedRules: []OwnedRuntimeRule{mustOwnedRule(t, old)}, Generations: map[string]RoutingGeneration{}, DeliveryBindings: map[string]string{}})
	manager := MatrixManager{Config: boundary, Store: store}
	generation := safeGenerationFixture("sha256:next")
	generation.Rules = []RuntimeRule{{Match: old.Match, Runtime: RuntimeValue{Provider: "cursor", Model: "new"}}}
	if _, err := manager.Apply(context.Background(), "workspace-1", "implement-tasks", generation); !errors.Is(err, ErrOwnershipUnproven) {
		t.Fatalf("Apply(modified owned rule) error = %v, want ErrOwnershipUnproven", err)
	}
	rules, _ := decodeRuntimeRules(boundary.config["runtime_rules"])
	if !slices.Equal(rules, []RuntimeRule{modified}) || boundary.writeCalls != 0 {
		t.Fatalf("modified rule changed or write occurred: rules=%#v writes=%d", rules, boundary.writeCalls)
	}
}

func TestMatrixRefreshBlocksDeletionWhenOwnershipCannotBeProven(t *testing.T) {
	t.Parallel()

	stored := RuntimeRule{Match: RuntimeMatch{Domain: DomainBackend, Complexity: ComplexityLow}, Runtime: RuntimeValue{Provider: "operator", Model: "model"}}
	boundary := newMemoryConfigBoundary(3, LoopConfigDocument{"runtime_rules": mustJSON(t, []RuntimeRule{stored})})
	store, _ := NewOwnershipStore(t.TempDir())
	missingOwned := RuntimeRule{Match: RuntimeMatch{Domain: DomainFrontend, Complexity: ComplexityHigh}, Runtime: RuntimeValue{Provider: "cursor", Model: "missing"}}
	_ = store.Save("workspace-1", RoutingJournal{SchemaVersion: 1, OwnedRules: []OwnedRuntimeRule{mustOwnedRule(t, missingOwned)}, Generations: map[string]RoutingGeneration{}, DeliveryBindings: map[string]string{}})
	manager := MatrixManager{Config: boundary, Store: store}
	generation := safeGenerationFixture("sha256:new")
	generation.Rules = []RuntimeRule{{Match: RuntimeMatch{Domain: DomainFrontend, Complexity: ComplexityLow}, Runtime: RuntimeValue{Provider: "cursor", Model: "grok-4.6"}}}
	if _, err := manager.Apply(context.Background(), "workspace-1", "implement-tasks", generation); !errors.Is(err, ErrOwnershipUnproven) {
		t.Fatalf("Apply(missing journal with stored rules) error = %v, want ErrOwnershipUnproven", err)
	}
	if boundary.writeCalls != 0 {
		t.Fatalf("write calls = %d, want 0", boundary.writeCalls)
	}
}

func TestMatrixFirstApplyTreatsExistingRulesAsOperatorOwned(t *testing.T) {
	t.Parallel()

	operator := RuntimeRule{Match: RuntimeMatch{Domain: DomainFrontend, Complexity: ComplexityLow}, Runtime: RuntimeValue{Provider: "operator", Model: "model"}}
	next := RuntimeRule{Match: RuntimeMatch{Domain: DomainFrontend, Complexity: ComplexityLow}, Runtime: RuntimeValue{Provider: "cursor", Model: "grok-4.6"}}
	boundary := newMemoryConfigBoundary(3, LoopConfigDocument{"runtime_rules": mustJSON(t, []RuntimeRule{operator})})
	store, _ := NewOwnershipStore(t.TempDir())
	manager := MatrixManager{Config: boundary, Store: store}
	generation := safeGenerationFixture("sha256:new")
	generation.Rules = []RuntimeRule{next}
	if _, err := manager.Apply(context.Background(), "workspace-1", "implement-tasks", generation); err != nil {
		t.Fatalf("Apply(first matrix over operator rules) error = %v", err)
	}
	rules, _ := decodeRuntimeRules(boundary.config["runtime_rules"])
	if !slices.Equal(rules, []RuntimeRule{operator, next}) {
		t.Fatalf("rules = %#v, want operator preserved before later Batuta rule", rules)
	}
}

func TestMatrixJournalNeverContainsRawInventoryOrCredentials(t *testing.T) {
	t.Parallel()

	store, _ := NewOwnershipStore(t.TempDir())
	generation := safeGenerationFixture("sha256:generation")
	generation.InventoryDigest = "sha256:redacted"
	journal := RoutingJournal{SchemaVersion: 1, CurrentGeneration: generation.Digest, Generations: map[string]RoutingGeneration{generation.Digest: generation}, DeliveryBindings: map[string]string{}}
	if err := store.Save("workspace-1", journal); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	body, _ := os.ReadFile(store.pathFor("workspace-1"))
	for _, secret := range []string{"raw_inventory", "credential", "api_key", "task body"} {
		if strings.Contains(string(body), secret) {
			t.Fatalf("journal contains forbidden %q: %s", secret, body)
		}
	}
}

type memoryConfigBoundary struct {
	revision   int64
	config     LoopConfigDocument
	writeCalls int
}

func newMemoryConfigBoundary(revision int64, config LoopConfigDocument) *memoryConfigBoundary {
	return &memoryConfigBoundary{revision: revision, config: cloneLoopConfig(config)}
}

func (b *memoryConfigBoundary) Read(context.Context, string, string) (LoopConfigSnapshot, error) {
	rules, err := decodeRuntimeRules(b.config["runtime_rules"])
	return LoopConfigSnapshot{Config: cloneLoopConfig(b.config), RuntimeRules: rules, EffectiveConfig: json.RawMessage(`{}`), ConfigRevision: b.revision}, err
}

func (b *memoryConfigBoundary) Write(_ context.Context, _, _ string, expected int64, config LoopConfigDocument) (LoopConfigSnapshot, error) {
	b.writeCalls++
	if expected != b.revision {
		return LoopConfigSnapshot{}, &ConfigRevisionConflictError{ExpectedRevision: expected, CurrentRevision: b.revision}
	}
	b.revision++
	b.config = cloneLoopConfig(config)
	return b.Read(context.Background(), "", "")
}

func mustOwnedRule(t *testing.T, rule RuntimeRule) OwnedRuntimeRule {
	t.Helper()
	fingerprint, err := ruleFingerprint(rule)
	if err != nil {
		t.Fatalf("ruleFingerprint() error = %v", err)
	}
	return OwnedRuntimeRule{Rule: rule, Fingerprint: fingerprint}
}
