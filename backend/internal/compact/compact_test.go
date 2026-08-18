package compact

import (
	"context"
	"errors"
	"strings"
	"testing"
	"unicode/utf8"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type fakeSummaryModel struct {
	summary    string
	err        error
	calls      int
	lastPrompt string
}

func (m *fakeSummaryModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	m.calls++
	if len(input) > 0 {
		m.lastPrompt = input[len(input)-1].Content
	}
	if m.err != nil {
		return nil, m.err
	}
	return schema.AssistantMessage(m.summary, nil), nil
}

func (m *fakeSummaryModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	resp, err := m.Generate(ctx, input, opts...)
	if err != nil {
		return nil, err
	}
	return schema.StreamReaderFromArray([]*schema.Message{resp}), nil
}

func TestBeforeModelRewriteStateCompactsAndKeepsRecentTail(t *testing.T) {
	t.Parallel()

	model := &fakeSummaryModel{summary: "SUMMARY: earlier troubleshooting context"}
	mw := NewMiddleware(Config{
		Model:           model,
		Store:           NewStore(),
		ContextWindow:   14_000,
		MaxOutputTokens: 100,
		TailTokens:      60,
		WorkDir:         t.TempDir(),
	})
	msgs := []*schema.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("OLD-PREFIX " + strings.Repeat("a", 8_000)),
		schema.AssistantMessage("OLD-REPLY "+strings.Repeat("b", 8_000), nil),
		schema.UserMessage("RECENT-A unique-marker-A"),
		schema.AssistantMessage("RECENT-B unique-marker-B", nil),
	}
	state := &adk.ChatModelAgentState{Messages: msgs}

	_, outState, err := mw.BeforeModelRewriteState(context.Background(), state, &adk.ModelContext{
		Tools: []*schema.ToolInfo{{Name: "ReadFile", Desc: "Read files."}},
	})
	if err != nil {
		t.Fatalf("BeforeModelRewriteState returned error: %v", err)
	}
	if model.calls != 1 {
		t.Fatalf("expected summarizer to be called once, got %d", model.calls)
	}
	if containsContent(outState.Messages, "OLD-PREFIX") {
		t.Fatalf("expected old prefix to be summarized away")
	}
	if !containsContent(outState.Messages, "SUMMARY: earlier troubleshooting context") {
		t.Fatalf("expected summary in compacted messages")
	}
	if !containsContent(outState.Messages, "RECENT-A unique-marker-A") || !containsContent(outState.Messages, "RECENT-B unique-marker-B") {
		t.Fatalf("expected recent tail to be preserved")
	}
	if strings.Contains(model.lastPrompt, "RECENT-A unique-marker-A") {
		t.Fatalf("summary prompt included kept tail")
	}
}

func TestBeforeModelRewriteStateNoopsUnderThreshold(t *testing.T) {
	t.Parallel()

	model := &fakeSummaryModel{summary: "unused"}
	mw := NewMiddleware(Config{
		Model:           model,
		Store:           NewStore(),
		ContextWindow:   128_000,
		MaxOutputTokens: 1_000,
		WorkDir:         t.TempDir(),
	})
	msgs := []*schema.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("short"),
	}
	state := &adk.ChatModelAgentState{Messages: msgs}

	_, outState, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState returned error: %v", err)
	}
	if model.calls != 0 {
		t.Fatalf("expected no summarizer call, got %d", model.calls)
	}
	if len(outState.Messages) != len(msgs) {
		t.Fatalf("expected no-op message length, got %d", len(outState.Messages))
	}
}

