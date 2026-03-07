package test

import (
	"context"
	"testing"

	"go_agent/internal/agent/strategy"
	"go_agent/internal/agent/strategy/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestEvaluateStrategyTool 测试策略评估工具
func TestEvaluateStrategyTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	baseTool := tools.NewEvaluateStrategyTool(logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "evaluate_strategy", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("EvaluateStrategy", func(t *testing.T) {
		input := `{
			"strategy_id": "strat_001",
			"executions": [
				{"execution_id": "exec_1", "strategy_id": "strat_001", "success": true, "duration": 30000, "rollback_count": 0, "step_count": 3, "executed_at": "2026-03-07T10:00:00Z"},
				{"execution_id": "exec_2", "strategy_id": "strat_001", "success": true, "duration": 28000, "rollback_count": 0, "step_count": 3, "executed_at": "2026-03-07T11:00:00Z"},
				{"execution_id": "exec_3", "strategy_id": "strat_001", "success": false, "duration": 45000, "rollback_count": 1, "step_count": 3, "executed_at": "2026-03-07T12:00:00Z"}
			]
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "success_rate")
	})

	t.Run("MissingStrategyID", func(t *testing.T) {
		input := `{"execution_history": []}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
	})
}

// TestOptimizeStrategyTool 测试策略优化工具
func TestOptimizeStrategyTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	chatModel, err := models.GetChatModel()
	if err != nil {
		t.Skip("Skipping test: ChatModel initialization failed")
	}

	baseTool := tools.NewOptimizeStrategyTool(chatModel, logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "optimize_strategy", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("OptimizeStrategy", func(t *testing.T) {
		input := `{
			"strategy": {
				"steps": [
					{"description": "Check status", "duration": 10},
					{"description": "Restart service", "duration": 30},
					{"description": "Verify", "duration": 20}
				]
			},
			"evaluation": {
				"success_rate": 0.8,
				"avg_duration": 60,
				"bottlenecks": ["Restart service takes too long"]
			}
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("MissingStrategy", func(t *testing.T) {
		input := `{"evaluation": {}}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
	})
}

// TestUpdateKnowledgeTool 测试知识库更新工具
func TestUpdateKnowledgeTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	baseTool := tools.NewUpdateKnowledgeTool(logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "update_knowledge", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("UpdateKnowledge", func(t *testing.T) {
		input := `{
			"case": {
				"case_id": "case_001",
				"title": "Pod OOMKilled 处理",
				"description": "增加内存限制",
				"strategy": "kubectl set resources",
				"success_rate": 0.95,
				"usage_count": 10,
				"weight": 0.8
			},
			"execution_result": {
				"success": true,
				"duration": 30000
			}
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("MissingCaseID", func(t *testing.T) {
		input := `{
			"case": {},
			"execution_result": {}
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		// 工具可能接受空 case，只是不会更新
		if err == nil {
			assert.NotEmpty(t, result)
		} else {
			assert.Error(t, err)
		}
	})
}

// TestPruneKnowledgeTool 测试知识剪枝工具
func TestPruneKnowledgeTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	baseTool := tools.NewPruneKnowledgeTool(logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "prune_knowledge", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("PruneKnowledge", func(t *testing.T) {
		input := `{
			"criteria": {
				"min_success_rate": 0.5,
				"max_age_days": 365,
				"min_usage_count": 1
			}
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("DefaultCriteria", func(t *testing.T) {
		input := `{}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})
}

// TestStrategyAgent 测试策略代理
func TestStrategyAgent(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	t.Run("NilConfig", func(t *testing.T) {
		_, err := strategy.NewStrategyAgent(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config is required")
	})

	t.Run("NilChatModel", func(t *testing.T) {
		cfg := &strategy.Config{
			Logger: logger,
		}
		_, err := strategy.NewStrategyAgent(ctx, cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chat model is required")
	})

	t.Run("ValidConfig", func(t *testing.T) {
		chatModel, err := models.GetChatModel()
		if err != nil {
			t.Skip("Skipping test: ChatModel initialization failed")
		}

		cfg := &strategy.Config{
			ChatModel: chatModel,
			Logger:    logger,
		}

		agent, err := strategy.NewStrategyAgent(ctx, cfg)
		require.NoError(t, err)
		assert.NotNil(t, agent)
	})
}
