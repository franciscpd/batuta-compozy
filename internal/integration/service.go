package integration

import (
	"context"
	"path/filepath"
	"reflect"
	"strings"
)

type Service struct {
	Git      Git
	Locker   LockedJournal
	Tracking TrackingSynchronizer
	Fault    func(string) error
}

func (s Service) Integrate(ctx context.Context, request IntegrationRequest) (IntegrationResult, error) {
	if err := ctx.Err(); err != nil {
		return IntegrationResult{}, err
	}
	if s.Git == nil || s.Locker == nil || s.Tracking == nil || !boundedServiceID(request.WorkspaceID) ||
		!sha256Digest(request.DeliveryID) || !sha256Digest(request.OperationID) || !sha256Digest(request.RequestDigest) ||
		!filepath.IsAbs(request.IntegrationRoot) || filepath.Clean(request.IntegrationRoot) != request.IntegrationRoot ||
		!gitSHA(request.StartingHeadSHA) || len(request.Candidates) == 0 || len(request.Candidates) > 4 ||
		validateCandidateSequence(request.Candidates) != nil {
		return IntegrationResult{}, ErrInvalidCandidate
	}
	exists := false
	if err := s.Locker.WithLocked(ctx, request.WorkspaceID, func(journal Journal) error {
		_, existsNow, err := journal.Load(ctx, request.WorkspaceID, request.DeliveryID, request.OperationID)
		exists = existsNow
		if err != nil {
			return err
		}
		_, err = projectTracking(ctx, journal, request)
		return err
	}); err != nil {
		return IntegrationResult{}, err
	}
	var planned *PreflightResult
	if !exists {
		preflight, err := s.Git.Preflight(ctx, PreflightRequest{
			OperationID: request.OperationID, RequestDigest: request.RequestDigest,
			IntegrationRoot: request.IntegrationRoot, StartingHeadSHA: request.StartingHeadSHA,
			Candidates: cloneCandidates(request.Candidates),
		})
		if err != nil {
			return IntegrationResult{}, err
		}
		if err := validatePreflightForRequest(preflight, request); err != nil {
			return IntegrationResult{}, err
		}
		planned = &preflight
	}
	var result IntegrationResult
	err := s.Locker.WithLocked(ctx, request.WorkspaceID, func(journal Journal) error {
		projection, projectionErr := projectTracking(ctx, journal, request)
		if projectionErr != nil {
			return projectionErr
		}
		var err error
		result, err = s.integrateLocked(ctx, journal, request, projection, planned)
		return err
	})
	return result, err
}

