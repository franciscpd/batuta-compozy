package adapters

import (
	"encoding/json"
	"strings"
	"testing"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

func TestCodexConfigProjectionKeepsOnlyAllowlistedDeclaredFields(t *testing.T) {
	t.Parallel()

	const secret = "BATUTA_CODEX_CONFIG_SECRET_4a90"
	raw := []byte(`
model = "gpt-5.6-sol"
model_provider = "openai"
approval_policy = "never"
sandbox_mode = "workspace-write"

[model_providers.private]
env_key = "` + secret + `"

[mcp_servers.private]
command = "server"
args = ["--token", "` + secret + `"]
`)
	evidence, err := ParseCodexConfig(raw)
	if err != nil {
		t.Fatalf("ParseCodexConfig() error = %v", err)
	}
	if evidence.State != inventory.ResolutionDeclared {
		t.Fatalf("State = %q, want declared", evidence.State)
	}
	payload, err := json.Marshal(inventory.Redact(evidence, secret))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(payload)
	for _, required := range []string{"model:gpt-5.6-sol", "provider:openai", "approval:never", "sandbox:workspace-write"} {
		if !strings.Contains(text, required) {
			t.Fatalf("projection missing %q: %s", required, text)
		}
	}
	for _, forbidden := range []string{secret, "4a90", "model_providers", "mcp_servers", "--token"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("projection leaks %q: %s", forbidden, text)
		}
	}
}
