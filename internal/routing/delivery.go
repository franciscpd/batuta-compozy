package routing

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"time"
)

const (
	deliveryAttemptCeiling = 4
	// DeliveryTokenCeiling bounds the tokens CompozyOS reports for every child run
	// of one delivery. CompozyOS sums the context each provider turn reports
	// (Claude ACP reports context size only), so a single medium task spends
	// several million tokens under this accounting.
	DeliveryTokenCeiling = int64(100_000_000)
)

var (
	ErrDeliveryConflict          = errors.New("routing: delivery journal conflict")
	ErrInvalidDeliveryTransition = errors.New("routing: invalid delivery transition")

	canonicalSHA256  = regexp.MustCompile(`^sha256:[0-9a-f]{64}$`)
	canonicalHexHash = regexp.MustCompile(`^[0-9a-f]{64}$`)
	canonicalGitSHA  = regexp.MustCompile(`^(?:[0-9a-f]{40}|[0-9a-f]{64})$`)
)

type DeliveryState string

const (
	DeliveryStateActive    DeliveryState = "active"
	DeliveryStateDone      DeliveryState = "done"
	DeliveryStateBlocked   DeliveryState = "blocked"
	DeliveryStateExhausted DeliveryState = "exhausted"
)

type AttemptState string

const (
	AttemptPlanned   AttemptState = "planned"
	AttemptSubmitted AttemptState = "submitted"
	AttemptTerminal  AttemptState = "terminal"
)

type WorktreeFingerprint struct {
	HeadSHA         string `json:"head_sha"`
	PorcelainSHA256 string `json:"porcelain_sha256"`
	ContentSHA256   string `json:"content_sha256"`
}

type DeliveryAttempt struct {
	Attempt             int                  `json:"attempt"`
	OperationID         string               `json:"operation_id"`
	RequestDigest       string               `json:"request_digest"`
	RuntimeRules        []RuntimeRule        `json:"runtime_rules"`
	State               AttemptState         `json:"state"`
	RunID               string               `json:"run_id,omitempty"`
	ChildRunIDs         []string             `json:"child_run_ids,omitempty"`
	FailedTaskIDs       []string             `json:"failed_task_ids,omitempty"`
	PlannedAt           time.Time            `json:"planned_at"`
	StartedAt           *time.Time           `json:"started_at,omitempty"`
	TerminalAt          *time.Time           `json:"terminal_at,omitempty"`
	TerminalStatus      string               `json:"terminal_status,omitempty"`
	TokensUsed          int64                `json:"tokens_used,omitempty"`
	GraphTokensUsed     int64                `json:"graph_tokens_used,omitempty"`
	ReviewTokensUsed    int64                `json:"review_tokens_used,omitempty"`
	WorktreeFingerprint *WorktreeFingerprint `json:"worktree_fingerprint,omitempty"`
	PublicationMutation bool                 `json:"publication_mutation"`
	BlockerCode         string               `json:"blocker_code,omitempty"`
}

func DeriveAttemptIdentity(
	record DeliveryRecord,
	attempt int,
	incompleteTaskIDs []string,
	rules []RuntimeRule,
	remainingTokens int64,
	remainingWallSeconds int64,
) (string, string, error) {
	if !canonicalSHA256.MatchString(record.DeliveryID) || !boundedArgument(record.WorkspaceID) ||
		attempt < 1 || attempt > record.AttemptCeiling || !canonicalSHA256.MatchString(record.RoutingGenerationDigest) ||
		len(incompleteTaskIDs) == 0 || len(rules) == 0 || remainingTokens < 1 || remainingWallSeconds < 1 {
		return "", "", ErrDeliveryConflict
	}
	operationPayload, err := json.Marshal(struct {
		WorkspaceID       string        `json:"workspace_id"`
		DeliveryID        string        `json:"delivery_id"`
		Attempt           int           `json:"attempt"`
		RoutingGeneration string        `json:"routing_generation"`
		IncompleteTaskIDs []string      `json:"incomplete_task_ids"`
		RuntimeRules      []RuntimeRule `json:"runtime_rules"`
	}{record.WorkspaceID, record.DeliveryID, attempt, record.RoutingGenerationDigest, incompleteTaskIDs, rules})
	if err != nil {
		return "", "", ErrDeliveryConflict
	}
	operationID := sha256Digest(operationPayload)
	requestPayload, err := json.Marshal(struct {
		OperationID      string    `json:"operation_id"`
		WorktreeID       string    `json:"worktree_id"`
		Slug             string    `json:"slug"`
		OriginSessionID  string    `json:"origin_session_id"`
		AbsoluteDeadline time.Time `json:"absolute_deadline"`
		TokenCeiling     int64     `json:"token_ceiling"`
		RemainingTokens  int64     `json:"remaining_tokens"`
		RemainingWallSec int64     `json:"remaining_wall_seconds"`
	}{operationID, record.WorktreeID, record.Slug, record.OriginSessionID, record.AbsoluteDeadline, record.TokenCeiling, remainingTokens, remainingWallSeconds})
	if err != nil {
		return "", "", ErrDeliveryConflict
	}
	return operationID, sha256Digest(requestPayload), nil
}

