package extensionapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"reflect"

	"github.com/batuta-ai/core/integration"
	"github.com/batuta-ai/core/routing"
)

type routingIntegrationLocker struct {
	StoreForCall func() (*routing.OwnershipStore, error)
}

func (l routingIntegrationLocker) WithLocked(
	ctx context.Context,
	workspaceID string,
	action func(integration.Journal) error,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if l.StoreForCall == nil || action == nil {
		return integration.ErrJournalConflict
	}
	store, err := l.StoreForCall()
	if err != nil || store == nil {
		if err != nil {
			return err
		}
		return integration.ErrJournalConflict
	}
	return store.WithLockedJournal(workspaceID, func(tx *routing.JournalTx) error {
		if err := ctx.Err(); err != nil {
			return err
		}
		return action(&routingIntegrationJournal{tx: tx, workspaceID: workspaceID})
	})
}

type routingIntegrationJournal struct {
	tx          *routing.JournalTx
	workspaceID string
}

func (j *routingIntegrationJournal) Load(
	ctx context.Context,
	workspaceID string,
	deliveryID string,
	operationID string,
) (integration.OperationState, bool, error) {
	if err := ctx.Err(); err != nil {
		return integration.OperationState{}, false, err
	}
	if !j.validScope(workspaceID, deliveryID) || !routingDigestPattern.MatchString(operationID) {
		return integration.OperationState{}, false, integration.ErrJournalConflict
	}
	payload, exists := j.tx.Journal.IntegrationStates[operationID]
	if !exists {
		return integration.OperationState{}, false, nil
	}
	var state integration.OperationState
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&state); err != nil {
		return integration.OperationState{}, false, integration.ErrJournalConflict
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) || state.WorkspaceID != workspaceID ||
		state.DeliveryID != deliveryID || state.OperationID != operationID {
		return integration.OperationState{}, false, integration.ErrJournalConflict
	}
	return state, true, nil
}

func (j *routingIntegrationJournal) CompareAndSwap(
	ctx context.Context,
	before *integration.OperationState,
	after integration.OperationState,
) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if !j.validScope(after.WorkspaceID, after.DeliveryID) || !routingDigestPattern.MatchString(after.OperationID) {
		return integration.ErrJournalConflict
	}
	current, exists, err := j.Load(ctx, after.WorkspaceID, after.DeliveryID, after.OperationID)
	if err != nil {
		return err
	}
	if before == nil {
		if exists {
			return integration.ErrJournalConflict
		}
	} else if !exists || !reflect.DeepEqual(current, *before) {
		return integration.ErrJournalConflict
	}
	payload, err := json.Marshal(after)
	if err != nil {
		return integration.ErrJournalConflict
	}
	if j.tx.Journal.IntegrationStates == nil {
		j.tx.Journal.IntegrationStates = map[string]json.RawMessage{}
	}
	j.tx.Journal.IntegrationStates[after.OperationID] = append(json.RawMessage(nil), payload...)
	if err := j.tx.Persist(); err != nil {
		return err
	}
	return nil
}

