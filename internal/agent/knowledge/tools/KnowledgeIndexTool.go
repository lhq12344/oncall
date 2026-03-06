package tools

import (
	"context"
	"encoding/json"
	"fmt"

	"github.com/cloudwego/eino/components/tool"
	"github.com/cloudwego/eino/schema"
	"go.uber.org/zap"
)

// KnowledgeIndexTool 知识索引工具
type KnowledgeIndexTool struct {
	// indexer *milvusIndexer.Indexer // TODO: implement
	logger *zap.Logger
}

func NewKnowledgeIndexTool(idx interface{}, logger *zap.Logger) tool.BaseTool {
	return &KnowledgeIndexTool{
		// indexer: idx,
		logger: logger,
	}
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
	// 解析参数
	var params struct {
		Content  string                 `json:"content"`
		Metadata map[string]interface{} `json:"metadata"`
	}

	if err := json.Unmarshal([]byte(argumentsInJSON), &params); err != nil {
		return "", fmt.Errorf("invalid parameters: %w", err)
	}

	// 参数验证
	if params.Content == "" {
		return "", fmt.Errorf("content is required")
	}

	t.logger.Info("knowledge index invoked (placeholder)",
		zap.Int("content_length", len(params.Content)))

	// TODO: Implement Milvus indexing
	return `{"error": "知识索引功能尚未完全实现（需要 Milvus 集成）", "indexed": false}`, nil
}
