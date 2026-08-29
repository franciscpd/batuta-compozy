package repository

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

const (
	bootstrapStdoutLimit      int64 = 16 << 20
	bootstrapStderrLimit      int64 = 64 << 10
	bootstrapCommitMessage          = "chore: initialize workspace"
	bootstrapBlockedPathLimit       = 10_000
)

var gitSHA = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)

type BootstrapState string

const (
	BootstrapInitialized        BootstrapState = "initialized"
	BootstrapAlreadyInitialized BootstrapState = "already_initialized"
	BootstrapBlocked            BootstrapState = "blocked_sensitive_paths"
)

type BootstrapResult struct {
	State          BootstrapState `json:"state"`
	Branch         string         `json:"branch,omitempty"`
	HeadSHA        string         `json:"head_sha,omitempty"`
	CommitMessage  string         `json:"commit_message,omitempty"`
	CommittedFiles int            `json:"committed_files"`
	BlockedPaths   []string       `json:"blocked_paths,omitempty"`
}

type Bootstrapper struct {
	GitExecutable string
	Runner        publication.CommandRunner
}

func (b Bootstrapper) Bootstrap(ctx context.Context, workspaceRoot string) (output BootstrapResult, err error) {
	root, err := b.validate(workspaceRoot)
	if err != nil {
		return BootstrapResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return BootstrapResult{}, err
	}

	createdRepository := false
	keepRepository := false
	defer func() {
		if !createdRepository || keepRepository {
			return
		}
		if cleanupErr := removeOwnedRepository(root); cleanupErr != nil && err == nil {
			output = BootstrapResult{}
			err = cleanupErr
		}
	}()

	top, topErr := b.run(ctx, root, "rev-parse", "--show-toplevel")
	if topErr == nil {
		if filepath.Clean(strings.TrimSpace(string(top.Stdout))) != root {
			return BootstrapResult{}, errors.New("repository bootstrap: workspace root is not the repository root")
		}
	} else {
		if top.ExitCode != 128 {
			return BootstrapResult{}, fmt.Errorf("repository bootstrap: inspect repository: %w", topErr)
		}
		gitPath := filepath.Join(root, ".git")
		if _, statErr := os.Lstat(gitPath); !errors.Is(statErr, os.ErrNotExist) {
			return BootstrapResult{}, errors.New("repository bootstrap: untrusted .git path already exists")
		}
		if _, err := b.run(ctx, root, "init", "--initial-branch=main", "."); err != nil {
			return BootstrapResult{}, fmt.Errorf("repository bootstrap: initialize Git: %w", err)
		}
		createdRepository = true
	}

	head, headErr := b.run(ctx, root, "rev-parse", "--verify", "HEAD")
	if headErr == nil {
		headSHA, err := parseSHA(head.Stdout)
		if err != nil {
			return BootstrapResult{}, err
		}
		branch, err := b.branchOrDetached(ctx, root)
		if err != nil {
			return BootstrapResult{}, err
		}
		keepRepository = true
		return BootstrapResult{State: BootstrapAlreadyInitialized, Branch: branch, HeadSHA: headSHA}, nil
	}
	if head.ExitCode != 1 && head.ExitCode != 128 {
		return BootstrapResult{}, fmt.Errorf("repository bootstrap: inspect Git HEAD: %w", headErr)
	}
	initialBranch, err := b.branch(ctx, root)
	if err != nil {
		return BootstrapResult{}, err
	}
	if initialBranch != "main" {
		return BootstrapResult{}, errors.New("repository bootstrap: existing HEAD-less repository must use branch main")
	}

	listed, err := b.run(ctx, root, "ls-files", "--cached", "--others", "--exclude-standard", "-z")
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("repository bootstrap: list initial files: %w", err)
	}
	paths, err := parsePaths(listed.Stdout)
	if err != nil {
		return BootstrapResult{}, err
	}
	blocked := sensitivePaths(paths)
	if len(blocked) > 0 {
		return BootstrapResult{State: BootstrapBlocked, BlockedPaths: boundedBlockedPaths(blocked)}, nil
	}

	index, err := snapshotIndex(ctx, b, root)
	if err != nil {
		return BootstrapResult{}, err
	}
	indexCommitted := false
	defer func() {
		if createdRepository || indexCommitted {
			return
		}
		if restoreErr := restoreIndex(index); restoreErr != nil && err == nil {
			output = BootstrapResult{}
			err = restoreErr
		}
	}()
	if _, err := b.run(ctx, root, "add", "--all", "--", "."); err != nil {
		return BootstrapResult{}, fmt.Errorf("repository bootstrap: stage initial files: %w", err)
	}
	staged, err := b.run(ctx, root, "diff", "--cached", "--name-only", "-z")
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("repository bootstrap: inspect staged files: %w", err)
	}
	stagedPaths, err := parsePaths(staged.Stdout)
	if err != nil {
		return BootstrapResult{}, err
	}
	if blocked = sensitivePaths(stagedPaths); len(blocked) > 0 {
		return BootstrapResult{State: BootstrapBlocked, BlockedPaths: boundedBlockedPaths(blocked)}, nil
	}
	commitArgs := []string{
		"-c", "commit.gpgsign=false", "-c", "core.hooksPath=", "commit", "--no-verify", "--no-gpg-sign", "--allow-empty", "-m", bootstrapCommitMessage,
	}
	if _, err := b.runWithEnvironment(ctx, root, []string{
		"GIT_AUTHOR_NAME=Batuta", "GIT_AUTHOR_EMAIL=batuta@example.invalid",
		"GIT_COMMITTER_NAME=Batuta", "GIT_COMMITTER_EMAIL=batuta@example.invalid",
	}, commitArgs...); err != nil {
		return BootstrapResult{}, fmt.Errorf("repository bootstrap: create initial commit: %w", err)
	}
	indexCommitted = true
	head, err = b.run(ctx, root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return BootstrapResult{}, fmt.Errorf("repository bootstrap: verify initial commit: %w", err)
	}
	headSHA, err := parseSHA(head.Stdout)
	if err != nil {
		return BootstrapResult{}, err
	}
	branch, err := b.branch(ctx, root)
	if err != nil {
		return BootstrapResult{}, err
	}
	keepRepository = true
	return BootstrapResult{
		State: BootstrapInitialized, Branch: branch, HeadSHA: headSHA,
		CommitMessage: bootstrapCommitMessage, CommittedFiles: len(stagedPaths),
	}, nil
}

