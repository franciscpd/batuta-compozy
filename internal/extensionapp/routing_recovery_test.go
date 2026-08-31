package extensionapp

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/franciscpd/batuta-compozy/internal/publication"
	"github.com/franciscpd/batuta-compozy/internal/routing"
)

func TestDeliveryAttemptServiceStartsOnceAndReplaysSubmittedRun(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	first, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	second, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start(replay) error = %v", err)
	}
	if first.DeliveryRunID != "run_attempt_1" || second.DeliveryRunID != first.DeliveryRunID || first.OperationID != second.OperationID || fixture.client.startCalls != 1 || fixture.client.recentCalls != 1 {
		t.Fatalf("starts = %#v / %#v, client=%#v", first, second, fixture.client)
	}
	if fixture.client.lastRequest.IterationCap != 64 {
		t.Fatalf("parent iteration cap = %d, want 64 graph generations while fresh starts remain journal-capped", fixture.client.lastRequest.IterationCap)
	}
	journal, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("Load() exists=%v error=%v", exists, err)
	}
	delivery := journal.Deliveries[fixture.deliveryID]
	if len(delivery.Attempts) != 1 || delivery.Attempts[0].State != routing.AttemptSubmitted || delivery.Attempts[0].RunID != "run_attempt_1" {
		t.Fatalf("attempts = %#v", delivery.Attempts)
	}
}

func TestDeliveryParentIterationCapKeepsLegacyAtFour(t *testing.T) {
	t.Parallel()
	if got := deliveryParentIterationCap(nil); got != 4 {
		t.Fatalf("legacy iteration cap = %d, want 4", got)
	}
	if got := deliveryParentIterationCap(&routing.DeliveryGraph{}); got != 64 {
		t.Fatalf("graph iteration cap = %d, want 64", got)
	}
}

func TestDeliveryAttemptServiceRejectsPrematureGraphParentCompletionWithoutReadingTaskChildren(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	enableGraphDelivery(t, fixture)
	started, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	fixture.client.statuses = map[string]deliveryRunDetail{
		started.DeliveryRunID: testParentRunDetail(
			fixture.scope.WorkspaceID, started.DeliveryRunID, "done", fixture.client.lastRequest,
			[]deliveryOutput{{NodeID: "run_task", ItemIndex: 0, Status: "succeeded", ChildLoopRunID: "run_batuta_task"}},
		),
		"run_batuta_task": testChildRunDetail(
			fixture.scope.WorkspaceID, "run_batuta_task", "batuta-task", "done", 999,
			nil,
		),
	}
	if _, err := fixture.service.Reconcile(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID); !errors.Is(err, routing.ErrDeliveryConflict) {
		t.Fatalf("Reconcile(premature graph parent) error = %v, want delivery conflict", err)
	}
	if fixture.client.statusCalls != 1 {
		t.Fatalf("graph reconciliation status calls=%d, want parent only", fixture.client.statusCalls)
	}
	journal, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !exists || journal.Deliveries[fixture.deliveryID].Graph == nil || journal.Deliveries[fixture.deliveryID].Attempts[0].State != routing.AttemptSubmitted {
		t.Fatalf("graph reconciliation mutated graph child evidence: journal=%#v exists=%v error=%v", journal.Deliveries[fixture.deliveryID], exists, err)
	}
}

func TestDeliveryAttemptServiceRecoversPersistedGraphFallbackWithoutParentRunTaskMarker(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	started, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	prepareGraphTaskForTest(t, fixture, taskRoot)
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		generation := tx.Journal.Generations[delivery.RoutingGenerationDigest]
		_, err := delivery.Graph.RecordFailure("task_01", 1, routing.TaskFailure{
			ChildRunID: "run_batuta_task", TerminalStatus: "failed", BlockerCode: "task_terminal_failed", TokensUsed: 125,
		}, generation, delivery.InitialWorktreeFingerprint.HeadSHA, true)
		if err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist graph fallback: %v", err)
	}
	fixture.client.statuses = map[string]deliveryRunDetail{
		started.DeliveryRunID: testParentRunDetail(fixture.scope.WorkspaceID, started.DeliveryRunID, "failed", fixture.client.lastRequest, nil),
	}
	settled, err := fixture.service.Reconcile(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID)
	if err != nil || !settled.Recoverable || settled.State != "active" || settled.TokensUsed != 125 || fixture.client.statusCalls != 1 {
		t.Fatalf("graph fallback reconciliation=%#v error=%v status_calls=%d", settled, err, fixture.client.statusCalls)
	}
	recovered, err := fixture.service.Recover(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID)
	if err != nil || recovered.Attempt != 2 || fixture.client.startCalls != 2 {
		t.Fatalf("Recover(graph fallback)=%#v error=%v starts=%d", recovered, err, fixture.client.startCalls)
	}
}

