package ops

import (
	"context"
	"strings"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type incidentTeamTestAgent struct {
	name string
}

func (a incidentTeamTestAgent) Name(context.Context) string {
	return a.name
}

func (a incidentTeamTestAgent) Description(context.Context) string {
	return "test agent " + a.name
}

func (a incidentTeamTestAgent) Run(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()
	}()
	return iterator
}

func TestNewIncidentWorkflowTeamDefaultShape(t *testing.T) {
	t.Parallel()

	team, maxLoops, err := newIncidentWorkflowTeam(0, completeIncidentWorkflowTestMembers())
	if err != nil {
		t.Fatalf("newIncidentWorkflowTeam: %v", err)
	}

	if maxLoops != incidentDefaultMaxExecutionLoops {
		t.Fatalf("maxLoops = %d, want %d", maxLoops, incidentDefaultMaxExecutionLoops)
	}
	if team.Name != "incident_workflow_agent" {
		t.Fatalf("team name = %q", team.Name)
	}
	if len(team.Members) != 9 {
		t.Fatalf("member count = %d, want 9", len(team.Members))
	}

	wantStages := []struct {
		name    string
		members []string
	}{
		{name: "incident_response_loop", members: []string{"incident_analysis", "diagnosis_gate", "plan", "plan_gate", "plan_approval", "execute_plan", "verify_plan", "replan_decider"}},
		{name: "incident_final_report_stage", members: []string{"final_report"}},
	}
	if len(team.Stages) != len(wantStages) {
		t.Fatalf("stage count = %d, want %d", len(team.Stages), len(wantStages))
	}
	for idx, want := range wantStages {
		got := team.Stages[idx]
		if got.Name != want.name {
			t.Fatalf("stage[%d] name = %q, want %q", idx, got.Name, want.name)
		}
		if len(got.Members) != len(want.members) {
			t.Fatalf("stage[%d] members = %v, want %v", idx, got.Members, want.members)
		}
		for memberIdx, wantMember := range want.members {
			if got.Members[memberIdx] != wantMember {
				t.Fatalf("stage[%d] member[%d] = %q, want %q", idx, memberIdx, got.Members[memberIdx], wantMember)
			}
		}
	}
	if team.Stages[0].MaxIterations != maxLoops {
		t.Fatalf("response loop max = %d, want %d", team.Stages[0].MaxIterations, maxLoops)
	}
}

func TestNewIncidentWorkflowTeamHonorsConfiguredLoopCount(t *testing.T) {
	t.Parallel()

	team, maxLoops, err := newIncidentWorkflowTeam(5, completeIncidentWorkflowTestMembers())
	if err != nil {
		t.Fatalf("newIncidentWorkflowTeam: %v", err)
	}
	if maxLoops != 5 {
		t.Fatalf("maxLoops = %d, want 5", maxLoops)
	}
	if team.Stages[0].MaxIterations != 5 {
		t.Fatalf("response loop max = %d, want 5", team.Stages[0].MaxIterations)
	}
}

func TestNewIncidentWorkflowTeamBuildsResumableAgent(t *testing.T) {
	t.Parallel()

	team, _, err := newIncidentWorkflowTeam(1, completeIncidentWorkflowTestMembers())
	if err != nil {
		t.Fatalf("newIncidentWorkflowTeam: %v", err)
	}
	agent, err := team.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if _, ok := any(agent).(adk.ResumableAgent); !ok {
		t.Fatal("built workflow does not satisfy adk.ResumableAgent")
	}
	if got := agent.Name(context.Background()); got != "incident_workflow_agent" {
		t.Fatalf("agent name = %q, want incident_workflow_agent", got)
	}
}

func TestNewIncidentWorkflowTeamRejectsMissingMembers(t *testing.T) {
	t.Parallel()

	members := completeIncidentWorkflowTestMembers()
	members.executePlan = nil
	_, _, err := newIncidentWorkflowTeam(1, members)
	if err == nil {
		t.Fatal("expected error for missing execute_plan member")
	}
}

func completeIncidentWorkflowTestMembers() incidentWorkflowMembers {
	return incidentWorkflowMembers{
		incident:      incidentTeamTestAgent{name: "ops_incident_agent"},
		diagnosisGate: incidentTeamTestAgent{name: "diagnosis_gate"},
		plan:          incidentTeamTestAgent{name: "plan_agent"},
		planGate:      incidentTeamTestAgent{name: "plan_gate"},
		planApproval:  incidentTeamTestAgent{name: "plan_approval"},
		executePlan:   incidentTeamTestAgent{name: "execution_agent"},
		verifyPlan:    incidentTeamTestAgent{name: "verify_plan"},
		gate:          incidentTeamTestAgent{name: "replan_decider"},
		reporter:      incidentTeamTestAgent{name: "final_report"},
	}
}

