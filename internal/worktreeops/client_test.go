package worktreeops

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

const worktreeTestSHA = "0123456789abcdef0123456789abcdef01234567"

func TestTaskWorktreeIdentityIsDeterministicCollisionSafeAndCanonical(t *testing.T) {
	t.Parallel()

	input := IdentityInput{
		WorkspaceID: "workspace-demo", DeliveryID: "sha256:" + strings.Repeat("a", 64),
		Wave: 2, Slug: "Checkout UI", TaskID: "TASK_02/Auth Form", Execution: 3, BaseSHA: worktreeTestSHA,
	}
	first, err := DeriveIdentity(input)
	if err != nil {
		t.Fatalf("DeriveIdentity() error = %v", err)
	}
	second, err := DeriveIdentity(input)
	if err != nil || !reflect.DeepEqual(first, second) {
		t.Fatalf("DeriveIdentity(replay) = %#v, error %v, want %#v", second, err, first)
	}
	if !strings.HasPrefix(first.Name, "batuta-checkout-ui-task-02-auth-form-a3-") ||
		!strings.HasPrefix(first.Branch, "batuta/task/aaaaaaaaaaaa/task-02-auth-form/a3-") ||
		first.TaskID != input.TaskID ||
		!strings.HasPrefix(first.OperationID, "sha256:") || !strings.HasPrefix(first.RequestDigest, "sha256:") {
		t.Fatalf("identity = %#v", first)
	}
	changed := input
	changed.Wave++
	different, err := DeriveIdentity(changed)
	if err != nil {
		t.Fatalf("DeriveIdentity(changed) error = %v", err)
	}
	if different.Name == first.Name || different.Branch == first.Branch || different.OperationID == first.OperationID {
		t.Fatalf("changed immutable identity collided: first %#v, changed %#v", first, different)
	}
}

func TestTaskWorktreeIdentityRejectsInvalidImmutableInput(t *testing.T) {
	t.Parallel()

	valid := IdentityInput{
		WorkspaceID: "workspace-demo", DeliveryID: "sha256:" + strings.Repeat("a", 64),
		Wave: 1, Slug: "demo", TaskID: "task_01", Execution: 1, BaseSHA: worktreeTestSHA,
	}
	for _, test := range []struct {
		name   string
		mutate func(*IdentityInput)
	}{
		{name: "blank workspace", mutate: func(input *IdentityInput) { input.WorkspaceID = " " }},
		{name: "malformed delivery", mutate: func(input *IdentityInput) { input.DeliveryID = "delivery" }},
		{name: "missing wave", mutate: func(input *IdentityInput) { input.Wave = 0 }},
		{name: "blank slug", mutate: func(input *IdentityInput) { input.Slug = "///" }},
		{name: "blank task", mutate: func(input *IdentityInput) { input.TaskID = "..." }},
		{name: "execution above ceiling", mutate: func(input *IdentityInput) { input.Execution = 5 }},
		{name: "malformed base", mutate: func(input *IdentityInput) { input.BaseSHA = "main" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			input := valid
			test.mutate(&input)
			if _, err := DeriveIdentity(input); err == nil {
				t.Fatal("DeriveIdentity() error = nil")
			}
		})
	}
}

