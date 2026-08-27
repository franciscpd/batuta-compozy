package publication

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

const (
	defaultStdoutLimit int64 = 1024 * 1024
	defaultStderrLimit int64 = 64 * 1024
)

type Command struct {
	Executable  string
	Args        []string
	Directory   string
	Stdin       []byte
	Environment []string
	StdoutLimit int64
	StderrLimit int64
}

type CommandResult struct {
	Stdout          []byte
	Stderr          []byte
	ExitCode        int
	StdoutTruncated bool
	StderrTruncated bool
}

type CommandRunner interface {
	Run(context.Context, Command) (CommandResult, error)
}

type ExecRunner struct{}

func (ExecRunner) Run(ctx context.Context, command Command) (CommandResult, error) {
	if strings.TrimSpace(command.Executable) == "" || !filepath.IsAbs(command.Executable) {
		return CommandResult{ExitCode: -1}, errors.New("publication: executable must be absolute")
	}
	if command.StdoutLimit < 0 || command.StderrLimit < 0 {
		return CommandResult{ExitCode: -1}, errors.New("publication: output limits must not be negative")
	}
	for _, entry := range command.Environment {
		name, _, found := strings.Cut(entry, "=")
		if !found || name == "" || strings.ContainsAny(name, "\x00=") || strings.ContainsRune(entry, '\x00') {
			return CommandResult{ExitCode: -1}, errors.New("publication: invalid environment override")
		}
	}

	stdoutLimit := command.StdoutLimit
	if stdoutLimit == 0 {
		stdoutLimit = defaultStdoutLimit
	}
	stderrLimit := command.StderrLimit
	if stderrLimit == 0 {
		stderrLimit = defaultStderrLimit
	}

	stdout := newBoundedWriter(stdoutLimit)
	stderr := newBoundedWriter(stderrLimit)
	cmd := exec.CommandContext(ctx, command.Executable, command.Args...)
	cmd.Dir = command.Directory
	cmd.Stdin = bytes.NewReader(command.Stdin)
	cmd.Env = append(os.Environ(), command.Environment...)
	cmd.Stdout = stdout
	cmd.Stderr = stderr

	err := cmd.Run()
	result := CommandResult{
		Stdout:          stdout.Bytes(),
		Stderr:          stderr.Bytes(),
		ExitCode:        0,
		StdoutTruncated: stdout.Truncated(),
		StderrTruncated: stderr.Truncated(),
	}
	if err == nil {
		return result, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		result.ExitCode = -1
		return result, ctxErr
	}

	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) {
		result.ExitCode = exitErr.ExitCode()
		return result, fmt.Errorf("publication: command exited with code %d: %s: %w", result.ExitCode, result.Stderr, err)
	}
	result.ExitCode = -1
	return result, fmt.Errorf("publication: start command: %w", err)
}

type boundedWriter struct {
	limit     int64
	data      []byte
	truncated bool
}

func newBoundedWriter(limit int64) *boundedWriter {
	return &boundedWriter{limit: limit}
}

func (w *boundedWriter) Write(p []byte) (int, error) {
	written := len(p)
	remaining := w.limit - int64(len(w.data))
	if remaining <= 0 {
		if written > 0 {
			w.truncated = true
		}
		return written, nil
	}
	if int64(written) > remaining {
		w.data = append(w.data, p[:remaining]...)
		w.truncated = true
		return written, nil
	}
	w.data = append(w.data, p...)
	return written, nil
}

func (w *boundedWriter) Bytes() []byte {
	return append([]byte(nil), w.data...)
}

func (w *boundedWriter) Truncated() bool {
	return w.truncated
}
