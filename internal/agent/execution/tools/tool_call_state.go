package tools

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
)

const executionToolStateSessionKey = "_execution_tool_state_v1"

func init() {
	gob.Register(&executionToolState{})
}

// executionToolState 保存单轮 execution_agent 执行内的状态。
// 用于：
// 1. 记录计划是否已准备完成。
// 2. 记录验证统计信息，用于日志观测与最终报告。
type executionToolState struct {
	mu sync.RWMutex

	planPrepared              bool
	planID                    string
	preparedPlanJSON          string
	executedStepIDs           []int
	lastExecutedStepID        int
	lastExecutionResultJSON   string
	planValidated             bool
	validatedPlanID           string
	hasValidStep              bool
	lastValidStepID           int
	consecutiveHardInvalids   int
	consecutivePlanMismatches int
	stopExecution             bool
	stopReason                string
	stopAction                string
	lastRuntimeSummary        string
}

type executionToolStateSnapshot struct {
	PlanPrepared              bool
	PlanID                    string
	PreparedPlanJSON          string
	ExecutedStepIDs           []int
	LastExecutedStepID        int
	LastExecutionResultJSON   string
	PlanValidated             bool
	ValidatedPlanID           string
	HasValidStep              bool
	LastValidStepID           int
	ConsecutiveHardInvalids   int
	ConsecutivePlanMismatches int
	StopExecution             bool
	StopReason                string
	StopAction                string
	LastRuntimeSummary        string
}

