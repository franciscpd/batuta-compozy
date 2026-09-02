package integration

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

func TestGitClientCandidateValidatesOneCommitAndTaskLocalTracking(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	candidateRoot, branch, commit := fixture.candidate(t, "task_01", "product-a.txt", "candidate a\n")
	trackingPath := filepath.Join(candidateRoot, ".compozy", "tasks", "demo", "task_01.md")
	writeIntegrationFile(t, trackingPath, "status: completed\n")
	verification := []byte(`{"checks":["go test ./..."],"status":"passed","task_id":"task_01"}`)
	runner := &delegatingRunner{delegate: publication.ExecRunner{}}
	client := GitClient{Executable: fixture.git, Runner: runner, scratchRootForTest: filepath.Join(t.TempDir(), "scratch")}

	evidence, err := client.Candidate(context.Background(), CandidateRequest{
		TaskID: "task_01", Slug: "demo", WorktreeRoot: candidateRoot,
		RepositoryRoot: fixture.root, ExpectedBranch: branch, BaseSHA: fixture.base,
		Verification: verification, VerificationDigest: integrationDigest(verification),
		AllowedTrackingPaths: []string{".compozy/tasks/demo/task_01.md"},
	})
	if err != nil {
		t.Fatalf("Candidate() error = %v", err)
	}
	if evidence.TaskID != "task_01" || evidence.CommitSHA != commit || evidence.BaseSHA != fixture.base ||
		evidence.TreeSHA == "" || evidence.RepositoryIdentity == "" ||
		!reflect.DeepEqual(evidence.OwnedTrackingPaths, []string{".compozy/tasks/demo/task_01.md"}) ||
		!reflect.DeepEqual(evidence.Tracking, []TrackingFile{{
			Path: ".compozy/tasks/demo/task_01.md", Digest: integrationDigest([]byte("status: completed\n")),
		}}) {
		t.Fatalf("Candidate() = %#v", evidence)
	}
	wantArgs := [][]string{
		{"rev-parse", "--show-toplevel"},
		{"rev-parse", "--git-common-dir"},
		{"rev-parse", "--git-common-dir"},
		{"rev-parse", "HEAD"},
		{"symbolic-ref", "--quiet", "--short", "HEAD"},
		{"rev-list", "--reverse", "--ancestry-path", fixture.base + "..HEAD"},
		{"rev-list", "--parents", "-n", "1", commit},
		{"show", "-s", "--format=%s", commit},
		{"rev-parse", commit + "^{tree}"},
		{"diff-tree", "--no-commit-id", "--name-only", "-r", "-z", commit},
		{"diff", "--name-only", "-z", "HEAD"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
		{"ls-files", "--others", "--ignored", "--exclude-standard", "-z", "--", ".compozy/"},
	}
	if len(runner.commands) != len(wantArgs) {
		t.Fatalf("command count = %d, want %d: %#v", len(runner.commands), len(wantArgs), runner.commands)
	}
	for index, command := range runner.commands {
		wantDirectory := candidateRoot
		if index == 2 {
			wantDirectory = fixture.root
		}
		if !reflect.DeepEqual(command.Args, wantArgs[index]) || command.Directory != wantDirectory ||
			command.Executable != fixture.git || command.StdoutLimit != GitStdoutLimit || command.StderrLimit != GitStderrLimit {
			t.Fatalf("command[%d] = %#v, want args %#v directory %q", index, command, wantArgs[index], wantDirectory)
		}
	}
}

func TestGitClientCandidateRequiresConventionalCommitSubject(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		subject string
		wantErr bool
	}{
		{name: "simple", subject: "feat: implement task"},
		{name: "scope", subject: "fix(parser): preserve task"},
		{name: "breaking", subject: "refactor(graph)!: replace scheduler"},
		{name: "missing type", subject: "implement task", wantErr: true},
		{name: "uppercase type", subject: "Feat: implement task", wantErr: true},
		{name: "missing separator", subject: "feat implement task", wantErr: true},
		{name: "empty description", subject: "feat:", wantErr: true},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newIntegrationGitFixture(t)
			root, branch, _ := fixture.candidate(t, "task_01", "product.txt", "candidate\n")
			fixture.run(t, root, "commit", "--amend", "-m", test.subject)
			verification := []byte(`{"status":"passed","task_id":"task_01"}`)
			_, err := (GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}}).Candidate(context.Background(), CandidateRequest{
				TaskID: "task_01", Slug: "demo", WorktreeRoot: root, RepositoryRoot: fixture.root,
				ExpectedBranch: branch, BaseSHA: fixture.base,
				Verification: verification, VerificationDigest: integrationDigest(verification),
			})
			if test.wantErr && !errors.Is(err, ErrInvalidCandidate) {
				t.Fatalf("Candidate(%q) error = %v, want ErrInvalidCandidate", test.subject, err)
			}
			if !test.wantErr && err != nil {
				t.Fatalf("Candidate(%q) error = %v", test.subject, err)
			}
		})
	}
}

