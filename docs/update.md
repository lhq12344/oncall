# Oncall 系统重构设计方案
## 基于 Eino ADK 的企业级多 Agent 自主运维系统

---

## 一、系统架构概览

### 1.1 设计目标

将现有的 oncall 系统从单一 RAG 聊天升级为具备以下能力的自主运维平台：

- **智能意图理解**：预测用户下一步问题，主动引导故障排查
- **多维监控分析**：整合 K8s、Prometheus、日志、链路追踪的多维信号
- **自动化执行**：生成并执行运维脚本，具备回滚与验证能力
- **知识进化**：从成功案例中学习，持续优化故障处理策略
- **上下文隔离**：支持多会话并发，保证 Agent 间状态隔离

### 1.2 技术栈

- **框架**：GoFrame + Eino ADK (Cloudwego AI 编排框架)
- **LLM**：DeepSeek V3 (via Volcengine Ark API)
- **向量数据库**：Milvus (RAG 检索 + 意图预测)
- **存储**：Redis (热数据) + PostgreSQL (冷数据) + sync.Map (内存缓存)
- **监控**：Prometheus + K8s API + 日志分析
- **前端**：Vanilla JS (保持现有架构)

---

## 二、核心 Agent 架构设计

基于 Eino ADK 的多 Agent 协同系统，采用分层设计：

### 2.1 Supervisor Agent（总控代理）

**模式**: Supervisor 模式

**职责**:
- 接收用户输入，理解全局意图
- 路由请求到合适的子 Agent
- 协调多 Agent 协作（串行/并行/递归）
- 管理会话上下文生命周期
- 聚合子 Agent 结果并返回用户

**实现**: 基于 `SupervisorAgent` + 自定义路由策略

**工具集**:
- `IntentClassifier`（意图分类）：识别用户请求类型（监控查询/故障诊断/知识检索/执行操作）
- `ContextManager`（上下文管理）：管理会话状态、Agent 状态、执行上下文
- `AgentRouter`（代理路由）：根据意图和当前状态选择合适的子 Agent
- `ResultAggregator`（结果聚合）：合并多个 Agent 的输出

### 2.2 Knowledge Agent（知识库代理）

**模式**: ChatModelAgent + RAG

**职责**:
- 检索历史故障案例（基于语义相似度）
- 提供最佳实践建议和解决方案
- 知识沉淀与更新（成功案例自动入库）
- 知识质量评估与剪枝

**实现**: 复用现有 `chat_pipeline` + 增强的 Milvus 检索

**工具集**:
- `VectorSearch`（向量检索）：基于 Milvus 的语义搜索，支持混合检索（向量+关键词）
- `KnowledgeIndexer`（知识索引）：文档分块、向量化、入库（复用 `knowledge_index_pipeline`）
- `CaseRanker`（案例排序）：根据相似度、时效性、成功率排序
- `KnowledgeFeedback`（知识反馈）：收集用户反馈，更新案例权重
- `SuccessPathExtractor`（成功路径提取）：从执行日志中提取可复用的解决方案

### 2.3 Dialogue Agent（对话代理）

**模式**: DeepAgents 模式（多轮深度推理）

**职责**:
- 理解用户意图并预测下一步问题
- 提供交互式问题引导（主动推送候选问题）
- 维护对话状态机（意图收敛度跟踪）
- 生成澄清性问题（当意图模糊时）

**实现**: 基于 Eino 的 `ReactAgent` + 自定义意图预测模块

**工具集**:
- `IntentPredictor`（意图预测）：
  - 使用滑动窗口维护最近 N 轮对话向量
  - 基于余弦相似度检索历史成功路径
  - 计算语义熵判断意图收敛度（熵降低 → 意图明确）
  - 异步预生成候选问题（不阻塞主流程）
- `QuestionGenerator`（问题生成）：根据当前上下文生成 3-5 个候选问题
- `SemanticAnalyzer`（语义分析）：提取关键实体（服务名、指标名、时间范围）
- `ContextEntropyCalculator`（上下文熵计算）：量化对话的不确定性
- `DialogueStateTracker`（对话状态跟踪）：维护多轮对话的状态转移

**意图预测示例**:

| 故障类别 | 当前监控信号 | 典型后续问题 A | 典型后续问题 B | 概率置信度 |
|---------|------------|--------------|--------------|----------|
| 数据库连接溢出 | max_connections 告警 | 慢查询日志分析 | 活跃连接分布检查 | 0.88 |
| 内存泄漏 (OOM) | container_memory_usage | 堆内存转储请求 | GC 频率与耗时分析 | 0.75 |
| 网络分区 | io_timeout 增加 | 下游依赖熔断状态 | 跨机房丢包率检测 | 0.92 |

### 2.4 Ops Agent（运维代理）

**模式**: Plan-Execute 模式

**职责**:
- 监控系统状态（K8s、服务、资源）
- 生成诊断报告（多维信号聚合）
- 执行运维操作（查询、重启、扩容）
- 验证执行结果

**实现**: 基于 Eino 的 `PlanExecuteAgent` + 工具集成

**工具集**:
- `K8sMonitor`（K8s 监控）：
  - Pod 状态查询（Running/Pending/Failed）
  - 资源使用率（CPU/Memory/Disk）
  - 事件日志（kubectl events）
  - HPA 状态（自动扩缩容）
- `ServiceHealthChecker`（服务健康检查）：
  - HTTP 健康检查（/health, /ready）
  - gRPC 健康检查
  - 依赖服务连通性测试
- `MetricsCollector`（指标采集）：
  - Prometheus 查询（PromQL）
  - 动态阈值计算（滑动窗口 + 标准差）
  - 异常评分（偏离基准线 k 倍标准差）
- `LogAnalyzer`（日志分析）：
  - 错误日志聚合（按时间窗口）
  - 关键字匹配（OOM、Timeout、Connection refused）
  - 日志频率异常检测
- `TraceAnalyzer`（链路追踪分析）：
  - 慢请求分析（P99 延迟）
  - 调用链路可视化
  - 上下游依赖识别
- `AnomalyDetector`（异常检测）：
  - 基于统计学的异常检测（$|X_t - \mu_t| > k \cdot \sigma_t$）
  - 多维信号关联分析（CPU + 延迟 + 错误率）

**动态阈值算法**:
```
Anomaly = |X_t - μ_t| > k · σ_t
其中：
- X_t: 当前指标值
- μ_t: 滑动窗口均值
- σ_t: 滑动窗口标准差
- k: 敏感度系数（通常取 2-3）
```

### 2.5 Execution Agent（执行代理）

**模式**: WorkflowAgents（工作流编排）

**职责**:
- 生成结构化执行计划（JSON 格式）
- 沙盒环境执行脚本（隔离风险）
- 回滚与验证（失败自动回滚）
- 执行日志记录（审计追踪）

**实现**: 基于 Eino 的 `WorkflowAgent` + 内置终端

**工具集**:
- `ScriptGenerator`（脚本生成）：
  - LLM 生成 Shell/Python 脚本
  - 结构化执行计划（Pre-check → Action → Post-check）
  - 命令白名单校验（禁止 `rm -rf /`, `DROP TABLE` 等危险操作）
