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

// SessionMemoryConfig 会话内存配置结构。
//
// 字段说明：
// - ReserveToolTokens: 为工具调用预留的 token 数量
// - MaxRecentTurns: 保留的最近对话轮次数量
// - SummarizeAfterTurns: 超过此轮次后开始总结
// - SummaryMaxRunes: 总结内容的最大字符数
type SessionMemoryConfig struct {
	ReserveToolTokens   int
	MaxRecentTurns      int
	SummarizeAfterTurns int
	SummaryMaxRunes     int
}

// DefaultSessionMemoryConfig 返回默认的会话内存配置。
//
// 默认值：
// - ReserveToolTokens: 20000
// - MaxRecentTurns: 20
// - SummarizeAfterTurns: 40
// - SummaryMaxRunes: 1200
func DefaultSessionMemoryConfig() SessionMemoryConfig {
	return SessionMemoryConfig{
		ReserveToolTokens:   20000,
		MaxRecentTurns:      20,
		SummarizeAfterTurns: 40,
		SummaryMaxRunes:     1200,
	}
}

// SessionMemory 会话内存管理器，负责构建和保存对话历史。
//
// 功能：
// - 根据 token 预算构建消息历史
// - 保存对话轮次到 Redis
// - 控制历史长度和总结策略
type SessionMemory struct {
	cfg    SessionMemoryConfig
	logger *zap.Logger
}

// NewSessionMemory 创建会话内存管理器。
//
// 输入：
// - cfg: 会话内存配置（可选，使用默认配置）
// - logger: 日志记录器（可选）
//
// 输出：
// - *SessionMemory: 初始化完成的会话内存管理器
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

// BuildMessages 根据会话 ID 和当前问题构建消息历史。
//
// 功能：
// 1. 从 Redis 加载历史对话消息
// 2. 根据 token 预算裁剪历史（保留最近的对话）
// 3. 如果加载失败或历史为空，返回当前问题作为唯一消息
//
// 调用位置：
// - chat_v1.go:105 行，处理聊天流式请求时调用
//
// 输入：
// - ctx: 上下文
// - sessionID: 会话 ID
// - question: 当前问题
//
// 输出：
// - []*schema.Message: 消息历史（包含历史对话 + 当前问题）
// - error: 加载历史过程中的错误
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

// SaveTurn 保存对话轮次到会话内存。
//
// 功能：
// 1. 验证回答是否为空（空回答不保存）
// 2. 创建用户消息和助手消息
// 3. 将消息保存到 Redis（通过 mem utility）
//
// 调用位置：
// - chat_v1.go:191 行，聊天流式请求完成后调用
// - chat_v1.go:325 行，中断恢复请求完成后调用
//
// 输入：
// - ctx: 上下文
// - sessionID: 会话 ID
// - question: 用户问题
// - answer: 助手回答
// - promptMessages: 提示消息（可选，用于重建上下文）
//
// 输出：无（异步保存到 Redis）
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
