package dialogue

import (
	"context"
	"fmt"

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

// NewDialogueAgent 创建 Dialogue Agent（意图分析 + 问题预测）
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
	toolsList := buildDialogueTools(cfg, knowledgeRetriever, opsCaseRetriever)

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
		Name:        "dialogue_agent",
		Description: "分析用户意图、预测问题并引导对话的对话代理",
		Model:       cfg.ChatModel.Client,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: toolsList,
			},
		},
		Handlers: []adk.ChatModelAgentMiddleware{summaryHandler},
		Instruction: `你是一个对话助手，负责理解用户意图并引导对话。

					你的职责：
					1. 分析用户输入意图（monitor/diagnose/knowledge/execute/general）
					2. 当用户要“看状态/监控/健康度”时，优先调用系统检查工具
					3. 当用户问历史案例或知识时，调用 knowledge_retrieve 检索相似文本
					4. 当用户问历史故障处理记录时，优先调用 ops_case_retrieve（该库与通用知识库隔离）
					5. 当信息不足时先追问，再给建议

					系统检查提示词工程（必须遵循）：
					- 先查 Kubernetes 资源状态：调用 k8s_monitor
					- 再查关键指标：调用 metrics_collector（提供明确 PromQL）
					- 先事实后建议：先返回观测结果，再给出下一步排查建议
					- 缺少上下文时必须追问：例如命名空间、资源名、时间范围

					常用监控查询示例（按需改写）：
					- CPU: sum(rate(container_cpu_usage_seconds_total[5m])) by (pod)
					- Memory: sum(container_memory_working_set_bytes) by (pod)
					- Pod 重启: increase(kube_pod_container_status_restarts_total[1h])

					回答风格：
					- 结构化输出：现状 -> 发现 -> 建议
					- 明确标注工具结果来源，避免猜测`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create dialogue agent: %w", err)
	}

	return agent, nil
}

func buildDialogueTools(cfg *Config, knowledgeRetriever einoretriever.Retriever, opsCaseRetriever einoretriever.Retriever) []tool.BaseTool {
	toolsList := []tool.BaseTool{
		tools.NewIntentAnalysisTool(cfg.ChatModel, cfg.Embedder, cfg.Logger, cfg.EnableToolLLM),
		tools.NewQuestionPredictionTool(cfg.ChatModel, cfg.Logger, cfg.EnableToolLLM),
		tools.NewKnowledgeRetrieveTool(knowledgeRetriever, cfg.Logger),
		tools.NewOpsCaseRetrieveTool(opsCaseRetriever, cfg.Logger),
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