func TestDeliveryAttemptServiceAttributesGraphDeltaAndReviewUsageOnce(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	delivery, _, _ := func() (routing.DeliveryRecord, bool, error) {
		journal, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
		return journal.Deliveries[fixture.deliveryID], exists, err
	}()
	used := int64(125)
	delivery.Graph.Tasks[0].Attempts = []routing.GraphTaskAttempt{{TokensUsed: &used}}
	delivery.Attempts = []routing.DeliveryAttempt{{State: routing.AttemptTerminal, GraphTokensUsed: 125}}
	fixture.client.statuses = map[string]deliveryRunDetail{
		"run_review": func() deliveryRunDetail {
			child := testChildRunDetail(fixture.scope.WorkspaceID, "run_review", "review-and-fix", "failed", 55, nil)
			child.Run.TokensUsedPresent = true
			child.Run.ParentLoopRunID = "run_parent"
			return child
		}(),
	}
	graphTokens, reviewTokens, childIDs, err := fixture.service.graphParentUsage(context.Background(), fixture.scope.WorkspaceID, delivery, deliveryRunDetail{
		Run:         deliveryRun{ID: "run_parent"},
		Generations: []deliveryGeneration{{Outputs: []deliveryOutput{{NodeID: "review", Status: "failed", ChildLoopRunID: "run_review"}}}},
	})
	if err != nil || graphTokens != 0 || reviewTokens != 55 || !reflect.DeepEqual(childIDs, []string{"run_review"}) || fixture.client.statusCalls != 1 {
		t.Fatalf("graph parent usage graph=%d review=%d children=%#v error=%v calls=%d", graphTokens, reviewTokens, childIDs, err, fixture.client.statusCalls)
	}
}

func TestDeliveryAttemptServiceRejectsUnownedOrAmbiguousReviewUsage(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	base := func(id, parent string, present bool) deliveryRunDetail {
		child := testChildRunDetail(fixture.scope.WorkspaceID, id, "review-and-fix", "exhausted", 5, nil)
		child.Run.ParentLoopRunID, child.Run.TokensUsedPresent = parent, present
		return child
	}
	for _, test := range []struct {
		name     string
		outputs  []deliveryOutput
		statuses map[string]deliveryRunDetail
	}{
		{"missing_tokens", []deliveryOutput{{NodeID: "review", Status: "failed", ChildLoopRunID: "run_review"}}, map[string]deliveryRunDetail{"run_review": base("run_review", "run_parent", false)}},
		{"foreign_parent", []deliveryOutput{{NodeID: "review", Status: "failed", ChildLoopRunID: "run_review"}}, map[string]deliveryRunDetail{"run_review": base("run_review", "run_foreign", true)}},
		{"two_reviews", []deliveryOutput{{NodeID: "review", Status: "failed", ChildLoopRunID: "run_a"}, {NodeID: "review", Status: "failed", ChildLoopRunID: "run_b"}}, map[string]deliveryRunDetail{"run_a": base("run_a", "run_parent", true), "run_b": base("run_b", "run_parent", true)}},
	} {
		t.Run(test.name, func(t *testing.T) {
			fixture.client.statuses = test.statuses
			if _, _, _, err := fixture.service.graphParentUsage(context.Background(), fixture.scope.WorkspaceID, delivery, deliveryRunDetail{Run: deliveryRun{ID: "run_parent"}, Generations: []deliveryGeneration{{Outputs: test.outputs}}}); !errors.Is(err, routing.ErrDeliveryConflict) {
				t.Fatalf("graphParentUsage() error = %v, want conflict", err)
			}
		})
	}
}

func TestDeliveryAttemptServicePreservesTerminalizedGraphExhaustion(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	started, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		delivery.State = routing.DeliveryStateExhausted
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist terminalized exhaustion: %v", err)
	}
	fixture.client.statuses = map[string]deliveryRunDetail{
		started.DeliveryRunID: testParentRunDetail(fixture.scope.WorkspaceID, started.DeliveryRunID, "failed", fixture.client.lastRequest, nil),
	}
	settled, err := fixture.service.Reconcile(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID)
	if err != nil || settled.State != string(routing.DeliveryStateExhausted) || settled.Recoverable {
		t.Fatalf("Reconcile(terminalized exhaustion)=%#v error=%v", settled, err)
	}
}

