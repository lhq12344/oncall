package ops

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"go_agent/internal/agent/ops/tools"

	einotool "github.com/cloudwego/eino/components/tool"
	"go.uber.org/zap"
)

// IntegratedOpsExecutor 在 Agent 层封装多工具查询执行（顺序调用 + 超时控制）。
type IntegratedOpsExecutor struct {
	timeout time.Duration

	k8sTool        einotool.InvokableTool
	prometheusTool einotool.InvokableTool
	esLogTool      einotool.InvokableTool
}

// IntegratedOpsConfig 集成执行器配置。
type IntegratedOpsConfig struct {
	KubeConfig    string
	PrometheusURL string
	Logger        *zap.Logger
	Timeout       time.Duration
}

// QueryAllSourcesInput 多数据源查询参数。
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
	Data   map[string]string
	Errors map[string]string
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

	_ = ctx // 预留上下文扩展

	return &IntegratedOpsExecutor{
		timeout:        timeout,
		k8sTool:        k8sTool,
		prometheusTool: promTool,
		esLogTool:      esTool,
	}, nil
}

// QueryAllSources 顺序查询 K8s、Prometheus、Elasticsearch。
func (e *IntegratedOpsExecutor) QueryAllSources(ctx context.Context, in QueryAllSourcesInput) (*QueryAllSourcesOutput, error) {
	if e.timeout > 0 {
		timeoutCtx, cancel := context.WithTimeout(ctx, e.timeout)
		defer cancel()
		ctx = timeoutCtx
	}

	if in.SessionID == "" {
		in.SessionID = "default-session"
	}
	if in.Namespace == "" {
		in.Namespace = "infra"
	}
	if in.TimeRange == "" {
		in.TimeRange = "5m"
	}
	if in.PromQuery == "" {
		in.PromQuery = buildNamespaceCPUHistoryQuery(in.Namespace)
	}
	if in.ESIndex == "" {
		in.ESIndex = "logs-*"
	}
	if in.ESQuery == "" {
		in.ESQuery = buildNamespaceESQuery(in.Namespace)
	}
	if in.ESSize <= 0 {
		in.ESSize = 100
	}

	out := &QueryAllSourcesOutput{
		Data:   map[string]string{},
		Errors: map[string]string{},
	}

	k8sArgs, _ := json.Marshal(map[string]interface{}{
		"namespace":     in.Namespace,
		"resource_type": "pod",
	})
	k8sPayload, err := e.k8sTool.InvokableRun(ctx, string(k8sArgs))
	if err != nil {
		out.Errors["k8s"] = err.Error()
	} else {
		out.Data["k8s"] = k8sPayload
	}

	promArgs, _ := json.Marshal(map[string]interface{}{
		"query":      in.PromQuery,
		"time_range": in.TimeRange,
	})
	promPayload, err := e.prometheusTool.InvokableRun(ctx, string(promArgs))
	if err != nil {
		out.Errors["prometheus"] = err.Error()
	} else {
		out.Data["prometheus"] = promPayload
	}

	promSourceArgs, _ := json.Marshal(map[string]interface{}{
		"action": "discover_sources",
		"limit":  20,
	})
	promSourcePayload, err := e.prometheusTool.InvokableRun(ctx, string(promSourceArgs))
	if err != nil {
		out.Errors["prometheus_sources"] = err.Error()
	} else {
		out.Data["prometheus_sources"] = promSourcePayload
	}

	promHistoryQueries := map[string]string{
		"cpu":      buildNamespaceCPUHistoryQuery(in.Namespace),
		"memory":   buildNamespaceMemoryHistoryQuery(in.Namespace),
		"restarts": buildNamespaceRestartHistoryQuery(in.Namespace),
	}
	for name, query := range promHistoryQueries {
		args, _ := json.Marshal(map[string]interface{}{
			"query":      query,
			"time_range": in.TimeRange,
		})
		payload, err := e.prometheusTool.InvokableRun(ctx, string(args))
		if err != nil {
			out.Errors["prometheus_"+name] = err.Error()
			continue
		}
		out.Data["prometheus_"+name] = payload
	}

	esDiscoverArgs, _ := json.Marshal(map[string]interface{}{
		"action": "discover_indices",
		"index":  "*",
		"size":   20,
	})
	esDiscoverPayload, err := e.esLogTool.InvokableRun(ctx, string(esDiscoverArgs))
	if err != nil {
		out.Errors["elasticsearch_indices"] = err.Error()
	} else {
		out.Data["elasticsearch_indices"] = esDiscoverPayload
		if resolved := resolveLogIndexPattern(esDiscoverPayload, in.ESIndex); resolved != "" {
			in.ESIndex = resolved
		}
	}

	esArgs, _ := json.Marshal(map[string]interface{}{
		"index":      in.ESIndex,
		"query":      in.ESQuery,
		"time_range": in.TimeRange,
		"level":      in.ESLevel,
		"size":       in.ESSize,
	})
	esPayload, err := e.esLogTool.InvokableRun(ctx, string(esArgs))
	if err != nil {
		out.Errors["elasticsearch"] = err.Error()
	} else {
		out.Data["elasticsearch"] = esPayload
	}

	return out, nil
}

