package config

import (
	"strings"
	"testing"
	"time"
)

func TestDefaultConfigValidatesOptionalDependencies(t *testing.T) {
	cfg := Default()
	if err := cfg.Validate(); err != nil {
		t.Fatalf("Default().Validate(): %v", err)
	}
	if cfg.Storage.Redis.Required || cfg.Storage.Elasticsearch.Required || cfg.Storage.Milvus.Required || cfg.Observability.Trace.Required {
		t.Fatalf("optional dependencies should default required=false: %+v", cfg)
	}
}

func TestRequiredStorageValidation(t *testing.T) {
	cfg := Default()
	cfg.Storage.Redis.Required = true
	cfg.Storage.Redis.Addr = ""
	if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), "redis address") {
		t.Fatalf("expected redis required validation error, got %v", err)
	}
}

func TestRequiredExternalDependencyValidation(t *testing.T) {
	tests := []struct {
		name    string
		mutate  func(*Config)
		message string
	}{
		{
			name: "elasticsearch",
			mutate: func(cfg *Config) {
				cfg.Storage.Elasticsearch.Required = true
				cfg.Storage.Elasticsearch.Addresses = nil
				cfg.Storage.Elasticsearch.CloudID = ""
			},
			message: "elasticsearch address",
		},
		{
			name: "milvus",
			mutate: func(cfg *Config) {
				cfg.Storage.Milvus.Required = true
				cfg.Storage.Milvus.Address = ""
			},
			message: "milvus address",
		},
		{
			name: "trace",
			mutate: func(cfg *Config) {
				cfg.Observability.Trace.Required = true
				cfg.Observability.Trace.Exporter = ""
			},
			message: "trace exporter",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := Default()
			tt.mutate(&cfg)
			if err := cfg.Validate(); err == nil || !strings.Contains(err.Error(), tt.message) {
				t.Fatalf("expected %s validation error, got %v", tt.message, err)
			}
		})
	}
}

func TestLoadEnvNormalizesTypedConfig(t *testing.T) {
	env := map[string]string{
		"ONCALL_LOG_LEVEL":   "debug",
		"REDIS_ADDR":         "127.0.0.1:6379",
		"REDIS_DB":           "2",
		"REDIS_DIAL_TIMEOUT": "250ms",
		"PROMETHEUS_URL":     "http://10.98.198.63:30090",
		"KUBECONFIG":         "C:/Users/lhq/.kube/config",
		"MILVUS_TIMEOUT":     "3s",
	}
	cfg := LoadEnv(func(key string) string { return env[key] })
	if cfg.Runtime.LogLevel != "debug" || cfg.Storage.Redis.DB != 2 || cfg.Storage.Redis.DialTimeout != 250*time.Millisecond || cfg.Storage.Milvus.Timeout != 3*time.Second {
		t.Fatalf("unexpected config: %+v", cfg)
	}
	if cfg.Storage.Redis.Addr != "127.0.0.1:6379" || cfg.Runtime.PrometheusURL != "http://10.98.198.63:30090" || cfg.Runtime.KubeConfig != "C:/Users/lhq/.kube/config" {
		t.Fatalf("unexpected config: %+v", cfg)
	}
}

func TestLoadEnvDetectsConfiguredCozeLoop(t *testing.T) {
	cfg := LoadEnv(func(key string) string {
		switch key {
		case "COZELOOP_API_BASE_URL":
			return "http://127.0.0.1:18082"
		case "COZELOOP_WORKSPACE_ID":
			return "workspace"
		case "COZELOOP_API_TOKEN":
			return "token"
		default:
			return ""
		}
	})
	if cfg.Observability.Trace.Exporter != "cozeloop" {
		t.Fatalf("trace exporter=%q, want cozeloop", cfg.Observability.Trace.Exporter)
	}
	if cfg.Observability.Trace.Endpoint != "http://127.0.0.1:18082" {
		t.Fatalf("trace endpoint=%q", cfg.Observability.Trace.Endpoint)
	}
}
