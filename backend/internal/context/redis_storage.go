package context

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

// RedisStorage Redis 存储实现
type RedisStorage struct {
	client *redis.Client
	prefix string
}

// NewRedisStorage 创�� Redis 存储
func NewRedisStorage(client *redis.Client, prefix string) *RedisStorage {
	return &RedisStorage{
		client: client,
		prefix: prefix,
	}
}

// SaveSession 保存会话
func (r *RedisStorage) SaveSession(ctx context.Context, sessionID string, data []byte, ttl time.Duration) error {
	key := fmt.Sprintf("%s:session:%s", r.prefix, sessionID)
	return r.client.Set(ctx, key, data, ttl).Err()
}

// LoadSession 加载会话
func (r *RedisStorage) LoadSession(ctx context.Context, sessionID string) (*SessionContext, error) {
	key := fmt.Sprintf("%s:session:%s", r.prefix, sessionID)
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}

	var session SessionContext
	if err := json.Unmarshal(data, &session); err != nil {
		return nil, err
	}

	return &session, nil
}

// DeleteSession 删除会话
func (r *RedisStorage) DeleteSession(ctx context.Context, sessionID string) error {
	key := fmt.Sprintf("%s:session:%s", r.prefix, sessionID)
	return r.client.Del(ctx, key).Err()
}

// SaveAgentContext 保存 Agent 上下文
func (r *RedisStorage) SaveAgentContext(ctx context.Context, agentID string, data []byte, ttl time.Duration) error {
	key := fmt.Sprintf("%s:agent:%s", r.prefix, agentID)
	return r.client.Set(ctx, key, data, ttl).Err()
}

// LoadAgentContext 加载 Agent 上下文
func (r *RedisStorage) LoadAgentContext(ctx context.Context, agentID string) (*AgentContext, error) {
	key := fmt.Sprintf("%s:agent:%s", r.prefix, agentID)
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}

	var agentCtx AgentContext
	if err := json.Unmarshal(data, &agentCtx); err != nil {
		return nil, err
	}

	return &agentCtx, nil
}

// SaveExecutionContext 保存执行上下文
func (r *RedisStorage) SaveExecutionContext(ctx context.Context, executionID string, data []byte, ttl time.Duration) error {
	key := fmt.Sprintf("%s:execution:%s", r.prefix, executionID)
	return r.client.Set(ctx, key, data, ttl).Err()
}

// LoadExecutionContext 加载执行上下文
func (r *RedisStorage) LoadExecutionContext(ctx context.Context, executionID string) (*ExecutionContext, error) {
	key := fmt.Sprintf("%s:execution:%s", r.prefix, executionID)
	data, err := r.client.Get(ctx, key).Bytes()
	if err != nil {
		return nil, err
	}

	var execCtx ExecutionContext
	if err := json.Unmarshal(data, &execCtx); err != nil {
		return nil, err
	}

	return &execCtx, nil
}

// ListSessions 列出会话
func (r *RedisStorage) ListSessions(ctx context.Context, pattern string) ([]string, error) {
	key := fmt.Sprintf("%s:session:%s", r.prefix, pattern)
	return r.client.Keys(ctx, key).Result()
}

// DeleteExpiredSessions 删除过期会话
func (r *RedisStorage) DeleteExpiredSessions(ctx context.Context, before time.Time) error {
	// Redis 的 TTL 机制会自动删除过期数据
	// 这里可以实现额外的清理逻辑
	return nil
}
