# 并发处理模块

本模块实现了 oncall 系统的并发处理功能，包括：

1. **Agent 并行调用** - 支持多个 Agent 同时执行
2. **工具并行执行** - 支持多个工具同时调用
3. **超时控制** - 防止任务无限期挂起
4. **熔断机制** - 保护系统免受级联故障影响

## 目录结构

```
internal/concurrent/
├── executor.go           # 通用并发执行器
├── circuit_breaker.go    # 熔断器实现
├── agent_executor.go     # Agent 并行执行器
├── tool_executor.go      # 工具并行执行器
└── example.go           # 使用示例
```

## 核心组件

### 1. Executor - 通用并发执行器

提供基础的并发执行能力，支持：
- 并发数限制
- 超时控制
- 错误处理

```go
executor := concurrent.NewExecutor(&concurrent.ExecutorConfig{
    MaxConcurrency: 10,           // 最大并发数
    Timeout:        30 * time.Second,  // 超时时间
    Logger:         logger,
})

tasks := []concurrent.TaskFunc{
    func(ctx context.Context) (interface{}, error) {
        // 任务 1
        return "result1", nil
    },
    func(ctx context.Context) (interface{}, error) {
        // 任务 2
        return "result2", nil
    },
}

results := executor.ExecuteParallel(ctx, tasks)
```

### 2. CircuitBreaker - 熔断器

实现熔断器模式，保护系统免受故障影响：

**三种状态：**
- `Closed` - 正常状态，请求正常通过
- `Open` - 熔断状态，直接拒绝请求
- `HalfOpen` - 半开状态，允许少量请求尝试恢复

**触发条件：**
- 连续失败次数超过阈值
- 失败率超过阈值

```go
cb := concurrent.NewCircuitBreaker(&concurrent.CircuitBreakerConfig{
    Name:              "my_service",
    MaxRequests:       1,                // 半开状态允许的请求数
    Interval:          60 * time.Second, // 统计周期
    Timeout:           60 * time.Second, // 熔断持续时间
    FailureThreshold:  5,                // 连续失败阈值
    FailureRatio:      0.5,              // 失败率阈值（50%）
    MinRequestCount:   10,               // 最小请求数
    Logger:            logger,
})

result, err := cb.Execute(ctx, func(ctx context.Context) (interface{}, error) {
    // 执行可能失败的操作
    return doSomething()
})
```

### 3. AgentExecutor - Agent 并行执行器

专门用于并行执行多个 Agent：

```go
agentExecutor := concurrent.NewAgentExecutor(&concurrent.AgentExecutorConfig{
    Executor: executor,
    CircuitBreakerManager: cbManager,
    Logger: logger,
})

tasks := []concurrent.AgentTask{
    {
        Name:  "knowledge",
        Agent: knowledgeAgent,
        Input: &schema.Message{
            Role:    schema.User,
            Content: "查询历史案例",
        },
    },
    {
        Name:  "ops",
        Agent: opsAgent,
        Input: &schema.Message{
            Role:    schema.User,
            Content: "检查系统状态",
        },
    },
}

results := agentExecutor.ExecuteAgentsParallel(ctx, tasks)
```

### 4. ToolExecutor - 工具并行执行器

专门用于并行执行多个工具：

```go
toolExecutor := concurrent.NewToolExecutor(&concurrent.ToolExecutorConfig{
    Executor: executor,
    CircuitBreakerManager: cbManager,
    Logger: logger,
})

tasks := []concurrent.ToolTask{
    {
        Name:      "k8s_monitor",
        Tool:      k8sTool,
        Arguments: `{"resource": "pods"}`,
    },
    {
        Name:      "metrics_collector",
        Tool:      metricsTool,
        Arguments: `{"query": "cpu_usage"}`,
    },
}

results := toolExecutor.ExecuteToolsParallel(ctx, tasks)
```

## 使用场景

### 场景 1: 并行查询多个数据源

当需要同时从多个数据源获取信息时：

