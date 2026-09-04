package invoker

import (
	"context"
	"strings"
	"testing"

	"go_agent/internal/events"
	"go_agent/internal/telemetry"
	"go_agent/internal/tools/policy"
)

type fakeExecutor struct{ result ToolResult }

func (f fakeExecutor) Execute(context.Context, map[string]any) ToolResult { return f.result }

func TestInvokerEmitsEventsAndRedactsOutput(t *testing.T) {
	eventSink := &events.MemorySink{}
	emitter, err := events.NewEmitter("run", "trace", eventSink)
	if err != nil {
		t.Fatalf("NewEmitter: %v", err)
	}
	telemetrySink := telemetry.NewMemorySink()
	result, decision := New(policy.NewEngine(""), emitter, telemetry.NewRecorder(telemetrySink)).Invoke(context.Background(), ToolCall{
		ID:          "tool-call",
		TraceID:     "trace",
		ToolID:      "read",
		ToolVersion: "v1",
		Risk:        policy.RiskLow,
		Args:        map[string]any{"target": "pod"},
		Executor:    fakeExecutor{result: ToolResult{Output: "password=abc ok"}},
	})
	if decision.Effect != policy.Allow || result.IsError || strings.Contains(result.Output, "password=abc") {
		t.Fatalf("unexpected result/decision: %+v %+v", result, decision)
	}
	if got := len(eventSink.Events()); got != 3 {
		t.Fatalf("events=%d, want 3", got)
	}
	if got := len(telemetrySink.Spans()); got != 1 {
		t.Fatalf("spans=%d, want 1", got)
	}
}

func TestInvokerStopsBeforeMutationWithoutApproval(t *testing.T) {
	eventSink := &events.MemorySink{}
	emitter, _ := events.NewEmitter("run", "trace", eventSink)
	result, decision := New(policy.NewEngine(""), emitter, nil).Invoke(context.Background(), ToolCall{
		ToolID:     "execute_step",
		Capability: "execution.mutation",
		Risk:       policy.RiskHigh,
		Args:       map[string]any{"command": "kubectl restart"},
		Executor:   fakeExecutor{result: ToolResult{Output: "should not run"}},
	})
	if decision.Effect != policy.Ask || !result.IsError || strings.Contains(result.Output, "should not run") {
		t.Fatalf("mutation should stop before executor: %+v %+v", result, decision)
	}
	if got := len(eventSink.Events()); got != 2 {
		t.Fatalf("events=%d, want requested+approval", got)
	}
}

func TestInvokerUsesRequestTraceContext(t *testing.T) {
	sink := telemetry.NewMemorySink()
	ctx := telemetry.WithContext(context.Background(), telemetry.ContextInfo{TraceID: "request-trace", RunID: "request-run"})
	_, decision := New(policy.NewEngine(""), nil, telemetry.NewRecorder(sink)).Invoke(ctx, ToolCall{
		ID:          "tool-call",
		TraceID:     "fallback-trace",
		RunID:       "fallback-run",
		ToolID:      "read",
		ToolVersion: "v1",
		Risk:        policy.RiskLow,
		Executor:    fakeExecutor{result: ToolResult{Output: "ok"}},
	})
	if decision.Effect != policy.Allow {
		t.Fatalf("decision=%+v", decision)
	}
	spans := sink.Spans()
	if len(spans) != 1 || spans[0].TraceID != "request-trace" {
		t.Fatalf("spans=%+v", spans)
	}
}
