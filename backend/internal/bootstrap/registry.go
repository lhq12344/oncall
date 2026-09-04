package bootstrap

import (
	"context"
	"fmt"
	"strings"
)

// Assembly is the in-progress application graph used only during startup.
// Runtime code should depend on the typed modules exposed by Application, not
// on this assembly object.
type Assembly struct {
	Config     *Config
	App        *Application
	Infra      *Infrastructure
	State      *StateLayer
	Agents     *AgentLayer
	Runtime    *RuntimeLayer
	Background *BackgroundLayer
}

type LayerBuilder func(context.Context, *Assembly) error

type layerStep struct {
	name    string
	builder LayerBuilder
}

// LayerRegistry builds application layers in a deterministic startup order.
type LayerRegistry struct {
	steps []layerStep
}

func NewLayerRegistry() *LayerRegistry {
	return &LayerRegistry{}
}

func (r *LayerRegistry) Register(name string, builder LayerBuilder) {
	if r == nil || builder == nil {
		return
	}
	name = strings.TrimSpace(name)
	if name == "" {
		name = "unnamed"
	}
	r.steps = append(r.steps, layerStep{name: name, builder: builder})
}

func (r *LayerRegistry) Build(ctx context.Context, assembly *Assembly) error {
	if r == nil {
		return nil
	}
	if assembly == nil {
		assembly = &Assembly{}
	}
	for _, step := range r.steps {
		if err := step.builder(ctx, assembly); err != nil {
			return fmt.Errorf("build layer %s: %w", step.name, err)
		}
	}
	return nil
}
