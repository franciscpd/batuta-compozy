package extensionapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/batuta-ai/core/integration"
	"github.com/batuta-ai/core/publication"
	"github.com/batuta-ai/core/routing"
	"github.com/franciscpd/batuta-compozy/internal/worktreeops"
)

func TestDeliveryGraphServicePreparesReadyWaveAndReconcilesWithoutDuplicateCreate(t *testing.T) {
	t.Parallel()

	for _, noSetupCommand := range []bool{false, true} {
		fixture := newDeliveryServiceFixture(t)
		worktrees := &fakeGraphWorktreeClient{scope: fixture.scope, state: "ready", noSetupCommand: noSetupCommand}
		service := deliveryGraphService{
			Store: fixture.store, Worktrees: worktrees, Now: func() time.Time { return fixture.now },
			CommitReachable: func(context.Context, string, string, string) (bool, error) { return true, nil },
			WorktreeState: func(context.Context, string) (publication.WorktreeState, error) {
				return publication.WorktreeState{
					HeadSHA:         "0123456789abcdef0123456789abcdef01234567",
					PorcelainSHA256: emptyDigest(), ContentSHA256: emptyDigest(),
				}, nil
			},
		}
		input := DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: fixture.deliveryID}
		first, err := service.Execute(context.Background(), fixture.scope, input)
		if err != nil || first.Disposition != GraphDispositionWaveReady || first.Wave != 1 || len(first.Tasks) == 0 {
			t.Fatalf("Execute(first) = %#v, error %v", first, err)
		}
		created := worktrees.createCalls
		second, err := service.Execute(context.Background(), fixture.scope, input)
		if err != nil || !reflect.DeepEqual(second, first) || worktrees.createCalls != created || worktrees.inspectCalls != len(first.Tasks) {
			t.Fatalf("Execute(replay) = %#v, error %v; creates=%d inspects=%d", second, err, worktrees.createCalls, worktrees.inspectCalls)
		}
		journal, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
		if err != nil || !exists {
			t.Fatalf("Load() exists=%v error=%v", exists, err)
		}
		graph := journal.Deliveries[fixture.deliveryID].Graph
		for _, descriptor := range first.Tasks {
			task, exists := graph.Task(descriptor.TaskID)
			if !exists || task.State != routing.GraphTaskRunning || len(task.Attempts) != descriptor.Execution ||
				task.Attempts[descriptor.Execution-1].WorktreeID != descriptor.WorktreeID {
				t.Fatalf("durable task %s = %#v, exists=%v", descriptor.TaskID, task, exists)
			}
		}
	}
}

func TestDeliveryGraphServiceReturnsStructuredExhaustionBeforePreparingWave(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	service := deliveryGraphService{
		Store: fixture.store, Worktrees: &fakeGraphWorktreeClient{scope: fixture.scope},
		Now: func() time.Time { return delivery.AbsoluteDeadline },
		WorktreeState: func(context.Context, string) (publication.WorktreeState, error) {
			return publication.WorktreeState{
				HeadSHA:         delivery.InitialWorktreeFingerprint.HeadSHA,
				PorcelainSHA256: emptyDigest(), ContentSHA256: emptyDigest(),
			}, nil
		},
	}

	output, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpPrepareWave, DeliveryID: fixture.deliveryID,
	})
	if err != nil || output.Disposition != GraphDispositionExhausted || output.BlockerCode != "delivery_budget_exhausted" {
		t.Fatalf("prepare exhausted = %#v, error=%v", output, err)
	}
}

func TestDeliveryWaveSlotsAreBoundedByAvailableTokens(t *testing.T) {
	t.Parallel()

	for _, test := range []struct {
		available int64
		want      int
	}{{available: 0, want: 0}, {available: 1, want: 1}, {available: 3, want: 3}, {available: 4, want: 4}, {available: 100, want: 4}} {
		if got := deliveryWaveSlots(test.available); got != test.want {
			t.Fatalf("deliveryWaveSlots(%d) = %d, want %d", test.available, got, test.want)
		}
	}
}

func TestDeliveryGraphServiceRecoversCreateAfterCrashFromPersistedIntent(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	crash := errors.New("simulated process crash after create")
	worktrees := &fakeGraphWorktreeClient{scope: fixture.scope, state: "ready", createErrAfterPersist: crash}
	service := deliveryGraphService{
		Store: fixture.store, Worktrees: worktrees, Now: func() time.Time { return fixture.now },
		WorktreeState: func(context.Context, string) (publication.WorktreeState, error) {
			return publication.WorktreeState{
				HeadSHA:         "0123456789abcdef0123456789abcdef01234567",
				PorcelainSHA256: emptyDigest(), ContentSHA256: emptyDigest(),
			}, nil
		},
	}
	input := DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: fixture.deliveryID}

	if _, err := service.Execute(context.Background(), fixture.scope, input); !errors.Is(err, crash) {
		t.Fatalf("Execute(crash) error = %v, want simulated crash", err)
	}
	journal, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("Load(crash) exists=%v error=%v", exists, err)
	}
	task, _ := journal.Deliveries[fixture.deliveryID].Graph.Task("task_01")
	if len(task.Attempts) != 1 || task.Attempts[0].WorktreeIntent == nil || task.Attempts[0].WorktreeID != "" {
		t.Fatalf("persisted create intent = %#v", task.Attempts)
	}

	worktrees.createErrAfterPersist = nil
	output, err := service.Execute(context.Background(), fixture.scope, input)
	if err != nil || output.Disposition != GraphDispositionWaveReady || worktrees.createCalls != 1 || worktrees.findCalls != 2 {
		t.Fatalf("Execute(reconcile) = %#v, error=%v creates=%d finds=%d", output, err, worktrees.createCalls, worktrees.findCalls)
	}
}

func TestDeliveryGraphServicePrepareWaveSkipsCandidateAlreadyReadyForSettlement(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		if _, err := delivery.Graph.RecordCandidate("task_01", 1, routing.TaskCandidate{
			ChildRunID: "run_task_01", BaseHeadSHA: wave.BaseHeadSHA,
			CommitSHA:          "1111111111111111111111111111111111111111",
			VerificationDigest: digestValue("candidate-ready"), TokensUsed: 10,
		}); err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist candidate: %v", err)
	}
	worktrees := &fakeGraphWorktreeClient{scope: fixture.scope, state: "ready"}
	service := deliveryGraphService{
		Store: fixture.store, Worktrees: worktrees, Now: func() time.Time { return fixture.now },
		WorktreeState: func(context.Context, string) (publication.WorktreeState, error) {
			return publication.WorktreeState{
				HeadSHA: wave.BaseHeadSHA, PorcelainSHA256: emptyDigest(), ContentSHA256: emptyDigest(),
			}, nil
		},
	}

	output, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpPrepareWave, DeliveryID: fixture.deliveryID,
	})
	if err != nil || output.Disposition != GraphDispositionWaveReady || output.Wave != wave.Number ||
		len(output.Tasks) != 0 || worktrees.findCalls != 0 || worktrees.inspectCalls != 0 || worktrees.createCalls != 0 {
		t.Fatalf("prepare settled candidate = %#v, error=%v worktree calls=%d/%d/%d", output, err, worktrees.findCalls, worktrees.inspectCalls, worktrees.createCalls)
	}
}

func TestActiveDeliveryWaveReturnsOlderCandidateAfterConflictRetrySettles(t *testing.T) {
	t.Parallel()

	older := routing.DeliveryWave{Number: 1, BaseHeadSHA: "1111111111111111111111111111111111111111", TaskIDs: []string{"task_01", "task_02"}}
	retry := routing.DeliveryWave{Number: 2, BaseHeadSHA: "2222222222222222222222222222222222222222", TaskIDs: []string{"task_01"}}
	graph := &routing.DeliveryGraph{
		Tasks: []routing.GraphTask{
			{TaskID: "task_01", State: routing.GraphTaskIntegrated},
			{TaskID: "task_02", State: routing.GraphTaskCandidate},
		},
		Waves: []routing.DeliveryWave{older, retry},
	}

	got, exists := activeDeliveryWave(graph)
	if !exists || !reflect.DeepEqual(got, older) {
		t.Fatalf("activeDeliveryWave() = %#v, exists=%v, want older candidate wave", got, exists)
	}
}

func TestDeliveryGraphServiceReturnsExactTaskContextWithoutMutation(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	before, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("Load(before) exists=%v error=%v", exists, err)
	}
	service := deliveryGraphService{Store: fixture.store, Now: func() time.Time { return fixture.now }}

	output, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpTaskContext, DeliveryID: fixture.deliveryID,
		Wave: wave.Number, TaskID: "task_01", Execution: 1,
	})
	if err != nil {
		t.Fatalf("Execute(task_context) error = %v", err)
	}
	task, _ := before.Deliveries[fixture.deliveryID].Graph.Task("task_01")
	wantWall, _ := before.Deliveries[fixture.deliveryID].RemainingActiveWall(fixture.now)
	if output.Operation != GraphOpTaskContext || output.Disposition != GraphDispositionTaskReady ||
		output.TaskID != "task_01" || output.Execution != 1 ||
		output.TaskFile != ".compozy/tasks/demo/task_01.md" || output.Runtime == nil || *output.Runtime != task.Attempts[0].Runtime ||
		output.WorktreeID != "wt_task_01" || output.WorktreeRoot != taskRoot ||
		output.BaseSHA != wave.BaseHeadSHA || output.RemainingTaskExecutions != 3 ||
		output.RemainingTokens != routing.DeliveryTokenCeiling || output.RemainingWallSeconds != int(wantWall/time.Second) ||
		output.Answers == nil || len(output.Answers) != 0 {
		t.Fatalf("task context = %#v", output)
	}
	after, _, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("task_context mutated journal: error=%v before=%#v after=%#v", err, before, after)
	}
	exhausted, err := (&deliveryGraphService{
		Store: fixture.store, Now: func() time.Time { return before.Deliveries[fixture.deliveryID].AbsoluteDeadline },
	}).Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpTaskContext, DeliveryID: fixture.deliveryID,
		Wave: wave.Number, TaskID: "task_01", Execution: 1,
	})
	if err != nil || exhausted.Disposition != GraphDispositionExhausted ||
		exhausted.BlockerCode != "delivery_budget_exhausted" || exhausted.RemainingWallSeconds != 0 {
		t.Fatalf("task context exhausted = %#v, error=%v", exhausted, err)
	}
}

