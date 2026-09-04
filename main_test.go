package main

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"text/template"

	"github.com/batuta-ai/core/routing"
	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/franciscpd/batuta-compozy/internal/extensionapp"
	"gopkg.in/yaml.v3"
)

type fakeExtensionRunner struct {
	runCalls int
	err      error
}

func (f *fakeExtensionRunner) Run(context.Context) error {
	f.runCalls++
	return f.err
}

func TestRunRejectsMissingOrRelativeCompozyExecutable(t *testing.T) {
	t.Parallel()

	for _, executable := range []string{"", "compozy"} {
		t.Run(executable, func(t *testing.T) {
			t.Parallel()
			if err := run(context.Background(), executable, nil, nil); err == nil {
				t.Fatalf("run(%q) error = nil", executable)
			}
		})
	}
}

func TestRunDescribeDoesNotRequireRuntimeExecutables(t *testing.T) {
	t.Parallel()

	runner := &fakeExtensionRunner{}
	err := runDescribe(context.Background(), func(compozyExecutable, gitExecutable string) (extensionRunner, error) {
		if !filepath.IsAbs(compozyExecutable) || !filepath.IsAbs(gitExecutable) {
			t.Fatalf("describe paths = %q, %q", compozyExecutable, gitExecutable)
		}
		return runner, nil
	})
	if err != nil {
		t.Fatalf("runDescribe() error = %v", err)
	}
	if runner.runCalls != 1 {
		t.Fatalf("run calls = %d, want 1", runner.runCalls)
	}
}

func TestRunResolvesGitOnceAndStartsInjectedExtension(t *testing.T) {
	t.Parallel()

	compozyExecutable := filepath.Join(string(filepath.Separator), "opt", "compozy")
	gitExecutable := filepath.Join(string(filepath.Separator), "usr", "bin", "git")
	resolveCalls := 0
	runner := &fakeExtensionRunner{}
	err := run(
		context.Background(),
		compozyExecutable,
		func(name string) (string, error) {
			resolveCalls++
			if name != "git" {
				t.Fatalf("resolve name = %q", name)
			}
			return gitExecutable, nil
		},
		func(gotCompozy, gotGit string) (extensionRunner, error) {
			if gotCompozy != compozyExecutable || gotGit != gitExecutable {
				t.Fatalf("factory paths = %q, %q", gotCompozy, gotGit)
			}
			return runner, nil
		},
	)
	if err != nil {
		t.Fatalf("run() error = %v", err)
	}
	if resolveCalls != 1 || runner.runCalls != 1 {
		t.Fatalf("calls = resolve:%d run:%d", resolveCalls, runner.runCalls)
	}
}

func TestRunPropagatesResolverAndRuntimeFailures(t *testing.T) {
	t.Parallel()

	compozyExecutable := filepath.Join(string(filepath.Separator), "opt", "compozy")
	resolveErr := errors.New("git absent")
	if err := run(context.Background(), compozyExecutable, func(string) (string, error) {
		return "", resolveErr
	}, nil); !errors.Is(err, resolveErr) {
		t.Fatalf("resolver error = %v", err)
	}

	runErr := errors.New("runtime stopped")
	if err := run(
		context.Background(),
		compozyExecutable,
		func(string) (string, error) { return "/usr/bin/git", nil },
		func(string, string) (extensionRunner, error) { return &fakeExtensionRunner{err: runErr}, nil },
	); !errors.Is(err, runErr) {
		t.Fatalf("runtime error = %v", err)
	}
}

