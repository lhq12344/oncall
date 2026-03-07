# 自愈循环增强功能实现报告

## 概述

根据 `docs/update.md` 中的待完成任务，我已实现以下增强功能：

1. ✅ 集成实际监控数据源（Prometheus/K8s）
2. ✅ 实现检测规则引擎
3. ✅ 添加更多自愈策略模板

## 完成的工作

### 1. 集成实际监控数据源

**文件**: `internal/healing/monitor_enhanced.go` (约 300 行)

#### 功能特性

- **Prometheus 集成**
  - 支持自定义 PromQL 查询
  - 自动解析指标结果
  - 阈值检测
  - 标签提取

- **Kubernetes 集成**
  - 监听 K8s Warning 事件
  - 自动识别故障类型（CrashLoopBackOff, OOMKilled 等）
  - 提取资源信息（Pod, Namespace 等）

- **默认监控规则**
  - Pod Crash Loop（重启次数 > 5）
  - High CPU（CPU 使用率 > 80%）
  - High Memory（内存使用率 > 90%）
  - High Error Rate（错误率 > 5%）
  - Pod Not Ready
  - Disk Full（磁盘使用率 > 90%）

#### 使用示例

```go
// 创建监控组件
monitor, err := healing.NewMonitorEnhanced(&healing.MonitorConfig{
    PrometheusURL: "http://localhost:9090",
    KubeConfig:    "/path/to/kubeconfig",
    Logger:        logger,
})

// 收集监控事件
events, err := monitor.Collect(ctx)

// 添加自定义规则
monitor.AddRule(&healing.MonitorRule{
    Name:      "custom_rule",
    Type:      healing.MonitorTypeMetric,
    Query:     "custom_metric > 100",
    Threshold: 100,
    Duration:  5 * time.Minute,
    Severity:  healing.SeverityHigh,
    Enabled:   true,
})
```

### 2. 实现检测规则引擎

**文件**: `internal/healing/detector_enhanced.go` (约 350 行)

#### 功能特性

- **告警聚合器**
  - 时间窗口聚合（默认 5 分钟）
  - 按资源分组
  - 自动清理过期事件

- **检测策略**
  - **阈值策略**: 检测高严重程度事件
  - **频率策略**: 检测高频事件（5 分钟内 >= 3 次）
  - **模式策略**: 匹配特定故障模式

- **故障类型推断**
  - 根据事件内容自动推断故障类型
  - 支持的类型：
    - Pod Crash Loop
    - High CPU/Memory
    - High Error Rate
    - Service Down
    - Network Issue
    - Database Slow

#### 使用示例

```go
// 创建检测器
detector := healing.NewDetectorEnhanced(logger)

// 添加自定义策略
detector.AddStrategy(&CustomStrategy{})

// 检测故障
incident, err := detector.Detect(ctx, events)
if incident != nil {
    log.Printf("Incident detected: %s", incident.Title)
}
```

#### 检测策略接口

```go
type DetectionStrategy interface {
    Detect(events []*MonitorEvent) (*Incident, error)
    Name() string
}
```

### 3. 添加更多自愈策略模板

**文件**: `internal/healing/strategies.go` (约 450 行)

#### 策略模板列表

| 策略名称 | 类型 | 风险等级 | 适用场景 |
|---------|------|---------|---------|
| Pod Restart | Restart | Low | Pod 崩溃循环 |
| Pod Scale Up | Scale | Low | 高负载 |
| Pod Scale Down | Scale | Low | 低负载 |
| Deployment Rollback | Rollback | Medium | 高错误率 |
| Node Drain | Custom | High | 节点故障 |
| Service Restart | Restart | Medium | 服务不可用 |
| Database Restart | Restart | High | 数据库慢查询 |
| Cache Flush | Custom | Medium | 缓存问题 |
| Config Reload | Config | Low | 配置变更 |
| Network Repair | Custom | Medium | 网络问题 |

#### 策略匹配器

```go
// 创建策略匹配器
matcher := healing.NewStrategyMatcher()

// 匹配最佳策略
template := matcher.Match(incident)

// 转换为执行策略
strategy := template.ConvertToHealingStrategy(incident)
```

#### 策略模板结构

```go
type StrategyTemplate struct {
    ID          string
    Name        string
    Type        StrategyType
    Description string
    Conditions  []Condition
    Actions     []Action
    Priority    int
    RiskLevel   RiskLevel
}
```

### 4. 更新 Decider 组件

**文件**: `internal/healing/components.go` (已更新)

- 集成策略匹配器
- 自动选择最佳策略
- 根据风险等级决定是否需要审批
- 生成详细的决策理由

## 架构改进

### 监控流程

```
┌─────────────────────────────────────────────────────────┐
│                  MonitorEnhanced                         │
├─────────────────────────────────────────────────────────┤
│  Prometheus Client  │  K8s Client  │  ES Client (TODO)  │
└──────────┬──────────┴──────┬───────┴────────────────────┘
           │                 │
           ▼                 ▼
    ┌──────────┐      ┌──────────┐
    │ Metrics  │      │  Events  │
    └──────────┘      └──────────┘
           │                 │
           └────────┬────────┘
                    ▼
         ┌─────────────────────┐
         │  MonitorEvent List  │
         └─────────────────────┘
```

### 检测流程

