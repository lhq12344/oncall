package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	executiontools "go_agent/internal/agent/execution/tools"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

type planGateAgent struct {
	name   string
	desc   string
	logger *zap.Logger
}

func newPlanGateAgent(logger *zap.Logger) adk.Agent {
	return &planGateAgent{
		name:   "plan_gate",
		desc:   "validates the canonical Graph State ExecutionPlan before approval/execution",
		logger: logger,
	}
}

func (a *planGateAgent) Name(_ context.Context) string { return a.name }

func (a *planGateAgent) Description(_ context.Context) string { return a.desc }

func (a *planGateAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()

		state := getIncidentState(ctx)
		validation := validateCanonicalPlan(state)
		applyPlanGateValidationState(state, validation)
		appendIncidentExecutionLog(state, fmt.Sprintf("[plan_gate] plan_id=%s valid=%v blocked=%v risk=%s", validation.PlanID, validation.Valid, validation.Blocked, validation.RiskLevel))
		state.UpdatedAt = time.Now().Format(time.RFC3339)
		setIncidentState(ctx, state)

		payload, _ := json.Marshal(validation)
		if validation.Blocked || !validation.Valid {
			if a.logger != nil {
				a.logger.Warn("canonical plan blocked by plan gate", zap.String("plan_id", validation.PlanID), zap.Strings("reasons", validation.Reasons))
			}
			generator.Send(assistantEvent(string(payload)))
			return
		}
		if a.logger != nil {
			a.logger.Info("canonical plan passed plan gate", zap.String("plan_id", validation.PlanID), zap.String("risk_level", validation.RiskLevel))
		}
		generator.Send(assistantEvent(string(payload)))
	}()
	return iterator
}

func validateCanonicalPlan(state *IncidentState) *PlanValidationResult {
	if state == nil || state.PlanState == nil {
		return &PlanValidationResult{
			Valid:     false,
			Blocked:   true,
			RiskLevel: "high",
			Reasons:   []string{"missing canonical execution plan in Graph State"},
		}
	}
	plan := planStateToExecutionToolPlan(state.PlanState)
	result := executiontools.ValidateExecutionPlan(plan)
	return planValidationResultFromExecutionTools(result)
}

func planStateToExecutionToolPlan(plan *PlanState) *executiontools.ExecutionPlan {
	if plan == nil {
		return nil
	}
	steps := make([]executiontools.ExecutionStep, 0, len(plan.Steps))
	for _, step := range plan.Steps {
		steps = append(steps, executiontools.ExecutionStep{
			StepID:          step.StepID,
			Description:     strings.TrimSpace(step.Description),
			Command:         strings.TrimSpace(step.Command),
			Args:            append([]string(nil), step.Args...),
			ExpectedResult:  strings.TrimSpace(step.ExpectedResult),
			RollbackCommand: strings.TrimSpace(step.RollbackCommand),
			RollbackArgs:    append([]string(nil), step.RollbackArgs...),
			Timeout:         step.Timeout,
			Critical:        step.Critical,
		})
	}
	return &executiontools.ExecutionPlan{
		PlanID:        strings.TrimSpace(plan.PlanID),
		Description:   strings.TrimSpace(plan.Description),
		Steps:         steps,
		TotalSteps:    plan.TotalSteps,
		EstimatedTime: plan.EstimatedTime,
		RiskLevel:     strings.TrimSpace(plan.RiskLevel),
	}
}

func planValidationResultFromExecutionTools(result *executiontools.PlanValidationResult) *PlanValidationResult {
	if result == nil {
		return &PlanValidationResult{Valid: false, Blocked: true, RiskLevel: "high", Reasons: []string{"plan validation returned nil"}}
	}
	return &PlanValidationResult{
		Valid:                result.Valid,
		Blocked:              result.Blocked,
		RequiresConfirmation: result.RequiresConfirmation || len(result.ReviewCommands) > 0,
		RiskLevel:            strings.TrimSpace(result.RiskLevel),
		Reasons:              append([]string(nil), result.Reasons...),
		UnsafeCommands:       append([]string(nil), result.UnsafeCommands...),
		ReviewCommands:       append([]string(nil), result.ReviewCommands...),
		PlanID:               strings.TrimSpace(result.PlanID),
	}
}

type planApprovalAgent struct {
	name   string
	desc   string
	logger *zap.Logger
}

func newPlanApprovalAgent(logger *zap.Logger) adk.ResumableAgent {
	return &planApprovalAgent{
		name:   "plan_approval",
		desc:   "binds approval to the full canonical plan snapshot before execution",
		logger: logger,
	}
}

