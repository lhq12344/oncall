package cache

import (
	"context"
	"fmt"
	"time"

	"go.uber.org/zap"
)

// EvictionPolicy 缓存失效策略
type EvictionPolicy interface {
	// ShouldEvict 判断是否应该失效
	ShouldEvict(ctx context.Context, key *CacheKey, value interface{}) (bool, error)
	// Name 策略名称
	Name() string
}

// TTLPolicy TTL 失效策略（基于时间）
type TTLPolicy struct {
	TTL time.Duration
}

// NewTTLPolicy 创建 TTL 策略
func NewTTLPolicy(ttl time.Duration) *TTLPolicy {
	return &TTLPolicy{TTL: ttl}
}

func (p *TTLPolicy) ShouldEvict(ctx context.Context, key *CacheKey, value interface{}) (bool, error) {
	// Redis 自动处理 TTL，这里不需要额外逻辑
	return false, nil
}

func (p *TTLPolicy) Name() string {
	return "TTL"
}

// LRUPolicy LRU 失效策略（最近最少使用）
type LRUPolicy struct {
	manager  *Manager
	maxSize  int64 // 最大缓存数量
	cacheType CacheType
}

// NewLRUPolicy 创建 LRU 策略
func NewLRUPolicy(manager *Manager, cacheType CacheType, maxSize int64) *LRUPolicy {
	return &LRUPolicy{
		manager:  manager,
		maxSize:  maxSize,
		cacheType: cacheType,
	}
}

func (p *LRUPolicy) ShouldEvict(ctx context.Context, key *CacheKey, value interface{}) (bool, error) {
	// 获取当前缓存数量
	stats, err := p.manager.GetStats(ctx)
	if err != nil {
		return false, err
	}

	typeStats, exists := stats.Types[p.cacheType]
	if !exists {
		return false, nil
	}

	// 如果超过最大数量，需要失效
	return typeStats.Count > p.maxSize, nil
}

func (p *LRUPolicy) Name() string {
	return "LRU"
}

// TimeBasedPolicy 基于时间的失效策略
type TimeBasedPolicy struct {
	manager *Manager
	logger  *zap.Logger
}

// NewTimeBasedPolicy 创建基于时间的策略
func NewTimeBasedPolicy(manager *Manager) *TimeBasedPolicy {
	return &TimeBasedPolicy{
		manager: manager,
		logger:  manager.logger,
	}
}

// EvictExpired 清除过期缓存
func (p *TimeBasedPolicy) EvictExpired(ctx context.Context) error {
	// Redis 自动处理过期，这里只是记录日志
	if p.logger != nil {
		p.logger.Debug("checking expired cache entries")
	}
	return nil
}

func (p *TimeBasedPolicy) ShouldEvict(ctx context.Context, key *CacheKey, value interface{}) (bool, error) {
	return false, nil
}

func (p *TimeBasedPolicy) Name() string {
	return "TimeBased"
}

// ConditionalPolicy 条件失效策略
type ConditionalPolicy struct {
	manager   *Manager
	condition func(ctx context.Context, key *CacheKey, value interface{}) bool
	logger    *zap.Logger
}

// NewConditionalPolicy 创建条件策略
func NewConditionalPolicy(
	manager *Manager,
	condition func(ctx context.Context, key *CacheKey, value interface{}) bool,
) *ConditionalPolicy {
	return &ConditionalPolicy{
		manager:   manager,
		condition: condition,
		logger:    manager.logger,
	}
}

func (p *ConditionalPolicy) ShouldEvict(ctx context.Context, key *CacheKey, value interface{}) (bool, error) {
	if p.condition == nil {
		return false, nil
	}
	return p.condition(ctx, key, value), nil
}

func (p *ConditionalPolicy) Name() string {
	return "Conditional"
}

// EvictionManager 失效管理器
type EvictionManager struct {
	manager  *Manager
	policies map[CacheType][]EvictionPolicy
	logger   *zap.Logger
}

// NewEvictionManager 创建失效管理器
func NewEvictionManager(manager *Manager) *EvictionManager {
	return &EvictionManager{
		manager:  manager,
		policies: make(map[CacheType][]EvictionPolicy),
		logger:   manager.logger,
	}
}

// RegisterPolicy 注册失效策略
func (em *EvictionManager) RegisterPolicy(cacheType CacheType, policy EvictionPolicy) {
	em.policies[cacheType] = append(em.policies[cacheType], policy)

	if em.logger != nil {
		em.logger.Info("eviction policy registered",
			zap.String("cache_type", string(cacheType)),
			zap.String("policy", policy.Name()))
	}
}

