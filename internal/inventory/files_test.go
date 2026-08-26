package inventory

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestFileReaderAllowsContainedFilesAndRejectsSymlinkEscapes(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(workspace, ".cursor", "rules"), 0o755); err != nil {
		t.Fatalf("mkdir rules: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, ".cursor", "rules", "safe.mdc"), []byte("safe"), 0o600); err != nil {
		t.Fatalf("write safe file: %v", err)
	}
	secretPath := filepath.Join(outside, "secret.mdc")
	if err := os.WriteFile(secretPath, []byte("must-not-be-read"), 0o600); err != nil {
		t.Fatalf("write outside file: %v", err)
	}
	if err := os.Symlink(secretPath, filepath.Join(workspace, ".cursor", "rules", "escape.mdc")); err != nil {
		t.Fatalf("symlink: %v", err)
	}

	reader, err := NewFileReader(workspace)
	if err != nil {
		t.Fatalf("NewFileReader() error = %v", err)
	}
	got, err := reader.ReadWorkspace(filepath.Join(".cursor", "rules", "safe.mdc"))
	if err != nil {
		t.Fatalf("ReadWorkspace(safe) error = %v", err)
	}
	if string(got) != "safe" {
		t.Fatalf("ReadWorkspace(safe) = %q, want safe", got)
	}
	_, err = reader.ReadWorkspace(filepath.Join(".cursor", "rules", "escape.mdc"))
	if !errors.Is(err, ErrFileOutsideTrustedRoot) {
		t.Fatalf("ReadWorkspace(escape) error = %v, want ErrFileOutsideTrustedRoot", err)
	}
	if _, err := reader.ReadWorkspace(filepath.Join("..", "outside")); !errors.Is(err, ErrFileOutsideTrustedRoot) {
		t.Fatalf("ReadWorkspace(traversal) error = %v, want ErrFileOutsideTrustedRoot", err)
	}
}

func TestFileReaderEnforcesSharedFileAndByteBudgets(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	if err := os.WriteFile(filepath.Join(workspace, "one"), []byte("12345"), 0o600); err != nil {
		t.Fatalf("write one: %v", err)
	}
	if err := os.WriteFile(filepath.Join(workspace, "two"), []byte("67890"), 0o600); err != nil {
		t.Fatalf("write two: %v", err)
	}
	reader, err := NewFileReaderWithBudget(workspace, FileBudget{MaxFiles: 1, MaxBytes: 8})
	if err != nil {
		t.Fatalf("NewFileReaderWithBudget() error = %v", err)
	}
	if _, err := reader.ReadWorkspace("one"); err != nil {
		t.Fatalf("ReadWorkspace(one) error = %v", err)
	}
	if _, err := reader.ReadWorkspace("two"); !errors.Is(err, ErrFileBudgetExceeded) {
		t.Fatalf("ReadWorkspace(two) error = %v, want ErrFileBudgetExceeded", err)
	}
}
