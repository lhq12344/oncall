package tokenizer

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

// CountTextTokens 使用 DeepSeek Tokenization API 精确计算文本 token 数。
func CountTextTokens(ctx context.Context, text string) (int, error) {
	if strings.TrimSpace(text) == "" {
		return 0, nil
	}

	apiKey, baseURL, model, err := loadTokenizationConfig(ctx)
	if err != nil {
		return 0, err
	}

	body := map[string]interface{}{
		"model": model,
		"text":  text,
	}
	payload, err := json.Marshal(body)
	if err != nil {
		return 0, fmt.Errorf("marshal tokenization payload: %w", err)
	}

	endpoints := buildTokenizationEndpoints(baseURL)
	var lastErr error

	for _, endpoint := range endpoints {
		tokens, callErr := callTokenizationEndpoint(ctx, endpoint, apiKey, payload)
		if callErr == nil {
			return tokens, nil
		}
		lastErr = callErr
	}

	if lastErr == nil {
		lastErr = fmt.Errorf("tokenization endpoint not available")
	}
	return 0, lastErr
}

// CountMessageTokens 计算单条消息 token（按照消息 footprint 序列化后计数）。
func CountMessageTokens(ctx context.Context, msg *schema.Message, includeReasoning bool) (int, error) {
	if msg == nil {
		return 0, nil
	}

	text, err := buildMessageFootprintJSON(msg, includeReasoning)
	if err != nil {
		return 0, err
	}

	return CountTextTokens(ctx, text)
}

// CountMessagesTokens 计算多条消息 token 总数。
func CountMessagesTokens(ctx context.Context, msgs []*schema.Message, includeReasoning bool) (int, error) {
	total := 0
	for _, msg := range msgs {
		t, err := CountMessageTokens(ctx, msg, includeReasoning)
		if err != nil {
			return 0, err
		}
		total += t
	}
	return total, nil
}

func loadTokenizationConfig(ctx context.Context) (apiKey string, baseURL string, model string, err error) {
	apiKeyV, err := g.Cfg().Get(ctx, "ds_quick_chat_model.api_key")
	if err != nil {
		return "", "", "", fmt.Errorf("read ds_quick_chat_model.api_key: %w", err)
	}
	baseURLV, err := g.Cfg().Get(ctx, "ds_quick_chat_model.base_url")
	if err != nil {
		return "", "", "", fmt.Errorf("read ds_quick_chat_model.base_url: %w", err)
	}
	modelV, err := g.Cfg().Get(ctx, "ds_quick_chat_model.model")
	if err != nil {
		return "", "", "", fmt.Errorf("read ds_quick_chat_model.model: %w", err)
	}

	apiKey = strings.TrimSpace(apiKeyV.String())
	baseURL = strings.TrimSpace(baseURLV.String())
	model = strings.TrimSpace(modelV.String())

	if apiKey == "" || baseURL == "" || model == "" {
		return "", "", "", fmt.Errorf("tokenization config incomplete")
	}

	return apiKey, baseURL, model, nil
}

func buildTokenizationEndpoints(baseURL string) []string {
	baseURL = strings.TrimRight(strings.TrimSpace(baseURL), "/")
	if baseURL == "" {
		return nil
	}

	seen := map[string]struct{}{}
	endpoints := make([]string, 0, 3)
	add := func(u string) {
		u = strings.TrimSpace(u)
		if u == "" {
			return
		}
		if _, ok := seen[u]; ok {
			return
		}
		seen[u] = struct{}{}
		endpoints = append(endpoints, u)
	}

	add(baseURL + "/tokenization")

	parsed, err := url.Parse(baseURL)
	if err != nil {
		return endpoints
	}
	if parsed.Path != "" {
		root := *parsed
		root.Path = ""
		root.RawPath = ""
		add(strings.TrimRight(root.String(), "/") + "/v3/tokenization")
		add(strings.TrimRight(root.String(), "/") + "/api/v3/tokenization")
	}

	return endpoints
}

