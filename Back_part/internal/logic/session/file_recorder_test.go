package session

import (
	"context"
	"strconv"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestFileSessionRecorderLoadsRecentVisibleMessages(t *testing.T) {
	dir := t.TempDir()
	recorder := NewFileSessionRecorder(dir)
	sessionID := "session-1"

	for i := 1; i <= 6; i++ {
		suffix := strconv.Itoa(i)
		promptMessages := []*schema.Message{
			schema.UserMessage("prompt snapshot should be ignored"),
		}
		userMsg := schema.UserMessage("user-" + suffix)
		assistantMsg := schema.AssistantMessage("assistant-"+suffix, nil)
		if err := recorder.AppendTurnWithPrompt(context.Background(), sessionID, "chat_stream_graph", promptMessages, userMsg, assistantMsg); err != nil {
			t.Fatalf("AppendTurnWithPrompt returned error: %v", err)
		}
	}

	messages, err := recorder.LoadRecentMessages(context.Background(), sessionID, 10)
	if err != nil {
		t.Fatalf("LoadRecentMessages returned error: %v", err)
	}
	if len(messages) != 7 {
		t.Fatalf("message count = %d, want latest 10 file rows filtered to 7 visible messages", len(messages))
	}
	if messages[0].Content != "assistant-3" {
		t.Fatalf("first recovered message = %q, want assistant-3", messages[0].Content)
	}
	if messages[len(messages)-1].Content != "assistant-6" {
		t.Fatalf("last recovered message = %q, want assistant-6", messages[len(messages)-1].Content)
	}
	for _, msg := range messages {
		if strings.Contains(msg.Content, "prompt snapshot") {
			t.Fatalf("prompt snapshot was recovered as visible message: %#v", msg)
		}
	}
}
