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
		if logger != nil {
			logger.Warn("elasticsearch client not available, tool will try lazy init before fallback")
		}
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
		Desc: "查询 Elasticsearch 日志，或发现当前可用日志索引。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {
				Type:     schema.String,
				Desc:     "执行动作：query（默认）或 discover_indices（发现可用索引）",
				Required: false,
			},
			"index": {
				Type:     schema.String,
				Desc:     "索引名称或模式（如 logs-*、app-logs-2024.03.*）；discover_indices 时可选，默认 *",
				Required: false,
			},
			"query": {
				Type:     schema.String,
				Desc:     "搜索关键词（支持 Lucene 查询语法，action=query 时使用）",
				Required: false,
			},
			"time_range": {
				Type:     schema.String,
				Desc:     "时间范围（如 5m, 1h, 24h），默认 1h，action=query 时使用",
				Required: false,
			},
			"level": {
				Type:     schema.String,
				Desc:     "日志级别过滤（error/warn/info/debug），action=query 时使用",
				Required: false,
			},
			"size": {
				Type:     schema.Integer,
				Desc:     "返回结果数量或索引样本数量，默认 100，最大 1000",
				Required: false,
			},
		}),
	}, nil
}

func (t *ESLogQueryTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Action    string `json:"action"`
		Index     string `json:"index"`
		Query     string `json:"query"`
		TimeRange string `json:"time_range"`
		Level     string `json:"level"`
		Size      int    `json:"size"`
	}

	var in args
	if err := unmarshalOpsArgsLenient(argumentsInJSON, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Action = strings.ToLower(strings.TrimSpace(in.Action))
	if in.Action == "" {
		in.Action = "query"
	}
	if in.Action != "query" && in.Action != "discover_indices" {
		return "", fmt.Errorf("unsupported action: %s", in.Action)
	}

	// 参数校验
	in.Index = strings.TrimSpace(in.Index)

	// 默认值
	if in.Index == "" {
		if in.Action == "discover_indices" {
			in.Index = "*"
		} else {
			in.Index = "logs-*"
		}
	}
	if in.TimeRange == "" {
		in.TimeRange = "1h"
	}
	if in.Size <= 0 {
		in.Size = 100
	}
	if in.Size > 1000 {
		in.Size = 1000
	}

	if err := t.ensureClient(ctx); err != nil && t.logger != nil {
		t.logger.Warn("elasticsearch lazy init failed, fallback mode enabled", zap.Error(err))
	}

	// 如果客户端未初始化，返回降级数据
	if t.client == nil {
		return t.fallbackResponse(in.Index, in.Query, in.TimeRange, in.Level, in.Action), nil
	}

	if in.Action == "discover_indices" {
		result, err := t.discoverIndices(ctx, in.Index, in.Size)
		if err != nil {
			if t.logger != nil {
				t.logger.Error("es discover indices failed",
					zap.String("pattern", in.Index),
					zap.Error(err))
			}
			return "", fmt.Errorf("failed to discover indices: %w", err)
		}
		output, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("failed to marshal result: %w", err)
		}
		return string(output), nil
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

// ensureClient 尝试懒初始化 Elasticsearch 客户端。
// 输入：ctx。
// 输出：错误；若初始化成功则写回 t.client。
func (t *ESLogQueryTool) ensureClient(ctx context.Context) error {
	if t.client != nil {
		return nil
	}
	client := es.GetElasticsearch()
	if client != nil {
		t.client = client
		return nil
	}

	cfg := es.LoadElasticsearchConfigFromFile()
	if len(cfg.Addresses) == 0 && strings.TrimSpace(cfg.CloudID) == "" {
		return fmt.Errorf("elasticsearch config is empty")
	}
	client, err := es.InitElasticsearch(ctx, cfg)
	if err != nil {
		return err
	}
	t.client = client
	return nil
}

// discoverIndices 发现当前可用日志索引。
// 输入：ctx、pattern（索引模式）、limit（返回上限）。
// 输出：索引发现结果。
func (t *ESLogQueryTool) discoverIndices(ctx context.Context, pattern string, limit int) (map[string]interface{}, error) {
	if limit <= 0 {
		limit = 20
	}
	res, err := t.client.Cat.Indices(
		t.client.Cat.Indices.WithContext(ctx),
		t.client.Cat.Indices.WithIndex(pattern),
		t.client.Cat.Indices.WithFormat("json"),
		t.client.Cat.Indices.WithExpandWildcards("open,hidden"),
		t.client.Cat.Indices.WithH("health", "status", "index", "docs.count", "store.size"),
	)
	if err != nil {
		return nil, fmt.Errorf("cat indices request failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	var rows []map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&rows); err != nil {
		return nil, fmt.Errorf("failed to parse indices response: %w", err)
	}

	indices := make([]map[string]interface{}, 0, len(rows))
	for index, row := range rows {
		if limit > 0 && index >= limit {
			break
		}
		indices = append(indices, map[string]interface{}{
			"index":      strings.TrimSpace(fmt.Sprintf("%v", row["index"])),
			"health":     strings.TrimSpace(fmt.Sprintf("%v", row["health"])),
			"status":     strings.TrimSpace(fmt.Sprintf("%v", row["status"])),
			"docs_count": strings.TrimSpace(fmt.Sprintf("%v", row["docs.count"])),
			"store_size": strings.TrimSpace(fmt.Sprintf("%v", row["store.size"])),
		})
	}

	return map[string]interface{}{
		"type":    "index_discovery",
		"pattern": pattern,
		"count":   len(rows),
		"indices": indices,
	}, nil
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
func (t *ESLogQueryTool) fallbackResponse(index, query, timeRange, level, action string) string {
	if strings.TrimSpace(action) == "" {
		action = "query"
	}
	result := map[string]interface{}{
		"error":      "elasticsearch_unavailable",
		"message":    fmt.Sprintf("Elasticsearch client not available. Cannot execute action=%s on index: %s", action, index),
		"suggestion": "Please check Elasticsearch configuration and ensure the cluster is accessible",
		"query_params": map[string]interface{}{
			"action":     action,
			"index":      index,
			"query":      query,
			"time_range": timeRange,
			"level":      level,
		},
	}

	output, _ := json.Marshal(result)
	return string(output)
}
