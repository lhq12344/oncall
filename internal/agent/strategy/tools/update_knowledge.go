package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// UpdateKnowledgeTool 知识库更新工具
type UpdateKnowledgeTool struct {
	indexer indexer.Indexer
	logger  *zap.Logger
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
	Action      string         `json:"action"` // save/update/skip
	CaseID      string         `json:"case_id"`
	OldWeight   float64        `json:"old_weight"`
	NewWeight   float64        `json:"new_weight"`
	Reason      string         `json:"reason"`
	UpdatedCase *KnowledgeCase `json:"updated_case"`
}

func NewUpdateKnowledgeTool(idx indexer.Indexer, logger *zap.Logger) tool.BaseTool {
	return &UpdateKnowledgeTool{
		indexer: idx,
		logger:  logger,
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
		Case            map[string]any         `json:"case"`
		ExecutionResult map[string]interface{} `json:"execution_result"`
	}

	var in args
	if err := json.Unmarshal([]byte(argumentsInJSON), &in); err != nil {
		return "", fmt.Errorf("invalid arguments: %w", err)
	}

	knowledgeCase := normalizeKnowledgeCase(in.Case)
	knowledgeCase = enrichKnowledgeCase(ctx, knowledgeCase, in.ExecutionResult)

	// 决定是否更新知识库
	result := t.decideUpdate(knowledgeCase, in.ExecutionResult)

	output, err := json.Marshal(result)
	if err != nil {
		return "", fmt.Errorf("failed to marshal result: %w", err)
	}

	if t.logger != nil {
		t.logger.Info("knowledge update completed",
			zap.String("agent", currentAgentForLog(ctx, "strategy_agent")),
			zap.String("action", result.Action),
			zap.String("case_id", result.CaseID))
	}

	if result.UpdatedCase != nil && (result.Action == "save" || result.Action == "update") {
		if err := t.archiveCase(ctx, result); err != nil {
			if t.logger != nil {
				t.logger.Warn("failed to archive ops case",
					zap.String("agent", currentAgentForLog(ctx, "strategy_agent")),
					zap.String("case_id", result.CaseID),
					zap.Error(err))
			}
		}
	}

	return string(output), nil
}

// enrichKnowledgeCase 根据流程上下文补全知识案例关键字段。
// 输入：ctx、原始知识案例、执行结果。
// 输出：补全后的知识案例（不会强制写入 case_id，以保留 save/update 判定语义）。
func enrichKnowledgeCase(ctx context.Context, kcase KnowledgeCase, execResult map[string]interface{}) KnowledgeCase {
	state := getIncidentStateAsMap(ctx)
	now := time.Now()
	if kcase.CreatedAt.IsZero() {
		kcase.CreatedAt = now
	}
	if kcase.UpdatedAt.IsZero() {
		kcase.UpdatedAt = now
	}

	rootCause := strings.TrimSpace(anyToString(state["root_cause"]))
	targetNode := strings.TrimSpace(anyToString(state["target_node"]))
	planSummary := strings.TrimSpace(anyToString(state["plan_summary"]))
	impact := strings.TrimSpace(anyToString(state["impact"]))
	executionReason := strings.TrimSpace(anyToString(state["execution_reason"]))
	finalReport := strings.TrimSpace(anyToString(state["final_report"]))
	fallbackPlan := strings.TrimSpace(anyToString(state["execution_fallback"]))

	if strings.TrimSpace(kcase.Title) == "" {
		switch {
		case rootCause != "" && targetNode != "":
			kcase.Title = fmt.Sprintf("%s_%s_处置记录", rootCause, targetNode)
		case rootCause != "":
			kcase.Title = fmt.Sprintf("%s_故障处置记录", rootCause)
		case planSummary != "":
			kcase.Title = clipCaseText(planSummary, 96)
		default:
			kcase.Title = fmt.Sprintf("ops_case_%s", now.Format("20060102_150405"))
		}
	}

	if strings.TrimSpace(kcase.Description) == "" {
		kcase.Description = firstNonEmptyText(
			finalReport,
			executionReason,
			impact,
			strings.TrimSpace(anyToString(execResult["failed_reason"])),
			"自动处置完成，详细信息见执行日志。",
		)
	}

	if strings.TrimSpace(kcase.Strategy) == "" {
		kcase.Strategy = firstNonEmptyText(
			planSummary,
			fallbackPlan,
			strings.TrimSpace(anyToString(execResult["manual_plan"])),
			"基于 RCA 结论执行分步骤排障。",
		)
	}

	if kcase.SuccessRate <= 0 {
		if anyToBool(execResult["success"]) {
			kcase.SuccessRate = 1
		}
	}

	return kcase
}