func TestDeliveryLauncherDefersExtensionGraphToExactCore(t *testing.T) {
	t.Parallel()

	launcherPayload, err := os.ReadFile("loops/batuta-deliver/loop.yaml")
	if err != nil {
		t.Fatalf("read batuta-deliver: %v", err)
	}
	corePayload, err := os.ReadFile("loops/batuta-deliver-core/loop.yaml")
	if err != nil {
		t.Fatalf("read batuta-deliver-core: %v", err)
	}
	type deliveryLauncherNode struct {
		ID     string `yaml:"id"`
		Class  string `yaml:"class"`
		Kind   string `yaml:"kind"`
		Params struct {
			Loop            string `yaml:"loop"`
			ConfigOverrides struct {
				IterationCap      string `yaml:"iteration_cap"`
				BudgetTokens      string `yaml:"budget_tokens"`
				BudgetWallSec     string `yaml:"budget_wall_sec"`
				BudgetOnExceeded  string `yaml:"budget_on_exceeded"`
				ReattemptStrategy string `yaml:"reattempt_strategy"`
			} `yaml:"config_overrides"`
			Inputs map[string]string `yaml:"inputs"`
		} `yaml:"params"`
	}
	var launcher struct {
		Meta struct {
			Name string `yaml:"name"`
		} `yaml:"meta"`
		Concurrency string `yaml:"concurrency"`
		Inputs      map[string]struct {
			Required bool `yaml:"required"`
		} `yaml:"inputs"`
		Contract struct {
			IterationCap int `yaml:"iteration_cap"`
			Budget       struct {
				Tokens       int    `yaml:"tokens"`
				WallClockSec int    `yaml:"wall_clock_sec"`
				OnExceeded   string `yaml:"on_exceeded"`
			} `yaml:"budget"`
			OnDone      []taskLoopEffect `yaml:"on_done"`
			OnNoop      []taskLoopEffect `yaml:"on_noop"`
			OnBlocked   []taskLoopEffect `yaml:"on_blocked"`
			OnFailed    []taskLoopEffect `yaml:"on_failed"`
			OnExhausted []taskLoopEffect `yaml:"on_exhausted"`
			OnStalled   []taskLoopEffect `yaml:"on_stalled"`
			OnCanceled  []taskLoopEffect `yaml:"on_canceled"`
		} `yaml:"contract"`
		Graph struct {
			Nodes []deliveryLauncherNode `yaml:"nodes"`
		} `yaml:"graph"`
	}
	if err := yaml.Unmarshal(launcherPayload, &launcher); err != nil {
		t.Fatalf("decode batuta-deliver: %v", err)
	}
	if launcher.Meta.Name != "batuta-deliver" || launcher.Concurrency != "queue" {
		t.Fatalf("launcher identity = %#v", launcher)
	}
	for _, name := range []string{
		"delivery_envelope_version", "delivery_id", "attempt", "slug",
		"origin_session_id", "worktree_ref", "routing_generation",
		"absolute_deadline", "token_ceiling", "recovery_operation_id",
		"iteration_cap", "budget_tokens", "budget_wall_seconds",
	} {
		if input, exists := launcher.Inputs[name]; !exists || !input.Required {
			t.Fatalf("missing launcher input %q", name)
		}
	}
	if len(launcher.Graph.Nodes) != 1 || launcher.Graph.Nodes[0].ID != "delivery_core" ||
		launcher.Graph.Nodes[0].Kind != "run-loop" ||
		launcher.Graph.Nodes[0].Params.Loop != "batuta-deliver-core" {
		t.Fatalf("launcher graph = %#v", launcher.Graph)
	}
	for _, node := range launcher.Graph.Nodes {
		if strings.HasPrefix(node.Kind, "ext__") {
			t.Fatalf("public launcher resolves a hosted extension action: %#v", node)
		}
	}
	if launcher.Contract.IterationCap != 1 || launcher.Contract.Budget.Tokens != 0 ||
		launcher.Contract.Budget.WallClockSec != 14400 || launcher.Contract.Budget.OnExceeded != "halt" {
		t.Fatalf("launcher contract = %#v", launcher.Contract)
	}
	for name, effects := range map[string][]taskLoopEffect{
		"on_done": launcher.Contract.OnDone, "on_noop": launcher.Contract.OnNoop,
		"on_blocked": launcher.Contract.OnBlocked, "on_failed": launcher.Contract.OnFailed,
		"on_exhausted": launcher.Contract.OnExhausted, "on_stalled": launcher.Contract.OnStalled,
		"on_canceled": launcher.Contract.OnCanceled,
	} {
		if len(effects) != 1 || effects[0].Tool != "compozy__session_prompt" {
			t.Fatalf("launcher %s = %#v", name, effects)
		}
	}
	overrides := launcher.Graph.Nodes[0].Params.ConfigOverrides
	if overrides.IterationCap != "{{ .inputs.iteration_cap }}" ||
		overrides.BudgetTokens != "{{ .inputs.budget_tokens }}" ||
		overrides.BudgetWallSec != "{{ .inputs.budget_wall_seconds }}" ||
		overrides.BudgetOnExceeded != "halt" || overrides.ReattemptStrategy != "halt" {
		t.Fatalf("delivery core overrides = %#v", overrides)
	}
	for _, name := range []string{
		"delivery_envelope_version", "delivery_id", "attempt", "slug",
		"origin_session_id", "worktree_ref", "routing_generation",
		"absolute_deadline", "token_ceiling", "recovery_operation_id",
		"iteration_cap", "budget_tokens", "budget_wall_seconds",
	} {
		if got := launcher.Graph.Nodes[0].Params.Inputs[name]; got != "{{ .inputs."+name+" }}" {
			t.Fatalf("delivery core input %q = %q", name, got)
		}
	}
	if len(launcher.Graph.Nodes[0].Params.Inputs) != 13 {
		t.Fatalf("delivery core inputs = %#v", launcher.Graph.Nodes[0].Params.Inputs)
	}

	var core struct {
		Contract struct {
			OnDone      []taskLoopEffect `yaml:"on_done"`
			OnNoop      []taskLoopEffect `yaml:"on_noop"`
			OnBlocked   []taskLoopEffect `yaml:"on_blocked"`
			OnFailed    []taskLoopEffect `yaml:"on_failed"`
			OnExhausted []taskLoopEffect `yaml:"on_exhausted"`
			OnStalled   []taskLoopEffect `yaml:"on_stalled"`
			OnCanceled  []taskLoopEffect `yaml:"on_canceled"`
		} `yaml:"contract"`
	}
	if err := yaml.Unmarshal(corePayload, &core); err != nil {
		t.Fatalf("decode batuta-deliver-core: %v", err)
	}
	for name, effects := range map[string][]taskLoopEffect{
		"on_done": core.Contract.OnDone, "on_noop": core.Contract.OnNoop,
		"on_blocked": core.Contract.OnBlocked, "on_failed": core.Contract.OnFailed,
		"on_exhausted": core.Contract.OnExhausted, "on_stalled": core.Contract.OnStalled,
		"on_canceled": core.Contract.OnCanceled,
	} {
		if len(effects) != 0 {
			t.Fatalf("core %s = %#v", name, effects)
		}
	}
}

