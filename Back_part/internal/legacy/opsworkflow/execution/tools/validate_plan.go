package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// PlanValidationResult 执行计划校验结果。
type PlanValidationResult struct {
	Valid                bool     `json:"valid"`
	Blocked              bool     `json:"blocked"`
	RequiresConfirmation bool     `json:"requires_confirmation"`
	RiskLevel            string   `json:"risk_level"`
	Reasons              []string `json:"reasons"`
	UnsafeCommands       []string `json:"unsafe_commands,omitempty"`
	ReviewCommands       []string `json:"review_commands,omitempty"`
	PlanID               string   `json:"plan_id,omitempty"`
}

// ValidatePlanTool 校验命令级执行计划风险。
type ValidatePlanTool struct {
	logger *zap.Logger
}

func NewValidatePlanTool(logger *zap.Logger) tool.BaseTool {
	return &ValidatePlanTool{logger: logger}
}

func (t *ValidatePlanTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "validate_plan",
		Desc: "对 ExecutionPlan 做命令风险校验，输出 blocked/risk_level。变更类命令会记录风险并在 execute_step 阶段逐步触发审批。优先传入 {\"plan\": <ExecutionPlan>}；若同轮已通过 normalize_plan/generate_plan 生成计划，也支持空参复用。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"plan": {
				Type:     schema.Object,
				Desc:     "命令级 ExecutionPlan",
				Required: true,
			},
		}),
	}, nil
}

func (t *ValidatePlanTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	fail := func(err error) (string, error) {
		if err == nil {
			return "", nil
		}
		decision := recordExecutionToolFailure(ctx, "validate_plan", err.Error())
		return "", fmt.Errorf("%s", decision.StopReason)
	}

	plan, err := parseValidatePlanInput(ctx, argumentsInJSON)
	if err != nil {
		return fail(err)
	}

	result := t.validate(plan)
	if result.Valid && !result.Blocked && !result.RequiresConfirmation {
		markExecutionPlanValidated(ctx, plan.PlanID)
	} else {
		clearExecutionPlanValidated(ctx)
	}

	output, err := json.Marshal(result)
	if err != nil {
		return fail(fmt.Errorf("failed to marshal result: %w", err))
	}
	clearRepeatedExecutionToolFailureState(ctx)

	if t.logger != nil {
		t.logger.Info("execution plan validated",
			zap.String("plan_id", result.PlanID),
			zap.Bool("blocked", result.Blocked),
			zap.Bool("requires_confirmation", result.RequiresConfirmation),
			zap.String("risk_level", result.RiskLevel))
	}
	return string(output), nil
}

// parseValidatePlanInput 解析 validate_plan 入参，兼容包装对象、原始计划对象与空参复用三种形态。
// 输入：ctx、argumentsInJSON。
// 输出：可校验的 ExecutionPlan。
func parseValidatePlanInput(ctx context.Context, argumentsInJSON string) (*ExecutionPlan, error) {
	payload := strings.TrimSpace(argumentsInJSON)
	if payload == "" || payload == "{}" || strings.EqualFold(payload, "null") {
		if plan, ok := getPreparedExecutionPlan(ctx); ok {
			return plan, nil
		}
		return nil, fmt.Errorf("invalid arguments: missing plan, call validate_plan with {\"plan\": <ExecutionPlan>} after normalize_plan/generate_plan")
	}

	type wrappedArgs struct {
		Plan json.RawMessage `json:"plan"`
	}

	var wrapped wrappedArgs
	if err := json.Unmarshal([]byte(payload), &wrapped); err == nil {
		if len(wrapped.Plan) > 0 && !isNullJSON(wrapped.Plan) {
			plan, decodeErr := decodeExecutionPlanJSON(wrapped.Plan)
			if decodeErr != nil {
				return nil, fmt.Errorf("invalid arguments: %w", decodeErr)
			}
			return plan, nil
		}
	} else if isTruncatedJSONError(err) {
		// LLM 偶发输出截断 JSON 时，优先复用同轮已缓存计划，避免流程被 ToolNode 直接打断。
		if plan, ok := getPreparedExecutionPlan(ctx); ok {
			return plan, nil
		}
	}

	plan, err := decodeExecutionPlanJSON([]byte(payload))
	if err == nil {
		return plan, nil
	}

	if plan, ok := getPreparedExecutionPlan(ctx); ok {
		return plan, nil
	}

	return nil, fmt.Errorf("invalid arguments: %w", err)
}

