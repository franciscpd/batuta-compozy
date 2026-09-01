package adapters

import (
	"encoding/json"
	"math"
	"slices"
	"strings"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

type compozyModelIdentity struct {
	ProviderID   string `json:"provider_id"`
	ModelID      string `json:"model_id"`
	Availability string `json:"availability"`
}

func NewCompozy(executable, workspaceID string) (Adapter, error) {
	ids := map[string]inventory.ProbeID{
		"version": "compozy.version", "status": "compozy.status",
		"config_global": "compozy.config.global", "config_workspace": "compozy.config.workspace",
		"config": "compozy.config.show", "agents": "compozy.agents", "providers": "compozy.providers",
		"models": "compozy.models", "skills": "compozy.skills", "toolsets": "compozy.toolsets", "tools": "compozy.tools",
	}
	args := map[string][]string{
		"version": {"version", "-o", "json"}, "status": {"status", "-o", "json"},
		"config_global":    {"config", "path", "--scope", "global", "--workspace", workspaceID, "-o", "json"},
		"config_workspace": {"config", "path", "--scope", "workspace", "--workspace", workspaceID, "-o", "json"},
		"config":           {"config", "show", "--workspace", workspaceID, "-o", "json"},
		"agents":           {"agent", "list", "--workspace", workspaceID, "-o", "json"},
		"providers":        {"provider", "list", "-o", "json"},
		"models":           {"provider", "models", "list", "--all", "-o", "json"},
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
			ProviderID        string               `json:"provider_id"`
			ModelID           string               `json:"model_id"`
			AvailabilityState string               `json:"availability_state"`
			Hidden            bool                 `json:"hidden"`
			Deprecated        bool                 `json:"deprecated"`
			Cost              *inventory.ModelCost `json:"cost"`
		} `json:"models"`
	}
	modelRaw := outputs[ids["models"]]
	modelIDs := make([]string, 0)
	unknownModelIDs := make([]string, 0)
	if len(modelRaw) > 0 && json.Unmarshal(modelRaw, &models) == nil {
		identities := make([]compozyModelIdentity, 0, len(models.Models))
		for _, model := range models.Models {
			if model.Hidden || model.Deprecated || !safePublicIdentifier(model.ProviderID) || !safeRuntimeModelIdentifier(model.ModelID) {
				continue
			}
			exactID := model.ProviderID + "/" + model.ModelID
			availability := ""
			switch {
			case liveAvailableModel(model.AvailabilityState):
				availability = "available"
				modelIDs = append(modelIDs, exactID)
			case model.AvailabilityState == string(inventory.AvailabilityUnknown):
				availability = "unknown"
				unknownModelIDs = append(unknownModelIDs, exactID)
			default:
				continue
			}
			identities = append(identities, compozyModelIdentity{
				ProviderID: model.ProviderID, ModelID: model.ModelID, Availability: availability,
			})
			if cost := model.Cost; cost != nil && pricedModelCost(*cost) {
				snapshot.CatalogModelCosts = append(snapshot.CatalogModelCosts, inventory.CatalogModelCost{
					ProviderID: model.ProviderID, ModelID: model.ModelID, Cost: *cost,
				})
			}
		}
		slices.SortFunc(identities, func(a, b compozyModelIdentity) int {
			if value := strings.Compare(a.ProviderID, b.ProviderID); value != 0 {
				return value
			}
			if value := strings.Compare(a.ModelID, b.ModelID); value != 0 {
				return value
			}
			return strings.Compare(a.Availability, b.Availability)
		})
		semanticModelRaw, _ := json.Marshal(identities)
		snapshot.Capabilities = append(snapshot.Capabilities, evidence("models", "compozy provider models list", inventory.ResolutionResolved, semanticModelRaw, modelIDs))
		if len(unknownModelIDs) > 0 {
			snapshot.Capabilities = append(snapshot.Capabilities, evidence(
				"catalog_models_unknown", "compozy provider models list", inventory.ResolutionResolved, semanticModelRaw, unknownModelIDs,
			))
		}
	} else {
		snapshot.Capabilities = append(snapshot.Capabilities, unknownEvidence("models", "compozy provider models list", "probe_unavailable"))
	}

	var providers struct {
		Providers []struct {
			Name       string `json:"name"`
			AuthStatus struct {
				State string `json:"state"`
			} `json:"auth_status"`
		} `json:"providers"`
	}
	providerRaw := outputs[ids["providers"]]
	providerAuth := make([]string, 0)
	if len(providerRaw) > 0 && json.Unmarshal(providerRaw, &providers) == nil {
		for _, provider := range providers.Providers {
			if !safePublicIdentifier(provider.Name) {
				continue
			}
			providerAuth = append(providerAuth, provider.Name+"="+string(reducedProviderCredentialState(provider.AuthStatus.State)))
		}
		snapshot.Capabilities = append(snapshot.Capabilities, evidence("provider_auth", "compozy provider list", inventory.ResolutionResolved, providerRaw, providerAuth))
	} else {
		snapshot.Capabilities = append(snapshot.Capabilities, unknownEvidence("provider_auth", "compozy provider list", "probe_unavailable"))
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

func reducedProviderCredentialState(state string) inventory.CredentialState {
	switch strings.TrimSpace(state) {
	case "configured", "authenticated", "none":
		return inventory.CredentialConfigured
	case "missing_cli", "missing_credential", "needs_login", "permission_denied":
		return inventory.CredentialMissing
	default:
		return inventory.CredentialUnknown
	}
}

func pricedModelCost(cost inventory.ModelCost) bool {
	for _, rate := range []float64{
		cost.InputPerMillion, cost.OutputPerMillion, cost.CacheReadPerMillion, cost.CacheWritePerMillion,
	} {
		if rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
			return false
		}
	}
	return cost != inventory.ModelCost{}
}

func liveAvailableModel(state string) bool {
	return state == "available_live" || state == "available"
}

func safeRuntimeModelIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 || containsSensitiveSegment(value) {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-._/+[],:=", char) {
			continue
		}
		return false
	}
	return true
}
