package tools

import (
	"bytes"
	"context"
	"encoding/gob"
	"encoding/json"
	"fmt"
	"regexp"
	"strings"
	"sync"

	"github.com/cloudwego/eino/adk"
)

const executionToolStateSessionKey = "_execution_tool_state_v1"
const maxRepeatedExecutionToolFailures = 3

var (
	executionFailureSpacePattern = regexp.MustCompile(`\s+`)
	executionFailureNoisePattern = regexp.MustCompile(`(?i)\b(plan_[a-z0-9_-]+|tooluse_[a-z0-9_-]+|step[_\s-]*\d+|\d{4}-\d{2}-\d{2}[t\s]\d{2}:\d{2}:\d{2}[^\s]*|\d+)\b`)
)

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
	validatedStepIDs          []int
	stepAttemptCount          map[int]int
	stepCommandAttemptCount   map[string]int
	stepCommandSucceeded      map[string]bool
	stepCommandLastResultJSON map[string]string
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
	repeatedFailureKey        string
	repeatedFailureReason     string
	repeatedFailureCount      int
	repeatedFailureLimit      int
}

type executionToolStateSnapshot struct {
	PlanPrepared              bool
	PlanID                    string
	PreparedPlanJSON          string
	ExecutedStepIDs           []int
	ValidatedStepIDs          []int
	StepAttemptCount          map[int]int
	StepCommandAttemptCount   map[string]int
	StepCommandSucceeded      map[string]bool
	StepCommandLastResultJSON map[string]string
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
	RepeatedFailureKey        string
	RepeatedFailureReason     string
	RepeatedFailureCount      int
	RepeatedFailureLimit      int
}

type repeatedExecutionFailureDecision struct {
	Count      int
	Limit      int
	Reached    bool
	Key        string
	Reason     string
	StopReason string
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
		ValidatedStepIDs:          append([]int(nil), s.validatedStepIDs...),
		StepAttemptCount:          cloneIntIntMap(s.stepAttemptCount),
		StepCommandAttemptCount:   cloneStringIntMap(s.stepCommandAttemptCount),
		StepCommandSucceeded:      cloneStringBoolMap(s.stepCommandSucceeded),
		StepCommandLastResultJSON: cloneStringMap(s.stepCommandLastResultJSON),
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
		RepeatedFailureKey:        s.repeatedFailureKey,
		RepeatedFailureReason:     s.repeatedFailureReason,
		RepeatedFailureCount:      s.repeatedFailureCount,
		RepeatedFailureLimit:      s.repeatedFailureLimit,
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
	s.validatedStepIDs = append([]int(nil), snapshot.ValidatedStepIDs...)
	s.stepAttemptCount = cloneIntIntMap(snapshot.StepAttemptCount)
	s.stepCommandAttemptCount = cloneStringIntMap(snapshot.StepCommandAttemptCount)
	s.stepCommandSucceeded = cloneStringBoolMap(snapshot.StepCommandSucceeded)
	s.stepCommandLastResultJSON = cloneStringMap(snapshot.StepCommandLastResultJSON)
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
	s.repeatedFailureKey = snapshot.RepeatedFailureKey
	s.repeatedFailureReason = snapshot.RepeatedFailureReason
	s.repeatedFailureCount = snapshot.RepeatedFailureCount
	s.repeatedFailureLimit = snapshot.RepeatedFailureLimit
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
	state.validatedStepIDs = nil
	state.stepAttemptCount = nil
	state.stepCommandAttemptCount = nil
	state.stepCommandSucceeded = nil
	state.stepCommandLastResultJSON = nil
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
	clearRepeatedExecutionToolFailureLocked(state)
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
		if !result.Skipped {
			if state.stepAttemptCount == nil {
				state.stepAttemptCount = make(map[int]int)
			}
			state.stepAttemptCount[result.StepID]++
		}
		for _, existing := range state.executedStepIDs {
			if existing == result.StepID {
				break
			}
		}
		if len(state.executedStepIDs) == 0 || state.executedStepIDs[len(state.executedStepIDs)-1] != result.StepID {
			exists := false
			for _, existing := range state.executedStepIDs {
				if existing == result.StepID {
					exists = true
					break
				}
			}
			if !exists {
				state.executedStepIDs = append(state.executedStepIDs, result.StepID)
			}
		}
		if result.Success && result.Resolved {
			appendValidatedStepIDLocked(state, result.StepID)
		}
	}
	commandKey := buildStepCommandKey(result.StepID, result.Command)
	if commandKey != "" {
		if state.stepCommandAttemptCount == nil {
			state.stepCommandAttemptCount = make(map[string]int)
		}
		if state.stepCommandSucceeded == nil {
			state.stepCommandSucceeded = make(map[string]bool)
		}
		if state.stepCommandLastResultJSON == nil {
			state.stepCommandLastResultJSON = make(map[string]string)
		}
		state.stepCommandAttemptCount[commandKey]++
		if result.Success && !result.Skipped {
			state.stepCommandSucceeded[commandKey] = true
		}
		state.stepCommandLastResultJSON[commandKey] = string(payload)
	}
	return nil
}

