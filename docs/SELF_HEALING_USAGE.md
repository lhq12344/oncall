# 自愈循环使用指南

## 快速开始

### 1. 创建自愈循环管理器

```go
package main

import (
    "context"
    "go_agent/internal/healing"
    "time"
    "go.uber.org/zap"
)

func main() {
    logger, _ := zap.NewProduction()

    // 配置自愈循环
    config := &healing.HealingConfig{
        // 自动触发
        AutoTrigger:     true,
        MonitorInterval: 30 * time.Second,
        DetectionWindow: 5 * time.Minute,

        // 重试策略
        MaxRetries:        3,
        RetryDelay:        30 * time.Second,
        BackoffMultiplier: 2.0,

        // 审批策略
        RequireApproval: false,
        ApprovalTimeout: 5 * time.Minute,

        // 学习策略
        EnableLearning: true,
        MinConfidence:  0.7,

        // Agents（需要从 bootstrap 传入）
        RCAAgent:       rcaAgent,
        StrategyAgent:  strategyAgent,
        ExecutionAgent: executionAgent,
        KnowledgeAgent: knowledgeAgent,
        OpsAgent:       opsAgent,
    }

    // 创建管理器
    manager, err := healing.NewHealingLoopManager(config, logger)
    if err != nil {
        logger.Fatal("failed to create healing manager", zap.Error(err))
    }

    // 启动自愈循环
    ctx := context.Background()
    if err := manager.Start(ctx); err != nil {
        logger.Fatal("failed to start healing manager", zap.Error(err))
    }
    defer manager.Stop()

    // 保持运行
    select {}
}
```

### 2. 手动触发自愈

```go
// 创建故障事件
incident := &healing.Incident{
    ID:          "incident-001",
    Timestamp:   time.Now(),
    Severity:    healing.SeverityHigh,
    Type:        healing.IncidentTypePodCrashLoop,
    Title:       "Pod Crash Loop Detected",
    Description: "Pod nginx-deployment-xxx is in CrashLoopBackOff state",
    AffectedResources: []healing.Resource{
        {
            Type:      "pod",
            Namespace: "default",
            Name:      "nginx-deployment-xxx",
            Labels: map[string]string{
                "app": "nginx",
            },
        },
    },
}

// 触发自愈
session, err := manager.TriggerHealing(ctx, incident)
if err != nil {
    logger.Error("failed to trigger healing", zap.Error(err))
    return
}

logger.Info("healing triggered", zap.String("session_id", session.ID))

// 等待结果
result := <-session.ResultChan
if result.Success {
    logger.Info("healing completed successfully",
        zap.String("session_id", result.SessionID),
        zap.Duration("duration", result.Duration))
} else {
    logger.Error("healing failed",
        zap.String("session_id", result.SessionID),
        zap.Error(result.Error))
}
```

### 3. 查询自愈状态

```go
// 获取所有活跃会话
sessions := manager.GetActiveSessions()
for _, session := range sessions {
    logger.Info("active healing session",
        zap.String("session_id", session.ID),
        zap.String("state", string(session.State)),
        zap.String("incident_type", string(session.Incident.Type)),
        zap.Int("retry_count", session.RetryCount))
}

// 获取特定会话
session, err := manager.GetSession("session-id-xxx")
if err != nil {
    logger.Error("session not found", zap.Error(err))
    return
}

logger.Info("session details",
    zap.String("state", string(session.State)),
    zap.Any("diagnosis", session.Diagnosis),
    zap.Any("decision", session.Decision))
```

## 集成到 Bootstrap

在 `internal/bootstrap/app.go` 中集成自愈循环：

```go
// Application 应用实例
type Application struct {
    ContextManager  *appcontext.ContextManager
    SupervisorAgent adk.ResumableAgent
    OpsIntegration  *ops.IntegratedOpsExecutor
    Logger          *zap.Logger
    RedisClient     *redis.Client
    CBManager       *concurrent.CircuitBreakerManager
    HealingManager  *healing.HealingLoopManager  // 新增
}

// NewApplication 创建应用实例
func NewApplication(cfg *Config) (*Application, error) {
    // ... 现有初始化代码 ...

    // 初始化自愈循环管理器
    healingConfig := &healing.HealingConfig{
        AutoTrigger:     true,
        MonitorInterval: 30 * time.Second,
        MaxRetries:      3,
        EnableLearning:  true,
        RCAAgent:        rcaAgent,
        StrategyAgent:   strategyAgent,
        ExecutionAgent:  executionAgent,
        KnowledgeAgent:  knowledgeAgent,
        OpsAgent:        opsAgent,
    }

    healingManager, err := healing.NewHealingLoopManager(healingConfig, logger)
    if err != nil {
        return nil, fmt.Errorf("failed to create healing manager: %w", err)
    }

    // 启动自愈循环
    go func() {
        if err := healingManager.Start(context.Background()); err != nil {
            logger.Error("failed to start healing manager", zap.Error(err))
        }
    }()

    logger.Info("healing loop manager initialized")

    return &Application{
        // ... 现有字段 ...
        HealingManager: healingManager,
    }, nil
}

// Close 关闭应用
func (app *Application) Close() error {
    if app.HealingManager != nil {
        if err := app.HealingManager.Stop(); err != nil {
            return fmt.Errorf("failed to stop healing manager: %w", err)
        }
    }

    // ... 其他清理代码 ...
    return nil
}
```

## 添加 API 端点

在 `api/chat/v1/chat.go` 添加自愈相关的 API：

