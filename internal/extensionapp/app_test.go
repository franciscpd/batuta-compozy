package extensionapp

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/batuta-ai/core/publication"
	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/batuta-ai/compozy/internal/worktreeops"
)

func TestApplicationWiringUsesManagedWorktreeClientAndDeliveryGraphTool(t *testing.T) {
	t.Parallel()

	runner := &applicationWiringRunner{}
	client, ok := newTaskWorktreeClient("/controlled/compozy", runner).(worktreeops.CLIClient)
	if !ok || client.Executable != "/controlled/compozy" || client.Runner != runner {
		t.Fatalf("newTaskWorktreeClient() = %#v", client)
	}
	extension, err := newWithServices(serviceSet{worktrees: client})
	if err != nil {
		t.Fatalf("newWithServices() error = %v", err)
	}
	if got := len(extension.GetToolDescriptors()); got != 9 {
		t.Fatalf("tool descriptor count = %d, want 9", got)
	}
}

func TestApplicationDiscoversAbsoluteClaudeAndAgyExecutables(t *testing.T) {
	directory := t.TempDir()
	for _, name := range []string{"claude", "agy"} {
		path := filepath.Join(directory, name)
		if err := os.WriteFile(path, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
	}
	t.Setenv("PATH", directory)
	got := discoverInventoryExecutables("/opt/compozy")
	if got.Claude != filepath.Join(directory, "claude") || got.Agy != filepath.Join(directory, "agy") {
		t.Fatalf("inventory executables = %#v, want absolute Claude/Agy paths", got)
	}
}

func TestOptionalExecutableFallsBackToMiseWhenPathCandidateCannotRun(t *testing.T) {
	directory := t.TempDir()
	brokenPath := filepath.Join(directory, "codex")
	if err := os.WriteFile(brokenPath, []byte("#!/bin/sh\nexit 127\n"), 0o700); err != nil {
		t.Fatalf("write broken codex: %v", err)
	}
	misePath := filepath.Join(directory, "mise")
	miseScript := `#!/bin/sh
if [ "$1" = "which" ] && [ "$2" = "codex" ]; then
  printf '%s\n' "$FAKE_MISE_TARGET"
  exit 0
fi
exit 64
`
	if err := os.WriteFile(misePath, []byte(miseScript), 0o700); err != nil {
		t.Fatalf("write mise: %v", err)
	}
	targetPath := filepath.Join(directory, "mise-codex")
	if err := os.WriteFile(targetPath, []byte("#!/bin/sh\nexit 0\n"), 0o700); err != nil {
		t.Fatalf("write mise codex: %v", err)
	}
	t.Setenv("PATH", directory)
	t.Setenv("FAKE_MISE_TARGET", targetPath)

	if got := optionalExecutable("codex"); got != targetPath {
		t.Fatalf("optionalExecutable(codex) = %q, want mise target %q", got, targetPath)
	}
}

type applicationWiringRunner struct{}

func (*applicationWiringRunner) Run(context.Context, publication.Command) (publication.CommandResult, error) {
	return publication.CommandResult{}, errors.New("unexpected command")
}

func TestDescribeRegistersPublicationInventoryAndRoutingTools(t *testing.T) {
	t.Parallel()

	extension, err := newWithServices(serviceSet{})
	if err != nil {
		t.Fatalf("newWithServices() error = %v", err)
	}
	describe, err := extension.Describe()
	if err != nil {
		t.Fatalf("Describe() error = %v", err)
	}
	if describe.Name != "batuta" || describe.Version != "0.1.0-beta.6" {
		t.Fatalf("identity = %q@%q", describe.Name, describe.Version)
	}
	if !reflect.DeepEqual(describe.Resources.Agents, []compozysdk.DescribeResourcePath{{Path: "agents"}}) ||
		!reflect.DeepEqual(describe.Resources.Skills, []compozysdk.DescribeResourcePath{{Path: "resources/skills"}}) ||
		!reflect.DeepEqual(describe.Resources.Loops, []compozysdk.DescribeResourcePath{{Path: "loops"}}) {
		t.Fatalf("resources = %#v", describe.Resources)
	}
	if describe.Subprocess.Command != "./bin" ||
		describe.Subprocess.Env["COMPOZY_EXECUTABLE"] != "{{compozy_executable}}" ||
		describe.Subprocess.Env["COMPOZY_HOME"] != "{{env:COMPOZY_HOME}}" {
		t.Fatalf("subprocess = %#v", describe.Subprocess)
	}

	descriptors := extension.GetToolDescriptors()
	if len(descriptors) != 9 {
		t.Fatalf("len(descriptors) = %d, want 9", len(descriptors))
	}
	want := []struct {
		id       compozysdk.ToolID
		handler  string
		readOnly bool
		risk     compozysdk.RiskClass
	}{
		{id: "ext__batuta__delivery_budget_context", handler: "delivery_budget_context", readOnly: true, risk: compozysdk.RiskRead},
		{id: "ext__batuta__delivery_graph", handler: "delivery_graph", risk: compozysdk.RiskMutating},
		{id: "ext__batuta__executor_inventory", handler: "executor_inventory", readOnly: true, risk: compozysdk.RiskRead},
		{id: "ext__batuta__publication_plan", handler: "publication_plan", readOnly: true, risk: compozysdk.RiskRead},
		{id: "ext__batuta__publication_verify", handler: "publication_verify", readOnly: true, risk: compozysdk.RiskRead},
		{id: "ext__batuta__publish_worktree", handler: "publish_worktree", risk: compozysdk.RiskMutating},
		{id: "ext__batuta__routing_apply", handler: "routing_apply", risk: compozysdk.RiskMutating},
		{id: "ext__batuta__routing_context", handler: "routing_context", readOnly: true, risk: compozysdk.RiskRead},
		{id: "ext__batuta__routing_plan", handler: "routing_plan", readOnly: true, risk: compozysdk.RiskRead},
	}
	for index, descriptor := range descriptors {
		if descriptor.ID != want[index].id || descriptor.Handler != want[index].handler ||
			descriptor.ReadOnly != want[index].readOnly || descriptor.Risk != want[index].risk {
			t.Fatalf("descriptor[%d] = %#v, want %#v", index, descriptor, want[index])
		}
		var schema map[string]any
		if err := json.Unmarshal(descriptor.InputSchema, &schema); err != nil {
			t.Fatalf("descriptor[%d] schema: %v", index, err)
		}
		closedUnion := descriptor.Handler == "routing_apply" || descriptor.Handler == "delivery_graph"
		if !closedUnion && schema["additionalProperties"] != false {
			t.Fatalf("descriptor[%d] additionalProperties = %#v", index, schema["additionalProperties"])
		}
		if closedUnion && schema["oneOf"] == nil {
			t.Fatalf("descriptor[%d] %s schema has no closed union", index, descriptor.Handler)
		}
		if schema["type"] != "object" {
			t.Fatalf("descriptor[%d] %s schema root type = %#v, want \"object\"", index, descriptor.Handler, schema["type"])
		}
		assertNoNullRequired(t, descriptor.Handler, schema)
	}
}

// assertNoNullRequired walks a schema tree and fails on "required": null,
// which MCP clients reject for the whole tools/list result.
func assertNoNullRequired(t *testing.T, handler string, node any) {
	t.Helper()
	switch value := node.(type) {
	case map[string]any:
		if required, present := value["required"]; present && required == nil {
			t.Fatalf("%s schema contains \"required\": null", handler)
		}
		for _, child := range value {
			assertNoNullRequired(t, handler, child)
		}
	case []any:
		for _, child := range value {
			assertNoNullRequired(t, handler, child)
		}
	}
}

func TestDescribeDeliveryGraphDerivesInteractiveRequestIdentity(t *testing.T) {
	t.Parallel()

	extension, err := newWithServices(serviceSet{})
	if err != nil {
		t.Fatalf("newWithServices() error = %v", err)
	}
	var schema map[string]any
	for _, descriptor := range extension.GetToolDescriptors() {
		if descriptor.Handler == "delivery_graph" {
			if err := json.Unmarshal(descriptor.InputSchema, &schema); err != nil {
				t.Fatalf("delivery graph schema: %v", err)
			}
			break
		}
	}
	if schema == nil {
		t.Fatal("delivery graph descriptor is missing")
	}
	answer := schema["oneOf"].([]any)[3].(map[string]any)
	properties := answer["properties"].(map[string]any)
	for _, field := range []string{"request_loop_run_id", "request_generation", "request_node_id", "request_item_index"} {
		if _, exists := properties[field]; exists {
			t.Fatalf("published record_answer schema permits caller-controlled %q", field)
		}
	}
}

func TestHandlersRequireDaemonTrustedWorkspace(t *testing.T) {
	t.Parallel()

	app := application{services: serviceSet{}}
	for name, test := range map[string]func() error{
		"plan": func() error {
			_, err := app.plan(context.Background(), nil, publication.PlanInput{WorktreeRef: "wt_delivery"})
			return err
		},
		"publish": func() error {
			_, err := app.publish(context.Background(), nil, publication.PublishInput{WorktreeRef: "wt_delivery"})
			return err
		},
		"verify": func() error {
			_, err := app.verify(context.Background(), nil, publication.VerifyInput{WorktreeRef: "wt_delivery"})
			return err
		},
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()
			if err := test(); err == nil {
				t.Fatal("handler error = nil, want trusted workspace rejection")
			}
		})
	}
}

