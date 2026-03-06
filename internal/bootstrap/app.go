package bootstrap

import (
	"context"
	"fmt"
	"time"

	"go_agent/internal/agent/supervisor"
	appcontext "go_agent/internal/context"

	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Application 应用实例
type Application struct {
	ContextManager  *appcontext.ContextManager
	SupervisorAgent *supervisor.SupervisorAgent
	Logger          *zap.Logger
	RedisClient     *redis.Client
}

// Config 应用配置
type Config struct {
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	LogLevel      string
}

// NewApplication 创建应用实例
func NewApplication(cfg *Config) (*Application, error) {
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
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	if err := redisClient.Ping(ctx).Err(); err != nil {
		return nil, fmt.Errorf("failed to connect to redis: %w", err)
	}

	logger.Info("redis connected", zap.String("addr", cfg.RedisAddr))

	// 3. 初始化存储层
	storage := appcontext.NewRedisStorage(redisClient, "oncall")

	// 4. 初始化上下文管理器
	contextManager := appcontext.NewContextManager(storage)

	// 5. 初始化 Supervisor Agent
	supervisorAgent := supervisor.NewSupervisorAgent(&supervisor.Config{
		ContextManager: contextManager,
		Logger:         logger,
	})

	logger.Info("supervisor agent initialized")

	// 6. 启动后台任务（数据迁移）
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
