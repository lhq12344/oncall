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

// PruneKnowledgeTool 知识剪枝工具
type PruneKnowledgeTool struct {
	logger *zap.Logger
}

// pruneCase 剪枝计算使用的案例结构。
type pruneCase struct {
	CaseID      string    `json:"case_id"`
	Title       string    `json:"title"`
	Weight      float64   `json:"weight"`
	UsageCount  int       `json:"usage_count"`
	SuccessRate float64   `json:"success_rate"`
	CreatedAt   time.Time `json:"created_at"`
	UpdatedAt   time.Time `json:"updated_at"`
}

// PruneResult 剪枝结果
type PruneResult struct {
	TotalCases    int               `json:"total_cases"`
	DeletedCases  []string          `json:"deleted_cases"`
	MergedCases   []string          `json:"merged_cases"`
	RetainedCases int               `json:"retained_cases"`
	Reason        map[string]string `json:"reason"` // case_id -> reason
}

func NewPruneKnowledgeTool(logger *zap.Logger) tool.BaseTool {
	return &PruneKnowledgeTool{
		logger: logger,
	}
}

func (t *PruneKnowledgeTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "prune_knowledge",
		Desc: "清理知识库。删除低质量、过期的案例，合并相似案例。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"cases": {
				Type:     schema.Array,
				Desc:     "知识案例列表（JSON 数组）",
				Required: true,
			},
			"max_age_days": {
				Type:     schema.Integer,
				Desc:     "最大保留天数，默认 90",
				Required: false,
			},
			"min_weight": {
				Type:     schema.Number,
				Desc:     "最小权重阈值，默认 0.3",
				Required: false,
			},
		}),
	}, nil
}

func (t *PruneKnowledgeTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	type args struct {
		Cases      []map[string]any `json:"cases"`
		MaxAgeDays int              `json:"max_age_days"`
		MinWeight  float64          `json:"min_weight"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	// 默认值
	if in.MaxAgeDays <= 0 {
		in.MaxAgeDays = 90
	}
	if in.MinWeight <= 0 {
		in.MinWeight = 0.3
	}

	convertedCases := normalizePruneCases(in.Cases)

	// 执行剪枝
	result := t.pruneKnowledge(convertedCases, in.MaxAgeDays, in.MinWeight)

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("knowledge pruning completed",
			zap.String("agent", currentAgentForLog(ctx, "strategy_agent")),
			zap.Int("total", result.TotalCases),
			zap.Int("deleted", len(result.DeletedCases)),
			zap.Int("merged", len(result.MergedCases)),
			zap.Int("retained", result.RetainedCases))
	}

	return string(output), nil
}

// normalizePruneCases 规范化剪枝案例参数。
// 输入：原始 cases 参数。
// 输出：可用于剪枝计算的案例列表。
func normalizePruneCases(rawCases []map[string]any) []pruneCase {
	if len(rawCases) == 0 {
		return nil
	}

	now := time.Now()
	cases := make([]pruneCase, 0, len(rawCases))
	for _, raw := range rawCases {
		updatedAt, ok := parseFlexibleTimeArg(raw["updated_at"])
		if !ok {
			updatedAt = now
		}
		createdAt, ok := parseFlexibleTimeArg(raw["created_at"])
		if !ok {
			createdAt = updatedAt
		}

		cases = append(cases, pruneCase{
			CaseID:      anyToString(raw["case_id"]),
			Title:       anyToString(raw["title"]),
			Weight:      anyToFloat64(raw["weight"]),
			UsageCount:  anyToInt(raw["usage_count"]),
			SuccessRate: anyToFloat64(raw["success_rate"]),
			CreatedAt:   createdAt,
			UpdatedAt:   updatedAt,
		})
	}
	return cases
}

// pruneKnowledge 执行剪枝
func (t *PruneKnowledgeTool) pruneKnowledge(cases []pruneCase, maxAgeDays int, minWeight float64) *PruneResult {
	deletedCases := []string{}
	mergedCases := []string{}
	reasons := make(map[string]string)

	now := time.Now()
	maxAge := time.Duration(maxAgeDays) * 24 * time.Hour

	for _, kcase := range cases {
		shouldDelete := false
		reason := ""

		// 规则 1：权重过低
		if kcase.Weight < minWeight {
			shouldDelete = true
			reason = fmt.Sprintf("权重过低（%.2f < %.2f）", kcase.Weight, minWeight)
		}

		// 规则 2：过期且未使用
		age := now.Sub(kcase.UpdatedAt)
		if age > maxAge && kcase.UsageCount == 0 {
			shouldDelete = true
			reason = fmt.Sprintf("过期且未使用（%d 天）", int(age.Hours()/24))
		}

		// 规则 3：成功率过低
		if kcase.SuccessRate < 0.5 && kcase.UsageCount > 5 {
			shouldDelete = true
			reason = fmt.Sprintf("成功率过低（%.2f%%）", kcase.SuccessRate*100)
		}

		if shouldDelete {
			deletedCases = append(deletedCases, kcase.CaseID)
			reasons[kcase.CaseID] = reason
		}
	}

	// TODO: 实现案例合并逻辑（识别相似案例）

	retainedCases := len(cases) - len(deletedCases) - len(mergedCases)

	return &PruneResult{
		TotalCases:    len(cases),
		DeletedCases:  deletedCases,
		MergedCases:   mergedCases,
		RetainedCases: retainedCases,
		Reason:        reasons,
	}
}
