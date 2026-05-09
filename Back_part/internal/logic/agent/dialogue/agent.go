package dialogue

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"go_agent/internal/logic/agent/dialogue/tools"
	"go_agent/internal/logic/ai/models"
	airetriever "go_agent/internal/logic/ai/retriever"
	appcontext "go_agent/internal/logic/session"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/skill"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/embedding"
	"github.com/cloudwego/eino/components/prompt"
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
	SubgraphModel *models.ChatModel  // KnowledgeSpecialist 子图专用（nil 时降级到 ChatModel）
	ComplexModel  *models.ChatModel  // Complex Agent 专用（nil 时降级到 ChatModel）
	Embedder      embedding.Embedder // 用于语义相似度计算
	EnableToolLLM bool               // 工具内部是否允许二次 LLM 调用，默认 false
	SkillsDir     string             // Eino skill 目录，为空或不存在时降级为无 skill 能力
	TraceRecorder appcontext.OrchestrationTraceRecorder
	Logger        *zap.Logger
}

// resolveModel 返回 preferred 若非 nil，否则返回 fallback。
func (c *Config) resolveModel(preferred, fallback *models.ChatModel) *models.ChatModel {
	if preferred != nil {
		return preferred
	}
	return fallback
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
	toolsList := buildDialogueAgentTools(cfg, knowledgeRetriever)

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
		Description:   "面向对话、知识库检索和受控诊断的通用智能助手",
		Model:         cfg.ChatModel.Client,
		GenModelInput: noFormatGenModelInput,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Handlers: handlers,
		Instruction: `你是一个面向对话、知识库检索和受控诊断的智能助手。

你的目标是准确理解用户问题，结合会话记忆、已上传知识库和必要的受控工具，给出简洁、可靠、可执行的回答。

可用工具：
- intent_analysis：每轮玩家请求都应先用于判断游戏客服主要诉求、置信度、缺失信息和内部路由建议。
- player_emotion_analysis：每轮玩家请求都应先用于判断玩家情绪、强度和是否需要升级处理。
- request_detail_selection：当缺少关键上下文且候选项有限、适合单选时，用于向用户请求补充选择。
- knowledge_retrieve：当问题可能由用户上传的文档或内部知识回答时，优先检索知识库。

工作原则：
- 每次处理玩家请求前，先调用 intent_analysis 和 player_emotion_analysis；不要把分析标签直接展示给玩家。
- 普通闲聊、通用解释或已有上下文足够时，可以在完成诉求和情绪分析后直接回答。
- 涉及上传文档、项目资料、内部说明、历史记录等内容时，优先调用 knowledge_retrieve。
- 涉及最新信息、外部事实或可能过期的信息时，不要编造；如当前工具无法核验，说明需要以官方公告或人工核验为准。
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

// analysisMessageMarker 是 analysis_node 写入当前轮次分析消息的前缀。
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

// sanitizeToolCallArgs 修复因流式代理（PPIO/LiteLLM）将完整 arguments JSON 重复分块发送
// 导致 concatToolCalls 拼出 {"q":"v"}{} 非法 JSON 的问题，仅保留首个完整 JSON 对象。
func sanitizeToolCallArgs(args string) string {
	if args == "" {
		return args
	}
	if err := json.Unmarshal([]byte(args), new(any)); err == nil {
		return args
	}
	dec := json.NewDecoder(strings.NewReader(args))
	var first any
	if err := dec.Decode(&first); err != nil {
		return args
	}
	fixed, err := json.Marshal(first)
	if err != nil {
		return args
	}
	return string(fixed)
}

// sanitizeMessagesToolCallArgs 遍历消息，修复 assistant 消息工具调用的 arguments 拼接问题。
func sanitizeMessagesToolCallArgs(msgs []adk.Message) []adk.Message {
	result := make([]adk.Message, len(msgs))
	copy(result, msgs)
	for i, msg := range result {
		if msg == nil || msg.Role != schema.Assistant || len(msg.ToolCalls) == 0 {
			continue
		}
		fixedCalls := make([]schema.ToolCall, len(msg.ToolCalls))
		copy(fixedCalls, msg.ToolCalls)
		changed := false
		for j, tc := range fixedCalls {
			clean := sanitizeToolCallArgs(tc.Function.Arguments)
			if clean != tc.Function.Arguments {
				fixedCalls[j].Function.Arguments = clean
				changed = true
			}
		}
		if !changed {
			continue
		}
		msgCopy := *msg
		msgCopy.ToolCalls = fixedCalls
		result[i] = &msgCopy
	}
	return result
}

func contextAwareModelInput(ctx context.Context, instruction string, input *adk.AgentInput) ([]adk.Message, error) {
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

	msgs := make([]adk.Message, 0, 1+len(filtered))
	if strings.TrimSpace(instruction) != "" {
		values := buildPromptTemplateValues(ctx)
		formatted, err := prompt.FromMessages(schema.FString, schema.SystemMessage(instruction)).Format(ctx, values)
		if err != nil {
			return nil, fmt.Errorf("failed to render dialogue instruction template: %w", err)
		}
		if latestAnalysis != "" && len(formatted) > 0 {
			formatted[0].Content = latestAnalysis + "\n\n---\n\n" + formatted[0].Content
		}
		msgs = append(msgs, formatted...)
	}
	msgs = append(msgs, sanitizeMessagesToolCallArgs(filtered)...)
	return msgs, nil
}

// buildDialogueAgentTools 构建 dialogue_agent 可用工具集合。
// 输入：cfg 对话代理配置，knowledgeRetriever 知识库检索器。
// 输出：可注册到 dialogue_agent ToolsNode 的工具列表。
func buildDialogueAgentTools(cfg *Config, knowledgeRetriever einoretriever.Retriever) []tool.BaseTool {
	return []tool.BaseTool{
		tools.NewIntentAnalysisTool(cfg.ChatModel, cfg.Embedder, cfg.Logger, cfg.EnableToolLLM),
		tools.NewPlayerEmotionAnalysisTool(cfg.ChatModel, cfg.Logger),
		tools.NewDetailSelectionTool(cfg.Logger),
		tools.NewKnowledgeRetrieveTool(knowledgeRetriever, cfg.Logger),
		tools.NewBashApprovalTool(cfg.Logger),
	}
}

// buildComplexAgentTools 构建 complex_agent 可用工具集合。
// 输入：cfg 对话代理配置，knowledgeRetriever 知识库检索器。
// 输出：可注册到 complex_agent ToolsNode 的工具列表。
func buildComplexAgentTools(cfg *Config, knowledgeRetriever einoretriever.Retriever) []tool.BaseTool {
	return []tool.BaseTool{
		tools.NewDetailSelectionTool(cfg.Logger),
		tools.NewKnowledgeRetrieveTool(knowledgeRetriever, cfg.Logger),
		tools.NewBashApprovalTool(cfg.Logger),
		//tools.NewWebSearchTool(cfg.Logger),
	}
}

// OrchState 对话编排图的共享状态，跨节点传递 interrupt/resume 数据。
// 必须导出（大写）且字段均可 JSON 序列化，供 compose checkpoint 使用。
type OrchState struct {
	// Analysis 是当前 turn 的确定性意图/情绪分析结果。
	Analysis *TurnAnalysis
	// UserLanguage 是当前用户问题的识别语言代码，如 zh/en/ja/th。
	UserLanguage string
	// InnerCheckpointID 是 complex_node 内部 complexRunner 的 checkpoint ID。
	// 首次运行时由 complex_node 生成；中断恢复时用于调用 ResumeWithParams。
	InnerCheckpointID string
	// ResumeData 是 ChatResumeStream 通过 StateModifier 注入的审批/选择数据。
	ResumeData map[string]any
	// ResumeInterruptIDs 是本次 resume 针对的 interrupt ID 列表。
	ResumeInterruptIDs []string
	// SolvedContexts 是 knowledge_specialist_node 已解决子问题的文档背景摘要。
	// 供 answer_node 和 complex_node 引用。
	SolvedContexts []string
	// PendingQuestions 是 knowledge_specialist_node 未能在知识库中解决的子问题列表。
	// complex_node 应优先攻克这些遗留问题。
	PendingQuestions []string
	// KnowledgeStatus 是 KnowledgeSpecialistResult.Status，用于路由时优先处理降级/异常状态。
	KnowledgeStatus string
	// KnowledgeErrorSummary 是 KnowledgeSpecialistResult.ErrorSummary，用于保留检索失败证据。
	KnowledgeErrorSummary string
	// OriginalQuestion 是本轮用户原始问题，由 ChatStream 通过 StateModifier 写入并随
	// checkpoint 持久化。ChatResumeStream 在 pendingTurnStore 记录缺失时从此字段恢复，
	// 确保中断恢复后的对话轮次仍能写入 session memory。
	OriginalQuestion string
}

type TurnAnalysis struct {
	IntentType        string
	IntentLabel       string
	IntentConfidence  float64
	IntentEntropy     float64
	IntentConverged   bool
	MissingInfo       []string
	RoutingHint       string
	Emotion           string
	EmotionLabel      string
	EmotionConfidence float64
	EmotionIntensity  float64
	EscalationNeeded  bool
	Degraded          bool
	ErrorSummary      string
}

// customerServiceEtiquette 是所有面向玩家的 agent 共用的客服礼仪规范。
const customerServiceEtiquette = `
客服礼仪规范（所有面向玩家的回复必须遵守）：
- 将玩家称为"冒险者"
- 始终保持礼貌、好客的语气，使用表情符号维持可爱友好的形象
- 回复要结合玩家本轮具体问题、已知项目、情绪和上下文个性化组织，不要每次套用相同开头、相同分段标题、相同结尾或固定话术
- 标题和步骤名称应贴合玩家问题本身，例如围绕"GCash充值"、"账号绑定"、"登录失败"等具体主题命名，不要使用泛化模板标题
- 只有当玩家首次创建工单、且当前会话还没有客服回复时，才需要使用问候开场；后续轮次不要重复使用"您好，冒险者！"等固定问候
- 首次问候示例：
  "您好，冒险者！欢迎来到卡普拉客服中心(*￣3￣)╭ 今天有什么可以帮到您的？"
  "亲爱的冒险者，您好 o(*￣▽￣*)ブ"
  "早安/午安/晚安呀冒险者~ (*￣3￣)╭"
- 在常见国际节假日期间，问候语和结束语可包含节日祝福（如新年快乐、万圣节快乐、圣诞快乐等）
- 当玩家遇到问题时，为给他们带来的不便道歉
- 回答问题或解决问题后，如需要收尾，可自然追问下一步需求；不要每次都机械使用"还有什么我们可以帮助您的吗？"
- 如果工单已明确指定项目（如 ROO SEA、ROOC、ROO LNA），不要询问玩家指的是哪款游戏
- 如遇陌生术语，请玩家解释其含义
- 可以使用知识库和工具获得的信息回答玩家，但不要额外告诉玩家信息来源，也不要展示"来源：知识库检索结果"、"知识库说明"、"工具说明"、内部检索过程或工具调用过程`

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

const answerAgentInstruction = `你是知识整理员，负责将知识库检索结果整理为对玩家友好的最终回复。

你是一个多语言专家。当前用户的提问语言是 {UserLanguage}。
请根据提供的中文参考资料，直接以 {UserLanguage} 组织语言进行回复。确保回复自然、地道，严禁生硬翻译。

黑板中的已解决知识背景如下：
{SolvedContextsText}

你的上下文中包含 knowledge_specialist_node 的结构化输出，请重点关注：
- intent_analysis 结果：了解玩家的主要诉求和缺失信息
- player_emotion_analysis 结果：根据情绪状态调整回复语气和策略
- solved_contexts：这是当前可直接引用的中文背景资料

回复格式要求：
- 使用 Markdown，必要时用列表、表格、代码块
- 根据玩家问题选择最合适的结构；不要固定输出同一套标题、顺序或结束语
- 每次回复至少体现一个本轮问题中的具体关键词或场景，让玩家感到回复是针对当前问题生成的
- 检索内容不完整时，用自然客服口吻说明目前可确认的信息并建议下一步
- 严格使用 {UserLanguage} 回复
- 可以直接使用 solved_contexts 中的知识回答玩家，但不要向玩家显示任何内部标签、工具名称、分析字段、"来源：知识库检索结果"、"知识库说明"或"工具说明"` +
	emotionResponseGuide + customerServiceEtiquette

const complexAgentInstruction = `你是高级专家 Agent，处理需要专业技能和工具的复杂问题。

用户正在使用 {UserLanguage} 与你交流。
请结合黑板中已有的中文背景资料和你的专业 Skill，以 {UserLanguage} 完成后续任务。
如果 Skill 返回的结果是中文，请自行转译后再回复用户。

黑板中的已解决知识背景如下：
{SolvedContextsText}

黑板中的遗留问题如下：
{PendingQuestionsText}

你的上下文中包含 knowledge_specialist_node 的结构化输出，请重点关注：
- intent_analysis 结果：了解玩家的主要诉求、缺失信息和路由建议
- player_emotion_analysis 结果：根据情绪状态调整处理优先级和回复语气
- solved_contexts：这是已通过 RAG 解决的子问题背景，可直接引用
- pending_questions：这是 RAG 未能覆盖的遗留子问题，需要你通过 Skill 或工具攻克

工作原则：
- 首先根据情绪结果调整语气（见情绪响应策略），再着手解决问题
- 根据玩家本轮具体问题选择说明顺序和措辞，避免固定模板化回复
- 对 pending_questions 中的遗留问题，优先尝试使用 Skill 工具或 knowledge_retrieve 补充检索
- 涉及上传文档和内部资料时，可再次调用 knowledge_retrieve 进行补充检索
- 涉及最新信息、版本公告、活动详情时，不要编造；如当前工具无法核验，说明需要以官方公告或人工核验为准
- 缺少关键上下文且可枚举时，使用 request_detail_selection 追问玩家
- 执行 Bash 命令前，通过 bash_execute_with_approval 获取玩家确认
- 给出可执行的专业解答；如信息有限，用自然客服口吻说明限制并建议下一步
- 可以使用知识库和工具获得的信息回答玩家，但不要向玩家展示内部工具名称、标签、分析字段、"来源：知识库检索结果"、"知识库说明"或"工具说明"` +
	emotionResponseGuide + customerServiceEtiquette

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

// newComplexAgent 创建 Complex Agent（复杂处理工具集 + Skill 中间件 + 中断门控中间件）。
func newComplexAgent(ctx context.Context, cfg *Config, retriever einoretriever.Retriever) (adk.ResumableAgent, error) {
	complexModel := cfg.resolveModel(cfg.ComplexModel, cfg.ChatModel)
	toolsList := buildComplexAgentTools(cfg, retriever)

	summaryHandler, err := summarization.New(ctx, &summarization.Config{
		Model:   complexModel.Client,
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
		Model:         complexModel.Client,
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