- `SandboxExecutor`（沙盒执行器）：
  - 基于 `os/exec` + `creack/pty` 的交互式终端
  - 实时捕获 stdout/stderr
  - 超时控制（context.WithTimeout）
  - 资源限制（CPU/Memory）
- `RollbackManager`（回滚管理）：
  - 维护回滚栈（每个操作的逆操作）
  - 自动回滚失败步骤
  - 幂等性检查（避免重复执行）
- `ExecutionValidator`（执行验证）：
  - 验证执行结果（检查服务状态、指标恢复）
  - 生成执行报告（成功/失败/部分成功）
- `CommandWhitelist`（命令白名单）：
  - 预定义安全命令列表
  - 语义审查（防止命令注入）

**执行计划接口**:
```go
type TaskStep interface {
    Execute(ctx context.Context) error    // 执行核心动作
    Rollback(ctx context.Context) error   // 回滚操作
    Validate(ctx context.Context) (bool, error) // 验证结果
}
```

**自愈闭环**:
```
执行失败 → 捕获错误 → LLM 分析 → 生成修正计划 → 重新执行
示例：docker restart 失败（权限不足）→ 自动添加 sudo → 重试
```

### 2.6 RCA Agent（根因分析代理）

**模式**: Plan-Execute 模式

**职责**:
- 构建服务依赖图（DAG）
- 多维信号关联分析（Metrics + Logs + Traces）
- 根因定位与推理（反向搜索调用链）
- 影响范围评估

**实现**: 基于 Eino 的 `PlanExecuteAgent` + 图算法

**工具集**:
- `DependencyGraphBuilder`（依赖图构建）：
  - 从配置文件/服务注册中心构建依赖图
  - 动态更新依赖关系
  - 支持多层级依赖（直接依赖 + 间接依赖）
- `SignalCorrelator`（信号关联器）：
  - 时间窗口对齐（将不同来源的信号对齐到同一时间轴）
  - 因果关系推断（A 先发生 → B 后发生 → A 可能导致 B）
  - 相关性评分（Pearson 相关系数）
- `RootCauseInference`（根因推理）：
  - 沿调用链反向搜索（从故障节点向上游追溯）
  - 识别最先发生异常的节点
  - 生成根因假设（Top 3 可能原因）
- `ImpactAnalyzer`（影响分析）：
  - 评估故障影响范围（受影响的下游服务）
  - 预测故障扩散路径
  - 生成影响报告

**根因分析流程**:
```
1. 收集告警信号（CPU 高、延迟增加、错误率上升）
2. 构建依赖图（识别上下游服务）
3. 时间窗口对齐（对齐不同来源的信号）
4. 反向搜索（从故障节点向上游追溯）
5. 生成根因假设（排序 Top 3）
6. 验证假设（通过日志/指标验证）
```

### 2.7 Strategy Agent（策略代理）

**模式**: ChatModelAgent

**职责**:
- 评估执行策略质量（成功率、执行时长、回滚次数）
- 策略优化建议（识别低效步骤）
- 知识库更新决策（哪些案例值得保存）
- 知识剪枝（删除冗余/低质量知识）

**实现**: 基于 Eino 的 `ChatModelAgent` + 评分算法

**工具集**:
- `StrategyEvaluator`（策略评估）：
  - 成功率评分（解决问题的成功率）
  - 效率评分（执行时长、步骤数）
  - 稳定性评分（回滚次数、失败次数）
  - 综合评分（加权平均）
- `KnowledgePruner`（知识剪枝）：
  - 识别冗余知识（相似度 > 0.95 的案例）
  - 删除低质量知识（评分 < 阈值）
  - 定期清理过期知识（时效性衰减）
- `SuccessPathExtractor`（成功路径提取）：
  - 从执行日志中提取关键步骤
  - 生成可复用的模板
  - 向量化并存入知识库

**知识质量评分公式**:
```
Score = w1 · SuccessRate + w2 · (1 - NormalizedTime) + w3 · (1 - RollbackRate) + w4 · UserFeedback
其中：
- w1, w2, w3, w4: 权重系数（和为 1）
- SuccessRate: 成功率（0-1）
- NormalizedTime: 归一化执行时长（0-1）
- RollbackRate: 回滚率（0-1）
- UserFeedback: 用户评分（0-1）
```

---

## 三、上下文隔离与管理设计

### 3.1 分层上下文架构

```
GlobalContext（全局上下文）
├── SessionContext（会话上下文）
│   ├── SessionID（会话唯一标识）
│   ├── UserID（用户标识）
│   ├── ConversationHistory（对话历史）
│   ├── UserIntent（用户意图）
│   └── SessionMetadata（会话元数据：创建时间、最后活跃时间）
├── AgentContext（Agent 上下文）
│   ├── AgentID（Agent 唯一标识）
│   ├── AgentState（Agent 状态：idle/running/waiting）
│   ├── ToolCallHistory（工具调用历史）
│   ├── IntermediateResults（中间结果）
│   └── ErrorStack（错误栈）
└── ExecutionContext（执行上下文）
    ├── TaskQueue（任务队列）
    ├── ExecutionLog（执行日志）
    ├── RollbackStack（回滚栈）
    └── ValidationResults（验证结果）
```

### 3.2 上下文隔离策略

#### 3.2.1 会话级隔离
- 每个用户会话分配唯一的 `SessionID`（UUID）
- 使用 Go `context.Context` 的 `WithValue` 机制传递 SessionID
- 不同会话的上下文完全隔离，互不干扰

#### 3.2.2 Agent 级隔离
- 每个 Agent 实例维护独立的状态
- 使用 `sync.RWMutex` 保护 Agent 内部状态
- Agent 间通过消息传递通信（不共享内存）

#### 3.2.3 工具级隔离
- 工具调用时传递独立的 `ToolContext`
- 工具执行结果存储在独立的命名空间
- 支持工具并发调用（goroutine pool）

#### 3.2.4 并发安全保证
```go
// 上下文管理器示例
type ContextManager struct {
    sessions sync.Map // SessionID -> *SessionContext
    mu       sync.RWMutex
}

func (cm *ContextManager) GetSession(sessionID string) (*SessionContext, error) {
    if val, ok := cm.sessions.Load(sessionID); ok {
        return val.(*SessionContext), nil
    }
    return nil, ErrSessionNotFound
}

func (cm *ContextManager) CreateSession(userID string) *SessionContext {
    session := &SessionContext{
        SessionID:   uuid.New().String(),
        UserID:      userID,
        CreatedAt:   time.Now(),
        LastActive:  time.Now(),
        History:     make([]*Message, 0),
        mu:          sync.RWMutex{},
    }
    cm.sessions.Store(session.SessionID, session)
    return session
}
```

### 3.3 上下文存储策略（冷热分离）

| 层级 | 存储介质 | TTL | 访问延迟 | 用途 | 优化策略 |
|------|---------|-----|---------|------|---------|
| **L1 热数据** | Go sync.Map | 30min | < 1ms | 当前活跃会话 | 零拷贝读取、对象池化 |
| **L2 温数据** | Redis | 24h | 10-50ms | 近期会话快速恢复 | Pipeline 批量操作 |
| **L3 向量数据** | Milvus | 30d | 50-200ms | 语义检索与意图预测 | 并发索引构建 |
| **L4 冷数据** | PostgreSQL | 永久 | > 100ms | 审计与历史回溯 | 列式压缩存储 |

