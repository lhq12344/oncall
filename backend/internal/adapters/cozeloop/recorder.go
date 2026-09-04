package cozeloop

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"go_agent/internal/telemetry"

	cozeloop "github.com/coze-dev/cozeloop-go"
	"github.com/coze-dev/cozeloop-go/spec/tracespec"
)

const (
	envAPIBaseURL  = cozeloop.EnvApiBaseURL
	envWorkspaceID = cozeloop.EnvWorkspaceID
	envAPIToken    = cozeloop.EnvApiToken
)

// Recorder adapts the internal telemetry contract to the CozeLoop Go SDK.
// A nil client is an intentional degraded/no-op mode: observability must never
// prevent the core service from starting or handling requests.
type Recorder struct {
	client  cozeloop.Client
	dropped atomic.Int64
}

func NewRecorder() *Recorder { return &Recorder{} }

// NewFromEnv creates a CozeLoop recorder when all required environment
// variables are present. Missing configuration returns a degraded recorder.
func NewFromEnv(getenv func(string) string) (*Recorder, error) {
	if getenv == nil {
		getenv = os.Getenv
	}
	baseURL := strings.TrimSpace(getenv(envAPIBaseURL))
	workspaceID := strings.TrimSpace(getenv(envWorkspaceID))
	apiToken := strings.TrimSpace(getenv(envAPIToken))
	if baseURL == "" || workspaceID == "" || apiToken == "" {
		return NewRecorder(), nil
	}
	client, err := cozeloop.NewClient(
		cozeloop.WithAPIBaseURL(baseURL),
		cozeloop.WithWorkspaceID(workspaceID),
		cozeloop.WithAPIToken(apiToken),
	)
	if err != nil {
		return NewRecorder(), err
	}
	return &Recorder{client: client}, nil
}

func NewClientRecorder(client cozeloop.Client) *Recorder {
	return &Recorder{client: client}
}

func (r *Recorder) RecordSpan(ctx context.Context, span telemetry.Span) error {
	if r == nil || r.client == nil {
		if r != nil {
			r.dropped.Add(1)
		}
		return nil
	}
	startOptions := []cozeloop.StartSpanOption{
		cozeloop.WithSpanID(normalizeSpanID(span.SpanID)),
		cozeloop.WithChildOf(internalSpanContext{
			traceID: normalizeTraceID(span.TraceID),
			spanID:  normalizeParentID(span.ParentID),
		}),
	}
	spanCtx, loopSpan := r.client.StartSpan(ctx, span.Name, cozeSpanType(span), startOptions...)
	attrs := make(map[string]interface{}, len(span.Attrs)+3)
	attrs["trace_id"] = span.TraceID
	attrs["span_id"] = span.SpanID
	attrs["parent_id"] = span.ParentID
	for key, value := range span.Attrs {
		attrs[key] = value
	}
	applyCozeLoopBuiltinFields(spanCtx, loopSpan, span.Attrs)
	loopSpan.SetTags(spanCtx, attrs)
	if span.Err != "" {
		loopSpan.SetError(spanCtx, errString(span.Err))
		loopSpan.SetStatusCode(spanCtx, 1)
	}
	loopSpan.SetFinishTime(nonZeroTime(span.EndedAt))
	loopSpan.Finish(spanCtx)
	return nil
}

func cozeSpanType(span telemetry.Span) string {
	if span.Attrs != nil {
		switch strings.TrimSpace(span.Attrs[tracespec.SpanType]) {
		case tracespec.VModelSpanType, tracespec.VRetrieverSpanType, tracespec.VToolSpanType, tracespec.VPromptHubSpanType, tracespec.VPromptTemplateSpanType:
			return strings.TrimSpace(span.Attrs[tracespec.SpanType])
		}
	}
	return "custom"
}

func applyCozeLoopBuiltinFields(ctx context.Context, span cozeloop.Span, attrs map[string]string) {
	if span == nil || attrs == nil {
		return
	}
	if input := strings.TrimSpace(attrs[tracespec.Input]); input != "" {
		span.SetInput(ctx, input)
	}
	if output := strings.TrimSpace(attrs[tracespec.Output]); output != "" {
		span.SetOutput(ctx, output)
	}
	if provider := strings.TrimSpace(attrs[tracespec.ModelProvider]); provider != "" {
		span.SetModelProvider(ctx, provider)
	}
	if modelName := strings.TrimSpace(attrs[tracespec.ModelName]); modelName != "" {
		span.SetModelName(ctx, modelName)
	}
	if tokens, err := strconv.Atoi(strings.TrimSpace(attrs[tracespec.InputTokens])); err == nil && tokens > 0 {
		span.SetInputTokens(ctx, tokens)
	}
	if tokens, err := strconv.Atoi(strings.TrimSpace(attrs[tracespec.OutputTokens])); err == nil && tokens > 0 {
		span.SetOutputTokens(ctx, tokens)
	}
}