```go
// 并行执行：K8s 监控 + Prometheus 指标 + ES 日志
tasks := []concurrent.ToolTask{
    {Name: "k8s", Tool: k8sTool, Arguments: `{"resource": "pods"}`},
    {Name: "metrics", Tool: metricsTool, Arguments: `{"query": "cpu"}`},
    {Name: "logs", Tool: logTool, Arguments: `{"level": "error"}`},
}

results := toolExecutor.ExecuteToolsParallel(ctx, tasks)
// 3 个查询并行执行，总耗时 = max(t1, t2, t3) 而不是 t1+t2+t3
```

### 场景 2: 多 Agent 协作

当多个 Agent 可以独立工作时：

```go
// 并行执行：知识检索 + 监控分析
tasks := []concurrent.AgentTask{
    {Name: "knowledge", Agent: knowledgeAgent, Input: msg1},
    {Name: "ops", Agent: opsAgent, Input: msg2},
}

results := agentExecutor.ExecuteAgentsParallel(ctx, tasks)
```

### 场景 3: 熔断保护

保护关键服务免受故障影响：

```go
// 为每个外部服务创建熔断器
k8sCB := cbManager.GetOrCreate("k8s_api", &concurrent.CircuitBreakerConfig{
    FailureThreshold: 3,
    Timeout: 30 * time.Second,
})

promCB := cbManager.GetOrCreate("prometheus", &concurrent.CircuitBreakerConfig{
    FailureThreshold: 5,
    Timeout: 60 * time.Second,
})

// 使用熔断器执行
result, err := k8sCB.Execute(ctx, func(ctx context.Context) (interface{}, error) {
    return k8sClient.GetPods()
})
```

## 性能优化

### 并发数调优

```go
// 根据任务类型调整并发数
executor := concurrent.NewExecutor(&concurrent.ExecutorConfig{
    MaxConcurrency: 10,  // CPU 密集型：CPU 核心数
                         // IO 密集型：可以更高（如 50-100）
    Timeout: 30 * time.Second,
})
```

### 超时设置

```go
// 根据任务特性设置超时
executor := concurrent.NewExecutor(&concurrent.ExecutorConfig{
    Timeout: 5 * time.Second,   // 快速查询
    // Timeout: 30 * time.Second,  // 普通操作
    // Timeout: 2 * time.Minute,   // 复杂分析
})
```

### 熔断器参数

```go
cb := concurrent.NewCircuitBreaker(&concurrent.CircuitBreakerConfig{
    FailureThreshold: 5,      // 连续失败 5 次触发熔断
    FailureRatio:     0.5,    // 或失败率超过 50%
    MinRequestCount:  10,     // 至少 10 个请求才开始统计
    Timeout:          60 * time.Second,  // 熔断 60 秒后尝试恢复
})
```

## 监控指标

建议监控以下指标：

1. **并发执行指标**
   - 任务执行时间
   - 并发任务数
   - 超时任务数

2. **熔断器指标**
   - 熔断器状态（closed/open/half-open）
   - 请求成功率
   - 熔断触发次数

3. **Agent/Tool 执行指标**
   - 各 Agent 执行时间
   - 各 Tool 调用次数
   - 错误率

## 测试

运行测试：

```bash
# 测试并发执行器
go test ./test -run TestExecutorParallel -v

# 测试熔断器
go test ./test -run TestCircuitBreaker -v

# 运行所有并发测试
go test ./test -run Test.*Concurrent -v
```

## 注意事项

1. **并发安全**：所有组件都是并发安全的，可以在多个 goroutine 中使用

2. **资源限制**：注意设置合理的并发数，避免资源耗尽

3. **超时处理**：确保所有任务都能正确响应 context 取消

4. **熔断恢复**：熔断器会自动尝试恢复，无需手动干预

5. **错误处理**：并行执行时，部分任务失败不会影响其他任务

## 未来优化

- [ ] 添加优先级队列
- [ ] 支持任务依赖关系
- [ ] 添加重试机制
- [ ] 支持动态调整并发数
- [ ] 添加更多监控指标