#### 3.3.1 热数据管理（L1）
- **存储内容**：最近 5-10 轮对话、当前 Agent 状态、执行中的任务
- **优化策略**：
  - 使用 `sync.Pool` 预分配 Context 结构体，减少 GC 压力
  - 使用 `[]byte` 代替 `string`，避免字符串逃逸到堆
  - 使用 `unsafe.Pointer` 进行无拷贝转换（谨慎使用）
- **淘汰策略**：LRU（最近最少使用），30 分钟无活动自动迁移到 L2

#### 3.3.2 温数据管理（L2）
- **存储内容**：过去 24 小时的会话历史、用户偏好设置
- **优化策略**：
  - 使用 Redis Pipeline 批量读写，减少网络往返
  - 使用 Redis Hash 存储结构化数据，减少序列化开销
  - 设置合理的过期时间（TTL），自动清理过期数据
- **迁移策略**：定时任务（每 5 分钟）扫描 L1，将不活跃会话迁移到 L2

#### 3.3.3 向量数据管理（L3）
- **存储内容**：对话向量、知识库向量、意图预测索引
- **优化策略**：
  - 使用 Milvus 的混合检索（向量 + 标量过滤）
  - 并发构建索引（多个 goroutine 并行处理）
  - 定期重建索引（优化查询性能）
- **查询优化**：使用 LRU Cache 缓存热门查询结果

#### 3.3.4 冷数据管理（L4）
- **存储内容**：历史会话、审计日志、执行记录
- **优化策略**：
  - 使用 PostgreSQL 的分区表（按时间分区）
  - 使用列式压缩（zstd/snappy）减少存储空间
  - 定期归档到对象存储（S3/MinIO）
- **查询优化**：使用全文索引（GIN）加速关键字搜索

#### 3.3.5 数据迁移流程
```go
// 定时任务：L1 → L2 迁移
func (cm *ContextManager) MigrateToL2(ctx context.Context) {
    cm.sessions.Range(func(key, value interface{}) bool {
        session := value.(*SessionContext)
        if time.Since(session.LastActive) > 30*time.Minute {
            // 序列化并存入 Redis
            data, _ := json.Marshal(session)
            cm.redis.Set(ctx, "session:"+session.SessionID, data, 24*time.Hour)
            // 从内存中删除
            cm.sessions.Delete(key)
        }
        return true
    })
}

// 定时任务：L2 → L3/L4 迁移
func (cm *ContextManager) MigrateToL3L4(ctx context.Context) {
    // 扫描 Redis 中过期的会话
    keys, _ := cm.redis.Keys(ctx, "session:*").Result()
    for _, key := range keys {
        ttl, _ := cm.redis.TTL(ctx, key).Result()
        if ttl < 1*time.Hour {
            // 提取对话向量存入 Milvus（L3）
            session := cm.loadSessionFromRedis(ctx, key)
            cm.indexToMilvus(ctx, session)
            // 完整数据存入 PostgreSQL（L4）
            cm.archiveToPostgres(ctx, session)
            // 从 Redis 删除
            cm.redis.Del(ctx, key)
        }
    }
}
```

---

## 四、Agent 间协作模式

### 4.1 串行协作（诊断流程）

**适用场景**：故障诊断与修复（需要按顺序执行）

**流程**:
```
用户输入 → Supervisor → Dialogue（澄清意图）→ Ops（收集监控数据）→ RCA（根因分析）→ Knowledge（检索历史案例）→ Execution（执行修复）→ Strategy（评估结果）→ 返回用户
```

**实现**:
```go
func (s *SupervisorAgent) HandleDiagnosticFlow(ctx context.Context, input string) (string, error) {
    // 1. Dialogue: 澄清意图
    intent, err := s.dialogueAgent.ClarifyIntent(ctx, input)
    if err != nil {
        return "", err
    }

    // 2. Ops: 收集监控数据
    metrics, err := s.opsAgent.CollectMetrics(ctx, intent)
    if err != nil {
        return "", err
    }

    // 3. RCA: 根因分析
    rootCause, err := s.rcaAgent.Analyze(ctx, metrics)
    if err != nil {
        return "", err
    }

    // 4. Knowledge: 检索历史案例
    cases, err := s.knowledgeAgent.Search(ctx, rootCause)
    if err != nil {
        return "", err
    }

    // 5. Execution: 执行修复
    result, err := s.executionAgent.Execute(ctx, cases[0].Solution)
    if err != nil {
        return "", err
    }

    // 6. Strategy: 评估结果
    evaluation, err := s.strategyAgent.Evaluate(ctx, result)
    if err != nil {
        return "", err
    }

    return s.formatResponse(evaluation), nil
}
```

### 4.2 并行协作（信息收集）

**适用场景**：多维度信息收集（可以并行执行）

**流程**:
```
用户输入 → Supervisor → [Knowledge, Ops, RCA] 并行执行 → 结果聚合 → Dialogue（生成回复）→ 返回用户
```

**实现**:
```go
func (s *SupervisorAgent) HandleParallelCollection(ctx context.Context, input string) (string, error) {
    var wg sync.WaitGroup
    results := make(chan AgentResult, 3)

    // 并行执行三个 Agent
    wg.Add(3)

    // Knowledge Agent
    go func() {
        defer wg.Done()
        knowledge, err := s.knowledgeAgent.Search(ctx, input)
        results <- AgentResult{Type: "knowledge", Data: knowledge, Error: err}
    }()

    // Ops Agent
    go func() {
        defer wg.Done()
        metrics, err := s.opsAgent.CollectMetrics(ctx, input)
        results <- AgentResult{Type: "ops", Data: metrics, Error: err}
    }()

    // RCA Agent
    go func() {
        defer wg.Done()
        analysis, err := s.rcaAgent.QuickAnalyze(ctx, input)
        results <- AgentResult{Type: "rca", Data: analysis, Error: err}
    }()

    // 等待所有 Agent 完成
    go func() {
        wg.Wait()
        close(results)
    }()

    // 聚合结果
    aggregated := s.aggregateResults(results)

    // Dialogue Agent 生成回复
    response, err := s.dialogueAgent.GenerateResponse(ctx, aggregated)
    if err != nil {
        return "", err
    }

    return response, nil
}
```

### 4.3 递归协作（自愈循环）

**适用场景**：执行失败后自动重试（带修正）

**流程**:
```
Execution（执行）→ 验证失败 → RCA（分析失败原因）→ Dialogue（生成修正建议）→ Execution（重新执行）→ 验证成功 → 结束
```

