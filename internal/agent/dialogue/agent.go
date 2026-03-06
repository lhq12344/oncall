package dialogue

import (
	"context"
	"fmt"

	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config Dialogue Agent 配置
type Config struct {
	ChatModel *models.ChatModel
	Logger    *zap.Logger
}

// NewDialogueAgent 创建 Dialogue Agent（意图分析 + 问题预测）
func NewDialogueAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	// 创建工具集
	tools := []tool.BaseTool{
		NewIntentAnalysisTool(),
		NewQuestionPredictionTool(),
	}

	// 创建 ChatModelAgent
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Model: cfg.ChatModel.Client,
		Tools: &adk.ToolsConfig{
			Tools: tools,
		},
		SystemPrompt: `你是一个对话助手，负责理解用户意图并引导对话。

你的职责：
1. 分析用户输入的意图（监控查询/故障诊断/知识检索/执行操作）
2. 评估意图的明确程度（语义熵）
3. 当意图不明确时，生成澄清性问题
4. 当意图明确后，预测用户下一步可能提出的问题

意图分类：
- monitor: 查看系统状态、指标、资源使用情况
- diagnose: 故障排查、问题分析、异常诊断
- knowledge: 查询历史案例、最佳实践、文档
- execute: 执行操作、修复问题、变更配置
- general: 通用对话、闲聊

注意：
- 如果用户输入模糊（如"有问题"），主动询问具体情况
- 预测的问题应该具有引导性，帮助用户快速定位问题
- 保持对话的连贯性和上下文感知`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create dialogue agent: %w", err)
	}

	return agent, nil
}

// IntentAnalysisTool 意图分析工具
type IntentAnalysisTool struct{}

func NewIntentAnalysisTool() tool.BaseTool {
	return &IntentAnalysisTool{}
}

func (t *IntentAnalysisTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "intent_analysis",
		Desc: "分析用户输入的意图类型和明确程度。返回意图类型、置信度、语义熵等信息。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"user_input": {
				Type:     schema.String,
				Desc:     "用户输入文本",
				Required: true,
			},
		}),
	}, nil
}

func (t *IntentAnalysisTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// TODO: 实现意图分析逻辑
	return `{"intent_type": "general", "confidence": 0.5, "entropy": 1.0, "converged": false}`, nil
}

// QuestionPredictionTool 问题预测工具
type QuestionPredictionTool struct{}

func NewQuestionPredictionTool() tool.BaseTool {
	return &QuestionPredictionTool{}
}

func (t *QuestionPredictionTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "question_prediction",
		Desc: "预测用户下一步可能提出的问题。基于当前对话上下文，生成 3-5 个候选问题。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"context": {
				Type:     schema.String,
				Desc:     "当前对话上下文",
				Required: true,
			},
			"count": {
				Type:     schema.Integer,
				Desc:     "生成问题数量（默认 3）",
				Required: false,
			},
		}),
	}, nil
}

func (t *QuestionPredictionTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	// TODO: 实现问题预测逻辑
	return `["故障是从什么时候开始的？", "有看到具体的错误信息吗？", "影响范围有多大？"]`, nil
}
