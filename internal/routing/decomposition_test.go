package routing

import (
	"errors"
	"testing"
)

func TestDecompositionSplitsIndependentBackendAndFrontendWork(t *testing.T) {
	t.Parallel()

	taskSet := fixtureTaskSet(
		TaskArtifact{ID: "task_01", Domain: DomainBackend, Complexity: ComplexityHigh},
		TaskArtifact{ID: "task_02", Domain: DomainFrontend, Complexity: ComplexityHigh},
	)
	graph, err := ValidateClassification(taskSet, fixtureProposals(taskSet))
	if err != nil {
		t.Fatalf("ValidateClassification() error = %v", err)
	}
	if got := len(graph.Tasks); got != 2 {
		t.Fatalf("task count = %d, want 2 materialized tasks", got)
	}
	if graph.Tasks[0].Domain != DomainBackend || graph.Tasks[1].Domain != DomainFrontend {
		t.Fatalf("domains = %s/%s, want backend/frontend", graph.Tasks[0].Domain, graph.Tasks[1].Domain)
	}
}

func TestDecompositionPreservesDependencyEdgesAndRejectsCycles(t *testing.T) {
	t.Parallel()

	taskSet := fixtureTaskSet(
		TaskArtifact{ID: "task_01", Domain: DomainBackend, Complexity: ComplexityMedium},
		TaskArtifact{ID: "task_02", Domain: DomainFrontend, Complexity: ComplexityMedium, Dependencies: []string{"task_01"}},
	)
	graph, err := ValidateClassification(taskSet, fixtureProposals(taskSet))
	if err != nil {
		t.Fatalf("ValidateClassification() error = %v", err)
	}
	if len(graph.Tasks[1].Dependencies) != 1 || graph.Tasks[1].Dependencies[0] != "task_01" {
		t.Fatalf("dependencies = %#v, want task_01", graph.Tasks[1].Dependencies)
	}

	cyclic := fixtureTaskSet(
		TaskArtifact{ID: "task_01", Domain: DomainBackend, Complexity: ComplexityMedium, Dependencies: []string{"task_02"}},
		TaskArtifact{ID: "task_02", Domain: DomainFrontend, Complexity: ComplexityMedium, Dependencies: []string{"task_01"}},
	)
	if _, err := ValidateClassification(cyclic, fixtureProposals(cyclic)); !errors.Is(err, ErrReauthoringRequired) {
		t.Fatalf("ValidateClassification(cycle) error = %v, want ErrReauthoringRequired", err)
	}
}

func TestDecompositionUsesFullstackOnlyWithIndivisibilityEvidence(t *testing.T) {
	t.Parallel()

	taskSet := fixtureTaskSet(TaskArtifact{ID: "task_01", Domain: DomainFullstack, Complexity: ComplexityHigh})
	proposal := fixtureProposals(taskSet)
	if _, err := ValidateClassification(taskSet, proposal); !errors.Is(err, ErrReauthoringRequired) {
		t.Fatalf("ValidateClassification(fullstack without reason) error = %v, want ErrReauthoringRequired", err)
	}
	proposal[0].IndivisibleReason = "client and server protocol must change atomically"
	proposal[0].Evidence = []EvidenceReference{{Kind: EvidenceAcceptanceCriterion, Reference: "one atomic protocol transition"}}
	if _, err := ValidateClassification(taskSet, proposal); err != nil {
		t.Fatalf("ValidateClassification(fullstack with evidence) error = %v", err)
	}
}

func TestDecompositionRejectsDuplicateMissingAndInventedTaskIDs(t *testing.T) {
	t.Parallel()

	taskSet := fixtureTaskSet(
		TaskArtifact{ID: "task_01", Domain: DomainBackend, Complexity: ComplexityLow},
		TaskArtifact{ID: "task_02", Domain: DomainFrontend, Complexity: ComplexityLow},
	)
	tests := map[string][]ClassificationProposal{
		"duplicate": {
			{TaskID: "task_01", Domain: DomainBackend, Complexity: ComplexityLow, Confidence: 0.9},
			{TaskID: "task_01", Domain: DomainBackend, Complexity: ComplexityLow, Confidence: 0.9},
		},
		"missing": {
			{TaskID: "task_01", Domain: DomainBackend, Complexity: ComplexityLow, Confidence: 0.9},
		},
		"invented": {
			{TaskID: "task_01", Domain: DomainBackend, Complexity: ComplexityLow, Confidence: 0.9},
			{TaskID: "task_99", Domain: DomainFrontend, Complexity: ComplexityLow, Confidence: 0.9},
		},
	}
	for name, proposals := range tests {
		if _, err := ValidateClassification(taskSet, proposals); !errors.Is(err, ErrReauthoringRequired) {
			t.Fatalf("%s error = %v, want ErrReauthoringRequired", name, err)
		}
	}
}
