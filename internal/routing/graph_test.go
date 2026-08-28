package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func TestDeliveryGraphRejectsInvalidDAGAndMetadata(t *testing.T) {
	t.Parallel()

	valid := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "pending", DomainBackend, ComplexityLow),
		graphTaskArtifact("task_02", "pending", DomainFrontend, ComplexityHigh, "task_01"),
	})
	tests := []struct {
		name   string
		mutate func(*DeliveryTaskSnapshot)
	}{
		{name: "duplicate task", mutate: func(snapshot *DeliveryTaskSnapshot) {
			snapshot.Tasks[1].ID = "task_01"
			snapshot.ItemTaskIDs[1] = "task_01"
		}},
		{name: "missing dependency", mutate: func(snapshot *DeliveryTaskSnapshot) {
			snapshot.Tasks[1].Dependencies = []string{"task_99"}
		}},
		{name: "self dependency", mutate: func(snapshot *DeliveryTaskSnapshot) {
			snapshot.Tasks[0].Dependencies = []string{"task_01"}
		}},
		{name: "cycle", mutate: func(snapshot *DeliveryTaskSnapshot) {
			snapshot.Tasks[0].Dependencies = []string{"task_02"}
			snapshot.Tasks[1].Dependencies = []string{"task_01"}
		}},
		{name: "noncanonical domain", mutate: func(snapshot *DeliveryTaskSnapshot) {
			snapshot.Tasks[0].Domain = Domain("api")
		}},
		{name: "noncanonical complexity", mutate: func(snapshot *DeliveryTaskSnapshot) {
			snapshot.Tasks[0].Complexity = Complexity("large")
		}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			snapshot := cloneTaskSnapshotForTest(valid)
			test.mutate(&snapshot)
			redigestGraphSnapshot(t, &snapshot)
			if _, err := NewDeliveryGraph(snapshot, RoutingGeneration{}, graphGitSHA("base")); !errors.Is(err, ErrInvalidDeliveryGraph) {
				t.Fatalf("NewDeliveryGraph() error = %v, want ErrInvalidDeliveryGraph", err)
			}
		})
	}

	tasks := make([]TaskArtifact, MaxDeliveryTasks+1)
	for index := range tasks {
		tasks[index] = graphTaskArtifact(fmt.Sprintf("task_%03d", index+1), "pending", DomainGeneral, ComplexityLow)
	}
	tooLarge := graphSnapshotFixture(t, tasks)
	if _, err := NewDeliveryGraph(tooLarge, graphGenerationFixture(t, tooLarge), graphGitSHA("base")); !errors.Is(err, ErrInvalidDeliveryGraph) {
		t.Fatalf("NewDeliveryGraph(%d tasks) error = %v, want ErrInvalidDeliveryGraph", len(tasks), err)
	}
}

func TestDeliveryGraphRejectsGenerationDigestDrift(t *testing.T) {
	t.Parallel()

	snapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "pending", DomainBackend, ComplexityLow),
	})
	generation := graphGenerationFixture(t, snapshot)
	generation.PolicyVersion = "forged-policy"

	if _, err := NewDeliveryGraph(snapshot, generation, graphGitSHA("base")); !errors.Is(err, ErrInvalidDeliveryGraph) {
		t.Fatalf("NewDeliveryGraph() error = %v, want ErrInvalidDeliveryGraph", err)
	}
}

func TestDeliveryGraphKeepsCompletedTasksAndClonesAuthoredInput(t *testing.T) {
	t.Parallel()

	snapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "completed", DomainBackend, ComplexityLow),
		graphTaskArtifact("task_02", "pending", DomainFrontend, ComplexityHigh, "task_01"),
	})
	generation := graphGenerationFixture(t, snapshot)
	base := graphGitSHA("base")
	graph, err := NewDeliveryGraph(snapshot, generation, base)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	if len(graph.Tasks) != 2 || graph.Tasks[0].State != GraphTaskIntegrated ||
		graph.Tasks[0].IntegratedCommitSHA != base || len(graph.Tasks[0].Attempts) != 0 ||
		graph.Tasks[1].State != GraphTaskPending {
		t.Fatalf("graph tasks = %#v", graph.Tasks)
	}

	snapshot.Tasks[1].Dependencies[0] = "task_mutated"
	generation.Tasks[0].ID = "task_mutated"
	if !slices.Equal(graph.Tasks[1].Dependencies, []string{"task_01"}) || graph.Tasks[0].TaskID != "task_01" {
		t.Fatalf("graph aliases authored input: %#v", graph.Tasks)
	}
}

func TestReadyWaveRequiresReachableIntegratedDependenciesAndClampsSlots(t *testing.T) {
	t.Parallel()

	snapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "completed", DomainBackend, ComplexityLow),
		graphTaskArtifact("task_02", "pending", DomainFrontend, ComplexityHigh, "task_01"),
		graphTaskArtifact("task_03", "pending", DomainTesting, ComplexityMedium),
		graphTaskArtifact("task_04", "pending", DomainDocs, ComplexityLow),
		graphTaskArtifact("task_05", "pending", DomainInfra, ComplexityHigh),
	})
	base := graphGitSHA("base")
	graph, err := NewDeliveryGraph(snapshot, graphGenerationFixture(t, snapshot), base)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}

	wave, err := graph.AdmitReadyWave(ReadyWaveInput{
		IntegrationHeadSHA: base,
		RemainingSlots:     2,
		ReachableCommits:   map[string]bool{base: true},
	})
	if err != nil {
		t.Fatalf("AdmitReadyWave(first) error = %v", err)
	}
	if wave.Number != 1 || wave.BaseHeadSHA != base || !slices.Equal(wave.TaskIDs, []string{"task_02", "task_03"}) {
		t.Fatalf("first wave = %#v", wave)
	}
	wave.TaskIDs[0] = "mutated"
	if graph.Waves[0].TaskIDs[0] != "task_02" {
		t.Fatalf("stored wave aliases result: %#v", graph.Waves[0])
	}

	second, err := graph.AdmitReadyWave(ReadyWaveInput{
		IntegrationHeadSHA: base,
		RemainingSlots:     MaxParallelTasks,
		ReachableCommits:   map[string]bool{base: true},
	})
	if err != nil {
		t.Fatalf("AdmitReadyWave(second) error = %v", err)
	}
	if !slices.Equal(second.TaskIDs, []string{"task_04", "task_05"}) {
		t.Fatalf("second wave task IDs = %#v, want remaining two slots", second.TaskIDs)
	}
	if activeGraphTaskCount(graph.Tasks) != MaxParallelTasks {
		t.Fatalf("active tasks = %d, want %d", activeGraphTaskCount(graph.Tasks), MaxParallelTasks)
	}
}

