package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"
)

// MetricsCollectorTool Prometheus 指标采集工具
type MetricsCollectorTool struct {
	client v1.API
	logger *zap.Logger
}

const (
	maxMetricSampleItems       = 20
	maxMetricSeriesItems       = 8
	maxMetricSeriesValuePoints = 6
)

// NewMetricsCollectorTool 创建 Prometheus 指标采集工具
func NewMetricsCollectorTool(prometheusURL string, logger *zap.Logger) (tool.BaseTool, error) {
	if prometheusURL == "" {
		prometheusURL = "http://localhost:9090"
	}

	client, err := api.NewClient(api.Config{
		Address: prometheusURL,
	})
	if err != nil {
		if logger != nil {
			logger.Warn("failed to create prometheus client, tool will return placeholder data",
				zap.Error(err))
		}
		return &MetricsCollectorTool{
			client: nil,
			logger: logger,
		}, nil
	}

	v1api := v1.NewAPI(client)

	if logger != nil {
		logger.Info("prometheus client initialized", zap.String("url", prometheusURL))
	}

	return &MetricsCollectorTool{
		client: v1api,
		logger: logger,
	}, nil
}

func (t *MetricsCollectorTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "metrics_collector",
		Desc: "采集 Prometheus 指标数据。支持 PromQL 查询，也支持发现当前可用指标源（scrape targets/label values/关键指标可用性）。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {
				Type:     schema.String,
				Desc:     "执行动作：query（默认，执行 PromQL）或 discover_sources（发现指标源）",
				Required: false,
			},
			"query": {
				Type:     schema.String,
				Desc:     "PromQL 查询语句（action=query 时必填）",
				Required: false,
			},
			"time_range": {
				Type:     schema.String,
				Desc:     "时间范围（如 5m, 1h, 24h），仅 action=query 生效，默认当前时刻",
				Required: false,
			},
			"metric": {
				Type:     schema.String,
				Desc:     "action=discover_sources 时可选：按指标名过滤 metadata",
				Required: false,
			},
			"limit": {
				Type:     schema.Integer,
				Desc:     "action=discover_sources 时可选：返回源样本上限，默认 20，最大 100",
				Required: false,
			},
		}),
	}, nil
}

