package eino

import (
	"context"
	"encoding/json"
	"fmt"

	"go_agent/internal/tools/invoker"

	einotool "github.com/cloudwego/eino/components/tool"
)

type Adapter struct {
	Base einotool.BaseTool
}

func (a Adapter) Execute(ctx context.Context, args map[string]any) invoker.ToolResult {
	invokable, ok := a.Base.(einotool.InvokableTool)
	if !ok {
		return invoker.Error("tool is not invokable")
	}
	payload, err := json.Marshal(args)
	if err != nil {
		return invoker.Error(fmt.Sprintf("invalid arguments: %v", err))
	}
	out, err := invokable.InvokableRun(ctx, string(payload))
	if err != nil {
		return invoker.Error(err.Error())
	}
	return invoker.ToolResult{Output: out}
}
