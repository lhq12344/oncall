package cache

import (
	"context"
	"fmt"

	"go.uber.org/zap"
)

// VectorCache 向量检索缓存
type VectorCache struct {
	manager *Manager
	logger  *zap.Logger
}

// NewVectorCache 创建向量缓存
func NewVectorCache(manager *Manager) *VectorCache {
	return &VectorCache{
		manager: manager,
		logger:  manager.logger,
	}
}

// VectorCacheKey 向量缓存键参数
type VectorCacheKey struct {
	AgentID      string  // Agent ID（确保隔离）
	SessionID    string  // 会话 ID（确保隔离）
	Query        string  // 查询文本
	TopK         int     // 返回结果数量
	ScoreThreshold float64 // 相似度阈值（可选）
	Collection   string  // 集合名称
}

// VectorCacheValue 向量缓存值
type VectorCacheValue struct {
	Results   []VectorResult // 检索结果
	Timestamp int64          // 缓存时间戳
	Query     string         // 原始查询
}

// VectorResult 向量检索结果
type VectorResult struct {
	ID      string                 // 文档 ID
	Score   float64                // 相似度分数
	Content string                 // 文档内容
	Metadata map[string]interface{} // 元数据
}

// Get 获取向量检索缓存
func (c *VectorCache) Get(ctx context.Context, key *VectorCacheKey) (*VectorCacheValue, error) {
	// 生成缓存键
	cacheKey := &CacheKey{
		Type:      CacheTypeVector,
		AgentID:   key.AgentID,
		SessionID: key.SessionID,
		Key:       c.generateKey(key),
	}

	var value VectorCacheValue
	if err := c.manager.Get(ctx, cacheKey, &value); err != nil {
		return nil, err
	}

	if c.logger != nil {
		c.logger.Debug("vector cache hit",
			zap.String("agent_id", key.AgentID),
			zap.String("session_id", key.SessionID),
			zap.String("query", key.Query),
			zap.Int("results", len(value.Results)))
	}

	return &value, nil
}

// Set 设置向量检索缓存
func (c *VectorCache) Set(ctx context.Context, key *VectorCacheKey, results []VectorResult) error {
	// 生成缓存键
	cacheKey := &CacheKey{
		Type:      CacheTypeVector,
		AgentID:   key.AgentID,
		SessionID: key.SessionID,
		Key:       c.generateKey(key),
	}

	value := &VectorCacheValue{
		Results:   results,
		Timestamp: ctx.Value("timestamp").(int64),
		Query:     key.Query,
	}

	if err := c.manager.Set(ctx, cacheKey, value, 0); err != nil {
		return err
	}

	if c.logger != nil {
		c.logger.Debug("vector cache set",
			zap.String("agent_id", key.AgentID),
			zap.String("session_id", key.SessionID),
			zap.String("query", key.Query),
			zap.Int("results", len(results)))
	}

	return nil
}

// generateKey 生成缓存键
func (c *VectorCache) generateKey(key *VectorCacheKey) string {
	keyData := struct {
		Query          string
		TopK           int
		ScoreThreshold float64
		Collection     string
	}{
		Query:          key.Query,
		TopK:           key.TopK,
		ScoreThreshold: key.ScoreThreshold,
		Collection:     key.Collection,
	}

	return GenerateKeyHash(keyData)
}

// InvalidateAgent 清除指定 Agent 的向量缓存
func (c *VectorCache) InvalidateAgent(ctx context.Context, agentID string) error {
	pattern := fmt.Sprintf("%s:%s:*", CacheTypeVector, agentID)
	return c.manager.DeleteByPattern(ctx, pattern)
}

// InvalidateSession 清除指定会话的向量缓存
func (c *VectorCache) InvalidateSession(ctx context.Context, sessionID string) error {
	pattern := fmt.Sprintf("%s:*:%s:*", CacheTypeVector, sessionID)
	return c.manager.DeleteByPattern(ctx, pattern)
}

// InvalidateCollection 清除指定集合的所有缓存
func (c *VectorCache) InvalidateCollection(ctx context.Context, collection string) error {
	// 注意：这需要扫描所有向量缓存，性能可能较差
	// 实际使用中可以考虑在键中包含 collection 信息
	pattern := fmt.Sprintf("%s:*", CacheTypeVector)
	return c.manager.DeleteByPattern(ctx, pattern)
}
