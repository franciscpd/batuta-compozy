package extensionapp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

func TestDeliveryClientUsesExactBoundedCommandsAndSecureConfig(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.August, 26, 15, 0, 0, 0, time.UTC)
	request := deliveryStartRequest{
		DeliveryID: digestValue("delivery-client"), Attempt: 2, Slug: "demo", OriginSessionID: "session_demo",
		WorktreeRef: "wt_demo", RoutingGeneration: digestValue("routing-client"), AbsoluteDeadline: now.Add(2 * time.Hour),
		TokenCeiling: 1_000_000, RecoveryOperationID: digestValue("operation-client"),
		IterationCap: 3, BudgetTokens: 750_000, BudgetWallSec: 7200,
	}
	run := deliveryRun{
		ID: "run_demo", WorkspaceID: "ws_demo", LoopName: "batuta-deliver", Status: "queued",
		CreatedAt: now, Inputs: deliveryInputs(request),
	}
	runner := &deliveryRecordingRunner{results: []publication.CommandResult{
		{Stdout: mustJSON(t, map[string]any{"run": run, "generations": []any{}})},
		{Stdout: mustJSON(t, map[string]any{"items": []map[string]any{{
			"run_id": run.ID, "workspace_id": run.WorkspaceID, "loop_name": run.LoopName,
			"status": run.Status, "created_at": run.CreatedAt, "tokens_used": run.TokensUsed,
			"inputs": run.Inputs,
		}}, "next_cursor": ""})},
		{Stdout: mustJSON(t, map[string]any{"run": run})},
	}}
	client := deliveryLoopCLIClient{Executable: "/controlled/compozy", Runner: runner}

	detail, err := client.Status(context.Background(), "ws_demo", "run_demo")
	if err != nil || detail.Run.ID != "run_demo" {
		t.Fatalf("Status() = %#v, error %v", detail, err)
	}
	recent, err := client.Recent(context.Background(), "ws_demo", 200)
	if err != nil || len(recent) != 1 || recent[0].ID != "run_demo" {
		t.Fatalf("Recent() = %#v, error %v", recent, err)
	}
	started, err := client.Start(context.Background(), "ws_demo", request)
	if err != nil || started.ID != "run_demo" {
		t.Fatalf("Start() = %#v, error %v", started, err)
	}

	if len(runner.commands) != 3 {
		t.Fatalf("commands = %d, want 3", len(runner.commands))
	}
	wantStatus := publication.Command{Executable: "/controlled/compozy", Args: []string{"loop", "status", "--workspace", "ws_demo", "--run-id", "run_demo", "-o", "json"}, StdoutLimit: 32 << 20, StderrLimit: 64 << 10}
	wantRecent := publication.Command{Executable: "/controlled/compozy", Args: []string{"loop", "runs", "--workspace", "ws_demo", "--loop", "batuta-deliver", "--limit", "200", "-o", "json"}, StdoutLimit: 2 << 20, StderrLimit: 64 << 10}
	if !reflect.DeepEqual(runner.commands[0], wantStatus) || !reflect.DeepEqual(runner.commands[1], wantRecent) {
		t.Fatalf("read commands = %#v, want %#v / %#v", runner.commands[:2], wantStatus, wantRecent)
	}
	wantStartPrefix := []string{
		"loop", "run", "--workspace", "ws_demo", "--name", "batuta-deliver", "--no-prompt",
		"--input", "delivery_id=" + request.DeliveryID,
		"--input", "attempt=2",
		"--input", "slug=demo",
		"--input", "origin_session_id=session_demo",
		"--input", "worktree_ref=wt_demo",
		"--input", "routing_generation=" + request.RoutingGeneration,
		"--input", "absolute_deadline=" + request.AbsoluteDeadline.Format(time.RFC3339),
		"--input", "token_ceiling=1000000",
		"--input", "recovery_operation_id=" + request.RecoveryOperationID,
		"--config-file",
	}
	start := runner.commands[2]
	if start.Executable != "/controlled/compozy" || start.StdoutLimit != 2<<20 || start.StderrLimit != 64<<10 || len(start.Args) != len(wantStartPrefix)+3 || !reflect.DeepEqual(start.Args[:len(wantStartPrefix)], wantStartPrefix) || start.Args[len(start.Args)-2:][1] != "json" {
		t.Fatalf("start command = %#v", start)
	}
	if start.Args[len(start.Args)-2] != "-o" {
		t.Fatalf("start suffix = %#v, want -o json", start.Args[len(start.Args)-2:])
	}
	if runner.configMode.Perm() != 0o600 {
		t.Fatalf("config mode = %o, want 600", runner.configMode.Perm())
	}
	var config map[string]any
	if err := json.Unmarshal(runner.config, &config); err != nil {
		t.Fatalf("config JSON error = %v", err)
	}
	wantConfig := map[string]any{
		"iteration_cap": float64(3), "budget_tokens": float64(750_000), "budget_wall_sec": float64(7200),
		"budget_on_exceeded": "halt", "reattempt_strategy": "halt",
	}
	if !reflect.DeepEqual(config, wantConfig) {
		t.Fatalf("config = %#v, want %#v", config, wantConfig)
	}
	if _, err := os.Stat(runner.configPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("temporary config still exists: %v", err)
	}
	for _, command := range runner.commands {
		joined := strings.Join(command.Args, " ")
		for _, forbidden := range []string{"recover-nested", "loop configure", "loop config"} {
			if strings.Contains(joined, forbidden) {
				t.Fatalf("command contains forbidden boundary %q: %s", forbidden, joined)
			}
		}
	}
}

