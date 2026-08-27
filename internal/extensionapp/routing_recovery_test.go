package extensionapp

import (
	"context"
	"errors"
	"fmt"
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
	journal, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("Load() exists=%v error=%v", exists, err)
	}
	delivery := journal.Deliveries[fixture.deliveryID]
	if len(delivery.Attempts) != 1 || delivery.Attempts[0].State != routing.AttemptSubmitted || delivery.Attempts[0].RunID != "run_attempt_1" {
		t.Fatalf("attempts = %#v", delivery.Attempts)
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

type fakeDeliveryRunClient struct {
	now           time.Time
	recentCalls   int
	startCalls    int
	lastRequest   deliveryStartRequest
	recentFactory func(deliveryStartRequest) []deliveryRun
	startError    error
	statuses      map[string]deliveryRunDetail
}

func (c *fakeDeliveryRunClient) Status(_ context.Context, _ string, runID string) (deliveryRunDetail, error) {
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
