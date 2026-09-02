//go:build integration

package extensionapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/integration"
	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/franciscpd/batuta-compozy/internal/publication"
	"github.com/franciscpd/batuta-compozy/internal/routing"
	"github.com/franciscpd/batuta-compozy/internal/worktreeops"
)

func TestParallelDeliveryFixtureHasRunnableSharedConflictProject(t *testing.T) {
	t.Parallel()

	fixtureRoot, err := filepath.Abs(filepath.Join("..", "..", "tests", "fixtures", "parallel-delivery"))
	if err != nil {
		t.Fatalf("fixture root: %v", err)
	}
	shared, err := os.ReadFile(filepath.Join(fixtureRoot, "project", "shared.txt"))
	if err != nil {
		t.Fatalf("read shared conflict fixture: %v", err)
	}
	if string(shared) != "shared=base\n" {
		t.Fatalf("shared conflict fixture = %q, want base line", shared)
	}
}

func TestParallelDeliveryFixtureLoadsFiveTaskDependencyGraph(t *testing.T) {
	t.Parallel()

	fixtureRoot, err := filepath.Abs(filepath.Join("..", "..", "tests", "fixtures", "parallel-delivery"))
	if err != nil {
		t.Fatalf("fixture root: %v", err)
	}
	loader, err := routing.NewArtifactLoader(fixtureRoot)
	if err != nil {
		t.Fatalf("NewArtifactLoader() error = %v", err)
	}
	set, err := loader.Load("parallel-demo")
	if err != nil {
		t.Fatalf("Load(parallel-demo) error = %v", err)
	}
	if len(set.Tasks) != 5 {
		t.Fatalf("fixture task count = %d, want 5", len(set.Tasks))
	}
	want := []struct {
		id           string
		domain       routing.Domain
		dependencies []string
	}{
		{"task_01", routing.DomainBackend, nil},
		{"task_02", routing.DomainFrontend, nil},
		{"task_03", routing.DomainTesting, nil},
		{"task_04", routing.DomainDocs, nil},
		{"task_05", routing.DomainFullstack, []string{"task_01", "task_02", "task_03", "task_04"}},
	}
	for index, expected := range want {
		got := set.Tasks[index]
		if got.ID != expected.id || got.Domain != expected.domain || !reflect.DeepEqual(got.Dependencies, expected.dependencies) {
			t.Fatalf("task[%d] = %#v, want id=%s domain=%s dependencies=%#v", index, got, expected.id, expected.domain, expected.dependencies)
		}
	}
}

func TestParallelDeliveryHarnessCreatesFourRealInitialWorktrees(t *testing.T) {
	ctx := context.Background()
	harness := newParallelDeliveryHarness(t)

	prepared, err := harness.graph.Execute(ctx, harness.scope, DeliveryGraphInput{
		Operation: GraphOpPrepareWave, DeliveryID: harness.deliveryID,
	})
	if err != nil {
		t.Fatalf("prepare_wave: %v", err)
	}
	if prepared.Disposition != GraphDispositionWaveReady || len(prepared.Tasks) != 4 {
		t.Fatalf("prepare_wave = %#v, want four ready tasks", prepared)
	}
	for _, task := range prepared.Tasks {
		if _, err := os.Stat(task.WorktreeRoot); err != nil {
			t.Fatalf("task %s worktree %q: %v", task.TaskID, task.WorktreeRoot, err)
		}
	}
	if harness.worktrees.createCalls != 4 {
		t.Fatalf("worktree creates = %d, want 4", harness.worktrees.createCalls)
	}
	harness.startPreparedChildren(t, prepared)
	if len(harness.childStarts) != 4 {
		t.Fatalf("child starts = %d, want four", len(harness.childStarts))
	}
	replayed, err := harness.graph.Execute(ctx, harness.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: harness.deliveryID})
	if err != nil || len(replayed.Tasks) != 4 || harness.worktrees.createCalls != 4 || len(harness.childStarts) != 4 {
		t.Fatalf("fifth concurrent task admitted: replay=%#v error=%v creates=%d children=%d", replayed, err, harness.worktrees.createCalls, len(harness.childStarts))
	}
}

func TestParallelDeliveryWidthHarnessDeniesFifthIndependentTaskAndReplaysChildStarts(t *testing.T) {
	ctx := context.Background()
	h := newParallelDeliveryWidthHarness(t)
	prepared, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: h.deliveryID})
	if err != nil || len(prepared.Tasks) != routing.MaxParallelTasks {
		t.Fatalf("prepare independent width wave = %#v error=%v", prepared, err)
	}
	h.startPreparedChildren(t, prepared)
	if len(h.childStarts) != routing.MaxParallelTasks || h.worktrees.createCalls != routing.MaxParallelTasks {
		t.Fatalf("initial independent starts=%#v worktrees=%d", h.childStarts, h.worktrees.createCalls)
	}
	before := h.snapshot(t)
	replayed, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: h.deliveryID})
	if err != nil || !reflect.DeepEqual(replayed, prepared) {
		t.Fatalf("prepare width replay = %#v error=%v", replayed, err)
	}
	h.startPreparedChildren(t, replayed)
	after := h.snapshot(t)
	if before != after || len(h.childStarts) != routing.MaxParallelTasks || h.worktrees.createCalls != routing.MaxParallelTasks {
		t.Fatalf("fifth independent task was admitted or child start replay duplicated\nbefore=%s\nafter=%s", before, after)
	}
	journal, exists, loadErr := h.store.Load(h.scope.WorkspaceID)
	if loadErr != nil || !exists {
		t.Fatalf("load fifth independent task: exists=%v error=%v", exists, loadErr)
	}
	fifth, found := journal.Deliveries[h.deliveryID].Graph.Task("task_05")
	if !found {
		t.Fatal("fifth independent task is absent from graph")
	}
	if fifth.State != routing.GraphTaskPending || len(fifth.Attempts) != 0 {
		t.Fatalf("fifth independent task = %#v, want pending without worktree or child", fifth)
	}
	h.validateParallelWidthPythonEvidence(t, fifth, before, after)
}

func TestParallelDeliveryHarnessContinuesTypedQuestionOnSameChildAndWorktree(t *testing.T) {
	ctx := context.Background()
	harness := newParallelDeliveryHarness(t)
	prepared, err := harness.graph.Execute(ctx, harness.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: harness.deliveryID})
	if err != nil {
		t.Fatalf("prepare_wave: %v", err)
	}
	harness.startPreparedChildren(t, prepared)
	frontend := preparedTask(t, prepared, "task_02")
	delivery, wave, task := harness.graphTask(t, "task_02")
	run := deliveryRun{
		ID: "run_parallel_frontend", WorkspaceID: harness.scope.WorkspaceID, LoopName: "batuta-task", Status: "running",
		CreatedAt: time.Now().UTC(), StartedAt: time.Now().UTC(), Inputs: graphTaskRunInputs(delivery, wave, task, frontend.Execution),
	}
	questionInput := DeliveryGraphInput{
		Operation: GraphOpRecordQuestion, DeliveryID: harness.deliveryID, Wave: frontend.Wave,
		TaskID: "task_02", Execution: frontend.Execution, Prompt: "Which shared frontend value should be preserved?",
		Choices: []string{"shared=frontend", "shared=backend"},
	}
	harness.runs.recent = []deliveryRun{run}
	question, err := harness.graph.Execute(ctx, harness.scope, questionInput)
	if err != nil {
		t.Fatalf("record_question: %v", err)
	}
	if question.Disposition != GraphDispositionWaitingInput {
		t.Fatalf("record_question = %#v", question)
	}
	backend := preparedTask(t, prepared, "task_01")
	harness.repository.writeAndCommit(t, backend.WorktreeRoot, "project/backend-progress.txt", "backend=progress\\n", "feat(backend): progress while frontend waits")
	if head := strings.TrimSpace(harness.repository.run(t, backend.WorktreeRoot, "rev-parse", "HEAD")); head == backend.BaseSHA {
		t.Fatalf("sibling backend did not make progress while frontend waited: head=%s", head)
	}
	answeredAt := time.Now().UTC()
	harness.runs.statuses[run.ID] = deliveryRunDetail{
		Run: run,
		Requests: []deliveryRequest{{
			LoopRunID: run.ID, LoopName: "batuta-task", Generation: 2, NodeID: "ask_operator", ItemIndex: 0,
			Kind: "ask", State: "answered", Prompt: questionInput.Prompt, Context: json.RawMessage(`{"task_id":"task_02"}`),
			Expect: taskAnswerExpectation(), Decisions: []string{"respond"}, Agents: "deny", AnsweredDecision: "respond",
			ActorKind: "human", ActorID: "operator-parallel", AnsweredAt: timePointer(answeredAt), ResolvedAt: timePointer(answeredAt),
		}},
		Generations: []deliveryGeneration{{Generation: 2, Outputs: []deliveryOutput{{
			NodeID: "ask_operator", ItemIndex: 0, Status: "succeeded", OutputRef: answeredAskOutputRef("shared=frontend"),
		}}}},
	}
	answered, err := harness.graph.Execute(ctx, harness.scope, DeliveryGraphInput{
		Operation: GraphOpRecordAnswer, DeliveryID: harness.deliveryID, Wave: frontend.Wave,
		TaskID: "task_02", Execution: frontend.Execution, QuestionOperationID: question.QuestionOperationID, Answer: "shared=frontend",
	})
	if err != nil || answered.Disposition != GraphDispositionTaskReady || answered.Execution != frontend.Execution+1 {
		t.Fatalf("record_answer = %#v, error=%v", answered, err)
	}
	continued, err := harness.graph.Execute(ctx, harness.scope, DeliveryGraphInput{
		Operation: GraphOpTaskContext, DeliveryID: harness.deliveryID, Wave: frontend.Wave, TaskID: "task_02", Execution: frontend.Execution,
	})
	if err != nil || continued.Execution != answered.Execution || continued.WorktreeID != frontend.WorktreeID || continued.WorktreeRoot != frontend.WorktreeRoot || len(continued.Answers) != 1 {
		t.Fatalf("continued task_context = %#v, error=%v", continued, err)
	}
}

