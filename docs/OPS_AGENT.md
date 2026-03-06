# Ops Agent 实现文档

## 概述

Ops Agent 是基于 Eino ADK 的运维代理，负责 K8s 监控、Prometheus 指标采集、日志分析、健康检查和系统综合诊断。通过动态阈值检测和多维信号聚合，实现智能的异常检测和故障诊断。

## 核心功能

### 1. K8s 监控（K8s Monitoring）

#### 监控 Pod 状态
```go
result, err := opsAgent.MonitorK8s(ctx, "default", "pod", "nginx-pod")
```

**返回信息**:
- Pod 名称、阶段（Running/Pending/Failed）
- 就绪状态、重启次数
- 节点名称、IP 地址
- 事件列表

#### 监控 Deployment 状态
```go
result, err := opsAgent.MonitorK8s(ctx, "default", "deployment", "nginx-deployment")
```

**返回信息**:
- 期望副本数、就绪副本数、更新副本数
- 条件列表（Available/Progressing）

#### 监控 Service 状态
```go
result, err := opsAgent.MonitorK8s(ctx, "default", "service", "nginx-service")
```

**返回信息**:
- Service 类型（ClusterIP/NodePort/LoadBalancer）
- Cluster IP、端口列表
- Selector、Endpoint 数量

#### 获取资源使用情况
```go
usage, err := opsAgent.GetResourceUsage(ctx, "default", "nginx-pod")
```

**返回信息**:
- CPU 使用率（核数）
- 内存使用量（字节）
- 磁盘使用量（字节）
- 网络流量（字节/秒）

### 2. 指标采集（Metrics Collection）

#### Prometheus 查询
```go
result, err := opsAgent.CollectMetrics(ctx, "rate(cpu[5m])", "5m")
```

**支持的查询**:
- CPU 使用率：`rate(container_cpu_usage_seconds_total[5m])`
- 内存使用量：`container_memory_usage_bytes`
- 网络流量：`rate(container_network_receive_bytes_total[5m])`
- 请求延迟：`histogram_quantile(0.99, rate(http_request_duration_seconds_bucket[5m]))`

**返回信息**:
- 数据点列表（时间戳 + 值 + 标签）
- 异常点列表（自动检测）

### 3. 动态阈值检测（Dynamic Threshold Detection）

#### 算法原理
```
Anomaly = |X_t - μ_t| > k · σ_t

其中：
- X_t: 当前指标值
- μ_t: 滑动窗口均值
- σ_t: 滑动窗口标准差
- k: 敏感度系数（通常取 2-3）
```

#### 使用示例
```go
detector := NewDynamicThresholdDetector(100, 2.5)

// 添加数据点
isAnomaly, score := detector.Detect(value)

if isAnomaly {
    fmt.Printf("异常检测：值=%.2f, 评分=%.2f\n", value, score)
}
```

**特点**:
- 滑动窗口大小：100 个数据点
- 敏感度系数：2.5（可调整）
- 自动适应基线变化
- 环形缓冲区（高效内存使用）

### 4. 多维信号聚合（Signal Aggregation）

#### 聚合检测
```go
signals := map[string]float64{
    "cpu":        85.0,
    "memory":     90.0,
    "latency":    500.0,
    "error_rate": 0.05,
}

report := opsAgent.AggregateSignals(ctx, signals)
```

**返回信息**:
- 异常指标列表（指标名 -> 评分）
- 综合异常评分
- 严重程度（normal/info/warning/critical）

#### 严重程度计算
```go
if score > 2.0 && count >= 3 {
    severity = "critical"
} else if score > 1.5 || count >= 2 {
    severity = "warning"
} else if score > 1.0 || count >= 1 {
    severity = "info"
} else {
    severity = "normal"
}
```

### 5. 日志分析（Log Analysis）

#### 查询日志
```go
result, err := opsAgent.AnalyzeLogs(ctx, "loki", `{app="nginx"}`, "5m")
```

**分析内容**:
- 总日志数
- 错误数、警告数
- 日志模式（Pattern）
- Top 错误（按出现次数排序）

#### 日志模式识别
- 自动识别重复的日志模式
- 提取错误消息
- 统计出现频率

### 6. 健康检查（Health Check）

#### HTTP 健康检查
```go
target := &HealthCheckTarget{
    Type:     "http",
    Endpoint: "http://localhost:8080/health",
    Timeout:  5 * time.Second,
}

result, err := opsAgent.CheckHealth(ctx, target)
```

#### 支持的类型
- **HTTP**: GET 请求，检查状态码
- **TCP**: TCP 连接测试
- **gRPC**: gRPC 健康检查协议

