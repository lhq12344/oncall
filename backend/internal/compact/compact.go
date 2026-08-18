package compact

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
	"sync"

	"go_agent/internal/toolresult"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
)

const (
	defaultContextWindow     = 128_000
	defaultMaxOutputTokens   = 8_192
	defaultTailTokens        = 40_000
	defaultSoftFailureLimit  = 2
	contextCharsPerToken     = 3.5
	compactSummaryMaxTokens  = 2_000
	sessionIDKey             = "session_id"
	defaultSessionIDFallback = "_default"
)

// Config controls live context compaction for ADK ChatModelAgent calls.
type Config struct {
	Model            model.BaseChatModel
	WorkDir          string
	ContextWindow    int
	MaxOutputTokens  int
	TailTokens       int
	SoftFailureLimit int
	ToolResults      toolresult.Config
	Store            *Store
}

// Store keeps per-session compact state in-process.
type Store struct {
	mu       sync.Mutex
	sessions map[string]*SessionState
}

// SessionState tracks compression decisions and usage anchors for one session.
type SessionState struct {
	mu                sync.Mutex
	Replacements      *toolresult.ContentReplacementState
	Recovery          *RecoveryState
	Usage             UsageAnchor
	SoftFailures      int
	CompactionCount   int
	LastUsedTokens    int
	LastCompactReason string
}

// UsageAnchor blends real provider prompt usage with estimates for messages
// appended after that usage was recorded.
type UsageAnchor struct {
	HasUsage     bool
	PromptTokens int
	MessageCount int
}

// NewStore returns an empty in-memory state store.
func NewStore() *Store {
	return &Store{sessions: map[string]*SessionState{}}
}

func (s *Store) forSession(id string) *SessionState {
	if s == nil {
		s = defaultStore
	}
	if strings.TrimSpace(id) == "" {
		id = defaultSessionIDFallback
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.sessions == nil {
		s.sessions = map[string]*SessionState{}
	}
	if st := s.sessions[id]; st != nil {
		return st
	}
	st := &SessionState{
		Replacements: toolresult.NewContentReplacementState(),
		Recovery:     newRecoveryState(),
	}
	s.sessions[id] = st
	return st
}

var defaultStore = NewStore()

// Middleware implements Mew-style live context compaction for ADK agents.
type Middleware struct {
	*adk.BaseChatModelAgentMiddleware
	cfg Config
}

// NewMiddleware creates a compact middleware. It is safe to reuse across agent
// runs; state is partitioned by session_id.
func NewMiddleware(cfg Config) *Middleware {
	cfg = normalizeMiddlewareConfig(cfg)
	return &Middleware{
		BaseChatModelAgentMiddleware: &adk.BaseChatModelAgentMiddleware{},
		cfg:                          cfg,
	}
}

func normalizeMiddlewareConfig(cfg Config) Config {
	if cfg.ContextWindow <= 0 {
		cfg.ContextWindow = defaultContextWindow
	}
	if cfg.MaxOutputTokens <= 0 {
		cfg.MaxOutputTokens = defaultMaxOutputTokens
	}
	if cfg.TailTokens <= 0 {
		cfg.TailTokens = defaultTailTokens
	}
	if cfg.SoftFailureLimit <= 0 {
		cfg.SoftFailureLimit = defaultSoftFailureLimit
	}
	if strings.TrimSpace(cfg.WorkDir) == "" {
		cfg.WorkDir = "."
	}
	if cfg.Store == nil {
		cfg.Store = defaultStore
	}
	return cfg
}

func (m *Middleware) sessionState(ctx context.Context) *SessionState {
	return m.cfg.Store.forSession(sessionIDFromContext(ctx))
}

func sessionIDFromContext(ctx context.Context) string {
	if v, ok := adk.GetSessionValue(ctx, sessionIDKey); ok && v != nil {
		return fmt.Sprint(v)
	}
	return defaultSessionIDFallback
}

// BeforeModelRewriteState applies tool-result budget first, then compacts the
// live message list if the soft or hard threshold has been crossed.
func (m *Middleware) BeforeModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if state == nil {
		return ctx, state, nil
	}
	session := m.sessionState(ctx)
	messages, _, err := toolresult.Apply(state.Messages, m.cfg.WorkDir, session.Replacements, m.cfg.ToolResults)
	if err != nil {
		return ctx, state, err
	}
	tools := modelTools(mc)
	managed, _, err := m.manageMessages(ctx, messages, session, tools, false)
	if err != nil {
		return ctx, state, err
	}
	state.Messages = managed
	return ctx, state, nil
}

