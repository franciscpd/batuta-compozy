package routing

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"slices"
	"strings"
)

const (
	minimumClassificationConfidence = 0.70
	maxClassificationTasks          = 10_000
	maxCapabilityRequirements       = 32
	maxEvidenceReferences           = 24
	maxCapabilityIDBytes            = 128
	maxEvidenceReferenceBytes       = 512
	maxIndivisibleReasonBytes       = 512
)

var ErrClassificationRetryable = errors.New("routing: classification must be retried")

type CapabilityKind string

const (
	CapabilityLanguageToolchain     CapabilityKind = "language_toolchain"
	CapabilityBrowserTooling        CapabilityKind = "browser_tooling"
	CapabilityMobileTooling         CapabilityKind = "mobile_tooling"
	CapabilityDatabaseAccess        CapabilityKind = "database_access"
	CapabilityInfrastructureCLI     CapabilityKind = "infrastructure_cli"
	CapabilityMCP                   CapabilityKind = "mcp"
	CapabilityRepositoryInstruction CapabilityKind = "repository_instruction"
	CapabilitySandboxWrite          CapabilityKind = "sandbox_write"
	CapabilityTestCommand           CapabilityKind = "test_command"
)

type EvidenceKind string

const (
	EvidenceTaskField           EvidenceKind = "task_field"
	EvidencePath                EvidenceKind = "path"
	EvidenceInstruction         EvidenceKind = "instruction"
	EvidenceAcceptanceCriterion EvidenceKind = "acceptance_criterion"
)

type CapabilityRequirement struct {
	Kind              CapabilityKind `json:"kind"`
	ID                string         `json:"id"`
	Hard              bool           `json:"hard"`
	SecuritySensitive bool           `json:"security_sensitive,omitempty"`
}

type EvidenceReference struct {
	Kind      EvidenceKind `json:"kind"`
	Reference string       `json:"reference"`
}

type ClassificationProposal struct {
	TaskID            string                  `json:"task_id"`
	Domain            Domain                  `json:"domain"`
	Complexity        Complexity              `json:"complexity"`
	Confidence        float64                 `json:"confidence"`
	Requirements      []CapabilityRequirement `json:"requirements"`
	Evidence          []EvidenceReference     `json:"evidence"`
	Dependencies      []string                `json:"dependencies"`
	IndivisibleReason string                  `json:"indivisible_reason,omitempty"`
}

type ClassificationRequest struct {
	Slug      string                   `json:"slug"`
	Proposals []ClassificationProposal `json:"proposals"`
}

type ValidatedTask struct {
	ID                string                  `json:"task_id"`
	Title             string                  `json:"title"`
	Status            string                  `json:"status"`
	Domain            Domain                  `json:"domain"`
	Complexity        Complexity              `json:"complexity"`
	Confidence        float64                 `json:"confidence"`
	Requirements      []CapabilityRequirement `json:"requirements"`
	Evidence          []EvidenceReference     `json:"evidence"`
	Dependencies      []string                `json:"dependencies"`
	IndivisibleReason string                  `json:"indivisible_reason,omitempty"`
	ArtifactDigest    string                  `json:"artifact_digest"`
}

type ValidatedTaskGraph struct {
	Slug          string          `json:"slug"`
	TaskSetDigest string          `json:"task_set_digest"`
	Tasks         []ValidatedTask `json:"tasks"`
}

func DecodeClassificationRequest(payload []byte) (ClassificationRequest, error) {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	var request ClassificationRequest
	if err := decoder.Decode(&request); err != nil {
		return ClassificationRequest{}, fmt.Errorf("routing: invalid classification JSON: %w", err)
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return ClassificationRequest{}, errors.New("routing: classification JSON must contain exactly one object")
	}
	if !canonicalSlug.MatchString(request.Slug) {
		return ClassificationRequest{}, ErrInvalidSlug
	}
	if len(request.Proposals) > maxClassificationTasks {
		return ClassificationRequest{}, fmt.Errorf("%w: too many task proposals", ErrClassificationRetryable)
	}
	return request, nil
}

