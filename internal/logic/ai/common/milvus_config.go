package common

import (
	"context"
	"os"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

const DefaultMilvusAddress = "localhost:31953"
const DefaultMilvusTimeout = 8 * time.Second

type MilvusConfig struct {
	Address    string
	Database   string
	Collection string
	Timeout    time.Duration
}

// LoadMilvusConfig 读取 Milvus 配置。
// 按当前项目要求，优先读取 manifest/config/config.yaml，再回退到环境变量，最后使用默认值。
func LoadMilvusConfig(ctx context.Context) MilvusConfig {
	if ctx == nil {
		ctx = context.Background()
	}
	return MilvusConfig{
		Address: resolveMilvusSetting(
			readMilvusConfigString(ctx, "milvus.address"),
			os.Getenv("MILVUS_ADDRESS"),
			DefaultMilvusAddress,
		),
		Database: resolveMilvusSetting(
			readMilvusConfigString(ctx, "milvus.database"),
			os.Getenv("MILVUS_DATABASE"),
			MilvusDBName,
		),
		Collection: resolveMilvusSetting(
			readMilvusConfigString(ctx, "milvus.collection"),
			os.Getenv("MILVUS_COLLECTION"),
			MilvusCollectionName,
		),
		Timeout: resolveMilvusDuration(
			readMilvusConfigString(ctx, "milvus.timeout"),
			os.Getenv("MILVUS_TIMEOUT"),
			DefaultMilvusTimeout,
		),
	}
}

func readMilvusConfigString(ctx context.Context, key string) string {
	value := g.Cfg().MustGet(ctx, key)
	if value.IsEmpty() {
		return ""
	}
	return strings.TrimSpace(value.String())
}

func resolveMilvusSetting(configValue, envValue, defaultValue string) string {
	if value := strings.TrimSpace(configValue); value != "" {
		return value
	}
	if value := strings.TrimSpace(envValue); value != "" {
		return value
	}
	return strings.TrimSpace(defaultValue)
}

func resolveMilvusDuration(configValue, envValue string, defaultValue time.Duration) time.Duration {
	for _, candidate := range []string{configValue, envValue} {
		if value := strings.TrimSpace(candidate); value != "" {
			if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
				return parsed
			}
		}
	}
	return defaultValue
}
