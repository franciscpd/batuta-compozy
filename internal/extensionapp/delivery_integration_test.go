//go:build integration

package extensionapp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/franciscpd/batuta-compozy/internal/publication"
	"github.com/franciscpd/batuta-compozy/internal/routing"
)

const integrationCursorModel = "grok-4.6[effort=high,fast=true]"

func TestMigrationFreeDeliveryUsesFreshRunFallbackAndVerifiesPublicationIntegration(t *testing.T) {
	ctx := context.Background()
	repository := newMigrationFreeRepository(t)
	git := publication.GitClient{Executable: repository.git, Runner: publication.ExecRunner{}}
	scope := publication.TrustedScope{WorkspaceID: "workspace_delivery_lab", WorkspaceRoot: repository.root}
	planInput := migrationFreePlanInput()
	snapshot := migrationFreeInventory(t)
	store, err := routing.NewOwnershipStore(t.TempDir())
	if err != nil {
		t.Fatalf("NewOwnershipStore() error = %v", err)
	}
	manager := routing.MatrixManager{Store: store}
	engine := routingEngine{
		inventory: func(context.Context, publication.TrustedScope) (inventory.InventorySnapshot, error) {
			return snapshot, nil
		},
		inspectWorktree: func(_ context.Context, got publication.TrustedScope, ref string) (publication.WorktreeInspection, error) {
			return publication.WorktreeInspection{Worktree: publication.Worktree{
				ID: ref, WorkspaceID: got.WorkspaceID, Branch: repository.branch,
				Path: repository.root, State: "ready", BaseRef: "main",
			}}, nil
		},
		worktreeState:  git.WorktreeState,
		applyMatrix:    manager.Apply,
		loadGeneration: store.LoadGeneration,
	}

	generation, err := engine.Plan(ctx, scope, planInput)
	if err != nil {
		t.Fatalf("Plan() error = %v", err)
	}
	assertMigrationFreeRouting(t, generation)
	if err := store.ArchiveGeneration(scope.WorkspaceID, generation); err != nil {
		t.Fatalf("ArchiveGeneration() error = %v", err)
	}
	if _, err := (routing.AlignmentManager{Store: store}).Confirm(scope.WorkspaceID, "session_delivery_lab", generation); err != nil {
		t.Fatalf("Alignment.Confirm() error = %v", err)
	}
	applied, err := engine.Apply(ctx, scope, RoutingApplyInput{
		Operation: RoutingOperationApplyMatrix, RoutingPlan: &planInput,
		ExpectedGenerationDigest: generation.Digest, WorktreeRef: "worktree_delivery_lab",
		OriginSessionID: "session_delivery_lab",
	})
	if err != nil {
		t.Fatalf("Apply(matrix) error = %v", err)
	}
	if applied.Matrix == nil || applied.Matrix.DeliveryID == "" {
		t.Fatalf("Apply(matrix) = %#v", applied)
	}
	deliveryID := applied.Matrix.DeliveryID

	now := applied.Matrix.CreatedAt.Add(time.Minute)
	client := &integrationDeliveryClient{now: now, statuses: map[string]deliveryRunDetail{}}
	service := deliveryAttemptService{
		Store: store, Client: client, Now: func() time.Time { return now }, WorktreeState: git.WorktreeState,
	}
	first, err := service.Start(ctx, scope, deliveryID)
	if err != nil {
		t.Fatalf("Start(attempt 1) error = %v", err)
	}
	firstReplay, err := service.Start(ctx, scope, deliveryID)
	if err != nil {
		t.Fatalf("Start(attempt 1 replay) error = %v", err)
	}
	if firstReplay.DeliveryRunID != first.DeliveryRunID || !firstReplay.Replayed || client.startCalls != 1 {
		t.Fatalf("attempt 1 replay = %#v / %#v, starts=%d", first, firstReplay, client.startCalls)
	}
	firstRequest := client.requests[1]

	repository.completeTask(t, "task_01", "backend", "low", "backend implementation completed")
	repository.write(t, "backend.txt", "backend complete\n")
	repository.commit(t, "feat: complete backend task")
	backendHead := repository.head(t)
	contextService := &deliveryContextService{Store: store, Client: client, Now: func() time.Time { return now }}
	routingAfterProgress, err := contextService.Routing(ctx, scope, RoutingContextInput{
		DeliveryID: deliveryID, Attempt: first.Attempt, Slug: planInput.Slug, RoutingGeneration: generation.Digest,
	})
	if err != nil || len(routingAfterProgress.RuntimeRules) != 2 {
		t.Fatalf("Routing(after task_01 completed) = %#v, error=%v", routingAfterProgress, err)
	}
	firstCoreID := first.DeliveryRunID + "_core"
	firstLauncher, firstCore := launcherAndCoreRunDetails(scope.WorkspaceID, first.DeliveryRunID, "failed", "failed", firstRequest, []deliveryOutput{{
		NodeID: "implement", Status: "failed", ChildLoopRunID: "implement_attempt_1",
	}})
	client.statuses[first.DeliveryRunID] = firstLauncher
	client.statuses[firstCoreID] = firstCore
	client.statuses["implement_attempt_1"] = childRunDetail(
		scope.WorkspaceID, "implement_attempt_1", "implement-tasks", "failed", 1_200,
		[]deliveryOutput{{NodeID: "execute_task", ItemIndex: 0, Status: "succeeded"}, {NodeID: "execute_task", ItemIndex: 1, Status: "failed"}},
	)
	settled, err := service.Reconcile(ctx, scope, deliveryID, first.DeliveryRunID)
	if err != nil {
		t.Fatalf("Reconcile(attempt 1) error = %v", err)
	}
	if !settled.Recoverable || settled.State != "active" || settled.TokensUsed != 1_200 {
		t.Fatalf("attempt 1 settlement = %#v", settled)
	}
	if settled.DeliveryRunID != first.DeliveryRunID || settled.DeliveryRunID == firstCoreID {
		t.Fatalf("attempt 1 exposed core identity: start=%#v settlement=%#v", first, settled)
	}

	second, err := service.Recover(ctx, scope, deliveryID, first.DeliveryRunID)
	if err != nil {
		t.Fatalf("Recover(attempt 2) error = %v", err)
	}
	secondReplay, err := service.Recover(ctx, scope, deliveryID, first.DeliveryRunID)
	if err != nil {
		t.Fatalf("Recover(attempt 2 replay) error = %v", err)
	}
	if second.DeliveryRunID == first.DeliveryRunID || secondReplay.DeliveryRunID != second.DeliveryRunID ||
		!secondReplay.Replayed || client.startCalls != 2 {
		t.Fatalf("attempt 2 replay = %#v / %#v, starts=%d", second, secondReplay, client.startCalls)
	}
	secondRequest := client.requests[2]
	if len(secondRequestRules(store, scope.WorkspaceID, deliveryID)) != 1 {
		t.Fatalf("attempt 2 did not carry only the incomplete task")
	}

	repository.completeTask(t, "task_02", "frontend", "medium", "frontend fallback completed")
	repository.write(t, "frontend.txt", "frontend complete\n")
	repository.commit(t, "feat: complete frontend fallback")
	finalHead := repository.head(t)
	if finalHead == backendHead {
		t.Fatal("frontend fallback did not create a distinct commit")
	}
	secondCoreID := second.DeliveryRunID + "_core"
	secondLauncher, secondCore := launcherAndCoreRunDetails(scope.WorkspaceID, second.DeliveryRunID, "done", "done", secondRequest, []deliveryOutput{
		{NodeID: "implement", Status: "succeeded", ChildLoopRunID: "implement_attempt_2"},
		{NodeID: "review", Status: "succeeded", ChildLoopRunID: "review_attempt_2"},
		{NodeID: "publish", Status: "succeeded"},
	})
	client.statuses[second.DeliveryRunID] = secondLauncher
	client.statuses[secondCoreID] = secondCore
	client.statuses["implement_attempt_2"] = childRunDetail(scope.WorkspaceID, "implement_attempt_2", "implement-tasks", "done", 800, nil)
	client.statuses["review_attempt_2"] = childRunDetail(scope.WorkspaceID, "review_attempt_2", "review-and-fix", "done", 300, nil)
	finished, err := service.Reconcile(ctx, scope, deliveryID, second.DeliveryRunID)
	if err != nil {
		t.Fatalf("Reconcile(attempt 2) error = %v", err)
	}
	if finished.State != "done" || finished.Recoverable || finished.TokensUsed != 2_300 {
		t.Fatalf("attempt 2 settlement = %#v", finished)
	}
	if finished.DeliveryRunID != second.DeliveryRunID || finished.DeliveryRunID == secondCoreID {
		t.Fatalf("attempt 2 exposed core identity: start=%#v settlement=%#v", second, finished)
	}

	journal, exists, err := store.Load(scope.WorkspaceID)
	if err != nil || !exists {
		t.Fatalf("Load(journal) exists=%v error=%v", exists, err)
	}
	delivery := journal.Deliveries[deliveryID]
	assertFinalDeliveryJournal(t, delivery, generation.Digest, first.DeliveryRunID, second.DeliveryRunID)
	for _, attempt := range delivery.Attempts {
		if attempt.RunID == firstCoreID || attempt.RunID == secondCoreID {
			t.Fatalf("journal exposed core identity: %#v", delivery.Attempts)
		}
	}

	forge := &integrationWorktreeClient{t: t, repository: repository, git: git}
	planner := publication.PublicationPlanner{Compozy: forge, Git: git}
	publisher := publication.Publisher{Planner: planner, Compozy: forge, Git: git, PollInterval: time.Millisecond}
	published, err := publisher.Publish(ctx, scope, publication.PublishInput{
		WorktreeRef: "worktree_delivery_lab", ExpectedHeadSHA: finalHead,
	})
	if err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	if published.Status != publication.PublishStatusPublished || published.HeadSHA != finalHead ||
		published.PRURL != forge.prURL || !slices.Equal(published.OperationIDs, []string{"op_push", "op_pr"}) {
		t.Fatalf("Publish() = %#v", published)
	}
	verified, err := (publication.Verifier{Planner: planner, Git: git}).Verify(ctx, scope, publication.VerifyInput{
		WorktreeRef: "worktree_delivery_lab", ExpectedHeadSHA: finalHead, PublisherResult: published,
	})
	if err != nil || !verified.Verified || verified.HeadSHA != finalHead || verified.PRURL != forge.prURL {
		t.Fatalf("Verify() = %#v, error=%v", verified, err)
	}
	remoteHead, err := git.UpstreamHead(ctx, repository.root)
	if err != nil || remoteHead != finalHead || forge.pushCalls != 1 || forge.openPRCalls != 1 {
		t.Fatalf("remote evidence head=%q error=%v push=%d pr=%d", remoteHead, err, forge.pushCalls, forge.openPRCalls)
	}
}

