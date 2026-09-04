package strategy

import (
	"context"
	"fmt"
	"strings"

	aiindexer "go_agent/internal/ai/indexer"
	"go_agent/internal/ai/models"
	"go_agent/internal/context/compact/runtime"
	"go_agent/internal/prompt"
	toolregistry "go_agent/internal/tools"
	"go_agent/utility/common"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type Config struct {
	ChatModel *models.ChatModel
	Logger    *zap.Logger
}

func NewStrategyAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	opsCaseIndexer, err := aiindexer.NewMilvusIndexerWithCollection(ctx, common.MilvusOpsCollection)
	if err != nil && cfg.Logger != nil {
		cfg.Logger.Warn("failed to initialize ops case indexer, continue without archive", zap.Error(err))
	}
	toolsList, err := toolregistry.NewRegistry(toolregistry.Dependencies{
		ChatModel:      cfg.ChatModel,
		OpsCaseIndexer: opsCaseIndexer,
		Logger:         cfg.Logger,
	}).ExecutableToolsForAgent(ctx, toolregistry.AgentStrategy, toolregistry.ToolExposureAlways)
	if err != nil {
		return nil, fmt.Errorf("build strategy tools: %w", err)
	}

	env := prompt.DetectEnvironment("")
	instruction := prompt.BuildAgentPrompt(prompt.RoleStrategy, env, prompt.BuildOptions{})

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "strategy_agent",
		Description:   "Evaluates and optimizes execution strategies.",
		Model:         cfg.ChatModel.Client,
		GenModelInput: noFormatGenModelInput,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Handlers:    []adk.ChatModelAgentMiddleware{compact.NewMiddleware(compact.Config{Model: cfg.ChatModel.Client})},
		Instruction: instruction,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create strategy agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("strategy agent initialized", zap.Int("executable_tools", len(toolsList)))
	}
	return agent, nil
}

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
