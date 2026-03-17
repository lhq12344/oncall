package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/gogf/gf/v2/frame/g"
	"go.uber.org/zap"
)

const (
	defaultWebSearchTimeout = 10 * time.Second
	defaultWebSearchTopK    = 5
	maxWebSearchTopK        = 8

	webSearchProviderAuto    = "auto"
	webSearchProviderSerper  = "serper"
	webSearchProviderSearXNG = "searxng"
	webSearchProviderGoogle  = "google"

	serperSearchURL = "https://google.serper.dev/search"
)

// WebSearchTool 为 dialogue_agent 提供通用外部网络检索能力。
// 默认优先使用 Serper.dev，若未配置则回退到 SearXNG。
// 当搜索服务不可用时，返回结构化降级结果而不是抛错中断对话。
type WebSearchTool struct {
	httpClient *http.Client
	logger     *zap.Logger
}

type webSearchConfig struct {
	Provider       string
	SerperAPIKey   string
	SerperBaseURL  string
	SearXNGBaseURL string
}

// webSearchResultItem 定义单条网络检索结果。
type webSearchResultItem struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Summary string `json:"summary"`
}

// NewWebSearchTool 创建通用网络搜索工具。
// 输入：logger（日志）。
// 输出：可注册到 dialogue_agent 的 tool.BaseTool。
func NewWebSearchTool(logger *zap.Logger) tool.BaseTool {
	return &WebSearchTool{
		httpClient: &http.Client{Timeout: defaultWebSearchTimeout},
		logger:     logger,
	}
}

func (t *WebSearchTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "web_search",
		Desc: "搜索互联网最新公开信息、官方文档和报错线索。优先使用 Serper.dev，可配置回退到 SearXNG；网络异常时返回结构化降级结果。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "检索关键词或问题描述",
				Required: true,
			},
			"time_range": {
				Type:     schema.String,
				Desc:     "可选时间范围：d（近一天）/w（近一周）/m（近一月）/y（近一年）",
				Required: false,
			},
			"top_k": {
				Type:     schema.Integer,
				Desc:     "可选结果数，默认 5，最大 8",
				Required: false,
			},
		}),
	}, nil
}

