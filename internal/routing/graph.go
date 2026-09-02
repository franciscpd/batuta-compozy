package routing

import (
	"bytes"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"regexp"
	"slices"
	"strings"
	"time"
)

const (
	MaxParallelTasks  = 4
	MaxDeliveryTasks  = 64
	MaxTaskExecutions = 4

	maxQuestionBytes              = 2 << 10
	maxChoiceBytes                = 512
	maxAnswerBytes                = 4 << 10
	maxCandidateVerificationBytes = 64 << 10
)

var (
	ErrInvalidDeliveryGraph = errors.New("routing: invalid delivery graph")
	ErrDependencyBlocked    = errors.New("routing: delivery dependencies are blocked")
	ErrInvalidActiveWall    = errors.New("routing: active wall evidence is invalid")
)

type GraphTaskState string

const (
	GraphTaskPending      GraphTaskState = "pending"
	GraphTaskPreparing    GraphTaskState = "preparing"
	GraphTaskRunning      GraphTaskState = "running"
	GraphTaskWaitingInput GraphTaskState = "waiting_input"
	GraphTaskCandidate    GraphTaskState = "candidate"
	GraphTaskIntegrated   GraphTaskState = "integrated"
	GraphTaskBlocked      GraphTaskState = "blocked"
)

type DeliveryGraph struct {
	Tasks        []GraphTask            `json:"tasks"`
	Waves        []DeliveryWave         `json:"waves"`
	Integrations []IntegrationOperation `json:"integrations"`
	Pauses       []HumanPause           `json:"pauses"`
	Cleanups     []CleanupOperation     `json:"cleanups,omitempty"`
}

type GraphTask struct {
	TaskID              string             `json:"task_id"`
	AuthoredIndex       int                `json:"authored_index"`
	Dependencies        []string           `json:"dependencies"`
	Domain              Domain             `json:"domain"`
	Complexity          Complexity         `json:"complexity"`
	State               GraphTaskState     `json:"state"`
	Attempts            []GraphTaskAttempt `json:"attempts"`
	IntegratedCommitSHA string             `json:"integrated_commit_sha,omitempty"`
	BlockerCode         string             `json:"blocker_code,omitempty"`
}

type GraphTaskAttempt struct {
	Execution          int                    `json:"execution"`
	RunExecution       int                    `json:"run_execution,omitempty"`
	Runtime            RuntimeValue           `json:"runtime"`
	State              GraphTaskState         `json:"state"`
	BaseHeadSHA        string                 `json:"base_head_sha"`
	WorktreeIntent     *TaskWorktreeIntent    `json:"worktree_intent,omitempty"`
	WorktreeID         string                 `json:"worktree_id,omitempty"`
	WorktreeRoot       string                 `json:"worktree_root,omitempty"`
	ChildRunID         string                 `json:"child_run_id,omitempty"`
	CandidateCommitSHA string                 `json:"candidate_commit_sha,omitempty"`
	VerificationDigest string                 `json:"verification_digest,omitempty"`
	Question           *TaskQuestion          `json:"question,omitempty"`
	Conflict           *ConflictProof         `json:"conflict,omitempty"`
	CandidateEvidence  *TaskCandidateEvidence `json:"candidate_evidence,omitempty"`
	TokenAllowance     int64                  `json:"token_allowance,omitempty"`
	TokensUsed         *int64                 `json:"tokens_used,omitempty"`
	TerminalStatus     string                 `json:"terminal_status,omitempty"`
	BlockerCode        string                 `json:"blocker_code,omitempty"`
}

type TaskQuestion struct {
	RequestID     string      `json:"request_id"`
	Prompt        string      `json:"prompt"`
	ContextDigest string      `json:"context_digest"`
	Choices       []string    `json:"choices,omitempty"`
	Answer        *TaskAnswer `json:"answer,omitempty"`
}

type TaskAnswer struct {
	QuestionOperationID string `json:"question_operation_id"`
	LoopRunID           string `json:"loop_run_id"`
	Generation          int    `json:"generation"`
	NodeID              string `json:"node_id"`
	ItemIndex           int    `json:"item_index"`
	Value               string `json:"value"`
}

type ConflictProof struct {
	IntegrationOperationID string `json:"integration_operation_id"`
	IntegrationHeadSHA     string `json:"integration_head_sha"`
	CandidateCommitSHA     string `json:"candidate_commit_sha"`
	EvidenceDigest         string `json:"evidence_digest"`
}

type DeliveryWave struct {
	Number      int      `json:"number"`
	BaseHeadSHA string   `json:"base_head_sha"`
	TaskIDs     []string `json:"task_ids"`
}

type GraphWorktree struct {
	ID    string `json:"id"`
	Root  string `json:"root"`
	Ready bool   `json:"ready"`
}

type TaskWorktreeIntent struct {
	OperationID   string `json:"operation_id"`
	RequestDigest string `json:"request_digest"`
	Name          string `json:"name"`
	Branch        string `json:"branch"`
}

type TaskFailure struct {
	ChildRunID     string
	TerminalStatus string
	BlockerCode    string
	TokensUsed     int64
}

type TaskFailureResult struct {
	Replayed bool
	Blocked  bool
	Wave     DeliveryWave
	Runtime  RuntimeValue
}

type TaskCandidate struct {
	ChildRunID         string
	BaseHeadSHA        string
	CommitSHA          string
	VerificationDigest string
	TokensUsed         int64
	Evidence           *TaskCandidateEvidence
}

type TaskCandidateEvidence struct {
	Slug               string             `json:"slug"`
	RepositoryIdentity string             `json:"repository_identity"`
	Branch             string             `json:"branch"`
	TreeSHA            string             `json:"tree_sha"`
	Verification       json.RawMessage    `json:"verification"`
	OwnedTrackingPaths []string           `json:"owned_tracking_paths"`
	Tracking           []TaskTrackingFile `json:"tracking"`
}

type TaskTrackingFile struct {
	Path    string `json:"path"`
	Digest  string `json:"digest"`
	Ignored bool   `json:"ignored,omitempty"`
}

type IntegrationOperation struct {
	OperationID            string   `json:"operation_id"`
	RequestDigest          string   `json:"request_digest"`
	Wave                   int      `json:"wave"`
	StartingHeadSHA        string   `json:"starting_head_sha"`
	OrderedTaskIDs         []string `json:"ordered_task_ids"`
	CandidateCommitSHAs    []string `json:"candidate_commit_shas"`
	AcceptedTaskIDs        []string `json:"accepted_task_ids"`
	AcceptedCommitSHAs     []string `json:"accepted_commit_shas"`
	IntegratedCommitSHAs   []string `json:"integrated_commit_shas"`
	ConflictingTaskID      string   `json:"conflicting_task_id,omitempty"`
	ConflictEvidenceDigest string   `json:"conflict_evidence_digest,omitempty"`
	FinalHeadSHA           string   `json:"final_head_sha"`
}

type WaveSettlement struct {
	OperationID            string
	RequestDigest          string
	Wave                   int
	StartingHeadSHA        string
	OrderedTaskIDs         []string
	CandidateCommitSHAs    []string
	AcceptedTaskIDs        []string
	AcceptedCommitSHAs     []string
	IntegratedCommitSHAs   []string
	FirstConflictTaskID    string
	ConflictEvidenceDigest string
	FinalHeadSHA           string
}

type SettlementDisposition string

const (
	SettlementWaveIntegrated    SettlementDisposition = "wave_integrated"
	SettlementReexecuteConflict SettlementDisposition = "reexecute_conflict"
	SettlementAllIntegrated     SettlementDisposition = "all_integrated"
	SettlementBlocked           SettlementDisposition = "blocked"
)

type WaveSettlementResult struct {
	Replayed    bool
	Disposition SettlementDisposition
	TaskID      string
	Wave        DeliveryWave
	Runtime     RuntimeValue
}

type CleanupState string

const (
	CleanupPlanned  CleanupState = "planned"
	CleanupRemoved  CleanupState = "removed"
	CleanupRetained CleanupState = "retained"
)

type CleanupOperation struct {
	OperationID   string       `json:"operation_id"`
	RequestDigest string       `json:"request_digest"`
	TaskID        string       `json:"task_id"`
	Execution     int          `json:"execution"`
	WorktreeID    string       `json:"worktree_id"`
	State         CleanupState `json:"state"`
	BlockerCode   string       `json:"blocker_code,omitempty"`
}

type HumanPause struct {
	TaskID    string     `json:"task_id"`
	Execution int        `json:"execution"`
	RequestID string     `json:"request_identity"`
	StartedAt time.Time  `json:"started_at"`
	EndedAt   *time.Time `json:"ended_at,omitempty"`
}

type ReadyWaveInput struct {
	IntegrationHeadSHA string
	RemainingSlots     int
	ReachableCommits   map[string]bool
}

func NewDeliveryGraph(
	snapshot DeliveryTaskSnapshot,
	generation RoutingGeneration,
	integrationHeadSHA string,
) (*DeliveryGraph, error) {
	recomputedGeneration, generationErr := finalizeGeneration(generation)
	if len(snapshot.Tasks) == 0 || len(snapshot.Tasks) > MaxDeliveryTasks ||
		!canonicalGitSHA.MatchString(integrationHeadSHA) ||
		validateStandaloneTaskSnapshot(snapshot) != nil ||
		generationErr != nil || recomputedGeneration.Digest != generation.Digest ||
		len(generation.Tasks) != len(snapshot.Tasks) {
		return nil, ErrInvalidDeliveryGraph
	}
	generationTasks := make(map[string]GenerationTask, len(generation.Tasks))
	for _, task := range generation.Tasks {
		if _, duplicate := generationTasks[task.ID]; duplicate {
			return nil, ErrInvalidDeliveryGraph
		}
		generationTasks[task.ID] = task
	}
	graph := &DeliveryGraph{
		Tasks:        make([]GraphTask, 0, len(snapshot.Tasks)),
		Waves:        []DeliveryWave{},
		Integrations: []IntegrationOperation{},
		Pauses:       []HumanPause{},
		Cleanups:     []CleanupOperation{},
	}
	for authoredIndex, task := range snapshot.Tasks {
		generationTask, exists := generationTasks[task.ID]
		if !exists || generationTask.Domain != task.Domain || generationTask.Complexity != task.Complexity {
			return nil, ErrInvalidDeliveryGraph
		}
		state := GraphTaskPending
		integratedCommit := ""
		if task.Status == "completed" {
			state = GraphTaskIntegrated
			integratedCommit = integrationHeadSHA
		}
		graph.Tasks = append(graph.Tasks, GraphTask{
			TaskID: task.ID, AuthoredIndex: authoredIndex,
			Dependencies: append([]string{}, task.Dependencies...),
			Domain:       task.Domain, Complexity: task.Complexity,
			State: state, Attempts: []GraphTaskAttempt{}, IntegratedCommitSHA: integratedCommit,
		})
	}
	if err := validateGraphShape(graph); err != nil {
		return nil, err
	}
	return graph, nil
}

func (g *DeliveryGraph) AdmitReadyWave(input ReadyWaveInput) (DeliveryWave, error) {
	if g == nil || !canonicalGitSHA.MatchString(input.IntegrationHeadSHA) || input.RemainingSlots < 0 ||
		input.ReachableCommits == nil {
		return DeliveryWave{}, ErrInvalidDeliveryGraph
	}
	if err := validateGraphShape(g); err != nil {
		return DeliveryWave{}, err
	}
	active := activeGraphTaskCount(g.Tasks)
	capacity := min(MaxParallelTasks-active, input.RemainingSlots)
	if capacity <= 0 {
		return DeliveryWave{}, nil
	}
	byID := graphTasksByID(g.Tasks)
	ready := make([]int, 0, capacity)
	for index := range g.Tasks {
		task := g.Tasks[index]
		if task.State != GraphTaskPending || !graphDependenciesReady(task, byID, input.ReachableCommits) {
			continue
		}
		ready = append(ready, index)
		if len(ready) == capacity {
			break
		}
	}
	if len(ready) == 0 {
		if active == 0 && graphHasIncompleteTasks(g.Tasks) {
			return DeliveryWave{}, ErrDependencyBlocked
		}
		return DeliveryWave{}, nil
	}
	wave := DeliveryWave{
		Number: len(g.Waves) + 1, BaseHeadSHA: input.IntegrationHeadSHA,
		TaskIDs: make([]string, 0, len(ready)),
	}
	for _, index := range ready {
		g.Tasks[index].State = GraphTaskPreparing
		wave.TaskIDs = append(wave.TaskIDs, g.Tasks[index].TaskID)
	}
	g.Waves = append(g.Waves, cloneDeliveryWave(wave))
	return cloneDeliveryWave(wave), nil
}

