package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
)

const defaultSessionRecordDir = ".run/sessions"

// FileSessionRecorder appends user-visible session turns to per-session JSONL files.
type FileSessionRecorder struct {
	dir string
	mu  sync.Mutex
}

type sessionRecordLine struct {
	Type      string `json:"type"`
	SessionID string `json:"session_id"`
	CreatedAt string `json:"created_at,omitempty"`
	TurnID    string `json:"turn_id,omitempty"`
	Source    string `json:"source,omitempty"`
	Role      string `json:"role,omitempty"`
	Content   string `json:"content,omitempty"`
	Timestamp string `json:"timestamp,omitempty"`
}

// NewFileSessionRecorder creates an append-only file recorder.
func NewFileSessionRecorder(dir string) *FileSessionRecorder {
	dir = strings.TrimSpace(dir)
	if dir == "" {
		dir = defaultSessionRecordDir
	}
	return &FileSessionRecorder{dir: dir}
}

// AppendTurn appends one user/assistant turn to a session JSONL file.
func (r *FileSessionRecorder) AppendTurn(ctx context.Context, sessionID string, source string, userMsg *schema.Message, assistantMsg *schema.Message) error {
	if r == nil {
		return nil
	}
	if err := ctxErr(ctx); err != nil {
		return err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session id is required")
	}
	if userMsg == nil || assistantMsg == nil {
		return fmt.Errorf("user and assistant messages are required")
	}

	if err := os.MkdirAll(r.dir, 0o700); err != nil {
		return err
	}

	path := filepath.Join(r.dir, safeSessionRecordFileName(sessionID)+".jsonl")
	now := time.Now().UTC()
	turnID := fmt.Sprintf("%d", now.UnixNano())
	lines := []sessionRecordLine{
		buildMessageRecord(sessionID, turnID, source, userMsg, now),
		buildMessageRecord(sessionID, turnID, source, assistantMsg, now),
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	needsHeader, err := fileNeedsSessionHeader(path)
	if err != nil {
		return err
	}

	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return err
	}
	defer file.Close()

	writer := bufio.NewWriter(file)
	if needsHeader {
		if err := writeSessionRecordLine(writer, sessionRecordLine{
			Type:      "session_header",
			SessionID: sessionID,
			CreatedAt: now.Format(time.RFC3339Nano),
		}); err != nil {
			return err
		}
	}
	for _, line := range lines {
		if err := writeSessionRecordLine(writer, line); err != nil {
			return err
		}
	}
	return writer.Flush()
}

func buildMessageRecord(sessionID string, turnID string, source string, msg *schema.Message, now time.Time) sessionRecordLine {
	role := ""
	content := ""
	if msg != nil {
		role = string(msg.Role)
		content = msg.Content
	}
	return sessionRecordLine{
		Type:      "message",
		SessionID: sessionID,
		TurnID:    turnID,
		Source:    strings.TrimSpace(source),
		Role:      role,
		Content:   content,
		Timestamp: now.Format(time.RFC3339Nano),
	}
}

func fileNeedsSessionHeader(path string) (bool, error) {
	info, err := os.Stat(path)
	if err == nil {
		return info.Size() == 0, nil
	}
	if os.IsNotExist(err) {
		return true, nil
	}
	return false, err
}

func writeSessionRecordLine(writer *bufio.Writer, line sessionRecordLine) error {
	payload, err := json.Marshal(line)
	if err != nil {
		return err
	}
	if _, err := writer.Write(payload); err != nil {
		return err
	}
	return writer.WriteByte('\n')
}

func safeSessionRecordFileName(sessionID string) string {
	var builder strings.Builder
	for _, r := range sessionID {
		switch {
		case r >= 'a' && r <= 'z':
			builder.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			builder.WriteRune(r)
		case r >= '0' && r <= '9':
			builder.WriteRune(r)
		case r == '-' || r == '_':
			builder.WriteRune(r)
		default:
			builder.WriteByte('_')
		}
	}
	name := builder.String()
	if strings.TrimSpace(name) == "" {
		return "session"
	}
	return name
}

func ctxErr(ctx context.Context) error {
	if ctx == nil {
		return nil
	}
	return ctx.Err()
}