func TestDeliveryGraphServiceRecordsOneAuthoritativeQuestionAndAnswer(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	task, _ := delivery.Graph.Task("task_01")
	run := deliveryRun{
		ID: "run_task_01", WorkspaceID: fixture.scope.WorkspaceID, LoopName: "batuta-task",
		Status: "running", CreatedAt: fixture.now, StartedAt: fixture.now,
		Inputs: graphTaskRunInputs(delivery, wave, task, 1),
	}
	runs := &fakeGraphRunReader{recent: []deliveryRun{run}, statuses: map[string]deliveryRunDetail{}}
	service := deliveryGraphService{Store: fixture.store, Runs: runs, Now: func() time.Time { return fixture.now }}
	questionInput := DeliveryGraphInput{
		Operation: GraphOpRecordQuestion, DeliveryID: fixture.deliveryID, Wave: wave.Number,
		TaskID: "task_01", Execution: 1, Prompt: "Which compatibility behavior should we ship?",
		Choices: []string{"Preserve compatibility", "Adopt the new contract"},
	}

	question, err := service.Execute(context.Background(), fixture.scope, questionInput)
	if err != nil || question.Disposition != GraphDispositionWaitingInput ||
		!routingDigestPattern.MatchString(question.QuestionOperationID) || runs.recentCalls != 1 {
		t.Fatalf("record_question = %#v, error=%v recent_calls=%d", question, err, runs.recentCalls)
	}
	replay, err := service.Execute(context.Background(), fixture.scope, questionInput)
	if err != nil || !reflect.DeepEqual(replay, question) || runs.recentCalls != 1 {
		t.Fatalf("record_question replay = %#v, error=%v recent_calls=%d", replay, err, runs.recentCalls)
	}
	journal, _, _ = fixture.store.Load(fixture.scope.WorkspaceID)
	storedTask, _ := journal.Deliveries[fixture.deliveryID].Graph.Task("task_01")
	if storedTask.State != routing.GraphTaskWaitingInput || storedTask.Attempts[0].ChildRunID != run.ID ||
		storedTask.Attempts[0].Question == nil || storedTask.Attempts[0].Question.RequestID != question.QuestionOperationID ||
		storedTask.Attempts[0].Question.ContextDigest != "sha256:3fc04a933b93fe0df804c847180e8914e712e3cacc96df2b709fb8b90af02f86" ||
		len(journal.Deliveries[fixture.deliveryID].Graph.Pauses) != 1 ||
		journal.Deliveries[fixture.deliveryID].Graph.Pauses[0].EndedAt != nil {
		t.Fatalf("stored question task = %#v pauses=%#v", storedTask, journal.Deliveries[fixture.deliveryID].Graph.Pauses)
	}

	runs.statuses[run.ID] = deliveryRunDetail{
		Run: run,
		Generations: []deliveryGeneration{{Generation: 2, Outputs: []deliveryOutput{{
			NodeID: "ask_operator", ItemIndex: 0, Status: "succeeded", OutputRef: answeredAskOutputRef("Preserve compatibility"),
		}}}},
		Requests: []deliveryRequest{{
			LoopRunID: run.ID, LoopName: "batuta-task", Generation: 2, NodeID: "ask_operator",
			ItemIndex: 0, Kind: "ask", State: "answered", Prompt: questionInput.Prompt,
			Context: json.RawMessage(`{"task_id":"task_01"}`), Expect: taskAnswerExpectation(),
			Decisions: []string{"respond"}, Agents: "deny", AnsweredDecision: "respond",
			ActorKind: "human", ActorID: "operator-1", AnsweredAt: timePointer(fixture.now), ResolvedAt: timePointer(fixture.now),
		}},
	}
	answerInput := DeliveryGraphInput{
		Operation: GraphOpRecordAnswer, DeliveryID: fixture.deliveryID, Wave: wave.Number,
		TaskID: "task_01", Execution: 1, QuestionOperationID: question.QuestionOperationID,
		Answer: "Preserve compatibility",
	}
	answer, err := service.Execute(context.Background(), fixture.scope, answerInput)
	if err != nil || answer.Disposition != GraphDispositionTaskReady || answer.Execution != 2 || runs.statusCalls != 1 {
		t.Fatalf("record_answer = %#v, error=%v status_calls=%d", answer, err, runs.statusCalls)
	}
	answerReplay, err := service.Execute(context.Background(), fixture.scope, answerInput)
	if err != nil || answerReplay.Disposition != answer.Disposition || answerReplay.Execution != answer.Execution ||
		!answerReplay.Replayed || runs.statusCalls != 1 {
		t.Fatalf("record_answer replay = %#v, error=%v status_calls=%d", answerReplay, err, runs.statusCalls)
	}
	journal, _, _ = fixture.store.Load(fixture.scope.WorkspaceID)
	storedTask, _ = journal.Deliveries[fixture.deliveryID].Graph.Task("task_01")
	if storedTask.Attempts[0].Question == nil || storedTask.Attempts[0].Question.Answer == nil ||
		storedTask.Attempts[0].Question.Answer.LoopRunID != run.ID ||
		storedTask.Attempts[0].Question.Answer.Generation != 2 ||
		storedTask.Attempts[0].Question.Answer.NodeID != "ask_operator" ||
		storedTask.Attempts[0].Question.Answer.ItemIndex != 0 {
		t.Fatalf("record_answer did not persist daemon-derived identity: %#v", storedTask.Attempts[0].Question)
	}
	questionReplay, err := service.Execute(context.Background(), fixture.scope, questionInput)
	if err != nil || questionReplay.Disposition != GraphDispositionTaskReady || questionReplay.Execution != 2 ||
		questionReplay.QuestionOperationID != question.QuestionOperationID || !questionReplay.Replayed || runs.recentCalls != 1 {
		t.Fatalf("record_question after answer = %#v, error=%v recent_calls=%d", questionReplay, err, runs.recentCalls)
	}

	contextOutput, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpTaskContext, DeliveryID: fixture.deliveryID,
		Wave: wave.Number, TaskID: "task_01", Execution: 1,
	})
	if err != nil || contextOutput.Execution != 2 || len(contextOutput.Answers) != 1 || contextOutput.Answers[0].Value != "Preserve compatibility" ||
		contextOutput.Answers[0].QuestionOperationID != question.QuestionOperationID {
		t.Fatalf("continued task context = %#v, error=%v", contextOutput, err)
	}
	identity, err := worktreeops.DeriveIdentity(worktreeops.IdentityInput{
		WorkspaceID: fixture.scope.WorkspaceID, DeliveryID: fixture.deliveryID, Wave: wave.Number,
		Slug: delivery.Slug, TaskID: "task_01", Execution: 1, BaseSHA: wave.BaseHeadSHA,
	})
	if err != nil {
		t.Fatalf("DeriveIdentity() error = %v", err)
	}
	service.Worktrees = &fakeGraphWorktreeClient{scope: fixture.scope, state: "ready", byID: map[string]worktreeops.Worktree{
		"wt_task_01": {
			ID: "wt_task_01", Name: identity.Name, Root: taskRoot, WorkspaceID: fixture.scope.WorkspaceID,
			RepositoryRoot: fixture.scope.WorkspaceRoot, RepositoryIdentity: digestValue("repository"),
			Branch: identity.Branch, BaseRef: wave.BaseHeadSHA, BaseSHA: wave.BaseHeadSHA,
			State: "ready", Setup: worktreeops.SetupResult{State: "ok"},
		},
	}}
	service.WorktreeState = func(context.Context, string) (publication.WorktreeState, error) {
		return publication.WorktreeState{HeadSHA: wave.BaseHeadSHA, PorcelainSHA256: emptyDigest(), ContentSHA256: emptyDigest()}, nil
	}
	prepared, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpPrepareWave, DeliveryID: fixture.deliveryID,
	})
	if err != nil || prepared.Disposition != GraphDispositionWaveReady || len(prepared.Tasks) != 1 ||
		prepared.Tasks[0].Execution != 2 || prepared.Tasks[0].WorktreeID != "wt_task_01" {
		t.Fatalf("continued prepare_wave = %#v, error=%v", prepared, err)
	}

	verification := json.RawMessage(`{"checks":["go test ./..."],"status":"passed","task_id":"task_01"}`)
	verificationDigest := digestValue(string(verification))
	commitSHA := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	run.Status = "done"
	run.TokensUsed = 700
	run.TokensUsedPresent = true
	runs.statuses[run.ID] = deliveryRunDetail{
		Run: run, Generations: []deliveryGeneration{{Generation: 3, Outputs: []deliveryOutput{{
			NodeID: "implementation", Status: "succeeded",
			// The recorded verification is not canonical and the child digest is
			// bogus: Batuta must canonicalize and derive the digest itself.
			OutputRef: completedTaskOutputRef(t, "task_01", 2, commitSHA, json.RawMessage(`{"task_id": "task_01", "status": "passed", "checks": ["go test ./..."]}`), digestValue("bogus")),
		}}}},
	}
	validator := &fakeGraphCandidateValidator{evidence: integration.CandidateEvidence{
		RepositoryIdentity: digestValue("repository"), CommitSHA: commitSHA,
		TreeSHA: "1234512345123451234512345123451234512345", OwnedTrackingPaths: []string{},
		Tracking: []integration.TrackingFile{},
	}}
	service.Candidates = validator
	runs.recent = []deliveryRun{run}
	implicit, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpRecordCandidate, DeliveryID: fixture.deliveryID, Wave: wave.Number, TaskID: "task_01", Execution: 1,
	})
	if err != nil || implicit.Disposition != GraphDispositionCandidateRecorded || validator.calls != 1 {
		t.Fatalf("record_candidate without child_run_id = %#v, error=%v validator_calls=%d", implicit, err, validator.calls)
	}
	candidate, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpRecordCandidate, DeliveryID: fixture.deliveryID, Wave: wave.Number,
		TaskID: "task_01", Execution: 1, ChildRunID: run.ID, BaseSHA: wave.BaseHeadSHA,
		CommitSHA: commitSHA, Verification: verification, VerificationDigest: verificationDigest,
	})
	if err != nil || candidate.Disposition != GraphDispositionCandidateRecorded {
		t.Fatalf("continued record_candidate = %#v, error=%v", candidate, err)
	}
	if validator.last.ExpectedBranch != identity.Branch {
		t.Fatalf("continued candidate branch = %q, want original worktree branch %q", validator.last.ExpectedBranch, identity.Branch)
	}
}

func TestDeliveryGraphServiceRejectsNonUniqueOrMismatchedAnsweredRequests(t *testing.T) {
	type mutation struct {
		requests   func([]deliveryRequest) []deliveryRequest
		outputs    func([]deliveryOutput) []deliveryOutput
		generation int64
	}
	for name, mutate := range map[string]mutation{
		"zero requests":     {requests: func([]deliveryRequest) []deliveryRequest { return nil }},
		"multiple requests": {requests: func(requests []deliveryRequest) []deliveryRequest { return append(requests, requests[0]) }},
		"mismatched prompt": {requests: func(requests []deliveryRequest) []deliveryRequest {
			requests[0].Prompt = "A different question"
			return requests
		}},
		"mismatched human responder": {requests: func(requests []deliveryRequest) []deliveryRequest {
			requests[0].ActorKind = "agent"
			return requests
		}},
		"zero output cells":     {outputs: func([]deliveryOutput) []deliveryOutput { return nil }},
		"multiple output cells": {outputs: func(outputs []deliveryOutput) []deliveryOutput { return append(outputs, outputs[0]) }},
		"wrong generation cell": {generation: 3},
		"wrong item cell": {outputs: func(outputs []deliveryOutput) []deliveryOutput {
			outputs[0].ItemIndex = 1
			return outputs
		}},
		"wrong node cell": {outputs: func(outputs []deliveryOutput) []deliveryOutput {
			outputs[0].NodeID = "other_node"
			return outputs
		}},
		"non-succeeded cell": {outputs: func(outputs []deliveryOutput) []deliveryOutput {
			outputs[0].Status = "failed"
			return outputs
		}},
		"invalid output ref": {outputs: func(outputs []deliveryOutput) []deliveryOutput {
			outputs[0].OutputRef = "not-a-sha256-ref"
			return outputs
		}},
	} {
		t.Run(name, func(t *testing.T) {
			fixture := newDeliveryServiceFixture(t)
			taskRoot := t.TempDir()
			writeRoutingTask(t, taskRoot)
			wave := prepareGraphTaskForTest(t, fixture, taskRoot)
			journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
			delivery := journal.Deliveries[fixture.deliveryID]
			task, _ := delivery.Graph.Task("task_01")
			run := deliveryRun{ID: "run_task_01", WorkspaceID: fixture.scope.WorkspaceID, LoopName: "batuta-task", Status: "running", CreatedAt: fixture.now, StartedAt: fixture.now, Inputs: graphTaskRunInputs(delivery, wave, task, 1)}
			runs := &fakeGraphRunReader{recent: []deliveryRun{run}, statuses: map[string]deliveryRunDetail{}}
			service := deliveryGraphService{Store: fixture.store, Runs: runs, Now: func() time.Time { return fixture.now }}
			questionInput := DeliveryGraphInput{Operation: GraphOpRecordQuestion, DeliveryID: fixture.deliveryID, Wave: wave.Number, TaskID: "task_01", Execution: 1, Prompt: "Which compatibility behavior should we ship?", Choices: []string{"Preserve compatibility", "Adopt the new contract"}}
			question, err := service.Execute(context.Background(), fixture.scope, questionInput)
			if err != nil {
				t.Fatalf("record_question: %v", err)
			}
			requests := []deliveryRequest{{
				LoopRunID: run.ID, LoopName: "batuta-task", Generation: 2, NodeID: "ask_operator", ItemIndex: 0,
				Kind: "ask", State: "answered", Prompt: questionInput.Prompt, Context: json.RawMessage(`{"task_id":"task_01"}`), Expect: taskAnswerExpectation(),
				Decisions: []string{"respond"}, Agents: "deny", AnsweredDecision: "respond", ActorKind: "human", ActorID: "operator-1",
				AnsweredAt: timePointer(fixture.now), ResolvedAt: timePointer(fixture.now),
			}}
			if mutate.requests != nil {
				requests = mutate.requests(requests)
			}
			outputs := []deliveryOutput{{NodeID: "ask_operator", ItemIndex: 0, Status: "succeeded", OutputRef: answeredAskOutputRef("Preserve compatibility")}}
			if mutate.outputs != nil {
				outputs = mutate.outputs(outputs)
			}
			generation := int64(2)
			if mutate.generation != 0 {
				generation = mutate.generation
			}
			runs.statuses[run.ID] = deliveryRunDetail{Run: run, Requests: requests, Generations: []deliveryGeneration{{Generation: generation, Outputs: outputs}}}
			_, err = service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{Operation: GraphOpRecordAnswer, DeliveryID: fixture.deliveryID, Wave: wave.Number, TaskID: "task_01", Execution: 1, QuestionOperationID: question.QuestionOperationID, Answer: "Preserve compatibility"})
			if !errors.Is(err, routing.ErrDeliveryConflict) {
				t.Fatalf("record_answer error = %v, want delivery conflict", err)
			}
		})
	}
}

