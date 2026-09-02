package routing

import (
	"encoding/json"
	"errors"
	"os"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDeliveryJournalPersistsStrictSchemaV2(t *testing.T) {
	t.Parallel()

	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	delivery := validDeliveryFixture(t)
	err = store.WithLockedJournal("workspace-1", func(tx *JournalTx) error {
		tx.Journal.Generations[delivery.RoutingGenerationDigest] = validGenerationFixture(t)
		tx.Journal.CurrentGeneration = delivery.RoutingGenerationDigest
		tx.Journal.Deliveries[delivery.DeliveryID] = delivery
		return tx.Persist()
	})
	if err != nil {
		t.Fatalf("WithLockedJournal() error = %v", err)
	}

	loaded, exists, err := store.Load("workspace-1")
	if err != nil || !exists {
		t.Fatalf("Load() = exists:%v error:%v", exists, err)
	}
	if loaded.SchemaVersion != 2 || loaded.CurrentGeneration != delivery.RoutingGenerationDigest {
		t.Fatalf("journal header = %#v", loaded)
	}
	got, ok := loaded.Deliveries[delivery.DeliveryID]
	if !ok || got.Slug != "frontend-demo" || len(got.Attempts) != 1 || got.Attempts[0].State != AttemptPlanned {
		t.Fatalf("delivery = %#v, want exact persisted header and attempt", got)
	}
	journalInfo, err := os.Stat(store.pathFor("workspace-1"))
	if err != nil {
		t.Fatalf("stat journal: %v", err)
	}
	lockInfo, err := os.Stat(store.pathFor("workspace-1") + ".lock")
	if err != nil {
		t.Fatalf("stat lock: %v", err)
	}
	dirInfo, err := os.Stat(store.root)
	if err != nil {
		t.Fatalf("stat directory: %v", err)
	}
	if journalInfo.Mode().Perm() != 0o600 || lockInfo.Mode().Perm() != 0o600 || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("modes = journal:%o lock:%o dir:%o, want 600/600/700", journalInfo.Mode().Perm(), lockInfo.Mode().Perm(), dirInfo.Mode().Perm())
	}
}

func TestLegacyGraphlessDeliveryLoadsByteForByteAndCannotGainGraph(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewOwnershipStore(root)
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	delivery := validDeliveryFixture(t)
	generation := validGenerationFixture(t)
	journal := RoutingJournal{
		SchemaVersion: journalSchemaVersion, CurrentGeneration: generation.Digest,
		Generations: map[string]RoutingGeneration{generation.Digest: generation},
		Deliveries:  map[string]DeliveryRecord{delivery.DeliveryID: delivery},
	}
	payload, err := json.Marshal(journal)
	if err != nil {
		t.Fatalf("json.Marshal(legacy journal) error = %v", err)
	}
	if strings.Contains(string(payload), `"graph"`) {
		t.Fatalf("legacy payload unexpectedly contains graph: %s", payload)
	}
	if err := os.WriteFile(store.pathFor(delivery.WorkspaceID), payload, 0o600); err != nil {
		t.Fatalf("write legacy journal: %v", err)
	}
	loaded, exists, err := store.Load(delivery.WorkspaceID)
	if err != nil || !exists || loaded.Deliveries[delivery.DeliveryID].Graph != nil {
		t.Fatalf("Load(legacy) = %#v, exists %v, error %v", loaded, exists, err)
	}
	afterRead, err := os.ReadFile(store.pathFor(delivery.WorkspaceID))
	if err != nil || !slices.Equal(afterRead, payload) {
		t.Fatalf("legacy bytes changed on read: error %v\nwant %s\ngot  %s", err, payload, afterRead)
	}

	err = store.WithLockedJournal(delivery.WorkspaceID, func(tx *JournalTx) error {
		record := tx.Journal.Deliveries[delivery.DeliveryID]
		record.Graph, err = NewDeliveryGraph(record.TaskSnapshot, generation, record.InitialWorktreeFingerprint.HeadSHA)
		if err != nil {
			return err
		}
		tx.Journal.Deliveries[delivery.DeliveryID] = record
		return tx.Persist()
	})
	if !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("attach graph to started legacy delivery error = %v, want ErrDeliveryConflict", err)
	}
}

