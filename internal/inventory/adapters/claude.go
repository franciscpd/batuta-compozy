package adapters

import (
	"encoding/json"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

func NewClaude(executable string) (Adapter, error) {
	ids := map[string]inventory.ProbeID{
		"version": "claude.version",
		"plugins": "claude.plugins",
	}
	args := map[string][]string{
		"version": {"--version"},
		"plugins": {"plugin", "list", "--json"},
	}
	return orderedAdapter(inventory.ExecutorClaude, executable, []string{"version", "plugins"}, ids, args, "version", "plugins", func(outputs map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot {
		return normalizeClaude(ids, outputs)
	})
}

func normalizeClaude(ids map[string]inventory.ProbeID, outputs map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot {
	version, versionOK := versionEvidence(outputs[ids["version"]], "claude --version", "")
	snapshot := inventory.ExecutorSnapshot{
		ID: inventory.ExecutorClaude, Version: version, Diagnostics: diagnosticForVersion(versionOK),
		ProviderBindings: []inventory.ProviderBinding{{ProviderID: "claude"}},
		CredentialState:  inventory.CredentialUnknown,
	}
	raw := outputs[ids["plugins"]]
	plugins, ok := claudePluginIdentifiers(raw)
	if ok {
		snapshot.Capabilities = append(snapshot.Capabilities, evidence("plugins", "claude plugin list --json", inventory.ResolutionResolved, raw, plugins))
	} else {
		snapshot.Capabilities = append(snapshot.Capabilities, unknownEvidence("plugins", "claude plugin list --json", "probe_unavailable"))
	}
	appendSkew(&snapshot, schemaSkewed(raw))
	return snapshot
}

func claudePluginIdentifiers(raw []byte) ([]string, bool) {
	if len(raw) == 0 {
		return nil, false
	}
	var envelope struct {
		Plugins []map[string]any `json:"plugins"`
	}
	if err := json.Unmarshal(raw, &envelope); err == nil && envelope.Plugins != nil {
		return safeObjectIdentifiers(envelope.Plugins, "id", "name"), true
	}
	var list []map[string]any
	if err := json.Unmarshal(raw, &list); err != nil {
		return nil, false
	}
	return safeObjectIdentifiers(list, "id", "name"), true
}

func safeObjectIdentifiers(values []map[string]any, fields ...string) []string {
	identifiers := make([]string, 0, len(values))
	for _, value := range values {
		for _, field := range fields {
			identifier, ok := value[field].(string)
			if ok && safePublicIdentifier(identifier) {
				identifiers = append(identifiers, identifier)
				break
			}
		}
	}
	return cleanIdentifiers(identifiers)
}