func (b Bootstrapper) validate(workspaceRoot string) (string, error) {
	if strings.TrimSpace(b.GitExecutable) == "" || !filepath.IsAbs(b.GitExecutable) || b.Runner == nil {
		return "", errors.New("repository bootstrap: absolute Git executable and runner are required")
	}
	if !filepath.IsAbs(workspaceRoot) || filepath.Clean(workspaceRoot) != workspaceRoot {
		return "", errors.New("repository bootstrap: trusted workspace root must be canonical and absolute")
	}
	canonical, err := filepath.EvalSymlinks(workspaceRoot)
	if err != nil || filepath.Clean(canonical) != workspaceRoot {
		return "", errors.New("repository bootstrap: trusted workspace root is unavailable")
	}
	info, err := os.Stat(workspaceRoot)
	if err != nil || !info.IsDir() {
		return "", errors.New("repository bootstrap: trusted workspace root is unavailable")
	}
	return workspaceRoot, nil
}

func (b Bootstrapper) branch(ctx context.Context, root string) (string, error) {
	result, err := b.run(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		return "", errors.New("repository bootstrap: read initial branch failed")
	}
	branch := strings.TrimSpace(string(result.Stdout))
	if branch == "" || strings.ContainsAny(branch, "\x00\r\n") {
		return "", errors.New("repository bootstrap: Git returned an invalid branch")
	}
	return branch, nil
}

func (b Bootstrapper) branchOrDetached(ctx context.Context, root string) (string, error) {
	result, err := b.run(ctx, root, "symbolic-ref", "--quiet", "--short", "HEAD")
	if err != nil {
		if result.ExitCode == 1 {
			return "", nil
		}
		return "", errors.New("repository bootstrap: read initial branch failed")
	}
	branch := strings.TrimSpace(string(result.Stdout))
	if branch == "" || strings.ContainsAny(branch, "\x00\r\n") {
		return "", errors.New("repository bootstrap: Git returned an invalid branch")
	}
	return branch, nil
}

func (b Bootstrapper) run(ctx context.Context, root string, args ...string) (publication.CommandResult, error) {
	return b.runWithEnvironment(ctx, root, nil, args...)
}