func migrationFreePlanInput() RoutingPlanInput {
	return RoutingPlanInput{
		Slug: "delivery-demo",
		Proposals: []routing.ClassificationProposal{
			{TaskID: "task_01", Domain: routing.DomainBackend, Complexity: routing.ComplexityLow, Confidence: 0.99},
			{TaskID: "task_02", Domain: routing.DomainFrontend, Complexity: routing.ComplexityMedium, Confidence: 0.99, Dependencies: []string{"task_01"}},
		},
		Fit: []RoutingFitProposal{
			{
				TaskIDs: []string{"task_01"}, Domain: routing.DomainBackend, Complexity: routing.ComplexityLow,
				Candidates: []routing.FitCandidate{
					{ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "gpt-5.6-luna", Score: 0.99},
					{ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "gpt-5.6-terra", Score: 0.70},
				},
			},
			{
				TaskIDs: []string{"task_02"}, Domain: routing.DomainFrontend, Complexity: routing.ComplexityMedium,
				Candidates: []routing.FitCandidate{
					{ExecutorID: inventory.ExecutorCursorAgent, ProviderID: "cursor", ModelID: integrationCursorModel, Score: 0.99},
					{ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "gpt-5.6-terra", Score: 0.90},
				},
			},
		},
	}
}

func migrationFreeInventory(t *testing.T) inventory.InventorySnapshot {
	t.Helper()
	snapshot, err := inventory.NewSnapshot("catalog-delivery-lab", []inventory.ExecutorSnapshot{
		{
			ID: inventory.ExecutorCompozy, Availability: inventory.AvailabilityAvailable,
			Health: inventory.Evidence{Name: "health", State: inventory.ResolutionResolved},
			Capabilities: []inventory.Evidence{{
				Name: "models", State: inventory.ResolutionResolved, Digest: "catalog-delivery-lab",
				Identifiers: []string{"codex/gpt-5.6-luna", "codex/gpt-5.6-terra", "cursor/" + integrationCursorModel},
			}},
		},
		{
			ID: inventory.ExecutorCodex, Availability: inventory.AvailabilityAvailable,
			Health:          inventory.Evidence{Name: "health", State: inventory.ResolutionResolved},
			Capabilities:    []inventory.Evidence{{Name: "models", State: inventory.ResolutionResolved, Identifiers: []string{"codex/gpt-5.6-luna", "codex/gpt-5.6-terra"}}},
			CredentialState: inventory.CredentialConfigured,
		},
		{
			ID: inventory.ExecutorCursorAgent, Availability: inventory.AvailabilityAvailable,
			Health:          inventory.Evidence{Name: "health", State: inventory.ResolutionResolved},
			Capabilities:    []inventory.Evidence{{Name: "models", State: inventory.ResolutionResolved, Identifiers: []string{"cursor/cursor-grok-4.6-high"}}},
			CredentialState: inventory.CredentialConfigured,
		},
	})
	if err != nil {
		t.Fatalf("NewSnapshot() error = %v", err)
	}
	return snapshot
}

