package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	es "go_agent/internal/adapters/elasticsearch"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

type logSearcher interface {
	Available() bool
	Ensure(context.Context) error
	DiscoverIndices(context.Context, string, int) (map[string]interface{}, error)
	QueryLogs(context.Context, string, string, string, string, int) (map[string]interface{}, error)
}

// ESLogQueryTool keeps the agent-facing tool schema separate from the ES SDK adapter.
type ESLogQueryTool struct {
	searcher logSearcher
	logger   *zap.Logger
}

func NewESLogQueryTool(logger *zap.Logger) (tool.BaseTool, error) {
	if logger != nil {
		logger.Debug("elasticsearch log query tool will use lazy adapter initialization")
	}
	return NewESLogQueryToolWithSearcher(es.NewLazyClient(), logger), nil
}

func NewESLogQueryToolWithSearcher(searcher logSearcher, logger *zap.Logger) tool.BaseTool {
	if searcher == nil {
		searcher = es.NewLazyClient()
	}
	return &ESLogQueryTool{searcher: searcher, logger: logger}
}

func (t *ESLogQueryTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "es_log_query",
		Desc: "Query Elasticsearch logs, or discover currently available log indices.",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"action": {
				Type:     schema.String,
				Desc:     "Action: query by default, or discover_indices. Aliases such as discover/list_indices are accepted.",
				Required: false,
			},
			"index": {
				Type:     schema.String,
				Desc:     "Index name or pattern, such as logs-* or app-logs-2024.03.*. Defaults to * for discovery and logs-* for query.",
				Required: false,
			},
			"query": {
				Type:     schema.String,
				Desc:     "Search keyword or Lucene query string used by action=query.",
				Required: false,
			},
			"time_range": {
				Type:     schema.String,
				Desc:     "Time range such as 5m, 1h, or 24h. Defaults to 1h for action=query.",
				Required: false,
			},
			"level": {
				Type:     schema.String,
				Desc:     "Optional log level filter: error, warn, info, or debug.",
				Required: false,
			},
			"size": {
				Type:     schema.Integer,
				Desc:     "Maximum result count or index sample count. Defaults to 100 and caps at 1000.",
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

	rawAction := strings.TrimSpace(in.Action)
	in.Action = normalizeESLogAction(in.Action)
	if in.Action == "" {
		return "", fmt.Errorf("unsupported action: %s", rawAction)
	}

	in.Index = strings.TrimSpace(in.Index)
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

	if t.searcher != nil {
		if err := t.searcher.Ensure(ctx); err != nil && t.logger != nil {
			t.logger.Warn("elasticsearch lazy init failed, fallback mode enabled", zap.Error(err))
		}
	}
	if t.searcher == nil || !t.searcher.Available() {
		return t.fallbackResponse(in.Index, in.Query, in.TimeRange, in.Level, in.Action), nil
	}

	if in.Action == "discover_indices" {
		result, err := t.searcher.DiscoverIndices(ctx, in.Index, in.Size)
		if err != nil {
			if t.logger != nil {
				t.logger.Error("es discover indices failed", zap.String("pattern", in.Index), zap.Error(err))
			}
			return "", fmt.Errorf("failed to discover indices: %w", err)
		}
		return marshalESLogResult(result)
	}

	result, err := t.searcher.QueryLogs(ctx, in.Index, in.Query, in.TimeRange, in.Level, in.Size)
	if err != nil {
		if t.logger != nil {
			t.logger.Error("es log query failed", zap.String("index", in.Index), zap.String("query", in.Query), zap.Error(err))
		}
		return "", fmt.Errorf("failed to query logs: %w", err)
	}

	if t.logger != nil {
		hits, _ := result["total_hits"].(int)
		t.logger.Info("es log query completed", zap.String("index", in.Index), zap.String("query", in.Query), zap.Int("hits", hits))
	}
	return marshalESLogResult(result)
}

// normalizeESLogAction converts common model-generated aliases to canonical actions.
func normalizeESLogAction(raw string) string {
	original := strings.TrimSpace(raw)
	if original == "" {
		return "query"
	}

	normalized := strings.ToLower(original)
	normalized = strings.NewReplacer("-", "_", " ", "_").Replace(normalized)
	for strings.Contains(normalized, "__") {
		normalized = strings.ReplaceAll(normalized, "__", "_")
	}

	switch normalized {
	case "query", "search", "find", "lookup":
		return "query"
	case "discover_indices", "discover", "list_indices", "list_indexes", "indices", "index_discovery":
		return "discover_indices"
	default:
		return ""
	}
}

func marshalESLogResult(result map[string]interface{}) (string, error) {
	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}
	return string(output), nil
}

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
