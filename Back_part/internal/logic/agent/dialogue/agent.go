package dialogue

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_agent/internal/logic/agent/dialogue/tools"
	"go_agent/internal/logic/ai/models"
	airetriever "go_agent/internal/logic/ai/retriever"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"
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
	SkillsDir     string             // Eino skill 目录，为空或不存在时降级为无 skill 能力
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

	handlers := []adk.ChatModelAgentMiddleware{summaryHandler}
	skillHandler, err := newDialogueSkillMiddleware(ctx, cfg.SkillsDir, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create dialogue skill middleware: %w", err)
	}
	if skillHandler != nil {
		handlers = append(handlers, skillHandler)
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
		Handlers: handlers,
		Instruction: `你是一个面向对话、知识库检索和网络搜索的智能助手。

你的目标是准确理解用户问题，结合会话记忆、已上传知识库和必要的外部网络检索，给出简洁、可靠、可执行的回答。

可用工具：
- intent_analysis：每轮玩家请求都应先用于判断游戏客服主要诉求、置信度、缺失信息和内部路由建议。
- player_emotion_analysis：每轮玩家请求都应先用于判断玩家情绪、强度和是否需要升级处理。
- request_detail_selection：当缺少关键上下文且候选项有限、适合单选时，用于向用户请求补充选择。
- knowledge_retrieve：当问题可能由用户上传的文档或内部知识回答时，优先检索知识库。
- web_search：当问题依赖最新公告、官方文档、版本变化、外部资料或公开网页时使用。

工作原则：
- 每次处理玩家请求前，先调用 intent_analysis 和 player_emotion_analysis；不要把分析标签直接展示给玩家。
- 普通闲聊、通用解释或已有上下文足够时，可以在完成诉求和情绪分析后直接回答。
- 涉及上传文档、项目资料、内部说明、历史记录等内容时，优先调用 knowledge_retrieve。
- 涉及最新信息、外部事实或可能过期的信息时，调用 web_search，并标注为外部检索结果。
- 缺少关键字段且可枚举时，使用 request_detail_selection；开放式缺失信息用自然语言追问。
- 工具结果不足以支撑确定结论时，明确说明不确定点和下一步建议。
- 遇到愤怒、急迫、焦虑或需要升级的情绪时，语气更安抚、步骤更明确，必要时建议转人工或提交客服处理。

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

// newDialogueSkillMiddleware 基于 EINO_EXT_SKILLS_DIR 对应目录创建 Eino skill 中间件。
// 输入：ctx、skillsDir、logger。
// 输出：可追加到 ChatModelAgent Handlers 的 middleware；未配置或目录不可用时返回 nil。
func newDialogueSkillMiddleware(ctx context.Context, skillsDir string, logger *zap.Logger) (adk.ChatModelAgentMiddleware, error) {
	skillsDir = strings.TrimSpace(skillsDir)
	if skillsDir == "" {
		return nil, nil
	}

	absSkillsDir, err := filepath.Abs(skillsDir)
	if err != nil {
		if logger != nil {
			logger.Warn("failed to resolve dialogue skill directory, skill middleware disabled",
				zap.String("skills_dir", skillsDir),
				zap.Error(err))
		}
		return nil, nil
	}

	info, err := os.Stat(absSkillsDir)
	if err != nil {
		if logger != nil {
			logger.Warn("dialogue skill directory unavailable, skill middleware disabled",
				zap.String("skills_dir", absSkillsDir),
				zap.Error(err))
		}
		return nil, nil
	}
	if !info.IsDir() {
		if logger != nil {
			logger.Warn("dialogue skill path is not a directory, skill middleware disabled",
				zap.String("skills_dir", absSkillsDir))
		}
		return nil, nil
	}

	backend, err := skill.NewBackendFromFilesystem(ctx, &skill.BackendFromFilesystemConfig{
		Backend: newReadOnlySkillFilesystemBackend(absSkillsDir),
		BaseDir: absSkillsDir,
	})
	if err != nil {
		return nil, err
	}

	middleware, err := skill.NewMiddleware(ctx, &skill.Config{
		Backend: backend,
	})
	if err != nil {
		return nil, err
	}

	if logger != nil {
		logger.Info("dialogue skill middleware enabled", zap.String("skills_dir", absSkillsDir))
	}
	return middleware, nil
}

// analysisMessageMarker 是 graph.go appendMandatoryAnalysisMessage 写入分析消息的前缀。
// contextAwareModelInput 用此前缀识别、提取并将分析块提升到 System Prompt 顶部。
const analysisMessageMarker = "内部客服分析结果"

// noFormatGenModelInput 构建模型输入消息，不对 instruction 执行 FString 变量替换。
// 用于不需要感知意图/情绪上下文的外层包装 agent（如 NewDialogueAgent 本体）。
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

// contextAwareModelInput 构建模型输入消息，并将当前轮次的意图/情绪分析块提升至 System Prompt 顶部。
// 扫描 input.Messages，提取最新的分析 SystemMessage（通过 analysisMessageMarker 识别），
// 将其从消息列表中移除（避免历史中多轮累积），并前置到 instruction 之前组成完整系统提示。
// 效果：LLM 始终在 System Prompt 最顶部看到当前轮次的意图/情绪，不会被历史消息淹没。
func contextAwareModelInput(_ context.Context, instruction string, input *adk.AgentInput) ([]adk.Message, error) {
	var latestAnalysis string
	var filtered []adk.Message
	if input != nil {
		filtered = make([]adk.Message, 0, len(input.Messages))
		for _, msg := range input.Messages {
			if msg.Role == schema.System && strings.HasPrefix(msg.Content, analysisMessageMarker) {
				latestAnalysis = msg.Content // 多轮累积时取最后一条（最新轮次）
			} else {
				filtered = append(filtered, msg)
			}
		}
	}

	fullInstruction := instruction
	if latestAnalysis != "" {
		fullInstruction = latestAnalysis + "\n\n---\n\n" + instruction
	}

	msgs := make([]adk.Message, 0, 1+len(filtered))
	if strings.TrimSpace(fullInstruction) != "" {
		msgs = append(msgs, schema.SystemMessage(fullInstruction))
	}
	msgs = append(msgs, filtered...)
	return msgs, nil
}

// buildDialogueTools 构建 dialogue_agent 可用工具集合。
// 输入：cfg 对话代理配置，knowledgeRetriever 知识库检索器。
// 输出：可注册到 ToolsNode 的工具列表。
func buildDialogueTools(cfg *Config, knowledgeRetriever einoretriever.Retriever) []tool.BaseTool {
	return []tool.BaseTool{
		tools.NewIntentAnalysisTool(cfg.ChatModel, cfg.Embedder, cfg.Logger, cfg.EnableToolLLM),
		tools.NewPlayerEmotionAnalysisTool(cfg.Logger),
		tools.NewDetailSelectionTool(cfg.Logger),
		tools.NewKnowledgeRetrieveTool(knowledgeRetriever, cfg.Logger),
		tools.NewWebSearchTool(cfg.Logger),
		tools.NewBashApprovalTool(cfg.Logger),
	}
}

// OrchState 对话编排图的共享状态，跨节点传递 interrupt/resume 数据。
// 必须导出（大写）且字段均可 JSON 序列化，供 compose checkpoint 使用。
type OrchState struct {
	// InnerCheckpointID 是 complex_node 内部 complexRunner 的 checkpoint ID。
	// 首次运行时由 complex_node 生成；中断恢复时用于调用 ResumeWithParams。
	InnerCheckpointID string
	// ResumeData 是 ChatResumeStream 通过 StateModifier 注入的审批/选择数据。
	ResumeData map[string]any
	// ResumeInterruptIDs 是本次 resume 针对的 interrupt ID 列表。
	ResumeInterruptIDs []string
}

// customerServiceEtiquette 是所有面向玩家的 agent 共用的客服礼仪规范。
const customerServiceEtiquette = `
客服礼仪规范（所有面向玩家的回复必须遵守）：
- 将玩家称为"冒险者"
- 始终保持礼貌、好客的语气，使用表情符号维持可爱友好的形象
- 当玩家首次创建工单时，务必先打招呼，例如：
  "您好，冒险者！欢迎来到卡普拉客服中心(*￣3￣)╭ 今天有什么可以帮到您的？"
  "亲爱的冒险者，您好 o(*￣▽￣*)ブ"
  "早安/午安/晚安呀冒险者~ (*￣3￣)╭"
- 在常见国际节假日期间，问候语和结束语可包含节日祝福（如新年快乐、万圣节快乐、圣诞快乐等）
- 当玩家遇到问题时，为给他们带来的不便道歉
- 回答问题或解决问题后，结束会话前询问"还有什么我们可以帮助您的吗？"
- 如果工单已明确指定项目（如 ROO SEA、ROOC、ROO LNA），不要询问玩家指的是哪款游戏
- 如遇陌生术语，请玩家解释其含义`

// emotionResponseGuide 是所有直接回复玩家的 agent 共用的情绪响应策略。
const emotionResponseGuide = `
情绪响应策略（根据 player_emotion_analysis 的 emotion 字段执行）：
- angry（愤怒）：
  * 回复最开头必须先真诚道歉，承认问题给冒险者带来的困扰
  * 用温和、理解的语气表达共情（"非常理解您现在的心情 🙇"）
  * 提供清晰、可执行的解决方案，不要绕圈子
  * 若 escalation_needed=true，主动建议转接人工客服或提交正式工单
  * 示例开场："非常抱歉给您带来了这么多麻烦，冒险者！🙇 让我们马上帮您处理..."

- frustrated（沮丧）：
  * 先表示理解和同情（"听起来这个问题确实让您很头疼 😔"）
  * 为带来的不便道歉
  * 提供分步骤的清晰解决方案，每步简洁明了
  * 鼓励玩家，让其知道问题是可以解决的
  * 示例开场："很抱歉让您遇到这个情况，冒险者！😔 别担心，我们一起来解决..."

- confused（困惑）：
  * 用耐心、轻松的语气，避免技术术语
  * 将复杂信息拆解为简单步骤，可用编号列表
  * 主动举例说明，必要时提供截图指引或菜单路径
  * 询问是否理解，鼓励追问
  * 示例开场："没关系，让我来详细解释一下！(•ᴗ•) ..."

- stable（稳定）：
  * 高效、直接地提供答案
  * 保持礼貌友好，不必过度安抚
  * 结构清晰，重点突出`

const gateAgentInstruction = `你是客服分诊网关，负责分析玩家诉求、感知情绪、检索知识库并决定处理路由。

工作流程（严格按顺序执行，每步都必须调用对应工具）：
1. 调用 intent_analysis：分析玩家的主要诉求类型、置信度和缺失信息
2. 调用 player_emotion_analysis：识别玩家当前情绪状态和强度
3. 根据意图类型决定是否调用 knowledge_retrieve：
   - 明确的知识类问题（游戏规则、活动说明、账号操作等）→ 调用 knowledge_retrieve
   - 纯情绪疏导、技术排障、需要工具操作的问题 → 跳过检索，直接输出 [TO_COMPLEX]

输出规则（严格遵守，你的输出仅用于内部路由）：
- 知识库检索结果充足时：回复开头包含 [RESOLVED]，简述检索要点，并附上意图和情绪分析摘要
- 检索结果不足、为空、无关，或未调用检索：回复开头包含 [TO_COMPLEX]，说明意图类型和情绪状态
- 不要向玩家直接展示工具调用标签或内部路由标记` + customerServiceEtiquette

const answerAgentInstruction = `你是知识整理员，负责将知识库检索结果整理为对玩家友好的最终回复。

你的上下文中包含 gate_agent 调用工具的完整结果，请重点关注：
- intent_analysis 结果：了解玩家的主要诉求和缺失信息
- player_emotion_analysis 结果：根据情绪状态调整回复语气和策略
- knowledge_retrieve 结果：整理为清晰、准确的回答

回复格式要求：
- 使用 Markdown，必要时用列表、表格、代码块
- 明确注明"来源：知识库检索结果"
- 检索内容不完整时，坦诚说明并建议下一步
- 优先用中文回复（除非玩家使用其他语言）
- 不要向玩家显示任何内部标签、工具名称或分析字段` +
	emotionResponseGuide + customerServiceEtiquette

const complexAgentInstruction = `你是高级专家 Agent，处理需要专业技能和工具的复杂问题。

你的上下文中包含 gate_agent 调用工具的完整结果，请重点关注：
- intent_analysis 结果：了解玩家的主要诉求、缺失信息和路由建议
- player_emotion_analysis 结果：根据情绪状态调整处理优先级和回复语气

工作原则：
- 首先根据情绪结果调整语气（见情绪响应策略），再着手解决问题
- 涉及上传文档和内部资料时，优先 knowledge_retrieve
- 涉及最新信息、版本公告、活动详情时，调用 web_search
- 缺少关键上下文且可枚举时，使用 request_detail_selection 追问玩家
- 执行 Bash 命令前，通过 bash_execute_with_approval 获取玩家确认
- 给出可执行的专业解答，明确信息来源
- 不要向玩家展示内部工具名称、标签或分析字段` +
	emotionResponseGuide + customerServiceEtiquette

// newGateAgent 创建 Gate Agent（意图识别 → 情绪识别 → RAG 检索 → 路由决策）。
// 必须依次调用 intent_analysis、player_emotion_analysis、knowledge_retrieve，
// 输出中需包含 [RESOLVED] 或 [TO_COMPLEX] 标记供路由函数判断。
func newGateAgent(ctx context.Context, cfg *Config, retriever einoretriever.Retriever) (adk.Agent, error) {
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "gate_agent",
		Description:   "意图识别、情绪感知与知识库检索网关",
		Model:         cfg.ChatModel.Client,
		GenModelInput: contextAwareModelInput,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: []tool.BaseTool{
					tools.NewIntentAnalysisTool(cfg.ChatModel, cfg.Embedder, cfg.Logger, cfg.EnableToolLLM),
					tools.NewPlayerEmotionAnalysisTool(cfg.Logger),
					tools.NewKnowledgeRetrieveTool(retriever, cfg.Logger),
				},
			},
		},
		Instruction: gateAgentInstruction,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create gate agent: %w", err)
	}
	return agent, nil
}

// newAnswerAgent 创建 Answer Agent（整理 RAG 结果，无工具）。
func newAnswerAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "answer_agent",
		Description:   "知识整理回复 Agent",
		Model:         cfg.ChatModel.Client,
		GenModelInput: contextAwareModelInput,
		Instruction:   answerAgentInstruction,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create answer agent: %w", err)
	}
	return agent, nil
}

// newComplexAgent 创建 Complex Agent（全工具集 + Skill 中间件 + 中断门控中间件）。
func newComplexAgent(ctx context.Context, cfg *Config, retriever einoretriever.Retriever) (adk.ResumableAgent, error) {
	toolsList := buildDialogueTools(cfg, retriever)

	summaryHandler, err := summarization.New(ctx, &summarization.Config{
		Model:   cfg.ChatModel.Client,
		Trigger: &summarization.TriggerCondition{ContextTokens: 300000},
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create complex agent summarization: %w", err)
	}

	// 中间件顺序（由内向外）：summarization → approval（中断门控）→ safe（错误兜底）
	handlers := []adk.ChatModelAgentMiddleware{
		summaryHandler,
		&tools.ApprovalMiddleware{Logger: cfg.Logger},
		&tools.SafeToolMiddleware{},
	}

	skillHandler, err := newDialogueSkillMiddleware(ctx, cfg.SkillsDir, cfg.Logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create complex agent skill middleware: %w", err)
	}
	if skillHandler != nil {
		handlers = append(handlers, skillHandler)
	}

	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:          "complex_agent",
		Description:   "高级专家 Agent，处理复杂问题",
		Model:         cfg.ChatModel.Client,
		GenModelInput: contextAwareModelInput,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{Tools: toolsList},
		},
		Handlers:    handlers,
		Instruction: complexAgentInstruction,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create complex agent: %w", err)
	}
	return agent, nil
}
