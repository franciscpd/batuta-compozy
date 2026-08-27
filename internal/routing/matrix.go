package routing

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"time"
)

type MatrixManager struct {
	Store *OwnershipStore
}

type MatrixApplyInput struct {
	WorkspaceID                string
	WorkspaceRoot              string
	WorktreeID                 string
	WorktreeRoot               string
	Slug                       string
	OriginSessionID            string
	TaskSetDigest              string
	TaskSnapshot               DeliveryTaskSnapshot
	InitialWorktreeFingerprint WorktreeFingerprint
	Generation                 RoutingGeneration
}

type MatrixApplyResult struct {
	DeliveryID       string    `json:"delivery_id"`
	GenerationDigest string    `json:"generation_digest"`
	CreatedAt        time.Time `json:"created_at"`
	AbsoluteDeadline time.Time `json:"absolute_deadline"`
	AttemptCeiling   int       `json:"attempt_ceiling"`
	TokenCeiling     int64     `json:"token_ceiling"`
	RuleCount        int       `json:"rule_count"`
}

func (m MatrixManager) Apply(ctx context.Context, input MatrixApplyInput) (MatrixApplyResult, error) {
	if err := ctx.Err(); err != nil {
		return MatrixApplyResult{}, err
	}
	if m.Store == nil || !boundedArgument(input.WorkspaceID) || !boundedArgument(input.WorktreeID) ||
		!boundedArgument(input.OriginSessionID) || !canonicalSlug.MatchString(input.Slug) ||
		!filepath.IsAbs(input.WorkspaceRoot) || filepath.Clean(input.WorkspaceRoot) != input.WorkspaceRoot ||
		!canonicalHexHash.MatchString(input.TaskSetDigest) || input.Generation.TaskSetDigest != input.TaskSetDigest ||
		len(input.Generation.Rules) == 0 || trustedWorkspaceIdentityDigest(input.WorkspaceID, input.WorkspaceRoot) != input.Generation.WorkspaceIdentityDigest {
		return MatrixApplyResult{}, ErrOwnershipUnproven
	}
	if err := validateWorktreeFingerprint(input.InitialWorktreeFingerprint); err != nil {
		return MatrixApplyResult{}, err
	}
	if err := validateDeliveryTaskSnapshot(input.TaskSnapshot, input.TaskSetDigest, input.Generation); err != nil {
		return MatrixApplyResult{}, err
	}
	recomputed, err := finalizeGeneration(input.Generation)
	if err != nil || recomputed.Digest != input.Generation.Digest {
		return MatrixApplyResult{}, ErrOwnershipUnproven
	}
	deliveryID, err := deriveDeliveryID(input)
	if err != nil {
		return MatrixApplyResult{}, err
	}
	var result MatrixApplyResult
	err = m.Store.WithLockedJournal(input.WorkspaceID, func(tx *JournalTx) error {
		if archived, exists := tx.Journal.Generations[input.Generation.Digest]; exists && !reflect.DeepEqual(archived, input.Generation) {
			return ErrOwnershipUnproven
		}
		tx.Journal.Generations[input.Generation.Digest] = input.Generation
		tx.Journal.CurrentGeneration = input.Generation.Digest
		if existing, exists := tx.Journal.Deliveries[deliveryID]; exists {
			if !deliveryMatchesMatrixInput(existing, input) {
				return ErrDeliveryConflict
			}
			result = matrixResult(existing, len(input.Generation.Rules))
			return nil
		}
		createdAt := time.Now().UTC()
		delivery := DeliveryRecord{
			DeliveryID: deliveryID, WorkspaceID: input.WorkspaceID, WorktreeID: input.WorktreeID,
			WorktreeRoot: input.WorktreeRoot, Slug: input.Slug, TaskSetDigest: input.TaskSetDigest,
			TaskSnapshot:            input.TaskSnapshot,
			RoutingGenerationDigest: input.Generation.Digest, OriginSessionID: input.OriginSessionID,
			CreatedAt: createdAt, AbsoluteDeadline: createdAt.Add(4 * time.Hour),
			AttemptCeiling: deliveryAttemptCeiling, TokenCeiling: deliveryTokenCeiling,
			InitialWorktreeFingerprint: input.InitialWorktreeFingerprint,
			State:                      DeliveryStateActive, Attempts: []DeliveryAttempt{},
		}
		tx.Journal.Deliveries[deliveryID] = delivery
		if err := tx.Persist(); err != nil {
			return err
		}
		result = matrixResult(delivery, len(input.Generation.Rules))
		return nil
	})
	if err != nil {
		return MatrixApplyResult{}, err
	}
	journal, exists, err := m.Store.Load(input.WorkspaceID)
	if err != nil || !exists {
		if err != nil {
			return MatrixApplyResult{}, err
		}
		return MatrixApplyResult{}, ErrOwnershipUnproven
	}
	archivedGeneration, generationExists := journal.Generations[input.Generation.Digest]
	archivedDelivery, deliveryExists := journal.Deliveries[deliveryID]
	if !generationExists || !deliveryExists || archivedGeneration.Digest != input.Generation.Digest || archivedDelivery.DeliveryID != deliveryID || !deliveryMatchesMatrixInput(archivedDelivery, input) {
		return MatrixApplyResult{}, ErrOwnershipUnproven
	}
	return result, nil
}

