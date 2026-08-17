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
	"go_agent/internal/rag"
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

	milvusConfig := common.LoadMilvusConfig(ctx)
	ragConfig := rag.LoadConfig(ctx)
	knowledgePrimary, knowledgeLegacy, opsPrimary, opsLegacy, useHybrid := dialogueRetrieverCollections(ragConfig, milvusConfig)
	var knowledgeRetriever einoretriever.Retriever
	var opsCaseRetriever einoretriever.Retriever
	if useHybrid {
		rewriter := newDialogueQueryRewriter(cfg, ragConfig)
		knowledgeRetriever = newDialogueHybridRetriever(ctx, rag.ProfileKnowledge, knowledgePrimary, knowledgeLegacy, ragConfig, rewriter, cfg.Logger)
		opsCaseRetriever = newDialogueHybridRetriever(ctx, rag.ProfileOpsCase, opsPrimary, opsLegacy, ragConfig, rewriter, cfg.Logger)
	} else {
		knowledgeRetriever = newDialogueMilvusRetriever(ctx, knowledgePrimary, cfg.Logger)
		opsCaseRetriever = newDialogueMilvusRetriever(ctx, opsPrimary, cfg.Logger)
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

func newDialogueHybridRetriever(ctx context.Context, profile rag.RetrievalProfile, primaryCollection, legacyCollection string, ragConfig rag.Config, rewriter rag.QueryRewriter, logger *zap.Logger) einoretriever.Retriever {
	primary := newDialogueMilvusRetriever(ctx, primaryCollection, logger)
	legacy := newDialogueMilvusRetriever(ctx, legacyCollection, logger)

	var bm25 rag.BM25Index
	if ragConfig.BM25Enabled {
		idx, err := rag.NewProfileBM25Index(ragConfig.BM25Root, profile)
		if err != nil {
			if logger != nil {
				logger.Warn("failed to initialize bm25 index for dialogue agent",
					zap.String("profile", string(profile)),
					zap.Error(err))
			}
		} else {
			bm25 = idx
		}
	}

	var reranker rag.Reranker
	if ragConfig.RerankerEnabled && strings.TrimSpace(ragConfig.RerankerURL) != "" {
		reranker = rag.NewHTTPReranker(ragConfig.RerankerURL, ragConfig.RerankerTimeout)
	}

	return rag.NewHybridRetriever(rag.HybridRetrieverConfig{
		Profile:         profile,
		Config:          ragConfig,
		VectorRetriever: primary,
		LegacyRetriever: legacy,
		BM25Index:       bm25,
		Rewriter:        rewriter,
		Reranker:        reranker,
	})
}

func newDialogueQueryRewriter(cfg *Config, ragConfig rag.Config) rag.QueryRewriter {
	if ragConfig.RewriteEnabled && cfg != nil && cfg.ChatModel != nil && cfg.ChatModel.Client != nil {
		return rag.NewChatModelRewriter(cfg.ChatModel.Client)
	}
	return rag.NoopRewriter{}
}

func dialogueRetrieverCollections(ragConfig rag.Config, milvusConfig common.MilvusConfig) (knowledgePrimary, knowledgeLegacy, opsPrimary, opsLegacy string, useHybrid bool) {
	if !ragConfig.HybridEnabled {
		return milvusConfig.Collection, "", common.MilvusOpsCollection, "", false
	}
	return milvusConfig.KnowledgeV2Collection, milvusConfig.Collection, milvusConfig.OpsV2Collection, common.MilvusOpsCollection, true
}

func newDialogueMilvusRetriever(ctx context.Context, collection string, logger *zap.Logger) einoretriever.Retriever {
	collection = strings.TrimSpace(collection)
	if collection == "" {
		return nil
	}
	rtr, err := airetriever.NewMilvusRetrieverWithCollection(ctx, collection)
	if err != nil {
		if logger != nil {
			logger.Warn("failed to initialize milvus retriever for dialogue agent",
				zap.String("collection", collection),
				zap.Error(err))
		}
		return nil
	}
	return rtr
}
