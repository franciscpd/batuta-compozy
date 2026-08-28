package adapters

import (
	"encoding/json"
	"strings"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

func NewOpenCode(executable string) (Adapter, error) {
	ids := map[string]inventory.ProbeID{"version": "opencode.version", "config": "opencode.config", "paths": "opencode.paths", "agents": "opencode.agents", "skills": "opencode.skills", "mcp": "opencode.mcp", "auth": "opencode.auth", "models": "opencode.models"}
	args := map[string][]string{
		"version": {"--version"}, "config": {"debug", "config"}, "paths": {"debug", "paths"}, "agents": {"agent", "list"},
		"skills": {"debug", "skill"}, "mcp": {"mcp", "list"}, "auth": {"auth", "list"}, "models": {"models"},
	}
	order := []string{"version", "config", "paths", "agents", "skills", "mcp", "auth", "models"}
	a, err := orderedAdapter(inventory.ExecutorOpenCode, executable, order, ids, args, "version", "config", func(outputs map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot {
		return normalizeOpenCode(ids, outputs)
	})
	if err != nil {
		return nil, err
	}
	a.expand = func(outputs map[inventory.ProbeID][]byte) []inventory.ProbeSpec {
		var listed []struct {
			Name string `json:"name"`
		}
		if json.Unmarshal(outputs[ids["agents"]], &listed) != nil {
			return nil
		}
		names := make([]string, 0, len(listed))
		for _, item := range listed {
			if validDynamicIdentifier(item.Name, false) {
				names = append(names, item.Name)
			}
		}
		names = cleanIdentifiers(names)
		specs := make([]inventory.ProbeSpec, 0, len(names))
		for _, name := range names {
			specs = append(specs, inventory.ProbeSpec{
				ID: inventory.ProbeID("opencode.agent." + name), Executor: inventory.ExecutorOpenCode,
				Executable: executable, Args: []string{"debug", "agent", name},
			})
		}
		return specs
	}
	return a, nil
}

func normalizeOpenCode(ids map[string]inventory.ProbeID, outputs map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot {
	version, versionOK := versionEvidence(outputs[ids["version"]], "opencode --version", "")
	snapshot := inventory.ExecutorSnapshot{ID: inventory.ExecutorOpenCode, Version: version, Diagnostics: diagnosticForVersion(versionOK)}
	modelRaw := outputs[ids["models"]]
	modelIDs := make([]string, 0)
	for _, modelID := range nonemptyLines(modelRaw) {
		if strings.Contains(modelID, "/") && safePublicIdentifier(modelID) {
			modelIDs = append(modelIDs, "opencode/"+modelID)
			snapshot.ProviderBindings = append(snapshot.ProviderBindings, inventory.ProviderBinding{ProviderID: "opencode", ModelID: modelID})
		}
	}
	if len(modelIDs) > 0 {
		snapshot.Capabilities = append(snapshot.Capabilities, evidence("models", "opencode models", inventory.ResolutionResolved, modelRaw, modelIDs))
	} else {
		snapshot.Capabilities = append(snapshot.Capabilities, unknownEvidence("models", "opencode models", "probe_unavailable"))
	}
	configRaw := outputs[ids["config"]]
	if len(configRaw) > 0 && json.Valid(configRaw) {
		snapshot.Capabilities = append(snapshot.Capabilities, evidence("config", "opencode debug config", inventory.ResolutionResolved, configRaw, nil))
	} else {
		snapshot.Capabilities = append(snapshot.Capabilities, unknownEvidence("config", "opencode debug config", "probe_unavailable"))
	}
	for _, entry := range []struct{ key, name string }{{"agents", "agents"}, {"skills", "skills"}, {"mcp", "mcp"}} {
		raw := outputs[ids[entry.key]]
		state := inventory.ResolutionUnknown
		if len(raw) > 0 && json.Valid(raw) {
			state = inventory.ResolutionResolved
		}
		snapshot.Capabilities = append(snapshot.Capabilities, evidence(entry.name, "opencode "+entry.key, state, raw, nil))
	}
	if len(nonemptyLines(outputs[ids["auth"]])) > 0 {
		snapshot.CredentialState = inventory.CredentialConfigured
	} else {
		snapshot.CredentialState = inventory.CredentialUnknown
	}
	appendSkew(&snapshot, schemaSkewed(configRaw))
	return snapshot
}

func nonemptyLines(raw []byte) []string {
	lines := strings.Split(string(raw), "\n")
	values := make([]string, 0, len(lines))
	for _, line := range lines {
		if line = strings.TrimSpace(line); line != "" {
			values = append(values, line)
		}
	}
	return cleanIdentifiers(values)
}