func TestDeliveryCoreRunsDependencySafeTaskWavesWithBoundedChildOverrides(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile("loops/batuta-deliver-core/loop.yaml")
	if err != nil {
		t.Fatalf("read batuta-deliver-core: %v", err)
	}
	type deliveryCoreNode struct {
		ID     string `yaml:"id"`
		Class  string `yaml:"class"`
		Kind   string `yaml:"kind"`
		Params struct {
			Loop            string `yaml:"loop"`
			ConfigOverrides struct {
				IterationCap      int    `yaml:"iteration_cap"`
				BudgetTokens      string `yaml:"budget_tokens"`
				BudgetWallSec     string `yaml:"budget_wall_sec"`
				BudgetOnExceeded  string `yaml:"budget_on_exceeded"`
				ReattemptStrategy string `yaml:"reattempt_strategy"`
				RuntimeRules      string `yaml:"runtime_rules"`
				Environment       struct {
					Mode        string `yaml:"mode"`
					WorktreeRef string `yaml:"worktree_ref"`
				} `yaml:"environment"`
			} `yaml:"config_overrides"`
			Values map[string]any `yaml:",inline"`
		} `yaml:"params"`
		Collection  string `yaml:"collection"`
		Filter      string `yaml:"filter"`
		BatchSize   int    `yaml:"batch_size"`
		MaxParallel int    `yaml:"max_parallel"`
		MaxFanOut   int    `yaml:"max_fan_out"`
	}
	var definition struct {
		Meta struct {
			Name string `yaml:"name"`
		} `yaml:"meta"`
		Contract struct {
			IterationCap int    `yaml:"iteration_cap"`
			StopWhen     string `yaml:"stop_when"`
		} `yaml:"contract"`
		Graph struct {
			Nodes []deliveryCoreNode `yaml:"nodes"`
			Edges []struct {
				From string `yaml:"from"`
				To   string `yaml:"to"`
			} `yaml:"edges"`
		} `yaml:"graph"`
	}
	if err := yaml.Unmarshal(payload, &definition); err != nil {
		t.Fatalf("decode batuta-deliver-core: %v", err)
	}
	if definition.Meta.Name != "batuta-deliver-core" {
		t.Fatalf("core identity = %#v", definition.Meta)
	}
	nodes := map[string]deliveryCoreNode{}
	for _, node := range definition.Graph.Nodes {
		nodes[node.ID] = node
	}
	if definition.Contract.IterationCap != 64 ||
		definition.Contract.StopWhen != "nodes.prepare_wave.status == 'succeeded' && nodes.prepare_wave.output.disposition == 'all_integrated'" {
		t.Fatalf("parent generation contract = %#v", definition.Contract)
	}
	for _, nodeID := range []string{
		"load_check", "routing_context", "prepare_wave", "wave_route", "task_wave", "run_task",
		"record_candidate", "collect_wave", "settle_wave", "settle_route", "delivery_budget_context",
		"review", "publication_plan", "publication_route", "publish", "publication_verify",
		"publication_verify_nothing", "publication_verify_local", "cleanup",
	} {
		if _, exists := nodes[nodeID]; !exists {
			t.Fatalf("missing parent node %q", nodeID)
		}
	}
	for _, required := range []string{
		"terminal_blocked", "terminal_exhausted", "terminal_blocked_publication", "cleanup_route", "cleanup_complete",
	} {
		if _, exists := nodes[required]; !exists {
			t.Fatalf("terminal topology missing node %q", required)
		}
	}
	for nodeID, disposition := range map[string]string{"terminal_blocked": "blocked", "terminal_exhausted": "exhausted", "terminal_blocked_publication": "blocked"} {
		node := nodes[nodeID]
		if node.Kind != "ext__batuta__delivery_graph" || node.Params.Values["operation"] != "terminalize" ||
			node.Params.Values["terminal_disposition"] != disposition {
			t.Fatalf("%s = %#v", nodeID, node)
		}
	}
	wave := nodes["task_wave"]
	if wave.Class != "control" || wave.Kind != "fan-out" ||
		wave.Collection != "{{ .nodes.prepare_wave.output.tasks }}" ||
		wave.Filter != "nodes.prepare_wave.output.disposition == 'wave_ready'" || wave.BatchSize != 1 ||
		wave.MaxParallel != 4 || wave.MaxFanOut != 4 {
		t.Fatalf("task wave = %#v", wave)
	}
	runTask := nodes["run_task"]
	overrides := runTask.Params.ConfigOverrides
	if runTask.Kind != "run-loop" || runTask.Params.Loop != "batuta-task" || overrides.IterationCap != 4 ||
		overrides.BudgetTokens != "{{ .item.remaining_tokens }}" ||
		overrides.BudgetWallSec != "{{ .item.remaining_active_wall_seconds }}" ||
		overrides.BudgetOnExceeded != "halt" || overrides.ReattemptStrategy != "halt" ||
		overrides.RuntimeRules != "{{ .nodes.routing_context.output.runtime_rules }}" ||
		overrides.Environment.Mode != "worktree" || overrides.Environment.WorktreeRef != "{{ .item.worktree_id }}" {
		t.Fatalf("batuta-task child overrides = %#v", overrides)
	}
	for _, node := range definition.Graph.Nodes {
		if node.Kind == "gate" || node.ID == "human_gate" || node.ID == "implement-tasks" {
			t.Fatalf("core contains forbidden node %#v", node)
		}
	}
	for _, pair := range [][2]string{
		{"load_check", "routing_context"}, {"routing_context", "prepare_wave"}, {"prepare_wave", "task_wave"},
		{"task_wave", "run_task"}, {"run_task", "record_candidate"},
		{"record_candidate", "collect_wave"}, {"collect_wave", "wave_route"}, {"wave_route", "settle_wave"},
		{"settle_wave", "settle_route"},
		{"wave_route", "delivery_budget_context"}, {"delivery_budget_context", "review"},
		{"settle_route", "generation_continue_settle"},
		{"review", "publication_plan"}, {"publication_plan", "publication_route"}, {"publication_route", "publish"},
		{"publish", "publication_verify"}, {"publication_verify", "cleanup"},
		{"publication_route", "publication_verify_nothing"}, {"publication_verify_nothing", "cleanup"},
		{"publication_route", "publication_verify_local"}, {"publication_verify_local", "cleanup"},
		{"publication_route", "publication_blocked_stop"}, {"publication_blocked_stop", "terminal_blocked_publication"},
		{"wave_route", "terminal_exhausted"}, {"settle_route", "terminal_exhausted_settle"},
		{"settle_route", "terminal_blocked_settle"},
		{"cleanup", "cleanup_route"}, {"cleanup_route", "terminal_blocked_cleanup"},
	} {
		found := false
		for _, edge := range definition.Graph.Edges {
			if edge.From == pair[0] && edge.To == pair[1] {
				found = true
				break
			}
		}
		if !found {
			t.Fatalf("missing core graph edge %s -> %s", pair[0], pair[1])
		}
	}
}

