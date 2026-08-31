package extensionapp

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"slices"
	"strings"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/franciscpd/batuta-compozy/internal/publication"
	"github.com/franciscpd/batuta-compozy/internal/repository"
	"github.com/franciscpd/batuta-compozy/internal/routing"
)

type matrixApplyFunc func(context.Context, routing.MatrixApplyInput) (routing.MatrixApplyResult, error)
type routingWorktreeInspectFunc func(context.Context, publication.TrustedScope, string) (publication.WorktreeInspection, error)
type routingWorktreeStateFunc func(context.Context, string) (publication.WorktreeState, error)
type startDeliveryFunc func(context.Context, publication.TrustedScope, string) (RoutingStartResult, error)
type recoveryFunc func(context.Context, publication.TrustedScope, string, string) (RoutingStartResult, error)
type reconcileFunc func(context.Context, publication.TrustedScope, string, string) (RoutingReconcileResult, error)
type alignmentStatusFunc func(string, routing.RoutingGeneration) (routing.AlignmentStatus, error)
type alignmentConfirmFunc func(string, string, routing.RoutingGeneration) (routing.AlignmentStatus, error)
type repositoryBootstrapFunc func(context.Context, string) (repository.BootstrapResult, error)

type routingEngine struct {
	inventory           inventoryFunc
	applyMatrix         matrixApplyFunc
	inspectWorktree     routingWorktreeInspectFunc
	worktreeState       routingWorktreeStateFunc
	startDelivery       startDeliveryFunc
	recover             recoveryFunc
	reconcile           reconcileFunc
	alignmentStatus     alignmentStatusFunc
	alignmentConfirm    alignmentConfirmFunc
	bootstrapRepository repositoryBootstrapFunc
}

func (e *routingEngine) Plan(
	ctx context.Context,
	scope publication.TrustedScope,
	input RoutingPlanInput,
) (routing.RoutingGeneration, error) {
	if e == nil || e.inventory == nil {
		return routing.RoutingGeneration{}, errors.New("batuta: routing inventory is unavailable")
	}
	loader, err := routing.NewArtifactLoader(scope.WorkspaceRoot)
	if err != nil {
		return routing.RoutingGeneration{}, err
	}
	taskSet, err := loader.Load(input.Slug)
	if err != nil {
		return routing.RoutingGeneration{}, err
	}
	graph, err := routing.ValidateClassification(taskSet, input.Proposals)
	if err != nil {
		return routing.RoutingGeneration{}, err
	}
	snapshot, err := e.inventory(ctx, scope)
	if err != nil {
		return routing.RoutingGeneration{}, err
	}
	catalog, err := liveCatalogFromInventory(snapshot)
	if err != nil {
		return routing.RoutingGeneration{}, err
	}
	bindings, err := routing.BuildCandidateBindings(snapshot, catalog)
	if err != nil {
		return routing.RoutingGeneration{}, err
	}
	selector := routing.NewSelector(routing.DefaultSelectionPolicy())
	fit, err := validatedCellFit(graph, input.Fit)
	if err != nil {
		return routing.RoutingGeneration{}, err
	}
	return selector.Select(routing.SelectionInput{
		Graph: graph, Inventory: snapshot, Catalog: catalog, Bindings: bindings, Fit: fit,
		WorkspaceIdentityDigest: trustedWorkspaceDigest(scope),
		EnclosingBudget:         routing.LoopBudgetCeiling{IterationCap: 4, WallTimeSeconds: 14400},
	})
}

func validatedCellFit(graph routing.ValidatedTaskGraph, proposals []RoutingFitProposal) ([]routing.CellFitRecommendation, error) {
	expected := map[string][]string{}
	for _, task := range graph.Tasks {
		key := string(task.Domain) + "\x00" + string(task.Complexity)
		expected[key] = append(expected[key], task.ID)
	}
	for key := range expected {
		slices.Sort(expected[key])
	}
	seen := map[string]struct{}{}
	result := make([]routing.CellFitRecommendation, 0, len(proposals))
	for _, proposal := range proposals {
		key := string(proposal.Domain) + "\x00" + string(proposal.Complexity)
		want, exists := expected[key]
		if !exists {
			return nil, errors.New("batuta: fit proposal references an unpopulated routing cell")
		}
		if _, duplicate := seen[key]; duplicate {
			return nil, errors.New("batuta: fit proposal duplicates a routing cell")
		}
		taskIDs := slices.Clone(proposal.TaskIDs)
		slices.Sort(taskIDs)
		if !slices.Equal(taskIDs, want) || len(slices.Compact(taskIDs)) != len(taskIDs) {
			return nil, errors.New("batuta: fit proposal task IDs do not match the validated cell")
		}
		seen[key] = struct{}{}
		result = append(result, routing.CellFitRecommendation{
			Domain: proposal.Domain, Complexity: proposal.Complexity,
			Candidates: slices.Clone(proposal.Candidates),
		})
	}
	if len(seen) != len(expected) {
		return nil, errors.New("batuta: fit proposal coverage does not match populated routing cells")
	}
	return result, nil
}

