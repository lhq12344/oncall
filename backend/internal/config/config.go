package config

import (
	"fmt"
	"strings"
	"time"
)

// Config is the validated process configuration consumed by the composition root.
// Raw environment/config-file lookups should be normalized into this shape once at startup.
type Config struct {
	Runtime       Runtime
	Models        []ModelProfile
	Storage       Storage
	Observability Observability
}

type Runtime struct {
	LogLevel           string
	PrometheusURL      string
	KubeConfig         string
	LogSyncEnabled     bool
	LogSyncNamespaces  []string
	LogSyncInterval    time.Duration
	LogSyncTailLines   int64
	LogSyncIndexPrefix string
	HooksConfigPath    string
}

func Default() Config {
	return Config{
		Runtime: Runtime{
			LogLevel:           "info",
			LogSyncNamespaces:  []string{"infra"},
			LogSyncInterval:    30 * time.Second,
			LogSyncTailLines:   200,
			LogSyncIndexPrefix: "logs-k8s",
		},
		Models: []ModelProfile{DefaultChatModel(), DefaultEmbeddingModel()},
		Storage: Storage{
			Redis:         Redis{Required: false, DialTimeout: time.Second},
			Elasticsearch: Elasticsearch{Required: false, Timeout: 10 * time.Second},
			Milvus:        DefaultMilvus(),
		},
		Observability: Observability{Trace: Trace{Required: false, LocalBufferSize: 1024}},
	}
}

func (c Config) Validate() error {
	if err := c.Runtime.Validate(); err != nil {
		return err
	}
	if err := ValidateModels(c.Models); err != nil {
		return err
	}
	if err := c.Storage.Validate(); err != nil {
		return err
	}
	return c.Observability.Validate()
}

func (r Runtime) Validate() error {
	switch strings.TrimSpace(r.LogLevel) {
	case "", "debug", "info", "warn", "error":
	default:
		return fmt.Errorf("unsupported log level %q", r.LogLevel)
	}
	if r.LogSyncEnabled {
		if r.LogSyncInterval <= 0 {
			return fmt.Errorf("log sync interval must be positive when log sync is enabled")
		}
		if r.LogSyncTailLines <= 0 {
			return fmt.Errorf("log sync tail lines must be positive when log sync is enabled")
		}
	}
	return nil
}
