package tools

import (
	"context"
	"strings"

	"github.com/cloudwego/eino/adk"
)

// currentAgentForLog 从会话上下文中提取当前 agent 名称。
// 输入：ctx 与默认 agent 名称。
// 输出：可用于日志输出的 agent 名称。
func currentAgentForLog(ctx context.Context, fallback string) string {
	fallback = strings.TrimSpace(fallback)
	if fallback == "" {
		fallback = "unknown_agent"
	}
	if ctx == nil {
		return fallback
	}
	value, ok := adk.GetSessionValue(ctx, "current_agent")
	if !ok || value == nil {
		return fallback
	}
	name, ok := value.(string)
	if !ok {
		return fallback
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fallback
	}
	return name
}
