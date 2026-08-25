package publication

import (
	"os"
	"path/filepath"
	"testing"
)

func TestExecutableResolverReturnsAnAbsolutePath(t *testing.T) {
	directory := t.TempDir()
	executable := filepath.Join(directory, "controlled-tool")
	if err := os.WriteFile(executable, []byte("test executable"), 0o755); err != nil {
		t.Fatalf("write executable: %v", err)
	}
	t.Setenv("PATH", directory)

	got, err := (ExecutableResolver{}).Resolve("controlled-tool")
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if got != executable {
		t.Fatalf("Resolve() = %q, want %q", got, executable)
	}
}

func TestExecutableResolverRejectsMissingExecutable(t *testing.T) {
	t.Setenv("PATH", t.TempDir())
	if _, err := (ExecutableResolver{}).Resolve("definitely-missing-tool"); err == nil {
		t.Fatal("Resolve() error = nil, want missing executable error")
	}
}

func TestExecutableResolverRejectsBlankName(t *testing.T) {
	if _, err := (ExecutableResolver{}).Resolve(" \t\n"); err == nil {
		t.Fatal("Resolve() error = nil, want blank-name error")
	}
}
