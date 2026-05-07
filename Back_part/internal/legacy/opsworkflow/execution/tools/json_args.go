package tools

import (
	"encoding/json"
	"strings"
)

// isTruncatedJSONError 判断是否为“JSON 被截断”类错误。
// 输入：err（解析错误）。
// 输出：true 表示可尝试容错修复。
func isTruncatedJSONError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(strings.TrimSpace(err.Error()))
	return strings.Contains(message, "unexpected end of json input") ||
		strings.Contains(message, "unexpected eof")
}

// unmarshalArgsLenient 容错解析工具参数。
// 输入：payload（原始 JSON 字符串）、target（目标结构体指针）。
// 输出：解析错误；截断 JSON 会尝试自动补齐后重试。
func unmarshalArgsLenient(payload string, target any) error {
	payload = strings.TrimSpace(payload)
	if payload == "" || payload == "{}" || strings.EqualFold(payload, "null") {
		return nil
	}
	if err := json.Unmarshal([]byte(payload), target); err == nil {
		return nil
	} else if !isTruncatedJSONError(err) {
		return err
	}

	repaired := repairTruncatedJSON(payload)
	if repaired == "" || repaired == payload {
		var emptyPayload = "{}"
		return json.Unmarshal([]byte(emptyPayload), target)
	}
	return json.Unmarshal([]byte(repaired), target)
}

// repairTruncatedJSON 尝试补齐被截断的 JSON 对象/数组。
// 输入：payload（原始 JSON 字符串）。
// 输出：补齐后的 JSON 字符串。
func repairTruncatedJSON(payload string) string {
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
