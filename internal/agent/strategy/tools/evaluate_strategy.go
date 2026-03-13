package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// EvaluateStrategyTool 策略评估工具
type EvaluateStrategyTool struct {
	logger *zap.Logger
}

// StrategyExecution 策略执行记录
type StrategyExecution struct {
	ExecutionID   string    `json:"execution_id"`
	StrategyID    string    `json:"strategy_id"`
	Success       bool      `json:"success"`
	Duration      int       `json:"duration"` // 毫秒
	RollbackCount int       `json:"rollback_count"`
	StepCount     int       `json:"step_count"`
	ExecutedAt    time.Time `json:"executed_at"`
}

// StrategyEvaluation 策略评估结果
type StrategyEvaluation struct {
	StrategyID       string   `json:"strategy_id"`
	TotalExecutions  int      `json:"total_executions"`
	SuccessRate      float64  `json:"success_rate"`
	AvgDuration      float64  `json:"avg_duration"` // 毫秒
	AvgRollbackCount float64  `json:"avg_rollback_count"`
	Quality          string   `json:"quality"`         // excellent/good/fair/poor
	Bottlenecks      []string `json:"bottlenecks"`     // 瓶颈步骤
	Recommendations  []string `json:"recommendations"` // 优化建议
}

func NewEvaluateStrategyTool(logger *zap.Logger) tool.BaseTool {
	return &EvaluateStrategyTool{
		logger: logger,
	}
}

func (t *EvaluateStrategyTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "evaluate_strategy",
		Desc: "评估执行策略的质量。分析成功率、执行时长、回滚次数等指标，识别瓶颈和优化机会。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"strategy_id": {
				Type:     schema.String,
				Desc:     "策略 ID",
				Required: true,
			},
			"executions": {
				Type:     schema.Array,
				Desc:     "执行记录列表（JSON 数组）",
				Required: true,
			},
		}),
	}, nil
}