func TestParallelDeliveryServicePausesActiveWallAfterSiblingsBecomeQuiescent(t *testing.T) {
	ctx := context.Background()
	h := newParallelDeliveryHarness(t)
	clock := time.Now().UTC()
	h.graph.Now = func() time.Time { return clock }
	prepared, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{
		Operation: GraphOpPrepareWave, DeliveryID: h.deliveryID,
	})
	if err != nil || len(prepared.Tasks) != 4 {
		t.Fatalf("prepare_wave = %#v, error=%v", prepared, err)
	}
	h.startPreparedChildren(t, prepared)

	frontend := preparedTask(t, prepared, "task_02")
	delivery, wave, task := h.graphTask(t, "task_02")
	run := deliveryRun{
		ID: "run_parallel_pause", WorkspaceID: h.scope.WorkspaceID, LoopName: "batuta-task", Status: "running",
		CreatedAt: clock, StartedAt: clock, Inputs: graphTaskRunInputs(delivery, wave, task, 1),
	}
	h.runs.recent = []deliveryRun{run}
	questionInput := DeliveryGraphInput{
		Operation: GraphOpRecordQuestion, DeliveryID: h.deliveryID, Wave: frontend.Wave,
		TaskID: "task_02", Execution: 1, Prompt: "Which frontend contract should remain stable?",
		Choices: []string{"Preserve", "Replace"},
	}
	question, err := h.graph.Execute(ctx, h.scope, questionInput)
	if err != nil || question.Disposition != GraphDispositionWaitingInput {
		t.Fatalf("record_question = %#v, error=%v", question, err)
	}
	journal, _, _ := h.store.Load(h.scope.WorkspaceID)
	if pauses := journal.Deliveries[h.deliveryID].Graph.Pauses; len(pauses) != 0 {
		t.Fatalf("pause opened while siblings were running: %#v", pauses)
	}

	for _, candidate := range []struct {
		taskID string
		path   string
	}{
		{taskID: "task_01", path: "project/pause-backend.txt"},
		{taskID: "task_03", path: "project/pause-tests.txt"},
		{taskID: "task_04", path: "project/pause-docs.txt"},
	} {
		descriptor := preparedTask(t, prepared, candidate.taskID)
		h.repository.writeAndCommit(t, descriptor.WorktreeRoot, candidate.path, candidate.taskID+"=candidate\n", "feat("+candidate.taskID+"): prepare pause fixture")
		if err := h.recordRealCandidate(ctx, candidate.taskID, 1, "run_pause_"+candidate.taskID); err != nil {
			t.Fatalf("record %s candidate: %v", candidate.taskID, err)
		}
	}
	journal, _, _ = h.store.Load(h.scope.WorkspaceID)
	record := journal.Deliveries[h.deliveryID]
	if len(record.Graph.Pauses) != 1 || record.Graph.Pauses[0].EndedAt != nil ||
		record.Graph.Pauses[0].RequestID != question.QuestionOperationID {
		t.Fatalf("quiescent sibling pause = %#v", record.Graph.Pauses)
	}
	remainingAtOpen, err := record.RemainingActiveWall(clock)
	if err != nil {
		t.Fatalf("remaining active wall at pause: %v", err)
	}

	clock = clock.Add(7 * time.Minute)
	h.runs.statuses[run.ID] = deliveryRunDetail{
		Run: run,
		Requests: []deliveryRequest{{
			LoopRunID: run.ID, LoopName: "batuta-task", Generation: 2, NodeID: "ask_operator", ItemIndex: 0,
			Kind: "ask", State: "answered", Prompt: questionInput.Prompt, Context: json.RawMessage(`{"task_id":"task_02"}`),
			Expect: taskAnswerExpectation(), Decisions: []string{"respond"}, Agents: "deny", AnsweredDecision: "respond",
			ActorKind: "human", ActorID: "operator-pause", AnsweredAt: timePointer(clock), ResolvedAt: timePointer(clock),
		}},
		Generations: []deliveryGeneration{{Generation: 2, Outputs: []deliveryOutput{{
			NodeID: "ask_operator", ItemIndex: 0, Status: "succeeded", OutputRef: answeredAskOutputRef("Preserve"),
		}}}},
	}
	answerInput := DeliveryGraphInput{
		Operation: GraphOpRecordAnswer, DeliveryID: h.deliveryID, Wave: frontend.Wave,
		TaskID: "task_02", Execution: 1, QuestionOperationID: question.QuestionOperationID, Answer: "Preserve",
	}
	answer, err := h.graph.Execute(ctx, h.scope, answerInput)
	if err != nil || answer.Disposition != GraphDispositionTaskReady || answer.Execution != 2 {
		t.Fatalf("record_answer = %#v, error=%v", answer, err)
	}
	journal, _, _ = h.store.Load(h.scope.WorkspaceID)
	record = journal.Deliveries[h.deliveryID]
	if len(record.Graph.Pauses) != 1 || record.Graph.Pauses[0].EndedAt == nil ||
		record.Graph.Pauses[0].EndedAt.Sub(record.Graph.Pauses[0].StartedAt) != 7*time.Minute {
		t.Fatalf("closed pause = %#v", record.Graph.Pauses)
	}
	remainingAfterAnswer, err := record.RemainingActiveWall(clock)
	if err != nil || remainingAfterAnswer != remainingAtOpen {
		t.Fatalf("remaining active wall after answer = %v, %v; want %v", remainingAfterAnswer, err, remainingAtOpen)
	}
	beforeReplay, _, _ := h.store.Load(h.scope.WorkspaceID)
	replay, err := h.graph.Execute(ctx, h.scope, answerInput)
	afterReplay, _, _ := h.store.Load(h.scope.WorkspaceID)
	if err != nil || !replay.Replayed || !reflect.DeepEqual(afterReplay, beforeReplay) {
		t.Fatalf("record_answer replay = %#v, error=%v", replay, err)
	}

	delivery, wave, task = h.graphTask(t, "task_02")
	run.Inputs = graphTaskRunInputs(delivery, wave, task, 1)
	h.runs.recent = []deliveryRun{run}
	second, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{
		Operation: GraphOpRecordQuestion, DeliveryID: h.deliveryID, Wave: frontend.Wave,
		TaskID: "task_02", Execution: 1, Prompt: "Which rollout should remain stable?", Choices: []string{"Gradual"},
	})
	if err != nil || second.Disposition != GraphDispositionWaitingInput {
		t.Fatalf("second record_question = %#v, error=%v", second, err)
	}
	journal, _, _ = h.store.Load(h.scope.WorkspaceID)
	pauses := journal.Deliveries[h.deliveryID].Graph.Pauses
	if len(pauses) != 2 || pauses[0].EndedAt == nil || pauses[1].EndedAt != nil || pauses[1].RequestID != second.QuestionOperationID {
		t.Fatalf("reopened pause archive = %#v", pauses)
	}
}

func TestParallelDeliveryHarnessIntegratesRealGitPrefixAndCreatesCumulativeConflictRetry(t *testing.T) {
	ctx := context.Background()
	harness := newParallelDeliveryHarness(t)
	prepared, err := harness.graph.Execute(ctx, harness.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: harness.deliveryID})
	if err != nil {
		t.Fatalf("prepare_wave: %v", err)
	}
	harness.startPreparedChildren(t, prepared)
	continuedFrontend := harness.answerFrontend(t, ctx, prepared)
	for _, taskID := range []string{"task_02"} {
		task := preparedTask(t, prepared, taskID)
		if taskID == "task_02" {
			task = continuedFrontend
		}
		value := "shared=backend\n"
		path := "project/shared.txt"
		message := "feat(backend): set shared fixture value"
		if taskID == "task_02" {
			value, message = "shared=frontend\n", "fix(frontend): set shared fixture value"
		}
		harness.repository.writeAndCommit(t, task.WorktreeRoot, path, value, message)
	}
	if err := harness.recordRealCandidate(ctx, "task_01", 1, "run_parallel_backend"); err != nil {
		t.Fatalf("record backend candidate: %v", err)
	}
	if err := harness.recordRealCandidate(ctx, "task_02", 1, "run_parallel_frontend"); err != nil {
		t.Fatalf("record frontend candidate: %v", err)
	}
	settled, err := harness.graph.Execute(ctx, harness.scope, DeliveryGraphInput{Operation: GraphOpSettleWave, DeliveryID: harness.deliveryID, Wave: 1})
	if err != nil || settled.Disposition != GraphDispositionReexecuteConflict || settled.TaskID != "task_02" || settled.Execution != 3 {
		t.Fatalf("settle_wave = %#v, error=%v", settled, err)
	}
	settlementSnapshot := harness.snapshot(t)
	settlementReplay, replayErr := harness.graph.Execute(ctx, harness.scope, DeliveryGraphInput{Operation: GraphOpSettleWave, DeliveryID: harness.deliveryID, Wave: 1})
	if replayErr != nil || settlementReplay.Disposition != GraphDispositionReexecuteConflict || harness.snapshot(t) != settlementSnapshot {
		t.Fatalf("conflict settlement replay = %#v error=%v", settlementReplay, replayErr)
	}
	retry, err := harness.graph.Execute(ctx, harness.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: harness.deliveryID})
	if err != nil {
		t.Fatalf("prepare retry wave: %v", err)
	}
	frontend := preparedTask(t, retry, "task_02")
	if frontend.Execution != 3 || frontend.BaseSHA != settled.BaseSHA || frontend.WorktreeID == preparedTask(t, prepared, "task_02").WorktreeID {
		t.Fatalf("retry frontend = %#v, settle=%#v", frontend, settled)
	}
	retrySnapshot := harness.snapshot(t)
	retryReplay, replayErr := harness.graph.Execute(ctx, harness.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: harness.deliveryID})
	if replayErr != nil || !reflect.DeepEqual(retryReplay, retry) || harness.snapshot(t) != retrySnapshot {
		t.Fatalf("conflict retry allocation replay = %#v error=%v", retryReplay, replayErr)
	}
	harness.completeRemainingParallelDelivery(t, ctx, prepared, retry)
}

func TestParallelDeliveryHarnessRejectsFifthPhysicalAttemptAfterThreeTypedResumes(t *testing.T) {
	ctx := context.Background()
	h := newParallelDeliveryHarness(t)
	prepared, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: h.deliveryID})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	h.answerFrontend(t, ctx, prepared)
	h.resumeFrontendAnswer(t, ctx, 2)
	h.resumeFrontendAnswer(t, ctx, 3)
	_, _, task := h.graphTask(t, "task_02")
	if len(task.Attempts) != routing.MaxTaskExecutions || task.Attempts[len(task.Attempts)-1].Execution != routing.MaxTaskExecutions {
		t.Fatalf("physical attempts = %#v", task.Attempts)
	}
	before := h.snapshot(t)
	_, err = h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpRecordQuestion, DeliveryID: h.deliveryID, Wave: 1, TaskID: "task_02", Execution: 1, Prompt: "Fourth answer continuation?"})
	if !errors.Is(err, routing.ErrInvalidDeliveryTransition) {
		t.Fatalf("fifth physical attempt error = %v, want invalid delivery transition", err)
	}
	after := h.snapshot(t)
	if before != after {
		index := firstSnapshotDifference(before, after)
		t.Fatalf("fifth attempt changed complete boundary snapshot at byte %d\nbefore=%s\nafter=%s", index, before[max(0, index-120):min(len(before), index+120)], after[max(0, index-120):min(len(after), index+120)])
	}
}

func TestParallelDeliveryHarnessReplaysAnsweredQuestionAtFourthPhysicalAttemptBeforeLiveness(t *testing.T) {
	ctx := context.Background()
	h := newParallelDeliveryHarness(t)
	prepared, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: h.deliveryID})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	h.answerFrontend(t, ctx, prepared)
	h.resumeFrontendAnswer(t, ctx, 2)
	h.resumeFrontendAnswer(t, ctx, 3)
	before := h.snapshot(t)
	replay, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{
		Operation: GraphOpRecordQuestion, DeliveryID: h.deliveryID, Wave: 1, TaskID: "task_02", Execution: 1,
		Prompt: "Which shared frontend value should be preserved?", Choices: []string{"shared=frontend", "shared=backend"},
	})
	if err != nil || !replay.Replayed || replay.Disposition != GraphDispositionTaskReady || replay.Execution != routing.MaxTaskExecutions {
		t.Fatalf("answered historical fourth-attempt replay = %#v error=%v", replay, err)
	}
	if after := h.snapshot(t); after != before {
		t.Fatalf("answered historical fourth-attempt replay changed full boundary snapshot\nbefore=%s\nafter=%s", before, after)
	}
}

func TestParallelDeliveryHarnessReplaysAnsweredQuestionAtFourthPhysicalCandidateBeforeLiveness(t *testing.T) {
	ctx := context.Background()
	h := newParallelDeliveryHarness(t)
	prepared, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: h.deliveryID})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	h.answerFrontend(t, ctx, prepared)
	h.resumeFrontendAnswer(t, ctx, 2)
	h.resumeFrontendAnswer(t, ctx, 3)
	_, _, current := h.graphTask(t, "task_02")
	h.repository.writeAndCommit(t, current.Attempts[len(current.Attempts)-1].WorktreeRoot, "project/fourth-candidate.txt", "candidate=fourth\n", "feat(frontend): preserve fourth continuation")
	if err := h.recordRealCandidate(ctx, "task_02", 1, "run_parallel_frontend"); err != nil {
		t.Fatalf("record fourth physical candidate: %v", err)
	}
	before := h.snapshot(t)
	replay, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{
		Operation: GraphOpRecordQuestion, DeliveryID: h.deliveryID, Wave: 1, TaskID: "task_02", Execution: 1,
		Prompt: "Which shared frontend value should be preserved?", Choices: []string{"shared=frontend", "shared=backend"},
	})
	if err != nil || !replay.Replayed || replay.Disposition != GraphDispositionCandidateRecorded || replay.Execution != routing.MaxTaskExecutions {
		t.Fatalf("answered historical fourth-attempt candidate replay = %#v error=%v", replay, err)
	}
	if after := h.snapshot(t); after != before {
		t.Fatalf("answered fourth-attempt candidate replay changed full boundary snapshot\nbefore=%s\nafter=%s", before, after)
	}
}

func TestParallelDeliveryHarnessReplaysAnsweredQuestionAtFourthPhysicalIntegrationBeforeLiveness(t *testing.T) {
	ctx := context.Background()
	h := newParallelDeliveryHarness(t)
	prepared, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: h.deliveryID})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	h.answerFrontend(t, ctx, prepared)
	h.resumeFrontendAnswer(t, ctx, 2)
	h.resumeFrontendAnswer(t, ctx, 3)
	_, _, current := h.graphTask(t, "task_02")
	h.repository.writeAndCommit(t, current.Attempts[len(current.Attempts)-1].WorktreeRoot, "project/fourth-integrated.txt", "integrated=fourth\n", "feat(frontend): integrate fourth continuation")
	if err := h.recordRealCandidate(ctx, "task_02", 1, "run_parallel_frontend"); err != nil {
		t.Fatalf("record fourth physical candidate: %v", err)
	}
	journal, exists, err := h.store.Load(h.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("load fourth integrated state: exists=%v error=%v", exists, err)
	}
	delivery := journal.Deliveries[h.deliveryID]
	for index := range delivery.Graph.Tasks {
		task := &delivery.Graph.Tasks[index]
		if task.TaskID != "task_02" {
			continue
		}
		attempt := &task.Attempts[len(task.Attempts)-1]
		attempt.State, task.State = routing.GraphTaskIntegrated, routing.GraphTaskIntegrated
		task.IntegratedCommitSHA = attempt.CandidateCommitSHA
		break
	}
	journal.Deliveries[h.deliveryID] = delivery
	if err := h.store.Save(h.scope.WorkspaceID, journal); err != nil {
		t.Fatalf("persist fourth physical integration fixture: %v", err)
	}
	before := h.snapshot(t)
	replay, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{
		Operation: GraphOpRecordQuestion, DeliveryID: h.deliveryID, Wave: 1, TaskID: "task_02", Execution: 1,
		Prompt: "Which shared frontend value should be preserved?", Choices: []string{"shared=frontend", "shared=backend"},
	})
	if err != nil || !replay.Replayed || replay.Disposition != GraphDispositionWaveIntegrated || replay.Execution != routing.MaxTaskExecutions {
		t.Fatalf("answered historical fourth-attempt integration replay = %#v error=%v", replay, err)
	}
	if after := h.snapshot(t); after != before {
		t.Fatalf("answered fourth-attempt integration replay changed full boundary snapshot\nbefore=%s\nafter=%s", before, after)
	}
}

