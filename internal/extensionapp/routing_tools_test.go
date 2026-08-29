package extensionapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/franciscpd/batuta-compozy/internal/publication"
	"github.com/franciscpd/batuta-compozy/internal/routing"
)

func TestRoutingPlanReturnsActionableDomainErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		planErr       error
		reasonCode    string
		secondaryCode string
	}{
		{name: "classification retry", planErr: routing.ErrClassificationRetryable, reasonCode: "classification_retryable"},
		{name: "fit retry", planErr: fmt.Errorf("%w: recommendation includes hard_capability_unresolved candidate", routing.ErrSelectionRetryable), reasonCode: "routing_fit_retryable", secondaryCode: "hard_capability_unresolved"},
		{name: "catalog drift", planErr: routing.ErrCatalogDrift, reasonCode: "catalog_drift"},
		{name: "reauthor tasks", planErr: routing.ErrReauthoringRequired, reasonCode: "task_reauthoring_required"},
		{name: "no eligible runtime", planErr: routing.ErrNoEligibleCandidate, reasonCode: "no_eligible_runtime"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			app := application{services: serviceSet{routingPlan: func(
				context.Context, publication.TrustedScope, RoutingPlanInput,
			) (routing.RoutingGeneration, error) {
				return routing.RoutingGeneration{}, tt.planErr
			}}}

			_, err := app.routingPlan(context.Background(), &compozysdk.ExtensionToolWorkspaceScope{
				ID: "ws_demo", Root: "/workspace",
			}, RoutingPlanInput{Slug: "demo"})
			var rpcErr *compozysdk.RPCError
			if !errors.As(err, &rpcErr) || rpcErr.Code != -32010 {
				t.Fatalf("routingPlan() error = %#v, want canonical tool execution RPC error", err)
			}
			var data map[string]any
			if json.Unmarshal(rpcErr.Data, &data) != nil || data["code"] != "tool_invalid_input" {
				t.Fatalf("routingPlan() error data = %s, want tool_invalid_input", rpcErr.Data)
			}
			reasons, ok := data["reason_codes"].([]any)
			if !ok || len(reasons) < 1 || reasons[0] != tt.reasonCode {
				t.Fatalf("routingPlan() error data = %s, want reason_code %q", rpcErr.Data, tt.reasonCode)
			}
			if tt.secondaryCode != "" && (len(reasons) != 2 || reasons[1] != tt.secondaryCode) {
				t.Fatalf("routingPlan() error data = %s, want secondary reason_code %q", rpcErr.Data, tt.secondaryCode)
			}
		})
	}
}

func TestRoutingPlanSchemaAcceptsOnlySlugClassificationAndFit(t *testing.T) {
	t.Parallel()

	extension, err := newWithServices(serviceSet{})
	if err != nil {
		t.Fatalf("newWithServices() error = %v", err)
	}
	descriptor := descriptorForHandler(t, extension, "routing_plan")
	var schema struct {
		Required             []string                   `json:"required"`
		Properties           map[string]json.RawMessage `json:"properties"`
		AdditionalProperties bool                       `json:"additionalProperties"`
	}
	if err := json.Unmarshal(descriptor.InputSchema, &schema); err != nil {
		t.Fatalf("input schema: %v", err)
	}
	if schema.AdditionalProperties || len(schema.Properties) != 3 {
		t.Fatalf("routing plan schema = %s", descriptor.InputSchema)
	}
	for _, key := range []string{"slug", "proposals", "fit"} {
		if _, ok := schema.Properties[key]; !ok {
			t.Fatalf("routing plan schema missing %q: %s", key, descriptor.InputSchema)
		}
	}
	for _, forbidden := range []string{"path", "tasks", "raw_config", "runtime_rules", "command"} {
		if _, ok := schema.Properties[forbidden]; ok {
			t.Fatalf("routing plan schema exposes forbidden %q", forbidden)
		}
	}
}

