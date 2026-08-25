package publication

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestCLIClientUsesExactArgumentsAndDecodesPublicContracts(t *testing.T) {
	t.Parallel()

	runner := &scriptedCommandRunner{results: []CommandResult{
		{Stdout: []byte(inspectFixtureJSON)},
		{Stdout: []byte(exitFixtureJSON)},
		{Stdout: []byte(`{"op_id":"op_push"}`)},
		{Stdout: []byte(`{"op_id":"op_pr"}`)},
	}}
	client := CLIClient{Executable: "/controlled/compozy", Runner: runner}
	scope := TrustedScope{WorkspaceID: "ws_trusted", WorkspaceRoot: "/trusted/workspace"}

	inspection, err := client.Inspect(context.Background(), scope, "wt_delivery")
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	if inspection.Worktree.WorkspaceID != scope.WorkspaceID || inspection.Worktree.BaseRef != "main" {
		t.Fatalf("Inspect() worktree = %#v", inspection.Worktree)
	}
	if inspection.Status == nil || inspection.Status.HeadSHA == nil || *inspection.Status.HeadSHA != testHeadSHA {
		t.Fatalf("Inspect() status = %#v", inspection.Status)
	}

	plan, err := client.ExitPlan(context.Background(), scope, "wt_delivery")
	if err != nil {
		t.Fatalf("ExitPlan() error = %v", err)
	}
	if plan.WorktreeID != "wt_delivery" || plan.PRPrefill == nil || plan.PRPrefill.Title != "Feature title" {
		t.Fatalf("ExitPlan() = %#v", plan)
	}

	push, err := client.Push(context.Background(), scope, "wt_delivery")
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if push.OperationID != "op_push" {
		t.Fatalf("Push() = %#v", push)
	}

	prefill := PRPrefill{
		Title: "Feature $(touch nope) `still literal`",
		Body:  "line one; echo nope\nline two with \"quotes\"",
	}
	opened, err := client.OpenPR(context.Background(), scope, "wt_delivery", prefill, "main")
	if err != nil {
		t.Fatalf("OpenPR() error = %v", err)
	}
	if opened.OperationID != "op_pr" {
		t.Fatalf("OpenPR() = %#v", opened)
	}

	want := []Command{
		{Executable: "/controlled/compozy", Args: []string{"worktree", "inspect", "--workspace", "ws_trusted", "-o", "json", "--", "wt_delivery"}},
		{Executable: "/controlled/compozy", Args: []string{"worktree", "exit", "--workspace", "ws_trusted", "-o", "json", "--", "wt_delivery"}},
		{Executable: "/controlled/compozy", Args: []string{"worktree", "push", "--workspace", "ws_trusted", "-o", "json", "--", "wt_delivery"}},
		{Executable: "/controlled/compozy", Args: []string{"worktree", "pr", "--workspace", "ws_trusted", "--title", prefill.Title, "--body", prefill.Body, "--base", "main", "-o", "json", "--", "wt_delivery"}},
	}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, want)
	}
}

func TestCLIClientKeepsFlagLikeRefsPositional(t *testing.T) {
	t.Parallel()

	refs := []string{"--help", "--workspace=foreign", "--"}
	for _, ref := range refs {
		ref := ref
		t.Run(ref, func(t *testing.T) {
			t.Parallel()
			runner := &scriptedCommandRunner{results: []CommandResult{{Stdout: []byte(strings.Replace(inspectFixtureJSON, "wt_delivery", ref, 1))}}}
			client := CLIClient{Executable: "/controlled/compozy", Runner: runner}
			inspection, err := client.Inspect(context.Background(), TrustedScope{WorkspaceID: "ws_trusted"}, ref)
			if err != nil {
				t.Fatalf("Inspect() error = %v", err)
			}
			if inspection.Worktree.ID != ref {
				t.Fatalf("worktree id = %q, want %q", inspection.Worktree.ID, ref)
			}
			wantArgs := []string{"worktree", "inspect", "--workspace", "ws_trusted", "-o", "json", "--", ref}
			if !reflect.DeepEqual(runner.commands[0].Args, wantArgs) {
				t.Fatalf("args = %#v, want %#v", runner.commands[0].Args, wantArgs)
			}
		})
	}
}

func TestCLIClientRejectsInvalidBoundaryInputsBeforeExecution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		executable string
		scope      TrustedScope
		ref        string
	}{
		{name: "relative executable", executable: "compozy", scope: TrustedScope{WorkspaceID: "ws_trusted"}, ref: "wt_delivery"},
		{name: "blank workspace", executable: "/controlled/compozy", scope: TrustedScope{}, ref: "wt_delivery"},
		{name: "blank ref", executable: "/controlled/compozy", scope: TrustedScope{WorkspaceID: "ws_trusted"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			runner := &scriptedCommandRunner{}
			client := CLIClient{Executable: tt.executable, Runner: runner}
			if _, err := client.Inspect(context.Background(), tt.scope, tt.ref); err == nil {
				t.Fatal("Inspect() error = nil, want validation error")
			}
			if len(runner.commands) != 0 {
				t.Fatalf("commands = %#v, want none", runner.commands)
			}
		})
	}
}

