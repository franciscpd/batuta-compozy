package publication

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestGitClientWorktreeStateUsesExactBoundedCommands(t *testing.T) {
	t.Parallel()

	head := "0123456789abcdef0123456789abcdef01234567"
	status := []byte("?? pending.txt\x00")
	diff := []byte("diff --git a/tracked.txt b/tracked.txt\n")
	runner := &recordingCommandRunner{results: []CommandResult{
		{Stdout: []byte(head + "\n")}, {Stdout: status}, {Stdout: diff}, {Stdout: nil},
	}}
	client := GitClient{Executable: "/controlled/git", Runner: runner}
	got, err := client.WorktreeState(context.Background(), "/trusted/worktree")
	if err != nil {
		t.Fatalf("WorktreeState() error = %v", err)
	}
	if got.HeadSHA != head || got.PorcelainSHA256 != prefixedSHA256(status) || got.ContentSHA256 != prefixedSHA256(diff) {
		t.Fatalf("WorktreeState() = %#v", got)
	}
	want := []Command{
		{Executable: "/controlled/git", Args: []string{"rev-parse", "HEAD"}, Directory: "/trusted/worktree", StdoutLimit: 16 << 20, StderrLimit: 64 << 10},
		{Executable: "/controlled/git", Args: []string{"status", "--porcelain=v1", "-z", "--untracked-files=all"}, Directory: "/trusted/worktree", StdoutLimit: 16 << 20, StderrLimit: 64 << 10},
		{Executable: "/controlled/git", Args: []string{"diff", "--binary", "--no-ext-diff", "HEAD"}, Directory: "/trusted/worktree", StdoutLimit: 16 << 20, StderrLimit: 64 << 10},
		{Executable: "/controlled/git", Args: []string{"ls-files", "--others", "--exclude-standard", "-z"}, Directory: "/trusted/worktree", StdoutLimit: 16 << 20, StderrLimit: 64 << 10},
	}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, want)
	}
}

func TestGitClientWorktreeStateDetectsUntrackedContentChanges(t *testing.T) {
	t.Parallel()

	repository := newTestRepository(t)
	untracked := filepath.Join(repository.path, "pending.txt")
	if err := os.WriteFile(untracked, []byte("first\n"), 0o644); err != nil {
		t.Fatalf("write first untracked content: %v", err)
	}
	client := GitClient{Executable: repository.git, Runner: ExecRunner{}}
	first, err := client.WorktreeState(context.Background(), repository.path)
	if err != nil {
		t.Fatalf("WorktreeState(first) error = %v", err)
	}
	if err := os.WriteFile(untracked, []byte("second\n"), 0o644); err != nil {
		t.Fatalf("write second untracked content: %v", err)
	}
	second, err := client.WorktreeState(context.Background(), repository.path)
	if err != nil {
		t.Fatalf("WorktreeState(second) error = %v", err)
	}
	if first.HeadSHA != second.HeadSHA || first.PorcelainSHA256 != second.PorcelainSHA256 || first.ContentSHA256 == second.ContentSHA256 {
		t.Fatalf("states = first:%#v second:%#v", first, second)
	}
	third, err := client.WorktreeState(context.Background(), repository.path)
	if err != nil || third != second {
		t.Fatalf("WorktreeState(stable replay) = %#v, error:%v, want %#v", third, err, second)
	}
}

func TestGitClientWorktreeStateHashesSymlinkTargetWithoutFollowing(t *testing.T) {
	t.Parallel()

	repository := newTestRepository(t)
	link := filepath.Join(repository.path, "pending-link")
	if err := os.Symlink("missing-first", link); err != nil {
		t.Fatalf("create first symlink: %v", err)
	}
	client := GitClient{Executable: repository.git, Runner: ExecRunner{}}
	first, err := client.WorktreeState(context.Background(), repository.path)
	if err != nil {
		t.Fatalf("WorktreeState(first link) error = %v", err)
	}
	if err := os.Remove(link); err != nil {
		t.Fatalf("remove first symlink: %v", err)
	}
	if err := os.Symlink("missing-second", link); err != nil {
		t.Fatalf("create second symlink: %v", err)
	}
	second, err := client.WorktreeState(context.Background(), repository.path)
	if err != nil {
		t.Fatalf("WorktreeState(second link) error = %v", err)
	}
	if first.PorcelainSHA256 != second.PorcelainSHA256 || first.ContentSHA256 == second.ContentSHA256 {
		t.Fatalf("symlink states = first:%#v second:%#v", first, second)
	}
}

func TestGitClientWorktreeStateRejectsUnsafeOrTruncatedEvidence(t *testing.T) {
	t.Parallel()

	head := []byte("0123456789abcdef0123456789abcdef01234567\n")
	tests := []struct {
		name    string
		results []CommandResult
	}{
		{name: "malformed head", results: []CommandResult{{Stdout: []byte("bad\n")}}},
		{name: "truncated diff", results: []CommandResult{{Stdout: head}, {}, {StdoutTruncated: true}}},
		{name: "path escape", results: []CommandResult{{Stdout: head}, {}, {}, {Stdout: []byte("../outside\x00")}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			client := GitClient{Executable: "/controlled/git", Runner: &recordingCommandRunner{results: test.results}}
			if _, err := client.WorktreeState(context.Background(), "/trusted/worktree"); err == nil {
				t.Fatal("WorktreeState() error = nil, want safe rejection")
			}
		})
	}
}

func prefixedSHA256(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

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
