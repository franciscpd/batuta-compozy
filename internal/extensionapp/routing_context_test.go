package extensionapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/franciscpd/batuta-compozy/internal/routing"
)

func TestDeliveryContextDescriptorsAreClosedReadOnlyTools(t *testing.T) {
	t.Parallel()
	extension, err := newWithServices(serviceSet{})
	if err != nil {
		t.Fatalf("newWithServices() error = %v", err)
	}
	tests := []struct {
		handler    string
		required   []string
		properties []string
	}{
		{handler: "routing_context", required: []string{"delivery_id", "attempt", "slug", "routing_generation"}, properties: []string{"delivery_id", "attempt", "slug", "routing_generation"}},
		{handler: "delivery_budget_context", required: []string{"delivery_id", "attempt"}, properties: []string{"delivery_id", "attempt"}},
	}
	for _, tt := range tests {
		t.Run(tt.handler, func(t *testing.T) {
			descriptor := descriptorForHandler(t, extension, tt.handler)
			if !descriptor.ReadOnly || descriptor.Risk != compozysdk.RiskRead {
				t.Fatalf("descriptor = %#v, want read-only", descriptor)
			}
			var schema struct {
				Required             []string                   `json:"required"`
				Properties           map[string]json.RawMessage `json:"properties"`
				AdditionalProperties bool                       `json:"additionalProperties"`
			}
			if err := json.Unmarshal(descriptor.InputSchema, &schema); err != nil {
				t.Fatalf("input schema: %v", err)
			}
			if schema.AdditionalProperties || !reflect.DeepEqual(schema.Required, tt.required) || len(schema.Properties) != len(tt.properties) {
				t.Fatalf("input schema = %s", descriptor.InputSchema)
			}
			for _, property := range tt.properties {
				if _, exists := schema.Properties[property]; !exists {
					t.Fatalf("input schema missing %q: %s", property, descriptor.InputSchema)
				}
			}
		})
	}
}

func TestRoutingContextReturnsAttemptRulesAndRemainingBudgetWithoutMutation(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	makeLegacyDeliveryFixture(t, fixture)
	started, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	before, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("Load(before) exists=%v error=%v", exists, err)
	}
	delivery := before.Deliveries[fixture.deliveryID]
	wantRules := append([]routing.RuntimeRule(nil), delivery.Attempts[0].RuntimeRules...)
	service := &deliveryContextService{Store: fixture.store, Client: fixture.client, Now: func() time.Time { return fixture.now }}

	output, err := service.Routing(context.Background(), fixture.scope, RoutingContextInput{
		DeliveryID: fixture.deliveryID, Attempt: started.Attempt, Slug: delivery.Slug,
		RoutingGeneration: delivery.RoutingGenerationDigest,
	})
	if err != nil {
		t.Fatalf("Routing() error = %v", err)
	}
	wantWall := int(delivery.AbsoluteDeadline.Sub(fixture.now) / time.Second)
	if !reflect.DeepEqual(output.RuntimeRules, wantRules) || output.RemainingTokens != delivery.TokenCeiling || output.RemainingWallSeconds != wantWall {
		t.Fatalf("Routing() = %#v, want rules=%#v tokens=%d wall=%d", output, wantRules, delivery.TokenCeiling, wantWall)
	}
	output.RuntimeRules[0].Runtime.Model = "mutated-by-caller"
	after, _, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("journal changed after routing context: error=%v before=%#v after=%#v", err, before, after)
	}
}

func TestRoutingContextRejectsCurrentTaskSetDrift(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	started, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	taskPath := filepath.Join(fixture.scope.WorkspaceRoot, ".compozy", "tasks", "demo", "task_01.md")
	if err := os.WriteFile(taskPath, []byte("---\nstatus: pending\ntitle: Changed\ntype: frontend\ncomplexity: low\n---\n\nchanged after matrix apply\n"), 0o600); err != nil {
		t.Fatalf("write changed task: %v", err)
	}
	service := &deliveryContextService{Store: fixture.store, Client: fixture.client, Now: func() time.Time { return fixture.now }}

	_, err = service.Routing(context.Background(), fixture.scope, RoutingContextInput{
		DeliveryID: fixture.deliveryID, Attempt: started.Attempt, Slug: delivery.Slug,
		RoutingGeneration: delivery.RoutingGenerationDigest,
	})
	if !errors.Is(err, routing.ErrDeliveryConflict) {
		t.Fatalf("Routing(task drift) error = %v, want ErrDeliveryConflict", err)
	}
}