func TestReadyWaveSkipsUnreachableDependencyAndReportsDependencyBlocked(t *testing.T) {
	t.Parallel()

	snapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "completed", DomainBackend, ComplexityLow),
		graphTaskArtifact("task_02", "pending", DomainFrontend, ComplexityHigh, "task_01"),
		graphTaskArtifact("task_03", "pending", DomainDocs, ComplexityLow),
	})
	base := graphGitSHA("base")
	graph, err := NewDeliveryGraph(snapshot, graphGenerationFixture(t, snapshot), base)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	wave, err := graph.AdmitReadyWave(ReadyWaveInput{
		IntegrationHeadSHA: base,
		RemainingSlots:     MaxParallelTasks,
		ReachableCommits:   map[string]bool{},
	})
	if err != nil || !slices.Equal(wave.TaskIDs, []string{"task_03"}) {
		t.Fatalf("AdmitReadyWave(unreachable) = %#v, error %v", wave, err)
	}

	blockedSnapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "pending", DomainBackend, ComplexityLow),
		graphTaskArtifact("task_02", "pending", DomainFrontend, ComplexityHigh, "task_01"),
	})
	blocked, err := NewDeliveryGraph(blockedSnapshot, graphGenerationFixture(t, blockedSnapshot), base)
	if err != nil {
		t.Fatalf("NewDeliveryGraph(blocked) error = %v", err)
	}
	blocked.Tasks[0].State = GraphTaskBlocked
	blocked.Tasks[0].BlockerCode = "implementation_failed"
	if _, err := blocked.AdmitReadyWave(ReadyWaveInput{
		IntegrationHeadSHA: base,
		RemainingSlots:     MaxParallelTasks,
		ReachableCommits:   map[string]bool{},
	}); !errors.Is(err, ErrDependencyBlocked) {
		t.Fatalf("AdmitReadyWave(blocked) error = %v, want ErrDependencyBlocked", err)
	}
}

func TestDeliveryGraphBeginsWaveAttemptsAndAttachesWorktreesIdempotently(t *testing.T) {
	t.Parallel()

	snapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "pending", DomainBackend, ComplexityLow),
		graphTaskArtifact("task_02", "pending", DomainFrontend, ComplexityHigh),
	})
	generation := graphGenerationFixture(t, snapshot)
	base := graphGitSHA("base")
	graph, err := NewDeliveryGraph(snapshot, generation, base)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	wave, err := graph.AdmitReadyWave(ReadyWaveInput{
		IntegrationHeadSHA: base, RemainingSlots: 2, ReachableCommits: map[string]bool{},
	})
	if err != nil {
		t.Fatalf("AdmitReadyWave() error = %v", err)
	}
	if err := graph.BeginWaveAttempts(wave.Number, generation); err != nil {
		t.Fatalf("BeginWaveAttempts() error = %v", err)
	}
	first, exists := graph.Task("task_01")
	if !exists || len(first.Attempts) != 1 || first.Attempts[0].Execution != 1 ||
		first.Attempts[0].Runtime != generation.Rules[0].Runtime || first.Attempts[0].BaseHeadSHA != base {
		t.Fatalf("first task = %#v, exists %v", first, exists)
	}
	pending := GraphWorktree{ID: "wt_task_01", Root: "/managed/task-01", Ready: false}
	if replay, err := graph.AttachWorktree("task_01", 1, pending); err != nil || replay {
		t.Fatalf("AttachWorktree(pending) replay=%v error=%v", replay, err)
	}
	if replay, err := graph.AttachWorktree("task_01", 1, pending); err != nil || !replay {
		t.Fatalf("AttachWorktree(pending replay) replay=%v error=%v", replay, err)
	}
	ready := pending
	ready.Ready = true
	if replay, err := graph.AttachWorktree("task_01", 1, ready); err != nil || replay {
		t.Fatalf("AttachWorktree(ready) replay=%v error=%v", replay, err)
	}
	first, _ = graph.Task("task_01")
	if first.State != GraphTaskRunning || first.Attempts[0].State != GraphTaskRunning ||
		first.Attempts[0].WorktreeID != pending.ID || first.Attempts[0].WorktreeRoot != pending.Root {
		t.Fatalf("ready first task = %#v", first)
	}
	foreign := ready
	foreign.ID = "wt_foreign"
	if _, err := graph.AttachWorktree("task_01", 1, foreign); !errors.Is(err, ErrInvalidDeliveryTransition) {
		t.Fatalf("AttachWorktree(foreign) error=%v, want ErrInvalidDeliveryTransition", err)
	}
}

func TestDeliveryGraphBeginWaveAttemptsReplaysExactGeneration(t *testing.T) {
	t.Parallel()

	snapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "pending", DomainBackend, ComplexityLow),
		graphTaskArtifact("task_02", "pending", DomainFrontend, ComplexityHigh),
	})
	generation := graphGenerationFixture(t, snapshot)
	base := graphGitSHA("replay-base")
	graph, err := NewDeliveryGraph(snapshot, generation, base)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	wave, err := graph.AdmitReadyWave(ReadyWaveInput{
		IntegrationHeadSHA: base, RemainingSlots: 2, ReachableCommits: map[string]bool{},
	})
	if err != nil {
		t.Fatalf("AdmitReadyWave() error = %v", err)
	}
	if err := graph.BeginWaveAttempts(wave.Number, generation); err != nil {
		t.Fatalf("BeginWaveAttempts(first) error = %v", err)
	}
	if err := graph.ReserveWaveTokens(wave.Number, 200); err != nil {
		t.Fatalf("ReserveWaveTokens() error = %v", err)
	}
	want := cloneDeliveryGraph(graph)

	if err := graph.BeginWaveAttempts(wave.Number, generation); err != nil {
		t.Fatalf("BeginWaveAttempts(replay) error = %v", err)
	}
	if !reflect.DeepEqual(graph, want) {
		t.Fatalf("BeginWaveAttempts(replay) graph = %#v, want unchanged %#v", graph, want)
	}
}

func TestDeliveryGraphRejectsWaveWithoutTaskAdmission(t *testing.T) {
	t.Parallel()

	snapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "pending", DomainBackend, ComplexityLow),
	})
	generation := graphGenerationFixture(t, snapshot)
	base := graphGitSHA("base")
	graph, err := NewDeliveryGraph(snapshot, generation, base)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	graph.Waves = append(graph.Waves, DeliveryWave{Number: 1, BaseHeadSHA: base, TaskIDs: []string{"task_01"}})
	if err := validateDeliveryGraph(
		graph,
		snapshot,
		generation,
		time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC),
		base,
	); !errors.Is(err, ErrInvalidDeliveryGraph) {
		t.Fatalf("validateDeliveryGraph(wave without admission) error = %v, want ErrInvalidDeliveryGraph", err)
	}
}

func TestActiveWallExcludesOnlyDurableHumanPauses(t *testing.T) {
	t.Parallel()

	record := validDeliveryFixture(t)
	record.Attempts = nil
	record.Graph = graphForDeliveryFixture(t, record, 1)
	created := record.CreatedAt
	if remaining, err := record.RemainingActiveWall(created.Add(time.Hour)); err != nil || remaining != 3*time.Hour {
		t.Fatalf("RemainingActiveWall(no pause) = %s, error %v", remaining, err)
	}

	pause := HumanPause{
		TaskID: "task_1", Execution: 1, RequestID: "request-1",
		StartedAt: created.Add(30 * time.Minute),
	}
	opened, replay, err := record.Graph.OpenPause(pause)
	if err != nil || replay || opened.RequestID != pause.RequestID {
		t.Fatalf("OpenPause() = %#v, replay %v, error %v", opened, replay, err)
	}
	if _, replay, err := record.Graph.OpenPause(pause); err != nil || !replay || len(record.Graph.Pauses) != 1 {
		t.Fatalf("OpenPause(replay) replay = %v, pauses %d, error %v", replay, len(record.Graph.Pauses), err)
	}
	if remaining, err := record.RemainingActiveWall(created.Add(2 * time.Hour)); err != nil || remaining != 3*time.Hour+30*time.Minute {
		t.Fatalf("RemainingActiveWall(open pause) = %s, error %v", remaining, err)
	}

	closed, replay, err := record.Graph.ClosePause("request-1", created.Add(90*time.Minute))
	if err != nil || replay || closed.EndedAt == nil {
		t.Fatalf("ClosePause() = %#v, replay %v, error %v", closed, replay, err)
	}
	if _, replay, err := record.Graph.ClosePause("request-1", created.Add(90*time.Minute)); err != nil || !replay {
		t.Fatalf("ClosePause(replay) replay = %v, error %v", replay, err)
	}
	if remaining, err := record.RemainingActiveWall(created.Add(2 * time.Hour)); err != nil || remaining != 3*time.Hour {
		t.Fatalf("RemainingActiveWall(closed pause) = %s, error %v", remaining, err)
	}
}

