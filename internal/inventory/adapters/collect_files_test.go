package adapters

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/franciscpd/batuta-compozy/internal/publication"
)

func TestCollectorProjectsCodexAndCursorFilesWithoutContents(t *testing.T) {
	const secret = "BATUTA_LOCAL_FILE_SECRET_a03d"
	workspace := t.TempDir()
	userHome := t.TempDir()
	codexHome := filepath.Join(userHome, ".codex")
	for _, directory := range []string{codexHome, filepath.Join(workspace, ".cursor", "rules")} {
		if err := os.MkdirAll(directory, 0o700); err != nil {
			t.Fatalf("mkdir %s: %v", directory, err)
		}
	}
	files := map[string]string{
		filepath.Join(codexHome, "config.toml"):                      "model = \"gpt-5.6-sol\"\napproval_policy = \"never\"\nprivate = \"" + secret + "\"\n",
		filepath.Join(workspace, "AGENTS.md"):                        "instructions " + secret,
		filepath.Join(workspace, ".cursor", "mcp.json"):              `{"servers":{"private":{"token":"` + secret + `"}}}`,
		filepath.Join(workspace, ".cursor", "rules", "frontend.mdc"): "rule " + secret,
	}
	for path, content := range files {
		if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
			t.Fatalf("write %s: %v", path, err)
		}
	}
	codexExecutable := filepath.Join(workspace, "codex")
	cursorExecutable := filepath.Join(workspace, "agent")
	for _, executable := range []string{codexExecutable, cursorExecutable} {
		if err := os.WriteFile(executable, []byte("fixture"), 0o700); err != nil {
			t.Fatalf("write executable: %v", err)
		}
	}

	collector, err := newCollectorWithRoots(&fileProjectionRunner{}, CollectorOptions{
		TrustedWorkspace: workspace,
		WorkspaceID:      "ws-fixture",
		CodexExecutable:  codexExecutable,
		CursorExecutable: cursorExecutable,
	}, collectorRoots{userHome: userHome, codexHome: codexHome})
	if err != nil {
		t.Fatalf("newCollectorWithRoots() error = %v", err)
	}
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, executorID := range []inventory.ExecutorID{inventory.ExecutorCodex, inventory.ExecutorCursorAgent} {
		executor := findExecutor(t, snapshot, executorID)
		if len(executor.ConfigurationDigests) == 0 {
			t.Fatalf("%s configuration digests = empty", executorID)
		}
		if len(executor.InstructionDigests) == 0 {
			t.Fatalf("%s instruction digests = empty", executorID)
		}
	}
	payload, err := json.Marshal(inventory.Redact(snapshot, secret))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, forbidden := range []string{secret, "a03d", "instructions " + secret, "rule " + secret} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("snapshot leaks %q: %s", forbidden, payload)
		}
	}
}

func findExecutor(t *testing.T, snapshot inventory.InventorySnapshot, id inventory.ExecutorID) inventory.ExecutorSnapshot {
	t.Helper()
	for _, executor := range snapshot.Executors {
		if executor.ID == id {
			return executor
		}
	}
	t.Fatalf("executor %s not found", id)
	return inventory.ExecutorSnapshot{}
}

type fileProjectionRunner struct{}

func (*fileProjectionRunner) Run(_ context.Context, command publication.Command) (publication.CommandResult, error) {
	key := filepath.Base(command.Executable) + " " + strings.Join(command.Args, " ")
	outputs := map[string]string{
		"codex --version":               "codex-cli 0.115.0",
		"codex doctor --json --summary": `{"schema_version":1}`,
		"codex debug models --bundled":  `{"models":[{"slug":"gpt-5.6-sol"}]}`,
		"agent --version":               "Cursor Agent 2026.08.20",
		"agent status":                  "Authenticated",
		"agent models":                  "grok-4.6 - Grok 4.6\n",
	}
	output := outputs[key]
	if output == "" {
		output = `{}`
	}
	return publication.CommandResult{Stdout: []byte(output)}, nil
}
