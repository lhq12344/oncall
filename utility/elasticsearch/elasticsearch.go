package elasticsearch

import (
	"context"
	"crypto/tls"
	"encoding/json"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/elastic/go-elasticsearch/v8"
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

var GlobalES *elasticsearch.Client

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
func InitElasticsearch(ctx context.Context, cfg ElasticsearchConfig) (*elasticsearch.Client, error) {
	if len(cfg.Addresses) == 0 && cfg.CloudID == "" {
		return nil, fmt.Errorf("elasticsearch addresses or cloud_id is required")
	}

	// 默认超时
	if cfg.Timeout <= 0 {
		cfg.Timeout = 10 * time.Second
	}

	// 构建 ES 配置
	esCfg := elasticsearch.Config{
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
	client, err := elasticsearch.NewClient(esCfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create elasticsearch client: %w", err)
	}

	// 测试连接
	res, err := client.Info()
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

	GlobalES = client
	return client, nil
}

// GetElasticsearch 获取全局 ES 客户端
func GetElasticsearch() *elasticsearch.Client {
	return GlobalES
}

// CloseElasticsearch 关闭 ES 客户端（ES v8 客户端无需显式关闭）
func CloseElasticsearch() error {
	// Elasticsearch v8 客户端不需要显式关闭
	GlobalES = nil
	return nil
}

// getConfigString 优先从环境变量读取，否则从配置文件读取
func getConfigString(ctx context.Context, configKey, envKey, defaultValue string) string {
	if val := os.Getenv(envKey); val != "" {
		return val
	}
	return g.Cfg().MustGet(ctx, configKey, defaultValue).String()
}

