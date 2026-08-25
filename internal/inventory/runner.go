package inventory

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

const (
	probeTimeoutLimit = 15 * time.Second
	probeStdoutLimit  = 1024 * 1024
	probeStderrLimit  = 64 * 1024
)

var (
	ErrProbeNotRegistered = errors.New("inventory: probe not registered")
	ErrProbeFailed        = errors.New("inventory: probe failed")
)

type ProbeID string

type ProbeSpec struct {
	ID         ProbeID
	Executor   ExecutorID
	Executable string
	Args       []string
	Timeout    time.Duration
}

type ProbeResult struct {
	ProbeID   ProbeID
	Executor  ExecutorID
	Output    []byte
	ExitCode  int
	Truncated bool
}

type Collector interface {
	Collect(context.Context) (InventorySnapshot, error)
}

type ProbeRunner struct {
	runner    publication.CommandRunner
	workspace string
	specs     map[ProbeID]ProbeSpec
}

func NewProbeRunner(runner publication.CommandRunner, trustedWorkspace string, specs []ProbeSpec) (*ProbeRunner, error) {
	if runner == nil {
		return nil, errors.New("inventory: command runner is required")
	}
	if strings.TrimSpace(trustedWorkspace) == "" || !filepath.IsAbs(trustedWorkspace) {
		return nil, errors.New("inventory: trusted workspace must be absolute")
	}

	registered := make(map[ProbeID]ProbeSpec, len(specs))
	for _, spec := range specs {
		if err := validateProbeSpec(spec); err != nil {
			return nil, err
		}
		if _, exists := registered[spec.ID]; exists {
			return nil, fmt.Errorf("inventory: duplicate probe %q", spec.ID)
		}
		spec.Args = slices.Clone(spec.Args)
		if spec.Timeout <= 0 || spec.Timeout > probeTimeoutLimit {
			spec.Timeout = probeTimeoutLimit
		}
		registered[spec.ID] = spec
	}

	return &ProbeRunner{
		runner:    runner,
		workspace: filepath.Clean(trustedWorkspace),
		specs:     registered,
	}, nil
}

func (r *ProbeRunner) Run(ctx context.Context, id ProbeID) (ProbeResult, error) {
	spec, ok := r.specs[id]
	if !ok {
		return ProbeResult{}, fmt.Errorf("%w: %q", ErrProbeNotRegistered, id)
	}

	probeCtx, cancel := context.WithTimeout(ctx, spec.Timeout)
	defer cancel()
	command := publication.Command{
		Executable:  spec.Executable,
		Args:        slices.Clone(spec.Args),
		Directory:   r.workspace,
		StdoutLimit: probeStdoutLimit,
		StderrLimit: probeStderrLimit,
	}
	result, err := r.runner.Run(probeCtx, command)
	if err != nil {
		if ctxErr := probeCtx.Err(); ctxErr != nil {
			return ProbeResult{}, ctxErr
		}
		return ProbeResult{ProbeID: id, Executor: spec.Executor, ExitCode: result.ExitCode},
			fmt.Errorf("%w: %q", ErrProbeFailed, id)
	}

	return ProbeResult{
		ProbeID:   id,
		Executor:  spec.Executor,
		Output:    slices.Clone(result.Stdout),
		ExitCode:  result.ExitCode,
		Truncated: result.StdoutTruncated || result.StderrTruncated,
	}, nil
}

func validateProbeSpec(spec ProbeSpec) error {
	if strings.TrimSpace(string(spec.ID)) == "" {
		return errors.New("inventory: probe ID is required")
	}
	if !spec.Executor.valid() {
		return fmt.Errorf("inventory: probe %q has unsupported executor %q", spec.ID, spec.Executor)
	}
	if strings.TrimSpace(spec.Executable) == "" || !filepath.IsAbs(spec.Executable) {
		return fmt.Errorf("inventory: probe %q executable must be absolute", spec.ID)
	}
	if spec.Timeout < 0 {
		return fmt.Errorf("inventory: probe %q timeout must not be negative", spec.ID)
	}
	for _, arg := range spec.Args {
		if unsafeProbeArgument(arg) {
			return fmt.Errorf("inventory: probe %q contains forbidden argument", spec.ID)
		}
	}
	return nil
}

func unsafeProbeArgument(arg string) bool {
	normalized := strings.ToLower(strings.TrimSpace(arg))
	switch normalized {
	case "login", "install", "update", "remove", "refresh", "run", "exec", "serve":
		return true
	}
	for _, flag := range []string{"--config", "--cwd", "--directory", "--path", "--file", "--home"} {
		if normalized == flag || strings.HasPrefix(normalized, flag+"=") {
			return true
		}
	}
	return false
}
