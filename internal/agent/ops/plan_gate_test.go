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
	if state.PlanGateState == nil || !state.PlanGateState.Blocked || state.PlanGateState.Valid {
		t.Fatalf("expected unsafe plan to be blocked, got validation=%#v gate=%#v", validation, state.PlanGateState)
	}
	if state.ReplanState == nil || state.ReplanState.Decision != "refresh_observation" {
		t.Fatalf("expected blocked plan to request observation refresh, got %#v", state.ReplanState)
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