func (g *DeliveryGraph) BeginWaveAttempts(waveNumber int, generation RoutingGeneration) error {
	if g == nil || waveNumber < 1 || waveNumber > len(g.Waves) {
		return ErrInvalidDeliveryGraph
	}
	candidate := cloneDeliveryGraph(g)
	wave := candidate.Waves[waveNumber-1]
	replayed := 0
	for _, taskID := range wave.TaskIDs {
		task := graphTaskByID(candidate.Tasks, taskID)
		if task == nil || task.State != GraphTaskPreparing {
			return ErrInvalidDeliveryTransition
		}
		var generationTask GenerationTask
		found := false
		for _, value := range generation.Tasks {
			if value.ID == taskID && value.Domain == task.Domain && value.Complexity == task.Complexity {
				generationTask = value
				found = true
				break
			}
		}
		runtime, runtimeFound := generationRuntimeForTask(generation, generationTask)
		if !found || !runtimeFound {
			return ErrInvalidDeliveryGraph
		}
		if len(task.Attempts) == 0 || task.Attempts[len(task.Attempts)-1].State != GraphTaskPreparing {
			continue
		}
		execution := len(task.Attempts)
		attempt := task.Attempts[execution-1]
		if attempt.Execution != execution || attempt.RunExecution != execution || attempt.Runtime != runtime ||
			attempt.BaseHeadSHA != wave.BaseHeadSHA {
			return ErrInvalidDeliveryTransition
		}
		replayed++
	}
	if replayed > 0 {
		if replayed != len(wave.TaskIDs) {
			return ErrInvalidDeliveryTransition
		}
		return nil
	}
	for _, taskID := range wave.TaskIDs {
		task := graphTaskByID(candidate.Tasks, taskID)
		if task == nil || task.State != GraphTaskPreparing {
			return ErrInvalidDeliveryTransition
		}
		execution := len(task.Attempts) + 1
		if execution > MaxTaskExecutions {
			return ErrInvalidDeliveryTransition
		}
		var generationTask GenerationTask
		found := false
		for _, value := range generation.Tasks {
			if value.ID == taskID && value.Domain == task.Domain && value.Complexity == task.Complexity {
				generationTask = value
				found = true
				break
			}
		}
		runtime, runtimeFound := generationRuntimeForTask(generation, generationTask)
		if !found || !runtimeFound {
			return ErrInvalidDeliveryGraph
		}
		task.Attempts = append(task.Attempts, GraphTaskAttempt{
			Execution: execution, RunExecution: execution, Runtime: runtime,
			State: GraphTaskPreparing, BaseHeadSHA: wave.BaseHeadSHA,
		})
	}
	if err := validateGraphShape(candidate); err != nil {
		return err
	}
	*g = *candidate
	return nil
}

func (g *DeliveryGraph) AttachWorktree(
	taskID string,
	execution int,
	worktree GraphWorktree,
) (bool, error) {
	if g == nil || !boundedArgument(taskID) || execution < 1 || execution > MaxTaskExecutions ||
		!boundedArgument(worktree.ID) || !filepath.IsAbs(worktree.Root) || filepath.Clean(worktree.Root) != worktree.Root {
		return false, ErrInvalidDeliveryTransition
	}
	task := graphTaskByID(g.Tasks, taskID)
	if task == nil || len(task.Attempts) != execution {
		return false, ErrInvalidDeliveryTransition
	}
	attempt := &task.Attempts[execution-1]
	if attempt.State != GraphTaskPreparing && attempt.State != GraphTaskRunning {
		return false, ErrInvalidDeliveryTransition
	}
	if attempt.WorktreeID != "" && (attempt.WorktreeID != worktree.ID || attempt.WorktreeRoot != worktree.Root) {
		return false, ErrInvalidDeliveryTransition
	}
	if attempt.State == GraphTaskRunning {
		if !worktree.Ready {
			return false, ErrInvalidDeliveryTransition
		}
		return true, nil
	}
	if attempt.WorktreeID == worktree.ID && !worktree.Ready {
		return true, nil
	}
	attempt.WorktreeID = worktree.ID
	attempt.WorktreeRoot = worktree.Root
	if worktree.Ready {
		attempt.State = GraphTaskRunning
		task.State = GraphTaskRunning
	}
	return false, nil
}

func (g *DeliveryGraph) PlanWorktree(
	taskID string,
	execution int,
	intent TaskWorktreeIntent,
) (bool, error) {
	if g == nil || !boundedArgument(taskID) || execution < 1 || execution > MaxTaskExecutions ||
		!validTaskWorktreeIntent(&intent) {
		return false, ErrInvalidDeliveryTransition
	}
	task := graphTaskByID(g.Tasks, taskID)
	if task == nil || len(task.Attempts) < execution {
		return false, ErrInvalidDeliveryTransition
	}
	attempt := &task.Attempts[execution-1]
	if attempt.WorktreeIntent != nil {
		if !reflect.DeepEqual(attempt.WorktreeIntent, &intent) {
			return false, ErrInvalidDeliveryTransition
		}
		return true, nil
	}
	if len(task.Attempts) != execution || task.State != GraphTaskPreparing ||
		attempt.State != GraphTaskPreparing || attempt.WorktreeID != "" {
		return false, ErrInvalidDeliveryTransition
	}
	cloned := intent
	attempt.WorktreeIntent = &cloned
	return false, nil
}

func (g *DeliveryGraph) Task(taskID string) (GraphTask, bool) {
	if g == nil {
		return GraphTask{}, false
	}
	task := graphTaskByID(g.Tasks, taskID)
	if task == nil {
		return GraphTask{}, false
	}
	cloned := *task
	cloned.Dependencies = append([]string{}, task.Dependencies...)
	cloned.Attempts = make([]GraphTaskAttempt, len(task.Attempts))
	for index, attempt := range task.Attempts {
		cloned.Attempts[index] = cloneGraphTaskAttempt(attempt)
	}
	return cloned, true
}

func (g *DeliveryGraph) CumulativeTokens() (int64, error) {
	if g == nil {
		return 0, ErrInvalidDeliveryGraph
	}
	var total int64
	for _, task := range g.Tasks {
		for _, attempt := range task.Attempts {
			if attempt.TokensUsed == nil {
				continue
			}
			if *attempt.TokensUsed < 0 || total > int64(^uint64(0)>>1)-*attempt.TokensUsed {
				return 0, ErrInvalidDeliveryGraph
			}
			total += *attempt.TokensUsed
		}
	}
	return total, nil
}

func (g *DeliveryGraph) AvailableTokenBudget(ceiling int64) (int64, error) {
	if g == nil || ceiling < 0 {
		return 0, ErrInvalidDeliveryGraph
	}
	used, err := g.CumulativeTokens()
	if err != nil || used > ceiling {
		return 0, ErrInvalidDeliveryGraph
	}
	reserved := int64(0)
	for _, task := range g.Tasks {
		if len(task.Attempts) == 0 {
			continue
		}
		attempt := task.Attempts[len(task.Attempts)-1]
		if attempt.TokensUsed != nil || attempt.State == GraphTaskIntegrated || attempt.State == GraphTaskBlocked {
			continue
		}
		if attempt.TokenAllowance < 0 || reserved > ceiling-attempt.TokenAllowance {
			return 0, ErrInvalidDeliveryGraph
		}
		reserved += attempt.TokenAllowance
	}
	remaining := ceiling - used - reserved
	if remaining < 0 {
		return 0, ErrInvalidDeliveryGraph
	}
	return remaining, nil
}

func (g *DeliveryGraph) ReserveWaveTokens(waveNumber int, tokens int64) error {
	if g == nil || waveNumber < 1 || waveNumber > len(g.Waves) || tokens < 1 {
		return ErrInvalidDeliveryTransition
	}
	wave := g.Waves[waveNumber-1]
	share := tokens / int64(len(wave.TaskIDs))
	remainder := tokens % int64(len(wave.TaskIDs))
	if share < 1 {
		return ErrInvalidDeliveryTransition
	}
	for index, taskID := range wave.TaskIDs {
		task := graphTaskByID(g.Tasks, taskID)
		if task == nil || len(task.Attempts) == 0 {
			return ErrInvalidDeliveryTransition
		}
		attempt := &task.Attempts[len(task.Attempts)-1]
		want := share
		if int64(index) < remainder {
			want++
		}
		if attempt.TokenAllowance != 0 && attempt.TokenAllowance != want {
			return ErrInvalidDeliveryTransition
		}
		if attempt.State != GraphTaskPreparing || attempt.TokensUsed != nil {
			return ErrInvalidDeliveryTransition
		}
		attempt.TokenAllowance = want
	}
	return nil
}

func (g *DeliveryGraph) ReserveAttemptTokens(taskID string, execution int, tokens int64) error {
	if g == nil || !boundedArgument(taskID) || execution < 1 || execution > MaxTaskExecutions || tokens < 1 {
		return ErrInvalidDeliveryTransition
	}
	task := graphTaskByID(g.Tasks, taskID)
	if task == nil || len(task.Attempts) != execution {
		return ErrInvalidDeliveryTransition
	}
	attempt := &task.Attempts[execution-1]
	if attempt.State != GraphTaskPreparing || attempt.TokensUsed != nil ||
		(attempt.TokenAllowance != 0 && attempt.TokenAllowance != tokens) {
		return ErrInvalidDeliveryTransition
	}
	attempt.TokenAllowance = tokens
	return nil
}

func (g *DeliveryGraph) RecordQuestion(
	taskID string,
	execution int,
	childRunID string,
	question TaskQuestion,
	startedAt time.Time,
) (bool, error) {
	if g == nil || !boundedArgument(taskID) || execution < 1 || execution >= MaxTaskExecutions ||
		!boundedArgument(childRunID) || !validTaskQuestion(&question) || question.Answer != nil ||
		startedAt.IsZero() || startedAt.Location() != time.UTC {
		return false, ErrInvalidDeliveryTransition
	}
	candidate := cloneDeliveryGraph(g)
	task := graphTaskByID(candidate.Tasks, taskID)
	if task == nil || len(task.Attempts) != execution {
		return false, ErrInvalidDeliveryTransition
	}
	attempt := &task.Attempts[execution-1]
	if attempt.Question != nil {
		if task.State != GraphTaskWaitingInput || attempt.State != GraphTaskWaitingInput ||
			attempt.ChildRunID != childRunID || !reflect.DeepEqual(attempt.Question, &question) {
			return false, ErrInvalidDeliveryTransition
		}
		return true, nil
	}
	if task.State != GraphTaskRunning || attempt.State != GraphTaskRunning || attempt.WorktreeID == "" ||
		(attempt.ChildRunID != "" && attempt.ChildRunID != childRunID) {
		return false, ErrInvalidDeliveryTransition
	}
	attempt.ChildRunID = childRunID
	attempt.Question = cloneTaskQuestion(&question)
	attempt.State = GraphTaskWaitingInput
	task.State = GraphTaskWaitingInput
	if _, err := candidate.ReconcileHumanPause(startedAt); err != nil {
		return false, ErrInvalidDeliveryTransition
	}
	if err := validateGraphTask(*task, "pending"); err != nil {
		return false, err
	}
	*g = *candidate
	return false, nil
}

