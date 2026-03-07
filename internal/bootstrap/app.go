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
	"go_agent/internal/concurrent"
	appcontext "go_agent/internal/context"
	"go_agent/internal/healing"
	"go_agent/utility/mem"

	"github.com/cloudwego/eino/adk"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Application 应用实例
type Application struct {
	ContextManager  *appcontext.ContextManager
	SupervisorAgent adk.ResumableAgent
	OpsIntegration  *ops.IntegratedOpsExecutor
	HealingManager  *healing.HealingLoopManager
	Logger          *zap.Logger
	RedisClient     *redis.Client
	CBManager       *concurrent.CircuitBreakerManager
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

	// 2.1 初始化 mem 工具（用于会话历史管理）
	if err := mem.InitRedis(redisClient, nil); err != nil {
		return nil, fmt.Errorf("failed to init mem utility: %w", err)
	}

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

	// 6.3.1 Ops 集成执行器（并发 + 缓存 + 熔断）
	opsIntegration, err := ops.NewIntegratedOpsExecutor(ctx, &ops.IntegratedOpsConfig{
		RedisClient:   redisClient,
		KubeConfig:    cfg.KubeConfig,
		PrometheusURL: cfg.PrometheusURL,
		Logger:        logger,
		Timeout:       30 * time.Second,
	})
	if err != nil {
		logger.Warn("failed to init integrated ops executor, degrade to normal path", zap.Error(err))
	}

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
		ChatModel:      chatModel,
		KnowledgeAgent: knowledgeAgent,
		DialogueAgent:  dialogueAgent,
		OpsAgent:       opsAgent,
		ExecutionAgent: executionAgent,
		RCAAgent:       rcaAgent,
		StrategyAgent:  strategyAgent,
		Logger:         logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create supervisor agent: %w", err)
	}

	logger.Info("supervisor agent initialized with Eino ADK")

	// 7. 初始化全局熔断器管理器（用于监控）
	cbManager := concurrent.NewCircuitBreakerManager(logger)

	// 8. 初始化自愈循环管理器
	healingConfig := &healing.HealingConfig{
		AutoTrigger:       true,
		MonitorInterval:   30 * time.Second,
		DetectionWindow:   5 * time.Minute,
		MaxRetries:        3,
		RetryDelay:        30 * time.Second,
		BackoffMultiplier: 2.0,
		RequireApproval:   false,
		ApprovalTimeout:   5 * time.Minute,
		EnableLearning:    true,
		MinConfidence:     0.7,
		RCAAgent:          rcaAgent,
		StrategyAgent:     strategyAgent,
		ExecutionAgent:    executionAgent,
		KnowledgeAgent:    knowledgeAgent,
		OpsAgent:          opsAgent,
	}

	healingManager, err := healing.NewHealingLoopManager(healingConfig, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create healing manager: %w", err)
	}

	// 启动自愈循环
	go func() {
		if err := healingManager.Start(context.Background()); err != nil {
			logger.Error("failed to start healing manager", zap.Error(err))
		}
	}()

	logger.Info("healing loop manager initialized and started")

	// 9. 启动后台任务
	go startBackgroundTasks(contextManager, cbManager, logger)

	return &Application{
		ContextManager:  contextManager,
		SupervisorAgent: supervisorAgent,
		OpsIntegration:  opsIntegration,
		HealingManager:  healingManager,
		Logger:          logger,
		RedisClient:     redisClient,
		CBManager:       cbManager,
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
func startBackgroundTasks(cm *appcontext.ContextManager, cbManager *concurrent.CircuitBreakerManager, logger *zap.Logger) {
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

	// 熔断器监控任务
	go func() {
		ticker := time.NewTicker(30 * time.Second)
		defer ticker.Stop()

		for range ticker.C {
			breakers := cbManager.List()
			for _, name := range breakers {
				cb, err := cbManager.Get(name)
				if err != nil {
					continue
				}

				state := cb.State()
				requests, successes, failures := cb.Counts()

				logger.Info("circuit breaker status",
					zap.String("name", name),
					zap.String("state", state.String()),
					zap.Uint32("requests", requests),
					zap.Uint32("successes", successes),
					zap.Uint32("failures", failures))
			}
		}
	}()
}

// Close 关闭应用
func (app *Application) Close() error {
	// 停止自愈循环
	if app.HealingManager != nil {
		if err := app.HealingManager.Stop(); err != nil {
			app.Logger.Error("failed to stop healing manager", zap.Error(err))
		}
	}

	if err := app.RedisClient.Close(); err != nil {
		return fmt.Errorf("failed to close redis: %w", err)
	}

	if err := app.Logger.Sync(); err != nil {
		return fmt.Errorf("failed to sync logger: %w", err)
	}

	return nil
}
