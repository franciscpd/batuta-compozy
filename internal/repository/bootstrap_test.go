package repository

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

func TestBootstrapInitializesSafeWorkspaceAndReplaysWithoutAnotherCommit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeBootstrapFile(t, root, ".gitignore", ".env\n")
	writeBootstrapFile(t, root, ".env", "SECRET=not-committed\n")
	writeBootstrapFile(t, root, "README.md", "# Demo\n")
	writeBootstrapFile(t, root, ".compozy/tasks/demo/task_01.md", "---\nstatus: pending\n---\n")
	bootstrapper := realBootstrapper(t)

	first, err := bootstrapper.Bootstrap(context.Background(), root)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if first.State != BootstrapInitialized || first.Branch != "main" || !gitSHA.MatchString(first.HeadSHA) || first.CommittedFiles != 3 {
		t.Fatalf("Bootstrap() = %#v", first)
	}
	if first.CommitMessage != "chore: initialize workspace" || len(first.BlockedPaths) != 0 {
		t.Fatalf("Bootstrap() metadata = %#v", first)
	}
	if got := runGit(t, root, "status", "--porcelain=v1", "--untracked-files=all"); got != "" {
		t.Fatalf("git status = %q, want ignored secret and clean workspace", got)
	}
	if got := runGit(t, root, "show", "--format=%s", "--no-patch", "HEAD"); got != "chore: initialize workspace" {
		t.Fatalf("initial commit subject = %q", got)
	}
	if got := runGit(t, root, "ls-files"); strings.Contains(got, ".env\n") || !strings.Contains(got, "README.md") || !strings.Contains(got, ".compozy/tasks/demo/task_01.md") {
		t.Fatalf("tracked files = %q", got)
	}

	replay, err := bootstrapper.Bootstrap(context.Background(), root)
	if err != nil || replay.State != BootstrapAlreadyInitialized || replay.HeadSHA != first.HeadSHA || replay.CommittedFiles != 0 {
		t.Fatalf("Bootstrap(replay) = %#v, error = %v", replay, err)
	}
	if got := runGit(t, root, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("commit count after replay = %q", got)
	}
}

func TestBootstrapBlocksUnignoredSensitivePathsAndRollsBackOwnedRepository(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeBootstrapFile(t, root, "README.md", "# Demo\n")
	writeBootstrapFile(t, root, ".env.production", "TOKEN=secret\n")
	writeBootstrapFile(t, root, "config/private.pem", "private-key\n")

	result, err := realBootstrapper(t).Bootstrap(context.Background(), root)
	if err != nil {
		t.Fatalf("Bootstrap() error = %v", err)
	}
	if result.State != BootstrapBlocked || result.HeadSHA != "" || result.Branch != "" || result.CommittedFiles != 0 {
		t.Fatalf("Bootstrap() = %#v", result)
	}
	want := []string{".env.production", "config/private.pem"}
	if strings.Join(result.BlockedPaths, "\n") != strings.Join(want, "\n") {
		t.Fatalf("blocked paths = %#v, want %#v", result.BlockedPaths, want)
	}
	if _, err := os.Lstat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatalf("owned .git remains after blocked bootstrap: %v", err)
	}
	if body, err := os.ReadFile(filepath.Join(root, ".env.production")); err != nil || string(body) != "TOKEN=secret\n" {
		t.Fatalf("workspace file changed: %q, error %v", body, err)
	}
}

func TestBootstrapPreservesExistingEmptyRepositoryWhenSensitivePathBlocksCommit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runGit(t, root, "init", "--initial-branch=main", ".")
	writeBootstrapFile(t, root, ".npmrc", "//registry.example/:_authToken=secret\n")

	result, err := realBootstrapper(t).Bootstrap(context.Background(), root)
	if err != nil || result.State != BootstrapBlocked || strings.Join(result.BlockedPaths, "") != ".npmrc" {
		t.Fatalf("Bootstrap(existing empty) = %#v, error %v", result, err)
	}
	if _, err := os.Stat(filepath.Join(root, ".git")); err != nil {
		t.Fatalf("existing .git was removed: %v", err)
	}
	if got := runGitAllowFailure(t, root, "rev-parse", "--verify", "HEAD"); got.exitCode == 0 {
		t.Fatalf("blocked existing repository gained a commit: %q", got.stdout)
	}
}

