package extensionapp

import (
	"context"
	"encoding/json"
	"reflect"
	"strings"
	"testing"

	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/franciscpd/batuta-compozy/internal/publication"
)

func TestDeliveryGraphToolExposesEightClosedOperations(t *testing.T) {
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
	if !ok || len(variants) != 8 {
		t.Fatalf("delivery graph schema variants = %#v", schema["oneOf"])
	}
	want := []GraphOperation{
		GraphOpPrepareWave,
		GraphOpTaskContext,
		GraphOpRecordQuestion,
		GraphOpRecordAnswer,
		GraphOpRecordCandidate,
		GraphOpRecordFailure,
		GraphOpSettleWave,
		GraphOpCleanup,
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
		if !containsAny(required, "operation") || !containsAny(required, "delivery_id") {
			t.Fatalf("variant[%d] required = %#v", index, required)
		}
	}
	payload := string(descriptor.InputSchema)
	for _, required := range []string{"maximum\":4", "maxLength\":2048", "maxItems\":4"} {
		if !strings.Contains(payload, required) {
			t.Fatalf("delivery graph schema missing bound %q: %s", required, payload)
		}
	}
}

func TestDeliveryGraphRecordAnswerDerivesRequestIdentity(t *testing.T) {
	t.Parallel()

	input := DeliveryGraphInput{
		Operation: GraphOpRecordAnswer, DeliveryID: digestValue("delivery-derived-answer"),
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
	wantRequired := []string{"operation", "delivery_id", "wave", "task_id", "execution", "question_operation_id", "answer"}
	if !reflect.DeepEqual(required, wantRequired) {
		t.Fatalf("record_answer required = %#v, want %#v", required, wantRequired)
	}
	for _, legacy := range []string{"request_loop_run_id", "request_generation", "request_node_id", "request_item_index"} {
		if _, exists := properties[legacy]; exists {
			t.Fatalf("record_answer exposes caller-controlled %q", legacy)
		}
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
		Operation: GraphOpRecordQuestion, DeliveryID: digestValue("delivery-question-validation"),
		Wave: 1, TaskID: "task_01", Execution: 1, Prompt: "Choose the supported compatibility behavior",
		ContextDigest: digestValue("bounded-redacted-context"), Choices: []string{"Preserve compatibility", "Adopt new behavior"},
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
		Operation: GraphOpRecordCandidate, DeliveryID: digestValue("delivery-candidate-validation"),
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
