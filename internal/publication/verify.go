package publication

import (
	"context"
	"errors"
	"strings"
)

type VerifyInput struct {
	WorktreeRef     string        `json:"worktree_ref"`
	ExpectedHeadSHA string        `json:"expected_head_sha"`
	PublisherResult PublishOutput `json:"publisher_result"`
}

type VerifyOutput struct {
	Verified bool          `json:"verified"`
	Status   PublishStatus `json:"status"`
	HeadSHA  string        `json:"head_sha"`
	PRURL    string        `json:"pr_url,omitempty"`
	Summary  string        `json:"summary"`
}

type Verifier struct {
	Planner PublicationPlanner
	Git     GitEvidence
}

func (v Verifier) Verify(
	ctx context.Context,
	scope TrustedScope,
	input VerifyInput,
) (VerifyOutput, error) {
	if !gitSHA.MatchString(input.ExpectedHeadSHA) {
		return verificationRejected(input.PublisherResult.Status, "expected head SHA is invalid")
	}
	if v.Git == nil {
		return verificationRejected(input.PublisherResult.Status, "verification dependencies are unavailable")
	}
	if input.PublisherResult.HeadSHA != input.ExpectedHeadSHA {
		return verificationRejected(input.PublisherResult.Status, "publisher head does not match expected head")
	}

	plan, err := v.Planner.Plan(ctx, scope, PlanInput{WorktreeRef: input.WorktreeRef})
	if err != nil {
		// A deterministic blocker is fresh evidence too: it is what a blocked
		// publisher result has to be verified against.
		var blocked *BlockedPlanError
		if !errors.As(err, &blocked) {
			return verificationRejected(input.PublisherResult.Status, "fresh publication plan is unavailable")
		}
		plan = blocked.Plan
	}
	if !plan.Clean || plan.HeadSHA != input.ExpectedHeadSHA {
		return verificationRejected(input.PublisherResult.Status, "local publication evidence changed")
	}

	switch input.PublisherResult.Status {
	case PublishStatusPublished:
		return v.verifyPublished(ctx, input, plan)
	case PublishStatusNothing:
		return verifyNothing(input, plan)
	case PublishStatusBlocked:
		return verifyBlocked(input, plan)
	default:
		return verificationRejected(input.PublisherResult.Status, "publisher status is unsupported")
	}
}

func (v Verifier) verifyPublished(
	ctx context.Context,
	input VerifyInput,
	plan PlanOutput,
) (VerifyOutput, error) {
	if plan.Disposition != DispositionPublishable || !isAbsoluteHTTPS(input.PublisherResult.PRURL) {
		return verificationRejected(PublishStatusPublished, "published result lacks current pull request evidence")
	}
	observedURL, ok := observedPRURL(plan.ExitPlan)
	if !ok || observedURL != input.PublisherResult.PRURL {
		return verificationRejected(PublishStatusPublished, "publisher pull request does not match current evidence")
	}
	upstreamHead, err := v.Git.UpstreamHead(ctx, plan.WorktreePath)
	if err != nil || upstreamHead != input.ExpectedHeadSHA {
		return verificationRejected(PublishStatusPublished, "upstream head does not match expected head")
	}
	return VerifyOutput{
		Verified: true,
		Status:   PublishStatusPublished,
		HeadSHA:  plan.HeadSHA,
		PRURL:    observedURL,
		Summary:  boundedSummary("publication independently verified"),
	}, nil
}

func verifyNothing(input VerifyInput, plan PlanOutput) (VerifyOutput, error) {
	if len(input.PublisherResult.OperationIDs) != 0 || input.PublisherResult.PRURL != "" {
		return verificationRejected(PublishStatusNothing, "no-op publication contains mutation claims")
	}
	if plan.Disposition != DispositionNothing {
		return verificationRejected(PublishStatusNothing, "fresh plan contains commits to publish")
	}
	return VerifyOutput{
		Verified: true,
		Status:   PublishStatusNothing,
		HeadSHA:  plan.HeadSHA,
		Summary:  boundedSummary("nothing-to-publish result independently verified"),
	}, nil
}

func verifyBlocked(input VerifyInput, plan PlanOutput) (VerifyOutput, error) {
	if len(input.PublisherResult.OperationIDs) != 0 || input.PublisherResult.PRURL != "" {
		return verificationRejected(PublishStatusBlocked, "blocked publication contains mutation claims")
	}
	if plan.Disposition != DispositionBlocked || len(plan.Blockers) == 0 {
		return verificationRejected(PublishStatusBlocked, "fresh plan is not blocked")
	}
	return VerifyOutput{
		Verified: true,
		Status:   PublishStatusBlocked,
		HeadSHA:  plan.HeadSHA,
		Summary:  boundedSummary("blocked publication independently verified: " + strings.Join(plan.Blockers, ",")),
	}, nil
}

func verificationRejected(status PublishStatus, reason string) (VerifyOutput, error) {
	return VerifyOutput{
		Verified: false,
		Status:   status,
		Summary:  boundedSummary(reason),
	}, errors.New("publication: independent verification rejected the result")
}
