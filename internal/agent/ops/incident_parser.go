package ops

import (
	"encoding/json"
	"strings"

	"github.com/cloudwego/eino/adk"
)

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
