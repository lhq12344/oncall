package strategy

import (
	"context"
	"fmt"

	"go_agent/internal/agent/strategy/tools"
	aiindexer "go_agent/internal/ai/indexer"
	"go_agent/internal/ai/models"
	"go_agent/utility/common"

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
	opsCaseIndexer, err := aiindexer.NewMilvusIndexerWithCollection(ctx, common.MilvusOpsCollection)
	if err != nil && cfg.Logger != nil {
		cfg.Logger.Warn("failed to initialize ops case indexer, continue without archive", zap.Error(err))
	}
	updateTool := tools.NewUpdateKnowledgeTool(opsCaseIndexer, cfg.Logger)
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
		Instruction: `你是故障复盘与学习代理，负责输出面向用户的最终修复报告，并沉淀可复用经验。

输入会包含：
- RCA 结构化报告
- Ops 计划与 Validator 风险判断
- Execution 执行记录与失败信息
- 用户确认结果

你的职责：
1. 使用 evaluate_strategy 评估本次处置质量。
2. 使用 optimize_strategy 给出后续优化建议。
3. 使用 update_knowledge 写入成功经验（作为下次检索样本）。
4. 使用 prune_knowledge 清理低价值或过时经验。

输出要求（最终必须输出一个 JSON 对象）：
{
  "summary": "最终结论",
  "root_cause": "根因",
  "actions_taken": ["动作1", "动作2"],
  "final_status": "resolved|partially_resolved|unresolved",
  "what_worked": ["有效动作"],
  "what_failed": ["失败动作"],
  "prevention": ["防复发建议1", "防复发建议2"],
  "knowledge_updated": true
}

约束：
- 结论必须与 execution 结果一致。
- 若 final_status != resolved，必须给出人工后续操作建议。
- 输出必须可解析，不要附加多余文本。`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create strategy agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("strategy agent initialized with 4 tools")
	}

	return agent, nil
}