func (t *MetricsCollectorTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Action    string `json:"action"`
		Query     string `json:"query"`
		TimeRange string `json:"time_range"`
		Metric    string `json:"metric"`
		Limit     int    `json:"limit"`
	}

	var in args
	if err := unmarshalOpsArgsLenient(argumentsInJSON, &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Action = strings.ToLower(strings.TrimSpace(in.Action))
	if in.Action == "" {
		in.Action = "query"
	}
	if in.Action != "query" && in.Action != "discover_sources" {
		return "", fmt.Errorf("unsupported action: %s", in.Action)
	}

	in.Query = strings.TrimSpace(in.Query)
	in.TimeRange = strings.TrimSpace(in.TimeRange)
	in.Metric = strings.TrimSpace(in.Metric)
	if in.Limit <= 0 {
		in.Limit = 20
	}
	if in.Limit > 100 {
		in.Limit = 100
	}

	if in.Action == "query" && in.Query == "" {
		return "", fmt.Errorf("query is required when action=query")
	}

	callCount := increaseToolCallCount(ctx, "metrics_collector")
	cacheKey := strings.ToLower(in.Action) + "|" +
		strings.ToLower(strings.TrimSpace(in.Query)) + "|" +
		strings.ToLower(in.TimeRange) + "|" +
		strings.ToLower(in.Metric) + "|" +
		strconv.Itoa(in.Limit)
	if cached, ok := getCachedToolResult(ctx, "metrics_collector", cacheKey); ok {
		if t.logger != nil {
			t.logger.Info("metrics collector cache hit",
				zap.String("agent", currentAgentForLog(ctx, "ops_agent")),
				zap.String("action", in.Action),
				zap.String("query", in.Query),
				zap.String("time_range", in.TimeRange),
				zap.Int("call_count", callCount))
		}
		return cached, nil
	}

	// 如果客户端未初始化，返回降级数据
	if t.client == nil {
		output := t.fallbackResponse(in.Action, in.Query)
		setCachedToolResult(ctx, "metrics_collector", cacheKey, output)
		return output, nil
	}

	// 指标源发现模式
	if in.Action == "discover_sources" {
		result, err := t.discoverMetricSources(ctx, in.Metric, in.Limit)
		if err != nil {
			if t.logger != nil {
				t.logger.Error("prometheus source discovery failed",
					zap.String("agent", currentAgentForLog(ctx, "ops_agent")),
					zap.String("metric", in.Metric),
					zap.Error(err))
			}
			return "", fmt.Errorf("failed to discover metric sources: %w", err)
		}

		output, err := json.Marshal(result)
		if err != nil {
			return "", fmt.Errorf("failed to marshal discovery result: %w", err)
		}

		if t.logger != nil {
			t.logger.Info("metrics source discovery completed",
				zap.String("agent", currentAgentForLog(ctx, "ops_agent")),
				zap.String("metric", in.Metric),
				zap.Int("call_count", callCount))
		}

		setCachedToolResult(ctx, "metrics_collector", cacheKey, string(output))
		return string(output), nil
	}

	// 解析时间范围
	queryTime := time.Now()
	var startTime, endTime time.Time
	var isRange bool

	if in.TimeRange != "" {
		duration, err := time.ParseDuration(in.TimeRange)
		if err != nil {
			return "", fmt.Errorf("invalid time_range format: %w", err)
		}
		startTime = queryTime.Add(-duration)
		endTime = queryTime
		isRange = true
	}

	// 执行查询
	var result interface{}
	var err error

	if isRange {
		// 范围查询
		result, err = t.queryRange(ctx, in.Query, startTime, endTime)
	} else {
		// 即时查询
		result, err = t.queryInstant(ctx, in.Query, queryTime)
	}

	if err != nil {
		if t.logger != nil {
			t.logger.Error("prometheus query failed",
				zap.String("agent", currentAgentForLog(ctx, "ops_agent")),
				zap.String("query", in.Query),
				zap.Error(err))
		}
		return "", fmt.Errorf("failed to query metrics: %w", err)
	}

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("metrics collection completed",
			zap.String("agent", currentAgentForLog(ctx, "ops_agent")),
			zap.String("action", in.Action),
			zap.String("query", in.Query),
			zap.Bool("is_range", isRange),
			zap.Int("call_count", callCount))
	}

	setCachedToolResult(ctx, "metrics_collector", cacheKey, string(output))
	return string(output), nil
}