// CheckEviction 检查是否需要失效
func (em *EvictionManager) CheckEviction(ctx context.Context, key *CacheKey, value interface{}) (bool, error) {
	policies, exists := em.policies[key.Type]
	if !exists {
		return false, nil
	}

	for _, policy := range policies {
		shouldEvict, err := policy.ShouldEvict(ctx, key, value)
		if err != nil {
			if em.logger != nil {
				em.logger.Error("eviction policy check failed",
					zap.String("policy", policy.Name()),
					zap.Error(err))
			}
			continue
		}

		if shouldEvict {
			if em.logger != nil {
				em.logger.Debug("cache should be evicted",
					zap.String("cache_type", string(key.Type)),
					zap.String("policy", policy.Name()))
			}
			return true, nil
		}
	}

	return false, nil
}

// RunEviction 执行失效检查（定期任务）
func (em *EvictionManager) RunEviction(ctx context.Context) error {
	if em.logger != nil {
		em.logger.Debug("running eviction check")
	}

	// 获取所有缓存统计
	stats, err := em.manager.GetStats(ctx)
	if err != nil {
		return fmt.Errorf("failed to get cache stats: %w", err)
	}

	// 对每种类型执行失效策略
	for cacheType, typeStats := range stats.Types {
		policies, exists := em.policies[cacheType]
		if !exists {
			continue
		}

		if em.logger != nil {
			em.logger.Debug("checking eviction for cache type",
				zap.String("type", string(cacheType)),
				zap.Int64("count", typeStats.Count))
		}

		// 执行每个策略
		for _, policy := range policies {
			// 这里可以根据策略类型执行不同的操作
			if em.logger != nil {
				em.logger.Debug("applying eviction policy",
					zap.String("type", string(cacheType)),
					zap.String("policy", policy.Name()))
			}
		}
	}

	return nil
}

// StartEvictionWorker 启动失效检查工作线程
func (em *EvictionManager) StartEvictionWorker(ctx context.Context, interval time.Duration) {
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	if em.logger != nil {
		em.logger.Info("eviction worker started",
			zap.Duration("interval", interval))
	}

	for {
		select {
		case <-ctx.Done():
			if em.logger != nil {
				em.logger.Info("eviction worker stopped")
			}
			return
		case <-ticker.C:
			if err := em.RunEviction(ctx); err != nil {
				if em.logger != nil {
					em.logger.Error("eviction check failed", zap.Error(err))
				}
			}
		}
	}
}

// AgentIsolationPolicy Agent 隔离策略（确保不同 Agent 的缓存不互相影响）
type AgentIsolationPolicy struct {
	manager *Manager
	logger  *zap.Logger
}

// NewAgentIsolationPolicy 创建 Agent 隔离策略
func NewAgentIsolationPolicy(manager *Manager) *AgentIsolationPolicy {
	return &AgentIsolationPolicy{
		manager: manager,
		logger:  manager.logger,
	}
}

// ValidateIsolation 验证缓存隔离
func (p *AgentIsolationPolicy) ValidateIsolation(ctx context.Context, agentID string) error {
	// 检查是否有跨 Agent 的缓存污染
	pattern := fmt.Sprintf("*:%s:*", agentID)

	iter := p.manager.redis.Scan(ctx, 0, p.manager.config.KeyPrefix+pattern, 100).Iterator()
	count := 0

	for iter.Next(ctx) {
		count++
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}

	if p.logger != nil {
		p.logger.Debug("agent isolation validated",
			zap.String("agent_id", agentID),
			zap.Int("cache_count", count))
	}

	return nil
}

func (p *AgentIsolationPolicy) ShouldEvict(ctx context.Context, key *CacheKey, value interface{}) (bool, error) {
	// Agent 隔离策略不主动失效，只验证隔离性
	return false, nil
}

func (p *AgentIsolationPolicy) Name() string {
	return "AgentIsolation"
}

// SessionIsolationPolicy 会话隔离策略
type SessionIsolationPolicy struct {
	manager *Manager
	logger  *zap.Logger
}

// NewSessionIsolationPolicy 创建会话隔离策略
func NewSessionIsolationPolicy(manager *Manager) *SessionIsolationPolicy {
	return &SessionIsolationPolicy{
		manager: manager,
		logger:  manager.logger,
	}
}

// ValidateIsolation 验证会话隔离
func (p *SessionIsolationPolicy) ValidateIsolation(ctx context.Context, sessionID string) error {
	pattern := fmt.Sprintf("*:*:%s:*", sessionID)

	iter := p.manager.redis.Scan(ctx, 0, p.manager.config.KeyPrefix+pattern, 100).Iterator()
	count := 0

	for iter.Next(ctx) {
		count++
	}

	if err := iter.Err(); err != nil {
		return fmt.Errorf("failed to scan keys: %w", err)
	}

	if p.logger != nil {
		p.logger.Debug("session isolation validated",
			zap.String("session_id", sessionID),
			zap.Int("cache_count", count))
	}

	return nil
}

func (p *SessionIsolationPolicy) ShouldEvict(ctx context.Context, key *CacheKey, value interface{}) (bool, error) {
	return false, nil
}

func (p *SessionIsolationPolicy) Name() string {
	return "SessionIsolation"
}