func TestActiveWallRejectsContradictoryPauseEvidence(t *testing.T) {
	t.Parallel()

	record := validDeliveryFixture(t)
	record.Attempts = nil
	record.Graph = graphForDeliveryFixture(t, record, 1)
	created := record.CreatedAt
	tests := []struct {
		name   string
		pauses []HumanPause
		now    time.Time
	}{
		{name: "future start", pauses: []HumanPause{{TaskID: "task_1", Execution: 1, RequestID: "request-1", StartedAt: created.Add(2 * time.Hour)}}, now: created.Add(time.Hour)},
		{name: "end before start", pauses: []HumanPause{{TaskID: "task_1", Execution: 1, RequestID: "request-1", StartedAt: created.Add(time.Hour), EndedAt: graphTime(created.Add(30 * time.Minute))}}, now: created.Add(2 * time.Hour)},
		{name: "overlap", pauses: []HumanPause{
			{TaskID: "task_1", Execution: 1, RequestID: "request-1", StartedAt: created.Add(10 * time.Minute), EndedAt: graphTime(created.Add(time.Hour))},
			{TaskID: "task_1", Execution: 1, RequestID: "request-2", StartedAt: created.Add(50 * time.Minute), EndedAt: graphTime(created.Add(70 * time.Minute))},
		}, now: created.Add(2 * time.Hour)},
		{name: "missing identity", pauses: []HumanPause{{TaskID: "task_1", Execution: 1, StartedAt: created.Add(time.Minute)}}, now: created.Add(time.Hour)},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			copy := record
			graph := cloneDeliveryGraph(record.Graph)
			graph.Pauses = test.pauses
			copy.Graph = graph
			if _, err := copy.RemainingActiveWall(test.now); !errors.Is(err, ErrInvalidActiveWall) {
				t.Fatalf("RemainingActiveWall() error = %v, want ErrInvalidActiveWall", err)
			}
		})
	}
}

func TestDeliveryGraphQuestionStoresOnlyBoundedRedactedContextEvidence(t *testing.T) {
	t.Parallel()

	valid := &TaskQuestion{
		RequestID: "request-1", Prompt: "Choose the public behavior",
		ContextDigest: digestFixture("redacted-context"), Choices: []string{"Keep compatibility", "Break compatibility"},
	}
	if !validTaskQuestion(valid) {
		t.Fatalf("validTaskQuestion(valid) = false: %#v", valid)
	}
	for _, test := range []struct {
		name   string
		mutate func(*TaskQuestion)
	}{
		{name: "unbounded prompt", mutate: func(question *TaskQuestion) { question.Prompt = strings.Repeat("x", maxQuestionBytes+1) }},
		{name: "raw context instead of digest", mutate: func(question *TaskQuestion) { question.ContextDigest = "api_key=secret" }},
		{name: "too many choices", mutate: func(question *TaskQuestion) { question.Choices = []string{"a", "b", "c", "d", "e"} }},
		{name: "unbounded choice", mutate: func(question *TaskQuestion) { question.Choices = []string{strings.Repeat("x", maxChoiceBytes+1)} }},
		{name: "github token", mutate: func(question *TaskQuestion) { question.Prompt = "Use ghp_0123456789abcdefghijklmnopqrstuv" }},
		{name: "aws secret", mutate: func(question *TaskQuestion) { question.Prompt = "AWS_SECRET_ACCESS_KEY = super-secret-token" }},
		{name: "single absolute path", mutate: func(question *TaskQuestion) { question.Prompt = "Inspect /etc" }},
		{name: "ssh relative path", mutate: func(question *TaskQuestion) { question.Prompt = "Inspect .ssh/id_rsa" }},
	} {
		t.Run(test.name, func(t *testing.T) {
			question := *valid
			question.Choices = append([]string(nil), valid.Choices...)
			test.mutate(&question)
			if validTaskQuestion(&question) {
				t.Fatalf("validTaskQuestion(%s) = true: %#v", test.name, question)
			}
		})
	}
}

func TestDeliveryGraphRecordsQuestionAndAnswerAsOneConsumedExecution(t *testing.T) {
	t.Parallel()

	record := validDeliveryFixture(t)
	record.Attempts = nil
	generation := validGenerationFixture(t)
	graph, err := NewDeliveryGraph(record.TaskSnapshot, generation, record.InitialWorktreeFingerprint.HeadSHA)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	wave, err := graph.AdmitReadyWave(ReadyWaveInput{
		IntegrationHeadSHA: record.InitialWorktreeFingerprint.HeadSHA,
		RemainingSlots:     1,
		ReachableCommits:   map[string]bool{},
	})
	if err != nil {
		t.Fatalf("AdmitReadyWave() error = %v", err)
	}
	if err := graph.BeginWaveAttempts(wave.Number, generation); err != nil {
		t.Fatalf("BeginWaveAttempts() error = %v", err)
	}
	if _, err := graph.AttachWorktree("task_1", 1, GraphWorktree{
		ID: "wt-task-1", Root: "/managed/task-1", Ready: true,
	}); err != nil {
		t.Fatalf("AttachWorktree() error = %v", err)
	}

	question := TaskQuestion{
		RequestID:     "sha256:8f3aa128644d1881832f6b9d4fcc63d704627715d80cb077dc77a29f9ebdc750",
		Prompt:        "Which public behavior should task_1 implement?",
		ContextDigest: "sha256:74bad2ae825e22fc7be89dad88e81cd9d68d80f8d0e9465dbed4b90992f12d99",
		Choices:       []string{"Preserve compatibility", "Adopt the new contract"},
	}
	startedAt := record.CreatedAt.Add(10 * time.Minute)
	if replay, err := graph.RecordQuestion("task_1", 1, "loop-task-1", question, startedAt); err != nil || replay {
		t.Fatalf("RecordQuestion() replay=%v error=%v", replay, err)
	}
	if replay, err := graph.RecordQuestion("task_1", 1, "loop-task-1", question, startedAt); err != nil || !replay {
		t.Fatalf("RecordQuestion(replay) replay=%v error=%v", replay, err)
	}

	answer := TaskAnswer{
		QuestionOperationID: question.RequestID,
		LoopRunID:           "loop-task-1", Generation: 2, NodeID: "ask_operator", ItemIndex: 0,
		Value: "Preserve compatibility",
	}
	endedAt := record.CreatedAt.Add(20 * time.Minute)
	nextExecution, replay, err := graph.RecordAnswer("task_1", 1, answer, endedAt)
	if err != nil || replay || nextExecution != 2 {
		t.Fatalf("RecordAnswer() next=%d replay=%v error=%v", nextExecution, replay, err)
	}
	nextExecution, replay, err = graph.RecordAnswer("task_1", 1, answer, endedAt)
	if err != nil || !replay || nextExecution != 2 {
		t.Fatalf("RecordAnswer(replay) next=%d replay=%v error=%v", nextExecution, replay, err)
	}

	task, exists := graph.Task("task_1")
	if !exists || task.State != GraphTaskRunning || len(task.Attempts) != 2 {
		t.Fatalf("task after answer = %#v, exists=%v", task, exists)
	}
	first, second := task.Attempts[0], task.Attempts[1]
	if first.State != GraphTaskRunning || first.Question == nil || first.Question.Answer == nil ||
		!reflect.DeepEqual(*first.Question.Answer, answer) {
		t.Fatalf("answered attempt = %#v", first)
	}
	if second.Execution != 2 || second.State != GraphTaskRunning || second.Runtime != first.Runtime ||
		second.RunExecution != 1 ||
		second.BaseHeadSHA != first.BaseHeadSHA || second.WorktreeID != first.WorktreeID ||
		second.WorktreeRoot != first.WorktreeRoot || second.ChildRunID != first.ChildRunID || second.Question != nil {
		t.Fatalf("continuation attempt = %#v, prior=%#v", second, first)
	}
	if len(graph.Pauses) != 1 || graph.Pauses[0].EndedAt == nil || !graph.Pauses[0].EndedAt.Equal(endedAt) {
		t.Fatalf("pause archive = %#v", graph.Pauses)
	}
	third := second
	third.Execution = 3
	third.RunExecution = 3
	graph.Tasks[0].Attempts = append(graph.Tasks[0].Attempts, third)
	nextExecution, replay, err = graph.RecordAnswer("task_1", 1, answer, endedAt)
	if err != nil || !replay || nextExecution != 2 {
		t.Fatalf("RecordAnswer(historical replay) next=%d replay=%v error=%v", nextExecution, replay, err)
	}
}