func TestGitClientCandidateValidatesCommittedTrackingFromGitObject(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name    string
		prepare func(*testing.T, string, string)
	}{
		{
			name: "symlink",
			prepare: func(t *testing.T, root, path string) {
				t.Helper()
				if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
					t.Fatalf("MkdirAll() error = %v", err)
				}
				if err := os.Symlink("outside", path); err != nil {
					t.Fatalf("Symlink() error = %v", err)
				}
			},
		},
		{
			name: "oversized blob",
			prepare: func(t *testing.T, root, path string) {
				t.Helper()
				writeIntegrationFile(t, path, strings.Repeat("x", VerificationLimit+1))
			},
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newIntegrationGitFixture(t)
			branch := "batuta/task/task_01"
			root := filepath.Join(t.TempDir(), "task_01")
			fixture.run(t, fixture.root, "worktree", "add", "-b", branch, root, fixture.base)
			writeIntegrationFile(t, filepath.Join(root, "product.txt"), "candidate\n")
			tracking := filepath.Join(root, ".compozy", "tasks", "demo", "task_01.md")
			test.prepare(t, root, tracking)
			fixture.run(t, root, "add", "product.txt", ".compozy/tasks/demo/task_01.md")
			fixture.run(t, root, "commit", "-m", "feat: implement task_01")
			if err := os.Remove(tracking); err != nil {
				t.Fatalf("Remove(tracking) error = %v", err)
			}
			writeIntegrationFile(t, tracking, "small residual\n")
			verification := []byte(`{"status":"passed","task_id":"task_01"}`)
			_, err := (GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}}).Candidate(context.Background(), CandidateRequest{
				TaskID: "task_01", Slug: "demo", WorktreeRoot: root, RepositoryRoot: fixture.root,
				ExpectedBranch: branch, BaseSHA: fixture.base,
				Verification: verification, VerificationDigest: integrationDigest(verification),
				AllowedTrackingPaths: []string{".compozy/tasks/demo/task_01.md"},
			})
			if !errors.Is(err, ErrInvalidCandidate) {
				t.Fatalf("Candidate() error = %v, want ErrInvalidCandidate", err)
			}
		})
	}
}

func TestGitClientCandidateRejectsCommittedSharedTracking(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	root, branch, _ := fixture.candidate(t, "task_01", ".compozy/shared.json", "{}\n")
	verification := []byte(`{"status":"passed","task_id":"task_01"}`)
	_, err := (GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}}).Candidate(context.Background(), CandidateRequest{
		TaskID: "task_01", Slug: "demo", WorktreeRoot: root, RepositoryRoot: fixture.root,
		ExpectedBranch: branch, BaseSHA: fixture.base,
		Verification: verification, VerificationDigest: integrationDigest(verification),
	})
	if !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("Candidate() error = %v, want ErrInvalidCandidate", err)
	}
}

func TestGitClientCandidateAcceptsOnlyExplicitCommittedTaskTracking(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	path := ".compozy/tasks/demo/task_01.md"
	root, branch, _ := fixture.candidate(t, "task_01", path, "status: completed\n")
	verification := []byte(`{"status":"passed","task_id":"task_01"}`)
	evidence, err := (GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}}).Candidate(context.Background(), CandidateRequest{
		TaskID: "task_01", Slug: "demo", WorktreeRoot: root, RepositoryRoot: fixture.root,
		ExpectedBranch: branch, BaseSHA: fixture.base,
		Verification: verification, VerificationDigest: integrationDigest(verification),
		AllowedTrackingPaths: []string{path},
	})
	if err != nil || len(evidence.Tracking) != 0 {
		t.Fatalf("Candidate() = %#v, error %v", evidence, err)
	}
	sharedPath := ".compozy/tasks/demo/_tasks.md"
	writeIntegrationFile(t, filepath.Join(root, sharedPath), "shared\n")
	request := CandidateRequest{
		TaskID: "task_01", Slug: "demo", WorktreeRoot: root, RepositoryRoot: fixture.root,
		ExpectedBranch: branch, BaseSHA: fixture.base,
		Verification: verification, VerificationDigest: integrationDigest(verification),
		AllowedTrackingPaths: []string{sharedPath},
	}
	if _, err := (GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}}).Candidate(context.Background(), request); !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("Candidate(shared allowlist) error = %v, want ErrInvalidCandidate", err)
	}
}

