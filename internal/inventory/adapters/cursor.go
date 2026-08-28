package adapters

import (
	"strings"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

func NewCursor(executable string) (Adapter, error) {
	ids := map[string]inventory.ProbeID{"version": "cursor.version", "status": "cursor.status", "models": "cursor.models", "mcp": "cursor.mcp"}
	args := map[string][]string{"version": {"--version"}, "status": {"status"}, "models": {"models"}, "mcp": {"mcp", "list"}}
	order := []string{"version", "status", "models", "mcp"}
	a, err := orderedAdapter(inventory.ExecutorCursorAgent, executable, order, ids, args, "version", "status", func(outputs map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot {
		return normalizeCursor(ids, outputs)
	})
	if err != nil {
		return nil, err
	}
	a.expand = func(outputs map[inventory.ProbeID][]byte) []inventory.ProbeSpec {
		names := make([]string, 0)
		for _, line := range strings.Split(string(outputs[ids["mcp"]]), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 2 || fields[len(fields)-1] != "connected" {
				continue
			}
			name := strings.Join(fields[:len(fields)-1], " ")
			if !validDynamicIdentifier(name, true) {
				continue
			}
			names = append(names, name)
		}
		names = cleanIdentifiers(names)
		specs := make([]inventory.ProbeSpec, 0, len(names))
		for _, name := range names {
			specs = append(specs, inventory.ProbeSpec{
				ID: inventory.ProbeID("cursor.mcp-tools." + name), Executor: inventory.ExecutorCursorAgent,
				Executable: executable, Args: []string{"mcp", "list-tools", name},
			})
		}
		return specs
	}
	return a, nil
}

func normalizeCursor(ids map[string]inventory.ProbeID, outputs map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot {
	version, versionOK := versionEvidence(outputs[ids["version"]], "agent --version", "")
	snapshot := inventory.ExecutorSnapshot{ID: inventory.ExecutorCursorAgent, Version: version, Diagnostics: diagnosticForVersion(versionOK)}
	snapshot.ProviderBindings = []inventory.ProviderBinding{{ProviderID: "cursor"}}
	modelRaw := outputs[ids["models"]]
	models := make([]string, 0)
	for _, line := range strings.Split(string(modelRaw), "\n") {
		parts := strings.SplitN(strings.TrimSpace(line), " - ", 2)
		if len(parts) == 2 && safePublicIdentifier(parts[0]) {
			models = append(models, "cursor/"+strings.TrimSpace(parts[0]))
		}
	}
	if len(models) > 0 {
		snapshot.Capabilities = append(snapshot.Capabilities, evidence("models", "agent models", inventory.ResolutionResolved, modelRaw, models))
	} else {
		snapshot.Capabilities = append(snapshot.Capabilities, unknownEvidence("models", "agent models", "probe_unavailable"))
	}
	snapshot.Capabilities = append(snapshot.Capabilities, unknownEvidence("config", "cursor CLI", "surface_unavailable"))
	if strings.Contains(strings.ToLower(string(outputs[ids["status"]])), "auth") {
		snapshot.CredentialState = inventory.CredentialConfigured
	} else {
		snapshot.CredentialState = inventory.CredentialUnknown
	}
	appendSkew(&snapshot, schemaSkewed(outputs[ids["status"]]))
	return snapshot
}
