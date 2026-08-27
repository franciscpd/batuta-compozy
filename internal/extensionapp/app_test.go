package extensionapp

import (
	"context"
	"encoding/json"
	"errors"
	"path/filepath"
	"reflect"
	"testing"

	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/franciscpd/batuta-compozy/internal/publication"
)

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
	if describe.Name != "batuta" || describe.Version != "0.1.0-beta.5" {
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
	if len(descriptors) != 8 {
		t.Fatalf("len(descriptors) = %d, want 8", len(descriptors))
	}
	want := []struct {
		id       compozysdk.ToolID
		handler  string
		readOnly bool
		risk     compozysdk.RiskClass
	}{
		{id: "ext__batuta__delivery_budget_context", handler: "delivery_budget_context", readOnly: true, risk: compozysdk.RiskRead},
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
		if descriptor.Handler != "routing_apply" && schema["additionalProperties"] != false {
			t.Fatalf("descriptor[%d] additionalProperties = %#v", index, schema["additionalProperties"])
		}
		if descriptor.Handler == "routing_apply" && schema["oneOf"] == nil {
			t.Fatalf("descriptor[%d] routing apply schema has no closed union", index)
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
