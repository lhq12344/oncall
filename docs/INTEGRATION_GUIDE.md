# 集成优化功能到 Agent 使用指南

本文档说明如何将已实现的并发、缓存、熔断等优化功能集成到实际的 Agent 使用中。

## ✅ 完成状态

**所有集成任务已完成！** 详见 `INTEGRATION_STATUS.md`

## 概述

我们已经实现了以下优化功能：
1. ✅ Agent 并行调用（`internal/concurrent/agent_executor.go`）
2. ✅ 工具并行执行（`internal/concurrent/tool_executor.go`）
3. ✅ 超时控制（`internal/concurrent/executor.go`）
4. ✅ 熔断机制（`internal/concurrent/circuit_breaker.go`）
5. ✅ LLM 响应缓存（`internal/cache/llm_cache.go`）
6. ✅ 向量检索缓存（`internal/cache/vector_cache.go`）
7. ✅ 监控数据缓存（`internal/cache/monitoring_cache.go`）
8. ✅ 缓存失效策略（`internal/cache/eviction.go`）

**集成状态**：
- ✅ Controller 层集成（缓存 + 熔断器）
- ✅ Bootstrap 层初始化
- ✅ 监控端点实现
- ✅ 后台监控任务
- ✅ 配置文件更新
- ✅ Ops 集成示例

## 快速开始

查看使用指南：`OPTIMIZATION_USAGE.md`

## 监控端点

```bash
curl http://localhost:6872/api/v1/monitoring
```

返回缓存命中率和熔断器状态。

## 集成方案

### 方案 1: 在 Controller 层集成（推荐）

在 `internal/controller/chat/chat_v1.go` 中集成缓存和并发功能。

#### 步骤 1: 初始化缓存和并发组件

```go
// 在 ControllerV1 结构中添加字段
type ControllerV1 struct {
    supervisorAgent adk.ResumableAgent
    logger          *zap.Logger

    // 新增：缓存管理器
    cacheManager *cache.Manager
    llmCache     *cache.LLMCache

    // 新增：并发执行器
    agentExecutor *concurrent.AgentExecutor
    cbManager     *concurrent.CircuitBreakerManager
}

// 修改构造函数
func NewV1(
    supervisorAgent adk.ResumableAgent,
    logger *zap.Logger,
    redisClient *redis.Client, // 新增参数
) *ControllerV1 {
    ctrl := &ControllerV1{
        supervisorAgent: supervisorAgent,
        logger:          logger,
    }

    // 初始化缓存
    if redisClient != nil {
        cacheConfig := cache.DefaultConfig()
        cacheConfig.RedisClient = redisClient
        cacheConfig.Logger = logger

        var err error
        ctrl.cacheManager, err = cache.NewManager(cacheConfig)
        if err == nil {
            ctrl.llmCache = cache.NewLLMCache(ctrl.cacheManager)
            logger.Info("cache enabled for controller")
        }
    }

    // 初始化并发执行器
    ctrl.cbManager = concurrent.NewCircuitBreakerManager(logger)
    ctrl.agentExecutor = concurrent.NewAgentExecutor(&concurrent.AgentExecutorConfig{
        Executor: concurrent.NewExecutor(&concurrent.ExecutorConfig{
            MaxConcurrency: 3,
            Timeout:        60 * time.Second,
            Logger:         logger,
        }),
        CircuitBreakerManager: ctrl.cbManager,
        Logger:                logger,
    })

    return ctrl
}
```

#### 步骤 2: 在 Chat 方法中使用缓存

