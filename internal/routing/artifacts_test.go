package routing

import (
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"
)

func TestArtifactLoaderUsesCanonicalSlugStableOrderAndEveryAuthoredByte(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTask(t, root, "demo", "task_02.md", "completed", "frontend", "high", nil, "frontend body")
	writeTask(t, root, "demo", "task_01.md", "completed", "backend", "low", nil, "backend body")
	writeTaskManifest(t, root, "demo", []string{"task_01", "task_02"}, [][2]string{{"task_01", "task_02"}})
	loader, err := NewArtifactLoader(root)
	if err != nil {
		t.Fatalf("NewArtifactLoader() error = %v", err)
	}
	first, err := loader.Load("demo")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got, want := []string{first.Tasks[0].ID, first.Tasks[1].ID}, []string{"task_01", "task_02"}; !slices.Equal(got, want) {
		t.Fatalf("task order = %#v, want %#v", got, want)
	}
	if first.Tasks[0].Status != "completed" || first.Tasks[1].Status != "completed" {
		t.Fatalf("completed tasks were filtered or rewritten: %#v", first.Tasks)
	}
	if got := first.Tasks[1].Dependencies; !slices.Equal(got, []string{"task_01"}) {
		t.Fatalf("task_02 dependencies = %#v, want task_01 from _tasks.md", got)
	}

	writeTask(t, root, "demo", "task_01.md", "completed", "backend", "low", nil, "backend body changed by one byte!")
	second, err := loader.Load("demo")
	if err != nil {
		t.Fatalf("Load(changed) error = %v", err)
	}
	if second.Digest == first.Digest {
		t.Fatalf("digest = %q after authored byte change, want different from %q", second.Digest, first.Digest)
	}
}

func TestArtifactLoaderRejectsInvalidSlugAndSymlinkEscape(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	loader, err := NewArtifactLoader(root)
	if err != nil {
		t.Fatalf("NewArtifactLoader() error = %v", err)
	}
	for _, slug := range []string{"", "../escape", "nested/path", "Uppercase", "double--dash"} {
		if _, err := loader.Load(slug); !errors.Is(err, ErrInvalidSlug) {
			t.Fatalf("Load(%q) error = %v, want ErrInvalidSlug", slug, err)
		}
	}

	outside := t.TempDir()
	writeTask(t, outside, "outside", "task_01.md", "pending", "backend", "low", nil, "secret outside body")
	writeTaskManifest(t, outside, "outside", []string{"task_01"}, nil)
	if err := os.MkdirAll(filepath.Join(root, ".compozy", "tasks"), 0o755); err != nil {
		t.Fatalf("mkdir tasks: %v", err)
	}
	if err := os.Symlink(filepath.Join(outside, ".compozy", "tasks", "outside"), filepath.Join(root, ".compozy", "tasks", "escape")); err != nil {
		t.Fatalf("symlink: %v", err)
	}
	if _, err := loader.Load("escape"); !errors.Is(err, ErrArtifactOutsideTrustedRoot) {
		t.Fatalf("Load(symlink escape) error = %v, want ErrArtifactOutsideTrustedRoot", err)
	}
}

func TestArtifactLoaderRequiresCanonicalDomainAndComplexity(t *testing.T) {
	t.Parallel()

	for _, taskType := range []string{"test", "refactor", "chore", "bugfix", "qa-report", "qa-execution", "unknown"} {
		root := t.TempDir()
		writeTask(t, root, "demo", "task_01.md", "pending", taskType, "low", nil, "body")
		writeTaskManifest(t, root, "demo", []string{"task_01"}, nil)
		loader, err := NewArtifactLoader(root)
		if err != nil {
			t.Fatalf("NewArtifactLoader() error = %v", err)
		}
		if _, err := loader.Load("demo"); !errors.Is(err, ErrReauthoringRequired) {
			t.Fatalf("Load(type=%q) error = %v, want ErrReauthoringRequired", taskType, err)
		}
	}

	root := t.TempDir()
	path := writeTask(t, root, "demo", "task_01.md", "pending", "backend", "low", nil, "body")
	writeTaskManifest(t, root, "demo", []string{"task_01"}, nil)
	payload := []byte("---\nstatus: pending\ntitle: Missing complexity\ntype: backend\n---\nbody\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("rewrite task: %v", err)
	}
	loader, _ := NewArtifactLoader(root)
	if _, err := loader.Load("demo"); !errors.Is(err, ErrReauthoringRequired) {
		t.Fatalf("Load(missing complexity) error = %v, want ErrReauthoringRequired", err)
	}
}