func callTokenizationEndpoint(ctx context.Context, endpoint, apiKey string, payload []byte) (int, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(payload))
	if err != nil {
		return 0, fmt.Errorf("create request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+apiKey)
	req.Header.Set("Content-Type", "application/json")

	cli := &http.Client{Timeout: 8 * time.Second}
	resp, err := cli.Do(req)
	if err != nil {
		return 0, fmt.Errorf("request failed: %w", err)
	}
	defer resp.Body.Close()

	respBody, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return 0, fmt.Errorf("http %d", resp.StatusCode)
	}

	tokens, err := extractTokenCount(respBody)
	if err != nil {
		return 0, err
	}
	if tokens < 0 {
		return 0, fmt.Errorf("invalid token count: %d", tokens)
	}
	return tokens, nil
}

func extractTokenCount(respBody []byte) (int, error) {
	var payload map[string]interface{}
	if err := json.Unmarshal(respBody, &payload); err != nil {
		return 0, fmt.Errorf("parse response: %w", err)
	}

	if usage, ok := payload["usage"].(map[string]interface{}); ok {
		if total := toInt(usage["total_tokens"]); total >= 0 {
			return total, nil
		}
	}

	if data, ok := payload["data"].([]interface{}); ok && len(data) > 0 {
		if first, ok := data[0].(map[string]interface{}); ok {
			if total := toInt(first["total_tokens"]); total >= 0 {
				return total, nil
			}
			if ids, ok := first["token_ids"].([]interface{}); ok {
				return len(ids), nil
			}
			if toks, ok := first["tokens"].([]interface{}); ok {
				return len(toks), nil
			}
		}
	}

	if ids, ok := payload["token_ids"].([]interface{}); ok {
		return len(ids), nil
	}
	if toks, ok := payload["tokens"].([]interface{}); ok {
		return len(toks), nil
	}

	return 0, fmt.Errorf("token count not found in response")
}

func toInt(v interface{}) int {
	switch x := v.(type) {
	case float64:
		return int(x)
	case float32:
		return int(x)
	case int:
		return x
	case int32:
		return int(x)
	case int64:
		return int(x)
	case string:
		if x == "" {
			return -1
		}
		var parsed int
		if _, err := fmt.Sscanf(x, "%d", &parsed); err == nil {
			return parsed
		}
	}
	return -1
}

func buildMessageFootprintJSON(m *schema.Message, includeReasoning bool) (string, error) {
	type footprint struct {
		Role    schema.RoleType `json:"role"`
		Content string          `json:"content"`

		MultiContent             []schema.ChatMessagePart   `json:"multi_content,omitempty"`
		UserInputMultiContent    []schema.MessageInputPart  `json:"user_input_multi_content,omitempty"`
		AssistantGenMultiContent []schema.MessageOutputPart `json:"assistant_output_multi_content,omitempty"`

		Name string `json:"name,omitempty"`

		ToolCalls  []schema.ToolCall `json:"tool_calls,omitempty"`
		ToolCallID string            `json:"tool_call_id,omitempty"`
		ToolName   string            `json:"tool_name,omitempty"`

		ReasoningContent string `json:"reasoning_content,omitempty"`
	}

	fp := footprint{
		Role:                     m.Role,
		Content:                  m.Content,
		MultiContent:             m.MultiContent,
		UserInputMultiContent:    m.UserInputMultiContent,
		AssistantGenMultiContent: m.AssistantGenMultiContent,
		Name:                     m.Name,
		ToolCalls:                m.ToolCalls,
		ToolCallID:               m.ToolCallID,
		ToolName:                 m.ToolName,
	}
	if includeReasoning {
		fp.ReasoningContent = m.ReasoningContent
	}

	b, err := json.Marshal(fp)
	if err != nil {
		return "", err
	}

	return string(b), nil
}
