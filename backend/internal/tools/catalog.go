package toolregistry

import (
	"context"
	"fmt"
)

type ToolQuery struct {
	Agent      AgentKind
	Capability Capability
	Exposure   ToolExposure
}

func (r *Registry) Resolve(ctx context.Context, query ToolQuery) ([]RegisteredTool, error) {
	if query.Agent != "" && !isKnownAgent(query.Agent) {
		return nil, fmt.Errorf("unknown agent kind %q", query.Agent)
	}
	_ = ctx
	out := make([]RegisteredTool, 0, len(r.entries))
	for _, entry := range r.entries {
		descriptor := descriptorForRegistration(entry)
		if query.Agent != "" {
			if _, ok := entry.agents[query.Agent]; !ok {
				continue
			}
		}
		if query.Capability != "" && descriptor.Capability != query.Capability {
			continue
		}
		if query.Exposure != "" && descriptor.Exposure != query.Exposure {
			continue
		}
		out = append(out, RegisteredTool{Descriptor: descriptor, Factory: ToolFactory(entry.build)})
	}
	return out, nil
}

func descriptorForRegistration(entry registration) ToolDescriptor {
	descriptor := DefaultDescriptor(ToolID(entry.name))
	descriptor.Capability = capabilityForTool(entry.name)
	descriptor.Risk = riskForTool(entry.name)
	return descriptor
}
