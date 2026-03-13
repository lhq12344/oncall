package tools

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
)

const toolCallStateSessionKey = "_ops_tool_call_state_v1"

// toolCallState 保存单轮 Agent 执行内的工具调用状态。
// 包括：
// - 相同参数调用的结果缓存（避免重复请求外部系统）
// - 工具总调用次数统计（用于日志观测）
type toolCallState struct {
	mu sync.RWMutex

	resultByKey map[string]string
	countByTool map[string]int
}

// getToolCallState 获取当前执行上下文中的工具调用状态。
// 输入：ctx（Agent 运行上下文）
// 输出：toolCallState 指针，若不存在则自动初始化并挂入 SessionValues。
func getToolCallState(ctx context.Context) *toolCallState {
	if ctx == nil {
		return nil
	}

	if v, ok := adk.GetSessionValue(ctx, toolCallStateSessionKey); ok {
		if state, ok := v.(*toolCallState); ok && state != nil {
			return state
		}
	}

	state := &toolCallState{
		resultByKey: make(map[string]string),
		countByTool: make(map[string]int),
	}
	adk.AddSessionValue(ctx, toolCallStateSessionKey, state)
	return state
}

// buildToolCallKey 构造工具调用缓存键。
// 输入：toolName、调用语义 key（已做归一化的参数串）
// 输出：固定长度缓存键。
func buildToolCallKey(toolName, callKey string) string {
	raw := strings.TrimSpace(toolName) + "|" + strings.TrimSpace(callKey)
	hash := sha1.Sum([]byte(raw))
	return strings.TrimSpace(toolName) + ":" + hex.EncodeToString(hash[:])
}

// getCachedToolResult 获取工具缓存结果。
// 输入：ctx、toolName、callKey
// 输出：缓存命中的结果与命中标记。
func getCachedToolResult(ctx context.Context, toolName, callKey string) (string, bool) {
	state := getToolCallState(ctx)
	if state == nil {
		return "", false
	}

	key := buildToolCallKey(toolName, callKey)
	state.mu.RLock()
	result, ok := state.resultByKey[key]
	state.mu.RUnlock()
	return result, ok
}

// setCachedToolResult 写入工具缓存结果。
// 输入：ctx、toolName、callKey、result
// 输出：无。
func setCachedToolResult(ctx context.Context, toolName, callKey, result string) {
	state := getToolCallState(ctx)
	if state == nil {
		return
	}

	key := buildToolCallKey(toolName, callKey)
	state.mu.Lock()
	state.resultByKey[key] = result
	state.mu.Unlock()
}

// increaseToolCallCount 增加工具调用次数并返回累计值。
// 输入：ctx、toolName
// 输出：该工具在本轮执行中的累计调用次数。
func increaseToolCallCount(ctx context.Context, toolName string) int {
	state := getToolCallState(ctx)
	if state == nil {
		return 0
	}

	toolName = strings.TrimSpace(toolName)
	if toolName == "" {
		toolName = "unknown_tool"
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	state.countByTool[toolName]++
	return state.countByTool[toolName]
}