func TestArtifactLoaderUsesManifestTopologicalOrderAndCanonicalDigest(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTask(t, root, "demo", "task_01.md", "pending", "frontend", "medium", nil, "first")
	writeTask(t, root, "demo", "task_02.md", "pending", "backend", "low", nil, "second")
	writeTaskManifest(t, root, "demo", []string{"task_01", "task_02"}, [][2]string{{"task_02", "task_01"}})
	loader, _ := NewArtifactLoader(root)
	first, err := loader.Load("demo")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	if got, want := []string{first.Tasks[0].ID, first.Tasks[1].ID}, []string{"task_02", "task_01"}; !slices.Equal(got, want) {
		t.Fatalf("topological order = %#v, want %#v", got, want)
	}
	if got := first.Tasks[1].Dependencies; !slices.Equal(got, []string{"task_02"}) {
		t.Fatalf("task_01 dependencies = %#v, want task_02", got)
	}
	writeTaskManifest(t, root, "demo", []string{"task_02", "task_01"}, [][2]string{{"task_02", "task_01"}})
	second, err := loader.Load("demo")
	if err != nil {
		t.Fatalf("Load(reordered manifest) error = %v", err)
	}
	if second.Digest != first.Digest {
		t.Fatalf("canonical task-set digest changed for equivalent graph ordering: first=%q second=%q", first.Digest, second.Digest)
	}
}

func TestTaskSetDeliverySnapshotKeepsCompletedTasksAndStableItemIdentity(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTask(t, root, "demo", "task_01.md", "completed", "backend", "low", nil, "done")
	writeTask(t, root, "demo", "task_02.md", "pending", "frontend", "high", nil, "remaining")
	writeTaskManifest(t, root, "demo", []string{"task_01", "task_02"}, [][2]string{{"task_01", "task_02"}})
	loader, err := NewArtifactLoader(root)
	if err != nil {
		t.Fatalf("NewArtifactLoader() error = %v", err)
	}
	taskSet, err := loader.Load("demo")
	if err != nil {
		t.Fatalf("Load() error = %v", err)
	}
	snapshot, err := taskSet.DeliverySnapshot()
	if err != nil {
		t.Fatalf("DeliverySnapshot() error = %v", err)
	}
	if snapshot.Digest != taskSet.Digest || len(snapshot.Tasks) != 2 || snapshot.Tasks[0].Status != "completed" || snapshot.Tasks[1].Status != "pending" {
		t.Fatalf("snapshot = %#v", snapshot)
	}
	if !slices.Equal(snapshot.IncompleteTaskIDs, []string{"task_02"}) {
		t.Fatalf("incomplete task IDs = %#v, want task_02", snapshot.IncompleteTaskIDs)
	}
	if snapshot.ItemTaskIDs[0] != "task_01" || snapshot.ItemTaskIDs[1] != "task_02" {
		t.Fatalf("item task IDs = %#v, want stable authored order", snapshot.ItemTaskIDs)
	}
}

func TestArtifactLoaderRejectsUnsupportedAuthoredStatus(t *testing.T) {
	t.Parallel()

	for _, status := range []string{"", "done", "in_progress", "blocked", "failed"} {
		root := t.TempDir()
		writeTask(t, root, "demo", "task_01.md", status, "backend", "low", nil, "body")
		writeTaskManifest(t, root, "demo", []string{"task_01"}, nil)
		loader, err := NewArtifactLoader(root)
		if err != nil {
			t.Fatalf("NewArtifactLoader() error = %v", err)
		}
		if _, err := loader.Load("demo"); !errors.Is(err, ErrReauthoringRequired) {
			t.Fatalf("Load(status=%q) error = %v, want ErrReauthoringRequired", status, err)
		}
	}
}

func TestTaskSnapshotReconcileCarriesCompletedTaskAndStableItemMapping(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeTask(t, root, "demo", "task_01.md", "pending", "backend", "low", nil, "first")
	writeTask(t, root, "demo", "task_02.md", "pending", "frontend", "high", nil, "second")
	writeTaskManifest(t, root, "demo", []string{"task_01", "task_02"}, [][2]string{{"task_01", "task_02"}})
	loader, err := NewArtifactLoader(root)
	if err != nil {
		t.Fatalf("NewArtifactLoader() error = %v", err)
	}
	initialSet, err := loader.Load("demo")
	if err != nil {
		t.Fatalf("Load(initial) error = %v", err)
	}
	initial, err := initialSet.DeliverySnapshot()
	if err != nil {
		t.Fatalf("DeliverySnapshot(initial) error = %v", err)
	}
	writeTask(t, root, "demo", "task_01.md", "completed", "backend", "low", nil, "first completed with evidence")
	currentSet, err := loader.Load("demo")
	if err != nil {
		t.Fatalf("Load(current) error = %v", err)
	}
	current, err := currentSet.DeliverySnapshot()
	if err != nil {
		t.Fatalf("DeliverySnapshot(current) error = %v", err)
	}

	progress, err := initial.Reconcile(current)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !slices.Equal(progress.IncompleteTaskIDs, []string{"task_02"}) || progress.ItemTaskIDs[0] != "task_01" || progress.ItemTaskIDs[1] != "task_02" {
		t.Fatalf("progress = %#v", progress)
	}
	if initial.Tasks[0].Status != "pending" || initial.Tasks[0].Digest == current.Tasks[0].Digest {
		t.Fatalf("initial snapshot mutated or completion digest unchanged: initial=%#v current=%#v", initial.Tasks[0], current.Tasks[0])
	}
}

