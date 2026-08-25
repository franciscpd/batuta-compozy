package publication

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGitClientSnapshotReturnsExactCleanBranchAndHead(t *testing.T) {
	t.Parallel()

	repository := newTestRepository(t)
	wantHead := repository.head(t)
	client := GitClient{Executable: repository.git, Runner: ExecRunner{}}

	got, err := client.Snapshot(context.Background(), repository.path)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got.HeadSHA != wantHead {
		t.Fatalf("HeadSHA = %q, want %q", got.HeadSHA, wantHead)
	}
	if got.Branch != "main" {
		t.Fatalf("Branch = %q, want main", got.Branch)
	}
	if !got.Clean {
		t.Fatal("Clean = false, want true")
	}
	if got.Detached {
		t.Fatal("Detached = true, want false")
	}
}

func TestGitClientSnapshotReportsDirtyAndDetached(t *testing.T) {
	t.Parallel()

	repository := newTestRepository(t)
	repository.run(t, "checkout", "--detach")
	if err := os.WriteFile(filepath.Join(repository.path, "change.txt"), []byte("dirty\n"), 0o644); err != nil {
		t.Fatalf("write dirty file: %v", err)
	}

	got, err := (GitClient{Executable: repository.git, Runner: ExecRunner{}}).Snapshot(context.Background(), repository.path)
	if err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if got.Clean {
		t.Fatal("Clean = true, want false")
	}
	if !got.Detached {
		t.Fatal("Detached = false, want true")
	}
	if got.Branch != "" {
		t.Fatalf("Branch = %q, want empty for detached HEAD", got.Branch)
	}
}

func TestGitClientUpstreamHeadReturnsTheRemoteTrackingSHA(t *testing.T) {
	t.Parallel()

	repository := newTestRepository(t)
	remote := filepath.Join(t.TempDir(), "remote.git")
	runCommand(t, repository.git, "", "init", "--bare", remote)
	repository.run(t, "remote", "add", "origin", remote)
	repository.run(t, "push", "--set-upstream", "origin", "main")
	want := repository.head(t)

	if err := os.WriteFile(filepath.Join(repository.path, "second.txt"), []byte("local only\n"), 0o644); err != nil {
		t.Fatalf("write second file: %v", err)
	}
	repository.run(t, "add", "second.txt")
	repository.run(t, "commit", "-m", "second")

	got, err := (GitClient{Executable: repository.git, Runner: ExecRunner{}}).UpstreamHead(context.Background(), repository.path)
	if err != nil {
		t.Fatalf("UpstreamHead() error = %v", err)
	}
	if got != want {
		t.Fatalf("UpstreamHead() = %q, want %q", got, want)
	}
}

func TestGitClientCommitsAheadOfBaseUsesExactValidatedRevision(t *testing.T) {
	t.Parallel()

	runner := &recordingCommandRunner{results: []CommandResult{{Stdout: []byte("2\n")}}}
	client := GitClient{Executable: "/controlled/git", Runner: runner}
	got, err := client.CommitsAheadOfBase(context.Background(), "/trusted/worktree", "main")
	if err != nil {
		t.Fatalf("CommitsAheadOfBase() error = %v", err)
	}
	if got != 2 {
		t.Fatalf("CommitsAheadOfBase() = %d, want 2", got)
	}
	want := []Command{{
		Executable: "/controlled/git",
		Args:       []string{"rev-list", "--count", "main..HEAD"},
		Directory:  "/trusted/worktree",
	}}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, want)
	}

	for _, base := range []string{"", "--help", "main..other", "main@{upstream}", "main branch"} {
		if _, err := client.CommitsAheadOfBase(context.Background(), "/trusted/worktree", base); err == nil {
			t.Fatalf("CommitsAheadOfBase(base=%q) error = nil, want validation error", base)
		}
	}
}

func TestGitClientRejectsRelativeWorktreePath(t *testing.T) {
	t.Parallel()

	client := GitClient{Executable: filepath.Join(string(filepath.Separator), "controlled", "git"), Runner: ExecRunner{}}
	if _, err := client.Snapshot(context.Background(), "relative/worktree"); err == nil {
		t.Fatal("Snapshot() error = nil, want relative-path rejection")
	}
	if _, err := client.UpstreamHead(context.Background(), "relative/worktree"); err == nil {
		t.Fatal("UpstreamHead() error = nil, want relative-path rejection")
	}
}

