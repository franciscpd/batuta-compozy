package extensionapp

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"

	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/franciscpd/batuta-compozy/internal/inventory/adapters"
	"github.com/franciscpd/batuta-compozy/internal/publication"
	"github.com/franciscpd/batuta-compozy/internal/routing"
)

type planFunc func(context.Context, publication.TrustedScope, publication.PlanInput) (publication.PlanOutput, error)
type publishFunc func(context.Context, publication.TrustedScope, publication.PublishInput) (publication.PublishOutput, error)
type verifyFunc func(context.Context, publication.TrustedScope, publication.VerifyInput) (publication.VerifyOutput, error)
type inventoryFunc func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error)
type routingPlanFunc func(context.Context, publication.TrustedScope, RoutingPlanInput) (routing.RoutingGeneration, error)
type routingApplyFunc func(context.Context, publication.TrustedScope, RoutingApplyInput) (RoutingApplyOutput, error)
type routingContextFunc func(context.Context, publication.TrustedScope, RoutingContextInput) (RoutingContextOutput, error)
type deliveryBudgetContextFunc func(context.Context, publication.TrustedScope, DeliveryBudgetContextInput) (DeliveryBudgetContextOutput, error)

type serviceSet struct {
	plan                  planFunc
	publish               publishFunc
	verify                verifyFunc
	inventory             inventoryFunc
	routingPlan           routingPlanFunc
	routingApply          routingApplyFunc
	routingContext        routingContextFunc
	deliveryBudgetContext deliveryBudgetContextFunc
}

type application struct {
	services serviceSet
}

func New(compozyExecutable, gitExecutable string) (*compozysdk.Extension, error) {
	if err := requireAbsoluteExecutable("Compozy", compozyExecutable); err != nil {
		return nil, err
	}
	if err := requireAbsoluteExecutable("Git", gitExecutable); err != nil {
		return nil, err
	}
	runner := publication.ExecRunner{}
	client := publication.CLIClient{Executable: compozyExecutable, Runner: runner}
	git := publication.GitClient{Executable: gitExecutable, Runner: runner}
	planner := publication.PublicationPlanner{Compozy: client, Git: git}
	publisher := publication.Publisher{Planner: planner, Compozy: client, Git: git}
	verifier := publication.Verifier{Planner: planner, Git: git}
	collectorPaths := inventoryExecutables{
		Compozy:  compozyExecutable,
		Codex:    optionalExecutable("codex"),
		OpenCode: optionalExecutable("opencode"),
		Cursor:   optionalExecutable("agent"),
	}
	inventoryService := func(ctx context.Context, scope publication.TrustedScope) (inventory.InventorySnapshot, error) {
		collector, err := adapters.NewCollector(runner, adapters.CollectorOptions{
			TrustedWorkspace: scope.WorkspaceRoot, WorkspaceID: scope.WorkspaceID,
			CompozyExecutable: collectorPaths.Compozy, CodexExecutable: collectorPaths.Codex,
			OpenCodeExecutable: collectorPaths.OpenCode, CursorExecutable: collectorPaths.Cursor,
			ProbeParallelism: 16,
		})
		if err != nil {
			return inventory.InventorySnapshot{}, err
		}
		return collector.Collect(ctx)
	}
	var storeOnce sync.Once
	var store *routing.OwnershipStore
	var storeErr error
	storeForCall := func() (*routing.OwnershipStore, error) {
		storeOnce.Do(func() { store, storeErr = routing.NewOwnershipStore("") })
		return store, storeErr
	}
	engine := &routingEngine{
		inventory:       inventoryService,
		inspectWorktree: client.Inspect,
		worktreeState:   git.WorktreeState,
		applyMatrix: func(ctx context.Context, input routing.MatrixApplyInput) (routing.MatrixApplyResult, error) {
			store, err := storeForCall()
			if err != nil {
				return routing.MatrixApplyResult{}, err
			}
			return (routing.MatrixManager{Store: store}).Apply(ctx, input)
		},
	}
	deliveryClient := deliveryLoopCLIClient{Executable: compozyExecutable, Runner: runner}
	fallbackService := func() (deliveryFallbackService, error) {
		store, err := storeForCall()
		return deliveryFallbackService{
			Store: store, Client: deliveryClient, WorktreeState: git.WorktreeState,
		}, err
	}
	contextService := &deliveryContextService{StoreForCall: storeForCall, Client: deliveryClient}
	engine.startDelivery = func(ctx context.Context, scope publication.TrustedScope, deliveryID string) (RoutingStartResult, error) {
		fallbacks, err := fallbackService()
		if err != nil {
			return RoutingStartResult{}, err
		}
		return fallbacks.Start(ctx, scope, deliveryID)
	}
	engine.recover = func(ctx context.Context, scope publication.TrustedScope, deliveryID, runID string) (RoutingStartResult, error) {
		fallbacks, err := fallbackService()
		if err != nil {
			return RoutingStartResult{}, err
		}
		return fallbacks.Recover(ctx, scope, deliveryID, runID)
	}
	engine.reconcile = func(ctx context.Context, scope publication.TrustedScope, deliveryID, runID string) (RoutingReconcileResult, error) {
		fallbacks, err := fallbackService()
		if err != nil {
			return RoutingReconcileResult{}, err
		}
		return fallbacks.Reconcile(ctx, scope, deliveryID, runID)
	}
	return newWithServices(serviceSet{
		plan: planner.Plan, publish: publisher.Publish, verify: verifier.Verify,
		inventory: inventoryService, routingPlan: engine.Plan, routingApply: engine.Apply,
		routingContext: contextService.Routing, deliveryBudgetContext: contextService.Budget,
	})
}

