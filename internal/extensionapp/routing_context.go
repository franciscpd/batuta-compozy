package extensionapp

import (
	"context"
	"errors"
	"strconv"
	"sync"
	"time"

	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/franciscpd/batuta-compozy/internal/publication"
	"github.com/franciscpd/batuta-compozy/internal/routing"
)

type RoutingContextInput struct {
	DeliveryID        string `json:"delivery_id"`
	Attempt           int    `json:"attempt"`
	Slug              string `json:"slug"`
	RoutingGeneration string `json:"routing_generation"`
}

type RoutingContextOutput struct {
	RuntimeRules         []routing.RuntimeRule `json:"runtime_rules"`
	RemainingTokens      int64                 `json:"remaining_tokens"`
	RemainingWallSeconds int                   `json:"remaining_wall_seconds"`
}

type DeliveryBudgetContextInput struct {
	DeliveryID string `json:"delivery_id"`
	Attempt    int    `json:"attempt"`
}

type DeliveryBudgetContextOutput struct {
	RemainingTokens      int64 `json:"remaining_tokens"`
	RemainingWallSeconds int   `json:"remaining_wall_seconds"`
}

type deliveryContextService struct {
	Store        *routing.OwnershipStore
	StoreForCall func() (*routing.OwnershipStore, error)
	Client       deliveryRunClient
	Now          func() time.Time

	mu        sync.Mutex
	accounted map[string]map[string]int64
}

func (a application) routingContext(
	ctx context.Context,
	workspace *compozysdk.ExtensionToolWorkspaceScope,
	input RoutingContextInput,
) (compozysdk.ToolResult, error) {
	scope, err := trustedScope(workspace)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	if a.services.routingContext == nil {
		return compozysdk.ToolResult{}, errors.New("batuta: routing context is unavailable")
	}
	output, err := a.services.routingContext(ctx, scope, input)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	return compozysdk.StructuredResult(output)
}

func (a application) deliveryBudgetContext(
	ctx context.Context,
	workspace *compozysdk.ExtensionToolWorkspaceScope,
	input DeliveryBudgetContextInput,
) (compozysdk.ToolResult, error) {
	scope, err := trustedScope(workspace)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	if a.services.deliveryBudgetContext == nil {
		return compozysdk.ToolResult{}, errors.New("batuta: delivery budget context is unavailable")
	}
	output, err := a.services.deliveryBudgetContext(ctx, scope, input)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	return compozysdk.StructuredResult(output)
}

func (s *deliveryContextService) Routing(
	ctx context.Context,
	scope publication.TrustedScope,
	input RoutingContextInput,
) (RoutingContextOutput, error) {
	if err := ctx.Err(); err != nil {
		return RoutingContextOutput{}, err
	}
	if s == nil || !routingDigestPattern.MatchString(input.DeliveryID) ||
		input.Attempt < 1 || !canonicalSlugPattern.MatchString(input.Slug) ||
		!routingDigestPattern.MatchString(input.RoutingGeneration) || !validOpaqueRunID(scope.WorkspaceID) {
		return RoutingContextOutput{}, routing.ErrDeliveryConflict
	}
	store, err := s.store()
	if err != nil {
		return RoutingContextOutput{}, err
	}
	journal, exists, err := store.Load(scope.WorkspaceID)
	if err != nil {
		return RoutingContextOutput{}, err
	}
	if !exists {
		return RoutingContextOutput{}, routing.ErrDeliveryConflict
	}
	delivery, attempt, err := deliveryAttemptForContext(journal, scope.WorkspaceID, input.DeliveryID, input.Attempt)
	if err != nil || delivery.Slug != input.Slug || delivery.RoutingGenerationDigest != input.RoutingGeneration {
		return RoutingContextOutput{}, routing.ErrDeliveryConflict
	}
	if _, exists := journal.Generations[input.RoutingGeneration]; !exists {
		return RoutingContextOutput{}, routing.ErrOwnershipUnproven
	}
	loader, err := routing.NewArtifactLoader(delivery.WorktreeRoot)
	if err != nil {
		return RoutingContextOutput{}, err
	}
	taskSet, err := loader.Load(input.Slug)
	if err != nil {
		return RoutingContextOutput{}, err
	}
	currentSnapshot, err := taskSet.DeliverySnapshot()
	if err != nil || taskSet.Digest != delivery.TaskSetDigest {
		return RoutingContextOutput{}, routing.ErrDeliveryConflict
	}
	progress, err := delivery.TaskSnapshot.Reconcile(currentSnapshot)
	if err != nil || len(progress.IncompleteTaskIDs) == 0 {
		return RoutingContextOutput{}, routing.ErrDeliveryConflict
	}
	remainingTokens, remainingWall, err := deliveryRemainingBudget(delivery, s.now())
	if err != nil {
		return RoutingContextOutput{}, err
	}
	return RoutingContextOutput{
		RuntimeRules:         append([]routing.RuntimeRule(nil), attempt.RuntimeRules...),
		RemainingTokens:      remainingTokens,
		RemainingWallSeconds: remainingWall,
	}, nil
}

