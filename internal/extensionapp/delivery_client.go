package extensionapp

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"time"

	"github.com/franciscpd/batuta-compozy/internal/publication"
	"github.com/franciscpd/batuta-compozy/internal/routing"
)

const (
	deliveryCommandTimeout    = 30 * time.Second
	deliveryStdoutLimit       = int64(2 << 20)
	deliveryStatusStdoutLimit = int64(32 << 20)
	deliveryStderrLimit       = int64(64 << 10)
)

const deliveryEnvelopeVersion int64 = 1

type deliveryRun struct {
	ID                string         `json:"id"`
	WorkspaceID       string         `json:"workspace_id"`
	ParentLoopRunID   string         `json:"parent_loop_run_id,omitempty"`
	LoopName          string         `json:"loop_name"`
	Status            string         `json:"status"`
	CreatedAt         time.Time      `json:"created_at"`
	StartedAt         time.Time      `json:"started_at"`
	TokensUsed        int64          `json:"tokens_used"`
	TokensUsedPresent bool           `json:"-"`
	Inputs            map[string]any `json:"inputs"`
}

func (r *deliveryRun) UnmarshalJSON(payload []byte) error {
	type deliveryRunWire deliveryRun
	var decoded deliveryRunWire
	if err := json.Unmarshal(payload, &decoded); err != nil {
		return err
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(payload, &fields); err != nil {
		return err
	}
	if rawRunID, exists := fields["run_id"]; exists {
		var runID string
		if err := json.Unmarshal(rawRunID, &runID); err != nil {
			return err
		}
		if decoded.ID != "" && decoded.ID != runID {
			return errors.New("batuta: conflicting Compozy run identities")
		}
		decoded.ID = runID
	}
	*r = deliveryRun(decoded)
	_, r.TokensUsedPresent = fields["tokens_used"]
	return nil
}

type deliveryRunListResponse struct {
	Items      []deliveryRun `json:"items"`
	NextCursor string        `json:"next_cursor"`
}

type deliveryRunDetail struct {
	Run         deliveryRun          `json:"run"`
	Generations []deliveryGeneration `json:"generations"`
	Requests    []deliveryRequest    `json:"requests"`
}

type deliveryRequest struct {
	LoopRunID        string          `json:"loop_run_id"`
	LoopName         string          `json:"loop_name,omitempty"`
	Generation       int             `json:"generation"`
	NodeID           string          `json:"node_id"`
	ItemIndex        int             `json:"item_index"`
	Kind             string          `json:"kind"`
	State            string          `json:"state"`
	Prompt           string          `json:"prompt"`
	Context          json.RawMessage `json:"context"`
	Expect           json.RawMessage `json:"expect,omitempty"`
	Decisions        []string        `json:"decisions"`
	Agents           string          `json:"agents"`
	AnsweredDecision string          `json:"answered_decision,omitempty"`
	ActorKind        string          `json:"actor_kind,omitempty"`
	ActorID          string          `json:"actor_id,omitempty"`
	AnsweredAt       *time.Time      `json:"answered_at,omitempty"`
	ResolvedAt       *time.Time      `json:"resolved_at,omitempty"`
}

type deliveryStartRequest struct {
	DeliveryID          string
	Attempt             int
	Slug                string
	OriginSessionID     string
	WorktreeRef         string
	RoutingGeneration   string
	AbsoluteDeadline    time.Time
	TokenCeiling        int64
	RecoveryOperationID string
	IterationCap        int
	BudgetTokens        int64
	BudgetWallSec       int
}

type deliveryRunClient interface {
	Status(context.Context, string, string) (deliveryRunDetail, error)
	Recent(context.Context, string, int) ([]deliveryRun, error)
	Start(context.Context, string, deliveryStartRequest) (deliveryRun, error)
}

// deliveryAgentName is the conducting agent that owns every delivery origin session.
const deliveryAgentName = "batuta"

type deliveryLoopCLIClient struct {
	Executable string
	Runner     publication.CommandRunner
}

func (c deliveryLoopCLIClient) Status(ctx context.Context, workspaceID, runID string) (deliveryRunDetail, error) {
	if err := c.validateBoundary(ctx, workspaceID); err != nil || !validOpaqueRunID(runID) {
		if err != nil {
			return deliveryRunDetail{}, err
		}
		return deliveryRunDetail{}, errors.New("batuta: invalid delivery run identity")
	}
	result, err := c.runWithStdoutLimit(ctx, []string{"loop", "status", "--workspace", workspaceID, "--run-id", runID, "-o", "json"}, deliveryStatusStdoutLimit)
	if err != nil {
		return deliveryRunDetail{}, err
	}
	var detail deliveryRunDetail
	if err := decodeDeliveryResponse(result, &detail); err != nil {
		return deliveryRunDetail{}, err
	}
	if err := validateDeliveryRun(detail.Run, workspaceID); err != nil || detail.Run.ID != runID {
		return deliveryRunDetail{}, errors.New("batuta: delivery status identity mismatch")
	}
	return detail, nil
}

func (c deliveryLoopCLIClient) Recent(ctx context.Context, workspaceID string, limit int) ([]deliveryRun, error) {
	if err := c.validateBoundary(ctx, workspaceID); err != nil {
		return nil, err
	}
	if limit != 200 {
		return nil, errors.New("batuta: delivery reconciliation requires the fixed recent-run limit")
	}
	result, err := c.run(ctx, []string{"loop", "runs", "--workspace", workspaceID, "--loop", "batuta-deliver", "--limit", strconv.Itoa(limit), "-o", "json"})
	if err != nil {
		return nil, err
	}
	var response deliveryRunListResponse
	if err := decodeDeliveryResponse(result, &response); err != nil {
		return nil, err
	}
	if response.Items == nil {
		return nil, errors.New("batuta: malformed Compozy recent delivery response")
	}
	if len(response.Items) > limit {
		return nil, errors.New("batuta: recent delivery response exceeds requested limit")
	}
	for _, run := range response.Items {
		if err := validateDeliveryRun(run, workspaceID); err != nil || run.LoopName != "batuta-deliver" {
			return nil, errors.New("batuta: recent delivery response contains invalid identity")
		}
	}
	return response.Items, nil
}

func (c deliveryLoopCLIClient) Start(ctx context.Context, workspaceID string, request deliveryStartRequest) (deliveryRun, error) {
	if err := c.validateBoundary(ctx, workspaceID); err != nil {
		return deliveryRun{}, err
	}
	if err := validateDeliveryStartRequest(request); err != nil {
		return deliveryRun{}, err
	}
	path, err := writeDeliveryConfig(request)
	if err != nil {
		return deliveryRun{}, err
	}
	defer func() { _ = os.Remove(path) }()

	args := []string{
		"loop", "run", "--workspace", workspaceID, "--name", "batuta-deliver", "--no-prompt",
		"--input", "delivery_id=" + request.DeliveryID,
		"--input", "attempt=" + strconv.Itoa(request.Attempt),
		"--input", "slug=" + request.Slug,
		"--input", "origin_session_id=" + request.OriginSessionID,
		"--input", "worktree_ref=" + request.WorktreeRef,
		"--input", "routing_generation=" + request.RoutingGeneration,
		"--input", "absolute_deadline=" + request.AbsoluteDeadline.Format(time.RFC3339),
		"--input", "token_ceiling=" + strconv.FormatInt(request.TokenCeiling, 10),
		"--input", "recovery_operation_id=" + request.RecoveryOperationID,
		"--input", "delivery_envelope_version=" + strconv.FormatInt(deliveryEnvelopeVersion, 10),
		"--input", "iteration_cap=" + strconv.Itoa(request.IterationCap),
		"--input", "budget_tokens=" + strconv.FormatInt(request.BudgetTokens, 10),
		"--input", "budget_wall_seconds=" + strconv.Itoa(request.BudgetWallSec),
		"--config-file", path, "-o", "json",
	}
	// The delivery must start as the conducting agent session: CompozyOS derives
	// every child session's provenance parent from the run's starting actor, and
	// a run started by the bare CLI user leaves the workers unparented.
	result, err := c.runWithEnvironment(ctx, args, []string{
		"COMPOZY_SESSION_ID=" + request.OriginSessionID,
		"COMPOZY_AGENT=" + deliveryAgentName,
	})
	if err != nil {
		return deliveryRun{}, err
	}
	var response struct {
		Run *deliveryRun `json:"run"`
	}
	if err := decodeDeliveryResponse(result, &response); err != nil || response.Run == nil {
		return deliveryRun{}, errors.New("batuta: malformed Compozy delivery response")
	}
	if err := validateDeliveryRun(*response.Run, workspaceID); err != nil || response.Run.LoopName != "batuta-deliver" || !deliveryRunMatchesRequest(*response.Run, request) {
		return deliveryRun{}, errors.New("batuta: started delivery does not match the requested attempt")
	}
	return *response.Run, nil
}

func (c deliveryLoopCLIClient) validateBoundary(ctx context.Context, workspaceID string) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if c.Runner == nil || !filepath.IsAbs(c.Executable) || filepath.Clean(c.Executable) != c.Executable {
		return errors.New("batuta: fixed Compozy delivery boundary is unavailable")
	}
	if !validOpaqueRunID(workspaceID) {
		return errors.New("batuta: invalid delivery workspace identity")
	}
	return nil
}

