# 自愈循环架构设计

## 概述

自愈循环（Self-Healing Loop）是一个自动化的故障检测、诊断、修复和学习系统。它能够在无人工干预的情况下，自动发现问题、分析根因、执行修复、验证结果，并从中学习。

## 架构设计

### 核心流程

```
┌─────────────────────────────────────────────────────────────┐
│                      自愈循环 (Healing Loop)                  │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
        ┌──────────────────────────────────────┐
        │  1. 监控 (Monitor)                    │
        │  - Prometheus 指标监控                │
        │  - K8s 事件监控                       │
        │  - 日志异常监控                       │
        │  - 健康检查                           │
        └──────────────┬───────────────────────┘
                       │ 检测到异常
                       ▼
        ┌──────────────────────────────────────┐
        │  2. 检测 (Detect)                     │
        │  - 异常阈值判断                       │
        │  - 模式匹配                           │
        │  - 趋势分析                           │
        │  - 告警聚合                           │
        └──────────────┬───────────────────────┘
                       │ 确认故障
                       ▼
        ┌──────────────────────────────────────┐
        │  3. 诊断 (Diagnose)                   │
        │  - RCA Agent 根因分析                 │
        │  - 依赖图分析                         │
        │  - 历史案例匹配                       │
        │  - 影响范围评估                       │
        └──────────────┬───────────────────────┘
                       │ 生成诊断报告
                       ▼
        ┌──────────────────────────────────────┐
        │  4. 决策 (Decide)                     │
        │  - Strategy Agent 策略评估            │
        │  - 风险评估                           │
        │  - 修复方案选择                       │
        │  - 回滚计划生成                       │
        └──────────────┬───────────────────────┘
                       │ 选择修复方案
                       ▼
        ┌──────────────────────────────────────┐
        │  5. 执行 (Execute)                    │
        │  - Execution Agent 执行修复           │
        │  - 沙箱验证                           │
        │  - 分阶段执行                         │
        │  - 实时监控                           │
        └──────────────┬───────────────────────┘
                       │ 执行完成
                       ▼
        ┌──────────────────────────────────────┐
        │  6. 验证 (Verify)                     │
        │  - 健康检查                           │
        │  - 指标对比                           │
        │  - 功能测试                           │
        │  - 用户影响评估                       │
        └──────────────┬───────────────────────┘
                       │
                ┌──────┴──────┐
                │             │
            验证成功      验证失败
                │             │
                ▼             ▼
        ┌───────────┐  ┌──────────────┐
        │ 7. 学习    │  │ 8. 回滚       │
        │ (Learn)   │  │ (Rollback)   │
        └───────────┘  └──────┬───────┘
                              │
                              ▼
                        重试或升级
```

## 组件设计

### 1. HealingLoop Manager

**职责**: 协调整个自愈流程

**文件**: `internal/healing/manager.go`

```go
type HealingLoopManager struct {
    monitor    *Monitor
    detector   *Detector
    diagnoser  *Diagnoser
    decider    *Decider
    executor   *Executor
    verifier   *Verifier
    learner    *Learner

    // 配置
    config     *HealingConfig

    // 状态管理
    activeLoops map[string]*HealingSession
    mutex       sync.RWMutex

    // 依赖
    logger     *zap.Logger
    cbManager  *concurrent.CircuitBreakerManager
}

type HealingSession struct {
    ID          string
    StartTime   time.Time
    State       HealingState
    Incident    *Incident
    Diagnosis   *DiagnosisResult
    Decision    *DecisionResult
    Execution   *ExecutionResult
    Verification *VerificationResult
    RetryCount  int
    MaxRetries  int
}

type HealingState string

const (
    StateMonitoring  HealingState = "monitoring"
    StateDetecting   HealingState = "detecting"
    StateDiagnosing  HealingState = "diagnosing"
    StateDeciding    HealingState = "deciding"
    StateExecuting   HealingState = "executing"
    StateVerifying   HealingState = "verifying"
    StateLearning    HealingState = "learning"
    StateRollingBack HealingState = "rolling_back"
    StateCompleted   HealingState = "completed"
    StateFailed      HealingState = "failed"
)
```

### 2. Monitor（监控）

**职责**: 持续监控系统状态，发现异常

**文件**: `internal/healing/monitor.go`

```go
type Monitor struct {
    // 数据源
    promClient *prometheus.Client
    k8sClient  *kubernetes.Clientset
    esClient   *elasticsearch.Client

    // 监控规则
    rules      []*MonitorRule

    // 事件通道
    eventChan  chan *MonitorEvent

    logger     *zap.Logger
}

type MonitorRule struct {
    Name        string
    Type        MonitorType  // Metric, Log, Event
    Query       string
    Threshold   float64
    Duration    time.Duration
    Severity    Severity
}

type MonitorEvent struct {
    ID          string
    Timestamp   time.Time
    Type        MonitorType
    Severity    Severity
    Source      string
    Message     string
    Labels      map[string]string
    Value       float64
}
```

