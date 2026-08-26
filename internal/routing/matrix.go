package routing

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
)

type LoopConfigBoundary interface {
	Read(context.Context, string, string) (LoopConfigSnapshot, error)
	Write(context.Context, string, string, int64, LoopConfigDocument) (LoopConfigSnapshot, error)
}

type MatrixManager struct {
	Config LoopConfigBoundary
	Store  *OwnershipStore
}

type MatrixApplyResult struct {
	GenerationDigest string `json:"generation_digest"`
	ConfigRevision   int64  `json:"config_revision"`
	RuleCount        int    `json:"rule_count"`
}

func (m MatrixManager) Apply(ctx context.Context, workspaceID, loopName string, generation RoutingGeneration) (MatrixApplyResult, error) {
	if m.Config == nil || m.Store == nil || generation.Digest == "" || generation.TaskSetDigest == "" || len(generation.Rules) == 0 {
		return MatrixApplyResult{}, errors.New("routing: complete matrix generation is required")
	}
	snapshot, err := m.Config.Read(ctx, workspaceID, loopName)
	if err != nil {
		return MatrixApplyResult{}, err
	}
	journal, exists, err := m.Store.Load(workspaceID)
	if err != nil {
		return MatrixApplyResult{}, err
	}
	if !exists {
		journal = RoutingJournal{
			SchemaVersion: journalSchemaVersion, Generations: map[string]RoutingGeneration{},
			DeliveryBindings: map[string]string{},
		}
	}
	previous := make([]RuntimeRule, 0, len(journal.OwnedRules))
	for _, owned := range journal.OwnedRules {
		previous = append(previous, owned.Rule)
	}
	merged, err := MergeRuntimeRules(snapshot.Config, previous, generation.Rules)
	if err != nil {
		return MatrixApplyResult{}, ErrOwnershipUnproven
	}
	written, err := m.Config.Write(ctx, workspaceID, loopName, snapshot.ConfigRevision, merged)
	if err != nil {
		return MatrixApplyResult{}, err
	}
	if written.ConfigRevision <= snapshot.ConfigRevision || !sameConfig(written.Config, merged) {
		return MatrixApplyResult{}, errors.New("routing: loop config write-back mismatch")
	}
	readBack, err := m.Config.Read(ctx, workspaceID, loopName)
	if err != nil {
		return MatrixApplyResult{}, err
	}
	if readBack.ConfigRevision != written.ConfigRevision || !sameConfig(readBack.Config, merged) {
		return MatrixApplyResult{}, errors.New("routing: loop config read-back mismatch")
	}
	owned := make([]OwnedRuntimeRule, 0, len(generation.Rules))
	for _, rule := range generation.Rules {
		fingerprint, err := ruleFingerprint(rule)
		if err != nil {
			return MatrixApplyResult{}, err
		}
		owned = append(owned, OwnedRuntimeRule{Rule: rule, Fingerprint: fingerprint})
	}
	journal.CurrentGeneration = generation.Digest
	journal.Generations[generation.Digest] = generation
	journal.OwnedRules = owned
	if err := m.Store.Save(workspaceID, journal); err != nil {
		return MatrixApplyResult{}, err
	}
	return MatrixApplyResult{GenerationDigest: generation.Digest, ConfigRevision: readBack.ConfigRevision, RuleCount: len(generation.Rules)}, nil
}

func sameConfig(left, right LoopConfigDocument) bool {
	leftJSON, leftErr := json.Marshal(left)
	rightJSON, rightErr := json.Marshal(right)
	return leftErr == nil && rightErr == nil && bytes.Equal(leftJSON, rightJSON)
}