func TestCLIClientCreateUsesExactBoundedCommandAndAcceptsAdditiveJSON(t *testing.T) {
	t.Parallel()

	repository := filepath.Join(string(filepath.Separator), "trusted", "repository")
	managed := filepath.Join(string(filepath.Separator), "managed", "worktrees", "task-1")
	request := CreateRequest{Name: "batuta-demo-task-01-a1-deadbeef", Branch: "batuta/task/aaaaaaaaaaaa/task-01/a1-deadbeef", BaseSHA: worktreeTestSHA}
	runner := &recordingRunner{result: publication.CommandResult{Stdout: []byte(worktreeJSON(
		"wt_task_01", "workspace-demo", request.Name, request.Branch, managed, "pending", "none", request.BaseSHA, "",
	)[:len(worktreeJSON("wt_task_01", "workspace-demo", request.Name, request.Branch, managed, "pending", "none", request.BaseSHA, ""))-1] + `,"future_field":{"nested":true}}`)}}
	client := CLIClient{Executable: "/controlled/compozy", Runner: runner}

	got, err := client.Create(context.Background(), publication.TrustedScope{
		WorkspaceID: "workspace-demo", WorkspaceRoot: repository,
	}, request)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	wantCommand := publication.Command{
		Executable: "/controlled/compozy", Directory: repository,
		Args:        []string{"worktree", "create", request.Name, "--workspace", "workspace-demo", "--branch", request.Branch, "--base", request.BaseSHA, "-o", "json"},
		StdoutLimit: WorktreeStdoutLimit, StderrLimit: WorktreeStderrLimit,
	}
	if !reflect.DeepEqual(runner.commands, []publication.Command{wantCommand}) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, []publication.Command{wantCommand})
	}
	if got.ID != "wt_task_01" || got.Root != managed || got.WorkspaceID != "workspace-demo" ||
		got.RepositoryRoot != repository || got.RepositoryIdentity == "" || got.BaseSHA != request.BaseSHA ||
		got.State != "pending" || got.Setup.State != "none" {
		t.Fatalf("Create() = %#v", got)
	}
}

func TestCLIClientFindsExactWorktreeByDeterministicName(t *testing.T) {
	t.Parallel()

	repository := filepath.Join(string(filepath.Separator), "trusted", "repository")
	managed := filepath.Join(string(filepath.Separator), "managed", "worktrees", "task-1")
	name := "batuta-demo-task-01-a1-deadbeef"
	raw := worktreeJSON(
		"wt_task_01", "workspace-demo", name,
		"batuta/task/aaaaaaaaaaaa/task-01/a1-deadbeef", managed, "ready", "ok", worktreeTestSHA, worktreeTestSHA,
	)
	runner := &recordingRunner{result: publication.CommandResult{Stdout: []byte("[" + raw + "]")}}
	client := CLIClient{Executable: "/controlled/compozy", Runner: runner}
	scope := publication.TrustedScope{WorkspaceID: "workspace-demo", WorkspaceRoot: repository}

	found, exists, err := client.FindByName(context.Background(), scope, name)
	if err != nil || !exists || found.ID != "wt_task_01" || found.Name != name {
		t.Fatalf("FindByName() = %#v, exists=%v, error=%v", found, exists, err)
	}
	want := publication.Command{
		Executable: "/controlled/compozy", Directory: repository,
		Args:        []string{"worktree", "list", "--workspace", "workspace-demo", "-o", "json"},
		StdoutLimit: WorktreeStdoutLimit, StderrLimit: WorktreeStderrLimit,
	}
	if !reflect.DeepEqual(runner.commands, []publication.Command{want}) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, []publication.Command{want})
	}
}

func TestCLIClientFindByNameIgnoresUnrelatedOperatorWorktree(t *testing.T) {
	t.Parallel()

	repository := filepath.Join(string(filepath.Separator), "trusted", "repository")
	name := "batuta-demo-task-01-a1-deadbeef"
	unrelated := worktreeJSON(
		"wt_operator", "workspace-demo", "operator-feature", "feature/operator",
		filepath.Join(string(filepath.Separator), "managed", "worktrees", "operator-feature"),
		"ready", "ok", "main", worktreeTestSHA,
	)
	target := worktreeJSON(
		"wt_task_01", "workspace-demo", name, "batuta/task/aaaaaaaaaaaa/task-01/a1-deadbeef",
		filepath.Join(string(filepath.Separator), "managed", "worktrees", "task-1"),
		"ready", "ok", worktreeTestSHA, worktreeTestSHA,
	)
	runner := &recordingRunner{result: publication.CommandResult{Stdout: []byte("[" + unrelated + "," + target + "]")}}
	client := CLIClient{Executable: "/controlled/compozy", Runner: runner}

	found, exists, err := client.FindByName(context.Background(), publication.TrustedScope{
		WorkspaceID: "workspace-demo", WorkspaceRoot: repository,
	}, name)
	if err != nil || !exists || found.ID != "wt_task_01" {
		t.Fatalf("FindByName(mixed workspace) = %#v, exists=%v, error=%v", found, exists, err)
	}
}