func (r *Recorder) RecordMetric(ctx context.Context, metric telemetry.Metric) error {
	if r == nil || r.client == nil {
		if r != nil {
			r.dropped.Add(1)
		}
		return nil
	}
	spanCtx, span := r.startSpan(ctx, "metric."+metric.Name, "metric", telemetry.NewSpanID())
	attrs := make(map[string]interface{}, len(metric.Attrs)+1)
	attrs["value"] = metric.Value
	for key, value := range metric.Attrs {
		attrs[key] = value
	}
	span.SetTags(spanCtx, attrs)
	span.SetFinishTime(nonZeroTime(metric.Timestamp))
	span.Finish(spanCtx)
	return nil
}

func (r *Recorder) RecordAudit(ctx context.Context, audit telemetry.AuditRecord) error {
	if r == nil || r.client == nil {
		if r != nil {
			r.dropped.Add(1)
		}
		return nil
	}
	spanCtx, span := r.startSpan(ctx, "audit."+audit.Action, "audit", telemetry.NewSpanID())
	attrs := make(map[string]interface{}, len(audit.Attrs)+5)
	attrs["trace_id"] = audit.TraceID
	attrs["run_id"] = audit.RunID
	attrs["actor"] = audit.Actor
	attrs["target"] = audit.Target
	attrs["decision"] = audit.Decision
	for key, value := range audit.Attrs {
		attrs[key] = value
	}
	span.SetTags(spanCtx, attrs)
	span.SetFinishTime(nonZeroTime(audit.Timestamp))
	span.Finish(spanCtx)
	return nil
}

func (r *Recorder) startSpan(ctx context.Context, name, spanType, spanID string) (context.Context, cozeloop.Span) {
	info := telemetry.ContextFrom(ctx)
	options := []cozeloop.StartSpanOption{
		cozeloop.WithSpanID(normalizeSpanID(spanID)),
		cozeloop.WithChildOf(internalSpanContext{
			traceID: normalizeTraceID(info.TraceID),
			spanID:  normalizeParentID(info.SpanID),
		}),
	}
	return r.client.StartSpan(ctx, name, spanType, options...)
}

func (r *Recorder) Dropped() int64 {
	if r == nil {
		return 0
	}
	return r.dropped.Load()
}

func (r *Recorder) Flush(ctx context.Context) {
	// cozeloop-go v0.1.23 implements ForceFlush by running drainQueue
	// concurrently with processQueue. The SDK's batch state is not protected
	// across those paths, which makes the public Flush method race-prone.
	// Spans are exported by the SDK's background batch processor; Close below
	// performs the safe shutdown/drain path when the process is stopping.
	_ = ctx
}

func (r *Recorder) Close(ctx context.Context) {
	if r != nil && r.client != nil {
		r.client.Close(ctx)
	}
}

type errString string

func (e errString) Error() string { return string(e) }

func nonZeroTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Now().UTC()
	}
	return value
}

type internalSpanContext struct {
	traceID string
	spanID  string
}

func (s internalSpanContext) GetTraceID() string { return s.traceID }
func (s internalSpanContext) GetSpanID() string  { return s.spanID }
func (s internalSpanContext) GetBaggage() map[string]string {
	return nil
}

func normalizeTraceID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 32 {
		if _, err := hex.DecodeString(value); err == nil {
			return value
		}
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:16])
}

func normalizeSpanID(value string) string {
	value = strings.TrimSpace(value)
	if len(value) == 16 {
		if _, err := hex.DecodeString(value); err == nil && value != "0000000000000000" {
			return value
		}
	}
	sum := sha256.Sum256([]byte(value))
	return hex.EncodeToString(sum[:8])
}

func normalizeParentID(value string) string {
	value = strings.TrimSpace(value)
	if value == "" {
		return "0"
	}
	return normalizeSpanID(value)
}