func TestDeliveryAttemptServiceAdoptsOneMatchingRecentRunWithoutStarting(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	fixture.client.startError = errors.New("simulated lost start response")
	if _, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID); err == nil {
		t.Fatal("Start(lost response) error = nil")
	}
	fixture.client.startError = nil
	fixture.client.recentFactory = func(request deliveryStartRequest) []deliveryRun {
		return []deliveryRun{{ID: "run_adopted", WorkspaceID: fixture.scope.WorkspaceID, LoopName: "batuta-deliver", Status: "queued", CreatedAt: fixture.now, Inputs: deliveryInputs(request)}}
	}
	result, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start(adopt) error = %v", err)
	}
	if result.DeliveryRunID != "run_adopted" || fixture.client.startCalls != 1 || fixture.client.recentCalls != 2 {
		t.Fatalf("result = %#v, client=%#v", result, fixture.client)
	}
}

func TestDeliveryAttemptServiceBlocksAmbiguousRecentRuns(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	fixture.client.startError = errors.New("simulated lost start response")
	if _, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID); err == nil {
		t.Fatal("Start(lost response) error = nil")
	}
	fixture.client.startError = nil
	fixture.client.recentFactory = func(request deliveryStartRequest) []deliveryRun {
		base := deliveryRun{WorkspaceID: fixture.scope.WorkspaceID, LoopName: "batuta-deliver", Status: "queued", CreatedAt: fixture.now, Inputs: deliveryInputs(request)}
		first, second := base, base
		first.ID, second.ID = "run_first", "run_second"
		return []deliveryRun{first, second}
	}
	if _, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID); !errors.Is(err, errAmbiguousDeliveryStart) {
		t.Fatalf("Start(ambiguous) error = %v, want errAmbiguousDeliveryStart", err)
	}
	if fixture.client.startCalls != 1 {
		t.Fatalf("start calls = %d, want exactly the ambiguous prior call", fixture.client.startCalls)
	}
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	if journal.Deliveries[fixture.deliveryID].State != routing.DeliveryStateBlocked {
		t.Fatalf("delivery state = %q, want blocked", journal.Deliveries[fixture.deliveryID].State)
	}
}

func TestDeliveryAttemptServiceSettlesFailedTaskAndStartsExactFallback(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	disableGraphDelivery(t, fixture)
	started, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	request := fixture.client.lastRequest
	fixture.client.statuses = map[string]deliveryRunDetail{
		started.DeliveryRunID: {
			Run:         deliveryRun{ID: started.DeliveryRunID, WorkspaceID: fixture.scope.WorkspaceID, LoopName: "batuta-deliver", Status: "failed", CreatedAt: fixture.now, StartedAt: fixture.now, Inputs: deliveryInputs(request)},
			Generations: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{{NodeID: "implement", Status: "failed", ChildLoopRunID: "run_implement"}}}},
		},
		"run_implement": {
			Run:         deliveryRun{ID: "run_implement", WorkspaceID: fixture.scope.WorkspaceID, LoopName: "implement-tasks", Status: "failed", CreatedAt: fixture.now, StartedAt: fixture.now, TokensUsed: 1250, Inputs: map[string]any{"slug": "demo"}},
			Generations: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{{NodeID: "execute_task", ItemIndex: 0, Status: "failed"}}}},
		},
	}
	reconciled, err := fixture.service.Reconcile(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID)
	if err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	if !reconciled.Recoverable || reconciled.State != "active" || reconciled.TokensUsed != 1250 {
		t.Fatalf("reconciliation = %#v", reconciled)
	}
	recovered, err := fixture.service.Recover(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	if recovered.Attempt != 2 || recovered.DeliveryRunID != "run_attempt_2" || fixture.client.lastRequest.RecoveryOperationID != recovered.OperationID {
		t.Fatalf("recovery = %#v, request=%#v", recovered, fixture.client.lastRequest)
	}
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	attempts := journal.Deliveries[fixture.deliveryID].Attempts
	if len(attempts) != 2 || len(attempts[0].FailedTaskIDs) != 1 || attempts[0].FailedTaskIDs[0] != "task_01" || attempts[0].TokensUsed != 1250 || reflect.DeepEqual(attempts[0].RuntimeRules, attempts[1].RuntimeRules) {
		t.Fatalf("attempts = %#v", attempts)
	}
}

