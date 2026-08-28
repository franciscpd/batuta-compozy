package publication

import (
	"context"
	"errors"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	maxPublicationSummaryBytes = 1024
	publicationPollTimeout     = 30 * time.Second
)

type PublishInput struct {
	WorktreeRef     string `json:"worktree_ref"`
	ExpectedHeadSHA string `json:"expected_head_sha"`
}

type PublishStatus string

const (
	PublishStatusPublished PublishStatus = "published"
	PublishStatusNothing   PublishStatus = "nothing_to_publish"
	PublishStatusBlocked   PublishStatus = "blocked"
)

type PublishOutput struct {
	Status       PublishStatus `json:"status"`
	HeadSHA      string        `json:"head_sha"`
	OperationIDs []string      `json:"op_ids"`
	PRURL        string        `json:"pr_url,omitempty"`
	Summary      string        `json:"summary"`
	LastExitPlan ExitPlan      `json:"last_exit_plan"`
}

type Publisher struct {
	Planner      PublicationPlanner
	Compozy      WorktreeClient
	Git          GitEvidence
	PollInterval time.Duration
	PollTimeout  time.Duration
}

func (p Publisher) Publish(
	ctx context.Context,
	scope TrustedScope,
	input PublishInput,
) (PublishOutput, error) {
	if !gitSHA.MatchString(input.ExpectedHeadSHA) {
		return PublishOutput{}, errors.New("publication: expected head SHA is invalid")
	}
	if p.Compozy == nil || p.Git == nil {
		return PublishOutput{}, errors.New("publication: publisher dependencies are required")
	}

	plan, err := p.Planner.Plan(ctx, scope, PlanInput{WorktreeRef: input.WorktreeRef})
	if err != nil {
		var blocked *BlockedPlanError
		if errors.As(err, &blocked) {
			return blockedPublish(blocked.Plan.HeadSHA, nil, blocked.Plan.ExitPlan, "publication plan is blocked"), nil
		}
		return blockedPublish("", nil, ExitPlan{}, "publication plan could not be read"), nil
	}
	output := PublishOutput{
		HeadSHA:      plan.HeadSHA,
		OperationIDs: []string{},
		LastExitPlan: plan.ExitPlan,
	}
	if !plan.Clean || plan.HeadSHA != input.ExpectedHeadSHA {
		return blockedPublish(plan.HeadSHA, nil, plan.ExitPlan, "local publication evidence changed"), nil
	}
	if plan.Disposition == DispositionNothing {
		output.Status = PublishStatusNothing
		output.Summary = boundedSummary("worktree has no commits to publish")
		return output, nil
	}
	if plan.Disposition != DispositionPublishable {
		return blockedPublish(plan.HeadSHA, nil, plan.ExitPlan, "publication state is not publishable"), nil
	}

	if prURL, ok := observedPRURL(plan.ExitPlan); ok {
		if !p.upstreamMatches(ctx, plan.WorktreePath, input.ExpectedHeadSHA) {
			return blockedPublish(plan.HeadSHA, nil, plan.ExitPlan, "pull request head is not published upstream"), nil
		}
		output.Status = PublishStatusPublished
		output.PRURL = prURL
		output.Summary = boundedSummary("existing pull request and upstream head verified")
		return output, nil
	}
	if err := ctx.Err(); err != nil {
		return blockedPublish(plan.HeadSHA, nil, plan.ExitPlan, "publication deadline reached before mutation"), nil
	}

	pushOperation, pushErr := p.Compozy.Push(ctx, scope, input.WorktreeRef)
	pushOperationID := strings.TrimSpace(pushOperation.OperationID)
	if pushOperationID != "" {
		output.OperationIDs = append(output.OperationIDs, pushOperationID)
	}
	var openPlan ExitPlan
	var prURL string
	var ready bool
	if pushErr != nil || pushOperationID == "" {
		openPlan, prURL, ready = p.reconcileOnce(
			ctx, scope, input.WorktreeRef, plan.WorktreePath, input.ExpectedHeadSHA, plan.ExitPlan,
		)
		if pushOperationID == "" {
			return blockedPublish(
				plan.HeadSHA, output.OperationIDs, openPlan, "push did not return a durable operation receipt",
			), nil
		}
		if prURL != "" {
			output.Status = PublishStatusPublished
			output.PRURL = prURL
			output.Summary = boundedSummary("pull request and upstream head verified after push reconciliation")
			output.LastExitPlan = openPlan
			return output, nil
		}
		if !ready {
			return blockedPublish(
				plan.HeadSHA, output.OperationIDs, openPlan, "push outcome is not safely observable",
			), nil
		}
	} else {
		openPlan, prURL, ready = p.waitForOpenPR(
			ctx, scope, input.WorktreeRef, plan.WorktreePath, input.ExpectedHeadSHA, plan.ExitPlan,
		)
	}
	output.LastExitPlan = openPlan
	if prURL != "" {
		output.Status = PublishStatusPublished
		output.PRURL = prURL
		output.Summary = boundedSummary("pull request and upstream head verified")
		return output, nil
	}
	if !ready {
		return blockedPublish(plan.HeadSHA, output.OperationIDs, openPlan, "push completed but pull request readiness was not observed"), nil
	}
	if openPlan.PRPrefill == nil {
		return blockedPublish(plan.HeadSHA, output.OperationIDs, openPlan, "pull request prefill is unavailable"), nil
	}
	base := strings.TrimSpace(openPlan.Base)
	if base == "" && openPlan.Forge != nil {
		base = strings.TrimSpace(openPlan.Forge.DefaultBranch)
	}
	if base == "" {
		return blockedPublish(plan.HeadSHA, output.OperationIDs, openPlan, "pull request base is unavailable"), nil
	}
	if err := ctx.Err(); err != nil {
		return blockedPublish(plan.HeadSHA, output.OperationIDs, openPlan, "publication deadline reached before opening pull request"), nil
	}

	prOperation, prErr := p.Compozy.OpenPR(ctx, scope, input.WorktreeRef, *openPlan.PRPrefill, base)
	prOperationID := strings.TrimSpace(prOperation.OperationID)
	if prOperationID != "" {
		output.OperationIDs = append(output.OperationIDs, prOperationID)
	}
	if prErr != nil || prOperationID == "" {
		visiblePlan, visibleURL, _ := p.reconcileOnce(
			ctx, scope, input.WorktreeRef, plan.WorktreePath, input.ExpectedHeadSHA, openPlan,
		)
		if prOperationID == "" {
			return blockedPublish(
				plan.HeadSHA, output.OperationIDs, visiblePlan,
				"pull request operation did not return a durable receipt",
			), nil
		}
		if visibleURL != "" {
			output.Status = PublishStatusPublished
			output.PRURL = visibleURL
			output.Summary = boundedSummary("pull request and upstream head verified after operation reconciliation")
			output.LastExitPlan = visiblePlan
			return output, nil
		}
		return blockedPublish(
			plan.HeadSHA, output.OperationIDs, visiblePlan, "pull request outcome is not safely observable",
		), nil
	}

	visiblePlan, visibleURL := p.waitForVisiblePR(
		ctx, scope, input.WorktreeRef, plan.WorktreePath, input.ExpectedHeadSHA, openPlan,
	)
	if visibleURL == "" {
		return blockedPublish(plan.HeadSHA, output.OperationIDs, visiblePlan, "pull request URL was not observed"), nil
	}
	output.Status = PublishStatusPublished
	output.PRURL = visibleURL
	output.Summary = boundedSummary("pull request and upstream head verified")
	output.LastExitPlan = visiblePlan
	return output, nil
}