func TestDeliveryGraphJournalAllowsOnlyValidTaskLifecycle(t *testing.T) {
	t.Parallel()

	store, delivery := persistedGraphDeliveryStore(t)
	base := delivery.InitialWorktreeFingerprint.HeadSHA
	mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
		_, err := graph.AdmitReadyWave(ReadyWaveInput{
			IntegrationHeadSHA: base, RemainingSlots: 1, ReachableCommits: map[string]bool{},
		})
		if err != nil {
			return err
		}
		graph.Tasks[0].Attempts = append(graph.Tasks[0].Attempts, GraphTaskAttempt{
			Execution: 1,
			Runtime:   RuntimeValue{Provider: "codex", Model: "gpt-5.6-luna", Reasoning: "high"},
			State:     GraphTaskPreparing, BaseHeadSHA: base,
		})
		return nil
	})
	mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
		task := &graph.Tasks[0]
		task.State = GraphTaskRunning
		task.Attempts[0].State = GraphTaskRunning
		task.Attempts[0].WorktreeID = "task-worktree-1"
		task.Attempts[0].WorktreeRoot = "/workspace/task-1"
		task.Attempts[0].ChildRunID = "child-run-1"
		return nil
	})
	mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
		_, err := graph.RecordQuestion("task_1", 1, "child-run-1", TaskQuestion{
			RequestID: digestFixture("request-1"), Prompt: "Choose the public behavior",
			ContextDigest: digestFixture("question-context"),
		}, delivery.CreatedAt.Add(10*time.Minute))
		return err
	})
	mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
		_, _, err := graph.RecordAnswer("task_1", 1, TaskAnswer{
			QuestionOperationID: digestFixture("request-1"), LoopRunID: "child-run-1",
			Generation: 2, NodeID: "ask_operator", ItemIndex: 0, Value: "Keep compatibility",
		}, delivery.CreatedAt.Add(20*time.Minute))
		return err
	})
	candidate := graphGitSHA("candidate")
	mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
		task := &graph.Tasks[0]
		task.State = GraphTaskCandidate
		attempt := &task.Attempts[len(task.Attempts)-1]
		attempt.State = GraphTaskCandidate
		attempt.CandidateCommitSHA = candidate
		attempt.VerificationDigest = digestFixture("verification")
		return nil
	})
	mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
		task := &graph.Tasks[0]
		task.State = GraphTaskIntegrated
		task.IntegratedCommitSHA = candidate
		task.Attempts[len(task.Attempts)-1].State = GraphTaskIntegrated
		return nil
	})

	journal, exists, err := store.Load(delivery.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("Load(final graph) = exists %v, error %v", exists, err)
	}
	got := journal.Deliveries[delivery.DeliveryID].Graph
	if got == nil || got.Tasks[0].State != GraphTaskIntegrated || got.Tasks[0].IntegratedCommitSHA != candidate ||
		len(got.Pauses) != 1 || got.Pauses[0].EndedAt == nil {
		t.Fatalf("final graph = %#v", got)
	}
	err = store.WithLockedJournal(delivery.WorkspaceID, func(tx *JournalTx) error {
		record := tx.Journal.Deliveries[delivery.DeliveryID]
		record.Graph.Tasks[0].State = GraphTaskBlocked
		record.Graph.Tasks[0].BlockerCode = "late_rewrite"
		tx.Journal.Deliveries[delivery.DeliveryID] = record
		return tx.Persist()
	})
	if !errors.Is(err, ErrInvalidDeliveryTransition) {
		t.Fatalf("rewrite integrated task error = %v, want ErrInvalidDeliveryTransition", err)
	}
}