**实现**:
```go
func (s *SupervisorAgent) HandleSelfHealingLoop(ctx context.Context, plan *ExecutionPlan) error {
    maxRetries := 3
    for i := 0; i < maxRetries; i++ {
        // 执行计划
        result, err := s.executionAgent.Execute(ctx, plan)
        if err == nil && result.Success {
            // 执行成功，退出循环
            return nil
        }

        // 执行失败，分析原因
        analysis, err := s.rcaAgent.AnalyzeFailure(ctx, result)
        if err != nil {
            return fmt.Errorf("RCA failed: %w", err)
        }

        // 生成修正计划
        correctedPlan, err := s.dialogueAgent.GenerateCorrectedPlan(ctx, plan, analysis)
        if err != nil {
            return fmt.Errorf("plan correction failed: %w", err)
        }

        // 更新计划，准备重试
        plan = correctedPlan
        log.Printf("Retry %d/%d with corrected plan", i+1, maxRetries)
    }

    return fmt.Errorf("self-healing failed after %d retries", maxRetries)
}
```

### 4.4 混合协作（复杂场景）

**适用场景**：复杂故障处理（串行 + 并行 + 递归）

**流程**:
```
1. Dialogue 澄清意图（串行）
2. [Knowledge, Ops, RCA] 并行收集信息（并行）
3. Execution 执行修复（串行）
4. 如果失败，进入自愈循环（递归）
5. Strategy 评估并更新知识库（串行）
```

---

## 五、工具设计（Tools as Agents）

### 5.1 工具封装原则

每个 Agent 可以被封装为 Tool 供其他 Agent 调用，遵循 Eino 的 `compose.ToolInfo` 接口：

```go
type Tool interface {
    Info() *compose.ToolInfo
    InvokableRun(ctx context.Context, input string) (string, error)
}
```

### 5.2 示例：将 Knowledge Agent 封装为 Tool

```go
// KnowledgeSearchTool 将 Knowledge Agent 封装为工具
type KnowledgeSearchTool struct {
    agent *KnowledgeAgent
}

func NewKnowledgeSearchTool(agent *KnowledgeAgent) *KnowledgeSearchTool {
    return &KnowledgeSearchTool{agent: agent}
}

func (t *KnowledgeSearchTool) Info() *compose.ToolInfo {
    return &compose.ToolInfo{
        Name: "knowledge_search",
        Desc: "搜索历史故障案例和最佳实践。输入：故障描述或关键词。输出：相关案例列表（包含解决方案）。",
        ParamsOneOf: compose.NewParamsOneOfByParams(
            map[string]*compose.ParameterInfo{
                "query": {
                    Type:     "string",
                    Desc:     "搜索关键词或故障描述",
                    Required: true,
                },
                "top_k": {
                    Type:     "integer",
                    Desc:     "返回结果数量（默认 5）",
                    Required: false,
                },
            },
        ),
    }
}

func (t *KnowledgeSearchTool) InvokableRun(ctx context.Context, input string) (string, error) {
    // 解析输入参数
    var params struct {
        Query string `json:"query"`
        TopK  int    `json:"top_k"`
    }
    if err := json.Unmarshal([]byte(input), &params); err != nil {
        return "", err
    }
    if params.TopK == 0 {
        params.TopK = 5
    }

    // 调用 Knowledge Agent
    cases, err := t.agent.Search(ctx, params.Query, params.TopK)
    if err != nil {
        return "", err
    }

    // 格式化输出
    output, err := json.Marshal(cases)
    if err != nil {
        return "", err
    }

    return string(output), nil
}
```

### 5.3 工具注册与使用

```go
// 在 Supervisor Agent 中注册工具
func (s *SupervisorAgent) RegisterTools() {
    s.tools = []compose.Tool{
        NewKnowledgeSearchTool(s.knowledgeAgent),
        NewOpsMonitorTool(s.opsAgent),
        NewRCAAnalyzeTool(s.rcaAgent),
        NewExecutionTool(s.executionAgent),
    }
}

// 在 Eino 的 ReactAgent 中使用工具
func (s *SupervisorAgent) CreateReactAgent() *agent.ReactAgent {
    return agent.NewReactAgent(&agent.ReactAgentConfig{
        Model:  s.llmClient,
        Tools:  s.tools,
        MaxIterations: 10,
    })
}
```

---

## 六、关键技术实现点

### 6.1 意图预测机制

#### 6.1.1 核心思路
通过分析对话历史，预测用户下一步可能提出的问题，主动推送候选问题，加速故障排查流程。

#### 6.1.2 实现步骤

**步骤 1：对话向量化**
```go
// 使用 Doubao Embedding 将对话转换为向量
func (d *DialogueAgent) vectorizeConversation(ctx context.Context, messages []*Message) ([]float32, error) {
    // 拼接最近 N 轮对话
    text := d.concatenateMessages(messages, 5)

    // 调用 Embedding API
    embedding, err := d.embeddingClient.Embed(ctx, text)
    if err != nil {
        return nil, err
    }

    return embedding, nil
}
```

**步骤 2：语义熵计算**
```go
// 计算对话的语义熵（衡量不确定性）
func (d *DialogueAgent) calculateSemanticEntropy(ctx context.Context, messages []*Message) (float64, error) {
    if len(messages) < 2 {
        return 1.0, nil // 初始状态，熵最大
    }

    // 计算最近两轮对话的向量相似度
    vec1, _ := d.vectorizeConversation(ctx, messages[:len(messages)-1])
    vec2, _ := d.vectorizeConversation(ctx, messages)

    similarity := cosineSimilarity(vec1, vec2)

    // 相似度越高，熵越低（意图越明确）
    entropy := 1.0 - similarity

    return entropy, nil
}
```

**步骤 3：检索历史成功路径**
```go
// 基于当前对话向量，检索历史相似场景
func (d *DialogueAgent) retrieveSimilarPaths(ctx context.Context, currentVector []float32) ([]*HistoricalPath, error) {
    // 在 Milvus 中检索 Top 10 相似对话
    results, err := d.milvusClient.Search(ctx, &milvus.SearchRequest{
        CollectionName: "conversation_paths",
        Vector:         currentVector,
        TopK:           10,
        MetricType:     "COSINE",
    })
    if err != nil {
        return nil, err
    }

    // 提取成功路径（只保留最终解决问题的对话）
    paths := make([]*HistoricalPath, 0)
    for _, result := range results {
        if result.Metadata["success"] == "true" {
            paths = append(paths, &HistoricalPath{
                ID:          result.ID,
                Similarity:  result.Score,
                NextQuestions: result.Metadata["next_questions"].([]string),
            })
        }
    }

    return paths, nil
}
```

**步骤 4：生成候选问题**
```go
// 异步生成候选问题（不阻塞主流程）
func (d *DialogueAgent) predictNextQuestions(ctx context.Context, sessionID string) {
    go func() {
        // 获取当前会话
        session, err := d.contextManager.GetSession(sessionID)
        if err != nil {
            return
        }

        // 计算语义熵
        entropy, err := d.calculateSemanticEntropy(ctx, session.History)
        if err != nil {
            return
        }

        // 如果熵低于阈值，说明意图已收敛，开始预测
        if entropy < 0.3 {
            // 向量化当前对话
            vector, err := d.vectorizeConversation(ctx, session.History)
            if err != nil {
                return
            }

            // 检索历史路径
            paths, err := d.retrieveSimilarPaths(ctx, vector)
            if err != nil {
                return
            }

            // 提取候选问题（取 Top 3）
            candidates := make([]string, 0)
            for i := 0; i < 3 && i < len(paths); i++ {
                candidates = append(candidates, paths[i].NextQuestions...)
            }

            // 去重并排序
            candidates = d.deduplicateAndRank(candidates)

            // 存入会话上下文
            session.PredictedQuestions = candidates[:min(3, len(candidates))]
        }
    }()
}
```

