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
