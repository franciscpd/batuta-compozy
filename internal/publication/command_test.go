package publication

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const helperMarker = "--exec-runner-helper"

func TestExecRunnerUsesExactExecutableArgvAndDirectory(t *testing.T) {
	t.Parallel()

	wantArgs := []string{
		"value with spaces",
		"line one\nline two",
		"$(touch should-not-exist)",
		"semi;colon",
		"`backticks`",
	}
	directory := t.TempDir()

	result, err := (ExecRunner{}).Run(context.Background(), helperCommand(t, directory, "argv", wantArgs...))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	var got struct {
		Args      []string `json:"args"`
		Directory string   `json:"directory"`
	}
	if err := json.Unmarshal(result.Stdout, &got); err != nil {
		t.Fatalf("decode helper output: %v; stdout=%q", err, result.Stdout)
	}
	if strings.Join(got.Args, "\x00") != strings.Join(wantArgs, "\x00") {
		t.Fatalf("argv = %#v, want %#v", got.Args, wantArgs)
	}
	if got.Directory != directory {
		t.Fatalf("directory = %q, want %q", got.Directory, directory)
	}
	if result.ExitCode != 0 {
		t.Fatalf("exit code = %d, want 0", result.ExitCode)
	}
}

func TestExecRunnerBoundsStdoutAndStderrWhileProcessRuns(t *testing.T) {
	t.Parallel()

	command := helperCommand(t, t.TempDir(), "large-output")
	command.StdoutLimit = 64 * 1024
	command.StderrLimit = 32 * 1024

	result, err := (ExecRunner{}).Run(context.Background(), command)
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := len(result.Stdout), 64*1024; got != want {
		t.Fatalf("stdout length = %d, want %d", got, want)
	}
	if got, want := len(result.Stderr), 32*1024; got != want {
		t.Fatalf("stderr length = %d, want %d", got, want)
	}
	if !result.StdoutTruncated || !result.StderrTruncated {
		t.Fatalf("truncation = stdout:%t stderr:%t, want both true", result.StdoutTruncated, result.StderrTruncated)
	}
}

func TestExecRunnerUsesSafeDefaultOutputLimits(t *testing.T) {
	t.Parallel()

	result, err := (ExecRunner{}).Run(context.Background(), helperCommand(t, t.TempDir(), "large-output"))
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	if got, want := len(result.Stdout), 1024*1024; got != want {
		t.Fatalf("stdout length = %d, want default %d", got, want)
	}
	if got, want := len(result.Stderr), 64*1024; got != want {
		t.Fatalf("stderr length = %d, want default %d", got, want)
	}
	if !result.StdoutTruncated || !result.StderrTruncated {
		t.Fatalf("truncation = stdout:%t stderr:%t, want both true", result.StdoutTruncated, result.StderrTruncated)
	}
}

func TestBoundedWriterRetainsOnlyItsLimitWhileDraining(t *testing.T) {
	t.Parallel()

	writer := newBoundedWriter(64 * 1024)
	input := []byte(strings.Repeat("x", 2*1024*1024))
	written, err := writer.Write(input)
	if err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if written != len(input) {
		t.Fatalf("Write() = %d, want drained length %d", written, len(input))
	}
	if got, want := len(writer.Bytes()), 64*1024; got != want {
		t.Fatalf("retained length = %d, want %d", got, want)
	}
	if got, max := cap(writer.data), 64*1024; got > max {
		t.Fatalf("retained capacity = %d, want at most %d", got, max)
	}
	if !writer.Truncated() {
		t.Fatal("Truncated() = false, want true")
	}
}

