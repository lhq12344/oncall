package dialogue

import (
	"context"
	"fmt"

	"go_agent/internal/agent/dialogue/tools"
	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"go.uber.org/zap"
)

// Config Dialogue Agent 配置
type Config struct {
	ChatModel *models.ChatModel
	Embedder  embedding.Embedder // 用于语义相似度计算
	Logger    *zap.Logger
}

// DialogueState 对话状态跟踪
type DialogueState struct {
	CurrentIntent   string                 // 当前意图
	IntentHistory   []string               // 意图历史
	Confidence      float64                // 置信度
	Entropy         float64                // 语义熵
	Converged       bool                   // 是否收敛
	ContextSummary  string                 // 上下文摘要
	MissingInfo     []string               // 缺失信息
	Metadata        map[string]interface{} // 额外元数据
}

// NewDialogueAgent 创建 Dialogue Agent（意图分析 + 问题预测）
func NewDialogueAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	// 创建工具集
	toolsList := []tool.BaseTool{
		tools.NewIntentAnalysisTool(cfg.ChatModel, cfg.Embedder, cfg.Logger),
		tools.NewQuestionPredictionTool(cfg.ChatModel, cfg.Logger),
	}

	// 创建 ChatModelAgent
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "dialogue_agent",
		Description: "分析用户意图、预测问题并引导对话的对话代理",
		Model:       cfg.ChatModel.Client,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Instruction: `你是一个对话助手，负责理解用户意图并引导对话。

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