func TestGitClientCandidateRejectsInvalidOrDirtyEvidence(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		name   string
		mutate func(*testing.T, *integrationGitFixture, string, *CandidateRequest)
	}{
		{name: "zero commits", mutate: func(t *testing.T, fixture *integrationGitFixture, root string, request *CandidateRequest) {
			fixture.run(t, root, "reset", "--hard", fixture.base)
		}},
		{name: "multiple commits", mutate: func(t *testing.T, fixture *integrationGitFixture, root string, _ *CandidateRequest) {
			writeIntegrationFile(t, filepath.Join(root, "second.txt"), "second\n")
			fixture.run(t, root, "add", "second.txt")
			fixture.run(t, root, "commit", "-m", "second")
		}},
		{name: "detached", mutate: func(t *testing.T, fixture *integrationGitFixture, root string, _ *CandidateRequest) {
			fixture.run(t, root, "checkout", "--detach")
		}},
		{name: "foreign branch", mutate: func(_ *testing.T, _ *integrationGitFixture, _ string, request *CandidateRequest) {
			request.ExpectedBranch = "batuta/task/foreign"
		}},
		{name: "valid base drift", mutate: func(t *testing.T, fixture *integrationGitFixture, root string, request *CandidateRequest) {
			request.BaseSHA = strings.TrimSpace(fixture.run(t, root, "rev-parse", "HEAD"))
		}},
		{name: "dirty product", mutate: func(t *testing.T, _ *integrationGitFixture, root string, _ *CandidateRequest) {
			writeIntegrationFile(t, filepath.Join(root, "untracked-product.txt"), "dirty\n")
		}},
		{name: "malformed base", mutate: func(_ *testing.T, _ *integrationGitFixture, _ string, request *CandidateRequest) {
			request.BaseSHA = "main"
		}},
		{name: "missing verification", mutate: func(_ *testing.T, _ *integrationGitFixture, _ string, request *CandidateRequest) {
			request.Verification = nil
		}},
		{name: "verification digest drift", mutate: func(_ *testing.T, _ *integrationGitFixture, _ string, request *CandidateRequest) {
			request.VerificationDigest = integrationDigest([]byte("different"))
		}},
		{name: "noncanonical verification", mutate: func(_ *testing.T, _ *integrationGitFixture, _ string, request *CandidateRequest) {
			request.Verification = []byte("{ \"status\": \"passed\" }")
			request.VerificationDigest = integrationDigest(request.Verification)
		}},
		{name: "tracking allowlist over limit", mutate: func(_ *testing.T, _ *integrationGitFixture, _ string, request *CandidateRequest) {
			request.AllowedTrackingPaths = make([]string, TrackingFileLimit+1)
			for index := range request.AllowedTrackingPaths {
				request.AllowedTrackingPaths[index] = fmt.Sprintf(".compozy/tasks/demo/task_%03d.md", index)
			}
		}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newIntegrationGitFixture(t)
			root, branch, _ := fixture.candidate(t, "task_01", "product.txt", "candidate\n")
			verification := []byte(`{"status":"passed","task_id":"task_01"}`)
			request := CandidateRequest{
				TaskID: "task_01", Slug: "demo", WorktreeRoot: root, RepositoryRoot: fixture.root,
				ExpectedBranch: branch, BaseSHA: fixture.base,
				Verification: verification, VerificationDigest: integrationDigest(verification),
			}
			test.mutate(t, fixture, root, &request)
			_, err := (GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}}).Candidate(context.Background(), request)
			if !errors.Is(err, ErrInvalidCandidate) {
				t.Fatalf("Candidate() error = %v, want ErrInvalidCandidate", err)
			}
		})
	}
}

func TestGitClientCandidateRejectsSymlinkRootAndCancellation(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	root, branch, _ := fixture.candidate(t, "task_01", "product.txt", "candidate\n")
	link := filepath.Join(t.TempDir(), "candidate-link")
	if err := os.Symlink(root, link); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	verification := []byte(`{"status":"passed","task_id":"task_01"}`)
	request := CandidateRequest{
		TaskID: "task_01", Slug: "demo", WorktreeRoot: link, RepositoryRoot: fixture.root,
		ExpectedBranch: branch, BaseSHA: fixture.base,
		Verification: verification, VerificationDigest: integrationDigest(verification),
	}
	client := GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}}
	if _, err := client.Candidate(context.Background(), request); !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("Candidate(symlink) error = %v, want ErrInvalidCandidate", err)
	}
	request.WorktreeRoot = root
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Candidate(canceled, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("Candidate(canceled) error = %v, want context.Canceled", err)
	}
}

