package publication

import (
	"context"
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPublisherRejectsHeadOrCleanlinessDriftBeforeMutation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		expected string
		dirty    bool
	}{
		{name: "head drift", expected: "89abcdef0123456789abcdef0123456789abcdef"},
		{name: "dirty worktree", expected: testHeadSHA, dirty: true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inspection := validInspection()
			snapshot := validSnapshot()
			if tt.dirty {
				inspection.Status.DirtyFiles = ptr(1)
				snapshot.Clean = false
			}
			client := &publisherWorktreeClient{inspection: inspection, exitPlans: []ExitPlan{validExitPlan()}}
			git := &fakeGitEvidence{snapshot: snapshot, baseAhead: 1}
			output, err := newTestPublisher(client, git).Publish(
				context.Background(), trustedScope(), PublishInput{WorktreeRef: "wt_delivery", ExpectedHeadSHA: tt.expected},
			)
			if err != nil {
				t.Fatalf("Publish() error = %v", err)
			}
			if output.Status != PublishStatusBlocked {
				t.Fatalf("Status = %q, want blocked", output.Status)
			}
			if client.pushCalls != 0 || client.openPRCalls != 0 {
				t.Fatalf("mutation calls = push:%d pr:%d, want zero", client.pushCalls, client.openPRCalls)
			}
		})
	}
}

func TestPublisherReturnsNothingToPublishWithoutCallingMutation(t *testing.T) {
	t.Parallel()

	inspection := validInspection()
	inspection.Status.AheadOfBase = ptr(0)
	plan := validExitPlan()
	plan.Actions = []ExitAction{{Action: "push", Enabled: false}}
	plan.Forge = nil
	client := &publisherWorktreeClient{inspection: inspection, exitPlans: []ExitPlan{plan}}
	git := &fakeGitEvidence{snapshot: validSnapshot(), baseAhead: 0}

	output, err := newTestPublisher(client, git).Publish(
		context.Background(), trustedScope(), PublishInput{WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA},
	)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if output.Status != PublishStatusNothing || len(output.OperationIDs) != 0 || output.PRURL != "" {
		t.Fatalf("Publish() = %#v", output)
	}
	if client.pushCalls != 0 || client.openPRCalls != 0 {
		t.Fatalf("mutation calls = push:%d pr:%d, want zero", client.pushCalls, client.openPRCalls)
	}
}

func TestPublisherReconcilesPushBeforeOpeningPR(t *testing.T) {
	t.Parallel()

	initial := validExitPlan()
	paused := initial
	paused.Actions = append([]ExitAction(nil), initial.Actions...)
	paused.GlobalPauseCause = "push running"
	for index := range paused.Actions {
		paused.Actions[index].Enabled = false
	}
	open := pushedOpenPRPlan()
	visible := existingPRPlan("https://github.com/acme/repo/pull/42")
	client := &publisherWorktreeClient{
		inspection:    validInspection(),
		exitPlans:     []ExitPlan{initial, paused, open, visible},
		pushOperation: Operation{OperationID: "op-push"},
		prOperation:   Operation{OperationID: "op-pr"},
	}
	git := &fakeGitEvidence{snapshot: validSnapshot(), upstream: testHeadSHA, baseAhead: 1}

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	output, err := newTestPublisher(client, git).Publish(
		ctx, trustedScope(), PublishInput{WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA},
	)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if output.Status != PublishStatusPublished || output.PRURL != "https://github.com/acme/repo/pull/42" {
		t.Fatalf("Publish() = %#v", output)
	}
	if !reflect.DeepEqual(output.OperationIDs, []string{"op-push", "op-pr"}) {
		t.Fatalf("OperationIDs = %#v", output.OperationIDs)
	}
	wantEvents := []string{"inspect", "exit", "push", "exit", "exit", "open_pr", "exit"}
	if !reflect.DeepEqual(client.events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", client.events, wantEvents)
	}
	if client.pushCalls != 1 || client.openPRCalls != 1 {
		t.Fatalf("mutation calls = push:%d pr:%d, want one each", client.pushCalls, client.openPRCalls)
	}
	if !reflect.DeepEqual(client.openedPrefill, PRPrefill{Title: "Feature title", Body: "Feature body"}) ||
		client.openedBase != "main" {
		t.Fatalf("OpenPR inputs = prefill:%#v base:%q", client.openedPrefill, client.openedBase)
	}
}