func (g *DeliveryGraph) RecordAnswer(
	taskID string,
	execution int,
	answer TaskAnswer,
	endedAt time.Time,
) (int, bool, error) {
	if g == nil || !boundedArgument(taskID) || execution < 1 || execution >= MaxTaskExecutions ||
		!validTaskAnswer(&answer) || endedAt.IsZero() || endedAt.Location() != time.UTC {
		return 0, false, ErrInvalidDeliveryTransition
	}
	candidate := cloneDeliveryGraph(g)
	task := graphTaskByID(candidate.Tasks, taskID)
	if task == nil || len(task.Attempts) < execution {
		return 0, false, ErrInvalidDeliveryTransition
	}
	attempt := &task.Attempts[execution-1]
	if attempt.Question == nil || attempt.Question.RequestID != answer.QuestionOperationID ||
		attempt.ChildRunID != answer.LoopRunID {
		return 0, false, ErrInvalidDeliveryTransition
	}
	if attempt.Question.Answer != nil {
		if len(task.Attempts) < execution+1 || !reflect.DeepEqual(attempt.Question.Answer, &answer) {
			return 0, false, ErrInvalidDeliveryTransition
		}
		return execution + 1, true, nil
	}
	if task.State != GraphTaskWaitingInput || attempt.State != GraphTaskWaitingInput || len(task.Attempts) != execution {
		return 0, false, ErrInvalidDeliveryTransition
	}
	attempt.Question.Answer = cloneTaskAnswer(&answer)
	attempt.State = GraphTaskRunning
	task.Attempts = append(task.Attempts, GraphTaskAttempt{
		Execution: execution + 1, RunExecution: graphAttemptRunExecution(*attempt),
		Runtime: attempt.Runtime, State: GraphTaskRunning,
		BaseHeadSHA: attempt.BaseHeadSHA, WorktreeID: attempt.WorktreeID, WorktreeRoot: attempt.WorktreeRoot,
		WorktreeIntent: cloneTaskWorktreeIntent(attempt.WorktreeIntent), ChildRunID: attempt.ChildRunID,
		TokenAllowance: attempt.TokenAllowance,
	})
	task.State = GraphTaskRunning
	if _, err := candidate.ReconcileHumanPause(endedAt); err != nil {
		return 0, false, ErrInvalidDeliveryTransition
	}
	if err := validateGraphTask(*task, "pending"); err != nil {
		return 0, false, err
	}
	*g = *candidate
	return execution + 1, false, nil
}

func (g *DeliveryGraph) RecordFailure(
	taskID string,
	execution int,
	failure TaskFailure,
	generation RoutingGeneration,
	nextBaseHeadSHA string,
	retryAllowed bool,
) (TaskFailureResult, error) {
	if g == nil || !boundedArgument(taskID) || execution < 1 || execution > MaxTaskExecutions ||
		!boundedArgument(failure.ChildRunID) || !validTaskTerminalStatus(failure.TerminalStatus) ||
		!boundedArgument(failure.BlockerCode) || failure.TokensUsed < 0 ||
		!canonicalGitSHA.MatchString(nextBaseHeadSHA) {
		return TaskFailureResult{}, ErrInvalidDeliveryTransition
	}
	recomputed, err := finalizeGeneration(generation)
	if err != nil || recomputed.Digest != generation.Digest {
		return TaskFailureResult{}, ErrInvalidDeliveryGraph
	}
	candidate := cloneDeliveryGraph(g)
	task := graphTaskByID(candidate.Tasks, taskID)
	if task == nil || execution > len(task.Attempts) {
		return TaskFailureResult{}, ErrInvalidDeliveryTransition
	}
	attempt := &task.Attempts[execution-1]
	if attempt.TokenAllowance > 0 && failure.TokensUsed > attempt.TokenAllowance {
		return TaskFailureResult{}, ErrInvalidDeliveryTransition
	}
	if attempt.State == GraphTaskBlocked {
		if attempt.ChildRunID != failure.ChildRunID || attempt.TerminalStatus != failure.TerminalStatus ||
			attempt.BlockerCode != failure.BlockerCode || attempt.TokensUsed == nil || *attempt.TokensUsed != failure.TokensUsed {
			return TaskFailureResult{}, ErrInvalidDeliveryTransition
		}
		if len(task.Attempts) >= execution+1 {
			next := task.Attempts[execution]
			if next.Execution != execution+1 {
				return TaskFailureResult{}, ErrInvalidDeliveryTransition
			}
			wave, found := deliveryWaveForTask(candidate.Waves, taskID, next.BaseHeadSHA)
			if !found {
				return TaskFailureResult{}, ErrInvalidDeliveryGraph
			}
			return TaskFailureResult{Replayed: true, Wave: wave, Runtime: next.Runtime}, nil
		}
		if task.State == GraphTaskBlocked && len(task.Attempts) == execution {
			return TaskFailureResult{Replayed: true, Blocked: true}, nil
		}
		return TaskFailureResult{}, ErrInvalidDeliveryTransition
	}
	if len(task.Attempts) != execution || task.State != GraphTaskRunning || attempt.State != GraphTaskRunning ||
		(attempt.ChildRunID != "" && attempt.ChildRunID != failure.ChildRunID) {
		return TaskFailureResult{}, ErrInvalidDeliveryTransition
	}
	attempt.ChildRunID = failure.ChildRunID
	attempt.State = GraphTaskBlocked
	attempt.TerminalStatus = failure.TerminalStatus
	attempt.BlockerCode = failure.BlockerCode
	tokens := failure.TokensUsed
	attempt.TokensUsed = &tokens

	nextRuntime, eligible := nextRuntimeForTask(generation, *task, attempt.Runtime)
	if !retryAllowed || execution == MaxTaskExecutions || !eligible {
		task.State = GraphTaskBlocked
		task.BlockerCode = failure.BlockerCode
		if err := validateGraphTask(*task, "pending"); err != nil {
			return TaskFailureResult{}, err
		}
		*g = *candidate
		return TaskFailureResult{Blocked: true}, nil
	}
	wave := DeliveryWave{
		Number: len(candidate.Waves) + 1, BaseHeadSHA: nextBaseHeadSHA, TaskIDs: []string{taskID},
	}
	candidate.Waves = append(candidate.Waves, cloneDeliveryWave(wave))
	task.Attempts = append(task.Attempts, GraphTaskAttempt{
		Execution: execution + 1, RunExecution: execution + 1,
		Runtime: nextRuntime, State: GraphTaskPreparing, BaseHeadSHA: nextBaseHeadSHA,
	})
	task.State = GraphTaskPreparing
	if err := validateGraphTask(*task, "pending"); err != nil {
		return TaskFailureResult{}, err
	}
	*g = *candidate
	return TaskFailureResult{Wave: wave, Runtime: nextRuntime}, nil
}

func (g *DeliveryGraph) RecordCandidate(taskID string, execution int, evidence TaskCandidate) (bool, error) {
	if g == nil || !boundedArgument(taskID) || execution < 1 || execution > MaxTaskExecutions ||
		!boundedArgument(evidence.ChildRunID) || !canonicalGitSHA.MatchString(evidence.BaseHeadSHA) ||
		!canonicalGitSHA.MatchString(evidence.CommitSHA) || !canonicalSHA256.MatchString(evidence.VerificationDigest) ||
		evidence.TokensUsed < 0 ||
		(evidence.Evidence != nil && !validTaskCandidateEvidence(evidence.Evidence, taskID, evidence.VerificationDigest)) {
		return false, ErrInvalidDeliveryTransition
	}
	candidate := cloneDeliveryGraph(g)
	task := graphTaskByID(candidate.Tasks, taskID)
	if task == nil || execution > len(task.Attempts) {
		return false, ErrInvalidDeliveryTransition
	}
	attempt := &task.Attempts[execution-1]
	if attempt.TokenAllowance > 0 && evidence.TokensUsed > attempt.TokenAllowance {
		return false, ErrInvalidDeliveryTransition
	}
	if attempt.State == GraphTaskCandidate || attempt.State == GraphTaskIntegrated {
		if attempt.ChildRunID != evidence.ChildRunID || attempt.BaseHeadSHA != evidence.BaseHeadSHA ||
			attempt.CandidateCommitSHA != evidence.CommitSHA || attempt.VerificationDigest != evidence.VerificationDigest ||
			attempt.TokensUsed == nil || *attempt.TokensUsed != evidence.TokensUsed ||
			!reflect.DeepEqual(attempt.CandidateEvidence, evidence.Evidence) {
			return false, ErrInvalidDeliveryTransition
		}
		return true, nil
	}
	if len(task.Attempts) != execution {
		return false, ErrInvalidDeliveryTransition
	}
	if task.State != GraphTaskRunning || attempt.State != GraphTaskRunning ||
		attempt.BaseHeadSHA != evidence.BaseHeadSHA ||
		(attempt.ChildRunID != "" && attempt.ChildRunID != evidence.ChildRunID) {
		return false, ErrInvalidDeliveryTransition
	}
	attempt.ChildRunID = evidence.ChildRunID
	attempt.State = GraphTaskCandidate
	attempt.CandidateCommitSHA = evidence.CommitSHA
	attempt.VerificationDigest = evidence.VerificationDigest
	attempt.CandidateEvidence = cloneTaskCandidateEvidence(evidence.Evidence)
	tokensUsed := evidence.TokensUsed
	attempt.TokensUsed = &tokensUsed
	task.State = GraphTaskCandidate
	if err := validateGraphTask(*task, "pending"); err != nil {
		return false, err
	}
	*g = *candidate
	return false, nil
}

func (g *DeliveryGraph) SettleWave(
	settlement WaveSettlement,
	generation RoutingGeneration,
	retryPolicy ...bool,
) (WaveSettlementResult, error) {
	if g == nil || !validWaveSettlement(settlement, len(g.Waves)) || len(retryPolicy) > 1 {
		return WaveSettlementResult{}, ErrInvalidDeliveryTransition
	}
	retryAllowed := true
	if len(retryPolicy) == 1 {
		retryAllowed = retryPolicy[0]
	}
	recomputed, err := finalizeGeneration(generation)
	if err != nil || recomputed.Digest != generation.Digest {
		return WaveSettlementResult{}, ErrInvalidDeliveryGraph
	}
	operation := integrationOperationFromSettlement(settlement)
	for _, existing := range g.Integrations {
		if existing.OperationID != settlement.OperationID {
			continue
		}
		if !reflect.DeepEqual(existing, operation) {
			return WaveSettlementResult{}, ErrInvalidDeliveryTransition
		}
		result, resultErr := settlementReplayResult(g, existing)
		result.Replayed = true
		return result, resultErr
	}
	wave := g.Waves[settlement.Wave-1]
	expectedStartingHead := wave.BaseHeadSHA
	if len(g.Integrations) > 0 {
		expectedStartingHead = g.Integrations[len(g.Integrations)-1].FinalHeadSHA
	}
	if expectedStartingHead != settlement.StartingHeadSHA {
		return WaveSettlementResult{}, ErrInvalidDeliveryTransition
	}
	candidate := cloneDeliveryGraph(g)
	candidateWave := candidate.Waves[settlement.Wave-1]
	expectedTaskIDs := make([]string, 0, len(candidateWave.TaskIDs))
	expectedCommits := make([]string, 0, len(candidateWave.TaskIDs))
	for _, taskID := range candidateWave.TaskIDs {
		task := graphTaskByID(candidate.Tasks, taskID)
		if task == nil || len(task.Attempts) == 0 {
			return WaveSettlementResult{}, ErrInvalidDeliveryTransition
		}
		attempt := task.Attempts[len(task.Attempts)-1]
		if task.State == GraphTaskCandidate && attempt.State == GraphTaskCandidate && attempt.Conflict == nil {
			expectedTaskIDs = append(expectedTaskIDs, taskID)
			expectedCommits = append(expectedCommits, attempt.CandidateCommitSHA)
		}
	}
	if !candidateSettlementSnapshot(expectedTaskIDs, expectedCommits, settlement.OrderedTaskIDs, settlement.CandidateCommitSHAs) {
		return WaveSettlementResult{}, ErrInvalidDeliveryTransition
	}
	for index, taskID := range settlement.AcceptedTaskIDs {
		task := graphTaskByID(candidate.Tasks, taskID)
		attempt := &task.Attempts[len(task.Attempts)-1]
		if attempt.CandidateCommitSHA != settlement.AcceptedCommitSHAs[index] {
			return WaveSettlementResult{}, ErrInvalidDeliveryTransition
		}
		attempt.State = GraphTaskIntegrated
		task.State = GraphTaskIntegrated
		task.IntegratedCommitSHA = settlement.IntegratedCommitSHAs[index]
	}
	candidate.Integrations = append(candidate.Integrations, cloneIntegrationOperation(operation))
	result := WaveSettlementResult{Disposition: SettlementWaveIntegrated}
	if settlement.FirstConflictTaskID != "" {
		task := graphTaskByID(candidate.Tasks, settlement.FirstConflictTaskID)
		if task == nil || len(task.Attempts) == 0 {
			return WaveSettlementResult{}, ErrInvalidDeliveryTransition
		}
		attempt := &task.Attempts[len(task.Attempts)-1]
		attempt.Conflict = &ConflictProof{
			IntegrationOperationID: settlement.OperationID, IntegrationHeadSHA: settlement.FinalHeadSHA,
			CandidateCommitSHA: attempt.CandidateCommitSHA, EvidenceDigest: settlement.ConflictEvidenceDigest,
		}
		nextRuntime, eligible := nextRuntimeForTask(generation, *task, attempt.Runtime)
		if !retryAllowed || !eligible || attempt.Execution == MaxTaskExecutions {
			attempt.State = GraphTaskBlocked
			attempt.BlockerCode = "integration_conflict_exhausted"
			task.State = GraphTaskBlocked
			task.BlockerCode = attempt.BlockerCode
			result.Disposition = SettlementBlocked
			result.TaskID = task.TaskID
		} else {
			nextWave := DeliveryWave{
				Number: len(candidate.Waves) + 1, BaseHeadSHA: settlement.FinalHeadSHA, TaskIDs: []string{task.TaskID},
			}
			candidate.Waves = append(candidate.Waves, cloneDeliveryWave(nextWave))
			task.Attempts = append(task.Attempts, GraphTaskAttempt{
				Execution: attempt.Execution + 1, RunExecution: attempt.Execution + 1,
				Runtime: nextRuntime, State: GraphTaskPreparing,
				BaseHeadSHA: settlement.FinalHeadSHA,
			})
			task.State = GraphTaskPreparing
			result.Disposition = SettlementReexecuteConflict
			result.TaskID = task.TaskID
			result.Wave = nextWave
			result.Runtime = nextRuntime
		}
	} else if allGraphTasksIntegrated(candidate.Tasks) {
		result.Disposition = SettlementAllIntegrated
	}
	if err := validateGraphShape(candidate); err != nil {
		return WaveSettlementResult{}, err
	}
	*g = *candidate
	return result, nil
}