func TestDeliveryAttemptServiceReplaysSubmittedRecovery(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	disableGraphDelivery(t, fixture)
	started, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	request := fixture.client.lastRequest
	fixture.client.statuses = map[string]deliveryRunDetail{
		started.DeliveryRunID: {
			Run: deliveryRun{
				ID: started.DeliveryRunID, WorkspaceID: fixture.scope.WorkspaceID,
				LoopName: "batuta-deliver", Status: "failed", CreatedAt: fixture.now,
				StartedAt: fixture.now, Inputs: deliveryInputs(request),
			},
			Generations: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{{
				NodeID: "implement", Status: "failed", ChildLoopRunID: "run_implement",
			}}}},
		},
		"run_implement": {
			Run: deliveryRun{
				ID: "run_implement", WorkspaceID: fixture.scope.WorkspaceID,
				LoopName: "implement-tasks", Status: "failed", CreatedAt: fixture.now,
				StartedAt: fixture.now, TokensUsed: 1250, Inputs: map[string]any{"slug": "demo"},
			},
			Generations: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{{
				NodeID: "execute_task", ItemIndex: 0, Status: "failed",
			}}}},
		},
	}
	if _, err := fixture.service.Reconcile(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID); err != nil {
		t.Fatalf("Reconcile() error = %v", err)
	}
	first, err := fixture.service.Recover(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID)
	if err != nil {
		t.Fatalf("Recover() error = %v", err)
	}
	second, err := fixture.service.Recover(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID)
	if err != nil {
		t.Fatalf("Recover(replay) error = %v", err)
	}
	if second.DeliveryRunID != first.DeliveryRunID || second.OperationID != first.OperationID || !second.Replayed || fixture.client.startCalls != 2 {
		t.Fatalf("recoveries = %#v / %#v, start calls = %d", first, second, fixture.client.startCalls)
	}
}

func TestDeliveryAttemptServiceStopBoundariesDoNotSubmit(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name              string
		prepare           func(*deliveryServiceFixture) context.Context
		allowedTransition routing.DeliveryState
	}{
		{
			name: "canceled context",
			prepare: func(*deliveryServiceFixture) context.Context {
				ctx, cancel := context.WithCancel(context.Background())
				cancel()
				return ctx
			},
		},
		{
			name: "worktree drift",
			prepare: func(fixture *deliveryServiceFixture) context.Context {
				fixture.service.WorktreeState = func(context.Context, string) (publication.WorktreeState, error) {
					return publication.WorktreeState{
						HeadSHA:         "1123456789abcdef0123456789abcdef01234567",
						PorcelainSHA256: digestValue("porcelain"), ContentSHA256: digestValue("content"),
					}, nil
				}
				return context.Background()
			},
		},
		{
			name: "task drift",
			prepare: func(fixture *deliveryServiceFixture) context.Context {
				path := filepath.Join(fixture.scope.WorkspaceRoot, ".compozy", "tasks", "demo", "task_01.md")
				payload := "---\nstatus: pending\ntitle: Frontend demo\ntype: frontend\ncomplexity: low\n---\n\nchanged while pending\n"
				if err := os.WriteFile(path, []byte(payload), 0o600); err != nil {
					t.Fatalf("write task drift: %v", err)
				}
				return context.Background()
			},
		},
		{
			name: "absolute deadline",
			prepare: func(fixture *deliveryServiceFixture) context.Context {
				journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
				deadline := journal.Deliveries[fixture.deliveryID].AbsoluteDeadline
				fixture.service.Now = func() time.Time { return deadline }
				return context.Background()
			},
			allowedTransition: routing.DeliveryStateExhausted,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newDeliveryServiceFixture(t)
			disableGraphDelivery(t, fixture)
			before, _, err := fixture.store.Load(fixture.scope.WorkspaceID)
			if err != nil {
				t.Fatalf("Load(before) error = %v", err)
			}
			ctx := test.prepare(&fixture)
			if _, err := fixture.service.Start(ctx, fixture.scope, fixture.deliveryID); err == nil {
				t.Fatal("Start(stop boundary) error = nil")
			}
			after, _, err := fixture.store.Load(fixture.scope.WorkspaceID)
			if err != nil {
				t.Fatalf("Load(after) error = %v", err)
			}
			if fixture.client.startCalls != 0 {
				t.Fatalf("start calls = %d, want zero", fixture.client.startCalls)
			}
			if test.allowedTransition == "" {
				if !reflect.DeepEqual(after, before) {
					t.Fatalf("journal changed across stop boundary:\nbefore=%#v\nafter=%#v", before, after)
				}
				return
			}
			delivery := after.Deliveries[fixture.deliveryID]
			if delivery.State != test.allowedTransition || len(delivery.Attempts) != 0 {
				t.Fatalf("allowed stop transition = %#v", delivery)
			}
		})
	}
}

