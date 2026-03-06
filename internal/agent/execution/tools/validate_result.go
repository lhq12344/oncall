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

// ValidateResultTool 验证结果工具
type ValidateResultTool struct {
	logger *zap.Logger
}

// ValidationResult 验证结果
type ValidationResult struct {
	StepID   int    `json:"step_id"`
	Valid    bool   `json:"valid"`
	Message  string `json:"message"`
	Expected string `json:"expected"`
	Actual   string `json:"actual"`
	Method   string `json:"method"` // exact/contains/regex/exit_code
}

func NewValidateResultTool(logger *zap.Logger) tool.BaseTool {
	return &ValidateResultTool{
		logger: logger,
	}
}

func (t *ValidateResultTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "validate_result",
		Desc: "验证执行结果是否符合预期。支持精确匹配、包含匹配、正则匹配和退出码验证。",
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
				Desc:     "验证方法：exact/contains/regex/exit_code，默认 contains",
				Required: false,
			},
		}),
	}, nil
}

func (t *ValidateResultTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		StepID           int    `json:"step_id"`
		ExpectedResult   string `json:"expected_result"`
		ActualOutput     string `json:"actual_output"`
		ExitCode         int    `json:"exit_code"`
		ValidationMethod string `json:"validation_method"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if in.ExpectedResult == "" {
		return "", fmt.Errorf("expected_result is required")
	}

	// 默认验证方法
	if in.ValidationMethod == "" {
		in.ValidationMethod = "contains"
	}

	// 执行验证
	result := t.validate(in.StepID, in.ExpectedResult, in.ActualOutput, in.ExitCode, in.ValidationMethod)

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("validation completed",
			zap.Int("step_id", in.StepID),
			zap.Bool("valid", result.Valid),
			zap.String("method", result.Method))
	}

	return string(output), nil
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
	}

	return result
}
