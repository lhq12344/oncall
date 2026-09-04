package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	promadapter "go_agent/internal/adapters/prometheus"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type metricCollector interface {
	Available() bool
	DiscoverMetricSources(context.Context, string, int) (interface{}, error)
	Query(context.Context, string, time.Time, time.Time, time.Time, bool) (interface{}, error)
}

type MetricsCollectorTool struct {
	collector metricCollector
	logger    *zap.Logger
}

func NewMetricsCollectorTool(prometheusURL string, logger *zap.Logger) (tool.BaseTool, error) {
	return NewMetricsCollectorToolWithCollector(promadapter.NewCollector(prometheusURL, logger), logger), nil
}

func NewMetricsCollectorToolWithCollector(collector metricCollector, logger *zap.Logger) tool.BaseTool {
	return &MetricsCollectorTool{collector: collector, logger: logger}
}

func (t *MetricsCollectorTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "metrics_collector",
		Desc: "Collect Prometheus metrics with PromQL, or discover available scrape targets, labels, and metric metadata.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {
				Type:     schema.String,
				Desc:     "Action: query by default, or discover_sources.",
				Required: false,
			},
			"query": {
				Type:     schema.String,
				Desc:     "PromQL query. Required when action=query.",
				Required: false,
			},
			"time_range": {
				Type:     schema.String,
				Desc:     "Optional time range such as 5m, 1h, or 24h. Empty means instant query.",
				Required: false,
			},
			"metric": {
				Type:     schema.String,
				Desc:     "Optional metric name filter for metadata discovery.",
				Required: false,
			},
			"limit": {
				Type:     schema.Integer,
				Desc:     "Discovery sample limit. Defaults to 20 and caps at 100.",
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
				zap.String("agent", currentAgentForLog(ctx, "ops_incident_agent")),
				zap.String("action", in.Action),
				zap.String("query", in.Query),
				zap.String("time_range", in.TimeRange),
				zap.Int("call_count", callCount))
		}
		return cached, nil
	}

	if t.collector == nil || !t.collector.Available() {
		output := t.fallbackResponse(in.Action, in.Query)
		setCachedToolResult(ctx, "metrics_collector", cacheKey, output)
		return output, nil
	}

	if in.Action == "discover_sources" {
		result, err := t.collector.DiscoverMetricSources(ctx, in.Metric, in.Limit)
		if err != nil {
			if t.logger != nil {
				t.logger.Error("prometheus source discovery failed",
					zap.String("agent", currentAgentForLog(ctx, "ops_incident_agent")),
					zap.String("metric", in.Metric),
					zap.Error(err))
			}
			return "", fmt.Errorf("failed to discover metric sources: %w", err)
		}
		output, err := marshalMetricsResult(result, "discovery")
		if err != nil {
			return "", err
		}
		if t.logger != nil {
			t.logger.Info("metrics source discovery completed",
				zap.String("agent", currentAgentForLog(ctx, "ops_incident_agent")),
				zap.String("metric", in.Metric),
				zap.Int("call_count", callCount))
		}
		setCachedToolResult(ctx, "metrics_collector", cacheKey, output)
		return output, nil
	}

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

	result, err := t.collector.Query(ctx, in.Query, queryTime, startTime, endTime, isRange)
	if err != nil {
		if t.logger != nil {
			t.logger.Error("prometheus query failed",
				zap.String("agent", currentAgentForLog(ctx, "ops_incident_agent")),
				zap.String("query", in.Query),
				zap.Error(err))
		}
		return "", fmt.Errorf("failed to query metrics: %w", err)
	}

	output, err := marshalMetricsResult(result, "result")
	if err != nil {
		return "", err
	}
	if t.logger != nil {
		t.logger.Info("metrics collection completed",
			zap.String("agent", currentAgentForLog(ctx, "ops_incident_agent")),
			zap.String("action", in.Action),
			zap.String("query", in.Query),
			zap.Bool("is_range", isRange),
			zap.Int("call_count", callCount))
	}

	setCachedToolResult(ctx, "metrics_collector", cacheKey, output)
	return output, nil
}

func marshalMetricsResult(result interface{}, label string) (string, error) {
	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal %s: %w", label, err)
	}
	return string(output), nil
}

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
