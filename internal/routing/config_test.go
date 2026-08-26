package routing

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/franciscpd/batuta-compozy/internal/publication"
)

func TestLoopConfigReadUsesStructuredReadOnlySnapshot(t *testing.T) {
	t.Parallel()

	var commands []publication.Command
	client := LoopConfigClient{
		Executable: "/opt/compozy",
		Runner: commandRunnerFunc(func(_ context.Context, command publication.Command) (publication.CommandResult, error) {
			commands = append(commands, command)
			return publication.CommandResult{Stdout: []byte(`{"config":{"iteration_cap":4,"runtime_rules":[]},"effective_config":{"iteration_cap":4},"config_revision":7}`)}, nil
		}),
	}
	snapshot, err := client.Read(context.Background(), "workspace-1", "implement-tasks")
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	wantArgs := []string{"loop", "config", "--workspace", "workspace-1", "--name", "implement-tasks", "-o", "json"}
	if len(commands) != 1 || commands[0].Executable != "/opt/compozy" || !slices.Equal(commands[0].Args, wantArgs) {
		t.Fatalf("commands = %#v, want exact read-only command %#v", commands, wantArgs)
	}
	if snapshot.ConfigRevision != 7 || len(snapshot.RuntimeRules) != 0 || string(snapshot.Config["iteration_cap"]) != "4" {
		t.Fatalf("snapshot = %#v, want revisioned structured config", snapshot)
	}
}

func TestLoopConfigUsesInternalFileAndTrustedWorkspaceOnly(t *testing.T) {
	t.Parallel()

	var internalPath string
	client := LoopConfigClient{
		Executable: "/opt/compozy",
		Runner: commandRunnerFunc(func(_ context.Context, command publication.Command) (publication.CommandResult, error) {
			wantPrefix := []string{"loop", "configure", "--workspace", "workspace-1", "--name", "implement-tasks", "--expected-revision", "7", "--file"}
			if len(command.Args) != len(wantPrefix)+3 || !slices.Equal(command.Args[:len(wantPrefix)], wantPrefix) || command.Args[len(command.Args)-2] != "-o" || command.Args[len(command.Args)-1] != "json" {
				t.Fatalf("write args = %#v, want fixed CAS command", command.Args)
			}
			internalPath = command.Args[len(wantPrefix)]
			if !filepath.IsAbs(internalPath) {
				t.Fatalf("internal config path = %q, want absolute", internalPath)
			}
			info, err := os.Stat(internalPath)
			if err != nil {
				t.Fatalf("stat internal config: %v", err)
			}
			if info.Mode().Perm() != 0o600 {
				t.Fatalf("internal config mode = %o, want 600", info.Mode().Perm())
			}
			body, err := os.ReadFile(internalPath)
			if err != nil {
				t.Fatalf("read internal config: %v", err)
			}
			var config map[string]json.RawMessage
			if json.Unmarshal(body, &config) != nil || string(config["iteration_cap"]) != "4" {
				t.Fatalf("internal config = %s, want typed internally-created document", body)
			}
			return publication.CommandResult{Stdout: []byte(`{"config":{"iteration_cap":4},"effective_config":{"iteration_cap":4},"config_revision":8}`)}, nil
		}),
	}
	config := LoopConfigDocument{"iteration_cap": json.RawMessage(`4`)}
	if _, err := client.Write(context.Background(), "workspace-1", "implement-tasks", 7, config); err != nil {
		t.Fatalf("Write() error = %v", err)
	}
	if _, err := os.Stat(internalPath); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("internal config remains after return: %v", err)
	}
}

