package inventory

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
)

const SchemaVersion = 1

type ExecutorID string

const (
	ExecutorCompozy     ExecutorID = "compozy"
	ExecutorCodex       ExecutorID = "codex"
	ExecutorOpenCode    ExecutorID = "opencode"
	ExecutorCursorAgent ExecutorID = "cursor-agent"
	ExecutorClaude      ExecutorID = "claude"
	ExecutorAgy         ExecutorID = "agy"
)

type ResolutionState string

const (
	ResolutionResolved ResolutionState = "resolved"
	ResolutionDeclared ResolutionState = "declared"
	ResolutionUnknown  ResolutionState = "unknown"
)

type CredentialState string

const (
	CredentialConfigured CredentialState = "configured"
	CredentialMissing    CredentialState = "missing"
	CredentialUnknown    CredentialState = "unknown"
)

type AvailabilityState string

const (
	AvailabilityAvailable AvailabilityState = "available"
	AvailabilityMissing   AvailabilityState = "missing"
	AvailabilityUnknown   AvailabilityState = "unknown"
)

type Evidence struct {
	Name           string          `json:"name"`
	Source         string          `json:"source"`
	State          ResolutionState `json:"state"`
	Digest         string          `json:"digest,omitempty"`
	Identifiers    []string        `json:"identifiers,omitempty"`
	DiagnosticCode string          `json:"diagnostic_code,omitempty"`
}

type Diagnostic struct {
	Code    string `json:"code"`
	Summary string `json:"summary,omitempty"`
}

type ProviderBinding struct {
	ProviderID string `json:"provider_id"`
	ModelID    string `json:"model_id,omitempty"`
}

// ModelCost carries catalog list rates in USD per million tokens.
type ModelCost struct {
	InputPerMillion      float64 `json:"input_per_million,omitempty"`
	OutputPerMillion     float64 `json:"output_per_million,omitempty"`
	CacheReadPerMillion  float64 `json:"cache_read_per_million,omitempty"`
	CacheWritePerMillion float64 `json:"cache_write_per_million,omitempty"`
}

type CatalogModelCost struct {
	ProviderID string    `json:"provider_id"`
	ModelID    string    `json:"model_id"`
	Cost       ModelCost `json:"cost"`
}

type ExecutorSnapshot struct {
	ID                   ExecutorID         `json:"executor_id"`
	Availability         AvailabilityState  `json:"availability"`
	Version              Evidence           `json:"version"`
	Health               Evidence           `json:"health,omitempty"`
	ConfigurationDigests []Evidence         `json:"configuration,omitempty"`
	InstructionDigests   []Evidence         `json:"instructions,omitempty"`
	Capabilities         []Evidence         `json:"capabilities,omitempty"`
	CredentialState      CredentialState    `json:"credential_state,omitempty"`
	ProviderBindings     []ProviderBinding  `json:"provider_bindings,omitempty"`
	CatalogModelCosts    []CatalogModelCost `json:"catalog_model_costs,omitempty"`
	Diagnostics          []Diagnostic       `json:"diagnostics,omitempty"`
}

type InventorySnapshot struct {
	SchemaVersion            int                `json:"schema_version"`
	CompozyCatalogGeneration string             `json:"compozy_catalog_generation,omitempty"`
	Executors                []ExecutorSnapshot `json:"executors"`
	Digest                   string             `json:"digest"`
}