func TestDeliveryGraphAllowsPendingAndRunningTasksToBlock(t *testing.T) {
	t.Parallel()

	t.Run("pending", func(t *testing.T) {
		t.Parallel()
		store, delivery := persistedGraphDeliveryStore(t)
		mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
			graph.Tasks[0].State = GraphTaskBlocked
			graph.Tasks[0].BlockerCode = "dependency_blocked"
			return nil
		})
		journal, _, err := store.Load(delivery.WorkspaceID)
		if err != nil || journal.Deliveries[delivery.DeliveryID].Graph.Tasks[0].State != GraphTaskBlocked {
			t.Fatalf("Load(blocked pending) error = %v, journal = %#v", err, journal)
		}
	})

	t.Run("running", func(t *testing.T) {
		t.Parallel()
		store, delivery := persistedGraphDeliveryStore(t)
		base := delivery.InitialWorktreeFingerprint.HeadSHA
		mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
			if _, err := graph.AdmitReadyWave(ReadyWaveInput{
				IntegrationHeadSHA: base, RemainingSlots: 1, ReachableCommits: map[string]bool{},
			}); err != nil {
				return err
			}
			graph.Tasks[0].Attempts = []GraphTaskAttempt{{
				Execution: 1,
				Runtime:   RuntimeValue{Provider: "codex", Model: "gpt-5.6-luna", Reasoning: "high"},
				State:     GraphTaskPreparing, BaseHeadSHA: base,
			}}
			return nil
		})
		mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
			task := &graph.Tasks[0]
			task.State = GraphTaskRunning
			task.Attempts[0].State = GraphTaskRunning
			task.Attempts[0].WorktreeID = "task-worktree-1"
			task.Attempts[0].WorktreeRoot = "/workspace/task-1"
			task.Attempts[0].ChildRunID = "child-run-1"
			return nil
		})
		mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
			task := &graph.Tasks[0]
			task.State = GraphTaskBlocked
			task.BlockerCode = "verification_failed"
			task.Attempts[0].State = GraphTaskBlocked
			task.Attempts[0].BlockerCode = "verification_failed"
			return nil
		})
		journal, _, err := store.Load(delivery.WorkspaceID)
		if err != nil || journal.Deliveries[delivery.DeliveryID].Graph.Tasks[0].State != GraphTaskBlocked {
			t.Fatalf("Load(blocked running) error = %v, journal = %#v", err, journal)
		}
	})
}

func TestDeliveryGraphConflictReexecutionAppendsAttemptAndIntegratedIsImmutable(t *testing.T) {
	t.Parallel()

	store, delivery := persistedGraphDeliveryStore(t)
	base := delivery.InitialWorktreeFingerprint.HeadSHA
	mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
		_, err := graph.AdmitReadyWave(ReadyWaveInput{
			IntegrationHeadSHA: base, RemainingSlots: 1, ReachableCommits: map[string]bool{},
		})
		if err != nil {
			return err
		}
		graph.Tasks[0].Attempts = []GraphTaskAttempt{{
			Execution: 1, Runtime: RuntimeValue{Provider: "codex", Model: "gpt-5.6-luna", Reasoning: "high"},
			State: GraphTaskPreparing, BaseHeadSHA: base,
		}}
		return nil
	})
	mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
		task := &graph.Tasks[0]
		task.State = GraphTaskRunning
		task.Attempts[0].State = GraphTaskRunning
		task.Attempts[0].WorktreeID = "task-worktree-1"
		task.Attempts[0].WorktreeRoot = "/workspace/task-1"
		task.Attempts[0].ChildRunID = "child-run-1"
		return nil
	})
	mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
		candidate := graphGitSHA("obsolete-candidate")
		task := &graph.Tasks[0]
		task.State = GraphTaskCandidate
		task.Attempts[0].State = GraphTaskCandidate
		task.Attempts[0].CandidateCommitSHA = candidate
		task.Attempts[0].VerificationDigest = digestFixture("verification-1")
		return nil
	})
	latestHead := graphGitSHA("latest-head")
	mutateGraphDelivery(t, store, delivery, func(graph *DeliveryGraph) error {
		task := &graph.Tasks[0]
		obsolete := task.Attempts[0].CandidateCommitSHA
		task.Attempts[0].Conflict = &ConflictProof{
			IntegrationOperationID: digestFixture("integration-operation"),
			IntegrationHeadSHA:     latestHead, CandidateCommitSHA: obsolete,
			EvidenceDigest: digestFixture("conflict-evidence"),
		}
		task.Attempts = append(task.Attempts, GraphTaskAttempt{
			Execution: 2, Runtime: RuntimeValue{Provider: "cursor", Model: "grok-4.6", Reasoning: "high"},
			State: GraphTaskPreparing, BaseHeadSHA: latestHead,
		})
		graph.Waves = append(graph.Waves, DeliveryWave{
			Number: len(graph.Waves) + 1, BaseHeadSHA: latestHead, TaskIDs: []string{task.TaskID},
		})
		task.State = GraphTaskPreparing
		return nil
	})

	journal, _, err := store.Load(delivery.WorkspaceID)
	if err != nil {
		t.Fatalf("Load(reexecution) error = %v", err)
	}
	reexecuted := journal.Deliveries[delivery.DeliveryID].Graph.Tasks[0]
	if len(reexecuted.Attempts) != 2 || reexecuted.Attempts[0].Conflict == nil ||
		reexecuted.Attempts[0].CandidateCommitSHA == "" || reexecuted.Attempts[1].BaseHeadSHA != latestHead {
		t.Fatalf("reexecuted task = %#v", reexecuted)
	}

	first := reexecuted.Attempts[0]
	err = store.WithLockedJournal(delivery.WorkspaceID, func(tx *JournalTx) error {
		record := tx.Journal.Deliveries[delivery.DeliveryID]
		record.Graph.Tasks[0].Attempts[0].CandidateCommitSHA = graphGitSHA("rewritten")
		tx.Journal.Deliveries[delivery.DeliveryID] = record
		return tx.Persist()
	})
	if !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("rewrite obsolete attempt error = %v, want ErrDeliveryConflict", err)
	}
	journal, _, _ = store.Load(delivery.WorkspaceID)
	if !reflect.DeepEqual(journal.Deliveries[delivery.DeliveryID].Graph.Tasks[0].Attempts[0], first) {
		t.Fatal("rejected rewrite changed durable attempt")
	}
}