func TestParallelDeliveryHarnessReplaysPersistedOpenQuestionAtFourthPhysicalAttemptBeforeLiveness(t *testing.T) {
	ctx := context.Background()
	h := newParallelDeliveryHarness(t)
	prepared, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: h.deliveryID})
	if err != nil {
		t.Fatalf("prepare: %v", err)
	}
	h.answerFrontend(t, ctx, prepared)
	h.resumeFrontendAnswer(t, ctx, 2)
	h.resumeFrontendAnswer(t, ctx, 3)
	input := DeliveryGraphInput{
		Operation: GraphOpRecordQuestion, DeliveryID: h.deliveryID, Wave: 1, TaskID: "task_02", Execution: 1,
		Prompt: "Persisted fourth physical question?", Choices: []string{"continue"},
	}
	requestID, err := deriveQuestionOperationID(h.scope, input)
	if err != nil {
		t.Fatalf("derive persisted request ID: %v", err)
	}
	journal, exists, err := h.store.Load(h.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("load persisted fourth question: exists=%v error=%v", exists, err)
	}
	delivery := journal.Deliveries[h.deliveryID]
	for index := range delivery.Graph.Tasks {
		task := &delivery.Graph.Tasks[index]
		if task.TaskID != "task_02" {
			continue
		}
		attempt := &task.Attempts[len(task.Attempts)-1]
		attempt.Question = &routing.TaskQuestion{RequestID: requestID, Prompt: input.Prompt, ContextDigest: canonicalTaskContextDigest(input.TaskID), Choices: input.Choices}
		attempt.State, task.State = routing.GraphTaskWaitingInput, routing.GraphTaskWaitingInput
		break
	}
	// The retained state models an older durable fourth-attempt question. Its
	// pause begins at the deterministic service clock after earlier answers.
	pausedAt := delivery.CreatedAt.Add(time.Minute)
	delivery.Graph.Pauses = append(delivery.Graph.Pauses, routing.HumanPause{TaskID: "task_02", Execution: routing.MaxTaskExecutions, RequestID: requestID, StartedAt: pausedAt})
	journal.Deliveries[h.deliveryID] = delivery
	if err := h.store.Save(h.scope.WorkspaceID, journal); err != nil {
		t.Fatalf("persist historical fourth open question: %v", err)
	}
	before := h.snapshot(t)
	replay, err := h.graph.Execute(ctx, h.scope, input)
	if err != nil || replay.Disposition != GraphDispositionWaitingInput || replay.Execution != routing.MaxTaskExecutions || replay.QuestionOperationID != requestID {
		t.Fatalf("persisted fourth open-question replay = %#v error=%v", replay, err)
	}
	if after := h.snapshot(t); after != before {
		t.Fatalf("persisted fourth open-question replay changed full boundary snapshot\nbefore=%s\nafter=%s", before, after)
	}
}

func firstSnapshotDifference(left, right string) int {
	limit := min(len(left), len(right))
	for index := 0; index < limit; index++ {
		if left[index] != right[index] {
			return index
		}
	}
	return limit
}

func TestParallelDeliveryHarnessRejectsSixtyFifthAuthoredTaskBeforeDeliverySideEffects(t *testing.T) {
	h := newParallelDeliveryHarness(t)
	directory := filepath.Join(h.repository.root, ".compozy", "tasks", "sixty-five")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		t.Fatalf("make authored task directory: %v", err)
	}
	var manifest strings.Builder
	manifest.WriteString("---\nschema_version: \"compozy.tasks/v2\"\nworkflow: sixty-five\ngraph:\n  nodes:\n")
	for number := 1; number <= routing.MaxDeliveryTasks+1; number++ {
		id := fmt.Sprintf("task_%02d", number)
		filename := id + ".md"
		manifest.WriteString(fmt.Sprintf("    - id: %s\n      file: %s\n", id, filename))
		payload := "---\nstatus: pending\ntitle: Authored task " + id + "\ntype: backend\ncomplexity: low\n---\n\nDeterministic authored task.\n"
		if err := os.WriteFile(filepath.Join(directory, filename), []byte(payload), 0o600); err != nil {
			t.Fatalf("write %s: %v", filename, err)
		}
	}
	manifest.WriteString("  edges: []\n---\n\n# Sixty-five authored tasks\n")
	if err := os.WriteFile(filepath.Join(directory, "_tasks.md"), []byte(manifest.String()), 0o600); err != nil {
		t.Fatalf("write canonical manifest: %v", err)
	}

	loader, err := routing.NewArtifactLoader(h.repository.root)
	if err != nil {
		t.Fatalf("NewArtifactLoader: %v", err)
	}
	set, err := loader.Load("sixty-five")
	if err != nil || len(set.Tasks) != routing.MaxDeliveryTasks+1 {
		t.Fatalf("Load sixty-five = %d tasks, error=%v", len(set.Tasks), err)
	}
	snapshot, err := set.DeliverySnapshot()
	if err != nil {
		t.Fatalf("DeliverySnapshot: %v", err)
	}
	before := h.snapshot(t)
	if _, err := routing.NewDeliveryGraph(snapshot, routing.RoutingGeneration{}, ""); !errors.Is(err, routing.ErrInvalidDeliveryGraph) {
		t.Fatalf("NewDeliveryGraph(65 authored tasks) error = %v, want ErrInvalidDeliveryGraph", err)
	}
	after := h.snapshot(t)
	if before != after {
		t.Fatalf("65-task graph admission changed delivery boundaries\nbefore=%s\nafter=%s", before, after)
	}
}

func TestParallelDeliveryHarnessRejectsAmbiguousDirtyGitEvidenceWithoutMutation(t *testing.T) {
	ctx := context.Background()
	h := newParallelDeliveryHarness(t)
	if _, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: h.deliveryID}); err != nil {
		t.Fatalf("prepare baseline: %v", err)
	}
	if err := os.WriteFile(filepath.Join(h.repository.root, "project", "ambiguous-evidence.txt"), []byte("untracked=ambiguous\n"), 0o600); err != nil {
		t.Fatalf("write ambiguous Git evidence: %v", err)
	}
	before := h.snapshot(t)
	if _, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: h.deliveryID}); !errors.Is(err, routing.ErrDeliveryConflict) {
		t.Fatalf("prepare with dirty parent evidence error = %v, want delivery conflict", err)
	}
	after := h.snapshot(t)
	if before != after {
		t.Fatalf("ambiguous Git evidence changed delivery boundaries\nbefore=%s\nafter=%s", before, after)
	}
}

func TestDeliveryAttemptServiceRejectsFifthTerminalFreshParentWithoutDuplicateStart(t *testing.T) {
	fixture := newFourFallbackDeliveryServiceFixture(t)
	disableGraphDelivery(t, fixture) // legacy delivery is deliberately capped at four parent attempts.
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		delivery.AttemptCeiling = 4
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("set four-parent attempt ceiling: %v", err)
	}
	ctx := context.Background()
	var last RoutingStartResult
	for attempt := 1; attempt <= 4; attempt++ {
		var err error
		if attempt == 1 {
			last, err = fixture.service.Start(ctx, fixture.scope, fixture.deliveryID)
		} else {
			last, err = fixture.service.Recover(ctx, fixture.scope, fixture.deliveryID, last.DeliveryRunID)
		}
		if err != nil {
			t.Fatalf("start fresh parent attempt %d: %v", attempt, err)
		}
		if last.Attempt != attempt || last.DeliveryRunID != fmt.Sprintf("run_attempt_%d", attempt) {
			t.Fatalf("fresh parent attempt %d = %#v", attempt, last)
		}
		settleFixtureAttemptFailure(t, &fixture, last, fmt.Sprintf("run_implement_%d", attempt), 100)
	}
	beforeFifth := deliveryFixtureSnapshot(t, fixture)
	if _, err := fixture.service.Recover(ctx, fixture.scope, fixture.deliveryID, last.DeliveryRunID); !errors.Is(err, routing.ErrNoEligibleCandidate) {
		t.Fatalf("fifth parent Recover error = %v, want ErrNoEligibleCandidate", err)
	}
	afterFifth := deliveryFixtureSnapshot(t, fixture)
	if afterFifth.Starts != 4 || afterFifth.Journal.Deliveries[fixture.deliveryID].State != routing.DeliveryStateExhausted {
		t.Fatalf("fifth parent started or did not terminalize: %#v", afterFifth)
	}
	if reflect.DeepEqual(beforeFifth, afterFifth) {
		t.Fatal("fifth parent denial did not durably record the exhausted terminal state")
	}
	stable := afterFifth
	if _, err := fixture.service.Recover(ctx, fixture.scope, fixture.deliveryID, last.DeliveryRunID); err == nil {
		t.Fatal("replayed fifth parent Recover error = nil")
	}
	if replayed := deliveryFixtureSnapshot(t, fixture); !reflect.DeepEqual(replayed, stable) {
		t.Fatalf("replayed fifth parent mutated a full boundary snapshot\nbefore=%#v\nafter=%#v", stable, replayed)
	}
}

func TestDeliveryAttemptServiceSeparatelyExhaustsTokenAndActiveWallBudgets(t *testing.T) {
	t.Run("token ceiling", func(t *testing.T) {
		fixture := newDeliveryServiceFixture(t)
		disableGraphDelivery(t, fixture)
		started, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
		if err != nil {
			t.Fatalf("Start: %v", err)
		}
		settleFixtureAttemptFailure(t, &fixture, started, "run_token_exhausted", routing.DeliveryTokenCeiling)
		before := deliveryFixtureSnapshot(t, fixture)
		if _, err := fixture.service.Recover(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID); !errors.Is(err, routing.ErrNoEligibleCandidate) {
			t.Fatalf("Recover(token ceiling): %v", err)
		}
		after := deliveryFixtureSnapshot(t, fixture)
		if after.Starts != 1 || after.Journal.Deliveries[fixture.deliveryID].State != routing.DeliveryStateExhausted {
			t.Fatalf("token ceiling snapshot = %#v", after)
		}
		if _, err := fixture.service.Recover(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID); err == nil {
			t.Fatal("token ceiling replay unexpectedly recovered")
		}
		if replay := deliveryFixtureSnapshot(t, fixture); !reflect.DeepEqual(replay, after) {
			t.Fatalf("token ceiling replay changed state\nbefore=%#v\nafter=%#v", after, replay)
		}
		if reflect.DeepEqual(before, after) { // the terminal state is the only permitted durable transition.
			t.Fatal("token ceiling did not record terminal exhaustion")
		}
	})

	t.Run("active wall", func(t *testing.T) {
		fixture := newDeliveryServiceFixture(t)
		disableGraphDelivery(t, fixture)
		journal, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
		if err != nil || !exists {
			t.Fatalf("load active-wall deadline: exists=%v error=%v", exists, err)
		}
		fixture.service.Now = func() time.Time { return journal.Deliveries[fixture.deliveryID].AbsoluteDeadline }
		before := deliveryFixtureSnapshot(t, fixture)
		if _, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID); !errors.Is(err, routing.ErrNoEligibleCandidate) {
			t.Fatalf("Start(expired active wall): %v", err)
		}
		after := deliveryFixtureSnapshot(t, fixture)
		if after.Starts != 0 || after.Journal.Deliveries[fixture.deliveryID].State != routing.DeliveryStateExhausted {
			t.Fatalf("active-wall snapshot = %#v", after)
		}
		if _, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID); err == nil {
			t.Fatal("active-wall replay unexpectedly started")
		}
		if replay := deliveryFixtureSnapshot(t, fixture); !reflect.DeepEqual(replay, after) {
			t.Fatalf("active-wall replay changed state\nbefore=%#v\nafter=%#v", after, replay)
		}
		if reflect.DeepEqual(before, after) {
			t.Fatal("active-wall ceiling did not record terminal exhaustion")
		}
	})
}