#### 6.1.3 前端展示

在前端展示预测的候选问题，用户可以点击快速提问：

```javascript
// 前端代码示例
function displayPredictedQuestions(questions) {
    const container = document.getElementById('predicted-questions');
    container.innerHTML = '<h4>您可能想问：</h4>';

    questions.forEach(q => {
        const button = document.createElement('button');
        button.textContent = q;
        button.onclick = () => sendMessage(q);
        container.appendChild(button);
    });
}
```

### 6.2 动态监控判断

#### 6.2.1 滑动窗口动态阈值算法

```go
// 动态阈值检测器
type DynamicThresholdDetector struct {
    windowSize int           // 滑动窗口大小（如 100 个数据点）
    k          float64       // 敏感度系数（通常取 2-3）
    buffer     *ring.Ring    // 环形缓冲区
}

func NewDynamicThresholdDetector(windowSize int, k float64) *DynamicThresholdDetector {
    return &DynamicThresholdDetector{
        windowSize: windowSize,
        k:          k,
        buffer:     ring.New(windowSize),
    }
}

// 检测异常
func (d *DynamicThresholdDetector) Detect(value float64) (bool, float64) {
    // 添加新数据点
    d.buffer.Value = value
    d.buffer = d.buffer.Next()

    // 计算均值和标准差
    mean, stddev := d.calculateStats()

    // 判断是否异常
    deviation := math.Abs(value - mean)
    threshold := d.k * stddev

    isAnomaly := deviation > threshold
    score := deviation / threshold // 异常评分

    return isAnomaly, score
}

// 计算统计量
func (d *DynamicThresholdDetector) calculateStats() (mean, stddev float64) {
    var sum, sumSq float64
    count := 0

    d.buffer.Do(func(v interface{}) {
        if v != nil {
            val := v.(float64)
            sum += val
            sumSq += val * val
            count++
        }
    })

    if count == 0 {
        return 0, 0
    }

    mean = sum / float64(count)
    variance := (sumSq / float64(count)) - (mean * mean)
    stddev = math.Sqrt(variance)

    return mean, stddev
}
```

#### 6.2.2 多维信号聚合

```go
// 多维信号聚合器
type SignalAggregator struct {
    detectors map[string]*DynamicThresholdDetector
}

func NewSignalAggregator() *SignalAggregator {
    return &SignalAggregator{
        detectors: map[string]*DynamicThresholdDetector{
            "cpu":       NewDynamicThresholdDetector(100, 2.5),
            "memory":    NewDynamicThresholdDetector(100, 2.5),
            "latency":   NewDynamicThresholdDetector(100, 3.0),
            "error_rate": NewDynamicThresholdDetector(100, 3.0),
        },
    }
}

// 聚合检测
func (a *SignalAggregator) AggregateDetect(signals map[string]float64) *AnomalyReport {
    report := &AnomalyReport{
        Timestamp: time.Now(),
        Anomalies: make(map[string]float64),
    }

    totalScore := 0.0
    anomalyCount := 0

    for metric, value := range signals {
        detector, ok := a.detectors[metric]
        if !ok {
            continue
        }

        isAnomaly, score := detector.Detect(value)
        if isAnomaly {
            report.Anomalies[metric] = score
            totalScore += score
            anomalyCount++
        }
    }

    // 计算综合异常评分
    if anomalyCount > 0 {
        report.OverallScore = totalScore / float64(anomalyCount)
        report.Severity = a.calculateSeverity(report.OverallScore, anomalyCount)
    }

    return report
}

// 计算严重程度
func (a *SignalAggregator) calculateSeverity(score float64, count int) string {
    // 综合考虑异常评分和异常指标数量
    if score > 2.0 && count >= 3 {
        return "critical"
    } else if score > 1.5 || count >= 2 {
        return "warning"
    } else {
        return "info"
    }
}
```

### 6.3 执行计划生成

#### 6.3.1 结构化执行计划

```go
// 执行计划接口
type TaskStep interface {
    Execute(ctx context.Context) error
    Rollback(ctx context.Context) error
    Validate(ctx context.Context) (bool, error)
    GetDescription() string
}

// 执行计划
type ExecutionPlan struct {
    ID          string
    Description string
    Steps       []TaskStep
    CreatedAt   time.Time
}

// 示例：重启服务的执行步骤
type RestartServiceStep struct {
    ServiceName string
    Namespace   string
    k8sClient   *kubernetes.Clientset
}

func (s *RestartServiceStep) Execute(ctx context.Context) error {
    // 执行重启
    return s.k8sClient.CoreV1().Pods(s.Namespace).Delete(ctx, s.ServiceName, metav1.DeleteOptions{})
}

func (s *RestartServiceStep) Rollback(ctx context.Context) error {
    // 重启操作无法回滚，但可以记录日志
    log.Printf("Cannot rollback restart operation for %s", s.ServiceName)
    return nil
}

func (s *RestartServiceStep) Validate(ctx context.Context) (bool, error) {
    // 验证服务是否恢复
    pod, err := s.k8sClient.CoreV1().Pods(s.Namespace).Get(ctx, s.ServiceName, metav1.GetOptions{})
    if err != nil {
        return false, err
    }

    return pod.Status.Phase == corev1.PodRunning, nil
}

func (s *RestartServiceStep) GetDescription() string {
    return fmt.Sprintf("重启服务 %s (namespace: %s)", s.ServiceName, s.Namespace)
}
```

#### 6.3.2 LLM 生成执行计划

```go
// 使用 LLM 生成执行计划
func (e *ExecutionAgent) GeneratePlan(ctx context.Context, problem string) (*ExecutionPlan, error) {
    prompt := fmt.Sprintf(`
你是一个运维专家。根据以下问题描述，生成一个结构化的执行计划。

问题描述：%s

请以 JSON 格式返回执行计划，包含以下字段：
{
  "description": "计划描述",
  "steps": [
    {
      "type": "check|execute|validate",
      "action": "具体操作",
      "command": "执行的命令（如果有）",
      "expected_result": "预期结果"
    }
  ]
}
`, problem)

    // 调用 LLM
    response, err := e.llmClient.Chat(ctx, prompt)
    if err != nil {
        return nil, err
    }

    // 解析 JSON
    var planJSON struct {
        Description string `json:"description"`
        Steps       []struct {
            Type           string `json:"type"`
            Action         string `json:"action"`
            Command        string `json:"command"`
            ExpectedResult string `json:"expected_result"`
        } `json:"steps"`
    }

    if err := json.Unmarshal([]byte(response), &planJSON); err != nil {
        return nil, err
    }

    // 转换为 ExecutionPlan
    plan := &ExecutionPlan{
        ID:          uuid.New().String(),
        Description: planJSON.Description,
        Steps:       make([]TaskStep, 0),
        CreatedAt:   time.Now(),
    }

    for _, stepJSON := range planJSON.Steps {
        step := e.createTaskStep(stepJSON.Type, stepJSON.Action, stepJSON.Command)
        plan.Steps = append(plan.Steps, step)
    }

    return plan, nil
}
```

