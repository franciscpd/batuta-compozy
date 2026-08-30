package extensionapp

import (
	"context"
	"errors"
	"regexp"
	"strings"

	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/franciscpd/batuta-compozy/internal/repository"
	"github.com/franciscpd/batuta-compozy/internal/routing"
)

type RoutingPlanInput struct {
	Slug      string                           `json:"slug"`
	Proposals []routing.ClassificationProposal `json:"proposals"`
	Fit       []RoutingFitProposal             `json:"fit"`
}

type RoutingFitProposal struct {
	TaskIDs    []string               `json:"task_ids"`
	Domain     routing.Domain         `json:"domain"`
	Complexity routing.Complexity     `json:"complexity"`
	Candidates []routing.FitCandidate `json:"candidates"`
}

type RoutingOperation string

const (
	RoutingOperationAlignmentStatus     RoutingOperation = "alignment_status"
	RoutingOperationConfirmAlignment    RoutingOperation = "confirm_alignment"
	RoutingOperationBootstrapRepository RoutingOperation = "bootstrap_repository"
	RoutingOperationApplyMatrix         RoutingOperation = "apply_matrix"
	RoutingOperationStartDelivery       RoutingOperation = "start_delivery"
	RoutingOperationRecoverDelivery     RoutingOperation = "recover_delivery"
	RoutingOperationReconcileFallbacks  RoutingOperation = "reconcile_fallbacks"
)

type RoutingApplyInput struct {
	Operation                RoutingOperation  `json:"operation"`
	RoutingPlan              *RoutingPlanInput `json:"routing_plan,omitempty"`
	ExpectedGenerationDigest string            `json:"expected_generation_digest,omitempty"`
	WorktreeRef              string            `json:"worktree_ref,omitempty"`
	OriginSessionID          string            `json:"origin_session_id,omitempty"`
	DeliveryID               string            `json:"delivery_id,omitempty"`
	DeliveryRunID            string            `json:"delivery_run_id,omitempty"`
}

type RoutingApplyOutput struct {
	Operation      RoutingOperation            `json:"operation"`
	Alignment      *routing.AlignmentStatus    `json:"alignment,omitempty"`
	Repository     *repository.BootstrapResult `json:"repository,omitempty"`
	Matrix         *routing.MatrixApplyResult  `json:"matrix,omitempty"`
	Start          *RoutingStartResult         `json:"start,omitempty"`
	Reconciliation *RoutingReconcileResult     `json:"reconciliation,omitempty"`
}

type RoutingReconcileResult struct {
	DeliveryID       string `json:"delivery_id"`
	Attempt          int    `json:"attempt"`
	DeliveryRunID    string `json:"delivery_run_id"`
	State            string `json:"state"`
	Recoverable      bool   `json:"recoverable"`
	AttemptsUsed     int    `json:"attempts_used"`
	AttemptsLimit    int    `json:"attempts_limit"`
	TokensUsed       int64  `json:"tokens_used"`
	TokensLimit      int64  `json:"tokens_limit"`
	RemainingWallSec int    `json:"remaining_wall_sec"`
	BlockerCode      string `json:"blocker_code,omitempty"`
}

var routingDigestPattern = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)

func (a application) routingPlan(
	ctx context.Context,
	workspace *compozysdk.ExtensionToolWorkspaceScope,
	input RoutingPlanInput,
) (compozysdk.ToolResult, error) {
	scope, err := trustedScope(workspace)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	if a.services.routingPlan == nil {
		return compozysdk.ToolResult{}, errors.New("batuta: routing planner is unavailable")
	}
	generation, err := a.services.routingPlan(ctx, scope, input)
	if err != nil {
		if rpcErr := routingPlanDomainError(err); rpcErr != nil {
			return compozysdk.ToolResult{}, rpcErr
		}
		return compozysdk.ToolResult{}, err
	}
	return compozysdk.StructuredResult(generation)
}

