package adapters

import (
	"encoding/json"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

const fixtureSecret = "BATUTA_ADAPTER_SECRET_91ef"

func TestAdaptersUseOnlyClosedCommandShapes(t *testing.T) {
	t.Parallel()

	workspaceID := "ws-fixture"
	tests := []struct {
		name    string
		adapter Adapter
		want    [][]string
	}{
		{
			name:    "compozy",
			adapter: mustNewCompozy(t, "/opt/bin/compozy", workspaceID),
			want: [][]string{
				{"version"},
				{"status", "-o", "json"},
				{"config", "path", "--scope", "global", "--workspace", workspaceID, "-o", "json"},
				{"config", "path", "--scope", "workspace", "--workspace", workspaceID, "-o", "json"},
				{"config", "show", "--workspace", workspaceID, "-o", "json"},
				{"agent", "list", "--workspace", workspaceID, "-o", "json"},
				{"provider", "list", "-o", "json"},
				{"provider", "models", "list", "--all", "-o", "json"},
				{"skill", "list", "--workspace", workspaceID, "-o", "json"},
				{"toolsets", "list", "-o", "json"},
				{"tool", "list", "-o", "json"},
			},
		},
		{
			name:    "codex",
			adapter: mustNewCodex(t, "/opt/bin/codex"),
			want: [][]string{
				{"--version"},
				{"doctor", "--json", "--summary"},
				{"mcp", "list", "--json"},
				{"plugin", "list", "--json"},
				{"plugin", "marketplace", "list", "--json"},
				{"debug", "models", "--bundled"},
			},
		},
		{
			name:    "opencode",
			adapter: mustNewOpenCode(t, "/opt/bin/opencode"),
			want: [][]string{
				{"--version"},
				{"debug", "config"},
				{"debug", "paths"},
				{"agent", "list"},
				{"debug", "skill"},
				{"mcp", "list"},
				{"auth", "list"},
				{"models"},
				{"debug", "agent", "build"},
				{"debug", "agent", "review"},
			},
		},
		{
			name:    "cursor",
			adapter: mustNewCursor(t, "/opt/bin/agent"),
			want: [][]string{
				{"--version"},
				{"status"},
				{"models"},
				{"mcp", "list"},
				{"mcp", "list-tools", "browser"},
			},
		},
		{
			name:    "claude",
			adapter: mustNewClaude(t, "/opt/bin/claude"),
			want: [][]string{
				{"--version"},
				{"plugin", "list", "--json"},
			},
		},
		{
			name:    "agy",
			adapter: mustNewAgy(t, "/opt/bin/agy"),
			want: [][]string{
				{"--version"},
				{"agent"},
				{"plugin", "list"},
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			specs := tt.adapter.StaticSpecs()
			dynamic, err := tt.adapter.DynamicSpecs(fixtureOutputs(t, tt.name))
			if err != nil {
				t.Fatalf("DynamicSpecs() error = %v", err)
			}
			specs = append(specs, dynamic...)
			got := make([][]string, len(specs))
			for i := range specs {
				got[i] = specs[i].Args
			}
			if !slices.EqualFunc(got, tt.want, slices.Equal[[]string]) {
				t.Fatalf("command shapes = %#v, want %#v", got, tt.want)
			}
		})
	}
}

func TestClaudeAdapterUsesOnlyReadOnlyBoundedCommands(t *testing.T) {
	t.Parallel()
	assertStaticCommandShapes(t, mustNewClaude(t, "/opt/bin/claude"), [][]string{{"--version"}, {"plugin", "list", "--json"}})
}

func TestAgyAdapterUsesOnlyReadOnlyBoundedCommands(t *testing.T) {
	t.Parallel()
	assertStaticCommandShapes(t, mustNewAgy(t, "/opt/bin/agy"), [][]string{{"--version"}, {"agent"}, {"plugin", "list"}})
}

func assertStaticCommandShapes(t *testing.T, adapter Adapter, want [][]string) {
	t.Helper()
	specs := adapter.StaticSpecs()
	got := make([][]string, len(specs))
	for i := range specs {
		got[i] = specs[i].Args
	}
	if !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Fatalf("command shapes = %#v, want %#v", got, want)
	}
}

