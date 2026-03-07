package cache

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

var (
	// ErrCacheMiss 缓存未命中
	ErrCacheMiss = errors.New("cache miss")
	// ErrCacheInvalid 缓存数据无效
	ErrCacheInvalid = errors.New("cache invalid")
)

// CacheType 缓存类型
type CacheType string

const (
	// CacheTypeLLM LLM 响应缓存
	CacheTypeLLM CacheType = "llm"
	// CacheTypeVector 向量检索缓存
	CacheTypeVector CacheType = "vector"
	// CacheTypeMonitoring 监控数据缓存
	CacheTypeMonitoring CacheType = "monitoring"
	// CacheTypeGeneral 通用缓存
	CacheTypeGeneral CacheType = "general"
)

// Config 缓存配置
type Config struct {
	RedisClient *redis.Client
	Logger      *zap.Logger

	// 默认 TTL 配置（按类型）
	LLMTTL        time.Duration // LLM 响应缓存时间
	VectorTTL     time.Duration // 向量检索缓存时间
	MonitoringTTL time.Duration // 监控数据缓存时间
	GeneralTTL    time.Duration // 通用缓存时间

	// 缓存键前缀
	KeyPrefix string

	// 是否启用缓存
	Enabled bool
}

// DefaultConfig 默认配置
func DefaultConfig() *Config {
	return &Config{
		LLMTTL:        30 * time.Minute, // LLM 响应缓存 30 分钟
		VectorTTL:     1 * time.Hour,    // 向量检索缓存 1 小时
		MonitoringTTL: 5 * time.Minute,  // 监控数据缓存 5 分钟
		GeneralTTL:    15 * time.Minute, // 通用缓存 15 分钟
		KeyPrefix:     "oncall:cache:",
		Enabled:       true,
	}
}

// Manager 缓存管理器
type Manager struct {
	redis  *redis.Client
	config *Config
	logger *zap.Logger
}

// NewManager 创建缓存管理器
func NewManager(config *Config) (*Manager, error) {
	if config == nil {
		config = DefaultConfig()
	}

	if config.RedisClient == nil {
		return nil, errors.New("redis client is required")
	}

	// 测试 Redis 连接
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	if err := config.RedisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("redis ping failed: %w", err)
	}

	return &Manager{
		redis:  config.RedisClient,
		config: config,
		logger: config.Logger,
	}, nil
}

// CacheKey 缓存键结构（确保 Agent 间隔离）
type CacheKey struct {
	Type      CacheType // 缓存类型
	AgentID   string    // Agent ID（用于隔离不同 Agent 的缓存）
	SessionID string    // 会话 ID（用于隔离不同会话的缓存）
	Key       string    // 实际的缓存键
}

// String 生成 Redis 键
func (ck *CacheKey) String(prefix string) string {
	// 格式: prefix:type:agentID:sessionID:key
	// 这样可以确保不同 Agent 和会话的缓存完全隔离
	return fmt.Sprintf("%s%s:%s:%s:%s",
		prefix,
		ck.Type,
		ck.AgentID,
		ck.SessionID,
		ck.Key,
	)
}

// GenerateKeyHash 生成键的哈希值（用于长键）
func GenerateKeyHash(data interface{}) string {
	b, _ := json.Marshal(data)
	hash := sha256.Sum256(b)
	return hex.EncodeToString(hash[:])
}

// Set 设置缓存
func (m *Manager) Set(ctx context.Context, key *CacheKey, value interface{}, ttl time.Duration) error {
	if !m.config.Enabled {
		return nil
	}

	// 如果未指定 TTL，使用默认值
	if ttl == 0 {
		ttl = m.getTTLByType(key.Type)
	}

	// 序列化值
	data, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("failed to marshal value: %w", err)
	}

	// 存储到 Redis
	redisKey := key.String(m.config.KeyPrefix)
	if err := m.redis.Set(ctx, redisKey, data, ttl).Err(); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to set cache",
				zap.String("key", redisKey),
				zap.Error(err))
		}
		return fmt.Errorf("failed to set cache: %w", err)
	}

	if m.logger != nil {
		m.logger.Debug("cache set",
			zap.String("type", string(key.Type)),
			zap.String("agent_id", key.AgentID),
			zap.String("session_id", key.SessionID),
			zap.String("key", key.Key),
			zap.Duration("ttl", ttl))
	}

	return nil
}

// Get 获取缓存
func (m *Manager) Get(ctx context.Context, key *CacheKey, dest interface{}) error {
	if !m.config.Enabled {
		return ErrCacheMiss
	}

	redisKey := key.String(m.config.KeyPrefix)
	data, err := m.redis.Get(ctx, redisKey).Bytes()
	if err != nil {
		if err == redis.Nil {
			return ErrCacheMiss
		}
		if m.logger != nil {
			m.logger.Error("failed to get cache",
				zap.String("key", redisKey),
				zap.Error(err))
		}
		return fmt.Errorf("failed to get cache: %w", err)
	}

	// 反序列化
	if err := json.Unmarshal(data, dest); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to unmarshal cache data",
				zap.String("key", redisKey),
				zap.Error(err))
		}
		return ErrCacheInvalid
	}

	if m.logger != nil {
		m.logger.Debug("cache hit",
			zap.String("type", string(key.Type)),
			zap.String("agent_id", key.AgentID),
			zap.String("session_id", key.SessionID),
			zap.String("key", key.Key))
	}

	return nil
}