func routingPlanDomainError(err error) *compozysdk.RPCError {
	reasonCode := ""
	switch {
	case errors.Is(err, routing.ErrClassificationRetryable):
		reasonCode = "classification_retryable"
	case errors.Is(err, routing.ErrSelectionRetryable):
		reasonCode = "routing_fit_retryable"
	case errors.Is(err, routing.ErrCatalogDrift):
		reasonCode = "catalog_drift"
	case errors.Is(err, routing.ErrReauthoringRequired):
		reasonCode = "task_reauthoring_required"
	case errors.Is(err, routing.ErrNoEligibleCandidate):
		reasonCode = "no_eligible_runtime"
	default:
		return nil
	}
	reasonCodes := []string{reasonCode}
	for _, candidateCode := range []string{
		"executor_unavailable", "credential_missing", "catalog_pair_unavailable",
		"executor_model_unproven", "model_below_floor", "hard_capability_unresolved",
	} {
		if strings.Contains(err.Error(), candidateCode) {
			reasonCodes = append(reasonCodes, candidateCode)
			break
		}
	}
	return compozysdk.NewRPCError(-32010, "Tool execution failed", map[string]any{
		"code":         "tool_invalid_input",
		"message":      "batuta: routing proposal rejected",
		"reason_codes": reasonCodes,
	})
}

func (a application) routingApply(
	ctx context.Context,
	workspace *compozysdk.ExtensionToolWorkspaceScope,
	input RoutingApplyInput,
) (compozysdk.ToolResult, error) {
	scope, err := trustedScope(workspace)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	if err := input.validate(); err != nil {
		return compozysdk.ToolResult{}, err
	}
	if a.services.routingApply == nil {
		return compozysdk.ToolResult{}, errors.New("batuta: routing application is unavailable")
	}
	output, err := a.services.routingApply(ctx, scope, input)
	if err != nil {
		if rpcErr := routingPlanDomainError(err); rpcErr != nil {
			return compozysdk.ToolResult{}, rpcErr
		}
		return compozysdk.ToolResult{}, err
	}
	return compozysdk.StructuredResult(output)
}

func (i RoutingApplyInput) validate() error {
	runID := strings.TrimSpace(i.DeliveryRunID)
	deliveryID := strings.TrimSpace(i.DeliveryID)
	switch i.Operation {
	case RoutingOperationAlignmentStatus:
		if i.RoutingPlan == nil || !routingDigestPattern.MatchString(i.ExpectedGenerationDigest) || i.WorktreeRef != "" ||
			i.OriginSessionID != "" || runID != "" || deliveryID != "" {
			return errors.New("batuta: alignment_status requires only routing_plan and expected_generation_digest")
		}
	case RoutingOperationConfirmAlignment:
		if i.RoutingPlan == nil || !routingDigestPattern.MatchString(i.ExpectedGenerationDigest) || i.WorktreeRef != "" ||
			!validOpaqueRunID(strings.TrimSpace(i.OriginSessionID)) || runID != "" || deliveryID != "" {
			return errors.New("batuta: confirm_alignment requires routing_plan, expected_generation_digest, and origin_session_id")
		}
	case RoutingOperationBootstrapRepository:
		if i.RoutingPlan == nil || !routingDigestPattern.MatchString(i.ExpectedGenerationDigest) || i.WorktreeRef != "" ||
			i.OriginSessionID != "" || runID != "" || deliveryID != "" {
			return errors.New("batuta: bootstrap_repository requires only routing_plan and expected_generation_digest")
		}
	case RoutingOperationApplyMatrix:
		if i.RoutingPlan == nil || runID != "" || deliveryID != "" || !routingDigestPattern.MatchString(i.ExpectedGenerationDigest) ||
			!validOpaqueRunID(strings.TrimSpace(i.WorktreeRef)) || !validOpaqueRunID(strings.TrimSpace(i.OriginSessionID)) {
			return errors.New("batuta: apply_matrix requires routing_plan, expected_generation_digest, worktree_ref, and origin_session_id")
		}
	case RoutingOperationStartDelivery:
		if i.RoutingPlan != nil || i.ExpectedGenerationDigest != "" || i.WorktreeRef != "" || i.OriginSessionID != "" || runID != "" || !routingDigestPattern.MatchString(deliveryID) {
			return errors.New("batuta: start_delivery requires only delivery_id")
		}
	case RoutingOperationRecoverDelivery, RoutingOperationReconcileFallbacks:
		if i.RoutingPlan != nil || i.ExpectedGenerationDigest != "" || i.WorktreeRef != "" || i.OriginSessionID != "" || !routingDigestPattern.MatchString(deliveryID) || !validOpaqueRunID(runID) {
			return errors.New("batuta: delivery routing operation requires delivery_id and delivery_run_id")
		}
	default:
		return errors.New("batuta: unsupported routing operation")
	}
	return nil
}