func TestDeliveryGraphServiceRecordsFailureAfterAnsweredContinuationAndReplays(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	task, _ := delivery.Graph.Task("task_01")
	run := deliveryRun{ID: "run_task_01", WorkspaceID: fixture.scope.WorkspaceID, LoopName: "batuta-task", Status: "running", CreatedAt: fixture.now, StartedAt: fixture.now, Inputs: graphTaskRunInputs(delivery, wave, task, 1)}
	runs := &fakeGraphRunReader{recent: []deliveryRun{run}, statuses: map[string]deliveryRunDetail{}}
	service := deliveryGraphService{Store: fixture.store, Runs: runs, Now: func() time.Time { return fixture.now }}
	questionInput := DeliveryGraphInput{Operation: GraphOpRecordQuestion, DeliveryID: fixture.deliveryID, Wave: wave.Number, TaskID: "task_01", Execution: 1, Prompt: "Which compatibility behavior should we ship?", Choices: []string{"Preserve compatibility", "Adopt the new contract"}}
	question, err := service.Execute(context.Background(), fixture.scope, questionInput)
	if err != nil {
		t.Fatalf("record_question: %v", err)
	}
	runs.statuses[run.ID] = deliveryRunDetail{Run: run, Generations: []deliveryGeneration{{Generation: 2, Outputs: []deliveryOutput{{NodeID: "ask_operator", ItemIndex: 0, Status: "succeeded", OutputRef: answeredAskOutputRef("Preserve compatibility")}}}}, Requests: []deliveryRequest{{
		LoopRunID: run.ID, LoopName: "batuta-task", Generation: 2, NodeID: "ask_operator", ItemIndex: 0, Kind: "ask", State: "answered", Prompt: questionInput.Prompt,
		Context: json.RawMessage(`{"task_id":"task_01"}`), Expect: taskAnswerExpectation(), Decisions: []string{"respond"}, Agents: "deny", AnsweredDecision: "respond", ActorKind: "human", ActorID: "operator-1", AnsweredAt: timePointer(fixture.now), ResolvedAt: timePointer(fixture.now),
	}}}
	if _, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{Operation: GraphOpRecordAnswer, DeliveryID: fixture.deliveryID, Wave: wave.Number, TaskID: "task_01", Execution: 1, QuestionOperationID: question.QuestionOperationID, Answer: "Preserve compatibility"}); err != nil {
		t.Fatalf("record_answer: %v", err)
	}
	run.Status, run.TokensUsed, run.TokensUsedPresent = "failed", 1200, true
	runs.statuses[run.ID] = deliveryRunDetail{Run: run}
	failureInput := DeliveryGraphInput{Operation: GraphOpRecordFailure, DeliveryID: fixture.deliveryID, Wave: wave.Number, TaskID: "task_01", Execution: 1, ChildRunID: run.ID, BlockerCode: "implementation_failed"}
	first, err := service.Execute(context.Background(), fixture.scope, failureInput)
	if err != nil || first.Disposition != GraphDispositionPreparing || first.Execution != 3 || first.Wave != 2 {
		t.Fatalf("record_failure after answer = %#v, error=%v", first, err)
	}
	replay, err := service.Execute(context.Background(), fixture.scope, failureInput)
	if err != nil || !reflect.DeepEqual(replay, first) || runs.statusCalls != 2 {
		t.Fatalf("record_failure after answer replay = %#v, error=%v status_calls=%d", replay, err, runs.statusCalls)
	}
}

func TestDeliveryGraphServiceRecordsCandidateAfterTwoAnsweredContinuations(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	task, _ := delivery.Graph.Task("task_01")
	run := deliveryRun{ID: "run_task_01", WorkspaceID: fixture.scope.WorkspaceID, LoopName: "batuta-task", Status: "running", CreatedAt: fixture.now, StartedAt: fixture.now, Inputs: graphTaskRunInputs(delivery, wave, task, 1)}
	runs := &fakeGraphRunReader{recent: []deliveryRun{run}, statuses: map[string]deliveryRunDetail{}}
	validator := &fakeGraphCandidateValidator{evidence: integration.CandidateEvidence{RepositoryIdentity: digestValue("repository"), CommitSHA: "abcdefabcdefabcdefabcdefabcdefabcdefabcd", TreeSHA: "1234512345123451234512345123451234512345", OwnedTrackingPaths: []string{}, Tracking: []integration.TrackingFile{}}}
	service := deliveryGraphService{Store: fixture.store, Runs: runs, Candidates: validator, Now: func() time.Time { return fixture.now }}
	answerInputs := map[int]DeliveryGraphInput{}
	answerQuestion := func(generation int, input DeliveryGraphInput, answer string) DeliveryGraphOutput {
		question, err := service.Execute(context.Background(), fixture.scope, input)
		if err != nil {
			t.Fatalf("record_question generation %d: %v", generation, err)
		}
		runs.statuses[run.ID] = deliveryRunDetail{Run: run, Generations: []deliveryGeneration{{Generation: int64(generation), Outputs: []deliveryOutput{{NodeID: "ask_operator", ItemIndex: 0, Status: "succeeded", OutputRef: answeredAskOutputRef(answer)}}}}, Requests: []deliveryRequest{{
			LoopRunID: run.ID, LoopName: "batuta-task", Generation: generation, NodeID: "ask_operator", ItemIndex: 0, Kind: "ask", State: "answered", Prompt: input.Prompt,
			Context: json.RawMessage(`{"task_id":"task_01"}`), Expect: taskAnswerExpectation(), Decisions: []string{"respond"}, Agents: "deny", AnsweredDecision: "respond", ActorKind: "human", ActorID: "operator-1", AnsweredAt: timePointer(fixture.now), ResolvedAt: timePointer(fixture.now),
		}}}
		answerInput := DeliveryGraphInput{Operation: GraphOpRecordAnswer, DeliveryID: fixture.deliveryID, Wave: wave.Number, TaskID: "task_01", Execution: 1, QuestionOperationID: question.QuestionOperationID, Answer: answer}
		answerInputs[generation] = answerInput
		output, err := service.Execute(context.Background(), fixture.scope, answerInput)
		if err != nil {
			t.Fatalf("record_answer generation %d: %v", generation, err)
		}
		return output
	}
	first := answerQuestion(2, DeliveryGraphInput{Operation: GraphOpRecordQuestion, DeliveryID: fixture.deliveryID, Wave: wave.Number, TaskID: "task_01", Execution: 1, Prompt: "Choose compatibility behavior", Choices: []string{"Preserve compatibility", "Adopt new contract"}}, "Preserve compatibility")
	second := answerQuestion(3, DeliveryGraphInput{Operation: GraphOpRecordQuestion, DeliveryID: fixture.deliveryID, Wave: wave.Number, TaskID: "task_01", Execution: 1, Prompt: "Choose rollout behavior", Choices: []string{"Ship gradually", "Ship immediately"}}, "Ship gradually")
	if first.Execution != 2 || second.Execution != 3 {
		t.Fatalf("continuation executions = %#v, %#v", first, second)
	}
	firstReplay, err := service.Execute(context.Background(), fixture.scope, answerInputs[2])
	if err != nil || firstReplay.Disposition != GraphDispositionTaskReady || firstReplay.Execution != 3 {
		t.Fatalf("first answer replay after second answer = %#v, error=%v", firstReplay, err)
	}
	verification := json.RawMessage(`{"checks":["go test ./..."],"status":"passed","task_id":"task_01"}`)
	verificationDigest := digestValue(string(verification))
	commitSHA := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	run.Status, run.TokensUsed, run.TokensUsedPresent = "done", 700, true
	runs.statuses[run.ID] = deliveryRunDetail{Run: run, Generations: []deliveryGeneration{{Generation: 4, Outputs: []deliveryOutput{{NodeID: "implementation", ItemIndex: 0, Status: "succeeded", OutputRef: completedTaskOutputRef(t, "task_01", 3, commitSHA, verification, verificationDigest)}}}}}
	journal, _, _ = fixture.store.Load(fixture.scope.WorkspaceID)
	delivery = journal.Deliveries[fixture.deliveryID]
	task, _ = delivery.Graph.Task("task_01")
	if !graphTaskRunMatches(run, fixture.scope.WorkspaceID, delivery, wave, task, 3) || !hasCompletedTaskOutput(runs.statuses[run.ID], DeliveryGraphInput{TaskID: "task_01", Execution: 3, CommitSHA: commitSHA, Verification: verification, VerificationDigest: verificationDigest}) {
		t.Fatalf("two-answer completion preconditions do not match task run: %#v", task)
	}
	output, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{Operation: GraphOpRecordCandidate, DeliveryID: fixture.deliveryID, Wave: wave.Number, TaskID: "task_01", Execution: 1, ChildRunID: run.ID, BaseSHA: wave.BaseHeadSHA, CommitSHA: commitSHA, Verification: verification, VerificationDigest: verificationDigest})
	if err != nil || output.Disposition != GraphDispositionCandidateRecorded || output.Execution != 3 {
		t.Fatalf("record_candidate after two answers = %#v, error=%v", output, err)
	}
	afterCandidate, err := service.Execute(context.Background(), fixture.scope, answerInputs[2])
	if err != nil || afterCandidate.Disposition != GraphDispositionCandidateRecorded || afterCandidate.Execution != 3 ||
		afterCandidate.BaseSHA != wave.BaseHeadSHA || !afterCandidate.Replayed {
		t.Fatalf("first answer replay after candidate = %#v, error=%v", afterCandidate, err)
	}
	service.Integrator = &fakeGraphIntegrator{integratedSHA: commitSHA}
	settled, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpSettleWave, DeliveryID: fixture.deliveryID, Wave: wave.Number,
	})
	if err != nil || settled.Disposition != GraphDispositionAllIntegrated {
		t.Fatalf("settle answered candidate = %#v, error=%v", settled, err)
	}
	afterIntegration, err := service.Execute(context.Background(), fixture.scope, answerInputs[2])
	if err != nil || afterIntegration.Disposition != GraphDispositionAllIntegrated || !afterIntegration.Replayed {
		t.Fatalf("first answer replay after integration = %#v, error=%v", afterIntegration, err)
	}
}

func TestDeliveryGraphServiceMapsConflictWithoutRetryBudgetToExhausted(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	operationID := digestValue("conflict-exhausted-operation")
	task := &delivery.Graph.Tasks[0]
	task.State = routing.GraphTaskBlocked
	task.BlockerCode = "integration_conflict_exhausted"
	task.Attempts = []routing.GraphTaskAttempt{{
		Execution: 1, State: routing.GraphTaskBlocked, BaseHeadSHA: delivery.InitialWorktreeFingerprint.HeadSHA,
		CandidateCommitSHA: "1111111111111111111111111111111111111111",
		BlockerCode:        "integration_conflict_exhausted",
		Conflict: &routing.ConflictProof{
			IntegrationOperationID: operationID, IntegrationHeadSHA: delivery.InitialWorktreeFingerprint.HeadSHA,
			CandidateCommitSHA: "1111111111111111111111111111111111111111",
			EvidenceDigest:     digestValue("conflict-exhausted"),
		},
	}}
	delivery.Graph.Waves = []routing.DeliveryWave{{
		Number: 1, BaseHeadSHA: delivery.InitialWorktreeFingerprint.HeadSHA, TaskIDs: []string{task.TaskID},
	}}
	delivery.Graph.Integrations = []routing.IntegrationOperation{{
		OperationID: operationID, RequestDigest: digestValue("conflict-exhausted-request"), Wave: 1,
		StartingHeadSHA: delivery.InitialWorktreeFingerprint.HeadSHA, OrderedTaskIDs: []string{task.TaskID},
		CandidateCommitSHAs: []string{"1111111111111111111111111111111111111111"},
		ConflictingTaskID:   task.TaskID, ConflictEvidenceDigest: digestValue("conflict-exhausted"),
		FinalHeadSHA: delivery.InitialWorktreeFingerprint.HeadSHA,
	}}
	service := deliveryGraphService{Now: func() time.Time { return fixture.now }}

	output, err := service.settlementOutput(delivery, DeliveryGraphInput{
		Operation: GraphOpSettleWave, DeliveryID: fixture.deliveryID, Wave: 1,
	}, operationID)
	if err != nil || output.Disposition != GraphDispositionExhausted || output.BlockerCode != task.BlockerCode {
		t.Fatalf("conflict exhausted = %#v, error=%v", output, err)
	}
}

func TestDeliveryGraphServiceReplaysConflictOutputAfterRetryIntegrated(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	operationID := digestValue("historical-conflict-operation")
	base := delivery.InitialWorktreeFingerprint.HeadSHA
	retryBase := "1111111111111111111111111111111111111111"
	finalHead := "2222222222222222222222222222222222222222"
	runtime := routing.RuntimeValue{Provider: "codex", Model: "gpt-5.6-terra", Reasoning: "high"}
	task := &delivery.Graph.Tasks[0]
	task.State = routing.GraphTaskIntegrated
	task.IntegratedCommitSHA = finalHead
	task.Attempts = []routing.GraphTaskAttempt{
		{Execution: 1, RunExecution: 1, State: routing.GraphTaskCandidate, BaseHeadSHA: base, Conflict: &routing.ConflictProof{
			IntegrationOperationID: operationID, IntegrationHeadSHA: retryBase,
			CandidateCommitSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", EvidenceDigest: digestValue("historical-conflict"),
		}},
		{Execution: 2, RunExecution: 2, Runtime: runtime, State: routing.GraphTaskIntegrated, BaseHeadSHA: retryBase},
	}
	delivery.Graph.Waves = []routing.DeliveryWave{
		{Number: 1, BaseHeadSHA: base, TaskIDs: []string{task.TaskID}},
		{Number: 2, BaseHeadSHA: retryBase, TaskIDs: []string{task.TaskID}},
	}
	delivery.Graph.Integrations = []routing.IntegrationOperation{
		{OperationID: operationID, Wave: 1, ConflictingTaskID: task.TaskID, FinalHeadSHA: retryBase},
		{OperationID: digestValue("historical-retry-operation"), Wave: 2, FinalHeadSHA: finalHead},
	}
	service := deliveryGraphService{Now: func() time.Time { return fixture.now }}

	output, err := service.settlementOutput(delivery, DeliveryGraphInput{
		Operation: GraphOpSettleWave, DeliveryID: fixture.deliveryID, Wave: 1,
	}, operationID)
	if err != nil || output.Disposition != GraphDispositionReexecuteConflict || output.TaskID != task.TaskID ||
		output.Execution != 2 || output.Runtime == nil || *output.Runtime != runtime || output.BaseSHA != retryBase {
		t.Fatalf("historical conflict output = %#v, error=%v", output, err)
	}
}