// buildNamespaceCPUHistoryQuery 构建命名空间 CPU 历史指标查询。
// 输入：namespace。
// 输出：兼容不同 label 命名的 PromQL。
func buildNamespaceCPUHistoryQuery(namespace string) string {
	namespace = firstNonEmptyText(strings.TrimSpace(namespace), "infra")
	return fmt.Sprintf(`sum(rate(container_cpu_usage_seconds_total{namespace="%s",container!="",image!=""}[5m])) by (pod) or sum(rate(container_cpu_usage_seconds_total{kubernetes_namespace="%s"}[5m])) by (kubernetes_pod_name)`, namespace, namespace)
}

// buildNamespaceMemoryHistoryQuery 构建命名空间内存历史指标查询。
// 输入：namespace。
// 输出：兼容不同 label 命名的 PromQL。
func buildNamespaceMemoryHistoryQuery(namespace string) string {
	namespace = firstNonEmptyText(strings.TrimSpace(namespace), "infra")
	return fmt.Sprintf(`sum(container_memory_working_set_bytes{namespace="%s",container!="",image!=""}) by (pod) or sum(container_memory_working_set_bytes{kubernetes_namespace="%s"}) by (kubernetes_pod_name)`, namespace, namespace)
}

// buildNamespaceRestartHistoryQuery 构建命名空间重启历史指标查询。
// 输入：namespace。
// 输出：兼容不同 label 命名的 PromQL。
func buildNamespaceRestartHistoryQuery(namespace string) string {
	namespace = firstNonEmptyText(strings.TrimSpace(namespace), "infra")
	return fmt.Sprintf(`max(kube_pod_container_status_restarts_total{namespace="%s"}) by (pod) or max(kube_pod_container_status_restarts_total{kubernetes_namespace="%s"}) by (kubernetes_pod_name)`, namespace, namespace)
}

// buildNamespaceESQuery 构建命名空间日志检索语句。
// 输入：namespace。
// 输出：兼容不同字段命名的 Lucene 查询语句。
func buildNamespaceESQuery(namespace string) string {
	namespace = firstNonEmptyText(strings.TrimSpace(namespace), "infra")
	return fmt.Sprintf(`(namespace:%[1]s OR kubernetes.namespace:%[1]s OR kubernetes.namespace_name:%[1]s) AND (error OR exception OR failed OR crash OR restart OR oom OR timeout)`, namespace)
}

// resolveLogIndexPattern 从索引发现结果中选择更合适的日志索引模式。
// 输入：discoverPayload（discover_indices 输出 JSON）、fallback（兜底索引模式）。
// 输出：建议使用的索引模式。
func resolveLogIndexPattern(discoverPayload string, fallback string) string {
	type indexEntry struct {
		Index string `json:"index"`
	}
	type discoverResult struct {
		Indices []indexEntry `json:"indices"`
	}

	fallback = strings.TrimSpace(fallback)
	result := discoverResult{}
	if err := json.Unmarshal([]byte(discoverPayload), &result); err != nil {
		return fallback
	}

	patterns := []string{"logs-", "filebeat-", "logstash-", "app-", "infra-"}
	for _, prefix := range patterns {
		for _, item := range result.Indices {
			indexName := strings.TrimSpace(item.Index)
			if strings.HasPrefix(indexName, prefix) {
				return prefix + "*"
			}
		}
	}

	if len(result.Indices) > 0 {
		return "*"
	}
	return fallback
}
