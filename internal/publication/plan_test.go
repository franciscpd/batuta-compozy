package publication

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"
)

func TestPlannerRejectsMissingTrustedScope(t *testing.T) {
	t.Parallel()

	tests := []TrustedScope{
		{},
		{WorkspaceID: "ws_trusted"},
		{WorkspaceRoot: "/trusted/workspace"},
		{WorkspaceID: "ws_trusted", WorkspaceRoot: "relative/workspace"},
	}
	for _, scope := range tests {
		scope := scope
		t.Run(scope.WorkspaceID+scope.WorkspaceRoot, func(t *testing.T) {
			t.Parallel()
			compozy := &fakeWorktreeClient{}
			_, err := (PublicationPlanner{Compozy: compozy, Git: &fakeGitEvidence{}}).Plan(
				context.Background(), scope, PlanInput{WorktreeRef: "wt_delivery"},
			)
			assertBlockedPlan(t, err, "trusted_scope_missing")
			if compozy.inspectCalls != 0 {
				t.Fatalf("Inspect calls = %d, want 0", compozy.inspectCalls)
			}
		})
	}
}

func TestPlannerRejectsForeignWorkspaceRecord(t *testing.T) {
	t.Parallel()

	inspection := validInspection()
	inspection.Worktree.WorkspaceID = "ws_foreign"
	planner := validPlanner(inspection)
	_, err := planner.Plan(context.Background(), trustedScope(), PlanInput{WorktreeRef: "wt_delivery"})
	assertBlockedPlan(t, err, "workspace_mismatch")
}

func TestPlannerRejectsNonReadyOrRelativePath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*WorktreeInspection)
		blocker string
	}{
		{name: "unavailable", mutate: func(i *WorktreeInspection) { i.Worktree.State = "missing" }, blocker: "worktree_unavailable"},
		{name: "pending", mutate: func(i *WorktreeInspection) { i.Worktree.State = "pending" }, blocker: "worktree_not_ready"},
		{name: "relative path", mutate: func(i *WorktreeInspection) { i.Worktree.Path = "relative/worktree" }, blocker: "worktree_path_invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inspection := validInspection()
			tt.mutate(&inspection)
			_, err := validPlanner(inspection).Plan(context.Background(), trustedScope(), PlanInput{WorktreeRef: "wt_delivery"})
			assertBlockedPlan(t, err, tt.blocker)
		})
	}
}

func TestPlannerBlocksDirtyDetachedDriftedAndUnreadableGit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		inspection func(*WorktreeInspection)
		snapshot   func(*GitSnapshot)
		gitErr     error
		blocker    string
	}{
		{name: "repo unavailable", inspection: func(i *WorktreeInspection) { i.Repo.GitAvailable = false }, blocker: "git_unreadable"},
		{name: "status missing", inspection: func(i *WorktreeInspection) { i.Status = nil }, blocker: "git_unreadable"},
		{name: "status read error", inspection: func(i *WorktreeInspection) { i.Status.ReadError = "redacted" }, blocker: "git_unreadable"},
		{name: "snapshot error", gitErr: errors.New("git failed"), blocker: "git_unreadable"},
		{name: "dirty snapshot", snapshot: func(s *GitSnapshot) { s.Clean = false }, blocker: "dirty_worktree"},
		{name: "dirty daemon status", inspection: func(i *WorktreeInspection) { i.Status.DirtyFiles = ptr(1) }, blocker: "dirty_worktree"},
		{name: "detached snapshot", snapshot: func(s *GitSnapshot) { s.Detached = true; s.Branch = "" }, blocker: "detached_head"},
		{name: "detached daemon status", inspection: func(i *WorktreeInspection) { i.Status.Detached = ptr(true) }, blocker: "detached_head"},
		{name: "branch drift", snapshot: func(s *GitSnapshot) { s.Branch = "feature/other" }, blocker: "branch_mismatch"},
		{name: "status branch drift", inspection: func(i *WorktreeInspection) { i.Status.Branch = ptr("feature/other") }, blocker: "branch_mismatch"},
		{name: "invalid head", snapshot: func(s *GitSnapshot) { s.HeadSHA = "not-a-sha" }, blocker: "head_invalid"},
		{name: "head drift", inspection: func(i *WorktreeInspection) { i.Status.HeadSHA = ptr("89abcdef0123456789abcdef0123456789abcdef") }, blocker: "head_invalid"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inspection := validInspection()
			if tt.inspection != nil {
				tt.inspection(&inspection)
			}
			snapshot := validSnapshot()
			if tt.snapshot != nil {
				tt.snapshot(&snapshot)
			}
			planner := PublicationPlanner{
				Compozy: &fakeWorktreeClient{inspection: inspection, plan: validExitPlan()},
				Git: &fakeGitEvidence{
					snapshot: snapshot, snapshotErr: tt.gitErr, upstream: testHeadSHA, baseAhead: 1,
				},
			}
			_, err := planner.Plan(context.Background(), trustedScope(), PlanInput{WorktreeRef: "wt_delivery"})
			assertBlockedPlan(t, err, tt.blocker)
		})
	}
}

