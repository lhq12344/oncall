package ops

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/compose"
)

// K8sMonitorTool K8s 监控工具
type K8sMonitorTool struct {
	agent *OpsAgent
}

// NewK8sMonitorTool 创建 K8s 监控工具
func NewK8sMonitorTool(agent *OpsAgent) *K8sMonitorTool {
	return &K8sMonitorTool{agent: agent}
}

// Info 返回工具信息
func (t *K8sMonitorTool) Info(ctx context.Context) (*compose.ToolInfo, error) {
	return &compose.ToolInfo{
		Name: "k8s_monitor",
		Desc: "监控 K8s 资源状态（Pod、Deployment、Service）。输入：命名空间、资源类型、资源名称。输出：资源状态详情。",
		ParamsOneOf: compose.NewParamsOneOfByParams(
			map[string]*compose.ParameterInfo{
				"namespace": {
					Type:     compose.String,
					Desc:     "命名空间",
					Required: true,
				},
				"resource_type": {
					Type:     compose.String,
					Desc:     "资源类型（pod/deployment/service）",
					Required: true,
				},
				"resource_name": {
					Type:     compose.String,
					Desc:     "资源名称",
					Required: true,
				},
			},
		),
	}, nil
}

// InvokableRun 执行工具
func (t *K8sMonitorTool) InvokableRun(ctx context.Context, input string, opts ...compose.ToolOption) (string, error) {
	var params struct {
		Namespace    string `json:"namespace"`
		ResourceType string `json:"resource_type"`
		ResourceName string `json:"resource_name"`
	}

	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	result, err := t.agent.MonitorK8s(ctx, params.Namespace, params.ResourceType, params.ResourceName)
	if err != nil {
		return "", fmt.Errorf("k8s monitoring failed: %w", err)
	}

	output := fmt.Sprintf(`K8s 资源监控结果：
命名空间: %s
资源类型: %s
资源名称: %s
健康状态: %v
详细信息: %+v
`,
		result.Namespace,
		result.ResourceType,
		result.ResourceName,
		result.Healthy,
		result.Status,
	)

	return output, nil
}

// MetricsCollectorTool 指标采集工具
type MetricsCollectorTool struct {
	agent *OpsAgent
}

// NewMetricsCollectorTool 创建指标采集工具
func NewMetricsCollectorTool(agent *OpsAgent) *MetricsCollectorTool {
	return &MetricsCollectorTool{agent: agent}
}

// Info 返回工具信息
func (t *MetricsCollectorTool) Info(ctx context.Context) (*compose.ToolInfo, error) {
	return &compose.ToolInfo{
		Name: "metrics_collector",
		Desc: "采集 Prometheus 指标数据。输入：PromQL 查询语句和时间范围。输出：指标数据和异常检测结果。",
		ParamsOneOf: compose.NewParamsOneOfByParams(
			map[string]*compose.ParameterInfo{
				"query": {
					Type:     compose.String,
					Desc:     "PromQL 查询语句",
					Required: true,
				},
				"time_range": {
					Type:     compose.String,
					Desc:     "时间范围（如 5m, 1h, 1d）",
					Required: false,
				},
			},
		),
	}, nil
}

// InvokableRun 执行工具
func (t *MetricsCollectorTool) InvokableRun(ctx context.Context, input string, opts ...compose.ToolOption) (string, error) {
	var params struct {
		Query     string `json:"query"`
		TimeRange string `json:"time_range"`
	}

	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	if params.TimeRange == "" {
		params.TimeRange = "5m"
	}

	result, err := t.agent.CollectMetrics(ctx, params.Query, params.TimeRange)
	if err != nil {
		return "", fmt.Errorf("metrics collection failed: %w", err)
	}

	output := fmt.Sprintf(`指标采集结果：
查询: %s
时间范围: %s
数据点数: %d
异常点数: %d
`,
		result.Query,
		result.TimeRange,
		len(result.Data),
		len(result.Anomalies),
	)

	if len(result.Anomalies) > 0 {
		output += "\n检测到异常：\n"
		for i, anomaly := range result.Anomalies {
			output += fmt.Sprintf("  %d. 时间: %s, 值: %.2f, 评分: %.2f\n",
				i+1, anomaly.Timestamp.Format("15:04:05"), anomaly.Value, anomaly.Score)
		}
	}

	return output, nil
}

// LogAnalyzerTool 日志分析工具
type LogAnalyzerTool struct {
	agent *OpsAgent
}

// NewLogAnalyzerTool 创建日志分析工具
func NewLogAnalyzerTool(agent *OpsAgent) *LogAnalyzerTool {
	return &LogAnalyzerTool{agent: agent}
}

// Info 返回工具信息
func (t *LogAnalyzerTool) Info(ctx context.Context) (*compose.ToolInfo, error) {
	return &compose.ToolInfo{
		Name: "log_analyzer",
		Desc: "分析日志数据。输入：日志源、查询语句、时间范围。输出：日志分析结果（错误数、警告数、Top 错误）。",
		ParamsOneOf: compose.NewParamsOneOfByParams(
			map[string]*compose.ParameterInfo{
				"source": {
					Type:     compose.String,
					Desc:     "日志源（loki/elasticsearch）",
					Required: true,
				},
				"query": {
					Type:     compose.String,
					Desc:     "查询语句",
					Required: true,
				},
				"time_range": {
					Type:     compose.String,
					Desc:     "时间范围（如 5m, 1h, 1d）",
					Required: false,
				},
			},
		),
	}, nil
}

