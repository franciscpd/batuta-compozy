package worktreeops

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

const (
	WorktreeStdoutLimit int64 = 256 * 1024
	WorktreeStderrLimit int64 = 64 * 1024
	defaultTimeout            = 30 * time.Second
)

type CLIClient struct {
	Executable string
	Runner     publication.CommandRunner
	Timeout    time.Duration
}

func (c CLIClient) Create(
	ctx context.Context,
	scope publication.TrustedScope,
	request CreateRequest,
) (Worktree, error) {
	if !boundedIdentity(request.Name) || !boundedIdentity(request.Branch) || !gitSHA(request.BaseSHA) {
		return Worktree{}, ErrInvalidWorktreeIdentity
	}
	var raw daemonWorktree
	err := c.runJSON(ctx, scope, []string{
		"worktree", "create", "--workspace", scope.WorkspaceID,
		"--branch", request.Branch,
		"--base", request.BaseSHA,
		"-o", "json", "--", request.Name,
	}, &raw)
	if err != nil {
		return Worktree{}, fmt.Errorf("worktreeops: create worktree: %w", err)
	}
	worktree, err := worktreeFromDaemon(scope, raw)
	if err != nil || worktree.Name != request.Name || worktree.Branch != request.Branch ||
		worktree.BaseRef != request.BaseSHA || worktree.BaseSHA != request.BaseSHA {
		return Worktree{}, ErrInvalidWorktreeIdentity
	}
	return worktree, nil
}

func (c CLIClient) Inspect(
	ctx context.Context,
	scope publication.TrustedScope,
	worktreeID string,
) (Worktree, error) {
	if !opaqueID(worktreeID) {
		return Worktree{}, ErrInvalidWorktreeIdentity
	}
	var response struct {
		Worktree daemonWorktree `json:"worktree"`
		Repo     struct {
			GitBacked    bool `json:"git_backed"`
			GitAvailable bool `json:"git_available"`
		} `json:"repo"`
	}
	if err := c.runJSON(ctx, scope, []string{
		"worktree", "inspect", "--workspace", scope.WorkspaceID, "-o", "json", "--", worktreeID,
	}, &response); err != nil {
		return Worktree{}, fmt.Errorf("worktreeops: inspect worktree: %w", err)
	}
	worktree, err := worktreeFromDaemon(scope, response.Worktree)
	if err != nil || worktree.ID != worktreeID || !response.Repo.GitBacked || !response.Repo.GitAvailable {
		return Worktree{}, ErrInvalidWorktreeIdentity
	}
	return worktree, nil
}

func (c CLIClient) FindByName(
	ctx context.Context,
	scope publication.TrustedScope,
	name string,
) (Worktree, bool, error) {
	if !boundedIdentity(name) {
		return Worktree{}, false, ErrInvalidWorktreeIdentity
	}
	var raw []daemonWorktree
	if err := c.runJSON(ctx, scope, []string{
		"worktree", "list", "--workspace", scope.WorkspaceID, "-o", "json",
	}, &raw); err != nil {
		return Worktree{}, false, fmt.Errorf("worktreeops: list worktrees: %w", err)
	}
	if len(raw) > 4096 {
		return Worktree{}, false, ErrInvalidWorktreeIdentity
	}
	var matched Worktree
	matches := 0
	for _, item := range raw {
		if item.Name != name {
			continue
		}
		worktree, err := worktreeFromDaemon(scope, item)
		if err != nil {
			return Worktree{}, false, ErrInvalidWorktreeIdentity
		}
		matched = worktree
		matches++
	}
	if matches > 1 {
		return Worktree{}, false, ErrInvalidWorktreeIdentity
	}
	return matched, matches == 1, nil
}

func (c CLIClient) Remove(
	ctx context.Context,
	scope publication.TrustedScope,
	worktreeID string,
) (Worktree, error) {
	if !opaqueID(worktreeID) {
		return Worktree{}, ErrInvalidWorktreeIdentity
	}
	var response struct {
		Action   string         `json:"action"`
		Worktree daemonWorktree `json:"worktree"`
	}
	if err := c.runJSON(ctx, scope, []string{
		"worktree", "remove", "--workspace", scope.WorkspaceID, "-o", "json", "--", worktreeID,
	}, &response); err != nil {
		return Worktree{}, fmt.Errorf("worktreeops: remove worktree: %w", err)
	}
	worktree, err := worktreeFromDaemon(scope, response.Worktree)
	if err != nil || response.Action != "removed" || worktree.ID != worktreeID || worktree.State != "removed" {
		return Worktree{}, ErrInvalidWorktreeIdentity
	}
	return worktree, nil
}