func TestCLIClientInspectAndRemoveUseCanonicalIDs(t *testing.T) {
	t.Parallel()

	repository := filepath.Join(string(filepath.Separator), "trusted", "repository")
	managed := filepath.Join(string(filepath.Separator), "managed", "worktrees", "task-1")
	raw := worktreeJSON(
		"wt_task_01", "workspace-demo", "batuta-demo-task-01-a1-deadbeef",
		"batuta/task/aaaaaaaaaaaa/task-01/a1-deadbeef", managed, "ready", "ok", worktreeTestSHA, worktreeTestSHA,
	)
	runner := &recordingRunner{results: []publication.CommandResult{
		{Stdout: []byte(`{"worktree":` + raw + `,"repo":{"git_backed":true,"git_available":true},"future":1}`)},
		{Stdout: []byte(`{"action":"removed","worktree":` + strings.Replace(raw, `"state":"ready"`, `"state":"removed"`, 1) + `}`)},
	}}
	client := CLIClient{Executable: "/controlled/compozy", Runner: runner}
	scope := publication.TrustedScope{WorkspaceID: "workspace-demo", WorkspaceRoot: repository}

	inspected, err := client.Inspect(context.Background(), scope, "wt_task_01")
	if err != nil || inspected.ID != "wt_task_01" || inspected.State != "ready" {
		t.Fatalf("Inspect() = %#v, error %v", inspected, err)
	}
	removed, err := client.Remove(context.Background(), scope, "wt_task_01")
	if err != nil || removed.ID != inspected.ID || removed.State != "removed" {
		t.Fatalf("Remove() = %#v, error %v", removed, err)
	}
	want := []publication.Command{
		{Executable: "/controlled/compozy", Directory: repository, Args: []string{"worktree", "inspect", "wt_task_01", "--workspace", "workspace-demo", "-o", "json"}, StdoutLimit: WorktreeStdoutLimit, StderrLimit: WorktreeStderrLimit},
		{Executable: "/controlled/compozy", Directory: repository, Args: []string{"worktree", "remove", "wt_task_01", "--workspace", "workspace-demo", "-o", "json"}, StdoutLimit: WorktreeStdoutLimit, StderrLimit: WorktreeStderrLimit},
	}
	if !reflect.DeepEqual(runner.commands, want) {
		t.Fatalf("commands = %#v, want %#v", runner.commands, want)
	}
}

func TestCLIClientRejectsAmbiguousOrUntrustedWorktreeOutput(t *testing.T) {
	t.Parallel()

	repository := filepath.Join(string(filepath.Separator), "trusted", "repository")
	managed := filepath.Join(string(filepath.Separator), "managed", "worktrees", "task-1")
	nonDirectory := filepath.Join(t.TempDir(), "not-a-worktree")
	if err := os.WriteFile(nonDirectory, []byte("not a directory"), 0o600); err != nil {
		t.Fatalf("WriteFile(non-directory) error = %v", err)
	}
	request := CreateRequest{Name: "batuta-demo-task-01-a1-deadbeef", Branch: "batuta/task/aaaaaaaaaaaa/task-01/a1-deadbeef", BaseSHA: worktreeTestSHA}
	valid := worktreeJSON("wt_task_01", "workspace-demo", request.Name, request.Branch, managed, "pending", "none", request.BaseSHA, "")
	tests := []struct {
		name   string
		stdout string
	}{
		{name: "duplicate key", stdout: strings.Replace(valid, `"id":"wt_task_01"`, `"id":"wt_task_01","id":"wt_foreign"`, 1)},
		{name: "trailing json", stdout: valid + `{}`},
		{name: "malformed id", stdout: strings.Replace(valid, "wt_task_01", "../foreign", 1)},
		{name: "wrong workspace", stdout: strings.Replace(valid, "workspace-demo", "workspace-foreign", 1)},
		{name: "relative path", stdout: strings.Replace(valid, managed, "relative/worktree", 1)},
		{name: "integration root", stdout: strings.Replace(valid, managed, repository, 1)},
		{name: "non-directory path", stdout: strings.Replace(valid, managed, nonDirectory, 1)},
		{name: "branch drift", stdout: strings.Replace(valid, request.Branch, "foreign/branch", 1)},
		{name: "base drift", stdout: strings.Replace(valid, request.BaseSHA, strings.Repeat("b", 40), 1)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			runner := &recordingRunner{result: publication.CommandResult{Stdout: []byte(test.stdout)}}
			client := CLIClient{Executable: "/controlled/compozy", Runner: runner}
			if _, err := client.Create(context.Background(), publication.TrustedScope{
				WorkspaceID: "workspace-demo", WorkspaceRoot: repository,
			}, request); err == nil {
				t.Fatal("Create() error = nil")
			}
		})
	}
}