func TestDeliveryGraphSuspendsWallOnlyWhenEveryActiveTaskWaitsForHuman(t *testing.T) {
	t.Parallel()

	snapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "pending", DomainBackend, ComplexityLow),
		graphTaskArtifact("task_02", "pending", DomainFrontend, ComplexityLow),
	})
	generation := graphGenerationFixture(t, snapshot)
	base := graphGitSHA("parallel-question-base")
	graph, err := NewDeliveryGraph(snapshot, generation, base)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	wave, err := graph.AdmitReadyWave(ReadyWaveInput{IntegrationHeadSHA: base, RemainingSlots: 2, ReachableCommits: map[string]bool{}})
	if err != nil || len(wave.TaskIDs) != 2 {
		t.Fatalf("AdmitReadyWave() = %#v, error=%v", wave, err)
	}
	if err := graph.BeginWaveAttempts(wave.Number, generation); err != nil {
		t.Fatalf("BeginWaveAttempts() error = %v", err)
	}
	for _, taskID := range wave.TaskIDs {
		if _, err := graph.AttachWorktree(taskID, 1, GraphWorktree{ID: "wt-" + taskID, Root: "/managed/" + taskID, Ready: true}); err != nil {
			t.Fatalf("AttachWorktree(%s) error = %v", taskID, err)
		}
	}
	startedAt := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	first := TaskQuestion{RequestID: digestFixture("request-1"), Prompt: "Choose task one behavior", ContextDigest: digestFixture("question-1")}
	if _, err := graph.RecordQuestion("task_01", 1, "run-task-01", first, startedAt); err != nil || len(graph.Pauses) != 0 {
		t.Fatalf("first active-sibling question pauses=%#v error=%v", graph.Pauses, err)
	}
	second := TaskQuestion{RequestID: digestFixture("request-2"), Prompt: "Choose task two behavior", ContextDigest: digestFixture("question-2")}
	if _, err := graph.RecordQuestion("task_02", 1, "run-task-02", second, startedAt.Add(time.Minute)); err != nil ||
		len(graph.Pauses) != 1 || graph.Pauses[0].RequestID != second.RequestID || graph.Pauses[0].EndedAt != nil {
		t.Fatalf("fully parked questions pauses=%#v error=%v", graph.Pauses, err)
	}
	answer := TaskAnswer{QuestionOperationID: first.RequestID, LoopRunID: "run-task-01", Generation: 2, NodeID: "ask_operator", Value: "Keep compatibility"}
	if _, _, err := graph.RecordAnswer("task_01", 1, answer, startedAt.Add(2*time.Minute)); err != nil || graph.Pauses[0].EndedAt == nil {
		t.Fatalf("answer resumes global wall pauses=%#v error=%v", graph.Pauses, err)
	}
}

func TestDeliveryGraphRejectsQuestionOnFourthExecution(t *testing.T) {
	t.Parallel()

	record := validDeliveryFixture(t)
	record.Attempts = nil
	record.Graph = graphForDeliveryFixture(t, record, 1)
	task := &record.Graph.Tasks[0]
	task.State = GraphTaskRunning
	base := task.Attempts[0]
	base.State = GraphTaskRunning
	base.Question = nil
	task.Attempts = []GraphTaskAttempt{base, base, base, base}
	for index := range task.Attempts {
		task.Attempts[index].Execution = index + 1
	}

	question := TaskQuestion{
		RequestID:     "sha256:8f3aa128644d1881832f6b9d4fcc63d704627715d80cb077dc77a29f9ebdc750",
		Prompt:        "Choose the final behavior",
		ContextDigest: "sha256:74bad2ae825e22fc7be89dad88e81cd9d68d80f8d0e9465dbed4b90992f12d99",
	}
	if _, err := record.Graph.RecordQuestion("task_1", 4, "loop-task-4", question, record.CreatedAt.Add(time.Minute)); !errors.Is(err, ErrInvalidDeliveryTransition) {
		t.Fatalf("RecordQuestion(fourth execution) error = %v, want ErrInvalidDeliveryTransition", err)
	}
}