func assertMigrationFreeRouting(t *testing.T, generation routing.RoutingGeneration) {
	t.Helper()
	if len(generation.Cells) != 2 {
		t.Fatalf("routing cells = %#v", generation.Cells)
	}
	byDomain := map[routing.Domain]routing.RoutingCell{}
	for _, cell := range generation.Cells {
		byDomain[cell.Domain] = cell
	}
	backend, frontend := byDomain[routing.DomainBackend], byDomain[routing.DomainFrontend]
	if backend.Selected.ProviderID != "codex" || backend.Selected.ModelID != "gpt-5.6-luna" {
		t.Fatalf("backend route = %#v", backend)
	}
	if frontend.Selected.ExecutorID != inventory.ExecutorCompozy || frontend.Selected.ProviderID != "cursor" ||
		frontend.Selected.ModelID != integrationCursorModel || len(frontend.Fallbacks) == 0 ||
		frontend.Fallbacks[0].ProviderID != "codex" || frontend.Fallbacks[0].ModelID != "gpt-5.6-terra" {
		t.Fatalf("frontend route = %#v", frontend)
	}
}

func secondRequestRules(store *routing.OwnershipStore, workspaceID, deliveryID string) []routing.RuntimeRule {
	journal, exists, err := store.Load(workspaceID)
	if err != nil || !exists {
		return nil
	}
	delivery := journal.Deliveries[deliveryID]
	if len(delivery.Attempts) != 2 {
		return nil
	}
	return delivery.Attempts[1].RuntimeRules
}

