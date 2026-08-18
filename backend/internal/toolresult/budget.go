package toolresult

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"unicode/utf8"

	"github.com/cloudwego/eino/schema"
)

const (
	DefaultSingleResultBytes     = 50_000
	DefaultMessageAggregateBytes = 200_000
	DefaultPreviewBytes          = 2_000
	DefaultSpillDir              = ".oncall/tool_results"
)

// Config controls the first compression layer that replaces oversize tool
// results with stable previews before each model call.
type Config struct {
	SpillDir              string
	SingleResultBytes     int
	MessageAggregateBytes int
	PreviewBytes          int
}

// ContentReplacementState freezes replacement decisions by tool_call_id. Once
// an id has been seen, future passes either replay the same preview or keep the
// original result unchanged.
type ContentReplacementState struct {
	mu           sync.Mutex
	SeenIDs      map[string]struct{}
	Replacements map[string]string
}

// NewContentReplacementState returns an empty replacement state.
func NewContentReplacementState() *ContentReplacementState {
	return &ContentReplacementState{
		SeenIDs:      map[string]struct{}{},
		Replacements: map[string]string{},
	}
}

// MarkOriginal freezes id as a non-spilled result. Middleware uses this for
// ReadFile calls that intentionally read back a spill file.
func (s *ContentReplacementState) MarkOriginal(id string) {
	if s == nil || strings.TrimSpace(id) == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.ensureLocked()
	s.SeenIDs[id] = struct{}{}
	delete(s.Replacements, id)
}

func (s *ContentReplacementState) ensureLocked() {
	if s.SeenIDs == nil {
		s.SeenIDs = map[string]struct{}{}
	}
	if s.Replacements == nil {
		s.Replacements = map[string]string{}
	}
}

// ReplacementRecord describes one result persisted to disk.
type ReplacementRecord struct {
	ToolCallID   string
	ToolName     string
	OriginalSize int
	PreviewSize  int
	SpillPath    string
}

type candidate struct {
	msgIndex int
	id       string
	name     string
	content  string
	size     int
	replaced bool
	preview  string
}

// Apply returns a cloned message slice where oversize tool results have been
// replaced with stable persisted-output previews.
func Apply(messages []*schema.Message, workDir string, state *ContentReplacementState, cfg Config) ([]*schema.Message, []ReplacementRecord, error) {
	if state == nil {
		state = NewContentReplacementState()
	}
	cfg = normalizeConfig(cfg, workDir)

	out := cloneMessages(messages)

	state.mu.Lock()
	defer state.mu.Unlock()
	state.ensureLocked()

	if err := os.MkdirAll(cfg.SpillDir, 0o700); err != nil {
		return nil, nil, fmt.Errorf("create tool result spill dir: %w", err)
	}

	candidates := map[int]*candidate{}
	ordered := make([]*candidate, 0)
	for i, msg := range out {
		if !isToolResult(msg) {
			continue
		}
		id := stableToolResultID(i, msg)
		if preview, ok := state.Replacements[id]; ok {
			msg.Content = preview
			continue
		}
		if _, seen := state.SeenIDs[id]; seen {
			continue
		}
		c := &candidate{
			msgIndex: i,
			id:       id,
			name:     msg.ToolName,
			content:  msg.Content,
			size:     len([]byte(msg.Content)),
		}
		candidates[i] = c
		ordered = append(ordered, c)
	}

	var records []ReplacementRecord
	for _, c := range ordered {
		if c.size > cfg.SingleResultBytes {
			record, err := replaceCandidate(out[c.msgIndex], c, cfg, state)
			if err != nil {
				return nil, nil, err
			}
			records = append(records, record)
		}
	}

	for _, group := range toolResultGroups(out) {
		total := 0
		groupCandidates := make([]*candidate, 0, len(group))
		for _, idx := range group {
			total += len([]byte(out[idx].Content))
			if c := candidates[idx]; c != nil && !c.replaced {
				groupCandidates = append(groupCandidates, c)
			}
		}
		if total <= cfg.MessageAggregateBytes || len(groupCandidates) == 0 {
			continue
		}
		sort.Slice(groupCandidates, func(i, j int) bool {
			return groupCandidates[i].size > groupCandidates[j].size
		})
		for _, c := range groupCandidates {
			record, err := replaceCandidate(out[c.msgIndex], c, cfg, state)
			if err != nil {
				return nil, nil, err
			}
			records = append(records, record)
			total = total - c.size + len([]byte(c.preview))
			if total <= cfg.MessageAggregateBytes {
				break
			}
		}
	}

	for _, c := range ordered {
		if !c.replaced {
			state.SeenIDs[c.id] = struct{}{}
		}
	}

	return out, records, nil
}

func replaceCandidate(msg *schema.Message, c *candidate, cfg Config, state *ContentReplacementState) (ReplacementRecord, error) {
	spillPath := filepath.Join(cfg.SpillDir, spillFileName(c.id, c.content))
	if err := os.WriteFile(spillPath, []byte(c.content), 0o600); err != nil {
		return ReplacementRecord{}, fmt.Errorf("write tool result spill %s: %w", spillPath, err)
	}
	preview := buildPreview(c, spillPath, cfg.PreviewBytes)
	msg.Content = preview
	c.replaced = true
	c.preview = preview
	state.SeenIDs[c.id] = struct{}{}
	state.Replacements[c.id] = preview
	return ReplacementRecord{
		ToolCallID:   c.id,
		ToolName:     c.name,
		OriginalSize: c.size,
		PreviewSize:  len([]byte(preview)),
		SpillPath:    spillPath,
	}, nil
}