type inventoryExecutables struct {
	Compozy  string
	Codex    string
	OpenCode string
	Cursor   string
}

func optionalExecutable(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return filepath.Clean(absolute)
}

func requireAbsoluteExecutable(name, value string) error {
	if strings.TrimSpace(value) == "" || !filepath.IsAbs(value) {
		return fmt.Errorf("batuta: %s executable must be absolute", name)
	}
	return nil
}

func newWithServices(services serviceSet) (*compozysdk.Extension, error) {
	app := application{services: services}
	extension := compozysdk.NewExtension(compozysdk.ExtensionDefinition{
		Name:        "batuta",
		Version:     "0.1.0-beta.5",
		Description: "Batuta conducts implementation, review, and verified publication on CompozyOS.",
		Resources: compozysdk.DescribeResources{
			Agents: []compozysdk.DescribeResourcePath{{Path: "agents"}},
			Skills: []compozysdk.DescribeResourcePath{{Path: "resources/skills"}},
			Loops:  []compozysdk.DescribeResourcePath{{Path: "loops"}},
		},
		Subprocess: compozysdk.DescribeSubprocess{
			Command: "./bin",
			Env: map[string]string{
				"COMPOZY_EXECUTABLE": "{{compozy_executable}}",
				"COMPOZY_HOME":       "{{env:COMPOZY_HOME}}",
			},
		},
	})
	if err := compozysdk.Tool(
		extension,
		"delivery_budget_context",
		compozysdk.ToolOptions{
			Description:  "Return the bounded review budget after one verified implementation child.",
			FriendlyVerb: "Read delivery budget",
			ReadOnly:     true,
			Risk:         compozysdk.RiskRead,
			InputSchema:  deliveryBudgetContextInputSchema(),
			OutputSchema: deliveryBudgetContextOutputSchema(),
		},
		func(ctx context.Context, req compozysdk.ToolRequest[DeliveryBudgetContextInput]) (compozysdk.ToolResult, error) {
			return app.deliveryBudgetContext(ctx, req.TrustedWorkspace, req.Input)
		},
	); err != nil {
		return nil, fmt.Errorf("batuta: register delivery budget context: %w", err)
	}
	if err := compozysdk.Tool(
		extension,
		"executor_inventory",
		compozysdk.ToolOptions{
			Description:  "Collect a redacted immutable snapshot of supported local executors.",
			FriendlyVerb: "Inventory executors",
			ReadOnly:     true,
			Risk:         compozysdk.RiskRead,
			InputSchema:  inventoryInputSchema(),
			OutputSchema: inventoryOutputSchema(),
		},
		func(ctx context.Context, req compozysdk.ToolRequest[struct{}]) (compozysdk.ToolResult, error) {
			return app.inventory(ctx, req.TrustedWorkspace)
		},
	); err != nil {
		return nil, fmt.Errorf("batuta: register executor inventory: %w", err)
	}
	if err := compozysdk.Tool(
		extension,
		"routing_plan",
		compozysdk.ToolOptions{
			Description:  "Validate authored task classification and propose an immutable runtime matrix.",
			FriendlyVerb: "Plan task routing",
			ReadOnly:     true,
			Risk:         compozysdk.RiskRead,
			InputSchema:  routingPlanInputSchema(),
			OutputSchema: routingGenerationOutputSchema(),
		},
		func(ctx context.Context, req compozysdk.ToolRequest[RoutingPlanInput]) (compozysdk.ToolResult, error) {
			return app.routingPlan(ctx, req.TrustedWorkspace, req.Input)
		},
	); err != nil {
		return nil, fmt.Errorf("batuta: register routing plan: %w", err)
	}
	if err := compozysdk.Tool(
		extension,
		"routing_context",
		compozysdk.ToolOptions{
			Description:  "Return the exact runtime rules and bounded budget for one delivery attempt.",
			FriendlyVerb: "Read routing context",
			ReadOnly:     true,
			Risk:         compozysdk.RiskRead,
			InputSchema:  routingContextInputSchema(),
			OutputSchema: routingContextOutputSchema(),
		},
		func(ctx context.Context, req compozysdk.ToolRequest[RoutingContextInput]) (compozysdk.ToolResult, error) {
			return app.routingContext(ctx, req.TrustedWorkspace, req.Input)
		},
	); err != nil {
		return nil, fmt.Errorf("batuta: register routing context: %w", err)
	}
	if err := compozysdk.Tool(
		extension,
		"routing_apply",
		compozysdk.ToolOptions{
			Description:  "Apply an owned routing matrix or perform one bounded delivery fallback operation.",
			FriendlyVerb: "Apply task routing",
			Risk:         compozysdk.RiskMutating,
			InputSchema:  routingApplyInputSchema(),
			OutputSchema: routingApplyOutputSchema(),
		},
		func(ctx context.Context, req compozysdk.ToolRequest[RoutingApplyInput]) (compozysdk.ToolResult, error) {
			return app.routingApply(ctx, req.TrustedWorkspace, req.Input)
		},
	); err != nil {
		return nil, fmt.Errorf("batuta: register routing apply: %w", err)
	}
	if err := compozysdk.Tool(
		extension,
		"publication_plan",
		compozysdk.ToolOptions{
			Description:  "Classify a daemon-trusted worktree for publication without mutation.",
			FriendlyVerb: "Plan publication",
			ReadOnly:     true,
			Risk:         compozysdk.RiskRead,
			InputSchema:  planInputSchema(),
			OutputSchema: planOutputSchema(),
		},
		func(ctx context.Context, req compozysdk.ToolRequest[publication.PlanInput]) (compozysdk.ToolResult, error) {
			return app.plan(ctx, req.TrustedWorkspace, req.Input)
		},
	); err != nil {
		return nil, fmt.Errorf("batuta: register publication plan: %w", err)
	}
	if err := compozysdk.Tool(
		extension,
		"publish_worktree",
		compozysdk.ToolOptions{
			Description:  "Push the exact trusted worktree HEAD and open its pull request.",
			FriendlyVerb: "Publish worktree",
			Risk:         compozysdk.RiskMutating,
			InputSchema:  publishInputSchema(),
			OutputSchema: publishOutputSchema(),
		},
		func(ctx context.Context, req compozysdk.ToolRequest[publication.PublishInput]) (compozysdk.ToolResult, error) {
			return app.publish(ctx, req.TrustedWorkspace, req.Input)
		},
	); err != nil {
		return nil, fmt.Errorf("batuta: register publisher: %w", err)
	}
	if err := compozysdk.Tool(
		extension,
		"publication_verify",
		compozysdk.ToolOptions{
			Description:  "Independently verify the exact published HEAD and pull request.",
			FriendlyVerb: "Verify publication",
			ReadOnly:     true,
			Risk:         compozysdk.RiskRead,
			InputSchema:  verifyInputSchema(),
			OutputSchema: verifyOutputSchema(),
		},
		func(ctx context.Context, req compozysdk.ToolRequest[publication.VerifyInput]) (compozysdk.ToolResult, error) {
			return app.verify(ctx, req.TrustedWorkspace, req.Input)
		},
	); err != nil {
		return nil, fmt.Errorf("batuta: register publication verifier: %w", err)
	}
	return extension, nil
}

