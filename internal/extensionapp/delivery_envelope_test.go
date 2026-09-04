package extensionapp

import (
	"context"
	"errors"
	"reflect"
	"testing"
	"time"

	"github.com/batuta-ai/core/routing"
)

func TestDeliveryRequestForAttemptReconstructsJournaledBudgets(t *testing.T) {
	t.Parallel()

	delivery := deliveryRequestFixture()
	request, err := deliveryRequestForAttempt(delivery, delivery.Attempts[1])
	if err != nil {
		t.Fatalf("deliveryRequestForAttempt() error = %v", err)
	}
	want := deliveryStartRequest{
		DeliveryID: "delivery_demo", Attempt: 2, Slug: "demo", OriginSessionID: "session_demo",
		WorktreeRef: "wt_demo", RoutingGeneration: "generation_demo",
		AbsoluteDeadline: delivery.AbsoluteDeadline, TokenCeiling: 1_000,
		RecoveryOperationID: "operation_second", IterationCap: 64,
		BudgetTokens: 750, BudgetWallSec: 6_600,
	}
	if !reflect.DeepEqual(request, want) {
		t.Fatalf("reconstructed request = %#v, want %#v", request, want)
	}
}

func TestDeliveryRequestForAttemptUsesEmptyFirstRecoveryIDAndLegacyCap(t *testing.T) {
	t.Parallel()

	delivery := deliveryRequestFixture()
	delivery.Graph = nil
	delivery.Attempts = delivery.Attempts[:1]
	request, err := deliveryRequestForAttempt(delivery, delivery.Attempts[0])
	if err != nil {
		t.Fatalf("deliveryRequestForAttempt() error = %v", err)
	}
	if request.RecoveryOperationID != "" || request.IterationCap != 4 || request.BudgetTokens != 1_000 || request.BudgetWallSec != 7_200 {
		t.Fatalf("first request = %#v", request)
	}
}

func TestDeliveryRequestForAttemptSupportsJournalReadStates(t *testing.T) {
	t.Parallel()

	for _, state := range []routing.AttemptState{routing.AttemptPlanned, routing.AttemptSubmitted, routing.AttemptTerminal} {
		state := state
		t.Run(string(state), func(t *testing.T) {
			delivery := deliveryRequestFixture()
			delivery.Attempts[1].State = state
			if _, err := deliveryRequestForAttempt(delivery, delivery.Attempts[1]); err != nil {
				t.Fatalf("deliveryRequestForAttempt(%s) error = %v", state, err)
			}
		})
	}
}

func TestDeliveryRequestForAttemptRejectsMalformedJournalOrExhaustedBudget(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		mutate  func(*routing.DeliveryRecord)
		attempt func(routing.DeliveryRecord) routing.DeliveryAttempt
	}{
		{
			name:   "attempt is not last",
			mutate: func(*routing.DeliveryRecord) {},
			attempt: func(delivery routing.DeliveryRecord) routing.DeliveryAttempt {
				return delivery.Attempts[0]
			},
		},
		{
			name: "attempt position is discontinuous",
			mutate: func(delivery *routing.DeliveryRecord) {
				delivery.Attempts[1].Attempt = 3
			},
		},
		{
			name: "prior attempt is not terminal",
			mutate: func(delivery *routing.DeliveryRecord) {
				delivery.Attempts[0].State = routing.AttemptSubmitted
			},
		},
		{
			name: "selected attempt has invalid state",
			mutate: func(delivery *routing.DeliveryRecord) {
				delivery.Attempts[1].State = "unknown"
			},
		},
		{
			name: "negative prior token usage",
			mutate: func(delivery *routing.DeliveryRecord) {
				delivery.Attempts[0].TokensUsed = -1
			},
		},
		{
			name: "token budget exhausted",
			mutate: func(delivery *routing.DeliveryRecord) {
				delivery.Attempts[0].TokensUsed = delivery.TokenCeiling
			},
		},
		{
			name: "wall budget non-positive",
			mutate: func(delivery *routing.DeliveryRecord) {
				delivery.AbsoluteDeadline = delivery.Attempts[1].PlannedAt
			},
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			delivery := deliveryRequestFixture()
			tt.mutate(&delivery)
			attempt := delivery.Attempts[len(delivery.Attempts)-1]
			if tt.attempt != nil {
				attempt = tt.attempt(delivery)
			}
			if _, err := deliveryRequestForAttempt(delivery, attempt); !errors.Is(err, routing.ErrDeliveryConflict) {
				t.Fatalf("deliveryRequestForAttempt() error = %v, want ErrDeliveryConflict", err)
			}
		})
	}
}