func normalizeConfig(cfg Config, workDir string) Config {
	if cfg.SingleResultBytes <= 0 {
		cfg.SingleResultBytes = DefaultSingleResultBytes
	}
	if cfg.MessageAggregateBytes <= 0 {
		cfg.MessageAggregateBytes = DefaultMessageAggregateBytes
	}
	if cfg.PreviewBytes <= 0 {
		cfg.PreviewBytes = DefaultPreviewBytes
	}
	if strings.TrimSpace(workDir) == "" {
		workDir = "."
	}
	if strings.TrimSpace(cfg.SpillDir) == "" {
		cfg.SpillDir = DefaultSpillDir
	}
	if !filepath.IsAbs(cfg.SpillDir) {
		cfg.SpillDir = filepath.Join(workDir, cfg.SpillDir)
	}
	cfg.SpillDir = filepath.Clean(cfg.SpillDir)
	return cfg
}

func cloneMessages(messages []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			out = append(out, nil)
			continue
		}
		cp := *msg
		if msg.ToolCalls != nil {
			cp.ToolCalls = append([]schema.ToolCall(nil), msg.ToolCalls...)
		}
		if msg.Extra != nil {
			cp.Extra = make(map[string]any, len(msg.Extra))
			for k, v := range msg.Extra {
				cp.Extra[k] = v
			}
		}
		out = append(out, &cp)
	}
	return out
}

func isToolResult(msg *schema.Message) bool {
	return msg != nil && msg.Role == schema.Tool && strings.TrimSpace(msg.Content) != ""
}

func stableToolResultID(index int, msg *schema.Message) string {
	if msg == nil {
		return fmt.Sprintf("message-%d", index)
	}
	if strings.TrimSpace(msg.ToolCallID) != "" {
		return strings.TrimSpace(msg.ToolCallID)
	}
	sum := sha256.Sum256([]byte(fmt.Sprintf("%d:%s:%s", index, msg.ToolName, msg.Content)))
	return "message-" + hex.EncodeToString(sum[:8])
}

func toolResultGroups(messages []*schema.Message) [][]int {
	var groups [][]int
	var current []int
	flush := func() {
		if len(current) > 0 {
			groups = append(groups, current)
			current = nil
		}
	}
	for i, msg := range messages {
		if isToolResult(msg) {
			current = append(current, i)
			continue
		}
		flush()
	}
	flush()
	return groups
}

func spillFileName(id, content string) string {
	safeID := sanitizeFilePart(id)
	sum := sha256.Sum256([]byte(content))
	return safeID + "-" + hex.EncodeToString(sum[:6]) + ".txt"
}

func sanitizeFilePart(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return "tool-result"
	}
	var b strings.Builder
	for _, r := range s {
		switch {
		case r >= 'a' && r <= 'z':
			b.WriteRune(r)
		case r >= 'A' && r <= 'Z':
			b.WriteRune(r)
		case r >= '0' && r <= '9':
			b.WriteRune(r)
		case r == '-' || r == '_' || r == '.':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	out := b.String()
	if len(out) > 80 {
		out = out[:80]
	}
	return out
}

func buildPreview(c *candidate, spillPath string, previewBytes int) string {
	preview := firstBytes(c.content, previewBytes)
	toolName := c.name
	if toolName == "" {
		toolName = "unknown"
	}
	return fmt.Sprintf("<persisted-tool-result>\nTool result %s from %s was too large (%d bytes). Full content was saved to:\n%s\n\nPreview (first %d bytes):\n%s\n</persisted-tool-result>", c.id, toolName, c.size, spillPath, len([]byte(preview)), preview)
}

func firstBytes(s string, maxBytes int) string {
	if maxBytes <= 0 || len([]byte(s)) <= maxBytes {
		return s
	}
	var b strings.Builder
	used := 0
	for _, r := range s {
		size := utf8.RuneLen(r)
		if size < 0 {
			size = len(string(r))
		}
		if used+size > maxBytes {
			break
		}
		b.WriteRune(r)
		used += size
	}
	return b.String()
}

// IsPathInSpillDir reports whether path resolves under the configured spill
// directory. It is used to avoid spilling ReadFile results that read back a
// previously persisted tool result.
func IsPathInSpillDir(path, workDir string, cfg Config) bool {
	if strings.TrimSpace(path) == "" {
		return false
	}
	cfg = normalizeConfig(cfg, workDir)
	target := path
	if !filepath.IsAbs(target) {
		base := workDir
		if strings.TrimSpace(base) == "" {
			base = "."
		}
		target = filepath.Join(base, target)
	}
	target = filepath.Clean(target)
	rel, err := filepath.Rel(cfg.SpillDir, target)
	if err != nil {
		return false
	}
	return rel == "." || (rel != "" && !strings.HasPrefix(rel, "..") && !filepath.IsAbs(rel))
}
