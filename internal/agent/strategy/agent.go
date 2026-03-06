package strategy

import (
	"context"
	"fmt"

	"go_agent/internal/agent/strategy/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"go.uber.org/zap"
)

// Config Strategy Agent 配置
type Config struct {
	ChatModel *models.ChatModel
	Logger    *zap.Logger
}

// NewStrategyAgent 创建 Strategy Agent（策略评估和优化）
func NewStrategyAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	// 创建工具集
	var toolsList []tool.BaseTool

	// 策略评估工具
	evaluateTool := tools.NewEvaluateStrategyTool(cfg.Logger)
	toolsList = append(toolsList, evaluateTool)

	// 策略优化工具
	optimizeTool := tools.NewOptimizeStrategyTool(cfg.ChatModel, cfg.Logger)
	toolsList = append(toolsList, optimizeTool)

	// 知识库更新工具
	updateTool := tools.NewUpdateKnowledgeTool(cfg.Logger)
	toolsList = append(toolsList, updateTool)

	// 知识剪枝工具
	pruneTool := tools.NewPruneKnowledgeTool(cfg.Logger)
	toolsList = append(toolsList, pruneTool)

	// 创建 ChatModelAgent
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "strategy_agent",
		Description: "评估和优化执行策略的策略代理",
		Model:       cfg.ChatModel.Client,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Instruction: `你是一个策略优化助手，负责评估和优化执行策略。

你的职责：
1. 使用 evaluate_strategy 工具评估执行策略的质量
2. 使用 optimize_strategy 工具优化执行策略
3. 使用 update_knowledge 工具更新知识库
4. 使用 prune_knowledge 工具清理低质量知识

评估维度：
- 成功率：策略执行的成功率
- 执行时长：策略执行的平均时长
- 回滚次数：策略执行失败需要回滚的次数
- 资源消耗：策略执行的资源消耗

优化方法：
- 识别低效步骤：找出执行时间长、失败率高的步骤
- 并行化：识别可以并行执行的步骤
- 简化：删除不必要的步骤
- 参数调优：优化超时时间、重试次数等参数

知识管理：
- 保存成功案例：成功率高、执行时间短的策略
- 更新案例权重：根据使用频率和成功率调整权重
- 删除低质量知识：删除过时、低成功率的案例
- 合并相似案例：合并重复或相似的案例

注意事项：
- 基于数据做决策，不要主观臆断
- 保持知识库的质量和时效性
- 定期清理过期数据
- 记录优化历史`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create strategy agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("strategy agent initialized with 4 tools")
	}

	return agent, nil
}
