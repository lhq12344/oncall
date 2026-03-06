package dialogue

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/compose"
)

// IntentAnalysisTool 意图分析工具
type IntentAnalysisTool struct {
	agent *DialogueAgent
}

// NewIntentAnalysisTool 创建意图分析工具
func NewIntentAnalysisTool(agent *DialogueAgent) *IntentAnalysisTool {
	return &IntentAnalysisTool{agent: agent}
}

// Info 返回工具信息
func (t *IntentAnalysisTool) Info(ctx context.Context) (*compose.ToolInfo, error) {
	return &compose.ToolInfo{
		Name: "intent_analysis",
		Desc: "分析用户意图，识别用户想要做什么（监控查询、故障诊断、执行操作、知识检索）。输入：会话 ID 和用户输入。输出：意图类型、置信度、语义熵。",
		ParamsOneOf: compose.NewParamsOneOfByParams(
			map[string]*compose.ParameterInfo{
				"session_id": {
					Type:     compose.String,
					Desc:     "会话 ID",
					Required: true,
				},
				"user_input": {
					Type:     compose.String,
					Desc:     "用户输入文本",
					Required: true,
				},
			},
		),
	}, nil
}

// InvokableRun 执行工具
func (t *IntentAnalysisTool) InvokableRun(ctx context.Context, input string, opts ...compose.ToolOption) (string, error) {
	// 解析输入参数
	var params struct {
		SessionID string `json:"session_id"`
		UserInput string `json:"user_input"`
	}

	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	// 分析意图
	analysis, err := t.agent.AnalyzeIntent(ctx, params.SessionID, params.UserInput)
	if err != nil {
		return "", fmt.Errorf("intent analysis failed: %w", err)
	}

	// 格式化输出
	output := fmt.Sprintf(`意图分析结果：
类型: %s
置信度: %.2f
语义熵: %.2f
是否收敛: %v

说明: `,
		analysis.Intent.Type,
		analysis.Confidence,
		analysis.Entropy,
		analysis.Converged,
	)

	switch analysis.Intent.Type {
	case "monitor":
		output += "用户想要查看监控数据或系统状态"
	case "diagnose":
		output += "用户遇到了故障，需要诊断和分析"
	case "execute":
		output += "用户想要执行某个操作（重启、扩容等）"
	case "knowledge":
		output += "用户想要查询历史案例或最佳实践"
	default:
		output += "通用对话"
	}

	return output, nil
}

// QuestionPredictionTool 问题预测工具
type QuestionPredictionTool struct {
	agent *DialogueAgent
}

// NewQuestionPredictionTool 创建问题预测工具
func NewQuestionPredictionTool(agent *DialogueAgent) *QuestionPredictionTool {
	return &QuestionPredictionTool{agent: agent}
}

// Info 返回工具信息
func (t *QuestionPredictionTool) Info(ctx context.Context) (*compose.ToolInfo, error) {
	return &compose.ToolInfo{
		Name: "question_prediction",
		Desc: "预测用户下一步可能提出的问题，主动引导对话。输入：会话 ID 和问题数量。输出：候选问题列表。",
		ParamsOneOf: compose.NewParamsOneOfByParams(
			map[string]*compose.ParameterInfo{
				"session_id": {
					Type:     compose.String,
					Desc:     "会话 ID",
					Required: true,
				},
				"count": {
					Type:     compose.Number,
					Desc:     "预测问题数量（默认 3）",
					Required: false,
				},
			},
		),
	}, nil
}

// InvokableRun 执行工具
func (t *QuestionPredictionTool) InvokableRun(ctx context.Context, input string, opts ...compose.ToolOption) (string, error) {
	// 解析输入参数
	var params struct {
		SessionID string `json:"session_id"`
		Count     int    `json:"count"`
	}

	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	// 设置默认值
	if params.Count == 0 {
		params.Count = 3
	}

	// 预测问题
	questions, err := t.agent.PredictNextQuestions(ctx, params.SessionID, params.Count)
	if err != nil {
		return "", fmt.Errorf("question prediction failed: %w", err)
	}

	if len(questions) == 0 {
		return "当前意图尚未收敛，暂无候选问题", nil
	}

	// 格式化输出
	output := fmt.Sprintf("预测的候选问题（共 %d 个）：\n\n", len(questions))
	for i, q := range questions {
		output += fmt.Sprintf("%d. %s\n", i+1, q)
	}

	return output, nil
}