func TestDeliveryBudgetContextCountsOneOwnedImplementationChildOnce(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	makeLegacyDeliveryFixture(t, fixture)
	started, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	fixture.client.statuses = map[string]deliveryRunDetail{
		started.DeliveryRunID: deliveryParentWithImplementation(fixture, started, "run_implement"),
		"run_implement": {Run: deliveryRun{
			ID: "run_implement", WorkspaceID: fixture.scope.WorkspaceID, ParentLoopRunID: started.DeliveryRunID,
			LoopName: "implement-tasks", Status: "done", CreatedAt: fixture.now, StartedAt: fixture.now,
			TokensUsed: 1250, TokensUsedPresent: true, Inputs: map[string]any{"slug": "demo"},
		}},
	}
	before, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	service := &deliveryContextService{Store: fixture.store, Client: fixture.client, Now: func() time.Time { return fixture.now }}
	input := DeliveryBudgetContextInput{DeliveryID: fixture.deliveryID, Attempt: 1}

	first, err := service.Budget(context.Background(), fixture.scope, input)
	if err != nil {
		t.Fatalf("Budget() error = %v", err)
	}
	second, err := service.Budget(context.Background(), fixture.scope, input)
	if err != nil {
		t.Fatalf("Budget(replay) error = %v", err)
	}
	if first.RemainingTokens != 998750 || second.RemainingTokens != first.RemainingTokens || fixture.client.statusCalls != 4 {
		t.Fatalf("budget = first:%#v second:%#v status_calls=%d", first, second, fixture.client.statusCalls)
	}
	after, _, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("journal changed after budget context: error=%v before=%#v after=%#v", err, before, after)
	}
}

func TestDeliveryBudgetContextRejectsForeignNonterminalFailedAndMissingUsageChildren(t *testing.T) {
	t.Parallel()
	tests := []struct {
		name   string
		mutate func(*deliveryRun)
	}{
		{name: "foreign parent", mutate: func(run *deliveryRun) { run.ParentLoopRunID = "run_foreign" }},
		{name: "nonterminal", mutate: func(run *deliveryRun) { run.Status = "running" }},
		{name: "failed", mutate: func(run *deliveryRun) { run.Status = "failed" }},
		{name: "missing usage", mutate: func(run *deliveryRun) { run.TokensUsedPresent = false }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newDeliveryServiceFixture(t)
			makeLegacyDeliveryFixture(t, fixture)
			started, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
			if err != nil {
				t.Fatalf("Start() error = %v", err)
			}
			child := deliveryRun{
				ID: "run_implement", WorkspaceID: fixture.scope.WorkspaceID, ParentLoopRunID: started.DeliveryRunID,
				LoopName: "implement-tasks", Status: "done", CreatedAt: fixture.now, StartedAt: fixture.now,
				TokensUsed: 1250, TokensUsedPresent: true, Inputs: map[string]any{"slug": "demo"},
			}
			tt.mutate(&child)
			fixture.client.statuses = map[string]deliveryRunDetail{
				started.DeliveryRunID: deliveryParentWithImplementation(fixture, started, "run_implement"),
				"run_implement":       {Run: child},
			}
			service := &deliveryContextService{Store: fixture.store, Client: fixture.client, Now: func() time.Time { return fixture.now }}

			_, err = service.Budget(context.Background(), fixture.scope, DeliveryBudgetContextInput{
				DeliveryID: fixture.deliveryID, Attempt: 1,
			})
			if !errors.Is(err, routing.ErrDeliveryConflict) {
				t.Fatalf("Budget() error = %v, want ErrDeliveryConflict", err)
			}
		})
	}
}