func (s Service) integrateLocked(
	ctx context.Context,
	journal Journal,
	request IntegrationRequest,
	projection TrackingProjection,
	planned *PreflightResult,
) (IntegrationResult, error) {
	state, exists, err := journal.Load(ctx, request.WorkspaceID, request.DeliveryID, request.OperationID)
	if err != nil {
		return IntegrationResult{}, err
	}
	if !exists {
		if planned == nil {
			return IntegrationResult{}, ErrJournalConflict
		}
		state = OperationState{
			WorkspaceID: request.WorkspaceID, DeliveryID: request.DeliveryID,
			OperationID: request.OperationID, RequestDigest: request.RequestDigest,
			IntegrationRoot: request.IntegrationRoot, Candidates: cloneCandidates(request.Candidates),
			ProjectionRevision: projection.Revision,
			SharedTracking:     cloneProjectedTracking(projection.Files),
			Preflight:          clonePreflight(*planned), AcceptedTaskIDs: []string{},
			AcceptedCommitSHAs: []string{}, ResultingHeadSHAs: []string{},
		}
		if err := journal.CompareAndSwap(ctx, nil, state); err != nil {
			return IntegrationResult{}, err
		}
		if err := s.inject("after_intent"); err != nil {
			return IntegrationResult{}, err
		}
	} else if validateOperationState(state) != nil || !stateMatchesRequest(state, request, projection) {
		return IntegrationResult{}, ErrJournalConflict
	}
	if len(state.AcceptedTaskIDs) == len(state.Preflight.AcceptedTaskIDs) {
		return s.finishTracking(ctx, journal, request, state)
	}

	reconciled, err := s.Git.Reconcile(ctx, ReconcileRequest{
		IntegrationRoot: request.IntegrationRoot, Preflight: clonePreflight(state.Preflight),
	})
	if err != nil {
		return IntegrationResult{}, err
	}
	if err := validateReconciledPrefix(reconciled, state.Preflight); err != nil {
		return IntegrationResult{}, err
	}
	if len(reconciled.AcceptedTaskIDs) < len(state.AcceptedTaskIDs) ||
		!prefixEqual(state.AcceptedTaskIDs, reconciled.AcceptedTaskIDs) ||
		!prefixEqual(state.AcceptedCommitSHAs, reconciled.AcceptedCommitSHAs) {
		return IntegrationResult{}, ErrForeignState
	}
	if len(reconciled.AcceptedTaskIDs) > len(state.AcceptedTaskIDs) {
		if len(reconciled.AcceptedTaskIDs) != len(state.AcceptedTaskIDs)+1 {
			return IntegrationResult{}, ErrForeignState
		}
		before := cloneOperationState(state)
		index := len(state.AcceptedTaskIDs)
		state.AcceptedTaskIDs = append(state.AcceptedTaskIDs, reconciled.AcceptedTaskIDs[index])
		state.AcceptedCommitSHAs = append(state.AcceptedCommitSHAs, reconciled.AcceptedCommitSHAs[index])
		state.ResultingHeadSHAs = append(state.ResultingHeadSHAs, reconciled.ResultingHeadSHA)
		if err := journal.CompareAndSwap(ctx, &before, state); err != nil {
			return IntegrationResult{}, err
		}
	}
	if len(state.AcceptedTaskIDs) == len(state.Preflight.AcceptedTaskIDs) {
		return s.finishTracking(ctx, journal, request, state)
	}

	for index := len(state.AcceptedTaskIDs); index < len(state.Preflight.AcceptedTaskIDs); index++ {
		expectedHead := state.Preflight.StartingHeadSHA
		if len(state.ResultingHeadSHAs) > 0 {
			expectedHead = state.ResultingHeadSHAs[len(state.ResultingHeadSHAs)-1]
		}
		applied, err := s.Git.Apply(ctx, ApplyRequest{
			OperationID: request.OperationID, RequestDigest: request.RequestDigest,
			IntegrationRoot: request.IntegrationRoot, ExpectedHeadSHA: expectedHead,
			TaskID:                  state.Preflight.AcceptedTaskIDs[index],
			CandidateCommitSHA:      state.Preflight.AcceptedCommitSHAs[index],
			ExpectedResultTreeSHA:   state.Preflight.AcceptedResultTreeSHAs[index],
			ExpectedResultCommitSHA: state.Preflight.AcceptedResultCommitSHAs[index],
		})
		if err != nil {
			return IntegrationResult{}, err
		}
		if len(applied.AcceptedTaskIDs) != 1 || len(applied.AcceptedCommitSHAs) != 1 ||
			applied.AcceptedTaskIDs[0] != state.Preflight.AcceptedTaskIDs[index] ||
			applied.AcceptedCommitSHAs[0] != state.Preflight.AcceptedCommitSHAs[index] ||
			applied.ResultingHeadSHA != state.Preflight.AcceptedResultCommitSHAs[index] {
			return IntegrationResult{}, ErrApplyAmbiguous
		}
		if err := s.inject("after_apply:" + applied.AcceptedTaskIDs[0]); err != nil {
			return IntegrationResult{}, err
		}
		before := cloneOperationState(state)
		state.AcceptedTaskIDs = append(state.AcceptedTaskIDs, applied.AcceptedTaskIDs[0])
		state.AcceptedCommitSHAs = append(state.AcceptedCommitSHAs, applied.AcceptedCommitSHAs[0])
		state.ResultingHeadSHAs = append(state.ResultingHeadSHAs, applied.ResultingHeadSHA)
		if err := journal.CompareAndSwap(ctx, &before, state); err != nil {
			return IntegrationResult{}, err
		}
	}

	return s.finishTracking(ctx, journal, request, state)
}