// getStepExecutionStats 获取步骤级执行统计。
// 输入：ctx、stepID。
// 输出：总执行次数、是否已完成校验、是否命中。
func getStepExecutionStats(ctx context.Context, stepID int) (int, bool, bool) {
	state := getExecutionToolState(ctx)
	if state == nil || stepID <= 0 {
		return 0, false, false
	}

	state.mu.RLock()
	defer state.mu.RUnlock()

	attemptCount := state.stepAttemptCount[stepID]
	validated := containsInt(state.validatedStepIDs, stepID)
	if attemptCount == 0 && !validated {
		return 0, false, false
	}
	return attemptCount, validated, true
}

// getStepCommandStats 获取同一步骤同一命令的执行统计。
// 输入：ctx、stepID、command。
// 输出：尝试次数、是否已成功、最近执行结果、是否命中。
func getStepCommandStats(ctx context.Context, stepID int, command string) (int, bool, *ExecutionResult, bool) {
	state := getExecutionToolState(ctx)
	if state == nil {
		return 0, false, nil, false
	}

	key := buildStepCommandKey(stepID, command)
	if key == "" {
		return 0, false, nil, false
	}

	state.mu.RLock()
	attemptCount := state.stepCommandAttemptCount[key]
	succeeded := state.stepCommandSucceeded[key]
	lastResultJSON := strings.TrimSpace(state.stepCommandLastResultJSON[key])
	state.mu.RUnlock()
	if attemptCount <= 0 && !succeeded && lastResultJSON == "" {
		return 0, false, nil, false
	}

	var lastResult *ExecutionResult
	if lastResultJSON != "" {
		var parsed ExecutionResult
		if err := json.Unmarshal([]byte(lastResultJSON), &parsed); err == nil {
			lastResult = &parsed
		}
	}
	return attemptCount, succeeded, lastResult, true
}

// buildStepCommandKey 构造步骤命令统计 key。
// 输入：stepID、command。
// 输出：归一化 key。
func buildStepCommandKey(stepID int, command string) string {
	command = strings.TrimSpace(command)
	if stepID <= 0 || command == "" {
		return ""
	}
	return fmt.Sprintf("%d|%s", stepID, strings.ToLower(command))
}

// cloneStringMap 复制字符串映射。
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