func (p Publisher) waitForOpenPR(
	ctx context.Context,
	scope TrustedScope,
	ref string,
	path string,
	expectedHead string,
	fallback ExitPlan,
) (ExitPlan, string, bool) {
	pollCtx, cancel := p.pollContext(ctx)
	defer cancel()
	last := fallback
	for {
		plan, err := p.Compozy.ExitPlan(pollCtx, scope, ref)
		if err != nil {
			return last, "", false
		}
		last = plan
		if prURL, ok := observedPRURL(plan); ok {
			if p.upstreamMatches(pollCtx, path, expectedHead) {
				return plan, prURL, false
			}
			return plan, "", false
		}
		if actionEnabled(plan, "open_pr") {
			return plan, "", true
		}
		if !p.wait(pollCtx) {
			return last, "", false
		}
	}
}

func (p Publisher) waitForVisiblePR(
	ctx context.Context,
	scope TrustedScope,
	ref string,
	path string,
	expectedHead string,
	fallback ExitPlan,
) (ExitPlan, string) {
	pollCtx, cancel := p.pollContext(ctx)
	defer cancel()
	last := fallback
	for {
		plan, err := p.Compozy.ExitPlan(pollCtx, scope, ref)
		if err != nil {
			return last, ""
		}
		last = plan
		if prURL, ok := observedPRURL(plan); ok {
			if p.upstreamMatches(pollCtx, path, expectedHead) {
				return plan, prURL
			}
			return plan, ""
		}
		if !p.wait(pollCtx) {
			return last, ""
		}
	}
}