func TestDeliveryEnvelopeSelectsExactlyOwnedCore(t *testing.T) {
	t.Parallel()

	request := envelopeRequestFixture()
	validLauncher, validCore := validDeliveryEnvelope(request)
	legacy := validLauncher
	legacy.Run.ID = "run_legacy"
	legacy.Run.Inputs = markerFreeDeliveryInputs(request)
	legacy.Generations = nil

	unsupported, unsupportedCore := validDeliveryEnvelope(request)
	unsupported.Run.Inputs["delivery_envelope_version"] = int64(2)
	malformed, malformedCore := validDeliveryEnvelope(request)
	malformed.Run.Inputs["delivery_envelope_version"] = "1"
	missing, missingCore := validDeliveryEnvelope(request)
	missing.Generations = nil
	duplicate, duplicateCore := validDeliveryEnvelope(request)
	duplicate.Generations[0].Outputs = append(duplicate.Generations[0].Outputs, deliveryOutput{NodeID: "delivery_core", Status: "failed", ChildLoopRunID: "run_other"})
	emptyID, emptyIDCore := validDeliveryEnvelope(request)
	emptyID.Generations[0].Outputs[0].ChildLoopRunID = ""
	foreign, foreignCore := validDeliveryEnvelope(request)
	foreignCore.Run.WorkspaceID = "ws_foreign"
	wrongParent, wrongParentCore := validDeliveryEnvelope(request)
	wrongParentCore.Run.ParentLoopRunID = "run_foreign_parent"
	wrongLoop, wrongLoopCore := validDeliveryEnvelope(request)
	wrongLoopCore.Run.LoopName = "batuta-deliver"
	nonterminal, nonterminalCore := validDeliveryEnvelope(request)
	nonterminalCore.Run.Status = "running"
	contradictory, contradictoryCore := validDeliveryEnvelope(request)
	contradictoryCore.Run.Inputs["budget_tokens"] = request.BudgetTokens - 1
	failedCoreOutput, failedCore := validDeliveryEnvelope(request)
	failedCore.Run.Status = "failed"
	failedLauncher, successfulCore := validDeliveryEnvelope(request)
	failedLauncher.Run.Status = "failed"

	tests := []struct {
		name      string
		launcher  deliveryRunDetail
		core      deliveryRunDetail
		wantRunID string
		wantErr   error
		wantCalls int
	}{
		{name: "valid launcher", launcher: validLauncher, core: validCore, wantRunID: "run_core", wantCalls: 1},
		{name: "legacy direct run", launcher: legacy, wantRunID: "run_legacy"},
		{name: "unsupported present version", launcher: unsupported, core: unsupportedCore, wantErr: routing.ErrDeliveryConflict},
		{name: "malformed present version", launcher: malformed, core: malformedCore, wantErr: routing.ErrDeliveryConflict},
		{name: "missing core output", launcher: missing, core: missingCore, wantErr: routing.ErrDeliveryConflict},
		{name: "duplicate core output", launcher: duplicate, core: duplicateCore, wantErr: routing.ErrDeliveryConflict},
		{name: "empty core id", launcher: emptyID, core: emptyIDCore, wantErr: routing.ErrDeliveryConflict},
		{name: "foreign workspace", launcher: foreign, core: foreignCore, wantErr: routing.ErrDeliveryConflict, wantCalls: 1},
		{name: "wrong parent", launcher: wrongParent, core: wrongParentCore, wantErr: routing.ErrDeliveryConflict, wantCalls: 1},
		{name: "wrong loop", launcher: wrongLoop, core: wrongLoopCore, wantErr: routing.ErrDeliveryConflict, wantCalls: 1},
		{name: "nonterminal core", launcher: nonterminal, core: nonterminalCore, wantErr: routing.ErrDeliveryConflict, wantCalls: 1},
		{name: "contradictory core inputs", launcher: contradictory, core: contradictoryCore, wantErr: routing.ErrDeliveryConflict, wantCalls: 1},
		{name: "success output with failed core", launcher: failedCoreOutput, core: failedCore, wantErr: routing.ErrDeliveryConflict, wantCalls: 1},
		{name: "failed launcher with successful core", launcher: failedLauncher, core: successfulCore, wantErr: routing.ErrDeliveryConflict, wantCalls: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := &envelopeStatusClient{detail: tt.core}
			service := deliveryAttemptService{Client: client}
			got, err := service.settlementParentDetail(context.Background(), "ws_demo", request, tt.launcher)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("settlementParentDetail() error = %v, want %v", err, tt.wantErr)
			}
			if got.Run.ID != tt.wantRunID {
				t.Fatalf("settlementParentDetail() run = %q, want %q", got.Run.ID, tt.wantRunID)
			}
			if tt.wantErr != nil && !reflect.DeepEqual(got, deliveryRunDetail{}) {
				t.Fatalf("settlementParentDetail() detail = %#v, want no adoptable detail", got)
			}
			if client.statusCalls != tt.wantCalls || client.statusCalls > 1 {
				t.Fatalf("Status() calls = %d, want %d and at most one", client.statusCalls, tt.wantCalls)
			}
		})
	}
}

