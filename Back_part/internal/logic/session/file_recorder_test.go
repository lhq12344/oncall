package session

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/cloudwego/eino/schema"
)

func TestFileSessionRecorderWritesPrivateFiles(t *testing.T) {
	dir := t.TempDir()
	recorder := NewFileSessionRecorder(dir)

	if err := recorder.AppendTurn(context.Background(), "session-1", "chat", schema.UserMessage("hello"), schema.AssistantMessage("world", nil)); err != nil {
		t.Fatalf("append turn: %v", err)
	}

	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatalf("read dir: %v", err)
	}
	if len(entries) != 1 {
		t.Fatalf("expected one record file, got %d", len(entries))
	}

	info, err := os.Stat(filepath.Join(dir, entries[0].Name()))
	if err != nil {
		t.Fatalf("stat record file: %v", err)
	}
	if mode := info.Mode().Perm(); mode != 0o600 {
		t.Fatalf("expected 0600 permissions, got %o", mode)
	}
}

func TestNewSessionFileRecorderFromEnvDisabledByDefault(t *testing.T) {
	t.Setenv("SESSION_FILE_RECORD_ENABLED", "")
	if recorder := newSessionFileRecorderFromEnv(); recorder != nil {
		t.Fatalf("expected recorder to be disabled by default")
	}
}

func TestNewSessionFileRecorderFromEnvEnabled(t *testing.T) {
	t.Setenv("SESSION_FILE_RECORD_ENABLED", "true")
	dir := t.TempDir()
	t.Setenv("SESSION_FILE_RECORD_DIR", dir)

	recorder := newSessionFileRecorderFromEnv()
	if recorder == nil {
		t.Fatalf("expected recorder to be enabled")
	}
	if got := strings.TrimSpace(recorder.dir); got != dir {
		t.Fatalf("expected record dir %q, got %q", dir, got)
	}
}