func TestBatutaTaskLoopKeepsInteractiveTaskIdentityDaemonOwned(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile("loops/batuta-task/loop.yaml")
	if err != nil {
		t.Fatalf("read batuta-task: %v", err)
	}
	var definition struct {
		Meta struct {
			Name string `yaml:"name"`
		} `yaml:"meta"`
		Concurrency string `yaml:"concurrency"`
		Inputs      map[string]struct {
			Required bool `yaml:"required"`
		} `yaml:"inputs"`
		Contract struct {
			IterationCap int              `yaml:"iteration_cap"`
			StopWhen     string           `yaml:"stop_when"`
			OnBlocked    []taskLoopEffect `yaml:"on_blocked"`
			OnFailed     []taskLoopEffect `yaml:"on_failed"`
			OnExhausted  []taskLoopEffect `yaml:"on_exhausted"`
			OnStalled    []taskLoopEffect `yaml:"on_stalled"`
			OnCanceled   []taskLoopEffect `yaml:"on_canceled"`
		} `yaml:"contract"`
		Graph struct {
			Nodes []struct {
				ID      string         `yaml:"id"`
				Kind    string         `yaml:"kind"`
				Params  map[string]any `yaml:"params"`
				Session struct {
					Isolated bool `yaml:"isolated"`
				} `yaml:"session"`
			} `yaml:"nodes"`
		} `yaml:"graph"`
	}
	if err := yaml.Unmarshal(payload, &definition); err != nil {
		t.Fatalf("decode batuta-task: %v", err)
	}
	if definition.Meta.Name != "batuta-task" || definition.Concurrency != "allow" || definition.Contract.IterationCap != 4 {
		t.Fatalf("task loop identity = %#v", definition)
	}
	for _, name := range []string{"delivery_id", "wave", "task_id", "execution", "routing_generation", "runtime", "worktree_ref", "base_sha", "budget_tokens", "budget_wall_seconds"} {
		input, exists := definition.Inputs[name]
		if !exists || !input.Required {
			t.Fatalf("missing immutable required input %q: %#v", name, definition.Inputs)
		}
	}
	if len(definition.Graph.Nodes) == 0 || definition.Graph.Nodes[0].ID != "task_context" || definition.Graph.Nodes[0].Kind != "ext__batuta__delivery_graph" {
		t.Fatalf("first node = %#v, want task_context delivery_graph", definition.Graph.Nodes)
	}
	nodes := map[string]struct {
		ID      string
		Kind    string
		Params  map[string]any
		Session struct{ Isolated bool }
	}{}
	for _, node := range definition.Graph.Nodes {
		nodes[node.ID] = struct {
			ID      string
			Kind    string
			Params  map[string]any
			Session struct{ Isolated bool }
		}{ID: node.ID, Kind: node.Kind, Params: node.Params, Session: struct{ Isolated bool }{Isolated: node.Session.Isolated}}
	}
	implementer, exists := nodes["implement_task"]
	if !exists || implementer.Kind != "run-agent" || !implementer.Session.Isolated ||
		implementer.Params["agent"] != "code_implementer" || implementer.Params["runtime"] != "{{ .inputs.runtime }}" {
		t.Fatalf("implementer = %#v", implementer)
	}
	environment, ok := implementer.Params["environment"].(map[string]any)
	if !ok || environment["mode"] != "worktree" || environment["worktree_ref"] != "{{ .inputs.worktree_ref }}" {
		t.Fatalf("implementer environment = %#v", implementer.Params["environment"])
	}
	output, ok := implementer.Params["output_schema"].(map[string]any)
	if !ok {
		t.Fatalf("implementer output schema = %#v", implementer.Params["output_schema"])
	}
	variants, ok := output["oneOf"].([]any)
	if !ok || len(variants) != 2 {
		t.Fatalf("implementer output schema = %#v", output)
	}
	if output["type"] != "object" || output["additionalProperties"] != false {
		t.Fatalf("implementer output root must be closed for reference resolution: %#v", output)
	}
	rootProperties, ok := output["properties"].(map[string]any)
	if !ok {
		t.Fatalf("implementer output root properties = %#v", output["properties"])
	}
	for _, name := range []string{"status", "commit_sha", "verification", "question", "choices"} {
		if _, exists := rootProperties[name]; !exists {
			t.Fatalf("implementer output root property %q is unavailable to downstream templates: %#v", name, rootProperties)
		}
	}
	for _, raw := range variants {
		variant := raw.(map[string]any)
		if _, exists := variant["not"]; !exists {
			t.Fatalf("implementer output variant must prohibit the other closed shape: %#v", variant)
		}
	}
	completed := variants[0].(map[string]any)
	needsInput := variants[1].(map[string]any)
	if !reflect.DeepEqual(completed["required"], []any{"status", "commit_sha", "verification"}) ||
		completed["properties"].(map[string]any)["status"].(map[string]any)["enum"].([]any)[0] != "completed" ||
		rootProperties["commit_sha"].(map[string]any)["pattern"] != "^[a-f0-9]{40}([a-f0-9]{24})?$" ||
		!reflect.DeepEqual(needsInput["required"], []any{"status", "question", "choices"}) ||
		needsInput["properties"].(map[string]any)["status"].(map[string]any)["enum"].([]any)[0] != "needs_input" {
		t.Fatalf("implementer closed variants = %#v", variants)
	}
	verificationSchema := rootProperties["verification"].(map[string]any)
	verificationProperties := verificationSchema["properties"].(map[string]any)
	checks := verificationProperties["checks"].(map[string]any)
	checkItems := checks["items"].(map[string]any)
	if verificationSchema["type"] != "object" || verificationSchema["additionalProperties"] != false ||
		!reflect.DeepEqual(verificationSchema["required"], []any{"task_id", "status", "checks"}) ||
		checks["maxItems"] != 16 || checkItems["maxLength"] != 512 {
		t.Fatalf("completion verification must remain deterministically inline: %#v", verificationSchema)
	}
	for _, name := range []string{"record_question", "ask_operator", "record_answer", "implementation"} {
		if _, exists := nodes[name]; !exists {
			t.Fatalf("missing interactive task node %q", name)
		}
	}
	answerParams := nodes["record_answer"].Params
	if len(answerParams) != 8 || answerParams["question_operation_id"] != "{{ .nodes.record_question.output.question_operation_id }}" ||
		answerParams["answer"] != "{{ .nodes.ask_operator.output.answer }}" ||
		answerParams["worktree_ref"] != "{{ .inputs.delivery_worktree_ref }}" {
		t.Fatalf("record_answer must receive only question_operation_id and typed answer beyond delivery identity: %#v", answerParams)
	}
	ask := nodes["ask_operator"].Params
	responders := ask["responders"].(map[string]any)
	expect := ask["expect"].(map[string]any)
	if responders["agents"] != "deny" || !reflect.DeepEqual(expect, map[string]any{
		"type": "object", "additionalProperties": false, "required": []any{"answer"},
		"properties": map[string]any{"answer": map[string]any{"type": "string", "minLength": 1, "maxLength": 4096}},
	}) {
		t.Fatalf("ask contract = %#v", ask)
	}
	if definition.Contract.StopWhen != "nodes.implement_task.status == 'succeeded' && nodes.implement_task.output.status == 'completed'" {
		t.Fatalf("stop_when = %q", definition.Contract.StopWhen)
	}
	for name, effects := range map[string][]taskLoopEffect{
		"on_blocked": definition.Contract.OnBlocked, "on_failed": definition.Contract.OnFailed,
		"on_exhausted": definition.Contract.OnExhausted, "on_stalled": definition.Contract.OnStalled,
		"on_canceled": definition.Contract.OnCanceled,
	} {
		if len(effects) != 1 || effects[0].Tool != "ext__batuta__delivery_graph" ||
			effects[0].With["operation"] != "record_failure" ||
			effects[0].With["child_run_id"] != "{{ .effect.identity.loop_run_id }}" ||
			!strings.Contains(effects[0].With["blocker_code"].(string), ".effect.identity.generation") {
			t.Fatalf("%s terminal effect = %#v", name, effects)
		}
	}
	if _, exists := nodes["record_candidate"]; exists {
		t.Fatal("batuta-task must not record its own candidate without the public child run identity")
	}
	for _, forbidden := range []string{"kind: run-loop", "publish_worktree", "publication_", "runtime_rules", "worktree_root"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("batuta-task contains forbidden %q", forbidden)
		}
	}
}

