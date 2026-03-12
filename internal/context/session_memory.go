package context

import (
	"context"
	"strings"

	"go_agent/utility/mem"
	"go_agent/utility/tokenizer"

	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type SessionMemoryConfig struct {
	ReserveToolTokens   int
	MaxRecentTurns      int
	SummarizeAfterTurns int
	SummaryMaxRunes     int
}

func DefaultSessionMemoryConfig() SessionMemoryConfig {
	return SessionMemoryConfig{
		ReserveToolTokens:   5,
		MaxRecentTurns:      20,
		SummarizeAfterTurns: 40,
		SummaryMaxRunes:     1200,
	}
}

type SessionMemory struct {
	cfg    SessionMemoryConfig
	logger *zap.Logger
}

func NewSessionMemory(cfg *SessionMemoryConfig, logger *zap.Logger) *SessionMemory {
	base := DefaultSessionMemoryConfig()
	if cfg != nil {
		if cfg.ReserveToolTokens > 0 {
			base.ReserveToolTokens = cfg.ReserveToolTokens
		}
		if cfg.MaxRecentTurns > 0 {
			base.MaxRecentTurns = cfg.MaxRecentTurns
		}
		if cfg.SummarizeAfterTurns > 0 {
			base.SummarizeAfterTurns = cfg.SummarizeAfterTurns
		}
		if cfg.SummaryMaxRunes > 0 {
			base.SummaryMaxRunes = cfg.SummaryMaxRunes
		}
	}
	return &SessionMemory{
		cfg:    base,
		logger: logger,
	}
}

func (s *SessionMemory) BuildMessages(ctx context.Context, sessionID, question string) ([]*schema.Message, error) {
	question = strings.TrimSpace(question)
	if question == "" {
		return nil, nil
	}

	messages, err := mem.GetMessagesForRequest(ctx, sessionID, schema.UserMessage(question), s.cfg.ReserveToolTokens)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to load memory from redis, fallback to current question only",
				zap.String("session_id", sessionID),
				zap.Error(err))
		}
		return []*schema.Message{schema.UserMessage(question)}, nil
	}
	if len(messages) == 0 {
		return []*schema.Message{schema.UserMessage(question)}, nil
	}
	return s.compactMessages(messages), nil
}

func (s *SessionMemory) SaveTurn(
	ctx context.Context,
	sessionID string,
	question string,
	answer string,
	promptMessages []*schema.Message,
) {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return
	}

	memory := mem.GetSimpleMemory(sessionID)
	userMsg := schema.UserMessage(question)
	assistantMsg := schema.AssistantMessage(answer, nil)

	promptTokens := len(question) / 4
	if precisePromptTokens, err := tokenizer.CountMessagesTokens(ctx, promptMessages, false); err == nil && precisePromptTokens > 0 {
		promptTokens = precisePromptTokens
	}

	completionTokens := len(answer) / 4
	if preciseCompletionTokens, err := tokenizer.CountMessageTokens(ctx, assistantMsg, false); err == nil && preciseCompletionTokens > 0 {
		completionTokens = preciseCompletionTokens
	}

	if err := memory.SetMessages(ctx, userMsg, assistantMsg, promptMessages, promptTokens, completionTokens); err != nil && s.logger != nil {
		s.logger.Warn("failed to save session memory",
			zap.String("session_id", sessionID),
			zap.Error(err))
	}
}

func (s *SessionMemory) compactMessages(messages []*schema.Message) []*schema.Message {
	if len(messages) == 0 {
		return messages
	}

	systemMessages := make([]*schema.Message, 0, 2)
	chatMessages := make([]*schema.Message, 0, len(messages))
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		switch msg.Role {
		case schema.System:
			systemMessages = append(systemMessages, msg)
		case schema.User, schema.Assistant:
			chatMessages = append(chatMessages, msg)
		}
	}

	turns := splitTurns(chatMessages)
	if len(turns) == 0 {
		return append(systemMessages, chatMessages...)
	}

	summarizeAfterTurns := s.cfg.SummarizeAfterTurns
	if summarizeAfterTurns <= 0 {
		summarizeAfterTurns = 40
	}
	maxRecentTurns := s.cfg.MaxRecentTurns
	if maxRecentTurns <= 0 {
		maxRecentTurns = 20
	}

	splitPoint := 0
	if len(turns) > maxRecentTurns {
		splitPoint = len(turns) - maxRecentTurns
	}
	if len(turns) > summarizeAfterTurns && splitPoint == 0 {
		splitPoint = len(turns) - summarizeAfterTurns/2
		if splitPoint < 1 {
			splitPoint = 1
		}
	}

	out := make([]*schema.Message, 0, len(systemMessages)+len(chatMessages)+1)
	out = append(out, systemMessages...)

	if splitPoint > 0 {
		summary := summarizeTurns(turns[:splitPoint], s.cfg.SummaryMaxRunes)
		if summary != "" {
			out = append(out, schema.SystemMessage("历史会话摘要：\n"+summary))
		}
		turns = turns[splitPoint:]
	}

	for _, turn := range turns {
		out = append(out, turn...)
	}
	return out
}

func splitTurns(messages []*schema.Message) [][]*schema.Message {
	turns := make([][]*schema.Message, 0, len(messages)/2+1)
	for _, msg := range messages {
		if msg == nil {
			continue
		}
		if msg.Role == schema.User || len(turns) == 0 {
			turns = append(turns, []*schema.Message{msg})
			continue
		}
		turns[len(turns)-1] = append(turns[len(turns)-1], msg)
	}
	return turns
}

func summarizeTurns(turns [][]*schema.Message, maxRunes int) string {
	if len(turns) == 0 {
		return ""
	}
	if maxRunes <= 0 {
		maxRunes = 1200
	}

	var builder strings.Builder
	for _, turn := range turns {
		for _, msg := range turn {
			if msg == nil {
				continue
			}
			content := strings.TrimSpace(msg.Content)
			if content == "" {
				continue
			}
			if msg.Role == schema.User {
				builder.WriteString("- 用户: ")
				builder.WriteString(content)
				builder.WriteString("\n")
				continue
			}
			if msg.Role == schema.Assistant {
				builder.WriteString("- 助手: ")
				builder.WriteString(content)
				builder.WriteString("\n")
			}
		}
		if len([]rune(builder.String())) >= maxRunes {
			break
		}
	}

	summary := strings.TrimSpace(builder.String())
	if summary == "" {
		return ""
	}
	runes := []rune(summary)
	if len(runes) > maxRunes {
		return string(runes[:maxRunes]) + "..."
	}
	return summary
}