func TestPlanHandlerPassesOnlyTrustedScopeAndReturnsBlockedRoutingData(t *testing.T) {
	t.Parallel()

	var seen publication.TrustedScope
	app := application{services: serviceSet{
		plan: func(_ context.Context, scope publication.TrustedScope, input publication.PlanInput) (publication.PlanOutput, error) {
			seen = scope
			blocked := publication.PlanOutput{
				Disposition: publication.DispositionBlocked,
				WorktreeID:  input.WorktreeRef,
				Blockers:    []string{"forge_unavailable"},
			}
			return blocked, &publication.BlockedPlanError{Plan: blocked}
		},
	}}
	root := filepath.Join(string(filepath.Separator), "workspace")
	result, err := app.plan(
		context.Background(),
		&compozysdk.ExtensionToolWorkspaceScope{ID: "ws_demo", Root: root},
		publication.PlanInput{WorktreeRef: "wt_delivery"},
	)
	if err != nil {
		t.Fatalf("plan() error = %v", err)
	}
	if !reflect.DeepEqual(seen, publication.TrustedScope{WorkspaceID: "ws_demo", WorkspaceRoot: root}) {
		t.Fatalf("scope = %#v", seen)
	}
	var output publication.PlanOutput
	if err := json.Unmarshal(result.Structured, &output); err != nil {
		t.Fatalf("structured result: %v", err)
	}
	if output.Disposition != publication.DispositionBlocked ||
		!reflect.DeepEqual(output.Blockers, []string{"forge_unavailable"}) {
		t.Fatalf("output = %#v", output)
	}
}

func TestPlanHandlerKeepsUnexpectedFailuresAsErrors(t *testing.T) {
	t.Parallel()

	want := errors.New("transport unavailable")
	app := application{services: serviceSet{
		plan: func(context.Context, publication.TrustedScope, publication.PlanInput) (publication.PlanOutput, error) {
			return publication.PlanOutput{}, want
		},
	}}
	_, err := app.plan(
		context.Background(),
		&compozysdk.ExtensionToolWorkspaceScope{ID: "ws_demo", Root: "/workspace"},
		publication.PlanInput{WorktreeRef: "wt_delivery"},
	)
	if !errors.Is(err, want) {
		t.Fatalf("plan() error = %v, want %v", err, want)
	}
}
