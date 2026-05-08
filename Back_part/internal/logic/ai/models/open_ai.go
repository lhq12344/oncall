package models

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
)

const (
	defaultChatModelTimeout       = 90 * time.Second
	defaultChatModelMaxRetries    = 2
	defaultChatModelRetryInterval = 800 * time.Millisecond
)

// 创建默认的 OpenAI 兼容聊天模型客户端。
// 输入：ctx。
// 输出：带超时与瞬时故障重试能力的 ToolCallingChatModel。
func OpenAIForDeepSeekV3Quick(ctx context.Context) (cm model.ToolCallingChatModel, err error) {
	modelName := readChatModelSetting(ctx, "ds_quick_chat_model.model", "DS_QUICK_CHAT_MODEL_MODEL")
	apiKey := readChatModelSetting(ctx, "ds_quick_chat_model.api_key", "DS_QUICK_CHAT_MODEL_API_KEY")
	baseURL := readChatModelSetting(ctx, "ds_quick_chat_model.base_url", "DS_QUICK_CHAT_MODEL_BASE_URL")
	apiKeyHeader := readChatModelSetting(ctx, "ds_quick_chat_model.api_key_header", "DS_QUICK_CHAT_MODEL_API_KEY_HEADER")
	if modelName == "" || apiKey == "" || baseURL == "" {
		return nil, errors.New("ds_quick_chat_model config incomplete; set config.yaml or DS_QUICK_CHAT_MODEL_* env vars")
	}
	config := &openai.ChatModelConfig{
		Model:      modelName,
		APIKey:     apiKey,
		BaseURL:    baseURL,
		Timeout:    defaultChatModelTimeout,
		HTTPClient: newRetryHTTPClient(defaultChatModelTimeout, apiKeyHeader, apiKey),
	}
	cm, err = openai.NewChatModel(ctx, config)
	if err != nil {
		return nil, err
	}
	return newSanitizingToolCallingChatModel(cm), nil
}

func readChatModelSetting(ctx context.Context, configKey, envKey string) string {
	if value, err := g.Cfg().Get(ctx, configKey); err == nil && value != nil {
		if text := strings.TrimSpace(value.String()); text != "" {
			return text
		}
	}
	return strings.TrimSpace(os.Getenv(envKey))
}

// ChatModel 封装 LLM 客户端
type ChatModel struct {
	Client model.ToolCallingChatModel
}

// GetChatModel 获取默认的 ChatModel
func GetChatModel() (*ChatModel, error) {
	ctx := context.Background()
	client, err := OpenAIForDeepSeekV3Quick(ctx) //返回一个chatmodel
	if err != nil {
		return nil, err
	}
	return &ChatModel{Client: client}, nil
}

// GetChatModelForRole 按角色获取专用 ChatModel，未配置时降级到默认模型。
// role 取值：gate、subgraph、complex。
// 对应配置键前缀：ds_{role}_model.{field} / DS_{ROLE}_MODEL_{FIELD}。
func GetChatModelForRole(role string) (*ChatModel, error) {
	ctx := context.Background()
	prefix := "ds_" + role + "_model"
	envPrefix := "DS_" + strings.ToUpper(role) + "_MODEL"

	modelName := readChatModelSetting(ctx, prefix+".model", envPrefix+"_MODEL")
	apiKey := readChatModelSetting(ctx, prefix+".api_key", envPrefix+"_API_KEY")
	baseURL := readChatModelSetting(ctx, prefix+".base_url", envPrefix+"_BASE_URL")
	apiKeyHeader := readChatModelSetting(ctx, prefix+".api_key_header", envPrefix+"_API_KEY_HEADER")

	if modelName == "" || apiKey == "" || baseURL == "" {
		return GetChatModel()
	}

	config := &openai.ChatModelConfig{
		Model:      modelName,
		APIKey:     apiKey,
		BaseURL:    baseURL,
		Timeout:    defaultChatModelTimeout,
		HTTPClient: newRetryHTTPClient(defaultChatModelTimeout, apiKeyHeader, apiKey),
	}
	client, err := openai.NewChatModel(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("failed to create chat model for role %s: %w", role, err)
	}
	return &ChatModel{Client: newSanitizingToolCallingChatModel(client)}, nil
}

type sanitizingToolCallingChatModel struct {
	base model.ToolCallingChatModel
}

func newSanitizingToolCallingChatModel(base model.ToolCallingChatModel) model.ToolCallingChatModel {
	if base == nil {
		return nil
	}
	if _, ok := base.(*sanitizingToolCallingChatModel); ok {
		return base
	}
	return &sanitizingToolCallingChatModel{base: base}
}

func (m *sanitizingToolCallingChatModel) Generate(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.Message, error) {
	return m.base.Generate(ctx, sanitizeModelInputToolCallArgs(input), opts...)
}

func (m *sanitizingToolCallingChatModel) Stream(ctx context.Context, input []*schema.Message, opts ...model.Option) (*schema.StreamReader[*schema.Message], error) {
	return m.base.Stream(ctx, sanitizeModelInputToolCallArgs(input), opts...)
}

func (m *sanitizingToolCallingChatModel) WithTools(tools []*schema.ToolInfo) (model.ToolCallingChatModel, error) {
	wrapped, err := m.base.WithTools(tools)
	if err != nil {
		return nil, err
	}
	return newSanitizingToolCallingChatModel(wrapped), nil
}