// discoverMetricSources 发现 Prometheus 当前可用指标源。
// 输入：ctx、metric（可选过滤指标名）、limit（返回源样本上限）。
// 输出：包含 targets、scrape pools、label values 与关键指标可用性的结构化结果。
func (t *MetricsCollectorTool) discoverMetricSources(ctx context.Context, metric string, limit int) (interface{}, error) {
	targets, err := t.client.Targets(ctx)
	if err != nil {
		return nil, err
	}

	active := targets.Active
	samples := make([]map[string]interface{}, 0, minInt(limit, len(active)))
	scrapePools := make([]string, 0, len(active))
	poolSet := make(map[string]struct{}, len(active))

	healthSummary := map[string]int{
		"up":      0,
		"down":    0,
		"unknown": 0,
	}

	for index, target := range active {
		health := strings.ToLower(strings.TrimSpace(string(target.Health)))
		switch health {
		case "up":
			healthSummary["up"]++
		case "down":
			healthSummary["down"]++
		default:
			healthSummary["unknown"]++
		}

		pool := strings.TrimSpace(target.ScrapePool)
		if pool != "" {
			if _, exists := poolSet[pool]; !exists {
				poolSet[pool] = struct{}{}
				scrapePools = append(scrapePools, pool)
			}
		}

		if index >= limit {
			continue
		}

		sample := map[string]interface{}{
			"scrape_pool": pool,
			"scrape_url":  strings.TrimSpace(target.ScrapeURL),
			"health":      health,
			"last_error":  strings.TrimSpace(target.LastError),
		}
		if value, ok := target.Labels["job"]; ok {
			sample["job"] = string(value)
		}
		if value, ok := target.Labels["instance"]; ok {
			sample["instance"] = string(value)
		}
		if value, ok := target.Labels["namespace"]; ok {
			sample["namespace"] = string(value)
		}
		if value, ok := target.Labels["pod"]; ok {
			sample["pod"] = string(value)
		}
		if value, ok := target.Labels["node"]; ok {
			sample["node"] = string(value)
		}
		samples = append(samples, sample)
	}
	sort.Strings(scrapePools)

	jobValues, jobWarnings, _ := t.client.LabelValues(ctx, "job", nil, time.Time{}, time.Time{})
	instanceValues, instanceWarnings, _ := t.client.LabelValues(ctx, "instance", nil, time.Time{}, time.Time{})
	namespaceValues, namespaceWarnings, _ := t.client.LabelValues(ctx, "namespace", nil, time.Time{}, time.Time{})

	labelWarnings := appendWarnings(jobWarnings, instanceWarnings, namespaceWarnings)

	probeMetrics := []string{
		"container_cpu_usage_seconds_total",
		"container_memory_working_set_bytes",
		"node_cpu_seconds_total",
		"node_memory_MemAvailable_bytes",
		"kube_pod_container_status_restarts_total",
	}
	metricProbe := t.probeMetricAvailability(ctx, probeMetrics)

	metadata, metaErr := t.client.Metadata(ctx, metric, strconv.Itoa(limit))
	metadataSummary := summarizeMetricMetadata(metadata, limit)

	result := map[string]interface{}{
		"type":            "source_discovery",
		"active_targets":  len(targets.Active),
		"dropped_targets": len(targets.Dropped),
		"health_summary":  healthSummary,
		"scrape_pools":    scrapePools,
		"target_samples":  samples,
		"label_values": map[string]interface{}{
			"job":       labelValuesToStrings(jobValues, limit),
			"instance":  labelValuesToStrings(instanceValues, limit),
			"namespace": labelValuesToStrings(namespaceValues, limit),
		},
		"label_warnings":  labelWarnings,
		"metric_probe":    metricProbe,
		"metric_metadata": metadataSummary,
	}

	if metaErr != nil {
		result["metadata_error"] = metaErr.Error()
	}

	return result, nil
}

// probeMetricAvailability 探测关键指标在当前 Prometheus 中是否可查询。
// 输入：ctx、指标名列表。
// 输出：metric -> 可用性、样本数量、错误信息。
func (t *MetricsCollectorTool) probeMetricAvailability(ctx context.Context, metrics []string) map[string]interface{} {
	out := make(map[string]interface{}, len(metrics))
	now := time.Now()
	for _, metric := range metrics {
		metric = strings.TrimSpace(metric)
		if metric == "" {
			continue
		}
		query := fmt.Sprintf("count(%s)", metric)
		value, _, err := t.client.Query(ctx, query, now)
		if err != nil {
			out[metric] = map[string]interface{}{
				"available": false,
				"error":     err.Error(),
			}
			continue
		}
		sampleCount := extractCountFromPromValue(value)
		out[metric] = map[string]interface{}{
			"available":    sampleCount > 0,
			"sample_count": sampleCount,
		}
	}
	return out
}

// extractCountFromPromValue 从 Prometheus 查询值中提取 count 数值。
// 输入：model.Value。
// 输出：count 数值，无法解析时返回 0。
func extractCountFromPromValue(value model.Value) float64 {
	switch typed := value.(type) {
	case model.Vector:
		if len(typed) == 0 {
			return 0
		}
		return float64(typed[0].Value)
	case *model.Scalar:
		if typed == nil {
			return 0
		}
		return float64(typed.Value)
	default:
		return 0
	}
}

