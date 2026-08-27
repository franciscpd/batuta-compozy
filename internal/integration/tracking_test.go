package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

func TestFileTrackingSynchronizerPreservesParallelTaskEvidenceAndRebuildsSharedState(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	first := trackingCandidate(t, fixture, "task_01", "first.txt", "first\n", "status: completed\n")
	second := trackingCandidate(t, fixture, "task_02", "second.txt", "second\n", "status: completed\n")
	operationID := integrationDigest([]byte("tracking-operation"))
	request := TrackingSyncRequest{
		OperationID: operationID, RequestDigest: integrationDigest([]byte("tracking-request")),
		ProjectionRevision: integrationDigest([]byte("tracking-projection")),
		IntegrationRoot:    fixture.root, ExpectedHeadSHA: fixture.base,
		Candidates: []CandidateEvidence{first, second}, AcceptedTaskIDs: []string{"task_01", "task_02"},
		SharedTracking: []ProjectedTrackingFile{{
			Path: ".compozy/tasks/demo/_index.json", Digest: integrationDigest([]byte(`{"tasks":["task_01","task_02"]}` + "\n")),
			Content: []byte(`{"tasks":["task_01","task_02"]}` + "\n"),
		}},
	}
	synchronizer := &FileTrackingSynchronizer{GitExecutable: fixture.git, Runner: publication.ExecRunner{}}

	firstResult, err := synchronizer.Sync(context.Background(), request)
	if err != nil || firstResult.MetadataCommitSHA == "" || !sha256Digest(firstResult.Digest) {
		t.Fatalf("Sync(first) = %#v, error %v", firstResult, err)
	}
	for _, taskID := range request.AcceptedTaskIDs {
		content, err := os.ReadFile(filepath.Join(fixture.root, ".compozy", "tasks", "demo", taskID+".md"))
		if err != nil || string(content) != "status: completed\n" {
			t.Fatalf("tracking %s = %q, error %v", taskID, content, err)
		}
	}
	shared, err := os.ReadFile(filepath.Join(fixture.root, ".compozy", "tasks", "demo", "_index.json"))
	if err != nil || string(shared) != `{"tasks":["task_01","task_02"]}`+"\n" {
		t.Fatalf("shared tracking = %q, error %v", shared, err)
	}

	replayed, err := synchronizer.Sync(context.Background(), request)
	if err != nil || !reflect.DeepEqual(replayed, firstResult) {
		t.Fatalf("Sync(replay) = %#v, error %v, want %#v", replayed, err, firstResult)
	}
	writeIntegrationFile(t, filepath.Join(first.WorktreeRoot, ".compozy", "tasks", "demo", "task_01.md"), "source removed after completion\n")
	knownRequest := request
	knownRequest.ExpectedDigest = firstResult.Digest
	knownRequest.ExpectedMetadataCommitSHA = firstResult.MetadataCommitSHA
	known, err := synchronizer.Sync(context.Background(), knownRequest)
	if err != nil || !reflect.DeepEqual(known, firstResult) {
		t.Fatalf("Sync(known replay) = %#v, error %v, want %#v", known, err, firstResult)
	}
	commits := strings.Fields(fixture.run(t, fixture.root, "rev-list", fixture.base+"..HEAD"))
	if !reflect.DeepEqual(commits, []string{firstResult.MetadataCommitSHA}) {
		t.Fatalf("metadata commits = %#v", commits)
	}
}