func TestPlannerBlocksDivergedBehindMissingRemoteAndMissingForge(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		inspection  func(*WorktreeInspection)
		plan        func(*ExitPlan)
		upstream    string
		upstreamErr error
		baseAhead   *int
		baseErr     error
		blocker     string
	}{
		{name: "diverged", inspection: func(i *WorktreeInspection) { setTracking(i, 1, 1) }, blocker: "branch_diverged"},
		{name: "behind", inspection: func(i *WorktreeInspection) { setTracking(i, 0, 1) }, blocker: "branch_behind"},
		{name: "remote missing", plan: func(p *ExitPlan) { p.Actions = nil; p.Forge = nil; p.ForgeStatus = nil }, blocker: "remote_missing"},
		{name: "upstream unreadable", inspection: func(i *WorktreeInspection) { setTracking(i, 0, 0) }, upstreamErr: errors.New("no upstream"), blocker: "remote_missing"},
		{name: "forge missing", plan: func(p *ExitPlan) { p.Forge = nil }, blocker: "forge_unavailable"},
		{name: "forge provider missing", plan: func(p *ExitPlan) { p.Forge.Provider = "" }, blocker: "forge_unavailable"},
		{name: "forge default branch missing", plan: func(p *ExitPlan) { p.Forge.DefaultBranch = "" }, blocker: "forge_unavailable"},
		{name: "no enabled publication state", plan: func(p *ExitPlan) { p.Actions = nil }, blocker: "publication_state_ambiguous"},
		{name: "exit mismatch", plan: func(p *ExitPlan) { p.WorktreeID = "wt_foreign" }, blocker: "exit_plan_mismatch"},
		{name: "exit paused", plan: func(p *ExitPlan) { p.GlobalPauseCause = "operation in progress" }, blocker: "exit_plan_paused"},
		{name: "base delta unreadable", baseErr: errors.New("base missing"), blocker: "git_unreadable"},
		{name: "base delta drift", baseAhead: ptr(2), blocker: "publication_state_ambiguous"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inspection := validInspection()
			if tt.inspection != nil {
				tt.inspection(&inspection)
			}
			plan := validExitPlan()
			if tt.plan != nil {
				tt.plan(&plan)
			}
			upstream := tt.upstream
			if upstream == "" {
				upstream = testHeadSHA
			}
			baseAhead := 1
			if tt.baseAhead != nil {
				baseAhead = *tt.baseAhead
			}
			planner := PublicationPlanner{
				Compozy: &fakeWorktreeClient{inspection: inspection, plan: plan},
				Git: &fakeGitEvidence{
					snapshot: validSnapshot(), upstream: upstream, upstreamErr: tt.upstreamErr,
					baseAhead: baseAhead, baseAheadErr: tt.baseErr,
				},
			}
			_, err := planner.Plan(context.Background(), trustedScope(), PlanInput{WorktreeRef: "wt_delivery"})
			assertBlockedPlan(t, err, tt.blocker)
		})
	}
}

