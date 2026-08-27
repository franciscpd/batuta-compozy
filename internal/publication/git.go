package publication

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"hash"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

const (
	gitEvidenceStdoutLimit int64 = 16 << 20
	gitEvidenceStderrLimit int64 = 64 << 10
)

var gitSHA = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
var gitBaseRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

type GitSnapshot struct {
	HeadSHA  string
	Branch   string
	Clean    bool
	Detached bool
}

type WorktreeState struct {
	HeadSHA         string `json:"head_sha"`
	PorcelainSHA256 string `json:"porcelain_sha256"`
	ContentSHA256   string `json:"content_sha256"`
}

type GitClient struct {
	Executable string
	Runner     CommandRunner
}

func (c GitClient) WorktreeState(ctx context.Context, worktreePath string) (WorktreeState, error) {
	if err := c.validate(worktreePath); err != nil {
		return WorktreeState{}, err
	}
	headResult, err := c.runEvidence(ctx, worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return WorktreeState{}, fmt.Errorf("publication: read git HEAD: %w", err)
	}
	head, err := parseGitSHA(headResult.Stdout)
	if err != nil {
		return WorktreeState{}, err
	}
	status, err := c.runEvidence(ctx, worktreePath, "status", "--porcelain=v1", "-z", "--untracked-files=all")
	if err != nil {
		return WorktreeState{}, fmt.Errorf("publication: read git status: %w", err)
	}
	diff, err := c.runEvidence(ctx, worktreePath, "diff", "--binary", "--no-ext-diff", "HEAD")
	if err != nil {
		return WorktreeState{}, fmt.Errorf("publication: read git diff: %w", err)
	}
	untracked, err := c.runEvidence(ctx, worktreePath, "ls-files", "--others", "--exclude-standard", "-z")
	if err != nil {
		return WorktreeState{}, fmt.Errorf("publication: list untracked files: %w", err)
	}
	paths, err := parseUntrackedPaths(untracked.Stdout)
	if err != nil {
		return WorktreeState{}, err
	}
	contentHash := sha256.New()
	if _, err := contentHash.Write(diff.Stdout); err != nil {
		return WorktreeState{}, errors.New("publication: hash tracked worktree state failed")
	}
	if err := hashUntrackedFiles(ctx, contentHash, worktreePath, paths); err != nil {
		return WorktreeState{}, err
	}
	return WorktreeState{
		HeadSHA: head, PorcelainSHA256: prefixedDigest(status.Stdout),
		ContentSHA256: "sha256:" + hex.EncodeToString(contentHash.Sum(nil)),
	}, nil
}

func (c GitClient) Snapshot(ctx context.Context, worktreePath string) (GitSnapshot, error) {
	if err := c.validate(worktreePath); err != nil {
		return GitSnapshot{}, err
	}

	status, err := c.run(ctx, worktreePath, "status", "--porcelain=v1")
	if err != nil {
		return GitSnapshot{}, fmt.Errorf("publication: read git status: %w", err)
	}
	head, err := c.run(ctx, worktreePath, "rev-parse", "HEAD")
	if err != nil {
		return GitSnapshot{}, fmt.Errorf("publication: read git HEAD: %w", err)
	}
	headSHA, err := parseGitSHA(head.Stdout)
	if err != nil {
		return GitSnapshot{}, err
	}

	branchResult, branchErr := c.run(ctx, worktreePath, "symbolic-ref", "--quiet", "--short", "HEAD")
	if branchErr != nil {
		if branchResult.ExitCode == 1 {
			return GitSnapshot{HeadSHA: headSHA, Clean: len(status.Stdout) == 0, Detached: true}, nil
		}
		return GitSnapshot{}, fmt.Errorf("publication: read git branch: %w", branchErr)
	}
	branch := strings.TrimSpace(string(branchResult.Stdout))
	if branch == "" {
		return GitSnapshot{}, errors.New("publication: git branch is empty")
	}
	return GitSnapshot{
		HeadSHA: headSHA,
		Branch:  branch,
		Clean:   len(status.Stdout) == 0,
	}, nil
}

func (c GitClient) UpstreamHead(ctx context.Context, worktreePath string) (string, error) {
	if err := c.validate(worktreePath); err != nil {
		return "", err
	}
	result, err := c.run(ctx, worktreePath, "rev-parse", "@{upstream}")
	if err != nil {
		return "", fmt.Errorf("publication: read git upstream HEAD: %w", err)
	}
	return parseGitSHA(result.Stdout)
}

func (c GitClient) CommitsAheadOfBase(ctx context.Context, worktreePath, base string) (int, error) {
	if err := c.validate(worktreePath); err != nil {
		return 0, err
	}
	base = strings.TrimSpace(base)
	if !gitBaseRef.MatchString(base) || strings.Contains(base, "..") || strings.Contains(base, "@{") {
		return 0, errors.New("publication: base branch is invalid")
	}
	result, err := c.run(ctx, worktreePath, "rev-list", "--count", base+"..HEAD")
	if err != nil {
		return 0, fmt.Errorf("publication: count commits ahead of base: %w", err)
	}
	count, err := strconv.Atoi(strings.TrimSpace(string(result.Stdout)))
	if err != nil || count < 0 {
		return 0, errors.New("publication: Git returned an invalid base-ahead count")
	}
	return count, nil
}