func ValidateClassification(taskSet TaskSet, proposals []ClassificationProposal) (ValidatedTaskGraph, error) {
	if len(taskSet.Tasks) == 0 || len(taskSet.Tasks) > maxClassificationTasks || len(proposals) != len(taskSet.Tasks) {
		return ValidatedTaskGraph{}, fmt.Errorf("%w: proposal coverage does not match authored tasks", ErrReauthoringRequired)
	}
	authored := make(map[string]TaskArtifact, len(taskSet.Tasks))
	for _, task := range taskSet.Tasks {
		if task.ID == "" || !task.Domain.Valid() || !task.Complexity.Valid() {
			return ValidatedTaskGraph{}, ErrReauthoringRequired
		}
		if _, duplicate := authored[task.ID]; duplicate {
			return ValidatedTaskGraph{}, fmt.Errorf("%w: duplicate authored task ID", ErrReauthoringRequired)
		}
		authored[task.ID] = task
	}

	byID := make(map[string]ClassificationProposal, len(proposals))
	for _, proposal := range proposals {
		task, exists := authored[proposal.TaskID]
		if !exists {
			return ValidatedTaskGraph{}, fmt.Errorf("%w: proposal references an invented task ID", ErrReauthoringRequired)
		}
		if _, duplicate := byID[proposal.TaskID]; duplicate {
			return ValidatedTaskGraph{}, fmt.Errorf("%w: duplicate proposal task ID", ErrReauthoringRequired)
		}
		if !slices.Equal(proposal.Dependencies, task.Dependencies) {
			return ValidatedTaskGraph{}, fmt.Errorf("%w: proposal contradicts authored dependencies", ErrReauthoringRequired)
		}
		if err := validateProposal(proposal, task); err != nil {
			return ValidatedTaskGraph{}, err
		}
		byID[proposal.TaskID] = proposal
	}

	tasks := make([]ValidatedTask, 0, len(taskSet.Tasks))
	for _, artifact := range taskSet.Tasks {
		proposal, exists := byID[artifact.ID]
		if !exists {
			return ValidatedTaskGraph{}, fmt.Errorf("%w: proposal is missing an authored task ID", ErrReauthoringRequired)
		}
		tasks = append(tasks, ValidatedTask{
			ID:                artifact.ID,
			Title:             artifact.Title,
			Status:            artifact.Status,
			Domain:            artifact.Domain,
			Complexity:        artifact.Complexity,
			Confidence:        proposal.Confidence,
			Requirements:      append([]CapabilityRequirement(nil), proposal.Requirements...),
			Evidence:          append([]EvidenceReference(nil), proposal.Evidence...),
			Dependencies:      append([]string(nil), artifact.Dependencies...),
			IndivisibleReason: strings.TrimSpace(proposal.IndivisibleReason),
			ArtifactDigest:    artifact.Digest,
		})
	}
	graph := ValidatedTaskGraph{Slug: taskSet.Slug, TaskSetDigest: taskSet.Digest, Tasks: tasks}
	if err := validateMaterializedGraph(graph); err != nil {
		return ValidatedTaskGraph{}, err
	}
	return graph, nil
}

func validateProposal(proposal ClassificationProposal, artifact TaskArtifact) error {
	if math.IsNaN(proposal.Confidence) || math.IsInf(proposal.Confidence, 0) || proposal.Confidence < minimumClassificationConfidence || proposal.Confidence > 1 {
		return fmt.Errorf("%w: confidence is outside the accepted range", ErrClassificationRetryable)
	}
	if !proposal.Domain.Valid() || !proposal.Complexity.Valid() {
		return fmt.Errorf("%w: proposal taxonomy is unknown", ErrClassificationRetryable)
	}
	if len(proposal.Requirements) > maxCapabilityRequirements {
		return fmt.Errorf("%w: too many capability requirements", ErrClassificationRetryable)
	}
	seenRequirements := make(map[string]struct{}, len(proposal.Requirements))
	for _, requirement := range proposal.Requirements {
		if !requirement.Kind.Valid() || !boundedText(requirement.ID, maxCapabilityIDBytes) {
			return fmt.Errorf("%w: capability requirement is invalid", ErrClassificationRetryable)
		}
		key := string(requirement.Kind) + "\x00" + requirement.ID
		if _, duplicate := seenRequirements[key]; duplicate {
			return fmt.Errorf("%w: duplicate capability requirement", ErrClassificationRetryable)
		}
		seenRequirements[key] = struct{}{}
	}
	if len(proposal.Evidence) > maxEvidenceReferences {
		return fmt.Errorf("%w: too many evidence references", ErrClassificationRetryable)
	}
	for _, evidence := range proposal.Evidence {
		if !evidence.Kind.Valid() || !boundedText(evidence.Reference, maxEvidenceReferenceBytes) {
			return fmt.Errorf("%w: evidence reference is invalid", ErrClassificationRetryable)
		}
	}
	reason := strings.TrimSpace(proposal.IndivisibleReason)
	if len(reason) > maxIndivisibleReasonBytes {
		return fmt.Errorf("%w: indivisible reason is too long", ErrClassificationRetryable)
	}
	if artifact.Domain == DomainFullstack {
		if reason == "" || !hasAcceptanceEvidence(proposal.Evidence) {
			return fmt.Errorf("%w: fullstack task lacks indivisibility evidence", ErrReauthoringRequired)
		}
	} else if reason != "" {
		return fmt.Errorf("%w: indivisibility reason is valid only for fullstack", ErrClassificationRetryable)
	}
	return nil
}

func (k CapabilityKind) Valid() bool {
	switch k {
	case CapabilityLanguageToolchain, CapabilityBrowserTooling, CapabilityMobileTooling, CapabilityDatabaseAccess, CapabilityInfrastructureCLI, CapabilityMCP, CapabilityRepositoryInstruction, CapabilitySandboxWrite, CapabilityTestCommand:
		return true
	default:
		return false
	}
}

func (k EvidenceKind) Valid() bool {
	switch k {
	case EvidenceTaskField, EvidencePath, EvidenceInstruction, EvidenceAcceptanceCriterion:
		return true
	default:
		return false
	}
}

func boundedText(value string, limit int) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && len(trimmed) <= limit && !strings.ContainsRune(trimmed, '\x00')
}

func hasAcceptanceEvidence(evidence []EvidenceReference) bool {
	for _, reference := range evidence {
		if reference.Kind == EvidenceAcceptanceCriterion {
			return true
		}
	}
	return false
}
