package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

type planValidatorAgent struct {
	name   string
	desc   string
	logger *zap.Logger
}

func newPlanValidatorAgent(logger *zap.Logger) adk.Agent {
	return &planValidatorAgent{
		name:   "pre_execution_validator",
		desc:   "执行前安全校验节点，检查命令风险并决定是否需要人工确认",
		logger: logger,
	}
}

func (a *planValidatorAgent) Name(_ context.Context) string {
	return a.name
}

func (a *planValidatorAgent) Description(_ context.Context) string {
	return a.desc
}

func (a *planValidatorAgent) Run(_ context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer generator.Close()

		result := a.validate(input)
		payload, err := json.Marshal(result)
		if err != nil {
			generator.Send(&adk.AgentEvent{Err: fmt.Errorf("marshal validation result failed: %w", err)})
			return
		}

		generator.Send(assistantEvent(string(payload)))
	}()

	return iterator
}

func (a *planValidatorAgent) validate(input *adk.AgentInput) *PlanValidationResult {
	result := &PlanValidationResult{
		Valid:     true,
		RiskLevel: "low",
	}
	if input == nil || len(input.Messages) == 0 {
		result.Valid = false
		result.Blocked = true
		result.RiskLevel = "high"
		result.Reasons = append(result.Reasons, "缺少执行计划输入")
		return result
	}

	plan, ok := parseOpsExecutionPlan(input.Messages)
	if !ok || plan == nil {
		result.Valid = true
		result.Blocked = false
		result.RiskLevel = "medium"
		result.Reasons = append(result.Reasons, "未检测到 ops_agent 的结构化计划，将由 execution_agent 自行生成计划")
		return result
	}

	result.PlanID = plan.PlanID

	if len(plan.Commands) == 0 {
		result.Valid = false
		result.Blocked = true
		result.RiskLevel = "high"
		result.Reasons = append(result.Reasons, "执行计划 commands 为空")
		return result
	}

	var (
		riskScore int
		blocked   bool
		confirm   bool
	)

	for _, step := range plan.Commands {
		command := strings.TrimSpace(step.Command)
		if command == "" {
			blocked = true
			result.Reasons = append(result.Reasons, fmt.Sprintf("步骤 %d 缺少 command", step.Step))
			continue
		}

		switch {
		case matchAny(command, absoluteForbiddenPatterns()...):
			blocked = true
			result.UnsafeCommands = append(result.UnsafeCommands, command)
			result.Reasons = append(result.Reasons, fmt.Sprintf("步骤 %d 命中禁止命令", step.Step))
			riskScore += 4
		case matchAny(command, highRiskPatterns()...):
			confirm = true
			result.ReviewCommands = append(result.ReviewCommands, command)
			result.Reasons = append(result.Reasons, fmt.Sprintf("步骤 %d 为高风险命令，需人工确认", step.Step))
			riskScore += 2
		default:
			if !matchAny(command, readOnlyPatterns()...) {
				riskScore++
			}
		}
	}

	result.Blocked = blocked
	result.RequiresConfirmation = !blocked && confirm
	result.Valid = !blocked
	switch {
	case blocked || riskScore >= 4:
		result.RiskLevel = "high"
	case riskScore >= 2:
		result.RiskLevel = "medium"
	default:
		result.RiskLevel = "low"
	}

	if len(result.Reasons) == 0 {
		result.Reasons = append(result.Reasons, "计划通过自动校验")
	}

	if a.logger != nil {
		a.logger.Info("plan validation completed",
			zap.String("plan_id", result.PlanID),
			zap.Bool("blocked", result.Blocked),
			zap.Bool("requires_confirmation", result.RequiresConfirmation),
			zap.String("risk_level", result.RiskLevel))
	}

	return result
}

type executionGateAgent struct {
	name   string
	desc   string
	logger *zap.Logger
}

func newExecutionGateAgent(logger *zap.Logger) adk.ResumableAgent {
	return &executionGateAgent{
		name:   "execution_gate",
		desc:   "根据执行结果决定中断、重规划或结束循环",
		logger: logger,
	}
}

func (a *executionGateAgent) Name(_ context.Context) string {
	return a.name
}

func (a *executionGateAgent) Description(_ context.Context) string {
	return a.desc
}

