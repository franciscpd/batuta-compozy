package extensionapp

import (
	"context"
	"encoding/json"
	"errors"
	"testing"

	compozysdk "github.com/compozy/compozy/sdk/go"

	"github.com/franciscpd/batuta-compozy/internal/routing"
)

func TestGuardToolErrorsReportsTypedToolExecutionErrors(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name string
		err  error
		code string
	}{
		{name: "journal conflict", err: routing.ErrOwnershipUnproven, code: "tool_conflict"},
		{name: "canceled", err: context.Canceled, code: "tool_canceled"},
		{name: "other", err: errors.New("batuta: publisher is unavailable"), code: "tool_backend_failed"},
	} {
		handler := guardToolErrors(func(context.Context, compozysdk.ToolRequest[struct{}]) (compozysdk.ToolResult, error) {
			return compozysdk.ToolResult{}, tc.err
		})
		_, err := handler(context.Background(), compozysdk.ToolRequest[struct{}]{})
		rpcErr, ok := errors.AsType[*compozysdk.RPCError](err)
		if !ok || rpcErr == nil {
			t.Fatalf("%s: error = %#v, want *compozysdk.RPCError", tc.name, err)
		}
		var data map[string]any
		if err := json.Unmarshal(rpcErr.Data, &data); err != nil || data["code"] != tc.code || data["message"] != tc.err.Error() {
			t.Fatalf("%s: data = %s (%v), want code %q message %q", tc.name, rpcErr.Data, err, tc.code, tc.err.Error())
		}
	}
	handler := guardToolErrors(func(context.Context, compozysdk.ToolRequest[struct{}]) (compozysdk.ToolResult, error) {
		return compozysdk.ToolResult{Preview: "ok"}, nil
	})
	if result, err := handler(context.Background(), compozysdk.ToolRequest[struct{}]{}); err != nil || result.Preview != "ok" {
		t.Fatalf("success passthrough = %#v, %v", result, err)
	}
}