func candidateSettlementSnapshot(allTaskIDs, allCommits, settledTaskIDs, settledCommits []string) bool {
	if len(settledTaskIDs) == 0 || len(settledTaskIDs) != len(settledCommits) || len(allTaskIDs) != len(allCommits) {
		return false
	}
	next := 0
	for index, taskID := range allTaskIDs {
		if next == len(settledTaskIDs) {
			break
		}
		if taskID == settledTaskIDs[next] && allCommits[index] == settledCommits[next] {
			next++
		}
	}
	return next == len(settledTaskIDs)
}

func (g *DeliveryGraph) RecordCleanup(proposed CleanupOperation) (bool, error) {
	if g == nil || !validCleanupOperation(proposed) {
		return false, ErrInvalidDeliveryTransition
	}
	for _, existing := range g.Cleanups {
		if existing.OperationID != proposed.OperationID {
			continue
		}
		if reflect.DeepEqual(existing, proposed) {
			return true, nil
		}
		return false, ErrInvalidDeliveryTransition
	}
	task := graphTaskByID(g.Tasks, proposed.TaskID)
	if task == nil || proposed.Execution > len(task.Attempts) ||
		task.Attempts[proposed.Execution-1].WorktreeID != proposed.WorktreeID ||
		proposed.State == CleanupRemoved ||
		(proposed.State == CleanupPlanned && task.State != GraphTaskIntegrated) {
		return false, ErrInvalidDeliveryTransition
	}
	g.Cleanups = append(g.Cleanups, proposed)
	return false, nil
}

func (g *DeliveryGraph) CompleteCleanup(
	operationID string,
	state CleanupState,
	blockerCode string,
) (CleanupOperation, bool, error) {
	if g == nil || !canonicalSHA256.MatchString(operationID) ||
		(state != CleanupRemoved && state != CleanupRetained) {
		return CleanupOperation{}, false, ErrInvalidDeliveryTransition
	}
	for index := range g.Cleanups {
		operation := &g.Cleanups[index]
		if operation.OperationID != operationID {
			continue
		}
		if operation.State == state && operation.BlockerCode == blockerCode {
			return *operation, true, nil
		}
		if operation.State != CleanupPlanned ||
			(state == CleanupRemoved && blockerCode != "") ||
			(state == CleanupRetained && !boundedArgument(blockerCode)) {
			return CleanupOperation{}, false, ErrInvalidDeliveryTransition
		}
		operation.State = state
		operation.BlockerCode = blockerCode
		return *operation, false, nil
	}
	return CleanupOperation{}, false, ErrInvalidDeliveryTransition
}

func (g *DeliveryGraph) OpenPause(proposed HumanPause) (HumanPause, bool, error) {
	if g == nil || !boundedArgument(proposed.TaskID) || proposed.Execution < 1 ||
		proposed.Execution > MaxTaskExecutions || !boundedArgument(proposed.RequestID) ||
		proposed.StartedAt.IsZero() || proposed.StartedAt.Location() != time.UTC || proposed.EndedAt != nil {
		return HumanPause{}, false, ErrInvalidActiveWall
	}
	for _, existing := range g.Pauses {
		if existing.RequestID != proposed.RequestID {
			continue
		}
		if reflect.DeepEqual(existing, proposed) {
			return cloneHumanPause(existing), true, nil
		}
		if existing.EndedAt == nil {
			return HumanPause{}, false, ErrInvalidActiveWall
		}
	}
	task := graphTaskByID(g.Tasks, proposed.TaskID)
	if task == nil || task.State != GraphTaskWaitingInput || len(task.Attempts) == 0 {
		return HumanPause{}, false, ErrInvalidActiveWall
	}
	attempt := task.Attempts[len(task.Attempts)-1]
	if attempt.Execution != proposed.Execution || attempt.Question == nil ||
		attempt.Question.RequestID != proposed.RequestID {
		return HumanPause{}, false, ErrInvalidActiveWall
	}
	if len(g.Pauses) > 0 {
		last := g.Pauses[len(g.Pauses)-1]
		if last.EndedAt == nil || proposed.StartedAt.Before(*last.EndedAt) {
			return HumanPause{}, false, ErrInvalidActiveWall
		}
	}
	g.Pauses = append(g.Pauses, cloneHumanPause(proposed))
	return cloneHumanPause(proposed), false, nil
}

func (g *DeliveryGraph) ClosePause(requestID string, endedAt time.Time) (HumanPause, bool, error) {
	if g == nil || !boundedArgument(requestID) || endedAt.IsZero() || endedAt.Location() != time.UTC {
		return HumanPause{}, false, ErrInvalidActiveWall
	}
	for index := len(g.Pauses) - 1; index >= 0; index-- {
		pause := &g.Pauses[index]
		if pause.RequestID != requestID {
			continue
		}
		if pause.EndedAt != nil {
			if pause.EndedAt.Equal(endedAt) {
				return cloneHumanPause(*pause), true, nil
			}
			return HumanPause{}, false, ErrInvalidActiveWall
		}
		if endedAt.Before(pause.StartedAt) {
			return HumanPause{}, false, ErrInvalidActiveWall
		}
		pause.EndedAt = cloneTime(&endedAt)
		return cloneHumanPause(*pause), false, nil
	}
	return HumanPause{}, false, ErrInvalidActiveWall
}

func (g *DeliveryGraph) ReconcileHumanPause(at time.Time) (bool, error) {
	if g == nil || at.IsZero() || at.Location() != time.UTC {
		return false, ErrInvalidActiveWall
	}
	var open *HumanPause
	for index := range g.Pauses {
		if g.Pauses[index].EndedAt == nil {
			if open != nil {
				return false, ErrInvalidActiveWall
			}
			open = &g.Pauses[index]
		}
	}
	if !graphEntirelyWaitingForHuman(g.Tasks) {
		if open == nil {
			return false, nil
		}
		if _, replay, err := g.ClosePause(open.RequestID, at); err != nil || replay {
			return false, ErrInvalidActiveWall
		}
		return true, nil
	}
	if open != nil {
		return false, nil
	}
	for index := len(g.Tasks) - 1; index >= 0; index-- {
		task := g.Tasks[index]
		if task.State != GraphTaskWaitingInput || len(task.Attempts) == 0 {
			continue
		}
		attempt := task.Attempts[len(task.Attempts)-1]
		if attempt.Question == nil || attempt.Question.Answer != nil {
			return false, ErrInvalidActiveWall
		}
		if _, replay, err := g.OpenPause(HumanPause{
			TaskID: task.TaskID, Execution: attempt.Execution,
			RequestID: attempt.Question.RequestID, StartedAt: at,
		}); err != nil || replay {
			return false, ErrInvalidActiveWall
		}
		return true, nil
	}
	return false, ErrInvalidActiveWall
}

func (d DeliveryRecord) RemainingActiveWall(now time.Time) (time.Duration, error) {
	if now.IsZero() || now.Location() != time.UTC || now.Before(d.CreatedAt) || d.CreatedAt.IsZero() ||
		d.AbsoluteDeadline.IsZero() || !d.AbsoluteDeadline.Equal(d.CreatedAt.Add(4*time.Hour)) {
		return 0, ErrInvalidActiveWall
	}
	paused := time.Duration(0)
	if d.Graph != nil {
		var err error
		paused, err = validatedPauseDuration(d.Graph, d.CreatedAt, now)
		if err != nil {
			return 0, err
		}
	}
	active := now.Sub(d.CreatedAt) - paused
	if active < 0 {
		return 0, ErrInvalidActiveWall
	}
	remaining := d.AbsoluteDeadline.Sub(d.CreatedAt) - active
	if remaining < 0 {
		return 0, nil
	}
	return remaining, nil
}

func validatedPauseDuration(graph *DeliveryGraph, createdAt, now time.Time) (time.Duration, error) {
	type intervalIdentity struct {
		requestID string
		startedAt time.Time
	}
	seen := make(map[intervalIdentity]struct{}, len(graph.Pauses))
	total := time.Duration(0)
	var priorEnd *time.Time
	for index, pause := range graph.Pauses {
		if !boundedArgument(pause.TaskID) || pause.Execution < 1 || pause.Execution > MaxTaskExecutions ||
			!boundedArgument(pause.RequestID) || pause.StartedAt.IsZero() || pause.StartedAt.Location() != time.UTC ||
			pause.StartedAt.Before(createdAt) || pause.StartedAt.After(now) {
			return 0, ErrInvalidActiveWall
		}
		identity := intervalIdentity{requestID: pause.RequestID, startedAt: pause.StartedAt}
		if _, duplicate := seen[identity]; duplicate {
			return 0, ErrInvalidActiveWall
		}
		seen[identity] = struct{}{}
		if priorEnd != nil && pause.StartedAt.Before(*priorEnd) {
			return 0, ErrInvalidActiveWall
		}
		end := now
		if pause.EndedAt != nil {
			if pause.EndedAt.Location() != time.UTC || pause.EndedAt.Before(pause.StartedAt) || pause.EndedAt.After(now) {
				return 0, ErrInvalidActiveWall
			}
			end = *pause.EndedAt
			priorEnd = cloneTime(pause.EndedAt)
		} else {
			if index != len(graph.Pauses)-1 {
				return 0, ErrInvalidActiveWall
			}
			priorEnd = cloneTime(&now)
		}
		total += end.Sub(pause.StartedAt)
	}
	return total, nil
}