// ClarificationTool 澄清问题工具
type ClarificationTool struct {
	agent *DialogueAgent
}

// NewClarificationTool 创建澄清问题工具
func NewClarificationTool(agent *DialogueAgent) *ClarificationTool {
	return &ClarificationTool{agent: agent}
}

// Info 返回工具信息
func (t *ClarificationTool) Info(ctx context.Context) (*compose.ToolInfo, error) {
	return &compose.ToolInfo{
		Name: "clarification",
		Desc: "当用户意图不明确时，生成澄清性问题。输入：会话 ID。输出：澄清问题。",
		ParamsOneOf: compose.NewParamsOneOfByParams(
			map[string]*compose.ParameterInfo{
				"session_id": {
					Type:     compose.String,
					Desc:     "会话 ID",
					Required: true,
				},
			},
		),
	}, nil
}

// InvokableRun 执行工具
func (t *ClarificationTool) InvokableRun(ctx context.Context, input string, opts ...compose.ToolOption) (string, error) {
	// 解析输入参数
	var params struct {
		SessionID string `json:"session_id"`
	}

	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	// 生成澄清问题
	question, err := t.agent.GenerateClarificationQuestion(ctx, params.SessionID)
	if err != nil {
		return "", fmt.Errorf("clarification generation failed: %w", err)
	}

	return question, nil
}

// EntityExtractionTool 实体提取工具
type EntityExtractionTool struct {
	agent *DialogueAgent
}

// NewEntityExtractionTool 创建实体提取工具
func NewEntityExtractionTool(agent *DialogueAgent) *EntityExtractionTool {
	return &EntityExtractionTool{agent: agent}
}

// Info 返回工具信息
func (t *EntityExtractionTool) Info(ctx context.Context) (*compose.ToolInfo, error) {
	return &compose.ToolInfo{
		Name: "entity_extraction",
		Desc: "从用户输入中提取关键实体（服务名、指标名、时间范围等）。输入：用户输入文本。输出：提取的实体。",
		ParamsOneOf: compose.NewParamsOneOfByParams(
			map[string]*compose.ParameterInfo{
				"user_input": {
					Type:     compose.String,
					Desc:     "用户输入文本",
					Required: true,
				},
			},
		),
	}, nil
}

// InvokableRun 执行工具
func (t *EntityExtractionTool) InvokableRun(ctx context.Context, input string, opts ...compose.ToolOption) (string, error) {
	// 解析输入参数
	var params struct {
		UserInput string `json:"user_input"`
	}

	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	// 提取实体
	entities, err := t.agent.ExtractEntities(ctx, params.UserInput)
	if err != nil {
		return "", fmt.Errorf("entity extraction failed: %w", err)
	}

	if len(entities) == 0 {
		return "未提取到实体", nil
	}

	// 格式化输出
	output := "提取的实体：\n"
	for key, value := range entities {
		output += fmt.Sprintf("- %s: %v\n", key, value)
	}

	return output, nil
}

// ConversationSummaryTool 对话总结工具
type ConversationSummaryTool struct {
	agent *DialogueAgent
}

// NewConversationSummaryTool 创建对话总结工具
func NewConversationSummaryTool(agent *DialogueAgent) *ConversationSummaryTool {
	return &ConversationSummaryTool{agent: agent}
}

// Info 返回工具信息
func (t *ConversationSummaryTool) Info(ctx context.Context) (*compose.ToolInfo, error) {
	return &compose.ToolInfo{
		Name: "conversation_summary",
		Desc: "总结对话内容，提取关键信息。输入：会话 ID。输出：对话总结。",
		ParamsOneOf: compose.NewParamsOneOfByParams(
			map[string]*compose.ParameterInfo{
				"session_id": {
					Type:     compose.String,
					Desc:     "会话 ID",
					Required: true,
				},
			},
		),
	}, nil
}

// InvokableRun 执行工具
func (t *ConversationSummaryTool) InvokableRun(ctx context.Context, input string, opts ...compose.ToolOption) (string, error) {
	// 解析输入参数
	var params struct {
		SessionID string `json:"session_id"`
	}

	if err := json.Unmarshal([]byte(input), &params); err != nil {
		return "", fmt.Errorf("failed to parse input: %w", err)
	}

	// 总结对话
	summary, err := t.agent.SummarizeConversation(ctx, params.SessionID)
	if err != nil {
		return "", fmt.Errorf("conversation summary failed: %w", err)
	}

	return summary, nil
}
