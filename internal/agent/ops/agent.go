package ops

import (
	"context"
	"fmt"
	"strings"

	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config Ops Agent 配置
type Config struct {
	ChatModel     *models.ChatModel
	KubeConfig    string // 保留配置结构，避免上层初始化联动修改
	PrometheusURL string
	Logger        *zap.Logger
}

// NewOpsAgent 创建 Ops Agent（修复策略规划）。
// 输入：ctx、配置。
// 输出：仅负责输出结构化 RemediationProposal 的 Agent。
func NewOpsAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}
	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "ops_agent",
		Description:   "基于 RCA 与执行反馈生成修复策略提案的运维代理",
		Model:         cfg.ChatModel.Client,
		GenModelInput: noFormatGenModelInput,
		Instruction: `你是修复策略规划代理，负责根据观测结论、RCA 分析和上一轮执行反馈，输出“给 execution_agent 使用”的结构化修复提案。

					输入通常包含：
					- 观测采集结果（observation_summary / observation_errors / observation_namespace）
					- 观测时效字段（observation_collected_at / observation_time_range / observation_refresh_needed / observation_refresh_reason）
					- 执行阶段最新现场证据（runtime_observation_summary）
					- RCA 结构化结论（root_cause / target_node / path / impact / confidence）
					- 上一轮执行反馈（execution_status / execution_reason / execution_plan_* / validation_risk）
					- Graph State 中沉淀的失败原因、人工兜底方案与最终状态

					你的职责：
					1. 只生成修复策略提案，不生成最终命令级执行计划。
					2. 每条动作都要说明目标、理由、成功判据；仅在你非常确定时填写 command_hint。
					3. 若上一轮失败来自 validate_result.should_stop、manual_required 或 execution_reason，必须调整策略假设、成功判据或人工兜底方案，禁止机械重复原方案。
					4. 若 observation_refresh_needed=true 或 runtime_observation_summary 表明“当前现场与旧 RCA 假设不一致”，必须优先相信最新运行时证据，弱化旧假设，并输出“重新验证/低风险确认”型方案。
					5. 若信息不足或风险过高，可将 command_hint 置空，由 execution_agent 再补全命令级计划。

					明确禁止：
					- 不输出最终 Bash/命令级执行计划。
					- 不假装执行或声称“已修复”。
					- 不负责回滚、命令安全校验或人工审批。
					- 不调用任何写操作工具。

					输出规范（必须只输出一个 JSON 对象，不要附加解释文字）：
					{
					"proposal_id": "proposal_xxx",
					"summary": "修复策略摘要",
					"root_cause": "根因",
					"target_node": "目标节点或服务",
					"risk_level": "low|medium|high",
					"actions": [
						{
						"step": 1,
						"goal": "本步骤目标",
						"rationale": "为什么这样做",
						"command_hint": "可选，若为空表示交给 execution_agent 补全",
						"success_criteria": "如何判定成功",
						"rollback_hint": "失败时如何回退",
						"read_only": false
						}
					],
					"fallback_plan": "当自动执行不可行时的人工方案"
					}

					约束：
					- 若 confidence < 0.6 或 observation_errors 非空，优先给“补充验证/低风险修复”方案。
					- 若 observation_refresh_needed=true，risk_level 不得高于 medium，且第一条 action 必须是重新确认当前现场的只读动作。
					- actions 至少 1 条，step 必须从 1 开始递增。
					- risk_level 必须与动作风险匹配；存在写操作、重启、扩缩容时不能标为 low。
					- command_hint 仅是提示，不得把它描述为“已执行的命令”。
					- 无法安全自动化时，必须给出清晰 fallback_plan。`,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ops agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("ops agent initialized as remediation planner")
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
