package extensionapp

import (
	"context"
	"errors"
	"reflect"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/publication"
	"github.com/franciscpd/batuta-compozy/internal/routing"
)

var errAmbiguousDeliveryStart = errors.New("batuta: more than one matching delivery run exists")

type deliveryOutput struct {
	NodeID         string `json:"node_id"`
	ItemIndex      int    `json:"item_index,omitempty"`
	Status         string `json:"status"`
	OutputRef      string `json:"output_ref,omitempty"`
	ChildLoopRunID string `json:"child_loop_run_id,omitempty"`
}

type deliveryGeneration struct {
	Generation int64            `json:"generation"`
	Outputs    []deliveryOutput `json:"outputs"`
}

type RoutingStartResult struct {
	DeliveryID    string `json:"delivery_id"`
	Attempt       int    `json:"attempt"`
	OperationID   string `json:"operation_id"`
	DeliveryRunID string `json:"delivery_run_id"`
	Replayed      bool   `json:"replayed,omitempty"`
}

type deliveryAttemptService struct {
	Store         *routing.OwnershipStore
	Client        deliveryRunClient
	WorktreeState func(context.Context, string) (publication.WorktreeState, error)
	Now           func() time.Time
}

func (s deliveryAttemptService) Start(ctx context.Context, scope publication.TrustedScope, deliveryID string) (RoutingStartResult, error) {
	return s.start(ctx, scope, deliveryID, "")
}