func TestDeliveryGraphServiceDoesNotAdvertiseOldRunReadyAfterConflictReexecution(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	base := delivery.InitialWorktreeFingerprint.HeadSHA
	retryBase := "1111111111111111111111111111111111111111"
	questionID := digestValue("conflict-replay-question")
	task := &delivery.Graph.Tasks[0]
	task.State = routing.GraphTaskPreparing
	task.Attempts = []routing.GraphTaskAttempt{
		{Execution: 1, RunExecution: 1, State: routing.GraphTaskCandidate, BaseHeadSHA: base, Question: &routing.TaskQuestion{
			RequestID: questionID, Prompt: "Choose compatibility behavior", ContextDigest: canonicalTaskContextDigest("task_01"),
			Answer: &routing.TaskAnswer{QuestionOperationID: questionID, LoopRunID: "run_old", Generation: 2, NodeID: "ask_operator", ItemIndex: 0, Value: "Preserve compatibility"},
		}},
		{Execution: 2, RunExecution: 2, State: routing.GraphTaskPreparing, BaseHeadSHA: retryBase},
	}
	delivery.Graph.Waves = []routing.DeliveryWave{
		{Number: 1, BaseHeadSHA: base, TaskIDs: []string{task.TaskID}},
		{Number: 2, BaseHeadSHA: retryBase, TaskIDs: []string{task.TaskID}},
	}
	service := deliveryGraphService{Now: func() time.Time { return fixture.now }}
	output, err := service.answerReplayOutput(delivery, *task, task.Attempts[0], 1, DeliveryGraphInput{
		Operation: GraphOpRecordAnswer, DeliveryID: fixture.deliveryID, Wave: 1, TaskID: task.TaskID,
		Execution: 1, QuestionOperationID: questionID,
	})
	if err != nil || output.Disposition != GraphDispositionPreparing || output.Execution != 2 || output.Wave != 2 || !output.Replayed {
		t.Fatalf("old-run answer replay after conflict = %#v, error=%v", output, err)
	}
}

func TestDeliveryGraphServiceRejectsHistoricalTaskContextAfterConflictRunIsRunning(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	task := &delivery.Graph.Tasks[0]
	retryBase := "1111111111111111111111111111111111111111"
	current := task.Attempts[0]
	conflictOperationID := digestValue("task-context-running-conflict")
	conflictEvidence := digestValue("task-context-running-conflict-evidence")
	candidateCommit := "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"
	current.State = routing.GraphTaskCandidate
	current.ChildRunID = "run_old"
	current.CandidateCommitSHA = candidateCommit
	current.VerificationDigest = digestValue("task-context-running-verification")
	current.Conflict = &routing.ConflictProof{
		IntegrationOperationID: conflictOperationID, IntegrationHeadSHA: retryBase,
		CandidateCommitSHA: candidateCommit, EvidenceDigest: conflictEvidence,
	}
	task.Attempts[0] = current
	task.Attempts = append(task.Attempts, routing.GraphTaskAttempt{
		Execution: 2, RunExecution: 2, State: routing.GraphTaskRunning,
		WorktreeID: "wt_retry", WorktreeRoot: taskRoot, BaseHeadSHA: retryBase,
		Runtime: current.Runtime, TokenAllowance: current.TokenAllowance,
	})
	task.State = routing.GraphTaskRunning
	delivery.Graph.Waves = append(delivery.Graph.Waves, routing.DeliveryWave{
		Number: 2, BaseHeadSHA: retryBase, TaskIDs: []string{task.TaskID},
	})
	delivery.Graph.Integrations = append(delivery.Graph.Integrations, routing.IntegrationOperation{
		OperationID: conflictOperationID, RequestDigest: digestValue("task-context-running-request"),
		Wave: 1, StartingHeadSHA: wave.BaseHeadSHA, OrderedTaskIDs: []string{task.TaskID},
		CandidateCommitSHAs: []string{candidateCommit}, AcceptedTaskIDs: []string{},
		AcceptedCommitSHAs: []string{}, IntegratedCommitSHAs: []string{},
		ConflictingTaskID: task.TaskID, ConflictEvidenceDigest: conflictEvidence, FinalHeadSHA: retryBase,
	})
	journal.Deliveries[fixture.deliveryID] = delivery
	if err := fixture.store.Save(fixture.scope.WorkspaceID, journal); err != nil {
		t.Fatalf("Save(conflict retry) error = %v", err)
	}

	service := deliveryGraphService{Store: fixture.store, Now: func() time.Time { return fixture.now }}
	_, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpTaskContext, DeliveryID: fixture.deliveryID, Wave: wave.Number,
		TaskID: "task_01", Execution: 1,
	})
	if !errors.Is(err, routing.ErrDeliveryConflict) {
		t.Fatalf("historical task_context after running conflict = %v, want ErrDeliveryConflict", err)
	}
}

func TestDeliveryGraphServiceRecordsTerminalFailureOnceAndPreparesExactFallback(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	task, _ := delivery.Graph.Task("task_01")
	generation := journal.Generations[delivery.RoutingGenerationDigest]
	if len(generation.Cells) != 1 || len(generation.Cells[0].Fallbacks) == 0 {
		t.Fatalf("fixture has no immutable fallback: %#v", generation.Cells)
	}
	run := deliveryRun{
		ID: "run_task_01", WorkspaceID: fixture.scope.WorkspaceID, LoopName: "batuta-task",
		Status: "failed", CreatedAt: fixture.now, StartedAt: fixture.now,
		TokensUsed: 1200, TokensUsedPresent: true, Inputs: graphTaskRunInputs(delivery, wave, task, 1),
	}
	runs := &fakeGraphRunReader{statuses: map[string]deliveryRunDetail{run.ID: {Run: run}}}
	service := deliveryGraphService{Store: fixture.store, Runs: runs, Now: func() time.Time { return fixture.now }}
	input := DeliveryGraphInput{
		Operation: GraphOpRecordFailure, DeliveryID: fixture.deliveryID, Wave: wave.Number,
		TaskID: "task_01", Execution: 1, ChildRunID: run.ID, BlockerCode: "implementation_failed",
	}

	first, err := service.Execute(context.Background(), fixture.scope, input)
	if err != nil || first.Disposition != GraphDispositionPreparing || first.Wave != 2 ||
		first.TaskID != "task_01" || first.Execution != 2 || first.Runtime == nil ||
		*first.Runtime == task.Attempts[0].Runtime || first.RemainingTokens != delivery.TokenCeiling-run.TokensUsed ||
		runs.statusCalls != 1 {
		t.Fatalf("record_failure = %#v, error=%v status_calls=%d", first, err, runs.statusCalls)
	}
	replay, err := service.Execute(context.Background(), fixture.scope, input)
	if err != nil || !reflect.DeepEqual(replay, first) || runs.statusCalls != 1 {
		t.Fatalf("record_failure replay = %#v, error=%v status_calls=%d", replay, err, runs.statusCalls)
	}

	journal, _, _ = fixture.store.Load(fixture.scope.WorkspaceID)
	stored, _ := journal.Deliveries[fixture.deliveryID].Graph.Task("task_01")
	if stored.State != routing.GraphTaskPreparing || len(stored.Attempts) != 2 ||
		stored.Attempts[0].State != routing.GraphTaskBlocked || stored.Attempts[0].TokensUsed == nil ||
		*stored.Attempts[0].TokensUsed != run.TokensUsed || stored.Attempts[1].Runtime != *first.Runtime {
		t.Fatalf("stored failed task = %#v", stored)
	}
}

func TestDeliveryGraphServiceRecordsOneValidatedCandidateAndUsage(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	task, _ := delivery.Graph.Task("task_01")
	run := deliveryRun{
		ID: "run_task_01", WorkspaceID: fixture.scope.WorkspaceID, LoopName: "batuta-task",
		Status: "done", CreatedAt: fixture.now, StartedAt: fixture.now,
		TokensUsed: 900, TokensUsedPresent: true, Inputs: graphTaskRunInputs(delivery, wave, task, 1),
	}
	verification := []byte(`{"checks":["go test ./..."],"status":"passed","task_id":"task_01"}`)
	verificationDigest := digestValue(string(verification))
	commitSHA := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	runOutput := completedTaskOutputRef(t, "task_01", 1, commitSHA, verification, verificationDigest)
	validator := &fakeGraphCandidateValidator{evidence: integration.CandidateEvidence{
		RepositoryIdentity: digestValue("repository"),
		CommitSHA:          commitSHA, TreeSHA: "1234512345123451234512345123451234512345",
		OwnedTrackingPaths: []string{}, Tracking: []integration.TrackingFile{},
	}}
	runs := &fakeGraphRunReader{statuses: map[string]deliveryRunDetail{run.ID: {
		Run: run, Generations: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{{
			NodeID: "implementation", Status: "succeeded", OutputRef: runOutput,
		}}}},
	}}}
	service := deliveryGraphService{
		Store: fixture.store, Runs: runs, Candidates: validator, Now: func() time.Time { return fixture.now },
	}
	input := DeliveryGraphInput{
		Operation: GraphOpRecordCandidate, DeliveryID: fixture.deliveryID, Wave: wave.Number,
		TaskID: "task_01", Execution: 1, ChildRunID: run.ID,
	}
	wrongBase := input
	wrongBase.BaseSHA = "1111111111111111111111111111111111111111"
	wrongBase.CommitSHA = commitSHA
	wrongBase.Verification = verification
	wrongBase.VerificationDigest = verificationDigest
	if _, err := service.Execute(context.Background(), fixture.scope, wrongBase); !errors.Is(err, routing.ErrDeliveryConflict) || validator.calls != 0 {
		t.Fatalf("record_candidate mismatched explicit base error=%v validator_calls=%d, want conflict before Git validation", err, validator.calls)
	}
	before, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	beforeTask, _ := before.Deliveries[fixture.deliveryID].Graph.Task("task_01")
	if beforeTask.State != routing.GraphTaskRunning {
		t.Fatalf("mismatched explicit base mutated graph task: %#v", beforeTask)
	}

	first, err := service.Execute(context.Background(), fixture.scope, input)
	if err != nil || first.Disposition != GraphDispositionCandidateRecorded || first.RemainingTokens != delivery.TokenCeiling-run.TokensUsed ||
		runs.statusCalls != 1 || validator.calls != 1 {
		t.Fatalf("record_candidate = %#v, error=%v status_calls=%d validator_calls=%d", first, err, runs.statusCalls, validator.calls)
	}
	replay, err := service.Execute(context.Background(), fixture.scope, input)
	if err != nil || !reflect.DeepEqual(replay, first) || runs.statusCalls != 1 || validator.calls != 1 {
		t.Fatalf("record_candidate replay = %#v, error=%v status_calls=%d validator_calls=%d", replay, err, runs.statusCalls, validator.calls)
	}
	journal, _, _ = fixture.store.Load(fixture.scope.WorkspaceID)
	stored, _ := journal.Deliveries[fixture.deliveryID].Graph.Task("task_01")
	if stored.State != routing.GraphTaskCandidate || stored.Attempts[0].CandidateCommitSHA != commitSHA ||
		stored.Attempts[0].TokensUsed == nil || *stored.Attempts[0].TokensUsed != run.TokensUsed {
		t.Fatalf("stored candidate = %#v", stored)
	}
	if stored.Attempts[0].CandidateEvidence == nil ||
		stored.Attempts[0].CandidateEvidence.RepositoryIdentity != validator.evidence.RepositoryIdentity ||
		stored.Attempts[0].CandidateEvidence.TreeSHA != validator.evidence.TreeSHA ||
		!bytes.Equal(stored.Attempts[0].CandidateEvidence.Verification, verification) {
		t.Fatalf("stored candidate evidence = %#v", stored.Attempts[0].CandidateEvidence)
	}
}