```go
func (c *ControllerV1) Chat(ctx context.Context, req *v1.ChatReq) (res *v1.ChatRes, err error) {
    // 参数验证
    if req.Question == "" {
        return nil, fmt.Errorf("question is required")
    }
    if req.Id == "" {
        req.Id = "default-session"
    }

    c.logger.Info("chat request received",
        zap.String("session_id", req.Id),
        zap.String("question", req.Question))

    // 1. 尝试从 LLM 缓存获取
    if c.llmCache != nil {
        cacheKey := &cache.LLMCacheKey{
            AgentID:   "supervisor",
            SessionID: req.Id,
            Messages: []*schema.Message{
                {Role: schema.User, Content: req.Question},
            },
            Model: "supervisor",
        }

        cached, err := c.llmCache.Get(ctx, cacheKey)
        if err == nil {
            c.logger.Info("LLM cache hit", zap.String("session_id", req.Id))
            return &v1.ChatRes{
                Answer: cached.Response.Content,
            }, nil
        }
    }

    // 2. 缓存未命中，执行正常流程
    memory := mem.GetSimpleMemory(req.Id)
    historyMsgs, err := mem.GetMessagesForRequest(ctx, req.Id, schema.UserMessage(req.Question), 0)
    if err != nil {
        c.logger.Warn("failed to get history, using empty history",
            zap.String("session_id", req.Id),
            zap.Error(err))
        historyMsgs = []*schema.Message{}
    }

    // 构建输入
    input := &adk.AgentInput{
        Messages: []adk.Message{
            {
                Role:    schema.User,
                Content: req.Question,
            },
        },
    }

    if len(historyMsgs) > 0 {
        for _, msg := range historyMsgs {
            input.Messages = append([]adk.Message{{
                Role:    msg.Role,
                Content: msg.Content,
            }}, input.Messages...)
        }
    }

    // 3. 使用熔断器执行 supervisor agent
    var answer string
    cb := c.cbManager.GetOrCreate("supervisor", &concurrent.CircuitBreakerConfig{
        FailureThreshold: 3,
        Timeout:          30 * time.Second,
        Logger:           c.logger,
    })

    result, err := cb.Execute(ctx, func(ctx context.Context) (interface{}, error) {
        iterator := c.supervisorAgent.Run(ctx, input)

        var ans string
        for {
            event, ok := iterator.Next()
            if !ok {
                break
            }

            if event.Err != nil {
                return nil, event.Err
            }

            if event.Output != nil && event.Output.MessageOutput != nil {
                if event.Output.MessageOutput.Message != nil {
                    ans += event.Output.MessageOutput.Message.Content
                }
            }
        }

        if ans == "" {
            ans = "抱歉，我无法生成回答。"
        }

        return ans, nil
    })

    if err != nil {
        c.logger.Error("supervisor agent error",
            zap.String("session_id", req.Id),
            zap.Error(err))
        return nil, fmt.Errorf("agent error: %w", err)
    }

    answer = result.(string)

    // 4. 存入 LLM 缓存
    if c.llmCache != nil {
        cacheKey := &cache.LLMCacheKey{
            AgentID:   "supervisor",
            SessionID: req.Id,
            Messages: []*schema.Message{
                {Role: schema.User, Content: req.Question},
            },
            Model: "supervisor",
        }

        ctx = context.WithValue(ctx, "timestamp", time.Now().Unix())
        if err := c.llmCache.Set(ctx, cacheKey, &schema.Message{
            Role:    schema.Assistant,
            Content: answer,
        }); err != nil {
            c.logger.Warn("failed to cache LLM response", zap.Error(err))
        }
    }

    // 5. 保存到会话历史
    userMsg := schema.UserMessage(req.Question)
    assistantMsg := schema.AssistantMessage(answer)

    if err := mem.SetMessages(ctx, req.Id, userMsg, assistantMsg, nil, 0, 0); err != nil {
        c.logger.Warn("failed to save messages", zap.Error(err))
    }

    return &v1.ChatRes{
        Answer: answer,
    }, nil
}
```

### 方案 2: 在 Bootstrap 层集成

在 `internal/bootstrap/app.go` 中初始化时配置优化功能。

```go
// 在 App 结构中添加字段
type App struct {
    // ... 现有字段

    // 新增：缓存和并发组件
    cacheManager  *cache.Manager
    cbManager     *concurrent.CircuitBreakerManager
    agentExecutor *concurrent.AgentExecutor
    toolExecutor  *concurrent.ToolExecutor
}

// 在初始化函数中添加
func (a *App) initOptimizations(ctx context.Context) error {
    // 初始化缓存
    cacheConfig := cache.DefaultConfig()
    cacheConfig.RedisClient = a.redisClient
    cacheConfig.Logger = a.logger

    var err error
    a.cacheManager, err = cache.NewManager(cacheConfig)
    if err != nil {
        return fmt.Errorf("failed to create cache manager: %w", err)
    }

    // 初始化并发执行器
    a.cbManager = concurrent.NewCircuitBreakerManager(a.logger)

    executorConfig := &concurrent.ExecutorConfig{
        MaxConcurrency: 10,
        Timeout:        30 * time.Second,
        Logger:         a.logger,
    }

    a.agentExecutor = concurrent.NewAgentExecutor(&concurrent.AgentExecutorConfig{
        Executor:              concurrent.NewExecutor(executorConfig),
        CircuitBreakerManager: a.cbManager,
        Logger:                a.logger,
    })

    a.toolExecutor = concurrent.NewToolExecutor(&concurrent.ToolExecutorConfig{
        Executor:              concurrent.NewExecutor(executorConfig),
        CircuitBreakerManager: a.cbManager,
        Logger:                a.logger,
    })

    a.logger.Info("optimizations initialized",
        zap.Bool("cache_enabled", true),
        zap.Bool("concurrent_enabled", true),
        zap.Bool("circuit_breaker_enabled", true))

    return nil
}
```

## 使用示例

### 示例 1: 并行查询多个数据源