func TestGitClientPreflightReturnsMaximalConflictFreePrefixAndCleansScratch(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	first := fixture.candidateEvidence(t, "task_01", "shared.txt", "first\n")
	second := fixture.candidateEvidence(t, "task_02", "shared.txt", "second\n")
	third := fixture.candidateEvidence(t, "task_03", "third.txt", "third\n")
	scratch := filepath.Join(t.TempDir(), "scratch")
	client := GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}, scratchRootForTest: scratch}
	request := PreflightRequest{
		OperationID: integrationDigest([]byte("preflight-op")), RequestDigest: integrationDigest([]byte("preflight-request")),
		IntegrationRoot: fixture.root, StartingHeadSHA: fixture.base,
		Candidates: []CandidateEvidence{first, second, third},
	}

	result, err := client.Preflight(context.Background(), request)
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if !reflect.DeepEqual(result.AcceptedTaskIDs, []string{"task_01"}) ||
		!reflect.DeepEqual(result.AcceptedCommitSHAs, []string{first.CommitSHA}) ||
		len(result.AcceptedResultTreeSHAs) != 1 || result.FirstConflictTaskID != "task_02" ||
		result.ConflictEvidenceDigest == "" || result.ResultingHeadSHA == fixture.base {
		t.Fatalf("Preflight() = %#v", result)
	}
	entries, err := os.ReadDir(scratch)
	if err != nil || len(entries) != 0 {
		t.Fatalf("scratch entries = %#v, error %v", entries, err)
	}
}

func TestGitClientPreflightAcceptsAllCandidatesAndHonorsCancellation(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	first := fixture.candidateEvidence(t, "task_01", "first.txt", "first\n")
	second := fixture.candidateEvidence(t, "task_02", "second.txt", "second\n")
	request := PreflightRequest{
		OperationID: integrationDigest([]byte("all-op")), RequestDigest: integrationDigest([]byte("all-request")),
		IntegrationRoot: fixture.root, StartingHeadSHA: fixture.base,
		Candidates: []CandidateEvidence{first, second},
	}
	client := GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}, scratchRootForTest: filepath.Join(t.TempDir(), "scratch")}
	result, err := client.Preflight(context.Background(), request)
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	if !reflect.DeepEqual(result.AcceptedTaskIDs, []string{"task_01", "task_02"}) ||
		len(result.AcceptedResultTreeSHAs) != 2 || result.FirstConflictTaskID != "" {
		t.Fatalf("Preflight() = %#v", result)
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Preflight(canceled, request); !errors.Is(err, context.Canceled) {
		t.Fatalf("Preflight(canceled) error = %v, want context.Canceled", err)
	}
}

func TestGitClientPreflightReportsFirstAndLastConflict(t *testing.T) {
	t.Parallel()

	for _, conflict := range []string{"first", "last"} {
		t.Run(conflict, func(t *testing.T) {
			fixture := newIntegrationGitFixture(t)
			conflicting := fixture.candidateEvidence(t, "task_conflict", "shared.txt", "candidate\n")
			writeIntegrationFile(t, filepath.Join(fixture.root, "shared.txt"), "integration\n")
			fixture.run(t, fixture.root, "add", "shared.txt")
			fixture.run(t, fixture.root, "commit", "-m", "advance integration")
			startingHead := strings.TrimSpace(fixture.run(t, fixture.root, "rev-parse", "HEAD"))
			candidates := []CandidateEvidence{conflicting}
			wantAccepted := []string{}
			if conflict == "last" {
				first := fixture.candidateEvidence(t, "task_01", "first.txt", "first\n")
				candidates = []CandidateEvidence{first, conflicting}
				wantAccepted = []string{"task_01"}
			}
			result, err := (GitClient{
				Executable: fixture.git, Runner: publication.ExecRunner{}, scratchRootForTest: filepath.Join(t.TempDir(), "scratch"),
			}).Preflight(context.Background(), PreflightRequest{
				OperationID:     integrationDigest([]byte("boundary-" + conflict)),
				RequestDigest:   integrationDigest([]byte("boundary-request-" + conflict)),
				IntegrationRoot: fixture.root, StartingHeadSHA: startingHead, Candidates: candidates,
			})
			if err != nil || !reflect.DeepEqual(result.AcceptedTaskIDs, wantAccepted) ||
				result.FirstConflictTaskID != "task_conflict" || !sha256Digest(result.ConflictEvidenceDigest) {
				t.Fatalf("Preflight(%s) = %#v, error %v", conflict, result, err)
			}
		})
	}
}

func TestGitClientPreflightRejectsSymlinkScratchRoot(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	candidate := fixture.candidateEvidence(t, "task_01", "first.txt", "first\n")
	actualScratch := filepath.Join(t.TempDir(), "actual-scratch")
	if err := os.MkdirAll(actualScratch, 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	scratchLink := filepath.Join(t.TempDir(), "scratch-link")
	if err := os.Symlink(actualScratch, scratchLink); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	client := GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}, scratchRootForTest: scratchLink}
	_, err := client.Preflight(context.Background(), PreflightRequest{
		OperationID: integrationDigest([]byte("symlink-op")), RequestDigest: integrationDigest([]byte("symlink-request")),
		IntegrationRoot: fixture.root, StartingHeadSHA: fixture.base, Candidates: []CandidateEvidence{candidate},
	})
	if err == nil {
		t.Fatal("Preflight() error = nil, want symlink rejection")
	}
}

