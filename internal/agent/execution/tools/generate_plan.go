package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// GeneratePlanTool 执行计划生成工具
type GeneratePlanTool struct {
	chatModel *models.ChatModel
	logger    *zap.Logger
}

// ProposalActionInput execution_agent 消费的修复提案动作。
type ProposalActionInput struct {
	Step            int    `json:"step"`
	Goal            string `json:"goal"`
	Rationale       string `json:"rationale"`
	CommandHint     string `json:"command_hint"`
	SuccessCriteria string `json:"success_criteria"`
	RollbackHint    string `json:"rollback_hint"`
	ReadOnly        bool   `json:"read_only"`
}

// RemediationProposalInput execution_agent 消费的结构化修复提案。
type RemediationProposalInput struct {
	ProposalID   string                `json:"proposal_id"`
	Summary      string                `json:"summary"`
	RootCause    string                `json:"root_cause"`
	TargetNode   string                `json:"target_node"`
	RiskLevel    string                `json:"risk_level"`
	Actions      []ProposalActionInput `json:"actions"`
	FallbackPlan string                `json:"fallback_plan"`
}

// ExecutionStep 执行步骤
type ExecutionStep struct {
	StepID          int      `json:"step_id"`
	Description     string   `json:"description"`
	Command         string   `json:"command"`
	Args            []string `json:"args"`
	ExpectedResult  string   `json:"expected_result"`
	RollbackCommand string   `json:"rollback_command"`
	RollbackArgs    []string `json:"rollback_args"`
	Timeout         int      `json:"timeout"`  // 秒
	Critical        bool     `json:"critical"` // 是否关键步骤
}

// ExecutionPlan 执行计划
type ExecutionPlan struct {
	PlanID        string          `json:"plan_id"`
	Description   string          `json:"description"`
	Steps         []ExecutionStep `json:"steps"`
	TotalSteps    int             `json:"total_steps"`
	EstimatedTime int             `json:"estimated_time"` // 秒
	RiskLevel     string          `json:"risk_level"`     // low/medium/high
}

func NewGeneratePlanTool(chatModel *models.ChatModel, logger *zap.Logger) tool.BaseTool {
	return &GeneratePlanTool{
		chatModel: chatModel,
		logger:    logger,
	}
}

func (t *GeneratePlanTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "generate_plan",
		Desc: "根据修复提案或用户意图生成命令级执行计划。仅在上游 command_hint 不足时使用。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"proposal": {
				Type:     schema.Object,
				Desc:     "上游 RemediationProposal 结构化提案",
				Required: false,
			},
			"intent": {
				Type:     schema.String,
				Desc:     "用户意图或修复目标摘要",
				Required: false,
			},
			"context": {
				Type:     schema.String,
				Desc:     "上下文信息（命名空间、资源名、风险、失败原因等）",
				Required: false,
			},
		}),
	}, nil
}

func (t *GeneratePlanTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Proposal map[string]any `json:"proposal"`
		Intent   string         `json:"intent"`
		Context  string         `json:"context"`
	}

	var in args
	if err := unmarshalArgsLenient(argumentsInJSON, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	proposal, err := parseProposalInput(in.Proposal)
	if err != nil {
		return "", err
	}
	intent, contextText := buildPlanIntentAndContext(proposal, in.Intent, in.Context)
	if strings.TrimSpace(intent) == "" && proposal == nil {
		return "", fmt.Errorf("intent or proposal is required")
	}

	plan, err := t.generatePlanWithLLM(ctx, proposal, intent, contextText)
	if err != nil {
		if t.logger != nil {
			t.logger.Warn("LLM plan generation failed, using template", zap.Error(err))
		}
		plan = t.generatePlanWithTemplate(proposal, intent, contextText)
	}

	if err := validateExecutionPlanStructure(plan); err != nil {
		return "", fmt.Errorf("plan validation failed: %w", err)
	}
	plan.RiskLevel = assessExecutionPlanRisk(plan)
	markExecutionPlanPrepared(ctx, plan.PlanID)
	if err := rememberExecutionPlan(ctx, plan); err != nil {
		return "", err
	}

	output, err := json.Marshal(plan)
	if err != nil {
		return "", fmt.Errorf("failed to marshal plan: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("execution plan generated",
			zap.String("plan_id", plan.PlanID),
			zap.Int("steps", plan.TotalSteps),
			zap.String("risk_level", plan.RiskLevel))
	}
	return string(output), nil
}