func TestDeliveryBudgetContextDoesNotAccountAnExhaustedChild(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	makeLegacyDeliveryFixture(t, fixture)
	started, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
	if err != nil {
		t.Fatalf("Start() error = %v", err)
	}
	child := deliveryRun{
		ID: "run_implement", WorkspaceID: fixture.scope.WorkspaceID, ParentLoopRunID: started.DeliveryRunID,
		LoopName: "implement-tasks", Status: "done", CreatedAt: fixture.now, StartedAt: fixture.now,
		TokensUsed: 1_000_000, TokensUsedPresent: true, Inputs: map[string]any{"slug": "demo"},
	}
	fixture.client.statuses = map[string]deliveryRunDetail{
		started.DeliveryRunID: deliveryParentWithImplementation(fixture, started, "run_implement"),
		"run_implement":       {Run: child},
	}
	service := &deliveryContextService{Store: fixture.store, Client: fixture.client, Now: func() time.Time { return fixture.now }}
	input := DeliveryBudgetContextInput{DeliveryID: fixture.deliveryID, Attempt: 1}

	if _, err := service.Budget(context.Background(), fixture.scope, input); !errors.Is(err, routing.ErrNoEligibleCandidate) {
		t.Fatalf("Budget(exhausted) error = %v, want ErrNoEligibleCandidate", err)
	}
	child.TokensUsed = 1
	fixture.client.statuses["run_implement"] = deliveryRunDetail{Run: child}
	output, err := service.Budget(context.Background(), fixture.scope, input)
	if err != nil {
		t.Fatalf("Budget(after rejected child) error = %v", err)
	}
	if output.RemainingTokens != 999999 {
		t.Fatalf("Budget(after rejected child) = %#v", output)
	}
}

func TestDeliveryBudgetContextUsesDurableGraphUsageWithoutPollingChildren(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		if _, err := delivery.Graph.RecordCandidate("task_01", 1, routing.TaskCandidate{
			ChildRunID: "run_task_01", BaseHeadSHA: wave.BaseHeadSHA,
			CommitSHA:          "abcdefabcdefabcdefabcdefabcdefabcdefabcd",
			VerificationDigest: digestValue("graph-budget-verification"), TokensUsed: 500,
		}); err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist graph usage: %v", err)
	}
	client := &fakeDeliveryRunClient{}
	service := deliveryContextService{Store: fixture.store, Client: client, Now: func() time.Time { return fixture.now }}

	output, err := service.Budget(context.Background(), fixture.scope, DeliveryBudgetContextInput{
		DeliveryID: fixture.deliveryID, Attempt: 1,
	})
	if err != nil || output.RemainingTokens != 999_500 || output.RemainingWallSeconds <= 0 || client.statusCalls != 0 {
		t.Fatalf("Budget(graph) = %#v, error=%v status_calls=%d", output, err, client.statusCalls)
	}
}

func TestDeliveryBudgetContextUsesGraphBeforeFirstWave(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	client := &fakeDeliveryRunClient{}
	service := deliveryContextService{Store: fixture.store, Client: client, Now: func() time.Time { return fixture.now }}

	output, err := service.Budget(context.Background(), fixture.scope, DeliveryBudgetContextInput{
		DeliveryID: fixture.deliveryID, Attempt: 1,
	})
	if err != nil || output.RemainingTokens != 1_000_000 || output.RemainingWallSeconds <= 0 || client.statusCalls != 0 {
		t.Fatalf("Budget(graph before wave) = %#v, error=%v status_calls=%d", output, err, client.statusCalls)
	}
}

func deliveryParentWithImplementation(
	fixture deliveryServiceFixture,
	started RoutingStartResult,
	implementationRunID string,
) deliveryRunDetail {
	return deliveryRunDetail{
		Run: deliveryRun{
			ID: started.DeliveryRunID, WorkspaceID: fixture.scope.WorkspaceID, LoopName: "batuta-deliver",
			Status: "running", CreatedAt: fixture.now, StartedAt: fixture.now,
			TokensUsedPresent: true, Inputs: deliveryInputs(fixture.client.lastRequest),
		},
		Generations: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{{
			NodeID: "implement", Status: "succeeded", ChildLoopRunID: implementationRunID,
		}}}},
	}
}

func makeLegacyDeliveryFixture(t *testing.T, fixture deliveryServiceFixture) {
	t.Helper()
	journal, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("Load(legacy fixture) exists=%v error=%v", exists, err)
	}
	delivery := journal.Deliveries[fixture.deliveryID]
	delivery.Graph = nil
	journal.Deliveries[fixture.deliveryID] = delivery
	if err := fixture.store.Save(fixture.scope.WorkspaceID, journal); err != nil {
		t.Fatalf("Save(legacy fixture) error=%v", err)
	}
}
