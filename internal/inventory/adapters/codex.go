package adapters

import (
	"encoding/json"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

func NewCodex(executable string) (Adapter, error) {
	ids := map[string]inventory.ProbeID{"version": "codex.version", "doctor": "codex.doctor", "mcp": "codex.mcp", "plugins": "codex.plugins", "marketplaces": "codex.marketplaces", "models": "codex.models"}
	args := map[string][]string{
		"version": {"--version"}, "doctor": {"doctor", "--json", "--summary"}, "mcp": {"mcp", "list", "--json"},
		"plugins": {"plugin", "list", "--json"}, "marketplaces": {"plugin", "marketplace", "list", "--json"}, "models": {"debug", "models", "--bundled"},
	}
	order := []string{"version", "doctor", "mcp", "plugins", "marketplaces", "models"}
	return orderedAdapter(inventory.ExecutorCodex, executable, order, ids, args, "version", "doctor", func(outputs map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot {
		return normalizeCodex(ids, outputs)
	})
}

func normalizeCodex(ids map[string]inventory.ProbeID, outputs map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot {
	version, versionOK := versionEvidence(outputs[ids["version"]], "codex --version", "")
	snapshot := inventory.ExecutorSnapshot{ID: inventory.ExecutorCodex, Version: version, Diagnostics: diagnosticForVersion(versionOK)}
	raw := outputs[ids["models"]]
	var models struct {
		Models []struct {
			Slug string `json:"slug"`
		} `json:"models"`
	}
	if len(raw) > 0 && json.Unmarshal(raw, &models) == nil {
		ids := make([]string, 0, len(models.Models))
		for _, model := range models.Models {
			if safePublicIdentifier(model.Slug) {
				ids = append(ids, "codex/"+model.Slug)
			}
		}
		snapshot.Capabilities = append(snapshot.Capabilities, evidence("models", "codex debug models --bundled", inventory.ResolutionResolved, raw, ids))
	} else {
		snapshot.Capabilities = append(snapshot.Capabilities, unknownEvidence("models", "codex debug models --bundled", "probe_unavailable"))
	}
	snapshot.Capabilities = append(snapshot.Capabilities, evidence("config", "CODEX_HOME config", inventory.ResolutionDeclared, nil, nil))
	for _, entry := range []struct{ key, name string }{{"mcp", "mcp"}, {"plugins", "plugins"}, {"marketplaces", "marketplaces"}} {
		raw := outputs[ids[entry.key]]
		state := inventory.ResolutionUnknown
		if len(raw) > 0 && json.Valid(raw) {
			state = inventory.ResolutionDeclared
		}
		snapshot.Capabilities = append(snapshot.Capabilities, evidence(entry.name, "codex "+entry.key, state, raw, nil))
	}
	snapshot.CredentialState = inventory.CredentialUnknown
	appendSkew(&snapshot, schemaSkewed(outputs[ids["doctor"]]))
	return snapshot
}
