package tools

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
)

const rcaToolCallStateSessionKey = "_rca_tool_call_state_v1"

// rcaToolCallState 保存单轮 RCA 执行内的工具调用状态。
// 包括：
// - 相同参数调用的结果缓存（避免重复执行）
// - 工具累计调用次数（用于日志观测）
type rcaToolCallState struct {
	mu sync.RWMutex

	resultByKey map[string]string
	countByTool map[string]int
}

// getRCAToolCallState 获取当前执行上下文中的 RCA 工具调用状态。
// 输入：ctx（Agent 运行上下文）。
// 输出：状态对象；不存在时自动初始化并写入 SessionValues。
func getRCAToolCallState(ctx context.Context) *rcaToolCallState {
	if ctx == nil {
		return nil
	}

	if value, ok := adk.GetSessionValue(ctx, rcaToolCallStateSessionKey); ok {
		if state, ok := value.(*rcaToolCallState); ok && state != nil {
			return state
		}
	}

	state := &rcaToolCallState{
		resultByKey: make(map[string]string),
		countByTool: make(map[string]int),
	}
	adk.AddSessionValue(ctx, rcaToolCallStateSessionKey, state)
	return state
}

// buildRCAToolCallKey 构造 RCA 工具缓存键。
// 输入：toolName、callKey（归一化参数串）。
// 输出：固定长度哈希键。
func buildRCAToolCallKey(toolName, callKey string) string {
	raw := strings.TrimSpace(toolName) + "|" + strings.TrimSpace(callKey)
	sum := sha1.Sum([]byte(raw))
	return strings.TrimSpace(toolName) + ":" + hex.EncodeToString(sum[:])
}

// getRCACachedToolResult 获取 RCA 工具缓存结果。
// 输入：ctx、toolName、callKey。
// 输出：命中结果与命中标记。
func getRCACachedToolResult(ctx context.Context, toolName, callKey string) (string, bool) {
	state := getRCAToolCallState(ctx)
	if state == nil {
		return "", false
	}

	key := buildRCAToolCallKey(toolName, callKey)
	state.mu.RLock()
	result, ok := state.resultByKey[key]
	state.mu.RUnlock()
	return result, ok
}

// setRCACachedToolResult 写入 RCA 工具缓存结果。
// 输入：ctx、toolName、callKey、result。
// 输出：无。
func setRCACachedToolResult(ctx context.Context, toolName, callKey, result string) {
	state := getRCAToolCallState(ctx)
	if state == nil {
		return
	}

	key := buildRCAToolCallKey(toolName, callKey)
	state.mu.Lock()
	state.resultByKey[key] = result
	state.mu.Unlock()
}

// increaseRCAToolCallCount 增加工具调用次数并返回累计值。
// 输入：ctx、toolName。
// 输出：该工具在本轮执行中的累计调用次数。
func increaseRCAToolCallCount(ctx context.Context, toolName string) int {
	state := getRCAToolCallState(ctx)
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
