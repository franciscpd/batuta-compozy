package publication

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"path/filepath"
	"strings"
)

type PlanInput struct {
	WorktreeRef string `json:"worktree_ref"`
}

type Disposition string

const (
	DispositionPublishable Disposition = "publishable"
	DispositionNothing     Disposition = "nothing_to_publish"
	DispositionBlocked     Disposition = "blocked"
)

type PlanOutput struct {
	Disposition  Disposition `json:"disposition"`
	WorktreeID   string      `json:"worktree_id"`
	Branch       string      `json:"branch"`
	BaseBranch   string      `json:"base_branch"`
	WorktreePath string      `json:"worktree_path"`
	HeadSHA      string      `json:"head_sha"`
	Clean        bool        `json:"clean"`
	ExitPlan     ExitPlan    `json:"exit_plan"`
	Blockers     []string    `json:"blockers,omitempty"`
}

// PublicationPlanner classifies a trusted worktree for publication. It does
// not plan or decompose implementation tasks; Compozy owns that workflow.
type PublicationPlanner struct {
	Compozy WorktreeClient
	Git     GitEvidence
}

type BlockedPlanError struct {
	Plan PlanOutput
}

func (e *BlockedPlanError) Error() string {
	return "publication plan blocked: " + strings.Join(e.Plan.Blockers, ",")
}

func (p PublicationPlanner) Plan(
	ctx context.Context,
	scope TrustedScope,
	input PlanInput,
) (PlanOutput, error) {
	if strings.TrimSpace(scope.WorkspaceID) == "" ||
		strings.TrimSpace(scope.WorkspaceRoot) == "" ||
		!filepath.IsAbs(scope.WorkspaceRoot) {
		return blockedPlan(PlanOutput{}, "trusted_scope_missing")
	}
	ref := strings.TrimSpace(input.WorktreeRef)
	if ref == "" {
		return PlanOutput{}, errors.New("publication: worktree reference is required")
	}
	if p.Compozy == nil {
		return PlanOutput{}, errors.New("publication: worktree client is required")
	}
	if p.Git == nil {
		return PlanOutput{}, errors.New("publication: Git evidence is required")
	}

	inspection, err := p.Compozy.Inspect(ctx, scope, ref)
	if err != nil {
		return PlanOutput{}, fmt.Errorf("publication: inspect trusted worktree: %w", err)
	}
	plan := PlanOutput{
		WorktreeID:   inspection.Worktree.ID,
		Branch:       inspection.Worktree.Branch,
		BaseBranch:   inspection.Worktree.BaseRef,
		WorktreePath: inspection.Worktree.Path,
	}
	if inspection.Worktree.WorkspaceID != strings.TrimSpace(scope.WorkspaceID) {
		return blockedPlan(plan, "workspace_mismatch")
	}
	switch inspection.Worktree.State {
	case "missing", "removed", "dismissed", "failed":
		return blockedPlan(plan, "worktree_unavailable")
	case "ready":
	default:
		return blockedPlan(plan, "worktree_not_ready")
	}
	if !filepath.IsAbs(inspection.Worktree.Path) {
		return blockedPlan(plan, "worktree_path_invalid")
	}
	if !inspection.Repo.GitBacked || !inspection.Repo.GitAvailable ||
		inspection.Status == nil || strings.TrimSpace(inspection.Status.ReadError) != "" {
		return blockedPlan(plan, "git_unreadable")
	}

	status := inspection.Status
	if status.Branch == nil || status.Detached == nil || status.HeadSHA == nil ||
		status.DirtyFiles == nil || status.HasUpstream == nil {
		return blockedPlan(plan, "git_unreadable")
	}
	snapshot, err := p.Git.Snapshot(ctx, inspection.Worktree.Path)
	if err != nil {
		return blockedPlan(plan, "git_unreadable")
	}
	plan.HeadSHA = snapshot.HeadSHA
	plan.Clean = snapshot.Clean
	if !snapshot.Clean || *status.DirtyFiles != 0 {
		return blockedPlan(plan, "dirty_worktree")
	}
	if snapshot.Detached || *status.Detached {
		return blockedPlan(plan, "detached_head")
	}
	if snapshot.Branch != inspection.Worktree.Branch || *status.Branch != inspection.Worktree.Branch {
		return blockedPlan(plan, "branch_mismatch")
	}
	if !gitSHA.MatchString(snapshot.HeadSHA) || !gitSHA.MatchString(*status.HeadSHA) ||
		snapshot.HeadSHA != *status.HeadSHA {
		return blockedPlan(plan, "head_invalid")
	}
	exitPlan, err := p.Compozy.ExitPlan(ctx, scope, ref)
	if err != nil {
		return PlanOutput{}, fmt.Errorf("publication: read trusted exit plan: %w", err)
	}
	plan.ExitPlan = exitPlan
	if exitPlan.WorktreeID != inspection.Worktree.ID {
		return blockedPlan(plan, "exit_plan_mismatch")
	}
	if strings.TrimSpace(exitPlan.GlobalPauseCause) != "" {
		return blockedPlan(plan, "exit_plan_paused")
	}
	if !hasRemoteEvidence(exitPlan) {
		return blockedPlan(plan, "remote_missing")
	}
	if *status.HasUpstream {
		if status.Ahead == nil || status.Behind == nil {
			return blockedPlan(plan, "git_unreadable")
		}
		if *status.Ahead > 0 && *status.Behind > 0 {
			return blockedPlan(plan, "branch_diverged")
		}
		if *status.Behind > 0 {
			return blockedPlan(plan, "branch_behind")
		}
		upstreamHead, upstreamErr := p.Git.UpstreamHead(ctx, inspection.Worktree.Path)
		if upstreamErr != nil || !gitSHA.MatchString(upstreamHead) {
			return blockedPlan(plan, "remote_missing")
		}
	}
	baseBranch, ok := consistentBaseBranch(inspection.Worktree.BaseRef, exitPlan.Base)
	if !ok {
		return blockedPlan(plan, "publication_state_ambiguous")
	}
	if baseBranch == "" && exitPlan.Forge != nil {
		baseBranch = strings.TrimSpace(exitPlan.Forge.DefaultBranch)
	}
	plan.BaseBranch = baseBranch
	baseAhead, err := p.Git.CommitsAheadOfBase(ctx, inspection.Worktree.Path, plan.BaseBranch)
	if err != nil {
		return blockedPlan(plan, "git_unreadable")
	}
	if status.AheadOfBase != nil && *status.AheadOfBase != baseAhead {
		return blockedPlan(plan, "publication_state_ambiguous")
	}
	if baseAhead == 0 {
		plan.Disposition = DispositionNothing
		return plan, nil
	}
	if exitPlan.Forge == nil || strings.TrimSpace(exitPlan.Forge.Provider) == "" ||
		strings.TrimSpace(exitPlan.Forge.DefaultBranch) == "" {
		return blockedPlan(plan, "forge_unavailable")
	}
	if plan.BaseBranch == "" {
		plan.BaseBranch = strings.TrimSpace(exitPlan.Forge.DefaultBranch)
	} else if plan.BaseBranch != strings.TrimSpace(exitPlan.Forge.DefaultBranch) {
		return blockedPlan(plan, "publication_state_ambiguous")
	}
	if !hasPublicationPath(exitPlan) {
		return blockedPlan(plan, "publication_state_ambiguous")
	}
	plan.Disposition = DispositionPublishable
	return plan, nil
}

