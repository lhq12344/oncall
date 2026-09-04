package config

import (
	"os"
	"strconv"
	"strings"
	"time"
)

type Env map[string]string

func LoadEnv(getenv func(string) string) Config {
	if getenv == nil {
		getenv = os.Getenv
	}
	cfg := Default()
	cfg.Runtime.LogLevel = firstNonEmpty(getenv("ONCALL_LOG_LEVEL"), cfg.Runtime.LogLevel)
	cfg.Runtime.PrometheusURL = getenv("PROMETHEUS_URL")
	cfg.Runtime.KubeConfig = getenv("KUBECONFIG")
	cfg.Runtime.HooksConfigPath = getenv("ONCALL_HOOKS_CONFIG")
	cfg.Storage.Redis.Addr = getenv("REDIS_ADDR")
	cfg.Storage.Redis.Password = getenv("REDIS_PASSWORD")
	cfg.Storage.Redis.DB = intEnv(getenv("REDIS_DB"), cfg.Storage.Redis.DB)
	cfg.Storage.Redis.DialTimeout = durationEnv(getenv("REDIS_DIAL_TIMEOUT"), cfg.Storage.Redis.DialTimeout)
	cfg.Storage.Milvus.Address = firstNonEmpty(getenv("MILVUS_ADDRESS"), cfg.Storage.Milvus.Address)
	cfg.Storage.Milvus.Database = firstNonEmpty(getenv("MILVUS_DATABASE"), cfg.Storage.Milvus.Database)
	cfg.Storage.Milvus.Collection = firstNonEmpty(getenv("MILVUS_COLLECTION"), cfg.Storage.Milvus.Collection)
	cfg.Storage.Milvus.KnowledgeV2Collection = firstNonEmpty(getenv("MILVUS_KNOWLEDGE_V2_COLLECTION"), cfg.Storage.Milvus.KnowledgeV2Collection)
	cfg.Storage.Milvus.OpsV2Collection = firstNonEmpty(getenv("MILVUS_OPS_V2_COLLECTION"), cfg.Storage.Milvus.OpsV2Collection)
	cfg.Storage.Milvus.Timeout = durationEnv(getenv("MILVUS_TIMEOUT"), cfg.Storage.Milvus.Timeout)
	cfg.Observability.Trace.Exporter = getenv("ONCALL_TRACE_EXPORTER")
	cfg.Observability.Trace.Endpoint = getenv("ONCALL_TRACE_ENDPOINT")
	if cfg.Observability.Trace.Exporter == "" &&
		getenv("COZELOOP_API_BASE_URL") != "" &&
		getenv("COZELOOP_WORKSPACE_ID") != "" &&
		getenv("COZELOOP_API_TOKEN") != "" {
		cfg.Observability.Trace.Exporter = "cozeloop"
		cfg.Observability.Trace.Endpoint = getenv("COZELOOP_API_BASE_URL")
	}
	return cfg
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		if trimmed := strings.TrimSpace(value); trimmed != "" {
			return trimmed
		}
	}
	return ""
}

func intEnv(value string, fallback int) int {
	if parsed, err := strconv.Atoi(strings.TrimSpace(value)); err == nil {
		return parsed
	}
	return fallback
}

func durationEnv(value string, fallback time.Duration) time.Duration {
	if parsed, err := time.ParseDuration(strings.TrimSpace(value)); err == nil {
		return parsed
	}
	return fallback
}
