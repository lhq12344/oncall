package rca

import (
	"context"
	"fmt"
	"strings"

	"go_agent/internal/ai/models"
	"go_agent/internal/context/compact/runtime"
	"go_agent/internal/prompt"
	toolregistry "go_agent/internal/tools"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type Config struct {
	ChatModel     *models.ChatModel
	KubeConfig    string
	PrometheusURL string
	Logger        *zap.Logger
}

func NewRCAAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	toolsList, err := toolregistry.NewRegistry(toolregistry.Dependencies{
		ChatModel:     cfg.ChatModel,
		KubeConfig:    cfg.KubeConfig,
		PrometheusURL: cfg.PrometheusURL,
		Logger:        cfg.Logger,
	}).ExecutableToolsForAgent(ctx, toolregistry.AgentRCA, toolregistry.ToolExposureAlways)
	if err != nil {
		return nil, fmt.Errorf("build rca tools: %w", err)
	}

	env := prompt.DetectEnvironment("")
	instruction := prompt.BuildAgentPrompt(prompt.RoleRCA, env, prompt.BuildOptions{})

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
		Handlers:    []adk.ChatModelAgentMiddleware{compact.NewMiddleware(compact.Config{Model: cfg.ChatModel.Client})},
		Instruction: instruction,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create rca agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("rca agent initialized", zap.Int("executable_tools", len(toolsList)))
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