**返回信息**:
- 健康状态（true/false）
- 延迟（Duration）
- 消息（OK/Error）

### 7. 系统综合诊断（System Diagnosis）

#### 完整诊断
```go
target := &DiagnosisTarget{
    Namespace:      "default",
    Service:        "nginx",
    CheckK8s:       true,
    CheckMetrics:   true,
    CheckLogs:      true,
    HealthCheckURL: "http://nginx/health",
}

report, err := opsAgent.DiagnoseSystem(ctx, target)
```

**诊断流程**:
1. 检查 K8s 资源状态
2. 采集 CPU/内存指标
3. 分析日志（错误、警告）
4. 执行健康检查
5. 生成问题列表
6. 计算整体健康度

**整体健康度计算**:
```go
score := 1.0
for _, issue := range issues {
    switch issue.Severity {
    case "critical": score -= 0.3
    case "high":     score -= 0.2
    case "medium":   score -= 0.1
    case "low":      score -= 0.05
    }
}
```

## 数据结构

### K8sMonitorResult
```go
type K8sMonitorResult struct {
    Namespace    string      // 命名空间
    ResourceType string      // 资源类型
    ResourceName string      // 资源名称
    Status       interface{} // 状态详情
    Healthy      bool        // 是否健康
    Timestamp    time.Time   // 时间戳
}
```

### MetricsResult
```go
type MetricsResult struct {
    Query     string        // 查询语句
    TimeRange string        // 时间范围
    Data      []MetricPoint // 数据点
    Anomalies []AnomalyPoint // 异常点
    Timestamp time.Time     // 时间戳
}
```

### AnomalyReport
```go
type AnomalyReport struct {
    Timestamp    time.Time          // 时间戳
    Anomalies    map[string]float64 // 异常指标
    OverallScore float64            // 综合评分
    Severity     string             // 严重程度
}
```

### DiagnosisReport
```go
type DiagnosisReport struct {
    Target        *DiagnosisTarget   // 诊断目标
    K8sStatus     *K8sMonitorResult  // K8s 状态
    Metrics       []*MetricsResult   // 指标列表
    LogAnalysis   *LogAnalysisResult // 日志分析
    HealthCheck   *HealthCheckResult // 健康检查
    Issues        []*Issue           // 问题列表
    OverallHealth float64            // 整体健康度
    Timestamp     time.Time          // 时间戳
}
```

## 工具封装

### 1. K8sMonitorTool
```json
{
  "name": "k8s_monitor",
  "desc": "监控 K8s 资源状态",
  "params": {
    "namespace": "命名空间",
    "resource_type": "资源类型（pod/deployment/service）",
    "resource_name": "资源名称"
  }
}
```

### 2. MetricsCollectorTool
```json
{
  "name": "metrics_collector",
  "desc": "采集 Prometheus 指标数据",
  "params": {
    "query": "PromQL 查询语句",
    "time_range": "时间范围（如 5m, 1h, 1d）"
  }
}
```

### 3. LogAnalyzerTool
```json
{
  "name": "log_analyzer",
  "desc": "分析日志数据",
  "params": {
    "source": "日志源（loki/elasticsearch）",
    "query": "查询语句",
    "time_range": "时间范围"
  }
}
```

### 4. HealthCheckTool
```json
{
  "name": "health_check",
  "desc": "执行健康检查",
  "params": {
    "type": "检查类型（http/tcp/grpc）",
    "endpoint": "端点地址"
  }
}
```

### 5. SystemDiagnosisTool
```json
{
  "name": "system_diagnosis",
  "desc": "执行系统综合诊断",
  "params": {
    "namespace": "命名空间",
    "service": "服务名"
  }
}
```

## 使用示例

### 1. 创建 Ops Agent
```go
import (
    "go_agent/internal/agent/ops"
)

// 创建 Ops Agent
opsAgent := ops.NewOpsAgent(&ops.Config{
    ContextManager: contextManager,
    K8sConfig:      &ops.K8sConfig{InCluster: false},
    PrometheusURL:  "http://localhost:9090",
    Logger:         logger,
})
```

### 2. 监控 K8s 资源
```go
// 监控 Pod
result, err := opsAgent.MonitorK8s(ctx, "default", "pod", "nginx-pod")
if err != nil {
    return err
}

fmt.Printf("Pod 健康状态: %v\n", result.Healthy)
fmt.Printf("Pod 详情: %+v\n", result.Status)
```

