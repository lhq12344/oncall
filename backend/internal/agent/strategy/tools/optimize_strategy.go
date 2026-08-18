package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"

	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// OptimizeStrategyTool 策略优化工具
type OptimizeStrategyTool struct {
	chatModel *models.ChatModel
	logger    *zap.Logger
}

// OptimizationResult 优化结果
type OptimizationResult struct {
	OriginalStrategy    interface{}        `json:"original_strategy"`
	OptimizedStrategy   interface{}        `json:"optimized_strategy"`
	Changes             []string           `json:"changes"`
	ExpectedImprovement map[string]float64 `json:"expected_improvement"`
	Reasoning           string             `json:"reasoning"`
}

func NewOptimizeStrategyTool(chatModel *models.ChatModel, logger *zap.Logger) tool.BaseTool {
	return &OptimizeStrategyTool{
		chatModel: chatModel,
		logger:    logger,
	}
}

func (t *OptimizeStrategyTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "optimize_strategy",
		Desc: "优化执行策略。识别低效步骤，建议并行化、简化或参数调优。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"strategy": {
				Type:     schema.Object,
				Desc:     "当前策略（JSON 对象）",
				Required: true,
			},
			"evaluation": {
				Type:     schema.Object,
				Desc:     "策略评估结果（JSON 对象）",
				Required: true,
			},
		}),
	}, nil
}

func (t *OptimizeStrategyTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Strategy   map[string]interface{} `json:"strategy"`
		Evaluation map[string]interface{} `json:"evaluation"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if in.Strategy == nil {
		return "", fmt.Errorf("strategy is required")
	}

	// 1. 使用 LLM 优化策略
	result, err := t.optimizeWithLLM(ctx, in.Strategy, in.Evaluation)
	if err != nil {
		if t.logger != nil {
			t.logger.Warn("LLM optimization failed, using rule-based optimization",
				zap.Error(err))
		}
		// 降级到规则优化
		result = t.optimizeWithRules(in.Strategy, in.Evaluation)
	}

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("strategy optimization completed",
			zap.String("agent", currentAgentForLog(ctx, "strategy_agent")),
			zap.Int("changes", len(result.Changes)))
	}

	return string(output), nil
}

// optimizeWithLLM 使用 LLM 优化策略
func (t *OptimizeStrategyTool) optimizeWithLLM(ctx context.Context, strategy, evaluation map[string]interface{}) (*OptimizationResult, error) {
	if t.chatModel == nil {
		return nil, fmt.Errorf("chat model not available")
	}

	strategyJSON, _ := json.Marshal(strategy)
	evaluationJSON, _ := json.Marshal(evaluation)

	prompt := fmt.Sprintf(`分析以下执行策略并提出优化建议：

当前策略：
%s

评估结果：
%s

请分析：
1. 哪些步骤可以并行执行？
2. 哪些步骤可以删除或简化？
3. 超时时间是否合理？
4. 是否有更高效的执行顺序？

返回 JSON 格式：
{
  "changes": ["变更1", "变更2"],
  "expected_improvement": {
    "success_rate": 0.05,
    "avg_duration": -5000
  },
  "reasoning": "优化理由"
}`, string(strategyJSON), string(evaluationJSON))

	resp, err := t.chatModel.Client.Generate(ctx, []*schema.Message{
		schema.UserMessage(prompt),
	})
	if err != nil {
		return nil, err
	}

	content := resp.Content
	if content == "" {
		return nil, fmt.Errorf("empty response from LLM")
	}

	// 解析 LLM 响应
	start := strings.Index(content, "{")
	end := strings.LastIndex(content, "}")
	if start >= 0 && end > start {
		jsonStr := content[start : end+1]

		var llmResult struct {
			Changes             []string           `json:"changes"`
			ExpectedImprovement map[string]float64 `json:"expected_improvement"`
			Reasoning           string             `json:"reasoning"`
		}

		if err := json.Unmarshal([]byte(jsonStr), &llmResult); err == nil {
			return &OptimizationResult{
				OriginalStrategy:    strategy,
				OptimizedStrategy:   strategy, // TODO: 应用优化
				Changes:             llmResult.Changes,
				ExpectedImprovement: llmResult.ExpectedImprovement,
				Reasoning:           llmResult.Reasoning,
			}, nil
		}
	}

	return nil, fmt.Errorf("failed to parse LLM response")
}

// optimizeWithRules 使用规则优化策略（降级方案）
func (t *OptimizeStrategyTool) optimizeWithRules(strategy, evaluation map[string]interface{}) *OptimizationResult {
	changes := []string{}
	expectedImprovement := make(map[string]float64)

	// 提取评估指标
	successRate := 0.0
	avgDuration := 0.0
	if eval, ok := evaluation["success_rate"].(float64); ok {
		successRate = eval
	}
	if eval, ok := evaluation["avg_duration"].(float64); ok {
		avgDuration = eval
	}

	// 规则 1：成功率低，增加重试
	if successRate < 0.8 {
		changes = append(changes, "增加关键步骤的重试次数")
		expectedImprovement["success_rate"] = 0.1
	}

	// 规则 2：执行时间长，优化超时
	if avgDuration > 30000 {
		changes = append(changes, "优化超时时间，识别可并行步骤")
		expectedImprovement["avg_duration"] = -5000
	}

	// 规则 3：通用优化
	if len(changes) == 0 {
		changes = append(changes, "策略已优化，建议保持当前配置")
	}

	return &OptimizationResult{
		OriginalStrategy:    strategy,
		OptimizedStrategy:   strategy,
		Changes:             changes,
		ExpectedImprovement: expectedImprovement,
		Reasoning:           "基于规则的优化建议",
	}
}