// generatePlanWithLLM 使用 LLM 生成执行计划。
// 输入：ctx、修复提案、意图摘要、上下文摘要。
// 输出：命令级执行计划。
func (t *GeneratePlanTool) generatePlanWithLLM(ctx context.Context, proposal *RemediationProposalInput, intent, contextText string) (*ExecutionPlan, error) {
	if t.chatModel == nil {
		return nil, fmt.Errorf("chat model not available")
	}

	proposalJSON := "{}"
	if proposal != nil {
		body, _ := json.Marshal(proposal)
		proposalJSON = string(body)
	}

	prompt := fmt.Sprintf(`你是命令级执行计划生成器，请把修复提案补全为可执行的 ExecutionPlan。

修复提案（上游输出）：
%s

意图摘要：
%s

上下文：
%s

要求：
1. 优先保留 proposal.actions 中已有的 command_hint，不得无故改写其意图。
2. 仅在 command_hint 缺失或明显不足时补全命令、参数、预期结果和回滚信息。
3. 返回的步骤必须是单步命令，禁止批量拼接执行。
4. 若命令需要管道、重定向或复合语法，请使用 command="bash"，args=["完整脚本"] 的形式。
5. 输出必须是一个 JSON 对象，字段如下：
{
  "plan_id": "plan_xxx",
  "description": "执行计划摘要",
  "steps": [
    {
      "step_id": 1,
      "description": "步骤描述",
      "command": "命令",
      "args": ["参数1", "参数2"],
      "expected_result": "预期结果",
      "rollback_command": "回滚命令",
      "rollback_args": ["回滚参数"],
      "timeout": 30,
      "critical": true
    }
  ],
  "total_steps": 2,
  "estimated_time": 60,
  "risk_level": "low|medium|high"
}`, proposalJSON, intent, contextText)

	resp, err := t.chatModel.Client.Generate(ctx, []*schema.Message{schema.UserMessage(prompt)})
	if err != nil {
		return nil, err
	}
	content := strings.TrimSpace(resp.Content)
	if content == "" {
		return nil, fmt.Errorf("empty response from LLM")
	}

	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start < 0 || end <= start {
		return nil, fmt.Errorf("failed to parse LLM response")
	}

	var plan ExecutionPlan
	if err := json.Unmarshal([]byte(content[start:end+1]), &plan); err != nil {
		return nil, fmt.Errorf("failed to parse LLM response: %w", err)
	}
	return &plan, nil
}

// generatePlanWithTemplate 使用模板生成执行计划（降级方案）。
// 输入：修复提案、意图摘要、上下文。
// 输出：命令级执行计划。
func (t *GeneratePlanTool) generatePlanWithTemplate(proposal *RemediationProposalInput, intent, contextText string) *ExecutionPlan {
	if proposal != nil && len(proposal.Actions) > 0 {
		steps := make([]ExecutionStep, 0, len(proposal.Actions))
		for index, action := range proposal.Actions {
			step, err := buildExecutionStepFromAction(action, index+1, true)
			if err != nil {
				continue
			}
			steps = append(steps, step)
		}
		if len(steps) > 0 {
			return finalizeExecutionPlan(
				firstNonEmptyExecText("plan_"+strings.TrimSpace(proposal.ProposalID), fmt.Sprintf("plan_template_%d", len(proposal.Actions))),
				firstNonEmptyExecText(strings.TrimSpace(proposal.Summary), "基于修复提案生成的执行计划"),
				steps,
			)
		}
	}

	lower := strings.ToLower(intent)
	var steps []ExecutionStep
	switch {
	case strings.Contains(lower, "重启") || strings.Contains(lower, "restart"):
		steps = []ExecutionStep{
			{
				StepID:          1,
				Description:     "执行重启操作",
				Command:         "bash",
				Args:            []string{firstNonEmptyExecText(contextText, "echo restart target not specified")},
				ExpectedResult:  "命令执行成功",
				RollbackCommand: "",
				Timeout:         30,
				Critical:        true,
			},
		}
	case strings.Contains(lower, "日志") || strings.Contains(lower, "log"):
		steps = []ExecutionStep{
			{
				StepID:          1,
				Description:     "查看目标日志",
				Command:         "bash",
				Args:            []string{firstNonEmptyExecText(contextText, "echo log target not specified")},
				ExpectedResult:  "输出目标日志",
				RollbackCommand: "",
				Timeout:         20,
				Critical:        false,
			},
		}
	default:
		steps = []ExecutionStep{
			{
				StepID:          1,
				Description:     "执行修复动作",
				Command:         "echo",
				Args:            []string{firstNonEmptyExecText(intent, "execute remediation")},
				ExpectedResult:  "动作完成",
				RollbackCommand: "",
				Timeout:         10,
				Critical:        false,
			},
		}
	}

	return finalizeExecutionPlan(
		fmt.Sprintf("plan_template_%d", len(firstNonEmptyExecText(intent, contextText))),
		firstNonEmptyExecText("执行："+intent, "基于上下文生成的执行计划"),
		steps,
	)
}

