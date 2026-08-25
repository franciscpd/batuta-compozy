package publication

import (
	"context"
	"strings"
	"testing"
)

func TestVerifierRejectsFabricatedOrMismatchedPublisherEvidence(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		mutate func(*PublishOutput)
	}{
		{name: "fabricated PR URL", mutate: func(o *PublishOutput) { o.PRURL = "https://github.com/acme/repo/pull/99" }},
		{name: "publisher head mismatch", mutate: func(o *PublishOutput) { o.HeadSHA = "89abcdef0123456789abcdef0123456789abcdef" }},
		{name: "blocked publisher", mutate: func(o *PublishOutput) { o.Status = PublishStatusBlocked }},
		{name: "unknown publisher status", mutate: func(o *PublishOutput) { o.Status = PublishStatus("invented") }},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			verifier := publishedVerifier("https://github.com/acme/repo/pull/42", testHeadSHA)
			result := genuinePublishedResult()
			tt.mutate(&result)
			output, err := verifier.Verify(context.Background(), trustedScope(), VerifyInput{
				WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA, PublisherResult: result,
			})
			if err == nil || output.Verified {
				t.Fatalf("Verify() = (%#v, %v), want rejection", output, err)
			}
		})
	}
}

func TestVerifierRejectsCompareURLAndMissingPR(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		plan ExitPlan
	}{
		{
			name: "compare URL only",
			plan: func() ExitPlan {
				plan := pushedOpenPRPlan()
				plan.BrowserURL = "https://github.com/acme/repo/compare/main...feature"
				return plan
			}(),
		},
		{name: "missing PR", plan: pushedOpenPRPlan()},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inspection := validInspection()
			setTracking(&inspection, 0, 0)
			client := &publisherWorktreeClient{inspection: inspection, exitPlans: []ExitPlan{tt.plan}}
			git := &fakeGitEvidence{snapshot: validSnapshot(), upstream: testHeadSHA, baseAhead: 1}
			verifier := Verifier{Planner: PublicationPlanner{Compozy: client, Git: git}, Git: git}
			output, err := verifier.Verify(context.Background(), trustedScope(), VerifyInput{
				WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA, PublisherResult: genuinePublishedResult(),
			})
			if err == nil || output.Verified {
				t.Fatalf("Verify() = (%#v, %v), want missing-PR rejection", output, err)
			}
		})
	}
}

func TestVerifierRejectsDirtyTreeHeadDriftAndUpstreamDrift(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		inspection func(*WorktreeInspection)
		snapshot   func(*GitSnapshot)
		upstream   string
	}{
		{
			name:       "dirty tree",
			inspection: func(i *WorktreeInspection) { i.Status.DirtyFiles = ptr(1) },
			snapshot:   func(s *GitSnapshot) { s.Clean = false },
			upstream:   testHeadSHA,
		},
		{
			name:       "local head drift",
			inspection: func(i *WorktreeInspection) { i.Status.HeadSHA = ptr("89abcdef0123456789abcdef0123456789abcdef") },
			snapshot:   func(s *GitSnapshot) { s.HeadSHA = "89abcdef0123456789abcdef0123456789abcdef" },
			upstream:   "89abcdef0123456789abcdef0123456789abcdef",
		},
		{name: "upstream drift", upstream: "89abcdef0123456789abcdef0123456789abcdef"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			inspection := validInspection()
			setTracking(&inspection, 0, 0)
			if tt.inspection != nil {
				tt.inspection(&inspection)
			}
			snapshot := validSnapshot()
			if tt.snapshot != nil {
				tt.snapshot(&snapshot)
			}
			client := &publisherWorktreeClient{
				inspection: inspection,
				exitPlans:  []ExitPlan{existingPRPlan("https://github.com/acme/repo/pull/42")},
			}
			git := &fakeGitEvidence{snapshot: snapshot, upstream: tt.upstream, baseAhead: 1}
			verifier := Verifier{Planner: PublicationPlanner{Compozy: client, Git: git}, Git: git}
			output, err := verifier.Verify(context.Background(), trustedScope(), VerifyInput{
				WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA, PublisherResult: genuinePublishedResult(),
			})
			if err == nil || output.Verified {
				t.Fatalf("Verify() = (%#v, %v), want evidence rejection", output, err)
			}
		})
	}
}