func blockedPlan(plan PlanOutput, blocker string) (PlanOutput, error) {
	plan.Disposition = DispositionBlocked
	plan.Blockers = []string{blocker}
	return plan, &BlockedPlanError{Plan: plan}
}

func consistentBaseBranch(worktreeBase, exitBase string) (string, bool) {
	worktreeBase = strings.TrimSpace(worktreeBase)
	exitBase = strings.TrimSpace(exitBase)
	if worktreeBase != "" && exitBase != "" && worktreeBase != exitBase {
		return "", false
	}
	if exitBase != "" {
		return exitBase, true
	}
	return worktreeBase, true
}

func hasRemoteEvidence(plan ExitPlan) bool {
	if plan.Forge != nil || strings.TrimSpace(plan.BrowserURL) != "" {
		return true
	}
	for _, action := range plan.Actions {
		switch action.Action {
		case "commit_push", "push", "open_pr", "view_pr":
			return true
		}
	}
	return false
}

func hasPublicationPath(plan ExitPlan) bool {
	if validPRURL(plan.ForgeStatus) {
		return true
	}
	for _, action := range plan.Actions {
		if !action.Enabled {
			continue
		}
		switch action.Action {
		case "view_pr":
			if isAbsoluteHTTPS(action.URL) {
				return true
			}
		case "push", "open_pr":
			return true
		}
	}
	return false
}

func validPRURL(status *ForgeStatus) bool {
	return status != nil && isAbsoluteHTTPS(status.PRURL)
}

func isAbsoluteHTTPS(value string) bool {
	parsed, err := url.Parse(strings.TrimSpace(value))
	return err == nil && parsed.Scheme == "https" && parsed.Host != ""
}