func (e *routingEngine) Apply(
	ctx context.Context,
	scope publication.TrustedScope,
	input RoutingApplyInput,
) (RoutingApplyOutput, error) {
	if err := input.validate(); err != nil {
		return RoutingApplyOutput{}, err
	}
	switch input.Operation {
	case RoutingOperationAlignmentStatus, RoutingOperationConfirmAlignment:
		fresh, err := e.Plan(ctx, scope, *input.RoutingPlan)
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		if fresh.Digest != input.ExpectedGenerationDigest {
			return RoutingApplyOutput{}, errors.New("batuta: routing generation changed before alignment")
		}
		var alignment routing.AlignmentStatus
		if input.Operation == RoutingOperationAlignmentStatus {
			if e.alignmentStatus == nil {
				return RoutingApplyOutput{}, errors.New("batuta: routing alignment status is unavailable")
			}
			alignment, err = e.alignmentStatus(scope.WorkspaceID, fresh)
		} else {
			if e.alignmentConfirm == nil {
				return RoutingApplyOutput{}, errors.New("batuta: routing alignment confirmation is unavailable")
			}
			alignment, err = e.alignmentConfirm(scope.WorkspaceID, input.OriginSessionID, fresh)
		}
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		return RoutingApplyOutput{Operation: input.Operation, Alignment: &alignment}, nil
	case RoutingOperationBootstrapRepository:
		fresh, err := e.Plan(ctx, scope, *input.RoutingPlan)
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		if fresh.Digest != input.ExpectedGenerationDigest {
			return RoutingApplyOutput{}, errors.New("batuta: routing generation changed before repository bootstrap")
		}
		if e.alignmentStatus == nil || e.bootstrapRepository == nil {
			return RoutingApplyOutput{}, errors.New("batuta: repository bootstrap is unavailable")
		}
		alignment, err := e.alignmentStatus(scope.WorkspaceID, fresh)
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		if alignment.State != routing.AlignmentConfirmed {
			return RoutingApplyOutput{}, routing.ErrRoutingAlignmentRequired
		}
		result, err := e.bootstrapRepository(ctx, scope.WorkspaceRoot)
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		return RoutingApplyOutput{Operation: input.Operation, Repository: &result}, nil
	case RoutingOperationApplyMatrix:
		if e == nil || e.applyMatrix == nil {
			return RoutingApplyOutput{}, errors.New("batuta: routing matrix application is unavailable")
		}
		fresh, err := e.Plan(ctx, scope, *input.RoutingPlan)
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		if fresh.Digest != input.ExpectedGenerationDigest {
			return RoutingApplyOutput{}, errors.New("batuta: routing generation changed before apply")
		}
		if e.inspectWorktree == nil || e.worktreeState == nil {
			return RoutingApplyOutput{}, errors.New("batuta: trusted worktree evidence is unavailable")
		}
		inspection, err := e.inspectWorktree(ctx, scope, input.WorktreeRef)
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		if inspection.Worktree.ID != input.WorktreeRef || inspection.Worktree.WorkspaceID != scope.WorkspaceID ||
			strings.TrimSpace(inspection.Worktree.Path) == "" {
			return RoutingApplyOutput{}, errors.New("batuta: trusted worktree evidence is inconsistent")
		}
		state, err := e.worktreeState(ctx, inspection.Worktree.Path)
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		loader, err := routing.NewArtifactLoader(inspection.Worktree.Path)
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		taskSet, err := loader.Load(input.RoutingPlan.Slug)
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		taskSnapshot, err := taskSet.DeliverySnapshot()
		if err != nil || taskSet.Digest != fresh.TaskSetDigest {
			return RoutingApplyOutput{}, errors.New("batuta: task set changed before apply")
		}
		result, err := e.applyMatrix(ctx, routing.MatrixApplyInput{
			WorkspaceID: scope.WorkspaceID, WorkspaceRoot: scope.WorkspaceRoot,
			WorktreeID: inspection.Worktree.ID, WorktreeRoot: inspection.Worktree.Path,
			Slug: input.RoutingPlan.Slug, OriginSessionID: input.OriginSessionID,
			TaskSetDigest: taskSet.Digest, TaskSnapshot: taskSnapshot,
			InitialWorktreeFingerprint: routing.WorktreeFingerprint{
				HeadSHA: state.HeadSHA, PorcelainSHA256: state.PorcelainSHA256, ContentSHA256: state.ContentSHA256,
			},
			Generation: fresh,
		})
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		return RoutingApplyOutput{Operation: input.Operation, Matrix: &result}, nil
	case RoutingOperationStartDelivery:
		if e == nil || e.startDelivery == nil {
			return RoutingApplyOutput{}, errors.New("batuta: delivery attempt service is unavailable")
		}
		result, err := e.startDelivery(ctx, scope, input.DeliveryID)
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		return RoutingApplyOutput{Operation: input.Operation, Start: &result}, nil
	case RoutingOperationRecoverDelivery:
		if e == nil || e.recover == nil {
			return RoutingApplyOutput{}, errors.New("batuta: delivery fallback service is unavailable")
		}
		result, err := e.recover(ctx, scope, input.DeliveryID, input.DeliveryRunID)
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		return RoutingApplyOutput{Operation: input.Operation, Start: &result}, nil
	case RoutingOperationReconcileFallbacks:
		if e == nil || e.reconcile == nil {
			return RoutingApplyOutput{}, errors.New("batuta: delivery fallback service is unavailable")
		}
		result, err := e.reconcile(ctx, scope, input.DeliveryID, input.DeliveryRunID)
		if err != nil {
			return RoutingApplyOutput{}, err
		}
		return RoutingApplyOutput{Operation: input.Operation, Reconciliation: &result}, nil
	default:
		return RoutingApplyOutput{}, errors.New("batuta: unsupported routing operation")
	}
}

