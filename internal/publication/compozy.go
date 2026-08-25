package publication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
)

type CLIClient struct {
	Executable string
	Runner     CommandRunner
}

func (c CLIClient) Inspect(
	ctx context.Context,
	scope TrustedScope,
	worktreeRef string,
) (WorktreeInspection, error) {
	ref, err := c.validate(scope, worktreeRef)
	if err != nil {
		return WorktreeInspection{}, err
	}
	var inspection WorktreeInspection
	if err := c.runJSON(ctx, []string{
		"worktree", "inspect", "--workspace", strings.TrimSpace(scope.WorkspaceID), "-o", "json", "--", ref,
	}, &inspection); err != nil {
		return WorktreeInspection{}, fmt.Errorf("publication: inspect worktree: %w", err)
	}
	if strings.TrimSpace(inspection.Worktree.ID) == "" || inspection.Worktree.ID != ref {
		return WorktreeInspection{}, errors.New("publication: inspection returned a mismatched worktree ID")
	}
	return inspection, nil
}

func (c CLIClient) ExitPlan(
	ctx context.Context,
	scope TrustedScope,
	worktreeRef string,
) (ExitPlan, error) {
	ref, err := c.validate(scope, worktreeRef)
	if err != nil {
		return ExitPlan{}, err
	}
	var plan ExitPlan
	if err := c.runJSON(ctx, []string{
		"worktree", "exit", "--workspace", strings.TrimSpace(scope.WorkspaceID), "-o", "json", "--", ref,
	}, &plan); err != nil {
		return ExitPlan{}, fmt.Errorf("publication: read worktree exit plan: %w", err)
	}
	if strings.TrimSpace(plan.WorktreeID) == "" || plan.WorktreeID != ref {
		return ExitPlan{}, errors.New("publication: exit plan returned a mismatched worktree ID")
	}
	return plan, nil
}

func (c CLIClient) Push(
	ctx context.Context,
	scope TrustedScope,
	worktreeRef string,
) (Operation, error) {
	ref, err := c.validate(scope, worktreeRef)
	if err != nil {
		return Operation{}, err
	}
	return c.runOperation(ctx, []string{
		"worktree", "push", "--workspace", strings.TrimSpace(scope.WorkspaceID), "-o", "json", "--", ref,
	})
}

func (c CLIClient) OpenPR(
	ctx context.Context,
	scope TrustedScope,
	worktreeRef string,
	prefill PRPrefill,
	base string,
) (Operation, error) {
	ref, err := c.validate(scope, worktreeRef)
	if err != nil {
		return Operation{}, err
	}
	return c.runOperation(ctx, []string{
		"worktree", "pr", "--workspace", strings.TrimSpace(scope.WorkspaceID),
		"--title", prefill.Title, "--body", prefill.Body, "--base", base,
		"-o", "json", "--", ref,
	})
}

func (c CLIClient) runOperation(ctx context.Context, args []string) (Operation, error) {
	var operation Operation
	if err := c.runJSON(ctx, args, &operation); err != nil {
		return Operation{}, fmt.Errorf("publication: run worktree operation: %w", err)
	}
	if strings.TrimSpace(operation.OperationID) == "" {
		return Operation{}, errors.New("publication: worktree operation returned an empty operation ID")
	}
	return operation, nil
}

func (c CLIClient) runJSON(ctx context.Context, args []string, target any) error {
	result, err := c.Runner.Run(ctx, Command{Executable: c.Executable, Args: args})
	if err != nil {
		return err
	}
	if err := json.Unmarshal(result.Stdout, target); err != nil {
		return fmt.Errorf("decode Compozy JSON: %w", err)
	}
	return nil
}

func (c CLIClient) validate(scope TrustedScope, worktreeRef string) (string, error) {
	if strings.TrimSpace(c.Executable) == "" || !filepath.IsAbs(c.Executable) {
		return "", errors.New("publication: Compozy executable must be absolute")
	}
	if c.Runner == nil {
		return "", errors.New("publication: Compozy command runner is required")
	}
	if strings.TrimSpace(scope.WorkspaceID) == "" {
		return "", errors.New("publication: trusted workspace ID is required")
	}
	ref := strings.TrimSpace(worktreeRef)
	if ref == "" {
		return "", errors.New("publication: worktree reference is required")
	}
	return ref, nil
}