func TestGitClientPreflightRejectsInsecureExistingScratchWithoutChangingPermissions(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	candidate := fixture.candidateEvidence(t, "task_01", "first.txt", "first\n")
	scratch := filepath.Join(t.TempDir(), "scratch")
	if err := os.Mkdir(scratch, 0o755); err != nil {
		t.Fatalf("Mkdir() error = %v", err)
	}
	client := GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}, scratchRootForTest: scratch}
	_, err := client.Preflight(context.Background(), PreflightRequest{
		OperationID: integrationDigest([]byte("permissions-op")), RequestDigest: integrationDigest([]byte("permissions-request")),
		IntegrationRoot: fixture.root, StartingHeadSHA: fixture.base, Candidates: []CandidateEvidence{candidate},
	})
	info, statErr := os.Stat(scratch)
	mode := os.FileMode(0)
	if statErr == nil {
		mode = info.Mode().Perm()
	}
	if err == nil || statErr != nil || mode != 0o755 {
		t.Fatalf("Preflight() error = %v, scratch mode %o, stat error %v", err, mode, statErr)
	}
}

func TestGitClientPreflightRejectsForeignRepositoryAndOperationalCherryPickFailure(t *testing.T) {
	t.Parallel()

	integrationFixture := newIntegrationGitFixture(t)
	foreignFixture := newIntegrationGitFixture(t)
	foreign := foreignFixture.candidateEvidence(t, "task_01", "first.txt", "first\n")
	client := GitClient{Executable: integrationFixture.git, Runner: publication.ExecRunner{}, scratchRootForTest: filepath.Join(t.TempDir(), "scratch")}
	request := PreflightRequest{
		OperationID: integrationDigest([]byte("foreign-repo-op")), RequestDigest: integrationDigest([]byte("foreign-repo-request")),
		IntegrationRoot: integrationFixture.root, StartingHeadSHA: integrationFixture.base, Candidates: []CandidateEvidence{foreign},
	}
	if _, err := client.Preflight(context.Background(), request); !errors.Is(err, ErrForeignState) {
		t.Fatalf("Preflight(foreign repository) error = %v, want ErrForeignState", err)
	}
	local := integrationFixture.candidateEvidence(t, "task_01", "first.txt", "first\n")
	local.CommitSHA = strings.Repeat("f", 40)
	request.OperationID = integrationDigest([]byte("missing-object-op"))
	request.RequestDigest = integrationDigest([]byte("missing-object-request"))
	request.Candidates = []CandidateEvidence{local}
	if result, err := client.Preflight(context.Background(), request); !errors.Is(err, ErrPreflightFailed) || result.FirstConflictTaskID != "" {
		t.Fatalf("Preflight(missing object) = %#v, error %v", result, err)
	}
}

func TestGitClientApplyDeterministicRestoresDurableRootAfterConflict(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	_, _, candidateCommit := fixture.candidate(t, "task_01", "shared.txt", "candidate\n")
	writeIntegrationFile(t, filepath.Join(fixture.root, "shared.txt"), "integration\n")
	fixture.run(t, fixture.root, "add", "shared.txt")
	fixture.run(t, fixture.root, "commit", "-m", "advance integration root")
	head := strings.TrimSpace(fixture.run(t, fixture.root, "rev-parse", "HEAD"))
	client := GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}}

	_, _, err := client.applyDeterministic(context.Background(), deterministicApplyRequest{
		Root: fixture.root, OperationID: integrationDigest([]byte("durable-conflict-op")),
		RequestDigest: integrationDigest([]byte("durable-conflict-request")), TaskID: "task_01",
		CandidateCommitSHA: candidateCommit,
	})
	if !errors.Is(err, ErrPreflightFailed) {
		t.Fatalf("applyDeterministic(conflict) error = %v, want ErrPreflightFailed", err)
	}
	if status := fixture.run(t, fixture.root, "status", "--porcelain=v1"); status != "" {
		t.Fatalf("durable integration root status after conflict = %q, want clean", status)
	}
	if got := strings.TrimSpace(fixture.run(t, fixture.root, "rev-parse", "HEAD")); got != head {
		t.Fatalf("durable integration root HEAD after conflict = %q, want %q", got, head)
	}
}