func (p Publisher) reconcileOnce(
	ctx context.Context,
	scope TrustedScope,
	ref string,
	path string,
	expectedHead string,
	fallback ExitPlan,
) (ExitPlan, string, bool) {
	plan, err := p.Compozy.ExitPlan(ctx, scope, ref)
	if err != nil {
		return fallback, "", false
	}
	if prURL, ok := observedPRURL(plan); ok {
		if p.upstreamMatches(ctx, path, expectedHead) {
			return plan, prURL, false
		}
		return plan, "", false
	}
	return plan, "", actionEnabled(plan, "open_pr")
}

func (p Publisher) upstreamMatches(ctx context.Context, path, expectedHead string) bool {
	upstream, err := p.Git.UpstreamHead(ctx, path)
	return err == nil && upstream == expectedHead
}

func (p Publisher) wait(ctx context.Context) bool {
	interval := p.PollInterval
	if interval <= 0 {
		interval = 100 * time.Millisecond
	}
	timer := time.NewTimer(interval)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
}

func (p Publisher) pollContext(ctx context.Context) (context.Context, context.CancelFunc) {
	timeout := p.PollTimeout
	if timeout <= 0 {
		timeout = publicationPollTimeout
	}
	return context.WithTimeout(ctx, timeout)
}

func actionEnabled(plan ExitPlan, action string) bool {
	for _, candidate := range plan.Actions {
		if candidate.Action == action && candidate.Enabled {
			return true
		}
	}
	return false
}

func observedPRURL(plan ExitPlan) (string, bool) {
	urls := make(map[string]struct{}, 2)
	if plan.ForgeStatus != nil && isAbsoluteHTTPS(plan.ForgeStatus.PRURL) {
		urls[strings.TrimSpace(plan.ForgeStatus.PRURL)] = struct{}{}
	}
	for _, action := range plan.Actions {
		if action.Action == "view_pr" && action.Enabled && isAbsoluteHTTPS(action.URL) {
			urls[strings.TrimSpace(action.URL)] = struct{}{}
		}
	}
	if len(urls) != 1 {
		return "", false
	}
	for value := range urls {
		return value, true
	}
	return "", false
}

func blockedPublish(headSHA string, operationIDs []string, plan ExitPlan, summary string) PublishOutput {
	return PublishOutput{
		Status:       PublishStatusBlocked,
		HeadSHA:      headSHA,
		OperationIDs: append([]string{}, operationIDs...),
		Summary:      boundedSummary(summary),
		LastExitPlan: plan,
	}
}

func boundedSummary(value string) string {
	value = strings.TrimSpace(value)
	if len(value) <= maxPublicationSummaryBytes {
		return value
	}
	value = value[:maxPublicationSummaryBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}
