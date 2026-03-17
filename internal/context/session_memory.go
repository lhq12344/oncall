package context

import (
	"context"
	"errors"
	"strings"
	"time"

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
		ReserveToolTokens:   20000,
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
	return messages, nil
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
	saveCtx := ctx
	if saveCtx == nil || saveCtx.Err() != nil {
		detachedCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		saveCtx = detachedCtx
	}

	promptTokens := len(question) / 4
	if precisePromptTokens, err := tokenizer.CountMessagesTokens(saveCtx, promptMessages, false); err == nil && precisePromptTokens > 0 {
		promptTokens = precisePromptTokens
	}

	completionTokens := len(answer) / 4
	if preciseCompletionTokens, err := tokenizer.CountMessageTokens(saveCtx, assistantMsg, false); err == nil && preciseCompletionTokens > 0 {
		completionTokens = preciseCompletionTokens
	}

	err := memory.SetMessages(saveCtx, userMsg, assistantMsg, promptMessages, promptTokens, completionTokens)
	if err != nil && (errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)) {
		detachedCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		err = memory.SetMessages(detachedCtx, userMsg, assistantMsg, promptMessages, promptTokens, completionTokens)
	}

	if err != nil && s.logger != nil {
		s.logger.Warn("failed to save session memory",
			zap.String("session_id", sessionID),
			zap.Error(err))
		return
	}

	compactErr := memory.CompactHistory(saveCtx, s.cfg.MaxRecentTurns, s.cfg.SummarizeAfterTurns, s.cfg.SummaryMaxRunes)
	if compactErr != nil && (errors.Is(compactErr, context.Canceled) || errors.Is(compactErr, context.DeadlineExceeded)) {
		detachedCtx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		defer cancel()
		compactErr = memory.CompactHistory(detachedCtx, s.cfg.MaxRecentTurns, s.cfg.SummarizeAfterTurns, s.cfg.SummaryMaxRunes)
	}
	if compactErr != nil && s.logger != nil {
		s.logger.Warn("failed to compact session memory",
			zap.String("session_id", sessionID),
			zap.Error(compactErr))
	}
}
