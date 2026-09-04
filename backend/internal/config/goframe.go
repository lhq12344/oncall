package config

import (
	"context"
	"os"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

type GoFrameLoadResult struct {
	Config  Config
	FileDir string
}

func LoadGoFrame(ctx context.Context) (GoFrameLoadResult, error) {
	fileDir, err := g.Cfg().Get(ctx, "file_dir")
	if err != nil {
		return GoFrameLoadResult{}, err
	}

	cfg := Default()
	redisAddr, _ := g.Cfg().Get(ctx, "redis.addr")
	redisDB, _ := g.Cfg().Get(ctx, "redis.db")
	dialTimeout, _ := g.Cfg().Get(ctx, "redis.dialTimeout")
	prometheusURL, _ := g.Cfg().Get(ctx, "prometheus.url")
	kubeConfig, _ := g.Cfg().Get(ctx, "kubeconfig")
	cfg.Storage.Redis.Addr = firstNonEmpty(os.Getenv("REDIS_ADDR"), redisAddr.String())
	cfg.Storage.Redis.DB = redisDB.Int()
	cfg.Storage.Redis.DialTimeout = time.Duration(dialTimeout.Int()) * time.Second
	cfg.Runtime.LogLevel = "info"
	cfg.Runtime.PrometheusURL = firstNonEmpty(os.Getenv("PROMETHEUS_URL"), prometheusURL.String())
	cfg.Runtime.KubeConfig = firstNonEmpty(os.Getenv("KUBECONFIG"), kubeConfig.String())
	cfg.Runtime.LogSyncEnabled = g.Cfg().MustGet(ctx, "log_sync.enabled", false).Bool()
	cfg.Runtime.LogSyncNamespaces = g.Cfg().MustGet(ctx, "log_sync.namespaces", []string{"infra"}).Strings()
	cfg.Runtime.LogSyncInterval = g.Cfg().MustGet(ctx, "log_sync.interval", "30s").Duration()
	cfg.Runtime.LogSyncTailLines = g.Cfg().MustGet(ctx, "log_sync.tail_lines", 200).Int64()
	cfg.Runtime.LogSyncIndexPrefix = g.Cfg().MustGet(ctx, "log_sync.index_prefix", "logs-k8s").String()
	cfg.Runtime.HooksConfigPath = os.Getenv("ONCALL_HOOKS_CONFIG")
	cfg.Observability.Trace.Exporter = firstNonEmpty(
		os.Getenv("ONCALL_TRACE_EXPORTER"),
		cozeloopExporterFromEnv(),
	)
	cfg.Observability.Trace.Endpoint = firstNonEmpty(
		os.Getenv("ONCALL_TRACE_ENDPOINT"),
		os.Getenv("COZELOOP_API_BASE_URL"),
	)
	return GoFrameLoadResult{Config: cfg, FileDir: fileDir.String()}, cfg.Validate()
}

func cozeloopExporterFromEnv() string {
	if os.Getenv("COZELOOP_API_BASE_URL") != "" &&
		os.Getenv("COZELOOP_WORKSPACE_ID") != "" &&
		os.Getenv("COZELOOP_API_TOKEN") != "" {
		return "cozeloop"
	}
	return ""
}