func TestPlannerReturnsNothingToPublishForCleanBaseIdenticalBranch(t *testing.T) {
	t.Parallel()

	inspection := validInspection()
	inspection.Status.AheadOfBase = ptr(0)
	plan := validExitPlan()
	plan.Actions = []ExitAction{{Action: "push", Enabled: false}}
	plan.Forge = nil
	planner := PublicationPlanner{
		Compozy: &fakeWorktreeClient{inspection: inspection, plan: plan},
		Git:     &fakeGitEvidence{snapshot: validSnapshot(), baseAhead: 0},
	}
	got, err := planner.Plan(context.Background(), trustedScope(), PlanInput{WorktreeRef: "wt_delivery"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got.Disposition != DispositionNothing || got.HeadSHA != testHeadSHA || !got.Clean {
		t.Fatalf("Plan() = %#v", got)
	}
	if got.WorktreePath != validInspection().Worktree.Path || got.BaseBranch != "main" {
		t.Fatalf("Plan() identity = %#v", got)
	}
}

func TestPlannerReturnsPublishableWithExactHeadPrefillAndForge(t *testing.T) {
	t.Parallel()

	git := &fakeGitEvidence{snapshot: validSnapshot(), upstream: testHeadSHA, baseAhead: 1}
	planner := PublicationPlanner{
		Compozy: &fakeWorktreeClient{inspection: validInspection(), plan: validExitPlan()},
		Git:     git,
	}
	got, err := planner.Plan(context.Background(), trustedScope(), PlanInput{WorktreeRef: "wt_delivery"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got.Disposition != DispositionPublishable || got.HeadSHA != testHeadSHA || !got.Clean {
		t.Fatalf("Plan() = %#v", got)
	}
	if got.ExitPlan.PRPrefill == nil || got.ExitPlan.PRPrefill.Title != "Feature title" {
		t.Fatalf("Exit plan prefill = %#v", got.ExitPlan.PRPrefill)
	}
	if got.ExitPlan.Forge == nil || got.ExitPlan.Forge.Provider != "github" {
		t.Fatalf("Exit plan forge = %#v", got.ExitPlan.Forge)
	}
	if git.upstreamCalls != 0 || git.baseAheadCalls != 1 {
		t.Fatalf("Git calls = upstream:%d base-ahead:%d, want 0 and 1", git.upstreamCalls, git.baseAheadCalls)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if !strings.Contains(string(encoded), `"head_sha":"`+testHeadSHA+`"`) {
		t.Fatalf("encoded plan = %s", encoded)
	}
}

func TestPlannerReturnsPublishableAfterPushWithNullAheadOfBase(t *testing.T) {
	t.Parallel()

	inspection := validInspection()
	setTracking(&inspection, 0, 0)
	if inspection.Status.AheadOfBase != nil {
		t.Fatal("fixture AheadOfBase must be nil for a tracking branch")
	}
	plan := validExitPlan()
	plan.Primary = "open_pr"
	plan.Actions = []ExitAction{
		{Action: "push", Enabled: false},
		{Action: "open_pr", Enabled: true, Publish: true},
	}
	git := &fakeGitEvidence{snapshot: validSnapshot(), upstream: testHeadSHA, baseAhead: 1}
	planner := PublicationPlanner{Compozy: &fakeWorktreeClient{inspection: inspection, plan: plan}, Git: git}

	got, err := planner.Plan(context.Background(), trustedScope(), PlanInput{WorktreeRef: "wt_delivery"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got.Disposition != DispositionPublishable {
		t.Fatalf("Disposition = %q, want %q", got.Disposition, DispositionPublishable)
	}
	if git.upstreamCalls != 1 || git.baseAheadCalls != 1 {
		t.Fatalf("Git calls = upstream:%d base-ahead:%d, want 1 each", git.upstreamCalls, git.baseAheadCalls)
	}
}

func TestPlannerRecognizesAnExistingPRWithoutInventingAnOperation(t *testing.T) {
	t.Parallel()

	plan := validExitPlan()
	plan.Actions = []ExitAction{{Action: "view_pr", Enabled: true, URL: "https://github.com/acme/repo/pull/42"}}
	plan.ForgeStatus = &ForgeStatus{Provider: "github", PRURL: "https://github.com/acme/repo/pull/42"}
	inspection := validInspection()
	setTracking(&inspection, 0, 0)
	planner := PublicationPlanner{
		Compozy: &fakeWorktreeClient{inspection: inspection, plan: plan},
		Git:     &fakeGitEvidence{snapshot: validSnapshot(), upstream: testHeadSHA, baseAhead: 1},
	}
	got, err := planner.Plan(context.Background(), trustedScope(), PlanInput{WorktreeRef: "wt_delivery"})
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if got.Disposition != DispositionPublishable {
		t.Fatalf("Disposition = %q, want %q", got.Disposition, DispositionPublishable)
	}
	encoded, err := json.Marshal(got)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	if strings.Contains(string(encoded), "op_id") {
		t.Fatalf("plan invented an operation: %s", encoded)
	}
}

func TestPlannerKeepsMalformedInputAndTransportErrorsDistinct(t *testing.T) {
	t.Parallel()

	planner := PublicationPlanner{Compozy: &fakeWorktreeClient{inspectErr: errors.New("daemon unavailable")}, Git: &fakeGitEvidence{}}
	if _, err := planner.Plan(context.Background(), trustedScope(), PlanInput{}); err == nil {
		t.Fatal("blank worktree ref error = nil")
	} else {
		var blocked *BlockedPlanError
		if errors.As(err, &blocked) {
			t.Fatalf("blank input error = %v, want non-blocked validation error", err)
		}
	}
	if _, err := planner.Plan(context.Background(), trustedScope(), PlanInput{WorktreeRef: "wt_delivery"}); err == nil {
		t.Fatal("transport error = nil")
	} else {
		var blocked *BlockedPlanError
		if errors.As(err, &blocked) {
			t.Fatalf("transport error = %v, want distinct error", err)
		}
	}
}

func assertBlockedPlan(t *testing.T, err error, blocker string) PlanOutput {
	t.Helper()
	if err == nil {
		t.Fatalf("Plan() error = nil, want blocker %q", blocker)
	}
	var blocked *BlockedPlanError
	if !errors.As(err, &blocked) {
		t.Fatalf("Plan() error = %T %v, want *BlockedPlanError", err, err)
	}
	if blocked.Plan.Disposition != DispositionBlocked {
		t.Fatalf("Disposition = %q, want %q", blocked.Plan.Disposition, DispositionBlocked)
	}
	if !reflect.DeepEqual(blocked.Plan.Blockers, []string{blocker}) {
		t.Fatalf("Blockers = %#v, want [%q]", blocked.Plan.Blockers, blocker)
	}
	if !strings.Contains(err.Error(), blocker) || len(err.Error()) > 256 {
		t.Fatalf("safe error = %q", err)
	}
	return blocked.Plan
}

func trustedScope() TrustedScope {
	return TrustedScope{WorkspaceID: "ws_trusted", WorkspaceRoot: "/trusted/workspace"}
}

func validInspection() WorktreeInspection {
	branch := "feature/delivery"
	detached := false
	dirty := 0
	aheadOfBase := 1
	hasUpstream := false
	return WorktreeInspection{
		Worktree: Worktree{
			ID:          "wt_delivery",
			WorkspaceID: "ws_trusted",
			Branch:      branch,
			Path:        "/trusted/workspace/.worktrees/delivery",
			State:       "ready",
			BaseRef:     "main",
		},
		Status: &WorktreeStatus{
			Branch:      &branch,
			Detached:    &detached,
			HeadSHA:     ptr(testHeadSHA),
			DirtyFiles:  &dirty,
			HasUpstream: &hasUpstream,
			Ahead:       nil,
			AheadOfBase: &aheadOfBase,
			Behind:      nil,
		},
		Forge: &ForgeStatus{Provider: "github"},
		Repo:  WorktreeRepo{GitBacked: true, GitAvailable: true},
	}
}

func validSnapshot() GitSnapshot {
	return GitSnapshot{HeadSHA: testHeadSHA, Branch: "feature/delivery", Clean: true}
}

func validExitPlan() ExitPlan {
	return ExitPlan{
		WorktreeID: "wt_delivery",
		Primary:    "push",
		Actions: []ExitAction{
			{Action: "push", Enabled: true, Publish: true},
			{Action: "open_pr", Enabled: false, BlockedReason: "Push commits before opening pull requests."},
		},
		Forge:       &ForgeCapabilities{Provider: "github", DefaultBranch: "main"},
		ForgeStatus: &ForgeStatus{Provider: "github"},
		PRPrefill:   &PRPrefill{Title: "Feature title", Body: "Feature body"},
		Base:        "main",
	}
}

func validPlanner(inspection WorktreeInspection) PublicationPlanner {
	return PublicationPlanner{
		Compozy: &fakeWorktreeClient{inspection: inspection, plan: validExitPlan()},
		Git:     &fakeGitEvidence{snapshot: validSnapshot(), upstream: testHeadSHA, baseAhead: 1},
	}
}

func setTracking(inspection *WorktreeInspection, ahead, behind int) {
	inspection.Status.HasUpstream = ptr(true)
	inspection.Status.Ahead = &ahead
	inspection.Status.AheadOfBase = nil
	inspection.Status.Behind = &behind
}

type fakeWorktreeClient struct {
	inspection WorktreeInspection
	plan       ExitPlan
	inspectErr error
	planErr    error

	inspectCalls int
	exitCalls    int
}

func (f *fakeWorktreeClient) Inspect(_ context.Context, _ TrustedScope, _ string) (WorktreeInspection, error) {
	f.inspectCalls++
	return f.inspection, f.inspectErr
}

func (f *fakeWorktreeClient) ExitPlan(_ context.Context, _ TrustedScope, _ string) (ExitPlan, error) {
	f.exitCalls++
	return f.plan, f.planErr
}

func (f *fakeWorktreeClient) Push(context.Context, TrustedScope, string) (Operation, error) {
	return Operation{}, errors.New("unexpected Push call")
}

func (f *fakeWorktreeClient) OpenPR(context.Context, TrustedScope, string, PRPrefill, string) (Operation, error) {
	return Operation{}, errors.New("unexpected OpenPR call")
}

type fakeGitEvidence struct {
	snapshot     GitSnapshot
	snapshotErr  error
	upstream     string
	upstreamErr  error
	baseAhead    int
	baseAheadErr error

	upstreamCalls  int
	baseAheadCalls int
}

func (f *fakeGitEvidence) Snapshot(context.Context, string) (GitSnapshot, error) {
	return f.snapshot, f.snapshotErr
}

func (f *fakeGitEvidence) UpstreamHead(context.Context, string) (string, error) {
	f.upstreamCalls++
	return f.upstream, f.upstreamErr
}

func (f *fakeGitEvidence) CommitsAheadOfBase(context.Context, string, string) (int, error) {
	f.baseAheadCalls++
	return f.baseAhead, f.baseAheadErr
}
