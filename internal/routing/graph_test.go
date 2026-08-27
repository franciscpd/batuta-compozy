package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
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
