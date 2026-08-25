package publication

import "context"

type TrustedScope struct {
	WorkspaceID   string `json:"workspace_id"`
	WorkspaceRoot string `json:"workspace_root"`
}

type Worktree struct {
	ID          string `json:"id"`
	WorkspaceID string `json:"workspace_id"`
	Branch      string `json:"branch"`
	Path        string `json:"path"`
	State       string `json:"state"`
	BaseRef     string `json:"base_ref,omitempty"`
}

type WorktreeStatus struct {
	Branch      *string `json:"branch"`
	Detached    *bool   `json:"detached"`
	HeadSHA     *string `json:"head_sha"`
	DirtyFiles  *int    `json:"dirty_files"`
	HasUpstream *bool   `json:"has_upstream"`
	Ahead       *int    `json:"ahead"`
	AheadOfBase *int    `json:"ahead_of_base"`
	Behind      *int    `json:"behind"`
	ReadError   string  `json:"read_error,omitempty"`
}

type WorktreeRepo struct {
	GitBacked    bool   `json:"git_backed"`
	GitAvailable bool   `json:"git_available"`
	Diagnostic   string `json:"diagnostic,omitempty"`
}

type WorktreeInspection struct {
	Worktree Worktree        `json:"worktree"`
	Status   *WorktreeStatus `json:"status"`
	Forge    *ForgeStatus    `json:"forge"`
	Repo     WorktreeRepo    `json:"repo"`
}

type ExitAction struct {
	Action        string `json:"action"`
	Enabled       bool   `json:"enabled"`
	BlockedReason string `json:"blocked_reason,omitempty"`
	Publish       bool   `json:"publish,omitempty"`
	URL           string `json:"url,omitempty"`
}

type ExitPlan struct {
	WorktreeID       string             `json:"worktree_id"`
	Primary          string             `json:"primary,omitempty"`
	Actions          []ExitAction       `json:"actions"`
	GlobalPauseCause string             `json:"global_pause_cause,omitempty"`
	BrowserURL       string             `json:"browser_url,omitempty"`
	Forge            *ForgeCapabilities `json:"forge,omitempty"`
	ForgeStatus      *ForgeStatus       `json:"forge_status,omitempty"`
	PRPrefill        *PRPrefill         `json:"pr_prefill,omitempty"`
	Base             string             `json:"base,omitempty"`
}

type ForgeCapabilities struct {
	Provider      string `json:"provider"`
	DefaultBranch string `json:"default_branch,omitempty"`
}

type ForgeStatus struct {
	Provider string `json:"provider"`
	PRURL    string `json:"pr_url,omitempty"`
}

type PRPrefill struct {
	Title string `json:"title,omitempty"`
	Body  string `json:"body,omitempty"`
}

type Operation struct {
	OperationID string `json:"op_id"`
}

type WorktreeClient interface {
	Inspect(context.Context, TrustedScope, string) (WorktreeInspection, error)
	ExitPlan(context.Context, TrustedScope, string) (ExitPlan, error)
	Push(context.Context, TrustedScope, string) (Operation, error)
	OpenPR(context.Context, TrustedScope, string, PRPrefill, string) (Operation, error)
}

type GitEvidence interface {
	Snapshot(context.Context, string) (GitSnapshot, error)
	UpstreamHead(context.Context, string) (string, error)
	CommitsAheadOfBase(context.Context, string, string) (int, error)
}