func assertFinalDeliveryJournal(t *testing.T, delivery routing.DeliveryRecord, generation, firstRun, secondRun string) {
	t.Helper()
	if delivery.State != routing.DeliveryStateDone || delivery.RoutingGenerationDigest != generation || len(delivery.Attempts) != 2 {
		t.Fatalf("delivery = %#v", delivery)
	}
	first, second := delivery.Attempts[0], delivery.Attempts[1]
	if first.RunID != firstRun || second.RunID != secondRun || first.RunID == second.RunID ||
		!slices.Equal(first.ChildRunIDs, []string{"implement_attempt_1"}) ||
		!slices.Equal(second.ChildRunIDs, []string{"implement_attempt_2", "review_attempt_2"}) ||
		!slices.Equal(first.FailedTaskIDs, []string{"task_02"}) {
		t.Fatalf("attempts = %#v", delivery.Attempts)
	}
	if len(first.RuntimeRules) != 2 || len(second.RuntimeRules) != 1 || second.RuntimeRules[0].Match.ID != "task_02" ||
		second.RuntimeRules[0].Runtime.Provider != "codex" || second.RuntimeRules[0].Runtime.Model != "gpt-5.6-terra" {
		t.Fatalf("runtime rules = first:%#v second:%#v", first.RuntimeRules, second.RuntimeRules)
	}
}

type integrationDeliveryClient struct {
	now        time.Time
	startCalls int
	requests   map[int]deliveryStartRequest
	statuses   map[string]deliveryRunDetail
}

func (c *integrationDeliveryClient) Status(_ context.Context, _ string, runID string) (deliveryRunDetail, error) {
	detail, exists := c.statuses[runID]
	if !exists {
		return deliveryRunDetail{}, fmt.Errorf("missing integration status for %s", runID)
	}
	return detail, nil
}

func (c *integrationDeliveryClient) Recent(context.Context, string, int) ([]deliveryRun, error) {
	return nil, nil
}

func (c *integrationDeliveryClient) Start(_ context.Context, workspaceID string, request deliveryStartRequest) (deliveryRun, error) {
	c.startCalls++
	if c.requests == nil {
		c.requests = map[int]deliveryStartRequest{}
	}
	c.requests[request.Attempt] = request
	return deliveryRun{
		ID: fmt.Sprintf("delivery_attempt_%d", request.Attempt), WorkspaceID: workspaceID,
		LoopName: "batuta-deliver", Status: "queued", CreatedAt: c.now, Inputs: deliveryInputs(request),
	}, nil
}

