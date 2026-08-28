package inventory_test

import (
	"context"
	"encoding/json"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/franciscpd/batuta-compozy/internal/inventory/adapters"
	"github.com/franciscpd/batuta-compozy/internal/publication"
)

func TestCollectorRunsProbesWithExplicitBoundedParallelism(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	runner := &parallelismCollectorRunner{}
	collector, err := adapters.NewCollector(runner, adapters.CollectorOptions{
		TrustedWorkspace:   workspace,
		WorkspaceID:        "ws-fixture",
		CompozyExecutable:  filepath.Join(workspace, "compozy"),
		CodexExecutable:    filepath.Join(workspace, "codex"),
		OpenCodeExecutable: filepath.Join(workspace, "opencode"),
		CursorExecutable:   filepath.Join(workspace, "agent"),
		ProbeParallelism:   16,
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	if _, err := collector.Collect(context.Background()); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if got := runner.maximum.Load(); got <= 11 || got > 16 {
		t.Fatalf("maximum concurrent probes = %d, want 12..16 across executor adapters", got)
	}
}

func TestCollectorKeepsHealthyExecutorsWhenOneAdapterIsMalformed(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	runner := &fixtureCollectorRunner{}
	collector, err := adapters.NewCollector(runner, adapters.CollectorOptions{
		TrustedWorkspace:   workspace,
		WorkspaceID:        "ws-fixture",
		CompozyExecutable:  filepath.Join(workspace, "compozy"),
		CodexExecutable:    filepath.Join(workspace, "codex"),
		OpenCodeExecutable: filepath.Join(workspace, "opencode"),
		CursorExecutable:   filepath.Join(workspace, "agent"),
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}

	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(snapshot.Executors) != 4 {
		t.Fatalf("executors = %d, want 4", len(snapshot.Executors))
	}
	states := make(map[inventory.ExecutorID]inventory.ResolutionState, len(snapshot.Executors))
	for _, executor := range snapshot.Executors {
		states[executor.ID] = executor.Version.State
	}
	if states[inventory.ExecutorCodex] != inventory.ResolutionUnknown {
		t.Fatalf("Codex version state = %q, want unknown malformed fixture", states[inventory.ExecutorCodex])
	}
	for _, id := range []inventory.ExecutorID{inventory.ExecutorCompozy, inventory.ExecutorOpenCode, inventory.ExecutorCursorAgent} {
		if states[id] != inventory.ResolutionResolved {
			t.Fatalf("%s version state = %q, want resolved", id, states[id])
		}
	}
}

func TestCollectorAssociatesConstructorOwnedProviderBindings(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	collector, err := adapters.NewCollector(&fixtureCollectorRunner{}, adapters.CollectorOptions{
		TrustedWorkspace:   workspace,
		WorkspaceID:        "ws-fixture",
		CompozyExecutable:  filepath.Join(workspace, "compozy"),
		CodexExecutable:    filepath.Join(workspace, "codex"),
		OpenCodeExecutable: filepath.Join(workspace, "opencode"),
		CursorExecutable:   filepath.Join(workspace, "agent"),
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	want := map[inventory.ExecutorID][]inventory.ProviderBinding{
		inventory.ExecutorCompozy:     nil,
		inventory.ExecutorCodex:       {{ProviderID: "codex"}, {ProviderID: "codex", ModelID: "gpt-5.6-sol"}},
		inventory.ExecutorOpenCode:    {{ProviderID: "opencode", ModelID: "openai/gpt-5.6-terra"}},
		inventory.ExecutorCursorAgent: {{ProviderID: "cursor"}},
	}
	for _, executor := range snapshot.Executors {
		if got := executor.ProviderBindings; !slices.Equal(got, want[executor.ID]) {
			t.Fatalf("%s provider bindings = %#v, want %#v", executor.ID, got, want[executor.ID])
		}
	}
}

func TestCollectorAlwaysReportsAllFourExecutorsWhenBinariesAreMissing(t *testing.T) {
	t.Parallel()

	collector, err := adapters.NewCollector(&fixtureCollectorRunner{}, adapters.CollectorOptions{
		TrustedWorkspace: t.TempDir(),
		WorkspaceID:      "ws-fixture",
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if len(snapshot.Executors) != 4 {
		t.Fatalf("executors = %d, want 4 missing snapshots", len(snapshot.Executors))
	}
	for _, executor := range snapshot.Executors {
		if executor.Version.State != inventory.ResolutionUnknown || len(executor.Diagnostics) == 0 || executor.Diagnostics[0].Code != "executable_missing" {
			t.Fatalf("missing executor = %#v, want closed missing diagnostic", executor)
		}
	}
}

func TestCollectorEnforcesOneSharedSubprocessBudget(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	runner := &fixtureCollectorRunner{manyAgents: true}
	collector, err := adapters.NewCollector(runner, adapters.CollectorOptions{
		TrustedWorkspace:   workspace,
		WorkspaceID:        "ws-fixture",
		OpenCodeExecutable: filepath.Join(workspace, "opencode"),
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	_, err = collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	if runner.calls != 64 {
		t.Fatalf("subprocess calls = %d, want global cap 64", runner.calls)
	}
}

func TestCollectorMarksEvidenceUnknownAtNormalizedRecordBudget(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	runner := &fixtureCollectorRunner{manyModels: true}
	collector, err := adapters.NewCollector(runner, adapters.CollectorOptions{
		TrustedWorkspace:   workspace,
		WorkspaceID:        "ws-fixture",
		OpenCodeExecutable: filepath.Join(workspace, "opencode"),
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, executor := range snapshot.Executors {
		if executor.ID != inventory.ExecutorOpenCode {
			continue
		}
		for _, capability := range executor.Capabilities {
			if capability.Name == "models" && capability.State != inventory.ResolutionUnknown {
				t.Fatalf("models state = %q, want unknown after record cap", capability.State)
			}
		}
		if !hasDiagnosticCode(executor.Diagnostics, "record_budget_exhausted") {
			t.Fatalf("diagnostics = %#v, want record_budget_exhausted", executor.Diagnostics)
		}
		return
	}
	t.Fatal("OpenCode snapshot not found")
}

type fixtureCollectorRunner struct {
	calls      int
	manyAgents bool
	manyModels bool
}

type parallelismCollectorRunner struct {
	current atomic.Int32
	maximum atomic.Int32
	mu      sync.Mutex
	fixture fixtureCollectorRunner
}

func (r *parallelismCollectorRunner) Run(ctx context.Context, command publication.Command) (publication.CommandResult, error) {
	current := r.current.Add(1)
	defer r.current.Add(-1)
	for maximum := r.maximum.Load(); current > maximum && !r.maximum.CompareAndSwap(maximum, current); maximum = r.maximum.Load() {
	}
	time.Sleep(10 * time.Millisecond)
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.fixture.Run(ctx, command)
}

func (r *fixtureCollectorRunner) Run(_ context.Context, command publication.Command) (publication.CommandResult, error) {
	r.calls++
	name := filepath.Base(command.Executable)
	args := strings.Join(command.Args, " ")
	var output string
	switch name + " " + args {
	case "compozy version":
		output = `{"Version":"0.3.0-beta.21"}`
	case "compozy status -o json":
		output = `{"schema_version":"1","daemon":{"status":"running"}}`
	case "compozy provider models list --all -o json":
		output = `{"models":[{"provider_id":"cursor","model_id":"grok-4.6","availability_state":"available"}]}`
	case "codex --version":
		output = `{`
	case "codex doctor --json --summary":
		output = `{"schema_version":1}`
	case "codex debug models --bundled":
		output = `{"models":[{"slug":"gpt-5.6-sol"}]}`
	case "opencode --version":
		output = `1.0.95`
	case "opencode debug config":
		output = `{"schema_version":1,"model":"openai/gpt-5.6-terra"}`
	case "opencode models":
		if r.manyModels {
			var models strings.Builder
			for i := 0; i < 10050; i++ {
				models.WriteString("provider/model-")
				models.WriteString(strconv.Itoa(i))
				models.WriteByte('\n')
			}
			output = models.String()
		} else {
			output = "openai/gpt-5.6-terra\n"
		}
	case "opencode agent list":
		if r.manyAgents {
			agents := make([]map[string]string, 100)
			for i := range agents {
				agents[i] = map[string]string{"name": "agent-" + strings.Repeat("x", i%10) + string(rune('a'+i%26))}
			}
			payload, _ := json.Marshal(agents)
			output = string(payload)
		} else {
			output = `[{"name":"build"}]`
		}
	case "agent --version":
		output = `Cursor Agent 2026.08.20`
	case "agent status":
		output = `Authenticated`
	case "agent models":
		output = "grok-4.6 - Grok 4.6\n"
	default:
		if strings.HasPrefix(args, "debug agent ") {
			output = `{}`
		} else {
			output = `{}`
		}
	}
	return publication.CommandResult{Stdout: []byte(output)}, nil
}

func hasDiagnosticCode(values []inventory.Diagnostic, code string) bool {
	for _, value := range values {
		if value.Code == code {
			return true
		}
	}
	return false
}