func TestCLIClientRejectsMalformedResponsesAndMismatchedIDs(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		method string
		stdout string
	}{
		{name: "malformed inspection", method: "inspect", stdout: "{"},
		{name: "empty inspection id", method: "inspect", stdout: strings.Replace(inspectFixtureJSON, `"id": "wt_delivery"`, `"id": ""`, 1)},
		{name: "mismatched inspection id", method: "inspect", stdout: strings.Replace(inspectFixtureJSON, "wt_delivery", "wt_foreign", 1)},
		{name: "malformed exit plan", method: "exit", stdout: "not-json"},
		{name: "mismatched exit plan id", method: "exit", stdout: strings.Replace(exitFixtureJSON, "wt_delivery", "wt_foreign", 1)},
		{name: "empty operation", method: "push", stdout: `{}`},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			client := CLIClient{Executable: "/controlled/compozy", Runner: &scriptedCommandRunner{results: []CommandResult{{Stdout: []byte(tt.stdout)}}}}
			scope := TrustedScope{WorkspaceID: "ws_trusted"}
			var err error
			switch tt.method {
			case "inspect":
				_, err = client.Inspect(context.Background(), scope, "wt_delivery")
			case "exit":
				_, err = client.ExitPlan(context.Background(), scope, "wt_delivery")
			case "push":
				_, err = client.Push(context.Background(), scope, "wt_delivery")
			}
			if err == nil {
				t.Fatal("error = nil, want response rejection")
			}
		})
	}
}

func TestPublicationWireTypesUseExactSnakeCaseKeys(t *testing.T) {
	t.Parallel()

	payload := struct {
		Scope      TrustedScope       `json:"scope"`
		Inspection WorktreeInspection `json:"inspection"`
		Plan       ExitPlan           `json:"plan"`
	}{
		Scope: TrustedScope{WorkspaceID: "ws_trusted", WorkspaceRoot: "/trusted/workspace"},
		Inspection: WorktreeInspection{
			Worktree: Worktree{BaseRef: "main"},
			Status:   &WorktreeStatus{HeadSHA: ptr(testHeadSHA)},
		},
		Plan: ExitPlan{
			Actions:          []ExitAction{{BlockedReason: "wait"}},
			GlobalPauseCause: "paused",
			BrowserURL:       "https://example.invalid/compare",
			ForgeStatus:      &ForgeStatus{PRURL: "https://example.invalid/pull/1"},
			PRPrefill:        &PRPrefill{Title: "title"},
		},
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, key := range []string{
		`"workspace_id"`, `"workspace_root"`, `"base_ref"`, `"blocked_reason"`,
		`"global_pause_cause"`, `"browser_url"`, `"forge_status"`, `"pr_prefill"`, `"head_sha"`,
	} {
		if !strings.Contains(string(encoded), key) {
			t.Fatalf("encoded payload %s does not contain %s", encoded, key)
		}
	}
}

type scriptedCommandRunner struct {
	commands []Command
	results  []CommandResult
	errors   []error
}

func (r *scriptedCommandRunner) Run(_ context.Context, command Command) (CommandResult, error) {
	r.commands = append(r.commands, command)
	if len(r.results) == 0 {
		return CommandResult{}, errors.New("unexpected command")
	}
	result := r.results[0]
	r.results = r.results[1:]
	if len(r.errors) == 0 {
		return result, nil
	}
	err := r.errors[0]
	r.errors = r.errors[1:]
	return result, err
}

func ptr[T any](value T) *T { return &value }

func absoluteTestPath(parts ...string) string {
	return filepath.Join(append([]string{string(filepath.Separator)}, parts...)...)
}

const testHeadSHA = "0123456789abcdef0123456789abcdef01234567"

const inspectFixtureJSON = `{
  "worktree": {
    "id": "wt_delivery",
    "workspace_id": "ws_trusted",
    "branch": "feature/delivery",
    "path": "/trusted/workspace/.worktrees/delivery",
    "state": "ready",
    "base_ref": "main"
  },
  "status": {
    "branch": "feature/delivery",
    "detached": false,
    "head_sha": "0123456789abcdef0123456789abcdef01234567",
    "dirty_files": 0,
    "has_upstream": false,
    "ahead": null,
    "ahead_of_base": 1,
    "behind": null
  },
  "forge": {"provider": "github", "pr_url": ""},
  "repo": {"git_backed": true, "git_available": true}
}`

const exitFixtureJSON = `{
  "worktree_id": "wt_delivery",
  "primary": "push",
  "actions": [
    {"action": "push", "enabled": true, "publish": true},
    {"action": "open_pr", "enabled": false, "blocked_reason": "Push commits before opening pull requests."}
  ],
  "forge": {"provider": "github", "default_branch": "main"},
  "forge_status": {"provider": "github", "pr_url": ""},
  "pr_prefill": {"title": "Feature title", "body": "Feature body"},
  "base": "main"
}`