func TestAdaptersRejectUnlistedOrUnsafeDynamicIdentifiers(t *testing.T) {
	t.Parallel()

	opencode := mustNewOpenCode(t, "/opt/bin/opencode")
	outputs := map[inventory.ProbeID][]byte{
		opencode.ProbeID("agents"): []byte(`[{"name":"build"},{"name":"../../escape"},{"name":"not listed; exec"}]`),
	}
	specs, err := opencode.DynamicSpecs(outputs)
	if err != nil {
		t.Fatalf("DynamicSpecs() error = %v", err)
	}
	if got, want := specArgs(specs), [][]string{{"debug", "agent", "build"}}; !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Fatalf("OpenCode dynamic specs = %#v, want %#v", got, want)
	}

	cursor := mustNewCursor(t, "/opt/bin/agent")
	outputs = map[inventory.ProbeID][]byte{
		cursor.ProbeID("mcp"): []byte("browser connected\nmy server connected\n../../escape connected\nfigma disconnected\n"),
	}
	specs, err = cursor.DynamicSpecs(outputs)
	if err != nil {
		t.Fatalf("DynamicSpecs() error = %v", err)
	}
	if got, want := specArgs(specs), [][]string{{"mcp", "list-tools", "browser"}, {"mcp", "list-tools", "my server"}}; !slices.EqualFunc(got, want, slices.Equal[[]string]) {
		t.Fatalf("Cursor dynamic specs = %#v, want %#v", got, want)
	}
}

func TestAdaptersNormalizeInstalledMissingMalformedPartialAndSkewed(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"compozy", "codex", "opencode", "cursor", "claude", "agy"} {
		name := name
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			adapter := fixtureAdapter(t, name)
			full := fixtureOutputs(t, name)

			installed := adapter.Normalize(full)
			if installed.Version.State != inventory.ResolutionResolved {
				t.Fatalf("installed version state = %q, want resolved", installed.Version.State)
			}
			installedEvidence := "models"
			if name == "claude" {
				installedEvidence = "plugins"
			} else if name == "agy" {
				installedEvidence = "agents"
			}
			if !hasEvidenceState(installed.Capabilities, installedEvidence, inventory.ResolutionResolved) {
				t.Fatalf("installed %s evidence = %#v, want resolved", installedEvidence, installed.Capabilities)
			}

			missing := adapter.Missing()
			if missing.Availability != inventory.AvailabilityMissing || missing.Version.State != inventory.ResolutionUnknown || !hasDiagnostic(missing, "executable_missing") {
				t.Fatalf("missing snapshot = %#v, want unknown/executable_missing", missing)
			}
			if installed.Availability != inventory.AvailabilityAvailable {
				t.Fatalf("installed availability = %q, want available", installed.Availability)
			}

			malformedInput := cloneOutputs(full)
			malformedInput[adapter.VersionProbeID()] = []byte("{")
			malformed := adapter.Normalize(malformedInput)
			if malformed.Version.State != inventory.ResolutionUnknown || !hasDiagnostic(malformed, "malformed_output") {
				t.Fatalf("malformed snapshot = %#v, want unknown/malformed_output", malformed)
			}

			partial := adapter.Normalize(map[inventory.ProbeID][]byte{
				adapter.VersionProbeID(): full[adapter.VersionProbeID()],
			})
			if partial.Version.State != inventory.ResolutionResolved || !hasEvidenceState(partial.Capabilities, installedEvidence, inventory.ResolutionUnknown) {
				t.Fatalf("partial snapshot = %#v, want resolved version and unknown %s", partial, installedEvidence)
			}

			skewedInput := cloneOutputs(full)
			skewedInput[adapter.SchemaProbeID()] = []byte(`{"schema_version":99}`)
			skewed := adapter.Normalize(skewedInput)
			if !hasDiagnostic(skewed, "version_skew") || hasEvidenceState(skewed.Capabilities, installedEvidence, inventory.ResolutionResolved) {
				t.Fatalf("skewed snapshot = %#v, want version_skew without resolved models", skewed)
			}
		})
	}
}