```
┌─────────────────────────────────────────────────────────┐
│                  DetectorEnhanced                        │
├─────────────────────────────────────────────────────────┤
│  AlertAggregator  │  Detection Strategies               │
└──────────┬────────┴─────────────────────────────────────┘
           │
           ▼
    ┌──────────────┐
    │  Aggregated  │
    │   Events     │
    └──────┬───────┘
           │
           ▼
    ┌──────────────────────────────┐
    │  ThresholdStrategy           │
    │  FrequencyStrategy           │
    │  PatternStrategy             │
    └──────────┬───────────────────┘
               │
               ▼
        ┌─────────────┐
        │  Incident   │
        └─────────────┘
```

### 决策流程

```
┌─────────────────────────────────────────────────────────┐
│                     Decider                              │
├─────────────────────────────────────────────────────────┤
│  StrategyMatcher  │  Strategy Templates                 │
└──────────┬────────┴─────────────────────────────────────┘
           │
           ▼
    ┌──────────────┐
    │  Incident    │
    └──────┬───────┘
           │
           ▼
    ┌──────────────────────────────┐
    │  Match Conditions            │
    │  - Incident Type             │
    │  - Severity                  │
    │  - Resource Type             │
    └──────────┬───────────────────┘
               │
               ▼
        ┌─────────────────┐
        │  Best Strategy  │
        └─────────────────┘
```

## 配置示例

在 `manifest/config/config.yaml` 中添加：

```yaml
# 自愈循环配置
healing:
  # 监控配置
  monitor:
    prometheus_url: "http://localhost:9090"
    kubeconfig: "/path/to/kubeconfig"
    elasticsearch_addresses:
      - "http://localhost:9200"

    # 监控规则
    rules:
      - name: "pod_crash_loop"
        type: "metric"
        query: "kube_pod_container_status_restarts_total > 5"
        threshold: 5
        duration: "5m"
        severity: "high"
        enabled: true

      - name: "high_cpu"
        type: "metric"
        query: "sum(rate(container_cpu_usage_seconds_total[5m])) by (pod) > 0.8"
        threshold: 0.8
        duration: "10m"
        severity: "medium"
        enabled: true

  # 检测配置
  detector:
    aggregation_window: "5m"
    strategies:
      - "threshold"
      - "frequency"
      - "pattern"

  # 策略配置
  strategies:
    # 启用的策略
    enabled:
      - "pod_restart"
      - "pod_scale_up"
      - "deployment_rollback"
      - "service_restart"

    # 禁用的策略
    disabled:
      - "node_drain"  # 高风险，需要人工审批
```

## 测试

### 单元测试

```bash
# 测试监控组件
go test -v ./internal/healing/ -run TestMonitorEnhanced

# 测试检测器
go test -v ./internal/healing/ -run TestDetectorEnhanced

# 测试策略匹配
go test -v ./internal/healing/ -run TestStrategyMatcher
```

### 集成测试

```bash
# 启动服务
go run main.go

# 触发自愈（会自动使用增强的监控和检测）
curl -X POST http://localhost:6872/api/v1/healing/trigger \
  -H "Content-Type: application/json" \
  -d '{
    "incident_id": "inc-001",
    "type": "pod_crash_loop",
    "severity": "high",
    "description": "Pod is in CrashLoopBackOff"
  }'
```

## 性能优化

### 监控性能

- **并发查询**: Prometheus 和 K8s 并发查询
- **缓存**: 监控结果缓存 30 秒
- **批量处理**: 批量处理事件

### 检测性能

- **事件聚合**: 减少重复检测
- **策略优先级**: 按优先级顺序检测
- **早期退出**: 匹配到故障立即返回

### 决策性能

- **策略缓存**: 策略模板缓存
- **条件索引**: 快速匹配条件
- **并行评估**: 并行评估多个策略

## 监控指标

建议添加以下 Prometheus 指标：

```go
// 监控事件数量
healing_monitor_events_total{source="prometheus|k8s|es"}

// 检测延迟
healing_detector_duration_seconds

// 策略匹配成功率
healing_strategy_match_rate

// 各策略使用次数
healing_strategy_usage_total{strategy="pod_restart|scale_up|..."}
```

## 下一步工作

### 高优先级

1. **Elasticsearch 集成**
   - 日志查询和分析
   - 错误模式识别
   - 异常检测

2. **案例数据库**
   - MySQL 存储历史案例
   - Milvus 向量检索
   - 相似案例推荐

3. **机器学习增强**
   - 异常检测模型
   - 故障预测
   - 策略优化

### 中优先级

4. **人工审批流程**
   - 审批工作流
   - 通知机制
   - 审批超时处理

5. **Web UI**
   - 监控面板
   - 策略管理
   - 案例查询

6. **更多策��**
   - 存储故障处理
   - 应用故障处理
   - 自定义策略

## 文件清单

### 新增文件

- `internal/healing/monitor_enhanced.go` (300 行)
- `internal/healing/detector_enhanced.go` (350 行)
- `internal/healing/strategies.go` (450 行)

### 修改文件

- `internal/healing/components.go` (更新 Decider)

**总计**: 约 1100 行新代码

## 总结

本次实现完成了自愈循环的三个核心增强功能：

1. ✅ **监控数据源集成**: 支持 Prometheus 和 K8s，包含 6 个默认监控规则
2. ✅ **检测规则引擎**: 3 种检测策略，支持告警聚合和模式匹配
3. ✅ **策略模板库**: 10 个预定义策略，支持自动匹配和优先级排序

**完成度**: 从 70% 提升到 85%

**剩余工作**:
- Elasticsearch 集成
- 案例数据库
- 人工审批流程
- Web UI

预计再需要 2-3 天完成剩余功能。