func (a application) plan(
	ctx context.Context,
	workspace *compozysdk.ExtensionToolWorkspaceScope,
	input publication.PlanInput,
) (compozysdk.ToolResult, error) {
	scope, err := trustedScope(workspace)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	if a.services.plan == nil {
		return compozysdk.ToolResult{}, errors.New("batuta: publication planner is unavailable")
	}
	output, err := a.services.plan(ctx, scope, input)
	if err != nil {
		var blocked *publication.BlockedPlanError
		if !errors.As(err, &blocked) {
			return compozysdk.ToolResult{}, err
		}
		output = blocked.Plan
	}
	return compozysdk.StructuredResult(output)
}

func (a application) publish(
	ctx context.Context,
	workspace *compozysdk.ExtensionToolWorkspaceScope,
	input publication.PublishInput,
) (compozysdk.ToolResult, error) {
	scope, err := trustedScope(workspace)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	if a.services.publish == nil {
		return compozysdk.ToolResult{}, errors.New("batuta: publisher is unavailable")
	}
	output, err := a.services.publish(ctx, scope, input)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	return compozysdk.StructuredResult(output)
}

func (a application) verify(
	ctx context.Context,
	workspace *compozysdk.ExtensionToolWorkspaceScope,
	input publication.VerifyInput,
) (compozysdk.ToolResult, error) {
	scope, err := trustedScope(workspace)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	if a.services.verify == nil {
		return compozysdk.ToolResult{}, errors.New("batuta: verifier is unavailable")
	}
	output, err := a.services.verify(ctx, scope, input)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	return compozysdk.StructuredResult(output)
}

