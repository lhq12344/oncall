package tools

import (
	"bytes"
	"context"
	"crypto/sha1"
	"encoding/gob"
	"encoding/hex"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
)

const toolCallStateSessionKey = "_ops_tool_call_state_v1"

func init() {
	gob.Register(&toolCallState{})
}

// toolCallState 保存单轮 Agent 执行内的工具调用状态。
// 包括：
// - 相同参数调用的结果缓存（避免重复请求外部系统）
// - 工具总调用次数统计（用于日志观测）
type toolCallState struct {
	mu sync.RWMutex

	resultByKey map[string]string
	countByTool map[string]int
}

type toolCallStateSnapshot struct {
	ResultByKey map[string]string
	CountByTool map[string]int
}

// GobEncode 自定义序列化 toolCallState，确保 checkpoint 可保存缓存与计数。
// 输入：无。
// 输出：gob 编码后的状态字节。
func (s *toolCallState) GobEncode() ([]byte, error) {
	if s == nil {
		return nil, nil
	}

	s.mu.RLock()
	snapshot := toolCallStateSnapshot{
		ResultByKey: cloneStringMap(s.resultByKey),
		CountByTool: cloneIntMap(s.countByTool),
	}
	s.mu.RUnlock()

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(snapshot); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GobDecode 自定义反序列化 toolCallState，恢复 checkpoint 中的缓存与计数。
// 输入：gob 编码字节。
// 输出：错误信息。
func (s *toolCallState) GobDecode(data []byte) error {
	if len(data) == 0 {
		s.resultByKey = make(map[string]string)
		s.countByTool = make(map[string]int)
		return nil
	}

	var snapshot toolCallStateSnapshot
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&snapshot); err != nil {
		return err
	}

	s.resultByKey = cloneStringMap(snapshot.ResultByKey)
	s.countByTool = cloneIntMap(snapshot.CountByTool)
	return nil
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

// cloneStringMap 复制字符串映射，避免状态快照共享底层引用。
// 输入：源 map。
// 输出：复制后的 map。
func cloneStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return make(map[string]string)
	}
	target := make(map[string]string, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
}

// cloneIntMap 复制整型映射，避免状态快照共享底层引用。
// 输入：源 map。
// 输出：复制后的 map。
func cloneIntMap(source map[string]int) map[string]int {
	if len(source) == 0 {
		return make(map[string]int)
	}
	target := make(map[string]int, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
}