func persistedGraphDeliveryStore(t *testing.T) (*OwnershipStore, DeliveryRecord) {
	t.Helper()
	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	delivery := validDeliveryFixture(t)
	delivery.Attempts = nil
	generation := validGenerationFixture(t)
	delivery.Graph, err = NewDeliveryGraph(delivery.TaskSnapshot, generation, delivery.InitialWorktreeFingerprint.HeadSHA)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	err = store.WithLockedJournal(delivery.WorkspaceID, func(tx *JournalTx) error {
		tx.Journal.Generations[generation.Digest] = generation
		tx.Journal.CurrentGeneration = generation.Digest
		tx.Journal.Deliveries[delivery.DeliveryID] = delivery
		return tx.Persist()
	})
	if err != nil {
		t.Fatalf("persist graph delivery: %v", err)
	}
	return store, delivery
}

func mutateGraphDelivery(
	t *testing.T,
	store *OwnershipStore,
	delivery DeliveryRecord,
	mutate func(*DeliveryGraph) error,
) {
	t.Helper()
	err := store.WithLockedJournal(delivery.WorkspaceID, func(tx *JournalTx) error {
		record := tx.Journal.Deliveries[delivery.DeliveryID]
		if record.Graph == nil {
			return ErrInvalidDeliveryGraph
		}
		if err := mutate(record.Graph); err != nil {
			return err
		}
		tx.Journal.Deliveries[delivery.DeliveryID] = record
		return tx.Persist()
	})
	if err != nil {
		t.Fatalf("mutate graph delivery: %v", err)
	}
}

func TestDeliveryJournalRejectsUnknownTrailingAndOversizedInput(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		body string
	}{
		{name: "unknown field", body: `{"schema_version":2,"current_generation":"","generations":{},"deliveries":{},"surprise":true}`},
		{name: "trailing value", body: `{"schema_version":2,"current_generation":"","generations":{},"deliveries":{}} {}`},
		{name: "oversized", body: strings.Repeat("x", maxRoutingJournalBytes+1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			store, err := NewOwnershipStore(root)
			if err != nil {
				t.Fatalf("NewOwnershipStore() error = %v", err)
			}
			if err := os.WriteFile(store.pathFor("workspace-1"), []byte(test.body), 0o600); err != nil {
				t.Fatalf("write fixture: %v", err)
			}
			if _, _, err := store.Load("workspace-1"); !errors.Is(err, ErrOwnershipUnproven) {
				t.Fatalf("Load() error = %v, want ErrOwnershipUnproven", err)
			}
		})
	}
}

func TestDeliveryJournalUpgradesV1OnlyOnExplicitMutation(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewOwnershipStore(root)
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	generation := validGenerationFixture(t)
	v1 := `{"schema_version":1,"current_generation":"` + generation.Digest + `","generations":{"` + generation.Digest + `":` + mustJSONText(t, generation) + `},"delivery_bindings":{"run-old":"` + generation.Digest + `"},"owned_rules":[]}`
	if err := os.WriteFile(store.pathFor("workspace-1"), []byte(v1), 0o600); err != nil {
		t.Fatalf("write v1 fixture: %v", err)
	}

	loaded, exists, err := store.Load("workspace-1")
	if err != nil || !exists {
		t.Fatalf("Load(v1) = exists:%v error:%v", exists, err)
	}
	if loaded.SchemaVersion != 2 || loaded.CurrentGeneration != generation.Digest || len(loaded.Generations) != 1 || len(loaded.Deliveries) != 0 {
		t.Fatalf("upgraded journal = %#v", loaded)
	}
	before, err := os.ReadFile(store.pathFor("workspace-1"))
	if err != nil {
		t.Fatalf("read v1 fixture: %v", err)
	}
	if string(before) != v1 {
		t.Fatal("read-only v1 upgrade rewrote the journal")
	}

	if err := store.WithLockedJournal("workspace-1", func(tx *JournalTx) error { return tx.Persist() }); err != nil {
		t.Fatalf("persist upgraded journal: %v", err)
	}
	after, err := os.ReadFile(store.pathFor("workspace-1"))
	if err != nil {
		t.Fatalf("read v2 journal: %v", err)
	}
	if strings.Contains(string(after), "delivery_bindings") || strings.Contains(string(after), "owned_rules") || !strings.Contains(string(after), `"schema_version":2`) {
		t.Fatalf("persisted upgrade = %s", after)
	}
}

