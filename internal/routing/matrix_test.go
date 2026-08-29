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
	confirmMatrixInput(t, store, input)
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
	if delivery.Graph == nil || len(delivery.Graph.Tasks) != len(input.TaskSnapshot.Tasks) ||
		delivery.Graph.Tasks[0].TaskID != input.TaskSnapshot.Tasks[0].ID ||
		delivery.Graph.Tasks[0].State != GraphTaskPending || delivery.Graph.Tasks[0].AuthoredIndex != 0 {
		t.Fatalf("delivery graph = %#v, want immutable pending task projection", delivery.Graph)
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
	confirmMatrixInput(t, store, input)
	first, err := manager.Apply(context.Background(), input)
	if err != nil {
		t.Fatalf("Apply(first) error = %v", err)
	}
	if err := store.WithLockedJournal(input.WorkspaceID, func(tx *JournalTx) error {
		delivery := tx.Journal.Deliveries[first.DeliveryID]
		if _, err := delivery.Graph.AdmitReadyWave(ReadyWaveInput{
			IntegrationHeadSHA: input.InitialWorktreeFingerprint.HeadSHA,
			RemainingSlots:     1,
			ReachableCommits:   map[string]bool{},
		}); err != nil {
			return err
		}
		tx.Journal.Deliveries[first.DeliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist graph progress: %v", err)
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
	graph := journal.Deliveries[first.DeliveryID].Graph
	if graph == nil || graph.Tasks[0].State != GraphTaskPreparing || len(graph.Waves) != 1 {
		t.Fatalf("replay reset durable graph progress: %#v", graph)
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
	confirmMatrixInput(t, store, firstInput)
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
	generation := alignmentGenerationFixture(t, validGenerationFixture(t))
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

func confirmMatrixInput(t *testing.T, store *OwnershipStore, input MatrixApplyInput) {
	t.Helper()
	if _, err := (AlignmentManager{Store: store}).Confirm(input.WorkspaceID, input.OriginSessionID, input.Generation); err != nil {
		t.Fatalf("Confirm(matrix input) error = %v", err)
	}
}