func (s *deliveryContextService) Budget(
	ctx context.Context,
	scope publication.TrustedScope,
	input DeliveryBudgetContextInput,
) (DeliveryBudgetContextOutput, error) {
	if err := ctx.Err(); err != nil {
		return DeliveryBudgetContextOutput{}, err
	}
	if s == nil || s.Client == nil || !routingDigestPattern.MatchString(input.DeliveryID) ||
		input.Attempt < 1 || !validOpaqueRunID(scope.WorkspaceID) {
		return DeliveryBudgetContextOutput{}, routing.ErrDeliveryConflict
	}
	store, err := s.store()
	if err != nil {
		return DeliveryBudgetContextOutput{}, err
	}
	journal, exists, err := store.Load(scope.WorkspaceID)
	if err != nil {
		return DeliveryBudgetContextOutput{}, err
	}
	if !exists {
		return DeliveryBudgetContextOutput{}, routing.ErrDeliveryConflict
	}
	delivery, attempt, err := deliveryAttemptForContext(journal, scope.WorkspaceID, input.DeliveryID, input.Attempt)
	if err != nil || attempt.State != routing.AttemptSubmitted || !validOpaqueRunID(attempt.RunID) {
		return DeliveryBudgetContextOutput{}, routing.ErrDeliveryConflict
	}
	parent, err := s.Client.Status(ctx, scope.WorkspaceID, attempt.RunID)
	if err != nil {
		return DeliveryBudgetContextOutput{}, err
	}
	if parent.Run.ID != attempt.RunID || parent.Run.WorkspaceID != scope.WorkspaceID ||
		parent.Run.LoopName != "batuta-deliver" || !runMatchesAttempt(parent.Run, delivery, attempt) {
		return DeliveryBudgetContextOutput{}, routing.ErrDeliveryConflict
	}
	implementationRunID, err := ownedImplementationRunID(parent)
	if err != nil {
		return DeliveryBudgetContextOutput{}, err
	}
	child, err := s.Client.Status(ctx, scope.WorkspaceID, implementationRunID)
	if err != nil {
		return DeliveryBudgetContextOutput{}, err
	}
	if child.Run.ID != implementationRunID || child.Run.WorkspaceID != scope.WorkspaceID ||
		child.Run.ParentLoopRunID != attempt.RunID || child.Run.LoopName != "implement-tasks" ||
		(child.Run.Status != "done" && child.Run.Status != "no-op") || !child.Run.TokensUsedPresent || child.Run.TokensUsed < 0 {
		return DeliveryBudgetContextOutput{}, routing.ErrDeliveryConflict
	}
	baseTokens, remainingWall, err := deliveryRemainingBudget(delivery, s.now())
	if err != nil {
		return DeliveryBudgetContextOutput{}, err
	}
	key := scope.WorkspaceID + "\x00" + input.DeliveryID + "\x00" + strconv.Itoa(input.Attempt)
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.accounted == nil {
		s.accounted = map[string]map[string]int64{}
	}
	children := s.accounted[key]
	if prior, exists := children[implementationRunID]; exists && prior != child.Run.TokensUsed {
		return DeliveryBudgetContextOutput{}, routing.ErrDeliveryConflict
	}
	currentTokens := int64(0)
	for _, tokens := range children {
		currentTokens += tokens
	}
	if _, exists := children[implementationRunID]; !exists {
		currentTokens += child.Run.TokensUsed
	}
	remainingTokens := baseTokens - currentTokens
	if remainingTokens <= 0 {
		return DeliveryBudgetContextOutput{}, routing.ErrNoEligibleCandidate
	}
	if children == nil {
		children = map[string]int64{}
		s.accounted[key] = children
	}
	children[implementationRunID] = child.Run.TokensUsed
	return DeliveryBudgetContextOutput{RemainingTokens: remainingTokens, RemainingWallSeconds: remainingWall}, nil
}

