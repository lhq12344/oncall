package ops

import (
	"context"
	"fmt"

	"go_agent/internal/ai/models"
	"go_agent/internal/compact"
	executiontools "go_agent/internal/execution/tools"
	"go_agent/internal/permissions"
	"go_agent/internal/prompt"
	"go_agent/internal/toolkit"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"go.uber.org/zap"
)

const defaultPlanAgentMaxIterations = 48

// PlanAgentConfig contains dependencies for the workflow-level plan agent.
type PlanAgentConfig struct {
	ChatModel *models.ChatModel
	Logger    *zap.Logger
}

// NewPlanAgent creates the dedicated planner stage. It may normalize, generate,
// and pre-validate an ExecutionPlan, but it never receives execute/rollback
// tools. Graph State remains the canonical source after wrapWithIncidentState.
func NewPlanAgent(ctx context.Context, cfg *PlanAgentConfig) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	deferredTools := []tool.BaseTool{
		executiontools.NewNormalizePlanTool(cfg.ChatModel, cfg.Logger),
		executiontools.NewGeneratePlanTool(cfg.ChatModel, cfg.Logger),
		executiontools.NewValidatePlanTool(cfg.Logger),
	}
	checker := permissions.NewChecker(permissions.Options{})
	toolsList := toolkit.BuildDeferredGatewayEinoTools(ctx, checker, deferredTools...)

	env := prompt.DetectEnvironment("")
	instruction := prompt.BuildAgentPrompt(prompt.RolePlan, env, prompt.BuildOptions{})

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "plan_agent",
		Description:   "生成 canonical ExecutionPlan 并做计划级预检的规划代理",
		Model:         cfg.ChatModel.Client,
		GenModelInput: noFormatGenModelInput,
		MaxIterations: defaultPlanAgentMaxIterations,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Handlers:    []adk.ChatModelAgentMiddleware{compact.NewMiddleware(compact.Config{Model: cfg.ChatModel.Client})},
		Instruction: instruction,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create plan agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("plan agent initialized with planner-only deferred tools", zap.Int("deferred_tools", len(deferredTools)))
	}
	return agent, nil
}
