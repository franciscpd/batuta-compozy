package extensionapp

import (
	"context"
	"encoding/json"
	"errors"
	"reflect"
	"strings"
	"testing"

	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/franciscpd/batuta-compozy/internal/publication"
)

func TestDeliveryGraphToolExposesNineClosedOperations(t *testing.T) {
	t.Parallel()

	extension, err := newWithServices(serviceSet{})
	if err != nil {
		t.Fatalf("newWithServices() error = %v", err)
	}
	descriptor := descriptorForHandler(t, extension, "delivery_graph")
	if descriptor.ReadOnly || descriptor.Risk != compozysdk.RiskMutating {
		t.Fatalf("delivery graph descriptor = %#v", descriptor)
	}
	var schema map[string]any
	if err := json.Unmarshal(descriptor.InputSchema, &schema); err != nil {
		t.Fatalf("input schema: %v", err)
	}
	variants, ok := schema["oneOf"].([]any)
	if !ok || len(variants) != 10 {
		t.Fatalf("delivery graph schema variants = %#v", schema["oneOf"])
	}
	want := []GraphOperation{
		GraphOpPrepareWave,
		GraphOpTaskContext,
		GraphOpRecordQuestion,
		GraphOpRecordAnswer,
		GraphOpRecordCandidate,
		GraphOpRecordCandidate,
		GraphOpRecordFailure,
		GraphOpSettleWave,
		GraphOpCleanup,
		GraphOpTerminalize,
	}
	for index, raw := range variants {
		variant, ok := raw.(map[string]any)
		if !ok || variant["additionalProperties"] != false {
			t.Fatalf("variant[%d] = %#v, want closed object", index, raw)
		}
		properties := variant["properties"].(map[string]any)
		operation := properties["operation"].(map[string]any)["enum"].([]any)
		if !reflect.DeepEqual(operation, []any{string(want[index])}) {
			t.Fatalf("variant[%d] operation = %#v, want %q", index, operation, want[index])
		}
		required := variant["required"].([]any)
		if !containsAny(required, "operation") || !containsAny(required, "delivery_id") || !containsAny(required, "worktree_ref") {
			t.Fatalf("variant[%d] required = %#v", index, required)
		}
	}
	payload := string(descriptor.InputSchema)
	for _, required := range []string{"maximum\":4", "maxLength\":2048", "maxItems\":4"} {
		if !strings.Contains(payload, required) {
			t.Fatalf("delivery graph schema missing bound %q: %s", required, payload)
		}
	}
	var outputSchema map[string]any
	if err := json.Unmarshal(descriptor.OutputSchema, &outputSchema); err != nil {
		t.Fatalf("output schema: %v", err)
	}
	if properties, ok := outputSchema["properties"].(map[string]any); !ok || !reflect.DeepEqual(properties["replayed"], map[string]any{"type": "boolean"}) {
		t.Fatalf("delivery graph output schema does not expose replayed: %#v", outputSchema["properties"])
	}
}

func TestDeliveryGraphRecordAnswerDerivesRequestIdentity(t *testing.T) {
	t.Parallel()

	input := DeliveryGraphInput{
		Operation: GraphOpRecordAnswer, DeliveryID: digestValue("delivery-derived-answer"), WorktreeRef: "wt_1234567890abcdef",
		Wave: 1, TaskID: "task_01", Execution: 1,
		QuestionOperationID: digestValue("question-operation"), Answer: "Preserve compatibility",
	}
	if err := input.validate(); err != nil {
		t.Fatalf("record_answer with only question_operation_id and answer: %v", err)
	}

	schema := deliveryGraphInputSchema()
	variant := schema["oneOf"].([]any)[3].(map[string]any)
	properties := variant["properties"].(map[string]any)
	required := variant["required"].([]string)
	wantRequired := []string{"operation", "delivery_id", "worktree_ref", "wave", "task_id", "execution", "question_operation_id", "answer"}
	if !reflect.DeepEqual(required, wantRequired) {
		t.Fatalf("record_answer required = %#v, want %#v", required, wantRequired)
	}
	for _, legacy := range []string{"request_loop_run_id", "request_generation", "request_node_id", "request_item_index"} {
		if _, exists := properties[legacy]; exists {
			t.Fatalf("record_answer exposes caller-controlled %q", legacy)
		}
	}
}

func TestDeliveryGraphRecordQuestionDerivesCanonicalContextDigest(t *testing.T) {
	t.Parallel()

	input := DeliveryGraphInput{
		Operation: GraphOpRecordQuestion, DeliveryID: digestValue("delivery-derived-context"), WorktreeRef: "wt_1234567890abcdef",
		Wave: 1, TaskID: "task_01", Execution: 1, Prompt: "Which compatibility behavior should we ship?",
		Choices: []string{"Preserve compatibility", "Adopt the new contract"},
	}
	if err := input.validate(); err != nil {
		t.Fatalf("record_question without caller context digest: %v", err)
	}
	variant := deliveryGraphInputSchema()["oneOf"].([]any)[2].(map[string]any)
	properties := variant["properties"].(map[string]any)
	if _, exists := properties["context_digest"]; exists {
		t.Fatal("record_question exposes caller-controlled context_digest")
	}
}