func validateGraphShape(graph *DeliveryGraph) error {
	if graph == nil || len(graph.Tasks) == 0 || len(graph.Tasks) > MaxDeliveryTasks ||
		graph.Tasks == nil || graph.Waves == nil || graph.Integrations == nil || graph.Pauses == nil {
		return ErrInvalidDeliveryGraph
	}
	byID := make(map[string]*GraphTask, len(graph.Tasks))
	for index := range graph.Tasks {
		task := &graph.Tasks[index]
		if !boundedArgument(task.TaskID) || task.AuthoredIndex != index || !task.Domain.Valid() ||
			!task.Complexity.Valid() || !task.State.valid() || task.Dependencies == nil || task.Attempts == nil {
			return ErrInvalidDeliveryGraph
		}
		if _, duplicate := byID[task.TaskID]; duplicate {
			return ErrInvalidDeliveryGraph
		}
		byID[task.TaskID] = task
	}
	for _, task := range graph.Tasks {
		seenDependencies := make(map[string]struct{}, len(task.Dependencies))
		for _, dependency := range task.Dependencies {
			if dependency == task.TaskID || byID[dependency] == nil {
				return ErrInvalidDeliveryGraph
			}
			if _, duplicate := seenDependencies[dependency]; duplicate {
				return ErrInvalidDeliveryGraph
			}
			seenDependencies[dependency] = struct{}{}
		}
	}
	if !graphOrderIsTopological(graph.Tasks) || activeGraphTaskCount(graph.Tasks) > MaxParallelTasks {
		return ErrInvalidDeliveryGraph
	}
	return nil
}

func validateDeliveryGraph(
	graph *DeliveryGraph,
	snapshot DeliveryTaskSnapshot,
	generation RoutingGeneration,
	createdAt time.Time,
	initialHeadSHA string,
) error {
	if err := validateGraphShape(graph); err != nil || len(graph.Tasks) != len(snapshot.Tasks) {
		return ErrInvalidDeliveryGraph
	}
	generationTasks := make(map[string]GenerationTask, len(generation.Tasks))
	for _, task := range generation.Tasks {
		if _, duplicate := generationTasks[task.ID]; duplicate {
			return ErrInvalidDeliveryGraph
		}
		generationTasks[task.ID] = task
	}
	for index, task := range graph.Tasks {
		authored := snapshot.Tasks[index]
		generationTask, exists := generationTasks[task.TaskID]
		if task.TaskID != authored.ID || task.AuthoredIndex != index ||
			!slices.Equal(task.Dependencies, authored.Dependencies) || task.Domain != authored.Domain ||
			task.Complexity != authored.Complexity || !exists || generationTask.Domain != task.Domain ||
			generationTask.Complexity != task.Complexity || validateGraphTask(task, authored.Status) != nil {
			return ErrInvalidDeliveryGraph
		}
		if authored.Status == "completed" && task.IntegratedCommitSHA != initialHeadSHA {
			return ErrInvalidDeliveryGraph
		}
	}
	if err := validateDeliveryWaves(graph.Waves, graph.Tasks); err != nil {
		return err
	}
	appearances := graphWaveAppearances(graph.Waves)
	for index, task := range graph.Tasks {
		count := appearances[task.TaskID]
		if snapshot.Tasks[index].Status == "completed" {
			if count != 0 {
				return ErrInvalidDeliveryGraph
			}
			continue
		}
		if task.State == GraphTaskPending {
			if count != 0 || len(task.Attempts) != 0 {
				return ErrInvalidDeliveryGraph
			}
			continue
		}
		attemptWaves, err := graphTaskAttemptWaveCount(task)
		if err != nil || count < attemptWaves || count > attemptWaves+1 ||
			(count == attemptWaves+1 && task.State != GraphTaskPreparing && task.State != GraphTaskBlocked) ||
			(count == 0 && task.State != GraphTaskBlocked) {
			return ErrInvalidDeliveryGraph
		}
	}
	if err := validateIntegrationOperations(graph.Integrations, graph.Waves); err != nil {
		return err
	}
	if err := validatePauseArchive(graph, createdAt); err != nil {
		return err
	}
	if err := validateCleanupOperations(graph); err != nil {
		return err
	}
	return nil
}

func validateGraphTask(task GraphTask, authoredStatus string) error {
	if len(task.Attempts) > MaxTaskExecutions {
		return ErrInvalidDeliveryGraph
	}
	switch authoredStatus {
	case "completed":
		if task.State != GraphTaskIntegrated || !canonicalGitSHA.MatchString(task.IntegratedCommitSHA) ||
			len(task.Attempts) != 0 || task.BlockerCode != "" {
			return ErrInvalidDeliveryGraph
		}
		return nil
	case "pending":
	default:
		return ErrInvalidDeliveryGraph
	}
	for index, attempt := range task.Attempts {
		if err := validateGraphTaskAttempt(attempt, index+1, task.TaskID); err != nil {
			return err
		}
		if index > 0 {
			prior := task.Attempts[index-1]
			continuedAfterAnswer := prior.State == GraphTaskRunning && prior.Question != nil && prior.Question.Answer != nil &&
				graphAttemptRunExecution(attempt) == graphAttemptRunExecution(prior) &&
				attempt.Runtime == prior.Runtime && attempt.BaseHeadSHA == prior.BaseHeadSHA &&
				reflect.DeepEqual(attempt.WorktreeIntent, prior.WorktreeIntent) &&
				attempt.WorktreeID == prior.WorktreeID && attempt.WorktreeRoot == prior.WorktreeRoot &&
				attempt.ChildRunID == prior.ChildRunID
			reexecutedConflict := prior.State == GraphTaskCandidate && prior.Conflict != nil
			retriedFailure := prior.State == GraphTaskBlocked && prior.TokensUsed != nil && validTaskTerminalStatus(prior.TerminalStatus)
			if !continuedAfterAnswer && !reexecutedConflict && !retriedFailure {
				return ErrInvalidDeliveryGraph
			}
		}
	}
	if len(task.Attempts) == 0 {
		if task.State != GraphTaskPending && task.State != GraphTaskPreparing && task.State != GraphTaskBlocked {
			return ErrInvalidDeliveryGraph
		}
	} else if task.Attempts[len(task.Attempts)-1].State != task.State {
		return ErrInvalidDeliveryGraph
	}
	if task.State == GraphTaskIntegrated {
		last := task.Attempts[len(task.Attempts)-1]
		if !canonicalGitSHA.MatchString(task.IntegratedCommitSHA) || last.State != GraphTaskIntegrated ||
			!canonicalGitSHA.MatchString(last.CandidateCommitSHA) || task.BlockerCode != "" {
			return ErrInvalidDeliveryGraph
		}
	} else if task.IntegratedCommitSHA != "" {
		return ErrInvalidDeliveryGraph
	}
	if task.State == GraphTaskBlocked {
		if !boundedArgument(task.BlockerCode) {
			return ErrInvalidDeliveryGraph
		}
	} else if task.BlockerCode != "" {
		return ErrInvalidDeliveryGraph
	}
	return nil
}

func validateGraphTaskAttempt(attempt GraphTaskAttempt, expectedExecution int, taskID string) error {
	if attempt.Execution != expectedExecution || expectedExecution < 1 || expectedExecution > MaxTaskExecutions ||
		graphAttemptRunExecution(attempt) < 1 || graphAttemptRunExecution(attempt) > attempt.Execution ||
		!boundedArgument(attempt.Runtime.Provider) || !boundedArgument(attempt.Runtime.Model) ||
		!boundedArgument(attempt.Runtime.Reasoning) || !canonicalGitSHA.MatchString(attempt.BaseHeadSHA) ||
		attempt.State == GraphTaskPending || !attempt.State.valid() {
		return ErrInvalidDeliveryGraph
	}
	if attempt.WorktreeRoot != "" && (!filepath.IsAbs(attempt.WorktreeRoot) || filepath.Clean(attempt.WorktreeRoot) != attempt.WorktreeRoot) {
		return ErrInvalidDeliveryGraph
	}
	if attempt.TokenAllowance < 0 || attempt.TokensUsed != nil && (*attempt.TokensUsed < 0 ||
		attempt.TokenAllowance > 0 && *attempt.TokensUsed > attempt.TokenAllowance) {
		return ErrInvalidDeliveryGraph
	}
	if (attempt.WorktreeID == "") != (attempt.WorktreeRoot == "") ||
		(attempt.WorktreeID != "" && !boundedArgument(attempt.WorktreeID)) ||
		(attempt.ChildRunID != "" && !boundedArgument(attempt.ChildRunID)) {
		return ErrInvalidDeliveryGraph
	}
	if attempt.WorktreeIntent != nil && !validTaskWorktreeIntent(attempt.WorktreeIntent) {
		return ErrInvalidDeliveryGraph
	}
	if attempt.Question != nil && !validTaskQuestion(attempt.Question) {
		return ErrInvalidDeliveryGraph
	}
	if attempt.Conflict != nil && !validConflictProof(attempt.Conflict, attempt.CandidateCommitSHA) {
		return ErrInvalidDeliveryGraph
	}
	if attempt.CandidateEvidence != nil &&
		!validTaskCandidateEvidence(attempt.CandidateEvidence, taskID, attempt.VerificationDigest) {
		return ErrInvalidDeliveryGraph
	}
	switch attempt.State {
	case GraphTaskPreparing:
		if attempt.ChildRunID != "" || attempt.CandidateCommitSHA != "" ||
			attempt.VerificationDigest != "" || attempt.CandidateEvidence != nil || attempt.Question != nil || attempt.Conflict != nil ||
			attempt.TokensUsed != nil || attempt.TerminalStatus != "" || attempt.BlockerCode != "" {
			return ErrInvalidDeliveryGraph
		}
	case GraphTaskRunning:
		if attempt.WorktreeID == "" || attempt.CandidateCommitSHA != "" ||
			attempt.VerificationDigest != "" || attempt.CandidateEvidence != nil || attempt.Conflict != nil || attempt.TokensUsed != nil ||
			attempt.TerminalStatus != "" || attempt.BlockerCode != "" {
			return ErrInvalidDeliveryGraph
		}
		if attempt.Question != nil && (attempt.Question.Answer == nil || !boundedArgument(attempt.ChildRunID)) {
			return ErrInvalidDeliveryGraph
		}
	case GraphTaskWaitingInput:
		if attempt.WorktreeID == "" || !boundedArgument(attempt.ChildRunID) || !validTaskQuestion(attempt.Question) ||
			attempt.Question.Answer != nil ||
			attempt.CandidateCommitSHA != "" || attempt.VerificationDigest != "" || attempt.CandidateEvidence != nil || attempt.Conflict != nil ||
			attempt.TokensUsed != nil || attempt.TerminalStatus != "" || attempt.BlockerCode != "" {
			return ErrInvalidDeliveryGraph
		}
	case GraphTaskCandidate, GraphTaskIntegrated:
		if attempt.WorktreeID == "" || !boundedArgument(attempt.ChildRunID) ||
			!canonicalGitSHA.MatchString(attempt.CandidateCommitSHA) ||
			!canonicalSHA256.MatchString(attempt.VerificationDigest) || attempt.TerminalStatus != "" || attempt.BlockerCode != "" {
			return ErrInvalidDeliveryGraph
		}
	case GraphTaskBlocked:
		terminalFailure := attempt.TerminalStatus != ""
		conflictExhausted := attempt.Conflict != nil && canonicalGitSHA.MatchString(attempt.CandidateCommitSHA) &&
			canonicalSHA256.MatchString(attempt.VerificationDigest) && attempt.TokensUsed != nil && attempt.TerminalStatus == ""
		if !boundedArgument(attempt.BlockerCode) || (attempt.CandidateEvidence != nil && !conflictExhausted) ||
			(!conflictExhausted && (attempt.CandidateCommitSHA != "" || attempt.VerificationDigest != "" || attempt.Conflict != nil)) ||
			(terminalFailure && (!boundedArgument(attempt.ChildRunID) || attempt.TokensUsed == nil ||
				!validTaskTerminalStatus(attempt.TerminalStatus))) ||
			(!terminalFailure && attempt.TokensUsed != nil && !conflictExhausted) ||
			(conflictExhausted && attempt.TerminalStatus != "") {
			return ErrInvalidDeliveryGraph
		}
	default:
		return ErrInvalidDeliveryGraph
	}
	return nil
}

func validConflictProof(proof *ConflictProof, candidateCommit string) bool {
	return proof != nil && canonicalSHA256.MatchString(proof.IntegrationOperationID) &&
		canonicalGitSHA.MatchString(proof.IntegrationHeadSHA) &&
		canonicalGitSHA.MatchString(proof.CandidateCommitSHA) && proof.CandidateCommitSHA == candidateCommit &&
		canonicalSHA256.MatchString(proof.EvidenceDigest)
}

