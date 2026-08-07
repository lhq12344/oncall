package ops

import (
	"context"
	"testing"

	"github.com/cloudwego/eino/adk"
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
	if len(team.Members) != 7 {
		t.Fatalf("member count = %d, want 7", len(team.Members))
	}

	wantStages := []struct {
		name    string
		members []string
	}{
		{name: "incident_observation_stage", members: []string{"observation"}},
		{name: "incident_rca_stage", members: []string{"rca"}},
		{name: "incident_execute_loop", members: []string{"ops", "execution", "gate"}},
		{name: "incident_strategy_stage", members: []string{"strategy"}},
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
	if team.Stages[2].MaxIterations != maxLoops {
		t.Fatalf("execute loop max = %d, want %d", team.Stages[2].MaxIterations, maxLoops)
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
	if team.Stages[2].MaxIterations != 5 {
		t.Fatalf("execute loop max = %d, want 5", team.Stages[2].MaxIterations)
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
		observation: incidentTeamTestAgent{name: "observation_collector"},
		rca:         incidentTeamTestAgent{name: "rca_agent"},
		ops:         incidentTeamTestAgent{name: "ops_agent"},
		execution:   incidentTeamTestAgent{name: "execution_agent"},
		gate:        incidentTeamTestAgent{name: "execution_gate"},
		strategy:    incidentTeamTestAgent{name: "strategy_agent"},
		reporter:    incidentTeamTestAgent{name: "final_report"},
	}
}
