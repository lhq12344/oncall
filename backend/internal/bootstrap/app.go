package bootstrap

import (
	"context"
	"fmt"
	"time"

	esutil "go_agent/internal/adapters/elasticsearch"
	redisadapter "go_agent/internal/adapters/redis"
	appconfig "go_agent/internal/config"
	appcontext "go_agent/internal/context"
	hookpkg "go_agent/internal/hooks"
	"go_agent/internal/model"
	"go_agent/internal/telemetry"
	"go_agent/internal/workflow/ops"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

// Application contains assembled runtime modules plus transport-facing handles
// exported by the deterministic layer registry.
type Application struct {
	Infra      *Infrastructure
	State      *StateLayer
	Agents     *AgentLayer
	Runtime    *RuntimeLayer
	Background *BackgroundLayer

	ContextManager *appcontext.ContextManager
	DialogueAgent  adk.ResumableAgent
	KnowledgeAgent adk.Agent
	OpsIntegration *ops.IntegratedOpsExecutor
	OpsAgent       adk.Agent
	Logger         *zap.Logger
	RedisClient    *redisadapter.Client
	HookEngine     *hookpkg.Engine
	ModelCatalog   *model.Catalog
	Telemetry      *telemetry.Recorder
}

// Config is the bootstrap input shape accepted by the current process entry
// points. Typed is the canonical config; scalar fields are normalized into it.
type Config struct {
	Typed              appconfig.Config
	RedisAddr          string
	RedisPassword      string
	RedisDB            int
	RedisDialTimeout   time.Duration
	LogLevel           string
	PrometheusURL      string
	KubeConfig         string
	LogSyncEnabled     bool
	LogSyncNamespaces  []string
	LogSyncInterval    time.Duration
	LogSyncTailLines   int64
	LogSyncIndexPrefix string
	HooksConfigPath    string
	Hooks              hookpkg.Config
}

func (cfg *Config) Normalize() (appconfig.Config, error) {
	if cfg == nil {
		out := appconfig.Default()
		return out, out.Validate()
	}
	out := cfg.Typed
	if len(out.Models) == 0 {
		out = appconfig.Default()
	}
	out.Runtime.LogLevel = firstNonEmptyString(cfg.LogLevel, out.Runtime.LogLevel)
	out.Runtime.PrometheusURL = firstNonEmptyString(cfg.PrometheusURL, out.Runtime.PrometheusURL)
	out.Runtime.KubeConfig = firstNonEmptyString(cfg.KubeConfig, out.Runtime.KubeConfig)
	out.Runtime.LogSyncEnabled = cfg.LogSyncEnabled
	if len(cfg.LogSyncNamespaces) > 0 {
		out.Runtime.LogSyncNamespaces = append([]string(nil), cfg.LogSyncNamespaces...)
	}
	if cfg.LogSyncInterval > 0 {
		out.Runtime.LogSyncInterval = cfg.LogSyncInterval
	}
	if cfg.LogSyncTailLines > 0 {
		out.Runtime.LogSyncTailLines = cfg.LogSyncTailLines
	}
	out.Runtime.LogSyncIndexPrefix = firstNonEmptyString(cfg.LogSyncIndexPrefix, out.Runtime.LogSyncIndexPrefix)
	out.Runtime.HooksConfigPath = firstNonEmptyString(cfg.HooksConfigPath, out.Runtime.HooksConfigPath)
	out.Storage.Redis.Addr = firstNonEmptyString(cfg.RedisAddr, out.Storage.Redis.Addr)
	out.Storage.Redis.Password = firstNonEmptyString(cfg.RedisPassword, out.Storage.Redis.Password)
	out.Storage.Redis.DB = cfg.RedisDB
	if cfg.RedisDialTimeout > 0 {
		out.Storage.Redis.DialTimeout = cfg.RedisDialTimeout
	}
	return out, out.Validate()
}

// NewApplication builds the current backend application through the deterministic
// layer registry.
func NewApplication(cfg *Config) (*Application, error) {
	ctx := context.Background()
	if cfg == nil {
		cfg = &Config{}
	}
	typed, err := cfg.Normalize()
	if err != nil {
		return nil, err
	}
	cfg.Typed = typed
	app := &Application{}
	assembly := &Assembly{Config: cfg, App: app}
	if err := defaultLayerRegistry().Build(ctx, assembly); err != nil {
		_ = app.Close()
		return nil, err
	}
	if assembly.State != nil && assembly.Background != nil {
		go startBackgroundTasks(assembly.State.ContextManager, assembly.Infra.Logger, assembly.Background.PodLogShipper)
	}
	return app, nil
}

func firstNonEmptyString(values ...string) string {
	for _, value := range values {
		if value != "" {
			return value
		}
	}
	return ""
}

// initLogger creates the process logger from a validated log level.
func initLogger(level string) (*zap.Logger, error) {
	var zapLevel zap.AtomicLevel

	switch level {
	case "debug":
		zapLevel = zap.NewAtomicLevelAt(zap.DebugLevel)
	case "info":
		zapLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	case "warn":
		zapLevel = zap.NewAtomicLevelAt(zap.WarnLevel)
	case "error":
		zapLevel = zap.NewAtomicLevelAt(zap.ErrorLevel)
	default:
		zapLevel = zap.NewAtomicLevelAt(zap.InfoLevel)
	}

	config := zap.Config{
		Level:            zapLevel,
		Development:      false,
		Encoding:         "json",
		EncoderConfig:    zap.NewProductionEncoderConfig(),
		OutputPaths:      []string{"stdout"},
		ErrorOutputPaths: []string{"stderr"},
	}

	return config.Build()
}

// startBackgroundTasks launches optional maintenance loops after startup.
func startBackgroundTasks(cm *appcontext.ContextManager, logger *zap.Logger, podLogShipper *ops.PodLogShipper) {
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			ctx := context.Background()

			// Run session migration.
			if err := cm.MigrateToL2(ctx); err != nil {
				logger.Error("failed to migrate to L2", zap.Error(err))
			} else {
				logger.Debug("migrated inactive sessions to L2")
			}
		}
	}()

	if podLogShipper != nil {
		go podLogShipper.Start(context.Background())
	}
}

// Close releases process resources.
func (app *Application) Close() error {
	if app == nil {
		return nil
	}

	var firstErr error
	if app.Telemetry != nil {
		app.Telemetry.Flush(context.Background())
		app.Telemetry.Close(context.Background())
	}
	if app.RedisClient != nil {
		if err := app.RedisClient.Close(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to close redis: %w", err)
		}
	}
	if err := esutil.CloseElasticsearch(); err != nil && firstErr == nil {
		firstErr = fmt.Errorf("failed to close elasticsearch: %w", err)
	}
	if app.Logger != nil {
		if err := app.Logger.Sync(); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("failed to sync logger: %w", err)
		}
	}

	return firstErr
}