func validateDeliveryWaves(waves []DeliveryWave, tasks []GraphTask) error {
	tasksByID := graphTasksByID(tasks)
	appearances := make(map[string]int, len(tasks))
	for index, wave := range waves {
		if wave.Number != index+1 || !canonicalGitSHA.MatchString(wave.BaseHeadSHA) ||
			len(wave.TaskIDs) == 0 || len(wave.TaskIDs) > MaxParallelTasks {
			return ErrInvalidDeliveryGraph
		}
		seen := make(map[string]struct{}, len(wave.TaskIDs))
		for _, taskID := range wave.TaskIDs {
			if tasksByID[taskID] == nil {
				return ErrInvalidDeliveryGraph
			}
			if _, duplicate := seen[taskID]; duplicate {
				return ErrInvalidDeliveryGraph
			}
			seen[taskID] = struct{}{}
			appearances[taskID]++
			if appearances[taskID] > MaxTaskExecutions {
				return ErrInvalidDeliveryGraph
			}
		}
	}
	return nil
}

func graphWaveAppearances(waves []DeliveryWave) map[string]int {
	appearances := make(map[string]int)
	for _, wave := range waves {
		for _, taskID := range wave.TaskIDs {
			appearances[taskID]++
		}
	}
	return appearances
}

func graphTaskAttemptWaveCount(task GraphTask) (int, error) {
	if len(task.Attempts) == 0 {
		return 0, nil
	}
	count := 1
	for index := 1; index < len(task.Attempts); index++ {
		prior := task.Attempts[index-1]
		switch {
		case prior.State == GraphTaskRunning && prior.Question != nil && prior.Question.Answer != nil:
		case prior.State == GraphTaskCandidate && prior.Conflict != nil:
			count++
		case prior.State == GraphTaskBlocked && prior.TokensUsed != nil && validTaskTerminalStatus(prior.TerminalStatus):
			count++
		default:
			return 0, ErrInvalidDeliveryGraph
		}
	}
	return count, nil
}

func validateIntegrationOperations(operations []IntegrationOperation, waves []DeliveryWave) error {
	seen := make(map[string]struct{}, len(operations))
	for _, operation := range operations {
		if !canonicalSHA256.MatchString(operation.OperationID) || !canonicalSHA256.MatchString(operation.RequestDigest) ||
			operation.Wave < 1 || operation.Wave > len(waves) ||
			!canonicalGitSHA.MatchString(operation.StartingHeadSHA) || !canonicalGitSHA.MatchString(operation.FinalHeadSHA) ||
			len(operation.OrderedTaskIDs) == 0 || len(operation.OrderedTaskIDs) > MaxParallelTasks ||
			len(operation.CandidateCommitSHAs) != len(operation.OrderedTaskIDs) ||
			len(operation.AcceptedTaskIDs) != len(operation.AcceptedCommitSHAs) ||
			len(operation.AcceptedTaskIDs) != len(operation.IntegratedCommitSHAs) ||
			len(operation.AcceptedTaskIDs) > len(operation.OrderedTaskIDs) {
			return ErrInvalidDeliveryGraph
		}
		if _, duplicate := seen[operation.OperationID]; duplicate {
			return ErrInvalidDeliveryGraph
		}
		seen[operation.OperationID] = struct{}{}
		for index, taskID := range operation.OrderedTaskIDs {
			if !boundedArgument(taskID) || !canonicalGitSHA.MatchString(operation.CandidateCommitSHAs[index]) {
				return ErrInvalidDeliveryGraph
			}
			if index < len(operation.AcceptedTaskIDs) {
				if operation.AcceptedTaskIDs[index] != taskID || operation.AcceptedCommitSHAs[index] != operation.CandidateCommitSHAs[index] ||
					!canonicalGitSHA.MatchString(operation.IntegratedCommitSHAs[index]) {
					return ErrInvalidDeliveryGraph
				}
			}
		}
		if operation.ConflictingTaskID != "" {
			accepted := len(operation.AcceptedTaskIDs)
			if accepted >= len(operation.OrderedTaskIDs) || operation.ConflictingTaskID != operation.OrderedTaskIDs[accepted] ||
				!canonicalSHA256.MatchString(operation.ConflictEvidenceDigest) {
				return ErrInvalidDeliveryGraph
			}
		} else if operation.ConflictEvidenceDigest != "" || len(operation.AcceptedTaskIDs) != len(operation.OrderedTaskIDs) {
			return ErrInvalidDeliveryGraph
		}
	}
	return nil
}

func validatePauseArchive(graph *DeliveryGraph, createdAt time.Time) error {
	type intervalIdentity struct {
		requestID string
		startedAt time.Time
	}
	seen := make(map[intervalIdentity]struct{}, len(graph.Pauses))
	var priorEnd *time.Time
	for index, pause := range graph.Pauses {
		if !boundedArgument(pause.TaskID) || pause.Execution < 1 || pause.Execution > MaxTaskExecutions ||
			!boundedArgument(pause.RequestID) || pause.StartedAt.IsZero() || pause.StartedAt.Location() != time.UTC ||
			pause.StartedAt.Before(createdAt) {
			return ErrInvalidActiveWall
		}
		identity := intervalIdentity{requestID: pause.RequestID, startedAt: pause.StartedAt}
		if _, duplicate := seen[identity]; duplicate {
			return ErrInvalidActiveWall
		}
		seen[identity] = struct{}{}
		if priorEnd != nil && pause.StartedAt.Before(*priorEnd) {
			return ErrInvalidActiveWall
		}
		task := graphTaskByID(graph.Tasks, pause.TaskID)
		if task == nil || pause.Execution > len(task.Attempts) {
			return ErrInvalidActiveWall
		}
		question := task.Attempts[pause.Execution-1].Question
		if question == nil || question.RequestID != pause.RequestID {
			return ErrInvalidActiveWall
		}
		if pause.EndedAt == nil {
			if index != len(graph.Pauses)-1 || task.State != GraphTaskWaitingInput {
				return ErrInvalidActiveWall
			}
			priorEnd = nil
			continue
		}
		if pause.EndedAt.Location() != time.UTC || pause.EndedAt.Before(pause.StartedAt) {
			return ErrInvalidActiveWall
		}
		priorEnd = pause.EndedAt
	}
	return nil
}

func validCleanupOperation(operation CleanupOperation) bool {
	if !canonicalSHA256.MatchString(operation.OperationID) || !canonicalSHA256.MatchString(operation.RequestDigest) ||
		!boundedArgument(operation.TaskID) || operation.Execution < 1 || operation.Execution > MaxTaskExecutions ||
		!boundedArgument(operation.WorktreeID) {
		return false
	}
	switch operation.State {
	case CleanupPlanned, CleanupRemoved:
		return operation.BlockerCode == ""
	case CleanupRetained:
		return boundedArgument(operation.BlockerCode)
	default:
		return false
	}
}

func validateCleanupOperations(graph *DeliveryGraph) error {
	seen := make(map[string]struct{}, len(graph.Cleanups))
	for _, operation := range graph.Cleanups {
		if !validCleanupOperation(operation) {
			return ErrInvalidDeliveryGraph
		}
		if _, duplicate := seen[operation.OperationID]; duplicate {
			return ErrInvalidDeliveryGraph
		}
		seen[operation.OperationID] = struct{}{}
		task := graphTaskByID(graph.Tasks, operation.TaskID)
		if task == nil || operation.Execution > len(task.Attempts) ||
			task.Attempts[operation.Execution-1].WorktreeID != operation.WorktreeID {
			return ErrInvalidDeliveryGraph
		}
	}
	return nil
}

func validateDeliveryGraphTransition(before, after *DeliveryGraph) error {
	if before == nil || after == nil || len(before.Tasks) != len(after.Tasks) ||
		len(after.Waves) < len(before.Waves) || len(after.Waves) > len(before.Waves)+1 ||
		len(after.Integrations) < len(before.Integrations) || len(after.Integrations) > len(before.Integrations)+1 ||
		len(after.Pauses) < len(before.Pauses) || len(after.Pauses) > len(before.Pauses)+1 ||
		len(after.Cleanups) < len(before.Cleanups) || len(after.Cleanups) > len(before.Cleanups)+1 {
		return ErrDeliveryConflict
	}
	if err := appendOnlyWaveTransition(before.Waves, after.Waves); err != nil {
		return err
	}
	if err := appendOnlyIntegrationTransition(before.Integrations, after.Integrations); err != nil {
		return err
	}
	if err := appendOnlyPauseTransition(before.Pauses, after.Pauses); err != nil {
		return err
	}
	if err := appendOnlyCleanupTransition(before.Cleanups, after.Cleanups); err != nil {
		return err
	}
	for index := range before.Tasks {
		if err := validateGraphTaskTransition(before.Tasks[index], after.Tasks[index]); err != nil {
			return err
		}
	}
	return nil
}

func validateGraphTaskTransition(before, after GraphTask) error {
	if before.TaskID != after.TaskID || before.AuthoredIndex != after.AuthoredIndex ||
		!slices.Equal(before.Dependencies, after.Dependencies) || before.Domain != after.Domain ||
		before.Complexity != after.Complexity {
		return ErrDeliveryConflict
	}
	if before.State == GraphTaskIntegrated || before.State == GraphTaskBlocked {
		if !reflect.DeepEqual(before, after) {
			return ErrInvalidDeliveryTransition
		}
		return nil
	}
	appendedAttempt := len(after.Attempts) == len(before.Attempts)+1
	stateAllowed := graphTaskTransitionAllowed(before.State, after.State) ||
		(appendedAttempt && before.State == GraphTaskRunning && after.State == GraphTaskPreparing)
	if !stateAllowed || len(after.Attempts) < len(before.Attempts) || len(after.Attempts) > len(before.Attempts)+1 {
		return ErrInvalidDeliveryTransition
	}
	if after.State == GraphTaskIntegrated {
		if before.IntegratedCommitSHA != "" || !canonicalGitSHA.MatchString(after.IntegratedCommitSHA) {
			return ErrDeliveryConflict
		}
	} else if before.IntegratedCommitSHA != after.IntegratedCommitSHA {
		return ErrDeliveryConflict
	}
	if after.State == GraphTaskBlocked {
		if before.BlockerCode != "" || !boundedArgument(after.BlockerCode) {
			return ErrDeliveryConflict
		}
	} else if before.BlockerCode != after.BlockerCode {
		return ErrDeliveryConflict
	}
	if len(after.Attempts) == len(before.Attempts) {
		for index := range before.Attempts {
			if index < len(before.Attempts)-1 {
				if !reflect.DeepEqual(before.Attempts[index], after.Attempts[index]) {
					return ErrDeliveryConflict
				}
				continue
			}
			if err := validateGraphAttemptTransition(before.Attempts[index], after.Attempts[index]); err != nil {
				return err
			}
		}
		return nil
	}
	if len(after.Attempts) == 0 || after.Attempts[len(after.Attempts)-1].Execution != len(after.Attempts) {
		return ErrDeliveryConflict
	}
	for index := range before.Attempts {
		if index < len(before.Attempts)-1 {
			if !reflect.DeepEqual(before.Attempts[index], after.Attempts[index]) {
				return ErrDeliveryConflict
			}
			continue
		}
		if before.Attempts[index].State == GraphTaskCandidate && before.Attempts[index].Conflict == nil {
			candidate := after.Attempts[index]
			withoutConflict := candidate
			withoutConflict.Conflict = nil
			if !reflect.DeepEqual(before.Attempts[index], withoutConflict) ||
				!validConflictProof(candidate.Conflict, candidate.CandidateCommitSHA) {
				return ErrDeliveryConflict
			}
		} else if before.Attempts[index].State == GraphTaskWaitingInput &&
			after.Attempts[index].State == GraphTaskRunning {
			candidate := after.Attempts[index]
			if candidate.Question == nil || candidate.Question.Answer == nil {
				return ErrDeliveryConflict
			}
			withoutAnswer := cloneGraphTaskAttempt(candidate)
			withoutAnswer.State = GraphTaskWaitingInput
			withoutAnswer.Question.Answer = nil
			if !reflect.DeepEqual(before.Attempts[index], withoutAnswer) {
				return ErrDeliveryConflict
			}
		} else if before.Attempts[index].State == GraphTaskRunning &&
			after.Attempts[index].State == GraphTaskBlocked {
			if err := validateGraphAttemptTransition(before.Attempts[index], after.Attempts[index]); err != nil {
				return err
			}
		} else if !reflect.DeepEqual(before.Attempts[index], after.Attempts[index]) {
			return ErrDeliveryConflict
		}
	}
	latest := after.Attempts[len(after.Attempts)-1]
	if len(after.Attempts) == 1 {
		if latest.State != GraphTaskPreparing {
			return ErrDeliveryConflict
		}
	} else {
		prior := after.Attempts[len(after.Attempts)-2]
		if prior.Question != nil && prior.Question.Answer != nil {
			if latest.State != GraphTaskRunning || graphAttemptRunExecution(latest) != graphAttemptRunExecution(prior) ||
				latest.Runtime != prior.Runtime || latest.BaseHeadSHA != prior.BaseHeadSHA ||
				!reflect.DeepEqual(latest.WorktreeIntent, prior.WorktreeIntent) ||
				latest.WorktreeID != prior.WorktreeID || latest.WorktreeRoot != prior.WorktreeRoot || latest.ChildRunID != prior.ChildRunID {
				return ErrDeliveryConflict
			}
		} else if latest.State != GraphTaskPreparing {
			return ErrDeliveryConflict
		}
	}
	return nil
}