func validOpaqueRunID(value string) bool {
	return value != "" && len(value) <= 256 && !strings.ContainsAny(value, "\x00\r\n/\\")
}

func routingPlanInputSchema() map[string]any {
	return objectSchema([]string{"slug", "proposals", "fit"}, map[string]any{
		"slug":      map[string]any{"type": "string", "pattern": "^[a-z0-9]+(?:-[a-z0-9]+)*$"},
		"proposals": map[string]any{"type": "array", "maxItems": 10000, "items": classificationProposalSchema()},
		"fit":       map[string]any{"type": "array", "maxItems": 40, "items": cellFitSchema()},
	})
}

func routingApplyInputSchema() map[string]any {
	return map[string]any{"oneOf": []any{
		objectSchema([]string{"operation", "routing_plan", "expected_generation_digest"}, map[string]any{
			"operation":                  map[string]any{"enum": []string{string(RoutingOperationAlignmentStatus)}},
			"routing_plan":               routingPlanInputSchema(),
			"expected_generation_digest": sha256OutputSchema(),
		}),
		objectSchema([]string{"operation", "routing_plan", "expected_generation_digest", "origin_session_id"}, map[string]any{
			"operation":                  map[string]any{"enum": []string{string(RoutingOperationConfirmAlignment)}},
			"routing_plan":               routingPlanInputSchema(),
			"expected_generation_digest": sha256OutputSchema(),
			"origin_session_id":          opaqueRunIDSchema(),
		}),
		objectSchema([]string{"operation", "routing_plan", "expected_generation_digest"}, map[string]any{
			"operation":                  map[string]any{"enum": []string{string(RoutingOperationBootstrapRepository)}},
			"routing_plan":               routingPlanInputSchema(),
			"expected_generation_digest": sha256OutputSchema(),
		}),
		objectSchema([]string{"operation", "routing_plan", "expected_generation_digest", "worktree_ref", "origin_session_id"}, map[string]any{
			"operation":                  map[string]any{"enum": []string{string(RoutingOperationApplyMatrix)}},
			"routing_plan":               routingPlanInputSchema(),
			"expected_generation_digest": map[string]any{"type": "string", "pattern": "^sha256:[0-9a-f]{64}$"},
			"worktree_ref":               opaqueRunIDSchema(),
			"origin_session_id":          opaqueRunIDSchema(),
		}),
		objectSchema([]string{"operation", "delivery_id"}, map[string]any{
			"operation":   map[string]any{"enum": []string{string(RoutingOperationStartDelivery)}},
			"delivery_id": sha256OutputSchema(),
		}),
		objectSchema([]string{"operation", "delivery_id", "delivery_run_id"}, map[string]any{
			"operation":       map[string]any{"enum": []string{string(RoutingOperationRecoverDelivery)}},
			"delivery_id":     sha256OutputSchema(),
			"delivery_run_id": opaqueRunIDSchema(),
		}),
		objectSchema([]string{"operation", "delivery_id", "delivery_run_id"}, map[string]any{
			"operation":       map[string]any{"enum": []string{string(RoutingOperationReconcileFallbacks)}},
			"delivery_id":     sha256OutputSchema(),
			"delivery_run_id": opaqueRunIDSchema(),
		}),
	}}
}