// Delete 删除缓存
func (m *Manager) Delete(ctx context.Context, key *CacheKey) error {
	if !m.config.Enabled {
		return nil
	}

	redisKey := key.String(m.config.KeyPrefix)
	if err := m.redis.Del(ctx, redisKey).Err(); err != nil {
		if m.logger != nil {
			m.logger.Error("failed to delete cache",
				zap.String("key", redisKey),
				zap.Error(err))
		}
		return fmt.Errorf("failed to delete cache: %w", err)
	}

	if m.logger != nil {
		m.logger.Debug("cache deleted",
			zap.String("type", string(key.Type)),
			zap.String("agent_id", key.AgentID),
			zap.String("session_id", key.SessionID),
			zap.String("key", key.Key))
	}

	return nil
}

// DeleteByPattern 按模式删除缓存（用于批量清理）
func (m *Manager) DeleteByPattern(ctx context.Context, pattern string) error {
	if !m.config.Enabled {
		return nil
	}

	fullPattern := m.config.KeyPrefix + pattern
	iter := m.redis.Scan(ctx, 0, fullPattern, 100).Iterator()

	count := 0
	for iter.Next(ctx) {
		if err := m.redis.Del(ctx, iter.Val()).Err(); err != nil {
			if m.logger != nil {
				m.logger.Error("failed to delete cache key",
					zap.String("key", iter.Val()),
					zap.Error(err))
			}
		} else {
			count++
		}
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}

	if m.logger != nil {
		m.logger.Info("cache deleted by pattern",
			zap.String("pattern", fullPattern),
			zap.Int("count", count))
	}

	return nil
}

// InvalidateAgent 清除指定 Agent 的所有缓存
func (m *Manager) InvalidateAgent(ctx context.Context, agentID string) error {
	pattern := fmt.Sprintf("*:%s:*", agentID)
	return m.DeleteByPattern(ctx, pattern)
}

// InvalidateSession 清除指定会话的所有缓存
func (m *Manager) InvalidateSession(ctx context.Context, sessionID string) error {
	pattern := fmt.Sprintf("*:*:%s:*", sessionID)
	return m.DeleteByPattern(ctx, pattern)
}

// InvalidateType 清除指定类型的所有缓存
func (m *Manager) InvalidateType(ctx context.Context, cacheType CacheType) error {
	pattern := fmt.Sprintf("%s:*", cacheType)
	return m.DeleteByPattern(ctx, pattern)
}

// GetStats 获取缓存统计信息
func (m *Manager) GetStats(ctx context.Context) (*Stats, error) {
	stats := &Stats{
		Types: make(map[CacheType]*TypeStats),
	}

	// 扫描所有缓存键
	iter := m.redis.Scan(ctx, 0, m.config.KeyPrefix+"*", 1000).Iterator()
	for iter.Next(ctx) {
		key := iter.Val()

		// 解析键类型
		// 格式: prefix:type:agentID:sessionID:key
		// 跳过 prefix
		keyWithoutPrefix := key[len(m.config.KeyPrefix):]

		// 提取类型
		var cacheType CacheType
		for _, t := range []CacheType{CacheTypeLLM, CacheTypeVector, CacheTypeMonitoring, CacheTypeGeneral} {
			if len(keyWithoutPrefix) > len(t) && keyWithoutPrefix[:len(t)] == string(t) {
				cacheType = t
				break
			}
		}

		if cacheType == "" {
			continue
		}

		// 初始化类型统计
		if stats.Types[cacheType] == nil {
			stats.Types[cacheType] = &TypeStats{}
		}

		stats.Types[cacheType].Count++
		stats.TotalKeys++

		// 获取 TTL
		ttl, err := m.redis.TTL(ctx, key).Result()
		if err == nil && ttl > 0 {
			stats.Types[cacheType].TotalTTL += ttl
		}
	}

	if err := iter.Err(); err != nil {
		return nil, fmt.Errorf("failed to scan keys: %w", err)
	}

	// 计算平均 TTL
	for _, typeStats := range stats.Types {
		if typeStats.Count > 0 {
			typeStats.AvgTTL = typeStats.TotalTTL / time.Duration(typeStats.Count)
		}
	}

	return stats, nil
}

// Stats 缓存统计信息
type Stats struct {
	TotalKeys int64
	Types     map[CacheType]*TypeStats
}

// TypeStats 类型统计信息
type TypeStats struct {
	Count    int64
	TotalTTL time.Duration
	AvgTTL   time.Duration
}

// getTTLByType 根据类型获取默认 TTL
func (m *Manager) getTTLByType(cacheType CacheType) time.Duration {
	switch cacheType {
	case CacheTypeLLM:
		return m.config.LLMTTL
	case CacheTypeVector:
		return m.config.VectorTTL
	case CacheTypeMonitoring:
		return m.config.MonitoringTTL
	case CacheTypeGeneral:
		return m.config.GeneralTTL
	default:
		return m.config.GeneralTTL
	}
}

// Exists 检查缓存是否存在
func (m *Manager) Exists(ctx context.Context, key *CacheKey) (bool, error) {
	if !m.config.Enabled {
		return false, nil
	}

	redisKey := key.String(m.config.KeyPrefix)
	result, err := m.redis.Exists(ctx, redisKey).Result()
	if err != nil {
		return false, fmt.Errorf("failed to check cache existence: %w", err)
	}

	return result > 0, nil
}

// Refresh 刷新缓存 TTL
func (m *Manager) Refresh(ctx context.Context, key *CacheKey, ttl time.Duration) error {
	if !m.config.Enabled {
		return nil
	}

	if ttl == 0 {
		ttl = m.getTTLByType(key.Type)
	}

	redisKey := key.String(m.config.KeyPrefix)
	if err := m.redis.Expire(ctx, redisKey, ttl).Err(); err != nil {
		return fmt.Errorf("failed to refresh cache TTL: %w", err)
	}

	return nil
}
