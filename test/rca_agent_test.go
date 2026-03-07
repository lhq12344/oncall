package test

import (
	"context"
	"testing"

	"go_agent/internal/agent/rca"
	"go_agent/internal/agent/rca/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestBuildDependencyGraphTool 测试依赖图构建工具
func TestBuildDependencyGraphTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	baseTool := tools.NewBuildDependencyGraphTool(logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "build_dependency_graph", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("BuildGraph", func(t *testing.T) {
		input := `{
			"services": [
				{"name": "api-gateway", "type": "service", "dependencies": ["user-service", "payment-service"]},
				{"name": "user-service", "type": "service", "dependencies": ["database"]},
				{"name": "payment-service", "type": "service", "dependencies": ["database"]},
				{"name": "database", "type": "database", "dependencies": []}
			]
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("MissingServices", func(t *testing.T) {
		input := `{"auto_discover": true}`
		result, err := invokableTool.InvokableRun(ctx, input)
		// 自动发现模式下，即使没有提供 services 也应该成功
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})
}

// TestCorrelateSignalsTool 测试信号关联工具
func TestCorrelateSignalsTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	baseTool := tools.NewCorrelateSignalsTool(logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "correlate_signals", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("CorrelateSignals", func(t *testing.T) {
		input := `{
			"signals": [
				{"source": "alert", "service": "api-gateway", "type": "cpu_high", "timestamp": "2026-03-07T10:00:00Z", "message": "High CPU", "severity": "warning"},
				{"source": "log", "service": "api-gateway", "type": "error", "timestamp": "2026-03-07T10:00:05Z", "message": "Connection timeout", "severity": "error"},
				{"source": "metric", "service": "user-service", "type": "latency", "timestamp": "2026-03-07T10:00:10Z", "message": "Latency spike", "severity": "warning"}
			],
			"time_window": 300
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("MissingSignals", func(t *testing.T) {
		input := `{"time_window": "5m"}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
	})
}

// TestInferRootCauseTool 测试根因推理工具
func TestInferRootCauseTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	chatModel, err := models.GetChatModel()
	if err != nil {
		t.Skip("Skipping test: ChatModel initialization failed")
	}

	baseTool := tools.NewInferRootCauseTool(chatModel, logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "infer_root_cause", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("InferRootCause", func(t *testing.T) {
		input := `{
			"fault_node": "api-gateway",
			"dependency_graph": {
				"nodes": {
					"api-gateway": {"name": "api-gateway", "dependencies": ["user-service"]},
					"user-service": {"name": "user-service", "dependencies": ["database"]}
				}
			},
			"correlations": [
				{"signal1": {"service": "api-gateway", "type": "error"}, "signal2": {"service": "user-service", "type": "latency"}, "correlation": 0.8}
			]
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("MissingData", func(t *testing.T) {
		input := `{}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
	})
}

// TestAnalyzeImpactTool 测试影响分析工具
func TestAnalyzeImpactTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	baseTool := tools.NewAnalyzeImpactTool(logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "analyze_impact", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("AnalyzeImpact", func(t *testing.T) {
		input := `{
			"root_cause": "database connection pool exhausted",
			"affected_service": "user-service",
			"dependency_graph": {"nodes": ["api-gateway", "user-service", "payment-service"], "edges": [["api-gateway", "user-service"], ["api-gateway", "payment-service"]]}
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("MissingRootCause", func(t *testing.T) {
		input := `{"affected_service": "user-service"}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
	})
}

// TestRCAAgent 测试根因分析代理
func TestRCAAgent(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	t.Run("NilConfig", func(t *testing.T) {
		_, err := rca.NewRCAAgent(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config is required")
	})

	t.Run("NilChatModel", func(t *testing.T) {
		cfg := &rca.Config{
			Logger: logger,
		}
		_, err := rca.NewRCAAgent(ctx, cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chat model is required")
	})

	t.Run("ValidConfig", func(t *testing.T) {
		chatModel, err := models.GetChatModel()
		if err != nil {
			t.Skip("Skipping test: ChatModel initialization failed")
		}

		cfg := &rca.Config{
			ChatModel: chatModel,
			Logger:    logger,
		}

		agent, err := rca.NewRCAAgent(ctx, cfg)
		require.NoError(t, err)
		assert.NotNil(t, agent)
	})
}
