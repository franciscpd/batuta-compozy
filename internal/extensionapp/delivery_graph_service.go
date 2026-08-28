package extensionapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"path/filepath"
	"reflect"
	"strings"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/integration"
	"github.com/franciscpd/batuta-compozy/internal/publication"
	"github.com/franciscpd/batuta-compozy/internal/routing"
	"github.com/franciscpd/batuta-compozy/internal/worktreeops"
)

type deliveryGraphService struct {
	Store           *routing.OwnershipStore
	StoreForCall    func() (*routing.OwnershipStore, error)
	Worktrees       worktreeops.Client
	WorktreeState   func(context.Context, string) (publication.WorktreeState, error)
	CommitReachable func(context.Context, string, string, string) (bool, error)
	Runs            deliveryGraphRunReader
	Candidates      deliveryGraphCandidateValidator
	Integrator      deliveryGraphIntegrator
	Now             func() time.Time
}

const graphSettlementIntentKind = "batuta.graph_settlement_intent/v1"

var errDeliveryGraphTerminalized = errors.New("batuta: delivery graph terminalized")

type graphSettlementIntent struct {
	Kind     string                         `json:"kind"`
	IntentID string                         `json:"intent_id"`
	Wave     int                            `json:"wave"`
	Request  integration.IntegrationRequest `json:"request"`
}

type deliveryGraphRunReader interface {
	RecentTasks(context.Context, string, int) ([]deliveryRun, error)
	Status(context.Context, string, string) (deliveryRunDetail, error)
}

type deliveryGraphCandidateValidator interface {
	Candidate(context.Context, integration.CandidateRequest) (integration.CandidateEvidence, error)
}

type deliveryGraphIntegrator interface {
	Integrate(context.Context, integration.IntegrationRequest) (integration.IntegrationResult, error)
}

func (s *deliveryGraphService) Execute(
	ctx context.Context,
	scope publication.TrustedScope,
	input DeliveryGraphInput,
) (DeliveryGraphOutput, error) {
	if err := ctx.Err(); err != nil {
		return DeliveryGraphOutput{}, err
	}
	if s == nil {
		return DeliveryGraphOutput{}, errors.New("batuta: delivery graph operation is unavailable")
	}
	switch input.Operation {
	case GraphOpPrepareWave:
		if s.Worktrees == nil || s.WorktreeState == nil {
			return DeliveryGraphOutput{}, errors.New("batuta: delivery graph operation is unavailable")
		}
		return s.prepareWave(ctx, scope, input)
	case GraphOpTaskContext:
		return s.taskContext(ctx, scope, input)
	case GraphOpRecordQuestion:
		if s.Runs == nil {
			return DeliveryGraphOutput{}, errors.New("batuta: delivery graph operation is unavailable")
		}
		return s.recordQuestion(ctx, scope, input)
	case GraphOpRecordAnswer:
		if s.Runs == nil {
			return DeliveryGraphOutput{}, errors.New("batuta: delivery graph operation is unavailable")
		}
		return s.recordAnswer(ctx, scope, input)
	case GraphOpRecordFailure:
		if s.Runs == nil {
			return DeliveryGraphOutput{}, errors.New("batuta: delivery graph operation is unavailable")
		}
		return s.recordFailure(ctx, scope, input)
	case GraphOpRecordCandidate:
		if s.Runs == nil || s.Candidates == nil {
			return DeliveryGraphOutput{}, errors.New("batuta: delivery graph operation is unavailable")
		}
		return s.recordCandidate(ctx, scope, input)
	case GraphOpSettleWave:
		if s.Integrator == nil {
			return DeliveryGraphOutput{}, errors.New("batuta: delivery graph operation is unavailable")
		}
		return s.settleWave(ctx, scope, input)
	case GraphOpCleanup:
		if s.Worktrees == nil || s.WorktreeState == nil || s.CommitReachable == nil {
			return DeliveryGraphOutput{}, errors.New("batuta: delivery graph operation is unavailable")
		}
		return s.cleanup(ctx, scope, input)
	case GraphOpTerminalize:
		return s.terminalize(scope, input)
	default:
		return DeliveryGraphOutput{}, errors.New("batuta: delivery graph operation is unavailable")
	}
}

// terminalize persists only a disposition already proven by graph state, then
// deliberately returns a stable error. The public Loop runtime therefore
// settles the parent through its ordinary failed-action terminal path instead
// of treating a successful transform as a completed generation.
func (s *deliveryGraphService) terminalize(scope publication.TrustedScope, input DeliveryGraphInput) (DeliveryGraphOutput, error) {
	store, err := s.store()
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	now := time.Now().UTC()
	if s.Now != nil {
		now = s.Now().UTC()
	}
	err = store.WithLockedJournal(scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery, exists := tx.Journal.Deliveries[input.DeliveryID]
		if !exists || delivery.WorkspaceID != scope.WorkspaceID || delivery.WorktreeRoot != scope.WorkspaceRoot || delivery.Graph == nil {
			return routing.ErrDeliveryConflict
		}
		proven, proofErr := terminalDispositionProven(delivery, input.TerminalDisposition, now)
		if proofErr != nil || !proven {
			return routing.ErrDeliveryConflict
		}
		wantState := routing.DeliveryStateBlocked
		if input.TerminalDisposition == GraphDispositionExhausted {
			wantState = routing.DeliveryStateExhausted
		}
		if delivery.State == wantState {
			return nil
		}
		if delivery.State != routing.DeliveryStateActive {
			return routing.ErrDeliveryConflict
		}
		delivery.State = wantState
		tx.Journal.Deliveries[input.DeliveryID] = delivery
		return tx.Persist()
	})
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	return DeliveryGraphOutput{}, errDeliveryGraphTerminalized
}

func terminalDispositionProven(delivery routing.DeliveryRecord, disposition GraphDisposition, now time.Time) (bool, error) {
	if delivery.Graph == nil {
		return false, routing.ErrDeliveryConflict
	}
	remainingTokens, remainingWall, err := graphRemainingBudget(delivery, now)
	if err != nil {
		return false, err
	}
	exhausted := remainingTokens <= 0 || remainingWall <= 0
	blocked := false
	for _, task := range delivery.Graph.Tasks {
		if task.State != routing.GraphTaskBlocked {
			continue
		}
		blocked = true
		if len(task.Attempts) >= routing.MaxTaskExecutions || task.BlockerCode == "integration_conflict_exhausted" {
			exhausted = true
		}
	}
	for _, cleanup := range delivery.Graph.Cleanups {
		if cleanup.State != routing.CleanupRemoved {
			blocked = true
		}
	}
	switch disposition {
	case GraphDispositionExhausted:
		return exhausted, nil
	case GraphDispositionBlocked:
		return blocked && !exhausted, nil
	default:
		return false, routing.ErrDeliveryConflict
	}
}

