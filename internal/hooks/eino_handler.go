package hooks

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/callbacks"
)

func NewEinoCallbackHandler(engine *Engine, base HookContext) callbacks.Handler {
	builder := callbacks.NewHandlerBuilder()
	if engine == nil {
		return builder.Build()
	}
	return builder.
		OnStartFn(func(ctx context.Context, info *callbacks.RunInfo, input callbacks.CallbackInput) context.Context {
			engine.RunEvent(ctx, EventAgentStart, enrichRunInfo(base, info, map[string]any{
				"callback":   "on_start",
				"input_type": fmt.Sprintf("%T", input),
			}))
			return ctx
		}).
		OnEndFn(func(ctx context.Context, info *callbacks.RunInfo, output callbacks.CallbackOutput) context.Context {
			engine.RunEvent(ctx, EventAgentEnd, enrichRunInfo(base, info, map[string]any{
				"callback":    "on_end",
				"output_type": fmt.Sprintf("%T", output),
			}))
			return ctx
		}).
		OnErrorFn(func(ctx context.Context, info *callbacks.RunInfo, err error) context.Context {
			hctx := enrichRunInfo(base, info, map[string]any{"callback": "on_error"})
			if err != nil {
				hctx.Error = err.Error()
			}
			engine.RunEvent(ctx, EventAgentError, hctx)
			return ctx
		}).
		Build()
}

func enrichRunInfo(base HookContext, info *callbacks.RunInfo, metadata map[string]any) HookContext {
	hctx := base
	if hctx.Metadata == nil {
		hctx.Metadata = map[string]any{}
	} else {
		copied := make(map[string]any, len(hctx.Metadata)+len(metadata)+2)
		for key, value := range hctx.Metadata {
			copied[key] = value
		}
		hctx.Metadata = copied
	}
	for key, value := range metadata {
		hctx.Metadata[key] = value
	}
	if info != nil {
		hctx.AgentName = info.Name
		hctx.Component = string(info.Component)
		if info.Type != "" {
			hctx.Metadata["run_type"] = info.Type
		}
	}
	return hctx
}
