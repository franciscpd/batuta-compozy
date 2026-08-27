package adapters

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

type Adapter interface {
	ID() inventory.ExecutorID
	StaticSpecs() []inventory.ProbeSpec
	DynamicSpecs(map[inventory.ProbeID][]byte) ([]inventory.ProbeSpec, error)
	Normalize(map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot
	Missing() inventory.ExecutorSnapshot
	VersionProbeID() inventory.ProbeID
	SchemaProbeID() inventory.ProbeID
	ProbeID(string) inventory.ProbeID
}

type adapter struct {
	id         inventory.ExecutorID
	executable string
	specs      []inventory.ProbeSpec
	probeIDs   map[string]inventory.ProbeID
	versionID  inventory.ProbeID
	schemaID   inventory.ProbeID
	normalize  func(map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot
	expand     func(map[inventory.ProbeID][]byte) []inventory.ProbeSpec
}

func (a *adapter) ID() inventory.ExecutorID { return a.id }

func (a *adapter) StaticSpecs() []inventory.ProbeSpec {
	specs := make([]inventory.ProbeSpec, len(a.specs))
	for i := range a.specs {
		specs[i] = a.specs[i]
		specs[i].Args = slices.Clone(a.specs[i].Args)
	}
	return specs
}

func (a *adapter) Normalize(outputs map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot {
	snapshot := a.normalize(outputs)
	snapshot.Availability = inventory.AvailabilityAvailable
	return snapshot
}

func (a *adapter) DynamicSpecs(outputs map[inventory.ProbeID][]byte) ([]inventory.ProbeSpec, error) {
	if a.expand == nil {
		return nil, nil
	}
	specs := a.expand(outputs)
	for _, spec := range specs {
		if spec.Executor != a.id || spec.Executable != a.executable {
			return nil, errors.New("inventory adapter: invalid dynamic probe ownership")
		}
	}
	return specs, nil
}

func (a *adapter) Missing() inventory.ExecutorSnapshot {
	return inventory.ExecutorSnapshot{
		ID:           a.id,
		Availability: inventory.AvailabilityMissing,
		Version: inventory.Evidence{
			Name:           "version",
			Source:         "executable discovery",
			State:          inventory.ResolutionUnknown,
			DiagnosticCode: "executable_missing",
		},
		Capabilities:    []inventory.Evidence{unknownEvidence("models", "executable discovery", "executable_missing")},
		CredentialState: inventory.CredentialUnknown,
		Diagnostics:     []inventory.Diagnostic{{Code: "executable_missing"}},
	}
}

func (a *adapter) VersionProbeID() inventory.ProbeID { return a.versionID }
func (a *adapter) SchemaProbeID() inventory.ProbeID  { return a.schemaID }
func (a *adapter) ProbeID(name string) inventory.ProbeID {
	return a.probeIDs[name]
}

func newAdapter(id inventory.ExecutorID, executable string, probeIDs map[string]inventory.ProbeID, args map[string][]string, versionKey, schemaKey string, normalize func(map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot) (*adapter, error) {
	if strings.TrimSpace(executable) == "" || !filepath.IsAbs(executable) {
		return nil, errors.New("inventory adapter: executable must be absolute")
	}
	specs := make([]inventory.ProbeSpec, 0, len(args))
	for _, key := range sortedKeys(args) {
		probeID, ok := probeIDs[key]
		if !ok {
			return nil, fmt.Errorf("inventory adapter: missing probe ID for %q", key)
		}
		specs = append(specs, inventory.ProbeSpec{
			ID:         probeID,
			Executor:   id,
			Executable: executable,
			Args:       slices.Clone(args[key]),
		})
	}
	return &adapter{
		id:         id,
		executable: executable,
		specs:      specs,
		probeIDs:   probeIDs,
		versionID:  probeIDs[versionKey],
		schemaID:   probeIDs[schemaKey],
		normalize:  normalize,
	}, nil
}

func orderedAdapter(id inventory.ExecutorID, executable string, order []string, probeIDs map[string]inventory.ProbeID, args map[string][]string, versionKey, schemaKey string, normalize func(map[inventory.ProbeID][]byte) inventory.ExecutorSnapshot) (*adapter, error) {
	a, err := newAdapter(id, executable, probeIDs, args, versionKey, schemaKey, normalize)
	if err != nil {
		return nil, err
	}
	byID := make(map[inventory.ProbeID]inventory.ProbeSpec, len(a.specs))
	for _, spec := range a.specs {
		byID[spec.ID] = spec
	}
	a.specs = a.specs[:0]
	for _, key := range order {
		a.specs = append(a.specs, byID[probeIDs[key]])
	}
	return a, nil
}

func sortedKeys(values map[string][]string) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	return keys
}

func versionEvidence(raw []byte, source string, jsonField string) (inventory.Evidence, bool) {
	value := strings.TrimSpace(string(raw))
	if value == "" {
		return unknownEvidence("version", source, "probe_unavailable"), false
	}
	if jsonField != "" {
		var object map[string]any
		if json.Unmarshal(raw, &object) != nil {
			return unknownEvidence("version", source, "malformed_output"), false
		}
		field, ok := object[jsonField].(string)
		if !ok || strings.TrimSpace(field) == "" {
			return unknownEvidence("version", source, "malformed_output"), false
		}
		value = strings.TrimSpace(field)
	} else if strings.HasPrefix(value, "{") || strings.HasPrefix(value, "[") {
		return unknownEvidence("version", source, "malformed_output"), false
	} else {
		value = versionToken(value)
	}
	if !safePublicIdentifier(value) || !strings.Contains(value, ".") {
		return unknownEvidence("version", source, "malformed_output"), false
	}
	return inventory.Evidence{
		Name:        "version",
		Source:      source,
		State:       inventory.ResolutionResolved,
		Digest:      safeDigest(raw),
		Identifiers: []string{value},
	}, true
}

func schemaSkewed(raw []byte) bool {
	if len(raw) == 0 {
		return false
	}
	var object map[string]any
	if json.Unmarshal(raw, &object) != nil {
		return false
	}
	value, ok := object["schema_version"]
	if !ok {
		return false
	}
	switch typed := value.(type) {
	case float64:
		return typed != 1
	case string:
		parsed, err := strconv.Atoi(typed)
		return err == nil && parsed != 1
	default:
		return true
	}
}

func evidence(name, source string, state inventory.ResolutionState, raw []byte, identifiers []string) inventory.Evidence {
	identifiers = cleanIdentifiers(identifiers)
	result := inventory.Evidence{Name: name, Source: source, State: state, Identifiers: identifiers}
	if state != inventory.ResolutionUnknown && len(raw) > 0 {
		result.Digest = safeDigest(raw)
	}
	return result
}

func unknownEvidence(name, source, code string) inventory.Evidence {
	return inventory.Evidence{
		Name:           name,
		Source:         source,
		State:          inventory.ResolutionUnknown,
		DiagnosticCode: code,
	}
}

func safeDigest(raw []byte) string {
	digest := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(digest[:])
}

func cleanIdentifiers(values []string) []string {
	cleaned := make([]string, 0, len(values))
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value == "" || len(value) > 256 || strings.ContainsAny(value, "\r\n\t") || containsSensitiveSegment(value) {
			continue
		}
		cleaned = append(cleaned, value)
	}
	slices.Sort(cleaned)
	return slices.Compact(cleaned)
}

func versionToken(value string) string {
	for _, field := range strings.Fields(value) {
		field = strings.Trim(field, "vV,;()[]")
		if strings.Contains(field, ".") && safePublicIdentifier(field) {
			return field
		}
	}
	return ""
}

func safePublicIdentifier(value string) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 256 || containsSensitiveSegment(value) {
		return false
	}
	for _, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || strings.ContainsRune("-._/+", char) {
			continue
		}
		return false
	}
	return true
}

