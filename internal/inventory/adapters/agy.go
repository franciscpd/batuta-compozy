package adapters

import (
	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

func NewAgy(executable string) (Adapter, error) {
	ids := map[string]inventory.ProbeID{
		"version": "agy.version",
		"agents":  "agy.agents",
		"plugins": "agy.plugins",
	}
	args := map[string][]string{
		"version": {"--version"},
		"agents":  {"agent"},
		"plugins": {"plugin", "list"},
	}
	return orderedAdapter(inventory.ExecutorAgy, executable, []string{"version", "agents", "plugins"}, ids, args, "version", "plugins", func(outputs map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot {
		return normalizeAgy(ids, outputs)
	})
}

func normalizeAgy(ids map[string]inventory.ProbeID, outputs map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot {
	version, versionOK := versionEvidence(outputs[ids["version"]], "agy --version", "")
	snapshot := inventory.ExecutorSnapshot{
		ID: inventory.ExecutorAgy, Version: version, Diagnostics: diagnosticForVersion(versionOK),
		CredentialState: inventory.CredentialUnknown,
	}
	for _, entry := range []struct {
		key, name, source string
	}{
		{key: "agents", name: "agents", source: "agy agent"},
		{key: "plugins", name: "plugins", source: "agy plugin list"},
	} {
		raw := outputs[ids[entry.key]]
		identifiers := safeLineIdentifiers(raw)
		if len(identifiers) == 0 {
			snapshot.Capabilities = append(snapshot.Capabilities, unknownEvidence(entry.name, entry.source, "probe_unavailable"))
			continue
		}
		snapshot.Capabilities = append(snapshot.Capabilities, evidence(entry.name, entry.source, inventory.ResolutionResolved, raw, identifiers))
	}
	appendSkew(&snapshot, schemaSkewed(outputs[ids["plugins"]]))
	return snapshot
}

func safeLineIdentifiers(raw []byte) []string {
	values := make([]string, 0)
	for _, value := range nonemptyLines(raw) {
		if safePublicIdentifier(value) {
			values = append(values, value)
		}
	}
	return cleanIdentifiers(values)
}
