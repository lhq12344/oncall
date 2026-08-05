package toolkit

import (
	"context"
	"encoding/json"
	"fmt"

	"go_agent/internal/permissions"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type EinoAdapter struct {
	Tool    Tool
	Checker *permissions.Checker
}

func NewEinoAdapter(t Tool, checker *permissions.Checker) einotool.BaseTool {
	return &EinoAdapter{Tool: t, Checker: checker}
}

func (a *EinoAdapter) Info(ctx context.Context) (*schema.ToolInfo, error) {
	s := a.Tool.Schema()
	props := map[string]any{}
	requiredSet := map[string]bool{}
	if input, ok := s["input_schema"].(map[string]any); ok {
		if p, ok := input["properties"].(map[string]any); ok {
			props = p
		}
		if req, ok := input["required"].([]string); ok {
			for _, name := range req {
				requiredSet[name] = true
			}
		}
	}
	params := map[string]*schema.ParameterInfo{}
	for name, raw := range props {
		if prop, ok := raw.(map[string]any); ok {
			params[name] = paramInfoFromSchema(prop, requiredSet[name])
		}
	}
	return &schema.ToolInfo{Name: a.Tool.Name(), Desc: a.Tool.Description(), ParamsOneOf: schema.NewParamsOneOfByParams(params)}, nil
}

func (a *EinoAdapter) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
	var args map[string]any
	if err := json.Unmarshal([]byte(argumentsInJSON), &args); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}
	checker := permissionChecker(a.Checker)
	decision := checker.Check(a.Tool.Name(), args)
	if decision.Effect != permissions.Allow {
		result := permissionDecisionResult(ctx, checker, a.Tool.Name(), args, decision)
		if result.IsError || result.Output != "__ONCALL_PERMISSION_APPROVED__" {
			return marshalToolResult(result)
		}
	}
	result := a.Tool.Execute(ctx, args)
	return marshalToolResult(result)
}

func marshalToolResult(result ToolResult) (string, error) {
	if result.IsError {
		payload, _ := json.Marshal(map[string]any{"success": false, "error": result.Output})
		return string(payload), nil
	}
	return result.Output, nil
}

func NewDefaultRegistry(ctx context.Context, checker *permissions.Checker, deferredTools ...einotool.BaseTool) *Registry {
	fsc := NewFileStateCache()
	reg := NewRegistry()
	reg.Register(&ReadFileTool{FileStateCache: fsc})
	reg.Register(&EditFileTool{FileStateCache: fsc})
	reg.Register(&WriteFileTool{FileStateCache: fsc})
	reg.Register(&GlobTool{})
	reg.Register(&GrepTool{})
	for _, base := range deferredTools {
		if base == nil {
			continue
		}
		wrapped := NewDeferredEinoTool(ctx, base)
		if wrapped.Name() != "" {
			reg.RegisterDeferred(wrapped)
		}
	}
	reg.Register(&ToolSearchTool{Registry: reg})
	reg.Register(&InvokeDeferredTool{Registry: reg, Checker: checker})
	return reg
}

func BuildAlwaysEinoTools(ctx context.Context, checker *permissions.Checker, deferredTools ...einotool.BaseTool) []einotool.BaseTool {
	reg := NewDefaultRegistry(ctx, checker, deferredTools...)
	always := reg.ListAlways()
	out := make([]einotool.BaseTool, 0, len(always))
	for _, t := range always {
		out = append(out, NewEinoAdapter(t, checker))
	}
	return out
}
