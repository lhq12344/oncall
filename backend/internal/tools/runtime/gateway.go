package runtime

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go_agent/internal/hooks"
	"go_agent/internal/telemetry"
	"go_agent/internal/tools/policy/permissions"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

type ToolSearchTool struct{ Catalog *DeferredGatewayCatalog }

func (t *ToolSearchTool) Name() string           { return "ToolSearch" }
func (t *ToolSearchTool) Description() string    { return ToolSearchDescription }
func (t *ToolSearchTool) Category() ToolCategory { return CategoryRead }
func (t *ToolSearchTool) Schema() map[string]any {
	return schemaMap(t.Name(), t.Description(), map[string]any{
		"query":       map[string]any{"type": "string", "description": "select:ToolName for exact selection, or keyword search"},
		"max_results": map[string]any{"type": "integer", "description": "Maximum search results", "default": 5},
	}, []string{"query"})
}
func (t *ToolSearchTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	if t.Catalog == nil {
		return errorResult("Error: deferred gateway catalog is required")
	}
	query, _ := args["query"].(string)
	query = strings.TrimSpace(query)
	if query == "" {
		return errorResult("Error: query is required")
	}
	maxResults := intArg(args, "max_results", 5)
	if maxResults < 1 {
		maxResults = 5
	}
	if maxResults > 20 {
		maxResults = 20
	}
	var schemas []map[string]any
	if strings.HasPrefix(query, "select:") {
		names := strings.Split(strings.TrimPrefix(query, "select:"), ",")
		for i := range names {
			names[i] = strings.TrimSpace(names[i])
		}
		schemas = t.Catalog.FindDeferredByNames(names)
	} else {
		schemas = t.Catalog.SearchDeferred(query, maxResults)
	}
	if len(schemas) == 0 {
		names := t.Catalog.GetDeferredToolNames(ctx)
		if len(names) == 0 {
			return ToolResult{Output: fmt.Sprintf("No deferred tools available for query %q.", query)}
		}
		return ToolResult{Output: fmt.Sprintf("No matching deferred tools found for query %q. Available deferred tools: %s", query, strings.Join(names, ", "))}
	}
	for _, s := range schemas {
		if name, ok := s["name"].(string); ok {
			t.Catalog.MarkDiscovered(ctx, name)
		}
	}
	out, _ := json.MarshalIndent(schemas, "", "  ")
	return ToolResult{Output: fmt.Sprintf("Found %d tool(s). Use InvokeDeferredTool with one of these tool names and matching arguments:\n%s", len(schemas), string(out))}
}

type InvokeDeferredTool struct {
	Catalog    *DeferredGatewayCatalog
	Checker    *permissions.Checker
	HookEngine *hooks.Engine
}

func (t *InvokeDeferredTool) Name() string           { return "InvokeDeferredTool" }
func (t *InvokeDeferredTool) Description() string    { return InvokeDeferredToolDescription }
func (t *InvokeDeferredTool) Category() ToolCategory { return CategoryCommand }
func (t *InvokeDeferredTool) Schema() map[string]any {
	return schemaMap(t.Name(), t.Description(), map[string]any{
		"tool_name": map[string]any{"type": "string", "description": "Discovered deferred tool name"},
		"arguments": map[string]any{"type": "object", "description": "Arguments object for the target tool"},
	}, []string{"tool_name", "arguments"})
}
func (t *InvokeDeferredTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	if t.Catalog == nil {
		return errorResult("Error: deferred gateway catalog is required")
	}
	toolName, _ := args["tool_name"].(string)
	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		return errorResult("Error: tool_name is required")
	}
	target := t.Catalog.Get(toolName)
	if target == nil || !t.Catalog.IsDeferred(toolName) {
		return errorResult(fmt.Sprintf("Error: deferred tool not found: %s", toolName))
	}
	if !t.Catalog.IsDiscovered(ctx, toolName) {
		return errorResult(fmt.Sprintf("Error: deferred tool %s has not been discovered in the current session. Use ToolSearch first.", toolName))
	}
	targetArgs, ok := mapArg(args["arguments"])
	if !ok {
		return errorResult("Error: arguments must be an object")
	}
	if result := runPreToolHooks(ctx, t.HookEngine, toolName, targetArgs); result.IsError {
		return result
	}
	checker := permissionChecker(t.Checker)
	if decision := checker.Check(toolName, targetArgs); decision.Effect != permissions.Allow {
		runApprovalRequestedHook(ctx, t.HookEngine, toolName, targetArgs, decision.Reason)
		result := permissionDecisionResult(ctx, checker, toolName, targetArgs, decision)
		if result.IsError || result.Output != "__ONCALL_PERMISSION_APPROVED__" {
			runPostToolHooks(ctx, t.HookEngine, toolName, targetArgs, result)
			return result
		}
	}
	result := target.Execute(ctx, targetArgs)
	runPostToolHooks(ctx, t.HookEngine, toolName, targetArgs, result)
	return result
}

type DeferredEinoTool struct {
	Base   einotool.BaseTool
	name   string
	desc   string
	schema map[string]any
}