### 3. Detector（检测）

**职责**: 分析监控事件，确认故障

**文件**: `internal/healing/detector.go`

```go
type Detector struct {
    // 检测策略
    strategies []DetectionStrategy

    // 告警聚合
    aggregator *AlertAggregator

    logger     *zap.Logger
}

type DetectionStrategy interface {
    Detect(events []*MonitorEvent) (*Incident, error)
}

type Incident struct {
    ID          string
    Timestamp   time.Time
    Severity    Severity
    Type        IncidentType
    Title       string
    Description string
    Events      []*MonitorEvent
    AffectedResources []Resource
}
```

### 4. Diagnoser（诊断）

**职责**: 使用 RCA Agent 进行根因分析

**文件**: `internal/healing/diagnoser.go`

```go
type Diagnoser struct {
    rcaAgent   adk.ResumableAgent
    opsAgent   adk.ResumableAgent

    // 历史案例库
    caseDB     *CaseDatabase

    logger     *zap.Logger
}

type DiagnosisResult struct {
    IncidentID  string
    RootCause   string
    Confidence  float64
    Evidence    []Evidence
    ImpactScope ImpactScope
    SimilarCases []*HistoricalCase
}
```

### 5. Decider（决策）

**职责**: 使用 Strategy Agent 选择修复方案

**文件**: `internal/healing/decider.go`

```go
type Decider struct {
    strategyAgent adk.ResumableAgent

    // 策略库
    strategies    []*HealingStrategy

    // 风险评估器
    riskAssessor  *RiskAssessor

    logger        *zap.Logger
}

type DecisionResult struct {
    IncidentID     string
    Strategy       *HealingStrategy
    RiskLevel      RiskLevel
    RollbackPlan   *RollbackPlan
    EstimatedTime  time.Duration
    RequiresApproval bool
}

type HealingStrategy struct {
    ID          string
    Name        string
    Type        StrategyType
    Actions     []Action
    Conditions  []Condition
    Priority    int
}
```

### 6. Executor（执行）

**职责**: 使用 Execution Agent 执行修复

**文件**: `internal/healing/executor.go`

```go
type Executor struct {
    executionAgent adk.ResumableAgent

    // 执行器
    k8sExecutor    *K8sExecutor
    scriptExecutor *ScriptExecutor

    // 熔断器
    cbManager      *concurrent.CircuitBreakerManager

    logger         *zap.Logger
}

type ExecutionResult struct {
    IncidentID    string
    StartTime     time.Time
    EndTime       time.Time
    Status        ExecutionStatus
    Actions       []*ActionResult
    Logs          []string
    Errors        []error
}
```

### 7. Verifier（验证）

**职责**: 验证修复效果

**文件**: `internal/healing/verifier.go`

```go
type Verifier struct {
    // 验证器
    healthChecker  *HealthChecker
    metricComparer *MetricComparer

    // Ops Agent（用于查询状态）
    opsAgent       adk.ResumableAgent

    logger         *zap.Logger
}

type VerificationResult struct {
    IncidentID     string
    Success        bool
    HealthStatus   HealthStatus
    MetricsBefore  map[string]float64
    MetricsAfter   map[string]float64
    Improvements   map[string]float64
    FailureReasons []string
}
```

### 8. Learner（学习）

**职责**: 从成功/失败案例中学习

**文件**: `internal/healing/learner.go`

```go
type Learner struct {
    // 知识库
    knowledgeAgent adk.ResumableAgent

    // 案例数据库
    caseDB         *CaseDatabase

    // 向量存储
    vectorStore    *milvus.Client

    logger         *zap.Logger
}

type LearningResult struct {
    IncidentID     string
    Success        bool
    Lessons        []Lesson
    UpdatedStrategies []*HealingStrategy
    KnowledgeUpdated  bool
}

type Lesson struct {
    Type        LessonType
    Content     string
    Confidence  float64
    Applicability []string
}
```

## 数据模型

### 案例数据库

```go
type HistoricalCase struct {
    ID              string
    Timestamp       time.Time
    Incident        *Incident
    Diagnosis       *DiagnosisResult
    Decision        *DecisionResult
    Execution       *ExecutionResult
    Verification    *VerificationResult
    Success         bool
    Duration        time.Duration
    Embedding       []float32  // 向量化表示
}
```

### 配置模型

```go
type HealingConfig struct {
    // 自动触发
    AutoTrigger     bool
    TriggerRules    []*TriggerRule

    // 重试策略
    MaxRetries      int
    RetryDelay      time.Duration
    BackoffMultiplier float64

    // 审批策略
    RequireApproval bool
    ApprovalTimeout time.Duration

    // 学习策略
    EnableLearning  bool
    MinConfidence   float64

    // 监控配置
    MonitorInterval time.Duration
    DetectionWindow time.Duration
}

type TriggerRule struct {
    Name        string
    Conditions  []Condition
    Severity    Severity
    AutoHeal    bool
}
```