func validateGraphAttemptTransition(before, after GraphTaskAttempt) error {
	if before.Execution != after.Execution || before.Runtime != after.Runtime || before.BaseHeadSHA != after.BaseHeadSHA ||
		graphAttemptRunExecution(before) != graphAttemptRunExecution(after) ||
		!graphTaskTransitionAllowed(before.State, after.State) {
		return ErrInvalidDeliveryTransition
	}
	if before.WorktreeID != "" && (before.WorktreeID != after.WorktreeID || before.WorktreeRoot != after.WorktreeRoot) {
		return ErrDeliveryConflict
	}
	if before.WorktreeIntent != nil && !reflect.DeepEqual(before.WorktreeIntent, after.WorktreeIntent) {
		return ErrDeliveryConflict
	}
	if before.WorktreeIntent == nil && after.WorktreeIntent != nil &&
		(before.State != GraphTaskPreparing || after.State != GraphTaskPreparing || after.WorktreeID != "") {
		return ErrDeliveryConflict
	}
	if before.ChildRunID != "" && before.ChildRunID != after.ChildRunID {
		return ErrDeliveryConflict
	}
	if before.Question != nil && !reflect.DeepEqual(before.Question, after.Question) {
		return ErrDeliveryConflict
	}
	if before.CandidateCommitSHA != "" && (before.CandidateCommitSHA != after.CandidateCommitSHA ||
		before.VerificationDigest != after.VerificationDigest) {
		return ErrDeliveryConflict
	}
	if before.Conflict != nil && !reflect.DeepEqual(before.Conflict, after.Conflict) {
		return ErrDeliveryConflict
	}
	if before.CandidateEvidence != nil && !reflect.DeepEqual(before.CandidateEvidence, after.CandidateEvidence) {
		return ErrDeliveryConflict
	}
	if before.BlockerCode != "" && before.BlockerCode != after.BlockerCode {
		return ErrDeliveryConflict
	}
	return nil
}

func graphAttemptRunExecution(attempt GraphTaskAttempt) int {
	if attempt.RunExecution > 0 {
		return attempt.RunExecution
	}
	return attempt.Execution
}

func graphTaskTransitionAllowed(before, after GraphTaskState) bool {
	if before == after {
		return true
	}
	switch before {
	case GraphTaskPending:
		return after == GraphTaskPreparing || after == GraphTaskBlocked
	case GraphTaskPreparing:
		return after == GraphTaskRunning || after == GraphTaskBlocked
	case GraphTaskRunning:
		return after == GraphTaskWaitingInput || after == GraphTaskCandidate || after == GraphTaskBlocked
	case GraphTaskWaitingInput:
		return after == GraphTaskRunning || after == GraphTaskBlocked
	case GraphTaskCandidate:
		return after == GraphTaskIntegrated || after == GraphTaskPreparing || after == GraphTaskBlocked
	default:
		return false
	}
}

func appendOnlyWaveTransition(before, after []DeliveryWave) error {
	for index := range before {
		if !reflect.DeepEqual(before[index], after[index]) {
			return ErrDeliveryConflict
		}
	}
	return nil
}

func appendOnlyIntegrationTransition(before, after []IntegrationOperation) error {
	for index := range before {
		if !reflect.DeepEqual(before[index], after[index]) {
			return ErrDeliveryConflict
		}
	}
	return nil
}

func appendOnlyPauseTransition(before, after []HumanPause) error {
	for index := range before {
		if index == len(before)-1 && before[index].EndedAt == nil && after[index].EndedAt != nil {
			closed := after[index]
			closed.EndedAt = nil
			if reflect.DeepEqual(before[index], closed) {
				continue
			}
		}
		if !reflect.DeepEqual(before[index], after[index]) {
			return ErrDeliveryConflict
		}
	}
	return nil
}

func appendOnlyCleanupTransition(before, after []CleanupOperation) error {
	for index := range before {
		if reflect.DeepEqual(before[index], after[index]) {
			continue
		}
		if index != len(before)-1 || before[index].State != CleanupPlanned ||
			(after[index].State != CleanupRemoved && after[index].State != CleanupRetained) {
			return ErrDeliveryConflict
		}
		candidate := after[index]
		candidate.State = CleanupPlanned
		candidate.BlockerCode = ""
		if !reflect.DeepEqual(before[index], candidate) {
			return ErrDeliveryConflict
		}
	}
	return nil
}

func graphOrderIsTopological(tasks []GraphTask) bool {
	positions := make(map[string]int, len(tasks))
	for index, task := range tasks {
		positions[task.TaskID] = index
	}
	for index, task := range tasks {
		for _, dependency := range task.Dependencies {
			if positions[dependency] >= index {
				return false
			}
		}
	}
	return true
}

func graphDependenciesReady(task GraphTask, byID map[string]*GraphTask, reachable map[string]bool) bool {
	for _, dependency := range task.Dependencies {
		predecessor := byID[dependency]
		if predecessor == nil || predecessor.State != GraphTaskIntegrated ||
			!canonicalGitSHA.MatchString(predecessor.IntegratedCommitSHA) || !reachable[predecessor.IntegratedCommitSHA] {
			return false
		}
	}
	return true
}

func graphHasIncompleteTasks(tasks []GraphTask) bool {
	for _, task := range tasks {
		if task.State != GraphTaskIntegrated && task.State != GraphTaskBlocked {
			return true
		}
	}
	return false
}

func activeGraphTaskCount(tasks []GraphTask) int {
	count := 0
	for _, task := range tasks {
		switch task.State {
		case GraphTaskPreparing, GraphTaskRunning, GraphTaskWaitingInput, GraphTaskCandidate:
			count++
		}
	}
	return count
}

func graphEntirelyWaitingForHuman(tasks []GraphTask) bool {
	waiting := false
	for _, task := range tasks {
		switch task.State {
		case GraphTaskWaitingInput:
			waiting = true
		case GraphTaskPending, GraphTaskCandidate, GraphTaskIntegrated, GraphTaskBlocked:
			continue
		case GraphTaskPreparing, GraphTaskRunning:
			return false
		default:
			return false
		}
	}
	return waiting
}

func graphTasksByID(tasks []GraphTask) map[string]*GraphTask {
	byID := make(map[string]*GraphTask, len(tasks))
	for index := range tasks {
		byID[tasks[index].TaskID] = &tasks[index]
	}
	return byID
}

func graphTaskByID(tasks []GraphTask, taskID string) *GraphTask {
	for index := range tasks {
		if tasks[index].TaskID == taskID {
			return &tasks[index]
		}
	}
	return nil
}

func (state GraphTaskState) valid() bool {
	switch state {
	case GraphTaskPending, GraphTaskPreparing, GraphTaskRunning, GraphTaskWaitingInput,
		GraphTaskCandidate, GraphTaskIntegrated, GraphTaskBlocked:
		return true
	default:
		return false
	}
}

func validTaskQuestion(question *TaskQuestion) bool {
	if question == nil || !boundedArgument(question.RequestID) ||
		!boundedGraphText(question.Prompt, maxQuestionBytes) || !SafeTaskQuestionText(question.Prompt) ||
		!canonicalSHA256.MatchString(question.ContextDigest) ||
		len(question.Choices) > 4 {
		return false
	}
	for _, choice := range question.Choices {
		if !boundedGraphText(choice, maxChoiceBytes) || !SafeTaskQuestionText(choice) {
			return false
		}
	}
	if question.Answer != nil && !validTaskAnswer(question.Answer) {
		return false
	}
	return true
}

var (
	unsafeGraphQuestionPath       = regexp.MustCompile(`(?i)(^|[[:space:]('"=])(?:/[^/[:space:]]+|~/|\.\./|[a-z]:\\|\.ssh(?:/|\\))`)
	unsafeGraphQuestionToken      = regexp.MustCompile(`(?i)(?:gh[pousr]_[a-z0-9_]{20,}|github_pat_[a-z0-9_]{20,}|xox[baprs]-[a-z0-9-]{10,}|sk-[a-z0-9_-]{16,}|AKIA[0-9A-Z]{16})`)
	unsafeGraphQuestionAssignment = regexp.MustCompile(`(?i)(?:aws_secret_access_key|secret_access_key|client_secret|api[_-]?key|access[_-]?token|password)[[:space:]]*[:=][[:space:]]*[^[:space:]]+`)
)

func SafeTaskQuestionText(value string) bool {
	if unsafeGraphQuestionPath.MatchString(value) || unsafeGraphQuestionToken.MatchString(value) ||
		unsafeGraphQuestionAssignment.MatchString(value) {
		return false
	}
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"authorization: bearer ", "api_key=", "api_key:", "api-key=", "api-key:",
		"access_token=", "access_token:", "password=", "password:", "-----begin ", "private key",
	} {
		if strings.Contains(lower, marker) {
			return false
		}
	}
	return true
}

func validTaskWorktreeIntent(intent *TaskWorktreeIntent) bool {
	return intent != nil && canonicalSHA256.MatchString(intent.OperationID) &&
		canonicalSHA256.MatchString(intent.RequestDigest) && boundedArgument(intent.Name) && boundedArgument(intent.Branch)
}

func validTaskAnswer(answer *TaskAnswer) bool {
	return answer != nil && canonicalSHA256.MatchString(answer.QuestionOperationID) &&
		boundedArgument(answer.LoopRunID) && answer.Generation >= 1 && boundedArgument(answer.NodeID) &&
		answer.ItemIndex >= 0 && boundedGraphText(answer.Value, maxAnswerBytes)
}

func validTaskCandidateEvidence(evidence *TaskCandidateEvidence, taskID, verificationDigest string) bool {
	if evidence == nil || !canonicalSlug.MatchString(evidence.Slug) ||
		!canonicalSHA256.MatchString(evidence.RepositoryIdentity) || !boundedArgument(evidence.Branch) ||
		!canonicalGitSHA.MatchString(evidence.TreeSHA) || len(evidence.Verification) == 0 ||
		len(evidence.Verification) > maxCandidateVerificationBytes || !json.Valid(evidence.Verification) ||
		sha256Digest(evidence.Verification) != verificationDigest || evidence.OwnedTrackingPaths == nil ||
		evidence.Tracking == nil || len(evidence.OwnedTrackingPaths) > 64 || len(evidence.Tracking) > 64 {
		return false
	}
	var decoded map[string]any
	if err := json.Unmarshal(evidence.Verification, &decoded); err != nil || decoded["task_id"] != taskID {
		return false
	}
	canonical, err := json.Marshal(decoded)
	if err != nil || !bytes.Equal(canonical, evidence.Verification) {
		return false
	}
	prefix := ".compozy/tasks/" + evidence.Slug + "/"
	seenPaths := make(map[string]struct{}, len(evidence.OwnedTrackingPaths))
	for _, path := range evidence.OwnedTrackingPaths {
		if !validTaskTrackingPath(path, prefix) {
			return false
		}
		if _, duplicate := seenPaths[path]; duplicate {
			return false
		}
		seenPaths[path] = struct{}{}
	}
	seenTracking := make(map[string]struct{}, len(evidence.Tracking))
	for _, file := range evidence.Tracking {
		if !validTaskTrackingPath(file.Path, prefix) || !canonicalSHA256.MatchString(file.Digest) {
			return false
		}
		if _, duplicate := seenTracking[file.Path]; duplicate {
			return false
		}
		seenTracking[file.Path] = struct{}{}
		if _, owned := seenPaths[file.Path]; !owned {
			return false
		}
	}
	return true
}

