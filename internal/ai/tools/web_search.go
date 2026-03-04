package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/components/tool/utils"
)

// WebSearchInput 联网搜索的输入参数
type WebSearchInput struct {
	Query string `json:"query" jsonschema:"description=搜索关键词，用于在互联网上搜索相关信息"`
	Count int    `json:"count,omitempty" jsonschema:"description=返回结果数量，默认5条，最多10条"`
}

// WebSearchResult 单条搜索结果
type WebSearchResult struct {
	Title   string `json:"title"`
	URL     string `json:"url"`
	Snippet string `json:"snippet"`
}

// WebSearchOutput 搜索输出
type WebSearchOutput struct {
	Success bool              `json:"success"`
	Query   string            `json:"query"`
	Results []WebSearchResult `json:"results"`
	Message string            `json:"message,omitempty"`
}

// NewWebSearchTool 创建联网搜索工具
// 使用方式：在 config.yaml 中配置 web_search.api_key 和 web_search.endpoint
// 默认使用 Bing Search API，你也可以替换为 Google/SerpAPI/Tavily 等
func NewWebSearchTool() tool.InvokableTool {
	t, err := utils.InferOptionableTool(
		"web_search",
		`Search the internet for real-time information. Use this tool when you need to find up-to-date information, news, technical documentation, or any knowledge that may not be available in internal docs. Returns a list of relevant web results with titles, URLs, and snippets.`,
		func(ctx context.Context, input *WebSearchInput, opts ...tool.Option) (output string, err error) {
			if input.Query == "" {
				return "", fmt.Errorf("search query cannot be empty")
			}
			count := input.Count
			if count <= 0 {
				count = 5
			}
			if count > 10 {
				count = 10
			}

			results, err := bingSearch(ctx, input.Query, count)
			if err != nil {
				out := WebSearchOutput{
					Success: false,
					Query:   input.Query,
					Message: fmt.Sprintf("搜索失败: %v", err),
				}
				b, _ := json.Marshal(out)
				return string(b), nil
			}

			out := WebSearchOutput{
				Success: true,
				Query:   input.Query,
				Results: results,
			}
			b, _ := json.Marshal(out)
			return string(b), nil
		},
	)
	if err != nil {
		log.Fatal(err)
	}
	return t
}

// bingSearch 调用 Bing Web Search API
// 需要在 config.yaml 中配置:
//
//	web_search:
//	  api_key: "your-bing-api-key"
//	  endpoint: "https://api.bing.microsoft.com/v7.0/search"  # 可选，有默认值
func bingSearch(ctx context.Context, query string, count int) ([]WebSearchResult, error) {
	// TODO: 从配置文件读取，这里先用占位符，使用前需替换为真实 key
	apiKey := "" // 请替换为你的 Bing Search API Key，或从 g.Cfg() 读取
	endpoint := "https://api.bing.microsoft.com/v7.0/search"

	// 也可以从 GoFrame 配置读取：
	// apiKey = g.Cfg().MustGet(gctx.New(), "web_search.api_key").String()
	// endpoint = g.Cfg().MustGet(gctx.New(), "web_search.endpoint", endpoint).String()

	if apiKey == "" {
		return nil, fmt.Errorf("web_search.api_key is not configured")
	}

	params := url.Values{}
	params.Set("q", query)
	params.Set("count", fmt.Sprintf("%d", count))
	params.Set("mkt", "zh-CN")

	reqURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())

	httpReq, err := http.NewRequestWithContext(ctx, http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}
	httpReq.Header.Set("Ocp-Apim-Subscription-Key", apiKey)

	client := &http.Client{Timeout: 10 * time.Second}
	resp, err := client.Do(httpReq)
	if err != nil {
		return nil, fmt.Errorf("http request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("bing API returned %d: %s", resp.StatusCode, string(body))
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil, fmt.Errorf("read response failed: %w", err)
	}

	// Bing API 响应结构
	var bingResp struct {
		WebPages struct {
			Value []struct {
				Name    string `json:"name"`
				URL     string `json:"url"`
				Snippet string `json:"snippet"`
			} `json:"value"`
		} `json:"webPages"`
	}

	if err := json.Unmarshal(body, &bingResp); err != nil {
		return nil, fmt.Errorf("parse response failed: %w", err)
	}

	results := make([]WebSearchResult, 0, len(bingResp.WebPages.Value))
	for _, v := range bingResp.WebPages.Value {
		results = append(results, WebSearchResult{
			Title:   v.Name,
			URL:     v.URL,
			Snippet: v.Snippet,
		})
	}

	return results, nil
}