func (c GitClient) IsAncestor(ctx context.Context, worktreePath, ancestor, descendant string) (bool, error) {
	if err := c.validate(worktreePath); err != nil || !gitSHA.MatchString(ancestor) || !gitSHA.MatchString(descendant) {
		if err != nil {
			return false, err
		}
		return false, errors.New("publication: invalid Git ancestry identity")
	}
	result, err := c.run(ctx, worktreePath, "merge-base", "--is-ancestor", ancestor, descendant)
	if err == nil {
		return true, nil
	}
	if result.ExitCode == 1 {
		return false, nil
	}
	return false, fmt.Errorf("publication: inspect Git ancestry: %w", err)
}

func (c GitClient) validate(worktreePath string) error {
	if strings.TrimSpace(c.Executable) == "" || !filepath.IsAbs(c.Executable) {
		return errors.New("publication: git executable must be absolute")
	}
	if !filepath.IsAbs(worktreePath) {
		return errors.New("publication: worktree path must be absolute")
	}
	if c.Runner == nil {
		return errors.New("publication: git command runner is required")
	}
	return nil
}

func (c GitClient) runEvidence(ctx context.Context, directory string, args ...string) (CommandResult, error) {
	result, err := c.Runner.Run(ctx, Command{
		Executable: c.Executable, Args: args, Directory: directory,
		StdoutLimit: gitEvidenceStdoutLimit, StderrLimit: gitEvidenceStderrLimit,
	})
	if err != nil {
		if ctxErr := ctx.Err(); ctxErr != nil {
			return result, ctxErr
		}
		return result, err
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return result, errors.New("publication: git evidence exceeded output limit")
	}
	return result, nil
}

func parseUntrackedPaths(payload []byte) ([]string, error) {
	if len(payload) == 0 {
		return nil, nil
	}
	if payload[len(payload)-1] != 0 {
		return nil, errors.New("publication: malformed untracked file list")
	}
	items := bytes.Split(payload[:len(payload)-1], []byte{0})
	paths := make([]string, 0, len(items))
	for _, item := range items {
		path := string(item)
		clean := filepath.Clean(filepath.FromSlash(path))
		if path == "" || filepath.IsAbs(path) || clean == "." || clean == ".." || strings.HasPrefix(clean, ".."+string(filepath.Separator)) || filepath.ToSlash(clean) != path {
			return nil, errors.New("publication: unsafe untracked file path")
		}
		paths = append(paths, path)
	}
	slices.Sort(paths)
	for index := 1; index < len(paths); index++ {
		if paths[index] == paths[index-1] {
			return nil, errors.New("publication: duplicate untracked file path")
		}
	}
	return paths, nil
}

func hashUntrackedFiles(ctx context.Context, target hash.Hash, worktreePath string, paths []string) error {
	if len(paths) == 0 {
		return nil
	}
	root, err := filepath.EvalSymlinks(filepath.Clean(worktreePath))
	if err != nil {
		return errors.New("publication: canonical worktree is unavailable")
	}
	for _, relative := range paths {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(root, filepath.FromSlash(relative))
		contained, err := filepath.Rel(root, path)
		if err != nil || contained == ".." || strings.HasPrefix(contained, ".."+string(filepath.Separator)) || filepath.IsAbs(contained) {
			return errors.New("publication: untracked file escaped worktree")
		}
		info, err := os.Lstat(path)
		if err != nil {
			return errors.New("publication: untracked file is unavailable")
		}
		_, _ = target.Write([]byte{0})
		_, _ = target.Write([]byte(relative))
		_, _ = target.Write([]byte{0})
		switch {
		case info.Mode().IsRegular():
			_, _ = target.Write([]byte{'f', 0})
			if err := hashRegularFile(ctx, target, path); err != nil {
				return err
			}
		case info.Mode()&os.ModeSymlink != 0:
			linkTarget, err := os.Readlink(path)
			if err != nil {
				return errors.New("publication: read untracked symlink failed")
			}
			_, _ = target.Write([]byte{'l', 0})
			_, _ = target.Write([]byte(linkTarget))
		default:
			return errors.New("publication: unsupported untracked file type")
		}
	}
	return nil
}

func hashRegularFile(ctx context.Context, target hash.Hash, path string) error {
	file, err := os.Open(path)
	if err != nil {
		return errors.New("publication: open untracked file failed")
	}
	defer file.Close()
	buffer := make([]byte, 64<<10)
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		read, err := file.Read(buffer)
		if read > 0 {
			_, _ = target.Write(buffer[:read])
		}
		if errors.Is(err, io.EOF) {
			return nil
		}
		if err != nil {
			return errors.New("publication: read untracked file failed")
		}
	}
}

func prefixedDigest(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func (c GitClient) run(ctx context.Context, directory string, args ...string) (CommandResult, error) {
	return c.Runner.Run(ctx, Command{
		Executable: c.Executable,
		Args:       args,
		Directory:  directory,
	})
}

func parseGitSHA(value []byte) (string, error) {
	sha := strings.TrimSpace(string(value))
	if !gitSHA.MatchString(sha) {
		return "", errors.New("publication: git returned an invalid SHA")
	}
	return sha, nil
}