func TestDeliveryGraphRecordsFailureOnceAndAdvancesOnlyFailedTaskRuntime(t *testing.T) {
	t.Parallel()

	record := validDeliveryFixture(t)
	record.Attempts = nil
	generation := validGenerationFixture(t)
	selected := generation.Rules[0].Runtime
	generation.Cells = []RoutingCell{{
		Domain: DomainFrontend, Complexity: ComplexityHigh, TaskIDs: []string{"task_1"},
		Selected: RuntimeCandidate{ProviderID: selected.Provider, ModelID: selected.Model, Reasoning: selected.Reasoning},
		Fallbacks: []RuntimeCandidate{
			{ProviderID: "codex", ModelID: "gpt-5.6-terra", Reasoning: "high"},
			{ProviderID: "codex", ModelID: "gpt-5.6-sol", Reasoning: "high"},
		},
		FallbackLimit: 2,
	}}
	generation, _ = finalizeGeneration(generation)
	graph, err := NewDeliveryGraph(record.TaskSnapshot, generation, record.InitialWorktreeFingerprint.HeadSHA)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	wave, err := graph.AdmitReadyWave(ReadyWaveInput{
		IntegrationHeadSHA: record.InitialWorktreeFingerprint.HeadSHA,
		RemainingSlots:     1, ReachableCommits: map[string]bool{},
	})
	if err != nil {
		t.Fatalf("AdmitReadyWave() error = %v", err)
	}
	if err := graph.BeginWaveAttempts(wave.Number, generation); err != nil {
		t.Fatalf("BeginWaveAttempts() error = %v", err)
	}
	if _, err := graph.AttachWorktree("task_1", 1, GraphWorktree{ID: "wt-task-1", Root: "/managed/task-1", Ready: true}); err != nil {
		t.Fatalf("AttachWorktree() error = %v", err)
	}
	nextBase := graphGitSHA("integration-after-siblings")
	failure := TaskFailure{
		ChildRunID: "loop-task-1", TerminalStatus: "failed", BlockerCode: "implementation_failed", TokensUsed: 1200,
	}

	result, err := graph.RecordFailure("task_1", 1, failure, generation, nextBase, true)
	if err != nil || result.Replayed || result.Blocked || result.Wave.Number != 2 ||
		result.Runtime != (RuntimeValue{Provider: "codex", Model: "gpt-5.6-terra", Reasoning: "high"}) {
		t.Fatalf("RecordFailure() = %#v, error=%v", result, err)
	}
	replay, err := graph.RecordFailure("task_1", 1, failure, generation, nextBase, true)
	if err != nil || !replay.Replayed || !reflect.DeepEqual(replay, TaskFailureResult{
		Replayed: true, Wave: result.Wave, Runtime: result.Runtime,
	}) {
		t.Fatalf("RecordFailure(replay) = %#v, error=%v", replay, err)
	}
	task, _ := graph.Task("task_1")
	if task.State != GraphTaskPreparing || len(task.Attempts) != 2 || task.Attempts[0].State != GraphTaskBlocked ||
		task.Attempts[0].TokensUsed == nil || *task.Attempts[0].TokensUsed != 1200 ||
		task.Attempts[0].TerminalStatus != "failed" || task.Attempts[0].BlockerCode != "implementation_failed" ||
		task.Attempts[1].Execution != 2 || task.Attempts[1].State != GraphTaskPreparing ||
		task.Attempts[1].Runtime != result.Runtime || task.Attempts[1].BaseHeadSHA != nextBase {
		t.Fatalf("task after fallback = %#v", task)
	}
	if _, err := graph.AttachWorktree("task_1", 2, GraphWorktree{ID: "wt-task-2", Root: "/managed/task-2", Ready: true}); err != nil {
		t.Fatalf("AttachWorktree(second attempt) error = %v", err)
	}
	secondFailure := TaskFailure{ChildRunID: "loop-task-2", TerminalStatus: "failed", BlockerCode: "implementation_failed", TokensUsed: 100}
	if secondResult, err := graph.RecordFailure("task_1", 2, secondFailure, generation, graphGitSHA("third-base"), true); err != nil || secondResult.Wave.Number != 3 {
		t.Fatalf("RecordFailure(second attempt) = %#v, error=%v", secondResult, err)
	}
	replay, err = graph.RecordFailure("task_1", 1, failure, generation, graphGitSHA("later-integration-head"), true)
	if err != nil || !replay.Replayed || replay.Wave.Number != result.Wave.Number || replay.Runtime != result.Runtime {
		t.Fatalf("RecordFailure(historical replay) = %#v, error=%v", replay, err)
	}
}

func TestDeliveryGraphReconcilesRepeatedGlobalPauseIntervals(t *testing.T) {
	t.Parallel()

	snapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "pending", DomainBackend, ComplexityLow),
		graphTaskArtifact("task_02", "pending", DomainFrontend, ComplexityLow),
	})
	generation := graphGenerationFixture(t, snapshot)
	graph, err := NewDeliveryGraph(snapshot, generation, graphGitSHA("pause-base"))
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	wave, err := graph.AdmitReadyWave(ReadyWaveInput{
		IntegrationHeadSHA: graphGitSHA("pause-base"), RemainingSlots: 2, ReachableCommits: map[string]bool{},
	})
	if err != nil || len(wave.TaskIDs) != 2 {
		t.Fatalf("AdmitReadyWave() = %#v, error=%v", wave, err)
	}
	if err := graph.BeginWaveAttempts(wave.Number, generation); err != nil {
		t.Fatalf("BeginWaveAttempts() error = %v", err)
	}
	for _, taskID := range wave.TaskIDs {
		if _, err := graph.AttachWorktree(taskID, 1, GraphWorktree{
			ID: "wt-" + taskID, Root: "/managed/" + taskID, Ready: true,
		}); err != nil {
			t.Fatalf("AttachWorktree(%s) error = %v", taskID, err)
		}
	}
	started := time.Date(2026, time.August, 27, 15, 0, 0, 0, time.UTC)
	requestID := func(taskID string) string { return digestFixture("request-" + taskID) }
	question := func(taskID string) TaskQuestion {
		return TaskQuestion{RequestID: requestID(taskID), Prompt: "Choose behavior for " + taskID, ContextDigest: digestFixture(taskID)}
	}
	if replay, err := graph.RecordQuestion("task_01", 1, "run-task_01", question("task_01"), started); err != nil || replay {
		t.Fatalf("RecordQuestion(task_01) replay=%v error=%v", replay, err)
	}
	if len(graph.Pauses) != 0 {
		t.Fatalf("pause opened while sibling active: %#v", graph.Pauses)
	}
	if replay, err := graph.RecordQuestion("task_02", 1, "run-task_02", question("task_02"), started.Add(time.Second)); err != nil || replay {
		t.Fatalf("RecordQuestion(task_02) replay=%v error=%v", replay, err)
	}
	if len(graph.Pauses) != 1 || graph.Pauses[0].EndedAt != nil {
		t.Fatalf("first global pause = %#v", graph.Pauses)
	}
	probe := cloneDeliveryGraph(graph)
	if _, replay, err := probe.ClosePause(graph.Pauses[0].RequestID, started.Add(2*time.Second)); err != nil || replay {
		t.Fatalf("ClosePause(probe) replay=%v error=%v pause=%#v", replay, err, graph.Pauses[0])
	}
	if _, _, err := graph.RecordAnswer("task_01", 1, TaskAnswer{
		QuestionOperationID: requestID("task_01"), LoopRunID: "run-task_01", Generation: 1,
		NodeID: "ask", ItemIndex: 0, Value: "continue",
	}, started.Add(2*time.Second)); err != nil {
		t.Fatalf("RecordAnswer(task_01) error = %v", err)
	}
	if graph.Pauses[0].EndedAt == nil {
		t.Fatalf("first pause not closed: %#v", graph.Pauses)
	}
	if changed, err := graph.ReconcileHumanPause(started.Add(3 * time.Second)); err != nil || changed {
		t.Fatalf("ReconcileHumanPause(active sibling) changed=%v error=%v", changed, err)
	}
	if result, err := graph.RecordFailure("task_01", 2, TaskFailure{
		ChildRunID: "run-task_01", TerminalStatus: "failed", BlockerCode: "implementation_failed", TokensUsed: 1,
	}, generation, graphGitSHA("pause-base"), false); err != nil || !result.Blocked {
		t.Fatalf("RecordFailure(task_01) = %#v, error=%v", result, err)
	}
	if changed, err := graph.ReconcileHumanPause(started.Add(4 * time.Second)); err != nil || !changed {
		t.Fatalf("ReconcileHumanPause(waiting only) changed=%v error=%v", changed, err)
	}
	if len(graph.Pauses) != 2 || graph.Pauses[1].RequestID != requestID("task_02") || graph.Pauses[1].EndedAt != nil {
		t.Fatalf("reopened global pause = %#v", graph.Pauses)
	}
}

func TestDeliveryGraphHumanPauseTreatsCandidateAndPendingDependentsAsQuiescent(t *testing.T) {
	t.Parallel()
	tasks := []GraphTask{
		{TaskID: "waiting", State: GraphTaskWaitingInput},
		{TaskID: "candidate", State: GraphTaskCandidate},
		{TaskID: "dependent", State: GraphTaskPending},
	}
	if !graphEntirelyWaitingForHuman(tasks) {
		t.Fatal("waiting question with candidate sibling and pending dependent must open the active-wall pause")
	}
	tasks[1].State = GraphTaskRunning
	if graphEntirelyWaitingForHuman(tasks) {
		t.Fatal("running sibling must keep active-wall accounting live")
	}
}