// getIncidentStateAsMap 从 SessionValues 读取 incident_graph_state。
// 输入：ctx。
// 输出：状态 map；读取失败时返回空 map。
func getIncidentStateAsMap(ctx context.Context) map[string]any {
	value, ok := adk.GetSessionValue(ctx, "incident_graph_state")
	if !ok || value == nil {
		return map[string]any{}
	}

	body, err := json.Marshal(value)
	if err != nil {
		return map[string]any{}
	}

	state := map[string]any{}
	if err := json.Unmarshal(body, &state); err != nil {
		return map[string]any{}
	}
	return state
}

// firstNonEmptyText 返回第一个非空文本。
// 输入：候选文本列表。
// 输出：第一个非空文本；全部为空时返回空字符串。
func firstNonEmptyText(values ...string) string {
	for _, value := range values {
		text := strings.TrimSpace(value)
		if text != "" {
			return text
		}
	}
	return ""
}

// clipCaseText 裁剪文本长度，避免字段过长。
// 输入：原文本、最大 rune 长度。
// 输出：裁剪后的文本。
func clipCaseText(text string, maxRunes int) string {
	text = strings.TrimSpace(text)
	if text == "" {
		return ""
	}
	if maxRunes <= 0 {
		maxRunes = 96
	}
	runes := []rune(text)
	if len(runes) <= maxRunes {
		return text
	}
	return string(runes[:maxRunes]) + "..."
}

// normalizeKnowledgeCase 规范化知识案例参数。
// 输入：原始 case 参数对象。
// 输出：字段类型已归一化的 KnowledgeCase。
func normalizeKnowledgeCase(raw map[string]any) KnowledgeCase {
	now := time.Now()
	createdAt, ok := parseFlexibleTimeArg(raw["created_at"])
	if !ok {
		createdAt = now
	}
	updatedAt, ok := parseFlexibleTimeArg(raw["updated_at"])
	if !ok {
		updatedAt = createdAt
	}

	return KnowledgeCase{
		CaseID:      anyToString(raw["case_id"]),
		Title:       anyToString(raw["title"]),
		Description: anyToString(raw["description"]),
		Strategy:    anyToString(raw["strategy"]),
		SuccessRate: anyToFloat64(raw["success_rate"]),
		UsageCount:  anyToInt(raw["usage_count"]),
		Weight:      anyToFloat64(raw["weight"]),
		CreatedAt:   createdAt,
		UpdatedAt:   updatedAt,
	}
}

func (t *UpdateKnowledgeTool) archiveCase(ctx context.Context, result *UpdateResult) error {
	if t.indexer == nil || result == nil || result.UpdatedCase == nil {
		return nil
	}
	caseDoc := result.UpdatedCase
	if caseDoc.CaseID == "" {
		caseDoc.CaseID = fmt.Sprintf("case_%d", time.Now().UnixNano())
	}

	payload := map[string]any{
		"case_id":      caseDoc.CaseID,
		"title":        caseDoc.Title,
		"description":  caseDoc.Description,
		"strategy":     caseDoc.Strategy,
		"success_rate": caseDoc.SuccessRate,
		"weight":       caseDoc.Weight,
		"updated_at":   caseDoc.UpdatedAt.Format(time.RFC3339),
	}
	body, _ := json.Marshal(payload)

	doc := &schema.Document{
		ID:      caseDoc.CaseID,
		Content: string(body),
		MetaData: map[string]any{
			"type":       "ops_case",
			"action":     result.Action,
			"reason":     result.Reason,
			"updated_at": caseDoc.UpdatedAt.Format(time.RFC3339),
		},
	}
	_, err := t.indexer.Store(ctx, []*schema.Document{doc})
	return err
}

// decideUpdate 决定是否更新
func (t *UpdateKnowledgeTool) decideUpdate(kcase KnowledgeCase, execResult map[string]interface{}) *UpdateResult {
	// 提取执行结果
	success := anyToBool(execResult["success"])
	duration := anyToFloat64(execResult["duration"])

	oldWeight := kcase.Weight

	// 决策逻辑
	if kcase.CaseID == "" {
		// 新案例
		if success && duration < 60000 {
			// 成功且执行时间合理，保存
			newWeight := t.calculateInitialWeight(success, duration)
			generatedCaseID := fmt.Sprintf("case_%d", time.Now().Unix())
			kcase.CaseID = generatedCaseID
			kcase.Weight = newWeight
			kcase.CreatedAt = time.Now()
			kcase.UpdatedAt = time.Now()

			return &UpdateResult{
				Action:      "save",
				CaseID:      generatedCaseID,
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
