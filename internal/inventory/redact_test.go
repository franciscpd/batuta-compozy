package inventory

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestRedactRemovesSecretCanariesRecursively(t *testing.T) {
	t.Parallel()

	const canary = "BATUTA_SECRET_CANARY_7f4c91"
	input := map[string]any{
		"executor_id": "codex",
		"source":      "codex doctor",
		"diagnostics": []any{
			map[string]any{
				"code":    "probe_failed",
				"summary": "authorization failed for " + canary,
				"headers": map[string]any{"authorization": "Bearer " + canary},
			},
		},
		"environment": map[string]any{"TOKEN": canary},
		"mcp_arguments": []any{
			"--header",
			"X-Token=" + canary,
		},
		"url": "https://user:" + canary + "@example.test/path?token=" + canary,
	}

	redacted := Redact(input, canary)
	payload, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(payload)
	for _, forbidden := range []string{canary, "7f4c91", "Bearer", "X-Token", "authorization", "environment", "mcp_arguments"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("redacted JSON contains %q: %s", forbidden, text)
		}
	}
}

func TestRedactNeverReturnsRawCommandOutput(t *testing.T) {
	t.Parallel()

	const raw = "raw-command-output-do-not-return"
	redacted := Redact(map[string]any{
		"executor_id": "opencode",
		"stdout":      raw,
		"stderr":      raw,
		"error":       "command failed: " + raw,
		"command":     []string{"opencode", "debug", "config"},
	})
	payload, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(payload)
	for _, forbidden := range []string{raw, "stdout", "stderr", "command failed", "opencode debug config"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("redacted JSON contains raw command material %q: %s", forbidden, text)
		}
	}
}

func TestRedactPreservesSafeIdentifiersAndProvenance(t *testing.T) {
	t.Parallel()

	redacted := Redact(map[string]any{
		"schema_version":  1,
		"executor_id":     "cursor-agent",
		"state":           "resolved",
		"source":          "cursor-agent models",
		"digest":          "sha256:0123456789abcdef",
		"identifiers":     []any{"cursor", "grok-4.6"},
		"diagnostic_code": "model_catalog_resolved",
	})
	payload, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(payload)
	for _, required := range []string{
		`"schema_version":1`,
		`"executor_id":"cursor-agent"`,
		`"state":"resolved"`,
		`"source":"cursor-agent models"`,
		`"digest":"sha256:0123456789abcdef"`,
		`"cursor"`,
		`"grok-4.6"`,
		`"diagnostic_code":"model_catalog_resolved"`,
	} {
		if !strings.Contains(text, required) {
			t.Fatalf("redacted JSON missing safe field %s: %s", required, text)
		}
	}
}

func TestRedactPreservesSafeProviderBindingsAndDropsUnknownCanaries(t *testing.T) {
	t.Parallel()

	const canary = "BATUTA_PROVIDER_SECRET_8ab1"
	redacted := Redact(map[string]any{
		"executor_id": "cursor-agent",
		"provider_bindings": []any{
			map[string]any{
				"provider_id": "cursor",
				"model_id":    "grok-4.6[effort=high,fast=true]",
				"login":       canary,
				"command":     "cursor-agent auth " + canary,
			},
		},
		"raw_provider": map[string]any{"token": canary},
	}, canary)
	payload, err := json.Marshal(redacted)
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	text := string(payload)
	for _, required := range []string{`"provider_bindings"`, `"provider_id":"cursor"`, `"model_id":"grok-4.6[effort=high,fast=true]"`} {
		if !strings.Contains(text, required) {
			t.Fatalf("redacted JSON missing safe provider binding %s: %s", required, text)
		}
	}
	for _, forbidden := range []string{canary, "8ab1", "login", "command", "raw_provider", "token"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("redacted JSON contains provider canary %q: %s", forbidden, text)
		}
	}
}