func NewSnapshot(catalogGeneration string, executors []ExecutorSnapshot) (InventorySnapshot, error) {
	snapshot := InventorySnapshot{
		SchemaVersion:            SchemaVersion,
		CompozyCatalogGeneration: strings.TrimSpace(catalogGeneration),
		Executors:                cloneExecutors(executors),
	}
	canonicalizeExecutors(snapshot.Executors)
	if err := snapshot.Validate(); err != nil {
		return InventorySnapshot{}, err
	}

	payload, err := json.Marshal(struct {
		SchemaVersion            int                `json:"schema_version"`
		CompozyCatalogGeneration string             `json:"compozy_catalog_generation,omitempty"`
		Executors                []ExecutorSnapshot `json:"executors"`
	}{
		SchemaVersion:            snapshot.SchemaVersion,
		CompozyCatalogGeneration: snapshot.CompozyCatalogGeneration,
		Executors:                snapshot.Executors,
	})
	if err != nil {
		return InventorySnapshot{}, fmt.Errorf("inventory: encode canonical snapshot: %w", err)
	}
	digest := sha256.Sum256(payload)
	snapshot.Digest = "sha256:" + hex.EncodeToString(digest[:])
	return snapshot, nil
}

func (s InventorySnapshot) Validate() error {
	if s.SchemaVersion != SchemaVersion {
		return fmt.Errorf("inventory: unsupported schema version %d", s.SchemaVersion)
	}
	seen := make(map[ExecutorID]struct{}, len(s.Executors))
	for i := range s.Executors {
		executor := s.Executors[i]
		if !executor.ID.valid() {
			return fmt.Errorf("inventory: unsupported executor %q", executor.ID)
		}
		if _, ok := seen[executor.ID]; ok {
			return fmt.Errorf("inventory: duplicate executor %q", executor.ID)
		}
		seen[executor.ID] = struct{}{}
		if err := validateExecutor(executor); err != nil {
			return fmt.Errorf("inventory: executor %q: %w", executor.ID, err)
		}
	}
	return nil
}

func (id ExecutorID) valid() bool {
	switch id {
	case ExecutorCompozy, ExecutorCodex, ExecutorOpenCode, ExecutorCursorAgent, ExecutorClaude, ExecutorAgy:
		return true
	default:
		return false
	}
}

func (state ResolutionState) valid() bool {
	switch state {
	case ResolutionResolved, ResolutionDeclared, ResolutionUnknown:
		return true
	default:
		return false
	}
}

func validateExecutor(executor ExecutorSnapshot) error {
	if executor.Availability != AvailabilityAvailable &&
		executor.Availability != AvailabilityMissing &&
		executor.Availability != AvailabilityUnknown {
		return fmt.Errorf("unsupported availability %q", executor.Availability)
	}
	groups := [][]Evidence{
		{executor.Version},
		{executor.Health},
		executor.ConfigurationDigests,
		executor.InstructionDigests,
		executor.Capabilities,
	}
	for _, group := range groups {
		for _, evidence := range group {
			if evidence.empty() {
				continue
			}
			if !evidence.State.valid() {
				return fmt.Errorf("evidence %q has unsupported resolution state %q", evidence.Name, evidence.State)
			}
		}
	}
	if executor.CredentialState != "" &&
		executor.CredentialState != CredentialConfigured &&
		executor.CredentialState != CredentialMissing &&
		executor.CredentialState != CredentialUnknown {
		return errors.New("unsupported credential state")
	}
	for _, binding := range executor.ProviderBindings {
		if !validProviderID(binding.ProviderID) || binding.ModelID != "" && !validModelID(binding.ModelID) {
			return errors.New("invalid provider binding")
		}
	}
	for _, entry := range executor.CatalogModelCosts {
		if !validProviderID(entry.ProviderID) || !validModelID(entry.ModelID) {
			return errors.New("invalid catalog model cost binding")
		}
		for _, rate := range []float64{
			entry.Cost.InputPerMillion, entry.Cost.OutputPerMillion,
			entry.Cost.CacheReadPerMillion, entry.Cost.CacheWritePerMillion,
		} {
			if rate < 0 || math.IsNaN(rate) || math.IsInf(rate, 0) {
				return errors.New("invalid catalog model cost rate")
			}
		}
	}
	return nil
}

func validProviderID(value string) bool { return validBindingID(value, "-._/+") }

func validModelID(value string) bool { return validBindingID(value, "-._/+[],:=") }

