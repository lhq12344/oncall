package bootstrap

import (
	"context"
	"errors"
	appconfig "go_agent/internal/config"
	"reflect"
	"strings"
	"testing"
	"time"
)

var errTestLayerFailure = errors.New("test layer failure")

func TestLayerRegistryBuildsInRegistrationOrder(t *testing.T) {
	t.Parallel()

	registry := NewLayerRegistry()
	var got []string
	registry.Register("infrastructure", func(ctx context.Context, assembly *Assembly) error {
		got = append(got, "infrastructure")
		return nil
	})
	registry.Register("state", func(ctx context.Context, assembly *Assembly) error {
		got = append(got, "state")
		return nil
	})
	registry.Register("agents", func(ctx context.Context, assembly *Assembly) error {
		got = append(got, "agents")
		return nil
	})

	if err := registry.Build(context.Background(), &Assembly{}); err != nil {
		t.Fatalf("Build returned error: %v", err)
	}

	want := []string{"infrastructure", "state", "agents"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("build order=%v, want %v", got, want)
	}
}

func TestLayerRegistryWrapsStepErrorWithLayerName(t *testing.T) {
	t.Parallel()

	registry := NewLayerRegistry()
	registry.Register("agents", func(ctx context.Context, assembly *Assembly) error {
		return errTestLayerFailure
	})

	err := registry.Build(context.Background(), &Assembly{})
	if err == nil {
		t.Fatal("Build returned nil error")
	}
	if got := err.Error(); got != "build layer agents: test layer failure" {
		t.Fatalf("error=%q", got)
	}
}

func TestBuildRuntimeLayerCreatesControllerRuntimeWithoutRedis(t *testing.T) {
	t.Parallel()

	assembly := &Assembly{
		Config: &Config{},
		App:    &Application{},
		Infra:  &Infrastructure{},
		Agents: &AgentLayer{},
	}

	if err := buildRuntimeLayer(context.Background(), assembly); err != nil {
		t.Fatalf("buildRuntimeLayer returned error: %v", err)
	}
	if assembly.Runtime == nil {
		t.Fatal("Runtime layer is nil")
	}
	if assembly.Runtime.CheckPointStore == nil {
		t.Fatal("checkpoint store is nil")
	}
	if assembly.Runtime.SessionMemory == nil {
		t.Fatal("session memory is nil")
	}
	if assembly.Runtime.SlashRegistry == nil {
		t.Fatal("slash registry is nil")
	}
	if assembly.Runtime.WorkDir == "" {
		t.Fatal("work dir is empty")
	}
	if assembly.Runtime.ToolPolicy == nil {
		t.Fatal("shared tool policy is nil")
	}
	if assembly.App.Runtime != assembly.Runtime {
		t.Fatal("application runtime was not wired to assembled runtime")
	}
}

func TestBuildStateLayerCreatesContextManagerWithoutRedis(t *testing.T) {
	t.Parallel()

	assembly := &Assembly{
		Config: &Config{},
		App:    &Application{},
		Infra:  &Infrastructure{},
	}

	if err := buildStateLayer(context.Background(), assembly); err != nil {
		t.Fatalf("buildStateLayer returned error: %v", err)
	}
	if assembly.State == nil || assembly.State.ContextManager == nil {
		t.Fatal("context manager was not built")
	}
	if assembly.App.ContextManager != assembly.State.ContextManager {
		t.Fatal("application context manager was not wired")
	}
}

func TestBuildInfrastructureLayerDegradesWhenRedisUnavailable(t *testing.T) {
	t.Parallel()

	assembly := &Assembly{
		Config: &Config{
			RedisAddr:        "127.0.0.1:1",
			RedisDialTimeout: time.Millisecond,
		},
		App: &Application{},
	}

	err := buildInfrastructureLayer(context.Background(), assembly)
	if err != nil && strings.Contains(err.Error(), "failed to connect to redis") {
		t.Fatalf("Redis outage should degrade infrastructure, got: %v", err)
	}
}

func TestApplicationCloseAllowsPartialAssembly(t *testing.T) {
	t.Parallel()

	if err := (&Application{}).Close(); err != nil {
		t.Fatalf("Close returned error for partial application: %v", err)
	}
}

func TestBootstrapConfigNormalizeUsesTypedConfig(t *testing.T) {
	t.Parallel()

	cfg := &Config{Typed: appconfig.Default(), LogLevel: "debug", RedisAddr: "127.0.0.1:6379", RedisDB: 3, RedisDialTimeout: 250 * time.Millisecond}
	typed, err := cfg.Normalize()
	if err != nil {
		t.Fatalf("Normalize returned error: %v", err)
	}
	if typed.Runtime.LogLevel != "debug" || typed.Storage.Redis.Addr != "127.0.0.1:6379" || typed.Storage.Redis.DB != 3 || typed.Storage.Redis.DialTimeout != 250*time.Millisecond {
		t.Fatalf("unexpected typed config: %+v", typed)
	}
}

func TestBootstrapConfigNormalizeRejectsInvalidTypedConfig(t *testing.T) {
	t.Parallel()

	cfg := &Config{Typed: appconfig.Default(), LogLevel: "verbose"}
	if _, err := cfg.Normalize(); err == nil || !strings.Contains(err.Error(), "unsupported log level") {
		t.Fatalf("expected log-level validation error, got %v", err)
	}
}
