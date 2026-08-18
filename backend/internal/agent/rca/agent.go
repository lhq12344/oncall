package rca

import (
	"context"
	"fmt"
	"strings"

	"go_agent/internal/agent/rca/tools"
	"go_agent/internal/ai/models"
	"go_agent/internal/compact"
	"go_agent/internal/permissions"
	"go_agent/internal/prompt"
	"go_agent/internal/toolkit"
	opstools "go_agent/internal/workflow/ops/tools"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config RCA Agent 配置
type Config struct {
	ChatModel     *models.ChatModel
	KubeConfig    string
	PrometheusURL string
	Logger        *zap.Logger
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
	var deferredTools []tool.BaseTool

	// K8s 监控工具
	k8sTool, err := opstools.NewK8sMonitorTool(cfg.KubeConfig, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create rca k8s monitor tool: %w", err)
	}
	deferredTools = append(deferredTools, k8sTool)

	// Prometheus 指标采集工具
	metricsTool, err := opstools.NewMetricsCollectorTool(cfg.PrometheusURL, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create rca metrics collector tool: %w", err)
	}
	deferredTools = append(deferredTools, metricsTool)

	// 时间查询工具
	timeTool := tools.NewTimeQueryTool(cfg.Logger)
	deferredTools = append(deferredTools, timeTool)

	// 依赖图构建工具
	depGraphTool := tools.NewBuildDependencyGraphTool(cfg.KubeConfig, cfg.Logger)
	deferredTools = append(deferredTools, depGraphTool)

	// 信号关联工具
	correlateTool := tools.NewCorrelateSignalsTool(cfg.Logger)
	deferredTools = append(deferredTools, correlateTool)

	// 根因推理工具
	inferenceTool := tools.NewInferRootCauseTool(cfg.ChatModel, cfg.Logger)
	deferredTools = append(deferredTools, inferenceTool)

	// 影响分析工具
	impactTool := tools.NewAnalyzeImpactTool(cfg.Logger)
	deferredTools = append(deferredTools, impactTool)

	// 创建 ChatModelAgent
	checker := permissions.NewChecker(permissions.Options{})
	toolsList := toolkit.BuildAlwaysEinoTools(ctx, checker, deferredTools...)

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
		cfg.Logger.Info("rca agent initialized with 7 tools")
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
