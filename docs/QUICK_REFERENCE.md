# 并发处理模块快速参考

## 快速导入

```go
import "go_agent/internal/concurrent"
```

## 1. 通用并发执行

```go
// 创建执行器
executor := concurrent.NewExecutor(&concurrent.ExecutorConfig{
    MaxConcurrency: 10,              // 最大并发数
    Timeout:        30 * time.Second, // 超时时间
    Logger:         logger,
})

// 定义任务
tasks := []concurrent.TaskFunc{
    func(ctx context.Context) (interface{}, error) {
        // 任务逻辑
        return result, nil
    },
}

// 执行
results := executor.ExecuteParallel(ctx, tasks)
```

## 2. Agent 并行执行

```go
// 创建 Agent 执行器
agentExecutor := concurrent.NewAgentExecutor(&concurrent.AgentExecutorConfig{
    Logger: logger,
})

// 定义任务
tasks := []concurrent.AgentTask{
    {
        Name:  "knowledge",
        Agent: knowledgeAgent,
        Input: &schema.Message{Role: schema.User, Content: "查询"},
    },
}

// 执行
results := agentExecutor.ExecuteAgentsParallel(ctx, tasks)
```

## 3. 工具并行执行

```go
// 创建工具执行器
toolExecutor := concurrent.NewToolExecutor(&concurrent.ToolExecutorConfig{
    Logger: logger,
})

// 定义任务
tasks := []concurrent.ToolTask{
    {
        Name:      "k8s_monitor",
        Tool:      k8sTool,
        Arguments: `{"resource": "pods"}`,
    },
}

// 执行
results := toolExecutor.ExecuteToolsParallel(ctx, tasks)
```

## 4. 熔断器

```go
// 创建熔断器管理器
cbManager := concurrent.NewCircuitBreakerManager(logger)

// 获取或创建熔断器
cb := cbManager.GetOrCreate("service_name", &concurrent.CircuitBreakerConfig{
    FailureThreshold: 5,              // 连续失败 5 次触发
    FailureRatio:     0.5,            // 失败率 50% 触发
    MinRequestCount:  10,             // 最少 10 个请求
    Timeout:          60 * time.Second, // 熔断 60 秒
})

// 使用熔断器执行
result, err := cb.Execute(ctx, func(ctx context.Context) (interface{}, error) {
    return doSomething()
})

// 检查状态
state := cb.State() // Closed/Open/HalfOpen
```

## 配置参数速查

### ExecutorConfig
| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| MaxConcurrency | int | 10 | 最大并发数 |
| Timeout | time.Duration | 30s | 超时时间 |
| Logger | *zap.Logger | nil | 日志记录器 |

### CircuitBreakerConfig
| 参数 | 类型 | 默认值 | 说明 |
|------|------|--------|------|
| Name | string | "default" | 熔��器名称 |
| MaxRequests | uint32 | 1 | 半开状态允许请求数 |
| Interval | time.Duration | 60s | 统计周期 |
| Timeout | time.Duration | 60s | 熔断持续时间 |
| FailureThreshold | uint32 | 5 | 连续失败阈值 |
| FailureRatio | float64 | 0.5 | 失败率阈值 |
| MinRequestCount | uint32 | 10 | 最小请求数 |

## 常见场景

### 场景 1: 并行查询多个数据源
```go
tasks := []concurrent.ToolTask{
    {Name: "k8s", Tool: k8sTool, Arguments: `{}`},
    {Name: "prom", Tool: promTool, Arguments: `{}`},
    {Name: "es", Tool: esTool, Arguments: `{}`},
}
results := toolExecutor.ExecuteToolsParallel(ctx, tasks)
```

### 场景 2: 多 Agent 协作
```go
tasks := []concurrent.AgentTask{
    {Name: "knowledge", Agent: knowledgeAgent, Input: msg1},
    {Name: "ops", Agent: opsAgent, Input: msg2},
}
results := agentExecutor.ExecuteAgentsParallel(ctx, tasks)
```

### 场景 3: 熔断保护外部服务
```go
cb := cbManager.GetOrCreate("external_api", &concurrent.CircuitBreakerConfig{
    FailureThreshold: 3,
    Timeout: 30 * time.Second,
})
result, err := cb.Execute(ctx, func(ctx context.Context) (interface{}, error) {
    return externalAPI.Call()
})
```

## 错误处理

```go
results := executor.ExecuteParallel(ctx, tasks)
for i, result := range results {
    if result.Error != nil {
        // 处理错误
        log.Printf("Task %d failed: %v", i, result.Error)
    } else {
        // 处理结果
        log.Printf("Task %d succeeded: %v", i, result.Result)
    }
}
```

## 性能调优

### CPU 密集型任务
```go
MaxConcurrency: runtime.NumCPU()
```

### IO 密集型任务
```go
MaxConcurrency: 50-100
```

### 快速查询
```go
Timeout: 5 * time.Second
```

### 复杂分析
```go
Timeout: 2 * time.Minute
```

## 测试

```bash
# 运行所有并发测试
go test ./test -run "TestExecutorParallel|TestCircuitBreaker" -v

# 运行演示程序
go run ./examples/concurrent_demo.go
```

## 文档

- 详细文档: `internal/concurrent/README.md`
- 实现总结: `docs/concurrent_implementation.md`
- 完成报告: `docs/COMPLETION_REPORT.md`
