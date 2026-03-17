package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"regexp"
	"strconv"
	"strings"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// ValidateResultTool 验证结果工具
type ValidateResultTool struct {
	logger *zap.Logger
}

// ValidationResult 验证结果
type ValidationResult struct {
	StepID           int    `json:"step_id"`
	Valid            bool   `json:"valid"`
	Message          string `json:"message"`
	Expected         string `json:"expected"`
	Actual           string `json:"actual"`
	Method           string `json:"method"` // exact/contains/regex/exit_code/not_empty/success
	FailureCategory  string `json:"failure_category,omitempty"`
	MismatchDetected bool   `json:"mismatch_detected,omitempty"`
	MismatchReason   string `json:"mismatch_reason,omitempty"`
	ShouldStop       bool   `json:"should_stop,omitempty"`
	StopReason       string `json:"stop_reason,omitempty"`
	StopAction       string `json:"stop_action,omitempty"`
	RuntimeSummary   string `json:"runtime_summary,omitempty"`
}

type validateResultArgs struct {
	StepID           int    `json:"step_id"`
	ExpectedResult   string `json:"expected_result"`
	ActualOutput     string `json:"actual_output"`
	ExitCode         int    `json:"exit_code"`
	ValidationMethod string `json:"validation_method"`
}

func NewValidateResultTool(logger *zap.Logger) tool.BaseTool {
	return &ValidateResultTool{
		logger: logger,
	}
}

func (t *ValidateResultTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "validate_result",
		Desc: "验证执行结果是否符合预期。支持 exact/contains/regex/exit_code/not_empty/success，并区分计划失配与真实执行故障。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"step_id": {
				Type:     schema.Integer,
				Desc:     "步骤 ID",
				Required: true,
			},
			"expected_result": {
				Type:     schema.String,
				Desc:     "预期结果",
				Required: true,
			},
			"actual_output": {
				Type:     schema.String,
				Desc:     "实际输出",
				Required: true,
			},
			"exit_code": {
				Type:     schema.Integer,
				Desc:     "退出码",
				Required: false,
			},
			"validation_method": {
				Type:     schema.String,
				Desc:     "验证方法：exact/contains/regex/exit_code/not_empty/success，默认 contains",
				Required: false,
			},
		}),
	}, nil
}

func (t *ValidateResultTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	fail := func(err error) (string, error) {
		if err == nil {
			return "", nil
		}
		decision := recordExecutionToolFailure(ctx, "validate_result", err.Error())
		return "", fmt.Errorf("%s", decision.StopReason)
	}

	in, err := parseValidateResultInput(ctx, argumentsInJSON)
	if err != nil {
		return fail(err)
	}

	if in.ExpectedResult == "" {
		return fail(fmt.Errorf("expected_result is required"))
	}

	// 默认验证方法
	if in.ValidationMethod == "" {
		in.ValidationMethod = "contains"
	}

	// 执行验证
	result := t.validate(in.StepID, in.ExpectedResult, in.ActualOutput, in.ExitCode, in.ValidationMethod)
	repeatedFailure := repeatedExecutionFailureDecision{}
	switch {
	case result.Valid, result.MismatchDetected:
		clearRepeatedExecutionToolFailureState(ctx)
	default:
		repeatedFailure = recordExecutionToolFailure(ctx, "validate_result", firstNonEmptyExecutionText(result.Message, result.FailureCategory, result.Expected))
	}
	decision := recordValidationProgress(ctx, in.StepID, result)
	result.ShouldStop = decision.ShouldStop
	result.StopReason = decision.StopReason
	result.StopAction = decision.StopAction
	result.RuntimeSummary = decision.RuntimeSummary
	if repeatedFailure.Reached {
		result.ShouldStop = true
		result.StopReason = repeatedFailure.StopReason
		result.StopAction = "manual_required"
	}

	output, err := json.Marshal(result)
	if err != nil {
		return fail(fmt.Errorf("failed to marshal result: %w", err))
	}

	if t.logger != nil {
		t.logger.Info("validation completed",
			zap.Int("step_id", in.StepID),
			zap.Bool("valid", result.Valid),
			zap.String("method", result.Method),
			zap.Bool("mismatch_detected", result.MismatchDetected),
			zap.String("failure_category", result.FailureCategory),
			zap.Bool("should_stop", result.ShouldStop),
			zap.String("stop_action", result.StopAction))
	}

	return string(output), nil
}

