package transcript

import (
	"context"
	"testing"
)

func TestMemoryStoreLifecycleIsTranscriptOnly(t *testing.T) {
	store := NewMemoryStore()
	if err := store.Append(context.Background(), "s1", Turn{User: Message{Role: "user", Content: "hi"}, Assistant: Message{Role: "assistant", Content: "hello"}}); err != nil {
		t.Fatalf("Append: %v", err)
	}
	if err := store.Append(context.Background(), "s1", Turn{User: Message{Role: "user", Content: "next"}, Assistant: Message{Role: "assistant", Content: "ok"}}); err != nil {
		t.Fatalf("Append second: %v", err)
	}
	if err := store.Compact(context.Background(), "s1", 2); err != nil {
		t.Fatalf("Compact: %v", err)
	}
	got, err := store.Load(context.Background(), "s1")
	if err != nil || len(got) != 2 || got[0].Content != "next" {
		t.Fatalf("Load got=%+v err=%v", got, err)
	}
}
