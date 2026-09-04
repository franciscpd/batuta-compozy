package compozyclient

import (
	"context"
	"errors"
	"github.com/batuta-ai/core/publication"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestCLIClientUsesExactArgumentsAndDecodesPublicContracts(t *testing.T) {
	t.Parallel()

	runner := &scriptedCommandRunner{results: []publication.CommandResult{
		{Stdout: []byte(statusFixtureJSON)},
		{Stdout: []byte(inspectFixtureJSON)},
		{Stdout: []byte(exitFixtureJSON)},
		{Stdout: []byte(`{"op_id":"op_push"}`)},
		{Stdout: []byte(`{"op_id":"op_pr"}`)},
	}}
	client := CLIClient{Executable: "/controlled/compozy", Runner: runner}
	scope := publication.TrustedScope{WorkspaceID: "ws_trusted", WorkspaceRoot: "/trusted/workspace"}

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
		t.Fatalf("publication.ExitPlan() error = %v", err)
	}
	if plan.WorktreeID != "wt_delivery" || plan.PRPrefill == nil || plan.PRPrefill.Title != "Feature title" {
		t.Fatalf("publication.ExitPlan() = %#v", plan)
	}

	push, err := client.Push(context.Background(), scope, "wt_delivery")
	if err != nil {
		t.Fatalf("Push() error = %v", err)
	}
	if push.OperationID != "op_push" {
		t.Fatalf("Push() = %#v", push)
	}

	prefill := publication.PRPrefill{
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

	want := []publication.Command{
		{Executable: "/controlled/compozy", Args: []string{"worktree", "status", "--workspace", "ws_trusted", "--refresh", "-o", "json", "--", "wt_delivery"}, StdoutLimit: 1024 * 1024, StderrLimit: 64 * 1024},
		{Executable: "/controlled/compozy", Args: []string{"worktree", "inspect", "--workspace", "ws_trusted", "-o", "json", "--", "wt_delivery"}, StdoutLimit: 1024 * 1024, StderrLimit: 64 * 1024},
		{Executable: "/controlled/compozy", Args: []string{"worktree", "exit", "--workspace", "ws_trusted", "-o", "json", "--", "wt_delivery"}, StdoutLimit: 1024 * 1024, StderrLimit: 64 * 1024},
		{Executable: "/controlled/compozy", Args: []string{"worktree", "push", "--workspace", "ws_trusted", "-o", "json", "--", "wt_delivery"}, StdoutLimit: 1024 * 1024, StderrLimit: 64 * 1024},
		{Executable: "/controlled/compozy", Args: []string{"worktree", "pr", "--workspace", "ws_trusted", "--title", prefill.Title, "--body", prefill.Body, "--base", "main", "-o", "json", "--", "wt_delivery"}, StdoutLimit: 1024 * 1024, StderrLimit: 64 * 1024},
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
			runner := &scriptedCommandRunner{results: []publication.CommandResult{
				{Stdout: []byte(strings.Replace(statusFixtureJSON, "wt_delivery", ref, 1))},
				{Stdout: []byte(strings.Replace(inspectFixtureJSON, "wt_delivery", ref, 1))},
			}}
			client := CLIClient{Executable: "/controlled/compozy", Runner: runner}
			inspection, err := client.Inspect(context.Background(), publication.TrustedScope{WorkspaceID: "ws_trusted"}, ref)
			if err != nil {
				t.Fatalf("Inspect() error = %v", err)
			}
			if inspection.Worktree.ID != ref {
				t.Fatalf("worktree id = %q, want %q", inspection.Worktree.ID, ref)
			}
			wantArgs := []string{"worktree", "status", "--workspace", "ws_trusted", "--refresh", "-o", "json", "--", ref}
			if !reflect.DeepEqual(runner.commands[0].Args, wantArgs) {
				t.Fatalf("args = %#v, want %#v", runner.commands[0].Args, wantArgs)
			}
			wantInspectArgs := []string{"worktree", "inspect", "--workspace", "ws_trusted", "-o", "json", "--", ref}
			if !reflect.DeepEqual(runner.commands[1].Args, wantInspectArgs) {
				t.Fatalf("inspect args = %#v, want %#v", runner.commands[1].Args, wantInspectArgs)
			}
		})
	}
}

