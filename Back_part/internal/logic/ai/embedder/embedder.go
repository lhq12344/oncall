package embedder

import (
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/cloudwego/eino-ext/components/embedding/ark"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/gogf/gf/v2/frame/g"
)

// DoubaoEmbedding 从配置构建 Ark Embedder。
// 输入：配置项 doubao_embedding_model.{model,api_key,base_url,api_type}
// 输出：符合 Eino embedding.Embedder 接口的实例。
func DoubaoEmbedding(ctx context.Context) (embedding.Embedder, error) {
	model := readEmbeddingSetting(ctx, "doubao_embedding_model.model", "DOUBAO_EMBEDDING_MODEL_MODEL")
	apiKey := readEmbeddingSetting(ctx, "doubao_embedding_model.api_key", "DOUBAO_EMBEDDING_MODEL_API_KEY")
	baseURL := readEmbeddingSetting(ctx, "doubao_embedding_model.base_url", "DOUBAO_EMBEDDING_MODEL_BASE_URL")

	// 可选配置：api_type=text|multi_modal。支持 config.yaml（doubao_embedding_model.api_type）
	// 和环境变量 DOUBAO_EMBEDDING_MODEL_API_TYPE，未配置时根据模型名自动推断。
	// 注意：模型名含 vision/multimodal 会自动推断为 multi_modal，若代理不支持该端点需显式设为 text。
	apiTypeCfg, _ := g.Cfg().Get(ctx, "doubao_embedding_model.api_type")
	apiTypeSrc := apiTypeCfg.String()
	if strings.TrimSpace(apiTypeSrc) == "" {
		apiTypeSrc = strings.TrimSpace(os.Getenv("DOUBAO_EMBEDDING_MODEL_API_TYPE"))
	}
	apiType, err := resolveAPIType(model, apiTypeSrc)
	if err != nil {
		return nil, err
	}

	return newArkEmbedder(model, apiKey, baseURL, apiType)
}

func readEmbeddingSetting(ctx context.Context, configKey, envKey string) string {
	if value, err := g.Cfg().Get(ctx, configKey); err == nil && value != nil {
		if text := strings.TrimSpace(value.String()); text != "" {
			return text
		}
	}
	return strings.TrimSpace(os.Getenv(envKey))
}

func newArkEmbedder(model, apiKey, baseURL string, apiType ark.APIType) (embedding.Embedder, error) {
	if model == "" {
		return nil, fmt.Errorf("empty model")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("empty apiKey")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("empty baseURL")
	}

	embedder, err := ark.NewEmbedder(context.Background(), &ark.EmbeddingConfig{
		APIKey:  apiKey,
		Model:   model,
		BaseURL: baseURL,
		APIType: &apiType,
	})
	if err != nil {
		return nil, fmt.Errorf("new ark embedder failed: %w", err)
	}
	return embedder, nil
}

func resolveAPIType(model, cfgValue string) (ark.APIType, error) {
	lowerModel := strings.ToLower(strings.TrimSpace(model))
	lowerCfg := strings.ToLower(strings.TrimSpace(cfgValue))

	switch lowerCfg {
	case "", "auto":
		if strings.Contains(lowerModel, "vision") || strings.Contains(lowerModel, "multimodal") {
			return ark.APITypeMultiModal, nil
		}
		return ark.APITypeText, nil
	case "text", "text_api", "embedding", "embeddings":
		return ark.APITypeText, nil
	case "multi_modal", "multimodal", "multi_modal_api", "vision":
		return ark.APITypeMultiModal, nil
	default:
		return "", fmt.Errorf("invalid doubao_embedding_model.api_type: %s", cfgValue)
	}
}
