package ops

import (
	"testing"
)

func TestPlanGateValidatesCanonicalPlanAndApprovalBindsSnapshot(t *testing.T) {
	t.Parallel()

	state := &IncidentState{}
	applyExecutionPlanState(state, testGeneratedPlan("pod Running", "kubectl", []string{"get", "pod", "api-0", "-n", "infra"}))

	validation := validateCanonicalPlan(state)
	applyPlanGateValidationState(state, validation)
	if state.PlanGateState == nil || !state.PlanGateState.Valid || state.PlanGateState.Blocked {
		t.Fatalf("expected plan gate pass, got validation=%#v gate=%#v", validation, state.PlanGateState)
	}
	if state.PlanGateState.RequiresApproval {
		t.Fatalf("read-only low-risk plan should not require full approval: %#v", state.PlanGateState)
	}

	approveCurrentPlan(state, "test")
	if !currentPlanApproved(state) {
		t.Fatalf("expected current plan approval to match snapshot: %#v", state.PlanApprovalState)
	}
	applyExecutionPlanState(state, testGeneratedPlan("pod Ready", "kubectl", []string{"get", "pod", "api-0", "-n", "infra"}))
	if currentPlanApproved(state) {
		t.Fatalf("plan approval should be invalid after snapshot change: %#v", state.PlanApprovalState)
	}
}

func TestPlanGateBlocksUnsafeCanonicalPlanAndWritesReplanState(t *testing.T) {
	t.Parallel()

	state := &IncidentState{}
	applyExecutionPlanState(state, testGeneratedPlan("filesystem removed", "rm", []string{"-rf", "/"}))

	validation := validateCanonicalPlan(state)
	applyPlanGateValidationState(state, validation)
	if !validation.Blocked || validation.Valid {
		t.Fatalf("expected unsafe plan validation to be blocked, got validation=%#v", validation)
	}
	if state.ReplanState == nil || state.ReplanState.Decision != "refresh_observation" {
		t.Fatalf("expected blocked plan to request observation refresh, got %#v", state.ReplanState)
	}
	if state.PlanState != nil || state.PlanGateState != nil || state.PlanApprovalState != nil {
		t.Fatalf("refresh_observation should invalidate reusable plan state, plan=%#v gate=%#v approval=%#v", state.PlanState, state.PlanGateState, state.PlanApprovalState)
	}
	if state.ExecutionStatus != "replan_required" || !state.ObservationRefreshNeeded {
		t.Fatalf("expected replan-required state, got status=%q refresh=%v", state.ExecutionStatus, state.ObservationRefreshNeeded)
	}
}

func TestExecutionGuardRequiresPlanGateAndApproval(t *testing.T) {
	t.Parallel()

	state := &IncidentState{IncidentContractValid: true}
	applyExecutionPlanState(state, testGeneratedPlan("pod Running", "kubectl", []string{"get", "pod", "api-0", "-n", "infra"}))
	if allowed, reason := executionGuardAllowsExecution(state); allowed || reason == "" {
		t.Fatalf("expected guard to require plan gate, allowed=%v reason=%q", allowed, reason)
	}

	applyPlanGateValidationState(state, validateCanonicalPlan(state))
	if allowed, reason := executionGuardAllowsExecution(state); allowed || reason == "" {
		t.Fatalf("expected guard to require plan approval, allowed=%v reason=%q", allowed, reason)
	}

	approveCurrentPlan(state, "test")
	if allowed, reason := executionGuardAllowsExecution(state); !allowed || reason != "" {
		t.Fatalf("expected guard to allow approved canonical plan, allowed=%v reason=%q", allowed, reason)
	}
}

func TestPendingPlanApprovalMustMatchCurrentSnapshot(t *testing.T) {
	t.Parallel()

	state := &IncidentState{}
	applyExecutionPlanState(state, testGeneratedPlan("pod Running", "kubectl", []string{"get", "pod", "api-0", "-n", "infra"}))
	applyPlanGateValidationState(state, validateCanonicalPlan(state))
	markPlanApprovalPending(state, "review read-only plan")
	if !pendingPlanApprovalMatchesCurrentPlan(state) {
		t.Fatalf("expected pending approval to match current plan: %#v", state.PlanApprovalState)
	}

	applyExecutionPlanState(state, testGeneratedPlan("pod Ready", "kubectl", []string{"get", "pod", "api-0", "-n", "infra"}))
	if pendingPlanApprovalMatchesCurrentPlan(state) {
		t.Fatalf("stale pending approval should not match changed plan: %#v", state.PlanApprovalState)
	}
}

