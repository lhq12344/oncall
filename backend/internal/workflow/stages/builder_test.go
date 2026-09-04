package stages

import (
	"context"
	"strings"
	"sync/atomic"
	"testing"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/schema"
)

type countingAgent struct {
	name  string
	count *int32
}

func (a countingAgent) Name(context.Context) string { return a.name }

func (a countingAgent) Description(context.Context) string { return "test agent " + a.name }

func (a countingAgent) Run(context.Context, *adk.AgentInput, ...adk.AgentRunOption) *adk.AsyncIterator[*adk.AgentEvent] {
	iterator, generator := adk.NewAsyncIteratorPair[*adk.AgentEvent]()
	go func() {
		defer generator.Close()
		atomic.AddInt32(a.count, 1)
		generator.Send(&adk.AgentEvent{
			AgentName: a.name,
			Output: &adk.AgentOutput{MessageOutput: &adk.MessageVariant{
				Message: &schema.Message{Role: schema.Assistant, Content: a.name},
			}},
		})
	}()
	return iterator
}

func TestTeamRejectsDuplicateMembers(t *testing.T) {
	t.Parallel()
	var count int32
	team := NewTeam("demo", "demo team")
	if err := team.AddMember("rca", "", countingAgent{name: "rca", count: &count}); err != nil {
		t.Fatalf("AddMember first: %v", err)
	}
	if err := team.AddMember("rca", "", countingAgent{name: "rca2", count: &count}); err == nil {
		t.Fatal("expected duplicate member error")
	}
}

func TestBuildRejectsUnknownMember(t *testing.T) {
	t.Parallel()
	var count int32
	team := NewTeam("demo", "demo team")
	if err := team.AddMember("rca", "", countingAgent{name: "rca", count: &count}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := team.AddSequentialStage("stage", "", "missing"); err != nil {
		t.Fatalf("AddSequentialStage: %v", err)
	}
	_, err := team.Build(context.Background())
	if err == nil || !strings.Contains(err.Error(), "unknown member") {
		t.Fatalf("Build error = %v, want unknown member", err)
	}
}

func TestSequentialStageRequiresMembers(t *testing.T) {
	t.Parallel()
	team := NewTeam("demo", "demo team")
	if err := team.AddSequentialStage("empty", ""); err == nil {
		t.Fatal("expected empty stage error")
	}
}

func TestStageRejectsBlankMemberName(t *testing.T) {
	t.Parallel()
	var count int32
	team := NewTeam("demo", "demo team")
	if err := team.AddMember("rca", "", countingAgent{name: "rca", count: &count}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := team.AddSequentialStage("stage", "", "rca", "  "); err == nil || !strings.Contains(err.Error(), "blank member") {
		t.Fatalf("AddSequentialStage error = %v, want blank member", err)
	}
}

func TestBuildReturnsResumableWorkflowWithTeamName(t *testing.T) {
	t.Parallel()
	var count int32
	team := NewTeam("incident_workflow_agent", "incident team")
	if err := team.AddMember("observation", "", countingAgent{name: "observation", count: &count}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := team.AddSequentialStage("observe", "", "observation"); err != nil {
		t.Fatalf("AddSequentialStage: %v", err)
	}
	agent, err := team.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	if got := agent.Name(context.Background()); got != "incident_workflow_agent" {
		t.Fatalf("Name = %q", got)
	}
	if _, ok := any(agent).(adk.ResumableAgent); !ok {
		t.Fatal("built workflow does not satisfy adk.ResumableAgent")
	}
}

func TestLoopStageDefaultsToThreeIterations(t *testing.T) {
	t.Parallel()
	var count int32
	team := NewTeam("demo", "demo team")
	if err := team.AddMember("worker", "", countingAgent{name: "worker", count: &count}); err != nil {
		t.Fatalf("AddMember: %v", err)
	}
	if err := team.AddLoopStage("loop", "", 0, "worker"); err != nil {
		t.Fatalf("AddLoopStage: %v", err)
	}
	agent, err := team.Build(context.Background())
	if err != nil {
		t.Fatalf("Build: %v", err)
	}
	iter := agent.Run(context.Background(), &adk.AgentInput{})
	for {
		if _, ok := iter.Next(); !ok {
			break
		}
	}
	if got := atomic.LoadInt32(&count); got != DefaultLoopMaxIterations {
		t.Fatalf("loop executions = %d, want %d", got, DefaultLoopMaxIterations)
	}
}