func TestGitClientUsesExactExecutableArgvAndDirectory(t *testing.T) {
	t.Parallel()

	runner := &recordingCommandRunner{results: []CommandResult{
		{},
		{Stdout: []byte("0123456789abcdef0123456789abcdef01234567\n")},
		{Stdout: []byte("delivery\n")},
		{Stdout: []byte("89abcdef0123456789abcdef0123456789abcdef\n")},
	}}
	client := GitClient{Executable: "/controlled/git", Runner: runner}
	if _, err := client.Snapshot(context.Background(), "/trusted/worktree"); err != nil {
		t.Fatalf("Snapshot() error = %v", err)
	}
	if _, err := client.UpstreamHead(context.Background(), "/trusted/worktree"); err != nil {
		t.Fatalf("UpstreamHead() error = %v", err)
	}

	want := []Command{
		{Executable: "/controlled/git", Args: []string{"status", "--porcelain=v1"}, Directory: "/trusted/worktree"},
		{Executable: "/controlled/git", Args: []string{"rev-parse", "HEAD"}, Directory: "/trusted/worktree"},
		{Executable: "/controlled/git", Args: []string{"symbolic-ref", "--quiet", "--short", "HEAD"}, Directory: "/trusted/worktree"},
		{Executable: "/controlled/git", Args: []string{"rev-parse", "@{upstream}"}, Directory: "/trusted/worktree"},
	}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, want)
	}
}

func TestGitClientRejectsMalformedSHAs(t *testing.T) {
	t.Parallel()

	malformed := []string{
		"0123456789abcdef0123456789abcdef0123456",
		"0123456789ABCDEF0123456789ABCDEF01234567",
		"0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef0",
		"not-a-sha",
	}
	for _, value := range malformed {
		t.Run(value, func(t *testing.T) {
			t.Parallel()
			snapshotRunner := &recordingCommandRunner{results: []CommandResult{{}, {Stdout: []byte(value + "\n")}}}
			client := GitClient{Executable: "/controlled/git", Runner: snapshotRunner}
			if _, err := client.Snapshot(context.Background(), "/trusted/worktree"); err == nil {
				t.Fatal("Snapshot() error = nil, want malformed-SHA error")
			}

			upstreamRunner := &recordingCommandRunner{results: []CommandResult{{Stdout: []byte(value + "\n")}}}
			client.Runner = upstreamRunner
			if _, err := client.UpstreamHead(context.Background(), "/trusted/worktree"); err == nil {
				t.Fatal("UpstreamHead() error = nil, want malformed-SHA error")
			}
		})
	}
}

type recordingCommandRunner struct {
	commands []Command
	results  []CommandResult
}

func (r *recordingCommandRunner) Run(_ context.Context, command Command) (CommandResult, error) {
	r.commands = append(r.commands, command)
	if len(r.results) == 0 {
		return CommandResult{}, errors.New("unexpected command")
	}
	result := r.results[0]
	r.results = r.results[1:]
	return result, nil
}

type testRepository struct {
	git  string
	path string
}

func newTestRepository(t *testing.T) testRepository {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("resolve git for test fixture: %v", err)
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatalf("absolute git path: %v", err)
	}
	repository := testRepository{git: git, path: filepath.Join(t.TempDir(), "repository with spaces")}
	runCommand(t, git, "", "init", "--initial-branch=main", repository.path)
	repository.run(t, "config", "user.email", "batuta@example.invalid")
	repository.run(t, "config", "user.name", "Batuta Test")
	if err := os.WriteFile(filepath.Join(repository.path, "tracked.txt"), []byte("initial\n"), 0o644); err != nil {
		t.Fatalf("write tracked file: %v", err)
	}
	repository.run(t, "add", "tracked.txt")
	repository.run(t, "commit", "-m", "initial")
	return repository
}

func (r testRepository) run(t *testing.T, args ...string) string {
	t.Helper()
	return runCommand(t, r.git, r.path, args...)
}

func (r testRepository) head(t *testing.T) string {
	t.Helper()
	return strings.TrimSpace(r.run(t, "rev-parse", "HEAD"))
}

func runCommand(t *testing.T, executable, directory string, args ...string) string {
	t.Helper()
	command := exec.Command(executable, args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("run %s %#v: %v: %s", executable, args, err, output)
	}
	return string(output)
}