```go
// HealingTriggerReq 触发自愈请求
type HealingTriggerReq struct {
    g.Meta      `path:"/healing/trigger" method:"post" summary:"触发自愈"`
    IncidentID  string `json:"incident_id" v:"required"`
    Type        string `json:"type" v:"required"`
    Severity    string `json:"severity" v:"required"`
    Description string `json:"description" v:"required"`
}

type HealingTriggerRes struct {
    SessionID string `json:"session_id"`
    Message   string `json:"message"`
}

// HealingStatusReq 查询自愈状态请求
type HealingStatusReq struct {
    g.Meta    `path:"/healing/status" method:"get" summary:"查询自愈状态"`
    SessionID string `json:"session_id"`
}

type HealingStatusRes struct {
    Sessions []HealingSessionInfo `json:"sessions"`
}

type HealingSessionInfo struct {
    SessionID   string `json:"session_id"`
    State       string `json:"state"`
    IncidentID  string `json:"incident_id"`
    StartTime   string `json:"start_time"`
    RetryCount  int    `json:"retry_count"`
}
```

在 `internal/controller/chat/chat_v1.go` 实现处理函数：

```go
func (c *ControllerV1) HealingTrigger(ctx context.Context, req *v1.HealingTriggerReq) (res *v1.HealingTriggerRes, err error) {
    if c.healingManager == nil {
        return nil, fmt.Errorf("healing manager not initialized")
    }

    // 创建故障事件
    incident := &healing.Incident{
        ID:          req.IncidentID,
        Timestamp:   time.Now(),
        Severity:    healing.Severity(req.Severity),
        Type:        healing.IncidentType(req.Type),
        Description: req.Description,
    }

    // 触发自愈
    session, err := c.healingManager.TriggerHealing(ctx, incident)
    if err != nil {
        return nil, err
    }

    return &v1.HealingTriggerRes{
        SessionID: session.ID,
        Message:   "Healing triggered successfully",
    }, nil
}

func (c *ControllerV1) HealingStatus(ctx context.Context, req *v1.HealingStatusReq) (res *v1.HealingStatusRes, err error) {
    if c.healingManager == nil {
        return nil, fmt.Errorf("healing manager not initialized")
    }

    var sessions []*healing.HealingSession
    if req.SessionID != "" {
        // 查询特定会话
        session, err := c.healingManager.GetSession(req.SessionID)
        if err != nil {
            return nil, err
        }
        sessions = []*healing.HealingSession{session}
    } else {
        // 查询所有活跃会话
        sessions = c.healingManager.GetActiveSessions()
    }

    // 转换为响应格式
    infos := make([]v1.HealingSessionInfo, 0, len(sessions))
    for _, session := range sessions {
        infos = append(infos, v1.HealingSessionInfo{
            SessionID:  session.ID,
            State:      string(session.State),
            IncidentID: session.Incident.ID,
            StartTime:  session.StartTime.Format(time.RFC3339),
            RetryCount: session.RetryCount,
        })
    }

    return &v1.HealingStatusRes{
        Sessions: infos,
    }, nil
}
```

## 配置文件

在 `manifest/config/config.yaml` 添加自愈配置：

```yaml
# 自愈循环配置
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
```

## 使用示例

### 1. 触发自愈

```bash
curl -X POST http://localhost:6872/api/v1/healing/trigger \
  -H "Content-Type: application/json" \
  -d '{
    "incident_id": "inc-001",
    "type": "pod_crash_loop",
    "severity": "high",
    "description": "Pod nginx-xxx is in CrashLoopBackOff"
  }'
```

响应：

```json
{
  "session_id": "session-xxx",
  "message": "Healing triggered successfully"
}
```

### 2. 查询状态

```bash
# 查询所有活跃会话
curl http://localhost:6872/api/v1/healing/status

# 查询特定会话
curl http://localhost:6872/api/v1/healing/status?session_id=session-xxx
```

响应：

```json
{
  "sessions": [
    {
      "session_id": "session-xxx",
      "state": "executing",
      "incident_id": "inc-001",
      "start_time": "2026-03-07T19:00:00Z",
      "retry_count": 0
    }
  ]
}
```

## 监控指标

建议添加 Prometheus 指标：

```go
import "github.com/prometheus/client_golang/prometheus"

var (
    healingLoopTotal = prometheus.NewCounterVec(
        prometheus.CounterOpts{
            Name: "healing_loop_total",
            Help: "Total number of healing loops",
        },
        []string{"state"},
    )

    healingLoopDuration = prometheus.NewHistogramVec(
        prometheus.HistogramOpts{
            Name: "healing_loop_duration_seconds",
            Help: "Duration of healing loops",
        },
        []string{"state"},
    )

    healingLoopActiveSessions = prometheus.NewGauge(
        prometheus.GaugeOpts{
            Name: "healing_loop_active_sessions",
            Help: "Number of active healing sessions",
        },
    )
)

func init() {
    prometheus.MustRegister(healingLoopTotal)
    prometheus.MustRegister(healingLoopDuration)
    prometheus.MustRegister(healingLoopActiveSessions)
}
```

## 总结

自愈循环已经实现了基础框架，包括：

1. ✅ 完整的状态机（监控 → 检测 → 诊断 → 决策 → 执行 → 验证 → 学习）
2. ✅ 自动触发机制
3. ✅ 验证失败重试（带指数退避）
4. ✅ 学习反馈循环
5. ✅ 回滚机制
6. ✅ 会话管理
7. ✅ 单元测试

下一步可以：

1. 实现实际的监控数据收集（Prometheus/K8s/ES）
2. 集成真实的 Agent 调用
3. 实现案例数据库存储
4. 添加 Web UI 展示自愈过程
5. 添加更多的自愈策略模板
