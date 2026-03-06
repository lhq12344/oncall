package main

import (
	"go_agent/internal/bootstrap"
	"go_agent/internal/controller/chat"
	"go_agent/utility/common"
	"go_agent/utility/mem"
	"go_agent/utility/middleware"
	"go_agent/utility/mysql"
	"log"
	"time"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/redis/go-redis/v9"
)

func main() {
	ctx := gctx.New()

	// 获取文件目录配置
	fileDir, err := g.Cfg().Get(ctx, "file_dir")
	if err != nil {
		panic(err)
	}
	common.FileDir = fileDir.String()

	// 初始化 Redis
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

	// 启动mysql
	cfg := mysql.LoadMySQLConfigFromFile()
	_, err = mysql.InitMySQL(ctx, cfg)
	if err != nil {
		log.Fatalf("init mysql failed: %v", err)
	}
	defer func() { _ = mysql.CloseMySQL() }()

	// 初始化新的 Agent 架构
	prometheusURL, _ := g.Cfg().Get(ctx, "prometheus.url")
	kubeConfig, _ := g.Cfg().Get(ctx, "kubeconfig")

	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr:     redisAddr.String(),
		RedisPassword: "",
		RedisDB:       redisDB.Int(),
		LogLevel:      "info",
		PrometheusURL: prometheusURL.String(),
		KubeConfig:    kubeConfig.String(),
	})
	if err != nil {
		log.Fatalf("failed to init application: %v", err)
	}
	defer app.Close()

	log.Println("Agent architecture initialized successfully")
	log.Printf("Supervisor Agent ready")
	log.Printf("Prometheus URL: %s", prometheusURL.String())

	// 启动 HTTP 服务
	s := g.Server()
	s.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.CORSMiddleware)
		group.Middleware(middleware.ResponseMiddleware)
		group.Group("/v1", func(v1Group *ghttp.RouterGroup) {
			v1Group.Bind(chat.NewV1())
		})
	})
	s.SetPort(6872)
	s.Run()
}
