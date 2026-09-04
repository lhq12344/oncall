package context

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestSessionMemoryPersistsTurnsWithoutRedis(t *testing.T) {
	t.Parallel()

	memory := NewSessionMemory(nil, nil)
	memory.SaveTurn(context.Background(), "session-1", "first question", "first answer", []*schema.Message{schema.UserMessage("first question")})

	messages, err := memory.BuildMessages(context.Background(), "session-1", "second question")
	if err != nil {
		t.Fatalf("BuildMessages returned error: %v", err)
	}
	contents := make([]string, 0, len(messages))
	for _, msg := range messages {
		contents = append(contents, msg.Content)
	}
	want := []string{"first question", "first answer", "second question"}
	if len(contents) != len(want) {
		t.Fatalf("messages=%v, want %v", contents, want)
	}
	for i := range want {
		if contents[i] != want[i] {
			t.Fatalf("messages=%v, want %v", contents, want)
		}
	}
}