func (c CLIClient) runJSON(
	ctx context.Context,
	scope publication.TrustedScope,
	args []string,
	target any,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Runner == nil || strings.TrimSpace(c.Executable) == "" || !filepath.IsAbs(c.Executable) {
		return errors.New("worktreeops: absolute Compozy executable and runner are required")
	}
	workspaceID := strings.TrimSpace(scope.WorkspaceID)
	root := filepath.Clean(scope.WorkspaceRoot)
	if !opaqueID(workspaceID) || scope.WorkspaceID != workspaceID || !filepath.IsAbs(scope.WorkspaceRoot) ||
		root != scope.WorkspaceRoot {
		return ErrInvalidWorktreeIdentity
	}
	timeout := c.Timeout
	if timeout == 0 {
		timeout = defaultTimeout
	}
	if timeout < 0 {
		return errors.New("worktreeops: timeout must not be negative")
	}
	bounded, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	result, err := c.Runner.Run(bounded, publication.Command{
		Executable: c.Executable, Args: append([]string(nil), args...), Directory: root,
		StdoutLimit: WorktreeStdoutLimit, StderrLimit: WorktreeStderrLimit,
	})
	if err != nil {
		if contextErr := bounded.Err(); contextErr != nil {
			return contextErr
		}
		return err
	}
	if result.StdoutTruncated || result.StderrTruncated {
		return errors.New("worktreeops: Compozy output was truncated")
	}
	if err := decodeSingleJSONObject(result.Stdout, target); err != nil {
		return fmt.Errorf("worktreeops: decode Compozy JSON: %w", err)
	}
	return nil
}

type daemonWorktree struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Name        string `json:"name"`
	Branch      string `json:"branch"`
	Path        string `json:"path"`
	State       string `json:"state"`
	SetupState  string `json:"setup_state"`
	SetupError  string `json:"setup_error"`
	BaseRef     string `json:"base_ref"`
	CreatedHead string `json:"created_head"`
}

func worktreeFromDaemon(scope publication.TrustedScope, raw daemonWorktree) (Worktree, error) {
	repositoryRoot := filepath.Clean(scope.WorkspaceRoot)
	root := filepath.Clean(raw.Path)
	if !opaqueID(raw.ID) || raw.WorkspaceID != scope.WorkspaceID || !boundedIdentity(raw.Name) ||
		!boundedIdentity(raw.Branch) || !filepath.IsAbs(raw.Path) || root != raw.Path || root == repositoryRoot ||
		!validWorktreeState(raw.State) || !validSetup(raw.SetupState, raw.SetupError) || !gitSHA(raw.BaseRef) ||
		(raw.CreatedHead != "" && (!gitSHA(raw.CreatedHead) || raw.CreatedHead != raw.BaseRef)) {
		return Worktree{}, ErrInvalidWorktreeIdentity
	}
	if info, err := os.Lstat(root); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return Worktree{}, ErrInvalidWorktreeIdentity
		}
	} else if err != nil && !errors.Is(err, os.ErrNotExist) {
		return Worktree{}, ErrInvalidWorktreeIdentity
	}
	identity := sha256.Sum256([]byte(scope.WorkspaceID + "\x00" + repositoryRoot))
	return Worktree{
		ID: raw.ID, Name: raw.Name, Root: root, WorkspaceID: raw.WorkspaceID,
		RepositoryRoot: repositoryRoot, RepositoryIdentity: "sha256:" + hex.EncodeToString(identity[:]),
		Branch: raw.Branch, BaseRef: raw.BaseRef, BaseSHA: raw.BaseRef, State: raw.State,
		Setup: SetupResult{State: raw.SetupState, Error: raw.SetupError},
	}, nil
}

func opaqueID(value string) bool {
	if !boundedIdentity(value) {
		return false
	}
	for _, current := range value {
		if current >= 'a' && current <= 'z' || current >= 'A' && current <= 'Z' ||
			current >= '0' && current <= '9' || strings.ContainsRune("._:-", current) {
			continue
		}
		return false
	}
	return true
}

func validWorktreeState(state string) bool {
	switch state {
	case "pending", "ready", "failed", "removing", "missing", "removed", "dismissed":
		return true
	default:
		return false
	}
}

func validSetup(state, diagnostic string) bool {
	if diagnostic != "" && (diagnostic != strings.TrimSpace(diagnostic) || len(diagnostic) > 16*1024 ||
		strings.ContainsRune(diagnostic, '\x00')) {
		return false
	}
	switch state {
	case "none", "ok":
		return diagnostic == ""
	case "failed":
		return diagnostic != ""
	default:
		return false
	}
}

func decodeSingleJSONObject(payload []byte, target any) error {
	if len(bytes.TrimSpace(payload)) == 0 {
		return errors.New("empty JSON object")
	}
	keys := json.NewDecoder(bytes.NewReader(payload))
	if err := consumeUniqueJSONValue(keys); err != nil {
		return err
	}
	if _, err := keys.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(target); err != nil {
		return err
	}
	return nil
}

func consumeUniqueJSONValue(decoder *json.Decoder) error {
	token, err := decoder.Token()
	if err != nil {
		return err
	}
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid JSON object key")
			}
			if _, duplicate := seen[key]; duplicate {
				return fmt.Errorf("duplicate JSON object key %q", key)
			}
			seen[key] = struct{}{}
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim('}') {
			return errors.New("invalid JSON object")
		}
	case '[':
		for decoder.More() {
			if err := consumeUniqueJSONValue(decoder); err != nil {
				return err
			}
		}
		closing, err := decoder.Token()
		if err != nil || closing != json.Delim(']') {
			return errors.New("invalid JSON array")
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	return nil
}