func sha256Digest(payload []byte) string {
	digest := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(digest[:])
}

type DeliveryRecord struct {
	DeliveryID                 string               `json:"delivery_id"`
	WorkspaceID                string               `json:"workspace_id"`
	WorktreeID                 string               `json:"worktree_id"`
	WorktreeRoot               string               `json:"worktree_root"`
	Slug                       string               `json:"slug"`
	TaskSetDigest              string               `json:"task_set_digest"`
	TaskSnapshot               DeliveryTaskSnapshot `json:"task_snapshot"`
	RoutingGenerationDigest    string               `json:"routing_generation_digest"`
	OriginSessionID            string               `json:"origin_session_id"`
	CreatedAt                  time.Time            `json:"created_at"`
	AbsoluteDeadline           time.Time            `json:"absolute_deadline"`
	AttemptCeiling             int                  `json:"attempt_ceiling"`
	TokenCeiling               int64                `json:"token_ceiling"`
	InitialWorktreeFingerprint WorktreeFingerprint  `json:"initial_worktree_fingerprint"`
	State                      DeliveryState        `json:"state"`
	Attempts                   []DeliveryAttempt    `json:"attempts"`
	Graph                      *DeliveryGraph       `json:"graph,omitempty"`
}

func SuccessorRuntimeRules(
	generation RoutingGeneration,
	prior []RuntimeRule,
	incompleteTaskIDs []string,
	failedTaskIDs []string,
) ([]RuntimeRule, error) {
	if len(incompleteTaskIDs) == 0 {
		return nil, ErrDeliveryConflict
	}
	incomplete := make(map[string]struct{}, len(incompleteTaskIDs))
	for _, taskID := range incompleteTaskIDs {
		if !boundedArgument(taskID) {
			return nil, ErrDeliveryConflict
		}
		if _, duplicate := incomplete[taskID]; duplicate {
			return nil, ErrDeliveryConflict
		}
		incomplete[taskID] = struct{}{}
	}
	failed := make(map[string]struct{}, len(failedTaskIDs))
	for _, taskID := range failedTaskIDs {
		if _, exists := incomplete[taskID]; !exists {
			return nil, ErrDeliveryConflict
		}
		if _, duplicate := failed[taskID]; duplicate {
			return nil, ErrDeliveryConflict
		}
		failed[taskID] = struct{}{}
	}
	priorByTask := make(map[string]RuntimeValue, len(prior))
	for _, rule := range prior {
		if !boundedArgument(rule.Match.ID) || rule.Match.Domain != "" || rule.Match.Complexity != "" {
			return nil, ErrDeliveryConflict
		}
		if _, duplicate := priorByTask[rule.Match.ID]; duplicate {
			return nil, ErrDeliveryConflict
		}
		priorByTask[rule.Match.ID] = rule.Runtime
	}
	tasks := make(map[string]GenerationTask, len(generation.Tasks))
	for _, task := range generation.Tasks {
		tasks[task.ID] = task
	}
	rules := make([]RuntimeRule, 0, len(incompleteTaskIDs))
	for _, taskID := range incompleteTaskIDs {
		task, exists := tasks[taskID]
		if !exists {
			return nil, ErrDeliveryConflict
		}
		cell, exists := generationCellForTask(generation, task)
		if !exists {
			return nil, ErrDeliveryConflict
		}
		current, hasPrior := priorByTask[taskID]
		if !hasPrior {
			current, exists = generationRuntimeForTask(generation, task)
			if !exists {
				return nil, ErrDeliveryConflict
			}
		}
		if _, shouldAdvance := failed[taskID]; shouldAdvance {
			index := candidatePosition(cell, current)
			if index < 0 || index >= len(cell.Fallbacks) {
				return nil, ErrNoEligibleCandidate
			}
			next := cell.Fallbacks[index]
			current = RuntimeValue{Provider: next.ProviderID, Model: next.ModelID, Reasoning: next.Reasoning}
		} else if candidatePosition(cell, current) < 0 {
			return nil, ErrDeliveryConflict
		}
		rules = append(rules, RuntimeRule{Match: RuntimeMatch{ID: taskID}, Runtime: current})
	}
	return rules, nil
}

