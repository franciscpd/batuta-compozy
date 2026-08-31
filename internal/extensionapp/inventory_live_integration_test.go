//go:build integration

package extensionapp

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"testing"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/franciscpd/batuta-compozy/internal/inventory/adapters"
	"github.com/franciscpd/batuta-compozy/internal/publication"
	"github.com/franciscpd/batuta-compozy/internal/routing"
)

func TestLiveInventorySnapshotIsStableWithoutExecutorChangesIntegration(t *testing.T) {
	compozyExecutable := os.Getenv("BATUTA_LIVE_COMPOZY")
	workspaceID := os.Getenv("BATUTA_LIVE_WORKSPACE_ID")
	workspaceRoot := os.Getenv("BATUTA_LIVE_WORKSPACE_ROOT")
	if compozyExecutable == "" || workspaceID == "" || workspaceRoot == "" {
		t.Skip("live Batuta inventory environment is not configured")
	}
	paths := inventoryExecutables{
		Compozy: compozyExecutable, Codex: liveExecutable("codex"),
		OpenCode: liveExecutable("opencode"), Cursor: liveExecutable("agent"),
	}
	collect := func() inventory.InventorySnapshot {
		collector, err := adapters.NewCollector(publication.ExecRunner{}, adapters.CollectorOptions{
			TrustedWorkspace: filepath.Clean(workspaceRoot), WorkspaceID: workspaceID,
			CompozyExecutable: paths.Compozy, CodexExecutable: paths.Codex,
			OpenCodeExecutable: paths.OpenCode, CursorExecutable: paths.Cursor,
			ProbeParallelism: 16,
		})
		if err != nil {
			t.Fatalf("NewCollector() error = %v", err)
		}
		snapshot, err := collector.Collect(context.Background())
		if err != nil {
			t.Fatalf("Collect() error = %v", err)
		}
		return snapshot
	}
	first, second := collect(), collect()
	if first.Digest != second.Digest {
		firstJSON, _ := json.Marshal(first)
		secondJSON, _ := json.Marshal(second)
		t.Fatalf("stable inventory digest = %q then %q\nfirst=%s\nsecond=%s", first.Digest, second.Digest, firstJSON, secondJSON)
	}
}

func TestLiveInventoryPromotesExactCodexTerraCatalogPairIntegration(t *testing.T) {
	compozyExecutable := os.Getenv("BATUTA_LIVE_COMPOZY")
	workspaceID := os.Getenv("BATUTA_LIVE_WORKSPACE_ID")
	workspaceRoot := os.Getenv("BATUTA_LIVE_WORKSPACE_ROOT")
	codexExecutable := liveExecutable("codex")
	if compozyExecutable == "" || workspaceID == "" || workspaceRoot == "" || codexExecutable == "" {
		t.Skip("live Batuta Codex inventory environment is not configured")
	}
	collector, err := adapters.NewCollector(publication.ExecRunner{}, adapters.CollectorOptions{
		TrustedWorkspace: filepath.Clean(workspaceRoot), WorkspaceID: workspaceID,
		CompozyExecutable: compozyExecutable, CodexExecutable: codexExecutable,
		OpenCodeExecutable: liveExecutable("opencode"), CursorExecutable: liveExecutable("agent"),
		ProbeParallelism: 16,
	})
	if err != nil {
		t.Fatalf("NewCollector() error = %v", err)
	}
	snapshot, err := collector.Collect(context.Background())
	if err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	catalog, err := liveCatalogFromInventory(snapshot)
	if err != nil {
		t.Fatalf("liveCatalogFromInventory() error = %v", err)
	}
	bindings, err := routing.BuildCandidateBindings(snapshot, catalog)
	if err != nil {
		t.Fatalf("BuildCandidateBindings() error = %v", err)
	}
	for _, binding := range bindings {
		if binding.ExecutorID == inventory.ExecutorCompozy && binding.ProviderID == "codex" &&
			binding.ModelID == "gpt-5.6-terra" && slices.Contains(binding.EnrichmentIDs, inventory.ExecutorCodex) {
			return
		}
	}
	t.Fatalf("bindings = %#v, want executable Compozy codex/gpt-5.6-terra with Codex adapter proof", bindings)
}

