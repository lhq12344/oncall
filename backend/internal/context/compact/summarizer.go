package compact

import "strings"

type Summarizer interface {
	Summarize([]Message) string
}

type Message struct {
	Role       string
	Content    string
	ToolCallID string
}

type ExtractiveSummarizer struct{}

func (ExtractiveSummarizer) Summarize(messages []Message) string {
	parts := make([]string, 0, len(messages))
	for _, msg := range messages {
		content := strings.TrimSpace(msg.Content)
		if content != "" {
			parts = append(parts, msg.Role+": "+content)
		}
	}
	return strings.Join(parts, "\n")
}