func TestRoutingPlanFitSchemaKeysCandidatesByProviderAndModel(t *testing.T) {
	t.Parallel()

	extension, err := newWithServices(serviceSet{})
	if err != nil {
		t.Fatalf("newWithServices() error = %v", err)
	}
	descriptor := descriptorForHandler(t, extension, "routing_plan")
	var schema map[string]any
	if err := json.Unmarshal(descriptor.InputSchema, &schema); err != nil {
		t.Fatalf("input schema: %v", err)
	}
	properties := schema["properties"].(map[string]any)
	fit := properties["fit"].(map[string]any)
	cell := fit["items"].(map[string]any)
	candidates := cell["properties"].(map[string]any)["candidates"].(map[string]any)
	candidate := candidates["items"].(map[string]any)
	candidateProperties := candidate["properties"].(map[string]any)
	for _, required := range []string{"provider_id", "model_id", "score"} {
		if _, exists := candidateProperties[required]; !exists {
			t.Fatalf("fit candidate schema missing %q: %s", required, descriptor.InputSchema)
		}
	}
	if _, exists := candidateProperties["enrichment_ids"]; exists {
		t.Fatalf("fit candidate schema accepts caller enrichment identity: %s", descriptor.InputSchema)
	}
	if candidate["additionalProperties"] != false {
		t.Fatalf("fit candidate schema is open: %#v", candidate)
	}
}

func TestRoutingOutputRecordsOnlyServerDerivedEnrichmentEvidence(t *testing.T) {
	t.Parallel()

	extension, err := newWithServices(serviceSet{})
	if err != nil {
		t.Fatalf("newWithServices() error = %v", err)
	}
	descriptor := descriptorForHandler(t, extension, "routing_plan")
	var schema map[string]any
	if err := json.Unmarshal(descriptor.OutputSchema, &schema); err != nil {
		t.Fatalf("output schema: %v", err)
	}
	properties := schema["properties"].(map[string]any)
	cell := properties["cells"].(map[string]any)["items"].(map[string]any)
	selected := cell["properties"].(map[string]any)["selected"].(map[string]any)
	selectedProperties := selected["properties"].(map[string]any)
	enrichment, exists := selectedProperties["enrichment_ids"].(map[string]any)
	if !exists || enrichment["type"] != "array" {
		t.Fatalf("selected enrichment schema = %#v", selectedProperties["enrichment_ids"])
	}
	items := enrichment["items"].(map[string]any)
	values := items["enum"].([]any)
	for _, want := range []any{"claude", "agy", "codex", "opencode", "cursor-agent"} {
		if !slices.Contains(values, want) {
			t.Fatalf("enrichment enum = %#v, missing %q", values, want)
		}
	}
	for _, forbidden := range []string{"raw_output", "capabilities", "diagnostics", "credentials"} {
		if containsJSONToken(string(descriptor.OutputSchema), forbidden) {
			t.Fatalf("routing output exposes adapter payload field %q: %s", forbidden, descriptor.OutputSchema)
		}
	}
}

func TestRoutingApplyAcceptsOnlyClosedOwnedOperations(t *testing.T) {
	t.Parallel()

	extension, err := newWithServices(serviceSet{})
	if err != nil {
		t.Fatalf("newWithServices() error = %v", err)
	}
	descriptor := descriptorForHandler(t, extension, "routing_apply")
	if descriptor.ReadOnly || descriptor.Risk != compozysdk.RiskMutating {
		t.Fatalf("routing apply descriptor = %#v", descriptor)
	}
	var schema map[string]any
	if err := json.Unmarshal(descriptor.InputSchema, &schema); err != nil {
		t.Fatalf("input schema: %v", err)
	}
	oneOf, ok := schema["oneOf"].([]any)
	if !ok || len(oneOf) != 4 {
		t.Fatalf("routing apply schema = %#v, want four closed variants", schema)
	}
	payload := string(descriptor.InputSchema)
	for _, operation := range []string{"apply_matrix", "start_delivery", "recover_delivery", "reconcile_fallbacks"} {
		if !containsJSONToken(payload, operation) {
			t.Fatalf("routing apply schema missing %q: %s", operation, payload)
		}
	}
	for _, forbidden := range []string{"workspace_path", "runtime_rules", "failure_evidence", "node_id", "item_index", "runtime_snapshot"} {
		if containsJSONToken(payload, forbidden) {
			t.Fatalf("routing apply schema exposes forbidden %q: %s", forbidden, payload)
		}
	}
}

