package toolkit

import (
	"context"
	"strings"

	"go_agent/internal/hooks"
)

func runPreToolHooks(ctx context.Context, engine *hooks.Engine, toolName string, args map[string]any) ToolResult {
	if engine == nil {
		return ToolResult{}
	}
	rejected, msg := engine.RunPreToolHooks(ctx, hooks.HookContext{
		ToolName: toolName,
		ToolArgs: args,
	})
	if !rejected {
		return ToolResult{}
	}
	msg = strings.TrimSpace(msg)
	if msg == "" {
		msg = "blocked by hook"
	}
	return errorResult("blocked by hook: " + msg)
}

func runPostToolHooks(ctx context.Context, engine *hooks.Engine, toolName string, args map[string]any, result ToolResult) {
	if engine == nil {
		return
	}
	event := hooks.EventToolPostUse
	hctx := hooks.HookContext{
		EventName: event,
		ToolName:  toolName,
		ToolArgs:  args,
		Result:    result.Output,
	}
	if result.IsError {
		event = hooks.EventToolError
		hctx.EventName = event
		hctx.Error = result.Output
	}
	engine.RunEvent(ctx, event, hctx)
}

func runApprovalRequestedHook(ctx context.Context, engine *hooks.Engine, toolName string, args map[string]any, reason string) {
	if engine == nil {
		return
	}
	engine.RunEvent(ctx, hooks.EventApprovalRequest, hooks.HookContext{
		ToolName: toolName,
		ToolArgs: args,
		Message:  reason,
		Metadata: map[string]any{"reason": reason},
	})
}
