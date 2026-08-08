package ops

import (
	"context"
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
	if len(team.Members) != 5 {
		t.Fatalf("member count = %d, want 5", len(team.Members))
	}

	wantStages := []struct {
		name    string
		members []string
	}{
		{name: "incident_response_loop", members: []string{"incident", "incident_contract_gate", "execution", "gate"}},
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
	members.execution = nil
	_, _, err := newIncidentWorkflowTeam(1, members)
	if err == nil {
		t.Fatal("expected error for missing execution member")
	}
}

func completeIncidentWorkflowTestMembers() incidentWorkflowMembers {
	return incidentWorkflowMembers{
		incident:     incidentTeamTestAgent{name: "ops_incident_agent"},
		contractGate: incidentTeamTestAgent{name: "incident_contract_gate"},
		execution:    incidentTeamTestAgent{name: "execution_agent"},
		gate:         incidentTeamTestAgent{name: "execution_gate"},
		reporter:     incidentTeamTestAgent{name: "final_report"},
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
	if state.RemediationProposalID != "proposal_1" || state.PlanID != "proposal_1" {
		t.Fatalf("proposal fields not captured: %#v", state)
	}
	if len(state.RemediationProposalActions) != 1 {
		t.Fatalf("proposal actions = %v, want one action", state.RemediationProposalActions)
	}
}
