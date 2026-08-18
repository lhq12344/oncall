package hooks

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

func TestEvaluateConditionLeafOps(t *testing.T) {
	t.Parallel()
	ctx := HookContext{
		EventName: EventToolPreUse,
		ToolName:  "Bash",
		ToolArgs: map[string]any{
			"command":   "rm -rf /tmp/x",
			"file_path": "src/foo.go",
		},
	}

	cases := map[string]bool{
		"tool == \"Bash\"":                              true,
		"tool == \"Read\"":                              false,
		"tool != \"Read\"":                              true,
		"event =~ /^tool_/":                             true,
		"args.command =~ /rm -rf/":                      true,
		"file_path =* \"src/*.go\"":                     true,
		"file_path =* \"src/*.py\"":                     false,
		"tool == \"Bash\" && file_path =* \"src/*.go\"": true,
		"tool == \"Read\" || tool == \"Bash\"":          true,
		"!(tool == \"Read\")":                           true,
		"!tool == \"Read\"":                             true,
	}
	for condition, want := range cases {
		got := evaluateCondition(condition, ctx)
		if got != want {
			t.Fatalf("evaluateCondition(%q) = %v, want %v", condition, got, want)
		}
	}
}

func TestValidateRejectsCommandActionByDefault(t *testing.T) {
	t.Parallel()
	err := Validate([]Hook{{
		ID:     "unsafe-command",
		Event:  EventToolPostUse,
		Action: Action{Type: ActionCommand, Message: "echo nope"},
	}})
	if err == nil || !strings.Contains(err.Error(), "command is disabled") {
		t.Fatalf("expected command action rejection, got %v", err)
	}
}

func TestRunPreToolHooksReject(t *testing.T) {
	t.Parallel()
	eng := NewEngine()
	if err := eng.LoadHooks([]Hook{{
		ID:           "block-rm-rf",
		Event:        EventToolPreUse,
		Condition:    "tool == \"Bash\" && args.command =~ /rm -rf/",
		Action:       Action{Type: ActionMessage, Message: "destructive command blocked"},
		Reject:       true,
		RejectReason: "destructive command blocked",
	}}); err != nil {
		t.Fatal(err)
	}

	rejected, msg := eng.RunPreToolHooks(context.Background(), HookContext{
		ToolName: "Bash",
		ToolArgs: map[string]any{"command": "rm -rf /tmp/x"},
	})
	if !rejected {
		t.Fatal("expected rejection")
	}
	if !strings.Contains(msg, "destructive command blocked") {
		t.Fatalf("unexpected reject message: %q", msg)
	}
}

func TestRunPreToolHooksAllowsWhenConditionFails(t *testing.T) {
	t.Parallel()
	eng := NewEngine()
	if err := eng.LoadHooks([]Hook{{
		ID:        "block-go",
		Event:     EventToolPreUse,
		Condition: "file_path =* \"**/*.go\"",
		Action:    Action{Type: ActionMessage, Message: "blocked"},
		Reject:    true,
	}}); err != nil {
		t.Fatal(err)
	}
	rejected, _ := eng.RunPreToolHooks(context.Background(), HookContext{
		ToolName: "WriteFile",
		ToolArgs: map[string]any{"file_path": "src/foo.py"},
	})
	if rejected {
		t.Fatal("expected allow for non-matching path")
	}
}

func TestHookAsyncIsNonBlocking(t *testing.T) {
	t.Parallel()
	eng := NewEngine()
	if err := eng.LoadHooks([]Hook{{
		ID:     "async-audit",
		Event:  EventTurnEnd,
		Async:  true,
		Action: Action{Type: ActionAudit, Message: "done"},
	}}); err != nil {
		t.Fatal(err)
	}
	start := time.Now()
	results := eng.RunEvent(context.Background(), EventTurnEnd, HookContext{SessionID: "s1"})
	if elapsed := time.Since(start); elapsed > 50*time.Millisecond {
		t.Fatalf("async hook blocked caller for %s", elapsed)
	}
	if len(results) != 1 || results[0].Output != "(async)" {
		t.Fatalf("expected async placeholder result, got %#v", results)
	}
}

func TestHookOnceOnlyFiresOnce(t *testing.T) {
	t.Parallel()
	eng := NewEngine()
	if err := eng.LoadHooks([]Hook{{
		ID:     "once",
		Event:  EventSessionStart,
		Once:   true,
		Action: Action{Type: ActionLog},
	}}); err != nil {
		t.Fatal(err)
	}
	if got := len(eng.RunEvent(context.Background(), EventSessionStart, HookContext{})); got != 1 {
		t.Fatalf("first run got %d results", got)
	}
	if got := len(eng.RunEvent(context.Background(), EventSessionStart, HookContext{})); got != 0 {
		t.Fatalf("second run got %d results", got)
	}
}

type captureRoundTripper struct {
	body string
}

func (rt *captureRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	data, _ := io.ReadAll(req.Body)
	rt.body = string(data)
	return &http.Response{
		StatusCode: http.StatusOK,
		Header:     make(http.Header),
		Body:       io.NopCloser(strings.NewReader("{\"ok\":true}")),
	}, nil
}

func TestWebhookRequiresAllowlistedHostAndRedactsPayload(t *testing.T) {
	t.Parallel()
	eng, err := NewEngineFromConfig(Config{
		Enabled:             true,
		WebhookAllowedHosts: []string{"hooks.example.com"},
		Hooks: []Hook{{
			ID:     "webhook",
			Event:  EventToolPostUse,
			Action: Action{Type: ActionWebhook, URL: "https://hooks.example.com/oncall"},
		}},
	})
	if err != nil {
		t.Fatal(err)
	}
	rt := &captureRoundTripper{}
	eng.httpClient = &http.Client{Transport: rt}

	results := eng.RunEvent(context.Background(), EventToolPostUse, HookContext{
		ToolName: "ReadFile",
		ToolArgs: map[string]any{"token": "secret-value", "file_path": "a.txt"},
	})
	if len(results) != 1 || !results[0].Success {
		t.Fatalf("expected successful webhook, got %#v", results)
	}
	if strings.Contains(rt.body, "secret-value") || !strings.Contains(rt.body, "[REDACTED]") {
		t.Fatalf("expected redacted webhook body, got %s", rt.body)
	}
}

func TestNewEngineFromConfigDisabledNoops(t *testing.T) {
	t.Parallel()
	eng, err := NewEngineFromConfig(Config{Enabled: false, Hooks: []Hook{{
		ID:     "would-run",
		Event:  EventTurnStart,
		Action: Action{Type: ActionLog},
	}}})
	if err != nil {
		t.Fatal(err)
	}
	if eng.Enabled() {
		t.Fatal("expected disabled engine")
	}
	if results := eng.RunEvent(context.Background(), EventTurnStart, HookContext{}); len(results) != 0 {
		t.Fatalf("expected no-op disabled engine, got %#v", results)
	}
}