func TestLoopConfigWriteUsesExpectedRevisionAndReportsConflict(t *testing.T) {
	t.Parallel()

	client := LoopConfigClient{
		Executable: "/opt/compozy",
		Runner: commandRunnerFunc(func(_ context.Context, _ publication.Command) (publication.CommandResult, error) {
			return publication.CommandResult{Stdout: []byte(`{"error":"loop config revision conflict","expected_revision":7,"current_revision":9}`), ExitCode: 1}, errors.New("exit 1")
		}),
	}
	_, err := client.Write(context.Background(), "workspace-1", "implement-tasks", 7, LoopConfigDocument{})
	var conflict *ConfigRevisionConflictError
	if !errors.As(err, &conflict) || conflict.ExpectedRevision != 7 || conflict.CurrentRevision != 9 {
		t.Fatalf("Write(conflict) error = %#v, want typed 7/9 conflict", err)
	}
	if strings.Contains(err.Error(), "batuta-loop-config") || strings.Contains(err.Error(), os.TempDir()) {
		t.Fatalf("conflict leaks internal path: %v", err)
	}
}

func TestLoopConfigRejectsMalformedOrMismatchedReadBack(t *testing.T) {
	t.Parallel()

	responses := [][]byte{
		[]byte(`not-json`),
		[]byte(`{"config":{},"effective_config":{},"config_revision":-1}`),
		[]byte(`{"config":{},"effective_config":{},"config_revision":1,"unknown":true}`),
	}
	for _, response := range responses {
		response := response
		client := LoopConfigClient{Executable: "/opt/compozy", Runner: commandRunnerFunc(func(context.Context, publication.Command) (publication.CommandResult, error) {
			return publication.CommandResult{Stdout: response}, nil
		})}
		if _, err := client.Read(context.Background(), "workspace-1", "implement-tasks"); err == nil {
			t.Fatalf("Read(%s) error = nil, want safe malformed response rejection", response)
		}
	}
}

func TestLoopConfigAppendsOwnedMatrixAndPreservesOperatorRules(t *testing.T) {
	t.Parallel()

	operator := RuntimeRule{Match: RuntimeMatch{ID: "task_special"}, Runtime: RuntimeValue{Provider: "operator", Model: "model", Speed: "fast"}}
	owned := RuntimeRule{Match: RuntimeMatch{Domain: DomainFrontend, Complexity: ComplexityHigh}, Runtime: RuntimeValue{Provider: "cursor", Model: "grok-4.6", Reasoning: "high"}}
	config := LoopConfigDocument{
		"iteration_cap": json.RawMessage(`4`),
		"runtime_rules": mustJSON(t, []RuntimeRule{operator}),
	}
	merged, err := MergeRuntimeRules(config, nil, []RuntimeRule{owned})
	if err != nil {
		t.Fatalf("MergeRuntimeRules() error = %v", err)
	}
	rules, err := decodeRuntimeRules(merged["runtime_rules"])
	if err != nil {
		t.Fatalf("decodeRuntimeRules() error = %v", err)
	}
	if !slices.Equal(rules, []RuntimeRule{operator, owned}) || string(merged["iteration_cap"]) != "4" {
		t.Fatalf("merged = %#v, want operator then owned with unrelated field preserved", merged)
	}
}

func TestLoopConfigNeverAcceptsCallerPathOrRawRules(t *testing.T) {
	t.Parallel()

	typ := []any{LoopConfigClient{}, LoopConfigDocument{}, RuntimeRule{}}
	encoded, err := json.Marshal(typ)
	if err != nil {
		t.Fatalf("json.Marshal(types) error = %v", err)
	}
	for _, forbidden := range []string{"caller_path", "config_path", "raw_rules", "command", "executable"} {
		if strings.Contains(string(encoded), forbidden) {
			t.Fatalf("public routing config types expose forbidden %q: %s", forbidden, encoded)
		}
	}
}

type commandRunnerFunc func(context.Context, publication.Command) (publication.CommandResult, error)

func (f commandRunnerFunc) Run(ctx context.Context, command publication.Command) (publication.CommandResult, error) {
	return f(ctx, command)
}

func mustJSON(t *testing.T, value any) json.RawMessage {
	t.Helper()
	payload, err := json.Marshal(value)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	return payload
}