func (a *planApprovalAgent) Name(_ context.Context) string { return a.name }

func (a *planApprovalAgent) Description(_ context.Context) string { return a.desc }

func (a *planApprovalAgent) Run(ctx context.Context, _ *adk.AgentInput, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()

		state := getIncidentState(ctx)
		if ok, reason := planReadyForApproval(state); !ok {
			appendIncidentExecutionLog(state, "[plan_approval] skipped: "+reason)
			setIncidentState(ctx, state)
			generator.Send(assistantEvent("plan_approval skipped: " + reason))
			return
		}
		if currentPlanApproved(state) {
			generator.Send(assistantEvent(fmt.Sprintf("plan_approval passed: plan %s revision %d already approved.", state.PlanState.PlanID, state.PlanState.Revision)))
			return
		}
		if planApprovalRequired(state) {
			reason := planApprovalReason(state)
			markPlanApprovalPending(state, reason)
			setIncidentState(ctx, state)
			message := fmt.Sprintf("计划 %s revision %d 需要整体审批后才能执行。原因：%s", state.PlanState.PlanID, state.PlanState.Revision, reason)
			generator.Send(interruptEvent(ctx, &IncidentInterruptInfo{
				Type:         "plan_approval_required",
				Reason:       reason,
				PlanID:       state.PlanState.PlanID,
				PlanRevision: state.PlanState.Revision,
				SnapshotHash: state.PlanState.SnapshotHash,
				FallbackPlan: state.ExecutionFallback,
			}, message))
			return
		}
		approveCurrentPlan(state, "auto_low_risk")
		appendIncidentExecutionLog(state, fmt.Sprintf("[plan_approval] auto-approved low-risk plan %s revision %d", state.PlanState.PlanID, state.PlanState.Revision))
		setIncidentState(ctx, state)
		generator.Send(assistantEvent(fmt.Sprintf("plan_approval auto-approved low-risk plan %s revision %d.", state.PlanState.PlanID, state.PlanState.Revision)))
	}()
	return iterator
}

func (a *planApprovalAgent) Resume(ctx context.Context, info *adk.ResumeInfo, _ ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()
		if info == nil || !info.WasInterrupted {
			generator.Send(assistantEvent("plan_approval resume skipped: no pending plan approval."))
			return
		}
		if !info.IsResumeTarget {
			generator.Send(interruptEvent(ctx, &IncidentInterruptInfo{
				Type:   "plan_approval_required",
				Reason: "仍在等待整体计划审批",
			}, "当前仍等待整体计划审批，请回复批准或拒绝。"))
			return
		}

		approved, comment := parsePlanApprovalDecision(info.ResumeData)
		state := getIncidentState(ctx)
		if approved {
			if !pendingPlanApprovalMatchesCurrentPlan(state) {
				reason := "pending plan approval snapshot does not match current canonical plan"
				markPlanApprovalPending(state, reason)
				applyReplanDecisionState(state, "manual_required", reason, "plan_approval", "")
				setIncidentState(ctx, state)
				generator.Send(breakLoopEvent(a.name, "整体计划审批快照已变化，拒绝复用旧审批并停止自动执行："+reason))
				return
			}
			approveCurrentPlan(state, firstNonEmptyText(comment, "user"))
			appendIncidentExecutionLog(state, fmt.Sprintf("[plan_approval] user approved plan %s revision %d", state.PlanState.PlanID, state.PlanState.Revision))
			setIncidentState(ctx, state)
			generator.Send(assistantEvent(fmt.Sprintf("收到整体计划审批：%s。继续执行 plan %s revision %d。", firstNonEmptyText(comment, "approved"), state.PlanState.PlanID, state.PlanState.Revision)))
			return
		}

		reason := firstNonEmptyText(comment, "用户拒绝整体计划审批")
		rejectCurrentPlan(state, reason)
		applyReplanDecisionState(state, "manual_required", reason, "plan_approval", "")
		state.FinalStatus = "unresolved"
		setIncidentState(ctx, state)
		generator.Send(breakLoopEvent(a.name, "整体计划审批被拒绝，停止自动执行并进入最终报告："+reason))
	}()
	return iterator
}