func (c deliveryLoopCLIClient) run(ctx context.Context, args []string) (publication.CommandResult, error) {
	return c.runWithStdoutLimit(ctx, args, deliveryStdoutLimit)
}

func (c deliveryLoopCLIClient) runWithEnvironment(ctx context.Context, args, environment []string) (publication.CommandResult, error) {
	return c.execute(ctx, publication.Command{
		Executable: c.Executable, Args: args, Environment: environment,
		StdoutLimit: deliveryStdoutLimit, StderrLimit: deliveryStderrLimit,
	})
}

func (c deliveryLoopCLIClient) runWithStdoutLimit(ctx context.Context, args []string, stdoutLimit int64) (publication.CommandResult, error) {
	return c.execute(ctx, publication.Command{
		Executable: c.Executable, Args: args, StdoutLimit: stdoutLimit, StderrLimit: deliveryStderrLimit,
	})
}

func (c deliveryLoopCLIClient) execute(ctx context.Context, command publication.Command) (publication.CommandResult, error) {
	commandCtx, cancel := context.WithTimeout(ctx, deliveryCommandTimeout)
	defer cancel()
	result, err := c.Runner.Run(commandCtx, command)
	if err != nil {
		if ctxErr := commandCtx.Err(); ctxErr != nil {
			return publication.CommandResult{}, ctxErr
		}
		return publication.CommandResult{}, errors.New("batuta: Compozy delivery command failed; reconcile recent runs before retrying")
	}
	return result, nil
}