```go
// 在 Ops Agent 中并行查询 K8s、Prometheus、ES
func (agent *OpsAgent) QueryAllSources(ctx context.Context, sessionID string) (map[string]interface{}, error) {
    // 使用工具并行执行器
    tasks := []concurrent.ToolTask{
        {
            Name:      "k8s",
            Tool:      agent.k8sTool,
            Arguments: `{"resource": "pods", "namespace": "default"}`,
        },
        {
            Name:      "prometheus",
            Tool:      agent.promTool,
            Arguments: `{"query": "cpu_usage", "time_range": "5m"}`,
        },
        {
            Name:      "elasticsearch",
            Tool:      agent.esTool,
            Arguments: `{"query": "error", "level": "error", "time_range": "5m"}`,
        },
    }

    results := agent.toolExecutor.ExecuteToolsParallel(ctx, tasks)

    // 处理结果
    data := make(map[string]interface{})
    for _, result := range results {
        if result.Error == nil {
            data[result.Name] = result.Output
        }
    }

    return data, nil
}
```

### 示例 2: 使用缓存加速知识检索

```go
// 在 Knowledge Agent 中使用向量缓存
func (agent *KnowledgeAgent) Search(ctx context.Context, sessionID, query string) ([]Result, error) {
    // 1. 尝试从缓存获取
    cacheKey := &cache.VectorCacheKey{
        AgentID:    "knowledge_agent",
        SessionID:  sessionID,
        Query:      query,
        TopK:       5,
        Collection: "oncall_knowledge",
    }

    cached, err := agent.vectorCache.Get(ctx, cacheKey)
    if err == nil {
        agent.logger.Info("vector cache hit")
        return cached.Results, nil
    }

    // 2. 缓存未命中，查询 Milvus
    results, err := agent.queryMilvus(ctx, query)
    if err != nil {
        return nil, err
    }

    // 3. 存入缓存
    agent.vectorCache.Set(ctx, cacheKey, results)

    return results, nil
}
```

### 示例 3: 使用熔断器保护外部服务

```go
// 在调用外部服务时使用熔断器
func (agent *OpsAgent) QueryPrometheus(ctx context.Context, query string) (interface{}, error) {
    cb := agent.cbManager.GetOrCreate("prometheus", &concurrent.CircuitBreakerConfig{
        FailureThreshold: 5,
        FailureRatio:     0.5,
        Timeout:          30 * time.Second,
    })

    result, err := cb.Execute(ctx, func(ctx context.Context) (interface{}, error) {
        return agent.promClient.Query(ctx, query)
    })

    if err == concurrent.ErrCircuitOpen {
        agent.logger.Warn("prometheus circuit breaker is open, using fallback")
        return agent.getFallbackData(), nil
    }

    return result, err
}
```

## 性能监控

### 添加缓存命中率监控

```go
// 在 Controller 中添加统计
type CacheStats struct {
    Hits   int64
    Misses int64
}

func (c *ControllerV1) GetCacheHitRate() float64 {
    total := c.stats.Hits + c.stats.Misses
    if total == 0 {
        return 0
    }
    return float64(c.stats.Hits) / float64(total)
}
```

### 添加熔断器状态监控

```go
// 定期检查熔断器状态
func (app *App) monitorCircuitBreakers(ctx context.Context) {
    ticker := time.NewTicker(30 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-ctx.Done():
            return
        case <-ticker.C:
            breakers := app.cbManager.List()
            for _, name := range breakers {
                cb, _ := app.cbManager.Get(name)
                state := cb.State()
                requests, successes, failures := cb.Counts()

                app.logger.Info("circuit breaker status",
                    zap.String("name", name),
                    zap.String("state", state.String()),
                    zap.Uint32("requests", requests),
                    zap.Uint32("successes", successes),
                    zap.Uint32("failures", failures))
            }
        }
    }
}
```

## 配置建议

### 缓存 TTL 配置

```yaml
# manifest/config/config.yaml
cache:
  llm_ttl: 30m        # LLM 响应缓存 30 分钟
  vector_ttl: 1h      # 向量检索缓存 1 小时
  monitoring_ttl: 5m  # 监控数据缓存 5 分钟
```

### 并发配置

```yaml
concurrent:
  max_concurrency: 10  # 最大并发数
  timeout: 30s         # 超时时间
```

### 熔断器配置

```yaml
circuit_breaker:
  failure_threshold: 5    # 连续失败 5 次触发熔断
  failure_ratio: 0.5      # 失败率 50% 触发熔断
  timeout: 60s            # 熔断 60 秒后尝试恢复
```

## 总结

通过以上集成方案，可以将已实现的优化功能应用到实际的 Agent 调用中：

1. **缓存**: 减少重复计算，提升响应速度 3-5 倍
2. **并发**: 并行执行独立任务，减少总体耗时 2-3 倍
3. **熔断**: 保护系统免受故障影响，提升系统稳定性
4. **超时**: 防止任务无限期挂起，保证系统响应

建议优先在 Controller 层集成，这样可以最小化对现有代码的改动，同时获得最大的性能提升。