func TestDeliveryAttemptServiceReconcilesCanceledAndStalledNoProgressParentTerminals(t *testing.T) {
	for _, terminal := range []struct {
		name   string
		status string
	}{
		{name: "canceled", status: "canceled"},
		// The public parent status is the only no-progress signal this pinned
		// runtime exposes; it carries no distinct reason metadata for Batuta to
		// validate or persist.
		{name: "stalled_no_progress", status: "stalled"},
	} {
		t.Run(terminal.name, func(t *testing.T) {
			fixture := newDeliveryServiceFixture(t)
			disableGraphDelivery(t, fixture)
			started, err := fixture.service.Start(context.Background(), fixture.scope, fixture.deliveryID)
			if err != nil {
				t.Fatalf("Start: %v", err)
			}
			fixture.client.statuses = map[string]deliveryRunDetail{
				started.DeliveryRunID:             testParentRunDetail(fixture.scope.WorkspaceID, started.DeliveryRunID, terminal.status, fixture.client.lastRequest, []deliveryOutput{{NodeID: "implement", Status: "failed", ChildLoopRunID: "run_terminal_" + terminal.status}}),
				"run_terminal_" + terminal.status: testChildRunDetail(fixture.scope.WorkspaceID, "run_terminal_"+terminal.status, "implement-tasks", terminal.status, 0, nil),
			}
			settled, err := fixture.service.Reconcile(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID)
			if err != nil || settled.Recoverable || settled.State != string(routing.DeliveryStateBlocked) || settled.BlockerCode != "terminal_not_recoverable" {
				t.Fatalf("Reconcile(%s) = %#v error=%v", terminal.status, settled, err)
			}
			before := deliveryFixtureSnapshot(t, fixture)
			if _, err := fixture.service.Recover(context.Background(), fixture.scope, fixture.deliveryID, started.DeliveryRunID); err == nil {
				t.Fatalf("Recover(%s) unexpectedly started", terminal.status)
			}
			if after := deliveryFixtureSnapshot(t, fixture); !reflect.DeepEqual(after, before) {
				t.Fatalf("%s terminal replay mutated journal or external start counters\nbefore=%#v\nafter=%#v", terminal.status, before, after)
			}
		})
	}
}

func TestDeliveryGraphServiceExcludesExactSingleTaskHumanPauseAndReplaysAnswer(t *testing.T) {
	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	journal, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("load single-task pause fixture: exists=%v error=%v", exists, err)
	}
	clock := journal.Deliveries[fixture.deliveryID].CreatedAt.Add(10 * time.Minute)
	delivery := journal.Deliveries[fixture.deliveryID]
	task, found := delivery.Graph.Task("task_01")
	if !found {
		t.Fatal("task_01 missing from single-task pause graph")
	}
	run := deliveryRun{ID: "run_exact_pause", WorkspaceID: fixture.scope.WorkspaceID, LoopName: "batuta-task", Status: "running", CreatedAt: clock, StartedAt: clock, Inputs: graphTaskRunInputs(delivery, wave, task, 1)}
	runs := &fakeGraphRunReader{recent: []deliveryRun{run}, statuses: map[string]deliveryRunDetail{}}
	service := deliveryGraphService{Store: fixture.store, Runs: runs, Now: func() time.Time { return clock }}
	questionInput := DeliveryGraphInput{Operation: GraphOpRecordQuestion, DeliveryID: fixture.deliveryID, Wave: wave.Number, TaskID: "task_01", Execution: 1, Prompt: "Continue after exactly seven minutes?", Choices: []string{"continue"}}
	question, err := service.Execute(context.Background(), fixture.scope, questionInput)
	if err != nil || question.Disposition != GraphDispositionWaitingInput {
		t.Fatalf("record exact pause question = %#v error=%v", question, err)
	}
	clock = clock.Add(7 * time.Minute)
	runs.statuses[run.ID] = deliveryRunDetail{Run: run, Requests: []deliveryRequest{{
		LoopRunID: run.ID, LoopName: "batuta-task", Generation: 2, NodeID: "ask_operator", ItemIndex: 0,
		Kind: "ask", State: "answered", Prompt: questionInput.Prompt, Context: json.RawMessage(`{"task_id":"task_01"}`),
		Expect: taskAnswerExpectation(), Decisions: []string{"respond"}, Agents: "deny", AnsweredDecision: "respond", ActorKind: "human", ActorID: "operator-pause", AnsweredAt: timePointer(clock), ResolvedAt: timePointer(clock),
	}}, Generations: []deliveryGeneration{{Generation: 2, Outputs: []deliveryOutput{{NodeID: "ask_operator", ItemIndex: 0, Status: "succeeded", OutputRef: answeredAskOutputRef("continue")}}}}}
	answerInput := DeliveryGraphInput{Operation: GraphOpRecordAnswer, DeliveryID: fixture.deliveryID, Wave: wave.Number, TaskID: "task_01", Execution: 1, QuestionOperationID: question.QuestionOperationID, Answer: "continue"}
	answer, err := service.Execute(context.Background(), fixture.scope, answerInput)
	if err != nil || answer.Disposition != GraphDispositionTaskReady || answer.Execution != 2 {
		t.Fatalf("record exact pause answer = %#v error=%v", answer, err)
	}
	journal, exists, err = fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("load closed exact pause: exists=%v error=%v", exists, err)
	}
	delivery = journal.Deliveries[fixture.deliveryID]
	pause := delivery.Graph.Pauses[0]
	if pause.EndedAt == nil || !pause.StartedAt.Equal(clock.Add(-7*time.Minute)) || !pause.EndedAt.Equal(clock) {
		t.Fatalf("exact pause interval = %#v", pause)
	}
	remaining, err := delivery.RemainingActiveWall(clock)
	if err != nil || remaining != 3*time.Hour+50*time.Minute {
		t.Fatalf("remaining active wall = %s error=%v, want 3h50m", remaining, err)
	}
	before := snapshotGraphRunBoundary(t, fixture, runs)
	replay, err := service.Execute(context.Background(), fixture.scope, answerInput)
	if err != nil || !replay.Replayed || runs.statusCalls != 1 {
		t.Fatalf("exact pause answer replay = %#v error=%v status_calls=%d", replay, err, runs.statusCalls)
	}
	if after := snapshotGraphRunBoundary(t, fixture, runs); !reflect.DeepEqual(before, after) {
		t.Fatalf("exact pause replay changed full boundary snapshot\nbefore=%#v\nafter=%#v", before, after)
	}
}

type graphRunBoundarySnapshot struct {
	Delivery deliveryFixtureBoundarySnapshot
	Recent   []deliveryRun
	Statuses map[string]deliveryRunDetail
	Reads    int
	Lookups  int
}

func snapshotGraphRunBoundary(t *testing.T, fixture deliveryServiceFixture, runs *fakeGraphRunReader) graphRunBoundarySnapshot {
	t.Helper()
	return graphRunBoundarySnapshot{
		Delivery: deliveryFixtureSnapshot(t, fixture),
		Recent:   append([]deliveryRun(nil), runs.recent...),
		Statuses: appendDeliveryRunDetails(nil, runs.statuses),
		Reads:    runs.recentCalls,
		Lookups:  runs.statusCalls,
	}
}

type deliveryFixtureBoundarySnapshot struct {
	Journal           routing.RoutingJournal
	Refs              string
	PhysicalWorktrees string
	ChildInventory    map[string]deliveryRunDetail
	Starts            int
	Recent            int
	Statuses          int
	WorktreeCreates   int
	WorktreeRemoves   int
}

func deliveryFixtureSnapshot(t *testing.T, fixture deliveryServiceFixture) deliveryFixtureBoundarySnapshot {
	t.Helper()
	ensureDeliveryFixtureRepository(t, fixture.scope.WorkspaceRoot)
	journal, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("load delivery fixture snapshot: exists=%v error=%v", exists, err)
	}
	return deliveryFixtureBoundarySnapshot{
		Journal:           journal,
		Refs:              strings.TrimSpace(deliveryFixtureGit(t, fixture.scope.WorkspaceRoot, "show-ref")),
		PhysicalWorktrees: strings.TrimSpace(deliveryFixtureGit(t, fixture.scope.WorkspaceRoot, "worktree", "list", "--porcelain")),
		ChildInventory:    appendDeliveryRunDetails(nil, fixture.client.statuses),
		Starts:            fixture.client.startCalls, Recent: fixture.client.recentCalls, Statuses: fixture.client.statusCalls,
		// Parent-attempt fixtures intentionally have no graph worktree service;
		// recording these explicit zero counters keeps their boundary shape
		// identical to the production graph fixture snapshot.
		WorktreeCreates: 0, WorktreeRemoves: 0,
	}
}

func appendDeliveryRunDetails(destination map[string]deliveryRunDetail, source map[string]deliveryRunDetail) map[string]deliveryRunDetail {
	if len(source) == 0 {
		return nil
	}
	destination = make(map[string]deliveryRunDetail, len(source))
	for id, detail := range source {
		destination[id] = detail
	}
	return destination
}

func ensureDeliveryFixtureRepository(t *testing.T, root string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(root, ".git")); err == nil {
		return
	} else if !os.IsNotExist(err) {
		t.Fatalf("stat fixture Git directory: %v", err)
	}
	deliveryFixtureGit(t, "", "init", "-b", "main", root)
	deliveryFixtureGit(t, root, "config", "user.email", "batuta@example.invalid")
	deliveryFixtureGit(t, root, "config", "user.name", "Batuta Delivery Fixture")
	deliveryFixtureGit(t, root, "add", "--all")
	deliveryFixtureGit(t, root, "commit", "-m", "chore: initialize delivery boundary fixture")
}

func deliveryFixtureGit(t *testing.T, root string, arguments ...string) string {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git executable is unavailable")
	}
	command := exec.Command(git, arguments...)
	command.Dir = root
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("fixture git %v in %q: %v\n%s", arguments, root, err, output)
	}
	return string(output)
}

func newFourFallbackDeliveryServiceFixture(t *testing.T) deliveryServiceFixture {
	t.Helper()
	root := t.TempDir()
	writeRoutingTask(t, root)
	// A high-complexity authored task has the production three-fallback limit:
	// selected runtime plus three distinct eligible fallbacks proves four fresh
	// parent attempts before the journal ceiling refuses a fifth.
	body := "---\nstatus: pending\ntitle: High fallback demo\ntype: frontend\ncomplexity: high\n---\n\n# Demo\n"
	if err := os.WriteFile(filepath.Join(root, ".compozy", "tasks", "demo", "task_01.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write high-complexity task: %v", err)
	}
	scope := publication.TrustedScope{WorkspaceID: "ws_demo", WorkspaceRoot: root}
	engine := routingEngine{inventory: func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error) {
		return fourFallbackInventory(t), nil
	}}
	plan := RoutingPlanInput{
		Slug:      "demo",
		Proposals: []routing.ClassificationProposal{{TaskID: "task_01", Domain: routing.DomainFrontend, Complexity: routing.ComplexityHigh, Confidence: 0.95}},
		Fit: []RoutingFitProposal{{
			TaskIDs: []string{"task_01"}, Domain: routing.DomainFrontend, Complexity: routing.ComplexityHigh,
			Candidates: []routing.FitCandidate{
				{ExecutorID: inventory.ExecutorCursorAgent, ProviderID: "cursor", ModelID: "grok-4.6", Score: 0.98},
				{ExecutorID: inventory.ExecutorCursorAgent, ProviderID: "cursor", ModelID: "grok-4.6[effort=high,fast=true]", Score: 0.97},
				{ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "gpt-5.6-sol", Score: 0.96},
				{ExecutorID: inventory.ExecutorOpenCode, ProviderID: "opencode", ModelID: "anthropic/claude-opus-5", Score: 0.95},
			},
		}},
	}
	generation, err := engine.Plan(context.Background(), scope, plan)
	if err != nil {
		t.Fatalf("plan four fallbacks: %v", err)
	}
	loader, _ := routing.NewArtifactLoader(root)
	taskSet, _ := loader.Load("demo")
	taskSnapshot, _ := taskSet.DeliverySnapshot()
	store, _ := routing.NewOwnershipStore(t.TempDir())
	fingerprint := routing.WorktreeFingerprint{HeadSHA: "0123456789abcdef0123456789abcdef01234567", PorcelainSHA256: digestValue("porcelain"), ContentSHA256: digestValue("content")}
	matrix, err := (routing.MatrixManager{Store: store}).Apply(context.Background(), routing.MatrixApplyInput{
		WorkspaceID: scope.WorkspaceID, WorkspaceRoot: root, WorktreeID: "wt_demo", WorktreeRoot: root,
		Slug: "demo", OriginSessionID: "session_demo", TaskSetDigest: taskSet.Digest, TaskSnapshot: taskSnapshot,
		InitialWorktreeFingerprint: fingerprint, Generation: generation,
	})
	if err != nil {
		t.Fatalf("apply four fallback matrix: %v", err)
	}
	now := matrix.CreatedAt.Add(time.Minute)
	client := &fakeDeliveryRunClient{now: now}
	service := deliveryAttemptService{
		Store: store, Client: client, Now: func() time.Time { return now },
		WorktreeState: func(context.Context, string) (publication.WorktreeState, error) {
			return publication.WorktreeState{HeadSHA: fingerprint.HeadSHA, PorcelainSHA256: fingerprint.PorcelainSHA256, ContentSHA256: fingerprint.ContentSHA256}, nil
		},
	}
	return deliveryServiceFixture{service: service, client: client, store: store, scope: scope, deliveryID: matrix.DeliveryID, now: now}
}

