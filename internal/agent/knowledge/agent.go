package knowledge

import (
	"context"
	"fmt"

	"go_agent/internal/ai/indexer"
	"go_agent/internal/ai/models"
	"go_agent/internal/ai/retriever"

	"github.com/cloudwego/eino/adk"
	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/compose"
	"go.uber.org/zap"
	"go_agent/internal/agent/knowledge/tools"
)

// Config Knowledge Agent 配置
type Config struct {
	ChatModel *models.ChatModel
	Logger    *zap.Logger
}

// NewKnowledgeAgent 创建 Knowledge Agent（基于 ChatModelAgent + RAG）
func NewKnowledgeAgent(ctx context.Context, cfg *Config) (adk.Agent, error) {
	if cfg == nil {
		return nil, fmt.Errorf("config is required")
	}

	if cfg.ChatModel == nil {
		return nil, fmt.Errorf("chat model is required")
	}

	// 初始化 Milvus Retriever
	milvusRetriever, err := retriever.NewMilvusRetriever(ctx)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Warn("failed to create milvus retriever, using placeholder", zap.Error(err))
		}
		milvusRetriever = nil
	}

	// 初始化 Milvus Indexer
	milvusIndexer, err := indexer.NewMilvusIndexer(ctx)
	if err != nil {
		if cfg.Logger != nil {
			cfg.Logger.Warn("failed to create milvus indexer, using placeholder", zap.Error(err))
		}
		milvusIndexer = nil
	}

	// 创建工具集
	tools := []tool.BaseTool{
		tools.NewVectorSearchTool(milvusRetriever, cfg.Logger),
		tools.NewKnowledgeIndexTool(milvusIndexer, cfg.Logger),
	}

	// 创建 ChatModelAgent
	agent, err := adk.NewChatModelAgent(ctx, &adk.ChatModelAgentConfig{
		Name:        "knowledge_agent",
		Description: "检索历史故障案例和最佳实践的知识库代理",
		Model:       cfg.ChatModel.Client,
		ToolsConfig: adk.ToolsConfig{
			ToolsNodeConfig: compose.ToolsNodeConfig{
				Tools: tools,
			},
		},
		Instruction: `你是一个知识库助手，负责检索历史故障案例和最佳实践。

你的职责：
1. 使用 vector_search 工具检索相关的历史案例
2. 使用 knowledge_index 工具索引新的文档到知识库
3. 根据相似度和时效性对案例进行排序
4. 提取关键的解决方案和最佳实践
5. 如果没有找到相关案例，明确告知用户

索引文档时的注意事项：
- knowledge_index 工具会自动将长文档分片（默认 1000 字符/片）
- 可以通过 chunk_size 参数调整分片大小
- 每个分片会自动添加文档标题和位置信息作为上下文
- 分片后的文档更适合向量检索

检索时的注意事项：
- 优先返回成功率高的案例
- 关注案例的时效性（最近的案例更有参考价值）
- 提供具体的操作步骤，而不是泛泛而谈
- 如果检索到多个分片，综合所有分片内容给出完整答案`,
	})

	if err != nil {
		return nil, fmt.Errorf("failed to create knowledge agent: %w", err)
	}

	if cfg.Logger != nil {
		cfg.Logger.Info("knowledge agent created",
			zap.Bool("retriever_available", milvusRetriever != nil),
			zap.Bool("indexer_available", milvusIndexer != nil))
	}

	return agent, nil
}