func TestRoutingOutputSchemasDeclareOnlyClosedSafeEvidence(t *testing.T) {
	t.Parallel()

	extension, err := newWithServices(serviceSet{})
	if err != nil {
		t.Fatalf("newWithServices() error = %v", err)
	}
	for _, handler := range []string{"routing_plan", "routing_apply"} {
		descriptor := descriptorForHandler(t, extension, handler)
		var schema map[string]any
		if err := json.Unmarshal(descriptor.OutputSchema, &schema); err != nil {
			t.Fatalf("%s output schema: %v", handler, err)
		}
		properties, ok := schema["properties"].(map[string]any)
		if !ok || schema["additionalProperties"] != false || len(properties) < 2 {
			t.Fatalf("%s output schema = %#v, want closed typed evidence", handler, schema)
		}
		payload := string(descriptor.OutputSchema)
		for _, forbidden := range []string{"credential", "raw_config", "task_body", "failure_evidence"} {
			if containsJSONToken(payload, forbidden) {
				t.Fatalf("%s output schema exposes forbidden %q", handler, forbidden)
			}
		}
		if handler == "routing_plan" {
			budget := properties["enclosing_budget"].(map[string]any)
			required := budget["required"].([]any)
			if !slices.Contains(required, any("iteration_cap")) || !slices.Contains(required, any("wall_time_seconds")) {
				t.Fatalf("routing_plan enclosing budget required = %#v", required)
			}
		}
	}
}

func TestCompozyNormalizationProjectsAuthoritativeProviderAuth(t *testing.T) {
	t.Parallel()

	snapshot, err := inventory.NewSnapshot("catalog-digest", []inventory.ExecutorSnapshot{{
		ID: inventory.ExecutorCompozy, Availability: inventory.AvailabilityAvailable,
		Capabilities: []inventory.Evidence{
			{Name: "models", State: inventory.ResolutionResolved, Digest: "catalog-digest", Identifiers: []string{
				"claude/claude-fixture", "cursor/grok-4.6", "future/model", "gemini/gemini-fixture",
			}},
			{Name: "provider_auth", State: inventory.ResolutionResolved, Identifiers: []string{
				"claude=configured", "cursor=missing", "future=unknown", "gemini=configured",
			}},
		},
	}})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	catalog, err := liveCatalogFromInventory(snapshot)
	if err != nil {
		t.Fatalf("liveCatalogFromInventory() error = %v", err)
	}
	want := map[string]inventory.CredentialState{
		"claude/claude-fixture": inventory.CredentialConfigured,
		"cursor/grok-4.6":       inventory.CredentialMissing,
		"future/model":          inventory.CredentialUnknown,
		"gemini/gemini-fixture": inventory.CredentialConfigured,
	}
	for _, model := range catalog.Models {
		key := model.ProviderID + "/" + model.ModelID
		if model.CredentialState != want[key] {
			t.Fatalf("catalog model %q auth = %q, want %q", key, model.CredentialState, want[key])
		}
		delete(want, key)
	}
	if len(want) != 0 {
		t.Fatalf("catalog missing models: %#v", want)
	}
}

func TestRoutingHandlersRequireTrustedWorkspaceAndPreserveTypedInputs(t *testing.T) {
	t.Parallel()

	wantGeneration := routing.RoutingGeneration{Digest: "sha256:generation", TaskSetDigest: "sha256:tasks"}
	var planSeen RoutingPlanInput
	var applySeen RoutingApplyInput
	app := application{services: serviceSet{
		routingPlan: func(_ context.Context, _ publication.TrustedScope, input RoutingPlanInput) (routing.RoutingGeneration, error) {
			planSeen = input
			return wantGeneration, nil
		},
		routingApply: func(_ context.Context, _ publication.TrustedScope, input RoutingApplyInput) (RoutingApplyOutput, error) {
			applySeen = input
			return RoutingApplyOutput{Operation: input.Operation}, nil
		},
	}}
	planInput := RoutingPlanInput{Slug: "demo"}
	if _, err := app.routingPlan(context.Background(), nil, planInput); err == nil {
		t.Fatal("routingPlan(nil workspace) error = nil")
	}
	workspace := &compozysdk.ExtensionToolWorkspaceScope{ID: "ws_demo", Root: "/workspace"}
	if _, err := app.routingPlan(context.Background(), workspace, planInput); err != nil {
		t.Fatalf("routingPlan() error = %v", err)
	}
	applyInput := RoutingApplyInput{Operation: RoutingOperationRecoverDelivery, DeliveryID: digestValue("delivery-handler"), DeliveryRunID: "run_demo"}
	if _, err := app.routingApply(context.Background(), workspace, applyInput); err != nil {
		t.Fatalf("routingApply() error = %v", err)
	}
	if planSeen.Slug != "demo" || applySeen.DeliveryID != applyInput.DeliveryID || applySeen.DeliveryRunID != "run_demo" {
		t.Fatalf("inputs = plan:%#v apply:%#v", planSeen, applySeen)
	}
}

