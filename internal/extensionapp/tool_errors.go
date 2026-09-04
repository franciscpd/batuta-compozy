package extensionapp

import (
	"context"
	"errors"

	compozysdk "github.com/compozy/compozy/sdk/go"

	"github.com/batuta-ai/core/routing"
	"github.com/franciscpd/batuta-compozy/internal/worktreeops"
)

// guardToolErrors turns a handler error into a typed tool execution error.
// A bare Go error reaches the daemon as an opaque internal failure and is
// reported to the agent as backend_unhealthy, hiding the actual message.
func guardToolErrors[T any](handler compozysdk.ToolHandlerFunc[T]) compozysdk.ToolHandlerFunc[T] {
	return func(ctx context.Context, req compozysdk.ToolRequest[T]) (compozysdk.ToolResult, error) {
		result, err := handler(ctx, req)
		if err == nil {
			return result, nil
		}
		return result, toolExecutionError(err)
	}
}

func toolExecutionError(err error) error {
	if rpcErr, ok := errors.AsType[*compozysdk.RPCError](err); ok {
		return rpcErr
	}
	return compozysdk.NewToolExecutionError(map[string]any{
		"code":    toolErrorCode(err),
		"message": err.Error(),
	})
}

func toolErrorCode(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "tool_canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "tool_timed_out"
	case errors.Is(err, routing.ErrDeliveryConflict),
		errors.Is(err, routing.ErrOwnershipUnproven),
		errors.Is(err, routing.ErrInvalidDeliveryTransition),
		errors.Is(err, routing.ErrDependencyBlocked),
		errors.Is(err, routing.ErrNoEligibleCandidate),
		errors.Is(err, worktreeops.ErrInvalidWorktreeIdentity):
		return "tool_conflict"
	default:
		return "tool_backend_failed"
	}
}
