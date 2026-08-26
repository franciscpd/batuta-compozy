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
	writeTask(t, root, "demo", "task_02.md", "completed", "frontend", "high", []string{"task_01"}, "frontend body")
	writeTask(t, root, "demo", "task_01.md", "completed", "backend", "low", nil, "backend body")
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
	payload := []byte("---\nstatus: pending\ntitle: Missing complexity\ntype: backend\n---\nbody\n")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("rewrite task: %v", err)
	}
	loader, _ := NewArtifactLoader(root)
	if _, err := loader.Load("demo"); !errors.Is(err, ErrReauthoringRequired) {
		t.Fatalf("Load(missing complexity) error = %v, want ErrReauthoringRequired", err)
	}
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
