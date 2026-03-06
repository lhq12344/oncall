package bootstrap

import (
	"context"
	"fmt"
	"time"

	"go_agent/internal/agent/dialogue"
	"go_agent/internal/agent/execution"
	"go_agent/internal/agent/knowledge"
	"go_agent/internal/agent/ops"
	"go_agent/internal/agent/rca"
	"go_agent/internal/agent/strategy"
	"go_agent/internal/agent/supervisor"
	"go_agent/internal/ai/embedder"
	"go_agent/internal/ai/models"
	appcontext "go_agent/internal/context"

	"github.com/cloudwego/eino/adk"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Application 应用实例
type Application struct {
	ContextManager  *appcontext.ContextManager
	SupervisorAgent adk.ResumableAgent
	Logger          *zap.Logger
	RedisClient     *redis.Client
}

// Config 应用配置
type Config struct {
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	LogLevel      string
	PrometheusURL string // Prometheus 地址
	KubeConfig    string // K8s kubeconfig 路径
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

	// 3. 初始化存储层
	storage := appcontext.NewRedisStorage(redisClient, "oncall")

	// 4. 初始化上下文管理器
	contextManager := appcontext.NewContextManager(storage)

	// 5. 初始化 LLM 模型
	chatModel, err := models.GetChatModel()
	if err != nil {
		return nil, fmt.Errorf("failed to get chat model: %w", err)
	}

	// 5.1 初始化 Embedder（用于语义相似度计算）
	embeddingModel, err := embedder.DoubaoEmbedding(ctx)
	if err != nil {
		logger.Warn("failed to initialize embedder, dialogue agent will work without semantic similarity",
			zap.Error(err))
		embeddingModel = nil // 允许降级
	} else {
		logger.Info("embedder initialized (Doubao)")
	}

	// 6. 初始化各个 Agent

	// 6.1 Knowledge Agent（集成 Milvus）
	knowledgeAgent, err := knowledge.NewKnowledgeAgent(ctx, &knowledge.Config{
		ChatModel: chatModel,
		Logger:    logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create knowledge agent: %w", err)
	}
	logger.Info("knowledge agent initialized with Milvus integration")

	// 6.2 Dialogue Agent（集成 Embedder）
	dialogueAgent, err := dialogue.NewDialogueAgent(ctx, &dialogue.Config{
		ChatModel: chatModel,
		Embedder:  embeddingModel, // 可能为 nil（降级）
		Logger:    logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create dialogue agent: %w", err)
	}
	logger.Info("dialogue agent initialized with enhanced intent analysis")

	// 6.3 Ops Agent（集成 K8s 和 Prometheus）
	opsAgent, err := ops.NewOpsAgent(ctx, &ops.Config{
		ChatModel:     chatModel,
		KubeConfig:    cfg.KubeConfig, // 从配置读取
		PrometheusURL: cfg.PrometheusURL,
		Logger:        logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create ops agent: %w", err)
	}
	logger.Info("ops agent initialized with K8s and Prometheus integration",
		zap.String("prometheus_url", cfg.PrometheusURL))

	// 6.4 Execution Agent（执行计划生成和安全执行）
	executionAgent, err := execution.NewExecutionAgent(ctx, &execution.Config{
		ChatModel: chatModel,
		Logger:    logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create execution agent: %w", err)
	}
	logger.Info("execution agent initialized with sandbox execution")

	// 6.5 RCA Agent（根因分析）
	rcaAgent, err := rca.NewRCAAgent(ctx, &rca.Config{
		ChatModel: chatModel,
		Logger:    logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create rca agent: %w", err)
	}
	logger.Info("rca agent initialized with root cause analysis")

	// 6.6 Strategy Agent（策略评估和优化）
	strategyAgent, err := strategy.NewStrategyAgent(ctx, &strategy.Config{
		ChatModel: chatModel,
		Logger:    logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create strategy agent: %w", err)
	}
	logger.Info("strategy agent initialized with strategy optimization")

	// 6.7 Supervisor Agent（使用 Eino ADK prebuilt supervisor）
	supervisorAgent, err := supervisor.NewSupervisorAgent(ctx, &supervisor.Config{
		ChatModel:       chatModel,
		KnowledgeAgent:  knowledgeAgent,
		DialogueAgent:   dialogueAgent,
		OpsAgent:        opsAgent,
		ExecutionAgent:  executionAgent,
		RCAAgent:        rcaAgent,
		StrategyAgent:   strategyAgent,
		Logger:          logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create supervisor agent: %w", err)
	}

	logger.Info("supervisor agent initialized with Eino ADK")

	// 7. 启动后台任务（数据迁移）
	go startBackgroundTasks(contextManager, logger)

	return &Application{
		ContextManager:  contextManager,
		SupervisorAgent: supervisorAgent,
		Logger:          logger,
		RedisClient:     redisClient,
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
func startBackgroundTasks(cm *appcontext.ContextManager, logger *zap.Logger) {
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