func validateDeliveryStartRequest(request deliveryStartRequest) error {
	if !routingDigestPattern.MatchString(request.DeliveryID) || request.Attempt < 1 || request.Attempt > 4 ||
		!validCanonicalSlug(request.Slug) || !validOpaqueRunID(request.OriginSessionID) ||
		!validOpaqueRunID(request.WorktreeRef) || !routingDigestPattern.MatchString(request.RoutingGeneration) ||
		request.AbsoluteDeadline.IsZero() || request.AbsoluteDeadline.Location() != time.UTC || request.TokenCeiling != routing.DeliveryTokenCeiling ||
		(request.Attempt == 1 && request.RecoveryOperationID != "") || (request.Attempt > 1 && !routingDigestPattern.MatchString(request.RecoveryOperationID)) ||
		request.IterationCap < 1 || request.IterationCap > 64 ||
		request.BudgetTokens < 1 || request.BudgetTokens > request.TokenCeiling || request.BudgetWallSec < 1 || request.BudgetWallSec > 14400 {
		return errors.New("batuta: invalid delivery start request")
	}
	return nil
}

func writeDeliveryConfig(request deliveryStartRequest) (string, error) {
	file, err := os.CreateTemp("", "batuta-delivery-config-*.json")
	if err != nil {
		return "", errors.New("batuta: create delivery configuration failed")
	}
	path := file.Name()
	remove := true
	defer func() {
		_ = file.Close()
		if remove {
			_ = os.Remove(path)
		}
	}()
	if err := file.Chmod(0o600); err != nil {
		return "", errors.New("batuta: secure delivery configuration failed")
	}
	config := struct {
		IterationCap      int    `json:"iteration_cap"`
		BudgetTokens      int64  `json:"budget_tokens"`
		BudgetWallSec     int    `json:"budget_wall_sec"`
		BudgetOnExceeded  string `json:"budget_on_exceeded"`
		ReattemptStrategy string `json:"reattempt_strategy"`
	}{request.IterationCap, request.BudgetTokens, request.BudgetWallSec, "halt", "halt"}
	if err := json.NewEncoder(file).Encode(config); err != nil {
		return "", errors.New("batuta: write delivery configuration failed")
	}
	if err := file.Sync(); err != nil {
		return "", errors.New("batuta: sync delivery configuration failed")
	}
	if err := file.Close(); err != nil {
		return "", errors.New("batuta: close delivery configuration failed")
	}
	remove = false
	return path, nil
}

func decodeDeliveryResponse(result publication.CommandResult, target any) error {
	if result.StdoutTruncated || len(result.Stdout) == 0 || int64(len(result.Stdout)) > deliveryStdoutLimit {
		return errors.New("batuta: malformed Compozy delivery response")
	}
	if err := rejectDuplicateJSONKeys(result.Stdout); err != nil {
		return errors.New("batuta: malformed Compozy delivery response")
	}
	if err := json.Unmarshal(result.Stdout, target); err != nil {
		return errors.New("batuta: malformed Compozy delivery response")
	}
	return nil
}

