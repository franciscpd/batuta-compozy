package routing

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestMatrixApplyArchivesGenerationAndDeliveryWithoutStoredConfig(t *testing.T) {
	t.Parallel()

	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	input := validMatrixApplyInput(t)
	started := time.Now().UTC()
	result, err := (MatrixManager{Store: store}).Apply(context.Background(), input)
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	finished := time.Now().UTC()
	if !canonicalSHA256.MatchString(result.DeliveryID) || result.GenerationDigest != input.Generation.Digest ||
		result.AttemptCeiling != 4 || result.TokenCeiling != 1_000_000 || result.RuleCount != len(input.Generation.Rules) {
		t.Fatalf("Apply() result = %#v", result)
	}
	if result.CreatedAt.Before(started) || result.CreatedAt.After(finished) || !result.AbsoluteDeadline.Equal(result.CreatedAt.Add(4*time.Hour)) {
		t.Fatalf("Apply() times = created:%v deadline:%v", result.CreatedAt, result.AbsoluteDeadline)
	}
	journal, exists, err := store.Load(input.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("Load() = exists:%v error:%v", exists, err)
	}
	delivery, exists := journal.Deliveries[result.DeliveryID]
	if !exists || journal.CurrentGeneration != input.Generation.Digest || len(journal.Generations) != 1 {
		t.Fatalf("journal = %#v", journal)
	}
	if delivery.WorkspaceID != input.WorkspaceID || delivery.WorktreeID != input.WorktreeID || delivery.WorktreeRoot != input.WorktreeRoot ||
		delivery.Slug != input.Slug || delivery.TaskSetDigest != input.TaskSetDigest || delivery.RoutingGenerationDigest != input.Generation.Digest ||
		delivery.OriginSessionID != input.OriginSessionID || len(delivery.Attempts) != 0 || delivery.State != DeliveryStateActive {
		t.Fatalf("delivery = %#v", delivery)
	}
}

func TestMatrixApplyReplaysIdenticalHeaderWithoutResettingBudget(t *testing.T) {
	t.Parallel()

	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	manager := MatrixManager{Store: store}
	input := validMatrixApplyInput(t)
	first, err := manager.Apply(context.Background(), input)
	if err != nil {
		t.Fatalf("Apply(first) error = %v", err)
	}
	time.Sleep(time.Millisecond)
	second, err := manager.Apply(context.Background(), input)
	if err != nil {
		t.Fatalf("Apply(replay) error = %v", err)
	}
	if first != second {
		t.Fatalf("Apply(replay) = %#v, want %#v", second, first)
	}
	journal, _, err := store.Load(input.WorkspaceID)
	if err != nil || len(journal.Deliveries) != 1 || len(journal.Generations) != 1 {
		t.Fatalf("replay journal = %#v, error:%v", journal, err)
	}
}

func TestMatrixApplyChangedTaskOrWorktreeCreatesDistinctDelivery(t *testing.T) {
	t.Parallel()

	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	manager := MatrixManager{Store: store}
	firstInput := validMatrixApplyInput(t)
	first, err := manager.Apply(context.Background(), firstInput)
	if err != nil {
		t.Fatalf("Apply(first) error = %v", err)
	}
	secondInput := firstInput
	secondInput.InitialWorktreeFingerprint.ContentSHA256 = digestFixture("changed-content")
	second, err := manager.Apply(context.Background(), secondInput)
	if err != nil {
		t.Fatalf("Apply(changed worktree) error = %v", err)
	}
	if first.DeliveryID == second.DeliveryID {
		t.Fatalf("delivery IDs = %q for distinct immutable worktree state", first.DeliveryID)
	}
	journal, _, err := store.Load(firstInput.WorkspaceID)
	if err != nil || len(journal.Deliveries) != 2 {
		t.Fatalf("changed-worktree journal = %#v, error:%v", journal, err)
	}
}

func TestMatrixApplyRejectsForeignOrForgedGenerationWithoutMutation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*MatrixApplyInput)
	}{
		{name: "foreign workspace", mutate: func(input *MatrixApplyInput) { input.WorkspaceID = "workspace-foreign" }},
		{name: "forged digest", mutate: func(input *MatrixApplyInput) { input.Generation.Digest = digestFixture("forged") }},
		{name: "task set mismatch", mutate: func(input *MatrixApplyInput) { input.TaskSetDigest = digestFixture("other-tasks") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			store, err := NewOwnershipStore(t.TempDir())
			if err != nil {
				t.Fatalf("NewOwnershipStore() error = %v", err)
			}
			input := validMatrixApplyInput(t)
			test.mutate(&input)
			if _, err := (MatrixManager{Store: store}).Apply(context.Background(), input); !errors.Is(err, ErrOwnershipUnproven) {
				t.Fatalf("Apply() error = %v, want ErrOwnershipUnproven", err)
			}
			if _, exists, err := store.Load(input.WorkspaceID); err != nil || exists {
				t.Fatalf("Load(after rejected apply) = exists:%v error:%v", exists, err)
			}
		})
	}
}

func validMatrixApplyInput(t *testing.T) MatrixApplyInput {
	t.Helper()
	generation := validGenerationFixture(t)
	taskSnapshot := validTaskSnapshotFixture(t)
	return MatrixApplyInput{
		WorkspaceID: "workspace-1", WorkspaceRoot: "/workspace/project", WorktreeID: "worktree-1", WorktreeRoot: "/workspace/project",
		Slug: "frontend-demo", OriginSessionID: "session-1", TaskSetDigest: generation.TaskSetDigest,
		TaskSnapshot: taskSnapshot,
		InitialWorktreeFingerprint: WorktreeFingerprint{
			HeadSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", PorcelainSHA256: digestFixture("porcelain"), ContentSHA256: digestFixture("content"),
		},
		Generation: generation,
	}
}
