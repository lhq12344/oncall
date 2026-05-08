package embedder

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/embedding"
	"github.com/gogf/gf/v2/frame/g"
)

const (
	defaultDashScopeEmbeddingEndpoint = "https://dashscope.aliyuncs.com/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding"
	dashScopeEmbeddingPath            = "/api/v1/services/embeddings/multimodal-embedding/multimodal-embedding"
	defaultDashScopeTimeout           = 30 * time.Second
)

// DoubaoEmbedding keeps the historical entrypoint name while constructing a
// DashScope multimodal embedding client.
// 输入：配置项 doubao_embedding_model.{model,api_key,base_url}
// 输出：符合 Eino embedding.Embedder 接口的实例。
func DoubaoEmbedding(ctx context.Context) (embedding.Embedder, error) {
	model := readEmbeddingSetting(ctx, "doubao_embedding_model.model", "DOUBAO_EMBEDDING_MODEL_MODEL")
	apiKey := readEmbeddingSetting(ctx, "doubao_embedding_model.api_key", "DOUBAO_EMBEDDING_MODEL_API_KEY")
	baseURL := readEmbeddingSetting(ctx, "doubao_embedding_model.base_url", "DOUBAO_EMBEDDING_MODEL_BASE_URL")

	return newDashScopeEmbedder(model, apiKey, baseURL, nil)
}

func readEmbeddingSetting(ctx context.Context, configKey, envKey string) string {
	if value, err := g.Cfg().Get(ctx, configKey); err == nil && value != nil {
		if text := cleanConfigValue(value.String()); text != "" {
			return text
		}
	}
	return cleanConfigValue(os.Getenv(envKey))
}

func cleanConfigValue(value string) string {
	return strings.Trim(strings.TrimSpace(value), `"'`)
}

func newDashScopeEmbedder(model, apiKey, baseURL string, client *http.Client) (embedding.Embedder, error) {
	model = cleanConfigValue(model)
	apiKey = cleanConfigValue(apiKey)
	endpoint := resolveDashScopeEndpoint(baseURL)

	if model == "" {
		return nil, fmt.Errorf("empty model")
	}
	if apiKey == "" {
		return nil, fmt.Errorf("empty apiKey")
	}
	if client == nil {
		client = &http.Client{Timeout: defaultDashScopeTimeout}
	}

	return &dashScopeEmbedder{
		model:    model,
		apiKey:   apiKey,
		endpoint: endpoint,
		client:   client,
	}, nil
}

func resolveDashScopeEndpoint(baseURL string) string {
	baseURL = strings.TrimRight(cleanConfigValue(baseURL), "/")
	if baseURL == "" {
		return defaultDashScopeEmbeddingEndpoint
	}
	if strings.HasSuffix(baseURL, dashScopeEmbeddingPath) {
		return baseURL
	}
	return baseURL + dashScopeEmbeddingPath
}

type dashScopeEmbedder struct {
	model    string
	apiKey   string
	endpoint string
	client   *http.Client
}

func (e *dashScopeEmbedder) EmbedStrings(ctx context.Context, texts []string, opts ...embedding.Option) ([][]float64, error) {
	if len(texts) == 0 {
		return [][]float64{}, nil
	}
	model := e.model
	options := embedding.GetCommonOptions(&embedding.Options{Model: &model}, opts...)
	if options.Model != nil {
		model = cleanConfigValue(*options.Model)
	}
	if model == "" {
		return nil, fmt.Errorf("empty model")
	}

	contents := make([]map[string]string, len(texts))
	for i, text := range texts {
		contents[i] = map[string]string{"text": text}
	}

	payload := dashScopeEmbeddingRequest{
		Model: model,
		Input: dashScopeEmbeddingInput{Contents: contents},
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return nil, fmt.Errorf("marshal dashscope embedding request failed: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, e.endpoint, bytes.NewReader(body))
	if err != nil {
		return nil, fmt.Errorf("create dashscope embedding request failed: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+e.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := e.client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("dashscope embedding request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, err := io.ReadAll(io.LimitReader(resp.Body, 4<<20))
	if err != nil {
		return nil, fmt.Errorf("read dashscope embedding response failed: %w", err)
	}
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return nil, dashScopeHTTPError(resp.StatusCode, respBody)
	}

	var parsed dashScopeEmbeddingResponse
	if err := json.Unmarshal(respBody, &parsed); err != nil {
		return nil, fmt.Errorf("decode dashscope embedding response failed: %w", err)
	}
	return parsed.vectors(len(texts))
}

func dashScopeHTTPError(statusCode int, body []byte) error {
	var parsed dashScopeErrorResponse
	if err := json.Unmarshal(body, &parsed); err == nil {
		if parsed.Code != "" || parsed.Message != "" {
			return fmt.Errorf("dashscope embedding http %d: %s %s", statusCode, parsed.Code, parsed.Message)
		}
		if parsed.Error.Code != "" || parsed.Error.Message != "" {
			return fmt.Errorf("dashscope embedding http %d: %s %s", statusCode, parsed.Error.Code, parsed.Error.Message)
		}
	}

	text := strings.TrimSpace(string(body))
	if len(text) > 500 {
		text = text[:500]
	}
	return fmt.Errorf("dashscope embedding http %d: %s", statusCode, text)
}

type dashScopeEmbeddingRequest struct {
	Model string                  `json:"model"`
	Input dashScopeEmbeddingInput `json:"input"`
}

type dashScopeEmbeddingInput struct {
	Contents []map[string]string `json:"contents"`
}

type dashScopeEmbeddingResponse struct {
	Output struct {
		Embeddings []dashScopeEmbedding `json:"embeddings"`
	} `json:"output"`
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type dashScopeEmbedding struct {
	Index     int       `json:"index"`
	Embedding []float64 `json:"embedding"`
}

func (r dashScopeEmbeddingResponse) vectors(want int) ([][]float64, error) {
	if r.Code != "" || r.Message != "" {
		return nil, fmt.Errorf("dashscope embedding failed: %s %s", r.Code, r.Message)
	}
	if len(r.Output.Embeddings) != want {
		return nil, fmt.Errorf("dashscope embedding result size mismatch: got=%d want=%d", len(r.Output.Embeddings), want)
	}

	vectors := make([][]float64, want)
	seen := make([]bool, want)
	for _, item := range r.Output.Embeddings {
		if item.Index < 0 || item.Index >= want {
			return nil, fmt.Errorf("dashscope embedding index out of range: index=%d want=%d", item.Index, want)
		}
		if seen[item.Index] {
			return nil, fmt.Errorf("dashscope embedding duplicate index: %d", item.Index)
		}
		if len(item.Embedding) == 0 {
			return nil, fmt.Errorf("dashscope embedding empty vector: index=%d", item.Index)
		}
		vectors[item.Index] = item.Embedding
		seen[item.Index] = true
	}
	for i, ok := range seen {
		if !ok {
			return nil, fmt.Errorf("dashscope embedding missing index: %d", i)
		}
	}
	return vectors, nil
}

type dashScopeErrorResponse struct {
	Code    string `json:"code"`
	Message string `json:"message"`
	Error   struct {
		Code    string `json:"code"`
		Message string `json:"message"`
	} `json:"error"`
}