func ownedImplementationRunID(parent deliveryRunDetail) (string, error) {
	childIDs := map[string]struct{}{}
	for _, generation := range parent.Generations {
		for _, output := range generation.Outputs {
			if output.NodeID != "implement" || output.Status != "succeeded" || !validOpaqueRunID(output.ChildLoopRunID) {
				continue
			}
			childIDs[output.ChildLoopRunID] = struct{}{}
		}
	}
	if len(childIDs) != 1 {
		return "", routing.ErrDeliveryConflict
	}
	for childID := range childIDs {
		return childID, nil
	}
	return "", routing.ErrDeliveryConflict
}

func deliveryAttemptForContext(
	journal routing.RoutingJournal,
	workspaceID string,
	deliveryID string,
	attemptNumber int,
) (routing.DeliveryRecord, routing.DeliveryAttempt, error) {
	delivery, exists := journal.Deliveries[deliveryID]
	if !exists || delivery.DeliveryID != deliveryID || delivery.WorkspaceID != workspaceID ||
		delivery.State != routing.DeliveryStateActive || attemptNumber > len(delivery.Attempts) {
		return routing.DeliveryRecord{}, routing.DeliveryAttempt{}, routing.ErrDeliveryConflict
	}
	attempt := delivery.Attempts[attemptNumber-1]
	if attempt.Attempt != attemptNumber || (attempt.State != routing.AttemptPlanned && attempt.State != routing.AttemptSubmitted) {
		return routing.DeliveryRecord{}, routing.DeliveryAttempt{}, routing.ErrDeliveryConflict
	}
	return delivery, attempt, nil
}

func deliveryRemainingBudget(delivery routing.DeliveryRecord, now time.Time) (int64, int, error) {
	usedTokens := int64(0)
	for _, attempt := range delivery.Attempts {
		if attempt.State == routing.AttemptTerminal {
			if attempt.TokensUsed < 0 {
				return 0, 0, routing.ErrDeliveryConflict
			}
			usedTokens += attempt.TokensUsed
		}
	}
	remainingTokens := delivery.TokenCeiling - usedTokens
	remainingWall := int(delivery.AbsoluteDeadline.Sub(now) / time.Second)
	if remainingTokens <= 0 || remainingWall <= 0 {
		return 0, 0, routing.ErrNoEligibleCandidate
	}
	return remainingTokens, remainingWall, nil
}

func (s *deliveryContextService) now() time.Time {
	if s.Now != nil {
		return s.Now().UTC()
	}
	return time.Now().UTC()
}

func (s *deliveryContextService) store() (*routing.OwnershipStore, error) {
	if s == nil {
		return nil, routing.ErrOwnershipUnproven
	}
	if s.Store != nil {
		return s.Store, nil
	}
	if s.StoreForCall == nil {
		return nil, routing.ErrOwnershipUnproven
	}
	store, err := s.StoreForCall()
	if err != nil || store == nil {
		if err != nil {
			return nil, err
		}
		return nil, routing.ErrOwnershipUnproven
	}
	return store, nil
}

func routingContextInputSchema() map[string]any {
	return objectSchema([]string{"delivery_id", "attempt", "slug", "routing_generation"}, map[string]any{
		"delivery_id":        sha256OutputSchema(),
		"attempt":            map[string]any{"type": "integer", "minimum": 1, "maximum": 4},
		"slug":               map[string]any{"type": "string", "pattern": "^[a-z0-9]+(?:-[a-z0-9]+)*$"},
		"routing_generation": sha256OutputSchema(),
	})
}

func routingContextOutputSchema() map[string]any {
	return objectSchema([]string{"runtime_rules", "remaining_tokens", "remaining_wall_seconds"}, map[string]any{
		"runtime_rules":          map[string]any{"type": "array", "minItems": 1, "items": runtimeRuleOutputSchema()},
		"remaining_tokens":       map[string]any{"type": "integer", "minimum": 1},
		"remaining_wall_seconds": map[string]any{"type": "integer", "minimum": 1},
	})
}

func deliveryBudgetContextInputSchema() map[string]any {
	return objectSchema([]string{"delivery_id", "attempt"}, map[string]any{
		"delivery_id": sha256OutputSchema(),
		"attempt":     map[string]any{"type": "integer", "minimum": 1, "maximum": 4},
	})
}

func deliveryBudgetContextOutputSchema() map[string]any {
	return objectSchema([]string{"remaining_tokens", "remaining_wall_seconds"}, map[string]any{
		"remaining_tokens":       map[string]any{"type": "integer", "minimum": 1},
		"remaining_wall_seconds": map[string]any{"type": "integer", "minimum": 1},
	})
}