func fourFallbackInventory(t *testing.T) inventory.InventorySnapshot {
	t.Helper()
	const catalog = "catalog-four-fallbacks"
	models := inventory.Evidence{Name: "models", Source: "fixture", State: inventory.ResolutionResolved, Digest: catalog,
		Identifiers: []string{"codex/gpt-5.6-sol", "cursor/grok-4.6", "cursor/grok-4.6[effort=high,fast=true]", "opencode/anthropic/claude-opus-5"}}
	snapshot, err := inventory.NewSnapshot(catalog, []inventory.ExecutorSnapshot{
		{ID: inventory.ExecutorCompozy, Availability: inventory.AvailabilityAvailable, Health: inventory.Evidence{Name: "health", State: inventory.ResolutionResolved}, Capabilities: []inventory.Evidence{models}, CredentialState: inventory.CredentialUnknown},
		{ID: inventory.ExecutorCursorAgent, Availability: inventory.AvailabilityAvailable, Health: inventory.Evidence{Name: "health", State: inventory.ResolutionResolved}, Capabilities: []inventory.Evidence{{Name: "models", Source: "fixture", State: inventory.ResolutionResolved, Digest: catalog, Identifiers: []string{"cursor/grok-4.6", "cursor/grok-4.6[effort=high,fast=true]"}}}, CredentialState: inventory.CredentialConfigured},
		{ID: inventory.ExecutorCodex, Availability: inventory.AvailabilityAvailable, Health: inventory.Evidence{Name: "health", State: inventory.ResolutionResolved}, Capabilities: []inventory.Evidence{{Name: "models", Source: "fixture", State: inventory.ResolutionResolved, Digest: catalog, Identifiers: []string{"codex/gpt-5.6-sol"}}}, CredentialState: inventory.CredentialConfigured},
		{ID: inventory.ExecutorOpenCode, Availability: inventory.AvailabilityAvailable, Health: inventory.Evidence{Name: "health", State: inventory.ResolutionResolved}, Capabilities: []inventory.Evidence{{Name: "models", Source: "fixture", State: inventory.ResolutionResolved, Digest: catalog, Identifiers: []string{"opencode/anthropic/claude-opus-5"}}}, CredentialState: inventory.CredentialConfigured},
	})
	if err != nil {
		t.Fatalf("four fallback inventory: %v", err)
	}
	return snapshot
}

type parallelDeliveryHarness struct {
	repository   *parallelDeliveryRepository
	scope        publication.TrustedScope
	store        *routing.OwnershipStore
	deliveryID   string
	graph        *deliveryGraphService
	worktrees    *parallelGitWorktreeClient
	runs         *fakeGraphRunReader
	reviewCalls  int
	childStarts  []deliveryRun
	forge        *parallelPublicationForge
	planner      publication.PublicationPlanner
	publisher    publication.Publisher
	published    *publication.PublishOutput
	reviewedHead string
}

func preparedTask(t *testing.T, output DeliveryGraphOutput, taskID string) DeliveryGraphTask {
	t.Helper()
	for _, task := range output.Tasks {
		if task.TaskID == taskID {
			return task
		}
	}
	t.Fatalf("prepared task %q is absent from %#v", taskID, output)
	return DeliveryGraphTask{}
}

func (h *parallelDeliveryHarness) startPreparedChildren(t *testing.T, prepared DeliveryGraphOutput) {
	t.Helper()
	for _, descriptor := range prepared.Tasks {
		delivery, wave, task := h.graphTask(t, descriptor.TaskID)
		if descriptor.Wave != wave.Number || descriptor.Execution != task.Attempts[len(task.Attempts)-1].Execution {
			t.Fatalf("child starter descriptor mismatch: %#v", descriptor)
		}
		run := deliveryRun{
			ID: fmt.Sprintf("run_started_%s_%d", descriptor.TaskID, descriptor.Execution), WorkspaceID: h.scope.WorkspaceID,
			LoopName: "batuta-task", Status: "running", CreatedAt: time.Now().UTC(), StartedAt: time.Now().UTC(),
			Inputs: graphTaskRunInputs(delivery, wave, task, descriptor.Execution),
		}
		for _, existing := range h.childStarts {
			if existing.ID == run.ID {
				if existing.WorkspaceID != run.WorkspaceID || existing.LoopName != run.LoopName || !reflect.DeepEqual(existing.Inputs, run.Inputs) {
					t.Fatalf("child start identity collision: existing=%#v next=%#v", existing, run)
				}
				goto next
			}
		}
		h.childStarts = append(h.childStarts, run)
	next:
	}
}

func (h *parallelDeliveryHarness) graphTask(t *testing.T, taskID string) (routing.DeliveryRecord, routing.DeliveryWave, routing.GraphTask) {
	t.Helper()
	journal, exists, err := h.store.Load(h.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("load graph journal: exists=%v error=%v", exists, err)
	}
	delivery := journal.Deliveries[h.deliveryID]
	task, found := delivery.Graph.Task(taskID)
	if !found || len(delivery.Graph.Waves) == 0 {
		t.Fatalf("graph task %q is absent from %#v", taskID, delivery.Graph)
	}
	attempt := task.Attempts[len(task.Attempts)-1]
	wave, found := graphWaveForBase(delivery.Graph, taskID, attempt.BaseHeadSHA)
	if !found {
		t.Fatalf("graph wave for %q base %s is absent from %#v", taskID, attempt.BaseHeadSHA, delivery.Graph.Waves)
	}
	return delivery, wave, task
}

func (h *parallelDeliveryHarness) snapshot(t *testing.T) string {
	t.Helper()
	journal, exists, err := h.store.Load(h.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("snapshot journal: exists=%v error=%v", exists, err)
	}
	refs := strings.TrimSpace(h.repository.run(t, h.repository.root, "show-ref"))
	physicalWorktrees := strings.TrimSpace(h.repository.run(t, h.repository.root, "worktree", "list", "--porcelain"))
	worktrees := make(map[string]string, len(h.worktrees.byID))
	for id, worktree := range h.worktrees.byID {
		worktrees[id] = worktree.State + ":" + worktree.Root
	}
	children := append([]deliveryRun(nil), h.childStarts...)
	slices.SortFunc(children, func(left, right deliveryRun) int { return strings.Compare(left.ID, right.ID) })
	payload, err := json.Marshal(struct {
		Journal           routing.RoutingJournal `json:"journal"`
		Refs              string                 `json:"refs"`
		PhysicalWorktrees string                 `json:"physical_worktrees"`
		Worktrees         map[string]string      `json:"worktrees"`
		ChildInventory    []deliveryRun          `json:"child_inventory"`
		Recent            int                    `json:"recent_calls"`
		Statuses          int                    `json:"status_calls"`
		Creates           int                    `json:"create_calls"`
		Removes           int                    `json:"remove_calls"`
		ReviewCalls       int                    `json:"review_calls"`
		PushCalls         int                    `json:"push_calls"`
		PullRequestCalls  int                    `json:"pull_request_calls"`
	}{journal, refs, physicalWorktrees, worktrees, children, h.runs.recentCalls, h.runs.statusCalls, h.worktrees.createCalls, h.worktrees.removeCalls, h.reviewCalls, h.forge.pushCalls, h.forge.openPRCalls})
	if err != nil {
		t.Fatalf("marshal real boundary snapshot: %v", err)
	}
	return string(payload)
}

func (h *parallelDeliveryHarness) answerFrontend(t *testing.T, ctx context.Context, prepared DeliveryGraphOutput) DeliveryGraphTask {
	t.Helper()
	frontend := preparedTask(t, prepared, "task_02")
	delivery, wave, task := h.graphTask(t, "task_02")
	run := deliveryRun{
		ID: "run_parallel_frontend", WorkspaceID: h.scope.WorkspaceID, LoopName: "batuta-task", Status: "running",
		CreatedAt: time.Now().UTC(), StartedAt: time.Now().UTC(), Inputs: graphTaskRunInputs(delivery, wave, task, frontend.Execution),
	}
	questionInput := DeliveryGraphInput{
		Operation: GraphOpRecordQuestion, DeliveryID: h.deliveryID, Wave: frontend.Wave,
		TaskID: "task_02", Execution: frontend.Execution, Prompt: "Which shared frontend value should be preserved?",
		Choices: []string{"shared=frontend", "shared=backend"},
	}
	h.runs.recent = []deliveryRun{run}
	question, err := h.graph.Execute(ctx, h.scope, questionInput)
	if err != nil || question.Disposition != GraphDispositionWaitingInput {
		t.Fatalf("record_question = %#v, error=%v", question, err)
	}
	questionSnapshot := h.snapshot(t)
	questionReplay, replayErr := h.graph.Execute(ctx, h.scope, questionInput)
	if replayErr != nil || !reflect.DeepEqual(questionReplay, question) || h.snapshot(t) != questionSnapshot {
		t.Fatalf("record_question replay = %#v error=%v", questionReplay, replayErr)
	}
	backend := preparedTask(t, prepared, "task_01")
	h.repository.writeAndCommit(t, backend.WorktreeRoot, "project/shared.txt", "shared=backend\n", "feat(backend): set shared fixture value")
	answeredAt := time.Now().UTC()
	h.runs.statuses[run.ID] = deliveryRunDetail{
		Run: run,
		Requests: []deliveryRequest{{
			LoopRunID: run.ID, LoopName: "batuta-task", Generation: 2, NodeID: "ask_operator", ItemIndex: 0,
			Kind: "ask", State: "answered", Prompt: questionInput.Prompt, Context: json.RawMessage(`{"task_id":"task_02"}`),
			Expect: taskAnswerExpectation(), Decisions: []string{"respond"}, Agents: "deny", AnsweredDecision: "respond",
			ActorKind: "human", ActorID: "operator-parallel", AnsweredAt: timePointer(answeredAt), ResolvedAt: timePointer(answeredAt),
		}},
		Generations: []deliveryGeneration{{Generation: 2, Outputs: []deliveryOutput{{
			NodeID: "ask_operator", ItemIndex: 0, Status: "succeeded", OutputRef: answeredAskOutputRef("shared=frontend"),
		}}}},
	}
	answered, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{
		Operation: GraphOpRecordAnswer, DeliveryID: h.deliveryID, Wave: frontend.Wave,
		TaskID: "task_02", Execution: frontend.Execution, QuestionOperationID: question.QuestionOperationID, Answer: "shared=frontend",
	})
	if err != nil || answered.Disposition != GraphDispositionTaskReady || answered.Execution != frontend.Execution+1 {
		t.Fatalf("record_answer = %#v, error=%v", answered, err)
	}
	answerSnapshot := h.snapshot(t)
	answerReplay, replayErr := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{
		Operation: GraphOpRecordAnswer, DeliveryID: h.deliveryID, Wave: frontend.Wave,
		TaskID: "task_02", Execution: frontend.Execution, QuestionOperationID: question.QuestionOperationID, Answer: "shared=frontend",
	})
	if replayErr != nil || !answerReplay.Replayed || h.snapshot(t) != answerSnapshot {
		t.Fatalf("record_answer replay = %#v error=%v", answerReplay, replayErr)
	}
	_, wave, task = h.graphTask(t, "task_02")
	attempt := task.Attempts[len(task.Attempts)-1]
	return DeliveryGraphTask{
		Wave: wave.Number, TaskID: task.TaskID, Execution: attempt.Execution, Domain: task.Domain, Complexity: task.Complexity,
		Runtime: attempt.Runtime, WorktreeID: attempt.WorktreeID, WorktreeRoot: attempt.WorktreeRoot, BaseSHA: attempt.BaseHeadSHA,
	}
}