func TestRoutingApplyReplansAndRejectsChangedGeneration(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRoutingTask(t, root)
	first := routingInventory(t, nil)
	second := routingInventory(t, []inventory.Diagnostic{{Code: "catalog_refreshed"}})
	collectCalls := 0
	matrixCalls := 0
	engine := routingEngine{
		inventory: func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error) {
			collectCalls++
			if collectCalls == 1 {
				return first, nil
			}
			return second, nil
		},
		applyMatrix: func(context.Context, routing.MatrixApplyInput) (routing.MatrixApplyResult, error) {
			matrixCalls++
			return routing.MatrixApplyResult{}, nil
		},
	}
	scope := publication.TrustedScope{WorkspaceID: "ws_demo", WorkspaceRoot: root}
	input := routingPlanFixture()
	planned, err := engine.Plan(context.Background(), scope, input)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	if planned.EnclosingBudget.IterationCap != 4 || planned.EnclosingBudget.WallTimeSeconds != 14400 {
		t.Fatalf("Plan() enclosing budget = %#v, want cap 4 and wall 14400", planned.EnclosingBudget)
	}
	_, err = engine.Apply(context.Background(), scope, RoutingApplyInput{
		Operation: RoutingOperationApplyMatrix, RoutingPlan: &input, ExpectedGenerationDigest: planned.Digest,
		WorktreeRef: "wt_demo", OriginSessionID: "session_demo",
	})
	if err == nil {
		t.Fatal("Apply(changed inventory) error = nil")
	}
	if matrixCalls != 0 {
		t.Fatalf("matrix calls = %d, want 0 before matching fresh digest", matrixCalls)
	}
}

func TestRoutingApplyArchivesTrustedWorktreeAndTaskEvidence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRoutingTask(t, root)
	snapshot := routingInventory(t, nil)
	var captured routing.MatrixApplyInput
	engine := routingEngine{
		inventory: func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error) {
			return snapshot, nil
		},
		inspectWorktree: func(_ context.Context, scope publication.TrustedScope, ref string) (publication.WorktreeInspection, error) {
			return publication.WorktreeInspection{Worktree: publication.Worktree{
				ID: ref, WorkspaceID: scope.WorkspaceID, Path: root,
			}}, nil
		},
		worktreeState: func(context.Context, string) (publication.WorktreeState, error) {
			return publication.WorktreeState{
				HeadSHA:         "0123456789abcdef0123456789abcdef01234567",
				PorcelainSHA256: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
				ContentSHA256:   "sha256:89abcdef0123456789abcdef0123456789abcdef0123456789abcdef01234567",
			}, nil
		},
		applyMatrix: func(_ context.Context, input routing.MatrixApplyInput) (routing.MatrixApplyResult, error) {
			captured = input
			return routing.MatrixApplyResult{DeliveryID: "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"}, nil
		},
	}
	scope := publication.TrustedScope{WorkspaceID: "ws_demo", WorkspaceRoot: root}
	planInput := routingPlanFixture()
	planned, err := engine.Plan(context.Background(), scope, planInput)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = engine.Apply(context.Background(), scope, RoutingApplyInput{
		Operation: RoutingOperationApplyMatrix, RoutingPlan: &planInput,
		ExpectedGenerationDigest: planned.Digest, WorktreeRef: "wt_demo", OriginSessionID: "session_demo",
	})
	if err != nil {
		t.Fatalf("Apply() error = %v", err)
	}
	if captured.WorkspaceID != scope.WorkspaceID || captured.WorkspaceRoot != root || captured.WorktreeID != "wt_demo" || captured.WorktreeRoot != root ||
		captured.OriginSessionID != "session_demo" || captured.Generation.Digest != planned.Digest || captured.TaskSetDigest != planned.TaskSetDigest ||
		len(captured.TaskSnapshot.Tasks) != 1 || captured.TaskSnapshot.Tasks[0].ID != "task_01" {
		t.Fatalf("captured matrix input = %#v", captured)
	}
}

