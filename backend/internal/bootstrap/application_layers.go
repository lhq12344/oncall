package bootstrap

import (
	"context"
	"fmt"
	"time"

	cozeloopadapter "go_agent/internal/adapters/cozeloop"
	esutil "go_agent/internal/adapters/elasticsearch"
	redisadapter "go_agent/internal/adapters/redis"
	aiembedder "go_agent/internal/ai/embedder"
	"go_agent/internal/ai/models"
	appcontext "go_agent/internal/context"
	hookpkg "go_agent/internal/hooks"
	"go_agent/internal/knowledge"
	"go_agent/internal/model"
	"go_agent/internal/telemetry"
	"go_agent/internal/tools/policy"
	"go_agent/internal/workflow/dialogue"
	"go_agent/internal/workflow/ops"

	"github.com/cloudwego/eino/adk"
	"go.uber.org/zap"
)

func defaultLayerRegistry() *LayerRegistry {
	registry := NewLayerRegistry()
	registry.Register("infrastructure", buildInfrastructureLayer)
	registry.Register("state", buildStateLayer)
	registry.Register("agents", buildAgentLayer)
	registry.Register("runtime", buildRuntimeLayer)
	registry.Register("background", buildBackgroundLayer)
	return registry
}

func buildInfrastructureLayer(ctx context.Context, assembly *Assembly) error {
	if assembly == nil || assembly.Config == nil {
		return fmt.Errorf("config is required")
	}
	if assembly.App == nil {
		assembly.App = &Application{}
	}
	cfg := assembly.Config
	typed := cfg.Typed

	logger, err := initLogger(typed.Runtime.LogLevel)
	if err != nil {
		return fmt.Errorf("failed to init logger: %w", err)
	}

	hookConfig := cfg.Hooks
	if cfg.HooksConfigPath != "" {
		hookConfig, err = hookpkg.LoadConfigFile(cfg.HooksConfigPath)
		if err != nil {
			return fmt.Errorf("failed to load hooks config: %w", err)
		}
	}
	hookEngine, err := hookpkg.NewEngineFromConfig(hookConfig)
	if err != nil {
		return fmt.Errorf("failed to initialize hooks: %w", err)
	}
	hookpkg.SetDefaultEngine(hookEngine)
	logger.Info("hook engine initialized",
		zap.Bool("enabled", hookEngine.Enabled()),
		zap.Int("rules", hookEngine.RuleCount()))

	var redisClient *redisadapter.Client
	if typed.Storage.Redis.Addr != "" {
		candidate, redisErr := redisadapter.Connect(ctx, redisadapter.Config{
			Addr:        typed.Storage.Redis.Addr,
			Password:    typed.Storage.Redis.Password,
			DB:          typed.Storage.Redis.DB,
			DialTimeout: typed.Storage.Redis.DialTimeout,
		})
		if redisErr != nil {
			logger.Warn("redis unavailable; using in-memory state", zap.String("addr", typed.Storage.Redis.Addr), zap.Error(redisErr))
		} else {
			redisClient = candidate
			logger.Info("redis connected", zap.String("addr", typed.Storage.Redis.Addr))
		}
	} else {
		logger.Info("redis not configured; using in-memory state")
	}

	esCfg := esutil.LoadElasticsearchConfigFromFile()
	var esClient *esutil.Client
	if len(esCfg.Addresses) > 0 || esCfg.CloudID != "" {
		esClient, err = esutil.InitElasticsearch(ctx, esCfg)
		if err != nil {
			logger.Warn("failed to init elasticsearch; log query tool will use fallback mode", zap.Error(err))
		} else {
			logger.Info("elasticsearch initialized successfully")
		}
	} else {
		logger.Info("elasticsearch not configured, log query tool will use fallback mode")
	}

	traceRecorder, traceErr := cozeloopadapter.NewFromEnv(nil)
	if traceErr != nil {
		logger.Warn("cozeloop unavailable; using degraded telemetry", zap.Error(traceErr))
		traceRecorder = cozeloopadapter.NewRecorder()
	} else if traceRecorder.Dropped() == 0 && cfg.Typed.Observability.Trace.Exporter == "cozeloop" {
		logger.Info("cozeloop telemetry initialized")
	} else {
		logger.Info("cozeloop not configured; using degraded telemetry")
	}

	telemetryRecorder := telemetry.NewRecorder(traceRecorder)
	chatModel, err := models.GetChatModelWithTelemetry(telemetryRecorder)
	if err != nil {
		if redisClient != nil {
			_ = redisClient.Close()
		}
		return fmt.Errorf("failed to get chat model: %w", err)
	}

	dialogueEmbedder, err := aiembedder.DoubaoEmbedding(ctx)
	if err != nil {
		logger.Warn("failed to init dialogue embedder, fallback to keyword-only intent analysis", zap.Error(err))
		dialogueEmbedder = nil
	}

	infra := &Infrastructure{
		ModelCatalog:     model.DefaultCatalog(),
		Telemetry:        telemetryRecorder,
		ToolPolicy:       policy.NewEngine(""),
		Logger:           logger,
		HookEngine:       hookEngine,
		RedisClient:      redisClient,
		Elasticsearch:    esClient,
		ChatModel:        chatModel,
		DialogueEmbedder: dialogueEmbedder,
	}
	assembly.Infra = infra
	assembly.App.Infra = infra
	assembly.App.Logger = logger
	assembly.App.RedisClient = redisClient
	assembly.App.HookEngine = hookEngine
	assembly.App.Telemetry = infra.Telemetry
	return nil
}