func TestTaskSnapshotReconcileRejectsIdentityDriftAndStatusRegression(t *testing.T) {
	t.Parallel()

	initial := validTaskSnapshotFixture(t)
	tests := []struct {
		name   string
		mutate func(*DeliveryTaskSnapshot)
	}{
		{name: "domain drift", mutate: func(snapshot *DeliveryTaskSnapshot) { snapshot.Tasks[0].Domain = DomainBackend }},
		{name: "dependency drift", mutate: func(snapshot *DeliveryTaskSnapshot) { snapshot.Tasks[0].Dependencies = []string{"task_99"} }},
		{name: "pending content drift", mutate: func(snapshot *DeliveryTaskSnapshot) { snapshot.Tasks[0].Digest = hexDigestFixture("changed-pending") }},
		{name: "completed regression", mutate: func(snapshot *DeliveryTaskSnapshot) {
			snapshot.Tasks[0].Status = "completed"
			completed := *snapshot
			completed.IncompleteTaskIDs = nil
			if _, err := completed.Reconcile(initial); err == nil {
				t.Fatal("Reconcile(completed to pending) error = nil")
			}
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := cloneTaskSnapshotForTest(initial)
			test.mutate(&current)
			if test.name == "completed regression" {
				return
			}
			if _, err := initial.Reconcile(current); !errors.Is(err, ErrReauthoringRequired) {
				t.Fatalf("Reconcile() error = %v, want ErrReauthoringRequired", err)
			}
		})
	}
}

func cloneTaskSnapshotForTest(snapshot DeliveryTaskSnapshot) DeliveryTaskSnapshot {
	cloned := snapshot
	cloned.Tasks = append([]DeliveryTaskSnapshotEntry(nil), snapshot.Tasks...)
	for index := range cloned.Tasks {
		cloned.Tasks[index].Dependencies = append([]string(nil), snapshot.Tasks[index].Dependencies...)
	}
	cloned.IncompleteTaskIDs = append([]string(nil), snapshot.IncompleteTaskIDs...)
	cloned.ItemTaskIDs = make(map[int]string, len(snapshot.ItemTaskIDs))
	for index, taskID := range snapshot.ItemTaskIDs {
		cloned.ItemTaskIDs[index] = taskID
	}
	return cloned
}

func writeTask(t *testing.T, root, slug, name, status, taskType, complexity string, dependencies []string, body string) string {
	t.Helper()
	directory := filepath.Join(root, ".compozy", "tasks", slug)
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("mkdir task dir: %v", err)
	}
	content := "---\nstatus: " + status + "\ntitle: Fixture task\ntype: " + taskType + "\ncomplexity: " + complexity + "\n"
	if len(dependencies) > 0 {
		content += "dependencies: ["
		for i, dependency := range dependencies {
			if i > 0 {
				content += ", "
			}
			content += dependency
		}
		content += "]\n"
	}
	content += "---\n\n# Fixture\n\n" + body + "\n"
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write task: %v", err)
	}
	return path
}

func writeTaskManifest(t *testing.T, root, slug string, nodes []string, edges [][2]string) {
	t.Helper()
	content := "---\nschema_version: \"compozy.tasks/v2\"\nworkflow: " + slug + "\ngraph:\n  nodes:\n"
	for _, node := range nodes {
		content += "    - id: " + node + "\n      file: " + node + ".md\n"
	}
	if len(edges) == 0 {
		content += "  edges: []\n"
	} else {
		content += "  edges:\n"
		for _, edge := range edges {
			content += "    - from: " + edge[0] + "\n      to: " + edge[1] + "\n"
		}
	}
	content += "---\n\n# Tasks\n"
	path := filepath.Join(root, ".compozy", "tasks", slug, "_tasks.md")
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write task manifest: %v", err)
	}
}
