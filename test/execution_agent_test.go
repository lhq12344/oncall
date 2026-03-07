package test

import (
	"context"
	"testing"

	"go_agent/internal/agent/execution"
	"go_agent/internal/agent/execution/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestGeneratePlanTool 测试执行计划生成工具
func TestGeneratePlanTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	chatModel, err := models.GetChatModel()
	if err != nil {
		t.Skip("Skipping test: ChatModel initialization failed")
	}

	baseTool := tools.NewGeneratePlanTool(chatModel, logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "generate_plan", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("RestartPod", func(t *testing.T) {
		input := `{"intent": "重启 nginx Pod", "context": "namespace: default"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "plan_id")
		assert.Contains(t, result, "steps")
	})

	t.Run("ScaleDeployment", func(t *testing.T) {
		input := `{"intent": "扩容 deployment 到 5 个副本"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "scale")
	})

	t.Run("RestartService", func(t *testing.T) {
		input := `{"intent": "重启 nginx 服务"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "systemctl")
	})

	t.Run("MissingIntent", func(t *testing.T) {
		input := `{"context": "test"}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "intent is required")
	})

	t.Run("RiskAssessment", func(t *testing.T) {
		input := `{"intent": "删除所有 Pod"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		// 应该包含风险等级
		assert.Contains(t, result, "risk_level")
	})
}

// TestExecuteStepTool 测试执行步骤工具
func TestExecuteStepTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	baseTool := tools.NewExecuteStepTool(logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "execute_step", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("DryRun", func(t *testing.T) {
		input := `{
			"step_id": 1,
			"command": "echo",
			"args": ["hello"],
			"dry_run": true
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "DRY RUN")
	})

	t.Run("ExecuteEcho", func(t *testing.T) {
		input := `{
			"step_id": 1,
			"command": "echo",
			"args": ["test"],
			"timeout": 5
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "success")
	})

	t.Run("CommandNotInWhitelist", func(t *testing.T) {
		input := `{
			"step_id": 1,
			"command": "rm",
			"args": ["-rf", "/tmp/test"]
		}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "not in whitelist")
	})

	t.Run("DangerousArgs", func(t *testing.T) {
		input := `{
			"step_id": 1,
			"command": "echo",
			"args": ["test; rm -rf /"]
		}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "dangerous pattern")
	})

	t.Run("MissingCommand", func(t *testing.T) {
		input := `{"step_id": 1}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "command is required")
	})
}

// TestValidateResultTool 测试验证结果工具
func TestValidateResultTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	baseTool := tools.NewValidateResultTool(logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "validate_result", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("ValidateSuccess", func(t *testing.T) {
		input := `{
			"step_id": 1,
			"actual_output": "Pod is running",
			"expected_result": "Pod is running"
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "valid")
	})

	t.Run("ValidateFailure", func(t *testing.T) {
		input := `{
			"step_id": 1,
			"actual_output": "Error: Pod not found",
			"expected_result": "Pod is running"
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "valid")
	})

	t.Run("MissingFields", func(t *testing.T) {
		input := `{"step_id": 1}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
	})
}

// TestRollbackTool 测试回滚工具
func TestRollbackTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	baseTool := tools.NewRollbackTool(logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "rollback", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("RollbackStep", func(t *testing.T) {
		input := `{
			"step_id": 2,
			"rollback_steps": [
				{
					"step_id": 2,
					"command": "kubectl",
					"args": ["scale", "deployment", "nginx", "--replicas=3"]
				},
				{
					"step_id": 1,
					"command": "echo",
					"args": ["rollback step 1"]
				}
			],
			"reason": "Deployment failed"
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("MissingRollbackCommand", func(t *testing.T) {
		input := `{"step_id": 1}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
	})

	t.Run("DryRunRollback", func(t *testing.T) {
		input := `{
			"step_id": 1,
			"rollback_steps": [
				{
					"step_id": 1,
					"command": "echo",
					"args": ["rollback"]
				}
			],
			"reason": "Test rollback"
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})
}

// TestExecutionAgent 测试执行代理
func TestExecutionAgent(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	t.Run("NilConfig", func(t *testing.T) {
		_, err := execution.NewExecutionAgent(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config is required")
	})

	t.Run("NilChatModel", func(t *testing.T) {
		cfg := &execution.Config{
			Logger: logger,
		}
		_, err := execution.NewExecutionAgent(ctx, cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chat model is required")
	})

	t.Run("ValidConfig", func(t *testing.T) {
		chatModel, err := models.GetChatModel()
		if err != nil {
			t.Skip("Skipping test: ChatModel initialization failed")
		}

		cfg := &execution.Config{
			ChatModel: chatModel,
			Logger:    logger,
		}

		agent, err := execution.NewExecutionAgent(ctx, cfg)
		require.NoError(t, err)
		assert.NotNil(t, agent)
	})
}