func TestDeliveryJournalRejectsHeaderMutationAndInvalidAttemptTransitions(t *testing.T) {
	t.Parallel()

	store := persistedDeliveryStore(t)
	delivery := validDeliveryFixture(t)
	tests := []struct {
		name   string
		mutate func(*DeliveryRecord)
	}{
		{name: "immutable slug", mutate: func(record *DeliveryRecord) { record.Slug = "changed" }},
		{name: "skip submitted", mutate: func(record *DeliveryRecord) { record.Attempts[0].State = AttemptTerminal }},
		{name: "noncontiguous attempt", mutate: func(record *DeliveryRecord) {
			record.Attempts = append(record.Attempts, DeliveryAttempt{Attempt: 3, OperationID: digestFixture("operation-3"), RequestDigest: digestFixture("request-3"), RuntimeRules: record.Attempts[0].RuntimeRules, State: AttemptPlanned, PlannedAt: record.CreatedAt.Add(time.Minute)})
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := store.WithLockedJournal("workspace-1", func(tx *JournalTx) error {
				record := tx.Journal.Deliveries[delivery.DeliveryID]
				test.mutate(&record)
				tx.Journal.Deliveries[delivery.DeliveryID] = record
				return tx.Persist()
			})
			if !errors.Is(err, ErrDeliveryConflict) {
				t.Fatalf("Persist() error = %v, want ErrDeliveryConflict", err)
			}
		})
	}
}

func TestDeliveryAttemptAppendReplaysOnlyIdenticalOperation(t *testing.T) {
	t.Parallel()

	delivery := validDeliveryFixture(t)
	proposed := DeliveryAttempt{
		Attempt: 2, OperationID: digestFixture("operation-2"), RequestDigest: digestFixture("request-2"),
		RuntimeRules: append([]RuntimeRule(nil), delivery.Attempts[0].RuntimeRules...), State: AttemptPlanned,
		PlannedAt: delivery.CreatedAt.Add(time.Minute),
	}
	appended, replay, err := delivery.AppendAttempt(proposed)
	if err != nil || replay || appended.Attempt != 2 || len(delivery.Attempts) != 2 {
		t.Fatalf("AppendAttempt(new) = %#v, replay:%v error:%v", appended, replay, err)
	}
	appended, replay, err = delivery.AppendAttempt(proposed)
	if err != nil || !replay || appended.RequestDigest != proposed.RequestDigest || len(delivery.Attempts) != 2 {
		t.Fatalf("AppendAttempt(replay) = %#v, replay:%v error:%v", appended, replay, err)
	}
	conflict := proposed
	conflict.RequestDigest = digestFixture("different-request")
	if _, _, err := delivery.AppendAttempt(conflict); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("AppendAttempt(conflict) error = %v, want ErrDeliveryConflict", err)
	}
}

func TestTerminalDeliveryRejectsNewAttemptsAtDomainAndJournalBoundaries(t *testing.T) {
	t.Parallel()

	for _, state := range []DeliveryState{DeliveryStateDone, DeliveryStateBlocked, DeliveryStateExhausted} {
		t.Run(string(state), func(t *testing.T) {
			delivery := validDeliveryFixture(t)
			delivery.State = state
			proposed := DeliveryAttempt{
				Attempt: 2, OperationID: digestFixture("terminal-operation-" + string(state)),
				RequestDigest: digestFixture("terminal-request-" + string(state)),
				RuntimeRules:  append([]RuntimeRule(nil), delivery.Attempts[0].RuntimeRules...),
				State:         AttemptPlanned, PlannedAt: delivery.CreatedAt.Add(time.Minute),
			}

			if _, _, err := delivery.AppendAttempt(proposed); !errors.Is(err, ErrInvalidDeliveryTransition) {
				t.Fatalf("AppendAttempt() error = %v, want ErrInvalidDeliveryTransition", err)
			}

			after := delivery
			after.Attempts = append(append([]DeliveryAttempt(nil), delivery.Attempts...), proposed)
			if err := validateDeliveryTransition(delivery, after); !errors.Is(err, ErrInvalidDeliveryTransition) {
				t.Fatalf("validateDeliveryTransition() error = %v, want ErrInvalidDeliveryTransition", err)
			}
		})
	}
}

