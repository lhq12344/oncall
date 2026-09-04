package commands

import "go_agent/internal/commands/slash"

type Type string

const (
	TypeLocal        Type = "local"
	TypePrompt       Type = "prompt"
	TypeWorkflow     Type = "workflow"
	TypeResume       Type = "resume"
	TypeClientAction Type = "client_action"
	TypeDeferred     Type = "deferred"
)

type CommandResult struct {
	Type     Type
	Content  string
	Prompt   string
	Action   string
	Payload  map[string]any
	Metadata map[string]any
}

type Registry struct {
	slash *slash.Registry
}

func NewRegistry(slashRegistry *slash.Registry) *Registry {
	if slashRegistry == nil {
		slashRegistry = slash.NewRegistry()
	}
	return &Registry{slash: slashRegistry}
}

func (r *Registry) Slash() *slash.Registry { return r.slash }

func fromSlashType(value slash.CommandType) Type {
	switch value {
	case slash.TypeOpsWorkflow:
		return TypeWorkflow
	case slash.TypeClientAction:
		return TypeClientAction
	case slash.TypeDeferred:
		return TypeDeferred
	case slash.TypePrompt:
		return TypePrompt
	default:
		return TypeLocal
	}
}
