package extensionapp

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"

	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/franciscpd/batuta-compozy/internal/routing"
)

type GraphOperation string

const (
	GraphOpPrepareWave     GraphOperation = "prepare_wave"
	GraphOpTaskContext     GraphOperation = "task_context"
	GraphOpRecordQuestion  GraphOperation = "record_question"
	GraphOpRecordAnswer    GraphOperation = "record_answer"
	GraphOpRecordCandidate GraphOperation = "record_candidate"
	GraphOpRecordFailure   GraphOperation = "record_failure"
	GraphOpSettleWave      GraphOperation = "settle_wave"
	GraphOpCleanup         GraphOperation = "cleanup"
	GraphOpTerminalize     GraphOperation = "terminalize"
)

type GraphDisposition string

const (
	GraphDispositionPreparing         GraphDisposition = "preparing"
	GraphDispositionWaveReady         GraphDisposition = "wave_ready"
	GraphDispositionTaskReady         GraphDisposition = "task_ready"
	GraphDispositionWaitingInput      GraphDisposition = "waiting_input"
	GraphDispositionCandidateRecorded GraphDisposition = "candidate_recorded"
	GraphDispositionWaveIntegrated    GraphDisposition = "wave_integrated"
	GraphDispositionReexecuteConflict GraphDisposition = "reexecute_conflict"
	GraphDispositionAllIntegrated     GraphDisposition = "all_integrated"
	GraphDispositionCleaned           GraphDisposition = "cleaned"
	GraphDispositionBlocked           GraphDisposition = "blocked"
	GraphDispositionExhausted         GraphDisposition = "exhausted"
)

type DeliveryGraphInput struct {
	Operation           GraphOperation   `json:"operation"`
	DeliveryID          string           `json:"delivery_id"`
	Wave                int              `json:"wave,omitempty"`
	TaskID              string           `json:"task_id,omitempty"`
	Execution           int              `json:"execution,omitempty"`
	ChildRunID          string           `json:"child_run_id,omitempty"`
	BaseSHA             string           `json:"base_sha,omitempty"`
	CommitSHA           string           `json:"commit_sha,omitempty"`
	Verification        json.RawMessage  `json:"verification,omitempty"`
	VerificationDigest  string           `json:"verification_digest,omitempty"`
	Prompt              string           `json:"prompt,omitempty"`
	Choices             []string         `json:"choices,omitempty"`
	QuestionOperationID string           `json:"question_operation_id,omitempty"`
	Answer              string           `json:"answer,omitempty"`
	BlockerCode         string           `json:"blocker_code,omitempty"`
	TerminalDisposition GraphDisposition `json:"terminal_disposition,omitempty"`
}

type DeliveryGraphOutput struct {
	Operation               GraphOperation         `json:"operation"`
	Disposition             GraphDisposition       `json:"disposition"`
	DeliveryID              string                 `json:"delivery_id,omitempty"`
	Wave                    int                    `json:"wave,omitempty"`
	TaskID                  string                 `json:"task_id,omitempty"`
	Execution               int                    `json:"execution,omitempty"`
	Tasks                   []DeliveryGraphTask    `json:"tasks,omitempty"`
	CleanupResults          []DeliveryGraphCleanup `json:"cleanup_results,omitempty"`
	TaskFile                string                 `json:"task_file,omitempty"`
	Runtime                 *routing.RuntimeValue  `json:"runtime,omitempty"`
	Answers                 []routing.TaskAnswer   `json:"answers,omitempty"`
	WorktreeID              string                 `json:"worktree_id,omitempty"`
	WorktreeRoot            string                 `json:"worktree_root,omitempty"`
	BaseSHA                 string                 `json:"base_sha,omitempty"`
	RemainingTaskExecutions int                    `json:"remaining_task_executions,omitempty"`
	QuestionOperationID     string                 `json:"question_operation_id,omitempty"`
	IntegrationOperationID  string                 `json:"integration_operation_id,omitempty"`
	RemainingTokens         int64                  `json:"remaining_tokens,omitempty"`
	RemainingWallSeconds    int                    `json:"remaining_wall_seconds,omitempty"`
	BlockerCode             string                 `json:"blocker_code,omitempty"`
	Replayed                bool                   `json:"replayed,omitempty"`
}

