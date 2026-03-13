package tools

import (
	"context"
	"fmt"
	"sync"

	"github.com/cloudwego/eino/adk"
)

const executionToolStateSessionKey = "_execution_tool_state_v1"

// executionToolState 保存单轮 execution_agent 执行内的状态。
// 用于：
// 1. 在首个有效验证后提前收敛，避免继续执行无必要步骤。
// 2. 在连续验证失败时提前停止自动执行，转人工处理。
type executionToolState struct {
	mu sync.RWMutex

	hasValidStep        bool
	lastValidStepID     int
	consecutiveInvalids int
}

// getExecutionToolState 获取 execution 工具状态。
// 输入：ctx（Agent 运行上下文）。
// 输出：状态对象；不存在时自动初始化并写入 SessionValues。
func getExecutionToolState(ctx context.Context) *executionToolState {
	if ctx == nil {
		return nil
	}
	if value, ok := adk.GetSessionValue(ctx, executionToolStateSessionKey); ok {
		if state, ok := value.(*executionToolState); ok && state != nil {
			return state
		}
	}
	state := &executionToolState{}
	adk.AddSessionValue(ctx, executionToolStateSessionKey, state)
	return state
}

// shouldSkipExecutionStep 判断当前步骤是否应被跳过。
// 输入：ctx、stepID。
// 输出：是否跳过、跳过原因。
func shouldSkipExecutionStep(ctx context.Context, stepID int) (bool, string) {
	state := getExecutionToolState(ctx)
	if state == nil {
		return false, ""
	}

	state.mu.RLock()
	defer state.mu.RUnlock()

	if state.hasValidStep {
		return true, fmt.Sprintf("步骤 %d 已验证通过，跳过后续执行步骤并等待汇总输出", state.lastValidStepID)
	}
	if state.consecutiveInvalids >= 2 {
		return true, "连续 2 步验证失败，停止自动执行并建议人工处理"
	}
	_ = stepID
	return false, ""
}

// recordValidationProgress 记录验证结果并返回是否应提前收敛。
// 输入：ctx、stepID、valid。
// 输出：是否建议停止后续步骤、原因描述。
func recordValidationProgress(ctx context.Context, stepID int, valid bool) (bool, string) {
	state := getExecutionToolState(ctx)
	if state == nil {
		return false, ""
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if valid {
		state.hasValidStep = true
		state.lastValidStepID = stepID
		state.consecutiveInvalids = 0
		return true, fmt.Sprintf("步骤 %d 验证通过，建议立即结束后续步骤并输出执行结果", stepID)
	}

	state.consecutiveInvalids++
	if state.consecutiveInvalids >= 2 {
		return true, "连续 2 步验证失败，建议停止自动执行并输出 manual_required"
	}
	return false, ""
}
