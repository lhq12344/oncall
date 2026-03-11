package bootstrap

import (
	"context"
	"fmt"
	"time"

	"go_agent/internal/agent/ops"
	"go_agent/internal/ai/indexer"
	"go_agent/internal/ai/models"
	"go_agent/internal/concurrent"
	appcontext "go_agent/internal/context"
	"go_agent/utility/mem"

	"github.com/cloudwego/eino/adk"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Application 应用实例
type Application struct {
	ContextManager *appcontext.ContextManager
	ChatAgent      adk.ResumableAgent
	OpsIntegration *ops.IntegratedOpsExecutor
	OpsAgent       adk.Agent   // Plan-Execute-Replan Ops Agent
	MilvusIndexer  interface{} // Milvus Indexer for direct document indexing
	Logger         *zap.Logger
	RedisClient    *redis.Client
	CBManager      *concurrent.CircuitBreakerManager
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

	// // 5. 初始化 LLM 模型
	chatModel, err := models.GetChatModel()
	if err != nil {
		return nil, fmt.Errorf("failed to get chat model: %w", err)
	}

	// 6. 初始化 Milvus Indexer（用于知识上传接口）
	var milvusIndexer interface{}
	createdMilvusIndexer, err := indexer.NewMilvusIndexer(ctx)
	if err != nil {
		logger.Warn("failed to init milvus indexer, file upload indexing disabled", zap.Error(err))
	} else {
		milvusIndexer = createdMilvusIndexer
		logger.Info("milvus indexer initialized")
	}

	// 7. 初始化 Ops Agent（集成 K8s 和 Prometheus）
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

	// 7.1 Ops 集成执行器（并发 + 缓存 + 熔断）
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

	// 8. 初始化统一故障处置工作流 Agent
	chatAgent, err := ops.NewIncidentWorkflowAgent(ctx, &ops.IncidentWorkflowConfig{
		ChatModel:     chatModel,
		KubeConfig:    cfg.KubeConfig,
		PrometheusURL: cfg.PrometheusURL,
		Logger:        logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create incident workflow agent: %w", err)
	}
	logger.Info("incident workflow chat agent initialized")

	// 9. 初始化全局熔断器管理器（用于监控）
	cbManager := concurrent.NewCircuitBreakerManager(logger)

	// 10. 启动后台任务
	go startBackgroundTasks(contextManager, cbManager, logger)

	return &Application{
		ContextManager: contextManager,
		ChatAgent:      chatAgent,
		OpsIntegration: opsIntegration,
		OpsAgent:       opsAgent,
		MilvusIndexer:  milvusIndexer,
		Logger:         logger,
		RedisClient:    redisClient,
		CBManager:      cbManager,
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
	if err := app.RedisClient.Close(); err != nil {
		return fmt.Errorf("failed to close redis: %w", err)
	}

	if err := app.Logger.Sync(); err != nil {
		return fmt.Errorf("failed to sync logger: %w", err)
	}

	return nil
}
