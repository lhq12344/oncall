package knowledge

import (
	"context"
	"fmt"

	"go_agent/internal/ai/models"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/indexer"
	"github.com/cloudwego/eino/components/retriever"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// Config Knowledge Agent 配置
type Config struct {
	ChatModel  *models.ChatModel
	Retriever  retriever.Retriever
	Indexer    indexer.Indexer
	Logger     *zap.Logger
}

// NewKnowledgeAgent 创建 Knowledge Agent（基于 ChatModelAgent + RAG）
func NewKnowledgeAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	// 创建工具集
	tools := []tool.BaseTool{
		NewVectorSearchTool(cfg.Retriever),
		NewKnowledgeIndexTool(cfg.Indexer),
	}

	// 创建 ChatModelAgent
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Model: cfg.ChatModel.Client,
		Tools: &adk.ToolsConfig{
			Tools: tools,
		},
		SystemPrompt: `你是一个知识库助手，负责检索历史故障案例和最佳实践。

你的职责：
1. 使用 vector_search 工具检索相关的历史案例
2. 根据相似度和时效性对案例进行排序
3. 提取关键的解决方案和最佳实践
4. 如果没有找到相关案例，明确告知用户

注意：
- 优先返回成功率高的案例
- 关注案例的时效性（最近的案例更有参考价值）
- 提供具体的操作步骤，而不是泛泛而谈`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create knowledge agent: %w", err)
	}

	return agent, nil
}

// VectorSearchTool 向量检索工具
type VectorSearchTool struct {
	retriever retriever.Retriever
}

func NewVectorSearchTool(r retriever.Retriever) tool.BaseTool {
	return &VectorSearchTool{retriever: r}
}

func (t *VectorSearchTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "vector_search",
		Desc: "基于语义相似度检索历史故障案例。输入查询文本，返回最相关的案例列表。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"query": {
				Type:     schema.String,
				Desc:     "搜索查询文本（故障描述或关键词）",
				Required: true,
			},
			"top_k": {
				Type:     schema.Integer,
				Desc:     "返回结果数量（默认 5）",
				Required: false,
			},
		}),
	}, nil
}

func (t *VectorSearchTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	if t.retriever == nil {
		return "向量检索功能尚未配置（需要 Milvus）", nil
	}

	// TODO: 解析参数并调用 retriever
	// 当前返回占位符
	return "知识检索功能开发中...", nil
}

// KnowledgeIndexTool 知识索引工具
type KnowledgeIndexTool struct {
	indexer indexer.Indexer
}

func NewKnowledgeIndexTool(idx indexer.Indexer) tool.BaseTool {
	return &KnowledgeIndexTool{indexer: idx}
}

func (t *KnowledgeIndexTool) Info(ctx context.Context) (*schema.ToolInfo, error) {
	return &schema.ToolInfo{
		Name: "knowledge_index",
		Desc: "将新的故障案例索引到知识库。输入案例内容，自动分块、向量化并存储。",
		ParamsOneOf: schema.NewParamsOneOfByParams(map[string]*schema.ParameterInfo{
			"content": {
				Type:     schema.String,
				Desc:     "案例内容（包含问题描述、解决方案、结果）",
				Required: true,
			},
			"metadata": {
				Type:     schema.Object,
				Desc:     "案例元数据（标签、时间、成功率等）",
				Required: false,
			},
		}),
	}, nil
}

func (t *KnowledgeIndexTool) InvokableRun(ctx context.Context, argumentsInJSON string, opts ...tool.Option) (string, error) {
	if t.indexer == nil {
		return "知识索引功能尚未配置（需要 Milvus）", nil
	}

	// TODO: 解析参数并调用 indexer
	return "知识索引功能开发中...", nil
}
