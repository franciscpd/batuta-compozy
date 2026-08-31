package routing

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"testing"
)

func TestMatrixApplyRequiresCurrentOperatorAlignmentBeforeNewDelivery(t *testing.T) {
	t.Parallel()

	input := validMatrixApplyInput(t)
	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}

	_, err = (MatrixManager{Store: store}).Apply(context.Background(), input)
	if !errors.Is(err, ErrRoutingAlignmentRequired) {
		t.Fatalf("Apply(unconfirmed) error = %v, want ErrRoutingAlignmentRequired", err)
	}
	if _, exists, loadErr := store.Load(input.WorkspaceID); loadErr != nil || exists {
		t.Fatalf("Load() = exists:%v error:%v, want no journal mutation", exists, loadErr)
	}
}

func TestAlignmentConfirmationRejectsUnarchivedGeneration(t *testing.T) {
	t.Parallel()

	input := validMatrixApplyInput(t)
	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	_, err = (AlignmentManager{Store: store}).Confirm(input.WorkspaceID, "session_operator", input.Generation)
	if !errors.Is(err, ErrGenerationUnknown) {
		t.Fatalf("Confirm(unarchived) error = %v, want ErrGenerationUnknown", err)
	}
}

func TestAlignmentConfirmationIsReusableUntilRuntimeOrFallbackChanges(t *testing.T) {
	t.Parallel()

	input := validMatrixApplyInput(t)
	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	manager := AlignmentManager{Store: store}

	before, err := manager.Status(input.WorkspaceID, input.Generation)
	if err != nil || before.State != AlignmentRequired {
		t.Fatalf("Status(unconfirmed) = %#v, error=%v", before, err)
	}
	if err := store.ArchiveGeneration(input.WorkspaceID, input.Generation); err != nil {
		t.Fatalf("ArchiveGeneration() error = %v", err)
	}
	confirmed, err := manager.Confirm(input.WorkspaceID, "session_operator", input.Generation)
	if err != nil || confirmed.State != AlignmentConfirmed || confirmed.AlignmentDigest == "" || confirmed.Replayed {
		t.Fatalf("Confirm() = %#v, error=%v", confirmed, err)
	}
	replay, err := manager.Confirm(input.WorkspaceID, "session_operator", input.Generation)
	if err != nil || !replay.Replayed || replay.AlignmentDigest != confirmed.AlignmentDigest || !replay.ConfirmedAt.Equal(confirmed.ConfirmedAt) {
		t.Fatalf("Confirm(replay) = %#v, error=%v; first=%#v", replay, err, confirmed)
	}
	after, err := manager.Status(input.WorkspaceID, input.Generation)
	if err != nil || after.State != AlignmentConfirmed || after.AlignmentDigest != confirmed.AlignmentDigest {
		t.Fatalf("Status(confirmed) = %#v, error=%v", after, err)
	}

	if _, err := (MatrixManager{Store: store}).Apply(context.Background(), input); err != nil {
		t.Fatalf("Apply(confirmed) error = %v", err)
	}

	changed := input.Generation
	changed.Cells = append([]RoutingCell(nil), input.Generation.Cells...)
	changed.Cells[0] = input.Generation.Cells[0]
	changed.Cells[0].Fallbacks = []RuntimeCandidate{{
		ExecutorID: "compozy", ProviderID: "claude", ModelID: "claude-fable-5", Reasoning: changed.Cells[0].Selected.Reasoning,
	}}
	changed, err = finalizeGeneration(changed)
	if err != nil {
		t.Fatalf("finalizeGeneration(changed) error = %v", err)
	}
	changedStatus, err := manager.Status(input.WorkspaceID, changed)
	if err != nil || changedStatus.State != AlignmentRequired || len(changedStatus.ChangedCells) != 1 {
		t.Fatalf("Status(changed fallback) = %#v, error=%v", changedStatus, err)
	}

	journalBefore, _, err := store.Load(input.WorkspaceID)
	if err != nil {
		t.Fatalf("Load(before changed apply) error = %v", err)
	}
	changedInput := input
	changedInput.Generation = changed
	_, err = (MatrixManager{Store: store}).Apply(context.Background(), changedInput)
	if !errors.Is(err, ErrRoutingAlignmentRequired) {
		t.Fatalf("Apply(changed fallback) error = %v, want ErrRoutingAlignmentRequired", err)
	}
	journalAfter, _, err := store.Load(input.WorkspaceID)
	if err != nil || !reflect.DeepEqual(journalAfter, journalBefore) {
		t.Fatalf("unconfirmed changed apply mutated journal: error=%v\nbefore=%#v\nafter=%#v", err, journalBefore, journalAfter)
	}
}

func TestAlignmentReplayProtectsLatestEquivalentGeneration(t *testing.T) {
	t.Parallel()

	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	first := alignmentGenerationFixture(t, validGenerationFixture(t))
	if err := store.ArchiveGeneration("workspace-1", first); err != nil {
		t.Fatalf("ArchiveGeneration(first) error = %v", err)
	}
	manager := AlignmentManager{Store: store}
	if _, err := manager.Confirm("workspace-1", "session-1", first); err != nil {
		t.Fatalf("Confirm(first) error = %v", err)
	}
	latest := first
	latest.PolicyVersion = "same-projection-new-generation"
	latest, err = finalizeGeneration(latest)
	if err != nil {
		t.Fatalf("finalizeGeneration(latest) error = %v", err)
	}
	if err := store.ArchiveGeneration("workspace-1", latest); err != nil {
		t.Fatalf("ArchiveGeneration(latest) error = %v", err)
	}
	replayed, err := manager.Confirm("workspace-1", "session-2", latest)
	if err != nil || !replayed.Replayed || replayed.GenerationDigest != latest.Digest {
		t.Fatalf("Confirm(latest) = %#v, error %v", replayed, err)
	}
	for index := 0; index < maxPendingGenerations+4; index++ {
		candidate := first
		candidate.PolicyVersion = fmt.Sprintf("abandoned-%02d", index)
		candidate, err = finalizeGeneration(candidate)
		if err != nil {
			t.Fatalf("finalizeGeneration(candidate %d) error = %v", index, err)
		}
		if err := store.ArchiveGeneration("workspace-1", candidate); err != nil {
			t.Fatalf("ArchiveGeneration(candidate %d) error = %v", index, err)
		}
	}
	if loaded, err := store.LoadGeneration("workspace-1", latest.Digest); err != nil || loaded.Digest != latest.Digest {
		t.Fatalf("LoadGeneration(latest) = %#v, error %v", loaded, err)
	}
}

func alignmentGenerationFixture(t *testing.T, generation RoutingGeneration) RoutingGeneration {
	t.Helper()
	generation.Cells = []RoutingCell{{
		Domain: DomainFrontend, Complexity: ComplexityHigh, TaskIDs: []string{"task_1"},
		Selected: RuntimeCandidate{
			ExecutorID: "compozy", ProviderID: "codex", ModelID: "gpt-5.6-luna", Reasoning: "high", ModelTier: ModelTierAdvanced,
		},
		Fallbacks:     []RuntimeCandidate{},
		FallbackLimit: 3,
		Policy:        complexityPolicy(ComplexityHigh),
	}}
	var err error
	generation, err = finalizeGeneration(generation)
	if err != nil {
		t.Fatalf("finalizeGeneration(alignment) error = %v", err)
	}
	return generation
}