func generationCellForTask(generation RoutingGeneration, task GenerationTask) (RoutingCell, bool) {
	for _, cell := range generation.Cells {
		if cell.Domain != task.Domain || cell.Complexity != task.Complexity {
			continue
		}
		for _, taskID := range cell.TaskIDs {
			if taskID == task.ID {
				return cell, true
			}
		}
	}
	return RoutingCell{}, false
}

func generationRuntimeForTask(generation RoutingGeneration, task GenerationTask) (RuntimeValue, bool) {
	for _, rule := range generation.Rules {
		if rule.Match.ID == task.ID || (rule.Match.ID == "" && rule.Match.Domain == task.Domain && rule.Match.Complexity == task.Complexity) {
			return rule.Runtime, true
		}
	}
	return RuntimeValue{}, false
}

func candidatePosition(cell RoutingCell, runtime RuntimeValue) int {
	if runtimeMatchesCandidate(runtime, cell.Selected) {
		return 0
	}
	for index, candidate := range cell.Fallbacks {
		if runtimeMatchesCandidate(runtime, candidate) {
			return index + 1
		}
	}
	return -1
}

func runtimeMatchesCandidate(runtime RuntimeValue, candidate RuntimeCandidate) bool {
	return runtime.Provider == candidate.ProviderID && runtime.Model == candidate.ModelID && runtime.Reasoning == candidate.Reasoning
}

func (d *DeliveryRecord) AppendAttempt(proposed DeliveryAttempt) (DeliveryAttempt, bool, error) {
	if d == nil {
		return DeliveryAttempt{}, false, ErrDeliveryConflict
	}
	for _, existing := range d.Attempts {
		if existing.OperationID == proposed.OperationID || existing.Attempt == proposed.Attempt {
			if reflect.DeepEqual(existing, proposed) {
				return cloneAttempt(existing), true, nil
			}
			return DeliveryAttempt{}, false, ErrDeliveryConflict
		}
	}
	if d.State != DeliveryStateActive {
		return DeliveryAttempt{}, false, ErrInvalidDeliveryTransition
	}
	if proposed.Attempt != len(d.Attempts)+1 || proposed.Attempt > d.AttemptCeiling {
		return DeliveryAttempt{}, false, ErrDeliveryConflict
	}
	if err := validateAttempt(proposed, proposed.Attempt); err != nil {
		return DeliveryAttempt{}, false, err
	}
	d.Attempts = append(d.Attempts, cloneAttempt(proposed))
	return cloneAttempt(proposed), false, nil
}

