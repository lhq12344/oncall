package elasticsearch

import (
	"bytes"
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strconv"
	"strings"
	"time"

	essdk "github.com/elastic/go-elasticsearch/v8"
	"github.com/gogf/gf/v2/frame/g"
	"github.com/gogf/gf/v2/os/gctx"
)

type ElasticsearchConfig struct {
	Addresses []string      // ES 集群地址列表
	Username  string        // 用户名
	Password  string        // 密码
	CloudID   string        // Elastic Cloud ID（可选）
	APIKey    string        // API Key（可选）
	Timeout   time.Duration // 请求超时
	TLSSkip   bool          // 跳过 TLS 验证（开发环境）
}

// Client owns the Elasticsearch SDK client and keeps SDK types at the adapter seam.
type Client struct {
	client *essdk.Client
}

// BulkDocument is the SDK-free document shape accepted by the ES bulk adapter.
type BulkDocument struct {
	Index string
	ID    string
	Body  map[string]interface{}
}

// NewLazyClient creates an adapter that will initialize itself from config on first use.
func NewLazyClient() *Client {
	return &Client{}
}

// LoadElasticsearchConfigFromFile 从配置文件加载 ES 配置
func LoadElasticsearchConfigFromFile() ElasticsearchConfig {
	ctx := gctx.New()

	cfg := ElasticsearchConfig{}

	// 从环境变量或配置文件读取
	if addresses := os.Getenv("ES_ADDRESSES"); addresses != "" {
		cfg.Addresses = strings.Split(addresses, ",")
	} else if addressesConfig := g.Cfg().MustGet(ctx, "elasticsearch.addresses"); !addressesConfig.IsEmpty() {
		cfg.Addresses = addressesConfig.Strings()
	}

	cfg.Username = getConfigString(ctx, "elasticsearch.username", "ES_USERNAME", "")
	cfg.Password = getConfigString(ctx, "elasticsearch.password", "ES_PASSWORD", "")
	cfg.CloudID = getConfigString(ctx, "elasticsearch.cloud_id", "ES_CLOUD_ID", "")
	cfg.APIKey = getConfigString(ctx, "elasticsearch.api_key", "ES_API_KEY", "")
	cfg.Timeout = g.Cfg().MustGet(ctx, "elasticsearch.timeout", 10*time.Second).Duration()
	cfg.TLSSkip = g.Cfg().MustGet(ctx, "elasticsearch.tls_skip", false).Bool()

	return cfg
}

// InitElasticsearch 初始化 Elasticsearch 客户端
func InitElasticsearch(ctx context.Context, cfg ElasticsearchConfig) (*Client, error) {
	if len(cfg.Addresses) == 0 && cfg.CloudID == "" {
		return nil, fmt.Errorf("elasticsearch addresses or cloud_id is required")
	}

	// 默认超时
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}

	// 构建 ES 配置
	esCfg := essdk.Config{
		Addresses: cfg.Addresses,
		CloudID:   cfg.CloudID,
		APIKey:    cfg.APIKey,
		Username:  cfg.Username,
		Password:  cfg.Password,
		Transport: &http.Transport{
			MaxIdleConnsPerHost:   10,
			ResponseHeaderTimeout: cfg.Timeout,
			TLSClientConfig: &tls.Config{
				InsecureSkipVerify: cfg.TLSSkip,
			},
		},
	}

	// 创建客户端
	sdkClient, err := essdk.NewClient(esCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create elasticsearch client: %w", err)
	}

	// 测试连接
	res, err := sdkClient.Info()
	if err != nil {
		return nil, fmt.Errorf("failed to ping elasticsearch: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	// 解析版本信息
	var info map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&info); err != nil {
		return nil, fmt.Errorf("failed to parse elasticsearch info: %w", err)
	}

	return &Client{client: sdkClient}, nil
}

