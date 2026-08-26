package adapters

import (
	"encoding/json"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

func NewCompozy(executable, workspaceID string) (Adapter, error) {
	ids := map[string]inventory.ProbeID{
		"version": "compozy.version", "status": "compozy.status",
		"config_global": "compozy.config.global", "config_workspace": "compozy.config.workspace",
		"config": "compozy.config.show", "agents": "compozy.agents", "providers": "compozy.providers",
		"models": "compozy.models", "skills": "compozy.skills", "toolsets": "compozy.toolsets", "tools": "compozy.tools",
	}
	args := map[string][]string{
		"version": {"version"}, "status": {"status", "-o", "json"},
		"config_global":    {"config", "path", "--scope", "global", "--workspace", workspaceID, "-o", "json"},
		"config_workspace": {"config", "path", "--scope", "workspace", "--workspace", workspaceID, "-o", "json"},
		"config":           {"config", "show", "--workspace", workspaceID, "-o", "json"},
		"agents":           {"agent", "list", "--workspace", workspaceID, "-o", "json"},
		"providers":        {"provider", "list", "-o", "json"},
		"models":           {"provider", "models", "list", "-o", "json"},
		"skills":           {"skill", "list", "--workspace", workspaceID, "-o", "json"},
		"toolsets":         {"toolsets", "list", "-o", "json"}, "tools": {"tool", "list", "-o", "json"},
	}
	order := []string{"version", "status", "config_global", "config_workspace", "config", "agents", "providers", "models", "skills", "toolsets", "tools"}
	return orderedAdapter(inventory.ExecutorCompozy, executable, order, ids, args, "version", "status", func(outputs map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot {
		return normalizeCompozy(ids, outputs)
	})
}

func normalizeCompozy(ids map[string]inventory.ProbeID, outputs map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot {
	version, versionOK := versionEvidence(outputs[ids["version"]], "compozy version", "Version")
	snapshot := inventory.ExecutorSnapshot{ID: inventory.ExecutorCompozy, Version: version, Diagnostics: diagnosticForVersion(versionOK)}

	var models struct {
		Models []struct {
			ProviderID        string `json:"provider_id"`
			ModelID           string `json:"model_id"`
			AvailabilityState string `json:"availability_state"`
			Hidden            bool   `json:"hidden"`
			Deprecated        bool   `json:"deprecated"`
		} `json:"models"`
	}
	modelRaw := outputs[ids["models"]]
	modelIDs := make([]string, 0)
	if len(modelRaw) > 0 && json.Unmarshal(modelRaw, &models) == nil {
		for _, model := range models.Models {
			if model.Hidden || model.Deprecated || !safePublicIdentifier(model.ProviderID) || !safePublicIdentifier(model.ModelID) {
				continue
			}
			modelIDs = append(modelIDs, model.ProviderID+"/"+model.ModelID)
		}
		snapshot.Capabilities = append(snapshot.Capabilities, evidence("models", "compozy provider models list", inventory.ResolutionResolved, modelRaw, modelIDs))
	} else {
		snapshot.Capabilities = append(snapshot.Capabilities, unknownEvidence("models", "compozy provider models list", "probe_unavailable"))
	}

	for _, entry := range []struct{ key, name, source string }{
		{"agents", "agents", "compozy agent list"}, {"skills", "skills", "compozy skill list"},
		{"toolsets", "toolsets", "compozy toolsets list"}, {"tools", "tools", "compozy tool list"},
	} {
		raw := outputs[ids[entry.key]]
		if len(raw) == 0 || !json.Valid(raw) {
			snapshot.Capabilities = append(snapshot.Capabilities, unknownEvidence(entry.name, entry.source, "probe_unavailable"))
			continue
		}
		snapshot.Capabilities = append(snapshot.Capabilities, evidence(entry.name, entry.source, inventory.ResolutionResolved, raw, nil))
	}
	if raw := outputs[ids["config"]]; len(raw) > 0 && json.Valid(raw) {
		snapshot.Capabilities = append(snapshot.Capabilities, evidence("config", "compozy config show", inventory.ResolutionResolved, raw, nil))
	} else {
		snapshot.Capabilities = append(snapshot.Capabilities, unknownEvidence("config", "compozy config show", "probe_unavailable"))
	}
	snapshot.CredentialState = inventory.CredentialUnknown
	appendSkew(&snapshot, schemaSkewed(outputs[ids["status"]]))
	return snapshot
}
