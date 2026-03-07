package test

import (
	"context"
	"testing"

	"go_agent/internal/agent/ops"
	"go_agent/internal/agent/ops/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestK8sMonitorTool 测试 K8s 监控工具
func TestK8sMonitorTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	// 创建工具（可能会降级）
	baseTool, err := tools.NewK8sMonitorTool("", logger)
	require.NoError(t, err)
	require.NotNil(t, baseTool)

	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "k8s_monitor", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("QueryPods", func(t *testing.T) {
		input := `{"namespace": "default", "resource_type": "pod"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("QuerySpecificPod", func(t *testing.T) {
		input := `{"namespace": "default", "resource_type": "pod", "resource_name": "test-pod"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("QueryNodes", func(t *testing.T) {
		input := `{"resource_type": "node"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("QueryDeployments", func(t *testing.T) {
		input := `{"namespace": "default", "resource_type": "deployment"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("QueryServices", func(t *testing.T) {
		input := `{"namespace": "default", "resource_type": "service"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("MissingResourceType", func(t *testing.T) {
		input := `{"namespace": "default"}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "resource_type is required")
	})

	t.Run("UnsupportedResourceType", func(t *testing.T) {
		input := `{"resource_type": "invalid"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		// 在降级模式下，工具会返回错误信息而不是抛出错误
		if err == nil {
			assert.Contains(t, result, "error")
		} else {
			assert.Error(t, err)
		}
	})

	t.Run("DefaultNamespace", func(t *testing.T) {
		input := `{"resource_type": "pod"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})
}

// TestMetricsCollectorTool 测试指标采集工具
func TestMetricsCollectorTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	// 创建工具（可能会降级）
	baseTool, err := tools.NewMetricsCollectorTool("", logger)
	require.NoError(t, err)
	require.NotNil(t, baseTool)

	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "metrics_collector", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("InstantQuery", func(t *testing.T) {
		input := `{"query": "up"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		// Prometheus 可能未运行，检查降级响应
		if err != nil {
			t.Logf("Prometheus not available (expected): %v", err)
			return
		}
		assert.NotEmpty(t, result)
	})

	t.Run("RangeQuery", func(t *testing.T) {
		input := `{"query": "rate(http_requests_total[5m])", "time_range": "1h"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		if err != nil {
			t.Logf("Prometheus not available (expected): %v", err)
			return
		}
		assert.NotEmpty(t, result)
	})

	t.Run("CPUQuery", func(t *testing.T) {
		input := `{"query": "container_cpu_usage_seconds_total"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		if err != nil {
			t.Logf("Prometheus not available (expected): %v", err)
			return
		}
		assert.NotEmpty(t, result)
	})

	t.Run("MemoryQuery", func(t *testing.T) {
		input := `{"query": "container_memory_usage_bytes"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		if err != nil {
			t.Logf("Prometheus not available (expected): %v", err)
			return
		}
		assert.NotEmpty(t, result)
	})

	t.Run("MissingQuery", func(t *testing.T) {
		input := `{"time_range": "1h"}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "query is required")
	})

	t.Run("InvalidTimeRange", func(t *testing.T) {
		input := `{"query": "up", "time_range": "invalid"}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
	})

	t.Run("ValidTimeRanges", func(t *testing.T) {
		timeRanges := []string{"5m", "1h", "24h", "7d"}
		for _, tr := range timeRanges {
			input := `{"query": "up", "time_range": "` + tr + `"}`
			result, err := invokableTool.InvokableRun(ctx, input)
			if err != nil {
				t.Logf("Prometheus not available for time range %s (expected): %v", tr, err)
				continue
			}
			assert.NotEmpty(t, result)
		}
	})
}

// TestLogAnalyzerTool 测试日志分析工具
func TestLogAnalyzerTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	baseTool := tools.NewLogAnalyzerTool(logger)
	require.NotNil(t, baseTool)

	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "log_analyzer", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("AnalyzeLogs", func(t *testing.T) {
		input := `{
			"source": "payment-service",
			"time_range": "1h",
			"level": "error"
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "source")
	})

	t.Run("EmptyLogs", func(t *testing.T) {
		input := `{"logs": []}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
	})

	t.Run("MissingLogs", func(t *testing.T) {
		input := `{}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
	})
}

// TestESLogQueryTool 测试 ES 日志查询工具
func TestESLogQueryTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	baseTool, err := tools.NewESLogQueryTool(logger)
	require.NoError(t, err)
	require.NotNil(t, baseTool)

	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "es_log_query", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("QueryWithKeyword", func(t *testing.T) {
		input := `{
			"index": "logs-*",
			"query": "error",
			"time_range": "1h",
			"size": 100
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("QueryWithLevel", func(t *testing.T) {
		input := `{
			"index": "logs-*",
			"query": "timeout",
			"level": "ERROR",
			"time_range": "30m"
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("QueryWithService", func(t *testing.T) {
		input := `{
			"index": "payment-service-*",
			"query": "exception",
			"time_range": "2h"
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("MissingKeyword", func(t *testing.T) {
		input := `{"time_range": "1h"}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
	})
}

// TestOpsAgent 测试运维代理
func TestOpsAgent(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	t.Run("NilConfig", func(t *testing.T) {
		_, err := ops.NewOpsAgent(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config is required")
	})

	t.Run("NilChatModel", func(t *testing.T) {
		cfg := &ops.Config{
			Logger: logger,
		}
		_, err := ops.NewOpsAgent(ctx, cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chat model is required")
	})

	t.Run("ValidConfig", func(t *testing.T) {
		chatModel, err := models.GetChatModel()
		if err != nil {
			t.Skip("Skipping test: ChatModel initialization failed")
		}

		cfg := &ops.Config{
			ChatModel:     chatModel,
			KubeConfig:    "",
			PrometheusURL: "",
			Logger:        logger,
		}

		agent, err := ops.NewOpsAgent(ctx, cfg)
		// Agent 可能会创建成功（即使工具降级）
		if err != nil {
			t.Logf("Agent creation failed: %v", err)
			return
		}

		assert.NotNil(t, agent)
	})
}
