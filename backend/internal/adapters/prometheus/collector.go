package prometheus

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/prometheus/client_golang/api"
	v1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"github.com/prometheus/common/model"
	"go.uber.org/zap"
)

// Collector Prometheus 指标采集工具
type Collector struct {
	client v1.API
	logger *zap.Logger
}

const (
	maxMetricSampleItems       = 20
	maxMetricSeriesItems       = 8
	maxMetricSeriesValuePoints = 6
)

// NewCollector 创建 Prometheus 指标采集工具
func NewCollector(prometheusURL string, logger *zap.Logger) *Collector {
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
		return &Collector{client: nil, logger: logger}
	}

	v1api := v1.NewAPI(client)

	if logger != nil {
		logger.Info("prometheus client initialized", zap.String("url", prometheusURL))
	}

	return &Collector{client: v1api, logger: logger}
}

func (t *Collector) Available() bool {
	return t != nil && t.client != nil
}

func (t *Collector) DiscoverMetricSources(ctx context.Context, metric string, limit int) (interface{}, error) {
	return t.discoverMetricSources(ctx, metric, limit)
}

func (t *Collector) Query(ctx context.Context, query string, queryTime time.Time, startTime time.Time, endTime time.Time, isRange bool) (interface{}, error) {
	if isRange {
		return t.queryRange(ctx, query, startTime, endTime)
	}
	return t.queryInstant(ctx, query, queryTime)
}

// discoverMetricSources 发现 Prometheus 当前可用指标源。
//
// 功能：
// 1. 获取 Prometheus 的 scrape targets 列表（active 和 dropped）
// 2. 提取 active targets 的样本信息（scrape pool、URL、健康状态、标签）
// 3. 获取关键标签的值（job、instance、namespace）
// 4. 探测关键指标的可用性（container、node、kube_pod 等）
// 5. 获取指定指标的元数据（类型、帮助信息、单位）
//
// 输入：
// - ctx: 上下文
// - metric: 可选的指标名过滤（用于获取该指标的元数据）
// - limit: 返回源样本上限（默认 20，最大 100）
//
// 输出：
// - interface{}: 包含以下信息的结构化结果
//   - type: "source_discovery"
//   - active_targets: 活跃目标数量
//   - dropped_targets: 丢弃目标数量
//   - health_summary: 健康状态统计（up/down/unknown）
//   - scrape_pools: scrape pool 名称列表
//   - target_samples: 目标样本列表（包含 scrape_pool、scrape_url、health、job、instance 等）
//   - label_values: 关键标签的值（job、instance、namespace）
//   - label_warnings: 标签查询警告
//   - metric_probe: 关键指标可用性探测结果
//   - metric_metadata: 指标元数据汇总
//
// 调用位置：
// - InvokableRun:161 行，当 action="discover_sources" 时调用
//
// Prometheus API 使用：
// - t.client.Targets(ctx): 获取 scrape targets 列表
// - t.client.LabelValues(ctx, "job", ...): 获取 job 标签的值
// - t.client.Metadata(ctx, metric, limit): 获取指标元数据
func (t *Collector) discoverMetricSources(ctx context.Context, metric string, limit int) (interface{}, error) {
	targets, err := t.client.Targets(ctx) //返回当前 Prometheus 的 scrape targets 列表，包含 active 和 dropped 两部分
	if err != nil {
		return nil, err
	}

	active := targets.Active
	samples := make([]map[string]interface{}, 0, minInt(limit, len(active))) // scrape targets 样本列表，包含 scrape pool、scrape url、health 状态、相关标签等信息
	scrapePools := make([]string, 0, len(active))                            // 使用集合辅助去重 scrape pool 名称
	poolSet := make(map[string]struct{}, len(active))                        // 健康状态统计

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
			//如果 scrape pool 不在列表中，则添加到列表和集合中
			// 保证 scrape pool 的唯一性
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
//
// 功能：
// 1. 对每个指标执行 PromQL 查询（count(metric)）
// 2. 检查查询结果是否包含样本数据
// 3. 返回每个指标的可用性状态和样本数量
//
// 输入：
// - ctx: 上下文
// - metrics: 需要探测的指标名列表
//
// 输出：
// - map[string]interface{}: 指标 -> 可用性信息
//   - available: 是否可用（bool）
//   - sample_count: 样本数量（float64，仅当可用时）
//   - error: 错误信息（仅当不可用时）
//
// 使用的指标：
// - container_cpu_usage_seconds_total: 容器 CPU 使用时间
// - container_memory_working_set_bytes: 容器内存工作集
// - node_cpu_seconds_total: 节点 CPU 使用时间
// - node_memory_MemAvailable_bytes: 节点可用内存
// - kube_pod_container_status_restarts_total: Pod 容器重启次数
func (t *Collector) probeMetricAvailability(ctx context.Context, metrics []string) map[string]interface{} {
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
func (t *Collector) queryInstant(ctx context.Context, query string, ts time.Time) (interface{}, error) {
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
func (t *Collector) queryRange(ctx context.Context, query string, start, end time.Time) (interface{}, error) {
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
func (t *Collector) formatResult(value model.Value, isRange bool) interface{} {
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
func (t *Collector) calculateStats(values []model.SamplePair) map[string]interface{} {
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