func TestJournalTransactionKeepsPersistedIntentAfterCallbackError(t *testing.T) {
	t.Parallel()

	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	delivery := validDeliveryFixture(t)
	injected := errors.New("external mutation failed")
	err = store.WithLockedJournal("workspace-1", func(tx *JournalTx) error {
		tx.Journal.Generations[delivery.RoutingGenerationDigest] = validGenerationFixture(t)
		tx.Journal.CurrentGeneration = delivery.RoutingGenerationDigest
		tx.Journal.Deliveries[delivery.DeliveryID] = delivery
		if err := tx.Persist(); err != nil {
			return err
		}
		return injected
	})
	if !errors.Is(err, injected) {
		t.Fatalf("WithLockedJournal() error = %v, want injected error", err)
	}
	loaded, exists, err := store.Load("workspace-1")
	if err != nil || !exists || loaded.Deliveries[delivery.DeliveryID].Attempts[0].State != AttemptPlanned {
		t.Fatalf("durable planned state = %#v, exists:%v error:%v", loaded, exists, err)
	}
}

func TestSuccessorRuntimeRulesAdvanceOnlyObservedFailedTasks(t *testing.T) {
	t.Parallel()

	generation := fallbackGenerationFixture(t)
	selected := RuntimeValue{Provider: "cursor", Model: "grok-4.6", Reasoning: "high"}
	prior := []RuntimeRule{
		{Match: RuntimeMatch{ID: "task_1"}, Runtime: selected},
		{Match: RuntimeMatch{ID: "task_2"}, Runtime: selected},
	}
	rules, err := SuccessorRuntimeRules(generation, prior, []string{"task_1", "task_2"}, []string{"task_1"})
	if err != nil {
		t.Fatalf("SuccessorRuntimeRules() error = %v", err)
	}
	if len(rules) != 2 || rules[0].Match.ID != "task_1" || rules[0].Runtime.Provider != "codex" || rules[0].Runtime.Model != "gpt-5.6-luna" {
		t.Fatalf("failed task rule = %#v", rules)
	}
	if rules[1].Match.ID != "task_2" || rules[1].Runtime != selected {
		t.Fatalf("dependency-skipped sibling advanced unexpectedly: %#v", rules[1])
	}
}

func TestSuccessorRuntimeRulesRejectUnobservedOrExhaustedFallback(t *testing.T) {
	t.Parallel()

	generation := fallbackGenerationFixture(t)
	selected := RuntimeValue{Provider: "cursor", Model: "grok-4.6", Reasoning: "high"}
	fallback := RuntimeValue{Provider: "codex", Model: "gpt-5.6-luna", Reasoning: "high"}
	if _, err := SuccessorRuntimeRules(generation, nil, []string{"task_2"}, []string{"task_1"}); !errors.Is(err, ErrDeliveryConflict) {
		t.Fatalf("SuccessorRuntimeRules(unobserved failure) error = %v, want ErrDeliveryConflict", err)
	}
	if _, err := SuccessorRuntimeRules(generation, []RuntimeRule{{Match: RuntimeMatch{ID: "task_1"}, Runtime: fallback}}, []string{"task_1"}, []string{"task_1"}); !errors.Is(err, ErrNoEligibleCandidate) {
		t.Fatalf("SuccessorRuntimeRules(exhausted) error = %v, want ErrNoEligibleCandidate", err)
	}
	rules, err := SuccessorRuntimeRules(generation, nil, []string{"task_1"}, nil)
	if err != nil || len(rules) != 1 || rules[0].Runtime != selected {
		t.Fatalf("SuccessorRuntimeRules(pending) = %#v, error:%v", rules, err)
	}
}

