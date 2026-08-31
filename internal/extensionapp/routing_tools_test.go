package extensionapp

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"slices"
	"testing"

	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/franciscpd/batuta-compozy/internal/publication"
	"github.com/franciscpd/batuta-compozy/internal/repository"
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

func TestRoutingPlanReturnsRejectedCandidateDetails(t *testing.T) {
	t.Parallel()

	rejection := routing.CandidateRejection{
		Domain: routing.DomainFrontend, Complexity: routing.ComplexityMedium,
		ExecutorID: inventory.ExecutorCompozy, ProviderID: "cursor",
		ModelID: "claude-sonnet-5[thinking=true,context=300k,effort=high]",
		Code:    "model_below_floor",
	}
	app := application{services: serviceSet{routingPlan: func(
		context.Context, publication.TrustedScope, RoutingPlanInput,
	) (routing.RoutingGeneration, error) {
		return routing.RoutingGeneration{}, &routing.RecommendedCandidateError{Rejection: rejection}
	}}}

	_, err := app.routingPlan(context.Background(), &compozysdk.ExtensionToolWorkspaceScope{
		ID: "ws_demo", Root: "/workspace",
	}, RoutingPlanInput{Slug: "demo"})
	var rpcErr *compozysdk.RPCError
	if !errors.As(err, &rpcErr) {
		t.Fatalf("routingPlan() error = %#v, want RPCError", err)
	}
	var data struct {
		CandidateRejections []routing.CandidateRejection `json:"candidate_rejections"`
	}
	if json.Unmarshal(rpcErr.Data, &data) != nil || len(data.CandidateRejections) != 1 ||
		!reflect.DeepEqual(data.CandidateRejections[0], rejection) {
		t.Fatalf("routingPlan() error data = %s, want rejection %#v", rpcErr.Data, rejection)
	}
}

func TestRoutingApplyReturnsActionableReplanErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name          string
		applyErr      error
		reasonCode    string
		secondaryCode string
	}{
		{
			name: "model below floor",
			applyErr: fmt.Errorf(
				"%w: candidate rejected: model_below_floor",
				routing.ErrSelectionRetryable,
			),
			reasonCode:    "routing_fit_retryable",
			secondaryCode: "model_below_floor",
		},
		{
			name:       "catalog drift",
			applyErr:   routing.ErrCatalogDrift,
			reasonCode: "catalog_drift",
		},
		{
			name:       "generation unknown",
			applyErr:   routing.ErrGenerationUnknown,
			reasonCode: "generation_unknown",
		},
		{
			name:       "generation superseded",
			applyErr:   routing.ErrGenerationSuperseded,
			reasonCode: "generation_superseded",
		},
		{
			name:       "generation capacity",
			applyErr:   routing.ErrGenerationCapacity,
			reasonCode: "generation_capacity_exhausted",
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			app := application{services: serviceSet{routingApply: func(
				context.Context,
				publication.TrustedScope,
				RoutingApplyInput,
			) (RoutingApplyOutput, error) {
				return RoutingApplyOutput{}, tt.applyErr
			}}}

			_, err := app.routingApply(
				context.Background(),
				&compozysdk.ExtensionToolWorkspaceScope{ID: "ws_demo", Root: "/workspace"},
				RoutingApplyInput{
					Operation:                RoutingOperationAlignmentStatus,
					RoutingPlan:              &RoutingPlanInput{Slug: "demo"},
					ExpectedGenerationDigest: digestValue("generation"),
				},
			)
			var rpcErr *compozysdk.RPCError
			if !errors.As(err, &rpcErr) || rpcErr.Code != -32010 {
				t.Fatalf("routingApply() error = %#v, want canonical tool execution RPC error", err)
			}
			var data struct {
				Code        string   `json:"code"`
				ReasonCodes []string `json:"reason_codes"`
			}
			if json.Unmarshal(rpcErr.Data, &data) != nil || data.Code != "tool_invalid_input" ||
				len(data.ReasonCodes) == 0 || data.ReasonCodes[0] != tt.reasonCode {
				t.Fatalf("routingApply() error data = %s, want reason_code %q", rpcErr.Data, tt.reasonCode)
			}
			if tt.secondaryCode != "" &&
				(len(data.ReasonCodes) != 2 || data.ReasonCodes[1] != tt.secondaryCode) {
				t.Fatalf(
					"routingApply() error data = %s, want secondary reason_code %q",
					rpcErr.Data,
					tt.secondaryCode,
				)
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
	reasoning, exists := candidateProperties["reasoning"].(map[string]any)
	if !exists || !reflect.DeepEqual(reasoning["enum"], []any{"low", "medium", "high", "xhigh"}) {
		t.Fatalf("fit candidate reasoning schema = %#v", candidateProperties["reasoning"])
	}
	if slices.Contains(candidate["required"].([]any), any("reasoning")) {
		t.Fatalf("fit candidate reasoning is unexpectedly required: %#v", candidate)
	}
	if _, exists := candidateProperties["enrichment_ids"]; exists {
		t.Fatalf("fit candidate schema accepts caller enrichment identity: %s", descriptor.InputSchema)
	}
	if candidate["additionalProperties"] != false {
		t.Fatalf("fit candidate schema is open: %#v", candidate)
	}
}

func TestRoutingOutputAllowsUnclassifiedLiveModelTier(t *testing.T) {
	t.Parallel()

	schema := runtimeCandidateOutputSchema()
	modelTier := schema["properties"].(map[string]any)["model_tier"].(map[string]any)
	if modelTier["minimum"] != 0 {
		t.Fatalf("model tier minimum = %#v, want 0 for unclassified live models", modelTier["minimum"])
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
	if !ok || len(oneOf) != 7 {
		t.Fatalf("routing apply schema = %#v, want seven closed variants", schema)
	}
	payload := string(descriptor.InputSchema)
	for _, operation := range []string{
		"alignment_status", "confirm_alignment", "bootstrap_repository", "apply_matrix", "start_delivery", "recover_delivery", "reconcile_fallbacks",
	} {
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

func TestRoutingApplyBootstrapsOnlyConfirmedGenerationAtTrustedWorkspaceRoot(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRoutingTask(t, root)
	snapshot := routingInventory(t, nil)
	bootstrapCalls := 0
	var planned routing.RoutingGeneration
	engine := routingEngine{
		inventory: func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error) {
			return snapshot, nil
		},
		loadGeneration: func(_ string, digest string) (routing.RoutingGeneration, error) {
			if digest != planned.Digest {
				return routing.RoutingGeneration{}, routing.ErrGenerationUnknown
			}
			return planned, nil
		},
		alignmentStatus: func(_ string, generation routing.RoutingGeneration) (routing.AlignmentStatus, error) {
			return routing.AlignmentStatus{
				State: routing.AlignmentConfirmed, AlignmentDigest: digestValue("alignment"), GenerationDigest: generation.Digest,
			}, nil
		},
		bootstrapRepository: func(_ context.Context, workspaceRoot string) (repository.BootstrapResult, error) {
			bootstrapCalls++
			if workspaceRoot != root {
				t.Fatalf("bootstrap root = %q, want trusted %q", workspaceRoot, root)
			}
			return repository.BootstrapResult{
				State: repository.BootstrapInitialized, Branch: "main",
				HeadSHA: "0123456789abcdef0123456789abcdef01234567", CommitMessage: "chore: initialize workspace", CommittedFiles: 3,
			}, nil
		},
	}
	scope := publication.TrustedScope{WorkspaceID: "ws_demo", WorkspaceRoot: root}
	planInput := routingPlanFixture()
	var err error
	planned, err = engine.Plan(context.Background(), scope, planInput)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	output, err := engine.Apply(context.Background(), scope, RoutingApplyInput{
		Operation: RoutingOperationBootstrapRepository, RoutingPlan: &planInput, ExpectedGenerationDigest: planned.Digest,
	})
	if err != nil || bootstrapCalls != 1 || output.Repository == nil || output.Repository.State != repository.BootstrapInitialized {
		t.Fatalf("Apply(bootstrap) = %#v, error %v, calls %d", output, err, bootstrapCalls)
	}

	engine.alignmentStatus = func(_ string, generation routing.RoutingGeneration) (routing.AlignmentStatus, error) {
		return routing.AlignmentStatus{
			State: routing.AlignmentRequired, AlignmentDigest: digestValue("alignment"), GenerationDigest: generation.Digest,
		}, nil
	}
	_, err = engine.Apply(context.Background(), scope, RoutingApplyInput{
		Operation: RoutingOperationBootstrapRepository, RoutingPlan: &planInput, ExpectedGenerationDigest: planned.Digest,
	})
	if !errors.Is(err, routing.ErrRoutingAlignmentRequired) || bootstrapCalls != 1 {
		t.Fatalf("Apply(unconfirmed bootstrap) error = %v, calls %d", err, bootstrapCalls)
	}
}

func TestRoutingApplyUnknownGenerationNeverBootstrapsRepository(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRoutingTask(t, root)
	bootstrapCalls := 0
	engine := routingEngine{
		loadGeneration: func(string, string) (routing.RoutingGeneration, error) {
			return routing.RoutingGeneration{}, routing.ErrGenerationUnknown
		},
		alignmentStatus: func(string, routing.RoutingGeneration) (routing.AlignmentStatus, error) {
			t.Fatal("alignment status must not run for an unknown generation")
			return routing.AlignmentStatus{}, nil
		},
		bootstrapRepository: func(context.Context, string) (repository.BootstrapResult, error) {
			bootstrapCalls++
			return repository.BootstrapResult{}, nil
		},
	}
	input := routingPlanFixture()

	_, err := engine.Apply(context.Background(), publication.TrustedScope{
		WorkspaceID: "ws_demo", WorkspaceRoot: root,
	}, RoutingApplyInput{
		Operation:                RoutingOperationBootstrapRepository,
		RoutingPlan:              &input,
		ExpectedGenerationDigest: digestValue("invented"),
	})
	if !errors.Is(err, routing.ErrGenerationUnknown) {
		t.Fatalf("Apply() error = %v, want ErrGenerationUnknown", err)
	}
	if bootstrapCalls != 0 {
		t.Fatalf("bootstrap calls = %d, want 0", bootstrapCalls)
	}
}

func TestRoutingApplyRejectsArchivedGenerationAfterWorkspaceRootRebinding(t *testing.T) {
	t.Parallel()

	originalRoot := t.TempDir()
	reboundRoot := t.TempDir()
	writeRoutingTask(t, originalRoot)
	snapshot := routingInventory(t, nil)
	var planned routing.RoutingGeneration
	mutations := 0
	inventoryCalls := 0
	engine := routingEngine{
		inventory: func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error) {
			inventoryCalls++
			return snapshot, nil
		},
		loadGeneration: func(string, string) (routing.RoutingGeneration, error) {
			return planned, nil
		},
		alignmentConfirm: func(string, string, routing.RoutingGeneration) (routing.AlignmentStatus, error) {
			mutations++
			return routing.AlignmentStatus{}, nil
		},
		alignmentStatus: func(string, routing.RoutingGeneration) (routing.AlignmentStatus, error) {
			mutations++
			return routing.AlignmentStatus{State: routing.AlignmentConfirmed}, nil
		},
		bootstrapRepository: func(context.Context, string) (repository.BootstrapResult, error) {
			mutations++
			return repository.BootstrapResult{}, nil
		},
		inspectWorktree: func(context.Context, publication.TrustedScope, string) (publication.WorktreeInspection, error) {
			mutations++
			return publication.WorktreeInspection{}, nil
		},
		worktreeState: func(context.Context, string) (publication.WorktreeState, error) {
			mutations++
			return publication.WorktreeState{}, nil
		},
		applyMatrix: func(context.Context, routing.MatrixApplyInput) (routing.MatrixApplyResult, error) {
			mutations++
			return routing.MatrixApplyResult{}, nil
		},
	}
	input := routingPlanFixture()
	originalScope := publication.TrustedScope{WorkspaceID: "ws_demo", WorkspaceRoot: originalRoot}
	var err error
	planned, err = engine.Plan(context.Background(), originalScope, input)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	reboundScope := publication.TrustedScope{WorkspaceID: originalScope.WorkspaceID, WorkspaceRoot: reboundRoot}
	for _, request := range []RoutingApplyInput{
		{
			Operation: RoutingOperationConfirmAlignment, RoutingPlan: &input,
			ExpectedGenerationDigest: planned.Digest, OriginSessionID: "session_operator",
		},
		{
			Operation: RoutingOperationBootstrapRepository, RoutingPlan: &input,
			ExpectedGenerationDigest: planned.Digest,
		},
		{
			Operation: RoutingOperationApplyMatrix, RoutingPlan: &input,
			ExpectedGenerationDigest: planned.Digest, WorktreeRef: "wt_demo", OriginSessionID: "session_operator",
		},
	} {
		if _, err := engine.Apply(context.Background(), reboundScope, request); !errors.Is(err, routing.ErrGenerationSuperseded) {
			t.Fatalf("Apply(%s) error = %v, want ErrGenerationSuperseded", request.Operation, err)
		}
	}
	if mutations != 0 {
		t.Fatalf("rebound generation reached %d mutation callbacks, want 0", mutations)
	}
	if inventoryCalls != 1 {
		t.Fatalf("rebound generation inventory calls = %d, want only the original plan", inventoryCalls)
	}
}

func TestRoutingApplyArchivesAtStatusAndConfirmsWithoutReplanning(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRoutingTask(t, root)
	snapshot := routingInventory(t, nil)
	var statusCalls, confirmCalls, inventoryCalls int
	var archived routing.RoutingGeneration
	engine := routingEngine{
		inventory: func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error) {
			inventoryCalls++
			return snapshot, nil
		},
		archiveGeneration: func(_ string, generation routing.RoutingGeneration) error {
			archived = generation
			return nil
		},
		loadGeneration: func(_ string, digest string) (routing.RoutingGeneration, error) {
			if archived.Digest == "" || digest != archived.Digest {
				return routing.RoutingGeneration{}, routing.ErrGenerationUnknown
			}
			return archived, nil
		},
		alignmentStatus: func(_ string, generation routing.RoutingGeneration) (routing.AlignmentStatus, error) {
			statusCalls++
			return routing.AlignmentStatus{
				State: routing.AlignmentRequired, AlignmentDigest: digestValue("alignment"), GenerationDigest: generation.Digest,
			}, nil
		},
		alignmentConfirm: func(_ string, actor string, generation routing.RoutingGeneration) (routing.AlignmentStatus, error) {
			confirmCalls++
			return routing.AlignmentStatus{
				State: routing.AlignmentConfirmed, AlignmentDigest: digestValue("alignment"), GenerationDigest: generation.Digest,
				ConfirmedBy: actor,
			}, nil
		},
	}
	scope := publication.TrustedScope{WorkspaceID: "ws_demo", WorkspaceRoot: root}
	planInput := routingPlanFixture()
	planned, err := engine.Plan(context.Background(), scope, planInput)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	status, err := engine.Apply(context.Background(), scope, RoutingApplyInput{
		Operation: RoutingOperationAlignmentStatus, RoutingPlan: &planInput, ExpectedGenerationDigest: planned.Digest,
	})
	if err != nil || status.Alignment == nil || status.Alignment.State != routing.AlignmentRequired || statusCalls != 1 || confirmCalls != 0 {
		t.Fatalf("Apply(alignment status) = %#v, error %v, calls status=%d confirm=%d", status, err, statusCalls, confirmCalls)
	}
	if archived.Digest != planned.Digest || inventoryCalls != 2 {
		t.Fatalf("archived digest = %q, inventory calls = %d; want %q and 2", archived.Digest, inventoryCalls, planned.Digest)
	}
	engine.inventory = func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error) {
		t.Fatal("confirm_alignment must not recollect inventory")
		return inventory.InventorySnapshot{}, nil
	}
	confirmed, err := engine.Apply(context.Background(), scope, RoutingApplyInput{
		Operation: RoutingOperationConfirmAlignment, RoutingPlan: &planInput,
		ExpectedGenerationDigest: planned.Digest, OriginSessionID: "session_operator",
	})
	if err != nil || confirmed.Alignment == nil || confirmed.Alignment.State != routing.AlignmentConfirmed ||
		confirmed.Alignment.ConfirmedBy != "session_operator" || statusCalls != 1 || confirmCalls != 1 || inventoryCalls != 2 {
		t.Fatalf("Apply(confirm alignment) = %#v, error %v, calls status=%d confirm=%d", confirmed, err, statusCalls, confirmCalls)
	}
}

func TestRoutingApplyDoesNotArchiveFailedAlignmentStatus(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRoutingTask(t, root)
	snapshot := routingInventory(t, nil)
	archiveCalls := 0
	statusErr := errors.New("alignment status unavailable")
	engine := routingEngine{
		inventory: func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error) {
			return snapshot, nil
		},
		archiveGeneration: func(string, routing.RoutingGeneration) error {
			archiveCalls++
			return nil
		},
		alignmentStatus: func(string, routing.RoutingGeneration) (routing.AlignmentStatus, error) {
			return routing.AlignmentStatus{}, statusErr
		},
	}
	scope := publication.TrustedScope{WorkspaceID: "ws_demo", WorkspaceRoot: root}
	input := routingPlanFixture()
	planned, err := engine.Plan(context.Background(), scope, input)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	_, err = engine.Apply(context.Background(), scope, RoutingApplyInput{
		Operation: RoutingOperationAlignmentStatus, RoutingPlan: &input, ExpectedGenerationDigest: planned.Digest,
	})
	if !errors.Is(err, statusErr) || archiveCalls != 0 {
		t.Fatalf("Apply() error = %v, archive calls = %d; want status error and zero archive", err, archiveCalls)
	}
}

