package ops

import (
	"time"
)

// K8sMonitorResult K8s 监控结果
type K8sMonitorResult struct {
	Namespace    string      `json:"namespace"`     // 命名空间
	ResourceType string      `json:"resource_type"` // 资源类型
	ResourceName string      `json:"resource_name"` // 资源名称
	Status       interface{} `json:"status"`        // 状态详情
	Healthy      bool        `json:"healthy"`       // 是否健康
	Timestamp    time.Time   `json:"timestamp"`     // 时间戳
}

// PodStatus Pod 状态
type PodStatus struct {
	Name      string            `json:"name"`       // Pod 名称
	Phase     string            `json:"phase"`      // 阶段（Running/Pending/Failed）
	Ready     bool              `json:"ready"`      // 是否就绪
	Restarts  int               `json:"restarts"`   // 重启次数
	Age       time.Duration     `json:"age"`        // 运行时长
	NodeName  string            `json:"node_name"`  // 节点名称
	IP        string            `json:"ip"`         // IP 地址
	Labels    map[string]string `json:"labels"`     // 标签
	Events    []string          `json:"events"`     // 事件列表
}

// DeploymentStatus Deployment 状态
type DeploymentStatus struct {
	Name            string    `json:"name"`             // Deployment 名称
	Replicas        int32     `json:"replicas"`         // 期望副本数
	ReadyReplicas   int32     `json:"ready_replicas"`   // 就绪副本数
	UpdatedReplicas int32     `json:"updated_replicas"` // 更新副本数
	Conditions      []string  `json:"conditions"`       // 条件列表
	Timestamp       time.Time `json:"timestamp"`        // 时间戳
}

// ServiceStatus Service 状态
type ServiceStatus struct {
	Name      string            `json:"name"`       // Service 名称
	Type      string            `json:"type"`       // 类型（ClusterIP/NodePort/LoadBalancer）
	ClusterIP string            `json:"cluster_ip"` // Cluster IP
	Ports     []int32           `json:"ports"`      // 端口列表
	Selector  map[string]string `json:"selector"`   // 选择器
	Endpoints int               `json:"endpoints"`  // Endpoint 数量
}

// MetricsResult 指标结果
type MetricsResult struct {
	Query     string          `json:"query"`      // 查询语句
	TimeRange string          `json:"time_range"` // 时间范围
	Data      []MetricPoint   `json:"data"`       // 数据点
	Anomalies []AnomalyPoint  `json:"anomalies"`  // 异常点
	Timestamp time.Time       `json:"timestamp"`  // 时间戳
}

// MetricPoint 指标数据点
type MetricPoint struct {
	Timestamp time.Time         `json:"timestamp"` // 时间戳
	Value     float64           `json:"value"`     // 值
	Labels    map[string]string `json:"labels"`    // 标签
}

// AnomalyPoint 异常点
type AnomalyPoint struct {
	Timestamp time.Time `json:"timestamp"` // 时间戳
	Value     float64   `json:"value"`     // ���
	Score     float64   `json:"score"`     // 异常评分
	Reason    string    `json:"reason"`    // 原因
}

// LogAnalysisResult 日志分析结果
type LogAnalysisResult struct {
	Source       string            `json:"source"`        // 日志源
	Query        string            `json:"query"`         // 查询语句
	TimeRange    string            `json:"time_range"`    // 时间范围
	TotalCount   int               `json:"total_count"`   // 总数
	ErrorCount   int               `json:"error_count"`   // 错误数
	WarningCount int               `json:"warning_count"` // 警告数
	Patterns     []LogPattern      `json:"patterns"`      // 日志模式
	TopErrors    []ErrorSummary    `json:"top_errors"`    // Top 错误
	Timestamp    time.Time         `json:"timestamp"`     // 时间戳
}

// LogPattern 日志模式
type LogPattern struct {
	Pattern string `json:"pattern"` // 模式
	Count   int    `json:"count"`   // 出现次数
	Example string `json:"example"` // 示例
}

// ErrorSummary 错误摘要
type ErrorSummary struct {
	Message   string    `json:"message"`   // 错误消息
	Count     int       `json:"count"`     // 出现次数
	FirstSeen time.Time `json:"first_seen"` // 首次出现
	LastSeen  time.Time `json:"last_seen"`  // 最后出现
}