func (s Service) finishTracking(
	ctx context.Context,
	journal Journal,
	request IntegrationRequest,
	state OperationState,
) (IntegrationResult, error) {
	tracking, err := s.Tracking.Sync(ctx, TrackingSyncRequest{
		OperationID: request.OperationID, RequestDigest: request.RequestDigest,
		IntegrationRoot: request.IntegrationRoot, ExpectedHeadSHA: currentOperationHead(state),
		Candidates:         cloneCandidates(state.Candidates),
		AcceptedTaskIDs:    append([]string{}, state.AcceptedTaskIDs...),
		ProjectionRevision: state.ProjectionRevision,
		SharedTracking:     cloneProjectedTracking(state.SharedTracking),
		ExpectedDigest:     state.TrackingDigest, ExpectedMetadataCommitSHA: state.MetadataCommitSHA,
	})
	if err != nil {
		return IntegrationResult{}, err
	}
	if !sha256Digest(tracking.Digest) || tracking.MetadataCommitSHA != "" && !gitSHA(tracking.MetadataCommitSHA) {
		return IntegrationResult{}, ErrForeignState
	}
	if state.Complete {
		if state.TrackingDigest != tracking.Digest || state.MetadataCommitSHA != tracking.MetadataCommitSHA {
			return IntegrationResult{}, ErrForeignState
		}
		return integrationResult(state), nil
	}
	if err := s.inject("after_tracking"); err != nil {
		return IntegrationResult{}, err
	}
	before := cloneOperationState(state)
	state.TrackingDigest = tracking.Digest
	state.MetadataCommitSHA = tracking.MetadataCommitSHA
	state.Complete = true
	if err := journal.CompareAndSwap(ctx, &before, state); err != nil {
		return IntegrationResult{}, err
	}
	return integrationResult(state), nil
}

func (s Service) inject(point string) error {
	if s.Fault == nil {
		return nil
	}
	return s.Fault(point)
}

func validatePreflightForRequest(preflight PreflightResult, request IntegrationRequest) error {
	if err := validatePreflightResult(preflight); err != nil || preflight.OperationID != request.OperationID ||
		preflight.RequestDigest != request.RequestDigest || preflight.StartingHeadSHA != request.StartingHeadSHA ||
		len(preflight.AcceptedTaskIDs) > len(request.Candidates) {
		return ErrForeignState
	}
	for index := range preflight.AcceptedTaskIDs {
		if preflight.AcceptedTaskIDs[index] != request.Candidates[index].TaskID ||
			preflight.AcceptedCommitSHAs[index] != request.Candidates[index].CommitSHA {
			return ErrForeignState
		}
	}
	if len(preflight.AcceptedTaskIDs) < len(request.Candidates) {
		if preflight.FirstConflictTaskID != request.Candidates[len(preflight.AcceptedTaskIDs)].TaskID {
			return ErrForeignState
		}
	} else if preflight.FirstConflictTaskID != "" {
		return ErrForeignState
	}
	return nil
}

func validateReconciledPrefix(reconciled ApplyResult, preflight PreflightResult) error {
	if reconciled.StartingHeadSHA != preflight.StartingHeadSHA || !gitSHA(reconciled.ResultingHeadSHA) ||
		len(reconciled.AcceptedTaskIDs) != len(reconciled.AcceptedCommitSHAs) ||
		len(reconciled.AcceptedTaskIDs) > len(preflight.AcceptedTaskIDs) ||
		!prefixEqual(reconciled.AcceptedTaskIDs, preflight.AcceptedTaskIDs) ||
		!prefixEqual(reconciled.AcceptedCommitSHAs, preflight.AcceptedCommitSHAs) {
		return ErrForeignState
	}
	expectedHead := preflight.StartingHeadSHA
	if len(reconciled.AcceptedTaskIDs) > 0 {
		expectedHead = preflight.AcceptedResultCommitSHAs[len(reconciled.AcceptedTaskIDs)-1]
	}
	if reconciled.ResultingHeadSHA != expectedHead {
		return ErrForeignState
	}
	return nil
}