func TestClaudeAndAgyAdaptersNormalizeInstalledMissingMalformedPartialAndSkewed(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"claude", "agy"} {
		adapter := fixtureAdapter(t, name)
		installed := adapter.Normalize(fixtureOutputs(t, name))
		if installed.Availability != inventory.AvailabilityAvailable || installed.Version.State != inventory.ResolutionResolved {
			t.Fatalf("%s installed snapshot = %#v", name, installed)
		}
		wantBindings := []inventory.ProviderBinding{{ProviderID: "claude"}}
		if name == "agy" {
			wantBindings = nil
		}
		if !slices.Equal(installed.ProviderBindings, wantBindings) {
			t.Fatalf("%s provider bindings = %#v, want %#v", name, installed.ProviderBindings, wantBindings)
		}
		missing := adapter.Missing()
		if missing.Availability != inventory.AvailabilityMissing || !hasDiagnostic(missing, "executable_missing") {
			t.Fatalf("%s missing snapshot = %#v", name, missing)
		}
		malformed := adapter.Normalize(map[inventory.ProbeID][]byte{adapter.VersionProbeID(): []byte("{")})
		if malformed.Version.State != inventory.ResolutionUnknown || !hasDiagnostic(malformed, "malformed_output") {
			t.Fatalf("%s malformed snapshot = %#v", name, malformed)
		}
		partial := adapter.Normalize(map[inventory.ProbeID][]byte{adapter.VersionProbeID(): fixtureOutputs(t, name)[adapter.VersionProbeID()]})
		if partial.Version.State != inventory.ResolutionResolved {
			t.Fatalf("%s partial snapshot = %#v", name, partial)
		}
		skewedOutputs := fixtureOutputs(t, name)
		skewedOutputs[adapter.SchemaProbeID()] = []byte(`{"schema_version":99}`)
		if skewed := adapter.Normalize(skewedOutputs); !hasDiagnostic(skewed, "version_skew") {
			t.Fatalf("%s skewed snapshot = %#v", name, skewed)
		}
	}
}

func TestAdaptersNeverLeakFixtureSecretCanaries(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"compozy", "codex", "opencode", "cursor", "claude", "agy"} {
		adapter := fixtureAdapter(t, name)
		snapshot := adapter.Normalize(fixtureOutputs(t, name))
		payload, err := json.Marshal(inventory.Redact(snapshot, fixtureSecret))
		if err != nil {
			t.Fatalf("json.Marshal(%s) error = %v", name, err)
		}
		for _, forbidden := range []string{fixtureSecret, "91ef", "/secret/", "apiKey", "instructions", "environment"} {
			if strings.Contains(string(payload), forbidden) {
				t.Fatalf("%s snapshot leaks %q: %s", name, forbidden, payload)
			}
		}
	}
}

func TestClaudeAndAgyAdaptersNeverLeakSecretCanaries(t *testing.T) {
	t.Parallel()

	for _, name := range []string{"claude", "agy"} {
		payload, err := json.Marshal(inventory.Redact(fixtureAdapter(t, name).Normalize(fixtureOutputs(t, name)), fixtureSecret))
		if err != nil {
			t.Fatalf("json.Marshal(%s) error = %v", name, err)
		}
		for _, forbidden := range []string{fixtureSecret, "91ef", "/secret/", "unknown_future"} {
			if strings.Contains(string(payload), forbidden) {
				t.Fatalf("%s snapshot leaks %q: %s", name, forbidden, payload)
			}
		}
	}
}

func TestAdaptersNeverPromoteRawOutputAsSafeIdentifiers(t *testing.T) {
	t.Parallel()

	const secret = "BATUTA_FIELD_SECRET_73ce"
	for _, name := range []string{"compozy", "codex", "opencode", "cursor", "claude", "agy"} {
		adapter := fixtureAdapter(t, name)
		outputs := fixtureOutputs(t, name)
		if name == "compozy" {
			outputs[adapter.VersionProbeID()] = []byte(`{"Version":"` + secret + `"}`)
			outputs[adapter.ProbeID("models")] = []byte(`{"models":[{"provider_id":"cursor","model_id":"` + secret + `"}]}`)
		} else {
			outputs[adapter.VersionProbeID()] = []byte(secret)
			switch name {
			case "codex":
				outputs[adapter.ProbeID("models")] = []byte(`{"models":[{"slug":"` + secret + `"}]}`)
			case "opencode":
				outputs[adapter.ProbeID("models")] = []byte("provider/" + secret + "\n")
			case "cursor":
				outputs[adapter.ProbeID("models")] = []byte(secret + " - injected\n")
			case "claude":
				outputs[adapter.ProbeID("plugins")] = []byte(`{"plugins":[{"id":"` + secret + `"}]}`)
			case "agy":
				outputs[adapter.ProbeID("agents")] = []byte(secret + "\n")
			}
		}
		payload, err := json.Marshal(inventory.Redact(adapter.Normalize(outputs)))
		if err != nil {
			t.Fatalf("json.Marshal(%s) error = %v", name, err)
		}
		if strings.Contains(string(payload), secret) || strings.Contains(string(payload), "73ce") {
			t.Fatalf("%s promoted raw secret as safe identifier: %s", name, payload)
		}
	}
}

