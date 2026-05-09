package common

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

const DefaultMilvusAddress = "localhost:31953"
const DefaultMilvusTimeout = 60 * time.Second
const DefaultEmbeddingDimension = 2048

type MilvusConfig struct {
	Address    string
	Database   string
	Collection string
	Timeout    time.Duration
}

// LoadEmbeddingDimension reads the vector dimension expected by Milvus schema.
func LoadEmbeddingDimension(ctx context.Context) int {
	if ctx == nil {
		ctx = context.Background()
	}
	return resolvePositiveInt(
		[]string{
			readMilvusConfigString(ctx, "doubao_embedding_model.dimensions"),
			readMilvusConfigString(ctx, "doubao_embedding_model.dimension"),
			os.Getenv("DOUBAO_EMBEDDING_MODEL_DIMENSIONS"),
			os.Getenv("DOUBAO_EMBEDDING_MODEL_DIMENSION"),
			os.Getenv("MILVUS_VECTOR_DIM"),
		},
		DefaultEmbeddingDimension,
	)
}

// LoadMilvusConfig 读取 Milvus 配置。
// 优先读取环境变量，便于 WSL/Windows 端口转发后覆盖动态 IP 和本地运行参数。
func LoadMilvusConfig(ctx context.Context) MilvusConfig {
	if ctx == nil {
		ctx = context.Background()
	}
	return MilvusConfig{
		Address: resolveMilvusSetting(
			os.Getenv("MILVUS_ADDRESS"),
			readMilvusConfigString(ctx, "milvus.address"),
			DefaultMilvusAddress,
		),
		Database: resolveMilvusSetting(
			os.Getenv("MILVUS_DATABASE"),
			readMilvusConfigString(ctx, "milvus.database"),
			MilvusDBName,
		),
		Collection: resolveMilvusSetting(
			os.Getenv("MILVUS_COLLECTION"),
			readMilvusConfigString(ctx, "milvus.collection"),
			MilvusCollectionName,
		),
		Timeout: resolveMilvusDuration(
			os.Getenv("MILVUS_TIMEOUT"),
			readMilvusConfigString(ctx, "milvus.timeout"),
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

func resolveMilvusSetting(primaryValue, fallbackValue, defaultValue string) string {
	if value := strings.TrimSpace(primaryValue); value != "" {
		return value
	}
	if value := strings.TrimSpace(fallbackValue); value != "" {
		return value
	}
	return strings.TrimSpace(defaultValue)
}

func resolveMilvusDuration(primaryValue, fallbackValue string, defaultValue time.Duration) time.Duration {
	for _, candidate := range []string{primaryValue, fallbackValue} {
		if value := strings.TrimSpace(candidate); value != "" {
			if parsed, err := time.ParseDuration(value); err == nil && parsed > 0 {
				return parsed
			}
		}
	}
	return defaultValue
}

func resolvePositiveInt(candidates []string, defaultValue int) int {
	for _, candidate := range candidates {
		value := strings.TrimSpace(candidate)
		if value == "" {
			continue
		}
		parsed, err := strconv.Atoi(value)
		if err == nil && parsed > 0 {
			return parsed
		}
	}
	return defaultValue
}