func stateMatchesRequest(state OperationState, request IntegrationRequest, projection TrackingProjection) bool {
	return state.WorkspaceID == request.WorkspaceID && state.DeliveryID == request.DeliveryID &&
		state.OperationID == request.OperationID && state.RequestDigest == request.RequestDigest &&
		state.IntegrationRoot == request.IntegrationRoot && state.Preflight.StartingHeadSHA == request.StartingHeadSHA &&
		reflect.DeepEqual(state.Candidates, request.Candidates) && state.ProjectionRevision == projection.Revision &&
		reflect.DeepEqual(state.SharedTracking, projection.Files)
}

func projectTracking(ctx context.Context, journal Journal, request IntegrationRequest) (TrackingProjection, error) {
	projection, err := journal.ProjectTracking(ctx, TrackingProjectionRequest{
		WorkspaceID: request.WorkspaceID, DeliveryID: request.DeliveryID,
		OperationID: request.OperationID, RequestDigest: request.RequestDigest,
	})
	if err != nil {
		return TrackingProjection{}, err
	}
	if !sha256Digest(projection.Revision) || projection.RequestDigest != request.RequestDigest || projection.Files == nil ||
		validateProjectedTracking(projection.Files) != nil || validateTrackingOwnership(request.Candidates, projection.Files) != nil {
		return TrackingProjection{}, ErrJournalConflict
	}
	projection.Files = cloneProjectedTracking(projection.Files)
	return projection, nil
}

func validateOperationState(state OperationState) error {
	if !boundedServiceID(state.WorkspaceID) || !sha256Digest(state.DeliveryID) || !sha256Digest(state.OperationID) ||
		!sha256Digest(state.RequestDigest) || !filepath.IsAbs(state.IntegrationRoot) ||
		filepath.Clean(state.IntegrationRoot) != state.IntegrationRoot || validateCandidateSequence(state.Candidates) != nil ||
		!sha256Digest(state.ProjectionRevision) || state.SharedTracking == nil || validateProjectedTracking(state.SharedTracking) != nil ||
		validateTrackingOwnership(state.Candidates, state.SharedTracking) != nil || validatePreflightResult(state.Preflight) != nil ||
		state.Preflight.OperationID != state.OperationID || state.Preflight.RequestDigest != state.RequestDigest ||
		len(state.AcceptedTaskIDs) != len(state.AcceptedCommitSHAs) || len(state.AcceptedTaskIDs) != len(state.ResultingHeadSHAs) ||
		len(state.AcceptedTaskIDs) > len(state.Preflight.AcceptedTaskIDs) ||
		!prefixEqual(state.AcceptedTaskIDs, state.Preflight.AcceptedTaskIDs) ||
		!prefixEqual(state.AcceptedCommitSHAs, state.Preflight.AcceptedCommitSHAs) {
		return ErrJournalConflict
	}
	for index, head := range state.ResultingHeadSHAs {
		if head != state.Preflight.AcceptedResultCommitSHAs[index] {
			return ErrJournalConflict
		}
	}
	if state.Complete {
		if len(state.AcceptedTaskIDs) != len(state.Preflight.AcceptedTaskIDs) || !sha256Digest(state.TrackingDigest) ||
			state.MetadataCommitSHA != "" && !gitSHA(state.MetadataCommitSHA) {
			return ErrJournalConflict
		}
	} else if state.TrackingDigest != "" || state.MetadataCommitSHA != "" {
		return ErrJournalConflict
	}
	return nil
}

func prefixEqual[T comparable](prefix, values []T) bool {
	if len(prefix) > len(values) {
		return false
	}
	for index := range prefix {
		if prefix[index] != values[index] {
			return false
		}
	}
	return true
}

func integrationResult(state OperationState) IntegrationResult {
	head := currentOperationHead(state)
	if state.MetadataCommitSHA != "" {
		head = state.MetadataCommitSHA
	}
	return IntegrationResult{
		OperationID: state.OperationID, RequestDigest: state.RequestDigest,
		AcceptedTaskIDs:     append([]string{}, state.AcceptedTaskIDs...),
		AcceptedCommitSHAs:  append([]string{}, state.AcceptedCommitSHAs...),
		FirstConflictTaskID: state.Preflight.FirstConflictTaskID, ResultingHeadSHA: head,
		MetadataCommitSHA: state.MetadataCommitSHA, Complete: state.Complete,
	}
}

