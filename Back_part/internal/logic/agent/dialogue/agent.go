package dialogue

import (
	"context"
	"fmt"
	"strings"
	"time"

	"go_agent/internal/logic/agent/dialogue/tools"
	"go_agent/internal/logic/ai/models"
	airetriever "go_agent/internal/logic/ai/retriever"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/embedding"
	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

const milvusRetrieverInitTimeout = 8 * time.Second

// Config Dialogue Agent 配置
type Config struct {
	ChatModel     *models.ChatModel
	Embedder      embedding.Embedder // 用于语义相似度计算
	EnableToolLLM bool               // 工具内部是否允许二次 LLM 调用，默认 false
	Logger        *zap.Logger
}

// DialogueState 对话状态跟踪
type DialogueState struct {
	CurrentIntent  string                 // 当前意图
	IntentHistory  []string               // 意图历史
	Confidence     float64                // 置信度
	Entropy        float64                // 语义熵
	Converged      bool                   // 是否收敛
	ContextSummary string                 // 上下文摘要
	MissingInfo    []string               // 缺失信息
	Metadata       map[string]interface{} // 额外元数据
}

// NewDialogueAgent 创建 Dialogue Agent（意图分析 + 工具编排）
func NewDialogueAgent(ctx context.Context, cfg *Config) (adk.ResumableAgent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	retrieverCtx, cancel := context.WithTimeout(ctx, milvusRetrieverInitTimeout)
	defer cancel()
	knowledgeRetriever, err := airetriever.NewMilvusRetriever(retrieverCtx)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Warn("failed to initialize milvus retriever for dialogue agent, fallback to degraded mode",
				zap.Error(err))
		}
		knowledgeRetriever = nil
	}

	// 创建工具集
	toolsList := buildDialogueTools(cfg, knowledgeRetriever)

	// 创建内置 Summarization 中间件（自动压缩对话历史）
	summaryConfig := &summarization.Config{
		Model: cfg.ChatModel.Client,
		Trigger: &summarization.TriggerCondition{
			ContextTokens: 300000, // 在 k tokens 时触发
		},
	}

	summaryHandler, err := summarization.New(ctx, summaryConfig)
	if err != nil {
		return nil, fmt.Errorf("failed to create dialogue summarization middleware: %w", err)
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "dialogue_agent",
		Description:   "面向对话、知识库检索和网络搜索的通用智能助手",
		Model:         cfg.ChatModel.Client,
		GenModelInput: noFormatGenModelInput,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Handlers: []adk.ChatModelAgentMiddleware{summaryHandler},
		Instruction: `你是一个面向对话、知识库检索和网络搜索的智能助手。

你的目标是准确理解用户问题，结合会话记忆、已上传知识库和必要的外部网络检索，给出简洁、可靠、可执行的回答。

可用工具：
- intent_analysis：当用户意图不清、问题跨多个方向、或缺少关键信息时，用于判断意图、置信度和缺失信息。
- request_detail_selection：当缺少关键上下文且候选项有限、适合单选时，用于向用户请求补充选择。
- knowledge_retrieve：当问题可能由用户上传的文档或内部知识回答时，优先检索知识库。
- web_search：当问题依赖最新公告、官方文档、版本变化、外部资料或公开网页时使用。

工作原则：
- 普通闲聊、通用解释或已有上下文足够时，直接回答，不强行调用工具。
- 涉及上传文档、项目资料、内部说明、历史记录等内容时，优先调用 knowledge_retrieve。
- 涉及最新信息、外部事实或可能过期的信息时，调用 web_search，并标注为外部检索结果。
- 缺少关键字段且可枚举时，使用 request_detail_selection；开放式缺失信息用自然语言追问。
- 工具结果不足以支撑确定结论时，明确说明不确定点和下一步建议。

输出风格：
- 简洁、专业、直接，优先用中文回答。
- 使用 Markdown 组织内容，必要时用列表、表格和代码块。
- 明确信息来源，例如“知识库检索结果”或“外部网络检索结果”。
- 不编造工具未返回的事实，不声称执行了不可用的命令或外部系统操作能力。`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create dialogue agent: %w", err)
	}

	return agent, nil
}

// noFormatGenModelInput 构建模型输入消息，不对 instruction 执行 FString 变量替换。
// 输入：instruction 系统提示词，input 用户/历史消息。
// 输出：拼接后的模型消息列表（system + input.Messages）。
func noFormatGenModelInput(_ context.Context, instruction string, input *adk.AgentInput) ([]adk.Message, error) {
	msgs := make([]adk.Message, 0, 1)
	if strings.TrimSpace(instruction) != "" {
		msgs = append(msgs, schema.SystemMessage(instruction))
	}
	if input != nil && len(input.Messages) > 0 {
		msgs = append(msgs, input.Messages...)
	}
	return msgs, nil
}

// buildDialogueTools 构建 dialogue_agent 可用工具集合。
// 输入：cfg 对话代理配置，knowledgeRetriever 知识库检索器。
// 输出：可注册到 ToolsNode 的工具列表。
func buildDialogueTools(cfg *Config, knowledgeRetriever einoretriever.Retriever) []tool.BaseTool {
	return []tool.BaseTool{
		tools.NewIntentAnalysisTool(cfg.ChatModel, cfg.Embedder, cfg.Logger, cfg.EnableToolLLM),
		tools.NewDetailSelectionTool(cfg.Logger),
		tools.NewKnowledgeRetrieveTool(knowledgeRetriever, cfg.Logger),
		//tools.NewWebSearchTool(cfg.Logger),
	}
}