func TestDeliveryAttemptIdentityIsStableAndOrderSensitive(t *testing.T) {
	t.Parallel()

	delivery := validDeliveryFixture(t)
	rules := delivery.Attempts[0].RuntimeRules
	operation, request, err := DeriveAttemptIdentity(delivery, 2, []string{"task_1"}, rules, 750_000, 3600)
	if err != nil {
		t.Fatalf("DeriveAttemptIdentity() error = %v", err)
	}
	operationReplay, requestReplay, err := DeriveAttemptIdentity(delivery, 2, []string{"task_1"}, append([]RuntimeRule(nil), rules...), 750_000, 3600)
	if err != nil || operationReplay != operation || requestReplay != request {
		t.Fatalf("replay identity = %q/%q, error %v; want %q/%q", operationReplay, requestReplay, err, operation, request)
	}
	changedOperation, changedRequest, err := DeriveAttemptIdentity(delivery, 2, []string{"task_2", "task_1"}, rules, 750_000, 3600)
	if err != nil || changedOperation == operation || changedRequest == request {
		t.Fatalf("ordered identity did not change: %q/%q, error %v", changedOperation, changedRequest, err)
	}
	if !canonicalSHA256.MatchString(operation) || !canonicalSHA256.MatchString(request) {
		t.Fatalf("identity format = %q/%q", operation, request)
	}
}

func persistedDeliveryStore(t *testing.T) *OwnershipStore {
	t.Helper()
	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	delivery := validDeliveryFixture(t)
	err = store.WithLockedJournal("workspace-1", func(tx *JournalTx) error {
		tx.Journal.Generations[delivery.RoutingGenerationDigest] = validGenerationFixture(t)
		tx.Journal.CurrentGeneration = delivery.RoutingGenerationDigest
		tx.Journal.Deliveries[delivery.DeliveryID] = delivery
		return tx.Persist()
	})
	if err != nil {
		t.Fatalf("persist fixture: %v", err)
	}
	return store
}

func validDeliveryFixture(t *testing.T) DeliveryRecord {
	t.Helper()
	generation := validGenerationFixture(t)
	taskSnapshot := validTaskSnapshotFixture(t)
	created := time.Date(2026, time.August, 26, 12, 0, 0, 0, time.UTC)
	return DeliveryRecord{
		DeliveryID: digestFixture("delivery"), WorkspaceID: "workspace-1", WorktreeID: "worktree-1",
		WorktreeRoot: "/workspace/project", Slug: "frontend-demo", TaskSetDigest: generation.TaskSetDigest,
		TaskSnapshot:            taskSnapshot,
		RoutingGenerationDigest: generation.Digest, OriginSessionID: "session-1", CreatedAt: created,
		AbsoluteDeadline: created.Add(4 * time.Hour), AttemptCeiling: 4, TokenCeiling: DeliveryTokenCeiling,
		InitialWorktreeFingerprint: WorktreeFingerprint{
			HeadSHA: strings.Repeat("a", 40), PorcelainSHA256: digestFixture("porcelain"), ContentSHA256: digestFixture("content"),
		},
		State: DeliveryStateActive,
		Attempts: []DeliveryAttempt{{
			Attempt: 1, OperationID: digestFixture("operation-1"), RequestDigest: digestFixture("request-1"),
			RuntimeRules: []RuntimeRule{{Match: RuntimeMatch{ID: "task_1"}, Runtime: RuntimeValue{Provider: "codex", Model: "gpt-5.6-luna", Reasoning: "high"}}},
			State:        AttemptPlanned, PlannedAt: created,
		}},
	}
}

func TestValidateDeliveryAcceptsTopologicalSnapshotWithLexicallySortedGeneration(t *testing.T) {
	t.Parallel()

	snapshot, err := (TaskSet{Slug: "ordered-demo", Tasks: []TaskArtifact{
		{ID: "task_02", Status: "pending", Domain: DomainBackend, Complexity: ComplexityLow, Digest: hexDigestFixture("task-02")},
		{ID: "task_01", Status: "pending", Domain: DomainFrontend, Complexity: ComplexityHigh, Dependencies: []string{"task_02"}, Digest: hexDigestFixture("task-01")},
	}}).DeliverySnapshot()
	if err != nil {
		t.Fatalf("DeliverySnapshot() error = %v", err)
	}
	generation, err := finalizeGeneration(RoutingGeneration{
		SchemaVersion: routingGenerationSchemaVersion, PolicyVersion: "test-v1",
		WorkspaceIdentityDigest: trustedWorkspaceIdentityDigest("workspace-1", "/workspace/project"), TaskSetDigest: snapshot.Digest,
		InventoryDigest: digestFixture("inventory-ordered"), CatalogGeneration: digestFixture("catalog-ordered"),
		Tasks: []GenerationTask{
			{ID: "task_01", Domain: DomainFrontend, Complexity: ComplexityHigh},
			{ID: "task_02", Domain: DomainBackend, Complexity: ComplexityLow},
		},
		Rules: []RuntimeRule{
			{Match: RuntimeMatch{ID: "task_01"}, Runtime: RuntimeValue{Provider: "codex", Model: "gpt-5.6-luna", Reasoning: "high"}},
			{Match: RuntimeMatch{ID: "task_02"}, Runtime: RuntimeValue{Provider: "codex", Model: "gpt-5.6-luna", Reasoning: "high"}},
		},
		DeliveryFallbackLimit: deliveryFallbackLimit,
	})
	if err != nil {
		t.Fatalf("finalizeGeneration() error = %v", err)
	}
	record := validDeliveryFixture(t)
	record.Slug = "ordered-demo"
	record.TaskSetDigest = snapshot.Digest
	record.TaskSnapshot = snapshot
	record.RoutingGenerationDigest = generation.Digest
	record.Attempts[0].RuntimeRules = append([]RuntimeRule(nil), generation.Rules...)

	if err := validateDelivery(record, map[string]RoutingGeneration{generation.Digest: generation}); err != nil {
		t.Fatalf("validateDelivery() error = %v", err)
	}
}

