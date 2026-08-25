package main

import (
	"context"
	"errors"
	"path/filepath"
	"testing"
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