// MarshalJSON preserves the explicit empty answer history required by the
// task-context Loop template without serializing a null answers field for the
// other closed output variants.
func (output DeliveryGraphOutput) MarshalJSON() ([]byte, error) {
	type wireOutput DeliveryGraphOutput
	encoded, err := json.Marshal(wireOutput(output))
	if err != nil || output.Operation != GraphOpTaskContext {
		return encoded, err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(encoded, &fields); err != nil {
		return nil, err
	}
	if _, exists := fields["answers"]; !exists {
		fields["answers"] = json.RawMessage("[]")
	}
	return json.Marshal(fields)
}

type DeliveryGraphTask struct {
	Wave                       int                  `json:"wave"`
	TaskID                     string               `json:"task_id"`
	Execution                  int                  `json:"execution"`
	Domain                     routing.Domain       `json:"domain"`
	Complexity                 routing.Complexity   `json:"complexity"`
	Runtime                    routing.RuntimeValue `json:"runtime"`
	WorktreeID                 string               `json:"worktree_id"`
	WorktreeRoot               string               `json:"worktree_root"`
	BaseSHA                    string               `json:"base_sha"`
	RemainingTokens            int64                `json:"remaining_tokens"`
	RemainingActiveWallSeconds int                  `json:"remaining_active_wall_seconds"`
}

type DeliveryGraphCleanup struct {
	OperationID string `json:"operation_id"`
	TaskID      string `json:"task_id"`
	Execution   int    `json:"execution"`
	WorktreeID  string `json:"worktree_id"`
	State       string `json:"state"`
	BlockerCode string `json:"blocker_code,omitempty"`
}

func (a application) deliveryGraph(
	ctx context.Context,
	workspace *compozysdk.ExtensionToolWorkspaceScope,
	input DeliveryGraphInput,
) (compozysdk.ToolResult, error) {
	scope, err := trustedScope(workspace)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	if err := input.validate(); err != nil {
		return compozysdk.ToolResult{}, err
	}
	if a.services.deliveryGraph == nil {
		return compozysdk.ToolResult{}, errors.New("batuta: delivery graph service is unavailable")
	}
	output, err := a.services.deliveryGraph(ctx, scope, input)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	return compozysdk.StructuredResult(output)
}

func (input DeliveryGraphInput) validate() error {
	if !routingDigestPattern.MatchString(input.DeliveryID) {
		return errors.New("batuta: delivery graph requires delivery_id")
	}
	if input.Operation != GraphOpTerminalize && input.TerminalDisposition != "" {
		return errors.New("batuta: delivery graph operation has unexpected terminal disposition")
	}
	task := input.Wave >= 1 && input.TaskID != "" && input.TaskID == strings.TrimSpace(input.TaskID) &&
		len(input.TaskID) <= 128 && input.Execution >= 1 && input.Execution <= 4
	switch input.Operation {
	case GraphOpPrepareWave, GraphOpCleanup:
		if input.hasTaskFields() || input.hasResultFields() || input.hasQuestionFields() {
			return errors.New("batuta: delivery graph operation has unexpected fields")
		}
	case GraphOpTaskContext:
		if !task || input.hasResultFields() || input.hasQuestionFields() {
			return errors.New("batuta: task_context requires wave, task_id, and execution")
		}
	case GraphOpRecordQuestion:
		if !task || input.Prompt == "" || len(input.Prompt) > 2048 ||
			len(input.Choices) > 4 || input.hasCandidateFields() || input.hasAnswerFields() || input.ChildRunID != "" || input.BlockerCode != "" {
			return errors.New("batuta: invalid task question")
		}
		if !safeTaskQuestionText(input.Prompt) {
			return errors.New("batuta: task question contains unsafe content")
		}
		for _, choice := range input.Choices {
			if len(choice) > 512 || !safeTaskQuestionText(choice) {
				return errors.New("batuta: task question contains unsafe content")
			}
		}
	case GraphOpRecordAnswer:
		if !task || !routingDigestPattern.MatchString(input.QuestionOperationID) || input.Answer == "" ||
			len(input.Answer) > 4096 || input.hasCandidateFields() || input.Prompt != "" ||
			input.Choices != nil || input.ChildRunID != "" || input.BlockerCode != "" {
			return errors.New("batuta: invalid task answer")
		}
	case GraphOpRecordCandidate:
		explicit := input.hasCandidateFields()
		if !task || !validOpaqueRunID(input.ChildRunID) || input.hasQuestionFields() || input.BlockerCode != "" ||
			(explicit && (!gitSHAValue(input.BaseSHA) || !gitSHAValue(input.CommitSHA) ||
				!validTaskVerification(input.Verification, input.VerificationDigest, input.TaskID))) {
			return errors.New("batuta: invalid task candidate")
		}
	case GraphOpRecordFailure:
		if !task || !validOpaqueRunID(input.ChildRunID) || input.BlockerCode == "" || len(input.BlockerCode) > 256 ||
			input.hasCandidateFields() || input.hasQuestionFields() {
			return errors.New("batuta: invalid task failure")
		}
	case GraphOpSettleWave:
		if input.Wave < 1 || input.TaskID != "" || input.Execution != 0 || input.hasResultFields() || input.hasQuestionFields() {
			return errors.New("batuta: settle_wave requires only delivery_id and wave")
		}
	case GraphOpTerminalize:
		if input.TerminalDisposition != GraphDispositionBlocked && input.TerminalDisposition != GraphDispositionExhausted ||
			input.hasTaskFields() || input.hasResultFields() || input.hasQuestionFields() {
			return errors.New("batuta: terminalize requires a blocked or exhausted terminal disposition")
		}
	default:
		return errors.New("batuta: unsupported delivery graph operation")
	}
	return nil
}

func safeTaskQuestionText(value string) bool {
	return value != "" && value == strings.TrimSpace(value) && !strings.ContainsRune(value, '\x00') &&
		routing.SafeTaskQuestionText(value)
}

func validTaskVerification(payload json.RawMessage, expectedDigest, taskID string) bool {
	if len(payload) == 0 || len(payload) > 64<<10 || !json.Valid(payload) || !routingDigestPattern.MatchString(expectedDigest) {
		return false
	}
	sum := sha256.Sum256(payload)
	if expectedDigest != "sha256:"+hex.EncodeToString(sum[:]) {
		return false
	}
	var verification struct {
		TaskID string   `json:"task_id"`
		Status string   `json:"status"`
		Checks []string `json:"checks"`
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&verification); err != nil {
		return false
	}
	if err := decoder.Decode(&struct{}{}); !errors.Is(err, io.EOF) {
		return false
	}
	if verification.TaskID != taskID || verification.Status != "passed" || len(verification.Checks) < 1 || len(verification.Checks) > 16 {
		return false
	}
	for _, check := range verification.Checks {
		if check == "" || check != strings.TrimSpace(check) || len(check) > 512 || strings.ContainsRune(check, '\x00') {
			return false
		}
	}
	return true
}

func (input DeliveryGraphInput) hasTaskFields() bool {
	return input.Wave != 0 || input.TaskID != "" || input.Execution != 0
}

func (input DeliveryGraphInput) hasCandidateFields() bool {
	return input.BaseSHA != "" || input.CommitSHA != "" || len(input.Verification) != 0 || input.VerificationDigest != ""
}

func (input DeliveryGraphInput) hasAnswerFields() bool {
	return input.QuestionOperationID != "" || input.Answer != ""
}

func (input DeliveryGraphInput) hasQuestionFields() bool {
	return input.Prompt != "" || input.Choices != nil || input.hasAnswerFields()
}

func (input DeliveryGraphInput) hasResultFields() bool {
	return input.ChildRunID != "" || input.hasCandidateFields() || input.BlockerCode != ""
}

func gitSHAValue(value string) bool {
	if len(value) != 40 && len(value) != 64 {
		return false
	}
	for _, current := range value {
		if current < '0' || current > '9' {
			if current < 'a' || current > 'f' {
				return false
			}
		}
	}
	return true
}

func deliveryGraphInputSchema() map[string]any {
	operation := func(value GraphOperation) map[string]any { return map[string]any{"enum": []string{string(value)}} }
	base := func(value GraphOperation) map[string]any {
		return map[string]any{
			"operation":   operation(value),
			"delivery_id": sha256OutputSchema(),
		}
	}
	task := func(value GraphOperation) map[string]any {
		properties := base(value)
		properties["wave"] = map[string]any{"type": "integer", "minimum": 1}
		properties["task_id"] = map[string]any{"type": "string", "minLength": 1, "maxLength": 128}
		properties["execution"] = map[string]any{"type": "integer", "minimum": 1, "maximum": 4}
		return properties
	}
	return map[string]any{"oneOf": []any{
		objectSchema([]string{"operation", "delivery_id"}, base(GraphOpPrepareWave)),
		objectSchema([]string{"operation", "delivery_id", "wave", "task_id", "execution"}, task(GraphOpTaskContext)),
		objectSchema([]string{"operation", "delivery_id", "wave", "task_id", "execution", "prompt"}, withSchema(task(GraphOpRecordQuestion), map[string]any{
			"prompt":  map[string]any{"type": "string", "minLength": 1, "maxLength": 2048},
			"choices": map[string]any{"type": "array", "maxItems": 4, "items": map[string]any{"type": "string", "minLength": 1, "maxLength": 512}},
		})),
		objectSchema([]string{"operation", "delivery_id", "wave", "task_id", "execution", "question_operation_id", "answer"}, withSchema(task(GraphOpRecordAnswer), map[string]any{
			"question_operation_id": sha256OutputSchema(),
			"answer":                map[string]any{"type": "string", "minLength": 1, "maxLength": 4096},
		})),
		objectSchema([]string{"operation", "delivery_id", "wave", "task_id", "execution", "child_run_id"}, withSchema(task(GraphOpRecordCandidate), map[string]any{
			"child_run_id": opaqueRunIDSchema(),
		})),
		objectSchema([]string{"operation", "delivery_id", "wave", "task_id", "execution", "child_run_id", "base_sha", "commit_sha", "verification", "verification_digest"}, withSchema(task(GraphOpRecordCandidate), map[string]any{
			"child_run_id": opaqueRunIDSchema(), "base_sha": gitSHAInputSchema(), "commit_sha": gitSHAInputSchema(),
			"verification": taskVerificationSchema(), "verification_digest": sha256OutputSchema(),
		})),
		objectSchema([]string{"operation", "delivery_id", "wave", "task_id", "execution", "child_run_id", "blocker_code"}, withSchema(task(GraphOpRecordFailure), map[string]any{
			"child_run_id": opaqueRunIDSchema(), "blocker_code": map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
		})),
		objectSchema([]string{"operation", "delivery_id", "wave"}, withSchema(base(GraphOpSettleWave), map[string]any{
			"wave": map[string]any{"type": "integer", "minimum": 1},
		})),
		objectSchema([]string{"operation", "delivery_id"}, base(GraphOpCleanup)),
		objectSchema([]string{"operation", "delivery_id", "terminal_disposition"}, withSchema(base(GraphOpTerminalize), map[string]any{
			"terminal_disposition": map[string]any{"enum": []string{string(GraphDispositionBlocked), string(GraphDispositionExhausted)}},
		})),
	}}
}

func deliveryGraphOutputSchema() map[string]any {
	return objectSchema([]string{"operation", "disposition"}, map[string]any{
		"operation": map[string]any{"enum": []string{
			string(GraphOpPrepareWave), string(GraphOpTaskContext), string(GraphOpRecordQuestion), string(GraphOpRecordAnswer),
			string(GraphOpRecordCandidate), string(GraphOpRecordFailure), string(GraphOpSettleWave), string(GraphOpCleanup), string(GraphOpTerminalize),
		}},
		"disposition": map[string]any{"enum": []string{
			string(GraphDispositionPreparing), string(GraphDispositionWaveReady), string(GraphDispositionTaskReady),
			string(GraphDispositionWaitingInput), string(GraphDispositionCandidateRecorded), string(GraphDispositionWaveIntegrated),
			string(GraphDispositionReexecuteConflict), string(GraphDispositionAllIntegrated), string(GraphDispositionCleaned),
			string(GraphDispositionBlocked), string(GraphDispositionExhausted),
		}},
		"delivery_id": sha256OutputSchema(),
		"wave":        map[string]any{"type": "integer", "minimum": 1},
		"task_id":     map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
		"execution":   map[string]any{"type": "integer", "minimum": 1, "maximum": 4},
		"tasks": map[string]any{"type": "array", "maxItems": 4, "items": objectSchema([]string{
			"wave", "task_id", "execution", "domain", "complexity", "runtime", "worktree_id", "worktree_root", "base_sha",
			"remaining_tokens", "remaining_active_wall_seconds",
		}, map[string]any{
			"wave": map[string]any{"type": "integer", "minimum": 1}, "task_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
			"execution": map[string]any{"type": "integer", "minimum": 1, "maximum": 4}, "domain": domainSchema(), "complexity": complexitySchema(),
			"runtime": runtimeValueOutputSchema(), "worktree_id": opaqueRunIDSchema(), "worktree_root": map[string]any{"type": "string", "minLength": 1},
			"base_sha": gitSHAInputSchema(), "remaining_tokens": map[string]any{"type": "integer", "minimum": 1},
			"remaining_active_wall_seconds": map[string]any{"type": "integer", "minimum": 1},
		})},
		"cleanup_results": map[string]any{"type": "array", "maxItems": 256, "items": objectSchema([]string{
			"operation_id", "task_id", "execution", "worktree_id", "state",
		}, map[string]any{
			"operation_id": sha256OutputSchema(), "task_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
			"execution": map[string]any{"type": "integer", "minimum": 1, "maximum": 4}, "worktree_id": opaqueRunIDSchema(),
			"state":        map[string]any{"enum": []string{string(routing.CleanupPlanned), string(routing.CleanupRemoved), string(routing.CleanupRetained)}},
			"blocker_code": map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
		})},
		"task_file": map[string]any{"type": "string", "pattern": "^\\.compozy/tasks/[a-z0-9]+(?:-[a-z0-9]+)*/task_[0-9]+\\.md$"},
		"runtime":   runtimeValueOutputSchema(),
		"answers": map[string]any{"type": "array", "maxItems": 3, "items": objectSchema([]string{
			"question_operation_id", "loop_run_id", "generation", "node_id", "item_index", "value",
		}, map[string]any{
			"question_operation_id": sha256OutputSchema(), "loop_run_id": opaqueRunIDSchema(),
			"generation": map[string]any{"type": "integer", "minimum": 1},
			"node_id":    map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
			"item_index": map[string]any{"type": "integer", "minimum": 0},
			"value":      map[string]any{"type": "string", "minLength": 1, "maxLength": 4096},
		})},
		"worktree_id":               opaqueRunIDSchema(),
		"worktree_root":             map[string]any{"type": "string", "minLength": 1},
		"base_sha":                  gitSHAInputSchema(),
		"remaining_task_executions": map[string]any{"type": "integer", "minimum": 0, "maximum": 3},
		"question_operation_id":     sha256OutputSchema(),
		"integration_operation_id":  sha256OutputSchema(),
		"remaining_tokens":          map[string]any{"type": "integer", "minimum": 0},
		"remaining_wall_seconds":    map[string]any{"type": "integer", "minimum": 0},
		"blocker_code":              map[string]any{"type": "string", "minLength": 1, "maxLength": 256},
		"replayed":                  map[string]any{"type": "boolean"},
	})
}

func taskVerificationSchema() map[string]any {
	return objectSchema([]string{"task_id", "status", "checks"}, map[string]any{
		"task_id": map[string]any{"type": "string", "minLength": 1, "maxLength": 128},
		"status":  map[string]any{"enum": []string{"passed"}},
		"checks": map[string]any{
			"type": "array", "minItems": 1, "maxItems": 16,
			"items": map[string]any{"type": "string", "minLength": 1, "maxLength": 512},
		},
	})
}

func withSchema(target map[string]any, additional map[string]any) map[string]any {
	for key, value := range additional {
		target[key] = value
	}
	return target
}

func gitSHAInputSchema() map[string]any {
	return map[string]any{"type": "string", "pattern": "^(?:[0-9a-f]{40}|[0-9a-f]{64})$"}
}