func TestGitClientApplyAndReconcileExactPrefix(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	first := fixture.candidateEvidence(t, "task_01", "first.txt", "first\n")
	second := fixture.candidateEvidence(t, "task_02", "second.txt", "second\n")
	client := GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}, scratchRootForTest: filepath.Join(t.TempDir(), "scratch")}
	preflight, err := client.Preflight(context.Background(), PreflightRequest{
		OperationID: integrationDigest([]byte("apply-op")), RequestDigest: integrationDigest([]byte("apply-request")),
		IntegrationRoot: fixture.root, StartingHeadSHA: fixture.base,
		Candidates: []CandidateEvidence{first, second},
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	firstApplied, err := client.Apply(context.Background(), ApplyRequest{
		OperationID: preflight.OperationID, RequestDigest: preflight.RequestDigest,
		IntegrationRoot: fixture.root, ExpectedHeadSHA: fixture.base, TaskID: first.TaskID,
		CandidateCommitSHA: first.CommitSHA, ExpectedResultTreeSHA: preflight.AcceptedResultTreeSHAs[0],
		ExpectedResultCommitSHA: preflight.AcceptedResultCommitSHAs[0],
	})
	if err != nil || firstApplied.ResultingHeadSHA == fixture.base {
		t.Fatalf("Apply(first) = %#v, error %v", firstApplied, err)
	}
	identity := fixture.run(t, fixture.root, "show", "-s", "--format=%s%x00%an%x00%B", "HEAD")
	if !strings.Contains(identity, "feat: implement task_01\x00Batuta Test\x00") ||
		!strings.Contains(identity, "Batuta-Operation: "+preflight.OperationID) ||
		!strings.Contains(identity, "Batuta-Request: "+preflight.RequestDigest) ||
		!strings.Contains(identity, "Batuta-Task: task_01") ||
		!strings.Contains(identity, "Batuta-Candidate: "+first.CommitSHA) {
		t.Fatalf("integration commit identity = %q", identity)
	}
	secondApplied, err := client.Apply(context.Background(), ApplyRequest{
		OperationID: preflight.OperationID, RequestDigest: preflight.RequestDigest,
		IntegrationRoot: fixture.root, ExpectedHeadSHA: firstApplied.ResultingHeadSHA, TaskID: second.TaskID,
		CandidateCommitSHA: second.CommitSHA, ExpectedResultTreeSHA: preflight.AcceptedResultTreeSHAs[1],
		ExpectedResultCommitSHA: preflight.AcceptedResultCommitSHAs[1],
	})
	if err != nil || secondApplied.ResultingHeadSHA == firstApplied.ResultingHeadSHA {
		t.Fatalf("Apply(second) = %#v, error %v", secondApplied, err)
	}
	reconciled, err := client.Reconcile(context.Background(), ReconcileRequest{IntegrationRoot: fixture.root, Preflight: preflight})
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !reflect.DeepEqual(reconciled.AcceptedTaskIDs, []string{"task_01", "task_02"}) ||
		!reflect.DeepEqual(reconciled.AcceptedCommitSHAs, []string{first.CommitSHA, second.CommitSHA}) ||
		reconciled.ResultingHeadSHA != secondApplied.ResultingHeadSHA {
		t.Fatalf("Reconcile() = %#v", reconciled)
	}
}

func TestGitClientReconcilesAmbiguousApplyWithoutDuplicatingCommit(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	candidate := fixture.candidateEvidence(t, "task_01", "first.txt", "first\n")
	client := GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}, scratchRootForTest: filepath.Join(t.TempDir(), "scratch")}
	preflight, err := client.Preflight(context.Background(), PreflightRequest{
		OperationID: integrationDigest([]byte("ambiguous-op")), RequestDigest: integrationDigest([]byte("ambiguous-request")),
		IntegrationRoot: fixture.root, StartingHeadSHA: fixture.base, Candidates: []CandidateEvidence{candidate},
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	ambiguous := &failAfterMutationRunner{delegate: publication.ExecRunner{}}
	client.Runner = ambiguous
	_, err = client.Apply(context.Background(), ApplyRequest{
		OperationID: preflight.OperationID, RequestDigest: preflight.RequestDigest,
		IntegrationRoot: fixture.root, ExpectedHeadSHA: fixture.base, TaskID: candidate.TaskID,
		CandidateCommitSHA: candidate.CommitSHA, ExpectedResultTreeSHA: preflight.AcceptedResultTreeSHAs[0],
		ExpectedResultCommitSHA: preflight.AcceptedResultCommitSHAs[0],
	})
	if !errors.Is(err, ErrApplyAmbiguous) {
		t.Fatalf("Apply() error = %v, want ErrApplyAmbiguous", err)
	}
	client.Runner = publication.ExecRunner{}
	reconciled, err := client.Reconcile(context.Background(), ReconcileRequest{IntegrationRoot: fixture.root, Preflight: preflight})
	if err != nil || !reflect.DeepEqual(reconciled.AcceptedTaskIDs, []string{"task_01"}) {
		t.Fatalf("Reconcile() = %#v, error %v", reconciled, err)
	}
	commits := strings.Fields(fixture.run(t, fixture.root, "rev-list", "--reverse", fixture.base+"..HEAD"))
	if len(commits) != 1 {
		t.Fatalf("commits after reconciliation = %#v, want exactly one", commits)
	}
}

func TestGitClientReconcileRejectsForeignCommitWithExpectedTree(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	candidate := fixture.candidateEvidence(t, "task_01", "first.txt", "first\n")
	client := GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}, scratchRootForTest: filepath.Join(t.TempDir(), "scratch")}
	preflight, err := client.Preflight(context.Background(), PreflightRequest{
		OperationID: integrationDigest([]byte("foreign-tree-op")), RequestDigest: integrationDigest([]byte("foreign-tree-request")),
		IntegrationRoot: fixture.root, StartingHeadSHA: fixture.base, Candidates: []CandidateEvidence{candidate},
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	result, err := (publication.ExecRunner{}).Run(context.Background(), publication.Command{
		Executable: fixture.git, Directory: fixture.root,
		Args: []string{"commit-tree", preflight.AcceptedResultTreeSHAs[0], "-p", fixture.base}, Stdin: []byte("foreign commit\n"),
		Environment: metadataCommitEnvironment, StdoutLimit: GitStdoutLimit, StderrLimit: GitStderrLimit,
	})
	if err != nil {
		t.Fatalf("commit-tree error = %v", err)
	}
	foreign := strings.TrimSpace(string(result.Stdout))
	fixture.run(t, fixture.root, "update-ref", "HEAD", foreign, fixture.base)
	fixture.run(t, fixture.root, "read-tree", "--reset", "-u", foreign)
	if _, err := client.Reconcile(context.Background(), ReconcileRequest{IntegrationRoot: fixture.root, Preflight: preflight}); !errors.Is(err, ErrForeignState) {
		t.Fatalf("Reconcile() error = %v, want ErrForeignState", err)
	}
}

func TestGitClientApplyAndReconcileRejectForeignDirtyAndCanceledState(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	candidate := fixture.candidateEvidence(t, "task_01", "first.txt", "first\n")
	client := GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}, scratchRootForTest: filepath.Join(t.TempDir(), "scratch")}
	preflight, err := client.Preflight(context.Background(), PreflightRequest{
		OperationID: integrationDigest([]byte("foreign-op")), RequestDigest: integrationDigest([]byte("foreign-request")),
		IntegrationRoot: fixture.root, StartingHeadSHA: fixture.base, Candidates: []CandidateEvidence{candidate},
	})
	if err != nil {
		t.Fatalf("Preflight() error = %v", err)
	}
	writeIntegrationFile(t, filepath.Join(fixture.root, "dirty.txt"), "dirty\n")
	if _, err := client.Apply(context.Background(), ApplyRequest{
		OperationID: preflight.OperationID, RequestDigest: preflight.RequestDigest,
		IntegrationRoot: fixture.root, ExpectedHeadSHA: fixture.base, TaskID: candidate.TaskID,
		CandidateCommitSHA: candidate.CommitSHA, ExpectedResultTreeSHA: preflight.AcceptedResultTreeSHAs[0],
		ExpectedResultCommitSHA: preflight.AcceptedResultCommitSHAs[0],
	}); !errors.Is(err, ErrForeignState) {
		t.Fatalf("Apply(dirty) error = %v, want ErrForeignState", err)
	}
	if _, err := client.Reconcile(context.Background(), ReconcileRequest{
		IntegrationRoot: fixture.root, Preflight: preflight,
	}); !errors.Is(err, ErrForeignState) {
		t.Fatalf("Reconcile(dirty) error = %v, want ErrForeignState", err)
	}
	if err := os.Remove(filepath.Join(fixture.root, "dirty.txt")); err != nil {
		t.Fatalf("Remove(dirty) error = %v", err)
	}
	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := client.Reconcile(canceled, ReconcileRequest{
		IntegrationRoot: fixture.root, Preflight: preflight,
	}); !errors.Is(err, context.Canceled) {
		t.Fatalf("Reconcile(canceled) error = %v, want context.Canceled", err)
	}
}