func (a *executionGateAgent) Run(ctx context.Context, input *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer generator.Close()

		var messages []adk.Message
		if input != nil {
			messages = input.Messages
		}

		plan, _ := parseOpsExecutionPlan(messages)
		validation, hasValidation := parseValidationResult(messages)
		execStatus := detectExecutionStatus(messages)

		if hasValidation && validation.Blocked {
			reason := formatReasons(validation.Reasons)
			fallback := ""
			planID := ""
			if plan != nil {
				fallback = plan.FallbackPlan
				planID = plan.PlanID
			}
			message := fmt.Sprintf("执行前校验阻断：%s。请先人工修正计划后再继续。", reason)
			generator.Send(interruptEvent(ctx, &IncidentInterruptInfo{
				Type:         "validator_blocked",
				Reason:       reason,
				PlanID:       planID,
				FallbackPlan: fallback,
			}, message))
			return
		}

		if hasValidation && validation.RequiresConfirmation {
			reason := formatReasons(validation.Reasons)
			fallback := ""
			planID := ""
			if plan != nil {
				fallback = plan.FallbackPlan
				planID = plan.PlanID
			}
			message := fmt.Sprintf("计划包含高风险命令，需要你确认后再执行。风险说明：%s。", reason)
			generator.Send(interruptEvent(ctx, &IncidentInterruptInfo{
				Type:         "approval_required",
				Reason:       reason,
				PlanID:       planID,
				FallbackPlan: fallback,
			}, message))
			return
		}

		if execStatus.Found && execStatus.Success {
			generator.Send(breakLoopEvent(a.name, "执行步骤已成功，停止重规划循环，进入策略复盘。"))
			return
		}

		if execStatus.ToolCannotFix {
			fallback := ""
			planID := ""
			if plan != nil {
				fallback = plan.FallbackPlan
				planID = plan.PlanID
			}
			message := fmt.Sprintf("自动工具未能完成修复，请按计划人工执行后回复确认。建议人工方案：%s", fallback)
			generator.Send(interruptEvent(ctx, &IncidentInterruptInfo{
				Type:         "manual_required",
				Reason:       strings.TrimSpace(execStatus.RawMessageHint),
				PlanID:       planID,
				FallbackPlan: fallback,
			}, message))
			return
		}

		generator.Send(assistantEvent("执行未达成预期，回到 ops_agent 重新生成计划。"))
	}()

	return iterator
}

func (a *executionGateAgent) Resume(ctx context.Context, info *adk.ResumeInfo, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()

	go func() {
		defer generator.Close()

		if info == nil || !info.WasInterrupted {
			generator.Send(assistantEvent("未检测到可恢复的中断状态，继续重规划。"))
			return
		}

		if !info.IsResumeTarget {
			generator.Send(interruptEvent(ctx, &IncidentInterruptInfo{
				Type:   "awaiting_user_confirmation",
				Reason: "仍在等待用户确认",
			}, "当前仍等待用户确认，请回复“确认已修复”或“继续重试”。"))
			return
		}

		approved, resolved, comment := parseResumeDecision(info.ResumeData)
		if approved || resolved {
			if strings.TrimSpace(comment) == "" {
				comment = "用户已确认"
			}
			generator.Send(breakLoopEvent(a.name, fmt.Sprintf("收到确认：%s。结束执行循环，进入 strategy_agent 生成最终报告。", comment)))
			return
		}

		if strings.TrimSpace(comment) == "" {
			comment = "用户要求继续重试"
		}
		generator.Send(assistantEvent(fmt.Sprintf("收到反馈：%s。将重新进入 ops_agent 生成新计划。", comment)))
	}()

	return iterator
}

func absoluteForbiddenPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)\brm\s+-rf\s+/`),
		regexp.MustCompile(`(?i)\bmkfs\b`),
		regexp.MustCompile(`(?i)\bdd\s+if=`),
		regexp.MustCompile(`(?i)\bshutdown\b|\breboot\b`),
		regexp.MustCompile(`:\(\)\s*\{\s*:\|:&\s*\};:`),
	}
}

func highRiskPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bsystemctl\s+(restart|stop)\b`),
		regexp.MustCompile(`(?i)\bdocker\s+(stop|rm|restart)\b`),
		regexp.MustCompile(`(?i)\bkubectl\s+(delete|scale|rollout\s+restart)\b`),
		regexp.MustCompile(`(?i)\bkill\s+-?\d*\b`),
		regexp.MustCompile(`(?i)\biptables\b`),
	}
}

func readOnlyPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)^\s*kubectl\s+(get|describe|logs)\b`),
		regexp.MustCompile(`(?i)^\s*(ls|df|du|cat|tail|grep|ps|top|free|uptime)\b`),
		regexp.MustCompile(`(?i)^\s*curl\s+-I\b`),
	}
}

func matchAny(command string, rules ...*regexp.Regexp) bool {
	for _, rule := range rules {
		if rule.MatchString(command) {
			return true
		}
	}
	return false
}