func (b Bootstrapper) runWithEnvironment(ctx context.Context, root string, environment []string, args ...string) (publication.CommandResult, error) {
	result, err := b.Runner.Run(ctx, publication.Command{
		Executable: b.GitExecutable, Args: args, Directory: root, Environment: environment,
		StdoutLimit: bootstrapStdoutLimit, StderrLimit: bootstrapStderrLimit,
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		return result, err
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return result, errors.New("repository bootstrap: Git evidence exceeded output limit")
	}
	return result, nil
}

func parseSHA(payload []byte) (string, error) {
	value := strings.TrimSpace(string(payload))
	if !gitSHA.MatchString(value) {
		return "", errors.New("repository bootstrap: Git returned an invalid HEAD")
	}
	return value, nil
}

func parsePaths(payload []byte) ([]string, error) {
	if len(payload) == 0 {
		return []string{}, nil
	}
	if payload[len(payload)-1] != 0 {
		return nil, errors.New("repository bootstrap: malformed initial file list")
	}
	parts := bytes.Split(payload[:len(payload)-1], []byte{0})
	paths := make([]string, 0, len(parts))
	for _, part := range parts {
		path := string(part)
		clean := filepath.ToSlash(filepath.Clean(filepath.FromSlash(path)))
		if path == "" || filepath.IsAbs(path) || clean != path || path == "." || path == ".." || strings.HasPrefix(path, "../") {
			return nil, errors.New("repository bootstrap: unsafe initial file path")
		}
		paths = append(paths, path)
	}
	slices.Sort(paths)
	if len(slices.Compact(slices.Clone(paths))) != len(paths) {
		return nil, errors.New("repository bootstrap: duplicate initial file path")
	}
	return paths, nil
}

func sensitivePaths(paths []string) []string {
	blocked := make([]string, 0)
	for _, path := range paths {
		if sensitivePath(path) {
			blocked = append(blocked, path)
		}
	}
	return blocked
}

func boundedBlockedPaths(paths []string) []string {
	if len(paths) <= bootstrapBlockedPathLimit {
		return append([]string(nil), paths...)
	}
	return append([]string(nil), paths[:bootstrapBlockedPathLimit]...)
}

func sensitivePath(path string) bool {
	lower := strings.ToLower(filepath.ToSlash(path))
	parts := strings.Split(lower, "/")
	for _, part := range parts[:len(parts)-1] {
		if part == ".ssh" || part == ".aws" || part == ".gnupg" {
			return true
		}
	}
	base := parts[len(parts)-1]
	if base == ".env.example" || base == ".env.sample" || base == ".env.template" ||
		strings.HasSuffix(base, ".env.example") || strings.HasSuffix(base, ".env.sample") || strings.HasSuffix(base, ".env.template") {
		return false
	}
	if base == ".env" || strings.HasPrefix(base, ".env.") || base == ".envrc" || base == ".npmrc" || base == ".pypirc" ||
		base == ".netrc" || base == ".git-credentials" || base == "credentials" || base == "credentials.json" ||
		base == "service-account.json" || base == "id_rsa" || base == "id_ed25519" || base == "id_ecdsa" || base == "id_dsa" ||
		base == "id_ecdsa_sk" || base == "id_ed25519_sk" {
		return true
	}
	extension := strings.ToLower(filepath.Ext(base))
	return extension == ".pem" || extension == ".key" || extension == ".p12" || extension == ".pfx"
}

type indexSnapshot struct {
	path   string
	body   []byte
	mode   os.FileMode
	exists bool
}

func snapshotIndex(ctx context.Context, bootstrapper Bootstrapper, root string) (indexSnapshot, error) {
	result, err := bootstrapper.run(ctx, root, "rev-parse", "--git-path", "index")
	if err != nil {
		return indexSnapshot{}, errors.New("repository bootstrap: locate Git index failed")
	}
	path := strings.TrimSpace(string(result.Stdout))
	if !filepath.IsAbs(path) {
		path = filepath.Join(root, path)
	}
	path = filepath.Clean(path)
	gitRoot := filepath.Join(root, ".git")
	relative, err := filepath.Rel(gitRoot, path)
	if err != nil || relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator)) || filepath.IsAbs(relative) {
		return indexSnapshot{}, errors.New("repository bootstrap: Git index escapes trusted repository")
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return indexSnapshot{path: path}, nil
	}
	if err != nil || !info.Mode().IsRegular() {
		return indexSnapshot{}, errors.New("repository bootstrap: Git index is unsafe")
	}
	body, err := os.ReadFile(path)
	if err != nil {
		return indexSnapshot{}, errors.New("repository bootstrap: read Git index failed")
	}
	return indexSnapshot{path: path, body: body, mode: info.Mode().Perm(), exists: true}, nil
}

func restoreIndex(snapshot indexSnapshot) error {
	if snapshot.path == "" {
		return nil
	}
	if !snapshot.exists {
		if err := os.Remove(snapshot.path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return errors.New("repository bootstrap: restore Git index failed")
		}
		return nil
	}
	if err := os.WriteFile(snapshot.path, snapshot.body, snapshot.mode); err != nil {
		return errors.New("repository bootstrap: restore Git index failed")
	}
	return nil
}

func removeOwnedRepository(root string) error {
	gitPath := filepath.Join(root, ".git")
	info, err := os.Lstat(gitPath)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return errors.New("repository bootstrap: owned Git metadata became unsafe")
	}
	if err := os.RemoveAll(gitPath); err != nil {
		return errors.New("repository bootstrap: rollback owned Git metadata failed")
	}
	return nil
}