// labelValuesToStrings 将 LabelValues 转为字符串切片。
// 输入：model.LabelValues、limit。
// 输出：截断后的字符串切片。
func labelValuesToStrings(values model.LabelValues, limit int) []string {
	out := make([]string, 0, len(values))
	for _, value := range values {
		out = append(out, string(value))
	}
	sort.Strings(out)
	if limit > 0 && len(out) > limit {
		return out[:limit]
	}
	return out
}

// appendWarnings 合并并去重 Prometheus warnings。
// 输入：多个 warnings 切片。
// 输出：合并去重后的字符串切片。
func appendWarnings(warnings ...v1.Warnings) []string {
	uniq := make(map[string]struct{})
	out := make([]string, 0)
	for _, group := range warnings {
		for _, warning := range group {
			text := strings.TrimSpace(string(warning))
			if text == "" {
				continue
			}
			if _, exists := uniq[text]; exists {
				continue
			}
			uniq[text] = struct{}{}
			out = append(out, text)
		}
	}
	sort.Strings(out)
	return out
}

// summarizeMetricMetadata 汇总指标元数据。
// 输入：metadata 原始映射、limit。
// 输出：简化后的指标元数据列表。
func summarizeMetricMetadata(metadata map[string][]v1.Metadata, limit int) []map[string]interface{} {
	if len(metadata) == 0 {
		return nil
	}

	keys := make([]string, 0, len(metadata))
	for name := range metadata {
		keys = append(keys, name)
	}
	sort.Strings(keys)
	if limit > 0 && len(keys) > limit {
		keys = keys[:limit]
	}

	out := make([]map[string]interface{}, 0, len(keys))
	for _, name := range keys {
		entries := metadata[name]
		if len(entries) == 0 {
			out = append(out, map[string]interface{}{
				"metric": name,
			})
			continue
		}
		entry := entries[0]
		out = append(out, map[string]interface{}{
			"metric": name,
			"type":   string(entry.Type),
			"help":   strings.TrimSpace(entry.Help),
			"unit":   strings.TrimSpace(entry.Unit),
		})
	}
	return out
}

// minInt 返回两个整数中的较小值。
// 输入：a、b。
// 输出：较小值。
func minInt(a, b int) int {
	if a < b {
		return a
	}
	return b
}

// queryInstant 即时查询
func (t *MetricsCollectorTool) queryInstant(ctx context.Context, query string, ts time.Time) (interface{}, error) {
	result, warnings, err := t.client.Query(ctx, query, ts)
	if err != nil {
		return nil, err
	}

	if len(warnings) > 0 && t.logger != nil {
		t.logger.Warn("prometheus query warnings", zap.Strings("warnings", warnings))
	}

	return t.formatResult(result, false), nil
}

// queryRange 范围查询
func (t *MetricsCollectorTool) queryRange(ctx context.Context, query string, start, end time.Time) (interface{}, error) {
	// 计算合适的步长（最多返回 100 个数据点）
	duration := end.Sub(start)
	step := duration / 100
	if step < time.Minute {
		step = time.Minute
	}

	r := v1.Range{
		Start: start,
		End:   end,
		Step:  step,
	}

	result, warnings, err := t.client.QueryRange(ctx, query, r)
	if err != nil {
		return nil, err
	}

	if len(warnings) > 0 && t.logger != nil {
		t.logger.Warn("prometheus query warnings", zap.Strings("warnings", warnings))
	}

	return t.formatResult(result, true), nil
}