func TestDeliveryAttemptServiceTerminalStopBoundariesBlockRecovery(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name      string
		status    string
		outputs   []deliveryOutput
		child     *deliveryRunDetail
		wantState string
		wantBlock string
	}{
		{name: "blocked", status: "blocked", wantState: "blocked", wantBlock: "terminal_not_recoverable"},
		{name: "exhausted", status: "exhausted", wantState: "exhausted", wantBlock: "compozy_exhausted"},
		{name: "stalled", status: "stalled", wantState: "blocked", wantBlock: "terminal_not_recoverable"},
		{name: "canceled", status: "canceled", wantState: "blocked", wantBlock: "terminal_not_recoverable"},
		{
			name: "review failure", status: "failed", wantState: "blocked", wantBlock: "non_recoverable_failure",
			outputs: []deliveryOutput{{NodeID: "review", Status: "failed", ChildLoopRunID: "run_review"}},
			child: func() *deliveryRunDetail {
				value := testChildRunDetail("ws_demo", "run_review", "review-and-fix", "failed", 50, nil)
				return &value
			}(),
		},
		{
			name: "publication started", status: "failed", wantState: "blocked", wantBlock: "non_recoverable_failure",
			outputs: []deliveryOutput{{NodeID: "publish", Status: "failed"}},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			t.Parallel()
			fixture := newDeliveryServiceFixture(t)
			disableGraphDelivery(t, fixture)
			started, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			request := fixture.client.lastRequest
			fixture.client.statuses = map[string]deliveryRunDetail{
				started.DeliveryRunID: testParentRunDetail(fixture.scope.WorkspaceID, started.DeliveryRunID, test.status, request, test.outputs),
			}
			if test.child != nil {
				child := *test.child
				child.Run.WorkspaceID = fixture.scope.WorkspaceID
				fixture.client.statuses[child.Run.ID] = child
			}
			settled, err := fixture.service.Reconcile(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID)
			if err != nil {
				t.Fatalf("Reconcile() error = %v", err)
			}
			if settled.Recoverable || settled.State != test.wantState || settled.BlockerCode != test.wantBlock {
				t.Fatalf("settlement = %#v", settled)
			}
			before, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
			if _, err := fixture.service.Recover(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID); err == nil {
				t.Fatal("Recover(terminal stop) error = nil")
			}
			after, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
			if fixture.client.startCalls != 1 || !reflect.DeepEqual(after, before) {
				t.Fatalf("recovery mutated terminal stop: starts=%d before=%#v after=%#v", fixture.client.startCalls, before, after)
			}
		})
	}
}

func TestDeliveryAttemptServiceRejectsForeignRunAndExhaustedFallbackWithoutMutation(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	disableGraphDelivery(t, fixture)
	first, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	settleFixtureAttemptFailure(t, &fixture, first, "run_implement_1", 100)

	beforeForeign, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	if _, err := fixture.service.Recover(context.Background(), fixture.scope, fixture.deliveryID, "foreign_run"); err == nil {
		t.Fatal("Recover(foreign run) error = nil")
	}
	afterForeign, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	if !reflect.DeepEqual(afterForeign, beforeForeign) || fixture.client.startCalls != 1 {
		t.Fatalf("foreign recovery mutated state: starts=%d before=%#v after=%#v", fixture.client.startCalls, beforeForeign, afterForeign)
	}

	second, err := fixture.service.Recover(context.Background(), fixture.scope, fixture.deliveryID, first.DeliveryRunID)
	if err != nil {
		t.Fatalf("Recover(attempt 2) error = %v", err)
	}
	settleFixtureAttemptFailure(t, &fixture, second, "run_implement_2", 100)
	beforeExhausted, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	if _, err := fixture.service.Recover(context.Background(), fixture.scope, fixture.deliveryID, second.DeliveryRunID); !errors.Is(err, routing.ErrNoEligibleCandidate) {
		t.Fatalf("Recover(exhausted fallback) error = %v", err)
	}
	afterExhausted, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	if !reflect.DeepEqual(afterExhausted, beforeExhausted) || fixture.client.startCalls != 2 {
		t.Fatalf("exhausted fallback mutated state: starts=%d before=%#v after=%#v", fixture.client.startCalls, beforeExhausted, afterExhausted)
	}
}