func TestCLIClientRejectsInvalidBoundaryInputsBeforeExecution(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		executable string
		scope      publication.TrustedScope
		ref        string
	}{
		{name: "relative executable", executable: "compozy", scope: publication.TrustedScope{WorkspaceID: "ws_trusted"}, ref: "wt_delivery"},
		{name: "blank workspace", executable: "/controlled/compozy", scope: publication.TrustedScope{}, ref: "wt_delivery"},
		{name: "blank ref", executable: "/controlled/compozy", scope: publication.TrustedScope{WorkspaceID: "ws_trusted"}},
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
		{name: "malformed refreshed status", method: "inspect", stdout: "{"},
		{name: "mismatched refreshed status id", method: "inspect", stdout: strings.Replace(statusFixtureJSON, "wt_delivery", "wt_foreign", 1)},
		{name: "empty refreshed status", method: "inspect", stdout: `{"worktree_id":"wt_delivery","status":null}`},
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
			results := []publication.CommandResult{{Stdout: []byte(tt.stdout)}}
			if tt.method == "inspect" && !strings.Contains(tt.name, "refreshed status") {
				results = []publication.CommandResult{{Stdout: []byte(statusFixtureJSON)}, {Stdout: []byte(tt.stdout)}}
			}
			client := CLIClient{Executable: "/controlled/compozy", Runner: &scriptedCommandRunner{results: results}}
			scope := publication.TrustedScope{WorkspaceID: "ws_trusted"}
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

func TestCLIClientRejectsTruncatedOutputAndBoundsCommands(t *testing.T) {
	t.Parallel()

	scope := publication.TrustedScope{WorkspaceID: "ws_trusted"}
	truncated := &scriptedCommandRunner{results: []publication.CommandResult{{
		Stdout: []byte(exitFixtureJSON), StdoutTruncated: true,
	}}}
	if _, err := (CLIClient{Executable: "/controlled/compozy", Runner: truncated}).ExitPlan(
		context.Background(), scope, "wt_delivery",
	); err == nil {
		t.Fatal("publication.ExitPlan(truncated) error = nil")
	}

	blocking := blockingPublicationCommandRunner{}
	started := time.Now()
	_, err := (CLIClient{
		Executable: "/controlled/compozy", Runner: blocking, Timeout: 10 * time.Millisecond,
	}).ExitPlan(context.Background(), scope, "wt_delivery")
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("publication.ExitPlan(timeout) error = %v, elapsed %s", err, time.Since(started))
	}
}

type scriptedCommandRunner struct {
	commands []publication.Command
	results  []publication.CommandResult
	errors   []error
}

type blockingPublicationCommandRunner struct{}

func (blockingPublicationCommandRunner) Run(ctx context.Context, _ publication.Command) (publication.CommandResult, error) {
	<-ctx.Done()
	return publication.CommandResult{}, ctx.Err()
}

func (r *scriptedCommandRunner) Run(_ context.Context, command publication.Command) (publication.CommandResult, error) {
	r.commands = append(r.commands, command)
	if len(r.results) == 0 {
		return publication.CommandResult{}, errors.New("unexpected command")
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

const statusFixtureJSON = `{
  "worktree_id": "wt_delivery",
  "status": {
    "branch": "feature/delivery",
    "detached": false,
    "head_sha": "0123456789abcdef0123456789abcdef01234567",
    "dirty_files": 0,
    "has_upstream": false,
    "ahead": null,
    "ahead_of_base": 1,
    "behind": null
  }
}`

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
