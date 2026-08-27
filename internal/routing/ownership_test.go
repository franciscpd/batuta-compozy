package routing

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func TestOwnershipWorkspaceLockSerializesSeparateStoreInstances(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	first, err := NewOwnershipStore(root)
	if err != nil {
		t.Fatalf("NewOwnershipStore(first) error = %v", err)
	}
	second, err := NewOwnershipStore(root)
	if err != nil {
		t.Fatalf("NewOwnershipStore(second) error = %v", err)
	}
	firstGeneration := validGenerationFixture(t)
	secondGeneration := firstGeneration
	secondGeneration.PolicyVersion = "test-v2"
	secondGeneration, err = finalizeGeneration(secondGeneration)
	if err != nil {
		t.Fatalf("finalizeGeneration(second) error = %v", err)
	}

	var entered atomic.Int32
	var wg sync.WaitGroup
	errorsByWriter := make(chan error, 2)
	write := func(store *OwnershipStore, generation RoutingGeneration) {
		defer wg.Done()
		errorsByWriter <- store.WithLockedJournal("workspace-1", func(tx *JournalTx) error {
			entered.Add(1)
			deadline := time.Now().Add(50 * time.Millisecond)
			for entered.Load() < 2 && time.Now().Before(deadline) {
				time.Sleep(time.Millisecond)
			}
			tx.Journal.Generations[generation.Digest] = generation
			tx.Journal.CurrentGeneration = generation.Digest
			return tx.Persist()
		})
	}
	wg.Add(2)
	go write(first, firstGeneration)
	go write(second, secondGeneration)
	wg.Wait()
	close(errorsByWriter)
	for err := range errorsByWriter {
		if err != nil {
			t.Fatalf("serialized journal update error = %v", err)
		}
	}
	journal, exists, err := first.Load("workspace-1")
	if err != nil || !exists || len(journal.Generations) != 2 {
		t.Fatalf("journal = %#v, exists %v, error %v; want both generations", journal, exists, err)
	}
}

