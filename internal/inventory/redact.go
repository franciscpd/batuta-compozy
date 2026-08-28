package inventory

import (
	"encoding/json"
	"strings"
)

const redactedValue = "[redacted]"

var safeOutputKeys = map[string]struct{}{
	"availability":               {},
	"capabilities":               {},
	"code":                       {},
	"compozy_catalog_generation": {},
	"configuration":              {},
	"credential_state":           {},
	"diagnostic_code":            {},
	"diagnostics":                {},
	"digest":                     {},
	"executor_id":                {},
	"executors":                  {},
	"health":                     {},
	"identifiers":                {},
	"instructions":               {},
	"name":                       {},
	"model_id":                   {},
	"provider_bindings":          {},
	"provider_id":                {},
	"schema_version":             {},
	"source":                     {},
	"state":                      {},
	"summary":                    {},
	"version":                    {},
}

// Redact projects a value onto the closed inventory output vocabulary and
// removes explicit secret canaries before the value reaches serialization.
func Redact(value any, secrets ...string) any {
	return redactValue(normalizeValue(value), cleanSecrets(secrets))
}

func normalizeValue(value any) any {
	switch value.(type) {
	case nil, bool, string, float64, json.Number, map[string]any, []any:
		return value
	default:
		payload, err := json.Marshal(value)
		if err != nil {
			return nil
		}
		var normalized any
		if err := json.Unmarshal(payload, &normalized); err != nil {
			return nil
		}
		return normalized
	}
}

func redactValue(value any, secrets []string) any {
	switch typed := value.(type) {
	case map[string]any:
		redacted := make(map[string]any, len(typed))
		for key, child := range typed {
			if _, ok := safeOutputKeys[key]; !ok {
				continue
			}
			redacted[key] = redactValue(normalizeValue(child), secrets)
		}
		return redacted
	case []any:
		redacted := make([]any, len(typed))
		for i := range typed {
			redacted[i] = redactValue(normalizeValue(typed[i]), secrets)
		}
		return redacted
	case string:
		for _, secret := range secrets {
			if strings.Contains(typed, secret) {
				return redactedValue
			}
		}
		return typed
	default:
		return typed
	}
}

func cleanSecrets(secrets []string) []string {
	cleaned := make([]string, 0, len(secrets))
	for _, secret := range secrets {
		if secret = strings.TrimSpace(secret); secret != "" {
			cleaned = append(cleaned, secret)
		}
	}
	return cleaned
}