func TestHardThresholdFallsBackWhenSummaryFails(t *testing.T) {
	t.Parallel()

	model := &fakeSummaryModel{err: errors.New("summary failed")}
	mw := NewMiddleware(Config{
		Model:           model,
		Store:           NewStore(),
		ContextWindow:   4_000,
		MaxOutputTokens: 100,
		TailTokens:      60,
		WorkDir:         t.TempDir(),
	})
	msgs := []*schema.Message{
		schema.SystemMessage("system"),
		schema.UserMessage("OLD-PREFIX " + strings.Repeat("a", 10_000)),
		schema.AssistantMessage("OLD-REPLY "+strings.Repeat("b", 10_000), nil),
		schema.UserMessage("RECENT unique-marker"),
	}
	state := &adk.ChatModelAgentState{Messages: msgs}

	_, outState, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState returned error: %v", err)
	}
	if containsContent(outState.Messages, "OLD-PREFIX") {
		t.Fatalf("expected fallback to drop old context")
	}
	if !containsContent(outState.Messages, "强制降载") {
		t.Fatalf("expected force-drop note")
	}
	if !containsContent(outState.Messages, "RECENT unique-marker") {
		t.Fatalf("expected recent message to remain")
	}
}

func TestToolResultBudgetRunsBeforeCompaction(t *testing.T) {
	t.Parallel()

	mw := NewMiddleware(Config{
		Model:           &fakeSummaryModel{summary: "unused"},
		Store:           NewStore(),
		ContextWindow:   128_000,
		MaxOutputTokens: 1_000,
		WorkDir:         t.TempDir(),
	})
	msgs := []*schema.Message{
		schema.ToolMessage(strings.Repeat("x", 60_000), "call-1", schema.WithToolName("Grep")),
	}
	state := &adk.ChatModelAgentState{Messages: msgs}

	_, outState, err := mw.BeforeModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("BeforeModelRewriteState returned error: %v", err)
	}
	if !strings.Contains(outState.Messages[0].Content, "<persisted-tool-result>") {
		t.Fatalf("expected tool result to be replaced by persisted preview")
	}
}

func TestAfterModelRewriteStateRecordsUsageAnchor(t *testing.T) {
	t.Parallel()

	store := NewStore()
	mw := NewMiddleware(Config{Store: store})
	state := &adk.ChatModelAgentState{Messages: []*schema.Message{
		schema.AssistantMessage("ok", nil),
	}}
	state.Messages[0].ResponseMeta = &schema.ResponseMeta{Usage: &schema.TokenUsage{PromptTokens: 1234}}

	_, _, err := mw.AfterModelRewriteState(context.Background(), state, nil)
	if err != nil {
		t.Fatalf("AfterModelRewriteState returned error: %v", err)
	}
	session := store.forSession(defaultSessionIDFallback)
	if !session.Usage.HasUsage || session.Usage.PromptTokens != 1234 || session.Usage.MessageCount != 1 {
		t.Fatalf("usage anchor not recorded: %+v", session.Usage)
	}
}

func TestRecoveryStoresBoundedUTF8SafeToolOutput(t *testing.T) {
	t.Parallel()

	state := newRecoveryState()
	large := strings.Repeat("故障日志", 3_000)

	state.recordTool("Grep", "call-1", large)
	records := state.snapshotTools(1)

	if len(records) != 1 {
		t.Fatalf("expected one recovery tool record, got %d", len(records))
	}
	if len([]byte(records[0].Output)) >= len([]byte(large)) {
		t.Fatalf("expected stored recovery output to be bounded")
	}
	if !strings.Contains(records[0].Output, "content truncated") {
		t.Fatalf("expected truncation marker")
	}
	if !utf8.ValidString(records[0].Output) {
		t.Fatalf("expected UTF-8 safe truncated output")
	}
}

func TestTruncateByTokensKeepsUTF8Boundary(t *testing.T) {
	t.Parallel()

	got := truncateByTokens(strings.Repeat("上下文", 1_000), 1)

	if !utf8.ValidString(got) {
		t.Fatalf("truncateByTokens split a UTF-8 rune")
	}
	if !strings.Contains(got, "content truncated") {
		t.Fatalf("expected truncation marker")
	}
}

func containsContent(messages []*schema.Message, needle string) bool {
	for _, msg := range messages {
		if msg != nil && strings.Contains(msg.Content, needle) {
			return true
		}
	}
	return false
}
