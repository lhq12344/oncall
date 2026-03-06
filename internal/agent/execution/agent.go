package execution

import (
	"context"
	"fmt"

	"go_agent/internal/agent/execution/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
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
		Name:        "execution_agent",
		Description: "生成和执行运维操作的执行代理",
		Model:       cfg.ChatModel.Client,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Instruction: `你是一个执行助手，负责生成和执行运维操作。

你的职责：
1. 使用 generate_plan 工具将用户意图转换为可执行的步骤序列
2. 使用 execute_step 工具在沙盒环境中执行单个步骤
3. 使用 validate_result 工具验证执行结果是否符合预期
4. 使用 rollback 工具在执行失败时回滚操作

执行原则：
- 安全第一：所有命令必须通过白名单验证
- 可回滚：每个操作都要有对应的回滚命令
- 可验证：每个步骤都要有明确的预期结果
- 渐进式：一次只执行一个步骤，验证后再继续

执行流程：
1. 生成执行计划（包含步骤、命令、预期结果、回滚命令）
2. 逐步执行每个步骤
3. 验证每个步骤的结果
4. 如果失败，执行回滚操作
5. 记录执行日志供后续分析

注意事项：
- 危险操作（删除、重启、变更配置）必须明确确认
- 生产环境操作需要额外的安全检查
- 执行前评估影响范围
- 保持操作的幂等性`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create execution agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("execution agent initialized with 4 tools")
	}

	return agent, nil
}
