package integration

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
)

func TestServicePersistsIntentBeforeMutationAndReplaysCompleteResult(t *testing.T) {
	t.Parallel()

	request := integrationRequestFixture()
	journal := &memoryIntegrationJournal{}
	git := newFakeIntegrationGit(request)
	tracking := &fakeTrackingSynchronizer{}
	faulted := false
	service := Service{Git: git, Locker: &memoryLockedJournal{journal: journal}, Tracking: tracking, Fault: func(point string) error {
		if point == "after_intent" && !faulted {
			faulted = true
			return errors.New("injected after intent")
		}
		return nil
	}}
	if _, err := service.Integrate(context.Background(), request); err == nil {
		t.Fatal("Integrate(first) error = nil, want injected failure")
	}
	state, exists, err := journal.Load(context.Background(), request.WorkspaceID, request.DeliveryID, request.OperationID)
	if err != nil || !exists || len(state.AcceptedTaskIDs) != 0 || git.applyCalls != 0 {
		t.Fatalf("durable intent = %#v, exists %v, error %v, apply calls %d", state, exists, err, git.applyCalls)
	}

	result, err := service.Integrate(context.Background(), request)
	if err != nil || !result.Complete || !reflect.DeepEqual(result.AcceptedTaskIDs, []string{"task_01", "task_02"}) ||
		git.applyCalls != 2 || tracking.calls != 1 {
		t.Fatalf("Integrate(replay) = %#v, error %v, apply %d, tracking %d", result, err, git.applyCalls, tracking.calls)
	}
	applyCalls, preflightCalls, trackingCalls := git.applyCalls, git.preflightCalls, tracking.calls
	replayed, err := service.Integrate(context.Background(), request)
	if err != nil || !reflect.DeepEqual(replayed, result) || git.applyCalls != applyCalls ||
		git.preflightCalls != preflightCalls || tracking.calls != trackingCalls+1 {
		t.Fatalf("Integrate(complete replay) = %#v, error %v; calls apply=%d preflight=%d tracking=%d", replayed, err, git.applyCalls, git.preflightCalls, tracking.calls)
	}
}

func TestServiceReconcilesCrashAfterApplyWithoutDuplicateMutation(t *testing.T) {
	t.Parallel()

	request := integrationRequestFixture()
	journal := &memoryIntegrationJournal{}
	git := newFakeIntegrationGit(request)
	faulted := false
	service := Service{Git: git, Locker: &memoryLockedJournal{journal: journal}, Tracking: &fakeTrackingSynchronizer{}, Fault: func(point string) error {
		if point == "after_apply:task_01" && !faulted {
			faulted = true
			return errors.New("injected after apply")
		}
		return nil
	}}
	if _, err := service.Integrate(context.Background(), request); err == nil {
		t.Fatal("Integrate(first) error = nil")
	}
	state, _, _ := journal.Load(context.Background(), request.WorkspaceID, request.DeliveryID, request.OperationID)
	if git.applied != 1 || len(state.AcceptedTaskIDs) != 0 {
		t.Fatalf("after crash applied=%d state=%#v", git.applied, state)
	}
	result, err := service.Integrate(context.Background(), request)
	if err != nil || !result.Complete || git.applyCalls != 2 || git.applied != 2 {
		t.Fatalf("Integrate(reconcile) = %#v, error %v, apply calls %d applied %d", result, err, git.applyCalls, git.applied)
	}
}

func TestServiceReconcilesCrashAfterTrackingWithoutDuplicateMetadata(t *testing.T) {
	t.Parallel()

	request := integrationRequestFixture()
	journal := &memoryIntegrationJournal{}
	git := newFakeIntegrationGit(request)
	tracking := &fakeTrackingSynchronizer{}
	faulted := false
	service := Service{Git: git, Locker: &memoryLockedJournal{journal: journal}, Tracking: tracking, Fault: func(point string) error {
		if point == "after_tracking" && !faulted {
			faulted = true
			return errors.New("injected after tracking")
		}
		return nil
	}}
	if _, err := service.Integrate(context.Background(), request); err == nil {
		t.Fatal("Integrate(first) error = nil")
	}
	state, _, _ := journal.Load(context.Background(), request.WorkspaceID, request.DeliveryID, request.OperationID)
	if len(state.AcceptedTaskIDs) != 2 || state.Complete || tracking.calls != 1 {
		t.Fatalf("after tracking crash state=%#v calls=%d", state, tracking.calls)
	}
	result, err := service.Integrate(context.Background(), request)
	if err != nil || !result.Complete || git.applyCalls != 2 || tracking.calls != 2 {
		t.Fatalf("Integrate(replay) = %#v, error %v, apply calls %d, tracking calls %d", result, err, git.applyCalls, tracking.calls)
	}
}