func TestDeliveryGraphServiceRejectsAmbiguousOrNonInlineDerivedCandidateOutput(t *testing.T) {
	t.Parallel()

	verification := json.RawMessage(`{"checks":["go test ./..."],"status":"passed","task_id":"task_01"}`)
	verificationDigest := digestValue(string(verification))
	commitSHA := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	tests := []struct {
		name    string
		outputs []deliveryGeneration
	}{
		{name: "zero implementation cells", outputs: []deliveryGeneration{{Generation: 1}}},
		{name: "multiple implementation cells", outputs: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{
			{NodeID: "implementation", Status: "succeeded", OutputRef: completedTaskOutputRef(t, "task_01", 1, commitSHA, verification, verificationDigest)},
			{NodeID: "implementation", Status: "succeeded", OutputRef: completedTaskOutputRef(t, "task_01", 1, commitSHA, verification, verificationDigest)},
		}}}},
		{name: "wrong latest generation", outputs: []deliveryGeneration{
			{Generation: 1, Outputs: []deliveryOutput{{NodeID: "implementation", Status: "succeeded", OutputRef: completedTaskOutputRef(t, "task_01", 1, commitSHA, verification, verificationDigest)}}},
			{Generation: 2, Outputs: []deliveryOutput{{NodeID: "implementation", Status: "succeeded", OutputRef: completedTaskOutputRef(t, "task_01", 2, commitSHA, verification, verificationDigest)}}},
		}},
		{name: "wrong node", outputs: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{{NodeID: "implement_task", Status: "succeeded", OutputRef: completedTaskOutputRef(t, "task_01", 1, commitSHA, verification, verificationDigest)}}}}},
		{name: "malformed inline payload", outputs: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{{NodeID: "implementation", Status: "succeeded", OutputRef: `{"status":"completed"}`}}}}},
		{name: "content addressed payload", outputs: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{{NodeID: "implementation", Status: "succeeded", OutputRef: digestValue("external-child-output")}}}}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			fixture := newDeliveryServiceFixture(t)
			taskRoot := t.TempDir()
			writeRoutingTask(t, taskRoot)
			wave := prepareGraphTaskForTest(t, fixture, taskRoot)
			journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
			delivery := journal.Deliveries[fixture.deliveryID]
			task, _ := delivery.Graph.Task("task_01")
			run := deliveryRun{ID: "run_task_01", WorkspaceID: fixture.scope.WorkspaceID, LoopName: "batuta-task", Status: "done", CreatedAt: fixture.now, StartedAt: fixture.now, TokensUsed: 10, TokensUsedPresent: true, Inputs: graphTaskRunInputs(delivery, wave, task, 1)}
			runs := &fakeGraphRunReader{statuses: map[string]deliveryRunDetail{run.ID: {Run: run, Generations: tt.outputs}}}
			service := deliveryGraphService{
				Store:      fixture.store,
				Runs:       runs,
				Candidates: &fakeGraphCandidateValidator{}, Now: func() time.Time { return fixture.now },
			}
			_, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{Operation: GraphOpRecordCandidate, DeliveryID: fixture.deliveryID, Wave: wave.Number, TaskID: "task_01", Execution: 1, ChildRunID: run.ID})
			if !errors.Is(err, routing.ErrDeliveryConflict) || runs.statusCalls != 1 {
				t.Fatalf("derived record_candidate error = %v status_calls=%d, want one authoritative read and ErrDeliveryConflict", err, runs.statusCalls)
			}
		})
	}
}

func TestDeliveryGraphServiceRejectsCandidateWithoutTypedCompletedOutput(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	task, _ := delivery.Graph.Task("task_01")
	verification := []byte(`{"checks":["go test ./..."],"status":"passed","task_id":"task_01"}`)
	input := DeliveryGraphInput{
		Operation: GraphOpRecordCandidate, DeliveryID: fixture.deliveryID, Wave: wave.Number,
		TaskID: "task_01", Execution: 1, ChildRunID: "run_task_01", BaseSHA: wave.BaseHeadSHA,
		CommitSHA: "abcdefabcdefabcdefabcdefabcdefabcdefabcd", Verification: verification,
		VerificationDigest: digestValue(string(verification)),
	}
	run := deliveryRun{
		ID: input.ChildRunID, WorkspaceID: fixture.scope.WorkspaceID, LoopName: "batuta-task",
		Status: "done", CreatedAt: fixture.now, StartedAt: fixture.now,
		TokensUsed: 10, TokensUsedPresent: true, Inputs: graphTaskRunInputs(delivery, wave, task, 1),
	}
	service := deliveryGraphService{
		Store: fixture.store, Runs: &fakeGraphRunReader{statuses: map[string]deliveryRunDetail{run.ID: {Run: run}}},
		Candidates: &fakeGraphCandidateValidator{}, Now: func() time.Time { return fixture.now },
	}

	if _, err := service.Execute(context.Background(), fixture.scope, input); !errors.Is(err, routing.ErrDeliveryConflict) {
		t.Fatalf("record_candidate without completed output error = %v, want ErrDeliveryConflict", err)
	}
}

func TestDeliveryGraphServiceSettlesCandidateThroughCrashSafeIntegratorOnce(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	verification := []byte(`{"checks":["go test ./..."],"status":"passed","task_id":"task_01"}`)
	verificationDigest := digestValue(string(verification))
	candidateSHA := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		_, err := delivery.Graph.RecordCandidate("task_01", 1, routing.TaskCandidate{
			ChildRunID: "run_task_01", BaseHeadSHA: wave.BaseHeadSHA, CommitSHA: candidateSHA,
			VerificationDigest: verificationDigest, TokensUsed: 900,
			Evidence: &routing.TaskCandidateEvidence{
				Slug: delivery.Slug, RepositoryIdentity: digestValue("repository"), Branch: "batuta/task/demo",
				TreeSHA: "1234512345123451234512345123451234512345", Verification: verification,
				OwnedTrackingPaths: []string{}, Tracking: []routing.TaskTrackingFile{},
			},
		})
		if err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist candidate: %v", err)
	}
	integratedSHA := "9876598765987659876598765987659876598765"
	crash := errors.New("simulated crash after durable settlement intent")
	integrator := &fakeGraphIntegrator{integratedSHA: integratedSHA, failOnce: crash}
	integrator.before = func(request integration.IntegrationRequest) error {
		journal, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
		if err != nil || !exists {
			return fmt.Errorf("load settlement intent: exists=%v error=%v", exists, err)
		}
		matches := 0
		for _, payload := range journal.IntegrationStates {
			var intent graphSettlementIntent
			if json.Unmarshal(payload, &intent) == nil && intent.Kind == graphSettlementIntentKind {
				if !reflect.DeepEqual(intent.Request, request) || intent.Wave != wave.Number {
					return fmt.Errorf("settlement intent = %#v, request=%#v", intent, request)
				}
				matches++
			}
		}
		if matches != 1 {
			return fmt.Errorf("settlement intents = %d, want 1", matches)
		}
		return nil
	}
	service := deliveryGraphService{Store: fixture.store, Integrator: integrator, Now: func() time.Time { return fixture.now }}
	input := DeliveryGraphInput{Operation: GraphOpSettleWave, DeliveryID: fixture.deliveryID, Wave: wave.Number}

	if _, err := service.Execute(context.Background(), fixture.scope, input); !errors.Is(err, crash) || integrator.calls != 1 {
		t.Fatalf("settle_wave crash error=%v calls=%d", err, integrator.calls)
	}
	first, err := service.Execute(context.Background(), fixture.scope, input)
	if err != nil || first.Disposition != GraphDispositionAllIntegrated ||
		!routingDigestPattern.MatchString(first.IntegrationOperationID) || integrator.calls != 2 {
		t.Fatalf("settle_wave = %#v, error=%v calls=%d", first, err, integrator.calls)
	}
	replay, err := service.Execute(context.Background(), fixture.scope, input)
	if err != nil || !reflect.DeepEqual(replay, first) || integrator.calls != 2 {
		t.Fatalf("settle_wave replay = %#v, error=%v calls=%d", replay, err, integrator.calls)
	}
	journal, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	stored, _ := journal.Deliveries[fixture.deliveryID].Graph.Task("task_01")
	if stored.State != routing.GraphTaskIntegrated || stored.IntegratedCommitSHA != integratedSHA ||
		len(journal.Deliveries[fixture.deliveryID].Graph.Integrations) != 1 {
		t.Fatalf("settled graph task=%#v operations=%#v", stored, journal.Deliveries[fixture.deliveryID].Graph.Integrations)
	}
	if pending, found, err := pendingGraphSettlementRequest(
		journal, fixture.scope, journal.Deliveries[fixture.deliveryID],
		routing.DeliveryWave{Number: 2, BaseHeadSHA: integratedSHA, TaskIDs: []string{"task_01"}},
	); err != nil || found {
		t.Fatalf("pending intent from completed older wave = %#v, found=%v error=%v", pending, found, err)
	}
	replayRuns := &fakeGraphRunReader{}
	replayValidator := &fakeGraphCandidateValidator{}
	candidateReplay, err := (&deliveryGraphService{
		Store: fixture.store, Runs: replayRuns, Candidates: replayValidator, Now: func() time.Time { return fixture.now },
	}).Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpRecordCandidate, DeliveryID: fixture.deliveryID, Wave: wave.Number,
		TaskID: "task_01", Execution: 1, ChildRunID: "run_task_01", BaseSHA: wave.BaseHeadSHA,
		CommitSHA: candidateSHA, Verification: verification, VerificationDigest: verificationDigest,
	})
	if err != nil || candidateReplay.Disposition != GraphDispositionCandidateRecorded ||
		replayRuns.statusCalls != 0 || replayValidator.calls != 0 {
		t.Fatalf("record_candidate after integration = %#v, error=%v status=%d validator=%d", candidateReplay, err, replayRuns.statusCalls, replayValidator.calls)
	}
}

func TestDeliveryGraphServiceReplaysConcurrentConflictSettlementWithoutReservingTokensAgain(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	verification := []byte(`{"checks":["go test ./..."],"status":"passed","task_id":"task_01"}`)
	candidateSHA := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		_, err := delivery.Graph.RecordCandidate("task_01", 1, routing.TaskCandidate{
			ChildRunID: "run_task_01", BaseHeadSHA: wave.BaseHeadSHA, CommitSHA: candidateSHA,
			VerificationDigest: digestValue(string(verification)), TokensUsed: 900,
			Evidence: &routing.TaskCandidateEvidence{
				Slug: delivery.Slug, RepositoryIdentity: digestValue("repository"), Branch: "batuta/task/demo",
				TreeSHA: "1234512345123451234512345123451234512345", Verification: verification,
				OwnedTrackingPaths: []string{}, Tracking: []routing.TaskTrackingFile{},
			},
		})
		if err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist candidate: %v", err)
	}

	started := make(chan struct{}, 2)
	release := make(chan struct{})
	integrator := &barrierConflictIntegrator{started: started, release: release}
	service := deliveryGraphService{Store: fixture.store, Integrator: integrator, Now: func() time.Time { return fixture.now }}
	input := DeliveryGraphInput{Operation: GraphOpSettleWave, DeliveryID: fixture.deliveryID, Wave: wave.Number}
	type result struct {
		output DeliveryGraphOutput
		err    error
	}
	results := make(chan result, 2)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	for range 2 {
		go func() {
			output, err := service.Execute(ctx, fixture.scope, input)
			results <- result{output: output, err: err}
		}()
	}
	for range 2 {
		select {
		case <-started:
		case <-ctx.Done():
			t.Fatalf("concurrent settlements did not both reach integration: %v", ctx.Err())
		}
	}
	close(release)
	for range 2 {
		settled := <-results
		if settled.err != nil || settled.output.Disposition != GraphDispositionReexecuteConflict ||
			settled.output.TaskID != "task_01" || settled.output.Execution != 2 {
			t.Fatalf("concurrent conflict settlement = %#v, error=%v", settled.output, settled.err)
		}
	}
}