#### 6.3.3 命令白名单校验

```go
// 命令白名单
var commandWhitelist = map[string]bool{
    "kubectl":     true,
    "docker":      true,
    "systemctl":   true,
    "curl":        true,
    "ping":        true,
    "netstat":     true,
    "ps":          true,
    "top":         true,
    "free":        true,
    "df":          true,
}

// 危险命令黑名单
var commandBlacklist = []string{
    "rm -rf /",
    "DROP TABLE",
    "DELETE FROM",
    "mkfs",
    "dd if=/dev/zero",
}

// 校验命令安全性
func (e *ExecutionAgent) validateCommand(command string) error {
    // 检查黑名单
    for _, dangerous := range commandBlacklist {
        if strings.Contains(command, dangerous) {
            return fmt.Errorf("dangerous command detected: %s", dangerous)
        }
    }

    // 检查白名单
    parts := strings.Fields(command)
    if len(parts) == 0 {
        return fmt.Errorf("empty command")
    }

    baseCommand := parts[0]
    if !commandWhitelist[baseCommand] {
        return fmt.Errorf("command not in whitelist: %s", baseCommand)
    }

    return nil
}
```

### 6.4 知识进化机制

#### 6.4.1 成功路径提取

```go
// 从执行日志中提取成功路径
func (s *StrategyAgent) ExtractSuccessPath(ctx context.Context, executionLog *ExecutionLog) (*KnowledgeEntry, error) {
    if !executionLog.Success {
        return nil, fmt.Errorf("execution failed, cannot extract success path")
    }

    // 提取关键信息
    entry := &KnowledgeEntry{
        ID:          uuid.New().String(),
        Problem:     executionLog.Problem,
        Solution:    executionLog.Plan.Description,
        Steps:       make([]string, 0),
        Metrics:     executionLog.Metrics,
        ExecutionTime: executionLog.Duration,
        SuccessRate: 1.0, // 初始成功率
        CreatedAt:   time.Now(),
    }

    // 提取执行步骤
    for _, step := range executionLog.Plan.Steps {
        entry.Steps = append(entry.Steps, step.GetDescription())
    }

    // 向量化
    vector, err := s.embeddingClient.Embed(ctx, entry.Problem+" "+entry.Solution)
    if err != nil {
        return nil, err
    }
    entry.Vector = vector

    return entry, nil
}
```

#### 6.4.2 知识质量评分

```go
// 计算知识质量评分
func (s *StrategyAgent) CalculateQualityScore(entry *KnowledgeEntry) float64 {
    // 权重系数
    w1, w2, w3, w4 := 0.4, 0.2, 0.2, 0.2

    // 成功率评分（0-1）
    successScore := entry.SuccessRate

    // 时间评分（归一化，越快越好）
    maxTime := 3600.0 // 1 小时
    timeScore := 1.0 - math.Min(entry.ExecutionTime.Seconds()/maxTime, 1.0)

    // 回滚率评分（越低越好）
    rollbackScore := 1.0 - entry.RollbackRate

    // 用户反馈评分（0-1）
    feedbackScore := entry.UserFeedback

    // 综合评分
    score := w1*successScore + w2*timeScore + w3*rollbackScore + w4*feedbackScore

    return score
}
```

#### 6.4.3 知识剪枝

```go
// 定期剪枝低质量知识
func (s *StrategyAgent) PruneKnowledge(ctx context.Context) error {
    // 查询所有知识条目
    entries, err := s.knowledgeRepo.ListAll(ctx)
    if err != nil {
        return err
    }

    toDelete := make([]string, 0)

    for _, entry := range entries {
        // 计算质量评分
        score := s.CalculateQualityScore(entry)

        // 评分低于阈值，标记删除
        if score < 0.5 {
            toDelete = append(toDelete, entry.ID)
            continue
        }

        // 检查是否有高度相似的条目
        similar, err := s.findSimilarEntries(ctx, entry, 0.95)
        if err != nil {
            continue
        }

        // 如果有相似条目且评分更高，删除当前条目
        for _, sim := range similar {
            if s.CalculateQualityScore(sim) > score {
                toDelete = append(toDelete, entry.ID)
                break
            }
        }
    }

    // 批量删除
    return s.knowledgeRepo.DeleteBatch(ctx, toDelete)
}
```

---

## 七、项目结构设计