func (s deliveryAttemptService) start(
	ctx context.Context,
	scope publication.TrustedScope,
	deliveryID string,
	priorRunID string,
) (RoutingStartResult, error) {
	if err := ctx.Err(); err != nil {
		return RoutingStartResult{}, err
	}
	if s.Store == nil || s.Client == nil || s.WorktreeState == nil || !routingDigestPattern.MatchString(deliveryID) || !validOpaqueRunID(scope.WorkspaceID) {
		return RoutingStartResult{}, errors.New("batuta: delivery attempt service is unavailable")
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	var result RoutingStartResult
	err := s.Store.WithLockedJournal(scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery, exists := tx.Journal.Deliveries[deliveryID]
		if !exists || delivery.WorkspaceID != scope.WorkspaceID || delivery.State != routing.DeliveryStateActive {
			return routing.ErrDeliveryConflict
		}
		generation, exists := tx.Journal.Generations[delivery.RoutingGenerationDigest]
		if !exists {
			return routing.ErrOwnershipUnproven
		}
		if len(delivery.Attempts) > 0 {
			last := delivery.Attempts[len(delivery.Attempts)-1]
			switch last.State {
			case routing.AttemptSubmitted:
				if !attemptPredecessorMatches(delivery.Attempts, last.Attempt, priorRunID) {
					return routing.ErrDeliveryConflict
				}
				result = routingStartResult(deliveryID, last, true)
				return nil
			case routing.AttemptPlanned:
				if !attemptPredecessorMatches(delivery.Attempts, last.Attempt, priorRunID) {
					return routing.ErrDeliveryConflict
				}
			case routing.AttemptTerminal:
				if priorRunID == "" || last.RunID != priorRunID {
					return routing.ErrDeliveryConflict
				}
			default:
				return routing.ErrDeliveryConflict
			}
		}
		if !now.Before(delivery.AbsoluteDeadline) || len(delivery.Attempts) >= delivery.AttemptCeiling {
			delivery.State = routing.DeliveryStateExhausted
			tx.Journal.Deliveries[deliveryID] = delivery
			if err := tx.Persist(); err != nil {
				return err
			}
			return routing.ErrNoEligibleCandidate
		}
		usedTokens := int64(0)
		for index, attempt := range delivery.Attempts {
			if attempt.State == routing.AttemptPlanned && index == len(delivery.Attempts)-1 {
				continue
			}
			if attempt.State != routing.AttemptTerminal || attempt.TokensUsed < 0 {
				return routing.ErrDeliveryConflict
			}
			usedTokens += attempt.TokensUsed
		}
		remainingTokens := delivery.TokenCeiling - usedTokens
		remainingWall := int64(delivery.AbsoluteDeadline.Sub(now) / time.Second)
		if remainingTokens <= 0 || remainingWall <= 0 {
			delivery.State = routing.DeliveryStateExhausted
			tx.Journal.Deliveries[deliveryID] = delivery
			if err := tx.Persist(); err != nil {
				return err
			}
			return routing.ErrNoEligibleCandidate
		}
		state, err := s.WorktreeState(ctx, delivery.WorktreeRoot)
		if err != nil {
			return err
		}
		fingerprint := routing.WorktreeFingerprint{HeadSHA: state.HeadSHA, PorcelainSHA256: state.PorcelainSHA256, ContentSHA256: state.ContentSHA256}
		wantFingerprint := delivery.InitialWorktreeFingerprint
		if len(delivery.Attempts) > 0 && delivery.Attempts[len(delivery.Attempts)-1].State == routing.AttemptTerminal {
			last := delivery.Attempts[len(delivery.Attempts)-1]
			if last.WorktreeFingerprint == nil {
				return routing.ErrDeliveryConflict
			}
			wantFingerprint = *last.WorktreeFingerprint
		}
		if !reflect.DeepEqual(fingerprint, wantFingerprint) {
			return routing.ErrDeliveryConflict
		}
		loader, err := routing.NewArtifactLoader(delivery.WorktreeRoot)
		if err != nil {
			return err
		}
		taskSet, err := loader.Load(delivery.Slug)
		if err != nil {
			return err
		}
		currentSnapshot, err := taskSet.DeliverySnapshot()
		if err != nil {
			return err
		}
		progress, err := delivery.TaskSnapshot.Reconcile(currentSnapshot)
		if err != nil || len(progress.IncompleteTaskIDs) == 0 {
			return routing.ErrDeliveryConflict
		}

		attemptNumber := len(delivery.Attempts) + 1
		var priorRules []routing.RuntimeRule
		var failedTaskIDs []string
		if len(delivery.Attempts) > 0 {
			last := delivery.Attempts[len(delivery.Attempts)-1]
			priorRules = last.RuntimeRules
			failedTaskIDs = last.FailedTaskIDs
		}
		rules, err := routing.SuccessorRuntimeRules(generation, priorRules, progress.IncompleteTaskIDs, failedTaskIDs)
		if err != nil {
			return err
		}
		operationID, requestDigest, err := routing.DeriveAttemptIdentity(delivery, attemptNumber, progress.IncompleteTaskIDs, rules, remainingTokens, remainingWall)
		if err != nil {
			return err
		}
		planned := routing.DeliveryAttempt{
			Attempt: attemptNumber, OperationID: operationID, RequestDigest: requestDigest,
			RuntimeRules: rules, State: routing.AttemptPlanned, PlannedAt: now,
		}
		if len(delivery.Attempts) > 0 && delivery.Attempts[len(delivery.Attempts)-1].State == routing.AttemptPlanned {
			planned = delivery.Attempts[len(delivery.Attempts)-1]
			attemptNumber = planned.Attempt
		} else {
			if _, _, err := delivery.AppendAttempt(planned); err != nil {
				return err
			}
			tx.Journal.Deliveries[deliveryID] = delivery
			if err := tx.Persist(); err != nil {
				return err
			}
		}
		request := deliveryStartRequest{
			DeliveryID: deliveryID, Attempt: attemptNumber, Slug: delivery.Slug, OriginSessionID: delivery.OriginSessionID,
			WorktreeRef: delivery.WorktreeID, RoutingGeneration: delivery.RoutingGenerationDigest,
			AbsoluteDeadline: delivery.AbsoluteDeadline, TokenCeiling: delivery.TokenCeiling,
			RecoveryOperationID: planned.OperationID, IterationCap: deliveryParentIterationCap(delivery.Graph),
			BudgetTokens: remainingTokens, BudgetWallSec: int(remainingWall),
		}
		if attemptNumber == 1 {
			request.RecoveryOperationID = ""
		}
		recent, err := s.Client.Recent(ctx, scope.WorkspaceID, 200)
		if err != nil {
			return err
		}
		matches := matchingRecentRuns(recent, request, planned.PlannedAt)
		if len(matches) > 1 {
			delivery.State = routing.DeliveryStateBlocked
			tx.Journal.Deliveries[deliveryID] = delivery
			if err := tx.Persist(); err != nil {
				return err
			}
			return errAmbiguousDeliveryStart
		}
		var run deliveryRun
		replayed := false
		if len(matches) == 1 {
			run, replayed = matches[0], true
		} else {
			run, err = s.Client.Start(ctx, scope.WorkspaceID, request)
			if err != nil {
				return err
			}
		}
		startedAt := run.StartedAt
		if startedAt.IsZero() {
			startedAt = run.CreatedAt
		}
		planned.State, planned.RunID, planned.StartedAt = routing.AttemptSubmitted, run.ID, &startedAt
		delivery.Attempts[attemptNumber-1] = planned
		tx.Journal.Deliveries[deliveryID] = delivery
		if err := tx.Persist(); err != nil {
			return err
		}
		result = routingStartResult(deliveryID, planned, replayed)
		return nil
	})
	return result, err
}