// AfterModelRewriteState records real prompt usage when the provider returns
// it, giving future token estimates a stable anchor.
func (m *Middleware) AfterModelRewriteState(ctx context.Context, state *adk.ChatModelAgentState, mc *adk.ModelContext) (context.Context, *adk.ChatModelAgentState, error) {
	if state == nil || len(state.Messages) == 0 {
		return ctx, state, nil
	}
	last := state.Messages[len(state.Messages)-1]
	if last != nil && last.ResponseMeta != nil && last.ResponseMeta.Usage != nil && last.ResponseMeta.Usage.PromptTokens > 0 {
		session := m.sessionState(ctx)
		session.mu.Lock()
		session.Usage = UsageAnchor{HasUsage: true, PromptTokens: last.ResponseMeta.Usage.PromptTokens, MessageCount: len(state.Messages)}
		session.mu.Unlock()
	}
	return ctx, state, nil
}

func (m *Middleware) WrapInvokableToolCall(ctx context.Context, endpoint adk.InvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.InvokableToolCallEndpoint, error) {
	return func(ctx context.Context, argumentsInJSON string, opts ...einotool.Option) (string, error) {
		session := m.sessionState(ctx)
		m.markSpillReadback(session, tCtx, argumentsInJSON)
		out, err := endpoint(ctx, argumentsInJSON, opts...)
		if err == nil {
			m.recordToolOutput(session, tCtx, argumentsInJSON, out)
		}
		return out, err
	}, nil
}

func (m *Middleware) WrapEnhancedInvokableToolCall(ctx context.Context, endpoint adk.EnhancedInvokableToolCallEndpoint, tCtx *adk.ToolContext) (adk.EnhancedInvokableToolCallEndpoint, error) {
	return func(ctx context.Context, arg *schema.ToolArgument, opts ...einotool.Option) (*schema.ToolResult, error) {
		session := m.sessionState(ctx)
		argJSON := toolArgumentJSON(arg)
		m.markSpillReadback(session, tCtx, argJSON)
		result, err := endpoint(ctx, arg, opts...)
		if err == nil {
			out := toolResultText(result)
			m.recordToolOutput(session, tCtx, argJSON, out)
		}
		return result, err
	}, nil
}

func (m *Middleware) WrapModel(ctx context.Context, base model.BaseChatModel, mc *adk.ModelContext) (model.BaseChatModel, error) {
	if base == nil {
		return base, nil
	}
	return &wrappedModel{base: base, middleware: m, tools: modelTools(mc)}, nil
}

