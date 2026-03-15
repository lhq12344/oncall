package models

import (
	"context"
	"errors"
	"io"
	"net/http"
	"time"

	"github.com/cloudwego/eino-ext/components/model/openai"
	"github.com/cloudwego/eino/components/model"
	"github.com/gogf/gf/v2/frame/g"
)

const (
	defaultChatModelTimeout       = 90 * time.Second
	defaultChatModelMaxRetries    = 2
	defaultChatModelRetryInterval = 800 * time.Millisecond
)

// OpenAIForDeepSeekV3Quick 创建默认的 OpenAI 兼容聊天模型客户端。
// 输入：ctx。
// 输出：带超时与瞬时故障重试能力的 ToolCallingChatModel。
func OpenAIForDeepSeekV3Quick(ctx context.Context) (cm model.ToolCallingChatModel, err error) {
	model, err := g.Cfg().Get(ctx, "ds_quick_chat_model.model")
	if err != nil {
		return nil, err
	}
	api_key, err := g.Cfg().Get(ctx, "ds_quick_chat_model.api_key")
	if err != nil {
		return nil, err
	}
	base_url, err := g.Cfg().Get(ctx, "ds_quick_chat_model.base_url")
	if err != nil {
		return nil, err
	}
	config := &openai.ChatModelConfig{
		Model:      model.String(),
		APIKey:     api_key.String(),
		BaseURL:    base_url.String(),
		Timeout:    defaultChatModelTimeout,
		HTTPClient: newRetryHTTPClient(defaultChatModelTimeout),
	}
	cm, err = openai.NewChatModel(ctx, config)
	if err != nil {
		return nil, err
	}
	return cm, nil
}

// ChatModel 封装 LLM 客户端
type ChatModel struct {
	Client model.ToolCallingChatModel
}

// GetChatModel 获取默认的 ChatModel
func GetChatModel() (*ChatModel, error) {
	ctx := context.Background()
	client, err := OpenAIForDeepSeekV3Quick(ctx)
	if err != nil {
		return nil, err
	}
	return &ChatModel{Client: client}, nil
}

// newRetryHTTPClient 创建带有限次退避重试能力的 HTTP 客户端。
// 输入：请求超时时间。
// 输出：用于 OpenAI 兼容接口调用的 HTTPClient。
func newRetryHTTPClient(timeout time.Duration) *http.Client {
	return &http.Client{
		Timeout: timeout,
		Transport: &retryRoundTripper{
			base:        http.DefaultTransport,
			maxRetries:  defaultChatModelMaxRetries,
			baseBackoff: defaultChatModelRetryInterval,
		},
	}
}

// retryRoundTripper 在上游模型服务出现瞬时故障时做有限次重试。
type retryRoundTripper struct {
	base        http.RoundTripper
	maxRetries  int
	baseBackoff time.Duration
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