func deliveryParentIterationCap(graph *routing.DeliveryGraph) int {
	if graph == nil {
		return 4
	}
	return 64
}

func (s deliveryAttemptService) Recover(
	ctx context.Context,
	scope publication.TrustedScope,
	deliveryID string,
	deliveryRunID string,
) (RoutingStartResult, error) {
	if !validOpaqueRunID(deliveryRunID) {
		return RoutingStartResult{}, routing.ErrDeliveryConflict
	}
	return s.start(ctx, scope, deliveryID, deliveryRunID)
}

func (s deliveryAttemptService) Reconcile(
	ctx context.Context,
	scope publication.TrustedScope,
	deliveryID string,
	deliveryRunID string,
) (RoutingReconcileResult, error) {
	if s.Store == nil || s.Client == nil || s.WorktreeState == nil || !routingDigestPattern.MatchString(deliveryID) || !validOpaqueRunID(deliveryRunID) {
		return RoutingReconcileResult{}, errors.New("batuta: delivery reconciliation service is unavailable")
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	var result RoutingReconcileResult
	err := s.Store.WithLockedJournal(scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery, exists := tx.Journal.Deliveries[deliveryID]
		if !exists || delivery.WorkspaceID != scope.WorkspaceID || len(delivery.Attempts) == 0 {
			return routing.ErrDeliveryConflict
		}
		attempt := delivery.Attempts[len(delivery.Attempts)-1]
		if attempt.RunID != deliveryRunID || (attempt.State != routing.AttemptSubmitted && attempt.State != routing.AttemptTerminal) {
			return routing.ErrDeliveryConflict
		}
		if attempt.State == routing.AttemptTerminal {
			result = reconcileResult(delivery, attempt, now)
			return nil
		}
		detail, err := s.Client.Status(ctx, scope.WorkspaceID, deliveryRunID)
		if err != nil {
			return err
		}
		if detail.Run.LoopName != "batuta-deliver" || !runMatchesAttempt(detail.Run, delivery, attempt) {
			return routing.ErrDeliveryConflict
		}
		if !terminalDeliveryStatus(detail.Run.Status) {
			result = reconcileResult(delivery, attempt, now)
			result.State = "in_progress"
			return nil
		}
		if delivery.Graph != nil &&
			(detail.Run.Status == "done" || detail.Run.Status == "no-op") && !graphAllIntegrated(delivery.Graph) {
			return routing.ErrDeliveryConflict
		}
		fingerprint, err := s.WorktreeState(ctx, delivery.WorktreeRoot)
		if err != nil {
			return err
		}
		var childIDs, failedTaskIDs []string
		var tokens, graphTokens, reviewTokens int64
		publicationMutation := false
		if delivery.Graph != nil {
			// The graph service has already recorded and validated every task
			// transition. Its fan-out batuta-task children are never legacy
			// settlement evidence; the journal graph is the authority even when
			// a failed parent did not retain a run_task output marker.
			graphTokens, reviewTokens, childIDs, err = s.graphParentUsage(ctx, scope.WorkspaceID, delivery, detail)
			if err != nil {
				return err
			}
			if graphTokens > int64(^uint64(0)>>1)-reviewTokens {
				return routing.ErrDeliveryConflict
			}
			tokens = graphTokens + reviewTokens
			failedTaskIDs = graphFallbackTaskIDs(delivery.Graph)
		} else {
			childIDs, failedTaskIDs, tokens, publicationMutation, err = s.settlementEvidence(ctx, scope.WorkspaceID, delivery, detail)
			if err != nil {
				return err
			}
		}
		terminalAt := now
		attempt.State = routing.AttemptTerminal
		attempt.TerminalAt = &terminalAt
		attempt.TerminalStatus = detail.Run.Status
		attempt.ChildRunIDs = childIDs
		attempt.FailedTaskIDs = failedTaskIDs
		attempt.TokensUsed = tokens
		attempt.GraphTokensUsed = graphTokens
		attempt.ReviewTokensUsed = reviewTokens
		attempt.PublicationMutation = publicationMutation
		attempt.WorktreeFingerprint = &routing.WorktreeFingerprint{
			HeadSHA: fingerprint.HeadSHA, PorcelainSHA256: fingerprint.PorcelainSHA256, ContentSHA256: fingerprint.ContentSHA256,
		}
		switch detail.Run.Status {
		case "done", "no-op":
			delivery.State = routing.DeliveryStateDone
		case "failed":
			if delivery.State == routing.DeliveryStateBlocked || delivery.State == routing.DeliveryStateExhausted {
				// terminalize proved and persisted this graph disposition before
				// intentionally failing the parent action. Do not reinterpret that
				// failure as a recoverable or generic parent failure.
			} else if delivery.Graph != nil && len(failedTaskIDs) > 0 {
				delivery.State = routing.DeliveryStateActive
			} else if delivery.Graph != nil && graphHasExhaustedTasks(delivery.Graph) {
				delivery.State = routing.DeliveryStateExhausted
				attempt.BlockerCode = "graph_task_exhausted"
			} else if len(failedTaskIDs) == 0 || publicationMutation {
				delivery.State = routing.DeliveryStateBlocked
				if delivery.Graph != nil {
					attempt.BlockerCode = "non_recoverable_graph_failure"
				} else {
					attempt.BlockerCode = "non_recoverable_failure"
				}
			}
		case "exhausted":
			delivery.State = routing.DeliveryStateExhausted
			attempt.BlockerCode = "compozy_exhausted"
		default:
			delivery.State = routing.DeliveryStateBlocked
			attempt.BlockerCode = "terminal_not_recoverable"
		}
		delivery.Attempts[len(delivery.Attempts)-1] = attempt
		tx.Journal.Deliveries[deliveryID] = delivery
		if err := tx.Persist(); err != nil {
			return err
		}
		result = reconcileResult(delivery, attempt, now)
		return nil
	})
	return result, err
}

