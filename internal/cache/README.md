# 缓存机制模块

本模块实现了 oncall 系统的缓存功能，包括：

1. **LLM 响应缓存** - 缓存相同问题的 LLM 回答
2. **向量检索缓存** - 缓存 Milvus 查询结果
3. **监控数据缓存** - 缓存 K8s/Prometheus/ES 数据
4. **缓存失效策略** - TTL、LRU、条件失效等策略

## 核心特性

### 🔒 Agent 间上下文隔离

**关键设计**：每个缓存键包含 `AgentID` 和 `SessionID`，确保不同 Agent 和会话的缓存完全隔离。

```
缓存键格式: prefix:type:agentID:sessionID:key
示例: oncall:cache:llm:agent1:session1:abc123
```

这样设计的好处：
- ✅ 不同 Agent 的缓存互不影响
- ✅ 不同会话的缓存互不污染
- ✅ 可以按 Agent 或会话批量清理缓存
- ✅ 支持多租户场景

## 目录结构

```
internal/cache/
├── cache.go              # 核心缓存管理器
├── llm_cache.go          # LLM 响应缓存
├── vector_cache.go       # 向量检索缓存
├── monitoring_cache.go   # 监控数据缓存
└── eviction.go           # 缓存失效策略
```

## 快速开始

### 1. 初始化缓存管理器

```go
import (
    "go_agent/internal/cache"
    "github.com/redis/go-redis/v9"
)

// 创建 Redis 客户端
rdb := redis.NewClient(&redis.Options{
    Addr: "localhost:6379",
    DB:   0,
})

// 创建缓存管理器
config := cache.DefaultConfig()
config.RedisClient = rdb
config.Logger = logger

manager, err := cache.NewManager(config)
if err != nil {
    log.Fatal(err)
}
```

### 2. LLM 响应缓存

```go
llmCache := cache.NewLLMCache(manager)

// 缓存键（包含 Agent 和会话隔离）
key := &cache.LLMCacheKey{
    AgentID:   "knowledge_agent",  // Agent ID
    SessionID: "user_session_123", // 会话 ID
    Messages: []*schema.Message{
        {Role: schema.User, Content: "如何排查 CPU 高的问题？"},
    },
    Model: "claude-opus-4-6",
}

// 尝试从缓存获取
cached, err := llmCache.Get(ctx, key)
if err == cache.ErrCacheMiss {
    // 缓存未命中，调用 LLM
    response := callLLM(key.Messages)

    // 存入缓存
    llmCache.Set(ctx, key, response)
} else {
    // 缓存命中，直接使用
    response = cached.Response
}
```

### 3. 向量检索缓存

```go
vectorCache := cache.NewVectorCache(manager)

key := &cache.VectorCacheKey{
    AgentID:      "knowledge_agent",
    SessionID:    "user_session_123",
    Query:        "CPU 故障排查",
    TopK:         5,
    Collection:   "oncall_knowledge",
}

// 尝试从缓存获取
cached, err := vectorCache.Get(ctx, key)
if err == cache.ErrCacheMiss {
    // 缓存未命中，查询 Milvus
    results := queryMilvus(key.Query, key.TopK)

    // 存入缓存
    vectorCache.Set(ctx, key, results)
} else {
    // 缓存命中
    results = cached.Results
}
```

### 4. 监控数据缓存

```go
// K8s 缓存
k8sCache := cache.NewK8sCache(manager)

pods, err := k8sCache.GetPods(ctx, "ops_agent", "session_123", "default")
if err == cache.ErrCacheMiss {
    // 查询 K8s
    pods = queryK8sPods("default")
    k8sCache.SetPods(ctx, "ops_agent", "session_123", "default", pods)
}

// Prometheus 缓存
promCache := cache.NewPrometheusCache(manager)

metrics, err := promCache.GetMetrics(ctx, "ops_agent", "session_123", "cpu_usage", "5m")
if err == cache.ErrCacheMiss {
    // 查询 Prometheus
    metrics = queryPrometheus("cpu_usage", "5m")
    promCache.SetMetrics(ctx, "ops_agent", "session_123", "cpu_usage", "5m", metrics)
}
```

## Agent 隔离示例

### 场景：两个 Agent 同时查询相同问题

