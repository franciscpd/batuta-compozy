package main

import (
	"context"
	"errors"
	"os"
	"path/filepath"
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
