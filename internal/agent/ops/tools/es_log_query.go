package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	es "go_agent/utility/elasticsearch"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/elastic/go-elasticsearch/v8"
	"go.uber.org/zap"
)

// ESLogQueryTool Elasticsearch 日志查询工具
type ESLogQueryTool struct {
	client *elasticsearch.Client
	logger *zap.Logger
}

// NewESLogQueryTool 创建 ES 日志查询工具
func NewESLogQueryTool(logger *zap.Logger) (tool.BaseTool, error) {
	client := es.GetElasticsearch()
	if client == nil {
		// 降级模式：返回工具但标记为不可用
		logger.Warn("elasticsearch client not available, tool will return placeholder data")
		return &ESLogQueryTool{
			client: nil,
			logger: logger,
		}, nil
	}

	return &ESLogQueryTool{
		client: client,
		logger: logger,
	}, nil
}

func (t *ESLogQueryTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "es_log_query",
		Desc: "查询 Elasticsearch 日志。支持关键词搜索、时间范围过滤、日志级别过滤等。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"index": {
				Type:     schema.String,
				Desc:     "索引名称或模式（如 logs-*, app-logs-2024.03.*）",
				Required: true,
			},
			"query": {
				Type:     schema.String,
				Desc:     "搜索关键词（支持 Lucene 查询语法）",
				Required: false,
			},
			"time_range": {
				Type:     schema.String,
				Desc:     "时间范围（如 5m, 1h, 24h），默认 1h",
				Required: false,
			},
			"level": {
				Type:     schema.String,
				Desc:     "日志级别过滤（error/warn/info/debug）",
				Required: false,
			},
			"size": {
				Type:     schema.Integer,
				Desc:     "返回结果数量，默认 100，最大 1000",
				Required: false,
			},
		}),
	}, nil
}

func (t *ESLogQueryTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Index     string `json:"index"`
		Query     string `json:"query"`
		TimeRange string `json:"time_range"`
		Level     string `json:"level"`
		Size      int    `json:"size"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	// 参数校验
	in.Index = strings.TrimSpace(in.Index)
	if in.Index == "" {
		return "", fmt.Errorf("index is required")
	}

	// 默认值
	if in.TimeRange == "" {
		in.TimeRange = "1h"
	}
	if in.Size <= 0 {
		in.Size = 100
	}
	if in.Size > 1000 {
		in.Size = 1000
	}

	// 如果客户端未初始化，返回降级数据
	if t.client == nil {
		return t.fallbackResponse(in.Index, in.Query, in.TimeRange, in.Level), nil
	}

	// 执行查询
	result, err := t.queryLogs(ctx, in.Index, in.Query, in.TimeRange, in.Level, in.Size)
	if err != nil {
		if t.logger != nil {
			t.logger.Error("es log query failed",
				zap.String("index", in.Index),
				zap.String("query", in.Query),
				zap.Error(err))
		}
		return "", fmt.Errorf("failed to query logs: %w", err)
	}

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("es log query completed",
			zap.String("index", in.Index),
			zap.String("query", in.Query),
			zap.Int("hits", result["total_hits"].(int)))
	}

	return string(output), nil
}

// queryLogs 查询日志
func (t *ESLogQueryTool) queryLogs(ctx context.Context, index, query, timeRange, level string, size int) (map[string]interface{}, error) {
	// 解析时间范围
	duration, err := time.ParseDuration(timeRange)
	if err != nil {
		return nil, fmt.Errorf("invalid time_range format: %w", err)
	}

	// 构建 ES 查询
	esQuery := t.buildQuery(query, duration, level)

	// 执行搜索
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(esQuery); err != nil {
		return nil, fmt.Errorf("failed to encode query: %w", err)
	}

	res, err := t.client.Search(
		t.client.Search.WithContext(ctx),
		t.client.Search.WithIndex(index),
		t.client.Search.WithBody(&buf),
		t.client.Search.WithSize(size),
		t.client.Search.WithSort("@timestamp:desc"),
	)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	// 解析响应
	var response map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	// 提取日志条目
	logs := t.extractLogs(response)

	return map[string]interface{}{
		"index":      index,
		"query":      query,
		"time_range": timeRange,
		"level":      level,
		"total_hits": t.getTotalHits(response),
		"logs":       logs,
	}, nil
}

// buildQuery 构建 ES 查询
func (t *ESLogQueryTool) buildQuery(query string, duration time.Duration, level string) map[string]interface{} {
	must := []map[string]interface{}{}

	// 时间范围过滤 - 转换为 ES 支持的格式
	// Go duration: 1h0m0s -> ES format: 1h
	esTimeRange := convertDurationToESFormat(duration)
	must = append(must, map[string]interface{}{
		"range": map[string]interface{}{
			"@timestamp": map[string]interface{}{
				"gte": fmt.Sprintf("now-%s", esTimeRange),
				"lte": "now",
			},
		},
	})

	// 关键词搜索
	if query != "" {
		must = append(must, map[string]interface{}{
			"query_string": map[string]interface{}{
				"query": query,
			},
		})
	}

	// 日志级别过滤
	if level != "" {
		must = append(must, map[string]interface{}{
			"match": map[string]interface{}{
				"level": level,
			},
		})
	}

	return map[string]interface{}{
		"query": map[string]interface{}{
			"bool": map[string]interface{}{
				"must": must,
			},
		},
	}
}

// convertDurationToESFormat 将 Go duration 转换为 ES 支持的格式
// 例如: 1h0m0s -> 1h, 30m0s -> 30m, 1h30m0s -> 90m
func convertDurationToESFormat(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 && minutes == 0 && seconds == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	if hours > 0 {
		// 转换为分钟
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if minutes > 0 && seconds == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	if minutes > 0 {
		// 转换为秒
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%ds", seconds)
}

// extractLogs 提取日志条目
func (t *ESLogQueryTool) extractLogs(response map[string]interface{}) []map[string]interface{} {
	logs := []map[string]interface{}{}

	hits, ok := response["hits"].(map[string]interface{})
	if !ok {
		return logs
	}

	hitsArray, ok := hits["hits"].([]interface{})
	if !ok {
		return logs
	}

	for _, hit := range hitsArray {
		hitMap, ok := hit.(map[string]interface{})
		if !ok {
			continue
		}

		source, ok := hitMap["_source"].(map[string]interface{})
		if !ok {
			continue
		}

		logs = append(logs, source)
	}

	return logs
}

// getTotalHits 获取总命中数
func (t *ESLogQueryTool) getTotalHits(response map[string]interface{}) int {
	hits, ok := response["hits"].(map[string]interface{})
	if !ok {
		return 0
	}

	total, ok := hits["total"].(map[string]interface{})
	if !ok {
		return 0
	}

	value, ok := total["value"].(float64)
	if !ok {
		return 0
	}

	return int(value)
}

// fallbackResponse 降级响应
func (t *ESLogQueryTool) fallbackResponse(index, query, timeRange, level string) string {
	result := map[string]interface{}{
		"error":      "elasticsearch_unavailable",
		"message":    fmt.Sprintf("Elasticsearch client not available. Cannot query index: %s", index),
		"suggestion": "Please check Elasticsearch configuration and ensure the cluster is accessible",
		"query_params": map[string]interface{}{
			"index":      index,
			"query":      query,
			"time_range": timeRange,
			"level":      level,
		},
	}

	output, _ := json.Marshal(result)
	return string(output)
}
