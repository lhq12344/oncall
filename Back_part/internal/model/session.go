package model

import "time"

// SessionMessage 对话会话全量消息持久化记录。
// 用于在 Redis TTL 过期后恢复会话上下文。
type SessionMessage struct {
	ID         uint64    `gorm:"primaryKey;autoIncrement"`
	SessionID  string    `gorm:"index;type:varchar(128);not null"`
	Role       string    `gorm:"type:varchar(32);not null"` // user/assistant/tool/system
	Content    string    `gorm:"type:text"`
	ToolCalls  string    `gorm:"type:text"`        // JSON 序列化的 ToolCalls
	ToolCallID string    `gorm:"type:varchar(128)"`
	TurnSeq    int64     `gorm:"default:0"` // 同一保存批次共用 UnixMilli 作为序列标识
	CreatedAt  time.Time `gorm:"autoCreateTime"`
}

func (SessionMessage) TableName() string { return "session_messages" }
