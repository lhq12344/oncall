package artifacts

import (
	"context"
	"strings"
	"testing"
)

func TestLocalStoreRedactsSecrets(t *testing.T) {
	store := NewLocalStore(t.TempDir())
	ref, err := store.Put(context.Background(), "tool_output", []byte("password=abc token:xyz ok"))
	if err != nil {
		t.Fatalf("Put: %v", err)
	}
	data, _, err := store.Get(context.Background(), ref.ID)
	if err != nil {
		t.Fatalf("Get: %v", err)
	}
	text := string(data)
	if strings.Contains(text, "abc") || strings.Contains(text, "xyz") || !strings.Contains(text, "[redacted]") {
		t.Fatalf("secret was not redacted: %q", text)
	}
}