func TestBootstrapRechecksTheExactStagedSetBeforeCommit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeBootstrapFile(t, root, "README.md", "# Demo\n")
	bootstrapper := realBootstrapper(t)
	delegate := bootstrapper.Runner
	bootstrapper.Runner = commandRunnerFunc(func(ctx context.Context, command publication.Command) (publication.CommandResult, error) {
		if slices.Equal(command.Args, []string{"add", "--all", "--", "."}) {
			writeBootstrapFile(t, root, ".env.raced", "TOKEN=secret\n")
		}
		return delegate.Run(ctx, command)
	})

	result, err := bootstrapper.Bootstrap(context.Background(), root)
	if err != nil || result.State != BootstrapBlocked || strings.Join(result.BlockedPaths, "") != ".env.raced" {
		t.Fatalf("Bootstrap(raced secret) = %#v, error %v", result, err)
	}
	if _, err := os.Lstat(filepath.Join(root, ".git")); !os.IsNotExist(err) {
		t.Fatalf("owned .git remains after raced secret: %v", err)
	}
}

func TestBootstrapRejectsExistingHeadlessRepositoryOnNonMainBranch(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runGit(t, root, "init", "--initial-branch=master", ".")
	writeBootstrapFile(t, root, "README.md", "# Demo\n")

	_, err := realBootstrapper(t).Bootstrap(context.Background(), root)
	if err == nil || !strings.Contains(err.Error(), "must use branch main") {
		t.Fatalf("Bootstrap(headless master) error = %v", err)
	}
	if got := runGitAllowFailure(t, root, "rev-parse", "--verify", "HEAD"); got.exitCode == 0 {
		t.Fatalf("headless master repository gained commit: %q", got.stdout)
	}
}

func TestBootstrapDoesNotDestroyCommittedRepositoryWhenConcurrentFileAppearsAfterCommit(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeBootstrapFile(t, root, "README.md", "# Demo\n")
	bootstrapper := realBootstrapper(t)
	delegate := bootstrapper.Runner
	bootstrapper.Runner = commandRunnerFunc(func(ctx context.Context, command publication.Command) (publication.CommandResult, error) {
		result, err := delegate.Run(ctx, command)
		if slices.Contains(command.Args, "commit") && err == nil {
			writeBootstrapFile(t, root, "AFTER.md", "concurrent\n")
		}
		return result, err
	})

	result, err := bootstrapper.Bootstrap(context.Background(), root)
	if err != nil || result.State != BootstrapInitialized || !gitSHA.MatchString(result.HeadSHA) {
		t.Fatalf("Bootstrap(concurrent post-commit file) = %#v, error %v", result, err)
	}
	if got := runGit(t, root, "rev-list", "--count", "HEAD"); got != "1" {
		t.Fatalf("commit count = %q", got)
	}
	if got := runGit(t, root, "status", "--porcelain=v1", "--untracked-files=all"); !strings.Contains(got, "AFTER.md") {
		t.Fatalf("concurrent file was lost: %q", got)
	}
}

func TestSensitivePathRecognizesLegacySSHPrivateKeys(t *testing.T) {
	t.Parallel()

	for _, path := range []string{"id_ecdsa", "config/id_dsa", "id_ecdsa_sk", "keys/id_ed25519_sk"} {
		if !sensitivePath(path) {
			t.Fatalf("sensitivePath(%q) = false", path)
		}
	}
}

