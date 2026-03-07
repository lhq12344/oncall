package cache

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// MonitoringCache 监控数据缓存
type MonitoringCache struct {
	manager *Manager
	logger  *zap.Logger
}

// NewMonitoringCache 创建监控缓存
func NewMonitoringCache(manager *Manager) *MonitoringCache {
	return &MonitoringCache{
		manager: manager,
		logger:  manager.logger,
	}
}

// MonitoringCacheKey 监控缓存键参数
type MonitoringCacheKey struct {
	AgentID    string                 // Agent ID（确保隔离）
	SessionID  string                 // 会话 ID（确保隔离）
	Source     string                 // 数据源（k8s/prometheus/elasticsearch）
	Query      string                 // 查询语句
	TimeRange  string                 // 时间范围
	Parameters map[string]interface{} // 额外参数
}

// MonitoringCacheValue 监控缓存值
type MonitoringCacheValue struct {
	Data      interface{} // 监控数据
	Timestamp int64       // 缓存时间戳
	Source    string      // 数据源
	Query     string      // 原始查询
}

// Get 获取监控数据缓存
func (c *MonitoringCache) Get(ctx context.Context, key *MonitoringCacheKey) (*MonitoringCacheValue, error) {
	// 生成缓存键
	cacheKey := &CacheKey{
		Type:      CacheTypeMonitoring,
		AgentID:   key.AgentID,
		SessionID: key.SessionID,
		Key:       c.generateKey(key),
	}

	var value MonitoringCacheValue
	if err := c.manager.Get(ctx, cacheKey, &value); err != nil {
		return nil, err
	}

	if c.logger != nil {
		c.logger.Debug("monitoring cache hit",
			zap.String("agent_id", key.AgentID),
			zap.String("session_id", key.SessionID),
			zap.String("source", key.Source),
			zap.String("query", key.Query))
	}

	return &value, nil
}

// Set 设置监控数据缓存
func (c *MonitoringCache) Set(ctx context.Context, key *MonitoringCacheKey, data interface{}) error {
	// 生成缓存键
	cacheKey := &CacheKey{
		Type:      CacheTypeMonitoring,
		AgentID:   key.AgentID,
		SessionID: key.SessionID,
		Key:       c.generateKey(key),
	}

	value := &MonitoringCacheValue{
		Data:      data,
		Timestamp: time.Now().Unix(),
		Source:    key.Source,
		Query:     key.Query,
	}

	if err := c.manager.Set(ctx, cacheKey, value, 0); err != nil {
		return err
	}

	if c.logger != nil {
		c.logger.Debug("monitoring cache set",
			zap.String("agent_id", key.AgentID),
			zap.String("session_id", key.SessionID),
			zap.String("source", key.Source),
			zap.String("query", key.Query))
	}

	return nil
}

// SetWithTTL 设置监控数据缓存（自定义 TTL）
func (c *MonitoringCache) SetWithTTL(ctx context.Context, key *MonitoringCacheKey, data interface{}, ttl time.Duration) error {
	// 生成缓存键
	cacheKey := &CacheKey{
		Type:      CacheTypeMonitoring,
		AgentID:   key.AgentID,
		SessionID: key.SessionID,
		Key:       c.generateKey(key),
	}

	value := &MonitoringCacheValue{
		Data:      data,
		Timestamp: time.Now().Unix(),
		Source:    key.Source,
		Query:     key.Query,
	}

	if err := c.manager.Set(ctx, cacheKey, value, ttl); err != nil {
		return err
	}

	if c.logger != nil {
		c.logger.Debug("monitoring cache set with custom TTL",
			zap.String("agent_id", key.AgentID),
			zap.String("session_id", key.SessionID),
			zap.String("source", key.Source),
			zap.String("query", key.Query),
			zap.Duration("ttl", ttl))
	}

	return nil
}

// generateKey 生成缓存键
func (c *MonitoringCache) generateKey(key *MonitoringCacheKey) string {
	keyData := struct {
		Source     string
		Query      string
		TimeRange  string
		Parameters map[string]interface{}
	}{
		Source:     key.Source,
		Query:      key.Query,
		TimeRange:  key.TimeRange,
		Parameters: key.Parameters,
	}

	return GenerateKeyHash(keyData)
}

// InvalidateAgent 清除指定 Agent 的监控缓存
func (c *MonitoringCache) InvalidateAgent(ctx context.Context, agentID string) error {
	pattern := fmt.Sprintf("%s:%s:*", CacheTypeMonitoring, agentID)
	return c.manager.DeleteByPattern(ctx, pattern)
}

