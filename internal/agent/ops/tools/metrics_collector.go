package tools

import (
	"context"
	"encoding/json"
	"fmt"
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
		Desc: "采集 Prometheus 指标数据。支持 PromQL 查询，可以查询 CPU、内存、网络等各类指标。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "PromQL 查询语句",
				Required: true,
			},
			"time_range": {
				Type:     schema.String,
				Desc:     "时间范围（如 5m, 1h, 24h），默认当前时刻",
				Required: false,
			},
		}),
	}, nil
}

func (t *MetricsCollectorTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Query     string `json:"query"`
		TimeRange string `json:"time_range"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	in.Query = strings.TrimSpace(in.Query)
	if in.Query == "" {
		return "", fmt.Errorf("query is required")
	}

	// 如果客户端未初始化，返回降级数据
	if t.client == nil {
		return t.fallbackResponse(in.Query), nil
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
			zap.String("query", in.Query),
			zap.Bool("is_range", isRange))
	}

	return string(output), nil
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
		samples := make([]map[string]interface{}, 0, len(v))
		for _, sample := range v {
			samples = append(samples, map[string]interface{}{
				"metric":    sample.Metric.String(),
				"value":     float64(sample.Value),
				"timestamp": sample.Timestamp.Time().Format("2006-01-02 15:04:05"),
			})
		}
		return map[string]interface{}{
			"type":    "instant",
			"count":   len(samples),
			"samples": samples,
		}

	case model.Matrix:
		// 范围查询结果
		series := make([]map[string]interface{}, 0, len(v))
		for _, stream := range v {
			values := make([]map[string]interface{}, 0, len(stream.Values))
			for _, pair := range stream.Values {
				values = append(values, map[string]interface{}{
					"timestamp": pair.Timestamp.Time().Format("2006-01-02 15:04:05"),
					"value":     float64(pair.Value),
				})
			}

			// 计算统计信息
			stats := t.calculateStats(stream.Values)

			series = append(series, map[string]interface{}{
				"metric": stream.Metric.String(),
				"values": values,
				"stats":  stats,
			})
		}
		return map[string]interface{}{
			"type":   "range",
			"count":  len(series),
			"series": series,
		}

	default:
		return map[string]interface{}{
			"type":  "unknown",
			"value": value.String(),
		}
	}
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
func (t *MetricsCollectorTool) fallbackResponse(query string) string {
	result := map[string]interface{}{
		"error":   "prometheus_client_unavailable",
		"message": fmt.Sprintf("Prometheus client not available. Cannot execute query: %s", query),
		"suggestion": "Please check Prometheus configuration and ensure it's running",
	}

	output, _ := json.Marshal(result)
	return string(output)
}

