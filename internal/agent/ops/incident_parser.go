package ops

import (
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/adk"
)

func parseRCAReport(messages []adk.Message) (*RCAReport, bool) {
	_, raw, ok := findLatestJSONObject(messages, "root_cause", "confidence")
	if !ok {
		return nil, false
	}

	var report RCAReport
	if err := json.Unmarshal([]byte(raw), &report); err != nil {
		return nil, false
	}
	return &report, true
}

func parseOpsExecutionPlan(messages []adk.Message) (*OpsExecutionPlan, bool) {
	obj, raw, ok := findLatestJSONObject(messages, "commands")
	if !ok {
		return nil, false
	}

	var plan OpsExecutionPlan
	if err := json.Unmarshal([]byte(raw), &plan); err == nil {
		return &plan, true
	}

	plan = OpsExecutionPlan{
		PlanID:       stringFromMap(obj, "plan_id"),
		Summary:      stringFromMap(obj, "summary"),
		RootCause:    stringFromMap(obj, "root_cause"),
		TargetNode:   stringFromMap(obj, "target_node"),
		RiskLevel:    stringFromMap(obj, "risk_level"),
		FallbackPlan: stringFromMap(obj, "fallback_plan"),
	}
	return &plan, true
}

func parseValidationResult(messages []adk.Message) (*PlanValidationResult, bool) {
	_, raw, ok := findLatestJSONObject(messages, "blocked", "risk_level")
	if !ok {
		return nil, false
	}

	var result PlanValidationResult
	if err := json.Unmarshal([]byte(raw), &result); err != nil {
		return nil, false
	}

	return &result, true
}

func parseExecutionResult(messages []adk.Message) (map[string]any, bool) {
	obj, _, ok := findLatestJSONObject(messages, "execution_status")
	if !ok {
		return nil, false
	}
	return obj, true
}

// parseExecutedStepCount 提取执行结果中的已执行步骤数量。
// 输入：execution_agent 输出的结构化结果对象。
// 输出：执行步骤数；无法识别时返回 0。
func parseExecutedStepCount(result map[string]any) int {
	if result == nil {
		return 0
	}
	value, exists := result["executed_steps"]
	if !exists || value == nil {
		return 0
	}
	switch typed := value.(type) {
	case []any:
		return len(typed)
	case []map[string]any:
		return len(typed)
	}
	return 0
}

// parseExecutionStatusText 提取 execution_status 字段文本。
// 输入：execution_agent 输出的结构化结果对象。
// 输出：标准化后的状态字符串（小写）；缺失时返回空字符串。
func parseExecutionStatusText(result map[string]any) string {
	if result == nil {
		return ""
	}
	value, exists := result["execution_status"]
	if !exists || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.ToLower(strings.TrimSpace(text))
	}
	return ""
}

// parseExecutionManualPlan 提取人工兜底执行计划文本。
// 输入：execution_agent 输出的结构化结果对象。
// 输出：manual_plan 文本；缺失时返回空字符串。
func parseExecutionManualPlan(result map[string]any) string {
	if result == nil {
		return ""
	}
	value, exists := result["manual_plan"]
	if !exists || value == nil {
		return ""
	}
	if text, ok := value.(string); ok {
		return strings.TrimSpace(text)
	}
	return ""
}

func parseStrategyReport(messages []adk.Message) (map[string]any, bool) {
	obj, _, ok := findLatestJSONObject(messages, "final_status")
	if !ok {
		return nil, false
	}
	return obj, true
}

func detectExecutionStatus(messages []adk.Message) ExecutionStatus {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg == nil {
			continue
		}

		content := strings.TrimSpace(msg.Content)
		if content == "" {
			continue
		}

		lower := strings.ToLower(content)

		// 优先读取结构化字段
		if obj, _, ok := findJSONObjectInText(content, "success"); ok {
			if success, ok := boolFromAny(obj["success"]); ok {
				return ExecutionStatus{
					Found:          true,
					Success:        success,
					ToolCannotFix:  containsToolFailureKeywords(lower),
					RawMessageHint: content,
				}
			}
		}

		// 退化到关键词判断
		if strings.Contains(lower, "execution_status") || strings.Contains(lower, "执行") ||
			strings.Contains(lower, "failed") || strings.Contains(lower, "success") {
			status := ExecutionStatus{
				Found:          true,
				Success:        !containsFailureKeywords(lower),
				ToolCannotFix:  containsToolFailureKeywords(lower),
				RawMessageHint: content,
			}
			if containsFailureKeywords(lower) {
				status.Success = false
			}
			if strings.Contains(lower, "success") || strings.Contains(lower, "成功") {
				status.Success = true
			}
			return status
		}
	}

	return ExecutionStatus{}
}

func containsFailureKeywords(lower string) bool {
	return strings.Contains(lower, "failed") ||
		strings.Contains(lower, "error") ||
		strings.Contains(lower, "执行失败") ||
		strings.Contains(lower, "\"success\":false")
}

func containsToolFailureKeywords(lower string) bool {
	return strings.Contains(lower, "command not in whitelist") ||
		strings.Contains(lower, "unsafe arguments") ||
		strings.Contains(lower, "需要人工") ||
		strings.Contains(lower, "manual") ||
		strings.Contains(lower, "tool cannot") ||
		strings.Contains(lower, "无法自动修复")
}

func findLatestJSONObject(messages []adk.Message, requiredKeys ...string) (map[string]any, string, bool) {
	for i := len(messages) - 1; i >= 0; i-- {
		msg := messages[i]
		if msg == nil || strings.TrimSpace(msg.Content) == "" {
			continue
		}

		obj, raw, ok := findJSONObjectInText(msg.Content, requiredKeys...)
		if ok {
			return obj, raw, true
		}
	}
	return nil, "", false
}

func findJSONObjectInText(content string, requiredKeys ...string) (map[string]any, string, bool) {
	candidates := extractJSONCandidates(content)
	for i := len(candidates) - 1; i >= 0; i-- {
		raw := strings.TrimSpace(candidates[i])
		if raw == "" {
			continue
		}

		obj := map[string]any{}
		if err := json.Unmarshal([]byte(raw), &obj); err != nil {
			continue
		}

		if hasAllKeys(obj, requiredKeys...) {
			return obj, raw, true
		}
	}
	return nil, "", false
}

func extractJSONCandidates(text string) []string {
	var (
		candidates []string
		start      = -1
		depth      = 0
		inString   = false
		escaped    = false
	)

	for index, r := range text {
		switch {
		case escaped:
			escaped = false
		case r == '\\' && inString:
			escaped = true
		case r == '"':
			inString = !inString
		case !inString && r == '{':
			if depth == 0 {
				start = index
			}
			depth++
		case !inString && r == '}':
			if depth == 0 {
				continue
			}
			depth--
			if depth == 0 && start >= 0 && index >= start {
				candidates = append(candidates, text[start:index+1])
				start = -1
			}
		}
	}

	return candidates
}

func hasAllKeys(obj map[string]any, keys ...string) bool {
	for _, key := range keys {
		if _, ok := obj[key]; !ok {
			return false
		}
	}
	return true
}

func stringFromMap(obj map[string]any, key string) string {
	value, ok := obj[key]
	if !ok || value == nil {
		return ""
	}
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	default:
		b, err := json.Marshal(v)
		if err != nil {
			return ""
		}
		return string(b)
	}
}