func validGenerationFixture(t *testing.T) RoutingGeneration {
	t.Helper()
	taskSnapshot := validTaskSnapshotFixture(t)
	generation, err := finalizeGeneration(RoutingGeneration{
		SchemaVersion: routingGenerationSchemaVersion, PolicyVersion: "test-v1",
		WorkspaceIdentityDigest: trustedWorkspaceIdentityDigest("workspace-1", "/workspace/project"), TaskSetDigest: taskSnapshot.Digest,
		InventoryDigest: digestFixture("inventory"), CatalogGeneration: digestFixture("catalog"),
		Tasks:                 []GenerationTask{{ID: "task_1", Domain: DomainFrontend, Complexity: ComplexityHigh}},
		Rules:                 []RuntimeRule{{Match: RuntimeMatch{ID: "task_1"}, Runtime: RuntimeValue{Provider: "codex", Model: "gpt-5.6-luna", Reasoning: "high"}}},
		DeliveryFallbackLimit: deliveryFallbackLimit,
	})
	if err != nil {
		t.Fatalf("finalizeGeneration() error = %v", err)
	}
	return generation
}

func validTaskSnapshotFixture(t *testing.T) DeliveryTaskSnapshot {
	t.Helper()
	snapshot, err := (TaskSet{Slug: "frontend-demo", Tasks: []TaskArtifact{{
		ID: "task_1", Status: "pending", Domain: DomainFrontend, Complexity: ComplexityHigh, Digest: hexDigestFixture("task-file"),
	}}}).DeliverySnapshot()
	if err != nil {
		t.Fatalf("DeliverySnapshot() error = %v", err)
	}
	return snapshot
}

func fallbackGenerationFixture(t *testing.T) RoutingGeneration {
	t.Helper()
	generation := validGenerationFixture(t)
	generation.Tasks = []GenerationTask{
		{ID: "task_1", Domain: DomainFrontend, Complexity: ComplexityHigh},
		{ID: "task_2", Domain: DomainFrontend, Complexity: ComplexityHigh},
	}
	generation.Cells = []RoutingCell{{
		Domain: DomainFrontend, Complexity: ComplexityHigh, TaskIDs: []string{"task_1", "task_2"},
		Selected:      RuntimeCandidate{ProviderID: "cursor", ModelID: "grok-4.6", Reasoning: "high"},
		Fallbacks:     []RuntimeCandidate{{ProviderID: "codex", ModelID: "gpt-5.6-luna", Reasoning: "high"}},
		FallbackLimit: 1,
	}}
	generation.Rules = []RuntimeRule{{
		Match:   RuntimeMatch{Domain: DomainFrontend, Complexity: ComplexityHigh},
		Runtime: RuntimeValue{Provider: "cursor", Model: "grok-4.6", Reasoning: "high"},
	}}
	generation, err := finalizeGeneration(generation)
	if err != nil {
		t.Fatalf("finalizeGeneration() error = %v", err)
	}
	return generation
}

func digestFixture(seed string) string {
	const hex = "0123456789abcdef"
	var builder strings.Builder
	builder.WriteString("sha256:")
	for index := 0; index < 64; index++ {
		builder.WriteByte(hex[(int(seed[index%len(seed)])+index)%len(hex)])
	}
	return builder.String()
}

func hexDigestFixture(seed string) string {
	return strings.TrimPrefix(digestFixture(seed), "sha256:")
}

func mustJSONText(t *testing.T, value any) string {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return string(payload)
}