func (h *parallelDeliveryHarness) resumeFrontendAnswer(t *testing.T, ctx context.Context, physicalExecution int) {
	t.Helper()
	delivery, wave, task := h.graphTask(t, "task_02")
	if len(task.Attempts) != physicalExecution {
		t.Fatalf("resume physical execution = %d, want %d", len(task.Attempts), physicalExecution)
	}
	run := deliveryRun{ID: "run_parallel_frontend", WorkspaceID: h.scope.WorkspaceID, LoopName: "batuta-task", Status: "running", CreatedAt: time.Now().UTC(), StartedAt: time.Now().UTC(), Inputs: graphTaskRunInputs(delivery, wave, task, 1)}
	prompt := fmt.Sprintf("Which frontend value applies at continuation %d?", physicalExecution)
	h.runs.recent = []deliveryRun{run}
	question, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpRecordQuestion, DeliveryID: h.deliveryID, Wave: wave.Number, TaskID: "task_02", Execution: 1, Prompt: prompt, Choices: []string{"shared=frontend"}})
	if err != nil || question.Disposition != GraphDispositionWaitingInput {
		t.Fatalf("resume question %d = %#v error=%v", physicalExecution, question, err)
	}
	at := time.Now().UTC()
	h.runs.statuses[run.ID] = deliveryRunDetail{Run: run, Requests: []deliveryRequest{{LoopRunID: run.ID, LoopName: "batuta-task", Generation: physicalExecution + 1, NodeID: "ask_operator", ItemIndex: 0, Kind: "ask", State: "answered", Prompt: prompt, Context: json.RawMessage(`{"task_id":"task_02"}`), Expect: taskAnswerExpectation(), Decisions: []string{"respond"}, Agents: "deny", AnsweredDecision: "respond", ActorKind: "human", ActorID: "operator-parallel", AnsweredAt: timePointer(at), ResolvedAt: timePointer(at)}}, Generations: []deliveryGeneration{{Generation: int64(physicalExecution + 1), Outputs: []deliveryOutput{{NodeID: "ask_operator", ItemIndex: 0, Status: "succeeded", OutputRef: answeredAskOutputRef("shared=frontend")}}}}}
	answer, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpRecordAnswer, DeliveryID: h.deliveryID, Wave: wave.Number, TaskID: "task_02", Execution: 1, QuestionOperationID: question.QuestionOperationID, Answer: "shared=frontend"})
	if err != nil || answer.Disposition != GraphDispositionTaskReady || answer.Execution != physicalExecution+1 {
		t.Fatalf("resume answer %d = %#v error=%v", physicalExecution, answer, err)
	}
}

func (h *parallelDeliveryHarness) completeRemainingParallelDelivery(t *testing.T, ctx context.Context, initial, retry DeliveryGraphOutput) {
	t.Helper()
	frontend := preparedTask(t, retry, "task_02")
	h.repository.writeAndCommit(t, frontend.WorktreeRoot, "project/frontend-resolution.txt", "frontend=resolved\n", "fix(frontend): resolve shared fixture conflict")
	if err := h.recordRealCandidate(ctx, "task_02", frontend.Execution, "run_parallel_frontend_retry"); err != nil {
		t.Fatalf("record frontend retry candidate: %v", err)
	}
	settled, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpSettleWave, DeliveryID: h.deliveryID, Wave: frontend.Wave})
	if err != nil || settled.Disposition != GraphDispositionWaveIntegrated {
		t.Fatalf("settle frontend retry wave = %#v, error=%v", settled, err)
	}

	if err := h.recordRealFailure(ctx, "task_04", 1, "run_parallel_docs"); err != nil {
		t.Fatalf("record docs terminal status: %v", err)
	}
	if err := h.recordRealFailure(ctx, "task_03", 1, "run_parallel_testing"); err != nil {
		t.Fatalf("record testing terminal status: %v", err)
	}
	complete := func(taskID, childRunID, path, content, message string) {
		prepared, prepareErr := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: h.deliveryID})
		if prepareErr != nil {
			t.Fatalf("prepare %s retry wave: %v", taskID, prepareErr)
		}
		task := preparedTask(t, prepared, taskID)
		h.repository.writeAndCommit(t, task.WorktreeRoot, path, content, message)
		if candidateErr := h.recordRealCandidate(ctx, taskID, task.Execution, childRunID); candidateErr != nil {
			t.Fatalf("record %s candidate: %v", taskID, candidateErr)
		}
		settlement, settleErr := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpSettleWave, DeliveryID: h.deliveryID, Wave: task.Wave})
		if settleErr != nil || settlement.Disposition != GraphDispositionWaveIntegrated {
			t.Fatalf("settle %s wave = %#v, error=%v", taskID, settlement, settleErr)
		}
	}
	complete("task_03", "run_parallel_testing_retry", "project/testing.txt", "testing=complete\n", "test: add fixture coverage")
	complete("task_04", "run_parallel_docs_retry", "project/docs.txt", "docs=complete\n", "docs: add fixture evidence")

	finalWave, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: h.deliveryID})
	if err != nil {
		t.Fatalf("prepare dependent task: %v", err)
	}
	dependentPrepareSnapshot := h.snapshot(t)
	dependentPrepareReplay, replayErr := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: h.deliveryID})
	if replayErr != nil || !reflect.DeepEqual(dependentPrepareReplay, finalWave) || h.snapshot(t) != dependentPrepareSnapshot {
		t.Fatalf("dependent prepare replay = %#v error=%v", dependentPrepareReplay, replayErr)
	}
	dependent := preparedTask(t, finalWave, "task_05")
	h.repository.writeAndCommit(t, dependent.WorktreeRoot, "project/fullstack.txt", "fullstack=integrated\n", "feat(fullstack): compose integrated fixture")
	if err := h.recordRealCandidate(ctx, "task_05", dependent.Execution, "run_parallel_fullstack"); err != nil {
		t.Fatalf("record dependent candidate: %v", err)
	}
	final, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpSettleWave, DeliveryID: h.deliveryID, Wave: dependent.Wave})
	if err != nil || final.Disposition != GraphDispositionAllIntegrated {
		t.Fatalf("settle dependent wave = %#v, error=%v", final, err)
	}
	if dependent.Wave <= preparedTask(t, initial, "task_04").Wave {
		t.Fatalf("dependent task admitted before prerequisites: %#v", dependent)
	}
	h.assertOneIntegratedConventionalCommitPerTask(t)
	h.publishReviewedParallelHead(t, ctx)
	publicationSnapshot := h.snapshot(t)
	pushes, prs := h.forge.pushCalls, h.forge.openPRCalls
	h.publishReviewedParallelHead(t, ctx)
	if h.snapshot(t) != publicationSnapshot || h.reviewCalls != 1 || h.forge.pushCalls != pushes || h.forge.openPRCalls != prs {
		t.Fatalf("publication replay duplicated mutation: review=%d push=%d/%d pr=%d/%d", h.reviewCalls, h.forge.pushCalls, pushes, h.forge.openPRCalls, prs)
	}
	dirty := preparedTask(t, initial, "task_03")
	if err := os.WriteFile(filepath.Join(dirty.WorktreeRoot, "project", "retained.txt"), []byte("retain=diagnostic\n"), 0o600); err != nil {
		t.Fatalf("dirty retained worktree: %v", err)
	}
	cleanup, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpCleanup, DeliveryID: h.deliveryID})
	if err != nil || cleanup.Disposition != GraphDispositionBlocked {
		t.Fatalf("cleanup retained worktree = %#v, error=%v", cleanup, err)
	}
	retained := false
	for _, result := range cleanup.CleanupResults {
		if result.WorktreeID == dirty.WorktreeID && result.State == string(routing.CleanupRetained) && result.BlockerCode == "worktree_evidence_changed" {
			retained = true
		}
	}
	if !retained {
		t.Fatalf("cleanup did not retain dirty worktree %s: %#v", dirty.WorktreeID, cleanup.CleanupResults)
	}
	journalAfterCleanup, exists, err := h.store.Load(h.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("load physical cleanup inventory: exists=%v error=%v", exists, err)
	}
	for _, task := range journalAfterCleanup.Deliveries[h.deliveryID].Graph.Tasks {
		for _, attempt := range task.Attempts {
			worktree, found := h.worktrees.byID[attempt.WorktreeID]
			if !found {
				t.Fatalf("cleanup lost worktree inventory for %s execution %d", task.TaskID, attempt.Execution)
			}
			if attempt.WorktreeID == dirty.WorktreeID {
				if worktree.State != "ready" {
					t.Fatalf("diagnostic worktree %s state=%s, want ready", attempt.WorktreeID, worktree.State)
				}
				if _, statErr := os.Stat(worktree.Root); statErr != nil {
					t.Fatalf("retained diagnostic worktree %s missing: %v", worktree.Root, statErr)
				}
				continue
			}
			if worktree.State != "removed" {
				t.Fatalf("eligible worktree %s state=%s, want removed", attempt.WorktreeID, worktree.State)
			}
			if _, statErr := os.Stat(worktree.Root); !os.IsNotExist(statErr) {
				t.Fatalf("eligible worktree %s remains on disk: %v", worktree.Root, statErr)
			}
		}
	}
	cleanupSnapshot := h.snapshot(t)
	replayed, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{Operation: GraphOpCleanup, DeliveryID: h.deliveryID})
	if err != nil || replayed.Disposition != GraphDispositionBlocked {
		t.Fatalf("replay cleanup retained worktree = %#v, error=%v", replayed, err)
	}
	if replay := h.snapshot(t); replay != cleanupSnapshot {
		t.Fatalf("cleanup replay changed full boundary snapshot\nbefore=%s\nafter=%s", cleanupSnapshot, replay)
	}
	h.validateParallelDeliveryPythonEvidence(t, initial, dirty, cleanup)
}

func (h *parallelDeliveryHarness) assertOneIntegratedConventionalCommitPerTask(t *testing.T) {
	t.Helper()
	journal, exists, err := h.store.Load(h.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("load integrated history journal: exists=%v error=%v", exists, err)
	}
	delivery := journal.Deliveries[h.deliveryID]
	base := delivery.InitialWorktreeFingerprint.HeadSHA
	records := strings.TrimSpace(h.repository.run(t, h.repository.root, "log", "--reverse", "--format=%H%x00%s", base+"..HEAD"))
	if records == "" {
		t.Fatal("integrated branch has no task commits")
	}
	parts := strings.Split(records, "\n")
	seen := make(map[string]int, len(parts))
	implementationCommits := 0
	trackingCommits := 0
	for _, record := range parts {
		fields := strings.SplitN(record, "\x00", 2)
		if len(fields) != 2 || !strings.Contains(fields[1], ":") {
			t.Fatalf("integrated commit is not Conventional Commit evidence: %q", record)
		}
		seen[fields[0]]++
		if fields[1] == "chore: sync Batuta task tracking" {
			trackingCommits++
		} else {
			implementationCommits++
		}
	}
	if implementationCommits != len(delivery.Graph.Tasks) || trackingCommits != len(delivery.Graph.Tasks) {
		t.Fatalf("integrated history implementations=%d tracking=%d, want one each per %d tasks: %q", implementationCommits, trackingCommits, len(delivery.Graph.Tasks), records)
	}
	for _, task := range delivery.Graph.Tasks {
		if task.State != routing.GraphTaskIntegrated || seen[task.IntegratedCommitSHA] != 1 {
			t.Fatalf("task %s integrated history evidence: state=%s sha=%s count=%d", task.TaskID, task.State, task.IntegratedCommitSHA, seen[task.IntegratedCommitSHA])
		}
	}
}

func (h *parallelDeliveryHarness) publishReviewedParallelHead(t *testing.T, ctx context.Context) {
	t.Helper()
	reviewedHead := strings.TrimSpace(h.repository.run(t, h.repository.root, "rev-parse", "HEAD"))
	if h.reviewedHead == "" {
		h.reviewCalls++ // The completed review child is the sole deterministic external seam.
		h.reviewedHead = reviewedHead
	} else if h.reviewedHead != reviewedHead {
		t.Fatalf("publication replay head = %s, want %s", reviewedHead, h.reviewedHead)
	}
	published, err := h.publisher.Publish(ctx, h.scope, publication.PublishInput{WorktreeRef: "wt_parallel_parent", ExpectedHeadSHA: reviewedHead})
	if err != nil || published.Status != publication.PublishStatusPublished || published.HeadSHA != reviewedHead || h.reviewCalls != 1 || h.forge.pushCalls != 1 || h.forge.openPRCalls != 1 {
		t.Fatalf("review/publication = %#v, error=%v review=%d push=%d pr=%d", published, err, h.reviewCalls, h.forge.pushCalls, h.forge.openPRCalls)
	}
	verified, err := (publication.Verifier{Planner: h.planner, Git: publication.GitClient{Executable: h.repository.git, Runner: publication.ExecRunner{}}}).Verify(ctx, h.scope, publication.VerifyInput{
		WorktreeRef: "wt_parallel_parent", ExpectedHeadSHA: reviewedHead, PublisherResult: published,
	})
	if err != nil || !verified.Verified || verified.HeadSHA != reviewedHead || verified.PRURL != h.forge.prURL {
		t.Fatalf("verify reviewed publication = %#v, error=%v", verified, err)
	}
	h.published = &published
}

