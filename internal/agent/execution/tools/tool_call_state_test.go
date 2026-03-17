package tools

import "testing"

func TestNormalizeExecutionFailureKey_RemovesNoise(t *testing.T) {
	left := normalizeExecutionFailureKey("Step 3 failed at 2026-03-16 10:00:00 plan_001 tooluse_abcd invalid arguments: command is required")
	right := normalizeExecutionFailureKey("step 9 failed at 2026-03-16 10:05:00 plan_009 tooluse_zzzz invalid arguments: command is required")

	if left == "" || right == "" {
		t.Fatalf("normalized keys should not be empty: left=%q right=%q", left, right)
	}
	if left != right {
		t.Fatalf("expected normalized keys to match, got left=%q right=%q", left, right)
	}
}

func TestRecordExecutionToolFailureLocked_ReachesManualRequiredLimit(t *testing.T) {
	state := &executionToolState{}

	var decision repeatedExecutionFailureDecision
	for i := 0; i < maxRepeatedExecutionToolFailures; i++ {
		decision = recordExecutionToolFailureLocked(state, "execute_step", "step 3 failed at 2026-03-16 10:00:00: invalid arguments: command is required")
	}

	if !decision.Reached {
		t.Fatalf("expected repeated failure limit to be reached")
	}
	if !state.stopExecution {
		t.Fatalf("expected stopExecution to be set")
	}
	if state.stopAction != "manual_required" {
		t.Fatalf("expected stopAction manual_required, got %q", state.stopAction)
	}
	if state.repeatedFailureCount != maxRepeatedExecutionToolFailures {
		t.Fatalf("expected repeatedFailureCount=%d, got %d", maxRepeatedExecutionToolFailures, state.repeatedFailureCount)
	}
}

func TestRecordExecutionToolFailureLocked_ResetsForDifferentIssue(t *testing.T) {
	state := &executionToolState{}

	first := recordExecutionToolFailureLocked(state, "generate_plan", "invalid arguments: intent or proposal is required")
	second := recordExecutionToolFailureLocked(state, "validate_plan", "invalid arguments: missing plan, call validate_plan with {\"plan\": <ExecutionPlan>}")

	if first.Count != 1 {
		t.Fatalf("expected first failure count 1, got %d", first.Count)
	}
	if second.Count != 1 {
		t.Fatalf("expected second failure count to reset to 1, got %d", second.Count)
	}
	if second.Key == first.Key {
		t.Fatalf("expected different normalized keys for different failure classes")
	}
}

func TestExecutionToolState_GobRoundTripPreservesRepeatedFailureState(t *testing.T) {
	original := &executionToolState{
		planPrepared:          true,
		planID:                "plan_001",
		stopExecution:         true,
		stopAction:            "manual_required",
		stopReason:            "execution_agent 内部连续 3 次遇到同类失败",
		repeatedFailureKey:    "invalid arguments: command is required",
		repeatedFailureReason: "invalid arguments: command is required",
		repeatedFailureCount:  3,
		repeatedFailureLimit:  maxRepeatedExecutionToolFailures,
	}

	data, err := original.GobEncode()
	if err != nil {
		t.Fatalf("GobEncode returned error: %v", err)
	}

	var decoded executionToolState
	if err := decoded.GobDecode(data); err != nil {
		t.Fatalf("GobDecode returned error: %v", err)
	}

	if !decoded.planPrepared {
		t.Fatalf("expected planPrepared to survive gob round trip")
	}
	if decoded.planID != original.planID {
		t.Fatalf("expected planID=%q, got %q", original.planID, decoded.planID)
	}
	if !decoded.stopExecution {
		t.Fatalf("expected stopExecution to survive gob round trip")
	}
	if decoded.stopAction != original.stopAction {
		t.Fatalf("expected stopAction=%q, got %q", original.stopAction, decoded.stopAction)
	}
	if decoded.repeatedFailureKey != original.repeatedFailureKey {
		t.Fatalf("expected repeatedFailureKey=%q, got %q", original.repeatedFailureKey, decoded.repeatedFailureKey)
	}
	if decoded.repeatedFailureCount != original.repeatedFailureCount {
		t.Fatalf("expected repeatedFailureCount=%d, got %d", original.repeatedFailureCount, decoded.repeatedFailureCount)
	}
	if decoded.repeatedFailureLimit != original.repeatedFailureLimit {
		t.Fatalf("expected repeatedFailureLimit=%d, got %d", original.repeatedFailureLimit, decoded.repeatedFailureLimit)
	}
}
