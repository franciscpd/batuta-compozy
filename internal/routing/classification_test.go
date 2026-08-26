package routing

import (
	"errors"
	"strings"
	"testing"
)

func TestClassificationRejectsUnknownAndContradictoryFields(t *testing.T) {
	t.Parallel()

	if _, err := DecodeClassificationRequest([]byte(`{"slug":"demo","proposals":[],"raw_config":{}}`)); err == nil {
		t.Fatal("DecodeClassificationRequest() accepted unknown raw_config field")
	}
	if _, err := DecodeClassificationRequest([]byte(`{"slug":"demo","proposals":[{"task_id":"task_01","domain":"backend","complexity":"low","confidence":0.9,"requirements":[],"evidence":[],"dependencies":[],"command":"rm"}]}`)); err == nil {
		t.Fatal("DecodeClassificationRequest() accepted unknown proposal field")
	}

	taskSet := fixtureTaskSet(
		TaskArtifact{ID: "task_01", Domain: DomainBackend, Complexity: ComplexityLow},
		TaskArtifact{ID: "task_02", Domain: DomainFrontend, Complexity: ComplexityMedium, Dependencies: []string{"task_01"}},
	)
	proposals := fixtureProposals(taskSet)
	proposals[1].Dependencies = nil
	if _, err := ValidateClassification(taskSet, proposals); !errors.Is(err, ErrReauthoringRequired) {
		t.Fatalf("ValidateClassification(contradictory dependencies) error = %v, want ErrReauthoringRequired", err)
	}
}

func TestClassificationAuthoredMetadataWins(t *testing.T) {
	t.Parallel()

	taskSet := fixtureTaskSet(TaskArtifact{
		ID:         "task_01",
		Title:      "Browser task",
		Status:     "pending",
		Domain:     DomainFrontend,
		Complexity: ComplexityHigh,
	})
	proposals := fixtureProposals(taskSet)
	proposals[0].Domain = DomainBackend
	proposals[0].Complexity = ComplexityLow
	proposals[0].Requirements = []CapabilityRequirement{{Kind: CapabilityBrowserTooling, ID: "chromium", Hard: true}}
	proposals[0].Evidence = []EvidenceReference{{Kind: EvidenceAcceptanceCriterion, Reference: "renders accessibly"}}

	graph, err := ValidateClassification(taskSet, proposals)
	if err != nil {
		t.Fatalf("ValidateClassification() error = %v", err)
	}
	if graph.Tasks[0].Domain != DomainFrontend || graph.Tasks[0].Complexity != ComplexityHigh {
		t.Fatalf("validated metadata = %s/%s, want frontend/high", graph.Tasks[0].Domain, graph.Tasks[0].Complexity)
	}
}

func TestClassificationRejectsLowConfidenceAndUnboundedEvidence(t *testing.T) {
	t.Parallel()

	taskSet := fixtureTaskSet(TaskArtifact{ID: "task_01", Domain: DomainBackend, Complexity: ComplexityLow})
	proposals := fixtureProposals(taskSet)
	proposals[0].Confidence = minimumClassificationConfidence - 0.01
	if _, err := ValidateClassification(taskSet, proposals); !errors.Is(err, ErrClassificationRetryable) {
		t.Fatalf("ValidateClassification(low confidence) error = %v, want ErrClassificationRetryable", err)
	}

	proposals = fixtureProposals(taskSet)
	proposals[0].Evidence = make([]EvidenceReference, maxEvidenceReferences+1)
	for i := range proposals[0].Evidence {
		proposals[0].Evidence[i] = EvidenceReference{Kind: EvidenceTaskField, Reference: "title"}
	}
	if _, err := ValidateClassification(taskSet, proposals); !errors.Is(err, ErrClassificationRetryable) {
		t.Fatalf("ValidateClassification(unbounded count) error = %v, want ErrClassificationRetryable", err)
	}

	proposals = fixtureProposals(taskSet)
	proposals[0].Evidence = []EvidenceReference{{Kind: EvidencePath, Reference: strings.Repeat("x", maxEvidenceReferenceBytes+1)}}
	if _, err := ValidateClassification(taskSet, proposals); !errors.Is(err, ErrClassificationRetryable) {
		t.Fatalf("ValidateClassification(unbounded length) error = %v, want ErrClassificationRetryable", err)
	}
}

func TestClassificationRejectsInventedCapabilityKinds(t *testing.T) {
	t.Parallel()

	taskSet := fixtureTaskSet(TaskArtifact{ID: "task_01", Domain: DomainSecurity, Complexity: ComplexityCritical})
	proposals := fixtureProposals(taskSet)
	proposals[0].Requirements = []CapabilityRequirement{{Kind: "arbitrary_shell", ID: "bash", Hard: true}}
	if _, err := ValidateClassification(taskSet, proposals); !errors.Is(err, ErrClassificationRetryable) {
		t.Fatalf("ValidateClassification(invented capability) error = %v, want ErrClassificationRetryable", err)
	}
}

func TestClassificationAcceptsEveryCanonicalTaxonomyValue(t *testing.T) {
	t.Parallel()

	domains := []Domain{DomainBackend, DomainFrontend, DomainMobile, DomainData, DomainInfra, DomainSecurity, DomainTesting, DomainDocs, DomainGeneral, DomainFullstack}
	complexities := []Complexity{ComplexityLow, ComplexityMedium, ComplexityHigh, ComplexityCritical}
	for _, domain := range domains {
		for _, complexity := range complexities {
			task := TaskArtifact{ID: "task_01", Domain: domain, Complexity: complexity}
			proposal := fixtureProposals(fixtureTaskSet(task))
			if domain == DomainFullstack {
				proposal[0].IndivisibleReason = "one atomic contract spans client and server"
				proposal[0].Evidence = []EvidenceReference{{Kind: EvidenceAcceptanceCriterion, Reference: "atomic cross-domain verification"}}
			}
			if _, err := ValidateClassification(fixtureTaskSet(task), proposal); err != nil {
				t.Fatalf("ValidateClassification(%s/%s) error = %v", domain, complexity, err)
			}
		}
	}
}

func fixtureTaskSet(tasks ...TaskArtifact) TaskSet {
	return TaskSet{Slug: "demo", Digest: "task-set-digest", Tasks: tasks}
}

func fixtureProposals(taskSet TaskSet) []ClassificationProposal {
	proposals := make([]ClassificationProposal, 0, len(taskSet.Tasks))
	for _, task := range taskSet.Tasks {
		proposals = append(proposals, ClassificationProposal{
			TaskID:       task.ID,
			Domain:       task.Domain,
			Complexity:   task.Complexity,
			Confidence:   0.9,
			Dependencies: append([]string(nil), task.Dependencies...),
		})
	}
	return proposals
}