```
oncall/
├── main.go                          # 主入口
├── go.mod                           # Go 模块定义
├── go.sum
├── Makefile                         # 构建脚本
├── manifest/
│   ├── config/
│   │   └── config.yaml             # 配置文件
│   ├── docker/
│   │   └── docker-compose.yml      # 基础设施编排
│   └── k8s/                        # K8s 部署配置
│       ├── deployment.yaml
│       └── service.yaml
├── api/                            # API 定义（GoFrame 规范）
│   └── chat/
│       └── v1/
│           ├── chat.go             # 聊天接口
│           ├── agent.go            # Agent 管理接口
│           └── ops.go              # 运维操作接口
├── internal/
│   ├── controller/                 # HTTP 控制器（自动生成）
│   │   └── chat/
│   │       ├── chat.go
│   │       ├── agent.go
│   │       └── ops.go
│   ├── logic/                      # 业务逻辑层
│   │   ├── chat/
│   │   │   ├── chat.go            # 聊天逻辑
│   │   │   └── stream.go          # SSE 流式响应
│   │   └── agent/
│   │       └── orchestrator.go    # Agent 编排逻辑
│   ├── agent/                      # Agent 实现（核心）
│   │   ├── supervisor/             # Supervisor Agent
│   │   │   ├── supervisor.go
│   │   │   ├── router.go          # 路由策略
│   │   │   └── aggregator.go      # 结果聚合
│   │   ├── knowledge/              # Knowledge Agent
│   │   │   ├── knowledge.go
│   │   │   ├── rag.go             # RAG 检索
│   │   │   └── indexer.go         # 知识索引
│   │   ├── dialogue/               # Dialogue Agent
│   │   │   ├── dialogue.go
│   │   │   ├── intent.go          # 意图预测
│   │   │   ├── entropy.go         # 语义熵计算
│   │   │   └── question_gen.go    # 问题生成
│   │   ├── ops/                    # Ops Agent
│   │   │   ├── ops.go
│   │   │   ├── monitor.go         # 监控采集
│   │   │   ├── anomaly.go         # 异常检测
│   │   │   └── threshold.go       # 动态阈值
│   │   ├── execution/              # Execution Agent
│   │   │   ├── execution.go
│   │   │   ├── plan.go            # 执行计划
│   │   │   ├── sandbox.go         # 沙盒执行
│   │   │   ├── rollback.go        # 回滚管理
│   │   │   └── whitelist.go       # 命令白名单
│   │   ├── rca/                    # RCA Agent
│   │   │   ├── rca.go
│   │   │   ├── graph.go           # 依赖图
│   │   │   ├── correlate.go       # 信号关联
│   │   │   └── inference.go       # 根因推理
│   │   └── strategy/               # Strategy Agent
│   │       ├── strategy.go
│   │       ├── evaluate.go        # 策略评估
│   │       ├── prune.go           # 知识剪枝
│   │       └── extract.go         # 成功路径提取
│   ├── tool/                       # 工具集（Tools as Agents）
│   │   ├── k8s/                    # K8s 工具
│   │   │   ├── monitor.go         # Pod/Node 监控
│   │   │   ├── health.go          # 健康检查
│   │   │   └── operator.go        # 操作工具（重启/扩容）
│   │   ├── monitor/                # 监控工具
│   │   │   ├── prometheus.go      # Prometheus 查询
│   │   │   ├── log.go             # 日志分析
│   │   │   └── trace.go           # 链路追踪
│   │   ├── executor/               # 执行工具
│   │   │   ├── shell.go           # Shell 执行
│   │   │   ├── python.go          # Python 执行
│   │   │   └── validator.go       # 执行验证
│   │   └── vector/                 # 向量检索工具
│   │       ├── milvus.go          # Milvus 客户端
│   │       ├── search.go          # 向量搜索
│   │       └── index.go           # 索引管理
│   ├── context/                    # 上下文管理
│   │   ├── manager.go             # 上下文管理器
│   │   ├── session.go             # 会话上下文
│   │   ├── agent.go               # Agent 上下文
│   │   ├── execution.go           # 执行上下文
│   │   ├── storage.go             # 存储层抽象
│   │   ├── isolation.go           # 隔离机制
│   │   └── migration.go           # 数据迁移
│   ├── workflow/                   # 工作流编排
│   │   ├── orchestrator.go        # 编排器
│   │   ├── pipeline.go            # 执行管道
│   │   ├── serial.go              # 串行协作
│   │   ├── parallel.go            # 并行协作
│   │   └── recursive.go           # 递归协作
│   ├── model/                      # 数据模型
│   │   ├── intent.go              # 意图模型
│   │   ├── plan.go                # 执行计划模型
│   │   ├── knowledge.go           # 知识模型
│   │   ├── message.go             # 消息模型
│   │   └── metric.go              # 指标模型
│   ├── dao/                        # 数据访问层（自动生成）
│   │   ├── knowledge.go
│   │   ├── execution_log.go
│   │   └── session.go
│   └── service/                    # 服务层接口（自动生成）
│       └── agent.go
├── utility/                        # 工具库
│   ├── mem/                        # 内存管理
│   │   └── redis_memory.go        # Redis 会话存储
│   ├── mysql/                      # MySQL 客户端
│   │   └── client.go
│   ├── logger/                     # 日志
│   │   └── zap.go
│   └── middleware/                 # 中间件
│       ├── cors.go
│       └── response.go
├── Front_page/                     # 前端（保持现有）
│   ├── index.html
│   ├── app.js
│   └── start.sh
└── docs/                           # 文档
    ├── ARCHITECTURE.md             # 架构文档
    ├── API.md                      # API 文档
    └── DEPLOYMENT.md               # 部署文档
```

### 7.1 目录说明

#### 7.1.1 核心目录

- **`internal/agent/`**: 所有 Agent 的实现，每个 Agent 一个子目录
- **`internal/tool/`**: 工具集实现，按功能分类（k8s/monitor/executor/vector）
- **`internal/context/`**: 上下文管理，包含隔离、存储、迁移逻辑
- **`internal/workflow/`**: 工作流编排，实现串行/并行/递归协作模式

#### 7.1.2 生成目录

- **`internal/controller/`**: 由 `make ctrl` 自动生成
- **`internal/dao/`**: 由 `make dao` 自动生成
- **`internal/service/`**: 由 `make service` 自动生成

#### 7.1.3 配置目录

- **`manifest/config/`**: 运行时配置（LLM API、数据库连接等）
- **`manifest/docker/`**: 本地开发环境（Milvus、Redis、Prometheus）
- **`manifest/k8s/`**: 生产环境部署配置

---

## 八、实施路线图

### Phase 1: 基础框架（2 周）

**目标**: 搭建 Eino ADK 基础架构，实现上下文管理

**任务**:
1. **项目初始化**
   - 创建目录结构
   - 配置 Go 模块依赖（Eino、GoFrame、Milvus 客户端）
   - 配置 Docker Compose（Milvus、Redis、PostgreSQL）

2. **上下文管理器**
   - 实现 `ContextManager`（会话创建、查询、删除）
   - 实现分层存储（L1-L4）
   - 实现数据迁移逻辑（定时任务）

3. **Supervisor Agent**
   - 实现基础路由逻辑（意图分类 → Agent 选择）
   - 实现结果聚合器
   - 集成 Eino 的 `SupervisorAgent`

4. **测试**
   - 单元测试：上下文管理器
   - 集成测试：Supervisor 路由逻辑

**交付物**:
- 可运行的基础框架
- Supervisor Agent 能够路由到占位符子 Agent

---

### Phase 2: 核心 Agent（3 周）

**目标**: 实现 Knowledge、Dialogue、Ops 三个核心 Agent

**任务**:
1. **Knowledge Agent**
   - 复用现有 `chat_pipeline` 和 `knowledge_index_pipeline`
   - 实现 `VectorSearch` 工具（Milvus 检索）
   - 实现 `CaseRanker`（案例排序）
   - 封装为 Tool（`KnowledgeSearchTool`）

2. **Dialogue Agent**
   - 实现意图预测模块（`IntentPredictor`）
   - 实现语义熵计算（`ContextEntropyCalculator`）
   - 实现问题生成器（`QuestionGenerator`）
   - 异步预测候选问题（goroutine）

3. **Ops Agent**
   - 实现 K8s 监控工具（`K8sMonitor`）
   - 实现 Prometheus 查询工具（`MetricsCollector`）
   - 实现动态阈值检测器（`DynamicThresholdDetector`）
   - 实现多维信号聚合器（`SignalAggregator`）

4. **工具集成**
   - 将三个 Agent 封装为 Tool
   - 在 Supervisor 中注册工具
   - 实现串行协作流程（Dialogue → Ops → Knowledge）

5. **测试**
   - 单元测试：每个 Agent 的核心功能
   - 集成测试：端到端故障诊断流程

**交付物**:
- 可用的 Knowledge、Dialogue、Ops Agent
- 支持基础的故障诊断流程

---

### Phase 3: 执行与自愈（3 周）

**目标**: 实现 Execution、RCA Agent，完成自愈闭环

**任务**:
1. **Execution Agent**
   - 实现执行计划生成器（`ScriptGenerator`）
   - 实现沙盒执行器（`SandboxExecutor`，基于 `os/exec` + `creack/pty`）
   - 实现回滚管理器（`RollbackManager`）
   - 实现命令白名单校验（`CommandWhitelist`）

2. **RCA Agent**
   - 实现依赖图构建器（`DependencyGraphBuilder`）
   - 实现信号关联器（`SignalCorrelator`）
   - 实现根因推理器（`RootCauseInference`）
   - 实现影响分析器（`ImpactAnalyzer`）

3. **自愈闭环**
   - 实现递归协作模式（Execution → 验证失败 → RCA → 重新规划 → Execution）
   - 实现失败重试逻辑（最多 3 次）
   - 实现执行日志记录