// HealthCheckTarget 健康检查目标
type HealthCheckTarget struct {
	Type     string        `json:"type"`     // 类型（http/tcp/grpc）
	Endpoint string        `json:"endpoint"` // 端点
	Timeout  time.Duration `json:"timeout"`  // 超时时间
	Headers  map[string]string `json:"headers"` // HTTP 头（可选）
}

// HealthCheckResult 健康检查结果
type HealthCheckResult struct {
	Healthy   bool          `json:"healthy"`   // 是否健康
	Latency   time.Duration `json:"latency"`   // 延迟
	Message   string        `json:"message"`   // 消息
	Timestamp time.Time     `json:"timestamp"` // 时间戳
}

// AnomalyReport 异常报告
type AnomalyReport struct {
	Timestamp    time.Time         `json:"timestamp"`     // 时间戳
	Anomalies    map[string]float64 `json:"anomalies"`    // 异常指标（指标名 -> 评分）
	OverallScore float64           `json:"overall_score"` // 综合评分
	Severity     string            `json:"severity"`      // 严重程度（info/warning/critical）
}

// DiagnosisTarget 诊断目标
type DiagnosisTarget struct {
	Namespace      string `json:"namespace"`        // 命名空间
	Service        string `json:"service"`          // 服务名
	CheckK8s       bool   `json:"check_k8s"`        // 是否检查 K8s
	CheckMetrics   bool   `json:"check_metrics"`    // 是否检查指标
	CheckLogs      bool   `json:"check_logs"`       // 是否检查日志
	HealthCheckURL string `json:"health_check_url"` // 健康检查 URL（可选）
}

// DiagnosisReport 诊断报告
type DiagnosisReport struct {
	Target        *DiagnosisTarget   `json:"target"`         // 诊断目标
	K8sStatus     *K8sMonitorResult  `json:"k8s_status"`     // K8s 状态
	Metrics       []*MetricsResult   `json:"metrics"`        // 指标列表
	LogAnalysis   *LogAnalysisResult `json:"log_analysis"`   // 日志分析
	HealthCheck   *HealthCheckResult `json:"health_check"`   // 健康检查
	Issues        []*Issue           `json:"issues"`         // 问题列表
	OverallHealth float64            `json:"overall_health"` // 整体健康度（0-1）
	Timestamp     time.Time          `json:"timestamp"`      // 时间戳
}

// Issue 问题
type Issue struct {
	Type     string `json:"type"`     // 类型（k8s/metrics/logs/health）
	Severity string `json:"severity"` // 严重程度（low/medium/high/critical）
	Message  string `json:"message"`  // 消息
}

// ResourceUsage 资源使用情况
type ResourceUsage struct {
	PodName     string    `json:"pod_name"`     // Pod 名称
	CPUUsage    float64   `json:"cpu_usage"`    // CPU 使用率（核数）
	MemoryUsage float64   `json:"memory_usage"` // 内存使用量（字节）
	DiskUsage   float64   `json:"disk_usage"`   // 磁盘使用量（字节）
	NetworkIn   float64   `json:"network_in"`   // 网络入流量（字节/秒）
	NetworkOut  float64   `json:"network_out"`  // 网络出流量（字节/秒）
	Timestamp   time.Time `json:"timestamp"`    // 时间戳
}

// K8sConfig K8s 配置
type K8sConfig struct {
	KubeConfig string `json:"kube_config"` // kubeconfig 路径
	InCluster  bool   `json:"in_cluster"`  // 是否在集群内
}

// LogEntry 日志条目
type LogEntry struct {
	Timestamp time.Time         `json:"timestamp"` // 时间戳
	Level     string            `json:"level"`     // 级别
	Message   string            `json:"message"`   // 消息
	Labels    map[string]string `json:"labels"`    // 标签
}

// LogAnalysis 日志分析
type LogAnalysis struct {
	ErrorCount   int            `json:"error_count"`   // 错误数
	WarningCount int            `json:"warning_count"` // 警告数
	Patterns     []LogPattern   `json:"patterns"`      // 模式
	TopErrors    []ErrorSummary `json:"top_errors"`    // Top 错误
}