func liveCatalogFromInventory(snapshot inventory.InventorySnapshot) (routing.LiveCatalog, error) {
	if err := snapshot.Validate(); err != nil || strings.TrimSpace(snapshot.CompozyCatalogGeneration) == "" {
		return routing.LiveCatalog{}, errors.New("batuta: live Compozy model catalog is unavailable")
	}
	catalog := routing.LiveCatalog{Generation: snapshot.CompozyCatalogGeneration}
	models := make(map[string]routing.CatalogModel)
	for _, executor := range snapshot.Executors {
		if executor.ID != inventory.ExecutorCompozy || executor.Availability != inventory.AvailabilityAvailable {
			continue
		}
		providerAuth := make(map[string]inventory.CredentialState)
		for _, capability := range executor.Capabilities {
			if capability.Name != "provider_auth" || capability.State != inventory.ResolutionResolved {
				continue
			}
			for _, identifier := range capability.Identifiers {
				provider, state, ok := strings.Cut(identifier, "=")
				if !ok {
					continue
				}
				switch inventory.CredentialState(state) {
				case inventory.CredentialConfigured, inventory.CredentialMissing, inventory.CredentialUnknown:
					providerAuth[provider] = inventory.CredentialState(state)
				}
			}
		}
		for _, capability := range executor.Capabilities {
			availability := inventory.AvailabilityUnknown
			switch capability.Name {
			case "models":
				availability = inventory.AvailabilityAvailable
			case "catalog_models_unknown":
			default:
				continue
			}
			if capability.State != inventory.ResolutionResolved || capability.Digest != snapshot.CompozyCatalogGeneration {
				continue
			}
			for _, identifier := range capability.Identifiers {
				provider, model, ok := strings.Cut(identifier, "/")
				if !ok || strings.TrimSpace(provider) == "" || strings.TrimSpace(model) == "" {
					continue
				}
				key := routing.ModelKey(provider, model)
				if existing, found := models[key]; found && existing.Availability == inventory.AvailabilityAvailable {
					continue
				}
				models[key] = routing.CatalogModel{ProviderID: provider, ModelID: model, Availability: availability, CredentialState: providerAuth[provider]}
			}
		}
	}
	keys := make([]string, 0, len(models))
	for key := range models {
		keys = append(keys, key)
	}
	slices.Sort(keys)
	for _, key := range keys {
		catalog.Models = append(catalog.Models, models[key])
	}
	if len(catalog.Models) == 0 {
		return routing.LiveCatalog{}, errors.New("batuta: live Compozy model catalog is unavailable")
	}
	return catalog, nil
}

func trustedWorkspaceDigest(scope publication.TrustedScope) string {
	digest := sha256.Sum256([]byte(scope.WorkspaceID + "\x00" + scope.WorkspaceRoot))
	return "sha256:" + hex.EncodeToString(digest[:])
}
