package embedder

import (
	"context"
	"fmt"
	"strings"

	"github.com/cloudwego/eino-ext/components/embedding/ark"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/gogf/gf/v2/frame/g"
)

// DoubaoEmbedding 从配置构建 Ark Embedder。
// 输入：配置项 doubao_embedding_model.{model,api_key,base_url,api_type}
// 输出：符合 Eino embedding.Embedder 接口的实例。
func DoubaoEmbedding(ctx context.Context) (embedding.Embedder, error) {
	model, err := g.Cfg().Get(ctx, "doubao_embedding_model.model")
	if err != nil {
		return nil, err
	}
	apiKey, err := g.Cfg().Get(ctx, "doubao_embedding_model.api_key")
	if err != nil {
		return nil, err
	}
	baseURL, err := g.Cfg().Get(ctx, "doubao_embedding_model.base_url")
	if err != nil {
		return nil, err
	}

	// 可选配置：api_type=text|multi_modal。未配置时根据模型名自动推断。
	apiTypeCfg, _ := g.Cfg().Get(ctx, "doubao_embedding_model.api_type")
	apiType, err := resolveAPIType(model.String(), apiTypeCfg.String())
	if err != nil {
		return nil, err
	}

	return newArkEmbedder(model.String(), apiKey.String(), baseURL.String(), apiType)
}

// NewCustomDoubaoEmbedder 为兼容旧调用保留，行为同 NewDoubaoMultimodalEmbedder。
// 输入：模型、鉴权、服务地址、维度（当前 Ark 组件不使用 dimensions 参数）。
// 输出：Embedder。
func NewCustomDoubaoEmbedder(model, apiKey, baseURL string, dimensions int) (embedding.Embedder, error) {
	return NewDoubaoMultimodalEmbedder(model, apiKey, baseURL, dimensions)
}

// NewDoubaoMultimodalEmbedder 创建 Ark Embedder。
// 输入：模型、鉴权、服务地址、维度（当前 Ark 组件不使用 dimensions 参数）。
// 输出：Embedder。
func NewDoubaoMultimodalEmbedder(model, apiKey, baseURL string, dimensions int) (embedding.Embedder, error) {
	_ = dimensions
	apiType, err := resolveAPIType(model, "")
	if err != nil {
		return nil, err
	}
	return newArkEmbedder(model, apiKey, baseURL, apiType)
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
