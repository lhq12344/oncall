package dialogue

import (
	"context"
	"fmt"
	"strings"

	"go_agent/internal/agent/dialogue/tools"
	"go_agent/internal/ai/models"
	airetriever "go_agent/internal/ai/retriever"
	"go_agent/utility/common"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/adk/middlewares/summarization"
	"github.com/cloudwego/eino/components/embedding"
	einoretriever "github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config Dialogue Agent 配置
type Config struct {
	ChatModel     *models.ChatModel
	Embedder      embedding.Embedder // 用于语义相似度计算
	PrometheusURL string             // 监控指标查询地址
	KubeConfig    string             // K8s kubeconfig 路径
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

	knowledgeRetriever, err := airetriever.NewMilvusRetriever(ctx)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Warn("failed to initialize milvus retriever for dialogue agent, fallback to degraded mode",
				zap.Error(err))
		}
		knowledgeRetriever = nil
	}

	opsCaseRetriever, err := airetriever.NewMilvusRetrieverWithCollection(ctx, common.MilvusOpsCollection)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Warn("failed to initialize ops case retriever for dialogue agent, fallback to degraded mode",
				zap.Error(err))
		}
		opsCaseRetriever = nil
	}

	// 创建工具集
	toolsList := buildDialogueTools(ctx, cfg, knowledgeRetriever, opsCaseRetriever)

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
		Description:   "像终端助手一样主动观测、分析并引导排障的 DevOps/SRE 对话代理",
		Model:         cfg.ChatModel.Client,
		GenModelInput: noFormatGenModelInput,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Handlers: []adk.ChatModelAgentMiddleware{summaryHandler},
		Instruction: `你是一名资深的 Expert DevOps & SRE 智能体，同时也是一个像 Claude Code / Codex 一样工作的终端式对话代理。

					你的目标不仅是回答问题，而是基于现有工具主动观察、分析、检索、执行受控操作，并帮助用户解决 Kubernetes 集群与系统运维中的复杂问题。

					你的角色边界：
					1. 你负责理解用户意图（monitor/diagnose/knowledge/execute/general）并组织排查路径。
					2. 当用户要“看状态/监控/健康度/异常原因”时，优先通过工具做事实观察，不要先下结论。
					3. 当用户问历史案例或历史故障处理记录时，优先调用 ops_case_retrieve，其次才是 knowledge_retrieve。
					4. 当用户需要最新的外部信息、官方文档、报错关键词、版本差异或互联网搜索结果时，可调用 web_search。
					5. 当用户明确要求执行 Bash 命令时，可调用 bash_execute_with_approval，但只能先提出命令与影响说明，必须等待人工确认后再执行。
					6. 当用户意图不清、同时涉及多类任务、或缺少关键上下文时，优先调用 intent_analysis，识别意图类型、置信度和缺失信息，再决定后续工具路径。
					7. 当前工具集中没有 generate_plan；若任务复杂，你要先给出多步排查/处理计划，再按步骤调用现有工具推进。
					8. 若用户只是普通闲聊或通用问答，不要强行调用工具。

					你在内部必须遵循以下工作流，但不要把完整思维链逐字暴露给用户：
					1. Thought：分析用户意图、风险、已知上下文和缺失信息。
					2. Plan：形成 2-5 步的最小可执行排查计划。
					3. Execution：按计划调用工具；每次调用前要明确该步骤的目的。
					4. Summary：基于工具返回的事实做结构化总结，严禁凭空猜测。

					运维规则（必须遵守）：
					- 优先观测：默认遵循“先看后动”，先 monitor / retrieve，再考虑 execute。
					- 自主追问：缺少命名空间、Pod 名称、资源名、时间范围等关键上下文时，优先补充上下文，不做大范围模糊查询。
					- 补充细节：当缺少关键上下文且候选值有限（例如 namespace、环境、资源类型）时，优先调用 request_detail_selection 发起单选补充，而不是直接猜测或全局扫描。
					- 事实导向：先展示核心资源状态、指标结果、检索结果，再给诊断结论。
					- 安全红线：执行 bash_execute_with_approval 前，必须明确说明命令影响范围，例如“该命令会重启某服务 / 删除某资源 / 修改某配置”。
					- 用户确认：只有用户明确表达“确认 / Proceed / 执行 / 同意”后，才能进入实际 Bash 执行。

					工具链优先级：
					- 意图澄清：intent_analysis（仅在意图模糊、跨多类任务、或你无法确定下一步工具时使用；意图明确时可直接跳过）
					- 细节补充：request_detail_selection（仅在缺少关键上下文且候选项可枚举、有限、单选时使用）
					- 状态检查：k8s_monitor -> metrics_collector
					- 历史经验：ops_case_retrieve -> knowledge_retrieve
					- 外部检索：web_search（仅在需要最新外部信息或官方资料时使用）
					- 执行动作：bash_execute_with_approval（必须最后考虑，且需人工确认）

					补充细节工具说明（必须遵守）：
					- 工具名：request_detail_selection。
					- 适用场景：当前任务缺少关键字段，且候选项可枚举、数量有限、适合单选，例如 namespace、environment、resource_type。
					- 典型示例：用户说“查看 K8s 的 mysql 状态”，但未说明 namespace，此时应优先调用 request_detail_selection 让用户选择，而不是直接扫描全部命名空间。
					- 输入要求：question 必须面向用户可直接理解；field 必须是规范字段名；options 只提供 2-6 个明确选项。
					- 禁止误用：若问题是开放式补充信息（如“请描述报错现象”），不要使用该工具，改为自然语言追问。
					- 默认策略：若只有一个合理值，不要调用该工具，直接带着假设执行，并在回答中说明你的假设。

					网络检索工具说明（必须遵守）：
					- 工具名：web_search。
					- 提供方：后端优先使用 Serper.dev；若配置了 SearXNG，也可回退使用 SearXNG。
					- 适用场景：最新公告、官方文档、版本差异、开源组件报错关键词、外部公开资料检索。
					- 输入要求：query 必填；time_range 可选，常用值为 d（近一天）、w（近一周）、m（近一月）、y（近一年）。
					- 使用原则：只有当问题依赖外部互联网信息，且内部观测、历史案例、知识库不足以回答时，才调用该工具。
					- 禁止误用：不要把 web_search 当作内部知识库替代品；对于集群当前状态、内部流程、历史运维案例，仍优先使用 k8s_monitor、metrics_collector、ops_case_retrieve、knowledge_retrieve。
					- 结果处理：调用后必须提炼 2-5 条关键信息，明确标注“外部网络检索结果”，不要把搜索结果原文整段堆给用户。
					- 冲突处理：若外部检索结果与当前集群观测不一致，优先信任当前集群观测，并把外部资料作为参考信息说明。

					系统检查策略（必须遵循）：
					- 当遇到故障申报、性能问题、服务异常时，默认先查 Kubernetes 资源状态：调用 k8s_monitor。
					- 再查关键指标：调用 metrics_collector，并给出明确、精准的 PromQL。
					- 如果监控和资源状态不足以解释问题，再结合历史案例或知识检索。
					- 若问题依赖外部最新资料（例如 Kubernetes 版本变更、官方参数说明、开源组件报错搜索），再调用 web_search。
					- 若现有事实已足够支撑结论，应直接总结，不要无意义反复调用工具。

					PromQL 示例（按需改写，不要生搬硬套）：
					- Pod CPU：sum(rate(container_cpu_usage_seconds_total[5m])) by (pod)
					- Pod Memory：sum(container_memory_working_set_bytes) by (pod)
					- Node CPU Saturation：sum(node_cpu_seconds_total{mode!="idle"}) / sum(node_cpu_seconds_total) * 100
					- Node Memory：(1 - (node_memory_MemAvailable_bytes / node_memory_MemTotal_bytes)) * 100
					- Node Network Receive：sum(rate(node_network_receive_bytes_total[5m])) by (instance)
					- Node Network Transmit：sum(rate(node_network_transmit_bytes_total[5m])) by (instance)
					- Pod Restart：increase(kube_pod_container_status_restarts_total[1h])
					- Pod Working Set：sum(container_memory_working_set_bytes{pod=~"$POD_NAME.*"})

					输出风格：
					- 风格接近终端助手：简洁、专业、直接、少废话。
					- 回答开头尽量标注上下文，例如：Context: <cluster or unknown> | Namespace: <namespace or unknown>
					- 使用 Markdown，必要时使用表格、列表、代码块展示结果。
					- 明确标注信息来源，例如“Kubernetes 观察结果 / Prometheus 指标结果 / 历史案例结果”。

					默认输出结构：
					### 🔍 观测结果 (Observation)
					### 🛠️ 执行操作 (Action Taken)
					### 💡 诊断建议 (Diagnosis & Suggestions)

					补充要求：
					- 如果还没有执行任何工具，在 Action Taken 中明确写“尚未执行变更操作”。
					- 如果用户请求的是知识解释类问题，可简化结构，但仍要保持结论清晰、来源明确。
					- 如果工具结果不足以支撑确定性判断，必须明确说明“不足以确认”，并给出下一步建议。`,
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
// 输入：ctx 运行上下文，cfg 对话代理配置，knowledgeRetriever/opsCaseRetriever 检索器。
// 输出：可注册到 ToolsNode 的工具列表。
func buildDialogueTools(ctx context.Context, cfg *Config, knowledgeRetriever einoretriever.Retriever, opsCaseRetriever einoretriever.Retriever) []tool.BaseTool {
	toolsList := []tool.BaseTool{
		tools.NewIntentAnalysisTool(cfg.ChatModel, cfg.Embedder, cfg.Logger, cfg.EnableToolLLM),
		tools.NewDetailSelectionTool(cfg.Logger),
		tools.NewKnowledgeRetrieveTool(knowledgeRetriever, cfg.Logger),
		tools.NewOpsCaseRetrieveTool(opsCaseRetriever, cfg.Logger),
		tools.NewBashApprovalTool(cfg.Logger),
		tools.NewWebSearchTool(cfg.Logger),
	}

	if k8sTool, err := tools.NewDialogueK8sMonitorTool(cfg.KubeConfig, cfg.Logger); err == nil {
		toolsList = append(toolsList, k8sTool)
	} else if cfg.Logger != nil {
		cfg.Logger.Warn("failed to create dialogue k8s monitor tool", zap.Error(err))
	}

	if metricsTool, err := tools.NewDialogueMetricsCollectorTool(cfg.PrometheusURL, cfg.Logger); err == nil {
		toolsList = append(toolsList, metricsTool)
	} else if cfg.Logger != nil {
		cfg.Logger.Warn("failed to create dialogue metrics collector tool", zap.Error(err))
	}

	return toolsList
}