// parseValidateResultInput 解析 validate_result 参数，兼容空参和基于最近执行结果自动补全。
// 输入：ctx、argumentsInJSON。
// 输出：可用于验证的参数。
func parseValidateResultInput(ctx context.Context, argumentsInJSON string) (validateResultArgs, error) {
	payload := strings.TrimSpace(argumentsInJSON)

	var in validateResultArgs
	if payload != "" && payload != "{}" && !strings.EqualFold(payload, "null") {
		if err := unmarshalArgsLenient(payload, &in); err != nil {
			if isTruncatedJSONError(err) {
				in = validateResultArgs{}
			} else {
				return validateResultArgs{}, fmt.Errorf("invalid arguments: %w", err)
			}
		}
	}

	lastResult, hasLastResult := getLastExecutionResult(ctx)
	if in.StepID <= 0 && hasLastResult && lastResult != nil {
		in.StepID = lastResult.StepID
	}
	if in.ActualOutput == "" && hasLastResult && lastResult != nil {
		in.ActualOutput = lastResult.Output
		in.ExitCode = lastResult.ExitCode
	}
	if in.ExpectedResult == "" && in.StepID > 0 {
		if step, ok := getExecutionPlanStepByID(ctx, in.StepID); ok && step != nil {
			in.ExpectedResult = strings.TrimSpace(step.ExpectedResult)
		}
	}

	if in.ExpectedResult == "" {
		return validateResultArgs{}, fmt.Errorf("expected_result is required")
	}
	return in, nil
}

// validate 执行验证
func (t *ValidateResultTool) validate(stepID int, expected, actual string, exitCode int, method string) *ValidationResult {
	result := &ValidationResult{
		StepID:   stepID,
		Expected: expected,
		Actual:   actual,
		Method:   method,
	}

	switch method {
	case "exact":
		// 精确匹配
		result.Valid = strings.TrimSpace(expected) == strings.TrimSpace(actual)
		if result.Valid {
			result.Message = "Output matches exactly"
		} else {
			result.Message = "Output does not match"
		}

	case "contains":
		// 包含匹配
		result.Valid = strings.Contains(actual, expected)
		if result.Valid {
			result.Message = fmt.Sprintf("Output contains expected string: %s", expected)
		} else {
			result.Message = fmt.Sprintf("Output does not contain expected string: %s", expected)
		}

	case "regex":
		// 正则匹配
		matched, err := regexp.MatchString(expected, actual)
		if err != nil {
			result.Valid = false
			result.Message = fmt.Sprintf("Invalid regex pattern: %s", err.Error())
		} else {
			result.Valid = matched
			if matched {
				result.Message = "Output matches regex pattern"
			} else {
				result.Message = "Output does not match regex pattern"
			}
		}

	case "exit_code":
		// 退出码验证
		expectedCode := 0
		fmt.Sscanf(expected, "%d", &expectedCode)
		result.Valid = exitCode == expectedCode
		if result.Valid {
			result.Message = fmt.Sprintf("Exit code matches: %d", exitCode)
		} else {
			result.Message = fmt.Sprintf("Exit code mismatch: expected %d, got %d", expectedCode, exitCode)
		}

	case "not_empty":
		// 非空验证
		result.Valid = strings.TrimSpace(actual) != ""
		if result.Valid {
			result.Message = "Output is not empty"
		} else {
			result.Message = "Output is empty"
		}

	case "success":
		// 成功验证（退出码为 0）
		result.Valid = exitCode == 0
		if result.Valid {
			result.Message = "Command executed successfully (exit code 0)"
		} else {
			result.Message = fmt.Sprintf("Command failed with exit code %d", exitCode)
		}

	default:
		result.Valid = false
		result.Message = fmt.Sprintf("Unknown validation method: %s", method)
		result.FailureCategory = "validation_error"
	}

	t.applyHeuristics(result, exitCode)
	return result
}

// applyHeuristics 对观测类步骤应用语义校验，减少字面 contains 误停，并识别计划失配。
// 输入：result（基础校验结果）、exitCode（命令退出码）。
// 输出：直接修改 result。
func (t *ValidateResultTool) applyHeuristics(result *ValidationResult, exitCode int) {
	if result == nil {
		return
	}

	expected := strings.TrimSpace(result.Expected)
	actual := strings.TrimSpace(result.Actual)
	expectedLower := strings.ToLower(expected)
	actualLower := strings.ToLower(actual)

	if result.Valid {
		if shouldMarkPlanMismatch(expectedLower, actualLower) {
			result.FailureCategory = "plan_mismatch"
			result.MismatchDetected = true
			result.MismatchReason = summarizeRuntimeObservation(actual)
			if result.Message == "" {
				result.Message = "Observation succeeded, but runtime evidence conflicts with upstream fault hypothesis"
			}
		}
		if result.FailureCategory == "" {
			result.FailureCategory = "validated"
		}
		return
	}

	if exitCode == 0 && looksLikeObservationExpectation(expectedLower) && actual != "" &&
		(result.Method == "contains" || result.Method == "exact") {
		if shouldMarkPlanMismatch(expectedLower, actualLower) {
			result.Valid = true
			result.FailureCategory = "plan_mismatch"
			result.MismatchDetected = true
			result.MismatchReason = summarizeRuntimeObservation(actual)
			result.Message = "Observation returned valid evidence, but current runtime state conflicts with upstream hypothesis"
			return
		}

		result.Valid = true
		result.FailureCategory = "descriptive_observation"
		result.Message = "Observation step returned non-empty output; descriptive expectation treated as satisfied"
		return
	}

	if result.FailureCategory == "" {
		if exitCode != 0 {
			result.FailureCategory = "execution_failure"
		} else {
			result.FailureCategory = "validation_mismatch"
		}
	}
}