func launcherAndCoreRunDetails(
	workspaceID string,
	launcherID string,
	launcherStatus string,
	coreStatus string,
	request deliveryStartRequest,
	outputs []deliveryOutput,
) (deliveryRunDetail, deliveryRunDetail) {
	coreID := launcherID + "_core"
	launcher := deliveryRunDetail{
		Run: deliveryRun{
			ID: launcherID, WorkspaceID: workspaceID, LoopName: "batuta-deliver", Status: launcherStatus,
			CreatedAt: request.AbsoluteDeadline.Add(-4 * time.Hour), StartedAt: request.AbsoluteDeadline.Add(-4 * time.Hour),
			Inputs: deliveryInputs(request),
		},
		Generations: []deliveryGeneration{{Generation: 1, Outputs: []deliveryOutput{{
			NodeID: "delivery_core", Status: launcherOutputStatus(coreStatus), ChildLoopRunID: coreID,
		}}}},
	}
	core := deliveryRunDetail{
		Run: deliveryRun{
			ID: coreID, WorkspaceID: workspaceID, ParentLoopRunID: launcherID,
			LoopName: "batuta-deliver-core", Status: coreStatus,
			CreatedAt: request.AbsoluteDeadline.Add(-4 * time.Hour), StartedAt: request.AbsoluteDeadline.Add(-4 * time.Hour),
			Inputs: deliveryInputs(request),
		},
		Generations: []deliveryGeneration{{Generation: 1, Outputs: outputs}},
	}
	return launcher, core
}

func childRunDetail(workspaceID, runID, loopName, status string, tokens int64, outputs []deliveryOutput) deliveryRunDetail {
	now := time.Date(2026, time.August, 27, 12, 0, 0, 0, time.UTC)
	return deliveryRunDetail{
		Run: deliveryRun{
			ID: runID, WorkspaceID: workspaceID, LoopName: loopName, Status: status,
			CreatedAt: now, StartedAt: now, TokensUsed: tokens, Inputs: map[string]any{"slug": "delivery-demo"},
		},
		Generations: []deliveryGeneration{{Generation: 1, Outputs: outputs}},
	}
}

type migrationFreeRepository struct {
	root   string
	remote string
	git    string
	branch string
}

func newMigrationFreeRepository(t *testing.T) *migrationFreeRepository {
	t.Helper()
	git, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git executable is unavailable")
	}
	git, err = filepath.Abs(git)
	if err != nil {
		t.Fatalf("absolute git path: %v", err)
	}
	repository := &migrationFreeRepository{
		root: filepath.Join(t.TempDir(), "workspace"), remote: filepath.Join(t.TempDir(), "remote.git"),
		git: filepath.Clean(git), branch: "batuta/delivery-demo",
	}
	repository.run(t, "", "init", "--bare", repository.remote)
	repository.run(t, "", "init", "-b", "main", repository.root)
	repository.run(t, repository.root, "config", "user.email", "batuta@example.invalid")
	repository.run(t, repository.root, "config", "user.name", "Batuta Integration")
	repository.write(t, "README.md", "# Delivery lab\n")
	repository.writeTask(t, "task_01", "pending", "backend", "low", "backend pending")
	repository.writeTask(t, "task_02", "pending", "frontend", "medium", "frontend pending")
	repository.write(t, ".compozy/tasks/delivery-demo/_tasks.md", `---
schema_version: "compozy.tasks/v2"
workflow: delivery-demo
graph:
  nodes:
    - id: task_01
      file: task_01.md
    - id: task_02
      file: task_02.md
  edges:
    - from: task_01
      to: task_02
---

# Tasks
`)
	repository.commit(t, "chore: initialize delivery lab")
	repository.run(t, repository.root, "remote", "add", "origin", repository.remote)
	repository.run(t, repository.root, "push", "-u", "origin", "main")
	repository.run(t, repository.root, "switch", "-c", repository.branch)
	return repository
}

func (r *migrationFreeRepository) writeTask(t *testing.T, id, status, domain, complexity, body string) {
	t.Helper()
	r.write(t, filepath.Join(".compozy", "tasks", "delivery-demo", id+".md"), fmt.Sprintf(
		"---\nstatus: %s\ntitle: %s integration task\ntype: %s\ncomplexity: %s\n---\n\n%s\n",
		status, domain, domain, complexity, body,
	))
}

