package execution

import (
	"context"
	"fmt"
	"strings"

	"go_agent/internal/agent/execution/tools"
	"go_agent/internal/ai/models"
	"go_agent/internal/prompt"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config Execution Agent 配置结构。
//
// 字段说明：
// - ChatModel: 聊天模型，用于生成执行计划
// - Logger: 日志记录器
type Config struct {
	ChatModel *models.ChatModel
	Logger    *zap.Logger
}

const defaultExecutionAgentMaxIterations = 96

// NewExecutionAgent 创建 Execution Agent（执行计划生成 + 安全执行）。
//
// 功能：
// 1. 创建工具集（规范化、生成计划、校验计划、执行步骤、验证结果、回滚）
// 2. 创建 ChatModelAgent 并配置工具和指令
// 3. 返回执行代理实例
//
// 调用位置：
// - incident_workflow.go:61 行，创建故障处置工作流时调用
//
// 输入：
// - ctx: 上下文
// - cfg: Execution Agent 配置
//
// 输出：
// - adk.Agent: 执行代理实例
// - error: 创建过程中的错误
//
// 执行流程：
// 1. 规范化提案（normalize_plan）
// 2. 生成执行计划（generate_plan，如果 command_hint 不足）
// 3. 校验计划风险（validate_plan）
// 4. 逐步执行命令（execute_step）
// 5. 验证执行结果（validate_result）
// 6. 必要时回滚（rollback）
func NewExecutionAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	// 创建工具集
	var toolsList []tool.BaseTool

	// 提案规范化工具
	normalizeTool := tools.NewNormalizePlanTool(cfg.ChatModel, cfg.Logger)
	toolsList = append(toolsList, normalizeTool)

	// 执行计划生成工具
	planTool := tools.NewGeneratePlanTool(cfg.ChatModel, cfg.Logger)
	toolsList = append(toolsList, planTool)

	// 计划校验工具
	validatePlanTool := tools.NewValidatePlanTool(cfg.Logger)
	toolsList = append(toolsList, validatePlanTool)

	// 执行步骤工具
	executeTool := tools.NewExecuteStepTool(cfg.Logger)
	toolsList = append(toolsList, executeTool)

	// 验证结果工具
	validateTool := tools.NewValidateResultTool(cfg.Logger)
	toolsList = append(toolsList, validateTool)

	// 回滚工具
	rollbackTool := tools.NewRollbackTool(cfg.Logger)
	toolsList = append(toolsList, rollbackTool)

	// 创建 ChatModelAgent
	env := prompt.DetectEnvironment("")
	instruction := prompt.BuildAgentPrompt(prompt.RoleExecution, env, prompt.BuildOptions{})

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "execution_agent",
		Description:   "唯一负责命令级计划生成、风险校验、执行与回滚的执行代理",
		Model:         cfg.ChatModel.Client,
		GenModelInput: noFormatGenModelInput,
		MaxIterations: defaultExecutionAgentMaxIterations,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Instruction: instruction,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create execution agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("execution agent initialized with 6 tools")
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