// InvalidateSession 清除指定会话的监控缓存
func (c *MonitoringCache) InvalidateSession(ctx context.Context, sessionID string) error {
	pattern := fmt.Sprintf("%s:*:%s:*", CacheTypeMonitoring, sessionID)
	return c.manager.DeleteByPattern(ctx, pattern)
}

// InvalidateSource 清除指定数据源的所有缓存
func (c *MonitoringCache) InvalidateSource(ctx context.Context, source string) error {
	// 注意：这需要扫描所有监控缓存
	pattern := fmt.Sprintf("%s:*", CacheTypeMonitoring)
	return c.manager.DeleteByPattern(ctx, pattern)
}

// K8sCache K8s 监控缓存（便捷方法）
type K8sCache struct {
	*MonitoringCache
}

// NewK8sCache 创建 K8s 缓存
func NewK8sCache(manager *Manager) *K8sCache {
	return &K8sCache{
		MonitoringCache: NewMonitoringCache(manager),
	}
}

// GetPods 获取 Pod 列表缓存
func (c *K8sCache) GetPods(ctx context.Context, agentID, sessionID, namespace string) (interface{}, error) {
	key := &MonitoringCacheKey{
		AgentID:   agentID,
		SessionID: sessionID,
		Source:    "k8s",
		Query:     "list_pods",
		Parameters: map[string]interface{}{
			"namespace": namespace,
		},
	}
	value, err := c.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return value.Data, nil
}

// SetPods 设置 Pod 列表缓存
func (c *K8sCache) SetPods(ctx context.Context, agentID, sessionID, namespace string, pods interface{}) error {
	key := &MonitoringCacheKey{
		AgentID:   agentID,
		SessionID: sessionID,
		Source:    "k8s",
		Query:     "list_pods",
		Parameters: map[string]interface{}{
			"namespace": namespace,
		},
	}
	return c.Set(ctx, key, pods)
}

// PrometheusCache Prometheus 监控缓存（便捷方法）
type PrometheusCache struct {
	*MonitoringCache
}

// NewPrometheusCache 创建 Prometheus 缓存
func NewPrometheusCache(manager *Manager) *PrometheusCache {
	return &PrometheusCache{
		MonitoringCache: NewMonitoringCache(manager),
	}
}

// GetMetrics 获取指标缓存
func (c *PrometheusCache) GetMetrics(ctx context.Context, agentID, sessionID, query, timeRange string) (interface{}, error) {
	key := &MonitoringCacheKey{
		AgentID:   agentID,
		SessionID: sessionID,
		Source:    "prometheus",
		Query:     query,
		TimeRange: timeRange,
	}
	value, err := c.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return value.Data, nil
}

// SetMetrics 设置指标缓存
func (c *PrometheusCache) SetMetrics(ctx context.Context, agentID, sessionID, query, timeRange string, metrics interface{}) error {
	key := &MonitoringCacheKey{
		AgentID:   agentID,
		SessionID: sessionID,
		Source:    "prometheus",
		Query:     query,
		TimeRange: timeRange,
	}
	return c.Set(ctx, key, metrics)
}

// ElasticsearchCache Elasticsearch 日志缓存（便捷方法）
type ElasticsearchCache struct {
	*MonitoringCache
}

// NewElasticsearchCache 创建 Elasticsearch 缓存
func NewElasticsearchCache(manager *Manager) *ElasticsearchCache {
	return &ElasticsearchCache{
		MonitoringCache: NewMonitoringCache(manager),
	}
}

// GetLogs 获取日志缓存
func (c *ElasticsearchCache) GetLogs(ctx context.Context, agentID, sessionID, query, timeRange string) (interface{}, error) {
	key := &MonitoringCacheKey{
		AgentID:   agentID,
		SessionID: sessionID,
		Source:    "elasticsearch",
		Query:     query,
		TimeRange: timeRange,
	}
	value, err := c.Get(ctx, key)
	if err != nil {
		return nil, err
	}
	return value.Data, nil
}

// SetLogs 设置日志缓存
func (c *ElasticsearchCache) SetLogs(ctx context.Context, agentID, sessionID, query, timeRange string, logs interface{}) error {
	key := &MonitoringCacheKey{
		AgentID:   agentID,
		SessionID: sessionID,
		Source:    "elasticsearch",
		Query:     query,
		TimeRange: timeRange,
	}
	return c.Set(ctx, key, logs)
}