type integrationGitFixture struct {
	git  string
	root string
	base string
}

func newIntegrationGitFixture(t *testing.T) *integrationGitFixture {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Fatalf("LookPath(git) error = %v", err)
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatalf("Abs(git) error = %v", err)
	}
	fixture := &integrationGitFixture{git: git, root: filepath.Join(t.TempDir(), "integration")}
	fixture.run(t, "", "init", "--initial-branch=main", fixture.root)
	fixture.run(t, fixture.root, "config", "user.email", "batuta@example.invalid")
	fixture.run(t, fixture.root, "config", "user.name", "Batuta Test")
	writeIntegrationFile(t, filepath.Join(fixture.root, "README.md"), "base\n")
	writeIntegrationFile(t, filepath.Join(fixture.root, "shared.txt"), "base\n")
	fixture.run(t, fixture.root, "add", "README.md", "shared.txt")
	fixture.run(t, fixture.root, "commit", "-m", "base")
	fixture.base = strings.TrimSpace(fixture.run(t, fixture.root, "rev-parse", "HEAD"))
	return fixture
}

func (f *integrationGitFixture) candidate(t *testing.T, taskID, path, content string) (string, string, string) {
	t.Helper()
	branch := "batuta/task/" + taskID
	root := filepath.Join(t.TempDir(), taskID)
	f.run(t, f.root, "worktree", "add", "-b", branch, root, f.base)
	writeIntegrationFile(t, filepath.Join(root, path), content)
	f.run(t, root, "add", path)
	f.run(t, root, "commit", "-m", "feat: implement "+taskID)
	return root, branch, strings.TrimSpace(f.run(t, root, "rev-parse", "HEAD"))
}