```go
// Agent 1 (knowledge_agent)
key1 := &cache.LLMCacheKey{
    AgentID:   "knowledge_agent",
    SessionID: "session_123",
    Messages:  []*schema.Message{{Content: "如何排查故障？"}},
    Model:     "claude-opus-4-6",
}
llmCache.Set(ctx, key1, &schema.Message{Content: "知识库回答..."})

// Agent 2 (ops_agent)
key2 := &cache.LLMCacheKey{
    AgentID:   "ops_agent",
    SessionID: "session_123",
    Messages:  []*schema.Message{{Content: "如何排查故障？"}},
    Model:     "claude-opus-4-6",
}
llmCache.Set(ctx, key2, &schema.Message{Content: "运维回答..."})

// 两个 Agent 的缓存完全隔离
cached1, _ := llmCache.Get(ctx, key1) // 返回 "知识库回答..."
cached2, _ := llmCache.Get(ctx, key2) // 返回 "运维回答..."
```

## 缓存失效策略

### 1. TTL 策略（默认）

```go
// 不同类型的缓存有不同的 TTL
config := cache.DefaultConfig()
config.LLMTTL = 30 * time.Minute        // LLM 缓存 30 分钟
config.VectorTTL = 1 * time.Hour        // 向量缓存 1 小时
config.MonitoringTTL = 5 * time.Minute  // 监控缓存 5 分钟
```

### 2. 手动失效

```go
// 清除指定 Agent 的所有缓存
manager.InvalidateAgent(ctx, "knowledge_agent")

// 清除指定会话的所有缓存
manager.InvalidateSession(ctx, "session_123")

// 清除指定类型的所有缓存
manager.InvalidateType(ctx, cache.CacheTypeLLM)
```

### 3. 条件失效

```go
evictionMgr := cache.NewEvictionManager(manager)

// 注册条件策略
policy := cache.NewConditionalPolicy(manager, func(ctx context.Context, key *cache.CacheKey, value interface{}) bool {
    // 自定义失效条件
    return shouldEvict(value)
})

evictionMgr.RegisterPolicy(cache.CacheTypeLLM, policy)
```

### 4. 自动失效工作线程

```go
evictionMgr := cache.NewEvictionManager(manager)

// 注册策略
evictionMgr.RegisterPolicy(cache.CacheTypeLLM, cache.NewTTLPolicy(30*time.Minute))

// 启动失效检查（每 5 分钟检查一次）
go evictionMgr.StartEvictionWorker(ctx, 5*time.Minute)
```

## 缓存统计

```go
stats, err := manager.GetStats(ctx)
if err != nil {
    log.Fatal(err)
}

fmt.Printf("总缓存数: %d\n", stats.TotalKeys)

for cacheType, typeStats := range stats.Types {
    fmt.Printf("%s 缓存:\n", cacheType)
    fmt.Printf("  数量: %d\n", typeStats.Count)
    fmt.Printf("  平均 TTL: %v\n", typeStats.AvgTTL)
}
```

## 配置参数

### 默认配置

```go
config := cache.DefaultConfig()
// LLMTTL:        30 * time.Minute
// VectorTTL:     1 * time.Hour
// MonitoringTTL: 5 * time.Minute
// GeneralTTL:    15 * time.Minute
// KeyPrefix:     "oncall:cache:"
// Enabled:       true
```

### 自定义配置

```go
config := &cache.Config{
    RedisClient:   rdb,
    Logger:        logger,
    LLMTTL:        1 * time.Hour,     // 延长 LLM 缓存时间
    VectorTTL:     2 * time.Hour,     // 延长向量缓存时间
    MonitoringTTL: 1 * time.Minute,   // 缩短监控缓存时间
    KeyPrefix:     "myapp:cache:",    // 自定义前缀
    Enabled:       true,
}
```

## 性能优化

### 1. 缓存命中率监控

```go
// 在应用中记录缓存命中率
var (
    cacheHits   int64
    cacheMisses int64
)

_, err := llmCache.Get(ctx, key)
if err == cache.ErrCacheMiss {
    atomic.AddInt64(&cacheMisses, 1)
} else {
    atomic.AddInt64(&cacheHits, 1)
}

// 计算命中率
hitRate := float64(cacheHits) / float64(cacheHits + cacheMisses)
```