func classificationProposalSchema() map[string]any {
	return objectSchema([]string{"task_id", "domain", "complexity", "confidence", "requirements", "evidence", "dependencies"}, map[string]any{
		"task_id":    map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
		"domain":     domainSchema(),
		"complexity": complexitySchema(),
		"confidence": map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		"requirements": map[string]any{"type": "array", "maxItems": 32, "items": objectSchema([]string{"kind", "id", "hard"}, map[string]any{
			"kind":               map[string]any{"enum": []string{"language_toolchain", "browser_tooling", "mobile_tooling", "database_access", "infrastructure_cli", "mcp", "repository_instruction", "sandbox_write", "test_command"}},
			"id":                 map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
			"hard":               map[string]any{"type": "boolean"},
			"security_sensitive": map[string]any{"type": "boolean"},
		})},
		"evidence": map[string]any{"type": "array", "maxItems": 24, "items": objectSchema([]string{"kind", "reference"}, map[string]any{
			"kind":      map[string]any{"enum": []string{"task_field", "path", "instruction", "acceptance_criterion"}},
			"reference": map[string]any{"type": "string", "minLength": 1, "maxLength": 512},
		})},
		"dependencies":       map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
		"indivisible_reason": map[string]any{"type": "string", "maxLength": 512},
	})
}

func cellFitSchema() map[string]any {
	return objectSchema([]string{"task_ids", "domain", "complexity", "candidates"}, map[string]any{
		"task_ids":   map[string]any{"type": "array", "minItems": 1, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 128}},
		"domain":     domainSchema(),
		"complexity": complexitySchema(),
		"candidates": map[string]any{"type": "array", "items": objectSchema([]string{"executor_id", "provider_id", "model_id", "score"}, map[string]any{
			"executor_id": map[string]any{"enum": []string{"compozy", "codex", "opencode", "cursor-agent"}},
			"provider_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
			"model_id":    map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
			"score":       map[string]any{"type": "number", "minimum": 0, "maximum": 1},
		})},
	})
}

func domainSchema() map[string]any {
	return map[string]any{"enum": []string{"backend", "frontend", "mobile", "data", "infra", "security", "testing", "docs", "general", "fullstack"}}
}

func complexitySchema() map[string]any {
	return map[string]any{"enum": []string{"low", "medium", "high", "critical"}}
}

func opaqueRunIDSchema() map[string]any {
	return map[string]any{"type": "string", "minLength": 1, "maxLength": 256, "pattern": "^[^/\\\\\\r\\n]+$"}
}

func routingGenerationOutputSchema() map[string]any {
	return objectSchema([]string{
		"schema_version", "policy_version", "workspace_identity_digest", "task_set_digest",
		"inventory_digest", "catalog_generation", "tasks", "rules", "cells",
		"delivery_fallback_limit", "enclosing_budget", "digest",
	}, map[string]any{
		"schema_version":            map[string]any{"type": "integer", "minimum": 1},
		"policy_version":            map[string]any{"type": "string", "minLength": 1},
		"workspace_identity_digest": sha256OutputSchema(),
		"task_set_digest":           hexDigestOutputSchema(),
		"inventory_digest":          sha256OutputSchema(),
		"catalog_generation":        sha256OutputSchema(),
		"tasks": map[string]any{
			"type": "array", "maxItems": 10000, "items": generationTaskOutputSchema(),
		},
		"rules": map[string]any{
			"type": "array", "maxItems": 40, "items": runtimeRuleOutputSchema(),
		},
		"cells": map[string]any{
			"type": "array", "maxItems": 40, "items": routingCellOutputSchema(),
		},
		"rejections": map[string]any{
			"type": "array", "maxItems": 640, "items": candidateRejectionOutputSchema(),
		},
		"delivery_fallback_limit": map[string]any{"type": "integer", "minimum": 0},
		"enclosing_budget":        loopBudgetOutputSchema(),
		"digest":                  sha256OutputSchema(),
	})
}