## 实现计划

### Phase 1: 基础框架（2天）

1. ✅ 创建目录结构
2. ✅ 实现 HealingLoopManager
3. ✅ 实现基础数据模型
4. ✅ 实现状态机

### Phase 2: 监控和检测（1天）

1. ✅ 实现 Monitor 组件
2. ✅ 实现 Detector 组件
3. ✅ 集成 Prometheus/K8s/ES
4. ✅ 实现告警聚合

### Phase 3: 诊断和决策（1天）

1. ✅ 集成 RCA Agent
2. ✅ 集成 Strategy Agent
3. ✅ 实现风险评估
4. ✅ 实现策略选择

### Phase 4: 执行和验证（1天）

1. ✅ 集成 Execution Agent
2. ✅ 实现验证逻辑
3. ✅ 实现回滚机制
4. ✅ 实现重试逻辑

### Phase 5: 学习和优化（1天）

1. ✅ 实现案例存储
2. ✅ 实现知识更新
3. ✅ 实现策略优化
4. ✅ 集成向量检索

### Phase 6: 测试和文档（1天）

1. ✅ 单元测试
2. ✅ 集成测试
3. ✅ 端到端测试
4. ✅ 文档编写

## 使用示例

### 自动触发

```go
// 启动自愈循环管理器
manager := healing.NewHealingLoopManager(&healing.HealingConfig{
    AutoTrigger:     true,
    MaxRetries:      3,
    EnableLearning:  true,
    MonitorInterval: 30 * time.Second,
})

// 启动监控
go manager.Start(ctx)

// 系统会自动检测、诊断、修复故障
```

### 手动触发

```go
// 手动创建故障事件
incident := &healing.Incident{
    Type:        healing.IncidentTypePodCrashLoop,
    Severity:    healing.SeverityHigh,
    Description: "Pod nginx-deployment-xxx is in CrashLoopBackOff",
}

// 触发自愈流程
session, err := manager.TriggerHealing(ctx, incident)
if err != nil {
    log.Fatal(err)
}

// 等待完成
result := <-session.ResultChan
if result.Success {
    log.Info("Healing completed successfully")
} else {
    log.Error("Healing failed", zap.Error(result.Error))
}
```

### 查询状态

```go
// 查询所有活跃的自愈会话
sessions := manager.GetActiveSessions()

// 查询历史案例
cases, err := manager.GetHistoricalCases(&healing.CaseQuery{
    IncidentType: healing.IncidentTypePodCrashLoop,
    Success:      true,
    Limit:        10,
})
```

## 监控指标

### Prometheus 指标

```
# 自愈循环总数
healing_loop_total{state="completed|failed"}

# 自愈成功率
healing_loop_success_rate

# 平均修复时间
healing_loop_duration_seconds{state="completed"}

# 重试次数
healing_loop_retries_total

# 当前活跃会话数
healing_loop_active_sessions
```

## 配置示例

```yaml
# manifest/config/config.yaml
healing:
  # 自动触发
  auto_trigger: true
  monitor_interval: "30s"
  detection_window: "5m"

  # 重试策略
  max_retries: 3
  retry_delay: "30s"
  backoff_multiplier: 2.0

  # 审批策略
  require_approval: false
  approval_timeout: "5m"

  # 学习策略
  enable_learning: true
  min_confidence: 0.7

  # 触发规则
  trigger_rules:
    - name: "pod_crash_loop"
      conditions:
        - type: "metric"
          query: "kube_pod_container_status_restarts_total > 5"
          duration: "5m"
      severity: "high"
      auto_heal: true

    - name: "high_cpu"
      conditions:
        - type: "metric"
          query: "container_cpu_usage_seconds_total > 0.8"
          duration: "10m"
      severity: "medium"
      auto_heal: true
```

## 安全考虑

1. **权限控制**: 执行操作需要适当的 RBAC 权限
2. **审计日志**: 所有操作记录到审计日志
3. **回滚机制**: 每个操作都有对应的回滚计划
4. **沙箱验证**: 高风险操作先在沙箱环境验证
5. **人工审批**: 高风险操作需要人工审批

## 总结

自愈循环是一个完整的自动化运维系统，能够：

1. **自动发现**: 持续监控，自动发现异常
2. **智能诊断**: 使用 AI 进行根因分析
3. **自动修复**: 选择最佳策略并执行
4. **验证效果**: 确保修复成功
5. **持续学习**: 从经验中学习，不断优化

通过这个系统，可以大幅降低 MTTR（平均修复时间），提升系统可用性。
