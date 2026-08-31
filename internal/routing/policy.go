package routing

import "strings"

type ModelTier int

const (
	ModelTierUnknown ModelTier = iota
	ModelTierEconomy
	ModelTierStandard
	ModelTierAdvanced
	ModelTierFrontier
)

type SelectionPolicy struct {
	Version    string               `json:"version"`
	ModelTiers map[string]ModelTier `json:"model_tiers"`
}

type ComplexityPolicySnapshot struct {
	Reasoning         string `json:"reasoning"`
	VerificationDepth string `json:"verification_depth"`
	ReviewPosture     string `json:"review_posture"`
	FallbackLimit     int    `json:"fallback_limit"`
}

func DefaultSelectionPolicy() SelectionPolicy {
	return SelectionPolicy{
		Version: "2026-08-31.v6",
		ModelTiers: map[string]ModelTier{
			ModelKey("claude", "claude-fable-5[1m]"):                                               ModelTierFrontier,
			ModelKey("claude", "claude-opus-5"):                                                    ModelTierFrontier,
			ModelKey("claude", "opus[1m]"):                                                         ModelTierFrontier,
			ModelKey("claude", "sonnet"):                                                           ModelTierAdvanced,
			ModelKey("cursor", "claude-fable-5"):                                                   ModelTierFrontier,
			ModelKey("cursor", "claude-opus-5"):                                                    ModelTierFrontier,
			ModelKey("cursor", "claude-sonnet-5"):                                                  ModelTierAdvanced,
			ModelKey("cursor", "claude-fable-5[thinking=true,context=300k,effort=high]"):           ModelTierFrontier,
			ModelKey("cursor", "claude-opus-5[thinking=true,context=300k,effort=high,fast=false]"): ModelTierFrontier,
			ModelKey("cursor", "claude-sonnet-5[thinking=true,context=300k,effort=high]"):          ModelTierAdvanced,
			ModelKey("cursor", "gpt-5.6-luna"):                                                     ModelTierAdvanced,
			ModelKey("cursor", "gpt-5.6-sol"):                                                      ModelTierFrontier,
			ModelKey("cursor", "gpt-5.6-terra"):                                                    ModelTierAdvanced,
			ModelKey("cursor", "gpt-5.6-luna[context=272k,reasoning=medium,fast=false]"):           ModelTierAdvanced,
			ModelKey("cursor", "gpt-5.6-sol[context=272k,reasoning=medium,fast=false]"):            ModelTierFrontier,
			ModelKey("cursor", "gpt-5.6-terra[context=272k,reasoning=medium,fast=false]"):          ModelTierAdvanced,
			ModelKey("cursor", "grok-4.6"):                                                         ModelTierFrontier,
			ModelKey("cursor", "grok-4.6[effort=high,fast=true]"):                                  ModelTierFrontier,
			ModelKey("codex", "gpt-5.6-sol"):                                                       ModelTierFrontier,
			ModelKey("codex", "gpt-5.6-terra"):                                                     ModelTierAdvanced,
			ModelKey("codex", "gpt-5.6-luna"):                                                      ModelTierAdvanced,
			ModelKey("opencode", "openai/gpt-5.6-terra"):                                           ModelTierAdvanced,
			ModelKey("opencode", "anthropic/claude-opus-5"):                                        ModelTierFrontier,
		},
	}
}

func ModelKey(providerID, modelID string) string {
	return strings.TrimSpace(providerID) + "\x00" + strings.TrimSpace(modelID)
}

func (p SelectionPolicy) modelTier(providerID, modelID string) ModelTier {
	return p.ModelTiers[ModelKey(providerID, modelID)]
}

func modelFloor(complexity Complexity) ModelTier {
	switch complexity {
	case ComplexityLow:
		return ModelTierStandard
	case ComplexityMedium:
		return ModelTierAdvanced
	case ComplexityHigh, ComplexityCritical:
		return ModelTierFrontier
	default:
		return ModelTierUnknown
	}
}

func fallbackLimit(complexity Complexity) int {
	switch complexity {
	case ComplexityLow:
		return 1
	case ComplexityMedium:
		return 2
	case ComplexityHigh, ComplexityCritical:
		return 3
	default:
		return 0
	}
}

func reasoningFor(complexity Complexity) string {
	switch complexity {
	case ComplexityLow:
		return "low"
	case ComplexityMedium:
		return "medium"
	case ComplexityHigh:
		return "high"
	case ComplexityCritical:
		return "xhigh"
	default:
		return ""
	}
}

func complexityPolicy(complexity Complexity) ComplexityPolicySnapshot {
	policy := ComplexityPolicySnapshot{
		Reasoning: reasoningFor(complexity), FallbackLimit: fallbackLimit(complexity),
	}
	switch complexity {
	case ComplexityLow:
		policy.VerificationDepth, policy.ReviewPosture = "focused", "standard"
	case ComplexityMedium:
		policy.VerificationDepth, policy.ReviewPosture = "focused_and_broad", "standard"
	case ComplexityHigh:
		policy.VerificationDepth, policy.ReviewPosture = "full", "strict"
	case ComplexityCritical:
		policy.VerificationDepth, policy.ReviewPosture = "full_and_independent", "independent"
	}
	return policy
}
