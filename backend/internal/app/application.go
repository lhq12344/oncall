package app

import (
	"context"
	"fmt"

	"go_agent/internal/config"
	"go_agent/internal/events"
	"go_agent/internal/model"
	"go_agent/internal/telemetry"
)

// Application is the runtime-facing module assembled by the composition root.
type Application struct {
	Config     config.Config
	Models     *model.Catalog
	Telemetry  *telemetry.Recorder
	EventSink  events.Sink
	Capability Capabilities
}

type Capabilities struct {
	RedisOptional         bool
	ElasticsearchOptional bool
	MilvusOptional        bool
	TraceOptional         bool
}

func New(ctx context.Context, cfg config.Config, sink telemetry.Sink, eventSink events.Sink) (*Application, error) {
	_ = ctx
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	profiles := make([]model.Profile, 0, len(cfg.Models))
	for _, profile := range cfg.Models {
		profiles = append(profiles, model.Profile{
			ID:              profile.ID,
			Provider:        profile.Provider,
			DisplayName:     profile.ID,
			Model:           profile.Model,
			Role:            firstModelRole(profile.Roles),
			Roles:           convertRoles(profile.Roles),
			ContextWindow:   profile.ContextWindow,
			MaxInputTokens:  profile.ContextWindow,
			MaxOutputTokens: profile.MaxOutputTokens,
			SupportsTools:   profile.Capabilities.Tools,
			SupportsStream:  profile.Capabilities.Streaming,
			Capabilities: model.Capabilities{
				Streaming: profile.Capabilities.Streaming,
				Tools:     profile.Capabilities.Tools,
				Vision:    profile.Capabilities.Vision,
				JSON:      profile.Capabilities.JSON,
				Tokenizer: profile.Capabilities.Tokenizer,
			},
			Timeout:     profile.Timeout,
			RetryPolicy: model.RetryPolicy{MaxAttempts: profile.Retry.MaxAttempts, Backoff: profile.Retry.Backoff},
			CostClass:   profile.CostClass,
			Default:     profile.Default,
		})
	}
	catalog, err := model.NewCatalog(profiles)
	if err != nil {
		return nil, fmt.Errorf("build model catalog: %w", err)
	}
	if sink == nil {
		sink = telemetry.NewMemorySink()
	}
	return &Application{
		Config:    cfg,
		Models:    catalog,
		Telemetry: telemetry.NewRecorder(sink),
		EventSink: eventSink,
		Capability: Capabilities{
			RedisOptional:         !cfg.Storage.Redis.Required,
			ElasticsearchOptional: !cfg.Storage.Elasticsearch.Required,
			MilvusOptional:        !cfg.Storage.Milvus.Required,
			TraceOptional:         !cfg.Observability.Trace.Required,
		},
	}, nil
}

func firstModelRole(roles []config.ModelRole) string {
	if len(roles) == 0 {
		return "default"
	}
	return string(roles[0])
}

func convertRoles(roles []config.ModelRole) []model.Role {
	if len(roles) == 0 {
		return nil
	}
	out := make([]model.Role, len(roles))
	for i, role := range roles {
		out[i] = model.Role(role)
	}
	return out
}
