package ops

import (
	"context"
	"fmt"

	"go_agent/internal/agent/ops/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/prebuilt/planexecute"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config Ops Agent 配置
type Config struct {
	ChatModel     *models.ChatModel
	KubeConfig    string // K8s kubeconfig 文件路径
	PrometheusURL string
	Logger        *zap.Logger
}

// NewOpsAgent 创建 Ops Agent（监控 + 异常检测）
func NewOpsAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	toolsList := buildOpsTools(cfg)

	if len(toolsList) == 0 {
		return nil, fmt.Errorf("no tools available for ops agent")
	}

	// 创建 Planner - 负责制定诊断计划
	// 注意：必须明确告知 Planner 可用的工具列表
	planner, err := planexecute.NewPlanner(ctx, &planexecute.PlannerConfig{
		ToolCallingChatModel: cfg.ChatModel.Client,
		GenInputFn: func(ctx context.Context, userInput []adk.Message) ([]adk.Message, error) {
			// 构建包含工具列表的系统提示
			systemPrompt := `你是一个运维诊断规划专家。根据用户的运维需求，制定详细的诊断计划。

				可用工具（只能使用以下工具）：
				1. k8s_monitor - 查看 Kubernetes 资源状态（Pod、Node、Deployment）
				2. metrics_collector - 采集 Prometheus 指标数据（CPU、内存、网络等）
				3. es_log_query - 查询 Elasticsearch 日志（支持关键词、时间范围、日志级别过滤）
				4. log_analyzer - 分析日志模式（备用）

				重要：不要使用任何其他工具（如 execute_command、shell_exec 等），只使用上述 4 个工具。

				规划原则：
				1. 先收集基础状态（K8s 资源、关键指标）
				2. 根据初步结果，针对性查询日志或详细指标
				3. 每个步骤要明确目标和预期输出
				4. 步骤之间要有逻辑关联`

			messages := []adk.Message{
				{
					Role:    schema.System,
					Content: systemPrompt,
				},
			}

			// 添加用户输入
			messages = append(messages, userInput...)

			return messages, nil
		},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create planner: %w", err)
	}

	// 创建 Executor - 负责执行计划中的工具调用
	executor, err := planexecute.NewExecutor(ctx, &planexecute.ExecutorConfig{
		Model: cfg.ChatModel.Client,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		MaxIterations: 20,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create executor: %w", err)
	}

	// 创建 Replanner - 负责根据执行结果决定是否重新规划
	replanner, err := planexecute.NewReplanner(ctx, &planexecute.ReplannerConfig{
		ChatModel: cfg.ChatModel.Client,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create replanner: %w", err)
	}

	// 组装 Plan-Execute-Replan Agent
	agent, err := planexecute.New(ctx, &planexecute.Config{
		Planner:       planner,
		Executor:      executor,
		Replanner:     replanner,
		MaxIterations: 10, // 最多执行 10 轮 plan-execute-replan 循环
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create plan-execute-replan ops agent: %w", err)
	}

	// 包装成带名称的 Agent（用于 supervisor 识别）
	namedAgent := &NamedAgent{
		Agent: agent,
		name:  "ops_agent",
		desc:  "监控系统状态、采集指标、分析日志的运维代理",
	}

	return namedAgent, nil
}

func buildOpsTools(cfg *Config) []tool.BaseTool {
	var toolsList []tool.BaseTool

	k8sTool, err := tools.NewK8sMonitorTool(cfg.KubeConfig, cfg.Logger)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Warn("failed to create k8s monitor tool", zap.Error(err))
		}
	} else {
		toolsList = append(toolsList, k8sTool)
	}

	metricsTool, err := tools.NewMetricsCollectorTool(cfg.PrometheusURL, cfg.Logger)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Warn("failed to create metrics collector tool", zap.Error(err))
		}
	} else {
		toolsList = append(toolsList, metricsTool)
	}

	esLogTool, err := tools.NewESLogQueryTool(cfg.Logger)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Warn("failed to create es log query tool", zap.Error(err))
		}
	} else {
		toolsList = append(toolsList, esLogTool)
	}

	logTool := tools.NewLogAnalyzerTool(cfg.Logger)
	toolsList = append(toolsList, logTool)

	return toolsList
}

// NamedAgent 包装 Agent 并添加名称和描述
type NamedAgent struct {
	adk.Agent
	name string
	desc string
}

// Name 返回 Agent 名称（实现 adk.Agent 接口）
func (a *NamedAgent) Name(ctx context.Context) string {
	return a.name
}

// Description 返回 Agent 描述（实现 adk.Agent 接口）
func (a *NamedAgent) Description(ctx context.Context) string {
	return a.desc
}