func TestDeliveryGraphRecordsCandidateAndSuccessfulUsageOnce(t *testing.T) {
	t.Parallel()

	record := validDeliveryFixture(t)
	record.Attempts = nil
	generation := validGenerationFixture(t)
	graph, err := NewDeliveryGraph(record.TaskSnapshot, generation, record.InitialWorktreeFingerprint.HeadSHA)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	wave, err := graph.AdmitReadyWave(ReadyWaveInput{
		IntegrationHeadSHA: record.InitialWorktreeFingerprint.HeadSHA,
		RemainingSlots:     1, ReachableCommits: map[string]bool{},
	})
	if err != nil {
		t.Fatalf("AdmitReadyWave() error = %v", err)
	}
	if err := graph.BeginWaveAttempts(wave.Number, generation); err != nil {
		t.Fatalf("BeginWaveAttempts() error = %v", err)
	}
	if _, err := graph.AttachWorktree("task_1", 1, GraphWorktree{ID: "wt-task-1", Root: "/managed/task-1", Ready: true}); err != nil {
		t.Fatalf("AttachWorktree() error = %v", err)
	}
	candidate := TaskCandidate{
		ChildRunID: "loop-task-1", BaseHeadSHA: wave.BaseHeadSHA,
		CommitSHA: graphGitSHA("candidate-task-1"), VerificationDigest: digestFixture("candidate-verification"),
		TokensUsed: 2400,
	}

	replayed, err := graph.RecordCandidate("task_1", 1, candidate)
	if err != nil || replayed {
		t.Fatalf("RecordCandidate() replay=%v error=%v", replayed, err)
	}
	replayed, err = graph.RecordCandidate("task_1", 1, candidate)
	if err != nil || !replayed {
		t.Fatalf("RecordCandidate(replay) replay=%v error=%v", replayed, err)
	}
	task, _ := graph.Task("task_1")
	if task.State != GraphTaskCandidate || task.Attempts[0].State != GraphTaskCandidate ||
		task.Attempts[0].ChildRunID != candidate.ChildRunID ||
		task.Attempts[0].CandidateCommitSHA != candidate.CommitSHA ||
		task.Attempts[0].VerificationDigest != candidate.VerificationDigest ||
		task.Attempts[0].TokensUsed == nil || *task.Attempts[0].TokensUsed != candidate.TokensUsed {
		t.Fatalf("candidate task = %#v", task)
	}
	if used, err := graph.CumulativeTokens(); err != nil || used != candidate.TokensUsed {
		t.Fatalf("CumulativeTokens() = %d, error=%v", used, err)
	}
}

func TestDeliveryGraphReservesOneGlobalTokenBudgetAcrossParallelAttempts(t *testing.T) {
	t.Parallel()

	snapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "pending", DomainBackend, ComplexityLow),
		graphTaskArtifact("task_02", "pending", DomainFrontend, ComplexityLow),
		graphTaskArtifact("task_03", "pending", DomainTesting, ComplexityLow),
		graphTaskArtifact("task_04", "pending", DomainDocs, ComplexityLow),
	})
	generation := graphGenerationFixture(t, snapshot)
	graph, err := NewDeliveryGraph(snapshot, generation, graphGitSHA("parallel-budget-base"))
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	wave, err := graph.AdmitReadyWave(ReadyWaveInput{
		IntegrationHeadSHA: graphGitSHA("parallel-budget-base"), RemainingSlots: 4, ReachableCommits: map[string]bool{},
	})
	if err != nil || len(wave.TaskIDs) != 4 {
		t.Fatalf("AdmitReadyWave() = %#v, error=%v", wave, err)
	}
	if err := graph.BeginWaveAttempts(wave.Number, generation); err != nil {
		t.Fatalf("BeginWaveAttempts() error = %v", err)
	}
	if err := graph.ReserveWaveTokens(wave.Number, 101); err != nil {
		t.Fatalf("ReserveWaveTokens() error = %v", err)
	}
	var reserved int64
	for index, taskID := range wave.TaskIDs {
		task, _ := graph.Task(taskID)
		allowance := task.Attempts[0].TokenAllowance
		if allowance != int64(25+map[bool]int{true: 1}[index == 0]) {
			t.Fatalf("task %s allowance = %d", taskID, allowance)
		}
		reserved += allowance
	}
	if reserved != 101 {
		t.Fatalf("reserved tokens = %d, want 101", reserved)
	}
	available, err := graph.AvailableTokenBudget(101)
	if err != nil || available != 0 {
		t.Fatalf("AvailableTokenBudget() = %d, error=%v", available, err)
	}
}