func trustedScope(workspace *compozysdk.ExtensionToolWorkspaceScope) (publication.TrustedScope, error) {
	if workspace == nil || strings.TrimSpace(workspace.ID) == "" ||
		strings.TrimSpace(workspace.Root) == "" || !filepath.IsAbs(workspace.Root) {
		return publication.TrustedScope{}, errors.New("batuta: daemon-trusted workspace is required")
	}
	return publication.TrustedScope{
		WorkspaceID:   strings.TrimSpace(workspace.ID),
		WorkspaceRoot: filepath.Clean(workspace.Root),
	}, nil
}

func planInputSchema() map[string]any {
	return objectSchema([]string{"worktree_ref"}, map[string]any{
		"worktree_ref": map[string]any{"type": "string", "minLength": 1},
	})
}

func publishInputSchema() map[string]any {
	return objectSchema([]string{"worktree_ref", "expected_head_sha"}, map[string]any{
		"worktree_ref":      map[string]any{"type": "string", "minLength": 1},
		"expected_head_sha": map[string]any{"type": "string", "pattern": "^[0-9a-f]{40,64}$"},
	})
}

func verifyInputSchema() map[string]any {
	return objectSchema([]string{"worktree_ref", "expected_head_sha", "publisher_result"}, map[string]any{
		"worktree_ref":      map[string]any{"type": "string", "minLength": 1},
		"expected_head_sha": map[string]any{"type": "string", "pattern": "^[0-9a-f]{40,64}$"},
		"publisher_result":  publishOutputSchema(),
	})
}

func planOutputSchema() map[string]any {
	return objectSchema([]string{"disposition", "worktree_id", "branch", "base_branch", "worktree_path", "head_sha", "clean", "exit_plan"}, map[string]any{
		"disposition":   map[string]any{"enum": []string{"publishable", "nothing_to_publish", "blocked"}},
		"worktree_id":   map[string]any{"type": "string"},
		"branch":        map[string]any{"type": "string"},
		"base_branch":   map[string]any{"type": "string"},
		"worktree_path": map[string]any{"type": "string"},
		"head_sha":      map[string]any{"type": "string"},
		"clean":         map[string]any{"type": "boolean"},
		"exit_plan":     map[string]any{"type": "object"},
		"blockers":      map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
	})
}

func publishOutputSchema() map[string]any {
	return objectSchema([]string{"status", "head_sha", "op_ids", "summary", "last_exit_plan"}, map[string]any{
		"status":         map[string]any{"enum": []string{"published", "nothing_to_publish", "blocked"}},
		"head_sha":       map[string]any{"type": "string"},
		"op_ids":         map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"pr_url":         map[string]any{"type": "string"},
		"summary":        map[string]any{"type": "string"},
		"last_exit_plan": map[string]any{"type": "object"},
	})
}

func verifyOutputSchema() map[string]any {
	return objectSchema([]string{"verified", "status", "head_sha", "summary"}, map[string]any{
		"verified": map[string]any{"type": "boolean"},
		"status":   map[string]any{"enum": []string{"published", "nothing_to_publish", "blocked"}},
		"head_sha": map[string]any{"type": "string"},
		"pr_url":   map[string]any{"type": "string"},
		"summary":  map[string]any{"type": "string"},
	})
}

func objectSchema(required []string, properties map[string]any) map[string]any {
	return map[string]any{
		"type":                 "object",
		"required":             required,
		"properties":           properties,
		"additionalProperties": false,
	}
}
