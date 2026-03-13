package tools

import (
	"encoding/json"
	"strconv"
	"strings"
	"time"
)

// anyToString 将任意值转换为字符串。
// 输入：任意类型值。
// 输出：去空白后的字符串；无法转换时返回空字符串。
func anyToString(value any) string {
	switch v := value.(type) {
	case string:
		return strings.TrimSpace(v)
	case nil:
		return ""
	default:
		return strings.TrimSpace(toJSONString(v))
	}
}

// anyToInt 将任意值转换为 int。
// 输入：任意类型值。
// 输出：转换后的整数；无法转换时返回 0。
func anyToInt(value any) int {
	switch v := value.(type) {
	case int:
		return v
	case int64:
		return int(v)
	case int32:
		return int(v)
	case float64:
		return int(v)
	case float32:
		return int(v)
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return 0
		}
		return int(n)
	case string:
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			return 0
		}
		return n
	default:
		return 0
	}
}

// anyToFloat64 将任意值转换为 float64。
// 输入：任意类型值。
// 输出：转换后的浮点数；无法转换时返回 0。
func anyToFloat64(value any) float64 {
	switch v := value.(type) {
	case float64:
		return v
	case float32:
		return float64(v)
	case int:
		return float64(v)
	case int64:
		return float64(v)
	case int32:
		return float64(v)
	case json.Number:
		f, err := v.Float64()
		if err != nil {
			return 0
		}
		return f
	case string:
		f, err := strconv.ParseFloat(strings.TrimSpace(v), 64)
		if err != nil {
			return 0
		}
		return f
	default:
		return 0
	}
}

// anyToBool 将任意值转换为 bool。
// 输入：任意类型值。
// 输出：转换结果；无法转换时返回 false。
func anyToBool(value any) bool {
	switch v := value.(type) {
	case bool:
		return v
	case string:
		switch strings.ToLower(strings.TrimSpace(v)) {
		case "true", "1", "yes", "y", "ok":
			return true
		default:
			return false
		}
	case int:
		return v != 0
	case int64:
		return v != 0
	case float64:
		return v != 0
	default:
		return false
	}
}

// parseFlexibleTimeArg 宽容解析时间参数。
// 输入：任意类型时间值（RFC3339 字符串、Unix 秒/毫秒、{seconds,nanos}/{$date:...} 对象）。
// 输出：解析后的时间和是否解析成功。
func parseFlexibleTimeArg(value any) (time.Time, bool) {
	switch v := value.(type) {
	case nil:
		return time.Time{}, false
	case time.Time:
		return v, true
	case string:
		text := strings.TrimSpace(v)
		if text == "" {
			return time.Time{}, false
		}
		layouts := []string{
			time.RFC3339Nano,
			time.RFC3339,
			"2006-01-02 15:04:05",
			"2006-01-02 15:04",
			"2006-01-02",
		}
		for _, layout := range layouts {
			if ts, err := time.Parse(layout, text); err == nil {
				return ts, true
			}
		}
		if n, err := strconv.ParseInt(text, 10, 64); err == nil {
			return unixToTimeArg(n), true
		}
		return time.Time{}, false
	case int:
		return unixToTimeArg(int64(v)), true
	case int32:
		return unixToTimeArg(int64(v)), true
	case int64:
		return unixToTimeArg(v), true
	case float32:
		return unixToTimeArg(int64(v)), true
	case float64:
		return unixToTimeArg(int64(v)), true
	case json.Number:
		n, err := v.Int64()
		if err != nil {
			return time.Time{}, false
		}
		return unixToTimeArg(n), true
	case map[string]any:
		if dateValue, ok := v["$date"]; ok {
			return parseFlexibleTimeArg(dateValue)
		}
		seconds := int64(anyToInt(v["seconds"]))
		nanos := int64(anyToInt(v["nanos"]))
		if seconds == 0 && nanos == 0 {
			return time.Time{}, false
		}
		return time.Unix(seconds, nanos), true
	default:
		return time.Time{}, false
	}
}

// unixToTimeArg 将 Unix 时间戳转换为 time.Time。
// 输入：Unix 秒或毫秒时间戳。
// 输出：time.Time。
func unixToTimeArg(unix int64) time.Time {
	if unix > 1_000_000_000_000 {
		return time.UnixMilli(unix)
	}
	return time.Unix(unix, 0)
}

// toJSONString 将任意值序列化为 JSON 字符串。
// 输入：任意值。
// 输出：JSON 字符串；失败时回退为空字符串。
func toJSONString(value any) string {
	bytes, err := json.Marshal(value)
	if err != nil {
		return ""
	}
	return string(bytes)
}
