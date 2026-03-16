package bootstrap

import (
	"context"
	"fmt"
	"time"

	"go_agent/internal/agent/dialogue"
	"go_agent/internal/agent/knowledge"
	"go_agent/internal/agent/ops"
	aiembedder "go_agent/internal/ai/embedder"
	"go_agent/internal/ai/models"
	appcontext "go_agent/internal/context"
	"go_agent/utility/mem"

	"github.com/cloudwego/eino/adk"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Application 应用实例
type Application struct {
	ContextManager *appcontext.ContextManager
	DialogueAgent  adk.ResumableAgent
	KnowledgeAgent adk.Agent
	OpsIntegration *ops.IntegratedOpsExecutor
	OpsAgent       adk.Agent
	Logger         *zap.Logger
	RedisClient    *redis.Client
}

// Config 应用配置
type Config struct {
	RedisAddr          string
	RedisPassword      string
	RedisDB            int
	LogLevel           string
	PrometheusURL      string   // Prometheus 地址
	KubeConfig         string   // K8s kubeconfig 路径
	LogSyncEnabled     bool     // 是否开启 Pod 日志写入 Elasticsearch
	LogSyncNamespaces  []string // 需要采集的命名空间列表
	LogSyncInterval    time.Duration
	LogSyncTailLines   int64
	LogSyncIndexPrefix string
}

// NewApplication 创建应用实例
func NewApplication(cfg *Config) (*Application, error) {
	ctx := context.Background()

	// 1. 初始化日志
	logger, err := initLogger(cfg.LogLevel)
	if err != nil {
		return nil, fmt.Errorf("failed to init logger: %w", err)
	}

	// 2. 初始化 Redis 客户端
	redisClient := redis.NewClient(&redis.Options{
		Addr:     cfg.RedisAddr,
		Password: cfg.RedisPassword,
		DB:       cfg.RedisDB,
	})

	// 测试 Redis 连接
	testCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(testCtx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	logger.Info("redis connected", zap.String("addr", cfg.RedisAddr))

	// 2.1 初始化 mem 工具（用于会话历史管理）
	if err := mem.InitRedis(redisClient, nil); err != nil {
		return nil, fmt.Errorf("failed to init mem utility: %w", err)
	}

	// 3. 初始化存储层
	storage := appcontext.NewRedisStorage(redisClient, "oncall")

	// 4. 初始化上下文管理器
	contextManager := appcontext.NewContextManager(storage)

	// // 5. 初始化 LLM 模型
	chatModel, err := models.GetChatModel()
	if err != nil {
		return nil, fmt.Errorf("failed to get chat model: %w", err)
	}

	// 6. 初始化对话 Embedding（失败时降级为关键词分类）
	dialogueEmbedder, err := aiembedder.DoubaoEmbedding(ctx)
	if err != nil {
		logger.Warn("failed to init dialogue embedder, fallback to keyword-only intent analysis", zap.Error(err))
		dialogueEmbedder = nil
	}

	// 7. 初始化 Dialogue Agent（用于前端对话）
	dialogueAgent, err := dialogue.NewDialogueAgent(ctx, &dialogue.Config{
		ChatModel:     chatModel,
		Embedder:      dialogueEmbedder,
		KubeConfig:    cfg.KubeConfig,
		PrometheusURL: cfg.PrometheusURL,
		Logger:        logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create dialogue agent: %w", err)
	}
	logger.Info("dialogue chat agent initialized")

	// 7.1 Ops 集成执行器（顺序工具查询 + 超时控制）
	opsIntegration, err := ops.NewIntegratedOpsExecutor(ctx, &ops.IntegratedOpsConfig{
		KubeConfig:    cfg.KubeConfig,
		PrometheusURL: cfg.PrometheusURL,
		Logger:        logger,
		Timeout:       30 * time.Second,
	})
	if err != nil {
		logger.Warn("failed to init integrated ops executor, degrade to normal path", zap.Error(err))
	}

	// 8. 初始化 Knowledge Agent（用于前端上传）
	knowledgeAgent, err := knowledge.NewKnowledgeAgent(ctx, &knowledge.Config{
		Logger: logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create knowledge agent: %w", err)
	}
	logger.Info("knowledge upload agent initialized")

	// 9. 初始化 Ops Agent（用于前端 ops 功能）
	opsAgent, err := ops.NewIncidentWorkflowAgent(ctx, &ops.IncidentWorkflowConfig{
		ChatModel:     chatModel,
		KubeConfig:    cfg.KubeConfig,
		PrometheusURL: cfg.PrometheusURL,
		Logger:        logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create incident workflow agent: %w", err)
	}
	logger.Info("incident workflow ops agent initialized")

	var podLogShipper *ops.PodLogShipper
	if cfg.LogSyncEnabled {
		podLogShipper, err = ops.NewPodLogShipper(&ops.PodLogShipperConfig{
			KubeConfig:  cfg.KubeConfig,
			Namespaces:  cfg.LogSyncNamespaces,
			Interval:    cfg.LogSyncInterval,
			TailLines:   cfg.LogSyncTailLines,
			IndexPrefix: cfg.LogSyncIndexPrefix,
			Logger:      logger,
		})
		if err != nil {
			logger.Warn("failed to init pod log shipper, log ingestion disabled", zap.Error(err))
		} else {
			logger.Info("pod log shipper initialized",
				zap.Strings("namespaces", cfg.LogSyncNamespaces))
		}
	}

	// 10. 启动后台任务
	go startBackgroundTasks(contextManager, logger, podLogShipper)

	return &Application{
		ContextManager: contextManager,
		DialogueAgent:  dialogueAgent,
		KnowledgeAgent: knowledgeAgent,
		OpsIntegration: opsIntegration,
		OpsAgent:       opsAgent,
		Logger:         logger,
		RedisClient:    redisClient,
	}, nil
}

// initLogger 初始化日志
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

// startBackgroundTasks 启动后台任务
func startBackgroundTasks(cm *appcontext.ContextManager, logger *zap.Logger, podLogShipper *ops.PodLogShipper) {
	// 数据迁移任务
	go func() {
		ticker := time.NewTicker(5 * time.Minute)
		defer ticker.Stop()

		for range ticker.C {
			ctx := context.Background()

			// 执行数据迁移（L1 → L2）
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

// Close 关闭应用
func (app *Application) Close() error {
	if err := app.RedisClient.Close(); err != nil {
		return fmt.Errorf("failed to close redis: %w", err)
	}

	if err := app.Logger.Sync(); err != nil {
		return fmt.Errorf("failed to sync logger: %w", err)
	}

	return nil
}