func (t *EvaluateStrategyTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		StrategyID string           `json:"strategy_id"`
		Executions []map[string]any `json:"executions"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	if in.StrategyID == "" {
		return "", fmt.Errorf("strategy_id is required")
	}

	if len(in.Executions) == 0 {
		return "", fmt.Errorf("executions is required")
	}
	executions := normalizeStrategyExecutions(in.StrategyID, in.Executions)
	if len(executions) == 0 {
		return "", fmt.Errorf("executions is required")
	}

	// 计算评估指标
	evaluation := t.evaluateStrategy(in.StrategyID, executions)

	output, err := json.Marshal(evaluation)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("strategy evaluation completed",
			zap.String("agent", currentAgentForLog(ctx, "strategy_agent")),
			zap.String("strategy_id", in.StrategyID),
			zap.Int("executions", len(executions)),
			zap.Float64("success_rate", evaluation.SuccessRate),
			zap.String("quality", evaluation.Quality))
	}

	return string(output), nil
}

// normalizeStrategyExecutions 规范化执行记录参数。
// 输入：strategyID 与原始 executions 参数。
// 输出：可用于评估计算的执行记录列表。
func normalizeStrategyExecutions(strategyID string, records []map[string]any) []StrategyExecution {
	if len(records) == 0 {
		return nil
	}

	now := time.Now()
	normalized := make([]StrategyExecution, 0, len(records))
	for index, record := range records {
		if len(record) == 0 {
			continue
		}

		execution := StrategyExecution{
			ExecutionID:   anyToString(record["execution_id"]),
			StrategyID:    anyToString(record["strategy_id"]),
			Success:       anyToBool(record["success"]),
			Duration:      anyToInt(record["duration"]),
			RollbackCount: anyToInt(record["rollback_count"]),
			StepCount:     anyToInt(record["step_count"]),
		}
		if execution.StrategyID == "" {
			execution.StrategyID = strings.TrimSpace(strategyID)
		}
		if execution.ExecutionID == "" {
			execution.ExecutionID = fmt.Sprintf("execution_%d", index+1)
		}

		if executedAt, ok := parseFlexibleTimeArg(record["executed_at"]); ok {
			execution.ExecutedAt = executedAt
		} else {
			execution.ExecutedAt = now
		}

		normalized = append(normalized, execution)
	}

	return normalized
}

// evaluateStrategy 评估策略
func (t *EvaluateStrategyTool) evaluateStrategy(strategyID string, executions []StrategyExecution) *StrategyEvaluation {
	totalExecutions := len(executions)
	successCount := 0
	totalDuration := 0.0
	totalRollbackCount := 0

	for _, exec := range executions {
		if exec.Success {
			successCount++
		}
		totalDuration += float64(exec.Duration)
		totalRollbackCount += exec.RollbackCount
	}

	successRate := float64(successCount) / float64(totalExecutions)
	avgDuration := totalDuration / float64(totalExecutions)
	avgRollbackCount := float64(totalRollbackCount) / float64(totalExecutions)

	// 评估质量
	quality := t.assessQuality(successRate, avgDuration, avgRollbackCount)

	// 识别瓶颈
	bottlenecks := t.identifyBottlenecks(executions)

	// 生成建议
	recommendations := t.generateRecommendations(successRate, avgDuration, avgRollbackCount, bottlenecks)

	return &StrategyEvaluation{
		StrategyID:       strategyID,
		TotalExecutions:  totalExecutions,
		SuccessRate:      successRate,
		AvgDuration:      avgDuration,
		AvgRollbackCount: avgRollbackCount,
		Quality:          quality,
		Bottlenecks:      bottlenecks,
		Recommendations:  recommendations,
	}
}

// assessQuality 评估质量
func (t *EvaluateStrategyTool) assessQuality(successRate, avgDuration, avgRollbackCount float64) string {
	score := 0.0

	// 成功率权重 50%
	score += successRate * 50

	// 执行时长权重 30%（越短越好，假设 30 秒以内为满分）
	durationScore := 1.0 - (avgDuration / 30000.0)
	if durationScore < 0 {
		durationScore = 0
	}
	score += durationScore * 30

	// 回滚次数权重 20%（越少越好，0 次为满分）
	rollbackScore := 1.0 - (avgRollbackCount / 3.0)
	if rollbackScore < 0 {
		rollbackScore = 0
	}
	score += rollbackScore * 20

	if score >= 90 {
		return "excellent"
	} else if score >= 75 {
		return "good"
	} else if score >= 60 {
		return "fair"
	}
	return "poor"
}

// identifyBottlenecks 识别瓶颈
func (t *EvaluateStrategyTool) identifyBottlenecks(executions []StrategyExecution) []string {
	bottlenecks := []string{}

	// 分析执行时长
	totalDuration := 0.0
	for _, exec := range executions {
		totalDuration += float64(exec.Duration)
	}
	avgDuration := totalDuration / float64(len(executions))

	if avgDuration > 30000 {
		bottlenecks = append(bottlenecks, "执行时间过长（平均 > 30 秒）")
	}

	// 分析回滚次数
	totalRollback := 0
	for _, exec := range executions {
		totalRollback += exec.RollbackCount
	}
	avgRollback := float64(totalRollback) / float64(len(executions))

	if avgRollback > 1.0 {
		bottlenecks = append(bottlenecks, "回滚次数过多（平均 > 1 次）")
	}

	// 分析成功率
	successCount := 0
	for _, exec := range executions {
		if exec.Success {
			successCount++
		}
	}
	successRate := float64(successCount) / float64(len(executions))

	if successRate < 0.8 {
		bottlenecks = append(bottlenecks, "成功率偏低（< 80%）")
	}

	return bottlenecks
}

// generateRecommendations 生成建议
func (t *EvaluateStrategyTool) generateRecommendations(successRate, avgDuration, avgRollbackCount float64, bottlenecks []string) []string {
	recommendations := []string{}

	if successRate < 0.8 {
		recommendations = append(recommendations, "分析失败原因，优化错误处理逻辑")
	}

	if avgDuration > 30000 {
		recommendations = append(recommendations, "识别耗时步骤，考虑并行化或优化")
	}

	if avgRollbackCount > 1.0 {
		recommendations = append(recommendations, "增强前置验证，减少执行失败")
	}

	if len(bottlenecks) == 0 {
		recommendations = append(recommendations, "策略表现良好，保持当前配置")
	}

	return recommendations
}