func TestBootstrapDisablesHooksThatCouldChangeTheInspectedIndex(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runGit(t, root, "init", "--initial-branch=main", ".")
	writeBootstrapFile(t, root, "README.md", "# Demo\n")
	hook := filepath.Join(root, ".git", "hooks", "prepare-commit-msg")
	if err := os.WriteFile(hook, []byte("#!/bin/sh\nprintf 'SECRET=hooked\\n' > .env.hooked\ngit add .env.hooked\n"), 0o700); err != nil {
		t.Fatalf("write prepare-commit-msg hook: %v", err)
	}

	result, err := realBootstrapper(t).Bootstrap(context.Background(), root)
	if err != nil || result.State != BootstrapInitialized {
		t.Fatalf("Bootstrap(with hook) = %#v, error %v", result, err)
	}
	if got := runGit(t, root, "ls-tree", "-r", "--name-only", "HEAD"); strings.Contains(got, ".env.hooked") {
		t.Fatalf("hook injected uninspected secret into commit: %q", got)
	}
	if _, err := os.Stat(filepath.Join(root, ".env.hooked")); !os.IsNotExist(err) {
		t.Fatalf("prepare-commit-msg hook unexpectedly ran: %v", err)
	}
}

func TestBootstrapTreatsDetachedValidHeadAsInitialized(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	runGit(t, root, "init", "--initial-branch=main", ".")
	writeBootstrapFile(t, root, "README.md", "# Demo\n")
	runGit(t, root, "add", "README.md")
	runGit(t, root, "-c", "user.name=Test", "-c", "user.email=test@example.invalid", "commit", "-m", "chore: seed")
	head := runGit(t, root, "rev-parse", "HEAD")
	runGit(t, root, "checkout", "--detach", head)

	result, err := realBootstrapper(t).Bootstrap(context.Background(), root)
	if err != nil || result.State != BootstrapAlreadyInitialized || result.HeadSHA != head || result.Branch != "" {
		t.Fatalf("Bootstrap(detached HEAD) = %#v, error %v", result, err)
	}
}

func TestBlockedPathEvidenceIsBoundedForThePublicSchema(t *testing.T) {
	t.Parallel()

	paths := make([]string, bootstrapBlockedPathLimit+1)
	for i := range paths {
		paths[i] = filepath.ToSlash(filepath.Join("secrets", fmt.Sprintf("%05d.pem", i)))
	}
	blocked := boundedBlockedPaths(paths)
	if len(blocked) != bootstrapBlockedPathLimit || blocked[0] != paths[0] || blocked[len(blocked)-1] != paths[bootstrapBlockedPathLimit-1] {
		t.Fatalf("boundedBlockedPaths() len=%d first=%q last=%q", len(blocked), blocked[0], blocked[len(blocked)-1])
	}
}

func realBootstrapper(t *testing.T) Bootstrapper {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is unavailable")
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatalf("absolute git path: %v", err)
	}
	return Bootstrapper{GitExecutable: filepath.Clean(git), Runner: publication.ExecRunner{}}
}

func writeBootstrapFile(t *testing.T, root, relative, body string) {
	t.Helper()
	path := filepath.Join(root, filepath.FromSlash(relative))
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", relative, err)
	}
	if err := os.WriteFile(path, []byte(body), 0o600); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

type gitResult struct {
	stdout   string
	exitCode int
}

type commandRunnerFunc func(context.Context, publication.Command) (publication.CommandResult, error)

func (f commandRunnerFunc) Run(ctx context.Context, command publication.Command) (publication.CommandResult, error) {
	return f(ctx, command)
}

func runGit(t *testing.T, root string, args ...string) string {
	t.Helper()
	result := runGitAllowFailure(t, root, args...)
	if result.exitCode != 0 {
		t.Fatalf("git %v exited %d", args, result.exitCode)
	}
	return result.stdout
}

func runGitAllowFailure(t *testing.T, root string, args ...string) gitResult {
	t.Helper()
	command := exec.Command("git", args...)
	command.Dir = root
	output, err := command.Output()
	if err == nil {
		return gitResult{stdout: strings.TrimSpace(string(output))}
	}
	if exitErr, ok := err.(*exec.ExitError); ok {
		return gitResult{stdout: strings.TrimSpace(string(output)), exitCode: exitErr.ExitCode()}
	}
	t.Fatalf("git %v: %v", args, err)
	return gitResult{}
}