func TestCLIClientRejectsTruncatedOutputAndHonorsTimeoutAndCancellation(t *testing.T) {
	t.Parallel()

	repository := filepath.Join(string(filepath.Separator), "trusted", "repository")
	scope := publication.TrustedScope{WorkspaceID: "workspace-demo", WorkspaceRoot: repository}
	request := CreateRequest{Name: "batuta-demo-task-01-a1-deadbeef", Branch: "batuta/task/aaaaaaaaaaaa/task-01/a1-deadbeef", BaseSHA: worktreeTestSHA}
	valid := worktreeJSON("wt_task_01", "workspace-demo", request.Name, request.Branch, filepath.Join(string(filepath.Separator), "managed", "task-1"), "pending", "none", request.BaseSHA, "")

	truncated := &recordingRunner{result: publication.CommandResult{Stdout: []byte(valid), StdoutTruncated: true}}
	if _, err := (CLIClient{Executable: "/controlled/compozy", Runner: truncated}).Create(context.Background(), scope, request); err == nil {
		t.Fatal("Create(truncated) error = nil")
	}

	blocking := &recordingRunner{run: func(ctx context.Context, _ publication.Command) (publication.CommandResult, error) {
		<-ctx.Done()
		return publication.CommandResult{}, ctx.Err()
	}}
	started := time.Now()
	_, err := (CLIClient{Executable: "/controlled/compozy", Runner: blocking, Timeout: 10 * time.Millisecond}).Create(context.Background(), scope, request)
	if !errors.Is(err, context.DeadlineExceeded) || time.Since(started) > time.Second {
		t.Fatalf("Create(timeout) error = %v, elapsed %s", err, time.Since(started))
	}

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = (CLIClient{Executable: "/controlled/compozy", Runner: blocking, Timeout: time.Minute}).Create(canceled, scope, request)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Create(canceled) error = %v, want context.Canceled", err)
	}
}

func TestSetupResultPreservesBoundedDaemonDiagnostic(t *testing.T) {
	t.Parallel()

	if !validSetup("failed", strings.Repeat("x", 1024)) {
		t.Fatal("validSetup() rejected a bounded daemon diagnostic")
	}
	if validSetup("failed", strings.Repeat("x", 16*1024+1)) {
		t.Fatal("validSetup() accepted an oversized daemon diagnostic")
	}
}

func worktreeJSON(id, workspaceID, name, branch, root, state, setupState, baseRef, createdHead string) string {
	payload := map[string]any{
		"id": id, "workspace_id": workspaceID, "name": name, "branch": branch,
		"path": root, "state": state, "setup_state": setupState, "base_ref": baseRef,
		"created_head": createdHead,
	}
	encoded, err := json.Marshal(payload)
	if err != nil {
		panic(fmt.Sprintf("encode worktree fixture: %v", err))
	}
	return string(encoded)
}

type recordingRunner struct {
	commands []publication.Command
	result   publication.CommandResult
	results  []publication.CommandResult
	err      error
	run      func(context.Context, publication.Command) (publication.CommandResult, error)
}

func (r *recordingRunner) Run(ctx context.Context, command publication.Command) (publication.CommandResult, error) {
	r.commands = append(r.commands, command)
	if r.run != nil {
		return r.run(ctx, command)
	}
	if len(r.results) > 0 {
		result := r.results[0]
		r.results = r.results[1:]
		return result, r.err
	}
	return r.result, r.err
}
