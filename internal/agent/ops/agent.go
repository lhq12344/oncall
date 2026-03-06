package ops

import (
	"context"
	"fmt"

	"go_agent/internal/agent/ops/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"go.uber.org/zap"
)

// Config Ops Agent 配置
type Config struct {
	ChatModel     *models.ChatModel
	KubeConfig    string // K8s kubeconfig 文件路径
	PrometheusURL string
	Logger        *zap.Logger
}

// NewOpsAgent 创建 Ops Agent（监控 + 异常检测）
func NewOpsAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	// 创建工具集
	var toolsList []tool.BaseTool

	// K8s 监控工具
	k8sTool, err := tools.NewK8sMonitorTool(cfg.KubeConfig, cfg.Logger)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Warn("failed to create k8s monitor tool", zap.Error(err))
		}
	} else {
		toolsList = append(toolsList, k8sTool)
	}

	// Prometheus 指标采集工具
	metricsTool, err := tools.NewMetricsCollectorTool(cfg.PrometheusURL, cfg.Logger)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Warn("failed to create metrics collector tool", zap.Error(err))
		}
	} else {
		toolsList = append(toolsList, metricsTool)
	}

	// 日志分析工具
	logTool := tools.NewLogAnalyzerTool(cfg.Logger)
	toolsList = append(toolsList, logTool)

	if len(toolsList) == 0 {
		return nil, fmt.Errorf("no tools available for ops agent")
	}

	// 创建 ChatModelAgent
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "ops_agent",
		Description: "监控系统状态和检测异常的运维代理",
		Model:       cfg.ChatModel.Client,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Instruction: `你是一个运维助手，负责监控系统状态和检测异常。

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
