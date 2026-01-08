package embedder

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/volcengine/volcengine-go-sdk/service/arkruntime"
	arkmodel "github.com/volcengine/volcengine-go-sdk/service/arkruntime/model"
)

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
	// 可选维度（没有就用 0）
	dimV, _ := g.Cfg().Get(ctx, "doubao_embedding_model.dimensions")
	dim := 0
	if dimV != nil {
		// 按你的配置系统类型做转换，这里示例：
		if dimV.Int() > 0 {
			dim = int(dimV.Int())
		}
	}
	return NewDoubaoMultimodalEmbedder(model.String(), apiKey.String(), baseURL.String(), dim)
}

// 确保实现 Eino 的 embedding.Embedder 接口（核心方法：EmbedStrings）
// Eino Embedder 形态：EmbedStrings(ctx, []string) -> [][]float64
type DoubaoMultimodalEmbedder struct {
	client     *arkruntime.Client
	model      string
	dimensions int // 0 表示不传，让服务端默认
	retry      int
}

// EmbedStrings：把每个文本 chunk 走一次 CreateMultiModalEmbeddings（multimodal endpoint 一次只返回一条 embedding）
func (e *DoubaoMultimodalEmbedder) EmbedStrings(
	ctx context.Context,
	texts []string,
	_ ...embedding.Option,
) ([][]float64, error) {
	out := make([][]float64, len(texts)) // 关键：长度固定

	for i, t := range texts {
		// 1) 空文本处理：两种策略二选一
		// 策略 A（推荐）：直接报错，保证数据质量
		if strings.TrimSpace(t) == "" {
			return nil, fmt.Errorf("empty text at index=%d", i)
		}

		vec, err := e.embedOne(ctx, t)
		if err != nil {
			return nil, fmt.Errorf("embed failed at index=%d: %w", i, err)
		}
		if len(vec) == 0 {
			return nil, fmt.Errorf("empty embedding returned at index=%d", i)
		}

		out[i] = vec
	}

	// 2) 再做一次硬校验（防御式编程）
	if len(out) != len(texts) {
		return nil, fmt.Errorf("embedding length mismatch: in=%d out=%d", len(texts), len(out))
	}
	for i := range out {
		if out[i] == nil || len(out[i]) == 0 {
			return nil, fmt.Errorf("nil/empty embedding at index=%d", i)
		}
	}

	return out, nil
}

func (e *DoubaoMultimodalEmbedder) embedOne(ctx context.Context, text string) ([]float64, error) {
	// SDK 请求结构来自 volcengine-go-sdk：
	// MultiModalEmbeddingRequest{ Model, Dimensions, Input: []MultimodalEmbeddingInput{ {Type:"text", Text:*string} } }
	// :contentReference[oaicite:4]{index=4}
	req := arkmodel.MultiModalEmbeddingRequest{
		Model: e.model,
		Input: []arkmodel.MultimodalEmbeddingInput{
			{
				Type: arkmodel.MultiModalEmbeddingInputTypeText,
				Text: &text,
			},
		},
	}
	if e.dimensions > 0 {
		dim := e.dimensions
		req.Dimensions = &dim
	}

	var lastErr error
	for attempt := 0; attempt <= e.retry; attempt++ {
		resp, err := e.client.CreateMultiModalEmbeddings(ctx, req)
		if err == nil {
			// resp.Data.Embedding 是 []float32 :contentReference[oaicite:5]{index=5}
			f32 := resp.Data.Embedding
			vec := make([]float64, len(f32))
			for i := range f32 {
				vec[i] = float64(f32[i])
			}
			return vec, nil
		}

		lastErr = err
		// 简单退避（避免 500 抖动时瞬间打爆）
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-time.After(time.Duration(200*(attempt+1)) * time.Millisecond):
		}
	}
	return nil, lastErr
}

func NewDoubaoMultimodalEmbedder(model, apiKey, baseURL string, dimensions int) (embedding.Embedder, error) {
	if model == "" {
		return nil, fmt.Errorf("empty model")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("empty apiKey")
	}
	if baseURL == "" {
		return nil, fmt.Errorf("empty baseURL")
	}

	// arkruntime.WithBaseUrl 用于指定方舟数据面 Base URL（带 /api/v3/）:contentReference[oaicite:6]{index=6}
	cli := arkruntime.NewClientWithApiKey(apiKey, arkruntime.WithBaseUrl(baseURL))

	return &DoubaoMultimodalEmbedder{
		client:     cli,
		model:      model,
		dimensions: dimensions,
		retry:      2, // 至少重试 2 次
	}, nil
}
