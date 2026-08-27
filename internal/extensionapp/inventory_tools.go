package extensionapp

import (
	"context"
	"errors"

	compozysdk "github.com/compozy/compozy/sdk/go"
	"github.com/franciscpd/batuta-compozy/internal/inventory"
)

func (a application) inventory(
	ctx context.Context,
	workspace *compozysdk.ExtensionToolWorkspaceScope,
) (compozysdk.ToolResult, error) {
	scope, err := trustedScope(workspace)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	if a.services.inventory == nil {
		return compozysdk.ToolResult{}, errors.New("batuta: executor inventory is unavailable")
	}
	snapshot, err := a.services.inventory(ctx, scope)
	if err != nil {
		return compozysdk.ToolResult{}, err
	}
	return compozysdk.StructuredResult(inventory.Redact(snapshot))
}

func inventoryInputSchema() map[string]any {
	return objectSchema(nil, map[string]any{})
}

func inventoryOutputSchema() map[string]any {
	return objectSchema([]string{"schema_version", "executors", "digest"}, map[string]any{
		"schema_version":             map[string]any{"type": "integer", "minimum": 1},
		"compozy_catalog_generation": map[string]any{"type": "string"},
		"executors":                  map[string]any{"type": "array", "items": map[string]any{"type": "object"}},
		"digest":                     map[string]any{"type": "string", "pattern": "^sha256:[0-9a-f]{64}$"},
	})
}
