package strategy

import (
	"context"
	"fmt"
	"strings"

	"go_agent/internal/agent/strategy/tools"
	aiindexer "go_agent/internal/ai/indexer"
	"go_agent/internal/ai/models"
	"go_agent/internal/prompt"
	"go_agent/utility/common"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
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
	env := prompt.DetectEnvironment("")
	instruction := prompt.BuildAgentPrompt(prompt.RoleStrategy, env, prompt.BuildOptions{})

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "strategy_agent",
		Description:   "评估和优化执行策略的策略代理",
		Model:         cfg.ChatModel.Client,
		GenModelInput: noFormatGenModelInput,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Instruction: instruction,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create strategy agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("strategy agent initialized with 4 tools")
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