func TestFileTrackingSynchronizerNeverForceAddsIgnoredEvidence(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	writeIntegrationFile(t, filepath.Join(fixture.root, ".gitignore"), ".compozy/tasks/demo/task_01.md\n")
	fixture.run(t, fixture.root, "add", ".gitignore")
	fixture.run(t, fixture.root, "commit", "-m", "ignore task evidence")
	fixture.base = strings.TrimSpace(fixture.run(t, fixture.root, "rev-parse", "HEAD"))
	candidate := trackingCandidate(t, fixture, "task_01", "product.txt", "product\n", "ignored\n")
	if len(candidate.Tracking) != 1 || !candidate.Tracking[0].Ignored {
		t.Fatalf("candidate tracking = %#v", candidate.Tracking)
	}
	runner := &delegatingRunner{delegate: publication.ExecRunner{}}
	synchronizer := &FileTrackingSynchronizer{GitExecutable: fixture.git, Runner: runner}
	result, err := synchronizer.Sync(context.Background(), TrackingSyncRequest{
		OperationID: integrationDigest([]byte("ignored-operation")), RequestDigest: integrationDigest([]byte("ignored-request")),
		ProjectionRevision: integrationDigest([]byte("ignored-projection")),
		IntegrationRoot:    fixture.root, ExpectedHeadSHA: fixture.base,
		Candidates: []CandidateEvidence{candidate}, AcceptedTaskIDs: []string{"task_01"},
		SharedTracking: []ProjectedTrackingFile{},
	})
	if err != nil || result.MetadataCommitSHA != "" {
		t.Fatalf("Sync() = %#v, error %v", result, err)
	}
	for _, command := range runner.commands {
		if len(command.Args) > 0 && command.Args[0] == "add" {
			t.Fatalf("ignored tracking was staged: %#v", command.Args)
		}
	}
	if _, err := os.Stat(filepath.Join(fixture.root, ".compozy", "tasks", "demo", "task_01.md")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("ignored tracking exists or stat failed: %v", err)
	}
}

func TestFileTrackingSynchronizerRejectsChangedSourceEvidence(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	candidate := trackingCandidate(t, fixture, "task_01", "product.txt", "product\n", "status: completed\n")
	writeIntegrationFile(t, filepath.Join(candidate.WorktreeRoot, ".compozy", "tasks", "demo", "task_01.md"), "status: replaced\n")
	synchronizer := &FileTrackingSynchronizer{GitExecutable: fixture.git, Runner: publication.ExecRunner{}}
	_, err := synchronizer.Sync(context.Background(), TrackingSyncRequest{
		OperationID: integrationDigest([]byte("drift-operation")), RequestDigest: integrationDigest([]byte("drift-request")),
		ProjectionRevision: integrationDigest([]byte("drift-projection")),
		IntegrationRoot:    fixture.root, ExpectedHeadSHA: fixture.base,
		Candidates: []CandidateEvidence{candidate}, AcceptedTaskIDs: []string{"task_01"},
		SharedTracking: []ProjectedTrackingFile{},
	})
	if !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("Sync() error = %v, want ErrInvalidCandidate", err)
	}
}

func TestFileTrackingSynchronizerRejectsSharedProjectionThatOverwritesTaskEvidence(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	candidate := trackingCandidate(t, fixture, "task_01", "product.txt", "product\n", "status: completed\n")
	synchronizer := &FileTrackingSynchronizer{GitExecutable: fixture.git, Runner: publication.ExecRunner{}}
	path := ".compozy/tasks/demo/task_01.md"
	shared := []byte("shared overwrite\n")
	_, err := synchronizer.Sync(context.Background(), TrackingSyncRequest{
		OperationID: integrationDigest([]byte("unowned-operation")), RequestDigest: integrationDigest([]byte("unowned-request")),
		ProjectionRevision: integrationDigest([]byte("unowned-projection")),
		IntegrationRoot:    fixture.root, ExpectedHeadSHA: fixture.base,
		Candidates: []CandidateEvidence{candidate}, AcceptedTaskIDs: []string{"task_01"},
		SharedTracking: []ProjectedTrackingFile{{Path: path, Digest: integrationDigest(shared), Content: shared}},
	})
	if !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("Sync() error = %v, want ErrInvalidCandidate", err)
	}
}