func TestDeliveryClientDecodesCurrentLoopRunsProjection(t *testing.T) {
	t.Parallel()

	parent := `{"resolution_source":"flag","items":[{"run_id":"run_parent","workspace_id":"ws_demo","loop_name":"batuta-deliver","status":"running","created_at":"2026-08-28T12:00:00Z","started_at":"2026-08-28T12:00:01Z","tokens_used":10,"inputs":{}}],"next_cursor":""}`
	child := `{"resolution_source":"flag","items":[{"run_id":"run_child","workspace_id":"ws_demo","parent_loop_run_id":"run_parent","loop_name":"batuta-task","status":"running","created_at":"2026-08-28T12:00:02Z","started_at":"2026-08-28T12:00:03Z","tokens_used":20,"inputs":{}}],"next_cursor":""}`
	runner := &deliveryRecordingRunner{results: []publication.CommandResult{
		{Stdout: []byte(parent)},
		{Stdout: []byte(child)},
	}}
	client := deliveryLoopCLIClient{Executable: "/controlled/compozy", Runner: runner}

	parents, err := client.Recent(context.Background(), "ws_demo", 200)
	if err != nil || len(parents) != 1 || parents[0].ID != "run_parent" {
		t.Fatalf("Recent(current CLI projection) = %#v, error=%v", parents, err)
	}
	children, err := client.RecentTasks(context.Background(), "ws_demo", 200)
	if err != nil || len(children) != 1 || children[0].ID != "run_child" || children[0].ParentLoopRunID != "run_parent" {
		t.Fatalf("RecentTasks(current CLI projection) = %#v, error=%v", children, err)
	}
}

func TestDeliveryClientSeparatesParentGenerationCapFromFreshRunCeiling(t *testing.T) {
	t.Parallel()

	request := validDeliveryStartRequest()
	request.IterationCap = 64
	run := deliveryRun{
		ID: "run_parent_64", WorkspaceID: "ws_demo", LoopName: "batuta-deliver", Status: "queued",
		CreatedAt: time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC), Inputs: deliveryInputs(request),
	}
	runner := &deliveryRecordingRunner{results: []publication.CommandResult{{Stdout: mustJSON(t, map[string]any{"run": run})}}}
	client := deliveryLoopCLIClient{Executable: "/controlled/compozy", Runner: runner}

	if _, err := client.Start(context.Background(), "ws_demo", request); err != nil {
		t.Fatalf("Start(parent generation cap 64) error = %v", err)
	}
	var config map[string]any
	if err := json.Unmarshal(runner.config, &config); err != nil {
		t.Fatalf("decode parent config: %v", err)
	}
	if got := config["iteration_cap"]; got != float64(64) {
		t.Fatalf("parent iteration_cap = %#v, want 64 generations", got)
	}
}