func (s deliveryAttemptService) graphParentUsage(
	ctx context.Context,
	workspaceID string,
	delivery routing.DeliveryRecord,
	parent deliveryRunDetail,
) (int64, int64, []string, error) {
	if delivery.Graph == nil {
		return 0, 0, nil, routing.ErrDeliveryConflict
	}
	totalGraphTokens, err := delivery.Graph.CumulativeTokens()
	if err != nil {
		return 0, 0, nil, routing.ErrDeliveryConflict
	}
	accountedGraphTokens := int64(0)
	for _, attempt := range delivery.Attempts {
		if attempt.State != routing.AttemptTerminal {
			continue
		}
		if attempt.GraphTokensUsed < 0 || attempt.GraphTokensUsed > totalGraphTokens-accountedGraphTokens {
			return 0, 0, nil, routing.ErrDeliveryConflict
		}
		accountedGraphTokens += attempt.GraphTokensUsed
	}
	reviewTokens := int64(0)
	childIDs := make([]string, 0, 1)
	seenReview := false
	for _, generation := range parent.Generations {
		for _, output := range generation.Outputs {
			if output.NodeID != "review" || output.ChildLoopRunID == "" {
				continue
			}
			if seenReview || (output.Status != "succeeded" && output.Status != "failed") {
				return 0, 0, nil, routing.ErrDeliveryConflict
			}
			seenReview = true
			child, statusErr := s.Client.Status(ctx, workspaceID, output.ChildLoopRunID)
			if statusErr != nil || child.Run.ID != output.ChildLoopRunID || child.Run.WorkspaceID != workspaceID || child.Run.ParentLoopRunID != parent.Run.ID || child.Run.LoopName != "review-and-fix" || !terminalDeliveryStatus(child.Run.Status) || !child.Run.TokensUsedPresent || child.Run.TokensUsed < 0 || child.Run.TokensUsed > int64(^uint64(0)>>1)-reviewTokens {
				return 0, 0, nil, routing.ErrDeliveryConflict
			}
			succeededChild := child.Run.Status == "done" || child.Run.Status == "no-op"
			if (output.Status == "succeeded") != succeededChild {
				return 0, 0, nil, routing.ErrDeliveryConflict
			}
			reviewTokens += child.Run.TokensUsed
			childIDs = append(childIDs, child.Run.ID)
		}
	}
	return totalGraphTokens - accountedGraphTokens, reviewTokens, childIDs, nil
}