func (m *Middleware) manageMessages(ctx context.Context, messages []*schema.Message, session *SessionState, tools []*schema.ToolInfo, force bool) ([]*schema.Message, bool, error) {
	if session == nil {
		session = m.sessionState(ctx)
	}
	used := computeUsedTokens(messages, session.Usage)
	soft := computeCompactThreshold(m.cfg.ContextWindow, m.cfg.MaxOutputTokens, false)
	hard := computeCompactThreshold(m.cfg.ContextWindow, m.cfg.MaxOutputTokens, true)
	session.mu.Lock()
	session.LastUsedTokens = used
	softFailures := session.SoftFailures
	session.mu.Unlock()

	if !force && used < soft {
		return messages, false, nil
	}
	force = force || used >= hard
	if !force && softFailures >= m.cfg.SoftFailureLimit {
		return messages, false, nil
	}

	compacted, err := m.compactMessages(ctx, messages, session, tools, force)
	if err != nil {
		if force {
			fallback, changed := m.forceDropOldest(messages, session, tools, err)
			return fallback, changed, nil
		}
		session.mu.Lock()
		session.SoftFailures++
		session.mu.Unlock()
		return messages, false, nil
	}
	if sameMessages(compacted, messages) {
		return messages, false, nil
	}
	session.mu.Lock()
	session.SoftFailures = 0
	session.CompactionCount++
	session.Usage = UsageAnchor{}
	session.LastCompactReason = "threshold"
	session.mu.Unlock()
	return compacted, true, nil
}

func (m *Middleware) compactMessages(ctx context.Context, messages []*schema.Message, session *SessionState, tools []*schema.ToolInfo, force bool) ([]*schema.Message, error) {
	leadingEnd := leadingSystemEnd(messages)
	keepStart := computeKeepStartIndex(messages, leadingEnd, m.cfg.TailTokens)
	if keepStart <= leadingEnd || keepStart >= len(messages) {
		if force {
			out, _ := m.forceDropOldest(messages, session, tools, errors.New("not enough prefix to summarize"))
			return out, nil
		}
		return messages, nil
	}

	prefix := messages[leadingEnd:keepStart]
	tail := messages[keepStart:]
	summary, err := m.summarize(ctx, prefix)
	if err != nil {
		if force {
			out, _ := m.forceDropOldest(messages, session, tools, err)
			return out, nil
		}
		return nil, err
	}
	attachment := buildRecoveryAttachment(session.Recovery, tools)
	content := "以下是自动上下文压缩后的早期对话摘要。继续任务时优先相信后续未压缩消息；需要精确内容时重新读取来源。\n\n" + strings.TrimSpace(summary)
	if attachment != "" {
		content += "\n\n---\n\n" + attachment
	}
	return rebuildMessages(messages[:leadingEnd], schema.SystemMessage(content), tail), nil
}

func (m *Middleware) summarize(ctx context.Context, prefix []*schema.Message) (string, error) {
	if m.cfg.Model == nil {
		return "", errors.New("compact summary model is nil")
	}
	prompt := []*schema.Message{
		schema.SystemMessage("You summarize earlier agent conversation context for a DevOps/SRE assistant. Preserve user goals, decisions, constraints, tool findings, file paths, errors, and unresolved next steps. Do not invent facts."),
		schema.UserMessage("Summarize the following earlier context into concise structured Chinese notes:\n\n" + formatMessages(prefix)),
	}
	resp, err := m.cfg.Model.Generate(ctx, prompt, model.WithMaxTokens(compactSummaryMaxTokens))
	if err != nil {
		return "", err
	}
	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		return "", errors.New("compact summary model returned empty summary")
	}
	return strings.TrimSpace(resp.Content), nil
}

func (m *Middleware) forceDropOldest(messages []*schema.Message, session *SessionState, tools []*schema.ToolInfo, cause error) ([]*schema.Message, bool) {
	leadingEnd := leadingSystemEnd(messages)
	keepStart := computeKeepStartIndex(messages, leadingEnd, m.cfg.TailTokens)
	if keepStart <= leadingEnd {
		return messages, false
	}
	keepStart = avoidOrphanToolResults(messages, leadingEnd, keepStart)
	note := fmt.Sprintf("早期上下文因达到硬性上下文限制已被强制降载；摘要模型不可用或失败：%v。后续保留最近未压缩消息。", cause)
	if attachment := buildRecoveryAttachment(session.Recovery, tools); attachment != "" {
		note += "\n\n---\n\n" + attachment
	}
	out := rebuildMessages(messages[:leadingEnd], schema.SystemMessage(note), messages[keepStart:])
	session.mu.Lock()
	session.SoftFailures = 0
	session.CompactionCount++
	session.Usage = UsageAnchor{}
	session.LastCompactReason = "force_drop_oldest"
	session.mu.Unlock()
	return out, len(out) != len(messages)
}

