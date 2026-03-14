package tools

import (
	"encoding/json"
	"strings"
)

// isTruncatedJSONError 判断是否为“参数 JSON 被截断”错误。
// 输入：err（解析错误）。
// 输出：true 表示可进入容错路径。
func isTruncatedJSONError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "unexpected end of json input") ||
		strings.Contains(message, "unexpected eof")
}

// unmarshalOpsArgsLenient 对工具参数做容错解析。
// 输入：payload（原始 JSON）、target（目标结构体指针）。
// 输出：解析错误；截断 JSON 会自动降级为空对象解析。
func unmarshalOpsArgsLenient(payload string, target any) error {
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "{}" || strings.EqualFold(payload, "null") {
		return nil
	}
	if err := json.Unmarshal([]byte(payload), target); err == nil {
		return nil
	} else if !isTruncatedJSONError(err) {
		return err
	}

	repaired := repairOpsTruncatedJSON(payload)
	if repaired == "" {
		repaired = "{}"
	}
	return json.Unmarshal([]byte(repaired), target)
}

// repairOpsTruncatedJSON 尝试补齐对象/数组尾部。
// 输入：payload（原始 JSON）。
// 输出：补齐后的 JSON。
func repairOpsTruncatedJSON(payload string) string {
	payload = strings.TrimSpace(payload)
	if payload == "" {
		return payload
	}
	if strings.HasSuffix(payload, ",") {
		payload = strings.TrimRight(payload, ",")
	}
	switch {
	case strings.HasPrefix(payload, "{"):
		openCount := strings.Count(payload, "{")
		closeCount := strings.Count(payload, "}")
		if closeCount < openCount {
			payload += strings.Repeat("}", openCount-closeCount)
		}
	case strings.HasPrefix(payload, "["):
		openCount := strings.Count(payload, "[")
		closeCount := strings.Count(payload, "]")
		if closeCount < openCount {
			payload += strings.Repeat("]", openCount-closeCount)
		}
	}
	return payload
}