func TestRoutingApplyKeepsTwoInterleavedAlignmentCandidatesConfirmable(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRoutingTask(t, root)
	store, err := routing.NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	manager := routing.AlignmentManager{Store: store}
	engine := routingEngine{
		inventory: func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error) {
			return routingInventory(t, nil), nil
		},
		archiveGeneration: store.ArchiveGeneration,
		loadGeneration:    store.LoadGeneration,
		alignmentStatus:   manager.Status,
		alignmentConfirm:  manager.Confirm,
	}
	scope := publication.TrustedScope{WorkspaceID: "ws_demo", WorkspaceRoot: root}
	firstInput := routingPlanFixture()
	secondInput := routingPlanFixture()
	secondInput.Fit[0].Candidates = []routing.FitCandidate{{
		ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "gpt-5.6-luna", Score: 0.95,
	}}
	first, err := engine.Plan(context.Background(), scope, firstInput)
	if err != nil {
		t.Fatalf("Plan(first) error = %v", err)
	}
	second, err := engine.Plan(context.Background(), scope, secondInput)
	if err != nil {
		t.Fatalf("Plan(second) error = %v", err)
	}
	for _, candidate := range []struct {
		input      RoutingPlanInput
		generation routing.RoutingGeneration
	}{
		{input: firstInput, generation: first},
		{input: secondInput, generation: second},
	} {
		if _, err := engine.Apply(context.Background(), scope, RoutingApplyInput{
			Operation: RoutingOperationAlignmentStatus, RoutingPlan: &candidate.input,
			ExpectedGenerationDigest: candidate.generation.Digest,
		}); err != nil {
			t.Fatalf("Apply(alignment_status %q) error = %v", candidate.generation.Digest, err)
		}
	}
	for index, candidate := range []struct {
		input      RoutingPlanInput
		generation routing.RoutingGeneration
	}{
		{input: firstInput, generation: first},
		{input: secondInput, generation: second},
	} {
		output, err := engine.Apply(context.Background(), scope, RoutingApplyInput{
			Operation: RoutingOperationConfirmAlignment, RoutingPlan: &candidate.input,
			ExpectedGenerationDigest: candidate.generation.Digest, OriginSessionID: fmt.Sprintf("session-%d", index+1),
		})
		if err != nil || output.Alignment == nil || output.Alignment.State != routing.AlignmentConfirmed {
			t.Fatalf("Apply(confirm %q) = %#v, error %v", candidate.generation.Digest, output, err)
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

func TestCompozyNormalizationPreservesUnknownCatalogPairsForExactAdapterProof(t *testing.T) {
	t.Parallel()

	snapshot, err := inventory.NewSnapshot("catalog-digest", []inventory.ExecutorSnapshot{{
		ID: inventory.ExecutorCompozy, Availability: inventory.AvailabilityAvailable,
		Capabilities: []inventory.Evidence{
			{Name: "models", State: inventory.ResolutionResolved, Digest: "catalog-digest", Identifiers: []string{"cursor/grok-4.6"}},
			{Name: "catalog_models_unknown", State: inventory.ResolutionResolved, Digest: "catalog-digest", Identifiers: []string{"codex/gpt-5.6-terra"}},
		},
	}})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	catalog, err := liveCatalogFromInventory(snapshot)
	if err != nil {
		t.Fatalf("liveCatalogFromInventory() error = %v", err)
	}
	want := map[string]inventory.AvailabilityState{
		"cursor/grok-4.6":     inventory.AvailabilityAvailable,
		"codex/gpt-5.6-terra": inventory.AvailabilityUnknown,
	}
	for _, model := range catalog.Models {
		key := model.ProviderID + "/" + model.ModelID
		if model.Availability != want[key] {
			t.Fatalf("catalog model %q availability = %q, want %q", key, model.Availability, want[key])
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
	var planned routing.RoutingGeneration
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
		loadGeneration: func(_ string, digest string) (routing.RoutingGeneration, error) {
			if digest != planned.Digest {
				return routing.RoutingGeneration{}, routing.ErrGenerationUnknown
			}
			return planned, nil
		},
	}
	scope := publication.TrustedScope{WorkspaceID: "ws_demo", WorkspaceRoot: root}
	input := routingPlanFixture()
	var err error
	planned, err = engine.Plan(context.Background(), scope, input)
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
	if !errors.Is(err, routing.ErrGenerationSuperseded) {
		t.Fatalf("Apply(changed inventory) error = %v, want ErrGenerationSuperseded", err)
	}
	if collectCalls != 2 || matrixCalls != 0 {
		t.Fatalf("calls inventory=%d matrix=%d, want 2 and 0 before matching fresh digest", collectCalls, matrixCalls)
	}
}

func TestRoutingApplyArchivesTrustedWorktreeAndTaskEvidence(t *testing.T) {
	t.Parallel()

	root := t.TempDir()
	writeRoutingTask(t, root)
	snapshot := routingInventory(t, nil)
	var captured routing.MatrixApplyInput
	var planned routing.RoutingGeneration
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
		loadGeneration: func(_ string, digest string) (routing.RoutingGeneration, error) {
			if digest != planned.Digest {
				return routing.RoutingGeneration{}, routing.ErrGenerationUnknown
			}
			return planned, nil
		},
	}
	scope := publication.TrustedScope{WorkspaceID: "ws_demo", WorkspaceRoot: root}
	planInput := routingPlanFixture()
	var err error
	planned, err = engine.Plan(context.Background(), scope, planInput)
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