func TestDeliveryClientRejectsNumericSlugBeforeStartingLoop(t *testing.T) {
	t.Parallel()

	request := validDeliveryStartRequest()
	request.Slug = "123"
	runner := &deliveryRecordingRunner{}
	client := deliveryLoopCLIClient{Executable: "/controlled/compozy", Runner: runner}

	if _, err := client.Start(context.Background(), "ws_demo", request); err == nil {
		t.Fatal("Start(numeric slug) error = nil")
	}
	if len(runner.commands) != 0 {
		t.Fatalf("Start(numeric slug) commands = %#v, want none", runner.commands)
	}
}

func TestDeliveryClientRejectsUnsafeInputsAndMalformedResponses(t *testing.T) {
	t.Parallel()

	valid := validDeliveryStartRequest()
	tests := []struct {
		name   string
		client deliveryLoopCLIClient
		call   func(deliveryLoopCLIClient) error
	}{
		{name: "relative executable", client: deliveryLoopCLIClient{Executable: "compozy", Runner: &deliveryRecordingRunner{}}, call: func(client deliveryLoopCLIClient) error {
			_, err := client.Start(context.Background(), "ws_demo", valid)
			return err
		}},
		{name: "unsafe workspace", client: deliveryLoopCLIClient{Executable: "/controlled/compozy", Runner: &deliveryRecordingRunner{}}, call: func(client deliveryLoopCLIClient) error {
			_, err := client.Recent(context.Background(), "../foreign", 200)
			return err
		}},
		{name: "wrong list limit", client: deliveryLoopCLIClient{Executable: "/controlled/compozy", Runner: &deliveryRecordingRunner{}}, call: func(client deliveryLoopCLIClient) error {
			_, err := client.Recent(context.Background(), "ws_demo", 199)
			return err
		}},
		{name: "duplicate JSON key", client: deliveryLoopCLIClient{Executable: "/controlled/compozy", Runner: &deliveryRecordingRunner{results: []publication.CommandResult{{Stdout: []byte(`{"runs":[],"runs":[]}`)}}}}, call: func(client deliveryLoopCLIClient) error {
			_, err := client.Recent(context.Background(), "ws_demo", 200)
			return err
		}},
		{name: "trailing JSON", client: deliveryLoopCLIClient{Executable: "/controlled/compozy", Runner: &deliveryRecordingRunner{results: []publication.CommandResult{{Stdout: []byte(`{"runs":[]} {}`)}}}}, call: func(client deliveryLoopCLIClient) error {
			_, err := client.Recent(context.Background(), "ws_demo", 200)
			return err
		}},
		{name: "oversized marker", client: deliveryLoopCLIClient{Executable: "/controlled/compozy", Runner: &deliveryRecordingRunner{results: []publication.CommandResult{{StdoutTruncated: true}}}}, call: func(client deliveryLoopCLIClient) error {
			_, err := client.Recent(context.Background(), "ws_demo", 200)
			return err
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if err := tt.call(tt.client); err == nil {
				t.Fatal("error = nil, want closed-boundary rejection")
			}
		})
	}
}