func graphFallbackTaskIDs(graph *routing.DeliveryGraph) []string {
	if graph == nil {
		return nil
	}
	failed := make([]string, 0)
	for _, task := range graph.Tasks {
		if task.State != routing.GraphTaskPreparing || len(task.Attempts) < 2 {
			continue
		}
		previous := task.Attempts[len(task.Attempts)-2]
		current := task.Attempts[len(task.Attempts)-1]
		if previous.State == routing.GraphTaskBlocked && previous.TokensUsed != nil && previous.TerminalStatus != "" &&
			current.State == routing.GraphTaskPreparing && current.Execution == previous.Execution+1 {
			failed = append(failed, task.TaskID)
		}
	}
	return failed
}

func graphHasExhaustedTasks(graph *routing.DeliveryGraph) bool {
	if graph == nil {
		return false
	}
	for _, task := range graph.Tasks {
		if task.State != routing.GraphTaskBlocked || len(task.Attempts) == 0 {
			continue
		}
		if len(task.Attempts) >= routing.MaxTaskExecutions || task.BlockerCode == "integration_conflict_exhausted" {
			return true
		}
	}
	return false
}

func (s deliveryAttemptService) settlementEvidence(
	ctx context.Context,
	workspaceID string,
	delivery routing.DeliveryRecord,
	parent deliveryRunDetail,
) ([]string, []string, int64, bool, error) {
	childIDs := make([]string, 0)
	seenChildren := map[string]struct{}{}
	failed := map[string]struct{}{}
	publicationMutation := false
	tokens := int64(0)
	for _, generation := range parent.Generations {
		for _, output := range generation.Outputs {
			if output.NodeID == "publish" {
				publicationMutation = true
			}
			if output.ChildLoopRunID == "" {
				continue
			}
			if _, duplicate := seenChildren[output.ChildLoopRunID]; duplicate {
				continue
			}
			seenChildren[output.ChildLoopRunID] = struct{}{}
			child, err := s.Client.Status(ctx, workspaceID, output.ChildLoopRunID)
			if err != nil || child.Run.WorkspaceID != workspaceID || (child.Run.LoopName != "implement-tasks" && child.Run.LoopName != "review-and-fix") || !terminalDeliveryStatus(child.Run.Status) {
				return nil, nil, 0, false, routing.ErrDeliveryConflict
			}
			childIDs = append(childIDs, child.Run.ID)
			tokens += child.Run.TokensUsed
			if output.NodeID != "implement" || output.Status != "failed" || child.Run.LoopName != "implement-tasks" || child.Run.Status != "failed" {
				continue
			}
			for _, childGeneration := range child.Generations {
				for _, childOutput := range childGeneration.Outputs {
					if childOutput.NodeID != "execute_task" || childOutput.Status != "failed" {
						continue
					}
					taskID, exists := delivery.TaskSnapshot.ItemTaskIDs[childOutput.ItemIndex]
					if !exists {
						return nil, nil, 0, false, routing.ErrDeliveryConflict
					}
					failed[taskID] = struct{}{}
				}
			}
		}
	}
	failedTaskIDs := make([]string, 0, len(failed))
	for _, task := range delivery.TaskSnapshot.Tasks {
		if _, exists := failed[task.ID]; exists {
			failedTaskIDs = append(failedTaskIDs, task.ID)
		}
	}
	return childIDs, failedTaskIDs, tokens, publicationMutation, nil
}