func validateDelivery(record DeliveryRecord, generations map[string]RoutingGeneration) error {
	if !canonicalSHA256.MatchString(record.DeliveryID) || !boundedArgument(record.WorkspaceID) ||
		!boundedArgument(record.WorktreeID) || !filepath.IsAbs(record.WorktreeRoot) || filepath.Clean(record.WorktreeRoot) != record.WorktreeRoot ||
		!canonicalSlug.MatchString(record.Slug) || !canonicalHexHash.MatchString(record.TaskSetDigest) ||
		!canonicalSHA256.MatchString(record.RoutingGenerationDigest) || !boundedArgument(record.OriginSessionID) ||
		record.CreatedAt.IsZero() || record.AbsoluteDeadline.IsZero() || record.CreatedAt.Location() != time.UTC ||
		record.AbsoluteDeadline.Location() != time.UTC || !record.AbsoluteDeadline.Equal(record.CreatedAt.Add(4*time.Hour)) ||
		record.AttemptCeiling != deliveryAttemptCeiling || record.TokenCeiling != DeliveryTokenCeiling ||
		!record.State.valid() || len(record.Attempts) > record.AttemptCeiling {
		return ErrOwnershipUnproven
	}
	if err := validateWorktreeFingerprint(record.InitialWorktreeFingerprint); err != nil {
		return err
	}
	generation, exists := generations[record.RoutingGenerationDigest]
	if !exists || generation.Digest != record.RoutingGenerationDigest || generation.TaskSetDigest != record.TaskSetDigest {
		return ErrOwnershipUnproven
	}
	if err := validateDeliveryTaskSnapshot(record.TaskSnapshot, record.TaskSetDigest, generation); err != nil {
		return err
	}
	if record.Graph != nil {
		if err := validateDeliveryGraph(
			record.Graph,
			record.TaskSnapshot,
			generation,
			record.CreatedAt,
			record.InitialWorktreeFingerprint.HeadSHA,
		); err != nil {
			return err
		}
	}
	operations := make(map[string]struct{}, len(record.Attempts))
	for index, attempt := range record.Attempts {
		if err := validateAttempt(attempt, index+1); err != nil {
			return err
		}
		if _, duplicate := operations[attempt.OperationID]; duplicate {
			return ErrDeliveryConflict
		}
		operations[attempt.OperationID] = struct{}{}
	}
	return nil
}

func validateDeliveryTaskSnapshot(snapshot DeliveryTaskSnapshot, digest string, generation RoutingGeneration) error {
	if snapshot.Digest != digest || !canonicalHexHash.MatchString(snapshot.Digest) || len(snapshot.Tasks) == 0 ||
		len(snapshot.ItemTaskIDs) != len(snapshot.Tasks) || len(snapshot.Tasks) != len(generation.Tasks) {
		return ErrOwnershipUnproven
	}
	canonical := TaskSet{Slug: "snapshot", Tasks: make([]TaskArtifact, 0, len(snapshot.Tasks))}
	generationTasks := make(map[string]GenerationTask, len(generation.Tasks))
	for _, task := range generation.Tasks {
		if _, duplicate := generationTasks[task.ID]; duplicate {
			return ErrOwnershipUnproven
		}
		generationTasks[task.ID] = task
	}
	wantIncomplete := make([]string, 0, len(snapshot.Tasks))
	for index, task := range snapshot.Tasks {
		generationTask, exists := generationTasks[task.ID]
		if snapshot.ItemTaskIDs[index] != task.ID || !exists || generationTask.Domain != task.Domain || generationTask.Complexity != task.Complexity {
			return ErrOwnershipUnproven
		}
		canonical.Tasks = append(canonical.Tasks, TaskArtifact{
			ID: task.ID, Status: task.Status, Domain: task.Domain, Complexity: task.Complexity,
			Dependencies: append([]string(nil), task.Dependencies...), Digest: task.Digest,
		})
		if task.Status != "completed" {
			wantIncomplete = append(wantIncomplete, task.ID)
		}
	}
	recomputed, err := canonical.DeliverySnapshot()
	if err != nil || recomputed.Digest != snapshot.Digest || !reflect.DeepEqual(recomputed.IncompleteTaskIDs, snapshot.IncompleteTaskIDs) || !reflect.DeepEqual(recomputed.ItemTaskIDs, snapshot.ItemTaskIDs) {
		return ErrOwnershipUnproven
	}
	return nil
}