func validTaskTrackingPath(path, prefix string) bool {
	return len(path) > len(prefix) && len(path) <= 1024 && strings.HasPrefix(path, prefix) &&
		filepath.ToSlash(filepath.Clean(filepath.FromSlash(path))) == path && filepath.Base(path) != "_tasks.md"
}

func validTaskTerminalStatus(status string) bool {
	switch status {
	case "blocked", "failed", "exhausted", "stalled", "canceled":
		return true
	default:
		return false
	}
}

func nextRuntimeForTask(generation RoutingGeneration, task GraphTask, current RuntimeValue) (RuntimeValue, bool) {
	var generationTask GenerationTask
	found := false
	for _, candidate := range generation.Tasks {
		if candidate.ID == task.TaskID && candidate.Domain == task.Domain && candidate.Complexity == task.Complexity {
			generationTask = candidate
			found = true
			break
		}
	}
	if !found {
		return RuntimeValue{}, false
	}
	cell, found := generationCellForTask(generation, generationTask)
	if !found || cell.FallbackLimit < 0 || len(cell.Fallbacks) > cell.FallbackLimit ||
		generation.DeliveryFallbackLimit < 0 || len(cell.Fallbacks) > generation.DeliveryFallbackLimit {
		return RuntimeValue{}, false
	}
	position := candidatePosition(cell, current)
	if position < 0 || position >= len(cell.Fallbacks) {
		return RuntimeValue{}, false
	}
	next := cell.Fallbacks[position]
	return RuntimeValue{Provider: next.ProviderID, Model: next.ModelID, Reasoning: next.Reasoning}, true
}

func deliveryWaveForTask(waves []DeliveryWave, taskID, baseHeadSHA string) (DeliveryWave, bool) {
	for index := len(waves) - 1; index >= 0; index-- {
		wave := waves[index]
		if wave.BaseHeadSHA != baseHeadSHA || !slices.Contains(wave.TaskIDs, taskID) {
			continue
		}
		return cloneDeliveryWave(wave), true
	}
	return DeliveryWave{}, false
}

func validWaveSettlement(settlement WaveSettlement, waveCount int) bool {
	if !canonicalSHA256.MatchString(settlement.OperationID) || !canonicalSHA256.MatchString(settlement.RequestDigest) ||
		settlement.Wave < 1 || settlement.Wave > waveCount ||
		!canonicalGitSHA.MatchString(settlement.StartingHeadSHA) || !canonicalGitSHA.MatchString(settlement.FinalHeadSHA) ||
		len(settlement.OrderedTaskIDs) == 0 || len(settlement.OrderedTaskIDs) > MaxParallelTasks ||
		len(settlement.CandidateCommitSHAs) != len(settlement.OrderedTaskIDs) ||
		len(settlement.AcceptedTaskIDs) != len(settlement.AcceptedCommitSHAs) ||
		len(settlement.AcceptedTaskIDs) != len(settlement.IntegratedCommitSHAs) ||
		len(settlement.AcceptedTaskIDs) > len(settlement.OrderedTaskIDs) {
		return false
	}
	seen := make(map[string]struct{}, len(settlement.OrderedTaskIDs))
	for index, taskID := range settlement.OrderedTaskIDs {
		if !boundedArgument(taskID) || !canonicalGitSHA.MatchString(settlement.CandidateCommitSHAs[index]) {
			return false
		}
		if _, duplicate := seen[taskID]; duplicate {
			return false
		}
		seen[taskID] = struct{}{}
		if index < len(settlement.AcceptedTaskIDs) &&
			(settlement.AcceptedTaskIDs[index] != taskID ||
				settlement.AcceptedCommitSHAs[index] != settlement.CandidateCommitSHAs[index] ||
				!canonicalGitSHA.MatchString(settlement.IntegratedCommitSHAs[index])) {
			return false
		}
	}
	if settlement.FirstConflictTaskID == "" {
		return settlement.ConflictEvidenceDigest == "" && len(settlement.AcceptedTaskIDs) == len(settlement.OrderedTaskIDs)
	}
	accepted := len(settlement.AcceptedTaskIDs)
	return accepted < len(settlement.OrderedTaskIDs) &&
		settlement.FirstConflictTaskID == settlement.OrderedTaskIDs[accepted] &&
		canonicalSHA256.MatchString(settlement.ConflictEvidenceDigest)
}

func integrationOperationFromSettlement(settlement WaveSettlement) IntegrationOperation {
	return IntegrationOperation{
		OperationID: settlement.OperationID, RequestDigest: settlement.RequestDigest,
		Wave: settlement.Wave, StartingHeadSHA: settlement.StartingHeadSHA,
		OrderedTaskIDs:       append([]string(nil), settlement.OrderedTaskIDs...),
		CandidateCommitSHAs:  append([]string(nil), settlement.CandidateCommitSHAs...),
		AcceptedTaskIDs:      append([]string(nil), settlement.AcceptedTaskIDs...),
		AcceptedCommitSHAs:   append([]string(nil), settlement.AcceptedCommitSHAs...),
		IntegratedCommitSHAs: append([]string(nil), settlement.IntegratedCommitSHAs...),
		ConflictingTaskID:    settlement.FirstConflictTaskID, ConflictEvidenceDigest: settlement.ConflictEvidenceDigest,
		FinalHeadSHA: settlement.FinalHeadSHA,
	}
}

func settlementReplayResult(graph *DeliveryGraph, operation IntegrationOperation) (WaveSettlementResult, error) {
	if operation.ConflictingTaskID != "" {
		task := graphTaskByID(graph.Tasks, operation.ConflictingTaskID)
		if task == nil || len(task.Attempts) == 0 {
			return WaveSettlementResult{}, ErrInvalidDeliveryGraph
		}
		for index := range task.Attempts {
			attempt := task.Attempts[index]
			if attempt.Conflict == nil || attempt.Conflict.IntegrationOperationID != operation.OperationID {
				continue
			}
			if index+1 == len(task.Attempts) {
				if attempt.State != GraphTaskBlocked || attempt.BlockerCode == "" {
					return WaveSettlementResult{}, ErrInvalidDeliveryGraph
				}
				return WaveSettlementResult{Disposition: SettlementBlocked, TaskID: task.TaskID}, nil
			}
			next := task.Attempts[index+1]
			wave, found := deliveryWaveForTask(graph.Waves, task.TaskID, next.BaseHeadSHA)
			if !found {
				return WaveSettlementResult{}, ErrInvalidDeliveryGraph
			}
			return WaveSettlementResult{
				Disposition: SettlementReexecuteConflict, TaskID: task.TaskID, Wave: wave, Runtime: next.Runtime,
			}, nil
		}
		return WaveSettlementResult{}, ErrInvalidDeliveryGraph
	}
	if allGraphTasksIntegrated(graph.Tasks) {
		return WaveSettlementResult{Disposition: SettlementAllIntegrated}, nil
	}
	return WaveSettlementResult{Disposition: SettlementWaveIntegrated}, nil
}

func allGraphTasksIntegrated(tasks []GraphTask) bool {
	for _, task := range tasks {
		if task.State != GraphTaskIntegrated {
			return false
		}
	}
	return true
}

func boundedGraphText(value string, limit int) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && trimmed == value && len(value) <= limit && !strings.ContainsRune(value, '\x00')
}

func cloneDeliveryGraph(graph *DeliveryGraph) *DeliveryGraph {
	if graph == nil {
		return nil
	}
	cloned := &DeliveryGraph{
		Tasks:        make([]GraphTask, len(graph.Tasks)),
		Waves:        make([]DeliveryWave, len(graph.Waves)),
		Integrations: make([]IntegrationOperation, len(graph.Integrations)),
		Pauses:       make([]HumanPause, len(graph.Pauses)),
		Cleanups:     append([]CleanupOperation(nil), graph.Cleanups...),
	}
	for index, task := range graph.Tasks {
		cloned.Tasks[index] = task
		cloned.Tasks[index].Dependencies = append([]string{}, task.Dependencies...)
		cloned.Tasks[index].Attempts = make([]GraphTaskAttempt, len(task.Attempts))
		for attemptIndex, attempt := range task.Attempts {
			cloned.Tasks[index].Attempts[attemptIndex] = cloneGraphTaskAttempt(attempt)
		}
	}
	for index, wave := range graph.Waves {
		cloned.Waves[index] = cloneDeliveryWave(wave)
	}
	for index, operation := range graph.Integrations {
		cloned.Integrations[index] = cloneIntegrationOperation(operation)
	}
	for index, pause := range graph.Pauses {
		cloned.Pauses[index] = cloneHumanPause(pause)
	}
	return cloned
}

func cloneGraphTaskAttempt(attempt GraphTaskAttempt) GraphTaskAttempt {
	attempt.WorktreeIntent = cloneTaskWorktreeIntent(attempt.WorktreeIntent)
	if attempt.Question != nil {
		attempt.Question = cloneTaskQuestion(attempt.Question)
	}
	if attempt.Conflict != nil {
		conflict := *attempt.Conflict
		attempt.Conflict = &conflict
	}
	attempt.CandidateEvidence = cloneTaskCandidateEvidence(attempt.CandidateEvidence)
	if attempt.TokensUsed != nil {
		tokensUsed := *attempt.TokensUsed
		attempt.TokensUsed = &tokensUsed
	}
	return attempt
}

func cloneTaskWorktreeIntent(intent *TaskWorktreeIntent) *TaskWorktreeIntent {
	if intent == nil {
		return nil
	}
	cloned := *intent
	return &cloned
}

func cloneTaskCandidateEvidence(evidence *TaskCandidateEvidence) *TaskCandidateEvidence {
	if evidence == nil {
		return nil
	}
	cloned := *evidence
	cloned.Verification = append(json.RawMessage(nil), evidence.Verification...)
	cloned.OwnedTrackingPaths = make([]string, len(evidence.OwnedTrackingPaths))
	copy(cloned.OwnedTrackingPaths, evidence.OwnedTrackingPaths)
	cloned.Tracking = make([]TaskTrackingFile, len(evidence.Tracking))
	copy(cloned.Tracking, evidence.Tracking)
	return &cloned
}

func cloneTaskQuestion(question *TaskQuestion) *TaskQuestion {
	if question == nil {
		return nil
	}
	cloned := *question
	cloned.Choices = append([]string(nil), question.Choices...)
	cloned.Answer = cloneTaskAnswer(question.Answer)
	return &cloned
}

func cloneTaskAnswer(answer *TaskAnswer) *TaskAnswer {
	if answer == nil {
		return nil
	}
	cloned := *answer
	return &cloned
}

func cloneDeliveryWave(wave DeliveryWave) DeliveryWave {
	wave.TaskIDs = append([]string(nil), wave.TaskIDs...)
	return wave
}

func cloneIntegrationOperation(operation IntegrationOperation) IntegrationOperation {
	operation.OrderedTaskIDs = append([]string(nil), operation.OrderedTaskIDs...)
	operation.CandidateCommitSHAs = append([]string(nil), operation.CandidateCommitSHAs...)
	operation.AcceptedTaskIDs = append([]string(nil), operation.AcceptedTaskIDs...)
	operation.AcceptedCommitSHAs = append([]string(nil), operation.AcceptedCommitSHAs...)
	operation.IntegratedCommitSHAs = append([]string(nil), operation.IntegratedCommitSHAs...)
	return operation
}

func cloneHumanPause(pause HumanPause) HumanPause {
	pause.EndedAt = cloneTime(pause.EndedAt)
	return pause
}

func cloneTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
