package agentteams

import (
	"context"
	"fmt"

	"github.com/cloudwego/eino/adk"
)

// Build validates and compiles a Team into the Eino ADK workflow runtime.
func Build(ctx context.Context, team *Team) (adk.ResumableAgent, error) {
	if err := validateTeam(team); err != nil {
		return nil, err
	}

	stageAgents := make([]adk.Agent, 0, len(team.Stages))
	for _, stage := range team.Stages {
		agent, err := buildStage(ctx, team, stage)
		if err != nil {
			return nil, err
		}
		stageAgents = append(stageAgents, agent)
	}

	return adk.NewSequentialAgent(ctx, &adk.SequentialAgentConfig{
		Name:        team.Name,
		Description: team.Description,
		SubAgents:   stageAgents,
	})
}

func validateTeam(team *Team) error {
	if team == nil {
		return fmt.Errorf("team is nil")
	}
	if team.Name == "" {
		return fmt.Errorf("team name is required")
	}
	if len(team.Members) == 0 {
		return fmt.Errorf("team %q must have at least one member", team.Name)
	}
	if len(team.Stages) == 0 {
		return fmt.Errorf("team %q must have at least one stage", team.Name)
	}
	for _, stage := range team.Stages {
		if stage.Name == "" {
			return fmt.Errorf("team %q contains unnamed stage", team.Name)
		}
		if len(stage.Members) == 0 {
			return fmt.Errorf("stage %q must reference at least one member", stage.Name)
		}
		for _, memberName := range stage.Members {
			if _, ok := team.Members[memberName]; !ok {
				return fmt.Errorf("stage %q references unknown member %q", stage.Name, memberName)
			}
		}
		switch stage.Kind {
		case StageSequential, StageLoop:
		default:
			return fmt.Errorf("stage %q has unsupported kind %q", stage.Name, stage.Kind)
		}
	}
	return nil
}

func buildStage(ctx context.Context, team *Team, stage Stage) (adk.Agent, error) {
	subAgents := make([]adk.Agent, 0, len(stage.Members))
	for _, memberName := range stage.Members {
		subAgents = append(subAgents, team.Members[memberName].Agent)
	}

	switch stage.Kind {
	case StageSequential:
		if len(subAgents) == 1 {
			return subAgents[0], nil
		}
		return adk.NewSequentialAgent(ctx, &adk.SequentialAgentConfig{
			Name:        stage.Name,
			Description: stage.Description,
			SubAgents:   subAgents,
		})
	case StageLoop:
		maxIterations := stage.MaxIterations
		if maxIterations <= 0 {
			maxIterations = DefaultLoopMaxIterations
		}
		return adk.NewLoopAgent(ctx, &adk.LoopAgentConfig{
			Name:          stage.Name,
			Description:   stage.Description,
			SubAgents:     subAgents,
			MaxIterations: maxIterations,
		})
	default:
		return nil, fmt.Errorf("stage %q has unsupported kind %q", stage.Name, stage.Kind)
	}
}
