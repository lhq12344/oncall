package test

import (
	"context"
	"testing"
	"time"

	"go_agent/internal/cache"

	"github.com/cloudwego/eino/schema"
	"github.com/redis/go-redis/v9"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

// 测试用 Redis 客户端
func getTestRedisClient() *redis.Client {
	return redis.NewClient(&redis.Options{
		Addr: "localhost:30379",
		DB:   1, // 使用测试数据库
	})
}

// TestCacheManager 测试缓存管理器
func TestCacheManager(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	rdb := getTestRedisClient()

	// 清理测试数据
	defer func() {
		rdb.FlushDB(context.Background())
	}()

	t.Run("创建缓存管理器", func(t *testing.T) {
		config := cache.DefaultConfig()
		config.RedisClient = rdb
		config.Logger = logger

		manager, err := cache.NewManager(config)
		assert.NoError(t, err)
		assert.NotNil(t, manager)
	})

	t.Run("基本缓存操作", func(t *testing.T) {
		config := cache.DefaultConfig()
		config.RedisClient = rdb
		config.Logger = logger

		manager, _ := cache.NewManager(config)

		ctx := context.Background()
		key := &cache.CacheKey{
			Type:      cache.CacheTypeGeneral,
			AgentID:   "test_agent",
			SessionID: "test_session",
			Key:       "test_key",
		}

		// 设置缓存
		testData := map[string]string{"hello": "world"}
		err := manager.Set(ctx, key, testData, 10*time.Second)
		assert.NoError(t, err)

		// 获取缓存
		var result map[string]string
		err = manager.Get(ctx, key, &result)
		assert.NoError(t, err)
		assert.Equal(t, "world", result["hello"])

		// 检查存在
		exists, err := manager.Exists(ctx, key)
		assert.NoError(t, err)
		assert.True(t, exists)

		// 删除缓存
		err = manager.Delete(ctx, key)
		assert.NoError(t, err)

		// 验证已删除
		err = manager.Get(ctx, key, &result)
		assert.ErrorIs(t, err, cache.ErrCacheMiss)
	})

	t.Run("Agent 隔离", func(t *testing.T) {
		config := cache.DefaultConfig()
		config.RedisClient = rdb
		config.Logger = logger

		manager, _ := cache.NewManager(config)
		ctx := context.Background()

		// Agent 1 的缓存
		key1 := &cache.CacheKey{
			Type:      cache.CacheTypeGeneral,
			AgentID:   "agent1",
			SessionID: "session1",
			Key:       "data",
		}
		manager.Set(ctx, key1, "agent1_data", 10*time.Second)

		// Agent 2 的缓存
		key2 := &cache.CacheKey{
			Type:      cache.CacheTypeGeneral,
			AgentID:   "agent2",
			SessionID: "session1",
			Key:       "data",
		}
		manager.Set(ctx, key2, "agent2_data", 10*time.Second)

		// 验证隔离
		var result1, result2 string
		manager.Get(ctx, key1, &result1)
		manager.Get(ctx, key2, &result2)

		assert.Equal(t, "agent1_data", result1)
		assert.Equal(t, "agent2_data", result2)
		assert.NotEqual(t, result1, result2)
	})

	t.Run("会话隔离", func(t *testing.T) {
		config := cache.DefaultConfig()
		config.RedisClient = rdb
		config.Logger = logger

		manager, _ := cache.NewManager(config)
		ctx := context.Background()

		// 会话 1 的缓存
		key1 := &cache.CacheKey{
			Type:      cache.CacheTypeGeneral,
			AgentID:   "agent1",
			SessionID: "session1",
			Key:       "data",
		}
		manager.Set(ctx, key1, "session1_data", 10*time.Second)

		// 会话 2 的缓存
		key2 := &cache.CacheKey{
			Type:      cache.CacheTypeGeneral,
			AgentID:   "agent1",
			SessionID: "session2",
			Key:       "data",
		}
		manager.Set(ctx, key2, "session2_data", 10*time.Second)

		// 验证隔离
		var result1, result2 string
		manager.Get(ctx, key1, &result1)
		manager.Get(ctx, key2, &result2)

		assert.Equal(t, "session1_data", result1)
		assert.Equal(t, "session2_data", result2)
		assert.NotEqual(t, result1, result2)
	})
}

// TestLLMCache 测试 LLM 缓存
func TestLLMCache(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	rdb := getTestRedisClient()

	defer func() {
		rdb.FlushDB(context.Background())
	}()

	t.Run("LLM 响应缓存", func(t *testing.T) {
		config := cache.DefaultConfig()
		config.RedisClient = rdb
		config.Logger = logger

		manager, _ := cache.NewManager(config)
		llmCache := cache.NewLLMCache(manager)

		ctx := context.WithValue(context.Background(), "timestamp", time.Now().Unix())

		// 缓存键
		key := &cache.LLMCacheKey{
			AgentID:   "test_agent",
			SessionID: "test_session",
			Messages: []*schema.Message{
				{Role: schema.User, Content: "Hello"},
			},
			Model: "claude-opus-4-6",
		}

		// 设置缓存
		response := &schema.Message{
			Role:    schema.Assistant,
			Content: "Hello! How can I help you?",
		}
		err := llmCache.Set(ctx, key, response)
		assert.NoError(t, err)

		// 获取缓存
		cached, err := llmCache.Get(ctx, key)
		assert.NoError(t, err)
		assert.Equal(t, "Hello! How can I help you?", cached.Response.Content)
	})

	t.Run("不同 Agent 的 LLM 缓存隔离", func(t *testing.T) {
		config := cache.DefaultConfig()
		config.RedisClient = rdb
		config.Logger = logger

		manager, _ := cache.NewManager(config)
		llmCache := cache.NewLLMCache(manager)

		ctx := context.WithValue(context.Background(), "timestamp", time.Now().Unix())

		// Agent 1
		key1 := &cache.LLMCacheKey{
			AgentID:   "agent1",
			SessionID: "session1",
			Messages:  []*schema.Message{{Role: schema.User, Content: "Test"}},
			Model:     "claude-opus-4-6",
		}
		llmCache.Set(ctx, key1, &schema.Message{Content: "Response 1"})

		// Agent 2
		key2 := &cache.LLMCacheKey{
			AgentID:   "agent2",
			SessionID: "session1",
			Messages:  []*schema.Message{{Role: schema.User, Content: "Test"}},
			Model:     "claude-opus-4-6",
		}
		llmCache.Set(ctx, key2, &schema.Message{Content: "Response 2"})

		// 验证隔离
		cached1, _ := llmCache.Get(ctx, key1)
		cached2, _ := llmCache.Get(ctx, key2)

		assert.Equal(t, "Response 1", cached1.Response.Content)
		assert.Equal(t, "Response 2", cached2.Response.Content)
	})
}

// TestVectorCache 测试向量缓存
func TestVectorCache(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	rdb := getTestRedisClient()

	defer func() {
		rdb.FlushDB(context.Background())
	}()

	t.Run("向量检索缓存", func(t *testing.T) {
		config := cache.DefaultConfig()
		config.RedisClient = rdb
		config.Logger = logger

		manager, _ := cache.NewManager(config)
		vectorCache := cache.NewVectorCache(manager)

		ctx := context.WithValue(context.Background(), "timestamp", time.Now().Unix())

		// 缓存键
		key := &cache.VectorCacheKey{
			AgentID:   "test_agent",
			SessionID: "test_session",
			Query:     "故障排查",
			TopK:      5,
			Collection: "oncall_knowledge",
		}

		// 设置缓存
		results := []cache.VectorResult{
			{ID: "doc1", Score: 0.95, Content: "文档1"},
			{ID: "doc2", Score: 0.85, Content: "文档2"},
		}
		err := vectorCache.Set(ctx, key, results)
		assert.NoError(t, err)

		// 获取缓存
		cached, err := vectorCache.Get(ctx, key)
		assert.NoError(t, err)
		assert.Len(t, cached.Results, 2)
		assert.Equal(t, "doc1", cached.Results[0].ID)
	})
}

// TestMonitoringCache 测试监控缓存
func TestMonitoringCache(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	rdb := getTestRedisClient()

	defer func() {
		rdb.FlushDB(context.Background())
	}()

	t.Run("监控数据缓存", func(t *testing.T) {
		config := cache.DefaultConfig()
		config.RedisClient = rdb
		config.Logger = logger

		manager, _ := cache.NewManager(config)
		monitoringCache := cache.NewMonitoringCache(manager)

		ctx := context.Background()

		// 缓存键
		key := &cache.MonitoringCacheKey{
			AgentID:   "test_agent",
			SessionID: "test_session",
			Source:    "prometheus",
			Query:     "cpu_usage",
			TimeRange: "5m",
		}

		// 设置缓存
		data := map[string]interface{}{
			"metric": "cpu_usage",
			"value":  45.2,
		}
		err := monitoringCache.Set(ctx, key, data)
		assert.NoError(t, err)

		// 获取缓存
		cached, err := monitoringCache.Get(ctx, key)
		assert.NoError(t, err)
		assert.Equal(t, "prometheus", cached.Source)
	})

	t.Run("K8s 缓存", func(t *testing.T) {
		config := cache.DefaultConfig()
		config.RedisClient = rdb
		config.Logger = logger

		manager, _ := cache.NewManager(config)
		k8sCache := cache.NewK8sCache(manager)

		ctx := context.Background()

		// 设置 Pod 缓存
		pods := []string{"pod1", "pod2", "pod3"}
		err := k8sCache.SetPods(ctx, "agent1", "session1", "default", pods)
		assert.NoError(t, err)

		// 获取 Pod 缓存
		cached, err := k8sCache.GetPods(ctx, "agent1", "session1", "default")
		assert.NoError(t, err)
		assert.NotNil(t, cached)
	})
}

// TestEvictionPolicy 测试失效策略
func TestEvictionPolicy(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	rdb := getTestRedisClient()

	defer func() {
		rdb.FlushDB(context.Background())
	}()

	t.Run("TTL 策略", func(t *testing.T) {
		config := cache.DefaultConfig()
		config.RedisClient = rdb
		config.Logger = logger

		manager, _ := cache.NewManager(config)
		ctx := context.Background()

		key := &cache.CacheKey{
			Type:      cache.CacheTypeGeneral,
			AgentID:   "test_agent",
			SessionID: "test_session",
			Key:       "ttl_test",
		}

		// 设置短 TTL
		manager.Set(ctx, key, "test_data", 1*time.Second)

		// 立即获取应该成功
		var result string
		err := manager.Get(ctx, key, &result)
		assert.NoError(t, err)

		// 等待过期
		time.Sleep(2 * time.Second)

		// 再次获取应该失败
		err = manager.Get(ctx, key, &result)
		assert.ErrorIs(t, err, cache.ErrCacheMiss)
	})

	t.Run("Agent 隔离策略", func(t *testing.T) {
		config := cache.DefaultConfig()
		config.RedisClient = rdb
		config.Logger = logger

		manager, _ := cache.NewManager(config)
		policy := cache.NewAgentIsolationPolicy(manager)

		ctx := context.Background()

		// 设置一些缓存
		key := &cache.CacheKey{
			Type:      cache.CacheTypeGeneral,
			AgentID:   "agent1",
			SessionID: "session1",
			Key:       "data",
		}
		manager.Set(ctx, key, "test", 10*time.Second)

		// 验证隔离
		err := policy.ValidateIsolation(ctx, "agent1")
		assert.NoError(t, err)
	})
}

// TestCacheStats 测试缓存统计
func TestCacheStats(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	rdb := getTestRedisClient()

	defer func() {
		rdb.FlushDB(context.Background())
	}()

	t.Run("获取缓存统计", func(t *testing.T) {
		config := cache.DefaultConfig()
		config.RedisClient = rdb
		config.Logger = logger

		manager, _ := cache.NewManager(config)
		ctx := context.Background()

		// 设置不同类型的缓存
		for i := 0; i < 5; i++ {
			key := &cache.CacheKey{
				Type:      cache.CacheTypeLLM,
				AgentID:   "agent1",
				SessionID: "session1",
				Key:       string(rune(i)),
			}
			manager.Set(ctx, key, "data", 10*time.Second)
		}

		for i := 0; i < 3; i++ {
			key := &cache.CacheKey{
				Type:      cache.CacheTypeVector,
				AgentID:   "agent1",
				SessionID: "session1",
				Key:       string(rune(i)),
			}
			manager.Set(ctx, key, "data", 10*time.Second)
		}

		// 获取统计
		stats, err := manager.GetStats(ctx)
		assert.NoError(t, err)
		assert.NotNil(t, stats)
		assert.True(t, stats.TotalKeys >= 8)
	})
}