func (t *WebSearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Query     string `json:"query"`
		TimeRange string `json:"time_range"`
		TopK      int    `json:"top_k"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Query = strings.TrimSpace(in.Query)
	in.TimeRange = strings.ToLower(strings.TrimSpace(in.TimeRange))
	if in.Query == "" {
		return "", fmt.Errorf("query is required")
	}
	if in.TopK <= 0 {
		in.TopK = defaultWebSearchTopK
	}
	if in.TopK > maxWebSearchTopK {
		in.TopK = maxWebSearchTopK
	}

	cfg := loadWebSearchConfig(ctx)
	provider, cfgErr := resolveWebSearchProvider(cfg)
	if cfgErr != nil {
		return t.degradedResponse(in.Query, in.TimeRange, "", cfgErr.Error())
	}

	switch provider {
	case webSearchProviderSerper:
		return t.searchWithSerper(ctx, cfg, in.Query, in.TimeRange, in.TopK)
	case webSearchProviderSearXNG:
		return t.searchWithSearXNG(ctx, cfg, in.Query, in.TimeRange, in.TopK)
	default:
		return t.degradedResponse(in.Query, in.TimeRange, provider, "no available web search provider configured")
	}
}

type serperSearchResponse struct {
	Organic []struct {
		Title   string `json:"title"`
		Link    string `json:"link"`
		Snippet string `json:"snippet"`
	} `json:"organic"`
	AnswerBox *struct {
		Title   string `json:"title"`
		Link    string `json:"link"`
		Answer  string `json:"answer"`
		Snippet string `json:"snippet"`
	} `json:"answerBox"`
	KnowledgeGraph *struct {
		Title       string `json:"title"`
		Description string `json:"description"`
	} `json:"knowledgeGraph"`
	Message string `json:"message"`
}

// searchWithSerper 使用 Serper.dev Search API 执行检索。
// 优先读取 web_search.* 配置，未配置时回退到环境变量。
func (t *WebSearchTool) searchWithSerper(ctx context.Context, cfg webSearchConfig, query, timeRange string, topK int) (string, error) {
	apiKey := strings.TrimSpace(cfg.SerperAPIKey)
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.SerperBaseURL), "/")
	if apiKey == "" {
		return t.degradedResponse(query, timeRange, webSearchProviderSerper, "missing web_search.serper_api_key or SERPER_API_KEY")
	}
	if baseURL == "" {
		baseURL = serperSearchURL
	}

	payload := map[string]any{
		"q":   query,
		"num": topK,
	}
	if tbs := normalizeSerperTimeRange(timeRange); tbs != "" {
		payload["tbs"] = tbs
	}
	reqBody, err := json.Marshal(payload)
	if err != nil {
		return t.degradedResponse(query, timeRange, webSearchProviderSerper, err.Error())
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, baseURL, strings.NewReader(string(reqBody)))
	if err != nil {
		return t.degradedResponse(query, timeRange, webSearchProviderSerper, err.Error())
	}
	req.Header.Set("X-API-KEY", apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := t.httpClient.Do(req)
	if err != nil {
		if t.logger != nil {
			t.logger.Warn("serper web search degraded",
				zap.String("query", query),
				zap.String("time_range", timeRange),
				zap.Error(err))
		}
		return t.degradedResponse(query, timeRange, webSearchProviderSerper, err.Error())
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return t.degradedResponse(query, timeRange, webSearchProviderSerper, err.Error())
	}

	var payloadResp serperSearchResponse
	if err := json.Unmarshal(body, &payloadResp); err != nil {
		return t.degradedResponse(query, timeRange, webSearchProviderSerper, err.Error())
	}
	if resp.StatusCode != http.StatusOK {
		errText := strings.TrimSpace(string(body))
		if strings.TrimSpace(payloadResp.Message) != "" {
			errText = payloadResp.Message
		}
		return t.degradedResponse(query, timeRange, webSearchProviderSerper, errText)
	}

	items := make([]webSearchResultItem, 0, topK)
	if payloadResp.AnswerBox != nil {
		summary := strings.TrimSpace(payloadResp.AnswerBox.Answer)
		if summary == "" {
			summary = strings.TrimSpace(payloadResp.AnswerBox.Snippet)
		}
		appendWebSearchItem(&items, webSearchResultItem{
			Title:   strings.TrimSpace(payloadResp.AnswerBox.Title),
			URL:     strings.TrimSpace(payloadResp.AnswerBox.Link),
			Summary: clipWebSearchText(summary, 280),
		}, topK)
	}
	if payloadResp.KnowledgeGraph != nil {
		appendWebSearchItem(&items, webSearchResultItem{
			Title:   strings.TrimSpace(payloadResp.KnowledgeGraph.Title),
			Summary: clipWebSearchText(strings.TrimSpace(payloadResp.KnowledgeGraph.Description), 280),
		}, topK)
	}
	for _, item := range payloadResp.Organic {
		if len(items) >= topK {
			break
		}
		items = append(items, webSearchResultItem{
			Title:   strings.TrimSpace(item.Title),
			URL:     strings.TrimSpace(item.Link),
			Summary: clipWebSearchText(strings.TrimSpace(item.Snippet), 280),
		})
	}

	return marshalWebSearchResponse(map[string]any{
		"status":     "success",
		"degraded":   false,
		"provider":   webSearchProviderSerper,
		"query":      query,
		"time_range": timeRange,
		"count":      len(items),
		"message":    fmt.Sprintf("serper search returned %d results", len(items)),
		"results":    items,
	})
}

type searXNGResponse struct {
	Results []struct {
		Title   string `json:"title"`
		URL     string `json:"url"`
		Content string `json:"content"`
	} `json:"results"`
}

// searchWithSearXNG 使用 SearXNG JSON 接口执行检索。
// 优先读取 web_search.* 配置，未配置时回退到环境变量。
func (t *WebSearchTool) searchWithSearXNG(ctx context.Context, cfg webSearchConfig, query, timeRange string, topK int) (string, error) {
	baseURL := strings.TrimRight(strings.TrimSpace(cfg.SearXNGBaseURL), "/")
	if baseURL == "" {
		return t.degradedResponse(query, timeRange, webSearchProviderSearXNG, "missing web_search.searxng_base_url or SEARXNG_BASE_URL")
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("format", "json")
	params.Set("language", "zh-CN")
	if strings.TrimSpace(timeRange) != "" {
		params.Set("time_range", strings.TrimSpace(timeRange))
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, baseURL+"/search?"+params.Encode(), nil)
	if err != nil {
		return t.degradedResponse(query, timeRange, webSearchProviderSearXNG, err.Error())
	}

	resp, err := t.httpClient.Do(req)
	if err != nil {
		if t.logger != nil {
			t.logger.Warn("searxng web search degraded",
				zap.String("query", query),
				zap.String("time_range", timeRange),
				zap.Error(err))
		}
		return t.degradedResponse(query, timeRange, webSearchProviderSearXNG, err.Error())
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return t.degradedResponse(query, timeRange, webSearchProviderSearXNG, err.Error())
	}
	if resp.StatusCode != http.StatusOK {
		return t.degradedResponse(query, timeRange, webSearchProviderSearXNG, string(body))
	}

	var payload searXNGResponse
	if err := json.Unmarshal(body, &payload); err != nil {
		return t.degradedResponse(query, timeRange, webSearchProviderSearXNG, err.Error())
	}

	items := make([]webSearchResultItem, 0, topK)
	for _, item := range payload.Results {
		if len(items) >= topK {
			break
		}
		items = append(items, webSearchResultItem{
			Title:   strings.TrimSpace(item.Title),
			URL:     strings.TrimSpace(item.URL),
			Summary: clipWebSearchText(strings.TrimSpace(item.Content), 280),
		})
	}

	return marshalWebSearchResponse(map[string]any{
		"status":     "success",
		"degraded":   false,
		"provider":   webSearchProviderSearXNG,
		"query":      query,
		"time_range": timeRange,
		"count":      len(items),
		"message":    fmt.Sprintf("searxng search returned %d results", len(items)),
		"results":    items,
	})
}

// degradedResponse 返回网络检索降级结果，避免工具错误中断整个 Agent 流程。
func (t *WebSearchTool) degradedResponse(query, timeRange, provider, errText string) (string, error) {
	return marshalWebSearchResponse(map[string]any{
		"status":     "degraded",
		"degraded":   true,
		"provider":   strings.TrimSpace(provider),
		"query":      strings.TrimSpace(query),
		"time_range": strings.TrimSpace(timeRange),
		"count":      0,
		"results":    []webSearchResultItem{},
		"message":    "外部网络检索暂时不可用，请稍后重试或优先使用内部观测/知识库结果",
		"error":      clipWebSearchText(strings.TrimSpace(errText), 320),
	})
}

// resolveWebSearchProvider 解析当前可用的网络搜索提供方。
// 优先级：
// 1. web_search.provider，未配置时回退 WEB_SEARCH_PROVIDER；
// 2. auto：优先 Serper.dev，次选 SearXNG。
func resolveWebSearchProvider(cfg webSearchConfig) (string, error) {
	provider := strings.ToLower(strings.TrimSpace(cfg.Provider))
	switch provider {
	case "", webSearchProviderAuto:
		if hasSerperConfig(cfg) {
			return webSearchProviderSerper, nil
		}
		if hasSearXNGConfig(cfg) {
			return webSearchProviderSearXNG, nil
		}
		return "", fmt.Errorf("no web search provider configured: set web_search.serper_api_key or SERPER_API_KEY, or web_search.searxng_base_url or SEARXNG_BASE_URL")
	case webSearchProviderSerper:
		if !hasSerperConfig(cfg) {
			return "", fmt.Errorf("web_search.provider=serper but web_search.serper_api_key or SERPER_API_KEY is missing")
		}
		return webSearchProviderSerper, nil
	case webSearchProviderGoogle:
		if !hasSerperConfig(cfg) {
			return "", fmt.Errorf("web_search.provider=google is deprecated; set web_search.serper_api_key or SERPER_API_KEY and use serper")
		}
		return webSearchProviderSerper, nil
	case webSearchProviderSearXNG:
		if !hasSearXNGConfig(cfg) {
			return "", fmt.Errorf("web_search.provider=searxng but web_search.searxng_base_url or SEARXNG_BASE_URL is missing")
		}
		return webSearchProviderSearXNG, nil
	default:
		return "", fmt.Errorf("unsupported web_search.provider: %s", provider)
	}
}

func loadWebSearchConfig(ctx context.Context) webSearchConfig {
	cfgValues := map[string]string{
		"web_search.provider":         readWebSearchConfigString(ctx, "web_search.provider"),
		"web_search.serper_api_key":   readWebSearchConfigString(ctx, "web_search.serper_api_key"),
		"web_search.serper_base_url":  readWebSearchConfigString(ctx, "web_search.serper_base_url"),
		"web_search.searxng_base_url": readWebSearchConfigString(ctx, "web_search.searxng_base_url"),
	}
	return loadWebSearchConfigFromSources(cfgValues, os.Getenv)
}

func loadWebSearchConfigFromSources(cfgValues map[string]string, getenv func(string) string) webSearchConfig {
	if getenv == nil {
		getenv = func(string) string { return "" }
	}
	return webSearchConfig{
		Provider: firstNonEmpty(
			cfgValues["web_search.provider"],
			getenv("WEB_SEARCH_PROVIDER"),
		),
		SerperAPIKey: firstNonEmpty(
			cfgValues["web_search.serper_api_key"],
			getenv("SERPER_API_KEY"),
		),
		SerperBaseURL: firstNonEmpty(
			cfgValues["web_search.serper_base_url"],
			getenv("SERPER_BASE_URL"),
		),
		SearXNGBaseURL: firstNonEmpty(
			cfgValues["web_search.searxng_base_url"],
			getenv("SEARXNG_BASE_URL"),
		),
	}
}

func readWebSearchConfigString(ctx context.Context, key string) string {
	value, err := g.Cfg().Get(ctx, key)
	if err != nil || value == nil {
		return ""
	}
	return strings.TrimSpace(value.String())
}

func hasSerperConfig(cfg webSearchConfig) bool {
	return strings.TrimSpace(cfg.SerperAPIKey) != ""
}

func hasSearXNGConfig(cfg webSearchConfig) bool {
	return strings.TrimSpace(cfg.SearXNGBaseURL) != ""
}

func firstNonEmpty(values ...string) string {
	for _, value := range values {
		value = strings.TrimSpace(value)
		if value != "" {
			return value
		}
	}
	return ""
}

// normalizeSerperTimeRange 将 d/w/m/y 转为 Serper tbs 参数。
func normalizeSerperTimeRange(timeRange string) string {
	switch strings.ToLower(strings.TrimSpace(timeRange)) {
	case "d":
		return "qdr:d"
	case "w":
		return "qdr:w"
	case "m":
		return "qdr:m"
	case "y":
		return "qdr:y"
	default:
		return ""
	}
}

// appendWebSearchItem 在结果有效且未超限时追加搜索条目。
// 输入：items 为结果集合指针，item 为候选结果，limit 为最大数量。
// 输出：无。
func appendWebSearchItem(items *[]webSearchResultItem, item webSearchResultItem, limit int) {
	if items == nil || len(*items) >= limit {
		return
	}
	item.Title = strings.TrimSpace(item.Title)
	item.URL = strings.TrimSpace(item.URL)
	item.Summary = strings.TrimSpace(item.Summary)
	if item.Title == "" && item.Summary == "" {
		return
	}
	*items = append(*items, item)
}

func marshalWebSearchResponse(payload map[string]any) (string, error) {
	body, err := json.Marshal(payload)
	if err != nil {
		return "", fmt.Errorf("failed to marshal web search result: %w", err)
	}
	return string(body), nil
}

func clipWebSearchText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if maxRunes <= 0 {
		maxRunes = 280
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}
