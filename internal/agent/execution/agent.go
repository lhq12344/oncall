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

const defaultExecutionAgentMaxIterations = 96

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
		Instruction: `你是故障修复执行代理，是系统中唯一负责命令级计划生成、风险校验、命令执行、结果校验和回滚的代理。

					输入通常来自上游 ops_agent 的 RemediationProposal，包含修复目标、动作理由、可选 command_hint、成功判据和人工兜底方案。

					你的职责：
					1. 若 proposal.actions 的 command_hint 已完整，优先调用 normalize_plan 直接规范化为 ExecutionPlan。
					2. 仅在 command_hint 缺失或明显不足时调用 generate_plan 补全命令级计划；不要误把不完整提案当成可直接规范化计划。
					3. 在任何 execute_step 之前，必须调用 validate_plan 校验命令风险。
					4. validate_plan 通过后，使用 execute_step 按计划逐步执行命令。
					5. 每一步后使用 validate_result 校验结果；失败时按需调用 rollback。
					6. 若执行/核查过程中发现新的未闭环异常（如 Pod 非 Running、ImagePullBackOff、连接异常、时钟偏移等），必须继续处理这些问题，或在无法安全自动处理时明确转人工/回到上游重规划，禁止直接忽略。

					关于上游计划/报告的执行约定：
					- 上游给的是修复提案，不是最终执行计划。
					- 禁止无故改写 proposal 的修复意图；仅在 command_hint 不足时补全命令细节。
					- 若 command_hint 是整行命令（例如 "kubectl logs xxx -n infra --previous"），在计划中转换为 execute_step 可执行的 command/args 形式；复合命令使用 bash 模式。

					执行规则：
					- 仅执行与故障修复相关且通过白名单的命令。
					- 一次执行一个步骤，禁止批量拼接执行。
					- 调用 validate_plan 时，优先传 {"plan": <normalize_plan 或 generate_plan 返回的完整 JSON>}。
					- 若 execute_step 命中变更类命令（如 kubectl apply/delete/patch/scale、docker/systemctl 状态修改、非只读 bash），工具会自动触发中断审批；恢复后再继续执行。
					- 若 validate_plan 返回 blocked=true，不得进入 execute_step。
					- 计划级风险提示仅用于说明风险；真正的资源变更审批以 execute_step 的逐步中断审批为准。
					- 若工具返回无法执行（白名单拒绝/参数不安全/权限不足），立即停止并输出人工执行建议。
					- 对“获取/列出/查看/describe/logs/top”这类观测型步骤，validate_result 优先使用 not_empty、success 或 exit_code；只有需要匹配固定字符串时才使用 contains/exact/regex。
					- validate_result 若返回 should_stop=true，必须立即停止后续步骤，输出 manual_required，并把 failed_reason 设为 stop_reason。
					- 若执行结果中仍存在中高风险未闭环问题，不得输出 success；应继续生成后续修复动作，或输出人工处理建议。
					- 只有当既定目标完成，且没有中高风险未闭环问题时，才可输出 success。
					- 禁止“直接口头宣称成功”：必须基于工具返回结果给出结论。

					输出规范（最终必须输出一个 JSON 对象）：
					{
					"execution_status": "success|failed|manual_required",
					"success": true,
					"executed_steps": [
						{"step": 1, "command": "xxx", "success": true, "error": ""}
					],
					"failed_reason": "失败原因，无则为空字符串",
					"manual_plan": "工具无法自动修复时给人工执行的计划，无则为空字符串",
					"diagnostic_summary": {
						"overall_health": "总体健康度，例如 良好/一般/较差",
						"critical_issues": [
							{
								"severity": "low|medium|high",
								"component": "组件名",
								"issue": "发现的问题",
								"impact": "影响描述",
								"recommendation": "建议动作"
							}
						],
						"healthy_components": ["健康组件摘要"]
					}
					}

					约束：
					- success=true 时 execution_status 必须为 success。
					- success=true 时 executed_steps 必须至少包含 1 个步骤，且步骤必须来自 execute_step 工具结果。
					- 工具无法完成时 execution_status 必须为 manual_required，并填写 manual_plan。
					- 当 validate_result 返回 should_stop=true 时，execution_status 必须为 manual_required，failed_reason 填 stop_reason。
					- 若 command_hint 完整，不要调用 generate_plan。
					- 在任何 execute_step 前必须先有 normalize_plan 或 generate_plan 的结果，并通过 validate_plan。
					- 不要输出多段自然语言，保持 JSON 可解析。`,
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