func computeCompactThreshold(contextWindow, maxOutput int, hard bool) int {
	if contextWindow <= 0 {
		contextWindow = defaultContextWindow
	}
	if maxOutput <= 0 {
		maxOutput = defaultMaxOutputTokens
	}
	reserve := maxOutput
	if reserve > 20_000 {
		reserve = 20_000
	}
	margin := 13_000
	if hard {
		margin = 3_000
	}
	threshold := contextWindow - reserve - margin
	if threshold < 1 {
		return 1
	}
	return threshold
}

func computeUsedTokens(messages []*schema.Message, anchor UsageAnchor) int {
	if anchor.HasUsage && anchor.MessageCount <= len(messages) {
		return anchor.PromptTokens + estimateMessagesTokens(messages[anchor.MessageCount:])
	}
	return estimateMessagesTokens(messages)
}

func estimateMessagesTokens(messages []*schema.Message) int {
	total := 0
	for _, msg := range messages {
		total += estimateMessageTokens(msg)
	}
	return total
}

func estimateMessageTokens(msg *schema.Message) int {
	if msg == nil {
		return 0
	}
	b, _ := json.Marshal(map[string]any{
		"role":         msg.Role,
		"content":      msg.Content,
		"name":         msg.Name,
		"tool_calls":   msg.ToolCalls,
		"tool_call_id": msg.ToolCallID,
		"tool_name":    msg.ToolName,
	})
	return 8 + int(float64(len(b))/contextCharsPerToken)
}

func computeKeepStartIndex(messages []*schema.Message, leadingEnd, tailTokens int) int {
	if len(messages) <= leadingEnd+2 {
		return leadingEnd
	}
	if tailTokens <= 0 {
		tailTokens = defaultTailTokens
	}
	used := 0
	keepStart := len(messages)
	for i := len(messages) - 1; i >= leadingEnd; i-- {
		used += estimateMessageTokens(messages[i])
		keepStart = i
		if used >= tailTokens {
			break
		}
	}
	if keepStart <= leadingEnd {
		keepStart = leadingEnd + 1
	}
	return avoidOrphanToolResults(messages, leadingEnd, keepStart)
}

func avoidOrphanToolResults(messages []*schema.Message, leadingEnd, keepStart int) int {
	for keepStart > leadingEnd && keepStart < len(messages) && messages[keepStart] != nil && messages[keepStart].Role == schema.Tool {
		keepStart--
	}
	return keepStart
}

func leadingSystemEnd(messages []*schema.Message) int {
	i := 0
	for i < len(messages) && messages[i] != nil && messages[i].Role == schema.System {
		i++
	}
	return i
}

func rebuildMessages(leading []*schema.Message, summary *schema.Message, tail []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(leading)+1+len(tail))
	out = append(out, cloneSchemaMessages(leading)...)
	out = append(out, summary)
	out = append(out, cloneSchemaMessages(tail)...)
	return out
}

func sameMessages(a, b []*schema.Message) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] == nil || b[i] == nil {
			if a[i] != b[i] {
				return false
			}
			continue
		}
		if a[i].Role != b[i].Role ||
			a[i].Content != b[i].Content ||
			a[i].ToolCallID != b[i].ToolCallID ||
			a[i].ToolName != b[i].ToolName ||
			len(a[i].ToolCalls) != len(b[i].ToolCalls) {
			return false
		}
	}
	return true
}

func cloneSchemaMessages(in []*schema.Message) []*schema.Message {
	out := make([]*schema.Message, 0, len(in))
	for _, msg := range in {
		if msg == nil {
			out = append(out, nil)
			continue
		}
		cp := *msg
		out = append(out, &cp)
	}
	return out
}