func currentOperationHead(state OperationState) string {
	if len(state.ResultingHeadSHAs) > 0 {
		return state.ResultingHeadSHAs[len(state.ResultingHeadSHAs)-1]
	}
	return state.Preflight.StartingHeadSHA
}

func cloneOperationState(state OperationState) OperationState {
	state.Candidates = cloneCandidates(state.Candidates)
	state.SharedTracking = cloneProjectedTracking(state.SharedTracking)
	state.Preflight = clonePreflight(state.Preflight)
	state.AcceptedTaskIDs = append([]string{}, state.AcceptedTaskIDs...)
	state.AcceptedCommitSHAs = append([]string{}, state.AcceptedCommitSHAs...)
	state.ResultingHeadSHAs = append([]string{}, state.ResultingHeadSHAs...)
	return state
}

func cloneCandidates(candidates []CandidateEvidence) []CandidateEvidence {
	cloned := make([]CandidateEvidence, len(candidates))
	for index, candidate := range candidates {
		cloned[index] = candidate
		cloned[index].OwnedTrackingPaths = append([]string{}, candidate.OwnedTrackingPaths...)
		if candidate.Tracking != nil {
			cloned[index].Tracking = append([]TrackingFile{}, candidate.Tracking...)
		}
	}
	return cloned
}

func clonePreflight(preflight PreflightResult) PreflightResult {
	preflight.AcceptedTaskIDs = append([]string{}, preflight.AcceptedTaskIDs...)
	preflight.AcceptedCommitSHAs = append([]string{}, preflight.AcceptedCommitSHAs...)
	preflight.AcceptedResultTreeSHAs = append([]string{}, preflight.AcceptedResultTreeSHAs...)
	preflight.AcceptedResultCommitSHAs = append([]string{}, preflight.AcceptedResultCommitSHAs...)
	return preflight
}

func cloneProjectedTracking(files []ProjectedTrackingFile) []ProjectedTrackingFile {
	cloned := make([]ProjectedTrackingFile, len(files))
	for index, file := range files {
		cloned[index] = file
		cloned[index].Content = append([]byte(nil), file.Content...)
	}
	return cloned
}

func validateProjectedTracking(files []ProjectedTrackingFile) error {
	if len(files) > TrackingFileLimit {
		return ErrInvalidCandidate
	}
	seen := make(map[string]struct{}, len(files))
	total := 0
	for _, file := range files {
		if len(file.Path) == 0 || len(file.Path) > TrackingPathLimit ||
			!strings.HasPrefix(file.Path, ".compozy/") ||
			filepath.ToSlash(filepath.Clean(filepath.FromSlash(file.Path))) != file.Path ||
			len(file.Content) > VerificationLimit || digest(file.Content) != file.Digest {
			return ErrInvalidCandidate
		}
		if _, duplicate := seen[file.Path]; duplicate {
			return ErrInvalidCandidate
		}
		seen[file.Path] = struct{}{}
		total += len(file.Content)
		if total > TrackingBytesLimit {
			return ErrInvalidCandidate
		}
	}
	return nil
}

func validateTrackingOwnership(candidates []CandidateEvidence, shared []ProjectedTrackingFile) error {
	seen := make(map[string]struct{})
	count := 0
	for _, candidate := range candidates {
		for _, path := range candidate.OwnedTrackingPaths {
			if _, duplicate := seen[path]; duplicate {
				return ErrInvalidCandidate
			}
			seen[path] = struct{}{}
			count++
		}
	}
	for _, file := range shared {
		if _, duplicate := seen[file.Path]; duplicate {
			return ErrInvalidCandidate
		}
		seen[file.Path] = struct{}{}
		count++
	}
	if count > TrackingFileLimit {
		return ErrInvalidCandidate
	}
	return nil
}

func boundedServiceID(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && len(value) <= 256 && !strings.ContainsRune(value, '\x00')
}
