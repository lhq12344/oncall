package cmd

import (
	"context"
	"log"
	"time"

	"go_agent/internal/controller/chat"
	"go_agent/internal/logic/ai/common"
	"go_agent/internal/logic/app"
	"go_agent/internal/logic/session/mem"
	"go_agent/utility/middleware"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

// Main is the application command entry used by main.go.
var Main = command{}

type command struct{}

// Run initializes dependencies, binds GoFrame routes, and starts the HTTP server.
func (command) Run(ctx context.Context) {
	if err := godotenv.Load(); err != nil {
		log.Println("Error loading .env file, using system default env")
	}

	fileDir, err := g.Cfg().Get(ctx, "file_dir")
	if err != nil {
		panic(err)
	}
	common.FileDir = fileDir.String()

	redisAddr, _ := g.Cfg().Get(ctx, "redis.addr")
	redisDB, _ := g.Cfg().Get(ctx, "redis.db")
	dialTimeout, _ := g.Cfg().Get(ctx, "redis.dialTimeout")

	rdb := redis.NewClient(&redis.Options{
		Addr:        redisAddr.String(),
		DB:          redisDB.Int(),
		DialTimeout: time.Duration(dialTimeout.Int()) * time.Second,
	})

	if err := mem.InitRedis(rdb, &mem.Config{
		MaxInputTokens:         96000,
		ReserveOutputTokens:    8192,
		ReserveToolsDefault:    20000,
		SafetyTokens:           2048,
		TTL:                    2 * time.Hour,
		KeepReasoningInContext: false,
	}); err != nil {
		panic(err)
	}
	defer func() { _ = rdb.Close() }()

	application, err := app.NewApplication(&app.Config{
		RedisAddr:     redisAddr.String(),
		RedisPassword: "",
		RedisDB:       redisDB.Int(),
		LogLevel:      "info",
	})
	if err != nil {
		log.Fatalf("failed to init application: %v", err)
	}
	defer application.Close()

	log.Println("Dialogue and knowledge application initialized successfully")

	s := g.Server()
	s.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.CORSMiddleware)
		group.Middleware(middleware.ResponseMiddleware)
		group.Group("/v1", func(v1Group *ghttp.RouterGroup) {
			chatController := chat.NewV1(
				application.DialogueAgent,
				application.Logger,
				application.RedisClient,
				application.KnowledgeAgent,
			)
			v1Group.Bind(chatController)
		})
	})
	s.SetPort(6872)
	s.Run()
}