// parseProposalInput 解析 execution_agent 的结构化修复提案输入。
// 输入：原始 proposal 对象。
// 输出：解析后的提案；当未提供 proposal 时返回 nil。
func parseProposalInput(raw map[string]any) (*RemediationProposalInput, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	body, err := json.Marshal(raw)
	if err != nil {
		return nil, fmt.Errorf("invalid proposal: %w", err)
	}
	var proposal RemediationProposalInput
	if err := json.Unmarshal(body, &proposal); err != nil {
		return nil, fmt.Errorf("invalid proposal: %w", err)
	}
	return &proposal, nil
}

// buildPlanIntentAndContext 生成命令计划的意图和上下文摘要。
// 输入：修复提案、额外 intent、额外 context。
// 输出：意图摘要与上下文摘要。
func buildPlanIntentAndContext(proposal *RemediationProposalInput, intent, contextText string) (string, string) {
	intent = strings.TrimSpace(intent)
	contextText = strings.TrimSpace(contextText)
	if proposal == nil {
		return intent, contextText
	}

	if intent == "" {
		actionGoals := make([]string, 0, len(proposal.Actions))
		for _, action := range proposal.Actions {
			if goal := strings.TrimSpace(action.Goal); goal != "" {
				actionGoals = append(actionGoals, goal)
			}
		}
		intent = firstNonEmptyExecText(
			strings.TrimSpace(proposal.Summary),
			strings.Join(actionGoals, "；"),
			fmt.Sprintf("根因=%s 目标=%s", strings.TrimSpace(proposal.RootCause), strings.TrimSpace(proposal.TargetNode)),
		)
	}

	if contextText == "" {
		lines := []string{}
		if value := strings.TrimSpace(proposal.RootCause); value != "" {
			lines = append(lines, "root_cause="+value)
		}
		if value := strings.TrimSpace(proposal.TargetNode); value != "" {
			lines = append(lines, "target_node="+value)
		}
		if value := strings.TrimSpace(proposal.RiskLevel); value != "" {
			lines = append(lines, "risk_level="+value)
		}
		if value := strings.TrimSpace(proposal.FallbackPlan); value != "" {
			lines = append(lines, "fallback_plan="+value)
		}
		contextText = strings.Join(lines, "\n")
	}
	return intent, contextText
}

// proposalHasCompleteCommandHints 判断修复提案是否已提供完整 command_hint。
// 输入：修复提案。
// 输出：是否全部动作都包含 command_hint。
func proposalHasCompleteCommandHints(proposal *RemediationProposalInput) bool {
	if proposal == nil || len(proposal.Actions) == 0 {
		return false
	}
	for _, action := range proposal.Actions {
		if strings.TrimSpace(action.CommandHint) == "" {
			return false
		}
	}
	return true
}

// normalizeProposalToPlan 将完整 command_hint 的修复提案直接规范化为 ExecutionPlan。
// 输入：修复提案。
// 输出：ExecutionPlan；若 command_hint 不完整则返回错误。
func normalizeProposalToPlan(proposal *RemediationProposalInput) (*ExecutionPlan, error) {
	if proposal == nil {
		return nil, fmt.Errorf("proposal is required")
	}
	if !proposalHasCompleteCommandHints(proposal) {
		return nil, fmt.Errorf("proposal has incomplete command_hint, call generate_plan instead")
	}
	steps := make([]ExecutionStep, 0, len(proposal.Actions))
	for index, action := range proposal.Actions {
		step, err := buildExecutionStepFromAction(action, index+1, false)
		if err != nil {
			return nil, err
		}
		steps = append(steps, step)
	}
	return finalizeExecutionPlan(
		firstNonEmptyExecText("plan_"+strings.TrimSpace(proposal.ProposalID), fmt.Sprintf("normalized_%d", len(steps))),
		firstNonEmptyExecText(strings.TrimSpace(proposal.Summary), "基于修复提案规范化的执行计划"),
		steps,
	), nil
}