func rejectDuplicateJSONKeys(payload []byte) error {
	decoder := json.NewDecoder(bytes.NewReader(payload))
	first, err := decoder.Token()
	if err != nil {
		return err
	}
	if err := scanJSONValue(decoder, first); err != nil {
		return err
	}
	if _, err := decoder.Token(); !errors.Is(err, io.EOF) {
		if err == nil {
			return errors.New("trailing JSON value")
		}
		return err
	}
	return nil
}

func scanJSONValue(decoder *json.Decoder, token json.Token) error {
	delimiter, composite := token.(json.Delim)
	if !composite {
		return nil
	}
	switch delimiter {
	case '{':
		seen := map[string]struct{}{}
		for decoder.More() {
			keyToken, err := decoder.Token()
			if err != nil {
				return err
			}
			key, ok := keyToken.(string)
			if !ok {
				return errors.New("invalid JSON object key")
			}
			if _, duplicate := seen[key]; duplicate {
				return errors.New("duplicate JSON object key")
			}
			seen[key] = struct{}{}
			value, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := scanJSONValue(decoder, value); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim('}') {
			return errors.New("invalid JSON object")
		}
	case '[':
		for decoder.More() {
			value, err := decoder.Token()
			if err != nil {
				return err
			}
			if err := scanJSONValue(decoder, value); err != nil {
				return err
			}
		}
		end, err := decoder.Token()
		if err != nil || end != json.Delim(']') {
			return errors.New("invalid JSON array")
		}
	default:
		return errors.New("invalid JSON delimiter")
	}
	return nil
}

func validateDeliveryRun(run deliveryRun, workspaceID string) error {
	if !validOpaqueRunID(run.ID) || run.WorkspaceID != workspaceID || !validOpaqueRunID(run.LoopName) ||
		!validDeliveryRunStatus(run.Status) || run.CreatedAt.IsZero() || run.CreatedAt.Location() != time.UTC ||
		(!run.StartedAt.IsZero() && run.StartedAt.Location() != time.UTC) || run.TokensUsed < 0 || run.Inputs == nil {
		return errors.New("batuta: invalid Compozy delivery run")
	}
	return nil
}

func validDeliveryRunStatus(status string) bool {
	switch status {
	case "queued", "running", "watching", "needs-approval", "paused", "done", "no-op", "blocked", "failed", "exhausted", "stalled", "canceled":
		return true
	default:
		return false
	}
}

func deliveryRunMatchesRequest(run deliveryRun, request deliveryStartRequest) bool {
	return deliveryIdentityMatchesRequest(run, request) &&
		intInput(run.Inputs, "delivery_envelope_version") == deliveryEnvelopeVersion &&
		intInput(run.Inputs, "iteration_cap") == int64(request.IterationCap) &&
		intInput(run.Inputs, "budget_tokens") == request.BudgetTokens &&
		intInput(run.Inputs, "budget_wall_seconds") == int64(request.BudgetWallSec)
}

func deliveryIdentityMatchesRequest(run deliveryRun, request deliveryStartRequest) bool {
	values := run.Inputs
	return stringInput(values, "delivery_id") == request.DeliveryID && intInput(values, "attempt") == int64(request.Attempt) &&
		stringInput(values, "slug") == request.Slug && stringInput(values, "origin_session_id") == request.OriginSessionID &&
		stringInput(values, "worktree_ref") == request.WorktreeRef && stringInput(values, "routing_generation") == request.RoutingGeneration &&
		stringInput(values, "absolute_deadline") == request.AbsoluteDeadline.Format(time.RFC3339) && intInput(values, "token_ceiling") == request.TokenCeiling &&
		stringInput(values, "recovery_operation_id") == request.RecoveryOperationID
}

func stringInput(values map[string]any, key string) string {
	value, _ := values[key].(string)
	return value
}

func intInput(values map[string]any, key string) int64 {
	switch value := values[key].(type) {
	case int:
		return int64(value)
	case int64:
		return value
	case float64:
		if value == float64(int64(value)) {
			return int64(value)
		}
	case json.Number:
		parsed, _ := value.Int64()
		return parsed
	}
	return -1
}

var (
	canonicalSlugPattern = regexp.MustCompile(`^[a-z0-9]+(?:-[a-z0-9]+)*$`)
	slugLetterPattern    = regexp.MustCompile(`[a-z]`)
)

func validCanonicalSlug(slug string) bool {
	return canonicalSlugPattern.MatchString(slug) && slugLetterPattern.MatchString(slug)
}