func TestStateBridgeIncidentStageCapturesRCAAndProposal(t *testing.T) {
	t.Parallel()

	state := &IncidentState{}
	msg := &schema.Message{Content: `{
		"root_cause": "pod_restart",
		"target_node": "infra/api-0",
		"path": "infra/api",
		"impact": "api unavailable",
		"confidence": 0.72,
		"evidence": ["k8s restart count > 0", "error logs matched"],
		"next_verification": ["confirm pod status"],
		"remediation_intent": "restore api pod health without unsafe changes",
		"planning_constraints": ["read-only first"],
		"fallback_guidance": "manual pod event inspection",
		"proposal_id": "proposal_1",
		"summary": "verify rollout and inspect logs",
		"risk_level": "medium",
		"actions": [
			{
				"step": 1,
				"goal": "确认 pod 当前状态",
				"rationale": "先只读验证现场",
				"command_hint": "kubectl get pod api-0 -n infra",
				"success_criteria": "pod Running",
				"rollback_hint": "无需回滚",
				"read_only": true
			}
		],
		"fallback_plan": "人工检查 pod 事件"
	}`}

	bridge := &stateBridgeAgent{stage: "incident"}
	bridge.updateByStage(state, msg)

	if state.RootCause != "pod_restart" || state.TargetNode != "infra/api-0" {
		t.Fatalf("RCA fields not captured: %#v", state)
	}
	if state.RemediationIntent != "restore api pod health without unsafe changes" || len(state.PlanningConstraints) != 1 {
		t.Fatalf("diagnosis planning intent not captured: %#v", state)
	}
	if state.RemediationProposalID != "proposal_1" || state.PlanID != "proposal_1" {
		t.Fatalf("proposal fields not captured: %#v", state)
	}
	if len(state.RemediationProposalActions) != 1 {
		t.Fatalf("proposal actions = %v, want one action", state.RemediationProposalActions)
	}
}

func TestStateBridgePlanStageCapturesCanonicalPlanState(t *testing.T) {
	t.Parallel()

	state := &IncidentState{
		PlanApprovalState: &PlanApprovalState{
			PlanID:       "plan_old",
			Revision:     1,
			SnapshotHash: "old_hash",
			Approved:     true,
			ApprovedAt:   "2026-08-16T00:00:00Z",
		},
	}
	planJSON := "{\"plan_id\":\"plan_001\",\"description\":\"inspect pod health\",\"risk_level\":\"low\",\"total_steps\":1,\"estimated_time\":5,\"steps\":[{\"step_id\":1,\"description\":\"check pod status\",\"command\":\"kubectl\",\"args\":[\"get\",\"pod\",\"api-0\",\"-n\",\"infra\"],\"expected_result\":\"pod Running\",\"timeout\":15}]}"
	msg := &schema.Message{Content: planJSON}

	bridge := &stateBridgeAgent{stage: "plan"}
	bridge.updateByStage(state, msg)

	if state.PlanState == nil {
		t.Fatal("expected canonical PlanState to be captured")
	}
	if state.PlanState.PlanID != "plan_001" || state.ExecutionPlanID != "plan_001" {
		t.Fatalf("plan id not mirrored: plan_state=%#v execution_plan_id=%q", state.PlanState, state.ExecutionPlanID)
	}
	if state.PlanState.Revision != 1 {
		t.Fatalf("initial revision = %d, want 1", state.PlanState.Revision)
	}
	if state.PlanState.SnapshotHash == "" {
		t.Fatal("expected plan snapshot hash")
	}
	if len(state.PlanState.StepSummaries) != 1 || len(state.ExecutionPlanSteps) != 1 {
		t.Fatalf("step summaries not captured: plan=%v legacy=%v", state.PlanState.StepSummaries, state.ExecutionPlanSteps)
	}
	if state.PlanApprovalState == nil || state.PlanApprovalState.Approved {
		t.Fatalf("expected changed plan to invalidate prior approval: %#v", state.PlanApprovalState)
	}

	firstHash := state.PlanState.SnapshotHash
	bridge.updateByStage(state, msg)
	if state.PlanState.Revision != 1 || state.PlanState.SnapshotHash != firstHash {
		t.Fatalf("same plan should keep revision/hash, got revision=%d hash=%q", state.PlanState.Revision, state.PlanState.SnapshotHash)
	}

	changedMsg := &schema.Message{Content: strings.Replace(planJSON, "pod Running", "pod Ready", 1)}
	bridge.updateByStage(state, changedMsg)
	if state.PlanState.Revision != 2 {
		t.Fatalf("changed plan revision = %d, want 2", state.PlanState.Revision)
	}
	if state.PlanState.SnapshotHash == firstHash {
		t.Fatal("changed plan should produce a new snapshot hash")
	}
	if rendered := renderIncidentState(state); !strings.Contains(rendered, "plan_state") || !strings.Contains(rendered, "snapshot_hash") {
		t.Fatalf("rendered state missing canonical plan summary: %s", rendered)
	}
}

