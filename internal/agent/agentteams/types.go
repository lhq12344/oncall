package agentteams

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino/adk"
)

// DefaultLoopMaxIterations matches the existing OnCall incident execution loop
// default. Keeping the value here makes team definitions deterministic when a
// caller leaves a loop stage unset.
const DefaultLoopMaxIterations = 3

// StageKind describes how a team stage runs its member agents.
type StageKind string

const (
	StageSequential StageKind = "sequential"
	StageLoop       StageKind = "loop"
)

// Member is a named agent in an OnCall team.
type Member struct {
	Name        string
	Description string
	Agent       adk.Agent
}

// Stage is one executable step in a team plan. Sequential stages run members
// once in order. Loop stages run the listed members repeatedly until the ADK
// loop exits or MaxIterations is reached.
type Stage struct {
	Name          string
	Description   string
	Kind          StageKind
	Members       []string
	MaxIterations int
}

// Team declares a group of named agents and the stages the lead will run.
type Team struct {
	Name        string
	Description string
	Members     map[string]Member
	Stages      []Stage
}

// NewTeam creates a declarative team definition.
func NewTeam(name, description string) *Team {
	return &Team{
		Name:        strings.TrimSpace(name),
		Description: strings.TrimSpace(description),
		Members:     make(map[string]Member),
	}
}

// AddMember registers a named agent. Names must be unique and non-empty.
func (t *Team) AddMember(name, description string, agent adk.Agent) error {
	if t == nil {
		return fmt.Errorf("team is nil")
	}
	name = strings.TrimSpace(name)
	if name == "" {
		return fmt.Errorf("member name is required")
	}
	if agent == nil {
		return fmt.Errorf("member %q agent is required", name)
	}
	if _, exists := t.Members[name]; exists {
		return fmt.Errorf("member %q already exists", name)
	}
	t.Members[name] = Member{Name: name, Description: strings.TrimSpace(description), Agent: agent}
	return nil
}

// AddSequentialStage appends a stage that runs members once in order.
func (t *Team) AddSequentialStage(name, description string, members ...string) error {
	return t.addStage(Stage{
		Name:        name,
		Description: description,
		Kind:        StageSequential,
		Members:     members,
	})
}

// AddLoopStage appends a bounded loop stage.
func (t *Team) AddLoopStage(name, description string, maxIterations int, members ...string) error {
	return t.addStage(Stage{
		Name:          name,
		Description:   description,
		Kind:          StageLoop,
		Members:       members,
		MaxIterations: maxIterations,
	})
}

func (t *Team) addStage(stage Stage) error {
	if t == nil {
		return fmt.Errorf("team is nil")
	}
	stage.Name = strings.TrimSpace(stage.Name)
	stage.Description = strings.TrimSpace(stage.Description)
	if stage.Name == "" {
		return fmt.Errorf("stage name is required")
	}
	if stage.Kind == "" {
		return fmt.Errorf("stage %q kind is required", stage.Name)
	}
	var err error
	stage.Members, err = normalizeMemberNames(stage.Members)
	if err != nil {
		return fmt.Errorf("stage %q %w", stage.Name, err)
	}
	if len(stage.Members) == 0 {
		return fmt.Errorf("stage %q must reference at least one member", stage.Name)
	}
	t.Stages = append(t.Stages, stage)
	return nil
}

func normalizeMemberNames(names []string) ([]string, error) {
	result := make([]string, 0, len(names))
	for _, name := range names {
		name = strings.TrimSpace(name)
		if name == "" {
			return nil, fmt.Errorf("contains blank member reference")
		}
		result = append(result, name)
	}
	return result, nil
}

// Build compiles the team into an ADK resumable workflow.
func (t *Team) Build(ctx context.Context) (adk.ResumableAgent, error) {
	return Build(ctx, t)
}