func TestDeliveryEnvelopeVersionDistinguishesMissingFromMalformed(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name        string
		inputs      map[string]any
		wantVersion int64
		wantPresent bool
	}{
		{name: "missing", inputs: map[string]any{}, wantVersion: 0, wantPresent: false},
		{name: "malformed present", inputs: map[string]any{"delivery_envelope_version": "1"}, wantVersion: -1, wantPresent: true},
		{name: "modern", inputs: map[string]any{"delivery_envelope_version": int64(1)}, wantVersion: 1, wantPresent: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			version, present := deliveryEnvelopeVersionOf(deliveryRun{Inputs: tt.inputs})
			if version != tt.wantVersion || present != tt.wantPresent {
				t.Fatalf("deliveryEnvelopeVersionOf() = (%d, %t), want (%d, %t)", version, present, tt.wantVersion, tt.wantPresent)
			}
		})
	}
}

func deliveryRequestFixture() routing.DeliveryRecord {
	plannedAt := time.Date(2026, time.September, 1, 10, 0, 0, 0, time.UTC)
	return routing.DeliveryRecord{
		DeliveryID: "delivery_demo", WorktreeID: "wt_demo", Slug: "demo",
		OriginSessionID: "session_demo", RoutingGenerationDigest: "generation_demo",
		AbsoluteDeadline: plannedAt.Add(2 * time.Hour), TokenCeiling: 1_000,
		Graph: &routing.DeliveryGraph{},
		Attempts: []routing.DeliveryAttempt{
			{Attempt: 1, OperationID: "operation_first", State: routing.AttemptTerminal, PlannedAt: plannedAt, TokensUsed: 250},
			{Attempt: 2, OperationID: "operation_second", State: routing.AttemptPlanned, PlannedAt: plannedAt.Add(10 * time.Minute)},
		},
	}
}

func envelopeRequestFixture() deliveryStartRequest {
	return deliveryStartRequest{
		DeliveryID: "delivery_demo", Attempt: 2, Slug: "demo", OriginSessionID: "session_demo",
		WorktreeRef: "wt_demo", RoutingGeneration: "generation_demo",
		AbsoluteDeadline: time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC),
		TokenCeiling:     1_000, RecoveryOperationID: "operation_second", IterationCap: 64,
		BudgetTokens: 750, BudgetWallSec: 6_600,
	}
}

func validDeliveryEnvelope(request deliveryStartRequest) (deliveryRunDetail, deliveryRunDetail) {
	launcher := deliveryRunDetail{
		Run: deliveryRun{
			ID: "run_launcher", WorkspaceID: "ws_demo", LoopName: "batuta-deliver", Status: "done",
			Inputs: deliveryInputs(request),
		},
		Generations: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{{
			NodeID: "delivery_core", Status: "succeeded", ChildLoopRunID: "run_core",
		}}}},
	}
	core := deliveryRunDetail{Run: deliveryRun{
		ID: "run_core", WorkspaceID: "ws_demo", ParentLoopRunID: "run_launcher",
		LoopName: "batuta-deliver-core", Status: "done", Inputs: deliveryInputs(request),
	}}
	return launcher, core
}

func markerFreeDeliveryInputs(request deliveryStartRequest) map[string]any {
	inputs := deliveryInputs(request)
	delete(inputs, "delivery_envelope_version")
	return inputs
}

type envelopeStatusClient struct {
	detail      deliveryRunDetail
	statusCalls int
}

func (c *envelopeStatusClient) Status(context.Context, string, string) (deliveryRunDetail, error) {
	c.statusCalls++
	return c.detail, nil
}

func (*envelopeStatusClient) Recent(context.Context, string, int) ([]deliveryRun, error) {
	return nil, errors.New("unexpected Recent call")
}

func (*envelopeStatusClient) Start(context.Context, string, deliveryStartRequest) (deliveryRun, error) {
	return deliveryRun{}, errors.New("unexpected Start call")
}
