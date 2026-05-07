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

const rcaToolCallStateSessionKey = "_rca_tool_call_state_v1"

func init() {
	gob.Register(&rcaToolCallState{})
}

// rcaToolCallState 保存单轮 RCA 执行内的工具调用状态。
// 包括：
// - 相同参数调用的结果缓存（避免重复执行）
// - 工具累计调用次数（用于日志观测）
type rcaToolCallState struct {
	mu sync.RWMutex

	resultByKey map[string]string
	countByTool map[string]int
}

type rcaToolCallStateSnapshot struct {
	ResultByKey map[string]string
	CountByTool map[string]int
}

// GobEncode 自定义序列化 RCA 工具状态，保证 checkpoint/resume 后缓存与计数不丢失。
// 输入：无。
// 输出：gob 编码字节。
func (s *rcaToolCallState) GobEncode() ([]byte, error) {
	if s == nil {
		return nil, nil
	}

	s.mu.RLock()
	snapshot := rcaToolCallStateSnapshot{
		ResultByKey: cloneRCAStringMap(s.resultByKey),
		CountByTool: cloneRCAIntMap(s.countByTool),
	}
	s.mu.RUnlock()

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(snapshot); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GobDecode 自定义反序列化 RCA 工具状态。
// 输入：gob 编码字节。
// 输出：错误信息。
func (s *rcaToolCallState) GobDecode(data []byte) error {
	if len(data) == 0 {
		s.resultByKey = make(map[string]string)
		s.countByTool = make(map[string]int)
		return nil
	}

	var snapshot rcaToolCallStateSnapshot
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&snapshot); err != nil {
		return err
	}

	s.resultByKey = cloneRCAStringMap(snapshot.ResultByKey)
	s.countByTool = cloneRCAIntMap(snapshot.CountByTool)
	return nil
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

// cloneRCAStringMap 复制字符串映射。
// 输入：源 map。
// 输出：复制后的 map。
func cloneRCAStringMap(source map[string]string) map[string]string {
	if len(source) == 0 {
		return make(map[string]string)
	}
	target := make(map[string]string, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
}

// cloneRCAIntMap 复制整型映射。
// 输入：源 map。
// 输出：复制后的 map。
func cloneRCAIntMap(source map[string]int) map[string]int {
	if len(source) == 0 {
		return make(map[string]int)
	}
	target := make(map[string]int, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
}