func TestReplanDecisionStateReferencesCanonicalPlan(t *testing.T) {
	t.Parallel()

	state := &IncidentState{}
	applyExecutionPlanState(state, &GeneratedExecutionPlan{
		PlanID:      "plan_001",
		Description: "inspect pod health",
		RiskLevel:   "low",
		Steps: []GeneratedExecutionStep{{
			StepID:         1,
			Description:    "check pod status",
			Command:        "kubectl",
			Args:           []string{"get", "pod", "api-0", "-n", "infra"},
			ExpectedResult: "pod Running",
		}},
	})

	applyReplanDecisionState(state, "refresh_observation", "pod is already healthy", "validate_result", "api-0 is Running and Ready")

	if state.ReplanState == nil {
		t.Fatal("expected ReplanState")
	}
	if state.ReplanState.Decision != "refresh_observation" {
		t.Fatalf("decision = %q, want refresh_observation", state.ReplanState.Decision)
	}
	if state.ReplanState.PlanID != "plan_001" || state.ReplanState.PlanRevision != 1 {
		t.Fatalf("replan should reference canonical plan, got %#v", state.ReplanState)
	}
	if !state.ObservationRefreshNeeded || state.ExecutionStatus != "replan_required" {
		t.Fatalf("refresh flags not set: status=%q refresh=%v", state.ExecutionStatus, state.ObservationRefreshNeeded)
	}
	if !strings.Contains(state.RuntimeObservationSummary, "Running") {
		t.Fatalf("runtime summary not captured: %q", state.RuntimeObservationSummary)
	}
	if rendered := renderIncidentState(state); !strings.Contains(rendered, "replan_state") || !strings.Contains(rendered, "refresh_observation") {
		t.Fatalf("rendered state missing replan decision: %s", rendered)
	}
}

func TestVerifyPlanPayloadRequiresExecutionTrace(t *testing.T) {
	t.Parallel()

	state := &IncidentState{ExecutionStatus: "success", ExecutionSuccess: true}
	applyExecutionPlanState(state, &GeneratedExecutionPlan{
		PlanID:      "plan_001",
		Description: "inspect pod health",
		RiskLevel:   "low",
		Steps: []GeneratedExecutionStep{{
			StepID:         7,
			Description:    "check pod status",
			Command:        "kubectl",
			Args:           []string{"get", "pod", "api-0", "-n", "infra"},
			ExpectedResult: "pod Running",
		}},
	})

	payload := buildPlanVerificationPayload(state)
	if payload.Success || payload.VerificationStatus != "failed" || payload.ExecutionStatus != "failed" {
		t.Fatalf("expected missing trace to fail verification, got %#v", payload)
	}
	if payload.FailedStepID != 7 {
		t.Fatalf("failed step = %d, want 7", payload.FailedStepID)
	}

	state.ExecutionStepCount = 1
	payload = buildPlanVerificationPayload(state)
	if !payload.Success || payload.VerificationStatus != "success" || payload.ExecutionStatus != "success" {
		t.Fatalf("expected traced success to pass verification, got %#v", payload)
	}
	if len(payload.ExecutedSteps) != 1 || payload.ExecutedSteps[0]["step_id"] != 7 {
		t.Fatalf("executed step trace not emitted: %#v", payload.ExecutedSteps)
	}
}

func TestStateBridgeExecutionStageCannotOverwriteCanonicalPlan(t *testing.T) {
	t.Parallel()

	state := &IncidentState{}
	applyExecutionPlanState(state, &GeneratedExecutionPlan{
		PlanID:      "plan_001",
		Description: "approved plan",
		RiskLevel:   "low",
		Steps: []GeneratedExecutionStep{{
			StepID:         1,
			Description:    "check pod status",
			Command:        "kubectl",
			Args:           []string{"get", "pod", "api-0", "-n", "infra"},
			ExpectedResult: "pod Running",
		}},
	})
	approvedHash := state.PlanState.SnapshotHash

	planJSON := "{\"plan_id\":\"plan_002\",\"description\":\"new execute_plan stage plan\",\"risk_level\":\"low\",\"total_steps\":1,\"steps\":[{\"step_id\":1,\"description\":\"changed\",\"command\":\"echo\",\"args\":[\"bad\"],\"expected_result\":\"bad\"}]}"
	msg := &schema.Message{Content: planJSON}
	bridge := &stateBridgeAgent{stage: "execute_plan"}
	bridge.updateByStage(state, msg)

	if state.PlanState == nil || state.PlanState.PlanID != "plan_001" || state.PlanState.SnapshotHash != approvedHash {
		t.Fatalf("execute_plan stage overwrote canonical plan: %#v", state.PlanState)
	}
	if state.ReplanState != nil {
		t.Fatalf("execute_plan bridge should not write ReplanState, got %#v", state.ReplanState)
	}
	if state.ExecutionStatus != "manual_required" || !strings.Contains(state.ExecutionReason, "execute_plan stage attempted") {
		t.Fatalf("expected bridge to record boundary fact, status=%q reason=%q", state.ExecutionStatus, state.ExecutionReason)
	}
}
