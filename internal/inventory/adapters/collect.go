package adapters

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/franciscpd/batuta-compozy/internal/publication"
)

const (
	collectionTimeout         = 60 * time.Second
	collectionProbeLimit      = 64
	collectionRecordLimit     = 10_000
	collectionDiagnosticLimit = 256
)

type CollectorOptions struct {
	TrustedWorkspace   string
	WorkspaceID        string
	CompozyExecutable  string
	CodexExecutable    string
	OpenCodeExecutable string
	CursorExecutable   string
}

type Collector struct {
	runner    publication.CommandRunner
	workspace string
	adapters  []configuredAdapter
	userHome  string
	codexHome string
	fileQuota *inventory.SharedFileBudget
}

type configuredAdapter struct {
	adapter   Adapter
	available bool
	readFiles bool
}

func NewCollector(runner publication.CommandRunner, options CollectorOptions) (*Collector, error) {
	userHome, _ := os.UserHomeDir()
	codexHome := strings.TrimSpace(os.Getenv("CODEX_HOME"))
	if codexHome == "" && userHome != "" {
		codexHome = filepath.Join(userHome, ".codex")
	}
	return newCollectorWithRoots(runner, options, collectorRoots{userHome: userHome, codexHome: codexHome})
}

type collectorRoots struct {
	userHome  string
	codexHome string
}

func newCollectorWithRoots(runner publication.CommandRunner, options CollectorOptions, roots collectorRoots) (*Collector, error) {
	if runner == nil {
		return nil, errors.New("inventory collector: command runner is required")
	}
	if strings.TrimSpace(options.TrustedWorkspace) == "" || !filepath.IsAbs(options.TrustedWorkspace) {
		return nil, errors.New("inventory collector: trusted workspace must be absolute")
	}
	if strings.TrimSpace(options.WorkspaceID) == "" {
		return nil, errors.New("inventory collector: workspace ID is required")
	}

	builders := []struct {
		executable string
		fallback   string
		build      func(string) (Adapter, error)
	}{
		{options.CompozyExecutable, "compozy", func(executable string) (Adapter, error) {
			return NewCompozy(executable, options.WorkspaceID)
		}},
		{options.CodexExecutable, "codex", NewCodex},
		{options.OpenCodeExecutable, "opencode", NewOpenCode},
		{options.CursorExecutable, "agent", NewCursor},
	}
	configured := make([]configuredAdapter, 0, len(builders))
	for _, builder := range builders {
		available := strings.TrimSpace(builder.executable) != ""
		executable := builder.executable
		if !available {
			executable = filepath.Join(string(filepath.Separator), "missing", builder.fallback)
		}
		adapter, err := builder.build(executable)
		if err != nil {
			return nil, err
		}
		configured = append(configured, configuredAdapter{adapter: adapter, available: available, readFiles: regularFile(executable)})
	}
	fileQuota, err := inventory.NewSharedFileBudget(inventory.FileBudget{MaxFiles: 256, MaxBytes: 8 * 1024 * 1024})
	if err != nil {
		return nil, err
	}
	return &Collector{
		runner: runner, workspace: filepath.Clean(options.TrustedWorkspace), adapters: configured,
		userHome: roots.userHome, codexHome: roots.codexHome, fileQuota: fileQuota,
	}, nil
}

func (c *Collector) Collect(ctx context.Context) (inventory.InventorySnapshot, error) {
	collectionCtx, cancel := context.WithTimeout(ctx, collectionTimeout)
	defer cancel()

	executors := make([]inventory.ExecutorSnapshot, 0, len(c.adapters))
	probeCount := 0
	recordCount := 0
	diagnosticCount := 0
	catalogGeneration := ""
	for _, configured := range c.adapters {
		if !configured.available {
			executors = append(executors, configured.adapter.Missing())
			continue
		}

		outputs := make(map[inventory.ProbeID][]byte)
		exhausted := false
		static := configured.adapter.StaticSpecs()
		probeCount, exhausted = c.runSpecs(collectionCtx, static, outputs, probeCount)
		if !exhausted {
			dynamic, err := configured.adapter.DynamicSpecs(outputs)
			if err != nil {
				return inventory.InventorySnapshot{}, fmt.Errorf("inventory collector: expand %s probes: %w", configured.adapter.ID(), err)
			}
			probeCount, exhausted = c.runSpecs(collectionCtx, dynamic, outputs, probeCount)
		}
		executor := configured.adapter.Normalize(outputs)
		if configured.readFiles {
			c.enrichFileEvidence(&executor)
		}
		if exhausted {
			executor.Diagnostics = append(executor.Diagnostics, inventory.Diagnostic{Code: "probe_budget_exhausted"})
		}
		recordCount = enforceRecordBudget(&executor, recordCount)
		diagnosticCount = enforceDiagnosticBudget(&executor, diagnosticCount)
		if configured.adapter.ID() == inventory.ExecutorCompozy {
			for _, capability := range executor.Capabilities {
				if capability.Name == "models" && capability.State == inventory.ResolutionResolved {
					catalogGeneration = capability.Digest
					break
				}
			}
		}
		executors = append(executors, executor)
	}
	return inventory.NewSnapshot(catalogGeneration, executors)
}

func regularFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && info.Mode().IsRegular()
}

func (c *Collector) enrichFileEvidence(executor *inventory.ExecutorSnapshot) {
	switch executor.ID {
	case inventory.ExecutorCodex:
		c.enrichCodexFiles(executor)
	case inventory.ExecutorCursorAgent:
		c.enrichCursorFiles(executor)
	}
}

