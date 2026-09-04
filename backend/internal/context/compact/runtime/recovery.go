package compact

import (
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/cloudwego/eino/schema"
)

const (
	recoveryFileLimit     = 5
	recoveryTokensPerFile = 5_000
	recoveryToolLimit     = 8
	recoveryTokensPerTool = 1_500
	recoveryCharsPerToken = 3.5
)

type fileReadRecord struct {
	Path      string
	Content   string
	Timestamp time.Time
}

type toolRecord struct {
	Name      string
	CallID    string
	Output    string
	Timestamp time.Time
}

// RecoveryState stores compact recovery attachments for one session.
type RecoveryState struct {
	mu    sync.Mutex
	files map[string]fileReadRecord
	tools map[string]toolRecord
}

func newRecoveryState() *RecoveryState {
	return &RecoveryState{
		files: map[string]fileReadRecord{},
		tools: map[string]toolRecord{},
	}
}

func (s *RecoveryState) recordFileRead(path, content string) {
	if s == nil || strings.TrimSpace(path) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.files[path] = fileReadRecord{Path: path, Content: truncateByTokens(content, recoveryTokensPerFile), Timestamp: time.Now()}
}

func (s *RecoveryState) recordTool(name, callID, output string) {
	if s == nil || strings.TrimSpace(name) == "" || strings.TrimSpace(output) == "" {
		return
	}
	key := callID
	if key == "" {
		key = fmt.Sprintf("%s-%d", name, time.Now().UnixNano())
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.tools[key] = toolRecord{Name: name, CallID: callID, Output: truncateByTokens(output, recoveryTokensPerTool), Timestamp: time.Now()}
}

func (s *RecoveryState) snapshotFiles(limit int) []fileReadRecord {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]fileReadRecord, 0, len(s.files))
	for _, rec := range s.files {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func (s *RecoveryState) snapshotTools(limit int) []toolRecord {
	if s == nil {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	out := make([]toolRecord, 0, len(s.tools))
	for _, rec := range s.tools {
		out = append(out, rec)
	}
	sort.Slice(out, func(i, j int) bool {
		return out[i].Timestamp.After(out[j].Timestamp)
	})
	if limit > 0 && len(out) > limit {
		out = out[:limit]
	}
	return out
}

func buildRecoveryAttachment(state *RecoveryState, tools []*schema.ToolInfo) string {
	var sb strings.Builder

	if files := state.snapshotFiles(recoveryFileLimit); len(files) > 0 {
		sb.WriteString("## Recently read files\n\n")
		sb.WriteString("These are the latest file snapshots returned by ReadFile before compaction. Re-read files if exact current bytes are needed.\n\n")
		for _, rec := range files {
			fmt.Fprintf(&sb, "### %s (%s)\n\n", rec.Path, rec.Timestamp.UTC().Format(time.RFC3339))
			sb.WriteString("~~~\n")
			sb.WriteString(truncateByTokens(rec.Content, recoveryTokensPerFile))
			if !strings.HasSuffix(rec.Content, "\n") {
				sb.WriteByte('\n')
			}
			sb.WriteString("~~~\n\n")
		}
	}

	if records := state.snapshotTools(recoveryToolLimit); len(records) > 0 {
		sb.WriteString("## Recent tool results\n\n")
		for _, rec := range records {
			name := rec.Name
			if rec.CallID != "" {
				name += " / " + rec.CallID
			}
			fmt.Fprintf(&sb, "### %s\n\n", name)
			sb.WriteString("~~~\n")
			sb.WriteString(truncateByTokens(rec.Output, recoveryTokensPerTool))
			if !strings.HasSuffix(rec.Output, "\n") {
				sb.WriteByte('\n')
			}
			sb.WriteString("~~~\n\n")
		}
	}

	if len(tools) > 0 {
		sb.WriteString("## Available tools\n\n")
		for _, info := range tools {
			if info == nil || strings.TrimSpace(info.Name) == "" {
				continue
			}
			desc := firstLine(info.Desc)
			if desc == "" {
				fmt.Fprintf(&sb, "- %s\n", info.Name)
			} else {
				fmt.Fprintf(&sb, "- %s - %s\n", info.Name, desc)
			}
		}
		sb.WriteString("\n")
	}

	if sb.Len() == 0 {
		return ""
	}
	sb.WriteString("## Note\n\nThis block is reconstructed context after automatic compaction. Re-read source data for exact code, log lines, or user text.\n")
	return sb.String()
}

func truncateByTokens(s string, tokenBudget int) string {
	if tokenBudget <= 0 || approxTokens(s) <= tokenBudget {
		return s
	}
	maxBytes := int(float64(tokenBudget) * recoveryCharsPerToken)
	if maxBytes <= 0 || maxBytes >= len([]byte(s)) {
		return s
	}
	return firstUTF8Bytes(s, maxBytes) + "\n... (content truncated)"
}

func approxTokens(s string) int {
	if s == "" {
		return 0
	}
	return int(float64(len([]byte(s))) / recoveryCharsPerToken)
}

func firstUTF8Bytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len([]byte(s)) <= maxBytes {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		size := len(string(r))
		if used+size > maxBytes {
			break
		}
		b.WriteRune(r)
		used += size
	}
	return b.String()
}

func firstLine(s string) string {
	for _, line := range strings.Split(s, "\n") {
		if trimmed := strings.TrimSpace(line); trimmed != "" {
			return trimmed
		}
	}
	return ""
}
