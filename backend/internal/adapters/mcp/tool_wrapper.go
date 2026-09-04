package mcp

import (
	"context"

	coremcp "go_agent/internal/mcp"
	"go_agent/internal/tools/invoker"
)

type Caller func(context.Context, map[string]any) (string, error)

type ToolWrapper struct {
	Server string
	Tool   string
	Caller Caller
}

func (w ToolWrapper) Execute(ctx context.Context, args map[string]any) invoker.ToolResult {
	if err := coremcp.ValidateToolName(w.Server, w.Tool); err != nil {
		return invoker.Error(err.Error())
	}
	if w.Caller == nil {
		return invoker.Error("mcp server unavailable")
	}
	out, err := w.Caller(ctx, args)
	if err != nil {
		return invoker.Error(err.Error())
	}
	return invoker.ToolResult{Output: out}
}
