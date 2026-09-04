package models

import (
	"context"
	"encoding/json"
	"io"
	"strconv"
	"strings"
	"sync"
	"time"

	"go_agent/internal/telemetry"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/coze-dev/cozeloop-go/spec/tracespec"
)

type instrumentedChatModel struct {
	base      model.ToolCallingChatModel
	telemetry *telemetry.Recorder
	modelName string
}

func InstrumentChatModel(base model.ToolCallingChatModel, recorder *telemetry.Recorder, modelName string) model.ToolCallingChatModel {
	if base == nil || recorder == nil {
		return base
	}
	return &instrumentedChatModel{base: base, telemetry: recorder, modelName: modelName}
}

func (m *instrumentedChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	attrs := m.modelSpanAttrs(input, true)
	finish := m.telemetry.StartContext(ctx, "model.generate", attrs)
	out, err := m.base.Generate(ctx, input, opts...)
	attrs[tracespec.Output] = modelOutputJSON(out)
	addTokenAttrs(attrs, out)
	finish(err)
	m.recordUsage(ctx, out)
	return out, err
}

func (m *instrumentedChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	started := time.Now()
	attrs := m.modelSpanAttrs(input, true)
	reader, err := m.base.Stream(ctx, input, opts...)
	if err != nil {
		finish := m.telemetry.StartContext(ctx, "model.stream", attrs)
		finish(err)
		return nil, err
	}
	var once sync.Once
	finish := func(streamErr error) {
		once.Do(func() {
			attrs["latency_ms"] = formatDuration(time.Since(started))
			done := m.telemetry.StartContext(ctx, "model.stream", attrs)
			done(streamErr)
		})
	}

	readers := reader.Copy(2)
	clientReader := readers[0]
	observer := readers[1]
	go func() {
		defer observer.Close()
		var usageMessage *schema.Message
		var output strings.Builder
		for {
			message, streamErr := observer.Recv()
			if streamErr == io.EOF {
				attrs[tracespec.Output] = modelTextOutputJSON(output.String())
				addTokenAttrs(attrs, usageMessage)
				m.recordUsage(ctx, usageMessage)
				finish(nil)
				return
			}
			if streamErr != nil {
				finish(streamErr)
				return
			}
			if message != nil && message.ResponseMeta != nil && message.ResponseMeta.Usage != nil {
				usageMessage = message
			}
			if message != nil && message.Content != "" {
				output.WriteString(message.Content)
			}
		}
	}()
	return clientReader, nil
}

func (m *instrumentedChatModel) modelSpanAttrs(input []*schema.Message, stream bool) map[string]string {
	return map[string]string{
		tracespec.SpanType:      tracespec.VModelSpanType,
		tracespec.ModelProvider: "openai-compatible",
		tracespec.ModelName:     m.modelName,
		"provider":              "openai-compatible",
		"model":                 m.modelName,
		tracespec.Stream:        strconv.FormatBool(stream),
		tracespec.Input:         modelInputJSON(input),
	}
}

func modelInputJSON(messages []*schema.Message) string {
	input := tracespec.ModelInput{Messages: make([]*tracespec.ModelMessage, 0, len(messages))}
	for _, message := range messages {
		if message == nil {
			continue
		}
		input.Messages = append(input.Messages, &tracespec.ModelMessage{Role: string(message.Role), Content: message.Content})
	}
	return mustJSON(input)
}

func modelOutputJSON(message *schema.Message) string {
	if message == nil {
		return ""
	}
	return modelTextOutputJSON(message.Content)
}

func modelTextOutputJSON(content string) string {
	if content == "" {
		return ""
	}
	output := tracespec.ModelOutput{Choices: []*tracespec.ModelChoice{{
		Index:        0,
		FinishReason: "stop",
		Message:      &tracespec.ModelMessage{Role: tracespec.VRoleAssistant, Content: content},
	}}}
	return mustJSON(output)
}

func addTokenAttrs(attrs map[string]string, message *schema.Message) {
	if attrs == nil || message == nil || message.ResponseMeta == nil || message.ResponseMeta.Usage == nil {
		return
	}
	usage := message.ResponseMeta.Usage
	attrs[tracespec.InputTokens] = strconv.Itoa(usage.PromptTokens)
	attrs[tracespec.OutputTokens] = strconv.Itoa(usage.CompletionTokens)
}

func mustJSON(value any) string {
	data, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(data)
}

func (m *instrumentedChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	base, err := m.base.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return &instrumentedChatModel{base: base, telemetry: m.telemetry, modelName: m.modelName}, nil
}

func formatDuration(value time.Duration) string {
	return strconv.FormatFloat(float64(value.Microseconds())/1000, 'f', 3, 64)
}

func (m *instrumentedChatModel) recordUsage(ctx context.Context, message *schema.Message) {
	if m == nil || m.telemetry == nil || message == nil || message.ResponseMeta == nil || message.ResponseMeta.Usage == nil {
		return
	}
	usage := message.ResponseMeta.Usage
	info := telemetry.ContextFrom(ctx)
	attrs := map[string]string{
		"model":             m.modelName,
		"prompt_tokens":     strconv.Itoa(usage.PromptTokens),
		"completion_tokens": strconv.Itoa(usage.CompletionTokens),
	}
	m.telemetry.Metric(ctx, "model.tokens", float64(usage.TotalTokens), attrs)
	m.telemetry.StartContext(ctx, "model.tokens", attrs)(nil)
	// Model providers may omit price information. Keep cost separate from token
	// usage so an unavailable price is explicit instead of fabricated.
	costAttrs := map[string]string{
		"model":       m.modelName,
		"cost_status": "provider_price_unconfigured",
		"trace_id":    info.TraceID,
	}
	m.telemetry.Metric(ctx, "model.cost", 0, costAttrs)
	m.telemetry.StartContext(ctx, "model.cost", costAttrs)(nil)
}