func TestBatutaTaskPromptRendersJournaledAnswers(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile("loops/batuta-task/loop.yaml")
	if err != nil {
		t.Fatalf("read batuta-task: %v", err)
	}
	var definition struct {
		Graph struct {
			Nodes []struct {
				ID     string         `yaml:"id"`
				Params map[string]any `yaml:"params"`
			} `yaml:"nodes"`
		} `yaml:"graph"`
	}
	if err := yaml.Unmarshal(payload, &definition); err != nil {
		t.Fatalf("decode batuta-task: %v", err)
	}
	var prompt string
	for _, node := range definition.Graph.Nodes {
		if node.ID == "implement_task" {
			prompt, _ = node.Params["prompt"].(string)
		}
	}
	if !strings.Contains(prompt, "{{ toJson .nodes.task_context.output.answers }}") {
		t.Fatalf("implementer prompt omits explicit journaled answers: %q", prompt)
	}
	render := template.Must(template.New("prompt").Funcs(template.FuncMap{
		"toJson": func(value any) (string, error) {
			encoded, err := json.Marshal(value)
			return string(encoded), err
		},
	}).Parse(prompt))
	for _, answers := range [][]routing.TaskAnswer{
		nil,
		{{QuestionOperationID: "sha256:one", Value: "one"}},
		{{QuestionOperationID: "sha256:one", Value: "one"}, {QuestionOperationID: "sha256:two", Value: "two"}},
	} {
		result, err := compozysdk.StructuredResult(extensionapp.DeliveryGraphOutput{
			Operation: extensionapp.GraphOpTaskContext, Disposition: extensionapp.GraphDispositionTaskReady,
			TaskFile: ".compozy/tasks/demo/task_01.md", Answers: answers,
		})
		if err != nil {
			t.Fatalf("StructuredResult(task_context) error = %v", err)
		}
		var output map[string]any
		if err := json.Unmarshal(result.Structured, &output); err != nil {
			t.Fatalf("decode structured task_context: %v", err)
		}
		var rendered bytes.Buffer
		if err := render.Option("missingkey=error").Execute(&rendered, map[string]any{
			"inputs": map[string]any{"task_id": "task_01"},
			"nodes":  map[string]any{"task_context": map[string]any{"output": output}},
		}); err != nil {
			t.Fatalf("render answers %#v output=%#v: %v", answers, output, err)
		}
		encoded, err := json.Marshal(output["answers"])
		if err != nil || !strings.Contains(rendered.String(), string(encoded)) {
			t.Fatalf("rendered prompt does not contain answers %s: %q", encoded, rendered.String())
		}
	}
}