func containsSensitiveSegment(value string) bool {
	segments := strings.FieldsFunc(strings.ToLower(value), func(char rune) bool {
		return !(char >= 'a' && char <= 'z' || char >= '0' && char <= '9')
	})
	for _, segment := range segments {
		switch segment {
		case "secret", "token", "password", "credential", "credentials", "apikey", "authkey", "bearer":
			return true
		}
	}
	return false
}

func diagnosticForVersion(ok bool) []inventory.Diagnostic {
	if ok {
		return nil
	}
	return []inventory.Diagnostic{{Code: "malformed_output"}}
}

func appendSkew(snapshot *inventory.ExecutorSnapshot, skewed bool) {
	if !skewed {
		return
	}
	snapshot.Diagnostics = append(snapshot.Diagnostics, inventory.Diagnostic{Code: "version_skew"})
	for i := range snapshot.Capabilities {
		if snapshot.Capabilities[i].State == inventory.ResolutionResolved {
			snapshot.Capabilities[i] = unknownEvidence(snapshot.Capabilities[i].Name, snapshot.Capabilities[i].Source, "version_skew")
		}
	}
}

func validDynamicIdentifier(value string, allowSpace bool) bool {
	value = strings.TrimSpace(value)
	if value == "" || len(value) > 128 {
		return false
	}
	for i, char := range value {
		if char >= 'a' && char <= 'z' || char >= 'A' && char <= 'Z' || char >= '0' && char <= '9' || char == '.' || char == '_' || char == '-' || allowSpace && char == ' ' {
			continue
		}
		if i == 0 {
			return false
		}
		return false
	}
	return true
}
