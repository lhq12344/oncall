package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"go_agent/internal/agent/ops/tools"
	"go_agent/internal/cache"
	"go_agent/internal/concurrent"

	einotool "github.com/cloudwego/eino/components/tool"
	"github.com/redis/go-redis/v9"
	"go.uber.org/zap"
)

// IntegratedOpsExecutor 在 Agent 层集成并发执行、缓存和熔断保护。
type IntegratedOpsExecutor struct {
	toolExecutor *concurrent.ToolExecutor
	cache        *cache.MonitoringCache
	logger       *zap.Logger

	k8sTool        einotool.InvokableTool
	prometheusTool einotool.InvokableTool
	esLogTool      einotool.InvokableTool
}

// IntegratedOpsConfig 集成执行器配置。
type IntegratedOpsConfig struct {
	RedisClient   *redis.Client
	KubeConfig    string
	PrometheusURL string
	Logger        *zap.Logger
	Timeout       time.Duration
}

// QueryAllSourcesInput 多数据源并行查询参数。
type QueryAllSourcesInput struct {
	SessionID string
	Namespace string

	PromQuery string
	TimeRange string

	ESIndex string
	ESQuery string
	ESLevel string
	ESSize  int
}

// QueryAllSourcesOutput 聚合输出。
type QueryAllSourcesOutput struct {
	Data      map[string]string
	CacheHits map[string]bool
	Errors    map[string]string
}

// NewIntegratedOpsExecutor 创建集成执行器。
func NewIntegratedOpsExecutor(ctx context.Context, cfg *IntegratedOpsConfig) (*IntegratedOpsExecutor, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	timeout := cfg.Timeout
	if timeout <= 0 {
		timeout = 30 * time.Second
	}

	cbMgr := concurrent.NewCircuitBreakerManager(cfg.Logger)
	executor := concurrent.NewExecutor(&concurrent.ExecutorConfig{
		MaxConcurrency: 3,
		Timeout:        timeout,
		Logger:         cfg.Logger,
	})
	toolExecutor := concurrent.NewToolExecutor(&concurrent.ToolExecutorConfig{
		Executor:              executor,
		CircuitBreakerManager: cbMgr,
		Logger:                cfg.Logger,
	})

	k8sBase, err := tools.NewK8sMonitorTool(cfg.KubeConfig, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("create k8s tool failed: %w", err)
	}
	k8sTool, ok := k8sBase.(einotool.InvokableTool)
	if !ok {
		return nil, fmt.Errorf("k8s tool does not implement invokable interface")
	}

	promBase, err := tools.NewMetricsCollectorTool(cfg.PrometheusURL, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("create prometheus tool failed: %w", err)
	}
	promTool, ok := promBase.(einotool.InvokableTool)
	if !ok {
		return nil, fmt.Errorf("prometheus tool does not implement invokable interface")
	}

	esBase, err := tools.NewESLogQueryTool(cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("create es tool failed: %w", err)
	}
	esTool, ok := esBase.(einotool.InvokableTool)
	if !ok {
		return nil, fmt.Errorf("es tool does not implement invokable interface")
	}

	var monitoringCache *cache.MonitoringCache
	if cfg.RedisClient != nil {
		cacheCfg := cache.DefaultConfig()
		cacheCfg.RedisClient = cfg.RedisClient
		cacheCfg.Logger = cfg.Logger

		mgr, cacheErr := cache.NewManager(cacheCfg)
		if cacheErr != nil {
			if cfg.Logger != nil {
				cfg.Logger.Warn("ops integration cache disabled", zap.Error(cacheErr))
			}
		} else {
			monitoringCache = cache.NewMonitoringCache(mgr)
		}
	}

	_ = ctx // 预留上下文扩展

	return &IntegratedOpsExecutor{
		toolExecutor:   toolExecutor,
		cache:          monitoringCache,
		logger:         cfg.Logger,
		k8sTool:        k8sTool,
		prometheusTool: promTool,
		esLogTool:      esTool,
	}, nil
}

