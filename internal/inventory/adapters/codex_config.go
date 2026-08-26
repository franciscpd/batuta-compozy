package adapters

import (
	"errors"
	"strings"
	"unicode"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/pelletier/go-toml/v2"
)

func ParseCodexConfig(raw []byte) (inventory.Evidence, error) {
	var projection struct {
		Model          string `toml:"model"`
		ModelProvider  string `toml:"model_provider"`
		ApprovalPolicy string `toml:"approval_policy"`
		SandboxMode    string `toml:"sandbox_mode"`
	}
	if err := toml.Unmarshal(raw, &projection); err != nil {
		return inventory.Evidence{}, errors.New("inventory adapter: malformed Codex config")
	}

	identifiers := make([]string, 0, 4)
	if safeConfigIdentifier(projection.Model) {
		identifiers = append(identifiers, "model:"+projection.Model)
	}
	if safeConfigIdentifier(projection.ModelProvider) {
		identifiers = append(identifiers, "provider:"+projection.ModelProvider)
	}
	if allowedValue(projection.ApprovalPolicy, "untrusted", "on-request", "never") {
		identifiers = append(identifiers, "approval:"+projection.ApprovalPolicy)
	}
	if allowedValue(projection.SandboxMode, "read-only", "workspace-write", "danger-full-access") {
		identifiers = append(identifiers, "sandbox:"+projection.SandboxMode)
	}
	return evidence("config", "CODEX_HOME config.toml", inventory.ResolutionDeclared, raw, identifiers), nil
}

func safeConfigIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return false
	}
	for _, char := range value {
		if unicode.IsLetter(char) || unicode.IsDigit(char) || strings.ContainsRune("-._/", char) {
			continue
		}
		return false
	}
	return true
}

func allowedValue(value string, allowed ...string) bool {
	for _, candidate := range allowed {
		if value == candidate {
			return true
		}
	}
	return false
}