type taskLoopEffect struct {
	Tool string         `yaml:"tool"`
	With map[string]any `yaml:"with"`
}

// TestLoopRouteTargetsHaveSingleRouteParent guards the Loop-engine contract
// discovered on 2026-09-02: a route target with more than one route parent
// deadlocks its generation and the run fails without diagnostics.
// The Loop engine prunes the branches a route did not take, but that pruning
// deadlocks on a target with more than one route parent and never crosses a
// fan-out/collect pair. Every route target must therefore be a plain node with
// exactly one route parent.
func TestLoopRouteTargetsArePrunable(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"batuta-deliver", "batuta-deliver-core", "batuta-task"} {
		payload, err := os.ReadFile(filepath.Join("loops", name, "loop.yaml"))
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		var definition struct {
			Graph struct {
				Nodes []struct {
					ID     string `yaml:"id"`
					Kind   string `yaml:"kind"`
					Routes []struct {
						To string `yaml:"to"`
					} `yaml:"routes"`
					Default string `yaml:"default"`
				} `yaml:"nodes"`
			} `yaml:"graph"`
		}
		if err := yaml.Unmarshal(payload, &definition); err != nil {
			t.Fatalf("decode %s: %v", name, err)
		}
		kinds := map[string]string{}
		for _, node := range definition.Graph.Nodes {
			kinds[node.ID] = node.Kind
		}
		parents := map[string]map[string]struct{}{}
		for _, node := range definition.Graph.Nodes {
			if node.Kind != "route" {
				continue
			}
			targets := make([]string, 0, len(node.Routes)+1)
			for _, route := range node.Routes {
				targets = append(targets, route.To)
			}
			if node.Default != "" {
				targets = append(targets, node.Default)
			}
			for _, target := range targets {
				if kind := kinds[target]; kind == "fan-out" || kind == "collect" {
					t.Fatalf("%s: route %q targets %s node %q", name, node.ID, kind, target)
				}
				if parents[target] == nil {
					parents[target] = map[string]struct{}{}
				}
				parents[target][node.ID] = struct{}{}
			}
		}
		for target, routeParents := range parents {
			if len(routeParents) > 1 {
				t.Fatalf("%s: route target %q has multiple route parents %v", name, target, routeParents)
			}
		}
	}
}
