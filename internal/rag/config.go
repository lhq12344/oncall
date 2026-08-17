package rag

import (
	"context"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gogf/gf/v2/frame/g"
)

type Config struct {
	HybridEnabled   bool
	RewriteEnabled  bool
	BM25Enabled     bool
	RerankerEnabled bool
	EmbeddingTopK   int
	BM25TopK        int
	FusionTopK      int
	FinalTopK       int
	MaxFinalTopK    int
	RRFK            int
	BM25Root        string
	RerankerURL     string
	RerankerTimeout time.Duration
}

func DefaultConfig() Config {
	return Config{
		HybridEnabled:   true,
		RewriteEnabled:  true,
		BM25Enabled:     true,
		RerankerEnabled: false,
		EmbeddingTopK:   20,
		BM25TopK:        20,
		FusionTopK:      20,
		FinalTopK:       3,
		MaxFinalTopK:    10,
		RRFK:            60,
		BM25Root:        ".oncall/rag/bm25",
		RerankerTimeout: 2 * time.Second,
	}
}

func LoadConfig(ctx context.Context) Config {
	if ctx == nil {
		ctx = context.Background()
	}
	cfg := DefaultConfig()
	cfg.HybridEnabled = readBool(ctx, "rag.hybrid_enabled", "RAG_HYBRID_ENABLED", cfg.HybridEnabled)
	cfg.RewriteEnabled = readBool(ctx, "rag.rewrite_enabled", "RAG_REWRITE_ENABLED", cfg.RewriteEnabled)
	cfg.BM25Enabled = readBool(ctx, "rag.bm25_enabled", "RAG_BM25_ENABLED", cfg.BM25Enabled)
	cfg.RerankerEnabled = readBool(ctx, "rag.reranker_enabled", "RAG_RERANKER_ENABLED", cfg.RerankerEnabled)
	cfg.EmbeddingTopK = readInt(ctx, "rag.embedding_top_k", "RAG_EMBEDDING_TOP_K", cfg.EmbeddingTopK)
	cfg.BM25TopK = readInt(ctx, "rag.bm25_top_k", "RAG_BM25_TOP_K", cfg.BM25TopK)
	cfg.FusionTopK = readInt(ctx, "rag.fusion_top_k", "RAG_FUSION_TOP_K", cfg.FusionTopK)
	cfg.FinalTopK = readInt(ctx, "rag.final_top_k", "RAG_FINAL_TOP_K", cfg.FinalTopK)
	cfg.MaxFinalTopK = readInt(ctx, "rag.max_final_top_k", "RAG_MAX_FINAL_TOP_K", cfg.MaxFinalTopK)
	cfg.RRFK = readInt(ctx, "rag.rrf_k", "RAG_RRF_K", cfg.RRFK)
	cfg.BM25Root = readString(ctx, "rag.bm25_root", "RAG_BM25_ROOT", cfg.BM25Root)
	cfg.RerankerURL = readString(ctx, "rag.reranker_url", "RAG_RERANKER_URL", cfg.RerankerURL)
	cfg.RerankerTimeout = readDuration(ctx, "rag.reranker_timeout", "RAG_RERANKER_TIMEOUT", cfg.RerankerTimeout)
	cfg.normalize()
	return cfg
}

func (c *Config) normalize() {
	defaults := DefaultConfig()
	if c.EmbeddingTopK <= 0 {
		c.EmbeddingTopK = defaults.EmbeddingTopK
	}
	if c.BM25TopK <= 0 {
		c.BM25TopK = defaults.BM25TopK
	}
	if c.FusionTopK <= 0 {
		c.FusionTopK = defaults.FusionTopK
	}
	if c.FinalTopK <= 0 {
		c.FinalTopK = defaults.FinalTopK
	}
	if c.MaxFinalTopK <= 0 {
		c.MaxFinalTopK = defaults.MaxFinalTopK
	}
	if c.FinalTopK > c.MaxFinalTopK {
		c.FinalTopK = c.MaxFinalTopK
	}
	if c.RRFK <= 0 {
		c.RRFK = defaults.RRFK
	}
	if strings.TrimSpace(c.BM25Root) == "" {
		c.BM25Root = defaults.BM25Root
	}
	if c.RerankerTimeout <= 0 {
		c.RerankerTimeout = defaults.RerankerTimeout
	}
}

func (c Config) CapFinalTopK(topK int) int {
	if topK <= 0 {
		topK = c.FinalTopK
	}
	if topK > c.MaxFinalTopK {
		topK = c.MaxFinalTopK
	}
	if topK <= 0 {
		return DefaultConfig().FinalTopK
	}
	return topK
}

func readString(ctx context.Context, key, envKey, fallback string) string {
	if value := strings.TrimSpace(readConfigValue(ctx, key)); value != "" {
		return value
	}
	if value := strings.TrimSpace(os.Getenv(envKey)); value != "" {
		return value
	}
	return strings.TrimSpace(fallback)
}

func readInt(ctx context.Context, key, envKey string, fallback int) int {
	for _, raw := range []string{readConfigValue(ctx, key), os.Getenv(envKey)} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if parsed, err := strconv.Atoi(raw); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func readBool(ctx context.Context, key, envKey string, fallback bool) bool {
	for _, raw := range []string{readConfigValue(ctx, key), os.Getenv(envKey)} {
		raw = strings.TrimSpace(strings.ToLower(raw))
		if raw == "" {
			continue
		}
		switch raw {
		case "1", "true", "yes", "y", "on":
			return true
		case "0", "false", "no", "n", "off":
			return false
		}
	}
	return fallback
}

func readDuration(ctx context.Context, key, envKey string, fallback time.Duration) time.Duration {
	for _, raw := range []string{readConfigValue(ctx, key), os.Getenv(envKey)} {
		raw = strings.TrimSpace(raw)
		if raw == "" {
			continue
		}
		if parsed, err := time.ParseDuration(raw); err == nil && parsed > 0 {
			return parsed
		}
	}
	return fallback
}

func readConfigValue(ctx context.Context, key string) (value string) {
	defer func() {
		if recover() != nil {
			value = ""
		}
	}()
	v := g.Cfg().MustGet(ctx, key)
	if v.IsEmpty() {
		return ""
	}
	return v.String()
}