func TestDeliveryGraphSettlesCanonicalPrefixAndReexecutesOnlyFirstConflict(t *testing.T) {
	t.Parallel()

	snapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "pending", DomainFrontend, ComplexityHigh),
		graphTaskArtifact("task_02", "pending", DomainFrontend, ComplexityHigh),
		graphTaskArtifact("task_03", "pending", DomainFrontend, ComplexityHigh),
	})
	generation := graphGenerationFixture(t, snapshot)
	selected := generation.Rules[0].Runtime
	generation.Cells = []RoutingCell{{
		Domain: DomainFrontend, Complexity: ComplexityHigh, TaskIDs: []string{"task_01", "task_02", "task_03"},
		Selected:  RuntimeCandidate{ProviderID: selected.Provider, ModelID: selected.Model, Reasoning: selected.Reasoning},
		Fallbacks: []RuntimeCandidate{{ProviderID: "codex", ModelID: "gpt-5.6-terra", Reasoning: "high"}}, FallbackLimit: 1,
	}}
	generation, _ = finalizeGeneration(generation)
	base := graphGitSHA("settlement-base")
	graph, err := NewDeliveryGraph(snapshot, generation, base)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	wave, err := graph.AdmitReadyWave(ReadyWaveInput{IntegrationHeadSHA: base, RemainingSlots: 3, ReachableCommits: map[string]bool{}})
	if err != nil {
		t.Fatalf("AdmitReadyWave() error = %v", err)
	}
	if err := graph.BeginWaveAttempts(wave.Number, generation); err != nil {
		t.Fatalf("BeginWaveAttempts() error = %v", err)
	}
	candidateSHAs := []string{graphGitSHA("candidate-1"), graphGitSHA("candidate-2"), graphGitSHA("candidate-3")}
	for index, taskID := range wave.TaskIDs {
		if _, err := graph.AttachWorktree(taskID, 1, GraphWorktree{
			ID: "wt-" + taskID, Root: "/managed/" + taskID, Ready: true,
		}); err != nil {
			t.Fatalf("AttachWorktree(%s) error = %v", taskID, err)
		}
		if _, err := graph.RecordCandidate(taskID, 1, TaskCandidate{
			ChildRunID: "run-" + taskID, BaseHeadSHA: base, CommitSHA: candidateSHAs[index],
			VerificationDigest: digestFixture("verification-" + taskID), TokensUsed: int64(1000 + index),
		}); err != nil {
			t.Fatalf("RecordCandidate(%s) error = %v", taskID, err)
		}
	}
	integratedSHA := graphGitSHA("integrated-task-1")
	settlement := WaveSettlement{
		OperationID: digestFixture("settle-operation"), RequestDigest: digestFixture("settle-request"),
		Wave: wave.Number, StartingHeadSHA: base, OrderedTaskIDs: append([]string(nil), wave.TaskIDs...),
		CandidateCommitSHAs: append([]string(nil), candidateSHAs...), AcceptedTaskIDs: []string{"task_01"},
		AcceptedCommitSHAs: []string{candidateSHAs[0]}, IntegratedCommitSHAs: []string{integratedSHA},
		FirstConflictTaskID: "task_02", ConflictEvidenceDigest: digestFixture("conflict"), FinalHeadSHA: integratedSHA,
	}

	result, err := graph.SettleWave(settlement, generation)
	if err != nil || result.Replayed || result.Disposition != SettlementReexecuteConflict || result.TaskID != "task_02" ||
		result.Wave.Number != 2 || result.Wave.BaseHeadSHA != integratedSHA || result.Runtime.Model != "gpt-5.6-terra" {
		t.Fatalf("SettleWave() = %#v, error=%v", result, err)
	}
	replayedCandidate, err := graph.RecordCandidate("task_02", 1, TaskCandidate{
		ChildRunID: "run-task_02", BaseHeadSHA: base, CommitSHA: candidateSHAs[1],
		VerificationDigest: digestFixture("verification-task_02"), TokensUsed: 1001,
	})
	if err != nil || !replayedCandidate {
		t.Fatalf("RecordCandidate(historical replay) replay=%v error=%v", replayedCandidate, err)
	}
	replay, err := graph.SettleWave(settlement, generation)
	if err != nil || !replay.Replayed || replay.Disposition != result.Disposition || replay.TaskID != result.TaskID ||
		replay.Wave.Number != result.Wave.Number || replay.Runtime != result.Runtime {
		t.Fatalf("SettleWave(replay) = %#v, error=%v", replay, err)
	}
	first, _ := graph.Task("task_01")
	second, _ := graph.Task("task_02")
	third, _ := graph.Task("task_03")
	if first.State != GraphTaskIntegrated || first.IntegratedCommitSHA != integratedSHA ||
		second.State != GraphTaskPreparing || len(second.Attempts) != 2 || second.Attempts[0].Conflict == nil ||
		second.Attempts[1].BaseHeadSHA != integratedSHA || third.State != GraphTaskCandidate || len(graph.Integrations) != 1 {
		t.Fatalf("settled tasks first=%#v second=%#v third=%#v integrations=%#v", first, second, third, graph.Integrations)
	}

	if _, err := graph.AttachWorktree("task_02", 2, GraphWorktree{ID: "wt-task-02-a2", Root: "/managed/task-02-a2", Ready: true}); err != nil {
		t.Fatalf("AttachWorktree(task_02 retry) error = %v", err)
	}
	replacementSHA := graphGitSHA("candidate-2-replacement")
	if _, err := graph.RecordCandidate("task_02", 2, TaskCandidate{
		ChildRunID: "run-task-02-a2", BaseHeadSHA: integratedSHA, CommitSHA: replacementSHA,
		VerificationDigest: digestFixture("verification-task-02-a2"), TokensUsed: 500,
	}); err != nil {
		t.Fatalf("RecordCandidate(task_02 retry) error = %v", err)
	}
	replacementHead := graphGitSHA("integrated-task-2-replacement")
	replacementSettlement := WaveSettlement{
		OperationID: digestFixture("settle-replacement"), RequestDigest: digestFixture("settle-replacement-request"),
		Wave: 2, StartingHeadSHA: integratedSHA, OrderedTaskIDs: []string{"task_02"},
		CandidateCommitSHAs: []string{replacementSHA}, AcceptedTaskIDs: []string{"task_02"},
		AcceptedCommitSHAs: []string{replacementSHA}, IntegratedCommitSHAs: []string{replacementHead}, FinalHeadSHA: replacementHead,
	}
	if result, err := graph.SettleWave(replacementSettlement, generation); err != nil || result.Disposition != SettlementWaveIntegrated {
		t.Fatalf("SettleWave(replacement) = %#v, error=%v", result, err)
	}
	thirdHead := graphGitSHA("integrated-task-3")
	suffixSettlement := WaveSettlement{
		OperationID: digestFixture("settle-suffix"), RequestDigest: digestFixture("settle-suffix-request"),
		Wave: 1, StartingHeadSHA: replacementHead, OrderedTaskIDs: []string{"task_03"},
		CandidateCommitSHAs: []string{candidateSHAs[2]}, AcceptedTaskIDs: []string{"task_03"},
		AcceptedCommitSHAs: []string{candidateSHAs[2]}, IntegratedCommitSHAs: []string{thirdHead}, FinalHeadSHA: thirdHead,
	}
	if result, err := graph.SettleWave(suffixSettlement, generation); err != nil || result.Disposition != SettlementAllIntegrated {
		t.Fatalf("SettleWave(suffix) = %#v, error=%v", result, err)
	}
	replayedConflict, err := graph.SettleWave(settlement, generation)
	if err != nil || !replayedConflict.Replayed || replayedConflict.Disposition != SettlementReexecuteConflict ||
		replayedConflict.TaskID != "task_02" || replayedConflict.Wave.Number != 2 || replayedConflict.Runtime.Model != "gpt-5.6-terra" {
		t.Fatalf("SettleWave(conflict after integration) = %#v, error=%v", replayedConflict, err)
	}
}

func TestDeliveryGraphSettlesPersistedCandidateSnapshotBeforeLaterSibling(t *testing.T) {
	t.Parallel()

	snapshot := graphSnapshotFixture(t, []TaskArtifact{
		graphTaskArtifact("task_01", "pending", DomainBackend, ComplexityLow),
		graphTaskArtifact("task_02", "pending", DomainFrontend, ComplexityLow),
	})
	generation := graphGenerationFixture(t, snapshot)
	base := graphGitSHA("persisted-settlement-base")
	graph, err := NewDeliveryGraph(snapshot, generation, base)
	if err != nil {
		t.Fatalf("NewDeliveryGraph() error = %v", err)
	}
	wave, err := graph.AdmitReadyWave(ReadyWaveInput{IntegrationHeadSHA: base, RemainingSlots: 2, ReachableCommits: map[string]bool{}})
	if err != nil {
		t.Fatalf("AdmitReadyWave() error = %v", err)
	}
	if err := graph.BeginWaveAttempts(wave.Number, generation); err != nil {
		t.Fatalf("BeginWaveAttempts() error = %v", err)
	}
	commits := []string{graphGitSHA("persisted-candidate-1"), graphGitSHA("later-candidate-2")}
	for index, taskID := range wave.TaskIDs {
		if _, err := graph.AttachWorktree(taskID, 1, GraphWorktree{ID: "wt-" + taskID, Root: "/managed/" + taskID, Ready: true}); err != nil {
			t.Fatalf("AttachWorktree(%s) error = %v", taskID, err)
		}
		if _, err := graph.RecordCandidate(taskID, 1, TaskCandidate{
			ChildRunID: "run-" + taskID, BaseHeadSHA: base, CommitSHA: commits[index],
			VerificationDigest: digestFixture("verification-" + taskID), TokensUsed: 10,
		}); err != nil {
			t.Fatalf("RecordCandidate(%s) error = %v", taskID, err)
		}
	}
	resultHead := graphGitSHA("persisted-candidate-result")
	result, err := graph.SettleWave(WaveSettlement{
		OperationID: digestFixture("persisted-settlement"), RequestDigest: digestFixture("persisted-settlement-request"),
		Wave: wave.Number, StartingHeadSHA: base, OrderedTaskIDs: []string{"task_01"},
		CandidateCommitSHAs: []string{commits[0]}, AcceptedTaskIDs: []string{"task_01"},
		AcceptedCommitSHAs: []string{commits[0]}, IntegratedCommitSHAs: []string{resultHead}, FinalHeadSHA: resultHead,
	}, generation)
	first, _ := graph.Task("task_01")
	second, _ := graph.Task("task_02")
	if err != nil || result.Disposition != SettlementWaveIntegrated || first.State != GraphTaskIntegrated || second.State != GraphTaskCandidate {
		t.Fatalf("SettleWave(persisted snapshot) = %#v, error=%v first=%#v second=%#v", result, err, first, second)
	}
}

