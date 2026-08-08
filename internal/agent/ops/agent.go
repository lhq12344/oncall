package ops

import (
	"context"
	"fmt"
	"strings"

	"go_agent/internal/agent/toolkit"
	"go_agent/internal/ai/models"
	"go_agent/internal/compact"
	"go_agent/internal/permissions"
	"go_agent/internal/prompt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config contains dependencies for the incident diagnosis and remediation planning agent.
type Config struct {
	ChatModel     *models.ChatModel
	KubeConfig    string
	PrometheusURL string
	Logger        *zap.Logger
}

const defaultOpsIncidentAgentMaxIterations = 48

// NewOpsAgent creates the incident agent that owns diagnosis, evidence collection,
// root-cause reasoning, and remediation proposal generation. The workflow keeps
// recovery/audit and execution boundaries; this agent decides which read-only
// diagnostic tools to call on demand.
func NewOpsAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	deferredTools, err := buildOpsIncidentDeferredTools(cfg)
	if err != nil {
		return nil, err
	}
	checker := permissions.NewChecker(permissions.Options{})
	toolsList := toolkit.BuildDeferredGatewayEinoTools(ctx, checker, deferredTools...)

	env := prompt.DetectEnvironment("")
	instruction := prompt.BuildAgentPrompt(prompt.RoleOps, env, prompt.BuildOptions{})

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "ops_incident_agent",
		Description:   "按需调用观测工具完成故障诊断、根因判断和修复提案生成的运维代理",
		Model:         cfg.ChatModel.Client,
		GenModelInput: noFormatGenModelInput,
		MaxIterations: defaultOpsIncidentAgentMaxIterations,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Handlers:    []adk.ChatModelAgentMiddleware{compact.NewMiddleware(compact.Config{Model: cfg.ChatModel.Client})},
		Instruction: instruction,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ops incident agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("ops incident agent initialized with deferred diagnostic tools",
			zap.Int("deferred_tools", len(deferredTools)))
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
