package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// TimeQueryTool 提供 RCA 阶段的时间查询与时间差分析能力。
type TimeQueryTool struct {
	logger *zap.Logger
}

// NewTimeQueryTool 创建时间查询工具。
// 输入：logger。
// 输出：可注册到 RCA Agent 的时间工具。
func NewTimeQueryTool(logger *zap.Logger) tool.BaseTool {
	return &TimeQueryTool{logger: logger}
}

func (t *TimeQueryTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "time_query",
		Desc: "查询当前时间，或比较故障时间、观测时间与当前时间的差异，帮助 RCA 判断数据是否过期。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {
				Type:     schema.String,
				Desc:     "操作类型：now（默认）或 compare",
				Required: false,
			},
			"timestamp": {
				Type:     schema.String,
				Desc:     "待分析时间，支持 RFC3339 / RFC3339Nano / 2006-01-02 15:04:05 / Unix 秒 / Unix 毫秒",
				Required: false,
			},
			"reference_time": {
				Type:     schema.String,
				Desc:     "参考时间；compare 时可选，默认当前时间",
				Required: false,
			},
			"timezone": {
				Type:     schema.String,
				Desc:     "时区名称，如 Asia/Shanghai；默认本地时区",
				Required: false,
			},
		}),
	}, nil
}

func (t *TimeQueryTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Action        string `json:"action"`
		Timestamp     string `json:"timestamp"`
		ReferenceTime string `json:"reference_time"`
		Timezone      string `json:"timezone"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Action = strings.ToLower(strings.TrimSpace(in.Action))
	if in.Action == "" {
		in.Action = "now"
	}
	if in.Action != "now" && in.Action != "compare" {
		return "", fmt.Errorf("unsupported action: %s", in.Action)
	}

	location, err := resolveLocation(strings.TrimSpace(in.Timezone))
	if err != nil {
		return "", err
	}

	callCount := increaseRCAToolCallCount(ctx, "time_query")
	cacheKey := strings.Join([]string{
		in.Action,
		strings.TrimSpace(in.Timestamp),
		strings.TrimSpace(in.ReferenceTime),
		location.String(),
	}, "|")
	if cached, ok := getRCACachedToolResult(ctx, "time_query", cacheKey); ok {
		if t.logger != nil {
			t.logger.Info("time query cache hit",
				zap.String("timezone", location.String()),
				zap.String("action", in.Action),
				zap.Int("call_count", callCount))
		}
		return cached, nil
	}

	now := time.Now().In(location)
	if in.Action == "now" {
		result := map[string]any{
			"action":           "now",
			"timezone":         location.String(),
			"current_time":     now.Format(time.RFC3339),
			"current_time_utc": now.UTC().Format(time.RFC3339),
			"unix_seconds":     now.Unix(),
			"unix_millis":      now.UnixMilli(),
		}
		return marshalAndCacheRCATimeResult(ctx, t.logger, cacheKey, result)
	}

	targetTime, err := parseFlexibleTimeString(strings.TrimSpace(in.Timestamp), location)
	if err != nil {
		return "", fmt.Errorf("invalid timestamp: %w", err)
	}

	referenceTime := now
	if strings.TrimSpace(in.ReferenceTime) != "" {
		referenceTime, err = parseFlexibleTimeString(strings.TrimSpace(in.ReferenceTime), location)
		if err != nil {
			return "", fmt.Errorf("invalid reference_time: %w", err)
		}
	}

	diff := referenceTime.Sub(targetTime)
	result := map[string]any{
		"action":             "compare",
		"timezone":           location.String(),
		"timestamp":          targetTime.Format(time.RFC3339),
		"reference_time":     referenceTime.Format(time.RFC3339),
		"delta_seconds":      int64(diff.Seconds()),
		"delta_minutes":      diff.Minutes(),
		"delta_human":        humanizeDuration(diff),
		"is_past_reference":  diff >= 0,
		"reference_time_utc": referenceTime.UTC().Format(time.RFC3339),
		"timestamp_time_utc": targetTime.UTC().Format(time.RFC3339),
	}
	return marshalAndCacheRCATimeResult(ctx, t.logger, cacheKey, result)
}

// resolveLocation 解析时区字符串。
// 输入：timezone。
// 输出：时区对象。
func resolveLocation(timezone string) (*time.Location, error) {
	if timezone == "" {
		return time.Local, nil
	}
	location, err := time.LoadLocation(timezone)
	if err != nil {
		return nil, fmt.Errorf("invalid timezone: %w", err)
	}
	return location, nil
}

// parseFlexibleTimeString 解析多种时间格式。
// 输入：原始时间文本、默认时区。
// 输出：time.Time。
func parseFlexibleTimeString(raw string, location *time.Location) (time.Time, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, fmt.Errorf("time string is empty")
	}

	if unixValue, err := strconv.ParseInt(raw, 10, 64); err == nil {
		switch {
		case len(raw) >= 13:
			return time.UnixMilli(unixValue).In(location), nil
		default:
			return time.Unix(unixValue, 0).In(location), nil
		}
	}

	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05",
		"2006-01-02 15:04",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if parsed, err := time.ParseInLocation(layout, raw, location); err == nil {
			return parsed, nil
		}
	}

	return time.Time{}, fmt.Errorf("unsupported time format: %s", raw)
}

// humanizeDuration 将时长格式化为简短可读文本。
// 输入：duration。
// 输出：如 5m30s。
func humanizeDuration(duration time.Duration) string {
	if duration < 0 {
		duration = -duration
	}
	if duration < time.Second {
		return duration.String()
	}

	var parts []string
	hours := duration / time.Hour
	if hours > 0 {
		parts = append(parts, fmt.Sprintf("%dh", hours))
		duration -= hours * time.Hour
	}
	minutes := duration / time.Minute
	if minutes > 0 {
		parts = append(parts, fmt.Sprintf("%dm", minutes))
		duration -= minutes * time.Minute
	}
	seconds := duration / time.Second
	if seconds > 0 || len(parts) == 0 {
		parts = append(parts, fmt.Sprintf("%ds", seconds))
	}
	return strings.Join(parts, "")
}

// marshalAndCacheRCATimeResult 序列化并缓存时间工具结果。
// 输入：ctx、logger、cacheKey、结果对象。
// 输出：序列化 JSON。
func marshalAndCacheRCATimeResult(ctx context.Context, logger *zap.Logger, cacheKey string, result map[string]any) (string, error) {
	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal time query result: %w", err)
	}

	payload := string(output)
	setRCACachedToolResult(ctx, "time_query", cacheKey, payload)
	if logger != nil {
		logger.Info("time query completed",
			zap.String("action", strings.TrimSpace(fmt.Sprintf("%v", result["action"]))),
			zap.String("timezone", strings.TrimSpace(fmt.Sprintf("%v", result["timezone"]))))
	}
	return payload, nil
}