func validateAttempt(attempt DeliveryAttempt, expectedNumber int) error {
	if attempt.Attempt != expectedNumber || attempt.Attempt < 1 || attempt.Attempt > deliveryAttemptCeiling ||
		!canonicalSHA256.MatchString(attempt.OperationID) || !canonicalSHA256.MatchString(attempt.RequestDigest) ||
		!attempt.State.valid() || attempt.PlannedAt.IsZero() || attempt.PlannedAt.Location() != time.UTC ||
		attempt.TokensUsed < 0 || attempt.GraphTokensUsed < 0 || attempt.ReviewTokensUsed < 0 || len(attempt.RuntimeRules) == 0 {
		return ErrDeliveryConflict
	}
	seenTasks := make(map[string]struct{}, len(attempt.RuntimeRules))
	for _, rule := range attempt.RuntimeRules {
		if !boundedArgument(rule.Match.ID) || rule.Match.Domain != "" || rule.Match.Complexity != "" ||
			!boundedArgument(rule.Runtime.Provider) || !boundedArgument(rule.Runtime.Model) || !boundedArgument(rule.Runtime.Reasoning) {
			return ErrDeliveryConflict
		}
		if _, duplicate := seenTasks[rule.Match.ID]; duplicate {
			return ErrDeliveryConflict
		}
		seenTasks[rule.Match.ID] = struct{}{}
	}
	switch attempt.State {
	case AttemptPlanned:
		if attempt.RunID != "" || len(attempt.ChildRunIDs) != 0 || attempt.StartedAt != nil || attempt.TerminalAt != nil ||
			attempt.TerminalStatus != "" || attempt.TokensUsed != 0 || attempt.GraphTokensUsed != 0 || attempt.ReviewTokensUsed != 0 || attempt.WorktreeFingerprint != nil || attempt.PublicationMutation || attempt.BlockerCode != "" {
			return ErrDeliveryConflict
		}
	case AttemptSubmitted:
		if !boundedArgument(attempt.RunID) || attempt.StartedAt == nil || attempt.StartedAt.Location() != time.UTC ||
			attempt.TerminalAt != nil || attempt.TerminalStatus != "" || attempt.TokensUsed != 0 || attempt.GraphTokensUsed != 0 || attempt.ReviewTokensUsed != 0 || attempt.WorktreeFingerprint != nil || attempt.BlockerCode != "" {
			return ErrDeliveryConflict
		}
	case AttemptTerminal:
		if !boundedArgument(attempt.RunID) || attempt.StartedAt == nil || attempt.TerminalAt == nil ||
			attempt.StartedAt.Location() != time.UTC || attempt.TerminalAt.Location() != time.UTC ||
			!boundedArgument(attempt.TerminalStatus) || attempt.WorktreeFingerprint == nil {
			return ErrDeliveryConflict
		}
		if err := validateWorktreeFingerprint(*attempt.WorktreeFingerprint); err != nil {
			return err
		}
		if attempt.GraphTokensUsed > attempt.TokensUsed || attempt.ReviewTokensUsed > attempt.TokensUsed-attempt.GraphTokensUsed {
			return ErrDeliveryConflict
		}
	}
	for _, runID := range attempt.ChildRunIDs {
		if !boundedArgument(runID) {
			return ErrDeliveryConflict
		}
	}
	failed := make(map[string]struct{}, len(attempt.FailedTaskIDs))
	for _, taskID := range attempt.FailedTaskIDs {
		if !boundedArgument(taskID) {
			return ErrDeliveryConflict
		}
		if _, duplicate := failed[taskID]; duplicate {
			return ErrDeliveryConflict
		}
		failed[taskID] = struct{}{}
	}
	if attempt.State != AttemptTerminal && len(attempt.FailedTaskIDs) != 0 {
		return ErrDeliveryConflict
	}
	if attempt.BlockerCode != "" && !boundedArgument(attempt.BlockerCode) {
		return ErrDeliveryConflict
	}
	return nil
}

func validateDeliveryTransition(before, after DeliveryRecord) error {
	if before.State != DeliveryStateActive && len(before.Attempts) != len(after.Attempts) {
		return ErrInvalidDeliveryTransition
	}
	beforeHeader := before
	afterHeader := after
	beforeHeader.State, afterHeader.State = "", ""
	beforeHeader.Attempts, afterHeader.Attempts = nil, nil
	beforeHeader.Graph, afterHeader.Graph = nil, nil
	if !reflect.DeepEqual(beforeHeader, afterHeader) || !deliveryStateTransitionAllowed(before.State, after.State) || len(after.Attempts) < len(before.Attempts) || len(after.Attempts) > len(before.Attempts)+1 {
		return ErrDeliveryConflict
	}
	if before.Graph == nil || after.Graph == nil {
		if before.Graph != nil || after.Graph != nil {
			return ErrDeliveryConflict
		}
	} else if err := validateDeliveryGraphTransition(before.Graph, after.Graph); err != nil {
		return err
	}
	for index := range before.Attempts {
		if index < len(before.Attempts)-1 {
			if !reflect.DeepEqual(before.Attempts[index], after.Attempts[index]) {
				return ErrDeliveryConflict
			}
			continue
		}
		if err := validateAttemptTransition(before.Attempts[index], after.Attempts[index]); err != nil {
			return err
		}
	}
	return nil
}

