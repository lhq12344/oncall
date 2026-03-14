package rca

import (
	"context"
	"fmt"
	"strings"

	opstools "go_agent/internal/agent/ops/tools"
	"go_agent/internal/agent/rca/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config RCA Agent 配置
type Config struct {
	ChatModel     *models.ChatModel
	KubeConfig    string
	PrometheusURL string
	Logger        *zap.Logger
}

// NewRCAAgent 创建 RCA Agent（根因分析）
func NewRCAAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	// 创建工具集
	var toolsList []tool.BaseTool

	// K8s 监控工具
	k8sTool, err := opstools.NewK8sMonitorTool(cfg.KubeConfig, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create rca k8s monitor tool: %w", err)
	}
	toolsList = append(toolsList, k8sTool)

	// Prometheus 指标采集工具
	metricsTool, err := opstools.NewMetricsCollectorTool(cfg.PrometheusURL, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create rca metrics collector tool: %w", err)
	}
	toolsList = append(toolsList, metricsTool)

	// 时间查询工具
	timeTool := tools.NewTimeQueryTool(cfg.Logger)
	toolsList = append(toolsList, timeTool)

	// 依赖图构建工具
	depGraphTool := tools.NewBuildDependencyGraphTool(cfg.KubeConfig, cfg.Logger)
	toolsList = append(toolsList, depGraphTool)

	// 信号关联工具
	correlateTool := tools.NewCorrelateSignalsTool(cfg.Logger)
	toolsList = append(toolsList, correlateTool)

	// 根因推理工具
	inferenceTool := tools.NewInferRootCauseTool(cfg.ChatModel, cfg.Logger)
	toolsList = append(toolsList, inferenceTool)

	// 影响分析工具
	impactTool := tools.NewAnalyzeImpactTool(cfg.Logger)
	toolsList = append(toolsList, impactTool)

	// 创建 ChatModelAgent
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "rca_agent",
		Description:   "分析故障根因和影响范围的根因分析代理",
		Model:         cfg.ChatModel.Client,
		GenModelInput: noFormatGenModelInput,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Instruction: `你是 RCA 根因分析代理，负责产出“可被下游 Ops 直接消费”的结构化结果。

输入中会包含 Graph State 的观测快照（observation_summary / observation_errors / observation_namespace / observation_collected_at / observation_time_range），必须先基于这些事实再推理。

你的职责：
1. 先检查观测快照是否足够、是否过期；必要时使用 time_query 对齐当前时间、故障窗口与观测时间。
2. 当观测快照缺少 Kubernetes 现场信息时，使用 k8s_monitor 补充 Pod / Node / Deployment / StatefulSet / Service 状态。
3. 当需要确认资源压力、重启趋势或历史指标时，使用 metrics_collector 执行 PromQL 查询。
4. 使用 build_dependency_graph 构建依赖拓扑。
5. 使用 correlate_signals 对齐告警/日志/指标信号。
6. 使用 infer_root_cause 推理最可能根因。
7. 使用 analyze_impact 评估影响面。

					输出规范（必须只输出一个 JSON 对象，不要附加解释）：
					{
					"root_cause": "根因标签，如 disk_full / downstream_timeout",
					"target_node": "受影响主机或服务，如 worker-01 / payment-api",
					"path": "关键路径，如 /var/log 或 serviceA->serviceB",
  "impact": "影响摘要",
  "confidence": 0.0,
  "evidence": ["证据1", "证据2", "证据3"],
  "next_verification": ["建议验证动作1", "建议验证动作2"],
  "missing_data": ["缺失数据1", "缺失数据2"]
}

约束：
- confidence 范围必须是 0~1。
- evidence 至少提供 2 条可审计证据。
- 若 observation_errors 非空或证据不足，confidence 必须小于 0.6，且必须填写 missing_data。
- 若 observation_collected_at 距当前时间过远、观测窗口与故障时间不一致，必须优先调用 time_query，并在 evidence 或 missing_data 中说明时效性问题。
- 若仅凭 observation_summary 无法确认当前现场状态，必须优先调用 k8s_monitor 或 metrics_collector 补充事实，再继续根因推理。
- 相同工具在相同参数下禁止重复调用，除非上一步返回错误且明确给出重试原因。
- 无法确定时输出低 confidence 并明确缺失信息，不得臆造。`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create rca agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("rca agent initialized with 7 tools")
	}

	return agent, nil
}

// noFormatGenModelInput 构建模型输入消息，不对 instruction 执行 FString 变量替换。
// 输入：instruction 系统提示词，input 用户/历史消息。
// 输出：拼接后的模型消息列表（system + input.Messages）。
func noFormatGenModelInput(_ context.Context, instruction string, input *adk.AgentInput) ([]adk.Message, error) {
	msgs := make([]adk.Message, 0, 1)
	if strings.TrimSpace(instruction) != "" {
		msgs = append(msgs, schema.SystemMessage(instruction))
	}
	if input != nil && len(input.Messages) > 0 {
		msgs = append(msgs, input.Messages...)
	}
	return msgs, nil
}