func TestPublisherReturnsExistingPRWithoutDuplicateMutation(t *testing.T) {
	t.Parallel()

	inspection := validInspection()
	setTracking(&inspection, 0, 0)
	client := &publisherWorktreeClient{
		inspection: inspection,
		exitPlans:  []ExitPlan{existingPRPlan("https://github.com/acme/repo/pull/42")},
	}
	git := &fakeGitEvidence{snapshot: validSnapshot(), upstream: testHeadSHA, baseAhead: 1}
	output, err := newTestPublisher(client, git).Publish(
		context.Background(), trustedScope(), PublishInput{WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA},
	)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if output.Status != PublishStatusPublished || output.PRURL == "" || len(output.OperationIDs) != 0 {
		t.Fatalf("Publish() = %#v", output)
	}
	if client.pushCalls != 0 || client.openPRCalls != 0 {
		t.Fatalf("mutation calls = push:%d pr:%d, want zero", client.pushCalls, client.openPRCalls)
	}
}

func TestPublisherDoesNotRetryAmbiguousPushBlindly(t *testing.T) {
	t.Parallel()

	reconciled := validExitPlan()
	reconciled.Primary = "reconciling_push"
	for index := range reconciled.Actions {
		reconciled.Actions[index].Enabled = false
	}
	client := &publisherWorktreeClient{
		inspection:    validInspection(),
		exitPlans:     []ExitPlan{validExitPlan(), reconciled},
		pushOperation: Operation{OperationID: "op-push"},
		pushErr:       errors.New("ambiguous transport failure"),
	}
	git := &fakeGitEvidence{snapshot: validSnapshot(), baseAhead: 1}
	output, err := newTestPublisher(client, git).Publish(
		context.Background(), trustedScope(), PublishInput{WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA},
	)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if output.Status != PublishStatusBlocked || client.pushCalls != 1 || client.openPRCalls != 0 {
		t.Fatalf("Publish() = %#v; calls push:%d pr:%d", output, client.pushCalls, client.openPRCalls)
	}
	if !reflect.DeepEqual(output.OperationIDs, []string{"op-push"}) || output.LastExitPlan.Primary != "reconciling_push" {
		t.Fatalf("reconciled output = %#v", output)
	}
	wantEvents := []string{"inspect", "exit", "push", "exit"}
	if !reflect.DeepEqual(client.events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", client.events, wantEvents)
	}
}

func TestPublisherReconcilesAmbiguousPRWithoutDuplicateMutation(t *testing.T) {
	t.Parallel()

	visible := existingPRPlan("https://github.com/acme/repo/pull/42")
	client := &publisherWorktreeClient{
		inspection:    validInspection(),
		exitPlans:     []ExitPlan{validExitPlan(), pushedOpenPRPlan(), visible},
		pushOperation: Operation{OperationID: "op-push"},
		prOperation:   Operation{OperationID: "op-pr"},
		prErr:         errors.New("ambiguous PR transport failure"),
	}
	git := &fakeGitEvidence{snapshot: validSnapshot(), upstream: testHeadSHA, baseAhead: 1}
	output, err := newTestPublisher(client, git).Publish(
		context.Background(), trustedScope(), PublishInput{WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA},
	)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if output.Status != PublishStatusPublished || output.PRURL != "https://github.com/acme/repo/pull/42" {
		t.Fatalf("Publish() = %#v", output)
	}
	if !reflect.DeepEqual(output.OperationIDs, []string{"op-push", "op-pr"}) {
		t.Fatalf("OperationIDs = %#v", output.OperationIDs)
	}
	if client.pushCalls != 1 || client.openPRCalls != 1 {
		t.Fatalf("mutation calls = push:%d pr:%d, want one each", client.pushCalls, client.openPRCalls)
	}
	wantEvents := []string{"inspect", "exit", "push", "exit", "open_pr", "exit"}
	if !reflect.DeepEqual(client.events, wantEvents) {
		t.Fatalf("events = %#v, want %#v", client.events, wantEvents)
	}
}

