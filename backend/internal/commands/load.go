package commands

import "go_agent/internal/commands/slash"

func LoadDefault(workDir string) *Registry {
	return NewRegistry(slash.CreateDefaultRegistry(workDir))
}