// formatResult 格式化查询结果
func (t *MetricsCollectorTool) formatResult(value model.Value, isRange bool) interface{} {
	switch v := value.(type) {
	case model.Vector:
		// 即时查询结果
		samples := make([]map[string]interface{}, 0, minInt(maxMetricSampleItems, len(v)))
		for index, sample := range v {
			if index >= maxMetricSampleItems {
				break
			}
			samples = append(samples, map[string]interface{}{
				"metric":    sample.Metric.String(),
				"value":     float64(sample.Value),
				"timestamp": sample.Timestamp.Time().Format("2006-01-02 15:04:05"),
			})
		}
		return map[string]interface{}{
			"type":           "instant",
			"count":          len(v),
			"returned_count": len(samples),
			"truncated":      len(v) > len(samples),
			"samples":        samples,
		}

	case model.Matrix:
		// 范围查询结果
		series := make([]map[string]interface{}, 0, minInt(maxMetricSeriesItems, len(v)))
		for index, stream := range v {
			if index >= maxMetricSeriesItems {
				break
			}

			// 计算统计信息
			stats := t.calculateStats(stream.Values)

			series = append(series, map[string]interface{}{
				"metric":          stream.Metric.String(),
				"stats":           stats,
				"returned_points": len(sampleMetricPoints(stream.Values, maxMetricSeriesValuePoints)),
				"total_points":    len(stream.Values),
				"values":          sampleMetricPoints(stream.Values, maxMetricSeriesValuePoints),
			})
		}
		return map[string]interface{}{
			"type":           "range",
			"count":          len(v),
			"returned_count": len(series),
			"truncated":      len(v) > len(series),
			"series":         series,
		}

	default:
		return map[string]interface{}{
			"type":  "unknown",
			"value": value.String(),
		}
	}
}

// sampleMetricPoints 对时间序列点做头尾采样，避免把整段原始序列直接回灌模型上下文。
// 输入：原始 points、最大保留点数。
// 输出：采样后的时间点列表。
func sampleMetricPoints(points []model.SamplePair, limit int) []map[string]interface{} {
	if len(points) == 0 || limit <= 0 {
		return nil
	}
	if len(points) <= limit {
		out := make([]map[string]interface{}, 0, len(points))
		for _, pair := range points {
			out = append(out, map[string]interface{}{
				"timestamp": pair.Timestamp.Time().Format("2006-01-02 15:04:05"),
				"value":     float64(pair.Value),
			})
		}
		return out
	}

	headCount := limit / 2
	tailCount := limit - headCount
	if headCount <= 0 {
		headCount = 1
		tailCount = limit - 1
	}

	out := make([]map[string]interface{}, 0, limit+1)
	for _, pair := range points[:headCount] {
		out = append(out, map[string]interface{}{
			"timestamp": pair.Timestamp.Time().Format("2006-01-02 15:04:05"),
			"value":     float64(pair.Value),
		})
	}
	out = append(out, map[string]interface{}{
		"truncated": true,
		"omitted":   len(points) - limit,
	})
	for _, pair := range points[len(points)-tailCount:] {
		out = append(out, map[string]interface{}{
			"timestamp": pair.Timestamp.Time().Format("2006-01-02 15:04:05"),
			"value":     float64(pair.Value),
		})
	}
	return out
}

// calculateStats 计算统计信息
func (t *MetricsCollectorTool) calculateStats(values []model.SamplePair) map[string]interface{} {
	if len(values) == 0 {
		return map[string]interface{}{}
	}

	var sum, min, max float64
	min = float64(values[0].Value)
	max = float64(values[0].Value)

	for _, pair := range values {
		val := float64(pair.Value)
		sum += val
		if val < min {
			min = val
		}
		if val > max {
			max = val
		}
	}

	avg := sum / float64(len(values))

	return map[string]interface{}{
		"min":   min,
		"max":   max,
		"avg":   avg,
		"count": len(values),
	}
}

// fallbackResponse 降级响应
func (t *MetricsCollectorTool) fallbackResponse(action, query string) string {
	action = strings.TrimSpace(action)
	if action == "" {
		action = "query"
	}
	result := map[string]interface{}{
		"error":      "prometheus_client_unavailable",
		"message":    fmt.Sprintf("Prometheus client not available. Cannot execute action: %s, query: %s", action, query),
		"suggestion": "Please check Prometheus configuration and ensure it's running",
	}

	output, _ := json.Marshal(result)
	return string(output)
}