// decodeExecutionPlanJSON 解析 ExecutionPlan，兼容对象本体与字符串化 JSON。
// 输入：原始 JSON 字节。
// 输出：ExecutionPlan。
func decodeExecutionPlanJSON(raw []byte) (*ExecutionPlan, error) {
	payload := strings.TrimSpace(string(raw))
	if payload == "" {
		return nil, fmt.Errorf("empty execution plan payload")
	}

	if strings.HasPrefix(payload, "\"") {
		var embedded string
		if err := json.Unmarshal([]byte(payload), &embedded); err != nil {
			return nil, fmt.Errorf("decode embedded plan string failed: %w", err)
		}
		payload = strings.TrimSpace(embedded)
	}

	var plan ExecutionPlan
	if err := json.Unmarshal([]byte(payload), &plan); err != nil {
		return nil, err
	}
	if strings.TrimSpace(plan.PlanID) == "" && len(plan.Steps) == 0 {
		return nil, fmt.Errorf("execution plan is empty")
	}
	return &plan, nil
}

func isNullJSON(raw []byte) bool {
	return strings.EqualFold(strings.TrimSpace(string(raw)), "null")
}

// validate 执行命令级计划风险校验。
// 输入：ExecutionPlan。
// 输出：结构化风险判断结果。
func (t *ValidatePlanTool) validate(plan *ExecutionPlan) *PlanValidationResult {
	result := &PlanValidationResult{
		Valid:     true,
		RiskLevel: "low",
	}
	if plan == nil {
		result.Valid = false
		result.Blocked = true
		result.RiskLevel = "high"
		result.Reasons = append(result.Reasons, "缺少命令级执行计划")
		return result
	}

	result.PlanID = strings.TrimSpace(plan.PlanID)
	if len(plan.Steps) == 0 {
		result.Valid = false
		result.Blocked = true
		result.RiskLevel = "high"
		result.Reasons = append(result.Reasons, "执行计划 steps 为空")
		return result
	}

	var (
		riskScore int
		blocked   bool
	)

	for _, step := range plan.Steps {
		commandText := strings.TrimSpace(renderPlanCommand(step))
		if commandText == "" {
			blocked = true
			result.Reasons = append(result.Reasons, fmt.Sprintf("步骤 %d 缺少命令", step.StepID))
			continue
		}

		switch {
		case matchCommandPattern(commandText, absoluteForbiddenPatterns()...):
			blocked = true
			result.UnsafeCommands = append(result.UnsafeCommands, commandText)
			result.Reasons = append(result.Reasons, fmt.Sprintf("步骤 %d 命中禁止命令", step.StepID))
			riskScore += 4
		case matchCommandPattern(commandText, highRiskPatterns()...):
			result.ReviewCommands = append(result.ReviewCommands, commandText)
			result.Reasons = append(result.Reasons, fmt.Sprintf("步骤 %d 为变更类命令，执行到该步骤时会触发人工审批", step.StepID))
			riskScore += 2
		default:
			if !matchCommandPattern(commandText, readOnlyPatterns()...) {
				riskScore++
			}
		}
		if step.Critical && strings.TrimSpace(step.RollbackCommand) == "" {
			riskScore++
			result.Reasons = append(result.Reasons, fmt.Sprintf("步骤 %d 为关键步骤但未提供回滚", step.StepID))
		}
	}

	result.Blocked = blocked
	result.RequiresConfirmation = false
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
	return result
}

// renderPlanCommand 将 ExecutionStep 渲染为完整命令文本。
// 输入：ExecutionStep。
// 输出：命令文本。
func renderPlanCommand(step ExecutionStep) string {
	command := strings.TrimSpace(step.Command)
	args := strings.Join(step.Args, " ")
	if strings.EqualFold(command, "bash") && len(step.Args) > 0 {
		return strings.TrimSpace(args)
	}
	return strings.TrimSpace(strings.TrimSpace(command) + " " + strings.TrimSpace(args))
}

func matchCommandPattern(command string, patterns ...*regexp.Regexp) bool {
	for _, pattern := range patterns {
		if pattern != nil && pattern.MatchString(command) {
			return true
		}
	}
	return false
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
		regexp.MustCompile(`(?i)\bkubectl\s+(delete|drain|cordon|uncordon|scale|rollout\s+restart|patch)\b`),
		regexp.MustCompile(`(?i)\bdocker\s+(stop|restart|rm)\b`),
		regexp.MustCompile(`(?i)\bsystemctl\s+(stop|restart|disable)\b`),
		regexp.MustCompile(`(?i)\bhelm\s+(upgrade|rollback|uninstall)\b`),
	}
}

func readOnlyPatterns() []*regexp.Regexp {
	return []*regexp.Regexp{
		regexp.MustCompile(`(?i)\bkubectl\s+(get|describe|logs|top)\b`),
		regexp.MustCompile(`(?i)\bcurl\b`),
		regexp.MustCompile(`(?i)\bwget\b`),
		regexp.MustCompile(`(?i)\b(cat|ls|ps|ss|netstat|top|df|du|free|uptime|echo)\b`),
	}
}
