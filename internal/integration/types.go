package integration

import (
	"context"
	"errors"
)

const (
	VerificationLimit  = 64 * 1024
	GitStdoutLimit     = 16 * 1024 * 1024
	GitStderrLimit     = 64 * 1024
	TrackingFileLimit  = 64
	TrackingBytesLimit = 4 * 1024 * 1024
	TrackingPathLimit  = 1024
)

var (
	ErrInvalidCandidate = errors.New("integration: invalid candidate")
	ErrForeignState     = errors.New("integration: foreign Git state")
	ErrApplyAmbiguous   = errors.New("integration: Git apply outcome is ambiguous")
	ErrPreflightFailed  = errors.New("integration: preflight operation failed")
	ErrJournalConflict  = errors.New("integration: journal operation conflict")
)

type Git interface {
	Candidate(context.Context, CandidateRequest) (CandidateEvidence, error)
	Preflight(context.Context, PreflightRequest) (PreflightResult, error)
	Apply(context.Context, ApplyRequest) (ApplyResult, error)
	Reconcile(context.Context, ReconcileRequest) (ApplyResult, error)
}

type CandidateRequest struct {
	TaskID               string   `json:"task_id"`
	Slug                 string   `json:"slug"`
	WorktreeRoot         string   `json:"worktree_root"`
	RepositoryRoot       string   `json:"repository_root"`
	ExpectedBranch       string   `json:"expected_branch"`
	BaseSHA              string   `json:"base_sha"`
	Verification         []byte   `json:"verification"`
	VerificationDigest   string   `json:"verification_digest"`
	AllowedTrackingPaths []string `json:"allowed_tracking_paths"`
}

type TrackingFile struct {
	Path    string `json:"path"`
	Digest  string `json:"digest"`
	Ignored bool   `json:"ignored,omitempty"`
}

type CandidateEvidence struct {
	TaskID             string         `json:"task_id"`
	Slug               string         `json:"slug"`
	WorktreeRoot       string         `json:"worktree_root"`
	RepositoryIdentity string         `json:"repository_identity"`
	Branch             string         `json:"branch"`
	BaseSHA            string         `json:"base_sha"`
	CommitSHA          string         `json:"commit_sha"`
	TreeSHA            string         `json:"tree_sha"`
	VerificationDigest string         `json:"verification_digest"`
	OwnedTrackingPaths []string       `json:"owned_tracking_paths"`
	Tracking           []TrackingFile `json:"tracking"`
}

type PreflightRequest struct {
	OperationID     string              `json:"operation_id"`
	RequestDigest   string              `json:"request_digest"`
	IntegrationRoot string              `json:"integration_root"`
	StartingHeadSHA string              `json:"starting_head_sha"`
	Candidates      []CandidateEvidence `json:"candidates"`
}

type PreflightResult struct {
	OperationID              string   `json:"operation_id"`
	RequestDigest            string   `json:"request_digest"`
	StartingHeadSHA          string   `json:"starting_head_sha"`
	AcceptedTaskIDs          []string `json:"accepted_task_ids"`
	AcceptedCommitSHAs       []string `json:"accepted_commit_shas"`
	AcceptedResultTreeSHAs   []string `json:"accepted_result_tree_shas"`
	AcceptedResultCommitSHAs []string `json:"accepted_result_commit_shas"`
	FirstConflictTaskID      string   `json:"first_conflict_task_id,omitempty"`
	ConflictEvidenceDigest   string   `json:"conflict_evidence_digest,omitempty"`
	ResultingHeadSHA         string   `json:"resulting_head_sha"`
}

type ApplyRequest struct {
	OperationID             string `json:"operation_id"`
	RequestDigest           string `json:"request_digest"`
	IntegrationRoot         string `json:"integration_root"`
	ExpectedHeadSHA         string `json:"expected_head_sha"`
	TaskID                  string `json:"task_id"`
	CandidateCommitSHA      string `json:"candidate_commit_sha"`
	ExpectedResultTreeSHA   string `json:"expected_result_tree_sha"`
	ExpectedResultCommitSHA string `json:"expected_result_commit_sha"`
}

type ApplyResult struct {
	StartingHeadSHA    string   `json:"starting_head_sha"`
	AcceptedTaskIDs    []string `json:"accepted_task_ids"`
	AcceptedCommitSHAs []string `json:"accepted_commit_shas"`
	ResultingHeadSHA   string   `json:"resulting_head_sha"`
}

type ReconcileRequest struct {
	IntegrationRoot string          `json:"integration_root"`
	Preflight       PreflightResult `json:"preflight"`
}