func deriveDeliveryID(input MatrixApplyInput) (string, error) {
	identity := struct {
		WorkspaceID                string              `json:"workspace_id"`
		WorktreeID                 string              `json:"worktree_id"`
		Slug                       string              `json:"slug"`
		TaskSetDigest              string              `json:"task_set_digest"`
		RoutingGenerationDigest    string              `json:"routing_generation_digest"`
		OriginSessionID            string              `json:"origin_session_id"`
		InitialWorktreeFingerprint WorktreeFingerprint `json:"initial_worktree_fingerprint"`
	}{
		WorkspaceID: input.WorkspaceID, WorktreeID: input.WorktreeID, Slug: input.Slug,
		TaskSetDigest: input.TaskSetDigest, RoutingGenerationDigest: input.Generation.Digest,
		OriginSessionID: input.OriginSessionID, InitialWorktreeFingerprint: input.InitialWorktreeFingerprint,
	}
	payload, err := json.Marshal(identity)
	if err != nil {
		return "", errors.New("routing: encode delivery identity failed")
	}
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:]), nil
}

func trustedWorkspaceIdentityDigest(workspaceID, root string) string {
	digest := sha256.Sum256([]byte(workspaceID + "\x00" + root))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func deliveryMatchesMatrixInput(delivery DeliveryRecord, input MatrixApplyInput) bool {
	return delivery.DeliveryID != "" && delivery.WorkspaceID == input.WorkspaceID && delivery.WorktreeID == input.WorktreeID &&
		delivery.WorktreeRoot == input.WorktreeRoot && delivery.Slug == input.Slug && delivery.TaskSetDigest == input.TaskSetDigest &&
		delivery.RoutingGenerationDigest == input.Generation.Digest && delivery.OriginSessionID == input.OriginSessionID &&
		reflect.DeepEqual(delivery.TaskSnapshot, input.TaskSnapshot) &&
		reflect.DeepEqual(delivery.InitialWorktreeFingerprint, input.InitialWorktreeFingerprint) &&
		delivery.AttemptCeiling == deliveryAttemptCeiling && delivery.TokenCeiling == deliveryTokenCeiling
}

func matrixResult(delivery DeliveryRecord, ruleCount int) MatrixApplyResult {
	return MatrixApplyResult{
		DeliveryID: delivery.DeliveryID, GenerationDigest: delivery.RoutingGenerationDigest,
		CreatedAt: delivery.CreatedAt, AbsoluteDeadline: delivery.AbsoluteDeadline,
		AttemptCeiling: delivery.AttemptCeiling, TokenCeiling: delivery.TokenCeiling, RuleCount: ruleCount,
	}
}