func TestDeliveryAttemptServiceTokenCeilingStopsBeforeAnotherSubmission(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	disableGraphDelivery(t, fixture)
	first, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	settleFixtureAttemptFailure(t, &fixture, first, "run_implement", 1_000_000)
	if _, err := fixture.service.Recover(context.Background(), fixture.scope, fixture.deliveryID, first.DeliveryRunID); !errors.Is(err, routing.ErrNoEligibleCandidate) {
		t.Fatalf("Recover(token ceiling) error = %v", err)
	}
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	if delivery.State != routing.DeliveryStateExhausted || len(delivery.Attempts) != 1 || fixture.client.startCalls != 1 {
		t.Fatalf("token ceiling state = %#v, start calls=%d", delivery, fixture.client.startCalls)
	}
}

func settleFixtureAttemptFailure(
	t *testing.T,
	fixture *deliveryServiceFixture,
	started RoutingStartResult,
	childID string,
	tokens int64,
) RoutingReconcileResult {
	t.Helper()
	request := fixture.client.lastRequest
	fixture.client.statuses = map[string]deliveryRunDetail{
		started.DeliveryRunID: testParentRunDetail(
			fixture.scope.WorkspaceID, started.DeliveryRunID, "failed", request,
			[]deliveryOutput{{NodeID: "implement", Status: "failed", ChildLoopRunID: childID}},
		),
		childID: testChildRunDetail(
			fixture.scope.WorkspaceID, childID, "implement-tasks", "failed", tokens,
			[]deliveryOutput{{NodeID: "execute_task", ItemIndex: 0, Status: "failed"}},
		),
	}
	result, err := fixture.service.Reconcile(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID)
	if err != nil {
		t.Fatalf("Reconcile(%s) error = %v", started.DeliveryRunID, err)
	}
	if !result.Recoverable {
		t.Fatalf("Reconcile(%s) = %#v, want recoverable", started.DeliveryRunID, result)
	}
	return result
}

type deliveryServiceFixture struct {
	service    deliveryAttemptService
	client     *fakeDeliveryRunClient
	store      *routing.OwnershipStore
	scope      publication.TrustedScope
	deliveryID string
	now        time.Time
}

func newDeliveryServiceFixture(t *testing.T) deliveryServiceFixture {
	t.Helper()
	root := t.TempDir()
	writeRoutingTask(t, root)
	scope := publication.TrustedScope{WorkspaceID: "ws_demo", WorkspaceRoot: root}
	engine := routingEngine{inventory: func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error) {
		return routingInventory(t, nil), nil
	}}
	generation, err := engine.Plan(context.Background(), scope, routingPlanFixture())
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	loader, _ := routing.NewArtifactLoader(root)
	taskSet, _ := loader.Load("demo")
	taskSnapshot, _ := taskSet.DeliverySnapshot()
	store, _ := routing.NewOwnershipStore(t.TempDir())
	if err := store.ArchiveGeneration(scope.WorkspaceID, generation); err != nil {
		t.Fatalf("ArchiveGeneration() error = %v", err)
	}
	if _, err := (routing.AlignmentManager{Store: store}).Confirm(scope.WorkspaceID, "session_demo", generation); err != nil {
		t.Fatalf("Alignment.Confirm() error = %v", err)
	}
	matrix, err := (routing.MatrixManager{Store: store}).Apply(context.Background(), routing.MatrixApplyInput{
		WorkspaceID: scope.WorkspaceID, WorkspaceRoot: root, WorktreeID: "wt_demo", WorktreeRoot: root,
		Slug: "demo", OriginSessionID: "session_demo", TaskSetDigest: taskSet.Digest, TaskSnapshot: taskSnapshot,
		InitialWorktreeFingerprint: routing.WorktreeFingerprint{HeadSHA: "0123456789abcdef0123456789abcdef01234567", PorcelainSHA256: digestValue("porcelain"), ContentSHA256: digestValue("content")},
		Generation:                 generation,
	})
	if err != nil {
		t.Fatalf("Matrix.Apply() error = %v", err)
	}
	now := matrix.CreatedAt.Add(time.Minute)
	client := &fakeDeliveryRunClient{now: now}
	service := deliveryAttemptService{
		Store: store, Client: client, Now: func() time.Time { return now },
		WorktreeState: func(context.Context, string) (publication.WorktreeState, error) {
			return publication.WorktreeState{HeadSHA: "0123456789abcdef0123456789abcdef01234567", PorcelainSHA256: digestValue("porcelain"), ContentSHA256: digestValue("content")}, nil
		},
	}
	return deliveryServiceFixture{service: service, client: client, store: store, scope: scope, deliveryID: matrix.DeliveryID, now: now}
}