// cloneStringIntMap 复制字符串整型映射。
// 输入：源 map。
// 输出：复制后的 map。
func cloneStringIntMap(source map[string]int) map[string]int {
	if len(source) == 0 {
		return make(map[string]int)
	}
	target := make(map[string]int, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
}

// cloneIntIntMap 复制整型映射。
// 输入：源 map。
// 输出：复制后的 map。
func cloneIntIntMap(source map[int]int) map[int]int {
	if len(source) == 0 {
		return make(map[int]int)
	}
	target := make(map[int]int, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
}

// cloneStringBoolMap 复制字符串布尔映射。
// 输入：源 map。
// 输出：复制后的 map。
func cloneStringBoolMap(source map[string]bool) map[string]bool {
	if len(source) == 0 {
		return make(map[string]bool)
	}
	target := make(map[string]bool, len(source))
	for key, value := range source {
		target[key] = value
	}
	return target
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

// clearRepeatedExecutionToolFailureState 清理 execution 内部工具重复失败计数。
// 输入：ctx（Agent 运行上下文）。
// 输出：无。
func clearRepeatedExecutionToolFailureState(ctx context.Context) {
	state := getExecutionToolState(ctx)
	if state == nil {
		return
	}
	state.mu.Lock()
	defer state.mu.Unlock()
	clearRepeatedExecutionToolFailureLocked(state)
}

func clearRepeatedExecutionToolFailureLocked(state *executionToolState) {
	if state == nil {
		return
	}
	state.repeatedFailureKey = ""
	state.repeatedFailureReason = ""
	state.repeatedFailureCount = 0
	state.repeatedFailureLimit = maxRepeatedExecutionToolFailures
}

// recordExecutionToolFailure 记录 generate_plan/validate_plan/execute_step/validate_result 的同类失败。
// 输入：ctx、toolName、reason。
// 输出：累计结果；达到阈值时会同步标记 execution 停止并要求人工介入。
func recordExecutionToolFailure(ctx context.Context, toolName, reason string) repeatedExecutionFailureDecision {
	state := getExecutionToolState(ctx)
	if state == nil {
		return repeatedExecutionFailureDecision{Limit: maxRepeatedExecutionToolFailures}
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	return recordExecutionToolFailureLocked(state, toolName, reason)
}

func recordExecutionToolFailureLocked(state *executionToolState, toolName, reason string) repeatedExecutionFailureDecision {
	if state == nil {
		return repeatedExecutionFailureDecision{Limit: maxRepeatedExecutionToolFailures}
	}

	normalizedKey := normalizeExecutionFailureKey(reason)
	reason = strings.TrimSpace(reason)
	if normalizedKey == "" {
		normalizedKey = normalizeExecutionFailureKey(toolName)
	}
	if reason == "" {
		reason = strings.TrimSpace(toolName)
	}

	state.repeatedFailureLimit = maxRepeatedExecutionToolFailures
	if normalizedKey == "" {
		return repeatedExecutionFailureDecision{Limit: maxRepeatedExecutionToolFailures}
	}

	if normalizedKey == strings.TrimSpace(state.repeatedFailureKey) {
		state.repeatedFailureCount++
	} else {
		state.repeatedFailureKey = normalizedKey
		state.repeatedFailureReason = clipRepeatedFailureReason(reason)
		state.repeatedFailureCount = 1
	}

	decision := repeatedExecutionFailureDecision{
		Count:  state.repeatedFailureCount,
		Limit:  maxRepeatedExecutionToolFailures,
		Key:    state.repeatedFailureKey,
		Reason: firstNonEmptyExecutionText(state.repeatedFailureReason, reason),
	}
	if state.repeatedFailureCount >= maxRepeatedExecutionToolFailures {
		decision.Reached = true
		decision.StopReason = fmt.Sprintf(
			"execution_agent 内部连续 %d 次遇到同类失败（%s），已停止自动重试并建议人工处理",
			state.repeatedFailureCount,
			firstNonEmptyExecutionText(decision.Reason, toolName),
		)
		state.stopExecution = true
		state.stopAction = "manual_required"
		state.stopReason = decision.StopReason
	} else {
		decision.StopReason = fmt.Sprintf(
			"%s（同类失败累计 %d/%d）",
			firstNonEmptyExecutionText(decision.Reason, toolName),
			state.repeatedFailureCount,
			maxRepeatedExecutionToolFailures,
		)
	}
	return decision
}

func normalizeExecutionFailureKey(reason string) string {
	reason = strings.ToLower(strings.TrimSpace(reason))
	if reason == "" {
		return ""
	}
	reason = strings.NewReplacer(
		"\n", " ",
		"\r", " ",
		"\t", " ",
		"`", "",
		"\"", "",
		"'", "",
	).Replace(reason)
	reason = executionFailureNoisePattern.ReplaceAllString(reason, "#")
	reason = executionFailureSpacePattern.ReplaceAllString(reason, " ")
	return strings.TrimSpace(reason)
}

func clipRepeatedFailureReason(reason string) string {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return ""
	}
	runes := []rune(reason)
	if len(runes) <= 220 {
		return reason
	}
	return string(runes[:220]) + "..."
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

// markExecutionStopped 主动标记 execution 进入停止状态。
// 输入：ctx（Agent 运行上下文）、reason（停止原因）、action（停止动作，如 manual_required/replan）。
// 输出：无。
func markExecutionStopped(ctx context.Context, reason, action string) {
	state := getExecutionToolState(ctx)
	if state == nil {
		return
	}

	reason = strings.TrimSpace(reason)
	action = strings.TrimSpace(action)
	if reason == "" {
		reason = "execution stopped by tool guard"
	}
	if action == "" {
		action = "manual_required"
	}

	state.mu.Lock()
	defer state.mu.Unlock()
	state.stopExecution = true
	state.stopReason = reason
	state.stopAction = action
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
	if state.stopExecution {
		return true, state.stopReason
	}
	if stepID > 0 && containsInt(state.validatedStepIDs, stepID) {
		return true, fmt.Sprintf("步骤 %d 已完成校验，无需重复执行，请继续下一步", stepID)
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
		appendValidatedStepIDLocked(state, stepID)
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

// appendValidatedStepIDLocked 记录已完成校验的步骤。
// 输入：state、stepID。
// 输出：直接修改 state。
func appendValidatedStepIDLocked(state *executionToolState, stepID int) {
	if state == nil || stepID <= 0 {
		return
	}
	if containsInt(state.validatedStepIDs, stepID) {
		return
	}
	state.validatedStepIDs = append(state.validatedStepIDs, stepID)
}

// containsInt 判断整数切片是否包含目标值。
// 输入：values、target。
// 输出：是否包含。
func containsInt(values []int, target int) bool {
	for _, value := range values {
		if value == target {
			return true
		}
	}
	return false
}
