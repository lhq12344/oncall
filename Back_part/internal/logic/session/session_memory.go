package session

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"go_agent/internal/logic/ai/tokenizer"
	"go_agent/internal/logic/session/mem"

	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
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
// - 从会话 JSONL 文件恢复 Redis TTL 过期后的最近历史
// - 异步写入 MySQL 作为可选持久化备份（SESSION_MYSQL_DSN 配置时启用）
type SessionMemory struct {
	cfg          SessionMemoryConfig
	logger       *zap.Logger
	fileRecorder *FileSessionRecorder
	mysqlWriter  *MySQLSessionWriter
}

// NewSessionMemory 创建会话内存管理器。
//
// 输入：
// - cfg: 会话内存配置（可选，使用默认配置）
// - mysqlWriter: MySQL 持久化写入器（可选，nil 时禁用 MySQL 双写）
// - logger: 日志记录器（可选）
//
// 输出：
// - *SessionMemory: 初始化完成的会话内存管理器
func NewSessionMemory(cfg *SessionMemoryConfig, mysqlWriter *MySQLSessionWriter, logger *zap.Logger) *SessionMemory {
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
		cfg:          base,
		logger:       logger,
		fileRecorder: newSessionFileRecorderFromEnv(),
		mysqlWriter:  mysqlWriter,
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

	// Redis 为空（TTL 过期）且文件记录可用时，从 JSONL 末尾恢复最近历史。
	if len(messages) <= 1 && s.fileRecorder != nil {
		if recovered := s.recoverFromFile(ctx, sessionID, question); len(recovered) > 1 {
			return recovered, nil
		}
	}

	if len(messages) == 0 {
		return []*schema.Message{schema.UserMessage(question)}, nil
	}
	return messages, nil
}

// recoverFromFile 从 JSONL 文件末尾加载最近消息，回写 Redis，再重新构建带 token 预算的消息列表。
// 仅在 Redis 为空（TTL 过期）时调用，失败时静默降级（返回 nil）。
func (s *SessionMemory) recoverFromFile(ctx context.Context, sessionID, question string) []*schema.Message {
	historical, err := s.fileRecorder.LoadRecentMessages(ctx, sessionID, defaultSessionRecoveryLines)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("file_recovery: failed to load history",
				zap.String("session_id", sessionID),
				zap.Error(err))
		}
		return nil
	}
	if len(historical) == 0 {
		return nil
	}

	// 将 JSONL 中的可见消息按 user/assistant 配对回写 Redis。
	memory := mem.GetSimpleMemory(sessionID)
	for i := 0; i+1 < len(historical); {
		u := historical[i]
		a := historical[i+1]
		if u.Role != schema.User || a.Role != schema.Assistant {
			// 非标准配对（system/tool 消息），跳过整对。
			i++
			continue
		}
		if err := memory.SetMessages(ctx, u, a, nil, 0, 0); err != nil && s.logger != nil {
			s.logger.Warn("file_recovery: failed to replay turn into redis",
				zap.String("session_id", sessionID),
				zap.Int("turn_index", i),
				zap.Error(err))
		}
		i += 2
	}

	if s.logger != nil {
		s.logger.Info("file_recovery: replayed history into redis",
			zap.String("session_id", sessionID),
			zap.Int("historical_msgs", len(historical)))
	}

	// 重新从 Redis 读取，应用 token 预算裁剪。
	rebuilt, err := mem.GetMessagesForRequest(ctx, sessionID, schema.UserMessage(question), s.cfg.ReserveToolTokens)
	if err != nil {
		if s.logger != nil {
			s.logger.Warn("file_recovery: failed to rebuild messages after replay",
				zap.String("session_id", sessionID),
				zap.Error(err))
		}
		return nil
	}
	return rebuilt
}

// SaveTurn 保存对话轮次到会话内存。
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
	s.SaveTurnWithSource(ctx, sessionID, question, answer, nil, promptMessages, "chat") //nolint:errcheck
}

