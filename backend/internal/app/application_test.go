package app

import (
	"context"
	"testing"

	"go_agent/internal/config"
	"go_agent/internal/events"
	"go_agent/internal/model"
	"go_agent/internal/telemetry"
)

func TestNewApplicationBuildsRuntimeModules(t *testing.T) {
	eventSink := &events.MemorySink{}
	app, err := New(context.Background(), config.Default(), telemetry.NewMemorySink(), eventSink)
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if _, err := app.Models.RequireCapability(context.Background(), model.RoleDialogue, "streaming"); err != nil {
		t.Fatalf("dialogue model capability: %v", err)
	}
	if !app.Capability.RedisOptional || !app.Capability.ElasticsearchOptional || !app.Capability.MilvusOptional || !app.Capability.TraceOptional {
		t.Fatalf("expected optional dependency capabilities, got %+v", app.Capability)
	}
	run, err := app.NewRun(context.Background(), "run-1", "trace-1")
	if err != nil {
		t.Fatalf("NewRun: %v", err)
	}
	if run.RunID != "run-1" || len(eventSink.Events()) != 1 || eventSink.Events()[0].TraceID != "trace-1" {
		t.Fatalf("unexpected run/events: %+v %+v", run, eventSink.Events())
	}
}

func TestApplicationCapabilitiesReflectRequiredDependencies(t *testing.T) {
	cfg := config.Default()
	cfg.Storage.Redis.Required = true
	cfg.Storage.Redis.Addr = "127.0.0.1:6379"
	cfg.Storage.Elasticsearch.Required = true
	cfg.Storage.Elasticsearch.Addresses = []string{"http://127.0.0.1:9200"}
	cfg.Storage.Milvus.Required = true
	cfg.Storage.Milvus.Address = "127.0.0.1:19530"
	cfg.Observability.Trace.Required = true
	cfg.Observability.Trace.Exporter = "otlp"

	app, err := New(context.Background(), cfg, telemetry.NewMemorySink(), &events.MemorySink{})
	if err != nil {
		t.Fatalf("New: %v", err)
	}
	if app.Capability.RedisOptional || app.Capability.ElasticsearchOptional || app.Capability.MilvusOptional || app.Capability.TraceOptional {
		t.Fatalf("required dependencies should not be advertised as optional: %+v", app.Capability)
	}
}