// QueryAllSources 并行查询 K8s、Prometheus、Elasticsearch，并带缓存短路能力。
func (e *IntegratedOpsExecutor) QueryAllSources(ctx context.Context, in QueryAllSourcesInput) (*QueryAllSourcesOutput, error) {
	if in.SessionID == "" {
		in.SessionID = "default-session"
	}
	if in.Namespace == "" {
		in.Namespace = "default"
	}
	if in.TimeRange == "" {
		in.TimeRange = "5m"
	}
	if in.PromQuery == "" {
		in.PromQuery = `up`
	}
	if in.ESIndex == "" {
		in.ESIndex = "logs-*"
	}
	if in.ESQuery == "" {
		in.ESQuery = "error"
	}
	if in.ESSize <= 0 {
		in.ESSize = 100
	}

	out := &QueryAllSourcesOutput{
		Data:      map[string]string{},
		CacheHits: map[string]bool{},
		Errors:    map[string]string{},
	}

	tasks := make([]concurrent.ToolTask, 0, 3)

	if payload, hit := e.tryMonitoringCache(ctx, "k8s", in.SessionID, "list_resources", "", map[string]interface{}{"namespace": in.Namespace}); hit {
		out.Data["k8s"] = payload
		out.CacheHits["k8s"] = true
	} else {
		args, _ := json.Marshal(map[string]interface{}{
			"namespace":     in.Namespace,
			"resource_type": "pod",
		})
		tasks = append(tasks, concurrent.ToolTask{Name: "k8s", Tool: e.k8sTool, Arguments: string(args)})
	}

	if payload, hit := e.tryMonitoringCache(ctx, "prometheus", in.SessionID, in.PromQuery, in.TimeRange, nil); hit {
		out.Data["prometheus"] = payload
		out.CacheHits["prometheus"] = true
	} else {
		args, _ := json.Marshal(map[string]interface{}{
			"query":      in.PromQuery,
			"time_range": in.TimeRange,
		})
		tasks = append(tasks, concurrent.ToolTask{Name: "prometheus", Tool: e.prometheusTool, Arguments: string(args)})
	}

	if payload, hit := e.tryMonitoringCache(ctx, "elasticsearch", in.SessionID, in.ESQuery, in.TimeRange, map[string]interface{}{"index": in.ESIndex, "level": in.ESLevel, "size": in.ESSize}); hit {
		out.Data["elasticsearch"] = payload
		out.CacheHits["elasticsearch"] = true
	} else {
		args, _ := json.Marshal(map[string]interface{}{
			"index":      in.ESIndex,
			"query":      in.ESQuery,
			"time_range": in.TimeRange,
			"level":      in.ESLevel,
			"size":       in.ESSize,
		})
		tasks = append(tasks, concurrent.ToolTask{Name: "elasticsearch", Tool: e.esLogTool, Arguments: string(args)})
	}

	if len(tasks) == 0 {
		return out, nil
	}

	results := e.toolExecutor.ExecuteToolsParallel(ctx, tasks)
	for _, r := range results {
		if r.Error != nil {
			out.Errors[r.Name] = r.Error.Error()
			continue
		}

		out.Data[r.Name] = r.Output
		e.storeMonitoringCache(ctx, r.Name, in, r.Output)
	}

	return out, nil
}

func (e *IntegratedOpsExecutor) tryMonitoringCache(
	ctx context.Context,
	source string,
	sessionID string,
	query string,
	timeRange string,
	params map[string]interface{},
) (string, bool) {
	if e.cache == nil {
		return "", false
	}

	key := &cache.MonitoringCacheKey{
		AgentID:    "ops_agent",
		SessionID:  sessionID,
		Source:     source,
		Query:      query,
		TimeRange:  timeRange,
		Parameters: params,
	}
	value, err := e.cache.Get(ctx, key)
	if err != nil || value == nil || value.Data == nil {
		return "", false
	}

	s, ok := value.Data.(string)
	if !ok {
		b, marshalErr := json.Marshal(value.Data)
		if marshalErr != nil {
			return "", false
		}
		s = string(b)
	}

	return s, true
}

func (e *IntegratedOpsExecutor) storeMonitoringCache(ctx context.Context, source string, in QueryAllSourcesInput, payload string) {
	if e.cache == nil {
		return
	}

	params := map[string]interface{}{}
	query := ""
	switch source {
	case "k8s":
		query = "list_resources"
		params["namespace"] = in.Namespace
	case "prometheus":
		query = in.PromQuery
	case "elasticsearch":
		query = in.ESQuery
		params["index"] = in.ESIndex
		params["level"] = in.ESLevel
		params["size"] = in.ESSize
	}

	key := &cache.MonitoringCacheKey{
		AgentID:    "ops_agent",
		SessionID:  in.SessionID,
		Source:     source,
		Query:      query,
		TimeRange:  in.TimeRange,
		Parameters: params,
	}

	if err := e.cache.Set(ctx, key, payload); err != nil && e.logger != nil {
		e.logger.Warn("set monitoring cache failed",
			zap.String("source", source),
			zap.Error(err))
	}
}