func TestExecRunnerReturnsBoundedStderrAndExitCode(t *testing.T) {
	t.Parallel()

	command := helperCommand(t, t.TempDir(), "fail")
	command.StderrLimit = 64 * 1024

	result, err := (ExecRunner{}).Run(context.Background(), command)
	if err == nil {
		t.Fatal("Run() error = nil, want nonzero-exit error")
	}
	if result.ExitCode != 23 {
		t.Fatalf("exit code = %d, want 23", result.ExitCode)
	}
	if got, want := len(result.Stderr), 64*1024; got != want {
		t.Fatalf("stderr length = %d, want %d", got, want)
	}
	if !result.StderrTruncated {
		t.Fatal("stderr truncated = false, want true")
	}
	if strings.Contains(err.Error(), strings.Repeat("e", 65*1024)) {
		t.Fatal("error contains unbounded stderr")
	}
}

func TestExecRunnerHonorsContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	_, err := (ExecRunner{}).Run(ctx, helperCommand(t, t.TempDir(), "block"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("Run() error = %v, want context deadline exceeded", err)
	}
}

func TestExecRunnerPreservesManualContextCancellation(t *testing.T) {
	t.Parallel()

	ctx, cancel := context.WithCancel(context.Background())
	timer := time.AfterFunc(100*time.Millisecond, cancel)
	defer timer.Stop()

	_, err := (ExecRunner{}).Run(ctx, helperCommand(t, t.TempDir(), "block"))
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("Run() error = %v, want context canceled", err)
	}
}

func TestExecRunnerRejectsUnsafeCommandConfiguration(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		command Command
	}{
		{name: "blank executable", command: Command{}},
		{name: "relative executable", command: Command{Executable: "git"}},
		{name: "negative stdout limit", command: Command{Executable: filepath.Join(string(filepath.Separator), "controlled", "git"), StdoutLimit: -1}},
		{name: "negative stderr limit", command: Command{Executable: filepath.Join(string(filepath.Separator), "controlled", "git"), StderrLimit: -1}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			result, err := (ExecRunner{}).Run(context.Background(), tt.command)
			if err == nil {
				t.Fatal("Run() error = nil, want validation error")
			}
			if result.ExitCode != -1 {
				t.Fatalf("ExitCode = %d, want -1", result.ExitCode)
			}
		})
	}
}

func TestExecRunnerHelper(t *testing.T) {
	marker := -1
	for i, arg := range os.Args {
		if arg == helperMarker {
			marker = i
			break
		}
	}
	if marker < 0 {
		return
	}
	if marker+1 >= len(os.Args) {
		fmt.Fprintln(os.Stderr, "missing helper action")
		os.Exit(2)
	}

	switch os.Args[marker+1] {
	case "argv":
		directory, err := os.Getwd()
		if err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
		payload := struct {
			Args      []string `json:"args"`
			Directory string   `json:"directory"`
		}{Args: os.Args[marker+2:], Directory: directory}
		if err := json.NewEncoder(os.Stdout).Encode(payload); err != nil {
			fmt.Fprintln(os.Stderr, err)
			os.Exit(2)
		}
	case "large-output":
		writeRepeated(os.Stdout, "o", 2*1024*1024)
		writeRepeated(os.Stderr, "e", 2*1024*1024)
	case "fail":
		writeRepeated(os.Stderr, "e", 128*1024)
		os.Exit(23)
	case "block":
		time.Sleep(30 * time.Second)
	default:
		fmt.Fprintf(os.Stderr, "unknown helper action %q\n", os.Args[marker+1])
		os.Exit(2)
	}
	os.Exit(0)
}

func helperCommand(t *testing.T, directory, action string, args ...string) Command {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatalf("os.Executable(): %v", err)
	}
	return Command{
		Executable: executable,
		Args: append([]string{
			"-test.run=^TestExecRunnerHelper$",
			"--",
			helperMarker,
			action,
		}, args...),
		Directory: directory,
	}
}

func writeRepeated(file *os.File, value string, size int) {
	chunk := strings.Repeat(value, 4096)
	for written := 0; written < size; written += len(chunk) {
		if _, err := file.WriteString(chunk); err != nil {
			os.Exit(2)
		}
	}
}
