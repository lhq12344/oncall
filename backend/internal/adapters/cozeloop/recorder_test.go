package cozeloop

import (
	"context"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"go_agent/internal/telemetry"
)

func TestRecorderDegradesWithoutFailingCoreTelemetry(t *testing.T) {
	recorder := NewRecorder()
	sink := telemetry.NewRecorder(recorder)
	done := sink.Start("trace", "span", "", "oncall.run", map[string]string{"token": "secret"})
	done(nil)
	sink.Metric(context.Background(), "dropped", 1, nil)
	sink.Audit(context.Background(), telemetry.AuditRecord{TraceID: "trace", Action: "approval.required"})
	if recorder.Dropped() != 3 {
		t.Fatalf("dropped=%d, want 3", recorder.Dropped())
	}
}

func TestNewFromEnvDegradesWhenConfigurationIsIncomplete(t *testing.T) {
	recorder, err := NewFromEnv(func(key string) string {
		if key == envWorkspaceID {
			return "workspace"
		}
		return ""
	})
	if err != nil {
		t.Fatalf("NewFromEnv() error = %v", err)
	}
	if recorder == nil || recorder.client != nil {
		t.Fatal("incomplete configuration must produce a degraded recorder")
	}
}

func TestConfiguredRecorderExportsSpanWithoutLeakingToken(t *testing.T) {
	var gotHeader string
	var gotBody string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeader = r.Header.Get("Authorization")
		buffer, _ := io.ReadAll(r.Body)
		gotBody = string(buffer)
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":0,"msg":"ok"}`))
	}))
	defer server.Close()

	recorder, err := NewFromEnv(func(key string) string {
		switch key {
		case envAPIBaseURL:
			return server.URL
		case envWorkspaceID:
			return "workspace"
		case envAPIToken:
			return "test-secret-token"
		default:
			return ""
		}
	})
	if err != nil {
		t.Fatalf("NewFromEnv() error = %v", err)
	}

	err = recorder.RecordSpan(context.Background(), telemetry.Span{
		TraceID:   "trace",
		SpanID:    "span",
		Name:      "oncall.run",
		StartedAt: time.Now().Add(-time.Second),
		EndedAt:   time.Now(),
		Attrs:     map[string]string{"token": "[redacted]", "safe": "ok", "span_type": "model", "input": `{"messages":[{"role":"user","content":"hello"}]}`, "output": `{"choices":[{"index":0,"message":{"role":"assistant","content":"ok"}}]}`, "input_tokens": "2", "output_tokens": "3", "model_name": "deepseek-v4-flash", "model_provider": "openai-compatible"},
	})
	if err != nil {
		t.Fatalf("RecordSpan() error = %v", err)
	}
	recorder.Close(context.Background())
	if gotHeader != "Bearer test-secret-token" {
		t.Fatalf("authorization header = %q", gotHeader)
	}
	if gotBody == "" || !strings.Contains(gotBody, "oncall.run") {
		t.Fatalf("export body does not contain span name: %q", gotBody)
	}
	if !strings.Contains(gotBody, normalizeTraceID("trace")) {
		t.Fatalf("export body does not contain normalized trace id: %q", gotBody)
	}
	if strings.Contains(gotBody, "test-secret-token") {
		t.Fatal("API token leaked into exported span body")
	}
	for _, want := range []string{"hello", "ok", "deepseek-v4-flash", "input_tokens", "output_tokens"} {
		if !strings.Contains(gotBody, want) {
			t.Fatalf("export body missing %q: %s", want, gotBody)
		}
	}
}

func TestSpanIDsAreNormalizedForCozeLoopTraceContext(t *testing.T) {
	if got := normalizeTraceID("trace-1"); len(got) != 32 {
		t.Fatalf("trace id length=%d", len(got))
	}
	if got := normalizeSpanID("span-1"); len(got) != 16 {
		t.Fatalf("span id length=%d", len(got))
	}
	if got := normalizeParentID(""); got != "0" {
		t.Fatalf("empty parent id=%q, want 0", got)
	}
}