func TestWaveSettlementSelectsIndependentCandidatesButFencesConflictSuffix(t *testing.T) {
	t.Parallel()

	base := "0123456789abcdef0123456789abcdef01234567"
	candidate := func(taskID, commit string) routing.GraphTask {
		verification := json.RawMessage(fmt.Sprintf(`{"checks":["test"],"status":"passed","task_id":%q}`, taskID))
		return routing.GraphTask{TaskID: taskID, State: routing.GraphTaskCandidate, Attempts: []routing.GraphTaskAttempt{{
			Execution: 1, State: routing.GraphTaskCandidate, BaseHeadSHA: base,
			WorktreeID: "wt_" + taskID, WorktreeRoot: "/managed/" + taskID, ChildRunID: "run_" + taskID,
			CandidateCommitSHA: commit, VerificationDigest: digestValue(string(verification)), TokensUsed: int64Pointer(10),
			CandidateEvidence: &routing.TaskCandidateEvidence{
				Slug: "demo", RepositoryIdentity: digestValue("repo"), Branch: "batuta/task/" + taskID,
				TreeSHA: "1234512345123451234512345123451234512345", Verification: verification,
				OwnedTrackingPaths: []string{}, Tracking: []routing.TaskTrackingFile{},
			},
		}}}
	}
	first := candidate("task_01", "1111111111111111111111111111111111111111")
	waiting := routing.GraphTask{TaskID: "task_02", State: routing.GraphTaskWaitingInput, Attempts: []routing.GraphTaskAttempt{{
		Execution: 1, State: routing.GraphTaskWaitingInput, BaseHeadSHA: base,
	}}}
	third := candidate("task_03", "3333333333333333333333333333333333333333")
	wave := routing.DeliveryWave{Number: 1, BaseHeadSHA: base, TaskIDs: []string{"task_01", "task_02", "task_03"}}
	delivery := routing.DeliveryRecord{Graph: &routing.DeliveryGraph{
		Tasks: []routing.GraphTask{first, waiting, third}, Waves: []routing.DeliveryWave{wave},
		Integrations: []routing.IntegrationOperation{}, Pauses: []routing.HumanPause{}, Cleanups: []routing.CleanupOperation{},
	}}

	candidates, replayOperation, err := waveSettlementCandidates(delivery, wave)
	if err != nil || replayOperation != "" || len(candidates) != 2 ||
		candidates[0].TaskID != "task_01" || candidates[1].TaskID != "task_03" {
		t.Fatalf("independent candidates = %#v replay=%q error=%v", candidates, replayOperation, err)
	}

	conflictOperation := digestValue("middle-conflict")
	delivery.Graph.Tasks[0].State = routing.GraphTaskIntegrated
	delivery.Graph.Tasks[1] = routing.GraphTask{
		TaskID: "task_02", State: routing.GraphTaskPreparing,
		Attempts: []routing.GraphTaskAttempt{
			{Execution: 1, State: routing.GraphTaskCandidate, BaseHeadSHA: base, Conflict: &routing.ConflictProof{
				IntegrationOperationID: conflictOperation, IntegrationHeadSHA: base,
				CandidateCommitSHA: "2222222222222222222222222222222222222222", EvidenceDigest: digestValue("conflict"),
			}},
			{Execution: 2, State: routing.GraphTaskPreparing, BaseHeadSHA: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa"},
		},
	}
	delivery.Graph.Integrations = []routing.IntegrationOperation{{
		OperationID: conflictOperation, Wave: 1, ConflictingTaskID: "task_02",
	}}
	candidates, replayOperation, err = waveSettlementCandidates(delivery, wave)
	if err != nil || len(candidates) != 0 || replayOperation != conflictOperation {
		t.Fatalf("conflict fence candidates = %#v replay=%q error=%v", candidates, replayOperation, err)
	}

	delivery.Graph.Tasks[1].State = routing.GraphTaskIntegrated
	candidates, replayOperation, err = waveSettlementCandidates(delivery, wave)
	if err != nil || replayOperation != "" || len(candidates) != 1 || candidates[0].TaskID != "task_03" {
		t.Fatalf("released suffix candidates = %#v replay=%q error=%v", candidates, replayOperation, err)
	}
}

func TestRoutingIntegrationJournalPersistsCASAndProjectsUnderOwnershipLock(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	candidateSHA := "1111111111111111111111111111111111111111"
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		if _, err := delivery.Graph.RecordCandidate("task_01", 1, routing.TaskCandidate{
			ChildRunID: "run_task_01", BaseHeadSHA: wave.BaseHeadSHA, CommitSHA: candidateSHA,
			VerificationDigest: digestValue("tracking-verification"), TokensUsed: 10,
		}); err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist candidate: %v", err)
	}
	locker := routingIntegrationLocker{StoreForCall: func() (*routing.OwnershipStore, error) {
		return fixture.store, nil
	}}
	operationID := digestValue("adapter-operation")
	requestDigest := digestValue("adapter-request")
	state := integration.OperationState{
		WorkspaceID: fixture.scope.WorkspaceID, DeliveryID: fixture.deliveryID,
		OperationID: operationID, RequestDigest: requestDigest,
	}
	if err := locker.WithLocked(context.Background(), fixture.scope.WorkspaceID, func(journal integration.Journal) error {
		if _, exists, err := journal.Load(context.Background(), fixture.scope.WorkspaceID, fixture.deliveryID, operationID); err != nil || exists {
			t.Fatalf("Load(before) exists=%v error=%v", exists, err)
		}
		projection, err := journal.ProjectTracking(context.Background(), integration.TrackingProjectionRequest{
			WorkspaceID: fixture.scope.WorkspaceID, DeliveryID: fixture.deliveryID,
			OperationID: operationID, RequestDigest: requestDigest,
			AcceptedTaskIDs:      []string{"task_01"},
			AcceptedCommitSHAs:   []string{candidateSHA},
			IntegratedCommitSHAs: []string{"2222222222222222222222222222222222222222"},
		})
		if err != nil || !routingDigestPattern.MatchString(projection.Revision) ||
			projection.RequestDigest != requestDigest || len(projection.Files) != 1 ||
			projection.Files[0].Path != ".compozy/tasks/demo/_index.json" ||
			!routingDigestPattern.MatchString(projection.Files[0].Digest) ||
			!bytes.Contains(projection.Files[0].Content, []byte(`"task_id":"task_01","state":"integrated"`)) ||
			!bytes.Contains(projection.Files[0].Content, []byte(`"integrated_commit_sha":"2222222222222222222222222222222222222222"`)) {
			t.Fatalf("ProjectTracking() = %#v, error=%v", projection, err)
		}
		return journal.CompareAndSwap(context.Background(), nil, state)
	}); err != nil {
		t.Fatalf("WithLocked(write) error = %v", err)
	}
	if err := locker.WithLocked(context.Background(), fixture.scope.WorkspaceID, func(journal integration.Journal) error {
		loaded, exists, err := journal.Load(context.Background(), fixture.scope.WorkspaceID, fixture.deliveryID, operationID)
		if err != nil || !exists || !reflect.DeepEqual(loaded, state) {
			t.Fatalf("Load(after) = %#v, exists=%v error=%v", loaded, exists, err)
		}
		if err := journal.CompareAndSwap(context.Background(), nil, state); !errors.Is(err, integration.ErrJournalConflict) {
			t.Fatalf("CompareAndSwap(stale) error = %v", err)
		}
		return nil
	}); err != nil {
		t.Fatalf("WithLocked(read) error = %v", err)
	}
}

func TestDeliveryGraphServiceCleansOnlyIntegratedOwnedWorktreeOnce(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	candidateSHA := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		if _, err := delivery.Graph.RecordCandidate("task_01", 1, routing.TaskCandidate{
			ChildRunID: "run_task_01", BaseHeadSHA: wave.BaseHeadSHA, CommitSHA: candidateSHA,
			VerificationDigest: digestValue("cleanup-verification"), TokensUsed: 500,
		}); err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist candidate: %v", err)
	}
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		generation := tx.Journal.Generations[delivery.RoutingGenerationDigest]
		_, err := delivery.Graph.SettleWave(routing.WaveSettlement{
			OperationID: digestValue("cleanup-settle-operation"), RequestDigest: digestValue("cleanup-settle-request"),
			Wave: wave.Number, StartingHeadSHA: wave.BaseHeadSHA, OrderedTaskIDs: []string{"task_01"},
			CandidateCommitSHAs: []string{candidateSHA}, AcceptedTaskIDs: []string{"task_01"},
			AcceptedCommitSHAs: []string{candidateSHA}, IntegratedCommitSHAs: []string{candidateSHA}, FinalHeadSHA: candidateSHA,
		}, generation)
		if err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist integrated task: %v", err)
	}
	identity, err := worktreeops.DeriveIdentity(worktreeops.IdentityInput{
		WorkspaceID: fixture.scope.WorkspaceID, DeliveryID: fixture.deliveryID, Wave: wave.Number,
		Slug: "demo", TaskID: "task_01", Execution: 1, BaseSHA: wave.BaseHeadSHA,
	})
	if err != nil {
		t.Fatalf("DeriveIdentity() error = %v", err)
	}
	worktrees := &fakeGraphWorktreeClient{scope: fixture.scope, state: "ready", allowRemove: true, byID: map[string]worktreeops.Worktree{
		"wt_task_01": {
			ID: "wt_task_01", Name: identity.Name, Root: taskRoot, WorkspaceID: fixture.scope.WorkspaceID,
			RepositoryRoot: fixture.scope.WorkspaceRoot, RepositoryIdentity: digestValue("repository"),
			Branch: identity.Branch, BaseRef: wave.BaseHeadSHA, BaseSHA: wave.BaseHeadSHA,
			State: "ready", Setup: worktreeops.SetupResult{State: "ok"},
		},
	}}
	service := deliveryGraphService{
		Store: fixture.store, Worktrees: worktrees, Now: func() time.Time { return fixture.now },
		CommitReachable: func(context.Context, string, string, string) (bool, error) { return true, nil },
		WorktreeState: func(context.Context, string) (publication.WorktreeState, error) {
			return publication.WorktreeState{HeadSHA: candidateSHA, PorcelainSHA256: emptyDigest(), ContentSHA256: emptyDigest()}, nil
		},
	}
	input := DeliveryGraphInput{Operation: GraphOpCleanup, DeliveryID: fixture.deliveryID}

	first, err := service.Execute(context.Background(), fixture.scope, input)
	if err != nil || first.Disposition != GraphDispositionCleaned || len(first.CleanupResults) != 1 ||
		first.CleanupResults[0].State != string(routing.CleanupRemoved) || worktrees.removeCalls != 1 {
		t.Fatalf("cleanup = %#v, error=%v remove_calls=%d", first, err, worktrees.removeCalls)
	}
	replay, err := service.Execute(context.Background(), fixture.scope, input)
	if err != nil || !reflect.DeepEqual(replay, first) || worktrees.removeCalls != 1 {
		t.Fatalf("cleanup replay = %#v, error=%v remove_calls=%d", replay, err, worktrees.removeCalls)
	}
}

func TestDeliveryGraphServiceTerminalizesOnlyProvenGraphDispositionAndReplays(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	prepareGraphTaskForTest(t, fixture, taskRoot)
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		generation := tx.Journal.Generations[delivery.RoutingGenerationDigest]
		if _, err := delivery.Graph.RecordFailure("task_01", 1, routing.TaskFailure{
			ChildRunID: "run_batuta_task", TerminalStatus: "failed", BlockerCode: "task_terminal_failed", TokensUsed: 125,
		}, generation, delivery.InitialWorktreeFingerprint.HeadSHA, false); err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist blocked graph: %v", err)
	}
	service := deliveryGraphService{Store: fixture.store, Now: func() time.Time { return fixture.now }}
	input := DeliveryGraphInput{Operation: GraphOpTerminalize, DeliveryID: fixture.deliveryID, TerminalDisposition: GraphDispositionBlocked}
	if _, err := service.Execute(context.Background(), fixture.scope, input); !errors.Is(err, errDeliveryGraphTerminalized) {
		t.Fatalf("Execute(terminalize blocked) error = %v, want stable terminalizer error", err)
	}
	journal, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !exists || journal.Deliveries[fixture.deliveryID].State != routing.DeliveryStateBlocked {
		t.Fatalf("terminalized delivery exists=%v error=%v delivery=%#v", exists, err, journal.Deliveries[fixture.deliveryID])
	}
	before := journal
	if _, err := service.Execute(context.Background(), fixture.scope, input); !errors.Is(err, errDeliveryGraphTerminalized) {
		t.Fatalf("Execute(terminalize replay) error = %v, want stable terminalizer error", err)
	}
	after, _, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("terminalizer replay mutated journal: before=%#v after=%#v error=%v", before, after, err)
	}
	if _, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpTerminalize, DeliveryID: fixture.deliveryID, TerminalDisposition: GraphDispositionExhausted,
	}); !errors.Is(err, routing.ErrDeliveryConflict) {
		t.Fatalf("Execute(unproven exhausted) error = %v, want delivery conflict", err)
	}
}

func TestDeliveryGraphServiceTerminalizesProvenExhaustedBudget(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	prepareGraphTaskForTest(t, fixture, taskRoot)
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		generation := tx.Journal.Generations[delivery.RoutingGenerationDigest]
		if _, err := delivery.Graph.RecordFailure("task_01", 1, routing.TaskFailure{
			ChildRunID: "run_batuta_task", TerminalStatus: "exhausted", BlockerCode: "task_budget_exhausted", TokensUsed: delivery.TokenCeiling,
		}, generation, delivery.InitialWorktreeFingerprint.HeadSHA, false); err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist exhausted graph: %v", err)
	}
	service := deliveryGraphService{Store: fixture.store, Now: func() time.Time { return fixture.now }}
	input := DeliveryGraphInput{Operation: GraphOpTerminalize, DeliveryID: fixture.deliveryID, TerminalDisposition: GraphDispositionExhausted}
	if _, err := service.Execute(context.Background(), fixture.scope, input); !errors.Is(err, errDeliveryGraphTerminalized) {
		t.Fatalf("Execute(terminalize exhausted) error = %v", err)
	}
	before, _, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || before.Deliveries[fixture.deliveryID].State != routing.DeliveryStateExhausted {
		t.Fatalf("exhausted terminal state error=%v journal=%#v", err, before)
	}
	if _, err := service.Execute(context.Background(), fixture.scope, input); !errors.Is(err, errDeliveryGraphTerminalized) {
		t.Fatalf("Execute(terminalize exhausted replay) error = %v", err)
	}
	after, _, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !reflect.DeepEqual(after, before) {
		t.Fatalf("exhausted replay mutated journal before=%#v after=%#v error=%v", before, after, err)
	}
}

