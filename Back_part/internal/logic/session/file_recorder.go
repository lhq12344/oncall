package session

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
)

const defaultSessionRecordDir = ".run/sessions"
const defaultSessionRecoveryLines = 10

// FileSessionRecorder appends session turns to per-session JSONL files.
type FileSessionRecorder struct {
	dir string
	mu  sync.Mutex
}

type sessionRecordLine struct {
	Type       string          `json:"type"`
	SessionID  string          `json:"session_id"`
	CreatedAt  string          `json:"created_at,omitempty"`
	TurnID     string          `json:"turn_id,omitempty"`
	Source     string          `json:"source,omitempty"`
	Role       string          `json:"role,omitempty"`
	Content    string          `json:"content,omitempty"`
	ToolCallID string          `json:"tool_call_id,omitempty"`
	ToolCalls  json.RawMessage `json:"tool_calls,omitempty"`
	Index      int             `json:"index,omitempty"`
	Timestamp  string          `json:"timestamp,omitempty"`
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
	return r.AppendTurnWithPrompt(ctx, sessionID, source, nil, userMsg, assistantMsg)
}

// AppendTurnWithPrompt appends the actual prompt snapshot plus the visible user/assistant turn.
func (r *FileSessionRecorder) AppendTurnWithPrompt(
	ctx context.Context,
	sessionID string,
	source string,
	promptMessages []*schema.Message,
	userMsg *schema.Message,
	assistantMsg *schema.Message,
) error {
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
	lines := make([]sessionRecordLine, 0, len(promptMessages)+2)
	for index, msg := range promptMessages {
		if msg == nil {
			continue
		}
		lines = append(lines, buildMessageRecord("prompt_message", sessionID, turnID, source, msg, index+1, now))
	}
	lines = append(lines,
		buildMessageRecord("message", sessionID, turnID, source, userMsg, 0, now),
		buildMessageRecord("message", sessionID, turnID, source, assistantMsg, 0, now),
	)

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

// LoadRecentMessages loads visible conversation messages from the last n JSONL rows.
func (r *FileSessionRecorder) LoadRecentMessages(ctx context.Context, sessionID string, n int) ([]*schema.Message, error) {
	if r == nil {
		return nil, nil
	}
	if err := ctxErr(ctx); err != nil {
		return nil, err
	}
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, fmt.Errorf("session id is required")
	}
	if n <= 0 {
		n = defaultSessionRecoveryLines
	}

	path := filepath.Join(r.dir, safeSessionRecordFileName(sessionID)+".jsonl")
	lines, err := readLastLines(path, n)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}

	messages := make([]*schema.Message, 0, len(lines))
	for _, line := range lines {
		if err := ctxErr(ctx); err != nil {
			return nil, err
		}
		var record sessionRecordLine
		if err := json.Unmarshal([]byte(line), &record); err != nil {
			return nil, err
		}
		msg := record.toVisibleMessage(sessionID)
		if msg == nil {
			continue
		}
		messages = append(messages, msg)
	}
	return messages, nil
}

func buildMessageRecord(recordType string, sessionID string, turnID string, source string, msg *schema.Message, index int, now time.Time) sessionRecordLine {
	role := ""
	content := ""
	toolCallID := ""
	var toolCalls json.RawMessage
	if msg != nil {
		role = string(msg.Role)
		content = msg.Content
		toolCallID = msg.ToolCallID
		if len(msg.ToolCalls) > 0 {
			if payload, err := json.Marshal(msg.ToolCalls); err == nil {
				toolCalls = payload
			}
		}
	}
	return sessionRecordLine{
		Type:       recordType,
		SessionID:  sessionID,
		TurnID:     turnID,
		Source:     strings.TrimSpace(source),
		Role:       role,
		Content:    content,
		ToolCallID: toolCallID,
		ToolCalls:  toolCalls,
		Index:      index,
		Timestamp:  now.Format(time.RFC3339Nano),
	}
}

func (r sessionRecordLine) toVisibleMessage(sessionID string) *schema.Message {
	if r.Type != "message" || r.SessionID != sessionID {
		return nil
	}
	msg := &schema.Message{
		Role:       schema.RoleType(r.Role),
		Content:    r.Content,
		ToolCallID: r.ToolCallID,
	}
	if len(r.ToolCalls) > 0 {
		_ = json.Unmarshal(r.ToolCalls, &msg.ToolCalls)
	}
	switch msg.Role {
	case schema.User, schema.Assistant, schema.Tool:
		return msg
	default:
		return nil
	}
}

func readLastLines(path string, n int) ([]string, error) {
	if n <= 0 {
		return nil, nil
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return nil, nil
	}

	var (
		pos      = info.Size()
		buffer   []byte
		tmp      = make([]byte, 4096)
		newlines int
	)
	for pos > 0 && newlines <= n {
		readSize := int64(len(tmp))
		if pos < readSize {
			readSize = pos
		}
		pos -= readSize
		chunk := tmp[:readSize]
		if _, err := file.ReadAt(chunk, pos); err != nil && err != io.EOF {
			return nil, err
		}
		for i := len(chunk) - 1; i >= 0; i-- {
			if chunk[i] == '\n' {
				newlines++
			}
		}
		buffer = append(append([]byte(nil), chunk...), buffer...)
	}

	parts := strings.Split(strings.TrimRight(string(buffer), "\n"), "\n")
	if len(parts) > n {
		parts = parts[len(parts)-n:]
	}
	return parts, nil
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
