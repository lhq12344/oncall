package commands

import (
	"context"
	"fmt"

	"go_agent/internal/commands/slash"
)

type Dispatcher struct {
	registry *Registry
}

func NewDispatcher(registry *Registry) *Dispatcher {
	if registry == nil {
		registry = NewRegistry(nil)
	}
	return &Dispatcher{registry: registry}
}

func (d *Dispatcher) Dispatch(ctx context.Context, input string, slashContext *slash.Context) (CommandResult, bool, error) {
	parsed, ok := Parse(input)
	if !ok {
		return CommandResult{}, false, nil
	}
	cmd, ok := d.registry.Slash().Find(parsed.Name)
	if !ok {
		return CommandResult{}, true, fmt.Errorf("unknown command /%s", parsed.Name)
	}
	if slashContext == nil {
		slashContext = &slash.Context{}
	}
	slashContext.Ctx = ctx
	slashContext.Args = parsed.Args
	slashContext.Registry = d.registry.Slash()
	result, err := cmd.Handler(slashContext)
	if err != nil {
		return CommandResult{}, true, err
	}
	return CommandResult{Type: fromSlashType(result.Type), Content: result.Content, Prompt: result.Prompt, Action: result.Action, Payload: result.Payload, Metadata: result.Metadata}, true, nil
}
