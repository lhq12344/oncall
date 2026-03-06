package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// UpdateKnowledgeTool 知识库更新工具
type UpdateKnowledgeTool struct {
	logger *zap.Logger
}

// KnowledgeCase 知识案例
type KnowledgeCase struct {
	CaseID      string    `json:"case_id"`
	Title       string    `json:"title"`
	Description string    `json:"description"`
	Strategy    string    `json:"strategy"`
	SuccessRate float64   `json:"success_rate"`
	UsageCount  int       `json:"usage_count"`
	Weight      float64   `json:"weight"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// UpdateResult 更新结果
type UpdateResult struct {
	Action      string        `json:"action"` // save/update/skip
	CaseID      string        `json:"case_id"`
	OldWeight   float64       `json:"old_weight"`
	NewWeight   float64       `json:"new_weight"`
	Reason      string        `json:"reason"`
	UpdatedCase *KnowledgeCase `json:"updated_case"`
}

func NewUpdateKnowledgeTool(logger *zap.Logger) tool.BaseTool {
	return &UpdateKnowledgeTool{
		logger: logger,
	}
}

func (t *UpdateKnowledgeTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "update_knowledge",
		Desc: "更新知识库。决定是否保存新案例，更新案例权重。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"case": {
				Type:     schema.Object,
				Desc:     "知识案例（JSON 对象）",
				Required: true,
			},
			"execution_result": {
				Type:     schema.Object,
				Desc:     "执行结果（JSON 对象）",
				Required: true,
			},
		}),
	}, nil
}

func (t *UpdateKnowledgeTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Case            KnowledgeCase          `json:"case"`
		ExecutionResult map[string]interface{} `json:"execution_result"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	// 决定是否更新知识库
	result := t.decideUpdate(in.Case, in.ExecutionResult)

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("knowledge update completed",
			zap.String("action", result.Action),
			zap.String("case_id", result.CaseID))
	}

	return string(output), nil
}

// decideUpdate 决定是否更新
func (t *UpdateKnowledgeTool) decideUpdate(kcase KnowledgeCase, execResult map[string]interface{}) *UpdateResult {
	// 提取执行结果
	success := false
	if s, ok := execResult["success"].(bool); ok {
		success = s
	}

	duration := 0.0
	if d, ok := execResult["duration"].(float64); ok {
		duration = d
	}

	oldWeight := kcase.Weight

	// 决策逻辑
	if kcase.CaseID == "" {
		// 新案例
		if success && duration < 60000 {
			// 成功且执行时间合理，保存
			newWeight := t.calculateInitialWeight(success, duration)
			kcase.Weight = newWeight
			kcase.CreatedAt = time.Now()
			kcase.UpdatedAt = time.Now()

			return &UpdateResult{
				Action:      "save",
				CaseID:      fmt.Sprintf("case_%d", time.Now().Unix()),
				OldWeight:   0,
				NewWeight:   newWeight,
				Reason:      "成功案例，值得保存",
				UpdatedCase: &kcase,
			}
		}
		return &UpdateResult{
			Action: "skip",
			Reason: "执行失败或时间过长，不保存",
		}
	}

	// 已存在案例，更新权重
	newWeight := t.updateWeight(kcase, success, duration)
	kcase.Weight = newWeight
	kcase.UsageCount++
	kcase.UpdatedAt = time.Now()

	return &UpdateResult{
		Action:      "update",
		CaseID:      kcase.CaseID,
		OldWeight:   oldWeight,
		NewWeight:   newWeight,
		Reason:      fmt.Sprintf("根据执行结果更新权重（成功：%v）", success),
		UpdatedCase: &kcase,
	}
}

// calculateInitialWeight 计算初始权重
func (t *UpdateKnowledgeTool) calculateInitialWeight(success bool, duration float64) float64 {
	weight := 0.5

	if success {
		weight += 0.3
	}

	// 执行时间越短，权重越高
	if duration < 10000 {
		weight += 0.2
	} else if duration < 30000 {
		weight += 0.1
	}

	return weight
}

// updateWeight 更新权重
func (t *UpdateKnowledgeTool) updateWeight(kcase KnowledgeCase, success bool, duration float64) float64 {
	// 使用指数移动平均
	alpha := 0.3 // 学习率

	currentPerformance := 0.0
	if success {
		currentPerformance = 1.0
	}

	// 考虑执行时间
	if duration < 30000 {
		currentPerformance *= 1.1
	} else if duration > 60000 {
		currentPerformance *= 0.9
	}

	// 指数移动平均
	newWeight := alpha*currentPerformance + (1-alpha)*kcase.Weight

	// 限制在 [0, 1]
	if newWeight > 1.0 {
		newWeight = 1.0
	}
	if newWeight < 0.0 {
		newWeight = 0.0
	}

	return newWeight
}