func TestAdaptersDistinguishResolvedDeclaredAndUnknown(t *testing.T) {
	t.Parallel()

	want := map[string]map[string]inventory.ResolutionState{
		"compozy":  {"models": inventory.ResolutionResolved, "agents": inventory.ResolutionResolved},
		"codex":    {"models": inventory.ResolutionResolved, "config": inventory.ResolutionDeclared},
		"opencode": {"models": inventory.ResolutionResolved, "config": inventory.ResolutionResolved},
		"cursor":   {"models": inventory.ResolutionResolved, "config": inventory.ResolutionUnknown},
	}
	for name, expectations := range want {
		snapshot := fixtureAdapter(t, name).Normalize(fixtureOutputs(t, name))
		for evidence, state := range expectations {
			if !hasEvidenceState(snapshot.Capabilities, evidence, state) {
				t.Fatalf("%s evidence %q = %#v, want %q", name, evidence, snapshot.Capabilities, state)
			}
		}
	}
}

func TestCompozyAdapterIncludesOnlyLiveAvailableModels(t *testing.T) {
	t.Parallel()

	adapter := mustNewCompozy(t, "/opt/compozy", "workspace-1")
	outputs := map[inventory.ProbeID][]byte{
		adapter.ProbeID("version"): []byte("0.3.0-beta.21"),
		adapter.ProbeID("models"): []byte(`{"models":[
			{"provider_id":"cursor","model_id":"grok-4.6[effort=high,fast=true]","availability_state":"available_live"},
			{"provider_id":"cursor","model_id":"unknown","availability_state":"unknown"},
			{"provider_id":"cursor","model_id":"stale","availability_state":"available_stale"},
			{"provider_id":"cursor","model_id":"down","availability_state":"unavailable_live"}
		]}`),
	}
	snapshot := adapter.Normalize(outputs)
	for _, capability := range snapshot.Capabilities {
		if capability.Name != "models" {
			continue
		}
		if !slices.Equal(capability.Identifiers, []string{"cursor/grok-4.6[effort=high,fast=true]"}) {
			t.Fatalf("model identifiers = %#v, want only available_live", capability.Identifiers)
		}
		return
	}
	t.Fatal("models capability missing")
}

func TestCompozyAdapterRetainsUnknownCatalogModelsForExactExecutorProof(t *testing.T) {
	t.Parallel()

	adapter := mustNewCompozy(t, "/opt/compozy", "workspace-1")
	outputs := map[inventory.ProbeID][]byte{
		adapter.ProbeID("version"): []byte("0.3.0-beta.21"),
		adapter.ProbeID("models"): []byte(`{"models":[
			{"provider_id":"cursor","model_id":"grok-4.6[effort=high,fast=true]","availability_state":"available_live"},
			{"provider_id":"codex","model_id":"gpt-5.6-terra","availability_state":"unknown"},
			{"provider_id":"cursor","model_id":"stale","availability_state":"available_stale"},
			{"provider_id":"cursor","model_id":"down","availability_state":"unavailable_live"},
			{"provider_id":"hidden","model_id":"hidden","availability_state":"unknown","hidden":true},
			{"provider_id":"deprecated","model_id":"deprecated","availability_state":"unknown","deprecated":true}
		]}`),
	}
	snapshot := adapter.Normalize(outputs)
	if got := evidenceIdentifiers(snapshot.Capabilities, "models"); !slices.Equal(got, []string{"cursor/grok-4.6[effort=high,fast=true]"}) {
		t.Fatalf("live models = %#v, want only available_live", got)
	}
	if got := evidenceIdentifiers(snapshot.Capabilities, "catalog_models_unknown"); !slices.Equal(got, []string{"codex/gpt-5.6-terra"}) {
		t.Fatalf("unknown catalog models = %#v, want visible exact unknown pair", got)
	}
}