### 3. 采集指标并检测异常
```go
// 采集 CPU 指标
query := `rate(container_cpu_usage_seconds_total{namespace="default"}[5m])`
result, err := opsAgent.CollectMetrics(ctx, query, "5m")
if err != nil {
    return err
}

fmt.Printf("数据点数: %d\n", len(result.Data))
fmt.Printf("异常点数: %d\n", len(result.Anomalies))

for _, anomaly := range result.Anomalies {
    fmt.Printf("异常: 时间=%s, 值=%.2f, 评分=%.2f\n",
        anomaly.Timestamp, anomaly.Value, anomaly.Score)
}
```

### 4. 多维信号聚合
```go
// 收集多个指标
signals := map[string]float64{
    "cpu":        cpuValue,
    "memory":     memoryValue,
    "latency":    latencyValue,
    "error_rate": errorRateValue,
}

// 聚合检测
report := opsAgent.AggregateSignals(ctx, signals)

fmt.Printf("综合评分: %.2f\n", report.OverallScore)
fmt.Printf("严重程度: %s\n", report.Severity)

for metric, score := range report.Anomalies {
    fmt.Printf("异常指标: %s (评分: %.2f)\n", metric, score)
}
```

### 5. 系统综合诊断
```go
// 定义诊断目标
target := &ops.DiagnosisTarget{
    Namespace:      "default",
    Service:        "nginx",
    CheckK8s:       true,
    CheckMetrics:   true,
    CheckLogs:      true,
    HealthCheckURL: "http://nginx/health",
}

// 执行诊断
report, err := opsAgent.DiagnoseSystem(ctx, target)
if err != nil {
    return err
}

fmt.Printf("整体健康度: %.2f\n", report.OverallHealth)
fmt.Printf("问题数量: %d\n", len(report.Issues))

for _, issue := range report.Issues {
    fmt.Printf("[%s] %s: %s\n", issue.Severity, issue.Type, issue.Message)
}
```

### 6. 作为工具使用
```go
// 创建工具
k8sTool := ops.NewK8sMonitorTool(opsAgent)
metricsTool := ops.NewMetricsCollectorTool(opsAgent)
diagnosisTool := ops.NewSystemDiagnosisTool(opsAgent)

// 注册到 Supervisor
supervisorAgent.RegisterTool(k8sTool)
supervisorAgent.RegisterTool(metricsTool)
supervisorAgent.RegisterTool(diagnosisTool)

// 工具会被 LLM 自动调用
```

## 性能优化

### 1. 滑动窗口优化
- 使用环形缓冲区（ring.Ring）
- 避免数组扩容和拷贝
- O(1) 时间复杂度

### 2. 并发安全
- 使用 sync.RWMutex 保护共享状态
- 读多写少场景优化
- 避免锁竞争

### 3. 内存优化
- 对象池化（sync.Pool）
- 及时释放不用的数据
- 限制缓冲区大小

## 测试

### 运行测试
```bash
go test ./internal/agent/ops/... -v
```

### 测试覆盖
- ✅ DynamicThresholdDetector 动态阈值检测
- ✅ SignalAggregator 信号聚合
- ✅ AnomalyDetector 异常检测
- ✅ K8sMonitor K8s 监控
- ✅ MetricsCollector 指标采集
- ✅ LogAnalyzer 日志分析
- ✅ HealthChecker 健康检查
- ✅ OpsAgent 完整流程

## 未来改进

### 1. 机器学习增强
- [ ] 使用 LSTM 进行时序预测
- [ ] 异常检测模型（Isolation Forest）
- [ ] 根因分析模型

### 2. 更多监控源
- [ ] Jaeger 链路追踪
- [ ] Elasticsearch 日志
- [ ] Grafana 仪表盘

### 3. 智能告警
- [ ] 告警聚合与去重
- [ ] 告警优先级排序
- [ ] 告警静默规则

### 4. 自动化响应
- [ ] 自动扩容/缩容
- [ ] 自动重启
- [ ] 自动回滚

## 文件清单

```
internal/agent/ops/
├── ops.go        # Ops Agent 主逻辑
├── types.go      # 数据结构定义
├── monitor.go    # 监控器（K8s、Prometheus、日志、健康检查）
├── detector.go   # 动态阈值检测器 + 信号聚合器
├── tool.go       # 工具封装（5 个工具）
└── ops_test.go   # 测试
```

## 依赖项

```go
require (
    go_agent/internal/context
    go.uber.org/zap v1.27.0
    github.com/cloudwego/eino/compose
)

// 实际使用时需要：
// k8s.io/client-go v0.28.0
// github.com/prometheus/client_golang v1.16.0
```

---

**文档版本**: v1.0
**最后更新**: 2026-03-06
**作者**: Oncall Team