func TestServiceRejectsOperationIdentityReuseAndForeignReconciliation(t *testing.T) {
	t.Parallel()

	request := integrationRequestFixture()
	journal := &memoryIntegrationJournal{}
	git := newFakeIntegrationGit(request)
	service := Service{Git: git, Locker: &memoryLockedJournal{journal: journal}, Tracking: &fakeTrackingSynchronizer{}}
	if _, err := service.Integrate(context.Background(), request); err != nil {
		t.Fatalf("Integrate() error = %v", err)
	}
	drifted := request
	drifted.RequestDigest = integrationDigest([]byte("different request"))
	if _, err := service.Integrate(context.Background(), drifted); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("Integrate(drift) error = %v, want ErrJournalConflict", err)
	}
	git.foreign = true
	state, _, _ := journal.Load(context.Background(), request.WorkspaceID, request.DeliveryID, request.OperationID)
	state.Complete = false
	state.AcceptedTaskIDs = state.AcceptedTaskIDs[:1]
	state.AcceptedCommitSHAs = state.AcceptedCommitSHAs[:1]
	state.ResultingHeadSHAs = state.ResultingHeadSHAs[:1]
	state.TrackingDigest = ""
	state.MetadataCommitSHA = ""
	journal.force(state)
	if _, err := service.Integrate(context.Background(), request); !errors.Is(err, ErrForeignState) {
		t.Fatalf("Integrate(foreign) error = %v, want ErrForeignState", err)
	}
}

func TestServiceSerializesCompareBeforeMutateThroughLockedJournal(t *testing.T) {
	t.Parallel()

	request := integrationRequestFixture()
	journal := &memoryIntegrationJournal{}
	locker := &memoryLockedJournal{journal: journal}
	git := newFakeIntegrationGit(request)
	git.locked = locker.active.Load
	tracking := &fakeTrackingSynchronizer{}
	service := Service{Git: git, Locker: locker, Tracking: tracking}
	results := make(chan IntegrationResult, 2)
	errorsChannel := make(chan error, 2)
	var start sync.WaitGroup
	start.Add(1)
	for range 2 {
		go func() {
			start.Wait()
			result, err := service.Integrate(context.Background(), request)
			results <- result
			errorsChannel <- err
		}()
	}
	start.Done()
	first, second := <-results, <-results
	firstErr, secondErr := <-errorsChannel, <-errorsChannel
	if firstErr != nil || secondErr != nil || !reflect.DeepEqual(first, second) || git.applyCalls != 2 {
		t.Fatalf("concurrent results = %#v / %#v, errors %v / %v, apply calls %d", first, second, firstErr, secondErr, git.applyCalls)
	}
}

func TestServiceDerivesTrackingProjectionFromCurrentLockedJournal(t *testing.T) {
	t.Parallel()

	request := integrationRequestFixture()
	content := []byte(`{"tasks":["task_01","task_02"]}` + "\n")
	projection := []ProjectedTrackingFile{{
		Path: ".compozy/tasks/demo/_index.json", Digest: integrationDigest(content), Content: content,
	}}
	journal := &memoryIntegrationJournal{
		projection: projection, projectionRevision: integrationDigest([]byte("current-projection")),
	}
	locker := &memoryLockedJournal{journal: journal}
	journal.locked = locker.active.Load
	tracking := &fakeTrackingSynchronizer{}
	service := Service{Git: newFakeIntegrationGit(request), Locker: locker, Tracking: tracking}

	result, err := service.Integrate(context.Background(), request)
	if err != nil || !result.Complete || journal.projectionCalls < 2 ||
		tracking.last.ProjectionRevision != journal.projectionRevision ||
		!reflect.DeepEqual(tracking.last.SharedTracking, projection) {
		t.Fatalf("Integrate() = %#v, error %v; projection calls %d, tracking %#v", result, err, journal.projectionCalls, tracking.last)
	}
	journal.mu.Lock()
	journal.projectionRevision = integrationDigest([]byte("stale-projection"))
	journal.projection = []ProjectedTrackingFile{{
		Path: ".compozy/tasks/demo/_index.json", Digest: integrationDigest([]byte("stale\n")), Content: []byte("stale\n"),
	}}
	journal.mu.Unlock()
	if _, err := service.Integrate(context.Background(), request); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("Integrate(stale projection) error = %v, want ErrJournalConflict", err)
	}
}