func TestLiveRoutingPlanSelectsDistinctDomainComplexityCellsIntegration(t *testing.T) {
	compozyExecutable := os.Getenv("BATUTA_LIVE_COMPOZY")
	workspaceID := os.Getenv("BATUTA_LIVE_WORKSPACE_ID")
	workspaceRoot := os.Getenv("BATUTA_LIVE_WORKSPACE_ROOT")
	if compozyExecutable == "" || workspaceID == "" || workspaceRoot == "" {
		t.Skip("live Batuta routing environment is not configured")
	}
	slug := installLiveRoutingFixture(t, workspaceRoot)
	var lastSnapshot inventory.InventorySnapshot
	engine := routingEngine{inventory: func(ctx context.Context, _ publication.TrustedScope) (inventory.InventorySnapshot, error) {
		collector, err := adapters.NewCollector(publication.ExecRunner{}, adapters.CollectorOptions{
			TrustedWorkspace: workspaceRoot, WorkspaceID: workspaceID,
			CompozyExecutable: compozyExecutable, CodexExecutable: liveExecutable("codex"),
			OpenCodeExecutable: liveExecutable("opencode"), CursorExecutable: liveExecutable("agent"),
			ProbeParallelism: 16,
		})
		if err != nil {
			return inventory.InventorySnapshot{}, err
		}
		snapshot, collectErr := collector.Collect(ctx)
		lastSnapshot = snapshot
		return snapshot, collectErr
	}}
	input := RoutingPlanInput{
		Slug: slug,
		Proposals: []routing.ClassificationProposal{
			{TaskID: "task_01", Domain: routing.DomainBackend, Complexity: routing.ComplexityLow, Confidence: 0.95},
			{TaskID: "task_02", Domain: routing.DomainFrontend, Complexity: routing.ComplexityMedium, Confidence: 0.95, Dependencies: []string{"task_01"}},
		},
		Fit: []RoutingFitProposal{
			{
				TaskIDs: []string{"task_01"}, Domain: routing.DomainBackend, Complexity: routing.ComplexityLow,
				Candidates: []routing.FitCandidate{
					{ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "gpt-5.6-luna", Score: 0.99},
					{ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "gpt-5.6-terra", Score: 0.80},
				},
			},
			{
				TaskIDs: []string{"task_02"}, Domain: routing.DomainFrontend, Complexity: routing.ComplexityMedium,
				Candidates: []routing.FitCandidate{
					{ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "gpt-5.6-terra", Score: 0.99},
					{ExecutorID: inventory.ExecutorCodex, ProviderID: "codex", ModelID: "gpt-5.6-luna", Score: 0.80},
				},
			},
		},
	}
	generation, err := engine.Plan(context.Background(), publication.TrustedScope{WorkspaceID: workspaceID, WorkspaceRoot: workspaceRoot}, input)
	if err != nil {
		catalog, _ := liveCatalogFromInventory(lastSnapshot)
		bindings, _ := routing.BuildCandidateBindings(lastSnapshot, catalog)
		var catalogRelevant []routing.CatalogModel
		for _, model := range catalog.Models {
			if model.ModelID == "gpt-5.6-luna" || model.ModelID == "gpt-5.6-terra" {
				catalogRelevant = append(catalogRelevant, model)
			}
		}
		var relevant []routing.CandidateBinding
		for _, binding := range bindings {
			if binding.ModelID == "gpt-5.6-luna" || binding.ModelID == "gpt-5.6-terra" {
				relevant = append(relevant, binding)
			}
		}
		t.Fatalf("Plan() error = %v\nrelevant catalog=%#v\nrelevant bindings=%#v", err, catalogRelevant, relevant)
	}
	if len(generation.Cells) != 2 || generation.Cells[0].Selected.ModelID != "gpt-5.6-luna" ||
		generation.Cells[1].Selected.ProviderID != "cursor" || generation.Cells[1].Selected.ModelID != "grok-4.6[effort=high,fast=true]" {
		t.Fatalf("routing cells = %#v, want backend/low luna and frontend/medium Cursor/Grok", generation.Cells)
	}
}

func installLiveRoutingFixture(t *testing.T, workspaceRoot string) string {
	t.Helper()
	slug := fmt.Sprintf("batuta-live-routing-fixture-%d", os.Getpid())
	tasksRoot := filepath.Join(workspaceRoot, ".compozy", "tasks")
	directory := filepath.Join(tasksRoot, slug)
	if _, err := os.Lstat(directory); !os.IsNotExist(err) {
		t.Fatalf("live routing fixture path is not unused: %s", directory)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		t.Fatalf("create live routing fixture: %v", err)
	}
	t.Cleanup(func() {
		if err := os.RemoveAll(directory); err != nil {
			t.Errorf("remove live routing fixture: %v", err)
		}
		if err := os.Remove(tasksRoot); err != nil && !os.IsNotExist(err) {
			// A non-empty tasks directory belongs to the lab and must be retained.
			if entries, readErr := os.ReadDir(tasksRoot); readErr != nil || len(entries) == 0 {
				t.Errorf("remove empty live routing tasks directory: %v", err)
			}
		}
	})
	files := map[string]string{
		"_tasks.md":  "---\nschema_version: \"compozy.tasks/v2\"\nworkflow: " + slug + "\ngraph:\n  nodes:\n    - id: task_01\n      file: task_01.md\n    - id: task_02\n      file: task_02.md\n  edges:\n    - from: task_01\n      to: task_02\n---\n\n# Tasks\n",
		"task_01.md": "---\nstatus: pending\ntitle: Backend fixture\ntype: backend\ncomplexity: low\n---\n\n# Backend fixture\n",
		"task_02.md": "---\nstatus: pending\ntitle: Frontend fixture\ntype: frontend\ncomplexity: medium\n---\n\n# Frontend fixture\n",
	}
	for name, content := range files {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write live routing fixture %s: %v", name, err)
		}
	}
	return slug
}

func liveExecutable(name string) string {
	path, err := exec.LookPath(name)
	if err != nil {
		return ""
	}
	absolute, err := filepath.Abs(path)
	if err != nil {
		return ""
	}
	return filepath.Clean(absolute)
}