// looksLikeObservationExpectation 判断预期是否属于“获取/列出/查看”类型的描述性观测步骤。
// 输入：expectedLower（小写预期文本）。
// 输出：是否为观测型预期。
func looksLikeObservationExpectation(expectedLower string) bool {
	keywords := []string{
		"显示", "列出", "获取", "查看", "检查", "确认", "返回", "输出", "采集",
		"describe", "show", "list", "get", "view", "check", "fetch", "query",
		"状态", "事件", "日志", "资源", "信息", "详情", "使用率", "趋势",
	}
	for _, keyword := range keywords {
		if strings.Contains(expectedLower, keyword) {
			return true
		}
	}
	return false
}

// shouldMarkPlanMismatch 判断当前运行时证据是否与上游故障假设不一致。
// 输入：expectedLower（预期文本小写）、actualLower（实际输出小写）。
// 输出：是否应标记为计划失配。
func shouldMarkPlanMismatch(expectedLower, actualLower string) bool {
	if actualLower == "" {
		return false
	}
	if !runtimeLooksHealthy(actualLower) {
		return false
	}

	faultHints := []string{
		"异常", "故障", "错误", "失败", "panic", "oom", "notready", "evict", "killed",
		"crash", "重启", "终止", "超时", "pressure", "瓶颈", "warning", "error", "failed",
	}
	for _, hint := range faultHints {
		if strings.Contains(expectedLower, hint) {
			return true
		}
	}

	return false
}

// runtimeLooksHealthy 判断实际输出是否呈现“当前系统健康/已恢复”的信号。
// 输入：actualLower（小写输出）。
// 输出：是否存在健康信号。
func runtimeLooksHealthy(actualLower string) bool {
	if strings.Count(actualLower, "running") >= 2 && !containsAny(actualLower, "crashloopbackoff", "failed", "error", "pending", "evicted", "oomkilled") {
		return true
	}
	if containsAny(actualLower,
		"ready", "healthy", "normal", "all pods are running", "无资源瓶颈", "当前系统健康", "资源正常",
	) {
		return true
	}
	if percentages := extractPercentages(actualLower); len(percentages) > 0 {
		healthyCount := 0
		for _, value := range percentages {
			if value >= 0 && value < 80 {
				healthyCount++
			}
		}
		if healthyCount == len(percentages) {
			return true
		}
	}
	return false
}

// summarizeRuntimeObservation 生成面向上游重规划的运行时摘要。
// 输入：actual（原始实际输出）。
// 输出：裁剪后的摘要文本。
func summarizeRuntimeObservation(actual string) string {
	actual = strings.TrimSpace(actual)
	if actual == "" {
		return "当前运行时校验返回空结果，无法确认现场状态"
	}
	if runtimeLooksHealthy(strings.ToLower(actual)) {
		return clipValidationText("运行时校验显示当前资源状态整体健康，可能已恢复或上游观测已过期："+actual, 320)
	}
	return clipValidationText(actual, 320)
}

// containsAny 判断文本是否包含任一关键字。
// 输入：text、关键字列表。
// 输出：是否命中。
func containsAny(text string, keywords ...string) bool {
	for _, keyword := range keywords {
		if strings.Contains(text, keyword) {
			return true
		}
	}
	return false
}

// extractPercentages 提取输出中的百分比数值。
// 输入：文本。
// 输出：百分比数值列表。
func extractPercentages(text string) []int {
	matches := regexp.MustCompile(`\b(\d{1,3})%\b`).FindAllStringSubmatch(text, -1)
	values := make([]int, 0, len(matches))
	for _, match := range matches {
		if len(match) < 2 {
			continue
		}
		value, err := strconv.Atoi(match[1])
		if err != nil {
			continue
		}
		values = append(values, value)
	}
	return values
}

// clipValidationText 裁剪校验文本，避免将大段日志写回状态。
// 输入：text、最大 rune 数。
// 输出：裁剪后的文本。
func clipValidationText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if maxRunes <= 0 {
		maxRunes = 320
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}
