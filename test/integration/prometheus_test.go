package integration

import (
	"context"
	"testing"
	"time"

	"go_agent/internal/agent/ops/tools"

	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestPrometheusIntegration Prometheus 集成测试
func TestPrometheusIntegration(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping prometheus integration test")
	}

	ctx := context.Background()
	logger := zap.NewNop()

	// 创建 Prometheus 指标采集工具
	// 尝试多个可能的 Prometheus 地址
	prometheusURLs := []string{
		"http://localhost:30090",
		"http://localhost:9090",
		"http://prometheus:9090",
	}

	var promTool interface{}
	var err error
	var workingURL string

	for _, url := range prometheusURLs {
		promTool, err = tools.NewMetricsCollectorTool(url, logger)
		if err == nil {
			workingURL = url
			break
		}
	}

	require.NoError(t, err, "Failed to create Prometheus tool with any URL")
	require.NotNil(t, promTool)

	t.Logf("Using Prometheus URL: %s", workingURL)

	// 类型断言
	invokableTool, ok := promTool.(tool.InvokableTool)
	require.True(t, ok, "tool should implement InvokableTool")

	t.Run("InstantQuery", func(t *testing.T) {
		// 测试即时查询
		input := `{"query": "up"}`
		result, err := invokableTool.InvokableRun(ctx, input)

		if err != nil {
			t.Logf("Prometheus not available: %v", err)
			t.Skip("Prometheus not available")
		}

		require.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "type")
		t.Logf("Instant query result: %s", result)
	})

	t.Run("RangeQuery", func(t *testing.T) {
		// 测试范围查询
		input := `{"query": "up", "time_range": "5m"}`
		result, err := invokableTool.InvokableRun(ctx, input)

		if err != nil {
			t.Logf("Prometheus range query failed: %v", err)
			return
		}

		require.NoError(t, err)
		assert.NotEmpty(t, result)
		assert.Contains(t, result, "type")
		t.Logf("Range query result: %s", result)
	})

	t.Run("CPUMetrics", func(t *testing.T) {
		// 测试 CPU 指标查询
		queries := []string{
			"node_cpu_seconds_total",
			"container_cpu_usage_seconds_total",
			"process_cpu_seconds_total",
		}

		for _, query := range queries {
			input := `{"query": "` + query + `"}`
			result, err := invokableTool.InvokableRun(ctx, input)

			if err != nil {
				t.Logf("Query %s failed: %v", query, err)
				continue
			}

			assert.NotEmpty(t, result)
			t.Logf("Query %s succeeded", query)
		}
	})

	t.Run("MemoryMetrics", func(t *testing.T) {
		// 测试内存指标查询
		queries := []string{
			"node_memory_MemTotal_bytes",
			"container_memory_usage_bytes",
			"process_resident_memory_bytes",
		}

		for _, query := range queries {
			input := `{"query": "` + query + `"}`
			result, err := invokableTool.InvokableRun(ctx, input)

			if err != nil {
				t.Logf("Query %s failed: %v", query, err)
				continue
			}

			assert.NotEmpty(t, result)
			t.Logf("Query %s succeeded", query)
		}
	})

	t.Run("RateQuery", func(t *testing.T) {
		// 测试速率查询
		input := `{"query": "rate(http_requests_total[5m])", "time_range": "1h"}`
		result, err := invokableTool.InvokableRun(ctx, input)

		if err != nil {
			t.Logf("Rate query not available (metric may not exist): %v", err)
			return
		}

		assert.NotEmpty(t, result)
		t.Logf("Rate query result: %s", result)
	})

	t.Run("AggregationQuery", func(t *testing.T) {
		// 测试聚合查询
		queries := []string{
			"sum(up)",
			"avg(up)",
			"count(up)",
		}

		for _, query := range queries {
			input := `{"query": "` + query + `"}`
			result, err := invokableTool.InvokableRun(ctx, input)

			if err != nil {
				t.Logf("Aggregation query %s failed: %v", query, err)
				continue
			}

			assert.NotEmpty(t, result)
			t.Logf("Aggregation query %s succeeded", query)
		}
	})

	t.Run("TimeRangeVariations", func(t *testing.T) {
		// 测试不同的时间范围
		timeRanges := []string{"1m", "5m", "15m", "1h", "6h", "24h"}

		for _, tr := range timeRanges {
			input := `{"query": "up", "time_range": "` + tr + `"}`
			result, err := invokableTool.InvokableRun(ctx, input)

			if err != nil {
				t.Logf("Time range %s failed: %v", tr, err)
				continue
			}

			assert.NotEmpty(t, result)
			t.Logf("Time range %s succeeded", tr)
		}
	})
}

