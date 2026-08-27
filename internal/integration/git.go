package integration

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

var conventionalCommitSubject = regexp.MustCompile(`^[a-z][a-z0-9-]*(\([A-Za-z0-9][A-Za-z0-9._/-]*\))?!?: [^[:space:]].*$`)

type GitClient struct {
	Executable         string
	Runner             publication.CommandRunner
	scratchRootForTest string
}

func (c GitClient) Candidate(ctx context.Context, request CandidateRequest) (CandidateEvidence, error) {
	if err := ctx.Err(); err != nil {
		return CandidateEvidence{}, err
	}
	verification, err := validateVerification(request.TaskID, request.Verification, request.VerificationDigest)
	allowedTracking, trackingErr := validateAllowedTrackingPaths(request.Slug, request.AllowedTrackingPaths)
	if err != nil || !canonicalTaskID(request.TaskID) || !canonicalSlug(request.Slug) ||
		!canonicalBranch(request.ExpectedBranch) || !gitSHA(request.BaseSHA) || trackingErr != nil {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	worktreeRoot, err := canonicalDirectory(request.WorktreeRoot)
	if err != nil || worktreeRoot != request.WorktreeRoot {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	repositoryRoot, err := canonicalDirectory(request.RepositoryRoot)
	if err != nil || repositoryRoot != request.RepositoryRoot || repositoryRoot == worktreeRoot {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	top, err := c.run(ctx, worktreeRoot, "rev-parse", "--show-toplevel")
	if err != nil || strings.TrimSpace(string(top.Stdout)) != worktreeRoot {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	worktreeCommon, err := c.commonDir(ctx, worktreeRoot)
	if err != nil {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	repositoryCommon, err := c.commonDir(ctx, repositoryRoot)
	if err != nil || repositoryCommon != worktreeCommon {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	headResult, err := c.run(ctx, worktreeRoot, "rev-parse", "HEAD")
	if err != nil {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	head, err := singleGitSHA(headResult.Stdout)
	if err != nil {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	branchResult, err := c.run(ctx, worktreeRoot, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil || strings.TrimSpace(string(branchResult.Stdout)) != request.ExpectedBranch {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	commitsResult, err := c.run(ctx, worktreeRoot, "rev-list", "--reverse", "--ancestry-path", request.BaseSHA+"..HEAD")
	if err != nil {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	commits := nonemptyLines(commitsResult.Stdout)
	if len(commits) != 1 || commits[0] != head || !gitSHA(commits[0]) {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	parentsResult, err := c.run(ctx, worktreeRoot, "rev-list", "--parents", "-n", "1", head)
	if err != nil {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	parents := strings.Fields(strings.TrimSpace(string(parentsResult.Stdout)))
	if len(parents) != 2 || parents[0] != head || parents[1] != request.BaseSHA {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	subjectResult, err := c.run(ctx, worktreeRoot, "show", "-s", "--format=%s", head)
	if err != nil || !validConventionalCommitSubject(subjectResult.Stdout) {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	treeResult, err := c.run(ctx, worktreeRoot, "rev-parse", head+"^{tree}")
	if err != nil {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	tree, err := singleGitSHA(treeResult.Stdout)
	if err != nil {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	committedTrackingPaths, committedTrackingBytes, err := c.validateCommittedPaths(ctx, worktreeRoot, head, allowedTracking)
	if err != nil {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	worktreeFS, err := os.OpenRoot(worktreeRoot)
	if err != nil {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	defer worktreeFS.Close()
	tracking, err := c.trackingEvidence(ctx, worktreeFS, allowedTracking, len(committedTrackingPaths), committedTrackingBytes)
	if err != nil {
		return CandidateEvidence{}, ErrInvalidCandidate
	}
	ownedTrackingPaths := append([]string{}, committedTrackingPaths...)
	for _, file := range tracking {
		if !slices.Contains(ownedTrackingPaths, file.Path) {
			ownedTrackingPaths = append(ownedTrackingPaths, file.Path)
		}
	}
	slices.Sort(ownedTrackingPaths)
	repositoryIdentity := sha256.Sum256([]byte(repositoryCommon))
	return CandidateEvidence{
		TaskID: request.TaskID, Slug: request.Slug, WorktreeRoot: worktreeRoot,
		RepositoryIdentity: "sha256:" + hex.EncodeToString(repositoryIdentity[:]),
		Branch:             request.ExpectedBranch, BaseSHA: request.BaseSHA, CommitSHA: head, TreeSHA: tree,
		VerificationDigest: verification, OwnedTrackingPaths: ownedTrackingPaths, Tracking: tracking,
	}, nil
}

func (c GitClient) Preflight(ctx context.Context, request PreflightRequest) (result PreflightResult, resultErr error) {
	if err := ctx.Err(); err != nil {
		return PreflightResult{}, err
	}
	if !sha256Digest(request.OperationID) || !sha256Digest(request.RequestDigest) ||
		!gitSHA(request.StartingHeadSHA) || len(request.Candidates) == 0 || len(request.Candidates) > 4 {
		return PreflightResult{}, ErrInvalidCandidate
	}
	integrationRoot, err := canonicalDirectory(request.IntegrationRoot)
	if err != nil || integrationRoot != request.IntegrationRoot {
		return PreflightResult{}, ErrInvalidCandidate
	}
	if err := validateCandidateSequence(request.Candidates); err != nil {
		return PreflightResult{}, err
	}
	integrationIdentity, err := c.repositoryIdentity(ctx, integrationRoot)
	if err != nil || integrationIdentity != request.Candidates[0].RepositoryIdentity {
		return PreflightResult{}, ErrForeignState
	}
	status, err := c.run(ctx, integrationRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || len(status.Stdout) != 0 {
		return PreflightResult{}, ErrForeignState
	}
	headResult, err := c.run(ctx, integrationRoot, "rev-parse", "HEAD")
	if err != nil {
		return PreflightResult{}, ErrForeignState
	}
	head, err := singleGitSHA(headResult.Stdout)
	if err != nil || head != request.StartingHeadSHA {
		return PreflightResult{}, ErrForeignState
	}
	scratch, err := c.secureScratchRoot(ctx, integrationRoot)
	if err != nil {
		return PreflightResult{}, err
	}
	operationRoot, err := os.MkdirTemp(scratch, "operation-")
	if err != nil {
		return PreflightResult{}, errors.New("integration: create preflight scratch failed")
	}
	worktreeRoot := filepath.Join(operationRoot, "worktree")
	added := false
	defer func() {
		cleanupErr := c.cleanupPreflight(integrationRoot, operationRoot, worktreeRoot, added)
		if resultErr == nil && cleanupErr != nil {
			result = PreflightResult{}
			resultErr = cleanupErr
		}
	}()
	if _, err := c.run(ctx, integrationRoot, "worktree", "add", "--detach", worktreeRoot, request.StartingHeadSHA); err != nil {
		return PreflightResult{}, errors.New("integration: create disposable preflight worktree failed")
	}
	added = true
	result = PreflightResult{
		OperationID: request.OperationID, RequestDigest: request.RequestDigest,
		StartingHeadSHA: request.StartingHeadSHA, AcceptedTaskIDs: []string{},
		AcceptedCommitSHAs: []string{}, AcceptedResultTreeSHAs: []string{}, AcceptedResultCommitSHAs: []string{},
		ResultingHeadSHA: request.StartingHeadSHA,
	}
	for _, candidate := range request.Candidates {
		if err := ctx.Err(); err != nil {
			return PreflightResult{}, err
		}
		applied, conflictEvidence, cherryPickErr := c.applyDeterministic(ctx, deterministicApplyRequest{
			Root: worktreeRoot, OperationID: request.OperationID, RequestDigest: request.RequestDigest,
			TaskID: candidate.TaskID, CandidateCommitSHA: candidate.CommitSHA, Disposable: true,
		})
		if cherryPickErr != nil {
			if ctxErr := ctx.Err(); ctxErr != nil {
				return PreflightResult{}, ctxErr
			}
			if conflictEvidence == "" {
				return PreflightResult{}, ErrPreflightFailed
			}
			result.FirstConflictTaskID = candidate.TaskID
			result.ConflictEvidenceDigest = conflictEvidence
			return result, nil
		}
		result.AcceptedTaskIDs = append(result.AcceptedTaskIDs, candidate.TaskID)
		result.AcceptedCommitSHAs = append(result.AcceptedCommitSHAs, candidate.CommitSHA)
		result.AcceptedResultTreeSHAs = append(result.AcceptedResultTreeSHAs, applied.TreeSHA)
		result.AcceptedResultCommitSHAs = append(result.AcceptedResultCommitSHAs, applied.CommitSHA)
		result.ResultingHeadSHA = applied.CommitSHA
	}
	return result, nil
}

func (c GitClient) Apply(ctx context.Context, request ApplyRequest) (ApplyResult, error) {
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, err
	}
	if !sha256Digest(request.OperationID) || !sha256Digest(request.RequestDigest) ||
		!gitSHA(request.ExpectedHeadSHA) || !canonicalTaskID(request.TaskID) ||
		!gitSHA(request.CandidateCommitSHA) || !gitSHA(request.ExpectedResultTreeSHA) {
		return ApplyResult{}, ErrInvalidCandidate
	}
	if !gitSHA(request.ExpectedResultCommitSHA) {
		return ApplyResult{}, ErrInvalidCandidate
	}
	root, err := canonicalDirectory(request.IntegrationRoot)
	if err != nil || root != request.IntegrationRoot {
		return ApplyResult{}, ErrInvalidCandidate
	}
	status, err := c.run(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || len(status.Stdout) != 0 {
		return ApplyResult{}, ErrForeignState
	}
	headResult, err := c.run(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return ApplyResult{}, ErrForeignState
	}
	head, err := singleGitSHA(headResult.Stdout)
	if err != nil || head != request.ExpectedHeadSHA {
		return ApplyResult{}, ErrForeignState
	}
	applied, _, err := c.applyDeterministic(ctx, deterministicApplyRequest{
		Root: root, OperationID: request.OperationID, RequestDigest: request.RequestDigest,
		TaskID: request.TaskID, CandidateCommitSHA: request.CandidateCommitSHA,
		ExpectedTreeSHA: request.ExpectedResultTreeSHA, ExpectedCommitSHA: request.ExpectedResultCommitSHA,
	})
	if err != nil || applied.TreeSHA != request.ExpectedResultTreeSHA || applied.CommitSHA != request.ExpectedResultCommitSHA {
		return ApplyResult{}, ErrApplyAmbiguous
	}
	return ApplyResult{
		StartingHeadSHA: request.ExpectedHeadSHA, AcceptedTaskIDs: []string{request.TaskID},
		AcceptedCommitSHAs: []string{request.CandidateCommitSHA}, ResultingHeadSHA: applied.CommitSHA,
	}, nil
}

func (c GitClient) Reconcile(ctx context.Context, request ReconcileRequest) (ApplyResult, error) {
	if err := ctx.Err(); err != nil {
		return ApplyResult{}, err
	}
	if err := validatePreflightResult(request.Preflight); err != nil {
		return ApplyResult{}, err
	}
	root, err := canonicalDirectory(request.IntegrationRoot)
	if err != nil || root != request.IntegrationRoot {
		return ApplyResult{}, ErrInvalidCandidate
	}
	status, err := c.run(ctx, root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || len(status.Stdout) != 0 {
		return ApplyResult{}, ErrForeignState
	}
	headResult, err := c.run(ctx, root, "rev-parse", "HEAD")
	if err != nil {
		return ApplyResult{}, ErrForeignState
	}
	head, err := singleGitSHA(headResult.Stdout)
	if err != nil {
		return ApplyResult{}, ErrForeignState
	}
	result := ApplyResult{StartingHeadSHA: request.Preflight.StartingHeadSHA, ResultingHeadSHA: head,
		AcceptedTaskIDs: []string{}, AcceptedCommitSHAs: []string{}}
	if head == request.Preflight.StartingHeadSHA {
		return result, nil
	}
	commitsResult, err := c.run(ctx, root, "rev-list", "--reverse", "--ancestry-path", request.Preflight.StartingHeadSHA+"..HEAD")
	if err != nil {
		return ApplyResult{}, ErrForeignState
	}
	commits := nonemptyLines(commitsResult.Stdout)
	if len(commits) == 0 || len(commits) > len(request.Preflight.AcceptedTaskIDs) || commits[len(commits)-1] != head ||
		!slices.Equal(commits, request.Preflight.AcceptedResultCommitSHAs[:len(commits)]) {
		return ApplyResult{}, ErrForeignState
	}
	for index, commit := range commits {
		if !gitSHA(commit) {
			return ApplyResult{}, ErrForeignState
		}
		treeResult, err := c.run(ctx, root, "rev-parse", commit+"^{tree}")
		if err != nil {
			return ApplyResult{}, ErrForeignState
		}
		tree, err := singleGitSHA(treeResult.Stdout)
		if err != nil || tree != request.Preflight.AcceptedResultTreeSHAs[index] {
			return ApplyResult{}, ErrForeignState
		}
	}
	result.AcceptedTaskIDs = append(result.AcceptedTaskIDs, request.Preflight.AcceptedTaskIDs[:len(commits)]...)
	result.AcceptedCommitSHAs = append(result.AcceptedCommitSHAs, request.Preflight.AcceptedCommitSHAs[:len(commits)]...)
	return result, nil
}

func validatePreflightResult(result PreflightResult) error {
	accepted := len(result.AcceptedTaskIDs)
	if !sha256Digest(result.OperationID) || !sha256Digest(result.RequestDigest) || !gitSHA(result.StartingHeadSHA) ||
		!gitSHA(result.ResultingHeadSHA) || accepted != len(result.AcceptedCommitSHAs) ||
		accepted != len(result.AcceptedResultTreeSHAs) || accepted != len(result.AcceptedResultCommitSHAs) || accepted > 4 {
		return ErrInvalidCandidate
	}
	for index, taskID := range result.AcceptedTaskIDs {
		if !canonicalTaskID(taskID) || !gitSHA(result.AcceptedCommitSHAs[index]) ||
			!gitSHA(result.AcceptedResultTreeSHAs[index]) || !gitSHA(result.AcceptedResultCommitSHAs[index]) {
			return ErrInvalidCandidate
		}
	}
	if accepted == 0 && result.ResultingHeadSHA != result.StartingHeadSHA {
		return ErrInvalidCandidate
	}
	if accepted > 0 && result.ResultingHeadSHA != result.AcceptedResultCommitSHAs[accepted-1] {
		return ErrInvalidCandidate
	}
	if result.FirstConflictTaskID != "" {
		if !canonicalTaskID(result.FirstConflictTaskID) || !sha256Digest(result.ConflictEvidenceDigest) {
			return ErrInvalidCandidate
		}
	} else if result.ConflictEvidenceDigest != "" {
		return ErrInvalidCandidate
	}
	return nil
}

func (c GitClient) trackingEvidence(
	ctx context.Context,
	root *os.Root,
	allowed map[string]struct{},
	initialFiles int,
	initialBytes int64,
) ([]TrackingFile, error) {
	type pathSource struct {
		args    []string
		ignored bool
	}
	sources := []pathSource{
		{args: []string{"diff", "--name-only", "-z", "HEAD"}},
		{args: []string{"ls-files", "--others", "--exclude-standard", "-z"}},
		{args: []string{"ls-files", "--others", "--ignored", "--exclude-standard", "-z"}, ignored: true},
	}
	byPath := map[string]TrackingFile{}
	totalBytes := initialBytes
	for _, source := range sources {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		result, err := c.run(ctx, root.Name(), source.args...)
		if err != nil {
			return nil, err
		}
		paths, err := parseNULPaths(result.Stdout)
		if err != nil {
			return nil, err
		}
		for _, path := range paths {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if len(path) > TrackingPathLimit {
				return nil, ErrInvalidCandidate
			}
			if _, accepted := allowed[path]; !accepted {
				return nil, ErrInvalidCandidate
			}
			relative := filepath.FromSlash(path)
			info, err := root.Lstat(relative)
			if err != nil || !info.Mode().IsRegular() {
				return nil, ErrInvalidCandidate
			}
			content, err := root.ReadFile(relative)
			if err != nil || len(content) > VerificationLimit {
				return nil, ErrInvalidCandidate
			}
			totalBytes += int64(len(content))
			if initialFiles+len(byPath) >= TrackingFileLimit || totalBytes > TrackingBytesLimit {
				return nil, ErrInvalidCandidate
			}
			candidate := TrackingFile{Path: path, Digest: digest(content), Ignored: source.ignored}
			if prior, exists := byPath[path]; exists && prior != candidate {
				return nil, ErrInvalidCandidate
			}
			byPath[path] = candidate
		}
	}
	paths := make([]string, 0, len(byPath))
	for path := range byPath {
		paths = append(paths, path)
	}
	slices.Sort(paths)
	tracking := make([]TrackingFile, 0, len(paths))
	for _, path := range paths {
		tracking = append(tracking, byPath[path])
	}
	return tracking, nil
}

func (c GitClient) validateCommittedPaths(
	ctx context.Context,
	root, head string,
	allowed map[string]struct{},
) ([]string, int64, error) {
	result, err := c.run(ctx, root, "diff-tree", "--no-commit-id", "--name-only", "-r", "-z", head)
	if err != nil {
		return nil, 0, err
	}
	paths, err := parseNULPaths(result.Stdout)
	if err != nil {
		return nil, 0, err
	}
	totalBytes := int64(0)
	trackingPaths := make([]string, 0, len(paths))
	for _, path := range paths {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if !strings.HasPrefix(path, ".compozy/") {
			continue
		}
		if len(path) > TrackingPathLimit {
			return nil, 0, ErrInvalidCandidate
		}
		if _, accepted := allowed[path]; !accepted {
			return nil, 0, ErrInvalidCandidate
		}
		size, err := c.committedRegularBlobSize(ctx, root, head, path)
		if err != nil || size > VerificationLimit {
			return nil, 0, ErrInvalidCandidate
		}
		trackingPaths = append(trackingPaths, path)
		totalBytes += size
		if len(trackingPaths) > TrackingFileLimit || totalBytes > TrackingBytesLimit {
			return nil, 0, ErrInvalidCandidate
		}
	}
	slices.Sort(trackingPaths)
	return trackingPaths, totalBytes, nil
}

func (c GitClient) committedRegularBlobSize(ctx context.Context, root, head, path string) (int64, error) {
	result, err := c.run(ctx, root, "ls-tree", "-z", "-l", "--full-tree", head, "--", path)
	if err != nil || len(result.Stdout) == 0 || result.Stdout[len(result.Stdout)-1] != 0 {
		return 0, ErrInvalidCandidate
	}
	entry := result.Stdout[:len(result.Stdout)-1]
	if bytes.ContainsRune(entry, 0) {
		return 0, ErrInvalidCandidate
	}
	metadata, entryPath, found := bytes.Cut(entry, []byte{'\t'})
	fields := strings.Fields(string(metadata))
	if !found || string(entryPath) != path || len(fields) != 4 ||
		(fields[0] != "100644" && fields[0] != "100755") || fields[1] != "blob" || !gitSHA(fields[2]) {
		return 0, ErrInvalidCandidate
	}
	size, err := strconv.ParseInt(fields[3], 10, 64)
	if err != nil || size < 0 {
		return 0, ErrInvalidCandidate
	}
	return size, nil
}

func validConventionalCommitSubject(payload []byte) bool {
	subject := strings.TrimSuffix(string(payload), "\n")
	return subject != "" && !strings.ContainsAny(subject, "\r\n\x00") && conventionalCommitSubject.MatchString(subject)
}

func (c GitClient) commonDir(ctx context.Context, root string) (string, error) {
	result, err := c.run(ctx, root, "rev-parse", "--git-common-dir")
	if err != nil {
		return "", err
	}
	common := strings.TrimSpace(string(result.Stdout))
	if common == "" || strings.ContainsRune(common, '\x00') {
		return "", ErrInvalidCandidate
	}
	if !filepath.IsAbs(common) {
		common = filepath.Join(root, common)
	}
	resolved, err := filepath.EvalSymlinks(filepath.Clean(common))
	if err != nil {
		return "", ErrInvalidCandidate
	}
	return resolved, nil
}

func (c GitClient) repositoryIdentity(ctx context.Context, root string) (string, error) {
	common, err := c.commonDir(ctx, root)
	if err != nil {
		return "", err
	}
	value := sha256.Sum256([]byte(common))
	return "sha256:" + hex.EncodeToString(value[:]), nil
}

type deterministicApplyRequest struct {
	Root               string
	OperationID        string
	RequestDigest      string
	TaskID             string
	CandidateCommitSHA string
	ExpectedTreeSHA    string
	ExpectedCommitSHA  string
	Disposable         bool
}

type deterministicApplyResult struct {
	CommitSHA string
	TreeSHA   string
}

func (c GitClient) applyDeterministic(
	ctx context.Context,
	request deterministicApplyRequest,
) (deterministicApplyResult, string, error) {
	message, environment, err := c.integrationCommitMaterial(ctx, request)
	if err != nil {
		return deterministicApplyResult{}, "", err
	}
	parentResult, err := c.run(ctx, request.Root, "rev-parse", "HEAD")
	if err != nil {
		return deterministicApplyResult{}, "", err
	}
	parent, err := singleGitSHA(parentResult.Stdout)
	if err != nil {
		return deterministicApplyResult{}, "", err
	}
	commandResult, cherryPickErr := c.run(ctx, request.Root, "cherry-pick", "--no-commit", request.CandidateCommitSHA)
	if cherryPickErr != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return deterministicApplyResult{}, "", ctxErr
		}
		conflicted := c.hasCherryPickConflict(ctx, request.Root)
		proof := ""
		if conflicted {
			proof = digest(append([]byte(request.TaskID+"\x00"+request.CandidateCommitSHA+"\x00"), commandResult.Stderr...))
		}
		if request.Disposable {
			cleanupCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), 10*time.Second)
			_, _ = c.run(cleanupCtx, request.Root, "cherry-pick", "--abort")
			cancel()
		}
		return deterministicApplyResult{}, proof, ErrPreflightFailed
	}
	treeResult, err := c.run(ctx, request.Root, "write-tree")
	if err != nil {
		return deterministicApplyResult{}, "", err
	}
	tree, err := singleGitSHA(treeResult.Stdout)
	if err != nil || request.ExpectedTreeSHA != "" && tree != request.ExpectedTreeSHA {
		return deterministicApplyResult{}, "", ErrForeignState
	}
	commitResult, err := c.runInput(ctx, request.Root, message, environment, "commit-tree", tree, "-p", parent)
	if err != nil {
		return deterministicApplyResult{}, "", err
	}
	commit, err := singleGitSHA(commitResult.Stdout)
	if err != nil || request.ExpectedCommitSHA != "" && commit != request.ExpectedCommitSHA {
		return deterministicApplyResult{}, "", ErrForeignState
	}
	if _, err := c.run(ctx, request.Root, "update-ref", "HEAD", commit, parent); err != nil {
		return deterministicApplyResult{}, "", err
	}
	status, err := c.run(ctx, request.Root, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil || len(status.Stdout) != 0 {
		return deterministicApplyResult{}, "", ErrForeignState
	}
	return deterministicApplyResult{CommitSHA: commit, TreeSHA: tree}, "", nil
}

func (c GitClient) hasCherryPickConflict(ctx context.Context, root string) bool {
	unmerged, err := c.run(ctx, root, "diff", "--name-only", "--diff-filter=U", "-z")
	if err != nil {
		return false
	}
	paths, err := parseNULPaths(unmerged.Stdout)
	return err == nil && len(paths) > 0
}

func (c GitClient) integrationCommitMaterial(
	ctx context.Context,
	request deterministicApplyRequest,
) ([]byte, []string, error) {
	metadata, err := c.run(ctx, request.Root, "show", "-s", "--format=%an%x00%ae%x00%aI%x00%B", request.CandidateCommitSHA)
	if err != nil || len(metadata.Stdout) == 0 || len(metadata.Stdout) > VerificationLimit {
		return nil, nil, ErrInvalidCandidate
	}
	parts := bytes.SplitN(metadata.Stdout, []byte{0}, 4)
	if len(parts) != 4 {
		return nil, nil, ErrInvalidCandidate
	}
	name, email, authoredAt, original := strings.TrimSpace(string(parts[0])), strings.TrimSpace(string(parts[1])), strings.TrimSpace(string(parts[2])), string(parts[3])
	if name == "" || email == "" || strings.ContainsAny(name+email, "\x00\r\n") {
		return nil, nil, ErrInvalidCandidate
	}
	if _, err := time.Parse(time.RFC3339, authoredAt); err != nil {
		return nil, nil, ErrInvalidCandidate
	}
	for _, marker := range []string{"Batuta-Operation:", "Batuta-Request:", "Batuta-Task:", "Batuta-Candidate:"} {
		if strings.Contains(original, marker) {
			return nil, nil, ErrInvalidCandidate
		}
	}
	message := strings.TrimRight(original, "\r\n") + "\n\n" +
		"Batuta-Operation: " + request.OperationID + "\n" +
		"Batuta-Request: " + request.RequestDigest + "\n" +
		"Batuta-Task: " + request.TaskID + "\n" +
		"Batuta-Candidate: " + request.CandidateCommitSHA + "\n"
	environment := []string{
		"GIT_AUTHOR_NAME=" + name,
		"GIT_AUTHOR_EMAIL=" + email,
		"GIT_AUTHOR_DATE=" + authoredAt,
		"GIT_COMMITTER_NAME=Batuta",
		"GIT_COMMITTER_EMAIL=batuta@example.invalid",
		"GIT_COMMITTER_DATE=" + authoredAt,
	}
	return []byte(message), environment, nil
}

func (c GitClient) run(ctx context.Context, directory string, args ...string) (publication.CommandResult, error) {
	return c.runInput(ctx, directory, nil, nil, args...)
}

func (c GitClient) runInput(
	ctx context.Context,
	directory string,
	stdin []byte,
	environment []string,
	args ...string,
) (publication.CommandResult, error) {
	if err := ctx.Err(); err != nil {
		return publication.CommandResult{}, err
	}
	if c.Runner == nil || !filepath.IsAbs(c.Executable) || filepath.Clean(c.Executable) != c.Executable ||
		!filepath.IsAbs(directory) || filepath.Clean(directory) != directory {
		return publication.CommandResult{}, ErrInvalidCandidate
	}
	result, err := c.Runner.Run(ctx, publication.Command{
		Executable: c.Executable, Args: append([]string(nil), args...), Directory: directory,
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
		return result, errors.New("integration: Git output exceeded bound")
	}
	return result, nil
}

func (c GitClient) secureScratchRoot(ctx context.Context, repositoryRoot string) (string, error) {
	root := strings.TrimSpace(c.scratchRootForTest)
	if root == "" {
		cache, err := os.UserCacheDir()
		if err != nil {
			return "", errors.New("integration: cache directory unavailable")
		}
		if err := os.MkdirAll(cache, 0o700); err != nil {
			return "", errors.New("integration: create user cache directory failed")
		}
		if _, err := canonicalDirectory(cache); err != nil {
			return "", errors.New("integration: user cache directory is not canonical")
		}
		batutaRoot := filepath.Join(cache, "batuta")
		if err := ensurePrivateDirectory(batutaRoot); err != nil {
			return "", err
		}
		root = filepath.Join(batutaRoot, "integration")
	} else {
		temporary, err := canonicalDirectory(os.TempDir())
		if err != nil || !pathContained(temporary, root) || root == temporary {
			return "", errors.New("integration: test scratch root must stay under the system temporary directory")
		}
	}
	if !filepath.IsAbs(root) || filepath.Clean(root) != root {
		return "", errors.New("integration: scratch root must be canonical and absolute")
	}
	common, err := c.commonDir(ctx, repositoryRoot)
	if err != nil || pathsOverlap(repositoryRoot, root) || pathsOverlap(common, root) {
		return "", errors.New("integration: scratch root must stay outside repository")
	}
	if err := ensurePrivateDirectory(root); err != nil {
		return "", err
	}
	canonical, err := canonicalDirectory(root)
	if err != nil || canonical != root {
		return "", errors.New("integration: scratch root must not traverse symlinks")
	}
	return root, nil
}

func ensurePrivateDirectory(path string) error {
	if err := os.Mkdir(path, 0o700); err != nil && !errors.Is(err, os.ErrExist) {
		return errors.New("integration: create private scratch directory failed")
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return errors.New("integration: scratch directory is not private")
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return errors.New("integration: scratch root must not traverse symlinks")
	}
	return nil
}

func pathsOverlap(first, second string) bool {
	return pathContained(first, second) || pathContained(second, first)
}

func pathContained(root, candidate string) bool {
	relative, err := filepath.Rel(root, candidate)
	return err == nil && relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator)) && !filepath.IsAbs(relative)
}

func (c GitClient) cleanupPreflight(integrationRoot, operationRoot, worktreeRoot string, added bool) error {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if added {
		_, _ = c.run(ctx, worktreeRoot, "cherry-pick", "--abort")
		if _, err := c.run(ctx, worktreeRoot, "read-tree", "--reset", "-u", "HEAD"); err != nil {
			return errors.New("integration: restore disposable preflight worktree failed")
		}
		if _, err := c.run(ctx, integrationRoot, "worktree", "remove", worktreeRoot); err != nil {
			return errors.New("integration: remove disposable preflight worktree failed")
		}
	}
	if err := os.RemoveAll(operationRoot); err != nil {
		return errors.New("integration: remove preflight scratch failed")
	}
	return nil
}

func validateVerification(taskID string, payload []byte, expectedDigest string) (string, error) {
	if len(payload) == 0 || len(payload) > VerificationLimit || !sha256Digest(expectedDigest) || digest(payload) != expectedDigest {
		return "", ErrInvalidCandidate
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.UseNumber()
	var object map[string]any
	if err := decoder.Decode(&object); err != nil || len(object) == 0 {
		return "", ErrInvalidCandidate
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		return "", ErrInvalidCandidate
	}
	canonical, err := json.Marshal(object)
	if err != nil || !bytes.Equal(canonical, payload) || object["task_id"] != taskID {
		return "", ErrInvalidCandidate
	}
	return expectedDigest, nil
}

func validateAllowedTrackingPaths(slug string, paths []string) (map[string]struct{}, error) {
	if len(paths) > TrackingFileLimit {
		return nil, ErrInvalidCandidate
	}
	prefix := ".compozy/tasks/" + slug + "/"
	allowed := make(map[string]struct{}, len(paths))
	for _, path := range paths {
		if len(path) == 0 || len(path) > TrackingPathLimit || !strings.HasPrefix(path, prefix) || path == prefix ||
			filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path || filepath.Base(path) == "_tasks.md" {
			return nil, ErrInvalidCandidate
		}
		if _, duplicate := allowed[path]; duplicate {
			return nil, ErrInvalidCandidate
		}
		allowed[path] = struct{}{}
	}
	return allowed, nil
}

func validateCandidateSequence(candidates []CandidateEvidence) error {
	seen := map[string]struct{}{}
	worktrees := map[string]struct{}{}
	trackingPaths := map[string]struct{}{}
	repository := ""
	base := ""
	for _, candidate := range candidates {
		if !canonicalTaskID(candidate.TaskID) || !canonicalSlug(candidate.Slug) ||
			!canonicalBranch(candidate.Branch) || !gitSHA(candidate.BaseSHA) || !gitSHA(candidate.CommitSHA) ||
			!gitSHA(candidate.TreeSHA) || !sha256Digest(candidate.RepositoryIdentity) ||
			!sha256Digest(candidate.VerificationDigest) || !filepath.IsAbs(candidate.WorktreeRoot) ||
			filepath.Clean(candidate.WorktreeRoot) != candidate.WorktreeRoot {
			return ErrInvalidCandidate
		}
		if _, duplicate := seen[candidate.TaskID]; duplicate {
			return ErrInvalidCandidate
		}
		seen[candidate.TaskID] = struct{}{}
		if _, duplicate := worktrees[candidate.WorktreeRoot]; duplicate {
			return ErrInvalidCandidate
		}
		worktrees[candidate.WorktreeRoot] = struct{}{}
		if base == "" {
			base = candidate.BaseSHA
		} else if base != candidate.BaseSHA {
			return ErrInvalidCandidate
		}
		if repository == "" {
			repository = candidate.RepositoryIdentity
		} else if repository != candidate.RepositoryIdentity {
			return ErrInvalidCandidate
		}
		prefix := ".compozy/tasks/" + candidate.Slug + "/"
		if candidate.OwnedTrackingPaths == nil {
			return ErrInvalidCandidate
		}
		owned := make(map[string]struct{}, len(candidate.OwnedTrackingPaths))
		for index, path := range candidate.OwnedTrackingPaths {
			if !strings.HasPrefix(path, prefix) || path == prefix ||
				filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) != path ||
				filepath.Base(path) == "_tasks.md" || index > 0 && candidate.OwnedTrackingPaths[index-1] >= path {
				return ErrInvalidCandidate
			}
			if _, duplicate := trackingPaths[path]; duplicate {
				return ErrInvalidCandidate
			}
			trackingPaths[path] = struct{}{}
			owned[path] = struct{}{}
		}
		evidencePaths := make(map[string]struct{}, len(candidate.Tracking))
		for _, tracking := range candidate.Tracking {
			if !strings.HasPrefix(tracking.Path, prefix) || tracking.Path == prefix ||
				filepath.ToSlash(filepath.Clean(filepath.FromSlash(tracking.Path))) != tracking.Path ||
				filepath.Base(tracking.Path) == "_tasks.md" || !sha256Digest(tracking.Digest) {
				return ErrInvalidCandidate
			}
			if _, exists := owned[tracking.Path]; !exists {
				return ErrInvalidCandidate
			}
			if _, duplicate := evidencePaths[tracking.Path]; duplicate {
				return ErrInvalidCandidate
			}
			evidencePaths[tracking.Path] = struct{}{}
		}
	}
	return nil
}

func canonicalDirectory(path string) (string, error) {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return "", ErrInvalidCandidate
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return "", ErrInvalidCandidate
	}
	resolved, err := filepath.EvalSymlinks(path)
	if err != nil || resolved != path {
		return "", ErrInvalidCandidate
	}
	return resolved, nil
}

func parseNULPaths(payload []byte) ([]string, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	if payload[len(payload)-1] != 0 {
		return nil, ErrInvalidCandidate
	}
	items := bytes.Split(payload[:len(payload)-1], []byte{0})
	paths := make([]string, 0, len(items))
	for _, item := range items {
		path := string(item)
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
		if path == "" || filepath.IsAbs(path) || clean != path || path == ".." || strings.HasPrefix(path, "../") {
			return nil, ErrInvalidCandidate
		}
		paths = append(paths, path)
	}
	return paths, nil
}

func nonemptyLines(payload []byte) []string {
	trimmed := strings.TrimSpace(string(payload))
	if trimmed == "" {
		return nil
	}
	return strings.Fields(trimmed)
}

func singleGitSHA(payload []byte) (string, error) {
	value := strings.TrimSpace(string(payload))
	if !gitSHA(value) || strings.ContainsAny(value, " \t\r\n") {
		return "", ErrInvalidCandidate
	}
	return value, nil
}

func canonicalTaskID(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 256 {
		return false
	}
	for _, current := range value {
		if current >= 'a' && current <= 'z' || current >= 'A' && current <= 'Z' ||
			current >= '0' && current <= '9' || strings.ContainsRune("._-", current) {
			continue
		}
		return false
	}
	return true
}

func canonicalSlug(value string) bool {
	if value == "" || value != strings.TrimSpace(value) || len(value) > 128 {
		return false
	}
	for index, current := range value {
		if current >= 'a' && current <= 'z' || current >= '0' && current <= '9' || current == '-' && index > 0 {
			continue
		}
		return false
	}
	return !strings.HasSuffix(value, "-")
}

func canonicalBranch(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 256 &&
		!strings.HasPrefix(value, "-") && !strings.HasSuffix(value, "/") && !strings.Contains(value, "..") &&
		!strings.Contains(value, "@{") && !strings.ContainsAny(value, " ~^:?*[\\\x00")
}

func gitSHA(value string) bool {
	return (len(value) == 40 || len(value) == 64) && lowerHex(value)
}

func sha256Digest(value string) bool {
	return len(value) == 71 && strings.HasPrefix(value, "sha256:") && lowerHex(strings.TrimPrefix(value, "sha256:"))
}

func lowerHex(value string) bool {
	for _, current := range value {
		if current < '0' || current > '9' {
			if current < 'a' || current > 'f' {
				return false
			}
		}
	}
	return true
}

func digest(payload []byte) string {
	value := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(value[:])
}