func routingPlanFixture() RoutingPlanInput {
	return RoutingPlanInput{
		Slug: "demo",
		Proposals: []routing.ClassificationProposal{{
			TaskID: "task_01", Domain: routing.DomainFrontend, Complexity: routing.ComplexityLow,
			Confidence: 0.95, Requirements: []routing.CapabilityRequirement{}, Evidence: []routing.EvidenceReference{}, Dependencies: []string{},
		}},
		Fit: []RoutingFitProposal{{
			TaskIDs: []string{"task_01"},
			Domain:  routing.DomainFrontend, Complexity: routing.ComplexityLow,
			Candidates: []routing.FitCandidate{{ExecutorID: inventory.ExecutorCursorAgent, ProviderID: "cursor", ModelID: "grok-4.6", Score: 0.95}},
		}},
	}
}

func routingInventory(t *testing.T, diagnostics []inventory.Diagnostic) inventory.InventorySnapshot {
	t.Helper()
	snapshot, err := inventory.NewSnapshot("catalog-digest", []inventory.ExecutorSnapshot{
		{
			ID: inventory.ExecutorCompozy, Availability: inventory.AvailabilityAvailable,
			Health:          inventory.Evidence{Name: "health", State: inventory.ResolutionResolved},
			Capabilities:    []inventory.Evidence{{Name: "models", State: inventory.ResolutionResolved, Digest: "catalog-digest", Identifiers: []string{"cursor/grok-4.6", "codex/gpt-5.6-luna"}}},
			CredentialState: inventory.CredentialUnknown,
		},
		{
			ID: inventory.ExecutorCursorAgent, Availability: inventory.AvailabilityAvailable,
			Health:          inventory.Evidence{Name: "health", State: inventory.ResolutionResolved},
			Capabilities:    []inventory.Evidence{{Name: "models", State: inventory.ResolutionResolved, Identifiers: []string{"grok-4.6"}}},
			CredentialState: inventory.CredentialConfigured, Diagnostics: diagnostics,
		},
		{
			ID: inventory.ExecutorCodex, Availability: inventory.AvailabilityAvailable,
			Health:          inventory.Evidence{Name: "health", State: inventory.ResolutionResolved},
			Capabilities:    []inventory.Evidence{{Name: "models", State: inventory.ResolutionResolved, Identifiers: []string{"gpt-5.6-luna"}}},
			CredentialState: inventory.CredentialConfigured,
		},
	})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	return snapshot
}

func writeRoutingTask(t *testing.T, root string) {
	t.Helper()
	directory := filepath.Join(root, ".compozy", "tasks", "demo")
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("mkdir: %v", err)
	}
	body := "---\nstatus: pending\ntitle: Frontend demo\ntype: frontend\ncomplexity: low\n---\n\n# Demo\n"
	if err := os.WriteFile(filepath.Join(directory, "task_01.md"), []byte(body), 0o600); err != nil {
		t.Fatalf("write task: %v", err)
	}
	writeRoutingManifest(t, directory, []string{"task_01"}, nil)
}

func writeRoutingManifest(t *testing.T, directory string, nodes []string, edges [][2]string) {
	t.Helper()
	content := "---\nschema_version: \"compozy.tasks/v2\"\nworkflow: demo\ngraph:\n  nodes:\n"
	for _, node := range nodes {
		content += "    - id: " + node + "\n      file: " + node + ".md\n"
	}
	if len(edges) == 0 {
		content += "  edges: []\n"
	} else {
		content += "  edges:\n"
		for _, edge := range edges {
			content += "    - from: " + edge[0] + "\n      to: " + edge[1] + "\n"
		}
	}
	content += "---\n\n# Tasks\n"
	if err := os.WriteFile(filepath.Join(directory, "_tasks.md"), []byte(content), 0o600); err != nil {
		t.Fatalf("write task manifest: %v", err)
	}
}

func containsJSONToken(payload, token string) bool {
	return len(token) > 0 && json.Valid([]byte(payload)) && contains(payload, `"`+token+`"`)
}

func contains(value, target string) bool {
	for index := 0; index+len(target) <= len(value); index++ {
		if value[index:index+len(target)] == target {
			return true
		}
	}
	return false
}
