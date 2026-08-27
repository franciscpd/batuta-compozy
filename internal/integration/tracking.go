package integration

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

var metadataCommitEnvironment = []string{
	"GIT_AUTHOR_NAME=Batuta",
	"GIT_AUTHOR_EMAIL=batuta@example.invalid",
	"GIT_AUTHOR_DATE=2000-01-01T00:00:00Z",
	"GIT_COMMITTER_NAME=Batuta",
	"GIT_COMMITTER_EMAIL=batuta@example.invalid",
	"GIT_COMMITTER_DATE=2000-01-01T00:00:00Z",
}

type FileTrackingSynchronizer struct {
	GitExecutable string
	Runner        publication.CommandRunner
	Fault         func(string) error
	mu            sync.Mutex
}

type desiredTrackingFile struct {
	Path    string
	Digest  string
	Content []byte
}

func (s *FileTrackingSynchronizer) Sync(ctx context.Context, request TrackingSyncRequest) (TrackingSyncResult, error) {
	if err := ctx.Err(); err != nil {
		return TrackingSyncResult{}, err
	}
	if s == nil || s.Runner == nil || !filepath.IsAbs(s.GitExecutable) || filepath.Clean(s.GitExecutable) != s.GitExecutable ||
		!sha256Digest(request.OperationID) || !sha256Digest(request.RequestDigest) ||
		!sha256Digest(request.ProjectionRevision) || !gitSHA(request.ExpectedHeadSHA) ||
		request.SharedTracking == nil || len(request.AcceptedTaskIDs) > len(request.Candidates) ||
		validateProjectedTracking(request.SharedTracking) != nil || validateCandidateSequence(request.Candidates) != nil ||
		validateTrackingOwnership(request.Candidates, request.SharedTracking) != nil {
		return TrackingSyncResult{}, ErrInvalidCandidate
	}
	root, err := canonicalDirectory(request.IntegrationRoot)
	if err != nil || root != request.IntegrationRoot {
		return TrackingSyncResult{}, ErrInvalidCandidate
	}
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return TrackingSyncResult{}, ErrInvalidCandidate
	}
	defer rootFS.Close()
	for index := range request.AcceptedTaskIDs {
		if request.AcceptedTaskIDs[index] != request.Candidates[index].TaskID {
			return TrackingSyncResult{}, ErrInvalidCandidate
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if request.ExpectedDigest != "" || request.ExpectedMetadataCommitSHA != "" {
		return s.verifyKnownResult(ctx, root, request)
	}
	desired, err := s.desiredFiles(ctx, root, request)
	if err != nil {
		return TrackingSyncResult{}, err
	}
	trackingDigest, err := trackingProjectionDigest(request, desired)
	if err != nil {
		return TrackingSyncResult{}, err
	}
	message := trackingCommitMessage(request.OperationID, request.RequestDigest, trackingDigest)
	headResult, err := s.run(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return TrackingSyncResult{}, ErrForeignState
	}
	head, err := singleGitSHA(headResult.Stdout)
	if err != nil {
		return TrackingSyncResult{}, ErrForeignState
	}
	if head != request.ExpectedHeadSHA {
		metadata, err := s.reconcileMetadata(ctx, root, request.ExpectedHeadSHA, head, message, desired)
		if err != nil {
			return TrackingSyncResult{}, err
		}
		return TrackingSyncResult{Digest: trackingDigest, MetadataCommitSHA: metadata}, nil
	}
	if err := s.validateOwnedPartial(ctx, root, rootFS, desired); err != nil {
		return TrackingSyncResult{}, err
	}
	for _, file := range desired {
		if err := ctx.Err(); err != nil {
			return TrackingSyncResult{}, err
		}
		if _, err := safeTrackingPath(file.Path); err != nil {
			return TrackingSyncResult{}, err
		}
		if err := writeTrackingFile(rootFS, file.Path, file.Content); err != nil {
			return TrackingSyncResult{}, err
		}
		if err := s.inject("after_write:" + file.Path); err != nil {
			return TrackingSyncResult{}, err
		}
		if _, err := s.run(ctx, root, "add", "--", file.Path); err != nil {
			return TrackingSyncResult{}, errors.New("integration: stage tracking failed")
		}
		if err := s.inject("after_add:" + file.Path); err != nil {
			return TrackingSyncResult{}, err
		}
	}
	stagedPaths, err := s.validatePreparedIndex(ctx, root, desired)
	if err != nil {
		return TrackingSyncResult{}, err
	}
	result := TrackingSyncResult{Digest: trackingDigest}
	if len(stagedPaths) == 0 {
		return result, nil
	}
	treeResult, err := s.run(ctx, root, "write-tree")
	if err != nil {
		return TrackingSyncResult{}, ErrApplyAmbiguous
	}
	tree, err := singleGitSHA(treeResult.Stdout)
	if err != nil {
		return TrackingSyncResult{}, ErrApplyAmbiguous
	}
	commitResult, err := s.runInput(ctx, root, []byte(message), metadataCommitEnvironment, "commit-tree", tree, "-p", request.ExpectedHeadSHA)
	if err != nil {
		return TrackingSyncResult{}, ErrApplyAmbiguous
	}
	metadata, err := singleGitSHA(commitResult.Stdout)
	if err != nil {
		return TrackingSyncResult{}, ErrApplyAmbiguous
	}
	if _, err := s.run(ctx, root, "update-ref", "HEAD", metadata, request.ExpectedHeadSHA); err != nil {
		return TrackingSyncResult{}, ErrApplyAmbiguous
	}
	if err := s.inject("after_commit"); err != nil {
		return TrackingSyncResult{}, err
	}
	if err := s.requireClean(ctx, root); err != nil {
		return TrackingSyncResult{}, ErrApplyAmbiguous
	}
	result.MetadataCommitSHA = metadata
	return result, nil
}

func (s *FileTrackingSynchronizer) desiredFiles(ctx context.Context, root string, request TrackingSyncRequest) ([]desiredTrackingFile, error) {
	byPath := make(map[string]desiredTrackingFile)
	total := 0
	add := func(path, expectedDigest string, content []byte) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		if len(path) == 0 || len(path) > TrackingPathLimit || !sha256Digest(expectedDigest) ||
			len(content) > VerificationLimit || digest(content) != expectedDigest {
			return ErrInvalidCandidate
		}
		if _, duplicate := byPath[path]; duplicate {
			return ErrInvalidCandidate
		}
		ignored, err := s.isIgnored(ctx, root, path)
		if err != nil {
			return err
		}
		if ignored {
			return nil
		}
		total += len(content)
		if len(byPath) >= TrackingFileLimit || total > TrackingBytesLimit {
			return ErrInvalidCandidate
		}
		byPath[path] = desiredTrackingFile{Path: path, Digest: expectedDigest, Content: append([]byte(nil), content...)}
		return nil
	}
	for _, candidate := range request.Candidates[:len(request.AcceptedTaskIDs)] {
		for _, tracking := range candidate.Tracking {
			if tracking.Ignored {
				continue
			}
			content, err := validatedTrackingContent(candidate, tracking)
			if err != nil {
				return nil, err
			}
			if err := add(tracking.Path, tracking.Digest, content); err != nil {
				return nil, err
			}
		}
	}
	for _, shared := range request.SharedTracking {
		if !validSharedTrackingPath(shared.Path) {
			return nil, ErrInvalidCandidate
		}
		if err := add(shared.Path, shared.Digest, shared.Content); err != nil {
			return nil, err
		}
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	desired := make([]desiredTrackingFile, 0, len(paths))
	for _, path := range paths {
		desired = append(desired, byPath[path])
	}
	return desired, nil
}

func (s *FileTrackingSynchronizer) validateOwnedPartial(
	ctx context.Context,
	root string,
	rootFS *os.Root,
	desired []desiredTrackingFile,
) error {
	owned := make(map[string]desiredTrackingFile, len(desired))
	for _, file := range desired {
		owned[file.Path] = file
	}
	commands := [][]string{
		{"diff", "--cached", "--name-only", "-z"},
		{"diff", "--name-only", "-z"},
		{"ls-files", "--others", "--exclude-standard", "-z"},
	}
	dirty := map[string]struct{}{}
	for _, args := range commands {
		result, err := s.run(ctx, root, args...)
		if err != nil {
			return ErrForeignState
		}
		paths, err := parseNULPaths(result.Stdout)
		if err != nil {
			return ErrForeignState
		}
		for _, path := range paths {
			dirty[path] = struct{}{}
		}
	}
	for path := range dirty {
		file, exists := owned[path]
		if !exists {
			return ErrForeignState
		}
		relative, err := safeTrackingPath(path)
		if err != nil {
			return err
		}
		content, err := rootFS.ReadFile(relative)
		if err != nil || !bytes.Equal(content, file.Content) {
			return ErrForeignState
		}
	}
	return nil
}

func (s *FileTrackingSynchronizer) validatePreparedIndex(ctx context.Context, root string, desired []desiredTrackingFile) ([]string, error) {
	unstaged, err := s.run(ctx, root, "diff", "--name-only", "-z")
	if err != nil || len(unstaged.Stdout) != 0 {
		return nil, ErrForeignState
	}
	untracked, err := s.run(ctx, root, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil || len(untracked.Stdout) != 0 {
		return nil, ErrForeignState
	}
	staged, err := s.run(ctx, root, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return nil, ErrForeignState
	}
	paths, err := parseNULPaths(staged.Stdout)
	if err != nil {
		return nil, ErrForeignState
	}
	allowed := make(map[string]struct{}, len(desired))
	for _, file := range desired {
		allowed[file.Path] = struct{}{}
	}
	for _, path := range paths {
		if _, exists := allowed[path]; !exists {
			return nil, ErrForeignState
		}
	}
	return paths, nil
}

func (s *FileTrackingSynchronizer) reconcileMetadata(
	ctx context.Context,
	root, expectedParent, head, message string,
	desired []desiredTrackingFile,
) (string, error) {
	parentResult, err := s.run(ctx, root, "rev-parse", "HEAD^")
	if err != nil {
		return "", ErrForeignState
	}
	parent, err := singleGitSHA(parentResult.Stdout)
	if err != nil || parent != expectedParent {
		return "", ErrForeignState
	}
	if err := s.verifyMetadataMessage(ctx, root, message); err != nil || s.requireClean(ctx, root) != nil {
		return "", ErrForeignState
	}
	changed, err := s.run(ctx, root, "diff-tree", "--no-commit-id", "--name-only", "-r", "-z", parent, head)
	if err != nil {
		return "", ErrForeignState
	}
	changedPaths, err := parseNULPaths(changed.Stdout)
	if err != nil {
		return "", ErrForeignState
	}
	owned := make(map[string]desiredTrackingFile, len(desired))
	for _, file := range desired {
		owned[file.Path] = file
		content, err := s.run(ctx, root, "show", head+":"+file.Path)
		if err != nil || !bytes.Equal(content.Stdout, file.Content) {
			return "", ErrForeignState
		}
	}
	for _, path := range changedPaths {
		if _, exists := owned[path]; !exists {
			return "", ErrForeignState
		}
	}
	treeResult, err := s.run(ctx, root, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return "", ErrForeignState
	}
	tree, err := singleGitSHA(treeResult.Stdout)
	if err != nil {
		return "", ErrForeignState
	}
	expected, err := s.runInput(ctx, root, []byte(message), metadataCommitEnvironment, "commit-tree", tree, "-p", parent)
	if err != nil {
		return "", ErrForeignState
	}
	expectedHead, err := singleGitSHA(expected.Stdout)
	if err != nil || expectedHead != head {
		return "", ErrForeignState
	}
	return head, nil
}

func (s *FileTrackingSynchronizer) verifyKnownResult(ctx context.Context, root string, request TrackingSyncRequest) (TrackingSyncResult, error) {
	if !sha256Digest(request.ExpectedDigest) || request.ExpectedMetadataCommitSHA != "" && !gitSHA(request.ExpectedMetadataCommitSHA) {
		return TrackingSyncResult{}, ErrInvalidCandidate
	}
	headResult, err := s.run(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return TrackingSyncResult{}, ErrForeignState
	}
	head, err := singleGitSHA(headResult.Stdout)
	if err != nil || s.requireClean(ctx, root) != nil {
		return TrackingSyncResult{}, ErrForeignState
	}
	result := TrackingSyncResult{Digest: request.ExpectedDigest, MetadataCommitSHA: request.ExpectedMetadataCommitSHA}
	if request.ExpectedMetadataCommitSHA == "" {
		if head != request.ExpectedHeadSHA {
			return TrackingSyncResult{}, ErrForeignState
		}
		return result, nil
	}
	if head != request.ExpectedMetadataCommitSHA {
		return TrackingSyncResult{}, ErrForeignState
	}
	parentResult, err := s.run(ctx, root, "rev-parse", "HEAD^")
	if err != nil {
		return TrackingSyncResult{}, ErrForeignState
	}
	parent, err := singleGitSHA(parentResult.Stdout)
	if err != nil || parent != request.ExpectedHeadSHA {
		return TrackingSyncResult{}, ErrForeignState
	}
	message := trackingCommitMessage(request.OperationID, request.RequestDigest, request.ExpectedDigest)
	if err := s.verifyMetadataMessage(ctx, root, message); err != nil {
		return TrackingSyncResult{}, ErrForeignState
	}
	treeResult, err := s.run(ctx, root, "rev-parse", "HEAD^{tree}")
	if err != nil {
		return TrackingSyncResult{}, ErrForeignState
	}
	tree, err := singleGitSHA(treeResult.Stdout)
	if err != nil {
		return TrackingSyncResult{}, ErrForeignState
	}
	expected, err := s.runInput(ctx, root, []byte(message), metadataCommitEnvironment, "commit-tree", tree, "-p", parent)
	if err != nil {
		return TrackingSyncResult{}, ErrForeignState
	}
	expectedHead, err := singleGitSHA(expected.Stdout)
	if err != nil || expectedHead != head {
		return TrackingSyncResult{}, ErrForeignState
	}
	return result, nil
}

func (s *FileTrackingSynchronizer) verifyMetadataMessage(ctx context.Context, root, expected string) error {
	message, err := s.run(ctx, root, "log", "-1", "--format=%B", "HEAD")
	if err != nil || strings.TrimSpace(string(message.Stdout)) != strings.TrimSpace(expected) {
		return ErrForeignState
	}
	return nil
}

func (s *FileTrackingSynchronizer) requireClean(ctx context.Context, root string) error {
	status, err := s.run(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || len(status.Stdout) != 0 {
		return ErrForeignState
	}
	return nil
}

func (s *FileTrackingSynchronizer) isIgnored(ctx context.Context, root, path string) (bool, error) {
	result, err := s.run(ctx, root, "check-ignore", "-q", "--", path)
	if err == nil {
		return true, nil
	}
	if result.ExitCode == 1 {
		return false, nil
	}
	return false, errors.New("integration: inspect tracking ignore state failed")
}

func (s *FileTrackingSynchronizer) run(ctx context.Context, directory string, args ...string) (publication.CommandResult, error) {
	return s.runInput(ctx, directory, nil, nil, args...)
}

func (s *FileTrackingSynchronizer) runInput(
	ctx context.Context,
	directory string,
	stdin []byte,
	environment []string,
	args ...string,
) (publication.CommandResult, error) {
	result, err := s.Runner.Run(ctx, publication.Command{
		Executable: s.GitExecutable, Args: append([]string(nil), args...), Directory: directory,
		Stdin: append([]byte(nil), stdin...), Environment: append([]string(nil), environment...),
		StdoutLimit: GitStdoutLimit, StderrLimit: GitStderrLimit,
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		return result, err
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return result, errors.New("integration: Git tracking output exceeded bound")
	}
	return result, nil
}

func (s *FileTrackingSynchronizer) inject(point string) error {
	if s.Fault == nil {
		return nil
	}
	return s.Fault(point)
}

func validatedTrackingContent(candidate CandidateEvidence, tracking TrackingFile) ([]byte, error) {
	prefix := ".compozy/tasks/" + candidate.Slug + "/"
	if !strings.HasPrefix(tracking.Path, prefix) || tracking.Path == prefix || !sha256Digest(tracking.Digest) {
		return nil, ErrInvalidCandidate
	}
	relative, err := safeTrackingPath(tracking.Path)
	if err != nil {
		return nil, err
	}
	root, err := canonicalDirectory(candidate.WorktreeRoot)
	if err != nil || root != candidate.WorktreeRoot {
		return nil, ErrInvalidCandidate
	}
	rootFS, err := os.OpenRoot(root)
	if err != nil {
		return nil, ErrInvalidCandidate
	}
	defer rootFS.Close()
	info, err := rootFS.Lstat(relative)
	if err != nil || !info.Mode().IsRegular() || info.Size() > VerificationLimit {
		return nil, ErrInvalidCandidate
	}
	content, err := rootFS.ReadFile(relative)
	if err != nil || digest(content) != tracking.Digest {
		return nil, ErrInvalidCandidate
	}
	return content, nil
}

func safeTrackingPath(relative string) (string, error) {
	clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(relative)))
	if relative == "" || clean != relative || filepath.IsAbs(relative) || relative == ".." || strings.HasPrefix(relative, "../") {
		return "", ErrInvalidCandidate
	}
	return filepath.FromSlash(relative), nil
}

func writeTrackingFile(root *os.Root, path string, content []byte) error {
	relative, err := safeTrackingPath(path)
	if err != nil || root == nil {
		return ErrInvalidCandidate
	}
	directory := filepath.Dir(relative)
	if err := root.MkdirAll(directory, 0o700); err != nil {
		return errors.New("integration: create tracking directory failed")
	}
	if info, err := root.Lstat(relative); err == nil && !info.Mode().IsRegular() {
		return ErrInvalidCandidate
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return ErrInvalidCandidate
	}
	random := make([]byte, 16)
	if _, err := rand.Read(random); err != nil {
		return errors.New("integration: generate tracking temporary failed")
	}
	temporaryPath := filepath.Join(directory, ".batuta-tracking-"+hex.EncodeToString(random))
	temporary, err := root.OpenFile(temporaryPath, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return errors.New("integration: create tracking temporary failed")
	}
	defer root.Remove(temporaryPath)
	if err := temporary.Chmod(0o600); err != nil {
		_ = temporary.Close()
		return errors.New("integration: secure tracking temporary failed")
	}
	if _, err := temporary.Write(content); err != nil {
		_ = temporary.Close()
		return errors.New("integration: write tracking temporary failed")
	}
	if err := temporary.Sync(); err != nil {
		_ = temporary.Close()
		return errors.New("integration: sync tracking temporary failed")
	}
	if err := temporary.Close(); err != nil {
		return errors.New("integration: close tracking temporary failed")
	}
	if err := root.Rename(temporaryPath, relative); err != nil {
		return errors.New("integration: replace tracking file failed")
	}
	return nil
}

func validSharedTrackingPath(path string) bool {
	return strings.HasPrefix(path, ".compozy/") && path != ".compozy/" &&
		filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) == path
}

func trackingProjectionDigest(request TrackingSyncRequest, desired []desiredTrackingFile) (string, error) {
	type manifestFile struct {
		Path   string `json:"path"`
		Digest string `json:"digest"`
	}
	type acceptedCandidate struct {
		TaskID             string `json:"task_id"`
		CommitSHA          string `json:"commit_sha"`
		VerificationDigest string `json:"verification_digest"`
	}
	payload := struct {
		OperationID        string              `json:"operation_id"`
		RequestDigest      string              `json:"request_digest"`
		ProjectionRevision string              `json:"projection_revision"`
		Accepted           []acceptedCandidate `json:"accepted"`
		Files              []manifestFile      `json:"files"`
	}{OperationID: request.OperationID, RequestDigest: request.RequestDigest,
		ProjectionRevision: request.ProjectionRevision,
		Accepted:           make([]acceptedCandidate, 0, len(request.AcceptedTaskIDs)), Files: make([]manifestFile, 0, len(desired))}
	for index := range request.AcceptedTaskIDs {
		candidate := request.Candidates[index]
		payload.Accepted = append(payload.Accepted, acceptedCandidate{candidate.TaskID, candidate.CommitSHA, candidate.VerificationDigest})
	}
	for _, file := range desired {
		payload.Files = append(payload.Files, manifestFile{file.Path, file.Digest})
	}
	encoded, err := canonicalJSON(payload)
	if err != nil {
		return "", err
	}
	return digest(encoded), nil
}

func trackingCommitMessage(operationID, requestDigest, trackingDigest string) string {
	return fmt.Sprintf("chore: sync Batuta task tracking\n\nBatuta-Operation: %s\nBatuta-Request: %s\nBatuta-Tracking: %s\n", operationID, requestDigest, trackingDigest)
}

func canonicalJSON(value any) ([]byte, error) {
	var buffer bytes.Buffer
	encoder := json.NewEncoder(&buffer)
	encoder.SetEscapeHTML(false)
	if err := encoder.Encode(value); err != nil {
		return nil, errors.New("integration: encode tracking identity failed")
	}
	return bytes.TrimSpace(buffer.Bytes()), nil
}
