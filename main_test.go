package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

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

func TestDeliveryRunsChildrenInTheSelectedWorktreeWithBoundedOverrides(t *testing.T) {
	t.Parallel()

	payload, err := os.ReadFile("loops/batuta-deliver/loop.yaml")
	if err != nil {
		t.Fatalf("read batuta-deliver: %v", err)
	}
	var definition struct {
		Graph struct {
			Nodes []struct {
				ID     string `yaml:"id"`
				Kind   string `yaml:"kind"`
				Params struct {
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
				} `yaml:"params"`
			} `yaml:"nodes"`
		} `yaml:"graph"`
	}
	if err := yaml.Unmarshal(payload, &definition); err != nil {
		t.Fatalf("decode batuta-deliver: %v", err)
	}
	wantContext := map[string]struct {
		budgetTokens string
		budgetWall   string
		runtimeRules string
	}{
		"implement": {
			budgetTokens: "{{ .nodes.routing_context.output.remaining_tokens }}",
			budgetWall:   "{{ .nodes.routing_context.output.remaining_wall_seconds }}",
			runtimeRules: "{{ .nodes.routing_context.output.runtime_rules }}",
		},
		"review": {
			budgetTokens: "{{ .nodes.delivery_budget_context.output.remaining_tokens }}",
			budgetWall:   "{{ .nodes.delivery_budget_context.output.remaining_wall_seconds }}",
		},
	}
	for nodeID, want := range wantContext {
		var found bool
		for _, node := range definition.Graph.Nodes {
			if node.ID != nodeID {
				continue
			}
			found = true
			got := node.Params.ConfigOverrides
			if node.Kind != "run-loop" || got.IterationCap != 4 ||
				got.BudgetTokens != want.budgetTokens || got.BudgetWallSec != want.budgetWall ||
				got.RuntimeRules != want.runtimeRules || got.BudgetOnExceeded != "halt" ||
				got.ReattemptStrategy != "halt" {
				t.Fatalf("node %s bounded overrides = %#v", nodeID, got)
			}
			if got.Environment.Mode != "worktree" ||
				got.Environment.WorktreeRef != "{{ .inputs.worktree_ref }}" {
				t.Fatalf("node %s environment = %#v, want selected worktree", nodeID, got.Environment)
			}
		}
		if !found {
			t.Fatalf("delivery node %s not found", nodeID)
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
	if definition.Meta.Name != "batuta-task" || definition.Concurrency != "queue" || definition.Contract.IterationCap != 4 {
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
	properties := output["properties"].(map[string]any)
	status := properties["status"].(map[string]any)["enum"].([]any)
	if !reflect.DeepEqual(status, []any{"completed", "needs_input"}) || output["additionalProperties"] != false {
		t.Fatalf("implementer output schema = %#v", output)
	}
	for _, name := range []string{"record_question", "ask_operator", "record_answer", "implementation"} {
		if _, exists := nodes[name]; !exists {
			t.Fatalf("missing interactive task node %q", name)
		}
	}
	answerParams := nodes["record_answer"].Params
	if len(answerParams) != 7 || answerParams["question_operation_id"] != "{{ .nodes.record_question.output.question_operation_id }}" ||
		answerParams["answer"] != "{{ .nodes.ask_operator.output.answer }}" {
		t.Fatalf("record_answer must receive only question_operation_id and typed answer beyond task identity: %#v", answerParams)
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
	if definition.Contract.StopWhen != "nodes.implementation.output.status == 'completed'" {
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

type taskLoopEffect struct {
	Tool string         `yaml:"tool"`
	With map[string]any `yaml:"with"`
}