func TestDeliveryClientDoesNotAssumeStartSuccessOnCommandError(t *testing.T) {
	t.Parallel()

	request := validDeliveryStartRequest()
	run := deliveryRun{ID: "run_demo", WorkspaceID: "ws_demo", LoopName: "batuta-deliver", Status: "queued", CreatedAt: time.Now().UTC(), Inputs: deliveryInputs(request)}
	runner := &deliveryRecordingRunner{results: []publication.CommandResult{{Stdout: mustJSON(t, map[string]any{"run": run}), ExitCode: 1}}, errors: []error{errors.New("exit 1")}}
	client := deliveryLoopCLIClient{Executable: "/controlled/compozy", Runner: runner}
	if _, err := client.Start(context.Background(), "ws_demo", request); err == nil {
		t.Fatal("Start(nonzero with valid response) error = nil")
	}
}

func TestDeliveryClientReturnsCallerCancellationAndCommandDeadline(t *testing.T) {
	t.Parallel()

	canceled, cancel := context.WithCancel(context.Background())
	cancel()
	runner := &deliveryRecordingRunner{}
	client := deliveryLoopCLIClient{Executable: "/controlled/compozy", Runner: runner}
	if _, err := client.Recent(canceled, "ws_demo", 200); !errors.Is(err, context.Canceled) {
		t.Fatalf("Recent(canceled) error = %v, want context.Canceled", err)
	}
	if len(runner.commands) != 0 {
		t.Fatalf("canceled commands = %#v, want none", runner.commands)
	}

	deadlineRunner := blockingDeliveryRunner{}
	deadlineClient := deliveryLoopCLIClient{Executable: "/controlled/compozy", Runner: deadlineRunner}
	deadlineCtx, deadlineCancel := context.WithTimeout(context.Background(), time.Millisecond)
	defer deadlineCancel()
	if _, err := deadlineClient.Recent(deadlineCtx, "ws_demo", 200); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Recent(deadline) error = %v, want context.DeadlineExceeded", err)
	}
}

func validDeliveryStartRequest() deliveryStartRequest {
	now := time.Date(2026, time.August, 26, 15, 0, 0, 0, time.UTC)
	return deliveryStartRequest{
		DeliveryID: digestValue("delivery-valid"), Attempt: 1, Slug: "demo", OriginSessionID: "session_demo",
		WorktreeRef: "wt_demo", RoutingGeneration: digestValue("routing-valid"), AbsoluteDeadline: now.Add(time.Hour),
		TokenCeiling: 1_000_000, IterationCap: 4,
		BudgetTokens: 1_000_000, BudgetWallSec: 3600,
	}
}

func deliveryInputs(request deliveryStartRequest) map[string]any {
	return map[string]any{
		"delivery_id": request.DeliveryID, "attempt": request.Attempt, "slug": request.Slug,
		"origin_session_id": request.OriginSessionID, "worktree_ref": request.WorktreeRef,
		"routing_generation": request.RoutingGeneration, "absolute_deadline": request.AbsoluteDeadline.Format(time.RFC3339),
		"token_ceiling": request.TokenCeiling, "recovery_operation_id": request.RecoveryOperationID,
	}
}

func mustJSON(t *testing.T, value any) []byte {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return payload
}

func digestValue(seed string) string {
	digest := sha256.Sum256([]byte(seed))
	return fmt.Sprintf("sha256:%x", digest)
}

type deliveryRecordingRunner struct {
	commands   []publication.Command
	results    []publication.CommandResult
	errors     []error
	configPath string
	config     []byte
	configMode os.FileMode
}

type blockingDeliveryRunner struct{}

func (blockingDeliveryRunner) Run(ctx context.Context, _ publication.Command) (publication.CommandResult, error) {
	<-ctx.Done()
	return publication.CommandResult{}, ctx.Err()
}

func (r *deliveryRecordingRunner) Run(_ context.Context, command publication.Command) (publication.CommandResult, error) {
	r.commands = append(r.commands, command)
	for index, arg := range command.Args {
		if arg != "--config-file" || index+1 >= len(command.Args) {
			continue
		}
		r.configPath = command.Args[index+1]
		info, err := os.Stat(r.configPath)
		if err == nil {
			r.configMode = info.Mode()
			r.config, _ = os.ReadFile(r.configPath)
		}
	}
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
