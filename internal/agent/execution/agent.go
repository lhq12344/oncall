package execution

import (
	"context"
	"fmt"
	"strings"

	"go_agent/internal/agent/execution/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config Execution Agent 配置
type Config struct {
	ChatModel *models.ChatModel
	Logger    *zap.Logger
}

// NewExecutionAgent 创建 Execution Agent（执行计划生成 + 安全执行）
func NewExecutionAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	// 创建工具集
	var toolsList []tool.BaseTool

	// 执行计划生成工具
	planTool := tools.NewGeneratePlanTool(cfg.ChatModel, cfg.Logger)
	toolsList = append(toolsList, planTool)

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
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "execution_agent",
		Description:   "生成和执行运维操作的执行代理",
		Model:         cfg.ChatModel.Client,
		GenModelInput: noFormatGenModelInput,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Instruction: `你是故障修复执行代理，负责按计划安全执行 bash 命令并回传结构化执行记录。

输入通常来自上游 ops_agent 与 pre_execution_validator，包含执行计划与风险信息。

你的职责：
1. 优先使用上游给出的计划（如果没有计划，再使用 generate_plan 兜底）。
2. 使用 execute_step 逐步执行命令。
3. 每一步后使用 validate_result 校验结果。
4. 失败时按需调用 rollback。

执行规则：
- 仅执行与故障修复相关且通过白名单的命令。
- 一次执行一个步骤，禁止批量拼接执行。
- 若工具返回无法执行（白名单拒绝/参数不安全/权限不足），立即停止并输出人工执行建议。

输出规范（最终必须输出一个 JSON 对象）：
{
  "execution_status": "success|failed|manual_required",
  "success": true,
  "executed_steps": [
    {"step": 1, "command": "xxx", "success": true, "error": ""}
  ],
  "failed_reason": "失败原因，无则为空字符串",
  "manual_plan": "工具无法自动修复时给人工执行的计划，无则为空字符串"
}

约束：
- success=true 时 execution_status 必须为 success。
- 工具无法完成时 execution_status 必须为 manual_required，并填写 manual_plan。
- 不要输出多段自然语言，保持 JSON 可解析。`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create execution agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("execution agent initialized with 4 tools")
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
