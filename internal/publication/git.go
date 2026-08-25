package publication

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

var gitSHA = regexp.MustCompile(`^[0-9a-f]{40,64}$`)
var gitBaseRef = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._/-]*$`)

type GitSnapshot struct {
	HeadSHA  string
	Branch   string
	Clean    bool
	Detached bool
}

type GitClient struct {
	Executable string
	Runner     CommandRunner
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

func (c GitClient) validate(worktreePath string) error {
	if !filepath.IsAbs(worktreePath) {
		return errors.New("publication: worktree path must be absolute")
	}
	if c.Runner == nil {
		return errors.New("publication: git command runner is required")
	}
	return nil
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