func routingApplyOutputSchema() map[string]any {
	return objectSchema([]string{"operation"}, map[string]any{
		"operation": map[string]any{"enum": []string{
			string(RoutingOperationAlignmentStatus),
			string(RoutingOperationConfirmAlignment),
			string(RoutingOperationBootstrapRepository),
			string(RoutingOperationApplyMatrix),
			string(RoutingOperationStartDelivery),
			string(RoutingOperationRecoverDelivery),
			string(RoutingOperationReconcileFallbacks),
		}},
		"alignment": objectSchema([]string{"state", "alignment_digest", "generation_digest", "replayed"}, map[string]any{
			"state":             map[string]any{"enum": []string{string(routing.AlignmentRequired), string(routing.AlignmentConfirmed)}},
			"alignment_digest":  sha256OutputSchema(),
			"generation_digest": sha256OutputSchema(),
			"confirmed_by":      opaqueRunIDSchema(),
			"confirmed_at":      map[string]any{"type": "string", "format": "date-time"},
			"changed_cells": map[string]any{
				"type": "array", "maxItems": 40, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
			},
			"replayed": map[string]any{"type": "boolean"},
		}),
		"repository": objectSchema([]string{"state", "committed_files"}, map[string]any{
			"state": map[string]any{"enum": []string{
				string(repository.BootstrapInitialized), string(repository.BootstrapAlreadyInitialized), string(repository.BootstrapBlocked),
			}},
			"branch":          map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
			"head_sha":        map[string]any{"type": "string", "pattern": "^(?:[0-9a-f]{40}|[0-9a-f]{64})$"},
			"commit_message":  map[string]any{"type": "string", "maxLength": 256},
			"committed_files": map[string]any{"type": "integer", "minimum": 0},
			"blocked_paths": map[string]any{
				"type": "array", "maxItems": 10000, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 16 << 20},
			},
		}),
		"matrix": objectSchema([]string{"delivery_id", "generation_digest", "created_at", "absolute_deadline", "attempt_ceiling", "token_ceiling", "rule_count"}, map[string]any{
			"delivery_id":       sha256OutputSchema(),
			"generation_digest": sha256OutputSchema(),
			"created_at":        map[string]any{"type": "string", "format": "date-time"},
			"absolute_deadline": map[string]any{"type": "string", "format": "date-time"},
			"attempt_ceiling":   map[string]any{"type": "integer", "minimum": 1},
			"token_ceiling":     map[string]any{"type": "integer", "minimum": 1},
			"rule_count":        map[string]any{"type": "integer", "minimum": 0},
		}),
		"start": objectSchema([]string{
			"delivery_id", "attempt", "operation_id", "delivery_run_id",
		}, map[string]any{
			"delivery_id":     sha256OutputSchema(),
			"attempt":         map[string]any{"type": "integer", "minimum": 1, "maximum": 4},
			"operation_id":    sha256OutputSchema(),
			"delivery_run_id": opaqueRunIDSchema(),
			"replayed":        map[string]any{"type": "boolean"},
		}),
		"reconciliation": objectSchema([]string{
			"delivery_id", "attempt", "delivery_run_id", "state", "recoverable", "attempts_used", "attempts_limit", "tokens_used", "tokens_limit", "remaining_wall_sec",
		}, map[string]any{
			"delivery_id":        sha256OutputSchema(),
			"attempt":            map[string]any{"type": "integer", "minimum": 1, "maximum": 4},
			"delivery_run_id":    opaqueRunIDSchema(),
			"state":              map[string]any{"type": "string", "minLength": 1, "maxLength": 64},
			"recoverable":        map[string]any{"type": "boolean"},
			"attempts_used":      map[string]any{"type": "integer", "minimum": 0},
			"attempts_limit":     map[string]any{"type": "integer", "minimum": 1},
			"tokens_used":        map[string]any{"type": "integer", "minimum": 0},
			"tokens_limit":       map[string]any{"type": "integer", "minimum": 1},
			"remaining_wall_sec": map[string]any{"type": "integer", "minimum": 0},
			"blocker_code":       map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
		}),
	})
}

func generationTaskOutputSchema() map[string]any {
	return objectSchema([]string{"task_id", "domain", "complexity"}, map[string]any{
		"task_id":    map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
		"domain":     domainSchema(),
		"complexity": complexitySchema(),
	})
}