func (c *Collector) enrichCodexFiles(executor *inventory.ExecutorSnapshot) {
	if filepath.IsAbs(c.codexHome) {
		if reader, err := inventory.NewFileReaderWithSharedBudget(c.codexHome, c.fileQuota); err == nil {
			for _, pattern := range []string{"config.toml", "*.config.toml"} {
				records, readErr := reader.ReadMatches(pattern)
				if readErr != nil {
					c.recordFileError(executor, readErr)
					continue
				}
				for _, record := range records {
					projection, parseErr := ParseCodexConfig(record.Content)
					if parseErr != nil {
						executor.Diagnostics = append(executor.Diagnostics, inventory.Diagnostic{Code: "malformed_config"})
						continue
					}
					projection.Source = "CODEX_HOME/" + record.Name
					executor.ConfigurationDigests = append(executor.ConfigurationDigests, projection)
				}
			}
		}
	}
	if reader, err := inventory.NewFileReaderWithSharedBudget(c.workspace, c.fileQuota); err == nil {
		records, readErr := reader.ReadMatches("AGENTS.md")
		if readErr != nil {
			c.recordFileError(executor, readErr)
			return
		}
		for _, record := range records {
			executor.InstructionDigests = append(executor.InstructionDigests, evidence(
				"instructions", record.Name, inventory.ResolutionDeclared, record.Content, nil,
			))
		}
	}
}

func (c *Collector) enrichCursorFiles(executor *inventory.ExecutorSnapshot) {
	if reader, err := inventory.NewFileReaderWithSharedBudget(c.workspace, c.fileQuota); err == nil {
		for _, pattern := range []string{filepath.Join(".cursor", "mcp.json"), filepath.Join(".cursor", "rules", "*")} {
			records, readErr := reader.ReadMatches(pattern)
			if readErr != nil {
				c.recordFileError(executor, readErr)
				continue
			}
			for _, record := range records {
				name := "config"
				if strings.Contains(record.Name, "/rules/") {
					name = "instructions"
				}
				target := &executor.ConfigurationDigests
				if name == "instructions" {
					target = &executor.InstructionDigests
				}
				*target = append(*target, evidence(name, record.Name, inventory.ResolutionDeclared, record.Content, nil))
			}
		}
	}
	if filepath.IsAbs(c.userHome) {
		if reader, err := inventory.NewFileReaderWithSharedBudget(c.userHome, c.fileQuota); err == nil {
			records, readErr := reader.ReadMatches(filepath.Join(".cursor", "mcp.json"))
			if readErr != nil {
				c.recordFileError(executor, readErr)
				return
			}
			for _, record := range records {
				executor.ConfigurationDigests = append(executor.ConfigurationDigests, evidence("config", record.Name, inventory.ResolutionDeclared, record.Content, nil))
			}
		}
	}
}

func (c *Collector) recordFileError(executor *inventory.ExecutorSnapshot, err error) {
	code := "file_unavailable"
	if errors.Is(err, inventory.ErrFileBudgetExceeded) {
		code = "file_budget_exhausted"
	} else if errors.Is(err, inventory.ErrFileOutsideTrustedRoot) {
		code = "file_outside_trusted_root"
	}
	executor.Diagnostics = append(executor.Diagnostics, inventory.Diagnostic{Code: code})
	for i := range executor.Capabilities {
		if executor.Capabilities[i].Name == "config" || executor.Capabilities[i].Name == "instructions" {
			executor.Capabilities[i] = unknownEvidence(executor.Capabilities[i].Name, executor.Capabilities[i].Source, code)
		}
	}
}

func enforceRecordBudget(executor *inventory.ExecutorSnapshot, used int) int {
	groups := [][]inventory.Evidence{
		{executor.Version},
		{executor.Health},
		executor.ConfigurationDigests,
		executor.InstructionDigests,
		executor.Capabilities,
	}
	exhausted := false
	for groupIndex := range groups {
		for evidenceIndex := range groups[groupIndex] {
			current := &groups[groupIndex][evidenceIndex]
			if current.Name == "" {
				continue
			}
			cost := len(current.Identifiers)
			if cost == 0 {
				cost = 1
			}
			if used+cost > collectionRecordLimit {
				*current = unknownEvidence(current.Name, current.Source, "record_budget_exhausted")
				exhausted = true
				continue
			}
			used += cost
		}
	}
	executor.Version = groups[0][0]
	executor.Health = groups[1][0]
	if exhausted {
		executor.Diagnostics = append(executor.Diagnostics, inventory.Diagnostic{Code: "record_budget_exhausted"})
	}
	return used
}

func enforceDiagnosticBudget(executor *inventory.ExecutorSnapshot, used int) int {
	remaining := collectionDiagnosticLimit - used
	if remaining <= 0 {
		executor.Diagnostics = nil
		return used
	}
	if len(executor.Diagnostics) > remaining {
		executor.Diagnostics = executor.Diagnostics[:remaining]
	}
	return used + len(executor.Diagnostics)
}

func (c *Collector) runSpecs(ctx context.Context, specs []inventory.ProbeSpec, outputs map[inventory.ProbeID][]byte, count int) (int, bool) {
	if len(specs) == 0 {
		return count, false
	}
	runner, err := inventory.NewProbeRunner(c.runner, c.workspace, specs)
	if err != nil {
		return count, false
	}
	for _, spec := range specs {
		if count >= collectionProbeLimit {
			return count, true
		}
		count++
		result, err := runner.Run(ctx, spec.ID)
		if err != nil {
			continue
		}
		outputs[spec.ID] = result.Output
	}
	return count, false
}