// InvokableRun 执行工具
func (t *LogAnalyzerTool) InvokableRun(ctx context.Context, input string, opts ...compose.ToolOption) (string, error) {
	var params struct {
		Source    string `json:"source"`
		Query     string `json:"query"`
		TimeRange string `json:"time_range"`
	}

	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	if params.TimeRange == "" {
		params.TimeRange = "5m"
	}

	result, err := t.agent.AnalyzeLogs(ctx, params.Source, params.Query, params.TimeRange)
	if err != nil {
		return "", fmt.Errorf("log analysis failed: %w", err)
	}

	output := fmt.Sprintf(`日志分析结果：
日志源: %s
查询: %s
时间范围: %s
总日志数: %d
错误数: %d
警告数: %d
`,
		result.Source,
		result.Query,
		result.TimeRange,
		result.TotalCount,
		result.ErrorCount,
		result.WarningCount,
	)

	if len(result.TopErrors) > 0 {
		output += "\nTop 错误：\n"
		for i, err := range result.TopErrors {
			output += fmt.Sprintf("  %d. %s (出现 %d 次)\n", i+1, err.Message, err.Count)
		}
	}

	return output, nil
}

// HealthCheckTool 健康检查工具
type HealthCheckTool struct {
	agent *OpsAgent
}

// NewHealthCheckTool 创建健康检查工具
func NewHealthCheckTool(agent *OpsAgent) *HealthCheckTool {
	return &HealthCheckTool{agent: agent}
}

// Info 返回工具信息
func (t *HealthCheckTool) Info(ctx context.Context) (*compose.ToolInfo, error) {
	return &compose.ToolInfo{
		Name: "health_check",
		Desc: "执行健康检查。输入：检查类型（http/tcp/grpc）和端点。输出：健康状态和延迟。",
		ParamsOneOf: compose.NewParamsOneOfByParams(
			map[string]*compose.ParameterInfo{
				"type": {
					Type:     compose.String,
					Desc:     "检查类型（http/tcp/grpc）",
					Required: true,
				},
				"endpoint": {
					Type:     compose.String,
					Desc:     "端点地址",
					Required: true,
				},
			},
		),
	}, nil
}

// InvokableRun 执行工具
func (t *HealthCheckTool) InvokableRun(ctx context.Context, input string, opts ...compose.ToolOption) (string, error) {
	var params struct {
		Type     string `json:"type"`
		Endpoint string `json:"endpoint"`
	}

	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	target := &HealthCheckTarget{
		Type:     params.Type,
		Endpoint: params.Endpoint,
		Timeout:  5 * time.Second,
	}

	result, err := t.agent.CheckHealth(ctx, target)
	if err != nil {
		return "", fmt.Errorf("health check failed: %w", err)
	}

	output := fmt.Sprintf(`健康检查结果：
类型: %s
端点: %s
健康状态: %v
延迟: %v
消息: %s
`,
		params.Type,
		params.Endpoint,
		result.Healthy,
		result.Latency,
		result.Message,
	)

	return output, nil
}

// SystemDiagnosisTool 系统诊断工具
type SystemDiagnosisTool struct {
	agent *OpsAgent
}

// NewSystemDiagnosisTool 创建系统诊断工具
func NewSystemDiagnosisTool(agent *OpsAgent) *SystemDiagnosisTool {
	return &SystemDiagnosisTool{agent: agent}
}

// Info 返回工具信息
func (t *SystemDiagnosisTool) Info(ctx context.Context) (*compose.ToolInfo, error) {
	return &compose.ToolInfo{
		Name: "system_diagnosis",
		Desc: "执行系统综合诊断（K8s + 指标 + 日志 + 健康检查）。输入：命名空间和服务名。输出：完整的诊断报告。",
		ParamsOneOf: compose.NewParamsOneOfByParams(
			map[string]*compose.ParameterInfo{
				"namespace": {
					Type:     compose.String,
					Desc:     "命名空间",
					Required: true,
				},
				"service": {
					Type:     compose.String,
					Desc:     "服务名",
					Required: true,
				},
			},
		),
	}, nil
}

// InvokableRun 执行工具
func (t *SystemDiagnosisTool) InvokableRun(ctx context.Context, input string, opts ...compose.ToolOption) (string, error) {
	var params struct {
		Namespace string `json:"namespace"`
		Service   string `json:"service"`
	}

	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	target := &DiagnosisTarget{
		Namespace:    params.Namespace,
		Service:      params.Service,
		CheckK8s:     true,
		CheckMetrics: true,
		CheckLogs:    true,
	}

	report, err := t.agent.DiagnoseSystem(ctx, target)
	if err != nil {
		return "", fmt.Errorf("system diagnosis failed: %w", err)
	}

	output := fmt.Sprintf(`系统诊断报告：
命名空间: %s
服务: %s
整体健康度: %.2f
问题数量: %d
`,
		params.Namespace,
		params.Service,
		report.OverallHealth,
		len(report.Issues),
	)

	if len(report.Issues) > 0 {
		output += "\n发现的问题：\n"
		for i, issue := range report.Issues {
			output += fmt.Sprintf("  %d. [%s] %s: %s\n",
				i+1, issue.Severity, issue.Type, issue.Message)
		}
	} else {
		output += "\n未发现问题，系统运行正常。\n"
	}

	return output, nil
}
