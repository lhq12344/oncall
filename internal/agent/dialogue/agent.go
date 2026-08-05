package dialogue

import (
	"context"
	"fmt"
	"strings"

	"go_agent/internal/agent/dialogue/tools"
	"go_agent/internal/agent/toolkit"
	"go_agent/internal/ai/models"
	airetriever "go_agent/internal/ai/retriever"
	"go_agent/internal/compact"
	"go_agent/internal/permissions"
	"go_agent/internal/prompt"
	"go_agent/utility/common"

	"github.com/cloudwego/eino/adk"
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

	compactHandler := compact.NewMiddleware(compact.Config{Model: cfg.ChatModel.Client})

	env := prompt.DetectEnvironment("")
	instruction := prompt.BuildAgentPrompt(prompt.RoleDialogue, env, prompt.BuildOptions{})

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
		Handlers:    []adk.ChatModelAgentMiddleware{compactHandler},
		Instruction: instruction,
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
	deferredTools := []tool.BaseTool{
		tools.NewIntentAnalysisTool(cfg.ChatModel, cfg.Embedder, cfg.Logger, cfg.EnableToolLLM),
		tools.NewDetailSelectionTool(cfg.Logger),
		tools.NewKnowledgeRetrieveTool(knowledgeRetriever, cfg.Logger),
		tools.NewOpsCaseRetrieveTool(opsCaseRetriever, cfg.Logger),
		tools.NewBashApprovalTool(cfg.Logger),
		tools.NewWebSearchTool(cfg.Logger),
	}

	if k8sTool, err := tools.NewDialogueK8sMonitorTool(cfg.KubeConfig, cfg.Logger); err == nil {
		deferredTools = append(deferredTools, k8sTool)
	} else if cfg.Logger != nil {
		cfg.Logger.Warn("failed to create dialogue k8s monitor tool", zap.Error(err))
	}

	if metricsTool, err := tools.NewDialogueMetricsCollectorTool(cfg.PrometheusURL, cfg.Logger); err == nil {
		deferredTools = append(deferredTools, metricsTool)
	} else if cfg.Logger != nil {
		cfg.Logger.Warn("failed to create dialogue metrics collector tool", zap.Error(err))
	}

	checker := permissions.NewChecker(permissions.Options{})
	return toolkit.BuildAlwaysEinoTools(ctx, checker, deferredTools...)
}
