package models

import (
	"context"
	"io"
	"strings"
	"testing"
	"time"

	"go_agent/internal/telemetry"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type fakeChatModel struct{}

func (fakeChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return schema.AssistantMessage("ok", nil), nil
}

func (fakeChatModel) Stream(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{schema.AssistantMessage("ok", nil)}), nil
}

func (fakeChatModel) WithTools(_ []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return fakeChatModel{}, nil
}

func TestFirstEnvOrValueUsesEnvironmentOverride(t *testing.T) {
	t.Setenv("ONCALL_CHAT_MODEL", "deepseek-v4-flash")
	t.Setenv("DS_QUICK_CHAT_MODEL", "fallback-env")

	if got := firstEnvOrValue("config-model", "ONCALL_CHAT_MODEL", "DS_QUICK_CHAT_MODEL"); got != "deepseek-v4-flash" {
		t.Fatalf("model override = %q, want deepseek-v4-flash", got)
	}
}

func TestFirstEnvOrValueFallsBackToTrimmedConfig(t *testing.T) {
	if got := firstEnvOrValue(" config-model ", "ONCALL_CHAT_MODEL"); got != "config-model" {
		t.Fatalf("fallback = %q, want config-model", got)
	}
}

func TestDeepSeekAPIKeyEnvironmentVariableIsSupported(t *testing.T) {
	t.Setenv("ONCALL_CHAT_API_KEY", "")
	t.Setenv("DS_QUICK_CHAT_API_KEY", "")
	t.Setenv("DEEPSEEK_API_KEY", "deepseek-env-key")
	if got := firstEnvOrValue("", "ONCALL_CHAT_API_KEY", "DS_QUICK_CHAT_API_KEY", "DEEPSEEK_API_KEY"); got != "deepseek-env-key" {
		t.Fatalf("api key=%q, want deepseek-env-key", got)
	}
}

func TestInstrumentChatModelRecordsModelSpanWithRequestTrace(t *testing.T) {
	sink := telemetry.NewMemorySink()
	client := InstrumentChatModel(fakeChatModel{}, telemetry.NewRecorder(sink), "deepseek-v4-flash")
	ctx := telemetry.WithContext(context.Background(), telemetry.ContextInfo{TraceID: "trace-123", RunID: "run-123"})
	if _, err := client.Generate(ctx, []*schema.Message{schema.UserMessage("hello")}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	spans := sink.Spans()
	if len(spans) != 1 {
		t.Fatalf("spans=%d, want 1", len(spans))
	}
	if spans[0].Name != "model.generate" || spans[0].TraceID != "trace-123" || spans[0].SpanID == "" {
		t.Fatalf("unexpected model span: %+v", spans[0])
	}
	if spans[0].Attrs["model"] != "deepseek-v4-flash" {
		t.Fatalf("model attr=%q", spans[0].Attrs["model"])
	}
	if spans[0].Attrs["span_type"] != "model" || !strings.Contains(spans[0].Attrs["input"], "hello") || !strings.Contains(spans[0].Attrs["output"], "ok") {
		t.Fatalf("model span missing CozeLoop input/output fields: %+v", spans[0].Attrs)
	}
}

func TestInstrumentChatModelRecordsReturnedTokenUsage(t *testing.T) {
	sink := telemetry.NewMemorySink()
	client := InstrumentChatModel(usageChatModel{}, telemetry.NewRecorder(sink), "deepseek-v4-flash")
	if _, err := client.Generate(context.Background(), []*schema.Message{schema.UserMessage("hello")}); err != nil {
		t.Fatalf("Generate() error = %v", err)
	}
	metrics := sink.Metrics()
	if len(metrics) != 2 || metrics[0].Name != "model.tokens" || metrics[0].Value != 7 || metrics[1].Name != "model.cost" {
		t.Fatalf("unexpected metrics: %#v", metrics)
	}
	spans := sink.Spans()
	if len(spans) != 3 || spans[1].Name != "model.tokens" || spans[2].Name != "model.cost" {
		t.Fatalf("unexpected token/cost spans: %#v", spans)
	}
}

func TestInstrumentChatModelCompletesStreamAndRecordsUsage(t *testing.T) {
	sink := telemetry.NewMemorySink()
	client := InstrumentChatModel(streamUsageChatModel{}, telemetry.NewRecorder(sink), "deepseek-v4-flash")
	ctx := telemetry.WithContext(context.Background(), telemetry.ContextInfo{
		TraceID: "trace-stream",
		RunID:   "run-stream",
		SpanID:  "root-stream",
	})
	reader, err := client.Stream(ctx, []*schema.Message{schema.UserMessage("hello")})
	if err != nil {
		t.Fatalf("Stream() error = %v", err)
	}
	for {
		_, recvErr := reader.Recv()
		if recvErr == io.EOF {
			break
		}
		if recvErr != nil {
			t.Fatalf("Recv() error = %v", recvErr)
		}
	}
	reader.Close()

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if len(sink.Spans()) > 0 && len(sink.Metrics()) == 2 {
			break
		}
		time.Sleep(time.Millisecond)
	}
	spans := sink.Spans()
	if len(spans) != 3 {
		t.Fatalf("stream spans=%+v", spans)
	}
	for _, span := range spans {
		if span.ParentID != "root-stream" {
			t.Fatalf("stream span parent=%q, spans=%+v", span.ParentID, spans)
		}
	}
	metrics := sink.Metrics()
	if len(metrics) != 2 || metrics[0].Name != "model.tokens" || metrics[0].Value != 5 {
		t.Fatalf("stream metrics=%+v", metrics)
	}
	spans = sink.Spans()
	names := make(map[string]bool, len(spans))
	for _, span := range spans {
		names[span.Name] = true
	}
	if len(spans) != 3 || !names["model.stream"] || !names["model.tokens"] || !names["model.cost"] {
		t.Fatalf("stream token/cost spans=%+v", spans)
	}
	for _, span := range spans {
		if span.Name == "model.stream" && (!strings.Contains(span.Attrs["input"], "hello") || !strings.Contains(span.Attrs["output"], "ok") || span.Attrs["input_tokens"] != "2" || span.Attrs["output_tokens"] != "3") {
			t.Fatalf("stream model span missing input/output/token fields: %+v", span.Attrs)
		}
	}
}

func TestFormatDurationUsesMilliseconds(t *testing.T) {
	if got := formatDuration(1500 * time.Microsecond); got != "1.500" {
		t.Fatalf("duration=%q, want 1.500 milliseconds", got)
	}
}

type usageChatModel struct{ fakeChatModel }

func (usageChatModel) Generate(_ context.Context, _ []*schema.Message, _ ...model.Option) (*schema.Message, error) {
	return &schema.Message{Role: schema.Assistant, Content: "ok", ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 3, CompletionTokens: 4, TotalTokens: 7}}}, nil
}

type streamUsageChatModel struct{}

func (streamUsageChatModel) Generate(context.Context, []*schema.Message, ...model.Option) (*schema.Message, error) {
	return nil, nil
}

func (streamUsageChatModel) Stream(context.Context, []*schema.Message, ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return schema.StreamReaderFromArray([]*schema.Message{
		{Role: schema.Assistant, Content: "ok"},
		{Role: schema.Assistant, ResponseMeta: &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 2, CompletionTokens: 3, TotalTokens: 5}}},
	}), nil
}

func (streamUsageChatModel) WithTools([]*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	return streamUsageChatModel{}, nil
}
