package tools

import (
	"context"
	"encoding/json"
	"testing"

	"go.uber.org/zap"
)

func TestESLogQueryTool_Fallback(t *testing.T) {
	// 测试降级模式（ES 不可用）
	logger, _ := zap.NewDevelopment()
	baseTool, err := NewESLogQueryTool(logger)
	if err != nil {
		t.Fatalf("failed to create tool: %v", err)
	}

	// 类型断言获取实际工具
	tool, ok := baseTool.(*ESLogQueryTool)
	if !ok {
		t.Fatalf("failed to cast to ESLogQueryTool")
	}

	// 测试查询
	args := map[string]interface{}{
		"index":      "logs-*",
		"query":      "error",
		"time_range": "1h",
		"level":      "error",
		"size":       100,
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

	// 验证降级响应
	if response["error"] != "elasticsearch_unavailable" {
		t.Errorf("expected fallback response, got: %v", response)
	}

	t.Logf("Fallback response: %s", result)
}

func TestESLogQueryTool_Info(t *testing.T) {
	logger, _ := zap.NewDevelopment()
	tool, err := NewESLogQueryTool(logger)
	if err != nil {
		t.Fatalf("failed to create tool: %v", err)
	}

	info, err := tool.Info(context.Background())
	if err != nil {
		t.Fatalf("failed to get tool info: %v", err)
	}

	if info.Name != "es_log_query" {
		t.Errorf("expected tool name 'es_log_query', got: %s", info.Name)
	}

	t.Logf("Tool info: %+v", info)
}
