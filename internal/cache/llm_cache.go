package cache

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// LLMCache LLM 响应缓存
type LLMCache struct {
	manager *Manager
	logger  *zap.Logger
}

// NewLLMCache 创建 LLM 缓存
func NewLLMCache(manager *Manager) *LLMCache {
	return &LLMCache{
		manager: manager,
		logger:  manager.logger,
	}
}

// LLMCacheKey LLM 缓存键参数
type LLMCacheKey struct {
	AgentID   string              // Agent ID（确保隔离）
	SessionID string              // 会话 ID（确保隔离）
	Messages  []*schema.Message   // 输入消息
	Model     string              // 模型名称
	Tools     []string            // 工具列表（可选）
	Extra     map[string]interface{} // 额外参数（可选）
}

// LLMCacheValue LLM 缓存值
type LLMCacheValue struct {
	Response  *schema.Message // LLM 响应
	Timestamp int64           // 缓存时间戳
	Model     string          // 使用的模型
}

// Get 获取 LLM 响应缓存
func (c *LLMCache) Get(ctx context.Context, key *LLMCacheKey) (*LLMCacheValue, error) {
	// 生成缓存键
	cacheKey := &CacheKey{
		Type:      CacheTypeLLM,
		AgentID:   key.AgentID,
		SessionID: key.SessionID,
		Key:       c.generateKey(key),
	}

	var value LLMCacheValue
	if err := c.manager.Get(ctx, cacheKey, &value); err != nil {
		return nil, err
	}

	if c.logger != nil {
		c.logger.Debug("LLM cache hit",
			zap.String("agent_id", key.AgentID),
			zap.String("session_id", key.SessionID),
			zap.String("model", key.Model))
	}

	return &value, nil
}

// Set 设置 LLM 响应缓存
func (c *LLMCache) Set(ctx context.Context, key *LLMCacheKey, response *schema.Message) error {
	// 生成缓存键
	cacheKey := &CacheKey{
		Type:      CacheTypeLLM,
		AgentID:   key.AgentID,
		SessionID: key.SessionID,
		Key:       c.generateKey(key),
	}

	value := &LLMCacheValue{
		Response:  response,
		Timestamp: ctx.Value("timestamp").(int64),
		Model:     key.Model,
	}

	if err := c.manager.Set(ctx, cacheKey, value, 0); err != nil {
		return err
	}

	if c.logger != nil {
		c.logger.Debug("LLM cache set",
			zap.String("agent_id", key.AgentID),
			zap.String("session_id", key.SessionID),
			zap.String("model", key.Model))
	}

	return nil
}

// generateKey 生成缓存键（基于输入消息和参数）
func (c *LLMCache) generateKey(key *LLMCacheKey) string {
	// 构建缓存键数据
	keyData := struct {
		Messages []*schema.Message
		Model    string
		Tools    []string
		Extra    map[string]interface{}
	}{
		Messages: c.normalizeMessages(key.Messages),
		Model:    key.Model,
		Tools:    key.Tools,
		Extra:    key.Extra,
	}

	return GenerateKeyHash(keyData)
}

// normalizeMessages 规范化消息（移除不影响响应的字段）
func (c *LLMCache) normalizeMessages(messages []*schema.Message) []*schema.Message {
	normalized := make([]*schema.Message, len(messages))
	for i, msg := range messages {
		if msg == nil {
			continue
		}

		// 只保留影响 LLM 响应的字段
		normalized[i] = &schema.Message{
			Role:    msg.Role,
			Content: msg.Content,
			// 不包含 ResponseMeta、Extra 等元数据
		}
	}
	return normalized
}

// InvalidateAgent 清除指定 Agent 的 LLM 缓存
func (c *LLMCache) InvalidateAgent(ctx context.Context, agentID string) error {
	pattern := fmt.Sprintf("%s:%s:*", CacheTypeLLM, agentID)
	return c.manager.DeleteByPattern(ctx, pattern)
}

// InvalidateSession 清除指定会话的 LLM 缓存
func (c *LLMCache) InvalidateSession(ctx context.Context, sessionID string) error {
	pattern := fmt.Sprintf("%s:*:%s:*", CacheTypeLLM, sessionID)
	return c.manager.DeleteByPattern(ctx, pattern)
}