func TestDeliveryGraphTaskContextWireOutputAlwaysIncludesAnswersArray(t *testing.T) {
	t.Parallel()

	for name, output := range map[string]DeliveryGraphOutput{
		"task context":    {Operation: GraphOpTaskContext, Disposition: GraphDispositionTaskReady},
		"other operation": {Operation: GraphOpRecordAnswer, Disposition: GraphDispositionTaskReady},
	} {
		t.Run(name, func(t *testing.T) {
			result, err := compozysdk.StructuredResult(output)
			if err != nil {
				t.Fatalf("StructuredResult() error = %v", err)
			}
			var fields map[string]json.RawMessage
			if err := json.Unmarshal(result.Structured, &fields); err != nil {
				t.Fatalf("decode structured output: %v", err)
			}
			answers, exists := fields["answers"]
			if name == "task context" && (!exists || string(answers) != "[]") {
				t.Fatalf("task_context answers = %q exists=%v, want []", answers, exists)
			}
			if name == "other operation" && exists {
				t.Fatalf("non-task_context emitted answers = %q", answers)
			}
		})
	}
}

func TestDeliveryGraphHandlerRequiresDaemonTrustedWorkspaceBeforeService(t *testing.T) {
	t.Parallel()

	called := false
	app := application{services: serviceSet{deliveryGraph: func(
		context.Context,
		publication.TrustedScope,
		DeliveryGraphInput,
	) (DeliveryGraphOutput, error) {
		called = true
		return DeliveryGraphOutput{}, nil
	}}}
	if _, err := app.deliveryGraph(context.Background(), nil, DeliveryGraphInput{
		Operation: GraphOpPrepareWave, DeliveryID: digestValue("delivery-graph-handler"),
	}); err == nil {
		t.Fatal("deliveryGraph(nil workspace) error = nil")
	}
	if called {
		t.Fatal("delivery graph service called before trusted workspace validation")
	}
}

func TestDeliveryGraphInputRejectsSecretPathQuestionsAndOpenVerification(t *testing.T) {
	t.Parallel()

	baseQuestion := DeliveryGraphInput{
		Operation: GraphOpRecordQuestion, DeliveryID: digestValue("delivery-question-validation"), WorktreeRef: "wt_1234567890abcdef",
		Wave: 1, TaskID: "task_01", Execution: 1, Prompt: "Choose the supported compatibility behavior",
		Choices: []string{"Preserve compatibility", "Adopt new behavior"},
	}
	for _, prompt := range []string{
		"Use Authorization: Bearer super-secret-token",
		"Use ghp_0123456789abcdefghijklmnopqrstuv",
		"Set AWS_SECRET_ACCESS_KEY = super-secret-token",
		"Read the decision from /home/operator/private/answer.txt",
		"Read the host setting from /etc",
		"Use .ssh/id_rsa for authentication",
		"Load ../../outside-workspace/config.json",
	} {
		input := baseQuestion
		input.Prompt = prompt
		if err := input.validate(); err == nil {
			t.Fatalf("question prompt %q error = nil", prompt)
		}
	}
	oversizedChoice := baseQuestion
	oversizedChoice.Choices = []string{strings.Repeat("a", 513)}
	if err := oversizedChoice.validate(); err == nil {
		t.Fatal("oversized question choice error = nil")
	}

	verification := json.RawMessage(`{"checks":["go test ./..."],"status":"passed","task_id":"task_01"}`)
	baseCandidate := DeliveryGraphInput{
		Operation: GraphOpRecordCandidate, DeliveryID: digestValue("delivery-candidate-validation"), WorktreeRef: "wt_1234567890abcdef",
		Wave: 1, TaskID: "task_01", Execution: 1, ChildRunID: "run_task_01",
		BaseSHA: "0123456789abcdef0123456789abcdef01234567", CommitSHA: "abcdefabcdefabcdefabcdefabcdefabcdefabcd",
		Verification: verification, VerificationDigest: digestValue(string(verification)),
	}
	if err := baseCandidate.validate(); err != nil {
		t.Fatalf("valid candidate error = %v", err)
	}
	open := baseCandidate
	open.Verification = json.RawMessage(`{"checks":["go test ./..."],"status":"passed","task_id":"task_01","secret":"leak"}`)
	open.VerificationDigest = digestValue(string(open.Verification))
	if err := open.validate(); err == nil {
		t.Fatal("open verification error = nil")
	}
}