func runtimeRuleOutputSchema() map[string]any {
	return objectSchema([]string{"match", "runtime"}, map[string]any{
		"match": objectSchema([]string{"type", "complexity"}, map[string]any{
			"id":         map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
			"type":       domainSchema(),
			"complexity": complexitySchema(),
		}),
		"runtime": runtimeValueOutputSchema(),
	})
}

func runtimeValueOutputSchema() map[string]any {
	return objectSchema([]string{"provider", "model", "reasoning"}, map[string]any{
		"provider":  map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
		"model":     map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
		"reasoning": map[string]any{"enum": []string{"low", "medium", "high", "xhigh"}},
		"speed":     map[string]any{"type": "string", "minLength": 1, "maxLength": 64},
	})
}

func runtimeCandidateOutputSchema() map[string]any {
	return objectSchema([]string{"executor_id", "provider_id", "model_id", "reasoning", "model_tier"}, map[string]any{
		"executor_id":    map[string]any{"enum": []string{"compozy", "codex", "opencode", "cursor-agent"}},
		"provider_id":    map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
		"model_id":       map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
		"enrichment_ids": enrichmentIDsOutputSchema(),
		"reasoning":      map[string]any{"enum": []string{"low", "medium", "high", "xhigh"}},
		"model_tier":     map[string]any{"type": "integer", "minimum": 1, "maximum": 4},
	})
}

func routingCellOutputSchema() map[string]any {
	return objectSchema([]string{"domain", "complexity", "task_ids", "selected", "fallbacks", "fallback_limit", "policy"}, map[string]any{
		"domain":     domainSchema(),
		"complexity": complexitySchema(),
		"task_ids": map[string]any{
			"type": "array", "minItems": 1, "maxItems": 10000,
			"items": map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
		},
		"selected": runtimeCandidateOutputSchema(),
		"fallbacks": map[string]any{
			"type": "array", "maxItems": 3, "items": runtimeCandidateOutputSchema(),
		},
		"fallback_limit": map[string]any{"type": "integer", "minimum": 0, "maximum": 3},
		"policy": objectSchema([]string{"reasoning", "verification_depth", "review_posture", "fallback_limit"}, map[string]any{
			"reasoning":          map[string]any{"enum": []string{"low", "medium", "high", "xhigh"}},
			"verification_depth": map[string]any{"enum": []string{"focused", "focused_and_broad", "full", "full_and_independent"}},
			"review_posture":     map[string]any{"enum": []string{"standard", "strict", "independent"}},
			"fallback_limit":     map[string]any{"type": "integer", "minimum": 0, "maximum": 3},
		}),
	})
}

func candidateRejectionOutputSchema() map[string]any {
	return objectSchema([]string{"domain", "complexity", "executor_id", "provider_id", "model_id", "code"}, map[string]any{
		"domain":         domainSchema(),
		"complexity":     complexitySchema(),
		"executor_id":    map[string]any{"enum": []string{"compozy", "codex", "opencode", "cursor-agent"}},
		"provider_id":    map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
		"model_id":       map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
		"enrichment_ids": enrichmentIDsOutputSchema(),
		"code": map[string]any{"enum": []string{
			"executor_unavailable", "credential_missing", "catalog_pair_unavailable",
			"executor_model_unproven", "model_below_floor", "hard_capability_unresolved",
		}},
	})
}

func enrichmentIDsOutputSchema() map[string]any {
	return map[string]any{
		"type": "array", "maxItems": 5, "uniqueItems": true,
		"items": map[string]any{"enum": []string{"agy", "claude", "codex", "cursor-agent", "opencode"}},
	}
}

func loopBudgetOutputSchema() map[string]any {
	return objectSchema([]string{"iteration_cap", "wall_time_seconds"}, map[string]any{
		"iteration_cap":     map[string]any{"type": "integer", "minimum": 1},
		"token_budget":      map[string]any{"type": "integer", "minimum": 1},
		"wall_time_seconds": map[string]any{"type": "integer", "minimum": 1},
	})
}

func sha256OutputSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^sha256:[0-9a-f]{64}$"}
}

func hexDigestOutputSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^[0-9a-f]{64}$"}
}
