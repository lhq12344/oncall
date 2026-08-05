package ops

import (
	"context"
	"fmt"
	"strings"

	"go_agent/internal/ai/models"
	"go_agent/internal/compact"
	"go_agent/internal/prompt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config contains dependencies for the remediation planning agent.
type Config struct {
	ChatModel     *models.ChatModel
	KubeConfig    string
	PrometheusURL string
	Logger        *zap.Logger
}

// NewOpsAgent creates the remediation planning agent.
func NewOpsAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	env := prompt.DetectEnvironment("")
	instruction := prompt.BuildAgentPrompt(prompt.RoleOps, env, prompt.BuildOptions{})

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "ops_agent",
		Description:   "基于 RCA 与执行反馈生成修复策略提案的运维代理",
		Model:         cfg.ChatModel.Client,
		GenModelInput: noFormatGenModelInput,
		Handlers:      []adk.ChatModelAgentMiddleware{compact.NewMiddleware(compact.Config{Model: cfg.ChatModel.Client})},
		Instruction:   instruction,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ops agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("ops agent initialized as remediation planner")
	}
	return agent, nil
}

// noFormatGenModelInput builds model input without template formatting.
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