func TestDeliveryGraphRecordsCleanupIntentAndTerminalResultIdempotently(t *testing.T) {
	t.Parallel()

	record := validDeliveryFixture(t)
	record.Attempts = nil
	graph := graphForDeliveryFixture(t, record, 1)
	task := &graph.Tasks[0]
	task.Attempts[0].CandidateCommitSHA = graphGitSHA("cleanup-candidate")
	task.Attempts[0].VerificationDigest = digestFixture("cleanup-verification")
	task.State = GraphTaskIntegrated
	task.IntegratedCommitSHA = task.Attempts[0].CandidateCommitSHA
	task.Attempts[0].State = GraphTaskIntegrated
	task.Attempts[0].Question = nil
	operation := CleanupOperation{
		OperationID: digestFixture("cleanup-operation"), RequestDigest: digestFixture("cleanup-request"),
		TaskID: task.TaskID, Execution: 1, WorktreeID: task.Attempts[0].WorktreeID, State: CleanupPlanned,
	}

	replayed, err := graph.RecordCleanup(operation)
	if err != nil || replayed {
		t.Fatalf("RecordCleanup() replay=%v error=%v", replayed, err)
	}
	replayed, err = graph.RecordCleanup(operation)
	if err != nil || !replayed {
		t.Fatalf("RecordCleanup(replay) replay=%v error=%v", replayed, err)
	}
	removed, replayed, err := graph.CompleteCleanup(operation.OperationID, CleanupRemoved, "")
	if err != nil || replayed || removed.State != CleanupRemoved {
		t.Fatalf("CompleteCleanup() = %#v replay=%v error=%v", removed, replayed, err)
	}
	removed, replayed, err = graph.CompleteCleanup(operation.OperationID, CleanupRemoved, "")
	if err != nil || !replayed || removed.State != CleanupRemoved || len(graph.Cleanups) != 1 {
		t.Fatalf("CompleteCleanup(replay) = %#v replay=%v error=%v cleanups=%#v", removed, replayed, err, graph.Cleanups)
	}
}

func TestDeliveryGraphRejectsCleanupTerminalStateWithoutPlannedIntent(t *testing.T) {
	t.Parallel()

	record := validDeliveryFixture(t)
	record.Attempts = nil
	graph := graphForDeliveryFixture(t, record, 1)
	task := graph.Tasks[0]
	operation := CleanupOperation{
		OperationID:   digestFixture("direct-cleanup-removed"),
		RequestDigest: digestFixture("direct-cleanup-request-removed"),
		TaskID:        task.TaskID, Execution: 1, WorktreeID: task.Attempts[0].WorktreeID,
		State: CleanupRemoved,
	}

	if replayed, err := graph.RecordCleanup(operation); !errors.Is(err, ErrInvalidDeliveryTransition) || replayed {
		t.Fatalf("RecordCleanup() replay=%v error=%v, want removed-without-intent rejection", replayed, err)
	}
	if len(graph.Cleanups) != 0 {
		t.Fatalf("cleanups = %#v, want no mutation", graph.Cleanups)
	}
}

func graphForDeliveryFixture(t *testing.T, record DeliveryRecord, taskCount int) *DeliveryGraph {
	t.Helper()
	if taskCount != 1 {
		t.Fatalf("graphForDeliveryFixture taskCount = %d, only one-task delivery fixture is supported", taskCount)
	}
	generation := validGenerationFixture(t)
	graph, err := NewDeliveryGraph(record.TaskSnapshot, generation, record.InitialWorktreeFingerprint.HeadSHA)
	if err != nil {
		t.Fatalf("NewDeliveryGraph(fixture) error = %v", err)
	}
	graph.Tasks[0].State = GraphTaskWaitingInput
	graph.Tasks[0].Attempts = []GraphTaskAttempt{{
		Execution:   1,
		Runtime:     generation.Rules[0].Runtime,
		State:       GraphTaskWaitingInput,
		BaseHeadSHA: record.InitialWorktreeFingerprint.HeadSHA,
		WorktreeID:  "task-worktree-1", WorktreeRoot: "/workspace/task-1", ChildRunID: "child-run-1",
		Question: &TaskQuestion{RequestID: "request-1", Prompt: "Choose the public behavior", ContextDigest: digestFixture("question-context")},
	}}
	return graph
}

func graphSnapshotFixture(t *testing.T, tasks []TaskArtifact) DeliveryTaskSnapshot {
	t.Helper()
	snapshot, err := (TaskSet{Slug: "graph-demo", Tasks: tasks}).DeliverySnapshot()
	if err != nil {
		t.Fatalf("DeliverySnapshot() error = %v", err)
	}
	return snapshot
}

func graphTaskArtifact(id, status string, domain Domain, complexity Complexity, dependencies ...string) TaskArtifact {
	return TaskArtifact{
		ID: id, Status: status, Domain: domain, Complexity: complexity,
		Dependencies: dependencies, Digest: hexDigestFixture(id),
	}
}

func graphGenerationFixture(t *testing.T, snapshot DeliveryTaskSnapshot) RoutingGeneration {
	t.Helper()
	tasks := make([]GenerationTask, 0, len(snapshot.Tasks))
	rules := make([]RuntimeRule, 0, len(snapshot.Tasks))
	for _, task := range snapshot.Tasks {
		tasks = append(tasks, GenerationTask{ID: task.ID, Domain: task.Domain, Complexity: task.Complexity})
		rules = append(rules, RuntimeRule{
			Match:   RuntimeMatch{ID: task.ID},
			Runtime: RuntimeValue{Provider: "codex", Model: "gpt-5.6-luna", Reasoning: "high"},
		})
	}
	generation, err := finalizeGeneration(RoutingGeneration{
		SchemaVersion: routingGenerationSchemaVersion, PolicyVersion: "graph-test-v1",
		WorkspaceIdentityDigest: digestFixture("graph-workspace"), TaskSetDigest: snapshot.Digest,
		InventoryDigest: digestFixture("graph-inventory"), CatalogGeneration: digestFixture("graph-catalog"),
		Tasks: tasks, Rules: rules, DeliveryFallbackLimit: deliveryFallbackLimit,
	})
	if err != nil {
		t.Fatalf("finalizeGeneration() error = %v", err)
	}
	return generation
}

func redigestGraphSnapshot(t *testing.T, snapshot *DeliveryTaskSnapshot) {
	t.Helper()
	payload, err := json.Marshal(snapshot.Tasks)
	if err != nil {
		t.Fatalf("json.Marshal(snapshot tasks) error = %v", err)
	}
	digest := sha256.Sum256(payload)
	snapshot.Digest = hex.EncodeToString(digest[:])
	snapshot.IncompleteTaskIDs = snapshot.IncompleteTaskIDs[:0]
	for _, task := range snapshot.Tasks {
		if task.Status != "completed" {
			snapshot.IncompleteTaskIDs = append(snapshot.IncompleteTaskIDs, task.ID)
		}
	}
}

func graphGitSHA(seed string) string {
	digest := sha256.Sum256([]byte(seed))
	return hex.EncodeToString(digest[:20])
}

func graphTime(value time.Time) *time.Time {
	return &value
}