func TestDeliveryGraphCrossStoreWritersNeverAdmitMoreThanFourTasks(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	stores := make([]*OwnershipStore, 2)
	for index := range stores {
		store, err := NewOwnershipStore(root)
		if err != nil {
			t.Fatalf("NewOwnershipStore(%d) error = %v", index, err)
		}
		stores[index] = store
	}
	snapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "pending", DomainBackend, ComplexityLow),
		graphTaskArtifact("task_02", "pending", DomainFrontend, ComplexityLow),
		graphTaskArtifact("task_03", "pending", DomainTesting, ComplexityLow),
		graphTaskArtifact("task_04", "pending", DomainDocs, ComplexityLow),
		graphTaskArtifact("task_05", "pending", DomainInfra, ComplexityLow),
	})
	generation := graphGenerationFixture(t, snapshot)
	delivery := validDeliveryFixture(t)
	delivery.DeliveryID = digestFixture("parallel-delivery")
	delivery.Slug = "graph-demo"
	delivery.TaskSetDigest = snapshot.Digest
	delivery.TaskSnapshot = snapshot
	delivery.RoutingGenerationDigest = generation.Digest
	delivery.Attempts = nil
	graph, err := NewDeliveryGraph(snapshot, generation, delivery.InitialWorktreeFingerprint.HeadSHA)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	delivery.Graph = graph
	if err := stores[0].WithLockedJournal(delivery.WorkspaceID, func(tx *JournalTx) error {
		tx.Journal.Generations[generation.Digest] = generation
		tx.Journal.CurrentGeneration = generation.Digest
		tx.Journal.Deliveries[delivery.DeliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist delivery: %v", err)
	}

	start := make(chan struct{})
	errorsByWriter := make(chan error, len(snapshot.Tasks))
	var writers sync.WaitGroup
	for index := range snapshot.Tasks {
		writers.Add(1)
		go func(index int) {
			defer writers.Done()
			<-start
			errorsByWriter <- stores[index%len(stores)].WithLockedJournal(delivery.WorkspaceID, func(tx *JournalTx) error {
				record := tx.Journal.Deliveries[delivery.DeliveryID]
				wave, err := record.Graph.AdmitReadyWave(ReadyWaveInput{
					IntegrationHeadSHA: delivery.InitialWorktreeFingerprint.HeadSHA,
					RemainingSlots:     1,
					ReachableCommits:   map[string]bool{},
				})
				if err != nil {
					return err
				}
				if len(wave.TaskIDs) == 1 {
					task := graphTaskByID(record.Graph.Tasks, wave.TaskIDs[0])
					if task == nil {
						return ErrInvalidDeliveryGraph
					}
					task.Attempts = append(task.Attempts, GraphTaskAttempt{
						Execution:   1,
						Runtime:     RuntimeValue{Provider: "codex", Model: "gpt-5.6-luna", Reasoning: "high"},
						State:       GraphTaskPreparing,
						BaseHeadSHA: delivery.InitialWorktreeFingerprint.HeadSHA,
					})
				}
				tx.Journal.Deliveries[delivery.DeliveryID] = record
				return tx.Persist()
			})
		}(index)
	}
	close(start)
	writers.Wait()
	close(errorsByWriter)
	for err := range errorsByWriter {
		if err != nil {
			t.Fatalf("parallel admission error = %v", err)
		}
	}
	if err := stores[1].WithLockedJournal(delivery.WorkspaceID, func(tx *JournalTx) error {
		record := tx.Journal.Deliveries[delivery.DeliveryID]
		for index := range record.Graph.Tasks {
			task := &record.Graph.Tasks[index]
			if task.State != GraphTaskPreparing {
				continue
			}
			task.State = GraphTaskRunning
			task.Attempts[0].State = GraphTaskRunning
			task.Attempts[0].WorktreeID = "worktree-" + task.TaskID
			task.Attempts[0].WorktreeRoot = "/workspace/" + task.TaskID
			task.Attempts[0].ChildRunID = "child-" + task.TaskID
		}
		tx.Journal.Deliveries[delivery.DeliveryID] = record
		return tx.Persist()
	}); err != nil {
		t.Fatalf("promote admitted tasks to running: %v", err)
	}
	journal, exists, err := stores[0].Load(delivery.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("Load() = exists %v, error %v", exists, err)
	}
	durable := journal.Deliveries[delivery.DeliveryID].Graph
	if durable == nil || activeGraphTaskCount(durable.Tasks) != MaxParallelTasks || len(durable.Waves) != MaxParallelTasks {
		t.Fatalf("durable graph = %#v, want exactly %d admitted tasks", durable, MaxParallelTasks)
	}
	pending := 0
	for _, task := range durable.Tasks {
		if task.State == GraphTaskPending {
			pending++
		}
	}
	if pending != 1 {
		t.Fatalf("pending tasks = %d, want one", pending)
	}
	running := 0
	for _, task := range durable.Tasks {
		if task.State == GraphTaskRunning {
			running++
		}
	}
	if running != MaxParallelTasks {
		t.Fatalf("running tasks = %d, want %d", running, MaxParallelTasks)
	}
}

func TestOwnershipJournalHashesWorkspaceFilenameAndExcludesSecrets(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewOwnershipStore(root)
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	workspaceID := "workspace-secret-identifier"
	generation := validGenerationFixture(t)
	if err := store.WithLockedJournal(workspaceID, func(tx *JournalTx) error {
		tx.Journal.Generations[generation.Digest] = generation
		tx.Journal.CurrentGeneration = generation.Digest
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist journal: %v", err)
	}
	path := store.pathFor(workspaceID)
	if strings.Contains(path, workspaceID) || filepath.Base(path) == workspaceID+".json" {
		t.Fatalf("journal path leaks workspace ID: %q", path)
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	for _, forbidden := range []string{"raw inventory", "task body", "credential-secret"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("journal contains forbidden value %q: %s", forbidden, body)
		}
	}
}

func TestRoutingGenerationArchiveSurvivesRefreshAndRestart(t *testing.T) {
	t.Parallel()

	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	first := validGenerationFixture(t)
	second := first
	second.PolicyVersion = "test-v2"
	second, err = finalizeGeneration(second)
	if err != nil {
		t.Fatalf("finalizeGeneration(second) error = %v", err)
	}
	if err := store.WithLockedJournal("workspace-1", func(tx *JournalTx) error {
		tx.Journal.Generations[first.Digest] = first
		tx.Journal.Generations[second.Digest] = second
		tx.Journal.CurrentGeneration = second.Digest
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist generations: %v", err)
	}

	restarted, err := NewOwnershipStore(store.root)
	if err != nil {
		t.Fatalf("NewOwnershipStore(restart) error = %v", err)
	}
	loaded, exists, err := restarted.Load("workspace-1")
	if err != nil || !exists || loaded.CurrentGeneration != second.Digest || len(loaded.Generations) != 2 {
		t.Fatalf("Load(restart) = %#v, exists:%v error:%v", loaded, exists, err)
	}
}

func TestJournalTransactionWithoutPersistIsReadOnly(t *testing.T) {
	t.Parallel()

	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	generation := validGenerationFixture(t)
	if err := store.WithLockedJournal("workspace-1", func(tx *JournalTx) error {
		tx.Journal.Generations[generation.Digest] = generation
		return nil
	}); err != nil {
		t.Fatalf("WithLockedJournal(read-only) error = %v", err)
	}
	if _, err := os.Stat(store.pathFor("workspace-1")); !os.IsNotExist(err) {
		t.Fatalf("read-only transaction journal stat error = %v, want not-exist", err)
	}
}

func safeGenerationFixture(_ string) RoutingGeneration {
	generation, err := finalizeGeneration(RoutingGeneration{
		SchemaVersion: routingGenerationSchemaVersion, PolicyVersion: "test-v1",
		WorkspaceIdentityDigest: digestFixture("workspace"), TaskSetDigest: hexDigestFixture("tasks"),
		InventoryDigest: digestFixture("inventory"), CatalogGeneration: digestFixture("catalog"),
		Tasks:                 []GenerationTask{{ID: "task_1", Domain: DomainFrontend, Complexity: ComplexityHigh}},
		Rules:                 []RuntimeRule{{Match: RuntimeMatch{ID: "task_1"}, Runtime: RuntimeValue{Provider: "codex", Model: "gpt-5.6-luna", Reasoning: "high"}}},
		DeliveryFallbackLimit: deliveryFallbackLimit,
	})
	if err != nil {
		panic(err)
	}
	return generation
}