func TestServiceRejectsTrackingProjectionForDifferentRequestRevision(t *testing.T) {
	t.Parallel()

	request := integrationRequestFixture()
	journal := &memoryIntegrationJournal{
		projectionRevision:      integrationDigest([]byte("current-projection")),
		projectionRequestDigest: integrationDigest([]byte("different-request")),
	}
	git := newFakeIntegrationGit(request)
	service := Service{
		Git: git, Locker: &memoryLockedJournal{journal: journal}, Tracking: &fakeTrackingSynchronizer{},
	}
	if _, err := service.Integrate(context.Background(), request); !errors.Is(err, ErrJournalConflict) {
		t.Fatalf("Integrate() error = %v, want ErrJournalConflict", err)
	}
	if git.preflightCalls != 0 || git.applyCalls != 0 {
		t.Fatalf("Git calls after stale projection: preflight=%d apply=%d", git.preflightCalls, git.applyCalls)
	}
}

func integrationRequestFixture() IntegrationRequest {
	candidates := []CandidateEvidence{
		{TaskID: "task_01", Slug: "demo", WorktreeRoot: "/worktrees/task-01", RepositoryIdentity: integrationDigest([]byte("repo")), Branch: "batuta/task/task-01", BaseSHA: strings.Repeat("a", 40), CommitSHA: strings.Repeat("b", 40), TreeSHA: strings.Repeat("c", 40), VerificationDigest: integrationDigest([]byte("verification-1")), OwnedTrackingPaths: []string{}},
		{TaskID: "task_02", Slug: "demo", WorktreeRoot: "/worktrees/task-02", RepositoryIdentity: integrationDigest([]byte("repo")), Branch: "batuta/task/task-02", BaseSHA: strings.Repeat("a", 40), CommitSHA: strings.Repeat("d", 40), TreeSHA: strings.Repeat("e", 40), VerificationDigest: integrationDigest([]byte("verification-2")), OwnedTrackingPaths: []string{}},
	}
	return IntegrationRequest{
		WorkspaceID: "workspace-demo", DeliveryID: integrationDigest([]byte("delivery")),
		OperationID: integrationDigest([]byte("operation")), RequestDigest: integrationDigest([]byte("request")),
		IntegrationRoot: "/integration", StartingHeadSHA: strings.Repeat("a", 40), Candidates: candidates,
	}
}

type fakeIntegrationGit struct {
	mu             sync.Mutex
	request        IntegrationRequest
	preflightCalls int
	applyCalls     int
	reconcileCalls int
	applied        int
	foreign        bool
	locked         func() bool
}

func newFakeIntegrationGit(request IntegrationRequest) *fakeIntegrationGit {
	return &fakeIntegrationGit{request: request}
}

func (g *fakeIntegrationGit) Candidate(context.Context, CandidateRequest) (CandidateEvidence, error) {
	return CandidateEvidence{}, errors.New("unexpected Candidate call")
}

func (g *fakeIntegrationGit) Preflight(_ context.Context, request PreflightRequest) (PreflightResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.preflightCalls++
	result := PreflightResult{OperationID: request.OperationID, RequestDigest: request.RequestDigest,
		StartingHeadSHA: request.StartingHeadSHA, ResultingHeadSHA: request.StartingHeadSHA}
	for index, candidate := range request.Candidates {
		result.AcceptedTaskIDs = append(result.AcceptedTaskIDs, candidate.TaskID)
		result.AcceptedCommitSHAs = append(result.AcceptedCommitSHAs, candidate.CommitSHA)
		result.AcceptedResultTreeSHAs = append(result.AcceptedResultTreeSHAs, strings.Repeat(string(rune('1'+index)), 40))
		result.AcceptedResultCommitSHAs = append(result.AcceptedResultCommitSHAs, strings.Repeat(string(rune('5'+index)), 40))
		result.ResultingHeadSHA = result.AcceptedResultCommitSHAs[index]
	}
	return result, nil
}

