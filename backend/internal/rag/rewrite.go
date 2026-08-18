package rag

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
)

type QueryRewriter interface {
	Rewrite(ctx context.Context, input RewriteInput) (RewriteResult, error)
}

type RewriteInput struct {
	Query          string   `json:"query"`
	SessionSummary string   `json:"session_summary,omitempty"`
	RecentTurns    []string `json:"recent_turns,omitempty"`
}

type RewriteResult struct {
	RewrittenQueries      []string       `json:"rewritten_queries"`
	Entities              map[string]any `json:"entities,omitempty"`
	Confidence            float64        `json:"confidence"`
	NeedsClarification    bool           `json:"needs_clarification"`
	ClarificationQuestion string         `json:"clarification_question,omitempty"`
}

type NoopRewriter struct{}

func (NoopRewriter) Rewrite(ctx context.Context, input RewriteInput) (RewriteResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return RewriteResult{}, err
	}
	query := strings.TrimSpace(input.Query)
	return RewriteResult{RewrittenQueries: []string{strings.TrimSpace(query)}, Confidence: 1}, nil
}

type rewriteContextKey struct{}

func WithRewriteContext(ctx context.Context, input RewriteInput) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	return context.WithValue(ctx, rewriteContextKey{}, input.normalized())
}

func RewriteInputFromContext(ctx context.Context, query string) RewriteInput {
	if ctx != nil {
		if input, ok := ctx.Value(rewriteContextKey{}).(RewriteInput); ok {
			input.Query = firstNonEmpty(query, input.Query)
			return input.normalized()
		}
	}
	return RewriteInput{Query: strings.TrimSpace(query)}.normalized()
}

func BuildRewriteInput(query string, messages []*schema.Message) RewriteInput {
	input := RewriteInput{Query: strings.TrimSpace(query)}
	if len(messages) == 0 {
		return input.normalized()
	}

	var summaries []string
	var turns []string
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}
		switch msg.Role {
		case schema.System:
			summaries = append(summaries, content)
		case schema.User, schema.Assistant:
			if content == input.Query {
				continue
			}
			turns = append(turns, fmt.Sprintf("%s: %s", msg.Role, clipRewriteText(content, 320)))
		}
	}
	if len(summaries) > 0 {
		input.SessionSummary = clipRewriteText(strings.Join(summaries, "\n"), 1200)
	}
	if len(turns) > 4 {
		turns = turns[len(turns)-4:]
	}
	input.RecentTurns = turns
	return input.normalized()
}

func (in RewriteInput) normalized() RewriteInput {
	in.Query = strings.TrimSpace(in.Query)
	in.SessionSummary = clipRewriteText(in.SessionSummary, 1200)
	if len(in.RecentTurns) > 4 {
		in.RecentTurns = in.RecentTurns[len(in.RecentTurns)-4:]
	}
	out := make([]string, 0, len(in.RecentTurns))
	for _, turn := range in.RecentTurns {
		turn = clipRewriteText(turn, 400)
		if turn != "" {
			out = append(out, turn)
		}
	}
	in.RecentTurns = out
	return in
}

type ChatModelRewriter struct {
	Model       model.BaseChatModel
	MaxRewrites int
}

func NewChatModelRewriter(m model.BaseChatModel) ChatModelRewriter {
	return ChatModelRewriter{Model: m, MaxRewrites: 2}
}

func (r ChatModelRewriter) Rewrite(ctx context.Context, input RewriteInput) (RewriteResult, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	input = input.normalized()
	if input.Query == "" {
		return RewriteResult{}, fmt.Errorf("query is required")
	}
	if r.Model == nil {
		return NoopRewriter{}.Rewrite(ctx, input)
	}
	maxRewrites := r.MaxRewrites
	if maxRewrites <= 0 || maxRewrites > 2 {
		maxRewrites = 2
	}
	payload, _ := json.Marshal(input)
	messages := []*schema.Message{
		schema.SystemMessage("You rewrite search queries for an OnCall DevOps/SRE RAG retriever. Return only JSON with keys rewritten_queries, entities, confidence, needs_clarification, clarification_question. Keep the original facts, do not invent pods/files/namespaces, and generate at most two rewritten_queries."),
		schema.UserMessage(fmt.Sprintf("Rewrite this retrieval request. Always preserve the original query separately; rewritten_queries should contain only useful alternatives, max %d. If required context is missing, set needs_clarification=true.\n%s", maxRewrites, string(payload))),
	}
	resp, err := r.Model.Generate(ctx, messages, model.WithMaxTokens(512))
	if err != nil {
		return RewriteResult{}, err
	}
	if resp == nil || strings.TrimSpace(resp.Content) == "" {
		return RewriteResult{}, fmt.Errorf("rewrite model returned empty content")
	}
	parsed := ParseRewriteResult(extractJSONObject(resp.Content), input.Query)
	parsed.RewrittenQueries = NormalizeQueryVariants(input.Query, parsed.RewrittenQueries, maxRewrites+1)
	return parsed, nil
}

func ParseRewriteResult(raw, original string) RewriteResult {
	var parsed RewriteResult
	if err := json.Unmarshal([]byte(strings.TrimSpace(raw)), &parsed); err != nil {
		return RewriteResult{RewrittenQueries: []string{strings.TrimSpace(original)}, Confidence: 0}
	}
	parsed.RewrittenQueries = NormalizeQueryVariants(original, parsed.RewrittenQueries, 3)
	if parsed.Confidence < 0 {
		parsed.Confidence = 0
	}
	if parsed.Confidence > 1 {
		parsed.Confidence = 1
	}
	return parsed
}

func extractJSONObject(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return raw
	}
	if strings.HasPrefix(raw, "```") {
		raw = strings.TrimPrefix(raw, "```json")
		raw = strings.TrimPrefix(raw, "```")
		raw = strings.TrimSuffix(raw, "```")
		raw = strings.TrimSpace(raw)
	}
	start := strings.Index(raw, "{")
	end := strings.LastIndex(raw, "}")
	if start >= 0 && end >= start {
		return raw[start : end+1]
	}
	return raw
}

func NormalizeQueryVariants(original string, rewrites []string, max int) []string {
	original = strings.TrimSpace(original)
	if max <= 0 {
		max = 3
	}
	out := make([]string, 0, max)
	seen := map[string]struct{}{}
	add := func(value string) {
		value = strings.TrimSpace(value)
		if value == "" {
			return
		}
		key := strings.ToLower(value)
		if _, ok := seen[key]; ok {
			return
		}
		seen[key] = struct{}{}
		out = append(out, value)
	}
	add(original)
	for _, rewrite := range rewrites {
		if len(out) >= max {
			break
		}
		add(rewrite)
	}
	return out
}

func clipRewriteText(value string, maxRunes int) string {
	value = strings.TrimSpace(value)
	if value == "" || maxRunes <= 0 {
		return value
	}
	runes := []rune(value)
	if len(runes) <= maxRunes {
		return value
	}
	return string(runes[:maxRunes]) + "..."
}