func TestFileTrackingSynchronizerRejectsSharedProjectionThatOverwritesCommittedTaskEvidence(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	branch := "batuta/task/task_01"
	root := filepath.Join(t.TempDir(), "task_01")
	fixture.run(t, fixture.root, "worktree", "add", "-b", branch, root, fixture.base)
	writeIntegrationFile(t, filepath.Join(root, "product.txt"), "product\n")
	path := ".compozy/tasks/demo/task_01.md"
	committed := []byte("status: completed\n")
	writeIntegrationFile(t, filepath.Join(root, filepath.FromSlash(path)), string(committed))
	fixture.run(t, root, "add", "product.txt", path)
	fixture.run(t, root, "commit", "-m", "feat: implement task_01")
	verification := []byte(`{"status":"passed","task_id":"task_01"}`)
	candidate, err := (GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}}).Candidate(context.Background(), CandidateRequest{
		TaskID: "task_01", Slug: "demo", WorktreeRoot: root, RepositoryRoot: fixture.root,
		ExpectedBranch: branch, BaseSHA: fixture.base,
		Verification: verification, VerificationDigest: integrationDigest(verification),
		AllowedTrackingPaths: []string{path},
	})
	if err != nil || len(candidate.Tracking) != 0 || !reflect.DeepEqual(candidate.OwnedTrackingPaths, []string{path}) {
		t.Fatalf("Candidate() = %#v, error %v", candidate, err)
	}
	shared := []byte("shared overwrite\n")
	_, err = (&FileTrackingSynchronizer{GitExecutable: fixture.git, Runner: publication.ExecRunner{}}).Sync(context.Background(), TrackingSyncRequest{
		OperationID: integrationDigest([]byte("committed-overwrite-operation")), RequestDigest: integrationDigest([]byte("committed-overwrite-request")),
		ProjectionRevision: integrationDigest([]byte("committed-overwrite-projection")),
		IntegrationRoot:    fixture.root, ExpectedHeadSHA: fixture.base,
		Candidates: []CandidateEvidence{candidate}, AcceptedTaskIDs: []string{"task_01"},
		SharedTracking: []ProjectedTrackingFile{{Path: path, Digest: integrationDigest(shared), Content: shared}},
	})
	if !errors.Is(err, ErrInvalidCandidate) {
		t.Fatalf("Sync() error = %v, want ErrInvalidCandidate", err)
	}
}