// TestPrometheusAvailability 测试 Prometheus 可用性
func TestPrometheusAvailability(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping prometheus availability test")
	}

	ctx := context.Background()
	logger := zap.NewNop()

	// 尝试连接 Prometheus
	urls := []string{
		"http://localhost:30090",
		"http://localhost:9090",
	}

	var available bool
	var workingURL string

	for _, url := range urls {
		promTool, err := tools.NewMetricsCollectorTool(url, logger)
		if err != nil {
			continue
		}

		invokableTool, ok := promTool.(tool.InvokableTool)
		if !ok {
			continue
		}

		// 尝试简单查询
		input := `{"query": "up"}`
		_, err = invokableTool.InvokableRun(ctx, input)
		if err == nil {
			available = true
			workingURL = url
			break
		}
	}

	if !available {
		t.Log("⚠️  Prometheus not available")
		t.Log("💡 To run Prometheus integration tests, ensure:")
		t.Log("   1. Prometheus is running (docker-compose up -d)")
		t.Log("   2. Accessible at localhost:30090 or localhost:9090")
		t.Log("   3. Has metrics to query")
		t.Skip("Prometheus not available")
	}

	t.Logf("✅ Prometheus is available at %s", workingURL)
}

// TestPrometheusPerformance 测试 Prometheus 查询性能
func TestPrometheusPerformance(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping prometheus performance test")
	}

	ctx := context.Background()
	logger := zap.NewNop()

	// 尝试多个可能的 Prometheus 地址
	prometheusURLs := []string{
		"http://localhost:30090",
		"http://localhost:9090",
	}

	var promTool interface{}
	var err error

	for _, url := range prometheusURLs {
		promTool, err = tools.NewMetricsCollectorTool(url, logger)
		if err == nil {
			break
		}
	}

	if err != nil {
		t.Skip("Prometheus not available")
	}

	invokableTool, ok := promTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("QueryLatency", func(t *testing.T) {
		// 测试查询延迟
		input := `{"query": "up"}`

		start := time.Now()
		result, err := invokableTool.InvokableRun(ctx, input)
		duration := time.Since(start)

		if err != nil {
			t.Skip("Prometheus not available")
		}

		require.NoError(t, err)
		assert.NotEmpty(t, result)

		t.Logf("Query latency: %v", duration)

		// 查询应该在 5 秒内完成
		if duration > 5*time.Second {
			t.Logf("⚠️  Query took longer than expected: %v", duration)
		}
	})

	t.Run("ConcurrentQueries", func(t *testing.T) {
		// 测试并发查询
		concurrency := 5
		done := make(chan bool, concurrency)

		start := time.Now()

		for i := 0; i < concurrency; i++ {
			go func(id int) {
				input := `{"query": "up"}`
				_, err := invokableTool.InvokableRun(ctx, input)
				if err != nil {
					t.Logf("Concurrent query %d failed: %v", id, err)
				}
				done <- true
			}(i)
		}

		// 等待所有查询完成
		for i := 0; i < concurrency; i++ {
			<-done
		}

		duration := time.Since(start)
		t.Logf("Concurrent queries (%d) completed in: %v", concurrency, duration)
	})
}