func formatMessages(messages []*schema.Message) string {
	var sb strings.Builder
	for i, msg := range messages {
		if msg == nil {
			continue
		}
		name := string(msg.Role)
		if msg.ToolName != "" {
			name += "/" + msg.ToolName
		}
		fmt.Fprintf(&sb, "[%d] %s", i+1, name)
		if msg.ToolCallID != "" {
			fmt.Fprintf(&sb, " (%s)", msg.ToolCallID)
		}
		sb.WriteString(":\n")
		sb.WriteString(msg.Content)
		sb.WriteString("\n\n")
	}
	return sb.String()
}

func modelTools(mc *adk.ModelContext) []*schema.ToolInfo {
	if mc == nil {
		return nil
	}
	return mc.Tools
}

func (m *Middleware) markSpillReadback(session *SessionState, tCtx *adk.ToolContext, argsJSON string) {
	if session == nil || tCtx == nil || tCtx.Name != "ReadFile" || strings.TrimSpace(tCtx.CallID) == "" {
		return
	}
	args := map[string]any{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return
	}
	path := stringArg(args, "file_path")
	if path == "" {
		path = stringArg(args, "path")
	}
	if toolresult.IsPathInSpillDir(path, m.cfg.WorkDir, m.cfg.ToolResults) {
		session.Replacements.MarkOriginal(tCtx.CallID)
	}
}

func (m *Middleware) recordToolOutput(session *SessionState, tCtx *adk.ToolContext, argsJSON, output string) {
	if session == nil || tCtx == nil || output == "" {
		return
	}
	session.Recovery.recordTool(tCtx.Name, tCtx.CallID, output)
	if tCtx.Name != "ReadFile" {
		return
	}
	args := map[string]any{}
	if err := json.Unmarshal([]byte(argsJSON), &args); err != nil {
		return
	}
	path := stringArg(args, "file_path")
	if path == "" {
		path = stringArg(args, "path")
	}
	session.Recovery.recordFileRead(path, output)
}

func stringArg(args map[string]any, key string) string {
	if args == nil {
		return ""
	}
	if s, ok := args[key].(string); ok {
		return strings.TrimSpace(s)
	}
	return ""
}

func toolArgumentJSON(arg *schema.ToolArgument) string {
	if arg == nil {
		return "{}"
	}
	b, err := json.Marshal(arg)
	if err != nil {
		return "{}"
	}
	return string(b)
}

func toolResultText(result *schema.ToolResult) string {
	if result == nil {
		return ""
	}
	b, err := json.Marshal(result)
	if err != nil {
		return ""
	}
	return string(b)
}

type wrappedModel struct {
	base       model.BaseChatModel
	middleware *Middleware
	tools      []*schema.ToolInfo
}

func (w *wrappedModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	resp, err := w.base.Generate(ctx, input, opts...)
	if err == nil {
		return resp, nil
	}
	if !isContextTooLong(err) {
		return resp, err
	}
	session := w.middleware.sessionState(ctx)
	compacted, _, compactErr := w.middleware.manageMessages(ctx, input, session, w.tools, true)
	if compactErr != nil {
		return resp, err
	}
	return w.base.Generate(ctx, compacted, opts...)
}

func (w *wrappedModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	reader, err := w.base.Stream(ctx, input, opts...)
	if err == nil {
		return reader, nil
	}
	if !isContextTooLong(err) {
		return reader, err
	}
	session := w.middleware.sessionState(ctx)
	compacted, _, compactErr := w.middleware.manageMessages(ctx, input, session, w.tools, true)
	if compactErr != nil {
		return reader, err
	}
	return w.base.Stream(ctx, compacted, opts...)
}

func isContextTooLong(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) || errors.Is(err, io.EOF) {
		return false
	}
	msg := strings.ToLower(err.Error())
	return strings.Contains(msg, "context") && (strings.Contains(msg, "too long") || strings.Contains(msg, "length") || strings.Contains(msg, "maximum"))
}
