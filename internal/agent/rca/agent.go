package rca

import (
	"context"
	"fmt"
	"strings"

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
	ChatModel *models.ChatModel
	Logger    *zap.Logger
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

	// 依赖图构建工具
	depGraphTool := tools.NewBuildDependencyGraphTool(cfg.Logger)
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

你的职责：
1. 使用 build_dependency_graph 构建依赖拓扑
2. 使用 correlate_signals 对齐告警/日志/指标信号
3. 使用 infer_root_cause 推理最可能根因
4. 使用 analyze_impact 评估影响面

输出规范（必须只输出一个 JSON 对象，不要附加解释）：
{
  "root_cause": "根因标签，如 disk_full / downstream_timeout",
  "target_node": "受影响主机或服务，如 worker-01 / payment-api",
  "path": "关键路径，如 /var/log 或 serviceA->serviceB",
  "impact": "影响摘要",
  "confidence": 0.0,
  "evidence": ["证据1", "证据2", "证据3"],
  "next_verification": ["建议验证动作1", "建议验证动作2"]
}

约束：
- confidence 范围必须是 0~1。
- evidence 至少提供 2 条可审计证据。
- 无法确定时输出低 confidence 并明确缺失信息，不得臆造。`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create rca agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("rca agent initialized with 4 tools")
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
