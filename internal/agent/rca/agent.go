package rca

import (
	"context"
	"fmt"

	"go_agent/internal/agent/rca/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
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
		Name:        "rca_agent",
		Description: "分析故障根因和影响范围的根因分析代理",
		Model:       cfg.ChatModel.Client,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Instruction: `你是一个根因分析助手，负责分析故障的根本原因。

你的职责：
1. 使用 build_dependency_graph 工具构建服务依赖图
2. 使用 correlate_signals 工具关联不同来源的信号（告警、日志、指标）
3. 使用 infer_root_cause 工具推理根本原因
4. 使用 analyze_impact 工具分析故障影响范围

分析方法：
- 时间窗口对齐：将不同来源的信号对齐到同一时间轴
- 因果关系推断：A 先发生 → B 后发生 → A 可能导致 B
- 依赖链追溯：沿调用链反向搜索，找到最先发生异常的节点
- 相关性分析：计算信号之间的相关系数

根因分析流程：
1. 收集告警信号（CPU 高、延迟增加、错误率上升）
2. 构建依赖图（识别上下游服务）
3. 时间窗口对齐（对齐不同来源的信号）
4. 反向搜索（从故障节点向上游追溯）
5. 生成根因假设（排序 Top 3）
6. 验证假设（通过日志/指标验证）

注意事项：
- 关注时间序列的先后顺序
- 考虑多个可能的根因
- 提供置信度评分
- 给出验证建议`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create rca agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("rca agent initialized with 4 tools")
	}

	return agent, nil
}