func NewDeferredEinoTool(ctx context.Context, base einotool.BaseTool) *DeferredEinoTool {
	info, _ := base.Info(ctx)
	name := ""
	desc := ""
	var params any
	if info != nil {
		name = info.Name
		desc = info.Desc
		params = info.ParamsOneOf
	}
	return &DeferredEinoTool{Base: base, name: name, desc: desc, schema: map[string]any{"name": name, "description": desc, "input_schema": params}}
}
func (t *DeferredEinoTool) Name() string           { return t.name }
func (t *DeferredEinoTool) Description() string    { return t.desc }
func (t *DeferredEinoTool) Category() ToolCategory { return CategoryCommand }
func (t *DeferredEinoTool) Schema() map[string]any { return t.schema }
func (t *DeferredEinoTool) ShouldDefer() bool      { return true }
func (t *DeferredEinoTool) Execute(ctx context.Context, args map[string]any) ToolResult {
	invokable, ok := t.Base.(einotool.InvokableTool)
	if !ok {
		return errorResult(fmt.Sprintf("Error: tool %s is not invokable", t.Name()))
	}
	payload, err := json.Marshal(args)
	if err != nil {
		return errorResult(fmt.Sprintf("Error: invalid arguments: %s", err))
	}
	finish := func(error) {}
	if info := telemetry.ContextFrom(ctx); info.Recorder != nil {
		finish = info.Recorder.StartContext(ctx, "tool.invoke", map[string]string{"tool_id": t.Name()})
	}
	out, err := invokable.InvokableRun(ctx, string(payload))
	if err != nil {
		finish(err)
		return errorResult(err.Error())
	}
	finish(nil)
	return ToolResult{Output: out}
}

type ToolApprovalInterruptInfo struct {
	ToolName string
	Args     map[string]any
	Reason   string
}

func (i *ToolApprovalInterruptInfo) String() string {
	if i == nil {
		return "工具调用需要审批。"
	}
	return fmt.Sprintf("工具 %s 需要审批：%s", i.ToolName, i.Reason)
}

func permissionDecisionResult(ctx context.Context, checker *permissions.Checker, toolName string, args map[string]any, decision permissions.Decision) ToolResult {
	if decision.Effect == permissions.Deny {
		return errorResult("permission denied: " + decision.Reason)
	}
	wasInterrupted, _, _ := einotool.GetInterruptState[any](ctx)
	if !wasInterrupted {
		err := einotool.Interrupt(ctx, &ToolApprovalInterruptInfo{ToolName: toolName, Args: args, Reason: decision.Reason})
		if err != nil {
			return errorResult(err.Error())
		}
		return errorResult("permission approval required")
	}
	isTarget, hasData, resumeData := einotool.GetResumeContext[map[string]any](ctx)
	if !isTarget || !hasData {
		err := einotool.Interrupt(ctx, &ToolApprovalInterruptInfo{ToolName: toolName, Args: args, Reason: decision.Reason})
		if err != nil {
			return errorResult(err.Error())
		}
		return errorResult("permission approval required")
	}
	approved, allowAlways := parseApprovalDecision(resumeData)
	if !approved {
		return errorResult("tool execution rejected by user")
	}
	if allowAlways {
		_ = checker.AllowAlways(toolName, args)
	}
	return ToolResult{Output: "__ONCALL_PERMISSION_APPROVED__"}
}

func parseApprovalDecision(data map[string]any) (bool, bool) {
	approved := false
	allowAlways := false
	for _, key := range []string{"approved", "allow", "confirmed"} {
		if b, ok := boolFromAny(data[key]); ok {
			approved = b
			break
		}
	}
	for _, key := range []string{"allow_always", "always_allow", "remember", "dont_ask_again"} {
		if b, ok := boolFromAny(data[key]); ok {
			allowAlways = b
			break
		}
	}
	return approved, allowAlways
}

func boolFromAny(value any) (bool, bool) {
	switch v := value.(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "y", "ok", "approved", "confirm", "confirmed":
			return true, true
		case "false", "0", "no", "n", "reject", "rejected":
			return false, true
		}
	}
	return false, false
}

func mapArg(value any) (map[string]any, bool) {
	switch v := value.(type) {
	case map[string]any:
		return v, true
	case string:
		var out map[string]any
		if err := json.Unmarshal([]byte(v), &out); err == nil {
			return out, true
		}
	}
	return nil, false
}

func permissionChecker(checker *permissions.Checker) *permissions.Checker {
	if checker != nil {
		return checker
	}
	return permissions.NewChecker(permissions.Options{})
}

func paramInfoFromSchema(prop map[string]any, required bool) *schema.ParameterInfo {
	typ, _ := prop["type"].(string)
	desc, _ := prop["description"].(string)
	info := &schema.ParameterInfo{Desc: desc, Required: required}
	switch typ {
	case "string":
		info.Type = schema.String
	case "integer", "number":
		info.Type = schema.Integer
	case "array":
		info.Type = schema.Array
		info.ElemInfo = &schema.ParameterInfo{Type: schema.String}
	case "object":
		info.Type = schema.Object
	case "boolean":
		info.Type = schema.Boolean
	default:
		info.Type = schema.String
	}
	return info
}