// SaveTurnWithSource 保存对话轮次。旁路文件记录启用时会追加本轮 prompt 快照和用户可见消息。
// 返回 error 仅反映 Redis SetMessages 失败；文件记录和 MySQL 异步写入失败仅记录日志，不影响返回值。
func (s *SessionMemory) SaveTurnWithSource(
	ctx context.Context,
	sessionID string,
	question string,
	answer string,
	intermediateMessages []*schema.Message,
	promptMessages []*schema.Message,
	source string,
) error {
	answer = strings.TrimSpace(answer)
	if answer == "" {
		return nil
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

	if err != nil {
		if s.logger != nil {
			s.logger.Warn("failed to save session memory",
				zap.String("session_id", sessionID),
				zap.Error(err))
		}
		return err
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

	if s.fileRecorder == nil {
		// 即使 fileRecorder 为 nil，也尝试 MySQL 异步写入
		s.mysqlWriter.SaveTurnAsync(sessionID, userMsg, assistantMsg, nil)
		return nil
	}

	recordCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := s.fileRecorder.AppendTurnWithPrompt(recordCtx, sessionID, strings.TrimSpace(source), promptMessages, userMsg, assistantMsg); err != nil && s.logger != nil {
		s.logger.Warn("failed to append session file record",
			zap.String("session_id", sessionID),
			zap.String("source", strings.TrimSpace(source)),
			zap.Error(err))
	}

	// MySQL 异步双写（不阻塞主流程）
	s.mysqlWriter.SaveTurnAsync(sessionID, userMsg, assistantMsg, nil)
	return nil
}

func newSessionFileRecorderFromEnv() *FileSessionRecorder {
	if !isTruthyEnv(os.Getenv("SESSION_FILE_RECORD_ENABLED")) {
		return nil
	}
	return NewFileSessionRecorder(os.Getenv("SESSION_FILE_RECORD_DIR"))
}

// NewMySQLWriterFromEnv 从 SESSION_MYSQL_DSN 环境变量构建 MySQLSessionWriter。
// 未设置或连接失败时返回 nil（MySQL 双写被禁用，不影响主流程）。
func NewMySQLWriterFromEnv(logger *zap.Logger) *MySQLSessionWriter {
	dsn := strings.TrimSpace(os.Getenv("SESSION_MYSQL_DSN"))
	if dsn == "" {
		return nil
	}
	db, err := openMySQLDB(dsn)
	if err != nil {
		if logger != nil {
			logger.Warn("session mysql: failed to open db, mysql write disabled",
				zap.String("dsn_prefix", safeDSNPrefix(dsn)),
				zap.Error(err))
		}
		return nil
	}
	w := NewMySQLSessionWriter(db, logger)
	if migrateErr := w.AutoMigrate(); migrateErr != nil && logger != nil {
		logger.Warn("session mysql: auto-migrate failed",
			zap.Error(migrateErr))
	}
	return w
}

// openMySQLDB opens a gorm DB with sensible connection-pool defaults.
func openMySQLDB(dsn string) (*gorm.DB, error) {
	db, err := gorm.Open(gormmysql.Open(dsn), &gorm.Config{})
	if err != nil {
		return nil, err
	}
	sqlDB, err := db.DB()
	if err != nil {
		return nil, err
	}
	sqlDB.SetMaxOpenConns(5)
	sqlDB.SetMaxIdleConns(2)
	sqlDB.SetConnMaxLifetime(30 * time.Minute)
	return db, nil
}

// safeDSNPrefix returns the first 30 chars of a DSN for safe logging (hides credentials).
func safeDSNPrefix(dsn string) string {
	if len(dsn) <= 30 {
		return dsn
	}
	return dsn[:30] + "..."
}

func isTruthyEnv(value string) bool {
	switch strings.ToLower(strings.TrimSpace(value)) {
	case "1", "true", "yes", "y", "on", "enabled":
		return true
	default:
		return false
	}
}