### 2. 预热缓存

```go
// 系统启动时预热常用查询
func warmupCache(ctx context.Context, llmCache *cache.LLMCache) {
    commonQueries := []string{
        "如何排查 CPU 高的问题？",
        "如何排查内存泄漏？",
        "如何排查网络故障？",
    }

    for _, query := range commonQueries {
        key := &cache.LLMCacheKey{
            AgentID:   "knowledge_agent",
            SessionID: "warmup",
            Messages:  []*schema.Message{{Content: query}},
            Model:     "claude-opus-4-6",
        }

        response := callLLM(query)
        llmCache.Set(ctx, key, response)
    }
}
```

### 3. 批量操作

```go
// 批量清理过期缓存
func cleanupExpiredCache(ctx context.Context, manager *cache.Manager) {
    // 按模式删除
    manager.DeleteByPattern(ctx, "llm:*:expired_session:*")
}
```

## 最佳实践

### 1. 始终使用 Agent 和会话隔离

```go
// ✅ 正确：包含 AgentID 和 SessionID
key := &cache.LLMCacheKey{
    AgentID:   agentID,
    SessionID: sessionID,
    Messages:  messages,
}

// ❌ 错误：缺少隔离信息
key := &cache.LLMCacheKey{
    Messages: messages,
}
```

### 2. 合理设置 TTL

```go
// 根据数据特性设置 TTL
// - LLM 响应：30 分钟（相对稳定）
// - 向量检索：1 小时（知识库更新不频繁）
// - 监控数据：5 分钟（实时性要求高）
```

### 3. 处理缓存失败

```go
cached, err := llmCache.Get(ctx, key)
if err != nil {
    if err == cache.ErrCacheMiss {
        // 缓存未命中，正常流程
        response = callLLM(messages)
        llmCache.Set(ctx, key, response)
    } else {
        // 缓存错误，记录日志但不影响业务
        logger.Error("cache error", zap.Error(err))
        response = callLLM(messages)
    }
}
```

### 4. 定期清理

```go
// 定期清理过期会话的缓存
func cleanupOldSessions(ctx context.Context, manager *cache.Manager) {
    ticker := time.NewTicker(1 * time.Hour)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            // 清理 24 小时前的会话
            oldSessions := getExpiredSessions()
            for _, sessionID := range oldSessions {
                manager.InvalidateSession(ctx, sessionID)
            }
        }
    }
}
```

## 故障排查

### 问题 1：缓存未命中

```go
// 检查缓存键是否正确
key := &cache.CacheKey{
    Type:      cache.CacheTypeLLM,
    AgentID:   agentID,
    SessionID: sessionID,
    Key:       cache.GenerateKeyHash(data),
}

exists, _ := manager.Exists(ctx, key)
fmt.Printf("缓存是否存在: %v\n", exists)
```

### 问题 2：缓存隔离失效

```go
// 验证 Agent 隔离
policy := cache.NewAgentIsolationPolicy(manager)
err := policy.ValidateIsolation(ctx, agentID)
if err != nil {
    log.Printf("隔离验证失败: %v", err)
}
```

### 问题 3：Redis 连接问题

```go
// 测试 Redis 连接
ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
defer cancel()

if err := rdb.Ping(ctx).Err(); err != nil {
    log.Fatal("Redis 连接失败:", err)
}
```

## 测试

```bash
# 运行所有缓存测试
go test ./test -run "TestCache" -v

# 运行特定测试
go test ./test -run TestLLMCache -v
go test ./test -run TestVectorCache -v
go test ./test -run TestMonitoringCache -v
go test ./test -run TestEvictionPolicy -v
```

## 注意事项

1. **Redis 依赖**：缓存模块依赖 Redis，确保 Redis 服务可用
2. **内存管理**：合理设置 TTL，避免缓存占用过多内存
3. **并发安全**：所有缓存操作都是并发安全的
4. **错误处理**：缓存失败不应影响主业务流程
5. **监控告警**：监控缓存命中率和 Redis 性能

## 未来优化

- [ ] 支持本地内存缓存（两级缓存）
- [ ] 支持缓存预热策略
- [ ] 支持缓存压缩
- [ ] 支持缓存版本控制
- [ ] 添加更多监控指标