func (h *parallelDeliveryHarness) validateParallelDeliveryPythonEvidence(t *testing.T, initial DeliveryGraphOutput, dirty DeliveryGraphTask, cleanup DeliveryGraphOutput) {
	t.Helper()
	journal, exists, err := h.store.Load(h.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("load evidence journal: exists=%v error=%v", exists, err)
	}
	delivery := journal.Deliveries[h.deliveryID]
	commits := make(map[string]string, len(delivery.Graph.Tasks))
	integrated := make([]string, 0, len(delivery.Graph.Tasks))
	for _, task := range delivery.Graph.Tasks {
		if task.State != routing.GraphTaskIntegrated || len(task.Attempts) == 0 {
			t.Fatalf("non-integrated task in evidence: %#v", task)
		}
		attempt := task.Attempts[len(task.Attempts)-1]
		commits[task.TaskID] = strings.TrimSpace(h.repository.run(t, h.repository.root, "log", "-1", "--format=%s", attempt.CandidateCommitSHA))
		integrated = append(integrated, task.TaskID)
	}
	frontend := preparedTask(t, initial, "task_02")
	frontendTask, found := delivery.Graph.Task("task_02")
	if !found || len(frontendTask.Attempts) == 0 || frontendTask.Attempts[0].Question == nil {
		t.Fatalf("frontend question identity missing from %#v", frontendTask)
	}
	childStartIDs := make([]string, 0, len(h.childStarts))
	for _, run := range h.childStarts {
		childStartIDs = append(childStartIDs, run.ID)
	}
	slices.Sort(childStartIDs)
	evidence := map[string]any{
		"identity": map[string]any{
			"scenario_id":            "parallel-demo",
			"delivery_id":            h.deliveryID,
			"extension_version":      extensionVersionFromDescriptor(t),
			"question_operation_id":  frontendTask.Attempts[0].Question.RequestID,
			"reviewed_head":          h.reviewedHead,
			"retained_worktree_id":   dirty.WorktreeID,
			"retained_worktree_root": dirty.WorktreeRoot,
		},
		"initial_tasks": []string{"task_01", "task_02", "task_03", "task_04"}, "initial_worktrees": len(initial.Tasks),
		"child_starts":   map[string]any{"count": len(childStartIDs), "ids": childStartIDs},
		"frontend_route": map[string]any{"provider": frontend.Runtime.Provider, "model": strings.Split(frontend.Runtime.Model, "[")[0]},
		"continuation":   map[string]any{"typed": true, "same_child": true, "same_worktree": true, "sibling_progress": true, "physical_execution": 2},
		"conflict":       map[string]any{"task_id": "task_02", "accepted_task_ids": []string{"task_01"}, "retry_execution": 3},
		"integrated":     integrated, "commits": commits,
		"dependent": map[string]any{"task_id": "task_05", "admitted_after_prerequisites": true},
		"cleanup":   map[string]any{"retained": true, "blocker_code": "worktree_evidence_changed", "worktree_id": dirty.WorktreeID},
		"replay":    map[string]any{"cleanup_journal_unchanged": true, "cleanup_removes_unchanged": true},
	}
	h.validateParallelPythonEvidence(t, evidence)
}

func extensionVersionFromDescriptor(t *testing.T) string {
	t.Helper()
	extension, err := newWithServices(serviceSet{})
	if err != nil {
		t.Fatalf("create extension descriptor: %v", err)
	}
	descriptor, err := extension.Describe()
	if err != nil || descriptor.Name != "batuta" || strings.TrimSpace(descriptor.Version) == "" {
		t.Fatalf("describe extension identity = %#v error=%v", descriptor, err)
	}
	return descriptor.Version
}

func (h *parallelDeliveryHarness) validateParallelWidthPythonEvidence(t *testing.T, fifth routing.GraphTask, before, after string) {
	t.Helper()
	ids := make([]string, 0, len(h.childStarts))
	for _, run := range h.childStarts {
		ids = append(ids, run.ID)
	}
	slices.Sort(ids)
	h.validateParallelPythonEvidence(t, map[string]any{"width_probe": map[string]any{
		"eligible_task_ids":     []string{"task_01", "task_02", "task_03", "task_04", "task_05"},
		"started_child_ids":     ids,
		"started_child_count":   len(ids),
		"pending_task_id":       fifth.TaskID,
		"pending_task_attempts": len(fifth.Attempts),
		"prepare_replay_stable": before == after,
		"create_calls":          h.worktrees.createCalls,
	}})
}

func (h *parallelDeliveryHarness) validateParallelPythonEvidence(t *testing.T, evidence map[string]any) {
	t.Helper()
	payload, err := json.Marshal(evidence)
	if err != nil {
		t.Fatalf("marshal Go harness evidence: %v", err)
	}
	t.Logf("parallel-delivery-evidence=%s", payload)
	path := filepath.Join(t.TempDir(), "parallel-delivery-evidence.json")
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write Go harness evidence: %v", err)
	}
	script, err := filepath.Abs(filepath.Join("..", "..", "tests", "e2e", "assert_parallel_delivery.py"))
	if err != nil {
		t.Fatalf("absolute evidence validator: %v", err)
	}
	output, err := exec.Command("python3", script, path).CombinedOutput()
	if err != nil {
		t.Fatalf("Go-emitted evidence validator: %v\n%s", err, output)
	}
}

func newParallelDeliveryHarness(t *testing.T) *parallelDeliveryHarness {
	t.Helper()
	return newParallelDeliveryHarnessFor(t, newParallelDeliveryRepository(t, "parallel-delivery", "batuta/parallel-demo"), "ws_parallel_delivery", parallelRoutingPlanInput())
}

func newParallelDeliveryWidthHarness(t *testing.T) *parallelDeliveryHarness {
	t.Helper()
	return newParallelDeliveryHarnessFor(t, newParallelDeliveryRepository(t, "parallel-width", "batuta/parallel-width"), "ws_parallel_width", parallelWidthRoutingPlanInput())
}

func newParallelDeliveryHarnessFor(t *testing.T, repository *parallelDeliveryRepository, workspaceID string, plan RoutingPlanInput) *parallelDeliveryHarness {
	t.Helper()
	ctx := context.Background()
	git := publication.GitClient{Executable: repository.git, Runner: publication.ExecRunner{}}
	scope := publication.TrustedScope{WorkspaceID: workspaceID, WorkspaceRoot: repository.root}
	store, err := routing.NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore: %v", err)
	}
	manager := routing.MatrixManager{Store: store}
	engine := routingEngine{
		inventory: func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error) {
			return parallelDeliveryInventory(t), nil
		},
		applyMatrix: manager.Apply,
		inspectWorktree: func(_ context.Context, got publication.TrustedScope, ref string) (publication.WorktreeInspection, error) {
			return publication.WorktreeInspection{Worktree: publication.Worktree{
				ID: ref, WorkspaceID: got.WorkspaceID, Branch: "main", Path: repository.root, State: "ready", BaseRef: "main",
			}}, nil
		},
		worktreeState: git.WorktreeState,
	}
	generation, err := engine.Plan(ctx, scope, plan)
	if err != nil {
		t.Fatalf("Plan(parallel-demo): %v", err)
	}
	if cell, found := routingCellForTask(generation, "task_02"); !found || cell.Selected.ProviderID != "cursor" || cell.Selected.ModelID != integrationCursorModel {
		t.Fatalf("frontend route = %#v, found=%v; want Cursor/Grok", cell, found)
	}
	applied, err := engine.Apply(ctx, scope, RoutingApplyInput{
		Operation: RoutingOperationApplyMatrix, RoutingPlan: &plan, ExpectedGenerationDigest: generation.Digest,
		WorktreeRef: "wt_parallel_parent", OriginSessionID: "session_parallel_delivery",
	})
	if err != nil || applied.Matrix == nil {
		t.Fatalf("Apply(matrix) = %#v, error=%v", applied, err)
	}
	worktrees := newParallelGitWorktreeClient(t, repository, scope)
	runs := &fakeGraphRunReader{statuses: map[string]deliveryRunDetail{}}
	integrationGit := integration.GitClient{Executable: repository.git, Runner: publication.ExecRunner{}}
	integrator := integration.Service{
		Git:      integrationGit,
		Locker:   routingIntegrationLocker{StoreForCall: func() (*routing.OwnershipStore, error) { return store, nil }},
		Tracking: &integration.FileTrackingSynchronizer{GitExecutable: repository.git, Runner: publication.ExecRunner{}},
	}
	now := applied.Matrix.CreatedAt.Add(time.Minute)
	forge := &parallelPublicationForge{t: t, repository: repository, git: git}
	planner := publication.PublicationPlanner{Compozy: forge, Git: git}
	return &parallelDeliveryHarness{
		repository: repository, scope: scope, store: store, deliveryID: applied.Matrix.DeliveryID, worktrees: worktrees, runs: runs,
		forge: forge, planner: planner, publisher: publication.Publisher{Planner: planner, Compozy: forge, Git: git, PollInterval: time.Millisecond},
		graph: &deliveryGraphService{
			Store: store, Worktrees: worktrees, WorktreeState: git.WorktreeState, CommitReachable: git.IsAncestor,
			Runs: runs, Candidates: integrationGit, Integrator: integrator, Now: func() time.Time { return now },
		},
	}
}

func (h *parallelDeliveryHarness) recordRealCandidate(ctx context.Context, taskID string, runExecution int, childRunID string) error {
	delivery, wave, task := h.graphTask(h.worktrees.t, taskID)
	attempt, exists := graphTaskAttemptForRunExecution(task, runExecution)
	if !exists {
		return fmt.Errorf("invalid run execution %d for %s", runExecution, taskID)
	}
	commit := strings.TrimSpace(h.repository.run(h.worktrees.t, attempt.WorktreeRoot, "rev-parse", "HEAD"))
	verification := json.RawMessage(fmt.Sprintf(`{"checks":["fixture-check"],"status":"passed","task_id":%q}`, taskID))
	verificationDigest := digestValue(string(verification))
	run := deliveryRun{
		ID: childRunID, WorkspaceID: h.scope.WorkspaceID, LoopName: "batuta-task", Status: "done",
		CreatedAt: time.Now().UTC(), StartedAt: time.Now().UTC(), TokensUsed: 100, TokensUsedPresent: true,
		Inputs: graphTaskRunInputs(delivery, wave, task, runExecution),
	}
	h.runs.statuses[childRunID] = deliveryRunDetail{Run: run, Generations: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{{
		NodeID: "implementation", ItemIndex: 0, Status: "succeeded",
		OutputRef: completedTaskOutputRef(h.worktrees.t, taskID, attempt.Execution, commit, verification, verificationDigest),
	}}}}}
	input := DeliveryGraphInput{
		Operation: GraphOpRecordCandidate, DeliveryID: h.deliveryID, Wave: wave.Number, TaskID: taskID,
		Execution: runExecution, ChildRunID: childRunID,
	}
	output, err := h.graph.Execute(ctx, h.scope, input)
	if err != nil {
		return err
	}
	if output.Disposition != GraphDispositionCandidateRecorded || output.BaseSHA != attempt.BaseHeadSHA {
		return fmt.Errorf("candidate output for %s = %#v", taskID, output)
	}
	snapshot := h.snapshot(h.worktrees.t)
	replay, replayErr := h.graph.Execute(ctx, h.scope, input)
	if replayErr != nil || replay.Disposition != GraphDispositionCandidateRecorded || h.snapshot(h.worktrees.t) != snapshot {
		return fmt.Errorf("candidate replay for %s = %#v error=%v", taskID, replay, replayErr)
	}
	return nil
}

func (h *parallelDeliveryHarness) recordRealFailure(ctx context.Context, taskID string, runExecution int, childRunID string) error {
	delivery, wave, task := h.graphTask(h.worktrees.t, taskID)
	attempt, exists := graphTaskAttemptForRunExecution(task, runExecution)
	if !exists {
		return fmt.Errorf("invalid run execution %d for %s", runExecution, taskID)
	}
	h.runs.statuses[childRunID] = deliveryRunDetail{Run: deliveryRun{
		ID: childRunID, WorkspaceID: h.scope.WorkspaceID, LoopName: "batuta-task", Status: "failed",
		CreatedAt: time.Now().UTC(), StartedAt: time.Now().UTC(), TokensUsed: 100, TokensUsedPresent: true,
		Inputs: graphTaskRunInputs(delivery, wave, task, runExecution),
	}}
	output, err := h.graph.Execute(ctx, h.scope, DeliveryGraphInput{
		Operation: GraphOpRecordFailure, DeliveryID: h.deliveryID, Wave: wave.Number, TaskID: taskID,
		Execution: runExecution, ChildRunID: childRunID, BlockerCode: "deterministic_fixture_failure",
	})
	if err != nil {
		return err
	}
	if output.Disposition != GraphDispositionPreparing || output.Execution != attempt.Execution+1 {
		return fmt.Errorf("failure output for %s = %#v", taskID, output)
	}
	return nil
}