func TestPublisherBlocksMissingMutationReceiptsDespiteFreshSuccessEvidence(t *testing.T) {
	t.Parallel()

	t.Run("push receipt missing", func(t *testing.T) {
		t.Parallel()
		client := &publisherWorktreeClient{
			inspection: validInspection(),
			exitPlans:  []ExitPlan{validExitPlan(), pushedOpenPRPlan()},
		}
		git := &fakeGitEvidence{snapshot: validSnapshot(), upstream: testHeadSHA, baseAhead: 1}
		output, err := newTestPublisher(client, git).Publish(
			context.Background(), trustedScope(), PublishInput{WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA},
		)
		if err != nil {
			t.Fatalf("Publish() error = %v", err)
		}
		if output.Status != PublishStatusBlocked || client.pushCalls != 1 || client.openPRCalls != 0 {
			t.Fatalf("Publish() = %#v; calls push:%d pr:%d", output, client.pushCalls, client.openPRCalls)
		}
		if output.LastExitPlan.Primary != "open_pr" || len(output.OperationIDs) != 0 {
			t.Fatalf("reconciled output = %#v", output)
		}
	})

	t.Run("PR receipt missing", func(t *testing.T) {
		t.Parallel()
		visible := existingPRPlan("https://github.com/acme/repo/pull/42")
		client := &publisherWorktreeClient{
			inspection:    validInspection(),
			exitPlans:     []ExitPlan{validExitPlan(), pushedOpenPRPlan(), visible},
			pushOperation: Operation{OperationID: "op-push"},
		}
		git := &fakeGitEvidence{snapshot: validSnapshot(), upstream: testHeadSHA, baseAhead: 1}
		output, err := newTestPublisher(client, git).Publish(
			context.Background(), trustedScope(), PublishInput{WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA},
		)
		if err != nil {
			t.Fatalf("Publish() error = %v", err)
		}
		if output.Status != PublishStatusBlocked || client.pushCalls != 1 || client.openPRCalls != 1 {
			t.Fatalf("Publish() = %#v; calls push:%d pr:%d", output, client.pushCalls, client.openPRCalls)
		}
		if output.PRURL != "" || !reflect.DeepEqual(output.OperationIDs, []string{"op-push"}) {
			t.Fatalf("receipt-less PR output = %#v", output)
		}
	})
}

func TestPublisherReportsPushedButBlockedWhenPRCannotBeObserved(t *testing.T) {
	t.Parallel()

	blocked := pushedOpenPRPlan()
	for index := range blocked.Actions {
		blocked.Actions[index].Enabled = false
	}
	client := &publisherWorktreeClient{
		inspection:    validInspection(),
		exitPlans:     []ExitPlan{validExitPlan(), blocked},
		pushOperation: Operation{OperationID: "op-push"},
	}
	git := &fakeGitEvidence{snapshot: validSnapshot(), upstream: testHeadSHA, baseAhead: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	output, err := newTestPublisher(client, git).Publish(
		ctx, trustedScope(), PublishInput{WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA},
	)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if output.Status != PublishStatusBlocked || !reflect.DeepEqual(output.OperationIDs, []string{"op-push"}) {
		t.Fatalf("Publish() = %#v", output)
	}
	if client.pushCalls != 1 || client.openPRCalls != 0 {
		t.Fatalf("mutation calls = push:%d pr:%d", client.pushCalls, client.openPRCalls)
	}
}

func TestPublisherRequiresRealPRURLAndExactUpstreamHead(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		visible    ExitPlan
		upstream   string
		wantStatus PublishStatus
	}{
		{
			name: "compare URL is not PR evidence",
			visible: func() ExitPlan {
				plan := pushedOpenPRPlan()
				plan.BrowserURL = "https://github.com/acme/repo/compare/main...feature"
				return plan
			}(),
			upstream: testHeadSHA, wantStatus: PublishStatusBlocked,
		},
		{
			name:     "upstream head drift",
			visible:  existingPRPlan("https://github.com/acme/repo/pull/42"),
			upstream: "89abcdef0123456789abcdef0123456789abcdef", wantStatus: PublishStatusBlocked,
		},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inspection := validInspection()
			setTracking(&inspection, 0, 0)
			client := &publisherWorktreeClient{inspection: inspection, exitPlans: []ExitPlan{tt.visible}}
			git := &fakeGitEvidence{snapshot: validSnapshot(), upstream: tt.upstream, baseAhead: 1}
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
			defer cancel()
			output, err := newTestPublisher(client, git).Publish(
				ctx, trustedScope(), PublishInput{WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA},
			)
			if err != nil {
				t.Fatalf("Publish() error = %v", err)
			}
			if output.Status != tt.wantStatus || output.PRURL != "" {
				t.Fatalf("Publish() = %#v", output)
			}
		})
	}
}

