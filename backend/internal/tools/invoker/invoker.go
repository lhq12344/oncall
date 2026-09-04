package invoker

import (
	"context"
	"fmt"
	"strings"

	"go_agent/internal/events"
	"go_agent/internal/telemetry"
	"go_agent/internal/tools/policy"
)

type Executor interface {
	Execute(context.Context, map[string]any) ToolResult
}

type ToolCall struct {
	ID               string
	RunID            string
	TraceID          string
	ToolID           string
	ToolVersion      string
	Capability       string
	Risk             policy.ToolRisk
	Args             map[string]any
	ApprovedSnapshot *policy.ApprovalSnapshot
	Executor         Executor
}

type Invoker struct {
	Policy    *policy.Engine
	Events    *events.Emitter
	Telemetry *telemetry.Recorder
	MaxOutput int
}

func New(policyEngine *policy.Engine, emitter *events.Emitter, recorder *telemetry.Recorder) *Invoker {
	if policyEngine == nil {
		policyEngine = policy.NewEngine("")
	}
	return &Invoker{Policy: policyEngine, Events: emitter, Telemetry: recorder, MaxOutput: 10000}
}

func (i *Invoker) Invoke(ctx context.Context, call ToolCall) (ToolResult, policy.Decision) {
	if i == nil {
		i = New(nil, nil, nil)
	}
	if call.Executor == nil {
		decision := policy.Decision{Effect: policy.Deny, ReasonCode: "missing_executor"}
		return Error("tool executor is required"), decision
	}
	if call.ToolVersion == "" {
		call.ToolVersion = "v1"
	}
	_ = emit(i.Events, ctx, events.EventToolRequested, map[string]any{"tool_id": call.ToolID})
	decision := i.Policy.Decide(ctx, policy.Request{ToolID: call.ToolID, ToolVersion: call.ToolVersion, Capability: call.Capability, Risk: call.Risk, Args: call.Args, Approved: call.ApprovedSnapshot})
	if decision.Effect != policy.Allow {
		if decision.Effect == policy.Ask {
			_ = emit(i.Events, ctx, events.EventApprovalRequired, map[string]any{"tool_id": call.ToolID, "reason": decision.ReasonCode})
		}
		return Error(decision.ReasonCode), decision
	}
	_ = emit(i.Events, ctx, events.EventToolStarted, map[string]any{"tool_id": call.ToolID})
	finish := func(error) {}
	if i.Telemetry != nil {
		info := telemetry.ContextFrom(ctx)
		if info.TraceID == "" {
			info.TraceID = call.TraceID
		}
		if info.RunID == "" {
			info.RunID = call.RunID
		}
		info.Recorder = i.Telemetry
		ctx = telemetry.WithContext(ctx, info)
		finish = i.Telemetry.StartContext(ctx, "tool.invoke", map[string]string{"tool_id": call.ToolID})
	}
	result := call.Executor.Execute(ctx, call.Args)
	result.Output = redactAndBudget(result.Output, i.MaxOutput)
	var err error
	if result.IsError {
		err = fmt.Errorf("%s", result.Output)
	}
	finish(err)
	_ = emit(i.Events, ctx, events.EventToolResult, map[string]any{"tool_id": call.ToolID, "is_error": result.IsError, "artifact_ref": result.ArtifactRef})
	return result, decision
}

func emit(emitter *events.Emitter, ctx context.Context, typ events.EventType, payload map[string]any) error {
	if emitter == nil {
		return nil
	}
	_, err := emitter.Emit(ctx, typ, payload)
	return err
}

func redactAndBudget(output string, max int) string {
	for _, marker := range []string{"password", "secret", "token", "api_key", "apikey"} {
		output = strings.ReplaceAll(output, marker+"=", marker+"=[redacted]")
	}
	if max <= 0 || len(output) <= max {
		return output
	}
	return output[:max] + "\n[truncated]"
}