func (g *fakeIntegrationGit) Apply(_ context.Context, request ApplyRequest) (ApplyResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.locked != nil && !g.locked() {
		return ApplyResult{}, errors.New("apply outside locked journal")
	}
	g.applyCalls++
	if g.foreign || g.applied >= len(g.request.Candidates) || g.request.Candidates[g.applied].TaskID != request.TaskID {
		return ApplyResult{}, ErrForeignState
	}
	g.applied++
	return ApplyResult{StartingHeadSHA: request.ExpectedHeadSHA, AcceptedTaskIDs: []string{request.TaskID},
		AcceptedCommitSHAs: []string{request.CandidateCommitSHA}, ResultingHeadSHA: request.ExpectedResultCommitSHA}, nil
}

func (g *fakeIntegrationGit) Reconcile(_ context.Context, request ReconcileRequest) (ApplyResult, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if g.locked != nil && !g.locked() {
		return ApplyResult{}, errors.New("reconcile outside locked journal")
	}
	g.reconcileCalls++
	if g.foreign {
		return ApplyResult{}, ErrForeignState
	}
	result := ApplyResult{StartingHeadSHA: request.Preflight.StartingHeadSHA, ResultingHeadSHA: request.Preflight.StartingHeadSHA}
	if g.applied > 0 {
		result.AcceptedTaskIDs = append([]string(nil), request.Preflight.AcceptedTaskIDs[:g.applied]...)
		result.AcceptedCommitSHAs = append([]string(nil), request.Preflight.AcceptedCommitSHAs[:g.applied]...)
		result.ResultingHeadSHA = request.Preflight.AcceptedResultCommitSHAs[g.applied-1]
	}
	return result, nil
}

type memoryIntegrationJournal struct {
	mu                      sync.Mutex
	state                   *OperationState
	projection              []ProjectedTrackingFile
	projectionRevision      string
	projectionRequestDigest string
	projectionCalls         int
	locked                  func() bool
}

func (j *memoryIntegrationJournal) ProjectTracking(
	_ context.Context,
	request TrackingProjectionRequest,
) (TrackingProjection, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.locked != nil && !j.locked() {
		return TrackingProjection{}, errors.New("projection outside locked journal")
	}
	j.projectionCalls++
	revision := j.projectionRevision
	if revision == "" {
		revision = integrationDigest([]byte("projection"))
	}
	requestDigest := j.projectionRequestDigest
	if requestDigest == "" {
		requestDigest = request.RequestDigest
	}
	if j.projection == nil {
		return TrackingProjection{Revision: revision, RequestDigest: requestDigest, Files: []ProjectedTrackingFile{}}, nil
	}
	return TrackingProjection{Revision: revision, RequestDigest: requestDigest, Files: cloneProjectedTracking(j.projection)}, nil
}

type memoryLockedJournal struct {
	mu      sync.Mutex
	journal Journal
	active  atomic.Bool
}

func (l *memoryLockedJournal) WithLocked(_ context.Context, _ string, action func(Journal) error) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.active.Store(true)
	defer l.active.Store(false)
	return action(l.journal)
}

func (j *memoryIntegrationJournal) Load(_ context.Context, workspaceID, deliveryID, operationID string) (OperationState, bool, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.state == nil || j.state.WorkspaceID != workspaceID || j.state.DeliveryID != deliveryID || j.state.OperationID != operationID {
		return OperationState{}, false, nil
	}
	return cloneOperationState(*j.state), true, nil
}

func (j *memoryIntegrationJournal) CompareAndSwap(_ context.Context, before *OperationState, after OperationState) error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if before == nil {
		if j.state != nil {
			return ErrJournalConflict
		}
	} else if j.state == nil || !reflect.DeepEqual(*j.state, *before) {
		return ErrJournalConflict
	}
	cloned := cloneOperationState(after)
	j.state = &cloned
	return nil
}

func (j *memoryIntegrationJournal) force(state OperationState) {
	j.mu.Lock()
	defer j.mu.Unlock()
	cloned := cloneOperationState(state)
	j.state = &cloned
}

type fakeTrackingSynchronizer struct {
	calls int
	last  TrackingSyncRequest
}

func (s *fakeTrackingSynchronizer) Sync(_ context.Context, request TrackingSyncRequest) (TrackingSyncResult, error) {
	s.calls++
	s.last = request
	return TrackingSyncResult{Digest: integrationDigest([]byte("tracking")), MetadataCommitSHA: strings.Repeat("9", 40)}, nil
}
