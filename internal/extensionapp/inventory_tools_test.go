package extensionapp

import (
	"context"
	"encoding/json"
	"path/filepath"
	"testing"

	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/franciscpd/batuta-compozy/internal/inventory"
	"github.com/franciscpd/batuta-compozy/internal/publication"
)

func TestInventoryToolHasNoCallerControlledPathsOrCommands(t *testing.T) {
	t.Parallel()

	extension, err := newWithServices(serviceSet{})
	if err != nil {
		t.Fatalf("newWithServices() error = %v", err)
	}
	descriptor := descriptorForHandler(t, extension, "executor_inventory")
	var schema map[string]any
	if err := json.Unmarshal(descriptor.InputSchema, &schema); err != nil {
		t.Fatalf("input schema: %v", err)
	}
	properties, ok := schema["properties"].(map[string]any)
	if !ok || len(properties) != 0 || schema["additionalProperties"] != false {
		t.Fatalf("inventory input schema = %#v, want closed empty object", schema)
	}
	if !descriptor.ReadOnly || descriptor.Risk != compozysdk.RiskRead {
		t.Fatalf("inventory descriptor = %#v, want read-only/read risk", descriptor)
	}
}

func TestInventoryHandlerRequiresTrustedWorkspaceAndReturnsSnapshot(t *testing.T) {
	t.Parallel()

	want := inventory.InventorySnapshot{SchemaVersion: inventory.SchemaVersion, Digest: "sha256:inventory"}
	var seen publication.TrustedScope
	app := application{services: serviceSet{inventory: func(_ context.Context, scope publication.TrustedScope) (inventory.InventorySnapshot, error) {
		seen = scope
		return want, nil
	}}}
	if _, err := app.inventory(context.Background(), nil); err == nil {
		t.Fatal("inventory(nil workspace) error = nil")
	}
	root := filepath.Join(string(filepath.Separator), "workspace")
	result, err := app.inventory(context.Background(), &compozysdk.ExtensionToolWorkspaceScope{ID: "ws_demo", Root: root})
	if err != nil {
		t.Fatalf("inventory() error = %v", err)
	}
	if seen.WorkspaceID != "ws_demo" || seen.WorkspaceRoot != root {
		t.Fatalf("trusted scope = %#v", seen)
	}
	var got inventory.InventorySnapshot
	if err := json.Unmarshal(result.Structured, &got); err != nil {
		t.Fatalf("structured result: %v", err)
	}
	if got.Digest != want.Digest {
		t.Fatalf("snapshot digest = %q, want %q", got.Digest, want.Digest)
	}
}

func descriptorForHandler(t *testing.T, extension *compozysdk.Extension, handler string) compozysdk.ExtensionToolRuntimeDescriptor {
	t.Helper()
	for _, descriptor := range extension.GetToolDescriptors() {
		if descriptor.Handler == handler {
			return descriptor
		}
	}
	t.Fatalf("descriptor %q not registered", handler)
	return compozysdk.ExtensionToolRuntimeDescriptor{}
}
