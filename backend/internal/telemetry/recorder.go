package telemetry

import (
	"context"
	"crypto/rand"
	"fmt"
	"strings"
	"sync"
	"time"
)

type contextKey struct{}

type ContextInfo struct {
	TraceID  string
	RunID    string
	SpanID   string
	Recorder *Recorder
}

func WithContext(ctx context.Context, info ContextInfo) context.Context {
	return context.WithValue(ctx, contextKey{}, info)
}

func ContextFrom(ctx context.Context) ContextInfo {
	if ctx == nil {
		return ContextInfo{}
	}
	info, _ := ctx.Value(contextKey{}).(ContextInfo)
	return info
}

func (r *Recorder) StartContext(ctx context.Context, name string, attrs map[string]string) func(error) {
	info := ContextFrom(ctx)
	return r.StartWithContext(ctx, info.TraceID, newSpanID(), info.SpanID, name, attrs)
}

// BeginRun creates a real root span and returns a context whose child spans
// can reference it. The root is emitted when the returned finish function is
// called.
func BeginRun(ctx context.Context, name string, attrs map[string]string) (context.Context, func(error)) {
	info := ContextFrom(ctx)
	if info.Recorder == nil {
		return ctx, func(error) {}
	}
	rootID := info.SpanID
	if rootID == "" {
		rootID = newSpanID()
	}
	runCtx := WithContext(ctx, ContextInfo{
		TraceID:  info.TraceID,
		RunID:    info.RunID,
		SpanID:   rootID,
		Recorder: info.Recorder,
	})
	return runCtx, info.Recorder.StartWithContext(runCtx, info.TraceID, rootID, "", name, attrs)
}

func newSpanID() string {
	buffer := make([]byte, 8)
	if _, err := rand.Read(buffer); err == nil {
		return fmt.Sprintf("%x", buffer)
	}
	return fmt.Sprintf("%016x", time.Now().UnixNano())
}

func NewSpanID() string {
	return newSpanID()
}

type Span struct {
	TraceID   string
	SpanID    string
	ParentID  string
	Name      string
	StartedAt time.Time
	EndedAt   time.Time
	Attrs     map[string]string
	Err       string
}

type Metric struct {
	Name      string
	Value     float64
	Attrs     map[string]string
	Timestamp time.Time
}

type AuditRecord struct {
	TraceID   string
	RunID     string
	Actor     string
	Action    string
	Target    string
	Decision  string
	Attrs     map[string]string
	Timestamp time.Time
}

type Sink interface {
	RecordSpan(context.Context, Span) error
	RecordMetric(context.Context, Metric) error
	RecordAudit(context.Context, AuditRecord) error
}

type Lifecycle interface {
	Flush(context.Context)
	Close(context.Context)
}

type Recorder struct {
	sink Sink
}

func NewRecorder(sink Sink) *Recorder { return &Recorder{sink: sink} }

func (r *Recorder) Flush(ctx context.Context) {
	if r == nil {
		return
	}
	if lifecycle, ok := r.sink.(Lifecycle); ok {
		lifecycle.Flush(ctx)
	}
}

func (r *Recorder) Close(ctx context.Context) {
	if r == nil {
		return
	}
	if lifecycle, ok := r.sink.(Lifecycle); ok {
		lifecycle.Close(ctx)
	}
}

func (r *Recorder) Start(traceID, spanID, parentID, name string, attrs map[string]string) func(error) {
	return r.StartWithContext(context.Background(), traceID, spanID, parentID, name, attrs)
}

func (r *Recorder) StartWithContext(ctx context.Context, traceID, spanID, parentID, name string, attrs map[string]string) func(error) {
	started := time.Now().UTC()
	return func(err error) {
		if r == nil || r.sink == nil {
			return
		}
		sp := Span{TraceID: traceID, SpanID: spanID, ParentID: parentID, Name: name, StartedAt: started, EndedAt: time.Now().UTC(), Attrs: redactAttrs(attrs)}
		if err != nil {
			sp.Err = err.Error()
		}
		if ctx == nil {
			ctx = context.Background()
		}
		_ = r.sink.RecordSpan(ctx, sp)
	}
}

func (r *Recorder) Metric(ctx context.Context, name string, value float64, attrs map[string]string) {
	if r == nil || r.sink == nil {
		return
	}
	_ = r.sink.RecordMetric(ctx, Metric{Name: name, Value: value, Attrs: redactAttrs(attrs), Timestamp: time.Now().UTC()})
}

func (r *Recorder) Audit(ctx context.Context, record AuditRecord) {
	if r == nil || r.sink == nil {
		return
	}
	record.Timestamp = time.Now().UTC()
	record.Attrs = redactAttrs(record.Attrs)
	_ = r.sink.RecordAudit(ctx, record)
}

type MemorySink struct {
	mu      sync.Mutex
	spans   []Span
	metrics []Metric
	audits  []AuditRecord
}

func NewMemorySink() *MemorySink { return &MemorySink{} }

func (s *MemorySink) RecordSpan(_ context.Context, sp Span) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	sp.Attrs = redactAttrs(sp.Attrs)
	s.spans = append(s.spans, sp)
	return nil
}

func (s *MemorySink) RecordMetric(_ context.Context, metric Metric) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	metric.Attrs = redactAttrs(metric.Attrs)
	s.metrics = append(s.metrics, metric)
	return nil
}

func (s *MemorySink) RecordAudit(_ context.Context, record AuditRecord) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	record.Attrs = redactAttrs(record.Attrs)
	s.audits = append(s.audits, record)
	return nil
}

func (s *MemorySink) Spans() []Span {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Span, len(s.spans))
	copy(out, s.spans)
	return out
}

func (s *MemorySink) Metrics() []Metric {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]Metric, len(s.metrics))
	copy(out, s.metrics)
	return out
}

func (s *MemorySink) Audits() []AuditRecord {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]AuditRecord, len(s.audits))
	copy(out, s.audits)
	return out
}

func redactAttrs(attrs map[string]string) map[string]string {
	if attrs == nil {
		return nil
	}
	out := make(map[string]string, len(attrs))
	for key, value := range attrs {
		lower := strings.ToLower(key)
		if isSensitiveAttrKey(lower) {
			out[key] = "[redacted]"
			continue
		}
		out[key] = value
	}
	return out
}

func isSensitiveAttrKey(key string) bool {
	if strings.Contains(key, "secret") || strings.Contains(key, "password") ||
		strings.Contains(key, "apikey") || strings.Contains(key, "api_key") {
		return true
	}
	if key == "token" || strings.HasSuffix(key, "_token") || strings.HasSuffix(key, "_tokens") {
		switch key {
		case "prompt_tokens", "completion_tokens", "total_tokens", "reasoning_tokens", "input_tokens", "output_tokens":
			return false
		default:
			return true
		}
	}
	return false
}