func TestPublisherReturnsKnownOperationIDsOnDeadline(t *testing.T) {
	t.Parallel()

	client := &publisherWorktreeClient{
		inspection:    validInspection(),
		exitPlans:     []ExitPlan{validExitPlan()},
		pushOperation: Operation{OperationID: "op-push"},
	}
	git := &fakeGitEvidence{snapshot: validSnapshot(), baseAhead: 1}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Millisecond)
	defer cancel()
	output, err := newTestPublisher(client, git).Publish(
		ctx, trustedScope(), PublishInput{WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA},
	)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if output.Status != PublishStatusBlocked || !reflect.DeepEqual(output.OperationIDs, []string{"op-push"}) {
		t.Fatalf("Publish() = %#v", output)
	}
}

func TestPublisherRejectsMalformedExpectedHeadAndBoundsSummary(t *testing.T) {
	t.Parallel()

	client := &publisherWorktreeClient{}
	publisher := newTestPublisher(client, &fakeGitEvidence{})
	for _, head := range []string{"", "ABCDEF0123456789abcdef0123456789abcdef01", "not-a-sha"} {
		if _, err := publisher.Publish(
			context.Background(), trustedScope(), PublishInput{WorktreeRef: "wt_delivery", ExpectedHeadSHA: head},
		); err == nil {
			t.Fatalf("Publish(head=%q) error = nil", head)
		}
	}
	if len(client.events) != 0 {
		t.Fatalf("events = %#v, want none", client.events)
	}

	client = &publisherWorktreeClient{
		inspection: validInspection(),
		exitPlans:  []ExitPlan{validExitPlan()},
		pushErr:    errors.New(strings.Repeat("secret-output", 1000)),
	}
	output, err := newTestPublisher(client, &fakeGitEvidence{snapshot: validSnapshot(), baseAhead: 1}).Publish(
		context.Background(), trustedScope(), PublishInput{WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA},
	)
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if len([]byte(output.Summary)) > 1024 || strings.Contains(output.Summary, "secret-output") {
		t.Fatalf("Summary is unsafe: length=%d value=%q", len([]byte(output.Summary)), output.Summary)
	}
}

func newTestPublisher(client *publisherWorktreeClient, git *fakeGitEvidence) Publisher {
	return Publisher{
		Planner:      PublicationPlanner{Compozy: client, Git: git},
		Compozy:      client,
		Git:          git,
		PollInterval: time.Millisecond,
	}
}

func pushedOpenPRPlan() ExitPlan {
	plan := validExitPlan()
	plan.Primary = "open_pr"
	plan.Actions = []ExitAction{
		{Action: "push", Enabled: false},
		{Action: "open_pr", Enabled: true, Publish: true},
	}
	return plan
}

func existingPRPlan(prURL string) ExitPlan {
	plan := validExitPlan()
	plan.Primary = "view_pr"
	plan.Actions = []ExitAction{{Action: "view_pr", Enabled: true, URL: prURL}}
	plan.ForgeStatus = &ForgeStatus{Provider: "github", PRURL: prURL}
	plan.PRPrefill = nil
	return plan
}

type publisherWorktreeClient struct {
	inspection WorktreeInspection
	exitPlans  []ExitPlan
	exitIndex  int

	pushOperation Operation
	prOperation   Operation
	pushErr       error
	prErr         error

	pushCalls     int
	openPRCalls   int
	openedPrefill PRPrefill
	openedBase    string
	events        []string
}

func (c *publisherWorktreeClient) Inspect(context.Context, TrustedScope, string) (WorktreeInspection, error) {
	c.events = append(c.events, "inspect")
	return c.inspection, nil
}

func (c *publisherWorktreeClient) ExitPlan(context.Context, TrustedScope, string) (ExitPlan, error) {
	c.events = append(c.events, "exit")
	if len(c.exitPlans) == 0 {
		return ExitPlan{}, errors.New("no exit plan")
	}
	index := c.exitIndex
	if index >= len(c.exitPlans) {
		index = len(c.exitPlans) - 1
	} else {
		c.exitIndex++
	}
	return c.exitPlans[index], nil
}

func (c *publisherWorktreeClient) Push(context.Context, TrustedScope, string) (Operation, error) {
	c.events = append(c.events, "push")
	c.pushCalls++
	return c.pushOperation, c.pushErr
}

func (c *publisherWorktreeClient) OpenPR(
	_ context.Context,
	_ TrustedScope,
	_ string,
	prefill PRPrefill,
	base string,
) (Operation, error) {
	c.events = append(c.events, "open_pr")
	c.openPRCalls++
	c.openedPrefill = prefill
	c.openedBase = base
	return c.prOperation, c.prErr
}
