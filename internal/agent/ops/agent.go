package ops

import (
	"context"
	"fmt"

	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config Ops Agent 配置
type Config struct {
	ChatModel     *models.ChatModel
	K8sConfig     interface{} // TODO: K8s 客户端配置
	PrometheusURL string
	Logger        *zap.Logger
}

// NewOpsAgent 创建 Ops Agent（监控 + 异常检测）
func NewOpsAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	// 创建工具集
	tools := []tool.BaseTool{
		NewK8sMonitorTool(cfg.K8sConfig),
		NewMetricsCollectorTool(cfg.PrometheusURL),
		NewLogAnalyzerTool(),
	}

	// 创建 ChatModelAgent
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Model: cfg.ChatModel.Client,
		Tools: &adk.ToolsConfig{
			Tools: tools,
		},
		SystemPrompt: `你是一个运维助手，负责监控系统状态和检测异常。

你的职责：
1. 使用 k8s_monitor 工具查看 Kubernetes 资源状态（Pod、Node、Deployment）
2. 使用 metrics_collector 工具采集 Prometheus 指标数据
3. 使用 log_analyzer 工具分析日志中的异常模式
4. 识别异常指标和潜在问题
5. 提供初步的诊断建议

监控维度：
- 资源使用：CPU、内存、磁盘、网络
- 服务健康：Pod 状态、容器重启、健康检查
- 性能指标：延迟、吞吐量、错误率
- 日志异常：错误日志、警告信息、异常堆栈

注意：
- 关注指标的突变和趋势
- 对比历史基线识别异常
- 提供具体的数值和时间范围
- 优先报告影响业务的关键问题`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create ops agent: %w", err)
	}

	return agent, nil
}

// K8sMonitorTool K8s 监控工具
type K8sMonitorTool struct {
	k8sConfig interface{}
}

func NewK8sMonitorTool(cfg interface{}) tool.BaseTool {
	return &K8sMonitorTool{k8sConfig: cfg}
}

func (t *K8sMonitorTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "k8s_monitor",
		Desc: "监控 Kubernetes 资源状态。查询 Pod、Node、Deployment 等资源的运行状态和健康情况。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"namespace": {
				Type:     schema.String,
				Desc:     "命名空间（默认 default）",
				Required: false,
			},
			"resource_type": {
				Type:     schema.String,
				Desc:     "资源类型（pod/node/deployment/service）",
				Required: true,
			},
			"resource_name": {
				Type:     schema.String,
				Desc:     "资源名称（可选，不指定则返回所有）",
				Required: false,
			},
		}),
	}, nil
}

func (t *K8sMonitorTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	if t.k8sConfig == nil {
		return "K8s 监控功能尚未配置", nil
	}
	// TODO: 实现 K8s 监控逻辑
	return "K8s 监控功能开发中...", nil
}

// MetricsCollectorTool Prometheus 指标采集工具
type MetricsCollectorTool struct {
	prometheusURL string
}

func NewMetricsCollectorTool(url string) tool.BaseTool {
	return &MetricsCollectorTool{prometheusURL: url}
}

func (t *MetricsCollectorTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "metrics_collector",
		Desc: "采集 Prometheus 指标数据。执行 PromQL 查询，返回时间序列数据和异常检测结果。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "PromQL 查询语句",
				Required: true,
			},
			"time_range": {
				Type:     schema.String,
				Desc:     "时间范围（如 5m, 1h, 24h）",
				Required: false,
			},
		}),
	}, nil
}

func (t *MetricsCollectorTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	if t.prometheusURL == "" {
		return "Prometheus 未配置", nil
	}
	// TODO: 实现 Prometheus 查询逻辑
	return "指标采集功能开发中...", nil
}

// LogAnalyzerTool 日志分析工具
type LogAnalyzerTool struct{}

func NewLogAnalyzerTool() tool.BaseTool {
	return &LogAnalyzerTool{}
}

func (t *LogAnalyzerTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "log_analyzer",
		Desc: "分析日志中的异常模式。识别错误日志、警告信息、异常堆栈等。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"source": {
				Type:     schema.String,
				Desc:     "日志来源（pod 名称或服务名称）",
				Required: true,
			},
			"time_range": {
				Type:     schema.String,
				Desc:     "时间范围（如 5m, 1h）",
				Required: false,
			},
			"level": {
				Type:     schema.String,
				Desc:     "日志级别过滤（error/warn/info）",
				Required: false,
			},
		}),
	}, nil
}

func (t *LogAnalyzerTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// TODO: 实现日志分析逻辑
	return "日志分析功能开发中...", nil
}