func buildStateLayer(ctx context.Context, assembly *Assembly) error {
	if assembly == nil || assembly.App == nil || assembly.Infra == nil {
		return fmt.Errorf("infrastructure is required")
	}
	var storage appcontext.Storage = appcontext.NewMemoryStorage("oncall")
	if assembly.Infra.RedisClient != nil {
		storage = redisadapter.NewStorage(assembly.Infra.RedisClient, "oncall")
	}
	state := &StateLayer{ContextManager: appcontext.NewContextManager(storage)}
	assembly.State = state
	assembly.App.State = state
	assembly.App.ContextManager = state.ContextManager
	_ = ctx
	return nil
}

func buildAgentLayer(ctx context.Context, assembly *Assembly) error {
	if assembly == nil || assembly.Config == nil || assembly.App == nil || assembly.Infra == nil {
		return fmt.Errorf("infrastructure is required")
	}
	cfg := assembly.Config
	typed := cfg.Typed
	logger := assembly.Infra.Logger
	chatModel := assembly.Infra.ChatModel

	logger.Info("initializing dialogue chat agent")
	dialogueAgent, err := dialogue.NewDialogueAgent(ctx, &dialogue.Config{
		ChatModel:     chatModel,
		Embedder:      assembly.Infra.DialogueEmbedder,
		KubeConfig:    typed.Runtime.KubeConfig,
		PrometheusURL: typed.Runtime.PrometheusURL,
		Logger:        logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create dialogue agent: %w", err)
	}
	logger.Info("dialogue chat agent initialized")

	opsIntegration, err := ops.NewIntegratedOpsExecutor(ctx, &ops.IntegratedOpsConfig{
		KubeConfig:    typed.Runtime.KubeConfig,
		PrometheusURL: typed.Runtime.PrometheusURL,
		Logger:        logger,
		Timeout:       30 * time.Second,
	})
	if err != nil {
		logger.Warn("failed to init integrated ops executor, degrade to normal path", zap.Error(err))
	}

	logger.Info("initializing knowledge upload agent")
	knowledgeAgent := buildOptionalKnowledgeAgent(ctx, logger)

	logger.Info("initializing incident workflow ops agent")
	opsAgent, err := ops.NewIncidentWorkflowAgent(ctx, &ops.IncidentWorkflowConfig{
		ChatModel:     chatModel,
		KubeConfig:    typed.Runtime.KubeConfig,
		PrometheusURL: typed.Runtime.PrometheusURL,
		Logger:        logger,
	})
	if err != nil {
		return fmt.Errorf("failed to create incident workflow agent: %w", err)
	}
	logger.Info("incident workflow ops agent initialized")

	agents := &AgentLayer{
		DialogueAgent:  dialogueAgent,
		KnowledgeAgent: knowledgeAgent,
		OpsIntegration: opsIntegration,
		OpsAgent:       opsAgent,
	}
	assembly.Agents = agents
	assembly.App.Agents = agents
	assembly.App.DialogueAgent = dialogueAgent
	assembly.App.KnowledgeAgent = knowledgeAgent
	assembly.App.OpsIntegration = opsIntegration
	assembly.App.OpsAgent = opsAgent
	return nil
}

type knowledgeAgentFactory func(context.Context, *knowledge.Config) (adk.Agent, error)

// buildOptionalKnowledgeAgent keeps knowledge indexing optional at process
// startup. Missing embedding credentials or an unavailable Milvus instance
// must disable only the upload capability; dialogue and incident handling
// remain available through their own degraded paths.
func buildOptionalKnowledgeAgent(ctx context.Context, logger *zap.Logger) adk.Agent {
	return buildOptionalKnowledgeAgentWith(ctx, logger, knowledge.NewKnowledgeAgent)
}

func buildOptionalKnowledgeAgentWith(ctx context.Context, logger *zap.Logger, factory knowledgeAgentFactory) adk.Agent {
	agent, err := factory(ctx, &knowledge.Config{Logger: logger})
	if err != nil {
		if logger != nil {
			logger.Warn("knowledge upload agent unavailable; continuing without knowledge upload",
				zap.Error(err))
		}
		return nil
	}
	if logger != nil {
		logger.Info("knowledge upload agent initialized")
	}
	return agent
}

func buildBackgroundLayer(ctx context.Context, assembly *Assembly) error {
	if assembly == nil || assembly.Config == nil || assembly.App == nil || assembly.Infra == nil {
		return fmt.Errorf("infrastructure is required")
	}
	cfg := assembly.Config
	typed := cfg.Typed
	logger := assembly.Infra.Logger
	background := &BackgroundLayer{}
	if typed.Runtime.LogSyncEnabled {
		podLogShipper, err := ops.NewPodLogShipper(&ops.PodLogShipperConfig{
			KubeConfig:  typed.Runtime.KubeConfig,
			Namespaces:  typed.Runtime.LogSyncNamespaces,
			Interval:    typed.Runtime.LogSyncInterval,
			TailLines:   typed.Runtime.LogSyncTailLines,
			IndexPrefix: typed.Runtime.LogSyncIndexPrefix,
			Logger:      logger,
		})
		if err != nil {
			logger.Warn("failed to init pod log shipper, log ingestion disabled", zap.Error(err))
		} else {
			background.PodLogShipper = podLogShipper
			logger.Info("pod log shipper initialized", zap.Strings("namespaces", cfg.LogSyncNamespaces))
		}
	}
	assembly.Background = background
	assembly.App.Background = background
	_ = ctx
	return nil
}