func TestFileTrackingSynchronizerRejectsSharedProjectionThroughEscapingSymlink(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	outside := t.TempDir()
	if err := os.MkdirAll(filepath.Join(fixture.root, ".compozy", "tasks"), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Symlink(outside, filepath.Join(fixture.root, ".compozy", "tasks", "demo")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	fixture.run(t, fixture.root, "add", ".compozy/tasks/demo")
	fixture.run(t, fixture.root, "commit", "-m", "test: add hostile tracking symlink")
	fixture.base = strings.TrimSpace(fixture.run(t, fixture.root, "rev-parse", "HEAD"))
	candidate := fixture.candidateEvidence(t, "task_01", "product.txt", "product\n")
	content := []byte("must stay inside\n")
	_, err := (&FileTrackingSynchronizer{GitExecutable: fixture.git, Runner: publication.ExecRunner{}}).Sync(context.Background(), TrackingSyncRequest{
		OperationID: integrationDigest([]byte("symlink-operation")), RequestDigest: integrationDigest([]byte("symlink-request")),
		ProjectionRevision: integrationDigest([]byte("symlink-projection")),
		IntegrationRoot:    fixture.root, ExpectedHeadSHA: fixture.base,
		Candidates: []CandidateEvidence{candidate}, AcceptedTaskIDs: []string{},
		SharedTracking: []ProjectedTrackingFile{{
			Path: ".compozy/tasks/demo/escaped.md", Digest: integrationDigest(content), Content: content,
		}},
	})
	if err == nil {
		t.Fatal("Sync() error = nil, want escaping symlink rejection")
	}
	if _, statErr := os.Stat(filepath.Join(outside, "escaped.md")); !errors.Is(statErr, os.ErrNotExist) {
		t.Fatalf("outside file was written or stat failed: %v", statErr)
	}
}

func TestFileTrackingSynchronizerRejectsSharedProjectionThroughInternalSymlink(t *testing.T) {
	t.Parallel()

	fixture := newIntegrationGitFixture(t)
	protected := filepath.Join(fixture.root, "protected")
	writeIntegrationFile(t, filepath.Join(protected, "owned.md"), "original\n")
	if err := os.MkdirAll(filepath.Join(fixture.root, ".compozy", "tasks"), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	if err := os.Symlink("../../protected", filepath.Join(fixture.root, ".compozy", "tasks", "demo")); err != nil {
		t.Fatalf("Symlink() error = %v", err)
	}
	fixture.run(t, fixture.root, "add", "protected/owned.md", ".compozy/tasks/demo")
	fixture.run(t, fixture.root, "commit", "-m", "test: add internal tracking symlink")
	fixture.base = strings.TrimSpace(fixture.run(t, fixture.root, "rev-parse", "HEAD"))
	candidate := fixture.candidateEvidence(t, "task_01", "product.txt", "product\n")
	content := []byte("must not replace product state\n")
	_, err := (&FileTrackingSynchronizer{GitExecutable: fixture.git, Runner: publication.ExecRunner{}}).Sync(context.Background(), TrackingSyncRequest{
		OperationID: integrationDigest([]byte("internal-symlink-operation")), RequestDigest: integrationDigest([]byte("internal-symlink-request")),
		ProjectionRevision: integrationDigest([]byte("internal-symlink-projection")),
		IntegrationRoot:    fixture.root, ExpectedHeadSHA: fixture.base,
		Candidates: []CandidateEvidence{candidate}, AcceptedTaskIDs: []string{},
		SharedTracking: []ProjectedTrackingFile{{
			Path: ".compozy/tasks/demo/owned.md", Digest: integrationDigest(content), Content: content,
		}},
	})
	if err == nil {
		t.Fatal("Sync() error = nil, want internal symlink rejection")
	}
	actual, readErr := os.ReadFile(filepath.Join(protected, "owned.md"))
	if readErr != nil || string(actual) != "original\n" {
		t.Fatalf("protected file = %q, error %v", actual, readErr)
	}
}

func TestFileTrackingSynchronizerReconcilesExactPartialAndCommittedTracking(t *testing.T) {
	t.Parallel()

	for _, faultPoint := range []string{
		"after_write:.compozy/tasks/demo/task_01.md",
		"after_add:.compozy/tasks/demo/task_01.md",
		"after_commit",
	} {
		t.Run(faultPoint, func(t *testing.T) {
			fixture := newIntegrationGitFixture(t)
			candidate := trackingCandidate(t, fixture, "task_01", "product.txt", "product\n", "status: completed\n")
			request := TrackingSyncRequest{
				OperationID: integrationDigest([]byte("fault-" + faultPoint)), RequestDigest: integrationDigest([]byte("fault-request")),
				ProjectionRevision: integrationDigest([]byte("fault-projection")),
				IntegrationRoot:    fixture.root, ExpectedHeadSHA: fixture.base,
				Candidates: []CandidateEvidence{candidate}, AcceptedTaskIDs: []string{"task_01"},
				SharedTracking: []ProjectedTrackingFile{},
			}
			synchronizer := &FileTrackingSynchronizer{
				GitExecutable: fixture.git, Runner: publication.ExecRunner{},
				Fault: func(point string) error {
					if point == faultPoint {
						return errors.New("injected tracking fault")
					}
					return nil
				},
			}
			if _, err := synchronizer.Sync(context.Background(), request); err == nil {
				t.Fatal("Sync(first) error = nil")
			}
			synchronizer.Fault = nil
			result, err := synchronizer.Sync(context.Background(), request)
			if err != nil || result.MetadataCommitSHA == "" {
				t.Fatalf("Sync(replay) = %#v, error %v", result, err)
			}
			commits := strings.Fields(fixture.run(t, fixture.root, "rev-list", fixture.base+"..HEAD"))
			if !reflect.DeepEqual(commits, []string{result.MetadataCommitSHA}) {
				t.Fatalf("commits = %#v", commits)
			}
		})
	}
}

func trackingCandidate(
	t *testing.T,
	fixture *integrationGitFixture,
	taskID, productPath, productContent, trackingContent string,
) CandidateEvidence {
	t.Helper()
	root, branch, _ := fixture.candidate(t, taskID, productPath, productContent)
	writeIntegrationFile(t, filepath.Join(root, ".compozy", "tasks", "demo", taskID+".md"), trackingContent)
	verification := []byte(`{"status":"passed","task_id":"` + taskID + `"}`)
	evidence, err := (GitClient{Executable: fixture.git, Runner: publication.ExecRunner{}}).Candidate(context.Background(), CandidateRequest{
		TaskID: taskID, Slug: "demo", WorktreeRoot: root, RepositoryRoot: fixture.root,
		ExpectedBranch: branch, BaseSHA: fixture.base,
		Verification: verification, VerificationDigest: integrationDigest(verification),
		AllowedTrackingPaths: []string{".compozy/tasks/demo/" + taskID + ".md"},
	})
	if err != nil {
		t.Fatalf("Candidate(%s) error = %v", taskID, err)
	}
	return evidence
}
