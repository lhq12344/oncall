package app

import (
	"context"
	"fmt"
	"os"
	"time"

	"go_agent/internal/logic/agent/dialogue"
	"go_agent/internal/logic/agent/knowledge"
	aiembedder "go_agent/internal/logic/ai/embedder"
	"go_agent/internal/logic/ai/models"
	"go_agent/internal/logic/session/mem"

	"github.com/cloudwego/eino/adk"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// Application 应用实例，包含所有核心组件的引用。
//
// 字段说明：
// - DialogueAgent: 对话代理，处理用户聊天请求
// - KnowledgeAgent: 知识代理，处理知识库上传和检索
// - Logger: 日志记录器
// - RedisClient: Redis 客户端，用于会话状态存储
type Application struct {
	DialogueAgent  adk.ResumableAgent
	KnowledgeAgent adk.Agent
	Logger         *zap.Logger
	RedisClient    *redis.Client
}

// Config 应用配置结构，用于初始化 Application。
//
// 字段说明：
// - RedisAddr: Redis 服务器地址
// - RedisPassword: Redis 密码（可选）
// - RedisDB: Redis 数据库编号
// - LogLevel: 日志级别（debug/info/warn/error）
type Config struct {
	RedisAddr     string
	RedisPassword string
	RedisDB       int
	LogLevel      string
}

// NewApplication 创建并初始化应用实例。
//
// 功能：
// 1. 初始化日志系统
// 2. 初始化 Redis 客户端并测试连接
// 3. 初始化 LLM 模型和 Embedding
// 4. 创建对话和知识 Agent
//
// 调用位置：
// - main.go:90-101 行，启动时调用
//
// 输入：
// - cfg: 应用配置结构指针
//
// 输出：
// - *Application: 初始化完成的应用实例
// - error: 初始化过程中的错误
//
// 使用示例：
//
//	app, err := bootstrap.NewApplication(&bootstrap.Config{...})
//	if err != nil {
//	    log.Fatalf("failed to init application: %v", err)
//	}
//	defer app.Close()
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

	// 3. 初始化 LLM 模型
	chatModel, err := models.GetChatModel()
	if err != nil {
		return nil, fmt.Errorf("failed to get chat model: %w", err)
	}

	// 4. 初始化对话 Embedding（失败时降级为关键词分类）
	dialogueEmbedder, err := aiembedder.DoubaoEmbedding(ctx)
	if err != nil {
		logger.Warn("failed to init dialogue embedder, fallback to keyword-only intent analysis", zap.Error(err))
		dialogueEmbedder = nil
	}

	// 5. 初始化 Dialogue Agent（用于前端对话）
	logger.Info("initializing dialogue chat agent")
	dialogueAgent, err := dialogue.NewDialogueAgent(ctx, &dialogue.Config{
		ChatModel: chatModel,
		Embedder:  dialogueEmbedder,
		SkillsDir: os.Getenv("EINO_EXT_SKILLS_DIR"),
		Logger:    logger,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create dialogue agent: %w", err)
	}
	logger.Info("dialogue chat agent initialized")

	// 6. 初始化 Knowledge Agent（用于前端上传）
	logger.Info("initializing knowledge upload agent")
	knowledgeAgent, err := knowledge.NewKnowledgeAgent(ctx, &knowledge.Config{
		Logger: logger,
	})
	if err != nil {
		logger.Warn("failed to initialize knowledge upload agent, file upload will be unavailable",
			zap.Error(err))
		knowledgeAgent = nil
	} else {
		logger.Info("knowledge upload agent initialized")
	}

	return &Application{
		DialogueAgent:  dialogueAgent,
		KnowledgeAgent: knowledgeAgent,
		Logger:         logger,
		RedisClient:    redisClient,
	}, nil
}

// initLogger 初始化日志系统。
//
// 功能：根据配置的日志级别创建 zap 日志记录器
//
// 输入：
// - level: 日志级别字符串，支持 "debug"、"info"、"warn"、"error"，默认 "info"
//
// 输出：
// - *zap.Logger: 初始化完成的日志记录器
// - error: 初始化过程中的错误
//
// 使用示例：
//
//	logger, err := initLogger("info")
//	if err != nil {
//	    return nil, fmt.Errorf("failed to init logger: %w", err)
//	}
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

// Close 关闭应用，释放资源。
//
// 功能：
// 1. 关闭 Redis 客户端连接
// 2. 同步日志缓冲区
//
// 调用位置：
// - main.go:105 行，应用退出时调用（通过 defer）
//
// 输入：无
//
// 输出：
// - error: 关闭过程中的错误（如果有）
func (app *Application) Close() error {
	if err := app.RedisClient.Close(); err != nil {
		return fmt.Errorf("failed to close redis: %w", err)
	}

	if err := app.Logger.Sync(); err != nil {
		return fmt.Errorf("failed to sync logger: %w", err)
	}

	return nil
}