func TestCompozyNormalizationPreservesExactLiveModelEvidence(t *testing.T) {
	t.Parallel()

	const secret = "BATUTA_PROVIDER_STATUS_SECRET_20bc"
	adapter := mustNewCompozy(t, "/opt/compozy", "workspace-1")
	outputs := map[inventory.ProbeID][]byte{
		adapter.ProbeID("version"): []byte("0.3.0-beta.21"),
		adapter.ProbeID("providers"): []byte(`{"providers":[
			{"name":"claude","auth_status":{"state":"authenticated","message":"` + secret + `","command":"login ` + secret + `"}},
			{"name":"configured","auth_status":{"state":"configured"}},
			{"name":"gemini","auth_status":{"state":"none"}},
			{"name":"cursor","auth_status":{"state":"permission_denied"}},
			{"name":"missing-cli","auth_status":{"state":"missing_cli"}},
			{"name":"bedrock","auth_status":{"state":"missing_credential"}},
			{"name":"needs-login","auth_status":{"state":"needs_login"}},
			{"name":"rate-limited","auth_status":{"state":"rate_limited"}},
			{"name":"transient","auth_status":{"state":"transient"}},
			{"name":"unknown","auth_status":{"state":"unknown"}},
			{"name":"future","auth_status":{"state":"future_auth_state"}}
		]}`),
		adapter.ProbeID("models"): []byte(`{"models":[
			{"provider_id":"claude","model_id":"claude-fixture","availability_state":"available_live"},
			{"provider_id":"gemini","model_id":"gemini/fixture-v1","availability_state":"available"}
		]}`),
	}
	snapshot := adapter.Normalize(outputs)
	if got := evidenceIdentifiers(snapshot.Capabilities, "models"); !slices.Equal(got, []string{"claude/claude-fixture", "gemini/gemini/fixture-v1"}) {
		t.Fatalf("models = %#v, want exact live identifiers", got)
	}
	wantAuth := []string{
		"bedrock=missing", "claude=configured", "configured=configured", "cursor=missing", "future=unknown",
		"gemini=configured", "missing-cli=missing", "needs-login=missing", "rate-limited=unknown",
		"transient=unknown", "unknown=unknown",
	}
	if got := evidenceIdentifiers(snapshot.Capabilities, "provider_auth"); !slices.Equal(got, wantAuth) {
		t.Fatalf("provider auth = %#v, want reduced enum-only evidence %#v", got, wantAuth)
	}
	payload, err := json.Marshal(inventory.Redact(snapshot, secret))
	if err != nil {
		t.Fatalf("json.Marshal() error = %v", err)
	}
	for _, forbidden := range []string{secret, "20bc", "message", "command", "future_auth_state"} {
		if strings.Contains(string(payload), forbidden) {
			t.Fatalf("normalized Compozy output contains %q: %s", forbidden, payload)
		}
	}
}

func evidenceIdentifiers(values []inventory.Evidence, name string) []string {
	for _, value := range values {
		if value.Name == name {
			return value.Identifiers
		}
	}
	return nil
}

func TestExecutorAdaptersEmitUnambiguousCompozyRuntimePairs(t *testing.T) {
	t.Parallel()

	wants := map[string][]string{
		"codex":    {"codex/gpt-5.6-sol"},
		"opencode": {"opencode/anthropic/claude-opus-5", "opencode/openai/gpt-5.6-terra"},
		"cursor":   {"cursor/auto", "cursor/composer-2.5", "cursor/grok-4.6"},
	}
	for name, want := range wants {
		snapshot := fixtureAdapter(t, name).Normalize(fixtureOutputs(t, name))
		var got []string
		for _, capability := range snapshot.Capabilities {
			if capability.Name == "models" {
				got = capability.Identifiers
				break
			}
		}
		if !slices.Equal(got, want) {
			t.Fatalf("%s model runtime pairs = %#v, want %#v", name, got, want)
		}
	}
}

func TestCollectorRejectsDynamicProviderBindingFromRawOutput(t *testing.T) {
	t.Parallel()

	adapter := mustNewOpenCode(t, "/opt/opencode")
	outputs := fixtureOutputs(t, "opencode")
	outputs[adapter.ProbeID("models")] = []byte("invented/model\nopencode/safe-model\n")
	snapshot := adapter.Normalize(outputs)
	want := []inventory.ProviderBinding{
		{ProviderID: "opencode", ModelID: "invented/model"},
		{ProviderID: "opencode", ModelID: "opencode/safe-model"},
	}
	if !slices.Equal(snapshot.ProviderBindings, want) {
		t.Fatalf("provider bindings = %#v, want fixed constructor owner with exact models %#v", snapshot.ProviderBindings, want)
	}
}

