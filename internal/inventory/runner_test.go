package inventory

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

func TestProbeRunnerAllowsOnlyRegisteredExecutableArgv(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	executable := filepath.Join(workspace, "bin", "codex")
	capture := &captureCommandRunner{result: publication.CommandResult{Stdout: []byte(`{"status":"ok"}`)}}
	runner, err := NewProbeRunner(capture, workspace, []ProbeSpec{{
		ID:         "codex.doctor",
		Executor:   ExecutorCodex,
		Executable: executable,
		Args:       []string{"doctor", "--json", "--summary"},
	}})
	if err != nil {
		t.Fatalf("NewProbeRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), "codex.doctor")
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := string(result.Output), `{"status":"ok"}`; got != want {
		t.Fatalf("Output = %q, want %q", got, want)
	}
	if capture.calls != 1 {
		t.Fatalf("command calls = %d, want 1", capture.calls)
	}
	if capture.command.Executable != executable {
		t.Fatalf("Executable = %q, want %q", capture.command.Executable, executable)
	}
	if !slices.Equal(capture.command.Args, []string{"doctor", "--json", "--summary"}) {
		t.Fatalf("Args = %#v, want registered argv", capture.command.Args)
	}

	_, err = runner.Run(context.Background(), "codex.login")
	if !errors.Is(err, ErrProbeNotRegistered) {
		t.Fatalf("Run(unregistered) error = %v, want ErrProbeNotRegistered", err)
	}
	if capture.calls != 1 {
		t.Fatalf("command calls after unregistered probe = %d, want 1", capture.calls)
	}
}

func TestProbeRunnerRejectsCallerPathsAndMutatingFlags(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	absolute := filepath.Join(workspace, "bin", "executor")
	tests := []struct {
		name string
		spec ProbeSpec
	}{
		{
			name: "relative executable",
			spec: ProbeSpec{ID: "codex.version", Executor: ExecutorCodex, Executable: "codex", Args: []string{"--version"}},
		},
		{
			name: "caller config path",
			spec: ProbeSpec{ID: "codex.config", Executor: ExecutorCodex, Executable: absolute, Args: []string{"doctor", "--config", "/caller/path"}},
		},
		{
			name: "mutating login",
			spec: ProbeSpec{ID: "codex.login", Executor: ExecutorCodex, Executable: absolute, Args: []string{"login"}},
		},
		{
			name: "mutating exec",
			spec: ProbeSpec{ID: "opencode.exec", Executor: ExecutorOpenCode, Executable: absolute, Args: []string{"exec", "echo safe"}},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			_, err := NewProbeRunner(&captureCommandRunner{}, workspace, []ProbeSpec{tt.spec})
			if err == nil {
				t.Fatal("NewProbeRunner() error = nil, want unsafe specification rejected")
			}
		})
	}
}

func TestProbeRunnerUsesTrustedWorkspaceTimeoutAndOutputCaps(t *testing.T) {
	t.Parallel()

	workspace := t.TempDir()
	capture := &captureCommandRunner{result: publication.CommandResult{Stdout: []byte("safe")}}
	runner, err := NewProbeRunner(capture, workspace, []ProbeSpec{{
		ID:         "compozy.version",
		Executor:   ExecutorCompozy,
		Executable: filepath.Join(workspace, "bin", "compozy"),
		Args:       []string{"version"},
		Timeout:    25 * time.Second,
	}})
	if err != nil {
		t.Fatalf("NewProbeRunner() error = %v", err)
	}

	if _, err := runner.Run(context.Background(), "compozy.version"); err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if capture.command.Directory != workspace {
		t.Fatalf("Directory = %q, want trusted workspace %q", capture.command.Directory, workspace)
	}
	if capture.command.StdoutLimit != 1024*1024 || capture.command.StderrLimit != 64*1024 {
		t.Fatalf("limits = stdout:%d stderr:%d, want 1048576/65536", capture.command.StdoutLimit, capture.command.StderrLimit)
	}
	if capture.deadline.IsZero() {
		t.Fatal("command context has no deadline")
	}
	remaining := time.Until(capture.deadline)
	if remaining <= 0 || remaining > 15*time.Second {
		t.Fatalf("deadline remaining = %s, want within (0, 15s]", remaining)
	}
}

func TestProbeRunnerRedactsFailuresBeforeReturning(t *testing.T) {
	t.Parallel()

	const secret = "BATUTA_PROBE_SECRET_4e77"
	capture := &captureCommandRunner{
		result: publication.CommandResult{
			Stdout:   []byte("stdout " + secret),
			Stderr:   []byte("authorization: Bearer " + secret),
			ExitCode: 23,
		},
		err: errors.New("executor failed with token " + secret),
	}
	runner, err := NewProbeRunner(capture, t.TempDir(), []ProbeSpec{{
		ID:         "cursor.status",
		Executor:   ExecutorCursorAgent,
		Executable: filepath.Join(t.TempDir(), "cursor-agent"),
		Args:       []string{"status", "--format", "json"},
	}})
	if err != nil {
		t.Fatalf("NewProbeRunner() error = %v", err)
	}

	result, err := runner.Run(context.Background(), "cursor.status")
	if err == nil {
		t.Fatal("Run() error = nil, want safe probe error")
	}
	if len(result.Output) != 0 {
		t.Fatalf("Output = %q, want empty on failure", result.Output)
	}
	for _, forbidden := range []string{secret, "4e77", "Bearer", "stdout", "token"} {
		if strings.Contains(err.Error(), forbidden) {
			t.Fatalf("safe error contains %q: %v", forbidden, err)
		}
	}
	if !errors.Is(err, ErrProbeFailed) {
		t.Fatalf("Run() error = %v, want ErrProbeFailed", err)
	}
}

type captureCommandRunner struct {
	command  publication.Command
	deadline time.Time
	result   publication.CommandResult
	err      error
	calls    int
}

func (r *captureCommandRunner) Run(ctx context.Context, command publication.Command) (publication.CommandResult, error) {
	r.calls++
	r.command = command
	r.deadline, _ = ctx.Deadline()
	return r.result, r.err
}
