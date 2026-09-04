package telemetry

import (
	"context"
	"testing"
)

type captureSink struct {
	span   Span
	metric Metric
	audit  AuditRecord
	ctx    context.Context
}

type lifecycleSink struct {
	captureSink
	flushed bool
	closed  bool
}

func (s *lifecycleSink) Flush(context.Context) { s.flushed = true }
func (s *lifecycleSink) Close(context.Context) { s.closed = true }

func (s *captureSink) RecordSpan(ctx context.Context, sp Span) error {
	s.ctx = ctx
	s.span = sp
	return nil
}

func (s *captureSink) RecordMetric(_ context.Context, metric Metric) error {
	s.metric = metric
	return nil
}

func (s *captureSink) RecordAudit(_ context.Context, record AuditRecord) error {
	s.audit = record
	return nil
}

func TestRecorderRecordsSpan(t *testing.T) {
	sink := NewMemorySink()
	done := NewRecorder(sink).Start("trace", "span", "", "unit", map[string]string{"k": "v"})
	done(nil)
	spans := sink.Spans()
	if len(spans) != 1 || spans[0].TraceID != "trace" || spans[0].Name != "unit" {
		t.Fatalf("unexpected spans: %+v", spans)
	}
	if spans[0].StartedAt.IsZero() || spans[0].EndedAt.IsZero() {
		t.Fatalf("missing timestamps: %+v", spans[0])
	}
}

func TestRecorderRedactsSensitiveAttrsAcrossRecords(t *testing.T) {
	sink := NewMemorySink()
	recorder := NewRecorder(sink)
	done := recorder.Start("trace", "span", "", "unit", map[string]string{"password": "secret", "safe": "ok"})
	done(nil)
	recorder.Metric(context.Background(), "metric", 1, map[string]string{"api_key": "secret"})
	recorder.Audit(context.Background(), AuditRecord{TraceID: "trace", Action: "approval", Attrs: map[string]string{"token": "secret"}})
	if got := sink.Spans()[0].Attrs["password"]; got != "[redacted]" {
		t.Fatalf("span password attr=%q", got)
	}
	if got := sink.Metrics()[0].Attrs["api_key"]; got != "[redacted]" {
		t.Fatalf("metric api_key attr=%q", got)
	}
	if got := sink.Audits()[0].Attrs["token"]; got != "[redacted]" {
		t.Fatalf("audit token attr=%q", got)
	}
}

func TestRecorderRedactsBeforeAdapterSink(t *testing.T) {
	sink := &captureSink{}
	recorder := NewRecorder(sink)
	done := recorder.Start("trace", "span", "", "cozeloop.export", map[string]string{"token": "secret", "safe": "ok", "input_tokens": "2", "output_tokens": "3"})
	done(nil)
	recorder.Metric(context.Background(), "metric", 1, map[string]string{"password": "secret"})
	recorder.Audit(context.Background(), AuditRecord{TraceID: "trace", Action: "approval", Attrs: map[string]string{"api_key": "secret"}})

	if got := sink.span.Attrs["token"]; got != "[redacted]" {
		t.Fatalf("span reached adapter without redaction: %q", got)
	}
	if sink.span.Attrs["input_tokens"] != "2" || sink.span.Attrs["output_tokens"] != "3" {
		t.Fatalf("token usage fields should remain observable: %+v", sink.span.Attrs)
	}
	if got := sink.metric.Attrs["password"]; got != "[redacted]" {
		t.Fatalf("metric reached adapter without redaction: %q", got)
	}
	if got := sink.audit.Attrs["api_key"]; got != "[redacted]" {
		t.Fatalf("audit reached adapter without redaction: %q", got)
	}
}

func TestRecorderForwardsLifecycle(t *testing.T) {
	sink := &lifecycleSink{}
	recorder := NewRecorder(sink)
	recorder.Flush(context.Background())
	recorder.Close(context.Background())
	if !sink.flushed || !sink.closed {
		t.Fatalf("lifecycle not forwarded: flushed=%v closed=%v", sink.flushed, sink.closed)
	}
}

func TestContextRoundTrip(t *testing.T) {
	ctx := WithContext(context.Background(), ContextInfo{TraceID: "trace-1", RunID: "run-1", SpanID: "span-1"})
	if got := ContextFrom(ctx); got.TraceID != "trace-1" || got.RunID != "run-1" || got.SpanID != "span-1" {
		t.Fatalf("context info=%+v", got)
	}
}

func TestRecorderPreservesContextForSink(t *testing.T) {
	sink := &captureSink{}
	recorder := NewRecorder(sink)
	ctx := WithContext(context.Background(), ContextInfo{TraceID: "trace", RunID: "run", SpanID: "root-span"})
	recorder.StartContext(ctx, "unit", nil)(nil)
	if ContextFrom(sink.ctx).TraceID != "trace" {
		t.Fatalf("sink context=%+v, want trace context", ContextFrom(sink.ctx))
	}
	if sink.span.ParentID != "root-span" || sink.span.SpanID == "root-span" {
		t.Fatalf("span parent=%q span=%q, want root-span and unique child", sink.span.ParentID, sink.span.SpanID)
	}
}

func TestBeginRunCreatesRootSpanContext(t *testing.T) {
	sink := NewMemorySink()
	recorder := NewRecorder(sink)
	ctx := WithContext(context.Background(), ContextInfo{
		TraceID:  "trace-1",
		RunID:    "run-1",
		Recorder: recorder,
	})
	runCtx, finish := BeginRun(ctx, "run.chat", nil)
	info := ContextFrom(runCtx)
	if info.TraceID != "trace-1" || info.RunID != "run-1" || info.SpanID == "" {
		t.Fatalf("root context=%+v", info)
	}
	finish(nil)
	spans := sink.Spans()
	if len(spans) != 1 || spans[0].Name != "run.chat" || spans[0].ParentID != "" {
		t.Fatalf("root span=%+v", spans)
	}
}