func planReadyForApproval(state *IncidentState) (bool, string) {
	if state == nil || state.PlanState == nil || strings.TrimSpace(state.PlanState.PlanID) == "" {
		return false, "missing canonical plan"
	}
	if state.PlanGateState == nil {
		return false, "plan gate has not validated the canonical plan"
	}
	if !planGateMatchesCurrentPlan(state) {
		return false, "plan gate result is stale for current plan snapshot"
	}
	if state.PlanGateState.Blocked || !state.PlanGateState.Valid {
		return false, "plan gate blocked the canonical plan"
	}
	return true, ""
}

func planGateMatchesCurrentPlan(state *IncidentState) bool {
	if state == nil || state.PlanState == nil || state.PlanGateState == nil {
		return false
	}
	return strings.TrimSpace(state.PlanGateState.PlanID) == strings.TrimSpace(state.PlanState.PlanID) &&
		state.PlanGateState.Revision == state.PlanState.Revision &&
		strings.TrimSpace(state.PlanGateState.SnapshotHash) == strings.TrimSpace(state.PlanState.SnapshotHash)
}

func planApprovalRequired(state *IncidentState) bool {
	if state == nil || state.PlanGateState == nil {
		return false
	}
	return state.PlanGateState.RequiresApproval
}

func planApprovalReason(state *IncidentState) string {
	if state == nil || state.PlanGateState == nil {
		return "plan approval state unavailable"
	}
	reason := formatReasons(state.PlanGateState.Reasons)
	if reason == "" {
		reason = "risk_level=" + firstNonEmptyText(state.PlanGateState.RiskLevel, "unknown")
	}
	if len(state.PlanGateState.ReviewCommands) > 0 {
		reason = strings.TrimSpace(reason + "; review_commands=" + strings.Join(state.PlanGateState.ReviewCommands, " | "))
	}
	return clipText(reason, 600)
}

func currentPlanApproved(state *IncidentState) bool {
	if state == nil || state.PlanState == nil || state.PlanApprovalState == nil {
		return false
	}
	approval := state.PlanApprovalState
	return approval.Approved &&
		strings.EqualFold(strings.TrimSpace(approval.ApprovalStatus), "approved") &&
		strings.TrimSpace(approval.PlanID) == strings.TrimSpace(state.PlanState.PlanID) &&
		approval.Revision == state.PlanState.Revision &&
		strings.TrimSpace(approval.SnapshotHash) == strings.TrimSpace(state.PlanState.SnapshotHash)
}

func pendingPlanApprovalMatchesCurrentPlan(state *IncidentState) bool {
	if state == nil || state.PlanState == nil || state.PlanApprovalState == nil {
		return false
	}
	approval := state.PlanApprovalState
	return strings.EqualFold(strings.TrimSpace(approval.ApprovalStatus), "pending") &&
		strings.TrimSpace(approval.PlanID) == strings.TrimSpace(state.PlanState.PlanID) &&
		approval.Revision == state.PlanState.Revision &&
		strings.TrimSpace(approval.SnapshotHash) == strings.TrimSpace(state.PlanState.SnapshotHash)
}

func markPlanApprovalPending(state *IncidentState, reason string) {
	if state == nil || state.PlanState == nil {
		return
	}
	state.PlanApprovalState = &PlanApprovalState{
		PlanID:         state.PlanState.PlanID,
		Revision:       state.PlanState.Revision,
		SnapshotHash:   state.PlanState.SnapshotHash,
		ApprovalStatus: "pending",
		RejectedReason: clipText(reason, 600),
		ApprovalScope:  "full_plan",
	}
}

func approveCurrentPlan(state *IncidentState, approvedBy string) {
	if state == nil || state.PlanState == nil {
		return
	}
	state.PlanApprovalState = &PlanApprovalState{
		PlanID:         state.PlanState.PlanID,
		Revision:       state.PlanState.Revision,
		SnapshotHash:   state.PlanState.SnapshotHash,
		ApprovalStatus: "approved",
		Approved:       true,
		ApprovedBy:     clipText(approvedBy, 120),
		ApprovedAt:     time.Now().Format(time.RFC3339),
		ApprovalScope:  "full_plan",
	}
}

func rejectCurrentPlan(state *IncidentState, reason string) {
	if state == nil || state.PlanState == nil {
		return
	}
	state.PlanApprovalState = &PlanApprovalState{
		PlanID:         state.PlanState.PlanID,
		Revision:       state.PlanState.Revision,
		SnapshotHash:   state.PlanState.SnapshotHash,
		ApprovalStatus: "rejected",
		RejectedReason: clipText(reason, 600),
		ApprovalScope:  "full_plan",
	}
}
