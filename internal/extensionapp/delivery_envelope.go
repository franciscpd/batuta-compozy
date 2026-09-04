package extensionapp

import (
	"context"
	"reflect"
	"time"

	"github.com/batuta-ai/core/routing"
)

func deliveryRequestForAttempt(delivery routing.DeliveryRecord, attempt routing.DeliveryAttempt) (deliveryStartRequest, error) {
	if len(delivery.Attempts) == 0 || attempt.Attempt != len(delivery.Attempts) ||
		attempt.Attempt < 1 || !reflect.DeepEqual(delivery.Attempts[attempt.Attempt-1], attempt) {
		return deliveryStartRequest{}, routing.ErrDeliveryConflict
	}
	remainingTokens := delivery.TokenCeiling
	if remainingTokens <= 0 {
		return deliveryStartRequest{}, routing.ErrDeliveryConflict
	}
	for index, current := range delivery.Attempts {
		if current.Attempt != index+1 {
			return deliveryStartRequest{}, routing.ErrDeliveryConflict
		}
		if index == len(delivery.Attempts)-1 {
			switch current.State {
			case routing.AttemptPlanned, routing.AttemptSubmitted, routing.AttemptTerminal:
			default:
				return deliveryStartRequest{}, routing.ErrDeliveryConflict
			}
			if current.TokensUsed < 0 {
				return deliveryStartRequest{}, routing.ErrDeliveryConflict
			}
			continue
		}
		if current.State != routing.AttemptTerminal || current.TokensUsed < 0 || current.TokensUsed >= remainingTokens {
			return deliveryStartRequest{}, routing.ErrDeliveryConflict
		}
		remainingTokens -= current.TokensUsed
	}
	remainingWall := int64(delivery.AbsoluteDeadline.Sub(attempt.PlannedAt) / time.Second)
	if remainingTokens <= 0 || remainingWall <= 0 || int64(int(remainingWall)) != remainingWall {
		return deliveryStartRequest{}, routing.ErrDeliveryConflict
	}
	request := deliveryStartRequest{
		DeliveryID: delivery.DeliveryID, Attempt: attempt.Attempt, Slug: delivery.Slug,
		OriginSessionID: delivery.OriginSessionID, WorktreeRef: delivery.WorktreeID,
		RoutingGeneration: delivery.RoutingGenerationDigest, AbsoluteDeadline: delivery.AbsoluteDeadline,
		TokenCeiling: delivery.TokenCeiling, RecoveryOperationID: attempt.OperationID,
		IterationCap: deliveryParentIterationCap(delivery.Graph), BudgetTokens: remainingTokens,
		BudgetWallSec: int(remainingWall),
	}
	if attempt.Attempt == 1 {
		request.RecoveryOperationID = ""
	}
	return request, nil
}

func deliveryEnvelopeVersionOf(run deliveryRun) (int64, bool) {
	_, present := run.Inputs["delivery_envelope_version"]
	if !present {
		return 0, false
	}
	return intInput(run.Inputs, "delivery_envelope_version"), true
}

func (s deliveryAttemptService) settlementParentDetail(
	ctx context.Context,
	workspaceID string,
	request deliveryStartRequest,
	launcher deliveryRunDetail,
) (deliveryRunDetail, error) {
	version, present := deliveryEnvelopeVersionOf(launcher.Run)
	if !present {
		return launcher, nil
	}
	if version != deliveryEnvelopeVersion || !deliveryRunMatchesRequest(launcher.Run, request) {
		return deliveryRunDetail{}, routing.ErrDeliveryConflict
	}
	var coreOutput deliveryOutput
	found := false
	for _, generation := range launcher.Generations {
		for _, output := range generation.Outputs {
			if output.NodeID != "delivery_core" || (output.Status != "succeeded" && output.Status != "failed") {
				continue
			}
			if found {
				return deliveryRunDetail{}, routing.ErrDeliveryConflict
			}
			coreOutput = output
			found = true
		}
	}
	if !found || !validOpaqueRunID(coreOutput.ChildLoopRunID) || s.Client == nil {
		return deliveryRunDetail{}, routing.ErrDeliveryConflict
	}
	core, err := s.Client.Status(ctx, workspaceID, coreOutput.ChildLoopRunID)
	if err != nil || core.Run.ID != coreOutput.ChildLoopRunID || core.Run.WorkspaceID != workspaceID ||
		core.Run.ParentLoopRunID != launcher.Run.ID || core.Run.LoopName != "batuta-deliver-core" ||
		!deliveryRunMatchesRequest(core.Run, request) || !terminalDeliveryStatus(core.Run.Status) {
		return deliveryRunDetail{}, routing.ErrDeliveryConflict
	}
	coreSucceeded := core.Run.Status == "done" || core.Run.Status == "no-op"
	if coreSucceeded {
		if coreOutput.Status != "succeeded" || (launcher.Run.Status != "done" && launcher.Run.Status != "no-op") {
			return deliveryRunDetail{}, routing.ErrDeliveryConflict
		}
	} else if coreOutput.Status != "failed" || (launcher.Run.Status != core.Run.Status && launcher.Run.Status != "failed") {
		return deliveryRunDetail{}, routing.ErrDeliveryConflict
	}
	return core, nil
}
