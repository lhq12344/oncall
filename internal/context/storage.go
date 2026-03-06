package context

import (
	"context"
	"time"
)

// Storage 存储层抽象接口
type Storage interface {
	// Session 相关
	SaveSession(ctx context.Context, sessionID string, data []byte, ttl time.Duration) error
	LoadSession(ctx context.Context, sessionID string) (*SessionContext, error)
	DeleteSession(ctx context.Context, sessionID string) error

	// Agent 相关
	SaveAgentContext(ctx context.Context, agentID string, data []byte, ttl time.Duration) error
	LoadAgentContext(ctx context.Context, agentID string) (*AgentContext, error)

	// Execution 相关
	SaveExecutionContext(ctx context.Context, executionID string, data []byte, ttl time.Duration) error
	LoadExecutionContext(ctx context.Context, executionID string) (*ExecutionContext, error)

	// 批量操作
	ListSessions(ctx context.Context, pattern string) ([]string, error)
	DeleteExpiredSessions(ctx context.Context, before time.Time) error
}