// CloseElasticsearch 关闭 ES 客户端（ES v8 客户端无需显式关闭）
func CloseElasticsearch() error {
	// Elasticsearch v8 客户端不需要显式关闭
	return nil
}

// Available reports whether the adapter currently owns a live SDK client.
func (c *Client) Available() bool {
	return c != nil && c.client != nil
}

// Ensure lazily initializes the adapter from process configuration when needed.
func (c *Client) Ensure(ctx context.Context) error {
	if c == nil {
		return fmt.Errorf("elasticsearch client is nil")
	}
	if c.Available() {
		return nil
	}
	cfg := LoadElasticsearchConfigFromFile()
	if len(cfg.Addresses) == 0 && strings.TrimSpace(cfg.CloudID) == "" {
		return fmt.Errorf("elasticsearch config is empty")
	}
	initialized, err := InitElasticsearch(ctx, cfg)
	if err != nil {
		return err
	}
	c.client = initialized.client
	return nil
}

// DiscoverIndices discovers available Elasticsearch indices for a pattern.
func (c *Client) DiscoverIndices(ctx context.Context, pattern string, limit int) (map[string]interface{}, error) {
	if !c.Available() {
		return nil, fmt.Errorf("elasticsearch client not initialized")
	}
	if limit <= 0 {
		limit = 20
	}
	res, err := c.client.Cat.Indices(
		c.client.Cat.Indices.WithContext(ctx),
		c.client.Cat.Indices.WithIndex(pattern),
		c.client.Cat.Indices.WithFormat("json"),
		c.client.Cat.Indices.WithExpandWildcards("open,hidden"),
		c.client.Cat.Indices.WithH("health", "status", "index", "docs.count", "store.size"),
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

// QueryLogs runs a time-bounded Elasticsearch log query and returns SDK-free data.
func (c *Client) QueryLogs(ctx context.Context, index, query, timeRange, level string, size int) (map[string]interface{}, error) {
	if !c.Available() {
		return nil, fmt.Errorf("elasticsearch client not initialized")
	}
	duration, err := parseLogTimeRange(timeRange)
	if err != nil {
		return nil, fmt.Errorf("invalid time_range format: %w", err)
	}

	esQuery := buildQuery(query, duration, level)
	var buf bytes.Buffer
	if err := json.NewEncoder(&buf).Encode(esQuery); err != nil {
		return nil, fmt.Errorf("failed to encode query: %w", err)
	}

	res, err := c.client.Search(
		c.client.Search.WithContext(ctx),
		c.client.Search.WithIndex(index),
		c.client.Search.WithBody(&buf),
		c.client.Search.WithSize(size),
		c.client.Search.WithSort("@timestamp:desc"),
	)
	if err != nil {
		return nil, fmt.Errorf("search request failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return nil, fmt.Errorf("elasticsearch returned error: %s", res.String())
	}

	var response map[string]interface{}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return nil, fmt.Errorf("failed to parse response: %w", err)
	}

	return map[string]interface{}{
		"index":      index,
		"query":      query,
		"time_range": timeRange,
		"level":      level,
		"total_hits": getTotalHits(response),
		"logs":       extractLogs(response),
	}, nil
}

// parseLogTimeRange accepts Go duration syntax plus common operational units
// such as d and w that models routinely use in log queries.
func parseLogTimeRange(value string) (time.Duration, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	if value == "" {
		return 0, fmt.Errorf("time range is required")
	}
	if strings.HasSuffix(value, "d") || strings.HasSuffix(value, "w") {
		unit := value[len(value)-1]
		amount, err := strconv.ParseFloat(value[:len(value)-1], 64)
		if err != nil || amount <= 0 {
			return 0, fmt.Errorf("invalid time range %q", value)
		}
		multiplier := 24 * time.Hour
		if unit == 'w' {
			multiplier = 7 * 24 * time.Hour
		}
		return time.Duration(amount * float64(multiplier)), nil
	}
	return time.ParseDuration(value)
}

// BulkIndexDocuments writes documents through Elasticsearch bulk API in batches.
func (c *Client) BulkIndexDocuments(ctx context.Context, documents []BulkDocument, batchSize int) error {
	if !c.Available() {
		return fmt.Errorf("elasticsearch client not initialized")
	}
	if batchSize <= 0 {
		batchSize = 200
	}
	for start := 0; start < len(documents); start += batchSize {
		end := start + batchSize
		if end > len(documents) {
			end = len(documents)
		}
		if err := c.bulkIndexBatch(ctx, documents[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func (c *Client) bulkIndexBatch(ctx context.Context, documents []BulkDocument) error {
	var body bytes.Buffer
	encoder := json.NewEncoder(&body)

	for _, document := range documents {
		meta := map[string]map[string]string{
			"index": {
				"_index": document.Index,
				"_id":    document.ID,
			},
		}
		if err := encoder.Encode(meta); err != nil {
			return fmt.Errorf("encode bulk meta failed: %w", err)
		}
		if err := encoder.Encode(document.Body); err != nil {
			return fmt.Errorf("encode bulk document failed: %w", err)
		}
	}

	res, err := c.client.Bulk(
		bytes.NewReader(body.Bytes()),
		c.client.Bulk.WithContext(ctx),
		c.client.Bulk.WithRefresh("false"),
	)
	if err != nil {
		return fmt.Errorf("bulk index request failed: %w", err)
	}
	defer res.Body.Close()

	if res.IsError() {
		return fmt.Errorf("bulk index returned error: %s", res.String())
	}

	var response struct {
		Errors bool `json:"errors"`
		Items  []map[string]struct {
			Status int `json:"status"`
			Error  struct {
				Reason string `json:"reason"`
				Type   string `json:"type"`
			} `json:"error"`
		} `json:"items"`
	}
	if err := json.NewDecoder(res.Body).Decode(&response); err != nil {
		return fmt.Errorf("decode bulk response failed: %w", err)
	}

	if !response.Errors {
		return nil
	}

	for _, item := range response.Items {
		for _, result := range item {
			if result.Status < 300 {
				continue
			}
			return fmt.Errorf("bulk item failed: %s (%s)", strings.TrimSpace(result.Error.Reason), strings.TrimSpace(result.Error.Type))
		}
	}

	return fmt.Errorf("bulk index failed with unknown item error")
}

func buildQuery(query string, duration time.Duration, level string) map[string]interface{} {
	must := []map[string]interface{}{}
	esTimeRange := convertDurationToESFormat(duration)
	must = append(must, map[string]interface{}{
		"range": map[string]interface{}{
			"@timestamp": map[string]interface{}{
				"gte": fmt.Sprintf("now-%s", esTimeRange),
				"lte": "now",
			},
		},
	})

	if query != "" {
		must = append(must, map[string]interface{}{
			"query_string": map[string]interface{}{
				"query": query,
			},
		})
	}

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

func convertDurationToESFormat(d time.Duration) string {
	hours := int(d.Hours())
	minutes := int(d.Minutes()) % 60
	seconds := int(d.Seconds()) % 60

	if hours > 0 && minutes == 0 && seconds == 0 {
		return fmt.Sprintf("%dh", hours)
	}
	if hours > 0 {
		return fmt.Sprintf("%dm", int(d.Minutes()))
	}
	if minutes > 0 && seconds == 0 {
		return fmt.Sprintf("%dm", minutes)
	}
	if minutes > 0 {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	return fmt.Sprintf("%ds", seconds)
}

func extractLogs(response map[string]interface{}) []map[string]interface{} {
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

func getTotalHits(response map[string]interface{}) int {
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

// getConfigString 优先从环境变量读取，否则从配置文件读取
func getConfigString(ctx context.Context, configKey, envKey, defaultValue string) string {
	if val := os.Getenv(envKey); val != "" {
		return val
	}
	return g.Cfg().MustGet(ctx, configKey, defaultValue).String()
}
