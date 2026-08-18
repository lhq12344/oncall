package main

import (
	"log"
	"os"
	"time"

	"go_agent/internal/bootstrap"
	"go_agent/internal/controller/chat"
	"go_agent/utility/common"
	es "go_agent/utility/elasticsearch"
	"go_agent/utility/mem"
	"go_agent/utility/middleware"
	"go_agent/utility/mysql"

	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/net/ghttp"
	"github.com/gogf/gf/v2/os/gctx"
	"github.com/joho/godotenv"
	"github.com/redis/go-redis/v9"
)

// main starts the OnCall backend HTTP service on port 6872.
func main() {
	ctx := gctx.New()

	// Load backend-local .env first, then fall back to repo-root .env after the top-level split.
	envLoaded := false
	for _, envFile := range []string{".env", "../.env"} {
		if err := godotenv.Load(envFile); err == nil {
			envLoaded = true
			break
		}
	}
	if !envLoaded {
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

	mysqlCfg := mysql.LoadMySQLConfigFromFile()
	_, err = mysql.InitMySQL(ctx, mysqlCfg)
	if err != nil {
		log.Fatalf("init mysql failed: %v", err)
	}
	defer func() { _ = mysql.CloseMySQL() }()

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

	prometheusURL, _ := g.Cfg().Get(ctx, "prometheus.url")
	kubeConfig, _ := g.Cfg().Get(ctx, "kubeconfig")
	logSyncEnabled := g.Cfg().MustGet(ctx, "log_sync.enabled", false).Bool()
	logSyncNamespaces := g.Cfg().MustGet(ctx, "log_sync.namespaces", []string{"infra"}).Strings()
	logSyncInterval := g.Cfg().MustGet(ctx, "log_sync.interval", "30s").Duration()
	logSyncTailLines := g.Cfg().MustGet(ctx, "log_sync.tail_lines", 200).Int64()
	logSyncIndexPrefix := g.Cfg().MustGet(ctx, "log_sync.index_prefix", "logs-k8s").String()
	hooksConfigPath := os.Getenv("ONCALL_HOOKS_CONFIG")

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
		HooksConfigPath:    hooksConfigPath,
	})
	if err != nil {
		log.Fatalf("failed to init application: %v", err)
	}
	defer app.Close()

	log.Println("Agent architecture initialized successfully")
	log.Printf("Incident workflow agent ready")
	log.Printf("Prometheus URL: %s", prometheusURL.String())

	server := g.Server()
	server.Group("/api", func(group *ghttp.RouterGroup) {
		group.Middleware(middleware.CORSMiddleware)
		group.Middleware(middleware.ResponseMiddleware)
		group.Group("/v1", func(v1Group *ghttp.RouterGroup) {
			chatController := chat.NewV1WithHooks(
				app.DialogueAgent,
				app.Logger,
				app.RedisClient,
				app.OpsAgent,
				app.KnowledgeAgent,
				app.HookEngine,
			)
			v1Group.Bind(chatController)
		})
	})

	server.SetPort(6872)
	server.Run()
}