type IntegrationRequest struct {
	WorkspaceID     string              `json:"workspace_id"`
	DeliveryID      string              `json:"delivery_id"`
	OperationID     string              `json:"operation_id"`
	RequestDigest   string              `json:"request_digest"`
	IntegrationRoot string              `json:"integration_root"`
	StartingHeadSHA string              `json:"starting_head_sha"`
	Candidates      []CandidateEvidence `json:"candidates"`
}

type OperationState struct {
	WorkspaceID        string                  `json:"workspace_id"`
	DeliveryID         string                  `json:"delivery_id"`
	OperationID        string                  `json:"operation_id"`
	RequestDigest      string                  `json:"request_digest"`
	IntegrationRoot    string                  `json:"integration_root"`
	Candidates         []CandidateEvidence     `json:"candidates"`
	ProjectionRevision string                  `json:"projection_revision"`
	SharedTracking     []ProjectedTrackingFile `json:"shared_tracking"`
	Preflight          PreflightResult         `json:"preflight"`
	AcceptedTaskIDs    []string                `json:"accepted_task_ids"`
	AcceptedCommitSHAs []string                `json:"accepted_commit_shas"`
	ResultingHeadSHAs  []string                `json:"resulting_head_shas"`
	TrackingDigest     string                  `json:"tracking_digest,omitempty"`
	MetadataCommitSHA  string                  `json:"metadata_commit_sha,omitempty"`
	Complete           bool                    `json:"complete"`
}

type IntegrationResult struct {
	OperationID            string   `json:"operation_id"`
	RequestDigest          string   `json:"request_digest"`
	AcceptedTaskIDs        []string `json:"accepted_task_ids"`
	AcceptedCommitSHAs     []string `json:"accepted_commit_shas"`
	IntegratedCommitSHAs   []string `json:"integrated_commit_shas"`
	FirstConflictTaskID    string   `json:"first_conflict_task_id,omitempty"`
	ConflictEvidenceDigest string   `json:"conflict_evidence_digest,omitempty"`
	ResultingHeadSHA       string   `json:"resulting_head_sha"`
	MetadataCommitSHA      string   `json:"metadata_commit_sha,omitempty"`
	Complete               bool     `json:"complete"`
}

type Journal interface {
	Load(context.Context, string, string, string) (OperationState, bool, error)
	CompareAndSwap(context.Context, *OperationState, OperationState) error
	ProjectTracking(context.Context, TrackingProjectionRequest) (TrackingProjection, error)
}

type LockedJournal interface {
	WithLocked(context.Context, string, func(Journal) error) error
}

type ProjectedTrackingFile struct {
	Path    string `json:"path"`
	Digest  string `json:"digest"`
	Content []byte `json:"content"`
}

type TrackingProjectionRequest struct {
	WorkspaceID          string   `json:"workspace_id"`
	DeliveryID           string   `json:"delivery_id"`
	OperationID          string   `json:"operation_id"`
	RequestDigest        string   `json:"request_digest"`
	AcceptedTaskIDs      []string `json:"accepted_task_ids"`
	AcceptedCommitSHAs   []string `json:"accepted_commit_shas"`
	IntegratedCommitSHAs []string `json:"integrated_commit_shas"`
}

type TrackingProjection struct {
	Revision      string                  `json:"revision"`
	RequestDigest string                  `json:"request_digest"`
	Files         []ProjectedTrackingFile `json:"files"`
}

type TrackingSyncRequest struct {
	OperationID               string                  `json:"operation_id"`
	RequestDigest             string                  `json:"request_digest"`
	IntegrationRoot           string                  `json:"integration_root"`
	ExpectedHeadSHA           string                  `json:"expected_head_sha"`
	Candidates                []CandidateEvidence     `json:"candidates"`
	AcceptedTaskIDs           []string                `json:"accepted_task_ids"`
	ProjectionRevision        string                  `json:"projection_revision"`
	SharedTracking            []ProjectedTrackingFile `json:"shared_tracking"`
	ExpectedDigest            string                  `json:"expected_digest,omitempty"`
	ExpectedMetadataCommitSHA string                  `json:"expected_metadata_commit_sha,omitempty"`
}

type TrackingSyncResult struct {
	Digest            string `json:"digest"`
	MetadataCommitSHA string `json:"metadata_commit_sha,omitempty"`
}

type TrackingSynchronizer interface {
	Sync(context.Context, TrackingSyncRequest) (TrackingSyncResult, error)
}
