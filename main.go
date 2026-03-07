package main

import (
	"go_agent/internal/bootstrap"
	"go_agent/internal/controller/chat"
	"go_agent/utility/common"
	es "go_agent/utility/elasticsearch"
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

	// 启动 Elasticsearch（可选，如果配置了则初始化）
	esCfg := es.LoadElasticsearchConfigFromFile()
	if len(esCfg.Addresses) > 0 || esCfg.CloudID != "" {
		_, err = es.InitElasticsearch(ctx, esCfg)
		if err != nil {
			log.Printf("Warning: failed to init elasticsearch: %v (will use fallback mode)", err)
		} else {
			log.Println("Elasticsearch initialized successfully")
		}
		defer func() { _ = es.CloseElasticsearch() }()
	} else {
		log.Println("Elasticsearch not configured, log query tool will use fallback mode")
	}

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
			// 创建 controller 并传入 supervisor agent 和 healing manager
			chatController := chat.NewV1(app.SupervisorAgent, app.Logger, app.RedisClient, app.OpsIntegration, app.HealingManager)
			v1Group.Bind(chatController)
		})
	})
	s.SetPort(6872)
	s.Run()
}