func TestDeliveryGraphServiceTerminalizesRetainedCleanupWithoutExternalWork(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	commit := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		if _, err := delivery.Graph.RecordCandidate("task_01", 1, routing.TaskCandidate{ChildRunID: "run_task", BaseHeadSHA: wave.BaseHeadSHA, CommitSHA: commit, VerificationDigest: digestValue("retained-cleanup"), TokensUsed: 1}); err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist retained candidate: %v", err)
	}
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		generation := tx.Journal.Generations[delivery.RoutingGenerationDigest]
		if _, err := delivery.Graph.SettleWave(routing.WaveSettlement{OperationID: digestValue("retained-settle"), RequestDigest: digestValue("retained-request"), Wave: wave.Number, StartingHeadSHA: wave.BaseHeadSHA, OrderedTaskIDs: []string{"task_01"}, CandidateCommitSHAs: []string{commit}, AcceptedTaskIDs: []string{"task_01"}, AcceptedCommitSHAs: []string{commit}, IntegratedCommitSHAs: []string{commit}, FinalHeadSHA: commit}, generation); err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist retained settlement: %v", err)
	}
	identity, err := worktreeops.DeriveIdentity(worktreeops.IdentityInput{
		WorkspaceID: fixture.scope.WorkspaceID, DeliveryID: fixture.deliveryID, Wave: wave.Number,
		Slug: "demo", TaskID: "task_01", Execution: 1, BaseSHA: wave.BaseHeadSHA,
	})
	if err != nil {
		t.Fatalf("DeriveIdentity() error = %v", err)
	}
	worktrees := &fakeGraphWorktreeClient{scope: fixture.scope, state: "ready", byID: map[string]worktreeops.Worktree{
		"wt_task_01": {
			ID: "wt_task_01", Name: identity.Name, Root: taskRoot, WorkspaceID: fixture.scope.WorkspaceID,
			RepositoryRoot: fixture.scope.WorkspaceRoot, RepositoryIdentity: digestValue("repository"),
			Branch: identity.Branch, BaseRef: wave.BaseHeadSHA, BaseSHA: wave.BaseHeadSHA,
			State: "ready", Setup: worktreeops.SetupResult{State: "ok"},
		},
	}}
	service := deliveryGraphService{
		Store: fixture.store, Worktrees: worktrees, Now: func() time.Time { return fixture.now },
		CommitReachable: func(context.Context, string, string, string) (bool, error) { return true, nil },
		WorktreeState: func(context.Context, string) (publication.WorktreeState, error) {
			return publication.WorktreeState{
				HeadSHA: commit, PorcelainSHA256: digestValue("diagnostic-worktree"), ContentSHA256: emptyDigest(),
			}, nil
		},
	}
	cleanup, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpCleanup, DeliveryID: fixture.deliveryID,
	})
	if err != nil || cleanup.Disposition != GraphDispositionBlocked || cleanup.BlockerCode != "worktree_evidence_changed" ||
		len(cleanup.CleanupResults) != 1 || cleanup.CleanupResults[0].State != string(routing.CleanupRetained) ||
		worktrees.removeCalls != 0 {
		t.Fatalf("retained diagnostic cleanup = %#v error=%v remove_calls=%d", cleanup, err, worktrees.removeCalls)
	}
	retained, exists, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !exists || len(retained.Deliveries[fixture.deliveryID].Graph.Cleanups) != 1 ||
		retained.Deliveries[fixture.deliveryID].Graph.Cleanups[0].State != routing.CleanupRetained ||
		retained.Deliveries[fixture.deliveryID].Graph.Cleanups[0].BlockerCode != "worktree_evidence_changed" {
		t.Fatalf("retained diagnostic cleanup was not durable: journal=%#v exists=%v error=%v", retained, exists, err)
	}
	input := DeliveryGraphInput{Operation: GraphOpTerminalize, DeliveryID: fixture.deliveryID, TerminalDisposition: GraphDispositionBlocked}
	if _, err := service.Execute(context.Background(), fixture.scope, input); !errors.Is(err, errDeliveryGraphTerminalized) {
		t.Fatalf("terminalize retained cleanup: %v", err)
	}
	before, _, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || before.Deliveries[fixture.deliveryID].State != routing.DeliveryStateBlocked {
		t.Fatalf("state error=%v journal=%#v", err, before)
	}
	if _, err := service.Execute(context.Background(), fixture.scope, input); !errors.Is(err, errDeliveryGraphTerminalized) {
		t.Fatalf("terminalize replay: %v", err)
	}
	after, _, err := fixture.store.Load(fixture.scope.WorkspaceID)
	if err != nil || !reflect.DeepEqual(before, after) {
		t.Fatalf("retained cleanup replay mutated journal: %v", err)
	}
	if worktrees.removeCalls != 0 {
		t.Fatalf("terminalization/replay must not remove retained worktree; remove_calls=%d", worktrees.removeCalls)
	}
}

func TestDeliveryGraphServiceCleansAnsweredContinuationUsingLatestCandidateEvidence(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	questionID := digestValue("cleanup-question")
	candidateSHA := "fedcbafedcbafedcbafedcbafedcbafedcbafedc"
	persistTransition := func(label string, transition func(*routing.DeliveryRecord, routing.RoutingGeneration) error) {
		t.Helper()
		if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
			delivery := tx.Journal.Deliveries[fixture.deliveryID]
			generation := tx.Journal.Generations[delivery.RoutingGenerationDigest]
			if err := transition(&delivery, generation); err != nil {
				return err
			}
			tx.Journal.Deliveries[fixture.deliveryID] = delivery
			return tx.Persist()
		}); err != nil {
			t.Fatalf("persist %s: %v", label, err)
		}
	}
	persistTransition("question", func(delivery *routing.DeliveryRecord, _ routing.RoutingGeneration) error {
		_, err := delivery.Graph.RecordQuestion("task_01", 1, "run_task_01", routing.TaskQuestion{
			RequestID: questionID, Prompt: "Choose the compatible behavior",
			ContextDigest: canonicalTaskContextDigest("task_01"),
			Choices:       []string{"compatible"},
		}, fixture.now)
		return err
	})
	persistTransition("answer", func(delivery *routing.DeliveryRecord, _ routing.RoutingGeneration) error {
		_, _, err := delivery.Graph.RecordAnswer("task_01", 1, routing.TaskAnswer{
			QuestionOperationID: questionID, LoopRunID: "run_task_01", Generation: 1,
			NodeID: "ask_operator", ItemIndex: 0, Value: "compatible",
		}, fixture.now.Add(time.Minute))
		return err
	})
	persistTransition("candidate", func(delivery *routing.DeliveryRecord, _ routing.RoutingGeneration) error {
		_, err := delivery.Graph.RecordCandidate("task_01", 2, routing.TaskCandidate{
			ChildRunID: "run_task_01", BaseHeadSHA: wave.BaseHeadSHA, CommitSHA: candidateSHA,
			VerificationDigest: digestValue("cleanup-continuation-verification"), TokensUsed: 500,
		})
		return err
	})
	persistTransition("settlement", func(delivery *routing.DeliveryRecord, generation routing.RoutingGeneration) error {
		_, err := delivery.Graph.SettleWave(routing.WaveSettlement{
			OperationID: digestValue("cleanup-continuation-settle"), RequestDigest: digestValue("cleanup-continuation-request"),
			Wave: wave.Number, StartingHeadSHA: wave.BaseHeadSHA, OrderedTaskIDs: []string{"task_01"},
			CandidateCommitSHAs: []string{candidateSHA}, AcceptedTaskIDs: []string{"task_01"},
			AcceptedCommitSHAs: []string{candidateSHA}, IntegratedCommitSHAs: []string{candidateSHA}, FinalHeadSHA: candidateSHA,
		}, generation)
		return err
	})
	identity, err := worktreeops.DeriveIdentity(worktreeops.IdentityInput{
		WorkspaceID: fixture.scope.WorkspaceID, DeliveryID: fixture.deliveryID, Wave: wave.Number,
		Slug: "demo", TaskID: "task_01", Execution: 1, BaseSHA: wave.BaseHeadSHA,
	})
	if err != nil {
		t.Fatalf("DeriveIdentity() error = %v", err)
	}
	worktrees := &fakeGraphWorktreeClient{scope: fixture.scope, state: "ready", allowRemove: true, byID: map[string]worktreeops.Worktree{
		"wt_task_01": {
			ID: "wt_task_01", Name: identity.Name, Root: taskRoot, WorkspaceID: fixture.scope.WorkspaceID,
			RepositoryRoot: fixture.scope.WorkspaceRoot, RepositoryIdentity: digestValue("repository"),
			Branch: identity.Branch, BaseRef: wave.BaseHeadSHA, BaseSHA: wave.BaseHeadSHA,
			State: "ready", Setup: worktreeops.SetupResult{State: "ok"},
		},
	}}
	service := deliveryGraphService{
		Store: fixture.store, Worktrees: worktrees, Now: func() time.Time { return fixture.now.Add(2 * time.Minute) },
		CommitReachable: func(context.Context, string, string, string) (bool, error) { return true, nil },
		WorktreeState: func(context.Context, string) (publication.WorktreeState, error) {
			return publication.WorktreeState{HeadSHA: candidateSHA, PorcelainSHA256: emptyDigest(), ContentSHA256: emptyDigest()}, nil
		},
	}

	output, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpCleanup, DeliveryID: fixture.deliveryID,
	})
	if err != nil || output.Disposition != GraphDispositionCleaned || len(output.CleanupResults) != 1 ||
		output.CleanupResults[0].Execution != 1 || output.CleanupResults[0].State != string(routing.CleanupRemoved) ||
		worktrees.removeCalls != 1 {
		t.Fatalf("cleanup continuation = %#v, error=%v remove_calls=%d", output, err, worktrees.removeCalls)
	}
}

func graphTaskRunInputs(
	delivery routing.DeliveryRecord,
	wave routing.DeliveryWave,
	task routing.GraphTask,
	execution int,
) map[string]any {
	attempt := task.Attempts[execution-1]
	return map[string]any{
		"delivery_id": delivery.DeliveryID, "wave": wave.Number, "task_id": task.TaskID,
		"execution": execution, "routing_generation": delivery.RoutingGenerationDigest,
		"runtime": map[string]any{
			"provider": attempt.Runtime.Provider, "model": attempt.Runtime.Model, "reasoning": attempt.Runtime.Reasoning,
		},
		"worktree_ref": attempt.WorktreeID, "base_sha": attempt.BaseHeadSHA,
	}
}

func completedTaskOutputRef(
	t *testing.T,
	taskID string,
	execution int,
	commitSHA string,
	verification json.RawMessage,
	verificationDigest string,
) string {
	t.Helper()
	payload, err := json.Marshal(map[string]any{
		"status": "completed", "task_id": taskID, "execution": execution,
		"commit_sha":          commitSHA,
		"verification":        json.RawMessage(verification),
		"verification_digest": verificationDigest,
	})
	if err != nil {
		t.Fatalf("marshal completed task output: %v", err)
	}
	return string(payload)
}

func answeredAskOutputRef(answer string) string {
	return `{"answer":"` + answer + `"}`
}

func timePointer(value time.Time) *time.Time {
	return &value
}

func int64Pointer(value int64) *int64 {
	return &value
}

type fakeGraphRunReader struct {
	recent      []deliveryRun
	statuses    map[string]deliveryRunDetail
	recentCalls int
	statusCalls int
}

type fakeGraphCandidateValidator struct {
	evidence integration.CandidateEvidence
	calls    int
	last     integration.CandidateRequest
}

type fakeGraphIntegrator struct {
	calls         int
	integratedSHA string
	last          integration.IntegrationRequest
	before        func(integration.IntegrationRequest) error
	failOnce      error
}

type barrierConflictIntegrator struct {
	started chan<- struct{}
	release <-chan struct{}
}

func (i *barrierConflictIntegrator) Integrate(ctx context.Context, request integration.IntegrationRequest) (integration.IntegrationResult, error) {
	i.started <- struct{}{}
	select {
	case <-i.release:
	case <-ctx.Done():
		return integration.IntegrationResult{}, ctx.Err()
	}
	return integration.IntegrationResult{
		OperationID: request.OperationID, RequestDigest: request.RequestDigest,
		AcceptedTaskIDs: []string{}, AcceptedCommitSHAs: []string{}, IntegratedCommitSHAs: []string{},
		FirstConflictTaskID: request.Candidates[0].TaskID, ConflictEvidenceDigest: digestValue("concurrent-conflict"),
		ResultingHeadSHA: request.StartingHeadSHA, Complete: true,
	}, nil
}

func (i *fakeGraphIntegrator) Integrate(_ context.Context, request integration.IntegrationRequest) (integration.IntegrationResult, error) {
	i.calls++
	i.last = request
	if i.before != nil {
		if err := i.before(request); err != nil {
			return integration.IntegrationResult{}, err
		}
	}
	if i.failOnce != nil {
		err := i.failOnce
		i.failOnce = nil
		return integration.IntegrationResult{}, err
	}
	acceptedTaskIDs := make([]string, len(request.Candidates))
	acceptedCommitSHAs := make([]string, len(request.Candidates))
	integratedCommitSHAs := make([]string, len(request.Candidates))
	for index, candidate := range request.Candidates {
		acceptedTaskIDs[index] = candidate.TaskID
		acceptedCommitSHAs[index] = candidate.CommitSHA
		integratedCommitSHAs[index] = i.integratedSHA
	}
	return integration.IntegrationResult{
		OperationID: request.OperationID, RequestDigest: request.RequestDigest,
		AcceptedTaskIDs: acceptedTaskIDs, AcceptedCommitSHAs: acceptedCommitSHAs,
		IntegratedCommitSHAs: integratedCommitSHAs, ResultingHeadSHA: i.integratedSHA, Complete: true,
	}, nil
}

func (v *fakeGraphCandidateValidator) Candidate(_ context.Context, request integration.CandidateRequest) (integration.CandidateEvidence, error) {
	v.calls++
	v.last = request
	evidence := v.evidence
	evidence.TaskID = request.TaskID
	evidence.Slug = request.Slug
	evidence.WorktreeRoot = request.WorktreeRoot
	evidence.Branch = request.ExpectedBranch
	evidence.BaseSHA = request.BaseSHA
	evidence.VerificationDigest = request.VerificationDigest
	return evidence, nil
}

func (r *fakeGraphRunReader) RecentTasks(context.Context, string, int) ([]deliveryRun, error) {
	r.recentCalls++
	return append([]deliveryRun(nil), r.recent...), nil
}

func (r *fakeGraphRunReader) Status(_ context.Context, _ string, runID string) (deliveryRunDetail, error) {
	r.statusCalls++
	status, exists := r.statuses[runID]
	if !exists {
		return deliveryRunDetail{}, fmt.Errorf("unexpected status for %s", runID)
	}
	return status, nil
}

