package routing

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestOwnershipJournalUsesWorkspaceHashAndRestrictiveModes(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	store, err := NewOwnershipStore(root)
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	workspaceID := "workspace-secret-identifier"
	journal := RoutingJournal{
		SchemaVersion: 1, CurrentGeneration: "sha256:generation",
		Generations:      map[string]RoutingGeneration{"sha256:generation": safeGenerationFixture("sha256:generation")},
		DeliveryBindings: map[string]string{},
	}
	if err := store.Save(workspaceID, journal); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	path := store.pathFor(workspaceID)
	if strings.Contains(path, workspaceID) || filepath.Base(path) == workspaceID+".json" {
		t.Fatalf("journal path leaks workspace ID: %q", path)
	}
	fileInfo, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat journal: %v", err)
	}
	dirInfo, err := os.Stat(filepath.Dir(path))
	if err != nil {
		t.Fatalf("stat journal dir: %v", err)
	}
	if fileInfo.Mode().Perm() != 0o600 || dirInfo.Mode().Perm() != 0o700 {
		t.Fatalf("journal modes = file:%o dir:%o, want 600/700", fileInfo.Mode().Perm(), dirInfo.Mode().Perm())
	}
	body, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read journal: %v", err)
	}
	for _, forbidden := range []string{workspaceID, "raw inventory", "task body", "credential-secret"} {
		if strings.Contains(string(body), forbidden) {
			t.Fatalf("journal contains forbidden value %q: %s", forbidden, body)
		}
	}
}

func TestRoutingGenerationArchiveSurvivesMatrixRefreshAndRestart(t *testing.T) {
	t.Parallel()

	store, err := NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	first := safeGenerationFixture("sha256:first")
	second := safeGenerationFixture("sha256:second")
	journal := RoutingJournal{
		SchemaVersion: 1, CurrentGeneration: second.Digest,
		Generations:      map[string]RoutingGeneration{first.Digest: first, second.Digest: second},
		DeliveryBindings: map[string]string{"run-1": first.Digest},
	}
	if err := store.Save("workspace-1", journal); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	restarted, _ := NewOwnershipStore(store.root)
	loaded, exists, err := restarted.Load("workspace-1")
	if err != nil || !exists {
		t.Fatalf("Load(restart) = exists:%v error:%v", exists, err)
	}
	if loaded.CurrentGeneration != second.Digest || loaded.DeliveryBindings["run-1"] != first.Digest || len(loaded.Generations) != 2 {
		t.Fatalf("loaded journal = %#v, want both immutable generations and binding", loaded)
	}
}

func TestDeliveryBindingUsesAuthoritativeRunInput(t *testing.T) {
	t.Parallel()

	store, _ := NewOwnershipStore(t.TempDir())
	generation := safeGenerationFixture("sha256:generation")
	journal := RoutingJournal{SchemaVersion: 1, CurrentGeneration: generation.Digest, Generations: map[string]RoutingGeneration{generation.Digest: generation}, DeliveryBindings: map[string]string{}}
	if err := store.Save("workspace-1", journal); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.BindDelivery("workspace-1", "run-1", generation.Digest); err != nil {
		t.Fatalf("BindDelivery() error = %v", err)
	}
	loaded, err := store.GenerationForDelivery("workspace-1", "run-1", generation.Digest)
	if err != nil || loaded.Digest != generation.Digest {
		t.Fatalf("GenerationForDelivery() = %#v, %v", loaded, err)
	}
	if _, err := store.GenerationForDelivery("workspace-1", "run-1", "sha256:caller-mismatch"); !errors.Is(err, ErrDeliveryBindingConflict) {
		t.Fatalf("GenerationForDelivery(mismatch) error = %v, want ErrDeliveryBindingConflict", err)
	}
}

func TestGenerationGCPrunesOnlyUnreferencedTerminalDeliveries(t *testing.T) {
	t.Parallel()

	store, _ := NewOwnershipStore(t.TempDir())
	current := safeGenerationFixture("sha256:current")
	live := safeGenerationFixture("sha256:live")
	terminal := safeGenerationFixture("sha256:terminal")
	journal := RoutingJournal{
		SchemaVersion: 1, CurrentGeneration: current.Digest,
		Generations:      map[string]RoutingGeneration{current.Digest: current, live.Digest: live, terminal.Digest: terminal},
		DeliveryBindings: map[string]string{"run-live": live.Digest, "run-terminal": terminal.Digest},
	}
	if err := store.Save("workspace-1", journal); err != nil {
		t.Fatalf("Save() error = %v", err)
	}
	if err := store.Prune("workspace-1", map[string]DeliveryLiveness{"run-live": DeliveryLive, "run-terminal": DeliveryTerminal}); err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	loaded, _, _ := store.Load("workspace-1")
	if _, exists := loaded.Generations[current.Digest]; !exists {
		t.Fatal("Prune() removed current generation")
	}
	if _, exists := loaded.Generations[live.Digest]; !exists {
		t.Fatal("Prune() removed live referenced generation")
	}
	if _, exists := loaded.Generations[terminal.Digest]; exists || loaded.DeliveryBindings["run-terminal"] != "" {
		t.Fatalf("Prune() retained terminal unneeded generation/binding: %#v", loaded)
	}
	before, _ := os.ReadFile(store.pathFor("workspace-1"))
	if err := store.Prune("workspace-1", map[string]DeliveryLiveness{"run-live": DeliveryUnknown}); !errors.Is(err, ErrDeliveryLivenessUnknown) {
		t.Fatalf("Prune(unknown) error = %v, want ErrDeliveryLivenessUnknown", err)
	}
	after, _ := os.ReadFile(store.pathFor("workspace-1"))
	if string(before) != string(after) {
		t.Fatal("Prune(unknown) mutated journal")
	}
}

func safeGenerationFixture(digest string) RoutingGeneration {
	return RoutingGeneration{
		SchemaVersion: 1, PolicyVersion: "test-v1", WorkspaceIdentityDigest: "sha256:workspace",
		TaskSetDigest: "sha256:tasks", InventoryDigest: "sha256:inventory", CatalogGeneration: "sha256:catalog",
		Digest: digest,
	}
}