func validateAttemptTransition(before, after DeliveryAttempt) error {
	beforeState, afterState := before.State, after.State
	before.State, after.State = "", ""
	if before.Attempt != after.Attempt || before.OperationID != after.OperationID || before.RequestDigest != after.RequestDigest ||
		!reflect.DeepEqual(before.RuntimeRules, after.RuntimeRules) || !before.PlannedAt.Equal(after.PlannedAt) {
		return ErrDeliveryConflict
	}
	switch beforeState {
	case AttemptPlanned:
		if afterState != AttemptPlanned && afterState != AttemptSubmitted {
			return ErrInvalidDeliveryTransition
		}
		if afterState == AttemptPlanned && !reflect.DeepEqual(before, after) {
			return ErrDeliveryConflict
		}
	case AttemptSubmitted:
		if afterState != AttemptSubmitted && afterState != AttemptTerminal {
			return ErrInvalidDeliveryTransition
		}
		if afterState == AttemptSubmitted && !reflect.DeepEqual(before, after) {
			return ErrDeliveryConflict
		}
	case AttemptTerminal:
		if afterState != AttemptTerminal || !reflect.DeepEqual(before, after) {
			return ErrInvalidDeliveryTransition
		}
	default:
		return ErrInvalidDeliveryTransition
	}
	return nil
}

func validateWorktreeFingerprint(fingerprint WorktreeFingerprint) error {
	if !canonicalGitSHA.MatchString(fingerprint.HeadSHA) || !canonicalSHA256.MatchString(fingerprint.PorcelainSHA256) || !canonicalSHA256.MatchString(fingerprint.ContentSHA256) {
		return ErrOwnershipUnproven
	}
	return nil
}

func (state DeliveryState) valid() bool {
	return state == DeliveryStateActive || state == DeliveryStateDone || state == DeliveryStateBlocked || state == DeliveryStateExhausted
}

func (state AttemptState) valid() bool {
	return state == AttemptPlanned || state == AttemptSubmitted || state == AttemptTerminal
}

func deliveryStateTransitionAllowed(before, after DeliveryState) bool {
	if before == after {
		return true
	}
	return before == DeliveryStateActive && (after == DeliveryStateDone || after == DeliveryStateBlocked || after == DeliveryStateExhausted)
}

func boundedArgument(value string) bool {
	trimmed := strings.TrimSpace(value)
	return trimmed != "" && trimmed == value && len(value) <= 256 && !strings.ContainsAny(value, "\x00\r\n")
}

func cloneAttempt(attempt DeliveryAttempt) DeliveryAttempt {
	attempt.RuntimeRules = append([]RuntimeRule(nil), attempt.RuntimeRules...)
	attempt.ChildRunIDs = append([]string(nil), attempt.ChildRunIDs...)
	attempt.FailedTaskIDs = append([]string(nil), attempt.FailedTaskIDs...)
	if attempt.StartedAt != nil {
		value := *attempt.StartedAt
		attempt.StartedAt = &value
	}
	if attempt.TerminalAt != nil {
		value := *attempt.TerminalAt
		attempt.TerminalAt = &value
	}
	if attempt.WorktreeFingerprint != nil {
		value := *attempt.WorktreeFingerprint
		attempt.WorktreeFingerprint = &value
	}
	return attempt
}

func immutableDeliveryIdentity(record DeliveryRecord) string {
	return strings.Join([]string{
		record.WorkspaceID, record.WorktreeID, record.WorktreeRoot, record.Slug, record.TaskSetDigest,
		record.RoutingGenerationDigest, record.OriginSessionID, record.InitialWorktreeFingerprint.HeadSHA,
		record.InitialWorktreeFingerprint.PorcelainSHA256, record.InitialWorktreeFingerprint.ContentSHA256,
	}, "\x00")
}
