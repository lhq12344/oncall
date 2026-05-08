package session

import (
	"context"
	"encoding/json"
	"time"

	"go_agent/internal/model"

	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
	"gorm.io/gorm"
)

// MySQLSessionWriter 将对话轮次异步写入 MySQL，作为 Redis 的持久化备份。
// 写入失败不影响主流程，仅记录警告日志。
type MySQLSessionWriter struct {
	db     *gorm.DB
	logger *zap.Logger
}

// NewMySQLSessionWriter 创建 MySQL 会话写入器。
// db 为 nil 时返回 nil（调用方需判空）。
func NewMySQLSessionWriter(db *gorm.DB, logger *zap.Logger) *MySQLSessionWriter {
	if db == nil {
		return nil
	}
	return &MySQLSessionWriter{db: db, logger: logger}
}

// AutoMigrate 自动建表（幂等）。
func (w *MySQLSessionWriter) AutoMigrate() error {
	return w.db.AutoMigrate(&model.SessionMessage{})
}

// SaveTurnAsync 异步将一轮对话（user + assistant）写入 MySQL。
// 使用独立 goroutine，不阻塞调用方。
func (w *MySQLSessionWriter) SaveTurnAsync(
	sessionID string,
	userMsg *schema.Message,
	assistantMsg *schema.Message,
	promptMessages []*schema.Message,
) {
	if w == nil {
		return
	}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		if err := w.saveTurn(ctx, sessionID, userMsg, assistantMsg, promptMessages); err != nil && w.logger != nil {
			w.logger.Warn("mysql_session_writer: failed to save turn",
				zap.String("session_id", sessionID),
				zap.Error(err))
		}
	}()
}

func (w *MySQLSessionWriter) saveTurn(
	ctx context.Context,
	sessionID string,
	userMsg *schema.Message,
	assistantMsg *schema.Message,
	promptMessages []*schema.Message,
) error {
	turnSeq := time.Now().UnixMilli()
	records := make([]model.SessionMessage, 0, 2+len(promptMessages))

	// 保存 prompt 上下文（system/tool 消息）
	for _, msg := range promptMessages {
		if msg == nil {
			continue
		}
		rec := model.SessionMessage{
			SessionID: sessionID,
			Role:      string(msg.Role),
			Content:   msg.Content,
			TurnSeq:   turnSeq,
		}
		if len(msg.ToolCalls) > 0 {
			if b, err := json.Marshal(msg.ToolCalls); err == nil {
				rec.ToolCalls = string(b)
			}
		}
		if msg.ToolCallID != "" {
			rec.ToolCallID = msg.ToolCallID
		}
		records = append(records, rec)
	}

	// 保存 user 消息
	if userMsg != nil {
		records = append(records, model.SessionMessage{
			SessionID: sessionID,
			Role:      string(userMsg.Role),
			Content:   userMsg.Content,
			TurnSeq:   turnSeq,
		})
	}

	// 保存 assistant 消息
	if assistantMsg != nil {
		rec := model.SessionMessage{
			SessionID: sessionID,
			Role:      string(assistantMsg.Role),
			Content:   assistantMsg.Content,
			TurnSeq:   turnSeq,
		}
		if len(assistantMsg.ToolCalls) > 0 {
			if b, err := json.Marshal(assistantMsg.ToolCalls); err == nil {
				rec.ToolCalls = string(b)
			}
		}
		records = append(records, rec)
	}

	if len(records) == 0 {
		return nil
	}

	return w.db.WithContext(ctx).Create(&records).Error
}

// LoadTurnsBySession 从 MySQL 加载指定会话的所有消息，按 TurnSeq + ID 排序。
// 用于 Redis TTL 过期后的会话恢复。
func (w *MySQLSessionWriter) LoadTurnsBySession(ctx context.Context, sessionID string) ([]*schema.Message, error) {
	if w == nil {
		return nil, nil
	}
	var records []model.SessionMessage
	if err := w.db.WithContext(ctx).
		Where("session_id = ?", sessionID).
		Order("turn_seq ASC, id ASC").
		Find(&records).Error; err != nil {
		return nil, err
	}

	msgs := make([]*schema.Message, 0, len(records))
	for _, rec := range records {
		msg := &schema.Message{
			Role:    schema.RoleType(rec.Role),
			Content: rec.Content,
		}
		if rec.ToolCallID != "" {
			msg.ToolCallID = rec.ToolCallID
		}
		if rec.ToolCalls != "" {
			var tcs []schema.ToolCall
			if err := json.Unmarshal([]byte(rec.ToolCalls), &tcs); err == nil {
				msg.ToolCalls = tcs
			}
		}
		msgs = append(msgs, msg)
	}
	return msgs, nil
}