func containsAny(values []any, wanted any) bool {
	for _, value := range values {
		if value == wanted {
			return true
		}
	}
	return false
}

func TestTaskVerificationMatchesThePublicSixteenCheckCeiling(t *testing.T) {
	t.Parallel()

	verification := func(count int) json.RawMessage {
		t.Helper()
		checks := make([]string, count)
		for index := range checks {
			checks[index] = "check"
		}
		payload, err := json.Marshal(map[string]any{
			"task_id": "task_01", "status": "passed", "checks": checks,
		})
		if err != nil {
			t.Fatalf("json.Marshal() error = %v", err)
		}
		return payload
	}

	atCeiling := verification(16)
	if !validTaskVerification(atCeiling, digestValue(string(atCeiling)), "task_01") {
		t.Fatal("validTaskVerification(16 checks) = false")
	}
	aboveCeiling := verification(17)
	if validTaskVerification(aboveCeiling, digestValue(string(aboveCeiling)), "task_01") {
		t.Fatal("validTaskVerification(17 checks) = true, want schema-aligned rejection")
	}
}

func TestDeliveryGraphHandlerScopesServiceToInspectedWorktree(t *testing.T) {
	t.Parallel()

	var inspectedRef string
	inspect := func(_ context.Context, _ publication.TrustedScope, ref string) (publication.WorktreeInspection, error) {
		inspectedRef = ref
		return publication.WorktreeInspection{Worktree: publication.Worktree{
			ID: "wt_1234567890abcdef", WorkspaceID: "ws_fixture", Path: "/absolute/delivery-worktree",
		}}, nil
	}
	var seen publication.TrustedScope
	app := application{services: serviceSet{inspectDeliveryWorktree: inspect, deliveryGraph: func(
		_ context.Context,
		scope publication.TrustedScope,
		_ DeliveryGraphInput,
	) (DeliveryGraphOutput, error) {
		seen = scope
		return DeliveryGraphOutput{Operation: GraphOpPrepareWave, Disposition: GraphDispositionWaveReady}, nil
	}}}
	workspace := &compozysdk.ExtensionToolWorkspaceScope{ID: "ws_fixture", Root: "/absolute/workspace"}
	if _, err := app.deliveryGraph(context.Background(), workspace, DeliveryGraphInput{
		Operation: GraphOpPrepareWave, DeliveryID: digestValue("delivery-graph-worktree"),
		WorktreeRef: "wt_1234567890abcdef",
	}); err != nil {
		t.Fatalf("deliveryGraph() error = %v", err)
	}
	if inspectedRef != "wt_1234567890abcdef" {
		t.Fatalf("inspected worktree = %q", inspectedRef)
	}
	if seen.WorkspaceID != "ws_fixture" || seen.WorkspaceRoot != "/absolute/delivery-worktree" {
		t.Fatalf("service scope = %#v, want workspace identity with delivery worktree root", seen)
	}
}

func TestDeliveryGraphHandlerRejectsForeignOrMissingWorktree(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name    string
		inspect routingWorktreeInspectFunc
		ref     string
	}{
		{name: "missing ref", ref: "", inspect: func(context.Context, publication.TrustedScope, string) (publication.WorktreeInspection, error) {
			return publication.WorktreeInspection{}, nil
		}},
		{name: "inspect error", ref: "wt_1234567890abcdef", inspect: func(context.Context, publication.TrustedScope, string) (publication.WorktreeInspection, error) {
			return publication.WorktreeInspection{}, errors.New("gone")
		}},
		{name: "foreign workspace", ref: "wt_1234567890abcdef", inspect: func(context.Context, publication.TrustedScope, string) (publication.WorktreeInspection, error) {
			return publication.WorktreeInspection{Worktree: publication.Worktree{
				ID: "wt_1234567890abcdef", WorkspaceID: "ws_other", Path: "/absolute/delivery-worktree",
			}}, nil
		}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			called := false
			app := application{services: serviceSet{inspectDeliveryWorktree: tt.inspect, deliveryGraph: func(
				context.Context,
				publication.TrustedScope,
				DeliveryGraphInput,
			) (DeliveryGraphOutput, error) {
				called = true
				return DeliveryGraphOutput{}, nil
			}}}
			workspace := &compozysdk.ExtensionToolWorkspaceScope{ID: "ws_fixture", Root: "/absolute/workspace"}
			if _, err := app.deliveryGraph(context.Background(), workspace, DeliveryGraphInput{
				Operation: GraphOpPrepareWave, DeliveryID: digestValue("delivery-graph-worktree"),
				WorktreeRef: tt.ref,
			}); err == nil {
				t.Fatal("deliveryGraph() error = nil, want worktree scope rejection")
			}
			if called {
				t.Fatal("delivery graph service called with unresolved worktree scope")
			}
		})
	}
}