func (s *deliveryGraphService) prepareWave(
	ctx context.Context,
	scope publication.TrustedScope,
	input DeliveryGraphInput,
) (DeliveryGraphOutput, error) {
	store, err := s.store()
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	state, err := s.WorktreeState(ctx, scope.WorkspaceRoot)
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	if state.PorcelainSHA256 != emptyDigest() || state.ContentSHA256 != emptyDigest() {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}

	var wave routing.DeliveryWave
	var generation routing.RoutingGeneration
	var delivery routing.DeliveryRecord
	var remainingTokens int64
	var remainingWall time.Duration
	exhausted := false
	err = store.WithLockedJournal(scope.WorkspaceID, func(tx *routing.JournalTx) error {
		record, exists := tx.Journal.Deliveries[input.DeliveryID]
		if !exists || record.WorkspaceID != scope.WorkspaceID || record.WorktreeRoot != scope.WorkspaceRoot ||
			record.State != routing.DeliveryStateActive || record.Graph == nil {
			return routing.ErrDeliveryConflict
		}
		ownedGeneration, exists := tx.Journal.Generations[record.RoutingGenerationDigest]
		if !exists {
			return routing.ErrOwnershipUnproven
		}
		expectedHead := record.InitialWorktreeFingerprint.HeadSHA
		if len(record.Graph.Integrations) > 0 {
			expectedHead = record.Graph.Integrations[len(record.Graph.Integrations)-1].FinalHeadSHA
		}
		if state.HeadSHA != expectedHead {
			return routing.ErrDeliveryConflict
		}
		remainingTokens, remainingWall, err = graphRemainingBudget(record, s.now())
		if err != nil {
			return err
		}
		if remainingTokens <= 0 || remainingWall <= 0 {
			exhausted = true
			delivery = record
			return nil
		}
		current, exists := activeDeliveryWave(record.Graph)
		if exists {
			wave = current
		} else {
			availableTokens, budgetErr := record.Graph.AvailableTokenBudget(record.TokenCeiling)
			if budgetErr != nil {
				return budgetErr
			}
			remainingSlots := deliveryWaveSlots(availableTokens)
			if remainingSlots == 0 {
				exhausted = true
				remainingTokens = 0
				delivery = record
				return nil
			}
			reachable := make(map[string]bool)
			for _, task := range record.Graph.Tasks {
				if task.State == routing.GraphTaskIntegrated {
					reachable[task.IntegratedCommitSHA] = true
				}
			}
			wave, err = record.Graph.AdmitReadyWave(routing.ReadyWaveInput{
				IntegrationHeadSHA: state.HeadSHA, RemainingSlots: remainingSlots,
				ReachableCommits: reachable,
			})
			if err != nil || len(wave.TaskIDs) == 0 {
				if err != nil {
					return err
				}
				return routing.ErrDependencyBlocked
			}
			if err := record.Graph.BeginWaveAttempts(wave.Number, ownedGeneration); err != nil {
				return err
			}
			if err := record.Graph.ReserveWaveTokens(wave.Number, availableTokens); err != nil {
				return err
			}
			tx.Journal.Deliveries[input.DeliveryID] = record
			if err := tx.Persist(); err != nil {
				return err
			}
		}
		delivery = record
		generation = ownedGeneration
		return nil
	})
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	if exhausted {
		return DeliveryGraphOutput{
			Operation: input.Operation, Disposition: GraphDispositionExhausted,
			DeliveryID: input.DeliveryID, RemainingTokens: max(0, remainingTokens),
			RemainingWallSeconds: max(0, int(remainingWall/time.Second)), BlockerCode: "delivery_budget_exhausted",
		}, nil
	}

	for _, taskID := range wave.TaskIDs {
		task, exists := delivery.Graph.Task(taskID)
		if !exists {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		if task.State != routing.GraphTaskPreparing && task.State != routing.GraphTaskRunning {
			continue
		}
		if err := s.reconcileTaskWorktree(ctx, store, scope, delivery, generation, wave, taskID); err != nil {
			return DeliveryGraphOutput{}, err
		}
	}
	return s.prepareWaveOutput(scope, input, wave)
}

func (s *deliveryGraphService) taskContext(
	ctx context.Context,
	scope publication.TrustedScope,
	input DeliveryGraphInput,
) (DeliveryGraphOutput, error) {
	if err := ctx.Err(); err != nil {
		return DeliveryGraphOutput{}, err
	}
	store, err := s.store()
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	journal, exists, err := store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	delivery, exists := journal.Deliveries[input.DeliveryID]
	if !exists || delivery.WorkspaceID != scope.WorkspaceID || delivery.WorktreeRoot != scope.WorkspaceRoot ||
		delivery.State != routing.DeliveryStateActive || delivery.Graph == nil || input.Wave > len(delivery.Graph.Waves) {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	wave := delivery.Graph.Waves[input.Wave-1]
	if !containsTaskID(wave.TaskIDs, input.TaskID) {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	task, exists := delivery.Graph.Task(input.TaskID)
	if !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	attempt, exists := graphTaskAttemptForRunExecution(task, input.Execution)
	if !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	if len(task.Attempts) == 0 || attempt.Execution != task.Attempts[len(task.Attempts)-1].Execution {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	if attempt.WorktreeID == "" || attempt.WorktreeRoot == "" || attempt.BaseHeadSHA != wave.BaseHeadSHA ||
		(task.State != routing.GraphTaskRunning && task.State != routing.GraphTaskWaitingInput) {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	loader, err := routing.NewArtifactLoader(attempt.WorktreeRoot)
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	set, err := loader.Load(delivery.Slug)
	if err != nil || set.Digest != delivery.TaskSetDigest {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	matched := false
	for _, artifact := range set.Tasks {
		if artifact.ID == input.TaskID && artifact.Domain == task.Domain && artifact.Complexity == task.Complexity {
			matched = true
			break
		}
	}
	if !matched {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	remainingTokens, remainingWall, err := graphRemainingBudget(delivery, s.now())
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	if attempt.TokenAllowance > 0 {
		remainingTokens = attempt.TokenAllowance
	}
	if task.State == routing.GraphTaskRunning && (attempt.TokenAllowance < 1 || remainingWall <= 0) {
		return DeliveryGraphOutput{
			Operation: input.Operation, Disposition: GraphDispositionExhausted,
			DeliveryID: input.DeliveryID, Wave: input.Wave, TaskID: input.TaskID,
			Execution: attempt.Execution, RemainingTokens: max(0, remainingTokens),
			RemainingWallSeconds: max(0, int(remainingWall/time.Second)), BlockerCode: "delivery_budget_exhausted",
		}, nil
	}
	disposition := GraphDispositionTaskReady
	if task.State == routing.GraphTaskWaitingInput {
		disposition = GraphDispositionWaitingInput
	}
	answers := make([]routing.TaskAnswer, 0)
	for _, prior := range task.Attempts {
		if prior.Question != nil && prior.Question.Answer != nil {
			answers = append(answers, *prior.Question.Answer)
		}
	}
	return DeliveryGraphOutput{
		Operation: input.Operation, Disposition: disposition, DeliveryID: input.DeliveryID,
		Wave: input.Wave, TaskID: input.TaskID, Execution: attempt.Execution,
		TaskFile: filepath.ToSlash(filepath.Join(".compozy", "tasks", delivery.Slug, input.TaskID+".md")),
		Runtime:  &attempt.Runtime, Answers: answers, WorktreeID: attempt.WorktreeID,
		WorktreeRoot: attempt.WorktreeRoot, BaseSHA: attempt.BaseHeadSHA,
		RemainingTaskExecutions: routing.MaxTaskExecutions - attempt.Execution,
		RemainingTokens:         remainingTokens, RemainingWallSeconds: int(remainingWall / time.Second),
	}, nil
}

func (s *deliveryGraphService) recordQuestion(
	ctx context.Context,
	scope publication.TrustedScope,
	input DeliveryGraphInput,
) (DeliveryGraphOutput, error) {
	store, err := s.store()
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	journal, exists, err := store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	delivery, task, attempt, wave, err := graphTaskForOperation(journal, scope, input)
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	runExecution := input.Execution
	operationID, err := deriveQuestionOperationID(scope, input)
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	input.Execution = attempt.Execution
	if prior, exists := graphTaskQuestionAttempt(task, runExecution, operationID); exists && prior.Question.Answer != nil {
		input.QuestionOperationID = operationID
		return s.answerReplayOutput(delivery, task, attempt, runExecution, input)
	}
	question := routing.TaskQuestion{
		RequestID: operationID, Prompt: input.Prompt, ContextDigest: canonicalTaskContextDigest(input.TaskID),
		Choices: append([]string(nil), input.Choices...),
	}
	if attempt.Question != nil {
		storedQuestion := *attempt.Question
		storedQuestion.Answer = nil
		if attempt.ChildRunID == "" || !reflect.DeepEqual(&storedQuestion, &question) {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		return s.questionOutput(delivery, input, operationID)
	}
	// An answered question materializes another physical attempt. Durable
	// historical and open-question replays above remain truthful; only a new
	// question at the fourth attempt is refused before liveness I/O.
	if attempt.Execution >= routing.MaxTaskExecutions {
		return DeliveryGraphOutput{}, routing.ErrInvalidDeliveryTransition
	}
	if task.State != routing.GraphTaskRunning || attempt.State != routing.GraphTaskRunning {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	runs, err := s.Runs.RecentTasks(ctx, scope.WorkspaceID, 200)
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	matches := make([]deliveryRun, 0, 1)
	for _, run := range runs {
		if run.Status == "running" && graphTaskRunMatches(run, scope.WorkspaceID, delivery, wave, task, input.Execution) {
			matches = append(matches, run)
		}
	}
	if len(matches) != 1 {
		return DeliveryGraphOutput{}, routing.ErrDeliveryLivenessUnknown
	}
	childRunID := matches[0].ID
	err = store.WithLockedJournal(scope.WorkspaceID, func(tx *routing.JournalTx) error {
		current, exists := tx.Journal.Deliveries[input.DeliveryID]
		if !exists || current.Graph == nil || current.WorkspaceID != scope.WorkspaceID || current.WorktreeRoot != scope.WorkspaceRoot {
			return routing.ErrDeliveryConflict
		}
		if _, err := current.Graph.RecordQuestion(input.TaskID, input.Execution, childRunID, question, s.now()); err != nil {
			return err
		}
		tx.Journal.Deliveries[input.DeliveryID] = current
		return tx.Persist()
	})
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	journal, exists, err = store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	delivery = journal.Deliveries[input.DeliveryID]
	return s.questionOutput(delivery, input, operationID)
}

func (s *deliveryGraphService) recordAnswer(
	ctx context.Context,
	scope publication.TrustedScope,
	input DeliveryGraphInput,
) (DeliveryGraphOutput, error) {
	store, err := s.store()
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	journal, exists, err := store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	delivery, task, attempt, _, err := graphTaskForOperation(journal, scope, input)
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	runExecution := input.Execution
	questionAttempt, exists := graphTaskQuestionAttempt(task, runExecution, input.QuestionOperationID)
	if !exists || questionAttempt.Question == nil || !validOpaqueRunID(questionAttempt.ChildRunID) {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	input.Execution = attempt.Execution
	if questionAttempt.Question.Answer != nil {
		if questionAttempt.Question.Answer.Value != input.Answer {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		return s.answerReplayOutput(delivery, task, attempt, runExecution, input)
	}
	if questionAttempt.Execution != attempt.Execution || task.State != routing.GraphTaskWaitingInput || attempt.State != routing.GraphTaskWaitingInput {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	_, remainingWall, budgetErr := graphRemainingBudget(delivery, s.now())
	if budgetErr != nil {
		return DeliveryGraphOutput{}, budgetErr
	}
	if attempt.TokenAllowance < 1 || remainingWall <= 0 {
		return DeliveryGraphOutput{
			Operation: input.Operation, Disposition: GraphDispositionExhausted,
			DeliveryID: input.DeliveryID, Wave: input.Wave, TaskID: input.TaskID, Execution: input.Execution,
			BaseSHA: attempt.BaseHeadSHA, RemainingTaskExecutions: routing.MaxTaskExecutions - input.Execution,
			RemainingTokens: max(0, attempt.TokenAllowance), RemainingWallSeconds: max(0, int(remainingWall/time.Second)),
			BlockerCode: "delivery_budget_exhausted",
		}, nil
	}
	detail, err := s.Runs.Status(ctx, scope.WorkspaceID, questionAttempt.ChildRunID)
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	request, matched := matchingAnsweredRequest(detail.Requests, questionAttempt.ChildRunID, *questionAttempt.Question)
	if detail.Run.ID != questionAttempt.ChildRunID || detail.Run.Status != "running" || !graphTaskRunMatches(
		detail.Run, scope.WorkspaceID, delivery, delivery.Graph.Waves[input.Wave-1], task, input.Execution,
	) || !matched || !matchingResolvedAskCell(detail.Generations, request, input.Answer) {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	answer := routing.TaskAnswer{
		QuestionOperationID: input.QuestionOperationID, LoopRunID: request.LoopRunID,
		Generation: request.Generation, NodeID: request.NodeID, ItemIndex: request.ItemIndex, Value: input.Answer,
	}
	err = store.WithLockedJournal(scope.WorkspaceID, func(tx *routing.JournalTx) error {
		current, exists := tx.Journal.Deliveries[input.DeliveryID]
		if !exists || current.Graph == nil || current.WorkspaceID != scope.WorkspaceID || current.WorktreeRoot != scope.WorkspaceRoot {
			return routing.ErrDeliveryConflict
		}
		if _, _, err := current.Graph.RecordAnswer(input.TaskID, questionAttempt.Execution, answer, s.now()); err != nil {
			return err
		}
		tx.Journal.Deliveries[input.DeliveryID] = current
		return tx.Persist()
	})
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	return answerOutput(input, attempt.Execution+1), nil
}

func (s *deliveryGraphService) questionOutput(
	delivery routing.DeliveryRecord,
	input DeliveryGraphInput,
	operationID string,
) (DeliveryGraphOutput, error) {
	remainingTokens, remainingWall, err := graphRemainingBudget(delivery, s.now())
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	return DeliveryGraphOutput{
		Operation: input.Operation, Disposition: GraphDispositionWaitingInput,
		DeliveryID: input.DeliveryID, Wave: input.Wave, TaskID: input.TaskID, Execution: input.Execution,
		QuestionOperationID: operationID, RemainingTaskExecutions: routing.MaxTaskExecutions - input.Execution,
		RemainingTokens: remainingTokens, RemainingWallSeconds: int(remainingWall / time.Second),
	}, nil
}

func answerOutput(input DeliveryGraphInput, nextExecution int) DeliveryGraphOutput {
	return DeliveryGraphOutput{
		Operation: input.Operation, Disposition: GraphDispositionTaskReady,
		DeliveryID: input.DeliveryID, Wave: input.Wave, TaskID: input.TaskID,
		Execution: nextExecution, QuestionOperationID: input.QuestionOperationID,
		RemainingTaskExecutions: routing.MaxTaskExecutions - nextExecution,
	}
}

func (s *deliveryGraphService) answerReplayOutput(
	delivery routing.DeliveryRecord,
	task routing.GraphTask,
	attempt routing.GraphTaskAttempt,
	runExecution int,
	input DeliveryGraphInput,
) (DeliveryGraphOutput, error) {
	if graphAttemptRunExecution(attempt) != runExecution {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	current := task.Attempts[len(task.Attempts)-1]
	if graphAttemptRunExecution(current) != runExecution {
		wave, found := graphWaveForBase(delivery.Graph, task.TaskID, current.BaseHeadSHA)
		if !found || task.State != routing.GraphTaskPreparing || current.State != routing.GraphTaskPreparing {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		runtime := current.Runtime
		return DeliveryGraphOutput{
			Operation: input.Operation, Disposition: GraphDispositionPreparing, Replayed: true,
			DeliveryID: input.DeliveryID, Wave: wave.Number, TaskID: input.TaskID,
			Execution: current.Execution, Runtime: &runtime, BaseSHA: current.BaseHeadSHA,
			RemainingTaskExecutions: routing.MaxTaskExecutions - current.Execution,
		}, nil
	}
	if task.State == routing.GraphTaskCandidate && attempt.State == routing.GraphTaskCandidate {
		replayInput := input
		replayInput.Execution = attempt.Execution
		replayInput.BaseSHA = attempt.BaseHeadSHA
		output, err := s.candidateOutput(delivery, replayInput)
		if err != nil {
			return DeliveryGraphOutput{}, err
		}
		output.Replayed = true
		return output, nil
	}
	if task.State == routing.GraphTaskIntegrated && attempt.State == routing.GraphTaskIntegrated {
		disposition := GraphDispositionWaveIntegrated
		if graphAllIntegrated(delivery.Graph) {
			disposition = GraphDispositionAllIntegrated
		}
		return DeliveryGraphOutput{
			Operation: input.Operation, Disposition: disposition, Replayed: true,
			DeliveryID: input.DeliveryID, Wave: input.Wave, TaskID: input.TaskID,
			Execution: attempt.Execution, BaseSHA: graphIntegrationHead(delivery),
			RemainingTaskExecutions: routing.MaxTaskExecutions - attempt.Execution,
		}, nil
	}
	if task.State != routing.GraphTaskRunning {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	output := answerOutput(input, attempt.Execution)
	output.Replayed = true
	return output, nil
}

func graphWaveForBase(graph *routing.DeliveryGraph, taskID, baseSHA string) (routing.DeliveryWave, bool) {
	if graph == nil {
		return routing.DeliveryWave{}, false
	}
	for index := len(graph.Waves) - 1; index >= 0; index-- {
		wave := graph.Waves[index]
		if wave.BaseHeadSHA == baseSHA && containsTaskID(wave.TaskIDs, taskID) {
			return wave, true
		}
	}
	return routing.DeliveryWave{}, false
}

func (s *deliveryGraphService) recordFailure(
	ctx context.Context,
	scope publication.TrustedScope,
	input DeliveryGraphInput,
) (DeliveryGraphOutput, error) {
	store, err := s.store()
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	journal, exists, err := store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	delivery, task, attempt, wave, err := graphTaskForOperation(journal, scope, input)
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	input.Execution = attempt.Execution
	failure := routing.TaskFailure{BlockerCode: input.BlockerCode}
	replay := attempt.State == routing.GraphTaskBlocked
	if replay {
		if attempt.ChildRunID != input.ChildRunID || attempt.TokensUsed == nil {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		failure.ChildRunID = attempt.ChildRunID
		failure.TerminalStatus = attempt.TerminalStatus
		failure.TokensUsed = *attempt.TokensUsed
	} else {
		if task.State != routing.GraphTaskRunning || attempt.State != routing.GraphTaskRunning {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		detail, statusErr := s.Runs.Status(ctx, scope.WorkspaceID, input.ChildRunID)
		if statusErr != nil {
			return DeliveryGraphOutput{}, statusErr
		}
		if detail.Run.ID != input.ChildRunID || !detail.Run.TokensUsedPresent || detail.Run.TokensUsed < 0 ||
			!terminalTaskFailureStatus(detail.Run.Status) ||
			!graphTaskRunMatches(detail.Run, scope.WorkspaceID, delivery, wave, task, input.Execution) {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		failure.ChildRunID = detail.Run.ID
		failure.TerminalStatus = detail.Run.Status
		failure.TokensUsed = detail.Run.TokensUsed
	}

	var result routing.TaskFailureResult
	var remainingTokens int64
	var remainingWall time.Duration
	err = store.WithLockedJournal(scope.WorkspaceID, func(tx *routing.JournalTx) error {
		current, exists := tx.Journal.Deliveries[input.DeliveryID]
		if !exists || current.WorkspaceID != scope.WorkspaceID || current.WorktreeRoot != scope.WorkspaceRoot ||
			current.State != routing.DeliveryStateActive || current.Graph == nil {
			return routing.ErrDeliveryConflict
		}
		generation, exists := tx.Journal.Generations[current.RoutingGenerationDigest]
		if !exists {
			return routing.ErrOwnershipUnproven
		}
		usedBefore, usageErr := current.Graph.CumulativeTokens()
		if usageErr != nil {
			return usageErr
		}
		remainingWall, usageErr = current.RemainingActiveWall(s.now())
		if usageErr != nil {
			return usageErr
		}
		currentTask, found := current.Graph.Task(input.TaskID)
		if !found || input.Execution > len(currentTask.Attempts) {
			return routing.ErrDeliveryConflict
		}
		alreadyRecorded := currentTask.Attempts[input.Execution-1].State == routing.GraphTaskBlocked
		availableBefore, usageErr := current.Graph.AvailableTokenBudget(current.TokenCeiling)
		if usageErr != nil {
			return usageErr
		}
		projectedAvailable := availableBefore
		usedAfter := usedBefore
		if !alreadyRecorded {
			projectedAvailable += currentTask.Attempts[input.Execution-1].TokenAllowance - failure.TokensUsed
			if projectedAvailable < 0 {
				projectedAvailable = 0
			}
			if failure.TokensUsed > current.TokenCeiling-usedBefore {
				usedAfter = current.TokenCeiling
			} else {
				usedAfter += failure.TokensUsed
			}
		}
		remainingTokens = current.TokenCeiling - usedAfter
		if remainingTokens < 0 {
			remainingTokens = 0
		}
		retryAllowed := projectedAvailable > 0 && remainingWall > 0
		result, usageErr = current.Graph.RecordFailure(
			input.TaskID, input.Execution, failure, generation, graphIntegrationHead(current), retryAllowed,
		)
		if usageErr != nil {
			return usageErr
		}
		if !result.Replayed && !result.Blocked {
			if usageErr := current.Graph.ReserveAttemptTokens(
				input.TaskID, input.Execution+1, projectedAvailable,
			); usageErr != nil {
				return usageErr
			}
		}
		if !result.Blocked {
			updatedTask, found := current.Graph.Task(input.TaskID)
			if !found || input.Execution >= len(updatedTask.Attempts) {
				return routing.ErrDeliveryConflict
			}
			remainingTokens = updatedTask.Attempts[input.Execution].TokenAllowance
		}
		if _, usageErr := current.Graph.ReconcileHumanPause(s.now()); usageErr != nil {
			return usageErr
		}
		tx.Journal.Deliveries[input.DeliveryID] = current
		return tx.Persist()
	})
	if err != nil {
		return DeliveryGraphOutput{}, err
	}

	disposition := GraphDispositionPreparing
	execution := input.Execution + 1
	outputWave := result.Wave.Number
	baseSHA := result.Wave.BaseHeadSHA
	var runtime *routing.RuntimeValue
	blockerCode := ""
	if result.Blocked {
		disposition = GraphDispositionBlocked
		if remainingTokens == 0 || remainingWall <= 0 || input.Execution == routing.MaxTaskExecutions {
			disposition = GraphDispositionExhausted
		}
		execution = input.Execution
		outputWave = input.Wave
		baseSHA = attempt.BaseHeadSHA
		blockerCode = input.BlockerCode
	} else {
		runtimeValue := result.Runtime
		runtime = &runtimeValue
	}
	return DeliveryGraphOutput{
		Operation: input.Operation, Disposition: disposition, DeliveryID: input.DeliveryID,
		Wave: outputWave, TaskID: input.TaskID, Execution: execution, Runtime: runtime, BaseSHA: baseSHA,
		RemainingTaskExecutions: routing.MaxTaskExecutions - execution,
		RemainingTokens:         remainingTokens, RemainingWallSeconds: max(0, int(remainingWall/time.Second)),
		BlockerCode: blockerCode,
	}, nil
}

func (s *deliveryGraphService) recordCandidate(
	ctx context.Context,
	scope publication.TrustedScope,
	input DeliveryGraphInput,
) (DeliveryGraphOutput, error) {
	store, err := s.store()
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	journal, exists, err := store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	delivery, task, attempt, wave, err := graphTaskForOperation(journal, scope, input)
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	input.Execution = attempt.Execution
	if attempt.State == routing.GraphTaskCandidate || attempt.State == routing.GraphTaskIntegrated {
		if !candidateReplayMatches(input, attempt) {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		input.BaseSHA = attempt.BaseHeadSHA
		input.CommitSHA = attempt.CandidateCommitSHA
		input.VerificationDigest = attempt.VerificationDigest
		if attempt.CandidateEvidence == nil {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		input.Verification = append(json.RawMessage(nil), attempt.CandidateEvidence.Verification...)
		if !validTaskVerification(input.Verification, input.VerificationDigest, input.TaskID) {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		evidence := routing.TaskCandidate{
			ChildRunID: input.ChildRunID, BaseHeadSHA: input.BaseSHA, CommitSHA: input.CommitSHA,
			VerificationDigest: input.VerificationDigest,
		}
		if attempt.TokensUsed == nil {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		evidence.TokensUsed = *attempt.TokensUsed
		evidence.Evidence = attempt.CandidateEvidence
		if _, err := delivery.Graph.RecordCandidate(input.TaskID, input.Execution, evidence); err != nil {
			return DeliveryGraphOutput{}, err
		}
		return s.candidateOutput(delivery, input)
	}
	if task.State != routing.GraphTaskRunning || attempt.State != routing.GraphTaskRunning ||
		attempt.WorktreeRoot == "" {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	if input.hasCandidateFields() && input.BaseSHA != attempt.BaseHeadSHA {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	detail, err := s.Runs.Status(ctx, scope.WorkspaceID, input.ChildRunID)
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	derived, ok := deriveCompletedTaskCandidate(detail, input.TaskID)
	if !ok || derived.Execution != attempt.Execution ||
		(input.hasCandidateFields() && !candidateFieldsEqual(input, derived)) {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	input.BaseSHA = attempt.BaseHeadSHA
	input.CommitSHA = derived.CommitSHA
	input.Verification = derived.Verification
	input.VerificationDigest = derived.VerificationDigest
	evidence := routing.TaskCandidate{
		ChildRunID: input.ChildRunID, BaseHeadSHA: input.BaseSHA, CommitSHA: input.CommitSHA,
		VerificationDigest: input.VerificationDigest,
	}
	if detail.Run.ID != input.ChildRunID || (detail.Run.Status != "done" && detail.Run.Status != "no-op") ||
		!detail.Run.TokensUsedPresent || detail.Run.TokensUsed < 0 ||
		!graphTaskRunMatches(detail.Run, scope.WorkspaceID, delivery, wave, task, input.Execution) {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	if attempt.TokenAllowance > 0 && detail.Run.TokensUsed > attempt.TokenAllowance {
		return DeliveryGraphOutput{}, routing.ErrNoEligibleCandidate
	}
	identity, err := worktreeops.DeriveIdentity(worktreeops.IdentityInput{
		WorkspaceID: scope.WorkspaceID, DeliveryID: delivery.DeliveryID, Wave: wave.Number,
		Slug: delivery.Slug, TaskID: task.TaskID, Execution: graphAttemptRunExecution(attempt), BaseSHA: wave.BaseHeadSHA,
	})
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	expectedBranch := identity.Branch
	if attempt.WorktreeIntent != nil {
		expectedBranch = attempt.WorktreeIntent.Branch
	}
	request := integration.CandidateRequest{
		TaskID: task.TaskID, Slug: delivery.Slug, WorktreeRoot: attempt.WorktreeRoot,
		RepositoryRoot: scope.WorkspaceRoot, ExpectedBranch: expectedBranch, BaseSHA: attempt.BaseHeadSHA,
		Verification: append([]byte(nil), input.Verification...), VerificationDigest: input.VerificationDigest,
		AllowedTrackingPaths: []string{filepath.ToSlash(filepath.Join(".compozy", "tasks", delivery.Slug, task.TaskID+".md"))},
	}
	validated, err := s.Candidates.Candidate(ctx, request)
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	if validated.TaskID != request.TaskID || validated.Slug != request.Slug ||
		validated.WorktreeRoot != request.WorktreeRoot || validated.Branch != request.ExpectedBranch ||
		validated.BaseSHA != input.BaseSHA || validated.CommitSHA != input.CommitSHA ||
		validated.VerificationDigest != input.VerificationDigest {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	evidence.TokensUsed = detail.Run.TokensUsed
	evidence.Evidence = routingCandidateEvidence(validated, input.Verification)
	err = store.WithLockedJournal(scope.WorkspaceID, func(tx *routing.JournalTx) error {
		current, exists := tx.Journal.Deliveries[input.DeliveryID]
		if !exists || current.WorkspaceID != scope.WorkspaceID || current.WorktreeRoot != scope.WorkspaceRoot || current.Graph == nil {
			return routing.ErrDeliveryConflict
		}
		if _, err := current.Graph.RecordCandidate(input.TaskID, input.Execution, evidence); err != nil {
			return err
		}
		if _, err := current.Graph.ReconcileHumanPause(s.now()); err != nil {
			return err
		}
		tx.Journal.Deliveries[input.DeliveryID] = current
		return tx.Persist()
	})
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	journal, exists, err = store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	return s.candidateOutput(journal.Deliveries[input.DeliveryID], input)
}

func (s *deliveryGraphService) candidateOutput(
	delivery routing.DeliveryRecord,
	input DeliveryGraphInput,
) (DeliveryGraphOutput, error) {
	remainingTokens, remainingWall, err := graphRemainingBudget(delivery, s.now())
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	return DeliveryGraphOutput{
		Operation: input.Operation, Disposition: GraphDispositionCandidateRecorded,
		DeliveryID: input.DeliveryID, Wave: input.Wave, TaskID: input.TaskID, Execution: input.Execution,
		BaseSHA: input.BaseSHA, RemainingTaskExecutions: routing.MaxTaskExecutions - input.Execution,
		RemainingTokens: remainingTokens, RemainingWallSeconds: int(remainingWall / time.Second),
	}, nil
}

func (s *deliveryGraphService) settleWave(
	ctx context.Context,
	scope publication.TrustedScope,
	input DeliveryGraphInput,
) (DeliveryGraphOutput, error) {
	store, err := s.store()
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	journal, exists, err := store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	delivery, exists := journal.Deliveries[input.DeliveryID]
	if !exists || delivery.WorkspaceID != scope.WorkspaceID || delivery.WorktreeRoot != scope.WorkspaceRoot ||
		delivery.State != routing.DeliveryStateActive || delivery.Graph == nil ||
		input.Wave < 1 || input.Wave > len(delivery.Graph.Waves) {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	wave := delivery.Graph.Waves[input.Wave-1]
	request, planned, err := pendingGraphSettlementRequest(journal, scope, delivery, wave)
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	if !planned {
		candidates, replayOperationID, candidateErr := waveSettlementCandidates(delivery, wave)
		if candidateErr != nil {
			return DeliveryGraphOutput{}, candidateErr
		}
		if replayOperationID != "" {
			return s.settlementOutput(delivery, input, replayOperationID)
		}
		if len(candidates) == 0 {
			if operationID, complete := completedWaveOperation(delivery.Graph, wave); complete {
				return s.settlementOutput(delivery, input, operationID)
			}
			return DeliveryGraphOutput{}, routing.ErrDeliveryLivenessUnknown
		}
		startingHead := graphIntegrationHead(delivery)
		operationID, requestDigest, identityErr := deriveIntegrationIdentity(scope, delivery, wave, startingHead, candidates)
		if identityErr != nil {
			return DeliveryGraphOutput{}, identityErr
		}
		request = integration.IntegrationRequest{
			WorkspaceID: scope.WorkspaceID, DeliveryID: delivery.DeliveryID,
			OperationID: operationID, RequestDigest: requestDigest,
			IntegrationRoot: scope.WorkspaceRoot, StartingHeadSHA: startingHead, Candidates: candidates,
		}
		if err := persistGraphSettlementIntent(store, scope, delivery, wave, request); err != nil {
			return DeliveryGraphOutput{}, err
		}
	}
	result, err := s.Integrator.Integrate(ctx, request)
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	if !result.Complete || result.OperationID != request.OperationID || result.RequestDigest != request.RequestDigest ||
		len(result.AcceptedTaskIDs) != len(result.AcceptedCommitSHAs) ||
		len(result.AcceptedTaskIDs) != len(result.IntegratedCommitSHAs) {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	orderedTaskIDs := make([]string, len(request.Candidates))
	candidateCommitSHAs := make([]string, len(request.Candidates))
	for index, candidate := range request.Candidates {
		orderedTaskIDs[index] = candidate.TaskID
		candidateCommitSHAs[index] = candidate.CommitSHA
	}
	settlement := routing.WaveSettlement{
		OperationID: request.OperationID, RequestDigest: request.RequestDigest, Wave: input.Wave,
		StartingHeadSHA: request.StartingHeadSHA, OrderedTaskIDs: orderedTaskIDs, CandidateCommitSHAs: candidateCommitSHAs,
		AcceptedTaskIDs:      append([]string(nil), result.AcceptedTaskIDs...),
		AcceptedCommitSHAs:   append([]string(nil), result.AcceptedCommitSHAs...),
		IntegratedCommitSHAs: append([]string(nil), result.IntegratedCommitSHAs...),
		FirstConflictTaskID:  result.FirstConflictTaskID, ConflictEvidenceDigest: result.ConflictEvidenceDigest,
		FinalHeadSHA: result.ResultingHeadSHA,
	}
	err = store.WithLockedJournal(scope.WorkspaceID, func(tx *routing.JournalTx) error {
		current, exists := tx.Journal.Deliveries[input.DeliveryID]
		if !exists || current.WorkspaceID != scope.WorkspaceID || current.WorktreeRoot != scope.WorkspaceRoot || current.Graph == nil {
			return routing.ErrDeliveryConflict
		}
		generation, exists := tx.Journal.Generations[current.RoutingGenerationDigest]
		if !exists {
			return routing.ErrOwnershipUnproven
		}
		availableTokens, settleErr := current.Graph.AvailableTokenBudget(current.TokenCeiling)
		if settleErr != nil {
			return settleErr
		}
		remainingWall, settleErr := current.RemainingActiveWall(s.now())
		if settleErr != nil {
			return settleErr
		}
		settled, settleErr := current.Graph.SettleWave(
			settlement, generation, availableTokens > 0 && remainingWall > 0,
		)
		if settleErr != nil {
			return settleErr
		}
		if settled.Disposition == routing.SettlementReexecuteConflict && !settled.Replayed {
			conflicted, found := current.Graph.Task(settled.TaskID)
			if !found || len(conflicted.Attempts) == 0 {
				return routing.ErrDeliveryConflict
			}
			if settleErr := current.Graph.ReserveAttemptTokens(
				settled.TaskID, conflicted.Attempts[len(conflicted.Attempts)-1].Execution, availableTokens,
			); settleErr != nil {
				return settleErr
			}
		}
		if _, settleErr := current.Graph.ReconcileHumanPause(s.now()); settleErr != nil {
			return settleErr
		}
		tx.Journal.Deliveries[input.DeliveryID] = current
		return tx.Persist()
	})
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	journal, exists, err = store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	return s.settlementOutput(journal.Deliveries[input.DeliveryID], input, request.OperationID)
}

func waveSettlementCandidates(
	delivery routing.DeliveryRecord,
	wave routing.DeliveryWave,
) ([]integration.CandidateEvidence, string, error) {
	if delivery.Graph == nil {
		return nil, "", routing.ErrDeliveryConflict
	}
	candidates := make([]integration.CandidateEvidence, 0, len(wave.TaskIDs))
	for _, taskID := range wave.TaskIDs {
		task, found := delivery.Graph.Task(taskID)
		if !found || len(task.Attempts) == 0 {
			return nil, "", routing.ErrDeliveryConflict
		}
		attempt := task.Attempts[len(task.Attempts)-1]
		if task.State == routing.GraphTaskIntegrated || task.State == routing.GraphTaskBlocked {
			continue
		}
		if attempt.BaseHeadSHA != wave.BaseHeadSHA {
			operationID := unresolvedConflictOperation(task)
			if operationID == "" {
				return nil, "", routing.ErrDeliveryConflict
			}
			return nil, operationID, nil
		}
		if task.State != routing.GraphTaskCandidate {
			continue
		}
		candidate, err := integrationCandidateEvidence(task, attempt)
		if err != nil {
			return nil, "", err
		}
		candidates = append(candidates, candidate)
	}
	return candidates, "", nil
}

func persistGraphSettlementIntent(
	store *routing.OwnershipStore,
	scope publication.TrustedScope,
	delivery routing.DeliveryRecord,
	wave routing.DeliveryWave,
	request integration.IntegrationRequest,
) error {
	intentID := graphSettlementIntentID(request.OperationID)
	intent := graphSettlementIntent{Kind: graphSettlementIntentKind, IntentID: intentID, Wave: wave.Number, Request: request}
	payload, err := json.Marshal(intent)
	if err != nil {
		return routing.ErrDeliveryConflict
	}
	return store.WithLockedJournal(scope.WorkspaceID, func(tx *routing.JournalTx) error {
		current, exists := tx.Journal.Deliveries[delivery.DeliveryID]
		if !exists || current.WorkspaceID != scope.WorkspaceID || current.WorktreeRoot != scope.WorkspaceRoot ||
			current.Graph == nil || wave.Number < 1 || wave.Number > len(current.Graph.Waves) {
			return routing.ErrDeliveryConflict
		}
		currentWave := current.Graph.Waves[wave.Number-1]
		candidates, replayOperationID, err := waveSettlementCandidates(current, currentWave)
		if err != nil || replayOperationID != "" {
			return routing.ErrDeliveryConflict
		}
		startingHead := graphIntegrationHead(current)
		operationID, requestDigest, err := deriveIntegrationIdentity(scope, current, currentWave, startingHead, candidates)
		if err != nil || operationID != request.OperationID || requestDigest != request.RequestDigest ||
			startingHead != request.StartingHeadSHA || !reflect.DeepEqual(candidates, request.Candidates) {
			return routing.ErrDeliveryConflict
		}
		if existing, found := tx.Journal.IntegrationStates[intentID]; found {
			if !bytes.Equal(existing, payload) {
				return routing.ErrDeliveryConflict
			}
			return nil
		}
		if tx.Journal.IntegrationStates == nil {
			tx.Journal.IntegrationStates = map[string]json.RawMessage{}
		}
		tx.Journal.IntegrationStates[intentID] = append(json.RawMessage(nil), payload...)
		return tx.Persist()
	})
}

func pendingGraphSettlementRequest(
	journal routing.RoutingJournal,
	scope publication.TrustedScope,
	delivery routing.DeliveryRecord,
	wave routing.DeliveryWave,
) (integration.IntegrationRequest, bool, error) {
	completed := make(map[string]struct{}, len(delivery.Graph.Integrations))
	for _, operation := range delivery.Graph.Integrations {
		completed[operation.OperationID] = struct{}{}
	}
	var matched *integration.IntegrationRequest
	for key, payload := range journal.IntegrationStates {
		var header struct {
			Kind string `json:"kind"`
		}
		if json.Unmarshal(payload, &header) != nil || header.Kind != graphSettlementIntentKind {
			continue
		}
		var intent graphSettlementIntent
		decoder := json.NewDecoder(bytes.NewReader(payload))
		decoder.DisallowUnknownFields()
		if decoder.Decode(&intent) != nil || decoder.Decode(&struct{}{}) != io.EOF || intent.Kind != graphSettlementIntentKind ||
			intent.IntentID != key || graphSettlementIntentID(intent.Request.OperationID) != key {
			return integration.IntegrationRequest{}, false, routing.ErrDeliveryConflict
		}
		if intent.Request.DeliveryID != delivery.DeliveryID {
			continue
		}
		if intent.Wave != wave.Number {
			continue
		}
		if _, exists := completed[intent.Request.OperationID]; exists {
			continue
		}
		operationID, requestDigest, err := deriveIntegrationIdentity(
			scope, delivery, wave, intent.Request.StartingHeadSHA, intent.Request.Candidates,
		)
		if err != nil || operationID != intent.Request.OperationID || requestDigest != intent.Request.RequestDigest ||
			intent.Request.WorkspaceID != scope.WorkspaceID || intent.Request.IntegrationRoot != scope.WorkspaceRoot ||
			intent.Request.StartingHeadSHA != graphIntegrationHead(delivery) {
			return integration.IntegrationRequest{}, false, routing.ErrDeliveryConflict
		}
		if matched != nil {
			return integration.IntegrationRequest{}, false, routing.ErrDeliveryConflict
		}
		request := intent.Request
		request.Candidates = append([]integration.CandidateEvidence(nil), intent.Request.Candidates...)
		matched = &request
	}
	if matched == nil {
		return integration.IntegrationRequest{}, false, nil
	}
	return *matched, true, nil
}

func graphSettlementIntentID(operationID string) string {
	digest := sha256.Sum256([]byte("graph-settlement-intent\x00" + operationID))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func unresolvedConflictOperation(task routing.GraphTask) string {
	for index := len(task.Attempts) - 1; index >= 0; index-- {
		if task.Attempts[index].Conflict != nil {
			return task.Attempts[index].Conflict.IntegrationOperationID
		}
	}
	return ""
}

func completedWaveOperation(graph *routing.DeliveryGraph, wave routing.DeliveryWave) (string, bool) {
	if graph == nil {
		return "", false
	}
	for _, taskID := range wave.TaskIDs {
		task, found := graph.Task(taskID)
		if !found || task.State != routing.GraphTaskIntegrated {
			return "", false
		}
	}
	for index := len(graph.Integrations) - 1; index >= 0; index-- {
		if graph.Integrations[index].Wave == wave.Number {
			return graph.Integrations[index].OperationID, true
		}
	}
	return "", false
}

func (s *deliveryGraphService) settlementOutput(
	delivery routing.DeliveryRecord,
	input DeliveryGraphInput,
	operationID string,
) (DeliveryGraphOutput, error) {
	var operation *routing.IntegrationOperation
	for index := range delivery.Graph.Integrations {
		if delivery.Graph.Integrations[index].OperationID == operationID {
			operation = &delivery.Graph.Integrations[index]
			break
		}
	}
	if operation == nil {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	disposition := GraphDispositionWaveIntegrated
	output := DeliveryGraphOutput{
		Operation: input.Operation, DeliveryID: input.DeliveryID, Wave: input.Wave,
		IntegrationOperationID: operationID, BaseSHA: operation.FinalHeadSHA,
	}
	if operation.ConflictingTaskID != "" {
		task, exists := delivery.Graph.Task(operation.ConflictingTaskID)
		if !exists || len(task.Attempts) == 0 {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		output.TaskID = task.TaskID
		conflictIndex := -1
		for index := range task.Attempts {
			proof := task.Attempts[index].Conflict
			if proof != nil && proof.IntegrationOperationID == operationID {
				conflictIndex = index
				break
			}
		}
		if conflictIndex < 0 {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		conflict := task.Attempts[conflictIndex]
		if conflictIndex+1 < len(task.Attempts) {
			next := task.Attempts[conflictIndex+1]
			disposition = GraphDispositionReexecuteConflict
			output.Execution = next.Execution
			output.BaseSHA = next.BaseHeadSHA
			runtime := next.Runtime
			output.Runtime = &runtime
		} else if conflict.State == routing.GraphTaskBlocked && conflict.BlockerCode != "" {
			disposition = GraphDispositionBlocked
			output.Execution = conflict.Execution
			if conflict.BlockerCode == "integration_conflict_exhausted" {
				disposition = GraphDispositionExhausted
			}
			output.BlockerCode = conflict.BlockerCode
		} else {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
	} else if graphAllIntegrated(delivery.Graph) {
		disposition = GraphDispositionAllIntegrated
	}
	remainingTokens, remainingWall, err := graphRemainingBudget(delivery, s.now())
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	output.Disposition = disposition
	output.RemainingTokens = remainingTokens
	output.RemainingWallSeconds = int(remainingWall / time.Second)
	if output.Execution > 0 {
		output.RemainingTaskExecutions = routing.MaxTaskExecutions - output.Execution
	}
	return output, nil
}

func (s *deliveryGraphService) cleanup(
	ctx context.Context,
	scope publication.TrustedScope,
	input DeliveryGraphInput,
) (DeliveryGraphOutput, error) {
	store, err := s.store()
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	journal, exists, err := store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	delivery, exists := journal.Deliveries[input.DeliveryID]
	if !exists || delivery.WorkspaceID != scope.WorkspaceID || delivery.WorktreeRoot != scope.WorkspaceRoot || delivery.Graph == nil {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	type cleanupTarget struct {
		task            routing.GraphTask
		identityAttempt routing.GraphTaskAttempt
		evidenceAttempt routing.GraphTaskAttempt
	}
	targets := make([]cleanupTarget, 0)
	byWorktree := make(map[string]int)
	for _, task := range delivery.Graph.Tasks {
		for _, attempt := range task.Attempts {
			if attempt.WorktreeID == "" {
				continue
			}
			if index, duplicate := byWorktree[attempt.WorktreeID]; duplicate {
				targets[index].evidenceAttempt = attempt
				continue
			}
			byWorktree[attempt.WorktreeID] = len(targets)
			targets = append(targets, cleanupTarget{task: task, identityAttempt: attempt, evidenceAttempt: attempt})
		}
	}
	for _, target := range targets {
		if err := s.cleanupAttempt(
			ctx, store, scope, delivery, target.task, target.identityAttempt, target.evidenceAttempt,
		); err != nil {
			return DeliveryGraphOutput{}, err
		}
	}
	journal, exists, err = store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	delivery = journal.Deliveries[input.DeliveryID]
	output := DeliveryGraphOutput{
		Operation: input.Operation, Disposition: GraphDispositionCleaned, DeliveryID: input.DeliveryID,
		CleanupResults: make([]DeliveryGraphCleanup, 0, len(delivery.Graph.Cleanups)),
	}
	for _, operation := range delivery.Graph.Cleanups {
		output.CleanupResults = append(output.CleanupResults, DeliveryGraphCleanup{
			OperationID: operation.OperationID, TaskID: operation.TaskID, Execution: operation.Execution,
			WorktreeID: operation.WorktreeID, State: string(operation.State), BlockerCode: operation.BlockerCode,
		})
		if operation.State != routing.CleanupRemoved {
			output.Disposition = GraphDispositionBlocked
			if output.BlockerCode == "" {
				output.BlockerCode = operation.BlockerCode
			}
		}
	}
	return output, nil
}

func (s *deliveryGraphService) cleanupAttempt(
	ctx context.Context,
	store *routing.OwnershipStore,
	scope publication.TrustedScope,
	delivery routing.DeliveryRecord,
	task routing.GraphTask,
	identityAttempt routing.GraphTaskAttempt,
	evidenceAttempt routing.GraphTaskAttempt,
) error {
	operation, err := deriveCleanupOperation(scope, delivery, task, identityAttempt)
	if err != nil {
		return err
	}
	for _, existing := range delivery.Graph.Cleanups {
		if existing.OperationID != operation.OperationID {
			continue
		}
		if existing.State == routing.CleanupRemoved || existing.State == routing.CleanupRetained {
			return nil
		}
		operation = existing
		break
	}
	if operation.State == "" {
		operation.State = routing.CleanupPlanned
	}
	if task.State != routing.GraphTaskIntegrated {
		operation.State = routing.CleanupRetained
		operation.BlockerCode = "task_not_integrated"
		return s.persistCleanup(store, scope.WorkspaceID, delivery.DeliveryID, operation)
	}
	wave, found := graphWaveForAttempt(delivery.Graph, task, identityAttempt.Execution)
	if !found {
		return routing.ErrDeliveryConflict
	}
	identity, err := worktreeops.DeriveIdentity(worktreeops.IdentityInput{
		WorkspaceID: scope.WorkspaceID, DeliveryID: delivery.DeliveryID, Wave: wave.Number,
		Slug: delivery.Slug, TaskID: task.TaskID, Execution: identityAttempt.Execution, BaseSHA: identityAttempt.BaseHeadSHA,
	})
	if err != nil {
		return err
	}
	worktree, err := s.Worktrees.Inspect(ctx, scope, identityAttempt.WorktreeID)
	if err != nil {
		return err
	}
	if worktree.ID != identityAttempt.WorktreeID || worktree.Root != identityAttempt.WorktreeRoot || worktree.Name != identity.Name ||
		worktree.Branch != identity.Branch || worktree.WorkspaceID != scope.WorkspaceID ||
		worktree.RepositoryRoot != scope.WorkspaceRoot || worktree.BaseSHA != identityAttempt.BaseHeadSHA ||
		worktree.BaseRef != identityAttempt.BaseHeadSHA {
		return worktreeops.ErrInvalidWorktreeIdentity
	}
	if worktree.State == "removed" {
		if operation.State == routing.CleanupPlanned {
			return s.completeCleanup(store, scope.WorkspaceID, delivery.DeliveryID, operation.OperationID, routing.CleanupRemoved, "")
		}
		return routing.ErrDeliveryConflict
	}
	if worktree.State != "ready" || worktree.Setup.State != "ok" {
		operation.State = routing.CleanupRetained
		operation.BlockerCode = "worktree_not_ready"
		return s.persistCleanup(store, scope.WorkspaceID, delivery.DeliveryID, operation)
	}
	state, err := s.WorktreeState(ctx, worktree.Root)
	if err != nil {
		return err
	}
	expectedHead := evidenceAttempt.BaseHeadSHA
	if evidenceAttempt.CandidateCommitSHA != "" {
		expectedHead = evidenceAttempt.CandidateCommitSHA
	}
	if state.HeadSHA != expectedHead || state.PorcelainSHA256 != emptyDigest() || state.ContentSHA256 != emptyDigest() {
		operation.State = routing.CleanupRetained
		operation.BlockerCode = "worktree_evidence_changed"
		return s.persistCleanup(store, scope.WorkspaceID, delivery.DeliveryID, operation)
	}
	reachable, err := s.CommitReachable(
		ctx, scope.WorkspaceRoot, task.IntegratedCommitSHA, graphIntegrationHead(delivery),
	)
	if err != nil {
		return err
	}
	if !reachable {
		operation.State = routing.CleanupRetained
		operation.BlockerCode = "integrated_commit_unreachable"
		return s.persistCleanup(store, scope.WorkspaceID, delivery.DeliveryID, operation)
	}
	if operation.State != routing.CleanupPlanned {
		return routing.ErrDeliveryConflict
	}
	if err := s.persistCleanup(store, scope.WorkspaceID, delivery.DeliveryID, operation); err != nil {
		return err
	}
	return store.WithLockedJournal(scope.WorkspaceID, func(tx *routing.JournalTx) error {
		current, exists := tx.Journal.Deliveries[delivery.DeliveryID]
		if !exists || current.Graph == nil || graphIntegrationHead(current) != graphIntegrationHead(delivery) {
			return routing.ErrDeliveryConflict
		}
		currentTask, found := current.Graph.Task(task.TaskID)
		if !found || currentTask.State != routing.GraphTaskIntegrated ||
			currentTask.IntegratedCommitSHA != task.IntegratedCommitSHA {
			return routing.ErrDeliveryConflict
		}
		currentWorktree, inspectErr := s.Worktrees.Inspect(ctx, scope, identityAttempt.WorktreeID)
		if inspectErr != nil {
			return inspectErr
		}
		currentState, stateErr := s.WorktreeState(ctx, currentWorktree.Root)
		if stateErr != nil {
			return stateErr
		}
		stillReachable, reachErr := s.CommitReachable(
			ctx, scope.WorkspaceRoot, currentTask.IntegratedCommitSHA, graphIntegrationHead(current),
		)
		if reachErr != nil {
			return reachErr
		}
		if currentWorktree.ID != identityAttempt.WorktreeID || currentWorktree.Root != identityAttempt.WorktreeRoot ||
			currentWorktree.Name != identity.Name || currentWorktree.Branch != identity.Branch || currentWorktree.State != "ready" ||
			currentWorktree.WorkspaceID != scope.WorkspaceID || currentWorktree.RepositoryRoot != scope.WorkspaceRoot ||
			currentWorktree.BaseSHA != identityAttempt.BaseHeadSHA || currentWorktree.BaseRef != identityAttempt.BaseHeadSHA ||
			currentWorktree.Setup.State != "ok" || currentState.HeadSHA != expectedHead ||
			currentState.PorcelainSHA256 != emptyDigest() || currentState.ContentSHA256 != emptyDigest() || !stillReachable {
			if _, _, transitionErr := current.Graph.CompleteCleanup(
				operation.OperationID, routing.CleanupRetained, "worktree_evidence_changed",
			); transitionErr != nil {
				return transitionErr
			}
			tx.Journal.Deliveries[delivery.DeliveryID] = current
			return tx.Persist()
		}
		removed, removeErr := s.Worktrees.Remove(ctx, scope, identityAttempt.WorktreeID)
		if removeErr != nil {
			return removeErr
		}
		if removed.ID != identityAttempt.WorktreeID || removed.State != "removed" || removed.WorkspaceID != scope.WorkspaceID {
			return worktreeops.ErrInvalidWorktreeIdentity
		}
		if _, _, transitionErr := current.Graph.CompleteCleanup(
			operation.OperationID, routing.CleanupRemoved, "",
		); transitionErr != nil {
			return transitionErr
		}
		tx.Journal.Deliveries[delivery.DeliveryID] = current
		return tx.Persist()
	})
}

func (s *deliveryGraphService) persistCleanup(
	store *routing.OwnershipStore,
	workspaceID string,
	deliveryID string,
	operation routing.CleanupOperation,
) error {
	return store.WithLockedJournal(workspaceID, func(tx *routing.JournalTx) error {
		delivery, exists := tx.Journal.Deliveries[deliveryID]
		if !exists || delivery.Graph == nil {
			return routing.ErrDeliveryConflict
		}
		if _, err := delivery.Graph.RecordCleanup(operation); err != nil {
			return err
		}
		tx.Journal.Deliveries[deliveryID] = delivery
		return tx.Persist()
	})
}

func (s *deliveryGraphService) completeCleanup(
	store *routing.OwnershipStore,
	workspaceID string,
	deliveryID string,
	operationID string,
	state routing.CleanupState,
	blockerCode string,
) error {
	return store.WithLockedJournal(workspaceID, func(tx *routing.JournalTx) error {
		delivery, exists := tx.Journal.Deliveries[deliveryID]
		if !exists || delivery.Graph == nil {
			return routing.ErrDeliveryConflict
		}
		if _, _, err := delivery.Graph.CompleteCleanup(operationID, state, blockerCode); err != nil {
			return err
		}
		tx.Journal.Deliveries[deliveryID] = delivery
		return tx.Persist()
	})
}

func (s *deliveryGraphService) reconcileTaskWorktree(
	ctx context.Context,
	store *routing.OwnershipStore,
	scope publication.TrustedScope,
	delivery routing.DeliveryRecord,
	_ routing.RoutingGeneration,
	wave routing.DeliveryWave,
	taskID string,
) error {
	journal, exists, err := store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		return routing.ErrDeliveryConflict
	}
	current := journal.Deliveries[delivery.DeliveryID]
	task, exists := current.Graph.Task(taskID)
	if !exists || len(task.Attempts) == 0 {
		return routing.ErrDeliveryConflict
	}
	attempt := task.Attempts[len(task.Attempts)-1]
	runExecution := graphAttemptRunExecution(attempt)
	identity, err := worktreeops.DeriveIdentity(worktreeops.IdentityInput{
		WorkspaceID: scope.WorkspaceID, DeliveryID: delivery.DeliveryID, Wave: wave.Number,
		Slug: delivery.Slug, TaskID: taskID, Execution: runExecution, BaseSHA: wave.BaseHeadSHA,
	})
	if err != nil {
		return err
	}
	intent := routing.TaskWorktreeIntent{
		OperationID: identity.OperationID, RequestDigest: identity.RequestDigest,
		Name: identity.Name, Branch: identity.Branch,
	}
	if attempt.WorktreeIntent != nil && !reflect.DeepEqual(attempt.WorktreeIntent, &intent) {
		return routing.ErrDeliveryConflict
	}
	if err := store.WithLockedJournal(scope.WorkspaceID, func(tx *routing.JournalTx) error {
		record, exists := tx.Journal.Deliveries[delivery.DeliveryID]
		if !exists || record.Graph == nil {
			return routing.ErrDeliveryConflict
		}
		replayed, err := record.Graph.PlanWorktree(taskID, attempt.Execution, intent)
		if err != nil {
			return err
		}
		if replayed {
			return nil
		}
		tx.Journal.Deliveries[delivery.DeliveryID] = record
		return tx.Persist()
	}); err != nil {
		return err
	}

	return store.WithLockedJournal(scope.WorkspaceID, func(tx *routing.JournalTx) error {
		record, exists := tx.Journal.Deliveries[delivery.DeliveryID]
		if !exists || record.Graph == nil {
			return routing.ErrDeliveryConflict
		}
		currentTask, exists := record.Graph.Task(taskID)
		if !exists || attempt.Execution > len(currentTask.Attempts) {
			return routing.ErrDeliveryConflict
		}
		currentAttempt := currentTask.Attempts[attempt.Execution-1]
		if currentAttempt.WorktreeIntent == nil || !reflect.DeepEqual(currentAttempt.WorktreeIntent, &intent) {
			return routing.ErrDeliveryConflict
		}

		var worktree worktreeops.Worktree
		if currentAttempt.WorktreeID != "" {
			worktree, err = s.Worktrees.Inspect(ctx, scope, currentAttempt.WorktreeID)
		} else {
			var found bool
			worktree, found, err = s.Worktrees.FindByName(ctx, scope, identity.Name)
			if err == nil && !found {
				worktree, err = s.Worktrees.Create(ctx, scope, worktreeops.CreateRequest{
					Name: identity.Name, Branch: identity.Branch, BaseSHA: wave.BaseHeadSHA,
				})
			}
		}
		if err != nil {
			return err
		}
		if worktree.Name != identity.Name || worktree.Branch != identity.Branch || worktree.BaseSHA != wave.BaseHeadSHA ||
			worktree.BaseRef != wave.BaseHeadSHA || worktree.WorkspaceID != scope.WorkspaceID ||
			worktree.RepositoryRoot != scope.WorkspaceRoot || worktree.Root == scope.WorkspaceRoot ||
			(worktree.State != "pending" && worktree.State != "ready") {
			return worktreeops.ErrInvalidWorktreeIdentity
		}
		ready := worktree.State == "ready" && worktree.Setup.State == "ok"
		if worktree.State == "pending" && worktree.Setup.State != "none" {
			return worktreeops.ErrInvalidWorktreeIdentity
		}
		if _, err := record.Graph.AttachWorktree(taskID, attempt.Execution, routing.GraphWorktree{
			ID: worktree.ID, Root: worktree.Root, Ready: ready,
		}); err != nil {
			return err
		}
		tx.Journal.Deliveries[delivery.DeliveryID] = record
		return tx.Persist()
	})
}

func (s *deliveryGraphService) prepareWaveOutput(
	scope publication.TrustedScope,
	input DeliveryGraphInput,
	wave routing.DeliveryWave,
) (DeliveryGraphOutput, error) {
	store, err := s.store()
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	journal, exists, err := store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	delivery, exists := journal.Deliveries[input.DeliveryID]
	if !exists || delivery.Graph == nil {
		return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
	}
	remainingTokens, remainingWall, err := graphRemainingBudget(delivery, s.now())
	if err != nil {
		return DeliveryGraphOutput{}, err
	}
	output := DeliveryGraphOutput{
		Operation: input.Operation, Disposition: GraphDispositionWaveReady,
		DeliveryID: input.DeliveryID, Wave: wave.Number, Tasks: make([]DeliveryGraphTask, 0, len(wave.TaskIDs)),
		RemainingTokens: remainingTokens, RemainingWallSeconds: int(remainingWall / time.Second),
	}
	for _, taskID := range wave.TaskIDs {
		task, exists := delivery.Graph.Task(taskID)
		if !exists || len(task.Attempts) == 0 {
			return DeliveryGraphOutput{}, routing.ErrDeliveryConflict
		}
		attempt := task.Attempts[len(task.Attempts)-1]
		if task.State != routing.GraphTaskPreparing && task.State != routing.GraphTaskRunning {
			continue
		}
		if task.State == routing.GraphTaskPreparing {
			output.Disposition = GraphDispositionPreparing
		}
		output.Tasks = append(output.Tasks, DeliveryGraphTask{
			Wave: wave.Number, TaskID: taskID, Execution: attempt.Execution,
			Domain: task.Domain, Complexity: task.Complexity, Runtime: attempt.Runtime,
			WorktreeID: attempt.WorktreeID, WorktreeRoot: attempt.WorktreeRoot, BaseSHA: attempt.BaseHeadSHA,
			RemainingTokens: attempt.TokenAllowance, RemainingActiveWallSeconds: int(remainingWall / time.Second),
		})
	}
	return output, nil
}

func (s *deliveryGraphService) store() (*routing.OwnershipStore, error) {
	if s.StoreForCall != nil {
		return s.StoreForCall()
	}
	if s.Store == nil {
		return nil, errors.New("batuta: delivery graph journal is unavailable")
	}
	return s.Store, nil
}

func (s *deliveryGraphService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func activeDeliveryWave(graph *routing.DeliveryGraph) (routing.DeliveryWave, bool) {
	if graph == nil || len(graph.Waves) == 0 {
		return routing.DeliveryWave{}, false
	}
	for index := len(graph.Waves) - 1; index >= 0; index-- {
		wave := graph.Waves[index]
		for _, taskID := range wave.TaskIDs {
			task, exists := graph.Task(taskID)
			if exists && task.State != routing.GraphTaskIntegrated && task.State != routing.GraphTaskBlocked {
				return wave, true
			}
		}
	}
	return routing.DeliveryWave{}, false
}

func graphIntegrationHead(delivery routing.DeliveryRecord) string {
	if delivery.Graph != nil && len(delivery.Graph.Integrations) > 0 {
		return delivery.Graph.Integrations[len(delivery.Graph.Integrations)-1].FinalHeadSHA
	}
	return delivery.InitialWorktreeFingerprint.HeadSHA
}

func terminalTaskFailureStatus(status string) bool {
	switch status {
	case "blocked", "failed", "exhausted", "stalled", "canceled":
		return true
	default:
		return false
	}
}

func graphRemainingBudget(delivery routing.DeliveryRecord, now time.Time) (int64, time.Duration, error) {
	if delivery.Graph == nil {
		return 0, 0, routing.ErrDeliveryConflict
	}
	used, err := delivery.Graph.CumulativeTokens()
	if err != nil {
		return 0, 0, err
	}
	remainingTokens := delivery.TokenCeiling - used
	if remainingTokens < 0 {
		remainingTokens = 0
	}
	remainingWall, err := delivery.RemainingActiveWall(now)
	if err != nil {
		return 0, 0, err
	}
	return remainingTokens, remainingWall, nil
}

func deliveryWaveSlots(availableTokens int64) int {
	if availableTokens <= 0 {
		return 0
	}
	if availableTokens >= int64(routing.MaxParallelTasks) {
		return routing.MaxParallelTasks
	}
	return int(availableTokens)
}

func routingCandidateEvidence(
	evidence integration.CandidateEvidence,
	verification json.RawMessage,
) *routing.TaskCandidateEvidence {
	tracking := make([]routing.TaskTrackingFile, len(evidence.Tracking))
	for index, file := range evidence.Tracking {
		tracking[index] = routing.TaskTrackingFile{Path: file.Path, Digest: file.Digest, Ignored: file.Ignored}
	}
	owned := make([]string, len(evidence.OwnedTrackingPaths))
	copy(owned, evidence.OwnedTrackingPaths)
	return &routing.TaskCandidateEvidence{
		Slug: evidence.Slug, RepositoryIdentity: evidence.RepositoryIdentity, Branch: evidence.Branch,
		TreeSHA: evidence.TreeSHA, Verification: append(json.RawMessage(nil), verification...),
		OwnedTrackingPaths: owned, Tracking: tracking,
	}
}

func integrationCandidateEvidence(
	task routing.GraphTask,
	attempt routing.GraphTaskAttempt,
) (integration.CandidateEvidence, error) {
	if attempt.CandidateEvidence == nil || attempt.State != routing.GraphTaskCandidate ||
		attempt.CandidateCommitSHA == "" || attempt.VerificationDigest == "" {
		return integration.CandidateEvidence{}, routing.ErrDeliveryConflict
	}
	evidence := attempt.CandidateEvidence
	tracking := make([]integration.TrackingFile, len(evidence.Tracking))
	for index, file := range evidence.Tracking {
		tracking[index] = integration.TrackingFile{Path: file.Path, Digest: file.Digest, Ignored: file.Ignored}
	}
	owned := make([]string, len(evidence.OwnedTrackingPaths))
	copy(owned, evidence.OwnedTrackingPaths)
	return integration.CandidateEvidence{
		TaskID: task.TaskID, Slug: evidence.Slug, WorktreeRoot: attempt.WorktreeRoot,
		RepositoryIdentity: evidence.RepositoryIdentity, Branch: evidence.Branch,
		BaseSHA: attempt.BaseHeadSHA, CommitSHA: attempt.CandidateCommitSHA, TreeSHA: evidence.TreeSHA,
		VerificationDigest: attempt.VerificationDigest, OwnedTrackingPaths: owned, Tracking: tracking,
	}, nil
}

func deriveIntegrationIdentity(
	scope publication.TrustedScope,
	delivery routing.DeliveryRecord,
	wave routing.DeliveryWave,
	startingHead string,
	candidates []integration.CandidateEvidence,
) (string, string, error) {
	payload, err := json.Marshal(struct {
		WorkspaceID  string                          `json:"workspace_id"`
		DeliveryID   string                          `json:"delivery_id"`
		Wave         routing.DeliveryWave            `json:"wave"`
		StartingHead string                          `json:"starting_head"`
		Candidates   []integration.CandidateEvidence `json:"candidates"`
	}{
		WorkspaceID: scope.WorkspaceID, DeliveryID: delivery.DeliveryID,
		Wave: wave, StartingHead: startingHead, Candidates: candidates,
	})
	if err != nil {
		return "", "", routing.ErrDeliveryConflict
	}
	request := sha256.Sum256(payload)
	operation := sha256.Sum256(append([]byte("settle-wave\x00"), payload...))
	return "sha256:" + hex.EncodeToString(operation[:]), "sha256:" + hex.EncodeToString(request[:]), nil
}

func deriveCleanupOperation(
	scope publication.TrustedScope,
	delivery routing.DeliveryRecord,
	task routing.GraphTask,
	attempt routing.GraphTaskAttempt,
) (routing.CleanupOperation, error) {
	payload, err := json.Marshal(struct {
		WorkspaceID string `json:"workspace_id"`
		DeliveryID  string `json:"delivery_id"`
		TaskID      string `json:"task_id"`
		Execution   int    `json:"execution"`
		WorktreeID  string `json:"worktree_id"`
	}{
		WorkspaceID: scope.WorkspaceID, DeliveryID: delivery.DeliveryID,
		TaskID: task.TaskID, Execution: attempt.Execution, WorktreeID: attempt.WorktreeID,
	})
	if err != nil {
		return routing.CleanupOperation{}, routing.ErrDeliveryConflict
	}
	request := sha256.Sum256(payload)
	operation := sha256.Sum256(append([]byte("cleanup-worktree\x00"), payload...))
	return routing.CleanupOperation{
		OperationID:   "sha256:" + hex.EncodeToString(operation[:]),
		RequestDigest: "sha256:" + hex.EncodeToString(request[:]),
		TaskID:        task.TaskID, Execution: attempt.Execution, WorktreeID: attempt.WorktreeID,
	}, nil
}

func graphWaveForAttempt(
	graph *routing.DeliveryGraph,
	task routing.GraphTask,
	execution int,
) (routing.DeliveryWave, bool) {
	if graph == nil || execution < 1 || execution > len(task.Attempts) {
		return routing.DeliveryWave{}, false
	}
	waveOrdinal := 1
	for index := 1; index < execution; index++ {
		prior := task.Attempts[index-1]
		continuedQuestion := prior.Question != nil && prior.Question.Answer != nil
		if !continuedQuestion {
			waveOrdinal++
		}
	}
	seen := 0
	for _, wave := range graph.Waves {
		if !containsTaskID(wave.TaskIDs, task.TaskID) {
			continue
		}
		seen++
		if seen == waveOrdinal && wave.BaseHeadSHA == task.Attempts[execution-1].BaseHeadSHA {
			return wave, true
		}
	}
	return routing.DeliveryWave{}, false
}

func graphAllIntegrated(graph *routing.DeliveryGraph) bool {
	if graph == nil || len(graph.Tasks) == 0 {
		return false
	}
	for _, task := range graph.Tasks {
		if task.State != routing.GraphTaskIntegrated {
			return false
		}
	}
	return true
}

func containsTaskID(values []string, taskID string) bool {
	for _, value := range values {
		if value == taskID {
			return true
		}
	}
	return false
}

func graphTaskForOperation(
	journal routing.RoutingJournal,
	scope publication.TrustedScope,
	input DeliveryGraphInput,
) (routing.DeliveryRecord, routing.GraphTask, routing.GraphTaskAttempt, routing.DeliveryWave, error) {
	delivery, exists := journal.Deliveries[input.DeliveryID]
	if !exists || delivery.WorkspaceID != scope.WorkspaceID || delivery.WorktreeRoot != scope.WorkspaceRoot ||
		delivery.State != routing.DeliveryStateActive || delivery.Graph == nil ||
		input.Wave < 1 || input.Wave > len(delivery.Graph.Waves) {
		return routing.DeliveryRecord{}, routing.GraphTask{}, routing.GraphTaskAttempt{}, routing.DeliveryWave{}, routing.ErrDeliveryConflict
	}
	wave := delivery.Graph.Waves[input.Wave-1]
	if !containsTaskID(wave.TaskIDs, input.TaskID) {
		return routing.DeliveryRecord{}, routing.GraphTask{}, routing.GraphTaskAttempt{}, routing.DeliveryWave{}, routing.ErrDeliveryConflict
	}
	task, exists := delivery.Graph.Task(input.TaskID)
	if !exists || input.Execution < 1 {
		return routing.DeliveryRecord{}, routing.GraphTask{}, routing.GraphTaskAttempt{}, routing.DeliveryWave{}, routing.ErrDeliveryConflict
	}
	attempt, exists := graphTaskAttemptForRunExecution(task, input.Execution)
	if !exists || attempt.BaseHeadSHA != wave.BaseHeadSHA {
		return routing.DeliveryRecord{}, routing.GraphTask{}, routing.GraphTaskAttempt{}, routing.DeliveryWave{}, routing.ErrDeliveryConflict
	}
	return delivery, task, attempt, wave, nil
}

func deriveQuestionOperationID(scope publication.TrustedScope, input DeliveryGraphInput) (string, error) {
	payload, err := json.Marshal(struct {
		WorkspaceID string   `json:"workspace_id"`
		DeliveryID  string   `json:"delivery_id"`
		Wave        int      `json:"wave"`
		TaskID      string   `json:"task_id"`
		Execution   int      `json:"execution"`
		Prompt      string   `json:"prompt"`
		Choices     []string `json:"choices"`
	}{
		WorkspaceID: scope.WorkspaceID, DeliveryID: input.DeliveryID, Wave: input.Wave,
		TaskID: input.TaskID, Execution: input.Execution, Prompt: input.Prompt,
		Choices: append([]string(nil), input.Choices...),
	})
	if err != nil {
		return "", routing.ErrDeliveryConflict
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func canonicalTaskContextDigest(taskID string) string {
	payload, _ := json.Marshal(struct {
		TaskID string `json:"task_id"`
	}{TaskID: taskID})
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func graphTaskRunMatches(
	run deliveryRun,
	workspaceID string,
	delivery routing.DeliveryRecord,
	wave routing.DeliveryWave,
	task routing.GraphTask,
	execution int,
) bool {
	if execution < 1 || execution > len(task.Attempts) {
		return false
	}
	attempt := task.Attempts[execution-1]
	if validateDeliveryRun(run, workspaceID) != nil || run.LoopName != "batuta-task" ||
		stringInput(run.Inputs, "delivery_id") != delivery.DeliveryID ||
		intInput(run.Inputs, "wave") != int64(wave.Number) || stringInput(run.Inputs, "task_id") != task.TaskID ||
		intInput(run.Inputs, "execution") != int64(graphAttemptRunExecution(attempt)) ||
		stringInput(run.Inputs, "routing_generation") != delivery.RoutingGenerationDigest {
		return false
	}
	if stringInput(run.Inputs, "worktree_ref") != attempt.WorktreeID || stringInput(run.Inputs, "base_sha") != attempt.BaseHeadSHA {
		return false
	}
	runtimeInput, ok := run.Inputs["runtime"].(map[string]any)
	return ok && stringInput(runtimeInput, "provider") == attempt.Runtime.Provider &&
		stringInput(runtimeInput, "model") == attempt.Runtime.Model &&
		stringInput(runtimeInput, "reasoning") == attempt.Runtime.Reasoning
}

func graphTaskAttemptForRunExecution(task routing.GraphTask, runExecution int) (routing.GraphTaskAttempt, bool) {
	for index := len(task.Attempts) - 1; index >= 0; index-- {
		attempt := task.Attempts[index]
		if graphAttemptRunExecution(attempt) == runExecution {
			return attempt, true
		}
	}
	return routing.GraphTaskAttempt{}, false
}

func graphTaskQuestionAttempt(task routing.GraphTask, runExecution int, operationID string) (routing.GraphTaskAttempt, bool) {
	for index := len(task.Attempts) - 1; index >= 0; index-- {
		attempt := task.Attempts[index]
		if graphAttemptRunExecution(attempt) == runExecution && attempt.Question != nil && attempt.Question.RequestID == operationID {
			return attempt, true
		}
	}
	return routing.GraphTaskAttempt{}, false
}

type completedTaskOutput struct {
	Status             string          `json:"status"`
	TaskID             string          `json:"task_id"`
	Execution          int             `json:"execution"`
	CommitSHA          string          `json:"commit_sha"`
	Verification       json.RawMessage `json:"verification"`
	VerificationDigest string          `json:"verification_digest"`
}

const maxInlineTaskCompletionBytes = 16 << 10

func deriveCompletedTaskCandidate(detail deliveryRunDetail, taskID string) (completedTaskOutput, bool) {
	var latest *deliveryGeneration
	for index := range detail.Generations {
		generation := &detail.Generations[index]
		if latest == nil || generation.Generation > latest.Generation {
			latest = generation
		}
	}
	if latest == nil {
		return completedTaskOutput{}, false
	}
	var candidate completedTaskOutput
	matches := 0
	for _, output := range latest.Outputs {
		if output.NodeID != "implementation" || output.ItemIndex != 0 || output.Status != "succeeded" ||
			output.OutputRef == "" || len(output.OutputRef) > maxInlineTaskCompletionBytes ||
			strings.HasPrefix(output.OutputRef, "sha256:") || rejectDuplicateJSONKeys([]byte(output.OutputRef)) != nil {
			continue
		}
		var completed completedTaskOutput
		decoder := json.NewDecoder(strings.NewReader(output.OutputRef))
		decoder.DisallowUnknownFields()
		if err := decoder.Decode(&completed); err != nil {
			continue
		}
		if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
			continue
		}
		if completed.Status != "completed" || completed.TaskID != taskID || completed.Execution < 1 ||
			!gitSHAValue(completed.CommitSHA) || !validTaskVerification(completed.Verification, completed.VerificationDigest, taskID) {
			continue
		}
		candidate = completed
		matches++
	}
	return candidate, matches == 1
}

func candidateFieldsEqual(input DeliveryGraphInput, completed completedTaskOutput) bool {
	return input.CommitSHA == completed.CommitSHA && input.VerificationDigest == completed.VerificationDigest &&
		bytes.Equal(input.Verification, completed.Verification)
}

func candidateReplayMatches(input DeliveryGraphInput, attempt routing.GraphTaskAttempt) bool {
	if input.ChildRunID == "" || input.ChildRunID != attempt.ChildRunID {
		return false
	}
	if !input.hasCandidateFields() {
		return true
	}
	return input.BaseSHA == attempt.BaseHeadSHA && input.CommitSHA == attempt.CandidateCommitSHA &&
		input.VerificationDigest == attempt.VerificationDigest && attempt.CandidateEvidence != nil &&
		bytes.Equal(input.Verification, attempt.CandidateEvidence.Verification)
}

func hasCompletedTaskOutput(detail deliveryRunDetail, input DeliveryGraphInput) bool {
	completed, ok := deriveCompletedTaskCandidate(detail, input.TaskID)
	return ok && completed.Execution == input.Execution && candidateFieldsEqual(input, completed)
}

func graphAttemptRunExecution(attempt routing.GraphTaskAttempt) int {
	if attempt.RunExecution > 0 {
		return attempt.RunExecution
	}
	return attempt.Execution
}

func matchingAnsweredRequest(requests []deliveryRequest, childRunID string, question routing.TaskQuestion) (deliveryRequest, bool) {
	var match deliveryRequest
	matches := 0
	for _, request := range requests {
		if request.LoopRunID == childRunID && request.LoopName == "batuta-task" &&
			request.Generation >= 1 && request.NodeID == "ask_operator" && request.ItemIndex >= 0 && request.Kind == "ask" && request.State == "answered" &&
			request.Prompt == question.Prompt && rawJSONDigest(request.Context) == question.ContextDigest &&
			jsonEquivalent(request.Expect, taskAnswerExpectation()) && reflect.DeepEqual(request.Decisions, []string{"respond"}) &&
			request.Agents == "deny" && request.AnsweredDecision == "respond" && request.ActorKind == "human" &&
			boundedRequestIdentity(request.ActorID) && request.AnsweredAt != nil && request.ResolvedAt != nil &&
			request.AnsweredAt.Equal(*request.ResolvedAt) {
			match = request
			matches++
		}
	}
	return match, matches == 1
}

func matchingResolvedAskCell(generations []deliveryGeneration, request deliveryRequest, answer string) bool {
	matches := 0
	for _, generation := range generations {
		if generation.Generation != int64(request.Generation) {
			continue
		}
		for _, output := range generation.Outputs {
			if output.NodeID != "ask_operator" || output.ItemIndex != request.ItemIndex || output.Status != "succeeded" ||
				!matchingInlineAskAnswer(output.OutputRef, answer) {
				continue
			}
			matches++
		}
	}
	return matches == 1
}

func matchingInlineAskAnswer(outputRef, answer string) bool {
	payload := []byte(outputRef)
	if len(payload) == 0 || len(payload) > maxInlineTaskCompletionBytes || rejectDuplicateJSONKeys(payload) != nil {
		return false
	}
	var decoded struct {
		Answer string `json:"answer"`
	}
	var fields map[string]json.RawMessage
	if json.Unmarshal(payload, &decoded) != nil || json.Unmarshal(payload, &fields) != nil || len(fields) != 1 {
		return false
	}
	_, hasAnswer := fields["answer"]
	return hasAnswer && decoded.Answer == answer
}

func taskAnswerExpectation() json.RawMessage {
	return json.RawMessage(`{"type":"object","additionalProperties":false,"required":["answer"],"properties":{"answer":{"type":"string","minLength":1,"maxLength":4096}}}`)
}

func rawJSONDigest(payload json.RawMessage) string {
	if !json.Valid(payload) {
		return ""
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func jsonEquivalent(left, right json.RawMessage) bool {
	var leftValue any
	var rightValue any
	return json.Unmarshal(left, &leftValue) == nil && json.Unmarshal(right, &rightValue) == nil &&
		reflect.DeepEqual(leftValue, rightValue)
}

func boundedRequestIdentity(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 256
}

func (c deliveryLoopCLIClient) RecentTasks(ctx context.Context, workspaceID string, limit int) ([]deliveryRun, error) {
	if err := c.validateBoundary(ctx, workspaceID); err != nil {
		return nil, err
	}
	if limit != 200 {
		return nil, errors.New("batuta: task reconciliation requires the fixed recent-run limit")
	}
	result, err := c.run(ctx, []string{
		"loop", "runs", "--workspace", workspaceID, "--loop", "batuta-task", "--limit", "200", "-o", "json",
	})
	if err != nil {
		return nil, err
	}
	var response deliveryRunListResponse
	if err := decodeDeliveryResponse(result, &response); err != nil || response.Items == nil || len(response.Items) > limit {
		return nil, errors.New("batuta: malformed Compozy task-run response")
	}
	for _, run := range response.Items {
		if validateDeliveryRun(run, workspaceID) != nil || run.LoopName != "batuta-task" {
			return nil, errors.New("batuta: task-run response contains invalid identity")
		}
	}
	return response.Items, nil
}

func emptyDigest() string {
	value := sha256.Sum256(nil)
	return "sha256:" + hex.EncodeToString(value[:])
}