func parallelRoutingPlanInput() RoutingPlanInput {
	proposal := func(taskID string, domain routing.Domain, complexity routing.Complexity, dependencies ...string) routing.ClassificationProposal {
		return routing.ClassificationProposal{TaskID: taskID, Domain: domain, Complexity: complexity, Confidence: 0.99, Dependencies: dependencies}
	}
	fit := func(taskID string, domain routing.Domain, complexity routing.Complexity, candidates ...routing.FitCandidate) RoutingFitProposal {
		return RoutingFitProposal{TaskIDs: []string{taskID}, Domain: domain, Complexity: complexity, Candidates: candidates}
	}
	codex := routing.FitCandidate{ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "gpt-5.6-luna", Score: 0.99}
	codexHigh := routing.FitCandidate{ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "gpt-5.6-sol", Score: 0.99}
	frontendFallback := routing.FitCandidate{ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "gpt-5.6-terra", Score: 0.90}
	return RoutingPlanInput{
		Slug: "parallel-demo",
		Proposals: []routing.ClassificationProposal{
			proposal("task_01", routing.DomainBackend, routing.ComplexityLow),
			proposal("task_02", routing.DomainFrontend, routing.ComplexityMedium),
			proposal("task_03", routing.DomainTesting, routing.ComplexityLow),
			proposal("task_04", routing.DomainDocs, routing.ComplexityLow),
			{TaskID: "task_05", Domain: routing.DomainFullstack, Complexity: routing.ComplexityHigh, Confidence: 0.99,
				Dependencies:      []string{"task_01", "task_02", "task_03", "task_04"},
				IndivisibleReason: "The final fixture task changes the integrated backend, frontend, tests, and docs as one atomic interface.",
				Evidence:          []routing.EvidenceReference{{Kind: routing.EvidenceAcceptanceCriterion, Reference: "all four prerequisite interfaces must remain compatible together"}}},
		},
		Fit: []RoutingFitProposal{
			fit("task_01", routing.DomainBackend, routing.ComplexityLow, codex, routing.FitCandidate{ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "gpt-5.6-terra", Score: 0.90}),
			fit("task_02", routing.DomainFrontend, routing.ComplexityMedium,
				routing.FitCandidate{ExecutorID: inventory.ExecutorCursorAgent, ProviderID: "cursor", ModelID: integrationCursorModel, Score: 0.99}, frontendFallback),
			fit("task_03", routing.DomainTesting, routing.ComplexityLow, codex),
			fit("task_04", routing.DomainDocs, routing.ComplexityLow, codex),
			fit("task_05", routing.DomainFullstack, routing.ComplexityHigh, codexHigh),
		},
	}
}

func parallelWidthRoutingPlanInput() RoutingPlanInput {
	plan := parallelRoutingPlanInput()
	plan.Slug = "parallel-width"
	plan.Proposals[4].Dependencies = nil
	return plan
}

func parallelDeliveryInventory(t *testing.T) inventory.InventorySnapshot {
	t.Helper()
	base := migrationFreeInventory(t)
	executors := append([]inventory.ExecutorSnapshot(nil), base.Executors...)
	for index := range executors {
		for capabilityIndex := range executors[index].Capabilities {
			capability := &executors[index].Capabilities[capabilityIndex]
			if capability.Name != "models" {
				continue
			}
			switch executors[index].ID {
			case inventory.ExecutorCompozy, inventory.ExecutorCodex:
				capability.Identifiers = append(capability.Identifiers, "codex/gpt-5.6-sol")
			}
		}
	}
	snapshot, err := inventory.NewSnapshot(base.CompozyCatalogGeneration, executors)
	if err != nil {
		t.Fatalf("NewSnapshot(parallel inventory): %v", err)
	}
	return snapshot
}

func routingCellForTask(generation routing.RoutingGeneration, taskID string) (routing.RoutingCell, bool) {
	for _, cell := range generation.Cells {
		for _, current := range cell.TaskIDs {
			if current == taskID {
				return cell, true
			}
		}
	}
	return routing.RoutingCell{}, false
}

type parallelDeliveryRepository struct {
	root      string
	worktrees string
	remote    string
	git       string
	branch    string
}

func newParallelDeliveryRepository(t *testing.T, fixtureName, branch string) *parallelDeliveryRepository {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git executable is unavailable")
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatalf("absolute git path: %v", err)
	}
	fixtureRoot, err := filepath.Abs(filepath.Join("..", "..", "tests", "fixtures", fixtureName))
	if err != nil {
		t.Fatalf("fixture root: %v", err)
	}
	repository := &parallelDeliveryRepository{
		root: filepath.Join(t.TempDir(), "workspace"), worktrees: t.TempDir(), remote: filepath.Join(t.TempDir(), "remote.git"),
		git: filepath.Clean(git), branch: branch,
	}
	if err := os.MkdirAll(repository.root, 0o755); err != nil {
		t.Fatalf("mkdir repository root: %v", err)
	}
	if err := os.CopyFS(repository.root, os.DirFS(fixtureRoot)); err != nil {
		t.Fatalf("copy fixture project: %v", err)
	}
	repository.run(t, "", "init", "--bare", repository.remote)
	repository.run(t, "", "init", "-b", "main", repository.root)
	repository.run(t, repository.root, "config", "user.email", "batuta@example.invalid")
	repository.run(t, repository.root, "config", "user.name", "Batuta Parallel Integration")
	repository.run(t, repository.root, "add", "--all")
	repository.run(t, repository.root, "commit", "-m", "chore: initialize parallel delivery fixture")
	repository.run(t, repository.root, "remote", "add", "origin", repository.remote)
	repository.run(t, repository.root, "push", "-u", "origin", "main")
	repository.run(t, repository.root, "switch", "-c", repository.branch)
	return repository
}

func (r *parallelDeliveryRepository) run(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command(r.git, args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %q: %v\n%s", args, directory, err, output)
	}
	return string(output)
}

func (r *parallelDeliveryRepository) writeAndCommit(t *testing.T, root, relative, content, message string) {
	t.Helper()
	path := filepath.Join(root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", relative, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
	r.run(t, root, "add", "--all")
	r.run(t, root, "commit", "-m", message)
}

type parallelGitWorktreeClient struct {
	t           *testing.T
	repository  *parallelDeliveryRepository
	scope       publication.TrustedScope
	byName      map[string]worktreeops.Worktree
	byID        map[string]worktreeops.Worktree
	createCalls int
	removeCalls int
}

func newParallelGitWorktreeClient(t *testing.T, repository *parallelDeliveryRepository, scope publication.TrustedScope) *parallelGitWorktreeClient {
	return &parallelGitWorktreeClient{t: t, repository: repository, scope: scope, byName: map[string]worktreeops.Worktree{}, byID: map[string]worktreeops.Worktree{}}
}

func (c *parallelGitWorktreeClient) Create(_ context.Context, scope publication.TrustedScope, request worktreeops.CreateRequest) (worktreeops.Worktree, error) {
	if scope != c.scope {
		return worktreeops.Worktree{}, worktreeops.ErrInvalidWorktreeIdentity
	}
	if existing, found := c.byName[request.Name]; found {
		return existing, nil
	}
	root := filepath.Join(c.repository.worktrees, request.Name)
	c.repository.run(c.t, c.repository.root, "worktree", "add", "-b", request.Branch, root, request.BaseSHA)
	c.createCalls++
	id := fmt.Sprintf("wt_parallel_%02d", c.createCalls)
	worktree := worktreeops.Worktree{
		ID: id, Name: request.Name, Root: root, WorkspaceID: scope.WorkspaceID, RepositoryRoot: scope.WorkspaceRoot,
		RepositoryIdentity: "parallel-git", Branch: request.Branch, BaseRef: request.BaseSHA, BaseSHA: request.BaseSHA,
		State: "ready", Setup: worktreeops.SetupResult{State: "ok"},
	}
	c.byName[worktree.Name], c.byID[worktree.ID] = worktree, worktree
	return worktree, nil
}

func (c *parallelGitWorktreeClient) FindByName(_ context.Context, scope publication.TrustedScope, name string) (worktreeops.Worktree, bool, error) {
	if scope != c.scope {
		return worktreeops.Worktree{}, false, worktreeops.ErrInvalidWorktreeIdentity
	}
	worktree, found := c.byName[name]
	return worktree, found, nil
}

func (c *parallelGitWorktreeClient) Inspect(_ context.Context, scope publication.TrustedScope, id string) (worktreeops.Worktree, error) {
	if scope != c.scope {
		return worktreeops.Worktree{}, worktreeops.ErrInvalidWorktreeIdentity
	}
	worktree, found := c.byID[id]
	if !found {
		return worktreeops.Worktree{}, worktreeops.ErrInvalidWorktreeIdentity
	}
	return worktree, nil
}

func (c *parallelGitWorktreeClient) Remove(_ context.Context, scope publication.TrustedScope, id string) (worktreeops.Worktree, error) {
	worktree, err := c.Inspect(context.Background(), scope, id)
	if err != nil {
		return worktreeops.Worktree{}, err
	}
	if worktree.State == "removed" {
		return worktree, nil
	}
	c.repository.run(c.t, c.repository.root, "worktree", "remove", "--force", worktree.Root)
	c.removeCalls++
	worktree.State = "removed"
	c.byID[id] = worktree
	c.byName[worktree.Name] = worktree
	return worktree, nil
}

var _ worktreeops.Client = (*parallelGitWorktreeClient)(nil)

type parallelPublicationForge struct {
	t           *testing.T
	repository  *parallelDeliveryRepository
	git         publication.GitClient
	pushCalls   int
	openPRCalls int
	prURL       string
}

func (c *parallelPublicationForge) Inspect(ctx context.Context, scope publication.TrustedScope, ref string) (publication.WorktreeInspection, error) {
	snapshot, err := c.git.Snapshot(ctx, c.repository.root)
	if err != nil {
		return publication.WorktreeInspection{}, err
	}
	dirty, ahead, behind := 0, 0, 0
	if !snapshot.Clean {
		dirty = 1
	}
	hasUpstream := c.pushCalls > 0
	aheadOfBase, err := c.git.CommitsAheadOfBase(ctx, c.repository.root, "main")
	if err != nil {
		return publication.WorktreeInspection{}, err
	}
	status := &publication.WorktreeStatus{Branch: &snapshot.Branch, Detached: &snapshot.Detached, HeadSHA: &snapshot.HeadSHA, DirtyFiles: &dirty, HasUpstream: &hasUpstream, Ahead: &ahead, Behind: &behind}
	if !hasUpstream {
		status.AheadOfBase = &aheadOfBase
	}
	return publication.WorktreeInspection{
		Worktree: publication.Worktree{ID: ref, WorkspaceID: scope.WorkspaceID, Branch: snapshot.Branch, Path: c.repository.root, State: "ready", BaseRef: "main"},
		Status:   status, Forge: &publication.ForgeStatus{Provider: "github"}, Repo: publication.WorktreeRepo{GitBacked: true, GitAvailable: true},
	}, nil
}

func (c *parallelPublicationForge) ExitPlan(context.Context, publication.TrustedScope, string) (publication.ExitPlan, error) {
	plan := publication.ExitPlan{WorktreeID: "wt_parallel_parent", Base: "main", Forge: &publication.ForgeCapabilities{Provider: "github", DefaultBranch: "main"}, PRPrefill: &publication.PRPrefill{Title: "Parallel delivery", Body: "Deterministic Batuta integration proof"}}
	switch {
	case c.prURL != "":
		plan.Actions = []publication.ExitAction{{Action: "view_pr", Enabled: true, URL: c.prURL}}
		plan.ForgeStatus = &publication.ForgeStatus{Provider: "github", PRURL: c.prURL}
	case c.pushCalls > 0:
		plan.Actions = []publication.ExitAction{{Action: "open_pr", Enabled: true, Publish: true}}
	default:
		plan.Actions = []publication.ExitAction{{Action: "push", Enabled: true, Publish: true}}
	}
	return plan, nil
}

func (c *parallelPublicationForge) Push(_ context.Context, _ publication.TrustedScope, _ string) (publication.Operation, error) {
	c.repository.run(c.t, c.repository.root, "push", "-u", "origin", "HEAD")
	c.pushCalls++
	return publication.Operation{OperationID: "op_parallel_push"}, nil
}

func (c *parallelPublicationForge) OpenPR(context.Context, publication.TrustedScope, string, publication.PRPrefill, string) (publication.Operation, error) {
	c.openPRCalls++
	c.prURL = "https://github.com/example/parallel-delivery/pull/8"
	return publication.Operation{OperationID: "op_parallel_pr"}, nil
}