func sanitizeModelInputToolCallArgs(input []*schema.Message) []*schema.Message {
	if len(input) == 0 {
		return input
	}

	var output []*schema.Message
	for i, msg := range input {
		if msg == nil || len(msg.ToolCalls) == 0 {
			if output != nil {
				output[i] = msg
			}
			continue
		}

		var fixedCalls []schema.ToolCall
		changed := false
		for j, tc := range msg.ToolCalls {
			clean := sanitizeToolCallArgumentsJSON(tc.Function.Arguments)
			if clean != tc.Function.Arguments {
				if fixedCalls == nil {
					fixedCalls = make([]schema.ToolCall, len(msg.ToolCalls))
					copy(fixedCalls, msg.ToolCalls)
				}
				fixedCalls[j].Function.Arguments = clean
				changed = true
			}
		}
		if !changed {
			if output != nil {
				output[i] = msg
			}
			continue
		}

		if output == nil {
			output = make([]*schema.Message, len(input))
			copy(output, input[:i])
		}
		msgCopy := *msg
		msgCopy.ToolCalls = fixedCalls
		output[i] = &msgCopy
	}

	if output == nil {
		return input
	}
	return output
}

func sanitizeToolCallArgumentsJSON(args string) string {
	if args == "" {
		return args
	}
	if err := json.Unmarshal([]byte(args), new(any)); err == nil {
		return args
	}

	dec := json.NewDecoder(strings.NewReader(args))
	var first any
	if err := dec.Decode(&first); err != nil {
		return args
	}
	fixed, err := json.Marshal(first)
	if err != nil {
		return args
	}
	return string(fixed)
}

// newRetryHTTPClient 创建带有限次退避重试能力的 HTTP 客户端。
// 输入：请求超时时间。
// 输出：用于 OpenAI 兼容接口调用的 HTTPClient。
func newRetryHTTPClient(timeout time.Duration, apiKeyHeader string, apiKey string) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &retryRoundTripper{
			base:         http.DefaultTransport,
			maxRetries:   defaultChatModelMaxRetries,
			baseBackoff:  defaultChatModelRetryInterval,
			apiKeyHeader: strings.TrimSpace(apiKeyHeader),
			apiKey:       strings.TrimSpace(apiKey),
		},
	}
}

// retryRoundTripper 在上游模型服务出现瞬时故障时做有限次重试。
type retryRoundTripper struct {
	base         http.RoundTripper
	maxRetries   int
	baseBackoff  time.Duration
	apiKeyHeader string
	apiKey       string
}

// RoundTrip 发送 HTTP 请求，并在 502/503/504 或瞬时网络错误时做退避重试。
// 输入：原始 HTTP 请求。
// 输出：响应或错误。
func (r *retryRoundTripper) RoundTrip(req *http.Request) (*http.Response, error) {
	base := r.base
	if base == nil {
		base = http.DefaultTransport
	}

	var lastErr error
	for attempt := 0; attempt <= r.maxRetries; attempt++ {
		currentReq, err := cloneRetryableRequest(req, attempt)
		if err != nil {
			return nil, err
		}
		r.applyAPIKeyHeader(currentReq)

		resp, err := base.RoundTrip(currentReq)
		if !shouldRetryModelRequest(req.Context(), resp, err, attempt, r.maxRetries) {
			return resp, err
		}

		lastErr = err
		if resp != nil && resp.Body != nil {
			_, _ = io.Copy(io.Discard, resp.Body)
			resp.Body.Close()
		}

		if waitErr := waitRetryBackoff(req.Context(), r.baseBackoff, attempt); waitErr != nil {
			if lastErr != nil {
				return nil, lastErr
			}
			return nil, waitErr
		}
	}

	if lastErr != nil {
		return nil, lastErr
	}
	return nil, errors.New("chat model request failed after retries")
}

func (r *retryRoundTripper) applyAPIKeyHeader(req *http.Request) {
	if req == nil || r.apiKeyHeader == "" || r.apiKey == "" {
		return
	}
	req.Header.Set(r.apiKeyHeader, r.apiKey)
	if !strings.EqualFold(r.apiKeyHeader, "Authorization") {
		req.Header.Del("Authorization")
	}
}

// cloneRetryableRequest 为重试创建可复用的请求副本。
// 输入：原始请求、当前尝试次数。
// 输出：可发送的请求副本。
func cloneRetryableRequest(req *http.Request, attempt int) (*http.Request, error) {
	if attempt == 0 {
		return req, nil
	}

	if req.Body == nil {
		return req.Clone(req.Context()), nil
	}
	if req.GetBody == nil {
		return nil, errors.New("request body is not retryable")
	}

	body, err := req.GetBody()
	if err != nil {
		return nil, err
	}
	cloned := req.Clone(req.Context())
	cloned.Body = body
	return cloned, nil
}

// shouldRetryModelRequest 判断当前模型请求是否值得继续重试。
// 输入：ctx、响应、错误、当前尝试次数、最大重试次数。
// 输出：true 表示继续重试；false 表示立即返回。
func shouldRetryModelRequest(ctx context.Context, resp *http.Response, err error, attempt, maxRetries int) bool {
	if attempt >= maxRetries {
		return false
	}
	if ctx.Err() != nil {
		return false
	}
	if err != nil {
		return true
	}
	if resp == nil {
		return false
	}
	switch resp.StatusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	default:
		return false
	}
}

// waitRetryBackoff 按尝试次数做线性退避，并响应上下文取消。
// 输入：ctx、基础退避时间、当前尝试次数。
// 输出：等待完成返回 nil；若上下文结束则返回错误。
func waitRetryBackoff(ctx context.Context, base time.Duration, attempt int) error {
	delay := base * time.Duration(attempt+1)
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}
