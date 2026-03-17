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
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

// main 是 OnCall 系统的唯一进程入口。
//
// 功能：
// 1. 初始化应用配置（从 .env 和 config.yaml）
// 2. 初始化基础设施（Redis、MySQL、Elasticsearch）
// 3. 初始化 Agent 架构（对话、知识、运维工作流）
// 4. 启动 HTTP 服务并绑定路由
//
// 调用位置：
// - go run main.go（直接启动）
// - 通过 systemd/k8s 部署启动
//
// 输入：无（从配置文件读取）
// 输出：启动 HTTP 服务，监听 6872 端口
func main() {
	ctx := gctx.New()

	// 启动时优先加载本地 .env，确保网络工具和代理配置可用。
	err := godotenv.Load()
	if err != nil {
		log.Println("Error loading .env file, using system default env")
	}
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
	logSyncEnabled := g.Cfg().MustGet(ctx, "log_sync.enabled", false).Bool()
	logSyncNamespaces := g.Cfg().MustGet(ctx, "log_sync.namespaces", []string{"infra"}).Strings()
	logSyncInterval := g.Cfg().MustGet(ctx, "log_sync.interval", "30s").Duration()
	logSyncTailLines := g.Cfg().MustGet(ctx, "log_sync.tail_lines", 200).Int64()
	logSyncIndexPrefix := g.Cfg().MustGet(ctx, "log_sync.index_prefix", "logs-k8s").String()

	app, err := bootstrap.NewApplication(&bootstrap.Config{
		RedisAddr:          redisAddr.String(),
		RedisPassword:      "",
		RedisDB:            redisDB.Int(),
		LogLevel:           "info",
		PrometheusURL:      prometheusURL.String(),
		KubeConfig:         kubeConfig.String(),
		LogSyncEnabled:     logSyncEnabled,
		LogSyncNamespaces:  logSyncNamespaces,
		LogSyncInterval:    logSyncInterval,
		LogSyncTailLines:   logSyncTailLines,
		LogSyncIndexPrefix: logSyncIndexPrefix,
	})
	if err != nil {
		log.Fatalf("failed to init application: %v", err)
	}
	defer app.Close()

	log.Println("Agent architecture initialized successfully")
	log.Printf("Incident workflow agent ready")
	log.Printf("Prometheus URL: %s", prometheusURL.String())

	// 启动 HTTP 服务
	s := g.Server()
	s.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.CORSMiddleware)
		group.Middleware(middleware.ResponseMiddleware)
		group.Group("/v1", func(v1Group *ghttp.RouterGroup) {
			// 创建 controller 并传入统一会话 Agent
			chatController := chat.NewV1(app.DialogueAgent, app.Logger, app.RedisClient, app.OpsAgent, app.KnowledgeAgent)
			v1Group.Bind(chatController)
		})
	})
	s.SetPort(6872)
	s.Run()
}