func TestParsePlanApprovalDecisionDeniesNegativePhrases(t *testing.T) {
	t.Parallel()

	cases := []any{
		"\u4e0d\u786e\u8ba4",
		"\u4e0d\u8981\u6267\u884c",
		"not yes",
		"approved:false",
		`{"approved":false,"comment":"no"}`,
		map[string]any{"approved": false, "comment": "reject this plan"},
		map[string]any{"resolved": true, "comment": "incident resolved but plan not approved"},
	}
	for _, input := range cases {
		approved, _ := parsePlanApprovalDecision(input)
		if approved {
			t.Fatalf("parsePlanApprovalDecision(%#v) approved a negative or non-approval decision", input)
		}
	}
}

func TestParsePlanApprovalDecisionAcceptsOnlyExplicitApproval(t *testing.T) {
	t.Parallel()

	cases := []any{
		"approved",
		"confirm",
		"\u786e\u8ba4",
		`{"approved":true,"comment":"user approved"}`,
		map[string]any{"approved": true, "comment": "approved by tester"},
	}
	for _, input := range cases {
		approved, _ := parsePlanApprovalDecision(input)
		if !approved {
			t.Fatalf("parsePlanApprovalDecision(%#v) did not approve an explicit approval", input)
		}
	}
}

func TestRefreshObservationInvalidatesApprovedPlanState(t *testing.T) {
	t.Parallel()

	state := &IncidentState{IncidentContractValid: true}
	applyExecutionPlanState(state, testGeneratedPlan("pod Running", "kubectl", []string{"get", "pod", "api-0", "-n", "infra"}))
	applyPlanGateValidationState(state, validateCanonicalPlan(state))
	approveCurrentPlan(state, "test")
	state.PlanVerification = &PlanVerificationState{
		PlanID:   state.PlanState.PlanID,
		Revision: state.PlanState.Revision,
		Status:   "success",
		Success:  true,
	}
	state.ExecutionStatus = "success"
	state.ExecutionSuccess = true
	state.ExecutionExecutedSteps = []ExecutionStepTrace{{StepID: 1, Ordinal: 1}}
	state.ExecutionStepCount = len(state.ExecutionExecutedSteps)
	oldPlanID := state.PlanState.PlanID
	oldRevision := state.PlanState.Revision

	applyReplanDecisionState(state, "refresh_observation", "runtime state changed", "test", "pod status changed")

	if currentPlanApproved(state) {
		t.Fatalf("stale approval should not remain current: %#v", state.PlanApprovalState)
	}
	if allowed, reason := executionGuardAllowsExecution(state); allowed || reason == "" {
		t.Fatalf("execution guard should reject invalidated plan, allowed=%v reason=%q", allowed, reason)
	}
	if state.PlanState != nil || state.PlanGateState != nil || state.PlanApprovalState != nil || state.PlanVerification != nil {
		t.Fatalf("expected plan/gate/approval/verification invalidated, plan=%#v gate=%#v approval=%#v verification=%#v", state.PlanState, state.PlanGateState, state.PlanApprovalState, state.PlanVerification)
	}
	if state.ExecutionStepCount != 0 || len(state.ExecutionExecutedSteps) != 0 || len(state.ExecutionFindings) != 0 || len(state.ExecutionIssues) != 0 {
		t.Fatalf("expected execution traces/findings cleared, count=%d traces=%#v findings=%#v issues=%#v", state.ExecutionStepCount, state.ExecutionExecutedSteps, state.ExecutionFindings, state.ExecutionIssues)
	}
	if state.ReplanState == nil || state.ReplanState.PlanID != oldPlanID || state.ReplanState.PlanRevision != oldRevision {
		t.Fatalf("replan state should preserve invalidated plan reference, got %#v", state.ReplanState)
	}
}

func testGeneratedPlan(expected, command string, args []string) *GeneratedExecutionPlan {
	return &GeneratedExecutionPlan{
		PlanID:        "plan_001",
		Description:   "inspect pod health",
		RiskLevel:     "low",
		TotalSteps:    1,
		EstimatedTime: 30,
		Steps: []GeneratedExecutionStep{{
			StepID:         1,
			Description:    "check pod status",
			Command:        command,
			Args:           append([]string(nil), args...),
			ExpectedResult: expected,
			Timeout:        30,
		}},
	}
}
