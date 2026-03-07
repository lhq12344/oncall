package test

import (
	"context"
	"strings"
	"testing"

	"go_agent/internal/agent/dialogue"
	"go_agent/internal/agent/dialogue/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/components/tool"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
)

// TestIntentAnalysisTool 测试意图分析工具
func TestIntentAnalysisTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	chatModel, err := models.GetChatModel()
	if err != nil {
		t.Skip("Skipping test: ChatModel initialization failed")
	}

	baseTool := tools.NewIntentAnalysisTool(chatModel, nil, logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "intent_analysis", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("MonitorIntent", func(t *testing.T) {
		input := `{"user_input": "查看 Pod CPU 使用率"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "monitor")
	})

	t.Run("DiagnoseIntent", func(t *testing.T) {
		input := `{"user_input": "服务报错了，需要诊断问题"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "diagnose")
	})

	t.Run("KnowledgeIntent", func(t *testing.T) {
		input := `{"user_input": "查询历史故障案例"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "knowledge")
	})

	t.Run("ExecuteIntent", func(t *testing.T) {
		input := `{"user_input": "执行重启 nginx Pod 的操作"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		// 可能被识别为 execute 或 monitor，都是合理的
		assert.True(t,
			strings.Contains(result, "execute") || strings.Contains(result, "monitor"),
			"Should contain execute or monitor intent")
	})

	t.Run("GeneralIntent", func(t *testing.T) {
		input := `{"user_input": "你好"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.Contains(t, result, "general")
	})

	t.Run("VagueInput", func(t *testing.T) {
		input := `{"user_input": "有问题"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)

		// 模糊输入应该有较高的熵和较低的置信度
		assert.Contains(t, result, "entropy")
		assert.Contains(t, result, "confidence")
	})

	t.Run("EmptyInput", func(t *testing.T) {
		input := `{"user_input": ""}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "user_input is required")
	})

	t.Run("InvalidJSON", func(t *testing.T) {
		input := `{invalid}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
	})

	t.Run("DetailedInput", func(t *testing.T) {
		input := `{"user_input": "生产环境的 payment-service Pod 在过去 1 小时内 CPU 使用率持续超过 90%，需要查看详细指标"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)

		// 详细输入应该有较低的熵和较高的置信度
		assert.Contains(t, result, "monitor")
		assert.Contains(t, result, "confidence")
	})

	t.Run("MissingInfo", func(t *testing.T) {
		input := `{"user_input": "查看监控"}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)

		// 应该识别出缺失的信息
		assert.Contains(t, result, "missing_info")
	})
}

// TestQuestionPredictionTool 测试问题预测工具
func TestQuestionPredictionTool(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	chatModel, err := models.GetChatModel()
	if err != nil {
		t.Skip("Skipping test: ChatModel initialization failed")
	}

	baseTool := tools.NewQuestionPredictionTool(chatModel, logger)
	invokableTool, ok := baseTool.(tool.InvokableTool)
	require.True(t, ok)

	t.Run("Info", func(t *testing.T) {
		info, err := baseTool.Info(ctx)
		require.NoError(t, err)
		assert.Equal(t, "question_prediction", info.Name)
		assert.NotEmpty(t, info.Desc)
	})

	t.Run("ValidInput", func(t *testing.T) {
		input := `{
			"context": "用户询问 Pod CPU 使用率",
			"intent": "monitor",
			"history": ["查看 Pod 状态"]
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("MissingContext", func(t *testing.T) {
		input := `{"intent": "monitor"}`
		_, err := invokableTool.InvokableRun(ctx, input)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "context is required")
	})

	t.Run("WithHistory", func(t *testing.T) {
		input := `{
			"context": "用户询问故障原因",
			"intent": "diagnose",
			"history": ["查看日志", "检查指标"]
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})

	t.Run("WithoutHistory", func(t *testing.T) {
		input := `{
			"context": "用户询问如何重启服务",
			"intent": "execute"
		}`
		result, err := invokableTool.InvokableRun(ctx, input)
		require.NoError(t, err)
		assert.NotEmpty(t, result)
	})
}

// TestDialogueAgent 测试对话代理
func TestDialogueAgent(t *testing.T) {
	ctx := context.Background()
	logger := zap.NewNop()

	t.Run("NilConfig", func(t *testing.T) {
		_, err := dialogue.NewDialogueAgent(ctx, nil)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "config is required")
	})

	t.Run("NilChatModel", func(t *testing.T) {
		cfg := &dialogue.Config{
			Logger: logger,
		}
		_, err := dialogue.NewDialogueAgent(ctx, cfg)
		assert.Error(t, err)
		assert.Contains(t, err.Error(), "chat model is required")
	})

	t.Run("ValidConfig", func(t *testing.T) {
		chatModel, err := models.GetChatModel()
		if err != nil {
			t.Skip("Skipping test: ChatModel initialization failed")
		}

		cfg := &dialogue.Config{
			ChatModel: chatModel,
			Logger:    logger,
		}

		agent, err := dialogue.NewDialogueAgent(ctx, cfg)
		require.NoError(t, err)
		assert.NotNil(t, agent)
	})
}

// TestDialogueState 测试对话状态
func TestDialogueState(t *testing.T) {
	t.Run("InitialState", func(t *testing.T) {
		state := &dialogue.DialogueState{
			CurrentIntent:  "monitor",
			IntentHistory:  []string{},
			Confidence:     0.8,
			Entropy:        0.3,
			Converged:      true,
			ContextSummary: "用户询问 Pod 状态",
			MissingInfo:    []string{},
			Metadata:       make(map[string]interface{}),
		}

		assert.Equal(t, "monitor", state.CurrentIntent)
		assert.Empty(t, state.IntentHistory)
		assert.Equal(t, 0.8, state.Confidence)
		assert.Equal(t, 0.3, state.Entropy)
		assert.True(t, state.Converged)
		assert.Equal(t, "用户询问 Pod 状态", state.ContextSummary)
		assert.Empty(t, state.MissingInfo)
		assert.NotNil(t, state.Metadata)
	})

	t.Run("StateTransition", func(t *testing.T) {
		state := &dialogue.DialogueState{
			CurrentIntent: "monitor",
			IntentHistory: []string{"general"},
			Confidence:    0.9,
			Entropy:       0.2,
			Converged:     true,
		}

		// 模拟意图转换
		state.IntentHistory = append(state.IntentHistory, state.CurrentIntent)
		state.CurrentIntent = "diagnose"
		state.Confidence = 0.85
		state.Entropy = 0.1
		state.Converged = false

		assert.Equal(t, "diagnose", state.CurrentIntent)
		assert.Contains(t, state.IntentHistory, "monitor")
		assert.Equal(t, 0.85, state.Confidence)
		assert.Equal(t, 0.1, state.Entropy)
		assert.False(t, state.Converged)
	})
}