// buildExecutionStepFromAction 将修复提案动作转换为命令执行步骤。
// 输入：动作、默认步骤序号、是否允许空 command_hint。
// 输出：ExecutionStep。
func buildExecutionStepFromAction(action ProposalActionInput, defaultStepID int, allowEmptyHint bool) (ExecutionStep, error) {
	stepID := action.Step
	if stepID <= 0 {
		stepID = defaultStepID
	}
	commandHint := strings.TrimSpace(action.CommandHint)
	if commandHint == "" && !allowEmptyHint {
		return ExecutionStep{}, fmt.Errorf("action step %d missing command_hint", stepID)
	}
	if commandHint == "" {
		commandHint = "echo " + firstNonEmptyExecText(strings.TrimSpace(action.Goal), "manual intervention required")
	}

	command, args := normalizeCommandHint(commandHint)
	rollbackCommand, rollbackArgs := normalizeCommandHint(strings.TrimSpace(action.RollbackHint))
	timeout := 30
	if action.ReadOnly {
		timeout = 15
	}

	return ExecutionStep{
		StepID:          stepID,
		Description:     firstNonEmptyExecText(strings.TrimSpace(action.Goal), fmt.Sprintf("执行步骤 %d", stepID)),
		Command:         command,
		Args:            args,
		ExpectedResult:  firstNonEmptyExecText(strings.TrimSpace(action.SuccessCriteria), "命令执行成功"),
		RollbackCommand: rollbackCommand,
		RollbackArgs:    rollbackArgs,
		Timeout:         timeout,
		Critical:        !action.ReadOnly,
	}, nil
}

// normalizeCommandHint 将 command_hint 规范化为 execute_step 所需 command/args。
// 输入：命令提示文本。
// 输出：command、args。
func normalizeCommandHint(hint string) (string, []string) {
	hint = strings.TrimSpace(hint)
	if hint == "" {
		return "", nil
	}
	if isComplexShellCommand(hint) {
		return "bash", []string{hint}
	}
	parts := strings.Fields(hint)
	if len(parts) == 0 {
		return "", nil
	}
	if len(parts) == 1 {
		return parts[0], nil
	}
	return parts[0], parts[1:]
}

// isComplexShellCommand 判断是否应走 bash 模式。
// 输入：命令提示文本。
// 输出：是否为复合 shell 命令。
func isComplexShellCommand(hint string) bool {
	patterns := []string{"|", "&&", "||", ";", ">", "<", "$(", "`", "\n"}
	for _, pattern := range patterns {
		if strings.Contains(hint, pattern) {
			return true
		}
	}
	return false
}

// finalizeExecutionPlan 补全计划统计字段。
// 输入：planID、描述、步骤列表。
// 输出：完整 ExecutionPlan。
func finalizeExecutionPlan(planID, description string, steps []ExecutionStep) *ExecutionPlan {
	totalTime := 0
	for _, step := range steps {
		totalTime += step.Timeout
	}
	plan := &ExecutionPlan{
		PlanID:        strings.TrimSpace(planID),
		Description:   strings.TrimSpace(description),
		Steps:         steps,
		TotalSteps:    len(steps),
		EstimatedTime: totalTime,
	}
	if plan.PlanID == "" {
		plan.PlanID = fmt.Sprintf("plan_%d", len(steps))
	}
	return plan
}

// validateExecutionPlanStructure 验证计划结构完整性。
// 输入：ExecutionPlan。
// 输出：错误；无错误表示结构有效。
func validateExecutionPlanStructure(plan *ExecutionPlan) error {
	if plan == nil {
		return fmt.Errorf("plan is nil")
	}
	if len(plan.Steps) == 0 {
		return fmt.Errorf("plan steps are empty")
	}
	for _, step := range plan.Steps {
		if strings.TrimSpace(step.Command) == "" {
			return fmt.Errorf("step %d missing command", step.StepID)
		}
		if strings.TrimSpace(step.Description) == "" {
			return fmt.Errorf("step %d missing description", step.StepID)
		}
	}
	return nil
}

// assessExecutionPlanRisk 评估执行计划风险。
// 输入：ExecutionPlan。
// 输出：风险等级。
func assessExecutionPlanRisk(plan *ExecutionPlan) string {
	riskScore := 0
	for _, step := range plan.Steps {
		if step.Critical {
			riskScore += 2
		}
		if step.RollbackCommand == "" && step.Critical {
			riskScore++
		}
		commandText := strings.ToLower(strings.TrimSpace(step.Command + " " + strings.Join(step.Args, " ")))
		if strings.Contains(commandText, "delete") || strings.Contains(commandText, "rm") {
			riskScore += 2
		}
		if strings.Contains(commandText, "restart") || strings.Contains(commandText, "rollout") ||
			strings.Contains(commandText, "scale") || strings.Contains(commandText, "patch") {
			riskScore++
		}
	}
	if plan.TotalSteps > 5 {
		riskScore++
	}
	switch {
	case riskScore >= 5:
		return "high"
	case riskScore >= 3:
		return "medium"
	default:
		return "low"
	}
}

// firstNonEmptyExecText 返回第一个非空文本。
// 输入：候选文本列表。
// 输出：首个非空文本；若都为空则返回空字符串。
func firstNonEmptyExecText(values ...string) string {
	for _, value := range values {
		if text := strings.TrimSpace(value); text != "" {
			return text
		}
	}
	return ""
}