4. **工作流编排**
   - 实现串行协作（`SerialWorkflow`）
   - 实现并行协作（`ParallelWorkflow`）
   - 实现递归协作（`RecursiveWorkflow`）

5. **测试**
   - 单元测试：执行计划生成、沙盒执行、回滚
   - 集成测试：完整的自愈流程（模拟故障 → 自动修复）

**交付物**:
- 可用的 Execution、RCA Agent
- 支持自愈闭环的完整系统

---

### Phase 4: 优化与进化（2 周）

**目标**: 实现 Strategy Agent，完善知识进化机制

**任务**:
1. **Strategy Agent**
   - 实现策略评估器（`StrategyEvaluator`）
   - 实现知识剪枝器（`KnowledgePruner`）
   - 实现成功路径提取器（`SuccessPathExtractor`）

2. **知识进化**
   - 实现成功案例自动入库
   - 实现知识质量评分
   - 实现定期剪枝任务（cron job）

3. **性能优化**
   - 优化上下文存储（减少序列化开销）
   - 优化向量检索（使用 LRU Cache）
   - 优化并发控制（Worker Pool）

4. **可观测性**
   - 集成 Prometheus 指标（Agent 调用次数、延迟、成功率）
   - 集成分布式追踪（OpenTelemetry）
   - 完善日志记录（结构化日志）

5. **文档**
   - 编写架构文档（`ARCHITECTURE.md`）
   - 编写 API 文档（`API.md`）
   - 编写部署文档（`DEPLOYMENT.md`）

6. **测试**
   - 压力测试：并发会话处理能力
   - 长时间运行测试：内存泄漏检测
   - 端到端测试：完整的故障处理流程

**交付物**:
- 完整的企业级 Oncall 系统
- 完善的文档和测试

---

## 九、技术选型与依赖

### 9.1 核心依赖

```go
// go.mod
module go_agent

go 1.21

require (
    github.com/gogf/gf/v2 v2.5.0                    // GoFrame 框架
    github.com/cloudwego/eino v0.1.0                // Eino ADK
    github.com/milvus-io/milvus-sdk-go/v2 v2.3.0    // Milvus 客户端
    github.com/go-redis/redis/v8 v8.11.5            // Redis 客户端
    gorm.io/gorm v1.25.0                            // GORM ORM
    gorm.io/driver/postgres v1.5.0                  // PostgreSQL 驱动
    k8s.io/client-go v0.28.0                        // K8s 客户端
    github.com/prometheus/client_golang v1.16.0     // Prometheus 客户端
    github.com/creack/pty v1.1.18                   // PTY（伪终端）
    github.com/google/uuid v1.3.0                   // UUID 生成
    go.uber.org/zap v1.24.0                         // 日志库
    github.com/robfig/cron/v3 v3.0.1                // 定时任务
)
```

### 9.2 基础设施

| 组件 | 版本 | 用途 |
|------|------|------|
| Milvus | 2.3+ | 向量数据库（RAG 检索、意图预测） |
| Redis | 7.0+ | 会话缓存、分布式锁 |
| PostgreSQL | 15+ | 持久化存储（审计日志、知识库） |
| Prometheus | 2.40+ | 指标采集与监控 |
| MinIO | RELEASE.2023+ | 对象存储（文件上传） |
| Kubernetes | 1.28+ | 容器编排（生产环境） |

### 9.3 LLM 服务

- **主模型**: DeepSeek V3 (via Volcengine Ark API)
- **Embedding**: Doubao Embedding (via Volcengine Ark API)
- **备选**: 支持 OpenAI 兼容接口的其他模型

---

## 十、关键设计决策

### 10.1 为什么选择 Eino ADK？

1. **原生支持多 Agent 模式**: Supervisor、Plan-Execute、DeepAgents 等模式开箱即用
2. **工具集成简单**: 统一的 `compose.Tool` 接口，易于扩展
3. **与 GoFrame 兼容**: 都是 Go 生态，集成无缝
4. **性能优越**: Go 语言的高并发能力，适合企业级应用

### 10.2 为什么采用分层上下文？

1. **隔离性**: 不同层级的上下文互不干扰，避免状态污染
2. **可扩展性**: 新增 Agent 时只需扩展 AgentContext，不影响其他层
3. **性能**: 冷热分离减少内存占用，提高查询效率

### 10.3 为什么需要自愈闭环？

1. **减少人工干预**: 自动重试和修正，提高故障恢复速度
2. **知识积累**: 每次失败都是学习机会，持续优化策略
3. **可靠性**: 多次重试机制提高成功率

### 10.4 为什么使用沙盒执行？

1. **安全性**: 隔离执行环境，防止误操作影响宿主机
2. **可控性**: 实时捕获输出，便于调试和日志记录
3. **可回滚性**: 失败时可以快速回滚，减少影响

---

## 十一、风险与挑战

### 11.1 技术风险

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| LLM 生成的执行计划不可靠 | 高 | 命令白名单校验 + 人工审核模式 |
| 向量检索召回率低 | 中 | 混合检索（向量 + 关键词） + 定期优化索引 |
| 并发场景下上下文冲突 | 高 | 严格的隔离机制 + 并发测试 |
| 内存泄漏 | 中 | 对象池化 + 定期 GC + 监控告警 |

### 11.2 业务风险

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| 自动执行导致故障扩大 | 高 | 沙盒环境 + 回滚机制 + 人工确认模式 |
| 知识库质量下降 | 中 | 定期剪枝 + 质量评分 + 人工审核 |
| 用户不信任 AI 决策 | 中 | 透明化执行过程 + 可解释性 + 人工接管 |

### 11.3 运维风险

| 风险 | 影响 | 缓解措施 |
|------|------|---------|
| Milvus 故障导致检索失败 | 高 | 降级到关键词搜索 + 主备切换 |
| Redis 故障导致会话丢失 | 中 | 持久化到 PostgreSQL + 快速恢复 |
| LLM API 限流 | 中 | 本地缓存 + 请求队列 + 降级策略 |

---

## 十二、总结

本设计方案基于 Eino ADK 构建了一个企业级的多 Agent 自主运维系统，具备以下核心能力：

1. **智能意图理解**: 预测用户下一步问题，主动引导故障排查
2. **多维监控分析**: 整合 K8s、Prometheus、日志、链路追踪的多维信号
3. **自动化执行**: 生成并执行运维脚本，具备回滚与验证能力
4. **知识进化**: 从成功案例中学习，持续优化故障处理策略
5. **上下文隔离**: 支持多会话并发，保证 Agent 间状态隔离

通过 4 个阶段的实施（基础框架 → 核心 Agent → 执行与自愈 → 优化与进化），预计 10 周完成整个系统的开发和测试。

系统采用 Go 语言的高并发能力，结合 Eino ADK 的多 Agent 编排能力，实现了工业级的稳定性和性能。通过严格的隔离机制、命令白名单、沙盒执行等安全措施，确保系统的可靠性和安全性。

未来的演进方向包括：
- 跨团队知识共享（多租户支持）
- 更深层次的自动驾驶运维（弱信号预判）
- 多云环境支持（AWS、Azure、GCP）
- 更强的可解释性（决策过程可视化）

---

**文档版本**: v1.0
**最后更新**: 2026-03-06
**作者**: Oncall Team
