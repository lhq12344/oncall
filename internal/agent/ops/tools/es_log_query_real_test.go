package tools

import (
	"context"
	"encoding/json"
	"testing"

	es "go_agent/utility/elasticsearch"
	"go.uber.org/zap"
)

func TestESLogQueryTool_RealQuery(t *testing.T) {
	// 初始化 Elasticsearch
	cfg := es.LoadElasticsearchConfigFromFile()
	_, err := es.InitElasticsearch(context.Background(), cfg)
	if err != nil {
		t.Skipf("Elasticsearch not available: %v", err)
	}

	// 创建工具
	logger, _ := zap.NewDevelopment()
	baseTool, err := NewESLogQueryTool(logger)
	if err != nil {
		t.Fatalf("failed to create tool: %v", err)
	}

	tool, ok := baseTool.(*ESLogQueryTool)
	if !ok {
		t.Fatalf("failed to cast to ESLogQueryTool")
	}

	// 测试查询
	args := map[string]interface{}{
		"index":      "logs-2024.03",
		"query":      "error",
		"time_range": "1h",
		"level":      "error",
		"size":       10,
	}

	argsJSON, _ := json.Marshal(args)
	result, err := tool.InvokableRun(context.Background(), string(argsJSON))
	if err != nil {
		t.Fatalf("query failed: %v", err)
	}

	// 解析结果
	var response map[string]interface{}
	if err := json.Unmarshal([]byte(result), &response); err != nil {
		t.Fatalf("failed to parse response: %v", err)
	}

	// 验证结果
	if response["error"] != nil {
		t.Errorf("query returned error: %v", response)
	}

	if response["total_hits"] == nil {
		t.Errorf("missing total_hits in response")
	}

	t.Logf("Query result: %s", result)
}