func TestVerifierAcceptsExactPublishedHeadAndObservedPR(t *testing.T) {
	t.Parallel()

	verifier := publishedVerifier("https://github.com/acme/repo/pull/42", testHeadSHA)
	output, err := verifier.Verify(context.Background(), trustedScope(), VerifyInput{
		WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA, PublisherResult: genuinePublishedResult(),
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !output.Verified || output.Status != PublishStatusPublished ||
		output.HeadSHA != testHeadSHA || output.PRURL != "https://github.com/acme/repo/pull/42" {
		t.Fatalf("Verify() = %#v", output)
	}
}

func TestVerifierAcceptsGenuineNothingToPublishWithoutOperationClaims(t *testing.T) {
	t.Parallel()

	inspection := validInspection()
	inspection.Status.AheadOfBase = ptr(0)
	plan := validExitPlan()
	plan.Actions = []ExitAction{{Action: "push", Enabled: false}}
	plan.Forge = nil
	client := &publisherWorktreeClient{inspection: inspection, exitPlans: []ExitPlan{plan}}
	git := &fakeGitEvidence{snapshot: validSnapshot(), baseAhead: 0}
	verifier := Verifier{Planner: PublicationPlanner{Compozy: client, Git: git}, Git: git}
	result := PublishOutput{
		Status: PublishStatusNothing, HeadSHA: testHeadSHA, OperationIDs: []string{}, Summary: "nothing",
	}
	output, err := verifier.Verify(context.Background(), trustedScope(), VerifyInput{
		WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA, PublisherResult: result,
	})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !output.Verified || output.Status != PublishStatusNothing || output.PRURL != "" {
		t.Fatalf("Verify() = %#v", output)
	}
}

func TestVerifierRejectsNothingToPublishWithOperationClaims(t *testing.T) {
	t.Parallel()

	inspection := validInspection()
	inspection.Status.AheadOfBase = ptr(0)
	plan := validExitPlan()
	plan.Actions = []ExitAction{{Action: "push", Enabled: false}}
	plan.Forge = nil
	client := &publisherWorktreeClient{inspection: inspection, exitPlans: []ExitPlan{plan}}
	git := &fakeGitEvidence{snapshot: validSnapshot(), baseAhead: 0}
	verifier := Verifier{Planner: PublicationPlanner{Compozy: client, Git: git}, Git: git}
	for _, mutate := range []func(*PublishOutput){
		func(o *PublishOutput) { o.OperationIDs = []string{"op-fabricated"} },
		func(o *PublishOutput) { o.PRURL = "https://github.com/acme/repo/pull/42" },
	} {
		result := PublishOutput{Status: PublishStatusNothing, HeadSHA: testHeadSHA, OperationIDs: []string{}}
		mutate(&result)
		output, err := verifier.Verify(context.Background(), trustedScope(), VerifyInput{
			WorktreeRef: "wt_delivery", ExpectedHeadSHA: testHeadSHA, PublisherResult: result,
		})
		if err == nil || output.Verified {
			t.Fatalf("Verify() = (%#v, %v), want claimed-evidence rejection", output, err)
		}
	}
}

func TestVerifierRejectsMalformedExpectedHead(t *testing.T) {
	t.Parallel()

	output, err := (Verifier{}).Verify(context.Background(), trustedScope(), VerifyInput{ExpectedHeadSHA: "not-a-sha"})
	if err == nil || output.Verified || strings.Contains(err.Error(), testHeadSHA) {
		t.Fatalf("Verify() = (%#v, %v), want safe validation error", output, err)
	}
}

func publishedVerifier(prURL, upstream string) Verifier {
	inspection := validInspection()
	setTracking(&inspection, 0, 0)
	client := &publisherWorktreeClient{inspection: inspection, exitPlans: []ExitPlan{existingPRPlan(prURL)}}
	git := &fakeGitEvidence{snapshot: validSnapshot(), upstream: upstream, baseAhead: 1}
	return Verifier{Planner: PublicationPlanner{Compozy: client, Git: git}, Git: git}
}

func genuinePublishedResult() PublishOutput {
	return PublishOutput{
		Status: PublishStatusPublished, HeadSHA: testHeadSHA,
		OperationIDs: []string{"op-push", "op-pr"},
		PRURL:        "https://github.com/acme/repo/pull/42",
		Summary:      "published",
	}
}