func validBindingID(value, punctuation string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 || strings.ContainsAny(value, "\r\n\t") {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune(punctuation, char) {
			continue
		}
		return false
	}
	return true
}

func (e Evidence) empty() bool {
	return e.Name == "" &&
		e.Source == "" &&
		e.State == "" &&
		e.Digest == "" &&
		len(e.Identifiers) == 0 &&
		e.DiagnosticCode == ""
}

func cloneExecutors(executors []ExecutorSnapshot) []ExecutorSnapshot {
	cloned := make([]ExecutorSnapshot, len(executors))
	for i := range executors {
		cloned[i] = executors[i]
		cloned[i].ConfigurationDigests = cloneEvidence(executors[i].ConfigurationDigests)
		cloned[i].InstructionDigests = cloneEvidence(executors[i].InstructionDigests)
		cloned[i].Capabilities = cloneEvidence(executors[i].Capabilities)
		cloned[i].Version.Identifiers = slices.Clone(executors[i].Version.Identifiers)
		cloned[i].Health.Identifiers = slices.Clone(executors[i].Health.Identifiers)
		cloned[i].ProviderBindings = slices.Clone(executors[i].ProviderBindings)
		cloned[i].CatalogModelCosts = slices.Clone(executors[i].CatalogModelCosts)
		cloned[i].Diagnostics = slices.Clone(executors[i].Diagnostics)
	}
	return cloned
}

func cloneEvidence(values []Evidence) []Evidence {
	cloned := make([]Evidence, len(values))
	for i := range values {
		cloned[i] = values[i]
		cloned[i].Identifiers = slices.Clone(values[i].Identifiers)
	}
	return cloned
}

func canonicalizeExecutors(executors []ExecutorSnapshot) {
	for i := range executors {
		canonicalizeEvidence(executors[i].ConfigurationDigests)
		canonicalizeEvidence(executors[i].InstructionDigests)
		canonicalizeEvidence(executors[i].Capabilities)
		slices.Sort(executors[i].Version.Identifiers)
		slices.Sort(executors[i].Health.Identifiers)
		slices.SortFunc(executors[i].ProviderBindings, compareProviderBindings)
		executors[i].ProviderBindings = slices.Compact(executors[i].ProviderBindings)
		slices.SortFunc(executors[i].CatalogModelCosts, func(a, b CatalogModelCost) int {
			if value := strings.Compare(a.ProviderID, b.ProviderID); value != 0 {
				return value
			}
			return strings.Compare(a.ModelID, b.ModelID)
		})
		executors[i].CatalogModelCosts = slices.Compact(executors[i].CatalogModelCosts)
		slices.SortFunc(executors[i].Diagnostics, func(a, b Diagnostic) int {
			if value := strings.Compare(a.Code, b.Code); value != 0 {
				return value
			}
			return strings.Compare(a.Summary, b.Summary)
		})
	}
	slices.SortFunc(executors, func(a, b ExecutorSnapshot) int {
		return strings.Compare(string(a.ID), string(b.ID))
	})
}

func compareProviderBindings(a, b ProviderBinding) int {
	if value := strings.Compare(a.ProviderID, b.ProviderID); value != 0 {
		return value
	}
	return strings.Compare(a.ModelID, b.ModelID)
}

func canonicalizeEvidence(values []Evidence) {
	for i := range values {
		slices.Sort(values[i].Identifiers)
	}
	slices.SortFunc(values, func(a, b Evidence) int {
		for _, value := range []int{
			strings.Compare(a.Name, b.Name),
			strings.Compare(a.Source, b.Source),
			strings.Compare(string(a.State), string(b.State)),
			strings.Compare(a.Digest, b.Digest),
			slices.Compare(a.Identifiers, b.Identifiers),
		} {
			if value != 0 {
				return value
			}
		}
		return strings.Compare(a.DiagnosticCode, b.DiagnosticCode)
	})
}
