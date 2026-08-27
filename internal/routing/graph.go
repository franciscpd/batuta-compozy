package routing

import (
	"errors"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"time"
)

const (
	MaxParallelTasks  = 4
	MaxDeliveryTasks  = 64
	MaxTaskExecutions = 4

	maxQuestionBytes = 2 << 10
	maxChoiceBytes   = 512
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
	Execution          int            `json:"execution"`
	Runtime            RuntimeValue   `json:"runtime"`
	State              GraphTaskState `json:"state"`
	BaseHeadSHA        string         `json:"base_head_sha"`
	WorktreeID         string         `json:"worktree_id,omitempty"`
	WorktreeRoot       string         `json:"worktree_root,omitempty"`
	ChildRunID         string         `json:"child_run_id,omitempty"`
	CandidateCommitSHA string         `json:"candidate_commit_sha,omitempty"`
	VerificationDigest string         `json:"verification_digest,omitempty"`
	Question           *TaskQuestion  `json:"question,omitempty"`
	Conflict           *ConflictProof `json:"conflict,omitempty"`
	BlockerCode        string         `json:"blocker_code,omitempty"`
}

type TaskQuestion struct {
	RequestID     string   `json:"request_id"`
	Prompt        string   `json:"prompt"`
	ContextDigest string   `json:"context_digest"`
	Choices       []string `json:"choices,omitempty"`
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

type IntegrationOperation struct {
	OperationID        string   `json:"operation_id"`
	RequestDigest      string   `json:"request_digest"`
	Wave               int      `json:"wave"`
	StartingHeadSHA    string   `json:"starting_head_sha"`
	OrderedTaskIDs     []string `json:"ordered_task_ids"`
	AcceptedTaskIDs    []string `json:"accepted_task_ids"`
	AcceptedCommitSHAs []string `json:"accepted_commit_shas"`
	ConflictingTaskID  string   `json:"conflicting_task_id,omitempty"`
	FinalHeadSHA       string   `json:"final_head_sha"`
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
		generation.TaskSetDigest != snapshot.Digest || len(generation.Tasks) != len(snapshot.Tasks) {
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
		return HumanPause{}, false, ErrInvalidActiveWall
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
	for index := range g.Pauses {
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
	seen := make(map[string]struct{}, len(graph.Pauses))
	total := time.Duration(0)
	var priorEnd *time.Time
	for index, pause := range graph.Pauses {
		if !boundedArgument(pause.TaskID) || pause.Execution < 1 || pause.Execution > MaxTaskExecutions ||
			!boundedArgument(pause.RequestID) || pause.StartedAt.IsZero() || pause.StartedAt.Location() != time.UTC ||
			pause.StartedAt.Before(createdAt) || pause.StartedAt.After(now) {
			return 0, ErrInvalidActiveWall
		}
		if _, duplicate := seen[pause.RequestID]; duplicate {
			return 0, ErrInvalidActiveWall
		}
		seen[pause.RequestID] = struct{}{}
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
		if count < len(task.Attempts) || count > len(task.Attempts)+1 ||
			(count == len(task.Attempts)+1 && task.State != GraphTaskPreparing && task.State != GraphTaskBlocked) ||
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
		if err := validateGraphTaskAttempt(attempt, index+1); err != nil {
			return err
		}
		if index > 0 {
			prior := task.Attempts[index-1]
			if prior.State != GraphTaskCandidate || prior.Conflict == nil || attempt.State != GraphTaskPreparing {
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
		if !canonicalGitSHA.MatchString(task.IntegratedCommitSHA) || task.IntegratedCommitSHA != last.CandidateCommitSHA ||
			task.BlockerCode != "" {
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

func validateGraphTaskAttempt(attempt GraphTaskAttempt, expectedExecution int) error {
	if attempt.Execution != expectedExecution || expectedExecution < 1 || expectedExecution > MaxTaskExecutions ||
		!boundedArgument(attempt.Runtime.Provider) || !boundedArgument(attempt.Runtime.Model) ||
		!boundedArgument(attempt.Runtime.Reasoning) || !canonicalGitSHA.MatchString(attempt.BaseHeadSHA) ||
		attempt.State == GraphTaskPending || !attempt.State.valid() {
		return ErrInvalidDeliveryGraph
	}
	if attempt.WorktreeRoot != "" && (!filepath.IsAbs(attempt.WorktreeRoot) || filepath.Clean(attempt.WorktreeRoot) != attempt.WorktreeRoot) {
		return ErrInvalidDeliveryGraph
	}
	if (attempt.WorktreeID == "") != (attempt.WorktreeRoot == "") ||
		(attempt.WorktreeID != "" && !boundedArgument(attempt.WorktreeID)) ||
		(attempt.ChildRunID != "" && !boundedArgument(attempt.ChildRunID)) {
		return ErrInvalidDeliveryGraph
	}
	if attempt.Question != nil && !validTaskQuestion(attempt.Question) {
		return ErrInvalidDeliveryGraph
	}
	if attempt.Conflict != nil && !validConflictProof(attempt.Conflict, attempt.CandidateCommitSHA) {
		return ErrInvalidDeliveryGraph
	}
	switch attempt.State {
	case GraphTaskPreparing:
		if attempt.WorktreeID != "" || attempt.ChildRunID != "" || attempt.CandidateCommitSHA != "" ||
			attempt.VerificationDigest != "" || attempt.Question != nil || attempt.Conflict != nil || attempt.BlockerCode != "" {
			return ErrInvalidDeliveryGraph
		}
	case GraphTaskRunning:
		if attempt.WorktreeID == "" || !boundedArgument(attempt.ChildRunID) || attempt.CandidateCommitSHA != "" ||
			attempt.VerificationDigest != "" || attempt.Conflict != nil || attempt.BlockerCode != "" {
			return ErrInvalidDeliveryGraph
		}
	case GraphTaskWaitingInput:
		if attempt.WorktreeID == "" || !boundedArgument(attempt.ChildRunID) || !validTaskQuestion(attempt.Question) ||
			attempt.CandidateCommitSHA != "" || attempt.VerificationDigest != "" || attempt.Conflict != nil ||
			attempt.BlockerCode != "" {
			return ErrInvalidDeliveryGraph
		}
	case GraphTaskCandidate, GraphTaskIntegrated:
		if attempt.WorktreeID == "" || !boundedArgument(attempt.ChildRunID) ||
			!canonicalGitSHA.MatchString(attempt.CandidateCommitSHA) ||
			!canonicalSHA256.MatchString(attempt.VerificationDigest) || attempt.BlockerCode != "" {
			return ErrInvalidDeliveryGraph
		}
	case GraphTaskBlocked:
		if !boundedArgument(attempt.BlockerCode) || attempt.CandidateCommitSHA != "" || attempt.VerificationDigest != "" {
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

func validateIntegrationOperations(operations []IntegrationOperation, waves []DeliveryWave) error {
	for _, operation := range operations {
		if !canonicalSHA256.MatchString(operation.OperationID) || !canonicalSHA256.MatchString(operation.RequestDigest) ||
			operation.Wave < 1 || operation.Wave > len(waves) ||
			!canonicalGitSHA.MatchString(operation.StartingHeadSHA) || !canonicalGitSHA.MatchString(operation.FinalHeadSHA) ||
			len(operation.OrderedTaskIDs) == 0 || len(operation.OrderedTaskIDs) > MaxParallelTasks ||
			len(operation.AcceptedTaskIDs) != len(operation.AcceptedCommitSHAs) ||
			len(operation.AcceptedTaskIDs) > len(operation.OrderedTaskIDs) {
			return ErrInvalidDeliveryGraph
		}
		for index, taskID := range operation.OrderedTaskIDs {
			if !boundedArgument(taskID) {
				return ErrInvalidDeliveryGraph
			}
			if index < len(operation.AcceptedTaskIDs) {
				if operation.AcceptedTaskIDs[index] != taskID || !canonicalGitSHA.MatchString(operation.AcceptedCommitSHAs[index]) {
					return ErrInvalidDeliveryGraph
				}
			}
		}
		if operation.ConflictingTaskID != "" {
			accepted := len(operation.AcceptedTaskIDs)
			if accepted >= len(operation.OrderedTaskIDs) || operation.ConflictingTaskID != operation.OrderedTaskIDs[accepted] {
				return ErrInvalidDeliveryGraph
			}
		}
	}
	return nil
}

func validatePauseArchive(graph *DeliveryGraph, createdAt time.Time) error {
	seen := make(map[string]struct{}, len(graph.Pauses))
	var priorEnd *time.Time
	for index, pause := range graph.Pauses {
		if !boundedArgument(pause.TaskID) || pause.Execution < 1 || pause.Execution > MaxTaskExecutions ||
			!boundedArgument(pause.RequestID) || pause.StartedAt.IsZero() || pause.StartedAt.Location() != time.UTC ||
			pause.StartedAt.Before(createdAt) {
			return ErrInvalidActiveWall
		}
		if _, duplicate := seen[pause.RequestID]; duplicate {
			return ErrInvalidActiveWall
		}
		seen[pause.RequestID] = struct{}{}
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

func validateDeliveryGraphTransition(before, after *DeliveryGraph) error {
	if before == nil || after == nil || len(before.Tasks) != len(after.Tasks) ||
		len(after.Waves) < len(before.Waves) || len(after.Waves) > len(before.Waves)+1 ||
		len(after.Integrations) < len(before.Integrations) || len(after.Integrations) > len(before.Integrations)+1 ||
		len(after.Pauses) < len(before.Pauses) || len(after.Pauses) > len(before.Pauses)+1 {
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
	if !graphTaskTransitionAllowed(before.State, after.State) || len(after.Attempts) < len(before.Attempts) ||
		len(after.Attempts) > len(before.Attempts)+1 {
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
	if len(after.Attempts) == 0 || after.Attempts[len(after.Attempts)-1].Execution != len(after.Attempts) ||
		after.Attempts[len(after.Attempts)-1].State != GraphTaskPreparing {
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
		} else if !reflect.DeepEqual(before.Attempts[index], after.Attempts[index]) {
			return ErrDeliveryConflict
		}
	}
	return nil
}

func validateGraphAttemptTransition(before, after GraphTaskAttempt) error {
	if before.Execution != after.Execution || before.Runtime != after.Runtime || before.BaseHeadSHA != after.BaseHeadSHA ||
		!graphTaskTransitionAllowed(before.State, after.State) {
		return ErrInvalidDeliveryTransition
	}
	if before.WorktreeID != "" && (before.WorktreeID != after.WorktreeID || before.WorktreeRoot != after.WorktreeRoot) {
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
	if before.BlockerCode != "" && before.BlockerCode != after.BlockerCode {
		return ErrDeliveryConflict
	}
	return nil
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
		!boundedGraphText(question.Prompt, maxQuestionBytes) || !canonicalSHA256.MatchString(question.ContextDigest) ||
		len(question.Choices) > 4 {
		return false
	}
	for _, choice := range question.Choices {
		if !boundedGraphText(choice, maxChoiceBytes) {
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
		cloned.Integrations[index] = operation
		cloned.Integrations[index].OrderedTaskIDs = append([]string(nil), operation.OrderedTaskIDs...)
		cloned.Integrations[index].AcceptedTaskIDs = append([]string(nil), operation.AcceptedTaskIDs...)
		cloned.Integrations[index].AcceptedCommitSHAs = append([]string(nil), operation.AcceptedCommitSHAs...)
	}
	for index, pause := range graph.Pauses {
		cloned.Pauses[index] = cloneHumanPause(pause)
	}
	return cloned
}

func cloneGraphTaskAttempt(attempt GraphTaskAttempt) GraphTaskAttempt {
	if attempt.Question != nil {
		question := *attempt.Question
		question.Choices = append([]string(nil), attempt.Question.Choices...)
		attempt.Question = &question
	}
	if attempt.Conflict != nil {
		conflict := *attempt.Conflict
		attempt.Conflict = &conflict
	}
	return attempt
}

func cloneDeliveryWave(wave DeliveryWave) DeliveryWave {
	wave.TaskIDs = append([]string(nil), wave.TaskIDs...)
	return wave
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
