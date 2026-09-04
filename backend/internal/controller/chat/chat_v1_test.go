package chat

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"go_agent/internal/commands/slash"
	appcontext "go_agent/internal/context"

	"github.com/cloudwego/eino/schema"
)

func TestWriteSSEPayload(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		data string
		want string
	}{
		{
			name: "single line",
			data: "hello",
			want: "data: hello\n\n",
		},
		{
			name: "mixed newlines",
			data: "hello\r\nworld\rnext",
			want: "data: hello\ndata: world\ndata: next\n\n",
		},
		{
			name: "trailing newline preserved",
			data: "hello\n",
			want: "data: hello\ndata: \n\n",
		},
		{
			name: "empty payload",
			data: "",
			want: "data: \n\n",
		},
	}

	for _, tt := range tests {
		tt := tt
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			var buf bytes.Buffer
			if err := writeSSEPayload(&buf, tt.data); err != nil {
				t.Fatalf("writeSSEPayload returned error: %v", err)
			}
			if got := buf.String(); got != tt.want {
				t.Fatalf("unexpected SSE payload\nwant: %q\ngot:  %q", tt.want, got)
			}
		})
	}
}

func TestWriteSSEPayloadLargeLine(t *testing.T) {
	t.Parallel()

	data := strings.Repeat("a", 70*1024)
	var buf bytes.Buffer
	if err := writeSSEPayload(&buf, data); err != nil {
		t.Fatalf("writeSSEPayload returned error: %v", err)
	}

	want := "data: " + data + "\n\n"
	if got := buf.String(); got != want {
		t.Fatalf("unexpected SSE payload size=%d want=%d", len(got), len(want))
	}
}

func TestWithSSEWorkflowAnnotatesOpsResume(t *testing.T) {
	t.Parallel()
	payload := withSSEWorkflow(map[string]any{"type": "interrupt"}, sseWorkflowOps, sseResumeEndpointOps)
	if payload["workflow"] != "ops" {
		t.Fatalf("workflow=%v, want ops", payload["workflow"])
	}
	if payload["resume_endpoint"] != "ai_ops_resume_stream" {
		t.Fatalf("resume_endpoint=%v, want ai_ops_resume_stream", payload["resume_endpoint"])
	}
}

func TestConvertRecentSlashMessagesSkipsProbeAndLimits(t *testing.T) {
	t.Parallel()
	messages := []*schema.Message{
		schema.UserMessage("old"),
		schema.AssistantMessage("middle", nil),
		schema.UserMessage(slashRecentMessagesProbe),
		schema.AssistantMessage("new", nil),
	}
	got := convertRecentSlashMessages(messages, slashRecentMessagesProbe, 2)
	if len(got) != 2 {
		t.Fatalf("len=%d, want 2: %#v", len(got), got)
	}
	if got[0].Content != "middle" || got[1].Content != "new" {
		t.Fatalf("messages=%#v, want middle/new", got)
	}
}

func TestBuildSlashContextWiresRecentMessagesProvider(t *testing.T) {
	t.Parallel()
	ctrl := &ControllerV1{}
	ctx := ctrl.buildSlashContext(context.Background(), "session-1", "", slash.NewRegistry())
	if ctx.RecentMessages == nil {
		t.Fatal("RecentMessages provider is nil")
	}
}

func TestSanitizeUserFacingContentRedactsSecretsAndClips(t *testing.T) {
	t.Parallel()
	secretLine := "error Authorization: Bearer abc.def password=hunter2 token=plain --kubeconfig C:/Users/me/.kube/config postgres://user:pass@example/db"
	got := sanitizeUserFacingContent(secretLine)
	for _, leaked := range []string{"abc.def", "hunter2", "plain", ".kube/config", "user:pass"} {
		if strings.Contains(got, leaked) {
			t.Fatalf("sanitize leaked %q in %q", leaked, got)
		}
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("sanitize=%q, want redaction marker", got)
	}

	long := sanitizeUserFacingContent(strings.Repeat("x", 700))
	if len([]rune(long)) > 500 {
		t.Fatalf("clipped length=%d, want <=500", len([]rune(long)))
	}
}

func TestBuildTrustedCommandActionPayload(t *testing.T) {
	t.Parallel()
	payload := buildTrustedCommandActionPayload(slash.Result{Action: "clear_session", Payload: map[string]any{"scope": "current"}})
	if payload["type"] != "command_action" || payload["action"] != "clear_session" {
		t.Fatalf("payload=%#v, want command_action clear_session", payload)
	}
	if payload["trusted_control"] != true {
		t.Fatalf("trusted_control=%v, want true", payload["trusted_control"])
	}
	if payload["scope"] != "current" {
		t.Fatalf("scope=%v, want current", payload["scope"])
	}
}

func TestNewV1FromDepsUsesPrebuiltRuntime(t *testing.T) {
	t.Parallel()

	registry := slash.NewRegistry()
	memory := appcontext.NewSessionMemory(nil, nil)

	ctrl := NewV1FromDeps(ControllerDeps{
		RootAgentName:    "dialogue-runtime",
		OpsRootAgentName: "ops-runtime",
		SessionMemory:    memory,
		SlashRegistry:    registry,
		WorkDir:          "D:/tmp/oncall-runtime",
	})

	if ctrl.rootAgentName != "dialogue-runtime" {
		t.Fatalf("rootAgentName=%q, want dialogue-runtime", ctrl.rootAgentName)
	}
	if ctrl.opsRootAgentName != "ops-runtime" {
		t.Fatalf("opsRootAgentName=%q, want ops-runtime", ctrl.opsRootAgentName)
	}
	if ctrl.sessionMemory != memory {
		t.Fatal("controller did not use injected session memory")
	}
	if ctrl.slashRegistry != registry {
		t.Fatal("controller did not use injected slash registry")
	}
	if ctrl.workDir != "D:/tmp/oncall-runtime" {
		t.Fatalf("workDir=%q, want injected workDir", ctrl.workDir)
	}
}