func (j *routingIntegrationJournal) ProjectTracking(
	ctx context.Context,
	request integration.TrackingProjectionRequest,
) (integration.TrackingProjection, error) {
	if err := ctx.Err(); err != nil {
		return integration.TrackingProjection{}, err
	}
	if !j.validScope(request.WorkspaceID, request.DeliveryID) ||
		!routingDigestPattern.MatchString(request.OperationID) || !routingDigestPattern.MatchString(request.RequestDigest) ||
		len(request.AcceptedTaskIDs) != len(request.AcceptedCommitSHAs) ||
		len(request.AcceptedTaskIDs) != len(request.IntegratedCommitSHAs) {
		return integration.TrackingProjection{}, integration.ErrJournalConflict
	}
	delivery := j.tx.Journal.Deliveries[request.DeliveryID]
	accepted := make(map[string]int, len(request.AcceptedTaskIDs))
	for index, taskID := range request.AcceptedTaskIDs {
		if _, duplicate := accepted[taskID]; duplicate || !gitSHAValue(request.AcceptedCommitSHAs[index]) ||
			!gitSHAValue(request.IntegratedCommitSHAs[index]) {
			return integration.TrackingProjection{}, integration.ErrJournalConflict
		}
		task, exists := delivery.Graph.Task(taskID)
		if !exists || len(task.Attempts) == 0 {
			return integration.TrackingProjection{}, integration.ErrJournalConflict
		}
		attempt := task.Attempts[len(task.Attempts)-1]
		if attempt.CandidateCommitSHA != request.AcceptedCommitSHAs[index] ||
			(task.State != routing.GraphTaskCandidate &&
				(task.State != routing.GraphTaskIntegrated || task.IntegratedCommitSHA != request.IntegratedCommitSHAs[index])) {
			return integration.TrackingProjection{}, integration.ErrJournalConflict
		}
		accepted[taskID] = index
	}
	if state, exists, err := j.Load(ctx, request.WorkspaceID, request.DeliveryID, request.OperationID); err != nil {
		return integration.TrackingProjection{}, err
	} else if exists && (state.RequestDigest != request.RequestDigest ||
		!reflect.DeepEqual(state.Preflight.AcceptedTaskIDs, request.AcceptedTaskIDs) ||
		!reflect.DeepEqual(state.Preflight.AcceptedCommitSHAs, request.AcceptedCommitSHAs) ||
		!reflect.DeepEqual(state.Preflight.AcceptedResultCommitSHAs, request.IntegratedCommitSHAs)) {
		return integration.TrackingProjection{}, integration.ErrJournalConflict
	}
	type projectedTask struct {
		TaskID              string                 `json:"task_id"`
		State               routing.GraphTaskState `json:"state"`
		Execution           int                    `json:"execution"`
		CandidateCommitSHA  string                 `json:"candidate_commit_sha,omitempty"`
		IntegratedCommitSHA string                 `json:"integrated_commit_sha,omitempty"`
		VerificationDigest  string                 `json:"verification_digest,omitempty"`
		BlockerCode         string                 `json:"blocker_code,omitempty"`
	}
	tracking := struct {
		SchemaVersion string          `json:"schema_version"`
		DeliveryID    string          `json:"delivery_id"`
		Tasks         []projectedTask `json:"tasks"`
	}{SchemaVersion: "batuta.delivery/v1", DeliveryID: delivery.DeliveryID, Tasks: make([]projectedTask, 0, len(delivery.Graph.Tasks))}
	for _, task := range delivery.Graph.Tasks {
		projected := projectedTask{
			TaskID: task.TaskID, State: task.State, Execution: len(task.Attempts),
			IntegratedCommitSHA: task.IntegratedCommitSHA, BlockerCode: task.BlockerCode,
		}
		if len(task.Attempts) > 0 {
			attempt := task.Attempts[len(task.Attempts)-1]
			projected.CandidateCommitSHA = attempt.CandidateCommitSHA
			projected.VerificationDigest = attempt.VerificationDigest
		}
		if index, exists := accepted[task.TaskID]; exists {
			projected.State = routing.GraphTaskIntegrated
			projected.CandidateCommitSHA = request.AcceptedCommitSHAs[index]
			projected.IntegratedCommitSHA = request.IntegratedCommitSHAs[index]
			projected.BlockerCode = ""
		}
		tracking.Tasks = append(tracking.Tasks, projected)
	}
	content, err := json.Marshal(tracking)
	if err != nil {
		return integration.TrackingProjection{}, integration.ErrJournalConflict
	}
	content = append(content, '\n')
	contentDigest := sha256.Sum256(content)
	payload, err := json.Marshal(struct {
		WorkspaceID   string          `json:"workspace_id"`
		DeliveryID    string          `json:"delivery_id"`
		OperationID   string          `json:"operation_id"`
		RequestDigest string          `json:"request_digest"`
		Content       json.RawMessage `json:"content"`
	}{
		WorkspaceID: request.WorkspaceID, DeliveryID: request.DeliveryID,
		OperationID: request.OperationID, RequestDigest: request.RequestDigest, Content: content,
	})
	if err != nil {
		return integration.TrackingProjection{}, integration.ErrJournalConflict
	}
	digest := sha256.Sum256(payload)
	return integration.TrackingProjection{
		Revision: "sha256:" + hex.EncodeToString(digest[:]), RequestDigest: request.RequestDigest,
		Files: []integration.ProjectedTrackingFile{{
			Path:   ".compozy/tasks/" + delivery.Slug + "/_index.json",
			Digest: "sha256:" + hex.EncodeToString(contentDigest[:]), Content: content,
		}},
	}, nil
}

func (j *routingIntegrationJournal) validScope(workspaceID, deliveryID string) bool {
	if j == nil || j.tx == nil || j.tx.Journal == nil || workspaceID != j.workspaceID ||
		!routingDigestPattern.MatchString(deliveryID) {
		return false
	}
	delivery, exists := j.tx.Journal.Deliveries[deliveryID]
	return exists && delivery.WorkspaceID == workspaceID && delivery.DeliveryID == deliveryID && delivery.Graph != nil
}

var _ integration.LockedJournal = routingIntegrationLocker{}
var _ integration.Journal = (*routingIntegrationJournal)(nil)