// GobEncode 自定义序列化 execution 内部状态，确保 checkpoint/resume 后校验进度不丢失。
// 输入：无。
// 输出：gob 编码后的状态字节。
func (s *executionToolState) GobEncode() ([]byte, error) {
	if s == nil {
		return nil, nil
	}

	s.mu.RLock()
	snapshot := executionToolStateSnapshot{
		PlanPrepared:              s.planPrepared,
		PlanID:                    s.planID,
		PreparedPlanJSON:          s.preparedPlanJSON,
		ExecutedStepIDs:           append([]int(nil), s.executedStepIDs...),
		LastExecutedStepID:        s.lastExecutedStepID,
		LastExecutionResultJSON:   s.lastExecutionResultJSON,
		PlanValidated:             s.planValidated,
		ValidatedPlanID:           s.validatedPlanID,
		HasValidStep:              s.hasValidStep,
		LastValidStepID:           s.lastValidStepID,
		ConsecutiveHardInvalids:   s.consecutiveHardInvalids,
		ConsecutivePlanMismatches: s.consecutivePlanMismatches,
		StopExecution:             s.stopExecution,
		StopReason:                s.stopReason,
		StopAction:                s.stopAction,
		LastRuntimeSummary:        s.lastRuntimeSummary,
	}
	s.mu.RUnlock()

	var buf bytes.Buffer
	if err := gob.NewEncoder(&buf).Encode(snapshot); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// GobDecode 自定义反序列化 execution 内部状态。
// 输入：gob 编码字节。
// 输出：错误信息。
func (s *executionToolState) GobDecode(data []byte) error {
	if len(data) == 0 {
		*s = executionToolState{}
		return nil
	}

	var snapshot executionToolStateSnapshot
	if err := gob.NewDecoder(bytes.NewReader(data)).Decode(&snapshot); err != nil {
		return err
	}

	s.planPrepared = snapshot.PlanPrepared
	s.planID = snapshot.PlanID
	s.preparedPlanJSON = snapshot.PreparedPlanJSON
	s.executedStepIDs = append([]int(nil), snapshot.ExecutedStepIDs...)
	s.lastExecutedStepID = snapshot.LastExecutedStepID
	s.lastExecutionResultJSON = snapshot.LastExecutionResultJSON
	s.planValidated = snapshot.PlanValidated
	s.validatedPlanID = snapshot.ValidatedPlanID
	s.hasValidStep = snapshot.HasValidStep
	s.lastValidStepID = snapshot.LastValidStepID
	s.consecutiveHardInvalids = snapshot.ConsecutiveHardInvalids
	s.consecutivePlanMismatches = snapshot.ConsecutivePlanMismatches
	s.stopExecution = snapshot.StopExecution
	s.stopReason = snapshot.StopReason
	s.stopAction = snapshot.StopAction
	s.lastRuntimeSummary = snapshot.LastRuntimeSummary
	return nil
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

// markExecutionPlanPrepared 标记当前执行轮次已完成计划生成。
// 输入：ctx（Agent 运行上下文）、planID（计划 ID，可为空）。
// 输出：无。
func markExecutionPlanPrepared(ctx context.Context, planID string) {
	state := getExecutionToolState(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.planPrepared = true
	state.planID = planID
	state.preparedPlanJSON = ""
	state.executedStepIDs = nil
	state.lastExecutedStepID = 0
	state.lastExecutionResultJSON = ""
	state.planValidated = false
	state.validatedPlanID = ""
	state.hasValidStep = false
	state.lastValidStepID = 0
	state.consecutiveHardInvalids = 0
	state.consecutivePlanMismatches = 0
	state.stopExecution = false
	state.stopReason = ""
	state.stopAction = ""
	state.lastRuntimeSummary = ""
}

// hasPreparedExecutionPlan 判断当前执行轮次是否已生成可执行计划。
// 输入：ctx（Agent 运行上下文）。
// 输出：是否已准备计划、计划 ID。
func hasPreparedExecutionPlan(ctx context.Context) (bool, string) {
	state := getExecutionToolState(ctx)
	if state == nil {
		return false, ""
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.planPrepared, state.planID
}

// rememberExecutionPlan 缓存当前执行轮次的完整计划，供 validate_plan 等工具兜底复用。
// 输入：ctx（Agent 运行上下文）、plan（完整命令级执行计划）。
// 输出：错误信息。
func rememberExecutionPlan(ctx context.Context, plan *ExecutionPlan) error {
	if plan == nil {
		return fmt.Errorf("execution plan is nil")
	}

	payload, err := json.Marshal(plan)
	if err != nil {
		return fmt.Errorf("marshal execution plan failed: %w", err)
	}

	state := getExecutionToolState(ctx)
	if state == nil {
		return nil
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	state.preparedPlanJSON = string(payload)
	return nil
}

// getPreparedExecutionPlan 读取当前执行轮次已缓存的完整执行计划。
// 输入：ctx（Agent 运行上下文）。
// 输出：执行计划、是否命中。
func getPreparedExecutionPlan(ctx context.Context) (*ExecutionPlan, bool) {
	state := getExecutionToolState(ctx)
	if state == nil {
		return nil, false
	}

	state.mu.RLock()
	payload := strings.TrimSpace(state.preparedPlanJSON)
	state.mu.RUnlock()
	if payload == "" {
		return nil, false
	}

	var plan ExecutionPlan
	if err := json.Unmarshal([]byte(payload), &plan); err != nil {
		return nil, false
	}
	if len(plan.Steps) == 0 {
		return nil, false
	}
	return &plan, true
}

// getExecutionPlanStepByID 从当前缓存计划中查找指定步骤。
// 输入：ctx、stepID。
// 输出：命中的步骤与是否存在。
func getExecutionPlanStepByID(ctx context.Context, stepID int) (*ExecutionStep, bool) {
	plan, ok := getPreparedExecutionPlan(ctx)
	if !ok || plan == nil {
		return nil, false
	}
	for _, step := range plan.Steps {
		if step.StepID == stepID {
			copied := step
			return &copied, true
		}
	}
	return nil, false
}

// getNextPendingExecutionStep 获取当前计划中尚未执行的下一步。
// 输入：ctx。
// 输出：下一待执行步骤与是否存在。
func getNextPendingExecutionStep(ctx context.Context) (*ExecutionStep, bool) {
	plan, ok := getPreparedExecutionPlan(ctx)
	if !ok || plan == nil {
		return nil, false
	}

	state := getExecutionToolState(ctx)
	if state == nil {
		return nil, false
	}

	state.mu.RLock()
	executedSet := make(map[int]struct{}, len(state.executedStepIDs))
	for _, stepID := range state.executedStepIDs {
		executedSet[stepID] = struct{}{}
	}
	state.mu.RUnlock()

	for _, step := range plan.Steps {
		if _, exists := executedSet[step.StepID]; exists {
			continue
		}
		copied := step
		return &copied, true
	}
	return nil, false
}

// rememberExecutionResult 缓存最近一次 execute_step 的结果，并记录步骤已处理。
// 输入：ctx、result。
// 输出：错误信息。
func rememberExecutionResult(ctx context.Context, result *ExecutionResult) error {
	if result == nil {
		return fmt.Errorf("execution result is nil")
	}

	payload, err := json.Marshal(result)
	if err != nil {
		return fmt.Errorf("marshal execution result failed: %w", err)
	}

	state := getExecutionToolState(ctx)
	if state == nil {
		return nil
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	state.lastExecutedStepID = result.StepID
	state.lastExecutionResultJSON = string(payload)
	if result.StepID > 0 {
		for _, existing := range state.executedStepIDs {
			if existing == result.StepID {
				return nil
			}
		}
		state.executedStepIDs = append(state.executedStepIDs, result.StepID)
	}
	return nil
}

// getLastExecutionResult 获取最近一次 execute_step 的结构化结果。
// 输入：ctx。
// 输出：执行结果与是否命中。
func getLastExecutionResult(ctx context.Context) (*ExecutionResult, bool) {
	state := getExecutionToolState(ctx)
	if state == nil {
		return nil, false
	}

	state.mu.RLock()
	payload := strings.TrimSpace(state.lastExecutionResultJSON)
	state.mu.RUnlock()
	if payload == "" {
		return nil, false
	}

	var result ExecutionResult
	if err := json.Unmarshal([]byte(payload), &result); err != nil {
		return nil, false
	}
	return &result, true
}

// markExecutionPlanValidated 标记当前执行轮次的计划已通过 validate_plan。
// 输入：ctx（Agent 运行上下文）、planID（计划 ID，可为空）。
// 输出：无。
func markExecutionPlanValidated(ctx context.Context, planID string) {
	state := getExecutionToolState(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.planValidated = true
	state.validatedPlanID = planID
}

// clearExecutionPlanValidated 清理计划校验状态。
// 输入：ctx（Agent 运行上下文）。
// 输出：无。
func clearExecutionPlanValidated(ctx context.Context) {
	state := getExecutionToolState(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	state.planValidated = false
	state.validatedPlanID = ""
}

// hasValidatedExecutionPlan 判断当前执行轮次是否已通过 validate_plan。
// 输入：ctx（Agent 运行上下文）。
// 输出：是否已通过校验、已校验计划 ID。
func hasValidatedExecutionPlan(ctx context.Context) (bool, string) {
	state := getExecutionToolState(ctx)
	if state == nil {
		return false, ""
	}
	state.mu.RLock()
	defer state.mu.RUnlock()
	return state.planValidated, state.validatedPlanID
}

// shouldSkipExecutionStep 判断当前步骤是否应被跳过。
// 输入：ctx、stepID。
// 输出：是否跳过、跳过原因。
func shouldSkipExecutionStep(ctx context.Context, stepID int) (bool, string) {
	state := getExecutionToolState(ctx)
	if state == nil {
		return false, ""
	}
	_ = stepID
	state.mu.RLock()
	defer state.mu.RUnlock()
	if state.stopExecution {
		return true, state.stopReason
	}
	return false, ""
}

// validationProgressDecision 描述 validate_result 对当前执行轮次的调度决策。
// 用于：区分“硬失败需人工处理”和“计划失配需重规划”。
type validationProgressDecision struct {
	ShouldStop     bool
	StopReason     string
	StopAction     string
	RuntimeSummary string
}

// recordValidationProgress 记录验证结果并返回是否应提前收敛。
// 输入：ctx、stepID、validation（当前步骤验证结果）。
// 输出：停止决策（是否停止、原因、动作类型、运行时摘要）。
func recordValidationProgress(ctx context.Context, stepID int, validation *ValidationResult) validationProgressDecision {
	state := getExecutionToolState(ctx)
	if state == nil || validation == nil {
		return validationProgressDecision{}
	}

	state.mu.Lock()
	defer state.mu.Unlock()

	if validation.Valid {
		state.hasValidStep = true
		state.lastValidStepID = stepID
		state.consecutiveHardInvalids = 0
	} else {
		state.consecutivePlanMismatches = 0
	}

	if validation.MismatchDetected {
		state.consecutivePlanMismatches++
		state.lastRuntimeSummary = strings.TrimSpace(firstNonEmptyExecutionText(validation.MismatchReason, validation.Actual))
		if state.consecutivePlanMismatches >= 2 {
			state.stopExecution = true
			state.stopAction = "replan"
			state.stopReason = fmt.Sprintf(
				"连续 %d 步观测结果与上游故障假设不一致，当前现场可能已恢复或上游 RCA/观测已过期，建议刷新现场认知后重规划",
				state.consecutivePlanMismatches,
			)
			return validationProgressDecision{
				ShouldStop:     true,
				StopReason:     state.stopReason,
				StopAction:     state.stopAction,
				RuntimeSummary: state.lastRuntimeSummary,
			}
		}
		return validationProgressDecision{
			ShouldStop:     false,
			RuntimeSummary: state.lastRuntimeSummary,
		}
	}

	if validation.Valid {
		state.consecutivePlanMismatches = 0
		state.lastRuntimeSummary = ""
		return validationProgressDecision{}
	}

	state.consecutiveHardInvalids++
	if state.consecutiveHardInvalids >= 2 {
		state.stopExecution = true
		state.stopAction = "manual_required"
		state.stopReason = fmt.Sprintf("连续 %d 步校验失败，判定当前执行计划可能不匹配，停止自动执行并建议人工处理", state.consecutiveHardInvalids)
		return validationProgressDecision{
			ShouldStop: true,
			StopReason: state.stopReason,
			StopAction: state.stopAction,
		}
	}
	return validationProgressDecision{}
}

// firstNonEmptyExecutionText 返回第一个非空文本。
// 输入：候选字符串列表。
// 输出：首个非空字符串；若均为空则返回空字符串。
func firstNonEmptyExecutionText(values ...string) string {
	for _, value := range values {
		if strings.TrimSpace(value) != "" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}