func (r *migrationFreeRepository) completeTask(t *testing.T, id, domain, complexity, body string) {
	t.Helper()
	r.writeTask(t, id, "completed", domain, complexity, body)
}

func (r *migrationFreeRepository) write(t *testing.T, relative, content string) {
	t.Helper()
	path := filepath.Join(r.root, relative)
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatalf("mkdir %s: %v", relative, err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatalf("write %s: %v", relative, err)
	}
}

func (r *migrationFreeRepository) commit(t *testing.T, message string) {
	t.Helper()
	r.run(t, r.root, "add", "--all")
	r.run(t, r.root, "commit", "-m", message)
}

func (r *migrationFreeRepository) head(t *testing.T) string {
	t.Helper()
	return strings.TrimSpace(r.run(t, r.root, "rev-parse", "HEAD"))
}

func (r *migrationFreeRepository) run(t *testing.T, directory string, args ...string) string {
	t.Helper()
	command := exec.Command(r.git, args...)
	command.Dir = directory
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v in %q: %v\n%s", args, directory, err, output)
	}
	return string(output)
}

type integrationWorktreeClient struct {
	t           *testing.T
	repository  *migrationFreeRepository
	git         publication.GitClient
	pushCalls   int
	openPRCalls int
	prURL       string
}

func (c *integrationWorktreeClient) Inspect(ctx context.Context, scope publication.TrustedScope, ref string) (publication.WorktreeInspection, error) {
	snapshot, err := c.git.Snapshot(ctx, c.repository.root)
	if err != nil {
		return publication.WorktreeInspection{}, err
	}
	dirty := 0
	if !snapshot.Clean {
		dirty = 1
	}
	hasUpstream := c.pushCalls > 0
	ahead, behind := 0, 0
	aheadOfBase, err := c.git.CommitsAheadOfBase(ctx, c.repository.root, "main")
	if err != nil {
		return publication.WorktreeInspection{}, err
	}
	status := &publication.WorktreeStatus{
		Branch: &snapshot.Branch, Detached: &snapshot.Detached, HeadSHA: &snapshot.HeadSHA,
		DirtyFiles: &dirty, HasUpstream: &hasUpstream, Ahead: &ahead, Behind: &behind,
	}
	if !hasUpstream {
		status.AheadOfBase = &aheadOfBase
	}
	return publication.WorktreeInspection{
		Worktree: publication.Worktree{
			ID: ref, WorkspaceID: scope.WorkspaceID, Branch: snapshot.Branch,
			Path: c.repository.root, State: "ready", BaseRef: "main",
		},
		Status: status, Forge: &publication.ForgeStatus{Provider: "github"},
		Repo: publication.WorktreeRepo{GitBacked: true, GitAvailable: true},
	}, nil
}

func (c *integrationWorktreeClient) ExitPlan(context.Context, publication.TrustedScope, string) (publication.ExitPlan, error) {
	plan := publication.ExitPlan{
		WorktreeID: "worktree_delivery_lab", Base: "main",
		Forge:     &publication.ForgeCapabilities{Provider: "github", DefaultBranch: "main"},
		PRPrefill: &publication.PRPrefill{Title: "Delivery demo", Body: "Automated Batuta integration proof"},
	}
	switch {
	case c.prURL != "":
		plan.Actions = []publication.ExitAction{{Action: "view_pr", Enabled: true, URL: c.prURL}}
		plan.ForgeStatus = &publication.ForgeStatus{Provider: "github", PRURL: c.prURL}
	case c.pushCalls > 0:
		plan.Actions = []publication.ExitAction{{Action: "open_pr", Enabled: true, Publish: true}}
	default:
		plan.Actions = []publication.ExitAction{{Action: "push", Enabled: true, Publish: true}}
	}
	return plan, nil
}

func (c *integrationWorktreeClient) Push(_ context.Context, _ publication.TrustedScope, _ string) (publication.Operation, error) {
	c.repository.run(c.t, c.repository.root, "push", "-u", "origin", "HEAD")
	c.pushCalls++
	return publication.Operation{OperationID: "op_push"}, nil
}

func (c *integrationWorktreeClient) OpenPR(context.Context, publication.TrustedScope, string, publication.PRPrefill, string) (publication.Operation, error) {
	c.openPRCalls++
	c.prURL = "https://github.com/example/delivery-lab/pull/42"
	return publication.Operation{OperationID: "op_pr"}, nil
}