func enableGraphDelivery(t *testing.T, fixture deliveryServiceFixture) {
	t.Helper()
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		generation := tx.Journal.Generations[delivery.RoutingGenerationDigest]
		graph, err := routing.NewDeliveryGraph(delivery.TaskSnapshot, generation, delivery.InitialWorktreeFingerprint.HeadSHA)
		if err != nil {
			return err
		}
		delivery.Graph = graph
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("enable graph delivery: %v", err)
	}
}

func disableGraphDelivery(t *testing.T, fixture deliveryServiceFixture) {
	t.Helper()
	journal, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("Load(legacy delivery) exists=%v error=%v", exists, err)
	}
	delivery := journal.Deliveries[fixture.deliveryID]
	delivery.Graph = nil
	journal.Deliveries[fixture.deliveryID] = delivery
	if err := fixture.store.Save(fixture.scope.WorkspaceID, journal); err != nil {
		t.Fatalf("Save(legacy delivery) error = %v", err)
	}
}

type fakeDeliveryRunClient struct {
	now           time.Time
	recentCalls   int
	startCalls    int
	statusCalls   int
	lastRequest   deliveryStartRequest
	recentFactory func(deliveryStartRequest) []deliveryRun
	startError    error
	statuses      map[string]deliveryRunDetail
}

func testParentRunDetail(workspaceID, runID, status string, request deliveryStartRequest, outputs []deliveryOutput) deliveryRunDetail {
	created := request.AbsoluteDeadline.Add(-4 * time.Hour)
	return deliveryRunDetail{
		Run: deliveryRun{
			ID: runID, WorkspaceID: workspaceID, LoopName: "batuta-deliver", Status: status,
			CreatedAt: created, StartedAt: created, Inputs: deliveryInputs(request),
		},
		Generations: []deliveryGeneration{{Generation: 1, Outputs: outputs}},
	}
}

func testChildRunDetail(workspaceID, runID, loopName, status string, tokens int64, outputs []deliveryOutput) deliveryRunDetail {
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	return deliveryRunDetail{
		Run: deliveryRun{
			ID: runID, WorkspaceID: workspaceID, LoopName: loopName, Status: status,
			CreatedAt: now, StartedAt: now, TokensUsed: tokens, Inputs: map[string]any{"slug": "demo"},
		},
		Generations: []deliveryGeneration{{Generation: 1, Outputs: outputs}},
	}
}

func (c *fakeDeliveryRunClient) Status(_ context.Context, _ string, runID string) (deliveryRunDetail, error) {
	c.statusCalls++
	detail, exists := c.statuses[runID]
	if !exists {
		return deliveryRunDetail{}, errors.New("unexpected status")
	}
	return detail, nil
}

func (c *fakeDeliveryRunClient) Recent(context.Context, string, int) ([]deliveryRun, error) {
	c.recentCalls++
	if c.recentFactory == nil {
		return nil, nil
	}
	return c.recentFactory(c.lastRequest), nil
}

func (c *fakeDeliveryRunClient) Start(_ context.Context, workspaceID string, request deliveryStartRequest) (deliveryRun, error) {
	c.startCalls++
	c.lastRequest = request
	if c.startError != nil {
		return deliveryRun{}, c.startError
	}
	return deliveryRun{ID: fmt.Sprintf("run_attempt_%d", request.Attempt), WorkspaceID: workspaceID, LoopName: "batuta-deliver", Status: "queued", CreatedAt: c.now, Inputs: deliveryInputs(request)}, nil
}