func runMatchesAttempt(run deliveryRun, delivery routing.DeliveryRecord, attempt routing.DeliveryAttempt) bool {
	return stringInput(run.Inputs, "delivery_id") == delivery.DeliveryID && intInput(run.Inputs, "attempt") == int64(attempt.Attempt) &&
		stringInput(run.Inputs, "slug") == delivery.Slug && stringInput(run.Inputs, "origin_session_id") == delivery.OriginSessionID &&
		stringInput(run.Inputs, "worktree_ref") == delivery.WorktreeID && stringInput(run.Inputs, "routing_generation") == delivery.RoutingGenerationDigest &&
		stringInput(run.Inputs, "absolute_deadline") == delivery.AbsoluteDeadline.Format(time.RFC3339) && intInput(run.Inputs, "token_ceiling") == delivery.TokenCeiling &&
		((attempt.Attempt == 1 && stringInput(run.Inputs, "recovery_operation_id") == "") || stringInput(run.Inputs, "recovery_operation_id") == attempt.OperationID)
}

func terminalDeliveryStatus(status string) bool {
	switch status {
	case "done", "no-op", "blocked", "failed", "exhausted", "stalled", "canceled":
		return true
	default:
		return false
	}
}

func reconcileResult(delivery routing.DeliveryRecord, attempt routing.DeliveryAttempt, now time.Time) RoutingReconcileResult {
	tokens := int64(0)
	for _, current := range delivery.Attempts {
		if current.State == routing.AttemptTerminal {
			tokens += current.TokensUsed
		}
	}
	remainingWall := int(delivery.AbsoluteDeadline.Sub(now) / time.Second)
	if remainingWall < 0 {
		remainingWall = 0
	}
	state := string(attempt.State)
	if attempt.State == routing.AttemptTerminal {
		state = string(delivery.State)
	}
	return RoutingReconcileResult{
		DeliveryID: delivery.DeliveryID, Attempt: attempt.Attempt, DeliveryRunID: attempt.RunID, State: state,
		Recoverable:  delivery.State == routing.DeliveryStateActive && attempt.State == routing.AttemptTerminal && attempt.TerminalStatus == "failed" && len(attempt.FailedTaskIDs) > 0 && !attempt.PublicationMutation,
		AttemptsUsed: len(delivery.Attempts), AttemptsLimit: delivery.AttemptCeiling,
		TokensUsed: tokens, TokensLimit: delivery.TokenCeiling, RemainingWallSec: remainingWall, BlockerCode: attempt.BlockerCode,
	}
}

func matchingRecentRuns(runs []deliveryRun, request deliveryStartRequest, plannedAt time.Time) []deliveryRun {
	threshold := plannedAt.Add(-time.Minute)
	result := make([]deliveryRun, 0, 1)
	for _, run := range runs {
		if run.LoopName != "batuta-deliver" || run.CreatedAt.Before(threshold) || (!run.StartedAt.IsZero() && run.StartedAt.Before(threshold)) || !deliveryRunMatchesRequest(run, request) {
			continue
		}
		result = append(result, run)
	}
	return result
}

func attemptPredecessorMatches(attempts []routing.DeliveryAttempt, attempt int, priorRunID string) bool {
	if attempt != len(attempts) || attempt < 1 {
		return false
	}
	if attempt == 1 {
		return priorRunID == ""
	}
	previous := attempts[attempt-2]
	return previous.State == routing.AttemptTerminal && previous.RunID == priorRunID && priorRunID != ""
}

func routingStartResult(deliveryID string, attempt routing.DeliveryAttempt, replayed bool) RoutingStartResult {
	return RoutingStartResult{DeliveryID: deliveryID, Attempt: attempt.Attempt, OperationID: attempt.OperationID, DeliveryRunID: attempt.RunID, Replayed: replayed}
}

type deliveryFallbackService = deliveryAttemptService