func (f *integrationGitFixture) candidateEvidence(t *testing.T, taskID, path, content string) CandidateEvidence {
	t.Helper()
	root, branch, _ := f.candidate(t, taskID, path, content)
	verification := []byte(`{"status":"passed","task_id":"` + taskID + `"}`)
	evidence, err := (GitClient{Executable: f.git, Runner: publication.ExecRunner{}}).Candidate(context.Background(), CandidateRequest{
		TaskID: taskID, Slug: "demo", WorktreeRoot: root, RepositoryRoot: f.root,
		ExpectedBranch: branch, BaseSHA: f.base,
		Verification: verification, VerificationDigest: integrationDigest(verification),
	})
	if err != nil {
		t.Fatalf("Candidate(%s) error = %v", taskID, err)
	}
	return evidence
}

func (f *integrationGitFixture) run(t *testing.T, directory string, args ...string) string {
	t.Helper()
	result, err := (publication.ExecRunner{}).Run(context.Background(), publication.Command{
		Executable: f.git, Args: args, Directory: directory,
		StdoutLimit: GitStdoutLimit, StderrLimit: GitStderrLimit,
	})
	if err != nil {
		t.Fatalf("git %v in %q: %v; stderr=%s", args, directory, err, result.Stderr)
	}
	return string(result.Stdout)
}

func writeIntegrationFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll(%s) error = %v", path, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("WriteFile(%s) error = %v", path, err)
	}
}

func integrationDigest(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

type delegatingRunner struct {
	delegate publication.CommandRunner
	commands []publication.Command
}

func (r *delegatingRunner) Run(ctx context.Context, command publication.Command) (publication.CommandResult, error) {
	r.commands = append(r.commands, command)
	return r.delegate.Run(ctx, command)
}

type failAfterMutationRunner struct {
	delegate publication.CommandRunner
	failed   bool
}

func (r *failAfterMutationRunner) Run(ctx context.Context, command publication.Command) (publication.CommandResult, error) {
	result, err := r.delegate.Run(ctx, command)
	if err == nil && !r.failed && len(command.Args) > 0 && command.Args[0] == "update-ref" {
		r.failed = true
		return result, errors.New("simulated lost mutation response")
	}
	return result, err
}

func TestGitClientCandidateSkipsIgnoredArtifactsOutsideTracking(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	writeIntegrationFile(t, filepath.Join(fixture.root, ".gitignore"), "node_modules/\ndist/\n")
	fixture.run(t, fixture.root, "add", ".gitignore")
	fixture.run(t, fixture.root, "commit", "-m", "ignore build artifacts")
	fixture.base = strings.TrimSpace(fixture.run(t, fixture.root, "rev-parse", "HEAD"))
	root, branch, commit := fixture.candidate(t, "task_01", "product.txt", "product\n")
	writeIntegrationFile(t, filepath.Join(root, "node_modules", "dep", "index.js"), "module.exports = 1\n")
	writeIntegrationFile(t, filepath.Join(root, "dist", "bundle.js"), "bundle\n")
	writeIntegrationFile(t, filepath.Join(root, ".compozy", "tasks", "demo", "task_01.md"), "status: completed\n")
	verification := []byte(`{"status":"passed","task_id":"task_01"}`)

	evidence, err := (GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}}).Candidate(context.Background(), CandidateRequest{
		TaskID: "task_01", Slug: "demo", WorktreeRoot: root, RepositoryRoot: fixture.root,
		ExpectedBranch: branch, BaseSHA: fixture.base,
		Verification: verification, VerificationDigest: integrationDigest(verification),
		AllowedTrackingPaths: []string{".compozy/tasks/demo/task_01.md"},
	})
	if err != nil {
		t.Fatalf("Candidate() error = %v", err)
	}
	if evidence.CommitSHA != commit || !reflect.DeepEqual(evidence.Tracking, []TrackingFile{{
		Path: ".compozy/tasks/demo/task_01.md", Digest: integrationDigest([]byte("status: completed\n")),
	}}) {
		t.Fatalf("Candidate() = %#v", evidence)
	}
}