func prepareGraphTaskForTest(t *testing.T, fixture deliveryServiceFixture, taskRoot string) routing.DeliveryWave {
	t.Helper()
	var wave routing.DeliveryWave
	err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		record := tx.Journal.Deliveries[fixture.deliveryID]
		generation := tx.Journal.Generations[record.RoutingGenerationDigest]
		var err error
		wave, err = record.Graph.AdmitReadyWave(routing.ReadyWaveInput{
			IntegrationHeadSHA: record.InitialWorktreeFingerprint.HeadSHA,
			RemainingSlots:     1,
			ReachableCommits:   map[string]bool{},
		})
		if err != nil {
			return err
		}
		if err := record.Graph.BeginWaveAttempts(wave.Number, generation); err != nil {
			return err
		}
		identity, err := worktreeops.DeriveIdentity(worktreeops.IdentityInput{
			WorkspaceID: fixture.scope.WorkspaceID, DeliveryID: fixture.deliveryID, Wave: wave.Number,
			Slug: record.Slug, TaskID: "task_01", Execution: 1, BaseSHA: wave.BaseHeadSHA,
		})
		if err != nil {
			return err
		}
		if _, err := record.Graph.PlanWorktree("task_01", 1, routing.TaskWorktreeIntent{
			OperationID: identity.OperationID, RequestDigest: identity.RequestDigest,
			Name: identity.Name, Branch: identity.Branch,
		}); err != nil {
			return err
		}
		available, err := record.Graph.AvailableTokenBudget(record.TokenCeiling)
		if err != nil {
			return err
		}
		if err := record.Graph.ReserveWaveTokens(wave.Number, available); err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = record
		return tx.Persist()
	})
	if err != nil {
		t.Fatalf("prepare graph task: %v", err)
	}
	err = fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		record := tx.Journal.Deliveries[fixture.deliveryID]
		if _, err := record.Graph.AttachWorktree("task_01", 1, routing.GraphWorktree{
			ID: "wt_task_01", Root: taskRoot, Ready: true,
		}); err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = record
		return tx.Persist()
	})
	if err != nil {
		t.Fatalf("attach graph worktree: %v", err)
	}
	return wave
}

type fakeGraphWorktreeClient struct {
	scope                 publication.TrustedScope
	state                 string
	createCalls           int
	inspectCalls          int
	removeCalls           int
	findCalls             int
	allowRemove           bool
	createErrAfterPersist error
	noSetupCommand        bool
	byID                  map[string]worktreeops.Worktree
}

// setupState mirrors CompozyOS: a workspace without worktrees.setup_command
// leaves ready worktrees at "none"; the fake reports "ok" unless noSetup is set.
func (c *fakeGraphWorktreeClient) setupState() string {
	if c.state != "ready" {
		return "none"
	}
	if c.noSetupCommand {
		return "none"
	}
	return "ok"
}

func (c *fakeGraphWorktreeClient) Create(
	_ context.Context,
	scope publication.TrustedScope,
	request worktreeops.CreateRequest,
) (worktreeops.Worktree, error) {
	c.createCalls++
	if scope != c.scope {
		return worktreeops.Worktree{}, fmt.Errorf("scope mismatch: %#v", scope)
	}
	if c.byID == nil {
		c.byID = map[string]worktreeops.Worktree{}
	}
	id := "wt_" + request.Name
	worktree := worktreeops.Worktree{
		ID: id, Name: request.Name, Root: filepath.Join("/managed", request.Name),
		WorkspaceID: scope.WorkspaceID, RepositoryRoot: scope.WorkspaceRoot,
		RepositoryIdentity: digestValue("repo-" + scope.WorkspaceID), Branch: request.Branch,
		BaseRef: request.BaseSHA, BaseSHA: request.BaseSHA, State: c.state,
		Setup: worktreeops.SetupResult{State: c.setupState()},
	}
	c.byID[id] = worktree
	if c.createErrAfterPersist != nil {
		return worktreeops.Worktree{}, c.createErrAfterPersist
	}
	return worktree, nil
}

func (c *fakeGraphWorktreeClient) FindByName(
	_ context.Context,
	_ publication.TrustedScope,
	name string,
) (worktreeops.Worktree, bool, error) {
	c.findCalls++
	for _, worktree := range c.byID {
		if worktree.Name == name {
			return worktree, true, nil
		}
	}
	return worktreeops.Worktree{}, false, nil
}

func (c *fakeGraphWorktreeClient) Inspect(
	_ context.Context,
	_ publication.TrustedScope,
	worktreeID string,
) (worktreeops.Worktree, error) {
	c.inspectCalls++
	worktree, exists := c.byID[worktreeID]
	if !exists {
		return worktreeops.Worktree{}, worktreeops.ErrInvalidWorktreeIdentity
	}
	worktree.State = c.state
	worktree.Setup.State = map[string]string{"ready": "ok", "pending": "none"}[c.state]
	c.byID[worktreeID] = worktree
	return worktree, nil
}

func (c *fakeGraphWorktreeClient) Remove(
	_ context.Context,
	_ publication.TrustedScope,
	worktreeID string,
) (worktreeops.Worktree, error) {
	c.removeCalls++
	if !c.allowRemove {
		return worktreeops.Worktree{}, fmt.Errorf("unexpected remove")
	}
	worktree, exists := c.byID[worktreeID]
	if !exists {
		return worktreeops.Worktree{}, worktreeops.ErrInvalidWorktreeIdentity
	}
	worktree.State = "removed"
	c.byID[worktreeID] = worktree
	return worktree, nil
}

func TestDeliveryGraphOutputMarshalKeepsPrepareWaveTasksExplicit(t *testing.T) {
	t.Parallel()

	encoded, err := json.Marshal(DeliveryGraphOutput{Operation: GraphOpPrepareWave, Disposition: GraphDispositionAllIntegrated})
	if err != nil || !bytes.Contains(encoded, []byte(`"tasks":[]`)) {
		t.Fatalf("prepare_wave output = %s, error=%v", encoded, err)
	}
	encoded, err = json.Marshal(DeliveryGraphOutput{Operation: GraphOpSettleWave, Disposition: GraphDispositionAllIntegrated})
	if err != nil || bytes.Contains(encoded, []byte(`"tasks"`)) {
		t.Fatalf("settle_wave output = %s, error=%v", encoded, err)
	}
}

func TestDeliveryGraphServicePrepareWaveReportsAllIntegratedAfterFinalSettlement(t *testing.T) {
	t.Parallel()

	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	verification := []byte(`{"checks":["go test ./..."],"status":"passed","task_id":"task_01"}`)
	candidateSHA := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		_, err := delivery.Graph.RecordCandidate("task_01", 1, routing.TaskCandidate{
			ChildRunID: "run_task_01", BaseHeadSHA: wave.BaseHeadSHA, CommitSHA: candidateSHA,
			VerificationDigest: digestValue(string(verification)), TokensUsed: 900,
			Evidence: &routing.TaskCandidateEvidence{
				Slug: delivery.Slug, RepositoryIdentity: digestValue("repository"), Branch: "batuta/task/demo",
				TreeSHA: "1234512345123451234512345123451234512345", Verification: verification,
				OwnedTrackingPaths: []string{}, Tracking: []routing.TaskTrackingFile{},
			},
		})
		if err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist candidate: %v", err)
	}
	integratedSHA := "9876598765987659876598765987659876598765"
	service := deliveryGraphService{
		Store: fixture.store, Integrator: &fakeGraphIntegrator{integratedSHA: integratedSHA},
		Worktrees:       &fakeGraphWorktreeClient{scope: fixture.scope, state: "ready"},
		CommitReachable: func(context.Context, string, string, string) (bool, error) { return true, nil },
		Now:             func() time.Time { return fixture.now },
		WorktreeState: func(context.Context, string) (publication.WorktreeState, error) {
			return publication.WorktreeState{HeadSHA: integratedSHA, PorcelainSHA256: emptyDigest(), ContentSHA256: emptyDigest()}, nil
		},
	}
	settled, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpSettleWave, DeliveryID: fixture.deliveryID, Wave: wave.Number,
	})
	if err != nil || settled.Disposition != GraphDispositionAllIntegrated {
		t.Fatalf("settle_wave = %#v, error=%v", settled, err)
	}
	// The core loop continues into one more generation after the final
	// settlement; prepare_wave must re-report all_integrated there so
	// wave_route can own the publication path.
	prepared, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: fixture.deliveryID})
	if err != nil || prepared.Disposition != GraphDispositionAllIntegrated || len(prepared.Tasks) != 0 || prepared.BaseSHA != integratedSHA {
		t.Fatalf("prepare_wave after final settlement = %#v, error=%v", prepared, err)
	}
	replay, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{Operation: GraphOpPrepareWave, DeliveryID: fixture.deliveryID})
	if err != nil || !reflect.DeepEqual(replay, prepared) {
		t.Fatalf("prepare_wave replay = %#v, error=%v", replay, err)
	}
}

func TestDeliveryGraphServiceTerminalizesBlockedPublicationWithRecordedBlockers(t *testing.T) {
	t.Parallel()
	fixture := newDeliveryServiceFixture(t)
	taskRoot := t.TempDir()
	writeRoutingTask(t, taskRoot)
	wave := prepareGraphTaskForTest(t, fixture, taskRoot)
	verification := []byte(`{"checks":["go test ./..."],"status":"passed","task_id":"task_01"}`)
	candidateSHA := "abcdefabcdefabcdefabcdefabcdefabcdefabcd"
	if err := fixture.store.WithLockedJournal(fixture.scope.WorkspaceID, func(tx *routing.JournalTx) error {
		delivery := tx.Journal.Deliveries[fixture.deliveryID]
		_, err := delivery.Graph.RecordCandidate("task_01", 1, routing.TaskCandidate{
			ChildRunID: "run_task_01", BaseHeadSHA: wave.BaseHeadSHA, CommitSHA: candidateSHA,
			VerificationDigest: digestValue(string(verification)), TokensUsed: 900,
			Evidence: &routing.TaskCandidateEvidence{
				Slug: delivery.Slug, RepositoryIdentity: digestValue("repository"), Branch: "batuta/task/demo",
				TreeSHA: "1234512345123451234512345123451234512345", Verification: verification,
				OwnedTrackingPaths: []string{}, Tracking: []routing.TaskTrackingFile{},
			},
		})
		if err != nil {
			return err
		}
		tx.Journal.Deliveries[fixture.deliveryID] = delivery
		return tx.Persist()
	}); err != nil {
		t.Fatalf("persist candidate: %v", err)
	}
	integratedSHA := "9876598765987659876598765987659876598765"
	service := deliveryGraphService{
		Store: fixture.store, Integrator: &fakeGraphIntegrator{integratedSHA: integratedSHA},
		Now: func() time.Time { return fixture.now },
	}
	if settled, err := service.Execute(context.Background(), fixture.scope, DeliveryGraphInput{
		Operation: GraphOpSettleWave, DeliveryID: fixture.deliveryID, Wave: wave.Number,
	}); err != nil || settled.Disposition != GraphDispositionAllIntegrated {
		t.Fatalf("settle_wave = %#v, error=%v", settled, err)
	}
	// Every task integrated and nothing else blocked: without publication
	// evidence a blocked terminal disposition is unproven.
	unproven := DeliveryGraphInput{Operation: GraphOpTerminalize, DeliveryID: fixture.deliveryID, TerminalDisposition: GraphDispositionBlocked}
	if _, err := service.Execute(context.Background(), fixture.scope, unproven); !errors.Is(err, routing.ErrDeliveryConflict) {
		t.Fatalf("Execute(terminalize without blockers) error = %v, want delivery conflict", err)
	}
	input := DeliveryGraphInput{
		Operation: GraphOpTerminalize, DeliveryID: fixture.deliveryID, TerminalDisposition: GraphDispositionBlocked,
		PublicationBlockers: []string{"remote_missing"},
	}
	if _, err := service.Execute(context.Background(), fixture.scope, input); !errors.Is(err, errDeliveryGraphTerminalized) {
		t.Fatalf("Execute(terminalize blocked publication) error = %v, want stable terminalizer error", err)
	}
	journal, _, err := fixture.store.Load(fixture.scope.WorkspaceID)
	delivery := journal.Deliveries[fixture.deliveryID]
	if err != nil || delivery.State != routing.DeliveryStateBlocked || !reflect.DeepEqual(delivery.Graph.PublicationBlockers, []string{"remote_missing"}) {
		t.Fatalf("terminalized delivery = %#v, error=%v", delivery, err)
	}
	before := journal
	if _, err := service.Execute(context.Background(), fixture.scope, input); !errors.Is(err, errDeliveryGraphTerminalized) {
		t.Fatalf("Execute(terminalize replay) error = %v", err)
	}
	after, _, _ := fixture.store.Load(fixture.scope.WorkspaceID)
	if !reflect.DeepEqual(after, before) {
		t.Fatalf("terminalize replay mutated journal")
	}
	input.PublicationBlockers = []string{"forge_unavailable"}
	if _, err := service.Execute(context.Background(), fixture.scope, input); !errors.Is(err, routing.ErrDeliveryConflict) {
		t.Fatalf("Execute(terminalize with different blockers) error = %v, want delivery conflict", err)
	}
}