func TestUnknownEvidenceDoesNotDigestRejectedVolatileOutput(t *testing.T) {
	t.Parallel()

	adapter := mustNewOpenCode(t, "/opt/opencode")
	firstOutputs := fixtureOutputs(t, "opencode")
	secondOutputs := cloneOutputs(firstOutputs)
	firstOutputs[adapter.ProbeID("agents")] = []byte("agent table rendered at 10:00")
	firstOutputs[adapter.ProbeID("skills")] = []byte("skill table rendered at 10:00")
	secondOutputs[adapter.ProbeID("agents")] = []byte("agent table rendered at 10:01")
	secondOutputs[adapter.ProbeID("skills")] = []byte("skill table rendered at 10:01")
	first, err := inventory.NewSnapshot("catalog", []inventory.ExecutorSnapshot{adapter.Normalize(firstOutputs)})
	if err != nil {
		t.Fatalf("NewSnapshot(first) error = %v", err)
	}
	second, err := inventory.NewSnapshot("catalog", []inventory.ExecutorSnapshot{adapter.Normalize(secondOutputs)})
	if err != nil {
		t.Fatalf("NewSnapshot(second) error = %v", err)
	}
	if first.Digest != second.Digest {
		t.Fatalf("unknown evidence digest = %q then %q, want stable", first.Digest, second.Digest)
	}
}

type fixtureFile map[string]string

func fixtureOutputs(t *testing.T, name string) map[inventory.ProbeID][]byte {
	t.Helper()
	payload, err := os.ReadFile(filepath.Join("testdata", name+".json"))
	if err != nil {
		t.Fatalf("read fixture: %v", err)
	}
	var fixture fixtureFile
	if err := json.Unmarshal(payload, &fixture); err != nil {
		t.Fatalf("decode fixture: %v", err)
	}
	adapter := fixtureAdapter(t, name)
	outputs := make(map[inventory.ProbeID][]byte, len(fixture))
	for key, value := range fixture {
		outputs[adapter.ProbeID(key)] = []byte(value)
	}
	return outputs
}

func fixtureAdapter(t *testing.T, name string) Adapter {
	t.Helper()
	switch name {
	case "compozy":
		return mustNewCompozy(t, "/opt/bin/compozy", "ws-fixture")
	case "codex":
		return mustNewCodex(t, "/opt/bin/codex")
	case "opencode":
		return mustNewOpenCode(t, "/opt/bin/opencode")
	case "cursor":
		return mustNewCursor(t, "/opt/bin/agent")
	case "claude":
		return mustNewClaude(t, "/opt/bin/claude")
	case "agy":
		return mustNewAgy(t, "/opt/bin/agy")
	default:
		t.Fatalf("unknown fixture adapter %q", name)
		return nil
	}
}

func mustNewClaude(t *testing.T, executable string) Adapter {
	t.Helper()
	adapter, err := NewClaude(executable)
	return mustAdapter(t, adapter, err)
}

func mustNewAgy(t *testing.T, executable string) Adapter {
	t.Helper()
	adapter, err := NewAgy(executable)
	return mustAdapter(t, adapter, err)
}

func mustAdapter(t *testing.T, adapter Adapter, err error) Adapter {
	t.Helper()
	if err != nil {
		t.Fatalf("new adapter: %v", err)
	}
	return adapter
}

func mustNewCompozy(t *testing.T, executable, workspaceID string) Adapter {
	t.Helper()
	adapter, err := NewCompozy(executable, workspaceID)
	return mustAdapter(t, adapter, err)
}

func mustNewCodex(t *testing.T, executable string) Adapter {
	t.Helper()
	adapter, err := NewCodex(executable)
	return mustAdapter(t, adapter, err)
}

func mustNewOpenCode(t *testing.T, executable string) Adapter {
	t.Helper()
	adapter, err := NewOpenCode(executable)
	return mustAdapter(t, adapter, err)
}

func mustNewCursor(t *testing.T, executable string) Adapter {
	t.Helper()
	adapter, err := NewCursor(executable)
	return mustAdapter(t, adapter, err)
}

func cloneOutputs(input map[inventory.ProbeID][]byte) map[inventory.ProbeID][]byte {
	cloned := make(map[inventory.ProbeID][]byte, len(input))
	for key, value := range input {
		cloned[key] = slices.Clone(value)
	}
	return cloned
}

func hasEvidenceState(values []inventory.Evidence, name string, state inventory.ResolutionState) bool {
	return slices.ContainsFunc(values, func(value inventory.Evidence) bool {
		return value.Name == name && value.State == state
	})
}

func hasDiagnostic(snapshot inventory.ExecutorSnapshot, code string) bool {
	return slices.ContainsFunc(snapshot.Diagnostics, func(value inventory.Diagnostic) bool {
		return value.Code == code
	})
}

func specArgs(specs []inventory.ProbeSpec) [][]string {
	args := make([][]string, len(specs))
	for i := range specs {
		args[i] = specs[i].Args
	}
	return args
}
